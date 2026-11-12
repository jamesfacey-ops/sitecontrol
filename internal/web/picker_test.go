// picker_test.go — U2's done-when tests for the person picker and for THE
// SAFETY RULE that governs the whole chunk:
//
//	which default direction is safe depends on whether the action ADDS or
//	REMOVES someone from the muster, not on the widget
//
// Every action that moves a person is tested here for its own default,
// separately, with the direction spelled out in the failure message — never
// by asserting that two widgets look alike. Three of these four actions use
// the same checkbox control and two of them default the opposite way from
// the other two; a test that generalised would defend nothing.
package web

import (
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// --- fixture ----------------------------------------------------------

type consoleWorld struct {
	st *store.Store
	// onSite / offSite are roster people, split by whether they have an
	// open visit at site-b (the server's own site).
	onSite  []domain.Person
	offSite []domain.Person
	// visitOf maps a person id to their open visit id.
	visitOf map[string]string
}

// newConsoleTestServer seeds two companies, two workgroups each, eight
// people — four checked in at site-b and four not — plus site-a as a peer.
// The split is what makes the scoping assertions meaningful: without people
// who are NOT on site, "the dispatch picker is scoped to on-site people" is
// true by accident.
func newConsoleTestServer(t *testing.T) (*Server, *consoleWorld) {
	t.Helper()
	srv, st, _ := newTripsTestServer(t)
	w := &consoleWorld{st: st, visitOf: map[string]string{}}

	// Names are deliberately NOT in the order the people are created, so a
	// picker that renders in creation/person_id order shows visibly wrong
	// ordering (person ids are crypto/rand hex — measured random w.r.t.
	// name in design/bulk-movement-v1.md).
	type seed struct{ company, trade, name string }
	seeds := []seed{
		{"Zulu Haulage", "Night Crew", "Wanda Zephyr"},
		{"Alpha Drilling", "Day Crew", "Mo Nakamura"},
		{"Zulu Haulage", "Day Crew", "Bo Ainsworth"},
		{"Alpha Drilling", "Night Crew", "Yara Quist"},
		{"Alpha Drilling", "Day Crew", "Ada Beaufort"},
		{"Zulu Haulage", "Night Crew", "Cy Delacroix"},
		{"Alpha Drilling", "Night Crew", "Eli Frost"},
		{"Zulu Haulage", "Day Crew", "Nia Okpik"},
	}
	companyID := map[string]string{}
	tradeID := map[string]string{}
	var persons []domain.Person
	for _, s := range seeds {
		if _, ok := companyID[s.company]; !ok {
			co, err := domain.CreateCompany(st, "site-b", 1, s.company, 0, "default")
			if err != nil {
				t.Fatal(err)
			}
			companyID[s.company] = co.CompanyID
		}
		// ⟦LOCK — §60, 2026-09-03⟧ ARITY SITE 4 — DISPOSITION (3): the FIXTURE KEY is
		// re-expressed, and this CORRECTS an earlier routing by this same pass.
		// ⚠ It was first routed on the reading that "the picker groups by company then
		// trade", so collapsing four company-scoped trades into two site-wide ones might
		// change the rendered grouping. ⭐ MEASURED, and that reading was WRONG: the picker
		// groups on the trade NAME, not its id — rosterGroups resolves `trade =
		// tradeName[*p.TradeID]` and hands a STRING to groupBuilder.add(company, trade
		// string, …) (picker.go:169, :263-274). Two companies sharing one "Night Crew"
		// trade row therefore render exactly as they did when each owned its own: same
		// headings, same members, same order.
		// ⟦DR-W8-2⟧ makes the company half of this key meaningless — a trade is site-wide —
		// so keying on the trade name alone is what the fixture now means, and the
		// discrimination under test (name ordering across company/trade groups) is
		// untouched.
		// ⟦was: "key := s.company + \"/\" + s.trade"⟧
		// ⟦was: "wg, err := domain.CreateTrade(st, \"site-b\", 1, companyID[s.company], s.trade, 0, \"default\")"⟧
		key := s.trade
		if _, ok := tradeID[key]; !ok {
			wg, err := domain.CreateTrade(st, "site-b", 1, s.trade, 0, "default")
			if err != nil {
				t.Fatal(err)
			}
			tradeID[key] = wg.TradeID
		}
		coID, wgID := companyID[s.company], tradeID[key]
		p, err := domain.CreatePerson(st, "site-b", 1, s.name, &coID, &wgID, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		persons = append(persons, p)
	}

	// Check the first four onto site-b through the ordinary machinery.
	var slots []domain.PersonSlot
	for _, p := range persons[:4] {
		slots = append(slots, domain.PersonSlot{ExistingID: p.PersonID})
		w.onSite = append(w.onSite, p)
	}
	w.offSite = append(w.offSite, persons[4:]...)
	if _, _, err := domain.GateCheckIn(st, "site-b", 1, domain.GateCheckInInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "CREW BUS", Driver: slots[0], Occupants: slots[1:],"⟧
		VehicleID: w7SeedVehicleID(t, st, "site-b", "CREW BUS"), Driver: slots[0], Occupants: slots[1:],
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	open, err := domain.OpenVisitsAt(st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 4 {
		t.Fatalf("fixture: %d open visits at site-b, want 4", len(open))
	}
	for _, v := range open {
		w.visitOf[v.PersonID] = v.VisitID
	}
	return srv, w
}

// --- markup helpers ---------------------------------------------------

// pickerMarkup returns the rendered markup of one picker by its id. The
// page is style-stripped first: a class name in the stylesheet is not a
// rendered element (this repo has been bitten twice by that).
func pickerMarkup(t *testing.T, page, id string) string {
	t.Helper()
	page = styleRe.ReplaceAllString(page, "")
	start := strings.Index(page, `<div class="picker" id="`+id+`"`)
	if start < 0 {
		t.Fatalf("no picker with id %q on the page", id)
	}
	rest := page[start:]
	// Brace-free scan: the picker ends at the closing </div> that balances
	// its own opening tag.
	depth := 0
	for i := 0; i < len(rest); i++ {
		if strings.HasPrefix(rest[i:], "<div") {
			depth++
		} else if strings.HasPrefix(rest[i:], "<details") {
			depth++
		} else if strings.HasPrefix(rest[i:], "</div>") {
			depth--
			if depth == 0 {
				return rest[:i+len("</div>")]
			}
		} else if strings.HasPrefix(rest[i:], "</details>") {
			depth--
		}
	}
	t.Fatalf("unterminated picker %q", id)
	return ""
}

// ⟦2026-09-10, the on-site sign-in date⟧ THIS SELECTOR IS THE TEST'S EYE,
// NOT ITS SUBJECT. The row may now END with one trailing
// `<span class="picker-when" …>…</span>` — the sign-in date the gate's
// on-site roster shows (gate_signin.go, _picker.html) — and nothing else.
// The optional group admits exactly that span; group 5 is still the BARE
// name, so every consumer below (the sort, the same-name pair, the bare
// visit id) reads what it always read. A row that grows anything else
// still fails to match, as before.
// ⟦was: "…([^>]*)> ([^<]*)</label>`)"⟧
var pickerRowRe = regexp.MustCompile(`<label class="picker-row"><input type="(checkbox|radio)" name="([^"]+)" value="([^"]+)"([^>]*)> ([^<]*)(?:<span class="picker-when"[^>]*>[^<]*</span>)?</label>`)

// vehicleFormClassRe matches "vehicle-form" as a whole CSS class token
// inside a class="..." attribute, whether it is the only class or one of
// several (⟦R-28⟧ gate_vehicle_form's Compact instantiation now also
// carries "vehicle-form-compact" in the same attribute) — a class-list
// match, the same thing a real `.vehicle-form` CSS/JS selector matches,
// never a literal-attribute-value match a second token would break.
var vehicleFormClassRe = regexp.MustCompile(`class="[^"]*\bvehicle-form\b[^"]*"`)

type renderedRow struct {
	kind, field, value, attrs, name string
}

func pickerRows(t *testing.T, page, id string) []renderedRow {
	t.Helper()
	var out []renderedRow
	for _, m := range pickerRowRe.FindAllStringSubmatch(pickerMarkup(t, page, id), -1) {
		out = append(out, renderedRow{m[1], m[2], m[3], m[4], m[5]})
	}
	return out
}

// --- the on-site roster TABLE's own row shape (GATE-ACCESS-STYLE, ---------
// James, 2026-09-12) ---------------------------------------------------
//
// checkout-picker's row is no longer pickerRowRe's shape: it carries five
// named cells (Last, First, Company, Time, Date — gate.go's
// buildRosterTableRows) instead of one bare name, because the roster is
// now a real table (design/ACCESS-APP-LAYOUT.md §2), not a picker of
// unlabelled rows. Deliberately a SEPARATE regex from pickerRowRe, which
// stays untouched and keeps proving the SAME "grows anything else, fails
// to match" guarantee for the driver/occupant/arrival pickers that still
// use it — widening the shared regex to admit this shape would let ANY of
// those grow the same five spans with nothing failing.
var (
	rosterRowBlockRe = regexp.MustCompile(`(?s)<label class="picker-row">(.*?)</label>`)
	rosterInputRe    = regexp.MustCompile(`<input type="(checkbox|radio)" name="([^"]+)" value="([^"]+)"([^>]*)>`)
	rosterLastRe     = regexp.MustCompile(`<span class="roster-cell roster-cell-last">([^<]*)</span>`)
	rosterFirstRe    = regexp.MustCompile(`<span class="roster-cell roster-cell-first">([^<]*?) ?(?:<span class="roster-ref">\(([^)]*)\)</span>)?</span>`)
	rosterCompanyRe  = regexp.MustCompile(`<span class="roster-cell roster-cell-company">([^<]*)</span>`)
	rosterTimeRe     = regexp.MustCompile(`<span class="roster-cell roster-cell-time"([^>]*)>([^<]*)</span>`)
	rosterDateRe     = regexp.MustCompile(`<span class="roster-cell roster-cell-date"([^>]*)>([^<]*)</span>`)
)

// rosterRow is one parsed row of the roster table. name is a convenience
// reconstruction ("Last, First (person id …tail)" when the row is
// same-name-tagged) kept so callers that only ever cared about "does this
// row read as a distinct thing from that one" — same-name pair tests —
// need no rewrite of their own comparisons.
type rosterRow struct {
	kind, field, value, attrs string
	last, first, ref, company string
	timeAttrs, timeText       string
	dateAttrs, dateText       string
	name                      string
}

func rosterRows(t *testing.T, page, id string) []rosterRow {
	t.Helper()
	var out []rosterRow
	for _, blk := range rosterRowBlockRe.FindAllStringSubmatch(pickerMarkup(t, page, id), -1) {
		body := blk[1]
		in := rosterInputRe.FindStringSubmatch(body)
		if in == nil {
			t.Fatalf("roster row has no recognisable checkbox/radio input: %s", body)
			continue
		}
		r := rosterRow{kind: in[1], field: in[2], value: in[3], attrs: in[4]}
		if m := rosterLastRe.FindStringSubmatch(body); m != nil {
			r.last = m[1]
		}
		if m := rosterFirstRe.FindStringSubmatch(body); m != nil {
			r.first, r.ref = m[1], m[2]
		}
		if m := rosterCompanyRe.FindStringSubmatch(body); m != nil {
			r.company = m[1]
		}
		if m := rosterTimeRe.FindStringSubmatch(body); m != nil {
			r.timeAttrs, r.timeText = m[1], m[2]
		}
		if m := rosterDateRe.FindStringSubmatch(body); m != nil {
			r.dateAttrs, r.dateText = m[1], m[2]
		}
		r.name = strings.TrimSpace(r.last + ", " + r.first)
		if r.ref != "" {
			r.name += " (" + r.ref + ")"
		}
		out = append(out, r)
	}
	return out
}

// padReturnServer renders a /trips page carrying BOTH arrival surfaces: an
// inbound expectation from the peer site AND an open run out to an
// uncontrolled pad, which is what makes the pad-return manifest render.
// V-06 exists because a fixture with only the first made every page-wide
// scan vacuous over the second.
func padReturnServer(t *testing.T) *Server {
	t.Helper()
	srv, w := newDestinationsTestServer(t)
	st, persons := w.st, w.persons

	injectForeignTrip(t, st, 1, "BUS 7", 60, persons[0].PersonID, persons[1].PersonID)

	walkIn, err := domain.DispatchLocalCheckIn(st, "site-b", 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "gate walk-in", DriverPersonID: persons[0].PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, st, "site-b", "gate walk-in"), DriverPersonID: persons[0].PersonID,
		OccupantPersonIDs: []string{persons[1].PersonID},
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.RecordExpectedArrival(st, "site-b", 1, walkIn.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	rec := postForm(t, srv, "/trips/dispatch", url.Values{
		"time_source":         {"default"},
		"vehicle":             {"PAD RUNNER 1"},
		"driver_person_id":    {persons[0].PersonID},
		"occupant_person_ids": {persons[1].PersonID},
		"destination_id":      {"conneco"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("uncontrolled dispatch = %d: %s", rec.Code, rec.Body.String())
	}
	return srv
}

// --- THE SAFETY RULE, one action at a time ----------------------------

// TestEveryPersonMovingActionDefaultsInItsOwnSafeDirection is the chunk's
// central test. Each case is re-derived, never generalised: two of these
// actions default to "included" and two to "not selected", and they use the
// same widget.
//
// Each case asserts BOTH halves:
//   - the rendered markup carries no pre-set state that contradicts the
//     default (no `checked`, no pre-selected option);
//   - the BEHAVIOUR of submitting the form untouched matches the stated
//     default, measured on store state.
func TestEveryPersonMovingActionDefaultsInItsOwnSafeDirection(t *testing.T) {
	t.Run("dispatch occupants: unticked = NOT on the vehicle (REMOVE direction)", func(t *testing.T) {
		srv, w := newConsoleTestServer(t)
		page := getTrips(t, srv)
		rows := pickerRows(t, page, "occupant-picker")
		if len(rows) != len(w.onSite) {
			t.Fatalf("precondition: occupant picker offers %d people, want the %d on site", len(rows), len(w.onSite))
		}
		for _, r := range rows {
			if strings.Contains(r.attrs, "checked") {
				t.Fatalf("%q is pre-ticked as an occupant. A dispatch REMOVES people from this site's muster: a pre-ticked list means one hurried submit checks out people who are still standing here, and nobody goes looking for them. Unticked is the safe default because a missed tick only ever leaves somebody ON the muster.", r.name)
			}
		}

		// Behaviour: submitting with only a driver must leave every other
		// on-site person exactly where they were.
		rec := postForm(t, srv, "/trips/dispatch", url.Values{
			"time_source": {"default"},
			"vehicle":     {"T1"}, "driver_person_id": {w.onSite[0].PersonID},
			"destination_site": {"site-a"}, "deadline_minutes": {"60"},
		})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("dispatch = %d: %s", rec.Code, rec.Body.String())
		}
		open, err := domain.OpenVisitsAt(w.st, "site-b")
		if err != nil {
			t.Fatal(err)
		}
		if len(open) != len(w.onSite)-1 {
			t.Fatalf("a dispatch with no occupants ticked removed %d people from the muster, want 1 (the driver only) — an unticked box must never mean 'this person left'", len(w.onSite)-len(open))
		}
	})

	t.Run("gate check-out: unticked = staying on site (REMOVE direction)", func(t *testing.T) {
		srv, st, _ := newGateTestServer(t)
		ids := checkInCrew(t, srv, st, 5)
		_, page := getGate(t, srv)
		rows := rosterRows(t, page, "checkout-picker")
		if len(rows) != len(ids) {
			t.Fatalf("precondition: check-out picker offers %d rows, want %d", len(rows), len(ids))
		}
		for _, r := range rows {
			if strings.Contains(r.attrs, "checked") {
				t.Fatalf("%q is pre-ticked for check-out. A check-out REMOVES somebody from the muster: a stale page rendered before they walked back in would clear a person who is still here. Unticked is safe because the worst a missed tick does is over-report the muster.", r.name)
			}
		}

		// Behaviour: an empty submit must author nothing and say so.
		rec := postForm(t, srv, "/gate/checkout", url.Values{"time_source": {"default"}})
		if rec.Code != http.StatusConflict {
			t.Fatalf("empty check-out = %d, want a 409 refusal that explains itself: %s", rec.Code, rec.Body.String())
		}
		open, err := domain.OpenVisitsAt(st, "site-a")
		if err != nil {
			t.Fatal(err)
		}
		if len(open) != len(ids) {
			t.Fatalf("an empty check-out removed %d people from the muster", len(ids)-len(open))
		}
	})

	// V-06. This subtest scans the WHOLE page for `stayed_person_ids`
	// controls, and BOTH arrival surfaces emit that name: the inbound board
	// (trips.html) and the pad-return manifest (trips.html, "Back from a
	// pad"). It used to run on a fixture that rendered only the first, so
	// the page-wide regex was vacuous over the second — adding `checked` to
	// the return manifest left the whole suite green, measured.
	//
	// The return manifest is the FIFTH person-moving surface and its
	// derivation was copied by its own comment. It is NOT a picker, so the
	// picker's structural no-`checked` protection does not reach it and
	// there is no backstop anywhere else. The fixture below therefore
	// renders both surfaces at once, and asserts (rule 6) that it really
	// did before drawing any conclusion from the scan.
	t.Run("inbound arrival AND pad return: unticked = CHECKED IN (ADD direction)", func(t *testing.T) {
		srv, w := newDestinationsTestServer(t)
		st, persons := w.st, w.persons

		// Surface 1: an inbound expectation from the other site.
		tripID := injectForeignTrip(t, st, 1, "BUS 7", 60, persons[0].PersonID, persons[1].PersonID)

		// Surface 2: an OPEN run out to an uncontrolled pad, which is what
		// renders the return manifest. Both people go out, so the manifest
		// carries rows rather than being an empty section.
		walkIn, err := domain.DispatchLocalCheckIn(st, "site-b", 1, domain.DispatchInput{
			// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "gate walk-in", DriverPersonID: persons[0].PersonID,"⟧
			VehicleID: w7SeedVehicleID(t, st, "site-b", "gate walk-in"), DriverPersonID: persons[0].PersonID,
			OccupantPersonIDs: []string{persons[1].PersonID},
		}, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := domain.RecordExpectedArrival(st, "site-b", 1, walkIn.TripID, nil, 0, "default"); err != nil {
			t.Fatal(err)
		}
		rec := postForm(t, srv, "/trips/dispatch", url.Values{
			"time_source":         {"default"},
			"vehicle":             {"PAD RUNNER 1"},
			"driver_person_id":    {persons[0].PersonID},
			"occupant_person_ids": {persons[1].PersonID},
			"destination_id":      {"conneco"},
		})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("uncontrolled dispatch = %d: %s", rec.Code, rec.Body.String())
		}

		page := styleRe.ReplaceAllString(getTrips(t, srv), "")

		// RULE 6 PRECONDITION, and it is the whole point of this edit: the
		// scan below proves nothing about a surface that did not render.
		// Both must be on the page, asserted by their own distinguishing
		// copy, before the shared regex is allowed to mean anything.
		if !strings.Contains(page, "Arrived — check in occupants (unticked rows)") {
			t.Fatalf("precondition: the INBOUND board did not render — the scan below would not be looking at it:\n%s", page)
		}
		if !strings.Contains(page, "Returned — check in everyone not marked as stayed") {
			t.Fatalf("precondition: the PAD RETURN manifest did not render — this is the surface V-06 is about, and a green scan here would mean nothing:\n%s", page)
		}
		if !strings.Contains(page, "stayed at Conneco Pad — remains off site") {
			t.Fatal("precondition: the return manifest rendered no per-person rows")
		}
		controls := regexp.MustCompile(`<input[^>]*stayed_person_ids[^>]*>`).FindAllString(page, -1)
		// Two surfaces × two people. Asserting the COUNT is what stops a
		// future refactor from quietly dropping one surface back out of the
		// scan's reach without failing a precondition.
		if len(controls) != 4 {
			t.Fatalf("page carries %d stayed controls, want 4 (2 inbound occupants + 2 pad returners) — the scan is not covering both surfaces", len(controls))
		}
		for _, m := range controls {
			if strings.Contains(m, "checked") {
				t.Fatalf("a stayed control is pre-ticked: %s. An arrival ADDS people to the muster. Here 'ticked' means NOT checked in, so a pre-ticked box would leave a person who is physically standing at the gate — or waiting at a pad with no gate and no timer — off the muster entirely, which is the direction that gets somebody left behind. Unticked (= everyone in) is the safe default, and it is the exact opposite of the dispatch and check-out pickers on purpose.", m)
			}
		}

		// Behaviour: submitting the inbound manifest untouched checks
		// EVERYONE in. (The pad run's own return behaviour is measured by
		// TestUncontrolledRunReturnManifestOverHTTP; what is asserted here
		// is the default it renders with.)
		rec = postForm(t, srv, "/trips/arrive", url.Values{"time_source": {"default"}, "trip_id": {tripID}})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("arrive = %d: %s", rec.Code, rec.Body.String())
		}
		open, err := domain.OpenVisitsAt(st, "site-b")
		if err != nil {
			t.Fatal(err)
		}
		if len(open) != 2 {
			t.Fatalf("an untouched arrival created %d visits, want 2 — every occupant must be checked in unless explicitly excepted", len(open))
		}
	})

	t.Run("unsolicited arrival: unticked = not aboard, and the empty case is refused (ADD direction)", func(t *testing.T) {
		srv, w := newConsoleTestServer(t)
		page := getTrips(t, srv)
		rows := pickerRows(t, page, "arrival-picker")
		if len(rows) == 0 {
			t.Fatal("precondition: the arrival picker rendered no people at all")
		}
		for _, r := range rows {
			if strings.Contains(r.attrs, "checked") {
				t.Fatalf("%q is pre-ticked as aboard. This action ADDS people to the muster, but its candidate list is the ROSTER, not a manifest — there is no defensible 'everyone' to pre-tick, so the omission risk is defended by the confirm's count and by the server refusing an empty selection instead.", r.name)
			}
		}

		// Behaviour: the dangerous direction (omission) is defended by a
		// refusal, not by a silent no-op.
		rec := postForm(t, srv, "/trips/arrive-unsolicited", url.Values{
			"time_source": {"default"},
			"vehicle":     {"MYSTERY 1"}, "origin_site": {"site-a"},
		})
		if rec.Code != http.StatusConflict {
			t.Fatalf("arrival with nobody selected = %d, want a 409 refusal: %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "at least one occupant is required") {
			t.Fatalf("the refusal does not explain itself: %s", rec.Body.String())
		}
		if _, err := domain.OpenVisitsAt(w.st, "site-b"); err != nil {
			t.Fatal(err)
		}
	})
}

// --- the multi-select is gone ----------------------------------------

func TestNoRenderedPageStillOffersAMultiSelectPersonList(t *testing.T) {
	selectRe := regexp.MustCompile(`<select[^>]*>`)
	for route, body := range renderedPages(t) {
		for _, s := range selectRe.FindAllString(styleRe.ReplaceAllString(body, ""), -1) {
			if strings.Contains(s, "multiple") {
				t.Errorf("%s still renders a multi-select (%s) — a stray plain click in one of those silently discards the whole selection, which is what U2 removed", route, s)
			}
		}
	}
}

// --- sorted, and provably so -----------------------------------------

// TestPickerRowsAreSortedByNameInsideEveryGroup asserts its own
// precondition: the fixture's person ids must NOT already be in name order,
// or a picker that did no sorting at all would pass.
func TestPickerRowsAreSortedByNameInsideEveryGroup(t *testing.T) {
	srv, w := newConsoleTestServer(t)

	all := append(append([]domain.Person{}, w.onSite...), w.offSite...)
	byID := append([]domain.Person{}, all...)
	sort.Slice(byID, func(i, j int) bool { return byID[i].PersonID < byID[j].PersonID })
	byName := append([]domain.Person{}, all...)
	sort.Slice(byName, func(i, j int) bool { return byName[i].Name < byName[j].Name })
	same := true
	for i := range byID {
		if byID[i].PersonID != byName[i].PersonID {
			same = false
		}
	}
	if same {
		t.Skip("precondition: this fixture's random person ids happened to land in name order, so ordering cannot be demonstrated here")
	}

	page := getTrips(t, srv)
	for _, id := range []string{"driver-picker", "occupant-picker", "arrival-picker"} {
		markup := pickerMarkup(t, page, id)
		// Names must ascend inside each workgroup block.
		for _, sub := range strings.Split(markup, `<div class="picker-sub">`)[1:] {
			var names []string
			for _, m := range pickerRowRe.FindAllStringSubmatch(sub, -1) {
				names = append(names, m[5])
			}
			for i := 1; i < len(names); i++ {
				if names[i-1] > names[i] {
					t.Errorf("%s: %q comes before %q — the picker is not in name order, which is the measured defect (person ids are crypto/rand hex, so the old person_id order was random w.r.t. name)", id, names[i-1], names[i])
				}
			}
		}
	}
}

// --- no unscoped select-all ------------------------------------------

var selectAllRe = regexp.MustCompile(`<label class="picker-all"[^>]*><input type="checkbox" data-picker-all="(group|sub)"> all (\d+) in ([^<]*)</label>`)

// TestNoPickerOffersAnUnscopedSelectAll: bulk-movement-v1.md calls an
// unscoped select-all "the single worst failure mode possible here". Every
// select-all must name the ONE company or workgroup it belongs to and the
// count it will tick, and no select-all may cover the whole picker when the
// picker has more than one group.
func TestNoPickerOffersAnUnscopedSelectAll(t *testing.T) {
	srv, _ := newConsoleTestServer(t)
	page := getTrips(t, srv)

	for _, id := range []string{"occupant-picker", "arrival-picker"} {
		markup := pickerMarkup(t, page, id)
		alls := selectAllRe.FindAllStringSubmatch(markup, -1)
		if len(alls) == 0 {
			t.Fatalf("precondition: %s rendered no select-all at all, so this test proves nothing about scoping", id)
		}
		total := len(pickerRows(t, page, id))
		groups := strings.Count(markup, `<div class="picker-group">`)
		for _, m := range alls {
			n, err := strconv.Atoi(m[2])
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(m[3]) == "" {
				t.Errorf("%s: a select-all names no scope: %s", id, m[0])
			}
			if groups > 1 && n >= total {
				t.Errorf("%s: a select-all covers %d of %d people across %d groups — that is an unscoped select-all: %s", id, n, total, groups, m[0])
			}
		}
	}

	// The driver picker is a radio group: "all drivers" is not a thing, and
	// offering it would be nonsense rather than merely unsafe.
	if strings.Contains(pickerMarkup(t, page, "driver-picker"), "data-picker-all") {
		t.Error("the driver picker offers a select-all")
	}
}

// --- scoping: filtered one way, ordered the other --------------------

// TestRemoveDirectionPickersAreFilteredToPeopleOnSite. Dropping somebody
// from a REMOVE-direction list can only fail by not removing them, which
// over-reports the muster — the safe direction — so these lists really are
// filtered.
func TestRemoveDirectionPickersAreFilteredToPeopleOnSite(t *testing.T) {
	srv, w := newConsoleTestServer(t)
	if len(w.offSite) == 0 {
		t.Fatal("precondition: the fixture has nobody off site, so 'filtered to on-site' is true by accident")
	}
	page := getTrips(t, srv)

	for _, id := range []string{"driver-picker", "occupant-picker"} {
		got := map[string]bool{}
		for _, r := range pickerRows(t, page, id) {
			got[r.value] = true
		}
		for _, p := range w.onSite {
			if !got[p.PersonID] {
				t.Errorf("%s omits %q, who IS recorded on site — a dispatch picker that cannot name somebody standing here forces the operator around the system", id, p.Name)
			}
		}
		for _, p := range w.offSite {
			if got[p.PersonID] {
				t.Errorf("%s offers %q, who is not recorded on site here — dispatching them would author a departure for somebody the record never had arriving", id, p.Name)
			}
		}
	}
}

// TestAddDirectionPickerIsOrderedButNeverFiltered. An arrival ADDS people
// to the muster, so a name missing from the picker is the same failure as a
// name nobody ticked: a person who is physically here and on no list.
// Nobody may be dropped — the scoping is ordering plus a collapsed section.
func TestAddDirectionPickerIsOrderedButNeverFiltered(t *testing.T) {
	srv, w := newConsoleTestServer(t)
	page := getTrips(t, srv)
	rows := pickerRows(t, page, "arrival-picker")

	offered := map[string]bool{}
	for _, r := range rows {
		if r.field != "person_ids" {
			t.Errorf("arrival picker row submits under %q, not the pre-existing person_ids field", r.field)
		}
		if offered[r.value] {
			t.Errorf("%q appears twice in the arrival picker — one person, two tick-boxes, and their id posted twice", r.name)
		}
		offered[r.value] = true
	}
	for _, p := range append(append([]domain.Person{}, w.onSite...), w.offSite...) {
		if !offered[p.PersonID] {
			t.Errorf("the arrival picker does not offer %q. An arrival picker that cannot name somebody who is standing in front of the operator omits them from the muster, which is the direction that gets people left behind — this list is ORDERED, never filtered", p.Name)
		}
	}

	// Precondition + behaviour: the ordering really is doing something —
	// there is a collapsed "everyone else" section, and it is not where the
	// people with an open record elsewhere ended up.
	markup := pickerMarkup(t, page, "arrival-picker")
	if !strings.Contains(markup, "<details class=\"picker-section\"") {
		t.Log("no collapsed section in this fixture (nobody is recorded elsewhere) — ordering is a no-op here, which is correct")
	}
}

// --- works with JavaScript off ---------------------------------------

// TestPickerWorksWithJavaScriptDisabled drives the action using ONLY what
// the served HTML contains: the form's action and method, and the checkbox
// values in the markup. No script runs in a Go test, so this is exactly the
// JS-off path — a half-loaded page in a shack must not silently drop a
// check-in.
func TestPickerWorksWithJavaScriptDisabled(t *testing.T) {
	srv, w := newConsoleTestServer(t)
	page := styleRe.ReplaceAllString(getTrips(t, srv), "")

	// The form really does carry a server action and method.
	if !strings.Contains(page, `<form method="post" action="/trips/dispatch"`) {
		t.Fatal("the dispatch form is not a plain server-posting form")
	}
	// The enhancements that only work with a script are HIDDEN in the
	// served markup, so a JS-off browser never shows a control that does
	// nothing when clicked.
	for _, frag := range []string{`<div class="picker-search" hidden>`, `<label class="picker-all" hidden>`} {
		if !strings.Contains(page, frag) {
			t.Errorf("served markup does not hide a script-only control (%s) — with JS off it would look live and do nothing", frag)
		}
	}

	// Now act as a JS-less browser: tick two occupants straight from the
	// markup and post.
	rows := pickerRows(t, page, "occupant-picker")
	if len(rows) < 3 {
		t.Fatalf("precondition: only %d occupant rows", len(rows))
	}
	driver := w.onSite[0].PersonID
	var occ []string
	for _, r := range rows {
		if r.value != driver && len(occ) < 2 {
			occ = append(occ, r.value)
		}
	}
	rec := postForm(t, srv, "/trips/dispatch", url.Values{
		"time_source": {"default"},
		"vehicle":     {"NOJS 1"}, "driver_person_id": {driver},
		"occupant_person_ids": occ,
		"destination_site":    {"site-a"}, "deadline_minutes": {"60"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("JS-off dispatch = %d: %s", rec.Code, rec.Body.String())
	}
	open, err := domain.OpenVisitsAt(w.st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if want := len(w.onSite) - 3; len(open) != want {
		t.Fatalf("after a JS-off dispatch of driver + 2 occupants: %d still on site, want %d", len(open), want)
	}
}

// TestGateCheckOutWorksWithJavaScriptDisabled is the same proof for the
// batch check-out: the ticks come straight out of the served markup.
func TestGateCheckOutWorksWithJavaScriptDisabled(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	ids := checkInCrew(t, srv, st, 6)
	_, page := getGate(t, srv)
	rows := rosterRows(t, page, "checkout-picker")
	if len(rows) != len(ids) {
		t.Fatalf("precondition: %d rows for %d visits", len(rows), len(ids))
	}
	var ticked []string
	for _, r := range rows[:3] {
		if r.field != "visit_ids" {
			t.Fatalf("check-out row submits under %q, not visit_ids", r.field)
		}
		ticked = append(ticked, r.value)
	}
	rec := postForm(t, srv, "/gate/checkout", url.Values{"time_source": {"default"}, "visit_ids": ticked})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("JS-off batch check-out = %d: %s", rec.Code, rec.Body.String())
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 3 {
		t.Fatalf("%d people still on site after checking out 3 of 6, want 3", len(open))
	}
}

// --- the safe default is on the page, in words -----------------------

// TestEveryPickerTellsTheOperatorWhichWayAMissedTickFails. The rule this
// chunk turns on is not obvious from the widget, so the page says it. This
// asserts the sentence exists AND that the two directions really are
// different sentences — a copy-pasted note would defeat the point.
func TestEveryPickerTellsTheOperatorWhichWayAMissedTickFails(t *testing.T) {
	srv, _ := newConsoleTestServer(t)
	trips := getTrips(t, srv)
	gateSrv, gateStore, _ := newGateTestServer(t)
	checkInCrew(t, gateSrv, gateStore, 2)
	_, gate := getGate(t, gateSrv)

	notes := map[string]string{}
	for _, tc := range []struct{ id, page string }{
		{"driver-picker", trips}, {"occupant-picker", trips},
		{"arrival-picker", trips}, {"checkout-picker", gate},
	} {
		markup := pickerMarkup(t, tc.page, tc.id)
		i := strings.Index(markup, `<p class="picker-default"><strong>Unticked means:</strong>`)
		if i < 0 {
			t.Errorf("%s does not state which direction a missed tick fails in", tc.id)
			continue
		}
		notes[tc.id] = markup[i : strings.Index(markup[i:], "</p>")+i]
	}
	if notes["occupant-picker"] == notes["arrival-picker"] {
		t.Error("the dispatch and arrival pickers carry the SAME default note, but they default opposite ways — one of them is wrong")
	}
	if notes["checkout-picker"] == notes["arrival-picker"] {
		t.Error("the check-out and arrival pickers carry the same default note, but check-out removes people from the muster and arrival adds them")
	}
	// The remove-direction notes must actually talk about staying on site;
	// the add-direction one about being left off.
	if !strings.Contains(notes["checkout-picker"], "staying on site") {
		t.Errorf("the check-out default note does not say unticked means staying: %q", notes["checkout-picker"])
	}
	if !strings.Contains(notes["arrival-picker"], "NOT checked in") {
		t.Errorf("the arrival default note does not say unticked means not checked in: %q", notes["arrival-picker"])
	}

	// M-04. The two assertions above covered two of the four pickers. The
	// other two were held only by the inequality checks, and an INVERTED
	// sentence differs from its neighbours just as well as a correct one
	// does — so an occupant-picker note that taught the operator the
	// opposite of the truth passed every assertion in this test, measured.
	//
	// That matters most on the occupant picker specifically, because it is
	// the DISPATCH path — the REMOVE direction. Inverted, it tells the
	// operator that ticking is the safe move on the one action that closes
	// the visit of a person still standing on site, and nothing downstream
	// catches it: the picker exists precisely so the operator need not
	// reason about direction, which makes this sentence the mechanism
	// rather than a comment on it.
	//
	// The assertions below pin the DIRECTION, not the wording: each regexp
	// requires the safe half and the dangerous half to stay attached to the
	// right one of "a missed tick" / "a tick you did not mean", within one
	// clause. Rewriting the sentence is free; swapping its two halves is
	// not.
	occ := notes["occupant-picker"]
	if !regexp.MustCompile(`missed tick[^.;]*over-report[^.;]*safe`).MatchString(occ) {
		t.Errorf("the DISPATCH occupant picker does not say that a MISSED tick is the safe direction — inverted, this sentence teaches the operator to tick people off the muster who are still standing here. Note was: %q", occ)
	}
	if !regexp.MustCompile(`tick[^.;]*removes a person who is still (standing|here)`).MatchString(occ) {
		t.Errorf("the DISPATCH occupant picker does not say that a WRONG tick removes somebody still on site: %q", occ)
	}
	if strings.Contains(occ, "checked in") {
		t.Errorf("the DISPATCH occupant picker's note talks about checking IN — this picker checks people OUT of this site: %q", occ)
	}

	drv := notes["driver-picker"]
	if !strings.Contains(drv, "Nobody is pre-selected") {
		t.Errorf("the driver picker does not state that nothing is pre-selected — a pre-filled driver attributes a departure to whoever sorted first: %q", drv)
	}
	if !regexp.MustCompile(`refused until you pick`).MatchString(drv) {
		t.Errorf("the driver picker does not say the dispatch is refused until a driver is chosen: %q", drv)
	}

	// And all four must be four different sentences, not two plus two
	// copies. The pair below is the one the inequality checks above never
	// covered.
	if drv == occ {
		t.Error("the driver and occupant pickers carry the SAME default note, but one selects exactly one person and the other selects any number")
	}
	if occ == notes["checkout-picker"] {
		t.Error("the dispatch and check-out pickers carry the same default note — they are both REMOVE direction, but they remove through different actions and a copy-paste here is how one of them stops being re-derived")
	}
}

// --- the driver control cannot arrive pre-filled ---------------------

// TestDriverIsNeverPreSelected. The control this replaced was a
// `<select required>` with no empty option, and a browser pre-selects the
// first option of such a select — so whoever sorted first was the default
// driver, and a hurried dispatch attributed a departure to them. Radios
// with nothing chosen cannot do that.
func TestDriverIsNeverPreSelected(t *testing.T) {
	srv, _ := newConsoleTestServer(t)
	page := getTrips(t, srv)
	rows := pickerRows(t, page, "driver-picker")
	if len(rows) == 0 {
		t.Fatal("precondition: the driver picker offered nobody")
	}
	for _, r := range rows {
		if r.kind != "radio" {
			t.Fatalf("driver row is a %s — exactly one driver must be choosable, and a checkbox list cannot say that", r.kind)
		}
		if strings.Contains(r.attrs, "checked") {
			t.Fatalf("driver %q is pre-selected", r.name)
		}
		if !strings.Contains(r.attrs, "required") {
			t.Fatalf("driver row %q is not required — with nothing pre-selected, the form must refuse rather than post an empty driver", r.name)
		}
	}
	if strings.Contains(pickerMarkup(t, page, "driver-picker"), "<option") {
		t.Error("the driver control still renders <option> elements")
	}
}

// --- the field names did not move ------------------------------------

// TestPickersSubmitUnderTheExistingFieldNames. This chunk is a
// re-composition of existing surfaces: every POST target and every form
// field name that worked before must still work.
func TestPickersSubmitUnderTheExistingFieldNames(t *testing.T) {
	srv, _ := newConsoleTestServer(t)
	page := getTrips(t, srv)
	want := map[string]string{
		"driver-picker":   "driver_person_id",
		"occupant-picker": "occupant_person_ids",
		"arrival-picker":  "person_ids",
	}
	for id, field := range want {
		rows := pickerRows(t, page, id)
		if len(rows) == 0 {
			t.Fatalf("precondition: %s rendered nothing", id)
		}
		for _, r := range rows {
			if r.field != field {
				t.Fatalf("%s submits under %q, want the pre-existing %q", id, r.field, field)
			}
		}
	}

	gateSrv, gateStore, _ := newGateTestServer(t)
	checkInCrew(t, gateSrv, gateStore, 2)
	_, gate := getGate(t, gateSrv)
	for _, r := range rosterRows(t, gate, "checkout-picker") {
		if r.field != "visit_ids" {
			t.Fatalf("check-out picker submits under %q, want visit_ids", r.field)
		}
	}
}

// --- the payload the scoping doc measured ----------------------------

// TestTheConsoleRendersThePersonListFewerTimesThanBefore pins the measured
// defect: /trips used to render the full person list THREE times (a driver
// <select>, an occupant <select multiple> and an unsolicited-arrival
// <select multiple>) — 3,837 <option> rows at season scale. The console now
// renders the roster once and the (much shorter) on-site list twice.
func TestTheConsoleRendersThePersonListFewerTimesThanBefore(t *testing.T) {
	srv, w := newConsoleTestServer(t)
	page := getTrips(t, srv)
	roster := len(w.onSite) + len(w.offSite)

	rosterSized := 0
	for _, id := range []string{"driver-picker", "occupant-picker", "arrival-picker"} {
		if len(pickerRows(t, page, id)) == roster {
			rosterSized++
		}
	}
	if rosterSized > 1 {
		t.Fatalf("%d of the console's pickers render the whole roster; only the arrival picker may, because only it must be able to name anybody", rosterSized)
	}
	if got := strings.Count(page, "<option"); got > 1 {
		t.Fatalf("%d <option> elements remain on the console — the person lists are checkbox rows now", got)
	}
}

// --- V-07 arm 2: every person-moving form carries its review binding ----
//
// WHAT THIS ASSERTS, AND WHAT IT DELIBERATELY DOES NOT.
//
// The picker's client-side safety case rests on `picker_script`
// (_picker.html). Two of its invariants are stated in CAPITALS in that
// file's own comment and both were mutation-tested to SURVIVE a green
// suite:
//
//	m11  the consolidated confirm severed from the unsolicited-arrival form
//	m13  the search filter UNTICKS what it filters out — a narrowed search
//	     silently shrinks the selection, which is the exact data-loss
//	     failure the picker was built to replace
//
// This test closes m11's class and only m11's class. It is a Go assertion
// over RENDERED MARKUP — the binding that wires a form to its review step
// is a served attribute, so it is behaviour the server controls and can be
// checked without executing any JavaScript.
//
// ============================================================
// KNOWN, UNASSERTED GAP — do not read the test above as covering it.
// ============================================================
// m13 and the rest of picker_script's RUNTIME behaviour have NO repeatable
// check in this repository. `go test` cannot go red for a regression to
// the filter path, the scoped select-all, the tally, or the confirm's own
// collection logic. Nothing here executes that script.
//
// This is stated as a gap rather than papered over because the obvious
// cheap substitute is worse than nothing: scanning the script's SOURCE for
// a forbidden substring (`checked = false`) is defeated by `checked=false`,
// `checked = !1`, `box['checked']=false`, or moving the mutation one
// function away — and asserting a class name as a proxy for behaviour is
// the exact mechanism V-03 exists to condemn and that UI_BRIEF.md:33
// forbids as a class.
//
// Closing it needs a JS runtime, and the toolchain constraint (no npm, no
// build step, no JS test framework) means that is a decision to be made,
// not a thing to be quietly assumed. Until it is made, this comment is the
// honest statement of coverage. It is NOT a description of a harness that
// exists.
func TestEveryPersonMovingFormCarriesItsReviewBinding(t *testing.T) {
	srv, _ := newConsoleTestServer(t)
	trips := styleRe.ReplaceAllString(getTrips(t, srv), "")
	gateSrv, gateStore, _ := newGateTestServer(t)
	checkInCrew(t, gateSrv, gateStore, 3)
	_, gate := getGate(t, gateSrv)
	gate = styleRe.ReplaceAllString(gate, "")
	// ⟦R-28 revision 2, team-lead, 2026-09-16⟧ item 1: the R-25 single-
	// check-in form no longer renders on /gate/vehicle at all — it moved
	// entirely to the individual page (gate.html), which this sweep never
	// fetched before because nothing here needed it to (the vehicle page's
	// own copy already covered the route). Fetched now, on the SAME
	// server the rest of this fixture already uses, so /gate/checkin/single
	// is still seen by this walk at all.
	_, gateIndividual := getGateIndividual(t, gateSrv)
	gateIndividual = styleRe.ReplaceAllString(gateIndividual, "")
	// The gate's vehicle form reaches THREE MORE person-moving routes than
	// its own `action` attribute names — the commit, the per-row delete and
	// the undo all ride `formaction` on their buttons — and the delete and
	// undo controls only exist once a passenger has been added and removed.
	// A sweep of a freshly-served page would therefore never see them, and
	// would report a clean bill of health over routes it had not looked at.
	// So a SECOND gate page is built, part-way through entering a vehicle,
	// with a row present and a deletion pending.
	busySrv, busyStore, _ := newGateTestServer(t)
	checkInCrew(t, busySrv, busyStore, 2)
	_, busyPage := getGate(t, busySrv)
	busyPage = gateSetVehicle(t, busySrv, busyPage, gateInbound, "TRK-14")
	busyPage = gateAdd(t, busySrv, busyPage, gateInbound, "Crew 00")
	busyPage = gateAdd(t, busySrv, busyPage, gateInbound, "Crew 01")
	busyVals := gateFormValues(t, busyPage, formIDFor(gateInbound))
	busyVals.Set("delete_index", "1")
	busyRec := postForm(t, busySrv, "/gate/passenger/delete", busyVals)
	if busyRec.Code != http.StatusOK {
		t.Fatalf("building the part-entered gate page: delete = %d", busyRec.Code)
	}
	gateBusy := styleRe.ReplaceAllString(busyRec.Body.String(), "")
	// ⟦R-28, team-lead, 2026-09-16⟧ /gate/checkout/vehicle no longer rides
	// an always-present outbound form a fresh or inbound-busy page would
	// ever show — it now renders only via the roster's own tick-and-select
	// door (gate_roster_half). A THIRD gate page is built the same way an
	// officer reaches it, so the sweep below can see it at all rather than
	// certifying a route it never looked at.
	departSrv, departStore, _ := newGateTestServer(t)
	departVisitIDs := checkInCrew(t, departSrv, departStore, 2)
	departPage, _ := gateDepartSelected(t, departSrv, departVisitIDs)
	gateDeparting := styleRe.ReplaceAllString(departPage, "")
	// The pad-return manifest is a further surface and renders on its own
	// fixture, so it gets its own page rather than being assumed absent.
	returnsSrv := padReturnServer(t)
	returns := styleRe.ReplaceAllString(getTrips(t, returnsSrv), "")

	formRe := regexp.MustCompile(`(?s)<form\b[^>]*>.*?</form>`)
	openRe := regexp.MustCompile(`^<form\b[^>]*>`)
	actionRe := regexp.MustCompile(`action="([^"]*)"`)
	formActionRe := regexp.MustCompile(`formaction="([^"]*)"`)

	// A form MOVES PEOPLE if it carries a control that names people. This
	// is derived from the served markup, not from a hand-kept list of
	// routes, so a new person-moving form cannot join the app unguarded
	// just because nobody remembered to add it here.
	personControls := []string{
		`name="visit_ids"`, `name="visit_id"`, `name="occupant_person_ids"`,
		`name="stayed_person_ids"`, `name="driver_person_id"`,
		`name="person_ids"`, `name="occupant_value"`, `name="driver_value"`,
		// The gate's vehicle form: the open box's resolved pick, the
		// accumulated rows' row numbers, and the driver flag. `new_value`
		// is listed because a freshly-served vehicle form has no rows yet
		// and would otherwise look like a form that moves nobody.
		`name="new_value"`, `name="occupant_index"`, `name="driver_index"`,
	}

	type seenForm struct{ page, action, open, body string }
	var moving []seenForm
	pages := map[string]string{
		"/trips": trips, "/gate": gate, "/gate (part-entered)": gateBusy,
		"/gate individual":          gateIndividual,
		"/gate/vehicle (departing)": gateDeparting,
		"/trips (pad returns)":      returns,
	}
	for page, body := range pages {
		for _, f := range formRe.FindAllString(body, -1) {
			carries := false
			for _, c := range personControls {
				if strings.Contains(f, c) {
					carries = true
					break
				}
			}
			if !carries {
				continue
			}
			open := openRe.FindString(f)
			a := actionRe.FindStringSubmatch(open)
			if a == nil {
				t.Errorf("%s: a person-moving form has no action at all: %s", page, open)
				continue
			}
			// EVERY route this form can reach, not just the one its action
			// names. A `formaction` on a submit button overrides the form's
			// action entirely, so a review binding checked only against the
			// action would certify a route the form never posts to.
			routes := map[string]bool{a[1]: true}
			for _, fa := range formActionRe.FindAllStringSubmatch(f, -1) {
				routes[fa[1]] = true
			}
			for route := range routes {
				moving = append(moving, seenForm{page: page, action: route, open: open, body: f})
			}
		}
	}
	if len(moving) < 8 {
		t.Fatalf("precondition: found only %d person-moving form/route pairs across /trips and /gate — this sweep is not looking at the app", len(moving))
	}

	// Each action's review mechanism, re-derived per action rather than
	// generalised from the widget — the same rule the safe-default tests
	// follow. `checkin` is the one that does NOT use the picker's binding:
	// it has its own inline off-roster confirm in gate.html, keyed to its
	// form id, and that is asserted directly below rather than exempted.
	const (
		byPicker  = "data-confirm-picker"
		byStayed  = "data-confirm-stayed"
		byCommit  = "the vehicle form's own commit confirm"
		byNoWrite = "nothing to review — this route authors nothing"
		// ⟦R-28 revision 2, team-lead, 2026-09-16⟧ item 1: was
		// bySingleCheckIn, checking gate_vehicle.html's own now-removed
		// R-25 form (class="single-checkin-form", button
		// class="p-checkin-single"). That form's route,
		// /gate/checkin/single, is served by exactly ONE instance now —
		// gate.html's own top CHECK IN form (class="individual-checkin-
		// form", button class="p-checkin-individual") — so this case is
		// re-derived against THAT form's own structure and its own,
		// separately wired review step (gate_vehicle.html's
		// ".individual-checkin-form" closure), not renamed by analogy.
		byIndividualCheckIn = "the individual page's own CHECK IN commit confirm"
	)
	want := map[string]string{
		"/trips/dispatch":           byPicker,
		"/trips/arrive-unsolicited": byPicker,
		"/trips/arrive":             byStayed,
		"/gate/checkout":            byPicker,
		// ⟦R-25, James, 2026-09-13⟧ its own review mechanism, RE-DERIVED
		// rather than borrowed: it is a genuinely separate <form> from
		// gate_vehicle_form's (see that form's own template comment for
		// why), so it cannot share `.gate-commit`'s form-scoped confirm —
		// see the byIndividualCheckIn case below for what is actually
		// checked.
		"/gate/checkin/single": byIndividualCheckIn,
		// The gate's two commits. Each carries its OWN confirm text,
		// rendered by the server per direction — the arriving one names the
		// consequence in the dangerous direction, the departing one does
		// not, because there it would be false. THIS test asserts only that
		// the binding exists; what each direction SAYS is asserted per
		// direction, without a shared helper, in gate_direction_test.go.
		"/gate/checkin":          byCommit,
		"/gate/checkout/vehicle": byCommit,
		// The accumulate routes move nobody: they parse the in-progress
		// list, change it, and re-render. A confirmation on a control
		// pressed 25 times per bus would train click-through and degrade
		// the confirm on the commit that matters. The claim that they
		// author nothing is not taken on trust — TestTheAccumulateRoutesAuthorNothing
		// asserts the store's watermark does not move across all three.
		"/gate/passenger/add":    byNoWrite,
		"/gate/passenger/delete": byNoWrite,
		"/gate/passenger/undo":   byNoWrite,
		// ⟦gate-restack, 2026-09-11⟧ "Depart together" (James: "tick people
		// ... then name the vehicle"): reads ticked visit ids off the
		// SAME form as /gate/checkout and stages the still-on-site ones
		// onto the outbound vehicle form as accumulated rows. Re-derived,
		// not by analogy with its neighbours: it authors nothing (same
		// TestTheAccumulateRoutesAuthorNothing guarantee the other three
		// get), so byNoWrite is the right bucket — but unlike those three
		// its own <form> ALSO carries checkout-picker's data-confirm-
		// picker binding, which is exactly right for THAT form's OTHER
		// button (/gate/checkout, a real write). See the byNoWrite case
		// below for how the two are told apart at the BUTTON, not the
		// form.
		"/gate/checkout/select": byNoWrite,
		// ⟦gate-checkin-spec, James, 2026-09-13⟧ "Edit person": reads ticked
		// visit ids off the SAME form as /gate/checkout and redirects — same
		// shape and same reasoning as /gate/checkout/select just above: it
		// authors nothing (TestTheAccumulateRoutesAuthorNothing covers it
		// too) and its own button carries data-no-confirm so a mis-click
		// never shows /gate/checkout's "remove from the muster" confirm
		// text before a route that removes nobody.
		"/gate/edit-selected": byNoWrite,
	}

	pickerIDRe := regexp.MustCompile(`id="([a-z0-9-]*picker)"`)
	sawAction := map[string]bool{}
	for _, f := range moving {
		mech, known := want[f.action]
		if !known {
			t.Errorf("%s: %s moves people and this test does not know what reviews it. Add the action here WITH its own re-derived mechanism — never by analogy with a neighbouring form: %s", f.page, f.action, f.open)
			continue
		}
		sawAction[f.action] = true
		switch mech {
		case byStayed:
			if !strings.Contains(f.open, byStayed) {
				t.Errorf("%s: %s carries stayed-behind controls but no %s binding — with JS on there would be NO review step before people are left off the muster: %s", f.page, f.action, byStayed, f.open)
			}
			// The binding is only real if the rows it reads are there.
			if !strings.Contains(f.body, `class="stayed"`) || !strings.Contains(f.body, "data-stayed-name") {
				t.Errorf("%s: %s is bound by %s, but its rows carry no label.stayed / data-stayed-name for the confirm to read — the review step would name nobody", f.page, f.action, byStayed)
			}
		case byPicker:
			m := regexp.MustCompile(byPicker + `="([^"]*)"`).FindStringSubmatch(f.open)
			if m == nil {
				t.Errorf("%s: %s selects people through a picker but carries no %s binding — a mis-tick would reach the server with no review step at all: %s", f.page, f.action, byPicker, f.open)
				continue
			}
			named := map[string]bool{}
			for _, id := range strings.Fields(m[1]) {
				named[id] = true
				if !strings.Contains(f.body, `id="`+id+`"`) && !strings.Contains(f.page, id) {
					// The picker must exist somewhere on the served page,
					// or getElementById returns null and the confirm
					// silently does nothing.
					page := map[string]string{"/trips": trips, "/gate": gate, "/trips (pad returns)": returns}[f.page]
					if !strings.Contains(page, `id="`+id+`"`) {
						t.Errorf("%s: %s names picker %q, which does not exist on the served page — document.getElementById returns null and the confirm silently does not run", f.page, f.action, id)
					}
				}
			}
			// V-04's own regression guard, at render level: EVERY picker
			// inside this form's markup must be named by the binding. The
			// dispatch form contains driver-picker and occupant-picker and
			// used to name only the second, so the confirm understated the
			// muster effect by one on every dispatch and reviewed the
			// driver radio not at all.
			for _, pm := range pickerIDRe.FindAllStringSubmatch(f.body, -1) {
				if !named[pm[1]] {
					t.Errorf("%s: %s contains picker %q but its %s binding does not name it (%q). Every picker a form submits must be named, or the confirm reports fewer people than the submit actually moves.", f.page, f.action, pm[1], byPicker, m[1])
				}
			}
		case byCommit:
			// The vehicle form's review is the commit confirm: the server
			// renders the text onto the commit button, and the page's
			// script fires it for that button only. Assert the two halves
			// are still connected to each other — a confirm attribute with
			// no listener, or a listener with no attribute, is no review
			// step at all.
			//
			// ⟦R-28, team-lead, 2026-09-16⟧ RETARGETED to a WORD match, not
			// the literal attribute: gate_vehicle_form's own Compact
			// instantiation now also carries "vehicle-form-compact" in the
			// SAME class attribute (`class="vehicle-form vehicle-form-
			// compact"`) — a real second CSS class, which the actual JS
			// binding (document.querySelectorAll('.vehicle-form'), a class
			// selector) matches exactly the same as before; only this
			// test's own literal `class="vehicle-form"` substring check
			// stopped matching, which is the check being wrong, not the
			// binding.
			if !vehicleFormClassRe.MatchString(f.open) {
				t.Errorf("%s: %s is reached from a form the gate script does not bind: %s", f.page, f.action, f.open)
			}
			m := regexp.MustCompile(`data-commit-confirm="([^"]*)"`).FindStringSubmatch(f.body)
			if m == nil || strings.TrimSpace(m[1]) == "" {
				t.Errorf("%s: %s has no commit confirmation text — the one control that writes a whole vehicle would submit with no review step: %s", f.page, f.action, f.open)
			} else if !strings.Contains(m[1], "?") {
				t.Errorf("%s: %s's confirmation does not ask anything: %q", f.page, f.action, m[1])
			}
			pageBody := pages[f.page]
			if !strings.Contains(pageBody, `document.querySelectorAll('.vehicle-form')`) {
				t.Errorf("%s: the gate script no longer binds the vehicle forms, so the commit confirm never fires", f.page)
			}
			if !strings.Contains(pageBody, `getAttribute('data-commit-confirm')`) {
				t.Errorf("%s: the gate script no longer reads the per-direction confirm text, so both directions would say whatever the script hard-codes", f.page)
			}
		case byIndividualCheckIn:
			// ⟦R-25⟧ re-derived, not borrowed from byCommit: this form is
			// NOT `.vehicle-form`, so the group commit's form-scoped
			// listener (keyed to a `commit` variable that loop owns) never
			// sees this form's submit event at all. Its review step is its
			// OWN button's OWN attribute, read by its OWN, separately
			// registered listener — both checked here, independently of the
			// byCommit case above.
			// ⟦R-28 revision 2, team-lead, 2026-09-16⟧ item 1: RETARGETED
			// from gate_vehicle.html's now-removed single-check-in form
			// (class="single-checkin-form"/p-checkin-single) to gate.html's
			// own top CHECK IN form (class="individual-checkin-form"/
			// p-checkin-individual), which now serves this route alone —
			// same re-derivation, same two things proved (a review step
			// exists on the button, and the script actually wires it), just
			// pointed at the one instance left.
			if !strings.Contains(f.open, `class="individual-checkin-form"`) {
				t.Errorf("%s: %s is not the individual page's own CHECK IN form — it must not silently become another instance of .vehicle-form, which would hand it the group commit's own confirm listener instead of its own: %s", f.page, f.action, f.open)
			}
			btn := regexp.MustCompile(`<button\b[^>]*class="[^"]*p-checkin-individual[^"]*"[^>]*>`).FindString(f.body)
			if btn == "" {
				t.Errorf("%s: %s carries no button of class p-checkin-individual for a dedicated listener to find: %s", f.page, f.action, f.open)
				continue
			}
			m := regexp.MustCompile(`data-commit-confirm="([^"]*)"`).FindStringSubmatch(btn)
			if m == nil || strings.TrimSpace(m[1]) == "" {
				t.Errorf("%s: %s's own button carries no commit confirmation text — the one control that authors a check-in here would submit with no review step: %s", f.page, f.action, btn)
			} else if !strings.Contains(m[1], "?") {
				t.Errorf("%s: %s's confirmation does not ask anything: %q", f.page, f.action, m[1])
			}
			pageBody := pages[f.page]
			if !strings.Contains(pageBody, `querySelector('.p-checkin-individual')`) {
				t.Errorf("%s: the gate script does not look up the individual CHECK IN button by its own class, so no listener is wired to it", f.page)
			}
		case byNoWrite:
			// The write-nothing guarantee is asserted directly against
			// store state elsewhere (TestTheAccumulateRoutesAuthorNothing).
			// What IS asserted here is the review step: absent, for a form
			// that carries no binding at all (passenger/add, /delete,
			// /undo — unchanged) — OR explicitly opted OUT of, for a
			// route reached via formaction from a form whose binding
			// belongs to a SIBLING write action (gate-restack, 2026-09-11:
			// /gate/checkout/select shares checkout-picker's form with the
			// real write /gate/checkout). Either shape is fine; what must
			// never happen is this route silently inheriting somebody
			// else's confirm text by sharing their form, or acquiring the
			// commit class itself.
			formHasBinding := strings.Contains(f.open, `data-confirm-picker`) || strings.Contains(f.open, `data-confirm-stayed`)
			if !formHasBinding {
				break
			}
			btn := regexp.MustCompile(`<button\b[^>]*formaction="` + regexp.QuoteMeta(f.action) + `"[^>]*>`).FindString(f.body)
			if btn == "" || !strings.Contains(btn, "data-no-confirm") {
				t.Errorf("%s: %s shares a form with a review-bound write action but its own button carries no data-no-confirm — a mis-click would show that OTHER action's confirm text before this one, which moves nobody, ever submits: %s", f.page, f.action, f.open)
			}
		}
	}
	for action := range want {
		if !sawAction[action] {
			t.Errorf("precondition: %s never rendered in this sweep, so nothing above proves anything about it", action)
		}
	}
}

// --- V-07 arm 1: the part of the script's safety case that IS server-side ---
//
// V-07 arm 1 was escalated because its prescribed remedy — scanning
// picker_script's SOURCE for a forbidden substring — is the defect class
// V-03 exists to condemn, and the "precedent" proposed in its place (a Node
// harness behind gate.html's RESOLVE_PICK markers) DOES NOT EXIST: there is
// no .js, .mjs, .cjs or package.json anywhere in this repository, verified
// again 2026-08-21. The toolchain constraint stands: no npm, no build step,
// no JS test framework.
//
// That does not make arm 1 entirely unassertable, and the KNOWN, UNASSERTED
// GAP note above should not be read as saying it is. Two of the ways the
// review step dies are facts about SERVED MARKUP, which the server controls
// and Go can check without executing anything:
//
//  1. THE SCRIPT IS NOT ON THE PAGE. Every confirm in this application is
//     installed by picker_script. A page that renders pickers and omits the
//     template loses every review step on it with JS ON — and loses nothing
//     with JS off, so the JS-off tests stay green, no handler errors, no
//     log line. Two of the three picker-bearing surfaces include it by hand
//     today (trips.html:267, gate.html:177); the third page to grow a
//     picker is the one that will forget.
//
//  2. THE COLLECTOR'S ROWS ARE NOT THERE. The confirm names people by
//     iterating `picker.querySelectorAll('.picker-row')`. If a ticked input
//     stops sitting inside a .picker-row, that person is not named — and if
//     none of them do, `names.length` is 0 and the script RETURNS WITHOUT
//     CONFIRMING (`if (!names.length) return;`). The submit then goes
//     through silently and unreviewed. That is the same shape as m11, one
//     layer in, and it is a property of the rendered markup.
//
// WHAT THIS STILL DOES NOT COVER, said plainly: m13 (the filter hiding
// rather than unticking), the scoped select-all's containment, the tally,
// and the collector's de-duplication are all RUNTIME behaviour of code
// nothing in this repository executes. This test does not touch them and
// must not be cited as if it did. See the gap note above.
func TestEveryPageWithAPickerServesTheScriptThatReviewsIt(t *testing.T) {
	srv, _ := newConsoleTestServer(t)
	gateSrv, gateStore, _ := newGateTestServer(t)
	checkInCrew(t, gateSrv, gateStore, 3)
	_, gate := getGate(t, gateSrv)

	pages := map[string]string{
		"/trips": styleRe.ReplaceAllString(getTrips(t, srv), ""),
		"/gate":  styleRe.ReplaceAllString(gate, ""),
		// Every other served page, so a picker appearing on one of them
		// without the script is caught rather than assumed impossible.
		"/trips/detail": styleRe.ReplaceAllString(getTripsDetail(t, srv), ""),
	}
	for route, body := range renderedPages(t) {
		if _, dup := pages[route]; !dup {
			pages[route] = styleRe.ReplaceAllString(body, "")
		}
	}

	pickerRe := regexp.MustCompile(`(?s)<div class="picker" id="([^"]+)"(.*?)\n</div>`)
	rowRe := regexp.MustCompile(`(?s)<label class="picker-row">(.*?)</label>`)
	namedInputRe := regexp.MustCompile(`<input type="(?:checkbox|radio)"[^>]*\sname="`)

	sawAPicker := false
	for route, body := range pages {
		blocks := regexp.MustCompile(`<div class="picker" id="([^"]+)"`).FindAllStringSubmatch(body, -1)
		if len(blocks) == 0 {
			continue
		}
		sawAPicker = true

		// 1. The script that reviews them is served with them.
		if !strings.Contains(body, `<script id="picker-script">`) {
			t.Errorf("%s renders %d picker(s) and does NOT include picker_script. With JS off this page is fine, which is why nothing else fails; with JS on every submit on it reaches the server with no review step at all — no confirm, no names read back, no error", route, len(blocks))
		}

		// Each picker id must resolve to exactly one element, or
		// getElementById picks one and the confirm reviews half the form.
		seen := map[string]bool{}
		for _, b := range blocks {
			if seen[b[1]] {
				t.Errorf("%s renders two elements with id=%q — getElementById returns the first, so the confirm would name only that one's ticks", route, b[1])
			}
			seen[b[1]] = true
		}

		// 2. Every input the collector must see is inside a .picker-row.
		for _, pm := range pickerRe.FindAllStringSubmatch(body, -1) {
			id, inner := pm[1], pm[2]
			inRows := 0
			for _, r := range rowRe.FindAllStringSubmatch(inner, -1) {
				inRows += len(namedInputRe.FindAllString(r[1], -1))
			}
			total := len(namedInputRe.FindAllString(inner, -1))
			if total == 0 {
				continue // an empty scope renders its Empty sentence, not rows
			}
			if inRows != total {
				t.Errorf("%s: picker %q renders %d named checkbox/radio inputs but only %d of them are inside a <label class=\"picker-row\">. The confirm collects the people it is about to name by iterating .picker-row; anything outside one is submitted WITHOUT being named, and if none of them are inside one the confirm sees an empty list and returns without asking at all", route, id, total, inRows)
			}
		}
	}
	if !sawAPicker {
		t.Fatal("precondition: no page in this sweep rendered a picker, so nothing above asserted anything")
	}
}
