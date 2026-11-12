package deadline

// road_work_test.go — the scan over WORKING-ON-THE-ROAD runs (James,
// 2026-10-03). The post under test is `here` and it OWNS the runs it sends
// out: the time basis is the log's own authored_at (captureOwnAuthored), so
// the tests move the wall clock through WallNow — the same injectable the
// reboot-bridge tests use — and never sleep.

import (
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// dispatchRoadWork authors a road-work run FROM `here` with the given limit
// and returns the trip plus its envelope.
func dispatchRoadWork(t *testing.T, st *store.Store, limitMinutes int64) (domain.Trip, store.Envelope) {
	t.Helper()
	// A real roster person: the check-in that closes the run (the ordinary
	// expected-arrival door) refuses a dangling driver id.
	driver, err := domain.CreatePerson(st, vehicleHome, 1, "Grader Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	trip, err := domain.Dispatch(st, here, 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, st, vehicleHome, "GRADER 7"), DriverPersonID: driver.PersonID,
		RoadWork: true, DeadlineMinutes: &limitMinutes,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	envs, err := st.Events(here, 1, trip.Seq, 1)
	if err != nil || len(envs) != 1 {
		t.Fatalf("fetch trip envelope: %v (%d)", err, len(envs))
	}
	return trip, envs[0]
}

func TestRoadWorkIsWorkingNotOverdueBeforeItsLimitAndOverdueOnceTheLimitPasses(t *testing.T) {
	st := newSite(t)
	d := newScanner(t, st)
	trip, env := dispatchRoadWork(t, st, 720)
	authored := time.UnixMilli(env.AuthoredAt.WallMS)
	// Rule 6: the envelope carries a plausible authoring instant — the time
	// basis the whole test rests on. If it were zero the arithmetic below
	// would be against 1970 and every verdict would be "overdue" by vacuity.
	if authored.Before(sanityFloor) {
		t.Fatalf("precondition: authored_at %v is before the sanity floor", authored)
	}

	// Eleven hours in: working, roughly an hour left of twelve.
	d.WallNow = func() time.Time { return authored.Add(11 * time.Hour) }
	a := findAlert(t, mustEval(t, d), trip.TripID)
	if !a.RoadWork {
		t.Fatal("the row is not marked RoadWork")
	}
	if a.Status != StatusCountdown {
		t.Fatalf("at 11 h of a 12 h limit: status %q (%s), want countdown", a.Status, a.Reason)
	}
	if a.Remaining < 55*time.Minute || a.Remaining > 65*time.Minute {
		t.Fatalf("remaining %s, want about an hour", a.Remaining)
	}
	if a.Origin != here || a.LimitText() != "12 h" {
		t.Fatalf("origin %q limit %q — want this post, 12 h", a.Origin, a.LimitText())
	}
	if len(mustEval(t, d).Banner()) != 0 {
		t.Fatal("a working run raised a banner row")
	}
	// The capture row is the own-authored basis: receipt == authoring, G == 0.
	k := key{origin: env.SiteID, epoch: env.NodeEpoch, seq: env.Seq}
	c := captureRow(t, d, k)
	if c.receiptWallMs != env.AuthoredAt.WallMS || !c.anchorOK || !c.gMs.Valid || c.gMs.Int64 != 0 || !c.xReceiptMs.Valid || c.xReceiptMs.Int64 != 720*60_000 {
		t.Fatalf("capture row %+v — want receipt=authored_at (%d), anchor_ok, G=0, X=720 min", c, env.AuthoredAt.WallMS)
	}

	// Twelve hours and one minute: overdue, live, by a minute.
	d.WallNow = func() time.Time { return authored.Add(12*time.Hour + time.Minute) }
	board := mustEval(t, d)
	a = findAlert(t, board, trip.TripID)
	if a.Status != StatusOverdueLive {
		t.Fatalf("at 12 h 1 min: status %q (%s), want overdue_live", a.Status, a.Reason)
	}
	if a.OverdueBy < 30*time.Second || a.OverdueBy > 90*time.Second {
		t.Fatalf("overdue by %s, want about a minute", a.OverdueBy)
	}
	if a.StalenessFace != "" {
		// MEASURED on L04 before this guard: the own row read "last heard
		// from ice-road: never" — a link measure on a run with no link.
		t.Fatalf("an own-authored run carries a staleness face: %q", a.StalenessFace)
	}
	banner := board.Banner()
	if len(banner) != 1 || banner[0].TripID != trip.TripID || !banner[0].RoadWork {
		t.Fatalf("banner = %+v, want the one road-work alert", banner)
	}
}

func TestAnAcknowledgedRoadWorkAlertLeavesTheBannerAndACheckInClosesIt(t *testing.T) {
	st := newSite(t)
	d := newScanner(t, st)
	trip, env := dispatchRoadWork(t, st, 2)
	authored := time.UnixMilli(env.AuthoredAt.WallMS)
	d.WallNow = func() time.Time { return authored.Add(3 * time.Minute) }
	if a := findAlert(t, mustEval(t, d), trip.TripID); a.Status != StatusOverdueLive {
		t.Fatalf("precondition: status %q, want overdue_live", a.Status)
	}
	by := "Ice Road"
	if err := Acknowledge(st, here, 1, trip.TripID, &by, 0, "default"); err != nil {
		t.Fatal(err)
	}
	board := mustEval(t, d)
	a := findAlert(t, board, trip.TripID)
	if !a.Acked || !strings.Contains(a.AckFace, "Ice Road") {
		t.Fatalf("after ack: Acked=%v AckFace=%q", a.Acked, a.AckFace)
	}
	if len(board.Banner()) != 0 {
		t.Fatalf("banner still carries %d row(s) after the ack", len(board.Banner()))
	}
	// The grader checks back in: the run closes and leaves the board.
	if _, err := domain.RecordExpectedArrival(st, here, 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	for _, row := range mustEval(t, d).Alerts {
		if row.TripID == trip.TripID {
			t.Fatalf("the run is still on the board after checking back in: %+v", row)
		}
	}
}

func TestAnotherPostsRoadWorkIsNotOnThisPostsBoardAndTheReplayGateCoversOurOwn(t *testing.T) {
	st := newSite(t)
	d := newScanner(t, st)
	// A run sent out by `origin` (the other post), replicated here: the
	// origin owns its clock — nothing for this scanner to say about it.
	limit := int64(720)
	foreign, err := domain.Dispatch(st, origin, 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, st, vehicleHome, "GRADER 2"), DriverPersonID: "driver-f",
		RoadWork: true, DeadlineMinutes: &limit,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	own, _ := dispatchRoadWork(t, st, 720)
	board := mustEval(t, d)
	for _, a := range board.Alerts {
		if a.TripID == foreign.TripID {
			t.Fatalf("the other post's road-work run is on this post's board: %+v", a)
		}
	}
	findAlert(t, board, own.TripID) // ours is
	// §9.4: while a from-zero replay runs, our own run is review, never live.
	d.SetReplaying(true)
	if a := findAlert(t, mustEval(t, d), own.TripID); a.Status != StatusOverdueUnresolved || !strings.Contains(a.Reason, "replay") {
		t.Fatalf("under the replay gate: status %q reason %q", a.Status, a.Reason)
	}
}

// captureRow reads the raw persisted capture row for a source key — the
// frozen-fields assertion surface. ⟦was: defined in phase_test.go, deleted
// with weather phases (James, 2026-10-04); re-homed here, unchanged⟧
func captureRow(t *testing.T, d *Deadlines, k key) capture {
	t.Helper()
	caps, err := d.loadCaptures()
	if err != nil {
		t.Fatal(err)
	}
	c, ok := caps[k]
	if !ok {
		t.Fatalf("no capture row for %+v", k)
	}
	return c
}
