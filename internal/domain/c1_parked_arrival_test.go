// c1_parked_arrival_test.go — C-1 / T-092 (sc-test campaign, 2026-09-29):
// an unsolicited arrival PARKED in the ambiguous review lane must STAY parked
// when another of its candidate trips is later closed by an explicit arrival
// at the same post. Before this fix Pass 2 re-evaluated the parked arrival
// against the now-singleton candidate set and bound it BY ELIMINATION —
// closing a trip none of the arrival's occupants were on, deleting its
// expectation and overdue alert at both posts, clearing the review row, and
// filing its crew "not aboard at arrival" on both evacuation sheets. §9.2's
// own rationale ("a wrong match would cancel a genuinely overdue vehicle's
// alert") is exactly what that did.
//
// Every fixture here ASSERTS the arrangement it rests on before acting
// (BUILDER_BRIEF_RULES rule 6), and every check runs both on the authoring
// store and under replayAllOrders — the fold must reach the same answer
// from any delivery order and on a from-zero rebuild of the event log.
package domain

import (
	"testing"

	"sitecontrol/internal/store"
)

// c1World is the T-092 fixture: two open trips on ONE correlation key (the
// same vehicle record, the same origin) from site-a to site-b, and an
// unsolicited arrival at site-b carrying that key with trip1's driver
// aboard — parked as ambiguous over exactly [trip1, trip2].
type c1World struct {
	*c6World
	vehicleID    string
	trip1, trip2 Trip
	parked       Arrival
}

// c1TwoOpenTrips dispatches the two same-key trips and syncs them to
// site-b. It asserts both are open expectations on site-b's board.
func c1TwoOpenTrips(t *testing.T) *c1World {
	t.Helper()
	w := &c1World{c6World: newC6World(t)}
	dest := "site-b"
	deadline := int64(30)
	w.vehicleID = w7SeedVehicleID(t, w.a, "site-a", "TRK-950")
	var err error
	w.trip1, err = Dispatch(w.a, "site-a", 1, DispatchInput{
		VehicleID: w.vehicleID, DriverPersonID: w.driver.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w.trip2, err = Dispatch(w.a, "site-a", 1, DispatchInput{
		VehicleID: w.vehicleID, DriverPersonID: w.passenger.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w.syncBFromA()
	// PRECONDITION: two open trips on one key — the arrangement §9.2's
	// ambiguity rule exists for. A fixture that no longer poses it must
	// shout, not pass.
	if w.trip1.TripID == w.trip2.TripID {
		t.Fatalf("PRECONDITION: the two dispatches minted one trip id %s", w.trip1.TripID)
	}
	for _, id := range []string{w.trip1.TripID, w.trip2.TripID} {
		if s := stateOf(t, w.b, id); s.State != TripStateOpen || s.ArrivalID != nil {
			t.Fatalf("PRECONDITION: trip %s is %q (arrival %v) at site-b, want open and unbound", id, s.State, s.ArrivalID)
		}
	}
	exp := c1ExpectationIDs(t, w.b, "site-b")
	if !exp[w.trip1.TripID] || !exp[w.trip2.TripID] {
		t.Fatalf("PRECONDITION: site-b's board lists %v, want both %s and %s", exp, w.trip1.TripID, w.trip2.TripID)
	}
	return w
}

// newC1ParkedWorld is c1TwoOpenTrips plus T-092 step 3: the unsolicited
// arrival at site-b, asserted parked as ambiguous over [trip1, trip2].
func newC1ParkedWorld(t *testing.T) *c1World {
	t.Helper()
	w := c1TwoOpenTrips(t)
	var err error
	w.parked, err = RecordUnsolicitedArrival(w.b, "site-b", 1, w.vehicleID, "site-a",
		[]string{w.driver.PersonID}, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// PRECONDITION: parked, binding nothing, both trips still open.
	c1AssertParkedOver(t, w.b, w.parked.ArrivalID, w.trip1.TripID, w.trip2.TripID)
	for _, id := range []string{w.trip1.TripID, w.trip2.TripID} {
		if s := stateOf(t, w.b, id); s.State != TripStateOpen {
			t.Fatalf("PRECONDITION: trip %s is %q after the ambiguous arrival, want open", id, s.State)
		}
	}
	return w
}

// c1AssertParkedOver is the review-lane shape of an ambiguous arrival:
// disposition ambiguous, bound to nothing, candidates exactly [t1, t2] in
// source-key order, and the one row LoadReviewLane surfaces.
func c1AssertParkedOver(t *testing.T, st *store.Store, arrivalID, t1, t2 string) {
	t.Helper()
	d := dispositionOf(t, st, arrivalID)
	if d.Disposition != ArrivalAmbiguous {
		t.Fatalf("arrival %s disposition = %q, want %q", arrivalID, d.Disposition, ArrivalAmbiguous)
	}
	if d.TripID != nil {
		t.Fatalf("arrival %s is bound to trip %s — an ambiguous arrival binds NOTHING", arrivalID, *d.TripID)
	}
	want := t1 + "," + t2
	if d.Candidates == nil || *d.Candidates != want {
		t.Fatalf("arrival %s candidates = %v, want %q", arrivalID, d.Candidates, want)
	}
	lane, err := LoadReviewLane(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(lane.Ambiguous) != 1 || lane.Ambiguous[0].ArrivalID != arrivalID {
		t.Fatalf("review lane Ambiguous = %+v, want exactly arrival %s", lane.Ambiguous, arrivalID)
	}
}

// c1ExpectationIDs is the set of open inbound expectations on a post's board
// — the deadline scanner's input, so "still an expectation" is the domain-
// level statement of "its overdue alert is still possible".
func c1ExpectationIDs(t *testing.T, st *store.Store, siteID string) map[string]bool {
	t.Helper()
	exps, err := OpenExpectations(st, siteID)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, e := range exps {
		out[e.Trip.TripID] = true
	}
	return out
}

// c1TripsBoundTo lists every trip whose trip_state.arrival_id is arrivalID.
func c1TripsBoundTo(t *testing.T, st *store.Store, arrivalID string) []string {
	t.Helper()
	states, err := TripStates(st)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, s := range states {
		if s.ArrivalID != nil && *s.ArrivalID == arrivalID {
			out = append(out, s.TripID)
		}
	}
	return out
}

// --- C-1 / T-092: the defect ------------------------------------------------

// TestC1ParkedAmbiguousArrivalIsNotBoundByEliminationWhenTheOtherCandidateArrivesExplicitly
// is the exact T-092 sequence. Step 4 — the officer presses "Arrived" on
// trip1's board row — must close trip1 and NOTHING ELSE: trip2 stays open
// and on the board (so its overdue alert stays live), the parked arrival
// stays parked over the same two candidates, and no trip anywhere is bound
// to the parked arrival.
//
// FAILS on 9a2fb30: trip2 reads `arrived` with arrival_id = the parked
// arrival, the board and the review lane are empty.
func TestC1ParkedAmbiguousArrivalIsNotBoundByEliminationWhenTheOtherCandidateArrivesExplicitly(t *testing.T) {
	w := newC1ParkedWorld(t)

	explicit, err := RecordExpectedArrival(w.b, "site-b", 1, w.trip1.TripID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	check := func(t *testing.T, st *store.Store) {
		// trip1 is closed by the officer's EXPLICIT arrival — their own action.
		s1 := stateOf(t, st, w.trip1.TripID)
		if s1.State != TripStateArrived || s1.ArrivalID == nil || *s1.ArrivalID != explicit.ArrivalID {
			t.Fatalf("trip1 %s = %q (arrival %v), want arrived by the explicit arrival %s", w.trip1.TripID, s1.State, s1.ArrivalID, explicit.ArrivalID)
		}
		// trip2 — nobody has said a word about it — is still open and still an
		// expectation on site-b's board. This is the alert that T-092 lost.
		s2 := stateOf(t, st, w.trip2.TripID)
		if s2.State != TripStateOpen || s2.ArrivalID != nil {
			t.Fatalf("trip2 %s = %q (arrival %v), want STILL OPEN — it was closed by elimination: an arrival none of its occupants were on silenced its alert", w.trip2.TripID, s2.State, s2.ArrivalID)
		}
		exp := c1ExpectationIDs(t, st, "site-b")
		if !exp[w.trip2.TripID] {
			t.Fatalf("trip2 %s has left site-b's board (%v) — its overdue expectation was deleted without an operator touching it", w.trip2.TripID, exp)
		}
		if exp[w.trip1.TripID] {
			t.Fatalf("trip1 %s is still on site-b's board after its explicit arrival", w.trip1.TripID)
		}
		// The parked arrival is still parked, over the same two candidates,
		// and the review row is still there for a human to decide.
		c1AssertParkedOver(t, st, w.parked.ArrivalID, w.trip1.TripID, w.trip2.TripID)
		if bound := c1TripsBoundTo(t, st, w.parked.ArrivalID); len(bound) != 0 {
			t.Fatalf("trip(s) %v are bound to the PARKED arrival %s — the system picked", bound, w.parked.ArrivalID)
		}
		// T-092's own oracle, kept: the driver is on site-b exactly once.
		if got := openVisitsAt(t, st, "site-b"); len(got) != 1 || got[0].PersonID != w.driver.PersonID {
			t.Fatalf("site-b open visits = %d, want exactly the driver's one", len(got))
		}
	}
	check(t, w.b)
	replayAllOrders(t, w.events(w.a, "site-a"), w.events(w.b, "site-b"), check)
}

// TestC1ParkedArrivalStaysParkedWhenEveryCandidateIsLaterClosedExplicitly
// pins the conservative reading of "parked until an operator resolves it":
// even once BOTH candidates have been closed by explicit arrivals, the
// parked arrival binds nothing and keeps its review row. Whether that row
// should instead clear itself once nothing is left to pick is a ruling
// question (C-1 fix report, Q2) — the records do not decide it, so the fold
// does the thing that can never close a trip or delete an alert on its own.
//
// FAILS on 9a2fb30 only in the interim state (after the first explicit
// arrival, when it bound trip2 by elimination); the end state there was
// `plain`. Asserted here so the chosen behaviour is measured, not stated.
func TestC1ParkedArrivalStaysParkedWhenEveryCandidateIsLaterClosedExplicitly(t *testing.T) {
	w := newC1ParkedWorld(t)

	e1, err := RecordExpectedArrival(w.b, "site-b", 1, w.trip1.TripID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// Interim: the T-092 state. trip2 must still be open here or the second
	// explicit arrival below is recording an arrival for a closed trip.
	if s := stateOf(t, w.b, w.trip2.TripID); s.State != TripStateOpen {
		t.Fatalf("PRECONDITION (interim): trip2 %s = %q after trip1's explicit arrival, want open", w.trip2.TripID, s.State)
	}
	e2, err := RecordExpectedArrival(w.b, "site-b", 1, w.trip2.TripID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	check := func(t *testing.T, st *store.Store) {
		for _, tc := range []struct{ trip, arrival string }{{w.trip1.TripID, e1.ArrivalID}, {w.trip2.TripID, e2.ArrivalID}} {
			s := stateOf(t, st, tc.trip)
			if s.State != TripStateArrived || s.ArrivalID == nil || *s.ArrivalID != tc.arrival {
				t.Fatalf("trip %s = %q (arrival %v), want arrived by its own explicit arrival %s", tc.trip, s.State, s.ArrivalID, tc.arrival)
			}
		}
		c1AssertParkedOver(t, st, w.parked.ArrivalID, w.trip1.TripID, w.trip2.TripID)
		if bound := c1TripsBoundTo(t, st, w.parked.ArrivalID); len(bound) != 0 {
			t.Fatalf("trip(s) %v are bound to the PARKED arrival %s", bound, w.parked.ArrivalID)
		}
	}
	check(t, w.b)
	replayAllOrders(t, w.events(w.a, "site-a"), w.events(w.b, "site-b"), check)
}

// --- unchanged behaviour, pinned so the fix cannot over-reach ----------------

// TestC1UniqueUnsolicitedMatchStillBindsWhenTheEarlierExplicitArrivalPrecededIt
// is the sequential case the fix must NOT disturb: trip1 is closed by an
// explicit arrival FIRST, and only then does the unsolicited arrival land.
// At that moment exactly one trip is open, so §9.2 binds it — "earlier
// arrivals consume earlier trips". The distinction from T-092 is purely the
// order within site-b's own stream, which is true authoring order.
func TestC1UniqueUnsolicitedMatchStillBindsWhenTheEarlierExplicitArrivalPrecededIt(t *testing.T) {
	w := c1TwoOpenTrips(t)

	explicit, err := RecordExpectedArrival(w.b, "site-b", 1, w.trip1.TripID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// PRECONDITION: trip1 consumed, trip2 the only open match, nothing parked.
	if s := stateOf(t, w.b, w.trip1.TripID); s.State != TripStateArrived {
		t.Fatalf("PRECONDITION: trip1 %s = %q, want arrived", w.trip1.TripID, s.State)
	}
	if s := stateOf(t, w.b, w.trip2.TripID); s.State != TripStateOpen {
		t.Fatalf("PRECONDITION: trip2 %s = %q, want open", w.trip2.TripID, s.State)
	}
	if lane, err := LoadReviewLane(w.b); err != nil || len(lane.Ambiguous) != 0 {
		t.Fatalf("PRECONDITION: review lane Ambiguous = %+v (err %v), want empty before the unsolicited arrival", lane.Ambiguous, err)
	}

	u, err := RecordUnsolicitedArrival(w.b, "site-b", 1, w.vehicleID, "site-a",
		[]string{w.passenger.PersonID}, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	check := func(t *testing.T, st *store.Store) {
		d := dispositionOf(t, st, u.ArrivalID)
		if d.Disposition != ArrivalCorrelated || d.TripID == nil || *d.TripID != w.trip2.TripID {
			t.Fatalf("unsolicited arrival = %q → %v, want correlated to the one open trip %s", d.Disposition, d.TripID, w.trip2.TripID)
		}
		s2 := stateOf(t, st, w.trip2.TripID)
		if s2.State != TripStateArrived || s2.ArrivalID == nil || *s2.ArrivalID != u.ArrivalID {
			t.Fatalf("trip2 %s = %q (arrival %v), want arrived by the unsolicited arrival %s", w.trip2.TripID, s2.State, s2.ArrivalID, u.ArrivalID)
		}
		s1 := stateOf(t, st, w.trip1.TripID)
		if s1.ArrivalID == nil || *s1.ArrivalID != explicit.ArrivalID {
			t.Fatalf("trip1 %s arrival = %v, want still the explicit arrival %s", w.trip1.TripID, s1.ArrivalID, explicit.ArrivalID)
		}
		lane, err := LoadReviewLane(st)
		if err != nil {
			t.Fatal(err)
		}
		if len(lane.Ambiguous) != 0 {
			t.Fatalf("review lane Ambiguous = %+v, want empty — a unique match is not ambiguous", lane.Ambiguous)
		}
	}
	check(t, w.b)
	replayAllOrders(t, w.events(w.a, "site-a"), w.events(w.b, "site-b"), check)
}

// TestC1SingleOpenTripStillCorrelatesAnUnsolicitedArrival is the headline
// §9.2 path with nothing else in play: one open trip, one unsolicited
// arrival on its key, bound.
func TestC1SingleOpenTripStillCorrelatesAnUnsolicitedArrival(t *testing.T) {
	w := newC6World(t)
	dest := "site-b"
	deadline := int64(30)
	vid := w7SeedVehicleID(t, w.a, "site-a", "TRK-950")
	trip, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		VehicleID: vid, DriverPersonID: w.driver.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w.syncBFromA()
	u, err := RecordUnsolicitedArrival(w.b, "site-b", 1, vid, "site-a", []string{w.driver.PersonID}, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	check := func(t *testing.T, st *store.Store) {
		if d := dispositionOf(t, st, u.ArrivalID); d.Disposition != ArrivalCorrelated || d.TripID == nil || *d.TripID != trip.TripID {
			t.Fatalf("unsolicited arrival = %q → %v, want correlated to %s", d.Disposition, d.TripID, trip.TripID)
		}
		if s := stateOf(t, st, trip.TripID); s.State != TripStateArrived || s.ArrivalID == nil || *s.ArrivalID != u.ArrivalID {
			t.Fatalf("trip %s = %q (arrival %v), want arrived by %s", trip.TripID, s.State, s.ArrivalID, u.ArrivalID)
		}
	}
	check(t, w.b)
	replayAllOrders(t, w.events(w.a, "site-a"), w.events(w.b, "site-b"), check)
}

// TestC1CrossingExplicitReferenceStillWinsASingleTripFromACorrelatedUnsolicitedArrival
// pins the boundary the fix deliberately does not cross. ONE open trip; an
// unsolicited arrival at site-c (D-96: it arrives where it arrives)
// correlates to it, while site-b — which has not yet heard of that arrival
// — records the trip's explicit arrival. When the two streams meet, Pass
// 1's explicit reference wins the trip and the unsolicited arrival drops to
// plain, exactly as before: it was never ambiguous, so there is nothing to
// keep parked, and the binder is in ANOTHER stream, where this fix makes
// no ordering claim (cross-stream order is unknowable without a causal
// reference — C-1 fix report, Q1). The same-stream version of this
// scenario cannot be posed through the door: RecordExpectedArrival refuses
// a trip that has already arrived here.
func TestC1CrossingExplicitReferenceStillWinsASingleTripFromACorrelatedUnsolicitedArrival(t *testing.T) {
	w := newC6World(t)
	c := newDomainTestStore(t)
	dest := "site-b"
	deadline := int64(30)
	vid := w7SeedVehicleID(t, w.a, "site-a", "TRK-950")
	trip, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		VehicleID: vid, DriverPersonID: w.driver.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w.syncBFromA()
	for _, ev := range w.events(w.a, "site-a") {
		if _, err := c.Apply(ev); err != nil {
			t.Fatal(err)
		}
	}
	u, err := RecordUnsolicitedArrival(c, "site-c", 1, vid, "site-a", []string{w.driver.PersonID}, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// PRECONDITION: correlated at site-c, never parked.
	if d := dispositionOf(t, c, u.ArrivalID); d.Disposition != ArrivalCorrelated {
		t.Fatalf("PRECONDITION: unsolicited arrival at site-c = %q, want correlated", d.Disposition)
	}
	// site-b has not heard of it: the trip is still open there.
	if s := stateOf(t, w.b, trip.TripID); s.State != TripStateOpen {
		t.Fatalf("PRECONDITION: trip %s = %q at site-b, want open (site-c's arrival has not replicated)", trip.TripID, s.State)
	}
	explicit, err := RecordExpectedArrival(w.b, "site-b", 1, trip.TripID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	check := func(t *testing.T, st *store.Store) {
		if s := stateOf(t, st, trip.TripID); s.State != TripStateArrived || s.ArrivalID == nil || *s.ArrivalID != explicit.ArrivalID {
			t.Fatalf("trip %s = %q (arrival %v), want arrived by the explicit arrival %s", trip.TripID, s.State, s.ArrivalID, explicit.ArrivalID)
		}
		if d := dispositionOf(t, st, u.ArrivalID); d.Disposition != ArrivalPlain || d.TripID != nil {
			t.Fatalf("unsolicited arrival = %q → %v, want plain and unbound once the explicit reference took the trip", d.Disposition, d.TripID)
		}
		lane, err := LoadReviewLane(st)
		if err != nil {
			t.Fatal(err)
		}
		if len(lane.Ambiguous) != 0 {
			t.Fatalf("review lane Ambiguous = %+v, want empty", lane.Ambiguous)
		}
	}
	// Three streams meet in both orders of the two arrival streams.
	aEv, bEv, cEv := w.events(w.a, "site-a"), w.events(w.b, "site-b"), w.events(c, "site-c")
	for i, order := range [][][]store.Envelope{{aEv, bEv, cEv}, {aEv, cEv, bEv}} {
		st := newDomainTestStore(t)
		for _, stream := range order {
			for _, ev := range stream {
				if _, err := st.Apply(ev); err != nil {
					t.Fatalf("order %d: apply %v: %v", i, ev.Key(), err)
				}
			}
		}
		check(t, st)
	}
}
