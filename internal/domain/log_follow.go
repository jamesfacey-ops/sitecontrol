// log_follow.go — FIX17 (perf, 2026-10-06): the whole-log replay behind the
// visit report and the analytics figures, kept up to date incrementally.
//
// VisitReport and AnalyticsWindow each walked EVERY event in the log on every
// call — ~40k events at Ice Road at season scale, decoded twice, on every
// gate and status page (the shift panel) and again after every write, since
// a write discards store.Memo's values. The log is append-only and each
// stream's rows are exactly 1..AppliedSeq (store.Watermarks), so the walk can
// be RESUMED: the follower remembers how far it consumed each stream and, on
// the next call, feeds only the events past that point to the same per-site
// replay the full walk would have used.
//
// RESUMING IS ONLY TAKEN WHEN IT IS THE SAME WALK. A site's replay walks its
// epochs ascending (§8.3), so new events may be fed only if, for every site,
// all of them sort AFTER everything already consumed for that site: growth at
// the tail of the site's last consumed epoch, or a new later epoch. Anything
// else — a stream that shrank or vanished, an earlier epoch growing beneath a
// later one (rejoin recovering own prior-epoch history) — starts the replay
// over from zero, exactly the old full walk. So does a failed event (the
// replay is left half-applied): the next call rebuilds, and fails again the
// same way, as the full walk did.
//
// What is retained is ONLY what the replay derives from envelopes, which
// never change once stored. Everything that can change under an old event —
// a time correction re-resolving its instant, a merge re-resolving its
// person — is applied afresh by the callers on every snapshot, as before.
//
// Never inside a Transact view (store.Retained hands a view a fresh
// follower): a view can see events that may yet roll back.
package domain

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"sitecontrol/internal/store"
)

type logFollower struct {
	mu       sync.Mutex
	consumed []store.Stream
	sites    map[string]*followedSite
}

// followedSite is one site's replay state: the visit replay, the newest-
// event facts, and the site's trip events, each in that site's walk order.
type followedSite struct {
	replay   *visitReplay
	records  []*VisitRecord
	fresh    analyticsFreshness
	hasEvent bool
	trips    []followedTripEvent
	last     map[int64]followedEnd // node_epoch -> the stream's last event
}

// followedTripEvent is one trip_dispatched, trip_arrival or trip_cancelled
// as decoded on the walk — exactly one of the three payloads is set. The
// payloads are shared and read-only once decoded.
type followedTripEvent struct {
	site       string
	key        store.Key
	basis      timeBasis
	dispatched *TripDispatched
	arrival    *TripArrival
	cancelled  *TripCancelled
}

// followedEnd is a stream's last event, for the instant a reader prints
// beside how far the stream has reached (MomentStream.Last).
type followedEnd struct {
	key   store.Key
	basis timeBasis
}

// logSnapshot is the follower's state at one census, handed out as the
// caller's own copy: records are fresh structs (visitReport writes the
// derived fields onto them), the slices and maps are new.
type logSnapshot struct {
	streams []store.Stream
	records []*VisitRecord
	fresh   map[string]analyticsFreshness
	trips   []followedTripEvent
	last    map[[2]any]followedEnd // (site, epoch) -> last event, streams that hold one
}

func newLogFollower() any { return &logFollower{} }

// followLog brings st's follower up to the store's current census and
// returns a snapshot of it; the visit records are copied only when asked.
func followLog(st *store.Store, withRecords bool) (*logSnapshot, error) {
	f := st.Retained("domain.logFollower", newLogFollower).(*logFollower)
	f.mu.Lock()
	defer f.mu.Unlock()
	streams, err := st.Watermarks()
	if err != nil {
		return nil, err
	}
	if f.ahead(streams) {
		// ⟦FIX17c⟧ A page reads one snapshot (store/readpool.go), and a
		// commit can land between its snapshot and this call: the follower
		// has then consumed events this snapshot does not hold. It must not
		// be rewound for an old snapshot — the next page would replay the log
		// from zero — nor hand this page events from after its snapshot. So
		// this page walks a follower of its own, and the shared one is reset
		// only when the newest committed census is behind it too: the log
		// itself shrank, the case resumable has always started over for.
		latest, err := st.Latest().Watermarks()
		if err != nil {
			return nil, err
		}
		if !f.ahead(latest) {
			own := &logFollower{sites: map[string]*followedSite{}}
			if err := own.advance(st, streams); err != nil {
				return nil, err
			}
			return own.snapshot(withRecords), nil
		}
	}
	if !f.resumable(streams) {
		f.consumed, f.sites = nil, map[string]*followedSite{}
	}
	if err := f.advance(st, streams); err != nil {
		f.consumed, f.sites = nil, nil // half-applied: the next call starts over
		return nil, err
	}
	return f.snapshot(withRecords), nil
}

// advance feeds f the events past f.consumed up to streams.
func (f *logFollower) advance(st *store.Store, streams []store.Stream) error {
	done := map[[2]any]int64{}
	for _, m := range f.consumed {
		done[[2]any{m.SiteID, m.NodeEpoch}] = m.AppliedSeq
	}
	for _, m := range streams {
		from := done[[2]any{m.SiteID, m.NodeEpoch}] + 1
		if m.AppliedSeq < from {
			continue
		}
		s := f.sites[m.SiteID]
		if s == nil {
			s = &followedSite{replay: newVisitReplay(m.SiteID), last: map[int64]followedEnd{}}
			f.sites[m.SiteID] = s
		}
		if err := st.EachEvent(m.SiteID, m.NodeEpoch, from, m.AppliedSeq, s.apply); err != nil {
			return err
		}
	}
	f.consumed = streams
	return nil
}

// ahead reports whether f has consumed events streams does not hold: a
// stream it walked is missing from streams or shorter there.
func (f *logFollower) ahead(streams []store.Stream) bool {
	if f.sites == nil {
		return false
	}
	now := map[[2]any]int64{}
	for _, m := range streams {
		now[[2]any{m.SiteID, m.NodeEpoch}] = m.AppliedSeq
	}
	for _, m := range f.consumed {
		if m.AppliedSeq > now[[2]any{m.SiteID, m.NodeEpoch}] {
			return true
		}
	}
	return false
}

// resumable reports whether feeding only the events past f.consumed is the
// same walk as replaying streams from zero (file comment).
func (f *logFollower) resumable(streams []store.Stream) bool {
	if f.sites == nil {
		return false
	}
	now := map[[2]any]int64{}
	for _, m := range streams {
		now[[2]any{m.SiteID, m.NodeEpoch}] = m.AppliedSeq
	}
	lastConsumed := map[string]int64{} // site -> highest epoch with consumed events
	for _, m := range f.consumed {
		seq, ok := now[[2]any{m.SiteID, m.NodeEpoch}]
		if !ok || seq < m.AppliedSeq {
			return false // a stream vanished or shrank: not the log we walked
		}
		if m.AppliedSeq > 0 {
			if e, ok := lastConsumed[m.SiteID]; !ok || m.NodeEpoch > e {
				lastConsumed[m.SiteID] = m.NodeEpoch
			}
		}
	}
	old := map[[2]any]int64{}
	for _, m := range f.consumed {
		old[[2]any{m.SiteID, m.NodeEpoch}] = m.AppliedSeq
	}
	for _, m := range streams {
		if m.AppliedSeq <= old[[2]any{m.SiteID, m.NodeEpoch}] {
			continue // nothing new here
		}
		if e, ok := lastConsumed[m.SiteID]; ok && m.NodeEpoch < e {
			return false // new events beneath a later epoch already walked
		}
	}
	return true
}

func (f *logFollower) snapshot(withRecords bool) *logSnapshot {
	snap := &logSnapshot{
		streams: append([]store.Stream(nil), f.consumed...),
		fresh:   map[string]analyticsFreshness{},
		last:    map[[2]any]followedEnd{},
	}
	seen := map[string]bool{}
	for _, m := range f.consumed {
		s := f.sites[m.SiteID]
		if s == nil {
			continue // a stream with no events yet
		}
		if e, ok := s.last[m.NodeEpoch]; ok {
			snap.last[[2]any{m.SiteID, m.NodeEpoch}] = e
		}
		if seen[m.SiteID] {
			continue
		}
		seen[m.SiteID] = true
		if withRecords {
			for _, r := range s.records {
				c := *r
				snap.records = append(snap.records, &c)
			}
		}
		if s.hasEvent {
			snap.fresh[m.SiteID] = s.fresh
		}
		snap.trips = append(snap.trips, s.trips...)
	}
	return snap
}

// apply feeds one event of this site's walk to every consumer: the visit
// replay (visitReplay.apply, unchanged), the newest-event facts, and the
// trip events the analytics, search, history and moment reads count.
func (s *followedSite) apply(env store.Envelope) error {
	if err := s.replay.apply(env, &s.records); err != nil {
		return err
	}
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(env.Payload, &head); err != nil {
		return fmt.Errorf("event %v: %w", env.Key(), err)
	}
	s.last[env.NodeEpoch] = followedEnd{key: env.Key(), basis: basisOf(env)}
	// FIX3 F9: streams come epoch-ascending (Watermarks) and each in seq
	// order, so the last event seen here is the site's newest.
	wall := env.AuthoredAt.WallMS
	s.hasEvent = true
	s.fresh.lastEventMS, s.fresh.lastEventBackMS = wall, 0
	if back := s.fresh.maxWallMS - wall; s.fresh.maxWallMS != 0 && back >= time.Minute.Milliseconds() {
		s.fresh.lastEventBackMS = back
	}
	if wall > s.fresh.maxWallMS {
		s.fresh.maxWallMS = wall
	}
	ev := followedTripEvent{site: env.SiteID, key: env.Key(), basis: basisOf(env)}
	switch head.Type {
	case "trip_dispatched":
		var p TripDispatched
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return fmt.Errorf("event %v: %w", env.Key(), err)
		}
		ev.dispatched = &p
	case "trip_arrival":
		var p TripArrival
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return fmt.Errorf("event %v: %w", env.Key(), err)
		}
		ev.arrival = &p
	case "trip_cancelled":
		var p TripCancelled
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return fmt.Errorf("event %v: %w", env.Key(), err)
		}
		ev.cancelled = &p
	default:
		return nil
	}
	s.trips = append(s.trips, ev)
	return nil
}
