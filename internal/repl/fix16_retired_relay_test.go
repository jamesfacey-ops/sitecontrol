package repl

// FIX16 H (James, 2026-10-06: "every site has the full database"): a
// RETIRED post's stream — a site in [[former_peers]] — is served by every
// live post from the copy it holds, so a laptop rebuilt blank after the
// retirement gets that history back. Live origins are untouched: their
// records still come only from their author.

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"sitecontrol/internal/config"
)

// retireFrom rebuilds a mesh site's engine with `gone` moved from its peers
// to its former peers — what `sitectl admin peer remove` does to the file.
func retireFrom(t *testing.T, s *meshSite, gone string) {
	t.Helper()
	nc := *s.cfg
	nc.Peers = nil
	for _, p := range s.cfg.Peers {
		if p.SiteID != gone {
			nc.Peers = append(nc.Peers, p)
		}
	}
	nc.FormerPeers = append(nc.FormerPeers, config.FormerPeer{SiteID: gone, DisplayName: gone})
	s.cfg = &nc
	s.eng = New(&nc, s.st)
	s.lh.set(s.eng.SyncHandler())
}

func authorN(t *testing.T, s *meshSite, n int) {
	t.Helper()
	for j := 0; j < n; j++ {
		if _, err := s.st.Author(s.cfg.SiteID, 1, tick(s.cfg.SiteID), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFix16ABlankRebuildGetsARetiredPostsRecordsFromALivePost(t *testing.T) {
	ctx := context.Background()
	sites := newMesh(t, 4) // a, b, c (to retire), d (holds only part of c)
	a, b, c, d := sites[0], sites[1], sites[2], sites[3]
	authorN(t, a, 3)
	authorN(t, b, 2)
	authorN(t, c, 4)
	for _, s := range sites {
		s.eng.PullAll(ctx)
	}
	authorN(t, c, 3) // d never pulls these: its copy of c lags at 4
	a.eng.PullAll(ctx)
	b.eng.PullAll(ctx)
	if mustWatermark(t, b.st, "site-c", 1) != 7 || mustWatermark(t, d.st, "site-c", 1) != 4 {
		t.Fatal("fixture: b must hold all 7 of c, d only 4")
	}
	// c retires; c's laptop is gone.
	c.srv.Close()
	for _, s := range []*meshSite{a, b, d} {
		retireFrom(t, s, "site-c")
	}

	// a's laptop dies and is rebuilt blank at epoch 2.
	nc := *a.cfg
	nc.NodeEpoch = 2
	nc.DBPath = t.TempDir() + "/a-new.db"
	st := openStore(t, nc.DBPath)
	eng := New(&nc, st)
	a.lh.set(eng.SyncHandler())
	eng.Rejoin(ctx)
	eng.PullAll(ctx) // a second cycle: both b's full copy and d's lagging copy have been asked

	if got := mustWatermark(t, st, "site-c", 1); got != 7 {
		t.Fatalf("retired c's records on the rebuilt laptop: %d of 7", got)
	}
	if got := mustWatermark(t, st, "site-a", 1); got != 3 {
		t.Fatalf("own pre-wipe history: %d of 3", got)
	}
	if got := mustWatermark(t, st, "site-b", 1); got != 2 {
		t.Fatalf("live b's records: %d of 2", got)
	}
	// A lagging copy (d: 4) beside a full one (b: 7) is lag between copies,
	// never a regression of the retired post's log.
	if s, ok := streamState(eng, "site-c", 1); !ok || s.State != StateOK {
		t.Fatalf("retired stream state = %+v, want ok", s)
	}
	if !bytes.Equal(mustDump(t, st), mustDump(t, b.st)) {
		t.Fatal("the rebuilt laptop's projections differ from a live post's")
	}
}

func TestFix16OnlyARetiredStreamIsRelayedNeverALiveOne(t *testing.T) {
	ctx := context.Background()
	sites := newMesh(t, 3)
	a, b, c := sites[0], sites[1], sites[2]
	authorN(t, b, 2)
	authorN(t, c, 2)
	for _, s := range sites {
		s.eng.PullAll(ctx)
	}
	retireFrom(t, a, "site-c")
	ask := func(path string) int {
		req, _ := http.NewRequest(http.MethodGet, a.srv.URL+path, nil)
		req.Header.Set("Authorization", "Bearer demo-key-site-a")
		req.Header.Set(HeaderFrom, "site-b")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := ask("/sync/streams?site=site-c"); code != http.StatusOK {
		t.Fatalf("retired c's streams from a: %d, want 200", code)
	}
	if code := ask("/sync/pull?site=site-c&epoch=1&from=1"); code != http.StatusOK {
		t.Fatalf("retired c's records from a: %d, want 200", code)
	}
	// A LIVE peer's stream is still served by its author alone — b asking
	// a for b's own stream is the self-history exception, so ask as a
	// third identity.
	req, _ := http.NewRequest(http.MethodGet, a.srv.URL+"/sync/streams?site=site-b", nil)
	req.Header.Set("Authorization", "Bearer demo-key-site-a")
	req.Header.Set(HeaderFrom, "site-z")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("live b's stream relayed by a: %d, want 403", resp.StatusCode)
	}
	if code := ask("/sync/streams?site=site-q"); code != http.StatusForbidden {
		t.Fatalf("an id in nobody's former_peers: %d, want 403", code)
	}
	// And on the pulling side a record claiming a retired site is refused
	// unless THIS post lists that site as retired: b (not yet edited) pulls
	// from a and must not take c's records through a.
	before := b.eng.Snapshot().Applied
	b.eng.PullAll(ctx)
	if got := b.eng.Snapshot().Applied; got != before {
		t.Fatalf("b applied %d record(s) relayed for a site it does not list as retired", got-before)
	}
}
