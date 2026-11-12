// return_manifest_test.go — per-person return disposition (James,
// 2026-08-20): a returning vehicle's occupant set is a list where each row
// has its own outcome, never a whole-vehicle group action. A person marked
// "stayed behind" is simply an occupant the arrival's visit set omits — NO
// new event type (TripArrival.Visits was always a per-person list) — and
// who is still out at a pad stays answerable via OutAtUncontrolled.
package domain

import (
	"testing"

	"sitecontrol/internal/store"
)

// --- per-person disposition on a controlled trip's arrival ---

func TestExpectedArrivalExcludingChecksInOnlyReturnees(t *testing.T) {
	w := newC6World(t)
	third, err := CreatePerson(w.a, "site-a", 1, "Casey Crew", &w.company.CompanyID, &w.trade.TradeID, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	dest, deadline := "site-b", int64(60)
	trip, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "BUS 7", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "BUS 7"), DriverPersonID: w.driver.PersonID,
		OccupantPersonIDs: []string{w.passenger.PersonID, third.PersonID},
		DestinationSite:   &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w.syncBFromA()

	if _, err := RecordExpectedArrivalExcluding(w.b, "site-b", 1, trip.TripID,
		[]string{third.PersonID}, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}

	open := openVisitsAt(t, w.b, "site-b")
	if len(open) != 2 {
		t.Fatalf("%d open visits at site-b, want 2 — the stayed person must not be checked in", len(open))
	}
	for _, v := range open {
		if v.PersonID == third.PersonID {
			t.Fatalf("the stayed person %s has an open visit at site-b — they are ON the muster of a site they never reached", third.PersonID)
		}
	}
	if s := stateOf(t, w.b, trip.TripID); s.State != TripStateArrived {
		t.Fatalf("trip state = %s, want arrived — the vehicle DID arrive; only the disposition is per person", s.State)
	}
}

func TestExpectedArrivalExcludingRefusesNonOccupant(t *testing.T) {
	w := newC6World(t)
	dest, deadline := "site-b", int64(60)
	trip, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "BUS 7", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "BUS 7"), DriverPersonID: w.driver.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w.syncBFromA()
	if _, err := RecordExpectedArrivalExcluding(w.b, "site-b", 1, trip.TripID,
		[]string{"site-a-person-doesnotexist"}, nil, 0, "default"); err == nil {
		t.Fatal("marking a non-occupant as stayed must be refused (the F4 typo posture)")
	}
	if s := stateOf(t, w.b, trip.TripID); s.State != TripStateOpen {
		t.Fatalf("trip state = %s after a refusal, want still open — nothing may have been authored", s.State)
	}
}

func TestExpectedArrivalExcludingEveryoneRefused(t *testing.T) {
	w := newC6World(t)
	dest, deadline := "site-b", int64(60)
	trip, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "BUS 7", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "BUS 7"), DriverPersonID: w.driver.PersonID,
		OccupantPersonIDs: []string{w.passenger.PersonID},
		DestinationSite:   &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w.syncBFromA()
	if _, err := RecordExpectedArrivalExcluding(w.b, "site-b", 1, trip.TripID,
		[]string{w.driver.PersonID, w.passenger.PersonID}, nil, 0, "default"); err == nil {
		t.Fatal("an arrival with every occupant marked stayed must be refused — nobody arrived means no arrival")
	}
	if s := stateOf(t, w.b, trip.TripID); s.State != TripStateOpen {
		t.Fatalf("trip state = %s after a refusal, want still open", s.State)
	}
}

// --- the operator's scenario: uncontrolled round trip with a drop-off ---

// A bus leaves the origin for the Conneco pad with three people, comes back
// with two. The return is a per-person manifest over the run: the two who
// returned check in (default), Casey stays (one click). Casey is OFF the
// origin muster but distinguishable from a plain check-out — they are on a
// Conneco run's occupant list — and OutAtUncontrolled names them at the pad.
// Later Casey comes back on a DIFFERENT vehicle through the ordinary gate
// check-in, and the out-at-pad record clears. Fold state is verified under
// every permuted cross-site delivery order at the mid-point.
func TestUncontrolledRunReturnManifestAndOutAtPad(t *testing.T) {
	w := newC6World(t)
	casey, err := CreatePerson(w.a, "site-a", 1, "Casey Crew", &w.company.CompanyID, &w.trade.TradeID, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	dale, err := CreatePerson(w.a, "site-a", 1, "Dale Dayworker", &w.company.CompanyID, &w.trade.TradeID, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// Everyone on site at the origin via the ordinary degenerate check-in.
	walkIn, err := DispatchLocalCheckIn(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "gate walk-in", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "gate walk-in"), DriverPersonID: w.driver.PersonID,
		OccupantPersonIDs: []string{w.passenger.PersonID, casey.PersonID, dale.PersonID},
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordExpectedArrival(w.a, "site-a", 1, walkIn.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// Dale simply checks out at the gate — the contrast case for
	// distinguishability: off site with NO run membership.
	for _, v := range openVisitsAt(t, w.a, "site-a") {
		if v.PersonID == dale.PersonID {
			if err := CheckOutVisit(w.a, "site-a", 1, v.VisitID, nil, 0, "default"); err != nil {
				t.Fatal(err)
			}
		}
	}

	// The run: BUS 7 to the Conneco pad with driver + passenger + Casey.
	pad := "conneco"
	run, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "BUS 7", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "BUS 7"), DriverPersonID: w.driver.PersonID,
		OccupantPersonIDs:       []string{w.passenger.PersonID, casey.PersonID},
		DestinationUncontrolled: &pad,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// The return manifest at the origin: Casey stayed, the rest check in.
	if _, err := RecordExpectedArrivalExcluding(w.a, "site-a", 1, run.TripID,
		[]string{casey.PersonID}, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}

	// A site-b event gives the permutation harness a second stream, so the
	// manifest fold is interleaved against genuine cross-site traffic.
	if _, err := CreateCompany(w.b, "site-b", 1, "Peer Contracting", 0, "default"); err != nil {
		t.Fatal(err)
	}

	// Mid-point invariants, under EVERY permuted delivery order.
	replayAllOrders(t, w.events(w.a, "site-a"), w.events(w.b, "site-b"), func(t *testing.T, st *store.Store) {
		open := openVisitsAt(t, st, "site-a")
		if len(open) != 2 {
			t.Fatalf("%d open visits at site-a, want 2 (driver + passenger back)", len(open))
		}
		for _, v := range open {
			if v.PersonID == casey.PersonID || v.PersonID == dale.PersonID {
				t.Fatalf("%s has an open visit — they are off site", v.PersonID)
			}
		}
		if s := stateOf(t, st, run.TripID); s.State != TripStateArrived {
			t.Fatalf("run state = %s, want arrived (the bus came back)", s.State)
		}
		outs, err := OutAtUncontrolled(st)
		if err != nil {
			t.Fatal(err)
		}
		if len(outs) != 1 || outs[0].Pad != "conneco" || outs[0].Vehicle != "BUS 7" ||
			len(outs[0].Persons) != 1 || outs[0].Persons[0].PersonID != casey.PersonID {
			t.Fatalf("OutAtUncontrolled = %+v, want exactly Casey out at conneco via BUS 7", outs)
		}
		// Dale — a plain check-out — must NOT read as out at any pad.
		for _, o := range outs {
			for _, p := range o.Persons {
				if p.PersonID == dale.PersonID {
					t.Fatalf("a plain gate check-out reads as out at a pad: %+v", outs)
				}
			}
		}
	})

	// Later, Casey comes back on a DIFFERENT vehicle via the ordinary gate
	// check-in — nothing requires returning the way they left.
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
	outs, err := OutAtUncontrolled(w.a)
	if err != nil {
		t.Fatal(err)
	}
	if len(outs) != 0 {
		t.Fatalf("OutAtUncontrolled after Casey's return = %+v, want empty", outs)
	}
}

// --- both directions dispatch; return legs key differently ---

// Each direction of travel is its own trip (operator ruling): the return leg
// is authored at the other site with ITS origin, so the correlation key
// (vehicleNorm, origin) differs and a vehicle cycling between the posts
// never collides with its own earlier legs. This also drives dispatch from
// the site-b side — the direction most testing has not exercised.
func TestReturnLegAuthoredAtPeerKeysDifferently(t *testing.T) {
	w := newC6World(t)
	dest, deadline := "site-b", int64(60)
	out, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ UN-ROUTED. The §47 route below is DISCHARGED and is
		// PRESERVED VERBATIM UNDERNEATH rather than deleted — it is HISTORY, not a live routing.
		// §50 rules that the ARRIVAL half receives the same transformation via the same helper;
		// this pass applied it at this fixture's `RecordUnsolicitedArrival` call, so the arrival now
		// carries a vehicle RECORD id, `vehicleNumberSnapshot` populates `trip_arrival.vehicle_entered`
		// again, and the correlation this fixture asserts is TRUE for the reason it always meant.
		// ⟦was ROUTED — §47 condition 3, 2026-09-03 — DISCHARGED by §50⟧ NOT RE-SEEDED. Re-seeding the TRIP half
		// makes this fixture's arrival assertion FALSE, not merely uncompilable.
		// `W7-4(d)(i)` re-typed `RecordUnsolicitedArrival`'s FOURTH POSITIONAL PARAMETER from
		// the number-as-typed to a vehicle RECORD id WITHOUT changing arity or type
		// (tripauthor.go, its own ⟦was: "…, vehicleEntered, originSite string, …"⟧), so the
		// unsolicited call in this fixture still COMPILES while passing a number.
		// ⚠ MEASURED on production code alone, no test file involved:
		//     number as the 4th arg -> vehicle_id="TRUCK 12"  snapshot=<nil>  disposition=PLAIN
		//     record id as the 4th arg -> vehicle_id="site-b-vehicle-…"  snapshot="TRUCK 12"  CORRELATED
		// `vehicleNumberSnapshot` finds no record for the number, so the arrival's snapshot is
		// NIL and it contributes no matching key (trip.go's own comment does the rest).
		// Repairing the arrival half is ONE root cause across 27 call sites and is the PLAN
		// OWNER's. See reports/window-build/RESEED-164.md §3.7.
		// ⟦was: "VehicleEntered: "TRUCK 12", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "TRUCK 12"), DriverPersonID: w.driver.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w.syncBFromA()
	if _, err := RecordExpectedArrival(w.b, "site-b", 1, out.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// The return leg: SITE-B dispatches, origin site-b, same vehicle.
	back := "site-a"
	ret, err := Dispatch(w.b, "site-b", 1, DispatchInput{
		// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ UN-ROUTED. The §47 route below is DISCHARGED and is
		// PRESERVED VERBATIM UNDERNEATH rather than deleted — it is HISTORY, not a live routing.
		// §50 rules that the ARRIVAL half receives the same transformation via the same helper;
		// this pass applied it at this fixture's `RecordUnsolicitedArrival` call, so the arrival now
		// carries a vehicle RECORD id, `vehicleNumberSnapshot` populates `trip_arrival.vehicle_entered`
		// again, and the correlation this fixture asserts is TRUE for the reason it always meant.
		// ⟦was ROUTED — §47 condition 3, 2026-09-03 — DISCHARGED by §50⟧ NOT RE-SEEDED — same cause as above:
		// the arrival half of this fixture passes a NUMBER where `W7-4(d)(i)` now wants a
		// vehicle RECORD id, so re-seeding the trip half makes the assertion FALSE.
		// ⟦was: "VehicleEntered: "TRUCK 12", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.b, "site-b", "TRUCK 12"), DriverPersonID: w.driver.PersonID,
		DestinationSite: &back, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// Sync site-b's stream into site-a's store.
	for _, ev := range w.events(w.b, "site-b") {
		if _, err := w.a.Apply(ev); err != nil {
			t.Fatal(err)
		}
	}
	// site-a sees exactly one inbound expectation: the return leg.
	exps, err := OpenExpectations(w.a, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(exps) != 1 || exps[0].Trip.TripID != ret.TripID {
		t.Fatalf("site-a inbound board = %+v, want exactly the return leg %s", exps, ret.TripID)
	}
	// An unsolicited arrival at site-a for (TRUCK 12, origin site-b) has
	// exactly one candidate — the return leg; the outbound leg keys under
	// origin site-a AND is already bound. No ambiguity, no collision.
	// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ ⟦was: "arr, err := RecordUnsolicitedArrival(w.a, "site-a", 1, "truck 12", "site-b","⟧
	arr, err := RecordUnsolicitedArrival(w.a, "site-a", 1, w7SeedVehicleID(t, w.a, "site-a", "truck 12"), "site-b",
		[]string{w.driver.PersonID}, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	d := dispositionOf(t, w.a, arr.ArrivalID)
	if d.Disposition != ArrivalCorrelated || d.TripID == nil || *d.TripID != ret.TripID {
		t.Fatalf("return-leg arrival disposition = %+v, want correlated to %s", d, ret.TripID)
	}
}

// --- the resume-hazard regression ---

// An open uncontrolled run is a crew recorded OUT at a pad — it must never
// appear in IncompleteCheckIns, whose one-click "resume" would check the
// whole Conneco crew back onto the origin muster. (Both shapes share
// destination_site NULL, so before this fix the filter matched both.)
func TestIncompleteCheckInsExcludesUncontrolledRuns(t *testing.T) {
	w := newC6World(t)
	pad := "conneco"
	if _, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "BUS 7", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "BUS 7"), DriverPersonID: w.driver.PersonID,
		DestinationUncontrolled: &pad,
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// A genuinely crashed check-in (trip authored, arrival never was) must
	// still be listed — the row-31 recovery this list exists for.
	stuck, err := DispatchLocalCheckIn(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "gate walk-in", DriverPersonID: w.passenger.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "gate walk-in"), DriverPersonID: w.passenger.PersonID,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	inc, err := IncompleteCheckIns(w.a, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(inc) != 1 || inc[0].TripID != stuck.TripID {
		t.Fatalf("IncompleteCheckIns = %+v, want exactly the crashed degenerate trip %s — an open uncontrolled run must NOT be offered for one-click resume", inc, stuck.TripID)
	}
}
