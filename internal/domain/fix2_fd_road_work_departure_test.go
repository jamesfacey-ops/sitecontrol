package domain

// fix2_fd_road_work_departure_test.go — FINDINGS-R4 D9 (L11 EV-6 = L18
// RW-1): a road-work departure was classified "re_checkin" — the degenerate
// local trip's fingerprint, a MISSED SIGN-OUT — because the visit replay read
// "destination-less" as "the dispatch half of a gate check-in". So every
// crew sent out working on the road was a sign-out failure at a text-message
// site and was left out of every recorded-departure average.
// James, 2026-10-04 (R5): "Road work counts as a departure."

import (
	"testing"

	"sitecontrol/internal/store"
)

func TestVisitReportRoadWorkRunIsADepartureNotAMissedSignout(t *testing.T) {
	st := newDomainTestStore(t)
	setClock := vrFixedClock(st, store.ClockSynced)
	setClock(vrAt(0, 10, 0))
	ps := vrRoster(t, st, "site-a", "Gail Grader")
	grader := w7SeedVehicleID(t, st, "site-a", "GRADER 7")
	vrCheckIn(t, st, "site-a", grader, ps[0].PersonID)
	v := vrVisitOf(t, st, "site-a", ps[0].PersonID, true)

	setClock(vrAt(0, 11, 30))
	limit := int64(720)
	if _, err := Dispatch(st, "site-a", 1, DispatchInput{
		VehicleID: grader, DriverPersonID: ps[0].PersonID, RoadWork: true, DeadlineMinutes: &limit,
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// A text-message site: the type at which a re_checkin IS a failure, so
	// the old classification is visible in Failure as well as in ClosedBy.
	recs, sum, err := VisitReport(st, SiteTypes{"site-a": SiteTypeTextMessage})
	if err != nil {
		t.Fatal(err)
	}
	r := vrByVisitID(recs)[v.VisitID]
	if r.ClosedBy != ClosedByDispatch {
		t.Fatalf("road-work run: closed_by %q, want dispatched (a recorded departure, R5)", r.ClosedBy)
	}
	if r.Failure != "" {
		t.Fatalf("road-work run: failure %q, want none — it is not a missed sign-out", r.Failure)
	}
	vrMinutes(t, "road-work on_site", r.OnSiteMinutes, 90)
	if sum.Dispatched != 1 || sum.ReCheckin != 0 {
		t.Fatalf("summary: dispatched %d re_checkin %d, want 1 / 0", sum.Dispatched, sum.ReCheckin)
	}
}
