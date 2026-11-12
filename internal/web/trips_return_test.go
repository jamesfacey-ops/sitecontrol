// trips_return_test.go — the per-person return disposition over real HTTP
// (operator, 2026-08-20): every arrival is a manifest where each occupant
// has their own outcome; default is check-in (zero extra clicks for the
// common case), the exception is one checkbox. Style regions are stripped
// before markup assertions; outcomes are asserted on store state.
package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// The inbound board's arrival action renders one "stayed behind" control per
// occupant, none pre-checked — the default disposition is check-in.
func TestBoardRendersPerPersonStayedControlsDefaultCheckIn(t *testing.T) {
	srv, st, persons := newTripsTestServer(t)
	injectForeignTrip(t, st, 1, "BUS 7", 60, persons[0].PersonID, persons[1].PersonID)

	page := styleRe.ReplaceAllString(getTrips(t, srv), "")
	if got := strings.Count(page, `name="stayed_person_ids"`); got != 2 {
		t.Fatalf("%d stayed controls on the board, want 2 (one per occupant)", got)
	}
	if !strings.Contains(page, "stayed behind — do not check in") {
		t.Fatal("board is missing the per-person stayed label")
	}
	// Default = everyone in: no stayed checkbox may be pre-checked.
	for _, m := range regexp.MustCompile(`<input[^>]*stayed_person_ids[^>]*>`).FindAllString(page, -1) {
		if strings.Contains(m, "checked") {
			t.Fatalf("a stayed control is pre-checked — the default must be check-in: %s", m)
		}
	}
}

// Arrival with one occupant marked stayed: the vehicle arrives, the trip
// closes, the returnee is on the muster set — and the stayed person is not.
func TestArriveWithStayedPersonOverHTTP(t *testing.T) {
	srv, st, persons := newTripsTestServer(t)
	tripID := injectForeignTrip(t, st, 1, "BUS 7", 60, persons[0].PersonID, persons[1].PersonID)

	rec := postForm(t, srv, "/trips/arrive", url.Values{
		"time_source":       {"default"},
		"trip_id":           {tripID},
		"stayed_person_ids": {persons[1].PersonID},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("arrive with a stayed person = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	open, err := domain.OpenVisitsAt(st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].PersonID != persons[0].PersonID {
		t.Fatalf("open visits = %+v, want exactly the returnee %s", open, persons[0].PersonID)
	}
	states, err := domain.TripStates(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].State != domain.TripStateArrived {
		t.Fatalf("trip states = %+v, want the trip arrived", states)
	}
}

// The operator's scenario end-to-end over HTTP: dispatch an uncontrolled run
// to Conneco with two people, the trips page offers the per-person return
// manifest, the return marks one as stayed — the returnee is back on the
// muster set, the stayed person is named on the out-at-pad panel.
func TestUncontrolledRunReturnManifestOverHTTP(t *testing.T) {
	srv, w := newDestinationsTestServer(t)

	// Both people on site via the ordinary degenerate check-in machinery.
	walkIn, err := domain.DispatchLocalCheckIn(w.st, "site-b", 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "gate walk-in", DriverPersonID: w.persons[0].PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.st, "site-b", "gate walk-in"), DriverPersonID: w.persons[0].PersonID,
		OccupantPersonIDs: []string{w.persons[1].PersonID},
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.RecordExpectedArrival(w.st, "site-b", 1, walkIn.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}

	// The run, through the real dispatch button.
	rec := postForm(t, srv, "/trips/dispatch", url.Values{
		"time_source":         {"default"},
		"vehicle":             {"BUS 7"},
		"driver_person_id":    {w.persons[0].PersonID},
		"occupant_person_ids": {w.persons[1].PersonID},
		"destination_id":      {"conneco"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("uncontrolled dispatch = %d: %s", rec.Code, rec.Body.String())
	}
	var runID string
	trips, err := domain.Trips(w.st)
	if err != nil {
		t.Fatal(err)
	}
	for _, tr := range trips {
		if tr.DestinationUncontrolled != nil {
			runID = tr.TripID
		}
	}
	if runID == "" {
		t.Fatal("no uncontrolled run recorded")
	}

	// The trips page offers the per-person return manifest for the open run.
	page := styleRe.ReplaceAllString(getTrips(t, srv), "")
	if !strings.Contains(page, "Returned — check in everyone not marked as stayed") {
		t.Fatal("trips page does not offer the run's return manifest")
	}
	if !strings.Contains(page, "stayed at Conneco Pad — remains off site") {
		t.Fatal("return manifest rows are not labelled with the pad")
	}

	// Return: Pat stayed at the pad, Dana comes in.
	rec = postForm(t, srv, "/trips/arrive", url.Values{
		"time_source":       {"default"},
		"trip_id":           {runID},
		"stayed_person_ids": {w.persons[1].PersonID},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("run return = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	open, err := domain.OpenVisitsAt(w.st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].PersonID != w.persons[0].PersonID {
		t.Fatalf("open visits after return = %+v, want exactly %s back on site", open, w.persons[0].PersonID)
	}

	// The out-at-pad panel names the stayed person under the pad. U2: that
	// panel is reference/answerability, not something the operator acts on
	// with a truck at the window, so it moved to /trips/detail with the
	// other reference tables. Same panel, same rows, same assertion.
	detail := styleRe.ReplaceAllString(getTripsDetail(t, srv), "")
	if !strings.Contains(detail, "Recorded as out at uncontrolled destinations") {
		t.Fatal("out-at panel missing")
	}
	if !strings.Contains(detail, "Pat Passenger") || !strings.Contains(detail, "Conneco Pad") {
		t.Fatalf("out-at panel does not name the stayed person at the pad:\n%s", detail)
	}
	// The run is closed, so the manifest form is no longer offered on the
	// console (where the return action lives).
	page = styleRe.ReplaceAllString(getTrips(t, srv), "")
	if strings.Contains(page, "Returned — check in everyone not marked as stayed") {
		t.Fatal("closed run still offers a return manifest")
	}
}

// Both sites dispatch: this harness IS site-b (Pad Site Control's shape — the server
// most testing has not driven dispatch from). A controlled dispatch from
// here creates an inbound expectation for the PEER, keyed by this origin.
func TestPeerSideDispatchCreatesExpectationForOtherSite(t *testing.T) {
	srv, w := newDestinationsTestServer(t)
	rec := postForm(t, srv, "/trips/dispatch", url.Values{
		"time_source":      {"default"},
		"vehicle":          {"TRUCK 12"},
		"driver_person_id": {w.persons[0].PersonID},
		"destination_id":   {"site-a"},
		"deadline_minutes": {"60"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("site-b-side controlled dispatch = %d: %s", rec.Code, rec.Body.String())
	}
	exps, err := domain.OpenExpectations(w.st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(exps) != 1 || exps[0].Trip.OriginSite != "site-b" {
		t.Fatalf("site-a's inbound board = %+v, want one expectation originated at site-b", exps)
	}
}
