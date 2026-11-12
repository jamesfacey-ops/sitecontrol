// C10 done-when test: "sever (the C4 script) → both sites' status visibly
// ages" (criterion 5's degradation clause).
//
// This is a LOCAL PROXY for the real rehearsal, not a run of
// deploy/sever.ps1 against the Proxmox cluster — the brief is explicit that
// C10 proves degradation locally and the team lead arranges the real
// sever/restore rehearsal separately. The proxy: a REAL repl.Engine talking
// to a REAL peer over a REAL httptest.Server, then closing that server mid-
// test. From the pulling engine's point of view this is functionally the
// same failure sever.ps1 produces (every outbound exchange starts failing
// with a connection error) — sever.ps1's iptables DROP and a closed listener
// both turn "the peer answers" into "the peer does not answer"; nothing in
// engine.go/syncstatus.go can tell the two apart, which is exactly why this// is a faithful proxy for what THIS chunk (the rendering) needs to prove.
//
// Uses only repl/store/config/domain's PUBLIC APIs — no import of repl's
// unexported test helpers (different package; BUILDER_BRIEF_RULES #15/#17:
// stay inside this chunk's own files).
package web

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/repl"
	"sitecontrol/internal/store"
)

func TestStatusPageVisiblyAgesAndDegradesWhenAPeerIsSeveredLocally(t *testing.T) {
	// --- Site A: a real engine serving its own log over a real listener. ---
	stA, err := store.Open(filepath.Join(t.TempDir(), "site-a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stA.Close() })
	if err := domain.Register(stA); err != nil {
		t.Fatal(err)
	}
	cfgA := &config.Config{SiteID: "site-a", DisplayName: "Site A", ListenAddr: ":0",
		NodeEpoch: 1, PeerKey: "key-a", SyncIntervalSeconds: 3600, DBPath: stA.Path}
	engA := repl.New(cfgA, stA)
	srvA := httptest.NewServer(engA.SyncHandler())
	// Reaped in a guaranteed step (brief's DO NOT: "start one, reap it in a
	// guaranteed step, not best-effort") — deferred here, and ALSO closed
	// explicitly mid-test below to simulate the sever; Close is idempotent-
	// safe to call twice on httptest.Server (the second Close is a no-op).
	defer srvA.Close()

	if _, err := engA.Author(stampTick("a-1"), 0, "default"); err != nil {
		t.Fatal(err)
	}

	// --- Site B: a real engine configured to pull A, over the real listener. ---
	stB, err := store.Open(filepath.Join(t.TempDir(), "site-b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stB.Close() })
	if err := domain.Register(stB); err != nil {
		t.Fatal(err)
	}
	cfgB := &config.Config{SiteID: "site-b", DisplayName: "Site B", ListenAddr: ":0",
		NodeEpoch: 1, PeerKey: "key-b", SyncIntervalSeconds: 3600, DBPath: stB.Path,
		Peers: []config.Peer{{SiteID: "site-a", BaseURL: srvA.URL, Key: "key-a"}}}
	engB := repl.New(cfgB, stB)
	t.Cleanup(engB.Shutdown) // guaranteed reap of B's own async work, not best-effort

	webB := New(cfgB, stB)
	webB.SyncSnapshot = engB.Snapshot

	// --- Phase 1: link up. B pulls A successfully. ---
	if err := engB.PullPeer(context.Background(), "site-a"); err != nil {
		t.Fatalf("precondition pull failed: %v", err)
	}
	body := getStatusPage(t, webB)
	if !strings.Contains(body, `data-health="ok"`) {
		t.Fatalf("phase 1 (link up): expected a peer box rendered healthy (data-health=\"ok\"); body:\n%s", body)
	}
	if strings.Contains(body, "last error") {
		t.Fatalf("phase 1 (link up): unexpected error surfaced; body:\n%s", body)
	}

	// Capture B's contact age now, then sleep briefly so the NEXT snapshot's
	// age has strictly grown — proving the age is live, render-time
	// arithmetic (store.go's "no wall-clock column" design), not a value
	// frozen at last-pull time. Short and jitter-tolerant: this only needs
	// "grew", not an exact value, so it isn't the flaky fixed-port timing
	// trap BUILDER_BRIEF_RULES #13 warns about (that one is a port-rebind
	// race between two SEPARATE processes in internal/repl; this is a single
	// process reading the wall clock twice).
	time.Sleep(50 * time.Millisecond)
	agedBody := getStatusPage(t, webB)
	beforeAge := ageFromBody(t, body)
	afterAge := ageFromBody(t, agedBody)
	if !(afterAge >= beforeAge) {
		t.Fatalf("status did not visibly age while the link was healthy: before=%v after=%v", beforeAge, afterAge)
	}

	// --- Phase 2: SEVER — close A's listener. Every subsequent exchange
	// from B fails exactly as it would under sever.ps1's iptables DROP. ---
	srvA.Close()

	// Prove the fixture CAN still fool a naive stream-only check: B's
	// cached StreamStatus for (site-a, epoch 1) is still literally "ok"
	// from phase 1 — nothing has overwritten it yet, because the failure
	// happens at /sync/streams, before any new StreamStatus is written
	// (engine.go's drainSite: notePeerError on failure, no setStream call).
	preSeverSnap := engB.Snapshot()
	sawCachedOK := false
	for _, s := range preSeverSnap.Streams {
		if s.SiteID == "site-a" && s.State == repl.StateOK {
			sawCachedOK = true
		}
	}
	if !sawCachedOK {
		t.Fatal("test setup: expected a cached StateOK stream to still be present immediately after severing, to prove the render doesn't trust it alone")
	}

	if err := engB.PullPeer(context.Background(), "site-a"); err == nil {
		t.Fatal("pull succeeded against a closed listener — sever simulation did not actually sever anything")
	}

	severedBody := getStatusPage(t, webB)
	if strings.Contains(severedBody, `data-health="ok"`) {
		t.Fatalf("STATUS PAGE READ HEALTHY WHILE THE PULL WAS FAILING — the exact silent-alerting-system defect §8.9 exists to prevent (stale cached stream state was StateOK, see sawCachedOK above). body:\n%s", severedBody)
	}
	if !strings.Contains(severedBody, "FAILED") {
		t.Fatalf("severed status page did not surface the failed sync attempt; body:\n%s", severedBody)
	}

	// And the "last heard from" age keeps growing even though sync is dead —
	// it is NOT frozen at the last successful contact's rendered value
	// (i.e. the page visibly ages exactly as criterion 5 requires).
	time.Sleep(50 * time.Millisecond)
	severedBodyLater := getStatusPage(t, webB)
	if ageFromBody(t, severedBody) > ageFromBody(t, severedBodyLater)+1e-9 {
		t.Fatalf("age went backwards across two renders of a severed link: %v then %v", ageFromBody(t, severedBody), ageFromBody(t, severedBodyLater))
	}
}

// stampTick authors a distinguishable event so sync/watermark progress is
// observable. G13: engA's store is domain.Register-wired, which fails
// closed on any authored type absent from domain's classified set — a bare
// "tick" is not a real event type at all, so this mints a real (ownMinted)
// company_created instead, which is otherwise inert for this test's purpose
// (only the log entry and watermark advance matter here, not the resulting
// company row).
func stampTick(k string) []byte {
	return []byte(`{"type":"company_created","company_id":"tick-` + k + `","name":"tick ` + k + `"}`)
}

// ageRE matches the shared internal/humantime phrasing ("N second(s)/
// minute(s)/hour(s)/day(s) ago") wherever it appears after "last heard ...
// ago" — anchored on "heard" and "ago" so it cannot accidentally match
// unrelated digits elsewhere on the page (e.g. a watermark or an epoch
// number in the stream table).
var ageRE = regexp.MustCompile(`heard (?:from )?(\d+) (second|minute|hour|day)s? ago`)

// ageFromBody pulls site-a's rendered contact age, converted to seconds, so
// the test can assert monotonic growth across two renders without re-
// deriving ageText's own formatting logic.
func ageFromBody(t *testing.T, body string) float64 {
	t.Helper()
	i := strings.Index(body, "last heard from site-a:")
	if i < 0 {
		t.Fatalf("body has no 'last heard from site-a' line:\n%s", body)
	}
	m := ageRE.FindStringSubmatch(body[i:])
	if m == nil {
		t.Fatalf("could not parse an age out of the site-a status line: %q", body[i:min(i+120, len(body))])
	}
	n, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		t.Fatalf("age %q did not parse: %v", m[1], err)
	}
	switch m[2] {
	case "minute":
		n *= 60
	case "hour":
		n *= 3600
	case "day":
		n *= 86400
	}
	return n
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
