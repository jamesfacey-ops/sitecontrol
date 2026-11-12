// away_origin_scope_test.go — the pad list belongs to the post that
// dispatched the run (James, 2026-08-21: "on the pad muster roster it should
// not display the persons going to the other site — the only one that would
// have any concern about them is the ice road site control").
//
// Trips REPLICATE. Every post's store holds every post's pad runs, so the
// unscoped predicate put Ice Road's pad crew on the Pad's muster, where the
// reader has no relationship to them at all: those people never came to the
// Pad and never will on that run. The scoped predicate is what the muster
// renders; the unscoped one still answers the wider question for the
// surfaces that were NOT part of the ruling.
//
// Both directions are pinned here, and both matter. A filter that returned
// nothing would satisfy "the peer's crew is absent" on its own, which is why
// every absence assertion below sits beside a presence assertion built from
// the SAME replica.
package domain

import (
	"testing"

	"sitecontrol/internal/store"
)

// padRunFrom records a walk-in at siteID and sends that person out to the
// pad on a run ORIGINATING at siteID, all authored into st. One store
// holding two posts' streams is exactly what replication produces.
func padRunFrom(t *testing.T, st *store.Store, siteID, name string) string {
	t.Helper()
	p, err := CreatePerson(st, siteID, 1, name, nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	walkIn, err := DispatchLocalCheckIn(st, siteID, 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "gate walk-in", DriverPersonID: p.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, st, siteID, "gate walk-in"), DriverPersonID: p.PersonID,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordExpectedArrival(st, siteID, 1, walkIn.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	driver, err := CreatePerson(st, siteID, 1, name+" Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	pad := "conneco"
	run, err := Dispatch(st, siteID, 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "BUS " + siteID, DriverPersonID: driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, st, siteID, "BUS "+siteID), DriverPersonID: driver.PersonID,
		OccupantPersonIDs:       []string{p.PersonID},
		DestinationUncontrolled: &pad,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordExpectedArrivalExcluding(st, siteID, 1, run.TripID,
		[]string{p.PersonID}, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	return p.PersonID
}

func TestOutAtUncontrolledFromKeepsOnlyThisPostsPadRuns(t *testing.T) {
	w := newC6World(t)
	// One replica, two posts' pad runs in it — the state every post is
	// actually in once sync has run.
	fromA := padRunFrom(t, w.a, "site-a", "Ann Aroad")
	fromB := padRunFrom(t, w.a, "site-b", "Ben Bpad")

	// Precondition, stated rather than assumed: the UNSCOPED predicate sees
	// both. Without this, every "the peer's crew is absent" assertion below
	// would also pass on a replica that simply never held the peer's run.
	all, err := OutAtUncontrolled(w.a)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := padListed(all, fromA); !ok {
		t.Fatalf("precondition: the site-a pad run is not in the unscoped answer: %+v", all)
	}
	if _, ok := padListed(all, fromB); !ok {
		t.Fatalf("precondition: the site-b pad run is not in the unscoped answer — this replica does not hold both posts' runs, so nothing here is being scoped: %+v", all)
	}

	// Each post sees its own run and ONLY its own. Asserted as a pair: the
	// presence half is what makes the absence half mean "filtered" rather
	// than "empty".
	for _, tc := range []struct {
		site, mine, theirs string
	}{
		{"site-a", fromA, fromB},
		{"site-b", fromB, fromA},
	} {
		got, err := OutAtUncontrolledFrom(w.a, tc.site)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := padListed(got, tc.mine); !ok {
			t.Errorf("%s does not list the crew IT sent to the pad — the post that dispatched them is the one post that must: %+v", tc.site, got)
		}
		if _, ok := padListed(got, tc.theirs); ok {
			t.Errorf("%s lists the OTHER post's pad crew. Those people never came here and never will on that run; the sheet's reader has no relationship to them: %+v", tc.site, got)
		}
		for _, o := range got {
			if o.OriginSite != tc.site {
				t.Errorf("%s: row from origin %q leaked through the filter", tc.site, o.OriginSite)
			}
		}
	}

	// A post with no pad runs of its own gets nothing — not "everything it
	// can see".
	none, err := OutAtUncontrolledFrom(w.a, "site-c")
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Errorf("a post that dispatched nobody to a pad still sees %d row(s): %+v", len(none), none)
	}
}

// OriginSite has to be carried on the row, not re-derived by the caller: it
// is what the filter reads, and a row that does not know where it came from
// cannot be scoped by any surface.
func TestUncontrolledOutCarriesItsOriginSite(t *testing.T) {
	w := newC6World(t)
	padRunFrom(t, w.a, "site-a", "Ann Aroad")
	all, err := OutAtUncontrolled(w.a)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) == 0 {
		t.Fatal("precondition: no pad runs recorded at all")
	}
	for _, o := range all {
		if o.OriginSite == "" {
			t.Errorf("pad row for %q carries no OriginSite — the muster cannot tell whose run it is: %+v", o.Pad, o)
		}
	}
}
