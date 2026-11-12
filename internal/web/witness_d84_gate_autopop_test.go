// witness_d84_gate_autopop_test.go — [[D-84]] and its amendment, witnessed
// through the real handlers over the real templates.
//
// THE RULING. For an already-enrolled person the gate SHOWS company and
// trade from their record instead of asking; a change is made through
// intake-shaped pickers (a list plus a "not listed — add it" escape) and
// applies ONLY behind a confirmation; a confirmed change amends the PERSON
// RECORD — durably, through the ordinary amendment edge — and the visit
// still snapshots from the record at commit. New people are still ASKED
// ([[D-66]]) and the escape never refuses ([[D-71]] obligation 2).
//
// THE INSTRUMENT is the one reports/witness/d83-checkin-gap.md measured the
// gap with: the JS-off browser simulated from the served markup (gateBuild /
// gateFormValues / postForm), committing through the real handlers, reading
// back visit snapshots, record state and the event log. Every number below
// marked MEASURED came off a run of this file.
//
// What is NOT witnessed here: the browser-side script (gate.html showKnown /
// showNew). There is no JavaScript runtime in the toolchain; the clone
// template it fills is locked field-for-field to the server row by
// TestTheScriptedRowTemplateMatchesTheServerRenderedRow and
// TestW8ServerRowAndJSRowTemplateDoNotDrift.
package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// --- fixtures and small instruments ------------------------------------

// d84Server is the gate fixture plus a second trade and a second company, so
// a change has somewhere to go.
func d84Server(t *testing.T) (*Server, *store.Store, []domain.Person, domain.Trade, domain.Company) {
	t.Helper()
	srv, st, persons := newGateTestServer(t)
	pipe, err := domain.CreateTrade(st, "site-a", 1, "Pipefitter", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	nanuq, err := domain.CreateCompany(st, "site-a", 1, "Nanuq Drilling Services", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	return srv, st, persons, pipe, nanuq
}

func d84Person(t *testing.T, st *store.Store, id string) domain.Person {
	t.Helper()
	p, err := domain.PersonByID(st, id)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatalf("person %s vanished from the roster", id)
	}
	return *p
}

func d84Visits(t *testing.T, st *store.Store) []domain.Visit {
	t.Helper()
	v, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func d84Counts(t *testing.T, st *store.Store) (persons, companies, trades int) {
	t.Helper()
	ps, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := domain.Companies(st)
	if err != nil {
		t.Fatal(err)
	}
	ts, err := domain.Trades(st)
	if err != nil {
		t.Fatal(err)
	}
	return len(ps), len(cs), len(ts)
}

func d84Deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// d84Set sets index i of the parallel array `name` in a captured form body.
func d84Set(t *testing.T, vals url.Values, name string, i int, v string) {
	t.Helper()
	arr := vals[name]
	if i >= len(arr) {
		t.Fatalf("form body carries %d %s values; cannot set index %d", len(arr), name, i)
	}
	arr[i] = v
}

var (
	d84OpenDetailsRe   = regexp.MustCompile(`<details class="org-change p-org-change" open>`)
	d84ClosedDetailsRe = regexp.MustCompile(`<details class="org-change p-org-change">`)
	d84AnyDetailsRe    = regexp.MustCompile(`(?s)<details class="org-change p-org-change"(?: open)?>.*?</details>`)
	d84TextInputRe     = regexp.MustCompile(`(?s)<input\b[^>]*type="text"[^>]*>`)
	d84SelectRe        = regexp.MustCompile(`(?s)<select\b[^>]*>`)
	d84CheckboxRe      = regexp.MustCompile(`(?s)<input\b[^>]*type="checkbox"[^>]*>`)
)

// d84Operable strips every CLOSED change block from a form's markup. A shut
// <details> renders none of its contents and puts none of them in the tab
// order — they are not controls the officer can see or type into.
func d84Operable(form string) string {
	return d84AnyDetailsRe.ReplaceAllStringFunc(form, func(m string) string {
		if strings.HasPrefix(m, `<details class="org-change p-org-change" open>`) {
			return m
		}
		return ""
	})
}

// --- 1. THE HEADLINE WITNESS -------------------------------------------

// TestD84AKnownPersonChecksInWithTheOfficerChoosingNothing: the row SHOWS
// the record; the officer picks nothing; the commit snapshots the record
// onto the visit; the record is untouched and nothing is minted or amended.
func TestD84AKnownPersonChecksInWithTheOfficerChoosingNothing(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	before := w2Watermark(t, st, "site-a", 1)
	p0, c0, t0 := d84Counts(t, st)

	page, vals := gateBuild(t, srv, gateInbound, "TRK-14", []string{persons[0].Name, persons[1].Name}, 0)
	form := gateFormMarkup(t, page, formIDFor(gateInbound))

	// SHOWN: the record's company and trade, as text, on each row.
	if n := strings.Count(form, "Kakivik Asset Management · Inspection"); n != 2 {
		t.Fatalf("the two known rows show the record's company · trade %d time(s), want 2:\n%s", n, form)
	}
	// The change block is SHUT on both rows — nothing is asked.
	if n := len(d84OpenDetailsRe.FindAllString(form, -1)); n != 0 {
		t.Fatalf("%d known row(s) rendered with the change block OPEN — a known person is shown, not asked", n)
	}
	if n := len(d84ClosedDetailsRe.FindAllString(form, -1)); n != 2 {
		t.Fatalf("%d shut change blocks, want 2 (one per known row)", n)
	}
	// The pickers start AT the record, so an untouched row is no change.
	for i := 0; i < 2; i++ {
		if got, want := vals["occupant_company_id"][i], d84Deref(persons[i].CompanyID); got != want {
			t.Fatalf("row %d company picker posts %q, want the record's %q", i, got, want)
		}
		if got, want := vals["occupant_trade_id"][i], d84Deref(persons[i].TradeID); got != want {
			t.Fatalf("row %d trade picker posts %q, want the record's %q", i, got, want)
		}
	}
	// And the trade control IS A LIST, offering the escape (W8-23(c)).
	if !strings.Contains(form, `<select class="p-trade-id" name="occupant_trade_id"`) || !strings.Contains(form, `value="__new__"`) {
		t.Fatalf("the served row offers no trade list with an add-new escape:\n%s", form)
	}

	rec := postForm(t, srv, "/gate/checkin", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("commit with nothing chosen = %d, want 303: %s", rec.Code, vehicleError(t, rec.Body.String(), gateInbound))
	}
	visits := d84Visits(t, st)
	if len(visits) != 2 {
		t.Fatalf("%d open visits, want 2", len(visits))
	}
	for _, v := range visits {
		if d84Deref(v.CompanySnapshotName) != "Kakivik Asset Management" || d84Deref(v.TradeSnapshotName) != "Inspection" {
			t.Fatalf("visit snapshot = %q / %q, want the record's Kakivik Asset Management / Inspection",
				d84Deref(v.CompanySnapshotName), d84Deref(v.TradeSnapshotName))
		}
	}
	// The record is UNCHANGED and nothing was minted or amended.
	for i := 0; i < 2; i++ {
		after := d84Person(t, st, persons[i].PersonID)
		if d84Deref(after.CompanyID) != d84Deref(persons[i].CompanyID) || d84Deref(after.TradeID) != d84Deref(persons[i].TradeID) {
			t.Fatalf("%s's record changed on a check-in where nothing was chosen: %+v", persons[i].Name, after)
		}
	}
	minted := w2EventsFrom(t, st, "site-a", 1, before)
	for _, typ := range []string{"person_amended", "person_created", "company_created", "trade_created"} {
		if n := len(w2EventsOfType(t, minted, typ)); n != 0 {
			t.Fatalf("%d %s event(s) authored by a check-in where nothing was chosen, want 0", n, typ)
		}
	}
	if p1, c1, t1 := d84Counts(t, st); p1 != p0 || c1 != c0 || t1 != t0 {
		t.Fatalf("persons/companies/trades %d/%d/%d -> %d/%d/%d; nothing may be minted", p0, c0, t0, p1, c1, t1)
	}
}

// --- 2. THE CONFIRMATION GATES; A CONFIRMED CHANGE AMENDS THE RECORD ------

// TestD84AChangeWithoutTheConfirmationIsRefusedAndAmendsNothing: the picker
// is moved, the tick is not set. The commit is refused with the list — and
// the officer's change — intact; the record is untouched; nothing is
// authored. Then the same form, ticked, goes through: the record is amended
// through person_amended, THIS visit snapshots the amended record, and the
// next check-in shows the new trade.
func TestD84AChangeWithoutTheConfirmationIsRefusedAndAmendsNothing(t *testing.T) {
	srv, st, persons, pipe, _ := d84Server(t)
	dana := persons[0]
	before := w2Watermark(t, st, "site-a", 1)

	_, vals := gateBuild(t, srv, gateInbound, "TRK-14", []string{dana.Name}, 0)
	if vals.Get("occupant_trade_id") != *dana.TradeID {
		t.Fatalf("precondition: the trade picker does not start at the record")
	}
	d84Set(t, vals, "occupant_trade_id", 0, pipe.TradeID) // moved — NOT ticked
	if len(vals["amend_confirm"]) != 0 {
		t.Fatalf("precondition: an untouched form posts amend_confirm=%v; the tick must start clear", vals["amend_confirm"])
	}

	rec := postForm(t, srv, "/gate/checkin", vals)
	if rec.Code != http.StatusConflict {
		t.Fatalf("an unconfirmed change committed with %d, want 409 — the confirmation is the safeguard, not decoration", rec.Code)
	}
	msg := vehicleError(t, rec.Body.String(), gateInbound)
	// ⟦W3 F-R2-L02-1, 2026-10-03⟧ ⟦was: "not ticked"⟧ — the refusal no longer
	// coaches the tick: it renders on a re-render, whose rows serve the
	// change disclosure hidden (HideOrgDetail), and the individual page has
	// none at all. It names the row, what the record says, and the two moves
	// that work. The tick itself still applies (below).
	if !strings.Contains(msg, "not what their record says") || !strings.Contains(msg, "Edit person") || !strings.Contains(msg, dana.Name) || strings.Contains(msg, "Tick it") {
		t.Fatalf("the refusal does not tell the officer which row and what to do: %q", msg)
	}
	// Nothing authored, record untouched, no trip.
	if wm := w2Watermark(t, st, "site-a", 1); wm != before {
		t.Fatalf("the refused commit moved the watermark %d -> %d; a refusal authors nothing", before, wm)
	}
	if got := d84Deref(d84Person(t, st, dana.PersonID).TradeID); got != *dana.TradeID {
		t.Fatalf("the record changed WITHOUT the confirmation: trade %q, want %q", got, *dana.TradeID)
	}
	if v := d84Visits(t, st); len(v) != 0 {
		t.Fatalf("%d visit(s) opened by a refused commit", len(v))
	}
	// The list — and the officer's change — survived the refusal, with the
	// change block OPEN so it is on screen.
	restored := gateFormValues(t, rec.Body.String(), formIDFor(gateInbound))
	if len(restored["occupant_typed"]) != 1 || restored.Get("occupant_trade_id") != pipe.TradeID {
		t.Fatalf("the refusal cost the officer their typing: rows=%v trade picker=%q", restored["occupant_typed"], restored.Get("occupant_trade_id"))
	}
	if !d84OpenDetailsRe.MatchString(gateFormMarkup(t, rec.Body.String(), formIDFor(gateInbound))) {
		t.Fatalf("the refused row's change block is shut — the change the officer is being asked about is off screen")
	}

	// --- now TICK, and resubmit the very form the refusal rendered.
	restored.Add("amend_confirm", "0")
	rec = postForm(t, srv, "/gate/checkin", restored)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the confirmed change = %d, want 303: %s", rec.Code, vehicleError(t, rec.Body.String(), gateInbound))
	}
	minted := w2EventsFrom(t, st, "site-a", 1, before)
	amended := w2EventsOfType(t, minted, "person_amended")
	if len(amended) != 1 {
		t.Fatalf("%d person_amended event(s), want exactly 1 — the change is an AMENDMENT (replicated), not a local edit", len(amended))
	}
	if !strings.Contains(string(amended[0].Payload), `"person_id":"`+dana.PersonID+`"`) || !strings.Contains(string(amended[0].Payload), `"trade_id":"`+pipe.TradeID+`"`) {
		t.Fatalf("the amendment does not name the person and the new trade: %s", amended[0].Payload)
	}
	if n := len(w2EventsOfType(t, minted, "trade_created")); n != 0 {
		t.Fatalf("%d trade_created — picking a LISTED trade mints nothing", n)
	}
	// The RECORD carries it, from now on.
	if got := d84Deref(d84Person(t, st, dana.PersonID).TradeID); got != pipe.TradeID {
		t.Fatalf("the record's trade is %q after a confirmed change to %q", got, pipe.TradeID)
	}
	// THIS visit followed the record — snapshot taken AFTER the amendment.
	visits := d84Visits(t, st)
	if len(visits) != 1 || d84Deref(visits[0].TradeSnapshotName) != "Pipefitter" {
		t.Fatalf("the visit did not follow the record: %d visit(s), trade snapshot %q, want Pipefitter", len(visits), d84Deref(visits[0].TradeSnapshotName))
	}
	// And the NEXT check-in shows the new trade — from the record.
	page, next := gateBuild(t, srv, gateInbound, "TRK-15", []string{dana.Name}, 0)
	if !strings.Contains(gateFormMarkup(t, page, formIDFor(gateInbound)), "Kakivik Asset Management · Pipefitter") {
		t.Fatalf("the next check-in does not show the amended trade")
	}
	if next.Get("occupant_trade_id") != pipe.TradeID {
		t.Fatalf("the next check-in's picker starts at %q, want the amended %q", next.Get("occupant_trade_id"), pipe.TradeID)
	}
}

// TestD84ATickWithNoChangeAuthorsNothing: confirmed-but-already-so is silent,
// never a no-op refusal (a refused vehicle re-renders with the tick still
// set and the record already updated, and the retry must go through).
func TestD84ATickWithNoChangeAuthorsNothing(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	before := w2Watermark(t, st, "site-a", 1)
	_, vals := gateBuild(t, srv, gateInbound, "TRK-14", []string{persons[0].Name}, 0)
	vals.Add("amend_confirm", "0") // ticked, pickers untouched
	rec := postForm(t, srv, "/gate/checkin", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("a tick with no change = %d, want 303: %s", rec.Code, vehicleError(t, rec.Body.String(), gateInbound))
	}
	if n := len(w2EventsOfType(t, w2EventsFrom(t, st, "site-a", 1, before), "person_amended")); n != 0 {
		t.Fatalf("%d person_amended for a tick that changed nothing", n)
	}
}

// TestD84TheEscapeOnAKnownRowResolvesBeforeItMints: "not listed" plus the
// name of a LISTED trade (in another case) resolves to it — no second
// heading — and "not listed" plus a blank name is untouched, not refused.
func TestD84TheEscapeOnAKnownRowResolvesBeforeItMints(t *testing.T) {
	srv, st, persons, pipe, _ := d84Server(t)
	before := w2Watermark(t, st, "site-a", 1)
	_, _, t0 := d84Counts(t, st)

	_, vals := gateBuild(t, srv, gateInbound, "TRK-14", []string{persons[0].Name}, 0)
	d84Set(t, vals, "occupant_trade_id", 0, newRef)
	d84Set(t, vals, "occupant_trade", 0, "  pipefitter ") // folded spelling of a listed trade
	vals.Add("amend_confirm", "0")
	rec := postForm(t, srv, "/gate/checkin", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("escape with a folded listed name = %d, want 303 (the gate never refuses): %s", rec.Code, vehicleError(t, rec.Body.String(), gateInbound))
	}
	if _, _, t1 := d84Counts(t, st); t1 != t0 {
		t.Fatalf("trades %d -> %d: the escape minted a second spelling of a listed trade", t0, t1)
	}
	if got := d84Deref(d84Person(t, st, persons[0].PersonID).TradeID); got != pipe.TradeID {
		t.Fatalf("record trade %q, want the LISTED Pipefitter %q", got, pipe.TradeID)
	}
	if n := len(w2EventsOfType(t, w2EventsFrom(t, st, "site-a", 1, before), "person_amended")); n != 1 {
		t.Fatalf("%d person_amended, want 1", n)
	}

	// Blank escape on a known row: untouched, never refused.
	srv2, st2, persons2, _, _ := d84Server(t)
	before2 := w2Watermark(t, st2, "site-a", 1)
	_, vals2 := gateBuild(t, srv2, gateInbound, "TRK-14", []string{persons2[0].Name}, 0)
	d84Set(t, vals2, "occupant_trade_id", 0, newRef)
	d84Set(t, vals2, "occupant_trade", 0, "")
	rec = postForm(t, srv2, "/gate/checkin", vals2)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("escape with a blank name = %d, want 303: %s", rec.Code, vehicleError(t, rec.Body.String(), gateInbound))
	}
	if n := len(w2EventsOfType(t, w2EventsFrom(t, st2, "site-a", 1, before2), "person_amended")); n != 0 {
		t.Fatalf("%d person_amended for a blank escape — it is treated as untouched", n)
	}
	if got := d84Deref(d84Person(t, st2, persons2[0].PersonID).TradeID); got != *persons2[0].TradeID {
		t.Fatalf("a blank escape changed the record to %q", got)
	}
}

// --- 3. D-66 INTACT: A NEW PERSON IS STILL ASKED, THE ESCAPE NEVER REFUSES -

// TestD84ANewPersonIsStillAskedAndTheEscapeNeverRefuses walks every way the
// gate's add-new escape can be used for a person who is not on the roster,
// and none of them refuses. [[D-76]]'s live break — folding one layer and
// not the other made the gate refuse — is exactly what (a) and (e) guard.
func TestD84ANewPersonIsStillAskedAndTheEscapeNeverRefuses(t *testing.T) {
	// The served row for an unknown name is the ASK state.
	srv, _, _ := newGateTestServer(t)
	page, vals := gateBuild(t, srv, gateInbound, "TRK-14", []string{"Walkin, Wanda"}, 0)
	form := gateFormMarkup(t, page, formIDFor(gateInbound))
	if !strings.Contains(form, notOnRoster) {
		t.Fatalf("a new person's row does not say it will add them:\n%s", form)
	}
	if !d84OpenDetailsRe.MatchString(form) {
		t.Fatalf("a new person's row has the ask block SHUT — [[D-66]]: the gate asks")
	}
	if !strings.Contains(form, `<label class="amend-confirm" hidden>`) {
		t.Fatalf("a new person's row offers a 'change their record' tick — there is no record")
	}
	if vals.Get("occupant_trade_id") != "" || vals.Get("occupant_company_id") != "" {
		t.Fatalf("a new person's pickers start at %q/%q, want none/none — nothing is inherited", vals.Get("occupant_company_id"), vals.Get("occupant_trade_id"))
	}

	inspectionID := func(st *store.Store) string {
		ts, err := domain.Trades(st)
		if err != nil {
			t.Fatal(err)
		}
		for _, tr := range ts {
			if tr.Name == "Inspection" {
				return tr.TradeID
			}
		}
		t.Fatal("fixture: no Inspection trade")
		return ""
	}
	kakivikID := func(st *store.Store) string {
		cs, err := domain.Companies(st)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cs {
			if c.Name == "Kakivik Asset Management" {
				return c.CompanyID
			}
		}
		t.Fatal("fixture: no Kakivik company")
		return ""
	}
	newPerson := func(st *store.Store) domain.Person {
		ps, err := domain.Persons(st)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range ps {
			if p.Name == "Walkin, Wanda" {
				return p
			}
		}
		t.Fatal("the new person was not created — the gate REFUSED")
		return domain.Person{}
	}

	cases := []struct {
		name                  string
		tradePick, tradeText  string
		compPick, compText    string
		wantTrades, wantComps int // delta
		wantTrade             func(st *store.Store) string
		wantCompany           func(st *store.Store) string
		wantTradeCreated      int
	}{
		// ⟦R-26, 2026-09-13⟧ (a)-(d) are about TRADE, not company — but
		// R-26 now refuses a new person with no company at all, so each
		// carries one too (the escape, folded to a LISTED company: mints
		// nothing, leaves wantComps at its zero default), to isolate what
		// each case actually tests from the new precondition.
		{name: "(a) escape + folded LISTED trade name resolves, mints nothing",
			tradePick: newRef, tradeText: "inspection", wantTrades: 0, wantTrade: inspectionID,
			compPick: newRef, compText: "kakivik asset management"},
		{name: "(b) escape + genuinely new trade name mints exactly one",
			tradePick: newRef, tradeText: "Pipefitter", wantTrades: 1, wantTradeCreated: 1,
			compPick: newRef, compText: "kakivik asset management",
			wantTrade: func(st *store.Store) string {
				ts, _ := domain.Trades(st)
				for _, tr := range ts {
					if tr.Name == "Pipefitter" {
						return tr.TradeID
					}
				}
				return "(unminted)"
			}},
		{name: "(c) escape + blank name: no trade, no refusal",
			tradePick: newRef, tradeText: "", wantTrades: 0, wantTrade: func(*store.Store) string { return "" },
			compPick: newRef, compText: "kakivik asset management"},
		{name: "(d) a LISTED trade picked from the list",
			tradePick: "PICK-INSPECTION", wantTrades: 0, wantTrade: inspectionID,
			compPick: newRef, compText: "kakivik asset management"},
		{name: "(e) company escape + folded LISTED company name resolves, mints nothing",
			compPick: newRef, compText: "kakivik asset management", wantComps: 0, wantCompany: kakivikID,
			wantTrade: func(*store.Store) string { return "" }},
		{name: "(f) company escape + genuinely new company mints exactly one",
			compPick: newRef, compText: "Aurora Logistics", wantComps: 1,
			wantCompany: func(st *store.Store) string {
				cs, _ := domain.Companies(st)
				for _, c := range cs {
					if c.Name == "Aurora Logistics" {
						return c.CompanyID
					}
				}
				return "(unminted)"
			}, wantTrade: func(*store.Store) string { return "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, st, _ := newGateTestServer(t)
			before := w2Watermark(t, st, "site-a", 1)
			p0, c0, t0 := d84Counts(t, st)
			_, vals := gateBuild(t, srv, gateInbound, "TRK-14", []string{"Walkin, Wanda"}, 0)
			pick := tc.tradePick
			if pick == "PICK-INSPECTION" {
				pick = inspectionID(st)
			}
			d84Set(t, vals, "occupant_trade_id", 0, pick)
			d84Set(t, vals, "occupant_trade", 0, tc.tradeText)
			d84Set(t, vals, "occupant_company_id", 0, tc.compPick)
			d84Set(t, vals, "occupant_company", 0, tc.compText)

			rec := postForm(t, srv, "/gate/checkin", vals)
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("[[D-66]] BROKEN: the gate REFUSED a new person (%d): %s", rec.Code, vehicleError(t, rec.Body.String(), gateInbound))
			}
			p := newPerson(st)
			p1, c1, t1 := d84Counts(t, st)
			if p1 != p0+1 {
				t.Fatalf("persons %d -> %d, want +1", p0, p1)
			}
			if t1 != t0+tc.wantTrades {
				t.Fatalf("trades %d -> %d, want delta %d", t0, t1, tc.wantTrades)
			}
			if c1 != c0+tc.wantComps {
				t.Fatalf("companies %d -> %d, want delta %d", c0, c1, tc.wantComps)
			}
			if got := d84Deref(p.TradeID); got != tc.wantTrade(st) {
				t.Fatalf("new person's trade = %q, want %q", got, tc.wantTrade(st))
			}
			if tc.wantCompany != nil {
				if got := d84Deref(p.CompanyID); got != tc.wantCompany(st) {
					t.Fatalf("new person's company = %q, want %q", got, tc.wantCompany(st))
				}
			}
			minted := w2EventsFrom(t, st, "site-a", 1, before)
			if n := len(w2EventsOfType(t, minted, "trade_created")); n != tc.wantTradeCreated {
				t.Fatalf("%d trade_created, want %d", n, tc.wantTradeCreated)
			}
			if n := len(w2EventsOfType(t, minted, "person_amended")); n != 0 {
				t.Fatalf("%d person_amended on a NEW person — new people are asked, never amended", n)
			}
			// The visit snapshots what the record now holds.
			v := d84Visits(t, st)
			if len(v) != 1 || d84Deref(v[0].TradeSnapshotID) != d84Deref(p.TradeID) {
				t.Fatalf("visit trade snapshot %q != record %q", d84Deref(v[0].TradeSnapshotID), d84Deref(p.TradeID))
			}
		})
	}
}

// --- 4. THE BUS OF TWELVE, MEASURED -------------------------------------

// TestD84TheBusOfTwelveAsksForNothingItAlreadyKnows re-measures d83's §3
// with d83's instrument. BEFORE (this tree at 74c3736, re-measured before
// any change): text=40 of which 12 name + 12 company + 12 trade, select=0 —
// 24 boxes that recorded nothing. AFTER, asserted here.
func TestD84TheBusOfTwelveAsksForNothingItAlreadyKnows(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	co, tr := persons[0].CompanyID, persons[0].TradeID
	names := []string{persons[0].Name, persons[1].Name, persons[2].Name}
	for _, n := range []string{"Rider Four", "Rider Five", "Rider Six", "Rider Seven", "Rider Eight",
		"Rider Nine", "Rider Ten", "Rider Eleven", "Rider Twelve"} {
		if _, err := domain.CreatePerson(st, "site-a", 1, n, co, tr, 0, "default"); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	p0, c0, t0 := d84Counts(t, st)
	before := w2Watermark(t, st, "site-a", 1)
	page, vals := gateBuild(t, srv, gateInbound, "BUS 12", names, 0)
	if got := len(vals["occupant_typed"]); got != 12 {
		t.Fatalf("precondition: %d rows accumulated, want 12", got)
	}
	form := gateFormMarkup(t, page, formIDFor(gateInbound))
	operable := d84Operable(form)

	wholeText := len(d84TextInputRe.FindAllString(form, -1))
	opText := len(d84TextInputRe.FindAllString(operable, -1))
	opSelect := len(d84SelectRe.FindAllString(operable, -1))
	opCheck := len(d84CheckboxRe.FindAllString(operable, -1))
	byName := map[string]int{}
	for _, tag := range d84TextInputRe.FindAllString(operable, -1) {
		byName[tagAttrs(tag)["name"]]++
	}
	t.Logf("MEASURED bus of 12 known passengers, arriving form, JS-off — whole form text inputs=%d; OPERABLE (outside a shut change block): text=%d select=%d checkbox=%d; operable text by name=%v",
		wholeText, opText, opSelect, opCheck, byName)

	// Per passenger, the officer faces the NAME and nothing else — Position/
	// Phone ⟦gate-checkin-spec, 2026-09-13⟧ joined Company/Trade in the SAME
	// shut change block, so a known, untouched row asks nothing about them
	// either.
	if byName["occupant_typed"] != 12 || byName["occupant_company"] != 0 || byName["occupant_trade"] != 0 ||
		byName["occupant_position"] != 0 || byName["occupant_phone"] != 0 {
		t.Fatalf("operable per-passenger text inputs: %v — want 12 names and NO company/trade/position/phone boxes", byName)
	}
	// ⟦gate-checkin-spec, 2026-09-13⟧ 16 -> 19: new_typed split into
	// new_last + new_first (net +1), and new_position/new_phone joined the
	// open box's own company/trade escape (+2) — all three are the NEXT
	// person's fields, not a re-ask about any of the 12 already on the bus.
	if opText != 19 { // 12 names + vehicle + the open box's new_last/new_first/new_company/new_trade/new_position/new_phone
		t.Fatalf("operable text inputs = %d, want 19 (d83 measured 40, of which 24 recorded nothing; gate-checkin-spec added new_first/new_position/new_phone to the open box, 2026-09-13)", opText)
	}
	// ⟦SchemaVersion 13, R-17, 2026-09-11⟧ checkbox want 0 -> 1: the
	// "Vehicle unknown" checkbox now renders on the inbound direction too
	// (it used to be outbound-only), and it is a FORM-LEVEL control
	// alongside the vehicle text field — not a per-row picker or tick, so
	// it is correctly outside any shut change block, exactly like the
	// vehicle text input it sits beside. This test's own subject (no ROW
	// picker or tick is operable on a bus of known passengers) is
	// unaffected: the count moved because a genuinely new top-level
	// control exists, not because a row-level one leaked out.
	// ⟦R-28, team-lead, 2026-09-16⟧ select want 2 -> 3: "a single Driver
	// dropdown" (R-28's own spec for the arriving form) is a genuinely new
	// top-level control — the SAME shape as the "checkbox 0 -> 1" change
	// just above when Vehicle unknown gained the inbound direction: it
	// sits once, after the whole list, never per-row (DriverSelect,
	// gate_vehicle_form's own comment on why it replaces per-row radios
	// rather than adding a THIRD way to flag a driver). This test's own
	// subject — no ROW picker or tick is operable on a bus of known
	// passengers — is unaffected: the count moved because a form-level
	// control was added, not because a row-level one leaked out of its
	// shut block.
	if opSelect != 3 || opCheck != 1 { // the open box's two pickers, the compact driver select, the vehicle-unknown checkbox; no row picker or tick is operable
		t.Fatalf("operable select=%d checkbox=%d, want 3/1 — every row's pickers and tick sit behind a shut block", opSelect, opCheck)
	}
	if n := len(d84OpenDetailsRe.FindAllString(form, -1)); n != 0 {
		t.Fatalf("%d of 12 known rows rendered with the change block open", n)
	}

	// And the whole bus commits with nothing chosen: 12 correct snapshots,
	// nothing minted, nothing amended.
	rec := postForm(t, srv, "/gate/checkin", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("bus commit = %d: %s", rec.Code, vehicleError(t, rec.Body.String(), gateInbound))
	}
	visits := d84Visits(t, st)
	if len(visits) != 12 {
		t.Fatalf("%d visits, want 12", len(visits))
	}
	for _, v := range visits {
		if d84Deref(v.CompanySnapshotName) != "Kakivik Asset Management" || d84Deref(v.TradeSnapshotName) != "Inspection" {
			t.Fatalf("a visit snapshot is %q / %q", d84Deref(v.CompanySnapshotName), d84Deref(v.TradeSnapshotName))
		}
	}
	if p1, c1, t1 := d84Counts(t, st); p1 != p0 || c1 != c0 || t1 != t0 {
		t.Fatalf("the bus minted something: %d/%d/%d -> %d/%d/%d", p0, c0, t0, p1, c1, t1)
	}
	if n := len(w2EventsOfType(t, w2EventsFrom(t, st, "site-a", 1, before), "person_amended")); n != 0 {
		t.Fatalf("%d person_amended on a bus where nothing was chosen", n)
	}
}

// --- 5. UNDO CARRIES THE PICK AND THE TICK -------------------------------

// TestD84UndoRestoresAChangeInProgress: a row with a moved picker and a set
// tick is deleted and undone; both come back. Site (4) of five.
func TestD84UndoRestoresAChangeInProgress(t *testing.T) {
	srv, _, persons, pipe, _ := d84Server(t)
	_, vals := gateBuild(t, srv, gateInbound, "TRK-14", []string{persons[0].Name, persons[1].Name}, 0)
	d84Set(t, vals, "occupant_trade_id", 1, pipe.TradeID)
	vals.Add("amend_confirm", "1")
	vals.Set("delete_index", "1")
	rec := postForm(t, srv, "/gate/passenger/delete", vals)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d", rec.Code)
	}
	undo := gateFormValues(t, rec.Body.String(), formIDFor(gateInbound))
	if undo.Get("removed_trade_id") != pipe.TradeID || undo.Get("removed_amend") != "1" {
		t.Fatalf("the undo carrier dropped the change: removed_trade_id=%q removed_amend=%q", undo.Get("removed_trade_id"), undo.Get("removed_amend"))
	}
	rec = postForm(t, srv, "/gate/passenger/undo", undo)
	if rec.Code != http.StatusOK {
		t.Fatalf("undo = %d", rec.Code)
	}
	restored := gateFormValues(t, rec.Body.String(), formIDFor(gateInbound))
	if len(restored["occupant_trade_id"]) != 2 || restored["occupant_trade_id"][1] != pipe.TradeID {
		t.Fatalf("after undo the trade pickers are %v, want row 2 back at Pipefitter", restored["occupant_trade_id"])
	}
	if got := restored["amend_confirm"]; len(got) != 1 || got[0] != "1" {
		t.Fatalf("after undo the tick is %v, want [1]", got)
	}
}

// --- 6. THE LEAVING FORM HONOURS THE SAME TICK ---------------------------

// TestD84TheLeavingFormHonoursTheSameConfirmation: the row is the same row;
// a confirmed change while checking a crew OUT amends the record too, and an
// unconfirmed difference is refused there too. The departing visit keeps its
// ARRIVAL snapshot.
func TestD84TheLeavingFormHonoursTheSameConfirmation(t *testing.T) {
	srv, st, persons, pipe, _ := d84Server(t)
	dana := persons[0]
	_, in := gateBuild(t, srv, gateInbound, "TRK-14", []string{dana.Name}, 0)
	if rec := postForm(t, srv, "/gate/checkin", in); rec.Code != http.StatusSeeOther {
		t.Fatalf("check-in = %d", rec.Code)
	}
	before := w2Watermark(t, st, "site-a", 1)

	// ⟦R-28, team-lead, 2026-09-16⟧ RETARGETED: the departing form is
	// reached via the roster's own tick-and-select door now — dana is the
	// only occupant either way (she was just checked in above, so ticking
	// her resolves to the SAME record a typed exact match would have),
	// and d84Set below only touches the already-scraped form value, which
	// does not care how row 0 was populated.
	_, out := gateBuildOutbound(t, srv, "TRK-14", []string{visitIDForCrewName(t, st, dana.Name)}, nil, 0)
	out.Set("destination_id", "conneco")
	d84Set(t, out, "occupant_trade_id", 0, pipe.TradeID)
	rec := postForm(t, srv, "/gate/checkout/vehicle", out)
	if rec.Code != http.StatusConflict {
		t.Fatalf("an unconfirmed change on the LEAVING form = %d, want 409", rec.Code)
	}
	if got := d84Deref(d84Person(t, st, dana.PersonID).TradeID); got != *dana.TradeID {
		t.Fatalf("the leaving form changed the record without the tick")
	}
	out.Add("amend_confirm", "0")
	rec = postForm(t, srv, "/gate/checkout/vehicle", out)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("confirmed change on the leaving form = %d, want 303: %s", rec.Code, vehicleError(t, rec.Body.String(), gateOutbound))
	}
	if n := len(w2EventsOfType(t, w2EventsFrom(t, st, "site-a", 1, before), "person_amended")); n != 1 {
		t.Fatalf("%d person_amended from the leaving form, want 1", n)
	}
	if got := d84Deref(d84Person(t, st, dana.PersonID).TradeID); got != pipe.TradeID {
		t.Fatalf("record trade %q after a confirmed change at departure", got)
	}
	// The closed visit keeps the snapshot taken when they ARRIVED.
	all, err := domain.Visits(st) // every visit, open or closed — the fixture has one site
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || d84Deref(all[0].TradeSnapshotName) != "Inspection" {
		t.Fatalf("the departing visit's arrival snapshot was rewritten: %d visit(s), trade %q", len(all), d84Deref(all[0].TradeSnapshotName))
	}
}

// --- 7. THE CROSS-SITE CASE — MEASURED, NOT RULED -------------------------

// TestD84ACrossSiteRecordChangeIsMeasuredNotRuled: a person enrolled at the
// OTHER post, checked in here with a confirmed trade change. [[D-72]] /
// [[D-77]]'s amend-permission rule has NO implementation today (measured by
// the D-77 pass: every peer may amend every peer), and D-84's brief says:
// do not build permissions, do not block it, report which case you land in.
// This test RECORDS today's behaviour so the question reaches James with a
// number attached; it decides nothing. If James rules the other way, this
// test flips — deliberately, under that ruling.
func TestD84ACrossSiteRecordChangeIsMeasuredNotRuled(t *testing.T) {
	srv, st, persons, pipe, _ := d84Server(t)
	fred, err := domain.CreatePerson(st, "site-b", 1, "Faraway, Fred", persons[0].CompanyID, persons[0].TradeID, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if fred.SiteID != "site-b" {
		t.Fatalf("fixture: Fred is enrolled at %q, want site-b", fred.SiteID)
	}
	before := w2Watermark(t, st, "site-a", 1)
	_, vals := gateBuild(t, srv, gateInbound, "TRK-14", []string{fred.Name}, 0)
	d84Set(t, vals, "occupant_trade_id", 0, pipe.TradeID)
	vals.Add("amend_confirm", "0")
	rec := postForm(t, srv, "/gate/checkin", vals)
	amended := w2EventsOfType(t, w2EventsFrom(t, st, "site-a", 1, before), "person_amended")
	t.Logf("MEASURED (not ruled): a site-b-enrolled person amended from site-a's gate — commit=%d, site-a-authored person_amended=%d. "+
		"D-72/D-77's amend-permission rule has no implementation; this is the cross-site case D-84's brief asks to be reported.",
		rec.Code, len(amended))
	if rec.Code != http.StatusSeeOther || len(amended) != 1 {
		t.Fatalf("today's measured behaviour changed: commit=%d amendments=%d (was 303/1). If a ruling changed it, re-point this test under that ruling",
			rec.Code, len(amended))
	}
	if amended[0].SiteID != "site-a" {
		t.Fatalf("the amendment was authored under %q; a site authors only its own stream", amended[0].SiteID)
	}
}
