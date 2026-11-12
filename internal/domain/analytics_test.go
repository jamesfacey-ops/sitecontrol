// analytics_test.go — witnesses for the dispatch-analytics figures
// (analytics.go). Same rule as visit_report_test.go: every scenario authors
// REAL events through the production doors under an injected store.Clock, so
// the figures are asserted over the log shape a post writes, never over
// hand-built rows. Every expected number below is hand-derived in the
// scenario comment, not read back from the code under test.
package domain

import (
	"testing"
	"time"

	"sitecontrol/internal/store"
)

func anSite(t *testing.T, fig AnalyticsFigures, id string) SiteFigures {
	t.Helper()
	for _, s := range fig.Sites {
		if s.SiteID == id {
			return s
		}
	}
	t.Fatalf("no figures for %s in %+v", id, fig.Sites)
	return SiteFigures{}
}

// anSeedSiteA authors the site-a log described on the test below and
// returns the store and Bob's dispatched trip.
func anSeedSiteA(t *testing.T) (*store.Store, Trip) {
	t.Helper()
	st := newDomainTestStore(t)
	setClock := vrFixedClock(st, store.ClockSynced)

	setClock(vrAt(-1, 8, 0))
	ps := vrRoster(t, st, "site-a", "Zed Yesterday", "Ann Arrival", "Bob Bus", "Cal Camper")
	zed, ann, bob, cal := ps[0].PersonID, ps[1].PersonID, ps[2].PersonID, ps[3].PersonID
	truck := w7SeedVehicleID(t, st, "site-a", "TRUCK 1")
	vrCheckIn(t, st, "site-a", truck, zed)
	setClock(vrAt(-1, 10, 0))
	if err := CheckOutVisit(st, "site-a", 1, vrVisitOf(t, st, "site-a", zed, true).VisitID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}

	setClock(vrAt(0, 10, 0))
	vrCheckIn(t, st, "site-a", truck, ann, bob, cal)
	setClock(vrAt(0, 12, 0))
	if err := CheckOutVisit(st, "site-a", 1, vrVisitOf(t, st, "site-a", ann, true).VisitID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	setClock(vrAt(0, 13, 0))
	dest, deadline := "site-b", int64(60)
	trip, err := Dispatch(st, "site-a", 1, DispatchInput{
		VehicleID: truck, DriverPersonID: bob, DestinationSite: &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	return st, trip
}

// TestAnalyticsFiguresOverASeededLog — the seeded site-a log:
//
//	yesterday 08:00  Zed checks in; 10:00 signs out          (120 min, season only)
//	today     10:00  Ann, Bob, Cal check in
//	today     12:00  Ann signs out                           (120 min)
//	today     13:00  Bob is dispatched to site-b             (180 min; trip open, controlled)
//	Cal never signs out: open, the one person on site.
//
// Expected at site-a, now = today 18:00 UTC:
//
//	on site now 1; visits today 3 / season 4;
//	avg on site today (Ann 120, Bob 180) = 150 over n=2; season (Zed 120, Ann 120, Bob 180) = 140 over n=3;
//	travel out 1/1; trips dispatched 1/1; arrived 0; in transit 1.
//
// The two gate check-ins each author a destination-less trip, which must NOT
// be counted as travel — that is what dispatched=1 pins.
func TestAnalyticsFiguresOverASeededLog(t *testing.T) {
	st, _ := anSeedSiteA(t)
	fig, err := BuildAnalytics(st, time.UnixMilli(vrAt(0, 18, 0)), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	a := anSite(t, fig, "site-a")
	if a.OnSiteNow != 1 {
		t.Errorf("on site now = %d, want 1 (Cal only)", a.OnSiteNow)
	}
	if a.VisitsToday != 3 || a.VisitsSeason != 4 {
		t.Errorf("visits today/season = %d/%d, want 3/4", a.VisitsToday, a.VisitsSeason)
	}
	if a.AvgOnSiteToday.N != 2 || a.AvgOnSiteToday.Mean != 150 {
		t.Errorf("avg on site today = %+v, want n=2 mean=150", a.AvgOnSiteToday)
	}
	if a.AvgOnSiteSeason.N != 3 || int(a.AvgOnSiteSeason.Mean+0.5) != 140 {
		t.Errorf("avg on site season = %+v, want n=3 mean=140", a.AvgOnSiteSeason)
	}
	if a.TravelOutToday != 1 || a.TravelOutSeason != 1 {
		t.Errorf("travel out today/season = %d/%d, want 1/1", a.TravelOutToday, a.TravelOutSeason)
	}
	if a.TravelInToday != 0 || a.TravelInSeason != 0 {
		t.Errorf("travel in today/season = %d/%d, want 0/0 (gate check-in arrivals are not travel)", a.TravelInToday, a.TravelInSeason)
	}
	if fig.TripsDispatched != 1 || fig.TripsDispatchedToday != 1 || fig.TripsArrived != 0 || fig.TripsInTransit != 1 {
		t.Errorf("trips dispatched %d/%d arrived %d in transit %d, want 1/1, 0, 1",
			fig.TripsDispatched, fig.TripsDispatchedToday, fig.TripsArrived, fig.TripsInTransit)
	}
	if got := fig.DistinctOnSite(map[string]bool{"site-a": true}); got != 1 {
		t.Errorf("distinct on site over {site-a} = %d, want 1", got)
	}
	if fig.TimesUnresolved != 0 {
		t.Errorf("times unresolved = %d, want 0", fig.TimesUnresolved)
	}
}

// TestAnalyticsTodayIsTheLocalDayOfNow — the same log read the next day has
// nothing "today" and the same season: yesterday's work does not leak into
// today's figures, and today's does not vanish from the season.
func TestAnalyticsTodayIsTheLocalDayOfNow(t *testing.T) {
	st, _ := anSeedSiteA(t)
	fig, err := BuildAnalytics(st, time.UnixMilli(vrAt(1, 9, 0)), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	a := anSite(t, fig, "site-a")
	if a.VisitsToday != 0 || a.TravelOutToday != 0 || fig.TripsDispatchedToday != 0 || a.AvgOnSiteToday.N != 0 {
		t.Errorf("next day: today figures must be empty, got visits %d out %d dispatched %d avgN %d",
			a.VisitsToday, a.TravelOutToday, fig.TripsDispatchedToday, a.AvgOnSiteToday.N)
	}
	if a.VisitsSeason != 4 || fig.TripsDispatched != 1 {
		t.Errorf("season must be unchanged: visits %d dispatched %d, want 4 and 1", a.VisitsSeason, fig.TripsDispatched)
	}
}

// TestAnalyticsCrossSiteArrivalAndFreshnessMark — the trip reaches site-b,
// which holds BOTH streams. At site-b: Bob is on site there; the trip is
// arrived, not in transit; travel in 1 at site-b; site-a's own row still
// shows Cal. The site-a row also carries its watermark and latest-event
// instant (the freshness a reader needs beside a peer's numbers): the last
// event site-a authored is Bob's 13:00 dispatch.
func TestAnalyticsCrossSiteArrivalAndFreshnessMark(t *testing.T) {
	a, trip := anSeedSiteA(t)
	b := newDomainTestStore(t)
	setB := vrFixedClock(b, store.ClockSynced)
	evs, err := a.Events("site-a", 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if _, err := b.Apply(ev); err != nil {
			t.Fatal(err)
		}
	}
	setB(vrAt(0, 14, 0))
	if _, err := RecordExpectedArrival(b, "site-b", 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}

	fig, err := BuildAnalytics(b, time.UnixMilli(vrAt(0, 18, 0)), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	sb, sa := anSite(t, fig, "site-b"), anSite(t, fig, "site-a")
	if sb.OnSiteNow != 1 || sb.TravelInToday != 1 || sb.TravelInSeason != 1 {
		t.Errorf("site-b on site %d travel in %d/%d, want 1 and 1/1", sb.OnSiteNow, sb.TravelInToday, sb.TravelInSeason)
	}
	if fig.TripsArrived != 1 || fig.TripsArrivedToday != 1 || fig.TripsInTransit != 0 {
		t.Errorf("arrived %d/%d in transit %d, want 1/1 and 0", fig.TripsArrived, fig.TripsArrivedToday, fig.TripsInTransit)
	}
	if sa.OnSiteNow != 1 {
		t.Errorf("site-a on site = %d, want 1 (Cal)", sa.OnSiteNow)
	}
	if !sa.LastEventOK || sa.LastEventMS != vrAt(0, 13, 0) {
		t.Errorf("site-a latest event = %d ok=%v, want the 13:00 dispatch %d", sa.LastEventMS, sa.LastEventOK, vrAt(0, 13, 0))
	}
	if sa.AppliedSeq == 0 || sa.EventCount == 0 {
		t.Errorf("site-a watermark missing: %+v", sa)
	}
	// Bob (site-b) + Cal (site-a) are two people; a person open at both would count once.
	if got := fig.DistinctOnSite(map[string]bool{"site-a": true, "site-b": true}); got != 2 {
		t.Errorf("distinct on site over both = %d, want 2", got)
	}
	if got := fig.DistinctOnSite(map[string]bool{"site-b": true}); got != 1 {
		t.Errorf("distinct on site over {site-b} = %d, want 1", got)
	}
}
