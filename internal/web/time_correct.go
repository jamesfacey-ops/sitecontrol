// time_correct.go — the officer-facing time-correction surface (W4,
// item 14; TP §4.8's fourteenth surface). One screen: name a recorded
// event by its log key, type what actually happened, sign it.
//
// THE TYPED INSTANT IS PAYLOAD, NEVER ENVELOPE (D-44): this form carries
// NO time_source field — overriding the correction act's own timestamp is
// declined UI (VF §5), so the act's envelope pair is always
// default-captured by the domain edge, and the corrected instant rides the
// event payload only. The route therefore classifies EXEMPT with a written
// reason in the W2 route-walk's table (TP §4.8 row 10's payload-instant
// precedent), never takes-input.
//
// TP §4.6's BOUNDS fire on the payload instant unconditionally (W4-5),
// through the same produced machinery every entered value goes through
// (timeBounds): future-beyond-allowance refuses on a synced clock and
// warn-accepts on an untrusted one; the records floor and the absurd-age
// horizon warn and never block; an accepted instant is stored
// byte-faithfully — no clamp, no nudge, no discard — and a refusal
// re-renders with everything typed intact.
//
// The POST appends (a losing correction is recorded and superseded, never
// refused — VF §5) and follows the house PRG pattern: 303 back to this
// surface with ?recorded=<site_id>.<epoch>.<seq>; the GET computes the
// outcome AT RENDER TIME through the resolver — read-time truth, never
// POST-time cache. Four renderings (VF §5): terse success; the winning
// post's supersession notice naming what it beat (with the officer suffix
// when the superseded correction's recorded_by is non-empty); the losing
// post's adverse-in-words notice with the radio-through recovery sentence;
// and the loud could-not-be-determined rendering for a garbled ?recorded=
// or a resolver error — never a blank block, never the terse success line
// beside it (the intake.go precedent of silently ignoring an unmatched
// query key is explicitly not followed here).
//
// This handler never reads the correction side table or builds a payload:
// it calls the domain edge and the domain resolver, the two sanctioned
// paths (the VF §7.6 consumer scan holds that mechanically).
package web

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

func (s *Server) registerTimeCorrectRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /time/correct", s.requireLogin(s.handleTimeCorrectPage))
	mux.HandleFunc("POST /time/correct", s.requireLogin(s.handleTimeCorrect))
}

// timeCorrectPeer is one superseded competitor on the winning rendering.
type timeCorrectPeer struct {
	Post       string // config-derived display name of the authoring post
	Wall       string // its asserted instant, site-local wall time
	RecordedBy string // its officer; rendered only when non-empty
}

// timeCorrectOutcome is the confirmation block, computed at render time.
type timeCorrectOutcome struct {
	// NotACorrection ⟦T-199, W4 2026-10-03⟧: the ?recorded= key names an
	// event at this post that is NOT a time correction (a check-in, a
	// dispatch …). Nothing was recorded by this page, so it says
	// so instead of "Recorded and effective"; the key is echoed, escaped,
	// so the officer can see what the link named.
	NotACorrection bool
	Key            string
	// Failed: VF §5 rendering 4 — the outcome could not be determined. The
	// append DID succeed; what failed is the read-back, and the officer
	// must know the confirmation is not vouching for the outcome.
	Failed bool
	// Superseded: rendering 3 — the recorded correction lost.
	Superseded bool
	WinnerPost string // the post whose correction stands
	// ⟦L-1's class, fifth instance⟧ WinnerAtThisPost: the correction that
	// stands was authored HERE, by a later correction at this same post —
	// so there is nobody to radio. A SIGNAL, not a name: peerDisplayName
	// answers for this post's own site id with its configured display name
	// ("Ice Road A"), which reads as a different post and is exactly what
	// made the recovery sentence impossible to follow. A separate bool
	// rather than a special-cased name because peerDisplayName is shared
	// with both amendment surfaces and with D-28's standing line.
	WinnerAtThisPost bool
	OwnWall          string // the losing correction's own value ("" when unknown)
	// Superseding: rendering 2 — the recorded correction won over these.
	Superseding []timeCorrectPeer
	// EffectiveWall is the resolved effective instant, all three renderings.
	EffectiveWall string
}

// timeCorrectPage is the render model.
type timeCorrectPage struct {
	pageShell // top bar: post, alert banner (pageshell.go)

	SiteID string
	Cfg    any

	Error string
	// Preserved form state: a refusal re-renders everything typed.
	Target        string
	CorrectedWall string
	RecordedBy    string

	// TimeWarnings are TP §4.6's warn-accept notices, carried across the
	// PRG redirect as query codes and rendered here — the page the officer
	// sees next.
	TimeWarnings []string

	// The D-28 standing-cost line, derived from config exactly as the
	// amendment surface derives it (VF §5's derivation rule).
	HasPeer       bool
	StandingOther string
	StandingTaker string

	Outcome *timeCorrectOutcome
}

// timeCorrectWall renders an instant as the site-local wall time the
// officer reads (display only — storage is UTC ms, untouched).
func timeCorrectWall(ms int64) string {
	return time.UnixMilli(ms).In(time.Local).Format("2006-01-02 15:04")
}

// buildTimeCorrectOutcome computes the confirmation block for one recorded
// key at RENDER time (VF §4.3 rule 2: read-time truth, never POST-time
// cache). Any failure to determine the outcome — an unparseable key, a
// resolver error (dangling target, cycle, unknown event) — is rendering 4,
// loudly, never an omitted block.
func (s *Server) buildTimeCorrectOutcome(recorded string) *timeCorrectOutcome {
	// ⟦F11⟧ a sealed trade id is another kind's confirmation, not a key.
	if _, isKey := parseRecordedKey(recorded); !isKey && s.recordedNamesOtherKind(recorded) {
		return &timeCorrectOutcome{NotACorrection: true, Key: recorded}
	}
	k, ok := parseRecordedKey(recorded)
	if !ok {
		return &timeCorrectOutcome{Failed: true}
	}
	// ⟦T-199⟧ the key must name a time CORRECTION. ResolveOccurredAt walks
	// to the original target from ANY key, so a check-in's or a dispatch's
	// own key resolved "effective" and the page said "Recorded and
	// effective" about an act nobody had just taken. Only an EXISTING event
	// of ANOTHER kind is answered "nothing to confirm": a garbled key, no
	// event at the key, or a read failure stay rendering 4 above and below
	// — VF-m5b's locked criterion, untouched:
	// TestW4CorrectionConfirmationFailurePathRendersLoudly.
	typ, err := domain.EventTypeAt(s.Store, k)
	if err != nil || typ == "" {
		return &timeCorrectOutcome{Failed: true}
	}
	if typ != domain.EventOccurredAtCorrected {
		return &timeCorrectOutcome{NotACorrection: true, Key: recorded}
	}
	eff, chain, err := domain.ResolveOccurredAt(s.Store, k)
	if err != nil {
		return &timeCorrectOutcome{Failed: true}
	}
	view := &timeCorrectOutcome{EffectiveWall: timeCorrectWall(eff.InstantUTCms)}
	if eff.Winner == k {
		// The recorded event's assertion stands. Name every correction it
		// beat (rendering 2); with no competitor the block stays terse
		// (rendering 1 — the cry-wolf rule: no competitor, no supersession
		// language anywhere).
		for _, c := range chain {
			if c.Status == domain.Superseded {
				view.Superseding = append(view.Superseding, timeCorrectPeer{
					Post:       s.peerDisplayName(c.Key.SiteID),
					Wall:       timeCorrectWall(c.InstantUTCms),
					RecordedBy: c.RecordedBy,
				})
			}
		}
		return view
	}
	// The recorded assertion is superseded (rendering 3).
	view.Superseded = true
	view.WinnerPost = s.peerDisplayName(eff.Winner.SiteID)
	// ⟦L-1's class⟧ The same-post signal, read off the WINNER's site id on
	// the LOSING arm — this branch, never the rendering-2 branch above.
	view.WinnerAtThisPost = eff.Winner.SiteID == s.Cfg.SiteID
	for _, c := range chain {
		if c.Key == k {
			view.OwnWall = timeCorrectWall(c.InstantUTCms)
		}
	}
	return view
}

func (s *Server) buildTimeCorrectPage(refusal, target, wall, recordedBy string, warnings []string, outcome *timeCorrectOutcome) timeCorrectPage {
	hasPeer, other, taker := s.standingLine()
	return timeCorrectPage{
		pageShell:     s.shell(),
		SiteID:        s.Cfg.SiteID,
		Cfg:           s.Cfg,
		Error:         refusal,
		Target:        target,
		CorrectedWall: wall,
		RecordedBy:    recordedBy,
		TimeWarnings:  warnings,
		HasPeer:       hasPeer,
		StandingOther: other,
		StandingTaker: taker,
		Outcome:       outcome,
	}
}

func (s *Server) handleTimeCorrectPage(w http.ResponseWriter, r *http.Request) {
	var outcome *timeCorrectOutcome
	if rec := noticeQuery(r).Get(recordedQueryKey); rec != "" {
		outcome = s.buildTimeCorrectOutcome(rec)
	}
	s.renderPage(w, "time correct", "time_correct.html",
		s.buildTimeCorrectPage("", "", "", "", timeWarningsFrom(r), outcome))
}

// handleTimeCorrect authors one correction. Refusals here are PRE-APPEND —
// an unreadable target or corrected time, or TP §4.6's one refusing bound —
// and every refusal re-renders this page with everything typed intact
// (TP §4.6 rule 6; W4-5's fidelity clause on the refusal arm).
// parseTypedEventKey reads an event key an officer typed or pasted: the
// site.epoch.seq form this page has always taken, or site:epoch:seq — the
// form the visits report (domain keyString) prints, so a key copied from
// the report is accepted as it is (FIX17b, sc-stress F6). The colon form is
// read only when the dotted one fails and the key has exactly two colons and
// no dot, so no key the dotted form accepts is read differently.
func parseTypedEventKey(v string) (store.Key, bool) {
	if k, ok := parseRecordedKey(v); ok {
		return k, true
	}
	if strings.Count(v, ":") == 2 && !strings.Contains(v, ".") {
		return parseRecordedKey(strings.ReplaceAll(v, ":", "."))
	}
	return store.Key{}, false
}

func (s *Server) handleTimeCorrect(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	target := strings.TrimSpace(r.FormValue("target"))
	wall := strings.TrimSpace(r.FormValue("corrected_occurred_at"))
	recordedBy := strings.TrimSpace(r.FormValue("recorded_by"))
	refuse := func(status int, msg string) {
		s.renderPageStatus(w, status, "time correct", "time_correct.html",
			s.buildTimeCorrectPage(msg, target, wall, recordedBy, nil, nil))
	}

	k, ok := parseTypedEventKey(target)
	if !ok {
		refuse(http.StatusBadRequest, "The target "+target+" could not be read (expected the event's key as site.epoch.seq, or site:epoch:seq as the visits report prints it). Nothing has been recorded; everything typed is still on this page.")
		return
	}
	if wall == "" {
		refuse(http.StatusBadRequest, "No corrected time arrived with this submission. Nothing has been recorded; everything typed is still on this page.")
		return
	}
	parsed, err := time.ParseInLocation(wallTimeLayout, wall, time.Local)
	if err != nil {
		refuse(http.StatusBadRequest, "The corrected time "+wall+" could not be read (expected a date and time like 2026-01-31T14:05). Nothing has been recorded; nothing was adjusted or guessed. Everything typed is still on this page.")
		return
	}
	// TP §4.6's bounds on the payload instant, through W2's produced
	// machinery — the corrected value is typed, always, so it is bounded
	// unconditionally; the value itself is never touched.
	tc := timeCapture{TimeSource: "entered", OccurredAtMS: parsed.UnixMilli(), Wall: wall}
	if cerr := s.timeBounds(&tc); cerr != nil {
		refuse(cerr.status, cerr.renderMessage())
		return
	}

	// FIX17b (sc-stress F12): a correction that puts an end before its
	// start — a check-out before its check-in, an arrival before its run's
	// departure, or the reverse — is WARNED, never refused: every bound on a
	// corrected instant warns and accepts (W4; the POST appends, VF §5), and
	// a mistake is mended by correcting the other time.
	if problem, _ := domain.TimeCorrectionOrderProblem(s.Store, k, tc.OccurredAtMS); problem != "" {
		tc.Warnings = append(tc.Warnings, timeWarnOrder)
	}

	env, err := domain.CorrectOccurredAt(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, domain.TimeCorrection{
		Target:                k,
		CorrectedOccurredAtMS: tc.OccurredAtMS,
		RecordedBy:            recordedBy,
	})
	if err != nil {
		refuse(http.StatusConflict, refusalText(err))
		return
	}
	http.Redirect(w, r, sealNotices(timeWarnRedirect(fmt.Sprintf("/time/correct?recorded=%s.%d.%d",
		env.SiteID, env.NodeEpoch, env.Seq), tc)), http.StatusSeeOther)
}
