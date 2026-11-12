// muster_test.go — C9's web-layer done-when: the muster view groups
// Company→Workgroup and offers a print path; a reprint of a specific frozen
// visit set never re-resolves through the alias table even after one
// exists; the muster is scoped to this site's own open visits.
package web

import (
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

func newMusterTestServer(t *testing.T, minGroupSize int) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		SiteID: "site-a", DisplayName: "Ice Road A", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 10,
		MusterMinGroupSize: minGroupSize,
	}
	return New(cfg, st), st
}

// checkIn mints a local (degenerate) trip and its expected arrival — the
// degenerate-trip pattern trips_test.go already uses — the simplest domain-level
// way to produce OPEN visit rows at siteID without going through HTTP forms.
func checkIn(t *testing.T, st *store.Store, siteID, driver string, occupants []string) {
	t.Helper()
	trip, err := domain.DispatchLocalCheckIn(st, siteID, 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRK-14", DriverPersonID: driver, OccupantPersonIDs: occupants,"⟧
		VehicleID: w7SeedVehicleID(t, st, siteID, "TRK-14"), DriverPersonID: driver, OccupantPersonIDs: occupants,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.RecordExpectedArrival(st, siteID, 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
}

func getMuster(t *testing.T, srv *Server, path string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec.Code, rec.Body.String()
}

// --- structural hooks -------------------------------------------------
//
// The muster's grouping used to be counted here by counting bare `<h2>`
// elements and grepping for `<h3>Tiny Team`. That is a STRUCTURAL PROXY
// standing on a DESIGN DECISION: it silently required company headings to
// stay bare, class-free h2/h3 for ever, and it broke the moment the page was
// restyled — the same family of defect as F19 (a class name in a stylesheet
// matching where a rendered element was meant to) and its reverse in
// auth_test.go. Worse, it could not distinguish a company heading from any
// other h2 the page might grow, so "2 company groups" and "1 company group
// plus 1 away heading" were the same assertion.
//
// muster.html now marks the grouping explicitly:
//
//	data-company-group="<company name>"  one per company section
//	data-trade-group="<trade name>"      one per listed trade
//
// ⟦LOCK — §94 disposition (3), 2026-09-03⟧ the second line is re-pointed with the
// scanner below; the superseded line is in reports/window-build/S100-ALIAS.md §2.3,
// not here (W8-6 census term, and this file is not allowlisted).
//
// ⚠⚠ THE CLAIM THAT USED TO STAND HERE IS FALSE AND IS NOT RESTORED. It read:
// "NOTHING in the stylesheet selects on those attributes (asserted by
// TestMusterStructuralHooksAreNeverStyled in muster_print_test.go)". [MEASURED
// 2026-09-03, S100-ALIAS.md §5.1b] that guard's hook list at muster_print_test.go:51
// still names the DEAD `data-workgroup-group` and does NOT name `data-trade-group`.
// Styling `[data-trade-group]` and running the guard yields --- PASS: the F19 defect
// it exists to catch goes straight through, and the assertions below silently become
// stylesheet matches with nothing anywhere going red. ⛔ Do not re-assert the coverage
// until muster_print_test.go:51 actually lists the rendered attribute.
//
// The assertions below are strictly stronger than the counts they replace: they name
// the companies and trades, not just how many anonymous headings appeared.

var (
	companyGroupRe = regexp.MustCompile(`data-company-group="([^"]*)"`)
	// ⟦LOCK — §94 disposition (3), 2026-09-03⟧ STALE BASE / LIVE ASSERTION: the BASE was
	// re-pointed, the assertion KEPT. ⚠ THIS SELECTOR IS THE TEST'S EYE, NOT ITS SUBJECT.
	// muster.html:315 renders `data-trade-group` (renamed in 19eafc4, one commit before this
	// base) and muster.html:55 names that attribute set deliberately — the PAGE was renamed and
	// this scanner was not, so it matched nothing and returned [] for every caller.
	// ⚠ The superseded line is preserved VERBATIM in reports/window-build/S100-ALIAS.md §2,
	// NOT here: it carries a W8-6 census term and this file is not allowlisted.
	tradeGroupRe = regexp.MustCompile(`data-trade-group="([^"]*)"`)
)

func attrValues(re *regexp.Regexp, body string) []string {
	var out []string
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// companyGroups names every company section rendered on a muster page, in
// document order.
func companyGroups(body string) []string { return attrValues(companyGroupRe, body) }

// workgroupGroups names every workgroup section LISTED on a muster page —
// a crew folded into the summary group has no such section, which is the
// difference the fold test turns on.
func tradeGroups(body string) []string { return attrValues(tradeGroupRe, body) }

// ⟦LOCK — §60, 2026-09-03⟧ W8 Workgroup→Trade rename fallout: the function was renamed and
// its own doc comment was not. RENAMED, not retired — the successor is declared four lines
// below and proves the same thing.
// ⚠ The superseded line is preserved VERBATIM in reports/window-build/
// ALIAS-DELETION-REPAIR.md §4.10, NOT here: it names a test that no longer exists, and
// citations_test.go:103 fails any comment that does.
// TestMusterLiveGroupsCompanyThenTradeAndOffersAPrintLink covers the
// done-when's basic shape: §11 grouping, and a print affordance reachable
// in one click FROM the live board (the second click is the interaction-
// budget test below).
func TestMusterLiveGroupsCompanyThenTradeAndOffersAPrintLink(t *testing.T) {
	srv, st := newMusterTestServer(t, 0)
	co, err := domain.CreateCompany(st, "site-a", 1, "Kakivik Asset Management", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ DR-W8-2 arity: the company argument is dropped — a trade is a site-wide open set, not scoped to a company. ⟦was: "wg, err := domain.CreateTrade(st, "site-a", 1, co.CompanyID, "Inspection", 0, "default")"⟧
	wg, err := domain.CreateTrade(st, "site-a", 1, "Inspection", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	p1, err := domain.CreatePerson(st, "site-a", 1, "Dana Driver", &co.CompanyID, &wg.TradeID, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	p2, err := domain.CreatePerson(st, "site-a", 1, "Pat Passenger", &co.CompanyID, &wg.TradeID, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	checkIn(t, st, "site-a", p1.PersonID, []string{p2.PersonID})

	code, body := getMuster(t, srv, "/muster")
	if code != 200 {
		t.Fatalf("GET /muster = %d: %s", code, body)
	}
	for _, want := range []string{
		"LIVE",
		"Dana Driver", "Pat Passenger",
		`href="/muster/print?visits=`, // the print affordance, one click away
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("muster page missing %q\n%s", want, body)
		}
	}
	// §11 grouping, asserted structurally rather than by heading shape: ONE
	// company section, named, with the workgroup section nested INSIDE it.
	if got := companyGroups(body); len(got) != 1 || got[0] != "Kakivik Asset Management" {
		t.Fatalf("company grouping: got %v, want exactly [Kakivik Asset Management]\n%s", got, body)
	}
	if got := tradeGroups(body); len(got) != 1 || got[0] != "Inspection" {
		t.Fatalf("workgroup grouping: got %v, want exactly [Inspection]\n%s", got, body)
	}
	// Nested, not merely both present: the workgroup marker must fall
	// between the company marker and the end of its section.
	ci := strings.Index(body, `data-company-group="Kakivik Asset Management"`)
	wi := strings.Index(body, `data-trade-group="Inspection"`)
	if !(ci >= 0 && wi > ci) {
		t.Fatalf("workgroup is not nested under its company (company at %d, workgroup at %d)\n%s", ci, wi, body)
	}
	// ...and the people are inside the workgroup's own list, not loose on
	// the page.
	if !strings.Contains(body[wi:], "Dana Driver") || !strings.Contains(body[wi:], "Pat Passenger") {
		t.Fatalf("crew is not listed under its workgroup:\n%s", body)
	}
}

// TestMusterOnlyShowsThisSitesOpenVisits: the muster is "the sheet someone
// carries outside" of ONE site (design §11) — it must never include another
// site's visits, and must never include a checked-out visit.
func TestMusterOnlyShowsThisSitesOpenVisits(t *testing.T) {
	srv, st := newMusterTestServer(t, 0)
	coA, err := domain.CreateCompany(st, "site-a", 1, "Local Co", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	pA, err := domain.CreatePerson(st, "site-a", 1, "Local Person", &coA.CompanyID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	checkIn(t, st, "site-a", pA.PersonID, nil)

	// A different site's own local check-in, authored into the SAME store
	// (the injected-foreign-event pattern trip_test.go/trips_test.go use to
	// exercise cross-site state without real replication).
	coB, err := domain.CreateCompany(st, "site-b", 1, "Foreign Co", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	pB, err := domain.CreatePerson(st, "site-b", 1, "Foreign Person", &coB.CompanyID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	checkIn(t, st, "site-b", pB.PersonID, nil)

	_, body := getMuster(t, srv, "/muster")
	if !strings.Contains(body, "Local Person") {
		t.Fatalf("site-a's own visit missing from its muster:\n%s", body)
	}
	if strings.Contains(body, "Foreign Person") {
		t.Fatalf("a DIFFERENT site's visit leaked onto site-a's muster:\n%s", body)
	}
}

// TestMusterPrintReprintNeverResolvesEvenAfterADevAlias is the C9 done-when's
// named scenario end to end, through real HTTP handlers and real store-backed
// merge state: after two duplicate companies are merged, the LIVE board shows
// one merged crew while a REPRINT of the visits captured BEFORE the merge
// existed still shows two — proved by reusing the exact visits= querystring
// the pre-merge /muster page generated.
//
// ⟦W5d-5 — POST-LOCK AMENDMENT (plan owner, 2026-08-31). The LIVE half below
// is INVERTED against the version this test shipped with, deliberately.⟧ It
// used to assert that a `ref_alias` row — the C5/M1 dev-alias mechanism —
// merged the two company groups on the live board. **W5d-5 requires the
// opposite in terms**: the muster's company grouping re-points to composed
// `company_amended.same_as`, and W3-4's exemption for that grouping (granted
// only "because its composed source does not exist until W5d") EXPIRES at
// W5d. After the re-point, a `ref_alias` row steering the company line is "a
// second resolution path" — the failure W5d-5 books, not the behaviour it
// wants (see TestW5dMusterCompanyGroupingResolvesComposedNotRefAlias). The
// old assertion encodes pre-W5d behaviour and is SUPERSEDED by a locked
// criterion, which outranks a landed test; it passed at baseline 44e497e and
// reds against W5d's build, and that is correct. The ruling and its
// measurements are recorded in reports/W5-POSTLOCK-AMENDMENTS.md.
//
// ⚠ The REPRINT half is UNTOUCHED and must stay so. It is a DIFFERENT
// guarantee — D-27's frozen snapshot, the C9 not-done-if "a reprint that
// re-resolves fails" — it still holds, and after this amendment it holds
// against BOTH merge mechanisms at once rather than one.
func TestMusterPrintReprintNeverResolvesEvenAfterADevAlias(t *testing.T) {
	srv, st := newMusterTestServer(t, 0)
	kakivik, err := domain.CreateCompany(st, "site-a", 1, "Kakivik", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	kakavik, err := domain.CreateCompany(st, "site-a", 1, "Kakavik", 0, "default") // duplicate, §8.7 case 2
	if err != nil {
		t.Fatal(err)
	}
	alice, err := domain.CreatePerson(st, "site-a", 1, "Alice", &kakivik.CompanyID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := domain.CreatePerson(st, "site-a", 1, "Bob", &kakavik.CompanyID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	checkIn(t, st, "site-a", alice.PersonID, nil)
	checkIn(t, st, "site-a", bob.PersonID, nil)

	// Capture the PRE-ALIAS sheet: the live board's own print link, taken
	// before any alias exists.
	_, preAliasBody := getMuster(t, srv, "/muster")
	if got := companyGroups(preAliasBody); len(got) != 2 {
		t.Fatalf("pre-alias live board: want 2 separate company groups (Kakivik, Kakavik), got %v:\n%s", got, preAliasBody)
	}
	// Precondition (rule 6): the scenario only poses its case if the two
	// groups really are the duplicate pair. If a fixture change ever made
	// these one company, everything below would pass while proving nothing.
	for _, want := range []string{"Kakivik", "Kakavik"} {
		if !slices.Contains(companyGroups(preAliasBody), want) {
			t.Fatalf("precondition: pre-alias board has no %q company group (%v) — the duplicate pair is not set up", want, companyGroups(preAliasBody))
		}
	}
	preAliasPrintHref := extractPrintHref(t, preAliasBody)

	// ⚠⚠ ⟦RETIRED — §60, 2026-09-03⟧ KIND C (site 20) — ⛔ NO SUBSTITUTION, then (1).
	// Retired on its OWN reasoning, not as a copy of its domain-side twin: what is unique
	// here is that the observation is the RENDERED PAGE, so the arm asserted the deleted
	// mechanism did not reach an OFFICER-FACING surface, not merely a map.
	// Superseded lines preserved verbatim:
	//   ⟦was: "// The dev alias route's own mechanism (chunk-plan C5/M1): an ordinary"⟧
	//   ⟦was: "// CreateAlias call linking the duplicate to the canonical."⟧
	//   ⟦was: "if _, err := domain.CreateAlias(st, \"site-a\", 1, kakavik.CompanyID, kakivik.CompanyID, 0, \"default\"); err != nil {"⟧
	//   ⟦was: "\tt.Fatalf(\"create alias: %v\", err)"⟧
	//   ⟦was: "}"⟧
	//   ⟦was: "// ⟦W5d-5⟧ LIVE, post-ref_alias: STILL TWO groups. This is the half that"⟧
	//   ⟦was: "// inverted, and it is the whole content of the re-point — company"⟧
	//   ⟦was: "// grouping no longer reads `ref_alias` at all. (Workgroup grouping still"⟧
	//   ⟦was: "// does, until W8 deletes the mechanism; DR-4 is why the two resolvers are"⟧
	//   ⟦was: "// separate parameters.)"⟧
	//   ⟦was: "_, liveAfterAlias := getMuster(t, srv, \"/muster\")"⟧
	//   ⟦was: "if got := companyGroups(liveAfterAlias); len(got) != 2 { … want STILL 2 company groups … }"⟧
	//   ⟦was: "for _, want := range []string{\"Kakivik\", \"Kakavik\"} { … both duplicates must still stand as their own groups … }"⟧
	//
	// WHAT IT PROVED: that authoring a second-mechanism edge over the duplicate pair left
	// the LIVE RENDERED muster showing two separate company groups.
	// WHICH RULING REMOVED ITS SUBJECT: P-3 §5 — the edge cannot be authored, so the arm
	// would assert "two groups when nothing merges anything", which the PRE-ALIAS assertion
	// twenty lines above already makes against the same page.
	// ⛔ §60 BARS the substitution: a composed AmendCompany edge here merges the groups at
	// this very assertion and inverts it.
	// ⭐ COVERAGE COST — NAMED, and it is the page-level half: "the officer's board does not
	// group through a second resolution path". ⚠ The composed arm below is untouched and
	// still proves the board DOES merge through `company_amended.same_as`, so a build that
	// merges nothing is still caught here; and the reprint arm — this test's headline, per
	// its own name — is untouched.

	// ⟦W5d-5⟧ ...and the mechanism that DOES merge them now: a composed
	// `company_amended.same_as` edge over the same duplicate pair. Asserting
	// only the negative above would leave a build that merges NOTHING green.
	if _, err := domain.AmendCompany(st, "site-a", 1, domain.CompanyAmendment{
		CompanyID: kakavik.CompanyID, SameAs: &kakivik.CompanyID,
	}, 0, "default"); err != nil {
		t.Fatalf("amend company (composed same_as merge): %v", err)
	}

	// LIVE, post-merge: one merged company group.
	_, liveAfter := getMuster(t, srv, "/muster")
	if got := companyGroups(liveAfter); len(got) != 1 || got[0] != "Kakivik" {
		t.Fatalf("post-merge LIVE board: want exactly 1 merged company group [Kakivik] resolved through composed same_as, got %v:\n%s", got, liveAfter)
	}
	if !strings.Contains(liveAfter, "Alice") || !strings.Contains(liveAfter, "Bob") {
		t.Fatalf("post-merge LIVE board missing a merged occupant:\n%s", liveAfter)
	}

	// REPRINT of the pre-alias sheet, fetched AFTER the alias exists: must
	// still show two groups. This is the exact not-done-if the chunk brief
	// names: "a reprint that re-resolves fails."
	code, reprintBody := getMuster(t, srv, preAliasPrintHref)
	if code != 200 {
		t.Fatalf("GET %s = %d: %s", preAliasPrintHref, code, reprintBody)
	}
	if !strings.Contains(reprintBody, "REPRINT") {
		t.Fatalf("reprint page does not label itself REPRINT:\n%s", reprintBody)
	}
	if got := companyGroups(reprintBody); len(got) != 2 {
		t.Fatalf("REPRINT of the pre-alias sheet: want STILL 2 company groups (never re-resolve), got %v:\n%s", got, reprintBody)
	}
	for _, want := range []string{"Kakivik", "Kakavik"} {
		if !slices.Contains(companyGroups(reprintBody), want) {
			t.Fatalf("REPRINT lost the %q group — it re-resolved through the alias table:\n%s", want, reprintBody)
		}
	}
}

// extractPrintHref pulls the /muster/print?visits=... href out of a rendered
// /muster page body.
func extractPrintHref(t *testing.T, body string) string {
	t.Helper()
	const marker = `href="/muster/print?visits=`
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("no print link found in body:\n%s", body)
	}
	rest := body[i+len(`href="`):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		t.Fatalf("malformed print href in body:\n%s", body)
	}
	return rest[:end]
}

// TestMusterFoldsSubThresholdCrewsUnderSummary exercises the numeric
// MusterMinCrewSize fold through the real HTTP path — the same guard as
// domain/muster_test.go's TestMusterFoldingIsNumericNotTextual, but end to
// end through the config value and the template.
func TestMusterFoldsSubThresholdCrewsUnderSummary(t *testing.T) {
	srv, st := newMusterTestServer(t, 3) // threshold 3
	co, err := domain.CreateCompany(st, "site-a", 1, "Small Crew Co", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ DR-W8-2 arity: the company argument is dropped — a trade is a site-wide open set, not scoped to a company. ⟦was: "wg, err := domain.CreateTrade(st, "site-a", 1, co.CompanyID, "Tiny Team", 0, "default")"⟧
	wg, err := domain.CreateTrade(st, "site-a", 1, "Tiny Team", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	p1, err := domain.CreatePerson(st, "site-a", 1, "Solo Worker", &co.CompanyID, &wg.TradeID, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	checkIn(t, st, "site-a", p1.PersonID, nil) // crew of 1, below threshold 3

	_, body := getMuster(t, srv, "/muster")
	if !strings.Contains(body, `data-muster-section="folded"`) {
		t.Fatalf("sub-threshold crew (1 < 3) produced no summary group:\n%s", body)
	}
	if !strings.Contains(body, "Summary group") || !strings.Contains(body, "Solo Worker") {
		t.Fatalf("sub-threshold crew (1 < 3) was not folded under the summary group:\n%s", body)
	}
	// "Tiny Team" still appears as an auditability column inside the Summary
	// table (MusterMember carries CompanyName/WorkgroupName even when
	// pooled) — proving the assertion below is about SECTIONING, not about
	// the name vanishing from the page.
	if !strings.Contains(body, "Tiny Team") {
		t.Fatalf("precondition: the folded crew's workgroup name is not on the page at all, so the sectioning assertion below proves nothing:\n%s", body)
	}
	// What must NOT happen: the workgroup getting its own listed section the
	// way an above-threshold crew does. Asserted on the structural hook, not
	// on heading shape.
	if got := tradeGroups(body); len(got) != 0 {
		t.Fatalf("sub-threshold workgroup got its own listed group %v instead of folding:\n%s", got, body)
	}
	// ...and the folded person is not double-counted onto the roster.
	if got := companyGroups(body); len(got) != 0 {
		t.Fatalf("a fully folded muster still rendered company sections %v:\n%s", got, body)
	}
	// The fold does not change who is on site: the total still counts them.
	if !strings.Contains(body, `data-tally-count="on-site">1<`) {
		t.Fatalf("a folded crew fell out of the on-site total:\n%s", body)
	}
	// Regression (U3): with every crew folded, Companies is empty while
	// Summary is not. The page must NOT then claim nobody is on site.
	if strings.Contains(body, "Nobody recorded on site") {
		t.Fatalf("an all-folded muster claimed nobody is on site while listing people:\n%s", body)
	}
}

// TestMusterPrintFailsLoudlyOnUnknownVisitID: a print URL naming a visit id
// the store cannot resolve must be refused, never rendered as a shorter
// muster sheet that silently omits whoever it couldn't find (verification
// finding, C9 FIX 2 — the domain-level guard is
// domain.TestVisitsByIDErrorsRatherThanSilentlyDroppingAnUnknownID).
func TestMusterPrintFailsLoudlyOnUnknownVisitID(t *testing.T) {
	srv, st := newMusterTestServer(t, 0)
	co, err := domain.CreateCompany(st, "site-a", 1, "Co", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	p, err := domain.CreatePerson(st, "site-a", 1, "Real Person", &co.CompanyID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	checkIn(t, st, "site-a", p.PersonID, nil)

	code, body := getMuster(t, srv, "/muster/print?visits=this-visit-id-does-not-exist")
	if code == 200 {
		t.Fatalf("GET /muster/print with an unresolvable visit id = 200, want a refusal; body:\n%s", body)
	}
	// Nothing that could be mistaken for a muster sheet may come back: no
	// company grouping, and none of the sheet's sections at all.
	if got := companyGroups(body); len(got) != 0 {
		t.Fatalf("an unresolvable visit id rendered company groups %v — a silently incomplete muster sheet:\n%s", got, body)
	}
	if strings.Contains(body, "data-muster-section=") {
		t.Fatalf("an unresolvable visit id must never render a (silently incomplete) muster sheet:\n%s", body)
	}
}

// --- interaction-budget test (§6: printable in <=2 interactions from any
// screen; verification found the hand-counted table wrong for / and /trips,
// which actually needed 3: no test in the repo asserted the count at all,
// which is why the discrepancy survived. This BFS's the REAL rendered
// <a href> links on each page (plus a `window.print()` trigger as the
// terminal action) so the count is measured, not narrated. ---

var hrefRe = regexp.MustCompile(`href="([^"]+)"`)

func extractHrefs(body string) []string {
	var out []string
	for _, m := range hrefRe.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// minClicksToPrint BFS's real links starting at `start`, counting a click on
// a rendered `onclick="window.print()"` trigger as the terminal action.
// Returns -1 if no such path exists within maxDepth.
func minClicksToPrint(t *testing.T, srv *Server, start string, maxDepth int) int {
	t.Helper()
	type node struct {
		path  string
		depth int
	}
	seen := map[string]bool{start: true}
	queue := []node{{start, 0}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		code, body := getMuster(t, srv, cur.path)
		if code != 200 {
			continue // a dead/erroring link is not a valid path to print
		}
		// The terminal state is arriving at a page that IS the sheet and
		// carries a print control that works WITHOUT a script (V-02).
		//
		// This used to look for `onclick="window.print()"`, which counted a
		// JS-only control as the print action. On the contract-locked
		// JS-off path that button does nothing at all, so the old measure
		// reported a reachable print where a scripting-off browser had
		// none. `.print-instruction` is the control that always works; the
		// one-click button is served hidden beside it and revealed by the
		// script, so both are present and only one of them is load-bearing.
		if strings.Contains(body, `class="print-instruction"`) {
			return cur.depth + 1 // the keystroke (or the enhanced button's click)
		}
		if cur.depth >= maxDepth {
			continue
		}
		for _, href := range extractHrefs(body) {
			if seen[href] || !strings.HasPrefix(href, "/") {
				continue
			}
			seen[href] = true
			queue = append(queue, node{href, cur.depth + 1})
		}
	}
	return -1
}

func TestPrintReachableInAtMostTwoInteractionsFromEveryScreen(t *testing.T) {
	srv, st := newMusterTestServer(t, 0)
	co, err := domain.CreateCompany(st, "site-a", 1, "Co", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	p, err := domain.CreatePerson(st, "site-a", 1, "Person", &co.CompanyID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	checkIn(t, st, "site-a", p.PersonID, nil)

	for _, screen := range []string{"/status", "/trips", "/muster", "/muster/print"} {
		clicks := minClicksToPrint(t, srv, screen, 3)
		if clicks < 0 {
			t.Fatalf("no reachable print action found from %s within 3 hops", screen)
		}
		if clicks > 2 {
			t.Fatalf("from %s: printing takes %d interaction(s), want <= 2 (design section 11 / section 6 budget)", screen, clicks)
		}
		t.Logf("%s: %d interaction(s) to print", screen, clicks)
	}
}
