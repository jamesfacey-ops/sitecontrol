// w8_causal_compose_test.go — the trade carrier under [[D-92]] at
// SchemaVersion 11 (step 3: the causal `seen` reference cloned onto trade
// SECOND, after company). The fixtures are the trade twins of
// w3_causal_compose_test.go's; the rule is resolveContest, read once.
// The contested fact is a POSITIONAL handset number, so the witness also
// reads the composed value through TradePhone — the ONE positional resolver
// — because that is the surface an officer dials.
//
// KILLER MUTANTS, each PRODUCED red once against this tree before commit
// (reports/witness/STEP3-CLONE.md): m-D92a (shared), m-D92b-trade (AmendTrade
// drops the stamp), m-D92c-trade (composeTrade falls back to last key),
// m-D92d-trade (tradeMergeMapFromRows falls back to last key).
package domain

import (
	"testing"

	"sitecontrol/internal/store"
)

func w8TradePairForCausal(t *testing.T, st *store.Store) (idA, idB string, stream []store.Envelope) {
	t.Helper()
	a, err := CreateTrade(st, "site-a", 1, "Welder", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	b, err := CreateTrade(st, "site-a", 1, "Rigger", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	return a.TradeID, b.TradeID, w3Events(t, st, "site-a", 1)
}

func w8AmendTradeLocal(t *testing.T, st *store.Store, clockMS int64, in TradeAmendment) store.Envelope {
	t.Helper()
	st.Clock = w3Reading(clockMS)
	env, err := AmendTrade(st, "site-a", 1, in, 0, "default")
	if err != nil {
		t.Fatalf("fixture trade amendment refused: %v", err)
	}
	return env
}

func w8Trade(t *testing.T, st *store.Store, id string) *Trade {
	t.Helper()
	tr, err := TradeByID(st, id)
	if err != nil {
		t.Fatal(err)
	}
	if tr == nil {
		t.Fatalf("trade %s: no row", id)
	}
	return tr
}

func w8SeenRowOf(t *testing.T, st *store.Store, k store.Key, tradeID string) []store.Key {
	t.Helper()
	rows, err := readTradeAmendmentsFor(st, tradeID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Key == k {
			return r.Seen
		}
	}
	t.Fatalf("no trade_amendment row at %v for %s", k, tradeID)
	return nil
}

// TestW8CausalTradeCorrectionMadeInKnowledgeWins — site-b (greater site_id,
// later clock) asserts the day handset over the bare record; site-a
// receives it and corrects it. The lesser-keyed correction wins: composed
// phone_day, TradePhone(ShiftDay), Outcome both ways, and a third post in
// either delivery order.
func TestW8CausalTradeCorrectionMadeInKnowledgeWins(t *testing.T) {
	const dayB, dayA = "555-8101", "555-8102"
	st := w3NewStore(t)
	x, _, roster := w8TradePairForCausal(t, st)

	b1 := d92Remote(t, roster, "site-b", w3Base+3*w3Hour, func(rs *store.Store) {
		if _, err := AmendTrade(rs, "site-b", 1, TradeAmendment{TradeID: x, PhoneDay: w3Str(dayB)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	w3Apply(t, st, b1)
	a1 := w8AmendTradeLocal(t, st, w3Base+1*w3Hour, TradeAmendment{TradeID: x, PhoneDay: w3Str(dayA)})
	d92RequireCausalArrangement(t, a1, b1)

	tr := w8Trade(t, st, x)
	if tr.PhoneDay == nil || *tr.PhoneDay != dayA {
		t.Fatalf("D-92 (m-D92a/m-D92c-trade): composed phone_day = %v, want site-a's %q — the correction made IN KNOWLEDGE of site-b's wins regardless of site_id; %q is D-28 Part B's answer", tr.PhoneDay, dayA, dayB)
	}
	if got := TradePhone(*tr, ShiftDay); got == nil || *got != dayA {
		t.Fatalf("D-92 / W8-4: the positional resolver dials %v, want the composed %q", got, dayA)
	}
	if ob := w3Fact(t, st, b1.Key(), "phone_day"); ob.Status != Superseded || ob.Winner != a1.Key() {
		t.Fatalf("D-92: site-b's assertion is (%v, winner %v), want (Superseded, %v)", ob.Status, ob.Winner, a1.Key())
	}
	if oa := w3Fact(t, st, a1.Key(), "phone_day"); oa.Status != Effective {
		t.Fatalf("D-92: site-a's correction is %v, want Effective", oa.Status)
	}
	for i, order := range [][]store.Envelope{{a1, b1}, {b1, a1}} {
		third := w3NewStore(t)
		w3ApplyAll(t, third, roster)
		w3ApplyAll(t, third, order)
		if q := w8Trade(t, third, x); q.PhoneDay == nil || *q.PhoneDay != dayA {
			t.Fatalf("W3-5 under D-92: a third post receiving order %d composes phone_day %v, want %q", i, q.PhoneDay, dayA)
		}
	}
}

// TestW8TradeSeenIsStampedFromEveryHeadAndConcurrentEditsTiebreak — a
// concurrent pair over p0 (both `seen` name p0, neither the other) falls to
// the site_id tiebreak; a third local amendment then names BOTH heads,
// ascending; the row reads the list back; the first assertion omits the
// key; TradeAmendment has no Seen field.
func TestW8TradeSeenIsStampedFromEveryHeadAndConcurrentEditsTiebreak(t *testing.T) {
	const n0, nA, nB, nC = "555-8200", "555-8201", "555-8202", "555-8203"
	st := w3NewStore(t)
	x, _, _ := w8TradePairForCausal(t, st)
	p0 := w8AmendTradeLocal(t, st, w3Base, TradeAmendment{TradeID: x, PhoneNight: w3Str(n0)})
	held := w3Events(t, st, "site-a", 1)
	a1 := w8AmendTradeLocal(t, st, w3Base+3*w3Hour, TradeAmendment{TradeID: x, PhoneNight: w3Str(nA)})
	b1 := d92Remote(t, held, "site-b", w3Base+1*w3Hour, func(rs *store.Store) {
		if _, err := AmendTrade(rs, "site-b", 1, TradeAmendment{TradeID: x, PhoneNight: w3Str(nB)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	w3Apply(t, st, b1)
	d92RequireConcurrentArrangement(t, a1, b1, []store.Key{p0.Key()})
	if tr := w8Trade(t, st, x); tr.PhoneNight == nil || *tr.PhoneNight != nB {
		t.Fatalf("D-92 tiebreak: composed phone_night = %v, want the greater site_id's %q (chronology and the lesser key both say %q)", tr.PhoneNight, nB, nA)
	}
	if got := w3Fact(t, st, a1.Key(), "phone_night"); got.Status != Superseded || got.Winner != b1.Key() {
		t.Fatalf("D-92 tiebreak: site-a's concurrent edit is (%v, %v), want (Superseded, %v)", got.Status, got.Winner, b1.Key())
	}

	c := w8AmendTradeLocal(t, st, w3Base+4*w3Hour, TradeAmendment{TradeID: x, PhoneNight: w3Str(nC)})
	want := []store.Key{a1.Key(), b1.Key()}
	if !want[0].Less(want[1]) {
		want[0], want[1] = want[1], want[0]
	}
	if seen := d92SeenOf(t, c); !w3KeysEqual(seen, want) {
		t.Fatalf("D-92 (m-D92b-trade): the third amendment carries seen=%v, want exactly the two heads %v", seen, want)
	}
	if row := w8SeenRowOf(t, st, c.Key(), x); !w3KeysEqual(row, want) {
		t.Fatalf("D-92: trade_amendment.seen reads back %v, want %v", row, want)
	}
	if d92HasSeenKey(t, p0) {
		t.Fatalf("D-92: the FIRST assertion carries a `seen` key — an author who saw nothing standing must omit it")
	}
	d92RequireNoSeenField(t, TradeAmendment{})
	if tr := w8Trade(t, st, x); tr.PhoneNight == nil || *tr.PhoneNight != nC {
		t.Fatalf("D-92: composed phone_night = %v, want %q", tr.PhoneNight, nC)
	}
}

// TestW8TradeUnMergeMadeInKnowledgeOfThePeersMergeTakesEffect — the merge
// pointer on the trade carrier: the un-merge made in knowledge of site-b's
// merge wins at the lesser key, and LoadTradeMergeMap (mergeMapFromEdges
// over the effective edges) resolves X to itself.
func TestW8TradeUnMergeMadeInKnowledgeOfThePeersMergeTakesEffect(t *testing.T) {
	st := w3NewStore(t)
	x, y, roster := w8TradePairForCausal(t, st)
	merge := d92Remote(t, roster, "site-b", w3Base+2*w3Hour, func(rs *store.Store) {
		if _, err := AmendTrade(rs, "site-b", 1, TradeAmendment{TradeID: x, SameAs: w3Str(y)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	w3Apply(t, st, merge)
	if tr := w8Trade(t, st, x); tr.SameAs == nil || *tr.SameAs != y {
		t.Fatalf("fixture defect: the peer's merge did not compose before the un-merge (same_as=%v)", tr.SameAs)
	}
	unmerge := w8AmendTradeLocal(t, st, w3Base+1*w3Hour, TradeAmendment{TradeID: x, Clear: []string{"same_as"}})
	d92RequireCausalArrangement(t, unmerge, merge)

	for i, post := range []*store.Store{st, func() *store.Store {
		p := w3NewStore(t)
		w3ApplyAll(t, p, roster)
		w3Apply(t, p, unmerge)
		w3Apply(t, p, merge)
		return p
	}()} {
		if tr := w8Trade(t, post, x); tr.SameAs != nil {
			t.Fatalf("D-92 (m-D92a/m-D92c-trade) at post %d: composed same_as = %q, want absent", i, *tr.SameAs)
		}
		m, err := LoadTradeMergeMap(post)
		if err != nil {
			t.Fatal(err)
		}
		if got := m.Resolve(x); got != x {
			t.Fatalf("D-92 (m-D92d-trade) at post %d: Resolve(%s) = %s, want identity — the trade merge snapshot reads the same rule", i, x, got)
		}
		if got := w3Fact(t, post, unmerge.Key(), "same_as"); got.Status != Effective {
			t.Fatalf("D-92 at post %d: Outcome(un-merge) = %v, want Effective", i, got.Status)
		}
		if got := w3Fact(t, post, merge.Key(), "same_as"); got.Status != Superseded || got.Winner != unmerge.Key() {
			t.Fatalf("D-92 at post %d: Outcome(peer merge) = (%v, %v), want (Superseded, %v)", i, got.Status, got.Winner, unmerge.Key())
		}
	}
}
