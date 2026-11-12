// analytics.go — the dispatch-analytics page's figures (GET /analytics,
// internal/web/analytics.go): persons on site, visits, time on site and
// travel counts per site, derived AT REQUEST TIME from the event log.
//
// BINDING RULES (design/analytics-v1.md header, the surviving four):
//   - §2.2 a visit carries no time — every instant here is read off the
//     event that authored it (the same reportInstant the visit report uses),
//     never off a projection column.
//   - §3.0 a season fits in page cache — so a linear replay per request is
//     the whole mechanism.
//   - §7.3 compute on request, no cache, no side table, no job.
//     ⟦FIX17 (perf, 2026-10-06): the request-independent half — the visit
//     report and the one log replay — is now reused between WRITES
//     (store.Memo, keyed by the store's head: any append, any local write,
//     any other connection's commit rebuilds it), and every window is still
//     counted per request. The figures are those a fresh computation would
//     give; what is gone is the season-length replay on every gate and status
//     page (the shift panel), 1-2 s each at season scale. Still no side
//     table and no job: nothing is stored, and nothing runs off-request.⟧
//   - §8.4 refuse figures from a stalled peer — that decision needs the
//     replication engine's freshness verdict, which the domain does not
//     have; the web layer joins it to these figures and does the refusing.
//     This file supplies what a reader needs beside each site's numbers:
//     its watermark and the instant of the latest event it authored.
//
// It reuses, rather than re-derives: VisitReport (the one linear visit
// replay — open set, arrival/close instants, recorded-departure duration),
// store.EachEvent (the one audit read over the log) and the
// projection-backed Trips/TripStates for "in transit now". No SQL here, no
// event minted, no clock ordering: "today" is a range test on an already
// resolved instant (a local value, not a comparator on Envelope.OccurredAt).
package domain

import (
	"fmt"
	"sort"
	"time"

	"sitecontrol/internal/store"
)

// MinutesAvg is a mean with its sample size, so a reader can never see an
// average without the n behind it (n=0 renders as "no departures yet").
//
// ⟦L18 AVG-1, fix round 2⟧ SUM, THEN DIVIDE ONCE. Sum holds the exact
// integer minutes and Mean is Sum/N, computed afresh on every add. ⟦was: a
// running mean, m = (m·n + x)/(n+1)⟧ — whose float drift turned ice-road's
// 561/34 = 16.5 into 16.499999999999996, so the half-up rounding the page
// promises showed "16 min" where the record says 17. RoundedMinutes rounds
// the exact rational, so no .5 tie can ever go the wrong way.
type MinutesAvg struct {
	N    int
	Sum  int64
	Mean float64
}

func (m *MinutesAvg) add(min int64) {
	m.N++
	m.Sum += min
	m.Mean = float64(m.Sum) / float64(m.N)
}

// RoundedMinutes is the mean rounded half-up, floor(Sum/N + ½), in integer
// arithmetic. 0 when N is 0 (the page says "no recorded departures" then).
func (m MinutesAvg) RoundedMinutes() int64 {
	if m.N == 0 {
		return 0
	}
	num, den := 2*m.Sum+int64(m.N), 2*int64(m.N)
	q := num / den
	if num%den != 0 && num < 0 {
		q-- // Go truncates toward zero; floor for a negative numerator
	}
	return q
}

// SiteFigures is one site's row as THIS store knows it. For a peer's site
// that is only what has replicated here — see AnalyticsFigures' caveat.
type SiteFigures struct {
	SiteID string

	// OnSiteNow is the count of distinct (merge-resolved) persons whose visit
	// at this site has no closer in this store's copy of the log.
	OnSiteNow int

	VisitsToday  int
	VisitsSeason int

	// SignOutsToday (FIX16 G): visits whose closer is a RECORDED DEPARTURE
	// (checkout or dispatch) at an instant inside the window. A visit closed
	// by the person's own re-entry is not a sign-out (DepartureRecorded).
	SignOutsToday int

	// AvgOnSiteToday / AvgOnSiteSeason average OnSiteMinutes over visits
	// whose closer is a RECORDED DEPARTURE (checkout or dispatch) — a
	// missed sign-out's span is an upper bound, never a measurement, so it
	// is left out (visit_report.go). Today = closed today.
	//
	// James, 2026-10-04 (R5, FINDINGS-R4 D14): the gate's "Remove — no
	// departure" authors a check-out, and it STAYS in the average — at
	// NPF/NDB/STP it is the only way a visit ends, so leaving it out would
	// leave those sites with no average at all. The page's footnote says the
	// average includes it (web/templates/analytics.html).
	AvgOnSiteToday  MinutesAvg
	AvgOnSiteSeason MinutesAvg

	// TravelOut: trips this site dispatched WITH a destination (a gate
	// check-in's destination-less half is not travel). TravelIn: arrivals
	// recorded at this site other than the arrival half of a local check-in.
	TravelOutToday, TravelOutSeason int
	TravelInToday, TravelInSeason   int

	// RoadWorkOutToday / RoadWorkOutSeason (James, 2026-10-03): road-work
	// runs this site sent out — equipment working on the road, no
	// destination (road_work.go). Counted apart from TravelOut: a grader
	// going out to blade the road is not a trip to a destination, and the
	// travel figures must not inflate on it.
	RoadWorkOutToday, RoadWorkOutSeason int

	// FIX16 stream E (the dispatch reports): per site, over the window.
	// TurnedBack…: trips this site dispatched whose binding arrival was back
	// at this site (the overview's TripsTurnedBack, split by origin; the
	// window is the arrival's). CheckOutsToday / LeftOnTripToday: visits
	// closed in the window by a recorded departure — a sign-out at the gate,
	// or leaving aboard a dispatched trip or road-work run. ReturnsClosedToday:
	// visits closed in the window by the person's own return with no
	// departure recorded (re_checkin / next_arrival — a missed sign-out at a
	// text-message site, normal at a camp; visit_report.go).
	TurnedBackToday, TurnedBackSeason int
	CheckOutsToday, LeftOnTripToday   int
	ReturnsClosedToday                int

	// Freshness of what this store holds FROM this site: the applied
	// watermark of its newest epoch, and when that post's own clock said it
	// recorded the NEWEST event this store holds from it — the last in
	// (node_epoch, seq) order, its authored_at (labelled "by that post's
	// clock" on the page; LastEventOK false when the site has no event).
	// FIX3 F9 (FINDINGS-R5 L09-05, L03-8a): ⟦was: the MAXIMUM resolved
	// instant over all its events — after a backward clock step that hid the
	// newest event behind an earlier, higher stamp, and an event stamped
	// while the clock ran fast stayed "latest" (and "AHEAD") after the clock
	// was put right and newer events arrived.⟧
	//
	// LastEventBackMS: how far that newest stamp is BEHIND the highest stamp
	// on an event the site recorded before it (0 when it is not behind by a
	// minute or more) — the one witness this store holds of a clock that was
	// set back, since stamps on one site's own log otherwise only rise.
	AppliedSeq      int64
	EventCount      int64
	LastEventMS     int64
	LastEventOK     bool
	LastEventBackMS int64

	maxWallMS int64 // highest authored_at so far in the replay (F9 witness)
}

// AnalyticsFigures is everything the page renders, minus freshness verdicts.
type AnalyticsFigures struct {
	DayStartMS, DayEndMS int64 // the local day "today" means

	Sites []SiteFigures // sorted by site id; every stream site in the store

	TripsDispatched, TripsDispatchedToday int // with a destination, all sites
	TripsArrived, TripsArrivedToday       int // distinct dispatched trips bound to an arrival (trip_state: explicit, correlated or resolved) — turned-back trips excluded
	// TripsTurnedBack / TripsTurnedBackToday: dispatched trips whose binding
	// arrival was recorded back at their own origin ("Arrived back here",
	// TripsReturnedToOrigin) — they never reached the destination, so they
	// are shown apart and not counted in TripsArrived. James, 2026-10-04
	// (R8, FINDINGS-R4 D7). dispatched = arrived + turned back + in transit.
	TripsTurnedBack, TripsTurnedBackToday int
	TripsInTransit                        int // open trips to a controlled destination (trip_state)

	// RoadWorkDispatched / RoadWorkDispatchedToday / RoadWorkOut (James,
	// 2026-10-03): road-work runs sent out, all sites; and how many are OUT
	// NOW (open, trip_state). Apart from the trip figures above for the
	// reason SiteFigures.RoadWorkOutSeason gives.
	RoadWorkDispatched, RoadWorkDispatchedToday int
	RoadWorkOut                                 int

	// TimesUnresolved: instants the resolver failed closed on. They are in
	// the season counts and OUT of every "today" count, and the page says so.
	TimesUnresolved int

	openBySite map[string]map[string]bool
}

// DistinctOnSite is the number of distinct persons open at ANY of the given
// sites — a person open at two sites (a peer's not-yet-replicated checkout)
// counts once. The web layer passes only the sites it is willing to call
// current (§8.4).
func (a AnalyticsFigures) DistinctOnSite(sites map[string]bool) int {
	seen := map[string]bool{}
	for site, persons := range a.openBySite {
		if !sites[site] {
			continue
		}
		for p := range persons {
			seen[p] = true
		}
	}
	return len(seen)
}

type analyticsTrip struct {
	origin   string
	hasDest  bool
	roadWork bool // James, 2026-10-03: the road-work kind — counted apart from travel
	todayDis bool
}

type analyticsArrival struct {
	site      string
	arrivalID string
	tripRef   *string // explicit reference, if any — only consulted to keep a gate check-in's arrival half out of TRAVEL IN
	today     bool
}

// BuildAnalytics derives the figures from st's whole log for the local day
// containing now in loc. Read-only.
func BuildAnalytics(st *store.Store, now time.Time, loc *time.Location) (AnalyticsFigures, error) {
	if loc == nil {
		loc = time.Local
	}
	local := now.In(loc)
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	return AnalyticsWindow(st, dayStart.UnixMilli(), dayStart.AddDate(0, 0, 1).UnixMilli())
}

// AnalyticsWindow is BuildAnalytics with "today" replaced by the window
// [startMS, endMS) — every …Today figure counts that window instead. It is
// the ONE windowed analytics computation (FIX16 integration unified stream E's
// AnalyticsWindow and stream G's buildAnalyticsWindow into it): the dispatch
// reports (web/dispatch_reports.go) pass a chosen day or week, the shift
// totals (shift.go) pass the current shift, and BuildAnalytics passes the
// local day — so a report, the shift panel and the analytics tiles for the
// same window can never count differently. Read-only.
func AnalyticsWindow(st *store.Store, startMS, endMS int64) (AnalyticsFigures, error) {
	fail := func(err error) (AnalyticsFigures, error) {
		return AnalyticsFigures{}, fmt.Errorf("domain: analytics: %w", err)
	}
	b, err := analyticsBasisFor(st)
	if err != nil {
		return fail(err)
	}
	fig := AnalyticsFigures{
		DayStartMS: startMS,
		DayEndMS:   endMS,
		openBySite: map[string]map[string]bool{},
	}
	isToday := func(ms int64) bool { return ms >= fig.DayStartMS && ms < fig.DayEndMS }

	recs := b.recs
	bySite := map[string]*SiteFigures{}
	site := func(id string) *SiteFigures {
		if s := bySite[id]; s != nil {
			return s
		}
		s := &SiteFigures{SiteID: id}
		bySite[id] = s
		fig.openBySite[id] = map[string]bool{}
		return s
	}

	for _, m := range b.streams {
		s := site(m.SiteID)
		s.EventCount += m.AppliedSeq
		s.AppliedSeq = m.AppliedSeq // streams are epoch-ascending: the newest epoch wins
	}

	for _, r := range recs {
		s := site(r.SiteID)
		s.VisitsSeason++
		if r.ArrivalLabel == timeUnresolvable {
			fig.TimesUnresolved++
		} else if isToday(r.ArrivedAtMS) {
			s.VisitsToday++
		}
		if r.ClosedBy == VisitStillOpen {
			if r.PersonID != "" {
				fig.openBySite[r.SiteID][r.PersonID] = true
			}
			continue
		}
		if r.CloseLabel != timeUnresolvable && r.ClosedAtMS != 0 && isToday(r.ClosedAtMS) {
			switch {
			case r.ClosedBy == ClosedByCheckout:
				s.CheckOutsToday++
			case r.ClosedBy == ClosedByDispatch:
				s.LeftOnTripToday++
			default: // re_checkin / next_arrival: closed by the person's own return
				s.ReturnsClosedToday++
			}
			// FIX16 G: a sign-out is a RECORDED departure — the two cases
			// above, never a close by the person's own re-entry.
			if r.ClosedBy.DepartureRecorded() {
				s.SignOutsToday++
			}
		}
		if r.OnSiteMinutes != nil {
			s.AvgOnSiteSeason.add(*r.OnSiteMinutes)
			if isToday(r.ClosedAtMS) {
				s.AvgOnSiteToday.add(*r.OnSiteMinutes)
			}
		}
	}

	// The replay's facts (analyticsBasisFor walked the log once, in log
	// order), now counted against this window. The per-site freshness is
	// window-free and copied as the walk left it.
	for id, f := range b.fresh {
		s := site(id)
		s.LastEventMS, s.LastEventOK, s.LastEventBackMS, s.maxWallMS = f.lastEventMS, true, f.lastEventBackMS, f.maxWallMS
	}
	trips := map[string]analyticsTrip{}
	for _, d := range b.dispatches {
		s := site(d.site)
		t := analyticsTrip{origin: d.site, hasDest: d.hasDest, roadWork: d.roadWork}
		if d.hasDest {
			s.TravelOutSeason++
			fig.TripsDispatched++
			if !d.ok {
				fig.TimesUnresolved++
			} else if isToday(d.ms) {
				t.todayDis = true
				s.TravelOutToday++
				fig.TripsDispatchedToday++
			}
		} else if d.roadWork {
			// James, 2026-10-03: its own figure, never travel.
			s.RoadWorkOutSeason++
			fig.RoadWorkDispatched++
			if !d.ok {
				fig.TimesUnresolved++
			} else if isToday(d.ms) {
				s.RoadWorkOutToday++
				fig.RoadWorkDispatchedToday++
			}
		}
		trips[d.tripID] = t
	}
	var arrivals []analyticsArrival
	for _, a := range b.arrivals {
		if !a.ok {
			fig.TimesUnresolved++
		}
		arrivals = append(arrivals, analyticsArrival{site: a.site, arrivalID: a.arrivalID, tripRef: a.tripRef, today: a.ok && isToday(a.ms)})
	}

	// Per-site travel-in is every arrival the site recorded, from the log —
	// except the arrival half of a local gate check-in (an explicit
	// reference to a trip with no destination), which is not travel.
	// Resolved against the trips only once every stream is walked: a site's
	// arrival can reference a trip dispatched at a site that sorts after it.
	arrivalToday := map[string]bool{}
	for _, a := range arrivals {
		arrivalToday[a.arrivalID] = a.today
		if a.tripRef != nil {
			if t, known := trips[*a.tripRef]; known && !t.hasDest {
				continue
			}
		}
		s := site(a.site)
		s.TravelInSeason++
		if a.today {
			s.TravelInToday++
		}
	}

	// ⟦RES-1a, 2026-09-30⟧ TRIPS ARRIVED is read from trip_state — the SAME
	// source IN TRANSIT reads below — as every dispatched trip bound to an
	// arrival, whichever way it was bound: an explicit reference (Pass 1),
	// Pass 2 correlation of an unsolicited arrival, or an officer's
	// resolution of a parked one (arrival_resolve.go). ⟦was: the log's
	// trip_arrival payloads with a non-nil trip_ref⟧ — which is only the
	// explicit kind, so a trip closed by a correlated or resolved arrival
	// left IN TRANSIT and never entered ARRIVED (lanes-r3/L11.md RES-1a:
	// dispatched 18 = 12 arrived + 6 in transit became 12 + 5). Reading the
	// binding from trip_state also makes an unref un-count the trip the
	// moment the recompute re-opens it, exactly as IN TRANSIT re-counts it.
	// "Today" is the BINDING arrival's own instant, recorded during the scan
	// above; a local gate check-in's degenerate trip has no destination and
	// is skipped as before.
	//
	// James, 2026-10-04 (R8, FINDINGS-R4 D7 / L18 TB-1): exclude turned-back
	// trucks from "trips arrived" and show "N turned back". A turned-back
	// trip is stored as `arrived` (its binding arrival is at its own
	// origin); RETURNED is derived at read time by TripsReturnedToOrigin,
	// which this reads rather than re-deriving. The origin's TRAVEL IN above
	// is deliberately unchanged by the ruling.
	allStates, returned := b.allStates, b.returned
	arrivedTrip := map[string]bool{}
	arrivedTripToday := map[string]bool{}
	turnedBack := map[string]bool{}
	turnedBackToday := map[string]bool{}
	for _, ts := range allStates {
		if ts.State != TripStateArrived && ts.State != TripStateArrivedAfterCancel {
			continue
		}
		if ts.ArrivalID == nil {
			continue
		}
		t, known := trips[ts.TripID]
		if !known || !t.hasDest {
			continue // the arrival half of a local gate check-in
		}
		into, intoToday := arrivedTrip, arrivedTripToday
		if returned[ts.TripID] {
			into, intoToday = turnedBack, turnedBackToday
			// FIX16 E: per origin, for the daily site report.
			o := site(t.origin)
			o.TurnedBackSeason++
			if arrivalToday[*ts.ArrivalID] {
				o.TurnedBackToday++
			}
		}
		into[ts.TripID] = true
		if arrivalToday[*ts.ArrivalID] {
			intoToday[ts.TripID] = true
		}
	}
	fig.TripsArrived = len(arrivedTrip)
	fig.TripsArrivedToday = len(arrivedTripToday)
	fig.TripsTurnedBack = len(turnedBack)
	fig.TripsTurnedBackToday = len(turnedBackToday)

	for _, ts := range b.openStates {
		if b.controlled[ts.TripID] {
			fig.TripsInTransit++
		}
		// James, 2026-10-03: open road-work runs are OUT NOW — read off the
		// same open set, by the kind the log said each trip was.
		if t, known := trips[ts.TripID]; known && t.roadWork {
			fig.RoadWorkOut++
		}
	}

	for id, s := range bySite {
		s.OnSiteNow = len(fig.openBySite[id])
		fig.Sites = append(fig.Sites, *s)
	}
	sort.Slice(fig.Sites, func(i, j int) bool { return fig.Sites[i].SiteID < fig.Sites[j].SiteID })
	return fig, nil
}

// analyticsBasis is everything AnalyticsWindow reads from the store, before
// any window is applied: the visit report, the stream census, the one replay
// of the log reduced to the facts the figures count (in log order), and the
// trip-state reads. FIX17 (perf, 2026-10-06): it is a pure function of the
// stored rows, so it is built once per store head (store.Memo) and every
// window — the shift panel on each gate and status page, the dispatch
// reports, /analytics — is counted from the same one. Shared, read-only.
type analyticsBasis struct {
	recs       []VisitRecord
	streams    []store.Stream
	fresh      map[string]analyticsFreshness
	dispatches []analyticsDispatchFact
	arrivals   []analyticsArrivalFact
	allStates  []TripState
	returned   map[string]bool
	openStates []TripState
	controlled map[string]bool // trips with a controlled destination
}

// analyticsFreshness is a site's newest-event facts as the replay left them
// (SiteFigures' LastEvent… fields; see FIX3 F9 there).
type analyticsFreshness struct {
	lastEventMS, lastEventBackMS, maxWallMS int64
}

// analyticsDispatchFact / analyticsArrivalFact: one trip_dispatched /
// trip_arrival as the replay saw it, its instant (ms, ok) resolved at one
// head (buildAnalyticsBasis).
type analyticsDispatchFact struct {
	site, tripID      string
	hasDest, roadWork bool
	ms                int64
	ok                bool
}

type analyticsArrivalFact struct {
	site, arrivalID string
	tripRef         *string
	ms              int64
	ok              bool
}

func analyticsBasisFor(st *store.Store) (*analyticsBasis, error) {
	v, err := st.Memo("domain.analyticsBasis", func() (any, error) { return buildAnalyticsBasis(st) })
	if err != nil {
		return nil, err
	}
	return v.(*analyticsBasis), nil
}

func buildAnalyticsBasis(st *store.Store) (*analyticsBasis, error) {
	b := &analyticsBasis{}
	var err error
	if b.recs, _, err = visitReportShared(st, nil); err != nil { // read-only (analyticsWindow ranges over copies)
		return nil, err
	}
	if b.streams, err = st.Watermarks(); err != nil {
		return nil, err
	}

	// The one replay for the travel counts and the per-site latest instant
	// is the log follower's (log_follow.go): the facts it kept are this
	// walk's former per-event body, and their instants are resolved here,
	// against this head's corrections — a correction can move an old
	// event's instant, so that is never retained.
	corrected, err := correctedInstants(st)
	if err != nil {
		return nil, err
	}
	snap, err := followLog(st, false)
	if err != nil {
		return nil, err
	}
	b.fresh = snap.fresh
	for _, e := range snap.trips {
		ms, _, ok := reportInstant(e.key, e.basis, corrected)
		switch {
		case e.dispatched != nil:
			p := e.dispatched
			b.dispatches = append(b.dispatches, analyticsDispatchFact{
				site: e.site, tripID: p.TripID,
				hasDest:  p.DestinationSite != nil || p.DestinationUncontrolled != nil,
				roadWork: p.RoadWork, ms: ms, ok: ok,
			})
		case e.arrival != nil:
			p := e.arrival
			b.arrivals = append(b.arrivals, analyticsArrivalFact{site: e.site, arrivalID: p.ArrivalID, tripRef: p.TripRef, ms: ms, ok: ok})
		}
	}

	if b.allStates, err = TripStates(st); err != nil {
		return nil, err
	}
	if b.returned, err = TripsReturnedToOrigin(st); err != nil {
		return nil, err
	}
	if b.openStates, err = openTripStates(st); err != nil {
		return nil, err
	}
	allTrips, err := Trips(st)
	if err != nil {
		return nil, err
	}
	b.controlled = map[string]bool{}
	for _, t := range allTrips {
		if t.DestinationSite != nil {
			b.controlled[t.TripID] = true
		}
	}
	return b, nil
}
