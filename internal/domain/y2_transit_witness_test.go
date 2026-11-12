// y2_transit_witness_test.go — witness phase, worktree y2-transit.
//
// Kills census survivor P6 (in_transit.go:307, `display = *v.Role` planted to
// `display = ""`): D-11 requires a numberless truck to be shown by its ROLE,
// never a blank or a stand-in, and S120 already covers this rule on the
// ALERT row — this is the SAME rule's second, previously-unwitnessed copy on
// the in-transit MUSTER row, the page whose whole job (D-17) is "can we
// reach this vehicle and stop it".
//
// Also works the file's three remaining unreached blocks from the W8-6
// census (blocks19_unc.tsv, all class LOGIC — none are ERR-ARM, so there is
// no refusal sentence in this file to render):
//
//   - line 308-310 (`display = ""`, both Number and Role nil): a state
//     CreateVehicle and AmendVehicle both refuse to ever create (vehicle.go
//     arm 1, vehicle_amend.go arm 6 — "a record identifying nothing").
//     Unreachable through the guarded domain API. But the guard lives only
//     at the AUTHORING EDGE — applyVehicleCreated (the fold) inserts
//     whatever a vehicle_created payload carries, unvalidated — so a
//     foreign or legacy event reaches it on replication. Witnessed by
//     authoring that event directly, the same way CreateVehicle itself
//     would.
//   - line 312-330 (`identity = display`, the `identity == ""` else arm):
//     the file's own comment names the real case — "a run authored before
//     the entity existed, or one whose vehicle record has not replicated
//     here". Witnessed the same way: a trip_dispatched event with no
//     vehicle_id, exactly the pre-W7 shape.
package domain

import (
	"encoding/json"
	"testing"
)

// TestWitnessY2P6VehicleWithNoNumberIsShownByItsRole kills P6.
//
// D-11, in_transit.go:302-307: a vehicle record with a role and no number
// must be shown BY ITS ROLE on the in-transit muster row — never a blank,
// never a stand-in. Under the plant (`display = ""`) a truck carrying
// people on the ice road renders with NO identifier at all on the one page
// whose job is "can we reach this vehicle and stop it" (D-17).
func TestWitnessY2P6VehicleWithNoNumberIsShownByItsRole(t *testing.T) {
	w := newC6World(t)
	v, err := CreateVehicle(w.a, "site-a", 1, nil, strPtr("Water Truck"), 0, "default")
	if err != nil {
		t.Fatalf("fixture: create numberless vehicle: %v", err)
	}
	dest, deadline := "site-b", int64(90)
	trip, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		VehicleID:         v.VehicleID,
		DriverPersonID:    w.driver.PersonID,
		OccupantPersonIDs: []string{w.passenger.PersonID},
		DestinationSite:   &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatalf("fixture: dispatch: %v", err)
	}

	rows, err := InTransitFrom(w.a, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].TripID != trip.TripID {
		t.Fatalf("precondition: expected exactly one in-transit row for %s, got %+v — this witness would pass vacuously otherwise", trip.TripID, rows)
	}
	if rows[0].Vehicle != "Water Truck" {
		t.Errorf("P6: in-transit row for a numberless truck reads Vehicle=%q, want %q — D-11: a truck with no number is shown by its ROLE, never a blank or a stand-in; the officer reading the in-transit muster cannot tell which vehicle this is", rows[0].Vehicle, "Water Truck")
	}
}

// TestWitnessY2VehicleIdentifyingNothingRendersEmptyVehicleField exercises
// in_transit.go:308-310 (`display = ""`): a composed *Vehicle whose Number
// AND Role are both nil.
//
// CreateVehicle and AmendVehicle both refuse to ever put a live vehicle
// record in that state ("a record identifying nothing") — so this is
// UNREACHABLE through the domain package's own guarded API, and this test
// says so rather than implying otherwise. It IS reachable on the wire: the
// guard is enforced only at the authoring edge, and applyVehicleCreated (the
// fold every replicated event runs through) performs no such check — it
// inserts whatever the payload says. A peer running different code, or an
// event that predates this guard, replicates exactly like any other
// vehicle_created event. This authors that event directly, exactly the path
// CreateVehicle itself would take, to prove the render path stays SAFE
// (empty string, not a crash, not a stand-in) rather than assuming the edge
// guard is the only thing between this branch and a nil-pointer deref.
func TestWitnessY2VehicleIdentifyingNothingRendersEmptyVehicleField(t *testing.T) {
	w := newC6World(t)
	vehicleID := newID("site-a", "vehicle")
	payload, err := json.Marshal(VehicleCreated{Type: evtVehicleCreated, VehicleID: vehicleID})
	if err != nil {
		t.Fatalf("fixture: marshal vehicle_created: %v", err)
	}
	if _, err := w.a.Author("site-a", 1, payload, 0, "default"); err != nil {
		t.Fatalf("fixture: author vehicle_created with neither number nor role: %v", err)
	}

	dest, deadline := "site-b", int64(90)
	trip, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		VehicleID:         vehicleID,
		DriverPersonID:    w.driver.PersonID,
		OccupantPersonIDs: []string{w.passenger.PersonID},
		DestinationSite:   &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatalf("fixture: dispatch: %v", err)
	}

	rows, err := InTransitFrom(w.a, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].TripID != trip.TripID {
		t.Fatalf("precondition: expected exactly one in-transit row for %s, got %+v — this witness would pass vacuously otherwise", trip.TripID, rows)
	}
	if rows[0].Vehicle != "" {
		t.Errorf("in-transit row for a vehicle record with neither a number nor a role reads Vehicle=%q, want empty — a record identifying nothing must never render a stand-in string", rows[0].Vehicle)
	}
}

// TestWitnessY2LegacyTripWithNoVehicleIDFallsBackToTheEnteredSnapshot
// exercises in_transit.go:312-330 (`identity = display`, the `identity ==
// ""` else arm).
//
// dispatchTrip refuses a NEW dispatch with no vehicle id (tripauthor.go: "a
// caller with a resolver has no id YET; one that supplies neither is still
// refused HERE"), so this arm cannot be produced by authoring a fresh trip
// through the guarded edge. But it is not dead code: in_transit.go's own
// comment on this branch names the real case — "a run authored before the
// entity existed, or one whose vehicle record has not replicated here" —
// i.e. a trip_dispatched event from before W7 item 9 introduced vehicle_id,
// which carried only the typed vehicle_entered snapshot and no id at all.
// Such an event replicates today exactly as it always did: nothing in the
// fold requires vehicle_id to be non-empty, only the authoring edge for a
// NEW dispatch does. This authors that pre-migration shape directly to
// prove the read path still falls back to the recorded string rather than
// losing the truck.
func TestWitnessY2LegacyTripWithNoVehicleIDFallsBackToTheEnteredSnapshot(t *testing.T) {
	w := newC6World(t)
	tripID := newID("site-a", "trip")
	dest, deadline := "site-b", int64(90)
	payload, err := json.Marshal(TripDispatched{
		Type:              "trip_dispatched",
		TripID:            tripID,
		VehicleEntered:    strPtr("TRUCK 12"),
		DriverPersonID:    w.driver.PersonID,
		OccupantPersonIDs: []string{w.passenger.PersonID},
		DestinationSite:   &dest, DeadlineMinutes: &deadline,
	})
	if err != nil {
		t.Fatalf("fixture: marshal legacy trip_dispatched: %v", err)
	}
	if _, err := w.a.Author("site-a", 1, payload, 0, "default"); err != nil {
		t.Fatalf("fixture: author legacy trip_dispatched with no vehicle_id: %v", err)
	}

	rows, err := InTransitFrom(w.a, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].TripID != tripID {
		t.Fatalf("precondition: expected exactly one in-transit row for %s, got %+v — this witness would pass vacuously otherwise", tripID, rows)
	}
	if rows[0].Vehicle != "TRUCK 12" || rows[0].VehicleIdentity != "TRUCK 12" {
		t.Errorf("legacy trip with no vehicle_id: row Vehicle=%q VehicleIdentity=%q, want both %q (the entered snapshot — the strongest identity available with no vehicle record to resolve)", rows[0].Vehicle, rows[0].VehicleIdentity, "TRUCK 12")
	}
}
