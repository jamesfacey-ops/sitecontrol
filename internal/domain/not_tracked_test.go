// not_tracked_test.go — the away-view's third category (operator ruling,
// 2026-08-20): people who left ON a vehicle and did NOT arrive with it,
// whereabouts not recorded. A display category over existing data — trip
// membership + the arrival's omitted visit set — with the same
// recorded-return arithmetic (sightedSince) as the pad list.
package domain

import "testing"

func TestNotTrackedListsEnRouteDropOffAndOnlyThat(t *testing.T) {
	w := newC6World(t)
	casey, err := CreatePerson(w.a, "site-a", 1, "Casey Crew", &w.company.CompanyID, &w.trade.TradeID, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	dale, err := CreatePerson(w.a, "site-a", 1, "Dale Dayworker", &w.company.CompanyID, &w.trade.TradeID, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// Dale: on site then a PLAIN check-out — untracked in the boring way,
	// must never appear (no trip membership).
	walkIn, err := DispatchLocalCheckIn(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "gate walk-in", DriverPersonID: dale.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "gate walk-in"), DriverPersonID: dale.PersonID,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordExpectedArrival(w.a, "site-a", 1, walkIn.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	for _, v := range openVisitsAt(t, w.a, "site-a") {
		if err := CheckOutVisit(w.a, "site-a", 1, v.VisitID, nil, 0, "default"); err != nil {
			t.Fatal(err)
		}
	}

	// A pad run with a stay-behind: category 1 (OutAtUncontrolled), must
	// NOT double up as not-tracked — the record DOES say where they are.
	pad := "conneco"
	run, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "PAD BUS", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "PAD BUS"), DriverPersonID: w.driver.PersonID,
		OccupantPersonIDs:       []string{w.passenger.PersonID},
		DestinationUncontrolled: &pad,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordExpectedArrivalExcluding(w.a, "site-a", 1, run.TripID,
		[]string{w.passenger.PersonID}, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}

	// The case itself: BUS 7 to site-b drops Casey somewhere en route; the
	// site-b officer marks them not aboard at arrival.
	dest, deadline := "site-b", int64(60)
	trip, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "BUS 7", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "BUS 7"), DriverPersonID: w.driver.PersonID,
		OccupantPersonIDs: []string{casey.PersonID},
		DestinationSite:   &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w.syncBFromA()
	if _, err := RecordExpectedArrivalExcluding(w.b, "site-b", 1, trip.TripID,
		[]string{casey.PersonID}, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	w2 := w.events(w.b, "site-b")
	for _, ev := range w2 {
		if _, err := w.a.Apply(ev); err != nil {
			t.Fatal(err)
		}
	}

	// An OPEN controlled trip must not appear (the deadline machinery owns
	// it until arrival). The occupant must be someone with NO visit of any
	// kind anywhere — reusing w.driver (already sighted via the PAD BUS
	// return and the BUS 7 arrival above) let sightedSince's rules 2/3
	// exclude this occupant on their own, so dropping NotTracked's
	// ARRIVED-only state filter stayed green (round-2 audit F3/N22): the
	// fixture could not produce the failure it named.
	toby, err := CreatePerson(w.a, "site-a", 1, "Toby Trucker", &w.company.CompanyID, &w.trade.TradeID, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRUCK 9", DriverPersonID: toby.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "TRUCK 9"), DriverPersonID: toby.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &deadline,
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}

	rows, err := NotTracked(w.a)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("NotTracked = %+v, want exactly one trip (BUS 7)", rows)
	}
	r := rows[0]
	if r.Vehicle != "BUS 7" || r.Destination != "site-b" ||
		len(r.Persons) != 1 || r.Persons[0].PersonID != casey.PersonID {
		t.Fatalf("NotTracked row = %+v, want Casey on BUS 7 to site-b", r)
	}

	// Casey later reaches a gate — the ordinary check-in at the origin —
	// and the gap closes (sightedSince rule 2).
	van, err := DispatchLocalCheckIn(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "VAN 2", DriverPersonID: casey.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "VAN 2"), DriverPersonID: casey.PersonID,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordExpectedArrival(w.a, "site-a", 1, van.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	rows, err = NotTracked(w.a)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("NotTracked after Casey reached a gate = %+v, want empty", rows)
	}
}
