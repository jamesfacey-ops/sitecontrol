// trips_uncontrolled_test.go — configurable destinations over real HTTP
// (James, 2026-08-20): one dispatch button per configured destination;
// controlled destinations require the deadline, uncontrolled ones refuse it
// (both directions), and an uncontrolled run records occupants and checks
// them out of the origin without ever becoming an expectation anywhere.
//
// Campaign traps honoured: assertions are on body content or store state,
// never status-of-GET (the catch-all returns 200 for everything); markup
// assertions strip <style> regions first (a class name in the stylesheet is
// not a rendered element).
package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

var styleRe = regexp.MustCompile(`(?s)<style>.*?</style>`)

// newDestinationsTestServer is newTripsTestServer plus an explicit
// destination list: the peer (controlled, display "Pad Site Control") and an
// uncontrolled pad (display "Conneco Pad"). Config objects built directly in
// tests bypass Load()'s validation exactly as every other web test does; the
// list here is one Load would accept (config_test.go proves the rules).
func newDestinationsTestServer(t *testing.T) (*Server, *destWorld) {
	t.Helper()
	srv, st, persons := newTripsTestServer(t)
	srv.Cfg.Destinations = []config.Destination{
		{ID: "site-a", DisplayName: "Pad Site Control", Controlled: true},
		{ID: "conneco", DisplayName: "Conneco Pad", Controlled: false},
	}
	return srv, &destWorld{st: st, persons: persons}
}

type destWorld struct {
	st      *store.Store
	persons []domain.Person
}

func TestDispatchFormRendersOneButtonPerConfiguredDestination(t *testing.T) {
	srv, _ := newDestinationsTestServer(t)
	page := styleRe.ReplaceAllString(getTrips(t, srv), "")

	for _, want := range []string{
		`name="destination_id" value="site-a"`,
		`name="destination_id" value="conneco"`,
		"Dispatch to Pad Site Control",
		"Record run to Conneco Pad",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("GET /trips (styles stripped) is missing %q", want)
		}
	}
	// The old single-select destination control is gone — the two paths are
	// separate buttons, not one form with a forgettable optional field.
	if strings.Contains(page, `name="destination_site"`) {
		t.Fatalf("dispatch form still renders a destination_site control")
	}
}

// The uncontrolled direction of the deadline rule, over the real entry
// point: a deadline alongside an uncontrolled destination is REFUSED (409 +
// explanation), never silently dropped.
func TestDispatchUncontrolledOverHTTPRefusesDeadline(t *testing.T) {
	srv, w := newDestinationsTestServer(t)

	rec := postForm(t, srv, "/trips/dispatch", url.Values{
		"time_source":      {"default"},
		"vehicle":          {"TRUCK 1"},
		"driver_person_id": {w.persons[0].PersonID},
		"destination_id":   {"conneco"},
		"deadline_minutes": {"60"},
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("uncontrolled destination with a deadline = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "uncontrolled") {
		t.Fatalf("refusal body does not explain the uncontrolled rule: %s", rec.Body.String())
	}
	trips, err := domain.Trips(w.st)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 0 {
		t.Fatalf("a trip was authored despite the refusal: %+v", trips)
	}
}

// The controlled direction still holds when reached via the new button
// field: no deadline → refused (F4, unchanged).
func TestDispatchControlledViaButtonStillRequiresDeadline(t *testing.T) {
	srv, w := newDestinationsTestServer(t)

	rec := postForm(t, srv, "/trips/dispatch", url.Values{
		"time_source":      {"default"},
		"vehicle":          {"TRUCK 1"},
		"driver_person_id": {w.persons[0].PersonID},
		"destination_id":   {"site-a"},
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("controlled destination without a deadline = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	trips, err := domain.Trips(w.st)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 0 {
		t.Fatalf("a trip was authored despite the refusal: %+v", trips)
	}
}

func TestDispatchUnknownDestinationIDRefused(t *testing.T) {
	srv, w := newDestinationsTestServer(t)

	rec := postForm(t, srv, "/trips/dispatch", url.Values{
		"time_source":      {"default"},
		"vehicle":          {"TRUCK 1"},
		"driver_person_id": {w.persons[0].PersonID},
		"destination_id":   {"connecoo"}, // the F4 typo shape, one layer up
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("unknown destination_id = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	trips, err := domain.Trips(w.st)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 0 {
		t.Fatalf("a trip was authored despite the refusal: %+v", trips)
	}
}

func TestDispatchConflictingDestinationFieldsRefused(t *testing.T) {
	srv, w := newDestinationsTestServer(t)

	rec := postForm(t, srv, "/trips/dispatch", url.Values{
		"time_source":      {"default"},
		"vehicle":          {"TRUCK 1"},
		"driver_person_id": {w.persons[0].PersonID},
		"destination_id":   {"conneco"},
		"destination_site": {"site-a"},
		"deadline_minutes": {"60"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("destination_id + destination_site = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	trips, err := domain.Trips(w.st)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 0 {
		t.Fatalf("a trip was authored despite the refusal: %+v", trips)
	}
}

// The whole uncontrolled workflow over HTTP: occupants on site, one button
// press → run recorded (destination + occupants), occupants checked out of
// the origin (off its muster set), and NO expectation exists anywhere — not
// on this site's board, not on the peer's.
func TestDispatchUncontrolledOverHTTPRecordsRunAndChecksOutOccupants(t *testing.T) {
	srv, w := newDestinationsTestServer(t)

	// Put both people on site at site-b via the ordinary degenerate
	// check-in machinery (the c6World pattern).
	walkIn, err := domain.DispatchLocalCheckIn(w.st, "site-b", 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "gate walk-in", DriverPersonID: w.persons[0].PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.st, "site-b", "gate walk-in"), DriverPersonID: w.persons[0].PersonID,
		OccupantPersonIDs: []string{w.persons[1].PersonID},
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.RecordExpectedArrival(w.st, "site-b", 1, walkIn.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	open, err := domain.OpenVisitsAt(w.st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 {
		t.Fatalf("precondition: %d open visits, want 2", len(open))
	}

	rec := postForm(t, srv, "/trips/dispatch", url.Values{
		"time_source":         {"default"},
		"vehicle":             {"TRUCK 1"},
		"driver_person_id":    {w.persons[0].PersonID},
		"occupant_person_ids": {w.persons[1].PersonID},
		"destination_id":      {"conneco"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("uncontrolled dispatch = %d, want 303: %s", rec.Code, rec.Body.String())
	}

	// Recorded: destination + occupants.
	trips, err := domain.Trips(w.st)
	if err != nil {
		t.Fatal(err)
	}
	var run *domain.Trip
	for i := range trips {
		if trips[i].DestinationUncontrolled != nil {
			run = &trips[i]
		}
	}
	if run == nil || *run.DestinationUncontrolled != "conneco" || run.DestinationSite != nil || run.DeadlineMinutes != nil {
		t.Fatalf("recorded run = %+v, want destination_uncontrolled=conneco, no site, no deadline", run)
	}
	occ, err := domain.TripOccupantsByTrip(w.st, run.TripID)
	if err != nil {
		t.Fatal(err)
	}
	if len(occ) != 2 {
		t.Fatalf("%d occupant rows on the run, want 2 — who is out at the pad must be answerable", len(occ))
	}

	// Checked out of the origin: off its open-visit (muster) set.
	open, err = domain.OpenVisitsAt(w.st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 0 {
		t.Fatalf("%d open visits after the run, want 0 — they are genuinely not on site", len(open))
	}

	// No expectation anywhere.
	for _, siteID := range []string{"site-a", "site-b", "conneco"} {
		exps, err := domain.OpenExpectations(w.st, siteID)
		if err != nil {
			t.Fatal(err)
		}
		if len(exps) != 0 {
			t.Fatalf("uncontrolled run appears on %s's inbound board: %+v", siteID, exps)
		}
	}

	// And the trips list names the pad rather than reading "local".
	page := styleRe.ReplaceAllString(getTrips(t, srv), "")
	if !strings.Contains(page, "Conneco Pad (uncontrolled — no timer)") {
		t.Fatalf("trips list does not show the uncontrolled destination:\n%s", page)
	}
}

// The legacy field keeps its exact pre-destinations meaning: a raw POST with
// destination_site (the controlled path) still authors an inbound
// expectation — deployed muscle memory and any bookmarked form keep working.
func TestLegacyDestinationSiteFieldStillControlled(t *testing.T) {
	srv, w := newDestinationsTestServer(t)

	rec := postForm(t, srv, "/trips/dispatch", url.Values{
		"time_source":      {"default"},
		"vehicle":          {"TRUCK 1"},
		"driver_person_id": {w.persons[0].PersonID},
		"destination_site": {"site-a"},
		"deadline_minutes": {"60"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("legacy controlled dispatch = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	trips, err := domain.Trips(w.st)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 1 || trips[0].DestinationSite == nil || *trips[0].DestinationSite != "site-a" {
		t.Fatalf("legacy dispatch rows = %+v, want one trip destined site-a", trips)
	}
}
