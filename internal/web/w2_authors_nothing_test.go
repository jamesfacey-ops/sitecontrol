// w2_authors_nothing_test.go — the two executable proofs W2-7's walk
// forces into existence:
//
//   - TestAuthRoutesAuthorNothing — TP §4.8's "proof owed at build, named
//     now" for POST /login and POST /logout: login (success AND failure)
//     and logout leave the event_log's row count and bytes unchanged. Its
//     red source is mutant m17 (an authoring login handler — applied and
//     reverted at the gate); presence is not efficacy, so the red is
//     planned, not improvised.
//
//   - TestW2SyncMountAuthorsNothing — the /sync/ row's
//     whole-mounted-handler assertion: the ENTIRE handler behind the
//     "POST /sync/" mount (every path registered on the repl mux,
//     internal/repl/server.go:25–31, including POST /sync/poke) applies
//     and never authors — asserted behaviourally over the mounted handler,
//     never the one /sync/poke path, so a future route added to the repl
//     mux cannot ship unexamined behind the mount. (Rev 3: the
//     internal/repl source-scan alternative is STRUCK — engine.Author is a
//     legitimate engine facility and the scan shape would demand a CONTROL
//     row §5's enumeration does not carry; this behavioural assertion is
//     the sole permitted discharge.)
//
// (WINDOW-BUILD-PLAN-v1 §3 W2, criteria LOCKED 2026-08-28.)
package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/repl"
	"sitecontrol/internal/store"
)

// w2LogFingerprint captures the own-stream state as (watermark, ordered row
// hashes) so "unchanged" means BYTES unchanged, not merely count unchanged.
func w2LogFingerprint(t *testing.T, st *store.Store, siteID string, epoch int64) (int64, []string) {
	t.Helper()
	wm := w2Watermark(t, st, siteID, epoch)
	var hashes []string
	for _, e := range w2EventsFrom(t, st, siteID, epoch, 0) {
		hashes = append(hashes, e.Hash())
	}
	return wm, hashes
}

func w2AssertLogUnchanged(t *testing.T, st *store.Store, siteID string, epoch, wantWM int64, wantHashes []string, what string) {
	t.Helper()
	wm, hashes := w2LogFingerprint(t, st, siteID, epoch)
	if wm != wantWM {
		t.Fatalf("%s authored %d event(s) (watermark %d -> %d) — this route is classified AUTHORS-NOTHING; if it authors, the classification and everything resting on it is a lie (mutant m17)", what, wm-wantWM, wantWM, wm)
	}
	if len(hashes) != len(wantHashes) {
		t.Fatalf("%s changed the event_log row count (%d -> %d)", what, len(wantHashes), len(hashes))
	}
	for i := range hashes {
		if hashes[i] != wantHashes[i] {
			t.Fatalf("%s changed stored event bytes at seq %d (hash %s -> %s) — authors-nothing means BYTES unchanged, not count unchanged", what, i+1, wantHashes[i], hashes[i])
		}
	}
}

// TestAuthRoutesAuthorNothing — POST /login (success and failure) and
// POST /logout, event_log row count and bytes unchanged (TP §4.8's
// authors-nothing table, the owed proof named there verbatim).
func TestAuthRoutesAuthorNothing(t *testing.T) {
	srv, st, _ := newW2Server(t, w2Base-2*w2Hour)
	wm, hashes := w2LogFingerprint(t, st, "site-a", 1)

	// Login FAILURE: the wrong password.
	form := url.Values{"password": {"not-the-password"}, "next": {"/"}}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusSeeOther {
		t.Fatalf("fixture defect: the wrong password logged in (%d) — the failure arm is not a failure", rec.Code)
	}
	w2AssertLogUnchanged(t, st, "site-a", 1, wm, hashes, "POST /login (failure)")

	// Login SUCCESS: the real round trip.
	cookie := loginCookie(t, srv, testLoginPassword)
	w2AssertLogUnchanged(t, st, "site-a", 1, wm, hashes, "POST /login (success)")

	// Logout, with the live session.
	req = httptest.NewRequest("POST", "/logout", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	w2AssertLogUnchanged(t, st, "site-a", 1, wm, hashes, "POST /logout")
}

// TestW2SyncMountAuthorsNothing — the whole mounted handler: every path
// registered on the repl mux is driven THROUGH the web mux's "POST /sync/"
// mount (plus an unregistered probe path and the no-key refusals), and the
// own stream is byte-unchanged afterwards. The mount applies peers' rows
// via Store.Apply and never mints an envelope.
func TestW2SyncMountAuthorsNothing(t *testing.T) {
	srv, st, _ := newW2Server(t, w2Base-2*w2Hour)
	srv.Cfg.PeerKey = "w2-sync-key"
	eng := repl.New(srv.Cfg, st)
	t.Cleanup(eng.Shutdown)
	srv.Sync = eng.SyncHandler()

	wm, hashes := w2LogFingerprint(t, st, "site-a", 1)
	drive := func(method, path string, withKey bool) {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		if withKey {
			req.Header.Set("Authorization", "Bearer w2-sync-key")
		}
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		w2AssertLogUnchanged(t, st, "site-a", 1, wm, hashes, method+" "+path+map[bool]string{true: " (keyed)", false: " (no key)"}[withKey])
	}
	// Every path the repl mux registers (server.go:27–29), keyed and
	// unkeyed, through the real mount — plus a probe path no one
	// registered, because the mount forwards EVERYTHING under /sync/ and
	// the classification binds the whole handler, not its current routes.
	drive("POST", "/sync/poke", true)
	drive("POST", "/sync/poke", false)
	drive("GET", "/sync/streams", true)
	drive("GET", "/sync/pull?site_id=site-a&node_epoch=1&from=1", true)
	drive("POST", "/sync/streams", true)
	drive("POST", "/sync/pull", true)
	drive("POST", "/sync/zz-future-route-probe", true)
	drive("POST", "/sync/zz-future-route-probe", false)
}
