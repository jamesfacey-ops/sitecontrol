// trade.go — the officer-facing TRADE surface (W8, item 10): the
// naming-and-numbers screen, the add-a-trade door, and the amendment door
// that carries the three positional handset numbers and the merge pointer.
//
// GATING. All three routes sit behind s.requireLogin, and one of them is a
// GET — which is a departure from this package's standing rule that reads
// stay ungated, so it is argued rather than assumed. §12 names the purpose of
// the login as accountability for entries, not confidentiality, and that is
// why /muster, /trips and /gate render to anyone. This screen is different in
// kind: it is not a view of what happened, it is REFERENCE DATA — the site's
// whole trade list and, beside each trade, THE THREE POSITIONAL HANDSET
// NUMBERS. A phone book for the site, served to any TCP caller, is the shape
// D-56 has already ruled against once on this program, one route over.
//
// ⚠⚠ THE UNGATED GET IS THE MUTANT W8-20(a-i) EXISTS FOR, and it is invisible
// to the landed W2 walk: that walk admits only "POST /` literals and does not
// recurse, so a `GET /trade` registered without the wrapper is never
// discovered, never classified and never reverse-reconciled. It would pass
// every landed gating test in this package. The criterion's route set is
// therefore DERIVED, as a set difference against the pre-chunk tree, rather
// than listed — a hand-kept list is how a kill gets booked while the screen
// ships open.
//
// ⚠ NO RAW err.Error() REACHES THIS PAGE. The domain's three error classes
// are separated BY TYPE (domain.TradeAmendRefusal, TradeAmendInputError, and
// everything else), exactly as the company surface separates them and for the
// same measured reason: a mistyped id answered with HTTP 500 and "radio it
// through" is false in every clause, and it is the officer's likeliest
// mistake on a surface whose id is typed by hand.
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

func (s *Server) registerTradeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /trade", s.requireLogin(s.handleTradePage))
	mux.HandleFunc("POST /trade/create", s.requireLogin(s.handleTradeCreate))
	mux.HandleFunc("POST /trade/amend", s.requireLogin(s.handleTradeAmend))
}

// --- the phone slots, held at PACKAGE LEVEL ------------------------------
//
// ⚠⚠ THE PLACEMENT IS LOAD-BEARING AND IS THE SAME RULE trade_amend.go
// states. W8-4(a) counts every FUNCTION BODY that names a shift slot and the
// shared slot as a POSITIONAL RESOLVER and requires EXACTLY ONE in this whole
// program — domain.TradePhone. A handler that wrote out
// `view.Day = tr.PhoneDay; view.Shared = tr.PhoneShared` would be a second
// selection site by that census's definition, and the census is right: a
// function that can see all three slots can silently choose between them.
// Func literals in a package-level var are inside no function body, so this
// table is a mechanical guarantee rather than a convention.
//
// ⚠ THE TEMPLATE IS UNDER THE SAME RULE, by its own arm of the criterion: a
// `{{with .PhoneDay}}…{{else}}{{$.PhoneShared}}{{end}}` coalesce is a second
// implementation an AST walk over Go cannot see, and the {{with}}…{{else}}
// idiom is already live on this app's muster sheet. trade.html therefore
// RANGES over the slots below and never names one.
var tradePhoneSlots = []struct {
	Key   string
	Label string
	Get   func(domain.Trade) *string
}{
	{"phone_day", "Day shift", func(tr domain.Trade) *string { return tr.PhoneDay }},
	{"phone_night", "Night shift", func(tr domain.Trade) *string { return tr.PhoneNight }},
	{"phone_shared", "Shared", func(tr domain.Trade) *string { return tr.PhoneShared }},
}

// tradeAmendSetters maps a form field to the amendment field it asserts.
// Package-level for the reason above.
var tradeAmendSetters = map[string]func(*domain.TradeAmendment, *string){
	"name":         func(in *domain.TradeAmendment, v *string) { in.Name = v },
	"phone_day":    func(in *domain.TradeAmendment, v *string) { in.PhoneDay = v },
	"phone_night":  func(in *domain.TradeAmendment, v *string) { in.PhoneNight = v },
	"phone_shared": func(in *domain.TradeAmendment, v *string) { in.PhoneShared = v },
	"same_as":      func(in *domain.TradeAmendment, v *string) { in.SameAs = v },
}

// tradeAmendFields is the form's control set, in the order it renders. The
// keys ARE the payload spellings, so nothing translates between the form and
// the wire.
//
// ⚠ THERE IS NO `retired` CONTROL AND THERE NEVER WILL BE: D-30(b) rules that
// a trade is never retired, so it is absent from this list, from the
// amendable-fact list, and therefore from this screen.
var tradeAmendFields = []tradeAmendField{
	{Key: "name", Label: "Trade name"},
	{Key: "phone_day", Label: "Day-shift handset"},
	{Key: "phone_night", Label: "Night-shift handset"},
	{Key: "phone_shared", Label: "Shared handset"},
	{Key: "same_as", Label: "Same as (merge into this trade id)"},
}

type tradeAmendField struct {
	Key   string
	Label string
	// Value carries the officer's typing back onto the control after a
	// refusal, so a validation error does not make them re-enter everything.
	Value string
}

// --- the render model ----------------------------------------------------

// tradePhoneCell is one slot as the screen shows it.
type tradePhoneCell struct {
	Label string
	// Number is the slot's OWN recorded number, or "" when the slot is
	// absent. ⚠ It is NOT resolved: the screen shows what was recorded, slot
	// by slot, so an officer editing the numbers can see which of the three
	// they are actually changing.
	Number string
}

type tradeRow struct {
	TradeID string
	Name    string
	Phones  []tradePhoneCell
	// InEffectDay / InEffectNight are what a caller reaching for the handset
	// would actually get: domain.TradePhone, the ONE positional resolver.
	// They are shown beside the raw slots because "shared covers the night"
	// is exactly the thing an officer needs to be able to see is true.
	InEffectDay   string
	InEffectNight string
	// MergedInto is the canonical trade this one resolves to, or "" when it
	// stands on its own. A merged-away trade's row SURVIVES — same_as is a
	// per-field assertion, never a deletion — so it is shown, marked.
	MergedInto string
	// SameNameOrigin ⟦F-SETUP-1, 2026-10-03⟧ is the display name of the post
	// that added this trade, filled ONLY when another trade row carries the
	// same name (under the one trade fold) — the one fact that tells two
	// same-named rows apart, beside the hint to merge them with Same as if
	// they are one trade. Empty on a uniquely named row, so the table stays
	// quiet where there is nothing to tell apart.
	SameNameOrigin string
}

type tradePage struct {
	pageShell // top bar: post, alert banner (pageshell.go)

	SiteID string
	Cfg    any

	Trades []tradeRow
	Fields []tradeAmendField

	NewName string
	TradeID string

	// The three error classes, kept apart because the officer's next action
	// differs in each: fix the amendment, fix the id, or raise a fault.
	// InternalError is a bare flag because a real fault's TEXT is exactly
	// what must not reach the page.
	Error         string
	InputError    string
	InternalError bool

	// Recorded is the NAME of the trade /trade/create just added, when
	// ?recorded= names a trade row on this post; the terse "Recorded."
	// notice renders only then. ⟦R-081, 2026-10-03⟧ ⟦was: the raw query
	// value, echoed as "Recorded." whatever it was⟧.
	Recorded string
	// Outcome ⟦R-081 / M-17, 2026-10-03⟧ is the per-fact confirmation for an
	// AMENDMENT key (?recorded=<site>.<epoch>.<seq>), computed at render
	// time exactly as /person/amend and /company/amend compute theirs (VF
	// §5): a clean amendment stays terse; a superseded one says so and names
	// the post whose assertion stands; a key that resolves to nothing says
	// the outcome could not be determined — never a bare "Recorded.".
	Outcome *companyAmendOutcomeView
	// RecordedUnknown ⟦R-080, W4 2026-10-03⟧: the ?recorded= value names no
	// trade change at this post, so the page confirms nothing (see
	// checkTradeRecorded) — it used to echo "Recorded." for any value at all.
	RecordedUnknown bool
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func (s *Server) buildTradePage(typed tradePage, refusal string) tradePage {
	typed.pageShell = s.shell()
	typed.SiteID = s.Cfg.SiteID
	typed.Cfg = s.Cfg
	typed.Error = refusal

	// The officer's typing, if this call carries any: handleTradeAmend fills
	// typed.Fields in with what was actually typed (trade.go:270-272) BEFORE
	// a refusal re-render reaches here, and that must survive — a refusal is
	// not a reason to make the officer retype the whole amendment. Captured
	// by Key into `prior` before the fresh, correctly-labelled copy below
	// replaces the slice, then restored onto it after.
	//
	// ⚠ KEYED AND GENERIC ON PURPOSE, never a switch or a direct field name.
	// This function must never name phone_day/phone_night together with
	// phone_shared (as an identifier OR a string literal) in its own body:
	// that is a second W8-4(a) positional-resolver selection site by the
	// census's own definition (w8_merge_test.go), and the census counts by
	// syntax, not by intent — see the placement note on tradeAmendSetters
	// above for the identical constraint.
	prior := make(map[string]string, len(typed.Fields))
	for _, f := range typed.Fields {
		prior[f.Key] = f.Value
	}
	typed.Fields = make([]tradeAmendField, len(tradeAmendFields))
	copy(typed.Fields, tradeAmendFields)
	for i := range typed.Fields {
		typed.Fields[i].Value = prior[typed.Fields[i].Key]
	}

	trades, err := domain.Trades(s.Store)
	if err != nil {
		// The LIST failed to read. That is a fault, and it is reported as
		// one — an empty list rendered silently would tell an officer this
		// site has no trades, which is a different and much worse answer.
		log.Printf("trade page: trades: %v", err)
		typed.InternalError = true
		return typed
	}
	merge, err := domain.LoadTradeMergeMap(s.Store)
	if err != nil {
		log.Printf("trade page: trade merge map: %v", err)
		typed.InternalError = true
		return typed
	}
	// ⟦F-SETUP-1⟧ which names are carried by more than one row, under the
	// ONE trade fold (domain.SameTradeName — the fold the create door and
	// the rename guard use). A trade inside one site's list is a small
	// table, so the pairwise shape costs nothing measurable.
	shared := make([]bool, len(trades))
	for i := range trades {
		for j := i + 1; j < len(trades); j++ {
			if domain.SameTradeName(trades[i].Name, trades[j].Name) {
				shared[i], shared[j] = true, true
			}
		}
	}
	for i, tr := range trades {
		row := tradeRow{TradeID: tr.TradeID, Name: tr.Name}
		for _, slot := range tradePhoneSlots {
			row.Phones = append(row.Phones, tradePhoneCell{Label: slot.Label, Number: deref(slot.Get(tr))})
		}
		row.InEffectDay = deref(domain.TradePhone(tr, domain.ShiftDay))
		row.InEffectNight = deref(domain.TradePhone(tr, domain.ShiftNight))
		if canon := merge.Resolve(tr.TradeID); canon != tr.TradeID {
			row.MergedInto = canon
		}
		if shared[i] {
			row.SameNameOrigin = s.peerDisplayName(tr.SiteID)
		}
		typed.Trades = append(typed.Trades, row)
	}
	sort.Slice(typed.Trades, func(i, j int) bool {
		if typed.Trades[i].Name != typed.Trades[j].Name {
			return typed.Trades[i].Name < typed.Trades[j].Name
		}
		return typed.Trades[i].TradeID < typed.Trades[j].TradeID
	})
	return typed
}

func (s *Server) handleTradePage(w http.ResponseWriter, r *http.Request) {
	page := tradePage{}
	// ⟦R-081 / M-17, 2026-10-03⟧ ?recorded= is READ, never echoed. Two
	// shapes reach this page: an AMENDMENT key (<site>.<epoch>.<seq>, from
	// /trade/amend) gets the per-fact outcome computed at render time, the
	// same read the person and company pages make; a TRADE ID (from
	// /trade/create) is checked to name a trade row here before the terse
	// "Recorded." renders. Anything else — a forged or stale value — renders
	// "outcome could not be determined" (VF §5 rendering 4), as the sibling
	// surfaces do; this page used to answer every value with "Recorded.".
	if rec := noticeQuery(r).Get(recordedQueryKey); rec != "" {
		// ⟦merge W4+W5, 2026-10-03⟧ one rule: a key naming a TRADE amendment
		// gets W5's per-fact outcome; a key naming any other event, or no
		// event, confirms nothing (W4 R-080, "NOTHING TO CONFIRM"); a trade id
		// here confirms "Recorded."; anything else fails closed (W5).
		if k, isKey := parseRecordedKey(rec); isKey {
			if typ, err := domain.EventTypeAt(s.Store, k); err == nil && typ == domain.EventTradeAmended {
				page.Outcome = s.buildRecordedOutcomeView(rec, "trade")
			} else {
				page.RecordedUnknown, page.Recorded = true, rec
			}
		} else if tr, err := domain.TradeByID(s.Store, rec); err == nil && tr != nil {
			page.Recorded = tr.Name
		} else {
			page.RecordedUnknown, page.Recorded = true, rec
		}
	}
	s.renderPage(w, "trade", "trade.html", s.buildTradePage(page, ""))
}

// checkTradeRecorded ⟦R-080, W4 2026-10-03⟧ confirms "Recorded." only for
// what this screen's own two redirects carry: a NEW trade's id (the create
// redirect) that is on this site's list, or the key of a trade AMENDMENT
// (the amend redirect) that is in this post's log. Anything else — a typo,
// a hand-edited link, another kind of event's key — confirms nothing and
// says so. Whether an amendment that IS on record stands or was superseded
// is a separate question this check does not answer.
func (s *Server) checkTradeRecorded(page *tradePage) {
	if page.Recorded == "" || page.InternalError {
		return
	}
	if k, ok := parseRecordedKey(page.Recorded); ok {
		typ, err := domain.EventTypeAt(s.Store, k)
		if err != nil {
			log.Printf("trade page: recorded key %s: %v", page.Recorded, err)
		}
		if err == nil && typ == domain.EventTradeAmended {
			return
		}
	} else {
		for _, row := range page.Trades {
			if row.TradeID == page.Recorded {
				return
			}
		}
	}
	page.RecordedUnknown = true
}

// handleTradeCreate adds a trade to this site's list. The refusal for a name
// already on the list is the register's own site-wide uniqueness rule
// ⟦DR-W8-11⟧, and it says what to do instead rather than only what is wrong.
func (s *Server) handleTradeCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderTradeRefusal(w, tradePage{}, "That form did not arrive intact. Nothing was recorded. Reload the page and enter it again.")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	typed := tradePage{NewName: name}
	if name == "" {
		s.renderTradeRefusal(w, typed, "Type the trade's name. Nothing was recorded.")
		return
	}
	tr, err := domain.CreateTrade(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, name, 0, "default")
	if err != nil {
		// ⚠ NO RAW err.Error() HERE. CreateTrade's only officer-reachable
		// refusal is the duplicate name, and the sentence below is authored
		// for this screen; anything else is a fault and is logged, never
		// shown.
		//
		// ⚠ MATCHED BY TYPE, NEVER BY TEXT ([[D-76]]). This branch used to
		// test strings.Contains(err.Error(), "...trade list") against an
		// error that said "...list" — it matched nothing, was dead from the
		// day it shipped, and every duplicate answered a raw 500 (MEASURED:
		// u1c-tradeweb §4). A string-match cannot be told from a dead branch
		// by reading it; errors.As against the domain's own type can.
		// The sentence names the ON-LIST spelling — the row to pick — which
		// under the fold may differ from what was typed.
		var refusal *domain.TradeCreateRefusal
		if errors.As(err, &refusal) {
			s.renderTradeRefusal(w, typed, fmt.Sprintf("%q is already on this site's trade list — pick it from the list above instead of adding a second one. Nothing was recorded.", refusal.Existing))
			return
		}
		log.Printf("trade create: %v", err)
		typed.InternalError = true
		s.renderPageStatus(w, http.StatusInternalServerError, "trade", "trade.html", s.buildTradePage(typed, ""))
		return
	}
	http.Redirect(w, r, sealNotices(fmt.Sprintf("/trade?recorded=%s", tr.TradeID)), http.StatusSeeOther)
}

// handleTradeAmend authors one trade amendment: a corrected name, any of the
// three positional numbers, the merge pointer, or a clear of any of them.
//
// Every fact-domain refusal is domain.AmendTrade's and is PRE-APPEND — a
// refused POST re-renders this page with the refusal and the officer's
// typing, an honest 409, and nothing in the log.
func (s *Server) handleTradeAmend(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderTradeRefusal(w, tradePage{}, "That form did not arrive intact. Nothing was recorded. Reload the page and enter it again.")
		return
	}
	typed := tradePage{TradeID: strings.TrimSpace(r.FormValue("trade_id"))}
	in := domain.TradeAmendment{TradeID: typed.TradeID}
	typed.Fields = make([]tradeAmendField, len(tradeAmendFields))
	copy(typed.Fields, tradeAmendFields)
	for i := range typed.Fields {
		v := strings.TrimSpace(r.FormValue(typed.Fields[i].Key))
		typed.Fields[i].Value = v
		// An empty control is ABSENT — the officer did not touch it — never
		// an assertion of empty-string. Un-setting a fact is `clear`, not an
		// empty submission.
		if v == "" {
			continue
		}
		val := v
		tradeAmendSetters[typed.Fields[i].Key](&in, &val)
	}
	for _, c := range r.Form["clear"] {
		if c = strings.TrimSpace(c); c != "" {
			in.Clear = append(in.Clear, c)
		}
	}

	env, err := domain.AmendTrade(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, in, 0, "default")
	if err != nil {
		// ⚠ THE PARTITION IS THREE-WAY BECAUSE THE DOMAIN HAS THREE CLASSES.
		// A two-way split over three does not merely lose detail: it puts one
		// whole class on the wrong arm, and the class it loses is the
		// officer's most likely mistake.
		var refusal *domain.TradeAmendRefusal
		if errors.As(err, &refusal) {
			s.renderTradeRefusal(w, typed, refusal.Reason+"; nothing was recorded")
			return
		}
		var bad *domain.TradeAmendInputError
		if errors.As(err, &bad) {
			typed.InputError = bad.Reason + "; nothing was recorded"
			s.renderPageStatus(w, http.StatusConflict, "trade", "trade.html", s.buildTradePage(typed, ""))
			return
		}
		log.Printf("trade amend: %v", err)
		typed.InternalError = true
		s.renderPageStatus(w, http.StatusInternalServerError, "trade", "trade.html", s.buildTradePage(typed, ""))
		return
	}
	http.Redirect(w, r, sealNotices(fmt.Sprintf("/trade?recorded=%s.%d.%d",
		env.SiteID, env.NodeEpoch, env.Seq)), http.StatusSeeOther)
}

// renderTradeRefusal re-renders the screen as the refusal, at an honest 409,
// with the officer's typing intact.
func (s *Server) renderTradeRefusal(w http.ResponseWriter, typed tradePage, reason string) {
	s.renderPageStatus(w, http.StatusConflict, "trade", "trade.html", s.buildTradePage(typed, reason))
}
