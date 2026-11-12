package web

// road_work_test.go — WORKING ON THE ROAD on every operator surface (James,
// 2026-10-03). Every assertion is on server-rendered markup and store state:
// the forms here are plain POSTs, so what these tests see is what a browser
// with scripting OFF submits; the JS-on path is the live Playwright run.

import (
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

type roadWorkWeb struct {
	srv   *Server
	st    *store.Store
	d     *deadline.Deadlines
	crew  []domain.Person
	bus   string
	limit int64
}

// newRoadWorkWeb builds THIS post (site-a, "Ice Road A") with the scanner
// wired, two people on site, and — when enabled — a [road_work] table with a
// twelve-hour limit spelled the demo's way.
func newRoadWorkWeb(t *testing.T, enabled bool) *roadWorkWeb {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	if err := deadline.Register(st); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		SiteID: "site-a", DisplayName: "Ice Road A", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 10,
		LoginPassword: testLoginPassword,
		Peers:         []config.Peer{{SiteID: "site-b", BaseURL: "http://example.invalid"}},
		Destinations:  []config.Destination{{ID: "site-b", DisplayName: "Pad Site Control", Controlled: true}},
		// ⟦was: Weather — a fixture [[weather.phase]] table (Phase 2, Phase 3)
		// for the Phase-3 road-work test; weather phases removed, James
		// 2026-10-04⟧
	}
	if enabled {
		cfg.RoadWork = &config.RoadWork{LimitMinutes: 720, DisplayName: "Working on the ice road"}
	}
	w := &roadWorkWeb{st: st, limit: 720}
	w.srv = New(cfg, st)
	d, err := deadline.New(st, "site-a", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	w.d = d
	w.srv.Alerts = d
	for _, n := range []string{"Blade, Ann", "Chains, Bo"} {
		p, err := domain.CreatePerson(st, "site-a", 1, n, nil, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		w.crew = append(w.crew, p)
	}
	w.bus = w7SeedVehicleID(t, st, "site-a", "BUS 1")
	checkIn(t, st, "site-a", w.crew[0].PersonID, []string{w.crew[1].PersonID})
	return w
}

func (w *roadWorkWeb) get(t *testing.T, path string, authed bool) string {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	if authed {
		req.AddCookie(loginCookie(t, w.srv, testLoginPassword))
	}
	rec := httptest.NewRecorder()
	w.srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("GET %s = %d: %s", path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// sendOut posts the console's road-work dispatch for the crew and returns the
// run's trip.
func (w *roadWorkWeb) sendOut(t *testing.T, extra url.Values) *domain.Trip {
	t.Helper()
	form := url.Values{
		"time_source": {"default"}, "vehicle": {"GRADER 7"},
		"driver_person_id": {w.crew[0].PersonID}, "occupant_person_ids": {w.crew[1].PersonID},
		"destination_id": {config.RoadWorkChoice},
	}
	for k, v := range extra {
		form[k] = v
	}
	rec := postForm(t, w.srv, "/trips/dispatch", form)
	if rec.Code != 303 {
		t.Fatalf("road-work dispatch = %d: %s", rec.Code, rec.Body.String())
	}
	trips, err := domain.Trips(w.st)
	if err != nil {
		t.Fatal(err)
	}
	for i := range trips {
		if rw, _ := domain.IsRoadWork(w.st, trips[i].TripID); rw {
			return &trips[i]
		}
	}
	t.Fatal("no road-work trip was authored")
	return nil
}

func TestTheConsoleOffersWorkingOnTheRoadOnlyWhereConfigDoesAndStampsTheLimit(t *testing.T) {
	off := newRoadWorkWeb(t, false)
	page := off.get(t, "/trips", false)
	if strings.Contains(page, "data-road-work-dispatch") || strings.Contains(page, `value="`+config.RoadWorkChoice+`"`) {
		t.Fatal("a post with no [road_work] table rendered the road-work button")
	}
	before, _ := off.st.Watermark("site-a", 1)
	rec := postForm(t, off.srv, "/trips/dispatch", url.Values{"time_source": {"default"}, "vehicle": {"GRADER 7"},
		"driver_person_id": {off.crew[0].PersonID}, "destination_id": {config.RoadWorkChoice}})
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "does not record equipment working on the road") {
		t.Fatalf("raw road-work POST at a post without the table = %d %q", rec.Code, rec.Body.String())
	}
	if after, _ := off.st.Watermark("site-a", 1); after != before {
		t.Fatal("the refusal authored something")
	}

	on := newRoadWorkWeb(t, true)
	page = on.get(t, "/trips", false)
	if !strings.Contains(page, "data-road-work-dispatch") || !strings.Contains(page, "Working on the ice road — no destination; overdue after 12 h") {
		t.Fatalf("the console does not offer the configured category:\n%s", page)
	}
	// A typed deadline that differs from the limit is refused, not silently
	// replaced — the limit is config.
	before, _ = on.st.Watermark("site-a", 1)
	rec = postForm(t, on.srv, "/trips/dispatch", url.Values{"time_source": {"default"}, "vehicle": {"GRADER 7"},
		"driver_person_id": {on.crew[0].PersonID}, "destination_id": {config.RoadWorkChoice}, "deadline_minutes": {"90"}})
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "fixed working limit of 12 h") {
		t.Fatalf("typed deadline 90 = %d %q, want the fixed-limit refusal", rec.Code, rec.Body.String())
	}
	if after, _ := on.st.Watermark("site-a", 1); after != before {
		t.Fatal("the refusal authored something")
	}
	trip := on.sendOut(t, nil)
	if trip.DeadlineMinutes == nil || *trip.DeadlineMinutes != 720 || trip.DestinationSite != nil {
		t.Fatalf("authored run = %+v — want the 720 limit and no destination", trip)
	}
}

func TestWorkingEquipmentIsListedAsWorkingNotOverdueOnEverySurfaceThenTurnsOverdueAcksAndChecksBackIn(t *testing.T) {
	w := newRoadWorkWeb(t, true)
	// --- the gate's LEAVING form offers the category. That form renders only
	// once a departure is in progress (the roster's tick-and-select door,
	// R-28), so it is reached that way, while the crew's visits are open.
	open, err := domain.OpenVisitsAt(w.st, "site-a")
	if err != nil || len(open) != 2 {
		t.Fatalf("precondition: 2 open visits at site-a, got %d, %v", len(open), err)
	}
	var visitIDs []string
	for _, v := range open {
		visitIDs = append(visitIDs, v.VisitID)
	}
	departing, _ := gateDepartSelected(t, w.srv, visitIDs)
	leaving := gateFormMarkup(t, departing, formIDFor(gateOutbound))
	if !strings.Contains(leaving, `value="`+config.RoadWorkChoice+`"`) || !strings.Contains(leaving, `data-road-work="true"`) ||
		!strings.Contains(leaving, "Working on the ice road — no destination; overdue after 12 h") {
		t.Fatalf("the gate's leaving form lacks the road-work option:\n%s", leaving)
	}
	trip := w.sendOut(t, nil)
	envs, err := w.st.Events("site-a", 1, trip.Seq, 1)
	if err != nil || len(envs) != 1 {
		t.Fatal(err)
	}
	authored := time.UnixMilli(envs[0].AuthoredAt.WallMS)
	w.d.WallNow = func() time.Time { return authored.Add(2 * time.Hour) }

	// --- console: its own section, WORKING, not on the inbound board, no banner
	page := w.get(t, "/trips", false)
	if !strings.Contains(page, `data-road-work="own"`) || !strings.Contains(page, "GRADER 7") || !strings.Contains(page, "Back from the road") {
		t.Fatalf("/trips lacks the own road-work row:\n%s", page)
	}
	if !strings.Contains(page, ">WORKING<") || !regexp.MustCompile(`working — 10h 0m left of the 12 h limit`).MatchString(page) {
		t.Fatalf("/trips does not read the run as WORKING with 10 h left:\n%s", page)
	}
	// Row markup only — the inlined stylesheet's attribute selectors also
	// spell `data-urgency="overdue"`, so the bare attribute is not evidence.
	// ⟦was: "EQUIPMENT OVERDUE" — Q-FH1, James 2026-10-04⟧
	for _, bad := range []string{`<article class="board-row" data-urgency="overdue"`, `class="board-urgency">OVERDUE<`, "on the road — OVERDUE"} {
		if strings.Contains(page, bad) {
			t.Fatalf("/trips calls working equipment overdue (%q present)", bad)
		}
	}
	if !strings.Contains(page, "No open inbound expectations.") {
		t.Fatalf("/trips lists the run as an inbound expectation (the empty-board sentence is missing):\n%s", page[strings.Index(page, "Inbound — open expectations"):][:600])
	}
	// --- gate vehicle page: the row below the inbound list
	gate := w.get(t, "/gate/vehicle", false)
	if !strings.Contains(gate, `data-road-work="own"`) || !strings.Contains(gate, "Back from the road — check in (2)") || !strings.Contains(gate, "working — 10h 0m left") {
		t.Fatalf("/gate/vehicle lacks the working row:\n%s", gate)
	}
	if strings.Contains(gate, `data-inbound-vehicle="GRADER 7"`) {
		t.Fatal("/gate/vehicle lists the grader as inbound")
	}
	// --- muster: its own section, named, counted apart; not in ON THE ROAD
	for _, path := range []string{"/muster", "/muster/print"} {
		m := w.get(t, path, false)
		if !strings.Contains(m, `data-muster-section="road-work"`) || !strings.Contains(m, "Blade, Ann") || !strings.Contains(m, "Chains, Bo") {
			t.Fatalf("%s lacks the road-work section with the crew:\n%s", path, m)
		}
		if !strings.Contains(m, `data-tally-count="road-work">2<`) || strings.Contains(m, `data-muster-section="in-transit"`) {
			t.Fatalf("%s: tally/section wrong — want 2 working on the road and no in-transit section", path)
		}
		if !strings.Contains(m, "Working on the ice road") || !strings.Contains(m, "GRADER 7") {
			t.Fatalf("%s does not name the category and the vehicle", path)
		}
	}
	// --- analytics: its own tile, apart from travel
	an := w.get(t, "/analytics", true)
	if !strings.Contains(an, `data-analytics-tile="road-work"`) || !strings.Contains(an, "1 <small>/ 1 / 1</small>") {
		t.Fatalf("/analytics lacks the road-work tile (out now / today / season = 1 / 1 / 1):\n%s", an)
	}
	if !strings.Contains(an, `data-analytics-road-work>1 / 1<`) {
		t.Fatalf("/analytics site card lacks the road-work line:\n%s", an)
	}
	if strings.Contains(an, `<div class="an-num">1 <small>/ 1</small></div>`) {
		t.Fatal("/analytics counted the road-work run as a dispatched TRIP")
	}

	// --- the limit passes: OVERDUE, banner row in the category's own words, ackable
	w.d.WallNow = func() time.Time { return authored.Add(12*time.Hour + 5*time.Minute) }
	page = w.get(t, "/trips", false)
	if !strings.Contains(page, `data-alert-road-work="true"`) || !strings.Contains(alertRowFlat(page), "<code>GRADER 7</code> on the road — OVERDUE") /* fix4 H: R6-1.8a spans */ || !strings.Contains(page, "past its\n  12 h working limit") && !strings.Contains(page, "12 h working limit") {
		t.Fatalf("/trips lacks the road-work overdue banner:\n%s", page)
	}
	if !strings.Contains(page, "OVERDUE — out 5m past the 12 h limit") {
		t.Fatalf("/trips row does not say how far past the limit:\n%s", page)
	}
	// ⟦was: "VEHICLE OVERDUE:" — Q-FH1 reworded the inbound red (James, 2026-10-04)⟧
	if strings.Contains(page, "inbound — OVERDUE") {
		t.Fatal("the banner words equipment as an inbound vehicle overdue")
	}
	rec := postForm(t, w.srv, "/alerts/ack", url.Values{"trip_id": {trip.TripID}})
	if rec.Code != 303 {
		t.Fatalf("ack = %d: %s", rec.Code, rec.Body.String())
	}
	page = w.get(t, "/trips", false)
	// ⟦was: "EQUIPMENT OVERDUE ON THE ROAD" — Q-FH1, James 2026-10-04⟧
	if strings.Contains(page, "on the road — OVERDUE") || !strings.Contains(page, "acknowledged by Ice Road A") {
		t.Fatalf("after the ack: banner still up, or no attribution on the row:\n%s", page)
	}

	// --- back from the road: the ordinary arrival door closes it; the crew is on site again
	rec = postForm(t, w.srv, "/trips/arrive", url.Values{"time_source": {"default"}, "trip_id": {trip.TripID}})
	if rec.Code != 303 {
		t.Fatalf("back from the road = %d: %s", rec.Code, rec.Body.String())
	}
	page = w.get(t, "/trips", false)
	if strings.Contains(page, `data-road-work="own"`) {
		t.Fatal("the run is still listed after checking back in")
	}
	open, err = domain.OpenVisitsAt(w.st, "site-a")
	if err != nil || len(open) != 2 {
		t.Fatalf("open visits at site-a = %d, %v — want the crew back", len(open), err)
	}
	if m := w.get(t, "/muster", false); strings.Contains(m, `data-muster-section="road-work"`) {
		t.Fatal("the muster still carries the road-work section after the check-in")
	}
}

func TestAnotherPostsWorkingEquipmentIsListedWithoutAClockAndCanBeCheckedInHere(t *testing.T) {
	w := newRoadWorkWeb(t, true)
	// site-b sent a grader out; the record replicated here (authored on
	// site-b's stream into this store, the injected-event pattern).
	limit := int64(720)
	foreign, err := domain.Dispatch(w.st, "site-b", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, w.st, "site-b", "GRADER 2"), DriverPersonID: w.crew[1].PersonID,
		RoadWork: true, DeadlineMinutes: &limit,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	page := w.get(t, "/trips", false)
	if !strings.Contains(page, `data-road-work="foreign"`) || !strings.Contains(page, "GRADER 2") || !strings.Contains(page, "Arrived here instead") {
		t.Fatalf("/trips lacks the other post's run with its arrived-here door:\n%s", page)
	}
	// FIX3 C (e01aee3): the owning post by its display name — no screen
	// shows a raw site id. ⟦was: "CLOCK AT site-b" / "held at site-b"⟧
	owner := w.srv.Cfg.SiteName("site-b")
	if !strings.Contains(page, "CLOCK AT "+owner) || !strings.Contains(page, "clock for this run is held at "+owner) {
		t.Fatalf("/trips invents a verdict for a run this post does not own:\n%s", page)
	}
	if !strings.Contains(page, `data-urgency="none" data-road-work="foreign"`) {
		t.Fatalf("the other post's run carries an urgency at a post that does not own its clock:\n%s", page)
	}
	if m := w.get(t, "/muster", false); strings.Contains(m, `data-muster-section="road-work"`) {
		t.Fatal("this post's muster lists the OTHER post's road work — the origin owns it")
	}
	rec := postForm(t, w.srv, "/trips/arrive", url.Values{"time_source": {"default"}, "trip_id": {foreign.TripID}})
	if rec.Code != 303 {
		t.Fatalf("arrived here instead = %d: %s", rec.Code, rec.Body.String())
	}
	if s, _ := domain.TripStateByID(w.st, foreign.TripID); s == nil || s.State != domain.TripStateArrived {
		t.Fatalf("trip state %+v, want arrived", s)
	}
}
