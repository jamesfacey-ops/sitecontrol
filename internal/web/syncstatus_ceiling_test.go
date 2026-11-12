// syncstatus_ceiling_test.go — SYNC-HONESTY (2026-09-09): the age ceiling
// on the ONE Healthy verdict, and the sync signal on the pages that lacked
// it.
//
// THE DEFECT, MEASURED AT 72d6111 BEFORE THE FIX. The first test below was
// run against the unfixed tree and FAILED like this:
//
//	a peer last heard 3 hours ago on a 10 s sync interval rendered
//	data-health="ok" (chip: "... 1 peer(s) healthy")
//
// A dead LINK already went red within ~20 s (a failed attempt sets
// LastError). This is the other way the data can stop flowing — a stalled
// ENGINE, no attempt ever made again — and nothing compared the last
// successful pull's age against sync_interval_seconds, so it stayed green
// for ever. The ceiling is syncStaleMultiple × the configured interval
// (syncstatus.go's constant carries the reasoning for THREE).
//
// Every fixture here states which side of the ceiling it sits on in terms
// of the interval, never as a bare number of seconds, so a later change to
// the multiple changes the fixtures' meaning visibly rather than silently.
//
// Naming rule (BUILDER_BRIEF_RULES #4): every test name states what it
// proves; the body proves exactly that, nothing wider.
package web

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/repl"
	"sitecontrol/internal/store"
)

// testSyncInterval is the interval every hand-built-fixture test in this
// package renders under — the deployed value (deploy/site-a.toml,
// deploy/site-b.toml: sync_interval_seconds = 10) and newTestServer's.
const testSyncInterval = 10 * time.Second

// naiveHealthyIgnoringAge replicates the EXACT pre-fix rule: no error and
// every owned stream ok ⇒ healthy, whatever the contact age. It exists ONLY
// to prove each fixture below is capable of tricking that rule — i.e. that
// the real assertion can fail (BUILDER_BRIEF_RULES #5).
func naiveHealthyIgnoringAge(p repl.PeerSnapshot, streams []repl.StreamStatus) bool {
	if p.LastContactAgoSec == nil || p.LastError != "" {
		return false
	}
	for _, s := range streams {
		if s.SiteID == p.SiteID && s.State != repl.StateOK {
			return false
		}
	}
	return true
}

// stalledSnapshot is the stalled-engine state: the last pull SUCCEEDED
// (a cached ok stream, no error) but that success is now `age` old.
func stalledSnapshot(age time.Duration) repl.Snapshot {
	return repl.Snapshot{
		SiteID: "site-b",
		Peers:  []repl.PeerSnapshot{{SiteID: "site-a", LastContactAgoSec: f(age.Seconds()), Drains: 12}},
		Streams: []repl.StreamStatus{
			{StreamKey: repl.StreamKey{SiteID: "site-a", NodeEpoch: 1}, Watermark: 500, Head: 500, State: repl.StateOK},
		},
	}
}

// --- the "before" witness: the status page, exactly as an operator sees it ---

func TestStatusPageStalledEngineIsNotRenderedHealthy(t *testing.T) {
	srv := newTestServer(t) // SyncIntervalSeconds: 10
	srv.SyncSnapshot = func() repl.Snapshot { return stalledSnapshot(3 * time.Hour) }
	body := getStatusPage(t, srv)
	if strings.Contains(body, `data-health="ok"`) {
		t.Fatalf("a peer last heard 3 hours ago on a 10 s sync interval rendered data-health=\"ok\" (chip: %q)", chipText(body))
	}
	if !strings.Contains(body, `data-health="degraded"`) {
		t.Fatalf("stalled-engine peer did not render as degraded; body: %s", body)
	}
	if !strings.Contains(body, "STALE") {
		t.Fatalf("the verdict does not say STALE; chip %q", chipText(body))
	}
}

// chipText returns the rendered sync chip (label + value), whitespace
// collapsed, or "(no sync chip)" — for failure messages and for the
// glass-equals-paper comparison in muster_sync_print_test.go.
func chipText(body string) string {
	i := strings.Index(body, `<span class="chip-label">sync</span>`)
	if i < 0 {
		return "(no sync chip)"
	}
	rest := body[i:]
	j := strings.Index(rest, "</div>")
	if j < 0 {
		return rest
	}
	return strings.Join(strings.Fields(rest[:j]), " ")
}

// --- the view model: the ceiling itself ---

func TestBuildSyncStatusViewStalledEngineBeyondTheCeilingIsNotHealthy(t *testing.T) {
	age := 3 * time.Hour // ≫ syncStaleMultiple × testSyncInterval
	snap := stalledSnapshot(age)

	// Proof the fixture CAN fail (rule #5): the pre-fix rule reads it healthy.
	if !naiveHealthyIgnoringAge(snap.Peers[0], snap.Streams) {
		t.Fatal("fixture is not capable of tricking the pre-fix rule — strengthen it before trusting the assertion below")
	}

	pv := buildSyncStatusView(snap, testSyncInterval).Peers[0]
	if pv.Healthy {
		t.Fatalf("a peer last heard %v ago on a %v interval rendered Healthy=true — the stalled-engine false green", age, testSyncInterval)
	}
	for _, want := range []string{"STALE", "3 hours ago", "3×", testSyncInterval.String(), "has NOT reached this post"} {
		if !strings.Contains(pv.Verdict, want) {
			t.Errorf("verdict %q does not carry %q", pv.Verdict, want)
		}
	}
}

func TestBuildSyncStatusViewHealthyPeerWithinTheCeilingStaysHealthy(t *testing.T) {
	// A working link's age is normally at most one interval plus the pull's
	// own duration. Every age here is inside syncStaleMultiple × interval;
	// each must stay green — an alarm that cries wolf on a working link
	// teaches officers to ignore it.
	for _, age := range []time.Duration{
		0,
		testSyncInterval / 2,
		testSyncInterval,
		testSyncInterval + time.Second,       // the age an existing test already calls healthy (11 s)
		2 * testSyncInterval,                 // one missed tick
		syncStaleMultiple * testSyncInterval, // EXACTLY the ceiling: not stale (strictly greater is)
	} {
		pv := buildSyncStatusView(stalledSnapshot(age), testSyncInterval).Peers[0]
		if !pv.Healthy {
			t.Errorf("age %v (ceiling %v) rendered Healthy=false: %q", age, syncStaleMultiple*testSyncInterval, pv.Verdict)
		}
		if !strings.HasPrefix(pv.Verdict, "ok — last heard") {
			t.Errorf("age %v: verdict %q is not the ok form", age, pv.Verdict)
		}
	}
	// And one tick past the ceiling is stale — the boundary is real.
	over := syncStaleMultiple*testSyncInterval + time.Millisecond
	if pv := buildSyncStatusView(stalledSnapshot(over), testSyncInterval).Peers[0]; pv.Healthy {
		t.Fatalf("age %v, just past the ceiling, rendered Healthy=true", over)
	}
}

func TestBuildSyncStatusViewHealthyPeerBehindADeadSiblingIsNotCriedWolfOn(t *testing.T) {
	// WHY THE MULTIPLE IS THREE AND NOT TWO (syncStaleMultiple's doc).
	// PullAll is sequential over peers: a healthy peer configured after a
	// dead one waits out the dead peer's attempt — up to engine.go's 10 s
	// client timeout — before its own pull runs. At the deployed 10 s
	// interval that is a legitimate age of ~interval + timeout = 20 s on a
	// link that is working perfectly.
	const clientTimeout = 10 * time.Second // engine.go New(): http.Client{Timeout: 10 * time.Second}
	legit := testSyncInterval + clientTimeout + time.Second

	// Fixture precondition: this age would be stale at 2× and is inside 3×,
	// so the test discriminates the two multiples rather than passing under
	// both.
	if !(legit > 2*testSyncInterval && legit <= syncStaleMultiple*testSyncInterval) {
		t.Fatalf("fixture age %v does not sit between 2× and %d× the interval — it cannot tell the multiples apart", legit, syncStaleMultiple)
	}

	snap := repl.Snapshot{
		SiteID: "site-c",
		Peers: []repl.PeerSnapshot{
			{SiteID: "site-a", LastContactAgoSec: f(300), LastError: "repl: GET /sync/streams: context deadline exceeded"}, // dead, first in config order
			{SiteID: "site-b", LastContactAgoSec: f(legit.Seconds()), Drains: 40},                                          // healthy, waited behind it
		},
		Streams: []repl.StreamStatus{
			{StreamKey: repl.StreamKey{SiteID: "site-a", NodeEpoch: 1}, Watermark: 9, Head: 9, State: repl.StateOK},
			{StreamKey: repl.StreamKey{SiteID: "site-b", NodeEpoch: 1}, Watermark: 77, Head: 77, State: repl.StateOK},
		},
	}
	view := buildSyncStatusView(snap, testSyncInterval)
	if view.Peers[0].Healthy {
		t.Fatal("the dead peer rendered Healthy=true")
	}
	if !view.Peers[1].Healthy {
		t.Fatalf("the working peer behind a dead sibling was cried wolf on at age %v: %q", legit, view.Peers[1].Verdict)
	}
	if view.DegradedCount() != 1 {
		t.Fatalf("DegradedCount = %d, want 1 (the dead one only)", view.DegradedCount())
	}
}

func TestBuildSyncStatusViewNoIntervalConfiguredIsNotHealthy(t *testing.T) {
	// config.validate refuses sync_interval_seconds <= 0, so production
	// never reaches this; a caller that does must not be handed a Healthy
	// it did not earn, and must not get a silent "ceiling off".
	pv := buildSyncStatusView(stalledSnapshot(time.Second), 0).Peers[0]
	if pv.Healthy {
		t.Fatal("a zero interval minted Healthy=true — that is a silent ceiling-off switch")
	}
	if !strings.Contains(pv.Verdict, "cannot judge freshness") {
		t.Fatalf("verdict %q does not say the ceiling could not be derived", pv.Verdict)
	}
}

func TestBuildSyncStatusViewStaleOutranksReachableButStalledWording(t *testing.T) {
	// A bad stream AND a contact older than the ceiling: the verdict must not
	// open with "reachable (last heard 3 hours ago)" — reachable is not a
	// claim this post can make about a peer it has exchanged nothing with
	// for longer than the ceiling.
	snap := stalledSnapshot(3 * time.Hour)
	snap.Streams[0].State = repl.StateSchemaMismatch
	pv := buildSyncStatusView(snap, testSyncInterval).Peers[0]
	if pv.Healthy {
		t.Fatal("Healthy=true with a refused stream and a stale contact")
	}
	if strings.HasPrefix(pv.Verdict, "reachable") {
		t.Fatalf("verdict claims reachability over a stale contact: %q", pv.Verdict)
	}
	if !strings.HasPrefix(pv.Verdict, "STALE") {
		t.Fatalf("verdict %q does not lead with STALE", pv.Verdict)
	}
}

func TestBuildSyncStatusViewCarriesTheCeilingItApplied(t *testing.T) {
	view := buildSyncStatusView(stalledSnapshot(time.Second), testSyncInterval)
	if view.StaleMultiple != syncStaleMultiple || view.StaleAfter != syncStaleMultiple*testSyncInterval {
		t.Fatalf("view carries multiple=%d after=%v, want %d and %v", view.StaleMultiple, view.StaleAfter, syncStaleMultiple, syncStaleMultiple*testSyncInterval)
	}
	body := getStatusPage(t, func() *Server {
		s := newTestServer(t)
		s.SyncSnapshot = func() repl.Snapshot { return stalledSnapshot(time.Second) }
		return s
	}())
	flat := strings.Join(strings.Fields(body), " ") // the template wraps the sentence across source lines
	if !strings.Contains(flat, "reads STALE once its last successful pull is older than 3× this (30s)") {
		t.Fatalf("status page does not state the ceiling beside the interval; body: %s", body)
	}
}

// --- the muster pages: the chip and the printed block, from the same verdict ---

// musterWithSync is newMusterTestServer with a snapshot wired in.
func musterWithSync(t *testing.T, snap func() repl.Snapshot) *Server {
	t.Helper()
	srv, _ := newMusterTestServer(t, 0) // SyncIntervalSeconds: 10
	srv.SyncSnapshot = snap
	return srv
}

func TestMusterPagesRenderTheSyncChipAndPrintBlockDegradedForAStalledEngine(t *testing.T) {
	srv := musterWithSync(t, func() repl.Snapshot { return stalledSnapshot(3 * time.Hour) })
	for _, path := range []string{"/muster", "/muster/print"} {
		body := rawPage(t, srv, path)
		if !strings.Contains(body, `<span class="chip-label">sync</span>`) {
			t.Errorf("%s: no sync chip in the top bar", path)
		}
		if !strings.Contains(chipText(body), "1 of 1 peer(s) degraded") {
			t.Errorf("%s: chip = %q, want the degraded summary", path, chipText(body))
		}
		if !strings.Contains(body, `data-sync-health="degraded"`) || strings.Contains(body, `data-sync-health="ok"`) {
			t.Errorf("%s: printed block did not mark site-a degraded (and only degraded)", path)
		}
		if !strings.Contains(body, `data-sync-peer="site-a"`) || !strings.Contains(body, "STALE — last heard 3 hours ago") {
			t.Errorf("%s: printed block does not carry site-a's STALE verdict", path)
		}
		if !strings.Contains(body, `class="sync-scope sync-scope-bad"`) {
			t.Errorf("%s: degraded block did not take the warning form", path)
		}
	}
}

func TestMusterPagesRenderTheSyncChipAndPrintBlockHealthyForAWorkingLink(t *testing.T) {
	srv := musterWithSync(t, func() repl.Snapshot { return stalledSnapshot(7 * time.Second) })
	for _, path := range []string{"/muster", "/muster/print"} {
		body := rawPage(t, srv, path)
		if !strings.Contains(chipText(body), "1 peer(s) healthy") {
			t.Errorf("%s: chip = %q, want healthy", path, chipText(body))
		}
		if !strings.Contains(body, `data-sync-health="ok"`) || strings.Contains(body, `data-sync-health="degraded"`) {
			t.Errorf("%s: printed block did not mark site-a ok (and only ok)", path)
		}
		if !strings.Contains(body, "ok — last heard 7 seconds ago") {
			t.Errorf("%s: printed block does not carry the ok verdict with its age", path)
		}
		// class="…" attribute, not the bare class name: ".sync-scope-bad" sits
		// in the inlined stylesheet on EVERY page (F19).
		if strings.Contains(body, `class="sync-scope sync-scope-bad"`) || !strings.Contains(body, `class="sync-scope" data-muster-section="sync"`) {
			t.Errorf("%s: a healthy link rendered the warning form", path)
		}
	}
}

func TestMusterPagesWithNoEngineWiredSayCannotTellNotHealthy(t *testing.T) {
	srv, _ := newMusterTestServer(t, 0) // SyncSnapshot nil
	for _, path := range []string{"/muster", "/muster/print"} {
		body := rawPage(t, srv, path)
		if !strings.Contains(chipText(body), "cannot tell") {
			t.Errorf("%s: chip = %q, want the explicit refusal", path, chipText(body))
		}
		if !strings.Contains(body, "Sync status unavailable") || !strings.Contains(body, "NOT a report that sync is healthy") {
			t.Errorf("%s: printed block lacks the explicit refusal", path)
		}
		if strings.Contains(body, `data-sync-health="ok"`) || strings.Contains(body, "peer(s) healthy") {
			t.Errorf("%s: an unwired engine read as healthy", path)
		}
	}
}

func TestTripsConsoleRendersTheSyncChipFromTheSameVerdict(t *testing.T) {
	srv, _, _ := newTripsTestServer(t)
	srv.SyncSnapshot = func() repl.Snapshot { return stalledSnapshot(3 * time.Hour) }
	body := getTrips(t, srv)
	if !strings.Contains(chipText(body), "1 of 1 peer(s) degraded") {
		t.Fatalf("/trips chip = %q, want the degraded summary for a stalled engine", chipText(body))
	}
	srv.SyncSnapshot = func() repl.Snapshot { return stalledSnapshot(7 * time.Second) }
	if body := getTrips(t, srv); !strings.Contains(chipText(body), "1 peer(s) healthy") {
		t.Fatalf("/trips chip = %q, want healthy for a working link", chipText(body))
	}
}

// --- a REAL engine that pulled once and then stopped: the muster must go
// red on its own, with no link error ever raised ---

func TestMusterGoesDegradedWhenARealEngineStopsPullingWithNoLinkError(t *testing.T) {
	// Site A: a real engine serving its own log over a real listener.
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
	defer srvA.Close()
	if _, err := engA.Author(stampTick("a-1"), 0, "default"); err != nil {
		t.Fatal(err)
	}

	// Site B: a real engine configured to pull A on a ONE-second interval —
	// so the ceiling is 3 s and this test's wait is bounded — whose Run
	// loop is deliberately NEVER started. One manual pull, then silence:
	// the stalled engine.
	stB, err := store.Open(filepath.Join(t.TempDir(), "site-b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stB.Close() })
	if err := domain.Register(stB); err != nil {
		t.Fatal(err)
	}
	cfgB := &config.Config{SiteID: "site-b", DisplayName: "Site B", ListenAddr: ":0",
		NodeEpoch: 1, PeerKey: "key-b", SyncIntervalSeconds: 1, DBPath: stB.Path,
		Peers: []config.Peer{{SiteID: "site-a", BaseURL: srvA.URL, Key: "key-a"}}}
	engB := repl.New(cfgB, stB)
	t.Cleanup(engB.Shutdown)
	webB := New(cfgB, stB)
	webB.SyncSnapshot = engB.Snapshot

	if err := engB.PullPeer(context.Background(), "site-a"); err != nil {
		t.Fatalf("precondition pull failed: %v", err)
	}
	body := rawPage(t, webB, "/muster/print")
	if !strings.Contains(body, `data-sync-health="ok"`) {
		t.Fatalf("right after a successful pull the muster did not read ok; chip %q", chipText(body))
	}

	// Wait past the ceiling (3 × 1 s). Polled, not a fixed sleep: the
	// assertion is "degrades once the age passes the ceiling", not a timing.
	ceiling := time.Duration(syncStaleMultiple) * cfgB.SyncInterval()
	deadline := time.Now().Add(ceiling + 5*time.Second)
	for {
		body = rawPage(t, webB, "/muster/print")
		if strings.Contains(body, `data-sync-health="degraded"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("a real engine that pulled once and never again still read ok %v past its ceiling; chip %q", 5*time.Second, chipText(body))
		}
		time.Sleep(200 * time.Millisecond)
	}
	if strings.Contains(body, "last error") || strings.Contains(body, "FAILED") {
		t.Fatalf("the stalled engine raised a link error — this is not the stalled-engine case; body: %s", body)
	}
	if !strings.Contains(body, "STALE") || !strings.Contains(chipText(body), "1 of 1 peer(s) degraded") {
		t.Fatalf("degraded, but not in the STALE vocabulary; chip %q", chipText(body))
	}
	// The engine itself still reports NO error: the red came from the age
	// ceiling alone.
	for _, p := range engB.Snapshot().Peers {
		if p.LastError != "" {
			t.Fatalf("engine reports an error %q — the fixture is not a pure stall", p.LastError)
		}
	}
}
