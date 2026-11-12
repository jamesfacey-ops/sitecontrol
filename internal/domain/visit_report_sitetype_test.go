// visit_report_sitetype_test.go — R-4/R-5 witnesses (James, 2026-09-09):
// the site type, and nothing else, decides whether a visit the person closed
// by their own return is a FAILURE TO SIGN OUT. One log, three readings.
//
// The camp shape is a 35-day stay — "a month or more" — in both forms a
// camp produces: Cam never signs out and walks back through the gate on day
// 35 (the old visit closes re_checkin, 50,400 minutes open); Rae signs out
// properly on day 35 (checked_out, 50,400 minutes ON SITE, bucket "over
// 30d"). Read as a CAMP neither is a failure. Read as a TEXT-MESSAGE site —
// the same bytes — Cam's IS one and Rae's still is not. Read with NO type
// known, nothing carries a verdict and the site is named as untyped, so an
// unknown is never printed as a zero. Every event is authored through the
// production doors under an injected clock, as visit_report_test.go does.
// Each arm watched red on a mutant of classifyReturn before it was trusted
// green — reports/witness/R4-R5-SITETYPE.md.
package domain

import (
	"bytes"
	"encoding/csv"
	"reflect"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

const vrThirtyFiveDaysMin = 35 * 24 * 60

// vrCampLog authors the two 35-day visits at site-a and returns Cam's OLD
// (return-closed) visit id, Rae's (checked-out) visit id, and both person ids.
func vrCampLog(t *testing.T) (st *store.Store, camOld, rae, camID, raeID string) {
	t.Helper()
	st = newDomainTestStore(t)
	setClock := vrFixedClock(st, store.ClockSynced)
	setClock(vrAt(0, 7, 0))
	ps := vrRoster(t, st, "site-a", "Cam Camper", "Rae Resident")
	camID, raeID = ps[0].PersonID, ps[1].PersonID
	bus := w7SeedVehicleID(t, st, "site-a", "CAMP BUS")
	vrCheckIn(t, st, "site-a", bus, camID, raeID)
	vCam := vrVisitOf(t, st, "site-a", camID, true)
	vRae := vrVisitOf(t, st, "site-a", raeID, true)
	setClock(vrAt(35, 7, 0))
	if err := CheckOutVisit(st, "site-a", 1, vRae.VisitID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	vrCheckIn(t, st, "site-a", bus, camID) // re_checkin closes vCam: nobody signed Cam out
	return st, vCam.VisitID, vRae.VisitID, camID, raeID
}

func vrPersonRow(t *testing.T, rows []PersonSignoutFailures, personID string) PersonSignoutFailures {
	t.Helper()
	for _, r := range rows {
		if r.PersonID == personID {
			return r
		}
	}
	t.Fatalf("no per-person row for %s in %+v", personID, rows)
	return PersonSignoutFailures{}
}

// TestVisitReportSiteTypeAloneDecidesWhetherAReturnIsAFailure is the R-5
// exemption proven both ways on ONE log: the verdict flips with the type
// and with nothing else. PROVEN RED BY (each arm separately, see the
// report): classifyReturn returning verdictMissedSignout for SiteTypeCamp
// (camp arm); classifyReturn skipping the closure check (Rae is flagged in
// the text-message arm); classifyReturn treating "" as text_message (the
// untyped arm).
func TestVisitReportSiteTypeAloneDecidesWhetherAReturnIsAFailure(t *testing.T) {
	st, camOld, rae, camID, raeID := vrCampLog(t)

	// ---- read as a CAMP: 35 days is normal; nothing is a failure (R-5).
	recs, sum, err := VisitReport(st, SiteTypes{"site-a": SiteTypeCamp})
	if err != nil {
		t.Fatal(err)
	}
	by := vrByVisitID(recs)
	c := by[camOld]
	if c.ClosedBy != ClosedByReCheckin {
		t.Fatalf("fixture: Cam's old visit must read re_checkin, got %q", c.ClosedBy)
	}
	vrMinutes(t, "Cam record_open at the camp", c.RecordOpenMinutes, vrThirtyFiveDaysMin)
	if c.SiteType != SiteTypeCamp || c.Failure != "" {
		t.Fatalf("CAMP: a 35-day visit closed by the resident's own return was FLAGGED — site_type %q failure %q (R-5: never a failure at a camp)", c.SiteType, c.Failure)
	}
	r := by[rae]
	if r.ClosedBy != ClosedByCheckout || r.Failure != "" || r.DurationBucket != "over 30d" {
		t.Fatalf("CAMP: a 35-day checked-out stay must bucket over 30d with no failure: %+v", r)
	}
	vrMinutes(t, "Rae on_site at the camp", r.OnSiteMinutes, vrThirtyFiveDaysMin)
	if sum.MissedSignouts != 0 || sum.ExemptCampReturns != 1 || sum.UnclassifiedReturns != 0 || len(sum.UntypedSites) != 0 {
		t.Fatalf("CAMP summary: missed %d exempt %d unclassified %d untyped %v", sum.MissedSignouts, sum.ExemptCampReturns, sum.UnclassifiedReturns, sum.UntypedSites)
	}
	if p := vrPersonRow(t, SignoutFailuresByPerson(recs), camID); p.MissedSignouts != 0 || p.ExemptCampReturns != 1 || p.UnclassifiedReturns != 0 || p.Visits != 2 || len(p.MissedSignoutSites) != 0 {
		t.Fatalf("CAMP per-person Cam: %+v", p)
	}

	// ---- the SAME LOG read as a TEXT-MESSAGE site: Cam's return IS the
	// failure (R-4); Rae's recorded departure still is not; Cam's new,
	// still-open visit carries nothing (open visits are not aged).
	recs, sum, err = VisitReport(st, SiteTypes{"site-a": SiteTypeTextMessage})
	if err != nil {
		t.Fatal(err)
	}
	by = vrByVisitID(recs)
	c = by[camOld]
	if c.SiteType != SiteTypeTextMessage || c.Failure != FailureMissedSignout {
		t.Fatalf("TEXT-MESSAGE: a visit the worker never signed out of, closed by their own return, was NOT flagged — site_type %q failure %q", c.SiteType, c.Failure)
	}
	if c.OnSiteMinutes != nil || c.DurationBucket != "" {
		t.Fatalf("a missed sign-out still carries no on-site duration and no bucket: %+v", c)
	}
	if r = by[rae]; r.Failure != "" || r.SiteType != SiteTypeTextMessage {
		t.Fatalf("TEXT-MESSAGE: a checked-out visit must carry no failure at any type: %+v", r)
	}
	if nv := by[vrVisitOf(t, st, "site-a", camID, true).VisitID]; nv.Failure != "" || nv.ClosedBy != VisitStillOpen {
		t.Fatalf("TEXT-MESSAGE: Cam's NEW, still-open visit must carry no verdict: %+v", nv)
	}
	if sum.MissedSignouts != 1 || sum.ExemptCampReturns != 0 || sum.UnclassifiedReturns != 0 || len(sum.UntypedSites) != 0 {
		t.Fatalf("TEXT-MESSAGE summary: missed %d exempt %d unclassified %d untyped %v", sum.MissedSignouts, sum.ExemptCampReturns, sum.UnclassifiedReturns, sum.UntypedSites)
	}
	persons := SignoutFailuresByPerson(recs)
	if p := vrPersonRow(t, persons, camID); p.MissedSignouts != 1 || !reflect.DeepEqual(p.MissedSignoutSites, []string{"site-a"}) || p.ExemptCampReturns != 0 || p.UnclassifiedReturns != 0 || p.PersonName != "Cam Camper" || p.Visits != 2 {
		t.Fatalf("TEXT-MESSAGE per-person Cam: %+v", p)
	}
	if p := vrPersonRow(t, persons, raeID); p.MissedSignouts != 0 || p.Visits != 1 || len(p.MissedSignoutSites) != 0 {
		t.Fatalf("TEXT-MESSAGE per-person Rae: %+v", p)
	}
	if persons[0].PersonID != camID {
		t.Fatalf("most failures first: got %s", persons[0].PersonID)
	}

	// ---- the SAME LOG with NO type known: no verdict either way, and the
	// site is named on the summary.
	recs, sum, err = VisitReport(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	c = vrByVisitID(recs)[camOld]
	if c.SiteType != "" || c.Failure != "" {
		t.Fatalf("UNTYPED: a return at a site of unknown type carried a verdict — site_type %q failure %q", c.SiteType, c.Failure)
	}
	if sum.MissedSignouts != 0 || sum.ExemptCampReturns != 0 || sum.UnclassifiedReturns != 1 || !reflect.DeepEqual(sum.UntypedSites, []string{"site-a"}) {
		t.Fatalf("UNTYPED summary: missed %d exempt %d unclassified %d untyped %v", sum.MissedSignouts, sum.ExemptCampReturns, sum.UnclassifiedReturns, sum.UntypedSites)
	}
	if p := vrPersonRow(t, SignoutFailuresByPerson(recs), camID); p.MissedSignouts != 0 || p.ExemptCampReturns != 0 || p.UnclassifiedReturns != 1 {
		t.Fatalf("UNTYPED per-person Cam: %+v", p)
	}
	joined := strings.Join(sum.Lines(), "\n")
	if !strings.Contains(joined, "NOT known at this post") || !strings.Contains(joined, "site-a") || !strings.Contains(joined, "unclassified, site type not known at this post 1") {
		t.Fatalf("the summary must name the untyped site and the unclassified count:\n%s", joined)
	}
}

// TestSignoutFailuresByPersonCountsAcrossSitesUnderEachSitesOwnType — R-4 is
// per person ACROSS sites, and one post's store holds every peer's log. Pat
// misses a sign-out at site-a (gate re-check-in), rides to site-b, and
// misses one there too — recorded under a DUPLICATE id that B's officer
// later links with same_as. Read at B with both sites text-message: ONE row,
// 2 failures, sites "site-a site-b". With B a camp: 1 failure (site-a) and
// 1 exempt. Unmerged first, as the control: two rows of one each — the
// split R-4 warned about, and the reason the fold keys by the resolved id.
func TestSignoutFailuresByPersonCountsAcrossSitesUnderEachSitesOwnType(t *testing.T) {
	a, b := newDomainTestStore(t), newDomainTestStore(t)
	setA := vrFixedClock(a, store.ClockSynced)
	setB := vrFixedClock(b, store.ClockSynced)

	setA(vrAt(0, 8, 0))
	pat := vrRoster(t, a, "site-a", "Pat Peripatetic")[0].PersonID
	truckA := w7SeedVehicleID(t, a, "site-a", "TRUCK 20")
	vrCheckIn(t, a, "site-a", truckA, pat)
	setA(vrAt(1, 8, 0))
	vrCheckIn(t, a, "site-a", truckA, pat) // day 1: closes day 0's visit — missed sign-out at A
	setA(vrAt(1, 9, 0))
	dest, deadline := "site-b", int64(90)
	trip, err := Dispatch(a, "site-a", 1, DispatchInput{VehicleID: truckA, DriverPersonID: pat, DestinationSite: &dest, DeadlineMinutes: &deadline}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	evs, err := a.Events("site-a", 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if _, err := b.Apply(ev); err != nil {
			t.Fatal(err)
		}
	}
	setB(vrAt(1, 11, 0))
	if _, err := RecordExpectedArrival(b, "site-b", 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// B's officer enrols Pat again — a duplicate record, same name, same
	// (replicated) company and trade — and the next two gate check-ins at B
	// are recorded under it. Reference data is estate-wide, so the company
	// and trade already exist at B and are reused rather than re-created.
	setB(vrAt(2, 8, 0))
	orig, err := PersonByID(b, pat)
	if err != nil || orig == nil {
		t.Fatalf("Pat's record must have replicated to B: %v", err)
	}
	dup, err := CreatePerson(b, "site-b", 1, "Pat Peripatetic", orig.CompanyID, orig.TradeID, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	pat2 := dup.PersonID
	truckB := w7SeedVehicleID(t, b, "site-b", "TRUCK 21")
	vrCheckIn(t, b, "site-b", truckB, pat2)
	setB(vrAt(3, 8, 0))
	vrCheckIn(t, b, "site-b", truckB, pat2) // day 3: closes day 2's visit — missed sign-out at B, under the duplicate

	both := SiteTypes{"site-a": SiteTypeTextMessage, "site-b": SiteTypeTextMessage}

	// Control: unmerged — two rows, one failure each, each at its own site.
	recs, _, err := VisitReport(b, both)
	if err != nil {
		t.Fatal(err)
	}
	rows := SignoutFailuresByPerson(recs)
	if len(rows) != 2 {
		t.Fatalf("control (unmerged): want 2 rows, got %d: %+v", len(rows), rows)
	}
	if p := vrPersonRow(t, rows, pat); p.MissedSignouts != 1 || !reflect.DeepEqual(p.MissedSignoutSites, []string{"site-a"}) || p.Visits != 3 {
		t.Fatalf("control: Pat's own id: %+v", p)
	}
	if p := vrPersonRow(t, rows, pat2); p.MissedSignouts != 1 || !reflect.DeepEqual(p.MissedSignoutSites, []string{"site-b"}) || p.Visits != 2 {
		t.Fatalf("control: the duplicate: %+v", p)
	}

	// The officer links the duplicate to Pat's record.
	if _, err := AmendPerson(b, "site-b", 1, PersonAmendment{PersonID: pat2, SameAs: &pat}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	recs, _, err = VisitReport(b, both)
	if err != nil {
		t.Fatal(err)
	}
	rows = SignoutFailuresByPerson(recs)
	if len(rows) != 1 {
		t.Fatalf("merged: want ONE row, got %d: %+v", len(rows), rows)
	}
	p := rows[0]
	if p.PersonID != pat || p.MissedSignouts != 2 || !reflect.DeepEqual(p.MissedSignoutSites, []string{"site-a", "site-b"}) || p.Visits != 5 || p.ExemptCampReturns != 0 || p.UnclassifiedReturns != 0 {
		t.Fatalf("merged, both text-message: %+v", p)
	}

	// The CSV: fixed header, one row, sites space-joined in one cell.
	var buf bytes.Buffer
	if err := WritePersonSignoutFailuresCSV(&buf, rows); err != nil {
		t.Fatal(err)
	}
	all, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || !reflect.DeepEqual(all[0], PersonSignoutFailureColumns) {
		t.Fatalf("csv: %q", all)
	}
	if want := []string{pat, "Pat Peripatetic", "5", "2", "site-a site-b", "0", "0"}; !reflect.DeepEqual(all[1], want) {
		t.Fatalf("csv row:\n got %q\nwant %q", all[1], want)
	}

	// Site-b a camp: its return is exempt; site-a's stands. Per-site types,
	// one person.
	recs, _, err = VisitReport(b, SiteTypes{"site-a": SiteTypeTextMessage, "site-b": SiteTypeCamp})
	if err != nil {
		t.Fatal(err)
	}
	p = vrPersonRow(t, SignoutFailuresByPerson(recs), pat)
	if p.MissedSignouts != 1 || !reflect.DeepEqual(p.MissedSignoutSites, []string{"site-a"}) || p.ExemptCampReturns != 1 || p.UnclassifiedReturns != 0 {
		t.Fatalf("merged, B a camp: %+v", p)
	}
	// And with only A typed — what a report run AT A would know: B's
	// return is unclassified, never a zero.
	p = vrPersonRow(t, SignoutFailuresByPerson(func() []VisitRecord {
		recs, _, err := VisitReport(b, SiteTypes{"site-a": SiteTypeTextMessage})
		if err != nil {
			t.Fatal(err)
		}
		return recs
	}()), pat)
	if p.MissedSignouts != 1 || p.ExemptCampReturns != 0 || p.UnclassifiedReturns != 1 {
		t.Fatalf("merged, only A typed: %+v", p)
	}
}
