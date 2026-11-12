// muster_at.go — the muster for any moment, and a person's last sighting
// (FIX16a, James 2026-10-06: "pick a site, a date and a time and get a
// muster-style list of who was ON SITE and who was ON THE ROAD at that
// moment"; "last seen for any person"). web/muster_at.go and
// web/last_seen.go are the pages; this file is the one read they need.
//
// WHAT IT IS. ONE linear replay of the log through store.EachEvent — the
// visit report's own replay (visit_report.go, historical-muster-v1 §11.2):
// visitReplay.apply mints and closes every visit exactly as the folds do,
// and reportInstant labels every end over correctedInstants resolved up
// front (D2 / AN-PANIC-1: nothing reads the store inside the callback).
// The same walk keeps every trip_dispatched, trip_arrival and
// trip_cancelled it passes, so a run's start, end and the arrival's
// held-out list come off the log too. After the walk, each RUN in scope is
// composed through EffectiveTrip (corrections applied, D1's dropped
// occupants visible as raw ∖ effective) with its trip_state — point reads.
//
// So the moment muster honours, with no rule of its own:
//   - time corrections: every instant is the effective one (reportInstant);
//   - FIX8 hand-entered per-person times: they are the envelopes' own
//     entered pairs (FIX8 option C splits differing times into one event per
//     time), labelled "claimed" and marked;
//   - F15 turn-backs: a person is off a run from the moment the record shows
//     them back (MomentLog.returned — sightedSince's rules, made historical);
//   - D1 NotTracked: people a correction took off a run are listed as not
//     tracked from its dispatch until they are next sighted.
//
// WHAT IT IS NOT. No event type, no projection column, no side table, no
// cache: computed on request, like the visit report, analytics and search.
// ⟦FIX17 (perf, 2026-10-06): the log walk is reused between WRITES
// (store.Memo, keyed by the store's head — any write rebuilds it); the runs a
// request asks for are composed per request, as before.⟧
// §4.4: nothing here ORDERS records by a clock — the replay is the stream's
// order, and "after the run" is a same-stream key comparison (keyAfter).
// The moment is a FILTER on each record's own instant, as each post's clock
// recorded it, and the page says so.
package domain

import (
	"fmt"
	"slices"
	"sort"
	"time"

	"sitecontrol/internal/store"
)

// MomentRunKind is what a run was.
type MomentRunKind string

const (
	RunToPost   MomentRunKind = "post"      // a controlled destination
	RunToPad    MomentRunKind = "pad"       // an uncontrolled destination: no gate, no timer
	RunRoadWork MomentRunKind = "road_work" // out working on the road
)

// MomentRun is one run (a dispatch with somewhere to go) as the log holds
// it. The destination-less dispatch half of a gate check-in is not a run.
type MomentRun struct {
	TripID     string
	OriginSite string
	Key        store.Key // the dispatch envelope
	Dispatch   RecordedInstant
	Kind       MomentRunKind
	// RawAboard is the driver and occupants as DISPATCHED (raw ids).
	RawAboard []string
	// Composed is the run with every correction applied (EffectiveTrip);
	// nil when the run was outside what the caller asked to compose, or the
	// trip row has not reached this post.
	Composed *ComposedTrip
	// State is trip_state's state; "" when not known here.
	State string
	// End is the event that closed the run — its arrival, or its cancel —
	// nil while the run is open.
	End *MomentRunEnd
}

// MomentRunEnd is what closed a run.
type MomentRunEnd struct {
	Key       store.Key
	At        RecordedInstant
	Cancelled bool
	ArrivalID string
	// NotEntered: aboard at arrival but held out at that gate for its
	// orientation (D-91/D-98) — accounted for on /orientation, never on a
	// muster (R-11), so never "not tracked".
	NotEntered map[string]bool
}

// TurnedBack reports whether the arrival that closed the run was recorded
// at its own origin (domain.TripsReturnedToOrigin's rule).
func (r MomentRun) TurnedBack() bool {
	return r.End != nil && !r.End.Cancelled && r.End.Key.SiteID == r.OriginSite
}

// Destination is the run's effective destination post ("" when none).
func (r MomentRun) Destination() string {
	if r.Composed != nil && r.Composed.Trip.DestinationSite != nil {
		return *r.Composed.Trip.DestinationSite
	}
	return ""
}

// Pad is the run's effective uncontrolled destination ("" when none).
func (r MomentRun) Pad() string {
	if r.Composed != nil && r.Composed.Trip.DestinationUncontrolled != nil {
		return *r.Composed.Trip.DestinationUncontrolled
	}
	return ""
}

// MomentStream is one stream as far as it has reached this post, with the
// instant of its last record (on that post's clock).
type MomentStream struct {
	SiteID     string
	NodeEpoch  int64
	AppliedSeq int64
	Last       RecordedInstant
}

// MomentLog is the season as the moment muster and last-seen read it.
type MomentLog struct {
	Visits  []VisitRecord // the visit report's rows, PersonID merge-resolved
	Runs    []MomentRun   // dispatch order within each stream
	Streams []MomentStream

	tripKey  map[string]store.Key // every trip, gate check-in halves included
	byPerson map[string][]int     // raw person id → indexes into Visits
	runIndex map[string]int       // trip id → index into Runs
	merge    AliasMap
	nameOf   map[string]string
}

// RunByID returns the run, or nil.
func (l *MomentLog) RunByID(tripID string) *MomentRun {
	if i, ok := l.runIndex[tripID]; ok {
		return &l.Runs[i]
	}
	return nil
}

// LoadMomentLog replays the log once (see the file comment). compose
// chooses which runs are composed after the walk (nil: every run); a run not
// composed is still listed, with Composed nil, and places nobody.
//
// FIX17 (perf): the walk — everything before the compose step — is a pure
// function of the stored rows and is reused between writes (store.Memo);
// each call gets its own Visits, Runs and Streams, composes its own runs,
// and keeps its own name cache. Shared and never written after the walk:
// the trip/person/run indexes and the merge map.
func LoadMomentLog(st *store.Store, compose func(MomentRun) bool) (*MomentLog, error) {
	fail := func(err error) (*MomentLog, error) {
		return nil, fmt.Errorf("domain: moment log: %w", err)
	}
	v, err := st.Memo("domain.momentWalk", func() (any, error) { return walkMomentLog(st) })
	if err != nil {
		return nil, err
	}
	w := v.(*momentWalk)
	l := &MomentLog{
		Visits: slices.Clone(w.l.Visits), Runs: slices.Clone(w.l.Runs), Streams: slices.Clone(w.l.Streams),
		tripKey: w.l.tripKey, byPerson: w.l.byPerson, runIndex: w.l.runIndex, merge: w.l.merge,
	}
	arrivals, arrivalsByTrip, cancels := w.arrivals, w.arrivalsByTrip, w.cancels

	// Compose the runs asked for: point reads, after the walk.
	for i := range l.Runs {
		r := &l.Runs[i]
		if compose != nil && !compose(*r) {
			continue
		}
		c, err := EffectiveTrip(st, r.TripID)
		if err != nil {
			return fail(err)
		}
		r.Composed = c
		ts, err := TripStateByID(st, r.TripID)
		if err != nil {
			return fail(err)
		}
		if ts == nil {
			continue
		}
		r.State = ts.State
		switch ts.State {
		case TripStateArrived, TripStateArrivedAfterCancel:
			id := ""
			if ts.ArrivalID != nil {
				id = *ts.ArrivalID
			} else if ids := arrivalsByTrip[r.TripID]; len(ids) > 0 {
				id = ids[0]
			}
			if a, ok := arrivals[id]; ok {
				end := &MomentRunEnd{Key: a.key, At: a.at, ArrivalID: id, NotEntered: map[string]bool{}}
				for _, p := range a.notEntered {
					end.NotEntered[p] = true
				}
				r.End = end
			}
		case TripStateCancelled:
			if c, ok := cancels[r.TripID]; ok {
				end := c
				r.End = &end
			}
		}
	}
	return l, nil
}

// momentWalk is LoadMomentLog's walk of the log, before any run is composed.
type momentWalk struct {
	l              *MomentLog
	arrivals       map[string]momentArrivalSeen
	arrivalsByTrip map[string][]string // trip_ref → arrival ids, replay order
	cancels        map[string]MomentRunEnd
}

type momentArrivalSeen struct {
	key        store.Key
	at         RecordedInstant
	tripRef    string
	notEntered []string
}

func walkMomentLog(st *store.Store) (*momentWalk, error) {
	fail := func(err error) (*momentWalk, error) {
		return nil, fmt.Errorf("domain: moment log: %w", err)
	}
	corrected, err := correctedInstants(st)
	if err != nil {
		return fail(err)
	}
	type arrivalSeen = momentArrivalSeen
	l := &MomentLog{tripKey: map[string]store.Key{}, byPerson: map[string][]int{}, runIndex: map[string]int{}}
	arrivals := map[string]arrivalSeen{}
	arrivalsByTrip := map[string][]string{} // trip_ref → arrival ids, replay order
	cancels := map[string]MomentRunEnd{}
	at := func(site string, key store.Key, basis timeBasis) RecordedInstant {
		ms, label, ok := reportInstant(key, basis, corrected)
		return RecordedInstant{SiteID: site, MS: ms, OK: ok, Label: label}
	}
	// FIX17: the walk is the log follower's (log_follow.go) — the same visit
	// replay and the same trip payloads in the same order, resumed rather
	// than restarted; the records come back as this walk's own copies.
	snap, err := followLog(st, true)
	if err != nil {
		return fail(err)
	}
	records := snap.records
	for _, m := range snap.streams {
		mark := MomentStream{SiteID: m.SiteID, NodeEpoch: m.NodeEpoch, AppliedSeq: m.AppliedSeq}
		if e, ok := snap.last[[2]any{m.SiteID, m.NodeEpoch}]; ok {
			mark.Last = at(m.SiteID, e.key, e.basis)
		}
		l.Streams = append(l.Streams, mark)
	}
	for _, e := range snap.trips {
		switch {
		case e.dispatched != nil:
			p := e.dispatched
			if p.TripID == "" {
				continue
			}
			l.tripKey[p.TripID] = e.key
			var kind MomentRunKind
			switch {
			case p.DestinationSite != nil:
				kind = RunToPost
			case p.DestinationUncontrolled != nil:
				kind = RunToPad
			case p.RoadWork:
				kind = RunRoadWork
			default:
				continue // a gate check-in's first half: not a run
			}
			raw := []string{}
			for _, id := range append([]string{p.DriverPersonID}, p.OccupantPersonIDs...) {
				if id != "" && !containsString(raw, id) {
					raw = append(raw, id)
				}
			}
			l.runIndex[p.TripID] = len(l.Runs)
			l.Runs = append(l.Runs, MomentRun{TripID: p.TripID, OriginSite: e.site, Key: e.key,
				Dispatch: at(e.site, e.key, e.basis), Kind: kind, RawAboard: raw})
		case e.arrival != nil:
			p := e.arrival
			if p.ArrivalID == "" {
				continue
			}
			a := arrivalSeen{key: e.key, at: at(e.site, e.key, e.basis), notEntered: p.NotEntered}
			if p.TripRef != nil {
				a.tripRef = *p.TripRef
				arrivalsByTrip[a.tripRef] = append(arrivalsByTrip[a.tripRef], p.ArrivalID)
			}
			arrivals[p.ArrivalID] = a
		case e.cancelled != nil:
			p := e.cancelled
			if p.TripID == "" {
				continue
			}
			if _, done := cancels[p.TripID]; !done {
				cancels[p.TripID] = MomentRunEnd{Key: e.key, At: at(e.site, e.key, e.basis), Cancelled: true}
			}
		}
	}

	merge, err := LoadPersonMergeMap(st)
	if err != nil {
		return fail(err)
	}
	l.merge = merge
	l.Visits = make([]VisitRecord, 0, len(records))
	for _, rec := range records {
		rec.PersonID = merge.Resolve(rec.VisitPersonID)
		rec.ArrivedAtMS, rec.ArrivalLabel, _ = reportInstant(rec.ArrivalKey, rec.arrival, corrected)
		if rec.ClosedBy != VisitStillOpen {
			rec.ClosedAtMS, rec.CloseLabel, _ = reportInstant(rec.CloseKey, rec.close, corrected)
		}
		l.byPerson[rec.VisitPersonID] = append(l.byPerson[rec.VisitPersonID], len(l.Visits))
		l.Visits = append(l.Visits, *rec)
	}
	return &momentWalk{l: l, arrivals: arrivals, arrivalsByTrip: arrivalsByTrip, cancels: cancels}, nil
}

// keyAfter: a was authored after b on the SAME stream — true authoring
// order (§8.3); never a cross-site comparison.
func keyAfter(a, b store.Key) bool {
	if a.SiteID != b.SiteID {
		return false
	}
	return a.NodeEpoch > b.NodeEpoch || (a.NodeEpoch == b.NodeEpoch && a.Seq > b.Seq)
}

// untrustedClock: the instant was read off a clock this post does not
// trust (unsynced/unverified, or an ill pair).
func untrustedClock(in RecordedInstant) bool {
	return in.Label == timeObservedClockBad || in.Label == timeUntrusted
}

// UntrustedClock is untrustedClock for the pages.
func (r RecordedInstant) UntrustedClock() bool { return untrustedClock(r) }

// returned is the "provably back" rule for one person off one run, as of
// atMS — sightedSince's rules 2 and 3 (and the arrival's own visits, the
// run's return manifest), with each sighting counted only from its own
// recorded instant. A visit counts when its time in is readable and at or
// before the moment, and it is
//   - minted by the event that closed the run (they arrived on it), or
//   - at the run's origin and minted after the dispatch on the origin's own
//     stream (back through that gate — F15's turn-back), or
//   - opened by a later trip from the same origin stream (carried onward).
func (l *MomentLog) returned(person string, r *MomentRun, atMS int64) bool {
	for _, i := range l.byPerson[person] {
		v := &l.Visits[i]
		in := v.ArrivalInstant()
		if !in.OK || in.MS > atMS {
			continue
		}
		if r.End != nil && !r.End.Cancelled && v.ArrivalKey == r.End.Key {
			return true
		}
		if v.SiteID == r.OriginSite && keyAfter(v.ArrivalKey, r.Key) {
			return true
		}
		if v.TripRef != "" {
			if k, ok := l.tripKey[v.TripRef]; ok && keyAfter(k, r.Key) {
				return true
			}
		}
	}
	return false
}

// MomentVisit is one visit placed (or not) at the moment.
type MomentVisit struct {
	Rec     VisitRecord
	In, Out RecordedInstant
	// NoSignOut: on site at the moment by the record, but the record was
	// closed later by the person's own return (re_checkin / next_arrival),
	// not by a recorded departure — they may have left earlier.
	NoSignOut bool
	// Why (Unplaced only) says in words why the record could not be placed.
	Why string
}

// MomentAboard is one run on the road at the moment, with who was aboard.
type MomentAboard struct {
	Run       *MomentRun
	PersonIDs []string // raw ids, dispatch order
	// EndUnknown: the run has an end on record whose time cannot be read.
	EndUnknown bool
}

// MomentGapWhy is why a person is not tracked.
type MomentGapWhy string

const (
	GapNotAboardAtArrival   MomentGapWhy = "not_aboard_at_arrival"
	GapTakenOffByCorrection MomentGapWhy = "taken_off_by_correction"
	GapRunCancelled         MomentGapWhy = "run_cancelled"
)

// MomentGap is one person who left on a run and is, at the moment, on no
// list the record can place them on.
type MomentGap struct {
	Run      *MomentRun
	PersonID string // raw
	Why      MomentGapWhy
	Since    RecordedInstant // when they became untracked
}

// MomentState is who the record places where at one instant.
type MomentState struct {
	AtMS       int64
	OnSite     []MomentVisit
	Unplaced   []MomentVisit
	OnRoad     []MomentAboard
	NotTracked []MomentGap
}

// At places every visit and every composed run at atMS. margin is how close
// to the moment an instant read off an untrusted clock must be for the
// record to be shown as "could not be placed" instead of silently left out.
func (l *MomentLog) At(atMS int64, margin time.Duration) MomentState {
	out := MomentState{AtMS: atMS}
	m := margin.Milliseconds()
	for _, rec := range l.Visits {
		in, cl := rec.ArrivalInstant(), rec.CloseInstant()
		open := rec.ClosedBy == VisitStillOpen
		mv := MomentVisit{Rec: rec, In: in, Out: cl}
		if !in.OK {
			if !open && cl.OK && cl.MS <= atMS {
				continue // gone by then, whenever they came
			}
			mv.Why = "the time in cannot be read (its correction record is incomplete here)"
			out.Unplaced = append(out.Unplaced, mv)
			continue
		}
		if in.MS > atMS {
			if untrustedClock(in) && in.MS-atMS <= m {
				mv.Why = "signed in just after this time, and that time is uncertain — may already have been on site"
				out.Unplaced = append(out.Unplaced, mv)
			}
			continue
		}
		switch {
		case open:
		case !cl.OK:
			mv.Why = "on site by the time in, but the time out cannot be read"
			out.Unplaced = append(out.Unplaced, mv)
			continue
		case cl.MS > atMS:
			mv.NoSignOut = !rec.ClosedBy.DepartureRecorded()
		default:
			if untrustedClock(cl) && atMS-cl.MS <= m {
				mv.Why = "signed out just before this time, and that time is uncertain — may still have been on site"
				out.Unplaced = append(out.Unplaced, mv)
			}
			continue
		}
		out.OnSite = append(out.OnSite, mv)
	}

	for i := range l.Runs {
		r := &l.Runs[i]
		if r.Composed == nil {
			continue
		}
		if !r.Dispatch.OK || r.Dispatch.MS > atMS {
			continue // not yet left (an unreadable dispatch: the origin visit it closed is unplaced instead)
		}
		ended, endUnknown := false, false
		if r.End != nil {
			if r.End.At.OK {
				ended = r.End.At.MS <= atMS
			} else {
				endUnknown = true
			}
		}
		effective := map[string]bool{}
		aboard := MomentAboard{Run: r, EndUnknown: endUnknown}
		for _, p := range r.Composed.OccupantIDs {
			effective[p] = true
		}
		// Effective set in dispatch order, then any a correction added.
		order := make([]string, 0, len(r.RawAboard)+len(r.Composed.OccupantIDs))
		for _, p := range r.RawAboard {
			if effective[p] {
				order = append(order, p)
			}
		}
		for _, p := range r.Composed.OccupantIDs {
			if !containsString(order, p) {
				order = append(order, p)
			}
		}
		for _, p := range order {
			if l.returned(p, r, atMS) {
				continue
			}
			switch {
			case !ended || r.Kind == RunToPad:
				// On the road; a pad crew stays out at the pad until its
				// return is recorded (OutAtUncontrolled's rule).
				aboard.PersonIDs = append(aboard.PersonIDs, p)
			case r.End.Cancelled:
				out.NotTracked = append(out.NotTracked, MomentGap{Run: r, PersonID: p, Why: GapRunCancelled, Since: r.End.At})
			case r.End.NotEntered[p]:
				// Held out at the gate for orientation: /orientation's list.
			default:
				out.NotTracked = append(out.NotTracked, MomentGap{Run: r, PersonID: p, Why: GapNotAboardAtArrival, Since: r.End.At})
			}
		}
		// D1: dispatched aboard, taken off by a correction — not tracked from
		// the dispatch until next sighted.
		for _, p := range r.RawAboard {
			if effective[p] || l.returned(p, r, atMS) {
				continue
			}
			out.NotTracked = append(out.NotTracked, MomentGap{Run: r, PersonID: p, Why: GapTakenOffByCorrection, Since: r.Dispatch})
		}
		if len(aboard.PersonIDs) > 0 {
			out.OnRoad = append(out.OnRoad, aboard)
		}
	}
	return out
}

// PersonVisits returns the visits recorded under any of ids (raw).
func (l *MomentLog) PersonVisits(ids map[string]bool) []VisitRecord {
	var out []VisitRecord
	for id := range ids {
		for _, i := range l.byPerson[id] {
			out = append(out, l.Visits[i])
		}
	}
	sort.Slice(out, func(i, j int) bool { return keyString(out[i].ArrivalKey) < keyString(out[j].ArrivalKey) })
	return out
}

// AmendedTripIDs is every trip some trip_amended names — the runs whose
// aboard set a correction may have changed, so a per-person read that
// composes only the runs a person was dispatched on composes these too.
func AmendedTripIDs(st *store.Store) (map[string]bool, error) {
	rows, err := dumpSection[struct {
		TripID *string `json:"trip_id"`
	}](st, "trip_amendment")
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, r := range rows {
		if r.TripID != nil {
			out[*r.TripID] = true
		}
	}
	return out, nil
}

// OccupantRef is one person aboard a run, for the moment muster's
// exception sections (on the road, not tracked) — merge-resolved at the
// read exactly as the live sheet's in-transit and not-tracked rows are
// (composedOccupantRefs; [[D-59]]: one human, one row). The ON-SITE roster
// never comes through here: its body reads Visit through MusterLive with
// the person key unresolved ([[D-50]] consequence 5 / W3-7). A raw id
// whose name has not replicated here reads as the id, never blank.
func (l *MomentLog) OccupantRef(st *store.Store, raw string) (OccupantRef, error) {
	refs, err := composedOccupantRefs(st, []string{raw}, l.merge, func(id string) (string, error) {
		if l.nameOf == nil {
			l.nameOf = map[string]string{}
		}
		if n, ok := l.nameOf[id]; ok {
			return n, nil
		}
		p, err := PersonByID(st, id)
		if err != nil {
			return "", err
		}
		n := ""
		if p != nil {
			n = p.Name
		}
		l.nameOf[id] = n
		return n, nil
	})
	if err != nil || len(refs) == 0 {
		return OccupantRef{PersonID: raw, Name: raw}, err
	}
	// The row's link names the human: the representative record.
	refs[0].PersonID = l.merge.Resolve(raw)
	return refs[0], nil
}
