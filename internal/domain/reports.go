// reports.go — the derivations behind the dispatch reports (FIX16 stream E,
// James's approved list; web/dispatch_reports.go renders them): a site's headcount
// over time, contractor hours by company, and the runs that went past their
// allowance.
//
// NOTHING HERE RE-COUNTS WHAT ANALYTICS ALREADY COUNTS. Visits, sign-ins,
// sign-outs, travel and turned-back trips for a day or a week come from
// AnalyticsWindow (analytics.go) — the same computation as the /analytics
// tiles, with "today" swapped for the chosen window. What is new here is
// only what the tiles never needed:
//
//   - HEADCOUNT OVER TIME is VisitReport's visits read as presence spans:
//     a person is on site from the visit's arrival instant until the
//     instant its record closed, or still, if it is open — exactly the set
//     the muster and the "persons on site now" tile count (an unclosed
//     visit stays on the muster until something closes it, so a missed
//     sign-out stands on the headcount until the person's return closes
//     it, as it stood on the muster). Distinct persons, merge-resolved, as
//     the tile counts them. A visit with an end the resolver failed closed
//     on is left out and counted, never guessed.
//   - CONTRACTOR HOURS sum VisitReport's OnSiteMinutes — set only for a
//     visit closed by a recorded departure, the same measurement the
//     average-time tiles use. A visit closed by the person's own return, or
//     still open, has no measured duration; those are returned apart,
//     flagged, and never added in.
//   - RUNS PAST THEIR ALLOWANCE are the overdue history's runs
//     (deadline_runs.go DeadlineRuns) judged against now: due = the
//     dispatch's EFFECTIVE departure (James 2026-10-06: from the typed time —
//     EffectiveDeparture, the basis every deadline judge uses) plus the run's
//     effective allowance. A run is late when its binding arrival was
//     recorded after that, or it is still out after it. Cancelled runs are
//     not judged. The arrival time is the receiving post's clock, so a late
//     verdict compares two posts' clocks — the page says so. ⟦was: due from
//     the origin's RECORDED dispatch time (authored_at), FINDINGS-R4 D13,
//     in a scan of its own; FIX16 integration made it a reading of
//     DeadlineRuns so the report and the overdue history cannot disagree⟧
//
// Read-only: nothing is authored, nothing is cached, no table is added.
package domain

import (
	"fmt"
	"sort"

	"sitecontrol/internal/store"
)

// PresenceSpan is one visit read as presence: Person on SiteID from FromMS,
// until ToMS when Open is false.
type PresenceSpan struct {
	Person string // canonical person id; "visit:<id>" when the visit names nobody
	SiteID string
	FromMS int64
	ToMS   int64
	Open   bool
}

// PresenceSpans turns visit records into spans at site ("" = every site).
// unknown is how many visits at that site were left out because an end
// could not be resolved.
func PresenceSpans(recs []VisitRecord, site string) (spans []PresenceSpan, unknown int) {
	for _, r := range recs {
		if site != "" && r.SiteID != site {
			continue
		}
		if r.ArrivalLabel == timeUnresolvable || (r.ClosedBy != VisitStillOpen && r.CloseLabel == timeUnresolvable) {
			unknown++
			continue
		}
		who := r.PersonID
		if who == "" {
			who = "visit:" + r.VisitID
		}
		sp := PresenceSpan{Person: who, SiteID: r.SiteID, FromMS: r.ArrivedAtMS, Open: r.ClosedBy == VisitStillOpen}
		if !sp.Open {
			sp.ToMS = r.ClosedAtMS
		}
		spans = append(spans, sp)
	}
	return spans, unknown
}

type headStep struct {
	ms    int64
	count int
}

// Headcount is the step function of distinct persons present, built once
// from a set of spans and then read at any instant.
type Headcount struct {
	steps []headStep
}

// NewHeadcount sweeps the spans. Everything that happens at one instant is
// applied before the count is taken, so a sign-out and a sign-in at the
// same minute never show a phantom dip or spike.
func NewHeadcount(spans []PresenceSpan) Headcount {
	type ev struct {
		ms     int64
		person string
		delta  int
	}
	evs := make([]ev, 0, 2*len(spans))
	for _, sp := range spans {
		if !sp.Open && sp.ToMS <= sp.FromMS {
			continue // closed the instant it opened (or by a clock set back): never present
		}
		evs = append(evs, ev{sp.FromMS, sp.Person, +1})
		if !sp.Open {
			evs = append(evs, ev{sp.ToMS, sp.Person, -1})
		}
	}
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].ms < evs[j].ms })
	n := map[string]int{}
	active := 0
	var h Headcount
	for i := 0; i < len(evs); {
		ms := evs[i].ms
		for ; i < len(evs) && evs[i].ms == ms; i++ {
			before := n[evs[i].person]
			n[evs[i].person] = before + evs[i].delta
			switch {
			case before <= 0 && n[evs[i].person] > 0:
				active++
			case before > 0 && n[evs[i].person] <= 0:
				active--
			}
		}
		h.steps = append(h.steps, headStep{ms, active})
	}
	return h
}

// At is the headcount at instant ms.
func (h Headcount) At(ms int64) int {
	i := sort.Search(len(h.steps), func(i int) bool { return h.steps[i].ms > ms })
	if i == 0 {
		return 0
	}
	return h.steps[i-1].count
}

// HeadcountExtremes is the highest and lowest headcount over a window, each
// with the first instant it was reached.
type HeadcountExtremes struct {
	Peak, Low     int
	PeakMS, LowMS int64
}

// Extremes reads the window [fromMS, toMS).
func (h Headcount) Extremes(fromMS, toMS int64) HeadcountExtremes {
	c := h.At(fromMS)
	x := HeadcountExtremes{Peak: c, Low: c, PeakMS: fromMS, LowMS: fromMS}
	i := sort.Search(len(h.steps), func(i int) bool { return h.steps[i].ms > fromMS })
	for ; i < len(h.steps) && h.steps[i].ms < toMS; i++ {
		if c := h.steps[i].count; c > x.Peak {
			x.Peak, x.PeakMS = c, h.steps[i].ms
		} else if c < x.Low {
			x.Low, x.LowMS = c, h.steps[i].ms
		}
	}
	return x
}

// HeadcountHour is one hour of the series: the count at its start and the
// highest count at any instant inside it.
type HeadcountHour struct {
	StartMS int64
	AtStart int
	Peak    int
}

const hourMS = int64(60 * 60 * 1000)

// Hourly samples [fromMS, toMS) hour by hour from fromMS.
func (h Headcount) Hourly(fromMS, toMS int64) []HeadcountHour {
	var out []HeadcountHour
	for t := fromMS; t < toMS; t += hourMS {
		end := t + hourMS
		if end > toMS {
			end = toMS
		}
		out = append(out, HeadcountHour{StartMS: t, AtStart: h.At(t), Peak: h.Extremes(t, end).Peak})
	}
	return out
}

// HoursVisit is one visit on the contractor-hours report.
type HoursVisit struct {
	VisitID    string
	PersonID   string
	PersonName string
	Company    string // as recorded at check-in, else the roster's; "" when neither names one
	SiteID     string
	ArrivedMS  int64
	ClosedMS   int64 // 0 while open
	// Minutes: the measured time on site (VisitRecord.OnSiteMinutes) — set
	// only for a visit closed by a recorded departure. Nil on every flagged
	// visit.
	Minutes *int64
	// Flag says why a visit's time is NOT counted; "" on a counted one.
	Flag string
	// OpenMinutes: for a visit closed by the person's return, how long the
	// record stood open — an upper bound, shown, never summed.
	OpenMinutes *int64
}

// The reasons a visit's hours are flagged rather than counted.
const (
	HoursFlagMissedSignOut = "missed-sign-out" // closed by the person's own return; no departure recorded
	HoursFlagStillOpen     = "still-open"      // no sign-out yet
	HoursFlagNoTime        = "time-unresolved" // an end's time could not be resolved
)

// HoursPerson / HoursCompany are the counted totals.
type HoursPerson struct {
	PersonID, Name string
	Minutes        int64
	Visits         int
	Flagged        int
}

type HoursCompany struct {
	Company string
	Minutes int64
	Visits  int
	Flagged int
	People  []HoursPerson // by name
}

// HoursReport is contractor hours over [FromMS, ToMS).
type HoursReport struct {
	FromMS, ToMS int64
	Companies    []HoursCompany // by name; "" (no company recorded) last
	Flagged      []HoursVisit   // every visit not counted, by arrival
	TotalMinutes int64
	TotalVisits  int // counted visits
}

// ContractorHours gathers the visits that ARRIVED in [fromMS, toMS) at any
// site (site "" ) or one site. A visit is counted whole, by its own measured
// duration — the same figure the average-time tiles average — so a visit
// that runs past the end of the range is counted in the range it started
// in, once.
func ContractorHours(st *store.Store, recs []VisitRecord, site string, fromMS, toMS int64) (HoursReport, error) {
	out := HoursReport{FromMS: fromMS, ToMS: toMS}
	ti, err := LoadTripInstants(st)
	if err != nil {
		return out, fmt.Errorf("domain: contractor hours: %w", err)
	}
	persons, err := Persons(st)
	if err != nil {
		return out, fmt.Errorf("domain: contractor hours: %w", err)
	}
	companies, err := Companies(st)
	if err != nil {
		return out, fmt.Errorf("domain: contractor hours: %w", err)
	}
	coName := map[string]string{}
	for _, c := range companies {
		coName[c.CompanyID] = c.Name
	}
	rosterCo, rosterName := map[string]string{}, map[string]string{}
	for _, p := range persons {
		rosterName[p.PersonID] = p.Name
		if p.CompanyID != nil {
			rosterCo[p.PersonID] = coName[*p.CompanyID]
		}
	}
	byCo := map[string]*HoursCompany{}
	byPerson := map[string]map[string]*HoursPerson{}
	for _, r := range recs {
		if site != "" && r.SiteID != site {
			continue
		}
		if r.ArrivalLabel == timeUnresolvable || r.ArrivedAtMS < fromMS || r.ArrivedAtMS >= toMS {
			continue
		}
		v := HoursVisit{VisitID: r.VisitID, PersonID: r.PersonID, PersonName: rosterName[r.PersonID],
			Company: ti.Visit[r.VisitID].Company, SiteID: r.SiteID, ArrivedMS: r.ArrivedAtMS}
		if v.PersonName == "" {
			v.PersonName = r.PersonName
		}
		if v.Company == "" {
			v.Company = rosterCo[r.PersonID]
		}
		if r.ClosedBy != VisitStillOpen && r.CloseLabel != timeUnresolvable {
			v.ClosedMS = r.ClosedAtMS
		}
		switch {
		case r.OnSiteMinutes != nil:
			v.Minutes = r.OnSiteMinutes
		case r.ClosedBy == VisitStillOpen:
			v.Flag = HoursFlagStillOpen
		case r.ClosedBy.DepartureRecorded():
			v.Flag = HoursFlagNoTime
		default:
			v.Flag = HoursFlagMissedSignOut
			v.OpenMinutes = r.RecordOpenMinutes
		}
		co := byCo[v.Company]
		if co == nil {
			co = &HoursCompany{Company: v.Company}
			byCo[v.Company] = co
			byPerson[v.Company] = map[string]*HoursPerson{}
		}
		key := v.PersonID
		if key == "" {
			key = "visit:" + v.VisitID
		}
		p := byPerson[v.Company][key]
		if p == nil {
			p = &HoursPerson{PersonID: v.PersonID, Name: v.PersonName}
			byPerson[v.Company][key] = p
		}
		if v.Flag != "" {
			co.Flagged++
			p.Flagged++
			out.Flagged = append(out.Flagged, v)
			continue
		}
		co.Minutes += *v.Minutes
		co.Visits++
		p.Minutes += *v.Minutes
		p.Visits++
		out.TotalMinutes += *v.Minutes
		out.TotalVisits++
	}
	for name, co := range byCo {
		for _, p := range byPerson[name] {
			co.People = append(co.People, *p)
		}
		sort.Slice(co.People, func(i, j int) bool {
			if co.People[i].Name != co.People[j].Name {
				return co.People[i].Name < co.People[j].Name
			}
			return co.People[i].PersonID < co.People[j].PersonID
		})
		out.Companies = append(out.Companies, *co)
	}
	sort.Slice(out.Companies, func(i, j int) bool {
		a, b := out.Companies[i].Company, out.Companies[j].Company
		if (a == "") != (b == "") {
			return b == ""
		}
		return a < b
	})
	sort.SliceStable(out.Flagged, func(i, j int) bool { return out.Flagged[i].ArrivedMS < out.Flagged[j].ArrivedMS })
	return out, nil
}

// LateRun is one run that went past its allowance.
type LateRun struct {
	TripID          string
	Origin          string
	DestinationSite string // "" for road work or an uncontrolled destination
	Destination     string // the uncontrolled place's name, when that is where it went
	RoadWork        bool
	Vehicle         string
	AllowanceMin    int64
	DueMS           int64 // the effective (typed) departure + allowance
	// LeftMS: when the run left (DeadlineRun.Left) — the instant a
	// reporting period places it by, the overdue history's rule.
	LeftMS int64
	// Open: still out at the report's instant. Otherwise ArrivedMS is the
	// binding arrival's instant, by the receiving post's clock.
	Open       bool
	ArrivedMS  int64
	TurnedBack bool
	OverByMS   int64
}

// Touches reports whether the run concerns site (sent from or bound to it).
func (l LateRun) Touches(site string) bool { return l.Origin == site || l.DestinationSite == site }

// RunsPastAllowance judges every run with an allowance against nowMS. It
// returns the late ones (by due instant) and how many runs could not be
// judged because their due time or their binding arrival's instant did not
// resolve here. It reads DeadlineRuns — the overdue history's read — so a
// run is late here exactly when the history measures it past due.
func RunsPastAllowance(st *store.Store, nowMS int64) ([]LateRun, int, error) {
	runs, err := DeadlineRuns(st)
	if err != nil {
		return nil, 0, fmt.Errorf("domain: runs past allowance: %w", err)
	}
	var late []LateRun
	unjudged := 0
	for _, r := range runs {
		if r.Outcome == RunCancelled {
			continue
		}
		open := r.Outcome == RunOpen
		endMS, endOK := r.End.MS, r.End.OK
		if open {
			endMS, endOK = nowMS, true
		}
		leftMS, leftOK := r.Left()
		if !r.DueOK || !endOK || !leftOK {
			unjudged++
			continue
		}
		if endMS <= r.DueMS {
			continue
		}
		l := LateRun{TripID: r.TripID, Origin: r.Origin, DestinationSite: r.Destination, Destination: r.Uncontrolled,
			RoadWork: r.RoadWork, Vehicle: r.Vehicle, AllowanceMin: r.Allowance, DueMS: r.DueMS, LeftMS: leftMS,
			Open: open, OverByMS: endMS - r.DueMS, TurnedBack: r.Outcome == RunTurnedBack}
		if !open {
			l.ArrivedMS = endMS
		}
		late = append(late, l)
	}
	sort.SliceStable(late, func(i, j int) bool { return late[i].DueMS < late[j].DueMS })
	return late, unjudged, nil
}
