// road_work.go — the WORKING ON THE ROAD category on the operator surfaces
// (James, 2026-10-03: "the ice road needs another category for equipment
// working on the ice road so that they are not listed as overdue when they
// will spend up to 12 hours on the road working, so once 12 hours hits they
// will be labelled overdue").
//
// The domain side is domain/road_work.go; the clock is deadline/scan.go's.
// This file only RENDERS: one row shape shared by the operator console
// (/trips) and the gate's vehicle page (/gate/vehicle), so the two cannot
// disagree about which run is out, for how long, or whether it is overdue.
//
// TWO KINDS OF ROW ON ONE LIST, told apart by Own:
//
//	Own    this post sent the equipment out. It holds the clock (the scan
//	       evaluates its own road-work runs — the origin owns them), so the
//	       row carries the C7 verdict (due / OVERDUE / verify by radio), the
//	       ack attribution, and the "back from the road" check-in.
//	!Own   another post sent it out and the record has replicated here. No
//	       clock is held for it here (the other post's is the one that
//	       alerts), so the row shows the LIMIT that was promised and nothing
//	       invented — the same posture the muster takes for a peer's clock —
//	       plus the "arrived here instead" door, because the vehicle arrives
//	       where it arrives (D-96) and this post can record that.
package web

import (
	"fmt"
	"sort"

	"sitecontrol/internal/config"
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
)

// roadWorkRow is one open road-work run as a board renders it.
type roadWorkRow struct {
	TripID  string
	Vehicle string // the dispatch-time snapshot through tripVehicleDisplay, like every board row
	Origin  string // the post that sent it out (an id: for data, never for prose)
	// OriginName is Origin by display name — James, 2026-10-04: no screen shows a raw site id.
	OriginName string
	Driver     string
	// Occupants: everyone aboard, name-sorted — the check-in manifest.
	Occupants []domain.OccupantRef
	// Own: this post sent it out (see the file comment).
	Own bool
	// Limit is the authored working limit ("12 h") — the deadline the run
	// was stamped with at dispatch, true everywhere.
	Limit string

	// The C7 verdict, Own rows only (the same vocabulary as boardRow /
	// inboundVehicleRow: "overdue" | "unresolved" | "due" | "none").
	Urgency      string
	UrgencyLabel string
	Countdown    string
	// Open: an overdue row renders expanded on the gate page — the people
	// this surface is least able to account for must not be a click away.
	Open bool

	StalenessFace string
	// ⟦was: PhaseFace, Override — weather phases removed, James 2026-10-04⟧
	Repudiation string
	AckFace     string
	Unackable   bool
	// VerifyOnlyAck (FINDINGS-R5 F4): see boardRow.VerifyOnlyAck.
	VerifyOnlyAck string
	remaining     int64
}

// formatLimit renders a configured working limit in minutes the way an
// officer says it — "12 h", "1 h 30 min", "2 min".
func formatLimit(mins int64) string {
	return limitTextMinutes(mins)
}

func limitTextMinutes(mins int64) string {
	h, m := mins/60, mins%60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%d h %d min", h, m)
	case h > 0:
		return fmt.Sprintf("%d h", h)
	default:
		return fmt.Sprintf("%d min", m)
	}
}

// roadWorkRows builds the list for THIS post: every open road-work run the
// store knows (any origin), worst-first within this post's own runs, then
// the other posts' runs oldest-first. alertOf is the scan the caller already
// ran — empty when the scanner is not wired (web-layer tests), in which case
// every own row reads "NO TIMER" rather than an invented verdict.
func (s *Server) roadWorkRows(alertOf map[string]deadline.Alert) ([]roadWorkRow, error) {
	runs, err := domain.OpenRoadWork(s.Store, "")
	if err != nil {
		return nil, err
	}
	var rows []roadWorkRow
	for _, e := range runs {
		row := roadWorkRow{
			TripID: e.Trip.TripID, Vehicle: tripVehicleDisplay(e.Trip.VehicleIsUnidentified(), e.Trip.VehicleEntered),
			Origin: e.Trip.OriginSite, OriginName: s.Cfg.SiteName(e.Trip.OriginSite), Driver: e.DriverName,
			Occupants: append([]domain.OccupantRef(nil), e.OccupantList...),
			Own:       e.Trip.OriginSite == s.Cfg.SiteID,
			Limit:     "—", Urgency: "none", UrgencyLabel: "NO TIMER",
		}
		sortOccupantRefs(row.Occupants)
		if row.Driver == "" && e.Trip.DriverPersonID != "" {
			row.Driver = e.Trip.DriverPersonID // the roster row has not replicated here yet — the id, never a blank
		}
		if e.Trip.DeadlineMinutes != nil {
			row.Limit = formatLimit(*e.Trip.DeadlineMinutes)
		}
		if !row.Own {
			// Another post's run: no clock held here, by design. The row
			// says what was promised; the owning post's banner says whether
			// it is late.
			row.UrgencyLabel = "CLOCK AT " + s.Cfg.SiteName(e.Trip.OriginSite)
			rows = append(rows, row)
			continue
		}
		if a, ok := alertOf[e.Trip.TripID]; ok {
			applyRoadWorkFace(&row, a)
		}
		rows = append(rows, row)
	}
	// Own rows first, worst-first within them (the same urgencyRank the
	// other two boards use); then the other posts' runs, oldest-first.
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Own != rows[j].Own {
			return rows[i].Own
		}
		ri, rj := urgencyRank(rows[i].Urgency), urgencyRank(rows[j].Urgency)
		if ri != rj {
			return ri < rj
		}
		if rows[i].remaining != rows[j].remaining {
			return rows[i].remaining < rows[j].remaining
		}
		return rows[i].TripID < rows[j].TripID
	})
	return rows, nil
}

// applyRoadWorkFace copies the scan's verdict onto an OWN row — a translation
// of deadline.Status into the row's fields, the same four cases
// inbound.go's applyAlertFace and trips.go's board render, adding no
// judgement of its own. Wording differs from the inbound board's in one
// respect only: the figure is read against a working LIMIT, so the countdown
// says "out N of 12 h", and the overdue face says how long past the limit.
func applyRoadWorkFace(row *roadWorkRow, a deadline.Alert) {
	switch a.Status {
	case deadline.StatusCountdown:
		row.Urgency, row.UrgencyLabel = "due", "WORKING"
		row.Countdown = "working — " + formatHM(a.Remaining) + " left of the " + a.LimitText() + " limit"
		row.remaining = int64(a.Remaining)
	case deadline.StatusOverdueLive:
		row.Urgency, row.UrgencyLabel = "overdue", "OVERDUE"
		row.Countdown = "OVERDUE — out " + formatHM(a.OverdueBy) + " past the " + a.LimitText() + " limit"
		row.Open = true
	case deadline.StatusOverdueUnresolved:
		// No numeral exists for this state: the limit passed with no
		// credible time basis here (restart, replay, a bad clock). The
		// sentence says that; a number would be a fabrication.
		// Q-FE2: the scan's own label — never OVERDUE on a row this post
		// cannot count. ⟦was: always "OVERDUE — UNRESOLVED"⟧
		row.Urgency, row.UrgencyLabel = "unresolved", a.ReviewLabel()
		row.Countdown = "past its limit by an amount this post cannot measure — verify by radio"
		if a.DeadlineUnknown {
			// D15's class (FINDINGS-R4): "past its limit" is a claim this
			// post cannot make for a replay or a missing capture.
			row.Countdown = "this post cannot count its limit right now — verify by radio"
		}
		if a.ClockChanged { // R9, James 2026-10-04: never a fabricated OVERDUE
			row.Countdown = deadline.ClockChangedNote
		}
		row.Open = true
	default:
		// Any status added later keeps "NO TIMER" rather than inventing a
		// verdict. ⟦was: "StatusPhaseReview lives in Board.PhaseReview …" —
		// weather phases removed⟧
	}
	if a.AckFace != "" {
		row.Countdown += " (" + a.AckFace + ")"
	}
	row.AckFace = a.AckFace
	row.Repudiation = a.RepudiationFace
	row.Unackable = a.Unackable() // F4 ⟦was: a.HasStandingAck⟧
	row.VerifyOnlyAck = a.VerifyOnlyAckFace
	row.StalenessFace = a.StalenessFace
}

// roadWorkChoice is what the dispatch forms render for the category: whether
// this post offers it, what it is called, and the limit it applies. One
// struct so the console and the gate read the same three facts from config
// the same way.
type roadWorkChoice struct {
	Enabled bool
	Label   string
	Limit   string // "12 h"
	Value   string // config.RoadWorkChoice — the destination_id the forms submit
}

func roadWorkChoiceFor(cfg *config.Config) roadWorkChoice {
	c := roadWorkChoice{Enabled: cfg.RoadWorkEnabled(), Label: cfg.RoadWorkLabel(), Value: config.RoadWorkChoice}
	if c.Enabled {
		c.Limit = formatLimit(cfg.RoadWorkLimitMinutes())
	}
	return c
}
