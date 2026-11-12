package deadline

// fix16i_typed_departure_test.go — James 2026-10-06: "from the typed time".
// A vehicle checked out with a typed, earlier time out (the "differing time"
// boxes) counts its allowance FROM THE TYPED TIME at every judge, not from
// the instant the record was entered (⟦was: FINDINGS-R4 D13, authored_at⟧).
// Typed back past its allowance, it is OVERDUE at entry.
//
// Fail-before: on 1cbf34f every test below that back-dates fails on its
// remaining/overdue assertion — the scan counted from authored_at.

import (
	"testing"
	"time"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// dispatchTyped is dispatchInbound with the time out typed `back` before
// the recording (TimeSource "entered", the gate's typed-time path). The
// store's own clock stamps authored_at; the typed instant is derived from a
// reading taken just before, so authored − typed ≥ back.
func dispatchTyped(t *testing.T, st *store.Store, in domain.DispatchInput, site string, back time.Duration) (domain.Trip, store.Envelope) {
	t.Helper()
	typed := time.Now().Add(-back).UnixMilli()
	trip, err := domain.Dispatch(st, site, 1, in, typed, "entered")
	if err != nil {
		t.Fatal(err)
	}
	envs, err := st.Events(site, 1, trip.Seq, 1)
	if err != nil || len(envs) != 1 {
		t.Fatalf("fetch trip envelope: %v (%d)", err, len(envs))
	}
	env := envs[0]
	// Rule 6: the typed time really is on the replicated envelope, earlier
	// than the recording by about `back` — else every assertion is vacuous.
	if env.TimeSource != "entered" || env.OccurredAt != typed {
		t.Fatalf("precondition: envelope carries (%q, %d), want (entered, %d)", env.TimeSource, env.OccurredAt, typed)
	}
	if lead := domain.DepartureLead(env); lead < back || lead > back+5*time.Second {
		t.Fatalf("precondition: lead %s, want about %s", lead, back)
	}
	return trip, env
}

func controlledRun(t *testing.T, st *store.Store, x int64) domain.DispatchInput {
	dest := here
	return domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, st, vehicleHome, "TRUCK 16"), DriverPersonID: rosterDriver(t, st, "Typed Driver"),
		DestinationSite: &dest, DeadlineMinutes: &x,
	}
}

func near(got, want time.Duration) bool {
	d := got - want
	return d > -10*time.Second && d < 10*time.Second
}

func TestFix16iABackdatedCheckoutCountsSixtyFiveMinutesFromTheTypedTimeAtBothEnds(t *testing.T) {
	st := newSite(t)
	trip, env := dispatchTyped(t, st, controlledRun(t, st, 65), origin, 30*time.Minute)
	authored := time.UnixMilli(env.AuthoredAt.WallMS)
	typed := time.UnixMilli(env.OccurredAt)

	// The origin: its own outbound row, judged at the instant of recording.
	dA := scannerFor(t, st, origin)
	dA.WallNow = func() time.Time { return authored }
	a := findAlert(t, mustEval(t, dA), trip.TripID)
	if !a.Outbound || a.Status != StatusCountdown || !near(a.Remaining, 35*time.Minute) {
		t.Fatalf("origin at recording: outbound %v %q remaining %s (%s), want a countdown of 65 − 30 = 35 min", a.Outbound, a.Status, a.Remaining, a.Reason)
	}

	// The destination: received now over a current link (G ≈ 0).
	dB := newScanner(t, st)
	dB.OnApplied(env, time.Now(), true)
	b := findAlert(t, mustEval(t, dB), trip.TripID)
	if b.Outbound || b.Status != StatusCountdown || !near(b.Remaining, a.Remaining) {
		t.Fatalf("destination: %q remaining %s, want the origin's %s", b.Status, b.Remaining, a.Remaining)
	}

	// The deadline is typed + 65 by the recorded due time too (Q-FE2's basis).
	due, ok := dB.recordedDue(key{origin: env.SiteID, epoch: env.NodeEpoch, seq: env.Seq}, 65*time.Minute)
	if !ok || !due.Equal(typed.Add(65*time.Minute)) {
		t.Fatalf("recorded due %v (ok %v), want typed + 65 = %v", due, ok, typed.Add(65*time.Minute))
	}

	// 36 minutes after the recording (66 after the typed time): red at the origin.
	dA.WallNow = func() time.Time { return authored.Add(36 * time.Minute) }
	if a = findAlert(t, mustEval(t, dA), trip.TripID); a.Status != StatusOverdueLive || !near(a.OverdueBy, time.Minute) {
		t.Fatalf("origin at typed+66: %q by %s, want overdue_live by about a minute", a.Status, a.OverdueBy)
	}
}

func TestFix16iATripTypedBackPastItsAllowanceIsOverdueAtEntry(t *testing.T) {
	st := newSite(t)
	trip, env := dispatchTyped(t, st, controlledRun(t, st, 65), origin, 90*time.Minute)
	authored := time.UnixMilli(env.AuthoredAt.WallMS)

	dA := scannerFor(t, st, origin)
	dA.WallNow = func() time.Time { return authored }
	if a := findAlert(t, mustEval(t, dA), trip.TripID); a.Status != StatusOverdueLive || !near(a.OverdueBy, 25*time.Minute) {
		t.Fatalf("origin at entry: %q by %s (%s), want OVERDUE by 90 − 65 = 25 min at once", a.Status, a.OverdueBy, a.Reason)
	}
	dB := newScanner(t, st)
	dB.OnApplied(env, time.Now(), true)
	if b := findAlert(t, mustEval(t, dB), trip.TripID); b.Status != StatusOverdueLive || !near(b.OverdueBy, 25*time.Minute) {
		t.Fatalf("destination at receipt: %q by %s (%s), want OVERDUE by about 25 min — never the partition review lane", b.Status, b.OverdueBy, b.Reason)
	}
	// No sync anchor at receipt (Q-FE2): the recorded due time, typed + 65,
	// has passed — the same red.
	st2 := newSite(t)
	replicate(t, st, st2, vehicleHome)
	replicate(t, st, st2, origin)
	dN := newScanner(t, st2)
	dN.OnApplied(env, time.Time{}, false)
	if n := findAlert(t, mustEval(t, dN), trip.TripID); n.Status != StatusOverdueLive {
		t.Fatalf("no-anchor destination: %q (%s), want the red from the recorded due time typed + 65", n.Status, n.Reason)
	}
}

func TestFix16iRoadWorkCountsItsLimitFromTheTypedTime(t *testing.T) {
	st := newSite(t)
	driver, err := domain.CreatePerson(st, vehicleHome, 1, "Grader Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	limit := int64(120)
	trip, env := dispatchTyped(t, st, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, st, vehicleHome, "GRADER 16"), DriverPersonID: driver.PersonID,
		RoadWork: true, DeadlineMinutes: &limit,
	}, here, 45*time.Minute)
	d := newScanner(t, st)
	d.WallNow = func() time.Time { return time.UnixMilli(env.AuthoredAt.WallMS) }
	a := findAlert(t, mustEval(t, d), trip.TripID)
	if !a.RoadWork || a.Status != StatusCountdown || !near(a.Remaining, 75*time.Minute) {
		t.Fatalf("road work at recording: roadwork %v %q remaining %s, want 120 − 45 = 75 min", a.RoadWork, a.Status, a.Remaining)
	}
}

func TestFix16iAFutureTypedTimeNeverExtendsTheAllowance(t *testing.T) {
	st := newSite(t)
	in := controlledRun(t, st, 65)
	typed := time.Now().Add(4 * time.Minute).UnixMilli() // within the 5-minute skew allowance: accepted
	trip, err := domain.Dispatch(st, origin, 1, in, typed, "entered")
	if err != nil {
		t.Fatal(err)
	}
	envs, _ := st.Events(origin, 1, trip.Seq, 1)
	env := envs[0]
	if env.OccurredAt <= env.AuthoredAt.WallMS {
		t.Fatalf("precondition: typed %d is not after authored %d", env.OccurredAt, env.AuthoredAt.WallMS)
	}
	if lead := domain.DepartureLead(env); lead != 0 {
		t.Fatalf("lead %s for a future typed time, want 0", lead)
	}
	dA := scannerFor(t, st, origin)
	dA.WallNow = func() time.Time { return time.UnixMilli(env.AuthoredAt.WallMS) }
	if a := findAlert(t, mustEval(t, dA), trip.TripID); a.Status != StatusCountdown || !near(a.Remaining, 65*time.Minute) {
		t.Fatalf("%q remaining %s, want the full 65 min from the recording", a.Status, a.Remaining)
	}
}

func TestFix16iADefaultTimeTripIsUnchanged(t *testing.T) {
	st := newSite(t)
	trip, env := dispatchInbound(t, st, 65, rosterDriver(t, st, "Default Driver"))
	if domain.DepartureLead(env) != 0 || !domain.EffectiveDeparture(env).Equal(time.UnixMilli(env.AuthoredAt.WallMS)) {
		t.Fatalf("a default-time dispatch moved its basis: lead %s", domain.DepartureLead(env))
	}
	dA := scannerFor(t, st, origin)
	dA.WallNow = func() time.Time { return time.UnixMilli(env.AuthoredAt.WallMS).Add(5 * time.Minute) }
	if a := findAlert(t, mustEval(t, dA), trip.TripID); a.Status != StatusCountdown || !near(a.Remaining, 60*time.Minute) {
		t.Fatalf("%q remaining %s, want 60 min", a.Status, a.Remaining)
	}
}
