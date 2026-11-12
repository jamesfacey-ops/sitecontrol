// timecapture.go — TP §4.5's explicit-signal time capture and TP §4.6's
// bounds, for every takes-input authoring surface (W2; TP §4.8 rows 1–8).
//
// THE SIGNAL IS EXPLICIT, NEVER DERIVED. Every takes-input form posts
// `time_source` — "default" (the officer left the time control alone) or
// "entered" (they opened it and typed a wall time into `occurred_at`). The
// server NEVER infers the signal from the values: a POST with no
// `time_source` field at all is REFUSED 400 (a stale pre-bump tab, or raw
// curl), because the natural fallback — compare the echoed value against
// now and guess — is exactly the equality-derivation TP §4.5 forbids at
// both ends. On the default path the echoed `occurred_at` value (the
// render instant) is ignored here and never stored: store.Author's rule W
// stamps OccurredAt := AuthoredAt from its own single clock reading.
//
// THE BOUNDS (TP §4.6) apply to ENTERED values ONLY — the default path is
// never bounded ("authoring is never refused for a clock reason", schema-10
// §5B.4 rule 1: a wrong clock must never refuse default-path authoring).
// Three bounds, one refusing and two warning, none ever silently adjusting:
//
//  1. FUTURE, conditioned on clock_state: an entered value more than
//     FUTURE_SKEW_ALLOWANCE ahead of this box's now is REFUSED with the
//     reason shown — ONLY when clock_state == synced. When the clock is
//     unsynced/unverified the same value WARNS AND ACCEPTS: a slow RTC
//     makes a TRUE entry future-dated relative to the box, and refusing
//     there refuses the truth. Within the allowance: accepted silently.
//  2. RECORDS FLOOR, warning only, comparator DEFINED (never improvised):
//     fires iff entered < min(authored_at) over the local event_log
//     (store.MinAuthoredAtWallMS), suppressed on an empty log — no
//     records, no floor.
//  3. ABSURD AGE, warning only: older than the 30-day horizon warns and
//     accepts.
//
// Accepted values — warned or not — are stored BYTE-EXACTLY as parsed;
// there is no clamp anywhere in this file, by design and by test (W2-4.7).
//
// WARNINGS RENDER ON THE PAGE THE OPERATOR SEES NEXT. The accept paths are
// post-redirect-GET, so a warning is carried as a `tw` query code on the
// redirect Location and rendered by the target page as a visible
// `time-warning` notice — never dropped by the redirect, never stored.
//
// CONSTANTS are S-5's recorded design defaults (TP §8 Q4), named so a
// re-valuing is one edit — never magic numbers.
//
// THE FORGIVING SHAPES (design/ACCESS-APP-LAYOUT.md §2's `frmCheckIn`
// forgiving HHMM parser, requested as a follow-up to the default gate page
// — reports/witness/DEFAULT-GATE-PAGE.md item 8) widen what `occurred_at`
// accepts beyond the strict datetime-local wire string: a bare clock time
// applied to TODAY in this box's zone, so an officer can type `600`,
// `6am`, or `6:30pm` instead of stepping through a picker's date segment
// for an event that plainly happened today. parseWallTime tries the exact
// wire format FIRST — nothing that worked before stops working — and only
// then tries the forgiving shapes. Every shape it accepts is UNAMBIGUOUS
// under a stated rule (see parseForgivingClock); anything else is refused
// with the accepted-forms list quoted back, never guessed. This is an
// insurance-facing record: a wrong guess here is a wrong time in the log
// forever, so refusing a typo costs five seconds and guessing costs the
// record's credibility.
package web

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"sitecontrol/internal/store"
)

const (
	// timeSourceField is TP §4.5's posted signal — the name is DOC-fixed.
	timeSourceField = "time_source"
	// occurredAtField carries the typed wall string on the entered path.
	occurredAtField = "occurred_at"
	// wallTimeLayout is datetime-local's wire form, interpreted in this
	// box's local zone (site-local per TP §4.1 — the box's zone is the
	// site's zone) and stored as UTC ms.
	wallTimeLayout = "2006-01-02T15:04"

	// timeFormatHelp is the accepted-forms list, quoted back in every
	// refusal so an officer sees exactly what this box will take, not just
	// that it failed.
	timeFormatHelp = `a date and time like 2026-01-31T14:05, or just today's time like 600, 0600, 6am, 6:30pm, or noon`

	// FUTURE_SKEW_ALLOWANCE (TP §4.6 rule 1; S-5 recorded default).
	futureSkewAllowanceMS = int64(5 * 60 * 1000)
	// The absurd-age horizon (TP §4.6 rule 3; S-5 recorded default).
	absurdAgeHorizonMS = int64(30 * 24 * 60 * 60 * 1000)
)

// Warning codes carried on the redirect (the query value is a fixed code,
// never free text — the target page maps it back to its copy).
const (
	timeWarnQueryKey = "tw"
	timeWarnFloor    = "floor"
	timeWarnAge      = "age"
	// timeWarnOrder ⟦FIX17b, sc-stress F12⟧: a time CORRECTION that puts an
	// end before its start (time_correct.go). Warn-accept, like every other
	// bound on a corrected instant (W4: the correction POST appends).
	timeWarnOrder     = "order"
	timeWarnClockSkew = "clock"
)

// timeCapture is one POST's parsed time claim, ready to hand to a domain
// authoring call as its (occurredAtMS, timeSource) pair.
type timeCapture struct {
	// OccurredAtMS is the parsed typed value on the entered path, byte
	// exact; 0 (ignored by rule W) on the default path.
	OccurredAtMS int64
	// TimeSource is the posted signal: "default" or "entered".
	TimeSource string
	// Wall is the typed wall string exactly as posted (entered only), so a
	// refusal can re-render it intact (TP §4.6 rule 6).
	Wall string
	// Warnings are the warn-accept codes the NEXT page must render.
	Warnings []string
}

// timeCaptureError is a capture/bounds refusal: an HTTP status and an
// operator-readable reason. Nothing has been authored when one is returned.
//
// THE COPY IS PER-SURFACE where the surfaces differ in fact (F-1). The nine
// bodies deliver a refusal two different ways: the gate check-in bodies
// re-render the whole posted form with every field and the typed time
// intact, while the captureTimeOrRefuse routes write a plain http.Error
// wall and the posted work is discarded. A single message cannot be true on
// both, so each refusal carries the copy for each surface: msg (the plain
// wall — must never claim any typed work was kept) and formMsg (the
// re-render — may promise exactly what the re-render delivers). The clauses
// true everywhere — it will not guess, nothing has been recorded — are
// shared verbatim. Neither variant says "reload": reloading a POST result
// re-sends the same refused submission.
type timeCaptureError struct {
	status int
	// msg is the refusal as delivered on the plain http.Error surfaces
	// (/gate/checkout, /gate/resume, the four /trips/* routes).
	msg string
	// formMsg, when non-empty, is the same refusal for the surfaces that
	// re-render the posted form with all state intact (the gate check-in
	// bodies). Empty means msg is true there too.
	formMsg string
}

func (e *timeCaptureError) Error() string { return e.msg }

// renderMessage is the copy for a surface that re-renders the posted form
// with every field and the typed time intact.
func (e *timeCaptureError) renderMessage() string {
	if e.formMsg != "" {
		return e.formMsg
	}
	return e.msg
}

// clockReading takes THE bounds-side clock reading, from the same injected
// clock store.Author stamps from.
func (s *Server) clockReading() store.ClockReading {
	clock := s.Store.Clock
	if clock == nil {
		clock = store.SystemClock
	}
	return clock()
}

// captureTime reads TP §4.5's explicit signal off a parsed form and, on the
// entered path, applies TP §4.6's bounds. It authors nothing and adjusts
// nothing: the value comes back exactly as parsed, refused, or warn-accepted.
func (s *Server) captureTime(r *http.Request) (timeCapture, *timeCaptureError) {
	switch ts := r.FormValue(timeSourceField); ts {
	case "":
		// The missing signal. Never inferred from the values — that is the
		// forbidden derivation (TP §4.5); the sender is a stale pre-bump
		// tab or a raw POST, and it is refused whole.
		const noSignalCore = "This submission carries no time_source signal, so this box cannot tell whether the time was left on the default or entered by hand — and it will not guess. Nothing has been recorded."
		return timeCapture{}, &timeCaptureError{status: http.StatusBadRequest,
			msg:     noSignalCore + " Nothing typed here was kept — this page cannot show it back. Reopen the page it was typed on and enter the work again. Do not reload this error page: reloading re-sends the same refused submission.",
			formMsg: noSignalCore + " Everything typed here is shown below exactly as it arrived — check it over and submit it again."}
	case "default":
		// The officer left the control alone. Any echoed occurred_at value
		// is the render instant and is deliberately ignored — rule W stamps
		// the stored time from the store's own single clock reading.
		return timeCapture{TimeSource: "default"}, nil
	case "entered":
		wall := strings.TrimSpace(r.FormValue(occurredAtField))
		if wall == "" {
			return timeCapture{}, &timeCaptureError{status: http.StatusBadRequest,
				msg: "The time was marked as entered by hand, but no time arrived with it. Nothing has been recorded. Type the time, or switch back to the current time."}
		}
		return s.captureEnteredWall(wall)
	default:
		// An unrecognised signal from this box's own request surface is a
		// malformed POST, refused rather than stored or normalised — the
		// store's no-write-side-normalisation rule governs bytes a caller
		// DOES author; this caller never gets that far.
		return timeCapture{}, &timeCaptureError{status: http.StatusBadRequest,
			msg: "This submission carries an unrecognised time_source signal. Nothing has been recorded."}
	}
}

// captureEnteredWall is the entered path for one typed wall string: parse
// (strict wire form first, then the forgiving shapes) and TP §4.6's bounds.
// captureTime's own "entered" branch and ⟦FIX8⟧ each roster row's own Time
// out box (rowTimeOut, gate_rowtime.go) share it, so a per-person time is
// held to exactly the rules a form-level one is — never a second set.
func (s *Server) captureEnteredWall(wall string) (timeCapture, *timeCaptureError) {
	siteNow := time.UnixMilli(s.clockReading().WallMS).In(time.Local)
	t, err := parseWallTime(wall, siteNow)
	if err != nil {
		return timeCapture{}, &timeCaptureError{status: http.StatusBadRequest,
			msg: "The entered time \"" + wall + "\" could not be read as " + timeFormatHelp + ". Nothing has been recorded; nothing was adjusted or guessed — correct it and submit again."}
	}
	tc := timeCapture{TimeSource: "entered", OccurredAtMS: t.UnixMilli(), Wall: wall}
	if refusal := s.timeBounds(&tc); refusal != nil {
		return timeCapture{}, refusal
	}
	return tc, nil
}

// --- the forgiving shapes -----------------------------------------------

// reClockColon matches H:MM or HH:MM, with an optional am/pm suffix —
// "6:30", "18:05", "6:30pm", "6:30 pm". The minute is always exactly two
// digits: "6:5" is refused rather than guessed as 6:05 or 6:50.
var reClockColon = regexp.MustCompile(`^(\d{1,2}):(\d{2})\s*([ap]m)?$`)

// reClockHourAmPm matches a bare hour with an am/pm suffix and no colon —
// "6am", "12 pm". Never matches without the suffix: a bare "6" is the
// digits-only shape below, not this one.
var reClockHourAmPm = regexp.MustCompile(`^(\d{1,2})\s*([ap]m)$`)

// reClockDigits matches 1-4 bare digits — the Access-style typed shapes
// "6", "18", "600", "0600", "1800".
var reClockDigits = regexp.MustCompile(`^\d{1,4}$`)

// parseWallTime interprets a typed wall-time string. It tries the exact
// datetime-local wire format FIRST (nothing that worked before stops
// working), then the forgiving clock shapes, which carry no date of their
// own and apply to siteNow's date — site-local "today", exactly like the
// Access checkin app's Date() default (design/ACCESS-APP-LAYOUT.md §2).
// Anything not unambiguously one of these shapes is refused.
func parseWallTime(raw string, siteNow time.Time) (time.Time, error) {
	if t, err := time.ParseInLocation(wallTimeLayout, raw, time.Local); err == nil {
		return t, nil
	}

	s := strings.Join(strings.Fields(strings.ToLower(raw)), " ")
	hour, minute, ok := parseForgivingClock(s)
	if !ok {
		return time.Time{}, fmt.Errorf("timecapture: %q is not a recognised time", raw)
	}
	y, m, d := siteNow.Date()
	return time.Date(y, m, d, hour, minute, 0, 0, time.Local), nil
}

// parseForgivingClock reads one of the Access-parity forgiving shapes and
// returns the 24-hour hour/minute it names. Every branch is a STATED,
// unambiguous rule — never a guess between two plausible readings:
//
//   - "noon" / "midnight" — 12:00 / 00:00.
//   - H[:MM] with an am/pm suffix — 12-hour, so the hour must be 1-12
//     ("13pm" refuses: am/pm only names hours 1-12). By the ordinary
//     12-hour convention, 12am is midnight (00:00) and 12pm is noon
//     (12:00) — surprising in casual speech but the one reading every
//     clock and library already agrees on, so it is applied rather than
//     refused.
//   - H:MM with no suffix — 24-hour; hour 0-23, minute 0-59.
//   - 1-2 bare digits — the HOUR alone, 24-hour ("6" -> 06:00, "18" ->
//     18:00); there is no minute component to this shape, so none is
//     guessed.
//   - 3 bare digits — H:MM, the FIRST digit is the hour and the last two
//     are the minute ("130" -> 01:30, "600" -> 06:00). This is the same
//     split a 4-digit HHMM uses one digit narrower; it is never read as a
//     2-digit hour ("130" is never 13:0), because that would need a
//     digit this shape does not have.
//   - 4 bare digits — HHMM, hour 0-23, minute 0-59 ("1800" -> 18:00,
//     "2500" refuses: hour 25 does not exist).
//
// Anything else — "6x30", "99:99", a bare "6:5" — is refused: it is not
// that these are rare, it is that more than one reading fits, and this is
// an insurance-facing record where a wrong guess is a wrong time kept
// forever. A refusal costs the operator five seconds; a guess costs the
// record's credibility.
func parseForgivingClock(s string) (hour, minute int, ok bool) {
	switch s {
	case "noon":
		return 12, 0, true
	case "midnight":
		return 0, 0, true
	}

	if m := reClockColon.FindStringSubmatch(s); m != nil {
		h, _ := strconv.Atoi(m[1])
		min, _ := strconv.Atoi(m[2])
		if min > 59 {
			return 0, 0, false
		}
		if m[3] != "" {
			return applyAmPmHour(h, m[3]), min, h >= 1 && h <= 12
		}
		if h > 23 {
			return 0, 0, false
		}
		return h, min, true
	}

	if m := reClockHourAmPm.FindStringSubmatch(s); m != nil {
		h, _ := strconv.Atoi(m[1])
		if h < 1 || h > 12 {
			return 0, 0, false
		}
		return applyAmPmHour(h, m[2]), 0, true
	}

	if reClockDigits.MatchString(s) {
		switch len(s) {
		case 1, 2:
			h, _ := strconv.Atoi(s)
			if h > 23 {
				return 0, 0, false
			}
			return h, 0, true
		case 3:
			h, _ := strconv.Atoi(s[:1])
			min, _ := strconv.Atoi(s[1:])
			if min > 59 {
				return 0, 0, false
			}
			return h, min, true
		case 4:
			h, _ := strconv.Atoi(s[:2])
			min, _ := strconv.Atoi(s[2:])
			if h > 23 || min > 59 {
				return 0, 0, false
			}
			return h, min, true
		}
	}

	return 0, 0, false
}

// applyAmPmHour converts a 1-12 hour under an "am"/"pm" suffix to 24-hour.
// 12am is midnight (0); 12pm stays noon (12) — the ordinary convention.
func applyAmPmHour(h int, suffix string) int {
	if suffix == "am" {
		if h == 12 {
			return 0
		}
		return h
	}
	if h == 12 {
		return 12
	}
	return h + 12
}

// timeBounds applies TP §4.6's three bounds to one entered value, filling
// tc.Warnings for the warn-accept shapes and returning a refusal only for
// the one refusing bound (future beyond allowance on a SYNCED clock). The
// value itself is never touched.
func (s *Server) timeBounds(tc *timeCapture) *timeCaptureError {
	reading := s.clockReading()

	// Bound 1 — the future bound, conditioned on clock_state.
	if tc.OccurredAtMS > reading.WallMS+futureSkewAllowanceMS {
		if reading.State == store.ClockSynced {
			futureCore := "The entered time " + tc.Wall + " is more than 5 minutes ahead of this box's clock, and this box's clock is synchronised — a future time cannot have been observed yet. Nothing has been recorded and nothing was adjusted"
			return &timeCaptureError{status: http.StatusConflict,
				msg:     futureCore + " — but nothing typed here was kept either: reopen the page it was typed on and enter the work again with a corrected time (or, if the event really is in the near future, wait and record it when it happens).",
				formMsg: futureCore + ": everything typed is still on this page. Correct the time (or, if the event really is in the near future, wait and record it when it happens)."}
		}
		// The identical value under an untrusted clock is warn-accepted: a
		// slow RTC makes a TRUE entry future-dated relative to this box,
		// and refusing there refuses the truth.
		tc.Warnings = append(tc.Warnings, timeWarnClockSkew)
	}

	// Bound 2 — the records floor: warning only, DEFINED comparator,
	// suppressed on an empty log. A read failure here is logged and skipped
	// rather than refused: the floor is an audit-side warning, and
	// authoring is never refused for a clock (or a warning-plumbing)
	// reason.
	if min, ok, err := s.Store.MinAuthoredAtWallMS(); err != nil {
		log.Printf("time bounds: records floor read failed (warning skipped, value untouched): %v", err)
	} else if ok && tc.OccurredAtMS < min {
		tc.Warnings = append(tc.Warnings, timeWarnFloor)
	}

	// Bound 3 — absurd age: warning only.
	if tc.OccurredAtMS < reading.WallMS-absurdAgeHorizonMS {
		tc.Warnings = append(tc.Warnings, timeWarnAge)
	}
	return nil
}

// captureTimeOrRefuse is captureTime for the handlers whose refusal posture
// is the plain http.Error surface (the trips console, the ticked check-out
// list, resume). It writes the refusal itself and reports ok=false; the
// gate's form-heavy bodies handle their own refusals so the accumulated
// list re-renders intact (TP §4.6 rule 6).
func (s *Server) captureTimeOrRefuse(w http.ResponseWriter, r *http.Request) (timeCapture, bool) {
	tc, cerr := s.captureTime(r)
	if cerr != nil {
		http.Error(w, cerr.msg, cerr.status)
		return timeCapture{}, false
	}
	return tc, true
}

// timeWarnRedirect appends any warn-accept codes to a redirect target so
// the page the operator sees next renders them — a warning that dies with
// the redirect is a warning nobody was shown.
func timeWarnRedirect(target string, tc timeCapture) string {
	if len(tc.Warnings) == 0 {
		return target
	}
	q := url.Values{}
	for _, code := range tc.Warnings {
		q.Add(timeWarnQueryKey, code)
	}
	sep := "?"
	if strings.Contains(target, "?") {
		sep = "&"
	}
	return sealNotices(target + sep + q.Encode()) // G-049: the commit token
}

// timeWarningCopy maps one redirect code to the notice the operator reads.
// Unknown codes render nothing: the query string is request surface, not a
// message channel.
func timeWarningCopy(code string) string {
	switch code {
	case timeWarnFloor:
		// "predates" is TP §4.6 rule 2's own quoted copy.
		return "The entered time predates this box's records — it is earlier than anything this box has ever recorded. It was accepted and stored exactly as typed; check the date if that is not what was meant."
	case timeWarnAge:
		return "The entered time is more than 30 days in the past. It was accepted and stored exactly as typed; check the date if that is not what was meant."
	case timeWarnOrder:
		return "This correction puts an end before its start — a check-out before that person's check-in, a check-in after their check-out, or an arrival before the run's departure — so that visit or run now reads as negative time in the reports. It was accepted and stored exactly as typed; correct the other time too if this is not what happened."
	case timeWarnClockSkew:
		return "The entered time is further ahead of this box's clock than the allowance, and this box's clock is currently not trusted (unsynced or unverified) — so the record was accepted and stored exactly as typed. Verify the time: the clock may be wrong, or the entry may be."
	default:
		return ""
	}
}

// timeWarningsFrom reads the warn codes off a request's query and returns
// the rendered copies, for the pages the accept paths redirect to.
func timeWarningsFrom(r *http.Request) []string {
	var out []string
	for _, code := range noticeQuery(r)[timeWarnQueryKey] { // G-049: sealed redirects only
		if msg := timeWarningCopy(code); msg != "" {
			out = append(out, msg)
		}
	}
	return out
}
