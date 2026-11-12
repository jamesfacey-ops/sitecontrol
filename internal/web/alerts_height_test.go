// alerts_height_test.go — V-01's guard.
//
// THE DEFECT. The C7 alert surface used to render every outstanding alert
// inside the STICKY top bar. One sync outage produces 18. Measured in
// Firefox at 1920x1080 (innerHeight 994 after browser chrome), 18 alert
// rows made the bar 1043px tall — taller than the viewport — so at >=11
// alerts it painted over the entire page at EVERY scroll offset. On
// /muster that meant AWAY and NOT TRACKED at 0% visible at every scroll
// position, together with the page's own Print button: the page that says
// who is on site became unreachable exactly when the site was having the
// kind of day that produces 18 alerts.
//
// WHY THE OLD GUARD DID NOT SEE IT.
// TestAlertBannerRidesInsideTheTopBarOnEveryScanningPage is four substring
// checks over served markup, run against newAlertingServer, which
// dispatches exactly ONE trip. Every one of its assertions is satisfied by
// a bar containing one alert, by a bar containing fifty, and by a bar whose
// alerts are collapsed behind a <details> — it passes identically before
// and after the fix, so it defends the mount point and nothing about the
// hazard.
//
// WHAT THIS ASSERTS INSTEAD, on a >=11-alert fixture with a rule-6
// precondition that the fixture really produced them:
//
//	a. the STRIP renders a stated, bounded number of vehicles — at most
//	   six per colour, as the FIX13 compact list (⟦was: one full alert
//	   row⟧) — whatever the total is;
//	b. the deferred count is stated IN the always-visible strip, and states
//	   the true total;
//	c. the row in the strip is the worst one outstanding;
//	d. nothing in the alert surface is collapsed or scrolled — no
//	   <details>, no overflow, no max-height. An alert that requires
//	   scrolling is an alert that gets missed, and one behind a disclosure
//	   triangle is worse: the page then renders as though nothing is wrong;
//	e. every alert remains individually acknowledgeable somewhere.
package web

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// newManyAlertsServer builds the outage the finding is about: one live
// overdue vehicle and manyAlertsCount-1 partition-expired expectations, all
// outstanding at once.
const manyAlertsCount = 18

func newManyAlertsServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	srv, st, _ := newTripsTestServer(t)
	if err := deadline.Register(st); err != nil {
		t.Fatal(err)
	}
	d, err := deadline.New(st, "site-b", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	srv.Alerts = d

	dest := "site-b"
	x := int64(2)
	for i := 0; i < manyAlertsCount-1; i++ {
		trip, err := domain.Dispatch(st, "site-a", 1, domain.DispatchInput{
			// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered:  fmt.Sprintf("OUTAGE TRUCK %02d", i),"⟧
			VehicleID:       w7SeedVehicleID(t, st, "site-a", fmt.Sprintf("OUTAGE TRUCK %02d", i)),
			DriverPersonID:  "driver-a",
			DestinationSite: &dest, DeadlineMinutes: &x,
		}, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		envs, err := st.Events("site-a", 1, trip.Seq, 1)
		if err != nil || len(envs) != 1 {
			t.Fatalf("fetch envelope %d: %v", i, err)
		}
		// Capture says the deadline passed while the link was down.
		d.OnApplied(envs[0], time.Now().Add(-(2*time.Minute+30*time.Second)), true) // off the whole-minute rounding boundary
	}
	// And one genuine live-overdue vehicle, which outranks all of them.
	dispatchLiveOverdueTrip(t, st, d, "THE WORST ONE")
	return srv, st
}

var alertRowRe = regexp.MustCompile(`data-alert-row="[^"]*"`)

func topBarOf(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`(?s)<header class="topbar">.*?</header>`).FindString(body)
	if m == "" {
		t.Fatalf("no top bar in the served page")
	}
	return m
}

func TestTheStickyAlertStripStaysBoundedHoweverManyAlertsAreOutstanding(t *testing.T) {
	srv, _ := newManyAlertsServer(t)

	// RULE 6 PRECONDITION. Everything below is meaningless unless the
	// fixture really produced the outage it claims to. This is the failure
	// the old guard had: its fixture held one alert, so "the bar is
	// bounded" was true by accident.
	rows, bannerErr := srv.banner()
	if bannerErr != "" {
		t.Fatalf("precondition: the scan failed (%s)", bannerErr)
	}
	if len(rows) < 11 {
		t.Fatalf("precondition: the fixture produced %d outstanding alerts, want >= 11 — below that threshold the bar fits the viewport and this test proves nothing", len(rows))
	}
	total := len(rows)

	// The sort banner() applies is what makes the strip's single slot a
	// guarantee rather than a coincidence.
	if rows[0].Status != deadline.StatusOverdueLive {
		t.Fatalf("the worst outstanding alert is not first in the banner (got %q) — the strip shows rows[0], so without the worst-first sort it shows whatever the scan emitted first", rows[0].Status)
	}

	for _, path := range []string{"/status", "/trips", "/trips/detail", "/gate", "/muster", "/muster/print"} {
		body := get(t, srv, path)
		bar := topBarOf(t, body)

		// (a) BOUNDED BY CONSTRUCTION: no alert ROW in the bar, and at most
		// stripItemsWide vehicles per line whatever the total is.
		// ⟦was: exactly one alert row (V-01). FIX13 (James, 2026-10-05): the
		// strip is a compact list — vehicle and people aboard — of at most 6
		// per colour then "+N more"⟧
		if got := len(alertRowRe.FindAllString(bar, -1)); got != 0 {
			t.Errorf("%s: the sticky bar carries %d full alert rows — the strip is the compact list; full rows belong under Outstanding alerts", path, got)
		}
		lines := fix13Lines(t, bar)
		for kind, line := range lines {
			if got := strings.Count(line, `data-alert-item=`); got > stripItemsWide {
				t.Errorf("%s: the %s line lists %d vehicles with %d alerts outstanding, want at most %d. Its height must not follow the alert count — at 18 rows it measured 1043px against a 994px viewport and painted over the whole page, muster included, at every scroll offset.", path, kind, got, total, stripItemsWide)
			}
		}

		// (b) THE DEFERRED COUNT, in the always-visible strip, true.
		if !strings.Contains(bar, fmt.Sprintf(`data-alert-total="%d"`, total)) {
			t.Errorf("%s: the sticky bar does not state the true outstanding total (%d). Listing six is honest ONLY while the rest are counted; without it the operator infers the total from what fits on screen.\nbar was:\n%s", path, total, bar)
		}
		// … and in words: 17 ambers, 6 listed, "+11 more".
		if !strings.Contains(lines["verify"], fmt.Sprintf(`data-alert-more="%d"`, total-1-stripItemsWide)) {
			t.Errorf("%s: the VERIFY BY RADIO line does not count what it did not list (+%d more):\n%s", path, total-1-stripItemsWide, lines["verify"])
		}

		// (c) and the worst one is on the red line, first.
		// ⟦was: "VEHICLE OVERDUE" — Q-FH1 (James, 2026-10-04): alert rows lead with the vehicle id and direction⟧
		// ⟦was: the bar's one row is "THE WORST ONE … inbound — OVERDUE" — FIX13⟧
		if !strings.Contains(lines["overdue"], "<code>THE WORST ONE</code> (") {
			t.Errorf("%s: the sticky bar's OVERDUE line does not name the live overdue vehicle — it is being summarised away behind partition-expired ones.\nbar was:\n%s", path, bar)
		}

		// (d) NOTHING HIDDEN AND NOTHING SCROLLED. These are the two
		// repairs that would have satisfied the height problem by making
		// alerts harder to see, which is the opposite of the point.
		if strings.Contains(bar, "<details") {
			t.Errorf("%s: the alert surface is collapsed behind a <details>. A page with a hidden alert renders as though nothing is wrong.", path)
		}
		if strings.Contains(bar, "<summary") {
			t.Errorf("%s: the alert surface carries a <summary> — see above", path)
		}
	}

	// (d, continued) and the stylesheet must not scroll it either. A
	// substring check would miss a wrapped selector list, so this resolves
	// the declarations through the parser.
	rules := paperStylesheet(t, get(t, srv, "/trips"))
	for _, class := range []string{"topbar", "topbar-alerts"} {
		for _, prop := range []string{"overflow", "overflow-y", "max-height"} {
			d, _, ok := paperValue(rules, func(sel string) bool { return targetsClass(sel, class) }, prop)
			if !ok {
				continue
			}
			v := strings.ToLower(d.value)
			if prop == "max-height" {
				t.Errorf(".%s sets %s: %s — capping the alert surface's height is what turns an alert into something you have to scroll to find", class, prop, d.value)
				continue
			}
			if strings.Contains(v, "scroll") || strings.Contains(v, "auto") {
				t.Errorf(".%s sets %s: %s — an alert that requires scrolling is an alert that gets missed", class, prop, d.value)
			}
		}
	}

	// (e) Every alert stays individually acknowledgeable. The strip carries
	// no form (FIX13; ⟦was: one⟧); the console carries them all.
	board := get(t, srv, "/trips")
	if got := strings.Count(board, `action="/alerts/ack"`); got < total {
		t.Errorf("the operator console offers %d acknowledgment forms for %d outstanding alerts — moving the list out of the bar must not cost any alert its own ack", got, total)
	}
	if got := len(alertRowRe.FindAllString(board, -1)); got < total {
		t.Errorf("the operator console renders %d alert rows for %d outstanding alerts — the full list must be complete wherever it lives", got, total)
	}
	// And the console's copy is in the page BODY, not the bar: it must
	// survive removing the header entirely.
	bodyOnly := regexp.MustCompile(`(?s)<header class="topbar">.*?</header>`).ReplaceAllString(board, "")
	if got := len(alertRowRe.FindAllString(bodyOnly, -1)); got < total {
		t.Errorf("only %d of %d alert rows are outside the top bar — the full list must scroll with the page, which is what keeps the bar's height off the alert count", got, total)
	}
}

// The zero-alert case must not regress into a bar that claims something.
func TestTheAlertStripSaysNothingWhenNothingIsOutstanding(t *testing.T) {
	srv, _, _ := newTripsTestServer(t)
	body := get(t, srv, "/trips")
	bar := topBarOf(t, body)
	if strings.Contains(bar, "data-alert-total") {
		t.Errorf("the bar states an alert count on a page with no alerts — an empty count reads as a measurement:\n%s", bar)
	}
	if len(alertRowRe.FindAllString(bar, -1)) != 0 {
		t.Errorf("the bar renders an alert row with nothing outstanding:\n%s", bar)
	}
}

// T1 #1 — the sticky top bar. One of eight stated brief criteria that had
// no asserting test of ANY kind: no test file contained the string
// "sticky". Deleting one declaration scrolls the C7 alert surface off a
// busy console, which is the failure the brief names in the same sentence
// as the requirement.
//
// SEQUENCING, and it matters. This guard lands AFTER V-01, not before.
// Written first it could only have asserted `position: sticky` on
// `.topbar` — the very declaration V-01 was free to change — and would
// have cemented the shape that produced the hazard. Written now, it
// asserts the REQUIREMENT: the alert surface, and its count, ride a bar
// that the page cannot scroll away from.
func TestTheAlertSurfaceRidesABarThePageCannotScrollAwayFrom(t *testing.T) {
	srv, _ := newManyAlertsServer(t)

	// The behavioural half, on every page that runs the scan: the count is
	// IN the bar, not merely somewhere on the page. A count that lived in
	// the body would scroll away with it.
	for _, path := range []string{"/status", "/trips", "/trips/detail", "/gate", "/muster", "/muster/print"} {
		bar := topBarOf(t, get(t, srv, path))
		if !strings.Contains(bar, "data-alert-total=") {
			t.Errorf("%s: the outstanding-alert count is not inside the top bar", path)
		}
		if !strings.Contains(bar, `class="topbar-alerts"`) {
			t.Errorf("%s: the alert surface is not mounted in the top bar", path)
		}
	}

	// The declaration half, RESOLVED rather than grepped — a substring scan
	// could not see a later override, and this property is the one thing
	// standing between the alert surface and the top of a long page.
	rules := paperStylesheet(t, get(t, srv, "/trips"))
	match := func(sel string) bool { return targetsClass(sel, "topbar") }
	pos, rule, ok := paperValue(rules, match, "position")
	if !ok {
		t.Fatal(".topbar declares no position at all — the alert surface scrolls off the top of a busy console, which is the exact failure the brief names")
	}
	if v := strings.ToLower(pos.value); v != "sticky" && v != "fixed" {
		t.Errorf(".topbar resolves to position: %s — the alert surface must stay in the viewport however far the page scrolls. Winning rule: %s", pos.value, strings.TrimSpace(rule.raw))
	}
	if top, _, ok := paperValue(rules, match, "top"); !ok || strings.TrimSpace(top.value) != "0" {
		t.Errorf(".topbar is positioned but does not pin to top: 0 (got %q) — a sticky bar with no offset does not stick", top.value)
	}
	// And it must paint: a transparent sticky bar lets the page's own rows
	// scroll THROUGH the alert text, which reads as corruption rather than
	// as an alert.
	if bg, _, ok := paperValue(rules, match, "background"); !ok || strings.TrimSpace(bg.value) == "" || strings.Contains(strings.ToLower(bg.value), "transparent") {
		t.Errorf(".topbar paints no opaque background (%q) — page content would scroll through the alert surface", bg.value)
	}
}
