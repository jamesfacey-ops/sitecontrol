// visit_closed.go — M-5 / M-19 (James, 2026-10-03, ruling: "keep the route;
// never drop silently — print 'N on the original sheet have checked out
// since: <names, time>' and replace the promise sentence with true text").
//
// WHAT WAS WRONG. A reprint link names a frozen visit set (web/muster.go);
// buildMuster lists only OPEN visits (§11 is a view over open visits). So a
// person who checked out AFTER the sheet was minted simply vanished from the
// reprint — MEASURED on L10 (M-19), twice: a 9-id link reloaded after one
// removal rendered 8 names under REPRINT and the sentence "This reproduces
// the same roster no matter when it is printed again". An UNKNOWN id fails
// loudly (410); a CLOSED one was dropped in silence, on the one document
// read aloud during an evacuation.
//
// THE RULING keeps the route and the frozen set, and makes the departure
// VISIBLE instead of invisible: the reprint still lists exactly the people
// still on site from the original sheet, and names — with the time — every
// person on the original sheet whose record has since closed. This file is
// the derivation behind that list: for a CLOSED visit, HOW it closed and
// WHEN, from the raw tables and the log, at read time.
//
// THE CLOSERS, mirrored from visit_open.go's derivation (THE RULE) and
// visit_report.go's reasons — one vocabulary (VisitClosure), no new one:
//
//   - a visit_checkout row naming the visit under its own site tag
//     → ClosedByCheckout, instant = the visit_checked_out event's. The
//     projection row carries no key (§5B.5: nothing copies a time, and the
//     check-out needs none to close by id), so the event is found by
//     walking the visit's OWN site's streams from the visit's key forward
//     (store.EachEvent) — bounded by that site's events since the person
//     signed in, never by the season — and resolved through
//     ResolveOccurredAt like every other displayed instant.
//   - the FIRST dispatch from the visit's site naming the person with a key
//     above the visit's → ClosedByDispatch (a destination or an uncontrolled
//     pad, or a road-work run), or ClosedByReCheckin when the trip has no
//     destination at all (the dispatch half of a gate check-in at the same
//     site — a missed sign-out, visit_report.go).
//   - else the person's NEXT visit at the same site (a greater key)
//     → ClosedByNextArrival; its row key IS its arrival envelope's key.
//   - else VisitClosedUnexplained: the table says closed and the raw rows do
//     not say why. Reported as exactly that — never guessed.
//
// ⛔ "FIRST" is stream order (store.Key.Less on one owning site), the only
// order the record has (D-90/D-92). The INSTANT is display only.
package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"sitecontrol/internal/store"
)

// VisitClosedUnexplained: the row is closed and none of the three raw
// closers names it. Reachable only through a store whose raw rows disagree
// with its derived column; reported, not repaired.
const VisitClosedUnexplained VisitClosure = "closed_unexplained"

// VisitClose is one CLOSED visit from a frozen set, with how and when it
// closed. Vehicle/Destination/Uncontrolled/RoadWork are filled for the
// dispatch closers only. Vehicle follows the one rule every trip surface
// keeps (DR-W7-18, web/trips.go tripRows): while the run is still OPEN it is
// the truck composed LIVE (LiveVehicles) — the same number the reprint's own
// "on the road" block, the alert and the inbound board show for that run —
// and once the run has closed it is the number AS RECORDED AT DISPATCH.
// ⟦FINDINGS-R4 L10 LIVE-2⟧ ⟦was: always the dispatch snapshot⟧ — so one
// reprint named one open run by two trucks: "TRK-L10J → Ice Pad" on the road
// and "left on TRK-L10H" in the departed list, after a mid-trip renumber.
type VisitClose struct {
	Visit        Visit
	How          VisitClosure
	Vehicle      string
	Destination  string // destination site id, dispatch to a controlled post
	Uncontrolled string // uncontrolled destination id
	RoadWork     bool
	InstantMS    int64
	InstantOK    bool
}

// ClosedVisits derives a VisitClose for every CLOSED row in visits (open
// rows are skipped), in the order given. Read-only; authors nothing.
func ClosedVisits(st *store.Store, visits []Visit) ([]VisitClose, error) {
	var out []VisitClose
	var live *LiveVehicles // built on the first open run met; most reprints meet none
	for _, v := range visits {
		if v.IsOpen == 1 {
			continue
		}
		c, err := closeOf(st, v, &live)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func closeOf(st *store.Store, v Visit, live **LiveVehicles) (VisitClose, error) {
	c := VisitClose{Visit: v, How: VisitClosedUnexplained}
	vKey := store.Key{SiteID: v.SiteID, NodeEpoch: v.NodeEpoch, Seq: v.Seq}

	// 1. An explicit check-out, under the visit's own site tag (D-87: a
	//    foreign site's check-out is inert, exactly as visit_open.go reads it).
	co, err := oneRow[VisitCheckout](st, "visit_checkout", store.Eq{Column: "visit_id", Value: v.VisitID})
	if err != nil {
		return c, fmt.Errorf("domain: closed visit %s: check-out: %w", v.VisitID, err)
	}
	if co != nil && co.SiteID == v.SiteID {
		c.How = ClosedByCheckout
		key, found, err := checkoutEventKey(st, v)
		if err != nil {
			return c, err
		}
		if found {
			c.InstantMS, c.InstantOK = resolvedInstant(st, key)
		}
		return c, nil
	}

	// 2. The FIRST dispatch from this site naming the person, above the visit.
	occs, err := dumpSectionWhere[TripOccupant](st, "trip_occupant", store.Eq{Column: "person_id", Value: v.PersonID})
	if err != nil {
		return c, fmt.Errorf("domain: closed visit %s: occupant rows: %w", v.VisitID, err)
	}
	var first *Trip
	for _, o := range occs {
		t, err := TripByID(st, o.TripID)
		if err != nil {
			return c, fmt.Errorf("domain: closed visit %s: trip %s: %w", v.VisitID, o.TripID, err)
		}
		if t == nil || t.OriginSite != v.SiteID {
			continue
		}
		tKey := store.Key{SiteID: t.OriginSite, NodeEpoch: t.NodeEpoch, Seq: t.Seq}
		if !vKey.Less(tKey) {
			continue
		}
		if first == nil || tKey.Less(store.Key{SiteID: first.OriginSite, NodeEpoch: first.NodeEpoch, Seq: first.Seq}) {
			tt := *t
			first = &tt
		}
	}
	if first != nil {
		if c.Vehicle, err = closeVehicle(st, *first, live); err != nil {
			return c, fmt.Errorf("domain: closed visit %s: %w", v.VisitID, err)
		}
		c.RoadWork, err = IsRoadWork(st, first.TripID)
		if err != nil {
			return c, fmt.Errorf("domain: closed visit %s: %w", v.VisitID, err)
		}
		switch {
		case first.DestinationSite != nil:
			c.How, c.Destination = ClosedByDispatch, *first.DestinationSite
		case first.DestinationUncontrolled != nil:
			c.How, c.Uncontrolled = ClosedByDispatch, *first.DestinationUncontrolled
		case c.RoadWork:
			c.How = ClosedByDispatch
		default:
			c.How = ClosedByReCheckin // the destination-less half of a gate check-in here
		}
		c.InstantMS, c.InstantOK = resolvedInstant(st, store.Key{SiteID: first.OriginSite, NodeEpoch: first.NodeEpoch, Seq: first.Seq})
		return c, nil
	}

	// 3. The person's next visit at this site (idx_visit_site_person_key).
	later, err := dumpSectionWhere[Visit](st, "visit",
		store.Eq{Column: "site_id", Value: v.SiteID}, store.Eq{Column: "person_id", Value: v.PersonID})
	if err != nil {
		return c, fmt.Errorf("domain: closed visit %s: later visits: %w", v.VisitID, err)
	}
	sort.Slice(later, func(i, j int) bool {
		a := store.Key{SiteID: later[i].SiteID, NodeEpoch: later[i].NodeEpoch, Seq: later[i].Seq}
		b := store.Key{SiteID: later[j].SiteID, NodeEpoch: later[j].NodeEpoch, Seq: later[j].Seq}
		if a == b {
			return later[i].VisitID < later[j].VisitID
		}
		return a.Less(b)
	})
	for _, lv := range later {
		lKey := store.Key{SiteID: lv.SiteID, NodeEpoch: lv.NodeEpoch, Seq: lv.Seq}
		if lv.VisitID != v.VisitID && vKey.Less(lKey) {
			c.How = ClosedByNextArrival
			c.InstantMS, c.InstantOK = resolvedInstant(st, lKey)
			return c, nil
		}
	}
	return c, nil
}

// errFoundCheckout stops the stream walk at the first match.
var errFoundCheckout = errors.New("found")

// checkoutEventKey walks the visit's own site's streams, from the visit's
// key forward, for the visit_checked_out event naming it. A check-out is
// authored against an existing visit, so it can only lie above the visit on
// the same stream or on a later epoch of the same site; those are the only
// rows read.
func checkoutEventKey(st *store.Store, v Visit) (store.Key, bool, error) {
	streams, err := st.Watermarks()
	if err != nil {
		return store.Key{}, false, fmt.Errorf("domain: closed visit %s: %w", v.VisitID, err)
	}
	var found store.Key
	for _, m := range streams { // Watermarks is (site_id, node_epoch) ordered
		if m.SiteID != v.SiteID || m.NodeEpoch < v.NodeEpoch {
			continue
		}
		from := int64(1)
		if m.NodeEpoch == v.NodeEpoch {
			from = v.Seq + 1
		}
		err := st.EachEvent(v.SiteID, m.NodeEpoch, from, 0, func(env store.Envelope) error {
			var head struct {
				Type    string `json:"type"`
				VisitID string `json:"visit_id"`
			}
			if json.Unmarshal(env.Payload, &head) != nil || head.Type != "visit_checked_out" || head.VisitID != v.VisitID {
				return nil
			}
			found = env.Key()
			return errFoundCheckout
		})
		switch {
		case errors.Is(err, errFoundCheckout):
			return found, true, nil
		case err != nil:
			return store.Key{}, false, fmt.Errorf("domain: closed visit %s: walk %s/%d: %w", v.VisitID, v.SiteID, m.NodeEpoch, err)
		}
	}
	return store.Key{}, false, nil
}

// resolvedInstant is ResolveOccurredAt as the roster reads it: the record as
// it stands, or an absence (TP §4.7: a dangling or unreadable correction is
// never presented, and neither is the value it superseded).
func resolvedInstant(st *store.Store, key store.Key) (int64, bool) {
	rt, _, err := ResolveOccurredAt(st, key)
	if err != nil {
		return 0, false
	}
	return rt.InstantUTCms, true
}

// closeVehicle is the departed line's truck: LIVE while the run is open, the
// dispatch snapshot once it has closed (VisitClose's comment).
func closeVehicle(st *store.Store, t Trip, live **LiveVehicles) (string, error) {
	state, err := TripStateByID(st, t.TripID)
	if err != nil {
		return "", err
	}
	if state == nil || state.State != TripStateOpen {
		return tripVehicleEnteredDisplay(t), nil
	}
	c, err := EffectiveTrip(st, t.TripID)
	if err != nil || c == nil {
		return tripVehicleEnteredDisplay(t), err
	}
	if *live == nil {
		if *live, err = NewLiveVehicles(st); err != nil {
			return "", err
		}
	}
	display, _, err := (*live).Compose(*c)
	return display, err
}
