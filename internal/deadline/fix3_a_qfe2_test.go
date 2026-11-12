package deadline

// fix3_a_qfe2_test.go — FIX3 stream A, the deadline half.
//
//	Q-FE2 (James, 2026-10-04: "no-anchor/clock rows stay AMBER, relabel
//	       VERIFY BY RADIO, show recorded due time, turn RED when that due
//	       time passes"). FINDINGS-R5 A6 MEASURED on fix2/integ: a run
//	       received before the destination had a sync anchor read amber
//	       "OVERDUE — UNRESOLVED" for the trip's whole life; the destination
//	       never went red. On e3d507d these tests do not compile (no DueAt,
//	       no ReviewLabel); the behaviour they pin is the measured one —
//	       branch 3 returned before any time test, so the "red at due+1 s"
//	       assertions fail on it.
//	F4     a red row whose only standing ack is an amber ack is NOT
//	       un-ackable and says so.
//	F8     the R9 sentence rounds the step.

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// noAnchorCaptured: an A→B run of X minutes, captured at B with NO sync
// anchor (the destination had just restarted), on a store at a known path
// so a test can reopen it. Returns the scanner, trip id, recorded due time
// and the store path.
func noAnchorCaptured(t *testing.T, xMinutes int64) (*Deadlines, string, time.Time, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "qfe2.db")
	st := openSiteAt(t, path)
	d := newScanner(t, st)
	trip, env := dispatchInbound(t, st, xMinutes, "driver-qfe2")
	d.OnApplied(env, time.Time{}, false)
	due := time.UnixMilli(env.AuthoredAt.WallMS).Add(time.Duration(xMinutes) * time.Minute)
	return d, trip.TripID, due, path
}

func openSiteAt(t *testing.T, path string) *store.Store {
	t.Helper()
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	if err := Register(st); err != nil {
		t.Fatal(err)
	}
	return st
}

func at(tm time.Time) func() time.Time { return func() time.Time { return tm } }

// (1) the amber label and the due time.
func TestQFE2NoAnchorRowIsVerifyByRadioWithTheRecordedDueTimeNeverOverdue(t *testing.T) {
	d, trip, due, _ := noAnchorCaptured(t, 65)
	d.WallNow = at(due.Add(-60 * time.Minute))
	a := findAlert(t, mustEval(t, d), trip)
	if a.Status != StatusOverdueUnresolved {
		t.Fatalf("five minutes into a 65-minute run: %q, want the amber review row", a.Status)
	}
	if got := a.ReviewLabel(); got != "VERIFY BY RADIO — no receipt timing at this post" {
		t.Fatalf("label %q", got)
	}
	if strings.Contains(strings.ToUpper(a.ReviewLabel()+" "+a.Reason), "OVERDUE") {
		t.Fatalf("the amber row says OVERDUE: label %q reason %q", a.ReviewLabel(), a.Reason)
	}
	if !a.DueAt.Equal(due) {
		t.Fatalf("DueAt %s, want the recorded due %s (authored_at + 65 min)", a.DueAt, due)
	}
	if a.DueClock() != due.In(time.Local).Format("15:04") {
		t.Fatalf("DueClock %q", a.DueClock())
	}
}

// (2) red at due+1 s and not at due−1 s, then the red is the ordinary red.
func TestQFE2NoAnchorRowTurnsRedWhenTheRecordedDueTimePasses(t *testing.T) {
	d, trip, due, _ := noAnchorCaptured(t, 65)
	d.WallNow = at(due.Add(-time.Second))
	if a := findAlert(t, mustEval(t, d), trip); a.Status != StatusOverdueUnresolved {
		t.Fatalf("at due−1 s: %q, want amber", a.Status)
	}
	d.WallNow = at(due.Add(time.Second))
	b := mustEval(t, d)
	a := findAlert(t, b, trip)
	if a.Status != StatusOverdueLive || a.OverdueBy != time.Second {
		t.Fatalf("at due+1 s: %q overdue by %s, want overdue_live by 1s", a.Status, a.OverdueBy)
	}
	if a.DeadlineUnknown || a.Reason != "" || !a.DueAt.Equal(due) {
		t.Fatalf("the red still carries review fields: unknown %v reason %q due %s", a.DeadlineUnknown, a.Reason, a.DueAt)
	}
	if len(b.Banner()) != 1 {
		t.Fatalf("banner rows %d, want the red", len(b.Banner()))
	}
	if a.StalenessFace == "" {
		t.Fatal("the red carries no staleness face (§9.3 rule 3 — Staleness is nil, so it must read unknown)")
	}
}

// (3) an amber ack made before the due time does not silence the later red.
func TestQFE2APreDueAmberAckLeavesTheLaterRedStanding(t *testing.T) {
	d, trip, due, _ := noAnchorCaptured(t, 65)
	d.WallNow = at(due.Add(-10 * time.Minute))
	by := "Ice Pad"
	if err := AcknowledgeKind(d.st, here, 1, trip, AckKindAmber, &by, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if a := findAlert(t, mustEval(t, d), trip); !a.Acked {
		t.Fatal("precondition: the amber ack silences the amber row")
	}
	d.WallNow = at(due.Add(2 * time.Second))
	b := mustEval(t, d)
	a := findAlert(t, b, trip)
	if a.Status != StatusOverdueLive || a.Acked || len(b.Banner()) != 1 {
		t.Fatalf("after the due time: %q acked %v banner %d — the pre-due amber ack silenced the red", a.Status, a.Acked, len(b.Banner()))
	}
	if a.Unackable() {
		t.Fatal("F4: the red row is un-ackable on the strength of an amber ack that does not silence it")
	}
	if a.VerifyOnlyAckFace != "acknowledged at Ice Pad as VERIFY only — this red is not acknowledged" {
		t.Fatalf("VerifyOnlyAckFace %q", a.VerifyOnlyAckFace)
	}
}

// (4) the state after a store reopen: nothing lives in memory.
func TestQFE2TheVerdictSurvivesAStoreReopen(t *testing.T) {
	d, trip, due, path := noAnchorCaptured(t, 2)
	if err := d.st.Close(); err != nil {
		t.Fatal(err)
	}
	st := openSiteAt(t, path)
	r := newScanner(t, st)
	r.WallNow = at(due.Add(-30 * time.Second))
	if a := findAlert(t, mustEval(t, r), trip); a.Status != StatusOverdueUnresolved || !a.DueAt.Equal(due) {
		t.Fatalf("reopened, before due: %q due %s", a.Status, a.DueAt)
	}
	r.WallNow = at(due.Add(30 * time.Second))
	if a := findAlert(t, mustEval(t, r), trip); a.Status != StatusOverdueLive {
		t.Fatalf("reopened, after due: %q, want overdue_live", a.Status)
	}
}

// On the receipt's boot the no-anchor comparison reads the boot clock, so a
// wall step after receipt cannot raise (or hold off) the red.
func TestQFE2NoAnchorRowIgnoresAWallStepOnTheSameBoot(t *testing.T) {
	st := newSite(t)
	d := newScanner(t, st)
	clk := newTwoClocks()
	trip, env := dispatchInbound(t, st, 65, "driver-qfe2-step")
	clk.wall = time.UnixMilli(env.AuthoredAt.WallMS)
	clk.install(d)
	d.OnApplied(env, time.Time{}, false)
	clk.Pass(10 * time.Minute)
	clk.Step(2 * time.Hour)
	if a := findAlert(t, mustEval(t, d), trip.TripID); a.Status != StatusOverdueUnresolved {
		t.Fatalf("+2 h wall step 10 min into a 65-min run: %q — the step raised the red", a.Status)
	}
	clk.Pass(56 * time.Minute)
	if a := findAlert(t, mustEval(t, d), trip.TripID); a.Status != StatusOverdueLive {
		t.Fatalf("66 min on the boot clock: %q, want overdue_live", a.Status)
	}
}

// (5) R9 clock-changed rows: red on the boot clock with the wall stepped
// ±2 h; amber for good on a different boot or with no boot facts.
func TestQFE2ClockChangedRowTurnsRedOnTheBootClockOnly(t *testing.T) {
	for _, step := range []time.Duration{2 * time.Hour, -2 * time.Hour} {
		clk := newTwoClocks()
		d, trip := inboundCaptured(t, clk) // 65 min, G ≈ 10 s, restarted
		clk.Pass(5 * time.Second)
		clk.Step(step)
		a := findAlert(t, mustEval(t, d), trip)
		if a.Status != StatusOverdueUnresolved || !a.ClockChanged {
			t.Fatalf("step %s: %q clockChanged %v, want the amber clock-changed row", step, a.Status, a.ClockChanged)
		}
		if a.DueAt.IsZero() || a.ReviewLabel() != ClockChangedLabel {
			t.Fatalf("step %s: due %s label %q", step, a.DueAt, a.ReviewLabel())
		}
		clk.Pass(64 * time.Minute) // boot clock: 64m05s since receipt, X−G ≈ 64m50s
		if a := findAlert(t, mustEval(t, d), trip); a.Status != StatusOverdueUnresolved {
			t.Fatalf("step %s, 45 s before the boot-clock due: %q, want amber", step, a.Status)
		}
		clk.Pass(time.Minute)
		a = findAlert(t, mustEval(t, d), trip)
		if a.Status != StatusOverdueLive || a.OverdueBy < 10*time.Second || a.OverdueBy > 20*time.Second {
			t.Fatalf("step %s, past the boot-clock due: %q overdue by %s, want overdue_live by ≈15 s", step, a.Status, a.OverdueBy)
		}
	}
}

func TestQFE2ClockChangedRowStaysAmberForGoodWithoutASameBootWitness(t *testing.T) {
	// A different boot: the host rebooted and the wall went back.
	clk := newTwoClocks()
	d, trip := inboundCaptured(t, clk)
	clk.Reboot(0, 30*time.Minute)
	clk.Step(-2 * time.Hour)
	for i := 0; i < 3; i++ {
		a := findAlert(t, mustEval(t, d), trip)
		if a.Status != StatusOverdueUnresolved || !a.ClockChanged {
			t.Fatalf("another boot, +%dh: %q clockChanged %v, want amber for good", i*3, a.Status, a.ClockChanged)
		}
		clk.Pass(3 * time.Hour)
	}

	// No boot facts: a capture with no witness, an implausible clock.
	st := newSite(t)
	old := newScanner(t, st) // Uptime nil: no witness
	trip2, env := dispatchInbound(t, st, 1, "driver-qfe2-legacy")
	old.OnApplied(env, time.Now().Add(-10*time.Second), true)
	r := newScanner(t, st)
	clk2 := &twoClocks{wall: time.Unix(60, 0), up: 5 * time.Hour, boot: "boot-1"}
	clk2.install(r)
	if a := findAlert(t, mustEval(t, r), trip2.TripID); a.Status != StatusOverdueUnresolved || !a.ClockChanged {
		t.Fatalf("no witness, implausible clock, 5 h up: %q — must stay amber, never red on arithmetic", a.Status)
	}
}

// The branch-4 row (deadline passed during a partition) keeps its verdict
// and label; it only gains the due time.
func TestQFE2PartitionExpiredRowKeepsItsLabelAndGainsTheDueTime(t *testing.T) {
	st := newSite(t)
	d := newScanner(t, st)
	trip, env := dispatchInbound(t, st, 1, "driver-qfe2-b4")
	d.OnApplied(env, time.Now().Add(-3*time.Minute), true)
	a := findAlert(t, mustEval(t, d), trip.TripID)
	if a.Status != StatusOverdueUnresolved || a.ReviewLabel() != "OVERDUE — UNRESOLVED" || a.DueAt.IsZero() {
		t.Fatalf("%q label %q due %s", a.Status, a.ReviewLabel(), a.DueAt)
	}
}

// F4: on the amber row the amber ack DOES silence it, so un-ack is offered.
func TestF4AnAmberAckOnAnAmberRowIsUnackable(t *testing.T) {
	d, trip, due, _ := noAnchorCaptured(t, 65)
	d.WallNow = at(due.Add(-time.Minute))
	by := "Ice Pad"
	if err := AcknowledgeKind(d.st, here, 1, trip, AckKindAmber, &by, 0, "default"); err != nil {
		t.Fatal(err)
	}
	a := findAlert(t, mustEval(t, d), trip)
	if !a.Unackable() || a.VerifyOnlyAckFace != "" {
		t.Fatalf("amber row, amber ack: unackable %v face %q", a.Unackable(), a.VerifyOnlyAckFace)
	}
}

// F8: the step is rounded, never truncated.
func TestF8ClockChangedReasonRoundsTheStep(t *testing.T) {
	for _, s := range []time.Duration{
		time.Hour + 59*time.Minute, time.Hour + 59*time.Minute + 52*time.Second,
		2 * time.Hour, 2*time.Hour + 30*time.Second, 2*time.Hour + time.Minute,
	} {
		for _, step := range []time.Duration{s, -s} {
			got := clockChangedReason(step)
			if !strings.Contains(got, "by about 2 hours ") {
				t.Errorf("step %s: %q, want \"about 2 hours\"", step, got)
			}
		}
	}
	if got := clockChangedReason(59*time.Minute + 40*time.Second); !strings.Contains(got, "about 1 hour ") {
		t.Errorf("59m40s: %q, want about 1 hour", got)
	}
	if got := clockChangedReason(45 * time.Minute); !strings.Contains(got, "about 45 minutes ") {
		t.Errorf("45m: %q", got)
	}
}
