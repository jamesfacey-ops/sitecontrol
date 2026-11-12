package web

import (
	"html"
	"strings"
	"testing"
	"time"
)

// TestFix4HKeyNoticePointsAtThePeersOnce — R6-2.2a (fix4 H). With a peer that
// refuses this post's key but still pulls it, the notice read "… each peer
// below says what that stops. See each peer below." FAIL BEFORE.
func TestFix4HKeyNoticePointsAtThePeersOnce(t *testing.T) {
	srv := newTestServer(t)
	for _, js := range []string{
		`{"site_id":"site-b","last_contact_ago_seconds":40,"last_error":"401","error_kind":"key_rejected","poke_refused":true,"poke_refused_kind":"key_rejected"}`,
		`{"site_id":"site-b","last_contact_ago_seconds":4,"inbound_key_refused":true}`,
		`{"site_id":"site-b","last_contact_ago_seconds":4,"own_copy_stuck":true,"own_copy_head":3}`,
	} {
		srv.SyncSnapshot = b3Snap(t, srv.Cfg.SiteID, js)
		n := strings.ToLower(html.UnescapeString(b3KeyFaultNotice(t, getStatusPage(t, srv))))
		if c := strings.Count(n, "each peer below"); c != 1 {
			t.Errorf("the key notice points at the peers %d times (want once):\n%s", c, n)
		}
	}
}

// TestFix4HADroppedPokeIsNotWordedAsNothingAnswered — R6-2.3 (fix4 H). A
// poke whose connection was accepted and then closed (EOF) read "nothing
// answered at its address"; something did answer. A refused connection keeps
// the old words. FAIL BEFORE.
func TestFix4HADroppedPokeIsNotWordedAsNothingAnswered(t *testing.T) {
	srv := newTestServer(t)
	for _, c := range []struct {
		err, want, forbid string
	}{
		{`poke: Post \"http://127.0.0.1:21032/sync/poke?t1=1\": EOF`, "dropped", "nothing answered"},
		{`poke: Post \"http://127.0.0.1:21032/sync/poke?t1=1\": read tcp 127.0.0.1:1-\u003e127.0.0.1:2: read: connection reset by peer`, "dropped", "nothing answered"},
		{`poke: Post \"http://127.0.0.1:9/sync/poke?t1=1\": dial tcp 127.0.0.1:9: connect: connection refused`, "nothing answered", "dropped"},
	} {
		srv.SyncSnapshot = b3Snap(t, srv.Cfg.SiteID, `{"site_id":"site-b","last_contact_ago_seconds":4,"poke_error":"`+c.err+`","poke_error_kind":"unreachable"}`)
		body := html.UnescapeString(getStatusPage(t, srv))
		sec := body[strings.Index(body, `<div id="sync-status">`):]
		if !strings.Contains(sec, "pokes to this peer are failing") {
			t.Fatalf("the failing poke is not on the page:\n%s", sec)
		}
		if !strings.Contains(sec, c.want) || strings.Contains(sec, c.forbid) {
			t.Errorf("poke error %q: page should say %q and not %q:\n%s", c.err, c.want, c.forbid, sec)
		}
		// The pull side words a dropped exchange the same way.
		pv := buildPeerView(b3Peer(t, `{"site_id":"site-b","last_contact_ago_seconds":900,"last_error":"`+c.err+`","error_kind":"unreachable"}`), b3Streams("site-b"), 10*time.Second, 10*time.Minute)
		if !strings.Contains(pv.Verdict, c.want) || strings.Contains(pv.Verdict, c.forbid) {
			t.Errorf("pull error %q: verdict should say %q and not %q:\n%s", c.err, c.want, c.forbid, pv.Verdict)
		}
	}
}
