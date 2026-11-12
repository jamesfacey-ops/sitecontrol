// trip_fold_driver_union_test.go — repair-queue row M (S123).
//
// THE GUARD. applyTripDispatched (trip.go) unions the driver into the
// occupant set before writing trip_occupant:
//
//	occupants := append([]string{p.DriverPersonID}, p.OccupantPersonIDs...)
//
// Its own comment states why: "§7: a label-only driver would vanish from
// every muster — enforced here as well as at authoring, so an
// injected/foreign trip event cannot produce a driverless occupant set."
//
// THE GAP THIS FILE CLOSES. Deleting that union produced exactly the
// driverless occupant set §7 forbids, on 131 trips, and 451 domain tests
// did not notice. The reason is specific and worth stating, because it is a
// reusable trap rather than an oversight:
//
//   - TestDispatchAloneChecksOccupantsOutOfOriginOnSiteList asserts the
//     right property and quotes the same §7 sentence — but it builds its
//     trip with Dispatch, and the AUTHORING path has already put the driver
//     into OccupantPersonIDs in the payload. By the time the fold runs, its
//     append is a duplicate that INSERT OR IGNORE discards. That test proves
//     the authoring layer while citing the fold layer, and it passes with
//     the fold-side union deleted.
//   - The fixtures that DO reach the fold with a driverless payload are all
//     self-comparisons (scoped versus wholesale recompute, twin soup,
//     round-trip). A mutation inside the fold is applied to BOTH sides of
//     every one of those comparisons, so it cancels and they stay green.
//
// So the gap was not "nobody tested it". It was "the layer under test and
// the layer asserted were different layers", which reads as coverage from
// every angle except the one that matters.
//
// THE SHAPE, per the S108 repair spec and modelled on
// TestCheckoutFoldNeverClosesAForeignSitesVisit (checkin_test.go), which
// does the same job for the check-out fold's site scoping: apply ONE RAW
// trip_dispatched envelope whose occupant_person_ids OMITS
// driver_person_id — which is precisely what an injected or foreign event
// looks like, and what no authored event can ever look like — then assert
// the fold put the driver in trip_occupant anyway. It must NOT route
// through Dispatch: that is the vacuity above, reintroduced.
package domain

import (
	"testing"

	"sitecontrol/internal/store"
)

// TestTripFoldUnionsTheDriverIntoAForeignEventsOccupantSet is row M.
//
// The `applied` check is not decoration. A silently-dropped envelope (a
// watermark that already covered this seq, a schema-version refusal) would
// leave trip_occupant empty and the assertion below would then be asserting
// nothing at all — a pass that never had a chance of being a fail. So the
// envelope is proven to have landed BEFORE its effect is read.
func TestTripFoldUnionsTheDriverIntoAForeignEventsOccupantSet(t *testing.T) {
	st := newDomainTestStore(t)

	const (
		tripID    = "site-b-trip-driverless-payload"
		driverID  = "site-b-person-drv-alpha"
		riderID   = "site-b-person-psgr-bravo"
		vehicleID = "site-b-vehicle-injected"
	)

	// A trip_dispatched whose occupant list names the passenger ONLY. No
	// authoring path in this module can mint this payload — tripauthor.go
	// prepends the driver before it authors — which is exactly why it is
	// the right shape to inject: it exercises the fold-side union while it
	// is genuinely load-bearing, rather than while it is a no-op duplicate.
	payload := mustJSON(t, TripDispatched{
		Type:              "trip_dispatched",
		TripID:            tripID,
		VehicleID:         vehicleID,
		DriverPersonID:    driverID,
		OccupantPersonIDs: []string{riderID},
		// No destination, no deadline: a degenerate trip, so this test says
		// nothing about expectations, alerts or arrivals. The narrower the
		// fixture, the fewer other guards can accidentally carry it.
	})

	applied, err := st.Apply(store.Envelope{
		SiteID: "site-b", NodeEpoch: 1, Seq: 1,
		SchemaVersion: store.SchemaVersion, OriginAuth: "site-b", Payload: payload,
	})
	if err != nil {
		t.Fatalf("apply injected trip_dispatched: %v", err)
	}
	if !applied {
		t.Fatal("the injected trip_dispatched was not applied at all (watermark issue, not the guard under test) — every assertion below would be vacuous")
	}

	occupants, err := TripOccupantsByTrip(st, tripID)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, o := range occupants {
		got[o.PersonID] = true
	}

	if !got[driverID] {
		t.Fatalf("§7 violated by the fold: a trip_dispatched whose occupant list omitted the driver produced a DRIVERLESS occupant set %v for trip %s. The driver is always an occupant row — a label-only driver would vanish from every muster, which is the one list that has to be right when a site is evacuated.", occupants, tripID)
	}
	// Non-vacuity in the other direction: the union must ADD the driver, not
	// REPLACE the payload's occupants. A fold that wrote only the driver
	// would satisfy the assertion above and lose the passenger — the same
	// §7 harm, wearing the other face.
	if !got[riderID] {
		t.Fatalf("the payload's own occupant %s is missing from %v — the driver union must add to the occupant set, never stand in for it", riderID, occupants)
	}
	if len(occupants) != 2 {
		t.Fatalf("trip_occupant = %v, want exactly the driver and the one payload occupant", occupants)
	}
}

// TestTripFoldDoesNotDoubleWriteADriverAlreadyInThePayload is the partner
// case, and it is what makes the union's IDEMPOTENCE explicit rather than
// incidental.
//
// It is also the executable statement of why the authored-path coverage is
// vacuous: on the shape Dispatch actually produces — driver already inside
// occupant_person_ids — the fold's append is a duplicate, INSERT OR IGNORE
// swallows it, and the resulting rows are identical whether the union runs
// or not. Anyone reading only the authored-path test cannot see that; this
// test says it in one assertion.
func TestTripFoldDoesNotDoubleWriteADriverAlreadyInThePayload(t *testing.T) {
	st := newDomainTestStore(t)

	const (
		tripID    = "site-b-trip-authored-shape"
		driverID  = "site-b-person-drv-alpha"
		riderID   = "site-b-person-psgr-bravo"
		vehicleID = "site-b-vehicle-injected"
	)

	// The AUTHORED shape: the driver is already first in the occupant list,
	// exactly as tripauthor.go writes it.
	payload := mustJSON(t, TripDispatched{
		Type:              "trip_dispatched",
		TripID:            tripID,
		VehicleID:         vehicleID,
		DriverPersonID:    driverID,
		OccupantPersonIDs: []string{driverID, riderID},
	})

	applied, err := st.Apply(store.Envelope{
		SiteID: "site-b", NodeEpoch: 1, Seq: 1,
		SchemaVersion: store.SchemaVersion, OriginAuth: "site-b", Payload: payload,
	})
	if err != nil {
		t.Fatalf("apply injected trip_dispatched: %v", err)
	}
	if !applied {
		t.Fatal("the injected trip_dispatched was not applied at all (watermark issue, not the guard under test)")
	}

	occupants, err := TripOccupantsByTrip(st, tripID)
	if err != nil {
		t.Fatal(err)
	}
	if len(occupants) != 2 {
		t.Fatalf("trip_occupant = %v (%d rows), want exactly 2 — the driver union must not produce a second row for a driver the payload already named", occupants, len(occupants))
	}
	seen := map[string]int{}
	for _, o := range occupants {
		seen[o.PersonID]++
	}
	if seen[driverID] != 1 || seen[riderID] != 1 {
		t.Fatalf("trip_occupant = %v, want one row each for %s and %s", occupants, driverID, riderID)
	}
}
