package web

import (
	"encoding/csv"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/repl"
	"sitecontrol/internal/store"
)

// FIX16f (James, 2026-10-06): the three history reports. history.go and
// signal_history.go have the design. One store holds three posts' records
// (site-a a text-message road post, site-b a camp, site-c the dispatch
// server, as NDB in the demo); each test reads it through the post it names.

type fix16fWorld struct {
	st    *store.Store
	clock time.Time
	// trips by role
	late, turned, onTime, open string
	t0                         time.Time
}

func (w *fix16fWorld) at(t time.Time) { w.clock = t }

func fix16fConfig(st *store.Store, site string) *config.Config {
	return &config.Config{
		SiteID: site, DisplayName: map[string]string{"site-a": "Road Post", "site-b": "Pad Post", "site-c": "North Dock"}[site],
		ListenAddr: ":0", NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 10, LoginPassword: testLoginPassword,
		SiteType: map[string]domain.SiteType{"site-a": domain.SiteTypeTextMessage, "site-b": domain.SiteTypeCamp, "site-c": domain.SiteTypeTextMessage}[site],
		Peers: func() []config.Peer {
			var ps []config.Peer
			for _, p := range []config.Peer{
				{SiteID: "site-a", BaseURL: "http://example.invalid", DisplayName: "Road Post", SiteType: domain.SiteTypeTextMessage},
				{SiteID: "site-b", BaseURL: "http://example.invalid", DisplayName: "Pad Post", SiteType: domain.SiteTypeCamp},
				{SiteID: "site-c", BaseURL: "http://example.invalid", DisplayName: "North Dock"},
			} {
				if p.SiteID != site {
					ps = append(ps, p)
				}
			}
			return ps
		}(),
	}
}

// fix16fSeed: four runs site-a -> site-b with a deadline — one 20 min late
// and acknowledged OVERDUE at site-b, one turned back 15 min past due, one
// on time, one still open — and missed sign-outs: Dana Reyes (Acme
// Drilling) leaves two visits at site-a (text message) unsigned-out, in two
// weeks; Eve Stone does the same at site-b (a camp: exempt).
func fix16fSeed(t *testing.T) *fix16fWorld {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	for _, reg := range []func(*store.Store) error{domain.Register, deadline.Register, RegisterSignalHistory} {
		if err := reg(st); err != nil {
			t.Fatal(err)
		}
	}
	w := &fix16fWorld{st: st}
	st.Clock = func() store.ClockReading {
		return store.ClockReading{WallMS: w.clock.UnixMilli(), MaxErrorUS: 1000, State: store.ClockSynced}
	}
	w.t0 = time.Date(2026, 9, 28, 8, 0, 0, 0, time.Local) // a Monday
	w.at(w.t0)
	acme, err := domain.CreateCompany(st, "site-a", 1, "Acme Drilling", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	person := func(name string, company *string) domain.Person {
		p, err := domain.CreatePerson(st, "site-a", 1, name, company, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	run := func(driver domain.Person, vehicle string, mins int64) string {
		checkIn(t, st, "site-a", driver.PersonID, nil)
		dest := "site-b"
		tr, err := domain.Dispatch(st, "site-a", 1, domain.DispatchInput{
			VehicleID: w7SeedVehicleID(t, st, "site-a", vehicle), DriverPersonID: driver.PersonID,
			DestinationSite: &dest, DeadlineMinutes: &mins,
		}, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		return tr.TripID
	}
	// Late, acknowledged OVERDUE at the far end, then arrived.
	w.at(w.t0)
	w.late = run(person("Late, Larry", nil), "TRK-LATE", 30)
	w.at(w.t0.Add(40 * time.Minute))
	by := "Officer Reyes"
	if err := deadline.AcknowledgeKind(st, "site-b", 1, w.late, deadline.AckKindRed, &by, 0, "default"); err != nil {
		t.Fatal(err)
	}
	w.at(w.t0.Add(50 * time.Minute))
	if _, err := domain.RecordExpectedArrival(st, "site-b", 1, w.late, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// Turned back 45 minutes after leaving on a 30-minute allowance.
	w.at(w.t0.Add(time.Hour))
	w.turned = run(person("Turner, Tess", nil), "TRK-BACK", 30)
	w.at(w.t0.Add(time.Hour + 45*time.Minute))
	if _, err := domain.RecordReturnToOrigin(st, "site-a", 1, w.turned, nil, nil, 0, "default", false); err != nil {
		t.Fatal(err)
	}
	// On time: 60 minutes allowed, arrived in 20.
	w.at(w.t0.Add(2 * time.Hour))
	w.onTime = run(person("Prompt, Pat", nil), "TRK-OK", 60)
	w.at(w.t0.Add(2*time.Hour + 20*time.Minute))
	if _, err := domain.RecordExpectedArrival(st, "site-b", 1, w.onTime, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// Still open: 30 minutes allowed.
	w.at(w.t0.Add(3 * time.Hour))
	w.open = run(person("Open, Olive", nil), "TRK-OPEN", 30)

	// Missed sign-outs.
	dana := person("Reyes, Dana", &acme.CompanyID)
	eve := person("Stone, Eve", nil)
	for _, at := range []time.Time{w.t0.Add(5 * time.Hour), w.t0.AddDate(0, 0, 8), w.t0.AddDate(0, 0, 9)} {
		w.at(at)
		checkIn(t, st, "site-a", dana.PersonID, nil)
		checkIn(t, st, "site-b", eve.PersonID, nil)
	}
	// "Now": two and a half hours after the open run was due.
	w.at(w.t0.Add(6 * time.Hour))
	return w
}

func (w *fix16fWorld) server(site string) *Server {
	return New(fix16fConfig(w.st, site), w.st)
}

var fix16fRowRe = regexp.MustCompile(`(?s)<tr data-trip="([^"]+)"[^>]*>(.*?)</tr>`)

func fix16fOverdueRows(t *testing.T, body string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, m := range fix16fRowRe.FindAllStringSubmatch(body, -1) {
		out[m[1]] = m[2]
	}
	return out
}

// The overdue history on the dispatch host: every run that went past its
// deadline, with how late, the acknowledgment and the outcome — and not
// the one that arrived on time.
func TestFix16fOverdueHistoryOnDispatchListsLateRunsWithAckAndOutcome(t *testing.T) {
	w := fix16fSeed(t)
	srv := w.server("site-c")
	rec := fix7Do(t, srv, "GET", fix7DispatchHost, "/history/overdue", nil)
	if rec.Code != 200 {
		t.Fatalf("GET /history/overdue on the dispatch host = %d", rec.Code)
	}
	body := rec.Body.String()
	rows := fix16fOverdueRows(t, body)
	if _, ok := rows[w.onTime]; ok {
		t.Errorf("the on-time run is listed as overdue")
	}
	want := map[string][]string{
		w.late: {"TRK-LATE", "Road Post → Pad Post", "20 minutes late",
			"OVERDUE acknowledged by Officer Reyes at Pad Post, " + w.t0.Add(40*time.Minute).Format("2006-01-02 15:04"),
			"Arrived at Pad Post, " + w.t0.Add(50*time.Minute).Format("2006-01-02 15:04")},
		w.turned: {"TRK-BACK", "15 minutes late", "not acknowledged", "Turned back — back at Road Post"},
		w.open:   {"TRK-OPEN", "2 hours 30 minutes past due, and still open", "Still open"},
	}
	for trip, wants := range want {
		row, ok := rows[trip]
		if !ok {
			t.Errorf("run %s missing from the overdue history:\n%s", trip, body)
			continue
		}
		for _, s := range wants {
			if !strings.Contains(row, s) {
				t.Errorf("run %s: row lacks %q:\n%s", trip, s, row)
			}
		}
	}
	if !strings.Contains(body, "3 runs past the deadline") || !strings.Contains(body, "of 4 with a deadline") {
		t.Errorf("heading does not count 3 of 4:\n%s", body)
	}
	if fix7ConsoleHref.MatchString(body) {
		t.Errorf("dispatch-host history page links into the console: %v", fix7ConsoleHref.FindAllString(body, -1))
	}
	if strings.Contains(body, "<form") && strings.Contains(body, `method="post"`) {
		t.Errorf("history page carries a POST form")
	}
	// Filters: a route, a site and a date range that excludes everything.
	for path, n := range map[string]int{
		"/history/overdue?route=site-a%3Esite-b": 3,
		"/history/overdue?site=site-b":           3,
		"/history/overdue?from=2026-10-01":       0,
		"/history/overdue?to=2026-09-28":         3,
	} {
		if got := len(fix16fOverdueRows(t, fix7Do(t, srv, "GET", fix7DispatchHost, path, nil).Body.String())); got != n {
			t.Errorf("%s: %d rows, want %d", path, got, n)
		}
	}
	if body := fix7Do(t, srv, "GET", fix7DispatchHost, "/history/overdue?from=yesterday", nil).Body.String(); !strings.Contains(body, "NOT FILTERED") {
		t.Errorf("a malformed date must be refused, not ignored")
	}
}

// A post's own host lists its own runs only; NDB's records of other posts'
// runs are not "its own trips".
func TestFix16fOverdueHistoryOnAPostIsItsOwnTrips(t *testing.T) {
	w := fix16fSeed(t)
	if n := len(fix16fOverdueRows(t, getAs(t, w.server("site-b"), "/history/overdue"))); n != 3 {
		t.Errorf("site-b (the destination) lists %d runs, want 3", n)
	}
	if n := len(fix16fOverdueRows(t, getAs(t, w.server("site-c"), "/history/overdue"))); n != 0 {
		t.Errorf("site-c on its own host lists %d runs it was not on, want 0", n)
	}
}

// The download is the rows on screen.
func TestFix16fOverdueHistoryCSV(t *testing.T) {
	w := fix16fSeed(t)
	rec := fix7Do(t, w.server("site-c"), "GET", fix7DispatchHost, "/history/overdue.csv", nil)
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("/history/overdue.csv = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	recs, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(rec.Body.String(), csvBOM))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 4 || recs[0][0] != "trip_id" {
		t.Fatalf("csv: %d records, header %v; want header + 3", len(recs), recs[0])
	}
	got := map[string][]string{}
	for _, r := range recs[1:] {
		got[r[0]] = r
	}
	if r := got[w.late]; r == nil || r[7] != "20" || r[8] != "arrived" || r[10] != "yes" || !strings.Contains(r[11], "OVERDUE|Officer Reyes|site-b|") {
		t.Errorf("late run's csv row: %v", got[w.late])
	}
	if r := got[w.turned]; r == nil || r[7] != "15" || r[8] != "turned_back" || r[10] != "no" {
		t.Errorf("turned-back run's csv row: %v", got[w.turned])
	}
}

// Missed sign-outs on dispatch: per site, per person, per company, per
// week, repeat offenders — and the camp's returns are exempt.
func TestFix16fSignoutsOnDispatchFollowPersonAndCompany(t *testing.T) {
	w := fix16fSeed(t)
	body := fix7Do(t, w.server("site-c"), "GET", fix7DispatchHost, "/history/signouts", nil).Body.String()
	section := func(name string) string {
		i := strings.Index(body, `data-history="`+name+`"`)
		if i < 0 {
			t.Fatalf("no %s table:\n%s", name, body)
		}
		return body[i : i+strings.Index(body[i:], "</table>")]
	}
	if !strings.Contains(body, "2 missed sign-outs") {
		t.Errorf("want 2 missed sign-outs (Dana at the road post; Eve's camp returns exempt):\n%s", body)
	}
	site := section("by-site")
	for _, want := range []string{`data-site="site-a"><th scope="row">Road Post</th><td>text-message site</td>
    <td>2</td><td>1</td><td>1</td><td>0</td>`, `data-site="site-b"><th scope="row">Pad Post</th><td>camp (exempt)</td>
    <td>0</td><td>0</td><td>0</td><td>2</td>`, `data-site="site-c"><th scope="row">North Dock</th><td>text-message site</td>`} {
		if !strings.Contains(site, want) {
			t.Errorf("by-site table lacks %q:\n%s", want, site)
		}
	}
	if p := section("by-person"); !strings.Contains(p, "Reyes, Dana <span class=\"history-flag\">REPEAT</span>") ||
		!strings.Contains(p, "week of Mon Oct 5: 1; week of Mon Sep 28: 1") || strings.Contains(p, "Stone, Eve") {
		t.Errorf("by-person table:\n%s", p)
	}
	if c := section("by-company"); !strings.Contains(c, "Acme Drilling <span class=\"history-flag\">REPEAT</span>") {
		t.Errorf("by-company table:\n%s", c)
	}
	if wk := section("by-week"); !strings.Contains(wk, "2026-09-28") || !strings.Contains(wk, "2026-10-05") {
		t.Errorf("by-week table:\n%s", wk)
	}
	// The site filter narrows to one site; the camp has none.
	if b := fix7Do(t, w.server("site-c"), "GET", fix7DispatchHost, "/history/signouts?site=site-b", nil).Body.String(); !strings.Contains(b, "0 missed sign-outs") {
		t.Errorf("site-b filter should show 0 missed sign-outs")
	}
}

// Each post sees its own site's only. The text-message post counts Dana's;
// the camp post counts nothing.
func TestFix16fSignoutsOnAPostAreThatSiteOnly(t *testing.T) {
	w := fix16fSeed(t)
	if b := getAs(t, w.server("site-a"), "/history/signouts"); !strings.Contains(b, "2 missed sign-outs") || strings.Contains(b, `data-site="site-b"`) {
		t.Errorf("site-a: want its own 2 and no other site:\n%s", b)
	}
	if b := getAs(t, w.server("site-b"), "/history/signouts"); !strings.Contains(b, "0 missed sign-outs") || !strings.Contains(b, "camp (exempt)") {
		t.Errorf("site-b (camp): want 0 and the camp exemption:\n%s", b)
	}
	// Without the peer's site type, dispatch does not guess.
	cfg := fix16fConfig(w.st, "site-c")
	for i := range cfg.Peers {
		cfg.Peers[i].SiteType = ""
	}
	b := fix7Do(t, New(cfg, w.st), "GET", fix7DispatchHost, "/history/signouts", nil).Body.String()
	if !strings.Contains(b, "NOT JUDGED") || !strings.Contains(b, "type not known at this post") {
		t.Errorf("untyped peers must be named as not judged:\n%s", b)
	}
}

// The lost-signal history records an outage as it happens, from the last
// contact to the first contact after, and shows it on dispatch and on the
// post's status page.
func TestFix16fSignalOutageIsRecordedAndListed(t *testing.T) {
	w := fix16fSeed(t)
	srv := w.server("site-c")
	now := w.t0.Add(6 * time.Hour)
	tick := func(at time.Time, a, b *float64) {
		t.Helper()
		srv.SyncSnapshot = fix15Snapshot("site-c", 3600, fix15Peer{"site-a", a, ""}, fix15Peer{"site-b", b, ""})
		if err := srv.recordSignal(at); err != nil {
			t.Fatal(err)
		}
	}
	tick(now, fix15Ago(2), fix15Ago(2))
	tick(now.Add(20*time.Second), fix15Ago(22), fix15Ago(1)) // inside the 30 s ceiling: not lost
	tick(now.Add(40*time.Second), fix15Ago(42), fix15Ago(1)) // lost since now-2s
	tick(now.Add(5*time.Minute), fix15Ago(5*60-2+0), fix15Ago(1))
	body := fix7Do(t, srv, "GET", fix7DispatchHost, "/history/signal", nil).Body.String()
	if !strings.Contains(body, "1 outage") || !strings.Contains(body, "still lost") || !strings.Contains(body, "1 still lost now") {
		t.Fatalf("an open outage must be listed as still lost:\n%s", body)
	}
	// Healed: contact 3 s before the 12-minute tick.
	tick(now.Add(12*time.Minute), fix15Ago(3), fix15Ago(1))
	body = fix7Do(t, srv, "GET", fix7DispatchHost, "/history/signal", nil).Body.String()
	from, back := now.Add(-2*time.Second).Format("2006-01-02 15:04"), now.Add(12*time.Minute-3*time.Second).Format("2006-01-02 15:04")
	for _, want := range []string{"<th scope=\"row\">Road Post</th>", from, back, "11 minutes", "1 outage"} {
		if !strings.Contains(body, want) {
			t.Errorf("healed outage: page lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Pad Post</th>") {
		t.Errorf("site-b was never lost")
	}
	status := getAs(t, srv, "/status")
	if !strings.Contains(status, "Lost-signal history") || !strings.Contains(status, "<th scope=\"row\">Road Post</th>") {
		t.Errorf("the status page must show the recorded outage")
	}
	rec := fix7Do(t, srv, "GET", fix7DispatchHost, "/history/signal.csv", nil)
	recs, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(rec.Body.String(), csvBOM))).ReadAll()
	if err != nil || len(recs) != 2 || recs[1][0] != "site-a" || recs[1][5] != "719" || recs[1][6] != "false" {
		t.Errorf("signal csv: %v %v", recs, err)
	}
}

// A post restarted mid-outage: the open row stays open through the start-up
// grace (no contact yet is not "back"), and closes at the first contact,
// marked as spanning the restart.
func TestFix16fSignalOutageAcrossARestart(t *testing.T) {
	w := fix16fSeed(t)
	srv := w.server("site-c")
	now := w.t0.Add(6 * time.Hour)
	srv.SyncSnapshot = fix15Snapshot("site-c", 3600, fix15Peer{"site-a", fix15Ago(60), ""})
	if err := srv.recordSignal(now); err != nil {
		t.Fatal(err)
	}
	// Restarted 10 minutes later: nothing heard yet, 5 s up.
	srv.SyncSnapshot = fix15Snapshot("site-c", 5, fix15Peer{"site-a", nil, ""})
	if err := srv.recordSignal(now.Add(10 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if b := getAs(t, srv, "/history/signal"); !strings.Contains(b, "still lost") {
		t.Fatalf("start-up grace must not close the outage:\n%s", b)
	}
	srv.SyncSnapshot = fix15Snapshot("site-c", 15, fix15Peer{"site-a", fix15Ago(1), ""})
	if err := srv.recordSignal(now.Add(10*time.Minute + 10*time.Second)); err != nil {
		t.Fatal(err)
	}
	if b := getAs(t, srv, "/history/signal"); !strings.Contains(b, "this post restarted during it") || strings.Contains(b, "still lost") {
		t.Errorf("closed across a restart:\n%s", b)
	}
}

// Read-only on the dispatch host: a POST is refused, and with no recorder
// the signal page says it keeps no record rather than "no outages".
func TestFix16fHistoryIsReadOnlyAndHonestWithoutARecord(t *testing.T) {
	w := fix16fSeed(t)
	srv := w.server("site-c")
	for _, p := range []string{"/history/overdue", "/history/signouts", "/history/signal"} {
		if rec := fix7Do(t, srv, "POST", fix7DispatchHost, p, nil); rec.Code != 403 {
			t.Errorf("POST %s on the dispatch host = %d, want 403", p, rec.Code)
		}
		req := httptest.NewRequest("GET", p, nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if strings.Contains(rec.Body.String(), "data-history=") {
			t.Errorf("GET %s without a login rendered the report", p)
		}
	}
	st2, err := store.Open(filepath.Join(t.TempDir(), "bare.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st2.Close() })
	if err := domain.Register(st2); err != nil {
		t.Fatal(err)
	}
	bare := New(fix16fConfig(st2, "site-c"), st2)
	bare.SyncSnapshot = func() repl.Snapshot { return repl.Snapshot{SiteID: "site-c"} }
	if b := getAs(t, bare, "/history/signal"); !strings.Contains(b, "NOT RECORDED") {
		t.Errorf("a post with no record must say so:\n%s", b)
	}
}
