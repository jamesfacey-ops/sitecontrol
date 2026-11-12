// auth.go — C11's site login, and the post attribution every authored
// record carries (chunk-plan C11, design §12/§10, finding F4, ruling D-5).
//
// THREE LAYERS, NONE CONFUSED (design §12's own framing — read it before
// touching this file):
//   - Site login (this file): one shared per-site credential from config
//     (config.Config.LoginPassword), gating "is this browser allowed to use
//     this site at all." Purpose is ACCOUNTABILITY, not confidentiality
//     (§12) — it is a speed bump, not a security boundary. See the C11
//     build report for exactly what it does and does not defend against.
//   - POST attribution (postAttribution below, plus the recorded_by wiring
//     in trips.go/gate.go/alerts.go): the SITE CONTROL POST this
//     box is, taken from config (Cfg.PostName), written as recorded_by on
//     newly authored visits/trips. James, D-5 (2026-08-21): "yes by post
//     not person." It costs the operator nothing — there is no picker, no
//     session state and no click, because a box is only ever one post.
//     This was never an authentication layer either: a payload naming the
//     post duplicates what store.Envelope already carries structurally
//     (SiteID/OriginAuth, hash-covered and enforced at ingest by
//     repl/engine.go) — see the OFFICER-ATTRIBUTION report.
//   - Replication-peer origin identity (design §8.4, built at C3a): the
//     demo peer_key a REMOTE SITE presents when pulling. Entirely separate
//     machinery — /sync/* and /debug/* are deliberately NOT gated by this
//     file's login: a peer node cannot interactively log in, and the debug
//     dump is operational tooling (C12's dumpcompare), not an officer form.
//
// WHAT IS GATED AND WHY (see the C11 build report for the full reasoning):
// only the MUTATING routes — the ones that author an event or change the
// session — are wrapped in requireLogin below: /trips/*, /gate/checkin,
// /gate/checkout, /gate/resume, /alerts/ack, /trade/create, /trade/amend —
// and ⟦W8⟧ the naming-and-numbers screen `GET /trade`, which is gated too and
// is the ONE READ THAT IS: it renders the site's trade list and the three
// positional handset numbers, which is reference data an unauthenticated
// caller has no business reading off a post. Every OTHER GET
// view (/, /trips, /gate, /muster, /muster/print) stays UNGATED on purpose:
// §12 names the purpose as accountability for entries, not confidentiality,
// so there is no stated reason to block a read, and gating reads would only
// add friction §12 explicitly warns against ("must not obstruct gate
// operation during a shift-change queue").
//
// D-5 (2026-08-21) RETIRED requireOfficer, which used to sit nested inside
// requireLogin on every authoring route and withhold the first submission
// of a session until a NAME was tapped from a config list. It did no access
// control whatsoever — every route it wrapped was already behind
// requireLogin, and it had an explicit no-op path when the list was empty —
// so removing it weakens no gate. What it recorded (a person) is not what
// records are attributed to (a post), and the post is already known: see
// postAttribution below.
//
// THE ABSENCE IS THE FEATURE (design §10, chunk-plan C11 not-done-if: "any
// timeout/lockout default survives"). See sessionStore's doc comment below
// for the structural argument, and auth_test.go's
// TestSessionIdleForHoursIsStillLive / TestLoginCookieHasNoServerEnforcedExpiry /
// TestNoLockoutAfterRepeatedWrongPasswords for the executable proof.
package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"sitecontrol/internal/config"
)

const sessionCookieName = "sc_session"

// Session is one logged-in browser's server-side state.
//
// It carries NO attribution state (D-5): the post a record is attributed to
// is a property of the box, not of the browser, so nothing here has to be
// chosen, defaulted, switched at handover, or blocked on. CreatedAt is
// AUDIT/DISPLAY ONLY — never read by any validity check in this file; see
// sessionStore's doc comment immediately below for why that absence is
// deliberate, not an oversight.
type Session struct {
	Token     string
	CreatedAt time.Time
	// Mode ⟦R-28, team-lead, 2026-09-16⟧ is the officer's login-time choice
	// between the two gate pages — "individual" (default/zero value) or
	// "vehicle". It is READ ONLY to pick the post-login landing page
	// (handleLoginSubmit) and to drive the header's "switch mode" link
	// (gate.go's gateData/_topbar.html); it never gates access to either
	// page — both /gate and /gate/vehicle stay reachable regardless of a
	// session's Mode, exactly as they already were before this field
	// existed, so a stale or wrong Mode can only mis-route a redirect, never
	// lock an officer out of either screen.
	Mode string
}

// sessionStore is a bare map: no expiry timestamp, no TTL field, no sweep
// goroutine, no comparison against time.Now() anywhere in this type. That
// absence IS design §10's "explicitly rejects idle-timeout and lockout" —
// a screen that locks itself is named a MUSTER HAZARD there (a gate officer
// mid-evacuation cannot record who left), so the mechanism that would
// produce one does not merely go unbuilt here, it structurally cannot exist
// without someone adding a time comparison to this file and contradicting
// this comment to do it. Sessions live and die with the process (a restart
// clears them, same acceptable-process-memory posture as trips.go's
// tripFirstSeen anchor) — nothing alert- or accountability-critical is lost
// by that: attribution is the box's post, not anything the session holds,
// and it is already durable in the event log (and in the envelope) by the
// time any session ends.
type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

func newSessionStore() *sessionStore {
	return &sessionStore{sessions: make(map[string]*Session)}
}

func newSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (ss *sessionStore) create() (*Session, error) {
	tok, err := newSessionToken()
	if err != nil {
		return nil, err
	}
	sess := &Session{Token: tok, CreatedAt: time.Now()}
	ss.mu.Lock()
	ss.sessions[tok] = sess
	ss.mu.Unlock()
	return sess, nil
}

// get looks a token up and returns it unchanged — no time check of any
// kind, deliberately (type doc above).
func (ss *sessionStore) get(token string) (*Session, bool) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	sess, ok := ss.sessions[token]
	return sess, ok
}

func (ss *sessionStore) delete(token string) {
	ss.mu.Lock()
	delete(ss.sessions, token)
	ss.mu.Unlock()
}

// --- request-scoped helpers ---

func (s *Server) currentSession(r *http.Request) (*Session, bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return nil, false
	}
	return s.Sessions.get(c.Value)
}

// proxyAuthHeader carries the demo proxy's shared token (Cfg.ProxyAuthToken).
const proxyAuthHeader = "X-SC-Proxy-Auth"

// proxyAuthed reports whether the request carries the configured proxy token.
// Off (false) whenever the config key is unset, whatever the header holds.
func (s *Server) proxyAuthed(r *http.Request) bool {
	tok := s.Cfg.ProxyAuthToken
	if tok == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(r.Header.Get(proxyAuthHeader)), []byte(tok)) == 1
}

// authedSession is currentSession plus the demo proxy path: a request that
// carries a valid proxy token but no live session gets a normal session
// minted and its cookie set, so everything downstream behaves as after a
// form login. Always (nil,false) for a non-proxy request without a session.
func (s *Server) authedSession(w http.ResponseWriter, r *http.Request) (*Session, bool) {
	if sess, ok := s.currentSession(r); ok {
		return sess, true
	}
	if !s.proxyAuthed(r) {
		return nil, false
	}
	sess, err := s.Sessions.create()
	if err != nil {
		return nil, false
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: sess.Token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	return sess, true
}

// postAttribution is what every form handler calls to fill recorded_by
// (D-5): the SITE CONTROL POST this box is, from config. It takes no
// request, reads no session and can never be nil, because the answer does
// not depend on who is at the keyboard — a box is one post for its whole
// life. It replaces recordedByPtr, which returned a session-picked PERSON
// and returned nil until someone tapped a picker.
//
// This value is deliberately REDUNDANT with store.Envelope.SiteID, which
// already identifies the authoring post structurally, is covered by the
// envelope hash, and is checked against the authenticated peer at ingest
// (repl/engine.go's identityOK). The payload copy is kept only because it
// is the authoring post's OWN name for itself — a peer rendering it does
// not have to have the right display name for us in its own config — and
// because two surfaces that read it were open questions for James rather
// than settled ones. ⟦was: "(the weather-phase declaration and the phase
// override)" — both removed with weather phases, James 2026-10-04⟧
// See reports/OFFICER-ATTRIBUTION.md.
func (s *Server) postAttribution() *string {
	post := s.Cfg.PostName()
	return &post
}

// --- the gate itself ---

type loginData struct {
	Cfg   *config.Config
	Next  string
	Error string

	// NotRecorded is true when this login page is being served IN PLACE OF
	// a mutating action the officer just submitted with a dead/missing
	// session (requireLogin below). Without this flag the officer lands on
	// a blank login form with no indication their check-in/dispatch/etc.
	// was never authored — discoverable only by noticing the on-site list
	// didn't grow, which a busy gate officer mid-queue can easily miss.
	// C12's own container power-cycles will hit exactly this path (a
	// process restart clears every in-memory session), so this is not a
	// theoretical edge case.
	NotRecorded bool
}

func (s *Server) renderLogin(w http.ResponseWriter, data loginData) {
	s.renderPage(w, "login", "login.html", data)
}

// requireLogin wraps a mutating handler. An unauthenticated request gets the
// login form SERVED DIRECTLY at the URL it tried to reach, status 200 — not
// a redirect — precisely so a probe can assert on BODY CONTENT ("the login
// form is served, or the protected content is absent") rather than status
// code, per the C5-verification carry-forward (row 22): the C1 catch-all
// already makes every unmatched path answer 200, so status alone proves
// nothing here either.
func (s *Server) requireLogin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authedSession(w, r); ok {
			next(w, r)
			return
		}
		nextPath := r.URL.Path
		notRecorded := false
		if r.Method != http.MethodGet {
			// A gated POST's own path (e.g. /trips/dispatch) has no GET
			// handler to land on after login; send them back to the page
			// that held the form instead. They will need to RE-ENTER the
			// action — nothing was authored (ParseForm is never called on
			// this path), and NotRecorded below is what tells them that
			// plainly instead of leaving it discoverable-only.
			notRecorded = true
			// S-037 (W4, 2026-10-03): a browser's Referer is ABSOLUTE
			// ("http://host/gate/vehicle"), and safeNext rightly refuses an
			// absolute URL — so every re-login after a refused POST used to
			// land on /gate, whichever page the officer was on. Reduce it to
			// its path first (refererPath, theme.go: scheme and host are
			// discarded, so the result can only ever name a page on THIS
			// box), then safeNext judges it as before.
			//
			// A Referer that is not a page — the JS-off vehicle form
			// re-renders at its POST-only accumulate routes
			// (/gate/passenger/add …), which have no GET — leaves next
			// EMPTY instead, so the login form's own mode choice
			// ("Individual check-in" / "Vehicle check-in") picks the page:
			// the officer presses the one they were on. Same for no
			// Referer at all (was "/", which also landed on /gate).
			nextPath = ""
			if ref := refererPath(r); ref != "" && s.isPage(ref) {
				nextPath = ref
			}
		}
		s.renderLogin(w, loginData{Cfg: s.Cfg, Next: nextPath, NotRecorded: notRecorded})
	}
}

// isPage reports whether a GET of this same-origin path reaches a real page
// on this box rather than the catch-all status page (web.go's "GET /").
// Answered by the served mux itself, so it cannot drift from the routes.
func (s *Server) isPage(path string) bool {
	mux := s.pages.Load()
	if mux == nil {
		return true
	}
	u, err := url.Parse(path)
	if err != nil {
		return false
	}
	_, pattern := mux.Handler(&http.Request{Method: http.MethodGet, URL: u, Host: "localhost"})
	return pattern != "" && pattern != "GET /"
}

func safeNext(next string) string {
	// A browser does not parse this string the way the two prefix checks
	// below do: the URL parser deletes every ASCII tab/CR/LF from the input
	// wherever it falls, and folds every backslash onto a forward slash.
	// So a value this function reads as one same-origin path (a single
	// leading "/") can still resolve, in a real browser, to "//host" -- a
	// different origin. Normalize the same way before deciding, so
	// admission tracks what actually reaches the browser, not the raw
	// bytes this function was handed. See s123HostileNext's "leading
	// backslash" and "embedded tab" cases.
	//
	// S-032 (W4, 2026-10-03): a value carrying ANY control character is
	// refused outright rather than normalised and admitted. "/gate\r\nSet-
	// Cookie: x=1" used to be admitted unchanged — net/http's header writer
	// folded the CR/LF to spaces, so no header was ever injected, but the
	// officer landed on "/gate  Set-Cookie:x=1", a NO SUCH PAGE. The guard
	// no longer leans on a lower layer for that: no legitimate path contains
	// a control character, so none is ever handed to http.Redirect.
	for _, c := range next {
		if c < 0x20 || c == 0x7f {
			return "/gate"
		}
	}
	normalized := strings.NewReplacer("\t", "", "\r", "", "\n", "", `\`, "/").Replace(next)
	if strings.HasPrefix(normalized, "/") && !strings.HasPrefix(normalized, "//") {
		return next
	}
	return "/gate"
}

// gateLandingPath ⟦R-28, team-lead, 2026-09-16⟧ is the mode-dependent
// default landing page — "individual" (or anything else; the zero value
// included) reads as /gate, same default as every day before this field
// existed. It is deliberately NOT folded into safeNext itself: that
// function's fallback-to-/gate behaviour on an empty or hostile `next` is
// pinned by TestSafeNextRefusesEveryHostileRedirectTarget and
// TestSafeNextPreservesLegitimateDestinations (auth_safenext_test.go) at
// ONE argument, and changing its signature would break both locked tests
// for no safety reason — the two concerns are independent (safeNext
// answers "is this destination safe to redirect to", this answers "which
// page does this session's chosen mode prefer") and are composed at the
// two call sites below instead.
func gateLandingPath(mode string) string {
	if mode == "vehicle" {
		return "/gate/vehicle"
	}
	return "/gate"
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	next := r.URL.Query().Get("next")
	if sess, ok := s.authedSession(w, r); ok {
		dest := safeNext(next)
		if next == "" {
			dest = gateLandingPath(sess.Mode)
		}
		http.Redirect(w, r, dest, http.StatusSeeOther)
		return
	}
	s.renderLogin(w, loginData{Cfg: s.Cfg, Next: next})
}

func (s *Server) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	password := r.FormValue("password")
	rawNext := r.FormValue("next")
	next := safeNext(rawNext)
	// ⟦R-28⟧ the mode choice only overrides the ORDINARY case — no deep
	// link was requested at all. A hostile or malformed `next` still falls
	// back to plain "/gate" exactly as safeNext always has (untouched,
	// still covered by the locked test above); this is a narrower,
	// independent branch layered on top, not a change to what "safe" means.
	mode := "individual"
	if r.FormValue("mode") == "vehicle" && s.Cfg.VehicleModeEnabled() {
		mode = "vehicle"
	}
	if rawNext == "" {
		next = gateLandingPath(mode)
	}
	notRecorded := r.FormValue("not_recorded") == "1" // carried through a wrong-password retry, see login.html

	// Constant-time compare: cheap to do right even though §12 names this
	// credential a speed bump, not a security boundary — that is a reason
	// not to build MORE than this, not a reason to build this line sloppily.
	// s.Cfg.LoginPassword == "" is refused explicitly so a misconfigured
	// (unset) site can never be logged into via a blank submission.
	if s.Cfg.LoginPassword == "" || subtle.ConstantTimeCompare([]byte(password), []byte(s.Cfg.LoginPassword)) != 1 {
		// NO FAILED-ATTEMPT COUNTER, NO LOCKOUT (design §10 not-done-if:
		// "any timeout/lockout default survives"). A wrong password simply
		// re-serves the form, unconditionally, every time — see
		// TestNoLockoutAfterRepeatedWrongPasswords.
		s.renderLogin(w, loginData{Cfg: s.Cfg, Next: next, Error: "That was the wrong password — try again.", NotRecorded: notRecorded})
		return
	}

	// D-5: nothing about attribution happens here. A session is a session;
	// what a record is attributed to is the box's post (postAttribution),
	// which is already true before anyone logs in. The predecessor of this
	// line stamped Cfg.Officers[0] as "a sane default" (F42 removed that,
	// at the cost of a mandatory tap before the first authored action);
	// there is now no name to default, so neither hazard exists.
	sess, err := s.Sessions.create()
	if err != nil {
		http.Error(w, "could not start session", http.StatusInternalServerError)
		return
	}
	sess.Mode = mode // ⟦R-28⟧ held for the header's switch-mode link and any later re-landing (handleLoginPage); never gates either page.
	http.SetCookie(w, &http.Cookie{
		Name:  sessionCookieName,
		Value: sess.Token,
		Path:  "/",
		// DELIBERATELY NO MaxAge, NO Expires (design §10: no idle-timeout).
		// Both are Go zero values, and net/http's documented behaviour for
		// http.Cookie is that a zero MaxAge/Expires emits NO Max-Age/Expires
		// attribute at all — an ordinary browser-session cookie whose
		// lifetime this SERVER never bounds. See
		// TestLoginCookieHasNoServerEnforcedExpiry.
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookieName); err == nil {
		s.Sessions.delete(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// registerAuthRoutes mounts the login/logout surface. Called from Handler()
// (web.go). GET/POST /login and POST /logout are deliberately UNGATED (you
// cannot require login to reach the login page).
//
// POST /officer is GONE (D-5). It is not disabled behind a flag and not
// left answering 405 — the route is not registered, so the C1 catch-all
// answers it like any other unknown path, and no session state exists for
// it to change. TestNoOfficerPickerRouteExists is the executable guard.
func (s *Server) registerAuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("POST /login", s.handleLoginSubmit)
	mux.HandleFunc("POST /logout", s.handleLogout)
}
