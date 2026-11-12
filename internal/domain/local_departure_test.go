// local_departure_test.go — D-4 (JAMES-DECISIONS.md): "Remove local
// check-out. Every arrival and departure is a vehicle."
//
// The operating environment is the Arctic North Slope, just west of
// Deadhorse, in winter, on private property: no privately owned vehicles,
// no pedestrians. Every person's presence on site is bounded by a vehicle
// trip in and a vehicle trip out, so a departure with no vehicle and no
// destination is not a thing that happens here.
//
// The reason this is a DOMAIN test and not a template test is that the
// defect it closes was a button, but the failure mode is a raw POST. A trip
// with no destination, no deadline and no arrival is byte-identical to a
// check-in that crashed between its two Author calls (see GateCheckIn's
// CRASH WINDOW comment), which is exactly what IncompleteCheckIns selects —
// so a destination-less departure appeared on /gate behind a Resume button
// that would put the departed crew back on the muster, and made the printed
// muster read MAY BE INCOMPLETE. Removing the ambiguity at its root means
// only one of the two shapes can be AUTHORED from now on.
package domain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDispatchRefusesADepartureWithNoDestination is the guard itself. It
// asserts the refusal AND that nothing reached the log — an authoring-time
// refusal that had already written the event would be no guard at all.
func TestDispatchRefusesADepartureWithNoDestination(t *testing.T) {
	st := newDomainTestStore(t)
	driver, p1 := seedTwoPersons(t, st, "site-a")
	// ⟦LOCK — §62, 2026-09-03⟧ R3 — THE SEED GOES ABOVE THE WINDOW. See the superseded route at
	// the call below for the mechanism; this line is its remedy, and it must stay ABOVE the
	// `head` read or it becomes the very defect the route describes.
	vehicleID := w7SeedVehicleID(t, st, "site-a", "TRK-14")
	head, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Dispatch(st, "site-a", 1, DispatchInput{
		// ⚠ ⟦was ROUTED — §47 condition 3, 2026-09-03 — DISCHARGED by §62 class R3⟧ NOT
		// RE-SEEDED. `head` is read above and re-read below as "a refused dispatch moved the log
		// head". A vehicle record authors an event, so re-seeding here would move the head and
		// redden that assertion. PLAN OWNER's.
		//
		// ⟦LOCK — §62, 2026-09-03⟧ ⭐ CLASSIFIED R3 ON ITS OWN EVIDENCE — this file is one of the
		// four §62 bar 3 leaves deliberately un-pre-classified. The note names a pure
		// MEASURED-WINDOW mechanism and nothing else: no guard moved (D-4's "must record where it
		// is going" refusal is untouched by W7), and nothing here became unconstructible. The
		// assertion is still true and still needed, so the disposition is R3 and not R1 or R2 —
		// and R3's remedy applies without residue, because the window has a clear opening line
		// (`head`, above) with room in front of it. The seed is hoisted there; the refusal, its
		// literal ("where it is going"), the trip count and the head comparison are all UNTOUCHED.
		// ⟦was: "VehicleEntered:    \"TRK-14\","⟧
		VehicleID:         vehicleID,
		DriverPersonID:    driver.PersonID,
		OccupantPersonIDs: []string{p1.PersonID},
	}, 0, "default")
	if err == nil {
		t.Fatal("Dispatch authored a departure with no destination — D-4 says there is no such thing")
	}
	if !strings.Contains(err.Error(), "where it is going") {
		t.Fatalf("refusal does not say what is missing: %v", err)
	}
	trips, err := Trips(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 0 {
		t.Fatalf("a refused dispatch authored a trip: %+v", trips)
	}
	if h2, err := st.Watermark("site-a", 1); err != nil || h2 != head {
		t.Fatalf("a refused dispatch moved the log head %d → %d — something was written", head, h2)
	}
}

// TestDispatchLocalCheckInRefusesADestinationOrDeadline is the other half of
// the pair. The two doors are mutually exclusive by construction: Dispatch
// will not author a trip with no destination, and DispatchLocalCheckIn will
// not author one WITH a destination. Without this leg, "route departures
// through DispatchLocalCheckIn" would be an available shortcut that
// satisfied the test above.
func TestDispatchLocalCheckInRefusesADestinationOrDeadline(t *testing.T) {
	st := newDomainTestStore(t)
	driver, _ := seedTwoPersons(t, st, "site-a")
	site := "site-b"
	pad := "conneco"
	mins := int64(60)

	cases := []struct {
		name string
		in   DispatchInput
	}{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "{"controlled destination", DispatchInput{VehicleEntered: "T1", DriverPersonID: driver.PersonID, DestinationSite: &site, DeadlineMinutes: &mins}},"⟧
		{"controlled destination", DispatchInput{VehicleID: w7SeedVehicleID(t, st, "site-a", "T1"), DriverPersonID: driver.PersonID, DestinationSite: &site, DeadlineMinutes: &mins}},
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "{"uncontrolled destination", DispatchInput{VehicleEntered: "T1", DriverPersonID: driver.PersonID, DestinationUncontrolled: &pad}},"⟧
		{"uncontrolled destination", DispatchInput{VehicleID: w7SeedVehicleID(t, st, "site-a", "T1"), DriverPersonID: driver.PersonID, DestinationUncontrolled: &pad}},
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "{"deadline alone", DispatchInput{VehicleEntered: "T1", DriverPersonID: driver.PersonID, DeadlineMinutes: &mins}},"⟧
		{"deadline alone", DispatchInput{VehicleID: w7SeedVehicleID(t, st, "site-a", "T1"), DriverPersonID: driver.PersonID, DeadlineMinutes: &mins}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DispatchLocalCheckIn(st, "site-a", 1, tc.in, 0, "default"); err == nil {
				t.Fatalf("DispatchLocalCheckIn accepted %s — it is the arrival half of a check-in, not a departure", tc.name)
			}
		})
	}
	if trips, err := Trips(st); err != nil || len(trips) != 0 {
		t.Fatalf("a refused local check-in authored a trip: %v %+v", err, trips)
	}
}

// TestARoutineDepartureIsNotAnIncompleteCheckIn is the consequence, at the
// domain level: the thing D-4 exists to stop.
//
// It asserts its own PRECONDITION first — a genuine crashed check-in IS
// listed — so a version of IncompleteCheckIns that returned nothing at all
// (which would satisfy the second half trivially) fails the first half.
// F1's box must keep meaning a crash and nothing else, which requires both
// legs.
func TestARoutineDepartureIsNotAnIncompleteCheckIn(t *testing.T) {
	st := newDomainTestStore(t)
	driver, p1 := seedTwoPersons(t, st, "site-a")

	// PRECONDITION: the crash shape is still detected. This is the ONLY way
	// to author it now, and its whole doc comment says the caller must
	// follow it with an arrival — here we deliberately do not.
	crashed, err := DispatchLocalCheckIn(st, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "T99", DriverPersonID: driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "T99"), DriverPersonID: driver.PersonID,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	stuck, err := IncompleteCheckIns(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(stuck) != 1 || stuck[0].TripID != crashed.TripID {
		t.Fatalf("a crashed check-in is no longer surfaced (%+v) — the rest of this test would pass vacuously", stuck)
	}

	// A routine departure: a real vehicle, a driver, and somewhere it went.
	pad := "conneco"
	if _, err := Dispatch(st, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered:          "TRK-14","⟧
		VehicleID:               w7SeedVehicleID(t, st, "site-a", "TRK-14"),
		DriverPersonID:          driver.PersonID,
		OccupantPersonIDs:       []string{p1.PersonID},
		DestinationUncontrolled: &pad,
	}, 0, "default"); err != nil {
		t.Fatalf("routine departure refused: %v", err)
	}

	stuck, err = IncompleteCheckIns(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(stuck) != 1 || stuck[0].TripID != crashed.TripID {
		t.Fatalf("a routine departure appeared as an incomplete check-in: %+v", stuck)
	}
}

// TestOnlyTheCheckInPathAuthorsALocalCheckInDispatch is the structural half
// of the guard, and it is the one that survives a future edit.
//
// DispatchLocalCheckIn has to be exported (the web and deadline packages
// build check-in fixtures with it), and on its own it authors an OPEN,
// never-arrived degenerate trip — precisely the shape D-4 removed. So the
// rule that keeps it honest is not a type: it is that no production code
// outside the check-in path may call it. A future "local check-out" wired
// to this function would compile, pass every other test, and reintroduce
// the defect in full. This test refuses to let that happen quietly.
//
// It scans SOURCE rather than behaviour deliberately — there is no runtime
// signal that distinguishes "authored by the check-in path" from "authored
// by something else", which is the whole reason the two events were
// indistinguishable in the first place.
func TestOnlyTheCheckInPathAuthorsALocalCheckInDispatch(t *testing.T) {
	allowed := map[string]bool{
		filepath.Join("internal", "domain", "tripauthor.go"): true, // the definition
		filepath.Join("internal", "domain", "checkin.go"):    true, // GateCheckIn, which authors the arrival next
	}
	root := filepath.Join("..", "..")
	var offenders []string
	seen := map[string]bool{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !strings.Contains(string(src), "DispatchLocalCheckIn") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		seen[rel] = true
		if !allowed[rel] {
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// The scan must actually be finding things, or it proves nothing.
	for want := range allowed {
		if !seen[want] {
			t.Fatalf("the source scan found no DispatchLocalCheckIn in %s — it is looking in the wrong place, so its result means nothing", want)
		}
	}
	if len(offenders) != 0 {
		t.Fatalf("DispatchLocalCheckIn is called from production code outside the check-in path: %v\n"+
			"On its own it authors an open, never-arrived degenerate trip — the exact shape D-4 removed "+
			"(it shows on /gate as an incomplete check-in with a Resume button, and prints MAY BE INCOMPLETE "+
			"on the muster). If you are recording a departure, use Dispatch and name where the vehicle went. "+
			"If you are recording an exit with no vehicle — medevac by air is the plausible case — D-4 says "+
			"that is NOT covered anywhere and must be its own event type with its own name on the muster. "+
			"Ask James before building it.", offenders)
	}
}
