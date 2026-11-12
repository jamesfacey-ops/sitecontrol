// witness_r28_vehicle_mode_test.go — team-lead's four explicit R-28 "PROVE
// IT" witnesses, over the real HTTP surface (never a hand-built body):
//
//	(i)   a vehicle carrying 3 passengers, checked in AND dispatched through
//	      the real vehicle-mode surfaces at post A, appears on the inbound
//	      board at post B; ONE POST /trips/arrive puts all 3 on B's roster,
//	      they are gone from A's roster, and the in-transit alert A was
//	      carrying for that vehicle is gone too.
//	(ii)  at an orientation post, one un-oriented passenger among three is
//	      held out (visibly, on the rendered page) while the other two are
//	      admitted — one arrival POST, not three.
//	(iii) a site with no configured destinations offers no mode choice at
//	      login, and its individual page renders exactly as it always did.
//	(iv)  the mode radio chosen at login is what selects /gate vs
//	      /gate/vehicle as the landing page — including the case where
//	      "vehicle" is requested at a site that cannot honour it.
//
// TWO POSTS, ONE STORE: this package's own precedent for a cross-post
// scenario (inbound_test.go's newInboundWorld, muster_in_transit_test.go's
// roadWorld) is a SINGLE *store.Store authored under two different site
// ids, read by two different *Server values configured for those sites —
// a real two-node HTTP+replication mesh belongs to internal/repl, not to a
// package whose whole job is rendering what one post's store already
// holds. Witness (i) follows that precedent for the RECEIVING side (server
// B reads the same store server A wrote), while driving the DISPATCHING
// side through server A's real HTTP handlers (the vehicle-mode arriving
// form, the roster's tick-and-depart flow, the real commit routes) so the
// "in vehicle mode" half of the claim is actually exercised, not asserted
// by construction.
//
// HONESTY NOTE on "RED before GREEN": these are new-capability witnesses,
// not regression tests for a pre-existing defect — there is no earlier
// broken behaviour to revert to and watch fail. What IS meaningfully RED
// is each test's OWN precondition assertions (aboard count, in-transit
// count, notice count before the fix-shaped action) failing if the
// surrounding plumbing regresses; each test asserts the BEFORE state
// before performing the ONE action, then the AFTER state, so a future
// regression in gateLandingPath, commitGateCheckIn, VehicleModeEnabled, or
// the arrival redirect would turn one of these red on its own.
package web

import (
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

// --- witness (i): cross-post one-click arrival, driven through the real
// vehicle-mode surfaces at the dispatching post ----------------------------

func TestWitnessVehicleModeCrossPostOneClickArrivalMovesTheWholeCrewAndCancelsTheAlert(t *testing.T) {
	srvA, st, persons := newGateTestServer(t) // site-a: Dana Driver, Pat Passenger, Chris Crew
	names := []string{persons[0].Name, persons[1].Name, persons[2].Name}

	// --- check the trio IN at A through the real vehicle-mode arriving
	// form (gate_vehicle.html's "VEHICLE ARRIVING" pane, POST /gate/checkin
	// — commitGateCheckIn, no second engine).
	_, checkinVals := gateBuild(t, srvA, gateInbound, "TRK-77", names, 0)
	if rec := postForm(t, srvA, "/gate/checkin", checkinVals); rec.Code != http.StatusSeeOther {
		t.Fatalf("check-in at A = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 3 {
		t.Fatalf("precondition: %d visits open at site-a after check-in, want 3", len(open))
	}
	var visitIDs []string
	for _, n := range names {
		visitIDs = append(visitIDs, visitIDForCrewName(t, st, n))
	}

	// --- dispatch the trio TOGETHER via the roster's tick-and-depart door
	// (gate_roster_half -> the SAME compact confirm box R-28 spec (c)
	// calls for), destined for site-b, one commit.
	_, outboundVals := gateBuildOutbound(t, srvA, "TRK-77", visitIDs, nil, 0)
	outboundVals.Set("destination_id", "site-b")
	outboundVals.Set("deadline_minutes", "90")
	rec := postForm(t, srvA, "/gate/checkout/vehicle", outboundVals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("dispatch at A = %d, want 303: %s", rec.Code, rec.Body.String())
	}

	// AFTER dispatch, BEFORE arrival: gone from A's roster, and A is
	// carrying the in-transit alert for this vehicle. This is the "RED"
	// half of this test's own before/after pair.
	open, err = domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 0 {
		t.Fatalf("%d people still show on site at A after the whole crew departed together: %+v", len(open), open)
	}
	before, err := domain.InTransitFrom(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 {
		t.Fatalf("A's in-transit alert count = %d before arrival, want exactly 1 (TRK-77 on the road)", len(before))
	}

	trips, err := domain.Trips(st)
	if err != nil {
		t.Fatal(err)
	}
	var tripID string
	for _, tr := range trips {
		// Two trips originate at site-a here: the LOCAL check-in trip
		// authored (and immediately self-arrived) by commitGateCheckIn
		// above, and the real departure to site-b. Only the one with a
		// destination is the departure under test — matching on
		// OriginSite alone picked up the already-arrived local trip in an
		// earlier version of this test and the arrival POST below was
		// correctly refused (409: "has already arrived here").
		if tr.OriginSite == "site-a" && tr.DestinationSite != nil && *tr.DestinationSite == "site-b" {
			tripID = tr.TripID
		}
	}
	if tripID == "" {
		t.Fatalf("no site-a -> site-b trip authored: %+v", trips)
	}

	// --- server B: the SAME store, site-b's own configuration (plain,
	// destination back to site-a — this is not the orientation scenario,
	// that is witness (ii) below).
	cfgB := &config.Config{
		SiteID: "site-b", DisplayName: "Pad Site Control", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 3600,
		LoginPassword: testLoginPassword,
		Peers:         []config.Peer{{SiteID: "site-a", BaseURL: "http://example.invalid"}},
		Destinations:  []config.Destination{{ID: "site-a", DisplayName: "Ice Road", Controlled: true}},
	}
	srvB := New(cfgB, st)

	// The vehicle is genuinely LISTED, inbound, at B before anything is
	// clicked.
	pageB := noScriptMarkup(t, srvB, "/gate/vehicle")
	block := vehicleBlock(t, pageB, "TRK-77")
	if !strings.Contains(block, "3 aboard") {
		t.Fatalf("B's inbound row for TRK-77 does not read 3 aboard:\n%s", block)
	}
	for _, n := range names {
		if !strings.Contains(block, n) {
			t.Errorf("B's inbound row for TRK-77 does not name %s", n)
		}
	}

	// --- ONE arrival POST at B, all three ticked.
	form := url.Values{"time_source": {"default"}, "trip_id": {tripID}, "from": {"gate"}}
	for _, p := range persons {
		form.Add("manifest_person_ids", p.PersonID)
		form.Add("arriving_person_ids", p.PersonID)
	}
	rec = postForm(t, srvB, "/trips/arrive", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("one-click arrival at B = %d, want 303: %s", rec.Code, rec.Body.String())
	}

	// AFTER: all three on B's roster, in one POST.
	openB, err := domain.OpenVisitsAt(st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(openB) != 3 {
		t.Fatalf("%d visits open at site-b after the one-click arrival, want 3: %+v", len(openB), openB)
	}
	got := map[string]bool{}
	for _, v := range openB {
		got[v.PersonID] = true
	}
	for _, p := range persons {
		if !got[p.PersonID] {
			t.Errorf("%s was aboard TRK-77 but is not on B's roster after the one-click arrival", p.Name)
		}
	}

	// AND the alert is cancelled: A no longer carries TRK-77 as in-transit
	// (the trip has folded to "arrived", which is InTransitFrom's own
	// documented exclusion — its occupants are on the destination's
	// roster now, not on the road any more).
	after, err := domain.InTransitFrom(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 {
		t.Fatalf("A's in-transit alert count = %d after arrival, want 0 (the alert was not cancelled): %+v", len(after), after)
	}
}

// --- witness (ii): orientation hold-out plus admit, at the receiving
// post's real inbound board ------------------------------------------------

func TestWitnessVehicleModeOrientationHoldsOutOneAndAdmitsTwoInOneArrivalPost(t *testing.T) {
	// orientBlockWorld (gate_orientation_block_test.go): a site-b pad with
	// orientation=true, and three people — driver + orla already hold
	// site-b's orientation, uma never does. Reused rather than
	// re-declared: the SAME fixture the pre-existing cross-site-orientation
	// suite already trusts for "who holds this post's orientation".
	w := newOrientBlockWorld(t, true)
	riding := []domain.Person{w.driver, w.orla, w.uma}

	dest, dl := "site-b", int64(90)
	trip, err := domain.Dispatch(w.st, "site-a", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, w.st, "site-a", "BUS-9"), DriverPersonID: w.driver.PersonID,
		OccupantPersonIDs: []string{w.orla.PersonID, w.uma.PersonID},
		DestinationSite:   &dest, DeadlineMinutes: &dl,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	// BEFORE: nobody from BUS-9 is on B's roster yet.
	openBefore, err := domain.OpenVisitsAt(w.st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(openBefore) != 0 {
		t.Fatalf("precondition: %d visits already open at site-b, want 0", len(openBefore))
	}

	// ONE arrival POST, all three ticked — R-9 holds: the officer does not
	// pre-filter who to tick, the post's own orientation gate does.
	form := url.Values{"time_source": {"default"}, "trip_id": {trip.TripID}, "from": {"gate"}}
	for _, p := range riding {
		form.Add("manifest_person_ids", p.PersonID)
		form.Add("arriving_person_ids", p.PersonID)
	}
	rec := postForm(t, w.srv, "/trips/arrive", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("arrival = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("Location %q did not parse: %v", rec.Header().Get("Location"), err)
	}

	// AFTER: the two oriented riders are on B's roster; Uma is not.
	openAfter, err := domain.OpenVisitsAt(w.st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	on := map[string]bool{}
	for _, v := range openAfter {
		on[v.PersonID] = true
	}
	if !on[w.driver.PersonID] || !on[w.orla.PersonID] {
		t.Fatalf("the two oriented riders are not both on B's roster after arrival: %+v", openAfter)
	}
	if on[w.uma.PersonID] {
		t.Fatalf("Uma Unoriented was admitted to B's roster despite holding no orientation here")
	}
	if len(openAfter) != 2 {
		t.Fatalf("%d visits open at site-b after arrival, want exactly 2 (the third held out)", len(openAfter))
	}

	// AND SHE IS VISIBLE — on the actual rendered page the redirect lands
	// on, not just absent from a store query. ⟦R-28 revision 2, team-lead,
	// 2026-09-16⟧ item 3 replaced the long NOT ENTERED paragraph with ONE
	// compact line under the green result for exactly this one-click-
	// arrival surface (the long wording stays, unchanged, for the
	// ordinary check-in-commit hold-out — see gate_vehicle.html's own
	// comment) — so this now reads the compact line instead, still
	// proving the same fact: named, visible, exactly once.
	page := noScriptMarkup(t, w.srv, loc.RequestURI())
	heldOutCount := strings.Count(page, "held-out-compact")
	if heldOutCount != 1 {
		t.Fatalf("the arrival page shows %d held-out lines, want exactly 1:\n%s", heldOutCount, page)
	}
	at := strings.Index(page, `class="held-out-compact"`)
	end := strings.Index(page[at:], "</p>")
	if at < 0 || end < 0 {
		t.Fatalf("could not locate the held-out-compact paragraph on the page:\n%s", page)
	}
	heldOutLine := page[at : at+end]
	if !strings.Contains(heldOutLine, "1 held out") {
		t.Errorf("the held-out line does not state the count: %s", heldOutLine)
	}
	if !strings.Contains(heldOutLine, w.uma.Name) {
		t.Errorf("the hold-out line does not name %s: %s", w.uma.Name, heldOutLine)
	}
	// The two ADMITTED riders' names belong on the roster this page also
	// renders, never inside the held-out line itself — scoped to that one
	// paragraph, not the whole page, since their names legitimately
	// appear elsewhere (the on-site roster) once they are checked in.
	if strings.Contains(heldOutLine, w.driver.Name) || strings.Contains(heldOutLine, w.orla.Name) {
		t.Errorf("an oriented rider is wrongly named in the held-out line: %s", heldOutLine)
	}
	// The one-click result line itself: 2 checked in, not 3 and not 0 —
	// the held-out rider must not silently inflate or vanish from the count
	// an officer reads at a glance.
	if got := loc.Query().Get("arrived_checked"); got != "2" {
		t.Errorf("arrived_checked = %q, want %q", got, "2")
	}
}

// --- witness (iii): a site with no configured destinations offers no
// vehicle mode, anywhere ----------------------------------------------------

func TestWitnessNoDestinationSiteOffersNoModeChoiceAndIndividualPageIsUnchanged(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "solo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		SiteID: "site-c", DisplayName: "Camp Solo", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 3600,
		LoginPassword: testLoginPassword,
		// Deliberately no Peers, no Destinations: VehicleModeEnabled derives
		// from len(DestinationList()) > 0 (config.go), not a separate flag.
	}
	srv := New(cfg, st)
	if srv.Cfg.VehicleModeEnabled() {
		t.Fatal("fixture: a config with no destinations reports VehicleModeEnabled")
	}

	// styleRe-stripped (trips_uncontrolled_test.go): a class name in the
	// one inlined stylesheet — .login-mode-choice, .chip-mode a — is not a
	// rendered element (this package's own F19 trap; MEASURED live here,
	// an earlier version of this test matched the CSS rule instead of any
	// actual markup).
	loginPage := styleRe.ReplaceAllString(rawPage(t, srv, "/login"), "")
	if strings.Contains(loginPage, "login-mode-choice") {
		t.Errorf("the login page offers a mode choice at a site with no destinations:\n%s", loginPage)
	}
	if strings.Contains(loginPage, "Vehicle check-in") {
		t.Errorf("the login page mentions vehicle check-in at a site with no destinations:\n%s", loginPage)
	}
	if !strings.Contains(loginPage, ">Log in<") {
		t.Errorf("the login page does not offer the plain single Log in button:\n%s", loginPage)
	}

	gatePage := styleRe.ReplaceAllString(rawPage(t, srv, "/gate"), "")
	if strings.Contains(gatePage, "chip-mode") {
		t.Errorf("the individual page's header carries a switch-mode chip at a site with no destinations:\n%s", gatePage)
	}
	if strings.Contains(gatePage, `href="/gate/vehicle"`) {
		t.Errorf("the individual page links to vehicle check-in at a site with no destinations:\n%s", gatePage)
	}
	// The page a human actually uses is otherwise unchanged: the same
	// on-site heading R-27's individual page has always rendered.
	if !strings.Contains(gatePage, "On site now (0)") {
		t.Errorf("the individual page does not render its ordinary on-site heading:\n%s", gatePage)
	}
}

// --- witness (iv): the mode choice at login is what selects the landing
// page --------------------------------------------------------------------

// postLoginRaw performs one login POST without postForm's fixed next=/, so
// mode and next can be controlled directly. Returns the response — the
// caller reads Location, never follows it here (this is proving WHICH page
// was selected, not rendering it).
func postLoginRaw(t *testing.T, srv *Server, mode string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"password": {testLoginPassword}}
	if mode != "" {
		form.Set("mode", mode)
	}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login (mode=%q) = %d, want 303: %s", mode, rec.Code, rec.Body.String())
	}
	return rec
}

func TestWitnessLoginModeChoiceSelectsTheRenderedLandingPage(t *testing.T) {
	srvA, _, _ := newGateTestServer(t) // vehicle mode enabled (destinations configured)

	if got := postLoginRaw(t, srvA, "vehicle").Header().Get("Location"); got != "/gate/vehicle" {
		t.Errorf("mode=vehicle at a vehicle-mode site landed on %q, want /gate/vehicle", got)
	}
	if got := postLoginRaw(t, srvA, "individual").Header().Get("Location"); got != "/gate" {
		t.Errorf("mode=individual landed on %q, want /gate", got)
	}
	if got := postLoginRaw(t, srvA, "").Header().Get("Location"); got != "/gate" {
		t.Errorf("no mode field (the plain-site shape) landed on %q, want /gate — individual is the default", got)
	}

	// And the safety case: requesting "vehicle" at a site that cannot
	// honour it is a silent, safe fallback to individual — never an error,
	// never a lockout, exactly as VehicleModeEnabled's own doc promises.
	stC, err := store.Open(filepath.Join(t.TempDir(), "solo2.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stC.Close() })
	if err := domain.Register(stC); err != nil {
		t.Fatal(err)
	}
	srvC := New(&config.Config{
		SiteID: "site-c", DisplayName: "Camp Solo", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: stC.Path, SyncIntervalSeconds: 3600,
		LoginPassword: testLoginPassword,
	}, stC)
	if got := postLoginRaw(t, srvC, "vehicle").Header().Get("Location"); got != "/gate" {
		t.Errorf("mode=vehicle at a no-destination site landed on %q, want /gate (safe fallback)", got)
	}
}
