// trips_test.go — C6's web done-when: the inbound board lists an injected
// open expectation with countdown and staleness columns; arrival visibly
// creates N visits with snapshots; cancel leaves occupants checked out.
package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// newTripsTestServer is site-b's box: registered domain projections, a small
// site-b-authored roster, and site-a configured as a peer.
func newTripsTestServer(t *testing.T) (*Server, *store.Store, []domain.Person) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	co, err := domain.CreateCompany(st, "site-b", 1, "Kakivik Asset Management", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ DR-W8-2 arity: the company argument is dropped — a trade is a site-wide open set, not scoped to a company. ⟦was: "wg, err := domain.CreateTrade(st, "site-b", 1, co.CompanyID, "Inspection", 0, "default")"⟧
	wg, err := domain.CreateTrade(st, "site-b", 1, "Inspection", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	var persons []domain.Person
	for _, name := range []string{"Dana Driver", "Pat Passenger"} {
		p, err := domain.CreatePerson(st, "site-b", 1, name, &co.CompanyID, &wg.TradeID, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		persons = append(persons, p)
	}
	cfg := &config.Config{
		SiteID: "site-b", DisplayName: "Ice Camp B", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 10,
		Peers:         []config.Peer{{SiteID: "site-a", BaseURL: "http://example.invalid"}},
		LoginPassword: testLoginPassword,
	}
	return New(cfg, st), st, persons
}

// testLoginPassword is the shared demo credential every web-layer test
// server is configured with (C11: config.Config.LoginPassword is now
// required — see config.go's validate()). Real per-site values live in
// deploy/site-*.toml; this is deliberately not one of them.
const testLoginPassword = "test-suite-password"

// loginCookie performs a REAL login round trip through srv.Handler() (not a
// backdoor into the session store) and returns the resulting session
// cookie — C11's gate must be exercised the same way a browser would hit
// it, per this project's own convention of testing through srv.Handler().
func loginCookie(t *testing.T, srv *Server, password string) *http.Cookie {
	t.Helper()
	form := url.Values{"password": {password}, "next": {"/"}}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			return c
		}
	}
	t.Fatalf("login with password %q did not set a session cookie; status=%d body=%s", password, rec.Code, rec.Body.String())
	return nil
}

// postForm logs in and submits form to path — requireLogin gates every
// mutating route this helper is used against, so the login round trip is
// folded in here rather than touching the ~30 call sites across
// gate_test.go/trips_test.go/alerts_test.go/alerts_phase_test.go/
// devroutes_test.go/trips_dispatch_validation_test.go/trips_return_test.go/
// trips_uncontrolled_test.go/weather_test.go that only care about the
// ACTION succeeding, not about session identity. D-5 removed the second
// round trip this used to make (a POST /officer tap): there is no picker,
// so a logged-in session can author on its first submission.
func postForm(t *testing.T, srv *Server, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	cookie := loginCookie(t, srv, testLoginPassword)
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func getTrips(t *testing.T, srv *Server) string {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/trips", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /trips = %d: %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// getTripsDetail fetches the reference/audit half of the old /trips (U2):
// all trips known here, the out-at-pad panel and the visits table. Same
// rows, same routes — a separate page so the operator console is not a
// scroll past six reference tables with a driver at the window.
func getTripsDetail(t *testing.T, srv *Server) string {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/trips/detail", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /trips/detail = %d: %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// injectForeignTrip applies a site-a-authored trip event destined for site-b
// — the injected open expectation of the board done-when.
func injectForeignTrip(t *testing.T, st *store.Store, seq int64, vehicle string, deadlineMin int64, driver, passenger string) string {
	t.Helper()
	dest := "site-b"
	tripID := "site-a-trip-injected-" + vehicle
	payload, err := json.Marshal(domain.TripDispatched{
		Type: "trip_dispatched", TripID: tripID,
		// ⟦LOCK — W7 seam repair, 2026-09-03⟧ ⟦was: "VehicleEntered: vehicle,"⟧
		VehicleEntered: w9aStr(vehicle), DriverPersonID: driver,
		OccupantPersonIDs: []string{passenger},
		DestinationSite:   &dest, DeadlineMinutes: &deadlineMin,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Apply(store.Envelope{
		SiteID: "site-a", NodeEpoch: 1, Seq: seq,
		SchemaVersion: store.SchemaVersion, OriginAuth: "site-a", Payload: payload,
	}); err != nil {
		t.Fatal(err)
	}
	return tripID
}

func TestInboundBoardListsInjectedOpenExpectationWithCountdownAndStaleness(t *testing.T) {
	srv, st, persons := newTripsTestServer(t)
	injectForeignTrip(t, st, 1, "TRUCK 12", 60, persons[0].PersonID, persons[1].PersonID)

	body := getTrips(t, srv)
	for _, want := range []string{
		"TRUCK 12",    // verbatim entered form on the board
		"Countdown",   // the column exists
		"Staleness",   // the column exists
		"remaining",   // a live countdown value rendered from the deadline
		"held ",       // a staleness value rendered from the first-seen anchor
		"Dana Driver", // driver resolved to a name
		"site-a",      // origin
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("inbound board is missing %q\n%s", want, body)
		}
	}
}

func TestArrivalFromBoardVisiblyCreatesOneSnapshotVisitPerOccupant(t *testing.T) {
	srv, st, persons := newTripsTestServer(t)
	tripID := injectForeignTrip(t, st, 1, "TRUCK 12", 60, persons[0].PersonID, persons[1].PersonID)

	rec := postForm(t, srv, "/trips/arrive", url.Values{"time_source": {"default"}, "trip_id": {tripID}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("arrive = %d: %s", rec.Code, rec.Body.String())
	}
	visits, err := domain.Visits(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(visits) != 2 {
		t.Fatalf("arrival created %d visit(s), want 2 (one per occupant, driver included)", len(visits))
	}
	for _, v := range visits {
		if v.IsOpen != 1 || v.CompanySnapshotName == nil || *v.CompanySnapshotName != "Kakivik Asset Management" {
			t.Fatalf("visit %+v lacks its point-in-time snapshot or is not open", v)
		}
	}
	// U2: the visits table and the trip-state column are the audit surface
	// and now live on /trips/detail. The assertion is unchanged — the same
	// five facts must be visible to an operator — it just names the page
	// that renders them. Nothing was weakened: this still fails if an
	// arrival stops producing a named, company-snapshotted, open visit.
	body := getTripsDetail(t, srv)
	for _, want := range []string{"Dana Driver", "Pat Passenger", "Kakivik Asset Management", "on site", "arrived"} {
		if !strings.Contains(body, want) {
			t.Fatalf("trips detail page does not visibly show the arrival: missing %q", want)
		}
	}
	exps, err := domain.OpenExpectations(st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(exps) != 0 {
		t.Fatalf("expectation still open after arrival: %+v", exps)
	}
}

// The demo-steps-2/4/5 loop, driven entirely through the forms: local
// check-in (degenerate trip + arrival), dispatch away (verbatim vehicle,
// occupants checked out), cancel (occupants STAY checked out).
//
// D-4 CHANGED HOW THE CHECK-IN HALF IS DRIVEN, NOT WHAT IS ASSERTED. This
// test used to reach the degenerate trip through POST /trips/dispatch with
// an empty destination — the console's "Local check-out only (no
// destination)" button, which D-4 removed. The check-in it stands in for is
// the gate's, so it is driven through POST /gate/checkin now: still a form,
// still the same degenerate trip + arrival underneath (domain.GateCheckIn),
// and every assertion below is unchanged. The removed path is asserted
// REFUSED first, so this test also fails if the console starts authoring
// destination-less trips again.
// TestARefusedPostAuthorsNothing — ⟦LOCK — §93, 2026-09-03⟧ ⟦WIDENED — §104, 2026-09-03⟧
// ⟦was: "TestARefusedDispatchPostAuthorsNothing"⟧ — the name loses "Dispatch" because the
// guarantee is not one door's. It is the same standing invariant, asserted across every door that
// co-mints a vehicle record.
// ⭐ ⟦LOCK — §137, 2026-09-03⟧ THE RETIRED NAME IS RESTORED TO THE FILE. §132 had to relocate it to
// the report, because the citations guard failed any comment naming a test that does not exist and
// could not tell a RECORD from a CITATION. §137 taught it that ⟦was:⟧ means retired, so the record
// now lives where its reader is. ⚠ It must NOT be "corrected" to the live name: "⟦was: <the current
// name>⟧" asserts that the old name was the new name, which is false and destroys the record.
//
// ⭐ WHY IT IS WIDENED BEFORE THE FIX AND NOT AFTER. §104: "Landing the Dispatch fix alone would
// turn this test FULLY GREEN while two doors still author on refusal — the exact false green this
// record exists to refuse, a guarantee that holds only where something happened to look." So the
// table below is DELIBERATELY RED ON ARRIVAL at two doors. ⚠ A widened assertion that arrives green
// has not been seen to bind.
//
// THE DEFECT: `vehicleIDForTypedNumber` CO-MINTS a `vehicle_created` event. Where that call sits
// above a door's validations, a POST refused further down has ALREADY authored one — and the
// operator is told nothing was recorded while a record exists on the muster's vehicle list.
//
// ⚠ THE DOOR COUNT IS DERIVED, NOT ASSUMED (§104 step 2). [MEASURED] four routes carry a co-mint —
// `trips.go:1033` (/trips/dispatch), `trips.go:1162` (/trips/arrive-unsolicited),
// `gate.go:1112` (/gate/checkin), and `gate.go:1330`+`:1464` (BOTH inside handleGateVehicleCheckOut,
// so /gate/checkout/vehicle is ONE door with two co-mint sites, not two doors).
//
// ⚠ EACH ARM MUST REFUSE **BELOW** ITS DOOR'S CO-MINT OR IT MEASURES NOTHING. A refusal that fires
// above the co-mint authors nothing trivially and would make the arm a comfort. That trap was met
// while deriving the count: three separate probes of /gate/checkout/vehicle all showed
// `authored=0`, and the discriminator — was the vehicle record actually created? — showed the
// co-mint had simply never run on any of them.
func TestARefusedPostAuthorsNothing(t *testing.T) {
	type door struct {
		route string
		site  string
		gate  bool // which fixture: the gate server (site-a) or the trips server (site-b)
		cases []struct {
			name string
			form url.Values
		}
	}
	type kase = struct {
		name string
		form url.Values
	}
	base := func(persons []domain.Person) url.Values {
		return url.Values{
			"time_source": {"default"}, "vehicle": {"TRK-14"},
			"driver_person_id": {persons[0].PersonID}, "occupant_person_ids": {persons[1].PersonID},
			"destination_site": {"site-a"}, "deadline_minutes": {"60"},
		}
	}
	_ = base

	doors := []door{
		{route: "/trips/dispatch", site: "site-b", cases: []kase{
			{"no time signal", url.Values{"vehicle": {"TRK-14"}, "destination_site": {"site-a"}, "deadline_minutes": {"60"}}},
			{"no destination (D-4)", url.Values{"time_source": {"default"}, "vehicle": {"TRK-14"}}},
			{"driver not on the roster", url.Values{"time_source": {"default"}, "vehicle": {"TRK-14"},
				"driver_person_id": {"site-b-person-ffffffffffffffff"},
				"destination_site": {"site-a"}, "deadline_minutes": {"60"}}},
			{"placeholder vehicle", url.Values{"time_source": {"default"}, "vehicle": {"walk-in"},
				"destination_site": {"site-a"}, "deadline_minutes": {"60"}}},
		}},
		{route: "/trips/arrive-unsolicited", site: "site-b", cases: []kase{
			{"no occupants", url.Values{"time_source": {"default"}, "vehicle": {"VAN 9"}, "origin_site": {"site-a"}}},
			{"no origin site", url.Values{"time_source": {"default"}, "vehicle": {"VAN 9"}}},
		}},
		{route: "/gate/checkin", site: "site-a", gate: true, cases: []kase{
			{"blank driver", url.Values{"time_source": {"default"}, "vehicle": {"VAN 9"},
				"driver_kind": {"new"}, "driver_value": {""}}},
			{"no driver at all", url.Values{"time_source": {"default"}, "vehicle": {"VAN 9"}}},
		}},
		{route: "/gate/checkout/vehicle", site: "site-a", gate: true, cases: []kase{
			{"typed name never checked in", url.Values{"time_source": {"default"}, "passenger_list": {"1"},
				"direction": {gateOutbound}, "vehicle": {"OUTBOUND 1"}, "driver_index": {"0"},
				"occupant_index": {"0"}, "occupant_kind": {"new"}, "occupant_typed": {"Ghost Rider"},
				"occupant_value": {""}}},
		}},
	}

	for _, d := range doors {
		d := d
		t.Run(d.route, func(t *testing.T) {
			for _, tc := range d.cases {
				tc := tc
				t.Run(tc.name, func(t *testing.T) {
					var srv *Server
					var st *store.Store
					var persons []domain.Person
					if d.gate {
						srv, st, persons = newGateTestServer(t)
					} else {
						srv, st, persons = newTripsTestServer(t)
					}
					form := url.Values{}
					for k, v := range tc.form {
						form[k] = v
					}
					// the dispatch door's cases need real person ids where they name one
					if d.route == "/trips/dispatch" && form.Get("driver_person_id") == "" && len(persons) > 0 {
						form.Set("driver_person_id", persons[0].PersonID)
						form.Set("occupant_person_ids", persons[1].PersonID)
					}

					before, err := st.Watermark(d.site, 1)
					if err != nil {
						t.Fatal(err)
					}
					rec := postForm(t, srv, d.route, form)
					if rec.Code == http.StatusSeeOther {
						t.Fatalf("precondition: %s %q was ACCEPTED (303) — this case must be REFUSED or it "+
							"measures nothing: %s", d.route, tc.name, rec.Body.String())
					}
					after, err := st.Watermark(d.site, 1)
					if err != nil {
						t.Fatal(err)
					}
					if after != before {
						t.Fatalf("%s %q was refused with %d but authored %d event(s) (watermark %d -> %d). "+
							"A refusal that writes is not a refusal: the operator is told nothing was recorded "+
							"while a record exists. ⚠ The cause is the vehicle co-mint running BEFORE this "+
							"door finishes validating — see §93 for the same defect at /trips/dispatch",
							d.route, tc.name, rec.Code, after-before, before, after)
					}
				})
			}
		})
	}
}

func TestDispatchAndCancelFormsCheckOutAndNeverReinstate(t *testing.T) {
	srv, st, persons := newTripsTestServer(t)

	// D-4: the console cannot author a destination-less departure at all.
	if rec := postForm(t, srv, "/trips/dispatch", url.Values{
		"time_source":         {"default"},
		"vehicle":             {"TRK-14"},
		"driver_person_id":    {persons[0].PersonID},
		"occupant_person_ids": {persons[1].PersonID},
	}); rec.Code != http.StatusConflict {
		t.Fatalf("destination-less console dispatch = %d, want 409 (D-4): %s", rec.Code, rec.Body.String())
	}
	if trips, err := domain.Trips(st); err != nil || len(trips) != 0 {
		t.Fatalf("a refused destination-less dispatch authored a trip: %v %+v", err, trips)
	}

	// Local check-in through the gate form — the degenerate trip + arrival.
	rec := postForm(t, srv, "/gate/checkin", url.Values{
		"time_source":    {"default"},
		"vehicle":        {"TRK-14"},
		"driver_kind":    {"existing"},
		"driver_value":   {persons[0].PersonID},
		"occupant_kind":  {"existing"},
		"occupant_value": {persons[1].PersonID},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("gate check-in = %d: %s", rec.Code, rec.Body.String())
	}
	trips, err := domain.Trips(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 1 || trips[0].DestinationSite != nil || trips[0].DeadlineMinutes != nil {
		t.Fatalf("degenerate trip mis-authored: %+v", trips)
	}

	openCount := func() int {
		visits, err := domain.Visits(st)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, v := range visits {
			if v.IsOpen == 1 {
				n++
			}
		}
		return n
	}
	if got := openCount(); got != 2 {
		t.Fatalf("after local check-in: %d open visits, want 2", got)
	}

	// Dispatch away — vehicle stored verbatim, both people checked out.
	// ⟦LOCK — §88, 2026-09-03⟧ ⟦was: "vehicle stored verbatim"⟧ — kept as the heading because it is
	// still what this block is about, but "verbatim" now means UNMANGLED, not UNTOUCHED. The
	// difference is one keystroke of padding and is spelled out at the assertion below.
	rec = postForm(t, srv, "/trips/dispatch", url.Values{
		"time_source":         {"default"},
		"vehicle":             {"  TRUCK   12"},
		"driver_person_id":    {persons[0].PersonID},
		"occupant_person_ids": {persons[1].PersonID},
		"destination_site":    {"site-a"},
		"deadline_minutes":    {"60"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("dispatch = %d: %s", rec.Code, rec.Body.String())
	}
	trips, err = domain.Trips(st)
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
		t.Fatal("dispatched trip not found")
	}
	// ⟦LOCK — W7 seam repair, 2026-09-03⟧ ⟦was: "if away.VehicleEntered != \"  TRUCK   12\" {"⟧
	//
	// ⟦LOCK — §88, 2026-09-03⟧ THE EXPECTED VALUE LOSES ITS PADDING AND KEEPS ITS INTERNAL
	// SPACING. This is §65 B1's disposition extended to the FORM layer — B1 ruled the same
	// criterion one layer in, at the fixture; §82 raised this site as the consumer-visible half,
	// and §88 ruled it.
	//
	// ⚠ WHAT WAS MEASURED, because the obvious answer was wrong. The question was whether the
	// FORM path trims on top of `CreateVehicle`'s deliberate `trimmedOrNil` — a second,
	// undeliberate trim. THERE ARE TWO TRIMS: `ResolveOrCreateVehicleByNumber` (vehicle.go:422,
	// the co-mint every handler calls through `vehicleIDForTypedNumber`) and `trimmedOrNil`
	// inside `CreateVehicle`. ⭐ BUT THE FORM'S IS NOT THE CAUSE, and the counterfactual was
	// PRODUCED rather than argued: calling `CreateVehicle("  TRUCK   12")` DIRECTLY — no handler,
	// no co-mint — still yields a record numbered "TRUCK   12". So the form's trim is REDUNDANT
	// for the stored value; deleting it would change nothing, and it is deliberately left alone
	// (it still serves the "the officer typed nothing" check on its own next line).
	//
	// ⚠ WHY "EXACTLY AS TYPED" COULD NOT SURVIVE AS WRITTEN. Its subject was the operator's
	// KEYSTROKES, and post-W7 `trip.vehicle_entered` is DERIVED from the composed record's own
	// number (tripauthor.go:28) — so the trip can only ever carry the RECORD's form. Carrying the
	// keystrokes instead would need a free-text vehicle string back on an authoring door, which
	// `DispatchInput`'s own comment forbids in terms ("RE-ADDING ONE UNDOES DR-W7-4") and which
	// `TestW7DispatchInputTakesAVehicleIDNotAString` enforces as a locked census. ⭐ The keystroke
	// reading was barred by a census, not by preference.
	//
	// ⚠ WHAT THIS GIVES UP, STATED RATHER THAN LEFT TO BE FOUND: after this change NOTHING in the
	// tree asserts that an operator's leading/trailing whitespace survives a form round trip —
	// [MEASURED] this was the only padded vehicle POSTed through any form. §88 accepts that FOR
	// VEHICLE NUMBERS SPECIFICALLY: padding on "  TRUCK 12" carries no meaning, so dropping it is
	// correct behaviour rather than lost provenance. ⚠ §88 SCHEDULES the general question — which
	// operator-entered fields are trimmed on the way in, and whether each is deliberate — because
	// leading whitespace on a NAME or a CARGO NOTE may not be meaningless. That is NOT this
	// change and must not be smuggled into it.
	//
	// ⚠ THE THREE SPACES BETWEEN "TRUCK" AND "12" ARE THE WHOLE FALSIFIABILITY OF THIS CHECK.
	// They are DATA. This is an equality test on the trip's own field — never a `Contains` over a
	// dump — because a loose needle here would be satisfied by a store that collapsed the run,
	// which is the exact mangling this asserts against. (B1's first needle WAS loose and passed a
	// plant; see reports/window-build/S62-ROUTES.md §9.1 and §25.)
	// ⟦was: "if away.VehicleEntered == nil || *away.VehicleEntered != \"  TRUCK   12\" {"⟧
	// ⟦was: "form round-trip altered the vehicle id: %q — it must be stored exactly as typed"⟧
	if away.VehicleEntered == nil || *away.VehicleEntered != "TRUCK   12" {
		t.Fatalf("form round-trip MANGLED the vehicle id: %q, want %q. Leading and trailing space is dropped "+
			"at the typing moment by design (trimmedOrNil, DR-W7-15: padding is ABSENCE, not data) — but the "+
			"INTERNAL spacing an operator typed is data and must reach storage untouched",
			vehicleSnapshotText(away.VehicleEntered), "TRUCK   12")
	}
	if got := openCount(); got != 0 {
		t.Fatalf("after dispatch: %d open visits, want 0 (occupants checked out)", got)
	}

	// ⟦D-78, 2026-09-05⟧ THE CANCEL HALF IS INVERTED, NOT DELETED.
	// ⟦was: "// Cancel — the trip closes, and NOBODY is reinstated."⟧
	// ⟦was: "if rec.Code != http.StatusSeeOther { t.Fatalf("cancel = %d: %s", …) }"⟧
	// ⟦was: "if s.TripID == away.TripID && s.State != domain.TripStateCancelled { t.Fatalf("cancelled trip state = %s", s.State) }"⟧
	// D-73: no trip is cancelled — a dispatched vehicle continues to the next
	// post, and coming back is a second trip. D-78 closed the route. The same
	// POST, with the same field shape, must now author NOTHING and leave the
	// trip open. What this test always owned — NOBODY is reinstated — still
	// holds, for the older reason: the crew departed, their visits closed at
	// dispatch, and no route puts them back on site.
	wmBefore := w2Watermark(t, st, srv.Cfg.SiteID, srv.Cfg.NodeEpoch)
	rec = postForm(t, srv, "/trips/cancel", url.Values{"time_source": {"default"}, "trip_id": {away.TripID}})
	if wmAfter := w2Watermark(t, st, srv.Cfg.SiteID, srv.Cfg.NodeEpoch); wmAfter != wmBefore {
		t.Fatalf("D-78: the closed cancel route authored %d event(s) (response %d: %s)", wmAfter-wmBefore, rec.Code, rec.Body.String())
	}
	states, err := domain.TripStates(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range states {
		if s.TripID == away.TripID && s.State != domain.TripStateOpen {
			t.Fatalf("D-78: after the old cancel POST the trip reads %s, want %s — the state the route used to produce must be unreachable from it", s.State, domain.TripStateOpen)
		}
	}
	if got := openCount(); got != 0 {
		t.Fatalf("after the old cancel POST: %d open visits, want 0 — nobody is reinstated by a POST that authors nothing", got)
	}
}

func TestUnsolicitedArrivalFormCorrelatesOrRefusesToGuess(t *testing.T) {
	srv, st, persons := newTripsTestServer(t)
	injectForeignTrip(t, st, 1, "TRUCK 12", 60, persons[0].PersonID, persons[1].PersonID)

	rec := postForm(t, srv, "/trips/arrive-unsolicited", url.Values{
		"time_source": {"default"},
		"vehicle":     {"truck 12"},
		"origin_site": {"site-a"},
		"person_ids":  {persons[0].PersonID, persons[1].PersonID},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unsolicited = %d: %s", rec.Code, rec.Body.String())
	}
	arrStates, err := domain.ArrivalStates(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(arrStates) != 1 || arrStates[0].Disposition != domain.ArrivalCorrelated {
		t.Fatalf("single open match should correlate: %+v", arrStates)
	}

	// A second matching trip makes the NEXT unsolicited arrival ambiguous —
	// surfaced on the page's review lane, never guessed.
	injectForeignTrip(t, st, 2, "Truck  12", 60, persons[0].PersonID, persons[1].PersonID)
	injectForeignTrip(t, st, 3, "TRUCK 12x", 60, persons[0].PersonID, persons[1].PersonID) // decoy, different vehicle
	rec = postForm(t, srv, "/trips/arrive-unsolicited", url.Values{
		"time_source": {"default"},
		"vehicle":     {"truck 12"},
		"origin_site": {"site-a"},
		"person_ids":  {persons[0].PersonID},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("second unsolicited = %d: %s", rec.Code, rec.Body.String())
	}
	body := getTrips(t, srv)
	if !strings.Contains(body, "ambiguous-arrival") || !strings.Contains(body, "will NOT pick") {
		t.Fatalf("review lane does not surface the ambiguous arrival:\n%s", body)
	}
}
