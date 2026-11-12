// w9c_target_identity_test.go — W9c-11: target-key identity is the FULL
// three columns.
//
// design/WINDOW-BUILD-PLAN-v1.md "#### W9c", criterion W9c-11 ⟦NEW at the
// rev-4 fold — W9R2-TE-5⟧, against design/alert-unack-v1.md revision 4 §7
// row T9.
//
// KILLER MUTANT: m9 — the epoch-blind target matcher. A `seq`-only or
// `(site_id, seq)` matcher retracts ack_e2 as well and X ALERTS (half 1);
// a `(node_epoch, seq)` site-blind matcher retracts ack_b as well and Y
// ALERTS (half 2).
//
// ⚠ BOTH PINS ARE SUPPRESSION PINS, DELIBERATELY. A blind matcher only ever
// WIDENS the retracted set, and the predicate is ∃ ack · ∄ retraction, so
// every consequence lands on the ALERTING side: an alerting assertion
// cannot redden this. Empty-banner is the only assertion shape that can,
// and a builder reaching for the alerting assertion by reflex books a false
// green (plan, W9c-11's own warning).
//
// ⚠ NO PRE-CHUNK RED. On the delivered tree the retractions fold into
// nothing, every ack stands, and both banners are empty — the asserted
// answer, reached for the wrong reason. Its evidence is the PLANTED m9
// control, never this clean pass. (The Phase-C sweep excluded W9c-11 from
// W9's already-true table on the ground that its fixture "cannot be
// constructed" pre-chunk because no retraction can be authored; a
// hand-built envelope over the wire seam — which is the seam W9c-11's own
// text names — constructs it. Recorded as a finding in
// reports/window-build/W9c-tests.md, not reconciled here.)
package deadline

import (
	"testing"
	"time"
)

// TestW9c11RetractionMatchesOnAllThreeKeyColumns — W9c-11 (T9).
//
// One fixture, two halves, both at the fold + Evaluate over the Envelope
// wire seam so that EVERY (site_id, node_epoch, seq) is stated literally —
// which no fixture reachable through Acknowledge can do, since that path
// takes whatever seq its stream is at.
func TestW9c11RetractionMatchesOnAllThreeKeyColumns(t *testing.T) {
	st := newSite(t)
	d := newScanner(t, st)

	// Trip X — half 1's epoch-restart twin. Trip Y — half 2's cross-site
	// (epoch, seq) collision twin. Both overdue_live.
	tripX, envX := dispatchInbound(t, st, 1, "driver-x")
	tripY, envY := dispatchInbound(t, st, 1, "driver-y")

	// --- the stated keys, verbatim from W9c-11 -------------------------
	ackE1 := w9cKey{origin, 1, 7} // X: (site-a, epoch 1, seq 7)
	ackE2 := w9cKey{origin, 2, 7} // X: (site-a, epoch 2, seq 7) — EQUAL seq, DIFFERENT epoch
	ackA := w9cKey{origin, 1, 9}  // Y: (site-a, epoch 1, seq 9)
	ackB := w9cKey{here, 1, 9}    // Y: (site-b, epoch 1, seq 9) — the SAME (epoch, seq) at the other post

	if ackE1.Seq != ackE2.Seq || ackE1.Epoch == ackE2.Epoch || ackE1.Site != ackE2.Site {
		t.Fatalf("W9c-11 fixture: half 1 needs ONE site, EQUAL seq, DIFFERENT epoch — got %v and %v", ackE1, ackE2)
	}
	if ackA.Epoch != ackB.Epoch || ackA.Seq != ackB.Seq || ackA.Site == ackB.Site {
		t.Fatalf("W9c-11 fixture: half 2 needs the SAME (epoch, seq) at DIFFERENT sites — got %v and %v", ackA, ackB)
	}

	// A restart at site-a re-issues seq from 1 — this world is ordinary,
	// not contrived: seq is issued per (site_id, node_epoch)
	// [store/log.go's watermarkTx].
	w9cAckAt(t, st, ackE1, tripX.TripID, "Pad Site Control", 1_700_000_100_000)
	w9cAckAt(t, st, ackA, tripY.TripID, "Pad Site Control", 1_700_000_200_000)
	w9cAckAt(t, st, ackE2, tripX.TripID, "Pad Site Control", 1_700_000_300_000)
	w9cAckAt(t, st, ackB, tripY.TripID, "Ice Camp B", 1_700_000_400_000)

	// Retract ONLY ack_e1 and ONLY ack_a, both authored at site-b.
	rE1 := w9cRetract(t, st, w9cKey{here, 1, 0}, ackE1, "Ice Camp B", 1_700_000_500_000)
	rA := w9cRetract(t, st, w9cKey{here, 1, 0}, ackA, "Ice Camp B", 1_700_000_600_000)

	// Exactly two retractions exist, and they name exactly the two
	// intended targets — a fixture that retracted a third ack would prove
	// nothing about matching.
	if got := w9cRetractionTargets(t, st); len(got) != 2 || got[0] != ackE1 || got[1] != ackA {
		t.Fatalf("W9c-11 fixture: the log's retractions (%v at %v, %v) name %v, want exactly [%v %v]", rE1, rA, "", got, ackE1, ackA)
	}

	// Both trips reach overdue_live on the monotonic clock.
	d.OnApplied(envX, time.Now().Add(-time.Minute+300*time.Millisecond), true)
	d.OnApplied(envY, time.Now().Add(-time.Minute+300*time.Millisecond), true)
	time.Sleep(400 * time.Millisecond)

	b := mustEval(t, d)

	// FIXTURE GUARD, and it can fail: both trips must be alert-worthy rows
	// that are currently SUPPRESSED BY AN ACK. Without this, "Banner() is
	// empty" would be equally true of a board with nothing on it.
	for _, tr := range []struct {
		id   string
		half string
	}{{tripX.TripID, "half 1 (epoch-restart twin X)"}, {tripY.TripID, "half 2 (cross-site collision twin Y)"}} {
		a := findAlert(t, b, tr.id)
		if a.Status != StatusOverdueLive {
			t.Fatalf("W9c-11 fixture: %s is %q, want overdue_live — an empty-banner assertion over a countdown row proves nothing", tr.half, a.Status)
		}
		// Reported, not fatal: under a blind matcher the surviving ack is
		// wrongly retracted and this guard fires — but so must the
		// criterion's own suppression pin below, and the pin is the message
		// that names the defect. t.Fatalf here would hide it.
		if !a.Acked {
			t.Errorf("W9c-11 fixture: %s reports Acked == false — its surviving ack is not suppressing, so the assertion below cannot distinguish a correct matcher from a blind one", tr.half)
		}
	}

	// ⟨half 1⟩ ack_e2 still suppresses: X's Banner() is EMPTY.
	if rows := w9cBannerRows(b, tripX.TripID); len(rows) != 0 {
		t.Errorf("W9c-11 half 1: only ack_e1 %v was retracted, but X is ALERTING (%d row(s)) — ack_e2 %v was matched too, so the retraction matcher is consulting fewer than all three ack_* key columns (a seq-only or (site_id, seq) matcher). Banner: %s",
			ackE1, len(rows), ackE2, w9cBannerStatuses(b))
	}

	// ⟨half 2⟩ ack_b still suppresses: Y's Banner() is EMPTY.
	if rows := w9cBannerRows(b, tripY.TripID); len(rows) != 0 {
		t.Errorf("W9c-11 half 2: only ack_a %v was retracted, but Y is ALERTING (%d row(s)) — ack_b %v was matched too, so the retraction matcher is site-blind ((node_epoch, seq) only). Banner: %s",
			ackA, len(rows), ackB, w9cBannerStatuses(b))
	}
}
