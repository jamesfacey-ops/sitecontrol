// truck_crosspost_test.go — James, 2026-10-03: "the truck number and driver
// are not the same on the overdue truck between the ice road and ice pad."
//
// The REPLICATION half (web/truck_crosspost_test.go renders the surfaces off
// one shared store). Here the origin and the destination are SEPARATE stores
// and every event reaches the destination only by being shipped across, so
// what is asserted is convergence: after each step — dispatch, renumber,
// same-as join, driver correction — the destination's expectation (the alert
// strip's, inbound board's and gate row's input) and the origin's in-transit
// row (the muster's) name the same truck and the same driver, and both posts
// compute both readers identically.
package domain

import (
	"testing"

	"sitecontrol/internal/store"
)

// truckShip applies every site-a event dst has not yet seen, in order — the
// contiguous delivery the replication engine performs.
func truckShip(t *testing.T, src, dst *store.Store) {
	t.Helper()
	wm, err := dst.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	envs, err := src.Events("site-a", 1, wm+1, 10000)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range envs {
		if _, err := dst.Apply(e); err != nil {
			t.Fatalf("applying %v at the destination: %v", e.Key(), err)
		}
	}
}

type truckReading struct{ vehicle, driver string }

func truckReadAt(t *testing.T, st *store.Store, tripID string) (exp, road truckReading) {
	t.Helper()
	exps, err := OpenExpectations(st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range exps {
		if e.Trip.TripID == tripID {
			exp = truckReading{e.Vehicle, e.DriverName}
		}
	}
	rows, err := InTransitFrom(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.TripID == tripID {
			road = truckReading{r.Vehicle, r.Driver}
		}
	}
	if exp.vehicle == "" || road.vehicle == "" {
		t.Fatalf("precondition: the open run is missing from a reader (expectation %+v, in-transit %+v)", exp, road)
	}
	return exp, road
}

// assertConverged: at BOTH stores, the destination's expectation and the
// origin's in-transit row read want — so Ice Pad's alert and Ice Road's sheet
// cannot disagree, and neither depends on which post did the reading.
func assertConverged(t *testing.T, step string, origin, dest *store.Store, tripID string, want truckReading) {
	t.Helper()
	for _, s := range []struct {
		name string
		st   *store.Store
	}{{"Ice Road (origin)", origin}, {"Ice Pad (destination)", dest}} {
		exp, road := truckReadAt(t, s.st, tripID)
		if exp != want {
			t.Errorf("%s, at %s: the inbound expectation (alert strip / board / gate row) reads %+v, want %+v", step, s.name, exp, want)
		}
		if road != want {
			t.Errorf("%s, at %s: the in-transit row (muster) reads %+v, want %+v", step, s.name, road, want)
		}
	}
}

func TestOverdueTruckConvergesAtBothPostsThroughRenumberJoinAndDriverCorrection(t *testing.T) {
	origin := newDomainTestStore(t)
	dest := newDomainTestStore(t)

	person := func(name string) Person {
		p, err := CreatePerson(origin, "site-a", 1, name, nil, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	driver, rider := person("Torstad, Owen"), person("Ravton, Val")
	num := func(s string) *string { return &s }
	truck, err := CreateVehicle(origin, "site-a", 1, num("TRK-72"), nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	destSite, x := "site-b", int64(1)
	trip, err := Dispatch(origin, "site-a", 1, DispatchInput{
		VehicleID: truck.VehicleID, DriverPersonID: driver.PersonID,
		OccupantPersonIDs: []string{rider.PersonID},
		DestinationSite:   &destSite, DeadlineMinutes: &x,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	truckShip(t, origin, dest)
	assertConverged(t, "after dispatch", origin, dest, trip.TripID, truckReading{"TRK-72", "Torstad, Owen"})

	// The exact live repro: the record renumbered while the run is out.
	if _, err := AmendVehicle(origin, "site-a", 1, VehicleAmendment{VehicleID: truck.VehicleID, Number: num("TRK-92")}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	truckShip(t, origin, dest)
	assertConverged(t, "after the renumber", origin, dest, trip.TripID, truckReading{"TRK-92", "Torstad, Owen"})

	// A same-as join onto another record: one truck, named by the survivor.
	other, err := CreateVehicle(origin, "site-a", 1, num("TRK-77"), nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AmendVehicle(origin, "site-a", 1, VehicleAmendment{VehicleID: truck.VehicleID, SameAs: &other.VehicleID}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	truckShip(t, origin, dest)
	assertConverged(t, "after the same-as join", origin, dest, trip.TripID, truckReading{"TRK-77", "Torstad, Owen"})

	// A driver correction on the open run: the amended driver, everywhere.
	if _, err := AmendTrip(origin, "site-a", 1, TripAmendment{TripID: trip.TripID, DriverPersonID: &rider.PersonID}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	truckShip(t, origin, dest)
	assertConverged(t, "after the driver correction", origin, dest, trip.TripID, truckReading{"TRK-77", "Ravton, Val"})
}
