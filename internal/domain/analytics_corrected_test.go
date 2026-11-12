// analytics_corrected_test.go — FINDINGS-R4 D2 (AN-PANIC-1): the first
// analytics witness over a log that carries a time correction. Before the
// fix, BuildAnalytics resolved a corrected instant from INSIDE the
// st.EachEvent callback, re-entering the store's single connection: the
// reentrancy guard panicked after its 10 s wait, so one routine correction
// took /analytics and every /analytics/site/* down at every post.
package domain

import (
	"fmt"
	"testing"
	"time"

	"sitecontrol/internal/store"
)

// anBuildNoPanic runs BuildAnalytics and turns the guard's REENTRANCY panic
// into a test failure, so the regression reads as a verdict, not a crash of
// the whole package.
func anBuildNoPanic(t *testing.T, st *store.Store, now time.Time) (fig AnalyticsFigures, err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("BuildAnalytics panicked: %v", r)
		}
	}()
	return BuildAnalytics(st, now, time.UTC)
}

// TestAnalyticsOverACorrectedInstant — anSeedSiteA's log, then at 14:00 an
// officer corrects Bob's dispatch (today 13:00, which also closes his visit)
// to today 11:00. Hand-derived, now = today 18:00 UTC:
//
//	Bob on site 10:00 -> 11:00 = 60 min (was 180);
//	avg on site today (Ann 120, Bob 60) = 90 over n=2;
//	season (Zed 120, Ann 120, Bob 60) = 100 over n=3;
//	the trip still dispatched today (11:00 is today): 1/1; times unresolved 0.
//
// The same instant through the report path: Bob's record closes at 11:00,
// labelled claimed-corrected.
func TestAnalyticsOverACorrectedInstant(t *testing.T) {
	st, trip := anSeedSiteA(t)
	setClock := vrFixedClock(st, store.ClockSynced)
	setClock(vrAt(0, 14, 0))
	dispatchKey := store.Key{SiteID: trip.OriginSite, NodeEpoch: trip.NodeEpoch, Seq: trip.Seq}
	if _, err := CorrectOccurredAt(st, "site-a", 1, TimeCorrection{
		Target: dispatchKey, CorrectedOccurredAtMS: vrAt(0, 11, 0), RecordedBy: "officer",
	}); err != nil {
		t.Fatal(err)
	}

	fig, err := anBuildNoPanic(t, st, time.UnixMilli(vrAt(0, 18, 0)))
	if err != nil {
		t.Fatal(err)
	}
	a := anSite(t, fig, "site-a")
	if a.AvgOnSiteToday.N != 2 || a.AvgOnSiteToday.Mean != 90 {
		t.Errorf("avg on site today = %+v, want n=2 mean=90 (Bob's close corrected to 11:00)", a.AvgOnSiteToday)
	}
	if a.AvgOnSiteSeason.N != 3 || a.AvgOnSiteSeason.Mean != 100 {
		t.Errorf("avg on site season = %+v, want n=3 mean=100", a.AvgOnSiteSeason)
	}
	if fig.TripsDispatched != 1 || fig.TripsDispatchedToday != 1 || a.TravelOutToday != 1 {
		t.Errorf("trips dispatched %d/%d, travel out today %d, want 1/1, 1", fig.TripsDispatched, fig.TripsDispatchedToday, a.TravelOutToday)
	}
	if fig.TimesUnresolved != 0 {
		t.Errorf("times unresolved = %d, want 0 (the correction resolves)", fig.TimesUnresolved)
	}
	if !a.LastEventOK || a.LastEventMS != vrAt(0, 14, 0) {
		t.Errorf("last event = %d (ok=%v), want the 14:00 correction act itself", a.LastEventMS, a.LastEventOK)
	}

	// Moving the dispatch to YESTERDAY must move it out of today — the
	// figure analytics reads inside its own replay, not via VisitReport.
	setClock(vrAt(0, 15, 0))
	if _, err := CorrectOccurredAt(st, "site-a", 1, TimeCorrection{
		Target: dispatchKey, CorrectedOccurredAtMS: vrAt(-1, 23, 0), RecordedBy: "officer",
	}); err != nil {
		t.Fatal(err)
	}
	fig, err = anBuildNoPanic(t, st, time.UnixMilli(vrAt(0, 18, 0)))
	if err != nil {
		t.Fatal(err)
	}
	a = anSite(t, fig, "site-a")
	if fig.TripsDispatched != 1 || fig.TripsDispatchedToday != 0 || a.TravelOutToday != 0 || a.TravelOutSeason != 1 {
		t.Errorf("after re-correcting to yesterday: trips %d/%d, travel out today/season %d/%d, want 1/0, 0/1",
			fig.TripsDispatched, fig.TripsDispatchedToday, a.TravelOutToday, a.TravelOutSeason)
	}

	recs, _, err := VisitReport(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	var bob *VisitRecord
	for i := range recs {
		if recs[i].CloseKey == dispatchKey {
			bob = &recs[i]
		}
	}
	if bob == nil {
		t.Fatalf("no visit record closed by the dispatch %v", dispatchKey)
	}
	if bob.CloseLabel != timeClaimedCorrected || bob.ClosedAtMS != vrAt(-1, 23, 0) {
		t.Errorf("report close = %d %q, want %d %q", bob.ClosedAtMS, bob.CloseLabel, vrAt(-1, 23, 0), timeClaimedCorrected)
	}
}

// TestAnalyticsOverADanglingCorrection — a correction whose target this
// post does not hold (legal: next-seq is predictable) resolves fail-closed,
// and analytics still builds: the same "no store read inside the replay"
// path, through the resolver's error branch.
func TestAnalyticsOverADanglingCorrection(t *testing.T) {
	st, _ := anSeedSiteA(t)
	vrFixedClock(st, store.ClockSynced)(vrAt(0, 14, 0))
	if _, err := CorrectOccurredAt(st, "site-a", 1, TimeCorrection{
		Target: store.Key{SiteID: "site-z", NodeEpoch: 1, Seq: 9}, CorrectedOccurredAtMS: vrAt(0, 11, 0), RecordedBy: "officer",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := anBuildNoPanic(t, st, time.UnixMilli(vrAt(0, 18, 0))); err != nil {
		t.Fatal(err)
	}
}
