// road_work.go — equipment WORKING ON THE ROAD: the trip kind James asked for
// on 2026-10-03 ("the ice road needs another category for equipment working
// on the ice road so that they are not listed as overdue when they will
// spend up to 12 hours on the road working, so once 12 hours hits they will
// be labelled overdue").
//
// WHAT IT IS, IN THE EXISTING MODEL. A road-work run is an ordinary
// trip_dispatched (trip.go) with a KIND, not a destination:
//
//   - DestinationSite and DestinationUncontrolled are BOTH nil — there is no
//     post at the far end, the equipment is out on the road itself;
//   - DeadlineMinutes is SET, to the origin post's configured working limit
//     (config [road_work], default twelve hours) — which is what keeps it out
//     of IncompleteCheckIns (deadline-less destination-less open trips are
//     crashed check-ins) and what the scan counts down;
//   - the payload carries `road_work: true`, and the trip fold writes the
//     fact to trip_road_work below — a raw, INSERT-only side table keyed by
//     trip id, so the kind is a set-shaped fact every post derives
//     identically in any delivery order.
//
// WHO OWNS IT: THE ORIGIN. A normal trip's clock lives at its destination
// (deadline.OnApplied captures trips destined HERE). Road work has no
// destination, and in_transit.go's own argument for origin ownership of
// people on the road applies with no second post to argue against it: the
// origin authored the dispatch (its own stream always holds it, link or no
// link), the dispatch fold closed the crew's visits here in the same
// transaction, and this is where the equipment comes back to. So the origin's
// scanner evaluates it (deadline/scan.go reads OpenRoadWork for ITS OWN site),
// the origin's banner alerts and acknowledges, and the origin's muster lists
// the crew. The OTHER post lists a replicated road-work run for information
// and can record its arrival there (D-96: the vehicle arrives where it
// arrives) — but holds no timer for it.
//
// HOW IT CLOSES. An EXPLICIT arrival naming the trip (RecordExpectedArrival —
// "back from the road" at the origin, or "arrived here instead" at another
// post) binds in Pass 1 of the fold exactly as any expected arrival does, and
// the crew's visits open at whichever post recorded it. Road-work trips are
// destination-less and therefore `degenerate` to the §9.2 correlation —
// NEVER a Pass-2 candidate — so an UNSOLICITED arrival can never bind one by
// vehicle match: it lands plain, the crew is on that post's roster, and the
// run stays listed as working until a human closes it (over-report, the safe
// direction). Making road work a correlation candidate is a ruling for James,
// not an inference made here.
//
// NO SchemaVersion BUMP: a new payload key that is omitted on every existing
// event, and a NEW table under IF NOT EXISTS — the W5-X5 exemption every raw
// side table since person_amendment has landed under. The one thing that
// cannot be healed that way — a binary that does not know this kind reading
// a log that carries it — files the run as a deadline-bearing degenerate
// trip: not on any board, crew on no muster. Every post of a deployment runs
// the same binary (one binary, config-only differentiation), so the fleet
// upgrades together; recorded here so it is a known edge and not a surprise.
package domain

import (
	"sort"

	"sitecontrol/internal/store"
)

// RoadWorkDestinationText is what a surface prints where a DESTINATION would
// go for a road-work run that has none — NotTracked's Destination column,
// for one. One spelling, exported, so no second surface invents another.
const RoadWorkDestinationText = "working on the road (no destination post)"

// TripRoadWork is one row of trip_road_work: the trip is a road-work run.
type TripRoadWork struct {
	TripID string `json:"trip_id"`
}

// tripRoadWorkProjection registers the side table so it self-creates on Open
// and is dumped/compared like every other raw table (§67 inversion:
// registered ⇒ compared by dumpcompare by default). Rows are written by
// applyTripDispatched (trip.go), not by an Apply of its own — the
// "written by another fold; dump-only here" shape of trip_occupant and
// trip_arrival_not_entered.
func tripRoadWorkProjection() store.Projection {
	return store.Projection{
		Name:    "trip_road_work",
		Table:   "trip_road_work",
		OrderBy: "trip_id",
		Schema: `CREATE TABLE IF NOT EXISTS trip_road_work (
			trip_id TEXT PRIMARY KEY
		) STRICT;`,
	}
}

// IsRoadWork reports whether tripID is a road-work run — one primary-key
// point lookup (F30), false for an unknown trip.
func IsRoadWork(st *store.Store, tripID string) (bool, error) {
	row, err := oneRow[TripRoadWork](st, "trip_road_work", store.Eq{Column: "trip_id", Value: tripID})
	if err != nil {
		return false, err
	}
	return row != nil, nil
}

// roadWorkTripIDs loads the whole kind set. The table holds one row per
// road-work run of the season — tens, not thousands — so a list surface
// reads it once rather than point-looking-up every open trip.
func roadWorkTripIDs(st *store.Store) (map[string]bool, error) {
	rows, err := dumpSection[TripRoadWork](st, "trip_road_work")
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(rows))
	for _, r := range rows {
		out[r.TripID] = true
	}
	return out, nil
}

// OpenRoadWork lists the OPEN road-work runs this store knows, oldest first
// by source key, composed exactly as OpenExpectations composes an inbound
// expectation (live vehicle, driver with its id fallback, occupant list,
// effective facts). ⟦D12, James 2026-10-04: "every reader that switches on
// trip kind needs a road-work arm"⟧ ⟦was: no Vehicle and no driver-id
// fallback — the scan reads Expectation.Vehicle for the alert strip, the
// outstanding-alerts list, the ⟦was: Phase-3⟧ review rows and the log line, so all
// four printed "EQUIPMENT OVERDUE ON THE ROAD: <code></code>" while the
// board row beneath named the machine (FINDINGS-R4 D12)⟧ originSite narrows
// the list to runs THAT post sent out — the owner's view, which is what the
// deadline scan and the origin's own boards read; "" is every post's, for a
// surface that lists replicated runs for information (the other post's
// "arrived here instead" door).
//
// Reads the kind set first and composes only its members: an open trip that
// is not road work is never composed here, so a scan pays for this read in
// proportion to the equipment actually out, not to every open trip.
func OpenRoadWork(st *store.Store, originSite string) ([]Expectation, error) {
	kind, err := roadWorkTripIDs(st)
	if err != nil {
		return nil, err
	}
	if len(kind) == 0 {
		return nil, nil
	}
	open, err := openTripStates(st)
	if err != nil {
		return nil, err
	}
	personName := map[string]string{}
	nameOf := func(personID string) (string, error) {
		name, ok := personName[personID]
		if !ok {
			p, err := PersonByID(st, personID)
			if err != nil {
				return "", err
			}
			if p != nil {
				name = p.Name
			}
			personName[personID] = name
		}
		return name, nil
	}
	merge, err := LoadPersonMergeMap(st)
	if err != nil {
		return nil, err
	}
	// The SAME live composition composeOpenExpectations and openRunsFrom
	// read (DR-W7-18), so the alert names the truck the board row and the
	// muster name.
	live, err := NewLiveVehicles(st)
	if err != nil {
		return nil, err
	}
	var out []Expectation
	for _, s := range open {
		if !kind[s.TripID] {
			continue
		}
		c, err := EffectiveTrip(st, s.TripID)
		if err != nil {
			return nil, err
		}
		if c == nil || !c.RoadWork {
			continue
		}
		if originSite != "" && c.Trip.OriginSite != originSite {
			continue
		}
		t := &c.Trip
		aboard, err := StillAboard(st, c) // F15: the muster's road-work section reads the same
		if err != nil {
			return nil, err
		}
		occList, err := composedOccupantRefs(st, aboard, merge, nameOf)
		if err != nil {
			return nil, err
		}
		name, err := nameOf(t.DriverPersonID)
		if err != nil {
			return nil, err
		}
		if name == "" {
			name = t.DriverPersonID // composeOpenExpectations' rule: never a blank driver
		}
		vehicle, _, err := live.Compose(*c)
		if err != nil {
			return nil, err
		}
		out = append(out, Expectation{
			Trip: *t, State: TripStateOpen,
			Vehicle:      vehicle,
			DriverName:   name,
			Occupants:    len(occList),
			OccupantList: occList,
		})
	}
	sortExpectationsBySourceKey(out)
	return out, nil
}

// sortExpectationsBySourceKey is OpenExpectations' own ordering (row 10 —
// never rowid, never wall-time), restated so the two lists cannot drift.
func sortExpectationsBySourceKey(out []Expectation) {
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Trip, out[j].Trip
		if a.NodeEpoch != b.NodeEpoch {
			return a.NodeEpoch < b.NodeEpoch
		}
		if a.Seq != b.Seq {
			return a.Seq < b.Seq
		}
		return a.TripID < b.TripID
	})
}

// RoadWorkFrom is the muster's share: the open road-work runs THIS post sent
// out, one row per run with the crew the record still has out — the same
// composition, the same sightedSince return predicate and the same ordering
// as InTransitFrom (in_transit.go), through the same code path, so the two
// sections can never disagree about who counts as back. Destination is ""
// on every row (there is none); the rendering surface names the category.
func RoadWorkFrom(st *store.Store, siteID string) ([]InTransitRow, error) {
	return openRunsFrom(st, siteID, true)
}

// RoadWorkCrewOnSiteElsewhere — D27 (FINDINGS-R4; James, 2026-10-04:
// "every reader that switches on trip kind needs a road-work arm") — maps
// each person listed on rows (RoadWorkFrom's rows for originSite) to the
// OPEN visits the same human holds at any OTHER site, in (SiteID, VisitID)
// order. A person with none is absent from the map.
//
// THE CASE. A road-work run is never a correlation candidate (file comment),
// so a crew member entered at the pad through the UNSOLICITED door opens a
// pad visit while the origin's run stays open: one human on the pad's
// on-site roster AND on the origin's "working on the road" list. Until
// someone presses "Arrived here instead" the two sheets disagree, and
// neither said so.
//
// THE RULING APPLIED is M-6's (visit_elsewhere.go, James 2026-10-03): a
// READ-TIME flag beside the name, both listings kept. Dropping the person
// from the road-work list would be the under-report direction — sightedSince
// deliberately does not trust a non-origin visit without a trip reference
// (a stranded peer check-out can leave one reading "open" for a whole
// outage) — and closing the run would be a correlation, which road_work.go
// leaves to James. What was wrong was the silence.
//
// Matching is on the MERGE-RESOLVED human, as the rows themselves are
// composed. No clock is read and nothing is ordered (D-90/D-92): SinceMS is
// the other visit's recorded sign-in instant, for display only.
func RoadWorkCrewOnSiteElsewhere(st *store.Store, originSite string, rows []InTransitRow) (map[string][]OpenElsewhere, error) {
	out := map[string][]OpenElsewhere{}
	if len(rows) == 0 {
		return out, nil
	}
	merge, err := LoadPersonMergeMap(st)
	if err != nil {
		return nil, err
	}
	resolve := func(id string) string {
		if merge != nil {
			return merge.Resolve(id)
		}
		return id
	}
	listed := map[string][]string{} // human -> row person ids naming them
	seen := map[string]bool{}
	for _, r := range rows {
		for _, p := range r.Persons {
			if seen[p.PersonID] {
				continue
			}
			seen[p.PersonID] = true
			h := resolve(p.PersonID)
			listed[h] = append(listed[h], p.PersonID)
		}
	}
	streams, err := st.Watermarks()
	if err != nil {
		return nil, err
	}
	sites := map[string]bool{}
	for _, m := range streams {
		if m.SiteID != originSite {
			sites[m.SiteID] = true
		}
	}
	others := make([]string, 0, len(sites))
	for s := range sites {
		others = append(others, s)
	}
	sort.Strings(others)
	for _, site := range others {
		theirs, err := OpenVisitsAt(st, site)
		if err != nil {
			return nil, err
		}
		var twins []Visit
		for _, v := range theirs {
			if _, ok := listed[resolve(v.PersonID)]; ok {
				twins = append(twins, v)
			}
		}
		if len(twins) == 0 {
			continue
		}
		times, err := VisitSignInTimes(st, site, twins)
		if err != nil {
			return nil, err
		}
		for _, v := range twins {
			ms, ok := times[v.VisitID]
			for _, pid := range listed[resolve(v.PersonID)] {
				out[pid] = append(out[pid], OpenElsewhere{SiteID: site, VisitID: v.VisitID, SinceMS: ms, SinceOK: ok})
			}
		}
	}
	for _, list := range out {
		sort.Slice(list, func(i, j int) bool {
			if list[i].SiteID != list[j].SiteID {
				return list[i].SiteID < list[j].SiteID
			}
			return list[i].VisitID < list[j].VisitID
		})
	}
	return out, nil
}
