// stale_peer_visit_test.go — F-T2M-1 (t2-muster, 2026-08-20): a stale open
// PEER-site visit must never suppress a pad listing. A visit row at site S
// is only ever closed by S's own stream (trip.go dispatch fold closes
// `WHERE site_id = env.SiteID`; check-out likewise), so this site's replica
// of a peer's open visit can be stale for the whole length of a partition —
// or forever, when the peer check-out was simply never recorded. Reading
// that row as "demonstrably back right now" (old rule 3) put a person who
// is out at a pad in NO muster category at either site: on-site false,
// at-pad false, not-tracked false. The one direction the away view's design
// promised could not happen.
//
// The fixed rule 3: an open visit counts only at the RUN'S ORIGIN site.
// That set is provably post-run — the run's own dispatch fold closed every
// origin visit its occupants held, inside the transaction that recorded the
// run, so any replica containing the run contains those closures; an open
// origin visit for an occupant therefore postdates the run on the origin's
// own stream (same-stream order, §8.3 — never a cross-site comparison). An
// open visit anywhere ELSE is unorderable against the run and its openness
// is unverifiable across a partition, so it is not evidence — the person
// stays listed. Every case this changes errs toward LISTING.
package domain

import (
	"testing"

	"sitecontrol/internal/store"
)

func padListed(outs []UncontrolledOut, personID string) (string, bool) {
	for _, o := range outs {
		for _, p := range o.Persons {
			if p.PersonID == personID {
				return o.Pad, true
			}
		}
	}
	return "", false
}

// The reproduction, verbatim from the finding: Pat checks in at B (and that
// replicates to A), the link drops, Pat checks out at B (stranded), drives
// to A, checks in at A's gate, and goes out on a pad run the bus returns
// from without them. Mid-partition, site A must list Pat at the pad — under
// the old rule 3 Pat was in no category at all.
func TestStalePeerVisitMustNotSuppressPadListing(t *testing.T) {
	w := newC6World(t)
	pat := w.passenger.PersonID
	w.syncBFromA() // B knows the roster

	// Pat checks in at site-b; those events replicate to A before the cut.
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
	prePartition := w.events(w.b, "site-b")
	for _, ev := range prePartition {
		if _, err := w.a.Apply(ev); err != nil {
			t.Fatal(err)
		}
	}

	// PARTITION. B records Pat's check-out — the event is stranded at B.
	for _, v := range openVisitsAt(t, w.b, "site-b") {
		if v.PersonID == pat {
			if err := CheckOutVisit(w.b, "site-b", 1, v.VisitID, nil, 0, "default"); err != nil {
				t.Fatal(err)
			}
		}
	}

	// Pat drives to A and checks in at the gate...
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
	// ...and goes out on a pad run; the bus returns without Pat.
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

	// Site A, mid-partition. Pat is at the pad; the sheet must say so.
	for _, v := range openVisitsAt(t, w.a, "site-a") {
		if v.PersonID == pat {
			t.Fatalf("Pat has an open site-a visit — the pad dispatch should have closed it")
		}
	}
	rows, err := NotTracked(w.a)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := func() (string, bool) {
		for _, r := range rows {
			for _, p := range r.Persons {
				if p.PersonID == pat {
					return r.TripID, true
				}
			}
		}
		return "", false
	}(); ok {
		t.Fatalf("Pat in NotTracked — a pad run records WHERE, this is the wrong category: %+v", rows)
	}
	outs, err := OutAtUncontrolled(w.a)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := padListed(outs, pat); !ok {
		t.Fatalf("F-T2M-1: Pat is at the pad and in NO muster category — the stale open site-b "+
			"visit (checkout stranded behind the partition) suppressed the listing: %+v", outs)
	} else if got != "conneco" {
		t.Fatalf("Pat listed at %q, want conneco", got)
	}
	// The driver came back on the bus (rule 1) — never listed.
	if _, ok := padListed(outs, w.driver.PersonID); ok {
		t.Fatalf("the returned driver is listed at the pad: %+v", outs)
	}

	// The link heals: B's stranded check-out replicates. Pat is STILL at the
	// pad — the listing must survive the heal (a closed peer visit is
	// unorderable and does not count; the documented asymmetry, unchanged).
	all := w.events(w.b, "site-b")
	for _, ev := range all[len(prePartition):] {
		if _, err := w.a.Apply(ev); err != nil {
			t.Fatal(err)
		}
	}
	outs, err = OutAtUncontrolled(w.a)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := padListed(outs, pat); !ok {
		t.Fatalf("Pat vanished from the pad list after the heal: %+v", outs)
	}

	// And the converged event set lists Pat under EVERY delivery order — the
	// fixed predicate is a function of the event set, not of arrival order.
	replayAllOrders(t, w.events(w.a, "site-a"), all, func(t *testing.T, st *store.Store) {
		outs, err := OutAtUncontrolled(st)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := padListed(outs, pat); !ok {
			t.Fatalf("converged replica does not list Pat at the pad: %+v", outs)
		}
	})
}

// The direction every changed case now errs (stated, pinned): a pad-run
// occupant who genuinely turns up at the PEER — recorded only as an
// unsolicited arrival there, the sole recordable path from an ungated pad —
// stays ON the origin's pad list while that peer visit is open, because a
// current peer visit and a stranded one are byte-identical in the replica
// and only ordering evidence may suppress. The person is never lost: the
// peer's own board carries them via its local open visit. Extra name on a
// rescue list, visible contradiction over omission — the design's stated
// asymmetry applied to the open interval too.
func TestPadOccupantSeenAtPeerStaysListedWhilePeerVisitOpen(t *testing.T) {
	w := newC6World(t)
	pat := w.passenger.PersonID
	pad := "conneco"
	if _, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "BUS 7", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "BUS 7"), DriverPersonID: w.driver.PersonID,
		OccupantPersonIDs:       []string{pat},
		DestinationUncontrolled: &pad,
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// Someone collects Pat from the pad; Pat turns up at site-b unannounced.
	// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ ⟦was: "if _, err := RecordUnsolicitedArrival(w.a, "site-b", 1, "PICKUP 1", "site-a","⟧
	// ⟦S121 ARMED, 2026-09-04⟧ Bound to a name rather than discarded, solely so
	// the §48 control below can read its correlation key. Assertions unchanged.
	arrPeer, err := RecordUnsolicitedArrival(w.a, "site-b", 1, w7SeedVehicleID(t, w.a, "site-b", "PICKUP 1"), "site-a",
		[]string{pat}, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// ⭐ ⟦S121 §48 CONTROL — repair queue row A, 2026-09-04⟧ S107 site A4.
	// S107 planted the pre-§50 literal "PICKUP 1" on the line above and this
	// test STAYED GREEN, correctly: "PICKUP 1" is deliberately not the
	// dispatched vehicle ("BUS 7"), so no bind is intended either way. The
	// green was right and unprotected — nothing below observes the correlation
	// key. See s121_arrival_key_control_test.go.
	s121RequireArrivalKeyedOnVehicleRecord(t, w.a, arrPeer, "PICKUP 1",
		"At THIS site the loss is that the PEER sighting stops being an arrival of a known vehicle: "+
			"this test's whole subject is that a peer-side arrival row is byte-identical to a stranded "+
			"one and may not suppress, and an arrival whose vehicle snapshot is NIL is a row §9.2 would "+
			"never compose — so the suppression claim would be judged against an impossible fixture.")
	outs, err := OutAtUncontrolled(w.a)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := padListed(outs, pat); !ok {
		t.Fatalf("open PEER visit suppressed the pad listing — unorderable against the run and "+
			"indistinguishable from a stranded record; the safe direction is ON the list: %+v", outs)
	}
	// Pat is simultaneously on the peer's own on-site set — never lost.
	found := false
	for _, v := range openVisitsAt(t, w.a, "site-b") {
		if v.PersonID == pat {
			found = true
		}
	}
	if !found {
		t.Fatalf("Pat's unsolicited arrival opened no site-b visit")
	}

	// An open visit at the ORIGIN, by contrast, still suppresses even with
	// no trip reference to order by: the run's own fold closed every prior
	// origin visit, so an open one provably postdates the run.
	// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ ⟦was: "if _, err := RecordUnsolicitedArrival(w.a, "site-a", 1, "PICKUP 2", "site-b","⟧
	// ⟦S121 ARMED, 2026-09-04⟧ Bound to a name rather than discarded, solely so
	// the §48 control below can read its correlation key. Assertions unchanged.
	arrOrigin, err := RecordUnsolicitedArrival(w.a, "site-a", 1, w7SeedVehicleID(t, w.a, "site-a", "PICKUP 2"), "site-b",
		[]string{pat}, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// ⭐ ⟦S121 §48 CONTROL — repair queue row A, 2026-09-04⟧ S107 site A5.
	// S107 planted the pre-§50 literal "PICKUP 2" on the line above and this
	// test STAYED GREEN. The ORIGIN-side limb below is the sharper half: it
	// asserts SUPPRESSION, and suppression here is a function of the open
	// origin visit, not of the vehicle — so it cannot see the key at all.
	// See s121_arrival_key_control_test.go.
	s121RequireArrivalKeyedOnVehicleRecord(t, w.a, arrOrigin, "PICKUP 2",
		"At THIS site the loss is asymmetric and worse than at the peer limb: the assertion below is a "+
			"SUPPRESSION (Pat must drop off the pad list), and a suppression cannot be weakened by a "+
			"degraded arrival — a fixture that authored no usable vehicle key would still suppress, so "+
			"this limb would stay green on an arrival the production door could not have composed.")
	outs, err = OutAtUncontrolled(w.a)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := padListed(outs, pat); ok {
		t.Fatalf("Pat listed at the pad despite an open ORIGIN-site visit: %+v", outs)
	}
}
