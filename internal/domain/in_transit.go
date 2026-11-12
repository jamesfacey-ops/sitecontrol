// in_transit.go — the fourth muster population: people ON THE ROAD.
//
// THE GAP THIS CLOSES (D-6, 2026-08-21). Until this file the muster knew of
// exactly two kinds of person: on site (counted) and off site at an
// uncontrolled pad or untracked (listed, not counted). A crew that left Ice
// Road twenty minutes ago and has not reached the Pad was on NEITHER post's
// sheet, on screen or on paper, even though the system holds their vehicle,
// their driver, their manifest and their destination. Measured on a real
// two-site instance before this change: six people checked in at site-a,
// three dispatched to site-b; site-a's muster showed 3 on site / 0 away /
// 0 not tracked, site-b's showed 0 / 0 / 0. Three people, on no sheet
// anywhere. Under D-6's binding 100% accountability requirement — an OSHA
// rule applied on the Slope, enforced for insurance reasons, with no
// regulation number — that is an accountability defect, not a missing
// feature.
//
// WHICH POST OWNS THEM: THE ORIGIN. This is the whole design decision and
// it is not the obvious one — the DESTINATION is where the deadline lives
// (deadline.OnApplied captures only trips whose destination_site is this
// site; verified: an origin has no deadline_capture row for its own
// outbound trip at all), so the destination is the post that could say
// "overdue". It is still the wrong owner, for two independent reasons:
//
//  1. REPLICATION. The origin AUTHORED trip_dispatched, so its own stream
//     always contains it — no sync, no peer, no link required. A
//     destination-owned section is EMPTY exactly when the link is down,
//     which is precisely when a crew is hardest to account for. D-6 names
//     this failure mode in as many words: not "all whose records finished
//     replicating".
//
//  2. THE DIRECTION OF THE ERROR AT EACH END OF THE JOURNEY. The dispatch
//     fold closes every occupant's origin visit in the SAME TRANSACTION
//     that records the trip (trip.go, applyTripDispatched), so under origin
//     ownership a person leaves "on site" and enters "on the road" atomically
//     — there is no instant at which they are nowhere. The arrival is
//     authored at the far end, so the origin clears them only once it
//     replicates back: until then they are listed BOTH on the road here and
//     on site at the peer. That is an over-report, the safe direction, and
//     it is the same asymmetry OutAtUncontrolled already defends ("a name in
//     two places costs search time, a name in no place is what this sheet
//     exists to prevent"). Destination ownership inverts both: a lagging
//     ENTRY (nobody's sheet) and an atomic exit. Under-reporting is the
//     direction this sheet exists to prevent.
//
// NO DOUBLE COUNT, STRUCTURALLY. Ownership keys on trip.OriginSite, which
// is single-valued and authored structurally as env.SiteID (trip.go: "Origin
// is the authoring site, structurally: env.SiteID, never a payload") — not a
// payload field that can be wrong. Every in-transit trip therefore matches
// EXACTLY ONE post's sheet: never two (only one site equals the origin) and
// never zero (the matching site is the one that authored the event and so
// cannot be missing it). Within a single sheet the sightedSince predicate
// below removes anyone provably back, so a person can never appear on the
// on-site roster and in this section at once.
package domain

import (
	"sort"
	"strconv"

	"sitecontrol/internal/store"
)

// InTransitRow is one open controlled run this site put on the road.
//
// Everything here comes from the TRIP projection — no new table, no new
// column, no SchemaVersion bump. What is deliberately NOT here is a wall-
// clock "time due": this ROW carries no clock. ⟦James, 2026-10-03 — NEW-a⟧
// The origin DOES now hold a time basis for its own outbound trips — the
// log's own authored_at on the dispatch envelope, the same basis the
// road-work scan has used since this morning (deadline.captureOwnAuthored),
// and the origin's scanner evaluates them (deadline/scan.go, OpenOutbound
// below) so the SENDING post's banner, board and analytics say OVERDUE too.
// That verdict is the scan's to render, on its own surfaces; this sheet row
// stays a roster of PEOPLE with the ALLOWANCE the officer promised, which is
// authored data and true everywhere. ⟦was: "the origin holds no time basis
// for its own outbound trips … an arrival clock cannot be computed here
// without inventing one" — true until the road-work ruling established that
// an own-authored envelope's authored_at is a recorded receipt, not an
// invented one; re-ruled, not silently edited⟧
type InTransitRow struct {
	TripID string
	// Vehicle: what an officer reads for this run.
	//
	// ⚠⚠ ⟦W7, DR-W7-18⟧ THIS SURFACE COMPOSES THE VEHICLE **LIVE**, and that
	// is a ruling with a measured operator consequence behind it. A truck goes
	// out with no number on it; somebody radios in the unit number; the
	// officer corrects the record — "a real win", because every past run of
	// that truck retroactively acquires it — and reprints. Read from the
	// frozen snapshot, the heading would still say what was recorded at
	// dispatch, WHILE THE LINE TWO BELOW IT RESOLVES THE DRIVER LIVE. A line
	// that is half-live and half-frozen reads as an oversight.
	//
	// The frozen snapshot is not lost and is not wrong: it is what surfaces
	// answering "what was recorded THEN" read. This one answers "which truck
	// is out RIGHT NOW".
	Vehicle string
	// ⟦W7-34(a)⟧ VehicleIdentity is WHICH TRUCK this run is, after merge
	// resolution — the thing two runs of one truck share and two runs of two
	// trucks do not. It is NOT the display string: two records joined by
	// `same_as` are ONE truck however differently their numbers read, and two
	// unjoined records are two trucks even if their numbers read alike.
	//
	// ⚠ It is what the headline COUNTS. The pre-W7 headline counted ROWS and
	// called them vehicles, so a truck with two open runs was reported as two
	// trucks on the ice — on the sheet an officer carries when the board
	// cannot be reached.
	//
	// ⚠ It does NOT merge the blocks. Which group a person is listed under is
	// satisfied by the resolved identity; merging two in-transit blocks into
	// one is the operation [[D-64]]'s safety clause warns about, it is the
	// only one that can drop a person, and D-64's text does not ask for it.
	VehicleIdentity string
	// Destination: the peer site id this run is bound for. Rendered through
	// the config display name where the surface has one.
	Destination string
	// Driver: resolved display name, falling back to the person id — an
	// incident report must be able to say who was at the wheel (D-4), and a
	// blank cell would read as "no driver was recorded", a different and
	// much worse claim than "this box cannot name them yet".
	Driver string
	// DeadlineMinutes is the journey ALLOWANCE promised at dispatch, in
	// minutes. Never nil in practice for a controlled trip — Dispatch
	// refuses a destination without a deadline, precisely so no trip can be
	// an open expectation with no alert promise — but carried as a pointer
	// so a fixture-authored trip renders as "no allowance recorded" instead
	// of as a confident zero.
	DeadlineMinutes *int64
	// Persons is everyone aboard, driver included (the driver is always an
	// occupant row, §7), name-sorted for reading aloud.
	Persons []OccupantRef
}

// Allowance renders DeadlineMinutes for a human to read aloud, singular and
// plural both correct. It is a METHOD rather than template arithmetic
// because DeadlineMinutes is a POINTER — a Go template can print through it
// but cannot compare it to 1, so an inline `{{if ne .DeadlineMinutes 1}}s`
// is a render-time error, and the version that "worked" printed
// "1 minutes" on a real printed sheet (MEASURED, 2026-08-21). Empty string
// when no allowance was recorded, so the caller can say so in words rather
// than print a confident zero.
func (r InTransitRow) Allowance() string {
	if r.DeadlineMinutes == nil {
		return ""
	}
	m := *r.DeadlineMinutes
	if m == 1 || m == -1 {
		return "1 minute"
	}
	return strconv.FormatInt(m, 10) + " minutes"
}

// Count is how many PEOPLE this run has aboard. A Go template cannot sum a
// nested range, and len(rows) is a count of VEHICLES, never of people —
// printing one as if it were the other is exactly the kind of quiet wrong
// number a muster cannot carry.
func (r InTransitRow) Count() int { return len(r.Persons) }

// InTransitFrom answers "who did THIS post put on the road and not hear
// back about" — open controlled runs whose origin is siteID.
//
// SCOPE, DELIBERATELY, AND WHAT EACH EXCLUSION SENDS ELSEWHERE:
//
//   - open trips only. An ARRIVED trip's occupants are on the destination's
//     on-site roster (and any who were not aboard are NotTracked). A
//     CANCELLED trip's occupants leave this section — the trip is not
//     happening, and continuing to render them as "on the road to the Pad"
//     would be a claim the record does not support. NOTE FOR WHOEVER READS
//     THIS NEXT: a cancel never changes anyone's visit state (TripCancelled:
//     "the occupants were checked out at dispatch and STAY checked out"),
//     and NotTracked excludes cancelled trips by its own contract, so a
//     cancelled controlled trip's occupants are on NO muster category. That
//     is a REAL, PRE-EXISTING accountability hole, separate from the one
//     this file closes, and it is reported rather than silently absorbed
//     here: naming those people needs a ruling on what the sheet should say
//     about them, not an inference.
//
//   - controlled destinations only (destination_site set). A run to a
//     configured pad is OutAtUncontrolled's business — it has no gate, no
//     timer and a different responsibility boundary. A degenerate local trip
//     (§10, both destinations nil) is half of a check-in and nobody departs
//     on it.
//
//   - occupants with no recorded return. sightedSince is the SAME predicate
//     OutAtUncontrolled and NotTracked use, so the three categories can
//     never disagree about what counts as a recorded return. It matters
//     here for two reachable cases: a crew that turned back and checked in
//     again at the origin (rule 3 — an open origin visit, provably
//     post-dispatch) would otherwise be listed on site AND on the road on
//     the same sheet; and a person carried onward by a LATER trip from this
//     origin (rule 2) would otherwise appear on two in-transit rows at once.
//     Both are within-sheet double counts, and both are removed here rather
//     than papered over in the template.
func InTransitFrom(st *store.Store, siteID string) ([]InTransitRow, error) {
	return openRunsFrom(st, siteID, false)
}

// openRunsFrom is InTransitFrom's body, shared with RoadWorkFrom
// (road_work.go — James, 2026-10-03) through one predicate: roadWork=false
// keeps open CONTROLLED runs (destination_site set), exactly the set this
// function always returned; roadWork=true keeps open ROAD-WORK runs (the kind
// side table) instead. Everything else — composition, the sightedSince return
// rule, the ordering — is one code path, so the muster's two on-the-road
// sections cannot disagree about who is aboard or who is back.
func openRunsFrom(st *store.Store, siteID string, roadWork bool) ([]InTransitRow, error) {
	// Open-first (F30): the open trip_state set is bounded by trips actually
	// in flight, never by the season's cumulative trip count.
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
		if name == "" {
			// Never let a row vanish because a roster reference has not
			// replicated here yet — show the id.
			return personID, nil
		}
		return name, nil
	}

	// ⟦W9a-2 consumer 3, W9a-18⟧ COMPOSED — the in-transit board renders
	// Class-A facts LIVE (driver, aboard set, vehicle) — and merge-resolved
	// at the read, so one human is one row on a document whose whole purpose
	// is that summing every post's sheet counts each person exactly once.
	merge, err := LoadPersonMergeMap(st)
	if err != nil {
		return nil, err
	}
	// ⟦W7, item 9⟧ The vehicle merge map, derived per read exactly as the
	// person one is. Two duplicate records an officer has joined are ONE
	// truck here from the moment the join replicates, and nothing is cached
	// between reads. ⟦James, 2026-10-03⟧ Read through LiveVehicles — the
	// same composition the alert strip, the inbound board and the gate
	// inbound row use — so the origin's sheet and the destination's board
	// cannot name two different trucks for one open run.
	live, err := NewLiveVehicles(st)
	if err != nil {
		return nil, err
	}
	var rows []InTransitRow
	byID := map[string]Trip{}
	for _, s := range open {
		c, err := EffectiveTrip(st, s.TripID)
		if err != nil {
			return nil, err
		}
		if c == nil || c.Trip.OriginSite != siteID {
			continue
		}
		// The ONE predicate the two sections differ on. A road-work run is
		// destination-less, so the controlled filter already excluded it
		// before road work existed; the kind filter admits exactly it.
		if roadWork {
			if !c.RoadWork {
				continue
			}
		} else if c.Trip.DestinationSite == nil {
			continue
		}
		t := &c.Trip
		aboardRaw, err := StillAboard(st, c)
		if err != nil {
			return nil, err
		}
		aboard, err := composedOccupantRefs(st, aboardRaw, merge, nameOf)
		if err != nil {
			return nil, err
		}
		if len(aboard) == 0 {
			// Every occupant is provably back. The vehicle may still be out,
			// but this section is a roster of PEOPLE, and it has none.
			continue
		}
		sortOccupantRefsByName(aboard)
		driver, err := nameOf(t.DriverPersonID)
		if err != nil {
			return nil, err
		}
		// ⟦W7-34(a)⟧ Identity and display, from the same resolution —
		// LiveVehicles.Compose (vehicle_live.go) carries the reasoning for
		// the unidentified, unreplicated-record and role-only cases.
		display, identity, err := live.Compose(*c)
		if err != nil {
			return nil, err
		}
		row := InTransitRow{
			TripID: t.TripID, Vehicle: display, VehicleIdentity: identity,
			Driver:          driver,
			DeadlineMinutes: t.DeadlineMinutes, Persons: aboard,
		}
		if t.DestinationSite != nil {
			row.Destination = *t.DestinationSite // "" on a road-work run: there is none
		}
		rows = append(rows, row)
		byID[t.TripID] = *t
	}
	// Oldest first by the trip event's source key (row 10) — never rowid,
	// never wall-time. The run that has been out longest reads first, which
	// is the order somebody chasing a late vehicle wants.
	sort.Slice(rows, func(i, j int) bool {
		a, b := byID[rows[i].TripID], byID[rows[j].TripID]
		if a.NodeEpoch != b.NodeEpoch {
			return a.NodeEpoch < b.NodeEpoch
		}
		if a.Seq != b.Seq {
			return a.Seq < b.Seq
		}
		return a.TripID < b.TripID
	})
	return rows, nil
}

// StillAboard is the run's effective occupant set (c.OccupantIDs) less
// everyone the record shows back at its origin since it left — the
// sightedSince return rule, read at the moment of asking. ⟦F15, James
// 2026-10-05: "If someone aboard a truck turns out never to have left and
// checks back in at the origin gate, they come off that trip EVERYWHERE."⟧
// It is the ONE place that rule is applied to an open run, and every
// present-tense reader of "who is on this truck" goes through it: the
// muster's in-transit and road-work sections (openRunsFrom), the alert
// strip, the inbound and outbound boards and the analytics runs
// (composeOpenExpectations, OpenRoadWork — Expectation.Occupants and
// OccupantList), the arrival's visit set (planExpectedArrival — a person
// back at the origin is never entered at the destination), the pad
// "Returned" manifest and the aboard-elsewhere warning. ⟦was: the muster
// alone applied sightedSince; the boards, the strip's "N aboard" and the
// arrival read c.OccupantIDs raw, so a crew member who turned back was on
// site at the origin and still expected — and checked in — at the
// destination⟧
//
// Derived at read time from events that already exist; nothing is
// authored and the record of who was dispatched is untouched (the trip's
// composition, NotTracked's raw∖effective arithmetic and every correction
// still read c.OccupantIDs). Eventual by construction: the destination
// learns of the origin check-in by replication and expects the person
// until it does. A run whose WHOLE crew is back stays an open run with its
// deadline — it is a vehicle trip — and reads "0 aboard".
func StillAboard(st *store.Store, c *ComposedTrip) ([]string, error) {
	if c == nil {
		return nil, nil
	}
	t := &c.Trip
	out := make([]string, 0, len(c.OccupantIDs))
	for _, id := range c.OccupantIDs {
		back, err := sightedSince(st, id, t.OriginSite, t.NodeEpoch, t.Seq)
		if err != nil {
			return nil, err
		}
		if !back {
			out = append(out, id)
		}
	}
	return out, nil
}

// OpenOutbound — James, 2026-10-03 (NEW-a: "the sending post sees its own
// overdue outbound trucks") — lists the open CONTROLLED runs THIS post sent
// out, as Expectations: the deadline scanner's input for the origin half of
// every Ice Road <-> Ice Pad trip, exactly as OpenExpectations is the input
// for the destination half. Composed by the SAME code (composeOpenExpectations,
// tripauthor.go) — live vehicle, merge-resolved manifest, driver fallback —
// so the alert row at the origin and the alert row at the destination
// describe one truck in one way. The selection is the in-transit sheet's
// (InTransitFrom): origin == this post AND a destination post is set; a
// road-work run (destination-less) has its own list (OpenRoadWork) and an
// uncontrolled run has no deadline to scan.
//
// Unlike InTransitFrom this does NOT drop a RUN whose occupants are all
// provably back: a vehicle's expectation stays open until an arrival closes
// the trip, and a run whose whole crew turned back and re-entered here is
// still an open expectation at the destination, so it must still be one
// here — the two scans watch the same set of open trips. Its PERSONS are
// StillAboard's, as the muster's are (F15), so such a run reads "0 aboard".
func OpenOutbound(st *store.Store, originSite string) ([]Expectation, error) {
	return composeOpenExpectations(st, func(c *ComposedTrip) bool {
		return c.Trip.OriginSite == originSite && c.Trip.DestinationSite != nil
	})
}

// InTransitCount sums PEOPLE across runs — see InTransitRow.Count on why a
// count of vehicles must never stand in for it.
func InTransitCount(rows []InTransitRow) int {
	n := 0
	for _, r := range rows {
		n += len(r.Persons)
	}
	return n
}

// sortOccupantRefsByName puts a manifest in the order a human scans when
// reading it aloud against the people in front of them, not in the order the
// occupant rows were authored. (web/trips.go has the same ordering for the
// arrival manifest; this is the domain-side copy so the muster does not have
// to reach into the web layer for it.)
func sortOccupantRefsByName(refs []OccupantRef) {
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Name != refs[j].Name {
			return refs[i].Name < refs[j].Name
		}
		return refs[i].PersonID < refs[j].PersonID
	})
}
