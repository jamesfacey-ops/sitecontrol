package deadline

// James, 2026-10-03: a TURNED-BACK truck recorded back at its origin
// (domain.RecordReturnToOrigin) — an ordinary explicit arrival on the
// origin's stream. Two real stores stand in for the two posts, envelopes
// carried through store.Apply (outbound_test.go's replicate): the trip closes
// at BOTH, the overdue red clears at BOTH, the crew is on site at the origin
// and nowhere else, and "returned" is derived identically at both.

import (
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

func TestATurnedBackTruckRecordedAtItsOriginClosesTheTripAndClearsTheRedAtBothPosts(t *testing.T) {
	stA := newSite(t)
	driver := rosterDriver(t, stA, "Turned Back Driver")
	dA, trip, env := outboundFixture(t, stA, 1, driver)
	stB := newSite(t)
	dB := scannerFor(t, stB, here)
	replicate(t, stA, stB, origin)
	dB.OnApplied(env, time.Now().Add(-3*time.Minute), true)
	authored := time.UnixMilli(env.AuthoredAt.WallMS)
	dA.WallNow = func() time.Time { return authored.Add(2 * time.Minute) }
	if len(mustEval(t, dA).Banner()) != 1 || len(mustEval(t, dB).Banner()) != 1 {
		t.Fatal("precondition: the overdue truck must alert at both posts")
	}

	// The destination cannot record it "back": it did not send it.
	if _, err := domain.RecordReturnToOrigin(stB, here, 1, trip.TripID, nil, nil, 0, "default", false); err == nil || !strings.Contains(err.Error(), "only the post that sent a truck out") {
		t.Fatalf("return recorded at the destination = %v, want a refusal", err)
	}

	arr, err := domain.RecordReturnToOrigin(stA, origin, 1, trip.TripID, nil, nil, 0, "default", false)
	if err != nil {
		t.Fatal(err)
	}
	if arr.SiteID != origin || arr.TripRef == nil || *arr.TripRef != trip.TripID {
		t.Fatalf("the return is %+v, want an explicit arrival at %s naming the trip", arr, origin)
	}
	if len(mustEval(t, dA).Banner()) != 0 {
		t.Fatal("the origin's red survived the truck being recorded back there")
	}
	replicate(t, stA, stB, origin)
	for _, row := range mustEval(t, dB).Alerts {
		if row.TripID == trip.TripID {
			t.Fatalf("the returned trip is still on the destination's board: %+v", row)
		}
	}
	for name, st := range map[string]*store.Store{"A": stA, "B": stB} {
		ts, err := domain.TripStateByID(st, trip.TripID)
		if err != nil || ts == nil || ts.State != domain.TripStateArrived {
			t.Fatalf("%s: trip state %+v (%v), want arrived", name, ts, err)
		}
		ret, err := domain.TripsReturnedToOrigin(st)
		if err != nil || !ret[trip.TripID] {
			t.Fatalf("%s: TripsReturnedToOrigin = %v (%v), want the trip marked returned", name, ret, err)
		}
		atA, _ := domain.OpenVisitsAt(st, origin)
		atB, _ := domain.OpenVisitsAt(st, here)
		if len(atA) != 1 || atA[0].PersonID != driver || len(atB) != 0 {
			t.Fatalf("%s: open visits at origin %d / destination %d, want the driver back at the origin only", name, len(atA), len(atB))
		}
	}

	// Already arrived: a second "back here" is refused, and so is a late
	// arrival at the destination (its own ordinary door).
	if _, err := domain.RecordReturnToOrigin(stA, origin, 1, trip.TripID, nil, nil, 0, "default", false); err == nil || !strings.Contains(err.Error(), "recorded as arrived here") {
		t.Fatalf("second return = %v, want refused as already arrived", err)
	}
	if _, err := domain.RecordExpectedArrival(stB, here, 1, trip.TripID, nil, 0, "default"); err == nil {
		t.Fatal("the destination recorded an arrival for a trip already back at its origin")
	}
}

func TestATruckThatAlreadyArrivedAtItsDestinationCannotBeRecordedBackAtTheOrigin(t *testing.T) {
	stA := newSite(t)
	_, trip, _ := outboundFixture(t, stA, 30, rosterDriver(t, stA, "Arrived Driver"))
	stB := newSite(t)
	replicate(t, stA, stB, origin)
	if _, err := domain.RecordExpectedArrival(stB, here, 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	replicate(t, stB, stA, here)
	_, err := domain.RecordReturnToOrigin(stA, origin, 1, trip.TripID, nil, nil, 0, "default", false)
	if err == nil || !strings.Contains(err.Error(), "recorded as arrived at") || strings.Contains(err.Error(), "this post") {
		t.Fatalf("return after the destination's arrival = %v, want refused naming the destination", err)
	}
	if ret, _ := domain.TripsReturnedToOrigin(stA); ret[trip.TripID] {
		t.Fatal("a trip that arrived at its destination is labelled returned")
	}
}
