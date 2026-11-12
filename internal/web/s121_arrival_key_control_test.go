// s121_arrival_key_control_test.go — the §48 CONTROL for A19, the one
// unacknowledged blind arrival site that lives in `package web`.
//
// ⟦S121 — repair queue row A, 2026-09-04⟧ Authority and mechanism are stated
// once, in `internal/domain/s121_arrival_key_control_test.go`. In brief:
// `W7-4(d)(i)` re-typed `RecordUnsolicitedArrival`'s FOURTH POSITIONAL
// PARAMETER from the vehicle number as typed to a vehicle RECORD's id, with
// type (string→string) and arity (9→9) unchanged — so a call site still
// passing a plain number compiles, vets and runs green, the derived snapshot
// is written NIL, and nothing correlates.
//
// ⚠ WHY THIS SITE IS THE SHARPEST OF THE FOUR, and why its guard is written
// as a PROBE DEFECT rather than as an ordinary assertion.
// `TestW9a7TheAlertStaysLiveAcrossAnArrivalClaimCorrection` is about
// CORRECTING AN ARRIVAL'S VEHICLE CLAIM: its own fixture comment calls the
// seeded arrival "the mistyped unsolicited arrival". Under S107's plant that
// arrival's claim is NIL — ⛔ there is no mistyped claim to correct — and the
// test still passed. The fixture's stated subject and what it could actually
// observe had come apart. The file already carries one guard of exactly this
// shape ("W9a-7 PROBE DEFECT: the banner does not carry the overdue trip
// before the correction, so \"still live\" is not a judgement"); this is its
// missing twin on the arrival half, and it is deliberately spelled in the
// same idiom.
//
// ⚠ IT ADDS NO BEHAVIOURAL CRITERION. W9a-7's claim — that a claim correction
// never re-runs correlation, never binds, never un-binds — is untouched. This
// guard asserts only that the premise that claim is judged against exists.
package web

import (
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// s121RequireArrivalKeyedOnVehicleRecord — the `package web` twin of the
// `package domain` door of the same name. It fails the calling fixture unless
// the unsolicited arrival just authored is keyed on a vehicle RECORD, held at
// this post, whose number is exactly wantNumber.
//
// ⚠ The non-nil snapshot check is sound only because the record is held here:
// a NIL `VehicleEntered` is legitimate when the authoring post does not hold
// the vehicle record (§9.2's partition). A19 seeds its vehicle into the SAME
// store it then authors against, so at that site a nil snapshot has exactly
// one cause — the id named no record. A partition fixture must not call this.
func s121RequireArrivalKeyedOnVehicleRecord(t *testing.T, st *store.Store, arr domain.Arrival, wantNumber, why string) {
	t.Helper()
	if arr.VehicleID == nil || strings.TrimSpace(*arr.VehicleID) == "" {
		t.Fatalf("§48 CONTROL FAILED: the unsolicited arrival carries NO vehicle id at all, so its §9.2 "+
			"correlation key is absent. %s", why)
	}
	v, err := domain.VehicleByID(st, *arr.VehicleID)
	if err != nil {
		t.Fatalf("§48 CONTROL: resolving the arrival's vehicle id %q: %v", *arr.VehicleID, err)
	}
	if v == nil {
		t.Fatalf("§48 CONTROL FAILED: the arrival's FOURTH ARGUMENT %q NAMES NO VEHICLE RECORD at this post. "+
			"`W7-4(d)(i)` re-typed that positional parameter from the number as typed to a vehicle record id "+
			"with type (string→string) and arity (9→9) unchanged, so a call site still passing the typed "+
			"number compiles, vets and runs. `VehicleByID` answers the miss with (nil, nil) rather than an "+
			"error, so `trip_arrival.vehicle_entered` is written NIL and there is no claim to correct. "+
			"%s", *arr.VehicleID, why)
	}
	if got := s121DerefString(v.Number); got != wantNumber {
		t.Fatalf("§48 CONTROL FAILED: the arrival's vehicle record %q carries the number %q, want %q — the "+
			"§50 re-seed carries the fixture's typed number through VERBATIM or it is not this "+
			"transformation. %s", *arr.VehicleID, got, wantNumber, why)
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

func s121DerefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
