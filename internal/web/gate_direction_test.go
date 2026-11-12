// gate_direction_test.go — THE SAFETY ASYMMETRY, one direction at a time.
//
// The gate's arriving and departing forms share one template, one
// accumulator and one set of routes, because James asked for the trip back
// out to be the same as the trip in. That is exactly the situation
// design/bulk-movement-v1.md's rule exists to guard:
//
//	which default direction is safe depends on whether the action ADDS or
//	REMOVES someone from the muster, not on the widget
//
// So NOTHING in this file is shared between the two directions. There is no
// helper that asserts "the confirmation mentions the count" for both and
// calls it done, because such a helper would pass with the two directions'
// warnings swapped — which is the precise defect this project already
// shipped once, when the pad-return manifest was given the inbound board's
// shape under the comment "same ARRIVAL derivation as the inbound board
// above".
//
// Each test below writes out its own derivation and asserts one direction.
// Swap the two MusterWarning strings in gate.html and BOTH go red.
package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

var commitButtonRe = regexp.MustCompile(`(?s)<button[^>]*class="button-primary gate-commit"[^>]*>.*?</button>`)

// commitControl returns the commit button's markup for ONE direction. It
// takes a direction and returns one button; it cannot be used to compare
// the two, which is the point.
func commitControl(t *testing.T, page, direction string) string {
	t.Helper()
	markup := gateFormMarkup(t, page, formIDFor(direction))
	b := commitButtonRe.FindString(markup)
	if b == "" {
		t.Fatalf("no commit button on the %q form:\n%s", direction, markup)
	}
	return b
}

// TestTheInboundCommitWarnsThatOmissionLeavesSomebodyOffTheMuster.
//
// DERIVATION, for the ARRIVING form only. Committing an inbound trip ADDS
// people to the muster.
//
//   - A person OMITTED from the list is physically on site with NO RECORD
//     AT ALL — absent from the evacuation sheet, and nobody will ever look
//     for them. This is the dangerous direction.
//   - A person wrongly INCLUDED is recorded on site when they are not: the
//     muster over-reports and somebody is searched for who is not there.
//     Wasted search time. Safe direction.
//   - After the commit, NOTHING WILL EVER FLAG THE OMISSION. There is no
//     later process that notices a missing arrival, because the system has
//     no expectation to compare against: under D-3 manifests are advisory
//     and are never loaded.
//
// Therefore this commit is the last moment anybody can catch a missed
// person, and it presses hardest: it states the count back AND names the
// consequence in the dangerous direction.
func TestTheInboundCommitWarnsThatOmissionLeavesSomebodyOffTheMuster(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	page, _ := gateBuild(t, srv, gateInbound, "TRK-14", []string{"Dana Driver", "Pat Passenger"}, 0)
	button := commitControl(t, page, gateInbound)

	if !strings.Contains(button, "will not be on the muster") {
		t.Fatalf("the arriving commit does not warn that an omitted person is off the muster:\n%s\nThis is the last moment anyone can catch a missed arrival — nothing afterwards ever flags one.", button)
	}
	if !strings.Contains(button, "Check in all 2 people on TRK-14") {
		t.Errorf("the arriving commit does not state the count and the vehicle back:\n%s", button)
	}
	// And the same sentence is on the page itself, not only inside a
	// scripted confirm — with scripting off there is no dialog at all.
	form := gateFormMarkup(t, page, formIDFor(gateInbound))
	if strings.Count(form, "will not be on the muster") < 2 {
		t.Errorf("the muster warning appears only in the confirm attribute. With scripting off no dialog fires, so it must also be visible on the page:\n%s", form)
	}
	// The warning is also carried as its own attribute, because the script
	// rebuilds the confirmation text as the passenger count changes and
	// must COPY the warning rather than decide it. If it were only inside
	// the composed confirm string, the script would have to re-derive it —
	// and a script that can compose this sentence can compose it for the
	// wrong direction.
	if !strings.Contains(button, `data-commit-warning="Anyone not on this list will not be on the muster."`) {
		t.Errorf("the arriving commit carries no separate warning attribute for the script to copy:\n%s", button)
	}
}

// TestTheOutboundCommitDoesNotWarnAboutTheMuster.
//
// DERIVATION, done from scratch for the DEPARTING form. Committing an
// outbound trip REMOVES people from the muster.
//
//   - A person OMITTED merely stays marked on site after leaving: the
//     muster over-reports, somebody is searched for who has gone. Wasted
//     search time. SAFE direction.
//   - A person wrongly INCLUDED is recorded as having left while still on
//     site — present, absent from the sheet. Dangerous. But reaching that
//     requires the operator to actively type a specific person's name into
//     a departing vehicle, which is a different and much rarer error than
//     forgetting one.
//   - Unlike inbound, the omission IS self-correcting: they stay on the
//     muster, and the next check-in or muster read surfaces them.
//
// Therefore the inbound warning is FALSE here — everyone not on this list
// stays ON the muster — and printing it anyway would do active harm: an
// operator who reads it six days a week where it is false learns to read
// past it where it is true. The count is still stated back, because the
// count is cheap and it catches the wrongly-included case, which is the
// dangerous one in THIS direction.
func TestTheOutboundCommitDoesNotWarnAboutTheMuster(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	checkInCrew(t, srv, st, 3)
	page, _ := gateBuildOutbound(t, srv, "TRK-14", []string{visitIDForCrewName(t, st, "Crew 00")}, []string{"Crew 01"}, 0)
	button := commitControl(t, page, gateOutbound)

	if strings.Contains(button, "will not be on the muster") {
		t.Fatalf("the departing commit carries the ARRIVING form's muster warning:\n%s\nIt is false here — everyone not on this list stays ON the muster — and an operator who reads it where it is false learns to read past it where it is true.", button)
	}
	// ⟦R-28⟧ RETARGETED wording only: the departing form is now the SAME
	// Compact box the individual page's checkout-confirm already used
	// ("Confirm check out ...", gate_roster_half) rather than this page's
	// own former plain "Check out ..." verb — the count-and-vehicle
	// assertion this line exists for is otherwise unchanged.
	if !strings.Contains(button, "Confirm check out all 2 people on TRK-14") {
		t.Errorf("the departing commit does not state the count and the vehicle back:\n%s\nThe count is what catches the wrongly-included case, which is the dangerous one in this direction.", button)
	}
	form := gateFormMarkup(t, page, formIDFor(gateOutbound))
	if strings.Contains(form, "will not be on the muster") {
		t.Errorf("the departing form states the arriving form's muster warning somewhere on the page:\n%s", form)
	}
	// The attribute the script copies must be EMPTY here — not merely
	// absent from the composed string, because the script rebuilds that
	// string on every change and would otherwise have to decide for itself
	// which direction gets the sentence.
	if !strings.Contains(button, `data-commit-warning=""`) {
		t.Errorf("the departing commit's warning attribute is not empty, so the script would copy a sentence that is false here:\n%s", button)
	}
}

// --- what the outbound form exists to record ---------------------------

// TestOutboundVehicleCheckOutRecordsAVehicleAndADriver is the capability
// this consolidation exists to add, so it gets its own assertion rather
// than being implied by the inbound tests.
//
// Until now the gate officer's ONLY exit was POST /gate/checkout, which
// reads visit ids and nothing else: VisitCheckedOut carries {VisitID,
// RecordedBy}. No vehicle. No driver. The driver flag exists so that a
// report written after an incident can say who was at the wheel, and
// James's reason for wanting the outbound leg to look like the inbound one
// is that the driver usually CHANGES between them — so an exit path that
// cannot record a driver defeats the flag on exactly the leg where it
// changes.
func TestOutboundVehicleCheckOutRecordsAVehicleAndADriver(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	checkInCrew(t, srv, st, 4)
	before, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 4 {
		t.Fatalf("precondition: %d people on site, want 4", len(before))
	}
	tripsBefore, err := domain.Trips(st)
	if err != nil {
		t.Fatal(err)
	}

	// Crew 02 drives out with Crew 00 and Crew 03. Crew 01 stays.
	_, vals := gateBuildOutbound(t, srv, "TRK-14", []string{visitIDForCrewName(t, st, "Crew 00")}, []string{"Crew 02", "Crew 03"}, 1)
	vals.Set("destination_id", "conneco")
	rec := postForm(t, srv, "/gate/checkout/vehicle", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("outbound commit = %d: %s", rec.Code, rec.Body.String())
	}

	trips, err := domain.Trips(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != len(tripsBefore)+1 {
		t.Fatalf("%d trips, want one more than the %d before", len(trips), len(tripsBefore))
	}
	var out *domain.Trip
	for i := range trips {
		// ⟦LOCK — W7 seam repair, 2026-09-03⟧ ⟦was: "if trips[i].VehicleEntered == \"TRK-14\" {"⟧
		if trips[i].VehicleEntered != nil && *trips[i].VehicleEntered == "TRK-14" {
			out = &trips[i]
		}
	}
	if out == nil {
		t.Fatal("no trip carries the departing vehicle id — the exit still records no vehicle")
	}
	driver, err := domain.PersonByID(st, out.DriverPersonID)
	if err != nil {
		t.Fatal(err)
	}
	if driver == nil || driver.Name != "Crew 02" {
		t.Fatalf("the departure records driver %v, want the flagged passenger Crew 02", driver)
	}

	// Three people left; the fourth is untouched.
	after, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 {
		t.Fatalf("%d people still on site after a 3-person departure, want 1", len(after))
	}
	stayed, err := domain.PersonByID(st, after[0].PersonID)
	if err != nil {
		t.Fatal(err)
	}
	if stayed == nil || stayed.Name != "Crew 01" {
		t.Fatalf("the person left on site is %v, want Crew 01 — somebody who was not on the vehicle was checked out", stayed)
	}
	// One occupant row per person, driver included exactly once.
	occ, err := domain.TripOccupantsByTrip(st, out.TripID)
	if err != nil {
		t.Fatal(err)
	}
	if len(occ) != 3 {
		t.Fatalf("%d occupant rows for a 3-person departure — the flagged passenger was counted twice", len(occ))
	}
}

// TestOutboundRefusesAPassengerWhoIsNotOnTheRoster. This is the ONE
// mechanical rule that differs between the two directions, and it falls out
// of the same ADD/REMOVE derivation rather than being bolted on.
//
// Arriving, a typed name matching nobody CREATES the person: that ADDS
// somebody to the muster, which over-reports — the safe direction — and it
// is this page's documented job, because a person who is physically here
// and on no list is the failure that matters. Departing, creating a person
// in order to immediately record them as having left produces a departure
// with no arrival behind it. The honest answer is that they were never
// checked in, so the refusal says exactly that and names them.
func TestOutboundRefusesAPassengerWhoIsNotOnTheRoster(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	checkInCrew(t, srv, st, 2)
	peopleBefore, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}

	_, vals := gateBuildOutbound(t, srv, "TRK-14", []string{visitIDForCrewName(t, st, "Crew 00")}, []string{"Nobody Knowsme"}, 0)
	vals.Set("destination_id", "conneco")
	rec := postForm(t, srv, "/gate/checkout/vehicle", vals)
	if rec.Code == http.StatusSeeOther {
		t.Fatal("a departing vehicle named somebody who was never checked in and it was accepted — that records a departure with no arrival behind it, and invents a roster person to hang it on")
	}
	if rec.Code != http.StatusConflict {
		t.Errorf("outbound with an unknown name = %d, want 409", rec.Code)
	}
	body := rec.Body.String()
	// The refusal NOTICE, not the page. The departing form's own help text
	// already contains "check them in", so a whole-body scan passes even
	// when the handler has stopped refusing at all — mutation-proved: a
	// version that invented the person survived that assertion.
	msg := vehicleError(t, body, gateOutbound)
	if !strings.Contains(msg, "Nobody Knowsme") {
		t.Errorf("the refusal does not name who could not be resolved: %q", msg)
	}
	if !strings.Contains(msg, "check them in") {
		t.Errorf("the refusal does not say what to do instead: %q", msg)
	}
	// Nothing authored, and the list survives.
	peopleAfter, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(peopleAfter) != len(peopleBefore) {
		t.Fatalf("a refused departure created %d roster person(s)", len(peopleAfter)-len(peopleBefore))
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 {
		t.Fatalf("a refused departure checked %d person(s) out", 2-len(open))
	}
	if !strings.Contains(body, "Crew 00") {
		t.Errorf("the refused page lost the rest of the list:\n%s", body)
	}
}

// TestTheInboundFormAddsAnOffRosterPassenger is the positive control for
// the rule above: the SAME typed-name case, in the other direction, must
// still add the person. Without this, "outbound refuses unknown names"
// could be satisfied by refusing them everywhere, which would break the
// gate's documented job.
func TestTheInboundFormAddsAnOffRosterPassenger(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	_, vals := gateBuild(t, srv, gateInbound, "TRK-14", []string{"Dana Driver", "Off Roster Ozzy"}, 0)
	// ⟦R-26⟧ this test is about off-roster ADD-ON-DEMAND, not company — give
	// the new passenger one so the commit is not refused for the reason
	// R-26 added.
	gateFillNewRowCompanies(t, vals, "R-26 Test Co")
	rec := postForm(t, srv, "/gate/checkin", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("inbound with an off-roster passenger = %d: %s", rec.Code, rec.Body.String())
	}
	people, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range people {
		if p.Name == "Off Roster Ozzy" {
			found = true
		}
	}
	if !found {
		t.Fatal("an off-roster arrival did not create the person — a person who is physically here and on no list is the failure that matters")
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 {
		t.Fatalf("%d people on the muster, want 2", len(open))
	}
}

// TestOutboundCanRecordARunToAPadWithADriver. The pad is the destination
// where "who was driving" matters most: there is no gate at the far end, no
// officer, and no timer, so the trip record is the only thing that will
// ever say who took the vehicle out there.
func TestOutboundCanRecordARunToAPadWithADriver(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	checkInCrew(t, srv, st, 3)
	_, vals := gateBuildOutbound(t, srv, "PAD RUNNER 1", []string{visitIDForCrewName(t, st, "Crew 00")}, []string{"Crew 01"}, 1)
	vals.Set("destination_id", "conneco")
	rec := postForm(t, srv, "/gate/checkout/vehicle", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("pad run = %d: %s", rec.Code, rec.Body.String())
	}
	outAt, err := domain.OutAtUncontrolled(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(outAt) != 1 || len(outAt[0].Persons) != 2 {
		t.Fatalf("out-at-pad panel = %+v, want one run with two people", outAt)
	}
	trips, err := domain.Trips(st)
	if err != nil {
		t.Fatal(err)
	}
	var run *domain.Trip
	for i := range trips {
		// ⟦LOCK — W7 seam repair, 2026-09-03⟧ ⟦was: "if trips[i].VehicleEntered == \"PAD RUNNER 1\" {"⟧
		if trips[i].VehicleEntered != nil && *trips[i].VehicleEntered == "PAD RUNNER 1" {
			run = &trips[i]
		}
	}
	if run == nil || run.DestinationUncontrolled == nil || *run.DestinationUncontrolled != "conneco" {
		t.Fatalf("the pad run was not recorded against the pad: %+v", run)
	}
	driver, err := domain.PersonByID(st, run.DriverPersonID)
	if err != nil {
		t.Fatal(err)
	}
	if driver == nil || driver.Name != "Crew 01" {
		t.Fatalf("the pad run records driver %v, want the flagged Crew 01 — at a pad with no gate and no timer this record is the only thing that will ever say who drove", driver)
	}
	// A pad REFUSES a deadline; the shared destination resolution must not
	// have quietly dropped that rule on this surface.
	if run.DeadlineMinutes != nil {
		t.Errorf("the pad run carries a deadline of %d minutes — nobody there can record the arrival that would close it, so a timer could only ever raise a false alarm", *run.DeadlineMinutes)
	}
}

// TestOutboundToAControlledDestinationStillRequiresADeadline. The gate form
// shares applyDispatchDestination with the operator console, so the rules
// the console enforces must be enforced here too — the whole reason for
// sharing it is that two copies would be two chances for one surface to
// author a trip the other would refuse.
func TestOutboundToAControlledDestinationStillRequiresADeadline(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	checkInCrew(t, srv, st, 2)
	_, vals := gateBuildOutbound(t, srv, "CONVOY 1", []string{visitIDForCrewName(t, st, "Crew 00")}, []string{"Crew 01"}, 0)
	vals.Set("destination_id", "site-b")

	rec := postForm(t, srv, "/gate/checkout/vehicle", vals)
	if rec.Code != http.StatusConflict {
		t.Fatalf("a controlled departure with no deadline = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if got := vehicleError(t, rec.Body.String(), gateOutbound); !strings.Contains(got, "requires a deadline") {
		t.Errorf("the refusal does not explain: %q", got)
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 {
		t.Fatal("a refused departure checked people out anyway")
	}

	vals.Set("deadline_minutes", "90")
	rec = postForm(t, srv, "/gate/checkout/vehicle", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("controlled departure with a deadline = %d: %s", rec.Code, rec.Body.String())
	}
	open, err = domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 0 {
		t.Fatalf("%d people still on site after the whole crew left", len(open))
	}
}

// TestTheGatePageOffersOneVehicleAtATime. With scripting off the
// accumulated list lives in the submitted form's own fields, so a POST from
// one direction's form carries nothing belonging to the other: touching the
// departing form's Add button with twenty arrivals accumulated would
// silently discard all twenty. The page therefore renders the other
// direction as a pointer rather than a live form while one is in progress.
// One truck is at the window at a time; this makes the page say so.
// ⟦R-28, team-lead, 2026-09-16⟧ RETARGETED, not weakened: the departing
// form no longer lives on a fresh /gate/vehicle load at all — it only ever
// appears via the roster's own tick-and-select door (gate_roster_half),
// so "a fresh page offers both" is no longer the precondition to state.
// What is UNCHANGED is the reason this test exists: with scripting off the
// accumulated list lives in the submitted form's own fields, so an
// arrival part-entered above must not leave the roster's tick/select door
// live underneath it — touching that door mid-arrival would compete for
// the same accumulate state. gate_roster_half's own
// `{{else if eq .InProgress "in"}}` branch (added for R-28) is what now
// withholds the roster's tick form in exactly that window; this test
// covers it end to end rather than trusting the branch exists.
func TestTheGatePageOffersOneVehicleAtATime(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	_, page := getGate(t, srv)
	// Precondition: a fresh page offers the arriving form AND the roster's
	// own tick/select door — "withheld below" is a consequence of the
	// accumulation, not of the roster door never having been there.
	gateFormMarkup(t, page, formIDFor(gateInbound))
	if !strings.Contains(page, `id="checkout-picker"`) {
		t.Fatalf("precondition: a fresh page has no roster tick/select door to withhold later:\n%s", page)
	}

	page = gateSetVehicle(t, srv, page, gateInbound, "TRK-14")
	page = gateAdd(t, srv, page, gateInbound, "Dana Driver")
	if strings.Contains(page, `id="checkout-picker"`) || strings.Contains(page, `id="vehicle-form-out"`) {
		t.Fatalf("the roster's own tick/select door (or the departing vehicle form it leads to) is live while an arrival is part-entered above. Ticking it — or adding to a departing form — would compete for the same accumulate state with no cue:\n%s", page)
	}
	if !strings.Contains(page, "part-entered above") {
		t.Errorf("the page does not say why the roster's tick/select door is unavailable:\n%s", page)
	}
	// Committing frees it again.
	vals := gateFormValues(t, page, formIDFor(gateInbound))
	vals = gateMarkDriver(vals, 0)
	if rec := postForm(t, srv, "/gate/checkin", vals); rec.Code != http.StatusSeeOther {
		t.Fatalf("commit = %d: %s", rec.Code, rec.Body.String())
	}
	_, page = getGate(t, srv)
	if !strings.Contains(page, `id="checkout-picker"`) {
		t.Fatalf("the roster's tick/select door did not come back once the arrival committed:\n%s", page)
	}
}

// TestTheIndividualCheckOutPickerStillWorksBesideTheVehicleForm. The two
// coexist by ruling: the picker handles an individual or off-vehicle
// departure and cannot record a driver; the vehicle form records a crew
// leaving in a vehicle and cannot express "one person walked off". Neither
// is a substitute for the other, so adding one must not have removed the
// other.
func TestTheIndividualCheckOutPickerStillWorksBesideTheVehicleForm(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	ids := checkInCrew(t, srv, st, 3)
	_, page := getGate(t, srv)
	if !strings.Contains(page, `id="checkout-picker"`) {
		t.Fatal("the individual check-out picker is gone from the gate page")
	}
	rec := postForm(t, srv, "/gate/checkout", url.Values{"time_source": {"default"}, "visit_ids": {ids[0]}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("picker check-out = %d: %s", rec.Code, rec.Body.String())
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 {
		t.Fatalf("%d people on site after one individual check-out, want 2", len(open))
	}
	// It still emits one event per person — the batching is of the request,
	// never of the events.
	checkouts, err := domain.VisitCheckouts(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(checkouts) != 1 {
		t.Fatalf("%d visit_checked_out rows for one departure, want 1", len(checkouts))
	}
}

// TestTripsDetailNamesTheDriverOfAnArrivedTrip closes the finding that the
// driver flag was unreachable through the UI the moment a trip arrived.
//
// It was rendered in exactly four places, and every one of them was
// in-flight or crashed: the inbound board (open expectations), the alert
// rows (open trips with a live deadline), /gate's incomplete check-ins and
// the muster's MAY BE INCOMPLETE table (crashed check-ins). The only
// recovery for an ARRIVED trip was GET /debug/projection-dump — raw JSON,
// where driver_person_id is an opaque id that has to be hand-joined against
// the same dump's person section. An incident report is written days later
// by somebody who will not do that. D-56 has since removed that route
// entirely, so this column is not a convenience over the dump any more; it
// is the only way to answer the question.
func TestTripsDetailNamesTheDriverOfAnArrivedTrip(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	_, vals := gateBuild(t, srv, gateInbound, "TRK-14", []string{"Dana Driver", "Pat Passenger"}, 1)
	if rec := postForm(t, srv, "/gate/checkin", vals); rec.Code != http.StatusSeeOther {
		t.Fatalf("check-in = %d: %s", rec.Code, rec.Body.String())
	}
	// Precondition (rule 6): the trip really is ARRIVED, which is the state
	// in which the driver used to disappear. A test run against an open
	// trip would pass for the wrong reason.
	states, err := domain.TripStates(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].State == domain.TripStateOpen {
		t.Fatalf("precondition: trip states %+v — this test needs an ARRIVED trip", states)
	}

	body := styleRe.ReplaceAllString(getTripsDetail(t, srv), "")
	tableStart := strings.Index(body, "All trips known here")
	if tableStart < 0 {
		t.Fatal("no trip table on /trips/detail")
	}
	table := body[tableStart:]
	if end := strings.Index(table, "</table>"); end > 0 {
		table = table[:end]
	}
	if !strings.Contains(table, "<th>Driver</th>") {
		t.Fatalf("the historical trip table has no driver column:\n%s", table)
	}
	if !strings.Contains(table, "Pat Passenger") {
		t.Fatalf("the arrived trip's row does not name who was driving. The flag exists for the report written after an incident, and until it is on this page the only way to read it is a raw JSON dump with a hand-joined person id:\n%s", table)
	}
	if strings.Contains(table, "Dana Driver") {
		t.Errorf("the trip table names a passenger who was NOT flagged as the driver:\n%s", table)
	}
}

// TestAnOutboundVehicleNeverLooksLikeACrashedCheckIn.
//
// THE DEFECT THIS LOCKS, found by driving the finished form in a browser
// rather than by reasoning about it. A trip with no destination, no
// deadline and no arrival is BYTE-IDENTICAL to a check-in that died
// between its two events: domain.IncompleteCheckIns selects exactly that
// shape. So a destination-less departure appears on /gate under "Incomplete
// check-ins — needs attention", with a Resume button that would check the
// departed crew back ONTO this muster in one click, and on the printed
// muster as MAY BE INCOMPLETE — on every routine departure, six days a
// week. That is alarm fatigue on the one warning that stops a sheet being
// silently short, and F1's box is supposed to mean a crash and nothing
// else.
//
// The gate therefore refuses a departure with no destination, and this test
// asserts both halves: the refusal, and that a departure WITH one leaves
// the warning silent.
func TestAnOutboundVehicleNeverLooksLikeACrashedCheckIn(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	checkInCrew(t, srv, st, 3)

	// Precondition (rule 6): the warning is genuinely quiet before any of
	// this, so "quiet afterwards" is a consequence and not the fixture.
	inc, err := domain.IncompleteCheckIns(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(inc) != 0 {
		t.Fatalf("precondition: %d incomplete check-ins before the departure", len(inc))
	}

	_, vals := gateBuildOutbound(t, srv, "TRK-14", []string{visitIDForCrewName(t, st, "Crew 00")}, []string{"Crew 01"}, 0)
	rec := postForm(t, srv, "/gate/checkout/vehicle", vals) // no destination
	if rec.Code != http.StatusConflict {
		t.Fatalf("a departure with no destination = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if got := vehicleError(t, rec.Body.String(), gateOutbound); !strings.Contains(got, "where this vehicle is going") {
		t.Errorf("the refusal does not ask the question: %q", got)
	}
	inc, err = domain.IncompleteCheckIns(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(inc) != 0 {
		t.Fatalf("the refused departure authored a trip that now reads as a crashed check-in: %+v", inc)
	}

	// With a destination it goes through, and the warning stays silent.
	vals.Set("destination_id", "conneco")
	rec = postForm(t, srv, "/gate/checkout/vehicle", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("departure to a pad = %d: %s", rec.Code, rec.Body.String())
	}
	inc, err = domain.IncompleteCheckIns(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(inc) != 0 {
		t.Fatalf("a routine departure is listed as an incomplete check-in: %+v\nThe gate would offer to Resume it, which puts the departed crew back on the muster in one click.", inc)
	}
	_, page := getGate(t, srv)
	if strings.Contains(page, "Incomplete check-ins") {
		t.Error("/gate offers to resume a routine departure as if it were a crashed check-in")
	}
	musterRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(musterRec, httptest.NewRequest("GET", "/muster", nil))
	if strings.Contains(musterRec.Body.String(), "MAY BE INCOMPLETE") {
		t.Error("the muster reads MAY BE INCOMPLETE after a routine departure — alarm fatigue on the one warning that stops a sheet being silently short")
	}
}

// TestTheOutboundFormIsWithheldWhereNoDestinationIsConfigured. The rule
// above has a consequence: a post with no destinations in its config cannot
// record a departing vehicle as a trip at all. That must be SAID, not
// discovered by an operator filling the form in and being refused.
//
// D-4 CHANGED WHAT IT MUST SAY. This test used to require the notice to
// point at the ticked-list check-out as "the surface that still works" for
// a departure. Under D-4 there is no destination-less exit at all — the
// ticked list corrects a mistaken check-in and records no departure — so
// pointing an operator at it when they are trying to record a truck leaving
// is now an invitation to author exactly the thing the ruling removed. The
// assertion is inverted rather than dropped: the notice must state that
// departures cannot be recorded here, and must NOT offer the ticked list as
// a substitute.
func TestTheOutboundFormIsWithheldWhereNoDestinationIsConfigured(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	checkInCrew(t, srv, st, 2)
	srv.Cfg.Destinations = nil
	srv.Cfg.Peers = nil // DestinationList() falls back to the peer list
	if len(srv.Cfg.DestinationList()) != 0 {
		t.Fatalf("precondition: %d destinations still configured", len(srv.Cfg.DestinationList()))
	}
	// ⟦R-28⟧ RETARGETED: the departing form is reached via the roster's own
	// tick-and-select door now, not present on a fresh page — see
	// gateBuildOutbound's own comment.
	page, _ := gateDepartSelected(t, srv, []string{visitIDForCrewName(t, st, "Crew 00")})
	markup := gateFormMarkup(t, page, formIDFor(gateOutbound))
	if strings.Contains(markup, `name="destination_id"`) {
		t.Error("a destination control is offered with nothing to choose from")
	}
	if !strings.Contains(markup, "No destinations are configured") {
		t.Errorf("the departing form does not say why it cannot record a trip here:\n%s", markup)
	}
	// Whitespace-collapsed, because the sentences below are wrapped in the
	// template and a literal match would break on a re-indent rather than on
	// the meaning changing.
	flat := strings.Join(strings.Fields(markup), " ")
	if !strings.Contains(flat, "no departure can be recorded here at all") {
		t.Errorf("the notice does not say that departures cannot be recorded at this post:\n%s", markup)
	}
	if !strings.Contains(flat, "not</em> a substitute") {
		t.Errorf("the notice does not rule the ticked list out as a substitute — an operator sent there takes a person off the muster with no vehicle trail:\n%s", markup)
	}
}
