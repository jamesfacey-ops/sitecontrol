package repl

// STREAM-DIVERGENCE witness (2026-09-14, scratch only — NOT shipped).
// Reproduces the live demo-pair reading: site-a's OWN row on
// /debug/sync-status froze at the boot-time Rejoin values while site-a kept
// authoring; the peer's row for the same stream tracks the truth.

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
)

func witnessRow(t *testing.T, snap Snapshot, site string, epoch int64) StreamStatus {
	t.Helper()
	for _, ss := range snap.Streams {
		if ss.SiteID == site && ss.NodeEpoch == epoch {
			return ss
		}
	}
	t.Fatalf("no row for (%s, %d) in %+v", site, epoch, snap.Streams)
	return StreamStatus{}
}

func TestWitnessOwnStreamRowOnSyncStatusTracksLocalHeadAfterAuthoring(t *testing.T) {
	ctx := context.Background()
	aLH, bLH := &lateHandler{}, &lateHandler{}
	aSrv, bSrv := httptest.NewServer(aLH), httptest.NewServer(bLH)
	defer aSrv.Close()
	defer bSrv.Close()
	aCfg := &config.Config{SiteID: "site-a", DisplayName: "site-a", ListenAddr: ":0", NodeEpoch: 1,
		PeerKey: "demo-key-site-a", SyncIntervalSeconds: 10, DBPath: filepath.Join(t.TempDir(), "a.db"),
		Peers: []config.Peer{{SiteID: "site-b", BaseURL: bSrv.URL, Key: "demo-key-site-b"}}}
	bCfg := &config.Config{SiteID: "site-b", DisplayName: "site-b", ListenAddr: ":0", NodeEpoch: 1,
		PeerKey: "demo-key-site-b", SyncIntervalSeconds: 10, DBPath: filepath.Join(t.TempDir(), "b.db"),
		Peers: []config.Peer{{SiteID: "site-a", BaseURL: aSrv.URL, Key: "demo-key-site-a"}}}
	aSt, bSt := r20DomainStore(t, aCfg.DBPath), r20DomainStore(t, bCfg.DBPath)
	aEng, bEng := New(aCfg, aSt), New(bCfg, bSt)
	aLH.set(aEng.SyncHandler())
	bLH.set(bEng.SyncHandler())

	// The live shape is a RESTART: the peer already holds the author's stream
	// when the author boots (the demo pair had a@14 at both posts before the
	// 14:45:25Z binary-swap restart). Seed one event, let the peer pull it,
	// then boot a NEW engine on the SAME store — main.go's boot, verbatim:
	// Rejoin (self-history first, then peers), before serving.
	aEng.Rejoin(ctx)
	bEng.Rejoin(ctx)
	if _, err := domain.CreatePerson(aSt, "site-a", 1, "Seed Person", nil, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	bEng.PullAll(ctx)
	aEng = New(aCfg, aSt) // site-a restarts (same DB, same epoch — a binary-only swap)
	aLH.set(aEng.SyncHandler())
	aEng.Rejoin(ctx)
	bootHead := mustWatermark(t, aSt, "site-a", 1)
	if bootHead != 1 {
		t.Fatalf("fixture: site-a head at boot %d, want 1", bootHead)
	}
	if r := witnessRow(t, aEng.Snapshot(), "site-a", 1); r.Watermark != 1 || r.Head != 1 || r.State != StateOK {
		t.Fatalf("fixture: own row at boot %+v, want 1/1 ok (the peer announced head 1)", r)
	}

	// site-a authors AFTER boot (the demo pair's live test: a person_created).
	if _, err := domain.CreatePerson(aSt, "site-a", 1, "Zzz Witness", nil, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	bEng.PullAll(ctx) // the routine 10 s path at the peer

	localHead := mustWatermark(t, aSt, "site-a", 1)
	if localHead != bootHead+1 {
		t.Fatalf("fixture: site-a head %d, want %d", localHead, bootHead+1)
	}
	peerRow := witnessRow(t, bEng.Snapshot(), "site-a", 1)
	if peerRow.Watermark != localHead || peerRow.Head != localHead {
		t.Fatalf("fixture: the PEER's row for (site-a,1) is wm=%d head=%d, want %d/%d", peerRow.Watermark, peerRow.Head, localHead, localHead)
	}
	ownRow := witnessRow(t, aEng.Snapshot(), "site-a", 1)
	if ownRow.Watermark != localHead || ownRow.Head != localHead {
		t.Fatalf("STREAM-DIVERGENCE reproduced: site-a's OWN /debug/sync-status row reads wm=%d head=%d state=%q while its store holds %d and its peer reports %d/%d — the own row is the boot-time Rejoin snapshot, never refreshed by authoring",
			ownRow.Watermark, ownRow.Head, ownRow.State, localHead, peerRow.Watermark, peerRow.Head)
	}
}
