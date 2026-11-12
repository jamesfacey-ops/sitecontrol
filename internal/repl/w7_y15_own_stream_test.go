// w7_y15_own_stream_test.go — W7 / Y-15 (sc-test L12, rounds 1 and 2, FAIL
// both times): every post logged a false `SYNC ERROR … peer_log_regressed …
// never legitimate` on its OWN stream after its own writes (29 of 29 such
// lines named the writer itself), and during a 34 s write burst the own row
// on /debug/sync-status sat in peer_log_regressed with a frozen watermark.
//
// Mechanism: checkLeftBehind (post-latch, every cycle) and the §8.6
// self-history drain (Rejoin at boot) both write the own-stream row through
// setStream with a PEER's COPY-head of our stream as `head`; setStream's
// §8.9 regression detector then compared that copy-head with the highest
// copy-head any OTHER peer had reported — so a peer that simply had not
// pulled yet (normal for up to one sync interval after every write) read as
// an author whose log went backwards. Fixed in setStream: the detector is
// for a PEER's log only; we are the author of our own stream, and nothing a
// peer reports about it is an author announcement.
//
// The third test is the same function's adjacent defect, found while fixing
// this one: the own row is ONE row written from N peers' points of view in
// one PullAll, and a healthy report from the LAST peer in config order
// erased a left_behind alarm (or a live identity_rejected) raised seconds
// earlier in the same cycle — SYNC ERROR / SYNC RECOVERED every ten seconds,
// and every cycle ended green.
//
// All three confirmed to FAIL against ecb0d51 (the pre-fix engine) and PASS
// after; see the W7 report.
package repl

import (
	"bytes"
	"context"
	"log"
	"path/filepath"
	"strings"
	"testing"
)

// authorAt appends through store.Author — the product's only authoring path,
// with no engine involvement and (OnAuthored unset) no poke — so the test,
// not an asynchronous poke-pull, decides exactly which peer has caught up.
func authorAt(t *testing.T, m *meshSite, k string) {
	t.Helper()
	if _, err := m.st.Author(m.cfg.SiteID, m.cfg.NodeEpoch, tick(k), 0, "default"); err != nil {
		t.Fatal(err)
	}
}

// TestOwnStreamRowIsNeverFlaggedRegressedByALaggingPeersCopyHead is the
// routine-cycle witness: three posts, all latched (Rejoin ran), A writes, B
// catches up, C has not pulled yet — the ordinary state of every post for up
// to one interval after every write. A's own routine cycle then asks B
// (copy-head 3) and C (copy-head 1) what they hold of A's stream. Pre-fix:
// C's 1 < B's 3 → "peer_log_regressed … never legitimate" on A's OWN row.
func TestOwnStreamRowIsNeverFlaggedRegressedByALaggingPeersCopyHead(t *testing.T) {
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(orig)

	ctx := context.Background()
	mesh := newMesh(t, 3)
	a, b, c := mesh[0], mesh[1], mesh[2]
	for _, m := range mesh {
		m.eng.Rejoin(ctx) // latch every self-history drain: checkLeftBehind runs post-latch
	}
	authorAt(t, a, "seed")
	b.eng.PullAll(ctx)
	c.eng.PullAll(ctx)
	authorAt(t, a, "w2")
	authorAt(t, a, "w3")
	b.eng.PullAll(ctx) // only B catches up; C lags, as every peer does after every write

	// Precondition (rule 6): the lag is real — B holds 3, C holds 1.
	if bw, cw := mustWatermark(t, b.st, "site-a", 1), mustWatermark(t, c.st, "site-a", 1); bw != 3 || cw != 1 {
		t.Fatalf("fixture: B holds %d and C holds %d of site-a; the scenario needs B=3 (caught up) and C=1 (lagging)", bw, cw)
	}

	buf.Reset()
	for cycle := 1; cycle <= 2; cycle++ {
		// A's routine cycle: checkLeftBehind(B) sees copy-head 3, then
		// checkLeftBehind(C) sees copy-head 1 — config order.
		a.eng.PullAll(ctx)
		row := witnessRow(t, a.eng.Snapshot(), "site-a", 1)
		if row.State != StateOK {
			t.Fatalf("Y-15, cycle %d: own row after a routine cycle with a lagging peer = %+v, want state=ok — a peer's copy-head of OUR stream is lag, not a regression of our log", cycle, row)
		}
		if row.Watermark != 3 || row.Head != 3 {
			t.Fatalf("cycle %d: own row %+v, want watermark=head=3 (the local truth, live — Y-02)", cycle, row)
		}
	}
	if out := buf.String(); strings.Contains(out, StateRegressed) {
		t.Fatalf("a false SYNC ERROR was logged on the post's own stream:\n%s", out)
	}
	// Fixture honesty: C still lags (nothing in the cycle above pulled for C).
	if cw := mustWatermark(t, c.st, "site-a", 1); cw != 1 {
		t.Fatalf("fixture: C moved to %d during A's cycles; the lag the test relies on was not sustained", cw)
	}
}

// TestRejoinFromPeersHoldingDifferentAmountsOfOurHistoryNeverFlagsOurOwnStreamRegressed
// is the boot-time witness: the same false alarm fired at every re-image
// whenever two peers held different amounts of the re-imaged post's pre-wipe
// history (the L12 reseeds show the class on four posts' streams). A drains
// its epoch-1 history from B (head 3) then from C (head 2); pre-fix the
// second drain's copy-head 2 < 3 read as a regression of A's own log.
func TestRejoinFromPeersHoldingDifferentAmountsOfOurHistoryNeverFlagsOurOwnStreamRegressed(t *testing.T) {
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
	authorAt(t, a, "e1")
	authorAt(t, a, "e2")
	b.eng.PullAll(ctx)
	c.eng.PullAll(ctx)
	authorAt(t, a, "e3")
	b.eng.PullAll(ctx) // B=3, C=2
	if bw, cw := mustWatermark(t, b.st, "site-a", 1), mustWatermark(t, c.st, "site-a", 1); bw != 3 || cw != 2 {
		t.Fatalf("fixture: B holds %d and C holds %d of site-a; the scenario needs B=3 and C=2", bw, cw)
	}

	// A is re-imaged WITH the §8.6 epoch bump: blank store, node_epoch 2.
	cfg2 := *a.cfg
	cfg2.NodeEpoch = 2
	cfg2.DBPath = filepath.Join(t.TempDir(), "site-a-reimaged.db")
	st2 := openStore(t, cfg2.DBPath)
	a2 := New(&cfg2, st2)
	a.lh.set(a2.SyncHandler())

	buf.Reset()
	a2.Rejoin(ctx) // self-history from B (announces 3), then C (announces 2) — config order
	row := witnessRow(t, a2.Snapshot(), "site-a", 1)
	if row.State != StateOK {
		t.Fatalf("Y-15 at boot: own pre-wipe row after Rejoin = %+v, want ok — the second peer announcing LESS of our own history than the first is lag, not a regression of our log", row)
	}
	if row.Watermark != 3 {
		t.Fatalf("own pre-wipe row %+v, want watermark=3 (everything the best peer held was recovered)", row)
	}
	if got := mustWatermark(t, st2, "site-a", 1); got != 3 {
		t.Fatalf("recovered epoch-1 watermark %d, want 3", got)
	}
	if out := buf.String(); strings.Contains(out, StateRegressed) {
		t.Fatalf("a false SYNC ERROR was logged on the re-imaged post's own stream at boot:\n%s", out)
	}
}

// TestALeftBehindAlarmFromOnePeerIsNotErasedByAHealthyReportFromAnother:
// three posts, A latched to both; A is restored from an older backup (same
// epoch) while B already holds more and C holds exactly what A now holds.
// In one PullAll, B (config order first) raises left_behind and C then
// reports healthy. Pre-fix C's write erased B's alarm every cycle — the row
// ended each cycle "ok" and the log flapped ERROR/RECOVERED. The alarm must
// stand until no peer is ahead, then clear exactly once.
func TestALeftBehindAlarmFromOnePeerIsNotErasedByAHealthyReportFromAnother(t *testing.T) {
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
	authorAt(t, a, "seed")
	b.eng.PullAll(ctx)
	c.eng.PullAll(ctx)

	// A restarts ordinarily (same store, same epoch) — the realistic way an
	// own row reaches a genuine, latched "ok" (left_behind_witness_test.go).
	a2 := New(a.cfg, a.st)
	a.lh.set(a2.SyncHandler())
	a2.Rejoin(ctx)
	if r := witnessRow(t, a2.Snapshot(), "site-a", 1); r.State != StateOK || r.Watermark != 1 {
		t.Fatalf("fixture: own row after ordinary restart %+v, want 1/1 ok", r)
	}

	authorAt(t, a, "w2")
	authorAt(t, a, "w3")
	b.eng.PullAll(ctx) // B=3; C stays at 1
	if bw, cw := mustWatermark(t, b.st, "site-a", 1), mustWatermark(t, c.st, "site-a", 1); bw != 3 || cw != 1 {
		t.Fatalf("fixture: B holds %d and C holds %d of site-a; the scenario needs B=3 (ahead) and C=1 (equal to the restored A)", bw, cw)
	}

	// A is restored from an older backup at the SAME epoch (a2 keeps running,
	// latched — see rollBackWatermark's doc for why this is the faithful
	// shape): local watermark back to 1; B is now ahead, C is level.
	rollBackWatermark(t, a.cfg.DBPath, "site-a", 1, 1)
	if wm := mustWatermark(t, a.st, "site-a", 1); wm != 1 {
		t.Fatalf("fixture: rollback did not take, watermark=%d", wm)
	}

	buf.Reset()
	for cycle := 1; cycle <= 2; cycle++ {
		a2.PullAll(ctx) // checkLeftBehind(B): head 3 > 1 → alarm; checkLeftBehind(C): head 1 <= 1 → healthy
		row := witnessRow(t, a2.Snapshot(), "site-a", 1)
		if row.State != StateLeftBehind {
			t.Fatalf("cycle %d: own row = %+v, want %s — C's healthy report (it holds no more than we do) says nothing about B, which does", cycle, row, StateLeftBehind)
		}
		if row.Watermark != 1 || row.Head != 3 {
			t.Fatalf("cycle %d: own row %+v, want watermark=1 (local truth) head=3 (the peer that is ahead)", cycle, row)
		}
	}
	out := buf.String()
	if n := countOccurrences(out, "entered state="+StateLeftBehind); n != 1 {
		t.Fatalf("left_behind logged %d times over two cycles, want exactly 1 (a transition, then gated):\n%s", n, out)
	}
	if n := countOccurrences(out, "SYNC RECOVERED: stream site=site-a"); n != 0 {
		t.Fatalf("the own row flapped: %d SYNC RECOVERED line(s) while B was still ahead:\n%s", n, out)
	}

	// Tier 2 lands the tail (here: the watermark is put back to 3 — the rows
	// never left the append-only log): B is no longer ahead, so B's own next
	// healthy report clears the alarm, exactly once.
	rollBackWatermark(t, a.cfg.DBPath, "site-a", 1, 3)
	a2.PullAll(ctx)
	row := witnessRow(t, a2.Snapshot(), "site-a", 1)
	if row.State != StateOK || row.Watermark != 3 {
		t.Fatalf("own row after the tail landed = %+v, want 3/3 ok", row)
	}
	out = buf.String()
	if n := countOccurrences(out, "SYNC RECOVERED: stream site=site-a"); n != 1 {
		t.Fatalf("recovery logged %d times, want exactly 1:\n%s", n, out)
	}
	_ = c
}
