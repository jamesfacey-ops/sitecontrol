// gate_row_retype_test.go — ⟦F-L03-R3-1, 2026-09-30⟧ a /gate/vehicle row
// RETYPED IN PLACE, with scripting on, to another roster person's exact name
// resolves to that person, exactly as the scripting-off form does.
//
// THE DEFECT (lanes-r3/L03.md, pre-existing in 9a2fb30): the row's resolver
// only honours a click-pick, so the retyped text resolved 'new' and the
// commit posted kind=new with the name — rowPersonSlot's DELIBERATE
// off-roster shape. At the ice road that minted a duplicate of a person on
// site elsewhere and checked the copy in; at the pad it was refused as
// "not on the roster". Scripting off, the same text resolves to the person.
//
// Two halves, as for every scripted-path fix in this package (no JS runtime
// here, by decision — gate_adopt_confirm_test.go's header):
//
//  1. the SCRIPT's shape: a 'new' row that was not added as a new person
//     posts the served default (kind=existing, no id) so the server
//     resolves its text; only a row still reading the name it was added
//     under as a new person posts kind=new. RED on 6490fbd.
//  2. the SERVER's answer to that body: the exact name is that person —
//     checked in, nobody minted, their record untouched, even when the row's
//     pickers still hold what the officer set for the row's previous
//     occupant (rehydrateStaleRows now re-derives them from the record, as
//     decorateRow does for a scripting-off add). RED on 6490fbd (refused
//     over the stale pickers). At an orientation post the same body enters
//     a holder instead of refusing "not on the roster".
// The browser half was driven live at the ice road and the pad.
package web

import (
	"net/http"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

func TestGateVehicleScriptPostsARetypedRowForServerResolution(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	_, page := getGate(t, srv)
	for _, want := range []string{
		"pick.kind === 'new' && row._offRosterAs !== pick.name",
		"kindField.value = 'existing'; valueField.value = '';",
		"listEl.lastElementChild._offRosterAs = addedAsNew;",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the served /gate/vehicle script lacks %q — a row retyped to a roster name is posted as a deliberate off-roster add", want)
		}
	}
}

func TestGateRetypedRowWithNoIdResolvesToTheExactRosterPerson(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	dana, chris := persons[0], persons[2]
	aurora, err := domain.CreateCompany(st, "site-a", 1, "Aurora Mechanical", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	before, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if countPersonsNamed(t, before, chris.Name) != 1 {
		t.Fatalf("precondition: exactly one %q on the roster", chris.Name)
	}
	// Row 1 was added as a new person with Aurora picked, then retyped in
	// place to Chris Crew's exact name: the script now posts no id.
	retyped := passengerRow{Typed: chris.Name, Kind: "existing", Value: "", CompanyID: aurora.CompanyID}
	body := scriptedCheckInBody("TRK-81", 0, []passengerRow{knownRow(dana), retyped})
	rec := postForm(t, srv, "/gate/checkin", body)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("retyped-row commit = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	after, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("roster %d -> %d: the retyped exact name minted a person", len(before), len(after))
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	on := false
	for _, v := range open {
		on = on || v.PersonID == chris.PersonID
	}
	if !on {
		t.Fatalf("%s is not on site — the exact name did not resolve to them", chris.Name)
	}
	now, _ := domain.PersonByID(st, chris.PersonID)
	if now == nil || now.CompanyID == nil || *now.CompanyID != *chris.CompanyID {
		t.Errorf("%s's company changed — the previous occupant's picker was applied to the record", chris.Name)
	}
}

func TestGateRetypedRowWithNoIdEntersAnOrientedPersonAtAnOrientationPost(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	count := omodelPersonCount(t, w)
	body := scriptedCheckInBody("TRK-82", 0, []passengerRow{
		{Kind: "existing", Value: w.driver.PersonID, Typed: w.driver.Name},
		{Kind: "existing", Value: "", Typed: w.orla.Name},
	})
	rec := postForm(t, w.srv, "/gate/checkin", body)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("retyped-row commit at an orientation post = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	if !orientBlockOpenPersonIDs(t, w.st)[w.orla.PersonID] {
		t.Fatal("the oriented person named by the retyped row was not entered")
	}
	if got := omodelPersonCount(t, w); got != count {
		t.Fatalf("roster %d -> %d", count, got)
	}
}
