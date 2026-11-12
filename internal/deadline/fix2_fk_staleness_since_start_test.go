// fix2_fk_staleness_since_start_test.go — FIX2 cluster K (sc-test L08 T-074,
// ACK-P1): Staleness is the engine's in-memory contact age, so its "never"
// means "not since this process started". After a restart during a partition
// the alert face read "last heard from ice-pad: never" for the whole cut —
// at a post holding ice-pad's records from minutes earlier — and an officer
// reads "never" as "this post has never heard from Ice Pad". With the uptime
// wired, the face scopes itself. Confirmed to FAIL against 2ba9188 (no
// Uptime hook: build failure; the hook is now ProcessUptime) and PASS after.
package deadline

import (
	"strings"
	"testing"
	"time"
)

func TestNotHeardSinceARestartSaysSinceThisPostStartedNotNever(t *testing.T) {
	st := newSite(t)
	d := newScanner(t, st)
	trip, env := dispatchInbound(t, st, 1, "driver-1")
	d.OnApplied(env, time.Now().Add(-time.Minute+30*time.Millisecond), true)
	time.Sleep(60 * time.Millisecond)

	d.Staleness = func(string) (time.Duration, bool) { return 0, false }
	d.ProcessUptime = func() time.Duration { return 3 * time.Minute }
	a := findAlert(t, mustEval(t, d), trip.TripID)
	if a.Status != StatusOverdueLive {
		t.Fatalf("status %q, want overdue_live", a.Status)
	}
	if strings.Contains(a.StalenessFace, "never") {
		t.Fatalf("face %q still says never after a restart", a.StalenessFace)
	}
	// ⟦was: "not heard from site-a …" — FIX5: capitalised, its own sentence⟧
	if want := "Not heard from site-a since this post started 3 minutes ago"; a.StalenessFace != want {
		t.Fatalf("face = %q, want %q", a.StalenessFace, want)
	}
}
