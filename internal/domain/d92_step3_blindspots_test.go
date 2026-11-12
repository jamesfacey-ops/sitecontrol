// d92_step3_blindspots_test.go — the two witness blind spots the step-2
// tester MEASURED on the person template (reports/witness/TESTER-step2.md,
// LOUD 1 and LOUD 2) and step 3's clones inherited, closed for every
// clone (reports/witness/STEP3-BLINDSPOTS.md):
//
//  1. THE MERGE-GROUP HALF OF THE STAMP. amendmentSeen (contest.go) unions
//     the target's OWN heads with the merged GROUP's heads. Drop the union
//     and every composed value stays byte-identical — composition is
//     target-scoped — and every step-3 witness stays GREEN; only Outcome
//     sees the difference, and it tells the loser it won. Closed here, in
//     the tester's shape, for the three clones that HAVE a merge pointer
//     (company, trade, vehicle). Trip, arrival and the correction carry no
//     merge pointer and so have no group scope to miss; that is pinned
//     structurally below rather than invented.
//  2. A CLOCK IN THE TIE-BREAK. A causal-chain fixture is decided by
//     dominance and cannot see a clock fed to the tiebreak: the tester's
//     FA1 control — greatest event_log.authored_at_wall_ms among the
//     survivors, under an off-list name — passed every scramble and causal
//     witness and the W1 source scan. Only a genuinely CONCURRENT pair
//     whose clock order DISAGREES with key order kills it. Every clone gets
//     one here: the greater key carries the STRICTLY EARLIER wall clock,
//     pinned from the READ-BACK LOG (never from fixture constants), and the
//     pair is re-applied with its two time bases SWAPPED, in both delivery
//     orders, so neither clock direction can hide behind the other.
//
// KILLER MUTANTS, each PRODUCED red once against this tree before commit
// (reports/witness/STEP3-BLINDSPOTS.md):
//   - m-GROUP: amendmentSeen drops the group-scope half of the union
//     (shared by every merge-pointer carrier).
//   - m-GROUP-company / -trade / -vehicle: the authoring edge passes
//     amendmentSeen a nil merge map (group scope collapses to target scope
//     for that one carrier).
//   - m-FA1: resolveContest breaks ties by the greatest
//     authored_at_wall_ms among the survivors — a clock from event_log
//     reaching the tiebreak per store, under an off-list name.
package domain

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// =======================================================================
// Shared machinery — fixture guards only, no instrument of its own
// =======================================================================

// d92s3Fresh opens a fresh post with newStore, applies the shared history,
// then evs in the given order: "a post that received these, this way".
func d92s3Fresh(t *testing.T, newStore func(*testing.T) *store.Store, held, evs []store.Envelope) *store.Store {
	t.Helper()
	st := newStore(t)
	w3ApplyAll(t, st, held)
	w3ApplyAll(t, st, evs)
	return st
}

// d92s3LogEnvelope reads ONE envelope back from the post's event_log by
// key — the read-back log, never the fixture's own copy of the envelope.
func d92s3LogEnvelope(t *testing.T, st *store.Store, k store.Key) store.Envelope {
	t.Helper()
	evs, err := st.Events(k.SiteID, k.NodeEpoch, k.Seq, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Key() != k {
		t.Fatalf("event_log has no row at %v (read back %d row(s))", k, len(evs))
	}
	return evs[0]
}

// d92s3RequireGreaterKeyCarriesTheEarlierClock is the rule-6 guard of every
// clock witness, taken from the READ-BACK LOG: `winner` (the tiebreak's
// answer) is the GREATER key and carries the STRICTLY EARLIER
// authored_at_wall_ms, so the chronological rival — the greatest wall
// clock, the tester's FA1 control — names `loser`. Key order and clock
// order genuinely disagree; a fixture where they agree certifies nothing
// (standing rule 3).
func d92s3RequireGreaterKeyCarriesTheEarlierClock(t *testing.T, st *store.Store, winner, loser store.Key) {
	t.Helper()
	w, l := d92s3LogEnvelope(t, st, winner), d92s3LogEnvelope(t, st, loser)
	if !l.Key().Less(w.Key()) {
		t.Fatalf("fixture defect: the certified tiebreak winner %v is not the greater key against %v", winner, loser)
	}
	if w.AuthoredAt.WallMS >= l.AuthoredAt.WallMS {
		t.Fatalf("FIXTURE DEFECT: DEGENERATE — the greater key %v reads back authored_at_wall_ms=%d and the lesser key %v reads back %d; the greater key must carry the STRICTLY EARLIER clock, or a clock in the tiebreak agrees with key order and the fixture cannot see it (standing rule 3; TESTER-step2 LOUD 2)", winner, w.AuthoredAt.WallMS, loser, l.AuthoredAt.WallMS)
	}
	byClock := w
	if l.AuthoredAt.WallMS > byClock.AuthoredAt.WallMS {
		byClock = l
	}
	if byClock.Key() != loser {
		t.Fatalf("FIXTURE DEFECT: the rival 'chronological max(authored_at_wall_ms) [m1 / FA1]' names %v, want the loser %v", byClock.Key(), loser)
	}
}

// d92s3SwapTimeBases returns a and b with their time bases (AuthoredAt,
// OccurredAt, TimeSource) EXCHANGED. The payloads — and so the keys and the
// `seen` lists — are untouched: the same record set, the other clocks.
func d92s3SwapTimeBases(a, b store.Envelope) (store.Envelope, store.Envelope) {
	sa, sb := a, b
	sa.AuthoredAt, sa.OccurredAt, sa.TimeSource = b.AuthoredAt, b.OccurredAt, b.TimeSource
	sb.AuthoredAt, sb.OccurredAt, sb.TimeSource = a.AuthoredAt, a.OccurredAt, a.TimeSource
	return sa, sb
}

// d92s3Verdicts renders Outcome(k, fact) for each key as one comparable
// line, so two posts' judgements can be asserted byte-identical.
func d92s3Verdicts(t *testing.T, st *store.Store, fact string, keys ...store.Key) string {
	t.Helper()
	var b strings.Builder
	for _, k := range keys {
		o := w3Fact(t, st, k, fact)
		fmt.Fprintf(&b, "%v=(%v,%v);", k, o.Status, o.Winner)
	}
	return b.String()
}

// d92s3Deref renders an optional composed fact; nil is ABSENT, never "".
func d92s3Deref(p *string) string {
	if p == nil {
		return "<absent>"
	}
	return *p
}

// =======================================================================
// Blind spot 1 — the merge-group half of the stamp (company, trade, vehicle)
// =======================================================================

// d92s3GroupCase is one merge-pointer carrier's fixture for the merge-group
// half of the stamp, in the tester's shape: X is merged into Y by site-b,
// which then asserts `fact` on X (the greater key, the later clock); site-a
// then asserts `fact` on Y. No row targets Y's fact, so the target-scope
// heads are EMPTY and only the group scope can name site-b's row.
type d92s3GroupCase struct {
	carrier  string
	x, y     string
	roster   []store.Envelope
	remote   func(rs *store.Store)                                 // site-b, at a scratch post: merge X→Y, then assert fact on X
	local    func(st *store.Store) store.Envelope                  // site-a, at w3Base+1h: assert fact on Y
	resolve  func(t *testing.T, st *store.Store, id string) string // the carrier's merge-snapshot Resolve
	composed func(t *testing.T, st *store.Store, id string) string // the composed fact of a record (BLIND here)
	fact     string
	wantY    string // what site-a asserted on Y
}

// d92s3RunGroupCase — THE STAMP is the first instrument (it must name the
// group's head), Outcome in the group scope is the second (the amendment
// made in knowledge of the group's standing row wins regardless of
// site_id, at site-a's post and at posts receiving either stream first).
// The composed value is asserted too, labelled BLIND: it is target-scoped
// and identical whether or not the stamp named the group's head, so nobody
// mistakes it for the instrument.
func d92s3RunGroupCase(t *testing.T, st *store.Store, c d92s3GroupCase) {
	t.Helper()
	remote := w3AuthorRemote(t, c.roster, "site-b", 1, w3Base+3*w3Hour, c.remote)
	if len(remote) != 2 {
		t.Fatalf("fixture defect: site-b minted %d events, want 2 (the merge, then the %s assertion on X)", len(remote), c.fact)
	}
	m1, bx := remote[0], remote[1]
	w3ApplyAll(t, st, remote)
	if got := c.resolve(t, st, c.x); got != c.y {
		t.Fatalf("fixture: Resolve(%s) = %s, want %s — the merge must stand before Y is amended", c.x, got, c.y)
	}
	ay := c.local(st)

	// Rule 6: ay is the LESSER key and the LATER-clock loser under both
	// rivals (fixed order and chronology both name bx, as READ BACK); only
	// the stamp can make it win the group contest.
	if !ay.Key().Less(bx.Key()) {
		t.Fatalf("fixture defect: %v is not less than %v", ay.Key(), bx.Key())
	}
	lay, lbx := d92s3LogEnvelope(t, st, ay.Key()), d92s3LogEnvelope(t, st, bx.Key())
	if lbx.AuthoredAt.WallMS <= lay.AuthoredAt.WallMS {
		t.Fatalf("fixture defect: site-b's row reads back authored_at_wall_ms=%d against site-a's %d — it must carry the LATER clock so the clock rival names it", lbx.AuthoredAt.WallMS, lay.AuthoredAt.WallMS)
	}
	if seen := d92SeenOf(t, bx); len(seen) != 0 {
		t.Fatalf("fixture defect: site-b's %s row carries seen=%v, want none — it was authored over a record with no %s standing", c.fact, seen, c.fact)
	}

	// THE STAMP: the group's head, though no row targets Y's fact.
	if got := d92SeenOf(t, ay); !w3KeysEqual(got, []store.Key{bx.Key()}) {
		t.Fatalf("D-92 stamp (merge-group scope; m-GROUP / m-GROUP-%s): Y's %s amendment names %v, want exactly [%v] — the head of the MERGED GROUP's %s contest; amendmentSeen must union the group scope with the target scope, or Outcome and the stamp disagree about what the author saw", c.carrier, c.fact, got, bx.Key(), c.fact)
	}

	for i, post := range []*store.Store{st,
		d92s3Fresh(t, w3NewStore, c.roster, []store.Envelope{ay, m1, bx}), // site-a's stream first
		d92s3Fresh(t, w3NewStore, c.roster, []store.Envelope{m1, bx, ay}), // site-b's stream first (site-b's own view)
	} {
		// The instrument: Outcome in the group scope.
		if got := w3Fact(t, post, ay.Key(), c.fact); got.Status != Effective || got.Winner != ay.Key() {
			t.Fatalf("D-92 (%s, post %d): Outcome(Y's %s %v) = (%v, %v), want (Effective, itself) — the amendment made in knowledge of the group's standing row wins the group contest regardless of site_id", c.carrier, i, c.fact, ay.Key(), got.Status, got.Winner)
		}
		if got := w3Fact(t, post, bx.Key(), c.fact); got.Status != Superseded || got.Winner != ay.Key() {
			t.Fatalf("D-92 (%s, post %d): Outcome(site-b's row %v) = (%v, %v), want (Superseded, %v) — superseded by a LESSER key because that key's author saw it; a target-scope-only stamp tells this row it WON", c.carrier, i, bx.Key(), got.Status, got.Winner, ay.Key())
		}
		comp, err := Competitors(post, ay.Key())
		if err != nil {
			t.Fatal(err)
		}
		if !w3KeysEqual(comp[c.fact], []store.Key{bx.Key()}) {
			t.Fatalf("W5d-9(b) (%s, post %d): Competitors(Y's %s) = %v, want [%v] — the group is the contest", c.carrier, i, c.fact, comp[c.fact], bx.Key())
		}
		// BLIND (by construction, not by defect): composition is
		// target-scoped and reads site-a's value whether or not the stamp
		// named the group's head. Kept so the reader sees what it cannot see.
		if got := c.composed(t, post, c.y); got != c.wantY {
			t.Fatalf("composition (%s, post %d): Y.%s = %q, want %q", c.carrier, i, c.fact, got, c.wantY)
		}
	}
}

// TestW5dCompanyStampNamesTheHeadsOfTheMergedGroupNotOnlyTheTargetsOwnRows
// — the company carrier's merge-group stamp (the twin of the person
// witness of the same name). Company's one value fact is `name`.
func TestW5dCompanyStampNamesTheHeadsOfTheMergedGroupNotOnlyTheTargetsOwnRows(t *testing.T) {
	st := w3NewStore(t)
	x, y, roster := w5dCompanyPair(t, st, "Group X Ltd", "Group Y Ltd")
	d92s3RunGroupCase(t, st, d92s3GroupCase{
		carrier: "company", x: x, y: y, roster: roster, fact: w5dFactName, wantY: "Group Y Corrected Ltd",
		remote: func(rs *store.Store) {
			for _, in := range []CompanyAmendment{{CompanyID: x, SameAs: w3Str(y)}, {CompanyID: x, Name: w3Str("Group X Peer Ltd")}} {
				if _, err := AmendCompany(rs, "site-b", 1, in, 0, "default"); err != nil {
					t.Fatal(err)
				}
			}
		},
		local: func(st *store.Store) store.Envelope {
			return w5dAmendLocal(t, st, "site-a", 1, w3Base+1*w3Hour, CompanyAmendment{CompanyID: y, Name: w3Str("Group Y Corrected Ltd")})
		},
		resolve:  func(t *testing.T, st *store.Store, id string) string { return w5dMergeMap(t, st).Resolve(id) },
		composed: func(t *testing.T, st *store.Store, id string) string { return w5dCompany(t, st, id).Name },
	})
}

// TestW8TradeStampNamesTheHeadsOfTheMergedGroupNotOnlyTheTargetsOwnRows —
// the trade carrier's merge-group stamp, on the positional `phone_day`.
func TestW8TradeStampNamesTheHeadsOfTheMergedGroupNotOnlyTheTargetsOwnRows(t *testing.T) {
	st := w3NewStore(t)
	x, y, roster := w8TradePairForCausal(t, st)
	d92s3RunGroupCase(t, st, d92s3GroupCase{
		carrier: "trade", x: x, y: y, roster: roster, fact: "phone_day", wantY: "555-8302",
		remote: func(rs *store.Store) {
			for _, in := range []TradeAmendment{{TradeID: x, SameAs: w3Str(y)}, {TradeID: x, PhoneDay: w3Str("555-8301")}} {
				if _, err := AmendTrade(rs, "site-b", 1, in, 0, "default"); err != nil {
					t.Fatal(err)
				}
			}
		},
		local: func(st *store.Store) store.Envelope {
			return w8AmendTradeLocal(t, st, w3Base+1*w3Hour, TradeAmendment{TradeID: y, PhoneDay: w3Str("555-8302")})
		},
		resolve: func(t *testing.T, st *store.Store, id string) string {
			m, err := LoadTradeMergeMap(st)
			if err != nil {
				t.Fatal(err)
			}
			return m.Resolve(id)
		},
		composed: func(t *testing.T, st *store.Store, id string) string { return d92s3Deref(w8Trade(t, st, id).PhoneDay) },
	})
}

// TestW7VehicleStampNamesTheHeadsOfTheMergedGroupNotOnlyTheTargetsOwnRows
// — the vehicle carrier's merge-group stamp, on `role`.
func TestW7VehicleStampNamesTheHeadsOfTheMergedGroupNotOnlyTheTargetsOwnRows(t *testing.T) {
	st := w3NewStore(t)
	x, y, roster := w7VehiclePairForCausal(t, st)
	d92s3RunGroupCase(t, st, d92s3GroupCase{
		carrier: "vehicle", x: x, y: y, roster: roster, fact: "role", wantY: "pickup",
		remote: func(rs *store.Store) {
			for _, in := range []VehicleAmendment{{VehicleID: x, SameAs: w3Str(y)}, {VehicleID: x, Role: w3Str("van")}} {
				if _, err := AmendVehicle(rs, "site-b", 1, in, 0, "default"); err != nil {
					t.Fatal(err)
				}
			}
		},
		local: func(st *store.Store) store.Envelope {
			return w7AmendVehicleLocal(t, st, w3Base+1*w3Hour, VehicleAmendment{VehicleID: y, Role: w3Str("pickup")})
		},
		resolve: func(t *testing.T, st *store.Store, id string) string {
			m, err := LoadVehicleMergeMap(st)
			if err != nil {
				t.Fatal(err)
			}
			return ResolveVehicleID(m, id)
		},
		composed: func(t *testing.T, st *store.Store, id string) string { return d92s3Deref(w7Vehicle(t, st, id).Role) },
	})
}

// TestD92TripArrivalAndCorrectionCarryNoMergePointer — the finding, pinned
// rather than papered over: three of step 3's carriers HAVE NO MERGE GROUP,
// so there is no group scope for their stamps to miss. Their authoring
// inputs and wire payloads offer no SameAs field and no `same_as` wire key;
// AmendTrip and AmendArrival pass amendmentSeen a nil map
// (trip_amend_author.go) and the correction's contest is W4-11's target
// chain (time_correct.go). If a merge pointer is ever cloned onto one of
// them, this red says: bring the group-scope witness with it. The control
// half asserts the three carriers that DO carry one still do, so the pin
// cannot pass vacuously through a rename.
func TestD92TripArrivalAndCorrectionCarryNoMergePointer(t *testing.T) {
	for _, v := range []any{TripAmendment{}, ArrivalAmendment{}, TimeCorrection{}, TripAmended{}, ArrivalAmended{}, OccurredAtCorrected{}} {
		rt := reflect.TypeOf(v)
		if _, ok := rt.FieldByName("SameAs"); ok {
			t.Fatalf("%s offers a SameAs field — this carrier has grown a merge pointer; the merge-group half of its stamp now needs the witness company/trade/vehicle carry in this file", rt.Name())
		}
		for i := 0; i < rt.NumField(); i++ {
			if tag := rt.Field(i).Tag.Get("json"); tag == "same_as" || strings.HasPrefix(tag, "same_as,") {
				t.Fatalf("%s.%s carries the `same_as` wire key — this carrier has grown a merge pointer; bring the group-scope witness with it", rt.Name(), rt.Field(i).Name)
			}
		}
	}
	for _, v := range []any{CompanyAmendment{}, TradeAmendment{}, VehicleAmendment{}} {
		if _, ok := reflect.TypeOf(v).FieldByName("SameAs"); !ok {
			t.Fatalf("%s offers no SameAs field — the control half of this pin no longer holds; re-derive which carriers have a merge group", reflect.TypeOf(v).Name())
		}
	}
}

// =======================================================================
// Blind spot 2 — a clock in the tiebreak (every clone)
// =======================================================================

// d92s3ClockCase is one carrier's genuinely concurrent pair over the shared
// history `held`: a1 is the LESSER key carrying the LATER clock (site-a's
// own amendment), b1 the GREATER key carrying the EARLIER clock (a peer's,
// authored at a scratch post primed with `held` only) — the tiebreak's
// winner. `composed` reads the value an officer would see.
type d92s3ClockCase struct {
	carrier  string
	newStore func(*testing.T) *store.Store
	held     []store.Envelope
	a1, b1   store.Envelope
	shared   []store.Key // what both `seen` lists must name — the history both held
	fact     string
	composed func(t *testing.T, st *store.Store) string
	wantB    string // b1's asserted value — what the tiebreak certifies
	wantA    string // a1's — what a tiebreak reading the greatest clock composes
}

// d92s3RunClockCase — the instrument. At the local post, at the true time
// bases: the pair is concurrent (rule 6), the greater key carries the
// STRICTLY earlier clock AS READ BACK, and the composed value and Outcome
// are the tiebreak's. Then the pair is re-applied at fresh posts with the
// two time bases SWAPPED, in both delivery orders: the greater key now
// carries the LATER clock (asserted from the read-back log) and the
// composed value and verdicts are byte-identical to the true-basis ones. A
// tiebreak reading the greatest clock reds the first half; one reading the
// least clock reds the second; the rule reads neither.
func d92s3RunClockCase(t *testing.T, st *store.Store, c d92s3ClockCase) {
	t.Helper()
	if c.wantA == c.wantB {
		t.Fatalf("fixture defect: a1 and b1 assert the same value %q — nothing is contested", c.wantA)
	}
	d92RequireConcurrentArrangement(t, c.a1, c.b1, c.shared)
	d92s3RequireGreaterKeyCarriesTheEarlierClock(t, st, c.b1.Key(), c.a1.Key())

	if got := c.composed(t, st); got != c.wantB {
		t.Fatalf("D-92 tiebreak (m-FA1) on %s: composed = %q, want the greater site_id's %q — neither author saw the other, so site_id decides; %q is what a tiebreak reading the greatest authored_at_wall_ms composes (the tester's FA1 control, TESTER-step2 §6)", c.carrier, got, c.wantB, c.wantA)
	}
	if o := w3Fact(t, st, c.a1.Key(), c.fact); o.Status != Superseded || o.Winner != c.b1.Key() {
		t.Fatalf("D-92 tiebreak (m-FA1) on %s: Outcome(site-a's %v) = (%v, %v), want (Superseded, %v)", c.carrier, c.a1.Key(), o.Status, o.Winner, c.b1.Key())
	}
	if o := w3Fact(t, st, c.b1.Key(), c.fact); o.Status != Effective || o.Winner != c.b1.Key() {
		t.Fatalf("D-92 tiebreak (m-FA1) on %s: Outcome(the peer's %v) = (%v, %v), want (Effective, itself)", c.carrier, c.b1.Key(), o.Status, o.Winner)
	}
	want := d92s3Verdicts(t, st, c.fact, c.a1.Key(), c.b1.Key())

	sa, sb := d92s3SwapTimeBases(c.a1, c.b1)
	for i, order := range [][]store.Envelope{{sa, sb}, {sb, sa}} {
		post := d92s3Fresh(t, c.newStore, c.held, order)
		la, lb := d92s3LogEnvelope(t, post, c.a1.Key()), d92s3LogEnvelope(t, post, c.b1.Key())
		if lb.AuthoredAt.WallMS <= la.AuthoredAt.WallMS {
			t.Fatalf("fixture defect: after the swap the greater key %v reads back authored_at_wall_ms=%d and the lesser %v reads back %d — the swap must give the greater key the LATER clock", c.b1.Key(), lb.AuthoredAt.WallMS, c.a1.Key(), la.AuthoredAt.WallMS)
		}
		if got := c.composed(t, post); got != c.wantB {
			t.Fatalf("D-92 tiebreak (m-FA1 inverted) on %s: with the time bases SWAPPED, delivery order %d, composed = %q, want %q — the same record set must compose the same value under either clock assignment; composition read a clock", c.carrier, i, got, c.wantB)
		}
		if got := d92s3Verdicts(t, post, c.fact, c.a1.Key(), c.b1.Key()); got != want {
			t.Fatalf("D-92 tiebreak on %s: with the time bases SWAPPED, delivery order %d, the verdicts changed — want %s, got %s", c.carrier, i, want, got)
		}
	}
}

// TestW5dConcurrentCompanyPairIsClockFreeUnderEitherTimeBasis — company.
func TestW5dConcurrentCompanyPairIsClockFreeUnderEitherTimeBasis(t *testing.T) {
	const name0, nameA, nameB = "Clock 0 Ltd", "Clock A Ltd", "Clock B Ltd"
	st := w3NewStore(t)
	x, _, _ := w5dCompanyPair(t, st, "Clock X Ltd", "Clock Y Ltd")
	p0 := w5dAmendLocal(t, st, "site-a", 1, w3Base, CompanyAmendment{CompanyID: x, Name: w3Str(name0)})
	held := w3Events(t, st, "site-a", 1)
	a1 := w5dAmendLocal(t, st, "site-a", 1, w3Base+3*w3Hour, CompanyAmendment{CompanyID: x, Name: w3Str(nameA)})
	b1 := d92Remote(t, held, "site-b", w3Base+1*w3Hour, func(rs *store.Store) {
		if _, err := AmendCompany(rs, "site-b", 1, CompanyAmendment{CompanyID: x, Name: w3Str(nameB)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	w3Apply(t, st, b1)
	d92s3RunClockCase(t, st, d92s3ClockCase{
		carrier: "company", newStore: w3NewStore, held: held, a1: a1, b1: b1,
		shared: []store.Key{p0.Key()}, fact: w5dFactName, wantB: nameB, wantA: nameA,
		composed: func(t *testing.T, st *store.Store) string { return w5dCompany(t, st, x).Name },
	})
}

// TestW8ConcurrentTradePairIsClockFreeUnderEitherTimeBasis — trade, on the
// positional `phone_night`.
func TestW8ConcurrentTradePairIsClockFreeUnderEitherTimeBasis(t *testing.T) {
	const n0, nA, nB = "555-8400", "555-8401", "555-8402"
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
	d92s3RunClockCase(t, st, d92s3ClockCase{
		carrier: "trade", newStore: w3NewStore, held: held, a1: a1, b1: b1,
		shared: []store.Key{p0.Key()}, fact: "phone_night", wantB: nB, wantA: nA,
		composed: func(t *testing.T, st *store.Store) string { return d92s3Deref(w8Trade(t, st, x).PhoneNight) },
	})
}

// TestW7ConcurrentVehiclePairIsClockFreeUnderEitherTimeBasis — vehicle, on
// `role`.
func TestW7ConcurrentVehiclePairIsClockFreeUnderEitherTimeBasis(t *testing.T) {
	const r0, rA, rB = "bus", "van", "pickup"
	st := w3NewStore(t)
	x, _, _ := w7VehiclePairForCausal(t, st)
	p0 := w7AmendVehicleLocal(t, st, w3Base, VehicleAmendment{VehicleID: x, Role: w3Str(r0)})
	held := w3Events(t, st, "site-a", 1)
	a1 := w7AmendVehicleLocal(t, st, w3Base+3*w3Hour, VehicleAmendment{VehicleID: x, Role: w3Str(rA)})
	b1 := d92Remote(t, held, "site-b", w3Base+1*w3Hour, func(rs *store.Store) {
		if _, err := AmendVehicle(rs, "site-b", 1, VehicleAmendment{VehicleID: x, Role: w3Str(rB)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	w3Apply(t, st, b1)
	d92s3RunClockCase(t, st, d92s3ClockCase{
		carrier: "vehicle", newStore: w3NewStore, held: held, a1: a1, b1: b1,
		shared: []store.Key{p0.Key()}, fact: "role", wantB: rB, wantA: rA,
		composed: func(t *testing.T, st *store.Store) string { return d92s3Deref(w7Vehicle(t, st, x).Role) },
	})
}

// TestW9aConcurrentTripPairIsClockFreeUnderEitherTimeBasis — trip, on the
// scalar `driver_person_id`.
func TestW9aConcurrentTripPairIsClockFreeUnderEitherTimeBasis(t *testing.T) {
	st := w9aNewStore(t)
	trip, _ := w9aCausalWorld(t, st, "TRK-K1")
	p0 := w9aAmendTripLocal(t, st, w3Base, TripAmendment{TripID: trip.TripID, DriverPersonID: w3Str("drv-okafor")})
	held := w3Events(t, st, "site-a", 1)
	a1 := w9aAmendTripLocal(t, st, w3Base+3*w3Hour, TripAmendment{TripID: trip.TripID, DriverPersonID: w3Str("drv-vance")})
	b1 := d92Remote(t, held, "site-b", w3Base+1*w3Hour, func(rs *store.Store) {
		if _, err := AmendTrip(rs, "site-b", 1, TripAmendment{TripID: trip.TripID, DriverPersonID: w3Str("drv-start")}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	w3Apply(t, st, b1)
	d92s3RunClockCase(t, st, d92s3ClockCase{
		carrier: "trip", newStore: w9aNewStore, held: held, a1: a1, b1: b1,
		shared: []store.Key{p0.Key()}, fact: factTripDriver, wantB: "drv-start", wantA: "drv-vance",
		composed: func(t *testing.T, st *store.Store) string { return w9aComposed(t, st, trip.TripID).Trip.DriverPersonID },
	})
}

// TestW9aConcurrentArrivalPairIsClockFreeUnderEitherTimeBasis — arrival, on
// `vehicle_entered`. The arrival carrier had no concurrent fixture at all
// before this one. Its peer is site-a's own second node_epoch, not another
// site_id: D-87/ownership.go gates arrival_amended to trip_arrival's own
// tag (a resolver refusal, unconditional), so no other site could ever
// author a rival correction over this record — the greater NodeEpoch is
// the tiebreak's dimension here in exactly the role the greater site_id
// plays for every ungated carrier's version of this witness (w9aOwnSiteRemote).
func TestW9aConcurrentArrivalPairIsClockFreeUnderEitherTimeBasis(t *testing.T) {
	st := w9aNewStore(t)
	w9aSeedPersons(t, st, "site-a", 1, "p-chen")
	arr := w9aUnsolicited(t, st, "site-a", 1, "TRK-K2", "site-b", []string{"p-chen"})
	held := w3Events(t, st, "site-a", 1)
	st.Clock = w3Reading(w3Base + 3*w3Hour)
	a1, err := AmendArrival(st, "site-a", 1, ArrivalAmendment{ArrivalID: arr.ArrivalID, VehicleEntered: w3Str("TRK-K2A")}, 0, "default")
	if err != nil {
		t.Fatalf("fixture arrival amendment refused: %v", err)
	}
	c1 := w9aOwnSiteRemote(t, held, "site-a", 2, w3Base+1*w3Hour, func(rs *store.Store) {
		if _, err := AmendArrival(rs, "site-a", 2, ArrivalAmendment{ArrivalID: arr.ArrivalID, VehicleEntered: w3Str("TRK-K2C")}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	w3Apply(t, st, c1)
	d92s3RunClockCase(t, st, d92s3ClockCase{
		carrier: "arrival", newStore: w9aNewStore, held: held, a1: a1, b1: c1,
		shared: nil, fact: factArrivalVehicle, wantB: "TRK-K2C", wantA: "TRK-K2A",
		composed: func(t *testing.T, st *store.Store) string {
			eff, err := EffectiveArrival(st, arr.ArrivalID)
			if err != nil || eff == nil {
				t.Fatalf("EffectiveArrival: %v (%v)", err, eff)
			}
			return d92s3Deref(eff.Arrival.VehicleEntered)
		},
	})
}

// TestW4ConcurrentCorrectionPairIsClockFreeUnderEitherTimeBasis — the
// correction carrier: the composed value is the resolved instant and its
// winner. The loser carries the greater corrected instant AND the later act
// clock (both rivals recomputed from the log by w4RequireOrderRivalsDisagree
// name it); the swap exchanges the act clocks only — the corrected instants
// are the asserted values, not a time basis.
func TestW4ConcurrentCorrectionPairIsClockFreeUnderEitherTimeBasis(t *testing.T) {
	const (
		localMS = w4OriginalMS + 2*w3Hour // the loser's — the greater instant
		peerMS  = w4OriginalMS + 1*w3Hour // the tiebreak winner's
	)
	st := w3NewStore(t)
	target := w4Target(t, st, "site-a", 1, "Clock Basis Target")
	held := w3Events(t, st, "site-a", 1)
	local := w4CorrectLocal(t, st, "site-a", 1, w3Base+3*w3Hour, target.Key(), localMS, "Ida Aputi")
	peer := w4RemoteCorrect(t, held, "site-b", w3Base+1*w3Hour, target.Key(), peerMS, "Pat Bell")
	w3Apply(t, st, peer)
	w4RequireOrderRivalsDisagree(t, st, target.Key(), peerMS, [][2]any{{"site-a", int64(1)}, {"site-b", int64(1)}})
	resolved := func(t *testing.T, st *store.Store) string {
		eff, chain, err := w4Resolve(t, st, target.Key())
		if err != nil {
			t.Fatalf("ResolveOccurredAt: %v", err)
		}
		w4RequireChainStatuses(t, chain, eff.Winner)
		return fmt.Sprintf("%d by %v", eff.InstantUTCms, eff.Winner)
	}
	d92s3RunClockCase(t, st, d92s3ClockCase{
		carrier: "correction", newStore: w3NewStore, held: held, a1: local, b1: peer,
		shared: nil, fact: w4FactOccurredAt,
		wantB: fmt.Sprintf("%d by %v", peerMS, peer.Key()), wantA: fmt.Sprintf("%d by %v", localMS, local.Key()),
		composed: resolved,
	})
}
