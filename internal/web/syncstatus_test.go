// C10 done-when tests, pure view-model layer (chunk-plan-v2 §C10). These
// test buildSyncStatusView/buildPeerView directly against hand-built
// repl.Snapshot fixtures — the engine behaviour that PRODUCES StateRegressed
// and StateSchemaMismatch is C3a/C3b's own responsibility, independently
// verified in internal/repl; this file verifies only that THIS package
// renders those states correctly, using the exact same repl.State* constants
// C3a/C3b's engine emits.
//
// Naming rule (BUILDER_BRIEF_RULES #4): every test name states what it
// proves; the body proves exactly that, nothing wider.
package web

import (
	"strings"
	"testing"

	"sitecontrol/internal/repl"
)

func f(v float64) *float64 { return &v }

// --- the core not-done-if guard: "the UI reads healthy while pulls are
// failing" (§8.9's "silent alerting system") ---

// naiveHealthyIgnoringTransportError replicates the EXACT wrong
// implementation this chunk exists to prevent: trusting the cached
// per-stream state alone, never looking at the peer's own transport error.
// It exists ONLY to prove the fixture below is capable of tricking that
// wrong implementation — i.e. that the test can fail — per BUILDER_BRIEF
// RULES #5.
func naiveHealthyIgnoringTransportError(streams []repl.StreamStatus) bool {
	for _, s := range streams {
		if s.State != repl.StateOK {
			return false
		}
	}
	return true
}

func TestBuildSyncStatusViewStaleOkStreamStateDoesNotOverrideAFreshTransportError(t *testing.T) {
	// The scenario: a peer answered fine a while ago (its stream is cached
	// as "ok" from that pull), but the MOST RECENT sync attempt failed —
	// exactly what happens on the tick right after a sever (§8.9/C4): the
	// stream table hasn't been touched (drainSite never got past
	// /sync/streams to write a new StreamStatus), but LastError is now set.
	snap := repl.Snapshot{
		SiteID: "site-b",
		Peers: []repl.PeerSnapshot{
			{SiteID: "site-a", LastContactAgoSec: f(45), LastError: "repl: GET /sync/streams: dial tcp: connect: connection refused", ErrorKind: repl.ErrorKindUnreachable, Drains: 12},
		},
		Streams: []repl.StreamStatus{
			{StreamKey: repl.StreamKey{SiteID: "site-a", NodeEpoch: 1}, Watermark: 500, Head: 500, State: repl.StateOK},
		},
	}

	// Proof the fixture CAN fail (rule #5): a naive stream-only check reads
	// this exact fixture as healthy.
	if !naiveHealthyIgnoringTransportError(snap.Streams) {
		t.Fatal("fixture is not capable of tricking a stream-only check — strengthen the fixture before trusting the real assertion below")
	}

	view := buildSyncStatusView(snap, testSyncInterval)
	if len(view.Peers) != 1 {
		t.Fatalf("got %d peers, want 1", len(view.Peers))
	}
	pv := view.Peers[0]
	if pv.Healthy {
		t.Fatalf("peer rendered Healthy=true while its last sync attempt FAILED (error: %q) — this is the exact silent-alerting-system defect §8.9 exists to prevent", pv.LastError)
	}
	// FIX2 / L01 F-5: the verdict says what failed IN WORDS — it is also
	// the site page's STALE line, and the raw error carries the peer's base
	// URL and t1 parameter. The raw text is not lost: it stays on LastError,
	// which the status page's technical "last error" line renders.
	// ⟦was: Contains(pv.Verdict, "connection refused")⟧
	if !strings.Contains(pv.Verdict, "FAILED") || !strings.Contains(pv.Verdict, "nothing answered") {
		t.Fatalf("verdict = %q, want it to surface the transport failure, not stay silent about it", pv.Verdict)
	}
	if !strings.Contains(pv.LastError, "connection refused") {
		t.Fatalf("LastError = %q, want the raw error kept for the technical line", pv.LastError)
	}
}

// --- §8.5 visible refusal: schema mismatch must render as queued/refused,
// never as silence ---

func TestBuildSyncStatusViewSchemaMismatchRendersAsVisibleRefusalNotSilence(t *testing.T) {
	snap := repl.Snapshot{
		SiteID: "site-b",
		Peers: []repl.PeerSnapshot{
			{SiteID: "site-x", LastContactAgoSec: f(5), Drains: 3},
		},
		Streams: []repl.StreamStatus{
			{StreamKey: repl.StreamKey{SiteID: "site-x", NodeEpoch: 1}, Watermark: 0, Head: 4,
				State: repl.StateSchemaMismatch, Detail: "schema_version 6 != 5"},
		},
	}
	view := buildSyncStatusView(snap, testSyncInterval)
	pv := view.Peers[0]
	if pv.Healthy {
		t.Fatal("peer with a schema-mismatched stream rendered Healthy=true")
	}
	if !strings.Contains(pv.Verdict, "STALLED") {
		t.Fatalf("verdict = %q, want it to say the log is stalled, not read as merely reachable-and-fine", pv.Verdict)
	}
	// The per-stream row itself must name the refusal, not a generic "error".
	var sv *streamView
	for i := range view.AllStreams {
		if view.AllStreams[i].SiteID == "site-x" {
			sv = &view.AllStreams[i]
		}
	}
	if sv == nil {
		t.Fatal("schema-mismatched stream missing from AllStreams — a refusal must never be silently dropped")
	}
	if sv.OK {
		t.Fatal("schema-mismatched stream rendered OK=true")
	}
	if !strings.Contains(sv.Label, "SCHEMA MISMATCH") || !strings.Contains(sv.Label, "queued") {
		t.Fatalf("stream label = %q, want it to name the §8.5 visible refusal (queued at author)", sv.Label)
	}
}

// --- §8.9 "peer log regressed" must render as its own distinct state ---

func TestBuildSyncStatusViewRegressedStreamRendersDistinctlyNotAsOk(t *testing.T) {
	snap := repl.Snapshot{
		SiteID: "site-b",
		Peers:  []repl.PeerSnapshot{{SiteID: "site-x", LastContactAgoSec: f(2)}},
		Streams: []repl.StreamStatus{
			{StreamKey: repl.StreamKey{SiteID: "site-x", NodeEpoch: 1}, Watermark: 10, Head: 3,
				State: repl.StateRegressed, Detail: "peer log regressed (§8.9): reported head 3 < previously seen 10"},
		},
	}
	view := buildSyncStatusView(snap, testSyncInterval)
	if view.Peers[0].Healthy {
		t.Fatal("peer with a regressed stream rendered Healthy=true")
	}
	sv := view.AllStreams[0]
	if sv.OK {
		t.Fatal("regressed stream rendered OK=true")
	}
	if !strings.Contains(sv.Label, "REGRESSED") {
		t.Fatalf("stream label = %q, want it to name the regression distinctly", sv.Label)
	}
	if strings.Contains(sv.Label, "SCHEMA MISMATCH") {
		t.Fatal("regressed state mislabelled as schema mismatch")
	}
}

// --- carry-forward row 19: frozen anchor + fresh contact + refused stream
// must render as ONE story, not scattered/contradictory signals ---

func TestBuildSyncStatusViewFrozenAnchorFreshContactAndRefusedStreamRenderAsOneStory(t *testing.T) {
	snap := repl.Snapshot{
		SiteID: "site-b",
		Peers: []repl.PeerSnapshot{
			// Contact is FRESH (5s ago: the peer answers /sync/streams fine)
			// but the anchor is OLD (900s: no full drain has completed since
			// then) because its stream is permanently refused.
			{SiteID: "site-x", LastContactAgoSec: f(5), AnchorAgoSec: f(900), Drains: 40},
		},
		Streams: []repl.StreamStatus{
			{StreamKey: repl.StreamKey{SiteID: "site-x", NodeEpoch: 1}, Watermark: 0, Head: 4,
				State: repl.StateSchemaMismatch, Detail: "schema_version 6 != 5"},
		},
	}
	view := buildSyncStatusView(snap, testSyncInterval)
	pv := view.Peers[0]
	if pv.Healthy {
		t.Fatal("a peer with a frozen anchor behind a refused stream rendered Healthy=true")
	}
	// ONE sentence must carry all three clauses: reachable, stalled, AND the
	// frozen anchor age — never just one or two of them. (900s renders as
	// "15 minutes ago" per the shared internal/humantime phrasing.)
	for _, want := range []string{"reachable", "5 seconds ago", "STALLED", "frozen", "15 minutes ago"} {
		if !strings.Contains(pv.Verdict, want) {
			t.Fatalf("verdict = %q, missing %q — the three signals must be ONE story, not scattered", pv.Verdict, want)
		}
	}
}

// --- a peer never successfully contacted must not read as healthy either ---

func TestBuildSyncStatusViewNeverContactedPeerIsNotHealthy(t *testing.T) {
	snap := repl.Snapshot{
		SiteID: "site-b",
		Peers:  []repl.PeerSnapshot{{SiteID: "site-x"}}, // LastContactAgoSec nil: zero value
	}
	view := buildSyncStatusView(snap, testSyncInterval)
	pv := view.Peers[0]
	if pv.Healthy {
		t.Fatal("never-contacted peer rendered Healthy=true")
	}
	// FIX2 cluster K (sc-test L08 T-074): contact is in-memory, so the
	// verdict says "not heard from since this post started", never "never
	// contacted" — which after a restart read as "this post has never heard
	// from it". ⟦was: Contains "never contacted"⟧
	if !strings.Contains(pv.Verdict, "not heard from since this post started") {
		t.Fatalf("verdict = %q, want it to say not heard from since this post started", pv.Verdict)
	}
}

// --- the reserved-but-empty clock-offset slot: exists, shows nothing,
// never a fabricated number (§8.9's stated production deferral) ---

func TestBuildSyncStatusViewHealthyPeerStillShowsOkWithAge(t *testing.T) {
	snap := repl.Snapshot{
		SiteID: "site-b",
		Peers:  []repl.PeerSnapshot{{SiteID: "site-a", LastContactAgoSec: f(11), Drains: 9}},
		Streams: []repl.StreamStatus{
			{StreamKey: repl.StreamKey{SiteID: "site-a", NodeEpoch: 1}, Watermark: 5, Head: 5, State: repl.StateOK},
		},
	}
	view := buildSyncStatusView(snap, testSyncInterval)
	pv := view.Peers[0]
	if !pv.Healthy {
		t.Fatalf("a peer with fresh contact, no error, and every stream OK rendered Healthy=false: verdict=%q", pv.Verdict)
	}
	if !strings.Contains(pv.Verdict, "ok") || !strings.Contains(pv.Verdict, "11 seconds ago") {
		t.Fatalf("verdict = %q, want it to say ok with the actual age", pv.Verdict)
	}
}

// --- carry-forward row 26: a phantom empty self-announced stream (the
// handleStreams "always announce current epoch" quirk, now epoch-bound
// inert per C3b) must render as an ordinary harmless row, never amplified
// into something alarming or mistaken for a real peer stream ---

func TestBuildSyncStatusViewPhantomEmptySelfStreamRendersAsOrdinaryNotAlarming(t *testing.T) {
	snap := repl.Snapshot{
		SiteID: "site-b", // this IS "our own" site — matches Own's definition
		Peers:  []repl.PeerSnapshot{{SiteID: "site-a", LastContactAgoSec: f(3)}},
		Streams: []repl.StreamStatus{
			// A zero-head, zero-watermark, State-OK entry bearing OUR OWN
			// site_id — exactly the shape a harmless phantom announce takes.
			{StreamKey: repl.StreamKey{SiteID: "site-b", NodeEpoch: 1}, Watermark: 0, Head: 0, State: repl.StateOK},
			// site-a's OWN real, healthy stream — present so the assertion
			// below (the phantom must not corrupt site-a's verdict) is
			// actually exercising the corruption question, not merely
			// re-testing the already-covered "no streams observed" case.
			{StreamKey: repl.StreamKey{SiteID: "site-a", NodeEpoch: 1}, Watermark: 20, Head: 20, State: repl.StateOK},
		},
	}
	view := buildSyncStatusView(snap, testSyncInterval)
	var sv *streamView
	for i := range view.AllStreams {
		if view.AllStreams[i].SiteID == "site-b" {
			sv = &view.AllStreams[i]
		}
	}
	if sv == nil {
		t.Fatal("own-site stream entry missing from AllStreams — self-history entries must never be silently dropped")
	}
	if !sv.Own {
		t.Fatal("own-site stream not flagged Own")
	}
	if !sv.OK || sv.Label != "ok" {
		t.Fatalf("harmless empty own stream rendered as %+v, want a plain ok row", sv)
	}
	// And it must not be attributed to any configured PEER's verdict — it
	// belongs to no peer's SiteID, so site-a's own verdict is unaffected.
	if !view.Peers[0].Healthy {
		t.Fatalf("an unrelated own-history phantom entry corrupted a real peer's verdict: %+v", view.Peers[0])
	}
}

// --- unavailable engine: must be an explicit refusal, never silence and
// never a false healthy reading ---

func TestBuildSyncStatusViewUnavailableIsExplicitNotSilent(t *testing.T) {
	view := syncStatusView{Available: false} // what buildSyncSection returns when SyncSnapshot is nil
	if view.Available {
		t.Fatal("sanity: Available should be false in this fixture")
	}
	if len(view.Peers) != 0 {
		t.Fatal("an unavailable view should carry no fabricated peer data")
	}
}
