// ack_history.go — every logged acknowledgment of a trip's alert, for the
// overdue history (FIX16f, James 2026-10-06: "a list of trips that went past
// their deadline … who acknowledged it and when (and from which post)").
//
// A READ, nothing more: the alert_ack and alert_ack_retraction projections
// the scan already reads, plus each ack event's own recorded authoring time
// (store.EventTimeAt). No event, column or table is added. Unlike ackedTrips
// it keeps the RETRACTED acks, flagged: a history that silently dropped an
// acknowledgment someone later withdrew would hide that it was ever made.
package deadline

import (
	"sort"

	"sitecontrol/internal/store"
)

// AckRecord is one logged acknowledgment.
type AckRecord struct {
	Key    AckKey
	TripID string
	// Site is the acknowledging post (the ack event's own stream); By is who
	// it recorded (recorded_by), or the post when it recorded nobody.
	Site string
	By   string
	// Kind is the row the officer pressed on: AckKindRed (OVERDUE),
	// AckKindAmber (VERIFY BY RADIO) or "" — a pre-16 ack, which silenced
	// both.
	Kind string
	// AtMS is the ack event's authored_at, by the acknowledging post's own
	// clock. AtOK=false: the event is not in this store's log.
	AtMS int64
	AtOK bool
	// Retracted: an alert_ack_retraction names this ack (W9c) — it no
	// longer silences anything, but it was made.
	Retracted bool
}

// AckHistory returns every acknowledgment this store holds, per trip, in
// log-key order. Legacy weather-phase acks are skipped, as every reader
// skips them (legacyPhaseAck).
func AckHistory(st *store.Store) (map[string][]AckRecord, error) {
	rows, err := dumpSection[ackRow](st, "alert_ack")
	if err != nil {
		return nil, err
	}
	retracted, err := retractionsByTarget(st)
	if err != nil {
		return nil, err
	}
	out := map[string][]AckRecord{}
	for _, r := range rows {
		if legacyPhaseAck(r) {
			continue
		}
		k := AckKey{SiteID: r.SiteID, NodeEpoch: r.NodeEpoch, Seq: r.Seq}
		rec := AckRecord{Key: k, TripID: r.TripID, Site: r.SiteID, By: attribution(r.RecordedBy, r.SiteID), Retracted: len(retracted[k]) > 0}
		if r.AlertKind != nil {
			rec.Kind = *r.AlertKind
		}
		et, found, err := st.EventTimeAt(store.Key{SiteID: k.SiteID, NodeEpoch: k.NodeEpoch, Seq: k.Seq})
		if err != nil {
			return nil, err
		}
		rec.AtMS, rec.AtOK = et.AuthoredAtMS, found
		out[r.TripID] = append(out[r.TripID], rec)
	}
	for _, recs := range out {
		sort.SliceStable(recs, func(i, j int) bool {
			a, b := recs[i].Key, recs[j].Key
			if a.SiteID != b.SiteID {
				return a.SiteID < b.SiteID
			}
			if a.NodeEpoch != b.NodeEpoch {
				return a.NodeEpoch < b.NodeEpoch
			}
			return a.Seq < b.Seq
		})
	}
	return out, nil
}

// OverdueAck reports whether the record proves the acknowledging post had
// the run OVERDUE: an effective red or kind-less ack (OverdueAckedBy's set).
func (r AckRecord) OverdueAck() bool { return !r.Retracted && r.Kind != AckKindAmber }
