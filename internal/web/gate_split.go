// gate_split.go — ⟦FIX8 option C, James, 2026-10-05⟧ "check in and out on
// each entry on differing times": every person row on the vehicle/group
// doors (and the roster's "Check out selected", which stages onto the
// departing one) carries its own optional Time in / Time out. On commit the
// rows are grouped by EFFECTIVE time — the row's own box, else the
// form-level time, else now — and each distinct time is recorded as ITS OWN
// trip, all inside the door's one Transact: every group lands or none does.
//
// NO SCHEMA CHANGE. A recorded time is an event envelope's (OccurredAt,
// TimeSource) pair, and a trip's dispatch/arrival events each carry exactly
// one; N times are N trips, each authored through the unchanged domain door
// (Dispatch outbound, GateCheckIn inbound), each entered time marked
// "entered" exactly as the form-level control marks it, each replicating as
// the ordinary events it already is. DEADLINES: each split departure
// carries the deadline minutes and is judged from its OWN typed time plus
// the allowance (James 2026-10-06: from the typed time —
// domain.EffectiveDeparture), exactly as for any backdated departure, so
// split trips recorded in one commit have one due time per typed time.
// ⟦was: the D13 basis — the dispatch envelope's authored_at — so split
// trips shared one due time and an entered time out did not move it⟧
//
// THE TWO REFUSALS (nothing recorded, list intact):
//   - A NAMED vehicle with differing times: one vehicle moves once. The
//     officer ticks Vehicle unknown (each time becomes its own unidentified
//     movement, the existing R-17 mechanism) or submits separately.
//   - A group with no driver. CHOSEN SHAPE (least confusing): the marked
//     driver drives their own group; a one-person group is its own driver
//     (one name, no other candidate — handleGateCheckInSingle's reasoning);
//     any other group gets a "Driver for the people leaving at 14:30"
//     picker of its own members, rendered whenever the rows' times differ,
//     and a commit with one left unchosen is refused naming that time.
//     Nobody is ever promoted into a driver slot automatically from a group
//     of two or more.
package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// groupDriverPrefix names a per-time driver pick: group_driver_<key>.
const groupDriverPrefix = "group_driver_"

// groupDriverView is one per-time driver picker the vehicle form renders.
type groupDriverView struct {
	Key     string
	Label   string
	Members []groupDriverMember
}

type groupDriverMember struct {
	ID      string // rowIdentity
	Name    string
	Checked bool
}

// timeGroup is one distinct effective time and the rows recorded at it.
type timeGroup struct {
	Key    string
	TC     timeCapture
	Rows   []int // indexes into the form's rows, in row order
	Driver int   // the row driving this group's trip; -1 until decided
}

// rowIdentity names a row's person stably across accumulate round trips
// (row INDEXES renumber; a pick must travel with the person, the driver
// flag's own rule).
func rowIdentity(row passengerRow) string { return row.Kind + ":" + row.Value }

// timeKey is a group's key: "now" for the default path, else the instant.
func timeKey(tc timeCapture) string {
	if tc.TimeSource != "entered" {
		return "now"
	}
	return strconv.FormatInt(tc.OccurredAtMS, 10)
}

func groupLabel(tc timeCapture) string {
	if tc.TimeSource != "entered" {
		return "the current time"
	}
	return wallDisplay(tc.OccurredAtMS)
}

// rowsLabel lists a group's people for a refusal.
func rowsLabel(f vehicleForm, idx []int) string {
	names := make([]string, len(idx))
	for i, n := range idx {
		names[i] = f.Rows[n].Typed
	}
	return strings.Join(names, "; ")
}

// splitByTime groups f's rows by effective time and decides each group's
// driver. base is the form-level capture (already bounds-checked). Every
// row box goes through captureEnteredWall — the same parse and TP §4.6
// bounds as the form-level control. Authors nothing.
func (s *Server) splitByTime(f vehicleForm, base timeCapture) ([]timeGroup, *rowTimeRefusal) {
	word := "Time in"
	if f.Direction == gateOutbound {
		word = "Time out"
	}
	var groups []timeGroup
	byKey := map[string]int{}
	for i, row := range f.Rows {
		tc := base
		if row.Time != "" {
			got, cerr := s.captureEnteredWall(row.Time)
			if cerr != nil {
				return nil, &rowTimeRefusal{status: cerr.status, msg: word + " for " + row.Typed + ": " + cerr.msg}
			}
			tc = got
		}
		k := timeKey(tc)
		gi, ok := byKey[k]
		if !ok {
			gi = len(groups)
			byKey[k] = gi
			groups = append(groups, timeGroup{Key: k, TC: tc, Driver: -1})
		}
		groups[gi].Rows = append(groups[gi].Rows, i)
		groups[gi].TC.Warnings = mergeWarnings(groups[gi].TC.Warnings, tc.Warnings)
	}
	if len(groups) > 1 && !f.VehicleUnknown {
		return nil, &rowTimeRefusal{status: http.StatusConflict, msg: "These people have different times, but one vehicle moves once. Nothing has been recorded and the list below is intact. Tick Vehicle unknown to record each time as its own movement, or submit the people with each time separately."}
	}
	for gi := range groups {
		g := &groups[gi]
		for _, n := range g.Rows {
			if f.Rows[n].IsDriver {
				g.Driver = n
			}
		}
		if g.Driver >= 0 {
			continue
		}
		if len(g.Rows) == 1 {
			g.Driver = g.Rows[0]
			continue
		}
		if pick := f.GroupDriverPick[g.Key]; pick != "" {
			for _, n := range g.Rows {
				if rowIdentity(f.Rows[n]) == pick {
					g.Driver = n
				}
			}
		}
		if g.Driver < 0 {
			verb := "arriving"
			if f.Direction == gateOutbound {
				verb = "leaving"
			}
			return nil, &rowTimeRefusal{status: http.StatusConflict, msg: fmt.Sprintf(
				"Nobody is marked as driving the people %s at %s (%s). Each different time is recorded as its own movement, and every movement needs a driver. Nothing has been recorded and the list below is intact. Choose one under “Driver for the people %s at %s”, or give them the same time as the marked driver.",
				verb, groupLabel(g.TC), rowsLabel(f, g.Rows), verb, groupLabel(g.TC))}
		}
	}
	return groups, nil
}

func mergeWarnings(a, b []string) []string {
	for _, c := range b {
		dup := false
		for _, x := range a {
			if x == c {
				dup = true
			}
		}
		if !dup {
			a = append(a, c)
		}
	}
	return a
}

// groupDriverViews computes, at render, the per-time driver pickers the
// form shows: only when the rows' times differ, only for groups of two or
// more that do not hold the marked driver. Display only — it parses without
// bounds (the commit applies them) and skips a row whose time it cannot
// read (the commit refuses that row by name).
func (s *Server) groupDriverViews(f vehicleForm) []groupDriverView {
	if len(f.Rows) < 2 {
		return nil
	}
	siteNow := time.UnixMilli(s.clockReading().WallMS).In(time.Local)
	base := timeCapture{TimeSource: "default"}
	if f.TimeSource == "entered" {
		if t, err := parseWallTime(strings.TrimSpace(f.OccurredAtWall), siteNow); err == nil {
			base = timeCapture{TimeSource: "entered", OccurredAtMS: t.UnixMilli()}
		}
	}
	type grp struct {
		tc   timeCapture
		rows []int
	}
	var order []string
	groups := map[string]*grp{}
	for i, row := range f.Rows {
		tc := base
		if row.Time != "" {
			t, err := parseWallTime(row.Time, siteNow)
			if err != nil {
				continue
			}
			tc = timeCapture{TimeSource: "entered", OccurredAtMS: t.UnixMilli()}
		}
		k := timeKey(tc)
		if groups[k] == nil {
			groups[k] = &grp{tc: tc}
			order = append(order, k)
		}
		groups[k].rows = append(groups[k].rows, i)
	}
	if len(order) < 2 {
		return nil
	}
	var out []groupDriverView
	for _, k := range order {
		g := groups[k]
		if len(g.rows) < 2 {
			continue
		}
		hasDriver := false
		for _, n := range g.rows {
			if f.Rows[n].IsDriver {
				hasDriver = true
			}
		}
		if hasDriver {
			continue
		}
		v := groupDriverView{Key: k, Label: groupLabel(g.tc)}
		for _, n := range g.rows {
			id := rowIdentity(f.Rows[n])
			v.Members = append(v.Members, groupDriverMember{ID: id, Name: f.Rows[n].Typed, Checked: f.GroupDriverPick[k] == id})
		}
		out = append(out, v)
	}
	return out
}

// TimeLabel is a row's own time as the compact check-out summary shows it
// (the staged rows themselves are hidden there): the datetime-local wire
// form read as "15:04 on 2 Jan 2006", anything else exactly as typed, ""
// when blank.
func (r passengerRow) TimeLabel() string {
	if r.Time == "" {
		return ""
	}
	if t, err := time.ParseInLocation(wallTimeLayout, r.Time, time.Local); err == nil {
		return t.Format("15:04 on 2 Jan 2006")
	}
	return r.Time
}
