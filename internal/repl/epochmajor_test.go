// epochmajor_test.go — F8 regression cover: a site's epochs are delivered
// epoch-major. drainSite used to continue past a failed epoch, so a transient
// failure on an epoch-1 pull let epoch-2 events of the SAME site apply first —
// the cross-epoch same-site reorder that left a departed person on a replica's
// muster (visit created in epoch 1, checked out in epoch 2: the checkout fold
// no-ops against the missing row, the later arrival inserts is_open=1) while
// sync status read ok. These tests pin the fix and the one property the old
// row-4 isolation existed to protect: one bad PEER still never stalls a good
// peer.
package repl

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/store"
)

// One transient blip on the FIRST epoch-1 pull during a multi-epoch catch-up
// (the replica re-image scenario, through the real pull path — engines and
// HTTP are honest, only the transport is rigged) must hold the author's later
// epochs back, then heal to full convergence on the next routine pull.
func TestTransientEarlyEpochPullFailureHoldsBackLaterEpochsThenHealsToConvergence(t *testing.T) {
	ctx := context.Background()

	// A real author whose log spans two epochs (a past re-image).
	aCfg := &config.Config{
		SiteID: "site-a", DisplayName: "site-a", ListenAddr: ":0", NodeEpoch: 2,
		PeerKey: "demo-key-site-a", SyncIntervalSeconds: 10,
		DBPath: filepath.Join(t.TempDir(), "site-a.db"),
	}
	aSt := openStore(t, aCfg.DBPath)
	for _, k := range []string{"e1-x", "e1-y"} {
		if _, err := aSt.Author("site-a", 1, tick(k), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	for _, k := range []string{"e2-x", "e2-y"} {
		if _, err := aSt.Author("site-a", 2, tick(k), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	aEng := New(aCfg, aSt)
	aSrv := httptest.NewServer(aEng.SyncHandler())
	defer aSrv.Close()

	// The rigged transport: fail exactly the FIRST /sync/pull for epoch 1
	// with a 502; pass every other exchange through untouched.
	aURL, err := url.Parse(aSrv.URL)
	if err != nil {
		t.Fatal(err)
	}
	fwd := httputil.NewSingleHostReverseProxy(aURL)
	var blipped atomic.Bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sync/pull" && r.URL.Query().Get("epoch") == "1" && blipped.CompareAndSwap(false, true) {
			http.Error(w, "bad gateway", http.StatusBadGateway)
			return
		}
		fwd.ServeHTTP(w, r)
	}))
	defer proxy.Close()

	bEng, bSt := engineWithPeers(t, "site-b",
		config.Peer{SiteID: "site-a", BaseURL: proxy.URL, Key: "demo-key-site-a"})

	// The blipped catch-up: epoch 1 fails once, epoch 2 is reachable.
	bEng.PullAll(ctx)
	if wm := mustWatermark(t, bSt, "site-a", 1); wm != 0 {
		t.Fatalf("epoch-1 watermark after blip = %d, want 0 (the pull failed)", wm)
	}
	// THE F8 ASSERTION: epoch 2 must be held back behind the failed epoch 1,
	// not pulled around it. Pre-fix this read 2 — the cross-epoch reorder.
	if wm := mustWatermark(t, bSt, "site-a", 2); wm != 0 {
		t.Fatalf("epoch-2 watermark after an epoch-1 blip = %d, want 0 — later epoch applied before an earlier one (F8)", wm)
	}
	if !blipped.Load() {
		t.Fatal("test premise broken: the rigged transport never saw an epoch-1 pull")
	}

	// The link is healed (the proxy only ever failed once): the next routine
	// pull drains both epochs in order and converges the stores.
	bEng.PullAll(ctx)
	if wm := mustWatermark(t, bSt, "site-a", 1); wm != 2 {
		t.Fatalf("epoch-1 watermark after heal = %d, want 2", wm)
	}
	if wm := mustWatermark(t, bSt, "site-a", 2); wm != 2 {
		t.Fatalf("epoch-2 watermark after heal = %d, want 2", wm)
	}
	for _, epoch := range []int64{1, 2} {
		if ss, ok := streamState(bEng, "site-a", epoch); !ok || ss.State != StateOK {
			t.Fatalf("stream (site-a,%d) after heal = %+v, want ok", epoch, ss)
		}
	}
	if a, b := mustDump(t, aSt), mustDump(t, bSt); !bytes.Equal(a, b) {
		t.Fatalf("projection dumps diverged after healed catch-up:\nauthor:  %s\nreplica: %s", a, b)
	}
}

// The row-4 property the F8 narrowing must NOT disturb: a peer stalled on an
// old epoch (here a terminal §8.5 schema mismatch) holds back its OWN site's
// later epochs — and nothing of any other peer's. One bad peer never stalls a
// good peer.
func TestSchemaStalledEarlyEpochHoldsBackOwnLaterEpochsButNeverAnotherPeer(t *testing.T) {
	// Rigged peer site-x: epoch 1 carries a foreign schema version (terminal
	// stall), epoch 2 is well-formed and would drain if pulled.
	r := &rig{siteID: "site-x"}
	r.setStreams(StreamHead{NodeEpoch: 1, Head: 4}, StreamHead{NodeEpoch: 2, Head: 3})
	r.setEvents(1, mkStream("site-x", 1, 4, store.SchemaVersion+1, "site-x"))
	r.setEvents(2, mkStream("site-x", 2, 3, store.SchemaVersion, "site-x"))
	rs := httptest.NewServer(r.handler())
	defer rs.Close()

	// A healthy REAL peer alongside it.
	mesh := newMesh(t, 1)
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

	// The mismatched epoch: nothing applied, refusal surfaced loudly.
	if wm := mustWatermark(t, st, "site-x", 1); wm != 0 {
		t.Fatalf("schema-mismatched stream was applied: watermark=%d", wm)
	}
	if ss, ok := streamState(eng, "site-x", 1); !ok || ss.State != StateSchemaMismatch {
		t.Fatalf("stream (site-x,1) = %+v, want %s", ss, StateSchemaMismatch)
	}
	// Epoch-major delivery: site-x's LATER epoch is held back behind the
	// stall — applying it first would be the F8 cross-epoch reorder.
	if wm := mustWatermark(t, st, "site-x", 2); wm != 0 {
		t.Fatalf("later epoch of a stalled site was applied around the stall: watermark(site-x,2)=%d, want 0 (F8)", wm)
	}
	// CROSS-PEER ISOLATION — the property row 4 exists to protect: the
	// healthy peer drained fully despite site-x's stall.
	if wm := mustWatermark(t, st, "site-a", 1); wm != 5 {
		t.Fatalf("healthy peer stalled by another peer's stall: watermark(site-a,1)=%d, want 5", wm)
	}
	if ss, ok := streamState(eng, "site-a", 1); !ok || ss.State != StateOK {
		t.Fatalf("healthy peer's stream = %+v, want ok", ss)
	}
	// A peer with a stalled stream never drains, so it can never earn an
	// anchor; the healthy peer earns one on its SECOND full drain (Anchor
	// exposes the PREV slot of the two-slot §9.3 rotation).
	eng.PullAll(context.Background())
	if _, ok := eng.Anchor("site-x"); ok {
		t.Fatal("anchor recorded for a peer whose site did not drain")
	}
	if _, ok := eng.Anchor("site-a"); !ok {
		t.Fatal("no anchor recorded for the twice-fully-drained healthy peer")
	}
}

// Announcement order is wire input from the peer — epoch-major delivery must
// hold locally even when a rigged server announces its epochs descending.
func TestDescendingEpochAnnouncementIsStillDeliveredEpochMajor(t *testing.T) {
	r := &rig{siteID: "site-x"}
	// Announced DESCENDING, and epoch 1 is unpullable: the rig claims head 4
	// but serves no events, which stalls the stream loudly ("advanced
	// nothing"). Epoch 2 would drain if it were ever pulled.
	r.setStreams(StreamHead{NodeEpoch: 2, Head: 3}, StreamHead{NodeEpoch: 1, Head: 4})
	r.setEvents(2, mkStream("site-x", 2, 3, store.SchemaVersion, "site-x"))
	rs := httptest.NewServer(r.handler())
	defer rs.Close()

	eng, st := engineWithPeers(t, "site-b", config.Peer{SiteID: "site-x", BaseURL: rs.URL, Key: "k"})
	if err := eng.PullPeer(context.Background(), "site-x"); err != nil {
		t.Fatalf("pull: %v", err)
	}

	if wm := mustWatermark(t, st, "site-x", 1); wm != 0 {
		t.Fatalf("unpullable epoch-1 watermark = %d, want 0", wm)
	}
	if wm := mustWatermark(t, st, "site-x", 2); wm != 0 {
		t.Fatalf("epoch 2 was delivered ahead of a stalled epoch 1 under a descending announcement: watermark=%d, want 0 (F8 ordering must be local, not the wire's)", wm)
	}
}
