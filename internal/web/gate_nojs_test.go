// gate_nojs_test.go — V-05. The gate check-in must work with scripting off.
//
// This is a LOCKED contract line ("every page works with JS off"), and the
// gate is the page that decides who is on the muster. Before V-05 the
// occupant rows existed only inside a <template>, which a browser does not
// render and a JS-off browser can never clone, and the visible driver field
// carried no `name` at all — so with scripting off this form could check in
// nobody, and with the naive fix (naming the visible field `driver_value`)
// it would have checked in nobody on the JS-ON path instead, because Go's
// FormValue takes the FIRST value for a key in body order.
//
// Every test here posts EXACTLY what a scriptless browser would send: the
// visible fields, and nothing the script would otherwise have filled in.
//
// TWO BODY SHAPES LIVE HERE, deliberately.
//
//   - The ACCUMULATOR shape, which is what the page serves today: a
//     passenger list with row numbers and a driver flag. It is driven
//     through the served markup in gate_form_test.go, whose whole
//     instrument is a scriptless browser, and its JS-off contract is
//     asserted below by TestGateCheckInWorksWithJavaScriptDisabled.
//   - The PRE-EXISTING shape (driver_kind/driver_value/driver_typed plus
//     bare occupant arrays), which no page emits any more but which the
//     handler still accepts, for the same reason handleGateCheckOut still
//     accepts a single `visit_id` beside `visit_ids`: a deployed bookmark
//     or raw POST keeps its exact meaning. nojsCheckIn below posts that
//     shape, and the tests using it pin behaviour the accumulator inherits
//     — ambiguity refused rather than guessed, an off-roster name added,
//     a resolved id preferred over typed text, a typed row never dropped
//     for want of a hidden field.
package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// nojsCheckIn posts the form the way a browser with no JavaScript does:
// the typed fields only, with every hidden *_value left exactly as the
// server rendered it (empty).
func nojsCheckIn(t *testing.T, srv *Server, vehicle, driver string, occupants ...string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{
		"time_source":  {"default"},
		"vehicle":      {vehicle},
		"driver_typed": {driver},
		"driver_kind":  {"existing"}, // as served
		"driver_value": {""},         // the script never ran
	}
	for _, o := range occupants {
		form.Add("occupant_typed", o)
		form.Add("occupant_kind", "existing")
		form.Add("occupant_value", "")
		form.Add("occupant_company", "")
		form.Add("occupant_workgroup", "")
	}
	return postForm(t, srv, "/gate/checkin", form)
}

// TestGateCheckInWorksWithJavaScriptDisabled is the locked contract line,
// asserted against the form the page actually serves.
//
// It checks the two things only the served markup can get wrong — that the
// page carries no control which is inert without a script, and that the
// accumulate step is a real server route rather than a scripted one — and
// then drives a whole vehicle through, with a crew LARGER than the old
// four-row cap, because that cap was itself a JS-off failure: it turned a
// 26-person bus into six separate entries with six drivers to reconcile.
func TestGateCheckInWorksWithJavaScriptDisabled(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	_, page := getGate(t, srv)
	page = styleRe.ReplaceAllString(page, "")

	// The accumulate step must be a real POST to a real route. If "Add
	// person" were a scripted control the whole form would be dead with
	// scripting off, which is the V-05 defect in a new costume.
	form := gateFormMarkup(t, page, formIDFor(gateInbound))
	if !strings.Contains(form, `action="/gate/passenger/add"`) {
		t.Fatalf("the vehicle form does not post its accumulate step to a server route:\n%s", form)
	}
	if strings.Contains(form, `type="button"`) {
		t.Errorf("the vehicle form serves a type=button control — with scripting off it looks live and does nothing:\n%s", form)
	}
	// The open box must carry a name, and it must NOT be the one the script
	// writes the resolved id into. Go's FormValue returns the FIRST value
	// for a key in body order, so a visible field named new_value would put
	// the operator's typed TEXT where the resolved id belongs.
	openBox := regexp.MustCompile(`<input[^>]*class="p-open"[^>]*>`).FindString(form)
	if openBox == "" {
		t.Fatalf("no open passenger box on the gate page:\n%s", form)
	}
	// ⟦gate-checkin-spec, 2026-09-13⟧ new_typed is gone — the open box is
	// now new_last (this one, still class="p-open") + new_first (a separate
	// plain box), composed server-side (gateComposeName, gate.go).
	if !strings.Contains(openBox, `name="new_last"`) {
		t.Fatalf("the visible passenger field submits nothing with JS off: %s", openBox)
	}
	if strings.Contains(openBox, `name="new_value"`) {
		t.Fatalf("the visible passenger field is named new_value: %s\nGo's FormValue returns the FIRST value for a key, so the typed TEXT would arrive where the resolved person id belongs.", openBox)
	}

	// And the form must actually WORK, at a size the old one could not do
	// in one entry at all.
	names := []string{"Dana Driver", "Pat Passenger", "Chris Crew"}
	for i := 0; i < 5; i++ {
		names = append(names, fmt.Sprintf("Extra %02d", i))
	}
	_, vals := gateBuild(t, srv, gateInbound, "NOJS TRUCK", names, 0)
	// ⟦R-26⟧ this test is about scripting-off mechanics, not company — give
	// the off-roster "Extra NN" rows one so the commit is not refused for
	// the reason R-26 added.
	gateFillNewRowCompanies(t, vals, "R-26 Test Co")
	rec := postForm(t, srv, "/gate/checkin", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("JS-off check-in = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != len(names) {
		t.Fatalf("a JS-off check-in of %d people put %d on the muster — this is the silent-short-muster direction", len(names), len(open))
	}
	// Each resolved to the RIGHT person, by id, not by position.
	got := map[string]bool{}
	for _, v := range open {
		got[v.PersonID] = true
	}
	for _, p := range persons {
		if !got[p.PersonID] {
			t.Errorf("%s (%s) was typed into the form but is not on the muster", p.Name, p.PersonID)
		}
	}
	// The driver flag survived the round trips and named the right person.
	trips, err := domain.Trips(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 1 {
		t.Fatalf("%d trips, want 1", len(trips))
	}
	if trips[0].DriverPersonID != persons[0].PersonID {
		t.Fatalf("the trip records driver %s, want Dana Driver (%s) — the flagged row", trips[0].DriverPersonID, persons[0].PersonID)
	}
}

// The PRE-EXISTING body shape rendered four occupant boxes and used two, so
// its blank rows must be ignored rather than refused. That stays true for
// that shape. The accumulator's rule is the opposite and is asserted
// separately by TestGateCheckInRefusesANamelessAccumulatedRow: there a row
// exists only because the operator added a person, so a nameless one means
// something went wrong and skipping it silently is how a vehicle is
// recorded holding fewer people than it did.
func TestGateCheckInIgnoresUnusedOccupantRows(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	rec := nojsCheckIn(t, srv, "TRK-14", "Dana Driver", "", "", "", "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("check-in with only a driver = %d: %s", rec.Code, rec.Body.String())
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 {
		t.Fatalf("four blank occupant rows produced %d visits, want 1 (the driver)", len(open))
	}
}

// A name nobody on the roster has ADDS them — the gate's documented job.
// Re-derived rather than assumed: adding over-reports the muster, which is
// the safe direction, so doing it without a confirmation on the JS-off path
// is acceptable where removing somebody would not be.
func TestGateCheckInWithoutScriptAddsAnOffRosterPerson(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	rec := nojsCheckIn(t, srv, "NOJS 2", "Dana Driver", "Sam Newcomer")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("check-in with an off-roster occupant = %d: %s", rec.Code, rec.Body.String())
	}
	persons, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range persons {
		if p.Name == "Sam Newcomer" {
			found = true
		}
	}
	if !found {
		t.Fatal("a typed name that is not on the roster did not create the person — the gate page's stated job is to add people who are not on the roster")
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 {
		t.Fatalf("%d people on the muster, want 2", len(open))
	}
}

// TWO people with the same display name is not exotic on a work crew, and
// the system never guesses about identity. The refusal must be LOUD, must
// author nothing, and must say what to do.
func TestGateCheckInRefusesAnAmbiguousTypedNameRatherThanGuessing(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	if _, err := domain.CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	persons, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	dupes := 0
	for _, p := range persons {
		if p.Name == "Dana Driver" {
			dupes++
		}
	}
	if dupes != 2 {
		t.Fatalf("precondition: %d people called Dana Driver, want 2", dupes)
	}

	rec := nojsCheckIn(t, srv, "AMBIG 1", "Dana Driver")
	if rec.Code == http.StatusSeeOther {
		t.Fatal("an ambiguous typed name was accepted — one of two people with that name would be on the muster, and nothing anywhere would say which")
	}
	if rec.Code != http.StatusConflict {
		t.Errorf("ambiguous name = %d, want 409", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Dana Driver", "roster"} {
		if !strings.Contains(body, want) {
			t.Errorf("the refusal does not mention %q: %q", want, body)
		}
	}
	// A refusal authors nothing.
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 0 {
		t.Fatalf("a refused check-in still authored %d visits", len(open))
	}
}

// THE ATOMIC HALF. The JS-ON path must be unharmed: a resolved id in
// driver_value wins over whatever text sits in driver_typed, so the
// duplicate-name protection that resolvePick exists to provide still holds.
func TestGateCheckInPrefersTheResolvedIDOverTheTypedText(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	if _, err := domain.CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	persons, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	var second string
	for _, p := range persons {
		if p.Name == "Dana Driver" {
			second = p.PersonID // whichever the loop ends on; both are valid targets
		}
	}
	if second == "" {
		t.Fatal("precondition: no Dana Driver")
	}

	// Exactly what the script sends: the typed text AND the resolved id.
	rec := postForm(t, srv, "/gate/checkin", url.Values{
		"time_source":  {"default"},
		"vehicle":      {"JSON 1"},
		"driver_typed": {"Dana Driver"},
		"driver_kind":  {"existing"},
		"driver_value": {second},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("JS-on check-in = %d: %s", rec.Code, rec.Body.String())
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 {
		t.Fatalf("%d visits, want 1", len(open))
	}
	if open[0].PersonID != second {
		t.Fatalf("the check-in resolved to %s, want the id the script submitted (%s). An ambiguous NAME was preferred over an unambiguous ID — the duplicate-name protection is gone.", open[0].PersonID, second)
	}
}

// THE SERVER-SIDE HALF OF THE LOUD-FAILURE GUARD.
//
// gate.html's submit listener aborts loudly if a row's resolver is missing,
// but a guard that lives only in the script is no guard at all for the case
// where the script is what failed. So the handler must not depend on the
// hidden inputs having arrived: a row carrying occupant_typed and NOTHING
// ELSE is still a person the operator entered, and dropping it is the
// silent-short-muster outcome this whole finding is about.
//
// The old handler iterated `r.Form["occupant_kind"]` and indexed the other
// arrays off it, so a row whose hidden fields never arrived was invisible.
// This posts exactly that shape.
func TestGateCheckInNeverDropsATypedOccupantWhoseHiddenFieldsAreMissing(t *testing.T) {
	srv, st, _ := newGateTestServer(t)

	// A body with typed names but no occupant_kind / occupant_value at all
	// — what a page whose script tore out or never wired the hidden inputs
	// would send.
	rec := postForm(t, srv, "/gate/checkin", url.Values{
		"time_source":    {"default"},
		"vehicle":        {"BROKEN SCRIPT 1"},
		"driver_typed":   {"Dana Driver"},
		"occupant_typed": {"Pat Passenger", "Chris Crew"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("check-in with typed-only occupant rows = %d: %s", rec.Code, rec.Body.String())
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 3 {
		t.Fatalf("a check-in naming a driver and two occupants put %d people on the muster, want 3. Two people the operator typed were dropped because their hidden fields did not arrive — nothing on any screen or sheet would say so, and that is precisely the direction the muster must never fail in.", len(open))
	}
}
