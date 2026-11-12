// fix2_fk_sync_selfstatus_test.go — FIX2 cluster K (sync self-status), the
// rendered half, driven through REAL engines over REAL listeners (public APIs
// only, like syncstatus_integration_test.go). Every test here compiles
// against 2ba9188 and was confirmed to FAIL there and PASS after:
//   - D26 (sc-test L12-Y31b; ruling R11, James 2026-10-04): a post whose
//     peers cannot read it rendered "ok" for every peer and "N peer(s)
//     healthy" on its chip;
//   - R11's literal case: a post whose own pulls are refused 401 rendered
//     only "the most recent sync attempt FAILED: <raw error>";
//   - L01 F-5: the site page's STALE verdict printed the raw sync error with
//     the peer's base URL and the t1 timestamp parameter;
//   - L12-Y05a / L16-P1b: a post restarted with its peer down showed "no
//     stream state observed yet" — not even its own row.
package web

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/repl"
	"sitecontrol/internal/store"
)

type fkSite struct {
	cfg *config.Config
	st  *store.Store
	eng *repl.Engine
	srv *httptest.Server
	web *Server
}

// fkPair builds A and B, each serving its own log; A's key for B and B's key
// for A are the ones each side's peer_key says unless overridden.
func fkPair(t *testing.T, aKeyForB, bKeyForA string) (a, b *fkSite) {
	t.Helper()
	mk := func(id string) *fkSite {
		st, err := store.Open(filepath.Join(t.TempDir(), id+".db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { st.Close() })
		if err := domain.Register(st); err != nil {
			t.Fatal(err)
		}
		return &fkSite{st: st, cfg: &config.Config{SiteID: id, DisplayName: id, ListenAddr: ":0",
			NodeEpoch: 1, PeerKey: "key-" + id, SyncIntervalSeconds: 3600, DBPath: st.Path}}
	}
	a, b = mk("site-a"), mk("site-b")
	lhA, lhB := &fkLate{}, &fkLate{}
	a.srv, b.srv = httptest.NewServer(lhA), httptest.NewServer(lhB)
	t.Cleanup(a.srv.Close)
	t.Cleanup(b.srv.Close)
	a.cfg.Peers = []config.Peer{{SiteID: "site-b", BaseURL: b.srv.URL, Key: aKeyForB}}
	b.cfg.Peers = []config.Peer{{SiteID: "site-a", BaseURL: a.srv.URL, Key: bKeyForA}}
	for _, s := range []*fkSite{a, b} {
		s.eng = repl.New(s.cfg, s.st)
		t.Cleanup(s.eng.Shutdown)
		s.web = New(s.cfg, s.st)
		s.web.SyncSnapshot = s.eng.Snapshot
	}
	lhA.h, lhB.h = a.eng.SyncHandler(), b.eng.SyncHandler()
	return a, b
}

// fkLate lets each listener exist before its engine (the peers' URLs are
// config); set once before any request.
type fkLate struct{ h http.Handler }

func (l *fkLate) ServeHTTP(w http.ResponseWriter, r *http.Request) { l.h.ServeHTTP(w, r) }

func fkSyncSection(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, `<div id="sync-status">`)
	if i < 0 {
		t.Fatalf("no sync section:\n%s", body)
	}
	return body[i:]
}

// D26: A's own peer_key changed; B (holding the old key) is refused when it
// pulls A. Every call A makes succeeds. A's page must not say healthy.
func TestD26APostWhosePeersCannotReadItIsNotRenderedHealthy(t *testing.T) {
	ctx := context.Background()
	a, b := fkPair(t, "key-site-b", "key-site-a")
	if _, err := a.eng.Author(stampTick("a-1"), 0, "default"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.eng.Author(stampTick("b-1"), 0, "default"); err != nil {
		t.Fatal(err)
	}
	a.cfg.PeerKey = "key-site-a-CHANGED"
	if err := b.eng.PullPeer(ctx, "site-a"); err == nil {
		t.Fatal("fixture: B's pull of A succeeded with the old key")
	}
	if err := a.eng.PullPeer(ctx, "site-b"); err != nil {
		t.Fatalf("fixture: A's own pull of B must still succeed: %v", err)
	}
	body := html.UnescapeString(getStatusPage(t, a.web))
	sec := fkSyncSection(t, body)
	if strings.Contains(sec, `data-health="ok"`) {
		t.Fatalf("A renders B healthy while B cannot read A (L12-Y31b):\n%s", sec)
	}
	if strings.Contains(body, "peer(s) healthy") {
		t.Fatalf("A's chip says healthy while its records reach nobody:\n%s", body)
	}
	for _, want := range []string{"PEER CANNOT READ THIS POST", "peers cannot read this post's records"} {
		if !strings.Contains(body, want) {
			t.Fatalf("A's status page lacks %q:\n%s", want, sec)
		}
	}
}

// R11, literally: A's pulls of B are refused 401 (A holds the wrong key for
// B). The page and chip must say "peers reject this post's key".
func TestR11APostWhosePullsAreRefusedSaysPeersRejectItsKey(t *testing.T) {
	ctx := context.Background()
	a, _ := fkPair(t, "key-site-b-WRONG", "key-site-a")
	if err := a.eng.PullPeer(ctx, "site-b"); err == nil {
		t.Fatal("fixture: A's pull with the wrong key succeeded")
	}
	body := html.UnescapeString(getStatusPage(t, a.web))
	for _, want := range []string{"PEER REJECTS THIS POST'S KEY", "peers reject this post's key"} {
		if !strings.Contains(body, want) {
			t.Fatalf("A's status page lacks %q:\n%s", want, fkSyncSection(t, body))
		}
	}
	if strings.Contains(fkSyncSection(t, body), `data-health="ok"`) {
		t.Fatal("a peer refusing A's key is rendered healthy")
	}
}

// L01 F-5: the site page's verdict is plain words — no base URL, no t1.
func TestF5TheSitePageStaleVerdictPrintsNoRawSyncError(t *testing.T) {
	st, setClock := anStore(t)
	setClock(anLocal(12, 0))
	srv := anServer(st)
	// A real site-a that answers once, then goes away: the LastError
	// verdict ("last heard N ago, but … FAILED"), the one F-5 measured.
	stA, err := store.Open(filepath.Join(t.TempDir(), "site-a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stA.Close() })
	if err := domain.Register(stA); err != nil {
		t.Fatal(err)
	}
	engA := repl.New(&config.Config{SiteID: "site-a", DisplayName: "site-a", ListenAddr: ":0",
		NodeEpoch: 1, PeerKey: "k", SyncIntervalSeconds: 10, DBPath: stA.Path}, stA)
	peerA := httptest.NewServer(engA.SyncHandler())
	defer peerA.Close()
	cfgE := *srv.Cfg
	cfgE.Peers = []config.Peer{{SiteID: "site-a", BaseURL: peerA.URL, Key: "k"}}
	eng := repl.New(&cfgE, st)
	t.Cleanup(eng.Shutdown)
	if err := eng.PullPeer(context.Background(), "site-a"); err != nil {
		t.Fatalf("fixture: first pull: %v", err)
	}
	peerA.Close()
	if err := eng.PullPeer(context.Background(), "site-a"); err == nil {
		t.Fatal("fixture: pull of a closed listener succeeded")
	}
	srv.SyncSnapshot = eng.Snapshot
	_, body := anGet(t, srv, true)
	card := html.UnescapeString(anSiteCard(t, body, "site-a"))
	if !strings.Contains(card, "STALE") {
		t.Fatalf("fixture: the dead peer's card is not STALE:\n%s", card)
	}
	for _, raw := range []string{"127.0.0.1", "t1=", "repl:", "dial tcp"} {
		if strings.Contains(card, raw) {
			t.Fatalf("the site card prints the raw sync error (%q):\n%s", raw, card)
		}
	}
	if !strings.Contains(card, "nothing answered") {
		t.Fatalf("the card does not say in words what failed:\n%s", card)
	}
}

// L12-Y05a: A holds its own and B's records, restarts while B is down.
func TestY05aARestartedPostWithItsPeerDownStillShowsBothRows(t *testing.T) {
	ctx := context.Background()
	a, b := fkPair(t, "key-site-b", "key-site-a")
	if _, err := a.eng.Author(stampTick("a-1"), 0, "default"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.eng.Author(stampTick("b-1"), 0, "default"); err != nil {
		t.Fatal(err)
	}
	if err := a.eng.PullPeer(ctx, "site-b"); err != nil {
		t.Fatal(err)
	}
	b.srv.Close()
	restarted := repl.New(a.cfg, a.st)
	t.Cleanup(restarted.Shutdown)
	restarted.Rejoin(ctx)
	a.web.SyncSnapshot = restarted.Snapshot
	sec := fkSyncSection(t, html.UnescapeString(getStatusPage(t, a.web)))
	if strings.Contains(sec, "no stream state observed yet") {
		t.Fatalf("the restarted post's stream table is empty:\n%s", sec)
	}
	if !strings.Contains(sec, "(own — local watermark") || !strings.Contains(sec, "NOT YET REPORTED") {
		t.Fatalf("the own row or the down peer's row is missing:\n%s", sec)
	}
	if strings.Contains(sec, "never contacted") || !strings.Contains(sec, "not heard from since this post started") {
		t.Fatalf("after a restart the verdict must say 'since this post started', not 'never':\n%s", sec)
	}
}
