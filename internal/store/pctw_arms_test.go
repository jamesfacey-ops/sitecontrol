// pctw_arms_test.go — PCTW-ARMS: drives store.Author / the fold / replication
// error arms that PCTW-SWEEP found either never executed by any runnable
// test, or executed but blind to a lost %w (a fmt.Errorf("...: %w", err)
// degraded to %s: .Error() is byte-identical, but errors.Is/As can no longer
// see the wrapped cause). Each test below is produced, not just asserted: it
// FAILS when the specific %w at its named site is swapped to %s, PASSES on
// the clean tree, and FAILS again when the underlying guard is disabled (the
// "swallow the error" mutant) — see reports/witness/PCTW-ARMS.md for the
// three-way transcript.
//
// Fault injection follows the y3-faultseam precedent (reports/witness/
// y3-faultseam.md, restated in PCTW-SWEEP §5's header comment): DROP TABLE
// via a direct *sql.DB statement (same package, so s.db is reachable without
// a LocalTx round-trip) isolates a fault to one specific read/write, at
// table-level granularity. Where no table drop can isolate the fault (the
// watermark UPSERT shares its table with watermarkTx's own read), a
// same-table trigger that RAISEs on the one statement being targeted is used
// instead — still pure test-side SQL, no production code touched.
package store

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	msqlite "modernc.org/sqlite"
)

// witnessRequireRawRoot walks err through every Unwrap and fails if the
// innermost error still carries one of this module's own "store: " prefixes.
// A %w->%s swap at ANY layer leaves .Error() byte-identical but makes that
// layer the root, so this is the one assertion strings.Contains cannot fake.
// database/sql exports no sentinel for a closed *sql.DB (MEASURED, PCTW-
// SWEEP), so a closed-store fault can only be guarded this way. Ported
// verbatim from internal/domain's witness_y8_tripauthor_test.go.
func witnessRequireRawRoot(t *testing.T, tag string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: witnessRequireRawRoot called with a nil error", tag)
	}
	root := err
	for {
		u := errors.Unwrap(root)
		if u == nil {
			break
		}
		root = u
	}
	if strings.HasPrefix(root.Error(), "store: ") {
		t.Errorf("%s: the innermost error is %q — one of this package's own wrappers, so a %%w between it and the underlying fault has been lost (the full text is unchanged, so no Contains check can see this)", tag, root.Error())
	}
}

// ---------------------------------------------------------------------------
// Arm: log.go's finalize() wrap ("store: finalize %q: %w") — the F28
// per-drain-batch seam. UNEXERCISED by any test before this file: no test
// anywhere registers a Finalize that errors. High consequence: per log.go's
// own doc comment, "a Finalize failure poisons the whole batch: derived
// state cannot be reconciled with the raw rows, so nothing may commit" — the
// exact "wrong branch forks the log" shape this audit is scoped to.
// ---------------------------------------------------------------------------

var errFinalizePoison = errors.New("finalize poison")

func registerPoisonFinalize(t *testing.T, s *Store, calls *int) {
	t.Helper()
	if err := s.RegisterProjection(Projection{
		Name: "zz_finalize_poison", Table: "zz_finalize_poison", OrderBy: "k",
		Schema: `CREATE TABLE IF NOT EXISTS zz_finalize_poison (k TEXT PRIMARY KEY) STRICT;`,
		Finalize: func(tx *sql.Tx, applied []Envelope) error {
			if calls != nil {
				*calls++
			}
			return errFinalizePoison
		},
	}); err != nil {
		t.Fatal(err)
	}
}

// TestWitnessPCTWArmsAuthorFinalizeFailureRollsBackTheWholeWrite witnesses
// log.go's Author path: insertAndFold succeeds (the log row and every fold
// write land inside the transaction), then Finalize fails — Author must
// return the error AND commit nothing (the deferred Rollback discards the
// whole transaction, log row included), never a half-written event.
func TestWitnessPCTWArmsAuthorFinalizeFailureRollsBackTheWholeWrite(t *testing.T) {
	s := newTestStore(t)
	registerPoisonFinalize(t, s, nil)

	_, err := s.Author("site-x", 1, tick("ok"), 0, "default")
	if err == nil {
		t.Fatalf("Author with a failing Finalize reported SUCCESS — log.go's finalize call did not fire")
	}
	const want = "store: finalize"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("Author error = %q, want it to contain %q", err.Error(), want)
	}
	// [[PCTW-ARMS]] The Contains check above is blind to a lost %w at either
	// finalize()'s own wrap or insertAndFold's caller; only errors.Is can see
	// that the projection's own sentinel is still reachable.
	if !errors.Is(err, errFinalizePoison) {
		t.Fatalf("Author error %q does not reach errFinalizePoison via errors.Is — a %%w has been lost (the text is unchanged, so no Contains check can see this)", err)
	}
	if wm, err := s.Watermark("site-x", 1); err != nil || wm != 0 {
		t.Fatalf("Finalize failed, yet watermark = %d, %v (want 0: NOTHING may commit)", wm, err)
	}
	if evs, err := s.Events("site-x", 1, 1, 0); err != nil || len(evs) != 0 {
		t.Fatalf("Finalize failed, yet the log holds %v (want none: the whole write must roll back)", evs)
	}
}

// TestWitnessPCTWArmsApplyBatchFinalizeFailurePoisonsTheWholeBatch witnesses
// the same wrap reached from applyBatch: TWO envelopes both fold cleanly
// (insertAndFold succeeds for each), then the ONE per-batch Finalize call
// fails — the doc comment's claim ("poisons the whole batch") means BOTH
// already-folded envelopes must roll back, not just the batch's tail.
func TestWitnessPCTWArmsApplyBatchFinalizeFailurePoisonsTheWholeBatch(t *testing.T) {
	s := newTestStore(t)
	var calls int
	registerPoisonFinalize(t, s, &calls)

	evs := foreignStream("site-x", 1, 2)
	_, err := s.ApplyBatch(evs)
	if err == nil {
		t.Fatalf("ApplyBatch with a failing Finalize reported SUCCESS")
	}
	if !errors.Is(err, errFinalizePoison) {
		t.Fatalf("ApplyBatch error %q does not reach errFinalizePoison via errors.Is — a %%w has been lost", err)
	}
	if calls != 1 {
		t.Fatalf("Finalize called %d times, want exactly 1 (once per batch, even though the batch poisons)", calls)
	}
	if wm, err := s.Watermark("site-x", 1); err != nil || wm != 0 {
		t.Fatalf("Finalize failed, yet watermark = %d, %v (want 0: BOTH folded envelopes must roll back)", wm, err)
	}
	if evs, err := s.Events("site-x", 1, 1, 0); err != nil || len(evs) != 0 {
		t.Fatalf("Finalize failed, yet the log holds %v (want none)", evs)
	}
}

// ---------------------------------------------------------------------------
// Arm: log.go's applyBatch begin-failure wrap ("store: apply: %w") and the
// public Watermark() begin-failure wrap ("store: watermark: %w"). Both
// UNEXERCISED before this file. Fault: close the store first — database/sql
// exports no sentinel for "database is closed" (MEASURED, PCTW-SWEEP), so
// only witnessRequireRawRoot's Unwrap walk can see a lost %w here, the same
// technique already used for the Author-side closed-store arm (domain's
// witness_y8_tripauthor_test.go X238/X320).
// ---------------------------------------------------------------------------

func TestWitnessPCTWArmsApplyBatchOnAClosedStoreIsWrapped(t *testing.T) {
	s := newTestStore(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	ev := foreignStream("site-x", 1, 1)[0]
	_, err := s.ApplyBatch([]Envelope{ev})
	if err == nil {
		t.Fatalf("ApplyBatch against a CLOSED store reported SUCCESS — log.go's Begin call did not fire")
	}
	const want = "store: apply:"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err.Error(), want)
	}
	witnessRequireRawRoot(t, "PCTW-ARMS-APPLYBEGIN", err)
}

func TestWitnessPCTWArmsWatermarkOnAClosedStoreIsWrapped(t *testing.T) {
	s := newTestStore(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := s.Watermark("site-x", 1)
	if err == nil {
		t.Fatalf("Watermark() against a CLOSED store reported SUCCESS — log.go's Begin call did not fire")
	}
	const want = "store: watermark:"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err.Error(), want)
	}
	witnessRequireRawRoot(t, "PCTW-ARMS-WATERMARKBEGIN", err)
}

// ---------------------------------------------------------------------------
// Arm: watermarkTx's own read wrap ("store: read watermark (%s, %d): %w"),
// reached from BOTH Author (before insertAndFold ever runs) and applyBatch's
// per-envelope loop. UNEXERCISED before this file. High consequence: this is
// the read that computes "the next seq is this stream's watermark + 1" (AR-5)
// — a silently-misread watermark is exactly a "wrong branch forks the log"
// shape, not merely a read-path inconvenience.
// ---------------------------------------------------------------------------

func TestWitnessPCTWArmsAuthorReadWatermarkFailureIsWrapped(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.db.Exec(`DROP TABLE watermark`); err != nil {
		t.Fatal(err)
	}
	_, err := s.Author("site-x", 1, tick("ok"), 0, "default")
	if err == nil {
		t.Fatalf("Author against a DROPPED watermark table reported SUCCESS — log.go's watermarkTx read did not fire")
	}
	const wantCause = "no such table: watermark"
	if !strings.Contains(err.Error(), wantCause) {
		t.Errorf("error = %q, want it to name the dropped table (%q)", err.Error(), wantCause)
	}
	var sqlErr *msqlite.Error
	if !errors.As(err, &sqlErr) {
		t.Errorf("error %q does not reach the driver's *sqlite.Error via errors.As — a %%w has been lost (the text is unchanged, so no Contains check can see this)", err)
	}
}

func TestWitnessPCTWArmsApplyBatchReadWatermarkFailureIsWrapped(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.db.Exec(`DROP TABLE watermark`); err != nil {
		t.Fatal(err)
	}
	ev := foreignStream("site-x", 1, 1)[0]
	_, err := s.ApplyBatch([]Envelope{ev})
	if err == nil {
		t.Fatalf("ApplyBatch against a DROPPED watermark table reported SUCCESS — log.go's watermarkTx read did not fire")
	}
	const wantCause = "no such table: watermark"
	if !strings.Contains(err.Error(), wantCause) {
		t.Errorf("error = %q, want it to name the dropped table (%q)", err.Error(), wantCause)
	}
	var sqlErr *msqlite.Error
	if !errors.As(err, &sqlErr) {
		t.Errorf("error %q does not reach the driver's *sqlite.Error via errors.As — a %%w has been lost", err)
	}
}

// ---------------------------------------------------------------------------
// Arm: insertAndFold's watermark UPSERT wrap ("store: advance watermark %v:
// %w"). UNEXERCISED before this file. Cannot be isolated by dropping the
// watermark table (that fails the EARLIER read inside watermarkTx instead —
// table-level fault granularity cannot tell the two calls on the same table
// apart, the same ceiling PCTW-SWEEP's y3-faultseam precedent names). A
// BEFORE INSERT trigger that RAISEs targets the UPSERT specifically, while
// leaving the plain SELECT in watermarkTx untouched — still pure test-side
// SQL DDL, no production code touched.
// ---------------------------------------------------------------------------

func TestWitnessPCTWArmsAdvanceWatermarkFailureIsWrapped(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.db.Exec(`CREATE TRIGGER zz_watermark_poison BEFORE INSERT ON watermark
		BEGIN SELECT RAISE(ABORT, 'watermark poison'); END;`); err != nil {
		t.Fatal(err)
	}
	_, err := s.Author("site-x", 1, tick("ok"), 0, "default")
	if err == nil {
		t.Fatalf("Author whose watermark UPSERT is poisoned by a trigger reported SUCCESS — log.go:312's guard did not fire")
	}
	const wantCause = "watermark poison"
	if !strings.Contains(err.Error(), wantCause) {
		t.Errorf("error = %q, want it to name the trigger's abort message (%q)", err.Error(), wantCause)
	}
	var sqlErr *msqlite.Error
	if !errors.As(err, &sqlErr) {
		t.Errorf("error %q does not reach the driver's *sqlite.Error via errors.As — a %%w has been lost (the text is unchanged, so no Contains check can see this)", err)
	}
	// The poisoned watermark advance must roll back EVERYTHING inside the
	// same transaction: no log row surviving with no matching watermark
	// advance — that would corrupt the per-stream contiguity invariant AR-5
	// depends on (a log row above the recorded watermark).
	if wm, err := s.Watermark("site-x", 1); err != nil || wm != 0 {
		t.Fatalf("watermark advance poisoned, yet Watermark() = %d, %v (want 0: the whole write must roll back)", wm, err)
	}
	if evs, err := s.Events("site-x", 1, 1, 0); err != nil || len(evs) != 0 {
		t.Fatalf("watermark advance poisoned, yet the log holds %v (want none)", evs)
	}
}

// ---------------------------------------------------------------------------
// Arm: Events' and Epochs' own db.Query wraps ("store: read events (...): %w"
// / "store: epochs for %s: %w") — the C3a pull server's read paths (Events
// serves a stream page, Epochs announces which streams exist). Both
// UNEXERCISED before this file. Directly "replication": a puller that cannot
// tell "this post has nothing yet" (empty, nil error) from "the read itself
// failed" (non-nil error, wrongly typed) risks treating a broken source as an
// empty, fully-caught-up one.
// ---------------------------------------------------------------------------

func TestWitnessPCTWArmsEventsReadFailureIsWrapped(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.db.Exec(`DROP TABLE event_log`); err != nil {
		t.Fatal(err)
	}
	_, err := s.Events("site-x", 1, 1, 0)
	if err == nil {
		t.Fatalf("Events() against a DROPPED event_log table reported SUCCESS — log.go's replication read did not fire")
	}
	const wantCause = "no such table: event_log"
	if !strings.Contains(err.Error(), wantCause) {
		t.Errorf("error = %q, want it to name the dropped table (%q)", err.Error(), wantCause)
	}
	var sqlErr *msqlite.Error
	if !errors.As(err, &sqlErr) {
		t.Errorf("error %q does not reach the driver's *sqlite.Error via errors.As — a %%w has been lost (the text is unchanged, so no Contains check can see this)", err)
	}
}

// ---------------------------------------------------------------------------
// Arm: applyBatch's double-failure wrap ("store: apply %v: %v; and the clean
// prefix failed on re-apply: %w", log.go:240) — UNEXERCISED by any test
// before this file, and the rarest arm in the file: it fires only when the
// ALREADY-ROLLED-BACK clean prefix (envs[:i]) fails AGAIN on its one retry.
// The function's own doc comment states folds are "pure functions of
// transaction state, so the prefix re-applies identically" — this test
// deliberately builds a fold that VIOLATES that invariant (an in-memory call
// counter survives the DB rollback that undoes its row) to reach the arm at
// all. That the arm requires breaking a stated invariant to reach is itself
// the finding: on THIS tree, with every registered fold obeying purity, this
// site is unreachable in practice, not merely untested.
// ---------------------------------------------------------------------------

var errFlakyOnRetry = errors.New("flaky fold: second invocation")

// registerFlakyFold's Apply: a payload marked "poison" always fails (the
// ordinary mid-batch fault that triggers the retry machinery at all); any
// other envelope's fold succeeds on its FIRST call per seq and fails on
// every call after — callCounts lives in the test's Go frame, not the DB
// transaction, so it is NOT rolled back with the aborted first attempt,
// which is exactly what makes the retry's second call see a "second
// invocation" for a seq that, from the DB's own state, has never committed.
func registerFlakyFold(t *testing.T, s *Store, callCounts map[int64]int) {
	t.Helper()
	if err := s.RegisterProjection(Projection{
		Name: "zz_flaky", Table: "zz_flaky", OrderBy: "seq",
		Schema: `CREATE TABLE IF NOT EXISTS zz_flaky (seq INTEGER PRIMARY KEY) STRICT;`,
		Apply: func(tx *sql.Tx, env Envelope) error {
			if strings.Contains(string(env.Payload), `"k":"poison"`) {
				return errPoisonFold
			}
			callCounts[env.Seq]++
			if callCounts[env.Seq] >= 2 {
				return errFlakyOnRetry
			}
			_, err := tx.Exec(`INSERT INTO zz_flaky (seq) VALUES (?)`, env.Seq)
			return err
		},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestWitnessPCTWArmsDoubleFailureOnPrefixRetryIsWrapped(t *testing.T) {
	s := newTestStore(t)
	callCounts := map[int64]int{}
	registerFlakyFold(t, s, callCounts)

	good := Envelope{SiteID: "site-x", NodeEpoch: 1, Seq: 1,
		SchemaVersion: SchemaVersion, OriginAuth: "site-x", Payload: tick("ok")}
	poison := Envelope{SiteID: "site-x", NodeEpoch: 1, Seq: 2,
		SchemaVersion: SchemaVersion, OriginAuth: "site-x",
		Payload: []byte(`{"type":"tick","k":"poison"}`)}

	_, err := s.ApplyBatch([]Envelope{good, poison})
	if err == nil {
		t.Fatalf("ApplyBatch([good, poison]) with a flaky-on-retry fold reported SUCCESS — the double-failure arm did not fire")
	}
	const want = "and the clean prefix failed on re-apply"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want it to contain %q — the retry-of-the-clean-prefix must itself have failed", err.Error(), want)
	}
	// [[PCTW-ARMS]] Only retryErr is wrapped with %w at this site (the
	// ORIGINAL envelope's fold error is interpolated with %v, by the site's
	// own design — see log.go:240's format string); errors.Is can only ever
	// reach the RETRY's error, never the original.
	if !errors.Is(err, errFlakyOnRetry) {
		t.Fatalf("error %q does not reach errFlakyOnRetry via errors.Is — a %%w between this arm and the retry's own failure has been lost (the text is unchanged, so no Contains check can see this)", err)
	}
	// Nothing may have committed: the double failure must not leave seq 1's
	// row from either attempt.
	if wm, err := s.Watermark("site-x", 1); err != nil || wm != 0 {
		t.Fatalf("double failure on retry, yet watermark = %d, %v (want 0)", wm, err)
	}
	if evs, err := s.Events("site-x", 1, 1, 0); err != nil || len(evs) != 0 {
		t.Fatalf("double failure on retry, yet the log holds %v (want none)", evs)
	}
}

func TestWitnessPCTWArmsEpochsReadFailureIsWrapped(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.db.Exec(`DROP TABLE event_log`); err != nil {
		t.Fatal(err)
	}
	_, err := s.Epochs("site-x")
	if err == nil {
		t.Fatalf("Epochs() against a DROPPED event_log table reported SUCCESS — log.go's replication read did not fire")
	}
	const wantCause = "no such table: event_log"
	if !strings.Contains(err.Error(), wantCause) {
		t.Errorf("error = %q, want it to name the dropped table (%q)", err.Error(), wantCause)
	}
	var sqlErr *msqlite.Error
	if !errors.As(err, &sqlErr) {
		t.Errorf("error %q does not reach the driver's *sqlite.Error via errors.As — a %%w has been lost", err)
	}
}
