// C3b done-when tests (chunk-plan-v2 §C3b): the self-history rejoin
// exception, the own-prior-epoch-first rejoin sequence through the
// IDENTICAL routine catch-up path (drainSite/pullStream — see engine.go),
// resumed authoring at the config-seeded epoch (row-13 residue, asserted
// directly here rather than left as a bare pointer to C12), the "peer log
// regressed" status flip, and the third-party-smuggling security boundary
// on both the server (refuseForeignSite) and client (pullStream's ingest
// check) sides of the self-history exception.
package repl

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/store"
)

// --- done-when: a blanked B recovers to dumps identical to its peers via
// the self-history exception, through the routine catch-up path; own-
// prior-epoch-first ordering is observed; resumed authoring lands at the
// config-seeded epoch (row 13) ---

func TestBlankNodeRejoinsRecoveringOwnHistoryFirstThenPeerLogsThroughRoutineCatchUp(t *testing.T) {
	// Phase 1: a live 2-site mesh, both in sync — the state just before B
	// is wiped. B authors its own history; A pulls it, so A's local log
	// (like any peer's, by ordinary replication) holds a full copy of B's
	// stream too — that copy is what a rejoining B will recover from.
	sites := newMesh(t, 2)
	a, b := sites[0], sites[1]
	for i := 0; i < 6; i++ {
		if _, err := b.st.Author("site-b", 1, tick(fmt.Sprintf("b-pre-%d", i)), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 4; i++ {
		if _, err := a.st.Author("site-a", 1, tick(fmt.Sprintf("a-pre-%d", i)), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	a.eng.PullAll(context.Background())
	if wm := mustWatermark(t, a.st, "site-b", 1); wm != 6 {
		t.Fatalf("precondition: A did not absorb B's pre-wipe history: watermark=%d", wm)
	}
	witnessDump := mustDump(t, a.st) // what a fully-recovered B should fold to

	// Phase 2: B is wiped and re-imaged — a fresh store, node_epoch bumped
	// 1->2 in config (the re-image provisioning step, §8.2), same site_id,
	// same peers. This is "blank" per §8.6.
	blankCfg := &config.Config{
		SiteID: "site-b", DisplayName: "site-b", ListenAddr: ":0", NodeEpoch: 2,
		PeerKey: "demo-key-site-b", SyncIntervalSeconds: 10,
		DBPath: filepath.Join(t.TempDir(), "site-b-reimaged.db"),
		Peers:  []config.Peer{{SiteID: "site-a", BaseURL: a.srv.URL, Key: "demo-key-site-a"}},
	}
	blankSt := openStore(t, blankCfg.DBPath)
	blankEng := New(blankCfg, blankSt)
	blankEng.PageLimit = 3 // force pagination during recovery too (both halves)

	// Track the ORDER of applied envelopes to prove own-history-first.
	var mu sync.Mutex
	var order []store.Key
	blankEng.OnApplied = func(env store.Envelope, _ time.Time, _ bool) {
		mu.Lock()
		order = append(order, env.Key())
		mu.Unlock()
	}

	blankEng.Rejoin(context.Background())

	// Recovered: own prior-epoch history, plus the peer's log, folding to
	// exactly the same dump a peer that was never wiped holds.
	if wm := mustWatermark(t, blankSt, "site-b", 1); wm != 6 {
		t.Fatalf("self-history not recovered: watermark(site-b,1)=%d, want 6", wm)
	}
	if wm := mustWatermark(t, blankSt, "site-a", 1); wm != 4 {
		t.Fatalf("peer log not recovered: watermark(site-a,1)=%d, want 4", wm)
	}
	if got := mustDump(t, blankSt); !bytes.Equal(got, witnessDump) {
		t.Fatalf("recovered dump diverges from a peer's:\n got:  %s\n want: %s", got, witnessDump)
	}

	// Own-prior-epoch-first ordering (§8.6): every self-history key
	// (site-b) precedes every peer-log key (site-a) in the apply sequence
	// — not merely "both eventually present".
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 10 {
		t.Fatalf("OnApplied fired %d times, want 10 (6 self-history + 4 peer log)", len(order))
	}
	lastSelfIdx, firstPeerIdx := -1, -1
	for i, k := range order {
		switch k.SiteID {
		case "site-b":
			lastSelfIdx = i
		case "site-a":
			if firstPeerIdx == -1 {
				firstPeerIdx = i
			}
		}
	}
	if firstPeerIdx == -1 || lastSelfIdx == -1 || lastSelfIdx > firstPeerIdx {
		t.Fatalf("own-prior-epoch-first ordering violated: apply order was %v", order)
	}

	// Row 13 (plan residue): "resumes authoring at the config-seeded
	// node_epoch+1" — asserted directly, not left as a bare pointer to
	// C12. The mechanism needs no new code: Author already stamps
	// e.cfg.NodeEpoch verbatim (C3a), and node_epoch+1 here is simply what
	// re-image provisioning wrote into blankCfg above. What's actually
	// under test is that Rejoin didn't disturb it, and that the fresh
	// epoch-2 stream is genuinely fresh (seq 1), not colliding with the
	// recovered epoch-1 history.
	env, err := blankEng.Author(tick("resumed"), 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if env.NodeEpoch != 2 || env.Seq != 1 {
		t.Fatalf("resumed authoring at (epoch %d, seq %d), want (2, 1) — the config-seeded epoch+1 fresh stream", env.NodeEpoch, env.Seq)
	}
	if wm := mustWatermark(t, blankSt, "site-b", 1); wm != 6 {
		t.Fatalf("resumed authoring disturbed the recovered epoch-1 history: watermark=%d", wm)
	}
	if wm := mustWatermark(t, blankSt, "site-b", 2); wm != 1 {
		t.Fatalf("resumed epoch-2 stream watermark=%d, want 1", wm)
	}
}

// --- not-done-if guard, and a robustness claim in Rejoin's own doc
// comment: calling Rejoin when no peer holds any self-history (an ordinary
// first boot, or a restart that was never wiped) is a harmless no-op — no
// error, nothing spuriously applied, ordinary peer catch-up still runs ---

func TestRejoinIsANoOpForSelfHistoryWhenNoPeerHoldsAnyAndStillCatchesUpPeerLogs(t *testing.T) {
	sites := newMesh(t, 2)
	a, b := sites[0], sites[1]
	for i := 0; i < 3; i++ {
		if _, err := a.st.Author("site-a", 1, tick(fmt.Sprintf("a%d", i)), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	// B has never authored anything, and A holds nothing under site-b: the
	// self-history step has nothing to recover.
	b.eng.Rejoin(context.Background())
	if wm := mustWatermark(t, b.st, "site-b", 1); wm != 0 {
		t.Fatalf("self-history watermark for a never-authored stream = %d, want 0", wm)
	}
	if wm := mustWatermark(t, b.st, "site-a", 1); wm != 3 {
		t.Fatalf("ordinary peer catch-up did not run: watermark(site-a,1)=%d, want 3", wm)
	}
}

// --- done-when: the "peer log regressed" state flips on a same-epoch
// answer below a previously seen seq ---

func TestPeerLogRegressedFlipsWhenSameEpochHeadDropsBelowPreviouslySeen(t *testing.T) {
	r := &rig{siteID: "site-x"}
	r.setStreams(StreamHead{NodeEpoch: 1, Head: 10})
	r.setEvents(1, mkStream("site-x", 1, 10, store.SchemaVersion, "site-x"))
	rs := httptest.NewServer(r.handler())
	defer rs.Close()

	eng, st := engineWithPeers(t, "site-b", config.Peer{SiteID: "site-x", BaseURL: rs.URL, Key: "k"})
	eng.PullPeer(context.Background(), "site-x")
	if wm := mustWatermark(t, st, "site-x", 1); wm != 10 {
		t.Fatalf("precondition: watermark=%d, want 10", wm)
	}
	if ss, ok := streamState(eng, "site-x", 1); !ok || ss.State != StateOK {
		t.Fatalf("precondition: stream state = %+v, want ok", ss)
	}

	// The peer's reported head regresses within the SAME epoch — a genuine
	// re-image would announce a HIGHER epoch instead (§8.6), so this is
	// never legitimate.
	r.setStreams(StreamHead{NodeEpoch: 1, Head: 3})
	eng.PullPeer(context.Background(), "site-x")

	ss, ok := streamState(eng, "site-x", 1)
	if !ok || ss.State != StateRegressed {
		t.Fatalf("stream state after head regression = %+v, want %s", ss, StateRegressed)
	}
	// The regression is surfaced, never applied backwards: watermark holds.
	if wm := mustWatermark(t, st, "site-x", 1); wm != 10 {
		t.Fatalf("watermark rolled back to %d after a regressed head report, want 10 held", wm)
	}
}

// --- scoping check on the same mechanism: a genuine higher-epoch
// announcement (a real re-image) must NOT be mistaken for a regression,
// even though its fresh stream's head starts low ---

func TestEpochBumpToAFreshLowHeadDoesNotFlipRegressed(t *testing.T) {
	r := &rig{siteID: "site-x"}
	r.setStreams(StreamHead{NodeEpoch: 1, Head: 10})
	r.setEvents(1, mkStream("site-x", 1, 10, store.SchemaVersion, "site-x"))
	rs := httptest.NewServer(r.handler())
	defer rs.Close()

	eng, st := engineWithPeers(t, "site-b", config.Peer{SiteID: "site-x", BaseURL: rs.URL, Key: "k"})
	eng.PullPeer(context.Background(), "site-x")

	// site-x re-images: a NEW epoch, head starts at 2 — numerically lower
	// than epoch 1's peak of 10, but a different StreamKey, so it must read
	// ok, not regressed.
	r.setStreams(StreamHead{NodeEpoch: 1, Head: 10}, StreamHead{NodeEpoch: 2, Head: 2})
	r.setEvents(2, mkStream("site-x", 2, 2, store.SchemaVersion, "site-x"))
	eng.PullPeer(context.Background(), "site-x")

	if ss, ok := streamState(eng, "site-x", 2); !ok || ss.State != StateOK {
		t.Fatalf("fresh higher-epoch stream state = %+v, want ok (not a false regression)", ss)
	}
	if wm := mustWatermark(t, st, "site-x", 2); wm != 2 {
		t.Fatalf("epoch-2 watermark=%d, want 2", wm)
	}
}

// --- server-side security boundary: the self-history exception serves a
// requester ONLY its own claimed identity's stream, never a third site's,
// even though the requester is an authenticated peer ---

func TestPullServerServesSelfHistoryOnlyNotAnyThirdSiteClaimedAsSelf(t *testing.T) {
	sites := newMesh(t, 2) // site-a, site-b
	a := sites[0]
	if _, err := a.st.Author("site-a", 1, tick("x"), 0, "default"); err != nil {
		t.Fatal(err)
	}
	do := func(path, from string) int {
		req, _ := http.NewRequest(http.MethodGet, a.srv.URL+path, nil)
		req.Header.Set("Authorization", "Bearer demo-key-site-a")
		req.Header.Set(HeaderFrom, from)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	// A legitimate self-history ask: site-b (an authenticated peer of A)
	// asking for its OWN stream — allowed, even though A holds nothing for
	// site-b yet (the ASK itself must not be refused).
	if code := do("/sync/streams?site=site-b", "site-b"); code != http.StatusOK {
		t.Fatalf("legitimate self-history streams request: %d, want 200", code)
	}
	if code := do("/sync/pull?site=site-b&epoch=1&from=1", "site-b"); code != http.StatusOK {
		t.Fatalf("legitimate self-history pull request: %d, want 200", code)
	}
	// The smuggling attempt: an authenticated requester claims to be
	// site-b but asks for a THIRD site's (site-c's) stream. "Any
	// authenticated peer" only ever widens who may be served OUR OWN
	// identity's records — it never lets a claimed identity fetch someone
	// else's.
	if code := do("/sync/streams?site=site-c", "site-b"); code != http.StatusForbidden {
		t.Fatalf("third-site streams request under a self claim: %d, want 403", code)
	}
	if code := do("/sync/pull?site=site-c&epoch=1&from=1", "site-b"); code != http.StatusForbidden {
		t.Fatalf("third-site pull request under a self claim: %d, want 403", code)
	}
}

// --- security boundary (review finding, post-independent-verification):
// the self-history exception is scoped to PRIOR epochs only (§8.4: "its own
// prior-epoch stream"; §8.6 rejoin order). A rigged/compromised peer must
// not be able to forge records into our OWN CURRENT (not yet authored) or a
// FUTURE epoch under cover of a self-history pull — that would let a single
// bad peer plant fake "self-authored" history that the whole mesh would
// later treat as genuine, and would corrupt our own next Author() seq. ---

func TestSelfHistoryIngestRejectsRecordsAtOrAboveOurOwnCurrentEpoch(t *testing.T) {
	cases := []struct {
		name        string
		forgedEpoch int64
	}{
		{"current epoch (not yet authored)", 2},
		{"future epoch", 99},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &rig{siteID: "site-x"}
			r.setStreams(StreamHead{NodeEpoch: tc.forgedEpoch, Head: 3})
			r.setEvents(tc.forgedEpoch, mkStream("site-b", tc.forgedEpoch, 3, store.SchemaVersion, "site-b"))
			rs := httptest.NewServer(r.handler())
			defer rs.Close()

			cfg := &config.Config{
				SiteID: "site-b", ListenAddr: ":0", NodeEpoch: 2, PeerKey: "demo-key-site-b",
				SyncIntervalSeconds: 10, DBPath: filepath.Join(t.TempDir(), "site-b.db"),
				Peers: []config.Peer{{SiteID: "site-x", BaseURL: rs.URL, Key: "k"}},
			}
			st := openStore(t, cfg.DBPath)
			eng := New(cfg, st)
			eng.Rejoin(context.Background())

			if wm := mustWatermark(t, st, "site-b", tc.forgedEpoch); wm != 0 {
				t.Fatalf("forged records at epoch %d applied: watermark(site-b,%d)=%d, want 0", tc.forgedEpoch, tc.forgedEpoch, wm)
			}
			ss, ok := streamState(eng, "site-b", tc.forgedEpoch)
			if !ok || ss.State != StateIdentityRejected {
				t.Fatalf("stream state = %+v, want %s (forged current/future-epoch self-history rejected)", ss, StateIdentityRejected)
			}
			// The forgery must not shift our own seq numbering: our first
			// real Author() must land at seq 1, not seq 4 (credit for the
			// 3 forged envelopes).
			env, err := eng.Author(tick("resumed"), 0, "default")
			if err != nil {
				t.Fatal(err)
			}
			if env.Seq != 1 {
				t.Fatalf("first Author() landed at seq %d, want 1 — forged epoch-%d envelopes were credited", env.Seq, tc.forgedEpoch)
			}
		})
	}
}

// --- client-side security boundary (defense in depth against a
// compromised/non-conforming peer server): during a self-history pull, the
// ingest check must still reject envelopes bearing a THIRD site's identity
// — the exception is "our own site_id, from any peer", never "whatever a
// peer sends back while we're rejoining" ---

func TestSelfHistoryIngestRejectsRecordsBearingAThirdSitesIdentity(t *testing.T) {
	// A rig that, asked for OUR OWN site's self-history, actually serves a
	// THIRD site's records — modelling a compromised or buggy peer trying
	// to smuggle foreign history in under cover of rejoin.
	r := &rig{siteID: "site-x"}
	r.setStreams(StreamHead{NodeEpoch: 1, Head: 3})
	r.setEvents(1, mkStream("site-c", 1, 3, store.SchemaVersion, "site-c")) // smuggled: bears site-c's identity, not ours
	rs := httptest.NewServer(r.handler())
	defer rs.Close()

	eng, st := engineWithPeers(t, "site-b", config.Peer{SiteID: "site-x", BaseURL: rs.URL, Key: "k"})
	eng.Rejoin(context.Background())

	if wm := mustWatermark(t, st, "site-c", 1); wm != 0 {
		t.Fatalf("smuggled third-site record applied under its own identity: watermark(site-c,1)=%d", wm)
	}
	if wm := mustWatermark(t, st, "site-b", 1); wm != 0 {
		t.Fatalf("smuggled record credited to our own self-history stream: watermark(site-b,1)=%d", wm)
	}
	ss, ok := streamState(eng, "site-b", 1) // the self-history stream we actually asked about
	if !ok || ss.State != StateIdentityRejected {
		t.Fatalf("self-history stream state = %+v, want %s (smuggled third-site record refused)", ss, StateIdentityRejected)
	}
	if snap := eng.Snapshot(); snap.Applied != 0 {
		t.Fatalf("applied=%d, want 0 — nothing from the smuggling attempt may apply", snap.Applied)
	}
}

// --- operational gap (review finding): a failed rejoin must not be silent
// on the console. Rejoin's own return carries no error (layered status,
// matching PullAll/Run), but a boot-time failure to reach ANY configured
// peer must still produce a visible log line — there is no C10 yet to read
// /debug/sync-status instead. ---

func TestRejoinLogsLoudlyWhenAConfiguredPeerIsUnreachable(t *testing.T) {
	cfg := &config.Config{
		SiteID: "site-b", ListenAddr: ":0", NodeEpoch: 1, PeerKey: "demo-key-site-b",
		SyncIntervalSeconds: 10, DBPath: filepath.Join(t.TempDir(), "site-b.db"),
		// Nothing listens here — every exchange to this peer fails.
		Peers: []config.Peer{{SiteID: "site-x", BaseURL: "http://127.0.0.1:1", Key: "k"}},
	}
	st := openStore(t, cfg.DBPath)
	eng := New(cfg, st)

	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(orig)

	eng.Rejoin(context.Background())

	out := buf.String()
	if !strings.Contains(out, "site-x") || !strings.Contains(out, "REJOIN") {
		t.Fatalf("Rejoin against an unreachable peer logged nothing loud mentioning it; got:\n%s", out)
	}
}

// --- re-check requested by review: handleStreams' "always announce my own
// current epoch" fallback (server.go) COULD phantom-announce a stream for a
// FOREIGN site (including a self-history query) at the SERVING peer's own
// node_epoch — reachable via self-history queries since C3b, unlike C3a
// where only own-site queries reached it — until the site == e.cfg.SiteID
// guard landed (Defect 2, design/app-fixes-v1.md §3.1). The "stays inert"
// framing remains true and this test stands as the guard for the
// NONZERO-HEAD ingest vector (a server whose store actually returns foreign
// records — §3.2; the announce guard itself is carried by
// appfix_defect2_test.go's T5): a REAL (non-rigged) server is used, so this
// exercises handleStreams' actual logic, not a scripted stand-in. ---

func TestHandleStreamsOwnEpochAnnounceQuirkStaysInertUnderTheEpochBound(t *testing.T) {
	sites := newMesh(t, 1)
	a := sites[0]
	a.cfg.NodeEpoch = 10 // far above the rejoining site's epoch below; a holds NOTHING for site-b

	blankCfg := &config.Config{
		SiteID: "site-b", ListenAddr: ":0", NodeEpoch: 2, PeerKey: "demo-key-site-b",
		SyncIntervalSeconds: 10, DBPath: filepath.Join(t.TempDir(), "site-b.db"),
		Peers: []config.Peer{{SiteID: "site-a", BaseURL: a.srv.URL, Key: "demo-key-site-a"}},
	}
	blankSt := openStore(t, blankCfg.DBPath)
	blankEng := New(blankCfg, blankSt)
	blankEng.Rejoin(context.Background())

	// The quirk WOULD announce (site-b, epoch 10) with head 0 (a genuinely
	// authored above-our-epoch stream cannot exist); the §3.1 guard now
	// suppresses the announcement entirely (measured: the same query
	// returns []), so these assertions hold vacuously and stand as the
	// ingest-vector guard — confirming no applied records and a healthy
	// read even if a nonzero-head phantom ever reappeared in another form.
	if wm := mustWatermark(t, blankSt, "site-b", 10); wm != 0 {
		t.Fatalf("quirk-announced stream at the peer's own epoch produced applied records: watermark(site-b,10)=%d", wm)
	}
	if snap := blankEng.Snapshot(); snap.Applied != 0 {
		t.Fatalf("applied=%d, want 0 — the quirk's phantom announcement must not become an ingest path", snap.Applied)
	}
}
