// away_guard_test.go — mutation guards for the two load-bearing conditions
// of the "hasn't come back" predicate that the round-2 mutation audit found
// undefended (2026-08-20): deleting either left the whole suite green, and
// each deletion drops a person who is still at the pad OFF the rescue list —
// the same direction as F35, through different doors.
package domain

import (
	"fmt"
	"testing"

	"sitecontrol/internal/store"
)

// Rule 2's origin guard (`vt.OriginSite == origin` in sightedSince): without
// it the predicate compares a PEER-stream trip's (node_epoch, seq) against
// the run's — a cross-site comparison that is meaningless per §8.3, because
// each site numbers its own stream independently. This test builds the
// concrete case the audit said no existing test could pose (both
// away-predicate tests were single-stream): a site-b trip that happens to
// carry a HIGHER (epoch, seq) than the site-a pad run while belonging
// entirely to Pat's past. Site-b's stream simply ran more events, so its
// counter is further along — that is all a cross-site key comparison ever
// measures. With the guard deleted, rule 2 reads Pat's closed pre-run
// site-b visit as "a check-in AFTER the run" and removes Pat, still at the
// pad, from every list.
func TestAwayGuardRejectsCrossStreamKeyComparison(t *testing.T) {
	w := newC6World(t)
	pat := w.passenger.PersonID
	w.syncBFromA() // B knows the roster

	// Site-b's stream runs ahead: unrelated reference-data traffic pushes
	// its sequence counter past anything site-a will author below.
	for i := 0; i < 8; i++ {
		if _, err := CreateCompany(w.b, "site-b", 1, fmt.Sprintf("Peer Contractor %d", i), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	// Pat's PAST at site-b: gate check-in (a b-origin trip whose source key
	// now exceeds the run's), then a recorded check-out. Closed visit,
	// b-origin trip reference — nothing about it is orderable against a
	// site-a run.
	bTrip, err := DispatchLocalCheckIn(w.b, "site-b", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "gate walk-in", DriverPersonID: pat,"⟧
		VehicleID: w7SeedVehicleID(t, w.b, "site-b", "gate walk-in"), DriverPersonID: pat,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordExpectedArrival(w.b, "site-b", 1, bTrip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	for _, v := range openVisitsAt(t, w.b, "site-b") {
		if v.PersonID == pat {
			if err := CheckOutVisit(w.b, "site-b", 1, v.VisitID, nil, 0, "default"); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Everything replicates — no partition in this scenario at all.
	bEvents := w.events(w.b, "site-b")
	for _, ev := range bEvents {
		if _, err := w.a.Apply(ev); err != nil {
			t.Fatal(err)
		}
	}

	// Pat travels to site-a, checks in, and goes out on a pad run the bus
	// returns from without them.
	aTrip, err := DispatchLocalCheckIn(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "gate walk-in", DriverPersonID: pat,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "gate walk-in"), DriverPersonID: pat,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordExpectedArrival(w.a, "site-a", 1, aTrip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	pad := "conneco"
	run, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "BUS 7", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "BUS 7"), DriverPersonID: w.driver.PersonID,
		OccupantPersonIDs:       []string{pat},
		DestinationUncontrolled: &pad,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordExpectedArrivalExcluding(w.a, "site-a", 1, run.TripID,
		[]string{pat}, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}

	// Precondition of the guard case, asserted so this test cannot rot into
	// one that passes vacuously: the b-trip's key really is numerically
	// higher than the run's. If harness event counts ever change and break
	// this, the test must be re-padded, not trusted.
	if !(bTrip.NodeEpoch > run.NodeEpoch ||
		(bTrip.NodeEpoch == run.NodeEpoch && bTrip.Seq > run.Seq)) {
		t.Fatalf("scenario precondition lost: b-trip key (%d,%d) is not numerically higher than "+
			"the run's (%d,%d) — the cross-stream comparison this test defends against would not "+
			"even fire; re-pad site-b's stream", bTrip.NodeEpoch, bTrip.Seq, run.NodeEpoch, run.Seq)
	}

	// Pat is at the pad. A higher-keyed PEER-stream trip in their past must
	// not read as "checked in after the run".
	outs, err := OutAtUncontrolled(w.a)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := padListed(outs, pat); !ok {
		t.Fatalf("Pat dropped from the pad list — a site-b trip's higher (epoch, seq) was compared "+
			"against a site-a run's (meaningless per §8.3, rule 2's origin guard): %+v", outs)
	}

	// And under every permuted delivery order of the two genuine streams.
	replayAllOrders(t, w.events(w.a, "site-a"), bEvents, func(t *testing.T, st *store.Store) {
		outs, err := OutAtUncontrolled(st)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := padListed(outs, pat); !ok {
			t.Fatalf("replica does not list Pat at the pad: %+v", outs)
		}
	})
}

// Rule 1's closure contract: a return recorded by the run's OWN manifest
// (visit.trip_id = run) counts open OR SINCE CLOSED — someone who came back
// on the bus and later checked out and went home is not at the pad, and
// re-listing them is the "rescue party for someone at home" error. The
// audit found the closed half unpinned: narrowing rule 1 to open manifest
// visits only left the suite green.
func TestAwayNotRelistedAfterManifestReturnAndLaterCheckout(t *testing.T) {
	w := newC6World(t)
	pat := w.passenger.PersonID
	pad := "conneco"
	run, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "BUS 7", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "BUS 7"), DriverPersonID: w.driver.PersonID,
		OccupantPersonIDs:       []string{pat},
		DestinationUncontrolled: &pad,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// The whole crew returns on the bus — Pat is on the return manifest.
	if _, err := RecordExpectedArrival(w.a, "site-a", 1, run.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// Later, Pat checks out and goes home; the manifest visit closes.
	for _, v := range openVisitsAt(t, w.a, "site-a") {
		if v.PersonID == pat {
			if err := CheckOutVisit(w.a, "site-a", 1, v.VisitID, nil, 0, "default"); err != nil {
				t.Fatal(err)
			}
		}
	}
	outs, err := OutAtUncontrolled(w.a)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := padListed(outs, pat); ok {
		t.Fatalf("Pat re-listed at the pad after a manifest return and a later check-out — rule 1 "+
			"must count closed manifest visits too: %+v", outs)
	}
}
