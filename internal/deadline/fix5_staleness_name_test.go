// fix5_staleness_name_test.go — FIX5 (R7 L01 R7-4a / L05, 2026-10-05): the
// red row's staleness face read "… (driver Calddahl, Oakley). not heard from
// ice-pad since this post started 8 seconds ago." — the raw far-post id, in
// a lower-case sentence after the row's full stop. It now names the post the
// way the rest of the row does (the store's SiteNamer, which the web layer
// installs from config — the templates' sitelabel) and opens with a capital.
// FAILS on 35a0024 (face = "not heard from site-a since …").
package deadline

import (
	"strings"
	"testing"
	"time"
)

func TestFix5StalenessFaceNamesTheFarPostByDisplayNameAsASentence(t *testing.T) {
	st := newSite(t)
	st.SiteNamer = func(id string) string {
		if id == "site-a" {
			return "Ice Pad"
		}
		return id
	}
	d := newScanner(t, st)
	trip, env := dispatchInbound(t, st, 1, "driver-1")
	d.OnApplied(env, time.Now().Add(-time.Minute+30*time.Millisecond), true)
	time.Sleep(60 * time.Millisecond)

	d.Staleness = func(string) (time.Duration, bool) { return 0, false }
	d.ProcessUptime = func() time.Duration { return 8 * time.Second }
	a := findAlert(t, mustEval(t, d), trip.TripID)
	if a.Status != StatusOverdueLive {
		t.Fatalf("status %q, want overdue_live", a.Status)
	}
	if want := "Not heard from Ice Pad since this post started 8 seconds ago"; a.StalenessFace != want {
		t.Fatalf("face = %q, want %q", a.StalenessFace, want)
	}

	// The other two arms name the post the same way.
	d.Staleness = func(string) (time.Duration, bool) { return 4 * time.Hour, true }
	if a = findAlert(t, mustEval(t, d), trip.TripID); a.StalenessFace != "Last heard from Ice Pad: 4 hours ago" {
		t.Fatalf("stale arm face = %q", a.StalenessFace)
	}
	d.Staleness = nil
	if a = findAlert(t, mustEval(t, d), trip.TripID); a.StalenessFace != "Last heard from Ice Pad: unknown" {
		t.Fatalf("unknown arm face = %q", a.StalenessFace)
	}
	if strings.Contains(a.StalenessFace, "site-a") {
		t.Fatalf("raw id on the face: %q", a.StalenessFace)
	}
}
