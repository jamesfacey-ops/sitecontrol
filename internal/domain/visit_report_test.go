// visit_report_test.go — witnesses for the analytics MVP (visit_report.go).
//
// Every scenario authors REAL events through the production doors
// (DispatchLocalCheckIn + RecordExpectedArrival is the gate check-in,
// CheckOutVisit the sign-out, Dispatch the departure) under an injected
// store.Clock, so the report is exercised over the same log shape a post
// writes, never over hand-built rows. The three closers, the labels, the
// merge fold and the cross-site read are each asserted on a row the fixture
// produced, and the summary is asserted alongside so an empty run can never
// pass as a full one.
package domain

import (
	"bytes"
	"encoding/csv"
	"reflect"
	"strconv"
	"testing"
	"time"

	"sitecontrol/internal/store"
)

// vrFixedClock injects a settable authoring clock in the given trust state.
// The returned func moves it; Author reads it exactly once per event.
func vrFixedClock(st *store.Store, state store.ClockState) func(ms int64) {
	var now int64
	st.Clock = func() store.ClockReading {
		return store.ClockReading{WallMS: now, MaxErrorUS: 500, State: state}
	}
	return func(ms int64) { now = ms }
}

// vrAt is an instant on the fixture's day, UTC ms.
func vrAt(day, hour, min int) int64 {
	return time.Date(2026, 9, 9+day, hour, min, 0, 0, time.UTC).UnixMilli()
}

// vrRoster seeds a company, a trade and n named persons at siteID.
func vrRoster(t *testing.T, st *store.Store, siteID string, names ...string) []Person {
	t.Helper()
	co, err := CreateCompany(st, siteID, 1, "Kakivik Asset Management", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	tr, err := CreateTrade(st, siteID, 1, "Inspection", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	var out []Person
	for _, n := range names {
		p, err := CreatePerson(st, siteID, 1, n, &co.CompanyID, &tr.TradeID, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// vrCheckIn drives the ordinary gate check-in at siteID: the degenerate local
// trip plus the same-site expected arrival, one visit per occupant.
func vrCheckIn(t *testing.T, st *store.Store, siteID, vehicleID string, driver string, occupants ...string) {
	t.Helper()
	trip, err := DispatchLocalCheckIn(st, siteID, 1, DispatchInput{
		VehicleID: vehicleID, DriverPersonID: driver, OccupantPersonIDs: occupants,
	}, 0, "default")
	if err != nil {
		t.Fatalf("check-in dispatch: %v", err)
	}
	if _, err := RecordExpectedArrival(st, siteID, 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatalf("check-in arrival: %v", err)
	}
}

// vrVisitOf finds the one visit for personID at siteID in the given open
// state; the fixtures never create two.
func vrVisitOf(t *testing.T, st *store.Store, siteID, personID string, open bool) Visit {
	t.Helper()
	all, err := Visits(st)
	if err != nil {
		t.Fatal(err)
	}
	var found []Visit
	for _, v := range all {
		if v.SiteID == siteID && v.PersonID == personID && (v.IsOpen == 1) == open {
			found = append(found, v)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one visit for %s at %s (open=%v), got %d", personID, siteID, open, len(found))
	}
	return found[0]
}

func vrByVisitID(recs []VisitRecord) map[string]VisitRecord {
	m := map[string]VisitRecord{}
	for _, r := range recs {
		m[r.VisitID] = r
	}
	return m
}

func vrMinutes(t *testing.T, what string, got *int64, want int64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: want %d minutes, got nil", what, want)
	}
	if *got != want {
		t.Fatalf("%s: want %d minutes, got %d", what, want, *got)
	}
}

// TestVisitReportClassifiesAllClosersAndOpen — the core witness. Four
// people check in together at 10:00 under a synced clock. Ann signs out at
// 12:00 (checked_out, 120 min). Bob is dispatched to site-b at 13:30
// (dispatched, 210 min). Cal never signs out and walks back through the gate
// next morning at 09:00: the OLD visit reads re_checkin — the gate-shaped
// missed sign-out, closed by the destination-less dispatch half of the
// check-in — with NO on-site duration and a 1380-minute record_open upper
// bound. Dee never signs out either and turns up at 09:30 on a truck from
// site-b nobody dispatched: the OLD visit reads next_arrival — the
// arrival-shaped missed sign-out. Both new visits read open. Every end under
// this clock is `observed`.
//
// ⚠ This test is what caught the first draft's error: with Cal's return
// driven through the real gate door, the closer is the DISPATCH, and a
// report that reads every dispatch as a departure calls a missed sign-out
// "dispatched".
func TestVisitReportClassifiesAllClosersAndOpen(t *testing.T) {
	st := newDomainTestStore(t)
	setClock := vrFixedClock(st, store.ClockSynced)

	setClock(vrAt(0, 10, 0))
	ps := vrRoster(t, st, "site-a", "Ann Arrival", "Bob Bus", "Cal Camper", "Dee Drifter")
	ann, bob, cal, dee := ps[0].PersonID, ps[1].PersonID, ps[2].PersonID, ps[3].PersonID
	truck := w7SeedVehicleID(t, st, "site-a", "TRUCK 1")
	vrCheckIn(t, st, "site-a", truck, ann, bob, cal, dee)
	vAnn := vrVisitOf(t, st, "site-a", ann, true)
	vBob := vrVisitOf(t, st, "site-a", bob, true)
	vCalOld := vrVisitOf(t, st, "site-a", cal, true)
	vDeeOld := vrVisitOf(t, st, "site-a", dee, true)

	setClock(vrAt(0, 12, 0))
	if err := CheckOutVisit(st, "site-a", 1, vAnn.VisitID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}

	setClock(vrAt(0, 13, 30))
	dest, deadline := "site-b", int64(60)
	if _, err := Dispatch(st, "site-a", 1, DispatchInput{
		VehicleID: truck, DriverPersonID: bob, DestinationSite: &dest, DeadlineMinutes: &deadline,
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}

	setClock(vrAt(1, 9, 0))
	vrCheckIn(t, st, "site-a", truck, cal)
	vCalNew := vrVisitOf(t, st, "site-a", cal, true)

	setClock(vrAt(1, 9, 30))
	if _, err := RecordUnsolicitedArrival(st, "site-a", 1, truck, "site-b", []string{dee}, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	vDeeNew := vrVisitOf(t, st, "site-a", dee, true)

	recs, sum, err := VisitReport(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 6 {
		t.Fatalf("want 6 visit rows, got %d: %+v", len(recs), recs)
	}
	by := vrByVisitID(recs)

	a := by[vAnn.VisitID]
	if a.ClosedBy != ClosedByCheckout {
		t.Fatalf("Ann: want checked_out, got %q", a.ClosedBy)
	}
	vrMinutes(t, "Ann on_site", a.OnSiteMinutes, 120)
	vrMinutes(t, "Ann record_open", a.RecordOpenMinutes, 120)
	if a.ArrivalLabel != "observed" || a.CloseLabel != "observed" || a.DurationLabel != "observed" {
		t.Fatalf("Ann labels: arrival %q close %q duration %q, want observed×3", a.ArrivalLabel, a.CloseLabel, a.DurationLabel)
	}
	if a.DurationBucket != "under 8h" {
		t.Fatalf("Ann bucket: %q", a.DurationBucket)
	}
	if a.ArrivedAtMS != vrAt(0, 10, 0) || a.ClosedAtMS != vrAt(0, 12, 0) {
		t.Fatalf("Ann instants: arrived %d closed %d", a.ArrivedAtMS, a.ClosedAtMS)
	}
	if a.PersonName != "Ann Arrival" || a.PersonID != ann || a.VisitPersonID != ann {
		t.Fatalf("Ann identity: %+v", a)
	}

	b := by[vBob.VisitID]
	if b.ClosedBy != ClosedByDispatch {
		t.Fatalf("Bob: want dispatched, got %q", b.ClosedBy)
	}
	vrMinutes(t, "Bob on_site", b.OnSiteMinutes, 210)
	if b.DurationLabel != "observed" {
		t.Fatalf("Bob duration label %q", b.DurationLabel)
	}

	c := by[vCalOld.VisitID]
	if c.ClosedBy != ClosedByReCheckin {
		t.Fatalf("Cal's first visit: want re_checkin (the gate-shaped missed sign-out), got %q", c.ClosedBy)
	}
	if c.ClosedBy.DepartureRecorded() || c.OnSiteMinutes != nil {
		t.Fatalf("Cal's missed sign-out must carry NO on-site duration (nothing recorded a departure), got %+v", c)
	}
	vrMinutes(t, "Cal record_open upper bound", c.RecordOpenMinutes, 23*60)
	if c.DurationBucket != "" {
		t.Fatalf("Cal's missed sign-out must not be bucketed, got %q", c.DurationBucket)
	}
	if c.CloseKey.Seq == 0 || c.CloseLabel != "observed" {
		t.Fatalf("Cal close: key %v label %q", c.CloseKey, c.CloseLabel)
	}

	d := by[vDeeOld.VisitID]
	if d.ClosedBy != ClosedByNextArrival {
		t.Fatalf("Dee's first visit: want next_arrival (the arrival-shaped missed sign-out), got %q", d.ClosedBy)
	}
	if d.ClosedBy.DepartureRecorded() || d.OnSiteMinutes != nil || d.DurationBucket != "" {
		t.Fatalf("Dee's missed sign-out must carry no on-site duration and no bucket: %+v", d)
	}
	vrMinutes(t, "Dee record_open upper bound", d.RecordOpenMinutes, 23*60+30)

	for _, nv := range []VisitRecord{by[vCalNew.VisitID], by[vDeeNew.VisitID]} {
		if nv.ClosedBy != VisitStillOpen || nv.OnSiteMinutes != nil || nv.RecordOpenMinutes != nil || nv.CloseLabel != "" || nv.DurationLabel != "" {
			t.Fatalf("a new visit must read open with no close data: %+v", nv)
		}
	}

	want := VisitReportSummary{Visits: 6, Open: 2, CheckedOut: 1, Dispatched: 1, ReCheckin: 1, NextArrival: 1,
		DistinctVisitPersonIDs: 4, DistinctPersons: 4,
		// Run with nil site types (R-4/R-5): both returns are UNCLASSIFIED —
		// neither failures nor exempt — and site-a is named as untyped.
		UnclassifiedReturns: 2, UntypedSites: []string{"site-a"},
		Streams: []StreamMark{{SiteID: "site-a", NodeEpoch: 1, AppliedSeq: 0}}}
	got := sum
	if len(got.Streams) != 1 || got.Streams[0].SiteID != "site-a" || got.Streams[0].AppliedSeq < 10 {
		t.Fatalf("coverage: want one site-a stream with a real watermark, got %+v", got.Streams)
	}
	want.Streams, got.Streams = nil, nil
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("summary:\n got %+v\nwant %+v", got, want)
	}
}

// TestVisitReportLabelsClaimedAndCorrectedEnds — an officer-entered sign-out
// time is a CLAIM and the duration uses it; a standing occurred_at_corrected
// on that sign-out wins (claimed-corrected) and the duration follows the
// corrected instant. The arrival end stays observed throughout.
func TestVisitReportLabelsClaimedAndCorrectedEnds(t *testing.T) {
	st := newDomainTestStore(t)
	setClock := vrFixedClock(st, store.ClockSynced)

	setClock(vrAt(0, 9, 0))
	ps := vrRoster(t, st, "site-a", "Eve Entered")
	truck := w7SeedVehicleID(t, st, "site-a", "TRUCK 2")
	vrCheckIn(t, st, "site-a", truck, ps[0].PersonID)
	v := vrVisitOf(t, st, "site-a", ps[0].PersonID, true)

	// Recorded at 15:00; the officer typed 14:00 as the actual departure.
	setClock(vrAt(0, 15, 0))
	if err := CheckOutVisit(st, "site-a", 1, v.VisitID, nil, vrAt(0, 14, 0), "entered"); err != nil {
		t.Fatal(err)
	}
	recs, _, err := VisitReport(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := vrByVisitID(recs)[v.VisitID]
	if r.ArrivalLabel != "observed" || r.CloseLabel != "claimed" || r.DurationLabel != "claimed" {
		t.Fatalf("labels before correction: arrival %q close %q duration %q", r.ArrivalLabel, r.CloseLabel, r.DurationLabel)
	}
	vrMinutes(t, "on_site from the entered time", r.OnSiteMinutes, 5*60)

	// A later correction moves the sign-out to 13:00.
	setClock(vrAt(0, 16, 0))
	if _, err := CorrectOccurredAt(st, "site-a", 1, TimeCorrection{
		Target: r.CloseKey, CorrectedOccurredAtMS: vrAt(0, 13, 0), RecordedBy: "officer",
	}); err != nil {
		t.Fatal(err)
	}
	recs, _, err = VisitReport(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	r = vrByVisitID(recs)[v.VisitID]
	if r.CloseLabel != "claimed-corrected" || r.DurationLabel != "claimed-corrected" || r.ArrivalLabel != "observed" {
		t.Fatalf("labels after correction: arrival %q close %q duration %q", r.ArrivalLabel, r.CloseLabel, r.DurationLabel)
	}
	if r.ClosedAtMS != vrAt(0, 13, 0) {
		t.Fatalf("corrected close instant not substituted: %d", r.ClosedAtMS)
	}
	vrMinutes(t, "on_site from the corrected time", r.OnSiteMinutes, 4*60)

	// The OPENER end resolves through the same path: correct the arrival
	// to 09:30 and the duration shrinks to 210 with the arrival now a claim.
	if _, err := CorrectOccurredAt(st, "site-a", 1, TimeCorrection{
		Target: r.ArrivalKey, CorrectedOccurredAtMS: vrAt(0, 9, 30), RecordedBy: "officer",
	}); err != nil {
		t.Fatal(err)
	}
	recs, _, err = VisitReport(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	r = vrByVisitID(recs)[v.VisitID]
	if r.ArrivalLabel != "claimed-corrected" || r.ArrivedAtMS != vrAt(0, 9, 30) {
		t.Fatalf("corrected arrival: label %q instant %d", r.ArrivalLabel, r.ArrivedAtMS)
	}
	vrMinutes(t, "on_site with both ends corrected", r.OnSiteMinutes, 210)
}

// TestVisitReportLabelsObservedUnderAnUntrustedClock — the same default
// stamps under a clock the kernel disowned read observed-clock-untrusted,
// never observed. The duration is still computed (same clock, both ends).
func TestVisitReportLabelsObservedUnderAnUntrustedClock(t *testing.T) {
	st := newDomainTestStore(t)
	setClock := vrFixedClock(st, store.ClockUnsynced)

	setClock(vrAt(0, 9, 0))
	ps := vrRoster(t, st, "site-a", "Una Unsynced")
	truck := w7SeedVehicleID(t, st, "site-a", "TRUCK 3")
	vrCheckIn(t, st, "site-a", truck, ps[0].PersonID)
	v := vrVisitOf(t, st, "site-a", ps[0].PersonID, true)
	setClock(vrAt(0, 9, 45))
	if err := CheckOutVisit(st, "site-a", 1, v.VisitID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	recs, _, err := VisitReport(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := vrByVisitID(recs)[v.VisitID]
	if r.ArrivalLabel != "observed-clock-untrusted" || r.CloseLabel != "observed-clock-untrusted" || r.DurationLabel != "observed-clock-untrusted" {
		t.Fatalf("labels: arrival %q close %q duration %q", r.ArrivalLabel, r.CloseLabel, r.DurationLabel)
	}
	vrMinutes(t, "on_site", r.OnSiteMinutes, 45)
}

// TestVisitReportCollapsesMergedPersonsToOne — two ids for one human, each
// with a visit. Before the officer links them the report counts two persons;
// after `same_as` both rows carry the canonical id and the count is one,
// while visit_person_id still shows which id each visit was recorded under.
// The visits themselves close exactly as before (the fold matches raw ids).
func TestVisitReportCollapsesMergedPersonsToOne(t *testing.T) {
	st := newDomainTestStore(t)
	setClock := vrFixedClock(st, store.ClockSynced)

	setClock(vrAt(0, 8, 0))
	ps := vrRoster(t, st, "site-a", "Dana Driver", "Dana  Driver")
	canon, dup := ps[0].PersonID, ps[1].PersonID
	truck := w7SeedVehicleID(t, st, "site-a", "TRUCK 4")
	vrCheckIn(t, st, "site-a", truck, canon)
	v1 := vrVisitOf(t, st, "site-a", canon, true)
	setClock(vrAt(0, 9, 0))
	if err := CheckOutVisit(st, "site-a", 1, v1.VisitID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	setClock(vrAt(0, 10, 0))
	vrCheckIn(t, st, "site-a", truck, dup)
	v2 := vrVisitOf(t, st, "site-a", dup, true)
	setClock(vrAt(0, 11, 0))
	if err := CheckOutVisit(st, "site-a", 1, v2.VisitID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}

	// Control: unmerged, two persons.
	recs, sum, err := VisitReport(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sum.DistinctVisitPersonIDs != 2 || sum.DistinctPersons != 2 {
		t.Fatalf("control (unmerged): want 2 ids -> 2 persons, got %d -> %d", sum.DistinctVisitPersonIDs, sum.DistinctPersons)
	}
	if vrByVisitID(recs)[v2.VisitID].PersonID != dup {
		t.Fatalf("control: the duplicate's visit must carry its own id before any merge")
	}

	// The officer links the duplicate to the canonical record.
	if _, err := AmendPerson(st, "site-a", 1, PersonAmendment{PersonID: dup, SameAs: &canon}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	recs, sum, err = VisitReport(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	by := vrByVisitID(recs)
	if by[v1.VisitID].PersonID != canon || by[v2.VisitID].PersonID != canon {
		t.Fatalf("merged: both visits must resolve to %s, got %s and %s", canon, by[v1.VisitID].PersonID, by[v2.VisitID].PersonID)
	}
	if by[v2.VisitID].VisitPersonID != dup {
		t.Fatalf("merged: visit_person_id must still show the id the visit was recorded under")
	}
	if sum.DistinctVisitPersonIDs != 2 || sum.DistinctPersons != 1 {
		t.Fatalf("merged: want 2 ids -> 1 person, got %d -> %d", sum.DistinctVisitPersonIDs, sum.DistinctPersons)
	}
	if by[v1.VisitID].ClosedBy != ClosedByCheckout || by[v2.VisitID].ClosedBy != ClosedByCheckout {
		t.Fatalf("merged: closure must be unchanged by the merge: %q / %q", by[v1.VisitID].ClosedBy, by[v2.VisitID].ClosedBy)
	}
}

// TestVisitReportReadsPeerSitesFromOneStore — estate-wide is free: site-b
// holds site-a's log up to the watermark, so a report run at B lists A's
// visits (closed by the dispatch that brought the crew to B) beside B's own
// open ones, and prints both streams' coverage. Run at A, B's arrival has
// not replicated back, so only A's rows appear — the coverage lines say so.
func TestVisitReportReadsPeerSitesFromOneStore(t *testing.T) {
	a, b := newDomainTestStore(t), newDomainTestStore(t)
	setA := vrFixedClock(a, store.ClockSynced)
	setB := vrFixedClock(b, store.ClockSynced)
	sync := func() {
		t.Helper()
		evs, err := a.Events("site-a", 1, 1, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, ev := range evs {
			if _, err := b.Apply(ev); err != nil {
				t.Fatal(err)
			}
		}
	}

	setA(vrAt(0, 10, 0))
	ps := vrRoster(t, a, "site-a", "Pia Peer", "Quin Queue")
	pia, quin := ps[0].PersonID, ps[1].PersonID
	truck := w7SeedVehicleID(t, a, "site-a", "TRUCK 5")
	vrCheckIn(t, a, "site-a", truck, pia, quin)
	setA(vrAt(0, 11, 0))
	dest, deadline := "site-b", int64(90)
	trip, err := Dispatch(a, "site-a", 1, DispatchInput{
		VehicleID: truck, DriverPersonID: pia, OccupantPersonIDs: []string{quin},
		DestinationSite: &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	sync()
	setB(vrAt(0, 12, 30))
	if _, err := RecordExpectedArrival(b, "site-b", 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}

	atB, sumB, err := VisitReport(b, nil)
	if err != nil {
		t.Fatal(err)
	}
	var aRows, bRows int
	for _, r := range atB {
		switch r.SiteID {
		case "site-a":
			aRows++
			if r.ClosedBy != ClosedByDispatch {
				t.Fatalf("at B, A's visit %s should read dispatched, got %q", r.VisitID, r.ClosedBy)
			}
			vrMinutes(t, "A visit on_site seen from B", r.OnSiteMinutes, 60)
		case "site-b":
			bRows++
			if r.ClosedBy != VisitStillOpen {
				t.Fatalf("at B, B's visit %s should read open, got %q", r.VisitID, r.ClosedBy)
			}
		}
	}
	if aRows != 2 || bRows != 2 {
		t.Fatalf("at B: want 2 site-a rows + 2 site-b rows, got %d + %d", aRows, bRows)
	}
	if len(sumB.Streams) != 2 || sumB.Streams[0].SiteID != "site-a" || sumB.Streams[1].SiteID != "site-b" {
		t.Fatalf("at B: coverage must name both streams, got %+v", sumB.Streams)
	}

	atA, sumA, err := VisitReport(a, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(atA) != 2 || len(sumA.Streams) != 1 {
		t.Fatalf("at A (B never synced back): want 2 rows and 1 stream, got %d rows, streams %+v", len(atA), sumA.Streams)
	}
}

// TestVisitReportCSVShape — the header is the fixed column list and a
// closed row renders instants as RFC 3339 UTC with the minute columns as
// plain integers; an open row leaves every close column blank.
func TestVisitReportCSVShape(t *testing.T) {
	st := newDomainTestStore(t)
	setClock := vrFixedClock(st, store.ClockSynced)
	setClock(vrAt(0, 10, 0))
	ps := vrRoster(t, st, "site-a", "Rae Row", "Sol Still")
	truck := w7SeedVehicleID(t, st, "site-a", "TRUCK 6")
	vrCheckIn(t, st, "site-a", truck, ps[0].PersonID, ps[1].PersonID)
	v := vrVisitOf(t, st, "site-a", ps[0].PersonID, true)
	setClock(vrAt(0, 12, 0))
	if err := CheckOutVisit(st, "site-a", 1, v.VisitID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	recs, _, err := VisitReport(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := WriteVisitReportCSV(&buf, recs); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("want header + 2 rows, got %d", len(rows))
	}
	if !reflect.DeepEqual(rows[0], VisitReportColumns) {
		t.Fatalf("header:\n got %q\nwant %q", rows[0], VisitReportColumns)
	}
	col := func(row []string, name string) string {
		for i, c := range VisitReportColumns {
			if c == name {
				return row[i]
			}
		}
		t.Fatalf("no column %q", name)
		return ""
	}
	var closed, open []string
	for _, row := range rows[1:] {
		if col(row, "visit_id") == v.VisitID {
			closed = row
		} else {
			open = row
		}
	}
	if closed == nil || open == nil {
		t.Fatalf("rows: %q", rows)
	}
	if col(closed, "arrived_at_utc") != "2026-09-09T10:00:00Z" || col(closed, "closed_at_utc") != "2026-09-09T12:00:00Z" {
		t.Fatalf("instants: %q / %q", col(closed, "arrived_at_utc"), col(closed, "closed_at_utc"))
	}
	if col(closed, "closed_by") != "checked_out" || col(closed, "departure_recorded") != "yes" || col(closed, "on_site_min") != "120" || col(closed, "record_open_min") != "120" ||
		col(closed, "duration_label") != "observed" || col(closed, "duration_bucket") != "under 8h" || col(closed, "person_name") != "Rae Row" {
		t.Fatalf("closed row: %q", closed)
	}
	if col(closed, "arrival_key") != "site-a:1:"+strconv.FormatInt(recs[0].ArrivalKey.Seq, 10) && col(closed, "arrival_key") != "site-a:1:"+strconv.FormatInt(recs[1].ArrivalKey.Seq, 10) {
		t.Fatalf("arrival_key not site:epoch:seq: %q", col(closed, "arrival_key"))
	}
	for _, c := range []string{"departure_recorded", "close_key", "closed_at_utc", "close_time_label", "on_site_min", "record_open_min", "duration_label", "duration_bucket"} {
		if col(open, c) != "" {
			t.Fatalf("open row must leave %s blank, got %q", c, col(open, c))
		}
	}
	if col(open, "closed_by") != "open" {
		t.Fatalf("open row closed_by: %q", col(open, "closed_by"))
	}
}

// TestVisitReportUncontrolledRunIsDispatchedNotReCheckin — lifted from the
// review (RV1). An UNCONTROLLED-pad run has destination_site nil exactly like
// a degenerate check-in but IS a recorded departure: it must read
// dispatched, never re_checkin. Dropping the destination_uncontrolled half of
// the classification leaves every other test green and fails this one.
func TestVisitReportUncontrolledRunIsDispatchedNotReCheckin(t *testing.T) {
	st := newDomainTestStore(t)
	setClock := vrFixedClock(st, store.ClockSynced)
	setClock(vrAt(0, 10, 0))
	ps := vrRoster(t, st, "site-a", "Uma Uncontrolled")
	truck := w7SeedVehicleID(t, st, "site-a", "TRUCK 9")
	vrCheckIn(t, st, "site-a", truck, ps[0].PersonID)
	v := vrVisitOf(t, st, "site-a", ps[0].PersonID, true)

	setClock(vrAt(0, 11, 0))
	pad := "Pad 3"
	if _, err := Dispatch(st, "site-a", 1, DispatchInput{
		VehicleID: truck, DriverPersonID: ps[0].PersonID, DestinationUncontrolled: &pad,
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	recs, sum, err := VisitReport(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := vrByVisitID(recs)[v.VisitID]
	if r.ClosedBy != ClosedByDispatch {
		t.Fatalf("uncontrolled run: want dispatched, got %q", r.ClosedBy)
	}
	vrMinutes(t, "uncontrolled on_site", r.OnSiteMinutes, 60)
	if sum.Dispatched != 1 || sum.ReCheckin != 0 {
		t.Fatalf("summary: dispatched %d re_checkin %d", sum.Dispatched, sum.ReCheckin)
	}
}

// TestVisitReportCSVBlankIsNotZero — lifted from the review (RV2). A
// zero-minute recorded departure renders on_site_min "0"; a missed sign-out
// renders on_site_min "" beside a numeric record_open_min and
// departure_recorded "no". Blank and zero must never be the same byte.
func TestVisitReportCSVBlankIsNotZero(t *testing.T) {
	st := newDomainTestStore(t)
	setClock := vrFixedClock(st, store.ClockSynced)
	setClock(vrAt(0, 10, 0))
	ps := vrRoster(t, st, "site-a", "Zed Zero", "Mia Missed")
	zed, mia := ps[0].PersonID, ps[1].PersonID
	truck := w7SeedVehicleID(t, st, "site-a", "TRUCK 10")
	vrCheckIn(t, st, "site-a", truck, zed, mia)
	vz := vrVisitOf(t, st, "site-a", zed, true)
	vm := vrVisitOf(t, st, "site-a", mia, true)
	// Same instant: a zero-minute departure.
	if err := CheckOutVisit(st, "site-a", 1, vz.VisitID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	setClock(vrAt(0, 12, 0))
	vrCheckIn(t, st, "site-a", truck, mia) // re_checkin closes vm

	recs, _, err := VisitReport(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := WriteVisitReportCSV(&buf, recs); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	col := func(row []string, name string) string {
		for i, c := range VisitReportColumns {
			if c == name {
				return row[i]
			}
		}
		t.Fatalf("no column %q", name)
		return ""
	}
	var zrow, mrow []string
	for _, row := range rows[1:] {
		switch col(row, "visit_id") {
		case vz.VisitID:
			zrow = row
		case vm.VisitID:
			mrow = row
		}
	}
	if zrow == nil || mrow == nil {
		t.Fatalf("rows: %q", rows)
	}
	if col(zrow, "on_site_min") != "0" || col(zrow, "record_open_min") != "0" || col(zrow, "departure_recorded") != "yes" || col(zrow, "duration_bucket") != "under 8h" {
		t.Fatalf("zero-minute checkout row: %q", zrow)
	}
	if col(mrow, "closed_by") != "re_checkin" || col(mrow, "departure_recorded") != "no" || col(mrow, "on_site_min") != "" || col(mrow, "record_open_min") != "120" || col(mrow, "duration_bucket") != "" || col(mrow, "duration_label") != "observed" {
		t.Fatalf("missed sign-out row: %q", mrow)
	}
}

// TestVisitReportMergeChainCollapsesToOne — lifted from the review (RV3). A
// three-link chain (c -> b -> a) collapses three visits to ONE person while
// visit_person_id keeps each raw id.
func TestVisitReportMergeChainCollapsesToOne(t *testing.T) {
	st := newDomainTestStore(t)
	setClock := vrFixedClock(st, store.ClockSynced)
	setClock(vrAt(0, 8, 0))
	ps := vrRoster(t, st, "site-a", "Al Alias", "Al  Alias", "AL ALIAS")
	a, b, c := ps[0].PersonID, ps[1].PersonID, ps[2].PersonID
	truck := w7SeedVehicleID(t, st, "site-a", "TRUCK 13")
	for i, id := range []string{a, b, c} {
		setClock(vrAt(0, 9+2*i, 0))
		vrCheckIn(t, st, "site-a", truck, id)
		v := vrVisitOf(t, st, "site-a", id, true)
		setClock(vrAt(0, 10+2*i, 0))
		if err := CheckOutVisit(st, "site-a", 1, v.VisitID, nil, 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := AmendPerson(st, "site-a", 1, PersonAmendment{PersonID: c, SameAs: &b}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if _, err := AmendPerson(st, "site-a", 1, PersonAmendment{PersonID: b, SameAs: &a}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	recs, sum, err := VisitReport(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 {
		t.Fatalf("want 3 visits, got %d", len(recs))
	}
	raw := map[string]bool{}
	for _, r := range recs {
		if r.PersonID != a {
			t.Fatalf("visit %s (raw %s) resolved to %s, want %s", r.VisitID, r.VisitPersonID, r.PersonID, a)
		}
		raw[r.VisitPersonID] = true
	}
	if len(raw) != 3 || sum.DistinctVisitPersonIDs != 3 || sum.DistinctPersons != 1 {
		t.Fatalf("raw ids %d, summary %d -> %d", len(raw), sum.DistinctVisitPersonIDs, sum.DistinctPersons)
	}
}

// TestVisitReportCrossEpochClosureIsNeverObserved — the review's RV4 turned
// into an assertion. A visit opened under node_epoch 1 and closed under
// node_epoch 2 is the same site on two incarnations of the post (a re-image
// bumps the epoch), so the same-clock justification does not reach across
// it: the closure is still found and the span still computed, but the
// duration is labelled cross-epoch, never observed, and the summary counts
// it. Both ends individually remain observed.
func TestVisitReportCrossEpochClosureIsNeverObserved(t *testing.T) {
	st := newDomainTestStore(t)
	setClock := vrFixedClock(st, store.ClockSynced)
	setClock(vrAt(0, 10, 0))
	ps := vrRoster(t, st, "site-a", "Eph Epoch")
	truck := w7SeedVehicleID(t, st, "site-a", "TRUCK 11")
	vrCheckIn(t, st, "site-a", truck, ps[0].PersonID)
	v := vrVisitOf(t, st, "site-a", ps[0].PersonID, true)
	setClock(vrAt(0, 14, 0))
	if err := CheckOutVisit(st, "site-a", 2, v.VisitID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	recs, sum, err := VisitReport(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := vrByVisitID(recs)[v.VisitID]
	if r.ClosedBy != ClosedByCheckout || r.ArrivalKey.NodeEpoch == r.CloseKey.NodeEpoch {
		t.Fatalf("fixture: want a checkout across epochs, got %q %v/%v", r.ClosedBy, r.ArrivalKey, r.CloseKey)
	}
	if r.ArrivalLabel != "observed" || r.CloseLabel != "observed" {
		t.Fatalf("each end on its own is still an observation: %q / %q", r.ArrivalLabel, r.CloseLabel)
	}
	if r.DurationLabel != "cross-epoch" {
		t.Fatalf("a duration spanning a re-image must be labelled cross-epoch, got %q", r.DurationLabel)
	}
	vrMinutes(t, "cross-epoch span (still computed)", r.OnSiteMinutes, 240)
	if sum.CrossEpoch != 1 || len(sum.Streams) != 2 {
		t.Fatalf("summary: cross_epoch %d streams %+v", sum.CrossEpoch, sum.Streams)
	}
}
