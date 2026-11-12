// c1d_unref_resolution_alert_test.go — James, 2026-09-30: unref undoes a
// resolution, at the scanner: withdrawing the resolution brings the trip's
// live overdue alert back.
package deadline

import (
	"testing"
	"time"

	"sitecontrol/internal/domain"
)

func TestC1dWithdrawingAResolutionBringsTheTripsOverdueAlertBack(t *testing.T) {
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
	time.Sleep(2 * time.Second)
	if _, err := domain.ResolveParkedArrival(st, here, 1, u.ArrivalID, &trip2.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// PRECONDITION: trip2's alert is gone, trip1's is live.
	mid := mustEval(t, d)
	for _, a := range mid.Alerts {
		if a.TripID == trip2.TripID {
			t.Fatalf("PRECONDITION: trip2 %s still on the board after resolution: %+v", trip2.TripID, a)
		}
	}
	if a := findAlert(t, mid, trip1.TripID); a.Status != StatusOverdueLive {
		t.Fatalf("PRECONDITION: trip1 status %q, want overdue_live", a.Status)
	}

	if _, err := domain.RetractArrivalRef(st, here, 1, u.ArrivalID, nil, 0, "default"); err != nil {
		t.Fatalf("unref of the resolved arrival refused: %v", err)
	}
	after := mustEval(t, d)
	a2 := findAlert(t, after, trip2.TripID) // fatal if the row did not return
	if a2.Status != StatusOverdueLive || a2.OverdueBy <= 0 {
		t.Fatalf("trip2 %s alert after unref = %q overdue-by %s, want overdue_live > 0 — the alert must come back", trip2.TripID, a2.Status, a2.OverdueBy)
	}
	if a1 := findAlert(t, after, trip1.TripID); a1.Status != StatusOverdueLive {
		t.Fatalf("trip1 %s alert = %q, want still overdue_live", trip1.TripID, a1.Status)
	}
}
