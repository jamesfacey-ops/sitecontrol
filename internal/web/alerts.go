// alerts.go — C7's acknowledgment route. The banner itself is a template
// partial (templates/banner.html) rendered on every page from Server.banner()
// — there is no "alerts page" to close and thereby lose an alert.
package web

import (
	"net/http"

	"sitecontrol/internal/deadline"
)

// handleAckAlert authors the explicit, logged acknowledgment (§9.4/A14).
// Only a trip CURRENTLY on the alert surface (overdue-live or
// overdue-unresolved, not yet acked) may be acknowledged: an ack recorded
// while a countdown is still healthy would pre-silence an alert that has not
// fired yet — refused, like every other unsafe authoring (the refusal
// authors nothing).
func (s *Server) handleAckAlert(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	tripID := r.FormValue("trip_id")
	if tripID == "" {
		http.Error(w, "trip_id is required", http.StatusBadRequest)
		return
	}
	board, err := s.Alerts.Evaluate()
	if err != nil {
		s.tripsFail(w, "ack: scan", err)
		return
	}
	// A cached weather VERIFY-row form (or a hand-built POST) still carrying
	// review_phase is REFUSED, never read as a plain ack: weather phases were
	// removed (James, 2026-10-04), and guessing would let a stale weather
	// form silence the trip's red. ⟦was: review_phase scoped the ack to a
	// weather-phase VERIFY row (weather-phases-v1 §7) and authored
	// deadline.AcknowledgePhaseReview⟧
	if _, stale := r.Form["review_phase"]; stale {
		http.Error(w, "review_phase is no longer accepted — weather phases were removed, so there is no weather VERIFY row to acknowledge. Reload the page and acknowledge the alert row itself. Nothing was recorded.", http.StatusBadRequest)
		return
	}
	var found *deadline.Alert
	for i := range board.Alerts {
		if board.Alerts[i].TripID == tripID {
			found = &board.Alerts[i]
			break
		}
	}
	switch {
	case found == nil:
		http.Error(w, "trip "+tripID+" is not on the deadline board — nothing to acknowledge", http.StatusConflict)
		return
	case found.Status == deadline.StatusCountdown:
		http.Error(w, "trip "+tripID+" is still counting down — acknowledging now would pre-silence an alert that has not fired; wait for it or record the arrival", http.StatusConflict)
		return
	case found.Acked:
		http.Error(w, "trip "+tripID+" is already acknowledged", http.StatusConflict)
		return
	}
	kind := string(found.Status)      // R3: the kind of row pressed on, from THIS post's board
	recordedBy := s.postAttribution() // D-5: this box's post, never a person
	// James, 2026-10-04 (ruling R3): ack per alert kind — an ack pressed
	// on an amber VERIFY row must not silence the other end's red.
	err2 := deadline.AcknowledgeKind(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, tripID, kind, recordedBy, 0, "default")
	if err2 != nil {
		refuse(w, err2)
		return
	}
	http.Redirect(w, r, "/trips", http.StatusSeeOther)
}

// alertDueText is Q-FE2's due clause (James, 2026-10-04) for the boards:
// "due about 11:07 by 2P Ice Road Entrance's clock" — the run's recorded due
// time (deadline.Alert.DueAt — the effective departure plus the allowance;
// James 2026-10-06: from the typed time) on the ORIGIN's clock, named by
// its display name, never its id. "" when no due time is on record.
func (s *Server) alertDueText(a deadline.Alert) string {
	c := a.DueClock()
	if c == "" {
		return ""
	}
	return "due about " + c + " by " + s.Cfg.SiteName(a.Origin) + "'s clock"
}
