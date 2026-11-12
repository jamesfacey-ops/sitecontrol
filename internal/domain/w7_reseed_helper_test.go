// w7_reseed_helper_test.go — the ONE seeding door this package's fixtures use
// after `W7-4(d)(i)`.
//
// ⟦LOCK — §47 re-seed, 2026-09-03⟧ Authority:
// `reports/W5-POSTLOCK-AMENDMENTS.md` §47, which grants ONE transformation
// applied uniformly across 164 sites, with a routing valve for anything that
// is not mechanical. This file is that transformation's mechanism; the
// transformation itself is stated once in
// `reports/window-build/RESEED-164.md` §1.
//
// WHY A SHARED HELPER RATHER THAN 164 INLINE `CreateVehicle` CALLS. §47's
// condition 1 is "stated once, applied uniformly". A single door is what makes
// "uniformly" checkable by a gate: every re-seeded site reads
// `VehicleID: w7SeedVehicleID(...)` and nothing else, so a site that was
// transformed some OTHER way is visible by inspection rather than by audit.
//
// ⚠ THIS FILE ADDS NO ASSERTION. Its two t.Fatalf calls are PRECONDITION
// guards on the transformation itself, not tests of production behaviour:
// they fire only when a site was re-seeded that §47 required to be ROUTED, and
// their whole purpose is that such a site cannot pass silently. See the
// comments on each.
package domain

import (
	"testing"

	"sitecontrol/internal/store"
)

// w7SeedVehicleID returns the id of a vehicle RECORD at siteID whose number is
// exactly `number`, creating it on first use and reusing it afterwards.
//
// ⚠ THE REUSE IS THE MEANING, NOT AN OPTIMISATION. Before `W7-4(d)(i)` two
// dispatches carrying the same typed string were two runs OF THE SAME TRUCK.
// Minting a fresh record per call would make them two DIFFERENT trucks, which
// changes distinct-vehicle counts, muster in-transit identity and the §9.2
// id-key correlation group — a fixture MEANING change, which §47 condition 3
// bars in terms. `w7_muster_road_test.go`'s re-author found the same thing and
// memoised for the same reason (its plant P8 is the produced proof).
//
// ⚠ IT LOOKS THE RECORD UP IN THE STORE RATHER THAN IN A PACKAGE-LEVEL MAP.
// A map keyed on *store.Store outlives the test that owned the store, and Go
// may hand a later test the same address — which would return an id naming no
// record in the NEW store, and `vehicleNumberSnapshot` answers a miss with a
// NIL snapshot rather than an error (tripauthor.go). That is a silent
// wrong-fixture, so the memo is derived from the store's own composed state
// and cannot go stale.
func w7SeedVehicleID(t *testing.T, st *store.Store, siteID, number string) string {
	t.Helper()
	existing, err := Vehicles(st)
	if err != nil {
		t.Fatalf("w7SeedVehicleID: read vehicles: %v", err)
	}
	for _, v := range existing {
		if v.SiteID == siteID && v.Number != nil && *v.Number == number {
			return v.VehicleID
		}
	}
	n := number
	v, err := CreateVehicle(st, siteID, 1, &n, nil, 0, "default")
	if err != nil {
		// ⚠ NOT AN ASSERTION — a ROUTING guard. CreateVehicle refuses exactly
		// two things: a record identifying nothing, and a PLACEHOLDER number
		// (the relocated F43 guard, vehicle.go). Both are §47-routed cases, so
		// reaching here means a site was transformed that should have been
		// routed to the plan owner.
		t.Fatalf("w7SeedVehicleID: CreateVehicle refused %q: %v — a site whose vehicle string "+
			"CreateVehicle refuses is a ROUTED site under §47 condition 3, not a re-seeded one", number, err)
	}
	if v.Number == nil || *v.Number != number {
		// ⚠ NOT AN ASSERTION — the second ROUTING guard. CreateVehicle applies
		// trimmedOrNil, so a padded literal ("  TRUCK   12") does NOT survive
		// verbatim into the record, and therefore not into the derived
		// trip.vehicle_entered snapshot either. Any fixture that depends on
		// the padding is a §47-routed site.
		t.Fatalf("w7SeedVehicleID: CreateVehicle stored %q for the number %q — the re-seed carries the "+
			"number through VERBATIM or it is not this transformation; a padded value is a ROUTED site",
			derefString(v.Number), number)
	}
	return v.VehicleID
}
