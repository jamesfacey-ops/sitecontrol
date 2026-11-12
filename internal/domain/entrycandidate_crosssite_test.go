// entrycandidate_crosssite_test.go — FALSIFIABILITY-2.md M-B:
// findEntryCandidate (orientation.go) must never attach a
// trip_arrival_not_entered row from a DIFFERENT site to a completion at
// THIS site. The guard (`arr.SiteID != siteID`) is present in the code at
// this tip, but no existing fixture poses "held out at site X, completion
// recorded at site Y" — schema12_entry_replication_test.go, d98's own
// world and the cross-site warning tests all hold a person out and
// complete them at the SAME site. This file poses the two-site shape by
// name: Nora is dispatched to, and held out at, site-c ONLY. Site-b's
// store later replicates site-c's stream (as any peer's would, over time),
// so site-b's own database now contains Nora's site-c not-entered row.
// Completing Nora's orientation at site-b must find nothing to attach —
// she has never been aboard any site-b arrival.
//
// Reuses replicateStream (schema12_entry_replication_test.go) — same
// package, same "one peer's own view of one stream" primitive.
package domain

import "testing"

func TestFindEntryCandidateNeverBorrowsAnotherSitesHeldOutArrival(t *testing.T) {
	a := newDomainTestStore(t) // roster origin
	b := newDomainTestStore(t) // the site under test — Nora never arrives here
	c := newDomainTestStore(t) // the site Nora actually travels to and is held out at

	driver, err := CreatePerson(a, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	nora, err := CreatePerson(a, "site-a", 1, "Nora Newcomer", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	// Roster must reach both b and c before either can dispatch-read or
	// look Nora up — the same pattern as d98World.syncBFromA, applied to
	// two destinations instead of one.
	replicateStream(t, b, a, "site-a")
	replicateStream(t, c, a, "site-a")

	dest, deadline := "site-c", int64(120)
	trip, err := Dispatch(a, "site-a", 1, DispatchInput{
		VehicleID: w7SeedVehicleID(t, a, "site-a", "TRK-C1"), DriverPersonID: driver.PersonID,
		OccupantPersonIDs: []string{nora.PersonID},
		DestinationSite:   &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatalf("Dispatch site-a -> site-c: %v", err)
	}
	replicateStream(t, c, a, "site-a") // the dispatch event itself

	if _, err := RecordExpectedArrival(c, "site-c", 1, trip.TripID, nil, 0, "default", true); err != nil {
		t.Fatalf("RecordExpectedArrival at site-c: %v", err)
	}

	// PRECONDITION: Nora really is held out at site-c — findEntryCandidate
	// must find her own site's row.
	candAtC, err := findEntryCandidate(c, "site-c", nora.PersonID)
	if err != nil {
		t.Fatal(err)
	}
	if candAtC == nil {
		t.Fatal("PRECONDITION: Nora has no entry candidate at site-c, her own held-out site — fixture defect")
	}

	// site-c's own stream (the arrival AND the not-entered row) reaches
	// site-b's database the way any peer's eventually would.
	replicateStream(t, b, c, "site-c")

	// Nora was NEVER dispatched to, nor held out at, site-b. Her only
	// not-entered row anywhere belongs to site-c.
	candAtB, err := findEntryCandidate(b, "site-b", nora.PersonID)
	if err != nil {
		t.Fatal(err)
	}
	if candAtB != nil {
		t.Fatalf("findEntryCandidate(site-b, Nora) = %+v, want nil — it borrowed a DIFFERENT site's (site-c's) held-out arrival", candAtB)
	}

	// Full-stack proof: completing Nora's orientation at site-b (the
	// marking-surface shape — no arrival at site-b at all) must not open a
	// visit for her there.
	if _, err := CompleteOrientation(b, "site-b", 1, nora.PersonID, nil, 0, "default"); err != nil {
		t.Fatalf("CompleteOrientation(site-b, Nora): %v", err)
	}
	visits, err := OpenVisitsAt(b, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range visits {
		if v.PersonID == nora.PersonID {
			t.Fatalf("Nora Newcomer has an OPEN VISIT at site-b — minted from site-c's held-out arrival — having never come through site-b's gate: %+v", v)
		}
	}
}
