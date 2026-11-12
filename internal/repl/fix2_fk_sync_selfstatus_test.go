// fix2_fk_sync_selfstatus_test.go — FIX2 cluster K (sync self-status), the
// engine half. D26 (sc-test L12-Y31b; ruling R11, James 2026-10-04): a post
// whose key every peer refuses showed itself "4 peer(s) healthy" and logged
// nothing, because every call it MAKES succeeded. L12-Y05a (= L16-P1b): after
// a restart with peers down the stream table dropped their rows, and with
// every peer down it held no rows at all, not even the post's own. Each test
// here was confirmed to FAIL against 2ba9188 (the fields and rows did not
// exist; the log lines were never written) and PASS after.
package repl

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestD26APostWhosePeersCannotReadItSaysSoAndLogsIt is the measured L12-Y31b
// shape: A's own peer_key changes, so B and C — still holding the old key —
// are refused when they pull A, while every call A makes (its pulls of B and
// C, its pokes) still succeeds.
func TestD26APostWhosePeersCannotReadItSaysSoAndLogsIt(t *testing.T) {
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(orig)
	ctx := context.Background()

	mesh := newMesh(t, 3)
	a, b, c := mesh[0], mesh[1], mesh[2]
	for _, m := range mesh {
		m.eng.Rejoin(ctx)
	}
	authorAt(t, a, "before")
	b.eng.PullAll(ctx)
	c.eng.PullAll(ctx)
	a.eng.PullAll(ctx)
	for _, p := range a.eng.Snapshot().Peers {
		if p.InboundKeyRefused || p.OwnCopyStuck || p.PokeRefused {
			t.Fatalf("fixture: converged mesh already reports a key fact for %s: %+v", p.SiteID, p)
		}
	}

	a.cfg.PeerKey = "demo-key-changed-on-a" // what L12 did to pad's site.toml
	authorAt(t, a, "after-1")
	b.eng.PullAll(ctx)
	c.eng.PullAll(ctx)

	for _, site := range []string{"site-b", "site-c"} {
		if p := peerSnapOf(t, a.eng, site); !p.InboundKeyRefused {
			t.Fatalf("A refused %s's pulls, yet its snapshot does not say %s cannot read it: %+v", site, site, p)
		}
	}
	out := buf.String()
	if !strings.Contains(out, "PEER CANNOT READ THIS POST") || !strings.Contains(out, "site-b") || !strings.Contains(out, "not an outage") {
		t.Fatalf("A logged nothing about refusing its peers' key:\n%s", out)
	}
	if strings.Contains(out, "demo-key-") {
		t.Fatalf("a key value reached the log:\n%s", out)
	}

	// The second, cause-independent signal: A asks B what B holds of A (the
	// per-cycle left-behind exchange, which A's own key still opens) and B
	// is behind. Not stuck yet — that takes 3 sync intervals.
	a.eng.PullAll(ctx)
	p := peerSnapOf(t, a.eng, "site-b")
	if p.OwnCopyBehindSec == nil || p.OwnCopyStuck {
		t.Fatalf("after one cycle B should be seen behind but not yet stuck: %+v", p)
	}
	if p.OwnCopyHead != 1 {
		t.Fatalf("B's copy of A's stream = %d, want 1 (it has the pre-change write only)", p.OwnCopyHead)
	}
	backdateOwnBehind(a.eng, OwnCopyLagMultiple*a.cfg.SyncInterval()+time.Second)
	a.eng.PullAll(ctx)
	if p := peerSnapOf(t, a.eng, "site-b"); !p.OwnCopyStuck {
		t.Fatalf("B has held less of A's stream for more than %d intervals, yet not stuck: %+v", OwnCopyLagMultiple, p)
	}
	if out := buf.String(); !strings.Contains(out, "THIS POST'S RECORDS ARE NOT REACHING PEER site-b") {
		t.Fatalf("no not-reaching line:\n%s", out)
	}

	// Heal: the key is put back; the peers pull; A says so and clears.
	a.cfg.PeerKey = "demo-key-site-a"
	b.eng.PullAll(ctx)
	c.eng.PullAll(ctx)
	a.eng.PullAll(ctx)
	for _, site := range []string{"site-b", "site-c"} {
		p := peerSnapOf(t, a.eng, site)
		if p.InboundKeyRefused || p.OwnCopyStuck || p.OwnCopyBehindSec != nil {
			t.Fatalf("after the heal %s still carries a key fact: %+v", site, p)
		}
	}
	out = buf.String()
	for _, want := range []string{"PEER CAN READ THIS POST AGAIN", "THIS POST'S RECORDS ARE REACHING PEER site-b AGAIN"} {
		if !strings.Contains(out, want) {
			t.Fatalf("no %q line after the heal:\n%s", want, out)
		}
	}
}

// TestD26OwnCopyLagThatKeepsMovingNeverAccruesAge: a peer that keeps up with
// a burst — a little behind at every check, but past what A held when the lag
// was first seen — must never be reported stuck (no wolf-crying on Y-30's
// 34 s write burst).
func TestD26OwnCopyLagThatKeepsMovingNeverAccruesAge(t *testing.T) {
	ctx := context.Background()
	mesh := newMesh(t, 2)
	a, b := mesh[0], mesh[1]
	for _, m := range mesh {
		m.eng.Rejoin(ctx)
	}
	authorAt(t, a, "w1")
	a.eng.PullAll(ctx) // B holds 0 of A's 1: behind, target 1
	if peerSnapOf(t, a.eng, "site-b").OwnCopyBehindSec == nil {
		t.Fatal("fixture: B not seen behind")
	}
	backdateOwnBehind(a.eng, OwnCopyLagMultiple*a.cfg.SyncInterval()+time.Second)
	b.eng.PullAll(ctx) // B reaches 1 …
	authorAt(t, a, "w2")
	a.eng.PullAll(ctx) // … while A is already at 2: still behind, but MOVING
	p := peerSnapOf(t, a.eng, "site-b")
	if p.OwnCopyStuck {
		t.Fatalf("a peer that reached the target is reported stuck: %+v", p)
	}
	if p.OwnCopyBehindSec == nil || *p.OwnCopyBehindSec > 5 {
		t.Fatalf("the lag clock did not restart at the new target: %+v", p)
	}
}

// TestD26AnAcceptedPullDoesNotHideARefusedPoke: a peer that does not list
// this post refuses its pokes (403) while its pulls — any holder of the key
// may read — succeed. The pull's success must not wipe the refusal.
func TestD26AnAcceptedPullDoesNotHideARefusedPoke(t *testing.T) {
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
			http.Error(w, `sync: poke from unknown peer "site-a"`, http.StatusForbidden) // handlePoke's exact answer
			return
		}
		real.ServeHTTP(w, r)
	}))
	a.eng.Rejoin(ctx)
	a.eng.PokePeers()
	waitFor(t, 5*time.Second, "poke refusal recorded", func() bool { return peerSnapOf(t, a.eng, "site-b").PokeRefused })
	a.eng.PullAll(ctx)
	p := peerSnapOf(t, a.eng, "site-b")
	if !p.PokeRefused {
		t.Fatalf("a successful pull wiped the poke refusal: %+v", p)
	}
	if !strings.Contains(buf.String(), "PEER REFUSES THIS POST'S POKES") {
		t.Fatalf("no poke-refusal line:\n%s", buf.String())
	}
	b.lh.set(real)
	a.eng.PokePeers()
	waitFor(t, 5*time.Second, "poke refusal cleared", func() bool { return !peerSnapOf(t, a.eng, "site-b").PokeRefused })
	if !strings.Contains(buf.String(), "PEER ACCEPTS THIS POST'S POKES AGAIN") {
		t.Fatalf("no clearing line:\n%s", buf.String())
	}
}

// TestY05aARestartedPostWithEveryPeerDownStillListsItsOwnAndEveryPeersStream
// is L12-Y05a / L16-P1b: A holds B's records and its own, restarts while B
// is down. Before: "streams": [] — not even its own.
func TestY05aARestartedPostWithEveryPeerDownStillListsItsOwnAndEveryPeersStream(t *testing.T) {
	ctx := context.Background()
	mesh := newMesh(t, 2)
	a, b := mesh[0], mesh[1]
	authorAt(t, a, "a1")
	authorAt(t, a, "a2")
	authorAt(t, b, "b1")
	a.eng.PullAll(ctx)

	b.srv.Close() // B is down
	restarted := New(a.cfg, a.st)
	restarted.Rejoin(ctx)
	snap := restarted.Snapshot()
	var own, peer *StreamStatus
	for i := range snap.Streams {
		switch snap.Streams[i].SiteID {
		case "site-a":
			own = &snap.Streams[i]
		case "site-b":
			peer = &snap.Streams[i]
		}
	}
	if own == nil || !own.Own || own.State != StateOK || own.Watermark != 2 || own.Head != 2 {
		t.Fatalf("own row missing or wrong with every peer down: %+v (all: %+v)", own, snap.Streams)
	}
	if peer == nil || peer.State != StateUnreported || peer.Watermark != 1 {
		t.Fatalf("peer row missing or wrong with that peer down: %+v (all: %+v)", peer, snap.Streams)
	}
	if p := peerSnapOf(t, restarted, "site-b"); p.ErrorKind != ErrorKindUnreachable {
		t.Fatalf("error kind for a dead peer = %q, want %q", p.ErrorKind, ErrorKindUnreachable)
	}
	if snap.UptimeSec <= 0 {
		t.Fatalf("uptime = %v, want > 0", snap.UptimeSec)
	}
}

func backdateOwnBehind(e *Engine, by time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, ps := range e.peers {
		if !ps.ownBehindSince.IsZero() {
			ps.ownBehindSince = ps.ownBehindSince.Add(-by)
		}
	}
}
