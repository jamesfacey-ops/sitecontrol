// local_departure_test.go — D-4 (JAMES-DECISIONS.md) at the HTTP layer:
// "Remove local check-out. Every arrival and departure is a vehicle."
//
// The console used to carry a "Local check-out only (no destination)"
// button that posted destination_id="" and authored a trip with no
// destination, no deadline and no arrival — byte-identical to a check-in
// that crashed between its two events. domain.IncompleteCheckIns selects
// exactly that shape, so every press put a departed crew on /gate behind a
// Resume button that would check them back onto this muster in one click,
// and made the printed muster read MAY BE INCOMPLETE.
//
// The tests here cover the three things that had to change together: the
// markup no longer offers it, the handler refuses it anyway (a raw POST
// never reads markup), and the three downstream consequences are gone.
package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// checkInPairThroughTheGateForm puts both fixture people on site the way an
// officer does — through the gate form, not through a domain call — so the
// departure tests below are measuring the real pipeline.
func checkInPairThroughTheGateForm(t *testing.T, srv *Server, w *destWorld, vehicle string) {
	t.Helper()
	rec := postForm(t, srv, "/gate/checkin", url.Values{
		"time_source":    {"default"},
		"vehicle":        {vehicle},
		"driver_kind":    {"existing"},
		"driver_value":   {w.persons[0].PersonID},
		"occupant_kind":  {"existing"},
		"occupant_value": {w.persons[1].PersonID},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("gate check-in = %d: %s", rec.Code, rec.Body.String())
	}
}

// TestTheConsoleOffersNoDestinationLessDispatch is the markup half.
//
// It asserts a POSITIVE CONTROL first — the real per-destination buttons are
// still rendered. Without that, deleting the whole button row (or breaking
// the template so nothing renders at all) would satisfy the absence
// assertions and read as a pass.
func TestTheConsoleOffersNoDestinationLessDispatch(t *testing.T) {
	srv, _ := newDestinationsTestServer(t)
	page := styleRe.ReplaceAllString(getTrips(t, srv), "")

	for _, want := range []string{
		`name="destination_id" value="site-a"`,
		`name="destination_id" value="conneco"`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("positive control failed: the dispatch buttons are missing %q — the absence assertions below would pass vacuously:\n%s", want, page)
		}
	}
	if strings.Contains(page, `name="destination_id" value=""`) {
		t.Error(`the console still submits an empty destination_id — that authors a trip indistinguishable from a crashed check-in (D-4)`)
	}
	if strings.Contains(page, "Local check-out only") {
		t.Error(`the "Local check-out only (no destination)" button is back — D-4 removed it`)
	}
}

// TestTheConsoleRefusesADestinationLessDispatch is the half that matters,
// because deleting a button protects nobody against a hand-crafted POST.
// Both shapes the removed button could produce are asserted: the field
// present and empty (exactly what it submitted) and the field absent
// altogether.
func TestTheConsoleRefusesADestinationLessDispatch(t *testing.T) {
	srv, w := newDestinationsTestServer(t)
	checkInPairThroughTheGateForm(t, srv, w, "TRK-14")

	before, err := domain.Trips(w.st)
	if err != nil {
		t.Fatal(err)
	}

	bodies := map[string]url.Values{
		"the removed button's exact body": {
			"vehicle": {"TRK-14"}, "driver_person_id": {w.persons[0].PersonID},
			"occupant_person_ids": {w.persons[1].PersonID}, "destination_id": {""},
		},
		"no destination field at all": {
			"vehicle": {"TRK-14"}, "driver_person_id": {w.persons[0].PersonID},
			"occupant_person_ids": {w.persons[1].PersonID},
		},
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			rec := postForm(t, srv, "/trips/dispatch", body)
			if rec.Code != http.StatusConflict {
				t.Fatalf("destination-less dispatch = %d, want 409: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "where it is going") {
				t.Errorf("the refusal does not say what is missing: %s", rec.Body.String())
			}
		})
	}

	after, err := domain.Trips(w.st)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("a refused dispatch authored %d trip(s): %+v", len(after)-len(before), after)
	}
	// And the people are still on the muster — a refusal that had already
	// checked them out would be worse than the bug it replaces.
	open, err := domain.OpenVisitsAt(w.st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 {
		t.Fatalf("after two refused dispatches: %d open visits, want 2 — nothing may have been recorded", len(open))
	}
}

// TestARoutineDepartureNeverReadsAsACrashedCheckIn is the consequence test:
// the three things D-4 exists to clear, asserted through the real pages.
//
// EVERY ASSERTION HAS A CONTROL. The second half of this test authors a
// genuine crashed check-in — the shape F1's box is supposed to mean, and the
// only remaining way to produce it — and asserts that all three DO fire.
// Without that half, a template that never renders the box, a muster section
// that was deleted, or a Contains() looking for a string that no longer
// exists anywhere would all read as a pass.
func TestARoutineDepartureNeverReadsAsACrashedCheckIn(t *testing.T) {
	srv, w := newDestinationsTestServer(t)
	checkInPairThroughTheGateForm(t, srv, w, "TRK-14")

	// Precondition: quiet before the departure, so "quiet afterwards" is a
	// consequence rather than the fixture.
	assertIncompletenessQuiet(t, srv, "before any departure")

	rec := postForm(t, srv, "/trips/dispatch", url.Values{
		"time_source": {"default"},
		"vehicle":     {"TRK-14"}, "driver_person_id": {w.persons[0].PersonID},
		"occupant_person_ids": {w.persons[1].PersonID},
		"destination_id":      {"conneco"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("routine departure to a pad = %d: %s", rec.Code, rec.Body.String())
	}
	if inc, err := domain.IncompleteCheckIns(w.st, "site-b"); err != nil || len(inc) != 0 {
		t.Fatalf("a routine departure is listed as an incomplete check-in: %v %+v", err, inc)
	}
	assertIncompletenessQuiet(t, srv, "after a routine departure")

	// THE CONTROL. A real crashed check-in — the dispatch half of a gate
	// check-in with no arrival behind it — must still fire all three.
	if _, err := domain.DispatchLocalCheckIn(w.st, "site-b", 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRUCK 9", DriverPersonID: w.persons[0].PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.st, "site-b", "TRUCK 9"), DriverPersonID: w.persons[0].PersonID,
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// ⟦R-27, James, 2026-09-16⟧ /gate/vehicle, not /gate — the incomplete-
	// check-ins box moved there unchanged when /gate became the
	// streamlined individual page; see gate_vehicle.html's own comment.
	gate := getPage(t, srv, "/gate/vehicle")
	if !strings.Contains(gate, "Incomplete check-ins — needs attention") {
		t.Fatalf("control failed: a genuine crashed check-in is not surfaced on /gate/vehicle, so the assertions above prove nothing:\n%s", gate)
	}
	if !strings.Contains(gate, `action="/gate/resume"`) {
		t.Fatal("control failed: a genuine crashed check-in offers no Resume button, so the assertions above prove nothing")
	}
	for _, path := range []string{"/muster", "/muster/print"} {
		body := getPage(t, srv, path)
		if !strings.Contains(body, `class="incomplete-warn"`) || !strings.Contains(body, "MAY BE INCOMPLETE") {
			t.Fatalf("control failed: %s does not warn about a genuine crashed check-in, so the assertions above prove nothing", path)
		}
	}
}

// assertIncompletenessQuiet checks all three consequences at once: no
// incomplete-check-in box on /gate, no Resume button, and no MAY BE
// INCOMPLETE on the live OR the printed muster (the printed one is the
// safety-critical half — .incomplete-warn is deliberately kept in the print
// stylesheet).
//
// It asserts on STRUCTURAL hooks as well as words, because the gate page's
// own operator copy names "MAY BE INCOMPLETE" while explaining why a
// destination is required — a whole-body scan for the phrase would match
// that furniture rather than the warning, which is the exact failure mode
// GATE-REBUILD's M13/M17 survivors had.
func assertIncompletenessQuiet(t *testing.T, srv *Server, when string) {
	t.Helper()
	// ⟦R-27⟧ /gate/vehicle — see this file's other getPage comment.
	gate := getPage(t, srv, "/gate/vehicle")
	if strings.Contains(gate, "Incomplete check-ins — needs attention") {
		t.Errorf("%s: /gate/vehicle shows the incomplete-check-in box", when)
	}
	if strings.Contains(gate, `action="/gate/resume"`) {
		t.Errorf("%s: /gate/vehicle offers a Resume button — one click would put a departed crew back on the muster", when)
	}
	for _, path := range []string{"/muster", "/muster/print"} {
		body := getPage(t, srv, path)
		if strings.Contains(body, `class="incomplete-warn"`) {
			t.Errorf("%s: %s renders the incompleteness warning", when, path)
		}
		if strings.Contains(body, "MAY BE INCOMPLETE") {
			t.Errorf("%s: %s reads MAY BE INCOMPLETE — alarm fatigue on the one warning that stops a printed roster being silently short", when, path)
		}
	}
}

// TestTheTickedListSaysItCorrectsACheckInAndRecordsNoDeparture.
//
// The ticked list survives D-4, but not as an exit. It is the only way to
// take a wrongly-recorded name off the muster, and an uncorrectable phantom
// is somebody an evacuation searches for who was never there. What it must
// no longer do is present itself as a way of leaving — the operator reading
// this page has to be able to tell a correction from a departure, because
// the system cannot tell them apart afterwards.
// departureCopyThatMustNotReturn is the copy D-4 removed: the ticked list
// presented as a way of leaving rather than as a correction.
var departureCopyThatMustNotReturn = []string{
	"off-vehicle departures",
	"Check out the ticked people",
}

func TestTheTickedListSaysItCorrectsACheckInAndRecordsNoDeparture(t *testing.T) {
	srv, w := newDestinationsTestServer(t)
	checkInPairThroughTheGateForm(t, srv, w, "TRK-14")
	// ⟦R-27, James, 2026-09-16⟧ /gate/vehicle used to carry the "what this
	// list is for" expander (gated on GateMode=="vehicle") stating this
	// fact in full prose, while gate.html (the individual page) never did
	// — R-27 deliberately stripped it there.
	// ⟦R-28 revision 2, team-lead, 2026-09-16⟧ item 4 is a new, explicit
	// ruling that supersedes the gap the comment above describes: "the
	// roster half in vehicle mode must be IDENTICAL to the individual
	// page's" — the expander is gone from BOTH pages now (gate_roster_half
	// is, and always was, the one shared define; only its own CSS scoping
	// used to make the two pages look different — see that define's own
	// comment). The POSITIVE assertion this test used to make ("the page
	// SAYS corrects a mistaken check-in / records no departure") is
	// retired with it: gate.html's own page never said this either, under
	// R-27's own already-accepted "streamlined" ruling, and nothing about
	// removing the vehicle page's copy makes that any less true there than
	// it already was here. What D-4 actually needs — that the ticked list
	// never AGAIN reads as a way of LEAVING — does not depend on this
	// prose existing at all, and is the assertion that survives below,
	// unweakened.
	page := getPage(t, srv, "/gate/vehicle")

	if !strings.Contains(page, `action="/gate/checkout"`) {
		t.Errorf("/gate/vehicle no longer serves the ticked-list correction form at all — missing action=\"/gate/checkout\"")
	}
	// The old copy called it a departure path. Under D-4 nobody leaves
	// off-vehicle, so that sentence is not merely stale — it invites the
	// exact act the ruling removed.
	//
	// Matched through phraseSeen: gate.html wraps its copy mid-sentence, and
	// "Check out the ticked people" was reintroducible across a line break
	// with this guard still green (MEASURED — S103-WATCHLISTS.md Amendment D,
	// MUTANT K). The control runs before the page is read.
	assertPhraseWatchBites(t, departureCopyThatMustNotReturn)
	for _, gone := range departureCopyThatMustNotReturn {
		if phraseSeen(page, gone) {
			t.Errorf("/gate still offers the ticked list as a way of leaving: %q", gone)
		}
	}
}

// TestAPostWithNoDestinationsConfiguredSaysDeparturesCannotBeRecorded.
//
// The consequence of "every departure names a destination" is that a post
// with none configured cannot record one at all. That has to be SAID — an
// empty button row reads as a broken page, and an operator who cannot see
// why will reach for whatever else is on the screen (which, before D-4, was
// the local check-out button).
func TestAPostWithNoDestinationsConfiguredSaysDeparturesCannotBeRecorded(t *testing.T) {
	srv, _ := newDestinationsTestServer(t)
	srv.Cfg.Destinations = nil
	srv.Cfg.Peers = nil

	page := styleRe.ReplaceAllString(getTrips(t, srv), "")
	if !strings.Contains(page, "No destinations are configured at this post") {
		t.Fatalf("the console does not say why it cannot dispatch:\n%s", page)
	}
	if strings.Contains(page, `name="destination_id"`) {
		t.Error("a dispatch button is rendered at a post with no destinations")
	}
}
