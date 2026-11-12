// w7_y25_lost_history_test.go — W7 / Y-25 (sc-test L12, rounds 1 and 2):
// a post re-imaged WITHOUT the §8.6 epoch bump recovers nobody and says so on
// its OWN surface (identity_rejected, "4 of 4 peer(s) degraded") — but from
// every OTHER post it read "ok … peer reports no pre-wipe history for this
// post": the author announced head 0 for a stream the peers held 467 of, and
// the §8.9 maxHead detector skips head 0 (the error paths pass 0 for
// "unknown") and forgets everything on a restart besides.
//
// Fixed in pullStream: for a PEER's own log at its CURRENT (highest
// announced) epoch, an announced head BELOW our watermark is lost history —
// our copy only ever came from that author, and an author's log only grows
// within an epoch — surfaced as StateRegressed with a detail that says what
// happened and what the operator does about it. Stateless: it reads the
// store's watermark, so it holds across our own restarts. A pre-wipe epoch an
// author is still recovering after a GENUINE bump (a higher epoch announced
// beside it) is deliberately not flagged.
//
// Both confirmed to FAIL against ecb0d51 and PASS after; see the W7 report.
package repl

import (
	"bytes"
	"context"
	"log"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/store"
)

func peerSnapOf(t *testing.T, e *Engine, site string) PeerSnapshot {
	t.Helper()
	for _, p := range e.Snapshot().Peers {
		if p.SiteID == site {
			return p
		}
	}
	t.Fatalf("no peer %q in snapshot", site)
	return PeerSnapshot{}
}

// TestAPostReimagedWithoutTheEpochBumpIsFlaggedRegressedFromThePeersSide is
// the Y-25 witness: A authors 5, B holds them; A is wiped and restarted at
// the SAME node_epoch; B's next routine cycle must not read "ok".
func TestAPostReimagedWithoutTheEpochBumpIsFlaggedRegressedFromThePeersSide(t *testing.T) {
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(orig)

	ctx := context.Background()
	mesh := newMesh(t, 2)
	a, b := mesh[0], mesh[1]
	a.eng.Rejoin(ctx)
	b.eng.Rejoin(ctx)
	for _, k := range []string{"e1", "e2", "e3", "e4", "e5"} {
		authorAt(t, a, k)
	}
	b.eng.PullAll(ctx)
	if wm := mustWatermark(t, b.st, "site-a", 1); wm != 5 {
		t.Fatalf("fixture: B holds %d of site-a, want 5", wm)
	}
	if r, ok := streamState(b.eng, "site-a", 1); !ok || r.State != StateOK {
		t.Fatalf("fixture: B's row for site-a before the re-image = %+v, want ok", r)
	}
	drainsBefore := peerSnapOf(t, b.eng, "site-a").Drains

	// A is wiped and restarted at the SAME node_epoch — the Y-25 shape.
	cfgX := *a.cfg
	cfgX.DBPath = filepath.Join(t.TempDir(), "site-a-amnesiac.db")
	stX := openStore(t, cfgX.DBPath)
	aX := New(&cfgX, stX)
	a.lh.set(aX.SyncHandler())
	aX.Rejoin(ctx)
	// Precondition: the amnesiac post could NOT recover — same-epoch
	// redelivery is refused at the R-23/AF3 identity bound — so its own copy
	// is empty and its own surface says so (this is exactly what the live
	// post showed under Y-25). Its peers' view is what this test is about.
	if wm := mustWatermark(t, stX, "site-a", 1); wm != 0 {
		t.Fatalf("fixture: the re-imaged post recovered %d of its own epoch-1 history without a bump; the scenario needs 0", wm)
	}
	if own := witnessRow(t, aX.Snapshot(), "site-a", 1); own.State != StateIdentityRejected {
		t.Fatalf("fixture: the re-imaged post's own row = %+v, want %s (its own surface is honest already)", own, StateIdentityRejected)
	}

	buf.Reset()
	b.eng.PullAll(ctx) // B's next routine cycle: A announces (epoch 1, head 0); B holds 5
	row, ok := streamState(b.eng, "site-a", 1)
	if !ok || row.State != StateRegressed {
		t.Fatalf("Y-25: B's row for the amnesiac author after one cycle = %+v, want %s — an author announcing LESS of its current stream than we hold has lost its history", row, StateRegressed)
	}
	if row.Watermark != 5 || row.Head != 0 {
		t.Fatalf("row %+v, want watermark=5 (ours, untouched) head=0 (what the author announced)", row)
	}
	for _, want := range []string{"LOST", "node_epoch 1", "holds 5", "bumping node_epoch at site-a"} {
		if !strings.Contains(row.Detail, want) {
			t.Fatalf("detail does not say %q:\n%s", want, row.Detail)
		}
	}
	// Nothing applied, nothing rolled back, and the anchor never rotated on
	// an author we cannot trust (drains unchanged).
	if wm := mustWatermark(t, b.st, "site-a", 1); wm != 5 {
		t.Fatalf("B's watermark for site-a moved to %d, want 5 held", wm)
	}
	if d := peerSnapOf(t, b.eng, "site-a").Drains; d != drainsBefore {
		t.Fatalf("drains for site-a went %d → %d across the flagged cycle, want unchanged (no full drain of a regressed author)", drainsBefore, d)
	}
	// Logged once as a transition, then gated — not once per cycle.
	b.eng.PullAll(ctx)
	if n := countOccurrences(buf.String(), "entered state="+StateRegressed); n != 1 {
		t.Fatalf("regression logged %d times over two cycles, want exactly 1:\n%s", n, buf.String())
	}
	if r, _ := streamState(b.eng, "site-a", 1); r.State != StateRegressed {
		t.Fatalf("second cycle: row %+v, want still %s", r, StateRegressed)
	}
}

// TestAnAuthorRecoveringAPreWipeEpochAfterABumpIsNotFlaggedButTheSameShortfallAtItsCurrentEpochIs
// pins the boundary with a scriptable author. B holds (site-a, 1) at 5.
//  1. The author announces (1,3),(2,0): bumped, still recovering epoch 1 —
//     legitimate, must read ok on both rows (negative control).
//  2. The author announces (1,3) alone: epoch 1 IS its current epoch, and
//     3 < 5 is lost history — regressed.
//  3. The author announces (1,5): back at our head — clears to ok.
//
// The maxHead detector cannot distinguish 1 from 2 (both see head 3 after
// head 3), so this is the stateless check alone being exercised.
func TestAnAuthorRecoveringAPreWipeEpochAfterABumpIsNotFlaggedButTheSameShortfallAtItsCurrentEpochIs(t *testing.T) {
	ctx := context.Background()
	r := &rig{siteID: "site-a"}
	rs := httptest.NewServer(r.handler())
	t.Cleanup(rs.Close)
	eng, st := engineWithPeers(t, "site-b", config.Peer{SiteID: "site-a", BaseURL: rs.URL, Key: "k"})
	full := mkStream("site-a", 1, 5, store.SchemaVersion, "site-a")
	if _, err := st.ApplyBatch(full); err != nil {
		t.Fatal(err)
	}
	if wm := mustWatermark(t, st, "site-a", 1); wm != 5 {
		t.Fatalf("fixture: B holds %d of site-a, want 5", wm)
	}

	// 1. bumped and recovering: (1,3) beside (2,0).
	r.setStreams(StreamHead{NodeEpoch: 1, Head: 3}, StreamHead{NodeEpoch: 2, Head: 0})
	r.setEvents(1, full[:3])
	eng.PullAll(ctx)
	if s, ok := streamState(eng, "site-a", 1); !ok || s.State != StateOK {
		t.Fatalf("negative control: a pre-wipe epoch the author is still recovering (a higher epoch announced beside it) = %+v, want ok", s)
	}
	if s, ok := streamState(eng, "site-a", 2); !ok || s.State != StateOK {
		t.Fatalf("negative control: the author's fresh current epoch = %+v, want ok", s)
	}

	// 2. the same shortfall, but epoch 1 is now the author's CURRENT epoch.
	r.setStreams(StreamHead{NodeEpoch: 1, Head: 3})
	eng.PullAll(ctx)
	s, ok := streamState(eng, "site-a", 1)
	if !ok || s.State != StateRegressed {
		t.Fatalf("the author's current epoch announces head 3 while we hold 5: row = %+v, want %s", s, StateRegressed)
	}
	if !strings.Contains(s.Detail, "CURRENT") {
		t.Fatalf("detail does not name the current-epoch rule:\n%s", s.Detail)
	}
	if wm := mustWatermark(t, st, "site-a", 1); wm != 5 {
		t.Fatalf("watermark moved to %d, want 5 held", wm)
	}

	// 3. the author is back at (or past) our head: clears.
	r.setStreams(StreamHead{NodeEpoch: 1, Head: 5})
	r.setEvents(1, full)
	eng.PullAll(ctx)
	if s, ok := streamState(eng, "site-a", 1); !ok || s.State != StateOK {
		t.Fatalf("after the author recovered to our head: row = %+v, want ok", s)
	}
}
