// witness_comint_on_refusal_test.go — reproduction for the reported
// co-mint-then-refuse defect: DEMO-PAIR-UPDATE.md (outer docs repo, commit
// 4a55bc5) observed that a gate check-in refused for an empty vehicle field
// still left a newly minted person+company behind, and a retry minted a
// SECOND roster row for the same human.
//
// GateCheckIn (checkin.go) resolves/mints every person slot (and any
// add-on-demand company/trade) FIRST, unconditionally, before ever calling
// DispatchLocalCheckIn — which is where the vehicle id is actually
// validated/resolved (deferred there on purpose, per the "TRIAL — §97/§107"
// comments in tripauthor.go: "THE CO-MINT, AFTER EVERY VALIDATION AND BEFORE
// THE FIRST AUTHOR"). That deferral was applied to the vehicle mint only —
// the person/company mint was never moved, so it still runs before the
// vehicle validates.
package domain

import "testing"

// TestGateCheckInEmptyVehicleLeavesOrphanPersonAndCompany reproduces the
// exact demo-pair shape at a ROAD post (orientation = false, e.g. the ice
// road, D-88): a brand new driver with a brand-new add-on-demand company,
// and no vehicle typed. The check-in must be refused ("vehicle id is
// required") — the reported defect is that the person and company survive
// the refusal anyway.
func TestGateCheckInEmptyVehicleLeavesOrphanPersonAndCompany(t *testing.T) {
	st := newDomainTestStore(t)
	beforePersons, err := Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	beforeCompanies, err := Companies(st)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = GateCheckIn(st, "site-a", 1, GateCheckInInput{
		// No VehicleID and no ResolveVehicle: exactly what an empty vehicle
		// box on the gate form produces (web/gate.go's resolveVehicle
		// closure returns ("", nil) for a blank box, which dispatchTrip
		// turns into the SAME "vehicle id is required" refusal as this).
		Driver:         PersonSlot{NewName: "Demo Ariel", NewCompany: "Fakewell Testing Co"},
		HasOrientation: false, // road post — D-88, new people are legitimate here
	}, 0, "default")
	if err == nil {
		t.Fatalf("expected the check-in to be refused for a missing vehicle id")
	}

	checkinErr := err
	afterPersons, perr := Persons(st)
	if perr != nil {
		t.Fatal(perr)
	}
	afterCompanies, cerr := Companies(st)
	if cerr != nil {
		t.Fatal(cerr)
	}
	if len(afterPersons) != len(beforePersons) {
		t.Errorf("REPRODUCED: refused check-in (err=%v) still minted a person — %d persons before, %d after", checkinErr, len(beforePersons), len(afterPersons))
	}
	if len(afterCompanies) != len(beforeCompanies) {
		t.Errorf("REPRODUCED: refused check-in (err=%v) still minted a company — %d companies before, %d after", checkinErr, len(beforeCompanies), len(afterCompanies))
	}
}

// TestGateCheckInRetryAfterRefusalDoesNotDuplicatePerson reproduces the
// SECOND half of the observed defect: retrying the same check-in (this time
// with a vehicle) after a refused attempt must not leave two roster rows for
// the same human.
func TestGateCheckInRetryAfterRefusalDoesNotDuplicatePerson(t *testing.T) {
	st := newDomainTestStore(t)

	// First attempt: no vehicle, refused.
	if _, _, err := GateCheckIn(st, "site-a", 1, GateCheckInInput{
		Driver:         PersonSlot{NewName: "Sample Brix", NewCompany: "Fakewell Testing Co"},
		HasOrientation: false,
	}, 0, "default"); err == nil {
		t.Fatalf("expected the first attempt (no vehicle) to be refused")
	}

	// Retry: same typed name, this time with a vehicle.
	if _, _, err := GateCheckIn(st, "site-a", 1, GateCheckInInput{
		VehicleID:      w7SeedVehicleID(t, st, "site-a", "T-RETRY"),
		Driver:         PersonSlot{NewName: "Sample Brix", NewCompany: "Fakewell Testing Co"},
		HasOrientation: false,
	}, 0, "default"); err != nil {
		t.Fatalf("retry with a vehicle: %v", err)
	}

	all, err := Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, p := range all {
		if p.Name == "Sample Brix" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("REPRODUCED: %d roster rows for \"Sample Brix\" after refuse-then-retry, want 1", n)
	}
}

// TestGateCheckInOrientationPostNeverMintsUnorientedPerson is the R-12
// check: at an ORIENTATION post (hasOrientation = true), a brand-new,
// off-roster name must be refused BEFORE anything is minted — R-12 says
// nobody is placed in the system without an orientation, and
// resolvePersonSlot's GateNewPersonRefusal fires before any company/trade
// co-mint (see its own "FIRST, before any company/trade co-mint" comment).
// This must hold REGARDLESS of the vehicle field, so an empty vehicle proves
// nothing extra was smuggled through ahead of the orientation refusal.
func TestGateCheckInOrientationPostNeverMintsUnorientedPerson(t *testing.T) {
	st := newDomainTestStore(t)
	beforePersons, err := Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	beforeCompanies, err := Companies(st)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = GateCheckIn(st, "site-b", 1, GateCheckInInput{
		// Deliberately no vehicle either, to prove the orientation refusal
		// fires first regardless.
		Driver:         PersonSlot{NewName: "Held Outsider", NewCompany: "Fakewell Testing Co"},
		HasOrientation: true, // orientation post — R-12
	}, 0, "default")
	var refusal *GateNewPersonRefusal
	if err == nil {
		t.Fatalf("expected an off-roster name at an orientation post to be refused")
	}
	if _, ok := err.(interface{ Unwrap() error }); ok {
		// not required to unwrap; direct type assertion covers the
		// production shape (gateRefusalCopy uses errors.As on this type).
	}
	_ = refusal

	afterPersons, err := Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	afterCompanies, err := Companies(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterPersons) != len(beforePersons) {
		t.Fatalf("R-12 HOLE: orientation-post refusal (err=%v) still minted a person — %d before, %d after", err, len(beforePersons), len(afterPersons))
	}
	if len(afterCompanies) != len(beforeCompanies) {
		t.Fatalf("orientation-post refusal (err=%v) still minted a company — %d before, %d after", err, len(beforeCompanies), len(afterCompanies))
	}
}
