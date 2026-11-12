package web

// fix3_no_weather_test.go — weather phases removed (James, 2026-10-04: "the
// phase conditions can be removed they will not effect site control … go
// ahead and delete the phase conditions"). What the web layer owes now:
//
//   - no page, at any of the five posts, renders a phase control, a phase
//     chip, a superseded notice, a phase override input or a weather VERIFY
//     row, and POST /weather/declare no longer exists;
//   - dispatch works with no phase concept at all, and on the ice road the
//     deadline is the configured 65 minutes, prefilled and recorded;
//   - a cached form still carrying review_phase (ack / un-ack) or a typed
//     phase_override_reason (trip correction) is REFUSED and authors
//     nothing — a stale weather form must never be guessed into a plain act.
//
// ⟦was: alerts_phase_test.go, weather_board_test.go,
// weather_board_noauthor_test.go, weather_test.go and
// r1_phase_superseded_test.go — the board, the declaration route, the
// phase-scoped acks and the R1 superseded notice; all deleted with the
// feature. dispatchInboundTrip was defined in alerts_phase_test.go and is
// re-homed here unchanged.⟧

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// dispatchInboundTrip is newAlertingServer's fixture pattern (alerts_test.go),
// parameterised: an inbound (site-a -> site-b) expectation of X minutes,
// applied with a receipt anchor leaving `leftover` of X remaining.
func dispatchInboundTrip(t *testing.T, st *store.Store, d *deadline.Deadlines, vehicle string, xMinutes int64, leftover time.Duration) domain.Trip {
	t.Helper()
	dest := "site-b"
	trip, err := domain.Dispatch(st, "site-a", 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: vehicle, DriverPersonID: "driver-a","⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", vehicle), DriverPersonID: "driver-a",
		DestinationSite: &dest, DeadlineMinutes: &xMinutes,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	envs, err := st.Events("site-a", 1, trip.Seq, 1)
	if err != nil || len(envs) != 1 {
		t.Fatalf("fetch trip envelope: %v", err)
	}
	d.OnApplied(envs[0], time.Now().Add(-time.Duration(xMinutes)*time.Minute+leftover), true)
	return trip
}

// fix3PhaseMarkers is every rendered trace the weather-phase surface left:
// the board's form and buttons, the chip, the superseded notice, the panel
// classes, the dispatch-form notices and the override input, the VERIFY
// row, and the hidden review_phase field.
var fix3PhaseMarkers = []*regexp.Regexp{
	regexp.MustCompile(`/weather/declare`),
	regexp.MustCompile(`name="phase"`),
	regexp.MustCompile(`(?i)weather phase`),
	regexp.MustCompile(`data-phase-`),
	regexp.MustCompile(`phase-panel|phase-gap|phase-blocked|phase-board`),
	regexp.MustCompile(`(?i)phase \d (—|-|is in effect)`),
	regexp.MustCompile(`(?i)superseded by a concurrent press`),
	regexp.MustCompile(`name="override_reason"|name="phase_override_reason"`),
	regexp.MustCompile(`(?i)phase override`),
	regexp.MustCompile(`data-alert-row="phase_review"|VERIFY BY RADIO \(weather\)`),
	regexp.MustCompile(`name="review_phase"`),
	regexp.MustCompile(`(?i)at current phase`),
}

// fix3Demo5Server serves one of the five demo posts from its committed toml
// (deploy/demo5) — the real configs, so "every post" means the five that
// ship, not a fixture's idea of them.
// The deploy-time secrets are swapped for the test suite's own login (the
// committed file carries placeholders, and a proxy token would route the
// login page away).
func fix3Demo5Server(t *testing.T, post string) (*Server, []domain.Person) {
	t.Helper()
	cfg, err := config.Load(filepath.Join("..", "..", "deploy", "demo5", post+".toml"))
	if err != nil {
		t.Fatalf("%s: %v", post, err)
	}
	cfg.LoginPassword = testLoginPassword
	cfg.ProxyAuthToken = ""
	srv, st, persons := newTripsTestServer(t)
	cfg.DBPath = st.Path
	srv.Cfg = cfg
	return srv, persons
}

// fix3Get is an authenticated GET returning the recorder, <style> stripped
// from the body the way getPage strips it.
func fix3Get(t *testing.T, srv *Server, path string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.AddCookie(loginCookie(t, srv, testLoginPassword))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec.Code, styleRe.ReplaceAllString(rec.Body.String(), "")
}

func TestFix3StaleWeatherFormFieldsAreRefusedAndAuthorNothing(t *testing.T) {
	srv, st, _, _ := newAlertingServer(t) // the ack/un-ack routes are registered only with the scanner wired
	for _, c := range []struct {
		path string
		form url.Values
	}{
		{"/alerts/ack", url.Values{"trip_id": {"site-a-trip-x"}, "review_phase": {"3"}}},
		{"/alerts/unack", url.Values{"trip_id": {"site-a-trip-x"}, "review_phase": {"3"}}},
		{"/trips/amend", url.Values{"trip_id": {"site-a-trip-x"}, "phase_override_reason": {"board-authorised"}}},
	} {
		before := countEvents(t, st)
		rec := postForm(t, srv, c.path, c.form)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Nothing was recorded") {
			t.Errorf("%s with a stale weather field: %d %q, want a 400 refusal saying nothing was recorded", c.path, rec.Code, rec.Body.String())
		}
		if after := countEvents(t, st); after != before {
			t.Errorf("%s authored %d event(s) on a stale weather field", c.path, after-before)
		}
	}
}
