// c1c_resolve_parked_test.go — James, 2026-09-30: officer resolution of
// parked arrivals (Option 1). A parked (ambiguous) arrival can be resolved
// by the post that recorded it to one of its candidate trips — binding it
// exactly as an explicit arrival would — or to "none of these", which
// leaves it a plain visit. Every refusal authors nothing; every outcome is
// the same under every delivery order and on a from-zero rebuild.
package domain

import (
	"errors"
	"testing"

	"sitecontrol/internal/store"
)

func c1cEventCount(t *testing.T, st *store.Store, site string) int {
	t.Helper()
	evs, err := st.Events(site, 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	return len(evs)
}

func TestC1cResolvingAParkedArrivalToATripBindsItExactlyLikeAnExplicitArrival(t *testing.T) {
	w := newC1ParkedWorld(t)
	visitsBefore := openVisitsAt(t, w.b, "site-b")

	env, err := ResolveParkedArrival(w.b, "site-b", 1, w.parked.ArrivalID, &w.trip2.TripID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := ResolutionsOf(w.b, w.parked.ArrivalID); err != nil || len(rows) != 1 || rows[0].TripID == nil || *rows[0].TripID != w.trip2.TripID {
		t.Fatalf("resolution rows = %+v (err %v), want exactly one naming %s", rows, err, w.trip2.TripID)
	}
	if env.SiteID != "site-b" {
		t.Fatalf("resolution authored at %s, want site-b", env.SiteID)
	}

	check := func(t *testing.T, st *store.Store) {
		d := dispositionOf(t, st, w.parked.ArrivalID)
		if d.Disposition != ArrivalResolved || d.TripID == nil || *d.TripID != w.trip2.TripID {
			t.Fatalf("parked arrival = %q → %v, want %q → %s", d.Disposition, d.TripID, ArrivalResolved, w.trip2.TripID)
		}
		s2 := stateOf(t, st, w.trip2.TripID)
		if s2.State != TripStateArrived || s2.ArrivalID == nil || *s2.ArrivalID != w.parked.ArrivalID {
			t.Fatalf("trip2 %s = %q (arrival %v), want arrived by the resolved arrival %s", w.trip2.TripID, s2.State, s2.ArrivalID, w.parked.ArrivalID)
		}
		s1 := stateOf(t, st, w.trip1.TripID)
		if s1.State != TripStateOpen || s1.ArrivalID != nil {
			t.Fatalf("trip1 %s = %q (arrival %v), want still open — resolving to trip2 must touch nothing else", w.trip1.TripID, s1.State, s1.ArrivalID)
		}
		exp := c1ExpectationIDs(t, st, "site-b")
		if !exp[w.trip1.TripID] || exp[w.trip2.TripID] {
			t.Fatalf("site-b board = %v, want trip1 only", exp)
		}
		lane, err := LoadReviewLane(st)
		if err != nil {
			t.Fatal(err)
		}
		if len(lane.Ambiguous) != 0 {
			t.Fatalf("review lane Ambiguous = %+v, want empty — the row clears on resolution", lane.Ambiguous)
		}
		// I-1: the visit record is untouched.
		if got := openVisitsAt(t, st, "site-b"); len(got) != len(visitsBefore) || got[0].VisitID != visitsBefore[0].VisitID {
			t.Fatalf("site-b open visits changed: %+v → %+v", visitsBefore, got)
		}
	}
	check(t, w.b)
	replayAllOrders(t, w.events(w.a, "site-a"), w.events(w.b, "site-b"), check)
}

func TestC1cResolvingAParkedArrivalAsNoneOfTheseLeavesEveryTripOpenAndClearsTheRow(t *testing.T) {
	w := newC1ParkedWorld(t)
	visitsBefore := openVisitsAt(t, w.b, "site-b")

	if _, err := ResolveParkedArrival(w.b, "site-b", 1, w.parked.ArrivalID, nil, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	check := func(t *testing.T, st *store.Store) {
		d := dispositionOf(t, st, w.parked.ArrivalID)
		if d.Disposition != ArrivalPlain || d.TripID != nil || d.Candidates != nil {
			t.Fatalf("parked arrival = %q → %v (candidates %v), want plain, unbound, no candidates", d.Disposition, d.TripID, d.Candidates)
		}
		for _, id := range []string{w.trip1.TripID, w.trip2.TripID} {
			if s := stateOf(t, st, id); s.State != TripStateOpen || s.ArrivalID != nil {
				t.Fatalf("trip %s = %q (arrival %v), want open — none of these closes nothing", id, s.State, s.ArrivalID)
			}
		}
		exp := c1ExpectationIDs(t, st, "site-b")
		if !exp[w.trip1.TripID] || !exp[w.trip2.TripID] {
			t.Fatalf("site-b board = %v, want both trips", exp)
		}
		lane, err := LoadReviewLane(st)
		if err != nil {
			t.Fatal(err)
		}
		if len(lane.Ambiguous) != 0 {
			t.Fatalf("review lane Ambiguous = %+v, want empty", lane.Ambiguous)
		}
		if got := openVisitsAt(t, st, "site-b"); len(got) != len(visitsBefore) {
			t.Fatalf("site-b open visits changed: %+v → %+v", visitsBefore, got)
		}
	}
	check(t, w.b)
	replayAllOrders(t, w.events(w.a, "site-a"), w.events(w.b, "site-b"), check)
}

// TestC1cEveryResolveRefusalAuthorsNothing walks each pre-append refusal —
// and D-87's refusal from the store's choke point — and asserts the
// authoring site's stream is exactly as long afterwards as before.
func TestC1cEveryResolveRefusalAuthorsNothing(t *testing.T) {
	w := newC1ParkedWorld(t)
	stores := c1bStores(t, w)
	for _, ev := range w.events(w.b, "site-b") {
		if _, err := stores["site-c"].Apply(ev); err != nil {
			t.Fatal(err)
		}
	}
	// A correlated (never parked) arrival, for the NotParked arm.
	corrVID := w7SeedVehicleID(t, w.a, "site-a", "TRK-1")
	dest := "site-b"
	deadline := int64(30)
	corrTrip, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		VehicleID: corrVID, DriverPersonID: w.passenger.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w.syncBFromA()
	corr, err := RecordUnsolicitedArrival(w.b, "site-b", 1, corrVID, "site-a", []string{w.passenger.PersonID}, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if d := dispositionOf(t, w.b, corr.ArrivalID); d.Disposition != ArrivalCorrelated {
		t.Fatalf("PRECONDITION: control arrival = %q, want correlated (trip %s)", d.Disposition, corrTrip.TripID)
	}
	// trip1 closed by an explicit arrival AFTER the parking, for the AlreadyBound arm.
	explicit, err := RecordExpectedArrival(w.b, "site-b", 1, w.trip1.TripID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	c1AssertParkedOver(t, w.b, w.parked.ArrivalID, w.trip1.TripID, w.trip2.TripID)

	bogus := "no-such-trip"
	for _, tc := range []struct {
		name     string
		st       *store.Store
		site     string
		arrival  string
		trip     *string
		wantCode ResolveRefusalCode
		foreign  bool
	}{
		{"unknown arrival", w.b, "site-b", "site-b-arrival-0000000000000000", &w.trip2.TripID, ResolveArrivalUnknown, false},
		{"not parked (correlated)", w.b, "site-b", corr.ArrivalID, &corrTrip.TripID, ResolveNotParked, false},
		{"trip not a candidate", w.b, "site-b", w.parked.ArrivalID, &bogus, ResolveTripNotCandidate, false},
		{"trip since closed by an explicit arrival", w.b, "site-b", w.parked.ArrivalID, &w.trip1.TripID, ResolveTripAlreadyBound, false},
		{"another post's arrival (D-87)", stores["site-c"], "site-c", w.parked.ArrivalID, &w.trip2.TripID, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := c1cEventCount(t, tc.st, tc.site)
			_, err := ResolveParkedArrival(tc.st, tc.site, 1, tc.arrival, tc.trip, nil, 0, "default")
			if err == nil {
				t.Fatalf("resolution accepted, want a refusal")
			}
			if tc.foreign {
				var oe *store.OwnershipError
				if !errors.As(err, &oe) {
					t.Fatalf("err = %v, want *store.OwnershipError", err)
				}
			} else {
				var ref *ResolveRefusal
				if !errors.As(err, &ref) || ref.Code != tc.wantCode {
					t.Fatalf("err = %v, want ResolveRefusal code %d", err, tc.wantCode)
				}
				if tc.wantCode == ResolveTripAlreadyBound && ref.BoundTo != explicit.ArrivalID {
					t.Fatalf("refusal names binder %q, want the explicit arrival %s", ref.BoundTo, explicit.ArrivalID)
				}
			}
			if after := c1cEventCount(t, tc.st, tc.site); after != before {
				t.Fatalf("%s authored %d event(s) while refusing", tc.site, after-before)
			}
			c1AssertParkedOver(t, w.b, w.parked.ArrivalID, w.trip1.TripID, w.trip2.TripID)
		})
	}
	// AlreadyResolved: one resolution lands, the second is refused.
	if _, err := ResolveParkedArrival(w.b, "site-b", 1, w.parked.ArrivalID, &w.trip2.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	before := c1cEventCount(t, w.b, "site-b")
	_, err = ResolveParkedArrival(w.b, "site-b", 1, w.parked.ArrivalID, nil, nil, 0, "default")
	var ref *ResolveRefusal
	if !errors.As(err, &ref) || ref.Code != ResolveAlreadyResolved {
		t.Fatalf("second resolution: err = %v, want ResolveAlreadyResolved", err)
	}
	if after := c1cEventCount(t, w.b, "site-b"); after != before {
		t.Fatalf("site-b authored %d event(s) while refusing the second resolution", after-before)
	}
}

// TestC1cAResolutionCrossingAnExplicitArrivalForTheSameTripIsDeterministic
// is the race: the arrival's post resolves it to trip2 while a third post,
// which has not heard of that, records trip2's explicit arrival. Every
// delivery order must reach one projection; the first binder in canonical
// order holds the trip and trip1 is never touched.
func TestC1cAResolutionCrossingAnExplicitArrivalForTheSameTripIsDeterministic(t *testing.T) {
	w := newC1ParkedWorld(t)
	stores := c1bStores(t, w)
	if _, err := ResolveParkedArrival(w.b, "site-b", 1, w.parked.ArrivalID, &w.trip2.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	explicit, err := RecordExpectedArrival(stores["site-c"], "site-c", 1, w.trip2.TripID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	check := func(t *testing.T, st *store.Store) {
		s2 := stateOf(t, st, w.trip2.TripID)
		if s2.State != TripStateArrived || s2.ArrivalID == nil {
			t.Fatalf("trip2 %s = %q (arrival %v), want arrived", w.trip2.TripID, s2.State, s2.ArrivalID)
		}
		// site-b sorts before site-c: the resolution is the canonical first binder.
		if *s2.ArrivalID != w.parked.ArrivalID {
			t.Fatalf("trip2 bound to %s, want the resolved arrival %s (first in canonical order; the explicit one is %s)", *s2.ArrivalID, w.parked.ArrivalID, explicit.ArrivalID)
		}
		if d := dispositionOf(t, st, w.parked.ArrivalID); d.Disposition != ArrivalResolved {
			t.Fatalf("resolved arrival = %q, want %q", d.Disposition, ArrivalResolved)
		}
		if s1 := stateOf(t, st, w.trip1.TripID); s1.State != TripStateOpen {
			t.Fatalf("trip1 %s = %q, want open", w.trip1.TripID, s1.State)
		}
	}
	aEv, bEv, cEv := w.events(w.a, "site-a"), w.events(w.b, "site-b"), w.events(stores["site-c"], "site-c")
	replayAllOrders(t, aEv, c1bConcat(bEv, cEv), check)
	replayAllOrders(t, aEv, c1bConcat(cEv, bEv), check)
}
