// d92_step3_tester_test.go — the tester's pass over step 3 of the 10 → 11
// increment (reports/witness/TESTER-step3.md): the three gaps the builder's
// own reports named as unclosed (STEP3-CLONE.md §7, STEP3-BLINDSPOTS.md §7),
// each watched RED under a planted mutant before it was trusted GREEN.
//
//  1. resolveContest rule 1 says a contestant is dominated by ANOTHER
//     contestant's `seen`. The `s != k` guard is the only line that makes
//     "another" true, and no honest edge can mint a self-reference (a key
//     is not known before Author), so nothing in the suite reached it. A
//     wire-formed row that names ITSELF must not be demoted by that guard's
//     absence — pinned on the correction carrier, the one that is not a
//     mechanical clone.
//  2. seenRefsOf's ordering and de-duplication ("two authors who saw the
//     same state stamp byte-identical lists") had only a ~50%-flaky killer:
//     with two heads a dropped sort survives half the runs. Pinned
//     white-box over twelve keys given twice, sixty-four shuffles.
//  3. The trip carrier's MEMBER facts (`occupant/<id>`) had no concurrent
//     clock witness — the blind-spot pass's trip witness is on the scalar
//     driver, and composeOccupants is a separate call site of the rule.
//     Closed in d92s3RunClockCase's exact shape.
//
// KILLER MUTANTS, each PRODUCED red once against this tree
// (reports/witness/TESTER-step3.md §6): m-SELF (the self-reference guard
// removed from resolveContest), m-SORT (seenRefsOf's sort dropped), m-DEDUP
// (seenRefsOf's set dropped), m-FA1-goside (a Go-side event_log clock query
// reaching the tiebreak — the tester's literal FA1 shape).
package domain

import (
	"encoding/json"
	"math/rand"
	"sort"
	"testing"

	"sitecontrol/internal/store"
)

// d92t3WireCorrection hand-builds an occurred_at_corrected envelope as a
// divergent peer might ship it: the five pinned keys plus a `seen` list of
// the caller's choosing — the shape no honest edge mints.
func d92t3WireCorrection(t *testing.T, k store.Key, authoredMS int64, target store.Key, correctedMS int64, recordedBy string, seen []store.Key) store.Envelope {
	t.Helper()
	env := w4WireCorrection(t, k.SiteID, k.NodeEpoch, k.Seq, authoredMS, store.ClockSynced, target, correctedMS, recordedBy)
	var raw map[string]any
	if err := json.Unmarshal(env.Payload, &raw); err != nil {
		t.Fatal(err)
	}
	refs := make([]SeenRef, 0, len(seen))
	for _, s := range seen {
		refs = append(refs, seenRefOf(s))
	}
	raw["seen"] = refs
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	env.Payload = b
	return env
}

// TestD92ASelfNamingWireCorrectionIsNotDominatedByItself — rule 1's word
// "another": a wire-formed correction whose `seen` names ITS OWN key stays
// a survivor. Against an honest local correction at the LESSER key (which
// saw nothing), the self-naming row is the greater key among two survivors
// and wins the tiebreak; a rule that let a row dominate itself would hand
// the win to the local key instead. Asserted at the local post and at a
// post receiving the wire row first; the self-reference is asserted to
// have SURVIVED the codec into the side table, so a NULLed list cannot make
// this pass for the wrong reason.
func TestD92ASelfNamingWireCorrectionIsNotDominatedByItself(t *testing.T) {
	const (
		localMS = w4OriginalMS + 2*w3Hour
		wireMS  = w4OriginalMS + 1*w3Hour
	)
	st := w3NewStore(t)
	target := w4Target(t, st, "site-a", 1, "Self Reference Target")
	primer := w3Events(t, st, "site-a", 1)
	local := w4CorrectLocal(t, st, "site-a", 1, w3Base+3*w3Hour, target.Key(), localMS, "Ida Aputi")
	self := store.Key{SiteID: "site-b", NodeEpoch: 1, Seq: 1}
	wire := d92t3WireCorrection(t, self, w3Base+1*w3Hour, target.Key(), wireMS, "Pat Bell", []store.Key{self})
	w3Apply(t, st, wire)

	// Fixture guards (rule 6): the local correction saw nothing; the local
	// key is the lesser one, so a demoted wire row would visibly hand the
	// win to it; and the self-reference reads back from the side table.
	if seen := d92SeenOf(t, local); len(seen) != 0 {
		t.Fatalf("fixture defect: the local correction carries seen=%v, want none (the first correction of the event)", seen)
	}
	if !local.Key().Less(self) {
		t.Fatalf("fixture defect: the local key %v is not less than the wire row's %v", local.Key(), self)
	}
	rows, err := correctionsTargeting(st, target.Key())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.Key == self {
			found = true
			if !w3KeysEqual(r.Seen, []store.Key{self}) {
				t.Fatalf("fixture defect: the wire row's self-reference reads back as %v, want [%v] — the codec dropped it, and this witness would then pass without reaching the guard", r.Seen, self)
			}
		}
	}
	if !found {
		t.Fatalf("no occurred_at_correction row at %v", self)
	}

	posts := []struct {
		name string
		post *store.Store
	}{{"the local post", st}}
	third := w3NewStore(t)
	w3ApplyAll(t, third, primer)
	w3Apply(t, third, wire)
	w3Apply(t, third, local)
	posts = append(posts, struct {
		name string
		post *store.Store
	}{"a post receiving the wire row first", third})
	for _, p := range posts {
		eff, chain, err := w4Resolve(t, p.post, target.Key())
		if err != nil {
			t.Fatalf("ResolveOccurredAt at %s: %v", p.name, err)
		}
		if eff.Winner != self || eff.InstantUTCms != wireMS {
			t.Fatalf("D-92 rule 1 (m-SELF) at %s: resolved (%d, %v), want (%d, %v) — a contestant is dominated only by ANOTHER contestant's `seen`; a row that names itself stays a survivor and, as the greater key, wins the tiebreak. (%d, %v) is what a rule that lets a row dominate itself resolves", p.name, eff.InstantUTCms, eff.Winner, wireMS, self, localMS, local.Key())
		}
		w4RequireChainStatuses(t, chain, self)
		if out := w4OccurredAtFact(t, p.post, local.Key()); out.Status != Superseded || out.Winner != self {
			t.Fatalf("D-92 rule 1 (m-SELF) at %s: Outcome(local) = (%v, %v), want (Superseded, %v)", p.name, out.Status, out.Winner, self)
		}
	}
}

// TestD92SeenRefsOfIsAscendingAndDeduplicatedRegardlessOfInputOrder — the
// stamp's byte-identity: seenRefsOf renders any multiset of keys as the
// ascending, de-duplicated list, whatever order it was handed. Twelve keys
// across three sites and two epochs, each given twice, sixty-four
// deterministic shuffles; and nothing-to-name renders as nil (the
// `omitempty` half of the wire convention).
func TestD92SeenRefsOfIsAscendingAndDeduplicatedRegardlessOfInputOrder(t *testing.T) {
	var keys []store.Key
	for _, site := range []string{"site-a", "site-b", "site-c"} {
		for epoch := int64(1); epoch <= 2; epoch++ {
			for seq := int64(1); seq <= 2; seq++ {
				keys = append(keys, store.Key{SiteID: site, NodeEpoch: epoch, Seq: seq})
			}
		}
	}
	want := append([]store.Key(nil), keys...)
	sort.Slice(want, func(i, j int) bool { return want[i].Less(want[j]) })
	rng := rand.New(rand.NewSource(0x5eed))
	for trial := 0; trial < 64; trial++ {
		in := append(append([]store.Key(nil), keys...), keys...) // every key twice
		rng.Shuffle(len(in), func(i, j int) { in[i], in[j] = in[j], in[i] })
		out := seenRefsOf(in)
		if len(out) != len(want) {
			t.Fatalf("D-92 (m-DEDUP): trial %d: %d refs from %d distinct keys each given twice, want %d — the stamp must de-duplicate", trial, len(out), len(want), len(want))
		}
		for i := range out {
			if out[i].key() != want[i] {
				t.Fatalf("D-92 (m-SORT): trial %d, position %d = %v, want %v — two authors who saw the same state must stamp byte-identical lists, so the order is ascending by key, never the map's", trial, i, out[i].key(), want[i])
			}
		}
	}
	if got := seenRefsOf(nil); got != nil {
		t.Fatalf("seenRefsOf(nil) = %v, want nil so `omitempty` drops the key", got)
	}
	if got := seenRefsOf([]store.Key{}); got != nil {
		t.Fatalf("seenRefsOf(empty) = %v, want nil so `omitempty` drops the key", got)
	}
}

// TestW9aConcurrentTripMemberPairIsClockFreeUnderEitherTimeBasis — the trip
// carrier's MEMBER fact under the blind-spot pass's clock shape: over a
// shared removal p0 of `occupant/p-brand`, site-a re-adds him (LESSER key,
// LATER clock) while site-b, cut off, removes him again (GREATER key,
// EARLIER clock). The tiebreak says not aboard; a tiebreak reading the
// greatest clock says aboard; the swapped bases must change nothing.
func TestW9aConcurrentTripMemberPairIsClockFreeUnderEitherTimeBasis(t *testing.T) {
	const member = "p-brand"
	st := w9aNewStore(t)
	trip, _ := w9aCausalWorld(t, st, "TRK-K3")
	p0 := w9aAmendTripLocal(t, st, w3Base, TripAmendment{TripID: trip.TripID, OccupantsRemove: []string{member}})
	held := w3Events(t, st, "site-a", 1)
	a1 := w9aAmendTripLocal(t, st, w3Base+3*w3Hour, TripAmendment{TripID: trip.TripID, OccupantsAdd: []string{member}})
	b1 := d92Remote(t, held, "site-b", w3Base+1*w3Hour, func(rs *store.Store) {
		if _, err := AmendTrip(rs, "site-b", 1, TripAmendment{TripID: trip.TripID, OccupantsRemove: []string{member}}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	w3Apply(t, st, b1)
	d92s3RunClockCase(t, st, d92s3ClockCase{
		carrier: "trip member", newStore: w9aNewStore, held: held, a1: a1, b1: b1,
		shared: []store.Key{p0.Key()}, fact: factOccupantPrefix + member, wantB: "not aboard", wantA: "aboard",
		composed: func(t *testing.T, st *store.Store) string {
			if w9aContainsID(w9aComposed(t, st, trip.TripID).OccupantIDs, member) {
				return "aboard"
			}
			return "not aboard"
		},
	})
}
