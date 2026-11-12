package web

import (
	"strings"
	"testing"
)

// James, 2026-10-03: ONLY /analytics and /analytics/site/{site}
// auto-refresh, both every 60 seconds, each with a visible "Pause live
// updates" link; ?live=0 renders without the meta refresh and shows "Live
// updates paused — Resume". No other page refreshes.

const liveMeta = `<meta http-equiv="refresh" content="60">`

func TestBothAnalyticsPagesAreLiveEverySixtySecondsWithAPauseLink(t *testing.T) {
	w := anRoadWorld(t)
	for _, path := range []string{"/analytics", "/analytics/site/site-a"} {
		code, body := anSiteGet(t, w.srv, "GET", path, true)
		if code != 200 {
			t.Fatalf("GET %s = %d", path, code)
		}
		if strings.Count(body, `http-equiv="refresh"`) != 1 || !strings.Contains(body, liveMeta) {
			t.Errorf("%s: want exactly one 60 s meta refresh", path)
		}
		if !strings.Contains(body, `<a href="?live=0">Pause live updates</a>`) {
			t.Errorf("%s: no visible Pause live updates link", path)
		}
		if strings.Contains(body, "Live updates paused") {
			t.Errorf("%s: a live page must not claim to be paused", path)
		}
	}
}

func TestPausedAnalyticsPagesRenderWithoutTheMetaRefreshAndOfferResume(t *testing.T) {
	w := anRoadWorld(t)
	for _, path := range []string{"/analytics?live=0", "/analytics/site/site-a?live=0"} {
		code, body := anSiteGet(t, w.srv, "GET", path, true)
		if code != 200 {
			t.Fatalf("GET %s = %d", path, code)
		}
		if strings.Contains(body, `http-equiv="refresh"`) {
			t.Errorf("%s: paused page still carries a meta refresh", path)
		}
		if !strings.Contains(body, "<strong>Live updates paused</strong>") || !strings.Contains(body, `<a href="?live=1">Resume</a>`) {
			t.Errorf("%s: paused page must say \"Live updates paused — Resume\"", path)
		}
		if strings.Contains(body, "Pause live updates") {
			t.Errorf("%s: paused page still offers Pause", path)
		}
	}
	// The pause rides the links between the two pages, so a paused wall
	// screen stays paused when the viewer drills in and back.
	_, body := anSiteGet(t, w.srv, "GET", "/analytics?live=0", true)
	if !strings.Contains(body, `href="/analytics/site/site-a?live=0"`) {
		t.Errorf("paused /analytics: site link drops the pause")
	}
	_, body = anSiteGet(t, w.srv, "GET", "/analytics/site/site-a?live=0", true)
	if !strings.Contains(body, `href="/analytics?live=0"`) {
		t.Errorf("paused site page: link back drops the pause")
	}
	// Live pages link plainly (no stray switch).
	_, body = anSiteGet(t, w.srv, "GET", "/analytics", true)
	if !strings.Contains(body, `href="/analytics/site/site-a"`) {
		t.Errorf("live /analytics: site link should carry no ?live")
	}
}

func TestNoOtherPageRefreshesItself(t *testing.T) {
	w := anRoadWorld(t)
	for _, path := range []string{"/gate", "/gate/vehicle", "/trips", "/muster", "/muster/print", "/status", "/roster", "/trade", "/orientation", "/sync/status", "/login"} {
		_, body := anSiteGet(t, w.srv, "GET", path, true)
		if strings.Contains(body, `http-equiv="refresh"`) {
			t.Errorf("%s must not auto-refresh (James, 2026-10-03: only the two analytics pages do)", path)
		}
		if strings.Contains(body, "Pause live updates") {
			t.Errorf("%s carries the analytics pause link", path)
		}
	}
}
