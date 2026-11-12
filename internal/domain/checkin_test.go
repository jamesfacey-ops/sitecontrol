// checkin_test.go — C8's domain-level done-when tests: the gate check-in
// over the degenerate-trip machinery, the off-roster add-on-demand path
// (M6), check-out as an ordinary own-site event (design §8.1), its row-28
// site-scoping guard, and the row-31 crash-window recovery path.
//
// newDomainTestStore is shared from refdata_test.go (same package).
package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

func seedTwoPersons(t *testing.T, st *store.Store, siteID string) (driver, passenger Person) {
	t.Helper()
	co, err := CreateCompany(st, siteID, 1, "Kakivik Asset Management", 0, "default")
	if err != nil {
		t.Fatalf("create company: %v", err)
	}
	// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ DR-W8-2 arity: the company argument is dropped — a trade is a site-wide open set, not scoped to a company. ⟦was: "wg, err := CreateTrade(st, siteID, 1, co.CompanyID, "Inspection", 0, "default")"⟧
	wg, err := CreateTrade(st, siteID, 1, "Inspection", 0, "default")
	if err != nil {
		t.Fatalf("create workgroup: %v", err)
	}
	driver, err = CreatePerson(st, siteID, 1, "Dana Driver", &co.CompanyID, &wg.TradeID, 0, "default")
	if err != nil {
		t.Fatalf("create driver: %v", err)
	}
	passenger, err = CreatePerson(st, siteID, 1, "Pat Passenger", &co.CompanyID, &wg.TradeID, 0, "default")
	if err != nil {
		t.Fatalf("create passenger: %v", err)
	}
	return driver, passenger
}

// TestGateCheckInAllOnRosterCreatesOneVisitPerOccupant is criterion 1's
// server-side half: a driver+2-passenger check-in, all on the roster,
// creates exactly one snapshot visit per occupant and a degenerate
// (no destination, no deadline) trip — the SAME machinery C6 built, per
// §10's "no second mechanism" rule.
func TestGateCheckInAllOnRosterCreatesOneVisitPerOccupant(t *testing.T) {
	st := newDomainTestStore(t)
	driver, p1 := seedTwoPersons(t, st, "site-a")
	p2, err := CreatePerson(st, "site-a", 1, "Third Passenger", nil, nil, 0, "default")
	if err != nil {
		t.Fatalf("create third passenger: %v", err)
	}

	trip, arrival, err := GateCheckIn(st, "site-a", 1, GateCheckInInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "T12","⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "T12"),
		Driver:    PersonSlot{ExistingID: driver.PersonID},
		Occupants: []PersonSlot{{ExistingID: p1.PersonID}, {ExistingID: p2.PersonID}},
	}, 0, "default")
	if err != nil {
		t.Fatalf("GateCheckIn: %v", err)
	}
	if trip.DestinationSite != nil || trip.DeadlineMinutes != nil {
		t.Fatalf("check-in trip is not degenerate: %+v", trip)
	}
	if arrival.ArrivalID == "" {
		t.Fatalf("no arrival authored")
	}

	visits, err := Visits(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(visits) != 3 {
		t.Fatalf("visits = %d, want 3 (driver + 2 occupants)", len(visits))
	}
	seen := map[string]bool{}
	for _, v := range visits {
		if v.IsOpen != 1 {
			t.Fatalf("visit %s not open: %+v", v.VisitID, v)
		}
		if v.CompanySnapshotName == nil && v.PersonID != p2.PersonID {
			t.Fatalf("visit %s missing its point-in-time company snapshot: %+v", v.VisitID, v)
		}
		seen[v.PersonID] = true
	}
	for _, id := range []string{driver.PersonID, p1.PersonID, p2.PersonID} {
		if !seen[id] {
			t.Fatalf("occupant %s is not on the on-site list immediately after check-in", id)
		}
	}
}

// TestGateCheckInOffRosterOccupantCreatesPersonAndCompletes is M6's
// executable assert: one off-roster occupant triggers add-on-demand,
// creates the person through the ORDINARY authoring path (CreatePerson —
// not a special-cased side write), and the check-in still completes.
func TestGateCheckInOffRosterOccupantCreatesPersonAndCompletes(t *testing.T) {
	st := newDomainTestStore(t)
	driver, p1 := seedTwoPersons(t, st, "site-a")

	beforePersons, err := Persons(st)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = GateCheckIn(st, "site-a", 1, GateCheckInInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRK-14","⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK-14"),
		Driver:    PersonSlot{ExistingID: driver.PersonID},
		Occupants: []PersonSlot{
			{ExistingID: p1.PersonID},
			{NewName: "New Guy", NewCompany: "Fresh Co", NewTrade: "Newcomers"},
		},
	}, 0, "default")
	if err != nil {
		t.Fatalf("GateCheckIn with an off-roster occupant: %v", err)
	}

	afterPersons, err := Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterPersons) != len(beforePersons)+1 {
		t.Fatalf("persons after = %d, want %d (+1 for the add-on-demand person)", len(afterPersons), len(beforePersons)+1)
	}
	var newPerson *Person
	for i := range afterPersons {
		if afterPersons[i].Name == "New Guy" {
			newPerson = &afterPersons[i]
		}
	}
	if newPerson == nil {
		t.Fatal("add-on-demand person was not created through CreatePerson (not found in Persons())")
	}
	if newPerson.CompanyID == nil || newPerson.TradeID == nil {
		t.Fatalf("add-on-demand person missing its on-the-spot company/workgroup: %+v", newPerson)
	}

	visits, err := Visits(st)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range visits {
		if v.PersonID == newPerson.PersonID {
			found = true
			if v.CompanySnapshotName == nil || *v.CompanySnapshotName != "Fresh Co" {
				t.Fatalf("new person's visit snapshot = %+v, want company Fresh Co", v)
			}
		}
	}
	if !found {
		t.Fatal("the off-roster occupant has no visit — check-in did not complete for them")
	}
}

// TestGateCheckInResolvesByIDNeverByNameEvenWithDuplicateDisplayNames is the
// server-side half of the verification-round-1 finding: two roster persons
// can share a display name (not exotic on a work crew), and identity must
// come ONLY from the submitted id, never be re-derived from the name text.
// resolvePersonSlot keys strictly through rosterByID (a map keyed on
// PersonID), so it cannot exhibit "first match by name wins" even in
// principle; this test pins that contract with a real duplicate-name
// fixture so a future edit that starts matching by name would fail it.
// (The actual BUG the review found lived entirely in gate.html's
// client-side JS, in its resolvePick function. NOTE, corrected 2026-08-21:
// this comment used to cite a "RESOLVE_PICK_START block" and a
// "fail-then-pass Node verification". Neither exists — there is no Node
// harness anywhere in this repository and the markers have been removed.
// The client-side half has NO automated check; gate.html says so at the
// function itself. What IS verified is the server-side half, below.)
func TestGateCheckInResolvesByIDNeverByNameEvenWithDuplicateDisplayNames(t *testing.T) {
	st := newDomainTestStore(t)
	coA, err := CreateCompany(st, "site-a", 1, "Alpha Co", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	coB, err := CreateCompany(st, "site-a", 1, "Beta Co", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// Same name, different companies, different ids — minted in an order
	// where a NAME-based "first match" would silently resolve to personA
	// (created first) no matter which one the caller actually meant.
	personA, err := CreatePerson(st, "site-a", 1, "Chris Crew", &coA.CompanyID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	personB, err := CreatePerson(st, "site-a", 1, "Chris Crew", &coB.CompanyID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if personA.PersonID == personB.PersonID {
		t.Fatal("fixture is broken: the two same-named persons must have distinct ids")
	}

	// Explicitly resolve the SECOND one by id (what a click on the second
	// dropdown entry sends server-side) — must never collapse to the first.
	_, _, err = GateCheckIn(st, "site-a", 1, GateCheckInInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRK-14","⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK-14"),
		Driver:    PersonSlot{ExistingID: personB.PersonID},
	}, 0, "default")
	if err != nil {
		t.Fatalf("GateCheckIn: %v", err)
	}

	visits, err := Visits(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(visits) != 1 {
		t.Fatalf("visits = %d, want 1", len(visits))
	}
	if visits[0].PersonID != personB.PersonID {
		t.Fatalf("checked in person %s, want %s (the SECOND Chris Crew) — identity collapsed to the wrong same-named person", visits[0].PersonID, personB.PersonID)
	}
	if visits[0].CompanySnapshotID == nil || *visits[0].CompanySnapshotID != coB.CompanyID {
		t.Fatalf("visit snapshot company = %v, want %s (Beta Co, personB's own company) — got the wrong same-named person's data", visits[0].CompanySnapshotID, coB.CompanyID)
	}
}

// TestGateCheckInRefusesUnknownExistingID guards a stale/tampered id: a
// PersonSlot claiming ExistingID that is not on this store's roster refuses
// cleanly rather than authoring a dangling reference.
func TestGateCheckInRefusesUnknownExistingID(t *testing.T) {
	st := newDomainTestStore(t)
	driver, _ := seedTwoPersons(t, st, "site-a")
	_, _, err := GateCheckIn(st, "site-a", 1, GateCheckInInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "T12","⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "T12"),
		Driver:    PersonSlot{ExistingID: driver.PersonID},
		Occupants: []PersonSlot{{ExistingID: "site-a-person-doesnotexist"}},
	}, 0, "default")
	if err == nil {
		t.Fatal("GateCheckIn with an unknown existing person id should refuse, got nil error")
	}
	trips, err := Trips(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 0 {
		t.Fatalf("a refused check-in must not author a trip: %+v", trips)
	}
}

// --- check-out (design §8.1: ordinary own-site log event) ---

func TestCheckOutClosesVisitAsAnOrdinaryOwnSiteEvent(t *testing.T) {
	st := newDomainTestStore(t)
	driver, _ := seedTwoPersons(t, st, "site-a")
	_, _, err := GateCheckIn(st, "site-a", 1, GateCheckInInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRK-14", Driver: PersonSlot{ExistingID: driver.PersonID},"⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK-14"), Driver: PersonSlot{ExistingID: driver.PersonID},
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	visits, err := Visits(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(visits) != 1 {
		t.Fatalf("visits = %d, want 1", len(visits))
	}
	visitID := visits[0].VisitID

	if err := CheckOutVisit(st, "site-a", 1, visitID, nil, 0, "default"); err != nil {
		t.Fatalf("check out: %v", err)
	}

	// The checkout is an ordinary authored event: it must be visible in the
	// raw visit_checkout dump, not just as a mutation nobody can point to.
	checkouts, err := dumpSection[VisitCheckout](st, "visit_checkout")
	if err != nil {
		t.Fatal(err)
	}
	if len(checkouts) != 1 || checkouts[0].VisitID != visitID {
		t.Fatalf("visit_checkout raw rows = %+v, want one row for %s", checkouts, visitID)
	}

	visits, err = Visits(st)
	if err != nil {
		t.Fatal(err)
	}
	if visits[0].IsOpen != 0 {
		t.Fatalf("visit still open after check-out: %+v", visits[0])
	}

	// A second check-out of the same visit refuses rather than silently
	// re-applying.
	if err := CheckOutVisit(st, "site-a", 1, visitID, nil, 0, "default"); err == nil {
		t.Fatal("a second check-out of an already-closed visit should refuse")
	}
}

// TestCheckOutRefusesAcrossSites is CheckOutVisit's own authoring-time guard:
// a site cannot check out a visit that belongs to a different site.
func TestCheckOutRefusesAcrossSites(t *testing.T) {
	st := newDomainTestStore(t)
	driverA, _ := seedTwoPersons(t, st, "site-a")
	_, _, err := GateCheckIn(st, "site-a", 1, GateCheckInInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRK-14", Driver: PersonSlot{ExistingID: driverA.PersonID},"⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK-14"), Driver: PersonSlot{ExistingID: driverA.PersonID},
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	visits, err := Visits(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckOutVisit(st, "site-b", 1, visits[0].VisitID, nil, 0, "default"); err == nil {
		t.Fatal("check-out from a different site should refuse")
	}
	visits, err = Visits(st)
	if err != nil {
		t.Fatal(err)
	}
	if visits[0].IsOpen != 1 {
		t.Fatal("a refused cross-site check-out must not have closed the visit")
	}
}

// TestCheckoutFoldNeverClosesAForeignSitesVisit is the C6-review-row-28
// carry-forward's defense-in-depth: it bypasses CheckOutVisit's own
// authoring-time same-site refusal entirely and injects the raw fold event
// directly (as a corrupted peer or a future bug might), to prove the FOLD
// ITSELF — not just the authoring-time check — scopes the mutation to its
// own authoring site and can never close a visit belonging to another site.
func TestCheckoutFoldNeverClosesAForeignSitesVisit(t *testing.T) {
	st := newDomainTestStore(t)
	driverB, _ := seedTwoPersons(t, st, "site-b")
	_, _, err := GateCheckIn(st, "site-b", 1, GateCheckInInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRK-14", Driver: PersonSlot{ExistingID: driverB.PersonID},"⟧
		VehicleID: w7SeedVehicleID(t, st, "site-b", "TRK-14"), Driver: PersonSlot{ExistingID: driverB.PersonID},
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	visits, err := Visits(st)
	if err != nil {
		t.Fatal(err)
	}
	siteBVisitID := visits[0].VisitID

	// Craft a checkout event claiming site-a authorship but naming site-b's
	// visit id — exactly the shape a foreign event would have to take to
	// reach in and close a visit it does not own.
	payload, err := json.Marshal(VisitCheckedOut{Type: "visit_checked_out", VisitID: siteBVisitID})
	if err != nil {
		t.Fatal(err)
	}
	applied, err := st.Apply(store.Envelope{
		SiteID: "site-a", NodeEpoch: 1, Seq: 1,
		SchemaVersion: store.SchemaVersion, OriginAuth: "site-a", Payload: payload,
	})
	if err != nil {
		t.Fatalf("apply foreign checkout event: %v", err)
	}
	if !applied {
		t.Fatal("foreign checkout event was not applied at all (watermark issue, not the guard under test)")
	}

	visits, err = Visits(st)
	if err != nil {
		t.Fatal(err)
	}
	if visits[0].IsOpen != 1 {
		t.Fatalf("row-28 guard failed: a site-a-authored checkout event closed site-b's visit: %+v", visits[0])
	}
}

// --- row 31: the crash window between dispatch and arrival ---

// TestIncompleteCheckInIsVisibleAndResumable is row 31's chosen strategy
// (visible + recoverable, not atomic — store.Author authors one event per
// call and internal/store/log.go is off limits to this chunk): simulate the
// crash by authoring ONLY the dispatch half directly (skipping
// RecordExpectedArrival, exactly what a process death between GateCheckIn's
// two Author calls would leave behind), confirm the stuck trip is found by
// IncompleteCheckIns, then confirm ResumeCheckIn (== RecordExpectedArrival)
// completes it.
func TestIncompleteCheckInIsVisibleAndResumable(t *testing.T) {
	st := newDomainTestStore(t)
	driver, p1 := seedTwoPersons(t, st, "site-a")

	trip, err := DispatchLocalCheckIn(st, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "T12", DriverPersonID: driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "T12"), DriverPersonID: driver.PersonID,
		OccupantPersonIDs: []string{p1.PersonID},
		// DestinationSite / DeadlineMinutes both nil — the degenerate local
		// check-in's dispatch half, with NO arrival authored: the crash.
	}, 0, "default")
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	visits, err := Visits(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(visits) != 0 {
		t.Fatalf("dispatch alone must not create visits: %+v", visits)
	}

	stuck, err := IncompleteCheckIns(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(stuck) != 1 || stuck[0].TripID != trip.TripID {
		t.Fatalf("IncompleteCheckIns = %+v, want exactly the crashed trip %s", stuck, trip.TripID)
	}

	if _, err := ResumeCheckIn(st, "site-a", 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatalf("resume: %v", err)
	}

	stuck, err = IncompleteCheckIns(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(stuck) != 0 {
		t.Fatalf("IncompleteCheckIns after resume = %+v, want none", stuck)
	}
	visits, err = Visits(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(visits) != 2 {
		t.Fatalf("visits after resume = %d, want 2 (driver + occupant)", len(visits))
	}
	for _, v := range visits {
		if v.IsOpen != 1 {
			t.Fatalf("resumed visit not open: %+v", v)
		}
	}

	// Resuming twice must not double-arrive (RecordExpectedArrival's own
	// already-arrived refusal, exercised through the resume path).
	if _, err := ResumeCheckIn(st, "site-a", 1, trip.TripID, nil, 0, "default"); err == nil {
		t.Fatal("resuming an already-arrived trip should refuse")
	}
}

// TestDispatchRefusesDestinationWithoutDeadline is the C7-architecture-
// review fix: a trip with a destination is an inbound EXPECTATION, but C7's
// scanner selects on DeadlineMinutes != nil (internal/deadline/scan.go) — a
// destination-without-deadline trip would enter OpenExpectations yet be
// silently skipped by the scan: dispatched, no alert promise, and no signal
// anywhere that none exists. A degenerate trip (BOTH nil, §10's local
// check-in) must remain unaffected.
func TestDispatchRefusesDestinationWithoutDeadline(t *testing.T) {
	st := newDomainTestStore(t)
	driver, _ := seedTwoPersons(t, st, "site-a")
	dest := "site-b"

	_, err := Dispatch(st, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "T12", DriverPersonID: driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "T12"), DriverPersonID: driver.PersonID,
		DestinationSite: &dest, // DeadlineMinutes deliberately nil
	}, 0, "default")
	if err == nil {
		t.Fatal("a trip with a destination but no deadline should be refused — it would enter OpenExpectations with no alert promise and no signal that none exists")
	}

	trips, err := Trips(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 0 {
		t.Fatalf("a refused dispatch must not author a trip: %+v", trips)
	}

	// The degenerate local check-in (both nil) must remain unaffected.
	if _, err := DispatchLocalCheckIn(st, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRK-14", DriverPersonID: driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK-14"), DriverPersonID: driver.PersonID,
	}, 0, "default"); err != nil {
		t.Fatalf("a degenerate trip (no destination, no deadline) must still be allowed: %v", err)
	}
}

// --- U2: CheckOutVisits, the batched REQUEST ---------------------------

// seedCrewOnSite checks n people onto siteID in one gate check-in and
// returns their open visit ids.
func seedCrewOnSite(t *testing.T, st *store.Store, siteID string, n int) []string {
	t.Helper()
	var slots []PersonSlot
	for i := 0; i < n; i++ {
		p, err := CreatePerson(st, siteID, 1, fmt.Sprintf("Crew %02d", i), nil, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		slots = append(slots, PersonSlot{ExistingID: p.PersonID})
	}
	if _, _, err := GateCheckIn(st, siteID, 1, GateCheckInInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "SHUTTLE", Driver: slots[0], Occupants: slots[1:],"⟧
		VehicleID: w7SeedVehicleID(t, st, siteID, "SHUTTLE"), Driver: slots[0], Occupants: slots[1:],
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	open, err := OpenVisitsAt(st, siteID)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != n {
		t.Fatalf("setup: %d open visits, want %d", len(open), n)
	}
	var ids []string
	for _, v := range open {
		ids = append(ids, v.VisitID)
	}
	return ids
}

// TestCheckOutVisitsAuthorsOneEventPerPerson is the whole point of the
// batch: it batches the REQUEST, never the events. A batch of N must be
// indistinguishable, in the log and therefore to every peer, from N
// separate check-outs.
func TestCheckOutVisitsAuthorsOneEventPerPerson(t *testing.T) {
	st := newDomainTestStore(t)
	ids := seedCrewOnSite(t, st, "site-a", 7)

	n, err := CheckOutVisits(st, "site-a", 1, ids, nil, 0, "default")
	if err != nil {
		t.Fatalf("batch check-out: %v", err)
	}
	if n != len(ids) {
		t.Fatalf("batch reported %d check-outs, want %d", n, len(ids))
	}

	checkouts, err := VisitCheckouts(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(checkouts) != len(ids) {
		t.Fatalf("%d visit_checked_out rows for a batch of %d — the events must not be batched, only the request", len(checkouts), len(ids))
	}
	got := map[string]bool{}
	for _, c := range checkouts {
		got[c.VisitID] = true
		if c.SiteID != "site-a" {
			t.Errorf("check-out row for %s is attributed to %s", c.VisitID, c.SiteID)
		}
	}
	for _, id := range ids {
		if !got[id] {
			t.Errorf("no check-out event for visit %s", id)
		}
	}
	open, err := OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 0 {
		t.Fatalf("%d visits still open after the batch", len(open))
	}
}

// TestCheckOutVisitsRefusesTheWholeBatchRatherThanHalfApplyingIt.
// Which direction this fails in is the point. Refusing leaves people
// recorded on site who the operator meant to remove — the muster
// over-reports, which wastes search time but never hides a live person.
// Applying half and reporting "some worked" would leave the operator
// guessing which half, and guessing is what this system never does.
func TestCheckOutVisitsRefusesTheWholeBatchRatherThanHalfApplyingIt(t *testing.T) {
	st := newDomainTestStore(t)
	ids := seedCrewOnSite(t, st, "site-a", 5)

	cases := []struct {
		name string
		ids  []string
		want string
	}{
		{"an unknown visit id", append(append([]string{}, ids...), "site-a-visit-nope"), "not known at this site"},
		{"an already-closed visit", ids, "already checked out"}, // closed below
		{"the same visit twice", []string{ids[0], ids[0]}, "named twice"},
		{"an empty id", []string{ids[0], ""}, "empty visit_id"},
		{"nothing selected", nil, "no visits selected"},
	}

	// Close one visit so the "already checked out" case is real.
	if err := CheckOutVisit(st, "site-a", 1, ids[4], nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	before, err := OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 4 {
		t.Fatalf("precondition: %d open visits, want 4", len(before))
	}
	baseline, err := VisitCheckouts(st)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, err := CheckOutVisits(st, "site-a", 1, tc.ids, nil, 0, "default")
			if err == nil {
				t.Fatalf("batch with %s was accepted (%d check-outs) — it must refuse and explain", tc.name, n)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("refusal does not name the problem (%q): %v", tc.want, err)
			}
			if !strings.Contains(err.Error(), "nothing was recorded") && len(tc.ids) > 0 {
				t.Errorf("refusal does not say that nothing happened: %v", err)
			}
			after, err := VisitCheckouts(st)
			if err != nil {
				t.Fatal(err)
			}
			if len(after) != len(baseline) {
				t.Fatalf("a refused batch authored %d event(s) anyway — all or nothing", len(after)-len(baseline))
			}
		})
	}
}

// TestCheckOutVisitsRefusesAnotherSitesVisit inherits CheckOutVisit's own
// own-site guard (§8.1) rather than re-implementing it, and proves the
// batch path did not open a hole around it.
func TestCheckOutVisitsRefusesAnotherSitesVisit(t *testing.T) {
	st := newDomainTestStore(t)
	ids := seedCrewOnSite(t, st, "site-a", 2)
	if _, err := CheckOutVisits(st, "site-b", 1, ids, nil, 0, "default"); err == nil {
		t.Fatal("site-b was allowed to batch-check-out site-a's visits")
	}
	open, err := OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 {
		t.Fatalf("%d visits closed by a foreign site's batch", 2-len(open))
	}
}
