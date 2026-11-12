// syncstatus_selfhistory_test.go — the two Defect-1 view units
// (design/app-fixes-v1.md §2.7): the T3-companion (pending row) and T9
// (vacuous annotation row).
//
// NEW FILE beside syncstatus_test.go / muster_rebuild_test.go, per the
// design's tracking rule. Fixture style is syncstatus_test.go's: hand-built
// repl.Snapshot values against buildSyncStatusView — the engine behaviour
// that PRODUCES the pending/vacuous facts is certified in internal/repl;
// this file certifies only that THIS package renders them.
//
// COMPILE-BLOCKED PENDING BUILD, deliberately: both tests reference
// repl.PeerSnapshot.SelfHistoryPending / .SelfHistoryVacuous, which do not
// exist until commit 2 lands. "Pre-fix" therefore cannot compile, and each
// test's honest fail-before is its NAMED MUTANT (§2.7/§6.4), not a baseline
// run.
package web

import (
	"strings"
	"testing"

	"sitecontrol/internal/repl"
)

// T3-companion (§2.7, hard requirement): SelfHistoryPending=true with fresh
// contact and OK streams must render NOT Healthy, and the verdict must name
// the muster consequence. This preserves the file's invariant the cheap way:
// the pending case only ever leaves Healthy unset.
//
// INTENDED KILLER MUTANT: L4, the case-dead mutant
// (`case pv.SelfHistoryPending:` → unreachable, §6.4 item 3). Measured: L4
// survived all 13 targeted web tests — under it this exact fixture falls
// through to the default branch and renders Healthy=true, "ok — last heard
// 4 seconds ago". This test is the ONLY check that kills it. Assertions are
// AF3's produced embodiment (APPFIX-PROBES.md), kept exactly.
func TestPendingSelfHistoryRendersNotHealthyEvenWithFreshContactAndOkStreams(t *testing.T) {
	snap := repl.Snapshot{
		SiteID: "site-b",
		Peers:  []repl.PeerSnapshot{{SiteID: "site-a", LastContactAgoSec: f(4), Drains: 3, SelfHistoryPending: true}},
		Streams: []repl.StreamStatus{
			{StreamKey: repl.StreamKey{SiteID: "site-a", NodeEpoch: 1}, Watermark: 5, Head: 5, State: repl.StateOK},
		},
	}
	view := buildSyncStatusView(snap, testSyncInterval)
	if len(view.Peers) != 1 {
		t.Fatalf("got %d peers, want 1", len(view.Peers))
	}
	pv := view.Peers[0]
	if pv.Healthy {
		t.Fatalf("SelfHistoryPending=true rendered Healthy=true: green over missing own history; verdict=%q", pv.Verdict)
	}
	if !strings.Contains(pv.Verdict, "muster") {
		t.Fatalf("verdict %q does not name the muster consequence", pv.Verdict)
	}
}

// T9 (§2.7): W6's rendering row. A vacuous latch (verified answer holding
// no PRE-WIPE history for this post) renders Healthy=true — ruled
// (VERDICT-2 §2.1 am.1; APPFIX-ARBITER-VERDICT §2.1c): a permanently-red
// first install trains operators to ignore red — with the honest clause
// appended to the default verdict, never as a verdict branch (§2.6's PI-6
// guard sentence).
//
// INTENDED KILLER MUTANT: annotation-dead (the clause never renders, e.g.
// `if pv.SelfHistoryVacuous && false`, §6.4 item 7) — red on the contains
// assert. Produced once by the round-2 witness; re-produced at the build
// gate.
//
// POSITIVE CONTROL / inverse guard: the same fixture with vacuous=false
// must render WITHOUT the clause — the clause channel demonstrably both
// fires and stays quiet, so an unconditionally-appended clause (the
// annotation gone always-on, W6's other failure direction) is also caught.
func TestVacuousSelfHistoryAnswerRendersHealthyWithTheNoPreWipeHistoryAnnotation(t *testing.T) {
	mk := func(vacuous bool) peerView {
		snap := repl.Snapshot{
			SiteID: "site-b",
			Peers:  []repl.PeerSnapshot{{SiteID: "site-a", LastContactAgoSec: f(4), Drains: 3, SelfHistoryVacuous: vacuous}},
			Streams: []repl.StreamStatus{
				{StreamKey: repl.StreamKey{SiteID: "site-a", NodeEpoch: 1}, Watermark: 5, Head: 5, State: repl.StateOK},
			},
		}
		view := buildSyncStatusView(snap, testSyncInterval)
		if len(view.Peers) != 1 {
			t.Fatalf("got %d peers, want 1", len(view.Peers))
		}
		return view.Peers[0]
	}
	const clause = "peer reports no pre-wipe history for this post"

	pv := mk(true)
	if !pv.Healthy {
		t.Fatalf("a vacuous latch rendered NOT Healthy (verdict %q) — ruled Healthy=true: the page must not go red on information that does not exist in the mesh (VERDICT-2 §2.1 am.1)", pv.Verdict)
	}
	if !strings.Contains(pv.Verdict, clause) {
		t.Fatalf("verdict %q does not carry the vacuous clause %q — the annotation is dead: \"verified empty answer\" and \"verified recovery\" become indistinguishable on the page (W6)", pv.Verdict, clause)
	}

	plain := mk(false)
	if !plain.Healthy {
		t.Fatalf("non-vacuous healthy fixture rendered NOT Healthy (verdict %q) — the annotation grew into a verdict branch", plain.Verdict)
	}
	if strings.Contains(plain.Verdict, clause) {
		t.Fatalf("the vacuous clause renders on a NON-vacuous peer (verdict %q) — the annotation is unconditional, so it no longer distinguishes anything", plain.Verdict)
	}
}
