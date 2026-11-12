// w3_causal_compose_test.go — W3-1/W3-5 RE-LOCKED at SchemaVersion 11 under
// [[D-92]]: composition is CAUSAL, then ascending (site_id, node_epoch, seq).
//
// ⟦was: "Composition is ascending (site_id, node_epoch, seq)" — [[D-28]]
// Part B, fixed order, one post always wins. REVERSED by [[D-92]]
// (JAMES-DECISIONS.md, 2026-09-05): "A correction made in full knowledge of
// an earlier one WINS; site_id breaks ties ONLY for genuinely concurrent
// edits — the case where neither author saw the other." Re-ruled and
// recorded, never silently edited (D-74/D-78, re-affirmed by D-92).⟧
//
// The rule under witness is resolveContest (person_amend.go), spelled once:
// an assertion named by another contestant's `seen` is DOMINATED and never
// wins; among the undominated the greatest key wins; a contest with no
// survivor (a wire-formed reference cycle) falls to the greatest key so the
// read stays total. `seen` is stamped by AmendPerson from personSeen — every
// HEAD (undominated survivor) of each asserted fact's contest as the
// authoring post holds it — and never by a caller.
//
// Every pinned literal below is guarded by standing rule 3: the named rival
// answers (the fixed-order rule D-92 reversed, the chronological rule D-90
// refused, arrival order) are recomputed from the read-back log and asserted
// DIFFERENT from the certified answer, so a fixture on which the rule and a
// rival agree cannot certify anything.
//
// KILLER MUTANTS, each PRODUCED red once against this tree before commit
// (reports/witness/STEP2-RESCUE.md):
//   - m-D92a: resolveContest ignores `seen` — the tiebreak is reached on a
//     causally ordered pair (D-28 Part B resurrected). Killed by
//     TestW3CausalCorrectionMadeInKnowledgeOfTheEarlierOneWins,
//     TestW3DominanceChainsThroughDirectReferences,
//     TestW3UnMergeMadeInKnowledgeOfThePeersMergeTakesEffect and the
//     invariance test.
//   - m-D92b: AmendPerson drops the `seen` stamp (authors an empty list).
//     Killed by TestW3SeenIsStampedAtTheEdgeFromEveryHeadOfTheContest and,
//     behaviourally, by every causal test above.
//   - m5 (epoch-blind ORDER BY, seq-major): at 11 W3-1's own cross-epoch
//     fixture is a CAUSAL chain (e2 saw e1b) and reaches its pin by
//     dominance, so m5 survives it; the compose-path kill moves to
//     TestW3ConcurrentCrossEpochPairStillPinsTheEpochMajorWinner. W3-11's
//     truth table still kills m5 at the comparator.
package domain

import (
	"encoding/json"
	"reflect"
	"testing"

	"sitecontrol/internal/store"
)

// --- helpers ------------------------------------------------------------

// w3SeenOf decodes the `seen` list off an envelope's OWN payload bytes.
func w3SeenOf(t *testing.T, env store.Envelope) []store.Key {
	t.Helper()
	var pl PersonAmended
	if err := json.Unmarshal(env.Payload, &pl); err != nil {
		t.Fatal(err)
	}
	var out []store.Key
	for _, r := range pl.Seen {
		out = append(out, r.key())
	}
	return out
}

// w3SeenRowOf reads the same list back off the person_amendment row — the
// fold/read round trip.
func w3SeenRowOf(t *testing.T, st *store.Store, k store.Key, personID string) []store.Key {
	t.Helper()
	rows, err := readPersonAmendmentsFor(st, personID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Key == k {
			return r.Seen
		}
	}
	t.Fatalf("no person_amendment row at %v for %s", k, personID)
	return nil
}

func w3KeysEqual(a, b []store.Key) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func w3PhoneOf(t *testing.T, env store.Envelope) string {
	t.Helper()
	var pl PersonAmended
	if err := json.Unmarshal(env.Payload, &pl); err != nil {
		t.Fatal(err)
	}
	if pl.Phone == nil {
		t.Fatalf("envelope %v asserts no phone", env.Key())
	}
	return *pl.Phone
}

// w3PhoneRivals recomputes the named rival rules over the phone-carrying
// envelopes as READ BACK, never from fixture constants.
func w3PhoneRivals(t *testing.T, evs []store.Envelope) map[string]string {
	t.Helper()
	var phones []store.Envelope
	for _, e := range evs {
		var pl PersonAmended
		if err := json.Unmarshal(e.Payload, &pl); err == nil && pl.Type == w3EvtPersonAmended && pl.Phone != nil {
			phones = append(phones, e)
		}
	}
	if len(phones) < 2 {
		t.Fatalf("fixture defect: %d phone-carrying envelopes, want a contest", len(phones))
	}
	byKey, byClock := phones[0], phones[0]
	for _, e := range phones[1:] {
		if byKey.Key().Less(e.Key()) {
			byKey = e
		}
		if e.AuthoredAt.WallMS > byClock.AuthoredAt.WallMS {
			byClock = e
		}
	}
	return map[string]string{
		"fixed order — greatest key wins [D-28 Part B, REVERSED by D-92]": w3PhoneOf(t, byKey),
		"chronological max(AuthoredAt) [m1, refused by D-90/D-92]":        w3PhoneOf(t, byClock),
		"arrival order — last applied [m10]":                              w3PhoneOf(t, phones[len(phones)-1]),
	}
}

// w3ComposedBytes is the byte-level composed state a post shows: every
// composed person plus the merge snapshot.
func w3ComposedBytes(t *testing.T, st *store.Store) string {
	t.Helper()
	b, err := json.Marshal(struct {
		Persons []Person
		Merge   AliasMap
	}{mustPersons(t, st), w3MergeMap(t, st)})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// =======================================================================
// W3-1 re-locked — the correction made in knowledge of the earlier one wins
// =======================================================================

// TestW3CausalCorrectionMadeInKnowledgeOfTheEarlierOneWins — D-92's first
// sentence. site-b (the GREATER site_id — the fixed-order winner, and here
// also the chronological winner) asserts a phone at a post holding only the
// roster. site-a receives it, reads it on the screen and corrects it. The
// LOWER-keyed correction wins, at site-a and at a third post that receives
// both by wire in either order; Outcome and Competitors read the same rule;
// site-a's envelope names exactly site-b's key as what it saw.
func TestW3CausalCorrectionMadeInKnowledgeOfTheEarlierOneWins(t *testing.T) {
	const phoneB, phoneA = "555-0611", "555-0612"
	st := w3NewStore(t)
	x, _, roster := w3RosterPair(t, st, "Causal X", "Causal Y")

	b1 := w3AuthorRemote(t, roster, "site-b", 1, w3Base+3*w3Hour, func(rs *store.Store) {
		if _, err := AmendPerson(rs, "site-b", 1, PersonAmendment{PersonID: x, Phone: w3Str(phoneB)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	if len(b1) != 1 {
		t.Fatalf("fixture defect: site-b minted %d events, want 1", len(b1))
	}
	w3ApplyAll(t, st, b1)
	a1 := w3AmendLocal(t, st, "site-a", 1, w3Base+1*w3Hour,
		PersonAmendment{PersonID: x, Phone: w3Str(phoneA)})

	// The fixture states its own arrangement (rule 6): a1 is the LESSER key
	// and its author saw b1 standing; b1's author saw nothing.
	if !a1.Key().Less(b1[0].Key()) {
		t.Fatalf("fixture defect: site-a's correction %v is not the lesser key against %v — the fixed-order rival would then agree with the rule", a1.Key(), b1[0].Key())
	}
	if seen := w3SeenOf(t, a1); !w3KeysEqual(seen, []store.Key{b1[0].Key()}) {
		t.Fatalf("D-92 (m-D92b, dropped stamp): site-a's correction carries seen=%v, want exactly [%v] — the key its author saw standing", seen, b1[0].Key())
	}
	if seen := w3SeenOf(t, b1[0]); len(seen) != 0 {
		t.Fatalf("fixture defect: site-b's assertion carries seen=%v, want none (it was authored over the bare roster)", seen)
	}
	w3RequireDistinct(t, "W3-1 causal (D-92)", phoneA,
		w3PhoneRivals(t, append(w3Events(t, st, "site-a", 1), w3Events(t, st, "site-b", 1)...)))

	// THE PIN.
	if p := w3Person(t, st, x); p.Phone == nil || *p.Phone != phoneA {
		t.Fatalf("W3-1 re-locked (D-92; m-D92a, fixed order resurrected): composed phone = %v, want site-a's %q — the correction made IN KNOWLEDGE of site-b's wins regardless of site_id; %q is D-28 Part B's answer, which D-92 reversed", p.Phone, phoneA, phoneB)
	}
	ob := w3Fact(t, st, b1[0].Key(), w3FactPhone)
	if ob.Status != Superseded || ob.Winner != a1.Key() {
		t.Fatalf("W3-9 under D-92: site-b's assertion is (%v, winner %v), want (Superseded, %v) — superseded by a key LESS than its own, because that key's author saw it", ob.Status, ob.Winner, a1.Key())
	}
	oa := w3Fact(t, st, a1.Key(), w3FactPhone)
	if oa.Status != Effective || oa.Winner != a1.Key() {
		t.Fatalf("W3-9 under D-92: site-a's correction is (%v, winner %v), want (Effective, itself)", oa.Status, oa.Winner)
	}
	ca, err := Competitors(st, a1.Key())
	if err != nil {
		t.Fatal(err)
	}
	cb, err := Competitors(st, b1[0].Key())
	if err != nil {
		t.Fatal(err)
	}
	if !w3KeysEqual(ca[w3FactPhone], []store.Key{b1[0].Key()}) || !w3KeysEqual(cb[w3FactPhone], []store.Key{a1.Key()}) {
		t.Fatalf("W5d-9(b) under D-92: Competitors are not symmetric over the pair: a1→%v b1→%v", ca[w3FactPhone], cb[w3FactPhone])
	}

	// W3-5's clause: a THIRD post receiving both by wire, in either order,
	// shows the same bytes and the same verdicts.
	var want string
	for i, order := range [][]store.Envelope{{a1, b1[0]}, {b1[0], a1}} {
		third := w3NewStore(t)
		w3ApplyAll(t, third, roster)
		w3ApplyAll(t, third, order)
		if q := w3Person(t, third, x); q.Phone == nil || *q.Phone != phoneA {
			t.Fatalf("W3-5 under D-92: a third post receiving order %d composes phone %v, want %q — the rule is a pure function of the record set", i, q.Phone, phoneA)
		}
		if got := w3Fact(t, third, b1[0].Key(), w3FactPhone); got.Status != Superseded || got.Winner != a1.Key() {
			t.Fatalf("W3-5 under D-92: at a third post (order %d) site-b's assertion is (%v, %v), want (Superseded, %v)", i, got.Status, got.Winner, a1.Key())
		}
		got := w3ComposedBytes(t, third)
		if i == 0 {
			want = got
			continue
		}
		if got != want {
			t.Fatalf("W3-5 under D-92: composed bytes differ between delivery orders 0 and %d —\n%s\n%s", i, want, got)
		}
	}
}

// =======================================================================
// D-92's second sentence — site_id breaks ties ONLY between concurrent edits
// =======================================================================

// TestW3GenuinelyConcurrentEditsFallToTheSiteIDTiebreak — the partition
// case. Both posts hold an earlier assertion p0; each corrects it without
// seeing the other's correction. Both `seen` lists name p0 and neither names
// the other; the greater site_id wins at BOTH posts; p0 is superseded by the
// tiebreak winner. The chronological rival (site-a's later clock) and the
// lesser-key rival both disagree with the certified answer.
func TestW3GenuinelyConcurrentEditsFallToTheSiteIDTiebreak(t *testing.T) {
	const phone0, phoneA, phoneB = "555-0620", "555-0621", "555-0622"
	st := w3NewStore(t)
	x, _, _ := w3RosterPair(t, st, "Concurrent X", "Concurrent Y")
	p0 := w3AmendLocal(t, st, "site-a", 1, w3Base, PersonAmendment{PersonID: x, Phone: w3Str(phone0)})
	held := w3Events(t, st, "site-a", 1) // the roster and p0: what BOTH posts hold when the link drops

	a1 := w3AmendLocal(t, st, "site-a", 1, w3Base+3*w3Hour, PersonAmendment{PersonID: x, Phone: w3Str(phoneA)})
	b1 := w3AuthorRemote(t, held, "site-b", 1, w3Base+1*w3Hour, func(rs *store.Store) {
		if _, err := AmendPerson(rs, "site-b", 1, PersonAmendment{PersonID: x, Phone: w3Str(phoneB)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	w3ApplyAll(t, st, b1)

	// Rule 6: genuinely concurrent — each saw p0 and NOT the other.
	if seen := w3SeenOf(t, a1); !w3KeysEqual(seen, []store.Key{p0.Key()}) {
		t.Fatalf("fixture defect: site-a's correction carries seen=%v, want [%v]", seen, p0.Key())
	}
	if seen := w3SeenOf(t, b1[0]); !w3KeysEqual(seen, []store.Key{p0.Key()}) {
		t.Fatalf("fixture defect: site-b's correction carries seen=%v, want [%v]", seen, p0.Key())
	}
	rivals := w3PhoneRivals(t, append(w3Events(t, st, "site-a", 1), b1...))
	delete(rivals, "fixed order — greatest key wins [D-28 Part B, REVERSED by D-92]") // ON a concurrent pair the tiebreak IS the fixed order — that is the point
	delete(rivals, "arrival order — last applied [m10]")                              // b1 was applied last at site-a; delivery order is W3-5's invariance test's subject
	rivals["lesser key wins (an inverted tiebreak)"] = phoneA
	w3RequireDistinct(t, "D-92 concurrent tiebreak", phoneB, rivals)

	for name, post := range map[string]*store.Store{"site-a's post": st} {
		if p := w3Person(t, post, x); p.Phone == nil || *p.Phone != phoneB {
			t.Fatalf("D-92 tiebreak at %s: composed phone = %v, want the greater site_id's %q — neither author saw the other, so site_id decides (D-28 Part B's one surviving case)", name, p.Phone, phoneB)
		}
	}
	// site-b's own view: the same set, the other way round.
	viewB := w3NewStore(t)
	w3ApplyAll(t, viewB, held)
	w3ApplyAll(t, viewB, b1)
	w3Apply(t, viewB, a1)
	if p := w3Person(t, viewB, x); p.Phone == nil || *p.Phone != phoneB {
		t.Fatalf("D-92 tiebreak at site-b's post: composed phone = %v, want %q", p.Phone, phoneB)
	}
	for _, c := range []struct {
		name string
		key  store.Key
	}{{"p0", p0.Key()}, {"site-a's correction", a1.Key()}} {
		if got := w3Fact(t, st, c.key, w3FactPhone); got.Status != Superseded || got.Winner != b1[0].Key() {
			t.Fatalf("D-92 tiebreak: %s is (%v, %v), want (Superseded, %v)", c.name, got.Status, got.Winner, b1[0].Key())
		}
	}
	if got := w3Fact(t, st, b1[0].Key(), w3FactPhone); got.Status != Effective {
		t.Fatalf("D-92 tiebreak: the tiebreak winner is %v, want Effective", got.Status)
	}
}

// =======================================================================
// W3-1's scramble + W3-5's permuted delivery, on a fixture `seen` decides
// =======================================================================

// TestW3CausalComposeIsInvariantUnderDeliveryOrderAndTimeBasis — the
// re-locked W3-1/W3-5 invariance clauses on a pair where dominance, not
// key order, picks the winner: byte-identical composed state under every
// delivery interleaving of the two streams and every permutation of the
// two amendments' time bases. The arrival-order rival gives DIFFERENT
// answers across interleavings (asserted), the clock rival and the
// fixed-order rival both name the loser (asserted).
func TestW3CausalComposeIsInvariantUnderDeliveryOrderAndTimeBasis(t *testing.T) {
	const phoneB, phoneA = "555-0631", "555-0632"
	origin := w3NewStore(t)
	x, _, roster := w3RosterPair(t, origin, "Invariant X", "Invariant Y")
	b1 := w3AuthorRemote(t, roster, "site-b", 1, w3Base+3*w3Hour, func(rs *store.Store) {
		if _, err := AmendPerson(rs, "site-b", 1, PersonAmendment{PersonID: x, Phone: w3Str(phoneB)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	w3ApplyAll(t, origin, b1)
	a1 := w3AmendLocal(t, origin, "site-a", 1, w3Base+1*w3Hour, PersonAmendment{PersonID: x, Phone: w3Str(phoneA)})
	if seen := w3SeenOf(t, a1); !w3KeysEqual(seen, []store.Key{b1[0].Key()}) {
		t.Fatalf("fixture defect: a1 does not name b1 as seen (%v)", seen)
	}

	interleavings := [][]store.Envelope{
		append(append([]store.Envelope{}, roster...), b1[0], a1),
		append(append([]store.Envelope{}, roster...), a1, b1[0]),
		append(append([]store.Envelope{roster[0]}, b1[0]), append(roster[1:], a1)...),
	}
	arrivalAnswers := map[string]bool{}
	for _, order := range interleavings {
		arrivalAnswers[w3PhoneRivals(t, order)["arrival order — last applied [m10]"]] = true
	}
	if len(arrivalAnswers) < 2 {
		t.Fatalf("FIXTURE DEFECT: DEGENERATE — the arrival-order rival answers %v on every interleaving; it cannot then be told from the rule", arrivalAnswers)
	}
	w3RequireDistinct(t, "W3-1/W3-5 causal invariance", phoneA, map[string]string{
		"fixed order — greatest key [D-28 Part B, reversed]": w3PhoneRivals(t, interleavings[0])["fixed order — greatest key wins [D-28 Part B, REVERSED by D-92]"],
		"chronological max(AuthoredAt) [m1]":                 w3PhoneRivals(t, interleavings[0])["chronological max(AuthoredAt) [m1, refused by D-90/D-92]"],
	})

	type basis struct {
		auth store.AuthoredAt
		occ  int64
		src  string
	}
	bases := []basis{{a1.AuthoredAt, a1.OccurredAt, a1.TimeSource}, {b1[0].AuthoredAt, b1[0].OccurredAt, b1[0].TimeSource}}
	amend := map[store.Key]bool{a1.Key(): true, b1[0].Key(): true}

	var wantComposed, wantDump string
	for i, order := range interleavings {
		for _, perm := range [][]int{{0, 1}, {1, 0}} {
			st := w3NewStore(t)
			j := 0
			for _, e := range order {
				if amend[e.Key()] {
					b := bases[perm[j]]
					e.AuthoredAt, e.OccurredAt, e.TimeSource = b.auth, b.occ, b.src
					j++
				}
				w3Apply(t, st, e)
			}
			if p := w3Person(t, st, x); p.Phone == nil || *p.Phone != phoneA {
				t.Fatalf("W3-1/W3-5 re-locked (D-92): interleaving %d, time basis %v composes phone %v, want %q — the correction made in knowledge of the other wins under every delivery order and every clock", i, perm, p.Phone, phoneA)
			}
			composed := w3ComposedBytes(t, st)
			if wantComposed == "" {
				wantComposed = composed
			} else if composed != wantComposed {
				t.Fatalf("W3-1 (m1) / W3-5 (m10) re-locked: composed bytes changed under interleaving %d, time basis %v —\nwant %s\ngot  %s\ncomposition read a clock or an arrival order; it must be causal, then ascending key", i, perm, wantComposed, composed)
			}
			if perm[0] == 0 { // dumps compared across DELIVERY orders only, at the true time bases
				dump, err := st.DumpProjections()
				if err != nil {
					t.Fatal(err)
				}
				if wantDump == "" {
					wantDump = string(dump)
				} else if string(dump) != wantDump {
					t.Fatalf("W3-5 (m10) re-locked: projection/dump state differs between delivery interleavings 0 and %d", i)
				}
			}
		}
	}
}

// =======================================================================
// The stamp — written at the edge, from every head, never by a caller
// =======================================================================

// TestW3SeenIsStampedAtTheEdgeFromEveryHeadOfTheContest — after a
// concurrent pair (two heads) a third local amendment names BOTH heads,
// sorted ascending, and NOT the dominated row beneath them; the side-table
// row reads the same list back; the first assertion of a fact carries no
// `seen` key at all on the wire; each reference is spelled with the
// envelope's own three key literals (the wire gains exactly ONE new
// literal); and the authoring input has no field a caller could supply it
// through.
func TestW3SeenIsStampedAtTheEdgeFromEveryHeadOfTheContest(t *testing.T) {
	const phone0, phoneA, phoneB, phoneC = "555-0640", "555-0641", "555-0642", "555-0643"
	st := w3NewStore(t)
	x, _, _ := w3RosterPair(t, st, "Stamp X", "Stamp Y")
	p0 := w3AmendLocal(t, st, "site-a", 1, w3Base, PersonAmendment{PersonID: x, Phone: w3Str(phone0)})
	held := w3Events(t, st, "site-a", 1)
	a1 := w3AmendLocal(t, st, "site-a", 1, w3Base+1*w3Hour, PersonAmendment{PersonID: x, Phone: w3Str(phoneA)})
	b1 := w3AuthorRemote(t, held, "site-b", 1, w3Base+2*w3Hour, func(rs *store.Store) {
		if _, err := AmendPerson(rs, "site-b", 1, PersonAmendment{PersonID: x, Phone: w3Str(phoneB)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	w3ApplyAll(t, st, b1)
	c := w3AmendLocal(t, st, "site-a", 1, w3Base+3*w3Hour, PersonAmendment{PersonID: x, Phone: w3Str(phoneC)})

	// (a) every head, ascending, de-duplicated, and not the dominated p0.
	want := []store.Key{a1.Key(), b1[0].Key()}
	if !want[0].Less(want[1]) {
		want[0], want[1] = want[1], want[0]
	}
	if seen := w3SeenOf(t, c); !w3KeysEqual(seen, want) {
		t.Fatalf("D-92 (m-D92b): the third amendment carries seen=%v, want exactly the two heads %v — every undominated survivor, ascending, and not %v which %v had already superseded", seen, want, p0.Key(), a1.Key())
	}
	// (b) the fold/read round trip.
	if row := w3SeenRowOf(t, st, c.Key(), x); !w3KeysEqual(row, want) {
		t.Fatalf("D-92: person_amendment.seen reads back %v, want %v — the fold or the read lost the reference", row, want)
	}
	// (c) the first assertion carries no key at all (omitempty: the pre-11
	// wire shape for an author who saw nothing standing).
	var raw0 map[string]json.RawMessage
	if err := json.Unmarshal(p0.Payload, &raw0); err != nil {
		t.Fatal(err)
	}
	if _, has := raw0["seen"]; has {
		t.Fatalf("D-92: the FIRST assertion of the fact carries a `seen` key (%s) — an author who saw nothing standing must omit it, not send an empty list", raw0["seen"])
	}
	// (d) the reference's spelling is the envelope's own three key literals.
	var rawC struct {
		Seen []map[string]json.RawMessage `json:"seen"`
	}
	if err := json.Unmarshal(c.Payload, &rawC); err != nil {
		t.Fatal(err)
	}
	for _, ref := range rawC.Seen {
		if len(ref) != 3 || ref["site_id"] == nil || ref["node_epoch"] == nil || ref["seq"] == nil {
			t.Fatalf("D-92 / B-2: a seen reference is spelled %v, want exactly {site_id, node_epoch, seq} — the wire gains ONE new literal, `seen`, and reuses the envelope's key spellings", ref)
		}
	}
	// (e) a caller cannot supply it.
	if _, ok := reflect.TypeOf(PersonAmendment{}).FieldByName("Seen"); ok {
		t.Fatal("D-92: PersonAmendment offers a Seen field — the stamp is the authoring post's own honest reading of its record set and must not be supplied by a caller")
	}
	// (f) and it did its job: the third amendment stands over both heads.
	if p := w3Person(t, st, x); p.Phone == nil || *p.Phone != phoneC {
		t.Fatalf("D-92: composed phone = %v, want %q", p.Phone, phoneC)
	}
	for _, k := range []store.Key{a1.Key(), b1[0].Key(), p0.Key()} {
		if got := w3Fact(t, st, k, w3FactPhone); got.Status != Superseded || got.Winner != c.Key() {
			t.Fatalf("D-92: %v is (%v, %v), want (Superseded, %v)", k, got.Status, got.Winner, c.Key())
		}
	}
}

// =======================================================================
// Chains and the total-read guarantee
// =======================================================================

// TestW3DominanceChainsThroughDirectReferences — A ← B ← C, each link a
// direct reference: C names only B (A was already dominated when C was
// authored, so A is not a head and is not named), and C still wins over A
// at a post holding all three. A carries the greatest key AND the latest
// clock, so both rivals name it.
func TestW3DominanceChainsThroughDirectReferences(t *testing.T) {
	const phoneA, phoneB, phoneC = "555-0651", "555-0652", "555-0653"
	st := w3NewStore(t)
	x, _, roster := w3RosterPair(t, st, "Chain X", "Chain Y")
	a := w3AuthorRemote(t, roster, "site-b", 1, w3Base+5*w3Hour, func(rs *store.Store) {
		if _, err := AmendPerson(rs, "site-b", 1, PersonAmendment{PersonID: x, Phone: w3Str(phoneA)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	w3ApplyAll(t, st, a)
	b := w3AmendLocal(t, st, "site-a", 1, w3Base+1*w3Hour, PersonAmendment{PersonID: x, Phone: w3Str(phoneB)})
	c := w3AmendLocal(t, st, "site-a", 1, w3Base+2*w3Hour, PersonAmendment{PersonID: x, Phone: w3Str(phoneC)})

	if seen := w3SeenOf(t, b); !w3KeysEqual(seen, []store.Key{a[0].Key()}) {
		t.Fatalf("fixture defect: B names %v, want [%v]", seen, a[0].Key())
	}
	if seen := w3SeenOf(t, c); !w3KeysEqual(seen, []store.Key{b.Key()}) {
		t.Fatalf("D-92 stamp: C names %v, want exactly [%v] — A is already dominated by B and is not a head", seen, b.Key())
	}
	w3RequireDistinct(t, "D-92 chain", phoneC, w3PhoneRivals(t, append(w3Events(t, st, "site-a", 1), a...)))

	if p := w3Person(t, st, x); p.Phone == nil || *p.Phone != phoneC {
		t.Fatalf("D-92 (m-D92a): composed phone = %v, want %q — the end of the chain wins; A (greatest key, latest clock) is dominated through B's direct reference", p.Phone, phoneC)
	}
	for _, k := range []store.Key{a[0].Key(), b.Key()} {
		if got := w3Fact(t, st, k, w3FactPhone); got.Status != Superseded || got.Winner != c.Key() {
			t.Fatalf("D-92 chain: %v is (%v, %v), want (Superseded, %v)", k, got.Status, got.Winner, c.Key())
		}
	}
}

// TestW3UnMergeMadeInKnowledgeOfThePeersMergeTakesEffect — the merge
// pointer under D-92, in the domain (its HTTP twin is W3-6's re-ruled
// causal arm in internal/web). site-b's merge X→Y arrives; site-a's
// officer, seeing it, un-merges. The un-merge WINS: composed same_as
// absent, the snapshot resolves X to itself, Outcome(un-merge) Effective,
// Outcome(merge) Superseded by the un-merge — at site-a and at a post
// receiving both by wire in either order.
// ⟦was: at 10 this exact construction was W3-6 (b)'s LOSING fixture — "the
// un-merge does not take effect, site-b's merge is later in record order,
// permanently" — reversed by D-92 as a correction made in knowledge⟧
func TestW3UnMergeMadeInKnowledgeOfThePeersMergeTakesEffect(t *testing.T) {
	st := w3NewStore(t)
	x, y, roster := w3RosterPair(t, st, "Unmerge X", "Unmerge Y")
	merge := w3AuthorRemote(t, roster, "site-b", 1, w3Base+2*w3Hour, func(rs *store.Store) {
		if _, err := AmendPerson(rs, "site-b", 1, PersonAmendment{PersonID: x, SameAs: w3Str(y)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	w3ApplyAll(t, st, merge)
	if p := w3Person(t, st, x); p.SameAs == nil || *p.SameAs != y {
		t.Fatalf("fixture defect: the peer's merge did not compose before the un-merge (same_as=%v)", p.SameAs)
	}
	unmerge := w3AmendLocal(t, st, "site-a", 1, w3Base+1*w3Hour, PersonAmendment{PersonID: x, Clear: []string{w3FactSameAs}})
	if seen := w3SeenOf(t, unmerge); !w3KeysEqual(seen, []store.Key{merge[0].Key()}) {
		t.Fatalf("fixture defect: the un-merge names %v, want [%v]", seen, merge[0].Key())
	}
	if !unmerge.Key().Less(merge[0].Key()) {
		t.Fatalf("fixture defect: the un-merge %v is not the lesser key against %v — fixed order would then agree with the rule", unmerge.Key(), merge[0].Key())
	}

	for i, post := range []*store.Store{st, func() *store.Store {
		p := w3NewStore(t)
		w3ApplyAll(t, p, roster)
		w3Apply(t, p, unmerge)
		w3ApplyAll(t, p, merge)
		return p
	}()} {
		if p := w3Person(t, post, x); p.SameAs != nil {
			t.Fatalf("W3-6 re-ruled under D-92 (m-D92a) at post %d: composed same_as = %q, want absent — the un-merge made in knowledge of the merge takes effect; \"site-b's merge is later in record order, permanently\" was D-28 Part B", i, *p.SameAs)
		}
		if got := w3MergeMap(t, post).Resolve(x); got != x {
			t.Fatalf("D-92 at post %d: Resolve(%s) = %s, want identity — the merge snapshot reads the same rule", i, x, got)
		}
		if got := w3Fact(t, post, unmerge.Key(), w3FactSameAs); got.Status != Effective {
			t.Fatalf("D-92 at post %d: Outcome(un-merge) = %v, want Effective", i, got.Status)
		}
		if got := w3Fact(t, post, merge[0].Key(), w3FactSameAs); got.Status != Superseded || got.Winner != unmerge.Key() {
			t.Fatalf("D-92 at post %d: Outcome(peer merge) = (%v, %v), want (Superseded, %v)", i, got.Status, got.Winner, unmerge.Key())
		}
	}
}

// TestW3WireFormedSeenCycleAndMalformedSeenKeepTheReadTotal — rule 3 and
// the fold's tolerance. No honest edge mints a reference cycle, but the
// wire can carry one: two amendments each naming the other leave no
// survivor, and the read falls to the greatest key — total, deterministic
// under both apply orders, Outcome consistent with it. A malformed `seen`
// and an empty one both LAND (the fold never refuses, W3-2), read back as
// no references, and compete on key alone.
func TestW3WireFormedSeenCycleAndMalformedSeenKeepTheReadTotal(t *testing.T) {
	t.Run("cycle", func(t *testing.T) {
		origin := w3NewStore(t)
		x, _, roster := w3RosterPair(t, origin, "Cycle X", "Cycle Y")
		refB := map[string]any{"site_id": "site-b", "node_epoch": 1, "seq": 1}
		refC := map[string]any{"site_id": "site-c", "node_epoch": 1, "seq": 1}
		eb := w3WireAmendment(t, "site-b", 1, 1, w3Base+2*w3Hour, x, map[string]any{"phone": "555-0661", "seen": []any{refC}})
		ec := w3WireAmendment(t, "site-c", 1, 1, w3Base+1*w3Hour, x, map[string]any{"phone": "555-0662", "seen": []any{refB}})
		if !eb.Key().Less(ec.Key()) {
			t.Fatalf("fixture defect: %v is not less than %v", eb.Key(), ec.Key())
		}
		for i, order := range [][]store.Envelope{{eb, ec}, {ec, eb}} {
			st := w3NewStore(t)
			w3ApplyAll(t, st, roster)
			w3ApplyAll(t, st, order)
			if p := w3Person(t, st, x); p.Phone == nil || *p.Phone != "555-0662" {
				t.Fatalf("D-92 rule 3 (order %d): composed phone = %v, want the greatest key's %q — every member dominated, so the read falls to key order rather than to nothing", i, p.Phone, "555-0662")
			}
			if got := w3Fact(t, st, eb.Key(), w3FactPhone); got.Status != Superseded || got.Winner != ec.Key() {
				t.Fatalf("D-92 rule 3 (order %d): Outcome(%v) = (%v, %v), want (Superseded, %v)", i, eb.Key(), got.Status, got.Winner, ec.Key())
			}
			if got := w3Fact(t, st, ec.Key(), w3FactPhone); got.Status != Effective {
				t.Fatalf("D-92 rule 3 (order %d): Outcome(%v) = %v, want Effective", i, ec.Key(), got.Status)
			}
			if row := w3SeenRowOf(t, st, eb.Key(), x); !w3KeysEqual(row, []store.Key{ec.Key()}) {
				t.Fatalf("D-92: the wire reference was not stored verbatim: %v", row)
			}
		}
	})
	t.Run("malformed and empty land as no references", func(t *testing.T) {
		st := w3NewStore(t)
		x, _, _ := w3RosterPair(t, st, "Malformed X", "Malformed Y")
		p0 := w3AmendLocal(t, st, "site-a", 1, w3Base, PersonAmendment{PersonID: x, Phone: w3Str("555-0670")})
		garbage := w3WireAmendment(t, "site-b", 1, 1, w3Base+1*w3Hour, x, map[string]any{"phone": "555-0671", "seen": "garbage"})
		empty := w3WireAmendment(t, "site-c", 1, 1, w3Base+2*w3Hour, x, map[string]any{"phone": "555-0672", "seen": []any{}})
		w3Apply(t, st, garbage) // fatals if the fold refuses (W3-2)
		w3Apply(t, st, empty)
		for _, e := range []store.Envelope{garbage, empty} {
			if row := w3SeenRowOf(t, st, e.Key(), x); len(row) != 0 {
				t.Fatalf("D-92: %v read back seen=%v, want none — an unreadable or empty list is \"saw nothing standing\"", e.Key(), row)
			}
		}
		// Nobody names anybody: pure key order — site-c's is the greatest.
		if p := w3Person(t, st, x); p.Phone == nil || *p.Phone != "555-0672" {
			t.Fatalf("D-92: composed phone = %v, want %q (no references anywhere, so the greatest key)", p.Phone, "555-0672")
		}
		if got := w3Fact(t, st, p0.Key(), w3FactPhone); got.Status != Superseded || got.Winner != empty.Key() {
			t.Fatalf("D-92: Outcome(p0) = (%v, %v), want (Superseded, %v)", got.Status, got.Winner, empty.Key())
		}
	})
}

// =======================================================================
// The standing cross-epoch CONTROL row, kept honest at 11
// =======================================================================

// TestW3ConcurrentCrossEpochPairStillPinsTheEpochMajorWinner — the §5
// standing cross-epoch CONTROL row for the COMPOSE path at SchemaVersion 11.
// W3-1's own w3BuildCrossEpochFixture authors e2 at the same post that holds
// e1b, so at 11 e2 names e1b and its epoch-major pin is reached by
// DOMINANCE; an epoch-blind ORDER BY (m5) survives it. Here e2 arrives as a
// wire envelope on (site-a, epoch 2) naming nothing — the re-imaged post
// that authored before catching up — so e1b and e2 are BOTH heads and the
// comparator is genuinely reached: epoch N+1 / LOW seq beats epoch N / HIGH
// seq. Seq-major and (site_id, seq)-only rivals (m5) and the clock (m1) all
// name e1b.
func TestW3ConcurrentCrossEpochPairStillPinsTheEpochMajorWinner(t *testing.T) {
	const phoneA1, phoneA2, phoneB = "555-0681", "555-0682", "555-0690"
	st := w3NewStore(t)
	x, _, _ := w3RosterPair(t, st, "Cross Epoch X", "Cross Epoch Y")
	e1a := w3AmendLocal(t, st, "site-a", 1, w3Base+1*w3Hour, PersonAmendment{PersonID: x, Phone: w3Str(phoneA1)})
	e1b := w3AmendLocal(t, st, "site-a", 1, w3Base+3*w3Hour, PersonAmendment{PersonID: x, Phone: w3Str(phoneA2)})
	e2 := w3WireAmendment(t, "site-a", 2, 1, w3Base+2*w3Hour, x, map[string]any{"phone": phoneB})
	w3Apply(t, st, e2)

	if seen := w3SeenOf(t, e1b); !w3KeysEqual(seen, []store.Key{e1a.Key()}) {
		t.Fatalf("fixture defect: e1b names %v, want [%v]", seen, e1a.Key())
	}
	if seen := w3SeenOf(t, e2); len(seen) != 0 {
		t.Fatalf("fixture defect: e2 names %v, want nothing — it must be CONCURRENT with e1b for the comparator to be reached", seen)
	}
	if e2.NodeEpoch <= e1b.NodeEpoch || e2.Seq >= e1b.Seq {
		t.Fatalf("fixture defect: not (epoch N+1, LOW seq) vs (epoch N, HIGH seq): e2=%v e1b=%v", e2.Key(), e1b.Key())
	}
	evs := append(w3Events(t, st, "site-a", 1), w3Events(t, st, "site-a", 2)...)
	rivals := w3PhoneRivals(t, evs)
	delete(rivals, "fixed order — greatest key wins [D-28 Part B, REVERSED by D-92]") // the certified rule on a concurrent pair
	delete(rivals, "arrival order — last applied [m10]")                              // e2 was applied last here; W3-5 owns m10
	pick := func(less func(a, b store.Envelope) bool) string {
		best := e1a
		for _, c := range []store.Envelope{e1b, e2} {
			if less(best, c) {
				best = c
			}
		}
		return w3PhoneOf(t, best)
	}
	rivals["seq-major (epoch-blind) [m5]"] = pick(func(a, b store.Envelope) bool { return a.Seq < b.Seq })
	rivals["(site_id, seq)-only (epoch-blind) [m5]"] = pick(func(a, b store.Envelope) bool {
		if a.SiteID != b.SiteID {
			return a.SiteID < b.SiteID
		}
		return a.Seq < b.Seq
	})
	w3RequireDistinct(t, "W3-1 cross-epoch CONTROL at 11", phoneB, rivals)

	if p := w3Person(t, st, x); p.Phone == nil || *p.Phone != phoneB {
		t.Fatalf("W3-1 (m5) at 11: composed phone = %v, want the PINNED epoch-major winner %q (epoch N+1, LOW seq) on a genuinely concurrent cross-epoch pair — %q is the epoch-blind/chronological rivals' answer", p.Phone, phoneB, phoneA2)
	}
	for _, k := range []store.Key{e1a.Key(), e1b.Key()} {
		if got := w3Fact(t, st, k, w3FactPhone); got.Status != Superseded || got.Winner != e2.Key() {
			t.Fatalf("W3-1 (m5) at 11: %v is (%v, %v), want (Superseded, %v)", k, got.Status, got.Winner, e2.Key())
		}
	}
	if got := w3Fact(t, st, e2.Key(), w3FactPhone); got.Status != Effective {
		t.Fatalf("W3-1 at 11: e2 is %v, want Effective", got.Status)
	}
}
