// gate_onsite_test.go — the PRESENCE check on the departing commit, and the
// deliberate absence of one on the arriving commit.
//
// WHY THIS FILE EXISTS. James asked for the two direction sections to become
// one form with two buttons, Check in and Check out. That removes a decision
// the operator has to make before typing, and it introduces one they can now
// get wrong at the last moment: two adjacent buttons with OPPOSITE
// consequences on the muster. The thing that makes that safe is not the
// button layout — it is that the server knows who is on site and can refuse a
// commit that contradicts it.
//
// The two directions are derived SEPARATELY here, and they come out
// DIFFERENT. That is the point of deriving them separately.
package web

import (
	"net/http"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// TestCheckingOutSomebodyWhoIsNotOnSiteIsRefusedByName.
//
// REMOVE direction. A departure recorded for somebody who never arrived is a
// record with no arrival behind it — and to a destination with a gate it also
// creates an inbound expectation with a deadline running against a person
// nobody has ever seen. Nothing downstream would question it.
//
// This is NOT covered by the existing off-roster refusal
// (TestOutboundRefusesAPassengerWhoIsNotOnTheRoster): that one catches a name
// the roster does not know, i.e. a typo. This one catches a real, correctly
// spelled, fully enrolled person who is simply not here — which is exactly
// what a mis-click on an ARRIVING vehicle looks like.
func TestCheckingOutSomebodyWhoIsNotOnSiteIsRefusedByName(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	checkInCrew(t, srv, st, 2) // Crew 00 and Crew 01 are on site, order N/A below

	// The absentee is a fully enrolled roster person who was never checked
	// in — so the off-roster refusal cannot fire and this test poses its own
	// case rather than a narrower one.
	absent, err := domain.CreatePerson(st, "site-a", 1, "Absent, Alex", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 {
		t.Fatalf("precondition: %d people on site, want 2", len(open))
	}
	for _, v := range open {
		if v.PersonID == absent.PersonID {
			t.Fatal("precondition: the absentee holds an open visit, so this test cannot pose its case")
		}
	}
	tripsBefore, err := domain.Trips(st)
	if err != nil {
		t.Fatal(err)
	}

	_, vals := gateBuildOutbound(t, srv, "TRK-14", []string{visitIDForCrewName(t, st, "Crew 00")}, []string{"Absent, Alex"}, 0)
	vals.Set("destination_id", "conneco")
	rec := postForm(t, srv, "/gate/checkout/vehicle", vals)

	if rec.Code == http.StatusSeeOther {
		t.Fatal("a departing vehicle named somebody who is not on site and it was accepted — that records a departure with no arrival behind it")
	}
	if rec.Code != http.StatusConflict {
		t.Errorf("outbound naming an absent person = %d, want 409", rec.Code)
	}
	// The refusal NOTICE, not the page: the departing form's own help text
	// mentions checking people in, so a whole-body scan would pass even with
	// the refusal removed.
	msg := vehicleError(t, rec.Body.String(), gateOutbound)
	if !strings.Contains(msg, "Absent, Alex") {
		t.Errorf("the refusal does not name who is not on site: %q", msg)
	}
	// It names ONLY who is actually absent. A refusal that recites the whole
	// passenger list is not selecting on presence at all, and it makes the
	// operator hunt for the one name that matters.
	if strings.Contains(msg, "Crew 00") {
		t.Errorf("the refusal names somebody who IS on site, so it is not selecting on presence: %q", msg)
	}
	if !strings.Contains(msg, "check-in") {
		t.Errorf("the refusal does not point at the other button, which is the likely intent: %q", msg)
	}

	trips, err := domain.Trips(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != len(tripsBefore) {
		t.Errorf("a refused departure authored %d trip(s), want 0", len(trips)-len(tripsBefore))
	}
	after, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 2 {
		t.Errorf("a refused departure changed the on-site list: %d open visits, want the 2 there were", len(after))
	}
}

// TestCheckingOutAVehicleOfPeopleWhoAreAllOnSiteStillWorks is the positive
// control. Without it, "outbound refuses absent people" would be satisfied by
// a handler that refuses every departure.
func TestCheckingOutAVehicleOfPeopleWhoAreAllOnSiteStillWorks(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	checkInCrew(t, srv, st, 3)

	_, vals := gateBuildOutbound(t, srv, "TRK-14", []string{visitIDForCrewName(t, st, "Crew 00")}, []string{"Crew 01"}, 0)
	vals.Set("destination_id", "conneco")
	rec := postForm(t, srv, "/gate/checkout/vehicle", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("a departure of people who are all on site = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	after, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 {
		t.Errorf("%d people left on site after two of three departed, want 1", len(after))
	}
}

// TestCheckingInSomebodyAlreadyOnSiteIsAcceptedAndLeavesOneVisit.
//
// ADD direction, derived on its own and coming out the OTHER WAY. Pressing
// Check in on a vehicle whose people are already on site is benign: the
// domain closes each occupant's prior open visit and opens a replacement
// (domain.GateCheckIn's own comment calls it "the routine stale-visit
// correction"). No duplicate visit exists to prevent, the person is on the
// muster before and after, and refusing it would block a correction operators
// actually make. So there is no matching refusal — and this test pins that
// the outcome really is one visit, not two, so the absence of the refusal
// rests on a measured property rather than on an argument.
func TestCheckingInSomebodyAlreadyOnSiteIsAcceptedAndLeavesOneVisit(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	checkInCrew(t, srv, st, 2)
	before, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 2 {
		t.Fatalf("precondition: %d people on site, want 2", len(before))
	}

	_, vals := gateBuild(t, srv, gateInbound, "TRK-14", []string{"Crew 00", "Crew 01"}, 0)
	rec := postForm(t, srv, "/gate/checkin", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("re-checking in people who are already on site = %d, want 303 — this is a correction, not an error: %s",
			rec.Code, rec.Body.String())
	}

	after, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 2 {
		t.Errorf("%d open visits after re-checking in 2 people who were already on site, want 2 — a second visit each would double them on the muster", len(after))
	}
	seen := map[string]int{}
	for _, v := range after {
		seen[v.PersonID]++
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("person %s holds %d open visits, want 1", id, n)
		}
	}
}
