// s121_arrival_key_control_test.go — the §48 CONTROL for the unacknowledged
// blind arrival sites in `package domain`.
//
// ⟦S121 — repair queue row A, 2026-09-04⟧ Authority:
// `reports/window-build/S107-SILENTCLASS.md`, which planted an old-style plain
// vehicle number at all 22 plantable `RecordUnsolicitedArrival` sites and found
// NINE that stayed GREEN. Five of the nine are booked (⟦DISARMED⟧ / ⭐RE-ARMED
// markers, or named in §48 / `RESEED-164.md §3.9`). FOUR carried no
// acknowledgement of any kind: A1 (`away_predicate_test.go:120`), A4 and A5
// (`stale_peer_visit_test.go:194,221`) and A19 (`web/w9a_consumers_test.go:467`).
// This file arms the three that live in `package domain`; the fourth is armed
// by `internal/web/s121_arrival_key_control_test.go`.
//
// THE DEFECT THIS CONTROL EXISTS TO KILL. `W7-4(d)(i)` re-typed
// `RecordUnsolicitedArrival`'s FOURTH POSITIONAL PARAMETER from *the vehicle
// number as typed at B's gate* to *a vehicle RECORD's id*. Type `string` →
// `string`; arity 9 → 9; and a positional parameter has NO NAME at the call
// site, so a caller that still passes `"PICKUP 1"` compiles, passes `go vet`,
// and runs green. Every step of what follows is a legal, non-erroring path:
//
//	RecordUnsolicitedArrival → vehicleNumberSnapshot → VehicleByID
//	  → `oneRow` misses → returns (nil, nil), NOT an error (vehicle.go)
//	  → the snapshot is NIL → `trip_arrival.vehicle_entered` is written NIL
//	  → `addValueKeys` (trip.go) contributes NO matching correlation key,
//	    and the id half matches nothing either.
//
// ⚠ WHY THIS IS A CONTROL AND NOT A NEW BEHAVIOURAL CRITERION. The three
// sites assert pad-listing and visit-lifecycle behaviour, and their greens
// under S107's plant were BEHAVIOURALLY CORRECT: each arrival's vehicle is
// deliberately NOT the dispatched vehicle, so no bind is intended and none
// happens. Nothing they assert is wrong. What was missing is that not one of
// them observes the correlation KEY at all — so the §50 transformation could
// be reverted at any of them today and the suite would say nothing. This
// helper adds no claim about pad listing, muster or correlation outcome. It
// asserts exactly one thing: THE VALUE THIS FIXTURE HANDED THE DOOR NAMES A
// VEHICLE RECORD AT THIS POST. That is what puts an already-correct green in
// a position to be red.
//
// ⚠ WHY A SHARED DOOR RATHER THAN THREE INLINE BLOCKS — the same argument
// `w7_reseed_helper_test.go` makes for the seeding half of the transformation.
// §47 condition 1 is "stated once, applied uniformly"; one call, spelled the
// same way at every armed site, makes "this arrival site is armed" GREPPABLE
// rather than auditable.
//
// ⚠ SCOPE, STATED SO IT IS CHECKABLE. This file arms A1, A4 and A5. It does
// NOT arm A2, A7, A8, A12 or A14 — those five are the booked half of S107's
// nine, and A12/A14 are structurally unarmable by any assertion of this shape
// (a fixture whose whole claim is `Disposition == plain` cannot distinguish a
// mutation that also yields `plain`; S107 §5b). Their correct disposition is
// a ledger row, and they have one.
package domain

import (
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// s121RequireArrivalKeyedOnVehicleRecord fails the calling fixture unless the
// unsolicited arrival it has just authored is keyed on a vehicle RECORD, held
// at this post, whose number is exactly wantNumber.
//
// `why` names what THIS site loses when the key is a bare typed number, so a
// red reads as the site's own failure rather than as a generic helper's.
//
// ⚠ THE NON-NIL SNAPSHOT CHECK IS SOUND ONLY BECAUSE THE RECORD IS HELD HERE.
// `RecordUnsolicitedArrival` writes a NIL `VehicleEntered` legitimately when
// the authoring post does not hold the vehicle record (§9.2's partition, and
// the id carries the correlation on its own in that case). Every site this
// helper guards seeds its vehicle into the SAME store it then authors against,
// so at these sites a nil snapshot has exactly one cause: the id named no
// record. A partition fixture must not call this door.
func s121RequireArrivalKeyedOnVehicleRecord(t *testing.T, st *store.Store, arr Arrival, wantNumber, why string) {
	t.Helper()
	if arr.VehicleID == nil || strings.TrimSpace(*arr.VehicleID) == "" {
		t.Fatalf("§48 CONTROL FAILED: the unsolicited arrival carries NO vehicle id at all, so its §9.2 "+
			"correlation key is absent. %s", why)
	}
	v, err := VehicleByID(st, *arr.VehicleID)
	if err != nil {
		t.Fatalf("§48 CONTROL: resolving the arrival's vehicle id %q: %v", *arr.VehicleID, err)
	}
	if v == nil {
		t.Fatalf("§48 CONTROL FAILED: the arrival's FOURTH ARGUMENT %q NAMES NO VEHICLE RECORD at this post. "+
			"`W7-4(d)(i)` re-typed that positional parameter from the number as typed to a vehicle record id "+
			"with type (string→string) and arity (9→9) unchanged, so a call site still passing the typed "+
			"number compiles, vets and runs. `VehicleByID` answers the miss with (nil, nil) rather than an "+
			"error, so `trip_arrival.vehicle_entered` is written NIL and the correlation key matches nothing. "+
			"%s", *arr.VehicleID, why)
	}
	if v.Number == nil || *v.Number != wantNumber {
		t.Fatalf("§48 CONTROL FAILED: the arrival's vehicle record %q carries the number %q, want %q — the "+
			"§50 re-seed carries the fixture's typed number through VERBATIM or it is not this "+
			"transformation. %s", *arr.VehicleID, derefString(v.Number), wantNumber, why)
	}
	if arr.VehicleEntered == nil {
		t.Fatalf("§48 CONTROL FAILED: the arrival's DERIVED B-side snapshot is NIL although this post holds "+
			"the record — `vehicleNumberSnapshot` answered nothing for id %q. %s", *arr.VehicleID, why)
	}
	if *arr.VehicleEntered != wantNumber {
		t.Fatalf("§48 CONTROL FAILED: the arrival's derived B-side snapshot is %q, want %q. %s",
			*arr.VehicleEntered, wantNumber, why)
	}
}
