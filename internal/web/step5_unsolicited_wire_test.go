// step5_unsolicited_wire_test.go — SCHEMA-11 step 5's REACHABILITY witness
// for the SECOND arrival door (D-98/D-91, tester-go follow-up to
// STEP5-WIRING-TEST.md/step5_wire_test.go, which proved only the first
// door). handleUnsolicitedArrival (trips.go:1220) was rewired onto
// domain.RecordUnsolicitedArrivalOriented and now passes
// s.Cfg.HasOrientation() (trips.go:1261), but nothing drove
// POST /trips/arrive-unsolicited at an orientation=true site to prove that
// argument actually reaches the choke point — the exact "edited but
// invisible" shape flagged for this door: a hardcoded false at trips.go:1261
// reproduces the clean-tree baseline byte-for-byte (528 pass / 3
// pre-existing fails, same names), so no existing test would notice it
// regressing. This file drives the REAL HTTP door, exactly as the operator
// console's unsolicited-arrival form does, and asserts the split actually
// lands in trip_arrival_not_entered.
package web

import (
	"database/sql"
	"net/http"
	"net/url"
	"path/filepath"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// step5UnsolicitedWireWorld is a site-b pad, orientation=true, with two
// people on the roster who arrive on a vehicle this site has never
// dispatched or heard of (the unsolicited door's own case — the vehicle
// outran the sync, or never synced at all). Adams holds B's orientation
// before the vehicle shows up; Baker does not.
type step5UnsolicitedWireWorld struct {
	srv   *Server
	st    *store.Store
	adams domain.Person
	baker domain.Person
}

func newStep5UnsolicitedWireWorld(t *testing.T) *step5UnsolicitedWireWorld {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "step5uns.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	o := true
	cfg := &config.Config{
		SiteID: "site-b", DisplayName: "Pad Site Control", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 3600,
		LoginPassword: testLoginPassword,
		Peers:         []config.Peer{{SiteID: "site-a", BaseURL: "http://example.invalid"}},
		Destinations:  []config.Destination{{ID: "site-a", DisplayName: "Ice Road", Controlled: true}},
		Orientation:   &o,
	}
	adams, err := domain.CreatePerson(st, "site-a", 1, "Adams", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	baker, err := domain.CreatePerson(st, "site-a", 1, "Baker", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// Adams already holds site-b's orientation; Baker never has. Recorded
	// directly through the domain door (not the point under test — the
	// marking surface is step 4's, already wired, orientation_test.go).
	if _, err := domain.CompleteOrientation(st, "site-b", 1, adams.PersonID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	return &step5UnsolicitedWireWorld{srv: New(cfg, st), st: st, adams: adams, baker: baker}
}

// step5UnsolicitedArrivalID reads the fold's own table directly (D-56's
// convention, w9c_seams_test.go), keyed the same way the unsolicited path's
// own partial index is (trip_ref IS NULL, origin_site) — there is no
// trip_ref to key on here, that is exactly what makes this door unsolicited.
func step5UnsolicitedArrivalID(t *testing.T, st *store.Store, originSite string) string {
	t.Helper()
	db, err := sql.Open("sqlite", st.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var id string
	if err := db.QueryRow(`SELECT arrival_id FROM trip_arrival WHERE trip_ref IS NULL AND origin_site = ?`, originSite).Scan(&id); err != nil {
		t.Fatalf("reading the unsolicited trip_arrival row for origin %s: %v", originSite, err)
	}
	return id
}

// TestStep5NotEnteredIsReachableThroughTheRealHTTPUnsolicitedArrivalDoor is
// this door's analogue of the first door's own witness (see
// step5_wire_test.go's TestStep5NotEnteredIsReachableThroughTheRealHTTPArrivalDoor):
// it must fail RED whenever
// handleUnsolicitedArrival stops actually passing s.Cfg.HasOrientation()
// into RecordUnsolicitedArrivalOriented — not because the domain mechanism
// is broken (it isn't; buildArrivalVisits is proven at d98_not_entered_test.go
// and is the SAME choke point both doors call). Both people are posted as
// arriving; the only variable is whether the handler tells the domain this
// site administers an orientation.
func TestStep5NotEnteredIsReachableThroughTheRealHTTPUnsolicitedArrivalDoor(t *testing.T) {
	w := newStep5UnsolicitedWireWorld(t)
	form := url.Values{
		"time_source": {"default"},
		"vehicle":     {"TRK-900"},
		"origin_site": {"site-a"},
		"person_ids":  {w.adams.PersonID, w.baker.PersonID},
	}
	rec := postForm(t, w.srv, "/trips/arrive-unsolicited", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /trips/arrive-unsolicited = %d: %s", rec.Code, rec.Body.String())
	}
	arrivalID := step5UnsolicitedArrivalID(t, w.st, "site-a")
	notEntered := step5NotEnteredPersonIDs(t, w.st, arrivalID)
	want := []string{w.baker.PersonID}
	if len(notEntered) != len(want) || notEntered[0] != want[0] {
		t.Fatalf("trip_arrival_not_entered for this HTTP-driven unsolicited arrival at an orientation=true site = %v, want exactly %v (Baker, who has no orientation — Adams holds it and must NOT appear). "+
			"This is the reachability gap: handleUnsolicitedArrival must pass s.Cfg.HasOrientation() into domain.RecordUnsolicitedArrivalOriented.", notEntered, want)
	}
}
