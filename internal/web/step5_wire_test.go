// step5_wire_test.go — SCHEMA-11 step 5's REACHABILITY witness (D-98/D-91,
// team-lead follow-up to the step5-builder's flagged gap): the domain split
// (buildArrivalVisits / RecordExpectedArrivalExcluding, see
// d98_not_entered_test.go) already works and is already tested at the
// domain layer — that is NOT what this file proves, because a domain-level
// call would pass whether or not any web caller ever turns the flag on.
// This file drives the REAL HTTP door, POST /trips/arrive, exactly as the
// gate/console forms do, and asserts the split actually lands. Produced RED
// first: on the pre-wiring tree handleExpectedArrival calls
// domain.RecordExpectedArrivalExcluding with no trailing hasOrientation
// argument at all, so it is always the zero-length variadic (= false)
// regardless of s.Cfg.HasOrientation() — every occupant enters and
// trip_arrival_not_entered stays empty even at an orientation=true site.
package web

import (
	"database/sql"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// step5WireWorld is a site-b pad, orientation=true, with one trip inbound
// from site-a carrying a driver and two occupants: Adams holds B's
// orientation before the trip ever arrives, Baker does not.
type step5WireWorld struct {
	srv   *Server
	st    *store.Store
	trip  domain.Trip
	adams domain.Person
	baker domain.Person
}

func newStep5WireWorld(t *testing.T) *step5WireWorld {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "step5wire.db"))
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
	driver, err := domain.CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	adams, err := domain.CreatePerson(st, "site-a", 1, "Adams", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	baker, err := domain.CreatePerson(st, "site-a", 1, "Baker", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// Adams and the driver already hold site-b's orientation; Baker never
	// has. The driver is an occupant too (§10: one visit per trip occupant,
	// driver included — TripOccupantsByTrip returns the driver alongside
	// the passengers), so it must be given orientation as well or it would
	// land in not_entered right alongside Baker and the assertion below
	// would not isolate the one case this test cares about. Recorded
	// directly through the domain door (not the point under test — the
	// marking surface is step 4's, already wired, orientation_test.go).
	for _, id := range []string{driver.PersonID, adams.PersonID} {
		if _, err := domain.CompleteOrientation(st, "site-b", 1, id, nil, 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	dest, dl := "site-b", int64(90)
	trip, err := domain.Dispatch(st, "site-a", 1, domain.DispatchInput{
		VehicleID:         w7SeedVehicleID(t, st, "site-a", "TRK-500"),
		DriverPersonID:    driver.PersonID,
		OccupantPersonIDs: []string{adams.PersonID, baker.PersonID},
		DestinationSite:   &dest, DeadlineMinutes: &dl,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	return &step5WireWorld{srv: New(cfg, st), st: st, trip: trip, adams: adams, baker: baker}
}

// step5ArrivalIDForTrip and step5NotEnteredPersonIDs read the fold's own
// tables directly (never an HTTP dump — D-56's convention, w9c_seams_test.go)
// so the assertion is against what was actually authored and folded, not
// against any rendered page.
func step5ArrivalIDForTrip(t *testing.T, st *store.Store, tripID string) string {
	t.Helper()
	db, err := sql.Open("sqlite", st.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var id string
	if err := db.QueryRow(`SELECT arrival_id FROM trip_arrival WHERE trip_ref = ?`, tripID).Scan(&id); err != nil {
		t.Fatalf("reading trip_arrival for trip %s: %v", tripID, err)
	}
	return id
}

func step5NotEnteredPersonIDs(t *testing.T, st *store.Store, arrivalID string) []string {
	t.Helper()
	db, err := sql.Open("sqlite", st.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT person_id FROM trip_arrival_not_entered WHERE arrival_id = ?`, arrivalID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// TestStep5NotEnteredIsReachableThroughTheRealHTTPArrivalDoor is the
// witness the team lead ordered: it must fail RED because the FEATURE IS
// UNREACHABLE from any live caller today, not because the domain mechanism
// is broken (it isn't — d98_not_entered_test.go already proves it). Both
// occupants are ticked as arriving (nobody stayed behind at an
// uncontrolled pad — TestUntickingSomebodyLeavesThemOffThisSitesMuster's
// concern, inbound_test.go, not this one); the only variable is whether
// the handler tells the domain this site administers an orientation.
func TestStep5NotEnteredIsReachableThroughTheRealHTTPArrivalDoor(t *testing.T) {
	w := newStep5WireWorld(t)
	form := url.Values{
		"time_source":         {"default"},
		"trip_id":             {w.trip.TripID},
		"manifest_person_ids": {w.adams.PersonID, w.baker.PersonID},
		"arriving_person_ids": {w.adams.PersonID, w.baker.PersonID},
	}
	rec := postForm(t, w.srv, "/trips/arrive", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /trips/arrive = %d: %s", rec.Code, rec.Body.String())
	}
	arrivalID := step5ArrivalIDForTrip(t, w.st, w.trip.TripID)
	notEntered := step5NotEnteredPersonIDs(t, w.st, arrivalID)
	want := []string{w.baker.PersonID}
	if len(notEntered) != len(want) || notEntered[0] != want[0] {
		t.Fatalf("trip_arrival_not_entered for this HTTP-driven arrival at an orientation=true site = %v, want exactly %v (Baker, who has no orientation — Adams holds it and must NOT appear). "+
			"This is the reachability gap: handleExpectedArrival must pass s.Cfg.HasOrientation() into domain.RecordExpectedArrivalExcluding.", notEntered, want)
	}
}
