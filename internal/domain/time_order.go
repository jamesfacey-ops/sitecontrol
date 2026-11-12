// time_order.go — FIX17b (sc-stress F12): a TYPED time may not put the end
// of something before its start. A check-out typed earlier than the person's
// check-in was accepted silently and became a visit of −20 minutes in every
// report and average; a departure typed before an occupant's check-in, an
// arrival typed before its run's departure, and a time correction that
// would do any of these are the same mistake by another door.
//
// Only an ENTERED time is held to this. A time the post's own clock stamped
// ("default") is never refused here: across two posts it can legitimately
// read earlier than the other post's stamp (a slow clock, sc-stress F11 —
// shown honestly per clock, by design), and refusing it would stop a gate
// recording somebody who is physically leaving now.
//
// The start is read as it stands — ResolveOccurredAt, so a corrected
// check-in counts at its corrected time. A start that cannot be resolved
// (a correction still in flight from another post) refuses nothing: this
// rule never blocks on a record it cannot read.
//
// A departure is not judged here: the gate's vehicle form already refuses a
// typed time out earlier than a passenger's time in (web/gate_rowtime.go,
// FIX8). A time CORRECTION is judged (TimeCorrectionOrderProblem) but only
// warned about: every bound on a corrected instant warns and accepts (W4).
package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"sitecontrol/internal/store"
)

// clockLabel renders an instant as the site-local time an officer reads:
// "15:04", with the date when it is not the same day as ref.
func clockLabel(ms, ref int64) string {
	t := time.UnixMilli(ms).In(time.Local)
	r := time.UnixMilli(ref).In(time.Local)
	if t.YearDay() != r.YearDay() || t.Year() != r.Year() {
		return t.Format("Jan 2 15:04")
	}
	return t.Format("15:04")
}

func visitPersonLabel(v Visit) string {
	if v.PersonSnapshotName != nil && strings.TrimSpace(*v.PersonSnapshotName) != "" {
		return *v.PersonSnapshotName
	}
	return "This person"
}

// endBeforeVisitStart is the sentence for an entered end time (a check-out,
// or a departure that closes the visit) earlier than the visit's check-in,
// or "" when the order is fine or cannot be judged. what is "check-out" or
// "departure".
func endBeforeVisitStart(st *store.Store, v Visit, endMS int64, timeSource, what string) string {
	if timeSource != "entered" {
		return ""
	}
	in, ok := resolvedInstant(st, store.Key{SiteID: v.SiteID, NodeEpoch: v.NodeEpoch, Seq: v.Seq})
	if !ok || endMS >= in {
		return ""
	}
	return fmt.Sprintf("%s was checked in here at %s, and the %s time typed, %s, is before that. Type the time they actually left (%s or later), or correct the check-in time first",
		visitPersonLabel(v), clockLabel(in, endMS), what, clockLabel(endMS, in), clockLabel(in, endMS))
}

// arrivalBeforeDeparture is the sentence for an entered arrival time
// earlier than its run's departure as recorded at the sending post, or "".
//
// A local check-in's trip is not a departure (it is the destination-less
// first half of the check-in itself): it is never judged here, so
// correcting a check-in to an earlier time stays possible.
func arrivalBeforeDeparture(st *store.Store, siteID string, trip Trip, arrivedMS int64, timeSource string) string {
	if timeSource != "entered" {
		return ""
	}
	if trip.DestinationSite == nil && trip.DestinationUncontrolled == nil {
		if rw, err := IsRoadWork(st, trip.TripID); err != nil || !rw {
			return ""
		}
	}
	dep, ok := resolvedInstant(st, store.Key{SiteID: trip.OriginSite, NodeEpoch: trip.NodeEpoch, Seq: trip.Seq})
	if !ok || arrivedMS >= dep {
		return ""
	}
	clock := ""
	if trip.OriginSite != siteID {
		clock = " (" + siteNameAt(st, trip.OriginSite) + "'s clock)"
	}
	return fmt.Sprintf("%s left %s at %s%s, and the arrival time typed, %s, is before that. Type the time it actually arrived (%s or later), or correct the departure time first",
		liveVehicleName(st, trip.TripID), siteNameAt(st, trip.OriginSite), clockLabel(dep, arrivedMS), clock, clockLabel(arrivedMS, dep), clockLabel(dep, arrivedMS))
}

// TimeCorrectionOrderProblem says, in an officer's words, why correcting
// the event at target to correctedMS would put an end before its start —
// a check-out before its check-in, a check-in after its check-out, an
// arrival before its run's departure, a departure after its arrival — or
// "" when it would not (or the event is of another kind). The corrected
// instant is always typed, so it is always held to the rule.
func TimeCorrectionOrderProblem(st *store.Store, target store.Key, correctedMS int64) (string, error) {
	original, err := correctionOriginal(st, target)
	if err != nil {
		return "", nil // the correction's own read path reports this
	}
	env, ok, err := eventAt(st, original)
	if err != nil || !ok {
		return "", err
	}
	var head struct {
		Type    string  `json:"type"`
		VisitID string  `json:"visit_id"`
		TripID  string  `json:"trip_id"`
		TripRef *string `json:"trip_ref"`
	}
	if json.Unmarshal(env.Payload, &head) != nil {
		return "", nil
	}
	switch head.Type {
	case "visit_checked_out":
		v, err := VisitByID(st, head.VisitID)
		if err != nil || v == nil {
			return "", err
		}
		return endBeforeVisitStart(st, *v, correctedMS, "entered", "check-out"), nil
	case "trip_arrival":
		if head.TripRef != nil {
			if trip, err := TripByID(st, *head.TripRef); err == nil && trip != nil {
				if p := arrivalBeforeDeparture(st, env.SiteID, *trip, correctedMS, "entered"); p != "" {
					return p, nil
				}
			}
		}
		// The visits this arrival opened: a check-in may not move past the
		// check-out (or departure) that closed one of them.
		visits, err := dumpSectionWhere[Visit](st, "visit", store.Eq{Column: "site_id", Value: original.SiteID},
			store.Eq{Column: "node_epoch", Value: original.NodeEpoch}, store.Eq{Column: "seq", Value: original.Seq})
		if err != nil {
			return "", err
		}
		closes, err := ClosedVisits(st, visits)
		if err != nil {
			return "", err
		}
		for _, c := range closes {
			if c.InstantOK && (c.How == ClosedByCheckout || c.How == ClosedByDispatch) && correctedMS > c.InstantMS {
				left := "checked out"
				if c.How == ClosedByDispatch {
					left = "left"
				}
				return fmt.Sprintf("%s %s at %s, and the check-in time typed, %s, is after that. Type the time they actually arrived (%s or earlier), or correct the %s time first",
					visitPersonLabel(c.Visit), left, clockLabel(c.InstantMS, correctedMS), clockLabel(correctedMS, c.InstantMS), clockLabel(c.InstantMS, correctedMS), map[bool]string{true: "check-out", false: "departure"}[c.How == ClosedByCheckout]), nil
			}
		}
	case "trip_dispatched":
		state, err := TripStateByID(st, head.TripID)
		if err != nil || state == nil || state.ArrivalID == nil {
			return "", err
		}
		a, err := arrivalByID(st, *state.ArrivalID)
		if err != nil || a == nil {
			return "", err
		}
		arr, ok := resolvedInstant(st, store.Key{SiteID: a.SiteID, NodeEpoch: a.NodeEpoch, Seq: a.Seq})
		if !ok || correctedMS <= arr {
			return "", nil
		}
		return fmt.Sprintf("%s arrived at %s at %s, and the departure time typed, %s, is after that. Type the time it actually left (%s or earlier), or correct the arrival time first",
			liveVehicleName(st, head.TripID), siteNameAt(st, a.SiteID), clockLabel(arr, correctedMS), clockLabel(correctedMS, arr), clockLabel(arr, correctedMS)), nil
	}
	return "", nil
}
