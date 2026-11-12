package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/repl"
	"sitecontrol/internal/store"
)

// TestNoProjectionDumpRouteExists pins D-56's removal: "this program will
// not ever have a data dump". GET /debug/projection-dump used to be served
// here, unauthenticated, outside cfg.DevRoutes, over every registered
// projection. It is gone, and the mux must not serve a dump on that path
// again — this test is what goes red if somebody re-registers it.
//
// ⚠️ IT IS DELIBERATELY NOT A STATUS-CODE ASSERTION, and must never become
// one. GET / is a Go 1.22 ServeMux CATCH-ALL (web.go), so an unregistered
// path answers 200 with the status page, not 404: "want 404" would pass
// whether the route existed or not, and "want 200" would pass either way
// too. The assertions are therefore on WHAT COMES BACK — the dump's own
// content-type, and whether the body decodes as the dump's shape carrying
// the registered projection names. Same discipline as
// TestNoOfficerPickerRouteExists.
//
// What was deleted with the route: the endpoint test that used to stand
// here, asserting this handler served the store's canonical JSON. (Its name
// is deliberately not repeated — citations_test.go rightly refuses a comment
// that names a Test… identifier which no longer exists, and this is exactly
// the channel it exists to keep honest.) That substrate guarantee is NOT lost — it is where it always belonged, on the store:
// internal/store/c2_test.go (byte-identical dumps across replays and across
// two independently-built stores) and internal/domain/determinism_test.go.
// cmd/dumpcompare now reads those bytes from a file produced by
// `sitectl -dump`, not from this mux.
func TestNoProjectionDumpRouteExists(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	// trip/visit are no longer self-registered by store.Open (C5: registration
	// moved to the composition root, domain.Register, which store cannot
	// import without a cycle). Local stub here per the same brief-sanctioned
	// fix used in internal/store/c2_test.go, keeping this web-layer test
	// independent of internal/domain.
	for _, p := range []store.Projection{
		{Name: "trip", Table: "trip", OrderBy: "trip_id"},
		{Name: "visit", Table: "visit", OrderBy: "visit_id"},
	} {
		if err := st.RegisterProjection(p); err != nil {
			t.Fatal(err)
		}
	}

	// ⚠️ DevRoutes: true is LOAD-BEARING here, and is the one setting this
	// test would be worthless without. dev_routes = true in all four shipped
	// deploy TOMLs, so a fixture with it off would prove absence only under a
	// configuration this program never runs — which is precisely the false
	// green the D-56 ERRATUM produced: the prescribed "gate it behind
	// cfg.DevRoutes" remedy took this assertion green in a DevRoutes-off
	// fixture while every deployed post still served the dump.
	cfg := &config.Config{SiteID: "site-a", DisplayName: "Test", ListenAddr: ":0", NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 10, DevRoutes: true, LoginPassword: "test"}
	srv := New(cfg, st)

	// Precondition, so the absence below means something: this store really
	// does have dumpable projections with rows in them. A store that could
	// not produce a dump would make every assertion here vacuous.
	if b, err := st.DumpProjections(); err != nil {
		t.Fatalf("test setup: the store cannot dump at all: %v", err)
	} else if !strings.Contains(string(b), `"trip"`) || !strings.Contains(string(b), `"visit"`) {
		t.Fatalf("test setup: the store's own dump does not carry the registered projections: %s", b)
	}

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/debug/projection-dump", nil))

	if ct := rec.Header().Get("Content-Type"); strings.HasPrefix(ct, "application/json") {
		t.Errorf("GET /debug/projection-dump answered content-type %q — the dump handler is registered again (D-56: it must not be)", ct)
	}
	var dump map[string][]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &dump); err == nil && len(dump) > 0 {
		t.Errorf("GET /debug/projection-dump returned a decodable projection dump with %d projections — the route is back:\n%.400s",
			len(dump), rec.Body.String())
	}
}

// Regression: the fully-wired Handler (Sync + SyncStatus set, as main.go
// always sets them) must construct and route without panicking. Before this
// test existed, "/sync/" (all methods) conflicted with "GET /" under Go 1.22
// ServeMux precedence and the binary PANICKED AT STARTUP — every suite stayed
// green because no test had ever wired Sync. Found by C7's E2E rehearsal.
func TestHandlerBootsWithSyncWiredAndRoutesSyncTraffic(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, p := range []store.Projection{
		{Name: "trip", Table: "trip", OrderBy: "trip_id"},
		{Name: "visit", Table: "visit", OrderBy: "visit_id"},
	} {
		if err := st.RegisterProjection(p); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{SiteID: "site-a", DisplayName: "Test", ListenAddr: ":0", NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 10, LoginPassword: "test"}
	srv := New(cfg, st)
	marker := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("sync:" + r.Method + " " + r.URL.Path)) })
	srv.Sync = marker
	srv.SyncStatus = marker
	// C10: main.go also wires SyncSnapshot alongside Sync/SyncStatus — extend
	// this regression test to the full set main.go actually wires (row 22's
	// instruction), so a future interaction bug between the C10 render and
	// the /sync/ mount would show up here too, not just in isolation.
	srv.SyncSnapshot = func() repl.Snapshot {
		return repl.Snapshot{SiteID: "site-a", Peers: []repl.PeerSnapshot{{SiteID: "site-b"}}}
	}

	h := srv.Handler() // panicked here before the method-split registration

	for _, tc := range []struct{ method, path, want string }{
		{"GET", "/sync/streams", "sync:GET /sync/streams"},
		{"GET", "/sync/pull", "sync:GET /sync/pull"},
		{"POST", "/sync/poke", "sync:POST /sync/poke"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != 200 || rec.Body.String() != tc.want {
			t.Fatalf("%s %s = %d %q, want 200 %q", tc.method, tc.path, rec.Code, rec.Body.String(), tc.want)
		}
	}
	// And the root page still answers, WITH the C10 sync-status section
	// present (not just a 200 — the section must actually render).
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/status", nil))
	if rec.Code != 200 {
		t.Fatalf("GET /status = %d after sync mounting", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "site-b") {
		t.Fatalf("GET /status did not render the C10 sync-status section once SyncSnapshot was wired; body:\n%s", body)
	}

	// C11: the login/logout/officer-picker routes register alongside the
	// rest without a ServeMux conflict either (same regression class row 22
	// warns about — extend this test to new routes rather than trusting a
	// clean build). GET /login must render; the gated /sync/ traffic above
	// already proves method-split registration works, so this only needs to
	// confirm C11's OWN routes are reachable in the fully-wired mux.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/login", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `action="/login"`) {
		t.Fatalf("GET /login in the fully-wired handler = %d, body:\n%s", rec.Code, rec.Body.String())
	}
}
