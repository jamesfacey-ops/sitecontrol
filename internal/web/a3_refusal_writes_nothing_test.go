// a3_refusal_writes_nothing_test.go — ⟦A3 / cluster A, 2026-09-29⟧ the
// "refusal that still writes" class on the GATE commit paths: FINDINGS H-4
// (a refused check-out still minted and replicated the vehicle record), H-5
// (a refused check-out still persisted a TICKED person-record change) and
// H-6 (a refused stale-tab Resume still recorded the ticked orientation and
// entered the person). One shape: something was authored BEFORE the door
// finished refusing, and the officer was told "Nothing was recorded".
//
// Every test here reads the store BEFORE and AFTER the refused POST and
// asserts the two readings are identical — watermark, vehicle register,
// trips, open visits, the person's record — never a status code alone. Each
// also asserts its own precondition (the vehicle number is absent, the trip
// really is `arrived`, the company really differs) so a fixture drift cannot
// turn the scenario into one that poses nothing (BUILDER_BRIEF_RULES rule 6),
// and each has a VALID twin that proves the same body, corrected, writes
// everything — so the refusal half cannot be green by the door being dead.
//
// MEASURED on 465deed (before the fix): the three refusal tests FAIL with a
// watermark step and a minted vehicle / written amendment / recorded
// orientation; the valid twins PASS. After the fix all pass.
package web

import (
	"bytes"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// a3Snapshot is one reading of everything a gate check-out can write.
type a3Snapshot struct {
	watermark  int64
	vehicles   int
	trips      int
	openVisits int
	company    string // the watched person's company id ("" = none)
	amendments int    // person_amendment rows
}

func a3Snap(t *testing.T, st *store.Store, site, personID string) a3Snapshot {
	t.Helper()
	var s a3Snapshot
	var err error
	if s.watermark, err = st.Watermark(site, 1); err != nil {
		t.Fatal(err)
	}
	vs, err := domain.Vehicles(st)
	if err != nil {
		t.Fatal(err)
	}
	s.vehicles = len(vs)
	trips, err := domain.Trips(st)
	if err != nil {
		t.Fatal(err)
	}
	s.trips = len(trips)
	open, err := domain.OpenVisitsAt(st, site)
	if err != nil {
		t.Fatal(err)
	}
	s.openVisits = len(open)
	if personID != "" {
		p, err := domain.PersonByID(st, personID)
		if err != nil || p == nil {
			t.Fatalf("person %s: %v %v", personID, p, err)
		}
		if p.CompanyID != nil {
			s.company = *p.CompanyID
		}
	}
	am, err := st.DumpProjection("person_amendment")
	if err != nil {
		t.Fatal(err)
	}
	s.amendments = bytes.Count(am, []byte(`"person_id"`))
	return s
}

// a3VehicleNumbered reports whether a vehicle record carries exactly this
// number.
func a3VehicleNumbered(t *testing.T, st *store.Store, number string) bool {
	t.Helper()
	vs, err := domain.Vehicles(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		if v.Number != nil && *v.Number == number {
			return true
		}
	}
	return false
}

// a3OutboundBody stages two on-site crew in the compact check-out box and
// returns the commit body with vehicle, destination and driver set; the
// caller decides the deadline / override / record-change fields.
func a3OutboundBody(t *testing.T, srv *Server, st *store.Store, vehicle string) url.Values {
	t.Helper()
	_, vals := gateDepartSelected(t, srv, []string{
		visitIDForCrewName(t, st, "Crew 00"), visitIDForCrewName(t, st, "Crew 01"),
	})
	if got := len(vals["occupant_typed"]); got != 2 {
		t.Fatalf("precondition: the compact box carries %d rows after ticking 2", got)
	}
	vals.Set("vehicle", vehicle)
	vals.Set("destination_id", "site-b") // CONTROLLED: a deadline is required
	vals = gateMarkDriver(vals, 0)
	return vals
}

func a3AssertNothingWritten(t *testing.T, before, after a3Snapshot, what string) {
	t.Helper()
	if after != before {
		t.Fatalf("%s: the refusal WROTE — before %+v, after %+v. A refusal that writes is not a refusal: the officer read \"Nothing was recorded\" while a replicated record exists (FINDINGS H-4/H-5/H-6).", what, before, after)
	}
}

// TestA3RefusedCheckOutForBlankDeadlineWritesNoVehicleNoRecordChangeNoTrip
// is H-4 + H-5 in one body (L03 G-069 row "blank", FINDINGS H-5 row b): a
// never-seen vehicle number, a TICKED company change on row 0, and a blank
// deadline toward a controlled destination. Dispatch refuses on the
// deadline; nothing — not the vehicle, not the amendment, not a trip or a
// visit — may be on the log afterwards. Then the same body with a deadline
// writes all three.
func TestA3RefusedCheckOutForBlankDeadlineWritesNoVehicleNoRecordChangeNoTrip(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	checkInCrew(t, srv, st, 3)
	other, err := domain.CreateCompany(st, "site-a", 1, "Frostgate Hauling", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	const vehicle = "A3-NEVER-1"
	vals := a3OutboundBody(t, srv, st, vehicle)
	row0 := vals["occupant_value"][0]
	if row0 == "" {
		t.Fatalf("precondition: row 0 of the compact box carries no person id: %v", vals)
	}
	// The ticked D-84 change: row 0's company picker moved to Frostgate and
	// the row's own "change their record" box ticked (amend_confirm = row
	// index — L13 V2/V4's semantics, FINDINGS H-5).
	vals["occupant_company_id"][0] = other.CompanyID
	vals.Add("amend_confirm", "0")
	vals.Set("deadline_minutes", "")

	before := a3Snap(t, st, "site-a", row0)
	if before.company == other.CompanyID {
		t.Fatalf("precondition: row 0's person already belongs to %s — the change would be a no-op and prove nothing", other.CompanyID)
	}
	if a3VehicleNumbered(t, st, vehicle) {
		t.Fatalf("precondition: %q already exists — a mint could not be told from a reuse", vehicle)
	}

	rec := postForm(t, srv, "/gate/checkout/vehicle", vals)
	if rec.Code != http.StatusConflict {
		t.Fatalf("blank-deadline check-out = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "requires a deadline") {
		t.Fatalf("the refusal is not the deadline one: %s", rec.Body.String())
	}
	after := a3Snap(t, st, "site-a", row0)
	if a3VehicleNumbered(t, st, vehicle) {
		t.Errorf("H-4 REPRODUCED: the refused check-out minted vehicle %q", vehicle)
	}
	if after.company != before.company {
		t.Errorf("H-5 REPRODUCED: the refused check-out changed row 0's company %q -> %q", before.company, after.company)
	}
	a3AssertNothingWritten(t, before, after, "blank deadline")

	// The valid twin: the identical body plus a deadline writes everything.
	vals.Set("deadline_minutes", "60")
	rec = postForm(t, srv, "/gate/checkout/vehicle", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the corrected check-out = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	done := a3Snap(t, st, "site-a", row0)
	if !a3VehicleNumbered(t, st, vehicle) {
		t.Errorf("the valid check-out did not mint vehicle %q", vehicle)
	}
	if done.company != other.CompanyID {
		t.Errorf("the valid check-out did not apply the ticked company change: %q", done.company)
	}
	if done.trips != before.trips+1 || done.openVisits != before.openVisits-2 {
		t.Errorf("the valid check-out: trips %d->%d (want +1), open visits %d->%d (want -2)", before.trips, done.trips, before.openVisits, done.openVisits)
	}
	if done.amendments != before.amendments+1 {
		t.Errorf("the valid check-out: person_amendment rows %d->%d, want +1", before.amendments, done.amendments)
	}
	// Replay determinism: the commit's events, applied at a fresh replica in
	// one batch, fold to the byte-identical dump.
	a3AssertReplaysIdentically(t, st, "site-a")
}

// TestA3StaleTabResumeRefusalRecordsNoOrientationAndEntersNobody is L03
// G-121 / FINDINGS H-6. An incomplete check-in (the row-31 crash shape) at
// an orientation post is resumed from tab A with the un-oriented occupant
// UNTICKED — held out, trip arrived. Tab B, a stale copy of the same page,
// ticks them and resumes: refused, "recording it again enters nobody" — and
// that must be true: no orientation row, no visit, nothing on the log.
func TestA3StaleTabResumeRefusalRecordsNoOrientationAndEntersNobody(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	trip, err := domain.DispatchLocalCheckIn(w.st, "site-b", 1, domain.DispatchInput{
		VehicleID:         w7SeedVehicleID(t, w.st, "site-b", "TRK-A3"),
		DriverPersonID:    w.driver.PersonID,
		OccupantPersonIDs: []string{w.uma.PersonID},
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// Tab A: resume, nobody ticked -> Uma held out, trip arrived.
	rec := postForm(t, w.srv, "/gate/resume", url.Values{"trip_id": {trip.TripID}, "time_source": {"default"}})
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), notEnteredQueryKey+"=") {
		t.Fatalf("tab A resume = %d %q, want 303 with a hold-out", rec.Code, rec.Header().Get("Location"))
	}
	state, err := domain.TripStateByID(w.st, trip.TripID)
	if err != nil || state == nil || state.State != domain.TripStateArrived {
		t.Fatalf("precondition: trip state after tab A = %+v (%v), want arrived", state, err)
	}
	if held, _ := domain.OrientationHeld(w.st, w.uma.PersonID, "site-b"); held != nil {
		t.Fatalf("precondition: Uma already holds site-b's orientation")
	}
	before := a3Snap(t, w.st, "site-b", "")
	if orientBlockOpenPersonIDs(t, w.st)[w.uma.PersonID] {
		t.Fatalf("precondition: Uma is on site after being held out")
	}

	// Tab B: the stale page, Uma ticked.
	rec = postForm(t, w.srv, "/gate/resume", url.Values{
		"trip_id": {trip.TripID}, "time_source": {"default"},
		"add_orientation_person": {w.uma.PersonID},
	})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "enters nobody") {
		t.Fatalf("stale-tab resume = %d, want 409 \"enters nobody\": %s", rec.Code, rec.Body.String())
	}
	if held, err := domain.OrientationHeld(w.st, w.uma.PersonID, "site-b"); err != nil || held != nil {
		t.Errorf("H-6 REPRODUCED: the refused resume recorded site-b's orientation for Uma (%+v, %v)", held, err)
	}
	if orientBlockOpenPersonIDs(t, w.st)[w.uma.PersonID] {
		t.Errorf("H-6 REPRODUCED: the refused resume ENTERED Uma (open visit) while saying it enters nobody")
	}
	after := a3Snap(t, w.st, "site-b", "")
	a3AssertNothingWritten(t, before, after, "stale-tab resume")
	a3AssertReplaysIdentically(t, w.st, "site-b")
}

// a3AssertReplaysIdentically replays this site's whole stream into a fresh
// replica in one batch and requires the byte-identical projection dump —
// the property every post's replication relies on, asserted here because
// the fix authors a commit's events together and they must fold the same
// whether authored one by one or applied as a batch.
func a3AssertReplaysIdentically(t *testing.T, st *store.Store, site string) {
	t.Helper()
	events, err := st.Events(site, 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatalf("precondition: no events on %s/1 to replay", site)
	}
	replica, err := store.Open(t.TempDir() + "/replica.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { replica.Close() })
	if err := domain.Register(replica); err != nil {
		t.Fatal(err)
	}
	results, err := replica.ApplyBatch(events)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(results) != len(events) {
		t.Fatalf("replay applied %d of %d", len(results), len(events))
	}
	want, err := st.DumpProjections()
	if err != nil {
		t.Fatal(err)
	}
	got, err := replica.DumpProjections()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("the replica's dump diverges from the author's after replaying %d events — replay is not deterministic", len(events))
	}
}
