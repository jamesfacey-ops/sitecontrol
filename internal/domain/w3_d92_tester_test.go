// w3_d92_tester_test.go — tester-go's INDEPENDENT witnesses for schema-11
// step 2 (the D-92 causal `seen` reference on PersonAmended /
// person_amendment; `pad_oriented` struck). Written against 911cc0a by an
// agent that did not build it (reports/witness/TESTER-step2.md).
//
// What this file adds over w3_causal_compose_test.go:
//
//   - THE FOUR DOMINANCE CASES AS ONE TABLE — (a) A saw B, (b) B saw A,
//     (c) neither saw the other, (d) both name each other (wire-formed) —
//     each viewed from every delivery permutation (post A, post B, a third
//     post either way) AND under four time-basis variants in which ONLY a
//     timestamp changes. If any ordering outcome moves when only a clock
//     moves, the clock has crept back in (D-90/D-92: THE CLOCK ORDERS
//     NOTHING). The clock rival is asserted to name the LOSER in every case,
//     so a compose that read a clock could not pass by coincidence.
//   - DELIVERY-ORDER INDEPENDENCE over a five-amendment, two-person record
//     set — concurrent pair, a correction over both heads, a merge and an
//     un-merge made in knowledge — under EVERY permutation of delivery:
//     composed bytes, the projection dump and every Outcome verdict are
//     identical. This is the property the whole increment exists to give.
//   - THE MERGE-GROUP HALF OF THE STAMP — personSeen unions the heads of the
//     target's own contest with the heads of the merged GROUP's contest
//     (Outcome's scope). None of the eight step-2 witnesses reaches that
//     union; a clone that drops it composes the same bytes and lies in
//     Outcome. The composed-state assertion here is deliberately labelled
//     BLIND; the Outcome assertion is the instrument.
//   - FOLD TOLERANCE FOR A STALE `pad_oriented` KEY on a replicated
//     person_created / person_amended envelope, through Apply and
//     ApplyBatch — the U4 stall shape: if the fold errors, the stream stops.
//
// Every pinned literal is guarded by standing rule 3: the named rivals are
// recomputed from the read-back envelopes and asserted DIFFERENT from the
// certified answer wherever the rule and the rival can disagree.
package domain

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// --- helpers ------------------------------------------------------------

// d92Perms returns every permutation of 0..n-1 (n ≤ 6 here).
func d92Perms(n int) [][]int {
	var out [][]int
	var rec func(cur []int, used []bool)
	rec = func(cur []int, used []bool) {
		if len(cur) == n {
			out = append(out, append([]int{}, cur...))
			return
		}
		for i := 0; i < n; i++ {
			if used[i] {
				continue
			}
			used[i] = true
			rec(append(cur, i), used)
			used[i] = false
		}
	}
	rec(nil, make([]bool, n))
	return out
}

// d92Interleavings returns every delivery order of evs that keeps each
// (site_id, node_epoch) stream in ascending seq — the orders replication
// can actually deliver (a stream is contiguous; only the interleaving
// ACROSS streams is free, W3-5). A post cannot apply seq 4 before seq 3 of
// the same stream: that is a GapError at the store, not a compose case.
func d92Interleavings(evs []store.Envelope) [][]store.Envelope {
	var out [][]store.Envelope
	for _, perm := range d92Perms(len(evs)) {
		order := make([]store.Envelope, 0, len(perm))
		for _, j := range perm {
			order = append(order, evs[j])
		}
		ok := true
		last := map[[2]any]int64{}
		for _, e := range order {
			s := [2]any{e.SiteID, e.NodeEpoch}
			if prev, seen := last[s]; seen && e.Seq <= prev {
				ok = false
				break
			}
			last[s] = e.Seq
		}
		if ok {
			out = append(out, order)
		}
	}
	return out
}

// d92Verdicts renders every Outcome verdict for the given keys as ONE
// string in key order — the cross-view / cross-clock comparison unit.
func d92Verdicts(t *testing.T, st *store.Store, keys []store.Key) string {
	t.Helper()
	sorted := append([]store.Key{}, keys...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Less(sorted[j]) })
	var b strings.Builder
	for _, k := range sorted {
		outs, err := Outcome(st, k)
		if err != nil {
			t.Fatalf("Outcome(%v): %v", k, err)
		}
		sort.Slice(outs, func(i, j int) bool { return outs[i].Fact < outs[j].Fact })
		for _, o := range outs {
			fmt.Fprintf(&b, "%v %s %v winner=%v\n", k, o.Fact, o.Status, o.Winner)
		}
	}
	return b.String()
}

// d92Keys is the key list of a slice of envelopes.
func d92Keys(evs []store.Envelope) []store.Key {
	out := make([]store.Key, 0, len(evs))
	for _, e := range evs {
		out = append(out, e.Key())
	}
	return out
}

// d92Fresh opens a post holding roster then evs in the given order.
func d92Fresh(t *testing.T, roster, evs []store.Envelope) *store.Store {
	t.Helper()
	st := w3NewStore(t)
	w3ApplyAll(t, st, roster)
	w3ApplyAll(t, st, evs)
	return st
}

// d92RawSeen returns the raw JSON of the `seen` key on an envelope's own
// payload bytes ("" when the key is absent).
func d92RawSeen(t *testing.T, env store.Envelope) string {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(env.Payload, &raw); err != nil {
		t.Fatal(err)
	}
	return string(raw["seen"])
}

// d92RemotePhone authors one phone amendment as siteID over primer and
// returns the single envelope it minted.
func d92RemotePhone(t *testing.T, primer []store.Envelope, siteID string, clockMS int64, personID, phone string) store.Envelope {
	t.Helper()
	evs := w3AuthorRemote(t, primer, siteID, 1, clockMS, func(rs *store.Store) {
		if _, err := AmendPerson(rs, siteID, 1, PersonAmendment{PersonID: personID, Phone: w3Str(phone)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	if len(evs) != 1 {
		t.Fatalf("fixture defect: %s minted %d events, want 1", siteID, len(evs))
	}
	return evs[0]
}

// d92Case is one of the four dominance cases.
type d92Case struct {
	name      string
	personID  string
	roster    []store.Envelope
	amend     []store.Envelope // the contested amendments, in minted order
	wantPhone string
	winner    store.Key
	losers    []store.Key
	// rivals the certified answer must differ from on THIS fixture (a rival
	// that legitimately agrees on a case — fixed order on the tiebreak — is
	// omitted, and the omission is stated in the case's comment).
	rivals func(t *testing.T, evs []store.Envelope) map[string]string
}

// d92ClockRival is "chronological max(AuthoredAt)" over the amendments as
// READ BACK — D-90's refused rule, m1.
func d92ClockRival(t *testing.T, evs []store.Envelope) string {
	t.Helper()
	best := evs[0]
	for _, e := range evs[1:] {
		if e.AuthoredAt.WallMS > best.AuthoredAt.WallMS {
			best = e
		}
	}
	return w3PhoneOf(t, best)
}

// d92GreatestKeyRival is D-28 Part B's fixed order over the amendments.
func d92GreatestKeyRival(t *testing.T, evs []store.Envelope) string {
	t.Helper()
	best := evs[0]
	for _, e := range evs[1:] {
		if best.Key().Less(e.Key()) {
			best = e
		}
	}
	return w3PhoneOf(t, best)
}

// d92LeastKeyRival is an inverted tiebreak.
func d92LeastKeyRival(t *testing.T, evs []store.Envelope) string {
	t.Helper()
	best := evs[0]
	for _, e := range evs[1:] {
		if e.Key().Less(best.Key()) {
			best = e
		}
	}
	return w3PhoneOf(t, best)
}

// d92BuildCases mints the four cases through REAL authoring edges where an
// edge can mint them ((a)–(c)) and by wire where none can ((d)).
func d92BuildCases(t *testing.T) []d92Case {
	t.Helper()
	var cases []d92Case

	// (a) A saw B → A wins. site-b (GREATER key, LATER clock) asserts over
	// the bare roster; site-a receives it and corrects. The LESSER key wins
	// because its author saw. Fixed order and the clock both name B.
	{
		st := w3NewStore(t)
		x, _, roster := w3RosterPair(t, st, "Case A X", "Case A Y")
		b := d92RemotePhone(t, roster, "site-b", w3Base+3*w3Hour, x, "555-0811")
		w3ApplyAll(t, st, []store.Envelope{b})
		a := w3AmendLocal(t, st, "site-a", 1, w3Base+1*w3Hour, PersonAmendment{PersonID: x, Phone: w3Str("555-0812")})
		if got := w3SeenOf(t, a); !w3KeysEqual(got, []store.Key{b.Key()}) {
			t.Fatalf("case (a) fixture: A names %v, want [%v]", got, b.Key())
		}
		if got := w3SeenOf(t, b); len(got) != 0 {
			t.Fatalf("case (a) fixture: B names %v, want nothing", got)
		}
		cases = append(cases, d92Case{
			name: "(a) A saw B — A wins", personID: x, roster: roster,
			amend: []store.Envelope{b, a}, wantPhone: "555-0812", winner: a.Key(), losers: []store.Key{b.Key()},
			rivals: func(t *testing.T, evs []store.Envelope) map[string]string {
				return map[string]string{
					"fixed order — greatest key [D-28 Part B, reversed]": d92GreatestKeyRival(t, evs),
					"chronological max(AuthoredAt) [m1]":                 d92ClockRival(t, evs),
				}
			},
		})
	}

	// (b) B saw A → B wins. The mirror: site-a (LESSER key, LATER clock)
	// asserts first; site-b receives it and corrects. B wins because it saw
	// — fixed order happens to agree here (it is the mirror, and says so);
	// the clock and the inverted tiebreak both name A.
	{
		st := w3NewStore(t)
		x, _, roster := w3RosterPair(t, st, "Case B X", "Case B Y")
		a := w3AmendLocal(t, st, "site-a", 1, w3Base+3*w3Hour, PersonAmendment{PersonID: x, Phone: w3Str("555-0821")})
		held := w3Events(t, st, "site-a", 1)
		b := d92RemotePhone(t, held, "site-b", w3Base+1*w3Hour, x, "555-0822")
		w3ApplyAll(t, st, []store.Envelope{b})
		if got := w3SeenOf(t, b); !w3KeysEqual(got, []store.Key{a.Key()}) {
			t.Fatalf("case (b) fixture: B names %v, want [%v]", got, a.Key())
		}
		if got := w3SeenOf(t, a); len(got) != 0 {
			t.Fatalf("case (b) fixture: A names %v, want nothing", got)
		}
		cases = append(cases, d92Case{
			name: "(b) B saw A — B wins", personID: x, roster: roster,
			amend: []store.Envelope{a, b}, wantPhone: "555-0822", winner: b.Key(), losers: []store.Key{a.Key()},
			rivals: func(t *testing.T, evs []store.Envelope) map[string]string {
				return map[string]string{
					"chronological max(AuthoredAt) [m1]":  d92ClockRival(t, evs),
					"lesser key wins (inverted tiebreak)": d92LeastKeyRival(t, evs),
				}
			},
		})
	}

	// (c) Neither saw the other → the tiebreak, and ONLY here. Both posts
	// hold p0; the link drops; each corrects p0 without seeing the other.
	// site-a's correction carries the LATER clock, so the clock names A;
	// the certified answer is the greater site_id, B. Fixed order agrees by
	// construction — this IS its one surviving case — and is omitted.
	{
		st := w3NewStore(t)
		x, _, _ := w3RosterPair(t, st, "Case C X", "Case C Y")
		p0 := w3AmendLocal(t, st, "site-a", 1, w3Base, PersonAmendment{PersonID: x, Phone: w3Str("555-0830")})
		held := w3Events(t, st, "site-a", 1)
		a := w3AmendLocal(t, st, "site-a", 1, w3Base+3*w3Hour, PersonAmendment{PersonID: x, Phone: w3Str("555-0831")})
		b := d92RemotePhone(t, held, "site-b", w3Base+1*w3Hour, x, "555-0832")
		w3ApplyAll(t, st, []store.Envelope{b})
		for name, e := range map[string]store.Envelope{"A": a, "B": b} {
			if got := w3SeenOf(t, e); !w3KeysEqual(got, []store.Key{p0.Key()}) {
				t.Fatalf("case (c) fixture: %s names %v, want [%v] — each saw p0 and NOT the other", name, got, p0.Key())
			}
		}
		// roster here is the site-a stream WITHOUT p0; p0 is a contestant.
		roster := held[:len(held)-1]
		cases = append(cases, d92Case{
			name: "(c) neither saw the other — tiebreak", personID: x, roster: roster,
			amend: []store.Envelope{p0, a, b}, wantPhone: "555-0832", winner: b.Key(), losers: []store.Key{p0.Key(), a.Key()},
			rivals: func(t *testing.T, evs []store.Envelope) map[string]string {
				return map[string]string{
					"chronological max(AuthoredAt) [m1]":  d92ClockRival(t, evs),
					"lesser key wins (inverted tiebreak)": d92LeastKeyRival(t, evs),
				}
			},
		})
	}

	// (d) Both name each other. No honest edge mints this (an author cannot
	// have seen a row that names its own not-yet-existing key), so it is
	// wire-formed. Every member is dominated; rule 3 falls to the greatest
	// key so the read stays total. The clock (B later) names the loser.
	{
		origin := w3NewStore(t)
		x, _, roster := w3RosterPair(t, origin, "Case D X", "Case D Y")
		refB := map[string]any{"site_id": "site-b", "node_epoch": 1, "seq": 1}
		refC := map[string]any{"site_id": "site-c", "node_epoch": 1, "seq": 1}
		eb := w3WireAmendment(t, "site-b", 1, 1, w3Base+2*w3Hour, x, map[string]any{"phone": "555-0841", "seen": []any{refC}})
		ec := w3WireAmendment(t, "site-c", 1, 1, w3Base+1*w3Hour, x, map[string]any{"phone": "555-0842", "seen": []any{refB}})
		cases = append(cases, d92Case{
			name: "(d) both saw each other — wire-formed cycle", personID: x, roster: roster,
			amend: []store.Envelope{eb, ec}, wantPhone: "555-0842", winner: ec.Key(), losers: []store.Key{eb.Key()},
			rivals: func(t *testing.T, evs []store.Envelope) map[string]string {
				return map[string]string{
					"chronological max(AuthoredAt) [m1]":  d92ClockRival(t, evs),
					"lesser key wins (inverted tiebreak)": d92LeastKeyRival(t, evs),
				}
			},
		})
	}
	return cases
}

// d92AssertCase checks one post's view of a case: the composed phone, the
// winner Effective, every loser Superseded BY the winner.
func d92AssertCase(t *testing.T, c d92Case, st *store.Store, view string) {
	t.Helper()
	if p := w3Person(t, st, c.personID); p.Phone == nil || *p.Phone != c.wantPhone {
		t.Fatalf("%s / %s: composed phone = %v, want %q", c.name, view, p.Phone, c.wantPhone)
	}
	if got := w3Fact(t, st, c.winner, w3FactPhone); got.Status != Effective || got.Winner != c.winner {
		t.Fatalf("%s / %s: Outcome(winner %v) = (%v, %v), want (Effective, itself)", c.name, view, c.winner, got.Status, got.Winner)
	}
	for _, l := range c.losers {
		if got := w3Fact(t, st, l, w3FactPhone); got.Status != Superseded || got.Winner != c.winner {
			t.Fatalf("%s / %s: Outcome(loser %v) = (%v, %v), want (Superseded, %v)", c.name, view, l, got.Status, got.Winner, c.winner)
		}
	}
}

// =======================================================================
// The four cases: every delivery permutation, every clock variant
// =======================================================================

// TestD92FourDominanceCasesAreClockFreeAtEveryPost — D-90/D-92: the clock
// orders NOTHING; site_id breaks ties ONLY between genuinely concurrent
// edits. Each case is composed at every delivery permutation (post A's
// order, post B's order, a third post's either way) and under four
// time-basis variants where ONLY the timestamps change: rotated among the
// amendments, all equal, the loser moved ten years later, the winner moved
// ten years earlier. Composed bytes and every verdict must be identical
// across all of it. Case (a)'s and case (c)'s corrections are asserted to
// carry DIFFERENT `seen` bytes — the wire difference that makes the read
// differ.
func TestD92FourDominanceCasesAreClockFreeAtEveryPost(t *testing.T) {
	cases := d92BuildCases(t)
	if len(cases) != 4 {
		t.Fatalf("fixture defect: %d cases, want 4", len(cases))
	}
	const tenYearsMS = int64(10 * 365 * 24 * 3_600_000)

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			// Rule 3: the named rivals disagree with the certified answer.
			w3RequireDistinct(t, c.name, c.wantPhone, c.rivals(t, c.amend))
			// The clock rival names a LOSER in every case — asserted as a
			// fixture property so a compose that read a clock cannot pass
			// here by coincidence.
			if clock := d92ClockRival(t, c.amend); clock == c.wantPhone {
				t.Fatalf("%s fixture: the clock rival names the winner — the fixture cannot tell a clock-reading compose from the rule", c.name)
			}

			keys := d92Keys(c.amend)
			var wantBytes, wantVerdicts string
			// Every deliverable interleaving of the contested set: post A's
			// order, post B's order, a third post's either way.
			orders := d92Interleavings(c.amend)
			if len(orders) < 2 {
				t.Fatalf("%s fixture: only %d deliverable interleaving — nothing to compare", c.name, len(orders))
			}
			for i, order := range orders {
				st := d92Fresh(t, c.roster, order)
				d92AssertCase(t, c, st, fmt.Sprintf("delivery interleaving %d %v", i, d92Keys(order)))
				gotBytes, gotVerdicts := w3ComposedBytes(t, st), d92Verdicts(t, st, keys)
				if i == 0 {
					wantBytes, wantVerdicts = gotBytes, gotVerdicts
					continue
				}
				if gotBytes != wantBytes {
					t.Fatalf("%s: composed bytes differ between delivery interleavings 0 and %d %v —\n%s\n%s", c.name, i, d92Keys(order), wantBytes, gotBytes)
				}
				if gotVerdicts != wantVerdicts {
					t.Fatalf("%s: verdicts differ between delivery interleavings 0 and %d %v —\n%s\n%s", c.name, i, d92Keys(order), wantVerdicts, gotVerdicts)
				}
			}

			// Four variants in which ONLY a timestamp changes.
			type basis struct {
				auth store.AuthoredAt
				occ  int64
				src  string
			}
			basisOf := func(e store.Envelope) basis { return basis{e.AuthoredAt, e.OccurredAt, e.TimeSource} }
			withBasis := func(e store.Envelope, b basis) store.Envelope {
				e.AuthoredAt, e.OccurredAt, e.TimeSource = b.auth, b.occ, b.src
				return e
			}
			shift := func(e store.Envelope, deltaMS int64) store.Envelope {
				e.AuthoredAt.WallMS += deltaMS
				e.OccurredAt += deltaMS
				return e
			}
			variants := map[string][]store.Envelope{}
			{ // rotated: each amendment takes the next one's time basis
				v := make([]store.Envelope, len(c.amend))
				for i := range c.amend {
					v[i] = withBasis(c.amend[i], basisOf(c.amend[(i+1)%len(c.amend)]))
				}
				variants["time bases rotated"] = v
			}
			{ // all equal
				v := make([]store.Envelope, len(c.amend))
				for i := range c.amend {
					v[i] = withBasis(c.amend[i], basisOf(c.amend[0]))
				}
				variants["all time bases equal"] = v
			}
			{ // the loser(s) ten years later than everything
				v := append([]store.Envelope{}, c.amend...)
				for i := range v {
					for _, l := range c.losers {
						if v[i].Key() == l {
							v[i] = shift(v[i], tenYearsMS)
						}
					}
				}
				variants["losers ten years later"] = v
			}
			{ // the winner ten years earlier than everything
				v := append([]store.Envelope{}, c.amend...)
				for i := range v {
					if v[i].Key() == c.winner {
						v[i] = shift(v[i], -tenYearsMS)
					}
				}
				variants["winner ten years earlier"] = v
			}
			names := make([]string, 0, len(variants))
			for n := range variants {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				v := variants[n]
				// The variant genuinely moved a clock (rule 3 on the mutation).
				moved := false
				for i := range v {
					if v[i].AuthoredAt.WallMS != c.amend[i].AuthoredAt.WallMS {
						moved = true
					}
				}
				if !moved {
					t.Fatalf("%s / %s: the variant moved no clock — it tests nothing", c.name, n)
				}
				st := d92Fresh(t, c.roster, v)
				d92AssertCase(t, c, st, n)
				if got := w3ComposedBytes(t, st); got != wantBytes {
					t.Fatalf("D-90/D-92 (m1): %s — composed bytes CHANGED when only a timestamp changed (%s):\nwant %s\ngot  %s\nthe clock has crept back into ordering", c.name, n, wantBytes, got)
				}
				if got := d92Verdicts(t, st, keys); got != wantVerdicts {
					t.Fatalf("D-90/D-92 (m1): %s — Outcome verdicts CHANGED when only a timestamp changed (%s):\nwant %s\ngot  %s", c.name, n, wantVerdicts, got)
				}
			}
		})
	}

	// (a) vs (c): the correction's `seen` bytes differ — (a)'s names the
	// peer's assertion, (c)'s names only the shared p0. The wire carries the
	// difference the read acts on.
	seenA := d92RawSeen(t, cases[0].amend[1]) // (a): A's correction
	seenC := d92RawSeen(t, cases[2].amend[1]) // (c): A's concurrent correction
	if seenA == "" || seenC == "" || seenA == seenC {
		t.Fatalf("D-92: case (a)'s correction carries seen=%s and case (c)'s seen=%s — they must both be present and DIFFER (item 12: the decisive and concurrent payloads differ in bytes)", seenA, seenC)
	}
	// (b) vs (c): B's amendment names A in (b) and does NOT in (c).
	bOfB, bOfC := w3SeenOf(t, cases[1].amend[1]), w3SeenOf(t, cases[2].amend[2])
	if !w3KeysEqual(bOfB, []store.Key{cases[1].amend[0].Key()}) {
		t.Fatalf("case (b): B names %v, want [A]", bOfB)
	}
	for _, k := range bOfC {
		if k == cases[2].amend[1].Key() {
			t.Fatalf("case (c): B names A (%v) — then it is not concurrent", k)
		}
	}
}

// =======================================================================
// Delivery-order independence over a real record set
// =======================================================================

// TestD92ComposedStateIsInvariantUnderEveryDeliveryPermutation — five
// amendments over two persons: a concurrent phone pair (a1 ‖ b1), a
// correction made in knowledge of BOTH heads (c1 names a1 and b1), a merge
// (m1, site-b) and an un-merge made in knowledge of it (u1 names m1). Every
// deliverable interleaving of the two streams (each stream contiguous; the
// cross-stream order free — C(5,2) = 10) composes byte-identical state, an
// identical projection dump and identical verdicts for all five keys; the
// arrival-order rival is asserted to disagree with itself across
// interleavings.
func TestD92ComposedStateIsInvariantUnderEveryDeliveryPermutation(t *testing.T) {
	origin := w3NewStore(t)
	x, y, roster := w3RosterPair(t, origin, "Perm X", "Perm Y")
	a1 := w3AmendLocal(t, origin, "site-a", 1, w3Base+1*w3Hour, PersonAmendment{PersonID: x, Phone: w3Str("555-0851")})
	remote := w3AuthorRemote(t, roster, "site-b", 1, w3Base+2*w3Hour, func(rs *store.Store) {
		if _, err := AmendPerson(rs, "site-b", 1, PersonAmendment{PersonID: x, Phone: w3Str("555-0852")}, 0, "default"); err != nil {
			t.Fatal(err)
		}
		if _, err := AmendPerson(rs, "site-b", 1, PersonAmendment{PersonID: x, SameAs: w3Str(y)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	if len(remote) != 2 {
		t.Fatalf("fixture defect: site-b minted %d events, want 2", len(remote))
	}
	b1, m1 := remote[0], remote[1]
	w3ApplyAll(t, origin, remote)
	c1 := w3AmendLocal(t, origin, "site-a", 1, w3Base+3*w3Hour, PersonAmendment{PersonID: x, Phone: w3Str("555-0853")})
	u1 := w3AmendLocal(t, origin, "site-a", 1, w3Base+4*w3Hour, PersonAmendment{PersonID: x, Clear: []string{w3FactSameAs}})

	// The fixture states its own causal arrangement (rule 6).
	if got := w3SeenOf(t, a1); len(got) != 0 {
		t.Fatalf("fixture: a1 names %v, want nothing", got)
	}
	if got := w3SeenOf(t, b1); len(got) != 0 {
		t.Fatalf("fixture: b1 names %v, want nothing (concurrent with a1)", got)
	}
	wantC := []store.Key{a1.Key(), b1.Key()}
	if got := w3SeenOf(t, c1); !w3KeysEqual(got, wantC) {
		t.Fatalf("fixture: c1 names %v, want both heads %v", got, wantC)
	}
	if got := w3SeenOf(t, u1); !w3KeysEqual(got, []store.Key{m1.Key()}) {
		t.Fatalf("fixture: u1 names %v, want [%v]", got, m1.Key())
	}

	amend := []store.Envelope{a1, b1, m1, c1, u1}
	keys := d92Keys(amend)
	wantBytes, wantVerdicts := w3ComposedBytes(t, origin), d92Verdicts(t, origin, keys)
	dump, err := origin.DumpProjections()
	if err != nil {
		t.Fatal(err)
	}
	wantDump := string(dump)
	// The origin's own answer is the pinned one.
	if p := w3Person(t, origin, x); p.Phone == nil || *p.Phone != "555-0853" || p.SameAs != nil {
		t.Fatalf("origin: composed X = %+v, want phone 555-0853 (c1 over both heads) and same_as absent (u1 over m1)", p)
	}
	if got := w3MergeMap(t, origin).Resolve(x); got != x {
		t.Fatalf("origin: Resolve(%s) = %s, want identity", x, got)
	}

	arrivalAnswers := map[string]bool{}
	orders := d92Interleavings(amend) // 3 site-a + 2 site-b events: C(5,2) = 10
	if len(orders) < 4 {
		t.Fatalf("fixture: only %d deliverable interleavings, want several", len(orders))
	}
	for i, order := range orders {
		perm := d92Keys(order)
		// arrival-order rival: the last-applied phone.
		for k := len(order) - 1; k >= 0; k-- {
			var pl PersonAmended
			if err := json.Unmarshal(order[k].Payload, &pl); err == nil && pl.Phone != nil {
				arrivalAnswers[*pl.Phone] = true
				break
			}
		}
		st := d92Fresh(t, roster, order)
		if got := w3ComposedBytes(t, st); got != wantBytes {
			t.Fatalf("W3-5 re-locked (m10): composed bytes differ at delivery permutation %d %v —\nwant %s\ngot  %s", i, perm, wantBytes, got)
		}
		if got := d92Verdicts(t, st, keys); got != wantVerdicts {
			t.Fatalf("W3-5 re-locked (m10): verdicts differ at delivery permutation %d %v —\nwant %s\ngot  %s", i, perm, wantVerdicts, got)
		}
		d, err := st.DumpProjections()
		if err != nil {
			t.Fatal(err)
		}
		if string(d) != wantDump {
			t.Fatalf("W3-5 re-locked (m10): projection dump differs at delivery permutation %d %v", i, perm)
		}
	}
	if len(arrivalAnswers) < 2 {
		t.Fatalf("FIXTURE DEFECT: DEGENERATE — the arrival-order rival answers %v on every permutation", arrivalAnswers)
	}
}

// =======================================================================
// The merge-group half of the stamp
// =======================================================================

// TestD92TheStampNamesTheHeadsOfTheMergedGroupNotOnlyTheTargetsOwnRows —
// personSeen unions the heads of the target's OWN contest with the heads
// of the merged GROUP's contest (the scope Outcome and Competitors judge,
// W3-9/W3-10). X is merged into Y and X carries a phone from site-b (the
// greater key). site-a then gives Y a phone: no row targets Y's phone, so
// the target-scope heads are empty — the stamp must still name site-b's
// row on X, because in the group's contest that row stands. With the name,
// Outcome(Y's phone) is Effective and site-b's row is Superseded by a
// LESSER key; without it the pair is concurrent and the greater key wins.
//
// ⚠ The composed value of Y.phone is IDENTICAL either way (composition is
// target-scoped) — that assertion is kept below, labelled BLIND, so nobody
// mistakes it for the instrument. Outcome is the instrument.
func TestD92TheStampNamesTheHeadsOfTheMergedGroupNotOnlyTheTargetsOwnRows(t *testing.T) {
	st := w3NewStore(t)
	x, y, roster := w3RosterPair(t, st, "Group X", "Group Y")
	remote := w3AuthorRemote(t, roster, "site-b", 1, w3Base+3*w3Hour, func(rs *store.Store) {
		if _, err := AmendPerson(rs, "site-b", 1, PersonAmendment{PersonID: x, SameAs: w3Str(y)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
		if _, err := AmendPerson(rs, "site-b", 1, PersonAmendment{PersonID: x, Phone: w3Str("555-0861")}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	if len(remote) != 2 {
		t.Fatalf("fixture defect: site-b minted %d events, want 2", len(remote))
	}
	m1, bx := remote[0], remote[1]
	w3ApplyAll(t, st, remote)
	if got := w3MergeMap(t, st).Resolve(x); got != y {
		t.Fatalf("fixture: Resolve(%s) = %s, want %s — the merge must stand before Y is amended", x, got, y)
	}
	ay := w3AmendLocal(t, st, "site-a", 1, w3Base+1*w3Hour, PersonAmendment{PersonID: y, Phone: w3Str("555-0862")})

	// Rule 6: ay is the LESSER key and the LATER-clock loser under both
	// rivals; only the stamp can make it win the group contest.
	if !ay.Key().Less(bx.Key()) {
		t.Fatalf("fixture: %v is not less than %v", ay.Key(), bx.Key())
	}
	if bx.AuthoredAt.WallMS <= ay.AuthoredAt.WallMS {
		t.Fatalf("fixture: site-b's row must carry the later clock so the clock rival names it")
	}

	// THE STAMP: the group's head, though no row targets Y's phone.
	if got := w3SeenOf(t, ay); !w3KeysEqual(got, []store.Key{bx.Key()}) {
		t.Fatalf("D-92 stamp (merge-group scope): Y's amendment names %v, want exactly [%v] — the head of the MERGED GROUP's phone contest; personSeen must union the group scope with the target scope, or Outcome and the stamp disagree about what the author saw", got, bx.Key())
	}

	for i, post := range []*store.Store{st,
		d92Fresh(t, roster, []store.Envelope{ay, m1, bx}), // site-a's stream first
		d92Fresh(t, roster, []store.Envelope{m1, bx, ay}), // site-b's stream first (site-b's own view)
	} {
		// The instrument: Outcome in the group scope.
		if got := w3Fact(t, post, ay.Key(), w3FactPhone); got.Status != Effective || got.Winner != ay.Key() {
			t.Fatalf("D-92 (post %d): Outcome(Y's phone %v) = (%v, %v), want (Effective, itself) — the amendment made in knowledge of the group's standing row wins the group contest regardless of site_id", i, ay.Key(), got.Status, got.Winner)
		}
		if got := w3Fact(t, post, bx.Key(), w3FactPhone); got.Status != Superseded || got.Winner != ay.Key() {
			t.Fatalf("D-92 (post %d): Outcome(site-b's row %v) = (%v, %v), want (Superseded, %v) — superseded by a LESSER key because that key's author saw it", i, bx.Key(), got.Status, got.Winner, ay.Key())
		}
		comp, err := Competitors(post, ay.Key())
		if err != nil {
			t.Fatal(err)
		}
		if !w3KeysEqual(comp[w3FactPhone], []store.Key{bx.Key()}) {
			t.Fatalf("W5d-9(b) (post %d): Competitors(Y's phone) = %v, want [%v] — the group is the contest", i, comp[w3FactPhone], bx.Key())
		}
		// BLIND (by construction, not by defect): the composed value is
		// target-scoped and reads 555-0862 whether or not the stamp named
		// the group's head. Kept so the reader sees what it cannot see.
		if p := w3Person(t, post, y); p.Phone == nil || *p.Phone != "555-0862" {
			t.Fatalf("composition (post %d): Y.phone = %v, want 555-0862", i, p.Phone)
		}
	}
}

// =======================================================================
// Fold tolerance: a divergent peer's envelope still carrying pad_oriented
// =======================================================================

// TestD92TheFoldLandsAReplicatedEnvelopeThatStillCarriesPadOriented — the
// key is struck from the wire shape, but during the window a divergent
// peer's envelope may still carry it. The fold NEVER refuses (W3-2): a
// person_created with the stale key lands as a person; a person_amended
// carrying it beside a phone and a `seen` list lands with the phone and
// the reference intact; a `clear` naming it lands and clears nothing. If
// the fold errored on any of these, the stream would stall (U4). Both
// Apply and ApplyBatch (the engine's path) are driven.
func TestD92TheFoldLandsAReplicatedEnvelopeThatStillCarriesPadOriented(t *testing.T) {
	build := func(t *testing.T) (roster []store.Envelope, x string, wire []store.Envelope) {
		t.Helper()
		origin := w3NewStore(t)
		var a string
		a, _, roster = w3RosterPair(t, origin, "Stale X", "Stale Y")
		x = a
		// A person_created still carrying the struck key: take a real roster
		// payload, re-target it, add the key.
		var created map[string]any
		if err := json.Unmarshal(roster[0].Payload, &created); err != nil {
			t.Fatal(err)
		}
		if created["type"] != "person_created" {
			t.Fatalf("fixture: roster[0] is %v, want person_created", created["type"])
		}
		created["person_id"] = "stale-pad-z"
		created["name"] = "Stale Z"
		created["pad_oriented"] = "true"
		raw, err := json.Marshal(created)
		if err != nil {
			t.Fatal(err)
		}
		ez := store.Envelope{
			SiteID: "site-b", NodeEpoch: 1, Seq: 1,
			SchemaVersion: store.SchemaVersion, OriginAuth: "site-b",
			OccurredAt: w3Base + w3Hour, TimeSource: "default",
			Payload: json.RawMessage(raw),
		}
		ez.AuthoredAt.WallMS = w3Base + w3Hour
		ez.AuthoredAt.MaxErrorUS = 42
		ez.AuthoredAt.State = store.ClockSynced
		// An amendment carrying the stale key beside a phone and a seen list
		// naming the roster's first event (any key; the reference must
		// survive verbatim).
		ref := map[string]any{"site_id": roster[0].SiteID, "node_epoch": roster[0].NodeEpoch, "seq": roster[0].Seq}
		e1 := w3WireAmendment(t, "site-b", 1, 2, w3Base+2*w3Hour, x, map[string]any{"phone": "555-0871", "pad_oriented": "true", "seen": []any{ref}})
		// An amendment clearing the struck key beside a real fact.
		e2 := w3WireAmendment(t, "site-b", 1, 3, w3Base+3*w3Hour, x, map[string]any{"position": "Rigger", "clear": []any{"pad_oriented"}})
		return roster, x, []store.Envelope{ez, e1, e2}
	}
	check := func(t *testing.T, st *store.Store, x string, wantRef store.Key) {
		t.Helper()
		if p, err := PersonByID(st, "stale-pad-z"); err != nil || p == nil || p.Name != "Stale Z" {
			t.Fatalf("W3-2 / U4: the person_created carrying pad_oriented did not land as a person (p=%v err=%v)", p, err)
		}
		p := w3Person(t, st, x)
		if p.Phone == nil || *p.Phone != "555-0871" {
			t.Fatalf("W3-2 / U4: the phone beside the stale key did not compose: %v", p.Phone)
		}
		if p.Position == nil || *p.Position != "Rigger" {
			t.Fatalf("W3-2 / U4: the position beside the stale clear did not compose: %v", p.Position)
		}
		rows, err := readPersonAmendmentsFor(st, x)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 {
			t.Fatalf("W3-2: %d person_amendment rows for %s, want 2 — both stale-keyed envelopes must land", len(rows), x)
		}
		for _, r := range rows {
			if _, has := r.Set["pad_oriented"]; has {
				t.Fatalf("SchemaVersion 11: the read decoded a pad_oriented fact from row %v — the fact is struck", r.Key)
			}
		}
		if !w3KeysEqual(rows[0].Seen, []store.Key{wantRef}) {
			t.Fatalf("D-92: the seen reference beside the stale key did not survive the fold: %v, want [%v]", rows[0].Seen, wantRef)
		}
		if len(rows[1].Cleared) != 1 || rows[1].Cleared[0] != "pad_oriented" {
			t.Fatalf("W3-2: the clear list landed as %v, want [pad_oriented] verbatim (the fold stores what arrived; composition ignores the struck fact)", rows[1].Cleared)
		}
	}

	t.Run("Apply, one envelope at a time", func(t *testing.T) {
		roster, x, wire := build(t)
		st := w3NewStore(t)
		w3ApplyAll(t, st, roster)
		for _, e := range wire {
			applied, err := st.Apply(e)
			if err != nil {
				t.Fatalf("W3-2 / U4 STALL: Apply(%v) refused an envelope carrying a stale pad_oriented key: %v", e.Key(), err)
			}
			if !applied {
				t.Fatalf("Apply(%v): not applied", e.Key())
			}
		}
		check(t, st, x, roster[0].Key())
	})
	t.Run("ApplyBatch, the engine's path", func(t *testing.T) {
		roster, x, wire := build(t)
		st := w3NewStore(t)
		w3ApplyAll(t, st, roster)
		results, err := st.ApplyBatch(wire)
		if err != nil {
			t.Fatalf("W3-2 / U4 STALL: ApplyBatch refused a batch carrying stale pad_oriented keys: %v", err)
		}
		if len(results) != len(wire) {
			t.Fatalf("ApplyBatch: %d results for %d envelopes — a failing envelope truncates the batch", len(results), len(wire))
		}
		for i, r := range results {
			if !r {
				t.Fatalf("ApplyBatch: envelope %d (%v) not applied", i, wire[i].Key())
			}
		}
		check(t, st, x, roster[0].Key())
	})
}
