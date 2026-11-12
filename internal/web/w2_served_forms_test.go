// w2_served_forms_test.go — W2-11: every SERVED FORM INSTANCE whose
// submission targets a takes-input route emits the time_source signal and
// the collapsed control / "change time" affordance structure. THE UNIT IS
// THE SERVED FORM INSTANCE, NOT THE ROUTE (rev 3, finding W2-R2-1 HIGH:
// the route↔form mapping is not 1:1 — /trips/arrive alone is served by
// three instances on two screens; a per-route discharge is blind to every
// sibling, and omitting the control from the gate's inbound-arrival form
// alone would leave the gate permanently unable to record an inbound
// arrival while eight per-route assertions stayed green).
//
// THE RENDER PATH EXECUTES. A source-grep of templates is EXPRESSLY
// FORBIDDEN (it would be a third scan outside plan §5's CONTROL
// enumeration): every page the app serves is rendered over real HTTP
// against a provisioned fixture world, the served bytes are parsed as a
// scriptless browser would read them (the gate_form_test.go instrument),
// and the assertions run on what was actually served.
//
// THE UNIT SET IS DERIVED, NEVER HAND-KEPT: screen enumeration is the
// internal/web/*.go glob over "GET …" registration literals (W2-7's
// universe discipline, used here ONLY to select which handlers to
// EXECUTE — no assertion in this test reads source); a form targets a
// takes-input route through its `action` OR any submit control's
// `formaction` (the second channel is load-bearing: the two gate vehicle
// instantiations carry action="/gate/passenger/add" and reach
// /gate/checkin // /gate/checkout/vehicle only via the commit button's
// formaction). The discovered set reconciles against the census (ten
// instances at lock; nine since [[D-78]] struck the cancel row) BOTH ways:
// a census instance the renders never surface is red
// (an under-provisioned fixture reads as a MISSING instance, never a
// silent pass), and a discovered instance outside the census is red — a
// newly-served form enters LOUDLY, and the census is amended only at a
// criteria lock. Killed by (m15), applied per served form instance — ten
// applications; its reds also run against the pre-chunk tree (zero
// time_source in internal/web at 49abf65: all ten instances red).
// (WINDOW-BUILD-PLAN-v1 §3 W2, criteria LOCKED 2026-08-28.)
package web

import (
	"go/scanner"
	"go/token"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// --- screen enumeration (derived, W2-7's universe discipline) -----------

// w2GetRegistrationsInDir: every "GET /…" registration literal across the
// package glob — the pages the app serves. Selection only; the assertions
// below run on served bytes.
func w2GetRegistrationsInDir(t *testing.T, dir string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var out []string
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		file := fset.AddFile(path, fset.Base(), len(src))
		var sc scanner.Scanner
		sc.Init(file, src, nil, 0)
		for {
			_, tok, lit := sc.Scan()
			if tok == token.EOF {
				break
			}
			if tok != token.STRING || !strings.HasPrefix(lit, `"GET /`) {
				continue
			}
			page := strings.TrimSuffix(strings.TrimPrefix(lit, `"GET `), `"`)
			if !seen[page] {
				seen[page] = true
				out = append(out, page)
			}
		}
	}
	return out
}

// --- served-form parsing (the scriptless-browser instrument) ------------

var w2FormOpenRe = regexp.MustCompile(`(?s)<form\b[^>]*>`)

type w2FormInstance struct {
	Page    string
	Markup  string
	Targets []string // action + every submit control's formaction
}

// w2FormInstances parses one served page's markup into its form instances.
func w2FormInstances(t *testing.T, page, markup string) []w2FormInstance {
	t.Helper()
	var out []w2FormInstance
	locs := w2FormOpenRe.FindAllStringIndex(markup, -1)
	for _, loc := range locs {
		rest := markup[loc[0]:]
		end := strings.Index(rest, "</form>")
		if end < 0 {
			t.Fatalf("W2-11: a <form> on %s is never closed:\n%s", page, rest)
		}
		body := rest[:end+len("</form>")]
		inst := w2FormInstance{Page: page, Markup: body}
		open := markup[loc[0]:loc[1]]
		if a := tagAttrs(open)["action"]; a != "" {
			inst.Targets = append(inst.Targets, a)
		}
		// Any submit control's formaction — the load-bearing second
		// channel (gate.html:307's definition, button :647).
		for _, tag := range inputTagRe.FindAllString(body, -1) {
			if fa := tagAttrs(tag)["formaction"]; fa != "" {
				inst.Targets = append(inst.Targets, fa)
			}
		}
		for _, tag := range regexp.MustCompile(`(?s)<button\b[^>]*>`).FindAllString(body, -1) {
			if fa := tagAttrs(tag)["formaction"]; fa != "" {
				inst.Targets = append(inst.Targets, fa)
			}
		}
		out = append(out, inst)
	}
	return out
}

// w2SubmitValues is the JS-off untouched submission of one form's markup:
// every named non-button input (radios/checkboxes only when checked), plus
// selects — exactly gateFormValues' browser rule, over an arbitrary form.
func w2SubmitValues(markup string) url.Values {
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
		vals.Set(name, picked)
	}
	return vals
}

// --- the census (criteria-lock material; the addendum's measured ten) ---

type w2CensusRow struct {
	Page   string
	Route  string
	Marker string // distinguishes same-page same-route siblings; "" = unique by page+route
	Cite   string // the template citation, for failure messages only
}

// ⟦R-27, James, 2026-09-16⟧ THE CENSUS SPLIT ACROSS TWO PAGES.
//
// GET /gate became the streamlined individual page (gate.html): one CHECK
// IN form (its own instance, /gate/checkin/single) and the on-site roster's
// "Remove — no departure" form (/gate/checkout) — both were ALREADY rows
// here under Page "/gate" and stay exactly where they are; only what
// renders under that page/route pair changed, not the pair itself.
//
// The whole-vehicle group form moved, unchanged in behaviour, to GET
// /gate/vehicle (gate_vehicle.html) — see that file's own comment — taking
// FOUR rows with it (checkin, checkout/vehicle, resume, trips/arrive: the
// inbound board, the incomplete-check-ins notice and the outbound
// accumulate form all live there now, and never render on the individual
// page). gate_vehicle.html ALSO kept its OWN copy of the single-check-in
// form and the roster's own "Remove" form (R-25's page never removed
// either), so /gate/checkin/single and /gate/checkout now EACH have TWO
// served instances — one per page — hence two rows apiece below.
var w2FormCensus = []w2CensusRow{
	{"/gate/vehicle", "/gate/checkin", "", "gate_vehicle.html (gate_vehicle_form, inbound instantiation — via the commit button's formaction)"},
	// ⟦R-25, James, 2026-09-13, added after W2's own criteria lock — the
	// same row-13 precedent TestW2RouteWalkEveryPOSTRouteClassified's own
	// header names: a genuinely new served form, discovered unclassified
	// by this walk, amended here WITH its own assertion.⟧ Its OWN <form>
	// (action=, not a formaction sharing gate_vehicle_form's instance) —
	// see that form's own template comment for why one instance may not
	// target two takes-input routes.
	{"/gate", "/gate/checkin/single", "", "gate.html (the R-27 individual page's own CHECK IN form)"},
	// ⟦R-27⟧ the vehicle page's OWN, UNCHANGED single-check-in form USED
	// TO be here too, as a second, genuinely separate served instance —
	// same route, other page.
	// ⟦R-28 revision 2, team-lead, 2026-09-16⟧ item 1 removed that form
	// from gate_vehicle.html entirely ("individual mode already does
	// that" — see that file's own comment at the removal site), so this
	// route now has exactly ONE served instance, the row directly above.
	// Removed rather than left red, the SAME precedent the outbound row
	// removal right below already set for this exact census: a route
	// whose serving instance is genuinely gone is a census amendment, not
	// a regression to paper over.
	// ⟦R-28, team-lead, 2026-09-16⟧ the OUTBOUND row for /gate/vehicle is
	// GONE, not merely unrendered: gate_vehicle_form's outbound
	// instantiation now lives ONLY inside the shared "gate_roster_half"
	// define (gate.go's gateData.GateMode picks its Cancel link/gate_page
	// marker), reached exclusively through the roster's own tick-and-
	// select door (POST /gate/checkout/select) — a state this walk's plain
	// GET of a freshly-provisioned world can never reach, the SAME reason
	// gate.html's own identical Compact box was NEVER a row here (see
	// "/gate", "/gate/checkin/single" above — no matching
	// "/gate/checkout/vehicle" row for that page exists, or ever did).
	// Removed at THIS criteria lock rather than left red: the form still
	// exists and is still exercised, by gate_form_test.go's
	// gateBuildOutbound and by
	// TestEveryPersonMovingFormCarriesItsReviewBinding's own third,
	// POST-staged gate fixture (picker_test.go) —
	// this walk's own discipline is a plain-GET screen sweep, which
	// structurally cannot stage the tick first, so it is not the right
	// instrument for this one instance any more than it ever was for
	// gate.html's copy.
	{"/gate", "/gate/checkout", "", "gate.html (the individual page's own roster 'Remove — no departure' form)"},
	// ⟦R-27⟧ the vehicle page's OWN, UNCHANGED correcting check-out picker.
	{"/gate/vehicle", "/gate/checkout", "", "gate_vehicle.html (the correcting check-out picker, unchanged)"},
	{"/gate/vehicle", "/gate/resume", "", "gate_vehicle.html (the incomplete-check-in resume notice)"},
	// ⟦A4 / H-7, G-120, team-lead, 2026-09-30 — added after W2's own
	// criteria lock, the R-25 precedent above: a genuinely new served form,
	// discovered unclassified by this walk, amended here WITH its own
	// assertion.⟧ 02cff60 moved the notice + Resume form into ONE shared
	// define (gate_vehicle.html "incomplete_checkins"), now rendered on
	// BOTH pages, so /gate/resume has TWO served instances — one per page,
	// the same markup from the same define. This row's own assertion is the
	// per-instance block below (time_source=default on the untouched JS-off
	// submit, a datetime-local control, the "change time" affordance) run
	// on /gate's served bytes; the fixture's crash-window check-in (Chris)
	// is what makes it render, and a /gate that stops serving it reds as
	// NEVER SURFACED.
	{"/gate", "/gate/resume", "", "gate_vehicle.html incomplete_checkins, rendered by gate.html (the individual page's incomplete-check-in resume notice)"},
	{"/gate/vehicle", "/trips/arrive", "", "gate_vehicle.html (the gate screen's inbound-arrival form)"},
	{"/trips", "/trips/arrive", "stayed behind — do not check in", "trips.html:97 (the inbound-expectations board)"},
	{"/trips", "/trips/arrive", "remains off site", "trips.html:141 (the returns board)"},
	{"/trips", "/trips/dispatch", "", "trips.html:179 (the console dispatch form)"},
	{"/trips", "/trips/arrive-unsolicited", "", "trips.html:248 (the unsolicited arrival form)"},
	// ⟦D-78, 2026-09-05⟧ THE TENTH ROW IS STRUCK WITH ITS FORM.
	// ⟦was: {"/trips/detail", "/trips/cancel", "", "trips_detail.html:38 (the open-trip cancel row)"}⟧
	// D-73 ruled that no trip is cancelled; D-78 closed POST /trips/cancel
	// and removed the button that posted to it. This file's header says the
	// census is amended only at a criteria lock — the authority here is
	// James's ruling (reports/JAMES-DECISIONS.md D-78), not a build's say-so.
	//
	// ⚠ HOW THIS STRIKE IS WITNESSED, stated honestly rather than overclaimed
	// [MEASURED 2026-09-05]. This walk only counts forms targeting a
	// takes-input route, and D-78 strikes the /trips/cancel row from
	// w2RouteTable in the SAME pass — so /trips/cancel leaves the takes-input
	// set and the still-served form (before the button removal landed) is not
	// discovered here at all. This census strike therefore does NOT go red on
	// its own; it is a consistency amendment that keeps this census in step
	// with the route table's strike, and it stays green at 9/9. The strike
	// that IS watched red is the route-walk's: TestW2RouteWalkEveryPOSTRouteClassified
	// reported "UNCLASSIFIED POST route /trips/cancel" with the row struck and
	// the registration still present [watched red 2026-09-05], and closing the
	// route is what turned it green. The button removal is witnessed by
	// TestOperatorConsoleCarriesTheWorkingSurfaceAndTheDetailViewCarriesTheRest
	// (no /trips/cancel form on the page) and by
	// TestD78ThreeOnTheRoadSurviveTheOldCancelPost (the old POST authors nothing).
}

// --- the provisioned fixture world --------------------------------------

// newW2ProvisionedWorld builds ONE site-a server in which every
// conditional section that serves a takes-input form has the state it
// needs to render: people on site (the check-out picker), an incomplete
// check-in (the resume notice), an inbound expectation (the gate inbound
// board AND the trips inbound board), and an open uncontrolled run (the
// returns board; until D-78 also an open trip's cancel row on /trips/detail).
func newW2ProvisionedWorld(t *testing.T) *Server {
	t.Helper()
	srv, st, persons := newW2Server(t, w2Base-2*w2Hour)
	st.Clock = w2Reading(w2Base-w2Hour, store.ClockSynced)
	// Dana + Pat on site.
	w2CheckIn(t, st, persons[:2])
	// The crash-window incomplete check-in (dispatch half only) — Chris.
	if _, err := domain.DispatchLocalCheckIn(st, "site-a", 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "T99", DriverPersonID: persons[2].PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "T99"), DriverPersonID: persons[2].PersonID,
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// An inbound expectation: site-b dispatched TRK-100 here.
	pb, err := domain.CreatePerson(st, "site-b", 1, "Nadeau, Pierre", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	dest, dl := "site-a", int64(90)
	if _, err := domain.Dispatch(st, "site-b", 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRK-100", DriverPersonID: pb.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, st, "site-b", "TRK-100"), DriverPersonID: pb.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &dl,
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// An open uncontrolled run authored here: Dana out to the pad (leaves
	// Pat on site for the pickers; the run stays open, so the returns
	// board offers its manifest and the detail table an open row).
	pad := "conneco"
	if _, err := domain.Dispatch(st, "site-a", 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "PAD RUN", DriverPersonID: persons[0].PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "PAD RUN"), DriverPersonID: persons[0].PersonID,
		DestinationUncontrolled: &pad,
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	st.Clock = w2Reading(w2Base, store.ClockSynced)
	return srv
}

// --- the walk ------------------------------------------------------------

func TestW2EveryServedTakesInputFormCarriesTheTimeControl(t *testing.T) {
	root := w2ModuleRoot(t)
	srv := newW2ProvisionedWorld(t)

	takesInput := map[string]bool{}
	for _, r := range w2RouteTable {
		if r.Class == w2TakesInput {
			takesInput[r.Route] = true
		}
	}

	pages := w2GetRegistrationsInDir(t, filepath.Join(root, w2WebDir))
	if len(pages) == 0 {
		t.Fatal("W2-11: the screen enumeration discovered zero GET registrations — nothing was rendered, so nothing below means anything")
	}

	// Render EVERY page and collect every form instance targeting a
	// takes-input route.
	var discovered []w2FormInstance
	isPage := map[string]bool{}
	for _, page := range pages {
		isPage[page] = true
	}
	for _, page := range pages {
		rec := w2Get(t, srv, page, nil)
		// ⟦2026-09-10, the default page⟧ A REDIRECT-ONLY ROUTE serves no
		// forms of its own: "GET /{$}" answers 302 to /gate (web.go
		// Handler — the officer's front door). It is admitted here ONLY when
		// its target is itself a page this walk renders, so a redirect can
		// never hide a form: the forms live on the target, and the target is
		// walked. Anything else non-200 is still the unrenderable-screen
		// fatal it always was. ⚠ The literal the walk sees for the catch-all
		// is "GET /", whose request path IS the root, so this arm is what the
		// root now exercises; the exact-root literal "GET /{$}" requests the
		// path "/{$}", which the catch-all renders as the status page.
		if rec.Code == 302 || rec.Code == 303 { // http.StatusFound / StatusSeeOther; this file does not import net/http
			if loc := rec.Header().Get("Location"); !isPage[loc] {
				t.Fatalf("W2-11: GET %s = %d to %q, which is NOT a page this walk renders — a redirect to an unwalked target could hide a takes-input form", page, rec.Code, loc)
			}
			continue
		}
		if rec.Code != 200 {
			t.Fatalf("W2-11: GET %s = %d — every served page must render against the provisioned world (an unrenderable screen hides its forms): %s", page, rec.Code, rec.Body.String())
		}
		markup := w2StripNonMarkup(rec.Body.String())
		for _, inst := range w2FormInstances(t, page, markup) {
			var hits []string
			for _, tgt := range inst.Targets {
				if takesInput[tgt] {
					hits = append(hits, tgt)
				}
			}
			if len(hits) > 1 {
				t.Fatalf("W2-11: a form on %s targets %d takes-input routes (%v) — one instance, one authoring target, or the census cannot mean anything", page, len(hits), hits)
			}
			if len(hits) == 1 {
				inst.Targets = hits
				discovered = append(discovered, inst)
			}
		}
	}

	// Reconcile against the census, BOTH directions.
	matched := make([]int, len(w2FormCensus))
	for _, inst := range discovered {
		route := inst.Targets[0]
		var candidates []int
		for i, row := range w2FormCensus {
			if row.Page == inst.Page && row.Route == route {
				if row.Marker == "" || strings.Contains(inst.Markup, row.Marker) {
					candidates = append(candidates, i)
				}
			}
		}
		if len(candidates) == 0 {
			t.Errorf("W2-11: DISCOVERED form outside the census: page %s targeting %s — a newly-served form enters the unit set automatically and LOUDLY; amend the census at a criteria lock with its own assertion. Markup:\n%.400s", inst.Page, route, inst.Markup)
			continue
		}
		if len(candidates) > 1 {
			// Same page+route: the marker must split them (the two
			// /trips arrive boards); ambiguity is a census defect.
			t.Errorf("W2-11: form on %s targeting %s matches %d census rows — markers failed to distinguish the siblings", inst.Page, route, len(candidates))
			continue
		}
		i := candidates[0]
		matched[i]++

		// THE PER-INSTANCE ASSERTIONS (m15 dies here, per instance):
		row := w2FormCensus[i]
		vals := w2SubmitValues(inst.Markup)
		if got := vals.Get(w2TimeSourceField); got != "default" {
			t.Errorf("W2-11 [%s]: the untouched JS-off submission of this form carries %s=%q, want \"default\" — a form without the signal makes every real submission a signal-less POST that W2-9's own 400 refuses: the screen cannot record the event it exists to record (mutant m15, this instance's application)", row.Cite, w2TimeSourceField, got)
		}
		if !regexp.MustCompile(`type="datetime-local"`).MatchString(inst.Markup) {
			t.Errorf("W2-11 [%s]: no datetime-local control in the served form — the collapsed control must exist in the markup a scriptless browser can open (TP §4.5)", row.Cite)
		}
		if !strings.Contains(strings.ToLower(inst.Markup), w2AffordancePhrase) {
			t.Errorf("W2-11 [%s]: no %q affordance in the served form (TP §4.5's explicit affordance)", row.Cite, w2AffordancePhrase)
		}
	}
	for i, row := range w2FormCensus {
		if matched[i] == 0 {
			t.Errorf("W2-11: census instance NEVER SURFACED by any render: %s (page %s → %s) — a missing instance, whether unrendered (under-provisioned fixture) or deleted (amend the census at a criteria lock); never a silent pass", row.Cite, row.Page, row.Route)
		}
	}
	if t.Failed() {
		t.Fatalf("W2-11: served-form walk failed — %d instances discovered against the %d-instance census", len(discovered), len(w2FormCensus))
	}
	// Anti-vacuity floor (behavioural, not a CONTROL row — §6's stated
	// reason: fewer than the ten measured instances discovered is red on
	// the CLEAN tree, so a mis-aimed enumeration, a broken parse or an
	// under-provisioned fixture cannot pass): the census match above
	// already guarantees ≥10 discoveries; this line records the number for
	// the ledger.
	t.Logf("W2-11: %d served takes-input form instances discovered and asserted (census: %d)", len(discovered), len(w2FormCensus))
}
