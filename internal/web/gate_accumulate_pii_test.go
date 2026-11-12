// gate_accumulate_pii_test.go — ⟦PII-1b, 2026-09-30⟧ the UNGATED accumulate
// routes give a logged-out caller nothing off a person's record.
//
// PII-1 emptied the roster JSON on /gate and /gate/vehicle for a request with
// no session, and TestNoPhoneNumberReachesTheMusterPrintOrRoster sweeps those
// two GETs. It did not reach the POSTs that render the same page: the
// accumulate routes are ungated by design (they author nothing), and
// decorateRow fills a row that exactly names a roster person FROM THE
// RECORD — company, trade, position, phone — so one anonymous POST of an
// exact name returned that person's phone and employer (measured live on
// 6a3a0fa, lanes-r2/L13.md). With names readable off the ungated /roster
// and /trips, that is the whole directory, one request per name.
//
// This is that sweep's sibling for the POSTs: every ungated route that
// renders a person row — /gate/passenger/add (an exact name, and a
// near-name that draws a duplicate-offer candidate), /gate/passenger/undo,
// /gate/checkout/select — posted WITHOUT a cookie must carry none of the
// record's phone, position, "company · trade" label or pre-selected
// company/trade picker. Each is paired with the SAME request WITH a session,
// which must still carry them: otherwise the sweep could pass because the
// fixture never put the facts on the page (rule 6), and logged-in behaviour
// is pinned unchanged. (/gate/passenger/delete renders through the same
// renderGatePage choke point with rows the caller itself posted;
// /gate/edit-selected only redirects.)
package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

func postFormAnon(srv *Server, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestNoRecordFieldReachesAnUnauthenticatedGateAccumulatePost(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	chris := persons[2] // "Chris Crew", Kakivik Asset Management · Inspection
	const phone, position = "9075550177", "Zqrigging Foreman"
	ph, pos := phone, position
	if _, err := domain.AmendPerson(st, "site-a", 1, domain.PersonAmendment{PersonID: chris.PersonID, Phone: &ph, Position: &pos}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if chris.CompanyID == nil || chris.TradeID == nil {
		t.Fatal("precondition: the person must hold a company AND a trade on the record")
	}
	orgLabel := "Kakivik Asset Management · Inspection"
	selectedCo := regexp.MustCompile(`value="` + regexp.QuoteMeta(*chris.CompanyID) + `"\s+selected`)
	selectedTr := regexp.MustCompile(`value="` + regexp.QuoteMeta(*chris.TradeID) + `"\s+selected`)

	// On site, so /gate/checkout/select has a visit to add.
	if rec := postForm(t, srv, "/gate/checkin/single", singleCheckInForm(t, srv, chris.Name)); rec.Code != http.StatusSeeOther {
		t.Fatalf("setup check-in = %d: %s", rec.Code, rec.Body.String())
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	visitID := ""
	for _, v := range open {
		if v.PersonID == chris.PersonID {
			visitID = v.VisitID
		}
	}
	if visitID == "" {
		t.Fatal("setup: the person has no open visit")
	}

	cases := []struct {
		name, path string
		form       url.Values
		// what the logged-in response must carry (precondition + unchanged)
		authedHas []string
	}{
		{"add, exact name", "/gate/passenger/add", url.Values{"direction": {gateInbound}, "new_kind": {"existing"}, "new_last": {chris.Name}}, []string{phone, position, orgLabel}},
		{"add, near-name candidate", "/gate/passenger/add", url.Values{"direction": {gateInbound}, "new_kind": {"existing"}, "new_last": {"Crew"}, "new_first": {"Chris"}}, []string{"Use " + chris.Name + " — " + orgLabel}},
		{"undo", "/gate/passenger/undo", url.Values{"direction": {gateInbound}, "removed_typed": {chris.Name}, "removed_kind": {"existing"}, "removed_index": {"0"}}, []string{phone, position, orgLabel}},
		{"checkout select", "/gate/checkout/select", url.Values{"visit_ids": {visitID}}, []string{phone, position, orgLabel}},
	}
	cookie := loginCookie(t, srv, testLoginPassword)
	for _, c := range cases {
		authed := postFormWithCookie(t, srv, c.path, c.form, cookie)
		ab := authed.Body.String()
		for _, want := range c.authedHas {
			if !strings.Contains(ab, want) {
				t.Errorf("%s: precondition — the LOGGED-IN response lacks %q, so the anonymous sweep below would prove nothing (or logged-in behaviour changed): %d", c.name, want, authed.Code)
			}
		}
		anon := postFormAnon(srv, c.path, c.form)
		if anon.Code != http.StatusOK {
			t.Errorf("%s: anonymous POST %s = %d, want 200 (the route is ungated by design)", c.name, c.path, anon.Code)
		}
		body := anon.Body.String()
		if !strings.Contains(body, chris.Name) {
			t.Errorf("%s: precondition — the anonymous response does not even carry the row, so the sweep is sweeping nothing", c.name)
		}
		for _, leak := range []string{phone, position, orgLabel} {
			if strings.Contains(body, leak) {
				t.Errorf("%s: an UNAUTHENTICATED POST %s returned the record's %q (PII-1b / D-56)", c.name, c.path, leak)
			}
		}
		if selectedCo.MatchString(body) || selectedTr.MatchString(body) {
			t.Errorf("%s: an UNAUTHENTICATED POST %s pre-selected the person's company/trade picker — the employer, by another route (PII-1b)", c.name, c.path)
		}
	}
}
