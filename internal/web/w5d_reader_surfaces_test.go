// w5d_reader_surfaces_test.go — W5d-5's WEB readers and W5d-3's surfacing.
// W5d-5's obligation is a PREDICATE, not a list: every non-test caller of
// Companies()/CompanyByID(), plus MusterLive's companies slice consumer,
// plus W5d's own merge-map loader, returns or consumes the COMPOSED
// company. Its current VALUE at 88dd6a6 is six call sites / seven
// enumerated entries [MEASURED]: web/muster.go's domain.Companies(s.Store),
// web/intake.go's two, web/gate.go's, web/picker.go's refDataNames, and
// domain/tripauthor.go's CompanyByID — plus domain/muster.go's companies
// slice consumer. Readers are pinned by FILE + CALL EXPRESSION, never by
// file:line (C-10: W5b inserts a refusal into intake.go and this window is
// parallelisable, so a line-pinned enumeration goes stale silently).
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, W5d LOCKED 2026-08-31.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD — that IS these tests' clean-tree red.
//
// KILLER MUTANTS: company-m14 (zero composition, surfacing as stale company
// names on the roster, the gate picker and the live muster), W3-m9 (a
// reader resolving via a second/stale path — the legacy-ref_alias
// divergence fixture at the WEB level, which is the only red that
// distinguishes a re-pointed muster.go:122 from an unmoved one that
// happened to agree).
//
// The domain-side halves of the same predicate (composed Company,
// Companies(), the derived snapshot, the dump) are in
// internal/domain/w5d_merge_readers_test.go and w5d_compose_test.go;
// tripauthor.go's CompanyByID is exercised through the check-ins here.
package web

import (
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// TestW5dCompanyReadersShowTheComposedName — W5d-3 at the web surfaces:
// after a company name amendment, the roster page, the gate picker and the
// live muster all show the COMPOSED name and never the base projection
// row's. At 88dd6a6 Companies()/CompanyByID() "compose nothing" — both
// return raw projection rows [MEASURED — refdata.go:351, :396] — which is
// precisely the pre-W3 shape of Persons()/PersonByID() that the
// zero-composition mutant targets.
func TestW5dCompanyReadersShowTheComposedName(t *testing.T) {
	const baseName = "Kakivik Base Row Ltd"
	const composedName = "Kakivik Composed Row Ltd"

	srv, st, _, _ := w5dSiteAServer(t)
	c, err := domain.CreateCompany(st, "site-a", 1, baseName, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// NOBODY is checked in on this fixture, deliberately — see the note on
	// the muster case below for why an on-site occupant would make the
	// negative assertion wrong rather than strict.
	if _, err := domain.CreatePerson(st, "site-a", 1, "Company Reader Roster Member", &c.CompanyID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// Fixture precondition (rule 6): the base name must be on screen BEFORE
	// the amendment, or "the composed name renders" cannot discriminate.
	if !strings.Contains(strings.ToLower(w3Get200(t, srv, "/roster")), strings.ToLower(baseName)) {
		t.Fatalf("W5d-3 fixture: the base company name %q does not render on /roster before the amendment — the surface is not showing company names at all, so the assertions below would be vacuous", baseName)
	}

	if _, err := domain.AmendCompany(st, "site-a", 1,
		domain.CompanyAmendment{CompanyID: c.CompanyID, Name: strPtrW5d(composedName)}, 0, "default"); err != nil {
		t.Fatal(err)
	}

	surfaces := []struct {
		path, label string
		raw         bool // the gate's roster rides an embedded JSON/markup block that w2StripNonMarkup removes
	}{
		{"/roster", "roster page (intake.go's domain.Companies)", false},
		{"/gate", "gate roster picker (gate.go's domain.Companies / picker.go's rosterGroups)", true},
	}
	for _, s := range surfaces {
		page := w3Get200(t, srv, s.path)
		text := w3PageText(page)
		if s.raw {
			text = strings.ToLower(page)
		}
		if !strings.Contains(text, strings.ToLower(composedName)) {
			t.Fatalf("W5d-3 (company-m14) on the %s: the composed company name %q does not render — company_amended ships, appends and folds, and this reader returns the created-state company untouched. That is \"the twice-produced CRITICAL class\", one entity over", s.label, composedName)
		}
		if strings.Contains(text, strings.ToLower(baseName)) {
			t.Fatalf("W5d-3 (company-m14) on the %s: the BASE-ROW company name %q still renders after the amendment — a reader answered from the raw projection row", s.label, baseName)
		}
	}
}

// TestW5dLiveMusterShowsTheComposedCompanyName — W5d-3 at the muster, kept
// as its own fixture because an ON-SITE occupant changes what /gate may
// legitimately show.
//
// ⚠ MEASURED, and it is a real boundary rather than a strictness choice:
// /gate's ON-SITE section is built by picker.go's onSiteGroups, whose input
// []onSiteRow is "deliberately-shaped … INCAPABLE of re-resolving a visit's
// company through anything but the snapshot already on the row"
// (picker.go:225–240, design §8.8-i). So once somebody is on site, the
// PRE-AMENDMENT company name renders on /gate CORRECTLY, as the snapshot of
// where they were when they checked in — D-27's boundary, one surface over
// from W5d-14's reprint. Asserting "the base name never renders" on /gate
// with an occupant present would fail a CORRECT build. The negative belongs
// only where the label comes from Companies()/CompanyByID().
func TestW5dLiveMusterShowsTheComposedCompanyName(t *testing.T) {
	const baseName = "Muster Base Row Ltd"
	const composedName = "Muster Composed Row Ltd"

	srv, st, _, _ := w5dSiteAServer(t)
	c, err := domain.CreateCompany(st, "site-a", 1, baseName, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	p, err := domain.CreatePerson(st, "site-a", 1, "Muster Reader Occupant", &c.CompanyID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := domain.GateCheckIn(st, "site-a", 1, domain.GateCheckInInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRK-52","⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK-52"),
		Driver:    domain.PersonSlot{ExistingID: p.PersonID},
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w3PageText(w3Get200(t, srv, "/muster")), strings.ToLower(baseName)) {
		t.Fatalf("W5d-3 fixture: the base company name %q does not group the occupant on /muster before the amendment — the assertions below would be vacuous", baseName)
	}
	if _, err := domain.AmendCompany(st, "site-a", 1,
		domain.CompanyAmendment{CompanyID: c.CompanyID, Name: strPtrW5d(composedName)}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	text := w3PageText(w3Get200(t, srv, "/muster"))
	if !strings.Contains(text, strings.ToLower(composedName)) {
		t.Fatalf("W5d-3 (company-m14) on the LIVE muster (muster.go's domain.Companies → MusterLive's companies slice): the composed company name %q does not render", composedName)
	}
	if strings.Contains(text, strings.ToLower(baseName)) {
		t.Fatalf("W5d-14(b-ii)/W5d-3: the LIVE muster still shows the pre-amendment company name %q — the live board is called from who the company IS now (muster.go:123–124). The as-recorded name belongs to the REPRINT, and that pairing is pinned at W5d-14(b-ii)", baseName)
	}
}

// TestW5dMusterCompanyGroupingResolvesComposedNotRefAlias — W5d-5's
// headline claim at the surface, and W3-m9's company-side producer. W3-4
// EXEMPTED the muster's company grouping "because its composed source does
// not exist until W5d"; W5d is the chunk that makes the exemption EXPIRE,
// so leaving muster.go:122 on LoadAliasMap is a W5d failure, not an
// inherited sanction.
//
// The fixture makes the two paths DISAGREE: a legacy ref_alias row maps the
// duplicate company somewhere the composed company_amended.same_as does
// not. This is the only red that distinguishes a re-pointed :122 from an
// unmoved one that happened to agree.
func TestW5dMusterCompanyGroupingResolvesComposedNotRefAlias(t *testing.T) {
	const dupName = "Kakavik Duplicate Ltd"
	const composedCanonical = "Kakivik Composed Canonical Ltd"
	const legacyStale = "Kakivik Legacy Stale Ltd"

	srv, st, _, _ := w5dSiteAServer(t)
	dup, err := domain.CreateCompany(st, "site-a", 1, dupName, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := domain.CreateCompany(st, "site-a", 1, composedCanonical, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// ⚠ ⟦RETIRED — §60, 2026-09-03⟧ KIND B (site 22): the stale third company goes with the
	// divergent legacy row that was its only purpose.
	//   ⟦was: "stale, err := domain.CreateCompany(st, \"site-a\", 1, legacyStale, 0, \"default\")"⟧
	//   ⟦was: "if err != nil {"⟧
	//   ⟦was: "\tt.Fatal(err)"⟧
	//   ⟦was: "}"⟧
	// The `legacyStale` constant is KEPT: the page assertion below still proves that name
	// appears NOWHERE, which is now a statement about a company that is never created —
	// weaker than it was, and said so rather than dressed up.
	p, err := domain.CreatePerson(st, "site-a", 1, "Grouped Occupant", &dup.CompanyID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := domain.GateCheckIn(st, "site-a", 1, domain.GateCheckInInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRK-53","⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK-53"),
		Driver:    domain.PersonSlot{ExistingID: p.PersonID},
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}

	// COMPOSED: dup → canonical. LEGACY ref_alias: dup → stale (divergent).
	if _, err := domain.AmendCompany(st, "site-a", 1,
		domain.CompanyAmendment{CompanyID: dup.CompanyID, SameAs: &canonical.CompanyID}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// ⟦LOCK — §60, 2026-09-03⟧ KIND B (sites 22–23) — DISPOSITION (3), RE-EXPRESSED AGAINST
	// A SURVIVING RIVAL. The criterion is that the muster's COMPANY line resolves through
	// the COMPOSED COMPANY map and through nothing else. Its old rival — a divergent second
	// company mechanism — is unconstructible (P-3 §5). ⭐ The surviving rival is the OTHER
	// resolver production actually passes: `MusterLive` takes a company map AND a trade map
	// (DR-4), so a build that feeds one map to both lines is still a live failure mode, and
	// the fixture below poses it by making the two maps DISJOINT.
	//   ⟦was: "if _, err := domain.CreateAlias(st, \"site-a\", 1, dup.CompanyID, stale.CompanyID, 0, \"default\"); err != nil {"⟧
	//   ⟦was: "\tt.Fatal(err)"⟧
	//   ⟦was: "}"⟧
	rivalTrade, err := domain.CreateTrade(st, "site-a", 1, "Rival Trade Dup", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	rivalCanon, err := domain.CreateTrade(st, "site-a", 1, "Rival Trade Canonical", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.AmendTrade(st, "site-a", 1,
		domain.TradeAmendment{TradeID: rivalTrade.TradeID, SameAs: &rivalCanon.TradeID}, 0, "default"); err != nil {
		t.Fatalf("fixture: composed trade merge refused: %v", err)
	}

	// Fixture preconditions (rule 6): the two paths must ACTUALLY disagree.
	composedMap, err := domain.LoadCompanyMergeMap(st)
	if err != nil {
		t.Fatal(err)
	}
	//   ⟦was: "legacyMap, err := domain.LoadAliasMap(st)"⟧
	tradeMap, err := domain.LoadTradeMergeMap(st)
	if err != nil {
		t.Fatal(err)
	}
	if got := composedMap.Resolve(dup.CompanyID); got != canonical.CompanyID {
		t.Fatalf("W5d-5 fixture defect: the composed map resolves the duplicate to %s, want %s", got, canonical.CompanyID)
	}
	//   ⟦was: "if got := legacyMap.Resolve(dup.CompanyID); got != stale.CompanyID {"⟧
	//   ⟦was: "\tt.Fatalf(\"W5d-5 fixture defect: the legacy ref_alias map resolves the duplicate to %s, want the DIVERGENT %s — if the two paths agree, an unmoved muster.go:122 passes and this test certifies nothing\", got, stale.CompanyID)"⟧
	//   ⟦was: "}"⟧
	// ⚠ REPLACED BY THE DISJOINTNESS PAIR, which is the same criterion posed against two
	// LIVE maps: if the two paths were one shared map, an unmoved company line would pass.
	if got := tradeMap.Resolve(rivalTrade.TradeID); got != rivalCanon.TradeID {
		t.Fatalf("W5d-5 fixture defect: the composed TRADE map does not carry its own edge (%s -> %s) — the rival resolver is empty, so a one-map build would not be caught", rivalTrade.TradeID, got)
	}
	if _, present := tradeMap[dup.CompanyID]; present {
		t.Fatalf("W5d-5 fixture defect: the TRADE map carries the duplicate COMPANY id — the two resolvers are not isolated and this test certifies nothing")
	}
	if _, present := composedMap[rivalTrade.TradeID]; present {
		t.Fatalf("W5d-5 fixture defect: the COMPANY map carries a trade edge — the two resolvers are not isolated")
	}

	page := w3Get200(t, srv, "/muster")
	text := w3PageText(page)
	if !strings.Contains(text, strings.ToLower(composedCanonical)) {
		t.Fatalf("W5d-5 (W3-m9): the live muster does not group the checked-in occupant under the COMPOSED canonical company %q — the muster's company grouping must re-point from LoadAliasMap to composed company_amended.same_as at W5d; W3-4's exemption expires HERE. Page:\n%s", composedCanonical, page)
	}
	if strings.Contains(text, strings.ToLower(legacyStale)) {
		t.Fatalf("W5d-5 (W3-m9): the live muster grouped through the LEGACY ref_alias row to %q — a second resolution path is steering the company line. Page:\n%s", legacyStale, page)
	}
	if strings.Contains(text, strings.ToLower(dupName)) {
		t.Fatalf("W5d-5: the live muster still shows the merged-away duplicate %q as its own group — the composed merge did not reach the grouping. Page:\n%s", dupName, page)
	}
}
