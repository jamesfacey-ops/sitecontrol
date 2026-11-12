// intake_test.go — the person-intake path (intake.go / templates/intake.html).
//
// WHAT THESE TESTS ARE FOR, stated plainly so a later reader can tell whether
// one has stopped defending anything: intake exists to stop three specific
// things, each of which the system does today.
//
//  1. One crew becoming N companies, because the gate's inline path calls
//     CreateCompany unconditionally and CreateCompany does not dedup by name.
//     That splits one muster group into N sub-minimum groups, all of which
//     fold into the flat summary bucket.
//  2. A person with no company reaching the muster silently. They can never
//     be grouped, and nothing anywhere says so.
//  3. One human becoming two identities when a post enrols somebody the
//     other post already enrolled. MusterLive alias-resolves company and
//     workgroup ids and NEVER person ids, so that one cannot be merged away.
//
// Every test below names which of those it defends.
package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// --- helpers ---

// intakeForm2 is the shared intake POST body every test in this package that
// enrols somebody is built from. What a test VARIES is a parameter; what
// intake merely REQUIRES is filled in here — which is why the phone below is
// a constant rather than a seventh argument.
//
// ⟦post-lock §11 — W5b⟧ THE PHONE KEY. Added 2026-09-01 by the plan-owner
// pass carrying out post-lock amendment §11's ruling (reports/W5-POSTLOCK-
// AMENDMENTS.md), on evidence routed by W5b's builder at
// reports/window-build/W5b-build.md §4 — routed, never edited by the builder
// whose code it made pass. W5b made a phone number mandatory AT THIS ONE
// HANDLER ([[D-52]]; the refusal is intake.go's last check before the
// authoring line), so every fixture in this package written before that field
// existed posted a body intake now answers 409 — including W5a's own ledger
// tests, whose evidence was unreproducible until this line landed. The ruling:
// those tests are SUPERSEDED fixtures, not assertions that disagree with W5b —
// each failed at its setup or first assertion, none reached the property it
// defends, and no property any of them defends is contradicted. Nothing else
// in any of them was touched.
//
// So a reader meeting a phone number in a fixture about company attachment and
// duplicate names is looking at W5b's requirement, not at anything these tests
// assert. The tests that are ABOUT the phone do not rely on this value: they
// set their own over it (TestNoPhoneNumberReachesTheMusterPrintOrRoster) or build
// their own body entirely (w5bForm, whose w5bNoPhone sentinel is the only way
// to post no phone key at all — see TestW5bIntakeNeverEnrolsAPersonWithoutAPhone).
// ⚠⚠ ⟦LOCK — §60, 2026-09-03⟧ W8 Workgroup→Trade rename — A THIRD MASKED DEFECT, and the
// worst-shaped of the three this pass found. These are FORM KEYS: plain map strings, so the
// compiler cannot see them, and they had gone stale against intake.go:470-471, which reads
// `trade_id` / `new_trade`. ⭐ PRODUCED, not reasoned: with the old keys,
// TestIntakeAddsANewCompanyWhenItGenuinelyIsNew reported "the new work group was not
// created" and TestARefusedIntakeAuthorsNothingAtAll got 303 where it wanted a refusal —
// because EVERY intake fixture in this file was silently posting NO trade at all.
// ⚠ Any intake test that did not assert on the trade was passing while never exercising the
// trade path. This is a fixture repair, not an assertion change: it restores what the
// fixture already meant.
// ⟦was: "\t\t\"workgroup_id\": {workgroupID}, \"new_workgroup\": {newWorkgroup},"⟧
func intakeForm2(last, first, companyID, newCompany, tradeID, newTrade string) url.Values {
	return url.Values{
		"last_name": {last}, "first_name": {first},
		"company_id": {companyID}, "new_company": {newCompany},
		"trade_id": {tradeID}, "new_trade": {newTrade},
		"phone": {"(907) 555-0163"}, // ⟦post-lock §11 — W5b⟧ see above
	}
}

func companyIDNamed(t *testing.T, srv *Server, name string) string {
	t.Helper()
	cs, err := domain.Companies(srv.Store)
	if err != nil {
		t.Fatal(err)
	}
	var hits []string
	for _, c := range cs {
		if c.Name == name {
			hits = append(hits, c.CompanyID)
		}
	}
	if len(hits) != 1 {
		t.Fatalf("test setup: %d companies named %q, want exactly 1 (ids %v)", len(hits), name, hits)
	}
	return hits[0]
}

func personNamed(t *testing.T, srv *Server, name string) []domain.Person {
	t.Helper()
	ps, err := domain.Persons(srv.Store)
	if err != nil {
		t.Fatal(err)
	}
	var out []domain.Person
	for _, p := range ps {
		if p.Name == name {
			out = append(out, p)
		}
	}
	return out
}

func countRows(t *testing.T, srv *Server) (persons, companies, workgroups int) {
	t.Helper()
	ps, err := domain.Persons(srv.Store)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := domain.Companies(srv.Store)
	if err != nil {
		t.Fatal(err)
	}
	// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ W8 Workgroup→Trade rename: the loader is re-pointed. This site reads only the row COUNT, so the assertion is preserved exactly. ⟦was: "ws, err := domain.Workgroups(srv.Store)"⟧
	ws, err := domain.Trades(srv.Store)
	if err != nil {
		t.Fatal(err)
	}
	return len(ps), len(cs), len(ws)
}

// --- (1) one crew, one company ---

// TestIntakeAttachesToAnExistingCompanyInsteadOfMintingASecondOne is the
// whole reason intake is a picker and not a text box. The gate's inline path
// mints a fresh company from typed text every single time.
func TestIntakeAttachesToAnExistingCompanyInsteadOfMintingASecondOne(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	companyID := companyIDNamed(t, srv, "Kakivik Asset Management")
	_, companiesBefore, _ := countRows(t, srv)

	for _, who := range [][2]string{{"Ferguson", "Mary"}, {"Okpik", "Sam"}, {"Vale", "Jo"}} {
		rec := postForm(t, srv, "/roster/intake", intakeForm2(who[0], who[1], companyID, "", "", ""))
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("intake %s = %d, want 303: %s", who[0], rec.Code, rec.Body.String())
		}
	}

	_, companiesAfter, _ := countRows(t, srv)
	if companiesAfter != companiesBefore {
		t.Errorf("three enrolments under one company created %d new company row(s), want 0 — "+
			"a second row with the same name splits one crew into two groups on the muster",
			companiesAfter-companiesBefore)
	}
	for _, name := range []string{"Ferguson, Mary", "Okpik, Sam", "Vale, Jo"} {
		hits := personNamed(t, srv, name)
		if len(hits) != 1 {
			t.Fatalf("%d roster rows for %q, want 1", len(hits), name)
		}
		if hits[0].CompanyID == nil || *hits[0].CompanyID != companyID {
			t.Errorf("%q attached to company %v, want the existing %s", name, hits[0].CompanyID, companyID)
		}
	}
}

// TestIntakeRefusesASecondCompanyWithAnExistingName closes the other half:
// picking "add a new company" and typing a name that is already on the list.
func TestIntakeRefusesASecondCompanyWithAnExistingName(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	_, before, _ := countRows(t, srv)

	rec := postForm(t, srv, "/roster/intake",
		intakeForm2("Ferguson", "Mary", "__new__", "kakivik asset management", "", ""))
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate company name = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if _, after, _ := countRows(t, srv); after != before {
		t.Errorf("a refused intake minted %d company row(s), want 0", after-before)
	}
	if ps := personNamed(t, srv, "Ferguson, Mary"); len(ps) != 0 {
		t.Errorf("a refused intake enrolled the person anyway")
	}
}

// TestIntakeAddsANewCompanyWhenItGenuinelyIsNew is the positive control for
// the two tests above. Without it, "intake refuses new companies" would
// satisfy them by refusing every company.
func TestIntakeAddsANewCompanyWhenItGenuinelyIsNew(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	_, before, _ := countRows(t, srv)

	rec := postForm(t, srv, "/roster/intake",
		intakeForm2("Ferguson", "Mary", "__new__", "Doyon Drilling", "__new__", "Drill Crew"))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("new company intake = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	if _, after, _ := countRows(t, srv); after != before+1 {
		t.Fatalf("new-company intake created %d companies, want exactly 1", after-before)
	}
	// ⟦LOCK — §60, 2026-09-03⟧ KIND A (intake site :190) — the FILED-UNDER arm is retired;
	// the CREATED arm is kept and re-pointed. ⚠ The two are separated deliberately: only
	// one of them is a fossil, and retiring both would have cost live coverage.
	// ⟦was: "newCo := companyIDNamed(t, srv, \"Doyon Drilling\")"⟧ — the binding goes with the
	// arm that used it; the CALL is kept because companyIDNamed FATALS unless exactly one
	// company of that name exists, so it is itself an assertion.
	companyIDNamed(t, srv, "Doyon Drilling")
	// ⟦was: "ws, err := domain.Workgroups(srv.Store)"⟧
	ws, err := domain.Trades(srv.Store)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range ws {
		if w.Name == "Drill Crew" {
			found = true
			// ⚠⚠ ⟦RETIRED — §60, 2026-09-03⟧ KIND A. Superseded lines preserved verbatim:
			//   ⟦was: "if w.CompanyID != newCo {"⟧
			//   ⟦was: "\tt.Errorf(\"new work group filed under company %s, want the new %s\", w.CompanyID, newCo)"⟧
			//   ⟦was: "}"⟧
			// WHAT IT PROVED: that intake filed the newly created work group UNDER the newly
			// created company.
			// WHICH RULING REMOVED ITS SUBJECT: ⟦DR-W8-2⟧ — "a trade is a site-wide open set
			// and is not scoped to a company… its absence is asserted (W8-2(a))". `Trade` has
			// no CompanyID field at all (refdata.go:202-214), so the relationship this arm
			// asserted does not merely lack a value — it no longer exists as a concept.
			// ⭐ COVERAGE COST: nil in the direction that matters. W8-2(a) asserts the ABSENCE
			// of the company predicate, so the property this arm defended is now the property
			// W8 forbids. The arm immediately below — that the trade was created at all — is
			// the live half and is KEPT.
		}
	}
	if !found {
		t.Error("the new work group was not created")
	}
}

// --- (2) nobody reaches the muster ungroupable, silently ---

// TestIntakeRefusesAPersonWithNoCompany. Intake is the DELIBERATE lane and it
// insists; the gate is the fast lane and does not. That asymmetry is the
// answer to "26 people at -30" and it only works if this half holds.
func TestIntakeRefusesAPersonWithNoCompany(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	before, _, _ := countRows(t, srv)

	rec := postForm(t, srv, "/roster/intake", intakeForm2("Ferguson", "Mary", "", "", "", ""))
	if rec.Code != http.StatusConflict {
		t.Fatalf("intake with no company = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if after, _, _ := countRows(t, srv); after != before {
		t.Errorf("a refused intake enrolled %d person row(s), want 0", after-before)
	}
	if !strings.Contains(rec.Body.String(), "read aloud during an evacuation") {
		t.Error("the refusal does not say WHY a company is needed; the copy rule is that an error says what went wrong and what to do")
	}
}

// TestTheRosterListsEverybodyWhoStillNeedsTheirDetails. The completion queue
// is DERIVED from person.company_id being absent, never from a stored flag —
// so it cannot disagree with the rows it describes, and nobody has to
// remember to clear it.
func TestTheRosterListsEverybodyWhoStillNeedsTheirDetails(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	// The fixture's three people all HAVE a company (precondition: without
	// this, "the queue lists the one with no company" could pass by listing
	// everybody).
	for _, p := range personNamed(t, srv, "Dana Driver") {
		if p.CompanyID == nil {
			t.Fatal("test setup: the fixture person already has no company, so this test cannot pose its case")
		}
	}
	// Somebody added at the window, the way checkin.go's resolvePersonSlot
	// does it when no company was typed.
	if _, err := domain.CreatePerson(st, "site-a", 1, "Hurried, Harry", nil, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}

	body := rawPage(t, srv, "/roster")
	queue := sectionBetween(t, body, `id="needs-completion"`, "</table>")
	if !strings.Contains(queue, "Hurried, Harry") {
		t.Errorf("the completion queue does not list the person with no company on file:\n%s", queue)
	}
	if strings.Contains(queue, "Dana Driver") {
		t.Errorf("the completion queue lists somebody who already HAS a company — it is not deriving anything:\n%s", queue)
	}
}

// --- (3) one human, one identity ---

// TestIntakeRefusesADuplicateNameUntilItIsAffirmed. At the window an
// unrecognised name is created, because over-reporting is the safe direction
// there. At intake it is refused, because the likeliest cause is that the
// person is already enrolled at the other post and the link is behind — and
// a second person id for one human cannot be merged away on the muster.
func TestIntakeRefusesADuplicateNameUntilItIsAffirmed(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	companyID := companyIDNamed(t, srv, "Kakivik Asset Management")

	if rec := postForm(t, srv, "/roster/intake", intakeForm2("Ferguson", "Mary", companyID, "", "", "")); rec.Code != http.StatusSeeOther {
		t.Fatalf("test setup: first enrolment = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	if got := len(personNamed(t, srv, "Ferguson, Mary")); got != 1 {
		t.Fatalf("test setup: %d rows for Ferguson, Mary after one enrolment, want 1", got)
	}

	rec := postForm(t, srv, "/roster/intake", intakeForm2("Ferguson", "Mary", companyID, "", "", ""))
	if rec.Code != http.StatusConflict {
		t.Fatalf("second enrolment of the same name = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if got := len(personNamed(t, srv, "Ferguson, Mary")); got != 1 {
		t.Fatalf("the refused duplicate was enrolled anyway: %d rows", got)
	}
	if !strings.Contains(rec.Body.String(), `name="same_name"`) {
		t.Error("the duplicate refusal does not offer the affirmation, so a genuine second person of the same name has no way through")
	}

	affirmed := intakeForm2("Ferguson", "Mary", companyID, "", "", "")
	affirmed.Set("same_name", "1")
	if rec := postForm(t, srv, "/roster/intake", affirmed); rec.Code != http.StatusSeeOther {
		t.Fatalf("affirmed same-name enrolment = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	if got := len(personNamed(t, srv, "Ferguson, Mary")); got != 2 {
		t.Errorf("%d rows for Ferguson, Mary after an affirmed second enrolment, want 2", got)
	}
}

// TestTheAffirmationIsOfferedONLYOnTheRefusalThatNeedsIt. A permanently
// visible "do it anyway" tick trains click-through, which degrades the one
// place the tick matters.
func TestTheAffirmationIsOfferedONLYOnTheRefusalThatNeedsIt(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	if body := rawPage(t, srv, "/roster"); strings.Contains(body, `name="same_name"`) {
		t.Error("the same-name override is on the page before anything was refused")
	}
	rec := postForm(t, srv, "/roster/intake", intakeForm2("Ferguson", "Mary", "", "", "", ""))
	if rec.Code != http.StatusConflict {
		t.Fatalf("test setup: no-company intake = %d, want 409", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `name="same_name"`) {
		t.Error("the same-name override is offered on a refusal that has nothing to do with duplicate names")
	}
}

// TestTheRosterSaysWhichPostEnrolledEachPerson. Ice Road is the only
// entrance, so a person enrolled at the Pad is an anomaly worth seeing. It
// needed no new field: person.site_id is the authoring site, structurally.
func TestTheRosterSaysWhichPostEnrolledEachPerson(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	if srv.Cfg.SiteID != "site-a" {
		t.Fatalf("test setup: this post is %q, expected site-a", srv.Cfg.SiteID)
	}
	// A person whose record was authored at the OTHER post — the ordinary
	// case, since everybody comes through the entrance.
	if _, err := domain.CreatePerson(st, "site-b", 1, "Elsewhere, Ed", nil, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	body := rawPage(t, srv, "/roster")
	table := sectionBetween(t, body, `id="roster-table"`, "</table>")
	// The RENDERED CELL, not a bare substring: every person id begins with
	// its authoring site, so `strings.Contains(table, "site-b")` is
	// satisfied by the row's data-person-id whatever the column says — and
	// a mutation pinning every row to this post survived on exactly that.
	// James, 2026-10-04 (cluster I): the cell names the post by DISPLAY name
	// where config has one; these two fixtures' names are asserted via
	// Cfg.SiteName so a raw id here would be the regression.
	// ⟦was: "<code>site-b</code>" / "<code>site-a</code>"⟧
	if !strings.Contains(table, "<code>"+srv.Cfg.SiteName("site-b")+"</code>") {
		t.Errorf("the roster does not show that a person was enrolled at the other post:\n%s", table)
	}
	if !strings.Contains(table, "<code>"+srv.Cfg.SiteName("site-a")+"</code>") {
		t.Errorf("the roster does not show that a person was enrolled at THIS post — the column would be true by having only one value:\n%s", table)
	}
}

// --- what the person box displays ---

// TestThePersonBoxCarriesTheCompanyForLastFirstCompany. James: "the only
// things needed to be displayed on the person box is the name and company in
// last, first, company format."
func TestThePersonBoxCarriesTheCompanyForLastFirstCompany(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	companyID := companyIDNamed(t, srv, "Kakivik Asset Management")
	if rec := postForm(t, srv, "/roster/intake", intakeForm2("Ferguson", "Mary", companyID, "", "", "")); rec.Code != http.StatusSeeOther {
		t.Fatalf("test setup: intake = %d, want 303: %s", rec.Code, rec.Body.String())
	}

	body := w5aGetAuthed(t, srv, "/gate/vehicle") // PII-1: payload is authenticated-only
	if !strings.Contains(body, `id="roster-data"`) {
		t.Fatalf("GET /gate/vehicle carries no roster-data block")
	}
	blob := sectionBetween(t, body, `id="roster-data"`, "</script>")
	if !strings.Contains(blob, `"name":"Ferguson, Mary"`) {
		t.Errorf("the roster payload does not carry the name in Last, First form:\n%s", blob)
	}
	if !strings.Contains(blob, `"company":"Kakivik Asset Management"`) {
		t.Errorf("the roster payload does not carry the company, so the person box cannot show Last, First, Company:\n%s", blob)
	}
	// The label is BUILT from those two and from nothing else. This is the
	// structural hook, not a text scan of rendered output: personLabel is
	// the only place the box's text is composed.
	if !strings.Contains(body, "function personLabel(it)") ||
		!strings.Contains(body, "it.name + ', ' + it.company") {
		t.Error("the person box no longer composes its label from name and company alone")
	}
}

// TestThePersonBoxNeverShowsAWorkgroup. Explicitly ruled: name and company,
// nothing else.
func TestThePersonBoxNeverShowsAWorkgroup(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	// Precondition: the fixture people DO have a work group, so its absence
	// from the payload means something.
	// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ W8 Workgroup→Trade rename: the loader is re-pointed. This site reads only each row's Name, so the assertion is preserved exactly. ⟦was: "ws, err := domain.Workgroups(srv.Store)"⟧
	ws, err := domain.Trades(srv.Store)
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) == 0 {
		t.Fatal("test setup: no work groups exist, so this test cannot pose its case")
	}
	req := httptest.NewRequest("GET", "/gate/vehicle", nil)
	req.AddCookie(loginCookie(t, srv, testLoginPassword))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	blob := sectionBetween(t, body, `id="roster-data"`, "</script>")
	for _, w := range ws {
		if strings.Contains(blob, w.Name) {
			t.Errorf("the person box payload carries work group %q; the box shows name and company only", w.Name)
		}
	}
}

// --- personal data ---

// TestNoPhoneNumberReachesTheMusterPrintOrRoster — renamed under R-24,
// James, 2026-09-13 ⟦was: "TestNoPhoneNumberIsAskedForOrKeptAnywhere"⟧. The
// OLD name asserted phone is never asked for or kept ANYWHERE, which R-24 made
// plainly false: phone IS deliberately asked for (the orientation intake
// form) and deliberately kept (on the person record, and now shown back on
// /gate — see criterion change 3 below). The claim THIS test defends is
// narrower and is what actually still holds: nothing a caller submits as a
// phone number is echoed onto the muster, the printed sheet, the roster
// page, or any machine-readable surface. Its author called the shape of
// this guard "the one that has to keep holding once a phone number does
// exist", and they were right — [[D-56]] clause 2 vindicates it in those
// terms; R-24 narrows WHERE it holds, not THAT it holds.
//
// ⚠️ THREE RECORDED CRITERION CHANGES. None is an edit to make code pass;
// all three are named in the record BEFORE the edit that made them true
// (D-56 clause 2 / W5b-BUILD-CRITIQUE §7 row 2 / W5-POSTLOCK-AMENDMENTS
// §14.5 for the first two; R-24, design/REQUIREMENTS-FROM-JAMES-2026-09-09.md,
// for the third).
//
//  1. The "intake does not ASK for one" half is STRUCK. It rested on "there
//     is nowhere to store it", and W5b gave person a phone column, so the
//     premise is gone: the form now asks for something it really does record.
//     Only that half is superseded — W5b-BUILD-CRITIQUE §7 row 2: "Strike the
//     name=\"phone\" assertion ONLY."
//  2. The sweep's fifth surface, /debug/projection-dump, STAYS SWEPT —
//     D-56 forbids scoping it out in terms — but a substring sweep alone
//     would now be VACUOUS there, and that trap is the whole reason this note
//     exists. The route was removed, and GET / is a Go 1.22 ServeMux
//     CATCH-ALL, so that path answers 200 with the status page: the number is
//     absent from the response because the response is a different page, not
//     because a dump stopped carrying it. So this surface carries an
//     extra assertion below that the response is not a projection dump at
//     all. Without it, re-registering the route would leave this test green.
//     ⚠️ This fixture runs with DevRoutes OFF, so it alone cannot tell a
//     REMOVED route from one gated behind a flag that is `true` in all four
//     shipped TOMLs. TestNoProjectionDumpRouteExists (web_test.go) is the
//     companion that closes that gap: same assertions, DevRoutes: true.
//  3. ⟦R-24, James, 2026-09-13⟧ /gate was STRUCK from the routed-page
//     sweep, by ruling: James, shown that this test's sweep of /gate was why
//     a type-ahead pick there always left the Phone box blank, said "the
//     screen will not be visable to anyone but the guard so dont keep it
//     hidden." ⚠ THE PREMISE RECORDED WITH THAT RULING WAS FALSE: it said
//     /gate is "behind a login". GET /gate and /gate/vehicle are UNGATED by
//     design (§12) and renderGate had no session check, so the roster
//     payload (name + employer + phone, every person) went to anyone — a
//     regression of D-56 (finding PII-1). CORRECTED (PII-1): /gate and
//     /gate/vehicle are back IN the sweep, fetched UNAUTHENTICATED, and
//     must carry no phone number; an authenticated /gate keeps the phone
//     (TestGateRosterPayloadCarriesPhoneForAKnownPerson). The real rule is
//     "phone only to an authenticated session", not "/gate is behind a
//     login". /roster, /muster, /muster/print and /debug/projection-dump
//     forbid a phone number outright, authenticated or not.
func TestNoPhoneNumberReachesTheMusterPrintOrRoster(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	companyID := companyIDNamed(t, srv, "Kakivik Asset Management")

	const number = "9075550137"
	form := intakeForm2("Ferguson", "Mary", companyID, "", "", "")
	form.Set("phone", number)
	form.Set("phone_number", number)
	if rec := postForm(t, srv, "/roster/intake", form); rec.Code != http.StatusSeeOther {
		t.Fatalf("test setup: intake = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	// Precondition: the person really was enrolled, so the sweep below is
	// sweeping pages that actually hold this person.
	if got := len(personNamed(t, srv, "Ferguson, Mary")); got != 1 {
		t.Fatalf("test setup: %d rows for the enrolled person, want 1", got)
	}

	// ⚠ THREE paths here, FOUR surfaces swept — the fourth is swept eighteen
	// lines below, off the same recorder that carries the two D-56
	// assertions. D-56 forbids scoping this sweep out in terms and it is NOT
	// scoped out; what moved is only WHICH FETCH that fourth surface is read
	// from.
	// ⟦was: "\t\tfor _, path := range []string{\"/roster\", \"/gate\", \"/muster\", \"/muster/print\", \"/debug/projection-dump\"} {"⟧
	// ⟦was, again — R-24, James, 2026-09-13: "\t\tfor _, path := range []string{\"/roster\", \"/gate\", \"/muster\", \"/muster/print\"} {"⟧
	// The second change is /gate coming OUT — see criterion change 3 in the
	// doc comment above; it is not a re-run of the S95 flake this note was
	// originally written to explain, and the two ⟦was⟧ lines record both,
	// in order, rather than overwriting one with the other.
	//
	// WHY THE FIFTH-SURFACE SPLIT MOVED [MEASURED — reports/window-build/S95-PHONE-FLAKE.md]:
	// rawPage t.Fatalf's on any non-200 (layout_test.go:32-34), and
	// /debug/projection-dump has NO ROUTE — it falls through to the GET /
	// catch-all, i.e. the STATUS PAGE. So any defect on an unrelated page
	// turned this privacy criterion red AND killed it before either D-56
	// assertion below could run. That is exactly what happened on
	// 2026-09-03: status.html read a config field W8-21 had renamed,
	// html/template binds at EXECUTION so build and vet stayed silent, and
	// this test reported "GET /debug/projection-dump = 500" — a status-page
	// defect wearing a phone-privacy test's name, with its real assertions
	// unexecuted.
	// ⭐ This is STRICTLY STRONGER than what it replaces: that surface's body
	// is now swept whatever the status code, where before a non-200 aborted
	// the test. Nothing is asserted less.
	// ⚠ The THREE routed pages KEEP rawPage's 200-or-Fatal contract
	// deliberately: a non-200 on /roster, /muster or /muster/print IS a
	// defect in a page this test is about, and failing loudly there is
	// right. /gate is no longer one of them (criterion change 3) — a
	// non-200 there is still a real defect, just not this test's to catch;
	// it is covered by /gate's own page tests (gate_test.go).
	for _, path := range []string{"/roster", "/muster", "/muster/print"} {
		body := rawPage(t, srv, path)
		if strings.Contains(body, number) {
			t.Errorf("%s carries a phone number. A printed evacuation roster circulating with everyone's number on it is not something to ship by default", path)
		}
	}
	// PII-1: /gate and /gate/vehicle are ungated (§12), so they are swept
	// UNAUTHENTICATED (rawPage sends no cookie). Precondition: the same
	// pages, WITH a session, do carry the number — otherwise this sweep
	// could pass because the fixture never put the phone in the payload.
	cookie := loginCookie(t, srv, testLoginPassword)
	for _, path := range []string{"/gate", "/gate/vehicle"} {
		body := rawPage(t, srv, path)
		if strings.Contains(body, number) {
			t.Errorf("%s carries a phone number to an UNAUTHENTICATED request (PII-1 / D-56)", path)
		}
		if strings.Contains(body, "Ferguson") {
			t.Errorf("%s carries the roster (a person's name) to an UNAUTHENTICATED request (PII-1 / D-56)", path)
		}
		req := httptest.NewRequest("GET", path, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if !strings.Contains(rec.Body.String(), number) {
			t.Errorf("test precondition: authenticated %s does not carry the phone, so the unauthenticated sweep proves nothing", path)
		}
	}

	// The fourth surface, made non-vacuous — see criterion change 2 in the
	// doc comment. This fetch now carries the substring sweep AS WELL AS the
	// two dump-shape assertions; the sweep proves the number is not in whatever
	// the mux answered with, and the two below prove what it answered with is
	// not a dump. Assert all three, and NONE of them on a status code: the
	// catch-all makes "want 404" and "want 200" equally true whether the route
	// exists or not — which is the rule the five-path loop originally used to
	// break, before that loop was split into this fetch plus the three-path
	// (was four, R-24 struck /gate) loop above.
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/debug/projection-dump", nil))
	if strings.Contains(rec.Body.String(), number) {
		t.Errorf("/debug/projection-dump carries a phone number. A printed evacuation roster circulating with everyone's number on it is not something to ship by default")
	}
	if ct := rec.Header().Get("Content-Type"); strings.HasPrefix(ct, "application/json") {
		t.Errorf("/debug/projection-dump answered content-type %q — the dump route is registered again, so the sweep above is testing the wrong page (D-56)", ct)
	}
	if strings.Contains(rec.Body.String(), `"person":[`) {
		t.Errorf("/debug/projection-dump served a person projection — every enrolled worker's row, phone column included, to an unauthenticated caller (D-56)")
	}
}

// TestGateRosterPayloadCarriesPhoneForAKnownPerson ⟦R-24, James, 2026-09-13⟧
// is the other half of the ruling proved above by absence: the embedded
// roster-data payload (id="roster-data" — rosterPerson's JSON, gate.go) is
// the ONLY thing gate.html's scripted pick (attachAutocomplete/showKnown)
// reads a known person's fields from, so this is what a pick actually
// fills the Phone box FROM. JS does not run in this suite, so this asserts
// the server-side half directly: the payload now carries phone for a
// person who has one on file, on the same "show, don't ask" terms Position
// already had (rosterPerson.Position/.Phone, gate.go) — James: "the
// information only needs to be added in one location", now true of phone
// as well — for an AUTHENTICATED request only (PII-1).
// TestNoPhoneNumberReachesTheMusterPrintOrRoster, immediately above, is
// this test's negative twin: the same fact must NOT reach /gate logged out,
// /roster, /muster, /muster/print or /debug/projection-dump.
func TestGateRosterPayloadCarriesPhoneForAKnownPerson(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	companyID := companyIDNamed(t, srv, "Kakivik Asset Management")

	const number = "9075550199"
	form := intakeForm2("Okonek", "Ray", companyID, "", "", "")
	form.Set("phone", number)
	form.Set("phone_number", number)
	if rec := postForm(t, srv, "/roster/intake", form); rec.Code != http.StatusSeeOther {
		t.Fatalf("test setup: intake = %d, want 303: %s", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest("GET", "/gate/vehicle", nil)
	req.AddCookie(loginCookie(t, srv, testLoginPassword))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	blob := sectionBetween(t, body, `id="roster-data"`, "</script>")
	if !strings.Contains(blob, number) {
		t.Fatalf("the roster-data payload does not carry the phone number on file for a known person — a type-ahead pick has nothing to fill the Phone box from:\n%s", blob)
	}
}

// --- the shell, and scripting off ---

// TestTheRosterPageComesOffTheSharedShellExactlyOnce mirrors
// TestEveryPageRendersTheSharedShellExactlyOnce for this route. That test's
// fixture map is maintained by hand, so a new page is invisible to it until
// somebody adds it; this asserts the same four invariants here meanwhile.
func TestTheRosterPageComesOffTheSharedShellExactlyOnce(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	body := rawPage(t, srv, "/roster")
	for _, c := range []struct {
		needle string
		what   string
	}{
		{"<!doctype html>", "doctype declaration"},
		{"<style>", "inlined stylesheet"},
		{`<header class="topbar">`, "top bar"},
		{`<main class="page`, "content column"},
	} {
		if got := strings.Count(body, c.needle); got != 1 {
			t.Errorf("/roster: %d %s(s), want exactly 1", got, c.what)
		}
	}
}

// TestTheIntakeFormWorksWithScriptingOff. The page ships NO script, so the
// scripted and unscripted renderings are the same bytes — but the trap it
// walked into once is subtler than that: the shared .new-fields class is
// display:none until a script reveals it, so a "type the new company here"
// box wearing that class is invisible on exactly the path that has no script.
func TestTheIntakeFormWorksWithScriptingOff(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	body := rawPage(t, srv, "/roster")
	form := sectionBetween(t, body, `action="/roster/intake"`, "</form>")

	// ⟦LOCK — §82, 2026-09-03⟧ ⟦was: "`name=\"workgroup_id\"`, `name=\"new_workgroup\"`"⟧
	// The controls are `trade_id` and `new_trade` (intake.html:237, :247). ⭐ §89's class again:
	// these are FORM KEYS — plain strings on both sides — so the W8 rename could not have flagged
	// them, and this test asked the page for two controls it no longer renders under those names.
	// ⚠ THE CRITERION IS UNCHANGED: the scripting-off page must render every control an officer
	// needs to fill. Only two of the names moved.
	for _, name := range []string{`name="last_name"`, `name="first_name"`, `name="company_id"`, `name="new_company"`, `name="trade_id"`, `name="new_trade"`} {
		if !strings.Contains(form, name) {
			t.Errorf("the intake form has no %s control", name)
		}
	}
	// The class sits BEFORE the id in the open tag, so a slice that starts
	// at the id attribute cannot see it — that exact mistake let a mutation
	// putting .new-fields back survive this test on its first run. openTag
	// walks BACK to the "<" so the whole element declaration is inspected.
	// ⟦LOCK — §82, 2026-09-03⟧ ⟦was: "\"new-workgroup-field\""⟧ — the element is
	// `id="new-trade-field"` (intake.html:245). ⚠ This arm is the one that matters most of the two:
	// it is the .new-fields display:none trap, and with a stale id `openTag` could not find the
	// element at all, so the check could not have caught the mutation it exists for.
	for _, id := range []string{"new-company-field", "new-trade-field"} {
		tag := openTag(t, form, `id="`+id+`"`)
		if strings.Contains(tag, "new-fields") {
			t.Errorf(`%s carries .new-fields, which is display:none until a script shows it — this page ships no script, so the box would be invisible and unfillable: %s`, id, tag)
		}
	}
}

// TestNothingIsPreselectedInTheCompanyPicker. A <select> must display
// something, so what it displays is the empty prompt. A pre-chosen company
// files a person under a crew nobody chose, and that is the field the muster
// groups by.
func TestNothingIsPreselectedInTheCompanyPicker(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	body := rawPage(t, srv, "/roster")
	sel := sectionBetween(t, body, `name="company_id"`, "</select>")
	if !strings.Contains(sel, `<option value=""`) {
		t.Fatal("test setup: the company picker has no empty option, so this test cannot pose its case")
	}
	if got := strings.Count(sel, " selected"); got != 1 {
		t.Fatalf("%d selected options in the company picker, want exactly 1 (the empty prompt)", got)
	}
	empty := sectionBetween(t, sel, `<option value=""`, "</option>")
	if !strings.Contains(empty, "selected") {
		t.Errorf("the pre-selected company option is not the empty prompt: %s", sel)
	}
}

// TestTheCompanyPickerIsSortedByName. Company ids are crypto/rand hex, so
// the stored order is effectively random — the measured defect that made the
// old roster autocomplete show a random 50 of 1,279 people.
func TestTheCompanyPickerIsSortedByName(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	for _, n := range []string{"Zulu Services", "ASRC Energy Services", "Muskox Logistics"} {
		if _, err := domain.CreateCompany(st, "site-a", 1, n, 0, "default"); err != nil {
			t.Fatal(err)
		}
	}

	// THE TEST ASSERTS ITS OWN PRECONDITION (standing rule 6). Company ids
	// are crypto/rand hex and domain.Companies() returns them in id order,
	// so the STORED order is usually not name order — but "usually" is not
	// a scenario. With four companies it coincides about once in 24 runs,
	// and on such a run an unsorted picker would pass. Mint more companies
	// until the stored order genuinely differs, then say so if it never did.
	stored := func() []string {
		t.Helper()
		cs, err := domain.Companies(st)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, 0, len(cs))
		for _, c := range cs {
			out = append(out, c.Name)
		}
		return out
	}
	for attempt := 0; sort.StringsAreSorted(stored()) && attempt < 12; attempt++ {
		if _, err := domain.CreateCompany(st, "site-a", 1, fmt.Sprintf("Tiebreak %02d Co", attempt), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	if sort.StringsAreSorted(stored()) {
		t.Fatal("test setup: the stored company order is already name order after 12 attempts, so an unsorted picker would pass — this test is not posing its case")
	}

	want := append([]string(nil), stored()...)
	sort.Strings(want)

	sel := sectionBetween(t, rawPage(t, srv, "/roster"), `name="company_id"`, "</select>")
	at := -1
	for _, n := range want {
		i := strings.Index(sel, ">"+n)
		if i < 0 {
			t.Fatalf("company %q is missing from the picker", n)
		}
		if i < at {
			t.Errorf("the company picker is not in name order — %q appears out of place:\n%s", n, sel)
		}
		at = i
	}
}

// --- refusals author nothing, and the commit is not repeatable ---

// TestARefusedIntakeAuthorsNothingAtAll. Everything is validated before
// anything is written; half an intake is a duplicate company nobody asked
// for, on the table the muster groups by.
func TestARefusedIntakeAuthorsNothingAtAll(t *testing.T) {
	// ⟦LOCK — §60, 2026-09-03⟧ KIND B (intake site :599) — DISPOSITION (3) ATTEMPTED AND IT
	// SUCCEEDED, so this test is RE-EXPRESSED rather than retired.
	//
	// WHAT IT GUARDS, unchanged: "Everything is validated before anything is written; half
	// an intake is a duplicate company nobody asked for, on the table the muster groups by."
	// WHY THE OLD RIVAL IS GONE: it posed "a work group belonging to Kakivik, submitted
	// under Doyon" — a CROSS-COMPANY pairing. ⟦DR-W8-2⟧ deletes company scoping, `Trade` has
	// no CompanyID (refdata.go:202-214), and intake.go:91 records the consequence in
	// production's own words: "no cross-company pairing to refuse".
	// ⭐ THE SURVIVING RIVAL [MEASURED — intake.go:573]: the handler still answers 409 for a
	// trade id that is not on the list — "That trade is no longer on the list… Nothing was
	// recorded." So a refusal that must author nothing is STILL constructible.
	// ⭐ AND THE RE-EXPRESSION IS STRONGER THAN WHAT IT REPLACES: the form now asks for a NEW
	// COMPANY *and* names an unknown trade, so the refusal happens AFTER the company would
	// have been created. That is the original rationale — "half an intake is a duplicate
	// company nobody asked for" — posed directly, where the old fixture only implied it.
	//   ⟦was: "wgs, err := domain.Workgroups(srv.Store)"⟧
	//   ⟦was: "if err != nil || len(wgs) == 0 {"⟧
	//   ⟦was: "\tt.Fatalf(\"test setup: no work group to mis-pair (err=%v)\", err)"⟧
	//   ⟦was: "}"⟧
	//   ⟦was: "otherCo, err := domain.CreateCompany(srv.Store, \"site-a\", 1, \"Doyon Drilling\", 0, \"default\")"⟧
	//   ⟦was: "if wgs[0].CompanyID == otherCo.CompanyID {"⟧
	//   ⟦was: "\tt.Fatal(\"test setup: the work group already belongs to the other company, so this test cannot pose its case\")"⟧
	//   ⟦was: "}"⟧
	//   ⟦was: "// A work group belonging to Kakivik, submitted under Doyon."⟧
	//   ⟦was: "rec := postForm(t, srv, \"/roster/intake\", intakeForm2(\"Ferguson\", \"Mary\", otherCo.CompanyID, \"\", wgs[0].WorkgroupID, \"\"))"⟧
	//   ⟦was: "if rec.Code != http.StatusConflict { t.Fatalf(\"cross-company work group = %d, want 409: %s\", rec.Code, rec.Body.String()) }"⟧
	srv, _, _ := newGateTestServer(t)

	// Fixture-defect guard (rule 6): the trade id below must ACTUALLY not exist, or the
	// refusal under test is not the refusal that fires.
	const unknownTrade = "site-a-trade-notonthelist000000"
	trades, err := domain.Trades(srv.Store)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range trades {
		if w.TradeID == unknownTrade {
			t.Fatalf("fixture defect: %q is on the trade list, so the refusal this test poses would not fire", unknownTrade)
		}
	}
	p0, c0, w0 := countRows(t, srv)

	// A NEW company plus a trade id that is not on the list: the refusal lands AFTER the
	// point where a half-committed intake would already have created the company.
	rec := postForm(t, srv, "/roster/intake",
		intakeForm2("Ferguson", "Mary", "__new__", "Doyon Drilling", unknownTrade, ""))
	if rec.Code != http.StatusConflict {
		t.Fatalf("intake naming an unknown trade = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	p1, c1, w1 := countRows(t, srv)
	if p1 != p0 || c1 != c0 || w1 != w0 {
		t.Errorf("a refused intake wrote rows: persons %d->%d, companies %d->%d, trades %d->%d — the company is the one that matters: a refusal reached after the company would have been created must leave NO company behind", p0, p1, c0, c1, w0, w1)
	}
}

// TestIntakeRedirectsSoARefreshCannotEnrolTwice. This route AUTHORS, so
// re-posting the identical body must not be reachable by pressing refresh —
// unlike the gate's accumulate routes, which render directly because they
// write nothing.
func TestIntakeRedirectsSoARefreshCannotEnrolTwice(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	companyID := companyIDNamed(t, srv, "Kakivik Asset Management")
	rec := postForm(t, srv, "/roster/intake", intakeForm2("Ferguson", "Mary", companyID, "", "", ""))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("intake = %d, want 303 (post/redirect/get)", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/roster?enrolled=") {
		t.Fatalf("intake redirected to %q, want /roster?enrolled=<id>", loc)
	}
	// And the landing page echoes what was written, in the past tense.
	body := rawPage(t, srv, loc)
	if !strings.Contains(body, "ENROLLED") || !strings.Contains(body, "Ferguson, Mary") ||
		!strings.Contains(body, "Kakivik Asset Management") {
		t.Error("the landing page does not echo the enrolment back naming the person and their company")
	}
}

// TestIntakeIsGatedBehindLogin — it authors, so C11's rule applies to it
// exactly as it does to POST /gate/checkin.
func TestIntakeIsGatedBehindLogin(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	companyID := companyIDNamed(t, srv, "Kakivik Asset Management")
	before, _, _ := countRows(t, srv)

	form := intakeForm2("Ferguson", "Mary", companyID, "", "", "")
	req := httptest.NewRequest("POST", "/roster/intake", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req) // no session cookie at all

	if after, _, _ := countRows(t, srv); after != before {
		t.Errorf("an unauthenticated intake enrolled %d person row(s), want 0", after-before)
	}
}

// --- name composition ---

func TestComposeNameProducesLastCommaFirst(t *testing.T) {
	for _, tc := range []struct{ last, first, want string }{
		{"Ferguson", "Mary", "Ferguson, Mary"},
		{"  van der Berg ", " Jan  ", "van der Berg, Jan"},
		{"Okpik", "Mary Anne", "Okpik, Mary Anne"},
		{"Ferguson", "", "Ferguson"},
		{"", "Mary", "Mary"},
		{"", "", ""},
	} {
		if got := composeName(tc.last, tc.first); got != tc.want {
			t.Errorf("composeName(%q, %q) = %q, want %q", tc.last, tc.first, got, tc.want)
		}
	}
}

// TestIntakeRefusesAHalfEnteredName — both halves are required, because the
// muster is read aloud from whatever was typed.
func TestIntakeRefusesAHalfEnteredName(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	companyID := companyIDNamed(t, srv, "Kakivik Asset Management")
	before, _, _ := countRows(t, srv)
	for _, tc := range [][2]string{{"Ferguson", ""}, {"", "Mary"}, {"   ", "  "}} {
		rec := postForm(t, srv, "/roster/intake", intakeForm2(tc[0], tc[1], companyID, "", "", ""))
		if rec.Code != http.StatusConflict {
			t.Errorf("intake(%q, %q) = %d, want 409", tc[0], tc[1], rec.Code)
		}
	}
	if after, _, _ := countRows(t, srv); after != before {
		t.Errorf("a half-entered name enrolled %d person row(s), want 0", after-before)
	}
}

// --- small local helpers ---

// sectionBetween returns the substring of body from the first occurrence of
// `from` up to the next `to`. It FAILS THE TEST if either marker is missing,
// so an assertion can never silently pass over a section that is not there —
// the searched-for text being absent from an empty string is exactly the
// false pass this project keeps finding.
func sectionBetween(t *testing.T, body, from, to string) string {
	t.Helper()
	i := strings.Index(body, from)
	if i < 0 {
		t.Fatalf("marker %q not found in the rendered page", from)
	}
	rest := body[i:]
	j := strings.Index(rest, to)
	if j < 0 {
		t.Fatalf("closing marker %q not found after %q", to, from)
	}
	return rest[:j]
}

// openTag returns the whole "<...>" element declaration containing marker —
// walking BACK to the opening "<" so attributes written BEFORE the marker
// (a class, typically) are inside the returned string. sectionBetween starts
// AT its marker and therefore cannot see them, which is a false pass waiting
// to happen for any assertion about an element's classes.
func openTag(t *testing.T, body, marker string) string {
	t.Helper()
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("marker %q not found in the rendered page", marker)
	}
	start := strings.LastIndex(body[:i], "<")
	if start < 0 {
		t.Fatalf("no element start found before %q", marker)
	}
	end := strings.Index(body[start:], ">")
	if end < 0 {
		t.Fatalf("no element end found after %q", marker)
	}
	return body[start : start+end+1]
}

// --- the entrance, and a flagged identity ---

// intakeSiteServer builds a post with a chosen site id and a chosen entrance,
// seeded with one company so intake has something to attach to. Separate from
// newGateTestServer because that fixture is site-a with no entrance
// configured, which is exactly the case these tests must vary.
func intakeSiteServer(t *testing.T, siteID, entranceID string) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	if _, err := domain.CreateCompany(st, siteID, 1, "Kakivik Asset Management", 0, "default"); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		SiteID: siteID, DisplayName: siteID, ListenAddr: ":0",
		NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 10,
		LoginPassword:  testLoginPassword,
		EntranceSiteID: entranceID,
	}
	return New(cfg, st), st
}

// TestAnUnrecognisedNameAwayFromTheEntranceIsFlaggedAndStillOnTheRoster.
// James: "if it sees someone new it should flag them for sure — not make a
// new entry." The flag is on the RECORD. The person is present, so they are
// on the roster and they reach the muster — presence is never withheld for a
// paperwork reason.
func TestAnUnrecognisedNameAwayFromTheEntranceIsFlaggedAndStillOnTheRoster(t *testing.T) {
	srv, st := intakeSiteServer(t, "site-b", "site-a")
	if _, err := domain.CreatePerson(st, "site-b", 1, "Newcomer, Nell", nil, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// Somebody enrolled at the entrance, replicated here — the control. If
	// the flag were keyed on anything but the authoring post, this row would
	// be flagged too and the test would still "pass" on the row above.
	if _, err := domain.CreatePerson(st, "site-a", 1, "Regular, Ray", nil, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}

	body := rawPage(t, srv, "/roster")
	flagged := sectionBetween(t, body, `id="unconfirmed-identities"`, "</table>")
	if !strings.Contains(flagged, "Newcomer, Nell") {
		t.Errorf("a record made away from the entrance is not flagged:\n%s", flagged)
	}
	if strings.Contains(flagged, "Regular, Ray") {
		t.Errorf("a record made AT the entrance is flagged; the flag is not derived from the authoring post:\n%s", flagged)
	}
	// And they are still on the roster proper — present, not withheld.
	roster := sectionBetween(t, body, `id="roster-table"`, "</table>")
	if !strings.Contains(roster, "Newcomer, Nell") {
		t.Error("the flagged person was withheld from the roster; presence is never withheld for a paperwork reason")
	}
}

// RETIRED 2026-08-28 at W3: the intake test that asserted the entrances-record
// link clears the Unconfirmed flag. Its original note read: "Reconciliation is
// the EXISTING alias merge (domain.CreateAlias), not a second mechanism beside
// it" — true when written, and superseded by gate P-3, under which `same_as`
// IS the one mechanism and the legacy alias drives no reader.
//
// Its name is deliberately not written here. The package's citation meta-test
// requires every test named in a comment to exist, and a retired test's name
// is not a citation the next reader can follow — naming it would be the very
// defect that guard exists to catch. (It caught this comment's first draft.)
//
// It asserted that a bare legacy `ref_alias` row (authored through
// domain.CreateAlias) CLEARS the roster's Unconfirmed flag. W3's locked
// criterion W3-4 asserts the exact opposite of the same stimulus: intake's
// Unconfirmed derives from the composed `same_as` state, so a legacy alias
// with no composed merge leaves the flag standing. No implementation can
// satisfy both, and the fixtures are indistinguishable to the code.
//
// The locked criterion wins, and it is right: under gate P-3's decision
// (option 1a, `design/P3-merge-mechanism.md`) `same_as` SUPERSEDES
// `ref_alias`, and a `ref_alias` row that still drove a live reader would be
// exactly the second mechanism P-3 exists to forbid — the recount's
// "what is forbidden is shipping both as independent mechanisms".
//
// Operator impact: none. The v10 log starts empty ([[D-29]]), and
// `ref_alias_created` is authorable only through the dev-gated /dev/alias
// route, which dies with alias.go at W8 (schedule stated once at P-3 §5).
// So no production ref_alias row exists for this behaviour to matter to.
//
// Its replacement, asserting the new contract on the same stimulus, is
// TestW3IntakeUnconfirmedIgnoresLegacyRefAlias in w3_reader_surfaces_test.go
// (criterion W3-4, mutant m9 — the second-path reader).
//
// Retired by the plan owner rather than edited, so the supersession is on the
// record instead of hidden in a diff. Build-critique note: the W3 builder
// stopped and reported this contradiction instead of resolving it, which is
// the required behaviour when a test's assertion collides with a locked
// criterion.

// TestIntakeIsNeverBlockedAwayFromTheEntrance. James: "i wouldnt say never —
// they should still have the ability to input someone new but it will be VERY
// unlikely." Rare is a matter of prominence; it is never a refusal, never a
// permission and never somebody else's approval.
func TestIntakeIsNeverBlockedAwayFromTheEntrance(t *testing.T) {
	srv, _ := intakeSiteServer(t, "site-b", "site-a")
	companyID := companyIDNamed(t, srv, "Kakivik Asset Management")
	rec := postForm(t, srv, "/roster/intake", intakeForm2("Newcomer", "Nell", companyID, "", "", ""))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("intake away from the entrance = %d, want 303 — the capability must never be withheld: %s", rec.Code, rec.Body.String())
	}
	if got := len(personNamed(t, srv, "Newcomer, Nell")); got != 1 {
		t.Errorf("%d roster rows after an off-entrance enrolment, want 1", got)
	}
}

// TestTheIntakeFormIsDemotedAwayFromTheEntranceAndOpenAtIt. Same capability,
// different prominence — asserted in BOTH directions, because "the form is
// inside a <details>" could otherwise be satisfied by burying it everywhere.
func TestTheIntakeFormIsDemotedAwayFromTheEntranceAndOpenAtIt(t *testing.T) {
	downstream, _ := intakeSiteServer(t, "site-b", "site-a")
	body := rawPage(t, downstream, "/roster")
	if !strings.Contains(body, `id="downstream-note"`) {
		t.Error("a post that is not the entrance does not say so")
	}
	disclosure := strings.Index(body, `id="intake-disclosure"`)
	form := strings.Index(body, `action="/roster/intake"`)
	if disclosure < 0 || form < 0 || disclosure > form {
		t.Error("the intake form is not inside the disclosure at a post away from the entrance")
	}
	// <details> is native HTML: one click, no script, no permission.
	if !strings.Contains(body, "<details") {
		t.Error("the demotion is not a native <details>, so it may not open without scripting")
	}

	entrance, _ := intakeSiteServer(t, "site-a", "site-a")
	atEntrance := rawPage(t, entrance, "/roster")
	if strings.Contains(atEntrance, `id="intake-disclosure"`) {
		t.Error("the intake form is buried at the ENTRANCE, where enrolling people is the everyday job")
	}
	if strings.Contains(atEntrance, `id="downstream-note"`) {
		t.Error("the entrance is told it is downstream of itself")
	}
}

// TestNoPostIsCalledDownstreamUntilSomebodySaysWhichIsTheEntrance.
// "This post is not the entrance" and "nobody has said which post is the
// entrance" are different statements, and an unconfigured deployment must
// make the second one.
func TestNoPostIsCalledDownstreamUntilSomebodySaysWhichIsTheEntrance(t *testing.T) {
	srv, st := intakeSiteServer(t, "site-b", "")
	if _, err := domain.CreatePerson(st, "site-b", 1, "Newcomer, Nell", nil, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	body := rawPage(t, srv, "/roster")
	if strings.Contains(body, `id="downstream-note"`) {
		t.Error("a deployment with no configured entrance calls this post downstream of one it has never heard of")
	}
	if strings.Contains(body, `id="unconfirmed-identities"`) {
		t.Error("records are flagged as unconfirmed against an entrance nobody has named")
	}
	if strings.Contains(body, `id="intake-disclosure"`) {
		t.Error("intake is demoted at a post that has not been told it is downstream of anything")
	}
}
