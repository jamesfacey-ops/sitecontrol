package domain

import (
	"testing"
	"time"

	"sitecontrol/internal/store"
)

// TestFix4NotTrackedLatestCorrectionByTimeNotSite is L03-03: both ends of a
// run take someone off it. The row must carry the LATER correction by its
// recorded instant, and that correction's post's clock — whatever the two
// posts' ids sort as. FAIL BEFORE: store.Key.Less is site-major, so the
// destination ("site-b" > "site-a") won though it corrected first.
func TestFix4NotTrackedLatestCorrectionByTimeNotSite(t *testing.T) {
	w := newFix2World(t)
	now := time.Now().UnixMilli()
	early, late := now-10*60*1000, now-5*60*1000
	if _, err := AmendTrip(w.dest, "site-b", 1, TripAmendment{TripID: w.trip.TripID, OccupantsRemove: []string{w.p.PersonID}}, early, "entered"); err != nil {
		t.Fatalf("destination correction refused: %v", err)
	}
	w.sync(t)
	if _, err := AmendTrip(w.origin, "site-a", 1, TripAmendment{TripID: w.trip.TripID, DriverPersonID: &w.b.PersonID, OccupantsRemove: []string{w.a.PersonID}}, late, "entered"); err != nil {
		t.Fatalf("origin correction refused: %v", err)
	}
	w.sync(t)
	for name, st := range map[string]*store.Store{"origin": w.origin, "destination": w.dest} {
		rows, err := NotTracked(st)
		if err != nil {
			t.Fatal(err)
		}
		details, err := NotTrackedDetails(st, rows)
		if err != nil {
			t.Fatal(err)
		}
		seen := false
		for i, r := range rows {
			for _, p := range r.Persons {
				if p.PersonID != w.a.PersonID {
					continue
				}
				seen = true
				if d := details[i]; d.AtSite != "site-a" || d.AtMS != late {
					t.Errorf("at the %s post, %s's row reads at %d by %q, want %d by \"site-a\" (the later correction)", name, p.PersonID, d.AtMS, d.AtSite, late)
				}
			}
		}
		if !seen {
			t.Fatalf("at the %s post, NotTracked = %+v lists nobody taken off by the origin", name, rows)
		}
	}
}
