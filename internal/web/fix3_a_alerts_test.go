package web

// fix3_a_alerts_test.go — FIX3 stream A, the web half.
//
//	Q-FE2 (James, 2026-10-04): a no-receipt-timing row reads VERIFY BY RADIO
//	       — no receipt timing at this post, with "due about HH:MM by
//	       <origin display name>'s clock", never OVERDUE; then turns red.
//	F4     a red board row whose only standing ack is an amber ack offers no
//	       un-ack and says "acknowledged at … as VERIFY only — this red is
//	       not acknowledged"; an amber row with its amber ack keeps the un-ack.
//	Q-FH1  the strip's row leads with the vehicle id and direction.
//	L09-03 the strip orders reds first, then ambers by soonest recorded due.
//
// On e3d507d these fail: the label read "OVERDUE — UNRESOLVED" with no due
// time, the board offered "Un-acknowledge" on the amber-acked red (A2), the
// strip opened "VERIFY BY RADIO:" / "VEHICLE OVERDUE:", and the 65-min run
// ("A-65") sorted ahead of the 1-min one by vehicle name.

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
)

// noAnchorWeb: a site-a → site-b run of X minutes, captured at site-b with no
// sync anchor (the destination had just restarted). Returns the server, the
// scanner, the trip and its recorded due time.
func noAnchorWeb(t *testing.T, vehicle string, x int64) (*Server, *deadline.Deadlines, domain.Trip, time.Time) {
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
	trip, err := domain.Dispatch(st, "site-a", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, st, "site-a", vehicle), DriverPersonID: "driver-a",
		DestinationSite: &dest, DeadlineMinutes: &x,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	envs, err := st.Events("site-a", 1, trip.Seq, 1)
	if err != nil || len(envs) != 1 {
		t.Fatalf("fetch trip envelope: %v", err)
	}
	d.OnApplied(envs[0], time.Time{}, false)
	due := time.UnixMilli(envs[0].AuthoredAt.WallMS).Add(time.Duration(x) * time.Minute)
	return srv, d, trip, due
}

func inboundBoardRow(t *testing.T, body, vehicle string) string {
	t.Helper()
	sec := boardSection(t, body, "Inbound — open expectations")
	for _, part := range strings.Split(sec, `<article class="board-row"`)[1:] {
		if strings.Contains(part, vehicle) {
			return part
		}
	}
	t.Fatalf("no inbound board row for %s:\n%s", vehicle, sec)
	return ""
}

func TestQFE2TheNoReceiptTimingRowIsVerifyByRadioWithTheDueTimeThenRed(t *testing.T) {
	srv, d, _, due := noAnchorWeb(t, "TRK-19", 65)
	origin := srv.Cfg.SiteName("site-a")
	dueText := "due about " + due.In(time.Local).Format("15:04") + " by " + origin + "'s clock"
	// On the board the clause is data, so html/template escapes the apostrophe.
	dueHTML := strings.ReplaceAll(dueText, "'", "&#39;")

	d.WallNow = func() time.Time { return due.Add(-60 * time.Minute) }
	body := get(t, srv, "/trips")
	row := inboundBoardRow(t, body, "TRK-19")
	if !strings.Contains(row, "VERIFY BY RADIO — no receipt timing at this post") || !strings.Contains(row, dueHTML) {
		t.Fatalf("board row lacks the Q-FE2 label or the due time %q:\n%s", dueText, row)
	}
	if strings.Contains(row, "OVERDUE") {
		t.Fatalf("the amber board row says OVERDUE:\n%s", row)
	}
	// FIX13 (James, 2026-10-05): the strip names the vehicle on its amber
	// line; the vehicle-first VERIFY row with the due time is the full row
	// under "Outstanding alerts", which the item links to. ⟦was: both
	// asserted on the strip's one row⟧
	bar := topBarOf(t, body)
	if lines := fix13Lines(t, bar); !strings.Contains(lines["verify"], "<code>TRK-19</code> (") || lines["overdue"] != "" {
		t.Fatalf("strip lacks TRK-19 on the VERIFY BY RADIO line (and only there):\n%s", bar)
	}
	full := fix13AlertList(t, body)
	// In the alert row the apostrophe is template text, so it is not escaped.
	if !strings.Contains(alertRowFlat(full), "<code>TRK-19</code> inbound — VERIFY BY RADIO — no receipt timing at this post") || !strings.Contains(full, dueText) {
		t.Fatalf("the full alert row lacks the vehicle-first VERIFY text with the due time:\n%s", full)
	}
	if strings.Contains(bar, "OVERDUE") || strings.Contains(full, "OVERDUE") {
		t.Fatalf("the amber alert says OVERDUE:\n%s\n%s", bar, full)
	}

	d.WallNow = func() time.Time { return due.Add(3 * time.Minute) }
	body = get(t, srv, "/trips")
	bar = topBarOf(t, body)
	full = fix13AlertList(t, body)
	if lines := fix13Lines(t, bar); !strings.Contains(lines["overdue"], "<code>TRK-19</code> (") ||
		!strings.Contains(full, `data-alert-row="overdue_live"`) || !strings.Contains(alertRowFlat(full), "<code>TRK-19</code> inbound — OVERDUE 3m0s") {
		t.Fatalf("past the recorded due time the alert is not the live red:\n%s\n%s", bar, full)
	}
	if row := inboundBoardRow(t, body, "TRK-19"); !strings.Contains(row, `data-urgency="overdue"`) {
		t.Fatalf("board row is not red past the due time:\n%s", row)
	}
}

func TestF4TheUnackControlFollowsWhetherTheAckSilencesTheRow(t *testing.T) {
	srv, d, trip, due := noAnchorWeb(t, "TRK-19", 65)

	// The amber row, acked as amber (the far end's press, or this post's):
	// the ack silences it, so the un-ack is offered.
	d.WallNow = func() time.Time { return due.Add(-10 * time.Minute) }
	rec := postForm(t, srv, "/alerts/ack", url.Values{"trip_id": {trip.TripID}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("amber ack = %d: %s", rec.Code, rec.Body.String())
	}
	row := inboundBoardRow(t, get(t, srv, "/trips"), "TRK-19")
	if !strings.Contains(row, `action="/alerts/unack"`) || strings.Contains(row, "data-verify-only-ack") {
		t.Fatalf("amber row with its amber ack: want the un-ack and no VERIFY-only face:\n%s", row)
	}

	// The due time passes: the row is red, the amber ack does not silence
	// it, and the board must not offer "put this alert back".
	d.WallNow = func() time.Time { return due.Add(time.Minute) }
	row = inboundBoardRow(t, get(t, srv, "/trips"), "TRK-19")
	if strings.Contains(row, `action="/alerts/unack"`) {
		t.Fatalf("A2/F4: the red row whose only ack is an amber one offers Un-acknowledge:\n%s", row)
	}
	want := regexp.MustCompile(`acknowledged at [^<]+ as VERIFY only — this red is not acknowledged`)
	if !want.MatchString(row) {
		t.Fatalf("the red row does not say the standing ack is VERIFY only:\n%s", row)
	}
}

func TestQFH1TheStripLeadsWithTheVehicleAndDirection(t *testing.T) {
	srv, d, _, due := noAnchorWeb(t, "TRK-19", 65)
	d.WallNow = func() time.Time { return due.Add(3 * time.Minute) }
	body := get(t, srv, "/trips")
	// FIX13 (James, 2026-10-05): the strip's item is the vehicle first, then
	// its people aboard; the full row (under "Outstanding alerts") still
	// leads with the vehicle and direction. ⟦was: the strip's one row⟧
	bar := topBarOf(t, body)
	item := regexp.MustCompile(`(?s)data-alert-item="overdue_live"[^>]*><a [^>]*><span aria-hidden="true">(.*?)</span>`).FindStringSubmatch(bar)
	if item == nil || !strings.HasPrefix(item[1], "<code>TRK-19</code> (") {
		t.Fatalf("no red item leading with the vehicle in the strip:\n%s", bar)
	}
	strong := regexp.MustCompile(`(?s)data-alert-row="overdue_live"[^>]*>\s*<strong>(.*?)</strong>`).FindStringSubmatch(fix13AlertList(t, body))
	if strong == nil {
		t.Fatalf("no red row under Outstanding alerts:\n%s", body)
	}
	text := regexp.MustCompile(`<[^>]+>`).ReplaceAllString(strong[1], "")
	if !strings.HasPrefix(text, "TRK-19 inbound — OVERDUE") {
		t.Fatalf("alert row text %q — want it to lead \"TRK-19 inbound — OVERDUE\"", text)
	}
}

// L09-03: a 1-minute and a 65-minute run, both received with no sync anchor.
// The vehicle names are chosen so name order is the WRONG order.
func TestQFE2TheStripOrdersRedFirstThenAmbersBySoonestRecordedDue(t *testing.T) {
	now := time.Now()
	short := deadline.Alert{TripID: "t1", Vehicle: "B-1MIN", Status: deadline.StatusOverdueUnresolved, DueAt: now.Add(30 * time.Second)}
	long := deadline.Alert{TripID: "t65", Vehicle: "A-65MIN", Status: deadline.StatusOverdueUnresolved, DueAt: now.Add(64 * time.Minute)}
	unknown := deadline.Alert{TripID: "tx", Vehicle: "0-NODUE", Status: deadline.StatusOverdueUnresolved}
	rows := []deadline.Alert{unknown, long, short}
	sortAlertsWorstFirst(rows)
	if rows[0].TripID != "t1" || rows[1].TripID != "t65" || rows[2].TripID != "tx" {
		t.Fatalf("before the 1-min due: order %s, %s, %s — want the 1-min run, the 65-min run, then the one with no due time", rows[0].Vehicle, rows[1].Vehicle, rows[2].Vehicle)
	}
	short.Status, short.OverdueBy = deadline.StatusOverdueLive, 5*time.Second
	rows = []deadline.Alert{long, unknown, short}
	sortAlertsWorstFirst(rows)
	if rows[0].TripID != "t1" {
		t.Fatalf("after the 1-min due: %s leads — want the red", rows[0].Vehicle)
	}
}
