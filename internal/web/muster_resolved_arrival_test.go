// muster_resolved_arrival_test.go — ⟦R3-07, 2026-09-30⟧ the muster after an
// officer resolves a parked arrival ("This was trip X", the real
// POST /arrivals/resolve door): the person that arrival checked in is ON
// SITE and must NOT also be filed under NOT TRACKED "not aboard at arrival —
// whereabouts not recorded" on the same sheet. Before the fix
// domain.NotTracked keyed aboard on VisitsByTrip and an unsolicited
// arrival's visits carry trip_id NULL, so both lines printed at every post
// (lanes-r3/L08.md R3-07). RED on d6e5e0e, GREEN after; the precondition
// (visits under trip_id NULL, trip bound to the resolved arrival) is
// asserted so the test cannot pass by VisitsByTrip alone.
package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

func TestMusterDoesNotFileAResolvedArrivalsOccupantAsNotTracked(t *testing.T) {
	srv, w := newDestinationsTestServer(t)
	driver, other := w.persons[0], w.persons[1]
	// T-092's arrangement in this store: two open same-key trips from
	// site-a to site-b (this post), then an unsolicited arrival here on that
	// key carrying trip1's driver — parked over both.
	dest, deadline := "site-b", int64(30)
	vehicleID := w7SeedVehicleID(t, w.st, "site-a", "TRK-950")
	trip1, err := domain.Dispatch(w.st, "site-a", 1, domain.DispatchInput{
		VehicleID: vehicleID, DriverPersonID: driver.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	trip2, err := domain.Dispatch(w.st, "site-a", 1, domain.DispatchInput{
		VehicleID: vehicleID, DriverPersonID: other.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	parked, err := domain.RecordUnsolicitedArrival(w.st, "site-b", 1, vehicleID, "site-a",
		[]string{driver.PersonID}, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if d := arrivalDispositionOf(t, w, parked.ArrivalID); d.Disposition != domain.ArrivalAmbiguous || d.TripID != nil {
		t.Fatalf("PRECONDITION: arrival = %q → %v, want parked (ambiguous, unbound)", d.Disposition, d.TripID)
	}

	// The officer resolves it to trip1 through the real door.
	rec := postForm(t, srv, "/arrivals/resolve", url.Values{
		"arrival_id": {parked.ArrivalID}, "trip_id": {trip1.TripID}, "resolve_trip": {trip1.TripID},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /arrivals/resolve = %d: %s", rec.Code, rec.Body.String())
	}
	// PRECONDITION (rule 6): trip1 is arrived BY the resolved arrival, whose
	// visit carries trip_id NULL — the shape that made the defect.
	states, err := domain.TripStates(w.st)
	if err != nil {
		t.Fatal(err)
	}
	bound := false
	for _, s := range states {
		if s.TripID == trip1.TripID {
			bound = s.State == domain.TripStateArrived && s.ArrivalID != nil && *s.ArrivalID == parked.ArrivalID
		}
		if s.TripID == trip2.TripID && s.State != domain.TripStateOpen {
			t.Fatalf("PRECONDITION: trip2 = %q, want still open", s.State)
		}
	}
	if !bound {
		t.Fatalf("PRECONDITION: trip1 is not arrived by the resolved arrival %s", parked.ArrivalID)
	}
	if v, err := domain.VisitsByTrip(w.st, trip1.TripID); err != nil || len(v) != 0 {
		t.Fatalf("PRECONDITION: VisitsByTrip(trip1) = %+v (err %v), want none — the resolved arrival's visit carries trip_id NULL", v, err)
	}
	open, err := domain.OpenVisitsAt(w.st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	onSite := false
	for _, v := range open {
		if v.PersonID == driver.PersonID && v.TripID == nil {
			onSite = true
		}
	}
	if !onSite {
		t.Fatalf("PRECONDITION: %s has no open trip-less visit at site-b: %+v", driver.Name, open)
	}

	for _, path := range []string{"/muster", "/muster/print"} {
		page := getPage(t, srv, path)
		if !strings.Contains(page, driver.Name) {
			t.Fatalf("%s: %s (on site) is not on the sheet:\n%s", path, driver.Name, page)
		}
		if strings.Contains(page, `data-muster-section="not-tracked"`) {
			t.Fatalf("%s: a Not tracked section rendered — %s is the person the resolved arrival checked in, ON SITE on this very sheet:\n%s", path, driver.Name, page)
		}
		if !strings.Contains(page, `data-tally-count="not-tracked">0<`) {
			t.Fatalf("%s: not-tracked tally is not an explicit 0:\n%s", path, page)
		}
		if strings.Contains(page, "not aboard at arrival") {
			t.Fatalf("%s: 'not aboard at arrival' printed for a trip whose binding arrival entered its crew:\n%s", path, page)
		}
	}
}

// arrivalDispositionOf reads the arrival_state row for one arrival.
func arrivalDispositionOf(t *testing.T, w *destWorld, arrivalID string) domain.ArrivalState {
	t.Helper()
	states, err := domain.ArrivalStates(w.st)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range states {
		if s.ArrivalID == arrivalID {
			return s
		}
	}
	t.Fatalf("no arrival_state row for %s", arrivalID)
	return domain.ArrivalState{}
}
