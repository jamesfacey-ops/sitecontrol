// inbound_test.go — the gate's "on the road to here" list and its
// ticked-by-default arrival control (James, 2026-08-21).
//
// THE SINGLE MOST IMPORTANT ASSERTION IN THIS FILE is
// TestTheGateArrivalControlIsTickedInTheServerRenderedMarkup. It is what
// stands between a forgotten tick and a person who is physically standing at
// the gate being absent from the muster — the direction D-6 exists to
// prevent. It asserts on the MARKUP the server produced, with the stylesheet
// and every script block stripped, because the default must hold with
// scripting off: a default applied by JavaScript is not a default.
//
// Assertions here are on structural hooks (data-inbound-vehicle,
// data-urgency, data-arriving-name) or on named form fields, never on a
// page-text scan, and markup is always stripped of <style> first — a class
// name in the one inlined stylesheet is not a rendered element (F19).
package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// scriptRe strips <script> blocks. Together with styleRe it leaves only what
// a browser with scripting disabled would actually act on — which is the
// only thing the "ticked by default" claim may be proved against.
var scriptRe = regexp.MustCompile(`(?s)<script[^>]*>.*?</script>`)

// noScriptMarkup is the page as a JS-off browser sees it.
func noScriptMarkup(t *testing.T, srv *Server, path string) string {
	t.Helper()
	return scriptRe.ReplaceAllString(styleRe.ReplaceAllString(rawPage(t, srv, path), ""), "")
}

// inboundWorld is a post with one vehicle on the road toward it: site-a
// dispatched TRK-100 to site-b with a driver and two passengers, and site-b
// (the server under test) has received the trip.
type inboundWorld struct {
	srv     *Server
	st      *store.Store
	trip    domain.Trip
	aboard  []domain.Person // driver first
	persons []domain.Person
}

func newInboundWorld(t *testing.T) *inboundWorld {
	t.Helper()
	srv, st, persons := newTripsTestServer(t) // site-b
	var aboard []domain.Person
	for _, n := range []string{"Nadeau, Pierre", "Ohara, Wendy", "Petrov, Sasha"} {
		p, err := domain.CreatePerson(st, "site-a", 1, n, nil, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		aboard = append(aboard, p)
	}
	dest, dl := "site-b", int64(90)
	trip, err := domain.Dispatch(st, "site-a", 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRK-100", DriverPersonID: aboard[0].PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK-100"), DriverPersonID: aboard[0].PersonID,
		OccupantPersonIDs: []string{aboard[1].PersonID, aboard[2].PersonID},
		DestinationSite:   &dest, DeadlineMinutes: &dl,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	return &inboundWorld{srv: srv, st: st, trip: trip, aboard: aboard, persons: persons}
}

// vehicleBlock returns one vehicle's <details> block from the gate page,
// scoped. A page-wide grep would let a checkbox belonging to the check-out
// picker further down the same page stand in for one of these.
// detailsTagRe finds every <details...> open tag and every </details> close
// tag, in document order — vehicleBlock below walks these to find the ROW's
// own matching close, not merely the first </details> encountered.
var detailsTagRe = regexp.MustCompile(`<details\b[^>]*>|</details>`)

// vehicleBlock returns one vehicle's <details class="inbound-row"> block,
// from its own open tag to its OWN matching close — nesting-aware since
// ⟦R-28 revision 2, team-lead, 2026-09-16⟧ item 2 put a second, nested
// <details class="inbound-adjust"> (the per-passenger ticks and time
// control, collapsed behind "adjust") inside the row. A plain
// strings.Index for the first "</details>" would return the INNER
// details' own close and silently truncate the block before the commit
// button that follows it — MEASURED:
// TestTheGateArrivalButtonSaysItChecksInEveryoneStillTicked started
// failing the moment the nesting landed, for exactly this reason.
func vehicleBlock(t *testing.T, page, vehicle string) string {
	t.Helper()
	at := strings.Index(page, `data-inbound-vehicle="`+vehicle+`"`)
	if at < 0 {
		t.Fatalf("the gate page has no inbound row for %s:\n%s", vehicle, page)
	}
	// Back up to this row's own <details ...> open tag (data-inbound-vehicle
	// is an attribute on it, not a separate element).
	openAt := strings.LastIndex(page[:at], "<details")
	if openAt < 0 {
		t.Fatalf("the inbound row for %s has no enclosing <details>:\n%s", vehicle, page)
	}
	depth := 0
	for _, m := range detailsTagRe.FindAllStringIndex(page[openAt:], -1) {
		tag := page[openAt+m[0] : openAt+m[1]]
		if tag == "</details>" {
			depth--
			if depth == 0 {
				return page[openAt : openAt+m[1]]
			}
		} else {
			depth++
		}
	}
	t.Fatalf("the inbound row for %s is never closed", vehicle)
	return ""
}

// --- the list itself ---------------------------------------------------

// James's shape, asserted end to end: the vehicles inbound to this post are
// listed at the GATE, each one opens to the people aboard it, and one button
// checks in everyone still ticked.
func TestTheGateListsTheVehiclesOnTheRoadTowardThisPost(t *testing.T) {
	w := newInboundWorld(t)
	block := vehicleBlock(t, noScriptMarkup(t, w.srv, "/gate/vehicle"), "TRK-100")

	for _, want := range []string{
		"from site-a",      // which post dispatched it
		"3 aboard",         // the at-a-glance headcount
		"allowance 90 min", // time due, as the authored allowance
		`name="trip_id" value="` + w.trip.TripID + `"`,
		`action="/trips/arrive"`, // the EXISTING arrival path, not a new one
	} {
		if !strings.Contains(block, want) {
			t.Errorf("the inbound row for TRK-100 is missing %q:\n%s", want, block)
		}
	}
	// Every person aboard is offered individually — D-3's affirmation is
	// per person, and a "check in the whole vehicle" button with no per-
	// person control would be the manifest confirmation D-3 waives.
	for _, p := range w.aboard {
		if !strings.Contains(block, `value="`+p.PersonID+`"`) {
			t.Errorf("%s is aboard TRK-100 but has no control on the gate list:\n%s", p.Name, block)
		}
		if !strings.Contains(block, `<span data-arriving-name>`+p.Name+`</span>`) {
			t.Errorf("%s is not NAMED on the gate list — an id is not something an officer can check against a face", p.Name)
		}
	}
	// The driver is named as the driver (D-4's flag exists for the incident
	// report, and a name in a list does not answer "who was at the wheel").
	if !strings.Contains(block, "Driver:") || !strings.Contains(block, w.aboard[0].Name) {
		t.Errorf("the row does not say who is driving:\n%s", block)
	}
}

// THE ASSERTION THIS WHOLE SURFACE TURNS ON. Every occupant's box is
// `checked` in the markup the SERVER produced — with the stylesheet and
// every script stripped, so nothing about this can be true only once
// JavaScript has run. A JS-off operator who was handed an empty selection
// would submit a whole crew's omission, and an omitted person is physically
// present with no record at all: the direction that leaves somebody behind.
func TestTheGateArrivalControlIsTickedInTheServerRenderedMarkup(t *testing.T) {
	w := newInboundWorld(t)
	block := vehicleBlock(t, noScriptMarkup(t, w.srv, "/gate/vehicle"), "TRK-100")

	boxes := regexp.MustCompile(`<input type="checkbox" name="arriving_person_ids"[^>]*>`).FindAllString(block, -1)
	if len(boxes) != len(w.aboard) {
		t.Fatalf("expected one arriving checkbox per occupant (%d), found %d:\n%s", len(w.aboard), len(boxes), block)
	}
	for _, b := range boxes {
		if !strings.Contains(b, " checked") {
			t.Errorf("an arriving checkbox is NOT ticked in the server-rendered markup — with scripting off this crew would be omitted from the muster: %s", b)
		}
	}
	// The hidden manifest must be there too, one per occupant: without it
	// the server cannot tell "nobody was ticked" from "no control was
	// rendered", and the failure stops being loud.
	hidden := regexp.MustCompile(`<input type="hidden" name="manifest_person_ids"[^>]*>`).FindAllString(block, -1)
	if len(hidden) != len(w.aboard) {
		t.Errorf("expected one hidden manifest row per occupant (%d), found %d — the server could not tell an empty tick set from a broken form", len(w.aboard), len(hidden))
	}
	// The control's STATE now matches its EFFECT. The console's inverted
	// spelling ("stayed behind — do not check in") must not appear on this
	// surface: two opposite meanings for a box beside a name, on the two
	// screens one officer uses, is a wrong-click generator.
	if strings.Contains(block, "stayed_person_ids") {
		t.Errorf("the gate row still carries the console's EXCLUSIVE control — ticking a name here must mean the person is coming IN:\n%s", block)
	}
}

// The button says what it does, in James's own terms.
func TestTheGateArrivalButtonSaysItChecksInEveryoneStillTicked(t *testing.T) {
	w := newInboundWorld(t)
	block := vehicleBlock(t, noScriptMarkup(t, w.srv, "/gate/vehicle"), "TRK-100")
	// ⟦was: "Check in all checked"⟧ — GATE-ACCESS-STYLE (2026-09-12) shortened
	// the button to "Check in (N)", one or two words per James's own terse-tag
	// ruling; the count stays, because the count is what an officer checks
	// against the people in front of them.
	if !strings.Contains(block, "Check in (") {
		t.Errorf("the arrival button does not say it checks in everyone checked, with a count:\n%s", block)
	}
}

// Expansion is the browser's own disclosure control, not a scripted
// accordion: an officer with scripting off must be able to open a vehicle
// and see who is in it. Asserted on the JS-stripped markup, where a
// <details>/<summary> pair still works and a div-based accordion would not.
func TestAVehicleOpensWithoutJavaScript(t *testing.T) {
	w := newInboundWorld(t)
	page := noScriptMarkup(t, w.srv, "/gate/vehicle")
	block := vehicleBlock(t, page, "TRK-100")
	if !strings.Contains(block, "<summary>") {
		t.Errorf("the inbound row has no <summary> — expansion depends on a script:\n%s", block)
	}
	if !strings.HasPrefix(strings.TrimSpace(page[strings.Index(page, `<details class="inbound-row"`):]), "<details") {
		t.Error("the inbound row is not a <details> element")
	}
	// ...and the occupant controls are inside it, so opening the vehicle is
	// what reveals the people. A list rendered outside the disclosure would
	// make the summary decorative.
	if !strings.Contains(block, `name="arriving_person_ids"`) {
		t.Errorf("the occupant controls are not inside the vehicle's disclosure:\n%s", block)
	}
}

// --- writing the arrival ------------------------------------------------

// The happy path over the real POST: everyone ticked checks in, and the
// people land on THIS site's muster.
func TestCheckingInAllCheckedPutsTheWholeCrewOnTheMuster(t *testing.T) {
	w := newInboundWorld(t)
	form := url.Values{"time_source": {"default"}, "trip_id": {w.trip.TripID}, "from": {"gate"}}
	for _, p := range w.aboard {
		form.Add("manifest_person_ids", p.PersonID)
		form.Add("arriving_person_ids", p.PersonID)
	}
	rec := postForm(t, w.srv, "/trips/arrive", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("arrival = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	// The gate officer stays at the gate. ⟦R-28, team-lead, 2026-09-16⟧
	// RETARGETED, not weakened: the redirect now carries the one-click
	// result line as query values (arrivedRedirect, inbound.go) — the PATH
	// this test was actually guarding is unchanged, so it now parses the
	// Location and checks the path, then goes further than the original
	// assertion did by pinning the new query's own values too (vehicle
	// text and the checked-in count), which nothing checked before.
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("Location %q did not parse: %v", rec.Header().Get("Location"), err)
	}
	if loc.Path != "/gate/vehicle" {
		t.Errorf("after a gate arrival the operator lands on %q, want /gate/vehicle", loc.Path)
	}
	wantVehicle := ""
	if w.trip.VehicleEntered != nil {
		wantVehicle = *w.trip.VehicleEntered
	}
	if got := loc.Query().Get("arrived_vehicle"); got != wantVehicle {
		t.Errorf("arrived_vehicle = %q, want %q (the trip's own vehicle)", got, wantVehicle)
	}
	wantChecked := strconv.Itoa(len(w.aboard))
	if got := loc.Query().Get("arrived_checked"); got != wantChecked {
		t.Errorf("arrived_checked = %q, want %q (all %d of w.aboard were ticked)", got, wantChecked, len(w.aboard))
	}
	open, err := domain.OpenVisitsAt(w.st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, v := range open {
		got[v.PersonID] = true
	}
	for _, p := range w.aboard {
		if !got[p.PersonID] {
			t.Errorf("%s was ticked but is not on site-b's muster after the arrival", p.Name)
		}
	}
}

// The exception path, and the documented real case: somebody got off at an
// uncontrolled pad en route. Unticking one name checks the rest in and
// leaves that person with no visit here.
func TestUntickingSomebodyLeavesThemOffThisSitesMuster(t *testing.T) {
	w := newInboundWorld(t)
	droppedOff := w.aboard[2]

	form := url.Values{"time_source": {"default"}, "trip_id": {w.trip.TripID}, "from": {"gate"}}
	for _, p := range w.aboard {
		form.Add("manifest_person_ids", p.PersonID)
		if p.PersonID != droppedOff.PersonID {
			form.Add("arriving_person_ids", p.PersonID)
		}
	}
	if rec := postForm(t, w.srv, "/trips/arrive", form); rec.Code != http.StatusSeeOther {
		t.Fatalf("arrival = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	open, err := domain.OpenVisitsAt(w.st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range open {
		if v.PersonID == droppedOff.PersonID {
			t.Fatalf("%s was unticked but was checked in anyway", droppedOff.Name)
		}
	}
	if len(open) != len(w.aboard)-1 {
		t.Fatalf("%d visits opened, want %d — unticking one person changed more than one row", len(open), len(w.aboard)-1)
	}

	// AND THEY DO NOT VANISH. The existing model already has the right name
	// for this and no third category is invented: they are NOT TRACKED —
	// left on a vehicle, did not arrive with it, whereabouts not recorded.
	nt, err := domain.NotTracked(w.st)
	if err != nil {
		t.Fatal(err)
	}
	named := false
	for _, row := range nt {
		for _, p := range row.Persons {
			if p.PersonID == droppedOff.PersonID {
				named = true
			}
		}
	}
	if !named {
		t.Errorf("%s did not arrive and is in NO muster category — an unticked passenger must land in NOT TRACKED, not nowhere", droppedOff.Name)
	}
}

// A submission that ticks NOBODY is REFUSED, loudly, rather than recording
// an arrival that checked in nobody. This is what makes the ticked default's
// failure mode safe: even if every box somehow rendered unticked, the
// operator gets an error instead of a silently empty arrival.
func TestAnArrivalWithNobodyTickedIsRefusedRatherThanRecordedEmpty(t *testing.T) {
	w := newInboundWorld(t)
	form := url.Values{"time_source": {"default"}, "trip_id": {w.trip.TripID}, "from": {"gate"}}
	for _, p := range w.aboard {
		form.Add("manifest_person_ids", p.PersonID)
	}
	rec := postForm(t, w.srv, "/trips/arrive", form)
	if rec.Code == http.StatusSeeOther {
		t.Fatal("an arrival with nobody ticked was ACCEPTED — the crew would be recorded as having arrived nowhere")
	}
	if !strings.Contains(rec.Body.String(), "nobody aboard") {
		t.Errorf("the refusal does not explain itself: %s", rec.Body.String())
	}
	// Nothing was authored: the trip is still open and still on the list.
	state, err := domain.TripStateByID(w.st, w.trip.TripID)
	if err != nil {
		t.Fatal(err)
	}
	if state == nil || state.State != domain.TripStateOpen {
		t.Fatalf("the trip is %v after a refused arrival — something was recorded", state)
	}
}

// The console's older exclusive form keeps working over the same endpoint.
// Both shapes reduce to the same stayed-behind set, so the two surfaces can
// never disagree about what was recorded — which is the whole reason this is
// one mechanism with two renderings rather than two mechanisms.
func TestTheConsolesExclusiveArrivalFormStillRecordsTheSameThing(t *testing.T) {
	w := newInboundWorld(t)
	stayed := w.aboard[1]
	rec := postForm(t, w.srv, "/trips/arrive", url.Values{
		"time_source":       {"default"},
		"trip_id":           {w.trip.TripID},
		"stayed_person_ids": {stayed.PersonID},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("console arrival = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/trips" {
		t.Errorf("a console arrival lands on %q, want /trips", got)
	}
	open, err := domain.OpenVisitsAt(w.st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range open {
		if v.PersonID == stayed.PersonID {
			t.Fatalf("%s was marked as stayed behind on the console form but was checked in", stayed.Name)
		}
	}
	if len(open) != len(w.aboard)-1 {
		t.Fatalf("%d visits opened, want %d", len(open), len(w.aboard)-1)
	}
}

// --- emptying: arrival, cancel, and the vehicle that never comes --------

// The list is a work queue, and the work leaving it is the point. Asserted
// with a BEFORE and an AFTER so "it is gone" cannot be true because it was
// never there.
func TestAVehicleLeavesTheGateListWhenItArrives(t *testing.T) {
	w := newInboundWorld(t)
	if !strings.Contains(noScriptMarkup(t, w.srv, "/gate/vehicle"), `data-inbound-vehicle="TRK-100"`) {
		t.Fatal("precondition: TRK-100 is not on the gate list before arriving")
	}
	if _, err := domain.RecordExpectedArrival(w.st, "site-b", 1, w.trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	after := noScriptMarkup(t, w.srv, "/gate/vehicle")
	if strings.Contains(after, `data-inbound-vehicle="TRK-100"`) {
		t.Fatalf("TRK-100 is still on the gate list after arriving:\n%s", after)
	}
}

// A CANCELLED run leaves the list too — the origin says it is not coming, so
// keeping it on an arrivals queue would have the officer waiting for a truck
// that is not on the road.
//
// IT MUST NOT STRAND ANYBODY, and it does not, because this list writes
// nothing on its own: a cancel never changes a visit state (TripCancelled),
// so the occupants are exactly where the cancel left them, and nothing about
// their accountability depends on whether a row is drawn here. If they turn
// up anyway they come in through the arriving-vehicle form above (§9.2's
// unsolicited arrival), which this list never removes.
//
// ⟦D-78, 2026-09-05⟧ No officer-facing route reaches domain.CancelTrip any
// more. The cancel below is authored DIRECTLY, as every test pinning the
// retained `cancelled` state does: the state stays (already-replicated
// history carrying it must still fold) while the door is gone. What this
// pins is the fold's behaviour on that history, not a path an operator can
// take.
func TestACancelledVehicleLeavesTheGateListWithoutChangingAnybodysState(t *testing.T) {
	w := newInboundWorld(t)
	before, err := domain.Visits(w.st)
	if err != nil {
		t.Fatal(err)
	}
	if err := domain.CancelTrip(w.st, "site-a", 1, w.trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	after := noScriptMarkup(t, w.srv, "/gate/vehicle")
	if strings.Contains(after, `data-inbound-vehicle="TRK-100"`) {
		t.Fatalf("a cancelled run is still queued as inbound at the gate:\n%s", after)
	}
	visits, err := domain.Visits(w.st)
	if err != nil {
		t.Fatal(err)
	}
	if len(visits) != len(before) {
		t.Fatalf("the cancel changed the visit set (%d → %d) — a cancel must never move anybody", len(before), len(visits))
	}
	// The by-hand path is still there for a crew that turns up regardless.
	if !strings.Contains(after, `action="/gate/checkin"`) {
		t.Error("the by-hand arriving-vehicle form is gone — a crew that arrives off-record could not be recorded")
	}
}

// An empty list SAYS it is empty, and says what that does and does not mean.
// A section that vanishes when it has nothing to say is indistinguishable
// from one that never ran (the F1 lesson), and here the difference is
// operationally load-bearing: "no open expectation" is not "nothing is
// coming" — a vehicle can outrun the sync.
func TestAnEmptyInboundListSaysSoAndDoesNotClaimNothingIsComing(t *testing.T) {
	srv, _, _ := newTripsTestServer(t)
	page := noScriptMarkup(t, srv, "/gate/vehicle")
	if strings.Contains(page, "data-inbound-vehicle=") {
		t.Fatal("precondition: something is inbound, so this is not the empty case")
	}
	for _, want := range []string{
		"No vehicles are recorded as on the road to",
		"not that nothing is coming",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the empty inbound list does not say %q:\n%s", want, page)
		}
	}
}

// --- the overdue case ----------------------------------------------------

// The four faces, driven straight from deadline.Status. This is the
// translation table applyAlertFace owns, asserted as a table so a fifth
// status added later without a case here shows up as a row nobody wrote.
func TestTheScanVerdictDrivesTheInboundRowsFace(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    string
		wantUrge  string
		wantLabel string
		wantOpen  bool
	}{
		{"countdown", "countdown", "due", "DUE IN", false},
		{"live overdue", "overdue_live", "overdue", "OVERDUE", true},
		{"unresolved", "overdue_unresolved", "unresolved", "OVERDUE — UNRESOLVED", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := inboundVehicleRow{Urgency: "none", UrgencyLabel: "NO TIMER"}
			applyAlertFace(&row, alertWithStatus(tc.status))
			if row.Urgency != tc.wantUrge || row.UrgencyLabel != tc.wantLabel {
				t.Errorf("status %s ⇒ urgency %q/%q, want %q/%q", tc.status, row.Urgency, row.UrgencyLabel, tc.wantUrge, tc.wantLabel)
			}
			if row.Open != tc.wantOpen {
				t.Errorf("status %s ⇒ Open=%v, want %v — an overdue run must not be one click away from being noticed", tc.status, row.Open, tc.wantOpen)
			}
			if row.Countdown == "" {
				t.Errorf("status %s left the row with no countdown text at all", tc.status)
			}
		})
	}
	// The unresolved row carries NO numeral: the deadline passed while the
	// link was down, so there is no honest figure. A number would be a
	// fabrication, and this is the one row an officer is most likely to act
	// on.
	row := inboundVehicleRow{Urgency: "none", UrgencyLabel: "NO TIMER"}
	applyAlertFace(&row, alertWithStatus("overdue_unresolved"))
	if !strings.Contains(row.Countdown, "verify by radio") {
		t.Errorf("the unresolved row does not tell the officer to verify by radio: %q", row.Countdown)
	}
	if regexp.MustCompile(`\d`).MatchString(row.Countdown) {
		t.Errorf("the unresolved row carries a numeral (%q) — no honest figure exists for this state", row.Countdown)
	}
}

// An unresolved row is told apart by SHAPE, not by hue: "the timer expired
// while the link was down" must never be mistaken for one more ordinary red
// row by a reader who cannot separate the two reds. The ordinary row is
// solid; the unresolved one is dashed — the same distinction .board-row
// draws on the console, asserted independently here because these are two
// different elements and a restyle can move one without the other.
func TestTheUnresolvedInboundRowIsToldApartByShapeNotHue(t *testing.T) {
	srv, _, _ := newTripsTestServer(t)
	rules := paperStylesheet(t, rawPage(t, srv, "/gate/vehicle"))

	base, ok := selectorValue(rules, ".inbound-row", "border")
	if !ok {
		t.Fatal("the inbound row has no border at all — it would render as an unbounded block")
	}
	dashed, ok := selectorValue(rules, `.inbound-row[data-urgency="unresolved"]`, "border")
	if !ok {
		t.Fatal("the unresolved inbound row has no border of its own — it is indistinguishable from an ordinary row by anything but hue")
	}
	if !strings.Contains(base, "solid") {
		t.Errorf("the ordinary inbound row is not solid: %q", base)
	}
	if !strings.Contains(dashed, "dashed") {
		t.Errorf("the unresolved inbound row is not dashed: %q", dashed)
	}
	if base == dashed {
		t.Errorf("an ordinary and an unresolved inbound row draw the same box (%q) — only hue would tell them apart", dashed)
	}
	// ...and the WORD is there too, from the scan itself, so the state
	// survives a monochrome screen and a reader who never sees the border.
	row := inboundVehicleRow{}
	applyAlertFace(&row, alertWithStatus("overdue_unresolved"))
	if row.UrgencyLabel != "OVERDUE — UNRESOLVED" {
		t.Errorf("the unresolved row's word is %q — shape alone is not enough", row.UrgencyLabel)
	}
}

// selectorValue resolves one property for an EXACT selector, last rule
// wins. paperClassValue matches a class alone and would return whichever
// urgency variant came last, which is exactly the cascade confusion the
// parser exists to prevent.
func selectorValue(rules []cssRule, selector, prop string) (string, bool) {
	var got string
	var found bool
	for _, r := range rules {
		for _, sel := range r.selectors {
			if sel != selector {
				continue
			}
			for _, d := range r.decls {
				if d.prop == prop {
					got, found = d.value, true
				}
			}
		}
	}
	return got, found
}

// alertWithStatus builds the one deadline.Alert shape applyAlertFace reads.
// Constructed here rather than driven through a real scan because THIS test
// is about the translation table, not about the scan: a scan-driven fixture
// would prove the scan works and say nothing about whether every status has
// a case of its own. deadline.Deadlines' own tests own the scan.
func alertWithStatus(status string) deadline.Alert {
	a := deadline.Alert{TripID: "t", Status: deadline.Status(status)}
	switch deadline.Status(status) {
	case deadline.StatusCountdown:
		a.Remaining = 12*time.Minute + 30*time.Second
	case deadline.StatusOverdueLive:
		a.OverdueBy = 4 * time.Minute
	}
	return a
}

// --- the D-3 boundary ---------------------------------------------------

// This list is NOT the manifest confirmation D-3 waives, and the difference
// is structural rather than a matter of wording: D-3 forbids entering
// somebody onto the muster without the operator affirming that person
// INDIVIDUALLY. The surface therefore has to offer a per-person control for
// every occupant — never a single "confirm this vehicle" action.
//
// Asserted here so a future optimisation that collapses the per-person
// controls into one button ("it's always everyone anyway") goes red with
// the reason attached, rather than looking like a harmless simplification.
func TestTheGateListAffirmsEachPersonIndividuallyAsD3Requires(t *testing.T) {
	w := newInboundWorld(t)
	block := vehicleBlock(t, noScriptMarkup(t, w.srv, "/gate/vehicle"), "TRK-100")
	boxes := strings.Count(block, `name="arriving_person_ids"`)
	if boxes != len(w.aboard) {
		t.Fatalf("the vehicle offers %d per-person controls for %d occupants — D-3 requires the operator to affirm each person individually, never to confirm a list", boxes, len(w.aboard))
	}
	// ...and the affirmation is per PERSON, not per row-position: each
	// control carries the person's own id, so deleting or reordering a row
	// can never re-point a tick at somebody else.
	for _, p := range w.aboard {
		if !strings.Contains(block, `name="arriving_person_ids" value="`+p.PersonID+`"`) {
			t.Errorf("%s's control is not keyed on their own person id", p.Name)
		}
	}
}
