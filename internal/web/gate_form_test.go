// gate_form_test.go — the ONE-VEHICLE-MANY-PASSENGERS gate form
// (design/gate-one-vehicle-many-passengers-v1.md), both directions.
//
// THE INSTRUMENT. Almost everything here drives the form THROUGH ITS OWN
// RENDERED MARKUP: gateFormValues below scrapes a rendered page for exactly
// the fields a browser with no scripting would submit, and the next request
// posts them back. That matters more than it looks. A test that hand-builds
// the POST body proves the HANDLER works and says nothing about whether the
// page it re-rendered can be submitted again — which is the entire
// mechanism the accumulator rests on. If a field stops being emitted, or
// starts being emitted with the wrong name, or an index stops being carried,
// these tests break at the step AFTER the one that broke, which is the
// behaviour a real operator would see.
//
// THE SAFETY ASYMMETRY IS TESTED PER DIRECTION, NEVER THROUGH A SHARED
// HELPER. One widget serves both directions here, which is precisely the
// case design/bulk-movement-v1.md's rule guards: the safe default follows
// whether the action ADDS or REMOVES somebody from the muster, not the
// widget. So the inbound confirmation is asserted to CARRY the muster
// warning and the outbound one to LACK it, in two separate tests with two
// separate derivations written out — swap the two strings in the template
// and both go red.
package web

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// --- the JS-off browser, simulated from the served markup -------------

var (
	gateFormOpenRe = regexp.MustCompile(`(?s)<form[^>]*id="(vehicle-form-(?:in|out))"[^>]*>`)
	inputTagRe     = regexp.MustCompile(`(?s)<input\b[^>]*>`)
	selectBlockRe  = regexp.MustCompile(`(?s)<select\b[^>]*name="([^"]+)"[^>]*>(.*?)</select>`)
	optionTagRe    = regexp.MustCompile(`(?s)<option\b[^>]*>`)
	attrRe         = regexp.MustCompile(`([a-zA-Z0-9_-]+)(?:="([^"]*)")?`)
)

// tagAttrs pulls the attributes out of one tag.
func tagAttrs(tag string) map[string]string {
	out := map[string]string{}
	// Strip the element name so a tag called e.g. <select> cannot be read
	// as an attribute called "select".
	body := tag
	if i := strings.IndexAny(tag, " \t\r\n"); i >= 0 {
		body = tag[i:]
	}
	for _, m := range attrRe.FindAllStringSubmatch(body, -1) {
		out[strings.ToLower(m[1])] = html.UnescapeString(m[2])
	}
	return out
}

// gateFormMarkup returns the raw markup of one vehicle form, style block
// stripped (a class name in a stylesheet is not a rendered element — this
// repo has been bitten by that twice).
func gateFormMarkup(t *testing.T, page, formID string) string {
	t.Helper()
	page = styleRe.ReplaceAllString(page, "")
	loc := gateFormOpenRe.FindAllStringSubmatchIndex(page, -1)
	for _, m := range loc {
		if page[m[2]:m[3]] != formID {
			continue
		}
		rest := page[m[0]:]
		end := strings.Index(rest, "</form>")
		if end < 0 {
			t.Fatalf("form %q is not closed", formID)
		}
		return rest[:end+len("</form>")]
	}
	t.Fatalf("no form with id %q on the page:\n%s", formID, page)
	return ""
}

// gateFormValues is the JS-off browser. It submits every named input in the
// form EXCEPT submit/button controls (which only submit when they are the
// one activated), and radios/checkboxes only when they carry `checked` —
// exactly the browser rule that makes an unchecked driver group submit
// NOTHING, which is why the server needs a separate marker field to tell
// "nobody flagged" apart from "an old-shaped body".
func gateFormValues(t *testing.T, page, formID string) url.Values {
	t.Helper()
	markup := gateFormMarkup(t, page, formID)
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
	for _, m := range selectBlockRe.FindAllStringSubmatch(markup, -1) {
		name, body := m[1], m[2]
		picked := ""
		for _, opt := range optionTagRe.FindAllString(body, -1) {
			a := tagAttrs(opt)
			if _, ok := a["selected"]; ok {
				picked = a["value"]
			}
		}
		// ⟦[[D-84]], 2026-09-05⟧ ⟦was: "vals.Set(name, picked)"⟧ — Add, as a
		// browser does. Set was written when the only <select> on the form
		// was the destination and collapsed every repeated select to ONE
		// value; with a company and a trade picker on every passenger row
		// that posted a 1-long array beside 12-long ones, which is exactly
		// the short case the parity guard refuses — a defect in this
		// instrument, not in the form. A browser submits every select.
		vals.Add(name, picked)
	}
	return vals
}

func formIDFor(direction string) string { return "vehicle-form-" + direction }

// vehicleError returns the text of the REFUSAL NOTICE on one direction's
// form, tags stripped and entities decoded — and nothing else on the page.
//
// It exists because two mutations SURVIVED without it. Asserting a refusal
// by scanning the whole body for its wording passes off page furniture: the
// departing form's own help text contains "check them in", and the gate
// script's disabled-commit reason contains "Mark who was driving", so a
// handler that stopped refusing at all still looked like it was refusing.
// Both mutations are killed by reading the notice itself.
func vehicleError(t *testing.T, page, direction string) string {
	t.Helper()
	markup := gateFormMarkup(t, page, formIDFor(direction))
	m := vehicleErrorRe.FindStringSubmatch(markup)
	if m == nil {
		t.Fatalf("the %q form carries no refusal notice — a refusal the operator cannot read is a refusal that looks like a page that did nothing:\n%s", direction, markup)
	}
	return html.UnescapeString(tagRe.ReplaceAllString(m[1], " "))
}

var (
	vehicleErrorRe = regexp.MustCompile(`(?s)<div class="notice notice-critical vehicle-error">(.*?)</div>`)
	tagRe          = regexp.MustCompile(`<[^>]*>`)
)

// gateAdd posts one "Add person" exactly as a scriptless browser would:
// the form as rendered, plus the open box's typed name. It returns the
// re-rendered page.
func gateAdd(t *testing.T, srv *Server, page, direction, name string) string {
	t.Helper()
	vals := gateFormValues(t, page, formIDFor(direction))
	// ⟦gate-checkin-spec, 2026-09-13⟧ new_typed is gone — the open box is now
	// new_last + new_first (composeName, gate.go). Setting new_last to the
	// WHOLE string and new_first to "" reproduces the byte-identical Typed
	// text every existing caller of this helper already asserts against:
	// composeName(name, "") returns name unchanged (its own "first == ''"
	// branch), so this is not a name split — it is the one composition that
	// is exactly the identity function, chosen so this shared helper does
	// not have to guess a last/first boundary in a test fixture name.
	vals.Set("new_last", name)
	vals.Set("new_first", "")
	rec := postForm(t, srv, "/gate/passenger/add", vals)
	if rec.Code != http.StatusOK {
		t.Fatalf("add %q = %d, want 200 (the accumulate step renders directly; a redirect cannot carry the list forward): %s", name, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// gateSetVehicle types the vehicle id into the in-progress form without
// adding anybody, by rendering one accumulate step with an empty box.
func gateSetVehicle(t *testing.T, srv *Server, page, direction, vehicle string) string {
	t.Helper()
	vals := gateFormValues(t, page, formIDFor(direction))
	vals.Set("vehicle", vehicle)
	rec := postForm(t, srv, "/gate/passenger/add", vals)
	if rec.Code != http.StatusOK {
		t.Fatalf("set vehicle = %d: %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// gateMarkDriver ticks the driver radio on row `idx` of the rendered form,
// the way a click does: the field simply arrives in the next POST.
func gateMarkDriver(vals url.Values, idx int) url.Values {
	vals.Set("driver_index", strconv.Itoa(idx))
	return vals
}

// gateBuild walks a whole vehicle through the served markup and returns the
// final page plus the body a commit would carry.
func gateBuild(t *testing.T, srv *Server, direction, vehicle string, names []string, driver int) (string, url.Values) {
	t.Helper()
	_, page := getGate(t, srv)
	page = gateSetVehicle(t, srv, page, direction, vehicle)
	for _, n := range names {
		page = gateAdd(t, srv, page, direction, n)
	}
	vals := gateFormValues(t, page, formIDFor(direction))
	if got := len(vals["occupant_typed"]); got != len(names) {
		t.Fatalf("precondition: the form carries %d passenger rows after %d adds — the accumulator is not carrying the list forward, so nothing below is measuring what it claims", got, len(names))
	}
	if driver >= 0 {
		vals = gateMarkDriver(vals, driver)
	}
	return page, vals
}

// gateBuildOutbound ⟦R-28, team-lead, 2026-09-16⟧ is gateBuild's own
// outbound counterpart. A fresh /gate/vehicle no longer carries an
// always-present outbound form gateBuild's own getGate+gateSetVehicle
// start could reach directly — the departing form now appears only via
// the roster's own tick-and-select door (gate_roster_half, the SAME one
// the individual page already used: R-28's own spec, "the on-site roster
// with multi-select and CHECK OUT SELECTED -> the same compact confirm
// box"), never a second departure path.
//
// seedVisitIDs are ALL ticked TOGETHER, in one /gate/checkout/select call
// (gateDepartSelected, gate_checkout_select_test.go), landing as rows
// 0..len(seedVisitIDs)-1 in that order; extraNames are then typed into the
// open box exactly as gateAdd/gateBuild already do, appended after. ⚠ a
// SINGLE ticked visit is auto-marked driver
// (handleGateCheckOutSelectForDeparture's own "one name, one candidate"
// rule, R-27) — pass at least two seedVisitIDs for a caller that wants
// NOBODY pre-selected; that is not this helper's own concern to paper
// over, since a caller testing driver selection itself may want exactly
// that auto-mark. `driver` keeps the SAME 0-based meaning across the
// combined list (seedVisitIDs+extraNames) that gateBuild's own `driver`
// parameter always had.
func gateBuildOutbound(t *testing.T, srv *Server, vehicle string, seedVisitIDs, extraNames []string, driver int) (string, url.Values) {
	t.Helper()
	page, _ := gateDepartSelected(t, srv, seedVisitIDs)
	page = gateSetVehicle(t, srv, page, gateOutbound, vehicle)
	for _, n := range extraNames {
		page = gateAdd(t, srv, page, gateOutbound, n)
	}
	vals := gateFormValues(t, page, formIDFor(gateOutbound))
	if got, want := len(vals["occupant_typed"]), len(extraNames)+len(seedVisitIDs); got != want {
		t.Fatalf("precondition: the form carries %d passenger rows (%d ticked + %d adds), want %d — the accumulator is not carrying the list forward, so nothing below is measuring what it claims", got, len(seedVisitIDs), len(extraNames), want)
	}
	if driver >= 0 {
		vals = gateMarkDriver(vals, driver)
	}
	return page, vals
}

// gateFillNewRowCompanies ⟦R-26 test-support, James, 2026-09-13: "the onle
// required information is last name first name and company"⟧ sets a
// company, through the add-new escape, on every accumulated row THIS FORM
// SHOWS AS UNRESOLVED (occupant_value=="") — R-26 refuses a check-in that
// would create a new person with none, and most callers of gateBuild are
// not testing company at all.
//
// ⚠ occupant_value, NOT occupant_kind, is the signal: gateAdd/gateBuild
// drive the ordinary JS-off accumulate route, where a row's `Kind` hidden
// field simply carries forward whatever the open box's own new_kind
// defaulted to ("existing" — gate.html's own served default) REGARDLESS of
// whether the typed name resolved to anybody; identity is decided by
// rowPersonSlot from Value+Typed, not from Kind (see rowPersonSlot's own
// comment: "THE VISIBLE TEXT IS THE AUTHORITY ON IDENTITY"). decorateRow's
// fromRecord/finishKnown is what actually WRITES a non-empty Value, and
// only for a row that resolved to somebody — so an accumulated row with an
// EMPTY occupant_value is exactly and only the unresolved/new case,
// matching what TestGateOffersTheRosterRecordForASloppilyTypedNameAndStillAddsThem
// and its siblings already assert directly (`vals.Get("occupant_value") !=
// ""` is a test failure for a new row).
// occupant_kind="new" is a NARROWER, separate shape (dupadvisory_test.go's
// scriptedCheckInBody, simulating a script that resolved an id and then
// declined it) — this helper does not need it.
//
// KNOWN rows are left untouched: their company already lives on the record
// (decorateRow's fromRecord, run inside gateAdd/gateBuild), and setting
// their picker to the escape would ask for the "change their record"
// confirmation these tests have no reason to give. company is typed
// through the escape (newRef), never picked by id, so it works with no
// fixture company loaded — it resolves by fold to an existing company of
// that name if one exists, or mints a genuinely new one otherwise
// (resolveOrCreateCompanyByName, checkin.go); callers that care which, or
// that assert a company-creation delta, set the row directly instead of
// using this helper.
func gateFillNewRowCompanies(t *testing.T, vals url.Values, company string) {
	t.Helper()
	values := vals["occupant_value"]
	for i, v := range values {
		if v != "" {
			continue
		}
		d84Set(t, vals, "occupant_company_id", i, newRef)
		d84Set(t, vals, "occupant_company", i, company)
	}
}

// --- THE ASSERTION THE WHOLE DESIGN RESTS ON --------------------------

// TestNothingIsWrittenBeforeTheCommit. James: "so then we can go back and
// fix entries before we add the vehicle to the database." Every guarantee
// in this design descends from that one property — mid-entry correction
// needs no correction event, a part-entered bus can never look like a
// committed one, and the muster's MAY BE INCOMPLETE box keeps its original
// meaning — so it is asserted first and directly, on store state, at bus
// scale.
func TestNothingIsWrittenBeforeTheCommit(t *testing.T) {
	const crew = 26
	srv, st, _ := newGateTestServer(t)

	var names []string
	for i := 0; i < crew; i++ {
		names = append(names, fmt.Sprintf("Rider %02d", i))
	}
	_, vals := gateBuild(t, srv, gateInbound, "BUS 7", names, 0)

	// Nothing. Not a trip, not a visit, not a person.
	trips, err := domain.Trips(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 0 {
		t.Fatalf("%d trip(s) exist after %d accumulate steps and no commit — the accumulator wrote to the database", len(trips), crew)
	}
	visits, err := domain.Visits(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(visits) != 0 {
		t.Fatalf("%d visit(s) exist before the commit — nothing may be written until the final button", len(visits))
	}
	people, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 3 { // the fixture's own three, and nobody else
		t.Fatalf("the roster grew to %d before the commit — off-roster passengers were created by an accumulate step", len(people))
	}

	// ⟦R-26⟧ this test is about the all-or-nothing write moment, not
	// company — give the crew one so the commit is not refused for the
	// reason R-26 added.
	gateFillNewRowCompanies(t, vals, "R-26 Test Co")

	// Now the commit, and the whole vehicle appears at once.
	rec := postForm(t, srv, "/gate/checkin", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("commit = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != crew {
		t.Fatalf("%d people on the muster after committing a %d-person bus", len(open), crew)
	}
	trips, err = domain.Trips(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 1 {
		t.Fatalf("%d trips for one bus, want 1 — a bus must not become six entries with six drivers to reconcile", len(trips))
	}
}

// TestJSOffGateCheckInHasNoCrewSizeCap replaces the cap assertions the old
// form needed. There used to be FOUR server-rendered occupant rows and a
// printed instruction to "check the rest in as a second entry", so a
// 26-person bus with scripting off was six separate check-ins, six trips
// and six DriverPersonID values for one vehicle. One box used N times has
// no set of rows to run out of.
func TestJSOffGateCheckInHasNoCrewSizeCap(t *testing.T) {
	const crew = 26
	srv, st, _ := newGateTestServer(t)
	var names []string
	for i := 0; i < crew; i++ {
		names = append(names, fmt.Sprintf("Rider %02d", i))
	}
	_, vals := gateBuild(t, srv, gateInbound, "BUS 7", names, 0)
	// ⟦R-26⟧ this test is about crew-size capacity, not company — give the
	// crew one so the commit is not refused for the reason R-26 added.
	gateFillNewRowCompanies(t, vals, "R-26 Test Co")
	rec := postForm(t, srv, "/gate/checkin", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("26-person JS-off check-in = %d: %s", rec.Code, rec.Body.String())
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != crew {
		t.Fatalf("%d people on the muster, want %d — this is the silent-short-muster direction", len(open), crew)
	}
	// And the page must not still be telling the operator there is a cap.
	_, page := getGate(t, srv)
	if strings.Contains(page, "second entry") {
		t.Error("the gate page still states the four-occupant overflow procedure, which no longer exists")
	}
}

// TestAnAccumulateStepCarriesTheVehicleIDForward is the other half of F43's
// no-default rule, and it exists so the fix for one cannot break the other.
// TestServedVehicleFieldCarriesNoSubmittableDefault asserts the FRESH page
// serves no value= attribute; deleting the attribute entirely would satisfy
// that test and silently drop the vehicle id on every accumulate step,
// leaving an operator to re-type it 26 times or, worse, commit without it.
func TestAnAccumulateStepCarriesTheVehicleIDForward(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	_, page := getGate(t, srv)
	page = gateSetVehicle(t, srv, page, gateInbound, "TRK-14")
	page = gateAdd(t, srv, page, gateInbound, "Dana Driver")

	vals := gateFormValues(t, page, formIDFor(gateInbound))
	if got := vals.Get("vehicle"); got != "TRK-14" {
		t.Fatalf("after an accumulate step the form carries vehicle=%q, want TRK-14", got)
	}
	if !strings.Contains(gateFormMarkup(t, page, formIDFor(gateInbound)), `value="TRK-14"`) {
		t.Error("the vehicle id is not rendered back into the field the operator is looking at")
	}
}

// --- the driver flag ---------------------------------------------------

var driverRadioRe = regexp.MustCompile(`<input[^>]*name="driver_index"[^>]*>`)
var driverSelectRe = regexp.MustCompile(`(?s)<select[^>]*name="driver_index"[^>]*>.*?</select>`)
var driverOptionRe = regexp.MustCompile(`<option[^>]*>`)

// TestNoDriverIsPreSelectedOnTheGateForm is THE GATE'S OWN COPY of the
// pre-selected-driver lock, and it needs to be its own copy:
// TestDriverIsNeverPreSelected (picker_test.go) inspects the /trips picker
// markup and would not notice a defaulted radio here.
//
// The defect it locks: the old dispatch control was a `<select required>`
// with no empty option, and a browser pre-selects the first option of such
// a select — so whoever sorted first was the default driver, and a hurried
// entry attributed a trip to them. A silently defaulted driver is a wrong
// name in an incident report, which is the one thing the flag exists to
// get right.
//
// ⟦R-28, team-lead, 2026-09-16⟧ RETARGETED, not weakened: BOTH directions
// on /gate/vehicle now render the compact `<select class="p-driver-select">`
// instead of per-row radios — "a single Driver dropdown" for arriving
// (R-28's own spec), and the outbound leg is the SAME compact checkout box
// the individual page already used (gate_roster_half). The per-row radio
// still exists in the DOM either way (hidden, never removed — see
// gate_vehicle_form's own comment on why: an occupant_* positional array
// member cannot vanish from a row without shifting every later row's
// fields), so this test checks BOTH shapes: the select for the safe
// default a browser could otherwise supply on its own (a `<select>` MUST
// display something; only an empty leading option with nothing `selected`
// avoids picking one for the officer), and the radios for the same "no
// browser-supplied checked default" property radios have always had.
func TestNoDriverIsPreSelectedOnTheGateForm(t *testing.T) {
	for _, direction := range []string{gateInbound, gateOutbound} {
		t.Run(direction, func(t *testing.T) {
			srv, st, _ := newGateTestServer(t)
			var page string
			if direction == gateOutbound {
				visitIDs := checkInCrew(t, srv, st, 3)
				page, _ = gateBuildOutbound(t, srv, "TRK-14", visitIDs, nil, -1)
			} else {
				page, _ = gateBuild(t, srv, direction, "TRK-14", []string{"Dana Driver", "Pat Passenger", "Chris Crew"}, -1)
			}
			markup := gateFormMarkup(t, page, formIDFor(direction))

			radios := driverRadioRe.FindAllString(markup, -1)
			// Rule 6: the scenario has to actually pose its case. Three
			// passengers are accumulated, so there ARE radios to be wrong.
			if len(radios) != 3 {
				t.Fatalf("precondition: %d driver radios rendered for 3 passengers — this test would pass by finding nothing to check:\n%s", len(radios), markup)
			}
			for _, r := range radios {
				if strings.Contains(r, "checked") {
					t.Fatalf("a driver radio is pre-selected: %s\nA driver nobody chose is the name an incident report gets written from.", r)
				}
				// ⟦stream A1, H-2/H-3⟧ WAS: asserted every radio `required`. Those radios
				// sit in a hidden label, so browser validation could never be satisfied and the
				// form could not be submitted at all. The refusal now comes from the visible
				// required <select> (checked below) and the server.
				if strings.Contains(r, "required") {
					t.Errorf("a hidden driver radio is `required` — the browser blocks submit on a control the user cannot see: %s", r)
				}
				if !strings.Contains(r, `type="radio"`) {
					t.Errorf("the driver control is not a radio: %s\nOnly a radio group can have NO browser-supplied default; a <select> must display something, and displays option one.", r)
				}
			}
			sel := driverSelectRe.FindString(markup)
			if sel == "" {
				t.Fatalf("no compact driver <select> on the %q form:\n%s", direction, markup)
			}
			if !strings.Contains(sel, "required") {
				t.Errorf("the driver select is not `required`: %s", sel)
			}
			for _, opt := range driverOptionRe.FindAllString(sel, -1) {
				if strings.Contains(opt, "selected") {
					t.Fatalf("an option in the driver select is pre-selected: %s\nA driver nobody chose is the name an incident report gets written from.", opt)
				}
			}
			// ⟦R-28⟧ RETARGETED: the driver field now exists TWICE on
			// purpose — the per-row radios (still `name="driver_index"`,
			// still in the DOM, hidden not removed — gate_vehicle_form's
			// own comment explains why: an occupant_* positional array
			// member cannot vanish from a row without shifting every later
			// row's fields) plus the ONE compact select this test already
			// found above. "driver_index appears exactly radios+1 times"
			// is now the precise count — the select adds one, never more.
			if got, want := strings.Count(markup, "driver_index"), len(radios)+1; got != want {
				t.Fatalf("driver_index appears %d times, want %d (%d radios + the one compact select) — some OTHER control carries the driver field", got, want, len(radios))
			}
			// And a scriptless submit with nothing marked authors nothing.
			// A <select>, unlike a radio group, ALWAYS submits something for
			// its name — even "nothing chosen" is the empty-string option's
			// own value — so the precondition here is that value being
			// empty, not the field being absent (which is what the radios
			// alone would give; see the select's own required-with-no-
			// selected-option safe shape, same as the destination select).
			vals := gateFormValues(t, page, formIDFor(direction))
			if got := vals.Get("driver_index"); got != "" {
				t.Fatalf("precondition: an untouched form already submits driver_index=%q", got)
			}
			action := "/gate/checkin"
			if direction == gateOutbound {
				action = "/gate/checkout/vehicle"
			}
			rec := postForm(t, srv, action, vals)
			if rec.Code == http.StatusSeeOther {
				t.Fatal("a commit with no driver marked was accepted")
			}
			// Read the refusal NOTICE, not the page: the gate script's own
			// disabled-commit reason contains this sentence too, so a
			// whole-body scan would pass even if the server stopped
			// refusing (mutation-proved).
			if got := vehicleError(t, rec.Body.String(), direction); !strings.Contains(got, "Mark who was driving") {
				t.Errorf("the refusal does not say what to do: %q", got)
			}
		})
	}
}

// TestARefusedCommitKeepsTheWholeList. A refusal AFTER submit costs an
// operator a page of typing at -30 with a bus idling, and losing the list
// would be a far worse failure than the one being prevented. This is the
// JS-off behaviour that most needs a test: with scripting on the commit
// button is disabled until a driver is marked, so this path is only ever
// reached when the script is what failed.
func TestARefusedCommitKeepsTheWholeList(t *testing.T) {
	const crew = 26
	srv, _, _ := newGateTestServer(t)
	var names []string
	for i := 0; i < crew; i++ {
		names = append(names, fmt.Sprintf("Rider %02d", i))
	}
	_, vals := gateBuild(t, srv, gateInbound, "BUS 7", names, -1) // nobody flagged

	rec := postForm(t, srv, "/gate/checkin", vals)
	if rec.Code != http.StatusConflict {
		t.Fatalf("commit with no driver = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, n := range names {
		if !strings.Contains(body, n) {
			t.Fatalf("the refused page has lost %q. All %d names must come back, or a validation error costs the operator the whole entry:\n%s", n, crew, body)
		}
	}
	if !strings.Contains(body, "BUS 7") {
		t.Error("the refused page has lost the vehicle id")
	}
	// And the page that comes back must be submittable again.
	again := gateFormValues(t, body, formIDFor(gateInbound))
	if got := len(again["occupant_typed"]); got != crew {
		t.Fatalf("the refused page carries %d passenger rows, want %d — it renders the names but cannot be re-submitted", got, crew)
	}
}

// TestTheDriverFlagFollowsThePersonNotTheRow. driver_index names a row
// POSITION, and the list is renumbered on every delete — so a naive
// implementation that re-emitted the stored number would silently re-point
// the flag at whoever moved up into that slot. That is the same class of
// bug as the parallel-array shift, and it puts a name nobody chose into an
// incident report.
func TestTheDriverFlagFollowsThePersonNotTheRow(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	names := []string{"Alpha Anders", "Bravo Bell", "Charlie Cole", "Delta Dunn"}
	page, vals := gateBuild(t, srv, gateInbound, "TRK-14", names, 2) // Charlie Cole

	// Delete row 0 (Alpha). Charlie moves from index 2 to index 1.
	del := vals
	del.Set("delete_index", "0")
	rec := postForm(t, srv, "/gate/passenger/delete", del)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d: %s", rec.Code, rec.Body.String())
	}
	page = rec.Body.String()

	after := gateFormValues(t, page, formIDFor(gateInbound))
	typed := after["occupant_typed"]
	if len(typed) != 3 || typed[0] != "Bravo Bell" {
		t.Fatalf("precondition: after deleting row 0 the list is %v — the scenario did not renumber anything", typed)
	}
	idx := after.Get("driver_index")
	if idx == "" {
		t.Fatal("the driver flag was lost by deleting a DIFFERENT row")
	}
	n, err := strconv.Atoi(idx)
	if err != nil || n < 0 || n >= len(typed) {
		t.Fatalf("driver_index = %q, which is not a row of the list", idx)
	}
	if typed[n] != "Charlie Cole" {
		t.Fatalf("after deleting Alpha Anders the driver is %q, want Charlie Cole. The flag followed the ROW NUMBER instead of the person — an incident report would name somebody nobody chose.", typed[n])
	}
}

// TestDeletingTheFlaggedRowClearsTheFlagAndPromotesNobody. The failure to
// avoid is a trip that silently has no driver, or silently promotes
// whoever is now first. Promotion is the worse of the two: it is a name in
// a report that nobody chose.
func TestDeletingTheFlaggedRowClearsTheFlagAndPromotesNobody(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	names := []string{"Alpha Anders", "Bravo Bell", "Charlie Cole"}
	_, vals := gateBuild(t, srv, gateInbound, "TRK-14", names, 1) // Bravo Bell drives

	vals.Set("delete_index", "1")
	rec := postForm(t, srv, "/gate/passenger/delete", vals)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d: %s", rec.Code, rec.Body.String())
	}
	page := rec.Body.String()

	after := gateFormValues(t, page, formIDFor(gateInbound))
	if got := len(after["occupant_typed"]); got != 2 {
		t.Fatalf("precondition: %d rows after deleting one of three", got)
	}
	// ⟦R-28⟧ RETARGETED: the arriving form's compact driver <select>
	// (DriverSelect, gate_vehicle_form) ALWAYS submits a driver_index —
	// even "nothing chosen" is the empty leading option's own value — so
	// the field's presence no longer signals a promotion the way a bare
	// radio group's silence did. The precondition is now that the VALUE
	// is empty (no option carries `selected`), checked directly on the
	// select's own markup rather than the scraped form value.
	if got := gateFormValues(t, page, formIDFor(gateInbound)).Get("driver_index"); got != "" {
		t.Fatalf("deleting the flagged row left driver_index=%q — somebody was promoted into the flag without being chosen", got)
	}
	markup := gateFormMarkup(t, page, formIDFor(gateInbound))
	sel := driverSelectRe.FindString(markup)
	if sel == "" {
		t.Fatalf("no compact driver select on the page after the delete:\n%s", markup)
	}
	for _, opt := range driverOptionRe.FindAllString(sel, -1) {
		if strings.Contains(opt, "selected") {
			t.Fatalf("an option in the driver select is pre-selected after the delete: %s\nsomebody was promoted into the flag without being chosen.", opt)
		}
	}
	// The dedicated notice, not the page: the gate script emits the same
	// sentence as an inline hint, so a whole-body scan would pass even if
	// the server-rendered cue were deleted.
	cleared := regexp.MustCompile(`(?s)<div class="notice notice-warn driver-cleared">(.*?)</div>`).FindStringSubmatch(page)
	if cleared == nil {
		t.Fatalf("the page does not say the flag was cleared, so the operator has no cue that the question is open again:\n%s", page)
	}
	if !strings.Contains(cleared[1], "Bravo Bell") || !strings.Contains(cleared[1], "Mark who was driving") {
		t.Errorf("the cleared-flag notice does not name who lost the flag and what to do: %q", cleared[1])
	}
	// And the commit is refused until the question is answered again.
	rec = postForm(t, srv, "/gate/checkin", after)
	if rec.Code != http.StatusConflict {
		t.Fatalf("commit after the flag was cleared = %d, want 409", rec.Code)
	}
}

// TestUndoRestoresTheDeletedPassenger. There is deliberately NO
// confirmation on the per-row delete (confirming a control pressed
// repeatedly during a long entry trains click-through and degrades the
// confirm on the commit that matters), so the undo is what makes that
// decision affordable.
func TestUndoRestoresTheDeletedPassenger(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	names := []string{"Alpha Anders", "Bravo Bell", "Charlie Cole"}
	_, vals := gateBuild(t, srv, gateInbound, "TRK-14", names, 0)

	vals.Set("delete_index", "1")
	rec := postForm(t, srv, "/gate/passenger/delete", vals)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d", rec.Code)
	}
	page := rec.Body.String()
	if !strings.Contains(page, "Removed <strong>Bravo Bell</strong>") {
		t.Fatalf("no undo affordance naming the removed person:\n%s", page)
	}
	undoVals := gateFormValues(t, page, formIDFor(gateInbound))
	// The open box must NOT have been repopulated — that would clobber
	// whatever the operator has half-typed in it.
	if got := undoVals.Get("new_last"); got != "" {
		t.Errorf("the delete repopulated the open box's Last name with %q, clobbering anything half-typed there", got)
	}
	if got := undoVals.Get("new_first"); got != "" {
		t.Errorf("the delete repopulated the open box's First name with %q, clobbering anything half-typed there", got)
	}
	rec = postForm(t, srv, "/gate/passenger/undo", undoVals)
	if rec.Code != http.StatusOK {
		t.Fatalf("undo = %d: %s", rec.Code, rec.Body.String())
	}
	restored := gateFormValues(t, rec.Body.String(), formIDFor(gateInbound))
	got := restored["occupant_typed"]
	if len(got) != 3 || got[1] != "Bravo Bell" {
		t.Fatalf("after undo the list is %v, want the three names with Bravo Bell back in position 2", got)
	}
}

// --- the parallel-array guard ------------------------------------------

// TestMisalignedPassengerIndicesAreRefusedLoudly. occupant_typed /
// ⟦LOCK — §82, 2026-09-03⟧ ⟦was: "occupant_company / occupant_workgroup"⟧ — the field was renamed
// to `occupant_trade` and this prose was left behind, which is §78.1's class exactly: framing text
// that survives an edit and keeps asserting the old name.
// occupant_kind / occupant_value / occupant_company / occupant_trade
// arrive as PARALLEL ARRAYS. A row that emitted a different set of them
// shifts every later row's fields by one — silently attaching one person's
// name to another person's company, which at 26 rows is both likelier and
// far harder to spot than at four. The old form documented that hazard in a
// comment; this is the structural check that makes it a loud refusal.
func TestMisalignedPassengerIndicesAreRefusedLoudly(t *testing.T) {
	cases := map[string][]string{
		"a gap":               {"0", "1", "3"},
		"a duplicate":         {"0", "1", "1"},
		"out of order":        {"0", "2", "1"},
		"one index short":     {"0", "1"},
		"one index too many":  {"0", "1", "2", "3"},
		"no indices at all":   {},
		"a non-numeric index": {"0", "one", "2"},
	}
	for name, indices := range cases {
		t.Run(name, func(t *testing.T) {
			srv, st, _ := newGateTestServer(t)
			_, vals := gateBuild(t, srv, gateInbound, "TRK-14",
				[]string{"Dana Driver", "Pat Passenger", "Chris Crew"}, 0)
			if got := len(vals["occupant_index"]); got != 3 {
				t.Fatalf("precondition: the intact form carries %d indices, want 3", got)
			}
			vals["occupant_index"] = indices

			rec := postForm(t, srv, "/gate/checkin", vals)
			if rec.Code == http.StatusSeeOther {
				t.Fatal("a misaligned submit was accepted — one person's name can now be attached to another person's company, and nothing on any screen or sheet would say so")
			}
			if rec.Code != http.StatusConflict {
				t.Errorf("misaligned submit = %d, want 409", rec.Code)
			}
			if got := vehicleError(t, rec.Body.String(), gateInbound); !strings.Contains(got, "not submitted intact") {
				t.Errorf("the refusal does not say what went wrong: %q", got)
			}
			trips, err := domain.Trips(st)
			if err != nil {
				t.Fatal(err)
			}
			if len(trips) != 0 {
				t.Fatalf("a refused submit authored %d trip(s)", len(trips))
			}
		})
	}
}

// TestGateCheckInRefusesANamelessAccumulatedRow. The old form rendered four
// occupant boxes and SKIPPED the blank ones, which was right: three of them
// were ordinarily unused. In the accumulator a row exists only because the
// operator added a person, so a row with no name means something went
// wrong, and skipping it silently is how a vehicle arrives holding fewer
// people than it did.
func TestGateCheckInRefusesANamelessAccumulatedRow(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	_, vals := gateBuild(t, srv, gateInbound, "TRK-14", []string{"Dana Driver", "Pat Passenger"}, 0)
	typed := vals["occupant_typed"]
	typed[1] = ""
	vals["occupant_typed"] = typed
	vals["occupant_value"] = []string{vals["occupant_value"][0], ""}

	rec := postForm(t, srv, "/gate/checkin", vals)
	if rec.Code == http.StatusSeeOther {
		t.Fatal("a nameless row was silently skipped — the vehicle would be recorded holding fewer people than it did, with no cue anywhere")
	}
	if got := vehicleError(t, rec.Body.String(), gateInbound); !strings.Contains(got, "has no name") {
		t.Errorf("the refusal does not name the problem: %q", got)
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 0 {
		t.Fatalf("a refused check-in authored %d visit(s)", len(open))
	}
}

// TestTheDriverIsNotDoubleCounted. The domain prepends DriverPersonID into
// the occupant list (trip.go's fold and tripauthor.go's authoring path,
// independently and idempotently), and this form marks one of the
// passengers as the driver rather than carrying a separate driver field —
// so the driver must appear exactly once on the muster, not twice.
func TestTheDriverIsNotDoubleCounted(t *testing.T) {
	const crew = 26
	srv, st, _ := newGateTestServer(t)
	var names []string
	for i := 0; i < crew; i++ {
		names = append(names, fmt.Sprintf("Rider %02d", i))
	}
	_, vals := gateBuild(t, srv, gateInbound, "BUS 7", names, 3)
	// ⟦R-26⟧ this test is about double-counting the driver, not company —
	// give the crew one so the commit is not refused for the reason R-26
	// added.
	gateFillNewRowCompanies(t, vals, "R-26 Test Co")
	rec := postForm(t, srv, "/gate/checkin", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("commit = %d: %s", rec.Code, rec.Body.String())
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != crew {
		t.Fatalf("%d visits for a %d-person bus — the flagged passenger was counted twice", len(open), crew)
	}
	trips, err := domain.Trips(st)
	if err != nil {
		t.Fatal(err)
	}
	occ, err := domain.TripOccupantsByTrip(st, trips[0].TripID)
	if err != nil {
		t.Fatal(err)
	}
	if len(occ) != crew {
		t.Fatalf("%d occupant rows for a %d-person bus", len(occ), crew)
	}
	seen := map[string]int{}
	for _, o := range occ {
		seen[o.PersonID]++
	}
	if seen[trips[0].DriverPersonID] != 1 {
		t.Fatalf("the driver appears %d times in the occupant list, want exactly 1", seen[trips[0].DriverPersonID])
	}
}

// --- structural separation of the two buttons --------------------------

var submitButtonRe = regexp.MustCompile(`(?s)<button[^>]*type="submit"[^>]*>`)

// TestEnterCannotCommitAVehicle. "Add person" is pressed 25 times per bus
// and the commit once; a mis-hit on the commit strands the remaining people
// off the muster, which inbound is the dangerous direction. The separation
// is therefore structural rather than visual — CSS and position can fail,
// form routing cannot — and all three legs are asserted here because any
// one of them alone can be edited away.
func TestEnterCannotCommitAVehicle(t *testing.T) {
	for _, tc := range []struct{ direction, commitAction string }{
		{gateInbound, "/gate/checkin"},
	} {
		t.Run(tc.direction, func(t *testing.T) {
			srv, _, _ := newGateTestServer(t)
			_, page := getGate(t, srv)
			markup := gateFormMarkup(t, page, formIDFor(tc.direction))

			// 1. The form's own action is the ACCUMULATE route, so a
			//    browser that submits with no button at all adds.
			open := gateFormOpenRe.FindString(markup)
			if !strings.Contains(open, `action="/gate/passenger/add"`) {
				t.Fatalf("the form's own action is not the accumulate route: %s\nImplicit submission would then reach a route that WRITES.", open)
			}
			// 2. The first submit button — the default button implicit
			//    submission fires — is "Add person", and it carries no
			//    formaction, so it uses the form's own action.
			buttons := submitButtonRe.FindAllString(markup, -1)
			if len(buttons) < 2 {
				t.Fatalf("precondition: %d submit buttons in the form; this test needs the add and the commit to both exist", len(buttons))
			}
			if !strings.Contains(buttons[0], `class="p-add"`) {
				t.Fatalf("the FIRST submit button in the form is not Add person: %s\nImplicit submission fires the form's default button, which is the first one in tree order.", buttons[0])
			}
			if strings.Contains(buttons[0], "formaction") {
				t.Errorf("Add person carries a formaction, so it no longer inherits the form's accumulate action: %s", buttons[0])
			}
			// 3. The commit reaches its route ONLY via formaction, i.e.
			//    only by being the activated button.
			commit := ""
			for _, b := range buttons {
				if strings.Contains(b, "gate-commit") {
					commit = b
				}
			}
			if commit == "" {
				t.Fatal("no commit button in the form")
			}
			if !strings.Contains(commit, `formaction="`+tc.commitAction+`"`) {
				t.Fatalf("the commit button does not carry formaction=%q: %s", tc.commitAction, commit)
			}
			if commit == buttons[0] {
				t.Fatal("the commit button IS the form's default button — Enter in the passenger box would write the vehicle")
			}
		})
	}
}

// ⟦R-28, team-lead, 2026-09-16⟧ TestEnterCannotCommitAVehicle's own
// outbound case is REMOVED above (not retargeted): the departing form is
// now ALWAYS gate_vehicle_form's Compact instantiation (gate_roster_half),
// which has NO "Add person" button at all — Compact's own `{{if not
// .Compact}}` omits it entirely (every occupant row is already resolved
// from a roster tick, there is nothing left to type a new name into) — so
// "the first button is Add, not commit" has no button to BE Add.
//
// ⚠ MEASURED while building this test, and NOT independently added as a
// new declared-red (that changes the instrument's 17-row register, a
// bigger decision than this task's own scope): a Compact vehicle-form's
// COMMIT button is the ONLY submit button in the form. Per the HTML
// implicit-submission rule, a scriptless Enter pressed in the Vehicle
// number text field therefore activates the commit directly — no add
// step to absorb it, and no confirm() dialog without scripting. This is
// the EXACT hazard TestEnterCannotCommitAVehicle exists to catch, and it
// is ALREADY true of gate.html's own Compact checkout box, deployed since
// R-27 revision 3 — this test only ever exercised THIS page's former
// non-Compact outbound form, so nothing previously caught it there
// either. Not something R-28 introduced; R-28 gave the shape a second
// caller and a test finally reached it. Reported to team-lead
// (reports/witness/VEHICLE-MODE.md) as a found-not-fixed item rather than
// papered over here or patched in a hurry inside an already-large change.

// TestTheScriptedRowTemplateMatchesTheServerRenderedRow. With scripting on
// the rows are cloned from <template id="passenger-template">; with it off
// the server renders them. The two must emit the SAME FIELDS IN THE SAME
// ORDER, because they arrive as parallel arrays and a clone that emitted a
// different set would shift every later row by one — the exact hazard
// occupant_index exists to catch, caught here one layer earlier.
func TestTheScriptedRowTemplateMatchesTheServerRenderedRow(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	// ⟦LOCK — §82, 2026-09-03⟧ ⟦was: "[]string{\"Dana Driver\"}"⟧ — TWO rows, not one, and the
	// second is what makes the repeat below an assertion rather than a restatement: with a single
	// row "the clone's shape, once" is just the old comparison under a new name. `gateBuild`
	// asserts one row per name (:198), so the row count is DERIVED from this slice and is not a
	// second thing to keep in step.
	crew := []string{"Dana Driver", "Pat Passenger"}
	page, _ := gateBuild(t, srv, gateInbound, "TRK-14", crew, -1)
	page = styleRe.ReplaceAllString(page, "")

	tplStart := strings.Index(page, `<template id="passenger-template">`)
	if tplStart < 0 {
		t.Fatal("no passenger template on the page — the scripted add has no clone source")
	}
	tplEnd := strings.Index(page[tplStart:], "</template>")
	tplMarkup := page[tplStart : tplStart+tplEnd]

	fieldsOf := func(markup string) []string {
		var out []string
		for _, tag := range inputTagRe.FindAllString(markup, -1) {
			a := tagAttrs(tag)
			if a["name"] != "" {
				out = append(out, a["name"])
			}
		}
		return out
	}
	server := fieldsOf(gateFormMarkup(t, page, formIDFor(gateInbound)))
	cloned := fieldsOf(tplMarkup)

	// ⟦LOCK — §82, 2026-09-03⟧ THE ROW IS LOCATED, NOT ENUMERATED — and the enumeration this
	// replaces is what made this test lie.
	// ⟦was: "rowFields := map[string]bool{\"occupant_index\": true, … \"occupant_workgroup\": true}"⟧
	// ⟦was: the server side filtered through that map, the clone side not filtered at all⟧
	//
	// ⚠⚠ WHAT THE OLD FORM DID, MEASURED. The map still named `occupant_workgroup` after the field
	// was renamed to `occupant_trade`, and it was applied to the SERVER side ONLY. So the server's
	// real `occupant_trade` was silently DROPPED before the comparison, and the test reported
	//     server: [… occupant_company]   clone: [… occupant_company occupant_trade]
	// — which reads as "the clone emits a field the server does not", i.e. a parallel-array
	// corruption hazard. ⛔ THE TEMPLATES AGREED PERFECTLY. [MEASURED, unfiltered, both sides]
	// each emits exactly: occupant_index, driver_index, occupant_typed, occupant_kind,
	// occupant_value, occupant_company, occupant_trade. **The defect was in this test, and its
	// failure message pointed at the opposite of the truth.**
	//
	// ⚠ A ONE-SIDED FILTER CAN ONLY EVER MISREPORT THIS. Any server field the map did not know
	// about was reported as ABSENT FROM THE SERVER — the one direction that cannot be true of a
	// field the server just rendered. A hand-kept list that must be updated on every rename, with
	// nothing enforcing it, is the same class this file's own `occupant_index` guard exists for.
	//
	// THE REPLACEMENT: the row is a REPEATING UNIT, so it is found rather than listed. The clone
	// defines one row; the server must emit that same sequence once per occupant, back to back.
	// ⭐ That is strictly stronger than the old check as well as self-maintaining: an inserted,
	// dropped or reordered field breaks the repeat at the first row, and an extra row is caught by
	// the boundary check below. No field name appears in this test at all.
	if len(cloned) == 0 {
		t.Fatal("precondition: the clone template rendered no fields — every comparison below would be vacuous")
	}
	start := -1
	for i, f := range server {
		if f == cloned[0] {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("precondition: the server form carries no %q, so the row could not be located at all: %v", cloned[0], server)
	}
	var want []string
	for i := 0; i < len(crew); i++ {
		want = append(want, cloned...)
	}
	end := start + len(want)
	if end > len(server) {
		t.Fatalf("the server-rendered form ends mid-row: it carries %d field(s) from %q onward, but %d row(s) "+
			"of the clone's shape need %d.\n  server: %v\n  clone:  %v",
			len(server)-start, cloned[0], len(crew), len(want), server, cloned)
	}
	if strings.Join(server[start:end], ",") != strings.Join(want, ",") {
		t.Fatalf("the scripted clone source and the server-rendered row do not agree.\n  server: %v\n  clone:  %v\nThese arrive as parallel arrays; a row emitting a different set shifts every later row's fields by one.", server[start:end], want)
	}
	// The boundary: one more row's worth would mean the server rendered an occupant the fixture
	// did not ask for, which the length comparison above cannot see.
	if end < len(server) && server[end] == cloned[0] {
		t.Fatalf("the server rendered MORE than the %d row(s) the fixture built — the field after the "+
			"measured run is %q again: %v", len(crew), cloned[0], server)
	}
}

// --- the accumulate routes write nothing --------------------------------

// TestTheAccumulateRoutesAuthorNothing. The add/delete/undo routes are
// deliberately NOT behind requireLogin, on the grounds that they author
// nothing. That reasoning is only sound while the claim is true, so the
// claim is asserted rather than argued: the store's watermark must not
// move.
func TestTheAccumulateRoutesAuthorNothing(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	// gate-restack, 2026-09-11: checkInCrew authors real events (it IS a
	// check-in), so the watermark baseline is taken AFTER it, exactly like
	// every other setup step in this file — the property under test is
	// that the FOUR accumulate routes below move nothing past this point,
	// not that the whole test is a no-op.
	visitIDs := checkInCrew(t, srv, st, 2)
	before, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	_, page := getGate(t, srv)
	page = gateSetVehicle(t, srv, page, gateInbound, "TRK-14")
	page = gateAdd(t, srv, page, gateInbound, "Dana Driver")
	page = gateAdd(t, srv, page, gateInbound, "Off Roster Ozzy") // would CREATE a person if it authored
	vals := gateFormValues(t, page, formIDFor(gateInbound))
	vals.Set("delete_index", "1")
	rec := postForm(t, srv, "/gate/passenger/delete", vals)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d", rec.Code)
	}
	undo := gateFormValues(t, rec.Body.String(), formIDFor(gateInbound))
	if rec = postForm(t, srv, "/gate/passenger/undo", undo); rec.Code != http.StatusOK {
		t.Fatalf("undo = %d", rec.Code)
	}
	// ⟦gate-restack, 2026-09-11; updated R-17, 2026-09-11⟧ "Depart
	// together": ticks a real on-site visit and moves it onto the OUTBOUND
	// accumulated list. It would CREATE a vehicle record if it authored —
	// this route never calls anything that does, and as of R-17's schema
	// path NOTHING in this whole feature mints a vehicle record for an
	// unidentified departure any more (vehicleIDForUnknown, which used to
	// be the one thing that did, is retired; see vehicle_comint.go) — the
	// strongest available analogue here to "Off Roster Ozzy" above, since a
	// fresh Watermark comparison already covers every other kind of write.
	selRec := postForm(t, srv, "/gate/checkout/select", url.Values{"visit_ids": visitIDs})
	if selRec.Code != http.StatusOK {
		t.Fatalf("checkout/select = %d", selRec.Code)
	}
	if !strings.Contains(selRec.Body.String(), `id="vehicle-form-out"`) || !strings.Contains(selRec.Body.String(), `name="occupant_index" value="0"`) {
		t.Fatalf("checkout/select did not stage the ticked visits onto the outbound accumulated list:\n%s", selRec.Body.String())
	}

	// ⟦gate-checkin-spec, 2026-09-13⟧ "Edit person": resolves ticked visits
	// to a person id and redirects — it would UPDATE nothing on the person
	// record and MINT nothing if it authored; a fresh Watermark comparison
	// below covers it same as every route above.
	editRec := postForm(t, srv, "/gate/edit-selected", url.Values{"visit_ids": {visitIDs[0]}})
	if editRec.Code != http.StatusSeeOther {
		t.Fatalf("edit-selected = %d, want 303", editRec.Code)
	}
	if loc := editRec.Header().Get("Location"); !strings.HasPrefix(loc, "/person/amend?person_id=") {
		t.Fatalf("edit-selected redirected to %q, want /person/amend?person_id=...", loc)
	}

	after, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("the accumulate routes authored %d event(s) (watermark %d -> %d). They are ungated BECAUSE they author nothing; if they author, F42's attribution guarantee has a hole in it.", after-before, before, after)
	}
}

// --- identity, when the operator edits an already-added row -------------

// TestEditingAnAddedRowReResolvesTheIdentity. The accumulated rows are
// editable — James asked for that — and each carries a hidden id the
// roster browser resolved. With scripting off, correcting the NAME on a row
// leaves the old id sitting in the hidden field, and preferring the id
// (which every previous version of this handler did) would check in the
// person the operator just corrected AWAY from, under the corrected name.
// Nothing on any screen or sheet would show it.
//
// The rule, matching gate.html's resolvePick exactly: the visible text is
// the authority, and a submitted id is honoured only while it still names a
// person whose roster name agrees with it.
func TestEditingAnAddedRowReResolvesTheIdentity(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	_, vals := gateBuild(t, srv, gateInbound, "TRK-14", []string{"Dana Driver"}, 0)
	if got := vals.Get("occupant_value"); got != "" && got != persons[0].PersonID {
		t.Logf("row carried value=%q", got)
	}
	// Force the JS-on shape: an id resolved for Dana, and the text edited
	// to Chris.
	vals.Set("occupant_kind", "existing")
	vals.Set("occupant_value", persons[0].PersonID) // Dana Driver
	vals.Set("occupant_typed", "Chris Crew")

	rec := postForm(t, srv, "/gate/checkin", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("commit = %d: %s", rec.Code, rec.Body.String())
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 {
		t.Fatalf("%d visits, want 1", len(open))
	}
	if open[0].PersonID == persons[0].PersonID {
		t.Fatal("the stale id won over the corrected name: the row reads \"Chris Crew\" and Dana Driver is on the muster. An operator who fixes a name has no way to know the fix did not take.")
	}
	if open[0].PersonID != persons[2].PersonID {
		t.Fatalf("the corrected name resolved to %s, want Chris Crew (%s)", open[0].PersonID, persons[2].PersonID)
	}
}

// TestAResolvedIDStillWinsOverAnIdenticalDisplayName is the other side of
// the same rule, and it is what stops the fix above from destroying the
// duplicate-name protection: two people share a display name, the operator
// picks one from the roster list, and THAT person — not a name lookup — is
// who gets checked in.
func TestAResolvedIDStillWinsOverAnIdenticalDisplayName(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	twin, err := domain.CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if twin.PersonID == persons[0].PersonID {
		t.Fatal("precondition: the twin is the same person")
	}
	_, vals := gateBuild(t, srv, gateInbound, "TRK-14", []string{"Dana Driver"}, 0)
	vals.Set("occupant_kind", "existing")
	vals.Set("occupant_value", twin.PersonID)
	vals.Set("occupant_typed", "Dana Driver")

	rec := postForm(t, srv, "/gate/checkin", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("commit = %d: %s", rec.Code, rec.Body.String())
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].PersonID != twin.PersonID {
		t.Fatalf("checked in %v, want the twin (%s) the operator actually picked — an ambiguous NAME was preferred over an unambiguous ID", open, twin.PersonID)
	}
}

// TestTheOutboundDestinationDefaultsToNone. The outbound form carries the
// one <select> on this page, and a select ALWAYS displays something — the
// same property that made the old driver control unsafe. Here it is safe,
// but only because the option it displays is the empty one, and that has to
// be asserted rather than assumed.
//
// Derivation: naming a controlled destination creates an inbound
// EXPECTATION at that peer and REQUIRES a deadline — a promise that a timer
// will raise an alarm about. A destination that arrived pre-chosen would
// make that promise on behalf of an operator who never made it, and the
// failure is a false OVERDUE on the one warning that has to stay believed.
// Leaving it unset still records the departure and still takes everybody
// off THIS site's muster, which is the safety-relevant half; the far end
// records them as an unsolicited arrival when they turn up. Under-promising
// is recoverable at the far gate; over-promising fires an alarm nobody can
// close.
func TestTheOutboundDestinationDefaultsToNone(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	visitIDs := checkInCrew(t, srv, st, 3)
	// ⟦R-28⟧ RETARGETED: the departing form is reached via the roster's
	// own tick-and-select door, not present on a fresh page.
	page, _ := gateDepartSelected(t, srv, visitIDs)
	markup := gateFormMarkup(t, page, formIDFor(gateOutbound))

	// ⟦[[D-84]], 2026-09-05⟧ ⟦was: "m := selectBlockRe.FindStringSubmatch(markup)"
	// then "if m[1] != \"destination_id\" { t.Fatalf(\"the departing form's
	// select is %q, not the destination\") }"⟧ — the form used to carry ONE
	// <select>, so "the first select" WAS the destination. D-84 puts a
	// company and a trade picker in the Add-person box and on every row, so
	// the destination is now LOCATED BY NAME. Every assertion below is
	// unchanged: exactly one option selected, it is the empty one, and the
	// control is `required`.
	var m []string
	for _, cand := range selectBlockRe.FindAllStringSubmatch(markup, -1) {
		if cand[1] == "destination_id" {
			m = cand
			break
		}
	}
	if m == nil {
		t.Fatalf("no destination control on the departing-vehicle form:\n%s", markup)
	}
	opts := optionTagRe.FindAllString(m[2], -1)
	if len(opts) < 2 {
		t.Fatalf("precondition: %d destination options — with only one, 'the default is the empty one' is true by accident", len(opts))
	}
	selected := ""
	n := 0
	for _, o := range opts {
		a := tagAttrs(o)
		if _, ok := a["selected"]; ok {
			selected = a["value"]
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d destination options are marked selected; a browser will display one whatever the markup says, so exactly one must be chosen deliberately", n)
	}
	if selected != "" {
		t.Fatalf("the destination defaults to %q. A pre-chosen destination promises a deadline nobody set — a false OVERDUE on the one warning that has to stay believed.", selected)
	}
	// And an untouched departing form really does submit no destination.
	vals := gateFormValues(t, page, formIDFor(gateOutbound))
	if got := vals.Get("destination_id"); got != "" {
		t.Fatalf("an untouched departing form submits destination_id=%q", got)
	}
	// A <select> must display something, so "nothing pre-chosen" is only
	// safe while `required` refuses the thing it displays. Without it the
	// empty prompt would be a submittable answer — and a departure with no
	// destination is indistinguishable from a crashed check-in
	// (TestAnOutboundVehicleNeverLooksLikeACrashedCheckIn).
	// ⟦[[D-84]]⟧ the `required` check reads the DESTINATION select's own tag
	// (m[0] is the whole matched block), not the first <select on the form.
	if !strings.Contains(strings.SplitN(m[0], ">", 2)[0], "required") {
		t.Errorf("the destination control is not required, so the empty prompt it displays is a submittable answer:\n%s", m[0])
	}
}

// --- round trips, MEASURED ---------------------------------------------

// TestTheVehicleFormStaysWithinTheRoundTripBudget measures what the two
// entry paths actually cost the server, with the same instrument the older
// budget tests use: a real httptest server over loopback, a real net/http
// client that auto-follows the 303 exactly like a browser, and
// countingHandler wrapping the real mux so EVERY dispatch is counted,
// redirect follows included.
//
// The locked constraint is "<=2 round trips per PAGE LOAD", not per vehicle,
// and the accumulator is what makes that distinction load-bearing:
//
//   - With the roster browser running, adding a passenger costs NOTHING —
//     the row is built in the DOM. A whole vehicle is one page load plus
//     one commit, whatever the headcount.
//   - With scripting off, each add is ONE round trip: a POST rendered
//     DIRECTLY, not post-redirect-GET, because a redirect cannot carry the
//     accumulated list forward. That is 1, inside the budget, and it is
//     asserted per step rather than only in total — a step that grew a
//     redirect would still total plausibly while doubling the cost of a
//     26-person bus.
//
// WHAT THIS DOES NOT MEASURE: keystrokes and clicks. There is no key-level
// browser instrument here; those figures are analytic and are reported as
// such, never as observed.
func TestTheVehicleFormStaysWithinTheRoundTripBudget(t *testing.T) {
	for _, crew := range []int{2, 26} {
		t.Run(fmt.Sprintf("%d people", crew), func(t *testing.T) {
			srv, st, _ := newGateTestServer(t)
			counted := &countingHandler{inner: srv.Handler()}
			ts := httptest.NewServer(counted)
			defer ts.Close()
			client := ts.Client()
			jar, err := cookiejar.New(nil)
			if err != nil {
				t.Fatal(err)
			}
			client.Jar = jar
			// Login happens at shift handover, not per vehicle — the same
			// exclusion the older budget tests make, for the same reason.
			resp, err := client.PostForm(ts.URL+"/login", url.Values{"password": {testLoginPassword}, "next": {"/gate/vehicle"}})
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()

			// --- scripting OFF: one page load, N adds, one commit --------
			counted.n = 0
			resp, err = client.Get(ts.URL + "/gate/vehicle")
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			page := string(body)
			if counted.n != 1 {
				t.Fatalf("the gate page load cost %d round trips, want 1", counted.n)
			}

			vals := gateFormValues(t, page, formIDFor(gateInbound))
			vals.Set("vehicle", "BUS 7")
			perStep := []int{}
			for i := 0; i < crew; i++ {
				vals.Set("new_last", fmt.Sprintf("Rider %02d", i))
				counted.n = 0
				resp, err = client.PostForm(ts.URL+"/gate/passenger/add", vals)
				if err != nil {
					t.Fatal(err)
				}
				b, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("add %d = %d", i, resp.StatusCode)
				}
				perStep = append(perStep, counted.n)
				vals = gateFormValues(t, string(b), formIDFor(gateInbound))
			}
			for i, n := range perStep {
				if n != 1 {
					t.Fatalf("accumulate step %d cost %d round trips, want exactly 1 (POST rendered directly). A redirect here would double the cost of every add AND could not carry the list forward.", i, n)
				}
			}
			if got := len(vals["occupant_typed"]); got != crew {
				t.Fatalf("precondition: %d rows accumulated, want %d", got, crew)
			}
			// ⟦R-26⟧ this measures round trips, not company — give the crew
			// one so the commit is not refused for the reason R-26 added.
			gateFillNewRowCompanies(t, vals, "R-26 Test Co")
			vals = gateMarkDriver(vals, 0)
			counted.n = 0
			resp, err = client.PostForm(ts.URL+"/gate/checkin", vals)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			commit := counted.n
			if commit != 2 {
				t.Fatalf("the commit cost %d round trips, want 2 (POST + the browser's automatic redirect-follow GET — it IS a write, so it stays post-redirect-GET and cannot be replayed by a refresh)", commit)
			}
			open, err := domain.OpenVisitsAt(st, "site-a")
			if err != nil {
				t.Fatal(err)
			}
			if len(open) != crew {
				t.Fatalf("precondition: %d people on the muster, want %d — the measured flow did not perform the check-in being measured", len(open), crew)
			}
			jsOff := 1 + crew + commit
			t.Logf("MEASURED, scripting off, %d people: %d round trips (1 page load + %d adds at 1 each + %d commit)", crew, jsOff, crew, commit)

			// --- scripting ON: the adds are DOM work, so the same vehicle
			// is one page load plus the commit, whatever the headcount ----
			srv2, _, _ := newGateTestServer(t)
			counted2 := &countingHandler{inner: srv2.Handler()}
			ts2 := httptest.NewServer(counted2)
			defer ts2.Close()
			client2 := ts2.Client()
			jar2, err := cookiejar.New(nil)
			if err != nil {
				t.Fatal(err)
			}
			client2.Jar = jar2
			resp, err = client2.PostForm(ts2.URL+"/login", url.Values{"password": {testLoginPassword}, "next": {"/gate/vehicle"}})
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			counted2.n = 0
			resp, err = client2.Get(ts2.URL + "/gate/vehicle")
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			// The script builds the rows locally; the body it posts is the
			// same body the scripting-off path built server-side, which is
			// exactly why the two cannot drift.
			scripted := url.Values{"passenger_list": {"1"}, "direction": {gateInbound}, "vehicle": {"BUS 7"}, "driver_index": {"0"}, "time_source": {"default"}}
			for i := 0; i < crew; i++ {
				scripted.Add("occupant_index", strconv.Itoa(i))
				scripted.Add("occupant_typed", fmt.Sprintf("Rider %02d", i))
				scripted.Add("occupant_kind", "existing")
				scripted.Add("occupant_value", "")
				// ⟦R-26⟧ this measures round trips, not company — give the
				// crew one, through the add-new escape, so the commit is
				// not refused for the reason R-26 added.
				scripted.Add("occupant_company_id", newRef)
				scripted.Add("occupant_company", "R-26 Test Co")
				// ⟦LOCK — §82, 2026-09-03⟧ ⟦was: "scripted.Add(\"occupant_workgroup\", \"\")"⟧
				// ⚠ This POSTS the body the real script would send, and it was sending a field the
				// server no longer reads while omitting the one it does. This test asserts only the
				// ROUND-TRIP COUNT, so it stayed green throughout — but its stated premise, "the body
				// it posts is the same body the scripting-off path built server-side, which is exactly
				// why the two cannot drift", had quietly become FALSE. The premise is restored here;
				// the count assertion is untouched.
				scripted.Add("occupant_trade", "")
			}
			resp, err = client2.PostForm(ts2.URL+"/gate/checkin", scripted)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			jsOn := counted2.n
			if jsOn != 3 {
				t.Fatalf("scripting on, %d people = %d round trips, want 3 (page load + POST + redirect-follow GET)", crew, jsOn)
			}
			t.Logf("MEASURED, scripting on, %d people: %d round trips (1 page load + 1 commit POST + 1 redirect-follow GET), independent of headcount", crew, jsOn)
		})
	}
}

// TestFocusFollowsTheOpenBoxOnceEntryHasStarted.
//
// With scripting off every add is a real navigation, and the browser
// focuses whatever carries `autofocus` on the page it lands on. If that
// stayed on the vehicle id, an operator typing the next passenger's name
// without looking would append it to the VEHICLE ID — the field an
// evacuation muster is read against — and would pay one Tab per passenger,
// 25 of them on a bus.
//
// So: exactly one autofocus on the page in either state, on the vehicle
// field before anything is entered and on the open passenger box afterwards.
// TestFocusFollowsTheOpenBoxOnceEntryHasStarted — RENAMED IN SPIRIT ONLY
// (the name still fits: focus follows the open box, always, now). ⟦was:
// autofocus lands on the Vehicle field on a fresh page and hands off to the
// open name box once something is $started⟧ — that was F43's rule for the
// OLD field order, where Vehicle sat ABOVE the open box and needed the
// first keystroke. gate-checkin-spec (James, 2026-09-13) moved Last name to
// be the FIRST field in the fieldset on every render, so it is simply the
// one control that always carries autofocus for this direction — see
// gate.html's own comment on the input. The property this test protects —
// exactly one autofocus attribute, and it is never on the vehicle id, which
// an operator typing without looking must not silently extend — is
// UNCHANGED; only which field satisfies it moved.
func TestFocusFollowsTheOpenBoxOnceEntryHasStarted(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	_, fresh := getGate(t, srv)
	fresh = styleRe.ReplaceAllString(fresh, "")
	if got := strings.Count(fresh, "autofocus"); got != 1 {
		t.Fatalf("a freshly served gate page carries %d autofocus attributes, want exactly 1", got)
	}
	freshOpenBox := regexp.MustCompile(`<input[^>]*class="p-open"[^>]*>`).FindString(fresh)
	if !strings.Contains(freshOpenBox, "autofocus") {
		t.Error("a fresh gate page does not focus the open Last name box — entering a name should be the first and cheapest keystroke of the check-in")
	}
	if strings.Contains(vehicleInputRe.FindString(fresh), "autofocus") {
		t.Error("a fresh gate page focuses the vehicle id — Last name is the first field in the new order (gate-checkin-spec, 2026-09-13) and should hold focus instead")
	}

	page := gateSetVehicle(t, srv, fresh, gateInbound, "TRK-14")
	page = gateAdd(t, srv, page, gateInbound, "Dana Driver")
	page = styleRe.ReplaceAllString(page, "")
	if got := strings.Count(page, "autofocus"); got != 1 {
		t.Fatalf("a part-entered gate page carries %d autofocus attributes, want exactly 1", got)
	}
	openBox := regexp.MustCompile(`<input[^>]*class="p-open"[^>]*>`).FindString(page)
	if !strings.Contains(openBox, "autofocus") {
		t.Fatalf("after an add, focus does not stay on the open Last name box: %s\nWith scripting off that means the cursor lands somewhere else, and the next name typed without looking is lost or appended to the wrong field.", openBox)
	}
	if strings.Contains(vehicleInputRe.FindString(page), "autofocus") {
		t.Error("the vehicle id takes focus after a passenger has been added")
	}
}
