// w7_y17_poke_hook_test.go — W7 / Y-17 (sc-test L12, rounds 1 and 2): the
// §8.9 poke fast path was dead in the product. repl.Engine.Author pokes, but
// nothing in internal/web ever called it — every door authors through
// store.Author (alone or inside a Transact commit), which poked nobody — so
// replication ran on the 10 s tick alone: 38 single-write latency samples of
// 3.0-17.1 s, all four peers landing on the same tick. Ruling (FIX-PLAN §3):
// wire the documented poke as best-effort and non-blocking after commit; the
// tick stays as the fallback.
//
// The seam is store.Store.OnAuthored — once per COMMITTED authoring
// transaction, after commit — set to Engine.PokePeers by cmd/sitectl/main.go.
// These tests cannot compile against ecb0d51 (the field did not exist); the
// behavioural fail-before is asserted INSIDE the first test as its own
// precondition: store.Author alone, no hook, no ticker, reaches no peer.
package repl

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/store"
)

// TestAuthoringThroughTheStoreAlonePokesPeersOnceTheHookIsWired is the
// product's path end to end: no Run (no ticker exists in either engine), a
// write through store.Author, the peer must reflect it via the poke alone —
// standalone and as a two-event Transact commit (one poke per commit).
func TestAuthoringThroughTheStoreAlonePokesPeersOnceTheHookIsWired(t *testing.T) {
	sites := newMesh(t, 2) // Run is never called: no ticker exists in either engine
	a, b := sites[0], sites[1]

	// PRECONDITION — the defect itself (Y-17): store.Author, the product's
	// only authoring path, reaches no peer by itself.
	if _, err := a.st.Author("site-a", 1, tick("unhooked"), 0, "default"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(600 * time.Millisecond)
	if wm := mustWatermark(t, b.st, "site-a", 1); wm != 0 {
		t.Fatalf("fixture: B holds %d of site-a with no hook and no ticker — some other mechanism moved the event, so this test cannot pose its case", wm)
	}

	// The wiring cmd/sitectl/main.go does.
	a.st.OnAuthored = a.eng.PokePeers
	interval := a.cfg.SyncInterval() // 10 s
	start := time.Now()
	if _, err := a.st.Author("site-a", 1, tick("hooked"), 0, "default"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, interval, "B to reflect a store-authored event via the hook's poke alone", func() bool {
		return mustWatermark(t, b.st, "site-a", 1) == 2
	})
	if el := time.Since(start); el >= interval/2 {
		t.Fatalf("the hooked write took %s to reach the peer — not the fast path (interval %s)", el, interval)
	} else {
		t.Logf("store.Author → peer via poke alone: %s", el)
	}

	// A Transact commit of two events — a gate door's shape: the hook fires
	// once, after the commit, and both events land at the peer.
	start = time.Now()
	err := a.st.Transact("w7 two-event door", func(v *store.Store) error {
		for _, k := range []string{"t1", "t2"} {
			if _, err := v.Author("site-a", 1, tick(k), 0, "default"); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, interval, "B to reflect both events of a Transact commit via the poke", func() bool {
		return mustWatermark(t, b.st, "site-a", 1) == 4
	})
	if el := time.Since(start); el >= interval/2 {
		t.Fatalf("the Transact commit took %s to reach the peer — not the fast path", el)
	}
	if !bytes.Contains(mustDump(t, b.st), []byte(`"t2"`)) {
		t.Fatal("B's projections do not reflect the second event of the commit (the fold did not run)")
	}
	a.eng.Shutdown()
	b.eng.Shutdown()
}

// slowPokeServer answers /sync/poke only once `release` is closed (or after
// `hold`, when release is nil), or at once when the client goes away — a
// dead-slow peer whose slowness the test controls, so nothing below depends
// on how fast this machine commits. Everything else is 404.
func slowPokeServer(t *testing.T, hold time.Duration, release <-chan struct{}, count *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sync/poke" {
			http.NotFound(w, r)
			return
		}
		if count != nil {
			count.Add(1)
		}
		var timer <-chan time.Time
		if release == nil {
			timer = time.After(hold)
		}
		select {
		case <-release:
		case <-timer:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"site_id":"site-s","t1":0,"t2":0,"t3":0}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestAuthorReturnsWithoutWaitingForASlowPeersPokeAnswer: the officer's
// write must not wait on the poke — "non-blocking after commit" by ruling.
func TestAuthorReturnsWithoutWaitingForASlowPeersPokeAnswer(t *testing.T) {
	srv := slowPokeServer(t, 3*time.Second, nil, nil)
	eng, st := engineWithPeers(t, "site-a", config.Peer{SiteID: "site-s", BaseURL: srv.URL, Key: "k"})
	st.OnAuthored = eng.PokePeers
	start := time.Now()
	if _, err := st.Author("site-a", 1, tick("x"), 0, "default"); err != nil {
		t.Fatal(err)
	}
	if el := time.Since(start); el > 500*time.Millisecond {
		t.Fatalf("Author took %s: the poke blocked the write", el)
	}
	done := make(chan struct{})
	go func() { eng.Shutdown(); close(done) }() // cancels the in-flight poke
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown did not return while a poke was in flight")
	}
}

// TestABurstOfOwnWritesCoalescesPokesToAtMostOneInFlightPlusOnePending: the
// sender-side coalescer. Twenty writes while the first poke is still being
// answered must produce exactly two pokes — the one in flight and ONE
// trailing poke that covers the last write — never one goroutine per write
// (the pre-fix PokePeers shape, which against a dead link would have parked
// 1 680 blocked requests for the L12 Y-30 burst). The peer holds the first
// poke until the test releases it AFTER the burst, so the case is posed
// however slowly this machine commits (a timed hold failed its own
// precondition under parallel-package load: 20 writes took 2.7 s).
func TestABurstOfOwnWritesCoalescesPokesToAtMostOneInFlightPlusOnePending(t *testing.T) {
	var pokes atomic.Int32
	release := make(chan struct{})
	srv := slowPokeServer(t, 0, release, &pokes)
	eng, st := engineWithPeers(t, "site-a", config.Peer{SiteID: "site-s", BaseURL: srv.URL, Key: "k"})
	st.OnAuthored = eng.PokePeers

	for i := 0; i < 20; i++ {
		if _, err := st.Author("site-a", 1, tick("burst"), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	// Precondition: exactly one poke is in flight (held by the peer) while
	// the other nineteen writes folded into the pending latch.
	waitFor(t, 5*time.Second, "the first poke to reach the peer", func() bool { return pokes.Load() >= 1 })
	if n := pokes.Load(); n != 1 {
		t.Fatalf("fixture: %d pokes reached the peer before the first was answered, want exactly 1 in flight", n)
	}
	close(release) // the peer answers; the trailing poke may now go
	waitFor(t, 5*time.Second, "the trailing poke after the in-flight one", func() bool { return pokes.Load() >= 2 })
	time.Sleep(500 * time.Millisecond) // long enough for any third poke to have been sent
	if n := pokes.Load(); n != 2 {
		t.Fatalf("20 writes during one held poke produced %d pokes, want exactly 2 (one in flight + one trailing)", n)
	}
	eng.Shutdown()
}
