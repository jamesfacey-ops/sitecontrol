// c1_parked_arrival_alert_test.go — C-1 / T-092 at the scanner: the OTHER
// candidate's live overdue alert must survive the officer's explicit arrival
// on the first candidate while an unsolicited arrival sits parked in the
// ambiguous review lane. The domain suite (internal/domain/
// c1_parked_arrival_test.go) proves the trip stays open; this proves the
// thing an officer would actually see — the alert row on the board.
package deadline

import (
	"testing"
	"time"

	"sitecontrol/internal/domain"
)

// TestC1OtherCandidatesLiveOverdueAlertSurvivesAnExplicitArrivalOnItsSibling
// FAILS on 9a2fb30: after the explicit arrival on trip1, the parked arrival
// was bound to trip2 by elimination and trip2's alert left the board.
func TestC1OtherCandidatesLiveOverdueAlertSurvivesAnExplicitArrivalOnItsSibling(t *testing.T) {
	st := newSite(t)
	d := newScanner(t, st)

	// Two same-key inbound trips (same vehicle record, same origin), both
	// with a 1-minute deadline anchored 80 ms from expiry. Their drivers are
	// on the roster: the explicit arrival door below refuses a trip whose
	// occupants it cannot find.
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
	// 1.5 s of countdown left at receipt (not the 80 ms the package's own
	// countdown test uses): under a loaded machine 80 ms is blown between
	// computing the anchor and the receipt, and X−G ≤ 0 at receipt reads as
	// overdue-UNRESOLVED — a precondition failure, not a defect signal
	// (measured 2026-09-30 with three suites running).
	anchor := time.Now().Add(-time.Minute + 1500*time.Millisecond)
	d.OnApplied(env1, anchor, true)
	d.OnApplied(env2, anchor, true)

	// An unsolicited arrival here carrying that key — parked as ambiguous.
	pax, err := domain.CreatePerson(st, here, 1, "Parked Passenger", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	vid := w7SeedVehicleID(t, st, vehicleHome, "TRUCK 12")
	u, err := domain.RecordUnsolicitedArrival(st, here, 1, vid, origin, []string{pax.PersonID}, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// PRECONDITION: parked over both trips, binding nothing.
	arrivals, err := domain.ArrivalStates(st)
	if err != nil {
		t.Fatal(err)
	}
	parked := false
	for _, a := range arrivals {
		if a.ArrivalID == u.ArrivalID {
			want := trip1.TripID + "," + trip2.TripID
			if a.Disposition != domain.ArrivalAmbiguous || a.TripID != nil || a.Candidates == nil || *a.Candidates != want {
				t.Fatalf("PRECONDITION: unsolicited arrival = %q → %v over %v, want ambiguous over %q", a.Disposition, a.TripID, a.Candidates, want)
			}
			parked = true
		}
	}
	if !parked {
		t.Fatalf("PRECONDITION: no arrival_state row for %s", u.ArrivalID)
	}

	// PRECONDITION: both alerts live and overdue before the officer acts.
	time.Sleep(2 * time.Second)
	before := mustEval(t, d)
	for _, id := range []string{trip1.TripID, trip2.TripID} {
		if a := findAlert(t, before, id); a.Status != StatusOverdueLive {
			t.Fatalf("PRECONDITION: trip %s status %q, want overdue_live", id, a.Status)
		}
	}

	// T-092 step 4: "Arrived" on trip1's row.
	if _, err := domain.RecordExpectedArrival(st, here, 1, trip1.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	after := mustEval(t, d)
	for _, a := range after.Alerts {
		if a.TripID == trip1.TripID {
			t.Fatalf("trip1 %s is still on the board after its explicit arrival: %+v", trip1.TripID, a)
		}
	}
	a2 := findAlert(t, after, trip2.TripID) // fatal if the row is gone — the T-092 loss
	if a2.Status != StatusOverdueLive || a2.OverdueBy <= 0 {
		t.Fatalf("trip2 %s alert = %q overdue-by %s, want still overdue_live > 0 — nobody has recorded its arrival", trip2.TripID, a2.Status, a2.OverdueBy)
	}
}
