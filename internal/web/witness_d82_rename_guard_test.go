// witness_d82_rename_guard_test.go — [[D-82]] (2026-09-05) at the one door
// that renames a trade: POST /trade/amend on the /trade admin screen.
//
// The domain half (AmendTrade's collision arm, the typed *TradeRenameRefusal,
// the composed-list read, the positive controls and the D-66 gate proofs) is
// witnessed in internal/domain/witness_d82_rename_guard_test.go. This file
// witnesses what an OFFICER sees:
//
//	rename onto a listed name  — 409, the refusal notice naming the trade
//	                             that already holds the name (its id and
//	                             on-list spelling), and nothing authored.
//	                             Before D-82 this POST answered 303
//	                             `/trade?recorded=…` and the list then
//	                             carried two rows named Pipefitter.
//	rename to a new name       — still 303, still recorded.
//	rename to its own name     — still 303, as on the clean tree.
//	the explicit same_as merge — still 303: the operation D-82 refuses to
//	                             fold into a rename is intact as itself.
//
// ⚠ web/trade.go is UNCHANGED by D-82. The refusal reaches the 409 arm
// because *TradeRenameRefusal unwraps to the *TradeAmendRefusal that arm
// matches BY TYPE (errors.As). That is what the 409 here proves: had the new
// type fallen outside the partition, this door would have answered a raw 500
// — the exact dead-branch shape D-76 found on the create side.
package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// d82Named returns every composed trade named exactly `name`.
func d82Named(t *testing.T, st *store.Store, name string) []domain.Trade {
	t.Helper()
	trades, err := domain.Trades(st)
	if err != nil {
		t.Fatal(err)
	}
	var hits []domain.Trade
	for _, tr := range trades {
		if tr.Name == name {
			hits = append(hits, tr)
		}
	}
	return hits
}

func d82Watermark(t *testing.T, st *store.Store) int64 {
	t.Helper()
	wm, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	return wm
}

// d82RefusalNotice is the FIRST notice-warn div's inner text, the element
// renderTradeRefusal puts the domain's sentence in. Scoped to the element,
// not the body.
func d82RefusalNotice(t *testing.T, body string) string {
	t.Helper()
	return d76Notice(t, body, "notice notice-warn")
}

// TestD82TradeScreenRefusesARenameOntoAListedNameAt409NamingTheHolderAndRecordsNothing
// creates Pipefitter and Catering through the real route, then POSTs
// trade_id=<Catering> name=Pipefitter. Want: 409, no redirect, the notice
// carrying the domain's own sentence — which names the Pipefitter row's id
// and the on-list spelling — the internal-error notice ABSENT, the typed
// name still in the form, the watermark unmoved, and exactly one row named
// Pipefitter, the original.
//
// CORRECTED CITATION (2026-09-06, evidence-auditor): this wording was never
// literally producible — this file constructs domain.TradeRenameRefusal{}
// (below), which does not exist pre-D-82, so `go vet` fails with
// `undefined: domain.TradeRenameRefusal` before any test can run, let alone
// "produce" a runtime result. The underlying claim is nonetheless TRUE:
// MEASURED by teammate d82-web-409 on pre-fix ancestor `c9283b8` via a
// minimal probe (no reference to the new type) exercising the identical
// POST: `= 303`, Location `/trade?recorded=site-a.1.3`, exact match
// including the watermark string. See reports/witness/D82-WEB-409.md §2-3
// and reports/witness/CITATION-AUDIT.md row 4.
func TestD82TradeScreenRefusesARenameOntoAListedNameAt409NamingTheHolderAndRecordsNothing(t *testing.T) {
	srv, st := newVehicleAmendTestServer(t)
	pipefitterID := createTradeViaRoute(t, srv, "Pipefitter")
	cateringID := createTradeViaRoute(t, srv, "Catering")
	wmBefore := d82Watermark(t, st)

	rec := postForm(t, srv, "/trade/amend", url.Values{"trade_id": {cateringID}, "name": {"Pipefitter"}})
	if rec.Code != http.StatusConflict {
		t.Errorf("POST /trade/amend trade_id=Catering name=\"Pipefitter\" with Pipefitter listed = %d (Location %q), want 409 — D-82: refused, same rule as creating", rec.Code, rec.Header().Get("Location"))
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("a refused rename redirected to %q — a refusal re-renders the screen, it does not confirm", loc)
	}
	body := rec.Body.String()
	if rec.Code == http.StatusConflict {
		notice := d82RefusalNotice(t, body)
		if !strings.Contains(notice, pipefitterID) {
			t.Errorf("the 409 notice does not name the trade already holding the name (id %s), so the officer cannot pick it:\n%s", pipefitterID, notice)
		}
		if !strings.Contains(notice, w5aEscape(`"Pipefitter"`)) {
			t.Errorf("the 409 notice does not carry the on-list spelling \"Pipefitter\":\n%s", notice)
		}
		if !strings.HasSuffix(strings.TrimSpace(notice), "nothing was recorded") {
			t.Errorf("the 409 notice does not end in the amend screen's \"nothing was recorded\" — it did not come through the refusal arm:\n%s", notice)
		}
		want := w5aEscape((&domain.TradeRenameRefusal{
			Renamed:  domain.Trade{TradeID: cateringID, Name: "Catering"},
			Typed:    "Pipefitter",
			Existing: domain.Trade{TradeID: pipefitterID, Name: "Pipefitter"},
		}).Reason() + "; nothing was recorded")
		if !strings.Contains(notice, want) {
			t.Errorf("the 409 notice is not the domain's authored sentence.\nwant: %s\ngot:  %s", want, notice)
		}
		// NOT asserted: the typed name surviving in the form. MEASURED while
		// writing this file — buildTradePage (trade.go) resets typed.Fields
		// to the blank control set on EVERY amend refusal, so no arm keeps
		// the officer's typing; pre-existing, outside D-82, reported in
		// reports/witness/TESTER-d82.md (not independently re-measured there;
		// this pre-existing UI behaviour is outside D-82's scope).
	}
	if strings.Contains(body, "This post could not complete that") {
		t.Error("the page rendered the INTERNAL-ERROR notice — a refusal is not a fault (the type fell outside the errors.As partition)")
	}

	if wmAfter := d82Watermark(t, st); wmAfter != wmBefore {
		t.Errorf("watermark advanced on a refused rename: %d -> %d", wmBefore, wmAfter)
	}
	if hits := d82Named(t, st, "Pipefitter"); len(hits) != 1 || hits[0].TradeID != pipefitterID {
		t.Errorf("rows named Pipefitter after the POST = %+v, want exactly the original %q", hits, pipefitterID)
	}
	if hits := d82Named(t, st, "Catering"); len(hits) != 1 || hits[0].TradeID != cateringID {
		t.Errorf("rows named Catering after the POST = %+v, want exactly the original %q — refused, not merged", hits, cateringID)
	}
}

// TestD82TradeScreenRefusesACaseVariantRenameNamingTheOnListSpelling: the
// officer types "pipefitter"; the notice names "Pipefitter" (the row to
// pick) and the holder's id.
//
// CORRECTED CITATION (2026-09-06, evidence-auditor): same compile-impossible
// shape as the previous test above — this file cannot vet, let alone run,
// pre-D-82. MEASURED by teammate d82-web-409 on pre-fix ancestor `c9283b8`
// via a minimal probe (the case-variant POST): `= 303`, same Location
// `/trade?recorded=site-a.1.3` — a second heading, spelled differently,
// which D-76's fold exists to forbid, exactly as claimed. See
// reports/witness/D82-WEB-409.md §2-3 and reports/witness/CITATION-AUDIT.md
// row 5.
func TestD82TradeScreenRefusesACaseVariantRenameNamingTheOnListSpelling(t *testing.T) {
	srv, st := newVehicleAmendTestServer(t)
	pipefitterID := createTradeViaRoute(t, srv, "Pipefitter")
	cateringID := createTradeViaRoute(t, srv, "Catering")

	rec := postForm(t, srv, "/trade/amend", url.Values{"trade_id": {cateringID}, "name": {"pipefitter"}})
	if rec.Code != http.StatusConflict {
		t.Fatalf("POST /trade/amend trade_id=Catering name=\"pipefitter\" with Pipefitter listed = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	notice := d82RefusalNotice(t, rec.Body.String())
	if !strings.Contains(notice, pipefitterID) || !strings.Contains(notice, w5aEscape(`"Pipefitter"`)) {
		t.Errorf("the 409 notice does not name the holder %s under its on-list spelling \"Pipefitter\":\n%s", pipefitterID, notice)
	}
	if hits := d82Named(t, st, "Catering"); len(hits) != 1 || hits[0].TradeID != cateringID {
		t.Errorf("rows named Catering after the POST = %+v, want exactly the original %q", hits, cateringID)
	}
	if got, err := domain.TradeByID(st, cateringID); err != nil || got == nil || !strings.EqualFold(got.Name, "Catering") {
		t.Errorf("Catering's composed name after the refused POST = %v (err %v), want unchanged", got, err)
	}
}

// --- positive controls at the door -------------------------------------------

// TestD82TradeScreenStillRecordsARenameToANewName — 303 and the composed
// name changes. Green on the clean tree by design.
func TestD82TradeScreenStillRecordsARenameToANewName(t *testing.T) {
	srv, st := newVehicleAmendTestServer(t)
	createTradeViaRoute(t, srv, "Pipefitter")
	cateringID := createTradeViaRoute(t, srv, "Catering")

	rec := postForm(t, srv, "/trade/amend", url.Values{"trade_id": {cateringID}, "name": {"Scaffolding"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /trade/amend Catering → \"Scaffolding\" = %d, want 303 — a new name still records: %s", rec.Code, rec.Body.String())
	}
	if !strings.HasPrefix(rec.Header().Get("Location"), "/trade?recorded=") {
		t.Errorf("Location = %q, want the /trade?recorded= confirmation", rec.Header().Get("Location"))
	}
	if hits := d82Named(t, st, "Scaffolding"); len(hits) != 1 || hits[0].TradeID != cateringID {
		t.Errorf("rows named Scaffolding = %+v, want the Catering row %q under its new name", hits, cateringID)
	}
}

// TestD82TradeScreenStillRecordsARenameToTheTradesOwnName — the no-op rename
// answered 303 on the clean tree (measured) and must not now be caught as a
// duplicate of itself.
func TestD82TradeScreenStillRecordsARenameToTheTradesOwnName(t *testing.T) {
	srv, st := newVehicleAmendTestServer(t)
	createTradeViaRoute(t, srv, "Pipefitter")
	cateringID := createTradeViaRoute(t, srv, "Catering")

	rec := postForm(t, srv, "/trade/amend", url.Values{"trade_id": {cateringID}, "name": {"Catering"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /trade/amend Catering → \"Catering\" (its own name) = %d, want 303 as on the clean tree: %s", rec.Code, rec.Body.String())
	}
	if hits := d82Named(t, st, "Catering"); len(hits) != 1 || hits[0].TradeID != cateringID {
		t.Errorf("rows named Catering = %+v, want the one row %q", hits, cateringID)
	}
}

// TestD82TradeScreenStillRecordsTheSeparateSameAsMerge — D-82 refuses to
// merge THROUGH a rename; the merge as its own amendment ([[D-30]](a)) is
// untouched: after the refused rename, same_as still records at 303.
func TestD82TradeScreenStillRecordsTheSeparateSameAsMerge(t *testing.T) {
	srv, st := newVehicleAmendTestServer(t)
	pipefitterID := createTradeViaRoute(t, srv, "Pipefitter")
	cateringID := createTradeViaRoute(t, srv, "Catering")
	if rec := postForm(t, srv, "/trade/amend", url.Values{"trade_id": {cateringID}, "name": {"Pipefitter"}}); rec.Code != http.StatusConflict {
		t.Fatalf("precondition: the rename onto Pipefitter = %d, want 409", rec.Code)
	}

	rec := postForm(t, srv, "/trade/amend", url.Values{"trade_id": {cateringID}, "same_as": {pipefitterID}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /trade/amend Catering same_as Pipefitter after the refused rename = %d, want 303 — the merge is its own operation and must remain available: %s", rec.Code, rec.Body.String())
	}
	got, err := domain.TradeByID(st, cateringID)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.SameAs == nil || *got.SameAs != pipefitterID {
		t.Errorf("Catering.SameAs = %v, want %q", got.SameAs, pipefitterID)
	}
	if got.Name != "Catering" {
		t.Errorf("Catering's composed name = %q, want \"Catering\" — the refused rename recorded nothing", got.Name)
	}
}
