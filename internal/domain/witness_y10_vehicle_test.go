// witness_y10_vehicle_test.go — witness phase, worktree y10-vehicle.
//
// Kills two census survivors (reports/laptop-pickup/U-row-Y-census.md, via
// row-y-evidence/plants.py): Q4 (vehicle.go:237, CreateVehicle's
// identifies-nothing guard) and Q15 (vehicle.go:423,
// ResolveOrCreateVehicleByNumber's empty-input short-circuit). Both are
// executed by other tests' fixtures but never asserted on directly — the
// census's point exactly: coverage is not catching.
//
// Also witnesses AmendVehicle's Arm 10 (vehicle_amend.go:676, the cycle
// check), which s120_vehicle_amend_test.go named and explicitly declined to
// cover ("ARM 10 (the cycle) IS NOT COVERED HERE") — recorded in the census
// as `AmendVehicle` moving 0/47 -> 36/47 under S120's repair "with Arm 10
// still unreached exactly as S120 said." No wire trick is needed for this
// arm specifically: the walk reads the COMPOSED merge map, which an ordinary
// same-process chain of two accepted amendments already populates.
//
// Bonus (working outward, ERR-ARM class): AmendVehicle's very first guard
// (vehicle_amend.go:552), which propagates a store fault from its own
// VehicleByID lookup. No test anywhere forces that lookup to fail, so no
// test has ever confirmed this arm does what W7-15's own design note (right
// above VehicleAmendInputError) says a real incident required: a store fault
// must reach the officer as an internal error, never be reclassified as "not
// a vehicle on this post" — the exact confusion class the note names as the
// W5d incident this file's error taxonomy was built to stop.
package domain

import (
	"errors"
	"strings"
	"testing"
)

// TestWitnessQ4VehicleWithNeitherNumberNorRoleIsRefused witnesses Q4.
//
// ⛔ MEASURED: no test anywhere in the tree calls CreateVehicle with both
// number and role nil (`grep -rn 'CreateVehicle(' --include='*_test.go'` —
// every call site passes at least one of the two). The guard at
// vehicle.go:237 is therefore reached by nothing that notices if it is cut.
func TestWitnessQ4VehicleWithNeitherNumberNorRoleIsRefused(t *testing.T) {
	st := newDomainTestStore(t)
	before := s120Watermark(t, st)

	_, err := CreateVehicle(st, "site-a", 1, nil, nil, 0, "default")
	if err == nil {
		t.Fatalf("CreateVehicle ACCEPTED a record with neither a number nor a role — " +
			"D-11: a numberless truck must still be shown by its role; a record with " +
			"neither identifies nothing and cannot be picked, searched for or read on a muster")
	}
	want := "a vehicle record needs a number or a role"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("the refusal does not say what it is for.\ngot:  %v\nwant a message containing: %q", err, want)
	}
	// A refusal must leave no trace — checked separately from the message
	// because a build can refuse loudly and append anyway.
	if after := s120Watermark(t, st); after != before {
		t.Fatalf("a REFUSED create authored %d event(s) — a refusal never calls Author", after-before)
	}
}

// TestWitnessQ15ResolveOrCreateVehicleByNumberOnEmptyInputIsANoOp witnesses
// Q15.
//
// ⛔ MEASURED: `grep -rn 'ResolveOrCreateVehicleByNumber' --include='*_test.go'`
// returns only a comment (internal/web/trips_test.go:446) — no test calls the
// function at all.
//
// ⭐ The officer-facing behaviour this guard protects is not "returns empty" —
// it is "does not ALSO try to mint a record and get refused doing it." The
// function's own comment states the refusal for an empty number belongs at
// the authoring door (W7-4(b)), not here; if the short-circuit at line 423 is
// cut, an empty number instead falls through to a co-mint attempt, and
// CreateVehicle's own Q4 guard (both trimmedOrNil'd arguments are nil) refuses
// it — turning a designed no-op into a surfaced domain error from the wrong
// call site.
func TestWitnessQ15ResolveOrCreateVehicleByNumberOnEmptyInputIsANoOp(t *testing.T) {
	st := newDomainTestStore(t)
	before := s120Watermark(t, st)

	id, err := ResolveOrCreateVehicleByNumber(st, "site-a", 1, "   ")
	if err != nil {
		t.Fatalf("ResolveOrCreateVehicleByNumber(blank) = err %v, want nil — "+
			"an officer who leaves the vehicle box blank must not meet a refusal "+
			"here; that sentence belongs at the authoring door (W7-4(b))", err)
	}
	if id != "" {
		t.Fatalf("ResolveOrCreateVehicleByNumber(blank) = id %q, want empty", id)
	}
	if after := s120Watermark(t, st); after != before {
		t.Fatalf("a blank-number call authored %d event(s) — it must be a pure no-op, not a co-mint attempt", after-before)
	}
}

// TestWitnessArm10AmendVehicleRefusesACycleClosedThroughComposedEdges
// witnesses AmendVehicle's Arm 10 (vehicle_amend.go:676).
//
// A -> B and B -> C are each accepted on their own (neither one is a cycle in
// isolation — this is W7-14's "edges refuse cycles locally" point, walked
// forward instead of formed on the wire). Attempting C -> A must then be
// refused: composed with the two standing edges it would close
// A -> B -> C -> A.
func TestWitnessArm10AmendVehicleRefusesACycleClosedThroughComposedEdges(t *testing.T) {
	st := newDomainTestStore(t)
	a := s120Vehicle(t, st, s120Str("TRUCK A"), nil)
	b := s120Vehicle(t, st, s120Str("TRUCK B"), nil)
	c := s120Vehicle(t, st, s120Str("TRUCK C"), nil)

	if _, err := AmendVehicle(st, "site-a", 1,
		VehicleAmendment{VehicleID: a.VehicleID, SameAs: s120Str(b.VehicleID)}, 0, "default"); err != nil {
		t.Fatalf("fixture: A->B must be accepted: %v", err)
	}
	if _, err := AmendVehicle(st, "site-a", 1,
		VehicleAmendment{VehicleID: b.VehicleID, SameAs: s120Str(c.VehicleID)}, 0, "default"); err != nil {
		t.Fatalf("fixture: B->C must be accepted: %v", err)
	}

	before := s120Watermark(t, st)
	_, err := AmendVehicle(st, "site-a", 1,
		VehicleAmendment{VehicleID: c.VehicleID, SameAs: s120Str(a.VehicleID)}, 0, "default")
	if err == nil {
		t.Fatalf("AmendVehicle ACCEPTED an amendment that closes a cycle A->B->C->A")
	}
	want := "would close a cycle"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("the refusal does not say what it is for.\ngot:  %v\nwant a message containing: %q", err, want)
	}
	if after := s120Watermark(t, st); after != before {
		t.Fatalf("a REFUSED amendment authored %d event(s) — a refusal never calls Author", after-before)
	}
}

// TestWitnessAmendVehicleFirstErrArmDoesNotMisreportAStoreFaultAsNotFound
// witnesses AmendVehicle's first ERR-ARM (vehicle_amend.go:552). Closing the
// store makes its own VehicleByID lookup fail before anything else runs, so
// the guard at 552 is the only thing standing between a real store fault and
// v==nil at line 555 — which reads EXACTLY like "no such vehicle" and would
// hand the officer *VehicleAmendInputError's "is not a vehicle on this post"
// for a cause that has nothing to do with what they typed.
func TestWitnessAmendVehicleFirstErrArmDoesNotMisreportAStoreFaultAsNotFound(t *testing.T) {
	st := newDomainTestStore(t)
	if err := st.Close(); err != nil {
		t.Fatalf("fixture: closing the store: %v", err)
	}

	_, err := AmendVehicle(st, "site-a", 1,
		VehicleAmendment{VehicleID: "site-a-vehicle-doesnotmatter", Number: s120Str("TRUCK 9")}, 0, "default")
	if err == nil {
		t.Fatalf("AmendVehicle on a closed store returned no error at all")
	}
	var inputErr *VehicleAmendInputError
	if errors.As(err, &inputErr) {
		t.Fatalf("AmendVehicle reported a STORE FAULT as %T (%q) — an officer would read this as "+
			"'that vehicle does not exist' when the real cause is the store itself failing, "+
			"exactly the class of confusion VehicleAmendInputError's own design note says a real "+
			"incident (W5d) already produced once", inputErr, err)
	}
	want := "domain: amend vehicle:"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("got %v, want the plain wrapped store error containing %q", err, want)
	}
}
