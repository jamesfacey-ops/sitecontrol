// main_test.go — cmd/sitectl's FIRST tests. Until this file `go list ./...`
// reported `sitecontrol/cmd/sitectl  [no test files]`: the composition root
// that decides whether a post refuses to start, what an operator's `-dump`
// hands to the two-post determinism check, and whether recovery finishes
// before anything is served, had NOTHING that could go red (S124 row F).
//
// ⭐ THE SUCCESS TEST THIS FILE IS WRITTEN AGAINST: a deliberately broken
// sitectl must make something here FAIL. Every test below was proven red by
// breaking the thing it covers — the mutation for each is named in its own
// comment and the run is recorded in reports/window-build/S124-TOOLING.md §F3.
//
// ⚠ WHY THESE ARE SUBPROCESS TESTS AND NOT UNIT TESTS. main.go is 169 lines
// of ONE func main(). Nothing in it is exported, nothing is injectable, and
// four of its behaviours (the closures wired onto alerts) are anonymous
// funcs that no test can name. The parts that CAN be reached are reached the
// way an operator reaches them: build the real binary and run it. That is
// also the only way to observe the two behaviours that matter most — an exit
// code, and whether a port was ever bound.
//
// ⚠ THIS FILE DELIBERATELY DOES NOT TEST: the alerts.Staleness closure
// (⟦was: "/ CurrentPhase / PhaseDisplayName / TravelMinutes closures" — the
// weather-phase wiring, removed James 2026-10-04⟧), and the
// domain.Register / deadline.Register failure arms. Reaching them requires
// either extracting them from main() or injecting a duplicate registration —
// both are PRODUCTION changes, and S124 row F is tests only. The gap is
// stated in the report rather than papered over with a test of something
// else that happens to be nearby.
package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// --------------------------------------------------------------------------
// harness
// --------------------------------------------------------------------------

// moduleRoot walks up to the directory holding go.mod. Same shape as
// internal/store's w10DcModuleRoot — the test binary's cwd is the package
// dir, and `go build ./cmd/sitectl` needs the module root.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %q", dir)
		}
		dir = parent
	}
}

// goTool finds the go binary. GOROOT/bin/go first (the toolchain that is
// running this test), then PATH, then the location this project's own briefs
// name. It FAILS rather than skips when none answers: an unrunnable control
// that reports ok is the defect this row exists to repair.
func goTool(t *testing.T) string {
	t.Helper()
	cands := []string{filepath.Join(runtime.GOROOT(), "bin", "go"), "/usr/local/go/bin/go"}
	if p, err := exec.LookPath("go"); err == nil {
		cands = append([]string{p}, cands...)
	}
	for _, c := range cands {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	t.Fatalf("no go toolchain found (tried %v) — this check FAILS rather than skips", cands)
	return ""
}

var (
	sitectlOnce sync.Once
	sitectlPath string
	sitectlErr  error
)

// sitectlBinary builds the REAL binary once per test run.
func sitectlBinary(t *testing.T) string {
	t.Helper()
	sitectlOnce.Do(func() {
		root := moduleRoot(t)
		out := filepath.Join(os.TempDir(), fmt.Sprintf("s124-sitectl-%d", os.Getpid()))
		if runtime.GOOS == "windows" {
			out += ".exe"
		}
		cmd := exec.Command(goTool(t), "build", "-o", out, "./cmd/sitectl")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			sitectlErr = fmt.Errorf("%v\n%s", err, stderr.String())
			return
		}
		sitectlPath = out
	})
	if sitectlErr != nil || sitectlPath == "" {
		t.Fatalf("could not build cmd/sitectl (%v) — this check FAILS rather than skips: "+
			"a control that cannot run must not report ok", sitectlErr)
	}
	return sitectlPath
}

// writeConfig writes a minimal VALID per-site config. Every field
// config.validate() requires is present, so a failure in any test below is
// about sitectl and not about the fixture.
func writeConfig(t *testing.T, dir, listenAddr, dbPath string, peers map[string]string) string {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "site_id = %q\n", "site-a")
	fmt.Fprintf(&b, "display_name = %q\n", "Site A")
	fmt.Fprintf(&b, "listen_addr = %q\n", listenAddr)
	fmt.Fprintf(&b, "node_epoch = 1\n")
	fmt.Fprintf(&b, "peer_key = %q\n", "demo-key")
	fmt.Fprintf(&b, "db_path = %q\n", dbPath)
	// Long on purpose: the routine sync ticker must not fire during a test.
	fmt.Fprintf(&b, "sync_interval_seconds = 3600\n")
	fmt.Fprintf(&b, "muster_min_group_size = 2\n")
	fmt.Fprintf(&b, "login_password = %q\n", "demo-pass")
	fmt.Fprintf(&b, "orientation = false\n")            // D-88: required, no default; the harness posts are road posts
	fmt.Fprintf(&b, "site_type = %q\n", "text_message") // R-4/R-5: required, no default; the harness posts sign people in per visit
	for id, url := range peers {
		fmt.Fprintf(&b, "\n[[peers]]\nsite_id = %q\nbase_url = %q\nkey = %q\n", id, url, "demo-key")
	}
	p := filepath.Join(dir, "site.toml")
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	return p
}

// runSitectl runs the binary to COMPLETION. Only for the refusal paths and
// -dump; serve mode never returns, so it uses startSitectl instead.
//
// ⚠ The context bound is not decoration. A mutation that turns a refusal
// into a server would otherwise hang until the package timeout and report a
// SILENT mode — no `--- FAIL` line at all. This makes it a named failure.
func runSitectl(t *testing.T, args ...string) (code int, stdout, stderr []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, sitectlBinary(t), args...)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("sitectl %v did not exit within 60s — it is SERVING where it should have exited.\nstderr:\n%s", args, se.String())
	}
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running sitectl %v: %v\nstderr: %s", args, err, se.String())
	}
	return code, so.Bytes(), se.Bytes()
}

// lockedBuf lets a test read a running process's stderr.
type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// startSitectl launches serve mode and guarantees the process is killed.
func startSitectl(t *testing.T, args ...string) *lockedBuf {
	t.Helper()
	cmd := exec.Command(sitectlBinary(t), args...)
	out := &lockedBuf{}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting sitectl: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	return out
}

// freeAddr returns a 127.0.0.1 address nothing is listening on.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a port: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("releasing reserved port: %v", err)
	}
	return addr
}

// heldAddr returns an address the TEST holds open for its whole duration.
// Anything that tries to bind it fails with "address already in use".
func heldAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("holding a port: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l.Addr().String()
}

// seedStore creates the site database and puts REAL rows in it, so a dump
// over it has content. An empty dump would compare equal to an empty dump
// however broken the projection set was.
func seedStore(t *testing.T, dbPath string) {
	t.Helper()
	st := openRegistered(t, dbPath)
	defer st.Close()
	c, err := domain.CreateCompany(st, "site-a", 1, "Acme Haulage", 0, "default")
	if err != nil {
		t.Fatalf("seeding company: %v", err)
	}
	if _, err := domain.CreatePerson(st, "site-a", 1, "A Driver", &c.CompanyID, nil, 0, "default"); err != nil {
		t.Fatalf("seeding person: %v", err)
	}
}

// openRegistered opens the store and performs EXACTLY the two registrations
// main.go performs, in main.go's order — the reference the dump is compared
// against.
func openRegistered(t *testing.T, dbPath string) *store.Store {
	t.Helper()
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open(%s): %v", dbPath, err)
	}
	if err := domain.Register(st); err != nil {
		st.Close()
		t.Fatalf("domain.Register: %v", err)
	}
	if err := deadline.Register(st); err != nil {
		st.Close()
		t.Fatalf("deadline.Register: %v", err)
	}
	return st
}

// --------------------------------------------------------------------------
// A — the refusals. A post that starts on a config or a database it should
// have refused writes real records under the wrong identity or the wrong
// schema, and nothing downstream can tell.
// --------------------------------------------------------------------------

// PROVEN RED BY: deleting the `if *configPath == ""` guard from main.go.
// Without it the binary still exits non-zero (config.Load fails on "") but
// says `config: open : no such file or directory`, which sends an operator
// hunting a missing FILE rather than a missing FLAG.
func TestNoConfigFlagRefusesToStartAndNamesTheFlag(t *testing.T) {
	code, stdout, stderr := runSitectl(t)
	if code == 0 {
		t.Fatalf("sitectl with no -config EXITED 0. One binary, config-only differentiation: a "+
			"default config is a post running under an identity nobody chose.\nstdout: %q\nstderr: %q", stdout, stderr)
	}
	if !strings.Contains(string(stderr), "-config is required") {
		t.Fatalf("refusal does not name the flag. An operator reads this line at 3am on a post that "+
			"will not start.\ngot stderr: %q", stderr)
	}
	if len(stdout) != 0 {
		t.Fatalf("a refused start wrote %d bytes to STDOUT: %q — stdout is the dump channel and "+
			"must carry nothing else", len(stdout), stdout)
	}
}

// PROVEN RED BY: replacing main.go's `log.Fatalf("sitectl: %v", err)` after
// config.Load with `log.Printf(...); os.Exit(0)` — the refusal demoted to a
// warning. Exit 0 on an unreadable config is a post the deploy believes came
// up.
func TestUnreadableConfigIsFatalAndNamesThePath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-there.toml")
	code, _, stderr := runSitectl(t, "-config", missing)
	if code == 0 {
		t.Fatalf("sitectl EXITED 0 on a config file that does not exist (%s)", missing)
	}
	if !strings.Contains(string(stderr), missing) {
		t.Fatalf("refusal does not name the path it could not read.\ngot stderr: %q", stderr)
	}
}

// PROVEN RED BY: the same demotion applied to the store.Open arm.
//
// The database is planted at schema_version SchemaVersion-1 against a binary
// that requires store.SchemaVersion (11 since the 10 -> 11 increment). This is the realistic shape — an old post, a new
// database, or the reverse after a partial rollout — and the whole §8.5
// freeze rests on the binary REFUSING rather than folding v10 events into a
// v9 shape.
func TestDatabaseAtTheWrongSchemaVersionIsFatal(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "site-a.db")
	seedStore(t, dbPath)

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("opening the planted database: %v", err)
	}
	if _, err := db.Exec(`UPDATE meta SET value = ? WHERE key = 'schema_version'`,
		fmt.Sprint(store.SchemaVersion-1)); err != nil {
		db.Close()
		t.Fatalf("planting the wrong schema_version: %v", err)
	}
	db.Close()

	cfg := writeConfig(t, dir, heldAddr(t), dbPath, nil)
	code, stdout, stderr := runSitectl(t, "-config", cfg)
	if code == 0 {
		t.Fatalf("sitectl EXITED 0 against a database at schema_version %d while requiring %d",
			store.SchemaVersion-1, store.SchemaVersion)
	}
	if !strings.Contains(string(stderr), "schema_version") {
		t.Fatalf("the refusal does not say WHICH refusal it was.\ngot stderr: %q", stderr)
	}
	if len(stdout) != 0 {
		t.Fatalf("a refused start wrote to STDOUT: %q", stdout)
	}
}

// --------------------------------------------------------------------------
// B — -dump. Since D-56 removed GET /debug/projection-dump this is the ONLY
// read path for the §13.3 step-6 two-post determinism check. A column
// missing, filtered or reordered here is a cross-site divergence that
// becomes invisible to the only check that would have caught it.
// --------------------------------------------------------------------------

// PROVEN RED BY (two mutations, both real-world shapes):
//  1. moving main.go's `if *dump { … }` block ABOVE `deadline.Register(st)` —
//     the dump then lacks alert_ack and alert_ack_retraction and the byte
//     comparison fails, naming the two missing keys;
//  2. deleting one key from the dump before writing it to stdout (the
//     "surely nobody needs person rows in a dump" change) — same failure.
func TestDumpIsExactlyTheLibraryBytesOverBothRegistrations(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "site-a.db")
	seedStore(t, dbPath)
	cfg := writeConfig(t, dir, heldAddr(t), dbPath, nil)

	code, stdout, stderr := runSitectl(t, "-config", cfg, "-dump")
	if code != 0 {
		t.Fatalf("sitectl -dump exited %d\nstderr: %s", code, stderr)
	}

	st := openRegistered(t, dbPath)
	want, err := st.DumpProjections()
	st.Close()
	if err != nil {
		t.Fatalf("reference DumpProjections: %v", err)
	}

	// Names first: a byte diff over 23 projections is unreadable, and the
	// registration-order defect this guards against is a MISSING NAME.
	var got, ref map[string]json.RawMessage
	if err := json.Unmarshal(stdout, &got); err != nil {
		t.Fatalf("sitectl -dump did not emit a JSON object (%v). dumpcompare parses these bytes; "+
			"anything else fails the §13.3 assert at step 6.\ngot: %q", err, stdout)
	}
	if err := json.Unmarshal(want, &ref); err != nil {
		t.Fatalf("reference dump did not parse: %v", err)
	}
	for name := range ref {
		if _, ok := got[name]; !ok {
			t.Errorf("projection %q is registered but ABSENT from `sitectl -dump`. A projection "+
				"missing from the dump can never be compared between two posts, and dumpcompare "+
				"reports it as a warning while still exiting 0", name)
		}
	}
	for name := range got {
		if _, ok := ref[name]; !ok {
			t.Errorf("`sitectl -dump` emitted %q, which the two Register calls do not register", name)
		}
	}
	// An independent statement of the same thing, so this test is not purely
	// a mirror of the code it checks: these names must be in EVERY dump.
	// deadline's two are the ones a dump taken before deadline.Register loses.
	for _, name := range []string{
		"company", "person", "trade", "trip", "trip_state", "arrival_state",
		"visit", "visit_checkout", "vehicle",
		"alert_ack", "alert_ack_retraction",
	} { // ⟦was: also "weather_phase" — weather phases removed, James 2026-10-04⟧
		if _, ok := got[name]; !ok {
			t.Errorf("`sitectl -dump` has no %q key", name)
		}
	}
	if t.Failed() {
		return
	}

	if !bytes.Equal(stdout, want) {
		t.Fatalf("`sitectl -dump` bytes differ from store.DumpProjections over the same database "+
			"with the same registrations. Nothing may be filtered, redacted or reordered on the "+
			"way out (main.go's own contract).\n got %d bytes\nwant %d bytes", len(stdout), len(want))
	}

	// The seeded rows must actually be in there: an empty dump compares equal
	// to an empty dump however broken the projection set is.
	if !strings.Contains(string(stdout), "Acme Haulage") {
		t.Fatalf("the dump does not contain the seeded company — it is empty or filtered")
	}
}

// PROVEN RED BY: deleting the `return` at the end of main.go's -dump block,
// so the dump falls through into serve mode.
//
// The listen address in this test is held open BY THE TEST. Serve mode there
// cannot start (ListenAndServe fails, log.Fatalf, exit 1). So an exit 0 with
// the full dump on stdout is PROOF the port was never bound — D-56's "costs a
// network caller everything" is a property of this control flow, not of a
// comment.
func TestDumpModeNeverBindsTheListenAddress(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "site-a.db")
	seedStore(t, dbPath)
	held := heldAddr(t)
	cfg := writeConfig(t, dir, held, dbPath, nil)

	code, stdout, stderr := runSitectl(t, "-config", cfg, "-dump")
	if code != 0 {
		t.Fatalf("sitectl -dump exited %d on a listen address held by this test (%s) — it TRIED TO "+
			"SERVE. -dump must exit without serving anything.\nstderr: %s", code, held, stderr)
	}
	if !json.Valid(stdout) {
		t.Fatalf("-dump did not emit valid JSON: %q", stdout)
	}
	if strings.Contains(string(stderr), "listening on") {
		t.Fatalf("-dump logged the serve-mode startup line: %q", stderr)
	}
}

// --------------------------------------------------------------------------
// C — serve mode. The wiring at the composition root, observed through the
// routes it does and does not produce.
// --------------------------------------------------------------------------

// waitServing polls until addr answers HTTP, or fails loudly.
func waitServing(t *testing.T, addr string, out *lockedBuf) {
	t.Helper()
	stopAt := time.Now().Add(30 * time.Second)
	for time.Now().Before(stopAt) {
		resp, err := http.Get("http://" + addr + "/")
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("sitectl never answered on %s within 30s.\noutput:\n%s", addr, out.String())
}

func get(t *testing.T, addr, path string) (code int, ctype, body string) {
	t.Helper()
	req, err := http.NewRequest("GET", "http://"+addr+path, nil)
	if err != nil {
		t.Fatalf("building request for %s: %v", path, err)
	}
	// Never follow redirects: /login vs / is a status, not a destination.
	c := &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return resp.StatusCode, resp.Header.Get("Content-Type"), string(b)
}

func getAuth(t *testing.T, addr, path, key string) (code int, ctype, body string) {
	t.Helper()
	req, err := http.NewRequest("GET", "http://"+addr+path, nil)
	if err != nil {
		t.Fatalf("building request for %s: %v", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	c := &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return resp.StatusCode, resp.Header.Get("Content-Type"), string(b)
}

func status(t *testing.T, addr, path string) int {
	t.Helper()
	code, _, _ := get(t, addr, path)
	return code
}

// PROVEN RED, each clause by its own mutation:
//   - `srv.Handler()` replaced with an empty http.NewServeMux() → GET /
//     becomes 404;
//   - `srv.SyncStatus = engine.StatusHandler()` deleted → /debug/sync-status
//     404s (web.go registers it only when the field is non-nil);
//   - `srv.Sync = engine.SyncHandler()` deleted → /sync/streams 404s;
//   - the startup log line's text changed → the log assertion fails.
//
// ⚠ THE 404 ON /debug/projection-dump IS NOT ASSERTED ALONE, and that is
// deliberate. A binary that serves NOTHING also 404s that path, so on its own
// the D-56 clause is satisfiable by total breakage. The three positive
// controls above it are what make the 404 mean "this route is gone" rather
// than "nothing is here".
func TestServeModeWiresTheRoutesAndTheDumpRouteIsGone(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "site-a.db")
	seedStore(t, dbPath)
	addr := freeAddr(t)
	cfg := writeConfig(t, dir, addr, dbPath, nil)

	out := startSitectl(t, "-config", cfg)
	waitServing(t, addr, out)

	if got := status(t, addr, "/"); got == http.StatusNotFound {
		t.Errorf("GET / is 404 — the site's own handler is not wired")
	}
	// ⚠⚠ THESE TWO WERE `!= 404` IN THE FIRST DRAFT AND BOTH WERE VACUOUS.
	// Deleting `srv.SyncStatus = engine.StatusHandler()` and deleting
	// `srv.Sync = engine.SyncHandler()` BOTH LEFT THIS TEST GREEN (S124 row F
	// mutations M6b/M6c): web.go's `GET /` is a catch-all, so an unwired
	// /debug/sync-status answers 200 with the STATUS PAGE, not 404. The
	// wiring has to be proven by what the route RETURNS.
	if code, ctype, body := get(t, addr, "/debug/sync-status"); !strings.Contains(strings.ToLower(ctype), "json") || !json.Valid([]byte(body)) {
		t.Errorf("GET /debug/sync-status answered %d %q with a non-JSON body — srv.SyncStatus is not "+
			"wired to the replication engine, so the engine's per-peer state is invisible to an "+
			"operator and to C10's status page, and the route silently serves the status page "+
			"instead.\nbody: %.200s", code, ctype, body)
	}
	// Unauthenticated: the engine's own key gate must answer, which is only
	// reachable through srv.Sync's mux. The catch-all cannot produce a 401.
	if code, _, body := get(t, addr, "/sync/streams"); code != http.StatusUnauthorized {
		t.Errorf("GET /sync/streams unauthenticated answered %d, want 401 — srv.Sync is not wired, "+
			"so no peer can ever pull this site's log and replication is silently dead.\nbody: %.200s",
			code, body)
	}
	// Authenticated: and it really serves the stream list, so the 401 above
	// is a gate in front of something rather than the whole story.
	if code, ctype, body := getAuth(t, addr, "/sync/streams?site=site-a", "demo-key"); code != http.StatusOK ||
		!strings.Contains(strings.ToLower(ctype), "json") {
		t.Errorf("GET /sync/streams with this site's peer key answered %d %q — the sync mount is not "+
			"serving.\nbody: %.200s", code, ctype, body)
	}
	// ⚠ THE D-56 CLAUSE IS NOT "IT 404s", AND THE FIRST DRAFT OF THIS TEST
	// ASSERTED THAT AND WENT RED FOR THE WRONG REASON. web.go registers
	// `GET /` as a catch-all (handleStatus), so /debug/projection-dump
	// answers 200 with the STATUS PAGE whether or not the dump route exists.
	// A 404 assertion here would be testing Go's mux, not D-56. What D-56
	// forbids is the BYTES: no unauthenticated caller may pull the
	// projections over HTTP. So the check is on the body — and it is anchored
	// on a row this test seeded itself, which is why seedStore runs above.
	code, ctype, body := get(t, addr, "/debug/projection-dump")
	if strings.Contains(strings.ToLower(ctype), "json") {
		t.Errorf("GET /debug/projection-dump answered %d with content-type %q — the dump route is "+
			"registered again (D-56)", code, ctype)
	}
	if strings.Contains(body, "Acme Haulage") {
		t.Errorf("GET /debug/projection-dump served this site's company projection to an " +
			"unauthenticated caller. D-56 removed exactly this: a verification instrument that was " +
			"also an operational data-dump surface. `sitectl -dump` is the replacement and it costs " +
			"a shell on the post")
	}
	if strings.Contains(body, `"trip_state"`) || strings.Contains(body, `"alert_ack"`) {
		t.Errorf("GET /debug/projection-dump answered with projection keys in the body (D-56)")
	}

	log := out.String()
	for _, want := range []string{"site=site-a", "epoch=1", "listening on " + addr, "sync every"} {
		if !strings.Contains(log, want) {
			t.Errorf("the startup line does not carry %q — it is the only record of which config a "+
				"running post actually loaded.\ngot:\n%s", want, log)
		}
	}
}

// PROVEN RED BY: changing `engine.Rejoin(context.Background())` to
// `go engine.Rejoin(context.Background())` in main.go.
//
// §8.6 requires this node's own prior-epoch history to be recovered BEFORE
// any peer log, and main.go says the synchronous call "is the point, not an
// optimisation": starting Run first would make own-history-first merely
// eventual rather than observed. Nothing else in the tree observes that
// ordering.
//
// The test makes it observable without timing luck. A peer is stood up whose
// first request BLOCKS until this test releases it. The sequence asserted is:
//
//	(1) the peer WAS contacted        → main really reached Rejoin (no vacuous pass)
//	(2) while it is blocked, the site's own port REFUSES connections
//	(3) once released, the site serves
//
// Step (1) is what stops this test passing for the wrong reason: without it,
// "the port is not open yet" would also be satisfied by a process that had
// crashed or was still loading its config.
func TestRejoinFinishesBeforeAnythingIsServed(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "site-a.db")
	seedStore(t, dbPath)

	contacted := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	peer := &http.Server{}
	pl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("peer listener: %v", err)
	}
	peer.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() {
			close(contacted)
			<-release
		})
		http.Error(w, "peer is not answering the sync protocol in this test", http.StatusNotFound)
	})
	go peer.Serve(pl)
	t.Cleanup(func() { _ = peer.Close() })

	addr := freeAddr(t)
	cfg := writeConfig(t, dir, addr, dbPath, map[string]string{
		"site-b": "http://" + pl.Addr().String(),
	})
	out := startSitectl(t, "-config", cfg)

	select {
	case <-contacted:
	case <-time.After(60 * time.Second):
		t.Fatalf("the peer was never contacted within 60s, so this test cannot say anything about "+
			"ordering. sitectl output:\n%s", out.String())
	}

	// Rejoin is now blocked inside drainSite. Nothing may be served yet.
	if c, err := net.DialTimeout("tcp", addr, 2*time.Second); err == nil {
		c.Close()
		close(release)
		t.Fatalf("%s accepted a connection WHILE boot-time rejoin was still in progress. §8.6's "+
			"own-history-first recovery is supposed to complete before this post serves anything; "+
			"a post that answers mid-replay serves state it has not finished recovering", addr)
	}
	close(release)

	// And it does come up afterwards — otherwise "refuses connections" above
	// would be satisfied by a binary that never serves at all.
	waitServing(t, addr, out)
}

// --------------------------------------------------------------------------
// D — -report visits: the analytics MVP's one operator door.
// --------------------------------------------------------------------------

// seedVisitStore seeds one completed visit (check-in at the gate, then an
// explicit sign-out) so the report has exactly one closed row to show.
func seedVisitStore(t *testing.T, dbPath string) {
	t.Helper()
	st := openRegistered(t, dbPath)
	defer st.Close()
	c, err := domain.CreateCompany(st, "site-a", 1, "Acme Haulage", 0, "default")
	if err != nil {
		t.Fatalf("seeding company: %v", err)
	}
	p, err := domain.CreatePerson(st, "site-a", 1, "A Driver", &c.CompanyID, nil, 0, "default")
	if err != nil {
		t.Fatalf("seeding person: %v", err)
	}
	number := "TRUCK 1"
	v, err := domain.CreateVehicle(st, "site-a", 1, &number, nil, 0, "default")
	if err != nil {
		t.Fatalf("seeding vehicle: %v", err)
	}
	trip, err := domain.DispatchLocalCheckIn(st, "site-a", 1, domain.DispatchInput{VehicleID: v.VehicleID, DriverPersonID: p.PersonID}, 0, "default")
	if err != nil {
		t.Fatalf("seeding check-in: %v", err)
	}
	if _, err := domain.RecordExpectedArrival(st, "site-a", 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatalf("seeding arrival: %v", err)
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil || len(open) != 1 {
		t.Fatalf("seeding: want one open visit, got %d (%v)", len(open), err)
	}
	if err := domain.CheckOutVisit(st, "site-a", 1, open[0].VisitID, nil, 0, "default"); err != nil {
		t.Fatalf("seeding check-out: %v", err)
	}
}

// PROVEN RED BY: deleting the `return` at the end of main.go's -report
// block (falls through to serve mode on a held port → exit 1), or by
// removing the block (the flag is then unknown → exit 2).
//
// The listen address is held BY THE TEST, exactly as the -dump test does it,
// so exit 0 with CSV on stdout is proof the port was never bound.
func TestReportVisitsEmitsCSVAndNeverBindsTheListenAddress(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "site-a.db")
	seedVisitStore(t, dbPath)
	held := heldAddr(t)
	cfg := writeConfig(t, dir, held, dbPath, nil)

	code, stdout, stderr := runSitectl(t, "-config", cfg, "-report", "visits")
	if code != 0 {
		t.Fatalf("sitectl -report visits exited %d on a listen address held by this test (%s) — it TRIED TO "+
			"SERVE. -report must exit without serving anything.\nstderr: %s", code, held, stderr)
	}
	if strings.Contains(string(stderr), "listening on") {
		t.Fatalf("-report logged the serve-mode startup line: %q", stderr)
	}
	lines := strings.Split(strings.TrimSpace(string(stdout)), "\n")
	if len(lines) != 2 {
		t.Fatalf("want header + 1 row on stdout, got %d lines:\n%s", len(lines), stdout)
	}
	if got, want := strings.TrimRight(lines[0], "\r"), strings.Join(domain.VisitReportColumns, ","); got != want {
		t.Fatalf("header:\n got %q\nwant %q", got, want)
	}
	if !strings.Contains(lines[1], ",checked_out,yes,") || !strings.Contains(lines[1], ",A Driver,") {
		t.Fatalf("row does not show the seeded visit as an explicit sign-out: %q", lines[1])
	}
	// The denominator lives on stderr so stdout stays pure CSV.
	for _, want := range []string{"visits examined: 1 (", "checked_out 1,", "coverage: site-a@1 applied through seq"} {
		if !strings.Contains(string(stderr), want) {
			t.Fatalf("stderr lacks the denominator line %q:\n%s", want, stderr)
		}
	}
	if strings.Contains(string(stdout), "visits examined") {
		t.Fatalf("the denominator leaked onto stdout, which must be pure CSV:\n%s", stdout)
	}
}

// PROVEN RED BY: dropping the name check — an unknown report would then
// fall through to serve mode.
func TestReportRefusesAnUnknownNameAndNamesTheOneReport(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "site-a.db")
	seedStore(t, dbPath)
	cfg := writeConfig(t, dir, heldAddr(t), dbPath, nil)

	code, stdout, stderr := runSitectl(t, "-config", cfg, "-report", "nosuch")
	if code == 0 {
		t.Fatalf("sitectl -report nosuch exited 0; stdout: %q", stdout)
	}
	if !strings.Contains(string(stderr), `"nosuch"`) || !strings.Contains(string(stderr), "'visits'") {
		t.Fatalf("refusal must name the bad report and the one that exists: %q", stderr)
	}
	if len(bytes.TrimSpace(stdout)) != 0 {
		t.Fatalf("a refused report must write nothing to stdout: %q", stdout)
	}
}
