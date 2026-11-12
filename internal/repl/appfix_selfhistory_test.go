// appfix_selfhistory_test.go — Defect 1's shipped engine-level suite
// (design/app-fixes-v1.md §2.7): T1, T3, T4a, T4b, T6, T7, T8, T10.
//
// COMPILE-BLOCKED PENDING BUILD, deliberately: every test here references
// PeerSnapshot.SelfHistoryPending / PeerSnapshot.SelfHistoryVacuous, which
// do not exist until commit 2 lands (§2.1 piece 5 — both fields, JSON tags
// "self_history_pending" / "self_history_vacuous", NO omitempty on either).
// That is the honest pre-fix state (APPFIX-PROBES.md, probe 3's note): the
// shipped tree has no pending flag, so "cannot compile" IS the fail-before
// for the field-referencing asserts. The behavioural fail-befores were
// produced against a field-stubbed baseline (AF4's instrument shape —
// stub fields added to a SCRATCH copy only) and are recorded in the
// test-writer report; the named killer mutants below are re-produced at the
// build gate per §6.4's eleven-item list.
//
// Every test names its intended killer mutant (§6.4 / APPFIX-PROBES.md) in
// its own comment. Standing rule: a check is invalid until its failure mode
// has been produced at least once — where a test asserts an absence, a
// positive control proves the observation channel works.
package repl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"sitecontrol/internal/config"
)

// peerSnap finds one peer's snapshot row by site id.
func peerSnap(t *testing.T, snap Snapshot, siteID string) PeerSnapshot {
	t.Helper()
	for _, p := range snap.Peers {
		if p.SiteID == siteID {
			return p
		}
	}
	t.Fatalf("snapshot has no peer row for %q", siteID)
	return PeerSnapshot{}
}

// reimagedSiteB is the shared teardown shape (probe 1 / §8.6): site-b wiped
// and re-imaged at epoch 2, config-seeded, pointing at A. The caller decides
// what A's listener is serving at boot.
func reimagedSiteB(t *testing.T, a *meshSite) *Engine {
	t.Helper()
	blankCfg := &config.Config{
		SiteID: "site-b", DisplayName: "site-b", ListenAddr: ":0", NodeEpoch: 2,
		PeerKey: "demo-key-site-b", SyncIntervalSeconds: 10,
		DBPath: filepath.Join(t.TempDir(), "site-b-reimaged.db"),
		Peers:  []config.Peer{{SiteID: "site-a", BaseURL: a.srv.URL, Key: "demo-key-site-a"}},
	}
	blankSt := openStore(t, blankCfg.DBPath)
	return New(blankCfg, blankSt)
}

// --- T1 — offline install, then link-up: the actual failure -----------

// T1 (§2.7): shipped descendant of APPFIX-PROBES.md probe 1. Fail-before
// produced (baseline transcript in APPFIX-PROBES.md): watermark(site-b,1)=0
// with the snapshot green — an empty camp behind a green status.
//
// INTENDED KILLER MUTANTS:
//   - watermark assert  → the defect itself (no retry ever runs).
//   - pending assert    → L1 (never latch): recovery succeeds, pending stuck
//     true (§6.4 item 1). L1 survived the entire pre-T1 produced set.
//   - vacuous==false    → always-true vacuous predicate (`preWipe >= 0`,
//     R2TE-1, §6.4 item 9): a peer that just delivered six events must not
//     read as "no pre-wipe history".
//   - literal-bytes assert → `omitempty` on self_history_vacuous (R2TE-2,
//     §6.4 item 10): false must be ON THE WIRE, because field-absent is the
//     USB detector's "unfixed/stale binary" signal (W1, §2.1 piece 5).
func TestOfflineInstalledPostRecoversOwnHistoryOnceTheLinkAppearsAndReportsItHonestly(t *testing.T) {
	sites := newMesh(t, 2)
	a, b := sites[0], sites[1]
	for i := 0; i < 6; i++ {
		if _, err := b.st.Author("site-b", 1, tick(fmt.Sprintf("b-pre-%d", i)), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	a.eng.PullAll(context.Background())
	if wm := mustWatermark(t, a.st, "site-b", 1); wm != 6 {
		t.Fatalf("precondition: A did not absorb B's pre-wipe history: watermark=%d", wm)
	}

	// B is wiped and re-imaged (epoch bumped per §8.2) but the link to A
	// is DOWN at boot: every exchange 503s.
	a.lh.set(nil)
	blankEng := reimagedSiteB(t, a)
	blankEng.Rejoin(context.Background()) // fails non-fatally; the binary serves anyway

	// The link appears. Routine pull cycles run (driven directly; Run's
	// ticker calls exactly this).
	a.lh.set(a.eng.SyncHandler())
	blankEng.PullAll(context.Background())
	blankEng.PullAll(context.Background())

	if wm := mustWatermark(t, blankEng.st, "site-b", 1); wm != 6 {
		t.Fatalf("own history NOT recovered after the link appeared: watermark(site-b,1)=%d want 6 — "+
			"an empty camp behind a green status (probe 1)", wm)
	}

	snap := blankEng.Snapshot()
	peer := peerSnap(t, snap, "site-a")
	if peer.SelfHistoryPending {
		t.Fatalf("recovery succeeded (watermark=6) but SelfHistoryPending is still true — the latch never set (mutant L1): every peer would read un-Healthy forever")
	}
	if peer.SelfHistoryVacuous {
		t.Fatalf("peer site-a just delivered six pre-wipe events yet SelfHistoryVacuous=true — the always-true predicate (>= 0, R2TE-1): every legitimate re-image would fire the routed PI-2 alarm")
	}
	// Green, and green is now honest.
	if peer.LastError != "" || peer.LastContactAgoSec == nil {
		t.Fatalf("peer not green after recovery: LastError=%q contact=%v", peer.LastError, peer.LastContactAgoSec != nil)
	}
	for _, ss := range snap.Streams {
		if ss.State != StateOK {
			t.Fatalf("stream (%s,%d) state=%s after recovery, want ok", ss.SiteID, ss.NodeEpoch, ss.State)
		}
	}
	// W1's wire contract, literal bytes: false is EMITTED, never elided.
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"self_history_vacuous":false`) {
		t.Fatalf("marshalled snapshot does not carry the literal `\"self_history_vacuous\":false` — omitempty (or a renamed tag) makes verified recovery wire-identical to an unfixed binary (W1/R2TE-2):\n%s", raw)
	}
}

// --- T3 — the masking window ------------------------------------------

// T3 (§2.7): shipped version of probe 3. A selective filter 503s ONLY
// queries about site-b (the self-history exchange); peer-log pulls succeed
// and clear lastError every cycle (engine.go's drainSite contact path). The
// pending flag is then the ONLY thing standing between the operator and a
// green page over a missing muster — produced in both directions by AF4
// (APPFIX-PROBES.md probe 3: baseline red via the field-stripped variant).
//
// Fail-before stated honestly: this test cannot compile pre-fix; AF4's
// field-stripped baseline is its produced red.
func TestSelectiveSelfHistoryFailureNeverReadsCleanWhilePeerLogPullsSucceed(t *testing.T) {
	sites := newMesh(t, 2)
	a, b := sites[0], sites[1]
	for i := 0; i < 6; i++ {
		if _, err := b.st.Author("site-b", 1, tick(fmt.Sprintf("b-pre-%d", i)), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.eng.PullPeer(context.Background(), "site-b"); err != nil {
		t.Fatal(err)
	}

	// A answers everything EXCEPT queries about site-b's own history.
	real := a.eng.SyncHandler()
	a.lh.set(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("site") == "site-b" {
			http.Error(w, "filtered", http.StatusServiceUnavailable)
			return
		}
		real.ServeHTTP(w, r)
	}))

	blankEng := reimagedSiteB(t, a)
	blankEng.Rejoin(context.Background())
	blankEng.PullAll(context.Background())
	blankEng.PullAll(context.Background())

	// Fixture pin: the failure is still standing (nothing recovered).
	if wm := mustWatermark(t, blankEng.st, "site-b", 1); wm != 0 {
		t.Fatalf("fixture drifted: own history recovered (watermark=%d) despite the self-history filter — no masking window was posed", wm)
	}
	peer := peerSnap(t, blankEng.Snapshot(), "site-a")
	t.Logf("peer=%s LastError=%q SelfHistoryPending=%v drains=%d contact=%v",
		peer.SiteID, peer.LastError, peer.SelfHistoryPending, peer.Drains, peer.LastContactAgoSec != nil)
	if peer.LastError == "" && !peer.SelfHistoryPending {
		t.Fatalf("MASKING WINDOW OPEN: peer %s reports LastError=\"\" AND SelfHistoryPending=false while own pre-wipe history is missing (watermark=0) — "+
			"the peer-log drain cleared the only error trace and nothing else went red (design §2.2)", peer.SiteID)
	}
}

// --- T4a — the empty answer latches, marked vacuous -------------------

// T4a (§2.7): a first-ever install must not pend forever. Rejoin against a
// peer verifiably holding nothing latches (pending=false) and is marked
// vacuous (vacuous=true), and BOTH facts are literally on the wire.
//
// INTENDED KILLER MUTANTS:
//   - pending assert → L6 (`drainedAll = len(streams) > 0`, §6.4 item 4):
//     the empty answer never latches — a permanent false alarm on every
//     first install, the mirror image of the false green (operators trained
//     to ignore red). L6 survived everything pre-T4a.
//   - literal `"self_history_pending":false` → omitempty-on-pending (§6.4
//     item 10's second half — pins W1's other field).
//   - literal `"self_history_vacuous":true` → the W1 wire contract the USB
//     detector's presence-required positive signal depends on.
func TestRejoinAgainstAPeerHoldingNothingLatchesVerifiedEmptyNotPendingForever(t *testing.T) {
	sites := newMesh(t, 2)
	b := sites[1] // A holds nothing for site-b; nobody has authored anything

	b.eng.Rejoin(context.Background())

	snap := b.eng.Snapshot()
	peer := peerSnap(t, snap, "site-a")
	// Positive control: the exchange genuinely happened and was verified —
	// a refused/failed exchange latching nothing would pass a bare
	// "pending is false on an untouched engine" vacuously.
	if peer.LastError != "" || peer.LastContactAgoSec == nil {
		t.Fatalf("fixture: no verified exchange with the peer (LastError=%q contact=%v) — an unverified empty answer proves nothing", peer.LastError, peer.LastContactAgoSec != nil)
	}
	if peer.SelfHistoryPending {
		t.Fatalf("peer verifiably answered \"I hold zero streams for you\" yet SelfHistoryPending=true — the empty answer never latches (mutant L6): a first install re-arms forever and its status row is permanently red")
	}
	if !peer.SelfHistoryVacuous {
		t.Fatalf("empty verified answer latched but SelfHistoryVacuous=false — a verified-empty answer must be distinguishable from verified recovery (§2.3/W6): the provisioning-side vacuous+re-image alarm hangs on this flag")
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	for _, literal := range []string{`"self_history_pending":false`, `"self_history_vacuous":true`} {
		if !strings.Contains(string(raw), literal) {
			t.Fatalf("marshalled snapshot does not carry the literal %s — W1's always-emit wire contract (field PRESENCE is the USB detector's positive signal):\n%s", literal, raw)
		}
	}
}

// --- T4b — un-armed engines never drain self-history ------------------

// T4b (§2.7): PullAll without Rejoin never drains self-history and never
// reports pending — §2.4's contract, the reason
// TestNothingOnThePadsRosterSurvivesAPeerLogOnlyReplay stays valid.
//
// INTENDED KILLER MUTANT: L5 (drop the rejoinArmed conjunct: `needSelf :=
// !ps.selfDrained`, §6.4/APPFIX-PROBES.md) — under it this un-armed PullAll
// recovers site-b's history and the watermark assert goes red. (L5 was
// already produced red once by AF3 via the peer-log-only muster test; this
// pins the same contract at the engine level.)
func TestPullAllWithoutRejoinNeverDrainsSelfHistoryAndNeverReportsPending(t *testing.T) {
	sites := newMesh(t, 2)
	a, b := sites[0], sites[1]
	for i := 0; i < 6; i++ {
		if _, err := b.st.Author("site-b", 1, tick(fmt.Sprintf("b-pre-%d", i)), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	a.eng.PullAll(context.Background())
	if wm := mustWatermark(t, a.st, "site-b", 1); wm != 6 {
		t.Fatalf("precondition: A did not absorb B's pre-wipe history: watermark=%d", wm)
	}

	// Re-imaged B with the link UP the whole time — and Rejoin NEVER called.
	blankEng := reimagedSiteB(t, a)
	blankEng.PullAll(context.Background())
	blankEng.PullAll(context.Background())

	if wm := mustWatermark(t, blankEng.st, "site-b", 1); wm != 0 {
		t.Fatalf("an UN-ARMED engine's PullAll drained self-history (watermark(site-b,1)=%d, want 0) — "+
			"PullAll's contract is exactly \"every configured peer's log\" for any caller that never entered §8.6 (mutant L5; see §2.4)", wm)
	}
	peer := peerSnap(t, blankEng.Snapshot(), "site-a")
	if peer.SelfHistoryPending {
		t.Fatalf("un-armed engine reports SelfHistoryPending=true — pending is Rejoin's contract, not PullAll's (§2.4)")
	}
	// Positive control: the pulls genuinely ran and succeeded (this is not
	// a dead engine passing by inactivity).
	if peer.LastError != "" || peer.LastContactAgoSec == nil {
		t.Fatalf("fixture: PullAll did not complete a verified exchange (LastError=%q contact=%v)", peer.LastError, peer.LastContactAgoSec != nil)
	}
}

// --- T6 — the partial drain: THE killer of L3 -------------------------

// T6 (§2.7, non-negotiable): the SOLE killer of L3, the most dangerous
// mis-implementation — latch on `err == nil` alone, ignoring `drained`.
// L3 survives every other test here by case analysis (T1/T3 use wholesale
// 503s where err != nil). Shape: the peer answers /sync/streams normally
// but 503s /sync/pull for our site, so the self-drain sees
// err == nil && drained == false — a PARTIAL drain that must NOT latch.
//
// The filter on /sync/pull (NOT /sync/streams) is the load-bearing
// difference from T3, and the fixture-pinning assert (R2TE-3) keeps that
// difference from silently eroding: a drifted fixture that filters all
// site=site-b queries leaves this test green while L3 lives — measured.
//
// INTENDED KILLER MUTANT: L3, three-value form, both latch sites (§6.4
// item 2 — T6 must be the SOLE red of the full repl suite, with the
// positive control wm=0 lastError="" pending=true on the transcript).
// AF3's embodiment was produced red under L3 and green under the fix.
func TestPartialSelfHistoryDrainMustNotLatchAsRecovered(t *testing.T) {
	sites := newMesh(t, 2)
	a, b := sites[0], sites[1]
	for i := 0; i < 6; i++ {
		if _, err := b.st.Author("site-b", 1, tick(fmt.Sprintf("b-pre-%d", i)), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	a.eng.PullAll(context.Background())
	if wm := mustWatermark(t, a.st, "site-b", 1); wm != 6 {
		t.Fatalf("precondition: A did not absorb B's pre-wipe history: watermark=%d", wm)
	}

	// A answers stream listings but 503s every PULL of site-b's history:
	// the self-history drain sees the announced head yet cannot fetch a
	// page. /sync/pull ONLY — never the streams listing (R2TE-3).
	real := a.eng.SyncHandler()
	a.lh.set(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sync/pull" && r.URL.Query().Get("site") == "site-b" {
			http.Error(w, "filtered", http.StatusServiceUnavailable)
			return
		}
		real.ServeHTTP(w, r)
	}))

	blankEng := reimagedSiteB(t, a)
	blankEng.Rejoin(context.Background())
	blankEng.PullAll(context.Background())

	wm := mustWatermark(t, blankEng.st, "site-b", 1)
	if wm == 6 {
		t.Fatal("setup broke: history recovered despite the pull filter — no partial drain was posed")
	}
	snap := blankEng.Snapshot()
	// R2TE-3 fixture pin: a (site-b,*) stream row exists, proving the
	// /sync/streams exchange SUCCEEDED and a genuine partial drain — not a
	// wholesale transport failure — was posed. Without this row the test
	// has degraded into T3's shape and no longer discriminates L3.
	var selfRow bool
	for _, ss := range snap.Streams {
		if ss.SiteID == "site-b" {
			selfRow = true
		}
	}
	if !selfRow {
		t.Fatalf("fixture drifted: no (site-b,*) stream row in the snapshot — the streams exchange did not succeed, so err==nil&&drained==false was never reached; T6 must 503 /sync/pull ONLY (streams=%+v)", snap.Streams)
	}
	peer := peerSnap(t, snap, "site-a")
	t.Logf("wm=%d lastError=%q pending=%v", wm, peer.LastError, peer.SelfHistoryPending)
	if !peer.SelfHistoryPending {
		t.Fatalf("own history PARTIAL/missing (watermark=%d of 6) but SelfHistoryPending=false: the latch recorded success it did not verify (lastError=%q) — permanent green over partial history (mutant L3)", wm, peer.LastError)
	}
	// AF3 standing instruction 1 — the positive control: lastError stays
	// empty, proving the pending flag, NOT the error fields, carries the
	// honesty here.
	if peer.LastError != "" {
		t.Fatalf("lastError=%q — expected \"\": the peer-log drain succeeds and clears it, and a self-history-only failure must not set it (W3); the pending flag alone must carry this", peer.LastError)
	}
}

// --- T7 — the latch is per-peer, proven at n=3 ------------------------

// T7 (§2.7, required per arbiter ruling §3.2): at n=2 a global latch is
// indistinguishable from per-peer BY CONSTRUCTION; this n=3 case is the
// only thing that certifies per-peer independence. The re-imaged site's
// self-history is answerable by peer A but filtered (503) at peer B.
//
// INTENDED KILLER MUTANT: globalized latch (one engine-level selfDrained
// instead of per-peer, §6.4 item 5) — under it, latching A falsely clears
// B and the site-b pending assert goes red ("latching A cleared B").
func TestSelfHistoryLatchIsPerPeerNotGlobalAcrossAThreeSiteMesh(t *testing.T) {
	sites := newMesh(t, 3)
	a, b, c := sites[0], sites[1], sites[2]
	for i := 0; i < 6; i++ {
		if _, err := c.st.Author("site-c", 1, tick(fmt.Sprintf("c-pre-%d", i)), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	a.eng.PullAll(context.Background())
	b.eng.PullAll(context.Background())
	if wm := mustWatermark(t, a.st, "site-c", 1); wm != 6 {
		t.Fatalf("precondition: A did not absorb C's pre-wipe history: watermark=%d", wm)
	}
	if wm := mustWatermark(t, b.st, "site-c", 1); wm != 6 {
		t.Fatalf("precondition: B did not absorb C's pre-wipe history: watermark=%d", wm)
	}

	// B answers peer-log queries but filters site-c's SELF-HISTORY queries;
	// A answers everything. Both peers hold identical history — the filter
	// is the ONLY difference between them.
	realB := b.eng.SyncHandler()
	b.lh.set(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("site") == "site-c" {
			http.Error(w, "filtered", http.StatusServiceUnavailable)
			return
		}
		realB.ServeHTTP(w, r)
	}))

	blankCfg := &config.Config{
		SiteID: "site-c", DisplayName: "site-c", ListenAddr: ":0", NodeEpoch: 2,
		PeerKey: "demo-key-site-c", SyncIntervalSeconds: 10,
		DBPath: filepath.Join(t.TempDir(), "site-c-reimaged.db"),
		Peers: []config.Peer{
			{SiteID: "site-a", BaseURL: a.srv.URL, Key: "demo-key-site-a"},
			{SiteID: "site-b", BaseURL: b.srv.URL, Key: "demo-key-site-b"},
		},
	}
	blankSt := openStore(t, blankCfg.DBPath)
	blankEng := New(blankCfg, blankSt)
	blankEng.Rejoin(context.Background())
	blankEng.PullAll(context.Background())

	// Positive control: recovery genuinely completed — from A.
	if wm := mustWatermark(t, blankSt, "site-c", 1); wm != 6 {
		t.Fatalf("own history not recovered from the answering peer: watermark(site-c,1)=%d want 6", wm)
	}
	snap := blankEng.Snapshot()
	pa := peerSnap(t, snap, "site-a")
	pb := peerSnap(t, snap, "site-b")
	if pa.SelfHistoryPending {
		t.Fatalf("peer A verifiably delivered everything it holds for us yet SelfHistoryPending=true")
	}
	if !pb.SelfHistoryPending {
		t.Fatalf("peer B has never confirmed our self-history (its exchange is filtered) yet SelfHistoryPending=false — "+
			"latching A cleared B: the latch is global, not per-peer (§2.3: peers can hold different amounts of our history; A pending=%v B pending=%v)",
			pa.SelfHistoryPending, pb.SelfHistoryPending)
	}
}

// --- T8 — a self-history-only failure never toggles reachability -------

// T8 (§2.7): the flap test's SEMANTIC half only — Snapshot-based, NO
// log-line counting ships (ruled, PS-1; the 0→25,920 bound is one-time
// evidence in APPFIX-PROBES.md). Under the probe-3 selective filter, the
// peer is reachable throughout and its peer-log drain succeeds every
// cycle; the self-history failure must never route through notePeerError.
//
// The observation is MID-CYCLE, not end-state: the peer-log drain clears
// LastError at end-of-cycle in both designs, so only a capture taken while
// the peer-log streams query is in flight can see the pre-W3 false "PEER
// UNREACHABLE" state. Capture safety (§2.7): the puller holds runMu, not
// mu, while blocked in doJSON; Snapshot takes only mu.
//
// INTENDED KILLER CONFIGURATION: pre-W3 (the retry arm calls notePeerError,
// §6.4 item 6) — every mid-cycle capture then carries the 503 as LastError,
// diagnosing a link flap that does not exist. Red against the
// design-as-previously-written; green under W3.
//
// POSITIVE CONTROLS for the absence asserts: the capture count must equal
// the driven cycles (the channel demonstrably sees every exchange), and the
// end state must show the failure still standing (pending=true, wm=0) with
// the peer contactable — otherwise "LastError was never set" is vacuous.
func TestSelfHistoryOnlyFailureNeverTogglesThePeerReachabilityVerdict(t *testing.T) {
	sites := newMesh(t, 2)
	a, b := sites[0], sites[1]
	for i := 0; i < 3; i++ {
		if _, err := b.st.Author("site-b", 1, tick(fmt.Sprintf("b-pre-%d", i)), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.eng.PullPeer(context.Background(), "site-b"); err != nil {
		t.Fatal(err)
	}

	var blankEng *Engine
	var afterRejoin atomic.Bool
	var capMu sync.Mutex
	var captures []string
	real := a.eng.SyncHandler()
	a.lh.set(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("site") == "site-b" {
			http.Error(w, "filtered", http.StatusServiceUnavailable)
			return
		}
		// A peer-log streams query is in flight: mid-cycle, AFTER this
		// cycle's self-history retry failed, BEFORE its success clears
		// anything. What does the engine say about us right now?
		if afterRejoin.Load() && r.URL.Path == "/sync/streams" {
			snap := blankEng.Snapshot()
			for _, p := range snap.Peers {
				if p.SiteID == "site-a" {
					capMu.Lock()
					captures = append(captures, p.LastError)
					capMu.Unlock()
				}
			}
		}
		real.ServeHTTP(w, r)
	}))

	blankEng = reimagedSiteB(t, a)
	blankEng.Rejoin(context.Background())
	afterRejoin.Store(true)

	const cycles = 5
	for i := 0; i < cycles; i++ {
		blankEng.PullAll(context.Background())
	}

	capMu.Lock()
	got := append([]string(nil), captures...)
	capMu.Unlock()
	if len(got) != cycles {
		t.Fatalf("positive control failed: expected %d mid-cycle captures (one peer-log streams query per PullAll), got %d — the observation channel is not seeing the exchanges", cycles, len(got))
	}
	for i, le := range got {
		if le != "" {
			t.Fatalf("cycle %d: mid-cycle LastError=%q — the self-history-only failure routed through notePeerError and toggled the reachability verdict "+
				"(the pre-W3 flap: a false PEER UNREACHABLE diagnosis every cycle for a peer whose peer-log drain succeeds in the same cycle)", i, le)
		}
	}
	// The failure is real and still standing; the peer is reachable.
	peer := peerSnap(t, blankEng.Snapshot(), "site-a")
	if !peer.SelfHistoryPending {
		t.Fatalf("fixture: SelfHistoryPending=false — no standing self-history failure was posed, so the LastError absence proves nothing")
	}
	if peer.LastContactAgoSec == nil {
		t.Fatalf("fixture: peer never contacted — the 'reachable throughout' premise did not hold")
	}
	if wm := mustWatermark(t, blankEng.st, "site-b", 1); wm != 0 {
		t.Fatalf("fixture drifted: own history recovered (watermark=%d) despite the filter", wm)
	}
}

// --- T10 — the echo-back must stay vacuous ----------------------------

// T10 (§2.7): W7's wrongly-false closure (R2FA-1). A mesh holding ZERO
// pre-wipe events; the re-imaged post's own single post-wipe check-in is
// pulled by the peer and echoed back in the peer's streams answer. The
// vacuous predicate counts PRE-WIPE streams only (NodeEpoch < cfg.NodeEpoch),
// so the echo-back must NOT flip vacuous to false: "verified recovery" over
// an empty mesh is exactly the reading under which the D-6 both-posts-lost
// alarm never fires in the case it exists for.
//
// INTENDED KILLER MUTANT: total-count predicate (revert W7's pre-wipe
// filter to a total announced count — the predicate as round 1 originally
// wrote it; §6.4 item 8). Under it the echo-back stream (current epoch,
// head 1) counts, vacuous reads false, red here. This red was first produced,
// against that same mutant, by the round-2 falsifiability lens R2FA in its own
// probe run; the test below is the landed one of that shape.
// ⟦was: "TestR2FAVacuousCaptureEchoBack" — R2FA's probe-run name, recorded in
// reports/APPFIX-PROBES.md ("Total-count vacuous predicate (T10's fail-before)")
// and APPFIX-REVISER-DISPOSITIONS.md's W7 row. It was NEVER a function in this
// module (no such declaration exists in any ref), so there is nothing to go
// looking for — this is a record of where the red came from, not a pointer. ⟧
func TestPostWipeEchoBackDoesNotMasqueradeAsRecoveredPreWipeHistory(t *testing.T) {
	sites := newMesh(t, 2)
	a, b := sites[0], sites[1]
	// ZERO pre-wipe events exist anywhere for site-b.

	// Boot with the link down: Rejoin fails non-fatally, the retry stays armed.
	a.lh.set(nil)
	blankEng := reimagedSiteB(t, a)
	blankEng.Rejoin(context.Background())

	// The re-imaged post authors ONE post-wipe (epoch-2) check-in...
	if _, err := blankEng.st.Author("site-b", 2, tick("post-wipe-checkin"), 0, "default"); err != nil {
		t.Fatal(err)
	}
	// ...and the peer pulls it — the echo-back. Same public listener, new
	// process behind it (the muster_rebuild harness's re-image rule).
	b.lh.set(blankEng.SyncHandler())
	if err := a.eng.PullPeer(context.Background(), "site-b"); err != nil {
		t.Fatal(err)
	}
	if wm := mustWatermark(t, a.st, "site-b", 2); wm != 1 {
		t.Fatalf("fixture: the peer did not absorb the post-wipe event (watermark(site-b,2)=%d want 1) — no echo-back was posed", wm)
	}

	// The link appears; the armed retry runs on the routine path.
	a.lh.set(a.eng.SyncHandler())
	blankEng.PullAll(context.Background())

	snap := blankEng.Snapshot()
	// Fixture pin: the echo-back stream WAS announced by the peer and
	// verified — without this row the peer answered empty and the vacuous
	// predicate was never discriminated at all.
	var echoRow bool
	for _, ss := range snap.Streams {
		if ss.SiteID == "site-b" && ss.NodeEpoch == 2 {
			echoRow = true
		}
	}
	if !echoRow {
		t.Fatalf("fixture drifted: no (site-b,2) stream row after the retry — the peer never announced the echo-back (streams=%+v)", snap.Streams)
	}
	peer := peerSnap(t, snap, "site-a")
	if peer.SelfHistoryPending {
		t.Fatalf("the retry verifiably drained everything the peer claims to hold yet SelfHistoryPending=true")
	}
	if !peer.SelfHistoryVacuous {
		t.Fatalf("echo-back of our own post-wipe authoring flipped SelfHistoryVacuous to false — \"verified recovery\" over a mesh holding ZERO pre-wipe events: " +
			"the predicate is counting total announced streams, not pre-wipe streams (NodeEpoch < cfg.NodeEpoch), and the D-6 both-posts-lost alarm would never fire in the exact case it exists for (W7/R2FA-1)")
	}
}
