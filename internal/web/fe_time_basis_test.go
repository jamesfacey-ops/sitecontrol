package web

// fe_time_basis_test.go — FINDINGS-R4 cluster E (time basis and clocks) on
// the web surfaces: D13 (one time basis on the third-post site pages), D15
// (the overdue tile never calls a run it cannot count "past deadline"), D16
// (a peer event stamped ahead of this post's clock shows the skew, not
// "0 seconds ago") and R9's face (a clock-changed row never reads OVERDUE).
// D13, D15 and D16 FAIL on 2ba9188 on their first assertion; the R9 face
// test needs Deadlines.Uptime, which 2ba9188 does not have.

import (
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// D13, as overruled by James 2026-10-06: "from the typed time". A dispatch
// entered with an earlier time ("this left at 23:54") is counted by both
// ends AND the third-post site page from the typed time
// (domain.EffectiveDeparture), so all three agree it is past its allowance.
// ⟦was: FINDINGS-R4 D13 — counted from the RECORDING, WITHIN ALLOWANCE here,
// with "recorded HH:MM" beside the entered time⟧
func TestD13SitePageJudgesABackdatedDispatchFromItsTypedTime(t *testing.T) {
	w := anRoadWorld(t) // this post runs no scanner: the third-post branch
	dest := "site-b"
	x := int64(90)
	entered := time.Now().Add(-2 * time.Hour).UnixMilli()
	if _, err := domain.Dispatch(w.st, "site-a", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, w.st, "site-a", "TRK-BACKDATED"), DriverPersonID: w.staying[0].PersonID,
		DestinationSite: &dest, DeadlineMinutes: &x,
	}, entered, "entered"); err != nil {
		t.Fatal(err)
	}
	_, body := anSiteGet(t, w.srv, "GET", "/analytics/site/site-a", true)
	var run string
	for _, part := range strings.Split(body, "<article")[1:] {
		if strings.Contains(part, "TRK-BACKDATED") {
			run = part[:strings.Index(part, "</article>")]
		}
	}
	if run == "" {
		t.Fatalf("no run for TRK-BACKDATED on the site page:\n%s", body)
	}
	if !strings.Contains(run, "PAST ALLOWANCE") || !strings.Contains(run, "by 30 minutes (") || strings.Contains(run, "WITHIN ALLOWANCE") {
		t.Fatalf("a 90-minute run typed out 2 h ago must read PAST ALLOWANCE by 30 minutes — every judge counts from the typed time:\n%s", run)
	}
	if strings.Contains(run, "&#39;s clock), recorded ") || strings.Contains(run, ", recorded ") {
		t.Fatalf("the basis IS the typed dispatch time, so no separate recording time is shown:\n%s", run)
	}
}

// D13's wording half: "by 25 minutes ago" — an age where a magnitude belongs.
func TestD13PastAllowanceSaysByAMagnitudeNotAnAge(t *testing.T) {
	w := anRoadWorld(t)
	later := time.Now().Add(2 * time.Hour).UnixMilli()
	w.st.Clock = func() store.ClockReading {
		return store.ClockReading{WallMS: later, MaxErrorUS: 500, State: store.ClockSynced}
	}
	_, body := anSiteGet(t, w.srv, "GET", "/analytics/site/site-a", true)
	run := body[strings.Index(body, `data-an-runs="outbound"`):]
	run = run[:strings.Index(run, "</article>")]
	if !strings.Contains(run, "PAST ALLOWANCE") || !strings.Contains(run, "by 30 minutes (") || strings.Contains(run, "minutes ago") {
		t.Fatalf("past allowance must read \"by 30 minutes\", never \"by 30 minutes ago\":\n%s", run)
	}
}

// D15 — the overdue tile said "(past deadline, this post cannot say by how
// much)" for every review-lane run, including ones whose deadline this post
// simply cannot count (no sync anchor at receipt, replay, a changed clock).
func TestD15OverdueTileNeverCallsAnUncountableRunPastDeadline(t *testing.T) {
	srv, st, d, persons := newOriginServer(t)
	sendOut(t, st, persons[0], "TRK-REPLAY", 65)
	d.SetReplaying(true) // every open run: review lane, deadline NOT known passed
	body := getAs(t, srv, "/analytics")
	tile := body[strings.Index(body, `data-analytics-overdue-split=`):]
	tile = tile[:strings.Index(tile, "</div>")]
	if !strings.Contains(tile, "+ 1 to verify by radio") {
		t.Fatalf("precondition: the replaying run must be counted as verify-by-radio:\n%s", tile)
	}
	if strings.Contains(tile, "past deadline") {
		t.Fatalf("a run inside its allowance that this post cannot count was labelled past deadline:\n%s", tile)
	}
	if !strings.Contains(tile, "1 this post cannot count") || !strings.Contains(tile, "not known to be late") {
		t.Fatalf("the tile must say this post cannot count it, not known to be late:\n%s", tile)
	}
}

// D16 — a peer's latest event stamped AHEAD of this post's clock rendered
// "(0 seconds ago, by that post's clock)": humantime.Ago clamps negatives,
// so a 2 h skew was invisible.
func TestD16PeerEventAheadOfThisClockShowsTheSkew(t *testing.T) {
	w := anRoadWorld(t)
	if _, err := domain.CreatePerson(w.st, "site-b", 1, "Ahead, Ada", nil, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	behind := time.Now().Add(-2 * time.Hour).UnixMilli() // this post's clock, 2 h behind
	w.st.Clock = func() store.ClockReading {
		return store.ClockReading{WallMS: behind, MaxErrorUS: 500, State: store.ClockSynced}
	}
	_, body := anSiteGet(t, w.srv, "GET", "/analytics", true)
	card := anSiteCard(t, body, "site-b")
	if strings.Contains(card, "0 seconds ago") {
		t.Fatalf("a peer event 2 h ahead of this clock rendered as \"0 seconds ago\" — the skew is invisible:\n%s", card)
	}
	if !strings.Contains(card, "AHEAD of this post") || !strings.Contains(card, "1 hour 59 minutes") {
		t.Fatalf("the card must say the peer's event is ahead of this post's clock, and by how much:\n%s", card)
	}
}

// R9's face (James, 2026-10-04): a trip whose bridge was refused because
// this box's clock stepped reads "CLOCK CHANGED — VERIFY BY RADIO" on the
// trips board, never OVERDUE.
func TestR9ClockChangedRowNeverReadsOverdueOnTheTripsBoard(t *testing.T) {
	srv, st, d, persons := newOriginServer(t)
	_, env := sendOut(t, st, persons[0], "TRK-STEP", 65)
	wall := time.UnixMilli(env.AuthoredAt.WallMS).Add(10 * time.Second)
	up := 3 * time.Hour
	d.WallNow = func() time.Time { return wall }
	d.Uptime = func() (string, time.Duration, bool) { return "boot-1", up, true }
	if body := getAs(t, srv, "/trips"); !strings.Contains(body, "DUE IN") {
		t.Fatalf("precondition: the outbound run must be counting down first")
	}
	wall = wall.Add(2 * time.Hour) // the step: the wall moves, the boot clock does not
	up += 5 * time.Second
	body := getAs(t, srv, "/trips")
	at := strings.Index(body, "TRK-STEP")
	if at < 0 {
		t.Fatal("TRK-STEP is not on the trips board")
	}
	if !strings.Contains(body, "CLOCK CHANGED — VERIFY BY RADIO") || !strings.Contains(body, "clock changed since receipt — verify by radio") {
		t.Fatalf("a clock-stepped run must read CLOCK CHANGED — VERIFY BY RADIO:\n%s", body)
	}
	if strings.Contains(body, "OVERDUE —") || strings.Contains(body, ">OVERDUE<") {
		t.Fatalf("a clock step must never render as OVERDUE:\n%s", body)
	}
}
