// staletick.go — the D-88 cross-site orientation warning's STALE TICK notice
// (orientation-doors lane, 2026-09-11 — gap 2 in
// reports/witness/CROSSSITE-WARNING.md: "A row that goes stale between
// accumulate and commit ... silently drops the tick rather than refusing").
//
// completeTickedOrientations (gate.go) refuses to author an orientation
// completion for a row whose typed identity changed between the accumulate
// step that rendered the warning and the final submit — see that function's
// header for the two shapes of staleness and why authoring blindly would be
// worse (a completion granted to the WRONG person, attributed as officer-
// confirmed when it was not). The non-negotiable this lane was given: the
// officer must never be left believing an orientation was recorded when it
// was not.
//
// The decision, and why: NOT a refusal of the whole vehicle. A stale tick
// means the row's PERSON changed under the officer's nose — the rest of the
// vehicle (everyone else's identity, everyone else's tick) is unaffected,
// and refusing the whole commit over one checkbox that no longer names
// anybody real would cost the entire bus its entry over a single stale
// field, at exactly the moment (a full vehicle, an officer mid-list) the
// team lead's brief named as the operationally worse failure. Instead this
// is the SAME warn-accept shape as notentered.go/dupadvisory.go: the
// check-in succeeds, and this notice rides the redirect naming EXACTLY who
// the tick did not apply to — the same "name the fact, not just its
// existence" posture notentered.go already argues for its own notice.
//
// The person named here almost always ALSO appears in the ordinary NOT
// ENTERED notice (notentered.go) if they are still an occupant of this same
// vehicle under a different row — the stale tick's target and the row's new
// occupant are different people. But when the row was edited to a person NOT
// on this vehicle at all (typed clean over, or removed), NOTHING else on the
// page would ever mention them: they simply are not part of this commit, and
// a bare NOT ENTERED list (keyed to this vehicle's actual occupants) cannot
// name someone who fell off it entirely. This notice is what closes that
// blind spot — it is keyed to the TICK, not to the vehicle's final
// occupant list.
package web

import (
	"net/http"
	"net/url"
	"strings"
)

// staleOrientTickQueryKey carries the ids of ticks that went stale across
// the post-check-in redirect. Ids, not names — same reasoning as
// notEnteredQueryKey: the query string is request surface, and an id
// personName does not recognise renders nothing.
const staleOrientTickQueryKey = "stale_orient_tick"

// staleOrientTickNotice is one rendered notice.
type staleOrientTickNotice struct {
	Name string // the ticked person's CURRENT roster name
	Site string // this post's display name — whose orientation the tick targeted
}

// staleOrientTickRedirect appends the stale ids to a redirect target, the
// same shape as timeWarnRedirect/dupWarnRedirect/notEnteredRedirect and
// chainable with all three.
func staleOrientTickRedirect(target string, personIDs []string) string {
	if len(personIDs) == 0 {
		return target
	}
	q := url.Values{}
	for _, id := range personIDs {
		q.Add(staleOrientTickQueryKey, id)
	}
	sep := "?"
	if strings.Contains(target, "?") {
		sep = "&"
	}
	return sealNotices(target + sep + q.Encode()) // G-049: the commit token
}

// staleOrientTickNoticesFrom reads the stale ids off a request's query and
// resolves each through personName (renderGate's own roster read). Repeats
// collapse; an unrecognised id renders nothing — the same posture as
// notEnteredNoticesFrom.
func staleOrientTickNoticesFrom(r *http.Request, personName map[string]string, siteDisplay string) []staleOrientTickNotice {
	var out []staleOrientTickNotice
	seen := map[string]bool{}
	for _, id := range noticeQuery(r)[staleOrientTickQueryKey] { // G-049: sealed redirects only
		if seen[id] {
			continue
		}
		seen[id] = true
		name, ok := personName[id]
		if !ok {
			continue
		}
		out = append(out, staleOrientTickNotice{Name: name, Site: siteDisplay})
	}
	return out
}
