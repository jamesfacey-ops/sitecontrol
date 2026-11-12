package web

import (
	"testing"

	"sitecontrol/internal/domain"
)

// TestFix4NotTrackedHeadingCountsDistinctTrips is R6 L03-03's LOW: "Not
// tracked (N on M trips)" counted ROWS, and one run can carry both a "taken
// off" row and a "not aboard at arrival" row. FAIL BEFORE: no distinct count
// existed; the template printed len .NotTracked (3 here).
func TestFix4NotTrackedHeadingCountsDistinctTrips(t *testing.T) {
	row := func(trip string, people ...string) notTrackedRow {
		r := notTrackedRow{}
		r.TripID = trip
		for _, p := range people {
			r.Persons = append(r.Persons, domain.OccupantRef{PersonID: p})
		}
		return r
	}
	d := &musterData{NotTracked: []notTrackedRow{
		row("t1", "a"), row("t1", "b", "c"), row("t2", "d"),
	}}
	d.countOffSite()
	if d.NotTrackedCount != 4 || d.NotTrackedTripCount != 2 {
		t.Fatalf("NotTrackedCount %d on %d trips, want 4 on 2", d.NotTrackedCount, d.NotTrackedTripCount)
	}
}
