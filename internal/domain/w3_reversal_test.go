// w3_reversal_test.go — W3-5: the merge reversal round-trip (merge →
// Clear ["same_as"] → effective-value equality with the never-merged
// state, NEVER erasure), and order-independence across streams under
// permuted delivery (the house TestReferenceFoldsReplayDeterministically
// pattern [MEASURED — named in alias.go]).
// design/WINDOW-BUILD-PLAN-v1.md §3 W3, criteria LOCKED 2026-08-27.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD — that IS the clean-tree red.
//
// KILLER MUTANT: m10 — compose same_as/amendments by arrival order (rowid).
// This fixture is m10's SOLE named killer (TE-3 struck it from W3-1: a
// value-scramble over a fixed stream cannot produce an
// arrival-contradicts-key order; only permuted DELIVERY can).
package domain

import (
	"encoding/json"
	"testing"

	"sitecontrol/internal/store"
)

// TestW3ReversalRoundTripEqualsNeverMerged — W3-5's first half: after
// merge X→Y then Clear ["same_as"], every read returns EFFECTIVE-VALUE
// equality with the never-merged state — equality of effective values,
// never erasure: both events remain in the log (append-only; VF §6
// "superseded, never merely absent").
func TestW3ReversalRoundTripEqualsNeverMerged(t *testing.T) {
	merged := w3NewStore(t)
	x, y, roster := w3RosterPair(t, merged, "Round Trip X", "Round Trip Y")
	mergeEnv := w3AmendLocal(t, merged, "site-a", 1, w3Base+1*w3Hour,
		PersonAmendment{PersonID: x, SameAs: w3Str(y)})
	clearEnv := w3AmendLocal(t, merged, "site-a", 1, w3Base+2*w3Hour,
		PersonAmendment{PersonID: x, Clear: []string{w3FactSameAs}})

	never := w3NewStore(t)
	w3ApplyAll(t, never, roster)

	// Effective-value equality across the reader set.
	for name, st := range map[string]*store.Store{"merged-then-cleared": merged, "never-merged": never} {
		p := w3Person(t, st, x)
		if p.SameAs != nil {
			t.Fatalf("W3-5: composed same_as on %s store = %q, want absent — the reversal did not take effect (or Clear composed as empty-string, m7's shape)", name, *p.SameAs)
		}
		m := w3MergeMap(t, st)
		if got := m.Resolve(x); got != x {
			t.Fatalf("W3-5: Resolve(%s) on %s store = %s, want the un-merged identity %s", x, name, got, x)
		}
	}
	mp, _ := json.Marshal(mustPersons(t, merged))
	np, _ := json.Marshal(mustPersons(t, never))
	if string(mp) != string(np) {
		t.Fatalf("W3-5: composed Persons() after merge+un-merge differs from the never-merged state —\nmerged:  %s\nnever:   %s", mp, np)
	}

	// NEVER erasure: the merge and un-merge envelopes remain in the log.
	found := map[store.Key]bool{}
	for _, e := range w3Events(t, merged, "site-a", 1) {
		found[e.Key()] = true
	}
	if !found[mergeEnv.Key()] || !found[clearEnv.Key()] {
		t.Fatalf("W3-5: merge (%v) or un-merge (%v) envelope missing from the log — reversal must be equality of EFFECTIVE VALUES, never erasure (append-only)", mergeEnv.Key(), clearEnv.Key())
	}
}

func mustPersons(t *testing.T, st *store.Store) []Person {
	t.Helper()
	ps, err := Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	return ps
}

// TestW3ReversalOrderIndependentAcrossStreams — W3-5's second half:
// permuted delivery orders of the SAME event set produce byte-identical
// projection/dump state AND identical composed reads, with the contested
// phone pinned to the record-order winner (site-b's) under every
// interleaving. Kills m10: an arrival-order (rowid) compose gives the
// last-delivered phone, which differs between interleavings by
// construction — asserted non-degenerate at runtime.
func TestW3ReversalOrderIndependentAcrossStreams(t *testing.T) {
	const phoneA = "555-0311" // site-a's phone assertion (record-order LOSER)
	const phoneB = "555-0322" // site-b's phone assertion (record-order WINNER, pinned)

	origin := w3NewStore(t)
	x, y, roster := w3RosterPair(t, origin, "Order X", "Order Y")

	// site-a's stream continues: phone, merge, un-merge (per-stream order
	// is fixed; only cross-stream interleaving is permutable).
	w3AmendLocal(t, origin, "site-a", 1, w3Base+1*w3Hour,
		PersonAmendment{PersonID: x, Phone: w3Str(phoneA)})
	w3AmendLocal(t, origin, "site-a", 1, w3Base+2*w3Hour,
		PersonAmendment{PersonID: x, SameAs: w3Str(y)})
	w3AmendLocal(t, origin, "site-a", 1, w3Base+3*w3Hour,
		PersonAmendment{PersonID: x, Clear: []string{w3FactSameAs}})
	siteA := w3Events(t, origin, "site-a", 1)

	// site-b's stream: one phone amendment, authored on a real edge at a
	// primed scratch post, EARLIER clock than site-a's phone (so a clock
	// compose also disagrees with the pinned winner).
	siteB := w3AuthorRemote(t, roster, "site-b", 1, w3Base+30*60_000, func(st *store.Store) {
		if _, err := AmendPerson(st, "site-b", 1, PersonAmendment{PersonID: x, Phone: w3Str(phoneB)}, 0, "default"); err != nil {
			t.Fatalf("site-b fixture amendment refused: %v", err)
		}
	})
	if len(siteB) != 1 {
		t.Fatalf("fixture defect: site-b minted %d events, want 1", len(siteB))
	}

	// Interleavings: per-stream order preserved, cross-stream order varied.
	// Interleaving 0 delivers site-b LAST; interleaving 1 delivers site-b
	// FIRST — an arrival-order compose cannot answer both identically.
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
	// data itself: the arrival-order (rowid) rival's winner is the LAST
	// phone-carrying event of each interleaving; if that rival agreed with
	// the pinned record-order winner on EVERY interleaving, m10 would pass
	// this test and the fixture would be degenerate. The clock rival's
	// winner is the max-AuthoredAt phone assertion.
	lastPhone := func(order []store.Envelope) string {
		winner := ""
		var clockWinner string
		var clockMax int64 = -1 << 62
		for _, e := range order {
			var pl struct {
				Type  string  `json:"type"`
				Phone *string `json:"phone"`
			}
			if err := json.Unmarshal(e.Payload, &pl); err == nil && pl.Type == w3EvtPersonAmended && pl.Phone != nil {
				winner = *pl.Phone
				if e.AuthoredAt.WallMS > clockMax {
					clockMax, clockWinner = e.AuthoredAt.WallMS, *pl.Phone
				}
			}
		}
		if clockWinner == phoneB {
			t.Fatalf("W3-5 FIXTURE DEFECT: DEGENERATE — the clock-order rival agrees with the pinned record-order winner %q (standing rule 3)", phoneB)
		}
		return winner
	}
	arrivalDisagrees := false
	for _, order := range interleavings {
		if lastPhone(order) != phoneB {
			arrivalDisagrees = true
		}
	}
	if !arrivalDisagrees {
		t.Fatalf("W3-5 FIXTURE DEFECT: DEGENERATE — the arrival-order rival (m10) agrees with the pinned winner %q on EVERY interleaving; the fixture cannot kill its own named mutant (standing rule 3)", phoneB)
	}

	var wantDump []byte
	var wantComposed []byte
	for i, order := range interleavings {
		st := w3NewStore(t)
		w3ApplyAll(t, st, order)

		p := w3Person(t, st, x)
		if p.Phone == nil || *p.Phone != phoneB {
			t.Fatalf("W3-5 (m10/m1): interleaving %d composed phone = %v, want the PINNED record-order winner %q (site-b beats site-a regardless of delivery and clock)", i, p.Phone, phoneB)
		}
		if p.SameAs != nil {
			t.Fatalf("W3-5: interleaving %d composed same_as = %q, want absent (the un-merge is latest in site-a's own stream)", i, *p.SameAs)
		}

		dump, err := st.DumpProjections()
		if err != nil {
			t.Fatal(err)
		}
		composed, err := json.Marshal(mustPersons(t, st))
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			wantDump, wantComposed = dump, composed
			continue
		}
		if string(dump) != string(wantDump) {
			t.Fatalf("W3-5 (m10): projection/dump state differs between delivery interleavings 0 and %d — composition must be inherited from W3-1's causal-then-ascending-key compose ⟦re-locked D-92; was: ascending-key⟧, never engineered per arrival", i)
		}
		if string(composed) != string(wantComposed) {
			t.Fatalf("W3-5 (m10): composed Persons() differs between delivery interleavings 0 and %d", i)
		}
	}
}
