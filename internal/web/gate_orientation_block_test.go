// gate_orientation_block_test.go — the GATE DOORS enforce the orientation
// rule (James, 2026-09-09: "they will not be allowed on site untill
// oriantation is completed the officer will not put there information in
// untill they are"). reports/witness/ORIENTATION-ENFORCEMENT.md MEASURED at
// f7afc3a that the rule held on the two trip-arrival doors and NOT on
// POST /gate/checkin or POST /gate/resume: domain.GateCheckIn and
// domain.ResumeCheckIn called RecordExpectedArrival without the
// hasOrientation flag, so buildArrivalVisits never consulted OrientationHeld
// and an un-oriented person walked onto the muster with nothing recorded.
//
// Three directions, each by driving the REAL HTTP door (step5_wire_test.go's
// posture — a domain-level call would pass whether or not any handler ever
// turns the flag on):
//  1. orientation=true: an un-oriented occupant is held out at BOTH gate
//     doors — no visit, a trip_arrival_not_entered row — and the officer is
//     told, by name, what to do next.
//  2. orientation=true: an ORIENTED vehicle still checks in normally and is
//     on the muster. The property that must not break.
//  3. orientation=false (a road post — the ice road): EVERY door enters
//     everyone; nobody owes an orientation there (D-88). Breaking this
//     would shut the ice road.
package web

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// orientBlockWorld is a site-b pad whose own config carries the orientation
// flag under test, with three roster people created AT site-b: Dana Driver
// and Orla Oriented hold B's orientation when the flag is on; Uma Unoriented
// never does. The names are chosen to be found nowhere else on any page.
type orientBlockWorld struct {
	srv    *Server
	st     *store.Store
	driver domain.Person
	orla   domain.Person
	uma    domain.Person
}

func newOrientBlockWorld(t *testing.T, orientation bool) *orientBlockWorld {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "orientblock.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	o := orientation
	cfg := &config.Config{
		SiteID: "site-b", DisplayName: "Pad Site Control", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 3600,
		LoginPassword: testLoginPassword,
		Peers:         []config.Peer{{SiteID: "site-a", BaseURL: "http://example.invalid"}},
		Destinations:  []config.Destination{{ID: "site-a", DisplayName: "Ice Road", Controlled: true}},
		Orientation:   &o,
	}
	w := &orientBlockWorld{st: st}
	w.driver = orientBlockPerson(t, st, "Dana Driver")
	w.orla = orientBlockPerson(t, st, "Orla Oriented")
	w.uma = orientBlockPerson(t, st, "Uma Unoriented")
	if orientation {
		// Recorded through the domain door, not the marking surface — step
		// 4's already-witnessed form is not the point here.
		for _, id := range []string{w.driver.PersonID, w.orla.PersonID} {
			if _, err := domain.CompleteOrientation(st, "site-b", 1, id, nil, 0, "default"); err != nil {
				t.Fatal(err)
			}
		}
	}
	w.srv = New(cfg, st)
	return w
}

func orientBlockPerson(t *testing.T, st *store.Store, name string) domain.Person {
	t.Helper()
	p, err := domain.CreatePerson(st, "site-b", 1, name, nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// orientBlockRows renders known roster people as the passenger-list rows the
// live form submits for a picked person: kind "existing", the id as value,
// the visible name as typed, the record's (empty) company/trade pickers.
func orientBlockRows(people ...domain.Person) []passengerRow {
	rows := make([]passengerRow, 0, len(people))
	for _, p := range people {
		rows = append(rows, passengerRow{Kind: "existing", Value: p.PersonID, Typed: p.Name})
	}
	return rows
}

// orientBlockDegenerateTrip finds THE site-b degenerate (local check-in)
// trip — origin site-b, no destination — which is what GateCheckIn authors.
func orientBlockDegenerateTrip(t *testing.T, st *store.Store) domain.Trip {
	t.Helper()
	trips, err := domain.Trips(st)
	if err != nil {
		t.Fatal(err)
	}
	var out []domain.Trip
	for _, tr := range trips {
		if tr.OriginSite == "site-b" && tr.DestinationSite == nil && tr.DestinationUncontrolled == nil {
			out = append(out, tr)
		}
	}
	if len(out) != 1 {
		t.Fatalf("expected exactly one site-b degenerate trip, found %d", len(out))
	}
	return out[0]
}

// orientBlockOpenPersonIDs is the set of people with an OPEN visit at
// site-b — "present on site" — read off the fold's own table.
func orientBlockOpenPersonIDs(t *testing.T, st *store.Store) map[string]bool {
	t.Helper()
	visits, err := domain.OpenVisitsAt(st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, v := range visits {
		out[v.PersonID] = true
	}
	return out
}

// orientBlockNotEnteredCount counts every trip_arrival_not_entered row in
// the store, whatever the arrival — the road-post proof needs "none at all".
func orientBlockNotEnteredCount(t *testing.T, st *store.Store) int {
	t.Helper()
	db, err := sql.Open("sqlite", st.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM trip_arrival_not_entered`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func orientBlockGet(t *testing.T, srv *Server, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// notEnteredNoticeText is the text of the NOT ENTERED notice(s) on a gate
// page, tags stripped — asserted on the notice itself, never the whole
// body, because the page's own help text elsewhere says "check them in".
func notEnteredNoticeText(page string) string {
	var out []string
	rest := page
	for {
		i := strings.Index(rest, `class="notice notice-warn not-entered"`)
		if i < 0 {
			break
		}
		rest = rest[i:]
		j := strings.Index(rest, "</div>")
		if j < 0 {
			break
		}
		out = append(out, rest[:j])
		rest = rest[j:]
	}
	return strings.Join(out, "\n")
}

// --- 1. orientation=true: the un-oriented are held out at BOTH gate doors --

// TestGateCheckInHoldsOutTheUnOrientedAtAnOrientationSite drives the busiest
// form in the application. Three aboard, one without B's orientation. The
// check-in must SUCCEED for the two who hold it (a partition, exactly what
// POST /trips/arrive does — not a refusal of the whole vehicle), Uma must
// have no visit and a not_entered row, and the page the officer lands on
// must name Uma and say what to do next.
func TestGateCheckInHoldsOutTheUnOrientedAtAnOrientationSite(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	body := scriptedCheckInBody("TRK-71", 0, orientBlockRows(w.driver, w.orla, w.uma))
	rec := postForm(t, w.srv, "/gate/checkin", body)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /gate/checkin = %d, want 303 (the check-in is not refused — the oriented occupants enter): %s", rec.Code, rec.Body.String())
	}
	trip := orientBlockDegenerateTrip(t, w.st)
	arrivalID := step5ArrivalIDForTrip(t, w.st, trip.TripID)
	if got, want := step5NotEnteredPersonIDs(t, w.st, arrivalID), []string{w.uma.PersonID}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("trip_arrival_not_entered for the gate check-in = %v, want exactly %v (Uma). This is the gate half of the gap ORIENTATION-ENFORCEMENT.md measured: GateCheckIn must pass HasOrientation through to RecordExpectedArrival.", got, want)
	}
	open := orientBlockOpenPersonIDs(t, w.st)
	if !open[w.driver.PersonID] || !open[w.orla.PersonID] {
		t.Fatalf("the ORIENTED occupants did not enter: open visits = %v", open)
	}
	if open[w.uma.PersonID] {
		t.Fatalf("Uma Unoriented has an OPEN VISIT at an orientation=true site — the gate entered an un-oriented person")
	}
	inc, err := domain.IncompleteCheckIns(w.st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(inc) != 0 {
		t.Fatalf("the check-in reads as INCOMPLETE after a hold-out: %+v — the trip must close; a hold-out is an arrival, not a crash", inc)
	}

	// The officer's next page. The redirect carries Uma's id, and the gate
	// page renders the notice off the roster: her NAME, the consequence,
	// and the route that fixes it.
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, notEnteredQueryKey+"="+w.uma.PersonID) {
		t.Fatalf("redirect %q does not carry the held-out id — the notice would die with the redirect", loc)
	}
	notice := notEnteredNoticeText(orientBlockGet(t, w.srv, loc))
	for _, want := range []string{
		"NOT ENTERED",
		"Uma Unoriented does not hold Pad Site Control's orientation",
		"not on site and not on the muster",
		"there is no override",
		`<a href="/orientation">Orientation</a>`,
		// ⟦W3, 2026-10-03⟧ ⟦was: "then check them in again"⟧ — stale since
		// entry-on-completion (R-16): recording the completion enters them
		// off this arrival, and a second check-in would be a second entry.
		"do not check them in again unless that page says it could not",
	} {
		if !strings.Contains(notice, want) {
			t.Errorf("the NOT ENTERED notice lacks %q.\nnotice: %s", want, notice)
		}
	}
	if strings.Contains(notice, "Orla Oriented") || strings.Contains(notice, "Dana Driver") {
		t.Errorf("the notice names someone who WAS entered: %s", notice)
	}
	// And the muster agrees with the notice.
	muster := orientBlockGet(t, w.srv, "/muster")
	if strings.Contains(muster, "Uma Unoriented") {
		t.Error("Uma Unoriented is on the muster")
	}
	for _, name := range []string{"Dana Driver", "Orla Oriented"} {
		if !strings.Contains(muster, name) {
			t.Errorf("%s is NOT on the muster after an ordinary oriented check-in", name)
		}
	}
}

// TestGateResumeHoldsOutTheUnOrientedAtAnOrientationSite is the second gate
// door: a check-in that died between its two Author calls (the dispatch
// half is authored directly here, exactly the crash shape IncompleteCheckIns
// lists) and is then resumed. The crash window is not a way around the rule.
func TestGateResumeHoldsOutTheUnOrientedAtAnOrientationSite(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	trip, err := domain.DispatchLocalCheckIn(w.st, "site-b", 1, domain.DispatchInput{
		VehicleID:         w7SeedVehicleID(t, w.st, "site-b", "TRK-72"),
		DriverPersonID:    w.driver.PersonID,
		OccupantPersonIDs: []string{w.orla.PersonID, w.uma.PersonID},
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if inc, err := domain.IncompleteCheckIns(w.st, "site-b"); err != nil || len(inc) != 1 {
		t.Fatalf("precondition: the half-authored check-in is not listed as incomplete (%d, %v)", len(inc), err)
	}
	rec := postForm(t, w.srv, "/gate/resume", url.Values{"trip_id": {trip.TripID}, "time_source": {"default"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /gate/resume = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	arrivalID := step5ArrivalIDForTrip(t, w.st, trip.TripID)
	if got, want := step5NotEnteredPersonIDs(t, w.st, arrivalID), []string{w.uma.PersonID}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("trip_arrival_not_entered for the RESUMED check-in = %v, want exactly %v (Uma): ResumeCheckIn must carry HasOrientation too", got, want)
	}
	open := orientBlockOpenPersonIDs(t, w.st)
	if !open[w.driver.PersonID] || !open[w.orla.PersonID] || open[w.uma.PersonID] {
		t.Fatalf("after resume, open visits = %v: want driver and Orla open, Uma NOT", open)
	}
	if inc, err := domain.IncompleteCheckIns(w.st, "site-b"); err != nil || len(inc) != 0 {
		t.Fatalf("the resumed check-in is still listed as incomplete (%d, %v)", len(inc), err)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, notEnteredQueryKey+"="+w.uma.PersonID) {
		t.Fatalf("resume redirect %q does not carry the held-out id", loc)
	}
	if notice := notEnteredNoticeText(orientBlockGet(t, w.srv, loc)); !strings.Contains(notice, "Uma Unoriented does not hold Pad Site Control's orientation") {
		t.Errorf("the resume's landing page does not name Uma in a NOT ENTERED notice: %s", notice)
	}
}

// --- 2. orientation=true: the ORIENTED still check in, end to end ----------

// TestAnOrientedVehicleStillChecksInAtTheGateAndIsOnTheMuster is the
// property the wiring must not break: at an orientation=true post, a
// vehicle whose every occupant holds the orientation checks in exactly as
// before — no notice, no not_entered row, everyone on the muster.
func TestAnOrientedVehicleStillChecksInAtTheGateAndIsOnTheMuster(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	if _, err := domain.CompleteOrientation(w.st, "site-b", 1, w.uma.PersonID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	body := scriptedCheckInBody("TRK-73", 0, orientBlockRows(w.driver, w.orla, w.uma))
	rec := postForm(t, w.srv, "/gate/checkin", body)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /gate/checkin (all oriented) = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if strings.Contains(loc, notEnteredQueryKey+"=") {
		t.Fatalf("an all-oriented check-in redirected with a hold-out: %q", loc)
	}
	if n := orientBlockNotEnteredCount(t, w.st); n != 0 {
		t.Fatalf("an all-oriented check-in wrote %d trip_arrival_not_entered row(s)", n)
	}
	open := orientBlockOpenPersonIDs(t, w.st)
	for _, p := range []domain.Person{w.driver, w.orla, w.uma} {
		if !open[p.PersonID] {
			t.Errorf("%s (oriented) has no open visit after the gate check-in", p.Name)
		}
	}
	if notice := notEnteredNoticeText(orientBlockGet(t, w.srv, loc)); notice != "" {
		t.Errorf("the landing page shows a NOT ENTERED notice for an all-oriented vehicle: %s", notice)
	}
	muster := orientBlockGet(t, w.srv, "/muster")
	for _, name := range []string{"Dana Driver", "Orla Oriented", "Uma Unoriented"} {
		if !strings.Contains(muster, name) {
			t.Errorf("%s is NOT on the muster after an all-oriented check-in", name)
		}
	}
}

// --- 3. orientation=false: a road post enters everyone through every door --

// TestARoadPostEntersEveryoneThroughEveryDoor: at an orientation=false post
// nobody holds an orientation (the marking route is not even registered —
// D-88), and that must not read as "nobody is oriented": every door enters
// everyone, no not_entered row is ever written, and the muster lists them
// all. All four doors, distinct people per door so no later door's dispatch
// closes an earlier door's visit.
func TestARoadPostEntersEveryoneThroughEveryDoor(t *testing.T) {
	w := newOrientBlockWorld(t, false)
	if w.srv.Cfg.HasOrientation() {
		t.Fatal("precondition: this world must be a road post")
	}
	mk := func(names ...string) []domain.Person {
		var out []domain.Person
		for _, n := range names {
			out = append(out, orientBlockPerson(t, w.st, n))
		}
		return out
	}
	var everyone []domain.Person

	// Door 1: POST /gate/checkin.
	d1 := mk("Gate Driver", "Gate One", "Gate Two")
	rec := postForm(t, w.srv, "/gate/checkin", scriptedCheckInBody("RD-1", 0, orientBlockRows(d1...)))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("road post POST /gate/checkin = %d: %s", rec.Code, rec.Body.String())
	}
	everyone = append(everyone, d1...)

	// Door 2: POST /gate/resume on a half-authored check-in.
	d2 := mk("Resume Driver", "Resume One")
	trip, err := domain.DispatchLocalCheckIn(w.st, "site-b", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, w.st, "site-b", "RD-2"), DriverPersonID: d2[0].PersonID, OccupantPersonIDs: []string{d2[1].PersonID},
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	rec = postForm(t, w.srv, "/gate/resume", url.Values{"trip_id": {trip.TripID}, "time_source": {"default"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("road post POST /gate/resume = %d: %s", rec.Code, rec.Body.String())
	}
	everyone = append(everyone, d2...)

	// Door 3: POST /trips/arrive for a trip dispatched from site-a.
	d3 := mk("Trip Driver", "Trip One", "Trip Two")
	dest, dl := "site-b", int64(90)
	inbound, err := domain.Dispatch(w.st, "site-a", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, w.st, "site-a", "RD-3"), DriverPersonID: d3[0].PersonID,
		OccupantPersonIDs: []string{d3[1].PersonID, d3[2].PersonID}, DestinationSite: &dest, DeadlineMinutes: &dl,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	rec = postForm(t, w.srv, "/trips/arrive", url.Values{
		"time_source":         {"default"},
		"trip_id":             {inbound.TripID},
		"manifest_person_ids": {d3[1].PersonID, d3[2].PersonID},
		"arriving_person_ids": {d3[1].PersonID, d3[2].PersonID},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("road post POST /trips/arrive = %d: %s", rec.Code, rec.Body.String())
	}
	everyone = append(everyone, d3...)

	// Door 4: POST /trips/arrive-unsolicited.
	d4 := mk("Unsol One", "Unsol Two")
	rec = postForm(t, w.srv, "/trips/arrive-unsolicited", url.Values{
		"time_source": {"default"}, "vehicle": {"RD-4"}, "origin_site": {"site-a"},
		"person_ids": {d4[0].PersonID, d4[1].PersonID},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("road post POST /trips/arrive-unsolicited = %d: %s", rec.Code, rec.Body.String())
	}
	everyone = append(everyone, d4...)

	if n := orientBlockNotEnteredCount(t, w.st); n != 0 {
		t.Fatalf("a road post wrote %d trip_arrival_not_entered row(s) — the ice road just held somebody out", n)
	}
	open := orientBlockOpenPersonIDs(t, w.st)
	muster := orientBlockGet(t, w.srv, "/muster")
	for _, p := range everyone {
		if !open[p.PersonID] {
			t.Errorf("%s has no open visit at the road post", p.Name)
		}
		if !strings.Contains(muster, p.Name) {
			t.Errorf("%s is not on the road post's muster", p.Name)
		}
	}
	if len(everyone) != 10 {
		t.Fatalf("this test drove %d people; it meant to drive 10 through four doors", len(everyone))
	}
	if notice := notEnteredNoticeText(orientBlockGet(t, w.srv, "/gate")); notice != "" {
		t.Errorf("a road post's gate shows a NOT ENTERED notice: %s", notice)
	}
}

// --- the scripting-off body shape reaches the same rule --------------------

// TestTheLegacyGateCheckInBodyHoldsOutTheUnOrientedToo: POST /gate/checkin
// has TWO body shapes — the passenger-list form above and the pre-existing
// driver_kind/driver_value + bare occupant arrays shape legacyGateCheckIn
// keeps working. Both construct GateCheckInInput; a fix that wired one and
// not the other would leave the rule enforceable by choice of form.
func TestTheLegacyGateCheckInBodyHoldsOutTheUnOrientedToo(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	rec := postForm(t, w.srv, "/gate/checkin", url.Values{
		"time_source":    {"default"},
		"vehicle":        {"TRK-74"},
		"driver_kind":    {"existing"},
		"driver_value":   {w.driver.PersonID},
		"occupant_kind":  {"existing", "existing"},
		"occupant_value": {w.orla.PersonID, w.uma.PersonID},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("legacy POST /gate/checkin = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	trip := orientBlockDegenerateTrip(t, w.st)
	if got := step5NotEnteredPersonIDs(t, w.st, step5ArrivalIDForTrip(t, w.st, trip.TripID)); len(got) != 1 || got[0] != w.uma.PersonID {
		t.Fatalf("legacy body: trip_arrival_not_entered = %v, want exactly Uma — legacyGateCheckIn must set HasOrientation too", got)
	}
	open := orientBlockOpenPersonIDs(t, w.st)
	if !open[w.driver.PersonID] || !open[w.orla.PersonID] || open[w.uma.PersonID] {
		t.Fatalf("legacy body: open visits = %v: want driver and Orla open, Uma NOT", open)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, notEnteredQueryKey+"="+w.uma.PersonID) {
		t.Fatalf("legacy redirect %q does not carry the held-out id", loc)
	}
}
