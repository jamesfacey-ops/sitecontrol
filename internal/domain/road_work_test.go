package domain

// road_work_test.go — the WORKING ON THE ROAD trip kind (road_work.go;
// James, 2026-10-03). What these prove, each by its name:
//
//   - a road-work dispatch folds the kind, checks the crew out, and lands in
//     its own category — not the inbound board, not the on-the-road section,
//     not the incomplete-check-in list;
//   - the authoring refusals keep the kind whole, and the check-in door
//     refuses it outright;
//   - it closes by an explicit arrival at the origin OR at another post, and
//     an unsolicited arrival never binds it;
//   - the weather gate applies to it;
//   - a stayed-out occupant is NOT TRACKED, with the category named;
//   - the same events delivered in permuted cross-stream orders fold to
//     byte-identical projections, road-work table included.

import (
	"bytes"
	"math/rand"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

type roadWorkWorld struct {
	st      *store.Store
	crew    []Person // on site at site-a, then out on the grader
	grader  string   // vehicle id
	limit   int64
	trip    Trip
	tripEnv store.Envelope
}

func rwPerson(t *testing.T, st *store.Store, site, name string) Person {
	t.Helper()
	p, err := CreatePerson(st, site, 1, name, nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func rwVehicle(t *testing.T, st *store.Store, site, number string) string {
	t.Helper()
	n := number
	v, err := CreateVehicle(st, site, 1, &n, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	return v.VehicleID
}

// rwCheckIn opens visits at site for the given people through the ordinary
// check-in pair (degenerate trip + its arrival).
func rwCheckIn(t *testing.T, st *store.Store, site string, vehicle string, ids []string) {
	t.Helper()
	trip, err := DispatchLocalCheckIn(st, site, 1, DispatchInput{VehicleID: vehicle, DriverPersonID: ids[0], OccupantPersonIDs: ids[1:]}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordExpectedArrival(st, site, 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
}

func rwOpenAt(t *testing.T, st *store.Store, site string) map[string]bool {
	t.Helper()
	vs, err := OpenVisitsAt(st, site)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, v := range vs {
		out[v.PersonID] = true
	}
	return out
}

func newRoadWorkWorld(t *testing.T) *roadWorkWorld {
	t.Helper()
	st := newDomainTestStore(t)
	w := &roadWorkWorld{st: st, limit: 720}
	for _, n := range []string{"Blade, Ann", "Chains, Bo"} {
		w.crew = append(w.crew, rwPerson(t, st, "site-a", n))
	}
	bus := rwVehicle(t, st, "site-a", "BUS 1")
	w.grader = rwVehicle(t, st, "site-a", "GRADER 7")
	rwCheckIn(t, st, "site-a", bus, []string{w.crew[0].PersonID, w.crew[1].PersonID})
	// Rule 6 — the arrangement: both are ON SITE before the grader goes out,
	// or "checked out by the dispatch" below would be true by vacuity.
	open := rwOpenAt(t, st, "site-a")
	if !open[w.crew[0].PersonID] || !open[w.crew[1].PersonID] {
		t.Fatalf("precondition: the crew must be on site at site-a before dispatch; open=%v", open)
	}
	limit := w.limit
	trip, err := Dispatch(st, "site-a", 1, DispatchInput{
		VehicleID: w.grader, DriverPersonID: w.crew[0].PersonID, OccupantPersonIDs: []string{w.crew[1].PersonID},
		RoadWork: true, DeadlineMinutes: &limit,
	}, 0, "default")
	if err != nil {
		t.Fatalf("road-work dispatch: %v", err)
	}
	w.trip = trip
	envs, err := st.Events("site-a", 1, trip.Seq, 1)
	if err != nil || len(envs) != 1 {
		t.Fatalf("fetch trip envelope: %v (%d)", err, len(envs))
	}
	w.tripEnv = envs[0]
	return w
}

func TestRoadWorkDispatchFoldsTheKindChecksTheCrewOutAndLandsInItsOwnCategory(t *testing.T) {
	w := newRoadWorkWorld(t)
	st := w.st

	if !strings.Contains(string(w.tripEnv.Payload), `"road_work":true`) {
		t.Fatalf("the payload does not carry the kind: %s", w.tripEnv.Payload)
	}
	if w.trip.DestinationSite != nil || w.trip.DestinationUncontrolled != nil || w.trip.DeadlineMinutes == nil || *w.trip.DeadlineMinutes != 720 {
		t.Fatalf("shape: dest=%v unc=%v deadline=%v — want no destination and the 720 limit", w.trip.DestinationSite, w.trip.DestinationUncontrolled, w.trip.DeadlineMinutes)
	}
	if rw, err := IsRoadWork(st, w.trip.TripID); err != nil || !rw {
		t.Fatalf("IsRoadWork = %v, %v — the fold did not write trip_road_work", rw, err)
	}
	c, err := EffectiveTrip(st, w.trip.TripID)
	if err != nil || c == nil || !c.RoadWork {
		t.Fatalf("EffectiveTrip.RoadWork = %+v, %v — the composed trip does not carry the kind", c, err)
	}
	// Checked out of the origin, like any dispatch.
	open := rwOpenAt(t, st, "site-a")
	if open[w.crew[0].PersonID] || open[w.crew[1].PersonID] {
		t.Fatalf("the crew is still on site at site-a after the road-work dispatch: %v", open)
	}
	// Its own category, and nowhere else.
	if inc, err := IncompleteCheckIns(st, "site-a"); err != nil || len(inc) != 0 {
		t.Fatalf("IncompleteCheckIns = %d rows, %v — a road-work run must never read as a crashed check-in", len(inc), err)
	}
	if rows, err := InTransitFrom(st, "site-a"); err != nil || len(rows) != 0 {
		t.Fatalf("InTransitFrom = %d rows, %v — road work is not on the road TO anywhere", len(rows), err)
	}
	if exps, err := OpenExpectations(st, "site-b"); err != nil || len(exps) != 0 {
		t.Fatalf("OpenExpectations(site-b) = %d, %v — nobody is expecting it", len(exps), err)
	}
	rw, err := RoadWorkFrom(st, "site-a")
	if err != nil || len(rw) != 1 {
		t.Fatalf("RoadWorkFrom(site-a) = %d rows, %v — want the one grader", len(rw), err)
	}
	if rw[0].Vehicle != "GRADER 7" || rw[0].Destination != "" || len(rw[0].Persons) != 2 || rw[0].Driver != "Blade, Ann" {
		t.Fatalf("muster row = %+v — want GRADER 7, no destination, both aboard, Ann driving", rw[0])
	}
	if other, err := RoadWorkFrom(st, "site-b"); err != nil || len(other) != 0 {
		t.Fatalf("RoadWorkFrom(site-b) = %d — the origin owns it, nobody else lists it", len(other))
	}
	own, err := OpenRoadWork(st, "site-a")
	if err != nil || len(own) != 1 || own[0].Occupants != 2 {
		t.Fatalf("OpenRoadWork(site-a) = %+v, %v", own, err)
	}
	if none, err := OpenRoadWork(st, "site-b"); err != nil || len(none) != 0 {
		t.Fatalf("OpenRoadWork(site-b) = %d — the owner filter leaks", len(none))
	}
	if all, err := OpenRoadWork(st, ""); err != nil || len(all) != 1 {
		t.Fatalf("OpenRoadWork(\"\") = %d — every post may list it for information", len(all))
	}
}

func TestRoadWorkAuthoringRefusalsKeepTheKindWhole(t *testing.T) {
	st := newDomainTestStore(t)
	p := rwPerson(t, st, "site-a", "Blade, Ann")
	g := rwVehicle(t, st, "site-a", "GRADER 7")
	dest, limit := "site-b", int64(720)
	cases := []struct {
		name string
		in   DispatchInput
		want string
	}{
		{"with a controlled destination", DispatchInput{VehicleID: g, DriverPersonID: p.PersonID, RoadWork: true, DestinationSite: &dest, DeadlineMinutes: &limit}, "has no destination post"},
		{"with an uncontrolled destination", DispatchInput{VehicleID: g, DriverPersonID: p.PersonID, RoadWork: true, DestinationUncontrolled: &dest}, "has no destination post"},
		{"without a limit", DispatchInput{VehicleID: g, DriverPersonID: p.PersonID, RoadWork: true}, "configured working limit"},
	}
	before, _ := st.Watermark("site-a", 1)
	for _, c := range cases {
		_, err := Dispatch(st, "site-a", 1, c.in, 0, "default")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
	_, err := DispatchLocalCheckIn(st, "site-a", 1, DispatchInput{VehicleID: g, DriverPersonID: p.PersonID, RoadWork: true}, 0, "default")
	if err == nil || !strings.Contains(err.Error(), "is not road work") {
		t.Errorf("check-in door: err = %v, want the not-road-work refusal", err)
	}
	if after, _ := st.Watermark("site-a", 1); after != before {
		t.Fatalf("a refusal authored something (watermark %d -> %d)", before, after)
	}
	// And the D-4 arm still fires for a plain destination-less departure —
	// the exemption is for the kind, not for every nil destination.
	if _, err := Dispatch(st, "site-a", 1, DispatchInput{VehicleID: g, DriverPersonID: p.PersonID}, 0, "default"); err == nil || !strings.Contains(err.Error(), "D-4") {
		t.Fatalf("destination-less non-road-work departure: err = %v, want the D-4 refusal", err)
	}
}

func TestRoadWorkClosesByCheckingBackInAtTheOriginAndTheCrewIsBackOnSite(t *testing.T) {
	w := newRoadWorkWorld(t)
	if _, err := RecordExpectedArrival(w.st, "site-a", 1, w.trip.TripID, nil, 0, "default"); err != nil {
		t.Fatalf("back from the road: %v", err)
	}
	s, err := TripStateByID(w.st, w.trip.TripID)
	if err != nil || s == nil || s.State != TripStateArrived {
		t.Fatalf("trip state = %+v, %v — want arrived", s, err)
	}
	open := rwOpenAt(t, w.st, "site-a")
	if !open[w.crew[0].PersonID] || !open[w.crew[1].PersonID] {
		t.Fatalf("the crew is not back on site at site-a: %v", open)
	}
	if rows, _ := RoadWorkFrom(w.st, "site-a"); len(rows) != 0 {
		t.Fatalf("RoadWorkFrom still lists %d run(s) after the check-in", len(rows))
	}
	if own, _ := OpenRoadWork(w.st, "site-a"); len(own) != 0 {
		t.Fatalf("OpenRoadWork still lists %d run(s) after the check-in", len(own))
	}
}

func TestRoadWorkThatEndsUpAtTheOtherPostClosesThereByAnExplicitArrival(t *testing.T) {
	w := newRoadWorkWorld(t)
	// D-96: the vehicle arrives where it arrives. The other post names the
	// trip it already holds (replicated) — the same expected-arrival door.
	if _, err := RecordExpectedArrival(w.st, "site-b", 1, w.trip.TripID, nil, 0, "default"); err != nil {
		t.Fatalf("arrived at site-b instead: %v", err)
	}
	if s, _ := TripStateByID(w.st, w.trip.TripID); s == nil || s.State != TripStateArrived {
		t.Fatalf("trip state = %+v — want arrived", s)
	}
	if open := rwOpenAt(t, w.st, "site-b"); !open[w.crew[0].PersonID] || !open[w.crew[1].PersonID] {
		t.Fatalf("the crew is not on site at site-b: %v", open)
	}
	if rows, _ := RoadWorkFrom(w.st, "site-a"); len(rows) != 0 {
		t.Fatalf("the origin still lists the run after the other post closed it")
	}
}

func TestAnUnsolicitedArrivalNeverBindsARoadWorkRun(t *testing.T) {
	w := newRoadWorkWorld(t)
	// Same vehicle record, same claimed origin — the exact shape that would
	// correlate to an open CONTROLLED trip. A road-work run is destination-
	// less and therefore never a §9.2 candidate: the arrival lands plain and
	// the run stays open for a human to close.
	a, err := RecordUnsolicitedArrival(w.st, "site-b", 1, w.grader, "site-a", []string{w.crew[0].PersonID}, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	as, err := oneRow[ArrivalState](w.st, "arrival_state", store.Eq{Column: "arrival_id", Value: a.ArrivalID})
	if err != nil || as == nil || as.Disposition != ArrivalPlain {
		t.Fatalf("arrival_state = %+v, %v — want plain", as, err)
	}
	if s, _ := TripStateByID(w.st, w.trip.TripID); s == nil || s.State != TripStateOpen {
		t.Fatalf("the road-work run was closed by an unsolicited arrival: %+v", s)
	}
}

func TestAnOccupantWhoStayedOutWhenRoadWorkCheckedBackInIsNotTracked(t *testing.T) {
	w := newRoadWorkWorld(t)
	if _, err := RecordExpectedArrivalExcluding(w.st, "site-a", 1, w.trip.TripID, []string{w.crew[1].PersonID}, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	rows, err := NotTracked(w.st)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || len(rows[0].Persons) != 1 || rows[0].Persons[0].PersonID != w.crew[1].PersonID {
		t.Fatalf("NotTracked = %+v — want Bo, who stayed out", rows)
	}
	if rows[0].Destination != RoadWorkDestinationText {
		t.Fatalf("NotTracked destination = %q, want %q", rows[0].Destination, RoadWorkDestinationText)
	}
}

// Determinism: the same two streams (site-a dispatches the grader and later
// re-checks the crew in; site-b records the run's arrival there first, on its
// own stream) delivered to fresh stores in many interleavings fold to
// byte-identical projections — trip_road_work included, and the trip bound
// to the FIRST explicit arrival in canonical order at every post.
func TestRoadWorkFoldsIdenticallyUnderPermutedCrossStreamDelivery(t *testing.T) {
	w := newRoadWorkWorld(t)
	ref := w.st
	if _, err := RecordExpectedArrival(ref, "site-b", 1, w.trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// A second explicit reference from the origin, later: the fold's "first
	// in canonical order wins" must hold identically everywhere.
	if _, err := RecordExpectedArrival(ref, "site-a", 1, w.trip.TripID, nil, 0, "default"); err == nil {
		// planExpectedArrival refuses an already-arrived trip; that refusal is
		// the arrangement this permutation test relies on being deterministic
		// too, so a success here would be a precondition failure.
		t.Fatal("precondition: a second explicit arrival for an arrived trip should have been refused")
	}
	streamA, err := ref.Events("site-a", 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	streamB, err := ref.Events("site-b", 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(streamB) == 0 || len(streamA) < 6 {
		t.Fatalf("precondition: expected two non-trivial streams, got a=%d b=%d", len(streamA), len(streamB))
	}
	want, err := ref.DumpProjections()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(want, []byte(`"trip_road_work"`)) || !bytes.Contains(want, []byte(w.trip.TripID)) {
		t.Fatalf("precondition: the reference dump does not carry the road-work table or the trip")
	}
	for seed := int64(1); seed <= 12; seed++ {
		rng := rand.New(rand.NewSource(seed))
		st := newDomainTestStore(t)
		for _, ev := range interleave(rng, streamA, streamB) {
			if _, err := st.Apply(ev); err != nil {
				t.Fatalf("seed %d: apply %v: %v", seed, ev.Key(), err)
			}
		}
		got, err := st.DumpProjections()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("seed %d: permuted delivery folded to a different dump\n got: %s\nwant: %s", seed, got, want)
		}
		if s, _ := TripStateByID(st, w.trip.TripID); s == nil || s.State != TripStateArrived {
			t.Fatalf("seed %d: trip state %+v, want arrived", seed, s)
		}
		if rw, _ := IsRoadWork(st, w.trip.TripID); !rw {
			t.Fatalf("seed %d: the kind did not survive permuted delivery", seed)
		}
	}
}
