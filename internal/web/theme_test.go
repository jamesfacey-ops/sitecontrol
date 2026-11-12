// theme_test.go — the light/dark switch's done-when tests (theme.go,
// _topbar.html, _layout.html, _styles.html). In order: is the control on
// every page this app serves (a source-driven sweep, so a new GET route is
// swept without anyone remembering to add it); does a JS-off click come
// back to the SAME page in the chosen theme; does the JS-on request get a
// 204 instead; are the cookie's attributes right (host-only by default,
// domain-wide only when configured and only for a host inside it); is the
// no-cookie render untouched; does print stay light whatever is chosen; do
// the token pairs meet WCAG AA in both themes; and does no template use a
// client-side storage API.
package web

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The control, as rendered: one wrapper and exactly one link of each kind.
// The stylesheet shows one of the two (TestThemeSwitchShowsOnlyTheOtherTheme).
const (
	themeSwitchWrapper   = `<div class="theme-switch">`
	themeSwitchDarkLink  = `<a class="theme-switch-to-dark" href="/theme?mode=dark" data-theme-set="dark">Switch to dark</a>`
	themeSwitchLightLink = `<a class="theme-switch-to-light" href="/theme?mode=light" data-theme-set="light">Switch to light</a>`
	themeScriptOpen      = `<script id="theme-script">`
)

func assertThemeSwitchOnce(t *testing.T, where, body string) {
	t.Helper()
	for _, m := range []string{themeSwitchWrapper, themeSwitchDarkLink, themeSwitchLightLink, themeScriptOpen} {
		if got := strings.Count(body, m); got != 1 {
			t.Errorf("%s: %q appears %d times, want exactly 1", where, m, got)
		}
	}
	// Inside the top bar, not merely somewhere on the page.
	hs, he := strings.Index(body, `<header class="topbar">`), strings.Index(body, `</header>`)
	if w := strings.Index(body, themeSwitchWrapper); hs < 0 || he < 0 || w < hs || w > he {
		t.Errorf("%s: the theme switch is not inside the top bar <header>", where)
	}
}

// themeSweepServer is the gate fixture with orientation switched on, so the
// orientation route (registered only when the post administers one) is in
// the swept set too.
func themeSweepServer(t *testing.T) (*Server, *http.Cookie) {
	t.Helper()
	srv, _, _ := newGateTestServer(t)
	on := true
	srv.Cfg.Orientation = &on
	return srv, loginCookie(t, srv, testLoginPassword)
}

// servedGETRoutes reads every `mux.Handle[Func]("GET …"` registration out of
// this package's own non-test source, resolving the two constant-built
// paths. Reading SOURCE (not a hand list) is the point: a page added next
// month is swept without anyone remembering this test.
func servedGETRoutes(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`mux\.Handle(?:Func)?\("GET ([^"]*)"(\+(\w+))?`)
	consts := map[string]string{"resolvePath": resolvePath, "unrefPath": unrefPath}
	var out []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			p := m[1]
			if m[3] != "" {
				c, ok := consts[m[3]]
				if !ok {
					t.Fatalf("%s: GET route built from unknown constant %s — add it to servedGETRoutes", f, m[3])
				}
				p += c
			}
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

func TestThemeSwitchIsInTheTopBarOfEveryServedPage(t *testing.T) {
	srv, sess := themeSweepServer(t)

	// GET patterns that are not pages, with what they answer instead. Each
	// must actually be registered (checked below), so this list cannot
	// quietly excuse a route that has become a page.
	notPages := map[string]int{
		"/{$}":   http.StatusFound,      // redirect to the front door
		"/":      http.StatusFound,      // bare "/" is the exact root; the catch-all is swept below as an unregistered path
		"/login": http.StatusSeeOther,   // logged in: redirect onward; swept logged-out below
		"/theme": http.StatusSeeOther,   // the switch's own route, no mode: back, unchanged
		// FIX16: a CSV download, not a page; with no search it refuses 400.
		"/search.csv": http.StatusBadRequest,
		// FIX16a: the muster-for-a-moment's CSV download; with no moment it refuses 400.
		"/muster/at.csv": http.StatusBadRequest,
		// FIX16f: the history downloads. This sweep's store registers neither
		// the deadline projections nor the signal table, so two refuse
		// (history.go csvRefused); the sign-outs file is a 200 text/csv.
		"/history/overdue.csv":  http.StatusInternalServerError,
		"/history/signouts.csv": http.StatusOK,
		"/history/signal.csv":   http.StatusNotFound,
		// FIX17d: the trips-detail download; a 200 text/csv for a logged-in viewer.
		"/trips/detail.csv": http.StatusOK,
	}
	routes := servedGETRoutes(t)
	seen := map[string]bool{}
	pages := 0
	for _, p := range routes {
		seen[p] = true
		// James, 2026-10-03: /analytics/site/{site} is the first GET page with
		// a path wildcard. It is swept as a real page by filling the wildcard
		// with this post's own site id, which the detail view always lists —
		// so the page is checked, never excused.
		path := strings.ReplaceAll(strings.TrimSuffix(p, "{$}"), "{site}", srv.Cfg.SiteID)
		// FIX16: /reports/{name} — the visits report page (its .csv twin is a
		// download, covered by fix16b_csv_test.go).
		path = strings.ReplaceAll(path, "{name}", "visits")
		if strings.Contains(path, "{") {
			t.Fatalf("GET %s has a path wildcard this sweep does not know how to fill — add it here", p)
		}
		req := httptest.NewRequest("GET", path, nil)
		req.AddCookie(sess)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if want, ok := notPages[p]; ok {
			if rec.Code != want {
				t.Errorf("GET %s = %d, want %d (listed as not-a-page)", p, rec.Code, want)
			}
			continue
		}
		if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
			t.Errorf("GET %s = %d %s, want a 200 HTML page", p, rec.Code, rec.Header().Get("Content-Type"))
			continue
		}
		pages++
		assertThemeSwitchOnce(t, "GET "+p, rec.Body.String())
	}
	for p := range notPages {
		if !seen[p] {
			t.Errorf("not-a-page entry %q is not a registered GET route any more — remove it", p)
		}
	}
	// Precondition: the sweep really reached the app's pages. 20 measured
	// on 2026-09-30 (analytics, company/vehicle/person/trip amend, gate,
	// gate/vehicle, roster, muster, muster/print, orientation, resolve,
	// unref, time/correct, trade, trips, trips/detail, status, sync/,
	// debug/sync-status — the last two fall through to status with no
	// replication engine wired in this fixture).
	if pages < 20 {
		t.Fatalf("swept only %d pages from %d GET routes %v — the source scan or the fixture has stopped reaching the app", pages, len(routes), routes)
	}

	// Pages the route list alone does not reach.
	extra := []struct {
		name   string
		req    *http.Request
		authed bool
		want   int
	}{
		{"login page, logged out", httptest.NewRequest("GET", "/login", nil), false, http.StatusOK},
		{"unregistered path (status fall-through)", httptest.NewRequest("GET", "/no-such-page", nil), true, http.StatusOK},
		{"trip amend, unknown trip", httptest.NewRequest("GET", "/trips/amend?trip_id=no-such-trip", nil), true, 0},
		{"vehicle amend, unknown vehicle", httptest.NewRequest("GET", "/vehicle/amend?vehicle_id=no-such-vehicle", nil), true, 0},
		{"orientation refusal (nobody ticked)", postReq("/orientation/complete", url.Values{}), true, 0},
		{"gate check-in refusal (empty form)", postReq("/gate/checkin", url.Values{}), true, 0},
	}
	for _, x := range extra {
		if x.authed {
			x.req.AddCookie(sess)
		}
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, x.req)
		if x.want != 0 && rec.Code != x.want {
			t.Errorf("%s: status %d, want %d", x.name, rec.Code, x.want)
		}
		if !strings.Contains(rec.Body.String(), "<!doctype html>") {
			t.Errorf("%s: status %d, not a page shell — body %.200q", x.name, rec.Code, rec.Body.String())
			continue
		}
		assertThemeSwitchOnce(t, x.name, rec.Body.String())
	}
}

func postReq(path string, form url.Values) *http.Request {
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

// The JS-off path end to end, as a browser does it: take the link's href
// FROM THE PAGE, request it with the page as Referer, follow the 303 with
// the cookie it set, and land on the same page (same path AND query) now
// carrying data-theme — then switch back.
func TestThemeSwitchWithScriptingOffComesBackToTheSamePage(t *testing.T) {
	srv, sess := themeSweepServer(t)
	h := ThemeMiddleware(srv.Handler())
	hrefRe := regexp.MustCompile(`<a class="theme-switch-to-(dark|light)" href="([^"]+)"`)

	get := func(path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req.AddCookie(sess)
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	titleOf := func(body string) string {
		m := regexp.MustCompile(`<title>([^<]*)</title>`).FindStringSubmatch(body)
		if m == nil {
			return ""
		}
		return m[1]
	}

	for _, page := range []string{"/gate", "/trips/detail?trip_id=T-1", "/muster", "/status", "/orientation"} {
		before := get(page)
		if before.Code != http.StatusOK {
			t.Fatalf("%s: %d", page, before.Code)
		}
		hrefs := map[string]string{}
		for _, m := range hrefRe.FindAllStringSubmatch(before.Body.String(), -1) {
			hrefs[m[1]] = m[2]
		}
		var theme *http.Cookie
		for _, mode := range []string{"dark", "light"} {
			req := httptest.NewRequest("GET", hrefs[mode], nil)
			req.Header.Set("Referer", "http://sc-npf.example.invalid"+page)
			if theme != nil {
				req.AddCookie(theme)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != page {
				t.Fatalf("%s: click %q = %d Location %q, want 303 back to %q", page, hrefs[mode], rec.Code, rec.Header().Get("Location"), page)
			}
			cs := rec.Result().Cookies()
			if len(cs) != 1 || cs[0].Name != themeCookieName || cs[0].Value != mode {
				t.Fatalf("%s: click %s set %+v, want one %s=%s", page, mode, cs, themeCookieName, mode)
			}
			theme = cs[0]
			after := get(page, theme)
			body := after.Body.String()
			if after.Code != http.StatusOK || titleOf(body) != titleOf(before.Body.String()) {
				t.Errorf("%s after %s: %d %q, want the same page (%q)", page, mode, after.Code, titleOf(body), titleOf(before.Body.String()))
			}
			if !strings.Contains(body, `<html lang="en" data-theme="`+mode+`">`) {
				t.Errorf("%s after %s: page does not carry data-theme=%q", page, mode, mode)
			}
			assertThemeSwitchOnce(t, page+" after "+mode, body)
		}
	}
}

// The JS-on path: same request plus js=1 gets 204, the cookie, and no
// redirect — the page_close script then flips data-theme in place.
func TestThemeSwitchWithScriptingOnGets204NotARedirect(t *testing.T) {
	srv := newTestServer(t)
	for _, mode := range []string{"dark", "light"} {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/theme?mode="+mode+"&js=1", nil))
		if rec.Code != http.StatusNoContent || rec.Header().Get("Location") != "" {
			t.Errorf("js=1 mode=%s: %d Location=%q, want 204 and no Location", mode, rec.Code, rec.Header().Get("Location"))
		}
		if cs := rec.Result().Cookies(); len(cs) != 1 || cs[0].Value != mode {
			t.Errorf("js=1 mode=%s: cookies %+v, want %s=%s", mode, cs, themeCookieName, mode)
		}
	}
	// And the script asks for exactly that: it appends "&js=1" to the
	// link's own href and sets data-theme from data-theme-set.
	body := rawPage(t, srv, "/status")
	i := strings.Index(body, themeScriptOpen)
	if i < 0 {
		t.Fatal("no theme script on /status")
	}
	script := body[i : i+strings.Index(body[i:], "</script>")]
	for _, want := range []string{`a.href + "&js=1"`, `r.status !== 204`, `setAttribute("data-theme", mode)`, `getAttribute("data-theme-set")`, `window.location.href = a.href`} {
		if !strings.Contains(script, want) {
			t.Errorf("theme script lacks %q", want)
		}
	}
}

func TestThemeCookieAttributes(t *testing.T) {
	set := func(srv *Server, target, host string, tls bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", target, nil)
		if host != "" {
			req.Host = host
		}
		if !tls {
			req.TLS = nil
		}
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}

	t.Run("host-only by default", func(t *testing.T) {
		srv := newTestServer(t)
		if srv.Cfg.ThemeCookieDomain != "" {
			t.Fatalf("precondition: fixture has theme_cookie_domain %q set", srv.Cfg.ThemeCookieDomain)
		}
		rec := set(srv, "/theme?mode=dark", "sc-npf.jnkfacey.com", false)
		cs := rec.Result().Cookies()
		if len(cs) != 1 {
			t.Fatalf("want one Set-Cookie, got %v", rec.Header()["Set-Cookie"])
		}
		c, raw := cs[0], rec.Header().Get("Set-Cookie")
		if c.Domain != "" || strings.Contains(strings.ToLower(raw), "domain=") {
			t.Errorf("default cookie carries a Domain: %q", raw)
		}
		if c.Path != "/" || c.SameSite != http.SameSiteLaxMode || c.MaxAge != 365*24*3600 || c.Secure {
			t.Errorf("default cookie = %q, want Path=/ SameSite=Lax Max-Age=1y, not Secure over plain http", raw)
		}
		if c.HttpOnly {
			t.Errorf("theme cookie is HttpOnly — the landing page's switch could then never overwrite it (theme.go)")
		}
	})

	t.Run("Secure when the request came over TLS", func(t *testing.T) {
		srv := newTestServer(t)
		req := httptest.NewRequest("GET", "https://sc-npf.jnkfacey.com/theme?mode=dark", nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if req.TLS == nil {
			t.Fatal("precondition: httptest did not mark an https:// target as TLS")
		}
		if cs := rec.Result().Cookies(); len(cs) != 1 || !cs[0].Secure {
			t.Errorf("TLS request: %v, want one Secure cookie", rec.Header()["Set-Cookie"])
		}
	})

	t.Run("domain-wide when configured, for a host inside it", func(t *testing.T) {
		srv := newTestServer(t)
		srv.Cfg.ThemeCookieDomain = ".jnkfacey.com"
		for _, host := range []string{"sc-npf.jnkfacey.com", "SC-NPF.jnkfacey.com:443", "jnkfacey.com"} {
			rec := set(srv, "/theme?mode=dark", host, false)
			var domainWide, hostOnlyExpiry int
			for _, c := range rec.Result().Cookies() {
				switch {
				case c.Domain == "jnkfacey.com" && c.Value == "dark" && c.MaxAge > 0:
					domainWide++
				case c.Domain == "" && c.MaxAge < 0:
					hostOnlyExpiry++
				default:
					t.Errorf("host %s: unexpected cookie %+v", host, c)
				}
			}
			if domainWide != 1 || hostOnlyExpiry != 1 {
				t.Errorf("host %s: Set-Cookie %v — want one Domain=jnkfacey.com cookie AND one expiry of a shadowing host-only one", host, rec.Header()["Set-Cookie"])
			}
		}
	})

	t.Run("host-only for a host outside the configured domain", func(t *testing.T) {
		srv := newTestServer(t)
		srv.Cfg.ThemeCookieDomain = ".jnkfacey.com"
		for _, host := range []string{"10.0.0.150:8083", "evil-jnkfacey.com", "jnkfacey.com.evil.example", "localhost"} {
			rec := set(srv, "/theme?mode=dark", host, false)
			cs := rec.Result().Cookies()
			if len(cs) != 1 || cs[0].Domain != "" {
				t.Errorf("host %s: %v, want exactly one host-only cookie (a browser would drop Domain=jnkfacey.com here)", host, rec.Header()["Set-Cookie"])
			}
		}
	})

	t.Run("the domain is never taken from the request", func(t *testing.T) {
		srv := newTestServer(t)
		req := httptest.NewRequest("GET", "/theme?mode=dark&domain=evil.example", nil)
		req.Host = "sc-npf.jnkfacey.com"
		req.Header.Set("X-Forwarded-Host", "evil.example")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if raw := strings.ToLower(strings.Join(rec.Header()["Set-Cookie"], "\n")); strings.Contains(raw, "domain=") {
			t.Errorf("unconfigured server set a Domain from request input: %q", raw)
		}
	})

	t.Run("auto clears the choice, junk changes nothing", func(t *testing.T) {
		srv := newTestServer(t)
		rec := set(srv, "/theme?mode=auto", "", false)
		if cs := rec.Result().Cookies(); rec.Code != http.StatusSeeOther || len(cs) != 1 || cs[0].MaxAge >= 0 {
			t.Errorf("mode=auto: %d %v, want 303 and one expiring cookie", rec.Code, rec.Header()["Set-Cookie"])
		}
		for _, mode := range []string{"", "toggle", "purple", "Dark"} {
			rec := set(srv, "/theme?mode="+url.QueryEscape(mode), "", false)
			if rec.Code != http.StatusSeeOther || len(rec.Result().Cookies()) != 0 {
				t.Errorf("mode=%q: %d %v, want 303 back and no cookie", mode, rec.Code, rec.Header()["Set-Cookie"])
			}
		}
	})
}

// The redirect lands on the page the click came from (path + query), and a
// hostile Referer can never make it leave this origin.
func TestThemeSwitchRedirectsBackToTheReferringPage(t *testing.T) {
	srv := newTestServer(t)
	cases := []struct{ referer, want string }{
		{"http://example.invalid/trips", "/trips"},
		{"http://example.invalid/trips/detail?trip_id=T-1", "/trips/detail?trip_id=T-1"},
		{"", "/gate"},
		{"http://example.invalid//evil.example/x", "/gate"},
		{`http://example.invalid/\evil.example`, "/%5Cevil.example"}, // escaped: a same-origin path, not //evil.example
	}
	for _, tc := range cases {
		req := httptest.NewRequest("GET", "/theme?mode=dark", nil)
		if tc.referer != "" {
			req.Header.Set("Referer", tc.referer)
		}
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if got := rec.Header().Get("Location"); got != tc.want {
			t.Errorf("Referer=%q: Location = %q, want %q", tc.referer, got, tc.want)
		}
	}
}

// --- the middleware: no cookie = byte-for-byte untouched ---

func TestThemeMiddlewareIsAByteForByteNoOpWithoutACookie(t *testing.T) {
	srv := newTestServer(t)
	raw := httptest.NewRecorder()
	srv.Handler().ServeHTTP(raw, httptest.NewRequest("GET", "/status", nil))
	wrapped := httptest.NewRecorder()
	ThemeMiddleware(srv.Handler()).ServeHTTP(wrapped, httptest.NewRequest("GET", "/status", nil))
	if raw.Body.String() != wrapped.Body.String() {
		t.Error("ThemeMiddleware changed the page when no theme cookie was present")
	}
	if got := strings.Count(wrapped.Body.String(), `<html lang="en">`); got != 1 {
		t.Errorf("no cookie: plain <html lang=\"en\"> appears %d times, want 1 (no data-theme invented)", got)
	}
}

func TestThemeMiddlewareAppliesAnExplicitCookieChoice(t *testing.T) {
	for cookie, wantTag := range map[string]string{
		"dark":   `<html lang="en" data-theme="dark">`,
		"light":  `<html lang="en" data-theme="light">`,
		"purple": `<html lang="en">`,
	} {
		srv := newTestServer(t)
		req := httptest.NewRequest("GET", "/status", nil)
		req.AddCookie(&http.Cookie{Name: themeCookieName, Value: cookie})
		rec := httptest.NewRecorder()
		ThemeMiddleware(srv.Handler()).ServeHTTP(rec, req)
		if got := strings.Count(rec.Body.String(), wantTag); got != 1 {
			t.Errorf("cookie=%q: %q appears %d times, want 1", cookie, wantTag, got)
		}
		assertThemeSwitchOnce(t, "cookie="+cookie, rec.Body.String())
	}
}

func TestThemeMiddlewareNeverTouchesANonPageResponse(t *testing.T) {
	srv := newTestServer(t)
	newReq := func() *http.Request {
		req := httptest.NewRequest("GET", "/theme?mode=dark", nil)
		req.AddCookie(&http.Cookie{Name: themeCookieName, Value: "dark"})
		return req
	}
	raw := httptest.NewRecorder()
	srv.Handler().ServeHTTP(raw, newReq())
	wrapped := httptest.NewRecorder()
	ThemeMiddleware(srv.Handler()).ServeHTTP(wrapped, newReq())
	if wrapped.Code != http.StatusSeeOther || raw.Body.String() != wrapped.Body.String() || raw.Header().Get("Location") != wrapped.Header().Get("Location") {
		t.Errorf("a redirect was altered by ThemeMiddleware: raw=%d %q wrapped=%d %q", raw.Code, raw.Body.String(), wrapped.Code, wrapped.Body.String())
	}
}

// --- the stylesheet ---

// styleSheet is the one inlined stylesheet as rendered.
func styleSheet(t *testing.T) string {
	t.Helper()
	body := rawPage(t, newTestServer(t), "/status")
	m := regexp.MustCompile(`(?s)<style>(.*?)</style>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no <style> on /status")
	}
	return m[1]
}

// cssBlock returns the declarations of the first rule whose selector text
// is exactly sel, starting the search at from.
func cssBlock(t *testing.T, css, sel string, from int) (string, int) {
	t.Helper()
	i := strings.Index(css[from:], sel+" {")
	if i < 0 {
		t.Fatalf("stylesheet has no %q rule after offset %d", sel, from)
	}
	i += from
	j := strings.Index(css[i:], "}")
	return css[i : i+j], i
}

func tokens(block string) map[string]string {
	out := map[string]string{}
	for _, m := range regexp.MustCompile(`--([\w-]+):\s*(#[0-9A-Fa-f]{6})`).FindAllStringSubmatch(block, -1) {
		out[m[1]] = strings.ToUpper(m[2])
	}
	return out
}

// Which link shows is decided in CSS by the same guard as the palette: the
// "to light" link is hidden by default, and each dark rule hides "to dark"
// and shows "to light". Chrome and Firefox rendering of this is measured
// by the Playwright pass; this pins the rules so an edit cannot drop one.
func TestThemeSwitchShowsOnlyTheOtherTheme(t *testing.T) {
	css := styleSheet(t)
	for _, rule := range []string{
		`.theme-switch .theme-switch-to-light { display: none; }`,
		`:root:not([data-theme="light"]) .theme-switch .theme-switch-to-dark { display: none; }`,
		`:root:not([data-theme="light"]) .theme-switch .theme-switch-to-light { display: inline-flex; }`,
		`:root[data-theme="dark"] .theme-switch .theme-switch-to-dark { display: none; }`,
		`:root[data-theme="dark"] .theme-switch .theme-switch-to-light { display: inline-flex; }`,
	} {
		if strings.Count(css, rule) != 1 {
			t.Errorf("stylesheet lacks (exactly once) %q", rule)
		}
	}
	media := strings.Index(css, "@media (prefers-color-scheme: dark) {\n  :root:not([data-theme=\"light\"]) .theme-switch")
	if media < 0 {
		t.Error(`the prefers-dark switch rules are not inside a prefers-color-scheme: dark block`)
	}
}

// Print is light whatever is chosen: the print block re-declares EVERY
// token either dark block sets, under selectors covering all three ways a
// page can be dark, and comes after them (equal specificity, so later
// wins). A token added to the dark palette but not to print would leak
// dark onto paper — that is what the completeness check catches.
func TestPrintIsLightWhateverThemeIsChosen(t *testing.T) {
	css := styleSheet(t)
	darkMedia, dmAt := cssBlock(t, css, `:root:not([data-theme="light"])`, strings.Index(css, "@media (prefers-color-scheme: dark)"))
	darkExplicit, deAt := cssBlock(t, css, `:root[data-theme="dark"]`, 0)
	printAt := strings.Index(css, "@media print {")
	if printAt < 0 || printAt < dmAt || printAt < deAt {
		t.Fatalf("the print block must come after both dark token blocks (print %d, dark %d/%d)", printAt, dmAt, deAt)
	}
	sel := "@media print {\n  :root,\n  :root:not([data-theme=\"light\"]),\n  :root[data-theme=\"dark\"],\n  :root[data-theme=\"light\"]"
	if !strings.HasPrefix(css[printAt:], sel) {
		t.Fatalf("print token rule does not cover all of :root / prefers-dark / data-theme=dark / data-theme=light")
	}
	pb := css[printAt:]
	pb = pb[:strings.Index(pb, "}")]
	printed := tokens(pb)
	for _, dark := range []map[string]string{tokens(darkMedia), tokens(darkExplicit)} {
		if len(dark) < 10 {
			t.Fatalf("precondition: parsed only %d dark tokens", len(dark))
		}
		for k := range dark {
			if _, ok := printed[k]; !ok {
				t.Errorf("--%s is set by a dark block but not re-declared for print — it would print dark", k)
			}
		}
	}
	if printed["ground"] != "#FFFFFF" || printed["text"] != "#000000" || !strings.Contains(pb, "color-scheme: light") {
		t.Errorf("print tokens are not black on white with color-scheme light: %v", printed)
	}
	// And the rendered print sheet under a dark cookie still carries it.
	srv, _ := newMusterTestServer(t, 0)
	req := httptest.NewRequest("GET", "/muster/print", nil)
	req.AddCookie(&http.Cookie{Name: themeCookieName, Value: "dark"})
	rec := httptest.NewRecorder()
	ThemeMiddleware(srv.Handler()).ServeHTTP(rec, req)
	if b := rec.Body.String(); !strings.Contains(b, `data-theme="dark"`) || !strings.Contains(b, sel) {
		t.Errorf("/muster/print with a dark cookie: missing data-theme=dark or the print override (%d)", rec.Code)
	}
}

func relLum(hex string) float64 {
	v, _ := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	ch := func(c uint64) float64 {
		s := float64(c) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(v>>16&0xFF) + 0.7152*ch(v>>8&0xFF) + 0.0722*ch(v&0xFF)
}

func contrast(a, b string) float64 {
	la, lb := relLum(a), relLum(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// WCAG AA on the token pairs the components actually use, in BOTH themes:
// every text colour (body, muted, accent, and the three status colours)
// on every ground it sits on — 4.5:1 — and the control boundary on every
// ground a control sits on — 3:1 (1.4.11). The status colours are also
// never the only signal (stripes/labels — TestTheAlertHuesStaySeparableWithoutColourVision), but
// their text must still be readable.
func TestThemeTokenPairsMeetWCAGAA(t *testing.T) {
	css := styleSheet(t)
	light, _ := cssBlock(t, css, ":root", 0)
	darkMedia, _ := cssBlock(t, css, `:root:not([data-theme="light"])`, strings.Index(css, "@media (prefers-color-scheme: dark)"))
	darkExplicit, _ := cssBlock(t, css, `:root[data-theme="dark"]`, 0)
	grounds := []string{"ground", "surface", "surface-2", "critical-bg", "warning-bg", "ok-bg", "ice-bg"}
	for name, tok := range map[string]map[string]string{"light": tokens(light), "dark (prefers)": tokens(darkMedia), "dark (explicit)": tokens(darkExplicit)} {
		get := func(k string) string {
			v, ok := tok[k]
			if !ok {
				t.Fatalf("%s: token --%s not defined", name, k)
			}
			return v
		}
		for _, fg := range []string{"text", "text-muted", "ice", "critical", "warning", "ok"} {
			for _, bg := range grounds {
				if c := contrast(get(fg), get(bg)); c < 4.5 {
					t.Errorf("%s: --%s on --%s = %.2f:1, want >= 4.5", name, fg, bg, c)
				}
			}
		}
		if c := contrast(get("on-ice"), get("ice")); c < 4.5 {
			t.Errorf("%s: --on-ice on --ice = %.2f:1, want >= 4.5", name, c)
		}
		for _, bg := range grounds {
			if c := contrast(get("control-line"), get(bg)); c < 3 {
				t.Errorf("%s: --control-line against --%s = %.2f:1, want >= 3 (a field's edge must be findable)", name, bg, c)
			}
		}
	}
	if tokens(darkMedia)["control-line"] != tokens(darkExplicit)["control-line"] {
		t.Error("the two dark blocks disagree on --control-line")
	}
	// Controls use it: inputs, buttons and the switch.
	for _, sel := range []string{"input, select, textarea {", "button, .button {", ".theme-switch a {"} {
		i := strings.Index(css, sel)
		if i < 0 || !strings.Contains(css[i:i+strings.Index(css[i:], "}")], "border: 1px solid var(--control-line)") {
			t.Errorf("%q does not draw its border with --control-line", sel)
		}
	}
}

// UI brief constraint 4, on the SOURCE — every template file, comments
// included — as well as TestNoPageUsesClientSideStorage's rendered pages,
// case-insensitively, plus the theme script specifically.
func TestNoTemplateUsesAStorageAPI(t *testing.T) {
	files, err := filepath.Glob("templates/*.html")
	if err != nil || len(files) < 20 {
		t.Fatalf("precondition: found %d templates (%v)", len(files), err)
	}
	banned := []string{"localstorage", "sessionstorage", "indexeddb", "document.cookie", "cookiestore", "caches.open"}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		low := strings.ToLower(string(b))
		for _, w := range banned {
			if strings.Contains(low, w) {
				t.Errorf("%s mentions %q — no client-side storage (UI brief constraint 4)", f, w)
			}
		}
	}
	srv, sess := themeSweepServer(t)
	for _, p := range []string{"/gate", "/gate/vehicle", "/trips", "/muster", "/status", "/orientation"} {
		req := httptest.NewRequest("GET", p, nil)
		req.AddCookie(sess)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		low := strings.ToLower(rec.Body.String())
		for _, w := range banned {
			if strings.Contains(low, w) {
				t.Errorf("GET %s renders %q", p, w)
			}
		}
	}
}

// The switch adds no request to page load: no src/href to another resource
// (the round-trip budget, UI brief constraint 3) — its links are clicks.
func TestThemeSwitchAddsNoAssetRequest(t *testing.T) {
	body := rawPage(t, newTestServer(t), "/status")
	i := strings.Index(body, themeScriptOpen)
	if i < 0 {
		t.Fatal("no theme script")
	}
	if strings.Contains(body[i:i+strings.Index(body[i:], "</script>")], "src=") {
		t.Error("the theme script loads something")
	}
	w := strings.Index(body, themeSwitchWrapper)
	sw := body[w : w+strings.Index(body[w:], "</div>")]
	if strings.Contains(sw, "<img") || strings.Contains(sw, "src=") || strings.Contains(sw, "url(") {
		t.Error(fmt.Sprintf("the switch markup references an asset: %q", sw))
	}
}
