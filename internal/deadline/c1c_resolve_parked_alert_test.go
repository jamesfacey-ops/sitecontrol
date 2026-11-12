// c1c_resolve_parked_alert_test.go — James, 2026-09-30: officer resolution
// of parked arrivals (Option 1) at the scanner: resolving the parked arrival
// to one candidate clears THAT trip's alert and no other.
package deadline

import (
	"testing"
	"time"

	"sitecontrol/internal/domain"
)

func TestC1cResolvingTheParkedArrivalToOneTripClearsOnlyThatTripsAlert(t *testing.T) {
	st := newSite(t)
	d := newScanner(t, st)
	drv1, err := domain.CreatePerson(st, origin, 1, "Driver One", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	drv2, err := domain.CreatePerson(st, origin, 1, "Driver Two", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	trip1, env1 := dispatchInbound(t, st, 1, drv1.PersonID)
	trip2, env2 := dispatchInbound(t, st, 1, drv2.PersonID)
	anchor := time.Now().Add(-time.Minute + 1500*time.Millisecond)
	d.OnApplied(env1, anchor, true)
	d.OnApplied(env2, anchor, true)
	pax, err := domain.CreatePerson(st, here, 1, "Parked Passenger", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	vid := w7SeedVehicleID(t, st, vehicleHome, "TRUCK 12")
	u, err := domain.RecordUnsolicitedArrival(st, here, 1, vid, origin, []string{pax.PersonID}, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	as, err := domain.ArrivalStateByID(st, u.ArrivalID)
	if err != nil || as == nil || as.Disposition != domain.ArrivalAmbiguous {
		t.Fatalf("PRECONDITION: arrival state %+v (err %v), want ambiguous", as, err)
	}
	time.Sleep(2 * time.Second)
	before := mustEval(t, d)
	for _, id := range []string{trip1.TripID, trip2.TripID} {
		if a := findAlert(t, before, id); a.Status != StatusOverdueLive {
			t.Fatalf("PRECONDITION: trip %s status %q, want overdue_live", id, a.Status)
		}
	}

	if _, err := domain.ResolveParkedArrival(st, here, 1, u.ArrivalID, &trip2.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	after := mustEval(t, d)
	for _, a := range after.Alerts {
		if a.TripID == trip2.TripID {
			t.Fatalf("trip2 %s is still on the board after being resolved as arrived: %+v", trip2.TripID, a)
		}
	}
	a1 := findAlert(t, after, trip1.TripID)
	if a1.Status != StatusOverdueLive || a1.OverdueBy <= 0 {
		t.Fatalf("trip1 %s alert = %q overdue-by %s, want still overdue_live > 0", trip1.TripID, a1.Status, a1.OverdueBy)
	}
}
