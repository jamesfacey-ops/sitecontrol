// departure.go — THE effective departure time (James 2026-10-06: "from the
// typed time").
//
// A run's allowance — a controlled run's journey deadline, road work's
// working limit — counts from ONE instant everywhere it is judged: the
// destination's scan, the origin's outbound and road-work scan, a third
// post's site page, the dispatch host's all-sites strip. Until 2026-10-06
// that instant was when the dispatch was RECORDED (FINDINGS-R4 D13: the
// envelope's authored_at), so a vehicle checked out at 14:30 with the time
// typed back to 14:00 was given its whole allowance from 14:30. James
// 2026-10-06: from the typed time. A run typed so far back that it is
// already past its allowance is OVERDUE the moment it is recorded.
//
// REPLICATION. The typed time is the dispatch envelope's own OccurredAt,
// with TimeSource "entered" (timecapture.go; ⟦FIX8⟧ one trip per distinct
// typed time). Both ride the replicated, hashed envelope (schema-10 §5B.3),
// so every post that holds the dispatch computes the same instant from the
// same bytes — no schema change, nothing new authored. Only the dispatch
// envelope's own typed value is read: a later time correction
// (occurred_at_corrected) does not move a running deadline.
//
// GUARDS. The departure is never LATER than the recording: a typed time
// ahead of authored_at (warn-accepted only on an unsynced clock, or within
// the 5-minute skew allowance — timecapture.go bound 1) counts from the
// recording, so a typed time can only shorten an allowance, never extend
// it. An implausibly old typed time keeps the time box's existing rule (30
// days warns and accepts — bound 3) and is simply overdue. A "default" or
// unrecognised time_source, a zero OccurredAt, or a dispatch recorded
// before this ruling with typed == recorded all give lead 0: authored_at,
// exactly the D13 basis, so trips already in a database are unchanged
// unless their dispatch carries an earlier typed time.
package domain

import (
	"fmt"
	"time"

	"sitecontrol/internal/store"
)

// DepartureLead is how far before its own recording a dispatch's typed
// time puts the departure: authored_at − OccurredAt for an entered,
// earlier time; 0 otherwise. Both values are the ORIGIN's clock, so the
// difference compares no clocks across posts — a destination subtracts it
// from its own receipt-based countdown, the origin from its own.
func DepartureLead(env store.Envelope) time.Duration {
	authored := env.AuthoredAt.WallMS
	if ProvenanceClass(env.TimeSource, env.OccurredAt, authored) != ProvenanceEntered ||
		env.OccurredAt <= 0 || env.OccurredAt >= authored {
		return 0
	}
	return time.Duration(authored-env.OccurredAt) * time.Millisecond
}

// EffectiveDeparture is the instant a run's allowance counts from, on the
// origin's clock: the dispatch's authored_at less DepartureLead.
func EffectiveDeparture(env store.Envelope) time.Time {
	return time.UnixMilli(env.AuthoredAt.WallMS).Add(-DepartureLead(env))
}

// DispatchDeparture reads the dispatch envelope at k and returns its
// effective departure and lead. found=false when this store does not hold
// the event (not yet replicated, or a from-zero rebuild mid-replay).
func DispatchDeparture(st *store.Store, k store.Key) (departure time.Time, lead time.Duration, found bool, err error) {
	evs, err := st.Events(k.SiteID, k.NodeEpoch, k.Seq, 1)
	if err != nil {
		return time.Time{}, 0, false, fmt.Errorf("domain: dispatch departure %v: %w", k, err)
	}
	if len(evs) != 1 || evs[0].Seq != k.Seq {
		return time.Time{}, 0, false, nil
	}
	return EffectiveDeparture(evs[0]), DepartureLead(evs[0]), true, nil
}
