// away_predicate_test.go — the "hasn't come back" predicate behind
// OutAtUncontrolled (team-lead follow-up, 2026-08-20). An uncontrolled
// run's trip_state stays open forever by design, so the answer is derived
// from VISITS alone: recorded back iff (1) a visit from the run's own
// return manifest exists (open or closed), or (2) a visit tied to a LATER
// same-stream trip exists (same origin, higher (node_epoch, seq) — true
// authoring order, §8.3), or (3) an open visit exists anywhere right now.
// A wrong predicate here puts someone on a rescue list who is in the break
// room — or worse, leaves them off it.
package domain

import "testing"

// The flaw this iteration fixes, reproduced end-to-end: Casey stays at the
// pad, later walks back in through the ordinary gate check-in (a different
// vehicle), and later still checks OUT and goes home. Under the old
// predicate (manifest visits + open visits only) the check-out made Casey
// REAPPEAR on the pad list — a rescue party sent to Conneco for someone
// who is at home. Rule 2 fixes it: the closed visit is tied to a trip on
// the same stream with a higher source key, which is exact proof the
// check-in happened after the run.
func TestAwayNotRelistedAfterReturnAndLaterCheckout(t *testing.T) {
	w := newC6World(t)
	casey, err := CreatePerson(w.a, "site-a", 1, "Casey Crew", &w.company.CompanyID, &w.trade.TradeID, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// On site, out to the pad, stays there when the bus returns.
	pad := "conneco"
	run, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "BUS 7", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "BUS 7"), DriverPersonID: w.driver.PersonID,
		OccupantPersonIDs:       []string{casey.PersonID},
		DestinationUncontrolled: &pad,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordExpectedArrivalExcluding(w.a, "site-a", 1, run.TripID,
		[]string{casey.PersonID}, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// Casey comes back later on a different vehicle via the ordinary gate
	// check-in...
	van, err := DispatchLocalCheckIn(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "VAN 2", DriverPersonID: casey.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "VAN 2"), DriverPersonID: casey.PersonID,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordExpectedArrival(w.a, "site-a", 1, van.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// ...and later checks out and goes home.
	for _, v := range openVisitsAt(t, w.a, "site-a") {
		if v.PersonID == casey.PersonID {
			if err := CheckOutVisit(w.a, "site-a", 1, v.VisitID, nil, 0, "default"); err != nil {
				t.Fatal(err)
			}
		}
	}

	outs, err := OutAtUncontrolled(w.a)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range outs {
		for _, p := range o.Persons {
			if p.PersonID == casey.PersonID {
				t.Fatalf("Casey is re-listed as away at %s after a RECORDED return and a later check-out — the pad list would send a rescue party for someone at home: %+v", o.Pad, outs)
			}
		}
	}
}

// The deliberate asymmetry of rule 3, pinned — DIRECTION FLIPPED 2026-08-20
// (F35 / t2-muster F-T2M-1), on purpose, not to make a test pass:
//
// The OLD rule counted an open visit at ANY site as "recorded back", and
// this test's first assertion pinned that: it looked right because an open
// visit reads as "demonstrably on a site right now", so sending a rescue
// party for someone standing at the peer's gate seemed absurd.
//
// It was WRONG because a visit row at a site is only ever closed by that
// site's OWN stream (trip.go, applyTripDispatched; check-out likewise) —
// so this site's replica of a peer's open visit can be stale for the whole
// length of a partition, or forever if the peer check-out was never
// recorded, and a stranded peer row is BYTE-IDENTICAL to a current one.
// Result, reproduced (stale_peer_visit_test.go): a pad-run occupant with a
// stale open peer visit was in NO muster category at either site — the one
// omission this view exists to make impossible.
//
// The rule NOW: an open visit counts only at the run's ORIGIN, where it is
// provably post-run — the run's own dispatch fold closed every open origin
// visit its occupants held, inside the transaction that recorded the run,
// and origin visit rows are only mutated by the origin's contiguous
// stream, so any replica containing the run contains those closures
// (same-stream ordering, carried by the fold). A peer-side sighting
// therefore never suppresses, open or closed: the person collected from
// the pad who walks into the peer unannounced stays on the pad list — an
// EXTRA NAME BY DESIGN, beside the peer's own on-site board which still
// lists them via its local open visit. Erring toward listing is the
// deliberate direction; an extra name is a nuisance, a missing one is the
// failure this system exists to prevent.
func TestAwayConservativeWhenReturnUnorderable(t *testing.T) {
	w := newC6World(t)
	pad := "conneco"
	if _, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "BUS 7", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "BUS 7"), DriverPersonID: w.driver.PersonID,
		OccupantPersonIDs:       []string{w.passenger.PersonID},
		DestinationUncontrolled: &pad,
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// Someone collects the passenger from the pad and they turn up at
	// site-b unannounced — an unsolicited arrival (visit with no trip ref).
	// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ ⟦was: "if _, err := RecordUnsolicitedArrival(w.a, "site-b", 1, "PICKUP 1", "site-a","⟧
	// ⟦S121 ARMED, 2026-09-04⟧ The arrival is now BOUND to a name rather than
	// discarded, solely so the §48 control below can read its correlation key.
	// The assertions of this test are unchanged.
	arrPeer, err := RecordUnsolicitedArrival(w.a, "site-b", 1, w7SeedVehicleID(t, w.a, "site-b", "PICKUP 1"), "site-a",
		[]string{w.passenger.PersonID}, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// ⭐ ⟦S121 §48 CONTROL — repair queue row A, 2026-09-04⟧ S107 site A1.
	// S107 planted the pre-§50 literal "PICKUP 1" on the line above and this
	// test STAYED GREEN, correctly: "PICKUP 1" is deliberately not the
	// dispatched vehicle ("BUS 7"), so no bind is intended and the pad-listing
	// claim below holds either way. That is exactly why nothing here was
	// protected — the §50 transformation could be reverted at this site today
	// and the suite would say nothing. See s121_arrival_key_control_test.go.
	s121RequireArrivalKeyedOnVehicleRecord(t, w.a, arrPeer, "PICKUP 1",
		"At THIS site the loss is that the peer sighting stops being a sighting OF A VEHICLE: with a "+
			"nil snapshot and an id naming no record, the arrival this test uses to make the return "+
			"unorderable is no longer the arrival §9.2 would compose, so the conservative-listing claim "+
			"below is being judged against a fixture the production door cannot produce.")
	outs, err := OutAtUncontrolled(w.a)
	if err != nil {
		t.Fatal(err)
	}
	listed := func(outs []UncontrolledOut, id string) bool {
		for _, o := range outs {
			for _, p := range o.Persons {
				if p.PersonID == id {
					return true
				}
			}
		}
		return false
	}
	if !listed(outs, w.passenger.PersonID) {
		t.Fatalf("open PEER visit suppressed the pad listing — unorderable against the run, and "+
			"indistinguishable from a stranded record (F35/F-T2M-1); must stay listed: %+v", outs)
	}
	// The peer visit closes: still unorderable, still not proof — listed.
	for _, v := range openVisitsAt(t, w.a, "site-b") {
		if v.PersonID == w.passenger.PersonID {
			if err := CheckOutVisit(w.a, "site-b", 1, v.VisitID, nil, 0, "default"); err != nil {
				t.Fatal(err)
			}
		}
	}
	outs, err = OutAtUncontrolled(w.a)
	if err != nil {
		t.Fatal(err)
	}
	if !listed(outs, w.passenger.PersonID) {
		t.Fatalf("closed unorderable peer visit must NOT count as recorded back — the safe direction is back ON the list: %+v", outs)
	}
	// The driver, who never returned at all, is listed throughout.
	if !listed(outs, w.driver.PersonID) {
		t.Fatalf("the never-returned driver is off the pad list: %+v", outs)
	}
}
