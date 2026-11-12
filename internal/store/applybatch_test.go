// applybatch_test.go — F28: ApplyBatch's contract. Correctness of WHAT a
// batch computes is held by domain's differential/batch-shape tests; this
// file pins the store-level semantics the replication engine leans on: the
// applied prefix commits when a mid-batch envelope fails (watermark rests at
// the last good seq, exactly the per-envelope loop's outcome), duplicates
// are per-envelope no-ops inside a batch, and Finalize runs once per batch
// over only the applied envelopes.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestApplyBatchCommitsThePrefixBeforeAMidBatchGap(t *testing.T) {
	s := newTestStore(t)
	evs := foreignStream("site-x", 1, 3)
	// seq 1, then a gap (seq 3): prefix {seq 1} must land, gap must be typed.
	results, err := s.ApplyBatch([]Envelope{evs[0], evs[2]})
	var gap *GapError
	if !errors.As(err, &gap) {
		t.Fatalf("want *GapError, got %v", err)
	}
	if len(results) != 1 || !results[0] {
		t.Fatalf("want the one-envelope applied prefix, got %v", results)
	}
	wm, err := s.Watermark("site-x", 1)
	if err != nil || wm != 1 {
		t.Fatalf("watermark after prefix commit: %d, %v (want 1)", wm, err)
	}
	// The stream is not poisoned: the contiguous remainder applies.
	if results, err = s.ApplyBatch(evs[1:]); err != nil || len(results) != 2 || !results[0] || !results[1] {
		t.Fatalf("remainder: %v, %v", results, err)
	}
}

func TestApplyBatchTreatsDuplicatesAsPerEnvelopeNoOps(t *testing.T) {
	s := newTestStore(t)
	evs := foreignStream("site-x", 1, 2)
	mustApplyAll(t, s, evs[:1])
	results, err := s.ApplyBatch(evs) // [duplicate, new]
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0] || !results[1] {
		t.Fatalf("want [dup applied], got %v", results)
	}
}

func TestFinalizeRunsOncePerBatchOverAppliedEnvelopesOnly(t *testing.T) {
	s := newTestStore(t)
	var calls int
	var lastBatch []Envelope
	if err := s.RegisterProjection(Projection{
		Name: "zz_finalize_probe", Table: "zz_finalize_probe", OrderBy: "k",
		Schema: `CREATE TABLE IF NOT EXISTS zz_finalize_probe (k TEXT PRIMARY KEY) STRICT;`,
		Finalize: func(tx *sql.Tx, applied []Envelope) error {
			calls++
			lastBatch = applied
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	evs := foreignStream("site-x", 1, 3)
	mustApplyAll(t, s, evs[:1]) // batch of one: one call, one envelope
	if calls != 1 || len(lastBatch) != 1 {
		t.Fatalf("after Apply: calls=%d batch=%d", calls, len(lastBatch))
	}
	// [duplicate, new, new]: one call, the two APPLIED envelopes only.
	if _, err := s.ApplyBatch(evs); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(lastBatch) != 2 || lastBatch[0].Seq != 2 || lastBatch[1].Seq != 3 {
		t.Fatalf("after batch: calls=%d batch=%v", calls, lastBatch)
	}
	// An all-duplicate batch applies nothing — Finalize must not run.
	if _, err := s.ApplyBatch(evs); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("finalize ran on an all-duplicate batch (calls=%d)", calls)
	}
}

// errPoisonFold is the fold's own sentinel — planted so a test can require
// (via errors.Is) that log.go's `store: fold %q on %v: %w` wrap keeps this
// value reachable through the chain. A `%w` -> `%s` swap at that site leaves
// every strings.Contains(err.Error(), "poison fold failure") check below
// byte-identical (PRODUCED: MEASURED green under that swap before the
// errors.Is guards were added) while breaking Unwrap — only errors.Is can
// see it. [[PCTW-ARMS]]
var errPoisonFold = errors.New("poison fold failure")

// registerPartialWriteFold registers a projection whose fold writes a row and
// THEN fails on any envelope whose payload k is "poison" — the mid-writes
// fold failure shape (verification finding on the F28 landing: ApplyBatch
// committed the failing envelope's partial writes where the per-envelope
// Apply rolled them back).
func registerPartialWriteFold(t *testing.T, s *Store) {
	t.Helper()
	if err := s.RegisterProjection(Projection{
		Name: "zz_partial", Table: "zz_partial", OrderBy: "seq",
		Schema: `CREATE TABLE IF NOT EXISTS zz_partial (seq INTEGER PRIMARY KEY) STRICT;`,
		Apply: func(tx *sql.Tx, env Envelope) error {
			// Write FIRST, then decide to fail — the partial-write shape.
			if _, err := tx.Exec(`INSERT INTO zz_partial (seq) VALUES (?)`, env.Seq); err != nil {
				return err
			}
			var p struct {
				K string `json:"k"`
			}
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				return err
			}
			if p.K == "poison" {
				return errPoisonFold
			}
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
}

// TestFoldErrorLeavesNoTraceOfTheFailingEnvelope: a fold error must roll back
// the FAILING envelope's writes entirely — log row, partial fold writes,
// everything — while the batch's applied prefix still commits. Anything less
// leaves committed state where derived != f(raw) (an event_log row above the
// watermark with no derived state — e.g. a poisoned trip_arrival's committed
// OPEN visit on the muster) and makes the retry fail on the event_log PRIMARY
// KEY instead of the true fold error, permanently masking it.
func TestFoldErrorLeavesNoTraceOfTheFailingEnvelope(t *testing.T) {
	s := newTestStore(t)
	registerPartialWriteFold(t, s)
	good := Envelope{SiteID: "site-x", NodeEpoch: 1, Seq: 1,
		SchemaVersion: SchemaVersion, OriginAuth: "site-x", Payload: tick("ok")}
	poison := Envelope{SiteID: "site-x", NodeEpoch: 1, Seq: 2,
		SchemaVersion: SchemaVersion, OriginAuth: "site-x",
		Payload: json.RawMessage(`{"type":"tick","k":"poison"}`)}

	results, err := s.ApplyBatch([]Envelope{good, poison})
	if err == nil || !strings.Contains(err.Error(), "poison fold failure") {
		t.Fatalf("want the fold error surfaced, got %v", err)
	}
	// [[PCTW-ARMS]] The Contains check above is blind to a lost %w: log.go's
	// `store: fold %q on %v: %w` wrap can degrade to %s without changing
	// .Error() at all. Only errors.Is, walking the real Unwrap chain, can see
	// that the fold's own sentinel is still reachable.
	if !errors.Is(err, errPoisonFold) {
		t.Fatalf("ApplyBatch error %q does not reach errPoisonFold via errors.Is — a %%w between insertAndFold's fold wrap and the fold's own error has been lost (the text is unchanged, so no Contains check can see this)", err)
	}
	if len(results) != 1 || !results[0] {
		t.Fatalf("want the good prefix applied, got %v", results)
	}
	if wm, err := s.Watermark("site-x", 1); err != nil || wm != 1 {
		t.Fatalf("watermark = %d, %v (want 1)", wm, err)
	}
	// The poisoned envelope must have left NOTHING: no log row above the
	// watermark, no partial fold write.
	evs, err := s.Events("site-x", 1, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 0 {
		t.Fatalf("poisoned envelope's log row was committed above the watermark: %v", evs[0].Key())
	}
	b, err := s.DumpProjection("zz_partial")
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `[{"seq":1}]` {
		t.Fatalf("partial fold write of the poisoned envelope was committed: %s", b)
	}
	// The retry must fail with the TRUE fold error again — not an event_log
	// primary-key conflict masking it forever.
	_, err = s.ApplyBatch([]Envelope{poison})
	if err == nil || !strings.Contains(err.Error(), "poison fold failure") {
		t.Fatalf("retry error = %v, want the original fold error (not a PK conflict mask)", err)
	}
	if !errors.Is(err, errPoisonFold) { // [[PCTW-ARMS]] same lost-%w blindness as above
		t.Fatalf("retry error %q does not reach errPoisonFold via errors.Is — a %%w has been lost", err)
	}
	// And Apply IS the batch of one — the single-envelope path must roll
	// back identically (this was the old per-envelope guarantee).
	s2 := newTestStore(t)
	registerPartialWriteFold(t, s2)
	poison.Seq = 1 // fresh stream: seq 1 is the applicable position
	if _, err := s2.Apply(poison); err == nil || !strings.Contains(err.Error(), "poison fold failure") {
		t.Fatalf("Apply(poison) error = %v", err)
	} else if !errors.Is(err, errPoisonFold) { // [[PCTW-ARMS]]
		t.Fatalf("Apply(poison) error %q does not reach errPoisonFold via errors.Is — a %%w has been lost", err)
	}
	if evs, err := s2.Events("site-x", 1, 1, 0); err != nil || len(evs) != 0 {
		t.Fatalf("Apply committed the poisoned log row: %v %v", evs, err)
	}
	if _, err := s2.Apply(poison); err == nil || !strings.Contains(err.Error(), "poison fold failure") {
		t.Fatalf("Apply retry error = %v, want the original fold error", err)
	} else if !errors.Is(err, errPoisonFold) { // [[PCTW-ARMS]]
		t.Fatalf("Apply retry error %q does not reach errPoisonFold via errors.Is — a %%w has been lost", err)
	}
}
