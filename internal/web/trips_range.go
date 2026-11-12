// trips_range.go — which trips /trips/detail lists (FIX17d, James 2026-10-07).
//
// The page used to list every trip of the season: ~14 MB and 3–4 s at season
// scale. It now lists the last seven days, a chosen range (a plain GET form,
// so it works with scripting off), or — on request, and said to be slow —
// everything, and offers the same trips as a CSV download.
//
// WHICH DAY A TRIP BELONGS TO. The instant every judge of a trip counts
// from: the dispatch's effective departure (domain.DispatchDeparture — the
// typed time, else the recording; departure.go), falling back, when the
// dispatch envelope is not in this store, to the resolved dispatch instant
// (the same pair recordedBasis reads). Days are this post's local days, both
// ends included, parsed exactly as the history reports parse theirs.
//
// The range is applied to the raw trips BEFORE their rows are composed
// (buildTripRowsScoped's keep), so a row is built by the same code from the
// same reads whether the range is a week or the whole season.
package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

const tripsRangeDefaultDays = 7

type tripsRange struct {
	All bool
	// VisitsOnly: the CSV is the visits table, not the trips (?table=visits).
	VisitsOnly bool
	From, To   string // YYYY-MM-DD, as shown in the form
	Problems   []string

	fromMS, toMS int64 // [from, to) in ms; unused when All
	days         int

	// Filled by finish, for the template.
	Line      string
	PrevHref  string
	PrevLabel string
	NextHref  string
	NextLabel string
	Unplaced  int // trips with no time known here (not in the range)
	// Visits: the Visits table follows the same days (by sign-in time).
	UnplacedVisits int
	VisitCount     int
	VisitsLine     string
	CSVVisitsHref  string
	Custom         bool
	CSVHref        string
	CSVAllHref     string

	when map[string]int64 // trip id -> departure ms, filled for the CSV
}

func localDay(v string) (time.Time, bool) {
	t, err := time.ParseInLocation("2006-01-02", v, time.Local)
	return t, err == nil
}

func (s *Server) parseTripsRange(r *http.Request) tripsRange {
	q := r.URL.Query()
	rng := tripsRange{VisitsOnly: q.Get("table") == "visits"}
	if strings.TrimSpace(q.Get("all")) != "" {
		rng.All = true
		return rng
	}
	now := time.UnixMilli(s.clockReading().WallMS).In(time.Local)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	from, to := strings.TrimSpace(q.Get("from")), strings.TrimSpace(q.Get("to"))
	var fromT, toT time.Time
	var okF, okT bool
	if from != "" {
		if fromT, okF = localDay(from); !okF {
			rng.Problems = append(rng.Problems, "From: write the date as YYYY-MM-DD.")
		}
	}
	if to != "" {
		if toT, okT = localDay(to); !okT {
			rng.Problems = append(rng.Problems, "To: write the date as YYYY-MM-DD.")
		}
	}
	rng.Custom = from != "" || to != ""
	switch {
	case len(rng.Problems) > 0:
		// Show the default week beside the complaint; the CSV refuses.
		rng.Custom = false
		fromT, toT = today.AddDate(0, 0, -(tripsRangeDefaultDays-1)), today
	case okF && okT:
	case okF:
		toT = today
		if toT.Before(fromT) {
			toT = fromT
		}
	case okT:
		fromT = toT.AddDate(0, 0, -(tripsRangeDefaultDays - 1))
	default:
		fromT, toT = today.AddDate(0, 0, -(tripsRangeDefaultDays-1)), today
	}
	if toT.Before(fromT) {
		rng.Problems = append(rng.Problems, "To is before From.")
		rng.Custom = false
		fromT, toT = today.AddDate(0, 0, -(tripsRangeDefaultDays-1)), today
	}
	rng.From, rng.To = fromT.Format("2006-01-02"), toT.Format("2006-01-02")
	end := toT.AddDate(0, 0, 1)
	rng.fromMS, rng.toMS = fromT.UnixMilli(), end.UnixMilli()
	rng.days = int(end.Sub(fromT).Round(24*time.Hour) / (24 * time.Hour))
	return rng
}

// tripDepartures places the trips on the clock in one pass: for each stream
// (origin, epoch) that holds trips, one narrow scan of the log's time columns
// (store.EventTimes), then domain.EffectiveDeparture on each trip's dispatch
// envelope — the very function DispatchDeparture applies to a point read. A
// trip whose dispatch envelope is not in this store falls back to the
// resolved dispatch instant, as recordedBasis does; one with neither is
// absent from the result.
func (s *Server) tripDepartures(trips []domain.Trip) (map[string]int64, error) {
	type stream struct {
		site  string
		epoch int64
	}
	want := map[stream]map[int64]struct{}{}
	for _, t := range trips {
		k := stream{t.OriginSite, t.NodeEpoch}
		if want[k] == nil {
			want[k] = map[int64]struct{}{}
		}
		want[k][t.Seq] = struct{}{}
	}
	envs := map[stream]map[int64]store.Envelope{}
	for k, seqs := range want {
		m, err := s.Store.EventTimes(k.site, k.epoch, seqs)
		if err != nil {
			return nil, err
		}
		envs[k] = m
	}
	out := make(map[string]int64, len(trips))
	for _, t := range trips {
		if env, ok := envs[stream{t.OriginSite, t.NodeEpoch}][t.Seq]; ok {
			out[t.TripID] = domain.EffectiveDeparture(env).UnixMilli()
		} else if at, ok := s.dispatchInstant(t); ok {
			out[t.TripID] = at.UnixMilli()
		}
	}
	return out, nil
}

// keep is the buildTripRowsScoped filter: nil for the unfiltered page.
// wantTimes also records each kept trip's departure (the CSV's column).
func (rng *tripsRange) keep(s *Server, wantTimes bool) (func(domain.Trip) bool, error) {
	if rng.All && !wantTimes {
		return nil, nil
	}
	trips, err := domain.Trips(s.Store)
	if err != nil {
		return nil, err
	}
	dep, err := s.tripDepartures(trips)
	if err != nil {
		return nil, err
	}
	if wantTimes {
		rng.when = dep
	}
	return func(t domain.Trip) bool {
		ms, ok := dep[t.TripID]
		if !ok {
			if rng.All {
				return true
			}
			rng.Unplaced++
			return false
		}
		return rng.All || (ms >= rng.fromMS && ms < rng.toMS)
	}, nil
}

func tripsDetailHref(from, to string) string {
	v := url.Values{}
	v.Set("from", from)
	v.Set("to", to)
	return "/trips/detail?" + v.Encode()
}

// finish writes the summary line and the neighbour links once the count is known.
func (rng *tripsRange) finish(s *Server, n int) {
	trips := strconv.Itoa(n) + " trips"
	if n == 1 {
		trips = "1 trip"
	}
	rng.CSVAllHref = "/trips/detail.csv?all=1"
	vn := strconv.Itoa(rng.VisitCount) + " visits"
	if rng.VisitCount == 1 {
		vn = "1 visit"
	}
	rng.VisitsLine = vn
	rng.CSVVisitsHref = "/trips/detail.csv?table=visits&all=1"
	if rng.All {
		rng.Line = "Showing every trip of the season · " + trips
		rng.CSVHref = rng.CSVAllHref
		return
	}
	fromT, _ := localDay(rng.From)
	toT, _ := localDay(rng.To)
	days := strconv.Itoa(rng.days) + " days"
	if rng.days == 1 {
		days = "1 day"
	}
	rng.Line = fmt.Sprintf("Showing %s (%s) · %s", dayRangeText(fromT, toT, time.UnixMilli(s.clockReading().WallMS).In(time.Local)), days, trips)
	rng.CSVHref = "/trips/detail.csv?" + url.Values{"from": {rng.From}, "to": {rng.To}}.Encode()
	rng.CSVVisitsHref = "/trips/detail.csv?" + url.Values{"table": {"visits"}, "from": {rng.From}, "to": {rng.To}}.Encode()
	step := rng.days
	label := func(dir string) string {
		if step == 1 {
			return dir + " day"
		}
		return fmt.Sprintf("%s %d days", dir, step)
	}
	rng.PrevHref = tripsDetailHref(fromT.AddDate(0, 0, -step).Format("2006-01-02"), toT.AddDate(0, 0, -step).Format("2006-01-02"))
	rng.PrevLabel = label("Previous")
	now := time.UnixMilli(s.clockReading().WallMS).In(time.Local)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	if toT.Before(today) {
		rng.NextHref = tripsDetailHref(fromT.AddDate(0, 0, step).Format("2006-01-02"), toT.AddDate(0, 0, step).Format("2006-01-02"))
		rng.NextLabel = label("Next")
	}
}

// dayRangeText: "6–13 Oct", "29 Sep–5 Oct", "6 Oct" and, when a year is not
// this year's, the year too.
func dayRangeText(from, to, now time.Time) string {
	withYear := from.Year() != now.Year() || to.Year() != now.Year()
	one := func(t time.Time, y bool) string {
		if y {
			return t.Format("2 Jan 2006")
		}
		return t.Format("2 Jan")
	}
	switch {
	case from.Equal(to):
		return one(from, withYear)
	case !withYear && from.Month() == to.Month():
		return fmt.Sprintf("%d–%s", from.Day(), one(to, false))
	default:
		return one(from, withYear) + "–" + one(to, withYear)
	}
}

var tripsCSVHeader = []string{"trip_id", "departed_utc", "vehicle", "driver", "from", "to", "state", "returned_to_origin"}

func (s *Server) tripsCSVRows(rows []tripListRow, when map[string]int64) [][]string {
	out := make([][]string, 0, len(rows))
	for _, r := range rows {
		dep := ""
		if ms, ok := when[r.TripID]; ok {
			dep = time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05Z")
		}
		ret := "no"
		if r.Returned {
			ret = "yes"
		}
		out = append(out, []string{r.TripID, dep, r.Vehicle, r.Driver, s.Cfg.SiteName(r.Origin), r.Destination, r.State, ret})
	}
	return out
}

// keepVisits is keep for the Visits table: a visit belongs to the day it was
// signed in — the arrival instant the visit report and the sign-out history
// use (domain.VisitReport, corrections applied). arrived maps visit id to
// that instant, for the CSV. A visit the report holds no resolvable arrival
// for appears only under Show all.
func (rng *tripsRange) keepVisits(s *Server) (func(domain.Visit) bool, map[string]int64, error) {
	recs, _, err := domain.VisitReport(s.Store, s.Cfg.KnownSiteTypes())
	if err != nil {
		return nil, nil, err
	}
	arrived := make(map[string]int64, len(recs))
	for _, r := range recs {
		if in := r.ArrivalInstant(); in.OK {
			arrived[r.VisitID] = in.MS
		}
	}
	if rng.All {
		return nil, arrived, nil
	}
	return func(v domain.Visit) bool {
		ms, ok := arrived[v.VisitID]
		if !ok {
			rng.UnplacedVisits++
			return false
		}
		return ms >= rng.fromMS && ms < rng.toMS
	}, arrived, nil
}

func visitStatusText(open bool) string {
	if open {
		return "on site"
	}
	return "checked out"
}

var visitsCSVHeader = []string{"signed_in_utc", "person", "site", "company", "trade", "status"}
