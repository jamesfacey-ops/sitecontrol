// theme.go — the light/dark switch (James, 2026-09-29: "a simple switch on
// the top of the page please"). Read _styles.html's "theme switch" comment
// first — the CSS side (three-selector token guard, already applied to
// every page) was doing everything except remembering an explicit choice
// and applying it before first paint. This file is the other two.
//
// WHY A COOKIE AND NOT localStorage: UI_BRIEF.md constraint 4 bans
// localStorage/sessionStorage/IndexedDB outright, and
// TestNoPageUsesClientSideStorage (layout_test.go) bans the literal
// substrings "localStorage", "sessionStorage", "indexedDB" and
// "document.cookie" from ever appearing in a rendered page — so no
// template may reference any of the four, in script or even in a comment.
// A cookie set by http.SetCookie and read back on the next request is a
// different thing: it is never "living only in a tab" (the constraint's
// own stated purpose), because the server sees and controls it on every
// request — the same way sc_session (auth.go) already does. It is
// never read or written by script: no template touches document.cookie, in
// either direction.
//
// WHY NO PRE-PAINT SCRIPT: the usual "localStorage + tiny inline <head>
// script" pattern exists to beat a flash of the wrong theme on a page that
// otherwise ships with no server opinion at all. This app has no such gap
// — the theme is baked into the very first bytes of the response by
// ThemeMiddleware below, before a single byte reaches the browser, so
// there is nothing for a client-side script to race against.
//
// THE CONTROL (2026-09-30, finishing the 09-29 WIP). Two states the user
// can pick — light and dark — plus the implicit third, "auto": no cookie,
// so the page follows the browser's prefers-color-scheme exactly as it did
// before this file existed. The top bar carries TWO links, "Switch to
// dark" (/theme?mode=dark) and "Switch to light" (/theme?mode=light), and
// the stylesheet shows exactly one of them, keyed on the SAME three-way
// guard (bare default / prefers-dark / explicit data-theme) that picks the
// palette — so the link on screen always names the theme the page is NOT
// showing, and the first click always changes something. (The WIP's single
// /theme?mode=toggle link flipped from the COOKIE, so a dark-OS browser's
// first click set dark on an already-dark page: a dead click. Deciding in
// CSS, where the effective theme is actually known, removes it with no
// client hint and no script.) /theme?mode=auto clears the choice; it is not
// on the bar (one quiet control, not three), but it is the documented way
// back to "follow the browser".
//
// JS OFF: the link is a plain GET; handleThemeSet sets the cookie and 303s
// back to the Referer's path, which reloads the same page in the new
// theme. JS ON: a few lines in page_close (_layout.html) intercept the
// click, make the same request with js=1 — answered 204, cookie set, no
// redirect — and flip data-theme on <html> in place, so a half-filled gate
// form is not thrown away by a reload. Any failure falls back to following
// the link. That script touches no storage API and never reads or writes
// a cookie itself; the cookie is only ever written by Set-Cookie.
//
// ACROSS POSTS: config.ThemeCookieDomain (theme_cookie_domain) scopes the
// cookie to a parent domain so one choice applies at every post (the demo,
// where each post is its own subdomain). Unset, the cookie is host-only.
// See themeCookieDomainFor.
package web

import (
	"bytes"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// sc_theme is a separate cookie from sc_session (auth.go) on purpose: this
// is a cosmetic preference with no bearing on accountability or
// attribution, and giving it its own name means it is never at risk of
// being read as, or cleared by, a login/logout.
const themeCookieName = "sc_theme"

// A year, not session-only: unlike sc_session (design §10's deliberate
// no-idle-timeout SESSION cookie), a theme choice is a standing preference
// an operator would be annoyed to re-make every time the browser restarts.
const themeCookieMaxAge = 60 * 60 * 24 * 365

func (s *Server) registerThemeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /theme", s.handleThemeSet)
}

// themeFromRequest reads the current explicit choice, if any. "" means no
// cookie (or a corrupted one) — the ordinary, unmodified case, where
// _styles.html's prefers-color-scheme media query decides alone, exactly
// as it did before this file existed.
func themeFromRequest(r *http.Request) string {
	c, err := r.Cookie(themeCookieName)
	if err != nil {
		return ""
	}
	switch c.Value {
	case "light", "dark":
		return c.Value
	default:
		return ""
	}
}

// handleThemeSet is the switch's one route. GET, not POST: UI brief
// constraint 2 means this must work with scripting off, and a plain
// <a href> is the only zero-JS control that looks and behaves the same in
// a bar that must be identical on every page. It sets no session, authors
// no record, and is deliberately ungated — same reasoning as every other
// GET in this package (see auth.go's header comment on what is and is not
// gated): there is nothing here to be accountable for, and the login page
// carries the switch too.
//
// mode=light|dark sets the choice, mode=auto clears it (back to the
// browser's preference); anything else changes nothing and redirects back.
// js=1 (the page_close script) gets 204 instead of a redirect.
//
// NOT HttpOnly, deliberately, unlike sc_session: the value is a cosmetic
// word, not a credential, and the demo landing page (a static page on the
// same parent domain) must be able to overwrite it — a browser refuses a
// script write over an HttpOnly cookie, which would freeze the landing
// page's switch the moment an app page had set one.
func (s *Server) handleThemeSet(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("mode")
	c := &http.Cookie{
		Name:     themeCookieName,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	}
	switch mode {
	case "light", "dark":
		c.Value = mode
		c.MaxAge = themeCookieMaxAge
	case "auto":
		c.MaxAge = -1 // sent as Max-Age=0: delete, so prefers-color-scheme decides again
	default:
		// Changes nothing and goes back where it came from — a 303 like
		// every other answer here, not a 400 page: a GET that is not a page
		// must redirect to one (TestW2EveryServedTakesInputFormCarriesTheTimeControl
		// walks every GET route and admits only 200 or a redirect to a
		// walked page).
		http.Redirect(w, r, safeNext(refererPath(r)), http.StatusSeeOther)
		return
	}
	if d := s.themeCookieDomainFor(r); d != "" {
		c.Domain = d
		// A host-only sc_theme left over from before the domain was
		// configured would SHADOW the domain-wide one (same name and path;
		// the browser sends the older one first and r.Cookie takes the
		// first), so expire it in the same response.
		http.SetCookie(w, &http.Cookie{Name: themeCookieName, Path: "/", MaxAge: -1, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil})
	}
	http.SetCookie(w, c)
	if r.URL.Query().Get("js") == "1" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// safeNext (auth.go) is pinned to admit only a bare same-origin PATH
	// (a single leading "/", never "//") and to fall back to "/gate"
	// otherwise. r.Referer() is never that shape — a browser always sends
	// a full scheme+host+path URL — so passing it to safeNext unmodified
	// would fail the prefix check on every real call and silently bounce
	// every toggle click to /gate regardless of which page it came from.
	// refererPath below throws the scheme and host away FIRST — safety
	// never depended on trusting the host anyway, since safeNext's own
	// guard is host-blind by design — leaving exactly the shape safeNext
	// expects.
	http.Redirect(w, r, safeNext(refererPath(r)), http.StatusSeeOther)
}

// themeCookieDomainFor returns the Domain attribute for the theme cookie:
// the CONFIGURED theme_cookie_domain, or "" (host-only). The value itself
// only ever comes from config (validated at load, config.go). The request
// is consulted for one thing — whether its host lies inside that domain —
// and only ever to DROP the attribute: a browser silently discards a
// cookie whose Domain does not cover the host that set it, so a post
// reached by IP or by another name would otherwise carry a switch that
// never sticks. Host-only is the honest fallback there.
func (s *Server) themeCookieDomainFor(r *http.Request) string {
	if s.Cfg == nil {
		return ""
	}
	d := s.Cfg.ThemeCookieDomainName()
	if d == "" {
		return ""
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.ToLower(host)
	if host == d || strings.HasSuffix(host, "."+d) {
		return d
	}
	return ""
}

// refererPath reduces the Referer header to a bare path (+ query, so a
// deep view like /trips/detail?trip_id=… still lands back on the SAME
// record, not just the same route), or "" if there is none or it does not
// parse. Discarding the scheme and host here, before safeNext ever sees the
// value, is what makes the redirect land back on the actual page a click
// came from instead of always missing safeNext's leading-"/" check and
// falling back to /gate.
func refererPath(r *http.Request) string {
	ref := r.Referer()
	if ref == "" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	return u.RequestURI()
}

// --- applying the choice: rewrite the one <html> tag the shared shell
// emits, once, before any byte reaches the browser. ---

var (
	htmlOpenTag      = []byte(`<html lang="en">`)
	htmlOpenTagDark  = []byte(`<html lang="en" data-theme="dark">`)
	htmlOpenTagLight = []byte(`<html lang="en" data-theme="light">`)
)

// ThemeMiddleware wraps a handler, once, applying the light/dark choice to
// everything it serves. It is deliberately layered OUTSIDE Server.Handler
// rather than inside it — Handler must keep returning a bare
// *http.ServeMux (several tests, e.g. w9a_web_seams_test.go's
// w9aRouteExists, type-assert its return value to *http.ServeMux to
// interrogate the routing table directly), so wrapping happens one level
// up, at cmd/sitectl/main.go's http.Server{Handler: ...} wiring, which is
// the only place that already sits outside every such assertion.
//
// It does nothing at all unless an explicit cookie is present — the
// ordinary, unmodified render path for the common case (no cookie) is
// completely untouched, byte for byte, by this file, and machine-to-machine
// traffic (/sync/*, /debug/*), which never carries this browser cookie,
// never reaches the wrapping writer at all.
//
// The rewrite targets the ONE literal `<html lang="en">` _layout.html ever
// emits (TestEveryPageRendersTheSharedShellExactlyOnce pins it to appear
// exactly once per page), and is safe against being split across writes
// because renderIntoStatus (web.go, M-15) buffers a WHOLE page and writes
// it in one w.Write call — never streamed, never chunked by this app's own
// code — so the wrapper below always sees the complete tag in one call or
// not at all.
func ThemeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		theme := themeFromRequest(r)
		if theme == "" {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(&themeInjectingWriter{ResponseWriter: w, theme: theme}, r)
	})
}

// themeInjectingWriter only overrides Write: Header/WriteHeader are
// promoted straight through by the embedded http.ResponseWriter, and
// nothing in this package ever asserts a ResponseWriter to http.Flusher
// or http.Hijacker (measured: neither identifier appears anywhere in this
// module), so there is no streaming/upgrade contract this could break.
type themeInjectingWriter struct {
	http.ResponseWriter
	theme string
}

func (t *themeInjectingWriter) Write(p []byte) (int, error) {
	if !bytes.Contains(p, htmlOpenTag) {
		// A non-page response (a redirect's small "<a href=…>" body,
		// /sync's JSON, /debug/sync-status) carries no <html> tag at all —
		// pass it through unexamined rather than rewritten.
		return t.ResponseWriter.Write(p)
	}
	replacement := htmlOpenTagLight
	if t.theme == "dark" {
		replacement = htmlOpenTagDark
	}
	rewritten := bytes.Replace(p, htmlOpenTag, replacement, 1)
	if _, err := t.ResponseWriter.Write(rewritten); err != nil {
		return 0, err
	}
	// Report the ORIGINAL length as fully written, per io.Writer's
	// contract (n < len(p) must mean a non-nil error): every byte the
	// caller handed us was accepted, just folded into a longer
	// data-theme-carrying page, so returning the rewritten length here
	// would read to the caller as a short write that never happened.
	return len(p), nil
}
