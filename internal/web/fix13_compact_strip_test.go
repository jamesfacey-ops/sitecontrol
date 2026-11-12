package web

// fix13_compact_strip_test.go — FIX13 (James, 2026-10-05): "the overdue
// banner at the top should show a concise shortened list of the overdue
// trucks and number of pax instead of the large banner just a small list with
// truck number and number of people". The sticky strip is a red OVERDUE line
// and an amber VERIFY BY RADIO line of "<vehicle> (<people aboard>)" items;
// the full rows and their acknowledgments stay on /trips, which each item
// links to on a post. The dispatch host gets the same list as plain text.
//
// FAIL BEFORE (4489278): the strip carried one full alert row with its ack
// form and a count sentence — no data-alert-line, no items, no "+N more", and
// an ack form in the bar.

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

// fix13Inbound authors a site-b -> site-a run of xMinutes into st with the
// given crew and captures it at site-a: live (a healthy countdown running out
// now — a red) or partitioned (the deadline passed while the link was down —
// an amber VERIFY BY RADIO).
func fix13Inbound(t *testing.T, st *store.Store, d *deadline.Deadlines, vehicle string, crew []string, live bool) domain.Trip {
	t.Helper()
	dest, x := "site-a", int64(1)
	if !live {
		x = 2
	}
	trip, err := domain.Dispatch(st, "site-b", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, st, "site-b", vehicle), DriverPersonID: crew[0],
		OccupantPersonIDs: crew[1:], DestinationSite: &dest, DeadlineMinutes: &x,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	envs, err := st.Events("site-b", 1, trip.Seq, 1)
	if err != nil || len(envs) != 1 {
		t.Fatalf("fetch envelope: %v", err)
	}
	if live {
		d.OnApplied(envs[0], time.Now().Add(-time.Minute+80*time.Millisecond), true)
	} else {
		d.OnApplied(envs[0], time.Now().Add(-(2*time.Minute + 30*time.Second)), true)
	}
	return trip
}

// fix13Post is THIS post (site-a, with a [road_work] table): three inbound
// overdue reds carrying 3, 1 and 2 people, one amber with 2, and its own
// road-work run (2 aboard) past its working limit.
func fix13Post(t *testing.T) (*roadWorkWeb, map[string]string) {
	t.Helper()
	w := newRoadWorkWeb(t, true)
	var people []string
	for i := 0; i < 8; i++ {
		p, err := domain.CreatePerson(w.st, "site-b", 1, fmt.Sprintf("Rider%02d, Pat", i), nil, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		people = append(people, p.PersonID)
	}
	ids := map[string]string{}
	ids["TRK-19"] = fix13Inbound(t, w.st, w.d, "TRK-19", people[0:3], true).TripID
	ids["TRK-20"] = fix13Inbound(t, w.st, w.d, "TRK-20", people[3:4], true).TripID
	ids["TRK-21"] = fix13Inbound(t, w.st, w.d, "TRK-21", people[4:6], true).TripID
	ids["TRK-22"] = fix13Inbound(t, w.st, w.d, "TRK-22", people[6:8], false).TripID
	time.Sleep(150 * time.Millisecond) // the live reds' last 80 ms run out
	rw := w.sendOut(t, nil)
	ids["GRADER 7"] = rw.TripID
	envs, err := w.st.Events("site-a", 1, rw.Seq, 1)
	if err != nil || len(envs) != 1 {
		t.Fatal(err)
	}
	authored := time.UnixMilli(envs[0].AuthoredAt.WallMS)
	w.d.WallNow = func() time.Time { return authored.Add(12*time.Hour + 5*time.Minute) }
	return w, ids
}

var fix13LineRe = regexp.MustCompile(`(?s)<div class="notice notice-(critical|warn) banner-notice alert-line" data-alert-line="(overdue|verify)">.*?</ul>`)

// fix13Lines returns the strip's lines by kind ("overdue", "verify").
func fix13Lines(t *testing.T, bar string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, m := range fix13LineRe.FindAllStringSubmatch(bar, -1) {
		out[m[2]] = m[0]
	}
	return out
}

// TestFix13StripListsVehiclesWithPeopleAboardOnARedAndAnAmberLine — the
// compact lines, with the right ids and counts, road work on the red line.
func TestFix13StripListsVehiclesWithPeopleAboardOnARedAndAnAmberLine(t *testing.T) {
	w, ids := fix13Post(t)
	rows, bannerErr := w.srv.banner()
	if bannerErr != "" || len(rows) != 5 {
		t.Fatalf("PRECONDITION: want 5 outstanding alerts (3 inbound reds, 1 amber, 1 road work), got %d (%s)", len(rows), bannerErr)
	}
	for _, path := range []string{"/gate", "/trips", "/muster"} {
		bar := topBarOf(t, w.get(t, path, true))
		lines := fix13Lines(t, bar)
		red, amber := lines["overdue"], lines["verify"]
		if red == "" || amber == "" || !strings.Contains(red, `notice-critical`) || !strings.Contains(amber, `notice-warn`) {
			t.Fatalf("%s: want a red OVERDUE line and an amber VERIFY BY RADIO line:\n%s", path, bar)
		}
		if !strings.Contains(red, `<strong class="alert-line-kind">OVERDUE:</strong>`) || !strings.Contains(amber, `<strong class="alert-line-kind">VERIFY BY RADIO:</strong>`) {
			t.Errorf("%s: the lines must state their kind in words:\n%s", path, bar)
		}
		for _, c := range []struct {
			line, vehicle, count, name string
		}{
			{red, "TRK-19", "3", "TRK-19, overdue, 3 people aboard"},
			{red, "TRK-20", "1", "TRK-20, overdue, 1 person aboard"},
			{red, "TRK-21", "2", "TRK-21, overdue, 2 people aboard"},
			{red, "GRADER 7", "2", "GRADER 7, overdue on the road, 2 people aboard"},
			{amber, "TRK-22", "2", "TRK-22, verify by radio, 2 people aboard"},
		} {
			if !strings.Contains(c.line, `<code>`+c.vehicle+`</code> (`+c.count+`)`) {
				t.Errorf("%s: line lacks %q with (%s) aboard:\n%s", path, c.vehicle, c.count, c.line)
			}
			if !strings.Contains(c.line, `<span class="sr-only">`+c.name+`</span>`) {
				t.Errorf("%s: item lacks the accessible name %q:\n%s", path, c.name, c.line)
			}
			if !strings.Contains(c.line, `data-trip-id="`+ids[c.vehicle]+`"`) {
				t.Errorf("%s: %s's item does not carry its trip", path, c.vehicle)
			}
		}
		if !regexp.MustCompile(`data-trip-id="` + regexp.QuoteMeta(ids["GRADER 7"]) + `" data-alert-road-work="true"`).MatchString(red) {
			t.Errorf("%s: the road-work item is not marked as road work:\n%s", path, red)
		}
		if strings.Contains(red, "TRK-22") || strings.Contains(amber, "TRK-19") {
			t.Errorf("%s: an item is on the wrong colour's line", path)
		}
		// Compact: no direction, origin, driver or due time in the bar.
		for _, gone := range []string{"inbound", "from Site", "driver", "due about", "aboard (", "Acknowledge"} {
			if strings.Contains(stripVisible(bar), gone) {
				t.Errorf("%s: the compact strip still carries %q:\n%s", path, gone, bar)
			}
		}
	}
}

// fix13Alerts is the top bar's alert surface alone (the rest of the bar has
// its own controls, the post chip's among them).
func fix13Alerts(t *testing.T, body string) string {
	t.Helper()
	bar := topBarOf(t, body)
	i := strings.Index(bar, `<div class="topbar-alerts">`)
	if i < 0 {
		t.Fatalf("no alert surface in the top bar:\n%s", bar)
	}
	return bar[i:]
}

// fix13AlertList is /trips' "Outstanding alerts" section — the full rows.
func fix13AlertList(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, `<h2 id="outstanding-alerts">`)
	if i < 0 {
		t.Fatalf("no Outstanding alerts section on the page")
	}
	sec := body[i:]
	if j := strings.Index(sec[1:], "<h2"); j >= 0 {
		sec = sec[:j+1]
	}
	return sec
}

// stripVisible drops the screen-reader-only names so a check can read what
// is painted.
func stripVisible(bar string) string {
	return regexp.MustCompile(`<span class="sr-only">[^<]*</span>`).ReplaceAllString(bar, "")
}

// TestFix13StripCarriesNoAckFormAndEachItemLandsOnItsAckableRow — post host:
// no form in the bar; each item links to /trips#alert-<trip>, and that row on
// /trips carries the alert's own Acknowledge form; the ack still works.
func TestFix13StripCarriesNoAckFormAndEachItemLandsOnItsAckableRow(t *testing.T) {
	w, ids := fix13Post(t)
	trips := w.get(t, "/trips", true)
	for _, path := range []string{"/gate", "/gate/vehicle", "/muster", "/status"} {
		bar := fix13Alerts(t, w.get(t, path, true))
		if strings.Contains(bar, "<form") || strings.Contains(bar, "/alerts/ack") || strings.Contains(bar, "<button") {
			t.Errorf("%s: the strip carries a form — acknowledgment lives on /trips:\n%s", path, bar)
		}
		for v, id := range ids {
			if !strings.Contains(bar, `<a href="/trips#alert-`+id+`">`) {
				t.Errorf("%s: %s's item does not link to its /trips row", path, v)
			}
		}
	}
	// On /trips itself the links stay on the page.
	tbar := fix13Alerts(t, trips)
	if strings.Contains(tbar, "<form") {
		t.Errorf("/trips: the strip carries a form:\n%s", tbar)
	}
	body := regexp.MustCompile(`(?s)<header class="topbar">.*?</header>`).ReplaceAllString(trips, "")
	for v, id := range ids {
		if !strings.Contains(tbar, `<a href="#alert-`+id+`">`) {
			t.Errorf("/trips: %s's item does not link to its row on the page", v)
		}
		row := regexp.MustCompile(`(?s)<div class="notice [^"]*" id="alert-` + regexp.QuoteMeta(id) + `" data-alert-row="[^"]+".*?</form>`).FindString(body)
		if row == "" || !strings.Contains(row, `<input type="hidden" name="trip_id" value="`+id+`">`) {
			t.Errorf("/trips: %s has no anchored row with its own Acknowledge form", v)
		}
	}
	// And the gate officer can ack every one of them from the console.
	for v, id := range ids {
		rec := postForm(t, w.srv, "/alerts/ack", map[string][]string{"trip_id": {id}})
		if rec.Code != 303 {
			t.Errorf("ack %s from /trips = %d: %s", v, rec.Code, rec.Body.String())
		}
	}
	if bar := topBarOf(t, w.get(t, "/gate", true)); strings.Contains(bar, "alert-line") {
		t.Errorf("every alert was acknowledged but the strip still lists vehicles:\n%s", bar)
	}
}

// TestFix13DispatchHostStripIsPlainTextWithNoConsoleLinksOrForms — the same
// items on the dispatch host's pages, with no link and no form.
func TestFix13DispatchHostStripIsPlainTextWithNoConsoleLinksOrForms(t *testing.T) {
	w, _ := fix13Post(t)
	for _, path := range []string{"/analytics", "/search?q=pat"} {
		rec := fix7Do(t, w.srv, "GET", fix7DispatchHost, path, nil)
		if rec.Code != 200 {
			t.Fatalf("dispatch %s = %d", path, rec.Code)
		}
		bar := topBarOf(t, rec.Body.String())
		strip := bar[strings.Index(bar, `id="alert-strip"`):]
		lines := fix13Lines(t, bar)
		if !strings.Contains(lines["overdue"], `<code>TRK-19</code> (3)`) || !strings.Contains(lines["verify"], `<code>TRK-22</code> (2)`) {
			t.Fatalf("dispatch %s: the compact list is missing:\n%s", path, bar)
		}
		if strings.Contains(strip, "<a ") || strings.Contains(strip, "<form") || strings.Contains(strip, "<button") {
			t.Errorf("dispatch %s: the strip carries a link or a form:\n%s", path, strip)
		}
		if !strings.Contains(strip, `<span class="alert-item-text">`) {
			t.Errorf("dispatch %s: items are not rendered as plain text:\n%s", path, strip)
		}
	}
}

// fix13Render executes the real alert_strip over a crafted banner slice.
func fix13Render(t *testing.T, data any) string {
	t.Helper()
	var sb strings.Builder
	if err := tmpl.ExecuteTemplate(&sb, "alert_strip", data); err != nil {
		t.Fatalf("alert_strip: %v", err)
	}
	return sb.String()
}

// TestFix13LongListsShowTheFirstSixThenPlusNMore — 8 reds and 3 ambers: six
// red items then "+2 more" (a phone shows two and "+6 more"); three amber
// items and only the phone's "+1 more". Plain text, not links, read-only.
func TestFix13LongListsShowTheFirstSixThenPlusNMore(t *testing.T) {
	var rows []deadline.Alert
	for i := 0; i < 8; i++ {
		rows = append(rows, deadline.Alert{TripID: fmt.Sprintf("r%d", i), Vehicle: fmt.Sprintf("RED-%d", i), Occupants: i + 1, Status: deadline.StatusOverdueLive})
	}
	for i := 0; i < 3; i++ {
		rows = append(rows, deadline.Alert{TripID: fmt.Sprintf("a%d", i), Vehicle: fmt.Sprintf("AMB-%d", i), Occupants: 1, Status: deadline.StatusOverdueUnresolved})
	}
	type page struct {
		Banner    []deadline.Alert
		BannerErr string
	}
	type roPage struct {
		Banner         []deadline.Alert
		BannerErr      string
		AlertsReadOnly bool
	}
	html := fix13Render(t, page{Banner: rows})
	lines := fix13Lines(t, html)
	red, amber := lines["overdue"], lines["verify"]
	if got := strings.Count(red, `data-alert-item=`); got != 6 {
		t.Errorf("red line renders %d items for 8 reds, want 6", got)
	}
	if got := strings.Count(red, `class="alert-item alert-item-wide"`); got != 4 {
		t.Errorf("red line marks %d items wide-only, want 4 (a phone shows 2)", got)
	}
	for i := 0; i < 6; i++ {
		if !strings.Contains(red, fmt.Sprintf(`<code>RED-%d</code> (%d)`, i, i+1)) {
			t.Errorf("red item %d missing or out of order", i)
		}
	}
	if strings.Contains(red, "RED-6") || strings.Contains(red, "RED-7") {
		t.Error("the 7th and 8th reds must be counted in +2 more, not listed")
	}
	if !strings.Contains(red, `<li class="alert-more alert-more-wide" data-alert-more="2"><a href="/trips#outstanding-alerts">+2 more`) {
		t.Errorf("red line lacks the wide \"+2 more\" link to the full list:\n%s", red)
	}
	if !strings.Contains(red, `<li class="alert-more alert-more-phone" data-alert-more="6"><a href="/trips#outstanding-alerts">+6 more`) {
		t.Errorf("red line lacks the phone \"+6 more\":\n%s", red)
	}
	if strings.Count(amber, `data-alert-item=`) != 3 || strings.Contains(amber, "alert-more-wide") ||
		!strings.Contains(amber, `data-alert-more="1"><a href="/trips#outstanding-alerts">+1 more`) {
		t.Errorf("amber line wants 3 items, no wide \"+N more\" and the phone's \"+1 more\":\n%s", amber)
	}
	if !strings.Contains(html, `data-alert-total="11" data-alert-overdue="8" data-alert-verify="3"`) {
		t.Errorf("the strip does not state the true totals:\n%s", html)
	}

	ro := fix13Render(t, roPage{Banner: rows, AlertsReadOnly: true})
	if strings.Contains(ro, "<a ") || strings.Contains(ro, "<form") {
		t.Errorf("read-only strip carries a link or a form:\n%s", ro)
	}
	if !strings.Contains(ro, `data-alert-more="2">+2 more<span class="sr-only"> overdue</span></li>`) {
		t.Errorf("read-only \"+2 more\" is not plain text:\n%s", ro)
	}
}

// TestFix13PeopleCountIsStillAboardAfterAnF15TurnBack — the bracketed count
// is the trip's StillAboard: Pat rides out with Dana, turns back and checks
// in at the origin, and the strip reads (1), the same as StillAboard.
func TestFix13PeopleCountIsStillAboardAfterAnF15TurnBack(t *testing.T) {
	srv, st, d, persons := newOriginServer(t)
	dana, pat := persons[0], persons[1]
	dest, mins := "site-a", int64(1)
	trip, err := domain.Dispatch(st, "site-b", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, st, "site-b", "TRK-F15"), DriverPersonID: dana.PersonID,
		OccupantPersonIDs: []string{pat.PersonID}, DestinationSite: &dest, DeadlineMinutes: &mins,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	envs, err := st.Events("site-b", 1, trip.Seq, 1)
	if err != nil || len(envs) != 1 {
		t.Fatal(err)
	}
	d.WallNow = func() time.Time { return time.UnixMilli(envs[0].AuthoredAt.WallMS).Add(2 * time.Minute) }
	if bar := topBarOf(t, getAs(t, srv, "/gate")); !strings.Contains(bar, `<code>TRK-F15</code> (2)`) {
		t.Fatalf("PRECONDITION: the strip reads TRK-F15 (2) before the turn-back:\n%s", bar)
	}
	f15CheckInAt(t, st, "site-b", pat.PersonID)
	c, err := domain.EffectiveTrip(st, trip.TripID)
	if err != nil {
		t.Fatal(err)
	}
	aboard, err := domain.StillAboard(st, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(aboard) != 1 {
		t.Fatalf("PRECONDITION: StillAboard = %d after Pat checked back in, want 1", len(aboard))
	}
	for _, path := range []string{"/gate", "/trips", "/analytics"} {
		bar := topBarOf(t, getAs(t, srv, path))
		want := fmt.Sprintf(`<code>TRK-F15</code> (%d)`, len(aboard))
		if !strings.Contains(bar, want) || strings.Contains(bar, `<code>TRK-F15</code> (2)`) {
			t.Errorf("%s: strip must read %s (StillAboard):\n%s", path, want, bar)
		}
		if !strings.Contains(bar, "TRK-F15, overdue, 1 person aboard") {
			t.Errorf("%s: the accessible name must say 1 person aboard:\n%s", path, bar)
		}
	}
}
