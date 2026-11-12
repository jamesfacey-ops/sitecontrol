// deadline_runs.go — every run that carried a deadline, with when it was due
// and how it ended: the read behind the overdue history (FIX16f, James
// 2026-10-06: "a list of trips that went past their deadline, showing how
// late each was … and the outcome (arrived, turned back, still open)").
//
// WHAT IT IS. A read over what already exists: the composed trip
// (EffectiveTrip — so a corrected allowance is the one judged), its state
// (trip_state, TripsReturnedToOrigin), the recorded instants of its dispatch
// and binding arrival (LoadTripInstants — reportInstant, the one place a
// report instant is produced), and, for a cancelled run, the cancel event's
// own instant. No event, column or table.
//
// THE DUE TIME is the one every deadline judge uses (James 2026-10-06: from
// the typed time — EffectiveDeparture, the basis of the scan's countdown and
// web/analytics_site.go recordedBasis): the dispatch's typed time out, else
// its authored_at, on the origin's own clock, plus the allowance. ⟦was:
// FINDINGS-R4 D13, the authored_at alone⟧ The end is the binding arrival as the receiving post
// recorded it (its own clock), the turned-back truck's arrival back at its
// origin, or the cancel. ⚠ So "how late" subtracts two posts' clocks for a
// run that arrived elsewhere; the page says so. It is the same comparison
// the dispatch view's red OVERDUE makes (dispatch_allsites.go), not a new
// one.
//
// WHAT IT IS NOT. It does not decide "went past its deadline" — the web layer
// joins it with the logged acknowledgments (deadline.AckHistory: an end's
// red ack is that end's own verdict) and the reading of "now".
package domain

import (
	"fmt"
	"slices"
	"sort"

	"sitecontrol/internal/store"
)

// How a deadline run ended, as the history names it.
const (
	RunArrived    = "arrived"
	RunTurnedBack = "turned_back"
	RunBack       = "back" // a road-work run closed by its arrival back at its origin
	RunOpen       = "open"
	RunCancelled  = "cancelled"
)

// DeadlineRun is one run that carried a deadline.
type DeadlineRun struct {
	TripID      string
	Origin      string
	Destination string // "" for road work
	// Uncontrolled: the uncontrolled place's name, when that is where the
	// (effective) run went.
	Uncontrolled string
	RoadWork     bool
	Vehicle      string // as recorded at dispatch (the frozen snapshot)
	Allowance    int64  // minutes, the effective deadline
	Dispatched   RecordedInstant
	// DueMS: the dispatch's effective departure (EffectiveDeparture — the
	// typed time out, else authored_at) plus the allowance — or, with no
	// envelope here, the effective dispatch instant plus it. DueOK false:
	// neither resolves.
	DueMS int64
	DueOK bool
	// RecordedMS: the dispatch envelope's authored_at (0 when the envelope
	// is not here). TimedFromMS: the instant DueMS counts from. The two
	// differ when the officer entered an earlier time out.
	RecordedMS  int64
	TimedFromMS int64
	// Outcome is one of the Run* values. End is the instant that closed it
	// (the binding arrival, the return, the cancel); zero while open.
	Outcome string
	End     RecordedInstant
}

// Left is the instant the run left, for placing it in a reporting period:
// the dispatch's recorded instant, else the due time less the allowance (its
// effective departure). ok=false: neither resolves. The overdue history and
// the dispatch reports' late runs (reports.go RunsPastAllowance) BOTH place a
// run by this, so the two agree on which runs a period holds (FIX16
// integration).
func (r DeadlineRun) Left() (ms int64, ok bool) {
	if r.Dispatched.OK {
		return r.Dispatched.MS, true
	}
	if r.DueOK {
		return r.DueMS - r.Allowance*60_000, true
	}
	return 0, false
}

// DeadlineRuns lists every run this store holds that carried a deadline,
// dispatch order (origin, epoch, seq).
//
// FIX17 (perf): reused between writes (store.Memo) — it composes every run
// of the season and replays the log for its instants; the overdue history
// and the dispatch reports read it. The caller gets its own slice.
func DeadlineRuns(st *store.Store) ([]DeadlineRun, error) {
	v, err := st.Memo("domain.DeadlineRuns", func() (any, error) { return deadlineRuns(st) })
	if err != nil {
		return nil, err
	}
	return slices.Clone(v.([]DeadlineRun)), nil
}

func deadlineRuns(st *store.Store) ([]DeadlineRun, error) {
	fail := func(err error) ([]DeadlineRun, error) {
		return nil, fmt.Errorf("domain: deadline runs: %w", err)
	}
	trips, err := Trips(st)
	if err != nil {
		return fail(err)
	}
	roadWork, err := roadWorkTripIDs(st)
	if err != nil {
		return fail(err)
	}
	states, err := TripStates(st)
	if err != nil {
		return fail(err)
	}
	stateOf := make(map[string]TripState, len(states))
	for _, s := range states {
		stateOf[s.TripID] = s
	}
	returned, err := TripsReturnedToOrigin(st)
	if err != nil {
		return fail(err)
	}
	instants, err := LoadTripInstants(st)
	if err != nil {
		return fail(err)
	}
	cancels, err := cancelInstants(st)
	if err != nil {
		return fail(err)
	}
	sortBySourceKeyTrips(trips)
	var out []DeadlineRun
	for _, t := range trips {
		// Candidates: a run to somewhere or on the road. A local check-in's
		// destination-less half has no allowance and is never amended into
		// one by the gate.
		if t.DestinationSite == nil && t.DeadlineMinutes == nil && !roadWork[t.TripID] {
			continue
		}
		c, err := EffectiveTrip(st, t.TripID)
		if err != nil {
			return fail(err)
		}
		if c == nil || c.Trip.DeadlineMinutes == nil {
			continue
		}
		r := DeadlineRun{
			TripID: t.TripID, Origin: t.OriginSite, RoadWork: c.RoadWork,
			Vehicle: instants.Vehicle[t.TripID], Allowance: *c.Trip.DeadlineMinutes,
			Dispatched: instants.Dispatch[t.TripID],
		}
		if c.Trip.DestinationSite != nil {
			r.Destination = *c.Trip.DestinationSite
		}
		if c.Trip.DestinationUncontrolled != nil {
			r.Uncontrolled = *c.Trip.DestinationUncontrolled
		}
		allowanceMS := r.Allowance * 60_000
		if dep, lead, found, err := DispatchDeparture(st, store.Key{SiteID: t.OriginSite, NodeEpoch: t.NodeEpoch, Seq: t.Seq}); err != nil {
			return fail(err)
		} else if found {
			r.TimedFromMS = dep.UnixMilli()
			r.DueMS, r.DueOK, r.RecordedMS = r.TimedFromMS+allowanceMS, true, dep.Add(lead).UnixMilli()
		} else if r.Dispatched.OK {
			r.DueMS, r.DueOK = r.Dispatched.MS+allowanceMS, true
		}
		s := stateOf[t.TripID]
		switch {
		case (s.State == TripStateArrived || s.State == TripStateArrivedAfterCancel) && s.ArrivalID != nil:
			r.End = instants.Arrival[*s.ArrivalID]
			switch {
			case r.RoadWork:
				r.Outcome = RunBack
			case returned[t.TripID]:
				r.Outcome = RunTurnedBack
			default:
				r.Outcome = RunArrived
			}
		case s.State == TripStateCancelled:
			r.Outcome, r.End = RunCancelled, cancels[t.TripID]
		default:
			r.Outcome = RunOpen
		}
		out = append(out, r)
	}
	return out, nil
}

// sortBySourceKeyTrips orders trips by their dispatch's log key.
func sortBySourceKeyTrips(ts []Trip) {
	sort.SliceStable(ts, func(i, j int) bool {
		a, b := ts[i], ts[j]
		if a.OriginSite != b.OriginSite {
			return a.OriginSite < b.OriginSite
		}
		if a.NodeEpoch != b.NodeEpoch {
			return a.NodeEpoch < b.NodeEpoch
		}
		return a.Seq < b.Seq
	})
}

// cancelInstants is each cancelled trip's first trip_cancelled instant, in
// one replay of the log (the LoadTripInstants shape: resolutions made up
// front, nothing read inside the EachEvent callback).
func cancelInstants(st *store.Store) (map[string]RecordedInstant, error) {
	corrected, err := correctedInstants(st)
	if err != nil {
		return nil, err
	}
	// FIX17: the log follower's walk (log_follow.go), resumed rather than
	// restarted — its trip_cancelled payloads, in walk order.
	snap, err := followLog(st, false)
	if err != nil {
		return nil, err
	}
	out := map[string]RecordedInstant{}
	for _, e := range snap.trips {
		p := e.cancelled
		if p == nil || p.Type != "trip_cancelled" || p.TripID == "" {
			continue
		}
		if _, seen := out[p.TripID]; seen {
			continue
		}
		ms, label, ok := reportInstant(e.key, e.basis, corrected)
		out[p.TripID] = RecordedInstant{SiteID: e.site, MS: ms, OK: ok, Label: label}
	}
	return out, nil
}
