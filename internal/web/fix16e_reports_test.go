// fix16e_reports_test.go — FIX16 stream E: the dispatch reports
// (dispatch_reports.go). Every figure is checked against a hand-worked two-day
// fixture, and the reports are cross-checked against the /analytics tiles
// for the same day: the two must be one count.
package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// f16eAt is an instant on Mon 2026-09-28 (day 0) or Tue 2026-09-29 (day 1).
func f16eAt(day, hour, min int) int64 {
	return time.Date(2026, 9, 28+day, hour, min, 0, 0, time.Local).UnixMilli()
}

// f16eWorld seeds the fixture, as site-b (its own post; site-a a peer):
//
//	Mon 08:00 Dana (driver) + Pat check in at site-b          headcount 2
//	Mon 09:00 Lee checks in                                    3  <- Mon peak, 09:00
//	Mon 12:00 Pat signs out at the gate (4h 00m)               2
//	Mon 13:00 Lee dispatched to site-a, 30-min allowance       1  (Lee 4h 00m, left on a trip)
//	Mon 14:00 Lee arrives at site-a — due 13:30, 30 min LATE
//	          Dana never signs out.
//	Tue 10:00 Dana checks in again: Monday's visit is closed by her
//	          return — a MISSED SIGN-OUT (record open 26h)       1
//	Tue 11:00 Sam (Polar Haul) checks in                       2  <- Tue peak, 11:00
//	Tue 11:30 Dana + Sam dispatched to site-a, 60-min allowance 0  <- Tue low, 11:30
//	          (Dana 1h 30m, Sam 0h 30m, both left on a trip)
//	Tue 12:00 the truck arrives back at site-b: TURNED BACK     2  (two open visits)
//	report time Tue 15:00.
//
// Hours, Mon–Tue, every site: Kakivik = Pat 4h + Lee 4h + Dana 1h30 = 9h30;
// Polar Haul = Sam 0h30; total 10h00 over 4 visits. Flagged, never counted:
// Dana's Monday visit (missed sign-out), Lee at site-a, Dana and Sam back at
// site-b (all three still open) = 4.
func f16eWorld(t *testing.T) (*Server, *store.Store, func(int64)) {
	t.Helper()
	st, setClock := anStore(t)
	srv := anServer(st)
	srv.Cfg.SiteType = domain.SiteTypeTextMessage
	setClock(f16eAt(0, 7, 0))
	ps := anRoster(t, st, "site-b", "Dana Driver", "Pat Passenger", "Lee Loader")
	dana, pat, lee := ps[0].PersonID, ps[1].PersonID, ps[2].PersonID
	co, err := domain.CreateCompany(st, "site-b", 1, "Polar Haul", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	sam, err := domain.CreatePerson(st, "site-b", 1, "Sam Spare", &co.CompanyID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	visitOf := func(person string) string {
		open, err := domain.OpenVisitsAt(st, "site-b")
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range open {
			if v.PersonID == person {
				return v.VisitID
			}
		}
		t.Fatalf("no open visit for %s", person)
		return ""
	}
	send := func(allow int64, driver string, occ ...string) domain.Trip {
		dest := "site-a"
		trip, err := domain.Dispatch(st, "site-b", 1, domain.DispatchInput{
			VehicleID: w7SeedVehicleID(t, st, "site-b", "TRK-16E"), DriverPersonID: driver, OccupantPersonIDs: occ,
			DestinationSite: &dest, DeadlineMinutes: &allow,
		}, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		return trip
	}

	setClock(f16eAt(0, 8, 0))
	checkIn(t, st, "site-b", dana, []string{pat})
	setClock(f16eAt(0, 9, 0))
	checkIn(t, st, "site-b", lee, nil)
	setClock(f16eAt(0, 12, 0))
	if err := domain.CheckOutVisit(st, "site-b", 1, visitOf(pat), nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	setClock(f16eAt(0, 13, 0))
	late := send(30, lee)
	setClock(f16eAt(0, 14, 0))
	if _, err := domain.RecordExpectedArrival(st, "site-a", 1, late.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	setClock(f16eAt(1, 10, 0))
	checkIn(t, st, "site-b", dana, nil)
	setClock(f16eAt(1, 11, 0))
	checkIn(t, st, "site-b", sam.PersonID, nil)
	setClock(f16eAt(1, 11, 30))
	back := send(60, dana, sam.PersonID)
	setClock(f16eAt(1, 12, 0))
	if _, err := domain.RecordExpectedArrival(st, "site-b", 1, back.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	setClock(f16eAt(1, 15, 0))
	return srv, st, setClock
}

func f16eGet(t *testing.T, srv *Server, host, path string, login bool) (int, string) {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	if host != "" {
		req.Host = host
	}
	if login {
		req.AddCookie(loginCookie(t, srv, testLoginPassword))
	}
	rec := httptest.NewRecorder()
	srv.Root().ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

var f16eFigRe = regexp.MustCompile(`data-rp="([a-z-]+)">([^<]*)<`)

// f16eFigs reads every data-rp figure on a page.
func f16eFigs(body string) map[string]string {
	out := map[string]string{}
	for _, m := range f16eFigRe.FindAllStringSubmatch(body, -1) {
		if _, seen := out[m[1]]; !seen {
			out[m[1]] = strings.TrimSpace(m[2])
		}
	}
	return out
}

func f16eWant(t *testing.T, what string, got map[string]string, want map[string]string) {
	t.Helper()
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %s = %q, want %q (all: %v)", what, k, got[k], v, got)
		}
	}
}

func TestFix16eDailySiteReportFigures(t *testing.T) {
	srv, _, _ := f16eWorld(t)
	code, mon := f16eGet(t, srv, "", "/analytics/reports/daily?site=site-b&date=2026-09-28", true)
	if code != http.StatusOK {
		t.Fatalf("daily = %d:\n%s", code, mon)
	}
	f16eWant(t, "Monday site-b", f16eFigs(mon), map[string]string{
		"peak": "3", "low": "0", "start": "0", "sign-ins": "3", "sign-outs": "2",
		"trips-out": "1", "trips-in": "0", "turned-back": "0", "late": "1", "missed": "0", "signal": "—",
	})
	for _, want := range []string{"first reached at 09:00", "1 signed out at the gate, 1 left aboard a trip", "Lost-signal gaps", "Not available",
		"30 minutes late", "TRK-16E"} {
		if !strings.Contains(mon, want) {
			t.Errorf("Monday report lacks %q", want)
		}
	}
	_, tue := f16eGet(t, srv, "", "/analytics/reports/daily?site=site-b&date=2026-09-29", true)
	f16eWant(t, "Tuesday site-b", f16eFigs(tue), map[string]string{
		"peak": "2", "low": "0", "start": "1", "sign-ins": "4", "sign-outs": "2",
		"trips-out": "1", "trips-in": "1", "turned-back": "1", "late": "0", "missed": "1",
	})
	for _, want := range []string{"first reached at 11:00", "first reached at 11:30", "Dana Driver", "missed sign-outs</td>"} {
		if !strings.Contains(tue, want) {
			t.Errorf("Tuesday report lacks %q", want)
		}
	}
	// site-a, Monday: Lee's late run touches it too (it was bound there).
	_, a := f16eGet(t, srv, "", "/analytics/reports/daily?site=site-a&date=2026-09-28", true)
	f16eWant(t, "Monday site-a", f16eFigs(a), map[string]string{"peak": "1", "sign-ins": "1", "trips-in": "1", "late": "1"})
}

func TestFix16eContractorHoursFlagsUnclosedVisits(t *testing.T) {
	srv, _, _ := f16eWorld(t)
	code, body := f16eGet(t, srv, "", "/analytics/reports/hours?from=2026-09-28&to=2026-09-29", true)
	if code != http.StatusOK {
		t.Fatalf("hours = %d", code)
	}
	if got := f16eFigs(body)["total-hours"]; got != "10h 00m" {
		t.Errorf("total = %q, want 10h 00m (flagged visits must not be counted)", got)
	}
	company := func(name string) string {
		m := regexp.MustCompile(`(?s)<tbody data-rp-company="` + regexp.QuoteMeta(name) + `">(.*?)</tbody>`).FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("no company block %q:\n%s", name, body)
		}
		return m[1]
	}
	k := company("Kakivik Asset Management")
	for _, want := range []string{`data-rp="company-hours">9h 30m<`, "Pat Passenger</th><td>4h 00m", "Lee Loader</th><td>4h 00m", "Dana Driver</th><td>1h 30m"} {
		if !strings.Contains(k, want) {
			t.Errorf("Kakivik block lacks %q:\n%s", want, k)
		}
	}
	if p := company("Polar Haul"); !strings.Contains(p, `data-rp="company-hours">0h 30m<`) {
		t.Errorf("Polar Haul block:\n%s", p)
	}
	flagged := regexp.MustCompile(`(?s)data-rp="flagged">.*?</table>`).FindString(body)
	if n := strings.Count(flagged, "<tr><td>"); n != 4 {
		t.Errorf("flagged rows = %d, want 4:\n%s", n, flagged)
	}
	if strings.Count(flagged, "MISSED SIGN-OUT") != 1 || strings.Count(flagged, "STILL OPEN") != 3 || !strings.Contains(flagged, "at most 26h 00m") {
		t.Errorf("flagged reasons wrong:\n%s", flagged)
	}
	// One day only: Monday's counted visits are Pat and Lee (8h); Dana's
	// Monday visit is flagged.
	_, mon := f16eGet(t, srv, "", "/analytics/reports/hours?from=2026-09-28&to=2026-09-28&site=site-b", true)
	if got := f16eFigs(mon)["total-hours"]; got != "8h 00m" {
		t.Errorf("Monday site-b total = %q, want 8h 00m", got)
	}
}

func TestFix16eWeeklyRollUp(t *testing.T) {
	srv, _, _ := f16eWorld(t)
	code, body := f16eGet(t, srv, "", "/analytics/reports/weekly?week=2026-09-30", true)
	if code != http.StatusOK {
		t.Fatalf("weekly = %d", code)
	}
	if !strings.Contains(body, "Mon 28 Sep 2026 to Sun 4 Oct 2026") {
		t.Errorf("a Wednesday must select the Monday-to-Sunday week")
	}
	// visits: site-b 3 Mon + 4 Tue, site-a 1 = 8. people: Dana, Pat, Lee, Sam.
	f16eWant(t, "week", f16eFigs(body), map[string]string{
		"visits": "8", "people": "4", "companies": "2", "dispatched": "2", "arrived": "1",
		"turned-back": "1", "late": "1", "missed": "1", "signal": "—",
	})
	row := regexp.MustCompile(`(?s)<tr data-rp-site="site-b">.*?</tr>`).FindString(body)
	if !strings.Contains(row, "<td>7</td><td>4</td><td>2</td><td>2</td><td>1</td><td>1</td><td>0</td><td>1</td><td>1</td>") {
		t.Errorf("site-b weekly row:\n%s", row)
	}
	// The week before holds nothing.
	_, prev := f16eGet(t, srv, "", "/analytics/reports/weekly?week=2026-09-21", true)
	f16eWant(t, "previous week", f16eFigs(prev), map[string]string{"visits": "0", "people": "0", "late": "0"})
}

func TestFix16eHeadcountChartAndTable(t *testing.T) {
	srv, _, _ := f16eWorld(t)
	code, body := f16eGet(t, srv, "", "/analytics/reports/headcount?site=site-b&from=2026-09-28&to=2026-09-29", true)
	if code != http.StatusOK {
		t.Fatalf("headcount = %d", code)
	}
	if !strings.Contains(body, `<svg class="rp-svg" role="img"`) || strings.Contains(body, "<script src") {
		t.Errorf("chart must be inline SVG with no external script")
	}
	table := regexp.MustCompile(`(?s)data-rp="hourly">.*?</table>`).FindString(body)
	rows := regexp.MustCompile(`<tr><th scope="row">([^<]+)</th><td>(\d+)</td><td>(\d+)</td></tr>`).FindAllStringSubmatch(table, -1)
	// Mon 00:00 .. Tue 15:00 inclusive (report time 15:00) = 24 + 16 hours.
	if len(rows) != 40 {
		t.Fatalf("hourly rows = %d, want 40", len(rows))
	}
	want := map[string][2]string{
		"Mon 28 Sep 07:00": {"0", "0"}, "Mon 28 Sep 08:00": {"2", "2"}, "Mon 28 Sep 09:00": {"3", "3"},
		"Mon 28 Sep 12:00": {"2", "2"}, "Mon 28 Sep 13:00": {"1", "1"}, "Tue 29 Sep 11:00": {"2", "2"},
		"Tue 29 Sep 12:00": {"2", "2"}, "Tue 29 Sep 14:00": {"2", "2"}, "Mon 28 Sep 23:00": {"1", "1"},
	}
	for _, r := range rows {
		if w, ok := want[r[1]]; ok && (r[2] != w[0] || r[3] != w[1]) {
			t.Errorf("%s: at start %s, peak %s; want %v", r[1], r[2], r[3], w)
		}
	}
	if got := f16eFigs(body)["max"]; got != "3" {
		t.Errorf("max = %q, want 3", got)
	}
}

// TestFix16eHeadcountRangeIsCapped — more than reportHeadcountMaxDays is
// refused with a reason, not silently truncated.
func TestFix16eHeadcountRangeIsCapped(t *testing.T) {
	srv, _, _ := f16eWorld(t)
	_, body := f16eGet(t, srv, "", "/analytics/reports/headcount?site=site-b&from=2026-09-01&to=2026-09-29", true)
	if !strings.Contains(body, "NO REPORT") || !strings.Contains(body, "at most 14 days") || strings.Contains(body, `data-rp="hourly"`) {
		t.Errorf("a 29-day range must be refused with the cap named")
	}
}

// TestFix16eReportsAgreeWithTheTiles — the daily report for today and the
// /analytics site card for today are one computation (AnalyticsWindow), and
// the headcount "now" is the tile's persons-on-site-now.
func TestFix16eReportsAgreeWithTheTiles(t *testing.T) {
	srv, st, _ := f16eWorld(t)
	_, an := anGet(t, srv, true)
	card := anSiteCard(t, an, "site-b")
	_, rep := f16eGet(t, srv, "", "/analytics/reports/daily?site=site-b&date=2026-09-29", true)
	f := f16eFigs(rep)
	for _, c := range []struct{ tile, want string }{
		{`<dt>Visits today / season</dt><dd>`, f["sign-ins"] + " / "},
		{`<dt>Travel out, today / season</dt><dd>`, f["trips-out"] + " / "},
		{`<dt>Travel in, today / season</dt><dd>`, f["trips-in"] + " / "},
	} {
		if !strings.Contains(card, c.tile+c.want) {
			t.Errorf("tile %q does not agree with the report's %q:\n%s", c.tile, c.want, card)
		}
	}
	if !strings.Contains(an, `<span class="an-big-sub">`) && !strings.Contains(an, "turned back") {
		t.Logf("overview carries no turned-back wording to compare")
	}
	// Headcount now == persons on site now, for every site.
	recs, _, err := domain.VisitReport(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(srv.clockReading().WallMS)
	fig, err := domain.BuildAnalytics(st, now, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	for _, sf := range fig.Sites {
		spans, _ := domain.PresenceSpans(recs, sf.SiteID)
		if got := domain.NewHeadcount(spans).At(now.UnixMilli()); got != sf.OnSiteNow {
			t.Errorf("%s: headcount now %d != persons on site now tile %d", sf.SiteID, got, sf.OnSiteNow)
		}
	}
	if got := anTile(t, an, "persons on site now"); got != "2" {
		t.Errorf("tile persons on site now = %s, want 2", got)
	}
	// The window computation reproduces BuildAnalytics exactly for today.
	day := time.Date(2026, 9, 29, 0, 0, 0, 0, time.Local)
	win, err := domain.AnalyticsWindow(st, day.UnixMilli(), day.AddDate(0, 0, 1).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if win.TripsDispatchedToday != fig.TripsDispatchedToday || win.TripsArrivedToday != fig.TripsArrivedToday ||
		win.TripsTurnedBackToday != fig.TripsTurnedBackToday || len(win.Sites) != len(fig.Sites) {
		t.Errorf("AnalyticsWindow(today) != BuildAnalytics(now): %+v vs %+v", win, fig)
	}
	for i := range win.Sites {
		a, b := win.Sites[i], fig.Sites[i]
		if a.VisitsToday != b.VisitsToday || a.TravelOutToday != b.TravelOutToday || a.TravelInToday != b.TravelInToday || a.OnSiteNow != b.OnSiteNow {
			t.Errorf("site %s differs: %+v vs %+v", a.SiteID, a, b)
		}
	}
	// The weekly roll-up's trip totals equal the overview's season totals
	// here (every trip of the season is in this week).
	_, wk := f16eGet(t, srv, "", "/analytics/reports/weekly?week=2026-09-28", true)
	w := f16eFigs(wk)
	m := regexp.MustCompile(`<div class="an-num">(\d+) <small>/ (\d+)</small></div>\s*<div class="an-label">trips dispatched`).FindStringSubmatch(an)
	if m == nil {
		t.Fatalf("no trips dispatched tile")
	}
	if w["dispatched"] != m[2] {
		t.Errorf("weekly dispatched %s != overview season trips dispatched %s", w["dispatched"], m[2])
	}
	if f["trips-out"] != m[1] {
		t.Errorf("today's site-b trips out %s != overview trips dispatched today %s (site-b is the only sender)", f["trips-out"], m[1])
	}
}

// TestFix16eReportsAreReadOnlyAndBehindLogin — anonymous gets the login
// page; every writing method is refused and records nothing; on the
// dispatch host the reports are allow-listed and carry no console link.
func TestFix16eReportsAreReadOnlyAndBehindLogin(t *testing.T) {
	srv, st, _ := f16eWorld(t)
	paths := []string{"/analytics/reports", "/analytics/reports/daily?site=site-b&date=2026-09-28",
		"/analytics/reports/hours?from=2026-09-28&to=2026-09-29", "/analytics/reports/weekly",
		"/analytics/reports/headcount?site=site-b"}
	before, err := st.DumpProjections()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		for _, host := range []string{"", fix7DispatchHost, fix7GateHost} {
			_, anon := f16eGet(t, srv, host, p, false)
			if !strings.Contains(anon, `name="password"`) || strings.Contains(anon, "data-rp=") {
				t.Errorf("anonymous GET %s on %q must be the login page", p, host)
			}
			code, body := f16eGet(t, srv, host, p, true)
			if code != http.StatusOK || !strings.Contains(body, "Reports") {
				t.Errorf("GET %s on %q = %d", p, host, code)
			}
			if host == fix7DispatchHost {
				if m := fix7ConsoleHref.FindString(body); m != "" {
					t.Errorf("dispatch %s links into the console: %s", p, m)
				}
				if strings.Contains(body, `method="post"`) {
					t.Errorf("dispatch %s carries a POST form", p)
				}
			}
		}
		for _, m := range []string{"POST", "PUT", "DELETE"} {
			for _, host := range []string{fix7DispatchHost, fix7GateHost} {
				rec := fix7Do(t, srv, m, host, strings.SplitN(p, "?", 2)[0], url.Values{"x": {"1"}})
				if rec.Code != http.StatusForbidden && rec.Code != http.StatusMethodNotAllowed {
					t.Errorf("%s %s on %s = %d, want 403/405", m, p, host, rec.Code)
				}
			}
		}
	}
	after, err := st.DumpProjections()
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("loading the reports changed the records")
	}
	// The nav: both analytics pages offer Reports.
	for _, p := range []string{"/analytics", "/analytics/site/site-b"} {
		_, body := f16eGet(t, srv, fix7DispatchHost, p, true)
		if !strings.Contains(body, `<a href="/analytics/reports">reports</a>`) {
			t.Errorf("%s has no Reports nav entry", p)
		}
	}
	if !dispatchReadable("/analytics/reports/daily") || dispatchReadable("/analytics/reportsx") {
		t.Errorf("dispatch allow-list wrong for the reports")
	}
}

// TestFix16eReportsDownloadAsCSV — every report offers "Download CSV" (stream
// B's writeCSVDownload), the file carries the page's figures, and a refused
// query is a 400 with no file.
func TestFix16eReportsDownloadAsCSV(t *testing.T) {
	srv, _, _ := f16eWorld(t)
	for _, c := range []struct{ page, want string }{
		{"/analytics/reports/daily?site=site-b&date=2026-09-28", "Peak headcount,3,first reached at 09:00"},
		{"/analytics/reports/hours?from=2026-09-28&to=2026-09-29", "yes,(all companies),(total),,,,10h 00m,10.00,4,4,"},
		{"/analytics/reports/weekly?week=2026-09-28", "(all sites),8,4,2,2,1,1,0,1,1,not available"},
		{"/analytics/reports/headcount?site=site-b&from=2026-09-28&to=2026-09-28", "Mon 28 Sep 09:00,3,3"},
	} {
		_, body := f16eGet(t, srv, fix7DispatchHost, c.page, true)
		href := regexp.MustCompile(`<a href="([^"]+)" data-csv="`).FindStringSubmatch(body)
		if href == nil {
			t.Fatalf("%s offers no Download CSV", c.page)
		}
		u := strings.ReplaceAll(href[1], "&amp;", "&")
		for _, host := range []string{fix7DispatchHost, ""} {
			req := httptest.NewRequest("GET", u, nil)
			req.Host = host
			req.AddCookie(loginCookie(t, srv, testLoginPassword))
			rec := httptest.NewRecorder()
			srv.Root().ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/csv") ||
				!strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
				t.Fatalf("GET %s on %q = %d %v", u, host, rec.Code, rec.Header())
			}
			if !strings.Contains(rec.Body.String(), c.want) {
				t.Errorf("%s lacks %q:\n%s", u, c.want, rec.Body.String())
			}
		}
		_, anon := f16eGet(t, srv, "", u, false)
		if !strings.Contains(anon, `name="password"`) {
			t.Errorf("anonymous %s must be the login page", u)
		}
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/analytics/reports/headcount?site=site-b&from=2026-09-01&to=2026-09-29&format=csv", nil)
	req.AddCookie(loginCookie(t, srv, testLoginPassword))
	srv.Root().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || strings.HasPrefix(rec.Header().Get("Content-Type"), "text/csv") {
		t.Errorf("a refused range must be a 400 with no file, got %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
}
