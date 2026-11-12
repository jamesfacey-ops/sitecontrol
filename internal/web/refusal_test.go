// refusal_test.go — W4 (2026-10-03): the wording layer. Every assertion here
// is on what an officer READS (rendered text) and on the status code being
// kept honest, never on the status code alone.
package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// internalLeakRe is what must never reach an officer: an error chain's
// package prefix, a design-document section sign, a Go formatting fault, a
// driver error, or the muster reprint's internal function name.
var internalLeakRe = regexp.MustCompile(`\b(?:domain|store|repl): |§|%!|\bsql: |VisitsByID`)

// visibleText strips the stylesheet, scripts and comments, so a CSS rule
// name or a code comment is never mistaken for words on the page.
func visibleText(body string) string {
	body = styleRe.ReplaceAllString(body, "")
	body = regexp.MustCompile(`(?s)<script.*?</script>`).ReplaceAllString(body, "")
	body = regexp.MustCompile(`(?s)<!--.*?-->`).ReplaceAllString(body, "")
	return body
}

// --- the mapper itself ---

func TestOfficerTextMapsInternalErrorStringsToPlainSentences(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		// The chains the test campaign saw on officer screens (G-069/079/
		// 109/121, R-131, /trips/arrive).
		{"domain: dispatch: deadline must be a positive number of minutes", "deadline must be a positive number of minutes"},
		{"domain: dispatch: vehicle id is required", "vehicle id is required"},
		{"domain: expected arrival: trip ice-road-trip-000000000000dead is not known at this site yet — record an unsolicited arrival instead (§9.2)",
			"trip ice-road-trip-000000000000dead is not known at this site yet — record an unsolicited arrival instead"},
		{`domain: amend person: refused — trade id "bogus" is not in the trade register; pick a trade from the list, or add it there first; nothing was recorded`,
			`trade id "bogus" is not in the trade register; pick a trade from the list, or add it there first; nothing was recorded`},
		{"domain: amend person: person_id is required", "person_id is required"},
		{"domain: dispatch: driver is required (and is always an occupant, §7)", "driver is required (and is always an occupant)"},
		{"domain: check out: no visits selected — tick the people who are leaving", "no visits selected — tick the people who are leaving"},
		{"domain: gate check-in: person npf-person-00000000000000 is not on the roster", "person npf-person-00000000000000 is not on the roster"},
		// A nested chain (%w of %w) loses every link, not just the first.
		{"domain: outcome: domain: dispatch: vehicle id is required", "vehicle id is required"},
		{"store: author: refused — trip_amended would modify site-b's record t1; this post is site-a, and a site modifies only its own site's data (D-87)",
			"trip_amended would modify site-b's record t1; this post is site-a, and a site modifies only its own site's data (D-87)"},
		// Faults are never shown raw — one plain sentence instead.
		{"domain: dispatch: sql: database is closed", faultSentence},
		{"domain: amend person: domain: dump: store: dump projection persons: sql: database is closed", faultSentence},
		{"domain: orientation: entry snapshot: expected 1 visit, got 2", faultSentence},
		{"strconv.Atoi: parsing \"x\": invalid syntax", faultSentence},
		// The fixed strings net/http-era handlers write.
		{"bad form", "That form did not arrive intact. Reload the page and enter it again."},
		// Authored copy passes through untouched.
		{"That form did not arrive intact. Nothing was recorded. Reload the page and enter it again.",
			"That form did not arrive intact. Nothing was recorded. Reload the page and enter it again."},
		{"", ""},
	}
	for _, tc := range cases {
		if got := officerText(tc.in); got != tc.want {
			t.Errorf("officerText(%q)\n got: %q\nwant: %q", tc.in, got, tc.want)
		}
	}
	// The muster reprint's loud failure keeps the ids — they are the point.
	got := officerText("domain: VisitsByID: 2 visit id(s) not found: v-1, v-2")
	if !strings.Contains(got, "v-1, v-2") || internalLeakRe.MatchString(got) {
		t.Errorf("the reprint failure lost its ids or kept its internals: %q", got)
	}
	// Idempotent: a page that maps an already-mapped sentence changes nothing.
	for _, tc := range cases {
		once := officerText(tc.in)
		if twice := officerText(once); twice != once {
			t.Errorf("officerText is not idempotent on %q: %q then %q", tc.in, once, twice)
		}
	}
}

// TestEveryDomainErrorFormatMapsWithoutInternals is the census: every
// "domain: …" error format string in internal/domain, with its verbs
// filled, comes out of the mapper with no package prefix and no section
// sign. A new refusal written the house way is covered the day it lands.
func TestEveryDomainErrorFormatMapsWithoutInternals(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "domain", "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no domain sources found: %v", err)
	}
	litRe := regexp.MustCompile(`(?:fmt\.Errorf|errors\.New)\("((?:domain|store): (?:[^"\\]|\\.)*)"`)
	verbRe := regexp.MustCompile(`%[-+# 0-9.]*[vsqdwT]`)
	n := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range litRe.FindAllStringSubmatch(string(src), -1) {
			n++
			msg := verbRe.ReplaceAllStringFunc(m[1], func(v string) string {
				if strings.HasSuffix(v, "w") {
					return "domain: inner op: the wrapped refusal"
				}
				return "x1"
			})
			got := officerText(msg)
			if internalLeakRe.MatchString(got) {
				t.Errorf("%s: %q maps to %q — still carries internals", filepath.Base(f), m[1], got)
			}
			if got == "" {
				t.Errorf("%s: %q maps to nothing", filepath.Base(f), m[1])
			}
		}
	}
	if n < 100 {
		t.Fatalf("census found only %d domain error formats — the extraction is broken, not the mapper", n)
	}
}

// --- the shared refusal page: the sweep ---

// TestNoRenderedRefusalCarriesAnInternalPrefix drives real refusals through
// the served surface (Root, as cmd/sitectl wires it) and asserts what the
// officer reads: a full page with the top bar, a bold lead, the plain
// sentence — no "domain:" chain, no "§" citation — at the SAME status code
// the handler chose.
func TestNoRenderedRefusalCarriesAnInternalPrefix(t *testing.T) {
	srv, st := newAuthTestServer(t)
	if _, err := domain.CreatePerson(st, "site-a", 1, "Ashby, Bailey", nil, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	cookie := loginCookie(t, srv, testLoginPassword)
	root := srv.Root()

	type kase struct {
		name, method, path, body string
		want                     []string // plain words the officer must read
	}
	cases := []kase{
		{"arrive: unknown trip", "POST", "/trips/arrive",
			url.Values{"time_source": {"default"}, "trip_id": {"site-b-trip-000000000000dead"}}.Encode(),
			[]string{"NOT RECORDED", "trip site-b-trip-000000000000dead is not known at this site yet"}},
		{"dispatch: driver not on the roster", "POST", "/trips/dispatch",
			url.Values{"time_source": {"default"}, "vehicle": {"TRK-14"}, "driver_person_id": {"site-a-person-ffffffffffffffff"},
				"destination_site": {"site-b"}, "deadline_minutes": {"60"}}.Encode(),
			[]string{"NOT RECORDED", "not on the roster"}},
		{"dispatch: no destination", "POST", "/trips/dispatch",
			url.Values{"time_source": {"default"}, "vehicle": {"TRK-14"}}.Encode(),
			[]string{"NOT RECORDED"}},
		{"unsolicited arrival: no occupants", "POST", "/trips/arrive-unsolicited",
			url.Values{"time_source": {"default"}, "vehicle": {"VAN 9"}, "origin_site": {"site-b"}}.Encode(),
			[]string{"NOT RECORDED", "at least one occupant is required"}},
		{"gate legacy body: blank driver", "POST", "/gate/checkin",
			url.Values{"time_source": {"default"}, "vehicle": {"VAN 9"}, "driver_kind": {"new"}, "driver_value": {""}}.Encode(),
			[]string{"NOT RECORDED"}},
		{"person amend: unknown person", "POST", "/person/amend",
			url.Values{"person_id": {"nope"}, "phone": {"555"}}.Encode(),
			[]string{"REFUSED", "is not on the roster — nothing was recorded"}},
		{"garbled form body", "POST", "/trips/dispatch", "a=%zz",
			[]string{"NOT RECORDED", "That form did not arrive intact"}},
		{"reprint of unknown visits", "GET", "/muster/print?visits=site-a-visit-gone1", "",
			[]string{"CANNOT SHOW THAT", "site-a-visit-gone1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mk := func() *http.Request {
				var req *http.Request
				if tc.method == "POST" {
					req = httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
					req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				} else {
					req = httptest.NewRequest("GET", tc.path, nil)
				}
				req.Header.Set("Referer", "http://127.0.0.1:21011/trips")
				req.AddCookie(cookie)
				return req
			}
			bare := httptest.NewRecorder()
			srv.Handler().ServeHTTP(bare, mk())
			if bare.Code < 400 {
				t.Fatalf("fixture defect: %s %s was not refused (%d): %s", tc.method, tc.path, bare.Code, bare.Body.String())
			}
			rec := httptest.NewRecorder()
			root.ServeHTTP(rec, mk())
			if rec.Code != bare.Code {
				t.Errorf("the refusal page changed the status: handler said %d, served %d", bare.Code, rec.Code)
			}
			body := visibleText(rec.Body.String())
			if !strings.Contains(body, `<header class="topbar"`) {
				t.Errorf("served as a bare page with no top bar:\n%s", body)
			}
			if leak := internalLeakRe.FindString(body); leak != "" {
				t.Errorf("an officer reads %q on this refusal:\n%s", leak, body)
			}
			for _, w := range tc.want {
				if !strings.Contains(body, w) {
					t.Errorf("refusal lacks %q:\n%s", w, body)
				}
			}
		})
	}
}

// TestRefusalPageLeavesMachineRoutesAlone: /sync and /debug answer
// programs, and their error bodies are a protocol.
func TestRefusalPageLeavesMachineRoutesAlone(t *testing.T) {
	srv, _ := newAuthTestServer(t)
	srv.Sync = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "domain: peer key refused", http.StatusUnauthorized)
	})
	rec := httptest.NewRecorder()
	srv.Root().ServeHTTP(rec, httptest.NewRequest("GET", "/sync/streams", nil))
	if rec.Code != http.StatusUnauthorized || strings.TrimSpace(rec.Body.String()) != "domain: peer key refused" ||
		!strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
		t.Errorf("a /sync error was rewritten: %d %q %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
}

// --- T-199 / R-080: confirmations from a URL confirm only what happened ---

func TestTimeCorrectConfirmsOnlyATimeCorrection(t *testing.T) {
	srv, st := newAuthTestServer(t)
	if _, err := domain.CreatePerson(st, "site-a", 1, "Ashby, Bailey", nil, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	evs, err := st.Events("site-a", 1, 1, 1)
	if err != nil || len(evs) != 1 {
		t.Fatalf("fixture: no first event: %v", err)
	}
	notACorrection := fmt.Sprintf("site-a.1.%d", evs[0].Seq)
	cookie := loginCookie(t, srv, testLoginPassword)
	// ⟦D23, fix2-fj 2026-10-04⟧ James, 2026-10-04 (FINDINGS-R4 cluster J,
	// R-default "seal every URL-driven notice"): ?recorded= is now read only
	// off a SEALED redirect. Each arm below is reached the way an officer
	// reaches it — through a URL this process sealed — and the same value
	// typed by hand must render no confirmation at all (unsealed, below).
	get := func(key string) string {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", sealNotices("/time/correct?recorded="+url.QueryEscape(key)), nil)
		req.AddCookie(cookie)
		srv.Root().ServeHTTP(rec, req)
		return visibleText(rec.Body.String())
	}
	unsealed := func(key string) string {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/time/correct?recorded="+url.QueryEscape(key), nil)
		req.AddCookie(cookie)
		srv.Root().ServeHTTP(rec, req)
		return visibleText(rec.Body.String())
	}
	for _, key := range []string{notACorrection, "site-a.1.999", "garbage"} {
		body := unsealed(key)
		for _, false_ := range []string{"NOTHING TO CONFIRM", "Recorded", "could not be determined"} {
			if strings.Contains(body, false_) {
				t.Errorf("unsealed ?recorded=%s renders %q — a hand-typed link confirms nothing (D23)", key, false_)
			}
		}
	}
	// A real event of another kind: nothing to confirm, and no claim at all
	// that anything was recorded.
	body := get(notACorrection)
	for _, false_ := range []string{"Recorded and effective", "Recorded, but", "was recorded"} {
		if strings.Contains(body, false_) {
			t.Errorf("?recorded=%s: the page claims %q about a key that names no time correction", notACorrection, false_)
		}
	}
	if !strings.Contains(body, "NOTHING TO CONFIRM.") {
		t.Errorf("?recorded=%s: the page does not say there is nothing to confirm:\n%s", notACorrection, body)
	}
	// A garbled key and a key naming no event stay VF §5 rendering 4 —
	// VF-m5b's locked criterion is not reopened by T-199:
	// TestW4CorrectionConfirmationFailurePathRendersLoudly.
	for _, key := range []string{"site-a.1.999", "garbage"} {
		if body := get(key); !strings.Contains(body, "could not be determined") {
			t.Errorf("?recorded=%s: rendering 4 lost:\n%s", key, body)
		}
	}
}

func TestTradePageConfirmsOnlyATradeChange(t *testing.T) {
	srv, st := newAuthTestServer(t)
	tr, err := domain.CreateTrade(st, "site-a", 1, "Electrician", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	cookie := loginCookie(t, srv, testLoginPassword)
	// ⟦D23, fix2-fj 2026-10-04⟧ James, 2026-10-04 (FINDINGS-R4 cluster J,
	// R-default "seal every URL-driven notice"): ?recorded= is now read only
	// off a SEALED redirect. Each arm below is reached the way an officer
	// reaches it — through a URL this process sealed — and the same value
	// typed by hand must render no confirmation at all (unsealed, below).
	get := func(v string) string {
		req := httptest.NewRequest("GET", sealNotices("/trade?recorded="+url.QueryEscape(v)), nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		srv.Root().ServeHTTP(rec, req)
		return visibleText(rec.Body.String())
	}
	for _, v := range []string{"FAKE-99", "site-a.1.1", tr.TradeID} {
		req := httptest.NewRequest("GET", "/trade?recorded="+url.QueryEscape(v), nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		srv.Root().ServeHTTP(rec, req)
		body := visibleText(rec.Body.String())
		if strings.Contains(body, "NOTHING TO CONFIRM") || strings.Contains(body, `<div class="notice notice-ok">Recorded.</div>`) {
			t.Errorf("unsealed /trade?recorded=%s renders a confirmation — a hand-typed link confirms nothing (D23)", v)
		}
	}
	for _, v := range []string{"FAKE-99", "site-a.1.1", "site-a.1.999"} {
		body := get(v)
		if strings.Contains(body, `<div class="notice notice-ok">Recorded.</div>`) {
			t.Errorf("?recorded=%s: confirmed \"Recorded.\" for something that is not a trade change here", v)
		}
		if !strings.Contains(body, "NOTHING TO CONFIRM.") {
			t.Errorf("?recorded=%s: does not say there is nothing to confirm", v)
		}
	}
	// The create redirect's own value still confirms.
	if body := get(tr.TradeID); !strings.Contains(body, `<div class="notice notice-ok">Recorded.</div>`) {
		t.Errorf("a real new trade's id no longer confirms \"Recorded.\"")
	}
}

// --- S-018 / S-037 ---

func TestLoginNotRecordedNoticeOpensWithItsKind(t *testing.T) {
	srv, _ := newAuthTestServer(t)
	req := httptest.NewRequest("POST", "/trips/dispatch", strings.NewReader("vehicle=T1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.Root().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `<p class="not-recorded" role="alert"><strong>NOT RECORDED.</strong>`) {
		t.Errorf("the not-recorded login notice does not open with a bold kind:\n%s", rec.Body.String())
	}
}

func TestReLoginAfterARefusedPostReturnsToThePageTheOfficerWasOn(t *testing.T) {
	srv, _ := newAuthTestServer(t)
	// The browser's Referer is ABSOLUTE; this used to be refused by safeNext
	// and every re-login landed on /gate.
	req := httptest.NewRequest("POST", "/gate/checkout/vehicle", strings.NewReader("vehicle=T1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "http://127.0.0.1:21011/gate/vehicle")
	rec := httptest.NewRecorder()
	srv.Root().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `name="next" value="/gate/vehicle"`) {
		t.Fatalf("the not-recorded login form does not carry the page the officer was on:\n%s", rec.Body.String())
	}
	form := url.Values{"password": {testLoginPassword}, "next": {"/gate/vehicle"}, "not_recorded": {"1"}}
	login := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	srv.Root().ServeHTTP(rec, login)
	if loc := rec.Header().Get("Location"); loc != "/gate/vehicle" {
		t.Errorf("re-login landed on %q, want /gate/vehicle", loc)
	}
	// A Referer that is not a page (the JS-off vehicle form re-renders at
	// its POST-only accumulate routes) leaves next empty, so the login's
	// own mode choice lands the officer — never on a NO SUCH PAGE.
	req = httptest.NewRequest("POST", "/gate/checkout/vehicle", strings.NewReader("vehicle=T1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "http://127.0.0.1:21012/gate/passenger/add")
	rec = httptest.NewRecorder()
	srv.Root().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `name="next" value=""`) {
		t.Errorf("a POST-only Referer was carried as next — re-login would land on a page that does not exist:\n%s", rec.Body.String())
	}
	form = url.Values{"password": {testLoginPassword}, "next": {""}, "not_recorded": {"1"}}
	login = httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	srv.Root().ServeHTTP(rec, login)
	if loc := rec.Header().Get("Location"); loc != "/gate" {
		t.Errorf("re-login with no next landed on %q, want the mode's page /gate", loc)
	}
	// A hostile Referer reduces to a path, and that path is still judged by
	// safeNext at login: a scheme-relative one lands on /gate, never off-box.
	form = url.Values{"password": {testLoginPassword}, "next": {"//evil.example.com/x"}, "not_recorded": {"1"}}
	login = httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	srv.Root().ServeHTTP(rec, login)
	if loc := rec.Header().Get("Location"); loc != "/gate" {
		t.Errorf("a scheme-relative next from a hostile Referer redirected to %q, want /gate", loc)
	}
}

// --- the amendment screens carry the full top bar (W8's slim-bar item) ---

// TestAmendScreensCarryTheFullTopBar: every correction screen shows the
// post (with Log out) and the alert strip, like the gate and the board — an
// overdue vehicle must not vanish from the top of the screen because the
// officer opened a correction form. No screen carries a weather-phase chip.
//
// ⟦was: "…, the weather phase and the alert strip", run twice — ungoverned
// (chip absent) and with a [[weather.phase]] table added (chip present on
// every amend screen). Weather phases removed, James 2026-10-04: one run,
// chip absent.⟧
func TestAmendScreensCarryTheFullTopBar(t *testing.T) {
	srv, _ := newAuthTestServer(t)
	amendScreensTopBar(t, srv)
}

func amendScreensTopBar(t *testing.T, srv *Server) {
	t.Helper()
	cookie := loginCookie(t, srv, testLoginPassword)
	for _, path := range []string{"/trade", "/person/amend", "/company/amend", "/vehicle/amend", "/time/correct", "/trips/amend"} {
		req := httptest.NewRequest("GET", path, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		srv.Root().ServeHTTP(rec, req)
		body := visibleText(rec.Body.String())
		i := strings.Index(body, `<header class="topbar"`)
		j := strings.Index(body, `</header>`)
		if rec.Code != http.StatusOK || i < 0 || j < i {
			t.Errorf("%s: %d, no top bar", path, rec.Code)
			continue
		}
		bar := body[i:j]
		for _, want := range []string{"Recording as:", "Log out", `class="topbar-alerts"`} {
			if !strings.Contains(bar, want) {
				t.Errorf("%s: the top bar lacks %q — the slim bar is back", path, want)
			}
		}
		if strings.Contains(bar, "weather phase") {
			t.Errorf("%s: a weather-phase chip is in the top bar — weather phases were removed", path)
		}
	}
}
