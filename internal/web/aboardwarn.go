package web

// aboardwarn.go — L17-15 (F16, fix3 2026-10-04): the gate's WARNING, never a
// refusal, when a person it just checked in is still recorded aboard another
// post's open run (domain.AboardOpenRunsAwayFrom). ⟦was: npf checked in
// somebody aboard an ice-road→ice-pad run with no word at all, and the person
// sat on npf's muster and on the run's sheet at once (L13-7b)⟧.
//
// Same warn-accept carriage as dupadvisory.go: the commit's sealed redirect
// carries person ids, and the gate page re-derives the run from the record at
// render time — the query string names who to look at, never what to say.

import (
	"net/http"
	"net/url"
	"strings"

	"sitecontrol/internal/domain"
)

const aboardWarnQueryKey = "aboard"

// aboardWarnRedirect appends the ids of the people this check-in entered who
// are still aboard another post's open run.
func aboardWarnRedirect(target string, personIDs []string) string {
	if len(personIDs) == 0 {
		return target
	}
	q := url.Values{}
	for _, id := range personIDs {
		q.Add(aboardWarnQueryKey, id)
	}
	sep := "?"
	if strings.Contains(target, "?") {
		sep = "&"
	}
	return sealNotices(target + sep + q.Encode()) // G-049: the commit token
}

// aboardAfterCheckIn is the commit's read: of the people just checked in by
// id, those still aboard another post's open run. Best-effort — a read
// fault costs the warning, never the check-in that already landed.
func (s *Server) aboardAfterCheckIn(slots []domain.PersonSlot) []string {
	var ids []string
	for _, sl := range slots {
		if sl.ExistingID != "" {
			ids = append(ids, sl.ExistingID)
		}
	}
	runs, err := domain.AboardOpenRunsAwayFrom(s.Store, s.Cfg.SiteID, ids, s.Cfg.SiteName)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(runs))
	for _, r := range runs {
		out = append(out, r.PersonID)
	}
	return out
}

// aboardWarningsFrom renders the sealed ids as notices, re-read from the
// record NOW: a run that has since arrived, or a sheet already corrected,
// says nothing.
func (s *Server) aboardWarningsFrom(r *http.Request, personName map[string]string) []string {
	ids := noticeQuery(r)[aboardWarnQueryKey] // G-049: sealed redirects only
	if len(ids) == 0 {
		return nil
	}
	runs, err := domain.AboardOpenRunsAwayFrom(s.Store, s.Cfg.SiteID, ids, s.Cfg.SiteName)
	if err != nil {
		return nil
	}
	var out []string
	for _, run := range runs {
		name, ok := personName[run.PersonID]
		if !ok {
			continue
		}
		out = append(out, aboardWarningCopy(name, run))
	}
	return out
}

// aboardWarningCopy is the notice text: who, which run, that nothing was
// blocked, and who to radio.
func aboardWarningCopy(name string, run domain.AboardRun) string {
	route, radio := run.Origin, run.Origin
	if run.Destination != "" {
		route += " to " + run.Destination
		radio += " or " + run.Destination
	}
	return name + " was checked in here, but is still recorded aboard " + run.Vehicle + " (" + route + "), a run that has not arrived. Nothing was blocked. Radio " + radio + " so that run's sheet is corrected — until it is, " + name + " is counted on this muster and on that run."
}
