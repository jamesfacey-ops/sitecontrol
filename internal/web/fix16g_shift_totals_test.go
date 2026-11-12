package web

// fix16g_shift_totals_test.go — FIX16 G: the read-only Shift totals panel on
// the post's gate pages and status page. FAIL BEFORE (1cbf34f): no panel on
// any page. The figures are checked against the analytics page's own "today"
// figures for the same instants, and against the hand count of the fixture
// (two crew signed in, one road-work run sent out).

import (
	"fmt"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/domain"
)

func fix16gGet(t *testing.T, w *roadWorkWeb, host, path string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	if host != "" {
		req.Host = host
	}
	rec := httptest.NewRecorder()
	w.srv.Handler().ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func fix16gFigure(t *testing.T, body, label string) int {
	t.Helper()
	m := regexp.MustCompile(`<b>(\d+)</b> ` + regexp.QuoteMeta(label) + `</span>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no %q figure in the shift totals panel; body:\n%s", label, body)
	}
	var n int
	fmt.Sscan(m[1], &n)
	return n
}

func TestFix16GShiftTotalsPanelOnPostPages(t *testing.T) {
	w := newRoadWorkWeb(t, true)
	w.sendOut(t, nil) // one road-work run out
	// Window = [now-1h, ...): contains now whatever the wall clock reads.
	w.srv.Cfg.ShiftStarts = []string{time.Now().Add(-time.Hour).Format("15:04")}

	fig, err := domain.BuildAnalytics(w.st, time.Now(), time.Local)
	if err != nil {
		t.Fatal(err)
	}
	var site domain.SiteFigures
	for _, s := range fig.Sites {
		if s.SiteID == "site-a" {
			site = s
		}
	}
	if site.VisitsToday != 2 || site.RoadWorkOutToday != 1 {
		t.Fatalf("fixture drifted: analytics says visits %d road-work out %d, want 2 and 1", site.VisitsToday, site.RoadWorkOutToday)
	}
	for _, path := range []string{"/gate", "/gate/vehicle", "/status"} {
		code, body := fix16gGet(t, w, "", path)
		if code != 200 {
			t.Fatalf("GET %s = %d", path, code)
		}
		if !strings.Contains(body, `class="shift-totals"`) {
			t.Fatalf("GET %s: no Shift totals panel", path)
		}
		if got := fix16gFigure(t, body, "in"); got != site.VisitsToday {
			t.Errorf("%s sign-ins = %d, analytics today %d", path, got, site.VisitsToday)
		}
		if got := fix16gFigure(t, body, "out"); got != site.SignOutsToday {
			t.Errorf("%s sign-outs = %d, want %d", path, got, site.SignOutsToday)
		}
		if got := fix16gFigure(t, body, "veh out"); got != site.TravelOutToday+site.RoadWorkOutToday {
			t.Errorf("%s vehicles out = %d, want %d", path, got, site.TravelOutToday+site.RoadWorkOutToday)
		}
		if got := fix16gFigure(t, body, "veh in"); got != site.TravelInToday {
			t.Errorf("%s vehicles in = %d, want %d", path, got, site.TravelInToday)
		}
		if got := fix16gFigure(t, body, "on site"); got != site.OnSiteNow {
			t.Errorf("%s on site = %d, want %d", path, got, site.OnSiteNow)
		}
		// Read-only: nothing inside the panel is a form control or a link.
		panel := body[strings.Index(body, `class="shift-totals"`):]
		panel = panel[:strings.Index(panel, "</details>")]
		for _, bad := range []string{"<form", "<input", "<button", "<a "} {
			if strings.Contains(panel, bad) {
				t.Errorf("%s: the panel contains %q — it must be read-only", path, bad)
			}
		}
		if !strings.Contains(panel, "Shift started") {
			t.Errorf("%s: panel does not say when the shift started", path)
		}
	}
}

func TestFix16GShiftTotalsDefaultAndConfiguredText(t *testing.T) {
	w := newRoadWorkWeb(t, false)
	_, body := fix16gGet(t, w, "", "/gate")
	if !strings.Contains(body, "06:00 and 18:00") || !strings.Contains(body, "the default") {
		t.Errorf("an unconfigured post's panel must state the 06:00 / 18:00 default")
	}
	w.srv.Cfg.ShiftStarts = []string{"05:30", "13:30", "21:30"}
	_, body = fix16gGet(t, w, "", "/gate")
	if !strings.Contains(body, "05:30, 13:30 and 21:30") || strings.Contains(body, "the default") {
		t.Errorf("a configured post's panel must name its own times and not claim the default")
	}
}

func TestFix16GShiftTotalsNotOnDispatchHost(t *testing.T) {
	w := newRoadWorkWeb(t, false)
	for _, path := range []string{"/analytics", "/gate", "/status"} {
		_, body := fix16gGet(t, w, "sc-analytics.example.com", path)
		if strings.Contains(body, `<details class="shift-totals"`) {
			t.Errorf("dispatch host %s renders the Shift totals panel", path)
		}
	}
}
