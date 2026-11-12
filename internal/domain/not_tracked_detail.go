package domain

import (
	"sitecontrol/internal/store"
)

// NotTrackedDetail is what a NOT TRACKED row prints beside its names, beyond
// the NotTrackedRow itself (fix round 3, FINDINGS-R5 F10 — L01-6, L07
// O-L07-2): WHEN the people became not tracked, and WHICH TRUCK the run is
// now. Kept out of NotTrackedRow on purpose: membership is NotTracked's
// question, and these two are display facts read only by the muster.
type NotTrackedDetail struct {
	// AtMS is the effective recorded instant (time corrections applied) the
	// row's people became not tracked: the arrival that came in without them,
	// or — on a TakenOffByCorrection row — the latest trip correction that
	// took any of them off the run. 0 when this post cannot resolve it (a
	// dangling key under partial replication): the row then prints no time
	// rather than a guessed one.
	AtMS int64
	// AtSite is the post that recorded that event (the arrival, or the
	// correction) — the clock AtMS was read from, named beside the time
	// on the sheet (FIX3 F9's rule: a time is read on some post's clock).
	AtSite string
	// Vehicle is the run's LIVE vehicle (LiveVehicles.Compose — the same
	// composition the road block, the boards and the alert strip use), so
	// the sheet never names a truck number the rest of the post has moved
	// on from. DispatchedAs is the row's verbatim as-dispatched number when
	// it differs, "" otherwise.
	Vehicle      string
	DispatchedAs string
}

// NotTrackedDetails returns one NotTrackedDetail per row, index-aligned.
func NotTrackedDetails(st *store.Store, rows []NotTrackedRow) ([]NotTrackedDetail, error) {
	out := make([]NotTrackedDetail, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	live, err := NewLiveVehicles(st)
	if err != nil {
		return nil, err
	}
	for i, r := range rows {
		d := NotTrackedDetail{Vehicle: r.Vehicle}
		c, err := EffectiveTrip(st, r.TripID)
		if err != nil {
			return nil, err
		}
		if c != nil {
			display, _, err := live.Compose(*c)
			if err != nil {
				return nil, err
			}
			if display != "" {
				d.Vehicle = display
			}
		}
		if d.Vehicle != r.Vehicle && r.Vehicle != "" {
			d.DispatchedAs = r.Vehicle
		}
		key, ok, err := notTrackedSinceKey(st, r)
		if err != nil {
			return nil, err
		}
		if ok {
			// Fail-closed reads (a dangling target) print no time; they are
			// never fatal to the sheet.
			if rt, _, rerr := ResolveOccurredAt(st, key); rerr == nil {
				d.AtMS, d.AtSite = rt.InstantUTCms, key.SiteID
			}
		}
		out[i] = d
	}
	return out, nil
}

// notTrackedSinceKey is the event the row's people became not tracked at.
func notTrackedSinceKey(st *store.Store, r NotTrackedRow) (store.Key, bool, error) {
	if !r.TakenOffByCorrection {
		state, err := TripStateByID(st, r.TripID)
		if err != nil || state == nil || state.ArrivalID == nil {
			return store.Key{}, false, err
		}
		a, err := arrivalByID(st, *state.ArrivalID)
		if err != nil || a == nil {
			return store.Key{}, false, err
		}
		return store.Key{SiteID: a.SiteID, NodeEpoch: a.NodeEpoch, Seq: a.Seq}, true, nil
	}
	rows, err := readTripAmendmentsFor(st, r.TripID)
	if err != nil {
		return store.Key{}, false, err
	}
	named := map[string]bool{}
	for _, p := range r.Persons {
		named[p.PersonID] = true
	}
	// "Latest" is by the correction's RECORDED instant (time corrections
	// applied), not store.Key.Less: Key.Less is site-major, so when both ends
	// corrected the run every correction of the alphabetically later post
	// outranked every one of the other (L03-03 — Ice Road's 02:15 was printed
	// for a person Ice Pad took off at 02:18). Ties, and corrections whose
	// time cannot be resolved here (a dangling target, ranked earliest), fall
	// back to Key.Less so every post picks the same one.
	type ranked struct {
		key store.Key
		at  int64
	}
	later := func(a, b ranked) bool { // a is later than b
		if a.at != b.at {
			return a.at > b.at
		}
		return b.key.Less(a.key)
	}
	var best, fallback ranked
	var found, foundFallback bool
	for _, a := range rows {
		hit := false
		for _, id := range a.OccupantsRemove {
			if named[id] {
				hit = true
			}
		}
		driverChange := a.Driver != nil || len(a.OccupantsRemove) > 0
		if !hit && !driverChange {
			continue
		}
		cand := ranked{key: a.Key}
		if rt, _, rerr := ResolveOccurredAt(st, a.Key); rerr == nil {
			cand.at = rt.InstantUTCms
		}
		if hit && (!found || later(cand, best)) {
			best, found = cand, true
		}
		// A replaced driver dropped by a correction that did not name them
		// in the removal (a merged-away id): the latest driver change.
		if driverChange && (!foundFallback || later(cand, fallback)) {
			fallback, foundFallback = cand, true
		}
	}
	if found {
		return best.key, true, nil
	}
	return fallback.key, foundFallback, nil
}
