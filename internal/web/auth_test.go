// auth_test.go — C11's own done-when tests (chunk-plan C11, design §12/§10),
// as amended by D-5 (2026-08-21, "yes by post not person"): forms require
// login. ⚠ ⟦LOCK — §60, 2026-09-03⟧ THIS SENTENCE CITED A TEST THAT WAS DELETED, NOT
// RENAMED, and it is RECORDED rather than re-pointed: the dev-alias route required login
// even with its config flag on, and the test proving it lived in internal/web/
// devroutes_test.go. ⛔ BOTH the route and that file went with the mechanism (P-3 §5 /
// [[D-47]]), and NO surviving test asserts it [MEASURED — nothing in the module pairs the
// dev route with the login gate]. Re-pointing this citation at a live login test would be
// a FALSE citation: no survivor proves what it proved.
// ⚠ The superseded line is preserved VERBATIM in
// reports/window-build/ALIAS-DELETION-REPAIR.md §4.10, NOT here — it names a test that no
// longer exists, and citations_test.go:103 fails any comment that does.
// recorded_by lands on new
// rows carrying THIS BOX'S POST and never a person; a logged-in session
// authors on its FIRST submission with no picker step in the way; a session
// idle for hours is still live, with no framework default (cookie MaxAge,
// session store expiry) quietly reintroducing a timeout; and no lockout
// survives repeated wrong passwords.
package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// newAuthTestServer is a dedicated site-a box. Its DisplayName is its POST
// NAME (config.PostName) — since D-5 that is the only attribution identity
// there is, so these tests assert against srv.Cfg.PostName() rather than
// against any configured list of people.
func newAuthTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		SiteID: "site-a", DisplayName: "Ice Road A", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 10,
		LoginPassword: testLoginPassword,
		// One peer, so this fixture can dispatch at all: since D-4 every
		// departure names where it is going, and the destination must be a
		// configured one. These tests are about authentication and officer
		// attribution — the destination is fixture, not subject.
		Peers: []config.Peer{{SiteID: "site-b", BaseURL: "http://127.0.0.1:0", Key: "k"}},
	}
	return New(cfg, st), st
}

func postFormWithCookie(t *testing.T, srv *Server, path string, form url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// --- "forms require login" ---

// TestUnauthenticatedFormsServeLoginNotTheAction is C11's done-when in its
// most literal form: "forms require login". Per the C5-verification
// carry-forward (row 22 — never assert gating by status code, C1's
// catch-all answers 200 to everything unmatched), every assertion here is
// on BODY CONTENT (the login form present, the action's own success text
// absent) and on STORE STATE (nothing authored), never on HTTP status alone.
func TestUnauthenticatedFormsServeLoginNotTheAction(t *testing.T) {
	srv, st := newAuthTestServer(t)

	cases := []struct {
		name string
		path string
		form url.Values
	}{
		{"dispatch", "/trips/dispatch", url.Values{"vehicle": {"T1"}, "driver_person_id": {"nobody"}}},
		// ⟦D-78, 2026-09-05⟧ ⟦was: {"cancel", "/trips/cancel", url.Values{"trip_id": {"whatever"}}}⟧
		// The cancel route is closed (D-73/D-78) — there is no action left to
		// gate. The row is REPLACED, not removed, so the number of gated
		// authoring forms this test walks does not shrink: /trips/arrive is
		// the trips-board form that stands in.
		{"arrive", "/trips/arrive", url.Values{"trip_id": {"whatever"}}},
		{"gate checkin", "/gate/checkin", url.Values{"vehicle": {"TRK-14"}, "driver_kind": {"new"}, "driver_value": {"Nobody"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wmBefore, err := st.Watermark("site-a", 1)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req) // NO cookie — unauthenticated

			body := rec.Body.String()
			if !strings.Contains(body, `action="/login"`) {
				t.Fatalf("%s: expected the login form to be served unauthenticated, got:\n%s", tc.path, body)
			}
			if strings.Contains(body, "Recording as:") {
				// the post chip only ever renders on an authenticated page —
				// its presence here would mean the protected page rendered.
				t.Fatalf("%s: protected page content leaked into an unauthenticated response", tc.path)
			}
			wmAfter, err := st.Watermark("site-a", 1)
			if err != nil {
				t.Fatal(err)
			}
			if wmAfter != wmBefore {
				t.Fatalf("%s: an event was authored despite no session (watermark %d -> %d)", tc.path, wmBefore, wmAfter)
			}
		})
	}
}

// --- "recorded_by lands on new rows", by POST (D-5) ---

// TestRecordedByOnTripsIsThePostAndCostsNoInteraction is what replaced
// C11's "the picker switches officer in one selection". D-5 ruled records
// are attributed to the site control post that made them, not to a named
// individual, so there is nothing to switch: the answer is config, known
// before anyone logs in.
//
// Two things are asserted together because they are the same ruling:
//
//   - the FIRST submission of a fresh authenticated session AUTHORS. No
//     need-officer page, no withheld-and-replayed request, no tap. This is
//     the operator cost F42 added and D-5 removed, asserted structurally
//     (303 + the event present) rather than by scanning page text.
//   - recorded_by is exactly this box's post, and it AGREES with the
//     envelope's own structural origin (trip.origin_site == Cfg.SiteID) —
//     the payload may be redundant with the envelope, but it must never
//     contradict it.
func TestRecordedByOnTripsIsThePostAndCostsNoInteraction(t *testing.T) {
	srv, st := newAuthTestServer(t)
	cookie := loginCookie(t, srv, testLoginPassword)
	// A real roster person — F11 (Dispatch validates driver/occupant ids
	// over real HTTP): this test is about attribution, not driver validity.
	driver, err := domain.CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	rec := postFormWithCookie(t, srv, "/trips/dispatch", url.Values{
		"time_source": {"default"},
		"vehicle":     {"TRUCK A"}, "driver_person_id": {driver.PersonID},
		"destination_site": {"site-b"}, "deadline_minutes": {"60"},
	}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("first dispatch of a fresh session = %d, want 303 (it authors immediately; D-5 removed the picker step): %s", rec.Code, rec.Body.String())
	}

	trips, err := domain.Trips(st)
	if err != nil {
		t.Fatal(err)
	}
	// ⟦LOCK — W7 seam repair, 2026-09-03⟧ ⟦was: "|| trips[0].VehicleEntered != \"TRUCK A\" {"⟧
	if len(trips) != 1 || trips[0].VehicleEntered == nil || *trips[0].VehicleEntered != "TRUCK A" {
		t.Fatalf("trips = %+v, want exactly the one dispatch, vehicle TRUCK A", trips)
	}
	post := srv.Cfg.PostName()
	if post == "" {
		t.Fatal("fixture: Cfg.PostName() is empty")
	}
	if trips[0].RecordedBy == nil || *trips[0].RecordedBy != post {
		t.Fatalf("trip recorded_by = %v, want %q (this box's post) — never a person's name and never nil", trips[0].RecordedBy, post)
	}
	// Attribution must not be able to disagree with the envelope: the
	// authoring site is structural (store.Envelope.SiteID, folded into
	// trip.origin_site), and the post name is that same site's name.
	if trips[0].OriginSite != srv.Cfg.SiteID {
		t.Fatalf("trip origin_site = %q, want %q — the envelope's own authoring site", trips[0].OriginSite, srv.Cfg.SiteID)
	}
	if *trips[0].RecordedBy != srv.Cfg.PostName() {
		t.Fatalf("recorded_by %q is not the post name of origin_site %q — the payload contradicts the envelope", *trips[0].RecordedBy, trips[0].OriginSite)
	}
}

// TestRecordedByLandsOnGateCheckInVisits is the gate-check-in half of
// "recorded_by lands on new rows" — the trips.go half is covered above.
// Same D-5 shape: one submission, no picker, the post on the row.
func TestRecordedByLandsOnGateCheckInVisits(t *testing.T) {
	srv, st := newAuthTestServer(t)
	cookie := loginCookie(t, srv, testLoginPassword)

	rec := postFormWithCookie(t, srv, "/gate/checkin", url.Values{
		"time_source": {"default"},
		"vehicle":     {"TRK-14"}, "driver_kind": {"new"}, "driver_value": {"Walk In Wanda"},
	}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("first gate check-in of a fresh session = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	visits, err := domain.Visits(st)
	if err != nil {
		t.Fatal(err)
	}
	post := srv.Cfg.PostName()
	if len(visits) != 1 || visits[0].RecordedBy == nil || *visits[0].RecordedBy != post {
		t.Fatalf("visits = %+v, want exactly 1 with recorded_by %q (this box's post)", visits, post)
	}
}

// TestPostAttributionFallsBackToSiteIDRatherThanBlank is the successor to
// F20's nil-contract test (test-suite audit mutation M11), which used to
// pose "no officer configured or picked" and assert a genuine NULL. Since
// D-5 there is no such state at authoring time — the post is config — so
// the live hazard moved: a box whose display_name was never filled in must
// not author a BLANK attribution. config.PostName falls back to SiteID,
// which is the same identity the envelope carries structurally, so the
// worst case is an id rather than an absence.
//
// Mutating config.PostName's fallback to return "" makes this go red.
func TestPostAttributionFallsBackToSiteIDRatherThanBlank(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		SiteID: "site-a", ListenAddr: ":0", // DisplayName deliberately unset
		NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 10,
		LoginPassword: testLoginPassword,
	}
	srv := New(cfg, st)
	cookie := loginCookie(t, srv, testLoginPassword)

	rec := postFormWithCookie(t, srv, "/gate/checkin", url.Values{
		"time_source": {"default"},
		"vehicle":     {"TRK-14"}, "driver_kind": {"new"}, "driver_value": {"Nameless Nolan"},
	}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("gate check-in = %d: %s", rec.Code, rec.Body.String())
	}
	visits, err := domain.Visits(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(visits) != 1 {
		t.Fatalf("visits = %+v, want exactly 1", visits)
	}
	if visits[0].RecordedBy == nil || *visits[0].RecordedBy != "site-a" {
		t.Fatalf("visit recorded_by = %v, want %q — an unnamed post falls back to its site id, never to blank or nil", visits[0].RecordedBy, "site-a")
	}
}

// --- "explicit absence of idle-timeout and lockout" ---

// TestSessionIdleForHoursIsStillLive is the chunk's headline assert (design
// §10 names auto-lock a MUSTER HAZARD: a gate officer whose session locked
// mid-evacuation cannot record who left). It simulates hours of idle time by
// directly rewriting the session's CreatedAt far into the past — same
// package, white-box — rather than sleeping in a test, then proves the
// server-side code path never consults that timestamp: a request bearing
// the (now apparently very old) session is still treated as fully live.
func TestSessionIdleForHoursIsStillLive(t *testing.T) {
	srv, st := newAuthTestServer(t)
	cookie := loginCookie(t, srv, testLoginPassword)
	// A real roster person — F11 (Dispatch now validates driver/occupant
	// ids over real HTTP): this test is about session idle liveness, not
	// driver validity, so the driver just needs to be a genuine roster
	// entry rather than the old made-up "driver-a".
	driver, err := domain.CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	sess, ok := srv.Sessions.get(cookie.Value)
	if !ok {
		t.Fatal("session not found right after login")
	}
	sess.CreatedAt = time.Now().Add(-6 * time.Hour) // simulate a shift's worth of idle time

	rec := postFormWithCookie(t, srv, "/trips/dispatch", url.Values{
		"time_source": {"default"},
		"vehicle":     {"STILL LIVE"}, "driver_person_id": {driver.PersonID},
		"destination_site": {"site-b"}, "deadline_minutes": {"60"},
	}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("dispatch with a 6-hour-old session = %d, want 303 (still authenticated): %s", rec.Code, rec.Body.String())
	}
	trips, err := domain.Trips(st)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tr := range trips {
		// ⟦LOCK — W7 seam repair, 2026-09-03⟧ ⟦was: "if tr.VehicleEntered == \"STILL LIVE\" {"⟧
		if tr.VehicleEntered != nil && *tr.VehicleEntered == "STILL LIVE" {
			found = true
		}
	}
	if !found {
		t.Fatal("the dispatch made with a 6-hour-old session was not authored — the session was treated as expired")
	}
}

// TestLoginCookieHasNoServerEnforcedExpiry is the framework-default check
// the brief specifically asked for: confirm no default (http.Cookie.MaxAge,
// http.Cookie.Expires) quietly reintroduces a timeout. Both must be their
// Go zero values, which net/http documents as "no such attribute sent" —
// an ordinary browser-session cookie, never a server-timed one.
func TestLoginCookieHasNoServerEnforcedExpiry(t *testing.T) {
	srv, _ := newAuthTestServer(t)
	form := url.Values{"password": {testLoginPassword}, "next": {"/"}}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	var found *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			found = c
		}
	}
	if found == nil {
		t.Fatal("login did not set a session cookie")
	}
	if found.MaxAge != 0 {
		t.Fatalf("session cookie MaxAge = %d, want 0 (no server-enforced expiry, design §10)", found.MaxAge)
	}
	if !found.Expires.IsZero() {
		t.Fatalf("session cookie Expires = %v, want the zero value (no server-enforced expiry, design §10)", found.Expires)
	}
}

// TestNoLockoutAfterRepeatedWrongPasswords: the not-done-if guard is "any
// timeout/lockout default survives". Twenty consecutive wrong passwords
// must never produce a "too many attempts" refusal, and the 21st attempt
// with the CORRECT password must succeed exactly as if it were the first.
func TestNoLockoutAfterRepeatedWrongPasswords(t *testing.T) {
	srv, _ := newAuthTestServer(t)
	for i := 0; i < 20; i++ {
		form := url.Values{"password": {"wrong password"}, "next": {"/"}}
		req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		// Style-stripped before matching, the convention this repo already
		// uses everywhere it asserts on markup: a word inside a stylesheet
		// is not a message shown to the officer. Without it "locked" below
		// could match a RULE NAME in the shared stylesheet (⟦was:
		// ".phase-blocked", removed with weather phases⟧) and this test fails on a page that says nothing of the kind — the
		// same class-name-is-not-a-rendered-element trap as F19, in reverse.
		shown := styleRe.ReplaceAllString(rec.Body.String(), "")
		if !strings.Contains(shown, "wrong password") {
			t.Fatalf("attempt %d: expected the ordinary wrong-password message, got:\n%s", i+1, shown)
		}
		if strings.Contains(strings.ToLower(shown), "too many") || strings.Contains(strings.ToLower(shown), "locked") {
			t.Fatalf("attempt %d: a lockout message appeared — design §10 explicitly rejects lockout:\n%s", i+1, shown)
		}
	}
	// The 21st attempt, correct password, must succeed exactly as a first attempt would.
	cookie := loginCookie(t, srv, testLoginPassword)
	if cookie == nil {
		t.Fatal("correct password after 20 wrong attempts did not create a session — a lockout is in effect")
	}
}

// --- a gated POST with no/dead session must say so, not discard silently ---

// TestGatedFormWithDeadSessionExplicitlyWarnsNothingWasRecorded is review
// FIX 1: requireLogin never calls ParseForm on the interrupted request, so
// the submitted check-in/dispatch/etc. is never authored — but the officer
// must be told that in plain words, not left to notice its absence on the
// on-site list. C12's own container power-cycles clear every in-memory
// session, so this path is not theoretical.
func TestGatedFormWithDeadSessionExplicitlyWarnsNothingWasRecorded(t *testing.T) {
	srv, st := newAuthTestServer(t)
	wmBefore, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/gate/checkin", strings.NewReader(url.Values{
		"vehicle": {"TRK-14"}, "driver_kind": {"new"}, "driver_value": {"Ghost Officer"},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// A cookie naming a session the server has never heard of — the exact
	// shape of "dead session" a process restart produces (the in-memory
	// store is empty, but a browser may still be holding an old cookie).
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "stale-token-nobody-issued"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "was NOT recorded") {
		t.Fatalf("expected an explicit not-recorded warning, got:\n%s", body)
	}
	if strings.Contains(strings.ToLower(body), "session expired") {
		t.Fatal("the warning must not be softened into \"session expired\" language (review FIX 1)")
	}
	if !strings.Contains(body, `name="not_recorded" value="1"`) {
		t.Fatal("the not-recorded flag must survive a wrong-password retry via the hidden form field")
	}
	wmAfter, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if wmAfter != wmBefore {
		t.Fatal("an event was authored despite the dead session — the form should never have been parsed")
	}

	// The warning must survive a WRONG password retry too, not just the
	// first render (login.html's hidden not_recorded field is what carries
	// it — this proves the round trip, not just the initial render).
	form := url.Values{"password": {"wrong"}, "next": {"/gate"}, "not_recorded": {"1"}}
	req2 := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec2, req2)
	if !strings.Contains(rec2.Body.String(), "was NOT recorded") {
		t.Fatalf("not-recorded warning did not survive a wrong-password retry:\n%s", rec2.Body.String())
	}
}

// --- reads stay ungated (design §12: accountability, not confidentiality) ---

// TestViewsStayUngated documents and asserts the C11 build report's stated
// scope decision: GET pages are never behind login, only the mutating
// routes are. This is not a done-when the chunk-plan names directly, but it
// is the flip side of "forms require login" and worth pinning so a future
// change does not silently widen the gate onto reads.
//
// ⚠️ /debug/projection-dump WAS in this list, and its removal from it is a
// CRITERION CHANGE, not a convenience. This test used to assert that the
// route answered 200/application/json unauthenticated, calling that
// "ungated by design... worth pinning hardest of all" — i.e. it pinned the
// exposure D-56 (2026-09-01) then ruled must not exist: "this program will
// not ever have a data dump". The route is gone, so the assertion is not
// weakened here, it is INVERTED and moved: TestNoProjectionDumpRouteExists
// (web_test.go) now asserts the path serves no dump, and asserts it on
// content and body shape rather than on a status code, because GET / is a
// catch-all and any code-only assertion about a removed path is vacuous.
//
// The pages listed below are ordinary rendered views carrying no contact
// details; nothing about the accountability-not-confidentiality posture for
// THEM changed.
func TestViewsStayUngated(t *testing.T) {
	srv, _ := newAuthTestServer(t)
	for _, path := range []string{"/status", "/trips", "/gate", "/muster", "/muster/print"} {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", path, nil)) // no cookie
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), `action="/login"`) {
			t.Fatalf("GET %s: served the login form even though views are not gated", path)
		}
	}
}

// --- D-5: there is no picker, and no route that could set one ---

// TestNoOfficerPickerRouteExists pins the removal itself. POST /officer was
// the picker's entire surface; it is not registered any more, so it must
// not (a) be served by some surviving handler, or (b) change anything.
//
// It is asserted on STORE STATE and on the ABSENCE of the picker's own
// effect, never on status code alone — the C1 catch-all answers 200 to
// every unmatched path (C5-verification carry-forward row 22), so a status
// assertion here would pass whether the route existed or not.
//
// Mutating registerAuthRoutes to re-register POST /officer (with any
// handler that stores a name and redirects 303) makes this go red on the
// redirect assertion.
func TestNoOfficerPickerRouteExists(t *testing.T) {
	srv, st := newAuthTestServer(t)
	cookie := loginCookie(t, srv, testLoginPassword)

	wmBefore, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	rec := postFormWithCookie(t, srv, "/officer", url.Values{"officer": {"Officer A2"}}, cookie)
	// The retired handler answered 303 (or 400 for an unlisted name). The
	// catch-all answers the site's own 200 page. Either of the handler's
	// answers means the route is back.
	if rec.Code == http.StatusSeeOther || rec.Code == http.StatusBadRequest {
		t.Fatalf("POST /officer = %d — the session-officer picker route is registered again (D-5 retired it): %s", rec.Code, rec.Body.String())
	}
	wmAfter, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if wmAfter != wmBefore {
		t.Fatalf("POST /officer authored an event (watermark %d -> %d)", wmBefore, wmAfter)
	}

	// And the session it could not touch still attributes to the post: a
	// dispatch after that POST records the post name, not "Officer A2".
	driver, err := domain.CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if r := postFormWithCookie(t, srv, "/trips/dispatch", url.Values{
		"time_source": {"default"},
		"vehicle":     {"AFTER-OFFICER-POST"}, "driver_person_id": {driver.PersonID},
		"destination_site": {"site-b"}, "deadline_minutes": {"60"},
	}, cookie); r.Code != http.StatusSeeOther {
		t.Fatalf("dispatch after POST /officer = %d, want 303: %s", r.Code, r.Body.String())
	}
	trips, err := domain.Trips(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 1 || trips[0].RecordedBy == nil || *trips[0].RecordedBy != srv.Cfg.PostName() {
		t.Fatalf("trips = %+v, want one trip recorded_by %q — a POST /officer must not be able to put a person's name on a record", trips, srv.Cfg.PostName())
	}
}

// TestNoPageOffersAPickAnOfficerControl is the rendered half of the same
// removal: no operator-reachable page may present a control that posts to
// /officer. Asserted on the STRUCTURAL hook (a form whose action is the
// retired route), not on prose.
//
// Mutating post_chip.html to render a <form action="/officer"> makes this
// go red.
func TestNoPageOffersAPickAnOfficerControl(t *testing.T) {
	for route, body := range renderedPages(t) {
		if strings.Contains(body, `action="/officer"`) {
			t.Errorf("%s: renders a form posting to /officer — the session-officer picker was retired by D-5", route)
		}
	}
}

// TestTopBarStatesTheRecordingPost is the positive half: the operator is
// still TOLD what their entries are attributed to, they are just not asked
// to choose it. The post chip carries the post name and the Log out
// control, on the pages whose handlers populate it.
//
// Mutating post_chip.html to drop {{.Post}} makes this go red.
func TestTopBarStatesTheRecordingPost(t *testing.T) {
	pages := renderedPages(t)
	// ⟦W3 S-034, 2026-10-03⟧ renderedPages fetches /gate with NO session (it
	// is ungated, §12); that page now says "Not logged in" instead of
	// "Recording as … / Log out". The recording-post claim is asserted on
	// the page an officer actually works — logged in — and the anonymous
	// render is pinned to say it holds no session.
	gateSrv, _, _ := newGateTestServer(t)
	pages["/gate"] = w3Get(t, gateSrv, "/gate/vehicle")
	if anon := w3AnonGet(t, gateSrv, "/gate/vehicle"); strings.Contains(anon, "Recording as:") || !strings.Contains(anon, "Not logged in") {
		t.Error("anonymous /gate claims a recording session (S-034)")
	}
	for _, route := range []string{"/trips", "/gate"} {
		body, ok := pages[route]
		if !ok {
			t.Fatalf("fixture: renderedPages has no %s", route)
		}
		if !strings.Contains(body, "Recording as:") {
			t.Errorf("%s: no recording-post chip in the top bar", route)
		}
		if !strings.Contains(body, `action="/logout"`) {
			t.Errorf("%s: the post chip must still carry the Log out control", route)
		}
	}
}
