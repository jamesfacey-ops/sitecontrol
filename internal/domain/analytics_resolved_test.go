// analytics_resolved_test.go — ⟦RES-1a, 2026-09-30⟧ /analytics' TRIPS
// ARRIVED tile counted only trips closed by an EXPLICIT arrival (the log's
// trip_arrival payloads with a trip_ref), while IN TRANSIT reads trip_state.
// A trip closed by an officer's resolution of a parked arrival — or by Pass
// 2 correlation of an unsolicited one — therefore left IN TRANSIT and never
// entered ARRIVED: dispatched 18 = 12 + 6 became 12 + 5 (lanes-r3/L11.md
// RES-1a). Both tiles now read trip_state, so the invariant
// dispatched = arrived + in transit holds through resolve, unref and
// re-resolve, and a from-zero rebuild agrees.
//
// RED on d6e5e0e (arrived stays 0 after the resolution / correlation),
// GREEN after. Preconditions asserted (rule 6): the binding arrival carries
// no trip_ref, which is exactly what the old count could not see.
package domain

import (
	"testing"
	"time"

	"sitecontrol/internal/store"
)

func anTiles(t *testing.T, st *store.Store) (dispatched, arrived, arrivedToday, inTransit int) {
	t.Helper()
	fig, err := BuildAnalytics(st, time.Now(), time.Local)
	if err != nil {
		t.Fatal(err)
	}
	if fig.TripsDispatched != fig.TripsArrived+fig.TripsInTransit {
		t.Errorf("tile invariant broken: dispatched %d != arrived %d + in transit %d", fig.TripsDispatched, fig.TripsArrived, fig.TripsInTransit)
	}
	return fig.TripsDispatched, fig.TripsArrived, fig.TripsArrivedToday, fig.TripsInTransit
}

func anAssertNoTripRef(t *testing.T, st *store.Store, arrivalID string) {
	t.Helper()
	a, err := oneRow[Arrival](st, "trip_arrival", store.Eq{Column: "arrival_id", Value: arrivalID})
	if err != nil || a == nil {
		t.Fatalf("PRECONDITION: arrival %s not readable: %v", arrivalID, err)
	}
	if a.TripRef != nil {
		t.Fatalf("PRECONDITION: arrival %s carries trip_ref %s — the case is an arrival WITHOUT one", arrivalID, *a.TripRef)
	}
}

func TestAnalyticsCountsATripClosedByAResolutionAsArrivedAndUnrefUncountsIt(t *testing.T) {
	w := newC1ParkedWorld(t)
	anAssertNoTripRef(t, w.b, w.parked.ArrivalID)
	if d, a, _, it := anTiles(t, w.b); d != 2 || a != 0 || it != 2 {
		t.Fatalf("PRECONDITION: parked: dispatched/arrived/in-transit = %d/%d/%d, want 2/0/2", d, a, it)
	}

	if _, err := ResolveParkedArrival(w.b, "site-b", 1, w.parked.ArrivalID, &w.trip1.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	afterResolve := func(t *testing.T, st *store.Store) {
		if s := stateOf(t, st, w.trip1.TripID); s.State != TripStateArrived {
			t.Fatalf("PRECONDITION: trip1 = %q after resolution, want arrived", s.State)
		}
		d, a, aToday, it := anTiles(t, st)
		if d != 2 || a != 1 || aToday != 1 || it != 1 {
			t.Fatalf("after resolution: dispatched/arrived/arrived-today/in-transit = %d/%d/%d/%d, want 2/1/1/1 — the resolved trip must count as arrived exactly like an explicitly closed one", d, a, aToday, it)
		}
	}
	afterResolve(t, w.b)
	replayAllOrders(t, w.events(w.a, "site-a"), w.events(w.b, "site-b"), afterResolve)

	// Unref withdraws the resolution: the trip re-opens and leaves ARRIVED.
	if _, err := RetractArrivalRef(w.b, "site-b", 1, w.parked.ArrivalID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	afterUnref := func(t *testing.T, st *store.Store) {
		if s := stateOf(t, st, w.trip1.TripID); s.State != TripStateOpen {
			t.Fatalf("PRECONDITION: trip1 = %q after unref, want open again", s.State)
		}
		if d, a, _, it := anTiles(t, st); d != 2 || a != 0 || it != 2 {
			t.Fatalf("after unref: dispatched/arrived/in-transit = %d/%d/%d, want 2/0/2", d, a, it)
		}
	}
	afterUnref(t, w.b)
	replayAllOrders(t, w.events(w.a, "site-a"), w.events(w.b, "site-b"), afterUnref)

	// Re-resolve, to the other trip this time: counted again.
	if _, err := ResolveParkedArrival(w.b, "site-b", 1, w.parked.ArrivalID, &w.trip2.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if d, a, _, it := anTiles(t, w.b); d != 2 || a != 1 || it != 1 {
		t.Fatalf("after re-resolution: dispatched/arrived/in-transit = %d/%d/%d, want 2/1/1", d, a, it)
	}
}

func TestAnalyticsCountsATripClosedByACorrelatedUnsolicitedArrivalAsArrived(t *testing.T) {
	w := newD98World(t)
	w.syncBFromA()
	dest, deadline := "site-b", int64(120)
	vehicleID := w7SeedVehicleID(t, w.a, "site-a", "BUS 7")
	trip, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		VehicleID: vehicleID, DriverPersonID: w.driver.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w.syncBFromA()
	if d, a, _, it := anTiles(t, w.b); d != 1 || a != 0 || it != 1 {
		t.Fatalf("PRECONDITION: on the road: dispatched/arrived/in-transit = %d/%d/%d, want 1/0/1", d, a, it)
	}
	arrival, err := RecordUnsolicitedArrival(w.b, "site-b", 1, vehicleID, "site-a",
		[]string{w.driver.PersonID}, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	anAssertNoTripRef(t, w.b, arrival.ArrivalID)
	check := func(t *testing.T, st *store.Store) {
		if d := dispositionOf(t, st, arrival.ArrivalID); d.Disposition != ArrivalCorrelated || d.TripID == nil || *d.TripID != trip.TripID {
			t.Fatalf("PRECONDITION: arrival = %q → %v, want correlated to %s", d.Disposition, d.TripID, trip.TripID)
		}
		d, a, aToday, it := anTiles(t, st)
		if d != 1 || a != 1 || aToday != 1 || it != 0 {
			t.Fatalf("after correlation: dispatched/arrived/arrived-today/in-transit = %d/%d/%d/%d, want 1/1/1/0", d, a, aToday, it)
		}
	}
	check(t, w.b)
	replayAllOrders(t, r3Events(t, w.a, "site-a"), r3Events(t, w.b, "site-b"), check)
}
