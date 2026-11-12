// w5d_reversal_test.go — W5d-7 (W3-5 cloned verbatim against company): the
// merge reversal round trip (merge → Clear:["same_as"] → EFFECTIVE-VALUE
// equality with the never-merged state, NEVER erasure) and
// order-independence across streams under permuted delivery, proven by
// BYTE-IDENTICAL projection/dump state.
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, W5d LOCKED 2026-08-31.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD — that IS the clean-tree red.
//
// KILLER MUTANT: W3-m10 — compose same_as/amendments by ARRIVAL order
// (rowid). The permuted-delivery fixture is its sole named killer: a
// value-scramble over a fixed stream cannot produce an
// arrival-contradicts-key order.
//
// ⚠ The byte-identical dump comparison is meaningful only because the dump
// path is STRUCTURALLY RAW: cmd/dumpcompare reads a file written by
// `sitectl -dump` (GET /debug/projection-dump until D-56 removed it; same
// bytes either way) → Store.DumpProjections() → the raw projection tables, never
// domain.Companies() [MEASURED — web/web.go:322–330, cmd/dumpcompare/main.go:40–47].
// A composed dump would compare composed state and this fixture would prove
// nothing. Recorded as a standing constraint at W5d-5.
package domain

import (
	"encoding/json"
	"testing"

	"sitecontrol/internal/store"
)

// TestW5dReversalRoundTripEqualsNeverMerged — W5d-7's first half: after
// merge X→Y then Clear:["same_as"], every read returns effective-value
// equality with the never-merged state — equality of EFFECTIVE VALUES,
// never erasure: both events remain in the log (append-only; VF §6
// "superseded, never merely absent").
func TestW5dReversalRoundTripEqualsNeverMerged(t *testing.T) {
	merged := w3NewStore(t)
	x, y, roster := w5dCompanyPair(t, merged, "Round Trip X Ltd", "Round Trip Y Ltd")
	mergeEnv := w5dAmendLocal(t, merged, "site-a", 1, w3Base+1*w3Hour,
		CompanyAmendment{CompanyID: x, SameAs: w3Str(y)})
	// Fixture precondition (rule 6): the merge must have taken effect, or
	// the reversal below reverses nothing.
	if c := w5dCompany(t, merged, x); c.SameAs == nil || *c.SameAs != y {
		t.Fatalf("W5d-7 fixture: the merge did not compose (same_as=%s) — the round trip has nothing to reverse", w5dSameAsOf(c))
	}
	clearEnv := w5dAmendLocal(t, merged, "site-a", 1, w3Base+2*w3Hour,
		CompanyAmendment{CompanyID: x, Clear: []string{w5dFactSameAs}})

	never := w3NewStore(t)
	w3ApplyAll(t, never, roster)

	for name, st := range map[string]*store.Store{"merged-then-cleared": merged, "never-merged": never} {
		c := w5dCompany(t, st, x)
		if c.SameAs != nil {
			t.Fatalf("W5d-7: composed same_as on the %s store = %q, want ABSENT — the reversal did not take effect (or Clear composed as empty-string, W3-m7's shape)", name, *c.SameAs)
		}
		if got := w5dMergeMap(t, st).Resolve(x); got != x {
			t.Fatalf("W5d-7: Resolve(%s) on the %s store = %s, want the un-merged identity %s", x, name, got, x)
		}
	}
	mc, err := json.Marshal(w5dCompanies(t, merged))
	if err != nil {
		t.Fatal(err)
	}
	nc, err := json.Marshal(w5dCompanies(t, never))
	if err != nil {
		t.Fatal(err)
	}
	if string(mc) != string(nc) {
		t.Fatalf("W5d-7: composed Companies() after merge+un-merge differs from the never-merged state —\nmerged: %s\nnever:  %s", mc, nc)
	}

	// NEVER erasure: both envelopes remain in the log.
	found := map[store.Key]bool{}
	for _, e := range w3Events(t, merged, "site-a", 1) {
		found[e.Key()] = true
	}
	if !found[mergeEnv.Key()] || !found[clearEnv.Key()] {
		t.Fatalf("W5d-7: merge (%v) or un-merge (%v) envelope missing from the log — reversal is equality of EFFECTIVE VALUES, never erasure (append-only)", mergeEnv.Key(), clearEnv.Key())
	}
}

// TestW5dReversalOrderIndependentAcrossStreams — W5d-7's second half:
// permuted delivery orders of the SAME event set produce BYTE-IDENTICAL
// projection/dump state and identical composed reads, with the contested
// company `name` pinned to the record-order winner (site-b's) under every
// interleaving. Kills W3-m10: an arrival-order (rowid) compose answers with
// the last-DELIVERED name, which differs between interleavings by
// construction — asserted non-degenerate at runtime.
func TestW5dReversalOrderIndependentAcrossStreams(t *testing.T) {
	const nameA = "Order Site A Ltd" // site-a's assertion — record-order LOSER
	const nameB = "Order Site B Ltd" // site-b's assertion — record-order WINNER, pinned

	origin := w3NewStore(t)
	x, y, roster := w5dCompanyPair(t, origin, "Order X Ltd", "Order Y Ltd")

	// site-a's stream continues: name, merge, un-merge (per-stream order is
	// fixed; only cross-stream interleaving is permutable).
	w5dAmendLocal(t, origin, "site-a", 1, w3Base+1*w3Hour,
		CompanyAmendment{CompanyID: x, Name: w3Str(nameA)})
	w5dAmendLocal(t, origin, "site-a", 1, w3Base+2*w3Hour,
		CompanyAmendment{CompanyID: x, SameAs: w3Str(y)})
	w5dAmendLocal(t, origin, "site-a", 1, w3Base+3*w3Hour,
		CompanyAmendment{CompanyID: x, Clear: []string{w5dFactSameAs}})
	siteA := w3Events(t, origin, "site-a", 1)

	// site-b's stream: one name amendment authored on a real edge at a
	// primed scratch post, with an EARLIER clock than site-a's (so a clock
	// compose also disagrees with the pinned winner).
	siteB := w3AuthorRemote(t, roster, "site-b", 1, w3Base+30*60_000, func(st *store.Store) {
		if _, err := AmendCompany(st, "site-b", 1, CompanyAmendment{CompanyID: x, Name: w3Str(nameB)}, 0, "default"); err != nil {
			t.Fatalf("site-b fixture amendment refused: %v", err)
		}
	})
	if len(siteB) != 1 {
		t.Fatalf("W5d-7 fixture defect: site-b minted %d events, want 1", len(siteB))
	}

	// Interleavings: per-stream order preserved, cross-stream order varied.
	interleavings := [][]store.Envelope{
		append(append([]store.Envelope{}, siteA...), siteB...),
		append(append([]store.Envelope{}, siteB...), siteA...),
		func() []store.Envelope { // site-b injected mid-stream, after the roster
			out := append([]store.Envelope{}, siteA[:len(roster)]...)
			out = append(out, siteB...)
			return append(out, siteA[len(roster):]...)
		}(),
	}

	// Runtime non-degeneracy (standing rule 3), recomputed from the fixture
	// data: the arrival-order (rowid) rival's winner is the LAST
	// name-carrying event of each interleaving. If that rival agreed with
	// the pinned record-order winner on EVERY interleaving, W3-m10 would
	// pass this test and the fixture would be degenerate.
	lastName := func(order []store.Envelope) string {
		winner := ""
		clockWinner := ""
		var clockMax int64 = -1 << 62
		for _, e := range order {
			var pl struct {
				Type string  `json:"type"`
				Name *string `json:"name"`
			}
			if err := json.Unmarshal(e.Payload, &pl); err == nil && pl.Type == w5dEvtCompanyAmended && pl.Name != nil {
				winner = *pl.Name
				if e.AuthoredAt.WallMS > clockMax {
					clockMax, clockWinner = e.AuthoredAt.WallMS, *pl.Name
				}
			}
		}
		if clockWinner == nameB {
			t.Fatalf("W5d-7 FIXTURE DEFECT: DEGENERATE — the clock-order rival agrees with the pinned record-order winner %q (standing rule 3)", nameB)
		}
		return winner
	}
	arrivalDisagrees := false
	for _, order := range interleavings {
		if lastName(order) != nameB {
			arrivalDisagrees = true
		}
	}
	if !arrivalDisagrees {
		t.Fatalf("W5d-7 FIXTURE DEFECT: DEGENERATE — the arrival-order rival (W3-m10) agrees with the pinned winner %q on EVERY interleaving; the fixture cannot kill its own named mutant (standing rule 3)", nameB)
	}

	var wantDump, wantComposed []byte
	for i, order := range interleavings {
		st := w3NewStore(t)
		w3ApplyAll(t, st, order)

		c := w5dCompany(t, st, x)
		got, ok := w5dCompanyName(c)
		if !ok || got != nameB {
			t.Fatalf("W5d-7 (W3-m10/W3-m1): interleaving %d composed name = %q (present=%v), want the PINNED record-order winner %q (site-b beats site-a regardless of delivery and clock)", i, got, ok, nameB)
		}
		if c.SameAs != nil {
			t.Fatalf("W5d-7: interleaving %d composed same_as = %q, want absent (the un-merge is latest in site-a's own stream)", i, *c.SameAs)
		}

		dump, err := st.DumpProjections()
		if err != nil {
			t.Fatal(err)
		}
		composed, err := json.Marshal(w5dCompanies(t, st))
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			wantDump, wantComposed = dump, composed
			continue
		}
		if string(dump) != string(wantDump) {
			t.Fatalf("W5d-7 (W3-m10): projection/dump state differs between delivery interleavings 0 and %d — composition must be inherited from W5d-16's ascending-key compose, never engineered per arrival", i)
		}
		if string(composed) != string(wantComposed) {
			t.Fatalf("W5d-7 (W3-m10): composed Companies() differs between delivery interleavings 0 and %d", i)
		}
	}
}
