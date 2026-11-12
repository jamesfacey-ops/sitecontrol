package web

// fix2_fd_road_work_seams_test.go — FINDINGS-R4 cluster D, "road-work seams
// with older consumers" (James, 2026-10-04: every reader that switches on
// trip kind needs a road-work arm). One test per reader that had none:
//
//   - D4:  /analytics/site/<origin> listed and counted nobody out working on
//          the road (InTransitFrom excludes road work; no section existed);
//   - L20: /analytics "overdue now" skipped overdue road-work runs while the
//          red bar above it named one;
//   - D27: a crew member entered at another post through the unsolicited
//          door stayed on the origin's road-work list with nothing saying so,
//          under a sentence promising the run leaves "when another post
//          records its arrival there";
//   - /trips/detail printed a road-work run's destination as "— (local)",
//          the degenerate check-in's word.

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// fdSection cuts body from the element carrying attr to the end of the
// enclosing block marker end (the next occurrence of end after attr).
func fdSection(t *testing.T, body, attr, end string) string {
	t.Helper()
	at := strings.Index(body, attr)
	if at < 0 {
		t.Fatalf("page has no %s:\n%s", attr, body)
	}
	sec := body[at:]
	if i := strings.Index(sec, end); i >= 0 {
		sec = sec[:i]
	}
	return sec
}

func TestFD_D4_SitePageListsAndCountsTheOriginsRoadWorkCrew(t *testing.T) {
	w := newRoadWorkWeb(t, true)
	contact := 1.0
	w.srv.SyncSnapshot = anHealthySnapshot("site-a", "site-b", &contact)
	trip := w.sendOut(t, nil)

	body := w.get(t, "/analytics/site/site-a", true)
	if !strings.Contains(body, `data-an-count="road-work">2<`) {
		t.Fatalf("site page road-work count is not 2 (D4):\n%s", body)
	}
	sec := fdSection(t, body, `data-an-runs="road-work"`, "</div>\n</section>")
	for _, p := range w.crew {
		if !strings.Contains(sec, p.Name) {
			t.Errorf("road-work section does not name %q (D4):\n%s", p.Name, sec)
		}
	}
	if !strings.Contains(sec, "GRADER 7") || !strings.Contains(sec, `data-an-trip="`+trip.TripID+`"`) {
		t.Errorf("road-work section does not carry the run GRADER 7 / %s:\n%s", trip.TripID, sec)
	}
	if !strings.Contains(sec, "working limit 720 minutes") {
		t.Errorf("road-work row does not say working limit:\n%s", sec)
	}
	// This post owns the run: the row carries the scan's own verdict.
	if !strings.Contains(sec, "DUE") {
		t.Errorf("own road-work row carries no scan verdict:\n%s", sec)
	}
}

func TestFD_D4_SitePageListsAnotherPostsRoadWorkWithoutCallingItOverdue(t *testing.T) {
	w := newRoadWorkWeb(t, true)
	contact := 1.0
	w.srv.SyncSnapshot = anHealthySnapshot("site-a", "site-b", &contact)
	limit := int64(1)
	if _, err := domain.Dispatch(w.st, "site-b", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, w.st, "site-b", "GRADER 2"), DriverPersonID: w.crew[1].PersonID,
		RoadWork: true, DeadlineMinutes: &limit,
	}, time.Now().UnixMilli(), "entered"); err != nil {
		t.Fatal(err)
	}
	// Past its limit from the RECORDED instant (fe's D13 rule, which holds
	// for road work too): the page reads 10 minutes after the recording.
	// ⟦was: occurred-at backdated 10 minutes with the recording at real now
	// — under D13 the limit counts from the recording, so the run read
	// WITHIN WORKING LIMIT and this fixture never reached the past-limit arm
	// (fd x fe, integ full suite 132732a)⟧
	later := time.Now().Add(10 * time.Minute).UnixMilli()
	w.st.Clock = func() store.ClockReading {
		return store.ClockReading{WallMS: later, MaxErrorUS: 500, State: store.ClockSynced}
	}
	body := w.get(t, "/analytics/site/site-b", true)
	if !strings.Contains(body, `data-an-count="road-work">1<`) {
		t.Fatalf("site-b's page does not count its road-work crew (D4):\n%s", body)
	}
	sec := fdSection(t, body, `data-an-runs="road-work"`, "</div>\n</section>")
	if !strings.Contains(sec, w.crew[1].Name) || !strings.Contains(sec, "GRADER 2") {
		t.Fatalf("site-b's road-work run is not listed:\n%s", sec)
	}
	if strings.Contains(sec, "OVERDUE") {
		t.Fatalf("a post that does not own the run called it OVERDUE:\n%s", sec)
	}
	// FIX3 F3d (FINDINGS-R5 L11-RAW-1; "no screen shows a raw site id"): the
	// owning post by its display name. ⟦was: "only site-b can call it overdue"⟧
	if !strings.Contains(sec, "PAST WORKING LIMIT") || !strings.Contains(sec, "only "+w.srv.Cfg.SiteName("site-b")+" can call it overdue") {
		t.Fatalf("past-limit foreign run lacks the verify-by-radio wording:\n%s", sec)
	}
}

func TestFD_OverdueNowCountsRoadWorkPastItsLimitAndSplitsItOut(t *testing.T) {
	w := newRoadWorkWeb(t, true)
	w.srv.Cfg.RoadWork.LimitMinutes = 1
	w.sendOut(t, nil)
	w.d.WallNow = func() time.Time { return time.Now().Add(3 * time.Minute) }

	body := w.get(t, "/analytics", true)
	page := styleRe.ReplaceAllString(body, "")
	m := regexp.MustCompile(`<div class="an-num">(\d+)</div>\s*<div class="an-label">overdue now</div>`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no overdue-now figure:\n%s", page)
	}
	if m[1] != "1" {
		t.Fatalf("overdue now = %s with one road-work run past its limit, want 1 (L20)", m[1])
	}
	if !strings.Contains(page, `data-analytics-overdue-split="0/0" data-analytics-overdue-road-work="1"`) {
		t.Fatalf("overdue split does not carry the road-work part:\n%s", page)
	}
	if !strings.Contains(page, "1 past the working limit</strong>") {
		t.Fatalf("overdue tile does not say which part is road work:\n%s", page)
	}
}

func TestFD_D27_OriginRoadWorkListFlagsACrewMemberEnteredElsewhere(t *testing.T) {
	w := newRoadWorkWeb(t, true)
	trip := w.sendOut(t, nil)
	// site-b's officer enters crew[0] through the UNSOLICITED door, naming
	// the grader: never a correlation for a road-work run, so the run
	// stays open here.
	if _, err := domain.RecordUnsolicitedArrival(w.st, "site-b", 1, w7SeedVehicleID(t, w.st, "site-b", "GRADER 7"),
		"site-a", []string{w.crew[0].PersonID}, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if s, _ := domain.TripStateByID(w.st, trip.TripID); s == nil || s.State != domain.TripStateOpen {
		t.Fatalf("precondition: the run must stay open (no correlation), got %+v", s)
	}
	body := w.get(t, "/muster", false)
	sec := fdSection(t, body, `data-muster-section="road-work"`, "</section>")
	li := regexp.MustCompile(`<li>`+regexp.QuoteMeta(w.crew[0].Name)+`[^\n]*?</li>`).FindString(sec)
	if li == "" {
		t.Fatalf("crew[0] is not listed on the road-work list (the listing stays, by M-6):\n%s", sec)
	}
	if !strings.Contains(li, `data-also-on-site-at="site-b"`) || !strings.Contains(li, "ALSO ON SITE AT Pad Site Control") {
		t.Fatalf("road-work row for a person on site at site-b carries no flag (D27): %s", li)
	}
	other := regexp.MustCompile(`<li>`+regexp.QuoteMeta(w.crew[1].Name)+`[^\n]*?</li>`).FindString(sec)
	if strings.Contains(other, "data-also-on-site-at") {
		t.Fatalf("crew[1] is flagged without being on site anywhere: %s", other)
	}
	if strings.Contains(sec, "records its arrival there") || !strings.Contains(sec, "Arrived here") {
		t.Fatalf("the closing sentence still promises a close the unsolicited door never performs:\n%s", sec)
	}
}

func TestFD_TripsDetailNamesRoadWorkNotLocal(t *testing.T) {
	w := newRoadWorkWeb(t, true)
	trip := w.sendOut(t, nil)
	body := w.get(t, "/trips/detail", false)
	at := strings.Index(body, trip.TripID)
	if at < 0 {
		t.Fatalf("/trips/detail does not list the run:\n%s", body)
	}
	row := body[at:]
	if i := strings.Index(row, "</tr>"); i >= 0 {
		row = row[:i]
	}
	if strings.Contains(row, "(local)") || !strings.Contains(row, domain.RoadWorkDestinationText) {
		t.Fatalf("road-work row on /trips/detail reads as a local check-in: %s", row)
	}
}
