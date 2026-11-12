// dispatch_validation_test.go — F4 + F11 (campaign findings, "one root
// cause, two symptoms"): Dispatch (tripauthor.go) checked only that
// driver/occupant ids were non-empty, never that they existed on the
// roster (F11) — unlike every other authoring path (resolvePersonSlot,
// checkin.go) — and never validated a destination site string at all (F4).
// Both are now opt-in via DispatchInput.ValidateRoster/KnownDestinationSites,
// OFF by default so every pre-existing Dispatch() call site across
// domain/web/deadline (many inject synthetic ids/sites to build a scenario
// without seeding a full roster or peer config) stays behaviour-identical —
// see the field's doc comment in tripauthor.go. New coverage only; nothing
// in trip_test.go or checkin_test.go is modified.
package domain

import "testing"

// --- F11: ValidateRoster ---

func TestDispatchValidateRosterRefusesUnknownDriverID(t *testing.T) {
	st := newDomainTestStore(t)
	if _, err := DispatchLocalCheckIn(st, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRUCK 1", DriverPersonID: "site-a-person-doesnotexist","⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRUCK 1"), DriverPersonID: "site-a-person-doesnotexist",
		ValidateRoster: true,
	}, 0, "default"); err == nil {
		t.Fatal("expected a refusal for a driver id that is not on the roster, got nil")
	}
}

func TestDispatchValidateRosterRefusesUnknownOccupantID(t *testing.T) {
	st := newDomainTestStore(t)
	driver, err := CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DispatchLocalCheckIn(st, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRUCK 1", DriverPersonID: driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRUCK 1"), DriverPersonID: driver.PersonID,
		OccupantPersonIDs: []string{"site-a-person-doesnotexist"},
		ValidateRoster:    true,
	}, 0, "default"); err == nil {
		t.Fatal("expected a refusal for an occupant id that is not on the roster, got nil")
	}
}

func TestDispatchValidateRosterAllowsRealRosterIDs(t *testing.T) {
	st := newDomainTestStore(t)
	driver, err := CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	passenger, err := CreatePerson(st, "site-a", 1, "Pat Passenger", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DispatchLocalCheckIn(st, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRUCK 1", DriverPersonID: driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRUCK 1"), DriverPersonID: driver.PersonID,
		OccupantPersonIDs: []string{passenger.PersonID},
		ValidateRoster:    true,
	}, 0, "default"); err != nil {
		t.Fatalf("expected success for real roster ids, got: %v", err)
	}
}

// TestDispatchWithoutValidateRosterStillAllowsUnknownIDs is the backward-
// compatibility negative control: the zero value (false) must behave
// exactly as before the fix — this is what keeps every pre-existing
// Dispatch() caller (internal/deadline's tests among them) unaffected.
func TestDispatchWithoutValidateRosterStillAllowsUnknownIDs(t *testing.T) {
	st := newDomainTestStore(t)
	if _, err := DispatchLocalCheckIn(st, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRUCK 1", DriverPersonID: "not-a-real-person","⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRUCK 1"), DriverPersonID: "not-a-real-person",
	}, 0, "default"); err != nil {
		t.Fatalf("ValidateRoster defaults to off: expected success, got: %v", err)
	}
}

// --- F4: KnownDestinationSites ---

func TestDispatchKnownDestinationSitesRefusesUnknownDestination(t *testing.T) {
	st := newDomainTestStore(t)
	driver, err := CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	dest := "site-bee" // the exact typo shape from the campaign finding
	deadlineMin := int64(60)
	if _, err := Dispatch(st, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRUCK 1", DriverPersonID: driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRUCK 1"), DriverPersonID: driver.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &deadlineMin,
		KnownDestinationSites: []string{"site-b", "site-c"},
	}, 0, "default"); err == nil {
		t.Fatal("expected a refusal for a destination not in the known peer list, got nil")
	}
}

func TestDispatchKnownDestinationSitesAllowsAListedDestination(t *testing.T) {
	st := newDomainTestStore(t)
	driver, err := CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	dest := "site-b"
	deadlineMin := int64(60)
	if _, err := Dispatch(st, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRUCK 1", DriverPersonID: driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRUCK 1"), DriverPersonID: driver.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &deadlineMin,
		KnownDestinationSites: []string{"site-b", "site-c"},
	}, 0, "default"); err != nil {
		t.Fatalf("expected success for a known peer destination, got: %v", err)
	}
}

// TestDispatchWithoutKnownDestinationSitesStillAllowsAnyDestination is the
// backward-compatibility negative control: KnownDestinationSites defaults to
// nil, which performs no check at all — the shape every pre-existing test
// using synthetic destinations ("site-b", "site-c", ...) already relies on.
func TestDispatchWithoutKnownDestinationSitesStillAllowsAnyDestination(t *testing.T) {
	st := newDomainTestStore(t)
	driver, err := CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	dest := "site-bee"
	deadlineMin := int64(60)
	if _, err := Dispatch(st, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRUCK 1", DriverPersonID: driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRUCK 1"), DriverPersonID: driver.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &deadlineMin,
	}, 0, "default"); err != nil {
		t.Fatalf("KnownDestinationSites defaults to nil (no check): expected success, got: %v", err)
	}
}
