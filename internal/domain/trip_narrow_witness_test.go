// trip_narrow_witness_test.go — witness for the "trip read path is
// wide-vs-narrow" claim: does the scoping OpenExpectations/openTripStates
// DOCUMENT (destination-site + open-state) actually have teeth, or is it
// merely believed?
//
// The only existing coverage
// (TestDegenerateLocalTripRunsTheSameMachineryAndSkipsTheBoard) proves a
// DEGENERATE trip (no destination at all) is invisible everywhere — it
// never poses a trip that HAS a destination but the WRONG one, or a trip
// that is destined here but is no longer OPEN. Nobody had produced the
// leak this test attempts.
//
// Two independent predicates are exercised, each with its own positive
// control so the test cannot pass by construction:
//   - the destination-site predicate (OpenExpectations' own `continue` on
//     *c.Trip.DestinationSite != siteID`)
//   - the open-state predicate (openTripStates' `state = 'open'` filter)
package domain

import "testing"

func TestOpenExpectationsExcludesForeignSiteAndNonOpenTrips(t *testing.T) {
	w := newC6World(t)
	dst := "site-b"
	ddl := int64(45)

	// Control: an open trip destined to site-b — must appear on site-b's board.
	tripOpen, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "OPEN 1"), DriverPersonID: w.driver.PersonID,
		DestinationSite: &dst, DeadlineMinutes: &ddl,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	// The foreign-site attempt: open, but destined to site-c, not site-b.
	// Must NOT appear on site-b's board (the site predicate); MUST appear
	// on site-c's own board, so its absence at site-b is a real exclusion
	// and not just "this trip is invisible everywhere."
	dstForeign := "site-c"
	tripForeign, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "FOREIGN 1"), DriverPersonID: w.driver.PersonID,
		DestinationSite: &dstForeign, DeadlineMinutes: &ddl,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	// The wrong-state attempt: destined to site-b like the control, but
	// ARRIVED. Must NOT appear on site-b's OPEN board (the state predicate).
	tripArrived, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "ARRIVED 1"), DriverPersonID: w.driver.PersonID,
		DestinationSite: &dst, DeadlineMinutes: &ddl,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordExpectedArrival(w.a, "site-a", 1, tripArrived.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if s := stateOf(t, w.a, tripArrived.TripID); s.State != TripStateArrived {
		t.Fatalf("setup: tripArrived state = %s, want arrived", s.State)
	}

	has := func(exps []Expectation, tripID string) bool {
		for _, e := range exps {
			if e.Trip.TripID == tripID {
				return true
			}
		}
		return false
	}

	// --- the site predicate, at OpenExpectations ---
	atB, err := OpenExpectations(w.a, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if !has(atB, tripOpen.TripID) {
		t.Fatalf("site-b's board is missing its own open trip %s — the control is broken, not the leak", tripOpen.TripID)
	}
	if has(atB, tripForeign.TripID) {
		t.Fatalf("LEAK: site-b's board shows %s, a trip destined to site-c — the destination-site predicate has no teeth", tripForeign.TripID)
	}
	if has(atB, tripArrived.TripID) {
		t.Fatalf("LEAK: site-b's board shows %s, an ARRIVED trip — the open-state predicate has no teeth", tripArrived.TripID)
	}

	atC, err := OpenExpectations(w.a, "site-c")
	if err != nil {
		t.Fatal(err)
	}
	if !has(atC, tripForeign.TripID) {
		t.Fatalf("site-c's board is missing its own open trip %s — positive control for the site predicate is broken", tripForeign.TripID)
	}

	// --- the open-state predicate, isolated at openTripStates itself ---
	open, err := openTripStates(w.a)
	if err != nil {
		t.Fatal(err)
	}
	var sawArrived, sawOpen bool
	for _, s := range open {
		if s.TripID == tripArrived.TripID {
			sawArrived = true
		}
		if s.TripID == tripOpen.TripID {
			sawOpen = true
		}
	}
	if sawArrived {
		t.Fatalf("LEAK: openTripStates returned the ARRIVED trip %s — the state='open' filter has no teeth", tripArrived.TripID)
	}
	if !sawOpen {
		t.Fatalf("openTripStates is missing the open control trip %s", tripOpen.TripID)
	}
}
