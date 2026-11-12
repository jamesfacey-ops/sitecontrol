// alerts_test.go — C7's web done-when: the persistent banner is sourced from
// a server-side DB scan and appears on every session; an explicit
// acknowledgment dismisses it and lands in the event log; the board columns
// come from the persisted scan when the scanner is wired.
package web

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// newAlertingServer builds site-b's box with the C7 scanner WIRED (the
// main.go composition), holding one partition-expired inbound expectation:
// an A-authored trip whose capture says the deadline passed while the link
// was down (anchor 3 min old against X = 2 min).
func newAlertingServer(t *testing.T) (*Server, *store.Store, *deadline.Deadlines, domain.Trip) {
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
	trip, err := domain.Dispatch(st, "site-a", 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "ICE TRUCK 7", DriverPersonID: "driver-a","⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "ICE TRUCK 7"), DriverPersonID: "driver-a",
		DestinationSite: &dest, DeadlineMinutes: &x,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	envs, err := st.Events("site-a", 1, trip.Seq, 1)
	if err != nil || len(envs) != 1 {
		t.Fatalf("fetch trip envelope: %v", err)
	}
	d.OnApplied(envs[0], time.Now().Add(-(2*time.Minute+30*time.Second)), true) // off the whole-minute rounding boundary
	return srv, st, d, trip
}

func get(t *testing.T, srv *Server, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	if rec.Code != 200 {
		t.Fatalf("GET %s = %d", path, rec.Code)
	}
	return rec.Body.String()
}

// dispatchLiveOverdueTrip adds a SECOND expectation on top of an
// already-wired alerting server and pushes it past its own deadline on the
// monotonic clock — deadline_test.go's
// TestHealthyCountdownExpiresIntoLiveOverdueAlertOnTheMonotonicClock pattern,
// reused here at the web layer. This is the genuine overdue_live/"VEHICLE
// OVERDUE" case (banner.html's notice-critical branch), as opposed to
// newAlertingServer's fixture trip, which is partition-expired/"VERIFY BY
// RADIO" (a notice-warn branch) — the two render through different template
// branches (F18 of the test-suite audit), so a test that only ever posed the
// review case left the live branch, and OverdueByText, at 0% coverage.
func dispatchLiveOverdueTrip(t *testing.T, st *store.Store, d *deadline.Deadlines, vehicle string) domain.Trip {
	t.Helper()
	dest := "site-b"
	x := int64(1) // 1 minute
	trip, err := domain.Dispatch(st, "site-a", 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: vehicle, DriverPersonID: "driver-live","⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", vehicle), DriverPersonID: "driver-live",
		DestinationSite: &dest, DeadlineMinutes: &x,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	envs, err := st.Events("site-a", 1, trip.Seq, 1)
	if err != nil || len(envs) != 1 {
		t.Fatalf("fetch trip envelope: %v", err)
	}
	// Receipt anchor leaves ~80ms of countdown remaining; real monotonic
	// time below runs it out, so this is a HEALTHY countdown expiring live —
	// never a partition-expired/no-capture review case.
	d.OnApplied(envs[0], time.Now().Add(-time.Minute+80*time.Millisecond), true)
	time.Sleep(150 * time.Millisecond)
	return trip
}

// dispatchCountdownTrip adds a THIRD kind of expectation to a wired
// alerting server: one whose deadline is still comfortably in the future,
// so the scan reports StatusCountdown and the board renders the "due" band.
//
// It exists for M-03. The two fixtures above between them produce only the
// two overdue bands; without a running countdown alongside them the board
// carries at most two urgencies, and "the words differ between states" is
// not a testable claim on a board that only ever shows one or two.
func dispatchCountdownTrip(t *testing.T, st *store.Store, d *deadline.Deadlines, vehicle string) domain.Trip {
	t.Helper()
	dest := "site-b"
	x := int64(600) // ten hours: long enough that no test runtime can expire it
	trip, err := domain.Dispatch(st, "site-a", 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: vehicle, DriverPersonID: "driver-countdown","⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", vehicle), DriverPersonID: "driver-countdown",
		DestinationSite: &dest, DeadlineMinutes: &x,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	envs, err := st.Events("site-a", 1, trip.Seq, 1)
	if err != nil || len(envs) != 1 {
		t.Fatalf("fetch trip envelope: %v", err)
	}
	// Received now, with the full ten hours on the clock: a live countdown.
	d.OnApplied(envs[0], time.Now(), true)
	return trip
}

// Every session surfaces the alert: fresh requests to every banner-bearing
// page — no cookies, no prior state, each rendering the alert surface from
// its own DB scan. (Killing a tab is a no-op by construction: the server
// holds no per-tab state to lose, which this proves by never creating any
// session in the first place.) Both banner classes are posed: the
// review-lane fixture (VERIFY BY RADIO) AND a genuine overdue_live trip
// (VEHICLE OVERDUE) — the live red branch an officer actually watches for.
// The board's countdown column is scan-sourced, not the C6 render-time
// stand-in.
//
// V-01 changed the SHAPE of the surface and this test was re-expressed for
// it rather than relaxed. The sticky strip now carries the worst single
// alert plus a persistent count of all of them; the full list lives in the
// operator console's body. So "the session surfaces the alert" is now
// asserted as three things instead of one, on MORE pages than before (six,
// not four — /trips/detail and /muster/print populate the surface too):
//
//  1. every page shows the WORST alert in full, in the bar;
//  2. every page states the TOTAL, so the alert that is not shown in full
//     is counted rather than hidden — this is the assertion that stops
//     "show one row" from degenerating into "show one alert";
//  3. /trips carries every row in full, with its own acknowledgment.
func TestBannerRendersOnEveryFreshSessionFromTheDBScan(t *testing.T) {
	srv, st, d, _ := newAlertingServer(t)
	dispatchLiveOverdueTrip(t, st, d, "LIVE OVERDUE TRUCK")

	// All six pages whose handler runs the scan.
	for _, path := range []string{"/status", "/trips", "/trips/detail", "/gate", "/muster", "/muster/print"} {
		body := get(t, srv, path)
		// 1. Both alerts, named in the bar: the live red on the OVERDUE
		// line and the review row on the VERIFY BY RADIO line, each as its
		// vehicle and people aboard. ⟦was (V-01): the worst alert in full —
		// "inbound — OVERDUE" — as the strip's one row. FIX13 (James,
		// 2026-10-05): the strip is a compact list of every vehicle⟧
		bar := topBarOf(t, body)
		lines := fix13Lines(t, bar)
		if !strings.Contains(lines["overdue"], "<code>LIVE OVERDUE TRUCK</code> (") {
			t.Fatalf("GET %s: the live overdue_live alert is not on the bar's OVERDUE line — it is the most urgent outstanding alert:\n%s", path, bar)
		}
		if !strings.Contains(lines["verify"], "<code>ICE TRUCK 7</code> (") {
			t.Fatalf("GET %s: the review-lane alert is not on the bar's VERIFY BY RADIO line:\n%s", path, bar)
		}
		// 2. And the total, stated. Two alerts are outstanding; the page
		// must say so even though it renders one of them in full.
		if !strings.Contains(body, `data-alert-total="2"`) {
			t.Fatalf("GET %s: the alert surface does not state how many alerts are outstanding. Showing one row is only honest while the count is visible — without it the operator has to infer the total from what fits on screen:\n%s", path, body)
		}
		if !strings.Contains(body, `data-alert-overdue="1"`) || !strings.Contains(body, `data-alert-verify="1"`) {
			t.Errorf("GET %s: the count does not break down by severity", path)
		}
	}

	// 3. The console carries the FULL list, each row with its own ack.
	board := get(t, srv, "/trips")
	if !strings.Contains(board, "VERIFY BY RADIO") || !strings.Contains(board, "ICE TRUCK 7") {
		t.Fatalf("GET /trips: the second outstanding alert is not listed anywhere in full — the strip summarises it, so the console must carry it:\n%s", board)
	}
	if got := strings.Count(board, `action="/alerts/ack"`); got < 2 {
		t.Fatalf("GET /trips: %d acknowledgment forms for 2 outstanding alerts — every alert must remain individually acknowledgeable", got)
	}
	if !strings.Contains(board, "overdue-unresolved — verify by radio") {
		t.Fatal("board countdown column is not sourced from the C7 scan")
	}
	if strings.Contains(board, "held ") {
		t.Fatal("board still renders the C6 render-time stand-in while the scanner is wired")
	}
}

func TestAcknowledgmentViaTheFormDismissesTheBannerAndIsLogged(t *testing.T) {
	srv, st, _, trip := newAlertingServer(t)
	wmBefore, err := st.Watermark("site-b", 1)
	if err != nil {
		t.Fatal(err)
	}

	rec := postForm(t, srv, "/alerts/ack", url.Values{"trip_id": {trip.TripID}})
	if rec.Code != 303 {
		t.Fatalf("ack = %d (%s), want 303", rec.Code, rec.Body.String())
	}
	envs, err := st.Events("site-b", 1, wmBefore+1, 0)
	if err != nil || len(envs) != 1 || !strings.Contains(string(envs[0].Payload), `"alert_acknowledged"`) {
		t.Fatalf("acknowledgment not in the event log: %v / %d", err, len(envs))
	}
	for _, path := range []string{"/status", "/trips"} {
		if body := get(t, srv, path); strings.Contains(body, "VERIFY BY RADIO") {
			t.Fatalf("GET %s: banner still present after logged acknowledgment", path)
		}
	}

	// A second acknowledgment of the same trip refuses and authors nothing.
	rec = postForm(t, srv, "/alerts/ack", url.Values{"trip_id": {trip.TripID}})
	if rec.Code != 409 {
		t.Fatalf("double-ack = %d, want 409", rec.Code)
	}
	if wm, _ := st.Watermark("site-b", 1); wm != wmBefore+1 {
		t.Fatalf("double-ack authored an event (watermark %d)", wm)
	}
}

// A healthy countdown cannot be pre-acknowledged: that would silence an
// alert that has not fired yet.
func TestAcknowledgingAHealthyCountdownIsRefused(t *testing.T) {
	srv, st, d, _ := newAlertingServer(t)
	dest := "site-b"
	x := int64(60)
	trip2, err := domain.Dispatch(st, "site-a", 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRUCK 2", DriverPersonID: "driver-a","⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRUCK 2"), DriverPersonID: "driver-a",
		DestinationSite: &dest, DeadlineMinutes: &x,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	envs, _ := st.Events("site-a", 1, trip2.Seq, 1)
	d.OnApplied(envs[0], time.Now(), true) // healthy: ~60m remaining

	wmBefore, _ := st.Watermark("site-b", 1)
	rec := postForm(t, srv, "/alerts/ack", url.Values{"trip_id": {trip2.TripID}})
	if rec.Code != 409 {
		t.Fatalf("ack of healthy countdown = %d, want 409", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "counting down") {
		t.Fatalf("refusal text = %q", rec.Body.String())
	}
	if wm, _ := st.Watermark("site-b", 1); wm != wmBefore {
		t.Fatal("refusal authored an event")
	}
}

// An arrival check-in (the ordinary board action) clears the banner without
// any acknowledgment — the trip closes and leaves the scan.
func TestArrivalCheckInClearsTheBannerThroughTheWebForm(t *testing.T) {
	srv, st, _, trip := newAlertingServer(t)
	// The arrival form builds visits from the roster, so this second
	// expectation's driver must be a real person here (the fixture trip's
	// "driver-a" is not, which is fine — it stays on the banner as the
	// control).
	p, err := domain.CreatePerson(st, "site-b", 1, "Roster Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	dest := "site-b"
	x := int64(2)
	trip2, err := domain.Dispatch(st, "site-a", 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "ICE TRUCK 8", DriverPersonID: p.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "ICE TRUCK 8"), DriverPersonID: p.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &x,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// No capture for trip2 → overdue-unresolved → the surface shows both
	// trips. Read on /trips, which carries the FULL list since V-01 (the
	// sticky strip shows the worst single row plus a count, so it is not
	// where "both trips are outstanding" can be read).
	before := get(t, srv, "/trips")
	for _, want := range []string{"ICE TRUCK 7", "ICE TRUCK 8"} {
		if !strings.Contains(before, want) {
			t.Fatalf("%s is not on the alert surface", want)
		}
	}
	// The count agrees with the list — this is the assertion that stops the
	// summary and its rows from drifting apart.
	if !strings.Contains(before, `data-alert-total="2"`) {
		t.Fatalf("the alert count does not say 2 with two expectations outstanding:\n%s", before)
	}

	rec := postForm(t, srv, "/trips/arrive", url.Values{"time_source": {"default"}, "trip_id": {trip2.TripID}})
	if rec.Code != 303 {
		t.Fatalf("arrival = %d (%s)", rec.Code, rec.Body.String())
	}
	body := get(t, srv, "/trips")
	if strings.Contains(body, "ICE TRUCK 8") {
		t.Fatal("the alert surface still shows the arrived trip")
	}
	if !strings.Contains(body, "ICE TRUCK 7") {
		t.Fatalf("the other (still-open) trip %s must remain on the alert surface", trip.TripID)
	}
	// And the count came down with it, on every page — an arrival that
	// cleared a row but left the total at 2 would be worse than no count.
	for _, path := range []string{"/status", "/trips", "/gate", "/muster"} {
		if p := get(t, srv, path); !strings.Contains(p, `data-alert-total="1"`) {
			t.Errorf("GET %s: the alert count did not come down when the trip arrived", path)
		}
	}
}
