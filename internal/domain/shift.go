// shift.go — the post's SHIFT TOTALS panel figures (FIX16 stream G, James
// approved): sign-ins, sign-outs, vehicles out, vehicles in and the on-site
// count for the CURRENT shift at one post.
//
// NOT A SECOND COUNTER. The figures are the analytics page's "today" figures
// with the window changed from the local day to the shift (AnalyticsWindow
// is the one function behind both), so the panel cannot disagree with
// /analytics for the same instants. Read-only, no event minted.
package domain

import (
	"fmt"
	"sort"
	"time"

	"sitecontrol/internal/store"
)

// DefaultShiftStarts is the shift change times, in minutes after local
// midnight, when the config names none: 06:00 and 18:00.
var DefaultShiftStarts = []int{6 * 60, 18 * 60}

// ParseShiftStart reads "HH:MM" (24 hour) into minutes after midnight.
func ParseShiftStart(s string) (int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil || len(s) != 5 {
		return 0, fmt.Errorf("%q is not a 24-hour HH:MM time such as \"06:00\"", s)
	}
	return t.Hour()*60 + t.Minute(), nil
}

// ShiftWindow returns the [start, end) instants, in unix ms, of the shift
// containing now, given the shift change times (minutes after local midnight)
// on loc's clock. A shift may cross midnight (18:00 → 06:00). starts empty
// means DefaultShiftStarts. Wall-clock times are built with time.Date per
// calendar day, so a DST change moves the boundary with the clock rather than
// drifting by an hour.
func ShiftWindow(now time.Time, loc *time.Location, starts []int) (startMS, endMS int64) {
	if loc == nil {
		loc = time.Local
	}
	if len(starts) == 0 {
		starts = DefaultShiftStarts
	}
	sorted := append([]int(nil), starts...)
	sort.Ints(sorted)
	local := now.In(loc)
	var bounds []time.Time // ascending: yesterday's, today's, tomorrow's changes
	for d := -1; d <= 1; d++ {
		for _, m := range sorted {
			bounds = append(bounds, time.Date(local.Year(), local.Month(), local.Day()+d, m/60, m%60, 0, 0, loc))
		}
	}
	start, end := bounds[0], bounds[1]
	for i := range bounds {
		if !bounds[i].After(now) && i+1 < len(bounds) {
			start, end = bounds[i], bounds[i+1]
		}
	}
	return start.UnixMilli(), end.UnixMilli()
}

// ShiftTotals is the panel's data for one site.
type ShiftTotals struct {
	StartMS, EndMS int64
	SignIns        int
	SignOuts       int
	VehiclesOut    int // trips with a destination plus road-work runs, dispatched in the window
	VehiclesIn     int // arrivals recorded here, as analytics' TRAVEL IN counts them
	OnSite         int // on site now (not windowed)
	// Unresolved: instants whose time the resolver failed closed on, left
	// out of every windowed count exactly as /analytics leaves them out.
	Unresolved int
}

// BuildShiftTotals derives siteID's totals for the shift containing now.
func BuildShiftTotals(st *store.Store, siteID string, now time.Time, loc *time.Location, starts []int) (ShiftTotals, error) {
	startMS, endMS := ShiftWindow(now, loc, starts)
	fig, err := AnalyticsWindow(st, startMS, endMS)
	if err != nil {
		return ShiftTotals{}, err
	}
	return shiftTotalsFrom(fig, siteID), nil
}

// shiftTotalsFrom reads one site's row out of figures built for a window.
func shiftTotalsFrom(fig AnalyticsFigures, siteID string) ShiftTotals {
	t := ShiftTotals{StartMS: fig.DayStartMS, EndMS: fig.DayEndMS, Unresolved: fig.TimesUnresolved}
	for _, s := range fig.Sites {
		if s.SiteID == siteID {
			t.SignIns, t.SignOuts = s.VisitsToday, s.SignOutsToday
			t.VehiclesOut = s.TravelOutToday + s.RoadWorkOutToday
			t.VehiclesIn = s.TravelInToday
			t.OnSite = s.OnSiteNow
		}
	}
	return t
}
