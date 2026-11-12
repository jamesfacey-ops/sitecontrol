// gate_checkin_single_test.go — R-25 (James, 2026-09-13: "if a vehicle id
// is added use that as a group but if a guard wants to just check in one
// person at a time then use that"): POST /gate/checkin/single, the SECOND
// inbound check-in rhythm alongside the existing accumulate-then-commit
// one. See gate.go's handleGateCheckInSingle/commitGateCheckIn for the
// implementation and their own comments for why this is not a new
// authoring path — it is the SAME body handleGateCheckIn already ran, fed
// a one-row list built straight from the open box; and gate.html's own
// template comment (the "arriving" section) for why it is a genuinely
// SEPARATE <form> rather than a second formaction on gate_vehicle_form's
// shared one.
//
// Bodies are built the way this package's own gate_form_test.go insists on
// (see its header comment): scraped off the FRESH page's real markup, not
// hand-typed — a hand-typed body proves the handler works and says nothing
// about whether the served page can actually reach it (TP §4.5's
// time-capture fields are exactly the kind of thing a hand-typed body
// silently omits). gateFormValues cannot be reused as-is: it locates a
// form by id="vehicle-form-(in|out)" (gate_form_test.go's own regex), and
// this form is a different, unrelated instance — singleCheckInFormValues
// below is the same scrape, over this form's own markup instead.
package web

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

var singleCheckInFormRe = regexp.MustCompile(`(?s)<form\b[^>]*action="/gate/checkin/single"[^>]*>.*?</form>`)

// singleCheckInFormValues is gateFormValues' own scrape (gate_form_test.go:
// every named input except submit/button controls, radios/checkboxes only
// when checked), run over the R-25 form's own markup instead of a
// vehicle-form-* instance. No <select> lives on this form, so that half of
// gateFormValues' rule has nothing to reuse.
func singleCheckInFormValues(t *testing.T, page string) url.Values {
	t.Helper()
	markup := singleCheckInFormRe.FindString(page)
	if markup == "" {
		t.Fatalf("no single-check-in form on the page:\n%s", page)
	}
	vals := url.Values{}
	for _, tag := range inputTagRe.FindAllString(markup, -1) {
		a := tagAttrs(tag)
		name := a["name"]
		if name == "" {
			continue
		}
		switch a["type"] {
		case "submit", "button", "reset":
			continue
		case "radio", "checkbox":
			if _, ok := a["checked"]; !ok {
				continue
			}
		}
		vals.Add(name, a["value"])
	}
	return vals
}

// singleCheckInForm is a fresh gate page's R-25 form, scraped off the page
// that actually serves it, with just a name typed into it, ready to POST
// to /gate/checkin/single.
//
// ⟦R-28 revision 2, team-lead, 2026-09-16⟧ item 1: RETARGETED from
// getGate (GET /gate/vehicle) to getGateIndividual (GET /gate) — the
// vehicle page's own copy of this rhythm is gone ("individual mode
// already does that"), and gate.html's own top CHECK IN form now posts
// to this SAME route (action="/gate/checkin/single" — see gate.html) and
// runs the SAME handleGateCheckInSingle body. singleCheckInFormValues
// still finds it: its regex matches ANY form by its action attribute, not
// by page or class, so the scrape needs no change, only which page is
// fetched. The individual form's own Vehicle number field carries a
// value (R-27's own field list), unlike the removed dedicated form which
// had none at all — left as scraped (blank on a fresh page, per F43) so
// every existing caller's "no vehicle typed" precondition still holds.
func singleCheckInForm(t *testing.T, srv *Server, lastName string) url.Values {
	t.Helper()
	_, page := getGateIndividual(t, srv)
	vals := singleCheckInFormValues(t, page)
	vals.Set("new_last", lastName)
	vals.Set("new_first", "")
	return vals
}

// TestGateCheckInSingleWithNoVehicleRecordsOneUnidentified is R-25's
// headline behaviour: ONE POST, no vehicle typed, no prior "Add person"
// step — a brand-new off-roster name (this fixture's site owes no
// orientation, D-88) is checked in as a group-of-one, D-4 intact: a
// vehicle IS still recorded, just an unidentified one (R-17/R-18's
// existing mechanism) — never a vehicle-less arrival.
func TestGateCheckInSingleWithNoVehicleRecordsOneUnidentified(t *testing.T) {
	srv, st, _ := newGateTestServer(t)

	vehiclesBefore, err := domain.Vehicles(st)
	if err != nil {
		t.Fatal(err)
	}

	vals := singleCheckInForm(t, srv, "Solo Walker")
	// ⟦R-26⟧ this test is about the vehicle/unidentified mechanism, not
	// company — give the new person one through the add-new escape so the
	// commit is not refused for the reason R-26 added.
	vals.Set("new_company_id", newRef)
	vals.Set("new_company", "R-26 Test Co")
	rec := postForm(t, srv, "/gate/checkin/single", vals)
	if rec.Code != 303 {
		t.Fatalf("single check-in = %d, want 303: %s", rec.Code, rec.Body.String())
	}

	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range open {
		if v.PersonSnapshotName != nil && *v.PersonSnapshotName == "Solo Walker" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Solo Walker is not on site after a single check-in: %+v", open)
	}

	// ⛔ NOTHING IS MINTED FOR AN UNIDENTIFIED VEHICLE (R-17).
	vehiclesAfter, err := domain.Vehicles(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(vehiclesAfter) != len(vehiclesBefore) {
		t.Fatalf("a single, vehicle-less check-in changed the vehicle count %d -> %d — nothing should ever be minted for this fact", len(vehiclesBefore), len(vehiclesAfter))
	}

	// ⛔ D-4: an unidentified-vehicle check-in is ORDINARY, never a crashed,
	// incomplete one.
	inc, err := domain.IncompleteCheckIns(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(inc) != 0 {
		t.Fatalf("a single check-in with no vehicle appears on IncompleteCheckIns: %+v", inc)
	}

	musterRec := getPage(t, srv, "/muster")
	if strings.Contains(musterRec, "MAY BE INCOMPLETE") {
		t.Error("the muster reads MAY BE INCOMPLETE after a single, vehicle-less check-in — D-4 forbids this ambiguity")
	}
	if !strings.Contains(musterRec, "Solo Walker") {
		t.Error("Solo Walker does not appear on the muster after a single check-in")
	}
	// The muster's on-site roster shows name/company/trade only (never a
	// vehicle, by James's own "name and company" rule — rosterPerson's own
	// comment) — tripVehicleDisplay's "Vehicle unknown" wording is wired to
	// /trips/detail's trip list (trips.go), not the muster, so that is
	// where this is checked.
	detailRec := getPage(t, srv, "/trips/detail")
	if !strings.Contains(detailRec, domain.VehicleUnidentifiedText) {
		t.Errorf("/trips/detail does not render %q for the single check-in's vehicle", domain.VehicleUnidentifiedText)
	}
}

// TestGateCheckInSingleAnExistingPersonNeedsNoConfirmationToBeUnchanged is
// the fix this route needed before it worked at all: a single check-in of
// a person ALREADY on the roster, typed exactly, with the open box's
// Company/Trade/Position/Phone boxes left blank (nobody was asked to fill
// them in — there was never an accumulate step to decorate the row from
// the record). Left undecorated, recordChanges reads every blank box as a
// claimed change against the record and refuses with "not what their
// record says" — this proves it does not, because handleGateCheckInSingle
// now decorates the row FROM THE RECORD first, exactly as the ordinary
// accumulate-then-render cycle already does for every other row.
func TestGateCheckInSingleAnExistingPersonNeedsNoConfirmationToBeUnchanged(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	chris := persons[2] // "Chris Crew" — see newGateTestServer

	vals := singleCheckInForm(t, srv, chris.Name)
	rec := postForm(t, srv, "/gate/checkin/single", vals)
	if rec.Code != 303 {
		t.Fatalf("single check-in of an unchanged existing person = %d, want 303: %s", rec.Code, rec.Body.String())
	}

	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range open {
		if v.PersonID == chris.PersonID {
			found = true
		}
	}
	if !found {
		t.Fatalf("%s is not on site after the single check-in: %+v", chris.Name, open)
	}

	all, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != len(persons) {
		t.Fatalf("a single check-in of an EXISTING person minted %d roster row(s), want %d unchanged — it should have resolved to the record, not created a second one", len(all), len(persons))
	}
}

// TestARefusedGateCheckInSingleAuthorsNothing is R-25's own all-or-nothing
// requirement (his ruling: "the same [guarantee] ... it is a property of a
// SUBMISSION, not of a group size"). It refuses the SAME way an ordinary
// accumulate-then-commit refusal does — an existing person whose posted
// company disagrees with their record, with "change their record" left
// unticked ([[D-84]]) — and proves nothing at all was authored: no visit,
// no vehicle, no second roster row, exactly the comint-on-refusal /
// D3-window guarantee this route reuses rather than reimplements.
func TestARefusedGateCheckInSingleAuthorsNothing(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	chris := persons[2]

	otherCo, err := domain.CreateCompany(st, "site-a", 1, "A Different Company LLC", 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	openBefore, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	personsBefore, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	vehiclesBefore, err := domain.Vehicles(st)
	if err != nil {
		t.Fatal(err)
	}

	vals := singleCheckInForm(t, srv, chris.Name)
	// ⟦[[D-84]]⟧ new_value simulates the scripted pick (a real mousedown on
	// Chris's own roster row) that stable-resolution (decorateRow's "the
	// pickers are the officer's" branch) needs before a posted company can
	// be compared against the record at all — WITHOUT it, decorateRow's
	// exact-match path overwrites the open box from the record before any
	// difference could be seen, which is the correct behaviour for an
	// ordinary typed name and exactly why this needs to be posted here.
	vals.Set("new_kind", "existing")
	vals.Set("new_value", chris.PersonID)
	vals.Set("new_company_id", otherCo.CompanyID) // disagrees with the record
	// amend_confirm deliberately NOT set — the officer never ticked it.
	rec := postForm(t, srv, "/gate/checkin/single", vals)
	if rec.Code != 409 {
		t.Fatalf("single check-in with an unconfirmed record change = %d, want 409 (refused): %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Nothing was recorded") && !strings.Contains(rec.Body.String(), "Nothing has been recorded") {
		t.Errorf("the refusal does not tell the officer nothing was recorded: %s", rec.Body.String())
	}

	openAfter, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(openAfter) != len(openBefore) {
		t.Fatalf("a REFUSED single check-in changed the open-visit count %d -> %d — nothing should be recorded until the whole submission validates", len(openBefore), len(openAfter))
	}
	personsAfter, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(personsAfter) != len(personsBefore) {
		t.Fatalf("a REFUSED single check-in changed the roster count %d -> %d", len(personsBefore), len(personsAfter))
	}
	vehiclesAfter, err := domain.Vehicles(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(vehiclesAfter) != len(vehiclesBefore) {
		t.Fatalf("a REFUSED single check-in changed the vehicle count %d -> %d", len(vehiclesBefore), len(vehiclesAfter))
	}
}

// TestGateCheckInSingleAndGroupRhythmsBothWork demonstrates BOTH halves of
// R-25's own ruling in one test: a vehicle id typed and two people
// accumulated commits as ONE GROUP (today's behaviour, untouched), and
// SEPARATELY, no vehicle id and one person checks in in ONE action — the
// two rhythms coexist, neither route disturbs the other's store state.
func TestGateCheckInSingleAndGroupRhythmsBothWork(t *testing.T) {
	srv, st, _ := newGateTestServer(t)

	// --- the GROUP rhythm: vehicle id typed, two people, one commit ---
	_, vals := gateBuild(t, srv, gateInbound, "TRK-77", []string{"Dana Driver", "Pat Passenger"}, 0)
	rec := postForm(t, srv, "/gate/checkin", vals)
	if rec.Code != 303 {
		t.Fatalf("group check-in = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 {
		t.Fatalf("after the group check-in, %d open visits, want 2: %+v", len(open), open)
	}

	// --- the SINGLE rhythm: no vehicle, one person, one action ---
	rec = postForm(t, srv, "/gate/checkin/single", singleCheckInForm(t, srv, "Chris Crew"))
	if rec.Code != 303 {
		t.Fatalf("single check-in after a group check-in = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	open, err = domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 3 {
		t.Fatalf("after group(2) + single(1), %d open visits, want 3: %+v", len(open), open)
	}
}
