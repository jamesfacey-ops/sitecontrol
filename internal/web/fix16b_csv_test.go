// fix16b_csv_test.go — FIX16 stream B (James-approved 2026-10-06):
//
//  1. a late run on /analytics/site/{site} carries the response essentials
//     — route with both ends, deadline as a clock time, and everyone aboard
//     with company and full phone — and a run inside its allowance does not;
//  2. "Download CSV" on search results and on the two sitectl reports: the
//     same columns and rows as the page, UTF-8 BOM, attachment with a dated
//     filename, formula cells neutralised, anonymous → login, dispatch-host
//     GET allowed and POST still 403.
//
// Every test here is black-box over HTTP (and the pre-existing domain report
// writer), so it compiles and FAILS against the pre-FIX16 tree.
package web

import (
	"bytes"
	"encoding/csv"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

const fix16BOM = "\xef\xbb\xbf"

var fix16FilenameRe = regexp.MustCompile(`^attachment; filename="sitecontrol-[a-z-]+-\d{8}-\d{4}\.csv"$`)

func fix16Get(t *testing.T, srv *Server, path string, login bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	if login {
		req.AddCookie(loginCookie(t, srv, testLoginPassword))
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// fix16CSV checks the download envelope (status, type, disposition, BOM)
// and returns the parsed records.
func fix16CSV(t *testing.T, rec *httptest.ResponseRecorder, what string) [][]string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: status %d, want 200:\n%s", what, rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Errorf("%s: Content-Type %q, want text/csv; charset=utf-8", what, ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !fix16FilenameRe.MatchString(cd) {
		t.Errorf("%s: Content-Disposition %q, want attachment; filename=\"sitecontrol-<what>-YYYYMMDD-HHMM.csv\"", what, cd)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, fix16BOM) {
		t.Fatalf("%s: body does not start with the UTF-8 BOM (Excel): %q", what, body[:min(len(body), 20)])
	}
	recs, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, fix16BOM))).ReadAll()
	if err != nil {
		t.Fatalf("%s: not parseable CSV: %v\n%s", what, err, body)
	}
	if len(recs) == 0 {
		t.Fatalf("%s: no header row", what)
	}
	return recs
}

var (
	fix16TagRe = regexp.MustCompile(`(?s)<[^>]*>`)
	fix16WSRe  = regexp.MustCompile(`\s+`)
	fix16TrRe  = regexp.MustCompile(`(?s)<tr[^>]*>(.*?)</tr>`)
	fix16TdRe  = regexp.MustCompile(`(?s)<t[dh][^>]*>(.*?)</t[dh]>`)
)

func fix16Text(s string) string {
	return strings.TrimSpace(fix16WSRe.ReplaceAllString(html.UnescapeString(fix16TagRe.ReplaceAllString(s, " ")), " "))
}

// fix16Table reads the page table carrying attr as rows of cell text,
// header first. A cell the page fills with a note for an absent value is
// read as blank, as the CSV leaves it.
func fix16Table(t *testing.T, body, attr string) [][]string {
	t.Helper()
	at := strings.Index(body, attr)
	if at < 0 {
		t.Fatalf("page has no table %s", attr)
	}
	tbl := body[at:]
	tbl = tbl[:strings.Index(tbl, "</table>")]
	// FIX16a: a person's "last seen" link is page navigation, not a value.
	tbl = regexp.MustCompile(`(?s)<a [^>]*data-last-seen-link[^>]*>.*?</a>`).ReplaceAllString(tbl, "")
	var out [][]string
	for _, tr := range fix16TrRe.FindAllStringSubmatch(tbl, -1) {
		var row []string
		for _, td := range fix16TdRe.FindAllStringSubmatch(tr[1], -1) {
			c := fix16Text(td[1])
			if c == "(no company recorded)" || c == "none on file" {
				c = ""
			}
			row = append(row, c)
		}
		out = append(out, row)
	}
	return out
}

func fix16Equal(t *testing.T, what string, got, want [][]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: CSV has %d rows (header included), page %d:\ncsv:  %q\npage: %q", what, len(got), len(want), got, want)
	}
	for i := range want {
		if strings.Join(got[i], "\x1f") != strings.Join(want[i], "\x1f") {
			t.Errorf("%s: row %d differs:\ncsv:  %q\npage: %q", what, i, got[i], want[i])
		}
	}
}

// fix16CSVHref is the page's own "Download CSV" link for a group.
func fix16CSVHref(t *testing.T, body, group string) string {
	t.Helper()
	m := regexp.MustCompile(`<a href="([^"]+)" data-csv="` + group + `"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("page offers no Download CSV link for %s", group)
	}
	return html.UnescapeString(m[1])
}

// The search CSV is the page's rows, cell for cell, in every group — and the
// link carries the current search, so following it is all it takes.
func TestFix16SearchCSVMatchesThePage(t *testing.T) {
	w := anRoadWorld(t)
	rider := w.riding[2] // Petrov, Sasha — aboard TRK-100
	ph := "907-555-0177"
	if _, err := domain.AmendPerson(w.st, "site-a", 1, domain.PersonAmendment{PersonID: rider.PersonID, Phone: &ph}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	page := fix16Get(t, w.srv, "/search?last=Petrov", true).Body.String()
	for _, g := range []string{"people", "visits", "trips"} {
		href := fix16CSVHref(t, page, g)
		if !strings.HasPrefix(href, "/search.csv?") || !strings.Contains(href, "last=Petrov") || !strings.Contains(href, "group="+g) {
			t.Errorf("%s link %q does not carry the current search", g, href)
		}
		got := fix16CSV(t, fix16Get(t, w.srv, href, true), "search "+g)
		want := fix16Table(t, page, `data-search-group="`+g+`"`)
		fix16Equal(t, "search "+g, got, want)
		if len(got) < 2 {
			t.Errorf("search %s: precondition — Petrov must be found in this group", g)
		}
	}
	people := fix16CSV(t, fix16Get(t, w.srv, fix16CSVHref(t, page, "people"), true), "people")
	if people[0][3] != "Phone" || people[1][3] != ph {
		t.Errorf("the people CSV must carry the phone in full (%q): %q", ph, people)
	}

	// The narrow-results filters apply too: a Result type filter that hides
	// trips leaves no trips link and the trips download refuses.
	narrowed := fix16Get(t, w.srv, "/search?last=Petrov&in_type=people", true).Body.String()
	if strings.Contains(narrowed, `data-csv="trips"`) {
		t.Errorf("a group the filters hide must offer no download")
	}
	if rec := fix16Get(t, w.srv, "/search.csv?last=Petrov&in_type=people&group=trips", true); rec.Code == http.StatusOK {
		t.Errorf("the download of a group the filters hide = 200, want a refusal")
	}
	// Nothing searched: no empty file that reads as "nobody matched".
	if rec := fix16Get(t, w.srv, "/search.csv?group=people", true); rec.Code != http.StatusBadRequest {
		t.Errorf("download with no search = %d, want 400", rec.Code)
	}
}

// A cell that starts with = + - @ is prefixed with a quote, so Excel shows it
// as text — and the page still shows the value as recorded.
func TestFix16CSVNeutralisesFormulaCells(t *testing.T) {
	w := anRoadWorld(t)
	rider := w.riding[2]
	for _, ph := range []string{`=HYPERLINK("http://example.invalid","x")`, "+1 907 555 0177", "-907", "@SUM(A1)"} {
		ph := ph
		if _, err := domain.AmendPerson(w.st, "site-a", 1, domain.PersonAmendment{PersonID: rider.PersonID, Phone: &ph}, 0, "default"); err != nil {
			t.Fatal(err)
		}
		recs := fix16CSV(t, fix16Get(t, w.srv, "/search.csv?last=Petrov&group=people", true), "people")
		if len(recs) != 2 {
			t.Fatalf("want Petrov's one row: %q", recs)
		}
		if got := recs[1][3]; got != "'"+ph {
			t.Errorf("phone %q exported as %q, want it prefixed with a quote", ph, got)
		}
		if page := fix16Get(t, w.srv, "/search?last=Petrov", true).Body.String(); !strings.Contains(html.UnescapeString(page), ph) {
			t.Errorf("the page must still show %q as recorded", ph)
		}
	}
}

// The two sitectl reports, as a page and a download: the CSV is the report
// writer's own rows (what `sitectl -report` prints), and the page's table is
// the same rows.
func TestFix16ReportsMatchSitectlAndThePage(t *testing.T) {
	w := anRoadWorld(t)
	recs, _, err := domain.VisitReport(w.st, domain.SiteTypes{w.srv.Cfg.SiteID: w.srv.Cfg.SiteType})
	if err != nil {
		t.Fatal(err)
	}
	for name, write := range map[string]func(*bytes.Buffer) error{
		"visits": func(b *bytes.Buffer) error { return domain.WriteVisitReportCSV(b, recs) },
		"signout-failures": func(b *bytes.Buffer) error {
			return domain.WritePersonSignoutFailuresCSV(b, domain.SignoutFailuresByPerson(recs))
		},
	} {
		var b bytes.Buffer
		if err := write(&b); err != nil {
			t.Fatal(err)
		}
		want, err := csv.NewReader(&b).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		if len(want) < 2 {
			t.Fatalf("%s: precondition — the fixture must give the report rows", name)
		}
		got := fix16CSV(t, fix16Get(t, w.srv, "/reports/"+name+".csv", true), "report "+name)
		fix16Equal(t, "report "+name+" vs sitectl", got, want)

		page := fix16Get(t, w.srv, "/reports/"+name, true)
		if page.Code != http.StatusOK {
			t.Fatalf("/reports/%s = %d", name, page.Code)
		}
		body := page.Body.String()
		fix16Equal(t, "report "+name+" page", fix16Table(t, body, `data-report="`+name+`"`), want)
		if href := fix16CSVHref(t, body, name); href != "/reports/"+name+".csv" {
			t.Errorf("report %s page links its download as %q", name, href)
		}
		if strings.Contains(body, "<form") || strings.Contains(body, "<button") {
			t.Errorf("report %s page must record nothing — no form, no button", name)
		}
	}
	if rec := fix16Get(t, w.srv, "/reports/nosuch.csv", true); rec.Code != http.StatusNotFound {
		t.Errorf("unknown report = %d, want 404", rec.Code)
	}
}

// Anonymous → the login form, never a file and never a name.
func TestFix16DownloadsNeedTheLogin(t *testing.T) {
	w := anRoadWorld(t)
	for _, p := range []string{"/search.csv?last=Petrov&group=people", "/reports/visits.csv", "/reports/signout-failures.csv",
		"/reports/visits", "/reports/signout-failures"} {
		rec := fix16Get(t, w.srv, p, false)
		body := rec.Body.String()
		if !strings.Contains(body, `name="password"`) || strings.HasPrefix(rec.Header().Get("Content-Type"), "text/csv") ||
			rec.Header().Get("Content-Disposition") != "" {
			t.Errorf("anonymous GET %s must get the login form, not a file (type %q)", p, rec.Header().Get("Content-Type"))
		}
		for _, who := range append(append([]domain.Person{}, w.staying...), w.riding...) {
			if strings.Contains(body, who.Name) {
				t.Errorf("anonymous GET %s leaked %q", p, who.Name)
			}
		}
	}
}

// On the dispatch host the downloads are allowed reads; a POST to them is
// still refused 403 before any handler runs.
func TestFix16DispatchHostAllowsCSVGetAndRefusesPost(t *testing.T) {
	w := anRoadWorld(t)
	for _, p := range []string{"/search.csv?last=Petrov&group=trips", "/reports/visits.csv", "/reports/signout-failures.csv"} {
		fix16CSV(t, fix7Do(t, w.srv, "GET", fix7DispatchHost, p, nil), "dispatch "+p)
		if rec := fix7Do(t, w.srv, "POST", fix7DispatchHost, strings.Split(p, "?")[0], nil); rec.Code != http.StatusForbidden {
			t.Errorf("POST %s on the dispatch host = %d, want 403", p, rec.Code)
		}
	}
	for _, p := range []string{"/reports/visits", "/reports/signout-failures", "/search?last=Petrov"} {
		rec := fix7Do(t, w.srv, "GET", fix7DispatchHost, p, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s on the dispatch host = %d, want 200", p, rec.Code)
		}
		if m := fix7ConsoleHref.FindString(rec.Body.String()); m != "" {
			t.Errorf("GET %s on the dispatch host links into the operator console (%s)", p, m)
		}
	}
}

// fix16RunArticle is the run card for a trip on the site page.
func fix16RunArticle(t *testing.T, body, tripID string) string {
	t.Helper()
	at := strings.Index(body, `data-an-trip="`+tripID+`"`)
	if at < 0 {
		t.Fatalf("no run card for %s", tripID)
	}
	a := body[at:]
	return a[:strings.Index(a, "</article>")]
}

// A run PAST its allowance (judged by a third post's page) carries the
// response essentials; the same run inside its allowance does not, and
// nobody merely on site has a phone shown.
func TestFix16SitePagePastAllowanceRunCarriesResponseEssentials(t *testing.T) {
	w := anRoadWorld(t)
	phones := map[string]string{}
	for i, p := range w.riding {
		ph := []string{"907-555-0101", "907-555-0102", "907-555-0103"}[i]
		phones[p.Name] = ph
		if _, err := domain.AmendPerson(w.st, "site-a", 1, domain.PersonAmendment{PersonID: p.PersonID, Phone: &ph}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	staying := "907-555-0999"
	if _, err := domain.AmendPerson(w.st, "site-a", 1, domain.PersonAmendment{PersonID: w.staying[0].PersonID, Phone: &staying}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	within := fix16RunArticle(t, fix16Get(t, w.srv, "/analytics/site/site-a", true).Body.String(), w.trip.TripID)
	if strings.Contains(within, "data-an-essentials") || strings.Contains(within, "907-555") {
		t.Errorf("a run inside its allowance must carry no response block and no phone:\n%s", within)
	}

	later := time.Now().Add(2 * time.Hour).UnixMilli()
	w.st.Clock = func() store.ClockReading {
		return store.ClockReading{WallMS: later, MaxErrorUS: 500, State: store.ClockSynced}
	}
	body := fix16Get(t, w.srv, "/analytics/site/site-a", true).Body.String()
	run := fix16RunArticle(t, body, w.trip.TripID)
	if !strings.Contains(run, "PAST ALLOWANCE") {
		t.Fatalf("precondition: the run must be past its allowance:\n%s", run)
	}
	route := w.srv.Cfg.SiteName("site-a") + " → " + w.srv.Cfg.SiteName("site-b")
	for _, want := range []string{"data-an-essentials", "<dt>Route</dt><dd>" + route + "</dd>", "<dt>Deadline</dt>",
		"TRK-100", "Driver ", "dispatched ", "by "} {
		if !strings.Contains(html.UnescapeString(run), want) {
			t.Errorf("past-allowance run lacks %q:\n%s", want, run)
		}
	}
	if !regexp.MustCompile(`<dt>Deadline</dt><dd>\d\d:\d\d \(`).MatchString(html.UnescapeString(run)) {
		t.Errorf("the deadline must be a clock time with its clock named:\n%s", run)
	}
	for _, p := range w.riding {
		li := regexp.MustCompile(`<li>` + regexp.QuoteMeta(html.EscapeString(p.Name)) + ` <span class="an-contact">([^<]*)<span class="an-phone">([^<]*)</span>`).FindStringSubmatch(run)
		if li == nil {
			t.Errorf("%s is not listed with company and phone:\n%s", p.Name, run)
			continue
		}
		if li[2] != phones[p.Name] {
			t.Errorf("%s: phone %q, want %q in full", p.Name, li[2], phones[p.Name])
		}
		if !strings.Contains(li[1], "no company recorded") && strings.TrimSpace(li[1]) == "·" {
			t.Errorf("%s: no company text", p.Name)
		}
	}
	if strings.Contains(body, staying) {
		t.Errorf("a person merely on site must not have their phone shown")
	}
	if strings.Contains(body, "<form") || strings.Contains(body, "<button") || strings.Contains(body, "/alerts/ack") {
		t.Errorf("the site page must stay read-only")
	}
}

// A run this post's own scan calls OVERDUE carries the essentials, with the
// company; on the dispatch host the page is still read-only with no console
// link.
func TestFix16SitePageOverdueRunCarriesResponseEssentials(t *testing.T) {
	srv, _, tripID := fix7Overdue(t)
	persons, err := domain.Persons(srv.Store)
	if err != nil {
		t.Fatal(err)
	}
	var driver domain.Person
	for _, p := range persons {
		if p.Name == "Dana Driver" {
			driver = p
		}
	}
	ph := "907-555-0142"
	if _, err := domain.AmendPerson(srv.Store, "site-b", 1, domain.PersonAmendment{PersonID: driver.PersonID, Phone: &ph}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{fix7GateHost, fix7DispatchHost} {
		rec := fix7Do(t, srv, "GET", host, "/analytics/site/site-b", nil)
		body := rec.Body.String()
		run := html.UnescapeString(fix16RunArticle(t, body, tripID))
		if !strings.Contains(run, ">OVERDUE<") {
			t.Fatalf("%s: precondition — the run must read OVERDUE:\n%s", host, run)
		}
		for _, want := range []string{"data-an-essentials", "<dt>Route</dt><dd>" + srv.Cfg.SiteName("site-b") + " → " + srv.Cfg.SiteName("site-a") + "</dd>",
			"<dt>Deadline</dt><dd>", "Dana Driver <span class=\"an-contact\">Kakivik Asset Management · <span class=\"an-phone\">" + ph + "</span>"} {
			if !strings.Contains(run, want) {
				t.Errorf("%s: overdue run lacks %q:\n%s", host, want, run)
			}
		}
		if strings.Contains(body, "<form") || strings.Contains(body, "/alerts/ack") {
			t.Errorf("%s: the site page must carry no ack", host)
		}
		if m := fix7ConsoleHref.FindString(body); host == fix7DispatchHost && m != "" {
			t.Errorf("dispatch host: console link %s", m)
		}
	}
}
