// auth_safenext_test.go — repair-queue row L (S123).
//
// safeNext (auth.go) is the open-redirect sanitiser on the post-login
// `next` parameter. Until this file existed, DELETING IT ENTIRELY changed
// nothing that any test in this module observed: not one test anywhere fed
// it a malicious value, and — unlike every other known gap in this tree —
// it carried no acknowledgement either, not beside the function and not in
// auth_test.go. A documented gap is debt and can be argued with; an
// undocumented one is risk, because nothing tells anyone to look.
//
// A security guard with no adversarial input is indistinguishable from no
// guard. It passes forever, including after someone breaks it.
//
// ── THE HOSTILE SET IS DERIVED FROM THE IMPLEMENTATION, NOT GUESSED ──
//
// safeNext admits exactly the strings that start with "/" and do not start
// with "//". Both of its call sites hand the result straight to
// http.Redirect, which puts it in a Location header essentially verbatim.
// So the ONE hazard the guard exists to stop is precisely: a Location value
// that a browser resolves against a DIFFERENT ORIGIN than this box. There
// are exactly two syntactic routes to that, and each generates a family:
//
//	(1) an ABSOLUTE URL — a scheme is present, so the authority that follows
//	    it wins outright. Family: http/https, upper/mixed-case schemes,
//	    non-http schemes the browser will still act on (javascript:, data:,
//	    file:), and the credential-prefixed form that makes an attacker host
//	    read like ours ("https://site-a@evil.example.com/").
//	(2) a SCHEME-RELATIVE URL — no scheme, but "//" starts an authority, so
//	    the origin is inherited from nothing and taken from the string.
//	    Family: bare "//host", "//host/path", "///host", credential-prefixed
//	    "//user:pass@host/", and the leading-BACKSLASH spellings ("\\host",
//	    "\/host") that browsers fold onto "//" when parsing.
//
// Plus the two shapes that are hostile in INTENT but stopped by the same
// first-character test — percent-encoded slashes ("%2f%2fhost") and any
// prefix that is not "/" at all (leading whitespace, a leading CR/LF).
//
// ── WHAT IS DELIBERATELY NOT ASSERTED HERE, AND WHY ──
//
// This file does NOT claim safeNext is airtight. Three findings, all
// MEASURED against net/http rather than reasoned about, are recorded in
// reports/window-build/S123-SYSTEMGAPS.md and are NOT asserted here because
// asserting them would either freeze a defect in place or go red:
//
//   - CRLF header injection ("/x\r\nSet-Cookie: …") WAS admitted by
//     safeNext and stopped only one layer down, by net/http's header writer
//     folding CR and LF to spaces (S-032: the officer then landed on a NO
//     SUCH PAGE). FIXED (W4, 2026-10-03): safeNext refuses any control
//     character itself; the cases are in s123HostileNext below.
//     TestLoginRedirectRefusesHeaderInjectionViaNext still proves the wire
//     OUTCOME independently of this guard.
//   - "/\evil.example.com" and "/<TAB>/evil.example.com" WERE ADMITTED by
//     safeNext and DID reach a browser as a cross-origin redirect (a
//     browser folds a leading backslash onto "//" and deletes ASCII
//     tab/CR/LF from anywhere in the string before parsing, so each of
//     these normalized to a scheme-relative authority even though the raw
//     bytes read as one same-origin path). FIXED: safeNext now normalizes
//     the same way before deciding, and both shapes are asserted below as
//     "leading backslash single slash" and "embedded tab before slash" in
//     s123HostileNext, closing what used to be repair-queue row U.
//   - The residue is scoped to the redirect only. Nothing here asserts
//     anything about what the target PAGE renders; that is other tests'
//     ground.
package web

import (
	"bufio"
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
)

// s123HostileNext is the derived adversarial set: every input that must be
// refused, with the family it stands for. Refusal means safeNext returns
// the "/gate" fallback, which is what the two call sites then redirect to.
var s123HostileNext = []struct {
	name string
	in   string
	why  string
}{
	// (2) scheme-relative — the classic protocol-relative open redirect.
	{"scheme relative bare", "//evil.example.com", "browser reads // as the start of an authority"},
	{"scheme relative with path", "//evil.example.com/phish", "the phishing form's own path"},
	{"scheme relative triple slash", "///evil.example.com", "an extra slash must not slip past a two-character prefix test"},
	{"scheme relative with credentials", "//user:pass@evil.example.com/", "credentials make the authority read like a path"},
	// (2) scheme-relative spelled with backslashes. net/http rewrites a
	// leading backslash run into a leading "/" run, so these reach the wire
	// as "/\\…" and a browser folds "\" onto "/" — cross-origin either way.
	{"leading double backslash", `\\evil.example.com`, "browsers fold backslash onto slash when parsing"},
	{"leading backslash slash", `\/evil.example.com`, "mixed spelling of the same authority"},
	// (2) the same fold, but starting from a SINGLE leading "/" so the raw
	// bytes pass the guard's own two-character prefix test. Row U: these
	// were live bypasses until safeNext started normalizing before it
	// decides. "/\host" folds to "//host"; "/<TAB>/host" folds to
	// "//host" once the browser deletes the tab that was holding the two
	// slashes apart.
	{"leading backslash single slash", `/\evil.example.com`, "one leading slash then a backslash still folds to // in a browser"},
	{"embedded tab before slash", "/\t/evil.example.com", "a browser deletes the tab, closing the gap into //"},
	// (1) absolute URLs.
	{"absolute http", "http://evil.example.com/phish", "a scheme wins outright"},
	{"absolute https", "https://evil.example.com/phish", "a scheme wins outright"},
	{"absolute mixed case scheme", "HtTpS://evil.example.com/phish", "schemes are case-insensitive; a case-sensitive check would miss this"},
	{"absolute with embedded credentials", "https://sitecontrol-site-a@evil.example.com/phish", "the userinfo is the part an operator reads as the host"},
	{"absolute with credentials and password", "https://site-a:x@evil.example.com/", "same trick, fuller spelling"},
	// (1) non-http schemes: not "another site" but still not this page.
	{"javascript scheme", "javascript:alert(1)", "a scheme the browser executes rather than fetches"},
	{"data scheme", "data:text/html,<script>alert(1)</script>", "attacker-authored document, no host at all"},
	{"file scheme", "file:///etc/passwd", "a scheme with no host that is still not this app"},
	// Hostile in intent, stopped by the same first-character test.
	{"encoded double slash lowercase", "%2f%2fevil.example.com", "percent-encoded authority prefix"},
	{"encoded double slash uppercase", "%2F%2Fevil.example.com", "case variant of the same"},
	{"leading space then absolute", " https://evil.example.com/phish", "whitespace padding to dodge a prefix test"},
	{"leading CRLF then scheme relative", "\r\n//evil.example.com", "control-character padding to dodge a prefix test"},
	{"bare host no scheme", "evil.example.com", "a browser resolving this relatively is luck, not a guarantee"},
	// S-032 (W4, 2026-10-03): control characters ANYWHERE are refused by the
	// guard itself — it no longer leans on net/http folding CR/LF to spaces
	// (which kept the header safe but landed the officer on a NO SUCH PAGE).
	{"CRLF header injection", "/gate\r\nSet-Cookie: x=1", "a response-splitting attempt; no real path carries a CR or LF"},
	{"bare LF", "/gate\nX-Injected: yes", "same, LF only"},
	{"NUL byte", "/gate\x00", "no real path carries a NUL"},
	{"DEL byte", "/gate\x7f", "no real path carries a DEL"},
	{"relative path", "gate", "no leading slash: whatever it resolves against is not stated here"},
	{"empty", "", "an empty Location is a browser-dependent target, not a stated one"},
}

// s123BenignNext pins the OTHER half of the contract. Without it, "return
// \"/gate\"" — a guard that redirects every successful login to the gate
// and throws the requested destination away — would satisfy every hostile
// case above and silently break the feature safeNext exists to make safe.
var s123BenignNext = []string{
	"/gate",
	"/trips",
	"/muster",
	"/gate/checkin",
	"/trips?site=site-b",
	"/muster?site=site-a&print=1",
	"/trips/detail?trip_id=t-site-a-1",
	// A percent-encoded slash INSIDE an absolute path stays on this origin:
	// a browser does not decode %2f before it parses the authority, so this
	// is a same-origin path with a strange name, not a redirect off-site. It
	// is admitted on purpose and recorded here so the next reader does not
	// mistake its admission for an oversight.
	"/%2f%2fevil.example.com",
}

// TestSafeNextRefusesEveryHostileRedirectTarget is the unit-level half of
// row L: the sanitiser itself, fed the derived adversarial set.
func TestSafeNextRefusesEveryHostileRedirectTarget(t *testing.T) {
	for _, tc := range s123HostileNext {
		t.Run(tc.name, func(t *testing.T) {
			if got := safeNext(tc.in); got != "/gate" {
				t.Errorf("safeNext(%q) = %q, want %q — %s. An open-redirect guard that admits this hands an operator's browser to somebody else's login form immediately after they typed this site's password.", tc.in, got, "/gate", tc.why)
			}
		})
	}
}

// TestSafeNextPreservesLegitimateDestinations is the anti-vacuity half: the
// guard must still carry an operator back to the page they were sent away
// from. A test file holding only the hostile table would be passed by a
// guard that discarded every destination.
func TestSafeNextPreservesLegitimateDestinations(t *testing.T) {
	for _, in := range s123BenignNext {
		if got := safeNext(in); got != in {
			t.Errorf("safeNext(%q) = %q — a same-origin destination was thrown away. The post-login bounce-back is the whole reason `next` exists; a guard that eats it is not safe, it is broken.", in, got)
		}
	}
}

// s123LocationAfter drives one of safeNext's two real call sites and hands
// back the Location header the box actually put on the wire. Asserting the
// header rather than the function's return value is what makes this a test
// of the SEAM: it would still catch a future call site that forgot to call
// safeNext at all, which a unit test on safeNext cannot.
func s123LocationAfter(t *testing.T, srv *Server, method, next string, cookie *http.Cookie) (int, string) {
	t.Helper()
	var req *http.Request
	switch method {
	case http.MethodPost:
		form := url.Values{"password": {testLoginPassword}, "next": {next}}
		req = httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	case http.MethodGet:
		req = httptest.NewRequest("GET", "/login?next="+url.QueryEscape(next), nil)
	default:
		t.Fatalf("unsupported method %q", method)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec.Code, rec.Header().Get("Location")
}

// TestLoginRedirectStaysOnThisOriginUnderHostileNext is row L at the seam
// that matters. Both of safeNext's call sites are exercised:
//
//   - POST /login with the correct password (handleLoginSubmit) — the shape
//     a phisher actually uses, because the victim really does log in and
//     really is redirected;
//   - GET /login carrying a live session (handleLoginPage) — the "you are
//     already in, go where you were going" bounce, which is reachable with
//     nothing but a crafted link.
//
// The assertion is on the LOCATION HEADER, and it is made twice: exact
// equality with the fallback, AND a structural parse showing the value
// names no scheme and no host. The second catches a future fallback that
// changed name; the first catches a fallback that quietly became absolute.
func TestLoginRedirectStaysOnThisOriginUnderHostileNext(t *testing.T) {
	srv, _ := newAuthTestServer(t)
	sessionCookie := loginCookie(t, srv, testLoginPassword)

	for _, tc := range s123HostileNext {
		t.Run(tc.name, func(t *testing.T) {
			for _, call := range []struct {
				what   string
				method string
				cookie *http.Cookie
			}{
				{"POST /login (fresh login)", http.MethodPost, nil},
				{"GET /login (already authenticated)", http.MethodGet, sessionCookie},
			} {
				code, loc := s123LocationAfter(t, srv, call.method, tc.in, call.cookie)
				if code != http.StatusSeeOther {
					t.Fatalf("%s with next=%q: status %d, want 303 — the redirect under test did not happen, so nothing below proves anything", call.what, tc.in, code)
				}
				if loc != "/gate" {
					t.Errorf("%s with next=%q: Location = %q, want %q — %s", call.what, tc.in, loc, "/gate", tc.why)
				}
				u, err := url.Parse(loc)
				if err != nil {
					// An unparseable Location is not a pass. Say so rather
					// than letting the error path read as success.
					t.Errorf("%s with next=%q: Location %q does not parse: %v", call.what, tc.in, loc, err)
					continue
				}
				if u.Scheme != "" || u.Host != "" {
					t.Errorf("%s with next=%q: Location %q names scheme %q host %q — the post-login redirect left this origin", call.what, tc.in, loc, u.Scheme, u.Host)
				}
			}
		})
	}
}

// TestLoginRedirectHonoursASameOriginNext is the seam-level anti-vacuity
// partner: a legitimate `next` still arrives in the Location header intact,
// so the test above cannot be satisfied by a box that redirects everyone to
// /gate unconditionally.
func TestLoginRedirectHonoursASameOriginNext(t *testing.T) {
	srv, _ := newAuthTestServer(t)
	sessionCookie := loginCookie(t, srv, testLoginPassword)

	for _, next := range []string{"/muster", "/trips?site=site-b"} {
		code, loc := s123LocationAfter(t, srv, http.MethodPost, next, nil)
		if code != http.StatusSeeOther || loc != next {
			t.Errorf("POST /login with next=%q: status %d Location %q, want 303 and %q", next, code, loc, next)
		}
		code, loc = s123LocationAfter(t, srv, http.MethodGet, next, sessionCookie)
		if code != http.StatusSeeOther || loc != next {
			t.Errorf("GET /login (authenticated) with next=%q: status %d Location %q, want 303 and %q", next, code, loc, next)
		}
	}
}

// TestLoginRedirectRefusesHeaderInjectionViaNext records the CRLF case
// HONESTLY, and its honesty is the point.
//
// A `next` of "/gate\r\nSet-Cookie: ..." starts with "/" and does not start
// with "//", so safeNext ADMITS IT UNCHANGED. The response is nonetheless
// safe, and the credit belongs entirely to net/http, whose header writer
// folds CR and LF to spaces on the way out.
//
// So: this test PASSES WITH safeNext DELETED. It is not coverage of the
// guard and is not counted as any. It is here because the adversarial set
// for a redirect sanitiser is not complete without CRLF, and the useful
// thing to write down is WHICH LAYER actually holds -- otherwise the next
// person to harden safeNext will believe this line is already defended by
// the function they are editing.
//
// [MEASURED, and it is the reason this test looks the way it does] The
// assertion is on the SERIALISED RESPONSE, not on the recorder's header
// map. httptest.ResponseRecorder stores whatever the handler set, verbatim:
// rec.Header().Get("Location") really does come back holding a raw CRLF,
// and a test that asserted on it would report an injection that never
// reaches a client. net/http's sanitisation happens at WRITE time, so the
// only honest place to look is the bytes.
func TestLoginRedirectRefusesHeaderInjectionViaNext(t *testing.T) {
	srv, _ := newAuthTestServer(t)

	for _, next := range []string{
		"/gate\r\nSet-Cookie: sc_session=forged; Path=/",
		"/gate\nSet-Cookie: sc_session=forged; Path=/",
		"/gate\rX-Injected: yes",
	} {
		form := url.Values{"password": {testLoginPassword}, "next": {next}}
		req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		wire, err := httputil.DumpResponse(rec.Result(), false)
		if err != nil {
			t.Fatalf("next=%q: dumping the response: %v", next, err)
		}
		// Re-read the bytes as a response, exactly as a client would. If the
		// injection worked, the forged header is a real header here.
		parsed, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(wire)), req)
		if err != nil {
			t.Fatalf("next=%q: the emitted response does not parse: %v\nwire:\n%s", next, err, wire)
		}
		parsed.Body.Close()

		if got := parsed.Header.Get("X-Injected"); got != "" {
			t.Errorf("next=%q injected header X-Injected: %q\nwire:\n%s", next, got, wire)
		}
		for _, c := range parsed.Cookies() {
			if c.Value == "forged" {
				t.Errorf("next=%q injected a forged %s cookie\nwire:\n%s", next, c.Name, wire)
			}
		}
		if n := len(parsed.Header.Values("Location")); n != 1 {
			t.Errorf("next=%q produced %d Location headers, want 1\nwire:\n%s", next, n, wire)
		}
		if loc := parsed.Header.Get("Location"); strings.ContainsAny(loc, "\r\n") {
			t.Errorf("next=%q put a raw CR/LF into the Location header on the wire: %q -- that is a response-splitting primitive", next, loc)
		}
	}
}
