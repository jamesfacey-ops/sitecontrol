// c1c_resolve_surface_test.go — James, 2026-09-30: officer resolution of
// parked arrivals (Option 1), at the surface: the review row renders one
// PLAIN FORM per still-open candidate plus "None of these" (JS off works),
// a candidate since closed is annotated and gets no button, POST
// /arrivals/resolve binds or dismisses and the confirmation READS the
// recomputed state, every refusal is an honest 409 that authors nothing,
// and another post's parked arrival shows no controls and is refused with
// the post to radio (D-87).
package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// c1cGet is an authenticated GET (the PII fix gates views), body returned.
func c1cGet(t *testing.T, srv *Server, path string) string {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.AddCookie(loginCookie(t, srv, testLoginPassword))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// c1cParkAt parks an unsolicited arrival at `site`: two same-key trips from
// site-c to `site`, then an unsolicited arrival at `site` on that key.
// Asserts the arrival is ambiguous over exactly [t1, t2].
func c1cParkAt(t *testing.T, st *store.Store, site, number string, drivers [2]domain.Person, pax domain.Person) (arrivalID, t1, t2 string) {
	t.Helper()
	num := number
	v, err := domain.CreateVehicle(st, "site-c", 1, &num, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	dest := site
	deadline := int64(30)
	var trips [2]domain.Trip
	for i, d := range drivers {
		trips[i], err = domain.Dispatch(st, "site-c", 1, domain.DispatchInput{
			VehicleID: v.VehicleID, DriverPersonID: d.PersonID,
			DestinationSite: &dest, DeadlineMinutes: &deadline,
		}, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
	}
	arr, err := domain.RecordUnsolicitedArrival(st, site, 1, v.VehicleID, "site-c", []string{pax.PersonID}, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	as, err := domain.ArrivalStateByID(st, arr.ArrivalID)
	if err != nil {
		t.Fatal(err)
	}
	want := trips[0].TripID + "," + trips[1].TripID
	if as == nil || as.Disposition != domain.ArrivalAmbiguous || as.Candidates == nil || *as.Candidates != want {
		t.Fatalf("PRECONDITION: arrival state %+v, want ambiguous over %q", as, want)
	}
	return arr.ArrivalID, trips[0].TripID, trips[1].TripID
}

func c1cOwnParked(t *testing.T) (*Server, *store.Store, string, string, string) {
	t.Helper()
	srv, st, persons := newGateTestServer(t)
	a, t1, t2 := c1cParkAt(t, st, "site-a", "TRK-950", [2]domain.Person{persons[0], persons[1]}, persons[2])
	return srv, st, a, t1, t2
}

func TestC1cReviewRowRendersOnePlainFormPerOpenCandidateAndNoneOfThese(t *testing.T) {
	srv, st, arrival, t1, t2 := c1cOwnParked(t)
	body := c1cGet(t, srv, "/trips")
	for _, want := range []string{
		`data-review-kind="ambiguous-arrival"`,
		"matched more than one open trip when it was recorded",
		`data-resolve-arrival="` + arrival + `"`,
		`<form method="post" action="/arrivals/resolve"`,
		`name="arrival_id" value="` + arrival + `"`,
		`name="trip_id" value="` + t1 + `"`,
		`name="trip_id" value="` + t2 + `"`,
		"This was trip " + t1,
		"This was trip " + t2,
		"driver Dana Driver",
		"driver Pat Passenger",
		"TRK-950 from site-c, dispatched ",
		`name="none" value="1"`,
		"None of these (plain visit)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/trips lacks %q", want)
		}
	}
	// The controls are plain forms: nothing between the row's open tag and
	// its last form depends on script.
	lane := body[strings.Index(body, `data-resolve-arrival="`+arrival+`"`):]
	lane = lane[:strings.Index(lane, "None of these")]
	if strings.Contains(lane, "<script") || strings.Contains(lane, "onclick") {
		t.Errorf("the resolution controls depend on script:\n%s", lane)
	}

	// t1 closed since by an explicit arrival: annotated, no button.
	if _, err := domain.RecordExpectedArrival(st, "site-a", 1, t1, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	body = c1cGet(t, srv, "/trips")
	if strings.Contains(body, `name="trip_id" value="`+t1+`"`) {
		t.Errorf("t1 %s still offers a button after its explicit arrival", t1)
	}
	if !strings.Contains(body, "since arrived") || !strings.Contains(body, "checked in at site-a") {
		t.Errorf("t1 is not annotated as since arrived at site-a")
	}
	if !strings.Contains(body, `name="trip_id" value="`+t2+`"`) {
		t.Errorf("t2 %s lost its button", t2)
	}
	if !strings.Contains(body, `data-resolve-arrival="`+arrival+`"`) {
		t.Errorf("the parked row lost its controls once one candidate closed")
	}
}

func TestC1cResolvePostBindsTheTripAndTheConfirmationReadsTheRecomputedState(t *testing.T) {
	srv, st, arrival, t1, t2 := c1cOwnParked(t)
	before := w2Watermark(t, st, "site-a", 1)
	rec := postForm(t, srv, "/arrivals/resolve", url.Values{"arrival_id": {arrival}, "trip_id": {t2}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /arrivals/resolve = %d, want 303; body:\n%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/arrivals/resolve?recorded=site-a.1.") {
		t.Fatalf("Location = %q, want the PRG key on this surface", loc)
	}
	if got := w2Watermark(t, st, "site-a", 1); got != before+1 {
		t.Fatalf("appended %d event(s), want exactly 1", got-before)
	}
	conf := c1cGet(t, srv, loc)
	for _, want := range []string{t2, "closed at every post by your selection", arrival} {
		if !strings.Contains(conf, want) {
			t.Errorf("confirmation lacks %q:\n%s", want, conf)
		}
	}
	ts, err := domain.TripStateByID(st, t2)
	if err != nil || ts == nil || ts.State != domain.TripStateArrived || ts.ArrivalID == nil || *ts.ArrivalID != arrival {
		t.Fatalf("trip2 state %+v (err %v), want arrived by %s", ts, err, arrival)
	}
	if ts, _ := domain.TripStateByID(st, t1); ts == nil || ts.State != domain.TripStateOpen {
		t.Fatalf("trip1 state %+v, want open", ts)
	}
	body := c1cGet(t, srv, "/trips")
	if strings.Contains(body, `data-review-kind="ambiguous-arrival"`) {
		t.Errorf("the ambiguous row is still on /trips after resolution")
	}
}

func TestC1cResolveNoneOfThesePostLeavesEveryTripOpenAndConfirmsAPlainVisit(t *testing.T) {
	srv, st, arrival, t1, t2 := c1cOwnParked(t)
	rec := postForm(t, srv, "/arrivals/resolve", url.Values{"arrival_id": {arrival}, "none": {"1"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST none = %d, want 303; body:\n%s", rec.Code, rec.Body.String())
	}
	conf := c1cGet(t, srv, rec.Header().Get("Location"))
	if !strings.Contains(conf, "plain visit") || !strings.Contains(conf, arrival) {
		t.Errorf("confirmation lacks the plain-visit sentence:\n%s", conf)
	}
	for _, id := range []string{t1, t2} {
		if ts, _ := domain.TripStateByID(st, id); ts == nil || ts.State != domain.TripStateOpen {
			t.Fatalf("trip %s state %+v, want open", id, ts)
		}
	}
	if strings.Contains(c1cGet(t, srv, "/trips"), `data-review-kind="ambiguous-arrival"`) {
		t.Errorf("the ambiguous row is still on /trips after 'none of these'")
	}
}

func TestC1cEveryResolveRefusalIsA409ThatAuthorsNothing(t *testing.T) {
	srv, st, arrival, t1, t2 := c1cOwnParked(t)
	if _, err := domain.RecordExpectedArrival(st, "site-a", 1, t1, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		form url.Values
		want string
	}{
		{"unknown arrival", url.Values{"arrival_id": {"site-a-arrival-0000000000000000"}, "trip_id": {t2}}, "no record of arrival"},
		{"trip not a candidate", url.Values{"arrival_id": {arrival}, "trip_id": {"no-such-trip"}}, "is not\none of the trips"},
		{"trip since closed", url.Values{"arrival_id": {arrival}, "trip_id": {t1}}, "since been closed by arrival"},
		{"no choice", url.Values{"arrival_id": {arrival}}, "No choice was made"},
		{"both a trip and none", url.Values{"arrival_id": {arrival}, "trip_id": {t2}, "none": {"1"}}, "No choice was made"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := w2Watermark(t, st, "site-a", 1)
			rec := postForm(t, srv, "/arrivals/resolve", tc.form)
			body := rec.Body.String()
			if rec.Code != http.StatusConflict {
				t.Fatalf("status %d, want 409; body:\n%s", rec.Code, body)
			}
			if !strings.Contains(body, "Nothing was recorded") || !strings.Contains(strings.ReplaceAll(body, "\n", " "), strings.ReplaceAll(tc.want, "\n", " ")) {
				t.Errorf("refusal lacks %q:\n%s", tc.want, body)
			}
			if after := w2Watermark(t, st, "site-a", 1); after != before {
				t.Errorf("authored %d event(s) while refusing", after-before)
			}
		})
	}
	// Already resolved.
	if rec := postForm(t, srv, "/arrivals/resolve", url.Values{"arrival_id": {arrival}, "trip_id": {t2}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("resolution = %d, want 303", rec.Code)
	}
	before := w2Watermark(t, st, "site-a", 1)
	rec := postForm(t, srv, "/arrivals/resolve", url.Values{"arrival_id": {arrival}, "none": {"1"}})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "already been resolved") {
		t.Fatalf("second resolution = %d, want 409 'already been resolved'; body:\n%s", rec.Code, rec.Body.String())
	}
	if after := w2Watermark(t, st, "site-a", 1); after != before {
		t.Errorf("authored %d event(s) while refusing the second resolution", after-before)
	}
}

func TestC1cAnotherPostsParkedArrivalShowsNoControlsAndIsRefusedNamingThePostToRadio(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	co, err := domain.CreateCompany(st, "site-b", 1, "Nanuq Drilling Services", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	var people [3]domain.Person
	for i, name := range []string{"Faraway, Fred", "Faraway, Frances", "Faraway, Finn"} {
		if people[i], err = domain.CreatePerson(st, "site-b", 1, name, &co.CompanyID, nil, 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	foreign, _, t2 := c1cParkAt(t, st, "site-b", "TRK-B", [2]domain.Person{people[0], people[1]}, people[2])

	body := c1cGet(t, srv, "/trips")
	if !strings.Contains(body, `data-review-kind="ambiguous-arrival"`) || !strings.Contains(body, foreign) {
		t.Fatalf("site-b's parked arrival is not on site-a's review lane")
	}
	if strings.Contains(body, `data-resolve-arrival="`+foreign+`"`) {
		t.Errorf("site-a renders resolution controls for site-b's arrival (D-87)")
	}
	before := w2Watermark(t, st, "site-a", 1)
	rec := postForm(t, srv, "/arrivals/resolve", url.Values{"arrival_id": {foreign}, "trip_id": {t2}})
	if rec.Code != http.StatusConflict {
		t.Fatalf("POST against site-b's arrival = %d, want 409; body:\n%s", rec.Code, rec.Body.String())
	}
	// ⟦was: store.OwnershipRule, which ended "(D-87)" — a decision id on an
	// officer-visible refusal. FIX5 pins the officer's words literally and
	// that no decision id is shown.⟧
	if strings.Contains(rec.Body.String(), "(D-87)") {
		t.Errorf("the refusal shows the decision id \"(D-87)\" to the officer; body:\n%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), fix5OwnershipWords) || !strings.Contains(rec.Body.String(), "Radio "+srv.Cfg.SiteName("site-b")) {
		t.Errorf("the 409 does not carry the D-87 sentence and 'Radio site-b'; body:\n%s", rec.Body.String())
	}
	if after := w2Watermark(t, st, "site-a", 1); after != before {
		t.Errorf("site-a appended %d event(s) while refusing", after-before)
	}
}
