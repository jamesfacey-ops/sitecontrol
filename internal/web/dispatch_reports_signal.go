// dispatch_reports_signal.go — the dispatch reports' lost-signal line, read
// from the serving post's lost-signal record (FIX16 integration: stream E's
// "not available" wired to stream F's site-local signal_outage table).
//
// WHAT IT CAN SAY. The record exists only from the first time a post ran a
// binary with the recorder (signal_history.go: past outages cannot be
// reconstructed). So:
//   - no table here, or no row in it: "not available", as before;
//   - a period wholly before the first row's recorder start: "not
//     available", with the date recording began;
//   - otherwise the outages overlapping the period, from THIS post's view
//     (which peers it lost contact with), "recorded since <first row>" —
//     and when the period begins before that, the page says the earlier part
//     is not available rather than implying it was quiet.
//
// "The first row" is the earliest recorder run that wrote one
// (opened_run_ms): the recorder was running from that process start, so a
// period after it with no rows was genuinely quiet as this post saw it.
package web

import (
	"errors"
	"fmt"
	"log"
	"time"
)

// signalGapView is the lost-signal line for one site (or every site) and
// one period.
type signalGapView struct {
	Available bool
	Count     int
	// Value is the table/CSV value ("—" when not available); Detail the
	// sentence beside it; Why, when not available, the reason alone.
	Value, Detail, Why string
}

// signalGapsFor reads this post's record for [fromMS, toMS). site "" means
// every peer; a site equal to this post's own id also means every peer (its
// own record is of everyone it lost).
func (s *Server) signalGapsFor(site string, fromMS, toMS int64) signalGapView {
	notAvail := func(why string) signalGapView {
		return signalGapView{Value: "—", Detail: "not available: " + why, Why: why}
	}
	rows, err := s.signalOutages()
	switch {
	case errors.Is(err, errSignalNotRecorded):
		return notAvail("this post keeps no lost-signal record")
	case err != nil:
		log.Printf("reports: %v", err)
		return notAvail("this post's lost-signal record could not be read")
	case len(rows) == 0:
		return notAvail("this post has recorded no lost-signal history yet")
	}
	since := rows[0].OpenedRunMS
	for _, o := range rows {
		if o.OpenedRunMS < since {
			since = o.OpenedRunMS
		}
	}
	sinceText := time.UnixMilli(since).In(time.Local).Format("2 Jan 2006 15:04")
	if toMS <= since {
		return notAvail("recorded only since " + sinceText)
	}
	v := signalGapView{Available: true}
	var total time.Duration
	open := 0
	for _, o := range rows {
		if site != "" && site != s.Cfg.SiteID && o.Peer != site {
			continue
		}
		end := o.EndedMS
		if o.Open() {
			end = s.reportNow().UnixMilli()
		}
		if o.LostFromMS >= toMS || end <= fromMS {
			continue
		}
		v.Count++
		if o.Open() {
			open++
		}
		a, b := o.LostFromMS, end
		if a < fromMS {
			a = fromMS
		}
		if b > toMS {
			b = toMS
		}
		if b > a {
			total += time.Duration(b-a) * time.Millisecond
		}
	}
	v.Value = fmt.Sprint(v.Count)
	who := "as " + s.Cfg.PostName() + " saw its contact with the other posts"
	if site != "" && site != s.Cfg.SiteID {
		who = "as " + s.Cfg.PostName() + " saw its contact with " + s.Cfg.SiteName(site)
	}
	switch {
	case v.Count == 0:
		v.Detail = "none, " + who
	default:
		v.Detail = hoursMinutes(total) + " without contact in this period, " + who
		if open > 0 {
			v.Detail += fmt.Sprintf(" (%d still lost)", open)
		}
	}
	v.Detail += "; recorded since " + sinceText
	if fromMS < since {
		v.Detail += " — the part of this period before that is not available"
	}
	return v
}
