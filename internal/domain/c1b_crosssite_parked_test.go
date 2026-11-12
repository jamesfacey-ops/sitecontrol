// c1b_crosssite_parked_test.go — C-1 / T-092, the CROSS-SITE variant.
// James, 2026-09-30: cross-site binders never exclude (option b) — an
// explicit arrival authored at ANOTHER post must never shrink a parked
// arrival's candidate set; if closing a candidate cross-site would leave a
// singleton, the parked arrival stays ambiguous for an officer to resolve.
// The same-site rule from c1_parked_arrival_test.go is unchanged.
//
// Cross-stream order is unknowable to the fold (arrivals carry no causal
// reference), so these scenarios are asserted under EVERY delivery order of
// the three streams and must reach one byte-identical projection.
package domain

import (
	"testing"

	"sitecontrol/internal/store"
)

// c1bStores gives the T-092 world a third post: site-b (the destination,
// w.b) and site-c (a fresh store that has heard site-a's stream). Neither
// has heard anything the other authors until the streams are merged.
func c1bStores(t *testing.T, w *c1World) map[string]*store.Store {
	t.Helper()
	c := newDomainTestStore(t)
	for _, ev := range w.events(w.a, "site-a") {
		if _, err := c.Apply(ev); err != nil {
			t.Fatal(err)
		}
	}
	return map[string]*store.Store{"site-b": w.b, "site-c": c}
}

func c1bConcat(x, y []store.Envelope) []store.Envelope {
	out := append([]store.Envelope{}, x...)
	return append(out, y...)
}

// TestC1bParkedArrivalIsNotBoundByEliminationWhenTheOtherCandidateArrivesExplicitlyAtAnotherPost
// is T-092 with the officer's "Arrived" pressed at a DIFFERENT post from
// the one that parked the unsolicited arrival, in both assignments. The
// parked arrival must stay parked over both candidates, trip2 must stay
// open on site-b's board (its alert), and no trip may be bound to the
// parked arrival — at every post, under every delivery order.
//
// FAILS on 6a3a0fa: the cross-site binder was excluded from the candidate
// set, so the parked arrival bound trip2 by elimination.
func TestC1bParkedArrivalIsNotBoundByEliminationWhenTheOtherCandidateArrivesExplicitlyAtAnotherPost(t *testing.T) {
	for _, tc := range []struct{ name, parkedAt, explicitAt string }{
		{"parked at the destination, explicit at a third post", "site-b", "site-c"},
		{"parked at a third post, explicit at the destination", "site-c", "site-b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := c1TwoOpenTrips(t)
			stores := c1bStores(t, w)
			if tc.parkedAt == tc.explicitAt {
				t.Fatalf("PRECONDITION: this fixture is the CROSS-site case; both posts are %s", tc.parkedAt)
			}

			parked, err := RecordUnsolicitedArrival(stores[tc.parkedAt], tc.parkedAt, 1, w.vehicleID, "site-a",
				[]string{w.driver.PersonID}, nil, 0, "default")
			if err != nil {
				t.Fatal(err)
			}
			// PRECONDITION: parked at its own post; the other post has heard nothing.
			c1AssertParkedOver(t, stores[tc.parkedAt], parked.ArrivalID, w.trip1.TripID, w.trip2.TripID)
			if s := stateOf(t, stores[tc.explicitAt], w.trip1.TripID); s.State != TripStateOpen {
				t.Fatalf("PRECONDITION: trip1 %s = %q at %s before the explicit arrival, want open", w.trip1.TripID, s.State, tc.explicitAt)
			}

			explicit, err := RecordExpectedArrival(stores[tc.explicitAt], tc.explicitAt, 1, w.trip1.TripID, nil, 0, "default")
			if err != nil {
				t.Fatal(err)
			}

			check := func(t *testing.T, st *store.Store) {
				s1 := stateOf(t, st, w.trip1.TripID)
				if s1.State != TripStateArrived || s1.ArrivalID == nil || *s1.ArrivalID != explicit.ArrivalID {
					t.Fatalf("trip1 %s = %q (arrival %v), want arrived by the explicit arrival %s", w.trip1.TripID, s1.State, s1.ArrivalID, explicit.ArrivalID)
				}
				s2 := stateOf(t, st, w.trip2.TripID)
				if s2.State != TripStateOpen || s2.ArrivalID != nil {
					t.Fatalf("trip2 %s = %q (arrival %v), want STILL OPEN — a cross-site explicit arrival on trip1 resolved the ambiguity by elimination", w.trip2.TripID, s2.State, s2.ArrivalID)
				}
				exp := c1ExpectationIDs(t, st, "site-b")
				if !exp[w.trip2.TripID] || exp[w.trip1.TripID] {
					t.Fatalf("site-b board = %v, want trip2 %s only", exp, w.trip2.TripID)
				}
				c1AssertParkedOver(t, st, parked.ArrivalID, w.trip1.TripID, w.trip2.TripID)
				if bound := c1TripsBoundTo(t, st, parked.ArrivalID); len(bound) != 0 {
					t.Fatalf("trip(s) %v are bound to the PARKED arrival %s", bound, parked.ArrivalID)
				}
			}
			aEv := w.events(w.a, "site-a")
			pEv := w.events(stores[tc.parkedAt], tc.parkedAt)
			eEv := w.events(stores[tc.explicitAt], tc.explicitAt)
			if len(pEv) != 1 || len(eEv) != 1 {
				t.Fatalf("PRECONDITION: arrival streams have %d and %d events, want 1 each (so the two orders below cover every interleaving)", len(pEv), len(eEv))
			}
			// Every interleaving of the three streams: site-a's stream against
			// the two arrivals in either relative order.
			replayAllOrders(t, aEv, c1bConcat(pEv, eEv), check)
			replayAllOrders(t, aEv, c1bConcat(eEv, pEv), check)
		})
	}
}

// TestC1bCrossSiteSequentialUniqueMatchNowParksInsteadOfBinding pins the
// accepted price of option (b): trip1 was closed by an explicit arrival at
// site-b; the next unsolicited arrival on the same key, at site-c, sees
// trip2 as the only OPEN trip — but the fold cannot know whether site-b's
// arrival came before or after it, so it parks rather than binds. trip2
// stays open (over-alerting, never a silenced alert) and an officer
// resolves it with one extra click.
//
// FAILS on 6a3a0fa (the arrival correlated to trip2). This is the one
// behaviour the ruling changes on purpose; it is asserted so the change is
// measured, not stated.
func TestC1bCrossSiteSequentialUniqueMatchNowParksInsteadOfBinding(t *testing.T) {
	w := c1TwoOpenTrips(t)
	stores := c1bStores(t, w)

	explicit, err := RecordExpectedArrival(w.b, "site-b", 1, w.trip1.TripID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// site-c hears site-b's arrival BEFORE it records its own — the
	// sequential shape, as far as any post can tell.
	for _, ev := range w.events(w.b, "site-b") {
		if _, err := stores["site-c"].Apply(ev); err != nil {
			t.Fatal(err)
		}
	}
	// PRECONDITION: at site-c trip1 is consumed and trip2 is the only open match.
	if s := stateOf(t, stores["site-c"], w.trip1.TripID); s.State != TripStateArrived {
		t.Fatalf("PRECONDITION: trip1 %s = %q at site-c, want arrived", w.trip1.TripID, s.State)
	}
	if s := stateOf(t, stores["site-c"], w.trip2.TripID); s.State != TripStateOpen {
		t.Fatalf("PRECONDITION: trip2 %s = %q at site-c, want open", w.trip2.TripID, s.State)
	}

	u, err := RecordUnsolicitedArrival(stores["site-c"], "site-c", 1, w.vehicleID, "site-a",
		[]string{w.passenger.PersonID}, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	check := func(t *testing.T, st *store.Store) {
		c1AssertParkedOver(t, st, u.ArrivalID, w.trip1.TripID, w.trip2.TripID)
		if s := stateOf(t, st, w.trip2.TripID); s.State != TripStateOpen || s.ArrivalID != nil {
			t.Fatalf("trip2 %s = %q (arrival %v), want open — option (b) parks the cross-site sequential case", w.trip2.TripID, s.State, s.ArrivalID)
		}
		if s := stateOf(t, st, w.trip1.TripID); s.ArrivalID == nil || *s.ArrivalID != explicit.ArrivalID {
			t.Fatalf("trip1 %s arrival = %v, want the explicit arrival %s", w.trip1.TripID, s.ArrivalID, explicit.ArrivalID)
		}
		if !c1ExpectationIDs(t, st, "site-b")[w.trip2.TripID] {
			t.Fatalf("trip2 %s has left site-b's board", w.trip2.TripID)
		}
	}
	check(t, stores["site-c"])
	aEv := w.events(w.a, "site-a")
	bEv := w.events(w.b, "site-b")
	cEv := w.events(stores["site-c"], "site-c")
	replayAllOrders(t, aEv, c1bConcat(bEv, cEv), check)
	replayAllOrders(t, aEv, c1bConcat(cEv, bEv), check)
}

// TestC1bLoneCrossSiteBoundCandidateStillFallsToPlain pins what option (b)
// does NOT change: with ONE trip on the key, closed by an explicit arrival
// at another post, an unsolicited arrival on that key has nothing left to
// choose between — it is plain, and the explicit reference keeps the trip.
// (The crossing case in c1_parked_arrival_test.go covers the correlated-
// then-crossed order; this is the never-correlated order.)
func TestC1bLoneCrossSiteBoundCandidateStillFallsToPlain(t *testing.T) {
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
	c := newDomainTestStore(t)
	for _, ev := range w.events(w.a, "site-a") {
		if _, err := c.Apply(ev); err != nil {
			t.Fatal(err)
		}
	}
	explicit, err := RecordExpectedArrival(w.b, "site-b", 1, trip.TripID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	u, err := RecordUnsolicitedArrival(c, "site-c", 1, vid, "site-a", []string{w.passenger.PersonID}, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	check := func(t *testing.T, st *store.Store) {
		if s := stateOf(t, st, trip.TripID); s.State != TripStateArrived || s.ArrivalID == nil || *s.ArrivalID != explicit.ArrivalID {
			t.Fatalf("trip %s = %q (arrival %v), want arrived by the explicit arrival %s", trip.TripID, s.State, s.ArrivalID, explicit.ArrivalID)
		}
		if d := dispositionOf(t, st, u.ArrivalID); d.Disposition != ArrivalPlain || d.TripID != nil {
			t.Fatalf("unsolicited arrival = %q → %v, want plain: one candidate, already taken by an explicit reference", d.Disposition, d.TripID)
		}
	}
	aEv := w.events(w.a, "site-a")
	bEv := w.events(w.b, "site-b")
	cEv := w.events(c, "site-c")
	replayAllOrders(t, aEv, c1bConcat(bEv, cEv), check)
	replayAllOrders(t, aEv, c1bConcat(cEv, bEv), check)
}
