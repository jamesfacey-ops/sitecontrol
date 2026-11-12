package domain

// aboard_elsewhere.go — L17-15 (F16, fix3 2026-10-04): a person checked in at
// a post while still recorded aboard ANOTHER post's open run.
//
// A run from ice-road to ice-pad that npf checks somebody in off is a third
// post's business only in one way: the person is now on npf's muster AND on
// that run's sheet, counted twice, and npf is the one post that knows it. The
// check-in is not refused — the person is physically at npf's gate, and
// presence is never refused on a record another post may have wrong — but it
// is said, so the officer radios the run's posts and the sheet gets
// corrected. Read-only; authors nothing.

import (
	"fmt"

	"sitecontrol/internal/store"
)

// AboardRun is one open run a person is still recorded aboard.
type AboardRun struct {
	PersonID string
	TripID   string
	Vehicle  string // the run's live vehicle number (LiveVehicles.Compose)
	// Origin and Destination are display names (siteName); Destination is
	// "" for a run with no destination post (road work, an uncontrolled pad).
	Origin, Destination string
}

// AboardOpenRunsAwayFrom returns, for each of personIDs, the open run that
// still carries them on its effective list and that neither left from nor
// is bound for siteID — "another post's open trip" from siteID's point of
// view. A run that starts or ends at siteID is siteID's own (its arrival
// or its departure) and is not this question. At most one run per person,
// in personIDs order; a person aboard nothing is absent.
func AboardOpenRunsAwayFrom(st *store.Store, siteID string, personIDs []string, siteName func(string) string) ([]AboardRun, error) {
	if len(personIDs) == 0 {
		return nil, nil
	}
	open, err := openTripStates(st)
	if err != nil {
		return nil, fmt.Errorf("domain: aboard elsewhere: %w", err)
	}
	var runs []ComposedTrip
	// F15: a person back at a run's origin is off it everywhere (StillAboard).
	aboard := map[string][]string{}
	for _, s := range open {
		c, err := EffectiveTrip(st, s.TripID)
		if err != nil {
			return nil, fmt.Errorf("domain: aboard elsewhere: %w", err)
		}
		if c == nil || c.Trip.OriginSite == siteID {
			continue
		}
		if c.Trip.DestinationSite != nil && *c.Trip.DestinationSite == siteID {
			continue
		}
		ids, err := StillAboard(st, c)
		if err != nil {
			return nil, fmt.Errorf("domain: aboard elsewhere: %w", err)
		}
		aboard[c.Trip.TripID] = ids
		runs = append(runs, *c)
	}
	if len(runs) == 0 {
		return nil, nil
	}
	live, err := NewLiveVehicles(st)
	if err != nil {
		return nil, fmt.Errorf("domain: aboard elsewhere: %w", err)
	}
	var out []AboardRun
	seen := map[string]bool{}
	for _, id := range personIDs {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		for _, c := range runs {
			if !containsString(aboard[c.Trip.TripID], id) {
				continue
			}
			vehicle, _, err := live.Compose(c)
			if err != nil {
				return nil, fmt.Errorf("domain: aboard elsewhere: %w", err)
			}
			r := AboardRun{PersonID: id, TripID: c.Trip.TripID, Vehicle: vehicle, Origin: siteName(c.Trip.OriginSite)}
			if c.Trip.DestinationSite != nil {
				r.Destination = siteName(*c.Trip.DestinationSite)
			}
			out = append(out, r)
			break
		}
	}
	return out, nil
}
