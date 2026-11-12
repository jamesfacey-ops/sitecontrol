// gate_test.go — C8's web-layer done-when tests: the driver+2-passenger
// check-in's SERVER round-trip count (measured, not assumed — see
// TestScriptedCheckInStaysWithinTheRoundTripBudget's own comment for what is
// and is not measured from here), the M6 off-roster add-on-demand confirm
// path, check-out closing a visit, the history test (§8.8-i: a later alias
// must never alter what the on-site list already showed), and row 31's
// crash-window recovery surfaced through the actual page.
//
// postForm is shared from trips_test.go (same package).
package web

import (
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

func newGateTestServer(t *testing.T) (*Server, *store.Store, []domain.Person) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	co, err := domain.CreateCompany(st, "site-a", 1, "Kakivik Asset Management", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ DR-W8-2 arity: the company argument is dropped — a trade is a site-wide open set, not scoped to a company. ⟦was: "wg, err := domain.CreateTrade(st, "site-a", 1, co.CompanyID, "Inspection", 0, "default")"⟧
	wg, err := domain.CreateTrade(st, "site-a", 1, "Inspection", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	var persons []domain.Person
	for _, name := range []string{"Dana Driver", "Pat Passenger", "Chris Crew"} {
		p, err := domain.CreatePerson(st, "site-a", 1, name, &co.CompanyID, &wg.TradeID, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		persons = append(persons, p)
	}
	cfg := &config.Config{
		SiteID: "site-a", DisplayName: "Ice Road A", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 10,
		LoginPassword: testLoginPassword,
		// One peer and the two destination shapes the real deploy configs
		// carry (deploy/site-a.toml): a CONTROLLED peer with a gate, where a
		// deadline is required and a timer runs, and an UNCONTROLLED pad
		// where a deadline is refused because nobody there can record the
		// arrival that would close it. The departing-vehicle form offers
		// both, so a fixture with neither would make every assertion about
		// its destination control true by having nothing to choose from.
		Peers: []config.Peer{{SiteID: "site-b", BaseURL: "http://example.invalid"}},
		Destinations: []config.Destination{
			{ID: "site-b", DisplayName: "Pad Site Control", Controlled: true},
			{ID: "conneco", DisplayName: "Conneco Pad", Controlled: false},
		},
	}
	return New(cfg, st), st, persons
}

// getGate ⟦R-27, James, 2026-09-16⟧ fetches the WHOLE-VEHICLE gate page —
// GET /gate/vehicle since the split, not GET /gate any more (that is now
// the R-27 individual page, gate.html; the group accumulate form this
// helper's callers exercise moved unchanged to gate_vehicle.html). Nearly
// every test in this package that calls getGate/gateBuild/gateAdd is
// testing the ACCUMULATE machinery itself, which did not change behaviour
// — only its address did — so retargeting this one helper is what keeps
// that whole body of tests meaning what they always meant. See
// getGateIndividual below for the new page.
func getGate(t *testing.T, srv *Server) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/gate/vehicle", nil))
	return rec.Code, rec.Body.String()
}

// getGateIndividual ⟦R-27⟧ fetches the individual page (gate.html) —
// used by tests that specifically exercise the streamlined one-person
// CHECK IN form and its own copy of the on-site roster.
func getGateIndividual(t *testing.T, srv *Server) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/gate", nil))
	return rec.Code, rec.Body.String()
}

// countingHandler counts every ServeHTTP call it forwards — the automatic,
// impossible-to-fudge-by-hand instrument behind the round-trip measurement
// below: it counts actual dispatches through the real mux, not a manual
// tally of what the test author intended to do.
type countingHandler struct {
	inner http.Handler
	n     int
}

func (c *countingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.n++
	c.inner.ServeHTTP(w, r)
}

// TestScriptedCheckInStaysWithinTheRoundTripBudget is C8's own accountable
// half of the §6 interaction budget (chunk-plan C8: "it is measured, not
// asserted"). FIXED after verification (round 1): the previous version
// called ServeHTTP for GET /gate and POST /gate/checkin directly and counted
// those two — then followed the 303 with a THIRD, separately-issued GET it
// explicitly excluded from the count as "outside the budget". That GET is
// not outside anything: POST /gate/checkin returns 303 See Other, and every
// real browser auto-follows a 303 with a GET as part of the SAME user
// action. The old test proved "two of my own ServeHTTP calls", a different,
// same-numbered claim from "the round trips one check-in costs".
//
// This version runs against a real httptest.NewServer over loopback TCP,
// using ts.Client() — a genuine net/http client that auto-follows 3xx
// exactly like a browser (redirect included) — wrapped in a counting
// http.Handler so EVERY dispatch through the mux is counted, redirect
// follow included. It reports BOTH figures the review asked to distinguish:
//   - cold start (this session's first check-in: GET /gate, the page load,
//     PLUS the check-in's own POST + redirect-follow GET) = 3.
//   - steady state (design §6's actual target — the gate page is already
//     open from the PREVIOUS check-in's own redirect-follow GET, matching
//     the real "3 check-ins/minute" gate workflow, §10): POST + the
//     redirect-follow GET = 2.
//
// WHAT THIS STILL DOES NOT MEASURE: the ≤10-keystrokes half of the budget.
// There is no browser in this environment, so no keypress or click ever
// actually happens; that half stays enumerated analytically in the build
// report, not claimed here.
func TestScriptedCheckInStaysWithinTheRoundTripBudget(t *testing.T) {
	srv, _, persons := newGateTestServer(t)
	counted := &countingHandler{inner: srv.Handler()}
	ts := httptest.NewServer(counted)
	defer ts.Close()
	client := ts.Client() // default CheckRedirect: follows 3xx, converts POST->GET on 303, exactly like a browser
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client.Jar = jar // carries the C11 session cookie across requests, exactly like a real browser

	// C11: log in ONCE, outside the counted budget — that happens at shift
	// handover, not per check-in (§12: "must not obstruct gate operation
	// during a shift-change queue"). Resetting counted.n after it is what
	// keeps the budget measuring the check-in alone. There is no second
	// handover step to exclude any more: D-5 retired the officer picker,
	// so a logged-in session authors on its first submission.
	loginResp, err := client.PostForm(ts.URL+"/login", url.Values{"password": {testLoginPassword}, "next": {"/gate"}})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	loginResp.Body.Close()
	counted.n = 0

	// Cold start: page load, then the driver+2-passenger check-in.
	resp, err := client.Get(ts.URL + "/gate")
	if err != nil {
		t.Fatalf("GET /gate: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /gate = %d", resp.StatusCode)
	}

	form := url.Values{
		"time_source":    {"default"},
		"vehicle":        {"T12"},
		"driver_kind":    {"existing"},
		"driver_value":   {persons[0].PersonID},
		"occupant_kind":  {"existing", "existing"},
		"occupant_value": {persons[1].PersonID, persons[2].PersonID},
	}
	resp, err = client.PostForm(ts.URL+"/gate/checkin", form)
	if err != nil {
		t.Fatalf("POST /gate/checkin: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK { // the client already followed the 303 -> this is the redirect target's response
		t.Fatalf("post-check-in (after redirect follow) = %d: %s", resp.StatusCode, body)
	}
	coldStart := counted.n
	if coldStart != 3 {
		t.Fatalf("cold-start round trips = %d, want 3 (GET page load + POST /gate/checkin + the browser's automatic redirect-follow GET)", coldStart)
	}

	// Criterion 1: all occupants appear on the on-site list IMMEDIATELY —
	// asserted against the page the redirect follow ITSELF returned, the
	// same page a real officer would already be looking at.
	for _, p := range persons {
		if !strings.Contains(string(body), p.Name) {
			t.Fatalf("on-site list is missing %q immediately after check-in:\n%s", p.Name, body)
		}
	}
	if !strings.Contains(string(body), "On site now (3)") {
		t.Fatalf("on-site count did not reach 3 immediately:\n%s", body)
	}

	// Steady state: the gate page is already open (from the redirect follow
	// above, exactly as it would be after the previous real check-in) —
	// only the submit itself is new. This is design §6's actual per-check-in
	// target.
	counted.n = 0
	steadyForm := url.Values{"time_source": {"default"}, "vehicle": {"T13"}, "driver_kind": {"existing"}, "driver_value": {persons[0].PersonID}}
	resp, err = client.PostForm(ts.URL+"/gate/checkin", steadyForm)
	if err != nil {
		t.Fatalf("steady-state POST /gate/checkin: %v", err)
	}
	resp.Body.Close()
	steadyState := counted.n
	if steadyState > 2 {
		t.Fatalf("steady-state round trips = %d, want <= 2 (POST + the browser's automatic redirect-follow GET; design §6)", steadyState)
	}
	t.Logf("measured round trips: cold start (page load + check-in) = %d, steady state (check-in only, page already open) = %d", coldStart, steadyState)
}

// TestOffRosterOccupantTriggersAddOnDemandAndCompletes is M6's web-layer
// assert: an occupant submitted with kind=new (what gate.html's confirm()
// gate authorizes client-side — see the file comment on why the server
// itself does not re-derive "confirmed") creates the person through the
// ordinary authoring path and the check-in completes.
func TestOffRosterOccupantTriggersAddOnDemandAndCompletes(t *testing.T) {
	srv, st, persons := newGateTestServer(t)

	rec := postForm(t, srv, "/gate/checkin", url.Values{
		"time_source":        {"default"},
		"vehicle":            {"TRK-14"},
		"driver_kind":        {"existing"},
		"driver_value":       {persons[0].PersonID},
		"occupant_kind":      {"new"},
		"occupant_value":     {"Off Roster Ozzy"},
		"occupant_company":   {""},
		"occupant_workgroup": {""},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("check-in with an off-roster occupant = %d: %s", rec.Code, rec.Body.String())
	}

	all, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range all {
		if p.Name == "Off Roster Ozzy" {
			found = true
		}
	}
	if !found {
		t.Fatal("off-roster occupant was not created via the ordinary CreatePerson path")
	}

	_, body := getGate(t, srv)
	if !strings.Contains(body, "Off Roster Ozzy") {
		t.Fatalf("newly added occupant is not on the on-site list:\n%s", body)
	}
}

// TestCheckOutClosesVisitEndToEnd covers the check-out button end to end.
func TestCheckOutClosesVisitEndToEnd(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	rec := postForm(t, srv, "/gate/checkin", url.Values{
		"time_source": {"default"},
		"vehicle":     {"TRK-14"}, "driver_kind": {"existing"}, "driver_value": {persons[0].PersonID},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("check-in = %d", rec.Code)
	}
	visits, err := domain.Visits(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(visits) != 1 {
		t.Fatalf("visits = %d, want 1", len(visits))
	}

	rec = postForm(t, srv, "/gate/checkout", url.Values{"time_source": {"default"}, "visit_id": {visits[0].VisitID}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("check-out = %d: %s", rec.Code, rec.Body.String())
	}

	_, body := getGate(t, srv)
	// NOTE: the roster (everyone, on-site or not) is always embedded in the
	// page for the autocomplete, so persons[0].Name legitimately still
	// appears there — "On site now (0)" / "Nobody on site" is the actual
	// on-site-list assertion, not a bare substring check on the whole body.
	if !strings.Contains(body, "On site now (0)") || !strings.Contains(body, "Nobody on site") {
		t.Fatalf("on-site list should be empty after the only visit checks out:\n%s", body)
	}
}

// TestOnSiteListNeverReResolvesCompanyEvenAfterAlias is the chunk's named
// history test (design §8.8-i / chunk brief): "changing a person's
// workgroup afterwards does not alter the displayed historical visit."
// There is no edit-person mechanism in this codebase, so — mirroring C9's
// OWN proof for the muster reprint (domain/muster_test.go,
// TestReprintNeverResolvesAliasEvenWhenOneExists) — this test uses a dev
// alias linking the snapshotted company to a different canonical company
// AFTER check-in. A re-resolving on-site list would show the alias target;
// the correct one must keep showing exactly what was recorded at check-in.
func TestOnSiteListNeverReResolvesCompanyEvenAfterAlias(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	rec := postForm(t, srv, "/gate/checkin", url.Values{
		"time_source": {"default"},
		"vehicle":     {"TRK-14"}, "driver_kind": {"existing"}, "driver_value": {persons[0].PersonID},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("check-in = %d", rec.Code)
	}
	// ⟦[[D-84]], 2026-09-05⟧ THE ASSERTIONS ARE SCOPED TO THE ON-SITE LIST —
	// the check-out form that renders it — not the whole page. ⟦was: three
	// whole-page strings.Contains⟧. D-84 puts a company picker on the gate
	// page that lists EVERY company, so a whole-page scan would now find
	// "Kakivik Asset Management" with nobody on site (the positive arm goes
	// VACUOUS) and "Renamed Holdings" in the picker (the negative arm reds
	// for furniture). The property — the on-site list shows the SNAPSHOT,
	// never the alias target — is unchanged and is now asserted on the
	// surface it is about.
	onSiteList := func(page string) string {
		return sectionBetween(t, page, `action="/gate/checkout"`, "</form>")
	}
	_, before := getGate(t, srv)
	if !strings.Contains(onSiteList(before), "Kakivik Asset Management") {
		t.Fatalf("on-site list does not show the snapshot company before any alias:\n%s", before)
	}

	newCo, err := domain.CreateCompany(st, "site-a", 1, "Renamed Holdings", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	companies, err := domain.Companies(st)
	if err != nil {
		t.Fatal(err)
	}
	var oldCoID string
	for _, c := range companies {
		if c.Name == "Kakivik Asset Management" {
			oldCoID = c.CompanyID
		}
	}
	if oldCoID == "" {
		t.Fatal("could not find the original company id")
	}
	// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ Scope §5's ONE mechanical
	// site (site 19). The deleted reference-merge author is replaced by the
	// entity's own amendment door with SameAs set — the survivor P-3 §5 names.
	// ⭐ THIS STRENGTHENS THE FIXTURE: the old distractor was a dead path, the
	// new one is a LIVE merge edge, so the negative arm below gets harder, not
	// easier. Both assertions hold under any merge mechanism — gate.go:34/:323
	// never reads Companies()/Trades()/a merge map, so the handler is
	// STRUCTURALLY incapable of re-resolving either way.
	// ⚠ THE SUPERSEDED LINE IS PRESERVED VERBATIM in
	// reports/window-build/ALIAS-DELETION-REPAIR.md §3.2, NOT here: it names a
	// symbol W8-9(c-i)'s byte census forbids, so a ⟦was:⟧ line in this file
	// would leave the file an offender and the repair worth nothing.
	if _, err := domain.AmendCompany(st, "site-a", 1, domain.CompanyAmendment{
		CompanyID: oldCoID,
		SameAs:    &newCo.CompanyID,
	}, 0, "default"); err != nil {
		t.Fatalf("amend company (same_as): %v", err)
	}

	_, after := getGate(t, srv)
	if !strings.Contains(onSiteList(after), "Kakivik Asset Management") {
		t.Fatalf("on-site list re-resolved through the alias — history test FAILED:\n%s", after)
	}
	if strings.Contains(onSiteList(after), "Renamed Holdings") {
		t.Fatalf("on-site list shows the alias target instead of the snapshot — history test FAILED:\n%s", after)
	}
}

// TestIncompleteCheckInShowsOnGatePageAndResumeCompletesIt is row 31's
// web-layer proof: simulate the crash by authoring ONLY the dispatch half
// directly (skipping the arrival, as a process death between GateCheckIn's
// two Author calls would), confirm the "incomplete check-ins" section
// surfaces it on the actual page, then confirm the Resume button's route
// completes it.
func TestIncompleteCheckInShowsOnGatePageAndResumeCompletesIt(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	trip, err := domain.DispatchLocalCheckIn(st, "site-a", 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "T99", DriverPersonID: persons[0].PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "T99"), DriverPersonID: persons[0].PersonID,
		OccupantPersonIDs: []string{persons[1].PersonID},
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	_, body := getGate(t, srv)
	if !strings.Contains(body, "Incomplete check-ins") || !strings.Contains(body, "T99") {
		t.Fatalf("crashed check-in is not visible on /gate:\n%s", body)
	}

	rec := postForm(t, srv, "/gate/resume", url.Values{"time_source": {"default"}, "trip_id": {trip.TripID}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("resume = %d: %s", rec.Code, rec.Body.String())
	}

	_, body = getGate(t, srv)
	if strings.Contains(body, "Incomplete check-ins") {
		t.Fatalf("resumed check-in still listed as incomplete:\n%s", body)
	}
	for _, p := range persons[:2] {
		if !strings.Contains(body, p.Name) {
			t.Fatalf("resumed occupant %q not on the on-site list:\n%s", p.Name, body)
		}
	}
}

// --- U2: batch check-out ---------------------------------------------

// checkInCrew checks n people onto site-a in one gate check-in and returns
// their open visit ids, so a test can drive a realistic bulk departure.
func checkInCrew(t *testing.T, srv *Server, st *store.Store, n int) []string {
	t.Helper()
	co, err := domain.CreateCompany(st, "site-a", 1, "Bulk Movement Co", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ DR-W8-2 arity: the company argument is dropped — a trade is a site-wide open set, not scoped to a company. ⟦was: "wg, err := domain.CreateTrade(st, "site-a", 1, co.CompanyID, "Shuttle Crew", 0, "default")"⟧
	wg, err := domain.CreateTrade(st, "site-a", 1, "Shuttle Crew", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	var slots []domain.PersonSlot
	for i := 0; i < n; i++ {
		p, err := domain.CreatePerson(st, "site-a", 1, fmt.Sprintf("Crew %02d", i), &co.CompanyID, &wg.TradeID, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		slots = append(slots, domain.PersonSlot{ExistingID: p.PersonID})
	}
	if _, _, err := domain.GateCheckIn(st, "site-a", 1, domain.GateCheckInInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "SHUTTLE 1", Driver: slots[0], Occupants: slots[1:],"⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "SHUTTLE 1"), Driver: slots[0], Occupants: slots[1:],
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != n {
		t.Fatalf("setup: %d open visits, want %d", len(open), n)
	}
	var ids []string
	for _, v := range open {
		ids = append(ids, v.VisitID)
	}
	return ids
}

// visitIDForCrewName ⟦R-28, team-lead, 2026-09-16⟧: checkInCrew's own
// returned ids are in domain.OpenVisitsAt's order, NOT creation order — it
// reads back through a fresh store query, not the slots this fixture built
// them from, so "Crew 00" is not reliably ids[0] (MEASURED: it moves
// between runs). Existing callers of checkInCrew never cared WHICH id
// belonged to which crew member (Depart-together's own tests tick "all of
// them" or "some arbitrary one"); gateBuildOutbound's own callers do, to
// seed the roster's tick-and-select door with a SPECIFIC named row while
// keeping the rest of a scenario's names typed through the open box — this
// resolves that one lookup by name, the same way the rest of this file
// already resolves identity (PersonByID), never by assuming an order.
func visitIDForCrewName(t *testing.T, st *store.Store, name string) string {
	t.Helper()
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range open {
		p, err := domain.PersonByID(st, v.PersonID)
		if err != nil {
			t.Fatal(err)
		}
		if p != nil && p.Name == name {
			return v.VisitID
		}
	}
	t.Fatalf("no open visit at site-a belongs to %q", name)
	return ""
}

// TestScriptedBatchCheckOutStaysWithinTheRoundTripBudget is the U2 sibling
// of TestScriptedCheckInStaysWithinTheRoundTripBudget above, and it uses
// the same instrument: a real httptest server over loopback, a real
// net/http client that auto-follows the 303 exactly like a browser, and the
// countingHandler wrapping the real mux so EVERY dispatch is counted,
// redirect follows included.
//
// It measures BOTH paths against the same 26-person departure, in the same
// process, so the comparison is not a claim about the past:
//
//   - the pre-existing per-row path (one POST carrying one visit_id, one
//     redirect-follow GET, per person) — design/bulk-movement-v1.md measured
//     this at exactly 52 for N=26, and this test re-measures it;
//   - the U2 batch (one POST carrying 26 visit_ids) — 2, flat, and the
//     assertion is on the BUDGET (<=2), not on the number 2, so a future
//     extra hop fails here.
//
// The batch is a batching of the REQUEST only: the test also asserts one
// visit_checked_out event per person, so per-person accuracy and
// replication semantics are demonstrably untouched.
//
// WHAT THIS DOES NOT MEASURE: clicks and keystrokes. Those were verified by
// hand in a real browser for this chunk (see the build report); nothing here
// claims them.
func TestScriptedBatchCheckOutStaysWithinTheRoundTripBudget(t *testing.T) {
	const crew = 26

	measure := func(t *testing.T, batched bool) (int, *store.Store) {
		t.Helper()
		srv, st, _ := newGateTestServer(t)
		ids := checkInCrew(t, srv, st, crew)

		counted := &countingHandler{inner: srv.Handler()}
		ts := httptest.NewServer(counted)
		defer ts.Close()
		client := ts.Client()
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatal(err)
		}
		client.Jar = jar
		// Login happens at shift handover, not per departure — same
		// exclusion the check-in budget test makes, for the same reason.
		resp, err := client.PostForm(ts.URL+"/login", url.Values{"password": {testLoginPassword}, "next": {"/gate"}})
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		counted.n = 0

		if batched {
			resp, err := client.PostForm(ts.URL+"/gate/checkout", url.Values{"time_source": {"default"}, "visit_ids": ids})
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("batch check-out (after redirect follow) = %d: %s", resp.StatusCode, body)
			}
		} else {
			for _, id := range ids {
				resp, err := client.PostForm(ts.URL+"/gate/checkout", url.Values{"time_source": {"default"}, "visit_id": {id}})
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("per-row check-out of %s = %d", id, resp.StatusCode)
				}
			}
		}
		return counted.n, st
	}

	perRow, stPerRow := measure(t, false)
	batch, stBatch := measure(t, true)

	// Precondition, not a claim: both paths must actually have emptied the
	// site, or the round-trip numbers below are comparing a departure with a
	// no-op.
	for name, st := range map[string]*store.Store{"per-row": stPerRow, "batch": stBatch} {
		open, err := domain.OpenVisitsAt(st, "site-a")
		if err != nil {
			t.Fatal(err)
		}
		if len(open) != 0 {
			t.Fatalf("precondition: %s path left %d people on site — it did not perform the departure being measured", name, len(open))
		}
	}

	if perRow != 2*crew {
		t.Errorf("per-row check-out of %d people = %d round trips, expected %d (one POST + one redirect-follow GET each) — this is the figure bulk-movement-v1.md measured", crew, perRow, 2*crew)
	}
	if batch > 2 {
		t.Errorf("batch check-out of %d people = %d round trips, want <= 2 (one POST + the browser's automatic redirect-follow GET)", crew, batch)
	}

	// One event per person: the request was batched, the events were not.
	checkouts, err := domain.VisitCheckouts(stBatch)
	if err != nil {
		t.Fatal(err)
	}
	if len(checkouts) != crew {
		t.Fatalf("batch authored %d visit_checked_out rows, want %d — one per person, so per-person accuracy and replication are untouched", len(checkouts), crew)
	}

	t.Logf("measured round trips to check out %d people: per-row = %d, batch = %d", crew, perRow, batch)
}

// TestCheckOutRefusesTheWholeBatchWhenOneRowIsStale is the direction this
// batch is allowed to fail in. If any ticked visit is already closed (the
// page was rendered before somebody else checked that person out), NOTHING
// is authored and the refusal names the problem. Refusing leaves people
// marked on site who the operator meant to remove — the muster
// over-reports, which wastes search time but never hides a live person.
// Authoring half a batch and reporting "some of these worked" would leave
// the operator guessing which half.
func TestCheckOutRefusesTheWholeBatchWhenOneRowIsStale(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	ids := checkInCrew(t, srv, st, 4)

	// Somebody else checks one of them out first: the operator's page is now stale.
	if err := domain.CheckOutVisit(st, "site-a", 1, ids[1], nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	before, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 3 {
		t.Fatalf("precondition: %d open visits, want 3 (one already checked out)", len(before))
	}

	rec := postForm(t, srv, "/gate/checkout", url.Values{"time_source": {"default"}, "visit_ids": ids})
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale batch = %d, want 409 with an explanation: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "already checked out") || !strings.Contains(rec.Body.String(), "nothing was recorded") {
		t.Fatalf("refusal does not say what went wrong and that nothing happened: %s", rec.Body.String())
	}
	after, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 3 {
		t.Fatalf("a refused batch authored %d check-out(s) anyway — it must be all or nothing", 3-len(after))
	}
}

// TestSingleVisitCheckOutFieldStillWorks: visit_id is the pre-U2 field and
// keeps its exact meaning, so any deployed muscle memory, bookmark or raw
// POST is unaffected by the batch path.
func TestSingleVisitCheckOutFieldStillWorks(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	ids := checkInCrew(t, srv, st, 3)

	rec := postForm(t, srv, "/gate/checkout", url.Values{"time_source": {"default"}, "visit_id": {ids[0]}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("single-field check-out = %d: %s", rec.Code, rec.Body.String())
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 {
		t.Fatalf("%d open visits after a single check-out, want 2", len(open))
	}
}
