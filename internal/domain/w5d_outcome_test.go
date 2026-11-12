// w5d_outcome_test.go — W5d-11 (the registry row, POSITIVE: Outcome on a
// REAL company_amended key, plus the coverage EQUALITY tooth), W5d-19
// (= W3-9, on DR-2(a)'s {name, same_as} two-fact construction), W5d-20
// (= W3-10, merge-induced supersession on `name` per DR-2), W5d-21
// (= W3-16, the deferred case at rest), W5d-10 (the T-A agreement oracle
// over W5d's OWN fixtures) and W5d-9 (b)/(c)/(d) — the competitor API, the
// FactOutcome byte-stability census, and the one-shared-pass agreement.
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, W5d LOCKED 2026-08-31.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD — that IS these tests' clean-tree red.
//
// KILLER MUTANTS: company-m18 (unregistered carrier → empty success instead
// of error — the planted-type control), (m8) (company_amended registered in
// carrierRegistry while Outcome's dispatch stays HARDCODED to
// evtPersonAmended — the failure disguised as outcomeMiss's sanctioned
// "registered carrier … has no amendment record" arm, which is exactly why
// this criterion is positive), W3-m15 (event-level outcome), W3-m16
// (same-target-only resolver, blind to assertion sets joined by a merge),
// W3-m22 (status stamped at write), W3-m17/W3-m5 (comparator/ordering
// drift, via the oracle), and a planted FIFTH FIELD on FactOutcome (the
// cheapest wrong build for W5d-9, which W4's lock depends on refusing).
package domain

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// w5dPlantedCompanyType is a carrier-CLASS name (`_amended` suffix, so the
// W3-12 filter enumerates it) that is never registered.
const w5dPlantedCompanyType = "gizmo_amended"

// --- W5d-11 -------------------------------------------------------------

// w5dOutcomePin is one pinned FactOutcome expectation, as literals.
type w5dOutcomePin struct {
	Fact     string
	Asserted string
	Status   Status
	Winner   store.Key
}

// w5dAssertOutcomePins compares Outcome(k) against a PINNED LITERAL SET —
// the fact vocabulary in canonical order, the Asserted renderings, the
// Status values and the Winner keys, all as literals.
func w5dAssertOutcomePins(t *testing.T, st *store.Store, criterion string, k store.Key, want []w5dOutcomePin) {
	t.Helper()
	outs, err := Outcome(st, k)
	if err != nil {
		// (m8)'s disguise: with the registry row landed but no dispatch
		// branch, Outcome falls through to the PERSON amendment table,
		// matches nothing, and outcomeMiss returns the SANCTIONED
		// "registered carrier … has no amendment record" error. A
		// negative-only control passes on exactly that build.
		if strings.Contains(err.Error(), "has no amendment record") {
			t.Fatalf("%s (m8): Outcome on a REAL company_amended key %v returned the SANCTIONED error %q — company_amended is registered but Outcome's dispatch is still hardcoded to evtPersonAmended, so the mechanism is unbuilt and its failure is disguised as one of the two permitted error paths", criterion, k, err)
		}
		if strings.Contains(err.Error(), "not a registered carrier") {
			t.Fatalf("%s (company-m18): Outcome on a REAL company_amended key %v says the type is not a registered carrier: %v", criterion, k, err)
		}
		t.Fatalf("%s: Outcome(%v) errored on a registered, present company amendment: %v — neither sanctioned error path is reachable here", criterion, k, err)
	}
	var got []w5dOutcomePin
	for _, o := range outs {
		got = append(got, w5dOutcomePin{Fact: o.Fact, Asserted: o.Asserted, Status: o.Status, Winner: o.Winner})
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: Outcome(%v) = %+v, want the PINNED literal set %+v — the fact vocabulary in canonical order, the Asserted renderings, the Status values and the Winner keys are all pinned", criterion, k, got, want)
	}
}

// TestW5dCompanyAmendedIsRegisteredAndOutcomeAnswersPositively — W5d-11,
// made POSITIVE at the lock. `Outcome` on a REAL `company_amended` key
// returns its per-fact outcomes against a pinned literal set, and NEITHER
// sanctioned error path is reachable for a registered, present company
// amendment. Kills (m8) — the mutant revision 5's negative-only control
// could not see.
func TestW5dCompanyAmendedIsRegisteredAndOutcomeAnswersPositively(t *testing.T) {
	const pinnedName = "Registry Composed Ltd"
	if !IsRegisteredCarrier(w5dEvtCompanyAmended) {
		t.Fatalf("W5d-11: IsRegisteredCarrier(%q) = false — company_amended must join the §4.4 registry with the three registration goods (DR-7's third class pair)", w5dEvtCompanyAmended)
	}

	st := w3NewStore(t)
	x, y, _ := w5dCompanyPair(t, st, "Registry X Ltd", "Registry Y Ltd")

	// DR-2(a)'s ONLY lawful two-fact construction: company has exactly one
	// content fact, so {name, same_as} in ONE event is the only way two
	// facts are asserted together.
	ev := w5dAmendLocal(t, st, "site-a", 1, w3Base+1*w3Hour,
		CompanyAmendment{CompanyID: x, Name: w3Str(pinnedName), SameAs: w3Str(y)})

	w5dAssertOutcomePins(t, st, "W5d-11", ev.Key(), []w5dOutcomePin{
		{Fact: "name", Asserted: pinnedName, Status: Effective, Winner: ev.Key()},
		{Fact: "same_as", Asserted: y, Status: Effective, Winner: ev.Key()},
	})

	// The un-merge's canonical Asserted rendering is the registry's own
	// second good, and it is what lets the surface say "the merge stands"
	// in the un-merge's own words (P-3 §5).
	un := w5dAmendLocal(t, st, "site-a", 1, w3Base+2*w3Hour,
		CompanyAmendment{CompanyID: x, Clear: []string{w5dFactSameAs}})
	w5dAssertOutcomePins(t, st, "W5d-11 (un-merge rendering)", un.Key(), []w5dOutcomePin{
		{Fact: "same_as", Asserted: "un-merge", Status: Effective, Winner: un.Key()},
	})
}

// TestW5dOutcomeUnregisteredCarrierErrors — W5d-11's CONTROL, retained
// BESIDE the positive assertion, never instead of it: a planted
// unregistered `_amended` type returns a NON-NIL error from Outcome, never
// ([]FactOutcome{}, nil). Kills company-m18.
func TestW5dOutcomeUnregisteredCarrierErrors(t *testing.T) {
	st := w3NewStore(t)
	w5dCompanyPair(t, st, "Unregistered X Ltd", "Unregistered Y Ltd")
	planted := w5dWireAmendment(t, "site-b", 1, 1, w3Base, "company-nobody", nil)
	planted.Payload = []byte(`{"type":"` + w5dPlantedCompanyType + `","company_id":"company-nobody","name":"Planted Ltd"}`)
	w3Apply(t, st, planted)

	outs, err := Outcome(st, planted.Key())
	if err == nil {
		t.Fatalf("W5d-11 CONTROL FAILED (company-m18): Outcome on the unregistered type %q returned (%v, nil) — an empty success is the silent-OK shipped inside the mechanism built to kill it", w5dPlantedCompanyType, outs)
	}
	if !strings.Contains(err.Error(), "not a registered carrier") {
		t.Fatalf("W5d-11 CONTROL: Outcome on the unregistered type errored with %q, want the §4.4 unregistered-carrier arm — a different error means the control is not exercising the path it claims", err)
	}
	if !IsRegisteredCarrier(w5dEvtCompanyAmended) {
		t.Fatalf("W5d-11 CONTROL: company_amended is not registered, so this control cannot discriminate")
	}
}

// TestW5dCarrierCoverageEnumeratesCompanyAmended — W5d-11's coverage half.
// The tooth is EQUALITY on the filtered inventory, not ⊇: under ⊇ alone an
// inventory that DROPS company_amended passes trivially, so "the fixture
// must author one" is necessary and not sufficient (M12). The landed W4
// assertion (w4_registry_test.go:150–156) is EXTENDED by one row rather
// than re-invented — the same helpers, the same judgement function, a
// three-row want. The coverage fixture AUTHORS a company_amended, so the
// live-log enumeration actually observes it (DR-1's degeneracy).
func TestW5dCarrierCoverageEnumeratesCompanyAmended(t *testing.T) {
	// --- CONTROL 1 (retained from W4): a planted unregistered type must be
	// flagged by the SAME judgement function the clean pass runs. ---------
	got := w4CoverageProblems([]string{w5dEvtCompanyAmended, w5dPlantedCompanyType}, nil, IsRegisteredCarrier)
	if len(got) != 1 || !strings.Contains(got[0], w5dPlantedCompanyType) {
		t.Fatalf("W5d-11 CONTROL 1 FAILED: the coverage judgement did not flag the planted unregistered type %q (got %v)", w5dPlantedCompanyType, got)
	}
	// --- CONTROL 2: an inventory that DROPS company_amended must still
	// pass the ⊇ judgement — which is precisely why the equality assertion
	// below is the load-bearing one. ------------------------------------
	if p := w4CoverageProblems([]string{w3EvtPersonAmended}, nil, IsRegisteredCarrier); len(p) != 0 {
		t.Fatalf("W5d-11 CONTROL 2 FAILED: the ⊇ coverage judgement reported problems (%v) on an inventory that merely OMITS company_amended — the control's whole point is that ⊇ cannot see the omission, so the equality assertion is required", p)
	}

	// --- the clean pass over a LIVE log that authors all three families --
	const correctedMS = w3Base - 9*w3Hour
	st := w3NewStore(t)
	x, y, _ := w5dCompanyPair(t, st, "Coverage X Ltd", "Coverage Y Ltd")
	w5dAmendLocal(t, st, "site-a", 1, w3Base+1*w3Hour,
		CompanyAmendment{CompanyID: x, SameAs: w3Str(y)})
	p, _, _ := w3RosterPair(t, st, "Coverage W5d A", "Coverage W5d B")
	w3AmendLocal(t, st, "site-a", 1, w3Base+2*w3Hour,
		PersonAmendment{PersonID: p, Phone: w3Str("555-0781")})
	target := w4Target(t, st, "site-a", 1, "Coverage W5d Target")
	w4CorrectLocal(t, st, "site-a", 1, w3Base+3*w3Hour, target.Key(), correctedMS, "Riley Okpik")

	inv := w3AmendmentFamilyTypes(t, st, [][2]interface{}{{"site-a", int64(1)}})
	want := []string{w4EvtCorrected, w3EvtPersonAmended, w5dEvtCompanyAmended}
	sort.Strings(want)
	if !reflect.DeepEqual(inv, want) {
		t.Fatalf("W5d-11: filtered inventory = %v, want exactly %v at W5d build time — an inventory missing this chunk's own carrier makes the coverage pass vacuous (M12; W4-13(a)'s landed equality, extended by one row)", inv, want)
	}
	if problems := w4CoverageProblems(inv, []string{}, IsRegisteredCarrier); len(problems) != 0 {
		t.Fatalf("W5d-11: coverage problems on the live log: %v — company_amended must be registered with the three §4.4 goods, and the exemption list is EMPTY at W5d", problems)
	}
}

// --- W5d-19 (= W3-9): per-fact outcome, never per-event -----------------

// w5dTwoFactFixture is DR-2(a)'s construction: ONE company_amended
// asserting BOTH {name, same_as}, with a later-keyed peer event taking
// exactly ONE of the two facts away — so the per-fact outcomes must DIFFER.
type w5dTwoFactFixture struct {
	st         *store.Store
	x, y, z    string
	twoFact    store.Envelope // site-a: {name, same_as→y}
	peerMerge  store.Envelope // site-b: {same_as→z} — takes same_as ONLY
	pinnedName string
}

func w5dBuildTwoFact(t *testing.T) w5dTwoFactFixture {
	t.Helper()
	fx := w5dTwoFactFixture{pinnedName: "Two Fact Ltd"}
	origin := w3NewStore(t)
	x, y, _ := w5dCompanyPair(t, origin, "Two Fact X Ltd", "Two Fact Y Ltd")
	z, err := CreateCompany(origin, "site-a", 1, "Two Fact Z Ltd", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	fx.x, fx.y, fx.z = x, y, z.CompanyID
	roster := w3Events(t, origin, "site-a", 1)

	// site-a's two-fact amendment, authored on a primed scratch edge.
	fx.twoFact = w3AuthorRemote(t, roster, "site-a", 1, w3Base+2*w3Hour, func(rs *store.Store) {
		if _, err := AmendCompany(rs, "site-a", 1, CompanyAmendment{
			CompanyID: fx.x, Name: w3Str(fx.pinnedName), SameAs: w3Str(fx.y),
		}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})[0]
	// site-b's merge, authored at a post that has NEVER seen site-a's (so
	// the standing-prior-merge refusal does not fire — two partitioned
	// posts can legally author competing merges).
	fx.peerMerge = w3AuthorRemote(t, roster, "site-b", 1, w3Base+1*w3Hour, func(rs *store.Store) {
		if _, err := AmendCompany(rs, "site-b", 1, CompanyAmendment{CompanyID: fx.x, SameAs: w3Str(fx.z)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})[0]

	fx.st = w3NewStore(t)
	w3ApplyAll(t, fx.st, roster)
	w3Apply(t, fx.st, fx.twoFact)
	w3Apply(t, fx.st, fx.peerMerge)

	// Fixture preconditions (rule 6): ONE event must carry BOTH facts, and
	// the peer must be later in RECORD order while carrying the EARLIER
	// clock (so a chronological resolver disagrees).
	if !fx.twoFact.Key().Less(fx.peerMerge.Key()) {
		t.Fatalf("W5d-19 fixture defect: the peer merge %v is not later in record order than the two-fact event %v", fx.peerMerge.Key(), fx.twoFact.Key())
	}
	if fx.peerMerge.AuthoredAt.WallMS >= fx.twoFact.AuthoredAt.WallMS {
		t.Fatalf("W5d-19 fixture defect: the record-order winner must carry the EARLIER clock, got %d >= %d", fx.peerMerge.AuthoredAt.WallMS, fx.twoFact.AuthoredAt.WallMS)
	}
	return fx
}

// TestW5dPerFactOutcomeOnTheTwoFactAmendment — W5d-19 (= W3-9's ride) on
// DR-2(a)'s {name, same_as} construction: one event, two facts, and the
// outcomes DIFFER — same_as Superseded by the peer, `name` still Effective.
// Kills W3-m15: an event-level verdict marks the whole earlier amendment
// lost and reports `name` Superseded too.
func TestW5dPerFactOutcomeOnTheTwoFactAmendment(t *testing.T) {
	fx := w5dBuildTwoFact(t)
	w5dAssertOutcomePins(t, fx.st, "W5d-19", fx.twoFact.Key(), []w5dOutcomePin{
		{Fact: "name", Asserted: fx.pinnedName, Status: Effective, Winner: fx.twoFact.Key()},
		{Fact: "same_as", Asserted: fx.y, Status: Superseded, Winner: fx.peerMerge.Key()},
	})
	// The Winner convention, asserted both directions (VF §4.2).
	nameOut := w5dFact(t, fx.st, fx.twoFact.Key(), w5dFactName)
	if nameOut.Winner != fx.twoFact.Key() {
		t.Fatalf("W5d-19: the Effective `name` fact's Winner is %v, want the queried key %v (Winner == queried key IFF Effective)", nameOut.Winner, fx.twoFact.Key())
	}
	sameAsOut := w5dFact(t, fx.st, fx.twoFact.Key(), w5dFactSameAs)
	if sameAsOut.Winner == fx.twoFact.Key() {
		t.Fatalf("W5d-19 (W3-m15): the Superseded `same_as` fact's Winner equals the queried key")
	}
}

// --- W5d-20 (= W3-10): merge-induced supersession on `name` -------------

type w5dMergeJoinFixture struct {
	st       *store.Store
	x, y     string
	xName    store.Envelope // site-a: X's name assertion (the LOSER, by merge)
	yName    store.Envelope // site-b: Y's name assertion (the record-order WINNER)
	merge    store.Envelope // site-a: X.same_as → Y, the JOINING assertion
	xNameLit string
	yNameLit string
}

func w5dBuildMergeJoin(t *testing.T) w5dMergeJoinFixture {
	t.Helper()
	fx := w5dMergeJoinFixture{xNameLit: "Join X Ltd", yNameLit: "Join Y Ltd"}
	origin := w3NewStore(t)
	x, y, _ := w5dCompanyPair(t, origin, "Join Source Ltd", "Join Target Ltd")
	fx.x, fx.y = x, y
	roster := w3Events(t, origin, "site-a", 1)

	// site-a: X's name, then the merge X→Y (same stream, ascending seq).
	local := w3NewStore(t)
	w3ApplyAll(t, local, roster)
	fx.xName = w5dAmendLocal(t, local, "site-a", 1, w3Base+3*w3Hour,
		CompanyAmendment{CompanyID: fx.x, Name: w3Str(fx.xNameLit)})
	fx.merge = w5dAmendLocal(t, local, "site-a", 1, w3Base+4*w3Hour,
		CompanyAmendment{CompanyID: fx.x, SameAs: w3Str(fx.y)})
	siteA := w3EventsAfter(t, local, "site-a", 1, int64(len(roster)))

	// site-b: Y's name, LATER in record order, EARLIER on the clock.
	fx.yName = w3AuthorRemote(t, roster, "site-b", 1, w3Base+1*w3Hour, func(rs *store.Store) {
		if _, err := AmendCompany(rs, "site-b", 1, CompanyAmendment{CompanyID: fx.y, Name: w3Str(fx.yNameLit)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})[0]

	fx.st = w3NewStore(t)
	w3ApplyAll(t, fx.st, roster)
	w3ApplyAll(t, fx.st, siteA)
	w3Apply(t, fx.st, fx.yName)

	// Fixture preconditions (rule 6): the two name assertions target
	// DIFFERENT records (so only the merge can join them), Y's is later in
	// record order, and it carries the EARLIER clock.
	if fx.x == fx.y {
		t.Fatalf("W5d-20 fixture defect: the two name assertions target the same record — the merge would join nothing")
	}
	if !fx.xName.Key().Less(fx.yName.Key()) {
		t.Fatalf("W5d-20 fixture defect: Y's name assertion %v is not later in record order than X's %v", fx.yName.Key(), fx.xName.Key())
	}
	if fx.yName.AuthoredAt.WallMS >= fx.xName.AuthoredAt.WallMS {
		t.Fatalf("W5d-20 fixture defect: the record-order winner must carry the EARLIER clock, got %d >= %d", fx.yName.AuthoredAt.WallMS, fx.xName.AuthoredAt.WallMS)
	}
	return fx
}

// TestW5dMergeInducedSupersession — W5d-20 (= W3-10's ride) on `name` per
// DR-2 (VF T9b / §6 case 5): X carries a name assertion; Y carries a
// later-keyed one; a same_as merge JOINS them; Outcome(X's assertion) is
// Superseded with Winner = Y's assertion key. Kills W3-m16: a
// same-target-only resolver is blind to assertion sets joined by a merge
// and leaves X's name Effective forever.
func TestW5dMergeInducedSupersession(t *testing.T) {
	fx := w5dBuildMergeJoin(t)
	w5dAssertOutcomePins(t, fx.st, "W5d-20", fx.xName.Key(), []w5dOutcomePin{
		{Fact: "name", Asserted: fx.xNameLit, Status: Superseded, Winner: fx.yName.Key()},
	})
	// Case-5 vocabulary renderable: the joining merge is identifiable via
	// its OWN Effective same_as outcome at its own key (FactOutcome carries
	// no joining-merge field — VF §4.2 is pinned, and W5d-9(c) asserts it).
	w5dAssertOutcomePins(t, fx.st, "W5d-20 (the joining merge)", fx.merge.Key(), []w5dOutcomePin{
		{Fact: "same_as", Asserted: fx.y, Status: Effective, Winner: fx.merge.Key()},
	})
}

// --- W5d-21 (= W3-16): the deferred case at rest ------------------------

// TestW5dDeferredCaseAtRest — W5d-21: append the local merge/un-merge,
// THEN apply the peer's winning assertion through the wire/fold path with
// NO POST at either end; Outcome(local) is Superseded at the next read.
// This is the behavioural tooth of §4.3 rule 2 (status computed at read);
// W5d-1 is its structural tooth. Kills W3-m22: a status stamped at write
// and never revisited stays Effective forever.
func TestW5dDeferredCaseAtRest(t *testing.T) {
	origin := w3NewStore(t)
	x, y, roster := w5dCompanyPair(t, origin, "Deferred X Ltd", "Deferred Y Ltd")

	local := w3NewStore(t)
	w3ApplyAll(t, local, roster)
	merge := w5dAmendLocal(t, local, "site-a", 1, w3Base+1*w3Hour,
		CompanyAmendment{CompanyID: x, SameAs: w3Str(y)})
	un := w5dAmendLocal(t, local, "site-a", 1, w3Base+2*w3Hour,
		CompanyAmendment{CompanyID: x, Clear: []string{w5dFactSameAs}})

	// BEFORE the peer arrives the un-merge is Effective — this pins the
	// moment at which a write-time stamp would be made, so W3-m22's red is
	// a CHANGE at the next read, not a value that was always wrong.
	w5dAssertOutcomePins(t, local, "W5d-21 (before the peer arrives)", un.Key(), []w5dOutcomePin{
		{Fact: "same_as", Asserted: "un-merge", Status: Effective, Winner: un.Key()},
	})

	// The peer's winning merge arrives by WIRE — no POST at either end.
	peer := w3AuthorRemote(t, roster, "site-b", 1, w3Base+30*60_000, func(rs *store.Store) {
		if _, err := AmendCompany(rs, "site-b", 1, CompanyAmendment{CompanyID: x, SameAs: w3Str(y)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})[0]
	if !un.Key().Less(peer.Key()) {
		t.Fatalf("W5d-21 fixture defect: the peer's assertion %v is not later in record order than the local un-merge %v", peer.Key(), un.Key())
	}
	w3Apply(t, local, peer)

	w5dAssertOutcomePins(t, local, "W5d-21 (at rest, after the fold)", un.Key(), []w5dOutcomePin{
		{Fact: "same_as", Asserted: "un-merge", Status: Superseded, Winner: peer.Key()},
	})
	if c := w5dCompany(t, local, x); c.SameAs == nil || *c.SameAs != y {
		t.Fatalf("W5d-21 (W3-m22): after the peer's winning merge folded, composed same_as = %s, want the merged %q — status is computed at READ, never stamped at write", w5dSameAsOf(c), y)
	}
	_ = merge
}

// --- W5d-10: the T-A agreement oracle over W5d's OWN fixtures -----------

// TestW5dTAOracleOverCompanyFixtures — W5d-10: W5d's fixtures JOIN the one
// T-A agreement oracle (the machinery is chunk-global; the fixtures are
// per-clone). Its two validity preconditions are asserted at RUNTIME inside
// the oracle — pairwise-distinct values per fact, and an unmatched fact is
// a FAILURE, never a skip — which is the CONTROL row this criterion owes,
// enforced at every run rather than recorded once. Kills W3-m17 and W3-m5.
func TestW5dTAOracleOverCompanyFixtures(t *testing.T) {
	t.Run("contested merge (same_as)", func(t *testing.T) {
		fx := w5dBuildContestedMerge(t)
		w5dRunTAOracle(t, fx.st, []w5dTAFixture{
			{Key: fx.toY.Key(), CompanyID: fx.x, Fact: w5dFactSameAs, Asserted: fx.y},
			{Key: fx.toZ.Key(), CompanyID: fx.x, Fact: w5dFactSameAs, Asserted: fx.z},
		})
	})
	t.Run("merge-joined name contest", func(t *testing.T) {
		fx := w5dBuildMergeJoin(t)
		w5dRunTAOracle(t, fx.st, []w5dTAFixture{
			{Key: fx.xName.Key(), CompanyID: fx.x, Fact: w5dFactName, Asserted: fx.xNameLit},
			{Key: fx.yName.Key(), CompanyID: fx.y, Fact: w5dFactName, Asserted: fx.yNameLit},
			{Key: fx.merge.Key(), CompanyID: fx.x, Fact: w5dFactSameAs, Asserted: fx.y},
		})
	})
	t.Run("same-site cross-epoch pair", func(t *testing.T) {
		fx := w5dBuildCrossEpochFixture(t)
		w5dRunTAOracle(t, fx.st, []w5dTAFixture{
			{Key: fx.e1a.Key(), CompanyID: fx.companyID, Fact: w5dFactName, Asserted: fx.nameA1},
			{Key: fx.e1b.Key(), CompanyID: fx.companyID, Fact: w5dFactName, Asserted: fx.nameA2},
			{Key: fx.e2.Key(), CompanyID: fx.companyID, Fact: w5dFactName, Asserted: fx.nameB},
		})
	})
	t.Run("cross-site AND cross-epoch", func(t *testing.T) {
		fx := w5dBuildCrossSiteEpochFixture(t)
		w5dRunTAOracle(t, fx.st, []w5dTAFixture{
			{Key: fx.siteA2.Key(), CompanyID: fx.companyID, Fact: w5dFactName, Asserted: fx.nameSiteA2},
			{Key: fx.siteB1.Key(), CompanyID: fx.companyID, Fact: w5dFactName, Asserted: fx.nameSiteB1},
		})
	})
	// ⚠ The two-fact fixture joins the oracle on its SAME_AS facts ONLY,
	// and the reason is a real property of the LANDED W3 design rather than
	// a convenience: DR-2(a)'s construction requires the peer to supersede
	// the same_as fact, which necessarily merges the amendment's own target
	// AWAY from the surviving representative. Composition folds only the
	// rows targeting a record, while Outcome contests VALUE facts across
	// the merge-JOINED set — so the winning `name` assertion sits on the
	// merged-away record and the composed resolved read answers with the
	// representative's own name. That asymmetry is exactly what M7 PINS at
	// W5d-5 ("a merged-away company's own later-keyed name assertion does
	// not drive the surviving representative's composed name"), and feeding
	// it to the T-A iff would manufacture a false red against a pinned
	// design decision. Same reason, same shape, second instance.
	t.Run("two-fact amendment (same_as facts only — see the note above)", func(t *testing.T) {
		fx := w5dBuildTwoFact(t)
		w5dRunTAOracle(t, fx.st, []w5dTAFixture{
			{Key: fx.twoFact.Key(), CompanyID: fx.x, Fact: w5dFactSameAs, Asserted: fx.y},
			{Key: fx.peerMerge.Key(), CompanyID: fx.x, Fact: w5dFactSameAs, Asserted: fx.z},
		})
	})
}

// TestW5dTAOraclePreconditionsAreEnforced — the oracle's own CONTROL: its
// precondition 1 (pairwise-distinct values per fact) must be capable of
// firing, or "the preconditions were met" is a claim nothing can fail.
// Driven through the same duplicate detector the oracle runs.
func TestW5dTAOraclePreconditionsAreEnforced(t *testing.T) {
	seen := map[string]map[string]int{}
	dup := false
	for _, f := range []w5dTAFixture{
		{Fact: w5dFactName, Asserted: "Same Value Ltd"},
		{Fact: w5dFactName, Asserted: "Same Value Ltd"},
	} {
		if seen[f.Fact] == nil {
			seen[f.Fact] = map[string]int{}
		}
		if _, d := seen[f.Fact][f.Asserted]; d {
			dup = true
		}
		seen[f.Fact][f.Asserted]++
	}
	if !dup {
		t.Fatalf("W5d-10 CONTROL FAILED: the T-A precondition-1 duplicate detector did not fire on two identical asserted values for one fact — an oracle whose preconditions were never checked is the self-confirming shape §5 exists to refuse")
	}
}

// --- W5d-9(c): FactOutcome's exported-field-set census ------------------

// w5dExportedFieldCensus returns "Name type" for each EXPORTED field of a
// struct type, in declaration order — the detector shared by the control
// and the real assertion.
func w5dExportedFieldCensus(v any) []string {
	rt := reflect.TypeOf(v)
	var out []string
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if !f.IsExported() {
			continue
		}
		out = append(out, fmt.Sprintf("%s %s", f.Name, f.Type.String()))
	}
	return out
}

// w5dPlantedFactOutcome is the CONTROL plant: FactOutcome's field set plus
// the fifth field DR-7 names as the cheapest wrong build — a Competitors
// field ON the struct, which would ship green and silently break W4's lock.
type w5dPlantedFactOutcome struct {
	Fact        string
	Asserted    string
	Status      Status
	Winner      store.Key
	Competitors []store.Key
}

// TestW5dFactOutcomeExportedFieldSetIsByteStable — W5d-9(c): the pinned
// struct gets an EXECUTABLE tooth. FactOutcome's exported fields are
// exactly {Fact, Asserted, Status, Winner}, in that order, with those types
// [MEASURED — outcome.go:44–51]. W5d-9's new competitor API lands BESIDE
// the struct, never as a field ON it, so W4's "no field added" guarantee
// holds. CONTROL FIRST: the planted fifth field must be detected before the
// clean pass counts (there are ZERO reflect.TypeOf/NumField assertions
// anywhere in the tree today, and w3_seams_test.go:49 calls the guarantee
// "DOC-pinned").
func TestW5dFactOutcomeExportedFieldSetIsByteStable(t *testing.T) {
	want := []string{
		"Fact string",
		"Asserted string",
		"Status domain.Status",
		"Winner store.Key",
	}

	// --- CONTROL FIRST: the planted fifth field ------------------------
	planted := w5dExportedFieldCensus(w5dPlantedFactOutcome{})
	if reflect.DeepEqual(planted, want) {
		t.Fatalf("W5d-9(c) CONTROL FAILED: the census reports the PLANTED five-field struct as identical to the pinned four-field set (%v) — a census that cannot see an added field proves nothing", planted)
	}
	if len(planted) != 5 || !strings.HasPrefix(planted[4], "Competitors ") {
		t.Fatalf("W5d-9(c) CONTROL FAILED: the census of the planted struct is %v, want five entries ending in the planted Competitors field", planted)
	}

	// --- the real assertion --------------------------------------------
	got := w5dExportedFieldCensus(FactOutcome{})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("W5d-9(c): FactOutcome's exported field set is %v, want EXACTLY %v in that order — VF §4.2 is pinned verbatim (\"no field added, no field renamed\") and W4's lock depends on it. Per-fact competitor enumeration is a NEW API beside the struct (W5d-9(b)), never a field on it: for amendments the competitors are discarded in the resolve loop, so the struct genuinely cannot hold them", got, want)
	}
}

// --- W5d-9 (b)/(d): the competitor API and the ONE shared pass ----------

// w5dCompanyAmendmentEvents reads every company_amended envelope in the
// given streams — the universe the completeness half of the one-pass
// agreement quantifies over.
func w5dCompanyAmendmentEvents(t *testing.T, st *store.Store, streams [][2]interface{}) []store.Envelope {
	t.Helper()
	var out []store.Envelope
	for _, s := range streams {
		for _, e := range w3Events(t, st, s[0].(string), s[1].(int64)) {
			var pl struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(e.Payload, &pl); err != nil {
				continue
			}
			if pl.Type == w5dEvtCompanyAmended {
				out = append(out, e)
			}
		}
	}
	return out
}

// TestW5dCompetitorEnumerationAndOutcomeShareOnePass — W5d-9(b) and (d).
//
// (b) The per-fact competitor enumeration exists and answers for a queried
// key. On the merge-joined fixture the WINNER's competitor set must contain
// the competitor a same_as merge JOINED to it — an event that never
// targeted the winner's record. That is the case a second, narrower
// spelling of the contest scope ("assertions on the same target id") gets
// wrong while still looking correct on every un-merged fixture.
//
// (d) ONE SHARED INTERNAL PASS, asserted as an AGREEMENT rather than as a
// structure: the rule that decides which events compete is written once, so
// Competitors and Outcome cannot disagree. Both directions are checked —
// every enumerated competitor must agree with Outcome about the winner, and
// every event Outcome places in the same contest must be enumerated. A
// contest-scope rule that exists in two spellings reds here, which is the
// same defect this document forbids for SQL ordering, one layer up.
func TestW5dCompetitorEnumerationAndOutcomeShareOnePass(t *testing.T) {
	fx := w5dBuildMergeJoin(t)
	streams := [][2]interface{}{{"site-a", int64(1)}, {"site-b", int64(1)}}

	// (b) — the merge-joined competitor is enumerated for the WINNER.
	got := w5dCompetitorKeys(t, fx.st, fx.yName.Key())
	if !w5dContains(got[w5dFactName], fx.xName.Key().String()) {
		t.Fatalf("W5d-9(b): Competitors(%v)[%q] = %v, want it to contain the MERGE-JOINED competitor %v — X's name assertion never targeted Y, and only the merge joins them; a competitor enumeration scoped to the same target id misses exactly this case (and is a SECOND spelling of the contest-scope rule, which (d) forbids)", fx.yName.Key(), w5dFactName, got[w5dFactName], fx.xName.Key())
	}
	if w5dContains(got[w5dFactName], fx.yName.Key().String()) {
		t.Fatalf("W5d-9(b): Competitors(%v)[%q] contains the QUERIED key itself — the enumeration is of the OTHER events contesting the fact", fx.yName.Key(), w5dFactName)
	}

	// (d) — agreement, both directions, over every fact of every company
	// amendment in the log.
	for _, ev := range w5dCompanyAmendmentEvents(t, fx.st, streams) {
		outs, err := Outcome(fx.st, ev.Key())
		if err != nil {
			t.Fatalf("W5d-9(d): Outcome(%v) errored: %v", ev.Key(), err)
		}
		comp := w5dCompetitorKeys(t, fx.st, ev.Key())
		for _, o := range outs {
			// Forward: every enumerated competitor shares this fact's winner.
			for _, ck := range comp[o.Fact] {
				other := w5dEventByKeyString(t, fx.st, streams, ck)
				co, err := Outcome(fx.st, other)
				if err != nil {
					t.Fatalf("W5d-9(d): Outcome(%v) — an enumerated competitor of %v — errored: %v", other, ev.Key(), err)
				}
				matched := false
				for _, c := range co {
					if c.Fact != o.Fact {
						continue
					}
					matched = true
					if c.Winner != o.Winner {
						t.Fatalf("W5d-9(d): the competitor enumeration and Outcome DISAGREE about fact %q: Competitors(%v) lists %v, but Outcome says their winners are %v and %v — the contest-scope rule exists in two spellings", o.Fact, ev.Key(), other, o.Winner, c.Winner)
					}
				}
				if !matched {
					t.Fatalf("W5d-9(d): Competitors(%v) lists %v as a competitor on fact %q, but Outcome(%v) reports no such fact — cannot-match is a FAILURE, never a skip", ev.Key(), other, o.Fact, other)
				}
			}
			// Completeness: every OTHER event Outcome places in the same
			// contest must be enumerated.
			for _, peer := range w5dCompanyAmendmentEvents(t, fx.st, streams) {
				if peer.Key() == ev.Key() {
					continue
				}
				po, err := Outcome(fx.st, peer.Key())
				if err != nil {
					continue // a wire no-op has no facts; not this check's business
				}
				for _, p := range po {
					if p.Fact != o.Fact || p.Winner != o.Winner {
						continue
					}
					if !w5dContains(comp[o.Fact], peer.Key().String()) {
						t.Fatalf("W5d-9(d): Outcome places %v and %v in the SAME contest on fact %q (both resolve to winner %v), but Competitors(%v)[%q] = %v does not enumerate it — the enumeration's contest scope is NARROWER than Outcome's, which is the two-spellings defect", ev.Key(), peer.Key(), o.Fact, o.Winner, ev.Key(), o.Fact, comp[o.Fact])
					}
				}
			}
		}
	}
}

func w5dContains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// w5dEventByKeyString resolves a competitor key rendered by store.Key.String
// back to the envelope key it names, so the agreement check can call
// Outcome on it.
func w5dEventByKeyString(t *testing.T, st *store.Store, streams [][2]interface{}, keyStr string) store.Key {
	t.Helper()
	for _, e := range w5dCompanyAmendmentEvents(t, st, streams) {
		if e.Key().String() == keyStr {
			return e.Key()
		}
	}
	t.Fatalf("W5d-9(d): the competitor enumeration named key %q, which matches no company_amended event in the log — a competitor must be a real event", keyStr)
	return store.Key{}
}
