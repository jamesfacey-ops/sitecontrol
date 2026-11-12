// orientation.go — SCHEMA-11 step 4: the site-orientation MARKING SURFACE
// (D-88, D-95, D-91, D-98; reports/witness/STEP4-ORIENTATION.md §5).
// `GET /orientation` lists who holds and who lacks THIS site's orientation;
// `POST /orientation/complete` records completions in bulk, one event per
// ticked person; `POST /orientation/retract` withdraws one mis-tick (D-95).
//
// ⚠ REGISTERED ONLY AT A POST WHOSE OWN CONFIG SAYS `orientation = true`
// (web.go Handler). At a road post the three routes DO NOT EXIST — not
// registered, never "disabled" behind a runtime check — the dev_routes
// pattern (an unregistered GET falls through to the "GET /" status page and
// an unregistered POST gets the mux's method refusal; nothing is authored) —
// and D-77's "no site identity in source": the condition is this post's own
// key, never a site name.
//
// ⚠ UNTICKED BY DEFAULT, deliberately and against the arrival list's grain
// (D-10, unreversed): a missed tick is friction and hides nobody; a wrong
// tick puts someone un-oriented on the pad, forever, with D-95's retraction
// as the only remedy. Each person is affirmed individually (D-3) — nothing
// here pre-fills from a manifest or a bus.
//
// ⚠ VALIDATE ALL, THEN AUTHOR PER PERSON (domain.CompleteOrientations): a
// refusal anywhere in a submission means NOTHING was recorded for any of it,
// and the page says so. The officer re-ticks a clean form rather than
// working out which of twenty-six landed.
//
// ⚠ THE REFUSALS RENDER COMPOSED COPY, NEVER err.Error() — domain returns a
// typed *OrientationRefusal and this file writes the officer's sentence,
// with this post's DISPLAY name where domain only knows its id. A refused
// POST comes back as this same page with an honest 409 (the house shape).
//
// ⚠ NOTHING HERE TOUCHES THE MUSTER. D-10's "not on the muster or any
// printed sheet" stands; this is the one screen that shows the flag.
//
// GATING: all three routes sit behind s.requireLogin. The GET is gated for
// the /trade reason: this is not a view of what happened, it is the site's
// whole roster with a per-person flag beside each name.
//
// Route classification: both POSTs are EXEMPT-with-written-reason in
// w2_route_walk_test.go's table — no typed instant; the completion is the
// act performed at the desk (D-88 rule 4: taken AT that site) and default
// capture describes when it was recorded.
package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"sitecontrol/internal/config"
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
)

// orientationPath is the page the two POSTs redirect back to.
const orientationPath = "/orientation"

// The registration literals are spelled out, NOT built from the consts
// above: the W2 route walk token-scans for `"POST /…"` STRING literals and
// a `"POST "+const` registration is invisible to it — the walk would
// neither discover nor classify these routes ([MEASURED 2026-09-06]: the
// concatenated form in unref.go is undiscovered today). The consts serve
// the redirects; the literals serve the walk.
func (s *Server) registerOrientationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /orientation", s.requireLogin(s.handleOrientationPage))
	mux.HandleFunc("POST /orientation/complete", s.requireLogin(s.handleOrientationComplete))
	mux.HandleFunc("POST /orientation/retract", s.requireLogin(s.handleOrientationRetract))
}

// orientationPage is the render model.
type orientationPage struct {
	SiteID string
	Cfg    *config.Config
	Post   string

	// Query is the name filter as typed (the season roster is four figures).
	Query string

	// The alert strip rides this page (James, 2026-10-04, L01 F-8).
	Banner    []deadline.Alert
	BannerErr string

	// Holding: people who hold this site's orientation, name order — each
	// with a retract control. Lacking: people who do not, name order — each
	// with an UNTICKED checkbox.
	Holding []orientationPerson
	Lacking []orientationPerson

	// HeldOut (2026-09-09): the people held out at THIS post's gate for lack
	// of its orientation and not entered since — domain.HeldOutAt, the
	// reader half of d91-arrival-respec.md §3.4. Oldest waiting first. THIS
	// page and no other: D-10's "not on the muster or any printed sheet"
	// stands, and this is the one screen that already shows the flag. Read
	// live per render, never cached; a person leaves it the moment a later
	// entry at this gate lands (heldout.go's predicate). An empty list is
	// rendered as a statement — nobody is waiting — not omitted.
	HeldOut []heldOutRow

	// Recorded: how many completions the last POST recorded (PRG echo);
	// Retracted: the name whose orientation the last POST withdrew.
	Recorded  int
	Retracted string
	// HeldOutReady ⟦heldout-recovery, 2026-09-10⟧: of the people the last
	// POST recorded, those STILL held out at this gate — "Uma Unoriented is"
	// / "A and B are", for the confirmation's sentence that they are not on
	// site and what to do next. Recording a completion enters nobody (R-9);
	// the Next cell in the table below already said so, but the officer
	// reads the confirmation, and a confirmation that stops at "recorded"
	// reads as "done". Composed off the CURRENT held-out read (the ids ride
	// the redirect, notentered.go's rule) — a person entered between the
	// redirect and this render names nobody. Empty when nobody recorded
	// was held out.
	//
	// ⚠ SchemaVersion 12 (R-16): this sentence's own premise — "recording a
	// completion enters nobody" — is no longer universally true. Entry-on-
	// completion (d91 §3.4) enters anyone whose completion found a held-out
	// arrival at THIS site; HeldOutReady now only ever names people whose
	// completion did NOT (the bulk-backfill case, or a cross-site hold-out
	// this edge could not resolve) — see HeldOutEntered for the other half.
	HeldOutReady string
	// HeldOutEntered (SchemaVersion 12, R-16): of the people the last POST
	// recorded, those this submission ENTERED — entry-on-completion minted
	// their visit off a held-out arrival. "Uma Unoriented is" / "A and B
	// are", for the confirmation to say they are NOW ON SITE rather than
	// staying silent about what just happened. ⟦L05 stale-second-completion,
	// 2026-10-03⟧ Computed from a before/after snapshot of this site's OPEN
	// VISITS taken around the CompleteOrientations call — "gained an open
	// visit here during this submission" — not from HeldOutAt: a person
	// held out at two vehicles, entered off the first, checked out and
	// retracted is no longer on the held-out list (sighted by that first
	// entry), yet a re-tick enters them off the second, still-unconsumed
	// arrival. Keyed on the held-out list, the banner said only "Recorded 1"
	// while the muster gained a person (L05 observation, orientation.go
	// :328-355 as it then stood). ⟦was: "a before/after snapshot of
	// HeldOutAt"⟧
	HeldOutEntered string

	// Refusal is the composed sentence for a refused POST; InternalError
	// says this post failed without dressing the fault as a refusal.
	Refusal       string
	InternalError bool
	// RefusalAmend ⟦F12, James, 2026-10-04: the L02 amend link must appear
	// on EVERY retired-name refusal surface⟧ is one amend link per retired
	// person named in a stale-retired Refusal (retiredOrientationRefusal).
	// ⟦was: the sentence named "the amend page" with no link (L12-4)⟧
	RefusalAmend []amendLink
}

// amendLink is one "open this person's amend page" link beside a refusal.
type amendLink struct {
	PersonID string
	Name     string
}

type orientationPerson struct {
	PersonID   string
	Name       string
	Company    string
	RecordedBy string // Holding only: the post that recorded the completion
	// Holding only ⟦R4, James, 2026-10-04⟧: when the completion was recorded
	// under a record since merged into this one, its name (shown) and id
	// (what the retract control withdraws — the row that actually exists).
	// Clearing that record's same_as is the other way to undo it.
	CarriedFromName string
	RetractID       string
}

// heldOutRow is one domain.HeldOut rendered for the officer: who (name,
// and company/trade — company is the field that tells two same-name people
// apart, measured 2026-09-09), WHEN they were held out, and what to do
// next. "Why" is one sentence for every row and lives in the template: they
// did not hold this post's orientation when they arrived.
type heldOutRow struct {
	PersonID string
	Name     string
	Company  string
	Trade    string
	// HeldOut is the hold-out arrival's effective occurred-at as site-local
	// wall time, through domain.ResolveOccurredAt so a corrected time reads
	// corrected — the same read and the same format the time-correction
	// surface uses (timeCorrectWall). Empty when the resolver fails closed
	// (a target that matches no local event); the template says so in
	// words rather than printing a blank cell.
	HeldOut string
	// Oriented: the person holds this post's orientation NOW. The row stays
	// listed either way — they are not entered until they are checked in
	// again — but the next action differs: record the completion, or go to
	// the gate.
	Oriented bool
}

// buildOrientationPage reads the composed roster and splits it by whether
// each person holds THIS site's orientation. Merged-away people (same_as
// set) are not listed: their surviving record is.
func (s *Server) buildOrientationPage(query string) (orientationPage, error) {
	page := orientationPage{SiteID: s.Cfg.SiteID, Cfg: s.Cfg, Post: s.Cfg.PostName(), Query: query}
	page.Banner, page.BannerErr = s.banner()
	persons, err := domain.Persons(s.Store)
	if err != nil {
		return page, err
	}
	merge, err := domain.LoadPersonMergeMap(s.Store)
	if err != nil {
		return page, err
	}
	holders, err := domain.OrientationHoldersAt(s.Store, merge, s.Cfg.SiteID)
	if err != nil {
		return page, err
	}
	nameOf := map[string]string{}
	for _, p := range persons {
		nameOf[p.PersonID] = p.Name
	}
	companies, err := domain.Companies(s.Store)
	if err != nil {
		return page, err
	}
	companyName := map[string]string{}
	for _, c := range companies {
		companyName[c.CompanyID] = c.Name
	}
	q := strings.ToLower(strings.TrimSpace(query))
	for _, p := range persons {
		if p.SameAs != nil {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(p.Name), q) {
			continue
		}
		row := orientationPerson{PersonID: p.PersonID, Name: p.Name}
		if p.CompanyID != nil {
			row.Company = companyName[*p.CompanyID]
		}
		if h, ok := holders[p.PersonID]; ok {
			if h.Row.RecordedBy != nil {
				row.RecordedBy = *h.Row.RecordedBy
			}
			row.RetractID = p.PersonID
			if h.CarriedFrom != "" {
				row.RetractID = h.CarriedFrom
				row.CarriedFromName = nameOf[h.CarriedFrom]
				if row.CarriedFromName == "" {
					row.CarriedFromName = "another record"
				}
			}
			page.Holding = append(page.Holding, row)
		} else if !p.IsRetired() {
			// ⟦R-118 / [[D-21.5]], 2026-10-03⟧ a retired record is not
			// offered a tick: the Lacking list is a picker for a NEW
			// completion, and retirement is "no longer selectable". A
			// retired person who already HOLDS the orientation stays in
			// Holding — that is their record, and the retract control is
			// how a mis-tick is still withdrawn.
			page.Lacking = append(page.Lacking, row)
		}
	}
	// Name order, id as the tiebreak — never the roster's incidental order
	// ((C1): force ordering).
	byName := func(rows []orientationPerson) {
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].Name != rows[j].Name {
				return rows[i].Name < rows[j].Name
			}
			return rows[i].PersonID < rows[j].PersonID
		})
	}
	byName(page.Holding)
	byName(page.Lacking)

	// The held-out list (heldout.go). Deliberately NOT filtered by the name
	// query: the filter is for finding a name among four figures of roster;
	// this list is the handful the gate refused, and hiding one of them
	// behind a search box is the invisibility this section exists to end.
	heldOut, err := domain.HeldOutAt(s.Store, s.Cfg.SiteID)
	if err != nil {
		return page, err
	}
	if len(heldOut) > 0 {
		trades, err := domain.Trades(s.Store)
		if err != nil {
			return page, err
		}
		tradeName := map[string]string{}
		for _, tr := range trades {
			tradeName[tr.TradeID] = tr.Name
		}
		byID := map[string]domain.Person{}
		for _, p := range persons {
			byID[p.PersonID] = p
		}
		for _, h := range heldOut {
			row := heldOutRow{PersonID: h.PersonID, Name: h.Name}
			if p, ok := byID[h.PersonID]; ok {
				if p.CompanyID != nil {
					row.Company = companyName[*p.CompanyID]
				}
				if p.TradeID != nil {
					row.Trade = tradeName[*p.TradeID]
				}
			}
			_, row.Oriented = holders[merge.Resolve(h.PersonID)]
			// Fail-closed on the time only, never on the row: a person the
			// gate refused stays listed even if this post cannot say when.
			if eff, _, err := domain.ResolveOccurredAt(s.Store, h.Key); err == nil {
				row.HeldOut = timeCorrectWall(eff.InstantUTCms)
			}
			page.HeldOut = append(page.HeldOut, row)
		}
	}
	return page, nil
}

func (s *Server) handleOrientationPage(w http.ResponseWriter, r *http.Request) {
	page, err := s.buildOrientationPage(r.URL.Query().Get("q"))
	if err != nil {
		http.Error(w, "the orientation list could not be read — nothing here is a complete record", http.StatusInternalServerError)
		return
	}
	// ⟦D23, fix2-fj 2026-10-04⟧ every confirmation key here is read from a
	// SEALED redirect only (noticeseal.go): a hand-typed
	// ?recorded=42&retracted=Fake+Person&entered=<anyone> used to print a
	// count, a retraction and "X is now on site" for events that never
	// happened.
	notice := noticeQuery(r)
	if n, err := strconv.Atoi(notice.Get(recordedQueryKey)); err == nil {
		page.Recorded = n
		page.HeldOutReady = heldOutReadyCopy(notice[heldOutQueryKey], page.HeldOut)
		page.HeldOutEntered = s.heldOutEnteredCopy(notice[heldOutEnteredQueryKey])
	}
	page.Retracted = notice.Get(retractedQueryKey)
	s.renderPage(w, "orientation", "orientation.html", page)
}

// heldOutQueryKey carries, across the post-completion redirect, the ids of
// the just-recorded people who were held out at this gate at that moment.
const heldOutQueryKey = "held"

// heldOutReadyCopy is the subject of the confirmation's "still not on site"
// sentence: the just-recorded ids that are on the CURRENT held-out list and
// hold the orientation now, as "X is" / "X and Y are". The query string
// says who was ticked; the live list says who is still waiting — an id it
// no longer lists (entered since, or retracted) renders nothing.
func heldOutReadyCopy(ids []string, rows []heldOutRow) string {
	ticked := map[string]bool{}
	for _, id := range ids {
		ticked[id] = true
	}
	var names []string
	for _, row := range rows {
		if ticked[row.PersonID] && row.Oriented {
			names = append(names, row.Name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return namesArePlural(names)
}

// heldOutEnteredQueryKey carries, across the post-completion redirect, the
// ids of the just-recorded people who gained an OPEN VISIT at this site
// during the submission — entry-on-completion (SchemaVersion 12, R-16)
// minted it off the held-out arrival that qualified. ⟦L05, 2026-10-03⟧ The
// signal is the visit, not the held-out list — see orientationPage.
// HeldOutEntered for the case the list missed.
const heldOutEnteredQueryKey = "entered"

// openPersonIDsHere is the set of people holding an open visit at THIS
// site right now — the before/after snapshot handleOrientationComplete
// takes around the completions. Best-effort: a read fault costs only the
// HeldOutEntered sentence, never the completions themselves, so it answers
// an empty set rather than an error.
func (s *Server) openPersonIDsHere() map[string]bool {
	out := map[string]bool{}
	open, err := domain.OpenVisitsAt(s.Store, s.Cfg.SiteID)
	if err != nil {
		return out
	}
	for _, v := range open {
		out[v.PersonID] = true
	}
	return out
}

// heldOutEnteredCopy composes HeldOutEntered's sentence subject: "X is" /
// "X and Y are", for ids resolved by domain.PersonByID (they are no longer
// on page.HeldOut — that is the whole point of having left it).
//
// ⚠ READ LIVE, same discipline heldOutReadyCopy's own comment states for
// its half: "is now on site" is a PRESENT-TENSE claim, so it is checked
// against a CURRENT open visit here, never asserted merely because this id
// rode the redirect. Without this a stale reload of the same confirmation
// URL after Uma later departs would keep telling the officer she is on
// site — the exact class of staleness bug heldout-recovery (2026-09-10)
// was built to refuse for HeldOutReady, now reused for its opposite.
func (s *Server) heldOutEnteredCopy(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	open, err := domain.OpenVisitsAt(s.Store, s.Cfg.SiteID)
	if err != nil {
		return ""
	}
	onSite := map[string]bool{}
	for _, v := range open {
		onSite[v.PersonID] = true
	}
	var names []string
	for _, id := range ids {
		if !onSite[id] {
			continue
		}
		p, err := domain.PersonByID(s.Store, id)
		if err != nil || p == nil {
			continue
		}
		names = append(names, p.Name)
	}
	if len(names) == 0 {
		return ""
	}
	return namesArePlural(names)
}

// retiredOrientationRefusal names the ticked people who are retired and not
// held out here; refused is false when there are none. A read fault refuses
// too — the safe direction for a stale form is "tick again", never a
// completion recorded against a record nobody could check.
func (s *Server) retiredOrientationRefusal(ids []string) (string, []amendLink, bool) {
	var heldOut map[string]bool
	var names []string
	var links []amendLink
	for _, id := range ids {
		p, err := domain.PersonByID(s.Store, id)
		if err != nil {
			return "This post could not read the roster to check the ticked names — nothing was recorded. Tick them again.", nil, true
		}
		if p == nil || !p.IsRetired() {
			continue
		}
		if heldOut == nil {
			heldOut = map[string]bool{}
			held, err := domain.HeldOutAt(s.Store, s.Cfg.SiteID)
			if err != nil {
				return "This post could not read the held-out list to check the ticked names — nothing was recorded. Tick them again.", nil, true
			}
			for _, h := range held {
				heldOut[h.PersonID] = true
			}
		}
		if heldOut[id] {
			continue
		}
		names = append(names, p.Name)
		links = append(links, amendLink{PersonID: p.PersonID, Name: p.Name})
	}
	if len(names) == 0 {
		return "", nil, false
	}
	return fmt.Sprintf("%s retired on the roster since this page was opened, so nothing was recorded for anyone ticked. If they are back, un-retire them on the amend page (clear retired) first, then tick them again.", namesWereRetired(names)), links, true
}

// namesWereRetired is the sentence head for retiredOrientationRefusal.
func namesWereRetired(names []string) string {
	if len(names) == 1 {
		return names[0] + " was"
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1] + " were"
}

// handleOrientationComplete records one `orientation_completed` per ticked
// person, or refuses PRE-APPEND for the whole submission.
func (s *Server) handleOrientationComplete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	ids := make([]string, 0, len(r.Form["person_id"]))
	for _, id := range r.Form["person_id"] {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		s.renderOrientationRefusal(w, r, "Nobody was ticked — nothing was recorded.")
		return
	}
	// ⟦L05 / R6, James, 2026-10-04: re-check retired at commit⟧ the Lacking
	// list stops offering a retired person a tick, but a form opened BEFORE
	// the retire still carries their box, and that tick recorded a new
	// completion against a record that is no longer selectable. Refused
	// here, pre-append, for the whole submission like every other refusal.
	// A person HELD OUT at this gate is exempt: they are physically here off
	// a truck (a presence path), and completing their orientation is how
	// they get onto the muster — retirement never strands someone on site.
	if sentence, links, refused := s.retiredOrientationRefusal(ids); refused {
		s.renderOrientationRefusalLinks(w, r, sentence, links)
		return
	}
	// SchemaVersion 12 (R-16): a BEFORE snapshot of who holds an open visit
	// here, taken before the completions land, so the redirect can tell
	// "this submission entered them" apart from "they were on site already"
	// and "nothing entered them". ⟦L05, 2026-10-03⟧ ⟦was: a snapshot of
	// HeldOutAt — which misses a person already SIGHTED by an earlier entry
	// whose second, unconsumed hold-out arrival this completion enters them
	// off⟧. Best-effort: a read fault here costs only the HeldOutEntered
	// sentence, never the completions themselves.
	openBefore := s.openPersonIDsHere()
	// No typed instant feeds the envelope: the completion is the act at the
	// desk, default-captured (the route's EXEMPT classification).
	envs, err := domain.CompleteOrientations(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, ids, s.postAttribution(), 0, "default")
	if err != nil {
		var ref *domain.OrientationRefusal
		if errors.As(err, &ref) {
			s.renderOrientationRefusal(w, r, s.orientationRefusalCopy(ref))
			return
		}
		// A store fault mid-batch: some may have landed. Do not dress it as
		// a refusal; say the post failed and let the page's lists show what
		// is now recorded.
		page, perr := s.buildOrientationPage("")
		if perr == nil {
			page.InternalError = true
			page.Recorded = len(envs)
			s.renderPageStatus(w, http.StatusInternalServerError, "orientation", "orientation.html", page)
			return
		}
		http.Error(w, "this post could not complete that — check the orientation list before re-ticking", http.StatusInternalServerError)
		return
	}
	// ⟦heldout-recovery, 2026-09-10⟧ the just-recorded people who are held
	// out at this gate ride the redirect by id, so the confirmation can say
	// they are still not on site and what to do next (heldOutReadyCopy).
	// Intersected here, not passed whole: a bus of twenty-six ticked at once
	// would otherwise put twenty-six ids in a URL to name one person.
	//
	// SchemaVersion 12 (R-16): a ticked id now splits three ways — still
	// held out (backfill/cross-site, heldOutQueryKey, unchanged); ENTERED by
	// this submission — no open visit here before it, one after it
	// (heldOutEnteredQueryKey; ⟦L05⟧ the visit is the signal, so a person the
	// held-out list had already stopped listing is still named when a
	// completion puts them on the muster); neither (rides no key).
	q := url.Values{"recorded": {strconv.Itoa(len(envs))}}
	openAfter := s.openPersonIDsHere()
	if held, err := domain.HeldOutAt(s.Store, s.Cfg.SiteID); err == nil {
		waiting := map[string]bool{}
		for _, h := range held {
			waiting[h.PersonID] = true
		}
		for _, id := range ids {
			switch {
			case waiting[id]:
				q.Add(heldOutQueryKey, id)
			case !openBefore[id] && openAfter[id]:
				q.Add(heldOutEnteredQueryKey, id)
			}
		}
	}
	http.Redirect(w, r, sealNotices(orientationPath+"?"+q.Encode()), http.StatusSeeOther)
}

// handleOrientationRetract withdraws ONE mis-ticked completion (D-95), own
// site only by construction (domain.RetractOrientation).
func (s *Server) handleOrientationRetract(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	id := strings.TrimSpace(r.FormValue("person_id"))
	if _, err := domain.RetractOrientation(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, id, s.postAttribution(), 0, "default"); err != nil {
		var ref *domain.OrientationRefusal
		if errors.As(err, &ref) {
			s.renderOrientationRefusal(w, r, s.orientationRefusalCopy(ref))
			return
		}
		page, perr := s.buildOrientationPage("")
		if perr == nil {
			page.InternalError = true
			s.renderPageStatus(w, http.StatusInternalServerError, "orientation", "orientation.html", page)
			return
		}
		http.Error(w, "this post could not complete that — nothing was recorded", http.StatusInternalServerError)
		return
	}
	name := id
	if p, err := domain.PersonByID(s.Store, id); err == nil && p != nil {
		name = p.Name
	}
	http.Redirect(w, r, sealNotices(orientationPath+"?retracted="+url.QueryEscape(name)), http.StatusSeeOther)
}

// orientationRefusalCopy writes the officer's sentence for a typed refusal,
// with this post's display name where domain only knows its id.
func (s *Server) orientationRefusalCopy(ref *domain.OrientationRefusal) string {
	who := ref.PersonName
	if who == "" {
		who = ref.PersonID
	}
	site := s.Cfg.PostName()
	switch ref.Code {
	case domain.OrientationPersonRequired:
		return "Nobody was named — nothing was recorded."
	case domain.OrientationPersonUnknown:
		return fmt.Sprintf("%s is not on this post's roster — nothing was recorded. Orientation does not enrol anyone; enrol them at intake first.", who) // D-18 ⟦fix4 H: was printed to the officer⟧
	case domain.OrientationAlreadyHeld:
		by := ref.HeldBy
		if by == "" {
			by = "an unattributed post"
		}
		return fmt.Sprintf("%s already holds %s's orientation (recorded by %s) — nothing was recorded for this submission. Untick them and submit again.", who, site, by)
	case domain.OrientationNotHeld:
		return fmt.Sprintf("%s does not hold %s's orientation — there is nothing to retract.", who, site)
	case domain.OrientationEnteredCannotRetract:
		return fmt.Sprintf("%s's orientation record entered them on site; a person on site cannot become un-oriented — complete their orientation, or retract after they have left.", who)
	}
	return "That could not be recorded."
}

func (s *Server) renderOrientationRefusal(w http.ResponseWriter, r *http.Request, sentence string) {
	s.renderOrientationRefusalLinks(w, r, sentence, nil)
}

// renderOrientationRefusalLinks is renderOrientationRefusal with amend links
// beside the sentence (F12).
func (s *Server) renderOrientationRefusalLinks(w http.ResponseWriter, r *http.Request, sentence string, links []amendLink) {
	page, err := s.buildOrientationPage("")
	if err != nil {
		http.Error(w, "the orientation list could not be read — nothing was recorded", http.StatusInternalServerError)
		return
	}
	page.Refusal = sentence
	page.RefusalAmend = links
	s.renderPageStatus(w, http.StatusConflict, "orientation", "orientation.html", page)
}
