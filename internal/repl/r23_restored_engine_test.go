// r23_restored_engine_test.go — R-23 through the REAL rejoin path: a peer
// serves forged self-history (CLEAN-NEGATIVES-RECHECK.md plant C2b) to a
// re-imaged post's boot Rejoin; the stranger's visit lands (the peer's copy
// is our only backup — recovery is not refused) and is classified RESTORED
// by domain.Visit.RestoredFromPeer under this post's identity, while a
// visit this post authors itself on the running epoch is not.
package repl

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

func TestR23ForgedSelfHistoryLandsAStrangersVisitClassifiedRestoredNeverObserved(t *testing.T) {
	ctx := context.Background()

	// The rig: an authenticated peer that, asked for site-b's own history,
	// serves a completion-with-visit for a person site-b never enrolled,
	// under site-b's tag at epoch 1 — exactly C2b.
	name := "Diaz, Forged"
	payload, err := json.Marshal(domain.OrientationCompleted{
		Type: "orientation_completed", PersonID: "p-stranger",
		Visit: &domain.ArrivalVisit{VisitID: "site-b-visit-forged000000001", PersonID: "p-stranger", PersonName: &name},
	})
	if err != nil {
		t.Fatal(err)
	}
	forged := store.Envelope{SiteID: "site-b", NodeEpoch: 1, Seq: 1, SchemaVersion: store.SchemaVersion, OriginAuth: "site-b", Payload: payload}
	r := &rig{siteID: "site-x"}
	r.setStreams(StreamHead{NodeEpoch: 1, Head: 1})
	r.setEvents(1, []store.Envelope{forged})
	rs := httptest.NewServer(r.handler())
	defer rs.Close()

	cfg := &config.Config{
		SiteID: "site-b", ListenAddr: ":0", NodeEpoch: 2, PeerKey: "demo-key-site-b",
		SyncIntervalSeconds: 10, DBPath: filepath.Join(t.TempDir(), "site-b.db"),
		Peers: []config.Peer{{SiteID: "site-x", BaseURL: rs.URL, Key: "k"}},
	}
	st := r20DomainStore(t, cfg.DBPath) // domain folds registered: the visit must actually materialise
	eng := New(cfg, st)
	eng.Rejoin(ctx) // main.go:188 — every boot

	if wm := mustWatermark(t, st, "site-b", 1); wm != 1 {
		t.Fatalf("the self-history page did not land (wm=%d) — the fixture no longer models C2b", wm)
	}
	open, err := domain.OpenVisitsAt(st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].PersonID != "p-stranger" {
		t.Fatalf("C2b: expected the stranger's ONE open visit at site-b (recovery is not refused), got %+v", open)
	}
	if !open[0].RestoredFromPeer(cfg.SiteID, cfg.NodeEpoch) {
		t.Errorf("R-23: the restored visit %s (epoch %d, running epoch %d) is classified as OBSERVED at this gate — second-hand presence presented as first-hand", open[0].VisitID, open[0].NodeEpoch, cfg.NodeEpoch)
	}
	// Scoped to OUR tag: another post reading this same row is not "restored"
	// in this sense — every copy of another site's presence is second-hand
	// by nature and is never shown on that post's own muster.
	if open[0].RestoredFromPeer("site-x", 99) {
		t.Errorf("the classification leaked past our own site tag")
	}

	// CONTROL: what this post observes itself, on the running epoch.
	p, err := domain.CreatePerson(st, "site-b", 2, "Local, Lee", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	num := "TRK-118"
	veh, err := domain.CreateVehicle(st, "site-b", 2, &num, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := domain.GateCheckIn(st, "site-b", 2, domain.GateCheckInInput{
		VehicleID: veh.VehicleID, Driver: domain.PersonSlot{ExistingID: p.PersonID}}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	open, err = domain.OpenVisitsAt(st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	restored, observed := 0, 0
	for _, v := range open {
		if v.RestoredFromPeer(cfg.SiteID, cfg.NodeEpoch) {
			restored++
		} else {
			observed++
		}
	}
	if restored != 1 || observed != 1 {
		t.Errorf("after a first-hand check-in: restored=%d observed=%d, want 1 and 1 — the running epoch's own rows are first-hand", restored, observed)
	}
	t.Logf("C2b through the real rejoin path: stranger's visit landed (recovery works) and reads RESTORED; the post's own epoch-2 check-in reads observed")
}
