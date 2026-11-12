// gate_rowtime.go — ⟦FIX8, James, 2026-10-05⟧ "we need to be able to check
// in and out on each entry on differing times so there needs to be time in
// and out boxs added for use when checking someone in or out a differing
// time than current time".
//
// WHAT A PER-PERSON TIME CAN BE, WITHOUT A SCHEMA CHANGE (James chose this,
// 2026-10-05, over a SchemaVersion bump). A recorded time is an EVENT's
// (OccurredAt, TimeSource) envelope pair. The on-site roster's check-out
// list authors one visit_checked_out event PER PERSON, so each ticked row
// can carry its own time out — that is what this file reads. A vehicle's
// arrival or departure is ONE event for everyone aboard, so the vehicle
// forms keep one time, labelled for the whole vehicle; a person who came on
// their own gets their own time in through the individual check-in.
//
// THE RULES ARE THE EXISTING ONES. Every typed box goes through
// captureEnteredWall — the same parse and TP §4.6 bounds the form-level
// control uses (future beyond the allowance on a synced clock refuses; the
// records floor and the 30-day horizon warn and accept). The one rule this
// file adds: a time out earlier than that person's recorded time in is
// refused — for a row's own box and for the departing-vehicle door's
// entered time, NOT for the roster form's pre-existing form-level time
// (see rosterCheckoutTimes). Every refusal is
// all-or-nothing: nothing is recorded and the officer is told which person
// and why.
package web

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"sitecontrol/internal/domain"
)

// rowTimeOutPrefix names each roster row's own Time out box:
// time_out_<visit id>.
const rowTimeOutPrefix = "time_out_"

// rowTimeRefusal is a per-person refusal: plain words, nothing recorded.
type rowTimeRefusal struct {
	status int
	msg    string
}

func (e *rowTimeRefusal) Error() string { return e.msg }

// visitDisplayName is the name a refusal uses for one visit: the snapshot
// taken when they signed in, else the visit id (never blank).
func visitDisplayName(v *domain.Visit, fallback string) string {
	if v != nil && v.PersonSnapshotName != nil && strings.TrimSpace(*v.PersonSnapshotName) != "" {
		return *v.PersonSnapshotName
	}
	return fallback
}

// wallDisplay renders an instant the way an officer reads it in a refusal.
func wallDisplay(ms int64) string {
	return time.UnixMilli(ms).In(time.Local).Format("15:04 on 2 Jan 2006")
}

// strayRowTimes names every row whose Time out box was filled while the row
// itself was not ticked. Refused rather than ignored: a typed time on an
// unticked row is almost always a forgotten tick, and silently dropping a
// time the officer typed is the one outcome they cannot see.
func strayRowTimes(r *http.Request, ticked map[string]bool) []string {
	var stray []string
	for k, vals := range r.Form {
		if !strings.HasPrefix(k, rowTimeOutPrefix) {
			continue
		}
		vid := strings.TrimPrefix(k, rowTimeOutPrefix)
		if ticked[vid] {
			continue
		}
		for _, v := range vals {
			if strings.TrimSpace(v) != "" {
				stray = append(stray, vid)
				break
			}
		}
	}
	sort.Strings(stray)
	return stray
}

// rosterCheckoutTimes resolves one time pair per ticked visit: the row's own
// Time out box when filled (entered, bounds applied), else base — the
// form-level control's pair, which is "now" unless the officer changed it.
// Warnings from every entered value are merged (deduplicated) for the
// redirect. Nothing is authored here.
func (s *Server) rosterCheckoutTimes(r *http.Request, visitIDs []string, base timeCapture) ([]domain.VisitCheckoutAt, []string, *rowTimeRefusal) {
	ticked := make(map[string]bool, len(visitIDs))
	for _, id := range visitIDs {
		ticked[id] = true
	}
	if stray := strayRowTimes(r, ticked); len(stray) > 0 {
		names := make([]string, len(stray))
		for i, vid := range stray {
			v, _ := domain.VisitByID(s.Store, vid)
			names[i] = visitDisplayName(v, vid)
		}
		return nil, nil, &rowTimeRefusal{status: http.StatusConflict, msg: fmt.Sprintf(
			"A time out was typed for %s, but %s not ticked. Nothing has been recorded. Tick everyone whose time out you typed (or clear the box), then submit again.",
			strings.Join(names, ", "), map[bool]string{true: "those rows were", false: "that row was"}[len(names) > 1])}
	}

	out := make([]domain.VisitCheckoutAt, 0, len(visitIDs))
	warnSeen := map[string]bool{}
	var warnings []string
	addWarn := func(codes []string) {
		for _, c := range codes {
			if !warnSeen[c] {
				warnSeen[c] = true
				warnings = append(warnings, c)
			}
		}
	}
	addWarn(base.Warnings)
	var own []domain.VisitCheckoutAt
	for _, vid := range visitIDs {
		tc := base
		if wall := strings.TrimSpace(r.FormValue(rowTimeOutPrefix + vid)); wall != "" {
			got, cerr := s.captureEnteredWall(wall)
			if cerr != nil {
				v, _ := domain.VisitByID(s.Store, vid)
				return nil, nil, &rowTimeRefusal{status: cerr.status, msg: "Time out for " + visitDisplayName(v, vid) + ": " + cerr.msg}
			}
			tc = got
			addWarn(got.Warnings)
			own = append(own, domain.VisitCheckoutAt{VisitID: vid, OccurredAtMS: tc.OccurredAtMS, TimeSource: tc.TimeSource})
		}
		out = append(out, domain.VisitCheckoutAt{VisitID: vid, OccurredAtMS: tc.OccurredAtMS, TimeSource: tc.TimeSource})
	}
	// Judged on the rows' OWN boxes only. The form-level time on this form
	// keeps its existing contract (W4): a mistyped Remove time is a fact
	// the time-correction page fixes afterwards, and the W4 suite builds
	// exactly that case — refusing it here would retire a ratified path.
	if refusal := s.refuseOutBeforeInVisits(own); refusal != nil {
		return nil, nil, refusal
	}
	return out, warnings, nil
}

// refuseOutBeforeInVisits refuses any ENTERED time out earlier than that
// visit's recorded time in (the sign-in instant every roster row shows,
// domain.VisitSignInTimes — a won correction included). A default-path
// time out is "now", which no recorded time in can follow by more than the
// clock's own drift, so it is not judged here — the default path is never
// refused for a clock reason (timecapture.go). A visit whose time in cannot
// be read is not judged either: there is nothing to compare against, and
// refusing on an absence would refuse the truth.
func (s *Server) refuseOutBeforeInVisits(at []domain.VisitCheckoutAt) *rowTimeRefusal {
	var visits []domain.Visit
	byID := map[string]*domain.Visit{}
	for _, a := range at {
		if a.TimeSource != "entered" {
			continue
		}
		v, err := domain.VisitByID(s.Store, a.VisitID)
		if err != nil || v == nil {
			continue // CheckOutVisitsAt's own validation names an unknown visit
		}
		visits = append(visits, *v)
		byID[v.VisitID] = v
	}
	if len(visits) == 0 {
		return nil
	}
	in, err := domain.VisitSignInTimes(s.Store, s.Cfg.SiteID, visits)
	if err != nil {
		return nil
	}
	var problems []string
	for _, a := range at {
		inMS, ok := in[a.VisitID]
		if a.TimeSource != "entered" || !ok || a.OccurredAtMS >= inMS {
			continue
		}
		problems = append(problems, fmt.Sprintf("%s's time out (%s) is earlier than their time in (%s)",
			visitDisplayName(byID[a.VisitID], a.VisitID), wallDisplay(a.OccurredAtMS), wallDisplay(inMS)))
	}
	if len(problems) == 0 {
		return nil
	}
	return &rowTimeRefusal{status: http.StatusConflict, msg: strings.Join(problems, "; ") +
		". Nobody can leave before they arrived. Nothing has been recorded. Correct the time out and submit again."}
}

// refuseOutBeforeInPersons is the same rule for the departing-vehicle door:
// an ENTERED vehicle time out is checked against the open visit here of
// every person aboard. The departure is one event, so one time is judged
// against each of them.
func (s *Server) refuseOutBeforeInPersons(personIDs []string, tc timeCapture) *rowTimeRefusal {
	if tc.TimeSource != "entered" || len(personIDs) == 0 {
		return nil
	}
	open, err := domain.OpenVisitsAt(s.Store, s.Cfg.SiteID)
	if err != nil {
		return nil
	}
	aboard := make(map[string]bool, len(personIDs))
	for _, id := range personIDs {
		aboard[id] = true
	}
	var at []domain.VisitCheckoutAt
	for _, v := range open {
		if aboard[v.PersonID] {
			at = append(at, domain.VisitCheckoutAt{VisitID: v.VisitID, OccurredAtMS: tc.OccurredAtMS, TimeSource: tc.TimeSource})
		}
	}
	return s.refuseOutBeforeInVisits(at)
}

// dedupeStrings keeps the first occurrence of each value, in order.
func dedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
