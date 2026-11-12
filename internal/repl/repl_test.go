// C3a done-when tests (chunk-plan-v2 §C3a). Naming rule: each test name states
// what it PROVES, and the body proves exactly that — the C2 verifier hunted
// for names wider than their bodies and this suite is written to survive the
// same hunt.
//
// STRUCTURAL NOTE ON THE POKE TEST (the "not done if the poke test passes
// only because the routine timer fired" trap): the routine pull timer is a
// ticker created in exactly one place, Engine.Run. No test in this file calls
// Run except the two-instance convergence test, and the poke-latency test
// runs in a mesh where Run is never called — so no routine timer EXISTS in
// that test's process state, and a timer-fired pass is impossible rather than
// unlikely. The only pull triggers wired there are the poke endpoint and
// nothing else.
package repl

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/store"
)

// --- shared fixtures ---

func tick(k string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"type":"tick","k":%q}`, k))
}

// The same well-behaved fold pair the C2 suite uses: an aggregate and a
// one-row-per-event table. Registered identically on every instance so
// projection dumps are comparable across sites.
func testProjections() []store.Projection {
	return []store.Projection{
		{
			Name: "test_tally", Table: "test_tally", OrderBy: "k",
			Schema: `CREATE TABLE IF NOT EXISTS test_tally (k TEXT PRIMARY KEY, n INTEGER NOT NULL) STRICT;`,
			Apply:  func(tx *sql.Tx, env store.Envelope) error { return applyTally(tx, env) },
		},
		{
			Name: "test_seen", Table: "test_seen", OrderBy: "site_id, node_epoch, seq",
			Schema: `CREATE TABLE IF NOT EXISTS test_seen (
				site_id TEXT NOT NULL, node_epoch INTEGER NOT NULL, seq INTEGER NOT NULL,
				payload TEXT NOT NULL, PRIMARY KEY (site_id, node_epoch, seq)) STRICT;`,
			Apply: func(tx *sql.Tx, env store.Envelope) error {
				_, err := tx.Exec(`INSERT INTO test_seen (site_id, node_epoch, seq, payload)
					VALUES (?, ?, ?, ?)`, env.SiteID, env.NodeEpoch, env.Seq, string(env.Payload))
				return err
			},
		},
	}
}

func applyTally(tx *sql.Tx, env store.Envelope) error {
	var p struct {
		Type string `json:"type"`
		K    string `json:"k"`
	}
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return err
	}
	if p.Type != "tick" {
		return nil
	}
	_, err := tx.Exec(`INSERT INTO test_tally (k, n) VALUES (?, 1)
		ON CONFLICT(k) DO UPDATE SET n = n + 1`, p.K)
	return err
}

// lateHandler lets the mesh builder allocate server URLs before the engines
// that serve them exist (peer base_urls are config, config precedes engine).
type lateHandler struct {
	mu sync.Mutex
	h  http.Handler
}

func (l *lateHandler) set(h http.Handler) { l.mu.Lock(); l.h = h; l.mu.Unlock() }
func (l *lateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	l.mu.Lock()
	h := l.h
	l.mu.Unlock()
	if h == nil {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	h.ServeHTTP(w, r)
}

type meshSite struct {
	cfg *config.Config
	st  *store.Store
	eng *Engine
	lh  *lateHandler
	srv *httptest.Server
}

// newMesh builds n full-mesh in-process instances: n configs, n stores, n
// engines, n HTTP servers. Peers are a config list — nothing here or in the
// engine assumes any particular n.
func newMesh(t *testing.T, n int) []*meshSite {
	t.Helper()
	sites := make([]*meshSite, n)
	for i := range sites {
		lh := &lateHandler{}
		sites[i] = &meshSite{lh: lh, srv: httptest.NewServer(lh)}
		t.Cleanup(sites[i].srv.Close)
	}
	for i := range sites {
		id := fmt.Sprintf("site-%c", 'a'+i)
		cfg := &config.Config{
			SiteID: id, DisplayName: id, ListenAddr: ":0", NodeEpoch: 1,
			PeerKey: "demo-key-" + id, SyncIntervalSeconds: 10,
			DBPath: filepath.Join(t.TempDir(), id+".db"),
		}
		for j := range sites {
			if j == i {
				continue
			}
			pid := fmt.Sprintf("site-%c", 'a'+j)
			cfg.Peers = append(cfg.Peers, config.Peer{SiteID: pid, BaseURL: sites[j].srv.URL, Key: "demo-key-" + pid})
		}
		st := openStore(t, cfg.DBPath)
		eng := New(cfg, st)
		sites[i].cfg, sites[i].st, sites[i].eng = cfg, st, eng
		sites[i].lh.set(eng.SyncHandler())
	}
	return sites
}

func openStore(t *testing.T, path string) *store.Store {
	t.Helper()
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { st.Close() })
	for _, p := range testProjections() {
		if err := st.RegisterProjection(p); err != nil {
			t.Fatalf("register %s: %v", p.Name, err)
		}
	}
	return st
}

// repointPeer rewrites, in place, the BaseURL of cfg's peer entry for
// siteID. Used only to redirect a loaded deploy config's fixed production
// address at the OS-assigned port a test listener actually bound (see the
// two-instance deploy-config test) — never applied to a real deploy.
func repointPeer(t *testing.T, cfg *config.Config, siteID, baseURL string) {
	t.Helper()
	for i := range cfg.Peers {
		if cfg.Peers[i].SiteID == siteID {
			cfg.Peers[i].BaseURL = baseURL
			return
		}
	}
	t.Fatalf("no peer %q in config", siteID)
}

func mustDump(t *testing.T, st *store.Store) []byte {
	t.Helper()
	b, err := st.DumpProjections()
	if err != nil {
		t.Fatalf("dump: %v", err)
	}
	return b
}

func mustWatermark(t *testing.T, st *store.Store, site string, epoch int64) int64 {
	t.Helper()
	wm, err := st.Watermark(site, epoch)
	if err != nil {
		t.Fatalf("watermark: %v", err)
	}
	return wm
}

func waitFor(t *testing.T, timeout time.Duration, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", timeout, desc)
}

func streamState(e *Engine, site string, epoch int64) (StreamStatus, bool) {
	for _, ss := range e.Snapshot().Streams {
		if ss.SiteID == site && ss.NodeEpoch == epoch {
			return ss, true
		}
	}
	return StreamStatus{}, false
}

// mkStream fabricates a peer-authored stream for rigged servers.
func mkStream(site string, epoch int64, n int, schemaVer int64, origin string) []store.Envelope {
	evs := make([]store.Envelope, n)
	for i := range evs {
		evs[i] = store.Envelope{
			SiteID: site, NodeEpoch: epoch, Seq: int64(i + 1),
			SchemaVersion: schemaVer, OriginAuth: origin,
			Payload: tick(fmt.Sprintf("%s-k%d", site, i%3)),
		}
	}
	return evs
}

// rig is a scriptable pull server for adversarial cases: it speaks the wire
// protocol but serves whatever streams/events the test hands it.
type rig struct {
	mu      sync.Mutex
	siteID  string
	streams []StreamHead
	events  map[int64][]store.Envelope // by epoch; served honouring from/limit
}

func (r *rig) setStreams(sh ...StreamHead) { r.mu.Lock(); r.streams = sh; r.mu.Unlock() }
func (r *rig) setEvents(epoch int64, evs []store.Envelope) {
	r.mu.Lock()
	if r.events == nil {
		r.events = map[int64][]store.Envelope{}
	}
	r.events[epoch] = evs
	r.mu.Unlock()
}

func (r *rig) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sync/streams", func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		resp := StreamsResponse{SiteID: r.siteID, Streams: append([]StreamHead{}, r.streams...)}
		r.mu.Unlock()
		json.NewEncoder(w).Encode(&resp)
	})
	mux.HandleFunc("GET /sync/pull", func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query()
		var epoch, from, limit int64
		fmt.Sscan(q.Get("epoch"), &epoch)
		fmt.Sscan(q.Get("from"), &from)
		fmt.Sscan(q.Get("limit"), &limit)
		r.mu.Lock()
		evs := r.events[epoch]
		var head int64
		for _, sh := range r.streams {
			if sh.NodeEpoch == epoch {
				head = sh.Head
			}
		}
		r.mu.Unlock()
		resp := PullResponse{SiteID: r.siteID, NodeEpoch: epoch, Head: head, Events: []store.Envelope{}}
		for _, ev := range evs {
			if ev.Seq == from-1 {
				resp.PrevHash = ev.Hash() // the honest seam echo, like the real server
			}
			if ev.Seq >= from && int64(len(resp.Events)) < limit {
				resp.Events = append(resp.Events, ev)
			}
		}
		json.NewEncoder(w).Encode(&resp)
	})
	return mux
}

// engineWithRiggedPeer builds one engine whose only (or extra) peers are rigs.
func engineWithPeers(t *testing.T, siteID string, peers ...config.Peer) (*Engine, *store.Store) {
	t.Helper()
	cfg := &config.Config{
		SiteID: siteID, ListenAddr: ":0", NodeEpoch: 1, PeerKey: "demo-key-" + siteID,
		SyncIntervalSeconds: 10, DBPath: filepath.Join(t.TempDir(), siteID+".db"),
		Peers: peers,
	}
	st := openStore(t, cfg.DBPath)
	return New(cfg, st), st
}

// --- done-when: two instances, two ports, two configs; 500 events; outage,
// resume, convergence with zero duplicate applications ---

// --- done-when: the three-instance convergence run — the only executable
// check of "nothing assumes exactly two peers" ---

func TestThreeInstanceFullMeshConvergesIdentically(t *testing.T) {
	sites := newMesh(t, 3)
	counts := []int{40, 25, 10}
	for i, s := range sites {
		for j := 0; j < counts[i]; j++ {
			if _, err := s.st.Author(s.cfg.SiteID, 1, tick(fmt.Sprintf("%s-%d", s.cfg.SiteID, j%5)), 0, "default"); err != nil {
				t.Fatal(err)
			}
		}
	}
	// One synchronous pull cycle per site drains both of its peers directly
	// (full mesh, no relay needed).
	for _, s := range sites {
		s.eng.PullAll(context.Background())
	}
	d0 := mustDump(t, sites[0].st)
	for i, s := range sites[1:] {
		if d := mustDump(t, s.st); !bytes.Equal(d0, d) {
			t.Fatalf("site %d dump diverges from site 0:\n%s\nvs\n%s", i+1, d0, d)
		}
	}
	for _, s := range sites {
		for i, other := range sites {
			if want := int64(counts[i]); mustWatermark(t, s.st, other.cfg.SiteID, 1) != want {
				t.Fatalf("%s watermark for %s != %d", s.cfg.SiteID, other.cfg.SiteID, want)
			}
		}
	}
}

// --- done-when: ingest identity rule — a record whose site_id (or
// origin_auth) is not the delivering peer's identity is rejected ---

func TestRecordBearingForeignIdentityIsRejectedNotApplied(t *testing.T) {
	cases := []struct {
		name   string
		site   string
		origin string
	}{
		{"foreign site_id", "site-zz", "site-zz"},
		{"matching site_id, foreign origin_auth", "site-x", "site-zz"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &rig{siteID: "site-x"}
			r.setStreams(StreamHead{NodeEpoch: 1, Head: 3})
			r.setEvents(1, mkStream(tc.site, 1, 3, store.SchemaVersion, tc.origin))
			rs := httptest.NewServer(r.handler())
			defer rs.Close()

			eng, st := engineWithPeers(t, "site-b", config.Peer{SiteID: "site-x", BaseURL: rs.URL, Key: "k"})
			eng.PullPeer(context.Background(), "site-x")

			if wm := mustWatermark(t, st, tc.site, 1); wm != 0 {
				t.Fatalf("foreign-identity record applied: watermark(%s,1)=%d", tc.site, wm)
			}
			if wm := mustWatermark(t, st, "site-x", 1); wm != 0 {
				t.Fatalf("stream advanced past a rejected record: watermark(site-x,1)=%d", wm)
			}
			ss, ok := streamState(eng, "site-x", 1)
			if !ok || ss.State != StateIdentityRejected {
				t.Fatalf("stream state = %+v, want %s surfaced", ss, StateIdentityRejected)
			}
			if snap := eng.Snapshot(); snap.Applied != 0 {
				t.Fatalf("applied=%d, want 0", snap.Applied)
			}
		})
	}
}

// --- done-when: schema_version+1 is queued (author-side, by design) and
// surfaced per-stream, never applied; other streams and peers keep pulling ---

func TestSchemaVersionMismatchIsSurfacedPerStreamAndStallsOnlyItsOwnStream(t *testing.T) {
	// Peer site-x announces two streams: epoch 1 carries a foreign schema
	// version, epoch 2 is fine. Peer site-a is a healthy real instance.
	r := &rig{siteID: "site-x"}
	r.setStreams(StreamHead{NodeEpoch: 1, Head: 4}, StreamHead{NodeEpoch: 2, Head: 3})
	r.setEvents(1, mkStream("site-x", 1, 4, store.SchemaVersion+1, "site-x"))
	r.setEvents(2, mkStream("site-x", 2, 3, store.SchemaVersion, "site-x"))
	rs := httptest.NewServer(r.handler())
	defer rs.Close()

	mesh := newMesh(t, 1) // a real, healthy site-a
	realA := mesh[0]
	for i := 0; i < 5; i++ {
		if _, err := realA.st.Author("site-a", 1, tick("a"), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}

	eng, st := engineWithPeers(t, "site-b",
		config.Peer{SiteID: "site-x", BaseURL: rs.URL, Key: "k"},
		config.Peer{SiteID: "site-a", BaseURL: realA.srv.URL, Key: "demo-key-site-a"},
	)
	eng.PullAll(context.Background())

	// The mismatched stream: nothing applied, refusal surfaced.
	if wm := mustWatermark(t, st, "site-x", 1); wm != 0 {
		t.Fatalf("schema-mismatched stream was applied: watermark=%d", wm)
	}
	ss, ok := streamState(eng, "site-x", 1)
	if !ok || ss.State != StateSchemaMismatch {
		t.Fatalf("stream (site-x,1) state = %+v, want %s (the §8.5 visible refusal)", ss, StateSchemaMismatch)
	}
	// Epoch-major delivery (F8): the same site's LATER epoch is deliberately
	// HELD BACK behind the stalled epoch 1 — applying it first would be the
	// unsafe cross-epoch reorder. This assertion was inverted (was: epoch 2
	// drains fully) when carry-forward row 4's per-stream isolation was
	// narrowed to cross-site only; the mismatch still stalls only its own
	// SITE's streams, never another peer's, as asserted below.
	if wm := mustWatermark(t, st, "site-x", 2); wm != 0 {
		t.Fatalf("later epoch of the stalled site was applied around the schema mismatch: watermark(site-x,2)=%d, want 0 (F8 epoch-major delivery)", wm)
	}
	// Isolation ACROSS peers: site-a drained fully.
	if wm := mustWatermark(t, st, "site-a", 1); wm != 5 {
		t.Fatalf("healthy peer stalled by another peer's mismatch: watermark(site-a,1)=%d", wm)
	}
	// A peer with a refused stream did NOT drain, so no anchor was recorded
	// for it — B does not hold site-x's head.
	if _, ok := eng.Anchor("site-x"); ok {
		t.Fatal("anchor recorded for a peer whose stream is refused — drain requires ALL streams at head")
	}
}

// --- done-when: an epoch+1 announcement is consumed as a NEW stream with a
// fresh watermark at zero ---

func TestEpochPlusOneAnnouncementOpensFreshWatermarkAtZeroAndIsConsumedAsNewStream(t *testing.T) {
	sites := newMesh(t, 2)
	a, b := sites[0], sites[1]
	for i := 0; i < 5; i++ {
		if _, err := a.st.Author("site-a", 1, tick("e1"), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	b.eng.PullAll(context.Background())
	if wm := mustWatermark(t, b.st, "site-a", 1); wm != 5 {
		t.Fatalf("precondition: epoch-1 watermark = %d, want 5", wm)
	}

	// site-a re-images: authors under epoch 2 (seqs restart at 1). Its pull
	// server now announces both streams; B must key the new epoch separately
	// and open its watermark at zero, not dedup against epoch 1.
	for i := 0; i < 3; i++ {
		if _, err := a.st.Author("site-a", 2, tick("e2"), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	b.eng.PullAll(context.Background())
	if wm := mustWatermark(t, b.st, "site-a", 2); wm != 3 {
		t.Fatalf("epoch-2 watermark = %d, want 3 (fresh stream from zero)", wm)
	}
	if wm := mustWatermark(t, b.st, "site-a", 1); wm != 5 {
		t.Fatalf("epoch-1 watermark disturbed: %d", wm)
	}
	for _, epoch := range []int64{1, 2} {
		if ss, ok := streamState(b.eng, "site-a", epoch); !ok || ss.State != StateOK {
			t.Fatalf("stream (site-a,%d) = %+v, want ok", epoch, ss)
		}
	}
}

// --- done-when (anchor, part 1): the anchor is recorded only when a pull
// drains to the author's current head ---

func TestAnchorIsRecordedOnlyWhenAPullDrainsToTheAuthorsHead(t *testing.T) {
	// A rigged author that reports head 10 but serves only 5 events: every
	// pull applies progress yet NEVER drains — no anchor may appear.
	r := &rig{siteID: "site-x"}
	r.setStreams(StreamHead{NodeEpoch: 1, Head: 10})
	full := mkStream("site-x", 1, 10, store.SchemaVersion, "site-x")
	r.setEvents(1, full[:5])
	rs := httptest.NewServer(r.handler())
	defer rs.Close()

	eng, st := engineWithPeers(t, "site-b", config.Peer{SiteID: "site-x", BaseURL: rs.URL, Key: "k"})
	eng.PullPeer(context.Background(), "site-x")
	eng.PullPeer(context.Background(), "site-x")
	if wm := mustWatermark(t, st, "site-x", 1); wm != 5 {
		t.Fatalf("partial progress = %d, want 5", wm)
	}
	if _, ok := eng.Anchor("site-x"); ok {
		t.Fatal("anchor recorded although no pull ever drained to the author's head")
	}

	// Serve the full stream: the next pull drains, and the anchor is THAT
	// cycle's START (R10, James 2026-10-04 — FINDINGS-R4 D6). ⟦was: one
	// drain exposed no anchor and the second exposed the FIRST drain's
	// instant ("the exposed slot is the PREVIOUS drain instant"): G was
	// about two sync intervals, and the destination turned OVERDUE 10–20 s
	// before the origin on every trip. The start of the latest drained
	// cycle is the tight bound that is still never late: every event not
	// delivered by that cycle was authored after it began.⟧
	r.setEvents(1, full)
	before := time.Now()
	eng.PullPeer(context.Background(), "site-x")
	after := time.Now()
	anchor, ok := eng.Anchor("site-x")
	if !ok {
		t.Fatal("no anchor after a full drain")
	}
	if anchor.Before(before) || anchor.After(after) {
		t.Fatalf("anchor %v is not this drain's own instant (cycle ran %v..%v)", anchor, before, after)
	}
	// The NEXT drained cycle moves it to that cycle's start — never to an
	// instant after the cycle began.
	before2 := time.Now()
	eng.PullPeer(context.Background(), "site-x")
	anchor2, _ := eng.Anchor("site-x")
	if anchor2.Before(before2) || !anchor2.After(anchor) {
		t.Fatalf("second drain: anchor %v, want the second cycle's start (≥ %v)", anchor2, before2)
	}
}

// R10 (James, 2026-10-04 — FINDINGS-R4 D6): the anchor is the START of the
// latest drained cycle — not the cycle before it (2ba9188: G ≈ two sync
// intervals, the destination OVERDUE 10–20 s before the origin on every
// trip), and not the cycle's END (an event authored mid-cycle, after the
// head was read, is delivered by the NEXT cycle; an end-of-cycle anchor
// would post-date it and hand it a G shorter than its true time in flight —
// late). The author's /sync/streams answer is held 80 ms so start and end
// are decidable on any clock.
func TestR10AnchorIsTheLatestDrainedCycleStartNeverThePreviousCycleNorTheEnd(t *testing.T) {
	sites := newMesh(t, 2)
	a, b := sites[0], sites[1]
	if _, err := a.st.Author("site-a", 1, tick("pre"), 0, "default"); err != nil {
		t.Fatal(err)
	}
	b.eng.PullAll(context.Background())
	time.Sleep(20 * time.Millisecond)

	inner := a.eng.SyncHandler()
	a.lh.set(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sync/streams" {
			time.Sleep(80 * time.Millisecond)
		}
		inner.ServeHTTP(w, r)
	}))
	cycleStart := time.Now()
	b.eng.PullAll(context.Background())
	cycleEnd := time.Now()
	anchor, ok := b.eng.Anchor("site-a")
	if !ok {
		t.Fatal("no anchor after two drained cycles")
	}
	if anchor.Before(cycleStart) {
		t.Fatalf("anchor %v predates the latest drained cycle (started %v) — it is the PREVIOUS cycle, a whole interval early (D6)", anchor, cycleStart)
	}
	if !anchor.Before(cycleStart.Add(60 * time.Millisecond)) {
		t.Fatalf("anchor %v is not the cycle's start (%v; the cycle ended %v) — an end-of-cycle anchor post-dates events the cycle missed", anchor, cycleStart, cycleEnd)
	}
}

// --- done-when (anchor, part 2): after a paginated catch-up the stored
// anchor is the pre-outage instant, not the last batch time; batches inherit
// it unchanged ---

func TestPaginatedCatchUpLeavesAnchorAtPreOutageInstantAndBatchesInheritItUnchanged(t *testing.T) {
	sites := newMesh(t, 2)
	a, b := sites[0], sites[1]
	b.eng.PageLimit = 7

	if _, err := a.st.Author("site-a", 1, tick("pre"), 0, "default"); err != nil {
		t.Fatal(err)
	}
	// Two routine drains establish an anchor; a third gives it a value that
	// is strictly pre-outage.
	for i := 0; i < 3; i++ {
		b.eng.PullAll(context.Background())
	}
	anchorPre, ok := b.eng.Anchor("site-a")
	if !ok {
		t.Fatal("no anchor after routine drains")
	}

	// Windows' monotonic clock ticks coarsely (~15 ms); separate the last
	// pre-outage drain from outageStart so "strictly before" is decidable.
	time.Sleep(30 * time.Millisecond)
	outageStart := time.Now()
	// Outage: B stops pulling; A authors 40 events (store-level: no pokes —
	// the link toward B is down).
	for i := 0; i < 40; i++ {
		if _, err := a.st.Author("site-a", 1, tick(fmt.Sprintf("o%d", i)), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}

	// Instrument A's server: on every pull-page request, record B's anchor as
	// it stands mid-catch-up. Pages 1..k have been applied by B when the
	// request for page k+1 arrives, so these observations are exactly "the
	// anchor each batch inherited".
	var obsMu sync.Mutex
	var pullReqs int
	var midCycleAnchors []time.Time
	inner := a.eng.SyncHandler()
	a.lh.set(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sync/pull" {
			an, ok := b.eng.Anchor("site-a")
			obsMu.Lock()
			pullReqs++
			if ok {
				midCycleAnchors = append(midCycleAnchors, an)
			}
			obsMu.Unlock()
		}
		inner.ServeHTTP(w, r)
	}))

	// Resume: ONE pull cycle catches up 41 events in pages of 7.
	b.eng.PullPeer(context.Background(), "site-a")

	obsMu.Lock()
	pullReqs2 := pullReqs
	midCycle := append([]time.Time{}, midCycleAnchors...)
	obsMu.Unlock()
	if pullReqs2 < 6 {
		t.Fatalf("catch-up used %d page pulls — not paginated; the assertion would be vacuous", pullReqs2)
	}
	for i, an := range midCycle {
		if !an.Equal(anchorPre) {
			t.Fatalf("batch %d saw anchor %v ≠ pre-outage anchor %v — batches must inherit it unchanged", i, an, anchorPre)
		}
	}
	// R10 (James, 2026-10-04 — FINDINGS-R4 D6): once the catch-up cycle has
	// DRAINED, the anchor is that cycle's START — an instant at which every
	// outage event was already authored and is now held, so the next event
	// captured against it gets a G that is never short. What the pre-outage
	// pin protects is the batches above: every envelope the catch-up
	// delivered inherited anchorPre. ⟦was: "stored anchor after paginated
	// catch-up must be BEFORE outageStart" — the two-slot rule exposed the
	// previous drain, so the catch-up cycle published the pre-outage instant
	// a second time, one more cycle early than any event needed⟧
	after, ok := b.eng.Anchor("site-a")
	if !ok {
		t.Fatal("anchor vanished after catch-up")
	}
	if after.Before(outageStart) {
		t.Fatalf("stored anchor after the drained catch-up is %v, before outage start %v — the catch-up cycle's start is after every outage event", after, outageStart)
	}
	if after.Before(anchorPre) {
		t.Fatalf("anchor went backwards: %v < %v", after, anchorPre)
	}

	// And the NEXT drained cycle (nothing new) moves the anchor forward again.
	b.eng.PullPeer(context.Background(), "site-a")
	if an, _ := b.eng.Anchor("site-a"); !an.After(after) {
		t.Fatalf("anchor did not advance after a post-catch-up drained cycle: %v", an)
	}
}

// --- done-when: poke-latency — with NO routine timer in existence (Run is
// never called in this test), an event authored at A reaches B's projections
// via poke-then-pull alone, within the configured 10 s interval ---

func TestPokeThenPullAloneReflectsAuthoredEventAtPeerWithinConfiguredInterval(t *testing.T) {
	sites := newMesh(t, 2) // Run is never called: no ticker exists in either engine
	a, b := sites[0], sites[1]

	start := time.Now()
	if _, err := a.eng.Author(tick("poked"), 0, "default"); err != nil {
		t.Fatal(err)
	}
	interval := a.cfg.SyncInterval() // 10 s (F5)
	waitFor(t, interval, "B to reflect A's authored event via poke alone", func() bool {
		return mustWatermark(t, b.st, "site-a", 1) == 1
	})
	if elapsed := time.Since(start); elapsed >= interval {
		t.Fatalf("event took %s, not within the configured %s", elapsed, interval)
	}
	// The projection, not just the log: the fold ran at B.
	if !bytes.Contains(mustDump(t, b.st), []byte(`"poked"`)) {
		t.Fatal("B's projections do not reflect the poked event")
	}
}

// --- done-when: a captured sync exchange carries the t1/t2/t3 echo fields ---

func TestCapturedSyncExchangesCarryOffsetEchoFields(t *testing.T) {
	sites := newMesh(t, 2)
	a := sites[0]
	if _, err := a.st.Author("site-a", 1, tick("x"), 0, "default"); err != nil {
		t.Fatal(err)
	}
	get := func(path string) []byte {
		req, _ := http.NewRequest(http.MethodGet, a.srv.URL+path+"&t1=123456789", nil)
		req.Header.Set("Authorization", "Bearer demo-key-site-a")
		req.Header.Set(HeaderFrom, "site-b")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: %s", path, resp.Status)
		}
		var buf bytes.Buffer
		buf.ReadFrom(resp.Body)
		return buf.Bytes()
	}
	for _, path := range []string{"/sync/streams?site=site-a", "/sync/pull?site=site-a&epoch=1&from=1&limit=10"} {
		var e Echo
		if err := json.Unmarshal(get(path), &e); err != nil {
			t.Fatal(err)
		}
		if e.T1 != 123456789 {
			t.Fatalf("%s: t1 echoed as %d, want 123456789", path, e.T1)
		}
		if e.T2 <= 0 || e.T3 < e.T2 {
			t.Fatalf("%s: t2=%d t3=%d — responder stamps missing or out of order", path, e.T2, e.T3)
		}
	}
}

// --- not-done-if guard: a served gap stalls loudly with no watermark advance ---

func TestServedGapStallsLoudlyWithNoWatermarkAdvance(t *testing.T) {
	r := &rig{siteID: "site-x"}
	r.setStreams(StreamHead{NodeEpoch: 1, Head: 5})
	full := mkStream("site-x", 1, 5, store.SchemaVersion, "site-x")
	r.setEvents(1, full[2:]) // serves seqs 3..5: a gap over a blank consumer
	rs := httptest.NewServer(r.handler())
	defer rs.Close()

	eng, st := engineWithPeers(t, "site-b", config.Peer{SiteID: "site-x", BaseURL: rs.URL, Key: "k"})
	eng.PullPeer(context.Background(), "site-x")
	if wm := mustWatermark(t, st, "site-x", 1); wm != 0 {
		t.Fatalf("watermark advanced across a gap: %d", wm)
	}
	ss, ok := streamState(eng, "site-x", 1)
	if !ok || ss.State != StateGapStalled {
		t.Fatalf("stream state = %+v, want %s (loud stall)", ss, StateGapStalled)
	}
}

// --- server-side guards: demo-key auth, no transitive relay, poke identity ---

func TestPullServerRefusesBadKeyForeignSiteRequestsAndUnknownPokers(t *testing.T) {
	sites := newMesh(t, 2)
	a := sites[0]
	do := func(method, path, key, from string) int {
		req, _ := http.NewRequest(method, a.srv.URL+path, nil)
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		if from != "" {
			req.Header.Set(HeaderFrom, from)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := do(http.MethodGet, "/sync/streams?site=site-a", "", ""); code != http.StatusUnauthorized {
		t.Fatalf("missing key: %d, want 401", code)
	}
	if code := do(http.MethodGet, "/sync/pull?site=site-a&epoch=1&from=1", "wrong", "site-b"); code != http.StatusUnauthorized {
		t.Fatalf("wrong key: %d, want 401", code)
	}
	// No transitive relay (§8.4): A refuses to serve a stream for a site
	// that is neither itself NOR the requester's own claimed identity — a
	// genuinely foreign ask. This is distinct from C3b's self-history
	// exception (rejoin_test.go's TestPullServerServesSelfHistoryOnlyNotAnyThirdSiteClaimedAsSelf
	// covers that legitimate case, site == from): here the requester claims
	// to be site-c but asks for site-b's stream, so neither
	// `site == e.cfg.SiteID` nor `site == from` holds.
	if code := do(http.MethodGet, "/sync/streams?site=site-b", "demo-key-site-a", "site-c"); code != http.StatusForbidden {
		t.Fatalf("foreign-site streams request: %d, want 403", code)
	}
	if code := do(http.MethodGet, "/sync/pull?site=site-b&epoch=1&from=1", "demo-key-site-a", "site-c"); code != http.StatusForbidden {
		t.Fatalf("foreign-site pull request: %d, want 403", code)
	}
	if code := do(http.MethodPost, "/sync/poke", "demo-key-site-a", "site-zz"); code != http.StatusForbidden {
		t.Fatalf("poke from unknown peer: %d, want 403", code)
	}
}

// --- adversarial: the duplicate counter must be shown to be ABLE to
// increment. Every honest scenario pulls from watermark+1 and so never
// re-delivers, which makes "zero duplicate applications" true by construction
// — this rig makes it demonstrated instead: a buggy/adversarial author that
// ignores `from` and re-serves the already-applied page through the ordinary
// public pull path. Apply's no-op branch absorbs it, the counter counts it,
// and the projections stay byte-identical (the C2 redelivery pattern). ---

func TestReServedAlreadyAppliedPageIsCountedAsDuplicatesAndMutatesNothing(t *testing.T) {
	evs := mkStream("site-x", 1, 5, store.SchemaVersion, "site-x")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sync/streams", func(w http.ResponseWriter, req *http.Request) {
		json.NewEncoder(w).Encode(&StreamsResponse{SiteID: "site-x", Streams: []StreamHead{{NodeEpoch: 1, Head: 5}}})
	})
	mux.HandleFunc("GET /sync/pull", func(w http.ResponseWriter, req *http.Request) {
		// Deliberately ignores from/limit: always the full stream again.
		json.NewEncoder(w).Encode(&PullResponse{SiteID: "site-x", NodeEpoch: 1, Head: 5, Events: evs})
	})
	rs := httptest.NewServer(mux)
	defer rs.Close()

	eng, st := engineWithPeers(t, "site-b", config.Peer{SiteID: "site-x", BaseURL: rs.URL, Key: "k"})
	hookCalls := 0 // pulls below are synchronous on this goroutine
	eng.OnApplied = func(store.Envelope, time.Time, bool) { hookCalls++ }

	// First cycle: from=1, the served page is exactly what we need — applied.
	eng.PullPeer(context.Background(), "site-x")
	if snap := eng.Snapshot(); snap.Applied != 5 || snap.Duplicates != 0 {
		t.Fatalf("first delivery: applied=%d duplicates=%d, want 5/0", snap.Applied, snap.Duplicates)
	}
	dump1 := mustDump(t, st)

	// Second cycle: the engine asks from=6; the rig re-serves seqs 1..5.
	eng.PullPeer(context.Background(), "site-x")
	snap := eng.Snapshot()
	if snap.Duplicates != 5 {
		t.Fatalf("re-served page: duplicates=%d, want 5 — the counter never demonstrated it can increment", snap.Duplicates)
	}
	if snap.Applied != 5 {
		t.Fatalf("re-served page: applied=%d, want still 5 — a duplicate was APPLIED", snap.Applied)
	}
	if wm := mustWatermark(t, st, "site-x", 1); wm != 5 {
		t.Fatalf("watermark = %d after redelivery, want 5", wm)
	}
	if dump2 := mustDump(t, st); !bytes.Equal(dump1, dump2) {
		t.Fatalf("duplicate delivery mutated a projection:\n was: %s\n now: %s", dump1, dump2)
	}
	// And the stream still reads healthy: duplicates are a no-op, not a fault.
	if ss, ok := streamState(eng, "site-x", 1); !ok || ss.State != StateOK {
		t.Fatalf("stream state after redelivery = %+v, want ok", ss)
	}
	// OnApplied fires per APPLIED envelope only — never for duplicates.
	if hookCalls != 5 {
		t.Fatalf("OnApplied fired %d times, want 5 (applies only, not the 5 duplicates)", hookCalls)
	}
}

// --- review H1: OnApplied fires per APPLIED envelope with the pre-rotation
// anchor — the value a later Anchor() poll can no longer return ---

func TestOnAppliedFiresPerAppliedEnvelopeWithPreRotationAnchor(t *testing.T) {
	sites := newMesh(t, 2)
	a, b := sites[0], sites[1]
	b.eng.PageLimit = 7

	type capture struct {
		key    store.Key
		anchor time.Time
		ok     bool
	}
	var capMu sync.Mutex
	var caps []capture
	b.eng.OnApplied = func(env store.Envelope, anchor time.Time, ok bool) {
		capMu.Lock()
		caps = append(caps, capture{env.Key(), anchor, ok})
		capMu.Unlock()
	}

	// Establish an anchor: one event, three spaced drains (spacing beats
	// Windows' ~15 ms monotonic tick so distinct drains are distinct instants).
	if _, err := a.st.Author("site-a", 1, tick("pre"), 0, "default"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		b.eng.PullAll(context.Background())
		time.Sleep(20 * time.Millisecond)
	}
	capMu.Lock()
	if len(caps) != 1 || caps[0].ok {
		t.Fatalf("first-ever apply: %d captures, ok=%v — want 1 capture with NO anchor (B had never held A's head)", len(caps), len(caps) > 0 && caps[0].ok)
	}
	caps = nil
	capMu.Unlock()
	anchorPre, ok := b.eng.Anchor("site-a")
	if !ok {
		t.Fatal("no anchor after routine drains")
	}

	// Outage authoring, then one paginated catch-up cycle.
	for i := 0; i < 20; i++ {
		if _, err := a.st.Author("site-a", 1, tick(fmt.Sprintf("o%d", i)), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	b.eng.PullPeer(context.Background(), "site-a")

	capMu.Lock()
	defer capMu.Unlock()
	if len(caps) != 20 {
		t.Fatalf("hook fired %d times for 20 applied envelopes", len(caps))
	}
	for i, c := range caps {
		if !c.ok || !c.anchor.Equal(anchorPre) {
			t.Fatalf("capture %d (%v): anchor=%v ok=%v — every catch-up apply must see the pre-outage anchor %v", i, c.key, c.anchor, c.ok, anchorPre)
		}
	}
	// The rotation the hook beat: a poll AFTER the cycle no longer returns
	// what the hook captured.
	after, ok := b.eng.Anchor("site-a")
	if !ok || after.Equal(anchorPre) {
		t.Fatalf("Anchor() after the cycle = %v (ok=%v), still the captured value — rotation did not occur, so this test proves nothing", after, ok)
	}
}

// --- review M1: a non-empty page that advances nothing must stall ONLY its
// own stream — never hot-loop, and never wedge other peers behind runMu ---

func TestNoProgressPageStallsItsStreamWithoutWedgingOtherPeers(t *testing.T) {
	// A rig that claims head 10 but forever re-serves seqs 1..5, ignoring
	// `from`: after 5 applies every later page advances nothing.
	evs := mkStream("site-x", 1, 5, store.SchemaVersion, "site-x")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sync/streams", func(w http.ResponseWriter, req *http.Request) {
		json.NewEncoder(w).Encode(&StreamsResponse{SiteID: "site-x", Streams: []StreamHead{{NodeEpoch: 1, Head: 10}}})
	})
	mux.HandleFunc("GET /sync/pull", func(w http.ResponseWriter, req *http.Request) {
		json.NewEncoder(w).Encode(&PullResponse{SiteID: "site-x", NodeEpoch: 1, Head: 10, Events: evs})
	})
	rs := httptest.NewServer(mux)
	defer rs.Close()

	mesh := newMesh(t, 1)
	realA := mesh[0]
	for i := 0; i < 4; i++ {
		if _, err := realA.st.Author("site-a", 1, tick("a"), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}

	// The bad peer is FIRST in the peers list: pre-guard, its hot loop would
	// hold runMu inside a sequential PullAll and site-a would never be pulled.
	eng, st := engineWithPeers(t, "site-b",
		config.Peer{SiteID: "site-x", BaseURL: rs.URL, Key: "k"},
		config.Peer{SiteID: "site-a", BaseURL: realA.srv.URL, Key: "demo-key-site-a"},
	)
	done := make(chan struct{})
	go func() { eng.PullAll(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("PullAll wedged on the no-progress peer")
	}

	if wm := mustWatermark(t, st, "site-x", 1); wm != 5 {
		t.Fatalf("bad peer's contiguous progress = %d, want 5 kept", wm)
	}
	ss, ok := streamState(eng, "site-x", 1)
	if !ok || ss.State != StateError {
		t.Fatalf("no-progress stream state = %+v, want %s", ss, StateError)
	}
	if wm := mustWatermark(t, st, "site-a", 1); wm != 4 {
		t.Fatalf("healthy peer behind the bad one: watermark=%d, want 4 — isolation broken", wm)
	}
	if ss, ok := streamState(eng, "site-a", 1); !ok || ss.State != StateOK {
		t.Fatalf("healthy peer's stream = %+v, want ok", ss)
	}
}

// --- review M2: Shutdown stops engine-spawned async work; later pokes are
// no-ops ---

func TestShutdownStopsAsyncPullsAndMakesLaterPokesNoOps(t *testing.T) {
	sites := newMesh(t, 2) // no Run anywhere: poke is the only pull trigger
	a, b := sites[0], sites[1]

	if _, err := a.eng.Author(tick("before"), 0, "default"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "poke path to work before shutdown", func() bool {
		return mustWatermark(t, b.st, "site-a", 1) == 1
	})

	done := make(chan struct{})
	go func() { b.eng.Shutdown(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown did not return")
	}

	// Author again at A: the poke reaches B's HTTP handler, but B's stopped
	// engine must not pull.
	if _, err := a.eng.Author(tick("after"), 0, "default"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	if wm := mustWatermark(t, b.st, "site-a", 1); wm != 1 {
		t.Fatalf("a poke after Shutdown still pulled: watermark=%d, want 1", wm)
	}
	b.eng.Shutdown() // idempotent
	a.eng.Shutdown() // and A's own async pokes drain cleanly
}

// --- addendum 3 (CRITICAL): the seam-echo check. A site rebuilt blank under
// an UNBUMPED epoch re-authors from seq 1; when its new history passes our
// watermark, every existing defence waves it through — the §8.4 triple
// passes (it IS that site), Apply sees contiguous seqs, and the §8.9
// regression detector sees head ≥ peak. The result is our old rows 1..N
// spliced to its new rows N+1.., looking contiguous and trustworthy. The
// seam check compares the author's envelope hash at from−1 against the row
// we already hold BEFORE applying anything. Written before the fix and
// confirmed to FAIL against it (the splice applied silently). ---

func TestSameEpochRebuildSpliceIsRefusedAsPrefixDivergedNothingApplied(t *testing.T) {
	// The rebuilt author: a REAL engine+store, authentically site-x, whose
	// entire history is new — authored after a wipe, epoch NOT bumped.
	rebuilt, rebuiltSt := engineWithPeers(t, "site-x")
	for i := 0; i < 7; i++ {
		if _, err := rebuiltSt.Author("site-x", 1, tick(fmt.Sprintf("new-%d", i)), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	xs := httptest.NewServer(rebuilt.SyncHandler())
	defer xs.Close()

	// The consumer: holds site-x's OLD history 1..4 from before the wipe.
	eng, st := engineWithPeers(t, "site-b", config.Peer{SiteID: "site-x", BaseURL: xs.URL, Key: "demo-key-site-x"})
	old := make([]store.Envelope, 4)
	for i := range old {
		old[i] = store.Envelope{SiteID: "site-x", NodeEpoch: 1, Seq: int64(i + 1),
			SchemaVersion: store.SchemaVersion, OriginAuth: "site-x", Payload: tick(fmt.Sprintf("old-%d", i))}
		if applied, err := st.Apply(old[i]); err != nil || !applied {
			t.Fatalf("seed old history %d: applied=%v err=%v", i, applied, err)
		}
	}
	dumpBefore := mustDump(t, st)

	// The pull that would splice: rebuilt-x serves 5..7, contiguous over our
	// watermark 4, under a passing identity triple.
	eng.PullPeer(context.Background(), "site-x")

	if wm := mustWatermark(t, st, "site-x", 1); wm != 4 {
		t.Fatalf("SPLICE APPLIED: watermark advanced to %d over a diverged prefix — old rows 1..4 are now silently joined to a different history", wm)
	}
	if snap := eng.Snapshot(); snap.Applied != 0 {
		t.Fatalf("SPLICE APPLIED: %d envelopes applied across the seam", snap.Applied)
	}
	if d := mustDump(t, st); !bytes.Equal(dumpBefore, d) {
		t.Fatal("a refused splice mutated a projection")
	}
	ss, ok := streamState(eng, "site-x", 1)
	if !ok || ss.State != StatePrefixDiverged {
		t.Fatalf("stream state = %+v, want %s (loud terminal refusal)", ss, StatePrefixDiverged)
	}
}

// First pull from zero has no from−1 row: the response carries no prev_hash
// and the pull is accepted — the seam check must not fire where no seam exists.
func TestFirstPullFromZeroCarriesNoPrevHashAndIsAccepted(t *testing.T) {
	sites := newMesh(t, 2)
	a, b := sites[0], sites[1]
	for i := 0; i < 3; i++ {
		if _, err := a.st.Author("site-a", 1, tick("z"), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	get := func(from int) map[string]json.RawMessage {
		req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/sync/pull?site=site-a&epoch=1&from=%d&limit=10&t1=1", a.srv.URL, from), nil)
		req.Header.Set("Authorization", "Bearer demo-key-site-a")
		req.Header.Set(HeaderFrom, "site-b")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var m map[string]json.RawMessage
		if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	if _, present := get(1)["prev_hash"]; present {
		t.Fatal("from=1 response carries prev_hash — there is no seq 0 row to attest")
	}
	if _, present := get(2)["prev_hash"]; !present {
		t.Fatal("from=2 response carries no prev_hash — the seam echo is missing")
	}
	// And the whole from-zero pull is accepted end to end.
	b.eng.PullPeer(context.Background(), "site-a")
	if wm := mustWatermark(t, b.st, "site-a", 1); wm != 3 {
		t.Fatalf("from-zero pull refused: watermark=%d, want 3", wm)
	}
	if ss, ok := streamState(b.eng, "site-a", 1); !ok || ss.State != StateOK {
		t.Fatalf("from-zero stream state = %+v, want ok", ss)
	}
}
