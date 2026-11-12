// r3_nottracked_binding_arrival_test.go — ⟦R3-07, 2026-09-30⟧ a trip closed
// by an UNSOLICITED arrival — Pass 2 correlation, or an officer's resolution
// of a parked row ("This was trip X", arrival_resolve.go) — has its crew's
// visits under trip_id NULL, so NotTracked (keyed on VisitsByTrip) filed the
// very people that arrival checked in as "not aboard at arrival —
// whereabouts not recorded" while the same sheet listed them ON SITE
// (lanes-r3/L08.md R3-07, re-confirmed on a correlated case R3-12 b). The
// fix counts the binding arrival's own visit rows (by its envelope key) as
// aboard, beside the held-out rows H-8 added.
//
// Each test asserts the arrangement it rests on (BUILDER_BRIEF_RULES rule
// 6): the binding arrival's visits really do carry trip_id NULL and
// trip_state really is bound to that arrival — otherwise the fix could pass
// by VisitsByTrip alone. RED on d6e5e0e, GREEN after. Every check runs on
// the authoring store and under replayAllOrders (same answer from any
// delivery order and on a from-zero rebuild).
package domain

import (
	"testing"

	"sitecontrol/internal/store"
)

// r3Events is c6World.events for the d98World fixture (which has none).
func r3Events(t *testing.T, st *store.Store, siteID string) []store.Envelope {
	t.Helper()
	evs, err := st.Events(siteID, 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

// r3NotTrackedIDs flattens NotTracked to the raw person ids listed under
// tripID (nil when the trip has no row).
func r3NotTrackedIDs(t *testing.T, st *store.Store, tripID string) []string {
	t.Helper()
	rows, err := NotTracked(st)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range rows {
		if r.TripID != tripID {
			continue
		}
		for _, p := range r.Persons {
			ids = append(ids, p.PersonID)
		}
	}
	return ids
}

// r3AssertBoundByUnsolicited is the precondition both cases rest on: the
// trip is arrived, bound to arrivalID, that arrival has no trip_ref, and
// every visit it minted (its envelope key) carries trip_id NULL.
func r3AssertBoundByUnsolicited(t *testing.T, st *store.Store, tripID, arrivalID string, wantEntered []string) {
	t.Helper()
	s := stateOf(t, st, tripID)
	if s.State != TripStateArrived || s.ArrivalID == nil || *s.ArrivalID != arrivalID {
		t.Fatalf("PRECONDITION: trip %s = %q (arrival %v), want arrived by %s", tripID, s.State, s.ArrivalID, arrivalID)
	}
	a, err := oneRow[Arrival](st, "trip_arrival", store.Eq{Column: "arrival_id", Value: arrivalID})
	if err != nil || a == nil {
		t.Fatalf("PRECONDITION: arrival %s not readable: %v", arrivalID, err)
	}
	if a.TripRef != nil {
		t.Fatalf("PRECONDITION: arrival %s carries trip_ref %s — an unsolicited arrival has none", arrivalID, *a.TripRef)
	}
	visits, err := dumpSectionWhere[Visit](st, "visit",
		store.Eq{Column: "site_id", Value: a.SiteID},
		store.Eq{Column: "node_epoch", Value: a.NodeEpoch},
		store.Eq{Column: "seq", Value: a.Seq})
	if err != nil {
		t.Fatal(err)
	}
	if len(visits) != len(wantEntered) {
		t.Fatalf("PRECONDITION: arrival %s minted %d visits, want %d (%v): %+v", arrivalID, len(visits), len(wantEntered), wantEntered, visits)
	}
	for _, v := range visits {
		if v.TripID != nil {
			t.Fatalf("PRECONDITION: visit %s carries trip_id %s — an unsolicited arrival's visits carry NULL, which is the whole case", v.VisitID, *v.TripID)
		}
		if v.IsOpen != 1 {
			t.Fatalf("PRECONDITION: visit %s is not open — the person must be ON SITE for the contradiction to exist", v.VisitID)
		}
	}
	if byTrip, err := VisitsByTrip(st, tripID); err != nil || len(byTrip) != 0 {
		t.Fatalf("PRECONDITION: VisitsByTrip(%s) = %+v (err %v), want none — otherwise the old aboard source alone would pass", tripID, byTrip, err)
	}
}

func TestNotTrackedNeverListsWhomAResolvedArrivalCheckedIn(t *testing.T) {
	// T-092's fixture: two open same-key trips from site-a; the unsolicited
	// arrival at site-b carries trip1's DRIVER and parks over [trip1, trip2].
	// The officer resolves it to trip1 — the trip the driver was actually on
	// (R3-07's own case: "This was trip A" for Torworth's trip).
	w := newC1ParkedWorld(t)
	if _, err := ResolveParkedArrival(w.b, "site-b", 1, w.parked.ArrivalID, &w.trip1.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	check := func(t *testing.T, st *store.Store) {
		r3AssertBoundByUnsolicited(t, st, w.trip1.TripID, w.parked.ArrivalID, []string{w.driver.PersonID})
		if ids := r3NotTrackedIDs(t, st, w.trip1.TripID); len(ids) != 0 {
			t.Fatalf("NotTracked lists %v under trip1 — the driver is the person the resolved arrival checked in, ON SITE on this very sheet", ids)
		}
		if s := stateOf(t, st, w.trip2.TripID); s.State != TripStateOpen {
			t.Fatalf("trip2 = %q, want still open (an open trip is never a NotTracked row)", s.State)
		}
		if ids := r3NotTrackedIDs(t, st, w.trip2.TripID); len(ids) != 0 {
			t.Fatalf("NotTracked lists %v under the still-open trip2", ids)
		}
	}
	check(t, w.b)
	replayAllOrders(t, w.events(w.a, "site-a"), w.events(w.b, "site-b"), check)
}

func TestNotTrackedNeverListsWhomACorrelatedArrivalCheckedInButStillListsWhomItDidNot(t *testing.T) {
	// BUS 7 site-a → site-b with the driver, Adams and Evans. Site-b records
	// an UNSOLICITED arrival on the same vehicle from site-a naming the
	// driver and Adams only — one open candidate, so Pass 2 correlates it and
	// closes the trip. Evans, whom that arrival did not enter, has no visit
	// anywhere: the conservative listing for him is unchanged.
	w := newD98World(t)
	w.syncBFromA()
	adams, evans := w.ids(0)[0], w.ids(4)[0]
	dest, deadline := "site-b", int64(120)
	vehicleID := w7SeedVehicleID(t, w.a, "site-a", "BUS 7")
	trip, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		VehicleID: vehicleID, DriverPersonID: w.driver.PersonID,
		OccupantPersonIDs: []string{adams, evans},
		DestinationSite:   &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w.syncBFromA()
	arrival, err := RecordUnsolicitedArrival(w.b, "site-b", 1, vehicleID, "site-a",
		[]string{w.driver.PersonID, adams}, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if d := dispositionOf(t, w.b, arrival.ArrivalID); d.Disposition != ArrivalCorrelated || d.TripID == nil || *d.TripID != trip.TripID {
		t.Fatalf("PRECONDITION: arrival = %q → %v, want correlated to %s", d.Disposition, d.TripID, trip.TripID)
	}
	check := func(t *testing.T, st *store.Store) {
		r3AssertBoundByUnsolicited(t, st, trip.TripID, arrival.ArrivalID, []string{w.driver.PersonID, adams})
		ids := r3NotTrackedIDs(t, st, trip.TripID)
		if len(ids) != 1 || ids[0] != evans {
			t.Fatalf("NotTracked under BUS 7 = %v, want exactly Evans — the driver and Adams are the people the correlated arrival checked in (on site here); Evans it did not enter stays listed", ids)
		}
	}
	check(t, w.b)
	replayAllOrders(t, r3Events(t, w.a, "site-a"), r3Events(t, w.b, "site-b"), check)
}

func TestNotTrackedStillOmitsAPersonACorrelatedArrivalHeldOutForOrientation(t *testing.T) {
	// The H-8 rule through the correlated door: site-b administers an
	// orientation; the driver holds it, Diaz does not. The correlated
	// unsolicited arrival enters the driver and holds Diaz out (a
	// trip_arrival_not_entered row on THIS arrival, which has no trip_ref).
	// Diaz was aboard and is on no muster sheet (D-91 / R-11); the driver is
	// on site. Neither is NOT TRACKED.
	w := newD98World(t)
	w.syncBFromA()
	diaz := w.ids(3)[0]
	if _, err := CompleteOrientation(w.b, "site-b", 1, w.driver.PersonID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	dest, deadline := "site-b", int64(120)
	vehicleID := w7SeedVehicleID(t, w.a, "site-a", "BUS 8")
	trip, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		VehicleID: vehicleID, DriverPersonID: w.driver.PersonID,
		OccupantPersonIDs: []string{diaz},
		DestinationSite:   &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w.syncBFromA()
	arrival, err := RecordUnsolicitedArrivalOriented(w.b, "site-b", 1, vehicleID, "site-a",
		[]string{w.driver.PersonID, diaz}, nil, 0, "default", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(arrival.NotEntered) != 1 || arrival.NotEntered[0] != diaz {
		t.Fatalf("PRECONDITION: arrival.NotEntered = %v, want exactly Diaz", arrival.NotEntered)
	}
	check := func(t *testing.T, st *store.Store) {
		r3AssertBoundByUnsolicited(t, st, trip.TripID, arrival.ArrivalID, []string{w.driver.PersonID})
		if rows := notEnteredRowsFor(t, st, diaz); len(rows) != 1 || rows[0].ArrivalID != arrival.ArrivalID {
			t.Fatalf("PRECONDITION: Diaz's not_entered rows = %+v, want one on arrival %s", rows, arrival.ArrivalID)
		}
		if ids := r3NotTrackedIDs(t, st, trip.TripID); len(ids) != 0 {
			t.Fatalf("NotTracked under BUS 8 = %v, want nobody (driver entered, Diaz held out at this gate)", ids)
		}
	}
	check(t, w.b)
	replayAllOrders(t, r3Events(t, w.a, "site-a"), r3Events(t, w.b, "site-b"), check)
}
