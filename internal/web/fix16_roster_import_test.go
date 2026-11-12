package web

// FIX16 H (c): roster import from a CSV. Every row is judged by intake's own
// rules; the dry run records nothing; the commit is bound to the file AND
// the roster it was judged against; nothing about a row is ever logged.

import (
	"bytes"
	"html"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

func importUpload(t *testing.T, srv *Server, cookie *http.Cookie, csvText string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "roster.csv")
	if err != nil {
		t.Fatal(err)
	}
	fw.Write([]byte(csvText))
	mw.Close()
	req := httptest.NewRequest("POST", "/roster/import/preview", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

var importRowRe = regexp.MustCompile(`<tr data-verdict="([a-z_]+)" data-line="(\d+)">`)

func importVerdicts(body string) map[string]string {
	out := map[string]string{}
	for _, m := range importRowRe.FindAllStringSubmatch(body, -1) {
		out[m[2]] = m[1]
	}
	return out
}

func importFormOf(t *testing.T, body string) (token, csvText string) {
	t.Helper()
	tm := regexp.MustCompile(`name="token" value="([0-9a-f]{64})"`).FindStringSubmatch(body)
	cm := regexp.MustCompile(`(?s)<textarea name="csv"[^>]*>(.*?)</textarea>`).FindStringSubmatch(body)
	if tm == nil || cm == nil {
		t.Fatalf("the preview carries no commit form:\n%s", body)
	}
	return tm[1], html.UnescapeString(cm[1])
}

func importCommit(t *testing.T, srv *Server, cookie *http.Cookie, token, csvText string, affirm ...string) *httptest.ResponseRecorder {
	t.Helper()
	return postFormWithCookie(t, srv, "/roster/import/commit", url.Values{"token": {token}, "csv": {csvText}, "affirm": affirm}, cookie)
}

// importWorld is an orientation post (site-b) whose roster already holds
// "Moreno, Luis" (live) and "Ashton, Joss" (RETIRED), both enrolled at site-a.
func importWorld(t *testing.T, orientation bool) (*Server, *store.Store, *http.Cookie) {
	t.Helper()
	srv, st, _ := newOrientationServer(t, orientation)
	if _, err := domain.CreatePerson(st, "site-a", 1, "Moreno, Luis", nil, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	ash, err := domain.CreatePerson(st, "site-a", 1, "Ashton, Joss", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	yes := "true"
	if _, err := domain.AmendPerson(st, "site-a", 1, domain.PersonAmendment{PersonID: ash.PersonID, Retired: &yes}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	return srv, st, loginCookie(t, srv, testLoginPassword)
}

const importCSV = "Last Name,First Name,Phone,Company,Trade\n" +
	"Quinn,Rory,555-0101,Brand New Co,Welder\n" + // 2 new (new company, new trade)
	"Moreno,Luis,555-0102,Brand New Co,\n" + // 3 possible duplicate
	"Quinn,Rory,555-0103,BRAND NEW CO,\n" + // 4 same person earlier in file
	"Ashton,Joss,555-0104,Acme,\n" + // 5 retired name ("Ashton, Joss")
	"Lee,,555-0105,Acme,\n" + // 6 no first name
	"Park,Min,,Acme,\n" + // 7 no phone
	"Kerr,Dana,555-0107,,\n" + // 8 no company
	"\n" + // blank row: skipped, not a row
	"Ngata,Tui,555-0109,brand new co,welder\n" // 10 new; company and trade fold onto row 2's

func TestFix16RosterImportPreviewJudgesEveryRowAndRecordsNothing(t *testing.T) {
	srv, st, cookie := importWorld(t, true)
	before := orientationWatermark(t, st)
	rec := importUpload(t, srv, cookie, importCSV)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview = %d:\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	want := map[string]string{"2": "new", "3": "duplicate", "4": "dup_in_file", "5": "retired", "6": "invalid", "7": "invalid", "8": "invalid", "10": "new"}
	got := importVerdicts(body)
	for line, v := range want {
		if got[line] != v {
			t.Errorf("row %s: verdict %q, want %q", line, got[line], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("verdict rows = %v, want exactly %v", got, want)
	}
	if after := orientationWatermark(t, st); after != before {
		t.Fatalf("the dry run authored: watermark %d -> %d", before, after)
	}
	for _, s := range []string{"New companies that will be created: <strong>Brand New Co</strong>.", "New trades that will be created: <strong>Welder</strong>.",
		"Moreno, Luis", "RETIRED", "A phone number is needed", "A company is needed", "Both the last name and the first name", "No orientation is recorded by an import"} {
		if !strings.Contains(body, s) && !strings.Contains(html.UnescapeString(body), s) {
			t.Errorf("preview does not say %q", s)
		}
	}
	if !strings.Contains(body, `name="affirm" value="3"`) || strings.Contains(body, `name="affirm" value="5"`) {
		t.Error("only a possible duplicate may be affirmed; a retired name never")
	}
}

func TestFix16RosterImportCommitAddsOnlyCommittableRowsThroughIntakesRules(t *testing.T) {
	srv, st, cookie := importWorld(t, true)
	persons0, _ := domain.Persons(st)
	token, csvText := importFormOf(t, importUpload(t, srv, cookie, importCSV).Body.String())
	rec := importCommit(t, srv, cookie, token, csvText, "3") // affirm Moreno as a different person
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "3 person(s) added") {
		t.Fatalf("commit = %d:\n%s", rec.Code, rec.Body.String())
	}
	persons, _ := domain.Persons(st)
	if len(persons) != len(persons0)+3 {
		t.Fatalf("persons %d -> %d, want +3 (rows 2, 3 affirmed, 10)", len(persons0), len(persons))
	}
	companies, _ := domain.Companies(st)
	trades, _ := domain.Trades(st)
	if len(companies) != 1 || companies[0].Name != "Brand New Co" {
		t.Errorf("companies = %+v, want ONE Brand New Co for three rows", companies)
	}
	if len(trades) != 1 || trades[0].Name != "Welder" {
		t.Errorf("trades = %+v, want ONE Welder", trades)
	}
	var quinn *domain.Person
	morenos := 0
	for i, p := range persons {
		switch p.Name {
		case "Quinn, Rory":
			quinn = &persons[i]
		case "Moreno, Luis":
			morenos++
		case "Ashton, Joss":
			if !p.IsRetired() {
				t.Error("a retired record was touched")
			}
		case "Lee", "Park, Min", "Kerr, Dana":
			t.Errorf("an invalid row was added: %s", p.Name)
		}
		if p.Name == "Ashton, Joss" && p.SiteID == "site-b" {
			t.Error("a retired name was enrolled again")
		}
	}
	if quinn == nil || quinn.SiteID != "site-b" || quinn.Phone == nil || *quinn.Phone != "555-0101" || quinn.LastName == nil || *quinn.LastName != "Quinn" || quinn.CompanyID == nil || quinn.TradeID == nil {
		t.Fatalf("Quinn not enrolled as intake would: %+v", quinn)
	}
	if morenos != 2 {
		t.Errorf("Moreno records = %d, want 2 (the affirmed different person)", morenos)
	}
	// Q3: no orientation recorded by an import, even at an orientation post.
	if held, _ := domain.OrientationHeld(st, quinn.PersonID, "site-b"); held != nil {
		t.Error("an import recorded an orientation")
	}
}

func TestFix16RosterImportRefusesAStaleRosterAndReimportDoublesNothing(t *testing.T) {
	srv, st, cookie := importWorld(t, false)
	token, csvText := importFormOf(t, importUpload(t, srv, cookie, importCSV).Body.String())
	// The roster changes between preview and commit (a record arrives).
	if _, err := domain.CreatePerson(st, "site-a", 1, "Holt, Sam", nil, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	before := orientationWatermark(t, st)
	rec := importCommit(t, srv, cookie, token, csvText)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "The roster changed since this file was checked") {
		t.Fatalf("stale commit = %d:\n%s", rec.Code, rec.Body.String())
	}
	if after := orientationWatermark(t, st); after != before {
		t.Fatal("a stale commit authored")
	}
	// The refusal re-previews with a fresh token, which commits.
	token2, _ := importFormOf(t, rec.Body.String())
	if rec := importCommit(t, srv, cookie, token2, csvText); rec.Code != http.StatusOK {
		t.Fatalf("fresh commit = %d", rec.Code)
	}
	n1, _ := domain.Persons(st)
	// The same file again: every row that was added is now ALREADY on the
	// roster (FIX17b, sc-stress F16: same name, phone and company — never
	// offered for ticking), nothing is new, and committing adds nobody.
	// ⟦was: v["2"] and v["10"] == "duplicate", each with a "different person
	// — add them" box⟧
	body := importUpload(t, srv, cookie, importCSV).Body.String()
	v := importVerdicts(body)
	if v["2"] != "already" || v["10"] != "already" {
		t.Fatalf("re-import verdicts %v", v)
	}
	token3, csv3 := importFormOf(t, body)
	importCommit(t, srv, cookie, token3, csv3)
	if n2, _ := domain.Persons(st); len(n2) != len(n1) {
		t.Fatalf("re-import added %d person(s)", len(n2)-len(n1))
	}
}

func TestFix16RosterImportParsing(t *testing.T) {
	srv, _, cookie := importWorld(t, false)
	semi := "\xef\xbb\xbfSurname;Given name;Mobile;Employer;Craft;Notes\nOkafor;Ada;555 1;Acme;Rigger;x\n"
	if v := importVerdicts(importUpload(t, srv, cookie, semi).Body.String()); v["2"] != "new" {
		t.Errorf("BOM + semicolons + header aliases: %v", v)
	}
	for _, c := range []struct{ name, csv, want string }{
		{"missing column", "last,first,company\nA,B,C\n", "Missing: phone"},
		{"empty", "", "the file is empty"},
		{"header only", "last,first,phone,company\n", "no people"},
		{"not utf-8", "last,first,phone,company\nM\xfcller,A,1,C\n", "not UTF-8"},
		{"too many", "last,first,phone,company\n" + strings.Repeat("A,B,1,C\n", importMaxRows+1), "more than 2000"},
		{"too big", "last,first,phone,company\n" + strings.Repeat("x", importMaxBytes), "larger than"},
	} {
		rec := importUpload(t, srv, cookie, c.csv)
		if rec.Code != http.StatusConflict || !strings.Contains(html.UnescapeString(rec.Body.String()), c.want) {
			t.Errorf("%s: %d, want 409 saying %q", c.name, rec.Code, c.want)
		}
	}
}

func TestFix16RosterImportIsGatedAndNeverLogsARow(t *testing.T) {
	srv, st, cookie := importWorld(t, false)
	// Logged out: the login form, never the upload form; a commit authors nothing.
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/roster/import", nil))
	if strings.Contains(rec.Body.String(), `action="/roster/import/preview"`) {
		t.Fatal("the import page is served without a login")
	}
	before := orientationWatermark(t, st)
	importCommit(t, srv, nil, strings.Repeat("0", 64), importCSV)
	if orientationWatermark(t, st) != before {
		t.Fatal("a logged-out commit authored")
	}
	// Logged in: nothing about a row reaches the log.
	var logged bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&logged)
	defer log.SetOutput(orig)
	token, csvText := importFormOf(t, importUpload(t, srv, cookie, importCSV).Body.String())
	importCommit(t, srv, cookie, token, csvText, "3")
	log.SetOutput(orig)
	for _, s := range []string{"Quinn", "555-01", "Moreno", "Brand New"} {
		if strings.Contains(logged.String(), s) {
			t.Fatalf("the log carries row content %q:\n%s", s, logged.String())
		}
	}
	// The roster page links to the import.
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/roster", nil))
	if !strings.Contains(rec.Body.String(), `href="/roster/import"`) {
		t.Error("the roster page does not link to the import")
	}
}
