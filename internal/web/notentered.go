// notentered.go — the gate's NOT ENTERED notice (D-98/D-91; James's ruling of
// 2026-09-09: "they will not be allowed on site untill oriantation is
// completed the officer will not put there information in untill they are").
//
// At a post whose own config says orientation = true, POST /gate/checkin and
// POST /gate/resume now hand s.Cfg.HasOrientation() to the domain
// (checkin.go), so buildArrivalVisits holds an un-oriented occupant out
// exactly as the two trip-arrival doors already did: no visit, a
// trip_arrival_not_entered row. The check-in itself SUCCEEDS — the vehicle
// came, the trip closes, everyone who holds the orientation is entered — so
// this is not a refusal of the form and cannot ride in vehicleForm.Error. It
// is the same warn-accept shape as dupadvisory.go: the ids ride the redirect
// as query values, and the gate page renders them off the CURRENT roster
// read, never off the query string.
//
// ⚠ WHAT THE NOTICE MUST DO, beyond saying "not entered": name the next
// action. The one real hazard of a hard rule is a worker who HAS completed
// the orientation but whose record is not yet in the system — standing at
// the gate in the cold while the officer reads a bare error. So the copy
// (gate.html "not_entered_notices") names the person, states the consequence
// (not on site, not on the muster), and links the page that fixes it: record
// the completion under /orientation, then check them in again. Both routes
// exist at every orientation=true post — web.go registers /orientation on
// exactly the condition that makes this notice reachable at all — and the
// second check-in is the ordinary gate form, no new door.
//
// ⚠ NO OVERRIDE. Orientation is a precondition for being on site, not an
// obstacle to entry an officer might need to bypass in a hurry (James,
// above). Nothing here, and nothing in the handlers, enters a person without
// the orientation row.
//
// ⚠ NOT A PERSISTENT SURFACE. This notice exists for the one page render
// after the redirect, to the officer who made the entry. The STANDING list
// is /orientation's held-out section (domain.HeldOutAt, heldout.go — R-11,
// 2026-09-09; the reader half of d91-arrival-respec.md §3.4), which this
// notice's own link lands on. Its other half, entry-on-completion, is NOT
// built and is gated ⟦was: "trip_arrival_not_entered has no production
// reader (MEASURED at f7afc3a, reports/witness/ORIENTATION-ENFORCEMENT.md); a
// standing 'held out, not yet oriented' list belongs with
// d91-arrival-respec.md §3.4's entry-on-completion, which needs the same
// reader, and is NOT built here"⟧.
package web

import (
	"net/http"
	"net/url"
	"strings"
)

// notEnteredQueryKey carries the held-out person ids across the
// post-check-in redirect. Ids, not names: the query string is request
// surface, and an id personName does not recognise renders nothing.
const notEnteredQueryKey = "not_entered"

// notEnteredNotice is one rendered notice. The template writes the sentence
// so the route can be a real link; this carries only what it interpolates.
type notEnteredNotice struct {
	Name string // the person's CURRENT roster name
	Site string // this post's display name — the site whose orientation they lack
}

// notEnteredRedirect appends the held-out ids to a redirect target, the same
// shape as timeWarnRedirect and dupWarnRedirect and chainable with both.
func notEnteredRedirect(target string, personIDs []string) string {
	if len(personIDs) == 0 {
		return target
	}
	q := url.Values{}
	for _, id := range personIDs {
		q.Add(notEnteredQueryKey, id)
	}
	sep := "?"
	if strings.Contains(target, "?") {
		sep = "&"
	}
	return sealNotices(target + sep + q.Encode()) // G-049: the commit token
}

// notEnteredNoticesFrom reads the held-out ids off a request's query and
// resolves each through personName (renderGate's own roster read). Repeats
// collapse; an unrecognised id renders nothing.
func notEnteredNoticesFrom(r *http.Request, personName map[string]string, siteDisplay string) []notEnteredNotice {
	var out []notEnteredNotice
	seen := map[string]bool{}
	for _, id := range noticeQuery(r)[notEnteredQueryKey] { // G-049: sealed redirects only
		if seen[id] {
			continue
		}
		seen[id] = true
		name, ok := personName[id]
		if !ok {
			continue
		}
		out = append(out, notEnteredNotice{Name: name, Site: siteDisplay})
	}
	return out
}
