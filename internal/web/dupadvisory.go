// dupadvisory.go — a non-blocking "possible existing match" advisory for
// the gate's add-on-demand person creation (resolvePersonSlot's isNew()
// branch, internal/domain/checkin.go). [[D-66]] already rules that the
// gate's add-it escape must never refuse; this rides alongside that rule
// and is strictly weaker than even a confirm — it does not ask the officer
// to do anything, it only offers a note they are free to ignore. Company
// and person creation carry NO uniqueness check by design (refdata.go's own
// header: "duplicates are the design's own worked example") — this file
// does not change that. It only reduces how often a duplicate gets minted
// by accident, by naming the likely match before the truck pulls away.
//
// SHAPE MIRRORS TP §4.6's time-warning warn-accept (timecapture.go): a
// check made before commit, carried across the accept path's redirect as
// query codes, rendered on the page the operator lands on next — a warning
// that dies with the redirect is a warning nobody was shown (same
// reasoning timeWarnRedirect's own comment gives).
//
// ONE DELIBERATE DIFFERENCE FROM timeWarnFloor/timeWarnAge/timeWarnClockSkew:
// those are a closed set of fixed codes because the copy they name is fixed
// too — timecapture.go's own rule is "the query value is a fixed code,
// never free text; the target page maps it back to its copy". This
// advisory's copy MUST name the candidate, so what rides the query string
// is the matched person's ID, never their name — the landing page resolves
// the id back to a name off the SAME roster read it already does for the
// picker (personName, gate.go's renderGate), never off the query string
// itself. An id the roster no longer recognises renders nothing, exactly
// like an unrecognised time-warning code.
package web

import (
	"log"
	"net/http"
	"net/url"
	"strings"

	"sitecontrol/internal/domain"
)

const dupWarnQueryKey = "dup"

// duplicateCandidatePersonIDs returns the ids of every roster person whose
// name READS AS slot's off-roster name under the one shared rule,
// personMatchCandidates (person_match.go: samePerson: picker.go's sameName
// plus the deterministic folds, each with its misses stated there) ⟦was:
// strings.EqualFold after TrimSpace, the same rule less the inner-whitespace
// collapse⟧, narrowed to the same company (case-insensitive on the
// company's current NAME) when the officer typed one for the new person. A
// slot with no company typed is not narrowed at all — the officer gave
// nothing to narrow by, so nothing is withheld on that account. The
// narrowing is this consumer's, not the rule's, and the tests here lock it
// for this copy's sake.
//
// An ExistingID slot (the officer picked a roster entry, or the no-JS
// fallback already resolved an unambiguous exact match to one — see
// resolveTypedPerson) is never a candidate: nothing is being created, so
// there is nothing to advise about.
func duplicateCandidatePersonIDs(slot domain.PersonSlot, persons []domain.Person, companyName map[string]string) []string {
	if slot.ExistingID != "" {
		return nil
	}
	name := strings.TrimSpace(slot.NewName)
	if name == "" {
		return nil
	}
	company := strings.TrimSpace(slot.NewCompany)
	var hits []string
	for _, m := range personMatchCandidates(persons, name, "") {
		p := m.Person
		if company != "" {
			var pc string
			if p.CompanyID != nil {
				pc = companyName[*p.CompanyID]
			}
			if !strings.EqualFold(strings.TrimSpace(pc), company) {
				continue
			}
		}
		hits = append(hits, p.PersonID)
	}
	return hits
}

// dupAdvisoryPersonIDs scans every off-roster slot on one vehicle (driver
// plus occupants) against a FRESH roster read taken HERE, immediately
// before this vehicle's own writes — so a match can only be a
// PRE-EXISTING roster entry, never one of this same commit's own new
// people (they are not on the roster yet when this read happens). Callers
// must call this before authoring, not after — see the call sites in
// gate.go.
//
// Two rows on the same vehicle typed with the same new name are NOT caught
// by this: that is a within-batch collision, a different question from
// "does this match the existing roster", and out of scope here.
//
// It never refuses: a lookup failure here is logged and treated as no
// advisory — the same "loud on failure, never fatal" stance renderGate
// already takes for the inbound list. An officer with a
// truck at the window must still get their check-in through if the only
// thing broken is this optional notice.
func (s *Server) dupAdvisoryPersonIDs(driver domain.PersonSlot, occupants []domain.PersonSlot) []string {
	persons, err := domain.Persons(s.Store)
	if err != nil {
		log.Printf("gate: dup advisory: persons: %v", err)
		return nil
	}
	companies, err := domain.Companies(s.Store)
	if err != nil {
		log.Printf("gate: dup advisory: companies: %v", err)
		return nil
	}
	companyName := make(map[string]string, len(companies))
	for _, c := range companies {
		companyName[c.CompanyID] = c.Name
	}
	slots := make([]domain.PersonSlot, 0, len(occupants)+1)
	slots = append(slots, driver)
	slots = append(slots, occupants...)

	var ids []string
	seen := map[string]bool{}
	for _, slot := range slots {
		for _, id := range duplicateCandidatePersonIDs(slot, persons, companyName) {
			if seen[id] {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

// dupWarnRedirect appends any advisory ids to a redirect target, same
// shape as timeWarnRedirect and chainable with it — either can run first,
// both check for an existing "?" before choosing the separator.
func dupWarnRedirect(target string, personIDs []string) string {
	if len(personIDs) == 0 {
		return target
	}
	q := url.Values{}
	for _, id := range personIDs {
		q.Add(dupWarnQueryKey, id)
	}
	sep := "?"
	if strings.Contains(target, "?") {
		sep = "&"
	}
	return sealNotices(target + sep + q.Encode()) // G-049: the commit token
}

// dupWarningCopy is the notice text, naming the candidate off the CURRENT
// roster read — never off the query string.
func dupWarningCopy(name string) string {
	return "A person named \"" + name + "\" is already on the roster. This check-in added another entry under the same name — nothing was blocked, and it was stored exactly as typed. Check the roster if this may be the same person."
}

// dupWarningsFrom reads the advisory ids off a request's query, resolves
// each through personName (the SAME roster-name lookup renderGate already
// builds for the picker), and returns the rendered copies. An id
// personName does not recognise renders nothing — the query string is
// request surface, not a message channel, exactly as timeWarningsFrom
// treats an unrecognised time-warning code.
func dupWarningsFrom(r *http.Request, personName map[string]string) []string {
	var out []string
	seen := map[string]bool{}
	for _, id := range noticeQuery(r)[dupWarnQueryKey] { // G-049: sealed redirects only
		if seen[id] {
			continue
		}
		seen[id] = true
		name, ok := personName[id]
		if !ok {
			continue
		}
		out = append(out, dupWarningCopy(name))
	}
	return out
}
