// d98_not_entered_test.go — SCHEMA-11 step 5 (D-98/D-91): the arrival split
// at the choke point (design/STEP5-PLAN.md; reports/witness/d91-arrival-respec.md
// §3.1-3.3, §4, §6 rows 1/4). Produced RED first: on this tree `TripArrival`
// carries only `visits[]`, and `buildArrivalVisits` takes no orientation
// flag, so this file does not compile until the field, the table and the
// split all land together (STEP5-PLAN.md §Q4 — "cannot pass vacuously
// through a partial change").
//
// The BUS 7 A(ice road, orientation=false) -> B(STP Pad, orientation=true)
// trace, Adams/Baker/Clark hold B's orientation, Diaz/Evans do not
// (d91-arrival-respec.md §4), narrowed to what step 5 actually builds: the
// split and the re-keyed "nobody aboard" refusal — NOT entry-on-completion
// (§3.4), which is a later build (orientation.go:63's own header names it a
// separate step).
package domain

import (
	"testing"

	"sitecontrol/internal/store"
)

// d98World is a two-site fixture: site-a (the ice road — never gated in
// this file) dispatches BUS 7 to site-b (the pad) with 5 occupants. The
// split is driven only at site-b: orientation is always the DESTINATION's
// own flag (orientation.go's file header — "the site is the envelope's,
// never the payload's"), so only B's HasOrientation() ever matters.
type d98World struct {
	t         *testing.T
	a, b      *store.Store
	company   Company
	trade     Trade
	driver    Person
	occupants []Person // Adams, Baker, Clark, Diaz, Evans, in that order
}

func newD98World(t *testing.T) *d98World {
	t.Helper()
	w := &d98World{t: t, a: newDomainTestStore(t), b: newDomainTestStore(t)}
	var err error
	if w.company, err = CreateCompany(w.a, "site-a", 1, "Kakivik Asset Management", 0, "default"); err != nil {
		t.Fatal(err)
	}
	if w.trade, err = CreateTrade(w.a, "site-a", 1, "Inspection", 0, "default"); err != nil {
		t.Fatal(err)
	}
	if w.driver, err = CreatePerson(w.a, "site-a", 1, "Dana Driver", &w.company.CompanyID, &w.trade.TradeID, 0, "default"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Adams", "Baker", "Clark", "Diaz", "Evans"} {
		p, err := CreatePerson(w.a, "site-a", 1, name, &w.company.CompanyID, &w.trade.TradeID, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		w.occupants = append(w.occupants, p)
	}
	return w
}

// syncBFromA replays site-a's stream into site-b so B can dispatch-read the
// trip/occupants before authoring its own arrival — the same pattern as
// c6World.syncBFromA (trip_test.go), duplicated here rather than shared
// because this fixture seeds 5 occupants, not 2.
func (w *d98World) syncBFromA() {
	w.t.Helper()
	evs, err := w.a.Events("site-a", 1, 1, 0)
	if err != nil {
		w.t.Fatal(err)
	}
	for _, ev := range evs {
		if _, err := w.b.Apply(ev); err != nil {
			w.t.Fatal(err)
		}
	}
}

// ids returns the person ids of w.occupants at the given indices, in order.
func (w *d98World) ids(idx ...int) []string {
	out := make([]string, len(idx))
	for i, n := range idx {
		out[i] = w.occupants[n].PersonID
	}
	return out
}

// dispatchBus7 authors the BUS 7 A->B trip carrying all 5 occupants and
// syncs it to B, returning the trip B will arrive.
func (w *d98World) dispatchBus7(t *testing.T) Trip {
	t.Helper()
	dest, deadline := "site-b", int64(120)
	trip, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "BUS 7"), DriverPersonID: w.driver.PersonID,
		OccupantPersonIDs: w.ids(0, 1, 2, 3, 4),
		DestinationSite:   &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatalf("Dispatch BUS 7: %v", err)
	}
	w.syncBFromA()
	return trip
}

func TestArrivalSplitsByOrientationAtTheChokePoint(t *testing.T) {
	// --- direct buildArrivalVisits probes: the choke point itself --------
	t.Run("direct: orientation=true splits 3 held / 2 unheld", func(t *testing.T) {
		w := newD98World(t)
		w.syncBFromA()         // the roster (site-a's) must reach B before B can look anyone up
		held := w.ids(0, 1, 2) // Adams, Baker, Clark
		for _, id := range held {
			if _, err := CompleteOrientation(w.b, "site-b", 1, id, nil, 0, "default"); err != nil {
				t.Fatalf("CompleteOrientation(%s): %v", id, err)
			}
		}
		visits, notEntered, err := buildArrivalVisits(w.b, "site-b", w.ids(0, 1, 2, 3, 4), true)
		if err != nil {
			t.Fatalf("buildArrivalVisits: %v", err)
		}
		if len(visits) != 3 {
			t.Fatalf("visits = %d, want 3 (Adams, Baker, Clark): %+v", len(visits), visits)
		}
		gotVisit := map[string]bool{}
		for _, v := range visits {
			gotVisit[v.PersonID] = true
		}
		for _, id := range held {
			if !gotVisit[id] {
				t.Errorf("held occupant %s missing from visits: %+v", id, visits)
			}
		}
		if len(notEntered) != 2 {
			t.Fatalf("notEntered = %d, want 2 (Diaz, Evans): %v", len(notEntered), notEntered)
		}
		gotNotEntered := map[string]bool{}
		for _, id := range notEntered {
			gotNotEntered[id] = true
		}
		for _, id := range w.ids(3, 4) {
			if !gotNotEntered[id] {
				t.Errorf("unheld occupant %s missing from notEntered: %v", id, notEntered)
			}
		}
	})

	t.Run("direct: orientation=true with exactly one un-held occupant aboard still routes them to notEntered — the gate must not depend on group size", func(t *testing.T) {
		w := newD98World(t)
		w.syncBFromA() // the roster (site-a's) must reach B before B can look anyone up
		// Nobody's orientation is completed anywhere; Diaz travels alone
		// (aboard=1) and is unheld, same as the 5-up case above.
		diaz := w.ids(3)
		visits, notEntered, err := buildArrivalVisits(w.b, "site-b", diaz, true)
		if err != nil {
			t.Fatalf("buildArrivalVisits: %v", err)
		}
		if len(visits) != 0 {
			t.Fatalf("visits = %d, want 0 (single un-held occupant, orientation=true): %+v", len(visits), visits)
		}
		if len(notEntered) != 1 || notEntered[0] != diaz[0] {
			t.Fatalf("notEntered = %v, want exactly %v (Diaz, alone, unheld)", notEntered, diaz)
		}
	})

	t.Run("direct: orientation=false is byte-identical to today — everyone enters, notEntered empty", func(t *testing.T) {
		w := newD98World(t)
		w.syncBFromA() // the roster (site-a's) must reach B before B can look anyone up
		// Nobody's orientation is completed anywhere; hasOrientation=false
		// must skip the OrientationHeld consult entirely (an ice-road post
		// cannot know it even if some row existed).
		visits, notEntered, err := buildArrivalVisits(w.b, "site-b", w.ids(0, 1, 2, 3, 4), false)
		if err != nil {
			t.Fatalf("buildArrivalVisits: %v", err)
		}
		if len(visits) != 5 {
			t.Fatalf("visits = %d, want 5 (old shape unchanged): %+v", len(visits), visits)
		}
		if len(notEntered) != 0 {
			t.Fatalf("notEntered = %v, want empty at an orientation=false post", notEntered)
		}
	})

	// --- end-to-end through RecordExpectedArrival(Excluding): the fold ---
	t.Run("end-to-end: fold splits visit rows from trip_arrival_not_entered rows", func(t *testing.T) {
		w := newD98World(t)
		w.syncBFromA() // the roster must reach B before B can complete anyone's orientation
		// TripOccupantsByTrip includes the driver (tripauthor.go:404, "one
		// visit per trip occupant, driver included — §10"), so the aboard
		// set is driver+Adams+Baker+Clark+Diaz+Evans (6), not 5. The driver
		// also holds B's orientation here (arrived with the crew, orients
		// alongside them) so the held/unheld split stays legible: 4 held,
		// Diaz/Evans (2) unheld.
		held := append(w.ids(0, 1, 2), w.driver.PersonID)
		for _, id := range held {
			if _, err := CompleteOrientation(w.b, "site-b", 1, id, nil, 0, "default"); err != nil {
				t.Fatalf("CompleteOrientation(%s): %v", id, err)
			}
		}
		trip := w.dispatchBus7(t)
		arrival, err := RecordExpectedArrival(w.b, "site-b", 1, trip.TripID, nil, 0, "default", true)
		if err != nil {
			t.Fatalf("RecordExpectedArrival: %v", err)
		}
		state, err := TripStateByID(w.b, trip.TripID)
		if err != nil {
			t.Fatal(err)
		}
		if state == nil || state.State != TripStateArrived {
			t.Fatalf("trip state = %+v, want %q — arrival closes the trip regardless of who entered", state, TripStateArrived)
		}
		visits, err := VisitsByTrip(w.b, trip.TripID)
		if err != nil {
			t.Fatal(err)
		}
		if len(visits) != 4 {
			t.Fatalf("visit rows = %d, want 4 (driver, Adams, Baker, Clark): %+v", len(visits), visits)
		}
		notEntered, err := dumpSectionWhere[ArrivalNotEntered](w.b, "trip_arrival_not_entered", store.Eq{Column: "arrival_id", Value: arrival.ArrivalID})
		if err != nil {
			t.Fatal(err)
		}
		if len(notEntered) != 2 {
			t.Fatalf("trip_arrival_not_entered rows = %d, want 2 (Diaz, Evans): %+v", len(notEntered), notEntered)
		}
	})

	// --- D-98 §3.2's re-keyed refusal: aboard=6/entered=0 is STILL an arrival ---
	t.Run("everyone un-oriented: still an arrival — the refusal keys on ABOARD, never on entered", func(t *testing.T) {
		w := newD98World(t)
		// Nobody's orientation is completed: all 6 aboard (driver + 5
		// occupants — §10), 0 will enter.
		trip := w.dispatchBus7(t)
		arrival, err := RecordExpectedArrivalExcluding(w.b, "site-b", 1, trip.TripID, nil, nil, 0, "default", true)
		if err != nil {
			t.Fatalf("RecordExpectedArrivalExcluding: %v — a bus load, all un-oriented, IS an arrival (D-98 §3.2)", err)
		}
		state, err := TripStateByID(w.b, trip.TripID)
		if err != nil {
			t.Fatal(err)
		}
		if state == nil || state.State != TripStateArrived {
			t.Fatalf("trip state = %+v, want %q", state, TripStateArrived)
		}
		visits, err := VisitsByTrip(w.b, trip.TripID)
		if err != nil {
			t.Fatal(err)
		}
		if len(visits) != 0 {
			t.Fatalf("visit rows = %d, want 0 (nobody entered)", len(visits))
		}
		notEntered, err := dumpSectionWhere[ArrivalNotEntered](w.b, "trip_arrival_not_entered", store.Eq{Column: "arrival_id", Value: arrival.ArrivalID})
		if err != nil {
			t.Fatal(err)
		}
		if len(notEntered) != 6 {
			t.Fatalf("trip_arrival_not_entered rows = %d, want 6 (driver + 5 occupants)", len(notEntered))
		}
	})

	// --- the pre-existing refusal must still fire on genuine 0-aboard -----
	t.Run("everyone stayed behind: still refused — nobody aboard is not an arrival", func(t *testing.T) {
		w := newD98World(t)
		trip := w.dispatchBus7(t)
		everyone := append(w.ids(0, 1, 2, 3, 4), w.driver.PersonID) // §10: the driver is an occupant too
		_, err := RecordExpectedArrivalExcluding(w.b, "site-b", 1, trip.TripID, everyone, nil, 0, "default", true)
		if err == nil {
			t.Fatal("RecordExpectedArrivalExcluding with everyone marked stayed: got no error, want the nobody-aboard refusal")
		}
	})

	// --- FALSIFIABILITY-2.md M-J: a PARTIAL stayed set mixed with an -----
	// --- ABOARD-but-unoriented occupant, at an orientation=true site -----
	t.Run("a PARTIAL stayed set plus an aboard-unoriented occupant: the stayed person is never listed as held out HERE", func(t *testing.T) {
		// Every existing subtest above either marks NOBODY as stayed or
		// marks EVERYONE as stayed — the shape M-J actually breaks (a
		// stayed id folded into the same "not entered" table as a
		// lacks-orientation id — the table's name invites exactly this
		// conflation) requires BOTH kinds of absence on the same arrival at
		// once: Evans came on the bus but holds no orientation; Diaz never
		// boarded at all (dropped at an uncontrolled pad en route, §9.1's
		// own stayed-behind scenario). Diaz must not appear ANYWHERE this
		// site reports "held out for lacking orientation" — he was never
		// here to lack anything.
		w := newD98World(t)
		w.syncBFromA()
		trip := w.dispatchBus7(t)
		held := append(w.ids(0, 1, 2), w.driver.PersonID) // Adams, Baker, Clark, driver
		for _, id := range held {
			if _, err := CompleteOrientation(w.b, "site-b", 1, id, nil, 0, "default"); err != nil {
				t.Fatalf("CompleteOrientation(%s): %v", id, err)
			}
		}
		diazID, evansID := w.ids(3)[0], w.ids(4)[0] // Diaz: stayed behind. Evans: aboard, unoriented.

		arr, err := RecordExpectedArrivalExcluding(w.b, "site-b", 1, trip.TripID, []string{diazID}, nil, 0, "default", true)
		if err != nil {
			t.Fatalf("RecordExpectedArrivalExcluding: %v", err)
		}

		if len(arr.NotEntered) != 1 || arr.NotEntered[0] != evansID {
			t.Fatalf("Arrival.NotEntered = %v, want exactly [Evans] — Diaz stayed behind and must not appear in the lacks-orientation set", arr.NotEntered)
		}

		heldOut, err := HeldOutAt(w.b, "site-b")
		if err != nil {
			t.Fatal(err)
		}
		sawEvans, sawDiaz := false, false
		for _, h := range heldOut {
			if h.PersonID == evansID {
				sawEvans = true
			}
			if h.PersonID == diazID {
				sawDiaz = true
			}
		}
		if !sawEvans {
			t.Fatalf("HeldOutAt(site-b) does not list Evans (aboard, unoriented): %+v", heldOut)
		}
		if sawDiaz {
			t.Fatalf("BROKEN: HeldOutAt(site-b) lists Diaz as held out at THIS gate — he stayed behind and never reached it: %+v", heldOut)
		}

		open := map[string]bool{}
		visits, err := OpenVisitsAt(w.b, "site-b")
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range visits {
			open[v.PersonID] = true
		}
		if open[diazID] {
			t.Fatalf("BROKEN: Diaz has an open visit at site-b despite staying behind")
		}
		if open[evansID] {
			t.Fatalf("Evans has an open visit at site-b despite lacking orientation")
		}
	})
}
