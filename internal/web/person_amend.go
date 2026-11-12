// person_amend.go — the officer-facing person amendment surface (W3,
// item 4; D-46: person amendment and merge are an OFFICER action, out from
// behind dev_routes — the ruling gates the surface, the design here is
// W3's). One screen does all of it: amend a field, clear a field, merge
// two records (same_as), un-merge (clear=same_as).
//
// The POST appends (or refuses PRE-APPEND — domain.AmendPerson holds every
// refusal; a refused POST authors nothing) and then follows the house PRG
// pattern with the visible-failure confirmation contract
// (design/visible-failure-v1.md §5): 303 back to this surface with
// ?recorded=<site_id>.<epoch>.<seq>, and the GET computes the outcome AT
// RENDER TIME by calling domain.Outcome on that key — read-time truth,
// never POST-time cache. A ?recorded= that does not parse, or an Outcome
// error, renders §5's rendering 4 LOUDLY ("outcome could not be
// determined") with the terse success register absent — the intake.go
// precedent of silently ignoring an unmatched query key is explicitly not
// followed here (a swallowed ?recorded= is a silent-OK).
//
// This surface also carries D-28's standing-cost line ("write it where
// officers will meet it"): a static sentence DERIVED from config — the
// greater site_id's display name takes precedence — never a hardcoded
// post name (VF §5's derivation rule; T10).
package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

func (s *Server) registerPersonAmendRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /person/amend", s.requireLogin(s.handlePersonAmendPage))
	mux.HandleFunc("POST /person/amend", s.requireLogin(s.handlePersonAmend))
}

// amendField is one of the form's value fields, named after the payload key
// (the wire spelling IS the form spelling, so nothing translates). Value
// ⟦R-132, 2026-10-03⟧ carries the officer's typing back onto the control
// after a refusal — the W5-X2 rule the company, trade and vehicle forms
// already kept and this one did not: a 409 here re-rendered every box blank,
// id included (L06 R-132, measured twice).
type amendField struct {
	Key   string
	Label string
	Value string
}

// amendFields lists the form's value fields, in render order.
var amendFields = []amendField{
	{Key: "name", Label: "Full name"},
	{Key: "last_name", Label: "Last name"},
	{Key: "first_name", Label: "First name"},
	{Key: "phone", Label: "Phone"},
	{Key: "company_id", Label: "Company id"},
	{Key: "trade_id", Label: "Trade id"},
	{Key: "role", Label: "Role"},
	// ⟦SchemaVersion 11⟧ {"pad_oriented", "Pad oriented"} is struck with the
	// fact ([[D-88]] ERRATA / [[D-91]]): orientation is the ORIENTING post's
	// own record, not a box on the enrolling site's person form. A POST that
	// still carries the key is ignored here and, asserting nothing else, is
	// refused by the edge as changing nothing.
	{Key: "position", Label: "Position"},
	{Key: "retired", Label: "Retired"},
	{Key: "same_as", Label: "Same as (merge into this person id)"},
}

type amendOutcomeRow struct {
	Fact       string
	Effective  bool
	UnMerge    bool   // the superseded assertion was an un-merge (clear=same_as)
	WinnerPost string // display name of the post whose assertion stands
	// ⟦L-1⟧ WinnerAtThisPost: the assertion that stands was authored HERE,
	// by a later amendment at this same post — so there is nobody to radio.
	// It is a SIGNAL, not a name: peerDisplayName answers with this post's
	// own configured display name ("Ice Road"), which reads as a different
	// post entirely and is the exact byte that made the losing arm's radio
	// order impossible to follow. It is a separate bool rather than a
	// special-cased name because peerDisplayName is shared with the
	// time-correction surface and with D-28's standing line, and changing
	// what it returns would change their copy too.
	WinnerAtThisPost bool
}

type amendOutcomeView struct {
	// NotThisKind ⟦D22, fix2-fj 2026-10-04⟧: the key names a real event at
	// this post of ANOTHER kind (a company rename, a trade change …). Nothing
	// was amended by this page, so it says so — the T-199 / R-080 rule
	// /time/correct and /trade already follow — instead of confirming the
	// other record's outcome as this one's. Key is echoed, escaped.
	NotThisKind bool
	Key         string
	// Failed: rendering 4 — the outcome could not be determined. The
	// append DID succeed; what failed is the read-back, and the officer
	// must know the confirmation is not vouching for the outcome.
	Failed bool
	Rows   []amendOutcomeRow
}

// personAmendPage is the render model.
type personAmendPage struct {
	pageShell // top bar: post, alert banner (pageshell.go)

	SiteID string
	Cfg    any

	Fields []amendField
	Error  string

	// PrefillPersonID ⟦gate-checkin-spec, 2026-09-13⟧: the person id box's
	// starting value, from the gate roster's "Edit person" button
	// (gate_edit_select.go) — ?person_id= on the initial GET — and, since
	// ⟦R-132, 2026-10-03⟧, the id the officer TYPED on a refused POST, so a
	// refusal no longer costs them the id along with everything else. ⟦was:
	// "Empty on a refused POST's re-render … widening it is a separate,
	// unasked-for change"⟧ — it was asked for: L06 R-132.
	PrefillPersonID string
	// Clear ⟦R-132⟧ is the Clear box's typing, kept across a refusal like
	// the value fields.
	Clear string

	// The standing-cost line (D-28), derived: HasPeer gates the line;
	// StandingOther is the peer named in it; StandingTaker is the GREATER
	// site_id's display name — "this post" when that is this post.
	HasPeer       bool
	StandingOther string
	StandingTaker string

	Outcome *amendOutcomeView
}

// peerDisplayName renders a site id as its configured display name: this
// post's own name for itself, the destination display name for a peer, and
// the bare id when config knows no better (never a blank).
func (s *Server) peerDisplayName(siteID string) string {
	return s.Cfg.SiteName(siteID)
}

// standingLine derives the precedence sentence's two names from the config
// this server actually loaded (VF §5's derivation rule; test T10): the
// GREATER site_id takes precedence — rendered as "this post" when this
// post is the greater party, never as a hardcoded post name.
func (s *Server) standingLine() (hasPeer bool, other, taker string) {
	takerSite := s.Cfg.SiteID
	taker = "this post"
	for _, p := range s.Cfg.Peers {
		hasPeer = true
		if other == "" {
			other = s.peerDisplayName(p.SiteID)
		}
		if p.SiteID > takerSite {
			takerSite = p.SiteID
			taker = s.peerDisplayName(p.SiteID)
			other = taker
		}
	}
	return hasPeer, other, taker
}

// personAmendTyped is what the officer had on the form when a POST was
// refused ⟦R-132⟧: the id, each value field by key, and the Clear box. The
// zero value renders a blank form (the initial GET).
type personAmendTyped struct {
	PersonID string
	Values   map[string]string
	Clear    string
}

func (s *Server) buildPersonAmendPage(refusal string, outcome *amendOutcomeView, typed personAmendTyped) personAmendPage {
	hasPeer, other, taker := s.standingLine()
	// The typed values ride the CONTROLS (W5-X2's rule, now on this form
	// too): a fresh copy of the field list, each Value filled from what was
	// posted under that key.
	fields := make([]amendField, len(amendFields))
	copy(fields, amendFields)
	for i := range fields {
		fields[i].Value = typed.Values[fields[i].Key]
	}
	return personAmendPage{
		pageShell:       s.shell(),
		SiteID:          s.Cfg.SiteID,
		Cfg:             s.Cfg,
		Fields:          fields,
		Error:           refusal,
		HasPeer:         hasPeer,
		StandingOther:   other,
		StandingTaker:   taker,
		Outcome:         outcome,
		PrefillPersonID: typed.PersonID,
		Clear:           typed.Clear,
	}
}

// parseRecordedKey parses ?recorded=<site_id>.<epoch>.<seq> RIGHT-ANCHORED,
// exactly as VF §5 fixes it: seq and epoch are the last two `.`-separated
// fields; everything left of them is the site_id. false = does not parse —
// which the caller renders as rendering 4, loudly, never as an omitted
// block.
func parseRecordedKey(v string) (store.Key, bool) {
	parts := strings.Split(v, ".")
	if len(parts) < 3 {
		return store.Key{}, false
	}
	epoch, err := strconv.ParseInt(parts[len(parts)-2], 10, 64)
	if err != nil {
		return store.Key{}, false
	}
	seq, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	if err != nil {
		return store.Key{}, false
	}
	siteID := strings.Join(parts[:len(parts)-2], ".")
	if siteID == "" || epoch < 1 || seq < 1 {
		return store.Key{}, false
	}
	return store.Key{SiteID: siteID, NodeEpoch: epoch, Seq: seq}, true
}

// recordedOtherKind ⟦D22, fix2-fj 2026-10-04⟧ reports whether k names an
// EXISTING event whose kind is none of `kinds` — the "nothing to confirm"
// arm, the same rule as /time/correct's T-199 and /trade's R-080. A key
// naming no event, or a read failure, is NOT this arm: those stay rendering
// 4 (VF-m5b), which since D23 a page reaches only off a sealed redirect this
// process wrote — a forged ?recorded= renders no notice at all.
func (s *Server) recordedOtherKind(k store.Key, kinds ...string) bool {
	typ, err := domain.EventTypeAt(s.Store, k)
	if err != nil || typ == "" {
		return false
	}
	for _, want := range kinds {
		if typ == want {
			return false
		}
	}
	return true
}

// recordedNamesOtherKind ⟦F11 — the D22 residual, fix3 2026-10-04⟧ is the
// "nothing to confirm" test every ?recorded= page uses: recorded is a key
// naming an existing event of none of `kinds` (recordedOtherKind), OR it is
// not a key at all but a TRADE id — /trade's create confirmation carries the
// new trade's id in the same ?recorded= slot (trade.go). ⟦was: only the
// event-key arm, so a genuinely sealed trade id reached rendering 4,
// "Recorded, but the outcome could not be determined", on the amend pages,
// and /arrivals/resolve and /arrivals/unref had no other-kind arm at all
// (L14-4) — saying something was recorded on a page where nothing was⟧. A
// garbled value, a key naming no event, or a read failure is still NOT this
// arm: those stay rendering 4 (VF-m5b), reachable only off a sealed redirect
// this process wrote (D23).
func (s *Server) recordedNamesOtherKind(recorded string, kinds ...string) bool {
	if k, ok := parseRecordedKey(recorded); ok {
		return s.recordedOtherKind(k, kinds...)
	}
	t, err := domain.TradeByID(s.Store, strings.TrimSpace(recorded))
	return err == nil && t != nil
}

// buildOutcomeView computes the confirmation block for one recorded key,
// at RENDER time (VF §4.3 rule 2: read-time truth, never POST-time cache).
func (s *Server) buildOutcomeView(recorded string) *amendOutcomeView {
	if s.recordedNamesOtherKind(recorded, domain.EventPersonAmended) {
		return &amendOutcomeView{NotThisKind: true, Key: recorded}
	}
	k, ok := parseRecordedKey(recorded)
	if !ok {
		return &amendOutcomeView{Failed: true}
	}
	outs, err := domain.Outcome(s.Store, k)
	if err != nil || len(outs) == 0 {
		return &amendOutcomeView{Failed: true}
	}
	view := &amendOutcomeView{}
	for _, o := range outs {
		row := amendOutcomeRow{Fact: o.Fact, Effective: o.Status == domain.Effective}
		if !row.Effective {
			row.WinnerPost = s.peerDisplayName(o.Winner.SiteID)
			// ⟦L-1⟧ The same-post signal. Read off the WINNER's site id on
			// the LOSING arm — the arm the officer standing at the winning
			// post reads about their own displaced assertion.
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

func (s *Server) handlePersonAmendPage(w http.ResponseWriter, r *http.Request) {
	var outcome *amendOutcomeView
	if rec := noticeQuery(r).Get(recordedQueryKey); rec != "" {
		outcome = s.buildOutcomeView(rec)
	}
	// ⟦gate-checkin-spec, 2026-09-13⟧ ?person_id= is the gate roster's "Edit
	// person" hand-off (gate_edit_select.go) — a plain prefill, not trusted
	// as anything: an id this box does not recognise just fails the POST's
	// own existing "person is not on the roster" refusal, exactly as if the
	// officer had typed it by hand.
	prefill := strings.TrimSpace(r.URL.Query().Get("person_id"))
	s.renderPage(w, "person amend", "person_amend.html", s.buildPersonAmendPage("", outcome, personAmendTyped{PersonID: prefill}))
}

// handlePersonAmend authors one amendment. Every refusal is
// domain.AmendPerson's and is PRE-APPEND: a refused POST re-renders this
// page with the refusal, an honest 409, and nothing in the log.
func (s *Server) handlePersonAmend(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	in := domain.PersonAmendment{PersonID: strings.TrimSpace(r.FormValue("person_id"))}
	// ⟦R-132⟧ what was typed, captured BEFORE any refusal so the re-render
	// can hand it back on the controls.
	typed := personAmendTyped{PersonID: in.PersonID, Values: map[string]string{}, Clear: strings.TrimSpace(r.FormValue("clear"))}
	// An empty form field is ABSENT — the officer did not touch it — never
	// an assertion of empty-string. Un-setting a field is `clear`, not an
	// empty submission (W3-3's distinction, carried through the form).
	setField := func(dst **string, key string) {
		if v := strings.TrimSpace(r.FormValue(key)); v != "" {
			typed.Values[key] = v
			*dst = &v
		}
	}
	setField(&in.Name, "name")
	setField(&in.LastName, "last_name")
	setField(&in.FirstName, "first_name")
	setField(&in.Phone, "phone")
	setField(&in.CompanyID, "company_id")
	setField(&in.TradeID, "trade_id")
	setField(&in.Role, "role")
	setField(&in.Position, "position")
	setField(&in.Retired, "retired")
	setField(&in.SameAs, "same_as")
	for _, c := range r.Form["clear"] {
		if c = strings.TrimSpace(c); c != "" {
			in.Clear = append(in.Clear, c)
		}
	}

	// No typed value here feeds the envelope: the amendment forms type no
	// instant at all, and the amendment act's own envelope pair is
	// default-captured (the route's EXEMPT classification, VF §7.5 item 4).
	env, err := domain.AmendPerson(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, in, 0, "default")
	if err != nil {
		s.renderPageStatus(w, http.StatusConflict, "person amend", "person_amend.html",
			s.buildPersonAmendPage(refusalText(err), nil, typed))
		return
	}
	http.Redirect(w, r, sealNotices(fmt.Sprintf("/person/amend?recorded=%s.%d.%d",
		env.SiteID, env.NodeEpoch, env.Seq)), http.StatusSeeOther)
}
