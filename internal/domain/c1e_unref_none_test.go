// c1e_unref_none_test.go — R3-RES-3: withdrawing a "None of these"
// resolution is an outcome of its own (no trip to reopen; the arrival is
// parked again), never a "pending" record about the post's own arrival.
package domain

import "testing"

func TestC1eUnrefOfANoneResolutionReportsTheWithdrawalNotAPendingRecord(t *testing.T) {
	w := newC1ParkedWorld(t)
	if _, err := ResolveParkedArrival(w.b, "site-b", 1, w.parked.ArrivalID, nil, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if d := dispositionOf(t, w.b, w.parked.ArrivalID); d.Disposition != ArrivalPlain {
		t.Fatalf("PRECONDITION: arrival = %q after 'none of these', want plain", d.Disposition)
	}
	if _, err := RetractArrivalRef(w.b, "site-b", 1, w.parked.ArrivalID, nil, 0, "default"); err != nil {
		t.Fatalf("unref of the none-resolution refused: %v", err)
	}
	c1AssertParkedOver(t, w.b, w.parked.ArrivalID, w.trip1.TripID, w.trip2.TripID)
	out, err := LoadUnrefOutcome(w.b, w.parked.ArrivalID)
	if err != nil {
		t.Fatal(err)
	}
	if out == nil {
		t.Fatalf("LoadUnrefOutcome = nil for the post's own withdrawn 'none of these' — the surface renders nil as PENDING (R3-RES-3)")
	}
	if !out.WasResolution || !out.WasNone || out.TripID != "" || out.Disposition != ArrivalAmbiguous {
		t.Fatalf("outcome = %+v, want a withdrawn NONE resolution with no trip and the arrival ambiguous again", out)
	}
	if out.Candidates != w.trip1.TripID+","+w.trip2.TripID {
		t.Fatalf("outcome candidates = %q, want the recomputed [%s,%s]", out.Candidates, w.trip1.TripID, w.trip2.TripID)
	}
	for _, id := range []string{w.trip1.TripID, w.trip2.TripID} {
		if s := stateOf(t, w.b, id); s.State != TripStateOpen {
			t.Fatalf("trip %s = %q, want open — a withdrawn 'none' changes no trip", id, s.State)
		}
	}
}
