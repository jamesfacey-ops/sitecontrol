// s120_vehicle_absence_test.go — S120: DR-W7-15 / [[D-11]] on the alert strip.
//
// THE RULE. `trip.vehicle_entered` is nullable since W7 item 8, because a
// role-only vehicle honestly has no number. `vehicleSnapshotText` maps that
// absence to the EMPTY STRING and never to a stand-in wording: the empty
// string IS the absence (DR-W7-15), and a sentinel here would be a magic
// string in the vehicle field, which [[D-11]] rules out in terms.
//
// WHY THIS FILE EXISTS. `deadline_test.go:93`'s own routed note says it
// exactly, about a repair it refused to make:
//
//	"⛔ but `Alert.Vehicle` silently becomes "" on the overdue-alert surface
//	 (`scan.go:227` via `vehicleSnapshotText`, which maps a nil snapshot to
//	 ""), and [MEASURED] NO test in this package asserts `Alert.Vehicle`, so
//	 it would go green unseen."
//
// §65 B3 then added the NON-nil assertion (`a.Vehicle != "TRUCK 12"`). The
// NIL side stayed unasserted, and a mutation audit confirmed it: replacing
// the empty string with "(no number)" left `./internal/deadline` (52 tests)
// and `./internal/web` (429 tests) byte-identical, with a runtime probe
// proving the branch was never once executed. [MEASURED 2026-09-04]
//
// ⟦James, 2026-10-03⟧ AMENDED, NOT DELETED. The alert row no longer reads
// the snapshot at all: DR-W7-18 rules the alert strip LIVE, and reading the
// frozen snapshot here is what let Ice Pad's alert name a different truck from
// Ice Road's sheet for the same overdue run. The snapshot itself is unchanged
// (still NULL for a role-only truck — the precondition below still pins it),
// and the absence rule's real content — no SENTINEL in the vehicle field —
// is still asserted; what changed is that the alert now shows the record's
// recorded role, as the in-transit block always has.
//
// ⚠ THIS FIXTURE BUILDS ITS OWN STORE ON PURPOSE. `deadline_test.go:93`
// records that seeding a vehicle record ahead of the shared dispatch moves
// its envelope key and reddens three W9c tests that pin keys by hand. Nothing
// here touches that fixture.
package deadline

import (
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// TestW7NumberlessVehicleRendersAsTheEmptyStringNeverASentinel drives a
// ROLE-ONLY vehicle — the ordinary shape for a truck with no plate — all the
// way to an alert row, and pins both sides of the absence rule.
func TestW7NumberlessVehicleRendersAsTheEmptyStringNeverASentinel(t *testing.T) {
	st := newSite(t)

	// A role-only record: CreateVehicle accepts a nil number when a role is
	// given (vehicle.go: "a vehicle record needs a number or a role").
	role := "water truck"
	roleOnly, err := domain.CreateVehicle(st, vehicleHome, 1, nil, &role, 0, "default")
	if err != nil {
		t.Fatalf("fixture: creating a role-only vehicle: %v", err)
	}
	if roleOnly.Number != nil {
		t.Fatalf("fixture: the role-only vehicle has number %q — this fixture's whole premise "+
			"is a record with NO number", *roleOnly.Number)
	}

	dest := here
	x := int64(90)
	trip, err := domain.Dispatch(st, origin, 1, domain.DispatchInput{
		VehicleID: roleOnly.VehicleID, DriverPersonID: "d-roleonly",
		DestinationSite: &dest, DeadlineMinutes: &x,
	}, 0, "default")
	if err != nil {
		t.Fatalf("fixture: dispatching the role-only vehicle: %v", err)
	}

	// PRECONDITION, stated so a later fixture change cannot make the
	// assertion below vacuously true: the composed snapshot really is NULL.
	// If a future build derived some number here, the nil branch would stop
	// being exercised and this test would pass while testing nothing.
	stored, err := domain.TripByID(st, trip.TripID)
	if err != nil {
		t.Fatalf("reading the trip back: %v", err)
	}
	if stored == nil {
		t.Fatal("precondition: the dispatched trip is not in the store")
	}
	if stored.VehicleEntered != nil {
		t.Fatalf("precondition: trip.vehicle_entered = %q, want NULL — a role-only vehicle has "+
			"no number to snapshot, and the nil branch this test exists for is not being reached",
			*stored.VehicleEntered)
	}

	a := findAlert(t, mustEval(t, newScanner(t, st)), trip.TripID)

	// THE ASSERTION. ⟦James, 2026-10-03 — "the truck number and driver are
	// not the same on the overdue truck between the ice road and ice pad"⟧
	// The alert strip now composes the vehicle LIVE (DR-W7-18 / W7-36(a)),
	// through the same domain.LiveVehicles the origin's in-transit block
	// reads, so a numberless truck reads by its recorded ROLE here exactly as
	// it does on the origin's sheet. The role is a recorded value, not a
	// stand-in: the rule this file guards — never a SENTINEL in the vehicle
	// field ([[D-11]], DR-W7-15) — still holds, and is asserted below.
	// ⟦was: a.Vehicle must be exactly "" — the snapshot reading, which named
	// the same truck "" at the destination and "water truck" at the origin⟧
	if a.Vehicle != role {
		t.Fatalf("the alert renders a role-only vehicle as %q, want its recorded role %q — the "+
			"origin's in-transit block names it by role (LiveVehicles), and two posts must not "+
			"name one overdue truck two ways", a.Vehicle, role)
	}
	for _, sentinel := range []string{"unknown", "(no number)", "—", "no number"} {
		if strings.Contains(strings.ToLower(a.Vehicle), sentinel) {
			t.Fatalf("the alert renders a numberless vehicle with the stand-in %q (%q) — [[D-11]] "+
				"bars a magic string in the vehicle field", sentinel, a.Vehicle)
		}
	}
}

// TestW7ANumberedVehicleStillRendersItsNumber is the ACCEPTING half, and it
// is not decoration: without it the assertion above is satisfied by a build
// that returns "" for every vehicle, numberless or not — which would blank
// the vehicle on every overdue alert on the board.
func TestW7ANumberedVehicleStillRendersItsNumber(t *testing.T) {
	st := newSite(t)
	trip, _ := dispatchInbound(t, st, 90, "d-numbered")
	a := findAlert(t, mustEval(t, newScanner(t, st)), trip.TripID)
	if a.Vehicle != "TRUCK 12" {
		t.Fatalf("the alert names the vehicle as %q, want %q — an alert that cannot say WHICH "+
			"TRUCK is overdue is the one thing this board exists to say", a.Vehicle, "TRUCK 12")
	}
}
