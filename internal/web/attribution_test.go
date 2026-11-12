// attribution_test.go — F2's own done-when tests, HTTP half (test-campaign
// fix), as amended by D-5. Mirrors auth_test.go's
// TestRecordedByLandsOnGateCheckInVisits and
// TestPostAttributionFallsBackToSiteIDRatherThanBlank EXACTLY (same
// helpers, same request shape) but for the add-on-demand company/workgroup/
// person rows those tests don't check, and for the dev alias route.
package web

import (
	"net/http"
	"net/url"
	"path/filepath"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// newUnnamedPostTestServer is the same inline setup auth_test.go's own
// TestPostAttributionFallsBackToSiteIDRatherThanBlank uses: a box whose
// display_name was never filled in, so config.PostName falls back to the
// site id. That is the only "degraded attribution" state D-5 leaves — there
// is no unpicked-officer state any more — and the rows below must carry the
// fallback, never a blank or a fabricated name.
func newUnnamedPostTestServer(t *testing.T) (*Server, *store.Store) {
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
		SiteID: "site-a", ListenAddr: ":0", // DisplayName deliberately unset
		NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 10,
		LoginPassword: testLoginPassword,
	}
	return New(cfg, st), st
}

// ⟦LOCK — §60, 2026-09-03⟧ W8 Workgroup→Trade rename fallout, BOTH names. RENAMED, not
// retired: this file's own test is declared eleven lines below, and its domain twin is at
// internal/domain/attribution_test.go:168. Both successors prove exactly what the cited
// names proved — the rename carried the assertion unchanged.
// ⚠ Both superseded lines are preserved VERBATIM in reports/window-build/
// ALIAS-DELETION-REPAIR.md §4.10, NOT here: they name tests that no longer exist, and
// citations_test.go:103 fails any comment that does.
// TestRecordedByLandsOnAddOnDemandCompanyTradeAndPerson is the real-HTTP
// twin of domain.TestGateCheckInAddOnDemandAttributesRecordedByOnCompanyTradeAndPerson
// — going through s.postAttribution() and the actual /gate/checkin handler
// (internal/web/gate.go), not a direct domain call. This is F2's headline
// scenario: a person (and, here, their company/workgroup) minted mid-check-in
// by an operator at the gate — exactly the record someone will question
// later, so who it is attributed to matters most here.
//
// D-5: the answer is the POST, on the FIRST submission. The predecessor of
// this test asserted the opposite shape (the check-in withheld until a
// person was tapped from a list), which is the machinery D-5 retired.
func TestRecordedByLandsOnAddOnDemandCompanyTradeAndPerson(t *testing.T) {
	srv, st := newAuthTestServer(t)
	cookie := loginCookie(t, srv, testLoginPassword)
	post := srv.Cfg.PostName()

	form := url.Values{
		"time_source": {"default"},
		"vehicle":     {"TRK-14"}, "driver_kind": {"new"}, "driver_value": {"Walk In Wanda"},
		// ⟦LOCK — §94 disposition (3), 2026-09-03⟧ STALE BASE / LIVE ASSERTION: the BASE was
		// re-pointed, the assertion KEPT. ⚠ THE FORM FIELD IS THE TEST'S STIMULUS, NOT ITS
		// SUBJECT. gate.go:1426 and :1508 read `driver_trade` — the DIRECT SUCCESSOR at the same
		// argument position of personSlotFrom/addRow, renamed in 19eafc4, one commit before this
		// base. The handler stopped reading the old name, so no trade was ever minted and the
		// attribution arm below never got to run.
		// ⚠ The superseded line is preserved VERBATIM in reports/window-build/S100-ALIAS.md §2,
		// NOT here: it carries a W8-6 census term and this file is not allowlisted.
		"driver_company": {"Brand New Co"}, "driver_trade": {"Brand New Crew"},
	}
	rec := postFormWithCookie(t, srv, "/gate/checkin", form, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("gate check-in on a fresh session = %d, want 303 (no picker step): %s", rec.Code, rec.Body.String())
	}

	companies, err := domain.Companies(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(companies) != 1 || companies[0].RecordedBy == nil || *companies[0].RecordedBy != post {
		t.Fatalf("add-on-demand company recorded_by = %+v, want %q (this box's post)", companies, post)
	}
	wgs, err := domain.Trades(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(wgs) != 1 || wgs[0].RecordedBy == nil || *wgs[0].RecordedBy != post {
		t.Fatalf("add-on-demand workgroup recorded_by = %+v, want %q", wgs, post)
	}
	persons, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(persons) != 1 || persons[0].RecordedBy == nil || *persons[0].RecordedBy != post {
		t.Fatalf("add-on-demand person recorded_by = %+v, want %q", persons, post)
	}
	// The post name must never be a person's name that leaked in from a
	// session: the fixture's own driver name is the nearest such string.
	if *persons[0].RecordedBy == "Walk In Wanda" {
		t.Fatal("recorded_by took the checked-in person's name — attribution is the POST, not anyone on the record")
	}
}

// TestRecordedByOnAddOnDemandFallsBackToSiteIDWhenThePostIsUnnamed is the
// add-on-demand analogue of auth_test.go's
// TestPostAttributionFallsBackToSiteIDRatherThanBlank. It replaces an
// F20/M11-era test that posed an empty Officers list and asserted a genuine
// NULL; since D-5 that state cannot occur at authoring time, so this poses
// the state that CAN — a box with no display_name — and asserts the rows
// carry the site id rather than an empty string or a fabricated placeholder.
func TestRecordedByOnAddOnDemandFallsBackToSiteIDWhenThePostIsUnnamed(t *testing.T) {
	srv, st := newUnnamedPostTestServer(t)
	cookie := loginCookie(t, srv, testLoginPassword)

	rec := postFormWithCookie(t, srv, "/gate/checkin", url.Values{
		"time_source": {"default"},
		"vehicle":     {"TRK-14"}, "driver_kind": {"new"}, "driver_value": {"Nameless Nolan"},
		"driver_company": {"No Officer Co"},
	}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("gate check-in = %d: %s", rec.Code, rec.Body.String())
	}

	companies, err := domain.Companies(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(companies) != 1 {
		t.Fatalf("companies = %+v, want exactly 1", companies)
	}
	if companies[0].RecordedBy == nil || *companies[0].RecordedBy != "site-a" {
		t.Fatalf("add-on-demand company recorded_by = %v, want %q — an unnamed post falls back to its site id, never blank or nil", companies[0].RecordedBy, "site-a")
	}
	persons, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(persons) != 1 {
		t.Fatalf("persons = %+v, want exactly 1", persons)
	}
	if persons[0].RecordedBy == nil || *persons[0].RecordedBy != "site-a" {
		t.Fatalf("add-on-demand person recorded_by = %v, want %q", persons[0].RecordedBy, "site-a")
	}
}

// TestDevAliasRecordedByLandsOnRefAlias exercises devroutes.go's
// handleDevAliasInject over real HTTP: the route sits behind requireLogin
// (C11), and the merge it authors is attributed like every other authored
// record — to the POST (D-5), not to a person.
//
// WHETHER AN ALIAS MERGE SPECIFICALLY WANTS A PERSON'S NAME is flagged for
// James in reports/OFFICER-ATTRIBUTION.md and deliberately not settled
// here: a merge is a judgement about two identities being one, not a gate
// transaction. This test pins today's behaviour so a change to it is
// visible rather than silent.
// ⚠⚠ ⟦RETIRED — §60, 2026-09-03⟧ KIND A (site 24) — DISPOSITION (1), RETIRE.
// Every superseded line is preserved verbatim below, INCLUDING the §55 re-expression
// this pass made earlier in the same block. Nothing is deleted.
//
// WHAT IT PROVED: that POST /dev/alias authored a merge attributed to the POST (D-5),
// landing a resolvable edge and a ref_alias row carrying this box's post name.
// WHICH RULING REMOVED ITS SUBJECT: P-3 §5 / [[D-47]]. ⭐ It is dead THREE TIMES OVER
// (scope §1.1): its helper `newDevRouteTestServer` is deleted, its route POST /dev/alias
// is deleted, and its loader was deleted. Only the helper is a compile error; the other
// two are silent.
//
// ⚠ WHY THE WHOLE TEST GOES, AND NOT JUST THE FOSSIL HALF — this reverses nothing that
// §55 protected, and the reason is checked rather than assumed. §55 correctly refused to
// retire the MERGE-RESOLUTION half with the fossil, and this pass re-expressed it against
// `LoadCompanyMergeMap`. But §60 rules the ENCLOSING test Kind A, and its stimulus — the
// deleted route — cannot be replaced without inventing a new test: re-pointing it to
// AmendCompany would leave a test named for a dev route that never touches one.
// ⭐ THE RE-EXPRESSED HALF IS NOT LOST, and it is covered THREE TIMES OVER IN THIS SAME
// PACKAGE [MEASURED]: w5d_visible_failure_test.go:127 and :190, and
// w5d_reader_surfaces_test.go:216 all assert composed company merge resolution through
// LoadCompanyMergeMap. So the coverage this retirement costs is NOT the merge half.
//
// ⭐ COVERAGE COST, NAMED EXACTLY: the D-5 attribution of a merge — "the merge carries
// this box's post, never a person". That half was already retired under §55 for a
// measured reason that still holds: the amendment carrier has NO recorded_by field
// (company_amend.go:19-21, W5d-2b — "attribution on an amendment is STRUCTURAL,
// env.SiteID, never a payload field"), so it has no surviving surface to be posed on.
//
// ⚠ ROUTED, NOT DECIDED HERE: the test NAME still carries the deleted route and the
// deleted table. §60 authorises no rename, and this pass has renamed nothing.

// ⚠ ⟦RETIRED — §60, 2026-09-03⟧ THE DECLARATION IS KEPT DELIBERATELY, AND THE REASON IS A
// COLLISION BETWEEN TWO GUARDS THAT IS WORTH RECORDING:
//
//	§60 requires a retired test's superseded lines to be preserved AT THE SITE, which means
//	preserving them as COMMENTS; but this package's citation meta-test
//	(citations_test.go:103) fails any comment naming a test that is not DECLARED. Commenting
//	the function out therefore satisfies §60 and breaks the meta-test — PRODUCED: two
//	"cites TestDevAliasRecordedByLandsOnRefAlias, which does not exist" failures, one from
//	this test's own doc comment above and one from the preserved `func` line below.
//
// ⭐ RESOLVED BY KEEPING THE NAME DECLARED AND THE BODY RETIRED. This satisfies both, and
// it satisfies §60's FIRST BAR better than a comment block would: a permanent, named SKIP
// is visible in EVERY run, where a commented-out function leaves no trace at all.
// ⚠ THIS IS NOT A DISABLED TEST AND MUST NOT BE RE-ENABLED — its subject is deleted. The
// skip message and the verbatim body below are the whole of what it used to prove.
func TestDevAliasRecordedByLandsOnRefAlias(t *testing.T) {
	t.Skip("⟦RETIRED — §60, 2026-09-03⟧ NOT DISABLED — RETIRED. Its subject (POST /dev/alias, " +
		"the ref_alias table, and the helper that built a dev-route server) was deleted by P-3 §5 " +
		"and [[D-47]]. Its merge-resolution half is covered by w5d_visible_failure_test.go:127/:190 " +
		"and w5d_reader_surfaces_test.go:216; its D-5 attribution half has no successor (W5d-2b: " +
		"attribution on an amendment is structural, never a payload field). The superseded body is " +
		"preserved verbatim below.")

	// --- THE SUPERSEDED BODY, PRESERVED VERBATIM -------------------------
	// func TestDevAliasRecordedByLandsOnRefAlias(t *testing.T) {
	// 	srv, st := newDevRouteTestServer(t, true)
	// 	c1, err := domain.CreateCompany(st, "site-a", 1, "Kakivik", 0, "default")
	// 	if err != nil {
	// 		t.Fatal(err)
	// 	}
	// 	c2, err := domain.CreateCompany(st, "site-a", 1, "Kakavik", 0, "default")
	// 	if err != nil {
	// 		t.Fatal(err)
	// 	}
	// 	cookie := loginCookie(t, srv, testLoginPassword)
	//
	// 	rec := postFormWithCookie(t, srv, "/dev/alias", url.Values{
	// 		"duplicate_id": {c2.CompanyID}, "canonical_id": {c1.CompanyID},
	// 	}, cookie)
	// 	if rec.Code != http.StatusCreated {
	// 		t.Fatalf("POST /dev/alias = %d: %s", rec.Code, rec.Body.String())
	// 	}
	//
	// 	// ⟦LOCK — §55, 2026-09-03⟧ THE MERGE ASSERTION, RE-EXPRESSED — NOT retired
	// 	// with the fossil below. ⚠ THE MERGE IS NOT DELETED: `Company.SameAs`
	// 	// survives (P-3 option 1a) and is resolved AT THE READ (W8-19); only the
	// 	// `ref_alias` CARRIER went. Retiring this half because its loader stopped
	// 	// compiling would have silently dropped a LIVE assertion about merge
	// 	// behaviour — a filter narrower than its subject.
	// 	// ⟦was: "m, err := domain.LoadAliasMap(st)"⟧
	// 	m, err := domain.LoadCompanyMergeMap(st)
	// 	if err != nil {
	// 		t.Fatal(err)
	// 	}
	// 	if got := m.Resolve(c2.CompanyID); got != c1.CompanyID {
	// 		t.Fatalf("the composed company merge resolves %s to %s, want %s", c2.CompanyID, got, c1.CompanyID)
	// 	}
	//
	// 	// ⚠⚠ ⟦RETIRED — §55, 2026-09-03⟧ THE TABLE ASSERTIONS ARE RETIRED, NOT
	// 	// REPAIRED, AND THE SUPERSEDED LINES ARE PRESERVED HERE VERBATIM:
	// 	//   ⟦was: "// D-5: the merge carries this box's post, from config — never nil and"⟧
	// 	//   ⟦was: "// never a person. Mutating postAttribution to return nil makes this go red."⟧
	// 	//   ⟦was: "aliases, err := domain.RefAliases(st)"⟧
	// 	//   ⟦was: "if err != nil {"⟧
	// 	//   ⟦was: "\tt.Fatal(err)"⟧
	// 	//   ⟦was: "}"⟧
	// 	//   ⟦was: "if len(aliases) != 1 {"⟧
	// 	//   ⟦was: "\tt.Fatalf(\"ref_alias rows = %+v, want exactly 1\", aliases)"⟧
	// 	//   ⟦was: "}"⟧
	// 	//   ⟦was: "if aliases[0].RecordedBy == nil || *aliases[0].RecordedBy != srv.Cfg.PostName() {"⟧
	// 	//   ⟦was: "\tt.Fatalf(\"alias recorded_by = %v, want %q (this box's post)\", aliases[0].RecordedBy, srv.Cfg.PostName())"⟧
	// 	//   ⟦was: "}"⟧
	// 	//
	// 	// Unlike arm (B) in w8_devroutes_test.go — which asserts the mechanism's
	// 	// ABSENCE and was therefore RE-EXPRESSED — these assert that the mechanism
	// 	// WORKS, which P-3 §5 and [[D-47]] removed on purpose. A test of a deleted
	// 	// mechanism is a fossil, not evidence.
	// 	//
	// 	// ⭐ CHECKED NEGATIVE, so the live half is not dropped with the fossil a
	// 	// second time: the D-5 attribution intent has NO PAYLOAD SUCCESSOR to
	// 	// re-express against. `AmendCompany` takes no recordedBy and has no
	// 	// `…RecordedBy` variant; `companyAmendmentRow` carries no such field; and
	// 	// company_amend.go:19-21 rules it deliberate — "CompanyCreated DOES carry
	// 	// RecordedBy (refdata.go:26); the amendment carrier deliberately does NOT
	// 	// (W5d-2b) — attribution on an amendment is STRUCTURAL, env.SiteID, never
	// 	// a payload field (D-5, S7)" — with :69-70 recording that `recorded_by`
	// 	// was "STRUCK from that freeze list at rev 4". Attribution did not vanish;
	// 	// it moved OFF the payload by ruling, so there is nothing here to keep.
	// }
}
