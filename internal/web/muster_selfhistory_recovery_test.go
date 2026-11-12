// muster_selfhistory_recovery_test.go — T2 (design/app-fixes-v1.md §2.7):
// Defect 1's actual failure, end-to-end at the artefact D-6 cares about —
// the rendered muster sheet — in one sequence: offline boot, link-later,
// history-back, green-is-honest.
//
// NEW FILE beside muster_rebuild_test.go, per the design's tracking rule
// (§5: muster_rebuild_test.go is tracked as of 7261f3e and must not be
// modified). The camp harness (newCamp, wipeAndReimage, renderMuster,
// settle) is reused from that file, not duplicated.
//
// FAIL-BEFORE (produced against the shipped tree — this file compiles
// pre-fix because it references no new field): after the link appears and
// routine pulls succeed, the rebuilt Pad's on-site roster stays EMPTY while
// its sync-status view reads Healthy — probe 1's "empty camp behind a green
// status" (APPFIX-PROBES.md probe 1), at the web level. The roster assert
// below is that red.
//
// This is the offline-install / link-up sequence as one test, not a proxy:
// the boot-time §8.6 Rejoin inside wipeAndReimage fails non-fatally against
// an unreachable peer, the binary serves anyway, and recovery must complete
// on the ROUTINE pull path once the link exists.
package web

import (
	"context"
	"testing"
)

// peerViewOf finds one peer's rendered view row by site id.
func peerViewOf(t *testing.T, view syncStatusView, siteID string) peerView {
	t.Helper()
	for _, pv := range view.Peers {
		if pv.SiteID == siteID {
			return pv
		}
	}
	t.Fatalf("sync-status view has no row for peer %q", siteID)
	return peerView{}
}

func TestRebuiltPadBootedWithItsPeerUnreachableRecoversTheMusterOnceTheLinkAppears(t *testing.T) {
	c := newCamp(t)
	onSite := append(append([]string{}, names(c.arrivedFromA)...), names(c.localAtB)...)

	// The link to Ice Road is DOWN when the Pad is wiped and re-imaged:
	// every exchange 503s ("not ready"), so the boot-time Rejoin inside
	// wipeAndReimage fails non-fatally — the offline-install boot.
	c.a.lh.set(nil)
	c.b = wipeAndReimage(t, c.b)

	// The blank Pad serves anyway, with an empty sheet (fixture check —
	// identical in both trees; a non-empty roster here means no offline
	// boot was posed at all).
	mid := renderMuster(t, c.b)
	if got := mid.tally(t, "on-site"); got != 0 {
		t.Fatalf("fixture: the rebuilt Pad already reads %d on site with the link still down — no offline boot was posed", got)
	}
	// While its own pre-re-image history is unrecovered, the sync-status
	// view must NOT read healthy.
	if pv := peerViewOf(t, buildSyncStatusView(c.b.eng.Snapshot(), c.b.cfg.SyncInterval()), "site-a"); pv.Healthy {
		t.Fatalf("link down, own history unrecovered, and the peer view reads Healthy=true (verdict %q)", pv.Verdict)
	}

	// The link appears. Routine pull cycles run — exactly what Run's
	// ticker calls; no restart, no second Rejoin.
	c.a.lh.set(c.a.eng.SyncHandler())
	c.b.eng.PullAll(context.Background())
	c.b.eng.PullAll(context.Background())

	// THE RED, pre-fix: the roster stays empty while the view below reads
	// green. Post-fix: every pre-wipe name is back.
	pvAfter := peerViewOf(t, buildSyncStatusView(c.b.eng.Snapshot(), c.b.cfg.SyncInterval()), "site-a")
	after := renderMuster(t, c.b)
	if got := after.tally(t, "on-site"); got != len(onSite) {
		t.Fatalf("own history NOT recovered on the routine path after the link appeared: the rebuilt Pad reads %d on site, want %d, "+
			"while its sync-status view reads Healthy=%v (verdict %q) — an empty camp behind a green status (probe 1, D-6's artefact)",
			got, len(onSite), pvAfter.Healthy, pvAfter.Verdict)
	}
	gotNames, _, _ := after.roster(t)
	if !sortedEqual(gotNames, onSite) {
		t.Fatalf("presence only partially recovered: the rebuilt Pad's roster names %v, want %v", gotNames, onSite)
	}

	// And green is now honest: recovery is complete, so the view is
	// Healthy for the right reason.
	if !pvAfter.Healthy {
		t.Fatalf("own history recovered in full yet the peer view is still not healthy: verdict %q", pvAfter.Verdict)
	}

	// The rebuilt post agrees with its peer again, not merely "looks right".
	settle(t, c.a, c.b)
	if got, want := string(mustDumpOf(t, c.b.st)), string(mustDumpOf(t, c.a.st)); got != want {
		t.Fatal("the rebuilt Pad and Ice Road do not agree after link-up recovery")
	}
}
