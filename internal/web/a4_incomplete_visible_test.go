// a4_incomplete_visible_test.go — A4 / H-7 (L03 G-120): an incomplete
// check-in must be visible AND resolvable from /gate, the officer's default
// page, at every post — including posts with no vehicle page link.
package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// TestA4IncompleteCheckInShowsOnIndividualGateWithWorkingResume: the notice,
// its vehicle/driver, and a Resume form live on /gate itself (the page an
// officer lands on), and the form works from there. Site with NO destinations
// (npf/ndb/stp shape): /gate/vehicle is not linked, so /gate is the only door.
func TestA4IncompleteCheckInShowsOnIndividualGateWithWorkingResume(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	// Make this a no-vehicle-mode post (npf/ndb/stp shape) — the shape that
	// used to have no route to the resume page at all — and assert it.
	srv.Cfg.Peers, srv.Cfg.Destinations = nil, nil
	if srv.Cfg.VehicleModeEnabled() {
		t.Fatal("precondition: fixture must be a post with no vehicle mode")
	}
	trip, err := domain.DispatchLocalCheckIn(st, "site-a", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, st, "site-a", "T99"), DriverPersonID: persons[0].PersonID,
		OccupantPersonIDs: []string{persons[1].PersonID},
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if inc, err := domain.IncompleteCheckIns(st, "site-a"); err != nil || len(inc) != 1 {
		t.Fatalf("precondition: want exactly one incomplete check-in, got %v err %v", inc, err)
	}

	_, body := getGateIndividual(t, srv)
	for _, want := range []string{`id="incomplete-checkins"`, "INCOMPLETE CHECK-IN", "T99", `action="/gate/resume"`, trip.TripID} {
		if !strings.Contains(body, want) {
			t.Fatalf("/gate does not show the incomplete check-in (missing %q):\n%s", want, body)
		}
	}

	rec := postForm(t, srv, "/gate/resume", url.Values{"time_source": {"default"}, "trip_id": {trip.TripID}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("resume = %d: %s", rec.Code, rec.Body.String())
	}
	_, body = getGateIndividual(t, srv)
	if strings.Contains(body, "INCOMPLETE CHECK-IN") {
		t.Fatalf("/gate still shows the incomplete check-in after Resume:\n%s", body)
	}
}

// TestA4GateQuietWhenNothingIncomplete: the notice is not rendered on a
// healthy post (guards the check above against being trivially true).
func TestA4GateQuietWhenNothingIncomplete(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	_, body := getGateIndividual(t, srv)
	if strings.Contains(body, "incomplete-checkins") || strings.Contains(body, "INCOMPLETE CHECK-IN") {
		t.Fatalf("/gate shows an incomplete notice with nothing incomplete:\n%s", body)
	}
}

// TestA4MusterLinkTargetsTheIncompleteNotice: the muster warning's link must
// land on the page section that holds the Resume, and that section must
// exist on /gate at the same time.
func TestA4MusterLinkTargetsTheIncompleteNotice(t *testing.T) {
	srv, st := newMusterTestServer(t, 0)
	p, err := domain.CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	crashCheckIn(t, st, "site-a", "TRUCK 9", p.PersonID)

	for _, path := range []string{"/muster", "/muster/print"} {
		_, body := getMuster(t, srv, path)
		m := regexp.MustCompile(`class="incomplete-warn"[\s\S]*?<a href="(/gate[^"]*)"`).FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("%s: incomplete warning has no link to /gate:\n%s", path, body)
		}
		if m[1] != "/gate#incomplete-checkins" {
			t.Fatalf("%s: warning links to %q, want /gate#incomplete-checkins", path, m[1])
		}
	}
	_, gate := getGateIndividual(t, srv)
	if !strings.Contains(gate, `id="incomplete-checkins"`) {
		t.Fatalf("the muster's link target id is absent from /gate:\n%s", gate)
	}
}
