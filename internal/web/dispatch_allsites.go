// dispatch_allsites.go — FIX14: the dispatch view lists every post's
// late runs, whichever post serves it (James 2026-10-05: "can we make the
// primary server of the dispatch page NDB, it will be the most likely to
// stay the longest").
//
// THE GAP THIS CLOSES. The strip and the "overdue now" tile come from the
// serving post's own deadline scan, and C7 judges a run only at its two ends
// (deadline.OnApplied captures runs bound HERE; NEW-a adds the runs sent out
// from here). Measured on a lane, 2026-10-05: with five reds and a road-work
// run past its limit between Ice Road and Ice Pad, the dispatch view served
// by Ice Road listed all six; served by NDB the strip was EMPTY and the tile
// said 0. The coordinator would have seen a quiet board.
//
// WHAT IS ADDED, on the dispatch host only (post hosts and the site pages
// are unchanged): every open run the serving post is NOT on — a controlled
// run between two other posts, or another post's equipment working on the
// road — judged by its ORIGIN's clock: the dispatch's effective departure
// plus its allowance (working limit), the instant both ends count from
// (recordedBasis; James 2026-10-06: from the typed time ⟦was: FINDINGS-R4
// D13, the envelope's authored_at⟧). Once that has passed, the run
// is a red OVERDUE on the strip and in "overdue now", exactly as the ends
// show it. James, 2026-10-05: "on the dispatch host a run the serving post
// is not on shows red OVERDUE as soon as it is past its deadline by the
// origin's recorded time" ⟦was (first cut, same day): an amber "PAST
// ALLOWANCE — verify by radio" line, never OVERDUE⟧. A run whose dispatch
// time cannot be resolved here at all is the one case even that time cannot
// judge: amber VERIFY BY RADIO. Read-only: nothing is authored or fetched.
//
// ACKNOWLEDGED. A red ack is stamped by the acknowledging post from its own
// board (deadline.OverdueAckedBy), so it is the end's own verdict, replicated
// in. Such a run is handled — silenced at both ends — so, as at the posts, it
// leaves the strip, and the overview names it "acknowledged as OVERDUE at
// <post>".
package web

import (
	"sort"
	"time"

	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
)

// dispatchPastView is the dispatch view's all-sites addition: the late runs
// the serving post is not on.
type dispatchPastView struct {
	// Rows: unacknowledged, as deadline.Alert values the strip and the
	// overview count like the scan's own — overdue_live past the origin's
	// recorded deadline, overdue_unresolved (DeadlineUnknown) when no
	// dispatch time resolves here. Merged into the page's Banner.
	Rows []deadline.Alert
	// AckedOverdue: the runs past their deadline whose red was acknowledged
	// at a post on the run, each with who acknowledged it.
	AckedOverdue []dispatchAckedRun
}

type dispatchAckedRun struct {
	TripID  string
	Vehicle string
	By      []string // site ids of the acknowledging posts
}

// dispatchPastAllowance builds the view. Read-only.
func (s *Server) dispatchPastAllowance(now time.Time) (dispatchPastView, error) {
	var out dispatchPastView
	acked, err := deadline.OverdueAckedBy(s.Store)
	if err != nil {
		return out, err
	}
	own := s.Cfg.SiteID
	for _, p := range s.Cfg.Peers {
		origin := p.SiteID
		for _, roadWork := range []bool{false, true} {
			read := domain.InTransitFrom
			if roadWork {
				read = domain.RoadWorkFrom
			}
			rows, err := read(s.Store, origin)
			if err != nil {
				return out, err
			}
			for _, row := range rows {
				if row.Destination == own {
					continue // this post is on the run: its own scan judges it (the strip's other lines)
				}
				c, err := domain.EffectiveTrip(s.Store, row.TripID)
				if err != nil {
					return out, err
				}
				if c == nil || c.Trip.DeadlineMinutes == nil {
					continue
				}
				x := time.Duration(*c.Trip.DeadlineMinutes) * time.Minute
				a := deadline.Alert{
					TripID: row.TripID, Vehicle: row.Vehicle, Origin: origin, Destination: row.Destination,
					Driver: row.Driver, Occupants: row.Count(), Deadline: x, RoadWork: roadWork, Outbound: !roadWork,
				}
				at, ok := s.dispatchInstant(c.Trip)
				if !ok {
					// No dispatch time resolves here: nothing to judge it by.
					a.Status, a.DeadlineUnknown = deadline.StatusOverdueUnresolved, true
					a.Reason = "its dispatch time does not resolve at this post, so it cannot be timed here"
				} else {
					due := s.recordedBasis(c.Trip, at).Add(x)
					if !now.After(due) {
						continue
					}
					a.Status, a.OverdueBy, a.DueAt = deadline.StatusOverdueLive, now.Sub(due).Truncate(time.Second), due
				}
				if by := acked[row.TripID]; len(by) > 0 {
					if a.Status == deadline.StatusOverdueLive {
						out.AckedOverdue = append(out.AckedOverdue, dispatchAckedRun{TripID: row.TripID, Vehicle: row.Vehicle, By: by})
					}
					continue
				}
				out.Rows = append(out.Rows, a)
			}
		}
	}
	sortAlertsWorstFirst(out.Rows)
	sort.SliceStable(out.AckedOverdue, func(i, j int) bool { return out.AckedOverdue[i].Vehicle < out.AckedOverdue[j].Vehicle })
	return out, nil
}
