// muster_sync_print_test.go — SYNC-HONESTY (2026-09-09): what the muster
// says about freshness. The top bar carries the sync chip on screen;
// muster.html renders its own per-peer block from the SAME verdict, and
// these tests hold that block to the render time and to the chip's words.
//
// AMENDED by James, 2026-10-01: print = roster only (no sync verdicts, no
// counts/summary pages). Until then the block also PRINTED,
// unconditionally, and this file asserted that. It is now screen-only: the
// on-screen block is unchanged (asserted below on all three routes, whose
// screen renderings still carry it) and the paper is asserted NOT to carry
// it. The tests whose names said "printed" were renamed to say what they
// now prove.
//
// Class-name trap (F19): assertions on the page use data-* hooks and
// class="…" attributes; assertions about the stylesheet parse it with
// printcss_test.go's cascade-aware helpers and say so.
package web

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/repl"
)

// printedSyncBlock returns the rendered sync section, or "".
func printedSyncBlock(body string) string {
	m := regexp.MustCompile(`(?s)<section class="sync-scope[^"]*" data-muster-section="sync".*?</section>`).FindString(body)
	return m
}

func TestTheMusterSyncBlockCarriesARenderTimeAndAPerPeerVerdictOnScreen(t *testing.T) {
	srv := musterWithSync(t, func() repl.Snapshot {
		return repl.Snapshot{
			SiteID: "site-a",
			Peers: []repl.PeerSnapshot{
				{SiteID: "site-b", LastContactAgoSec: f(4), Drains: 9},
				{SiteID: "site-c", LastContactAgoSec: f(3 * 3600), Drains: 2},
			},
			Streams: []repl.StreamStatus{
				{StreamKey: repl.StreamKey{SiteID: "site-b", NodeEpoch: 1}, Watermark: 3, Head: 3, State: repl.StateOK},
				{StreamKey: repl.StreamKey{SiteID: "site-c", NodeEpoch: 1}, Watermark: 3, Head: 3, State: repl.StateOK},
			},
		}
	})
	for _, path := range []string{"/muster", "/muster/print", "/muster/print?visits="} {
		body := rawPage(t, srv, path)
		block := printedSyncBlock(body)
		if block == "" {
			t.Fatalf("%s: no sync block on screen", path)
		}
		// One line per peer, each carrying its own verdict — never a merged
		// "some peers degraded" that hides WHICH post the sheet may be
		// missing records from.
		if !strings.Contains(block, `data-sync-peer="site-b" data-sync-health="ok"`) {
			t.Errorf("%s: site-b's ok line missing", path)
		}
		if !strings.Contains(block, `data-sync-peer="site-c" data-sync-health="degraded"`) {
			t.Errorf("%s: site-c's degraded line missing", path)
		}
		if !strings.Contains(block, "SYNC — 1 of 2 peer(s) degraded") {
			t.Errorf("%s: block headline is not the shared summary; block: %s", path, block)
		}
		// The render time is on the sheet (U3's RenderedAt) AND the block
		// anchors its verdicts to it — "as at" the moment the paper was made.
		stamp := regexp.MustCompile(`as at [0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2} [A-Z]+`)
		if !stamp.MatchString(block) {
			t.Errorf("%s: block does not anchor to the render time; block: %s", path, block)
		}
		// No stream table on paper — the per-peer verdict is the whole story
		// the sheet needs; a wall of stream rows would drown the roll.
		if strings.Contains(block, "<table") {
			t.Errorf("%s: the printed sync block grew a table", path)
		}
		// The footnote NAMES which counts a stale peer can move (post-review
		// narrowing, REVIEW-SYNC-HONESTY 2026-09-09) and makes no blanket
		// "nothing is changed" claim — that sentence read as a retraction of
		// the caution above it, on a safety document.
		flat := strings.Join(strings.Fields(block), " ")
		for _, want := range []string{
			"ON SITE and AWAY are built from this post's own records",
			"ON THE ROAD and NOT TRACKED depend on arrivals the peer records",
			"on the road may be over-stated and not tracked under-stated",
		} {
			if !strings.Contains(flat, want) {
				t.Errorf("%s: footnote lacks %q", path, want)
			}
		}
		for _, banned := range []string{"Nothing this sheet counts is changed", "nothing is changed", "not changed by this line"} {
			if strings.Contains(flat, banned) {
				t.Errorf("%s: footnote carries the over-broad claim %q", path, banned)
			}
		}
	}
}

func TestTheMusterSyncBlockSaysTheSameWordsAsTheChip(t *testing.T) {
	// Glass and paper must not drift into two vocabularies. The chip's value
	// and the block's headline both come from syncStatusView.Summary; this
	// pins that they are the same string on one render, in every case the
	// bar draws.
	cases := map[string]func() repl.Snapshot{
		"degraded": func() repl.Snapshot { return stalledSnapshot(3 * time.Hour) },
		"healthy":  func() repl.Snapshot { return stalledSnapshot(2 * time.Second) },
		"no peers": func() repl.Snapshot { return repl.Snapshot{SiteID: "site-a"} },
		"unwired":  nil,
	}
	chipValue := regexp.MustCompile(`<span class="chip-label">sync</span>\s*<span class="chip-value">([^<]*)</span>`)
	for name, snap := range cases {
		srv, _ := newMusterTestServer(t, 0)
		if snap != nil {
			srv.SyncSnapshot = snap
		}
		body := rawPage(t, srv, "/muster/print")
		m := chipValue.FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("%s: no sync chip", name)
		}
		if !strings.Contains(body, `data-sync-summary="`+m[1]+`"`) {
			t.Errorf("%s: chip says %q but the printed block's summary differs; block: %s", name, m[1], printedSyncBlock(body))
		}
		if !strings.Contains(printedSyncBlock(body), "SYNC — "+m[1]) {
			t.Errorf("%s: block headline is not %q", name, "SYNC — "+m[1])
		}
	}
}

func TestSyncSummaryIsTheChipsFourLegacyStrings(t *testing.T) {
	// The four strings the bar drew before Summary existed, verbatim.
	ok := peerView{Healthy: true}
	bad := peerView{}
	for _, tc := range []struct {
		view syncStatusView
		want string
	}{
		{syncStatusView{Available: false}, "cannot tell"},
		{syncStatusView{Available: true, Peers: []peerView{ok, bad, bad}}, "2 of 3 peer(s) degraded"},
		{syncStatusView{Available: true, Peers: []peerView{ok, ok}}, "2 peer(s) healthy"},
		{syncStatusView{Available: true}, "no peers configured"},
	} {
		if got := tc.view.Summary(); got != tc.want {
			t.Errorf("Summary() = %q, want %q", got, tc.want)
		}
	}
}

// TestTheSyncBlockIsOnScreenButNotOnThePrintedRoster — James, 2026-10-01:
// print = roster only (no sync verdicts, no counts/summary pages).
//
// This REPLACES ⟦was: TestTheSyncBlockSurvivesPrintingWhileTheBarThatCarriesTheChipDoesNot⟧,
// which asserted the opposite (the block must reach paper because the bar
// carrying the chip does not). The ruling moved the sync verdict off the
// paper deliberately; this asserts both halves of it, so it fails if the
// block creeps back onto paper AND if the screen block is removed with it.
// Stylesheet-level, with printcss_test.go's cascade-aware parser.
func TestTheSyncBlockIsOnScreenButNotOnThePrintedRoster(t *testing.T) {
	srv := musterWithSync(t, func() repl.Snapshot { return stalledSnapshot(3 * time.Hour) })
	for _, path := range []string{"/muster", "/muster/print"} {
		body := rawPage(t, srv, path)
		rules := paperStylesheet(t, body)
		// Precondition (rule 6): the paper cascade really does hide some
		// chrome, so "hidden" below is read from a working print block.
		if _, hidden := hiddenOnPaper(rules, "topbar"); !hidden {
			t.Fatalf("%s: precondition: the top bar is not hidden on paper — this test is not reading a working print cascade", path)
		}
		// Precondition: the block exists in the served HTML with the
		// degraded verdict in it — the screen half of the rule. Without it
		// "hidden on paper" would be true of a block that is not there.
		if !strings.Contains(printedSyncBlock(body), "STALE") {
			t.Fatalf("%s: the sync block (screen) does not carry the STALE verdict — the live page must keep it", path)
		}
		// THE RULING: off the paper. Both the plain and the degraded form.
		for _, class := range []string{"sync-scope", "sync-scope-bad"} {
			if _, hidden := hiddenOnPaper(rules, class); !hidden {
				t.Errorf("%s: .%s is NOT hidden on paper — James, 2026-10-01: print = roster only (no sync verdicts)", path, class)
			}
		}
		// ...and still shown on screen.
		if got, ok := screenClassValue(rules, "sync-scope", "display"); ok && strings.TrimSpace(got) == "none" {
			t.Errorf("%s: .sync-scope is hidden ON SCREEN — the ruling changed the paper only", path)
		}
	}
}

func TestTheSyncBlockChangesNothingTheMusterCounts(t *testing.T) {
	// SCOPE, stated precisely (post-review): this proves that RENDERING the
	// block does not alter the tally or the grouping — the same store
	// renders the same numbers whether the engine is healthy, stalled, or
	// unwired. It says NOTHING about whether the sheet's numbers depend on
	// peer-authored rows: this fixture holds no peer rows, so it is
	// structurally incapable of detecting that dependence (the "zero from
	// a method that cannot produce a one" trap). The reviewer's fixture
	// with peer rows measured the dependence — on the road 2 → 0, not
	// tracked 0 → 1 on delivery of one peer arrival — and the printed
	// footnote now names it. Do not read a PASS here as "the sheet is
	// unaffected by a stale peer".
	srv, st := newMusterTestServer(t, 0)
	co, err := domain.CreateCompany(st, "site-a", 1, "Kakivik Asset Management", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, name := range []string{"Dana Driver", "Pat Passenger", "Sam Second"} {
		p, err := domain.CreatePerson(st, "site-a", 1, name, &co.CompanyID, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.PersonID)
	}
	checkIn(t, st, "site-a", ids[0], ids[1:])
	tallies := map[string]string{}
	for name, snap := range map[string]func() repl.Snapshot{
		"unwired":  nil,
		"healthy":  func() repl.Snapshot { return stalledSnapshot(2 * time.Second) },
		"degraded": func() repl.Snapshot { return stalledSnapshot(3 * time.Hour) },
	} {
		srv.SyncSnapshot = snap
		body := rawPage(t, srv, "/muster")
		i := strings.Index(body, `data-tally-count="on-site">`)
		if i < 0 {
			t.Fatalf("%s: no on-site tally", name)
		}
		j := strings.Index(body[i:], "<")
		tallies[name] = body[i+len(`data-tally-count="on-site">`) : i+j]
		if strings.Count(body, "data-company-group=") != strings.Count(rawPage(t, srv, "/muster"), "data-company-group=") {
			t.Fatalf("%s: grouping not stable across two renders", name)
		}
	}
	if tallies["unwired"] != "3" || tallies["healthy"] != "3" || tallies["degraded"] != "3" {
		t.Fatalf("the on-site tally moved with the sync state: %v (want 3 in every case)", tallies)
	}
}
