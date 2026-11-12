// trip_return.go — a TURNED-BACK truck (James, 2026-10-03: on the origin
// post's OUTBOUND board, an "Arrived back here" door that records the truck
// returning to its origin — closing the trip as returned, entering its
// occupants back on site at the origin, clearing its overdue alert at both
// posts — authored like a normal arrival so it replicates and replays
// deterministically; refused if the trip already arrived anywhere).
//
// NO NEW EVENT KIND AND NO FOLD CHANGE. The return is an ordinary EXPLICIT
// trip_arrival naming the trip, authored on the ORIGIN's own stream — the
// same event the inbound board's "Arrived" and road work's "Back from the
// road" author (RecordExpectedArrivalExcluding; D-96: "the vehicle arrives
// where it arrives"). So, with nothing new to replicate or replay:
//
//   - the fold binds it in Pass 1 like any explicit arrival, and the trip's
//     state becomes `arrived` at every post that holds both facts;
//   - its visits open at the post that recorded it — the origin — so the
//     crew is back on the origin's muster (and on no other: the dispatch
//     closed their visits there, and nobody arrived anywhere else);
//   - every scan (the destination's inbound one and the origin's NEW-a
//     outbound one) reads OPEN trips only, so the overdue alert clears at
//     each post as soon as the arrival is there — the same way any arrival
//     clears it.
//
// "RETURNED" IS DERIVED, at read time: an arrived trip WITH a destination
// post whose binding arrival was recorded at the trip's own origin
// (TripsReturnedToOrigin). Road-work runs (no destination) come back to
// their origin by design and are never labelled returned.
//
// What this does NOT change: a trip and an arrival recorded concurrently at
// the far end during a partition are two explicit arrivals for one trip —
// the fold's existing rule (first in canonical order binds; both visit sets
// stand, W6's "also on site at" flag shows the double presence) applies
// unchanged. The refusal below sees only arrivals already replicated here.
//
// ⚠ KNOWN LIMIT (James, 2026-10-04: the simultaneous cross-post arrival
// double is accepted, documented and not fixed — FINDINGS-R5 Q-R5-4 / F5 /
// A4). The same rule applies with NO partition at all when the destination
// presses "Arrived" and the origin presses "Arrived back here" inside the
// poke window. A4 measured it: legs released 0 ms apart gave two
// trip_arrival rows at all 5 posts in 3 of 3 runs, trip_state `arrived`, and
// the driver with open visits at BOTH posts; at 200 ms apart the second leg
// was refused 409 "NOT RECORDED" (and at 0.5 / 2 / 6 s, L14). Both musters
// flag the double — "ALSO ON SITE AT <the other post>" (visit_elsewhere.go)
// — so it fails toward double-listed, never lost. It is the same class as
// W6's double-open visit: no lock spans two posts, and the only fix would
// be a cross-post handshake this design does not have. D18's SAME-post race
// is closed (the transaction above); this one is not, by decision.
//
// James, 2026-10-03 overrides D-73/D-78 for this one case: a trip is still
// never CANCELLED, but a truck that turned back is recorded as back.
package domain

import (
	"fmt"

	"sitecontrol/internal/store"
)

// RecordReturnToOrigin authors the turned-back truck's arrival at its own
// origin. Refusals, all before anything is written: the trip is unknown
// here; this post is not the trip's origin; the trip has no destination
// post (a local check-in or a road-work run — road work has its own "Back
// from the road" door); the trip already arrived anywhere this post knows
// of. Everything else (stayed-behind ticks, orientation hold-out) is the
// ordinary expected arrival's.
//
// D18 (FINDINGS-R4): the refusals and the write are one transaction (see
// RecordExpectedArrivalExcluding), so two "Arrived back here" presses sent
// together author one arrival and one refusal, never two arrivals.
func RecordReturnToOrigin(st *store.Store, siteID string, nodeEpoch int64, tripID string, stayedPersonIDs []string, recordedBy *string, occurredAtMS int64, timeSource string, hasOrientation bool) (Arrival, error) {
	var arrival Arrival
	err := st.Transact("return to origin", func(tx *store.Store) error {
		var err error
		arrival, err = recordReturnToOrigin(tx, siteID, nodeEpoch, tripID, stayedPersonIDs, recordedBy, occurredAtMS, timeSource, hasOrientation)
		return err
	})
	if err != nil {
		return Arrival{}, err
	}
	return arrival, nil
}

func recordReturnToOrigin(st *store.Store, siteID string, nodeEpoch int64, tripID string, stayedPersonIDs []string, recordedBy *string, occurredAtMS int64, timeSource string, hasOrientation bool) (Arrival, error) {
	trip, err := TripByID(st, tripID)
	if err != nil {
		return Arrival{}, fmt.Errorf("domain: return to origin: %w", err)
	}
	if trip == nil {
		return Arrival{}, fmt.Errorf("domain: return to origin: trip %s is not known at this post", tripID)
	}
	if trip.OriginSite != siteID {
		// FIX5 (R7 L03-01): the vehicle, never the trip id (the F13/F3
		// rule the already-arrived arm below follows). ⟦was: "trip %s was
		// sent out by …", tripID⟧
		return Arrival{}, fmt.Errorf("domain: return to origin: %s was sent out by %s, not this post — only the post that sent a truck out can record it back there; record it here as an arrival instead", liveVehicleName(st, tripID), siteNameAt(st, trip.OriginSite))
	}
	if trip.DestinationSite == nil {
		// ⟦was: "trip %s was not sent …", tripID⟧ — same rule.
		return Arrival{}, fmt.Errorf("domain: return to origin: %s was not sent to another post, so there is nothing to turn back from", liveVehicleName(st, tripID))
	}
	state, err := TripStateByID(st, tripID)
	if err != nil {
		return Arrival{}, fmt.Errorf("domain: return to origin: %w", err)
	}
	if state != nil && (state.State == TripStateArrived || state.State == TripStateArrivedAfterCancel) {
		// L03-06 (F13): name the vehicle, never the trip id or its state.
		where := "at another post"
		if state.ArrivalID != nil {
			if a, aerr := arrivalByID(st, *state.ArrivalID); aerr == nil && a != nil {
				where = "at " + siteNameAt(st, a.SiteID)
				if a.SiteID == siteID {
					where = "here"
				}
			}
		}
		return Arrival{}, fmt.Errorf("domain: return to origin: %s has already been recorded as arrived %s — its arrival is on record, so it cannot also be recorded back here. Nothing was recorded", liveVehicleName(st, tripID), where)
	}
	return RecordExpectedArrivalExcluding(st, siteID, nodeEpoch, tripID, stayedPersonIDs, recordedBy, occurredAtMS, timeSource, hasOrientation)
}

func arrivalByID(st *store.Store, arrivalID string) (*Arrival, error) {
	return oneRow[Arrival](st, "trip_arrival", store.Eq{Column: "arrival_id", Value: arrivalID})
}

// TripsReturnedToOrigin is the read-time "RETURNED" fact: the trips that
// were sent to another post and whose binding arrival was recorded back at
// their own origin (James, 2026-10-03; see the file comment).
func TripsReturnedToOrigin(st *store.Store) (map[string]bool, error) {
	states, err := TripStates(st)
	if err != nil {
		return nil, err
	}
	bound := map[string]string{} // arrival id -> trip id
	for _, s := range states {
		if s.ArrivalID != nil && (s.State == TripStateArrived || s.State == TripStateArrivedAfterCancel) {
			bound[*s.ArrivalID] = s.TripID
		}
	}
	out := map[string]bool{}
	if len(bound) == 0 {
		return out, nil
	}
	arrivals, err := Arrivals(st)
	if err != nil {
		return nil, err
	}
	trips, err := Trips(st)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]Trip, len(trips))
	for _, t := range trips {
		byID[t.TripID] = t
	}
	for _, a := range arrivals {
		tripID, ok := bound[a.ArrivalID]
		if !ok {
			continue
		}
		if t, ok := byID[tripID]; ok && t.DestinationSite != nil && a.SiteID == t.OriginSite {
			out[tripID] = true
		}
	}
	return out, nil
}
