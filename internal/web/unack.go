// unack.go — F6 §3.4's authoring surface for the alert-acknowledgment
// correction (window item 17): `POST /alerts/unack`.
//
// The sibling of alerts.go's `POST /alerts/ack`, and deliberately its mirror
// in form shape (`trip_id`; ⟦was: "optional `review_phase`" — refused since
// weather phases were removed, James 2026-10-04⟧) — but NOT in its
// refusals. The ack path refuses several things because an ack SILENCES: it
// refuses a trip still counting down, because acknowledging then would
// pre-silence an alert that has not fired. An un-ack can never pre-silence
// anything; it only ever REMOVES silence. So the safe direction needs no
// symmetric gate, and this handler has exactly ONE refusal about the ack
// itself — the target set is empty, i.e. there is no standing
// acknowledgment to withdraw. (James, 2026-10-04, R3: plus one about the
// post — a post that cannot ack the alert cannot un-ack it; canAckTrip.)
//
// ⚠ ROUTE CLASSIFICATION (W2's route walk): `/alerts/unack` is
// EXEMPT-with-written-reason. The un-acknowledgment IS the act performed
// live at the screen — it cannot occur before its authoring — and NO INSTANT
// IS TYPED: the form carries an identity (`trip_id`), never
// a time, so there is nothing for a takes-input wiring test to wire. That is
// the same reason `/alerts/ack` carries, and D-44 lands on
// `alert_ack_retracted` vacuously (its payload carries no instant at all),
// which strengthens rather than weakens it.
// ⚠ THE `w2RouteTable` ROW FOR THIS ROUTE IS NOT APPLIED HERE. That table is
// criteria-lock material inside a locked test file, and this build may not
// edit tests; the row and its written reason are routed to W9c's gate in
// `reports/window-build/W9c-build.md` §7. Until it lands, the route walk
// reports this route as unclassified — which is the walk working.
package web

import (
	"net/http"

	"sitecontrol/internal/deadline"
)

// registerAlertUnackRoutes mounts the un-ack surface.
//
// Unconditional, where `POST /alerts/ack` sits inside `if s.Alerts != nil`:
// the ack handler asks the SCAN whether the trip is currently alerting, and
// the un-ack handler asks neither the scan nor the board anything. Its whole
// question — "does this trip carry a standing acknowledgment in this scope" —
// is answered from the two projections, which exist wherever
// `deadline.Register` ran. Gating it on the scanner would make the correction
// path unreachable on a box where the alert it corrects is still in the log.
func (s *Server) registerAlertUnackRoutes(mux *http.ServeMux) {
	// Registered as a plain literal so W2's route walk — which token-scans
	// `internal/web/*.go` for "POST /…" string literals — DISCOVERS it. A
	// route assembled from a constant is invisible to that universe, and a
	// build that hid its own new route from the walk to keep a suite green
	// would be defeating the one check that exists to find it.
	mux.HandleFunc("POST /alerts/unack", s.requireLogin(s.handleUnackAlert))
}

// handleUnackAlert authors the F6 correction: one `alert_ack_retracted` per
// currently-effective acknowledgment of the trip.
//
// ONE PREDICATE DEFINES THE TARGET SET AND THE REFUSAL IS EXACTLY ITS
// EMPTINESS (F6 §3.4 rule 1). Nothing else is checked: not whether the trip
// is on the board, not whether it is currently alerting. Each of those would be a
// second predicate, and a second predicate is a second way to turn the
// officer away from a correction the record permits.
//
// ⟦James, 2026-10-04, ruling R3⟧ ONE exception, and it is not a second
// predicate on the ack state but a check on WHO is asking: the trip must be
// on this post's deadline board (canAckTrip). A post that could not have
// acknowledged the alert may not re-arm it.
func (s *Server) handleUnackAlert(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	tripID := r.FormValue("trip_id")
	if tripID == "" {
		http.Error(w, "trip_id is required", http.StatusBadRequest)
		return
	}

	// A cached weather-row form still carrying review_phase is REFUSED,
	// never read as the plain scope (weather phases removed, James
	// 2026-10-04): it named a weather acknowledgment, and silently widening
	// it to the trip's own acks would re-arm a red the officer never meant
	// to touch. ⟦was: review_phase SELECTED THE SCOPE BY ITS PRESENCE — the
	// wide phase-scope sweep that reached a dormant phase-2 ack at phase 3⟧
	if _, stale := r.Form["review_phase"]; stale {
		http.Error(w, "review_phase is no longer accepted — weather phases were removed, so there is no weather acknowledgment to withdraw. Reload the page and use the alert row's own control. Nothing was recorded.", http.StatusBadRequest)
		return
	}

	targets, err := deadline.EffectiveAcks(s.Store, tripID)
	if err != nil {
		s.tripsFail(w, "unack: effective acknowledgments", err)
		return
	}
	// James, 2026-10-04 (ruling R3): "a post that cannot ack an alert should
	// not be able to un-ack (re-arm) it." Acks have two owning ends now, and
	// a third post (npf) with no board row for the trip could re-arm a red
	// at both ice posts from a crafted POST (L08 T-100 group). So the door
	// asks the one question the ack door asks first — is this trip on THIS
	// post's deadline board — and refuses exactly when the ack door would
	// say "not on the deadline board". It deliberately does NOT inherit the
	// ack door's other refusals (countdown, already acked): an un-ack only
	// removes silence. ⟦was: "The phase scope stays WIDE among a board's own
	// trips … but needs a post the weather board governs" — weather phases
	// removed⟧
	// ⟦was: "Nothing else is checked: not whether the trip is on the board
	// … Gating it on the scanner would make the correction path unreachable
	// on a box where the alert it corrects is still in the log" — re-ruled:
	// such a box could not have acked it, so it may not re-arm it either⟧
	// Asked AFTER EffectiveAcks so that read stays the handler's first
	// store access (TestWitnessD5UnackHandlerSurfacesTheEffectiveAcksError
	// discriminates on that arm's own log line).
	if !s.canAckTrip(w, tripID) {
		return
	}
	if len(targets) == 0 {
		// The refusal authors NOTHING, like the ack's own refusals. The
		// sentence is written for the officer rather than passed up from a
		// domain error, so nothing internal leaks onto the screen.
		http.Error(w, "trip "+tripID+" carries no standing acknowledgment"+
			" — there is nothing to un-acknowledge", http.StatusConflict)
		return
	}

	// ONE CLICK, N RETRACTIONS, EXACTLY N — one per standing ack. A trip can
	// carry two effective acks (both posts acked before either replicated),
	// and retracting only one would leave the banner silent while the POST
	// reported success: a silent partial no-op on the officer's own screen.
	if err := deadline.RetractAcks(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, targets, s.postAttribution(), 0, "default"); err != nil {
		s.tripsFail(w, "unack: author retraction", err)
		return
	}
	http.Redirect(w, r, "/trips", http.StatusSeeOther)
}

// canAckTrip is R3's capability check for the un-ack door: true when this
// post's deadline board carries the trip as an alert row. On false it has
// already written the refusal, which authors nothing. ⟦was: a scope
// argument — the phase scope also accepted a weather VERIFY row, at a post
// the weather board governed⟧
func (s *Server) canAckTrip(w http.ResponseWriter, tripID string) bool {
	refusal := "trip " + tripID + " is not on this post's deadline board — a post that cannot acknowledge an alert cannot withdraw an acknowledgment of it either (only the trip's origin and destination can). Nothing was recorded."
	if s.Alerts == nil {
		http.Error(w, refusal, http.StatusConflict)
		return false
	}
	board, err := s.Alerts.Evaluate()
	if err != nil {
		s.tripsFail(w, "unack: scan", err)
		return false
	}
	for _, a := range board.Alerts {
		if a.TripID == tripID {
			return true
		}
	}
	http.Error(w, refusal, http.StatusConflict)
	return false
}
