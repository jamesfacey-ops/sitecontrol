// witness_d76_trade_fold_test.go — [[D-76]] (2026-09-05) at the three web
// doors that mint a trade: the /trade admin screen, the gate check-in's
// add-it escape, and intake's add-it escape.
//
// The domain half (createTrade's fold, the typed *TradeCreateRefusal, and
// resolveOrCreateTradeByName's fold) is witnessed in
// internal/domain/witness_d76_trade_fold_test.go. This file witnesses what an
// OFFICER sees at each door:
//
//	/trade   — "Welder" then "welder" is REFUSED at 409 with the authored
//	           sentence naming the on-list spelling, and no second trade is
//	           authored. Before D-76 this door minted the second spelling
//	           (y6-tradeweb §3) AND, for an exact retype, answered a raw 500
//	           because its friendly-refusal branch string-matched text the
//	           domain never emitted (u1c-tradeweb §4). The branch now matches
//	           the error BY TYPE.
//	/gate    — "welder" RESOLVES to Welder silently: 303, no refusal, no
//	           mint, the person tagged to the existing row. [[D-66]].
//	/intake  — its own folding pre-check is KEPT and still refuses at 409
//	           BEFORE any company is authored — the reason it is kept (see
//	           TestD76IntakeEscapeStillRefusesACaseVariantBeforeAnyCompanyIsAuthored).
package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// d76Notice returns the inner text of the FIRST element carrying `class`
// on the page (the notice, not the whole body — an assertion scoped to the
// body would be satisfied by the sentence appearing anywhere, e.g. in a
// help paragraph). Fails the test if no such element rendered.
func d76Notice(t *testing.T, body, class string) string {
	t.Helper()
	open := `<div class="` + class + `"`
	i := strings.Index(body, open)
	if i < 0 {
		t.Fatalf("no <div class=%q> notice rendered; body:\n%s", class, body)
	}
	rest := body[i:]
	j := strings.Index(rest, "</div>")
	if j < 0 {
		t.Fatalf("notice <div class=%q> is never closed; body:\n%s", class, body)
	}
	return rest[:j]
}

// d76TradeIDs re-reads the store and returns every trade id — the identity
// check every "nothing was minted" assertion below rests on.
func d76TradeIDs(t *testing.T, st *store.Store) []string {
	t.Helper()
	trades, err := domain.Trades(st)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(trades))
	for _, tr := range trades {
		ids = append(ids, tr.TradeID)
	}
	return ids
}

// d76SeedTradePair is the web-side twin of the domain file's helper: it lists
// TWO trades, distinct under the fold, and returns both ids and the list
// length. The gate test then checks in two walk-ins, one retype of each, and
// asserts each landed on their OWN row — a resolve-by-POSITION mutant sends
// both to the same trade, so one lands wrong on every run, with no dependence
// on Trades()'s random trade_id order. (Until F0 this helper seeded decoys
// until `name` was not Trades()[0] and `Fatal`ed after 100; that hatch fired
// 6/600 runs — reports/witness/F0-FLAKE-FIX.md.)
func d76SeedTradePair(t *testing.T, st *store.Store, nameA, nameB string) (string, string, int) {
	t.Helper()
	a, err := domain.CreateTrade(st, "site-a", 1, nameA, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	b, err := domain.CreateTrade(st, "site-a", 1, nameB, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	trades, err := domain.Trades(st)
	if err != nil {
		t.Fatal(err)
	}
	return a.TradeID, b.TradeID, len(trades)
}

// d76AssertLandedOn finds the person named `person` and asserts their trade
// id is exactly `wantID` (the row named `wantName`). Fatals if the person
// was never created.
func d76AssertLandedOn(t *testing.T, st *store.Store, person, wantID, wantName string) {
	t.Helper()
	persons, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	for i := range persons {
		if persons[i].Name != person {
			continue
		}
		if persons[i].TradeID == nil || *persons[i].TradeID != wantID {
			got := "<nil>"
			if persons[i].TradeID != nil {
				got = fmt.Sprintf("%q", *persons[i].TradeID)
			}
			t.Errorf("%s's trade id = %s, want the EXISTING %s row %q (resolved by folded NAME — a position-based resolve puts both walk-ins on one row)", person, got, wantName, wantID)
		}
		return
	}
	t.Fatalf("%s was not created", person)
}

// d76TradeRefusalSentence is the sentence trade.go authors for this screen,
// as html/template renders it (w5aEscape: `"` → &#34;, `'` → &#39;).
func d76TradeRefusalSentence(onList string) string {
	return w5aEscape(fmt.Sprintf("%q is already on this site's trade list — pick it from the list above instead of adding a second one. Nothing was recorded.", onList))
}

// --- door: /trade/create -----------------------------------------------------

// TestD76TradeScreenRefusesACaseVariantWithTheOfficerSentenceAndMintsNothing
// is the y6-tradeweb §3 defect closed: create "Welder" through the real
// route, then POST "welder". Want 409, the authored sentence naming the
// ON-LIST spelling "Welder" (the row to pick, not the officer's typo), and —
// re-reading the store — exactly the one original trade row.
//
// CLEAN-TREE FAILURE, produced: `POST /trade/create name="welder" after
// "Welder" = 303, want 409` — a second Welder heading was minted.
// INTERMEDIATE FAILURE with the domain folded but trade.go's dead
// string-match still in place: `= 500, want 409` — the u1c-tradeweb §4
// defect, now reachable by a case variant too.
func TestD76TradeScreenRefusesACaseVariantWithTheOfficerSentenceAndMintsNothing(t *testing.T) {
	srv, st := newVehicleAmendTestServer(t)
	welderID := createTradeViaRoute(t, srv, "Welder")

	rec := postForm(t, srv, "/trade/create", url.Values{"name": {"welder"}})
	if rec.Code != http.StatusConflict {
		t.Fatalf("POST /trade/create name=\"welder\" after \"Welder\" = %d, want 409 (D-76: one trade, refused with the officer sentence): %s", rec.Code, rec.Body.String())
	}
	notice := d76Notice(t, rec.Body.String(), "notice notice-warn")
	if want := d76TradeRefusalSentence("Welder"); !strings.Contains(notice, want) {
		t.Errorf("409 notice does not carry the authored sentence naming the on-list spelling.\nwant: %s\ngot:  %s", want, notice)
	}
	if strings.Contains(rec.Body.String(), "This post could not complete that") {
		t.Error("the page rendered the INTERNAL-ERROR notice alongside the refusal — a refusal is not a fault")
	}
	if ids := d76TradeIDs(t, st); len(ids) != 1 || ids[0] != welderID {
		t.Errorf("trades after the refused retype = %v, want exactly the original Welder row %q — nothing authored", ids, welderID)
	}
}

// TestD76TradeScreenRefusesAnExactDuplicateAt409NotA500 is the u1c-tradeweb
// §4 measurement made a witness: BEFORE this change an EXACT retype of a
// listed name answered a raw 500, because trade.go:233 tested
// `strings.Contains(err.Error(), "already on this site's trade list")` and
// the domain says "...on this site's list". The branch now matches
// *domain.TradeCreateRefusal by type, so the text can never drift it dead
// again.
//
// CLEAN-TREE FAILURE, produced: `POST /trade/create name="Welder" twice =
// 500, want 409`.
func TestD76TradeScreenRefusesAnExactDuplicateAt409NotA500(t *testing.T) {
	srv, st := newVehicleAmendTestServer(t)
	welderID := createTradeViaRoute(t, srv, "Welder")

	rec := postForm(t, srv, "/trade/create", url.Values{"name": {"Welder"}})
	if rec.Code != http.StatusConflict {
		t.Fatalf("POST /trade/create name=\"Welder\" twice = %d, want 409 — the friendly-refusal branch must be LIVE, matched by type: %s", rec.Code, rec.Body.String())
	}
	notice := d76Notice(t, rec.Body.String(), "notice notice-warn")
	if want := d76TradeRefusalSentence("Welder"); !strings.Contains(notice, want) {
		t.Errorf("409 notice does not carry the authored sentence.\nwant: %s\ngot:  %s", want, notice)
	}
	if ids := d76TradeIDs(t, st); len(ids) != 1 || ids[0] != welderID {
		t.Errorf("trades after the refused exact retype = %v, want exactly %q", ids, welderID)
	}
}

// TestD76TradeScreenRefusesAPaddedRetype is the whitespace half at this
// door: "  welder  ". handleTradeCreate trims before calling the domain, so
// what this proves at the web layer is that trim+fold together still land
// on the typed refusal and name the on-list spelling.
func TestD76TradeScreenRefusesAPaddedRetype(t *testing.T) {
	srv, st := newVehicleAmendTestServer(t)
	welderID := createTradeViaRoute(t, srv, "Welder")

	rec := postForm(t, srv, "/trade/create", url.Values{"name": {"  welder  "}})
	if rec.Code != http.StatusConflict {
		t.Fatalf("POST /trade/create name=\"  welder  \" after \"Welder\" = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	notice := d76Notice(t, rec.Body.String(), "notice notice-warn")
	if want := d76TradeRefusalSentence("Welder"); !strings.Contains(notice, want) {
		t.Errorf("409 notice does not carry the authored sentence.\nwant: %s\ngot:  %s", want, notice)
	}
	if ids := d76TradeIDs(t, st); len(ids) != 1 || ids[0] != welderID {
		t.Errorf("trades after the refused padded retype = %v, want exactly %q", ids, welderID)
	}
}

// --- door: /gate/checkin's add-it escape — D-66 -----------------------------

// TestD76GateCheckInWithACaseVariantTradeResolvesSilentlyAndMintsNothing is
// [[D-66]] at the real route. "Welder" and "Electrician" are on the list; an
// officer checks in two walk-ins, driver_trade="welder" on one truck and
// driver_trade="electrician" on the next. Want, for each: 303 (no refusal,
// no picker step); and afterwards the trade list UNCHANGED by identity and
// each walk-in tagged to their OWN existing row (see d76SeedTradePair for
// why two).
//
// CLEAN-TREE FAILURE, produced: 303 as wanted, but `trades after = N+1` and
// Wanda's trade id was a fresh one — the second heading, minted silently.
// INTERMEDIATE FAILURE with only createTrade folded: `= 409, want 303` — the
// gate REFUSED, which is the D-66 break folding the resolve step closes.
func TestD76GateCheckInWithACaseVariantTradeResolvesSilentlyAndMintsNothing(t *testing.T) {
	srv, st := newAuthTestServer(t)
	cookie := loginCookie(t, srv, testLoginPassword)
	welderID, electricianID, before := d76SeedTradePair(t, st, "Welder", "Electrician")

	for _, walkIn := range []struct{ vehicle, name, typed, listed string }{
		{"TRK-14", "Walk In Wanda", "welder", "Welder"},
		{"TRK-15", "Walk In Eddie", "electrician", "Electrician"},
	} {
		rec := postFormWithCookie(t, srv, "/gate/checkin", url.Values{
			"time_source": {"default"},
			"vehicle":     {walkIn.vehicle}, "driver_kind": {"new"}, "driver_value": {walkIn.name},
			"driver_trade": {walkIn.typed},
		}, cookie)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("gate check-in with driver_trade=%q while %q is listed = %d, want 303 — D-66: the gate never refuses: %s", walkIn.typed, walkIn.listed, rec.Code, rec.Body.String())
		}
	}

	if ids := d76TradeIDs(t, st); len(ids) != before {
		t.Errorf("trades after the two check-ins = %d, want %d — \"welder\" and \"electrician\" must resolve to their listed rows and mint NOTHING", len(ids), before)
	}
	d76AssertLandedOn(t, st, "Walk In Wanda", welderID, "Welder")
	d76AssertLandedOn(t, st, "Walk In Eddie", electricianID, "Electrician")
}

// --- door: /roster/intake's add-it escape ------------------------------------

// TestD76IntakeEscapeStillRefusesACaseVariantBeforeAnyCompanyIsAuthored is
// why intake.go's OWN folding pre-check is KEPT after the domain folds, and
// it is not redundancy. handleIntake authors the new COMPANY (intake.go:641)
// BEFORE it calls CreateTradeRecordedBy (intake.go:651). If the duplicate
// trade were caught only by the domain, a refused intake would already have
// minted "Brand New Co" — half an intake, "a duplicate company nobody asked
// for, on the table the muster groups by" (TestARefusedIntakeAuthorsNothingAtAll).
// The pre-check fires before ANY write, and answers 409 with intake's own
// sentence where the domain path would answer gateFail's 500.
//
// PLANT-CYCLE FAILURE, produced (the pre-check's `case form.TradeID ==
// newRef:` arm emptied, then reverted): `= 500, want 409` AND `companies
// after = [Kakivik Asset Management Brand New Co]` — the company was authored
// by a refused intake.
func TestD76IntakeEscapeStillRefusesACaseVariantBeforeAnyCompanyIsAuthored(t *testing.T) {
	srv, st, _ := newGateTestServer(t) // seeds exactly one trade, "Inspection", and one company
	inspectionID := tradeIDNamed(t, srv, "Inspection")
	companiesBefore, err := domain.Companies(st)
	if err != nil {
		t.Fatal(err)
	}

	rec := postForm(t, srv, "/roster/intake",
		intakeForm2("Ferguson", "Mary", newRef, "Brand New Co", newRef, "inspection"))
	// Errorf, not Fatalf: the STATE assertions below must run even when the
	// status is wrong — under the plant the interesting evidence is what got
	// authored on the way to the 500, not the 500 itself.
	if rec.Code != http.StatusConflict {
		t.Errorf("intake with a NEW company and trade \"inspection\" (Inspection listed) = %d, want 409 from intake's own pre-check: %.60s", rec.Code, rec.Body.String())
	}

	companiesAfter, err := domain.Companies(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(companiesAfter) != len(companiesBefore) {
		names := make([]string, 0, len(companiesAfter))
		for _, c := range companiesAfter {
			names = append(names, c.Name)
		}
		t.Errorf("companies after a REFUSED intake = %v, want unchanged (%d) — the pre-check must fire before the company write", names, len(companiesBefore))
	}
	if ids := d76TradeIDs(t, st); len(ids) != 1 || ids[0] != inspectionID {
		t.Errorf("trades after the refused intake = %v, want exactly %q", ids, inspectionID)
	}
	if ps := personNamed(t, srv, "Mary Ferguson"); len(ps) != 0 {
		t.Errorf("a refused intake enrolled %d person(s): %+v", len(ps), ps)
	}
	if rec.Code != http.StatusConflict {
		return // a non-409 page has no refusal notice to scope; the miss is already recorded above
	}
	notice := d76Notice(t, rec.Body.String(), "notice notice-critical")
	want := w5aEscape(`"Inspection" is already on this site's trade list — choose it from the list rather than adding a second one.`)
	if !strings.Contains(notice, want) {
		t.Errorf("409 notice does not carry intake's own sentence.\nwant: %s\ngot:  %s", want, notice)
	}
}
