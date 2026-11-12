// console_test.go — U2's done-when tests for the /trips split: the
// operator console (what somebody with a truck at the window needs) and
// /trips/detail (the reference/audit tables that used to be stacked under
// it). Nothing about what the system does changed, so these tests are
// mostly about WHERE a surface is and WHAT SHAPE it has — plus the two
// claims that are behavioural: every pre-existing route and field still
// works, and urgency is encoded in form, not only in hue.
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

// TestOperatorConsoleCarriesTheWorkingSurfaceAndTheDetailViewCarriesTheRest.
// The split is the point of the chunk: an operator should not scroll past
// three reference tables to reach the dispatch form while a driver waits.
func TestOperatorConsoleCarriesTheWorkingSurfaceAndTheDetailViewCarriesTheRest(t *testing.T) {
	srv, st, persons := newTripsTestServer(t)
	injectForeignTrip(t, st, 1, "TRUCK 12", 60, persons[0].PersonID, persons[1].PersonID)
	// A closed local visit, so the visits table has a row to show.
	if _, err := domain.DispatchLocalCheckIn(st, "site-b", 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRK-14", DriverPersonID: persons[0].PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, st, "site-b", "TRK-14"), DriverPersonID: persons[0].PersonID,
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}

	console := styleRe.ReplaceAllString(getTrips(t, srv), "")
	detail := styleRe.ReplaceAllString(getTripsDetail(t, srv), "")

	// The console: the board, the review lane, and both primary actions.
	for _, want := range []string{
		"Inbound",                            // the board
		"Needs a human",                      // the review lane
		`action="/trips/dispatch"`,           // a vehicle is leaving
		`action="/trips/arrive-unsolicited"`, // a vehicle has arrived
		`action="/trips/arrive"`,             // the board's own arrival action
	} {
		if !strings.Contains(console, want) {
			t.Errorf("the operator console is missing %q", want)
		}
	}

	// The reference tables are NOT on the console...
	for _, notWanted := range []string{"All trips known here", "<h2>Visits</h2>", "Recorded as out at uncontrolled destinations"} {
		if strings.Contains(console, notWanted) {
			t.Errorf("the operator console still carries the reference section %q — that is what /trips/detail is for", notWanted)
		}
	}
	// ...they are all on the detail view.
	// ⟦D-78, 2026-09-05⟧ ⟦was: the list below also required `action="/trips/cancel"` —
	// "the split must not have dropped a surface"⟧ The cancel form is no
	// longer a surface anywhere: D-73 ruled that no trip is cancelled and
	// D-78 closed the route. The assertion is INVERTED, not dropped.
	for _, want := range []string{
		"All trips known here", "<h2>Visits</h2>", "Recorded as out at uncontrolled destinations",
	} {
		if !strings.Contains(detail, want) {
			t.Errorf("/trips/detail is missing %q — the split must not have dropped a surface", want)
		}
	}
	// The fixture carries an OPEN trip (injectForeignTrip) — the one row
	// shape that used to render the Cancel button — so this negative is not
	// satisfied by a page with nothing to cancel.
	for name, page := range map[string]string{"the operator console": console, "/trips/detail": detail} {
		if strings.Contains(page, `action="/trips/cancel"`) {
			t.Errorf("%s still carries a form posting to /trips/cancel — D-78 closed that route; a form that posts to it is a button that does nothing, on the page the refusal sends the officer to", name)
		}
	}
	// And the detail page says WHY the column that held the button is empty,
	// in the officer's terms, and no longer describes the turnaround D-8/D-73
	// say cannot happen.
	if !strings.Contains(detail, "not cancelled") || strings.Contains(detail, "turned back re-enters") {
		t.Errorf("/trips/detail does not carry the D-73 note (\"…are not cancelled…\") or still carries the old cancel caption (\"a crew that turned back re-enters …\"). Page:\n%s", detail)
	}
	// And each page names the other, so neither is a dead end.
	if !strings.Contains(console, `href="/trips/detail"`) {
		t.Error("the console does not link to the detail view")
	}
	if !strings.Contains(detail, `href="/trips"`) {
		t.Error("the detail view does not link back to the console")
	}
}

// TestEveryPreExistingTripRouteAndFieldStillWorks. The brief for this chunk
// is explicit: keep every existing route, form field name and POST target
// working. This drives each of them with the field names they had before
// the re-composition and asserts on store state, not on markup.
func TestEveryPreExistingTripRouteAndFieldStillWorks(t *testing.T) {
	srv, w := newConsoleTestServer(t)

	// /trips/dispatch with driver_person_id + occupant_person_ids + the
	// legacy destination_site + deadline_minutes.
	rec := postForm(t, srv, "/trips/dispatch", url.Values{
		"time_source": {"default"},
		"vehicle":     {"LEGACY 1"}, "driver_person_id": {w.onSite[0].PersonID},
		"occupant_person_ids": {w.onSite[1].PersonID},
		"destination_site":    {"site-a"}, "deadline_minutes": {"60"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("dispatch with the pre-existing fields = %d: %s", rec.Code, rec.Body.String())
	}
	trips, err := domain.Trips(w.st)
	if err != nil {
		t.Fatal(err)
	}
	var away *domain.Trip
	for i := range trips {
		if trips[i].DestinationSite != nil {
			away = &trips[i]
		}
	}
	if away == nil {
		t.Fatal("destination_site no longer authors a controlled trip")
	}

	// /trips/cancel with trip_id — ⟦D-78, 2026-09-05⟧ CLOSED, and asserted
	// closed rather than dropped from this walk. ⟦was: "if rec := postForm(…, "/trips/cancel", …); rec.Code != http.StatusSeeOther { t.Fatalf("cancel = %d: %s", …) }"⟧
	// The brief this test serves ("keep every existing route working") is
	// superseded for this one route by D-73/D-78: no trip is cancelled. The
	// pre-existing field shape is posted exactly as before; the proof is on
	// the store, never the status: nothing authored, trip still open.
	{
		before := w2Watermark(t, w.st, srv.Cfg.SiteID, srv.Cfg.NodeEpoch)
		rec := postForm(t, srv, "/trips/cancel", url.Values{"time_source": {"default"}, "trip_id": {away.TripID}})
		if after := w2Watermark(t, w.st, srv.Cfg.SiteID, srv.Cfg.NodeEpoch); after != before {
			t.Fatalf("D-78: the closed cancel route authored %d event(s) (response %d: %s)", after-before, rec.Code, rec.Body.String())
		}
		state, err := domain.TripStateByID(w.st, away.TripID)
		if err != nil {
			t.Fatal(err)
		}
		if state == nil || state.State != domain.TripStateOpen {
			t.Fatalf("D-78: after the old cancel POST the trip reads %v, want %s", state, domain.TripStateOpen)
		}
	}

	// /trips/arrive-unsolicited with vehicle + origin_site + person_ids.
	rec = postForm(t, srv, "/trips/arrive-unsolicited", url.Values{
		"time_source": {"default"},
		"vehicle":     {"MYSTERY 2"}, "origin_site": {"site-a"},
		"person_ids": {w.offSite[0].PersonID},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unsolicited arrival with the pre-existing fields = %d: %s", rec.Code, rec.Body.String())
	}

	// /trips/arrive with trip_id + stayed_person_ids.
	srv2, st2, persons := newTripsTestServer(t)
	tripID := injectForeignTrip(t, st2, 1, "BUS 9", 60, persons[0].PersonID, persons[1].PersonID)
	rec = postForm(t, srv2, "/trips/arrive", url.Values{
		"time_source": {"default"},
		"trip_id":     {tripID}, "stayed_person_ids": {persons[1].PersonID},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("arrive with the pre-existing fields = %d: %s", rec.Code, rec.Body.String())
	}
	open, err := domain.OpenVisitsAt(st2, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].PersonID != persons[0].PersonID {
		t.Fatalf("stayed_person_ids no longer excludes the named person: %+v", open)
	}
}

// --- urgency in form, not only in hue --------------------------------

var boardRowRe = regexp.MustCompile(`(?s)<article class="board-row" data-urgency="([^"]+)">(.*?)</article>`)

// TestTheBoardEncodesUrgencyInWordsNotOnlyInColour. The muster runs in a
// workplace where colour-blindness is uncontrolled for, so every board row
// carries an uppercase word for its state as well as a stripe.
//
// M-03 (test-efficacy S1-5). This test's comment has always claimed the
// words are "asserted to DIFFER between states", and until 2026-08-21 it
// did not assert that at all: the `seen` map only ever compared two rows
// that shared an urgency, so a template rendering the SAME constant word
// on every row — mutation m8, `<span class="board-urgency">DUE IN</span>`,
// which puts "DUE IN" on a genuinely overdue vehicle — passed green. That
// is precisely the vacuity the comment said was precluded, and the reader
// it fails is the colour-blind operator the constraint exists for.
//
// Two things were needed, and one without the other is still vacuous:
//
//  1. THE FIXTURE MUST CARRY MORE THAN ONE BAND. `newAlertingServer` alone
//     produces a single partition-expired row, so "every urgency has its
//     own word" is true of it no matter what the template does. The
//     fixture below poses THREE genuinely different scan verdicts, each
//     through the wired scanner rather than the C6 stand-in.
//  2. THE WORDS MUST BE ASSERTED PAIRWISE DISTINCT ACROSS urgencies, not
//     merely consistent within one.
//
// The band-count precondition is load-bearing and is a Fatal: if a future
// fixture change collapses the board back to one urgency, this test must
// go red rather than quietly return to proving nothing.
func TestTheBoardEncodesUrgencyInWordsNotOnlyInColour(t *testing.T) {
	srv, st, d, _ := newAlertingServer(t)          // partition-expired -> "unresolved"
	dispatchLiveOverdueTrip(t, st, d, "RED TRUCK") // healthy deadline run out -> "overdue"
	dispatchCountdownTrip(t, st, d, "GREEN TRUCK") // still counting down    -> "due"

	body := styleRe.ReplaceAllString(rawPage(t, srv, "/trips"), "")
	rows := boardRowRe.FindAllStringSubmatch(body, -1)
	if len(rows) == 0 {
		t.Fatalf("precondition: the alerting fixture rendered no board rows at all:\n%s", body)
	}
	seen := map[string]string{}
	for _, m := range rows {
		urgency, inner := m[1], m[2]
		label := regexp.MustCompile(`<span class="board-urgency">([^<]*)</span>`).FindStringSubmatch(inner)
		if label == nil {
			t.Fatalf("a board row with urgency %q carries no word for its state — hue alone is not allowed to be the signal:\n%s", urgency, inner)
		}
		word := strings.TrimSpace(label[1])
		if word == "" || word != strings.ToUpper(word) {
			t.Errorf("board urgency word %q is not an unmistakable uppercase state", word)
		}
		if prev, ok := seen[urgency]; ok && prev != word {
			t.Errorf("urgency %q rendered two different words (%q, %q)", urgency, prev, word)
		}
		seen[urgency] = word
	}
	// The fixture holds a real overdue expectation, so the strongest state
	// must actually be reachable — otherwise this test only ever saw one
	// band and proved nothing about encoding.
	if _, ok := seen["overdue"]; !ok {
		if _, ok := seen["unresolved"]; !ok {
			t.Fatalf("precondition: no overdue row in a fixture built to have one; states seen: %v", seen)
		}
	}
	// PRECONDITION for the distinctness assertion below. Without this the
	// pairwise check is vacuously true on a one-band board, which is how
	// m8 survived.
	if len(seen) < 3 {
		t.Fatalf("precondition: the board rendered %d urgency band(s) (%v), and distinctness cannot be tested below 3. This fixture is built to produce a running countdown, a live overdue and a partition-expired row; if the scan no longer separates them, fix the fixture — do not weaken the assertion", len(seen), seen)
	}
	// M-03: the WORDS, pairwise. Two states that share a word are two
	// states an operator can only tell apart by hue.
	byWord := map[string]string{}
	for urgency, word := range seen {
		if other, clash := byWord[word]; clash {
			t.Errorf("urgency %q and urgency %q both render the word %q. The stripe is then the ONLY thing separating them, which is the encoding the UI brief forbids — an operator who cannot see the hue reads two different states as the same one", urgency, other, word)
			continue
		}
		byWord[word] = urgency
	}
}

// TestTheBoardPutsTheRowThatNeedsAHumanFirst. Presentation only — the same
// rows, in the order an operator scans for.
func TestTheBoardPutsTheRowThatNeedsAHumanFirst(t *testing.T) {
	srv, st, persons := newTripsTestServer(t)
	// Two inbound expectations: one with a long deadline (still counting
	// down) and one already overdue by construction (a zero-minute
	// deadline, so the C6 stand-in scan reports it overdue immediately).
	injectForeignTrip(t, st, 1, "PATIENT 1", 600, persons[0].PersonID, persons[1].PersonID)
	injectForeignTrip(t, st, 2, "URGENT 2", 0, persons[0].PersonID, persons[1].PersonID)

	body := styleRe.ReplaceAllString(getTrips(t, srv), "")
	rows := boardRowRe.FindAllStringSubmatch(body, -1)
	if len(rows) != 2 {
		t.Fatalf("precondition: %d board rows, want 2", len(rows))
	}
	if rows[0][1] != "overdue" {
		t.Fatalf("the first board row has urgency %q — the row that needs a human must be first (order: %q then %q)", rows[0][1], rows[0][1], rows[1][1])
	}
	if !strings.Contains(rows[0][2], "URGENT 2") {
		t.Fatalf("the first row is not the overdue vehicle:\n%s", rows[0][2])
	}
}

// TestTheCountdownIsTheLargestNumeralOnThePage. The UI brief makes this a
// requirement, not a preference: the countdown is what somebody scanning
// the room reads from their chair. Asserted against the served stylesheet's
// own font-size scale rather than by eye.
func TestTheCountdownIsTheLargestNumeralOnThePage(t *testing.T) {
	css := stylesheetOf(t, getTrips(t, newConsoleServerForCSS(t)))
	sizeRe := regexp.MustCompile(`--fs-([a-z0-9-]+):\s*([0-9.]+)rem`)
	sizes := map[string]float64{}
	for _, m := range sizeRe.FindAllStringSubmatch(blockAfter(t, css, ":root {"), -1) {
		v, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			t.Fatal(err)
		}
		sizes[m[1]] = v
	}
	countdown, ok := sizes["countdown"]
	if !ok {
		t.Fatal("precondition: no --fs-countdown token in the stylesheet")
	}
	if len(sizes) < 4 {
		t.Fatalf("precondition: only %d font-size tokens found, so 'largest' is not being tested", len(sizes))
	}
	for name, v := range sizes {
		if name == "countdown" {
			continue
		}
		if v >= countdown {
			t.Errorf("--fs-%s is %.3grem, not smaller than --fs-countdown (%.3grem) — the countdown must be the largest numeral on the page", name, v, countdown)
		}
	}
	// And it must actually be applied to the board's countdown value —
	// measured on a board driven by the WIRED deadline scanner, which is
	// the path the shipped binary takes. The fixture used to be a server
	// with srv.Alerts == nil, so every assertion below ran against the C6
	// render-time stand-in and the real scan path was untested: a mutation
	// forcing a permanent em-dash on the scanner's own countdown branch
	// SURVIVED green, measured.
	srv, _, _ := newCountdownBoardServer(t)
	if srv.Alerts == nil {
		t.Fatal("precondition: this fixture must have the deadline scanner WIRED, or the board takes the stand-in path and the scan branch is never exercised")
	}
	body := styleRe.ReplaceAllString(getTrips(t, srv), "")
	if !strings.Contains(body, "remaining") {
		t.Fatalf("precondition: the wired fixture produced no live countdown row:\n%s", body)
	}
	// The .countdown element must carry the NUMERAL and nothing else: a
	// prose status rendered at 2.5rem produced a four-line headline reading
	// "overdue-unresolved — verify by radio" (seen in Firefox, 2026-08-20).
	m := regexp.MustCompile(`<span class="countdown">([^<]*)</span>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("the board renders no .countdown element at all:\n%s", body)
	}
	value := strings.TrimSpace(m[1])
	// T2 arm 2. This assertion used to be `^([0-9]|—)`, which accepts an
	// em-dash BY DESIGN — the em-dash is the honest rendering for
	// overdue-unresolved, where no figure exists. But the fixture above is
	// a trip with a LIVE FUTURE DEADLINE, so on this row an em-dash is not
	// honest, it is a lost signal: a mutation that made every countdown a
	// permanent em-dash left the board's primary urgency figure gone on
	// every row and the whole suite green, measured.
	//
	// The two cases are therefore asserted separately, each against a
	// fixture that actually produces it.
	if !regexp.MustCompile(`^[0-9]`).MatchString(value) {
		t.Fatalf("a trip with a live future deadline renders its countdown as %q. This row HAS an honest figure — %q is what the board is scanned for from across the room. An em-dash is correct only for overdue-unresolved, where no figure exists; here it is the loss of the board's primary urgency signal.", value, "a numeral")
	}
	if len(value) > 12 {
		t.Fatalf("the countdown headline is %q (%d chars) — too long to be a numeral, so it will wrap into a multi-line headline at 2.5rem", value, len(value))
	}
	if !strings.Contains(body, "remaining") {
		t.Fatalf("the board no longer says what the countdown figure means:\n%s", body)
	}
	// R7c. This was a same-line substring scan of the stylesheet SOURCE —
	// `strings.Contains(css, ".countdown { font-size: var(--fs-countdown)")`
	// — which is the defect class V-03 exists to remove and which the print
	// guards were rewritten away from in the same pass. It is now resolved
	// through the CSS parser, so what is asserted is the value the element
	// actually receives after the cascade, not a line of text that happens
	// to appear. See screenValue (printcss_test.go) for the two evasions
	// this closes.
	rules := parseCSS(t, css)
	got, ok := screenClassValue(rules, "countdown", "font-size")
	if !ok {
		t.Fatal(".countdown has no font-size at all after the cascade — the countdown is rendering at the body size, which is the one thing the UI brief says it must not do")
	}
	if strings.TrimSpace(got) != "var(--fs-countdown)" {
		t.Errorf(".countdown resolves to font-size: %s, not var(--fs-countdown). The token scale asserted above is then decorative: raising --fs-countdown would not change what the operator sees from across the room", got)
	}

	// The other half of the same rule: where there IS no honest figure the
	// board must say so with the em-dash rather than invent one. Asserting
	// only the numeral case would leave "always render a number" passing,
	// which on an overdue-unresolved row is a fabricated countdown for a
	// deadline that expired while the link was down.
	alertSrv, _, _, _ := newAlertingServer(t)
	alertBody := styleRe.ReplaceAllString(getTrips(t, alertSrv), "")
	am := regexp.MustCompile(`<span class="countdown">([^<]*)</span>`).FindStringSubmatch(alertBody)
	if am == nil {
		t.Fatalf("the alerting fixture renders no .countdown element:\n%s", alertBody)
	}
	// Precondition: this fixture really is the no-figure state.
	if !strings.Contains(alertBody, "overdue-unresolved") {
		t.Fatalf("precondition: the alerting fixture is not in the overdue-unresolved state, so the em-dash case is not being tested:\n%s", alertBody)
	}
	if got := strings.TrimSpace(am[1]); got != "—" {
		t.Errorf("an overdue-unresolved row renders its countdown as %q, want an em-dash — the deadline passed while the link was down, so any numeral here is a fabrication", got)
	}
}

// newCountdownBoardServer wires the REAL deadline scanner and holds one
// inbound expectation whose deadline is live and in the future — i.e. a row
// in StatusCountdown, the state that HAS an honest figure. The countdown
// assertions need this rather than a scanner-less server, because without
// srv.Alerts the board falls back to the C6 render-time stand-in and the
// branch the shipped binary actually runs is never rendered.
func newCountdownBoardServer(t *testing.T) (*Server, *store.Store, []domain.Person) {
	t.Helper()
	srv, st, persons := newTripsTestServer(t)
	if err := deadline.Register(st); err != nil {
		t.Fatal(err)
	}
	d, err := deadline.New(st, "site-b", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	srv.Alerts = d
	injectForeignTrip(t, st, 1, "TRUCK 12", 60, persons[0].PersonID, persons[1].PersonID)
	envs, err := st.Events("site-a", 1, 1, 1)
	if err != nil || len(envs) != 1 {
		t.Fatalf("fetch trip envelope: %v", err)
	}
	// Seen just now, 60 minutes on the clock: a running countdown.
	d.OnApplied(envs[0], time.Now(), true)
	return srv, st, persons
}

func newConsoleServerForCSS(t *testing.T) *Server {
	t.Helper()
	srv, _ := newConsoleTestServer(t)
	return srv
}

// TestConsoleStillRunsWithoutTheDeadlineScanner keeps the C6 stand-in path
// honest after the board was restructured: a web-layer server with no
// Alerts wired must still render a countdown and a staleness figure.
func TestConsoleStillRunsWithoutTheDeadlineScanner(t *testing.T) {
	srv, st, persons := newTripsTestServer(t)
	if srv.Alerts != nil {
		t.Fatal("precondition: this fixture is supposed to have no deadline scanner wired")
	}
	injectForeignTrip(t, st, 1, "TRUCK 12", 60, persons[0].PersonID, persons[1].PersonID)
	body := styleRe.ReplaceAllString(getTrips(t, srv), "")
	for _, want := range []string{"Countdown", "Staleness", "remaining", "held "} {
		if !strings.Contains(body, want) {
			t.Errorf("the console lost %q when no scanner is wired", want)
		}
	}
	// The stand-in must render the same SHAPE as the scan path, not merely
	// "something". This branch has its own copy of the countdown
	// derivation, so an em-dash regression here would not be caught by the
	// wired-path assertion in
	// TestTheCountdownIsTheLargestNumeralOnThePage — measured: it survived.
	m := regexp.MustCompile(`<span class="countdown">([^<]*)</span>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("the stand-in board renders no .countdown element:\n%s", body)
	}
	if v := strings.TrimSpace(m[1]); !regexp.MustCompile(`^[0-9]`).MatchString(v) {
		t.Errorf("with no scanner wired, a live future deadline renders its countdown as %q — the stand-in must produce the same numeral the scan path does, or the board silently loses its urgency figure on exactly the boxes whose scanner failed to start", v)
	}
}
