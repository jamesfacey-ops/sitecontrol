// fix3_b_sync_test.go — FIX3 stream B, the engine half (FINDINGS-R5 F2, F7,
// P1a/P2b and the "(was )" log line). Each test was confirmed to FAIL against
// e3d507d and PASS after. The PeerSnapshot facts added here (poke_error,
// poke_refused_kind) are read through the snapshot's JSON, so the file
// compiles against e3d507d and fails there on the assertion, not the build.
package repl

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"testing"
	"time"
)

// peerJSON is one peer's snapshot as /debug/sync-status emits it.
func peerJSON(t *testing.T, e *Engine, site string) map[string]any {
	t.Helper()
	raw, err := json.Marshal(peerSnapOf(t, e, site))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// TestF2AFixedKeyClearsThePokeRefusalOnTheNextTickWithoutAWrite is re-run A3
// (FINDINGS-R5 F2, MEDIUM): B's peer_key changes, so A's poke and A's pull
// of B are both refused 401. The key is put back. A's next routine cycle —
// with NOTHING written at A — must leave no "peer rejects this post's key"
// fact behind. Before: poke_refused stayed true until A next wrote (100 s
// idle, measured), giving a false STALE on a current peer.
func TestF2AFixedKeyClearsThePokeRefusalOnTheNextTickWithoutAWrite(t *testing.T) {
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(orig)
	ctx := context.Background()

	mesh := newMesh(t, 2)
	a, b := mesh[0], mesh[1]
	for _, m := range mesh {
		m.eng.Rejoin(ctx)
	}
	b.cfg.PeerKey = "demo-key-changed-on-b"
	a.eng.PokePeers() // A's write, refused
	waitFor(t, 5*time.Second, "poke refusal recorded", func() bool { return peerSnapOf(t, a.eng, "site-b").PokeRefused })
	a.eng.PullAll(ctx)
	if p := peerJSON(t, a.eng, "site-b"); p["poke_refused_kind"] != ErrorKindKeyRejected {
		t.Errorf("a 401 poke refusal must be recorded as a KEY refusal (poke_refused_kind), so the page never calls it a 403: %v", p)
	}

	b.cfg.PeerKey = "demo-key-site-b" // the fix: B restarted with its right key
	a.eng.PullAll(ctx)                // one routine tick; no write at A
	waitFor(t, 2*time.Second, "the refusal to clear on the next tick", func() bool {
		p := peerSnapOf(t, a.eng, "site-b")
		return !p.PokeRefused && p.LastError == ""
	})
	if p := peerJSON(t, a.eng, "site-b"); p["poke_refused_kind"] != nil || p["poke_error"] != nil {
		t.Fatalf("a cleared refusal left a poke fact behind: %v", p)
	}
	if !strings.Contains(buf.String(), "PEER ACCEPTS THIS POST") {
		t.Fatalf("no clearing line in the log:\n%s", buf.String())
	}
}

// TestF2AnIdentityRefusalIsReprobedOnTheNextTick: a 403 poke refusal (the
// peer did not list this post) is NOT cleared by a successful pull — any
// holder of the key may read — but a fixed peer list must still clear it
// without a write: while the refusal stands, every routine cycle pokes once.
func TestF2AnIdentityRefusalIsReprobedOnTheNextTick(t *testing.T) {
	ctx := context.Background()
	mesh := newMesh(t, 2)
	a, b := mesh[0], mesh[1]
	real := b.eng.SyncHandler()
	b.lh.set(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			http.Error(w, `sync: poke from unknown peer "site-a"`, http.StatusForbidden)
			return
		}
		real.ServeHTTP(w, r)
	}))
	a.eng.Rejoin(ctx)
	a.eng.PokePeers()
	waitFor(t, 5*time.Second, "poke refusal recorded", func() bool { return peerSnapOf(t, a.eng, "site-b").PokeRefused })
	if p := peerJSON(t, a.eng, "site-b"); p["poke_refused_kind"] != ErrorKindIdentityRejected {
		t.Errorf("a 403 poke refusal must be recorded as an IDENTITY refusal: %v", p)
	}
	b.lh.set(real) // B now lists A
	a.eng.PullAll(ctx)
	waitFor(t, 2*time.Second, "the re-probe to clear the refusal", func() bool { return !peerSnapOf(t, a.eng, "site-b").PokeRefused })
}

// TestF7APokeThatFailsWithoutARefusalIsLoggedOnceAndKeptUntilAPokeSucceeds
// is r4 L16-04a / R5 L01-2a: a poke answered 503 (or lost in transport, or a
// proxy stripping POST) left a last_error the next pull erased, and 0 log
// lines. Now: one rate-limited line, and a poke_error that a successful PULL
// does not erase — only an accepted poke (re-probed each tick) does. The
// failure does not mark the peer degraded: pulls still carry the records.
func TestF7APokeThatFailsWithoutARefusalIsLoggedOnceAndKeptUntilAPokeSucceeds(t *testing.T) {
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(orig)
	ctx := context.Background()

	mesh := newMesh(t, 2)
	a, b := mesh[0], mesh[1]
	real := b.eng.SyncHandler()
	b.lh.set(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			http.Error(w, "proxy: service unavailable", http.StatusServiceUnavailable)
			return
		}
		real.ServeHTTP(w, r)
	}))
	a.eng.Rejoin(ctx)
	a.eng.PokePeers()
	waitFor(t, 5*time.Second, "the poke failure to be recorded", func() bool { return peerJSON(t, a.eng, "site-b")["poke_error"] != nil })
	a.eng.PullAll(ctx) // a successful pull, and one re-probe that fails again
	time.Sleep(300 * time.Millisecond)
	a.eng.PokePeers()
	time.Sleep(300 * time.Millisecond)

	p := peerJSON(t, a.eng, "site-b")
	if p["poke_error"] == nil || !strings.Contains(p["poke_error"].(string), "503") {
		t.Fatalf("a successful pull erased the poke failure, or it lost its status: %v", p)
	}
	if s := peerSnapOf(t, a.eng, "site-b"); s.LastError != "" || s.PokeRefused {
		t.Fatalf("a 503 poke is neither a pull failure nor a key refusal: %+v", s)
	}
	if n := strings.Count(buf.String(), "POKES ARE FAILING"); n != 1 {
		t.Fatalf("want exactly 1 poke-failure line for three failed pokes (rate-limited), got %d:\n%s", n, buf.String())
	}
	if !strings.Contains(buf.String(), "503") || !strings.Contains(buf.String(), "site-b") {
		t.Fatalf("the line must name the peer and the answer:\n%s", buf.String())
	}

	b.lh.set(real)
	a.eng.PullAll(ctx) // the tick re-probes; no write
	waitFor(t, 2*time.Second, "an accepted poke to clear the failure", func() bool { return peerJSON(t, a.eng, "site-b")["poke_error"] == nil })
	if !strings.Contains(buf.String(), "POKES REACH PEER site-b AGAIN") {
		t.Fatalf("no clearing line:\n%s", buf.String())
	}
}

// TestP1aAPeerReachedAgainIsPokedSoItNeedNotWaitForItsOwnTick is r4
// L16-P1a/P2b (R5 L01-3/4): after a heal, the link whose far end had nothing
// new to write waited for that end's next tick (~10.5 s) to pull. Now the
// first successful exchange after a failure pokes the peer at once. B runs no
// routine timer here, so only a poke can make it pull.
func TestP1aAPeerReachedAgainIsPokedSoItNeedNotWaitForItsOwnTick(t *testing.T) {
	ctx := context.Background()
	mesh := newMesh(t, 2)
	a, b := mesh[0], mesh[1]
	for _, m := range mesh {
		m.eng.Rejoin(ctx)
	}
	real := b.eng.SyncHandler()
	b.lh.set(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "cut", http.StatusBadGateway) // the link is cut
	}))
	a.eng.PullAll(ctx)
	if peerSnapOf(t, a.eng, "site-b").LastError == "" {
		t.Fatal("fixture: the cut was not seen")
	}
	authorAt(t, a, "during-the-cut") // no poke: the store hook is not wired here

	b.lh.set(real) // heal
	a.eng.PullAll(ctx)
	waitFor(t, 3*time.Second, "B to pull A's write without its own tick", func() bool {
		wm, err := b.st.Watermark("site-a", 1)
		return err == nil && wm >= 1
	})
}

// TestSyncRecoveredNeverPrintsAnEmptyPriorState: the first report after a
// start logged "back to ok (was )" (R5 L01, L04). It now names what it was.
func TestSyncRecoveredNeverPrintsAnEmptyPriorState(t *testing.T) {
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(orig)
	mesh := newMesh(t, 2)
	mesh[0].eng.setStream(StreamKey{SiteID: "site-b", NodeEpoch: 1}, 3, 3, StateOK, "")
	out := buf.String()
	if strings.Contains(out, "(was )") || !strings.Contains(out, "SYNC RECOVERED") {
		t.Fatalf("want one RECOVERED line naming the prior state, got:\n%s", out)
	}
	if !strings.Contains(out, "first report since this post started") {
		t.Fatalf("the first report must say it is the first:\n%s", out)
	}
}
