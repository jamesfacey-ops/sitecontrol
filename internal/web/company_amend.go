// company_amend.go — the officer-facing COMPANY amendment surface (W5d,
// item 12). One screen does all of it: correct a company's name, merge two
// duplicate company records (same_as), un-merge (clear = same_as).
//
// GATING IS D-51's, and it is its OWN ruling. Both routes sit behind
// s.requireLogin — the same gating as /person/amend — because James ruled
// this surface officer-facing on 2026-08-31 (JAMES-DECISIONS D-51). D-46 is
// deliberately NOT cited for it: D-46 ruled *person* merge out from behind
// dev_routes, and carrying that to company would be a ruling about one
// entity generalised to another. The company answer needed its own
// authority and has one. The accepted price is stated in the ruling: a
// company merge is consequential and is now reachable by anyone who can log
// in — the same price D-46 accepted for people.
//
// THERE IS NO SEPARATE MERGE AFFORDANCE. Company same_as is an amendment
// like any other and is authored through this one POST (S6's locked form:
// "the same POST path, never a parallel author").
//
// The POST appends (or refuses PRE-APPEND — domain.AmendCompany holds every
// refusal; a refused POST authors nothing) and then follows the house PRG
// pattern with the visible-failure confirmation contract
// (design/visible-failure-v1.md §5): 303 back to this surface with
// ?recorded=<site_id>.<epoch>.<seq>, and the GET computes the outcome AT
// RENDER TIME. Read-time truth, never POST-time cache.
//
// TWO THINGS HERE ARE DELIBERATELY *NOT* CLONED FROM person_amend.go, and
// both are criteria:
//
//   - W5d-24: the internal-error arm renders NO raw error string.
//     person_amend.go:240–242 passes err.Error() to its page on ANY error,
//     so an internal fault reaches an officer as a raw
//     `sql: database is closed` under the word REFUSED [MEASURED on the
//     clean tree — that is this criterion's genuine pre-chunk red]. Here a
//     REFUSAL (domain.CompanyAmendRefusal — authored copy) is rendered and
//     an INTERNAL error is LOGGED, with authored copy on the page. W5
//     owes the two surfaces it touches and no others; every other
//     err.Error() render in this package stays in W4's register, owed to
//     whoever next touches each surface.
//   - W5-X2: a refused POST re-renders this form with the officer's typing
//     PRESERVED as value="…" on the inputs. (W5d does NOT retrofit the
//     person amendment form, which carries zero value= attributes — that
//     is a change to shipped behaviour outside this chunk's scope, and it
//     is recorded as a decision rather than silently inherited.)
//
// This surface also carries VF §5 RENDERING 2's amendment variant — the
// WINNING post's side — built per D-53, in which James chose the FIX arm
// over accept-and-record. Its observable is part of the discharge, not just
// its means: the winning officer is told, in words, on this page, that
// their amendment took the place of a peer's. They are the one person
// present at the moment the peer's loss becomes real, and the only one who
// can radio about it.
package web

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"

	"sitecontrol/internal/domain"
)

func (s *Server) registerCompanyAmendRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /company/amend", s.requireLogin(s.handleCompanyAmendPage))
	mux.HandleFunc("POST /company/amend", s.requireLogin(s.handleCompanyAmend))
}

// companyAmendFields lists the form's value fields, named after the payload
// keys (the wire spelling IS the form spelling, so nothing translates).
//
// ⚠ The set is EXACTLY {name, same_as} and nothing else (W5d-22(a)).
// `retired` is absent from this list, from the amendable-fact list, and
// therefore from this surface — D-41.1: "a company can be corrected but
// never retired". trade_id/phone/role/pad_oriented/position and the name
// parts have no company limb at all.
var companyAmendFields = []companyAmendField{
	{Key: "name", Label: "Company name"},
	{Key: "same_as", Label: "Same as (merge into this company id)"},
}

// companyAmendField carries the officer's typed Value back onto the form
// after a refusal (W5-X2), so the control itself is what preserves it
// rather than the refusal sentence quoting it back.
type companyAmendField struct {
	Key   string
	Label string
	Value string
}

type companyAmendOutcomeRow struct {
	Fact       string
	Effective  bool
	UnMerge    bool   // the superseded assertion was an un-merge (clear=same_as)
	WinnerPost string // display name of the post whose assertion stands
	// ⟦L-1⟧ WinnerAtThisPost: the assertion that stands was authored HERE,
	// by a later amendment at this same post — so there is nobody to radio.
	// The person surface's field of the same name carries the full note;
	// the two surfaces derive it identically, from the winner's site id on
	// the LOSING arm.
	WinnerAtThisPost bool
	// SupersededPost is VF §5 RENDERING 2's amendment variant (D-53): when
	// THIS assertion is the one that stands and it displaced an earlier
	// one, this is where that earlier amendment was made — L-1's form, so
	// "this post" when the two share a site_id and the peer's
	// config-derived display name otherwise. Empty when nothing was
	// displaced, which is the ordinary case and stays terse.
	SupersededPost string
}

type companyAmendOutcomeView struct {
	// NotThisKind ⟦D22, fix2-fj 2026-10-04⟧: the key names an event of
	// another kind — see amendOutcomeView.NotThisKind.
	NotThisKind bool
	Key         string
	// Failed: rendering 4 — the outcome could not be determined. The
	// append DID succeed; what failed is the read-back, and the officer
	// must know the confirmation is not vouching for the outcome.
	Failed bool
	Rows   []companyAmendOutcomeRow
}

// companyAmendPage is the render model. The three typed values ride it so a
// refusal re-renders the form with the officer's typing intact (W5-X2): a
// refusal that discards it makes the officer re-enter everything, and a
// strings.Contains over the body cannot see the difference, because the
// refusal sentence quotes the value back.
type companyAmendPage struct {
	pageShell // top bar: post, alert banner (pageshell.go)

	SiteID string
	Cfg    any

	Fields []companyAmendField

	// Error is an AUTHORED fact-domain refusal sentence — never a raw error
	// string (W5d-24). The other two error classes are their own fields, so
	// the page can say which of the three happened rather than flattening
	// them: InputError is an AUTHORED sentence too (the officer named a
	// record this post cannot act on), and InternalError is a bare flag
	// because a real fault's text is exactly what must NOT reach the page.
	Error         string
	InputError    string
	InternalError bool

	CompanyID string
	Name      string
	SameAs    string
	Clear     string

	// The standing-cost line (D-28), derived exactly as the person surface
	// derives it: HasPeer gates the line; StandingOther is the peer named
	// in it; StandingTaker is the GREATER site_id's display name — "this
	// post" when that is this post, never a hardcoded name.
	HasPeer       bool
	StandingOther string
	StandingTaker string

	Outcome *companyAmendOutcomeView

	// SameNames ⟦F-SETUP-1, 2026-10-03⟧: every company name carried by MORE
	// THAN ONE company row, with each row's id, the post that added it and
	// its roster headcount. Two rows with one name are indistinguishable on
	// a picker and print as two identical muster headings; this surface is
	// where the officer types the id to merge them (same_as), so it is
	// where the ids are listed beside the one fact that tells them apart —
	// origin — with the hint to use Same as. Nothing is merged for them: the
	// default is no auto-merge, and a same-name pair minted at two posts
	// while partitioned (L04 R-071/R-072) may be two companies.
	SameNames []companySameNameGroup
}

// companySameNameGroup is one shared company name and the rows carrying it.
type companySameNameGroup struct {
	Name string
	Rows []companySameNameRow
}

// companySameNameRow is one of the rows sharing a name: its id, the display
// name of the post that added it, how many people are enrolled under it,
// and the id it is already merged into (empty when it stands on its own).
type companySameNameRow struct {
	ID         string
	Post       string
	Headcount  int
	MergedInto string
}

// companySameNameGroups ⟦F-SETUP-1⟧ reads the composed companies and groups
// them by the ONE company fold (domain.SameCompanyName — the fold the create
// doors and the rename guard use), keeping only names carried by two or more
// rows. Name order, then id order, so the list is the same at every post.
func (s *Server) companySameNameGroups() ([]companySameNameGroup, error) {
	companies, err := domain.Companies(s.Store)
	if err != nil {
		return nil, err
	}
	persons, err := domain.Persons(s.Store)
	if err != nil {
		return nil, err
	}
	headcount := map[string]int{}
	for _, p := range persons {
		if p.CompanyID != nil {
			headcount[*p.CompanyID]++
		}
	}
	merge, err := domain.LoadCompanyMergeMap(s.Store)
	if err != nil {
		return nil, err
	}
	sort.Slice(companies, func(i, j int) bool {
		if companies[i].Name != companies[j].Name {
			return companies[i].Name < companies[j].Name
		}
		return companies[i].CompanyID < companies[j].CompanyID
	})
	var out []companySameNameGroup
	used := make([]bool, len(companies))
	for i, c := range companies {
		if used[i] {
			continue
		}
		g := companySameNameGroup{Name: c.Name}
		for j := i; j < len(companies); j++ {
			if used[j] || !domain.SameCompanyName(companies[j].Name, c.Name) {
				continue
			}
			used[j] = true
			row := companySameNameRow{ID: companies[j].CompanyID, Post: s.peerDisplayName(companies[j].SiteID), Headcount: headcount[companies[j].CompanyID]}
			if canon := merge.Resolve(companies[j].CompanyID); canon != companies[j].CompanyID {
				row.MergedInto = canon
			}
			g.Rows = append(g.Rows, row)
		}
		if len(g.Rows) > 1 {
			out = append(out, g)
		}
	}
	return out, nil
}

// supersededPostName renders the post where a DISPLACED amendment was made,
// in L-1's form: "this post" when it is this post — which is the whole
// content of L-1 and is why peerDisplayName (which answers with this post's
// own configured name) cannot serve here — the destination display name for
// a peer, and the bare id when config knows no better (never a blank).
func (s *Server) supersededPostName(siteID string) string {
	if siteID == s.Cfg.SiteID {
		return "this post"
	}
	return s.Cfg.SiteName(siteID)
}

func (s *Server) buildCompanyAmendPage(refusal string, typed companyAmendPage, outcome *companyAmendOutcomeView) companyAmendPage {
	typed.pageShell = s.shell()
	hasPeer, other, taker := s.standingLine()
	typed.SiteID = s.Cfg.SiteID
	typed.Cfg = s.Cfg
	// The typed values ride the CONTROLS, so a refusal re-renders the form
	// with the officer's typing intact (W5-X2).
	typed.Fields = make([]companyAmendField, len(companyAmendFields))
	copy(typed.Fields, companyAmendFields)
	for i := range typed.Fields {
		switch typed.Fields[i].Key {
		case "name":
			typed.Fields[i].Value = typed.Name
		case "same_as":
			typed.Fields[i].Value = typed.SameAs
		}
	}
	typed.Error = refusal
	typed.HasPeer = hasPeer
	typed.StandingOther = other
	typed.StandingTaker = taker
	typed.Outcome = outcome
	// ⟦F-SETUP-1⟧ loud on failure, never fatal: the form still renders; a
	// hint list that could not be read is logged and omitted rather than
	// turning an amendment page into a 500.
	if groups, err := s.companySameNameGroups(); err != nil {
		log.Printf("company amend: same-name groups: %v", err)
	} else {
		typed.SameNames = groups
	}
	return typed
}

// buildCompanyOutcomeView computes the confirmation block for one recorded
// key, at RENDER time (VF §4.3 rule 2: read-time truth, never POST-time
// cache).
//
// The competitor enumeration is domain.Competitors — the one exported API
// D-53's FIX arm adds BESIDE the pinned FactOutcome, sharing one internal
// resolution pass with domain.Outcome so the rule deciding which events
// compete exists in a single spelling. It is read only for facts that
// STAND: rendering 2 is the winner's side, and the loser's side is the
// adverse register below it.
func (s *Server) buildCompanyOutcomeView(recorded string) *companyAmendOutcomeView {
	// ⟦D22⟧ checked here, not in the shared body: /trade reaches
	// buildRecordedOutcomeView only after its own trade-kind check.
	if s.recordedNamesOtherKind(recorded, domain.EventCompanyAmended) {
		return &companyAmendOutcomeView{NotThisKind: true, Key: recorded}
	}
	return s.buildRecordedOutcomeView(recorded, "company amend")
}

// buildRecordedOutcomeView is the shared body behind buildCompanyOutcomeView
// and, since ⟦R-081 / M-17, 2026-10-03⟧, /trade's outcome block
// (web/trade.go): the same read-time Outcome + Competitors pass, the same
// four renderings. `what` names the surface in the log line only. The trade
// page had NO outcome view at all — a losing rename (same post or cross-post,
// proven under partition) was confirmed with a bare green "Recorded." (L06
// R-081/R-106, measured twice), which D-28 calls the project's signature
// defect; it now computes exactly what the person and company pages do.
func (s *Server) buildRecordedOutcomeView(recorded, what string) *companyAmendOutcomeView {
	k, ok := parseRecordedKey(recorded)
	if !ok {
		return &companyAmendOutcomeView{Failed: true}
	}
	outs, err := domain.Outcome(s.Store, k)
	if err != nil || len(outs) == 0 {
		return &companyAmendOutcomeView{Failed: true}
	}
	competitors, cerr := domain.Competitors(s.Store, k)
	if cerr != nil {
		// The outcome itself resolved; only the competitor enumeration did
		// not. Rendering 4 is the honest answer — a confirmation that
		// quietly drops half of what it was asked to say is the silent-OK
		// this whole surface exists to refuse.
		log.Printf("%s: competitors %v: %v", what, k, cerr)
		return &companyAmendOutcomeView{Failed: true}
	}
	view := &companyAmendOutcomeView{}
	for _, o := range outs {
		row := companyAmendOutcomeRow{Fact: o.Fact, Effective: o.Status == domain.Effective}
		if row.Effective {
			// VF §5 rendering 2, the amendment variant (D-53). The post
			// named is the LATEST displaced assertion in record order —
			// the one this amendment immediately took the place of.
			if peers := competitors[o.Fact]; len(peers) > 0 {
				row.SupersededPost = s.supersededPostName(peers[len(peers)-1].SiteID)
			}
		} else {
			row.WinnerPost = s.peerDisplayName(o.Winner.SiteID)
			// ⟦L-1⟧ The same-post signal, on the LOSING arm — the mirror of
			// supersededPostName's same-post form on the winner's arm above.
			row.WinnerAtThisPost = o.Winner.SiteID == s.Cfg.SiteID
			// Clear: ["same_as"] IS the un-merge (P-3 §5); its canonical
			// Asserted rendering is the registry's, and it is what lets
			// this surface say "the merge stands" in the un-merge's own
			// words rather than a generic supersession line.
			row.UnMerge = o.Fact == "same_as" && o.Asserted == "un-merge"
		}
		view.Rows = append(view.Rows, row)
	}
	return view
}

func (s *Server) handleCompanyAmendPage(w http.ResponseWriter, r *http.Request) {
	var outcome *companyAmendOutcomeView
	if rec := noticeQuery(r).Get(recordedQueryKey); rec != "" {
		outcome = s.buildCompanyOutcomeView(rec)
	}
	s.renderPage(w, "company amend", "company_amend.html",
		s.buildCompanyAmendPage("", companyAmendPage{}, outcome))
}

// handleCompanyAmend authors one company amendment. Every fact-domain
// refusal is domain.AmendCompany's and is PRE-APPEND: a refused POST
// re-renders this page with the refusal and the officer's typing, an honest
// 409, and nothing in the log.
func (s *Server) handleCompanyAmend(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	typed := companyAmendPage{
		CompanyID: strings.TrimSpace(r.FormValue("company_id")),
		Name:      strings.TrimSpace(r.FormValue("name")),
		SameAs:    strings.TrimSpace(r.FormValue("same_as")),
		Clear:     strings.TrimSpace(r.FormValue("clear")),
	}
	in := domain.CompanyAmendment{CompanyID: typed.CompanyID}
	// An empty form field is ABSENT — the officer did not touch it — never
	// an assertion of empty-string. Un-setting a field is `clear`, not an
	// empty submission (W5d-18's distinction, carried through the form).
	if typed.Name != "" {
		in.Name = &typed.Name
	}
	if typed.SameAs != "" {
		in.SameAs = &typed.SameAs
	}
	for _, c := range r.Form["clear"] {
		if c = strings.TrimSpace(c); c != "" {
			in.Clear = append(in.Clear, c)
		}
	}

	// No typed value here feeds the envelope: the amendment forms type no
	// instant at all, and the amendment act's own envelope pair is
	// default-captured (the route's EXEMPT classification, VF §7.5 item 4 —
	// the row applied to w2RouteTable in this same commit, per W5d-23(b)).
	env, err := domain.AmendCompany(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, in, 0, "default")
	if err != nil {
		// W5d-24. A REFUSAL's sentence is authored copy and belongs on the
		// page; an INTERNAL error's text is not and does not. The two are
		// separable only because the domain says which is which by TYPE —
		// reading the string would be the same guess in a different place.
		//
		// ⚠ THE PARTITION IS THREE-WAY BECAUSE THE DOMAIN HAS THREE CLASSES.
		// A two-way split over three classes does not merely lose detail: it
		// puts one whole class on the wrong arm, and the class it loses is
		// the officer's most likely mistake. Measured on the build this
		// replaces, a mistyped company id was answered with HTTP 500 and
		// "this post could not complete the amendment … try again, and if it
		// repeats, radio it through" — every clause false, on a surface with
		// no company picker. The page must say which of the three happened,
		// because the officer's next action differs in each: fix the
		// amendment, fix the id, or raise a fault.
		var refusal *domain.CompanyAmendRefusal
		if errors.As(err, &refusal) {
			s.renderPageStatus(w, http.StatusConflict, "company amend", "company_amend.html",
				s.buildCompanyAmendPage(refusal.Reason, typed, nil))
			return
		}
		// INPUT VALIDITY: the officer named a record this post cannot act on.
		// Same 409 as a refusal — nothing was recorded and the entry is the
		// thing to correct — and the domain's own authored sentence, which
		// was previously discarded into the log, is what the page carries.
		// This is the status and shape the person surface has always used
		// for the same mistake.
		var bad *domain.CompanyAmendInputError
		if errors.As(err, &bad) {
			typed.InputError = bad.Reason
			s.renderPageStatus(w, http.StatusConflict, "company amend", "company_amend.html",
				s.buildCompanyAmendPage("", typed, nil))
			return
		}
		// INTERNAL: unchanged, and it keeps W5d-24's property — the fault is
		// LOGGED and the page shows authored copy, never an err.Error()
		// value. What changed is that it is now reached only by a real
		// fault, which is what makes its "try again … radio it through"
		// true.
		log.Printf("company amend: %v", err)
		typed.InternalError = true
		s.renderPageStatus(w, http.StatusInternalServerError, "company amend", "company_amend.html",
			s.buildCompanyAmendPage("", typed, nil))
		return
	}
	http.Redirect(w, r, sealNotices(fmt.Sprintf("/company/amend?recorded=%s.%d.%d",
		env.SiteID, env.NodeEpoch, env.Seq)), http.StatusSeeOther)
}
