package web

// w1_outbound_test.go — W1 (James, 2026-10-03), the web half:
//
//   NEW-a  the SENDING post sees its own overdue outbound truck — banner row
//          "VEHICLE OVERDUE — OUTBOUND", an OUTBOUND board on /trips, the
//          analytics tile and the live site page — and acknowledges it from
//          its own bar, which is the same logged event the destination's
//          bar authors.
//   T-005  overdue rows order by lateness, latest first.
//   A-27   the alert strip is a live region (role="alert"), no script needed.
//   ⟦was: T-042 (a phase press lands back on the status panel) and T-054
//          (a post with no [[weather.phase]] table has no phase controls) —
//          both tests deleted with weather phases, James 2026-10-04; the
//          no-phase-anywhere sweep is fix3_no_weather_test.go's⟧
//   /analytics refreshes itself every 30 s (meta refresh, scripting off).
//
// Failability: each test was run against the pre-change web package and
// failed on its first assertion (the origin's /trips had no outbound
// section, the bar had no role, the press redirected to "/", npf rendered
// three phase buttons, /analytics carried no meta refresh).

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// newOriginServer is newTripsTestServer (site-b) as an ORIGIN: a controlled
// road to site-a of James's 65 minutes, and the C7 scanner wired as main.go
// wires it. ⟦was: "with James's 65/65/240 table and the phase table", and the
// scanner's four weather-phase funcs — weather phases removed, James
// 2026-10-04⟧
func newOriginServer(t *testing.T) (*Server, *store.Store, *deadline.Deadlines, []domain.Person) {
	t.Helper()
	srv, st, persons := newTripsTestServer(t)
	srv.Cfg.Destinations = []config.Destination{{
		ID: "site-a", DisplayName: "Ice Pad", Controlled: true,
		TravelMinutes: 65,
	}}
	if err := deadline.Register(st); err != nil {
		t.Fatal(err)
	}
	d, err := deadline.New(st, "site-b", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	srv.Alerts = d
	return srv, st, d, persons
}

// sendOut dispatches one site-b -> site-a run of xMinutes with the given
// vehicle number and returns the trip and its envelope.
func sendOut(t *testing.T, st *store.Store, driver domain.Person, vehicle string, xMinutes int64) (domain.Trip, store.Envelope) {
	t.Helper()
	dest := "site-a"
	trip, err := domain.Dispatch(st, "site-b", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, st, "site-b", vehicle), DriverPersonID: driver.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &xMinutes,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if trip.OriginSite != "site-b" || trip.DestinationSite == nil || *trip.DestinationSite != "site-a" {
		t.Fatalf("precondition: trip is %s -> %v, want site-b -> site-a", trip.OriginSite, trip.DestinationSite)
	}
	envs, err := st.Events("site-b", 1, trip.Seq, 1)
	if err != nil || len(envs) != 1 {
		t.Fatalf("fetch envelope: %v (%d)", err, len(envs))
	}
	return trip, envs[0]
}

func getAs(t *testing.T, srv *Server, path string) string {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.AddCookie(loginCookie(t, srv, testLoginPassword))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("GET %s = %d:\n%s", path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// boardSection cuts the html between a heading and the next <h2>.
func boardSection(t *testing.T, body, heading string) string {
	t.Helper()
	i := strings.Index(body, heading)
	if i < 0 {
		t.Fatalf("heading %q missing from page:\n%s", heading, body)
	}
	rest := body[i+len(heading):]
	if j := strings.Index(rest, "<h2"); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

func TestTheSendingPostSeesAndAcknowledgesItsOwnOverdueOutboundTruck(t *testing.T) {
	srv, st, d, persons := newOriginServer(t)
	trip, env := sendOut(t, st, persons[0], "TRK-OUT-1", 1)
	authored := time.UnixMilli(env.AuthoredAt.WallMS)

	// Half a minute in: the OUTBOUND board lists it as due, nothing alerts.
	d.WallNow = func() time.Time { return authored.Add(30 * time.Second) }
	body := getAs(t, srv, "/trips")
	out := boardSection(t, body, "Outbound — on the road from")
	if !strings.Contains(out, "TRK-OUT-1") || !strings.Contains(out, "OUTBOUND — DUE IN") || !strings.Contains(out, `data-outbound="true"`) {
		t.Fatalf("outbound board must list the fresh run as OUTBOUND — DUE IN:\n%s", out)
	}
	if strings.Contains(boardSection(t, body, "Inbound — open expectations"), "TRK-OUT-1") {
		t.Fatal("the origin's own dispatch is on its INBOUND board")
	}
	// ⟦was: strings.Contains(body, "VEHICLE OVERDUE") — Q-FH1 reworded the red row (James, 2026-10-04); the row's own marker cannot drift with its words⟧
	if strings.Contains(body, `data-alert-row="overdue_live"`) {
		t.Fatalf("a due outbound run raised an alert:\n%s", topBarOf(t, body))
	}

	// Two minutes in: the bar carries the outbound red on EVERY page that
	// mounts the surface, worded as this post's own truck, naming the far
	// post; the board row reads OUTBOUND — OVERDUE.
	d.WallNow = func() time.Time { return authored.Add(2 * time.Minute) }
	for _, path := range []string{"/trips", "/gate", "/status", "/muster", "/analytics"} {
		body := getAs(t, srv, path)
		bar := topBarOf(t, body)
		// ⟦was: "VEHICLE OVERDUE — OUTBOUND:" — Q-FH1 (James, 2026-10-04): alert rows lead with the vehicle id and direction⟧
		// ⟦was: the bar's one full row — "<code>TRK-OUT-1</code> outbound —
		// OVERDUE", "to Ice Pad" and "Last heard from …" in the strip. FIX13
		// (James, 2026-10-05): the strip names the truck and its people; the
		// destination and its staleness are on the full row on /trips,
		// asserted below⟧
		if lines := fix13Lines(t, bar); !strings.Contains(lines["overdue"], "<code>TRK-OUT-1</code> (1)") {
			t.Fatalf("GET %s: the bar lacks the outbound red:\n%s", path, bar)
		}
		// A-27: the strip is a live region, in markup, with scripting off.
		if !regexp.MustCompile(`<div class="alert-strip"[^>]*role="alert"[^>]*aria-live="assertive"[^>]*aria-atomic="true"`).MatchString(bar) {
			t.Fatalf("GET %s: the alert strip is not a role=\"alert\" live region (A-27):\n%s", path, bar)
		}
	}
	body = getAs(t, srv, "/trips")
	full := fix13AlertList(t, body)
	if !strings.Contains(alertRowFlat(full), "<code>TRK-OUT-1</code> outbound — OVERDUE") || !strings.Contains(full, "to Ice Pad") && !strings.Contains(full, "to site-a") {
		t.Fatalf("/trips: the full alert row lacks the outbound red naming the destination:\n%s", full)
	}
	if !strings.Contains(full, "Last heard from "+srv.Cfg.SiteName("site-a")) {
		t.Fatalf("/trips: an outbound red must carry the DESTINATION's staleness on its face:\n%s", full)
	}
	out = boardSection(t, body, "Outbound — on the road from")
	if !strings.Contains(out, `data-urgency="overdue"`) || !strings.Contains(out, "OUTBOUND — OVERDUE") {
		t.Fatalf("outbound board must read OUTBOUND — OVERDUE:\n%s", out)
	}
	if strings.Contains(out, `action="/trips/arrive"`) {
		t.Fatal("the outbound board offers an arrival form — the arrival is recorded at the far end")
	}

	// Acknowledged from THIS post's bar — the same /alerts/ack the
	// destination's bar posts — the red leaves every page and the board row
	// says who acknowledged it, with the un-ack reachable.
	rec := postForm(t, srv, "/alerts/ack", url.Values{"trip_id": {trip.TripID}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("ack = %d: %s", rec.Code, rec.Body.String())
	}
	body = getAs(t, srv, "/trips")
	// ⟦was: "VEHICLE OVERDUE" — Q-FH1 reworded the red row (James, 2026-10-04)⟧
	if strings.Contains(topBarOf(t, body), `data-alert-item="overdue_live"`) { // ⟦was: data-alert-row — FIX13⟧
		t.Fatalf("the bar still carries the red after the origin's ack:\n%s", topBarOf(t, body))
	}
	out = boardSection(t, body, "Outbound — on the road from")
	if !strings.Contains(out, "acknowledged by") || !strings.Contains(out, `action="/alerts/unack"`) {
		t.Fatalf("the acked outbound row must name the acking post and offer the un-ack:\n%s", out)
	}
	// The logged event is trip-scoped on site-b's stream — what replicates to
	// site-a and clears its bar too (deadline/outbound_test.go proves the
	// two-database half).
	rows, err := st.DumpProjectionWhere("alert_ack", store.Eq{Column: "trip_id", Value: trip.TripID})
	if err != nil || !strings.Contains(string(rows), `"site_id":"site-b"`) {
		t.Fatalf("alert_ack rows for the trip = %s (%v), want one authored by site-b", rows, err)
	}
}

func TestOverdueRowsOrderByLatenessLatestFirst(t *testing.T) {
	srv, st, d, persons := newOriginServer(t)
	// Deterministic half, through the real row builder and the real
	// comparator: two overdue expectations whose TRIP IDS sort the WRONG way
	// round for lateness ("a…" is the less overdue one), so the pre-T-005
	// fallback — trip-id order within the overdue band — puts the wrong
	// truck first and this test goes red (MEASURED: a mutant that leaves
	// `remaining` at zero for overdue rows passes a page-level check by luck
	// one time in two, because dispatch mints random ids).
	dest := "site-a"
	mk := func(id string, overdueBy time.Duration) (domain.Expectation, deadline.Alert) {
		e := domain.Expectation{Trip: domain.Trip{TripID: id, OriginSite: "site-b", DestinationSite: &dest}, Vehicle: id}
		return e, deadline.Alert{TripID: id, Status: deadline.StatusOverdueLive, OverdueBy: overdueBy, Outbound: true, Destination: dest}
	}
	eLess, aLess := mk("a-less-overdue", 2*time.Minute)
	eMore, aMore := mk("b-more-overdue", 9*time.Minute)
	if !(eLess.Trip.TripID < eMore.Trip.TripID) {
		t.Fatal("precondition: the LESS overdue trip's id must sort first, or trip-id order would agree with lateness and prove nothing")
	}
	alertOf := map[string]deadline.Alert{aLess.TripID: aLess, aMore.TripID: aMore}
	rows := []boardRow{srv.boardRowFor(eLess, alertOf, time.Now(), true), srv.boardRowFor(eMore, alertOf, time.Now(), true)}
	sortBoard(rows)
	if rows[0].TripID != "b-more-overdue" {
		t.Fatalf("T-005: board order = %s, %s — the truck overdue longest must read first", rows[0].TripID, rows[1].TripID)
	}

	// The gate's inbound row carries the same key: an overdue row sorts
	// before a less-overdue one, both before a countdown — ids again chosen
	// so that id order contradicts lateness order.
	var inbound []inboundVehicleRow
	for _, a := range []deadline.Alert{
		{TripID: "a-due", Status: deadline.StatusCountdown, Remaining: time.Minute},
		{TripID: "b-late-2", Status: deadline.StatusOverdueLive, OverdueBy: 2 * time.Minute},
		{TripID: "c-late-9", Status: deadline.StatusOverdueLive, OverdueBy: 9 * time.Minute},
	} {
		row := inboundVehicleRow{TripID: a.TripID, Urgency: "none"}
		applyAlertFace(&row, a)
		inbound = append(inbound, row)
	}
	sortInboundRows(inbound)
	if inbound[0].TripID != "c-late-9" || inbound[1].TripID != "b-late-2" || inbound[2].TripID != "a-due" {
		t.Fatalf("gate inbound order = %s %s %s, want c-late-9 (9 min late), b-late-2 (2 min late), a-due", inbound[0].TripID, inbound[1].TripID, inbound[2].TripID)
	}

	// The rendered page, end to end — decisive only when the minted ids
	// happen to contradict lateness (checked, and said so when they do not).
	_, envLong := sendOut(t, st, persons[0], "TRK-LATE-5", 5)
	tripShort, envShort := sendOut(t, st, persons[1], "TRK-LATE-1", 1)
	d.WallNow = func() time.Time { return time.UnixMilli(envShort.AuthoredAt.WallMS).Add(10 * time.Minute) }
	board, err := d.Evaluate()
	if err != nil {
		t.Fatal(err)
	}
	var longID string
	for _, a := range board.Alerts {
		if a.Vehicle == "TRK-LATE-5" {
			longID = a.TripID
		}
		if a.Status != deadline.StatusOverdueLive {
			t.Fatalf("precondition: %s is %q, want overdue_live", a.Vehicle, a.Status)
		}
	}
	_ = envLong
	out := boardSection(t, getAs(t, srv, "/trips"), "Outbound — on the road from")
	i1, i5 := strings.Index(out, "TRK-LATE-1"), strings.Index(out, "TRK-LATE-5")
	if i1 < 0 || i5 < 0 {
		t.Fatalf("both trucks must be on the outbound board:\n%s", out)
	}
	if longID < tripShort.TripID {
		if i1 > i5 {
			t.Fatalf("T-005 on the page: TRK-LATE-1 (overdue 9 min) must read before TRK-LATE-5 (overdue 5 min):\n%s", out)
		}
	} else {
		t.Logf("minted ids already agree with lateness order this run; the page check is not discriminating (the unit halves above are)")
		if i1 > i5 {
			t.Fatalf("page order contradicts both keys:\n%s", out)
		}
	}
}

func TestAnalyticsRefreshesEverySixtySecondsAndCountsTheOutboundRed(t *testing.T) {
	srv, st, d, persons := newOriginServer(t)
	_, env := sendOut(t, st, persons[0], "TRK-AN-OUT", 1)
	d.WallNow = func() time.Time { return time.UnixMilli(env.AuthoredAt.WallMS).Add(2 * time.Minute) }
	body := getAs(t, srv, "/analytics")
	// James, 2026-10-03: 60 s ⟦was: 30 s, orchestrator default⟧.
	if !strings.Contains(body, `<meta http-equiv="refresh" content="60">`) || !strings.Contains(body, "refreshes every 60 s") {
		t.Fatalf("/analytics must refresh itself every 60 s and say so:\n%s", body[:min(len(body), 1500)])
	}
	if got := anTile(t, body, "overdue now"); got != "1" {
		t.Fatalf("overdue now = %s, want 1 — the origin's own outbound red counts", got)
	}
	if !strings.Contains(body, `data-analytics-overdue-split="0/1"`) || !strings.Contains(body, "0 inbound, 1 outbound") {
		t.Fatalf("the tile must say which half the red is:\n%s", body)
	}
	// And the live site page for THIS post judges its own outbound run with
	// the scan's verdict, never the weaker cross-clock PAST ALLOWANCE.
	body = getAs(t, srv, "/analytics/site/site-b")
	run := body[strings.Index(body, `data-an-runs="outbound"`):]
	run = run[:strings.Index(run, "</article>")]
	if !strings.Contains(run, "TRK-AN-OUT") || !strings.Contains(run, ">OVERDUE<") || strings.Contains(run, "PAST ALLOWANCE") {
		t.Fatalf("this post's own outbound run must carry the scan's OVERDUE verdict:\n%s", run)
	}
}

// TestAnalyticsSiteDetailShowsThisPostsOwnOutboundVerdict is the name
// analytics_site_test.go points at: the scanned case of a run FROM this post.
func TestAnalyticsSiteDetailShowsThisPostsOwnOutboundVerdict(t *testing.T) {
	srv, st, d, persons := newOriginServer(t)
	_, env := sendOut(t, st, persons[0], "TRK-SITE-OUT", 65)
	d.WallNow = func() time.Time { return time.UnixMilli(env.AuthoredAt.WallMS).Add(5 * time.Minute) }
	body := getAs(t, srv, "/analytics/site/site-b")
	run := body[strings.Index(body, `data-an-runs="outbound"`):]
	run = run[:strings.Index(run, "</article>")]
	if !strings.Contains(run, "TRK-SITE-OUT") || !strings.Contains(run, ">DUE<") || !strings.Contains(run, "remaining") || strings.Contains(run, "WITHIN ALLOWANCE") {
		t.Fatalf("this post's own outbound run must carry the scan's countdown, not the cross-clock estimate:\n%s", run)
	}
}

func countEvents(t *testing.T, st *store.Store) int64 {
	t.Helper()
	ms, err := st.Watermarks()
	if err != nil {
		t.Fatal(err)
	}
	var n int64
	for _, m := range ms {
		n += m.AppliedSeq
	}
	return n
}
