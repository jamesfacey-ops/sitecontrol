// c1b_crosssite_parked_alert_test.go — C-1 / T-092 cross-site variant at
// the scanner (James, 2026-09-30, option b): the officer's explicit arrival
// on the first candidate is authored at ANOTHER post, and the other
// candidate's live overdue alert on this post's board must survive.
package deadline

import (
	"testing"
	"time"

	"sitecontrol/internal/domain"
)

// TestC1bOtherCandidatesLiveOverdueAlertSurvivesAnExplicitArrivalOnItsSiblingAtAnotherPost
// FAILS on 6a3a0fa: the cross-site binder was excluded from the parked
// arrival's candidate set, it bound trip2 by elimination, and trip2's
// alert left the board.
func TestC1bOtherCandidatesLiveOverdueAlertSurvivesAnExplicitArrivalOnItsSiblingAtAnotherPost(t *testing.T) {
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
	// 1.5 s of countdown left at receipt (not the 80 ms the package's own
	// countdown test uses): under a loaded machine 80 ms is blown between
	// computing the anchor and the receipt, and X−G ≤ 0 at receipt reads as
	// overdue-UNRESOLVED — a precondition failure, not a defect signal.
	anchor := time.Now().Add(-time.Minute + 1500*time.Millisecond)
	d.OnApplied(env1, anchor, true)
	d.OnApplied(env2, anchor, true)

	// Parked here (the destination).
	pax, err := domain.CreatePerson(st, here, 1, "Parked Passenger", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	vid := w7SeedVehicleID(t, st, vehicleHome, "TRUCK 12")
	u, err := domain.RecordUnsolicitedArrival(st, here, 1, vid, origin, []string{pax.PersonID}, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
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
	time.Sleep(2 * time.Second)
	before := mustEval(t, d)
	for _, id := range []string{trip1.TripID, trip2.TripID} {
		if a := findAlert(t, before, id); a.Status != StatusOverdueLive {
			t.Fatalf("PRECONDITION: trip %s status %q, want overdue_live", id, a.Status)
		}
	}

	// "Arrived" on trip1 — at a THIRD post (D-96: it arrives where it arrives).
	other := "site-c"
	if other == here || other == origin {
		t.Fatalf("PRECONDITION: the explicit arrival must be authored at a post other than %s/%s", here, origin)
	}
	if _, err := domain.RecordExpectedArrival(st, other, 1, trip1.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	after := mustEval(t, d)
	for _, a := range after.Alerts {
		if a.TripID == trip1.TripID {
			t.Fatalf("trip1 %s is still on the board after its explicit arrival: %+v", trip1.TripID, a)
		}
	}
	a2 := findAlert(t, after, trip2.TripID) // fatal if the row is gone
	if a2.Status != StatusOverdueLive || a2.OverdueBy <= 0 {
		t.Fatalf("trip2 %s alert = %q overdue-by %s, want still overdue_live > 0", trip2.TripID, a2.Status, a2.OverdueBy)
	}
}
