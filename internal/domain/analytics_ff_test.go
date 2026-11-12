// analytics_ff_test.go — fix round 2, stream FF (FINDINGS-R4 cluster F and
// the averages). Same rule as analytics_test.go: real events through the
// production doors, every expected number hand-derived in the comment.
//
//   - D7 / L18 TB-1, ruling R8 (James, 2026-10-04): a truck that turned back
//     ("Arrived back here") is NOT a trip that arrived. TRIPS ARRIVED leaves
//     it out and the page shows it apart as "turned back"; the origin's
//     travel in is unchanged by the ruling.
//   - L18 AVG-1: the average on site is the exact mean of the integer
//     minutes, so a .5 tie rounds half-up as fmtAvg says it does — never a
//     running float mean that drifts to x.4999… and rounds down.
//
// Both were RED on fork/next 2ba9188 and are GREEN with the change.
package domain

import (
	"testing"
	"time"

	"sitecontrol/internal/store"
)

// TestAnalyticsATurnedBackTruckIsNotATripThatArrived — anSeedSiteA's log
// (Bob dispatched site-a → site-b today 13:00, trip open), then at 15:00
// site-a records "Arrived back here" for that trip: the truck never reached
// site-b.
//
// Expected at now = today 18:00 UTC:
//
//	trips dispatched 1/1; trips arrived 0/0 (was 1/1 on 2ba9188);
//	turned back 1 today / 1 season; in transit 0 (the run is closed);
//	site-a travel in 1/1 — R8 keeps the origin's travel in as it was.
//
// dispatched = arrived + turned back + in transit (1 = 0 + 1 + 0).
func TestAnalyticsATurnedBackTruckIsNotATripThatArrived(t *testing.T) {
	st, trip := anSeedSiteA(t)
	vrFixedClock(st, store.ClockSynced)(vrAt(0, 15, 0))
	if _, err := RecordReturnToOrigin(st, "site-a", 1, trip.TripID, nil, nil, 0, "default", true); err != nil {
		t.Fatal(err)
	}
	ret, err := TripsReturnedToOrigin(st)
	if err != nil {
		t.Fatal(err)
	}
	if !ret[trip.TripID] {
		t.Fatalf("precondition: the record must classify %s as returned to its origin", trip.TripID)
	}
	fig, err := BuildAnalytics(st, time.UnixMilli(vrAt(0, 18, 0)), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if fig.TripsArrived != 0 || fig.TripsArrivedToday != 0 {
		t.Errorf("trips arrived today/season = %d/%d, want 0/0 — the truck turned back and never reached site-b (D7, R8)",
			fig.TripsArrivedToday, fig.TripsArrived)
	}
	if fig.TripsTurnedBack != 1 || fig.TripsTurnedBackToday != 1 {
		t.Errorf("turned back today/season = %d/%d, want 1/1", fig.TripsTurnedBackToday, fig.TripsTurnedBack)
	}
	if fig.TripsDispatched != fig.TripsArrived+fig.TripsTurnedBack+fig.TripsInTransit {
		t.Errorf("dispatched %d != arrived %d + turned back %d + in transit %d",
			fig.TripsDispatched, fig.TripsArrived, fig.TripsTurnedBack, fig.TripsInTransit)
	}
	if a := anSite(t, fig, "site-a"); a.TravelInToday != 1 || a.TravelInSeason != 1 {
		t.Errorf("site-a travel in today/season = %d/%d, want 1/1 — R8 leaves the origin's travel in as it is", a.TravelInToday, a.TravelInSeason)
	}
}

// TestAnalyticsATruckThatReachedItsDestinationIsStillArrived is the control
// for the test above: the same trip arriving at site-b counts as arrived
// and nothing is "turned back".
func TestAnalyticsATruckThatReachedItsDestinationIsStillArrived(t *testing.T) {
	st, trip := anSeedSiteA(t)
	vrFixedClock(st, store.ClockSynced)(vrAt(0, 15, 0))
	if _, err := RecordExpectedArrival(st, "site-b", 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	fig, err := BuildAnalytics(st, time.UnixMilli(vrAt(0, 18, 0)), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if fig.TripsArrived != 1 || fig.TripsArrivedToday != 1 || fig.TripsTurnedBack != 0 || fig.TripsTurnedBackToday != 0 {
		t.Errorf("arrived %d/%d turned back %d/%d, want 1/1 and 0/0",
			fig.TripsArrivedToday, fig.TripsArrived, fig.TripsTurnedBackToday, fig.TripsTurnedBack)
	}
}

// TestMinutesAvgIsTheExactMeanOfTheMinutes — L18 AVG-1. These eight stays
// sum to 508 minutes: the mean is 63.5 exactly, which rounds half-up to 64.
// The running mean m = (m·n + x)/(n+1) over them in this order lands on
// 63.49999999999999 and rounds to 63 (found by search; the lane's own case
// was ice-road's 34 visits, 561/34 = 16.5 shown as "16 min").
func TestMinutesAvgIsTheExactMeanOfTheMinutes(t *testing.T) {
	var m MinutesAvg
	for _, x := range []int64{83, 86, 2, 46, 122, 46, 82, 41} {
		m.add(x)
	}
	if m.N != 8 || m.Mean != 63.5 {
		t.Errorf("MinutesAvg = n=%d mean=%v, want n=8 mean=63.5 exactly (508/8)", m.N, m.Mean)
	}
	if got := m.RoundedMinutes(); got != 64 {
		t.Errorf("RoundedMinutes = %d, want 64 (63.5 rounds half-up)", got)
	}
	var neg MinutesAvg // a skewed clock can give a negative stay; half-up is still floor(mean + ½)
	for _, x := range []int64{-3, -4} {
		neg.add(x)
	}
	if got := neg.RoundedMinutes(); got != -3 {
		t.Errorf("RoundedMinutes over -3, -4 = %d, want -3 (floor(-3.5 + 0.5))", got)
	}
}
