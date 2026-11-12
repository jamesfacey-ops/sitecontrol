package deadline

// r9_clockstep_test.go — R9 (James, 2026-10-04 — FINDINGS-R4 D5): a wall
// clock STEP under the reboot bridge sends the trip to "clock changed since
// receipt — verify by radio", never a countdown moved by the step and never
// a fabricated OVERDUE.
//
// MEASURED on 2ba9188 (L05, 2026-10-04, sc-skewbuild): iceroad → pad, 65 min;
// pad restarted at +2 h read "OVERDUE 55m16s" while iceroad read "DUE IN
// 1h4m52s". On 2ba9188 these tests do not compile (Deadlines has no second
// clock to inject); the behaviour they pin is the measured one above — the
// +2 h case classifies overdue_live there, which the first assertion below
// refuses.
//
// Every test drives BOTH clocks: WallNow (what the box's clock says) and
// Uptime (CLOCK_BOOTTIME — what really elapsed on this boot). Time passing
// honestly moves both by the same amount; a step moves only the wall.

import (
	"strings"
	"testing"
	"time"
)

// twoClocks is a fake (wall, boot) pair. Pass(d) is real time passing;
// Step(d) is someone setting the wall clock; Reboot starts a new boot.
type twoClocks struct {
	wall time.Time
	up   time.Duration
	boot string
}

func newTwoClocks() *twoClocks {
	return &twoClocks{wall: time.Now(), up: 3 * time.Hour, boot: "boot-1"}
}
func (c *twoClocks) Pass(d time.Duration) { c.wall = c.wall.Add(d); c.up += d }
func (c *twoClocks) Step(d time.Duration) { c.wall = c.wall.Add(d) }
func (c *twoClocks) Reboot(offFor, upSince time.Duration) {
	c.wall = c.wall.Add(offFor + upSince)
	c.up, c.boot = upSince, c.boot+"'"
}

func (c *twoClocks) install(d *Deadlines) {
	d.WallNow = func() time.Time { return c.wall }
	d.Uptime = func() (string, time.Duration, bool) { return c.boot, c.up, true }
}

// inboundCaptured: a 65-minute A→B trip captured at B with G = 10 s, both
// clocks installed BEFORE the capture so the witness is written.
func inboundCaptured(t *testing.T, clk *twoClocks) (*Deadlines, string) {
	t.Helper()
	st := newSite(t)
	d := newScanner(t, st)
	clk.install(d)
	trip, env := dispatchInbound(t, st, 65, "driver-r9")
	d.OnApplied(env, time.Now().Add(-10*time.Second), true)
	// The restart: a fresh scanner over the same database — the in-process
	// monotonic receipt is gone and the wall bridge is all there is.
	restarted := newScanner(t, st)
	clk.install(restarted)
	return restarted, trip.TripID
}

func TestR9ForwardStepAcrossARestartIsClockChangedNeverAFabricatedOverdue(t *testing.T) {
	clk := newTwoClocks()
	d, trip := inboundCaptured(t, clk)
	clk.Pass(5 * time.Second)
	clk.Step(2 * time.Hour) // D5's repro: pad restarted at +2 h

	a := findAlert(t, mustEval(t, d), trip)
	if a.Status == StatusOverdueLive {
		t.Fatalf("+2 h step read as 2 h of travel: overdue_live by %s — D5's fabricated OVERDUE", a.OverdueBy)
	}
	if a.Status != StatusOverdueUnresolved || !a.ClockChanged || !a.DeadlineUnknown {
		t.Fatalf("status %q clockChanged %v deadlineUnknown %v (%s) — want the review lane, flagged clock-changed and deadline-unknown", a.Status, a.ClockChanged, a.DeadlineUnknown, a.Reason)
	}
	if !strings.Contains(a.Reason, "clock changed since receipt") || !strings.Contains(a.Reason, "forward") || !strings.Contains(a.Reason, "verify by radio") {
		t.Fatalf("reason %q must say the clock changed since receipt, forward, verify by radio", a.Reason)
	}
	// Never a live red: the banner's red set excludes the review lane.
	for _, b := range mustEval(t, d).Alerts {
		if b.TripID == trip && b.Status == StatusOverdueLive {
			t.Fatal("a live red was raised for a clock step")
		}
	}
}

func TestR9HonestRestartKeepsCountingOnTheBootClock(t *testing.T) {
	clk := newTwoClocks()
	d, trip := inboundCaptured(t, clk)
	clk.Pass(10 * time.Minute)
	a := findAlert(t, mustEval(t, d), trip)
	// 65 min − G 10 s − 10 min elapsed ≈ 54m50s.
	if a.Status != StatusCountdown || a.Remaining < 54*time.Minute || a.Remaining > 55*time.Minute {
		t.Fatalf("honest restart: %q remaining %s (%s), want a countdown of about 54m50s", a.Status, a.Remaining, a.Reason)
	}
	clk.Pass(60 * time.Minute)
	if a := findAlert(t, mustEval(t, d), trip); a.Status != StatusOverdueLive {
		t.Fatalf("past the allowance after an honest restart: %q (%s), want overdue_live — R9 must not swallow real reds", a.Status, a.Reason)
	}
}

// A step inside the tolerance moves NOTHING: on the same boot elapsed comes
// from the boot clock, so the countdown is exact either way.
func TestR9SubToleranceStepDoesNotMoveTheCountdown(t *testing.T) {
	for _, step := range []time.Duration{20 * time.Second, -20 * time.Second} {
		clk := newTwoClocks()
		d, trip := inboundCaptured(t, clk)
		clk.Pass(10 * time.Minute)
		clk.Step(step)
		a := findAlert(t, mustEval(t, d), trip)
		if a.Status != StatusCountdown || a.Remaining < 54*time.Minute+45*time.Second || a.Remaining > 54*time.Minute+55*time.Second {
			t.Fatalf("step %s: %q remaining %s (%s), want 54m50s ± 5 s — unmoved by the step", step, a.Status, a.Remaining, a.Reason)
		}
	}
}

// A backward step SMALLER than the time since receipt passes the pre-R9
// "wall < receipt" check and would make the countdown LONGER — the late
// direction. R9 catches it.
func TestR9BackwardStepSmallerThanTheSpanIsClockChangedNotALongerCountdown(t *testing.T) {
	clk := newTwoClocks()
	d, trip := inboundCaptured(t, clk)
	clk.Pass(40 * time.Minute)
	clk.Step(-30 * time.Minute)
	a := findAlert(t, mustEval(t, d), trip)
	if a.Status != StatusOverdueUnresolved || !a.ClockChanged || !strings.Contains(a.Reason, "back") {
		t.Fatalf("−30 min step after 40 min: %q remaining %s (%s), want clock-changed (back)", a.Status, a.Remaining, a.Reason)
	}
}

// Across a HOST reboot the boot clock restarts: only "up longer than the
// wall says has passed" is provable (the clock went back). A forward step
// across a host reboot is indistinguishable from the box being off — the
// stated residual: the bridge then reads the wall, as before R9.
func TestR9HostRebootCatchesTheBackwardStepAndStatesTheForwardResidual(t *testing.T) {
	clk := newTwoClocks()
	d, trip := inboundCaptured(t, clk)
	clk.Reboot(2*time.Minute, 30*time.Minute) // off 2 min, up 30 min: 32 min passed
	if a := findAlert(t, mustEval(t, d), trip); a.Status != StatusCountdown || a.Remaining < 32*time.Minute || a.Remaining > 33*time.Minute {
		t.Fatalf("honest reboot: %q remaining %s (%s), want about 32m50s on the wall bridge", a.Status, a.Remaining, a.Reason)
	}
	clk.Step(-20 * time.Minute) // wall now says 12 min passed, but the box has been up 30
	if a := findAlert(t, mustEval(t, d), trip); a.Status != StatusOverdueUnresolved || !a.ClockChanged {
		t.Fatalf("up 30 min but the wall says 12 min since receipt: %q (%s), want clock-changed", a.Status, a.Reason)
	}
}

// The own-authored bridge (the origin's outbound row; road work) runs on
// EVERY scan, restart or not — so a step while the process runs moved those
// rows on 2ba9188 too.
func TestR9StepWhileRunningMovesNoOwnAuthoredOutboundRow(t *testing.T) {
	st := newSite(t)
	clk := newTwoClocks()
	dA, trip, env := outboundFixture(t, st, 60, "driver-r9-out")
	clk.wall = time.UnixMilli(env.AuthoredAt.WallMS)
	clk.install(dA)
	clk.Pass(10 * time.Second)
	if a := findAlert(t, mustEval(t, dA), trip.TripID); a.Status != StatusCountdown {
		t.Fatalf("precondition: first scan %q (%s), want a countdown", a.Status, a.Reason)
	}
	clk.Pass(5 * time.Minute)
	if a := findAlert(t, mustEval(t, dA), trip.TripID); a.Status != StatusCountdown || a.Remaining < 54*time.Minute || a.Remaining > 55*time.Minute {
		t.Fatalf("honest 5 min: %q remaining %s, want about 54m50s", a.Status, a.Remaining)
	}
	clk.Step(2 * time.Hour)
	a := findAlert(t, mustEval(t, dA), trip.TripID)
	if a.Status != StatusOverdueUnresolved || !a.ClockChanged || !a.Outbound {
		t.Fatalf("+2 h step under a running origin: %q overdueBy %s (%s), want clock-changed on the outbound row", a.Status, a.OverdueBy, a.Reason)
	}
}

// A capture written before R9 (no witness row) keeps the pre-R9 bridge —
// no false "clock changed" on every trip in flight across the upgrade.
func TestR9CaptureWithoutAWitnessKeepsThePreR9Bridge(t *testing.T) {
	st := newSite(t)
	d := newScanner(t, st) // Uptime nil: the capture writes no witness
	trip, env := dispatchInbound(t, st, 65, "driver-r9-old")
	d.OnApplied(env, time.Now().Add(-10*time.Second), true)
	receipt := time.Now()

	clk := &twoClocks{wall: receipt.Add(10 * time.Minute), up: time.Hour, boot: "boot-1"}
	restarted := newScanner(t, st)
	clk.install(restarted)
	a := findAlert(t, mustEval(t, restarted), trip.TripID)
	if a.Status != StatusCountdown || a.ClockChanged || a.Remaining < 54*time.Minute || a.Remaining > 55*time.Minute+time.Second {
		t.Fatalf("pre-R9 capture after an upgrade restart: %q remaining %s (%s), want the wall-bridge countdown, no clock-changed", a.Status, a.Remaining, a.Reason)
	}
}
