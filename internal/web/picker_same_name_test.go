package web

// picker_same_name_test.go — two tick-boxes carrying one name.
//
// THE DEFECT. buildOnSiteRows renders one visible string per open visit —
// the person's name — while check-out submits a visit id the operator never
// sees. Two open visits under two person ids sharing a name, company and
// trade therefore drew two BYTE-IDENTICAL tick-boxes. Tick the wrong one and
// the wrong visit closes; OpenVisitsAt and MusterLive both filter on
// is_open=1, so a person who is physically on site leaves the evacuation
// muster with no warning, no trace and no reopen path in this module. Two
// person ids for one human is ordinary here: resolvePersonSlot mints a fresh
// id whenever an officer picks "add new" at the gate, with no match check.
//
// EVERY ASSERTION HERE IS ON THE RENDERED PICKER — the person_picker
// partial executed for real, or the gate and console pages served for real
// — never on a Go field. The label an officer reads is composed in the
// template, and a Go-level assertion cannot see what the template does
// with it. It is also what lets this file compile, and be watched failing,
// on the tree before the repair.
//
// What is pinned:
//   - the safety property: within one company/trade subgroup, no two
//     tick-boxes read the same;
//   - the two cases the first version of this repair could not see: person
//     ids sharing a short tail (a fixed 4-hex tag renders that pair
//     identical again), and names the gate itself already treats as equal —
//     gate.go's resolveTypedPerson folds with TrimSpace + EqualFold, and a
//     browser collapses inner whitespace — which a byte-exact key missed;
//   - the non-alarm: a unique name is NOT decorated and NO note is rendered.
//     A marker that fires on every row trains officers to ignore it;
//   - the submitted value is untouched: the bare visit id, under visit_ids;
//   - ⭐ THE NOTE. A person-id tail tells an officer nothing they can choose
//     BY. The deliverable is the sentence at the subgroup that says what a
//     pair means, that it is never "two records — tick both", and that when
//     unsure the safe action is to tick neither. An untested note is the
//     whole repair untested, so it is asserted on the served gate page and
//     on every console picker that can show a pair.
//
// ⚠ WHAT IS NOT CLAIMED. The duplicate is made VISIBLE and the officer is
// told which way the safe error lies. Nothing here closes the defect. The
// discriminator an officer could choose BY — the check-in time — is
// available with no schema change and is the plan owner's choice; see
// refSameNameRows in picker.go for where it drops in. It would not retire
// this file: everyone on one arrival shares one time, so a pair off one
// vehicle still needs the note and the fallback tag pinned here.

import (
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// renderPicker executes the real person_picker partial over one picker.
func renderPicker(t *testing.T, p personPicker) string {
	t.Helper()
	var sb strings.Builder
	if err := tmpl.ExecuteTemplate(&sb, "person_picker", p); err != nil {
		t.Fatalf("person_picker: %v", err)
	}
	return sb.String()
}

// checkOutPickerOf is the check-out picker's shape from gate.go, over the
// given on-site rows, with nothing else set: these tests are about the rows
// and the subgroup, not the safe-default sentence.
func checkOutPickerOf(rows []onSiteRow) personPicker {
	return personPicker{
		FieldName: "visit_ids", Mode: "checkbox", IDPrefix: "checkout-picker",
		Sections: []pickerSection{section("", "", false, onSiteGroups(rows, pickByVisit))},
	}
}

// renderedSubgroups splits picker markup into its company/trade subgroups
// and parses each one's rows with the package's own row instrument.
func renderedSubgroups(markup string) [][]renderedRow {
	var out [][]renderedRow
	for _, sub := range strings.Split(markup, `<div class="picker-sub">`)[1:] {
		var rows []renderedRow
		for _, m := range pickerRowRe.FindAllStringSubmatch(sub, -1) {
			rows = append(rows, renderedRow{m[1], m[2], m[3], m[4], m[5]})
		}
		out = append(out, rows)
	}
	return out
}

// assertNoTwoBoxesReadTheSame is the safety property. wantRows guards the
// fixture: a test that silently stops exercising its own case proves
// nothing.
func assertNoTwoBoxesReadTheSame(t *testing.T, subs [][]renderedRow, wantRows int) {
	t.Helper()
	total := 0
	for i, rows := range subs {
		seen := map[string]string{}
		for _, r := range rows {
			total++
			if prev, dup := seen[r.name]; dup {
				t.Errorf("subgroup %d: two tick-boxes read IDENTICALLY as %q (values %q and %q). "+
					"The operator cannot tell them apart; ticking the wrong one removes a person who "+
					"is still on site from the evacuation muster, with no undo", i, r.name, prev, r.value)
			}
			seen[r.name] = r.value
		}
	}
	if total != wantRows {
		t.Fatalf("fixture did not survive rendering: %d rows rendered, want %d", total, wantRows)
	}
}

// twoOpenVisitsOneName is the shape the old system produced 45 times over:
// one human, two person ids, both on site, one company and trade. The ids
// have the shape ids.go mints — <site>-<kind>-<16 hex> — not a hand-made
// short form, so a tag derived from them is derived from something real.
func twoOpenVisitsOneName() []onSiteRow {
	return []onSiteRow{
		{VisitID: "site-a-visit-8f2a41c9d0e6b371", PersonID: "site-a-person-5b0c9e2f7a13d846", Person: "Robert Stone", Company: "Acme Ice Services", Trade: "Driver"},
		{VisitID: "site-a-visit-3d7b06e5a9c2f180", PersonID: "site-a-person-c47e1a9b6d02f5e3", Person: "Robert Stone", Company: "Acme Ice Services", Trade: "Driver"},
	}
}

// TestTwoOpenVisitsWithOneNameNeverDrawIdenticalTickBoxes is the safety
// property on the rendered check-out picker.
func TestTwoOpenVisitsWithOneNameNeverDrawIdenticalTickBoxes(t *testing.T) {
	markup := renderPicker(t, checkOutPickerOf(twoOpenVisitsOneName()))
	assertNoTwoBoxesReadTheSame(t, renderedSubgroups(markup), 2)
}

// TestPersonIDsSharingAShortTailAreStillToldApart is the case a fixed-width
// tag cannot see. Ids are 16 random hex characters, so two of them sharing
// a 4-character tail is 1 in 65,536 per pair — rare, but a tag that renders
// that pair identical has re-created the defect it exists to make visible.
// Both the person ids AND the visit ids here share their last SIX
// characters, so no fixed tail of four, five or six tells them apart.
func TestPersonIDsSharingAShortTailAreStillToldApart(t *testing.T) {
	rows := []onSiteRow{
		{VisitID: "site-a-visit-0000000000abe5a7", PersonID: "site-a-person-0000000000abe5a7", Person: "Robert Stone", Company: "Acme Ice Services", Trade: "Driver"},
		{VisitID: "site-a-visit-ffffffffffabe5a7", PersonID: "site-a-person-ffffffffffabe5a7", Person: "Robert Stone", Company: "Acme Ice Services", Trade: "Driver"},
	}
	markup := renderPicker(t, checkOutPickerOf(rows))
	assertNoTwoBoxesReadTheSame(t, renderedSubgroups(markup), 2)
}

// TestNamesTheGateAlreadyTreatsAsEqualAreACollision pins the key. The gate
// resolves a typed name with TrimSpace + EqualFold (gate.go,
// resolveTypedPerson) and a browser collapses inner whitespace, so "Robert
// stone" and "Robert  Stone" are one name to the officer and one name to the
// gate. A byte-exact collision key leaves both rows undecorated and renders
// no note, and the pair reads near-identically on screen.
func TestNamesTheGateAlreadyTreatsAsEqualAreACollision(t *testing.T) {
	rows := []onSiteRow{
		{VisitID: "site-a-visit-8f2a41c9d0e6b371", PersonID: "site-a-person-5b0c9e2f7a13d846", Person: "Robert stone", Company: "Acme Ice Services", Trade: "Driver"},
		{VisitID: "site-a-visit-3d7b06e5a9c2f180", PersonID: "site-a-person-c47e1a9b6d02f5e3", Person: "Robert  Stone", Company: "Acme Ice Services", Trade: "Driver"},
	}
	markup := renderPicker(t, checkOutPickerOf(rows))
	subs := renderedSubgroups(markup)
	assertNoTwoBoxesReadTheSame(t, subs, 2)
	for _, sub := range subs {
		for _, r := range sub {
			if !strings.Contains(r.name, "person id") {
				t.Errorf("row %q is undecorated: its name differs from its neighbour's only in case and inner whitespace, "+
					"which the gate folds away and the browser collapses — to the officer these are two boxes with one name", r.name)
			}
		}
	}
	if !strings.Contains(markup, `class="picker-samename"`) {
		t.Errorf("no same-name note rendered for a pair the gate itself treats as one name")
	}
}

// TestUniqueNamesAreNotDecorated is the non-alarm. Two different people
// draw two bare names and no note — a marker on every row is a marker
// nobody reads.
//
// ⚠ This passes on the unrepaired tree too, because that tree decorates
// nothing. It is here to hold the repair to the same silence.
func TestUniqueNamesAreNotDecorated(t *testing.T) {
	rows := []onSiteRow{
		{VisitID: "site-a-visit-8f2a41c9d0e6b371", PersonID: "site-a-person-5b0c9e2f7a13d846", Person: "Robert Stone", Company: "Acme Ice Services", Trade: "Driver"},
		{VisitID: "site-a-visit-3d7b06e5a9c2f180", PersonID: "site-a-person-c47e1a9b6d02f5e3", Person: "Maria Alvarez", Company: "Acme Ice Services", Trade: "Driver"},
	}
	markup := renderPicker(t, checkOutPickerOf(rows))
	for _, sub := range renderedSubgroups(markup) {
		for _, r := range sub {
			if r.name != "Robert Stone" && r.name != "Maria Alvarez" {
				t.Errorf("an unambiguous name was decorated: got %q, want the bare roster name", r.name)
			}
		}
	}
	if strings.Contains(markup, "picker-samename") {
		t.Errorf("a same-name note rendered for a subgroup with no shared name")
	}
}

// TestTheSameNameInADifferentTradeIsNotACollision pins the scope. The
// operator picks inside one company/trade subgroup, so two identical names
// under DIFFERENT headings are already distinguishable and must not be
// decorated — otherwise the signal stops meaning "look closely here".
func TestTheSameNameInADifferentTradeIsNotACollision(t *testing.T) {
	rows := []onSiteRow{
		{VisitID: "site-a-visit-8f2a41c9d0e6b371", PersonID: "site-a-person-5b0c9e2f7a13d846", Person: "Robert Stone", Company: "Acme Ice Services", Trade: "Driver"},
		{VisitID: "site-a-visit-3d7b06e5a9c2f180", PersonID: "site-a-person-c47e1a9b6d02f5e3", Person: "Robert Stone", Company: "Acme Ice Services", Trade: "Scaffolder"},
	}
	markup := renderPicker(t, checkOutPickerOf(rows))
	subs := renderedSubgroups(markup)
	if len(subs) != 2 {
		t.Fatalf("fixture defect: %d subgroups rendered, want 2 (one per trade)", len(subs))
	}
	for _, sub := range subs {
		for _, r := range sub {
			if r.name != "Robert Stone" {
				t.Errorf("name decorated across a trade boundary: %q", r.name)
			}
		}
	}
	if strings.Contains(markup, "picker-samename") {
		t.Errorf("a same-name note rendered across a trade boundary")
	}
}

// TestCheckOutStillSubmitsTheBareVisitID guards what the form posts.
// Whatever the label becomes, the submitted value must remain the bare
// visit id under the unchanged field name.
func TestCheckOutStillSubmitsTheBareVisitID(t *testing.T) {
	fixture := twoOpenVisitsOneName()
	want := map[string]bool{fixture[0].VisitID: false, fixture[1].VisitID: false}
	for _, sub := range renderedSubgroups(renderPicker(t, checkOutPickerOf(fixture))) {
		for _, r := range sub {
			if r.field != "visit_ids" {
				t.Errorf("row submits under %q, want visit_ids", r.field)
			}
			if _, ok := want[r.value]; !ok {
				t.Errorf("submitted value %q is not a bare visit id from the fixture", r.value)
				continue
			}
			want[r.value] = true
		}
	}
	for v, seen := range want {
		if !seen {
			t.Errorf("visit id %q is no longer submitted by any row", v)
		}
	}
}

// --- the served pages -------------------------------------------------

// checkInTwoPeopleCalled puts two roster records with one name, one company
// and one trade on site through the ordinary gate machinery, and returns
// their open visit ids. Nothing in domain refuses this — that is the point.
func checkInTwoPeopleCalled(t *testing.T, st *store.Store, siteID, name string) []string {
	t.Helper()
	ids := createTwoPeopleCalled(t, st, siteID, name)
	if _, _, err := domain.GateCheckIn(st, siteID, 1, domain.GateCheckInInput{
		VehicleID: w7SeedVehicleID(t, st, siteID, "TRK 9 "+name),
		Driver:    domain.PersonSlot{ExistingID: ids[0]},
		Occupants: []domain.PersonSlot{{ExistingID: ids[1]}},
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	open, err := domain.OpenVisitsAt(st, siteID)
	if err != nil {
		t.Fatal(err)
	}
	var visits []string
	for _, v := range open {
		if v.PersonID == ids[0] || v.PersonID == ids[1] {
			visits = append(visits, v.VisitID)
		}
	}
	if len(visits) != 2 {
		t.Fatalf("setup: %d open visits for the pair, want 2", len(visits))
	}
	return visits
}

// createTwoPeopleCalled creates two roster records with one name under one
// company and one trade, and returns their person ids.
func createTwoPeopleCalled(t *testing.T, st *store.Store, siteID, name string) []string {
	t.Helper()
	co, err := domain.CreateCompany(st, siteID, 1, "Acme Ice Services ("+name+")", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	wg, err := domain.CreateTrade(st, siteID, 1, "Driver ("+name+")", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for i := 0; i < 2; i++ {
		p, err := domain.CreatePerson(st, siteID, 1, name, &co.CompanyID, &wg.TradeID, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.PersonID)
	}
	return ids
}

// requireSameNameNote asserts one picker's served markup carries the
// same-name note, and that it says what it must for THIS picker's
// direction. Matched through phraseSeen: the copy is hand-wrapped.
func requireSameNameNote(t *testing.T, pickerID, markup string, must, mustNot []string) {
	t.Helper()
	if !strings.Contains(markup, `class="picker-samename"`) {
		t.Errorf("%s: two boxes carry one name and NO note tells the officer what that means or what to do. "+
			"A tag alone reads as 'two records — tick both', which is the fatal direction", pickerID)
		return
	}
	for _, want := range must {
		if !phraseSeen(markup, want) {
			t.Errorf("%s: the same-name note does not say %q", pickerID, want)
		}
	}
	for _, gone := range mustNot {
		if phraseSeen(markup, gone) {
			t.Errorf("%s: the same-name note says %q, which is the WRONG advice for this picker's direction", pickerID, gone)
		}
	}
}

// TestTheGatePageTellsTheOfficerWhatTwoBoxesWithOneNameMean is the
// deliverable, on the surface an officer actually uses: the served gate
// page, after two roster records with one name have checked in.
func TestTheGatePageTellsTheOfficerWhatTwoBoxesWithOneNameMean(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	visits := checkInTwoPeopleCalled(t, st, "site-a", "Robert Stone")
	_, page := getGate(t, srv)
	markup := pickerMarkup(t, page, "checkout-picker")

	// Both visits are offered, both submit the bare visit id, and the two
	// boxes do not read the same.
	offered := map[string]string{}
	for _, r := range rosterRows(t, page, "checkout-picker") {
		if r.field != "visit_ids" {
			t.Fatalf("check-out row submits under %q, want visit_ids", r.field)
		}
		offered[r.value] = r.name
	}
	for _, v := range visits {
		if _, ok := offered[v]; !ok {
			t.Fatalf("open visit %s is not offered for check-out at all", v)
		}
	}
	if offered[visits[0]] == offered[visits[1]] {
		t.Errorf("the two open visits for 'Robert Stone' render as the IDENTICAL tick-box %q — the operator has nothing to choose by, "+
			"and the wrong choice removes a person who is still on site from the muster", offered[visits[0]])
	}

	// The note: what a pair means, and that the safe action when unsure is
	// to tick neither. This is the sentence the repair exists to render.
	requireSameNameNote(t, "checkout-picker", markup,
		[]string{"tick neither", "a name left on the muster is the safe error"}, nil)

	// The paper procedure agrees: the safe-default sentence — the one
	// TestEveryPickerTellsTheOperatorWhichWayAMissedTickFails holds every
	// picker to — carries the same instruction, so an officer reading only
	// the top of the control gets it too.
	i := strings.Index(markup, `<p class="picker-default">`)
	if i < 0 {
		t.Fatal("the check-out picker has lost its safe-default sentence")
	}
	if def := markup[i : strings.Index(markup[i:], "</p>")+i]; !phraseSeen(def, "tick neither") {
		t.Errorf("the check-out safe-default sentence does not say that, when unsure which of two boxes with one name is the wrong record, the safe action is to tick neither: %q", def)
	}

	// The non-alarm, on the served page: a crew of distinct names renders
	// no note at all.
	srv2, st2, _ := newGateTestServer(t)
	checkInCrew(t, srv2, st2, 3)
	_, page2 := getGate(t, srv2)
	if strings.Contains(pickerMarkup(t, page2, "checkout-picker"), "picker-samename") {
		t.Errorf("the same-name note renders for a crew with no shared name — a note that is always there is a note nobody reads")
	}
}

// TestEveryConsolePickerSaysWhatTwoBoxesWithOneNameMean: the dispatch
// pickers share onSiteGroups with check-out, so they draw the same tagged
// pair, and the arrival picker draws it from the roster. Each must carry a
// note in ITS OWN direction. Dispatch REMOVES people from the muster here,
// so its advice is the check-out advice: when unsure, tick neither. Arrival
// ADDS people, so "tick neither" there is the omission that hides a live
// person — its note must say to still tick one, and must not say neither.
func TestEveryConsolePickerSaysWhatTwoBoxesWithOneNameMean(t *testing.T) {
	srv, st, _ := newTripsTestServer(t)
	checkInTwoPeopleCalled(t, st, "site-b", "Robert Stone") // on site: driver + occupant pickers
	createTwoPeopleCalled(t, st, "site-b", "Maria Alvarez") // roster only: the arrival picker
	page := getTrips(t, srv)

	for _, tc := range []struct {
		id, pair      string
		must, mustNot []string
	}{
		{"driver-picker", "Robert Stone", []string{"pick neither", "a name left on the muster"}, nil},
		{"occupant-picker", "Robert Stone", []string{"tick neither", "a name left on the muster is the safe error"}, nil},
		{"arrival-picker", "Maria Alvarez", []string{"still tick one"}, []string{"tick neither"}},
	} {
		markup := pickerMarkup(t, page, tc.id)
		subs := renderedSubgroups(markup)
		// Fixture guard: the pair this picker is supposed to show is
		// actually in it, so the property below is not vacuously true.
		named := 0
		for _, sub := range subs {
			for _, r := range sub {
				if strings.HasPrefix(r.name, tc.pair) {
					named++
				}
			}
		}
		if named != 2 {
			t.Fatalf("%s: fixture defect — %d rows named %q rendered, want 2", tc.id, named, tc.pair)
		}
		total := 0
		for _, sub := range subs {
			total += len(sub)
		}
		assertNoTwoBoxesReadTheSame(t, subs, total)
		requireSameNameNote(t, tc.id, markup, tc.must, tc.mustNot)
	}
}
