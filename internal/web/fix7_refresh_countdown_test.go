package web

import (
	"regexp"
	"strings"
	"testing"
)

// FIX7 (James, 2026-10-05: "for the refreshes every 60 seconds can you give
// me a timer on it as well so they know it's something"): the two live
// analytics pages show a countdown to the next reload beside "Pause live
// updates". The meta refresh stays the mechanism; the countdown is display.
// Scripting off: the countdown is [hidden] and the static "refreshes every
// 60 s" reads as before. Paused: no countdown at all. The ticking number is
// aria-hidden so a screen reader is never told it every second.

var anCountdownEl = regexp.MustCompile(`<span class="an-countdown" data-an-countdown="60" aria-hidden="true" hidden>updates in <span class="an-countdown-n">60</span> s</span>`)

func anLiveLine(t *testing.T, body, path string) string {
	t.Helper()
	i := strings.Index(body, `<p class="an-live note"`)
	if i < 0 {
		t.Fatalf("%s: no live line", path)
	}
	j := strings.Index(body[i:], "</p>")
	return body[i : i+j+len("</p>")]
}

func TestFix7LiveAnalyticsPagesCarryARefreshCountdown(t *testing.T) {
	w := anRoadWorld(t)
	for _, path := range []string{"/analytics", "/analytics/site/site-a"} {
		code, body := anSiteGet(t, w.srv, "GET", path, true)
		if code != 200 {
			t.Fatalf("GET %s = %d", path, code)
		}
		line := anLiveLine(t, body, path)
		if !anCountdownEl.MatchString(line) {
			t.Errorf("%s: live line has no aria-hidden countdown starting at 60:\n%s", path, line)
		}
		// Countdown sits beside the pause control, before it.
		if c, p := strings.Index(line, "data-an-countdown"), strings.Index(line, `<a href="?live=0">Pause live updates</a>`); c < 0 || p < 0 || c > p {
			t.Errorf("%s: countdown must sit just before Pause live updates:\n%s", path, line)
		}
		if !strings.Contains(body, `<script id="an-countdown-script">`) {
			t.Errorf("%s: no countdown script", path)
		}
		// The meta refresh is still the mechanism, unchanged.
		if !strings.Contains(body, liveMeta) {
			t.Errorf("%s: the 60 s meta refresh is gone", path)
		}
	}
}

func TestFix7ScriptingOffShowsTheStaticIntervalNotACountdown(t *testing.T) {
	w := anRoadWorld(t)
	for _, path := range []string{"/analytics", "/analytics/site/site-a"} {
		_, body := anSiteGet(t, w.srv, "GET", path, true)
		line := anLiveLine(t, body, path)
		// Visible in markup (no hidden / sr-only) — what a no-script reader sees.
		if !strings.Contains(line, `Live — <span data-an-every>refreshes every 60 s</span>`) {
			t.Errorf("%s: scripting-off text \"refreshes every 60 s\" is not plainly visible:\n%s", path, line)
		}
		// The countdown only appears when script un-hides it.
		if !regexp.MustCompile(`data-an-countdown="60"[^>]*\bhidden>`).MatchString(line) {
			t.Errorf("%s: countdown is not [hidden] in markup, so scripting-off would show a frozen number:\n%s", path, line)
		}
	}
}

// Only when live: the same world's paused pages and non-refreshing pages
// carry none (folded in here so this guard also fails on the base, which
// has no countdown anywhere).
func TestFix7CountdownOnlyWhenLive(t *testing.T) {
	w := anRoadWorld(t)
	if _, body := anSiteGet(t, w.srv, "GET", "/analytics", true); !strings.Contains(body, `data-an-countdown="60"`) {
		t.Fatalf("live /analytics has no countdown — the negative checks below would prove nothing")
	}
	for _, path := range []string{"/analytics?live=0", "/analytics/site/site-a?live=0", "/gate", "/trips", "/muster", "/status"} {
		_, body := anSiteGet(t, w.srv, "GET", path, true)
		if strings.Contains(body, `data-an-countdown="`) || strings.Contains(body, "updates in <span") {
			t.Errorf("%s: carries a refresh countdown but does not refresh itself", path)
		}
	}
	_, body := anSiteGet(t, w.srv, "GET", "/analytics?live=0", true)
	if !strings.Contains(body, "<strong>Live updates paused</strong>") || !strings.Contains(body, `<a href="?live=1">Resume</a>`) {
		t.Errorf("paused /analytics lost its paused/Resume state")
	}
}

func TestFix7CountdownScriptIsDisplayOnlyAndStorageFree(t *testing.T) {
	w := anRoadWorld(t)
	_, body := anSiteGet(t, w.srv, "GET", "/analytics", true)
	i := strings.Index(body, `<script id="an-countdown-script">`)
	if i < 0 {
		t.Fatalf("no countdown script")
	}
	script := body[i : i+strings.Index(body[i:], "</script>")]
	for _, bad := range []string{"localStorage", "sessionStorage", "document.cookie", "location.reload", "location.href"} {
		if strings.Contains(script, bad) {
			t.Errorf("countdown script uses %s — it must only display, the meta refresh reloads", bad)
		}
	}
	// It starts at the load event, when browsers start the meta refresh timer.
	if !strings.Contains(script, `addEventListener("load"`) {
		t.Errorf("countdown does not anchor its deadline at the load event")
	}
}
