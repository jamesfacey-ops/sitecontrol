// inbound.go — "who is on the road TOWARD this post", rendered at the gate.
//
// JAMES, 2026-08-21: "a list below that has the persons in transit to the
// site control, so when ice road checks someone in it populates on the
// bottom list, and pad site control can click on a vehicle and it will bring
// up the list of people in it, with a check in button on the bottom to check
// everyone in" — and then, on the control itself: "a checkbox that defaults
// to checked, and a 'check in all checked' — that way if someone does go to
// the other pad in route the site control can check them off before signing
// everyone on".
//
// WHAT IS NEW HERE AND WHAT IS NOT. Almost none of this is a new mechanism,
// and it was deliberately built that way:
//
//   - the DATA is domain.OpenExpectations, the same projection the operator
//     console's inbound board has always read;
//   - the DEADLINE FACE is the C7 scan's own verdict (deadline.Alert.Status),
//     never a re-derivation — this file makes no judgement the scan did not;
//   - the WRITE is POST /trips/arrive → domain.RecordExpectedArrivalExcluding,
//     the existing arrival path, unchanged.
//
// What is genuinely new is the SURFACE and the CONTROL: the list now lives at
// /gate, under the arriving-vehicle form, which is where an officer stands
// when a truck pulls up — the console is a different room's screen. And the
// per-person control is ticked-by-default ("this person is coming in")
// instead of the console's ticked-to-exclude ("stayed behind — do not check
// in"). See arrivalDisposition below for the safety derivation, which is not
// the console's and was not copied from it.
//
// WHY THIS IS NOT THE MANIFEST PREFILL D-3 WAIVED — read this before
// "correcting" it. D-3 refuses prefilling a check-in from a FLIGHT OR CREW
// MANIFEST, because a manifest states who was SCHEDULED to travel: "some
// people dont make the flight". The list below is categorically different in
// origin. Every name on it was entered ONE AT A TIME by an operator at the
// ORIGIN gate, from observed presence, under D-3's own rule, and then
// travelled with the vehicle as the trip's occupant rows. It is not an
// intention; it is a sighting, recorded by a colleague at the other end of
// the same road, and replicated. Refusing to lean on it would not make the
// system safer — it would make the Pad re-type 26 names it already holds, at
// -30, while the people stand outside. D-3 restricts entering somebody onto
// the muster without an operator affirming that person INDIVIDUALLY, and
// this surface still requires exactly that: a per-person control the officer
// looks at and can clear. What it removes is the TYPING, not the affirming.
package web

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
)

// inboundVehicleRow is one vehicle on the road toward this post.
//
// The SUMMARY fields (Vehicle, Origin, Occupants, Due, UrgencyLabel) are
// what a collapsed row shows: enough to tell six inbound trucks apart at a
// glance and no more. Everything else appears only when the row is opened.
type inboundVehicleRow struct {
	TripID  string
	Vehicle string // verbatim entered form, always (M8)
	Origin  string // the post that dispatched it
	Driver  string
	// Occupants is the manifest, name-sorted: this list is read against the
	// people standing in front of the officer, so it has to be in the order
	// a human scans rather than the order the occupant rows were authored.
	Occupants []domain.OccupantRef

	// Due is the journey allowance the origin promised ("90 min"), or "—"
	// when none was recorded. It is the trip's authored data, not a clock
	// reading, and the template labels it as such.
	Due string

	// Urgency / UrgencyLabel: the C7 scan's OWN verdict about this
	// expectation, in the same four-value vocabulary the operator console's
	// board uses, so the two surfaces can never disagree about which trip is
	// late. Nothing is judged here.
	//
	//	"overdue"    — StatusOverdueLive: the deadline passed on a live link
	//	"unresolved" — StatusOverdueUnresolved: it passed while the link was
	//	               down, so no honest figure exists — verify by radio
	//	"due"        — StatusCountdown: a timer is running
	//	"none"       — no scan verdict for this trip (no timer, or the scanner
	//	               is not wired in a web-layer test)
	Urgency      string
	UrgencyLabel string
	// Countdown is the scan's own text for the row's face — "12m30s
	// remaining", "OVERDUE by 4m", or the unresolved sentence. Empty when
	// there is no verdict.
	Countdown string
	// Open forces the vehicle's <details> block open on render. An overdue
	// or overdue-unresolved run must not be one click away from being
	// noticed: those people are the least accounted-for on the whole page.
	Open bool
	// StalenessFace: carried straight from the scan, exactly as it is
	// everywhere else it appears. ⟦was: also PhaseFace and Override — weather phases removed, James 2026-10-04⟧
	StalenessFace string
	// remaining orders the "due" band: soonest first. Not rendered.
	remaining int64
}

// inboundVehicleRows builds the gate's inbound list: open expectations for
// THIS site, worst-first, each carrying the C7 scan's verdict.
//
// It is destination-scoped on purpose and that is NOT in tension with the
// muster's origin-scoped "on the road" section (domain/in_transit.go). The
// two answer different questions and are used by different people:
//
//	the muster        WHO IS THIS OPERATION ACCOUNTABLE FOR — a document,
//	                  scoped so that summing every post's sheet counts each
//	                  person exactly once. Origin-owned, because the origin's
//	                  own stream always holds its dispatch, link or no link.
//	this list         WHAT IS ABOUT TO ARRIVE AT MY WINDOW — an operator
//	                  surface, scoped to the post that will do the work of
//	                  checking them in and that holds their arrival clock.
//
// A row here is therefore never counted in anything. It is a work queue.
func (s *Server) inboundVehicleRows() ([]inboundVehicleRow, error) {
	exps, err := domain.OpenExpectations(s.Store, s.Cfg.SiteID)
	if err != nil {
		return nil, err
	}
	// One scan, reused. alertOf is empty exactly when the scanner is not
	// wired (web-layer tests) — every row then reads "NO TIMER", which is
	// the honest answer rather than an invented countdown.
	alertOf := map[string]deadline.Alert{}
	if s.Alerts != nil {
		board, err := s.Alerts.Evaluate()
		if err != nil {
			// Loud, never fatal: the gate must still be able to check a
			// vehicle in when the scan fails. A row with no verdict says so.
			log.Printf("gate: inbound list: deadline scan: %v", err)
		} else {
			for _, a := range board.Alerts {
				alertOf[a.TripID] = a
			}
		}
	}

	var rows []inboundVehicleRow
	for _, e := range exps {
		row := inboundVehicleRow{
			// ⟦James, 2026-10-03 — DR-W7-18 / W7-36(a)⟧ the gate inbound row
			// composes the truck LIVE, as the alert strip and the origin's sheet do.
			// ⟦was: "Vehicle: tripVehicleDisplay(e.Trip.VehicleIsUnidentified(), e.Trip.VehicleEntered)"⟧
			TripID: e.Trip.TripID, Vehicle: e.Vehicle,
			Origin: e.Trip.OriginSite, Driver: e.DriverName,
			Occupants: append([]domain.OccupantRef(nil), e.OccupantList...),
			Due:       "—", Urgency: "none", UrgencyLabel: "NO TIMER",
		}
		sortOccupantRefs(row.Occupants)
		if row.Driver == "" && e.Trip.DriverPersonID != "" {
			// Show the id rather than an empty cell: empty reads as "no
			// driver was recorded", a different and much worse claim than
			// "this box cannot name them yet".
			row.Driver = e.Trip.DriverPersonID
		}
		if e.Trip.DeadlineMinutes != nil {
			row.Due = formatMinutes(*e.Trip.DeadlineMinutes)
		}
		if a, ok := alertOf[e.Trip.TripID]; ok {
			applyAlertFace(&row, a)
			// Q-FE2: the recorded due time, on the origin's clock, by its
			// display name — "due about 11:07 by Ice Road's clock".
			if due := s.alertDueText(a); due != "" && a.Status == deadline.StatusOverdueUnresolved {
				row.Countdown += "; " + due
			}
		}
		rows = append(rows, row)
	}
	sortInboundRows(rows)
	return rows, nil
}

// sortInboundRows: the row that needs a human first goes first — the same
// ordering the console's board uses, through the same urgencyRank, so an
// officer moving between the two screens sees one order and not two. Within
// a band by `remaining`: soonest due first, and (T-005) latest overdue first.
func sortInboundRows(rows []inboundVehicleRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		ri, rj := urgencyRank(rows[i].Urgency), urgencyRank(rows[j].Urgency)
		if ri != rj {
			return ri < rj
		}
		if rows[i].remaining != rows[j].remaining {
			return rows[i].remaining < rows[j].remaining
		}
		return rows[i].TripID < rows[j].TripID
	})
}

// formatHM ⟦R-28 revision 3, team-lead, 2026-09-16⟧ item 1: "due in 1h
// 26m", "OVERDUE 12m" — rounded to the MINUTE (never seconds: a second-
// by-second figure invites an officer to trust a precision the sync
// interval and human timing cannot deliver) and spaced ("1h 26m", not Go's
// own unspaced "1h26m"), with the hour segment dropped entirely under an
// hour rather than printed as "0h 12m". a.RemainingText()/a.OverdueByText()
// (deadline/scan.go) round to the SECOND for a different, still-valid
// purpose (the alert log's own precision) and are deliberately not reused
// here — this is the gate row's own derivation, not a truncation of theirs.
func formatHM(d time.Duration) string {
	d = d.Round(time.Minute)
	h := d / time.Hour
	m := (d % time.Hour) / time.Minute
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// applyAlertFace copies the C7 scan's verdict onto a row. It is a
// TRANSLATION of deadline.Status into the row's fields and adds no judgement
// of its own — every string below either comes from the alert or names the
// status the scan assigned.
//
// The four cases are the same four the console's board renders
// (web/trips.go, handleTripsPage). They are written out again here rather
// than shared because the two rows carry different field sets and a shared
// helper would have to take both — but they must not DIVERGE: if a fifth
// deadline.Status is ever added, both switches need the case, and the
// default below is what makes a missing case visible instead of silent.
func applyAlertFace(row *inboundVehicleRow, a deadline.Alert) {
	switch a.Status {
	case deadline.StatusCountdown:
		row.Urgency, row.UrgencyLabel = "due", "DUE IN"
		row.Countdown = "due in " + formatHM(a.Remaining)
		row.remaining = int64(a.Remaining)
	case deadline.StatusOverdueLive:
		row.Urgency, row.UrgencyLabel = "overdue", "OVERDUE"
		row.Countdown = "OVERDUE " + formatHM(a.OverdueBy)
		row.Open = true
		// T-005 (L07): within the OVERDUE band the LATEST truck reads first —
		// the most negative "remaining" sorts first, the same key the "due"
		// band already sorts on, so one comparator orders both bands.
		// ⟦was: remaining left at 0 for every overdue row, so the band fell
		// through to trip-id order⟧
		row.remaining = -int64(a.OverdueBy)
	case deadline.StatusOverdueUnresolved:
		// No numeral exists for this state: the deadline passed while the
		// link was down, so there IS no honest figure. The sentence says
		// that; a number here would be a fabrication.
		// Q-FE2 (James, 2026-10-04): the scan's own label — a row this
		// post cannot count says so and never says OVERDUE. ⟦was: always
		// "OVERDUE — UNRESOLVED"⟧
		row.Urgency, row.UrgencyLabel = "unresolved", a.ReviewLabel()
		row.Countdown = "overdue-unresolved — verify by radio"
		if a.DeadlineUnknown && !a.ClockChanged {
			row.Countdown = "no receipt timing at this post — verify by radio"
		}
		if a.ClockChanged { // R9, James 2026-10-04: never a fabricated OVERDUE
			row.Countdown = deadline.ClockChangedNote
		}

		row.Open = true
	default:
		// Any status added later: the row keeps "NO TIMER" rather than
		// inventing a verdict. ⟦was: "Includes StatusPhaseReview, which lives
		// in Board.PhaseReview …" — weather phases removed⟧
	}
	if a.Acked {
		row.Countdown += " (acknowledged)"
	}
	row.StalenessFace = a.StalenessFace
}

// formatMinutes renders an authored journey allowance. Its own function so
// the gate list and anything else that shows an allowance cannot drift into
// two spellings of the same number.
func formatMinutes(m int64) string {
	if m == 1 {
		return "1 min"
	}
	return itoa(m) + " min"
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// --- the arrival disposition -------------------------------------------

// arrivalDisposition reads the gate arrival form's per-person control and
// returns the ids to record as STAYED BEHIND — the set the domain's
// existing RecordExpectedArrivalExcluding takes.
//
// THE FORM SHAPE, and why it is this way round (James, 2026-08-21: "a
// checkbox that defaults to checked, and a 'check in all checked'"):
//
//	manifest_person_ids   hidden, one per occupant. ALWAYS submitted. This
//	                      is what the form OFFERED — without it the server
//	                      cannot tell "nobody was ticked" from "no control
//	                      was rendered".
//	arriving_person_ids   the ticked boxes. Ticked = this person is coming
//	                      in. Server-rendered `checked`, so the default
//	                      holds with scripting off.
//
// stayed = manifest − arriving.
//
// SAFE DEFAULT, DERIVED FOR THIS ACTION rather than copied from any widget
// on any other page — reusing a control by analogy is how this project
// shipped an unguarded surface once already.
//
//	An arrival ADDS people to the muster.
//	* A person wrongly INCLUDED is recorded on site when they got off at a
//	  turnoff: the muster OVER-reports, somebody is searched for who is not
//	  there. Wasted search time. SAFE.
//	* A person wrongly EXCLUDED is physically standing here with no record
//	  at all — absent from the evacuation sheet, and nothing downstream will
//	  ever flag it. DANGEROUS, and a D-6 failure.
//	Therefore the default must be INCLUDED, and it must be included without
//	the operator doing anything. Ticked-by-default is that. It must never be
//	inverted to "tick each person to admit them": a forgotten tick would then
//	omit somebody who is physically present, which is the direction that gets
//	a person left behind.
//
// WHY THE INVERSION WAS WORTH REMOVING. The console's form has the same safe
// direction by the opposite means — an UNticked box means "coming in" and
// ticking one excludes. Both defaults are safe; only this one's control
// STATE matches its EFFECT. Under time pressure at -30 a box beside a name
// reads as "this person", and the console makes it mean "not this person".
//
// WHY THE FAILURE MODE IS STILL LOUD. If every box somehow rendered
// unticked, stayed would be the whole manifest — and the domain REFUSES
// that outright ("an arrival with nobody aboard is not an arrival"), so the
// operator gets an error instead of an arrival that silently checked in
// nobody. If the hidden manifest is missing entirely, this returns nil and
// the arrival checks EVERYONE in, which is the over-reporting direction.
// Both degradations fall on the safe side, by construction, and
// TestTheGateArrivalControlIsTickedInTheServerRenderedMarkup pins the
// default itself.
//
// WHERE AN UNTICKED PERSON GOES, and it is not a new category: nowhere new.
// They get no visit here, so they stay checked out of the origin, and
// domain.NotTracked already names exactly this shape — "left on a vehicle
// and did NOT arrive with it, whereabouts NOT recorded". They appear in the
// muster's NOT TRACKED section, which states the uncertainty rather than
// letting anyone type a location the system has no basis for. If the officer
// KNOWS they got off at a pad, that is a different record (a run to that
// pad), not a location typed into this form.
func arrivalDisposition(r *http.Request) []string {
	manifest := r.Form["manifest_person_ids"]
	if len(manifest) == 0 {
		// The console's older exclusive form, which submits the stayed set
		// directly and no manifest. Both shapes reach the same domain call;
		// only ONE is ever rendered on a given page, so no operator sees two
		// meanings of a checkbox on one screen.
		return r.Form["stayed_person_ids"]
	}
	arriving := map[string]bool{}
	for _, id := range r.Form["arriving_person_ids"] {
		arriving[id] = true
	}
	var stayed []string
	for _, id := range manifest {
		if !arriving[id] {
			stayed = append(stayed, id)
		}
	}
	return stayed
}

// --- the one-click result line (R-28) -----------------------------------

// arrivedVehicleQueryKey / arrivedCheckedQueryKey carry the just-completed
// arrival's own result across the redirect, the same warn-accept shape as
// notEnteredQueryKey above (query values, resolved on the NEXT render, never
// trusted as a claim about current state — the roster itself is always read
// fresh from the store, same as every other page). ⟦G-126, 2026-10-03⟧ ⟦was:
// "a stale or hand-edited query value only produces a wrong RESULT LINE"⟧ —
// it no longer produces one at all: the line renders only off a redirect
// sealed by this process (noticeseal.go).
const (
	arrivedVehicleQueryKey = "arrived_vehicle"
	arrivedCheckedQueryKey = "arrived_checked"
)

// arrivedNotice is one arrival's result line — "TRK-14 arrived: 10 checked
// in". The held-out half of that line ("2 held out — need orientation:
// ...") is NOT carried here: it reuses NotEntered/notEnteredNoticesFrom
// above verbatim (same query key, same resolution against the roster), so
// there is exactly one mechanism for "who was held out", not two that could
// disagree.
type arrivedNotice struct {
	Vehicle string
	Checked int
}

// arrivedRedirect appends this arrival's vehicle/count to a redirect
// target, chainable with notEnteredRedirect/timeWarnRedirect/dupWarnRedirect
// exactly as they chain with each other.
func arrivedRedirect(target, vehicle string, checked int) string {
	q := url.Values{}
	q.Set(arrivedVehicleQueryKey, vehicle)
	q.Set(arrivedCheckedQueryKey, strconv.Itoa(checked))
	sep := "?"
	if strings.Contains(target, "?") {
		sep = "&"
	}
	return sealNotices(target + sep + q.Encode()) // G-126: the commit token
}

// arrivedNoticeFrom reads the result line off a request's query. Absent
// vehicle key = no notice (nil) — the ordinary render of every OTHER visit
// to this page, not just the one right after a one-click arrival. A
// malformed/missing count reads as 0 rather than refusing the render: this
// is display only, never a write.
func arrivedNoticeFrom(r *http.Request) *arrivedNotice {
	q := noticeQuery(r) // G-126: "FAKE-99 arrived: 42 checked in" off a typed URL renders nothing
	v := q.Get(arrivedVehicleQueryKey)
	if v == "" {
		return nil
	}
	checked, _ := strconv.Atoi(q.Get(arrivedCheckedQueryKey))
	return &arrivedNotice{Vehicle: v, Checked: checked}
}
