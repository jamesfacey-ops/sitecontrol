// dispatch_uncontrolled_test.go — uncontrolled destinations (James,
// 2026-08-20): a truck can go to a pad this company does not control — no
// gate, no officer, nobody who can record an arrival. Such a run is RECORDED
// (destination + occupants, occupants checked out of the origin: who is out
// there must stay answerable) but is NOT an expectation: no deadline, no
// board row, no scan, and NEVER a correlation candidate — a promise nobody
// can close would only ever produce a false alarm, and a correlation bind to
// one would silence a real trip's alert.
//
// The deadline rule, refined from F4's blanket "destination ⇒ deadline" and
// enforced in BOTH directions: controlled destination REQUIRES a deadline
// (unchanged, F4); uncontrolled destination MUST NOT carry one (refused, not
// ignored).
package domain

import (
	"testing"

	"sitecontrol/internal/store"
)

// strPtr lives in muster_test.go.

// --- the deadline rule, both directions ---

func TestDispatchUncontrolledRefusesDeadline(t *testing.T) {
	w := newC6World(t)
	deadline := int64(60)
	if _, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRUCK 12", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "TRUCK 12"), DriverPersonID: w.driver.PersonID,
		DestinationUncontrolled: strPtr("conneco"), DeadlineMinutes: &deadline,
	}, 0, "default"); err == nil {
		t.Fatal("a deadline on an uncontrolled destination must be REFUSED — nobody there can record the arrival that would close it")
	}
}

func TestDispatchControlledStillRequiresDeadline(t *testing.T) {
	// F4's own direction, untouched by this change.
	w := newC6World(t)
	if _, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRUCK 12", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "TRUCK 12"), DriverPersonID: w.driver.PersonID,
		DestinationSite: strPtr("site-b"),
	}, 0, "default"); err == nil {
		t.Fatal("a controlled destination without a deadline must still be refused (F4)")
	}
}

func TestDispatchRefusesBothDestinations(t *testing.T) {
	w := newC6World(t)
	deadline := int64(60)
	if _, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRUCK 12", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "TRUCK 12"), DriverPersonID: w.driver.PersonID,
		DestinationSite: strPtr("site-b"), DestinationUncontrolled: strPtr("conneco"),
		DeadlineMinutes: &deadline,
	}, 0, "default"); err == nil {
		t.Fatal("a trip with both a controlled and an uncontrolled destination must be refused")
	}
}

func TestDispatchUncontrolledMembershipCheck(t *testing.T) {
	w := newC6World(t)
	// Non-nil known list, non-member: refused (the F4 typo shape).
	if _, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRUCK 12", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "TRUCK 12"), DriverPersonID: w.driver.PersonID,
		DestinationUncontrolled:       strPtr("connecoo"),
		KnownUncontrolledDestinations: []string{"conneco"},
	}, 0, "default"); err == nil {
		t.Fatal("an uncontrolled destination outside the configured list must be refused")
	}
	// Member: authors fine.
	if _, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRUCK 12", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "TRUCK 12"), DriverPersonID: w.driver.PersonID,
		DestinationUncontrolled:       strPtr("conneco"),
		KnownUncontrolledDestinations: []string{"conneco"},
	}, 0, "default"); err != nil {
		t.Fatalf("a configured uncontrolled destination must author: %v", err)
	}
}

// --- recording and the muster ---

// An uncontrolled run records the destination and occupants and checks the
// occupants out of the origin — they are genuinely not on site, so they are
// off the origin muster while gone — while producing NO inbound expectation
// anywhere.
func TestUncontrolledRunRecordsAndChecksOutOccupants(t *testing.T) {
	w := newC6World(t)
	w.checkInAtA() // driver + passenger on site at A
	if got := openVisitsAt(t, w.a, "site-a"); len(got) != 2 {
		t.Fatalf("precondition: %d open visits at site-a, want 2", len(got))
	}

	trip, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRUCK 12", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "TRUCK 12"), DriverPersonID: w.driver.PersonID,
		OccupantPersonIDs:       []string{w.passenger.PersonID},
		DestinationUncontrolled: strPtr("conneco"),
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	// Recorded: the trip row carries the uncontrolled destination and no
	// controlled one; the occupant rows exist (who is out there is
	// answerable).
	stored, err := TripByID(w.a, trip.TripID)
	if err != nil {
		t.Fatal(err)
	}
	if stored == nil || stored.DestinationUncontrolled == nil || *stored.DestinationUncontrolled != "conneco" {
		t.Fatalf("trip row = %+v, want destination_uncontrolled = conneco", stored)
	}
	if stored.DestinationSite != nil || stored.DeadlineMinutes != nil {
		t.Fatalf("trip row = %+v, want destination_site and deadline_minutes both NULL", stored)
	}
	occ, err := TripOccupantsByTrip(w.a, trip.TripID)
	if err != nil {
		t.Fatal(err)
	}
	if len(occ) != 2 {
		t.Fatalf("%d occupant rows, want 2 — the muster answer for the pad lives here", len(occ))
	}

	// Checked out: off the origin's open-visit set, hence off its muster.
	if got := openVisitsAt(t, w.a, "site-a"); len(got) != 0 {
		t.Fatalf("%d open visits at site-a after the uncontrolled dispatch, want 0 — they are genuinely not on site", len(got))
	}

	// Not an expectation: on no site's inbound board — not the origin's, not
	// the peer's, and not under the pad's own id either (the pad has no site).
	for _, siteID := range []string{"site-a", "site-b", "conneco"} {
		exps, err := OpenExpectations(w.a, siteID)
		if err != nil {
			t.Fatal(err)
		}
		if len(exps) != 0 {
			t.Fatalf("uncontrolled trip appears on %s's inbound board: %+v", siteID, exps)
		}
	}
}

// --- correlation exclusion, under permuted cross-site delivery ---

// An open uncontrolled run must never be consumed by unsolicited-arrival
// correlation: if TRUCK 12 is recorded as gone to the Conneco pad and a
// TRUCK 12 later shows up unannounced at site-b, binding the two would
// invent an arrival nobody at the pad could ever have contradicted — and
// with a real inter-site trip also open, the wrong bind would silence a
// genuinely overdue vehicle's alert. The uncontrolled trip shares the
// degenerate trips' structural exclusion (destination_site NULL), so the
// arrival stays PLAIN and the uncontrolled trip stays open, in every
// delivery order.
func TestUncontrolledTripNeverCorrelates(t *testing.T) {
	w := newC6World(t)
	trip, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⚠⚠ ⟦DISARMED — §47 re-seed, 2026-09-03⟧ THIS SITE IS RE-SEEDED AND THIS TEST WILL NOW
		// PASS VACUOUSLY. ITS GREEN IS NOT EVIDENCE. `W7-4(d)(i)` re-typed
		// `RecordUnsolicitedArrival`'s 4th positional parameter to a vehicle RECORD id without
		// changing arity, so the call below still compiles while passing a NUMBER — and a number
		// names no record, so the arrival's snapshot is nil and it can correlate with NOTHING.
		// ⚠ This test asserts the uncontrolled run stays `ArrivalPlain`. That is now true for a
		// reason with nothing to do with the rule under test: THE NEGATIVE CONTROL CANNOT FAIL.
		// This project has a produced instance of a dead guard passing `ok` in 0.003s against a
		// deliberately broken fixture; do not count this green.
		// Re-armed by repairing the arrival half — the PLAN OWNER's, one root cause across 27
		// call sites. See reports/window-build/RESEED-164.md §3.9.
		// ⭐ ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ RE-ARMED, AND THE RE-ARM IS PRODUCED, NOT
		// CLAIMED. The arrival below now carries a vehicle RECORD id, so its snapshot is populated
		// and it is a candidate again. THE NEGATIVE CONTROL DISCRIMINATES [MEASURED, plant P1 —
		// the same fixture with the ONE mutation this rule is about]:
		//     uncontrolled run  -> plain        (this test's fixture)
		//     controlled run    -> correlated   (the mutation)
		// A guard that reports the same disposition either way is dead; this one does not.
		// ⚠ The green below may now be counted. See reports/window-build/ARRIVAL-RESEED.md.
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRUCK 12", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "TRUCK 12"), DriverPersonID: w.driver.PersonID,
		DestinationUncontrolled: strPtr("conneco"),
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w.syncBFromA()
	// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ ⟦was: "arr, err := RecordUnsolicitedArrival(w.b, "site-b", 1, "truck 12", "site-a","⟧
	arr, err := RecordUnsolicitedArrival(w.b, "site-b", 1, w7SeedVehicleID(t, w.b, "site-b", "truck 12"), "site-a",
		[]string{w.driver.PersonID}, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	replayAllOrders(t, w.events(w.a, "site-a"), w.events(w.b, "site-b"), func(t *testing.T, st *store.Store) {
		d := dispositionOf(t, st, arr.ArrivalID)
		if d.Disposition != ArrivalPlain {
			t.Fatalf("unsolicited arrival disposition = %+v, want plain — an uncontrolled run is NEVER a correlation candidate", d)
		}
		if s := stateOf(t, st, trip.TripID); s.State != TripStateOpen || s.ArrivalID != nil {
			t.Fatalf("uncontrolled trip state = %+v, want open and unbound", s)
		}
	})
}
