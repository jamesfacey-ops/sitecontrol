// gate_edit_select.go — the on-site roster's "Edit person" button
// (gate-checkin-spec, James, 2026-09-13: "an edit person button that will
// only work for editing one person at a time not more than that").
//
// SAME CLASS AS handleGateCheckOutSelectForDeparture (gate.go): it reads
// ticked visit ids off the roster's own form, resolves each against a FRESH
// read of this post's own open visits — a posted id is never trusted beyond
// "actually on site here right now" — and authors NOTHING. Registered
// ungated for the same reason: a route that only reads and redirects has
// nothing for requireLogin to protect.
//
// THE ONE-PERSON RULE. Zero or two-or-more distinct people ticked is a
// refusal, named in words (editSelectNoticeFrom), not a silent no-op and not
// a guess at which one was meant. Distinct PEOPLE, not distinct VISITS: two
// open visits for the same person (a data anomaly) still count as one — the
// button edits a PERSON record, not a visit row — so ticking both of that
// person's rows is not a reason to refuse.
//
// Exactly one resolves to a plain redirect to /person/amend?person_id=<id>,
// which person_amend.go reads to prefill its own free-text id box (a small,
// additive change there) — today that box has no picker of its own at all
// and expects the officer to already know the internal person id, which
// this button is what makes it reachable in practice from the roster.
package web

import (
	"net/http"
	"net/url"
	"strings"

	"sitecontrol/internal/domain"
)

// editSelectQueryKey carries the refusal reason across the redirect back to
// /gate — the same warn-accept query-carriage shape as notEnteredRedirect
// and dupWarnRedirect, but for a REFUSAL rather than an accepted warning:
// nothing was recorded either way, so there is nothing to accept.
const editSelectQueryKey = "editsel"

// editSelectNoticeFrom reads the refusal reason, if any, off the request's
// query. Anything else (absent, or a value this door never wrote) renders
// nothing — an unrecognised query key is silently ignored, matching every
// other warn-accept reader in this package.
func editSelectNoticeFrom(r *http.Request) string {
	switch noticeQuery(r).Get(editSelectQueryKey) { // G-049: sealed redirects only
	case "none":
		return "Tick exactly one person on the roster, then press Edit person."
	case "many":
		return "Edit person works on exactly one person at a time. Untick the rest, then press Edit person again."
	default:
		return ""
	}
}

// handleGateEditSelected resolves the roster's ticked rows to a single
// person id and hands off to /person/amend, or refuses in words when the
// tick count is not exactly one.
func (s *Server) handleGateEditSelected(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	visits, err := domain.OpenVisitsAt(s.Store, s.Cfg.SiteID)
	if err != nil {
		s.gateFail(w, "open visits", err)
		return
	}
	byVisit := make(map[string]domain.Visit, len(visits))
	for _, v := range visits {
		byVisit[v.VisitID] = v
	}
	seen := map[string]bool{}
	var personIDs []string
	for _, raw := range r.Form["visit_ids"] {
		vid := strings.TrimSpace(raw)
		if vid == "" {
			continue
		}
		v, ok := byVisit[vid]
		if !ok {
			// Not open here right now — a race, or a tampered/stale id.
			// Silently excluded from the count, the same posture a stale
			// tick gets in handleGateCheckOutSelectForDeparture: this door
			// authors nothing, so there is no list to keep intact, only a
			// count to get right.
			continue
		}
		if !seen[v.PersonID] {
			seen[v.PersonID] = true
			personIDs = append(personIDs, v.PersonID)
		}
	}
	// ⟦R-27, James, 2026-09-16⟧ this button is now served on BOTH gate
	// pages (see gateReturnPath's own comment) — a refusal must bounce the
	// officer back to whichever one they were on, not always "/gate".
	back := gateReturnPath(r)
	switch len(personIDs) {
	case 1:
		http.Redirect(w, r, "/person/amend?person_id="+url.QueryEscape(personIDs[0]), http.StatusSeeOther)
	case 0:
		http.Redirect(w, r, sealNotices(back+"?"+editSelectQueryKey+"=none"), http.StatusSeeOther)
	default:
		http.Redirect(w, r, sealNotices(back+"?"+editSelectQueryKey+"=many"), http.StatusSeeOther)
	}
}
