// w5d_surface_test.go — W5d-23 (the company amendment ROUTE exists and its
// GATING IS ASSERTED, per [[D-51]]; and the same commit applies the TP §4.8
// successor-table amendment-route row), W5d-24 (the surface renders NO raw
// error string on its internal-error arm), W5-X2's value= preservation on
// the refusals W5d builds, and W5d-2's "no `retired` on any authoring
// surface" limb.
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, W5d LOCKED 2026-08-31.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD — that IS these tests' clean-tree red.
//
// KILLER MUTANTS: (m12) (/company/amend shipped UNGUARDED, or behind
// dev_routes instead of requireLogin), (m13) (the route registered with NO
// TP §4.8 classification row, reaching W2's route-walk unclassified),
// (m14) (the new company handler copies person_amend.go:242's
// buildPersonAmendPage(err.Error(), nil) shape — W4's F-3 register cloned
// onto a brand-new officer-facing surface), (m2) (`Retired` resurrected on
// company — the edge-control limb).
//
// ⚠ W5d-24 owes NO control: its evidence is a genuine PRODUCED PRE-CHUNK
// RED against the person-side spelling, measured and quoted in the report.
// A gate must not hunt for a plant that does not exist here.
package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// --- W5d-23(a): the route exists and is GATED ---------------------------

// TestW5dCompanyAmendRoutesAreBehindRequireLogin — W5d-23(a), ruled by
// [[D-51]]: officer-facing, behind requireLogin, the same gating as
// /person/amend. DR-6 previously said "there is no second surface to gate"
// — a claim the ledger could not fail — and D-51's binding consequence 1
// makes the assertion mandatory: a ruling that is not asserted is not
// tested. Kills (m12).
//
// Asserted on BODY CONTENT and STORE STATE, never on the status code (the
// catch-all answers 200 to everything — the house's own carry-forward).
func TestW5dCompanyAmendRoutesAreBehindRequireLogin(t *testing.T) {
	srv, st, x, _ := w5dSiteAServer(t)

	// --- CONTROL FIRST: the route registered WITHOUT the wrapper must be
	// seen to be REACHED by the same judgement the real assertion uses. --
	unguarded := http.NewServeMux()
	unguarded.HandleFunc("GET /company/amend", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<h1>PLANTED unguarded company amend page</h1>"))
	})
	unguarded.HandleFunc("POST /company/amend", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<h1>PLANTED unguarded company amend handler</h1>"))
	})
	for _, m := range []string{http.MethodGet, http.MethodPost} {
		rec := w5dAnonRequest(t, unguarded, m, w5dAmendPath, url.Values{})
		if !w5dReachedHandler(rec) {
			t.Fatalf("W5d-23(a) CONTROL FAILED (m12): an UNWRAPPED %s %s was judged as NOT reached — the gating judgement cannot detect an unguarded route, so its clean verdict proves nothing. Body: %s", m, w5dAmendPath, rec.Body.String())
		}
	}
	// …and the same handler WRAPPED must be judged as NOT reached, or the
	// judgement answers "reached" for everything.
	guarded := http.NewServeMux()
	guarded.HandleFunc("POST /company/amend", srv.requireLogin(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<h1>PLANTED but guarded</h1>"))
	}))
	if rec := w5dAnonRequest(t, guarded, http.MethodPost, w5dAmendPath, url.Values{}); w5dReachedHandler(rec) {
		t.Fatalf("W5d-23(a) CONTROL FAILED: a requireLogin-WRAPPED handler was judged as reached — the judgement discriminates nothing. Body: %s", rec.Body.String())
	}

	// --- the real routes, UNAUTHENTICATED -------------------------------
	before := w2Watermark(t, st, "site-a", 1)
	for _, probe := range []struct {
		method string
		form   url.Values
	}{
		{http.MethodGet, nil},
		{http.MethodPost, url.Values{w5dFieldCompanyID: {x.CompanyID}, w5dFieldName: {"Sneaked In Ltd"}}},
	} {
		rec := w5dAnonRequest(t, srv.Handler(), probe.method, w5dAmendPath, probe.form)
		if w5dReachedHandler(rec) {
			t.Fatalf("W5d-23(a) (m12): an UNAUTHENTICATED %s %s reached the handler — [[D-51]] rules this surface officer-facing BEHIND requireLogin, the same gating as /person/amend, and it is a company MERGE affordance as well as a rename. Body: %s", probe.method, w5dAmendPath, rec.Body.String())
		}
	}
	if after := w2Watermark(t, st, "site-a", 1); after != before {
		t.Fatalf("W5d-23(a) (m12): an unauthenticated POST AUTHORED (watermark %d → %d) — the login page must be served IN PLACE OF the handler, with nothing recorded", before, after)
	}

	// --- and AUTHENTICATED, both verbs are accepted ---------------------
	cookie := loginCookie(t, srv, testLoginPassword)
	page := w2Get(t, srv, w5dAmendPath, cookie)
	if page.Code != http.StatusOK {
		t.Fatalf("W5d-23(a): authenticated GET %s = %d, want 200: %s", w5dAmendPath, page.Code, page.Body.String())
	}
	if _, ok := w5dInputValueByName(page.Body.String(), w5dFieldCompanyID); !ok {
		t.Fatalf("W5d-23(a): the authenticated GET %s renders no input named %q — the company amendment form is the surface D-51 gates, and it must exist. Page:\n%s", w5dAmendPath, w5dFieldCompanyID, page.Body.String())
	}
	rec, cookie := w5dPostAmend(t, srv, url.Values{
		w5dFieldCompanyID: {x.CompanyID}, w5dFieldName: {"Authenticated Rename Ltd"},
	})
	if _, loc := w3FollowRecorded(t, srv, rec, cookie); loc == "" {
		t.Fatalf("W5d-23(a): the authenticated POST did not produce a PRG confirmation")
	}
}

// TestW5dCompanyAmendRouteCarriesItsTPClassificationRow — W5d-23(b): the
// SAME COMMIT that registers the route applies the TP §4.8 successor-table
// amendment-route row (EXEMPT-with-written-reason), "so the route reaches
// W2's route-walk classified". Revision 5 dropped this silently at the
// first clone: no criterion carried it and no clone or ride bucket named
// it. Kills (m13).
//
// The row is read from w2RouteTable — W2's own criteria-lock table, in this
// same package — and the walk's own judgement function is then run over the
// real discovered universe, SCOPED to /company/amend so a concurrent
// sub-chunk's in-flight route cannot red this test (rule 18).
func TestW5dCompanyAmendRouteCarriesItsTPClassificationRow(t *testing.T) {
	var row *w2Row
	for i := range w2RouteTable {
		if w2RouteTable[i].Route == w5dAmendPath {
			row = &w2RouteTable[i]
		}
	}
	if row == nil {
		t.Fatalf("W5d-23(b) (m13): w2RouteTable carries NO row for %s — the TP §4.8 successor-table amendment-route row is a SAME-COMMIT obligation on the commit that registers the route (plan:1106–1113; the /person/amend row is the W3 precedent and /time/correct the W4 one). Without it the route reaches W2's route-walk UNCLASSIFIED, and \"silence is not exemption\"", w5dAmendPath)
	}
	if row.Class != w2Exempt {
		t.Fatalf("W5d-23(b) (m13): %s is classified %v, want EXEMPT-with-written-reason — no typed value feeds the envelope on an amendment route; typed values are payload, and the amendment act's own envelope pair is default-captured", w5dAmendPath, row.Class)
	}
	if strings.TrimSpace(row.Reason) == "" {
		t.Fatalf("W5d-23(b) (m13): %s is exempt with NO written reason — never a growing allowlist", w5dAmendPath)
	}

	// The walk's own judgement, over the real universe, scoped to this
	// route (a concurrent sub-chunk's unclassified route is its business,
	// not this test's).
	root := w2ModuleRoot(t)
	found := w2PostRegistrationsInDir(t, root+"/"+w2WebDir)
	registered := false
	for _, f := range found {
		if f.Route == w5dAmendPath {
			registered = true
		}
	}
	if !registered {
		t.Fatalf("W5d-23(a)/(b): the universe glob discovered no `POST %s` registration in %s — the route is unbuilt", w5dAmendPath, w2WebDir)
	}
	for _, p := range w2WalkProblems(found, w2RouteTable, func(dir, test string) bool {
		return w2TestFuncExists(t, root, dir, test)
	}) {
		if strings.Contains(p, w5dAmendPath) {
			t.Fatalf("W5d-23(b) (m13): the route-walk reports a problem for %s: %s", w5dAmendPath, p)
		}
	}
}

// --- W5d-24: no raw error string on the internal-error arm --------------

// TestW5dCompanyAmendInternalErrorRendersNoRawError — W5d-24. On the
// company amendment handler's INTERNAL-error arm — the analogue of
// LoadCompanyMergeMap failing — the rendered page carries NO err.Error()
// value: the error is LOGGED and the page shows authored copy. Kills
// (m14): the new handler copying person_amend.go:242's
// buildPersonAmendPage(err.Error(), nil) shape.
//
// ⚠ This is a genuine PRE-CHUNK RED against the person-side spelling, and
// it is the strongest ledger evidence for W4's F-3 anywhere in W5. Measured
// against /person/amend on the clean tree, the same probe renders
//
//	REFUSED — domain: amend person: domain: dump: store: dump projection
//	"person": sql: database is closed
//
// at HTTP 409. W5d-23 registers the company routes BY CLONING THAT FILE, so
// the mutant is the clone itself. W5 owes the two surfaces it touches and
// nothing else: every OTHER non-test err.Error() render in internal/web
// stays in W4's register, owed to whoever next touches each surface.
func TestW5dCompanyAmendInternalErrorRendersNoRawError(t *testing.T) {
	srv, st, x, _ := w5dSiteAServer(t)

	// Induce the INTERNAL arm: with the store closed, AmendCompany's own
	// reads fail — this is not a domain refusal.
	st.Close()

	// Fixture preconditions (rule 6), so the probe is known to be posing
	// the internal case and to carry a distinctive raw token: (1) the
	// domain call errors; (2) it is NOT a refusal; (3) its text carries
	// something a raw render would leak.
	_, derr := domain.AmendCompany(st, "site-a", 1,
		domain.CompanyAmendment{CompanyID: x.CompanyID, Name: strPtrW5d("Internal Error Ltd")}, 0, "default")
	if derr == nil {
		t.Fatalf("W5d-24 fixture defect: AmendCompany on a CLOSED store returned nil error — the internal-error arm cannot be posed")
	}
	if strings.Contains(derr.Error(), "refused") {
		t.Fatalf("W5d-24 fixture defect: the induced error is a REFUSAL (%v), not an internal error — refusal copy is authored and DOES belong on the page; this criterion is about the internal arm", derr)
	}
	rawTokens := []string{}
	for _, tok := range []string{"database is closed", "sql:", "domain: amend company"} {
		if strings.Contains(derr.Error(), tok) {
			rawTokens = append(rawTokens, tok)
		}
	}
	if len(rawTokens) == 0 {
		t.Fatalf("W5d-24 fixture defect: the induced internal error %q carries none of the distinctive raw tokens this test looks for — it cannot distinguish a leaked render from authored copy", derr)
	}

	rec, _ := w5dPostAmend(t, srv, url.Values{
		w5dFieldCompanyID: {x.CompanyID}, w5dFieldName: {"Internal Error Ltd"},
	})
	body := rec.Body.String()

	// NEGATIVE: no raw error value anywhere on the page. (Absence over the
	// whole body is strictly stronger than absence within one element, so
	// it is asserted body-wide; the POSITIVE half below is the
	// element-scoped one W5-X2 requires.)
	for _, tok := range rawTokens {
		if strings.Contains(body, tok) {
			t.Fatalf("W5d-24 (m14): the company amendment page renders the RAW ERROR token %q on its internal-error arm — the error is LOGGED and the page shows authored copy. This is exactly person_amend.go:240–242's shape, which on the clean tree renders %q to an officer under the word REFUSED. Page:\n%s", tok, derr.Error(), body)
		}
	}
	// POSITIVE, element-scoped per W5-X2: the officer is told SOMETHING, in
	// a notice element — an internal error that renders nothing at all is
	// the silent-OK this project's whole visible-failure design refuses.
	if notices := w5dNoticeTexts(body); len(notices) == 0 {
		t.Fatalf("W5d-24: the internal-error arm rendered NO notice element at all — suppressing the raw string must not become suppressing the failure. Page:\n%s", body)
	}
}

func strPtrW5d(s string) *string { return &s }

// --- W5-X2: the refusals W5d builds preserve typed input ----------------

// TestW5dRefusedCompanyAmendmentPreservesTypedInput — W5-X2, ruled at the
// lock rather than left to a builder: W5d DOES build refusal-preservation
// on the COMPANY amendment form, and W5-X2's value="…" assertion binds it.
// (W5d does NOT retrofit the person amendment form — that is a change to
// shipped behaviour outside W5d's scope, and it is recorded as a decision
// rather than silently inherited or silently skipped. Measured: that form
// has ZERO value= attributes today.)
//
// The W4 gate found a strings.Contains(body, wall) assertion that passed on
// the refusal sentence quoting the value back, so a mutant discarding the
// officer's typing survived it. This asserts the FORM CONTROL.
func TestW5dRefusedCompanyAmendmentPreservesTypedInput(t *testing.T) {
	srv, st, x, _ := w5dSiteAServer(t)
	person, err := domain.CreatePerson(st, "site-a", 1, "Not A Company", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	typedName := "Half Typed Rename Ltd"

	before := w2Watermark(t, st, "site-a", 1)
	// W5d-22(b) arm 8, INVERTED (W5d-13): a same_as naming a NON-COMPANY.
	rec, _ := w5dPostAmend(t, srv, url.Values{
		w5dFieldCompanyID: {x.CompanyID},
		w5dFieldName:      {typedName},
		w5dFieldSameAs:    {person.PersonID},
	})
	body := rec.Body.String()
	if after := w2Watermark(t, st, "site-a", 1); after != before {
		t.Fatalf("W5d-17/W3-m6: the refused POST AUTHORED (watermark %d → %d) — every edge refusal is PRE-APPEND", before, after)
	}
	if _, ok := w5dNoticeCarrying(body, "refused"); !ok {
		t.Fatalf("W5d-13/W5-X2: the refusal is not carried by a notice element — the officer must be told, in words, on the page. Notices: %v", w5dNoticeTexts(body))
	}
	for field, want := range map[string]string{
		w5dFieldCompanyID: x.CompanyID,
		w5dFieldName:      typedName,
		w5dFieldSameAs:    person.PersonID,
	} {
		got, present := w5dInputValueByName(body, field)
		if !present {
			t.Fatalf("W5-X2: the re-rendered company amendment form has no input named %q at all", field)
		}
		if got != want {
			t.Fatalf("W5-X2: after a refusal the input %q carries value=%q, want the officer's typed %q — a refusal that discards the typing makes the officer re-enter everything, and a strings.Contains over the body cannot see the difference because the refusal SENTENCE quotes the value back", field, got, want)
		}
	}
}

// TestW5dCompanyAmendSurfaceOffersNoRetired — W5d-2's third limb:
// `retired` is absent from the amendable-fact list AND from every authoring
// surface ([[D-41]].1 — "A company can be corrected but never retired").
// The company picker only grows; that cost was accepted with the ruling.
func TestW5dCompanyAmendSurfaceOffersNoRetired(t *testing.T) {
	// --- CONTROL FIRST: the input-name detector must see a planted field.
	planted := `<form><input name="company_id"><input name="retired"></form>`
	names := w5dInputNames(planted)
	sawRetired := false
	for _, n := range names {
		if n == "retired" {
			sawRetired = true
		}
	}
	if !sawRetired {
		t.Fatalf("W5d-2 CONTROL FAILED (m2): the input-name detector missed a planted <input name=\"retired\"> (found %v)", names)
	}

	srv, _, _, _ := w5dSiteAServer(t)
	cookie := loginCookie(t, srv, testLoginPassword)
	page := w2Get(t, srv, w5dAmendPath, cookie)
	if page.Code != http.StatusOK {
		t.Fatalf("W5d-2: GET %s = %d: %s", w5dAmendPath, page.Code, page.Body.String())
	}
	got := w5dInputNames(page.Body.String())
	forbidden := map[string]bool{
		"retired": true, "trade_id": true, "phone": true, "role": true,
		"pad_oriented": true, "position": true, "last_name": true, "first_name": true,
	}
	for _, n := range got {
		if forbidden[n] {
			t.Fatalf("W5d-2 (m2): the company amendment surface offers a %q control — the company amendable set is {name, same_as} and nothing else; `retired` in particular is ruled out by D-41.1. Inputs: %v", n, got)
		}
	}
	// Liveness (rule 6): the two real fields must be there, or "no
	// forbidden inputs" is true of a page with no form at all.
	for _, want := range []string{w5dFieldCompanyID, w5dFieldName, w5dFieldSameAs} {
		found := false
		for _, n := range got {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("W5d-2 liveness: the company amendment surface has no input named %q (inputs: %v) — a page with no form would satisfy the forbidden-field loop vacuously", want, got)
		}
	}
}

// TestW5dRendering2WordingIsOutsideTheAdverseRegister — W5d-9(a)'s wording
// constraint, verified rather than asserted: the pinned rendering-2 stem
// must be drawn from OUTSIDE w3AdverseTokens. The disposition warns that a
// test which "only greps for 'superseded' is m7′-blind", and the adverse
// register belongs to the LOSING side; a rendering-2 sentence built from it
// would make T6b's cry-wolf control and W5d-8's adverse assertions
// indistinguishable from each other.
func TestW5dRendering2WordingIsOutsideTheAdverseRegister(t *testing.T) {
	stem := strings.ToLower(w5dRendering2Stem)
	for _, tok := range w3AdverseTokens {
		if strings.Contains(stem, strings.ToLower(tok)) {
			t.Fatalf("W5d-9(a): the pinned rendering-2 stem %q contains the adverse-register token %q — the FIX arm's wording is chosen AGAINST w3AdverseTokens (disposition §3.3)", w5dRendering2Stem, tok)
		}
	}
	if strings.Contains(stem, strings.ToLower(w3PrecedencePhrase)) {
		t.Fatalf("W5d-9(a): the pinned rendering-2 stem %q contains the standing-cost line's phrase %q — the two sentences must stay distinguishable, since T10 extracts the standing sentence by that phrase", w5dRendering2Stem, w3PrecedencePhrase)
	}
}
