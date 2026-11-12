package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

// FIX7 (James, 2026-10-05): "on the dispatch page if i click acknowledge for
// the overdue vehicles it takes that acknowledgement rather than being read
// only. The dispatch is not supposed to be able to edit, only read, so they
// should only have the information displayed, not have access to the
// operator console." An ack from Dispatch silenced the overdue alarm at the
// gates. dispatch_surface.go has the design.

const fix7DispatchHost = "sc-analytics.jnkfacey.com"
const fix7GateHost = "sc-iceroad.jnkfacey.com"

// fix7Overdue is the w1 origin fixture with one outbound run two minutes
// past its one-minute allowance: an OVERDUE red on every page's strip.
func fix7Overdue(t *testing.T) (*Server, func() []byte, string) {
	t.Helper()
	srv, st, d, persons := newOriginServer(t)
	trip, env := sendOut(t, st, persons[0], "TRK-DSP-1", 1)
	d.WallNow = func() time.Time { return time.UnixMilli(env.AuthoredAt.WallMS).Add(2 * time.Minute) }
	dump := func() []byte {
		b, err := st.DumpProjections()
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	return srv, dump, trip.TripID
}

func fix7Do(t *testing.T, srv *Server, method, host, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if form != nil {
		req = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Host = host
	req.AddCookie(loginCookie(t, srv, testLoginPassword))
	rec := httptest.NewRecorder()
	srv.Root().ServeHTTP(rec, req)
	return rec
}

var fix7ConsoleHref = regexp.MustCompile(`href="/(trips|gate|muster|status|roster|trade|orientation|intake|person|vehicle|company|time|arrivals|alerts)\b`)

// (a) RENDERING: with an OVERDUE alert outstanding, both analytics pages show
// the alert's information and carry no ack form, no button, no form at all,
// and no link into the operator console — on the dispatch host and on a gate
// host alike.
func TestFix7AnalyticsPagesShowAlertsReadOnlyWithNoConsoleLinks(t *testing.T) {
	srv, _, _ := fix7Overdue(t)
	// Precondition: the console itself still offers the ack.
	if body := getAs(t, srv, "/trips"); !strings.Contains(body, `action="/alerts/ack"`) {
		t.Fatalf("precondition: /trips offers no ack form for the overdue run")
	}
	for _, host := range []string{fix7DispatchHost, fix7GateHost} {
		for _, path := range []string{"/analytics", "/analytics/site/site-b"} {
			rec := fix7Do(t, srv, "GET", host, path, nil)
			if rec.Code != 200 {
				t.Fatalf("%s%s = %d", host, path, rec.Code)
			}
			body := rec.Body.String()
			if path == "/analytics" {
				bar := topBarOf(t, body)
				// ⟦was: "<code>TRK-DSP-1</code> outbound — OVERDUE", the strip's
				// one full row — FIX13 (James, 2026-10-05): the strip is the
				// compact list, the vehicle on the OVERDUE line with its people⟧
				if lines := fix13Lines(t, bar); !strings.Contains(lines["overdue"], "<code>TRK-DSP-1</code> (1)") {
					t.Errorf("%s%s: the alert's information (vehicle, OVERDUE, people aboard) is missing:\n%s", host, path, bar)
				}
			}
			if strings.Contains(body, "/alerts/ack") || strings.Contains(body, "/alerts/unack") || strings.Contains(body, "Acknowledge (logged)") {
				t.Errorf("%s%s: carries an acknowledge control", host, path)
			}
			if strings.Contains(body, "<form") || strings.Contains(body, "<button") {
				t.Errorf("%s%s: carries a form or button — the dispatch view records nothing", host, path)
			}
			// ⟦was: every host⟧ FIX10 (James 2026-10-05): on a POST host the top
			// bar's Home goes to /gate even from the analytics pages; the
			// no-console-link rule is the DISPATCH host's.
			if m := fix7ConsoleHref.FindString(body); host == fix7DispatchHost && (m != "" || strings.Contains(body, "operator console</a>")) {
				t.Errorf("%s%s: links into the operator console (%s)", host, path, m)
			}
		}
	}
}

// (b) ENFORCEMENT: a crafted ack (and every other write) aimed at the
// dispatch host is refused 403 and nothing is recorded — the projection dump
// (what `sitectl -dump` writes) is byte-identical and the alarm still stands
// at the gate. Console GETs on that host land on /analytics. Then the SAME
// ack through a gate host still works (regression).
func TestFix7DispatchHostRefusesWritesAndTheGateStillAcks(t *testing.T) {
	srv, dump, tripID := fix7Overdue(t)
	before := dump()

	rec := fix7Do(t, srv, "POST", fix7DispatchHost, "/alerts/ack", url.Values{"trip_id": {tripID}})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("crafted ack on the dispatch host = %d, want 403:\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "NOT RECORDED.") || strings.Contains(rec.Body.String(), "<form") {
		t.Errorf("refusal page must say NOT RECORDED and offer no form:\n%s", rec.Body.String())
	}
	for _, p := range []string{"/alerts/unack", "/trips/dispatch", "/trips/arrive", "/trips/returned", "/gate/checkin", "/gate/checkout",
		"/gate/checkout/vehicle", "/roster/intake", "/person/amend", "/vehicle/amend", "/trips/amend", "/time/correct", "/trade/create", "/sync/poke"} {
		if rec := fix7Do(t, srv, "POST", fix7DispatchHost, p, url.Values{"trip_id": {tripID}}); rec.Code != http.StatusForbidden {
			t.Errorf("POST %s on the dispatch host = %d, want 403", p, rec.Code)
		}
	}
	if rec := fix7Do(t, srv, "PUT", fix7DispatchHost+":443", "/alerts/ack", url.Values{"trip_id": {tripID}}); rec.Code != http.StatusForbidden {
		t.Errorf("PUT on the dispatch host (with port) = %d, want 403", rec.Code)
	}
	if after := dump(); !bytes.Equal(before, after) {
		t.Fatalf("a refused dispatch-host write changed the projection dump — something was recorded")
	}
	if bar := topBarOf(t, getAs(t, srv, "/trips")); !strings.Contains(bar, `data-alert-item="overdue_live"`) { // ⟦was: data-alert-row — FIX13⟧
		t.Fatalf("the gate's overdue alarm was silenced by a dispatch-host request")
	}
	for _, p := range []string{"/trips", "/gate", "/muster", "/status", "/", "/roster"} {
		rec := fix7Do(t, srv, "GET", fix7DispatchHost, p, nil)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/analytics" {
			t.Errorf("GET %s on the dispatch host = %d → %q, want 303 → /analytics", p, rec.Code, rec.Header().Get("Location"))
		}
	}

	// Regression: the gate host acks exactly as before.
	rec = fix7Do(t, srv, "POST", fix7GateHost, "/alerts/ack", url.Values{"trip_id": {tripID}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("ack at the gate host = %d, want 303:\n%s", rec.Code, rec.Body.String())
	}
	if bytes.Equal(before, dump()) {
		t.Errorf("the gate's ack recorded nothing")
	}
	if bar := topBarOf(t, getAs(t, srv, "/trips")); strings.Contains(bar, `data-alert-row="overdue_live"`) {
		t.Errorf("the gate's ack did not clear the alarm")
	}
}

func TestFix7DispatchHostRule(t *testing.T) {
	for host, want := range map[string]bool{
		"sc-analytics.jnkfacey.com": true, "analytics.example": true, "dispatch.example:8443": true,
		"SC-ANALYTICS.JNKFACEY.COM": true, "north-dispatch.example": true,
		"sc-iceroad.jnkfacey.com": false, "127.0.0.1:8081": false, "localhost": false,
		"analyticsx.example": false, "sc-pad.jnkfacey.com": false, "example.analytics.com": false,
	} {
		if got := dispatchHost(host); got != want {
			t.Errorf("dispatchHost(%q) = %v, want %v", host, got, want)
		}
	}
}
