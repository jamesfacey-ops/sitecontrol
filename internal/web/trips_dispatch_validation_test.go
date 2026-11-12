// trips_dispatch_validation_test.go — F4 + F11 (campaign findings),
// reproduced over REAL HTTP against the actual production entry point:
// POST /trips/dispatch (handleDispatch, trips.go) is the one Dispatch()
// caller reachable by a raw/malformed POST — the form's own <select>
// elements only ever submit valid roster ids and real peer sites, but
// nothing stops a direct POST from carrying anything else. New coverage
// only; nothing in trips_test.go is modified.
package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// TestHandleDispatchOverHTTPRefusesGarbageDriverPersonID is F11's exact
// reproduction: a raw POST with a driver_person_id that names nobody on the
// roster. Before the fix this authored a trip that could never complete via
// arrival (409 "not on the roster" at the destination) and became a
// permanent false OVERDUE once its deadline passed.
func TestHandleDispatchOverHTTPRefusesGarbageDriverPersonID(t *testing.T) {
	srv, st, _ := newTripsTestServer(t)

	rec := postForm(t, srv, "/trips/dispatch", url.Values{
		"time_source":      {"default"},
		"vehicle":          {"TRUCK 1"},
		"driver_person_id": {"nobody-on-the-roster"},
		"destination_site": {"site-a"},
		"deadline_minutes": {"60"},
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("dispatch with a garbage driver_person_id = %d, want 409 (refused): %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "roster") {
		t.Fatalf("refusal body does not explain the roster check: %s", rec.Body.String())
	}
	trips, err := domain.Trips(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 0 {
		t.Fatalf("a phantom trip was authored despite the refusal: %+v", trips)
	}
}

// TestHandleDispatchOverHTTPRefusesGarbageOccupantPersonID covers the
// occupant half of the same check.
func TestHandleDispatchOverHTTPRefusesGarbageOccupantPersonID(t *testing.T) {
	srv, st, persons := newTripsTestServer(t)

	rec := postForm(t, srv, "/trips/dispatch", url.Values{
		"time_source":         {"default"},
		"vehicle":             {"TRUCK 1"},
		"driver_person_id":    {persons[0].PersonID},
		"occupant_person_ids": {"nobody-on-the-roster"},
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("dispatch with a garbage occupant id = %d, want 409 (refused): %s", rec.Code, rec.Body.String())
	}
	trips, err := domain.Trips(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 0 {
		t.Fatalf("a phantom trip was authored despite the refusal: %+v", trips)
	}
}

// TestHandleDispatchOverHTTPRefusesUnknownDestinationSite is F4's exact
// reproduction: a raw POST with a typo'd destination that matches no
// configured peer. Before the fix this authored fine and matched no site's
// OpenExpectations — dispatched, a deadline promised, scanned nowhere,
// alerted never.
func TestHandleDispatchOverHTTPRefusesUnknownDestinationSite(t *testing.T) {
	srv, st, persons := newTripsTestServer(t) // only "site-a" is a configured peer

	rec := postForm(t, srv, "/trips/dispatch", url.Values{
		"time_source":      {"default"},
		"vehicle":          {"TRUCK 1"},
		"driver_person_id": {persons[0].PersonID},
		"destination_site": {"site-bee"}, // the exact typo shape from the finding
		"deadline_minutes": {"60"},
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("dispatch to an unknown destination = %d, want 409 (refused): %s", rec.Code, rec.Body.String())
	}
	trips, err := domain.Trips(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 0 {
		t.Fatalf("a scanned-by-nobody trip was authored despite the refusal: %+v", trips)
	}
}

// TestHandleDispatchOverHTTPStillAcceptsARealRosterIDAndKnownDestination is
// the positive control: legitimate traffic — an id actually on the roster,
// a destination that is actually a configured peer — must still work
// through the real HTTP path after both checks are wired on.
func TestHandleDispatchOverHTTPStillAcceptsARealRosterIDAndKnownDestination(t *testing.T) {
	srv, st, persons := newTripsTestServer(t)

	rec := postForm(t, srv, "/trips/dispatch", url.Values{
		"time_source":      {"default"},
		"vehicle":          {"TRUCK 1"},
		"driver_person_id": {persons[0].PersonID},
		"destination_site": {"site-a"},
		"deadline_minutes": {"60"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("dispatch with a real roster id and known destination = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	trips, err := domain.Trips(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 1 {
		t.Fatalf("trips = %+v, want exactly 1", trips)
	}
}
