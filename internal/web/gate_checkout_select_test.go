// gate_checkout_select_test.go — "Depart together" (James, 2026-09-11: "tick
// people on the list to be checked out all at once ... then name the
// vehicle"), and the follow-up ruling that a check-out must not be blocked
// on a vehicle number the officer does not have.
//
// THE SHAPE OF EVERY TEST HERE: checkInCrew puts real people on site with
// real open visits (never hand-built), POST /gate/checkout/select stages
// the ticked ones onto the outbound accumulated list exactly as the roster
// button does, gateFormValues scrapes THAT rendered page (the same
// instrument gate_form_test.go uses throughout this package — a test that
// hand-built the commit body would prove the handler works and say nothing
// about whether the page this route renders can actually be submitted), and
// the final POST /gate/checkout/vehicle is the SAME existing commit route
// every other departure in this file already exercises. Nothing here
// invents a second departure path.
package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// gateDepartSelected drives the whole "Depart together" step: ticks the
// given visit ids on the roster's own form and returns the outbound
// accumulated-list values the resulting page carries, ready for the caller
// to add destination_id / driver_index / vehicle (or vehicle_unknown) and
// post to /gate/checkout/vehicle.
func gateDepartSelected(t *testing.T, srv *Server, visitIDs []string) (string, url.Values) {
	t.Helper()
	form := url.Values{}
	for _, id := range visitIDs {
		form.Add("visit_ids", id)
	}
	rec := postForm(t, srv, "/gate/checkout/select", form)
	if rec.Code != http.StatusOK {
		t.Fatalf("/gate/checkout/select = %d: %s", rec.Code, rec.Body.String())
	}
	page := rec.Body.String()
	return page, gateFormValues(t, page, formIDFor(gateOutbound))
}

// TestDepartTogetherOneCommitDepartsTheWholeTickedGroup is the done-when for
// the feature: several ticked, one vehicle named, one commit, everybody
// leaves together and the on-site count drops by exactly that many.
func TestDepartTogetherOneCommitDepartsTheWholeTickedGroup(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	visitIDs := checkInCrew(t, srv, st, 3)

	tripsBefore, err := domain.Trips(st)
	if err != nil {
		t.Fatal(err)
	}

	_, vals := gateDepartSelected(t, srv, visitIDs)
	if got := len(vals["occupant_typed"]); got != 3 {
		t.Fatalf("precondition: the outbound form carries %d rows after ticking 3 — Depart Together did not stage them all: %v", got, vals)
	}
	vals.Set("vehicle", "TRK-14")
	vals.Set("destination_id", "conneco") // uncontrolled: no deadline required
	vals = gateMarkDriver(vals, 0)

	rec := postForm(t, srv, "/gate/checkout/vehicle", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the commit = %d, want 303: %s", rec.Code, rec.Body.String())
	}

	// D-4, asserted directly for this consolidated route (checkout-dedupe,
	// 2026-09-11 — this is the route the tick-off-the-roster group check-out
	// now lands on after removing the duplicate /gate/checkout/roster door):
	// a named-vehicle group departure staged via "Depart together" must not
	// read as a crashed check-in. assertIncompletenessQuiet is the same
	// helper local_departure_test.go's TestARoutineDepartureNeverReadsAsACrashedCheckIn
	// proves sensitive to a real one (its own CONTROL fires all three checks
	// against a genuine crashed dispatch) — this reuses that proven sensitivity
	// against THIS route rather than re-deriving it.
	assertIncompletenessQuiet(t, srv, "after the whole ticked group departed")

	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 0 {
		t.Fatalf("%d people still show on site after the whole ticked group departed: %+v", len(open), open)
	}

	// ONE trip, not three — "not one departure per person".
	tripsAfter, err := domain.Trips(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(tripsAfter)-len(tripsBefore) != 1 {
		t.Fatalf("the group's departure authored %d new trip(s), want exactly 1: before=%d after=%d", len(tripsAfter)-len(tripsBefore), len(tripsBefore), len(tripsAfter))
	}

	_, page := getGate(t, srv)
	if !strings.Contains(page, "On site now (0)") {
		t.Errorf("/gate does not show the headcount dropping to 0 after the group departed:\n%s", page)
	}
}

// TestDepartTogetherWithNothingTickedAddsNobodyAndAuthorsNothing.
func TestDepartTogetherWithNothingTickedAddsNobodyAndAuthorsNothing(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	before, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	rec := postForm(t, srv, "/gate/checkout/select", url.Values{})
	if rec.Code != http.StatusOK {
		t.Fatalf("= %d, want 200 (a fumble, not an error): %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Nothing was ticked") {
		t.Errorf("an empty tick set is not explained to the officer:\n%s", rec.Body.String())
	}
	vals := gateFormValues(t, rec.Body.String(), formIDFor(gateOutbound))
	if got := len(vals["occupant_typed"]); got != 0 {
		t.Fatalf("an empty tick set staged %d row(s) anyway", got)
	}
	after, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("posting no ticks moved the watermark %d -> %d", before, after)
	}
}

// TestDepartTogetherScopesToThisPostsOwnOpenVisitsRightNow is the "a posted
// id must never be trusted beyond that" guard, on the SAME footing as the
// orientation tick's own scoping. A visit id that is not, right now, an open
// visit at THIS site — because it never existed, or it belongs to a visit
// that is not open here — must never turn into an accumulated row: that
// would let a hand-crafted POST smuggle an arbitrary person id onto a
// departing vehicle with no on-site check at all (the real check-and-refuse
// still lives at the /gate/checkout/vehicle commit, but this route's own job
// is to never manufacture a row the commit would have to catch).
func TestDepartTogetherScopesToThisPostsOwnOpenVisitsRightNow(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	real := checkInCrew(t, srv, st, 1)

	notAVisit := "visit-does-not-exist"
	page, vals := gateDepartSelected(t, srv, append(append([]string{}, real...), notAVisit))

	if got := len(vals["occupant_typed"]); got != 1 {
		t.Fatalf("the real, valid visit was not added despite the bogus id riding beside it: %d row(s), %v", got, vals)
	}
	msg := vehicleError(t, page, gateOutbound)
	if !strings.Contains(msg, notAVisit) {
		t.Errorf("the officer is not told BY NAME which tick could not be added — a silent drop is exactly what the brief forbids: %q", msg)
	}

	// The domain-level backstop still holds even if this route's own
	// scoping were ever bypassed: a hand-crafted commit naming an off-site
	// person is refused, not authored.
	absent, err := domain.CreatePerson(st, "site-a", 1, "Never Here", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	vals.Add("occupant_index", strconv.Itoa(len(vals["occupant_typed"])))
	vals.Add("occupant_kind", "existing")
	vals.Add("occupant_value", absent.PersonID)
	vals.Add("occupant_typed", absent.Name)
	vals.Add("occupant_company_id", "")
	vals.Add("occupant_company", "")
	vals.Add("occupant_trade_id", "")
	vals.Add("occupant_trade", "")
	// ⟦gate-checkin-spec, 2026-09-13⟧ occupant_position/occupant_phone/
	// occupant_last/occupant_first are FOUR more parallel arrays every row
	// now emits (W8-23(c)) — checkIndexAlignment refuses a body where these
	// carry fewer entries than occupant_index, and this hand-built second
	// row would otherwise trip that guard with the WRONG refusal message
	// before ever reaching the domain-level one this test is about.
	vals.Add("occupant_position", "")
	vals.Add("occupant_phone", "")
	vals.Add("occupant_last", "")
	vals.Add("occupant_first", "")
	// ⟦FIX8 option C⟧ occupant_time is one more parallel array every row
	// emits — same reason as the four above.
	vals.Add("occupant_time", "")
	vals.Set("destination_id", "conneco")
	vals = gateMarkDriver(vals, 0)
	rec := postForm(t, srv, "/gate/checkout/vehicle", vals)
	if rec.Code != http.StatusConflict {
		t.Fatalf("a commit naming a person with no open visit here = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(vehicleError(t, rec.Body.String(), gateOutbound), "Never Here") {
		t.Errorf("the domain-level refusal does not name the absent person")
	}
}

// TestDepartTogetherNeverTicksAVisitOpenAtAnotherPost is FALSIFIABILITY-2.md's
// M-CS: the sibling test above only ever poses a NONEXISTENT visit id or a
// person with no open visit anywhere — never a visit id that is REAL and
// genuinely OPEN, just at a different site. This post's own read must be
// scoped to ITS OWN open visits (domain.OpenVisitsAt(this site)), never all
// sites' (domain.Visits filtered by is_open) — the latter would let an
// officer at site-a tick, and stage for departure, a person who is
// currently on site-b's own muster and has never been near site-a's gate.
func TestDepartTogetherNeverTicksAVisitOpenAtAnotherPost(t *testing.T) {
	srv, st, _ := newGateTestServer(t) // Cfg.SiteID = "site-a"
	real := checkInCrew(t, srv, st, 1)

	// A REAL, currently open visit — but at site-b, authored directly
	// through the domain edge on the SAME underlying store (the shape a
	// fully-synced peer's own database would carry).
	fred, err := domain.CreatePerson(st, "site-b", 1, "Faraway Fred", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := domain.GateCheckIn(st, "site-b", 1, domain.GateCheckInInput{
		VehicleID: w7SeedVehicleID(t, st, "site-b", "PAD TRUCK"),
		Driver:    domain.PersonSlot{ExistingID: fred.PersonID},
	}, 0, "default"); err != nil {
		t.Fatalf("setup: GateCheckIn at site-b: %v", err)
	}
	bOpen, err := domain.OpenVisitsAt(st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(bOpen) != 1 {
		t.Fatalf("setup: site-b has %d open visits, want 1: %+v", len(bOpen), bOpen)
	}
	foreignVisitID := bOpen[0].VisitID

	page, vals := gateDepartSelected(t, srv, append(append([]string{}, real...), foreignVisitID))

	if got := len(vals["occupant_typed"]); got != 1 {
		t.Fatalf("BROKEN: %d row(s) staged, want exactly 1 (site-a's real visit) — a visit open at site-b was staged onto a site-a departure: %v", got, vals)
	}
	msg := vehicleError(t, page, gateOutbound)
	if !strings.Contains(msg, foreignVisitID) {
		t.Errorf("the officer is not told BY (visit) ID which tick could not be added — a visit belonging to another site names nobody here: %q", msg)
	}
	if strings.Contains(msg, "Faraway Fred") {
		t.Errorf("site-a's refusal names a site-b person by name — this post must never resolve identity for a visit it does not own: %q", msg)
	}

	// site-b's own muster must be completely undisturbed by a site-a
	// checkout-select call that merely READ the ticked id.
	stillOpen, err := domain.OpenVisitsAt(st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(stillOpen) != 1 || stillOpen[0].VisitID != foreignVisitID {
		t.Fatalf("BROKEN: site-b's open visit was disturbed: before=%+v after=%+v", bOpen, stillOpen)
	}
}

// TestDepartTogetherNamesSomebodyWhoLeftBetweenTickAndSubmit is the "partial
// failure must not lie" case: one of two ticked visits closes (a correction,
// a race, a second officer) between the tick and this route's own read.
// staletick.go's own lesson, applied here — the officer must never be left
// believing everyone they ticked was added.
func TestDepartTogetherNamesSomebodyWhoLeftBetweenTickAndSubmit(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	visitIDs := checkInCrew(t, srv, st, 2)

	// domain.OpenVisitsAt (behind checkInCrew) makes no ordering promise, so
	// this resolves which visit belongs to which name explicitly rather than
	// assuming visitIDs[0] is any particular person — the property under
	// test does not depend on which of the two closes early.
	nameOf := map[string]string{}
	for _, v := range mustOpenVisits(t, st, "site-a") {
		p, err := domain.PersonByID(st, v.PersonID)
		if err != nil || p == nil {
			t.Fatalf("precondition: could not resolve visit %s to a person", v.VisitID)
		}
		nameOf[v.VisitID] = p.Name
	}
	closing, staying := visitIDs[0], visitIDs[1]
	closingName, stayingName := nameOf[closing], nameOf[staying]

	// closingName's visit closes out from under the tick — e.g. another
	// officer used the correction picker in the seconds before this one
	// submitted.
	if _, err := domain.CheckOutVisits(st, "site-a", 1, []string{closing}, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}

	page, vals := gateDepartSelected(t, srv, visitIDs)
	if got := len(vals["occupant_typed"]); got != 1 {
		t.Fatalf("the still-open visit was not added: %d row(s)", got)
	}
	if vals["occupant_typed"][0] != stayingName {
		t.Errorf("the wrong person was staged: got %v, want %q", vals["occupant_typed"], stayingName)
	}
	msg := vehicleError(t, page, gateOutbound)
	if !strings.Contains(msg, closingName) {
		t.Errorf("the officer is not told BY NAME that %s could not be added: %q", closingName, msg)
	}
	if !strings.Contains(msg, "not added") {
		t.Errorf("the notice does not say the missing tick was not added: %q", msg)
	}
}

// mustOpenVisits is a thin, t.Fatal-on-error wrapper so the ordering note
// above reads as one call rather than three lines of error plumbing.
func mustOpenVisits(t *testing.T, st *store.Store, siteID string) []domain.Visit {
	t.Helper()
	open, err := domain.OpenVisitsAt(st, siteID)
	if err != nil {
		t.Fatal(err)
	}
	return open
}

// TestVehicleUnknownDepartsWithoutBeingMistakenForACrashedCheckIn is James's
// correction (2026-09-11): "vehicle should not be a reason to not check
// someone out". It mirrors TestAnOutboundVehicleNeverLooksLikeACrashedCheckIn
// (gate_direction_test.go) exactly, over the unknown-vehicle path instead of
// a typed number, because the D-4 property under test — a destination
// always accompanies the trip, so it can never read as the degenerate
// no-destination shape IncompleteCheckIns selects — does not care what the
// vehicle was.
func TestVehicleUnknownDepartsWithoutBeingMistakenForACrashedCheckIn(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	visitIDs := checkInCrew(t, srv, st, 2)

	_, vals := gateDepartSelected(t, srv, visitIDs)
	vals.Set("vehicle_unknown", "1")
	vals.Set("destination_id", "conneco")
	vals = gateMarkDriver(vals, 0)

	rec := postForm(t, srv, "/gate/checkout/vehicle", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("an unknown-vehicle departure = %d, want 303: %s", rec.Code, rec.Body.String())
	}

	inc, err := domain.IncompleteCheckIns(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(inc) != 0 {
		t.Fatalf("an unknown-vehicle departure reads as a crashed check-in: %+v — /gate would offer to Resume it and put the departed crew back on the muster in one click", inc)
	}
	_, page := getGate(t, srv)
	if strings.Contains(page, "Incomplete check-ins") {
		t.Error("/gate offers to resume an unknown-vehicle departure as if it were a crashed check-in")
	}
	musterRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(musterRec, httptest.NewRequest("GET", "/muster", nil))
	if strings.Contains(musterRec.Body.String(), "MAY BE INCOMPLETE") {
		t.Error("the muster reads MAY BE INCOMPLETE after an unknown-vehicle departure")
	}

	// ⟦SchemaVersion 13, R-17, 2026-09-11⟧ EXPLICIT, NOT A SILENT BLANK —
	// but the schema path makes it explicit a different way than the
	// retired stopgap did. ⟦was: this block asserted VehicleID != "" and
	// looked up a MINTED vehicle record with Role "Vehicle unknown"⟧. R-17
	// removes the mint entirely: the fact now lives directly on the trip
	// (VehicleUnidentified), vehicle_id stays "" (never a record, never a
	// correlation key another trip could collide with), and the frozen
	// vehicle_entered snapshot stays nil exactly as a role-only vehicle's
	// always did (DR-W7-15/[[D-11]]: the absence IS the empty value; a
	// stand-in belongs to a render helper, never to this frozen field).
	trips, err := domain.Trips(st)
	if err != nil {
		t.Fatal(err)
	}
	var last domain.Trip
	for _, tr := range trips {
		if tr.DestinationUncontrolled != nil && *tr.DestinationUncontrolled == "conneco" {
			last = tr
		}
	}
	if last.TripID == "" {
		t.Fatalf("could not find the departure trip to conneco among %+v", trips)
	}
	if last.VehicleEntered != nil {
		t.Errorf("an unidentified vehicle's frozen snapshot is %q, want nil — DR-W7-15 says the absence IS the empty value; a text stand-in belongs to a render helper, never to this frozen field", *last.VehicleEntered)
	}
	if !last.VehicleIsUnidentified() {
		t.Fatal("the unknown-vehicle departure does not carry VehicleUnidentified")
	}
	if last.VehicleID != "" {
		t.Errorf("the unidentified trip carries a non-empty vehicle_id %q — R-17's schema path never names a record for this movement", last.VehicleID)
	}
	// Explicit on the RENDERED sheet too — the muster must SAY the vehicle
	// was not identified, never render the same blank a role-only
	// vehicle's absent number already produces (that ambiguity is the
	// precise failure R-17 names).
	if !strings.Contains(musterRec.Body.String(), domain.VehicleUnidentifiedText) {
		t.Errorf("the muster does not say %q for an unidentified-vehicle departure:\n%s", domain.VehicleUnidentifiedText, musterRec.Body.String())
	}

	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 0 {
		t.Fatalf("%d people still show on site after an unknown-vehicle departure — the crew must leave the roster exactly as a numbered departure would: %+v", len(open), open)
	}
}

// TestVehicleUnknownDeparturesMintNoVehicleRecordAtAll: R-17's schema path
// removes the need to mint a vehicle record for an unidentified departure at
// all — ⟦was: TestVehicleUnknownEachDepartureMintsItsOwnDistinctRecord,
// which asserted the OPPOSITE: that two separate unknown-vehicle departures
// must never share one MINTED vehicle id, because the retired stopgap
// (vehicleIDForUnknown, vehicle_comint.go) minted a fresh, distinct record
// per departure and a shared one would have recreated the "walk-in"
// collision class under a new spelling⟧. That collision class cannot recur
// under the schema path AT ALL, because nothing is ever minted for this
// fact any more — proved here the strongest available way: MANY
// unidentified departures, and domain.Vehicles() still returns exactly the
// count it started with (this is also R-17's accumulation criterion: the
// admin vehicle-merge screen must not fill with indistinguishable "Vehicle
// unknown" rows, one per departure, forever).
func TestVehicleUnknownDeparturesMintNoVehicleRecordAtAll(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	visits := checkInCrew(t, srv, st, 2)

	before, err := domain.Vehicles(st)
	if err != nil {
		t.Fatal(err)
	}

	depart := func(visitID string) {
		_, vals := gateDepartSelected(t, srv, []string{visitID})
		vals.Set("vehicle_unknown", "1")
		vals.Set("destination_id", "conneco")
		vals = gateMarkDriver(vals, 0)
		rec := postForm(t, srv, "/gate/checkout/vehicle", vals)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("departure = %d: %s", rec.Code, rec.Body.String())
		}
	}
	depart(visits[0])
	depart(visits[1])

	after, err := domain.Vehicles(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("two unidentified-vehicle departures changed the vehicle count %d -> %d — R-17's schema path should mint nothing at all: %v -> %v", len(before), len(after), before, after)
	}

	trips, err := domain.Trips(st)
	if err != nil {
		t.Fatal(err)
	}
	unidentifiedCount := 0
	for _, tr := range trips {
		if tr.DestinationUncontrolled != nil && *tr.DestinationUncontrolled == "conneco" {
			if !tr.VehicleIsUnidentified() || tr.VehicleID != "" {
				t.Errorf("conneco departure %s: VehicleIsUnidentified=%v VehicleID=%q, want true/\"\"", tr.TripID, tr.VehicleIsUnidentified(), tr.VehicleID)
			}
			unidentifiedCount++
		}
	}
	if unidentifiedCount != 2 {
		t.Fatalf("found %d unidentified departure(s) to conneco, want 2", unidentifiedCount)
	}
}
