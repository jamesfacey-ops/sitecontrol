// r20_engine_rejoin_test.go — R-20 through the REAL engine: the scenario
// domain's TestR20LateOwnEpochAfterAuthoringLeavesTheAuthorAgreeingWithACanonicalReplay
// poses by hand, produced here by repl.Engine itself — Rejoin fails at
// boot, the post authors, pullPeer's armed self-history retry then drains
// the post's own epoch-1 stream over real HTTP — with domain's real folds
// registered. Ported from the F-3 audit's witness
// (reports/witness/D87-D92-COHERENCE-witness_engine_test.go.txt), which
// measured the DIVERGENCE (author 0, peer 1) on the unchanged tree; this
// shipped copy asserts the agreement visit_open.go's derivation restores.
package repl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

func r20DomainStore(t *testing.T, path string) *store.Store {
	t.Helper()
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	return st
}

func r20OpenFor(t *testing.T, st *store.Store, site, personID string) int {
	t.Helper()
	open, err := domain.OpenVisitsAt(st, site)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, v := range open {
		if v.PersonID == personID {
			n++
		}
	}
	return n
}

func TestR20EngineLateSelfHistoryAfterAuthoringAgreesWithThePeer(t *testing.T) {
	ctx := context.Background()

	// Two real posts, full mesh, domain folds registered.
	aLH, bLH := &lateHandler{}, &lateHandler{}
	aSrv, bSrv := httptest.NewServer(aLH), httptest.NewServer(bLH)
	defer aSrv.Close()
	defer bSrv.Close()
	aCfg := &config.Config{SiteID: "site-a", DisplayName: "site-a", ListenAddr: ":0", NodeEpoch: 1,
		PeerKey: "demo-key-site-a", SyncIntervalSeconds: 10, DBPath: filepath.Join(t.TempDir(), "a.db"),
		Peers: []config.Peer{{SiteID: "site-b", BaseURL: bSrv.URL, Key: "demo-key-site-b"}}}
	b1Cfg := &config.Config{SiteID: "site-b", DisplayName: "site-b", ListenAddr: ":0", NodeEpoch: 1,
		PeerKey: "demo-key-site-b", SyncIntervalSeconds: 10, DBPath: filepath.Join(t.TempDir(), "b1.db"),
		Peers: []config.Peer{{SiteID: "site-a", BaseURL: aSrv.URL, Key: "demo-key-site-a"}}}
	aSt, b1St := r20DomainStore(t, aCfg.DBPath), r20DomainStore(t, b1Cfg.DBPath)
	aEng, b1Eng := New(aCfg, aSt), New(b1Cfg, b1St)
	aLH.set(aEng.SyncHandler())
	bLH.set(b1Eng.SyncHandler())

	// site-a: roster + vehicle. site-b (epoch 1): Pat checks in, then out.
	p, err := domain.CreatePerson(aSt, "site-a", 1, "Pat Reorder", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	num := "T12"
	veh, err := domain.CreateVehicle(aSt, "site-a", 1, &num, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	b1Eng.PullAll(ctx) // roster reaches B
	if _, _, err := domain.GateCheckIn(b1St, "site-b", 1, domain.GateCheckInInput{
		VehicleID: veh.VehicleID, Driver: domain.PersonSlot{ExistingID: p.PersonID}}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	open, err := domain.OpenVisitsAt(b1St, "site-b")
	if err != nil || len(open) != 1 {
		t.Fatalf("fixture: %v %d", err, len(open))
	}
	if err := domain.CheckOutVisit(b1St, "site-b", 1, open[0].VisitID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	aEng.PullAll(ctx) // A holds B's epoch 1 (head 3)
	if wm := mustWatermark(t, aSt, "site-b", 1); wm != 3 {
		t.Fatalf("fixture: A holds B epoch-1 watermark %d, want 3", wm)
	}

	// B is wiped and re-imaged (node_epoch 2). The link to A is DOWN at boot.
	aLH.set(nil)
	b2Cfg := &config.Config{SiteID: "site-b", DisplayName: "site-b", ListenAddr: ":0", NodeEpoch: 2,
		PeerKey: "demo-key-site-b", SyncIntervalSeconds: 10, DBPath: filepath.Join(t.TempDir(), "b2.db"),
		Peers: []config.Peer{{SiteID: "site-a", BaseURL: aSrv.URL, Key: "demo-key-site-a"}}}
	b2St := r20DomainStore(t, b2Cfg.DBPath)
	b2Eng := New(b2Cfg, b2St)
	bLH.set(b2Eng.SyncHandler())
	b2Eng.Rejoin(ctx) // fails non-fatally; the binary serves anyway (main.go:188)

	// The partition heals selectively: A's own log is deliverable (the
	// roster reaches B) but every exchange about site-b's history still
	// fails — a filter that 503s only queries naming site-b. Pat walks up
	// and is checked in on epoch 2 while B's own epoch-1 history is still
	// unrecovered.
	aLH.set(r20Blackout{inner: aEng.SyncHandler(), site: "site-b"})
	b2Eng.PullAll(ctx)
	if wm := mustWatermark(t, b2St, "site-a", 1); wm == 0 {
		t.Fatal("fixture: roster did not reach the re-imaged B")
	}
	if wm := mustWatermark(t, b2St, "site-b", 1); wm != 0 {
		t.Fatalf("fixture: B's own epoch-1 history arrived early (wm=%d); the blackout is not modelling the partition", wm)
	}
	if _, _, err := domain.GateCheckIn(b2St, "site-b", 2, domain.GateCheckInInput{
		VehicleID: veh.VehicleID, Driver: domain.PersonSlot{ExistingID: p.PersonID}}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if n := r20OpenFor(t, b2St, "site-b", p.PersonID); n != 1 {
		t.Fatalf("fixture: epoch-2 check-in left %d open visits", n)
	}

	// The self-history path heals: routine cycles now recover B's epoch 1
	// from A — AFTER B authored epoch 2. And A pulls B's epoch 2 epoch-major.
	aLH.set(aEng.SyncHandler())
	b2Eng.PullAll(ctx)
	b2Eng.PullAll(ctx)
	aEng.PullAll(ctx)
	if wm := mustWatermark(t, b2St, "site-b", 1); wm != 3 {
		t.Fatalf("own history not recovered: wm=%d", wm)
	}
	if wm := mustWatermark(t, aSt, "site-b", 2); wm != 2 {
		t.Fatalf("A did not receive B's epoch 2: wm=%d", wm)
	}
	for _, ss := range b2Eng.Snapshot().Streams {
		t.Logf("B stream (%s,%d): state=%s wm=%d", ss.SiteID, ss.NodeEpoch, ss.State, ss.Watermark)
	}

	atB, atA := r20OpenFor(t, b2St, "site-b", p.PersonID), r20OpenFor(t, aSt, "site-b", p.PersonID)
	t.Logf("Pat's open visits at site-b — as held by the AUTHOR (site-b): %d · as held by PEER site-a: %d", atB, atA)
	if atB != atA {
		t.Errorf("DIVERGENCE through the real engine: both posts hold identical raw logs for site-b "+
			"(epoch1 wm=3, epoch2 wm=2) yet the author shows %d open visit(s) and the peer shows %d", atB, atA)
	}
	if atB != 1 {
		t.Errorf("R-20: Pat physically checked in on epoch 2 and the AUTHOR shows %d open visit(s) at site-b, want 1 — "+
			"the post's own late epoch-1 history must not close a visit minted after it", atB)
	}
}

// r20Blackout serves everything except queries about `site` (both
// /sync/streams and /sync/pull carry it) — a partition that reaches the
// peer's own log but not our history.
type r20Blackout struct {
	inner http.Handler
	site  string
}

func (b r20Blackout) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("site") == b.site {
		http.Error(w, "partitioned", http.StatusServiceUnavailable)
		return
	}
	b.inner.ServeHTTP(w, r)
}
