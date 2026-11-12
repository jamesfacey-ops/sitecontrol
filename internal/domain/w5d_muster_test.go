// w5d_muster_test.go — W5d-6 (DR-4: the workgroup twin at muster.go:133 is
// UNTOUCHED, asserted BEHAVIOURALLY because a structural assertion is
// satisfied by a build that passes the same map to both) and W5d-14 (DR-3:
// snapshots stay frozen on BOTH paths, asserted distinctly, with the live
// path's composed-name flow asserted as a deliberate CHANGE).
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, W5d LOCKED 2026-08-31.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD — that IS these tests' clean-tree red.
// It is ALSO the red for SEAM W5D-S-MUSTER's signature change: MusterLive
// cannot serve DR-4 with one aliasMap parameter (domain/muster.go:107 feeds
// both :122 and :133), so a second parameter is unavoidable. The builder
// owns the four existing call sites in domain/muster_test.go (:175, :199,
// :353, :384) and the one non-test caller (web/muster.go:326/:332).
//
// KILLER MUTANTS: (m3) (the workgroup twin re-pointed alongside the company
// line — or, the cheapest wrong build, ONE map passed to both lines),
// (m1) (the snapshot re-resolving through the corrected company, AND its
// inverse: the live path converted to snapshot-preference).
package domain

import (
	"reflect"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// w5dCompanyGroupLabel returns the rendered company label for the group
// carrying companyID, and whether such a group exists.
func w5dCompanyGroupLabel(b MusterBoard, companyID string) (string, bool) {
	for _, g := range b.Companies {
		if g.CompanyID == companyID {
			return g.CompanyName, true
		}
	}
	return "", false
}

// w5dWorkgroupLabel returns the rendered workgroup label for workgroupID
// anywhere on the board.
func w5dTradeLabel(b MusterBoard, tradeID string) (string, bool) {
	for _, g := range b.Companies {
		for _, w := range g.Trades {
			if w.TradeID == tradeID {
				return w.TradeName, true
			}
		}
	}
	return "", false
}

func w5dTrades(t *testing.T, st *store.Store) []Trade {
	t.Helper()
	ws, err := Trades(st)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

// --- W5d-6 (DR-4): the two resolvers must be able to DISAGREE -----------

// TestW5dTradeTwinStillResolvesThroughRefAlias — W5d-6. The fixture
// makes the two maps DISAGREE inside the SAME rendered board: a company id
// and a workgroup id where the legacy ref_alias map resolves one way and
// the composed company_amended.same_as map the other. The company label is
// pinned to the COMPOSED answer; the workgroup label to the REF_ALIAS
// answer. A build that passes one map to both lines cannot satisfy both
// pins — which is exactly why DR-4's guard had to become behavioural: a
// structural assertion that :133 still names "an alias map" is SATISFIED by
// that build, and that build is the failure.
func TestW5dTradeTwinStillResolvesThroughRefAlias(t *testing.T) {
	st := w3NewStore(t)
	// Two companies: the duplicate X and the composed canonical Z.
	x, z, _ := w5dCompanyPair(t, st, "Muster Dup Ltd", "Muster Canonical Ltd")
	// ⚠ ⟦RETIRED — §60, 2026-09-03⟧ KIND B (site 15): the stale third company is retired
	// with the divergent LEGACY company row that was its only purpose. Superseded lines:
	//   ⟦was: "// A THIRD company: the one a stale legacy ref_alias row would name."⟧
	//   ⟦was: "stale, err := CreateCompany(st, \"site-a\", 1, \"Muster Stale Ltd\", 0, \"default\")"⟧
	//   ⟦was: "if err != nil {"⟧
	//   ⟦was: "\tt.Fatal(err)"⟧
	//   ⟦was: "}"⟧
	// WHAT IT PROVED: that the company line did NOT resolve through the second mechanism.
	// WHICH RULING REMOVED ITS SUBJECT: P-3 §5 — with no second company mechanism, no
	// divergent company edge can be authored, so this arm is unconstructible.
	// ⭐ (3) FIRST: the arm's PURPOSE — "one map passed to both lines cannot satisfy both
	// pins" — is NOT lost. It is re-armed below by making the two COMPOSED maps disjoint,
	// which catches the same build: a company line fed the trade map cannot find X, and a
	// trade line fed the company map cannot find wgDup. Only the third company goes.
	// Two workgroups under the canonical company: the duplicate and its
	// legacy ref_alias canonical.
	// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ DR-W8-2 arity: the company argument is dropped — a trade is a site-wide open set, not scoped to a company. ⟦was: "wgDup, err := CreateTrade(st, "site-a", 1, z, "Crew Dup", 0, "default")"⟧
	wgDup, err := CreateTrade(st, "site-a", 1, "Crew Dup", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ DR-W8-2 arity: the company argument is dropped — a trade is a site-wide open set, not scoped to a company. ⟦was: "wgCanon, err := CreateTrade(st, "site-a", 1, z, "Crew Canonical", 0, "default")"⟧
	wgCanon, err := CreateTrade(st, "site-a", 1, "Crew Canonical", 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	// COMPOSED company merge: X → Z.
	w5dAmendLocal(t, st, "site-a", 1, w3Base+1*w3Hour,
		CompanyAmendment{CompanyID: x, SameAs: w3Str(z)})
	// ⟦LOCK — §60, 2026-09-03⟧ KIND B (sites 16–17) — DISPOSITION (3), THE DISCRIMINATION
	// RE-EXPRESSED AGAINST A SURVIVING RIVAL. W5d-6's criterion is that THE TWO RESOLVERS
	// MUST BE ABLE TO DISAGREE inside one rendered board, and that criterion is untouched
	// by W8: `MusterLive` still takes two AliasMap parameters. Only the SECOND MAP's
	// identity changed — the trade line's resolver is now the COMPOSED trade merge map
	// (trade_amend.go:507) where it used to be the deleted one.
	// ⭐ THIS IS A STRICTLY STRONGER FIXTURE than the one it replaces: the trade line is now
	// pinned to a LIVE mechanism rather than a dead one, so the pin can actually be broken
	// by a wrong build instead of passing over a path nothing could reach.
	//   ⟦was: "// LEGACY ref_alias rows: the company one is DIVERGENT (X → stale); the"⟧
	//   ⟦was: "// workgroup one is the live mechanism until W8 (P-3 §5's schedule,"⟧
	//   ⟦was: "// cited never restated)."⟧
	//   ⟦was: "if _, err := CreateAlias(st, \"site-a\", 1, x, stale.CompanyID, 0, \"default\"); err != nil {"⟧
	//   ⟦was: "\tt.Fatal(err)"⟧
	//   ⟦was: "}"⟧
	//   ⟦was: "if _, err := CreateAlias(st, \"site-a\", 1, wgDup.TradeID, wgCanon.TradeID, 0, \"default\"); err != nil {"⟧
	//   ⟦was: "\tt.Fatal(err)"⟧
	//   ⟦was: "}"⟧
	if _, err := AmendTrade(st, "site-a", 1, TradeAmendment{TradeID: wgDup.TradeID, SameAs: &wgCanon.TradeID}, 0, "default"); err != nil {
		t.Fatalf("fixture: composed trade merge refused: %v", err)
	}

	companyMerge := w5dMergeMap(t, st)
	// ⟦was: "legacy, err := LoadAliasMap(st)"⟧ — ONE map over ALL kinds; its successors are
	// four KIND-PARTITIONED maps (scope §2), and the trade line takes its own.
	tradeMerge, err := LoadTradeMergeMap(st)
	if err != nil {
		t.Fatal(err)
	}
	// Fixture preconditions (rule 6): each map must carry ITS OWN edge and NEITHER may
	// carry the other's — otherwise this test cannot pose its case.
	// ⟦LOCK — §60, 2026-09-03⟧ The DISJOINTNESS pair below is what now does the work the
	// divergent legacy row used to do: a build that passes ONE map to BOTH lines fails,
	// because the company line fed the trade map cannot find X and the trade line fed the
	// company map cannot find wgDup. That is W5d-6's criterion, unchanged, posed against
	// two LIVE mechanisms instead of one live and one dead.
	if got := companyMerge.Resolve(x); got != z {
		t.Fatalf("W5d-6 fixture defect: the composed company map resolves %s to %s, want %s", x, got, z)
	}
	//   ⟦was: "if got := legacy.Resolve(x); got != stale.CompanyID {"⟧
	//   ⟦was: "\tt.Fatalf(\"W5d-6 fixture defect: the legacy ref_alias map resolves %s to %s, want the DIVERGENT %s — if the two maps agree, a single shared map passes and the criterion certifies nothing\", x, got, stale.CompanyID)"⟧
	//   ⟦was: "}"⟧
	// ⚠ RETIRED and REPLACED BY ITS MIRROR: a divergent company edge in the OTHER map is
	// no longer authorable (AmendTrade refuses a non-trade id, trade_amend.go:584-585), so
	// the disjointness is asserted directly instead — same criterion, constructible fixture.
	if _, present := tradeMerge[x]; present {
		t.Fatalf("W5d-6 fixture defect: the TRADE merge map carries a company edge (%s) — the fixture is not isolating the two mechanisms, and a single shared map would pass", x)
	}
	//   ⟦was: "if got := legacy.Resolve(wgDup.TradeID); got != wgCanon.TradeID {"⟧
	if got := tradeMerge.Resolve(wgDup.TradeID); got != wgCanon.TradeID {
		t.Fatalf("W5d-6 fixture defect: the composed trade merge map resolves trade %s to %s, want %s", wgDup.TradeID, got, wgCanon.TradeID)
	}
	if _, present := companyMerge[wgDup.TradeID]; present {
		t.Fatalf("W5d-6 fixture defect: the COMPANY merge map carries a trade edge — the fixture is not isolating the two mechanisms")
	}

	person, err := CreatePerson(st, "site-a", 1, "Muster Occupant", &x, &wgDup.TradeID, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	dupName, wgDupName := "Muster Dup Ltd", "Crew Dup"
	visits := []Visit{{
		VisitID: "w5d-v1", SiteID: "site-a", PersonID: person.PersonID,
		CompanySnapshotID: &x, CompanySnapshotName: &dupName,
		TradeSnapshotID: &wgDup.TradeID, TradeSnapshotName: &wgDupName,
		IsOpen: 1,
	}}

	board := MusterLive(visits, []Person{person}, w5dCompanies(t, st), w5dTrades(t, st),
		companyMerge, tradeMerge, 0)

	// The COMPANY line resolves through the COMPOSED COMPANY map.
	//   ⟦was: "if _, isStale := w5dCompanyGroupLabel(board, stale.CompanyID); isStale {"⟧
	//   ⟦was: "\tt.Fatalf(\"W5d-5/W5d-6 (W3-m9): the muster's COMPANY line resolved %s through the legacy ref_alias row to %s — at W5d it must resolve through composed company_amended.same_as to %s (the exemption W3-4 granted that grouping EXPIRES here)\", x, stale.CompanyID, z)"⟧
	//   ⟦was: "}"⟧
	// ⚠ The stale-target branch is retired with the third company: nothing can map X
	// anywhere but Z any more, so the branch could never be taken.
	if _, ok := w5dCompanyGroupLabel(board, z); !ok {
		t.Fatalf("W5d-5: the muster board carries no group for the composed canonical company %s; groups: %+v — a company line fed the TRADE map would land here, which is how this pin catches the one-map build", z, board.Companies)
	}
	// The TRADE line resolves through the COMPOSED TRADE map.
	if _, ok := w5dTradeLabel(board, wgCanon.TradeID); !ok {
		// ⟦was: ""⟧ — message re-pointed at §60; the resolver named is now the
		// COMPOSED trade merge map, not the deleted one. The criterion is unchanged.
		t.Fatalf("W5d-6 (m3): the muster's TRADE line did not resolve %s to its composed canonical %s — the trade line takes its OWN resolver (LoadTradeMergeMap, trade_amend.go:507), separate from the company line's (DR-4). A build that passes ONE map to both lines fails HERE, which is why this guard is behavioural rather than structural", wgDup.TradeID, wgCanon.TradeID)
	}
	if _, ok := w5dTradeLabel(board, wgDup.TradeID); ok {
		t.Fatalf("W5d-6 (m3): the trade group is still keyed by the UNRESOLVED id %s — the composed trade resolution stopped happening", wgDup.TradeID)
	}
}

// --- W5d-14 (DR-3): snapshots stay frozen, on BOTH paths ----------------

// w5dParamTypes returns the parameter type strings of a function value —
// the detector shared by W5d-14(a)'s planted control and its real
// structural assertion.
func w5dParamTypes(fn any) []string {
	ft := reflect.TypeOf(fn)
	var out []string
	for i := 0; i < ft.NumIn(); i++ {
		out = append(out, ft.In(i).String())
	}
	return out
}

// w5dHasResolvingParam reports whether any parameter is an AliasMap — the
// "can this function re-resolve at all?" test that makes MusterReprint's
// guarantee TYPE-LEVEL rather than a runtime flag (muster.go:148–154).
func w5dHasResolvingParam(fn any) bool {
	want := reflect.TypeOf(AliasMap{}).String()
	for _, p := range w5dParamTypes(fn) {
		if p == want {
			return true
		}
	}
	return false
}

// w5dPlantedResolvingReprint is W5d-14(a)'s CONTROL: a reprint-shaped
// function that DOES take a resolving parameter. The detector must flag it
// before the real assertion's clean pass counts.
func w5dPlantedResolvingReprint(visits []Visit, persons []Person, aliasMap AliasMap, minGroupSize int) MusterBoard {
	return MusterBoard{}
}

// TestW5dReprintCannotResolveAndRendersAsRecorded — W5d-14(a): the reprint
// renders the AS-RECORDED company name after a correction, and takes NO
// composed-company or alias input at all — the structural guarantee at
// domain/muster.go:148–154, asserted STRUCTURALLY and BEHAVIOURALLY, with a
// planted resolving-parameter as its control. This is D-27's stated
// boundary: "correct the entity, and every future visit picks up the
// correction. Past visits keep what was true when they happened."
func TestW5dReprintCannotResolveAndRendersAsRecorded(t *testing.T) {
	// --- CONTROL FIRST: the planted resolving parameter -----------------
	if !w5dHasResolvingParam(w5dPlantedResolvingReprint) {
		t.Fatalf("W5d-14(a) CONTROL FAILED: the resolving-parameter detector did not flag a planted reprint that takes an AliasMap (params: %v) — a detector that cannot detect its plant proves nothing", w5dParamTypes(w5dPlantedResolvingReprint))
	}

	// --- the real structural assertion ---------------------------------
	if w5dHasResolvingParam(MusterReprint) {
		t.Fatalf("W5d-14(a) (m1): MusterReprint's signature now takes an AliasMap (params: %v) — the reprint's whole guarantee is that it is STRUCTURALLY incapable of re-resolving, no matter when or how many times it is called. W5d re-points the LIVE company line only", w5dParamTypes(MusterReprint))
	}

	// --- the behavioural assertion, on a corrected company --------------
	fx := w5dBuildFrozenSnapshotFixture(t)
	reprint := MusterReprint(fx.visits, []Person{fx.person}, 0)
	got, ok := w5dCompanyGroupLabel(reprint, fx.companyID)
	if !ok {
		t.Fatalf("W5d-14(a): the reprint carries no group for %s; groups: %+v", fx.companyID, reprint.Companies)
	}
	if got != fx.recordedName {
		t.Fatalf("W5d-14(a) (m1): the reprint renders %q, want the AS-RECORDED snapshot name %q — a company correction must never rewrite recorded history (D-27's stated boundary; the plan's own named mutant is \"snapshot re-resolving through the corrected company\")", got, fx.recordedName)
	}
}

// w5dFrozenSnapshotFixture is the ONE fixture W5d-14(b-ii) requires: one
// company amended via company_amended.name, ONE set of visit rows, and TWO
// outputs that must DISAGREE.
type w5dFrozenSnapshotFixture struct {
	st           *store.Store
	person       Person
	companyID    string
	visits       []Visit
	recordedName string // the snapshot name, frozen at check-in
	amendedName  string // the composed name, current
}

func w5dBuildFrozenSnapshotFixture(t *testing.T) w5dFrozenSnapshotFixture {
	t.Helper()
	fx := w5dFrozenSnapshotFixture{
		recordedName: "Kakivik Contracting Ltd",
		amendedName:  "Kakivik Construction Ltd",
	}
	fx.st = w3NewStore(t)
	c, err := CreateCompany(fx.st, "site-a", 1, fx.recordedName, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	fx.companyID = c.CompanyID
	p, err := CreatePerson(fx.st, "site-a", 1, "Frozen Snapshot Occupant", &fx.companyID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	fx.person = p
	// The visit snapshots the company AS IT WAS at check-in.
	recorded := fx.recordedName
	fx.visits = []Visit{{
		VisitID: "w5d-frozen-1", SiteID: "site-a", PersonID: p.PersonID,
		CompanySnapshotID: &fx.companyID, CompanySnapshotName: &recorded,
		IsOpen: 1,
	}}
	// The correction lands AFTER the check-in (DR-2: `name` is company's
	// only content fact, so it is the fact under test).
	w5dAmendLocal(t, fx.st, "site-a", 1, w3Base+1*w3Hour,
		CompanyAmendment{CompanyID: fx.companyID, Name: w3Str(fx.amendedName)})

	// Fixture preconditions (rule 6): the two names must DIFFER, the
	// composition must have taken effect, and the snapshot must still hold
	// the old one — otherwise "the two outputs differ" is unfalsifiable.
	if fx.recordedName == fx.amendedName {
		t.Fatalf("W5d-14 fixture defect: the recorded and amended names are identical")
	}
	if got, ok := w5dCompanyName(w5dCompany(t, fx.st, fx.companyID)); !ok || got != fx.amendedName {
		t.Fatalf("W5d-14 fixture defect: the composed company name is %q (present=%v), want the amended %q", got, ok, fx.amendedName)
	}
	if *fx.visits[0].CompanySnapshotName != fx.recordedName {
		t.Fatalf("W5d-14 fixture defect: the visit snapshot name changed under the correction — snapshots are written once at arrival")
	}
	return fx
}

// TestW5dLiveMusterPrefersTheComposedCurrentNameAndReprintDoesNot —
// W5d-14(b-ii), the H9 split: the live muster's PREFERENCE RULE is
// unchanged, but its OBSERVABLE OUTPUT deliberately changes, because
// W5d-3 makes Companies() composed and DR-2 picks `name` as the fact under
// test. ONE fixture, TWO outputs, asserted DIFFERENT and both pinned as
// literals: the amended name on the LIVE board, the as-recorded name on the
// REPRINT, from the SAME visit rows.
//
// Written as a pinned literal against pre-W5d rendering this criterion
// would be red by construction; written loosely enough to pass either way
// it would certify nothing. Pinning BOTH outputs and asserting they differ
// is what makes it neither.
func TestW5dLiveMusterPrefersTheComposedCurrentNameAndReprintDoesNot(t *testing.T) {
	fx := w5dBuildFrozenSnapshotFixture(t)
	live := MusterLive(fx.visits, []Person{fx.person}, w5dCompanies(t, fx.st), w5dTrades(t, fx.st),
		w5dMergeMap(t, fx.st), AliasMap{}, 0)
	reprint := MusterReprint(fx.visits, []Person{fx.person}, 0)

	liveLabel, ok := w5dCompanyGroupLabel(live, fx.companyID)
	if !ok {
		t.Fatalf("W5d-14(b-ii): the live board carries no group for %s; groups: %+v", fx.companyID, live.Companies)
	}
	reprintLabel, ok := w5dCompanyGroupLabel(reprint, fx.companyID)
	if !ok {
		t.Fatalf("W5d-14(b-ii): the reprint carries no group for %s; groups: %+v", fx.companyID, reprint.Companies)
	}
	if liveLabel != fx.amendedName {
		t.Fatalf("W5d-14(b-ii) (m1's inverse): the LIVE muster renders %q, want the composed CURRENT name %q — the live board is called from who the company IS now (muster.go:123–124); a builder implementing \"snapshots stay frozen\" must not convert the live path to snapshot-preference", liveLabel, fx.amendedName)
	}
	if reprintLabel != fx.recordedName {
		t.Fatalf("W5d-14(b-ii) (m1): the REPRINT renders %q, want the as-recorded snapshot name %q", reprintLabel, fx.recordedName)
	}
	if liveLabel == reprintLabel {
		t.Fatalf("W5d-14(b-ii) fixture defect: the two paths rendered the SAME label %q — DR-3's whole content is that they disagree, and a fixture where they agree proves neither", liveLabel)
	}
}

// TestW5dLiveMusterPreferenceRuleAllThreeArms — W5d-14(b-i): the live
// board's preference rule is unchanged and ALL THREE arms are pinned —
// the CURRENT composed name (muster.go:123–124), the SNAPSHOT name when
// the resolved id is not in the company list (:125–126), and the RAW
// COMPANY ID when neither is available (:127–128). The third arm was
// unmentioned before the lock and is where W5d-22(d)'s nameless company
// surfaces during a roll call.
func TestW5dLiveMusterPreferenceRuleAllThreeArms(t *testing.T) {
	st := w3NewStore(t)
	known, err := CreateCompany(st, "site-a", 1, "Known Ltd", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w5dAmendLocal(t, st, "site-a", 1, w3Base+1*w3Hour,
		CompanyAmendment{CompanyID: known.CompanyID, Name: w3Str("Known Amended Ltd")})

	// Arm 3's subject: a company whose name has been CLEARED from the wire
	// (the edge refuses Clear:["name"], the fold never refuses — W5d-22(d)).
	nameless, err := CreateCompany(st, "site-a", 1, "Soon Nameless Ltd", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w3Apply(t, st, w5dWireAmendment(t, "site-b", 1, 1, w3Base+2*w3Hour, nameless.CompanyID,
		map[string]any{"clear": []string{w5dFactName}}))
	if _, present := w5dCompanyName(w5dCompany(t, st, nameless.CompanyID)); present {
		t.Fatalf("W5d-22(d) fixture defect: the wire Clear:[\"name\"] did not compose to an absent name — arm 3 cannot be posed")
	}

	// Arm 2's subject: an id this store has NOT replicated a company row
	// for, carrying a snapshot name.
	const unreplicated = "site-x-company-unreplicated"
	knownSnap, otherSnap := "Known Ltd", "Snapshot Only Ltd"

	// ⚠ The nameless visit deliberately carries NO snapshot name: with one
	// present, arm 2 would answer and arm 3 would never be reached, so the
	// criterion's pinned "raw company id" rendering could not be posed.
	persons := []Person{}
	visits := []Visit{}
	for i, spec := range []struct {
		id   string
		snap *string
	}{
		{known.CompanyID, &knownSnap},
		{unreplicated, &otherSnap},
		{nameless.CompanyID, nil},
	} {
		p, err := CreatePerson(st, "site-a", 1, "Arm Occupant "+string(rune('A'+i)), nil, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		persons = append(persons, p)
		id := spec.id
		visits = append(visits, Visit{
			VisitID: "w5d-arm-" + string(rune('A'+i)), SiteID: "site-a", PersonID: p.PersonID,
			CompanySnapshotID: &id, CompanySnapshotName: spec.snap, IsOpen: 1,
		})
	}

	board := MusterLive(visits, persons, w5dCompanies(t, st), w5dTrades(t, st),
		w5dMergeMap(t, st), AliasMap{}, 0)

	arm1, ok := w5dCompanyGroupLabel(board, known.CompanyID)
	if !ok || arm1 != "Known Amended Ltd" {
		t.Fatalf("W5d-14(b-i) arm 1: label = %q (present=%v), want the CURRENT composed name %q (muster.go:123–124)", arm1, ok, "Known Amended Ltd")
	}
	arm2, ok := w5dCompanyGroupLabel(board, unreplicated)
	if !ok || arm2 != otherSnap {
		t.Fatalf("W5d-14(b-i) arm 2: label = %q (present=%v), want the SNAPSHOT name %q — an id this store has not replicated falls back to the row's own snapshot name rather than blanking (muster.go:125–126)", arm2, ok, otherSnap)
	}
	arm3, ok := w5dCompanyGroupLabel(board, nameless.CompanyID)
	if !ok {
		t.Fatalf("W5d-14(b-i) arm 3: the board carries no group for the nameless company %s", nameless.CompanyID)
	}
	if arm3 == "" {
		t.Fatalf("W5d-22(d)/W5d-18 (W3-m7): a company whose name was cleared from the wire renders a BLANK label during a roll call — \"absent, never empty-string\" is asserted AT THE READER, and the third preference arm is the raw company id (muster.go:127–128)")
	}
	if arm3 != nameless.CompanyID {
		t.Fatalf("W5d-14(b-i) arm 3 / W5d-22(d): label = %q, want the RAW COMPANY ID %q — that is the pinned rendering for a composed company with no name, and it is chosen rather than discovered", arm3, nameless.CompanyID)
	}
	if strings.TrimSpace(arm3) == "" {
		t.Fatalf("W5d-14(b-i) arm 3: the label is whitespace-only")
	}
}
