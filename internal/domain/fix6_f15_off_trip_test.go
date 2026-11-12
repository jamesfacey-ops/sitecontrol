// fix6_f15_off_trip_test.go — F15 (James, 2026-10-05): "If someone aboard a
// truck turns out never to have left and checks back in at the origin gate,
// they come off that trip EVERYWHERE."
//
// Before this build the muster already had them off (openRunsFrom applies
// sightedSince), but Expectation — the destination's inbound list, the
// origin's outbound row, the alert's "N aboard", the analytics runs — and
// the arrival's visit set read the composed occupant list raw: the person
// was on site at the origin AND expected (then checked in) at the
// destination. StillAboard is now the one rule all of them read.
//
// Every test is two posts, two stores, with replication (fix2World); each
// asserts at BOTH posts after sync, and extends the D1 "exactly one place"
// invariant to the turned-back case.
package domain

import (
	"testing"

	"sitecontrol/internal/store"
)

// f15Run returns the one Expectation for the world's trip in list, or fails.
func f15Run(t *testing.T, list []Expectation, tripID, where string) Expectation {
	t.Helper()
	for _, e := range list {
		if e.Trip.TripID == tripID {
			return e
		}
	}
	t.Fatalf("%s: the trip is not listed (%d rows) — the run must stay open", where, len(list))
	return Expectation{}
}

// f15Aboard asserts, at one post, that the destination's inbound row and
// the origin's outbound row both carry exactly want (count and list), and
// that the muster's in-transit row agrees.
func (w *fix2World) f15Aboard(t *testing.T, st *store.Store, post string, want ...string) {
	t.Helper()
	want = w9aSortedCopy(want)
	in, err := OpenExpectations(st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	out, err := OpenOutbound(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	for name, e := range map[string]Expectation{
		"inbound (destination list, strip, analytics)": f15Run(t, in, w.trip.TripID, post+" inbound"),
		"outbound (/trips row, origin strip)":          f15Run(t, out, w.trip.TripID, post+" outbound"),
	} {
		var got []string
		for _, o := range e.OccupantList {
			got = append(got, o.PersonID)
		}
		got = w9aSortedCopy(got)
		if e.Occupants != len(want) || !w9aEqualStrings(got, want) {
			t.Errorf("@%s %s: %d aboard %v, want %d %v", post, name, e.Occupants, got, len(want), want)
		}
	}
	rows, err := InTransitFrom(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	var muster []string
	for _, r := range rows {
		if r.TripID == w.trip.TripID {
			for _, o := range r.Persons {
				muster = append(muster, o.PersonID)
			}
		}
	}
	if muster = w9aSortedCopy(muster); !w9aEqualStrings(muster, want) {
		t.Errorf("@%s muster in-transit = %v, want %v — every surface must agree", post, muster, want)
	}
}

// TestF15OriginCheckInTakesThePersonOffTheRunEverywhere: P checks back in at
// the origin. At the origin at once, at the destination once it replicates,
// P is off the inbound list, the outbound row and the muster; the arrival
// then enters A only.
// FAIL BEFORE: both posts' Expectations listed 2 aboard after sync, and the
// arrival entered P at the destination while P was on site at the origin
// (the invariant read "origin visit + destination visit").
func TestF15OriginCheckInTakesThePersonOffTheRunEverywhere(t *testing.T) {
	w := newFix2World(t)
	w.f15Aboard(t, w.origin, "origin", w.a.PersonID, w.p.PersonID)
	w.f15Aboard(t, w.dest, "destination", w.a.PersonID, w.p.PersonID)

	fix2CheckInAt(t, w.origin, "site-a", w.p.PersonID)
	// The origin knows at once; the destination still expects P until the
	// check-in replicates — the accepted eventual consistency.
	w.f15Aboard(t, w.origin, "origin, before sync", w.a.PersonID)
	in, err := OpenExpectations(w.dest, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if e := f15Run(t, in, w.trip.TripID, "destination before sync"); e.Occupants != 2 {
		t.Fatalf("PRECONDITION: before replication the destination expects %d, want 2", e.Occupants)
	}

	w.sync(t)
	w.f15Aboard(t, w.origin, "origin", w.a.PersonID)
	w.f15Aboard(t, w.dest, "destination", w.a.PersonID)
	w.invariant(t, "after P checked back in at the origin", map[string]string{
		w.a.PersonID: "in-transit", w.p.PersonID: "origin visit"})

	// The truck arrives with the destination's arrival form as it now reads
	// — nobody ticked — and P must not be entered there.
	w.arrive(t)
	w.agree(t, "site-b", []string{w.a.PersonID})
	w.agree(t, "site-a", []string{w.p.PersonID})
	w.invariant(t, "after arrival", map[string]string{
		w.a.PersonID: "destination visit", w.p.PersonID: "origin visit"})
}

// TestF15StaleStayedTickNamingTheTurnedBackPersonIsHarmless: a destination
// form rendered before replication may still list P and tick them stayed;
// the arrival accepts it and still enters only A. Without the tick, the
// same stale form must not enter P either (the visit set is decided from
// the record at the press, not from the form).
func TestF15StaleStayedTickNamingTheTurnedBackPersonIsHarmless(t *testing.T) {
	w := newFix2World(t)
	fix2CheckInAt(t, w.origin, "site-a", w.p.PersonID)
	w.sync(t)
	w.arrive(t, w.p.PersonID)
	w.agree(t, "site-b", []string{w.a.PersonID})
	w.invariant(t, "after arrival", map[string]string{
		w.a.PersonID: "destination visit", w.p.PersonID: "origin visit"})
}

// TestF15WholeCrewBackRunStaysOpenAtZeroAboard: driver and passenger both
// check back in at the origin. The run is a vehicle trip — it stays open
// on both boards with its deadline, reading 0 aboard; the muster has no
// in-transit row for it (no people); when the truck arrives the arrival is
// recorded, closes the trip and enters nobody.
// FAIL BEFORE: 2 aboard on both boards, and the arrival entered both at the
// destination while both were on site at the origin.
func TestF15WholeCrewBackRunStaysOpenAtZeroAboard(t *testing.T) {
	w := newFix2World(t)
	fix2CheckInAt(t, w.origin, "site-a", w.a.PersonID)
	fix2CheckInAt(t, w.origin, "site-a", w.p.PersonID)
	w.sync(t)
	w.f15Aboard(t, w.origin, "origin", nil...)
	w.f15Aboard(t, w.dest, "destination", nil...)
	w.invariant(t, "whole crew back", map[string]string{
		w.a.PersonID: "origin visit", w.p.PersonID: "origin visit"})

	w.arrive(t)
	for name, st := range map[string]*store.Store{"origin": w.origin, "destination": w.dest} {
		s, err := TripStateByID(st, w.trip.TripID)
		if err != nil {
			t.Fatal(err)
		}
		if s == nil || s.State != TripStateArrived {
			t.Fatalf("@%s the trip is %+v after its arrival, want arrived", name, s)
		}
	}
	w.agree(t, "site-b", nil)
	w.agree(t, "site-a", []string{w.a.PersonID, w.p.PersonID})
	w.invariant(t, "after arrival", map[string]string{
		w.a.PersonID: "origin visit", w.p.PersonID: "origin visit"})
}

// TestF15ArrivedBackHereDoesNotReEnterThePersonAlreadyBack: the truck is
// recorded "Arrived back here" at the origin after P had already checked in
// there. The return's visit set is A alone — P's own check-in stands, and
// the trip's arrival does not mint a second origin visit for them.
// FAIL BEFORE: the return minted a visit for P on the trip.
func TestF15ArrivedBackHereDoesNotReEnterThePersonAlreadyBack(t *testing.T) {
	w := newFix2World(t)
	fix2CheckInAt(t, w.origin, "site-a", w.p.PersonID)
	if _, err := RecordReturnToOrigin(w.origin, "site-a", 1, w.trip.TripID, nil, nil, 0, "default", false); err != nil {
		t.Fatal(err)
	}
	w.sync(t)
	for name, st := range map[string]*store.Store{"origin": w.origin, "destination": w.dest} {
		vs, err := VisitsByTrip(st, w.trip.TripID)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, v := range vs {
			got = append(got, v.PersonID)
		}
		if !w9aEqualStrings(w9aSortedCopy(got), []string{w.a.PersonID}) {
			t.Errorf("@%s the return's visits = %v, want only A", name, got)
		}
	}
	w.agree(t, "site-a", []string{w.a.PersonID, w.p.PersonID})
}

// TestF15ArrivalBeforeTheCheckInReplicatesIsTheAcceptedWindow pins the
// eventual-consistency edge rather than leaving it implied: if the
// destination records the arrival BEFORE P's origin check-in reaches it,
// it enters P (it had no way to know). After sync both posts read the same
// two open visits for P, and the visit derivation — not this build — owns
// what that means. This test asserts only that both posts AGREE.
func TestF15ArrivalBeforeTheCheckInReplicatesIsTheAcceptedWindow(t *testing.T) {
	w := newFix2World(t)
	fix2CheckInAt(t, w.origin, "site-a", w.p.PersonID)
	if _, err := RecordExpectedArrivalExcluding(w.dest, "site-b", 1, w.trip.TripID, nil, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	w.sync(t)
	for _, site := range []string{"site-a", "site-b"} {
		if a, b := fix2OpenAt(t, w.origin, site), fix2OpenAt(t, w.dest, site); !w9aEqualStrings(a, b) {
			t.Errorf("open visits at %s: origin reads %v, destination reads %v — the posts must agree after sync", site, a, b)
		}
	}
}
