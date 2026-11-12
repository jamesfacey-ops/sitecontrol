// gate_signin.go — the sign-in date and time on each on-site roster row
// (James, 2026-09-10: "the date of sign in should be displayed on the onsite
// roster as well"; and on whose clock it is: "that is a moot point use the
// system clock").
//
// SHOWN PLAINLY, time first and date second — James: "time/date" — as
// `hh:nn mm/dd`, 24-hour, no seconds — because the roster he reads today
// shows `hh:nn` alone, and 13 % of visits close on a later calendar day
// (measured in his file; design/ACCESS-APP-LAYOUT.md). The value is the recorded instant
// (domain.VisitSignInTimes: the recording box's system clock at the
// check-in, the officer's typed time on the entered path, or a correction
// that won), rendered in this box's zone. No caveat, no tooltip, no
// distinction between a row this box recorded and one a previous box did —
// ruled out.
//
// ⛔ NOTHING HERE READS A CLOCK AND NOTHING HERE ORDERS. The only time on
// this path is the recorded instant coming out of the store; time.Local is
// a zone, not a reading. buildOnSiteRows sorted by name before this ran and
// the picker's groupBuilder sorts by name again after it; attachSignIns
// writes one display string per row in place (D-90/D-92, TP §4.4).
package web

import (
	"time"

	"sitecontrol/internal/domain"
)

// signInLayout is the officer-facing form: time first, date second — James:
// "time/date" — `hh:nn mm/dd`.
const signInLayout = "15:04 01/02"

// signInUnavailable is what a row shows when this store cannot produce the
// instant (no arrival event behind the visit, or a correction chain that
// cannot be resolved — see domain.VisitSignInTimes). A dash, never a guess;
// the name and the count are unaffected.
const signInUnavailable = "—"

// decorateSignIns attaches the sign-in text to every row. Returns the
// operator-facing reason if the read failed as a whole; the rows are then
// left without a date and the page says why. Loud on failure, never fatal
// to the gate: a truck at the window must still be entered.
func (s *Server) decorateSignIns(rows []onSiteRow, visits []domain.Visit) string {
	times, err := domain.VisitSignInTimes(s.Store, s.Cfg.SiteID, visits)
	if err != nil {
		return refusalText(err)
	}
	attachSignIns(rows, times, time.Local)
	return ""
}

// attachSignIns writes each row's sign-in text in place. It never reorders
// rows and reads nothing but the instant recorded for that row's visit.
func attachSignIns(rows []onSiteRow, times map[string]int64, zone *time.Location) {
	for i := range rows {
		ms, ok := times[rows[i].VisitID]
		if !ok {
			rows[i].SignedIn = signInUnavailable
			continue
		}
		rows[i].SignedIn = time.UnixMilli(ms).In(zone).Format(signInLayout)
	}
}
