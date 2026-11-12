// fix3_b_sync_wording_test.go — FIX3 stream B, the rendered half
// (FINDINGS-R5 F6, F7's face, F9, F3d). Each test was confirmed to FAIL
// against e3d507d and PASS after. Peer snapshots are built from the JSON
// /debug/sync-status emits, so the facts added in this round (poke_error,
// poke_refused_kind) leave the file compiling against e3d507d, where the
// assertions — not the build — fail.
package web

import (
	"encoding/json"
	"html"
	"regexp"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/repl"
	"sitecontrol/internal/store"
)

// b3Peer decodes one peer snapshot from its /debug/sync-status JSON.
func b3Peer(t *testing.T, js string) repl.PeerSnapshot {
	t.Helper()
	var p repl.PeerSnapshot
	if err := json.Unmarshal([]byte(js), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

// b3Snap is a snapshot of this post (own) with one peer whose stream is ok.
func b3Snap(t *testing.T, own, peerJS string) func() repl.Snapshot {
	p := b3Peer(t, peerJS)
	return func() repl.Snapshot {
		return repl.Snapshot{SiteID: own, UptimeSec: 600, Peers: []repl.PeerSnapshot{p},
			Streams: []repl.StreamStatus{{StreamKey: repl.StreamKey{SiteID: p.SiteID, NodeEpoch: 1}, Watermark: 3, Head: 3, State: repl.StateOK}}}
	}
}

func b3Streams(site string) []streamView {
	return []streamView{{SiteID: site, NodeEpoch: 1, State: repl.StateOK, OK: true, Watermark: 3, Head: 3, Label: "ok"}}
}

// TestF6EachKeyVerdictNamesWhatActuallyFails is FINDINGS-R5 F6 (L04-N1,
// L01-2a, L04-F1's wrong diagnosis): one row per verdict shape.
func TestF6EachKeyVerdictNamesWhatActuallyFails(t *testing.T) {
	const pull401 = `"last_error":"repl: GET /sync/streams: 401 Unauthorized","error_kind":"key_rejected"`
	const pull403 = `"last_error":"repl: GET /sync/streams: 403 Forbidden","error_kind":"identity_rejected"`
	for _, c := range []struct {
		name, js     string
		want, forbid []string
	}{
		{
			// L04-1: this post holds the wrong key for the peer. Its pulls and
			// pokes are refused 401, but the PEER's pulls of this post use the
			// peer's key and succeed: its records reached all four in ≤ 6 s.
			name:   "pulls and pokes refused 401, the peer still reads this post",
			js:     `{"site_id":"site-b","last_contact_ago_seconds":40,` + pull401 + `,"poke_refused":true,"poke_refused_kind":"key_rejected"}`,
			want:   []string{"PEER REJECTS THIS POST'S KEY", "401", "nothing new from it arrives here", "own routine pull", "must match the peer's own peer_key"},
			forbid: []string{"403", "nothing recorded here reaches it", "does not list"},
		},
		{
			// L04-F1 / re-run A3: a poke answered 401 and no pull refused. It
			// was diagnosed "403 — it does not list this post as a peer".
			name:   "only a poke refused 401",
			js:     `{"site_id":"site-b","last_contact_ago_seconds":7,"poke_refused":true,"poke_refused_kind":"key_rejected"}`,
			want:   []string{"PEER REJECTS THIS POST'S KEY", "401", "pokes", "own routine pull", "must match the peer's own peer_key"},
			forbid: []string{"403", "does not list", "nothing recorded here reaches it", "nothing new from it arrives here"},
		},
		{
			name:   "only a poke refused 403: the peer does not list this post",
			js:     `{"site_id":"site-b","last_contact_ago_seconds":7,"poke_refused":true,"poke_refused_kind":"identity_rejected"}`,
			want:   []string{"403", "does not list this post", "nothing recorded here reaches it", "list this post in its [[peers]]"},
			forbid: []string{"401", "nothing new from it arrives here", "must match the peer's own peer_key"},
		},
		{
			name:   "pulls refused 403",
			js:     `{"site_id":"site-b","last_contact_ago_seconds":40,` + pull403 + `}`,
			want:   []string{"403", "does not list this post", "nothing new from it arrives here", "list this post in its [[peers]]"},
			forbid: []string{"401", "must match the peer's own peer_key"},
		},
		{
			name:   "pulls refused 401 AND the peer cannot read this post",
			js:     `{"site_id":"site-b","last_contact_ago_seconds":40,` + pull401 + `,"inbound_key_refused":true}`,
			want:   []string{"401", "nothing new from it arrives here", "nothing recorded here reaches it"},
			forbid: []string{"403"},
		},
		{
			name:   "this post refuses the peer's key",
			js:     `{"site_id":"site-b","last_contact_ago_seconds":4,"inbound_key_refused":true}`,
			want:   []string{"PEER CANNOT READ THIS POST", "this post is REFUSING its requests"},
			forbid: []string{"403", "401"},
		},
		{
			name:   "a one-way cut: the peer's copy of this post is stuck",
			js:     `{"site_id":"site-b","last_contact_ago_seconds":4,"own_copy_stuck":true,"own_copy_head":3}`,
			want:   []string{"PEER CANNOT READ THIS POST", "has not caught up"},
			forbid: []string{"REFUSING"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			pv := buildPeerView(b3Peer(t, c.js), b3Streams("site-b"), 10*time.Second, 10*time.Minute)
			if pv.Healthy {
				t.Fatalf("rendered healthy: %s", pv.Verdict)
			}
			for _, w := range c.want {
				if !strings.Contains(pv.Verdict, w) {
					t.Errorf("verdict lacks %q:\n%s", w, pv.Verdict)
				}
			}
			for _, f := range c.forbid {
				if strings.Contains(pv.Verdict, f) {
					t.Errorf("verdict says %q, which is not what fails here:\n%s", f, pv.Verdict)
				}
			}
		})
	}
}

// b3KeyFaultNotice is the status page's leading key-fault notice.
func b3KeyFaultNotice(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, "data-sync-key-fault")
	if i < 0 {
		t.Fatalf("no key-fault notice:\n%s", body)
	}
	sec := body[i:]
	return sec[:strings.Index(sec, "</div>")]
}

// TestF6TheHeadlineDoesNotBlameKeysForAOneWayCut is L01-8: a one-way cut
// read "not reaching those posts until the keys in the posts' config files
// agree" — the keys were right.
func TestF6TheHeadlineDoesNotBlameKeysForAOneWayCut(t *testing.T) {
	srv := newTestServer(t)
	srv.SyncSnapshot = b3Snap(t, srv.Cfg.SiteID, `{"site_id":"site-b","last_contact_ago_seconds":4,"own_copy_stuck":true,"own_copy_head":3}`)
	n := html.UnescapeString(b3KeyFaultNotice(t, getStatusPage(t, srv)))
	if strings.Contains(n, "keys") {
		t.Fatalf("a one-way cut is blamed on the keys:\n%s", n)
	}
	if !strings.Contains(n, "link") {
		t.Fatalf("the notice must point at the link:\n%s", n)
	}

	srv.SyncSnapshot = b3Snap(t, srv.Cfg.SiteID, `{"site_id":"site-b","last_contact_ago_seconds":4,"inbound_key_refused":true}`)
	if n := html.UnescapeString(b3KeyFaultNotice(t, getStatusPage(t, srv))); !strings.Contains(n, "keys") || !strings.Contains(n, "not reaching") {
		t.Fatalf("a refused key must still name the keys and say records are not reaching it:\n%s", n)
	}

	// L04-1's shape: this post's pulls are refused but the peer reads it. The
	// notice must not say its records are not reaching that post.
	srv.SyncSnapshot = b3Snap(t, srv.Cfg.SiteID, `{"site_id":"site-b","last_contact_ago_seconds":40,"last_error":"401","error_kind":"key_rejected","poke_refused":true,"poke_refused_kind":"key_rejected"}`)
	if n := html.UnescapeString(b3KeyFaultNotice(t, getStatusPage(t, srv))); strings.Contains(n, "not reaching") {
		t.Fatalf("records made here DO reach a peer that still pulls this post:\n%s", n)
	}
}

// TestF6TheOwnAnalyticsCardSaysNotReceivingOnlyWhenTheyAreNot is L04-N1's
// second half: "N peer(s) are NOT receiving them" while the peers pulled
// this post's records within 6 s.
func TestF6TheOwnAnalyticsCardSaysNotReceivingOnlyWhenTheyAreNot(t *testing.T) {
	w := anRoadWorld(t)
	w.srv.SyncSnapshot = b3Snap(t, "site-a", `{"site_id":"site-b","last_contact_ago_seconds":40,"last_error":"401","error_kind":"key_rejected","poke_refused":true,"poke_refused_kind":"key_rejected"}`)
	_, body := anSiteGet(t, w.srv, "GET", "/analytics", true)
	own := html.UnescapeString(anSiteCard(t, body, "site-a"))
	if strings.Contains(own, "NOT receiving") {
		t.Fatalf("own card says the peer is not receiving this post's records, but it still pulls them:\n%s", own)
	}
	if !strings.Contains(own, "refuse") {
		t.Fatalf("own card must still say a peer refuses this post's key:\n%s", own)
	}

	w.srv.SyncSnapshot = b3Snap(t, "site-a", `{"site_id":"site-b","last_contact_ago_seconds":4,"inbound_key_refused":true}`)
	_, body = anSiteGet(t, w.srv, "GET", "/analytics", true)
	if own := html.UnescapeString(anSiteCard(t, body, "site-a")); !strings.Contains(own, "NOT receiving") {
		t.Fatalf("a peer this post refuses is NOT receiving its records; the card must say so:\n%s", own)
	}
}

// TestF7AFailingPokeIsShownButDoesNotDegradeTheLink: a 503 poke while pulls
// work is reported on the status page in words — and the peer stays ok,
// because its records still arrive and it still pulls this post.
func TestF7AFailingPokeIsShownButDoesNotDegradeTheLink(t *testing.T) {
	srv := newTestServer(t)
	srv.SyncSnapshot = b3Snap(t, srv.Cfg.SiteID, `{"site_id":"site-b","last_contact_ago_seconds":4,"poke_error":"poke: repl: POST /sync/poke: 503 Service Unavailable: x","poke_error_kind":"bad_answer"}`)
	body := html.UnescapeString(getStatusPage(t, srv))
	sec := body[strings.Index(body, `<div id="sync-status">`):]
	if !strings.Contains(sec, `data-health="ok"`) {
		t.Fatalf("a failing poke turned a current peer degraded:\n%s", sec)
	}
	if !strings.Contains(sec, "pokes to this peer are failing") || !strings.Contains(sec, "own routine pull") {
		t.Fatalf("the failing poke is not on the page:\n%s", sec)
	}
}

// TestF9AnAheadStampClearsOnceThePeersClockIsRight is L03-8a: the card kept
// saying "the two posts' clocks disagree" after the peer's clock was put
// right, because it showed the MAX stamp, not the newest event. Now: the
// newest event the site recorded, and no claim about the clocks NOW.
func TestF9AnAheadStampClearsOnceThePeersClockIsRight(t *testing.T) {
	w := anRoadWorld(t)
	real := w.st.Clock
	ahead := time.Now().Add(30 * time.Minute).UnixMilli()
	w.st.Clock = func() store.ClockReading {
		return store.ClockReading{WallMS: ahead, MaxErrorUS: 500, State: store.ClockSynced}
	}
	if _, err := domain.CreatePerson(w.st, "site-b", 1, "Ahead, Ada", nil, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	w.st.Clock = real
	_, body := anSiteGet(t, w.srv, "GET", "/analytics", true)
	card := html.UnescapeString(anSiteCard(t, body, "site-b"))
	if !strings.Contains(card, "AHEAD of this post's clock") {
		t.Fatalf("fixture: the ahead-stamped event is not flagged:\n%s", card)
	}
	if strings.Contains(card, "clocks disagree") {
		t.Errorf("the card asserts a PRESENT disagreement it cannot know — the event was merely stamped ahead:\n%s", card)
	}

	// site-b's clock is put right and it records one more event.
	if _, err := domain.CreatePerson(w.st, "site-b", 1, "Right, Rae", nil, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	_, body = anSiteGet(t, w.srv, "GET", "/analytics", true)
	if card := html.UnescapeString(anSiteCard(t, body, "site-b")); strings.Contains(card, "AHEAD") {
		t.Fatalf("the skew cleared and the site recorded a newer event, yet the card still shows the old ahead stamp:\n%s", card)
	}
}

// TestF9ANewestEventStampedBehindAnEarlierOneShowsTheClockWentBack is
// L09-05: after a backward step the card showed the earlier, max-stamped
// event as "latest", hid the newest one, and said nothing about the skew.
func TestF9ANewestEventStampedBehindAnEarlierOneShowsTheClockWentBack(t *testing.T) {
	w := anRoadWorld(t)
	real := w.st.Clock
	if _, err := domain.CreatePerson(w.st, "site-b", 1, "Before, Bo", nil, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	back := time.Now().Add(-30 * time.Minute)
	w.st.Clock = func() store.ClockReading {
		return store.ClockReading{WallMS: back.UnixMilli(), MaxErrorUS: 500, State: store.ClockSynced}
	}
	if _, err := domain.CreatePerson(w.st, "site-b", 1, "After, Al", nil, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	w.st.Clock = real
	_, body := anSiteGet(t, w.srv, "GET", "/analytics", true)
	card := html.UnescapeString(anSiteCard(t, body, "site-b"))
	if !strings.Contains(card, back.In(time.Local).Format("2006-01-02 15:04")) {
		t.Fatalf("the latest event shown is not the newest one the site recorded (%s):\n%s", back.In(time.Local).Format("15:04"), card)
	}
	if !strings.Contains(card, "EARLIER than an event that post recorded before it") {
		t.Fatalf("a backward step is invisible on the card:\n%s", card)
	}
}

// TestF9SinceNamesTheClockItWasReadFrom is SKb (r4 L16, R5 L03-8): "ALSO ON
// SITE AT Ice Pad since 10:04" was another post's stamp, 30 min in this
// post's future, with no word whose clock it was.
func TestF9SinceNamesTheClockItWasReadFrom(t *testing.T) {
	w := newRoadWorkWeb(t, true)
	w.sendOut(t, nil)
	if _, err := domain.RecordUnsolicitedArrival(w.st, "site-b", 1, w7SeedVehicleID(t, w.st, "site-b", "GRADER 7"),
		"site-a", []string{w.crew[0].PersonID}, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	body := html.UnescapeString(w.get(t, "/muster", false))
	tag := regexp.MustCompile(`ALSO ON SITE AT Pad Site Control[^<]*`).FindString(body)
	if tag == "" {
		t.Fatalf("fixture: no elsewhere tag on the sheet:\n%s", body)
	}
	if !regexp.MustCompile(`since \d\d:\d\d \d\d/\d\d by Pad Site Control's clock`).MatchString(tag) {
		t.Fatalf("the since time does not name whose clock it was read from: %q", tag)
	}
}

// TestF3dAnalyticsSitePagesNameTheOriginByDisplayName is FINDINGS-R5 F3d
// (L11-RAW-1, L15 TEXT-2): "by ice-road's clock" on a run row.
func TestF3dAnalyticsSitePagesNameTheOriginByDisplayName(t *testing.T) {
	w := anRoadWorld(t)
	name := w.srv.Cfg.SiteName("site-a")
	if name == "site-a" {
		t.Fatal("fixture: site-a needs a display name that differs from its id")
	}
	for _, site := range []string{"site-a", "site-b"} {
		_, body := anSiteGet(t, w.srv, "GET", "/analytics/site/"+site, true)
		page := html.UnescapeString(styleRe.ReplaceAllString(body, ""))
		if strings.Contains(page, "site-a's clock") || strings.Contains(page, "only site-a") {
			t.Fatalf("/analytics/site/%s names a post by its raw id:\n%s", site, page)
		}
		if !strings.Contains(page, name+"'s clock") {
			t.Fatalf("/analytics/site/%s does not name the origin's clock by its display name %q:\n%s", site, name, page)
		}
	}
}
