// C2 done-when tests (chunk-plan-v2 C2), plus the triple-key and permutation
// guarantees the design critique made load-bearing (§8.3/8.4, M4).
package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"path/filepath"
	"strings"
	"testing"
)

// Two well-behaved test folds exercising the framework:
//   - test_tally: commutative-across-streams aggregate (upsert n+1 per key).
//   - test_seen: one row per event, keyed on the full triple.
//
// Both may rely on in-order delivery WITHIN a stream (the watermark enforces
// it) and must be order-independent ACROSS streams — the fold contract.
//
// Plus a LOCAL STUB standing in for the trip/visit dump-only built-ins that
// Open used to self-register (removed at C5 per C2 review L1 / chunk-carry-
// forward row 6/8: RegisterProjection rejects duplicate names, so a real C6
// trip fold could never register alongside a built-in of the same name, and
// registration now lives at the composition root, domain.Register, which
// store cannot import without a cycle). C5's brief sanctions exactly this
// fix over restoring the built-ins: "fix the test to call domain.Register
// (or register a local stub)" — store's own tests stay domain-independent by
// taking the stub option.
func testProjections() []Projection {
	return []Projection{
		{
			Name:    "test_tally",
			Table:   "test_tally",
			OrderBy: "k",
			Schema: `CREATE TABLE IF NOT EXISTS test_tally (
				k TEXT PRIMARY KEY, n INTEGER NOT NULL) STRICT;`,
			Apply: func(tx *sql.Tx, env Envelope) error {
				var p struct {
					Type string `json:"type"`
					K    string `json:"k"`
				}
				if err := json.Unmarshal(env.Payload, &p); err != nil {
					return err
				}
				if p.Type != "tick" {
					return nil
				}
				_, err := tx.Exec(`INSERT INTO test_tally (k, n) VALUES (?, 1)
					ON CONFLICT(k) DO UPDATE SET n = n + 1`, p.K)
				return err
			},
		},
		{
			Name:    "test_seen",
			Table:   "test_seen",
			OrderBy: "site_id, node_epoch, seq",
			Schema: `CREATE TABLE IF NOT EXISTS test_seen (
				site_id TEXT NOT NULL, node_epoch INTEGER NOT NULL, seq INTEGER NOT NULL,
				payload TEXT NOT NULL,
				PRIMARY KEY (site_id, node_epoch, seq)) STRICT;`,
			Apply: func(tx *sql.Tx, env Envelope) error {
				_, err := tx.Exec(`INSERT INTO test_seen (site_id, node_epoch, seq, payload)
					VALUES (?, ?, ?, ?)`, env.SiteID, env.NodeEpoch, env.Seq, string(env.Payload))
				return err
			},
		},
		// trip/visit local stubs (see doc comment above): dump-only, no
		// Schema (the tables already exist via store's own schema const), no
		// Apply (nil Apply = dump-only, per Projection's own doc).
		{Name: "trip", Table: "trip", OrderBy: "trip_id"},
		{Name: "visit", Table: "visit", OrderBy: "visit_id"},
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	for _, p := range testProjections() {
		if err := s.RegisterProjection(p); err != nil {
			t.Fatalf("register %s: %v", p.Name, err)
		}
	}
	return s
}

func tick(k string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"type":"tick","k":%q}`, k))
}

// foreignStream builds a stream of envelopes AS IF authored elsewhere — the
// done-when (d) mechanism that makes C5/C6 buildable before replication exists.
func foreignStream(siteID string, epoch int64, n int) []Envelope {
	evs := make([]Envelope, n)
	for i := range evs {
		evs[i] = Envelope{
			SiteID: siteID, NodeEpoch: epoch, Seq: int64(i + 1),
			SchemaVersion: SchemaVersion, OriginAuth: siteID,
			Payload: tick(fmt.Sprintf("%s-k%d", siteID, i%3)),
		}
	}
	return evs
}

func mustApplyAll(t *testing.T, s *Store, evs []Envelope) {
	t.Helper()
	for _, ev := range evs {
		if applied, err := s.Apply(ev); err != nil {
			t.Fatalf("apply %v: %v", ev.Key(), err)
		} else if !applied {
			t.Fatalf("apply %v: unexpectedly deduped on first delivery", ev.Key())
		}
	}
}

func mustDump(t *testing.T, s *Store) []byte {
	t.Helper()
	b, err := s.DumpProjections()
	if err != nil {
		t.Fatalf("dump: %v", err)
	}
	return b
}

// --- own authoring: per-epoch contiguous seq assignment ---

func TestAuthorAssignsContiguousSeq(t *testing.T) {
	s := newTestStore(t)
	for i := 1; i <= 5; i++ {
		env, err := s.Author("site-a", 1, tick("a"), 0, "default")
		if err != nil {
			t.Fatalf("author #%d: %v", i, err)
		}
		if env.Seq != int64(i) {
			t.Fatalf("author #%d: seq = %d, want %d (contiguous)", i, env.Seq, i)
		}
		if env.SchemaVersion != SchemaVersion {
			t.Fatalf("envelope schema_version = %d, want store.SchemaVersion = %d", env.SchemaVersion, SchemaVersion)
		}
		if env.OriginAuth != "site-a" {
			t.Fatalf("origin_auth = %q, want authoring site", env.OriginAuth)
		}
	}
	if wm, _ := s.Watermark("site-a", 1); wm != 5 {
		t.Fatalf("own watermark = %d, want 5", wm)
	}
	evs, err := s.Events("site-a", 1, 1, 0)
	if err != nil || len(evs) != 5 {
		t.Fatalf("events: %d, %v; want 5", len(evs), err)
	}
}

// --- done-when (a): idempotency — re-delivering the same stream is a no-op ---

func TestRedeliveryIsByteIdenticalNoOp(t *testing.T) {
	s := newTestStore(t)
	// Own-authored stream plus an injected foreign stream.
	for i := 0; i < 10; i++ {
		if _, err := s.Author("site-a", 1, tick(fmt.Sprintf("a-k%d", i%2)), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	foreign := foreignStream("site-x", 1, 8)
	mustApplyAll(t, s, foreign)
	dump1 := mustDump(t, s)

	// Re-deliver the ENTIRE foreign stream and the own stream (read back from
	// the log, as a peer would re-serve it).
	own, err := s.Events("site-a", 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range append(append([]Envelope{}, foreign...), own...) {
		applied, err := s.Apply(ev)
		if err != nil {
			t.Fatalf("redeliver %v: %v", ev.Key(), err)
		}
		if applied {
			t.Fatalf("redeliver %v: duplicate was APPLIED — idempotency broken", ev.Key())
		}
	}
	if dump2 := mustDump(t, s); !bytes.Equal(dump1, dump2) {
		t.Fatalf("dump changed after duplicate delivery:\n was: %s\n now: %s", dump1, dump2)
	}
}

// --- done-when (b) + M4: replay from zero, in PERMUTED delivery orders,
// into a blank store reproduces the incrementally-built dump ---

// interleave merges streams into one delivery order, preserving each stream's
// internal order, choosing the next stream with a seeded RNG.
func interleave(rng *rand.Rand, streams ...[]Envelope) []Envelope {
	idx := make([]int, len(streams))
	var out []Envelope
	for {
		var live []int
		for i, st := range streams {
			if idx[i] < len(st) {
				live = append(live, i)
			}
		}
		if len(live) == 0 {
			return out
		}
		pick := live[rng.Intn(len(live))]
		out = append(out, streams[pick][idx[pick]])
		idx[pick]++
	}
}

func TestReplayFromZeroPermutedOrderDeterminism(t *testing.T) {
	// Build the reference store incrementally: three streams (own authoring
	// plus two injected foreign sites — nothing assumes exactly two peers).
	ref := newTestStore(t)
	for i := 0; i < 12; i++ {
		if _, err := ref.Author("site-a", 1, tick(fmt.Sprintf("a-k%d", i%3)), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	sx := foreignStream("site-x", 1, 9)
	sy := foreignStream("site-y", 2, 7) // a later-epoch stream, for variety
	mustApplyAll(t, ref, sx)
	mustApplyAll(t, ref, sy)
	refDump := mustDump(t, ref)

	sa, err := ref.Events("site-a", 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Deliver into blank stores in several distinct orders: stream-by-stream,
	// reversed stream order, and five seeded random interleavings.
	orders := [][]Envelope{
		interleave(rand.New(rand.NewSource(0)), sa, sx, sy),
	}
	orders = append(orders, append(append(append([]Envelope{}, sa...), sx...), sy...))
	orders = append(orders, append(append(append([]Envelope{}, sy...), sx...), sa...))
	for seed := int64(1); seed <= 5; seed++ {
		orders = append(orders, interleave(rand.New(rand.NewSource(seed)), sa, sx, sy))
	}

	for i, order := range orders {
		blank := newTestStore(t)
		mustApplyAll(t, blank, order)
		if d := mustDump(t, blank); !bytes.Equal(refDump, d) {
			t.Fatalf("order %d: replayed dump diverges from incremental dump\n ref: %s\n got: %s", i, refDump, d)
		}
	}
}

// --- done-when (c): a gap stalls the watermark loudly, never silently ---

func TestGapStallsWatermarkLoudly(t *testing.T) {
	s := newTestStore(t)
	st := foreignStream("site-x", 1, 5)
	mustApplyAll(t, s, st[:2]) // seq 1, 2
	before := mustDump(t, s)

	applied, err := s.Apply(st[3]) // seq 4: gap (3 missing)
	if applied || err == nil {
		t.Fatalf("gap delivery: applied=%v err=%v — must refuse loudly", applied, err)
	}
	var gap *GapError
	if !errors.As(err, &gap) {
		t.Fatalf("gap error is %T (%v), want *GapError", err, err)
	}
	if gap.Watermark != 2 || gap.Key.Seq != 4 {
		t.Fatalf("gap error names watermark %d / seq %d, want 2 / 4", gap.Watermark, gap.Key.Seq)
	}
	if wm, _ := s.Watermark("site-x", 1); wm != 2 {
		t.Fatalf("watermark advanced across a gap: %d", wm)
	}
	if after := mustDump(t, s); !bytes.Equal(before, after) {
		t.Fatal("a refused gap delivery mutated a projection")
	}
	// The stall is recoverable: deliver the missing seq, then the rest.
	mustApplyAll(t, s, st[2:])
	if wm, _ := s.Watermark("site-x", 1); wm != 5 {
		t.Fatalf("watermark after backfill = %d, want 5", wm)
	}
}

// --- the idempotency key is the FULL triple, epoch-major (§8.3) ---
// A re-imaged site's new epoch restarts seq at 1; keying on (site_id, seq)
// would dedup the whole new stream as already-applied. This is the test that
// guards the design round spent on exactly that failure.

func TestNewEpochIsANewStreamNotADuplicate(t *testing.T) {
	s := newTestStore(t)
	mustApplyAll(t, s, foreignStream("site-x", 1, 6)) // epoch 1, seqs 1..6

	epoch2 := foreignStream("site-x", 2, 3) // re-image: epoch 2, seqs 1..3
	for _, ev := range epoch2 {
		applied, err := s.Apply(ev)
		if err != nil {
			t.Fatalf("epoch-2 %v: %v", ev.Key(), err)
		}
		if !applied {
			t.Fatalf("epoch-2 %v deduped against epoch 1 — the key must be the full (site_id, node_epoch, seq) triple", ev.Key())
		}
	}
	// Each epoch keeps its own watermark (§8.4: fresh watermark at zero).
	if wm, _ := s.Watermark("site-x", 1); wm != 6 {
		t.Fatalf("epoch-1 watermark = %d, want 6", wm)
	}
	if wm, _ := s.Watermark("site-x", 2); wm != 3 {
		t.Fatalf("epoch-2 watermark = %d, want 3", wm)
	}
}

// --- done-when (e): nullable recorded_by on visit and trip from first schema ---

func TestRecordedByNullableFromFirstSchema(t *testing.T) {
	s := newTestStore(t)
	for _, table := range []string{"trip", "visit"} {
		rows, err := s.db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for rows.Next() {
			var cid, notnull, pk int
			var name, ctype string
			var dflt any
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
				t.Fatal(err)
			}
			if name == "recorded_by" {
				found = true
				if notnull != 0 {
					t.Fatalf("%s.recorded_by is NOT NULL — must be nullable (§10)", table)
				}
			}
		}
		rows.Close()
		if !found {
			t.Fatalf("%s has no recorded_by column — unbackfillable later (§10)", table)
		}
	}
}

// --- guards on the log and the apply loop ---

func TestEventLogIsAppendOnlyByTrigger(t *testing.T) {
	s := newTestStore(t)
	mustApplyAll(t, s, foreignStream("site-x", 1, 1))
	if _, err := s.db.Exec(`UPDATE event_log SET payload = '{}'`); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("UPDATE on event_log: err = %v, want append-only abort", err)
	}
	if _, err := s.db.Exec(`DELETE FROM event_log`); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("DELETE on event_log: err = %v, want append-only abort", err)
	}
}

func TestApplyRefusesForeignSchemaVersion(t *testing.T) {
	s := newTestStore(t)
	ev := foreignStream("site-x", 1, 1)[0]
	ev.SchemaVersion = SchemaVersion + 1
	applied, err := s.Apply(ev)
	var sv *SchemaVersionError
	if applied || !errors.As(err, &sv) {
		t.Fatalf("foreign schema_version: applied=%v err=%v, want *SchemaVersionError", applied, err)
	}
	if wm, _ := s.Watermark("site-x", 1); wm != 0 {
		t.Fatalf("refused event advanced the watermark to %d", wm)
	}
}

func TestApplyRefusesInvalidEnvelope(t *testing.T) {
	s := newTestStore(t)
	bad := []Envelope{
		{SiteID: "", NodeEpoch: 1, Seq: 1, SchemaVersion: SchemaVersion, OriginAuth: "x", Payload: tick("k")},
		{SiteID: "site-x", NodeEpoch: 0, Seq: 1, SchemaVersion: SchemaVersion, OriginAuth: "x", Payload: tick("k")},
		{SiteID: "site-x", NodeEpoch: 1, Seq: 0, SchemaVersion: SchemaVersion, OriginAuth: "x", Payload: tick("k")},
		{SiteID: "site-x", NodeEpoch: 1, Seq: 1, SchemaVersion: SchemaVersion, OriginAuth: "x", Payload: json.RawMessage(`{not json`)},
	}
	for i, ev := range bad {
		if applied, err := s.Apply(ev); applied || err == nil {
			t.Fatalf("bad envelope %d accepted (applied=%v err=%v)", i, applied, err)
		}
	}
}

func TestDumpIsCanonicalAndIncludesBuiltins(t *testing.T) {
	s := newTestStore(t)
	mustApplyAll(t, s, foreignStream("site-x", 1, 4))
	d1 := mustDump(t, s)
	d2 := mustDump(t, s)
	if !bytes.Equal(d1, d2) {
		t.Fatal("two dumps of unchanged state differ — not canonical")
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(d1, &m); err != nil {
		t.Fatalf("dump is not valid JSON: %v", err)
	}
	for _, name := range []string{"trip", "visit", "test_tally", "test_seen"} {
		v, ok := m[name]
		if !ok {
			t.Fatalf("dump missing projection %q", name)
		}
		if string(v) == "null" {
			t.Fatalf("projection %q dumps as null, want [] when empty", name)
		}
	}
}

// --- C3a store extensions (C2 review M1): paginated Events + Epochs ---

func TestEventsPaginatesAndEpochsEnumerates(t *testing.T) {
	s := newTestStore(t)
	mustApplyAll(t, s, foreignStream("site-x", 1, 7))
	mustApplyAll(t, s, foreignStream("site-x", 3, 2)) // a later epoch, gap in epoch numbers is legal

	page, err := s.Events("site-x", 1, 3, 2) // from seq 3, limit 2
	if err != nil || len(page) != 2 || page[0].Seq != 3 || page[1].Seq != 4 {
		t.Fatalf("page = %d events (err %v), want seqs [3 4]", len(page), err)
	}
	all, err := s.Events("site-x", 1, 1, 0) // limit 0 = unlimited
	if err != nil || len(all) != 7 {
		t.Fatalf("unlimited read = %d events (err %v), want 7", len(all), err)
	}
	epochs, err := s.Epochs("site-x")
	if err != nil || len(epochs) != 2 || epochs[0] != 1 || epochs[1] != 3 {
		t.Fatalf("epochs = %v (err %v), want [1 3]", epochs, err)
	}
	if epochs, _ := s.Epochs("site-never"); len(epochs) != 0 {
		t.Fatalf("epochs of unknown site = %v, want empty", epochs)
	}
}
