// c1d_unref_resolution_test.go — James, 2026-09-30: unref undoes a
// resolution. POST /arrivals/unref on a RESOLVED arrival reopens the trip
// the resolution bound (its expectation returns) and parks the arrival
// again with its candidates recomputed, so it can be resolved again —
// correctly. Unref of an unresolved or plain arrival is refused exactly as
// before. Every outcome is identical under every delivery order.
package domain

import (
	"errors"
	"testing"

	"sitecontrol/internal/store"
)

func TestC1dUnrefOfAResolvedArrivalReopensTheTripAndParksTheArrivalAgain(t *testing.T) {
	w := newC1ParkedWorld(t)
	if _, err := ResolveParkedArrival(w.b, "site-b", 1, w.parked.ArrivalID, &w.trip2.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// PRECONDITION: resolved and bound.
	if s := stateOf(t, w.b, w.trip2.TripID); s.State != TripStateArrived || s.ArrivalID == nil || *s.ArrivalID != w.parked.ArrivalID {
		t.Fatalf("PRECONDITION: trip2 %s = %q (arrival %v), want arrived by the resolved arrival", w.trip2.TripID, s.State, s.ArrivalID)
	}
	if standing, err := StandingResolution(w.b, w.parked.ArrivalID); err != nil || standing == nil {
		t.Fatalf("PRECONDITION: no standing resolution (err %v)", err)
	}

	env, err := RetractArrivalRef(w.b, "site-b", 1, w.parked.ArrivalID, nil, 0, "default")
	if err != nil {
		t.Fatalf("unref of the resolved arrival refused: %v", err)
	}
	if env.SiteID != "site-b" {
		t.Fatalf("retraction authored at %s, want site-b", env.SiteID)
	}
	check := func(t *testing.T, st *store.Store) {
		s2 := stateOf(t, st, w.trip2.TripID)
		if s2.State != TripStateOpen || s2.ArrivalID != nil {
			t.Fatalf("trip2 %s = %q (arrival %v), want OPEN again — the withdrawn resolution no longer binds it", w.trip2.TripID, s2.State, s2.ArrivalID)
		}
		exp := c1ExpectationIDs(t, st, "site-b")
		if !exp[w.trip1.TripID] || !exp[w.trip2.TripID] {
			t.Fatalf("site-b board = %v, want both trips back", exp)
		}
		c1AssertParkedOver(t, st, w.parked.ArrivalID, w.trip1.TripID, w.trip2.TripID)
		if standing, err := StandingResolution(st, w.parked.ArrivalID); err != nil || standing != nil {
			t.Fatalf("a resolution still stands after the unref: %+v (err %v)", standing, err)
		}
		out, err := LoadUnrefOutcome(st, w.parked.ArrivalID)
		if err != nil || out == nil || !out.WasResolution || out.TripID != w.trip2.TripID || !out.Reopened() || out.Disposition != ArrivalAmbiguous {
			t.Fatalf("unref outcome = %+v (err %v), want a withdrawn resolution of %s, reopened, arrival ambiguous", out, err, w.trip2.TripID)
		}
	}
	check(t, w.b)
	replayAllOrders(t, w.events(w.a, "site-a"), w.events(w.b, "site-b"), check)
}

func TestC1dAnArrivalCanBeResolvedAgainAfterItsResolutionWasWithdrawn(t *testing.T) {
	w := newC1ParkedWorld(t)
	if _, err := ResolveParkedArrival(w.b, "site-b", 1, w.parked.ArrivalID, &w.trip2.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if _, err := RetractArrivalRef(w.b, "site-b", 1, w.parked.ArrivalID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// PRECONDITION: parked again.
	c1AssertParkedOver(t, w.b, w.parked.ArrivalID, w.trip1.TripID, w.trip2.TripID)

	// The correct trip this time.
	if _, err := ResolveParkedArrival(w.b, "site-b", 1, w.parked.ArrivalID, &w.trip1.TripID, nil, 0, "default"); err != nil {
		t.Fatalf("re-resolution refused: %v", err)
	}
	check := func(t *testing.T, st *store.Store) {
		d := dispositionOf(t, st, w.parked.ArrivalID)
		if d.Disposition != ArrivalResolved || d.TripID == nil || *d.TripID != w.trip1.TripID {
			t.Fatalf("arrival = %q → %v, want resolved to %s", d.Disposition, d.TripID, w.trip1.TripID)
		}
		s1 := stateOf(t, st, w.trip1.TripID)
		if s1.State != TripStateArrived || s1.ArrivalID == nil || *s1.ArrivalID != w.parked.ArrivalID {
			t.Fatalf("trip1 %s = %q (arrival %v), want arrived by the re-resolved arrival", w.trip1.TripID, s1.State, s1.ArrivalID)
		}
		if s2 := stateOf(t, st, w.trip2.TripID); s2.State != TripStateOpen || s2.ArrivalID != nil {
			t.Fatalf("trip2 %s = %q (arrival %v), want open — the first, withdrawn resolution stays withdrawn", w.trip2.TripID, s2.State, s2.ArrivalID)
		}
		if standing, err := StandingResolution(st, w.parked.ArrivalID); err != nil || standing == nil || standing.TripID == nil || *standing.TripID != w.trip1.TripID {
			t.Fatalf("standing resolution = %+v (err %v), want %s", standing, err, w.trip1.TripID)
		}
	}
	check(t, w.b)
	replayAllOrders(t, w.events(w.a, "site-a"), w.events(w.b, "site-b"), check)

	// And a second withdrawal reopens trip1 in turn — last mark wins.
	if _, err := RetractArrivalRef(w.b, "site-b", 1, w.parked.ArrivalID, nil, 0, "default"); err != nil {
		t.Fatalf("second unref refused: %v", err)
	}
	if s1 := stateOf(t, w.b, w.trip1.TripID); s1.State != TripStateOpen {
		t.Fatalf("trip1 %s = %q after the second unref, want open", w.trip1.TripID, s1.State)
	}
	c1AssertParkedOver(t, w.b, w.parked.ArrivalID, w.trip1.TripID, w.trip2.TripID)
}

func TestC1dUnrefOfAnUnresolvedOrPlainArrivalIsRefusedExactlyAsBefore(t *testing.T) {
	w := newC1ParkedWorld(t)
	// A plain arrival too (no matching trip).
	plainVID := w7SeedVehicleID(t, w.b, "site-b", "TRK-PLAIN")
	plain, err := RecordUnsolicitedArrival(w.b, "site-b", 1, plainVID, "site-a", []string{w.passenger.PersonID}, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if d := dispositionOf(t, w.b, plain.ArrivalID); d.Disposition != ArrivalPlain {
		t.Fatalf("PRECONDITION: control arrival = %q, want plain", d.Disposition)
	}
	for _, tc := range []struct{ name, arrival string }{
		{"parked, never resolved", w.parked.ArrivalID},
		{"plain", plain.ArrivalID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := c1cEventCount(t, w.b, "site-b")
			_, err := RetractArrivalRef(w.b, "site-b", 1, tc.arrival, nil, 0, "default")
			var ref *UnrefRefusal
			if !errors.As(err, &ref) || ref.Code != UnrefNoReference {
				t.Fatalf("err = %v, want UnrefNoReference", err)
			}
			if after := c1cEventCount(t, w.b, "site-b"); after != before {
				t.Fatalf("site-b authored %d event(s) while refusing", after-before)
			}
		})
	}
	c1AssertParkedOver(t, w.b, w.parked.ArrivalID, w.trip1.TripID, w.trip2.TripID)
}
