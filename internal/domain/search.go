// search.go — the recorded instants behind the records search (FIX9, James
// 2026-10-05: "add a searching feature as well to all sites and dispatch it
// should be able to search by last name, first name, company, date, time,
// phone number and truck number"). web/search.go is the page; this file is
// the one read it needs that the web layer cannot make itself.
//
// WHAT IT IS. One linear replay of the log (store.EachEvent, the same shape
// BuildAnalytics' travel pass uses) collecting, for every trip_dispatched
// and every trip_arrival this post holds, the event's EFFECTIVE instant and
// its time label — reportInstant, the one place a report instant is
// produced, over correctedInstants resolved up front (D2 / AN-PANIC-1: the
// EachEvent callback holds the store's one connection and must not read).
// So a corrected dispatch or arrival time is what the search shows and
// filters on: the record as it stands.
//
// WHAT IT IS NOT. No event type, no projection column, no side table, no
// cache: compute on request, like the visit report and analytics. ⟦FIX17
// (perf, 2026-10-06): the replay's result is reused between WRITES
// (store.Memo, keyed by the store's head — any write rebuilds it).⟧ Nothing
// here orders anything (D-90/D-92) — an instant is display and a filter, the
// record's order is the stream's.
package domain

import (
	"fmt"
	"maps"

	"sitecontrol/internal/store"
)

// RecordedInstant is one event's effective occurred instant as the report
// read classifies it. OK=false: the correction chain failed closed and the
// instant is not shown (never guessed). Label is the visit report's time
// label ("observed", "claimed", "claimed-corrected", …).
type RecordedInstant struct {
	SiteID string // the post whose clock stamped it
	MS     int64
	OK     bool
	Label  string
}

// ByHand reports whether the instant is an officer's typed claim rather
// than the post's clock reading: entered at the time ("claimed") or a time
// correction that stands ("claimed-corrected").
func (r RecordedInstant) ByHand() bool {
	return r.Label == timeClaimed || r.Label == timeClaimedCorrected
}

// Corrected reports whether a time correction is what stands. A
// VisitRecord end classifies the same way: wrap its ArrivalLabel /
// CloseLabel in a RecordedInstant.
func (r RecordedInstant) Corrected() bool { return r.Label == timeClaimedCorrected }

// TripInstants is what the search needs from the log's trip and arrival
// events, gathered in one replay so the search reads no whole projection
// table it does not need:
//
//   - Dispatch by trip id and Arrival by arrival id: the effective instant.
//   - Run: the trips that are RUNS — dispatched to a destination (controlled
//     or a pad) or out working on the road — as opposed to the
//     destination-less dispatch half of a gate check-in, which opens a visit
//     and is not a journey (visit_report.go's re_checkin rule).
//   - Vehicle by trip id: the truck as recorded AT DISPATCH (the same frozen
//     snapshot the trip row carries; a run's corrected or live truck is the
//     composed trip's, EffectiveTrip).
//   - Visit by visit id: what the arrival that opened it recorded — the
//     company and trade at check-in, and the truck as entered at that gate.
type TripInstants struct {
	Dispatch map[string]RecordedInstant
	Arrival  map[string]RecordedInstant
	Run      map[string]bool
	Vehicle  map[string]string
	Visit    map[string]VisitAsRecorded
}

// VisitAsRecorded is one visit's facts from its arrival payload.
type VisitAsRecorded struct {
	Company, Trade string
	Vehicle        string // "" when the arrival named no truck
}

// LoadTripInstants replays the log once. See the file comment.
//
// FIX17 (perf): reused between writes (store.Memo) — search, the history
// pages and the dispatch reports each replayed the whole log for it. The
// caller gets its own maps.
func LoadTripInstants(st *store.Store) (TripInstants, error) {
	v, err := st.Memo("domain.LoadTripInstants", func() (any, error) { return loadTripInstants(st) })
	if err != nil {
		return TripInstants{}, err
	}
	shared := v.(TripInstants)
	return TripInstants{
		Dispatch: maps.Clone(shared.Dispatch), Arrival: maps.Clone(shared.Arrival),
		Run: maps.Clone(shared.Run), Vehicle: maps.Clone(shared.Vehicle), Visit: maps.Clone(shared.Visit),
	}, nil
}

func loadTripInstants(st *store.Store) (TripInstants, error) {
	fail := func(err error) (TripInstants, error) {
		return TripInstants{}, fmt.Errorf("domain: trip instants: %w", err)
	}
	out := TripInstants{Dispatch: map[string]RecordedInstant{}, Arrival: map[string]RecordedInstant{},
		Run: map[string]bool{}, Vehicle: map[string]string{}, Visit: map[string]VisitAsRecorded{}}
	corrected, err := correctedInstants(st)
	if err != nil {
		return fail(err)
	}
	// FIX17: the replay is the log follower's (log_follow.go) — the same
	// trip_dispatched / trip_arrival payloads in the same walk order, the
	// walk resumed rather than restarted; their instants are resolved here,
	// against this head's corrections.
	snap, err := followLog(st, false)
	if err != nil {
		return fail(err)
	}
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	for _, e := range snap.trips {
		at := func() RecordedInstant {
			ms, label, ok := reportInstant(e.key, e.basis, corrected)
			return RecordedInstant{SiteID: e.site, MS: ms, OK: ok, Label: label}
		}
		switch {
		case e.dispatched != nil:
			p := e.dispatched
			if p.TripID == "" {
				continue
			}
			out.Dispatch[p.TripID] = at()
			if p.DestinationSite != nil || p.DestinationUncontrolled != nil || p.RoadWork {
				out.Run[p.TripID] = true
			}
			if p.VehicleUnidentified {
				out.Vehicle[p.TripID] = VehicleUnidentifiedText
			} else {
				out.Vehicle[p.TripID] = str(p.VehicleEntered)
			}
		case e.arrival != nil:
			p := e.arrival
			if p.ArrivalID == "" {
				continue
			}
			out.Arrival[p.ArrivalID] = at()
			vehicle := str(p.VehicleEntered)
			if p.VehicleUnidentified {
				vehicle = VehicleUnidentifiedText
			}
			for _, v := range p.Visits {
				out.Visit[v.VisitID] = VisitAsRecorded{Company: str(v.CompanyName), Trade: str(v.TradeName), Vehicle: vehicle}
			}
		}
	}
	return out, nil
}

// ArrivalInstant / CloseInstant are a VisitRecord's two ends in the same
// shape. CloseInstant is the zero value (OK=false, no label) while the visit
// is open.
func (r VisitRecord) ArrivalInstant() RecordedInstant {
	return RecordedInstant{SiteID: r.SiteID, MS: r.ArrivedAtMS, OK: r.ArrivalLabel != timeUnresolvable, Label: r.ArrivalLabel}
}

func (r VisitRecord) CloseInstant() RecordedInstant {
	if r.ClosedBy == VisitStillOpen {
		return RecordedInstant{SiteID: r.SiteID}
	}
	return RecordedInstant{SiteID: r.SiteID, MS: r.ClosedAtMS, OK: r.CloseLabel != timeUnresolvable, Label: r.CloseLabel}
}
