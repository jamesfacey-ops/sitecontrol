// visit_signin.go — WHEN each open visit was opened, for DISPLAY on the
// gate's on-site roster (James, 2026-09-10: "the date of sign in should be
// displayed on the onsite roster as well"; and on whose clock: "that is a
// moot point use the system clock").
//
// THE VISIT ROW CARRIES NO CLOCK, BY DESIGN. store.go's event_log comment
// (§5B.5): the time basis orders nothing, "no projection column copies
// them". So the instant a person was signed in lives on exactly one thing:
// the trip_arrival EVENT that opened the visit — its envelope's OccurredAt,
// which on the default path is the recording box's system clock at the
// check-in and on the entered path the time the officer typed. It is read
// through the SAME resolver the time-correction surface uses
// (ResolveOccurredAt), so a W4 correction that won is what the roster
// shows: the record as it stands.
//
// HOW A VISIT FINDS ITS ARRIVAL. applyTripArrival inserts each visit with
// trip_id = the arrival's trip_ref (trip.go) and NOTHING that names the
// arrival itself, so the join is: visit.trip_id → trip_arrival rows at this
// site with that trip_ref (idx_arrival_trip_ref; usually exactly one) → the
// event at (site_id, node_epoch, seq) → its TripArrival payload's Visits
// list, which names the visit id exactly. Unsolicited arrivals carry no
// trip_ref, so their visits have trip_id NULL; those are matched through the
// site's trip_ref-IS-NULL arrivals (idx_arrival_unsolicited's set — rare,
// and bounded by the number of such arrivals, never by the season). The
// match is on the visit id itself in every case: a trip with two arrivals
// referencing it cannot mis-attribute a visit.
//
// ⛔ NOTHING HERE ORDERS ANYTHING (D-90, D-92, TP §4.4, schema-10 §5B.5).
// The map this returns is keyed by visit id; the roster sorts by name
// before and after it is consulted (web/gate.go buildOnSiteRows and the
// picker's groupBuilder), and no caller may sort, filter, or decide by the
// instants inside it. It is the sign-in date an officer reads off a row,
// nothing more.
package domain

import (
	"encoding/json"
	"fmt"

	"sitecontrol/internal/store"
)

// VisitSignInTimes maps each of visits' ids (those at siteID; others are
// ignored) to the recorded sign-in instant behind it, UTC ms. A visit is
// absent from the map when this store holds no arrival event behind it, or
// when its correction chain cannot be resolved fail-closed (TP §4.7: a
// dangling or unreadable correction is never presented, and neither is the
// value it superseded) — the renderer shows the absence, never a guess.
func VisitSignInTimes(st *store.Store, siteID string, visits []Visit) (map[string]int64, error) {
	wanted := make(map[string]bool, len(visits))
	// Group the wanted visits by the trip they reference. Unsolicited
	// arrivals leave trip_id NULL; they share one bucket.
	byRef := map[string]bool{}
	nullRef := false
	for _, v := range visits {
		if v.SiteID != siteID {
			continue
		}
		wanted[v.VisitID] = true
		if v.TripID == nil {
			nullRef = true
		} else {
			byRef[*v.TripID] = true
		}
	}
	out := make(map[string]int64, len(wanted))
	if len(wanted) == 0 {
		return out, nil
	}

	conds := make([]store.Eq, 0, len(byRef)+1)
	for ref := range byRef {
		conds = append(conds, store.Eq{Column: "trip_ref", Value: ref})
	}
	if nullRef {
		conds = append(conds, store.Eq{Column: "trip_ref", Value: nil}) // IS NULL
	}

	type resolvedAt struct {
		ms int64
		ok bool
	}
	resolved := map[store.Key]resolvedAt{}
	resolveKey := func(key store.Key) resolvedAt {
		if r, done := resolved[key]; done {
			return r
		}
		// The record as it stands: the envelope's own instant, or the
		// winning correction's. An unresolvable chain is an absence, per
		// the doc comment.
		var r resolvedAt
		if rt, _, err := ResolveOccurredAt(st, key); err == nil {
			r = resolvedAt{ms: rt.InstantUTCms, ok: true}
		}
		resolved[key] = r
		return r
	}
	for _, cond := range conds {
		arrivals, err := dumpSectionWhere[Arrival](st, "trip_arrival", store.Eq{Column: "site_id", Value: siteID}, cond)
		if err != nil {
			return nil, fmt.Errorf("domain: visit sign-in times: %w", err)
		}
		for _, a := range arrivals {
			key := store.Key{SiteID: a.SiteID, NodeEpoch: a.NodeEpoch, Seq: a.Seq}
			env, ok, err := eventAt(st, key)
			if err != nil {
				return nil, fmt.Errorf("domain: visit sign-in times: read %v: %w", key, err)
			}
			if !ok {
				continue // no event behind the row: reported as absence, see above
			}
			var p TripArrival
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				return nil, fmt.Errorf("domain: visit sign-in times: decode %v: %w", key, err)
			}
			for _, av := range p.Visits {
				if !wanted[av.VisitID] {
					continue
				}
				if _, seen := out[av.VisitID]; seen {
					continue
				}
				if r := resolveKey(key); r.ok {
					out[av.VisitID] = r.ms
				}
			}
		}
		// ⟦W4 fix, WINDOW-AUDIT.md⟧ entry-on-completion (d91-arrival-respec.md
		// §3.4) mints a visit from `orientation_completed`, never from
		// `trip_arrival` — this function's join above never sees it, so the
		// roster rendered a dash for a person entry-on-completion put on
		// site. The completion IS the check-in, and its own envelope's
		// instant is the sign-in time to show, resolved the identical way
		// (TP §4.7's correction chain, same resolveKey). Grouped by the
		// SAME trip_ref bucket: buildOrientationCompletedPayload carries the
		// held-out arrival's own TripRef onto the completion unchanged
		// (orientation.go), so the two sides never disagree about which
		// trip a visit belongs to.
		completions, err := dumpSectionWhere[OrientationCompletionRow](st, "orientation_completion", store.Eq{Column: "site_id", Value: siteID}, cond)
		if err != nil {
			return nil, fmt.Errorf("domain: visit sign-in times: %w", err)
		}
		for _, c := range completions {
			key := store.Key{SiteID: c.SiteID, NodeEpoch: c.NodeEpoch, Seq: c.Seq}
			env, ok, err := eventAt(st, key)
			if err != nil {
				return nil, fmt.Errorf("domain: visit sign-in times: read %v: %w", key, err)
			}
			if !ok {
				continue
			}
			var p OrientationCompleted
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				return nil, fmt.Errorf("domain: visit sign-in times: decode %v: %w", key, err)
			}
			if p.Visit == nil || !wanted[p.Visit.VisitID] {
				continue
			}
			if _, seen := out[p.Visit.VisitID]; seen {
				continue
			}
			if r := resolveKey(key); r.ok {
				out[p.Visit.VisitID] = r.ms
			}
		}
	}
	return out, nil
}
