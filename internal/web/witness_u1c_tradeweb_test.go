// witness_u1c_tradeweb_test.go — witness phase, worktree u1c-trade.
//
// internal/web/trade.go had 25 unreached blocks (row-Y census,
// SiteControl-vm-extras/row-y-evidence/blocks19_unc.tsv, filtered to this
// file) and ZERO dedicated tests — only route-walk (w2_route_walk_test.go)
// and gating (w8_routes_test.go) tests touched it, and neither asserts
// anything about the page's own behaviour. y6-tradeweb's survey
// (reports/witness/y6-tradeweb.md §4) found these mostly reachable by plain
// HTTP round-trips once trade.html's form fields were known, and this file
// is that pass.
//
// A trade is a HEADING on the evacuation muster sheet (D-30) and carries the
// day/night handset numbers read aloud during a muster — so a refusal or a
// read failure on this screen that fails to fire corrupts a life-safety
// document rather than merely a UI nicety.
//
// ⛔ GATED, NOT TOUCHED HERE: handleTradeCreate's duplicate-name behaviour
// (trade.go:227-243) is backlog item G3 (James's ruling, carried from
// y6-tradeweb's report) — a real defect (refdata.go:572 exact-match vs
// intake.go's EqualFold) already found there. While building this file I
// MEASURED a second, distinct defect in the same neighbourhood, empirically
// (a throwaway test, run and deleted, never committed): trade.go:233's
// `strings.Contains(err.Error(), "already on this site's trade list")`
// checks for text CreateTrade's actual error never contains — refdata.go:573
// says "...is already on this site's list..." (no "trade" before "list").
// POSTing the SAME name twice to /trade/create measured 500 both times, not
// a 409 with the friendly refusal — the friendly duplicate-name sentence at
// trade.go:234 is dead code today regardless of case-folding. This is left
// for the record only: no test here asserts on it in either direction, and
// trade.go:227-243 (rows 227-228, 228-233, 233-236, 237-240, 242 of the
// census) are NOT among the blocks this file claims — touching any of them
// pins today's behaviour (bug or not) into the suite, which the gate on G3
// forbids regardless of which specific bug is in view.
//
// Also left out: line 203's tie-break (`return
// typed.Trades[i].TradeID < typed.Trades[j].TradeID`) needs two trades with
// the IDENTICAL name to observe, which is only reachable by bypassing
// CreateTrade's own duplicate check — the same neighbourhood as G3 — so it
// is left unwitnessed rather than fabricated around the gate. Row 182-186
// (buildTradePage's LoadTradeMergeMap error branch) is surveyed but not
// witnessed for a structural reason, not a time-budget one — see the
// comment above TestWitnessU1cTradePageTradesReadFailureAnswers500.
package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// postRawTradeForm is postForm's sibling for the two ParseForm-failure
// witnesses below: url.Values.Encode() always produces well-formed
// percent-encoding, so a genuinely malformed body has to be written by hand.
func postRawTradeForm(t *testing.T, srv *Server, path, rawBody string) *httptest.ResponseRecorder {
	t.Helper()
	cookie := loginCookie(t, srv, testLoginPassword)
	req := httptest.NewRequest("POST", path, strings.NewReader(rawBody))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// createTrade drives the real POST /trade/create route (never calling
// domain.CreateTrade directly) so the fixture itself exercises the create
// path the same way an officer would, and returns the minted TradeID parsed
// from the PRG redirect.
func createTradeViaRoute(t *testing.T, srv *Server, name string) string {
	t.Helper()
	rec := postForm(t, srv, "/trade/create", url.Values{"name": {name}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("test setup: POST /trade/create name=%q = %d, want 303: %s", name, rec.Code, rec.Body.String())
	}
	const prefix = "/trade?recorded="
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, prefix) {
		t.Fatalf("test setup: POST /trade/create redirect Location = %q, want prefix %q", loc, prefix)
	}
	// ⟦D23, fix2-fj 2026-10-04⟧ the redirect now ends in the notice seal
	// ("&ns=…", noticeseal.go); the id is the value up to it.
	id, _, _ := strings.Cut(strings.TrimPrefix(loc, prefix), "&")
	if id == "" {
		t.Fatalf("test setup: POST /trade/create redirect Location %q carries no trade id", loc)
	}
	return id
}

// --- rows 79-82, 162: the four non-merge amend setters + deref's real-value arm ---

// TestWitnessU1cTradeAmendSetsNameAndAllThreePhoneSlots kills the
// FUNC-NEVER-ENTERED name/phone_day/phone_night/phone_shared setters
// (trade.go:79-82, the tradeAmendSetters map) and deref's real-value arm
// (trade.go:162, `return *p`) in one round trip: a single POST /trade/amend
// naming all four facts, verified two ways — a direct re-read of the store
// (the redirect proves nothing by itself) and the rendered /trade page,
// scoped to the exact table cell each phone slot owns rather than a
// whole-body Contains.
func TestWitnessU1cTradeAmendSetsNameAndAllThreePhoneSlots(t *testing.T) {
	srv, st := newVehicleAmendTestServer(t)
	id := createTradeViaRoute(t, srv, "Alpha Fabrication")

	rec := postForm(t, srv, "/trade/amend", url.Values{
		"trade_id":     {id},
		"name":         {"Alpha Fab Corrected"},
		"phone_day":    {"555-0101"},
		"phone_night":  {"555-0102"},
		"phone_shared": {"555-0199"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /trade/amend (name+3 phones) = %d, want 303: %s", rec.Code, rec.Body.String())
	}

	got, err := domain.TradeByID(st, id)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatalf("trade %s vanished after amendment", id)
	}
	if got.Name != "Alpha Fab Corrected" {
		t.Errorf("trade.go:79 (name setter): store Name after amendment = %q, want %q", got.Name, "Alpha Fab Corrected")
	}
	if got.PhoneDay == nil || *got.PhoneDay != "555-0101" {
		t.Errorf("trade.go:80 (phone_day setter): store PhoneDay after amendment = %v, want \"555-0101\"", got.PhoneDay)
	}
	if got.PhoneNight == nil || *got.PhoneNight != "555-0102" {
		t.Errorf("trade.go:81 (phone_night setter): store PhoneNight after amendment = %v, want \"555-0102\"", got.PhoneNight)
	}
	if got.PhoneShared == nil || *got.PhoneShared != "555-0199" {
		t.Errorf("trade.go:82 (phone_shared setter): store PhoneShared after amendment = %v, want \"555-0199\"", got.PhoneShared)
	}

	body := w2Get(t, srv, "/trade", loginCookie(t, srv, testLoginPassword)).Body.String()
	if !strings.Contains(body, "<td>Alpha Fab Corrected</td>") {
		t.Errorf("trade.go:79: GET /trade does not carry the renamed trade's own cell <td>Alpha Fab Corrected</td>:\n%s", body)
	}
	// Each assertion is scoped to the exact <div>Label: number</div> the
	// template emits (trade.html:62) rather than a bare Contains(number) —
	// deref's nil-arm renders the SAME number as literal text nowhere else
	// on the page, but scoping to the element is what trade.go:162's
	// real-value arm actually controls: without it every slot would render
	// "not recorded" regardless of what was amended.
	for _, want := range []string{
		"<div>Day shift: 555-0101</div>",
		"<div>Night shift: 555-0102</div>",
		"<div>Shared: 555-0199</div>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("trade.go:162 (deref real-value arm) / phone slot rendering: GET /trade body does not contain %q:\n%s", want, body)
		}
	}
}

// --- rows 83, 194-196: the same_as setter + buildTradePage's merge marker ---

// TestWitnessU1cTradeAmendSameAsSetsTheMergeMarkerOnTheMergedTradesRow kills
// the same_as FUNC-NEVER-ENTERED setter (trade.go:83) and the merge-resolve
// arm in buildTradePage (trade.go:194-196, `if canon := merge.Resolve(...);
// canon != tr.TradeID`). Bravo is merged INTO Alpha, so Bravo's own row must
// carry the marker; Alpha's must not.
func TestWitnessU1cTradeAmendSameAsSetsTheMergeMarkerOnTheMergedTradesRow(t *testing.T) {
	srv, st := newVehicleAmendTestServer(t)
	alphaID := createTradeViaRoute(t, srv, "Alpha Fabrication")
	bravoID := createTradeViaRoute(t, srv, "Bravo Crew")

	rec := postForm(t, srv, "/trade/amend", url.Values{"trade_id": {bravoID}, "same_as": {alphaID}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /trade/amend (same_as) = %d, want 303: %s", rec.Code, rec.Body.String())
	}

	got, err := domain.TradeByID(st, bravoID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.SameAs == nil || *got.SameAs != alphaID {
		t.Fatalf("trade.go:83 (same_as setter): Bravo's store SameAs after amendment = %v, want %q", got, alphaID)
	}

	body := w2Get(t, srv, "/trade", loginCookie(t, srv, testLoginPassword)).Body.String()
	wantMarker := `<td>Bravo Crew <small class="note">merged into ` + alphaID + `</small></td>`
	if !strings.Contains(body, wantMarker) {
		t.Errorf("trade.go:194-196 (merge-resolve arm): GET /trade does not carry Bravo's own merge marker %q:\n%s", wantMarker, body)
	}
	if strings.Contains(body, `<td>Alpha Fabrication <small class="note">merged into`) {
		t.Errorf("Alpha's row carries a merge marker of its own — Alpha is the TARGET of the merge, not merged away")
	}
}

// --- row 199-200 (and, on the same fixture, 200-202): buildTradePage's sort ---

// TestWitnessU1cTradePageSortsTradesByNameNotInsertionOrder kills
// trade.go:199-200 (the sort.Slice call/closure — every prior test in this
// package that ever hit GET /trade used at most one seeded trade, so the
// comparator's body never ran) and, on the same fixture, trade.go:200-202's
// name-comparison branch.
//
// ⚠ MEASURED, NOT ASSUMED: a first draft created "Zeta Crew" before "Alpha
// Fabrication", reasoning that creation order and name order would then
// disagree. They did not — domain.Trades' own unsorted order is by TradeID
// (dumpSection's OrderBy), a random hex suffix (crypto/rand, ids.go)
// uncorrelated with creation order, and in that run it happened to already
// put Alpha first. A plant that removed the sort ENTIRELY still passed —
// caught by watching the plant actually fail to fail, not by reasoning
// about "insertion order" the store never promised. Fixed by not depending
// on creation order at all: two trades are created under placeholder
// names, then RENAMED (via the same /trade/amend this file already
// witnesses) so the alphabetically-first name lands on whichever TradeID
// sorts LATER — guaranteeing name order and TradeID order disagree
// regardless of which random ID either trade drew.
func TestWitnessU1cTradePageSortsTradesByNameNotInsertionOrder(t *testing.T) {
	srv, _ := newVehicleAmendTestServer(t)
	id1 := createTradeViaRoute(t, srv, "Placeholder One")
	id2 := createTradeViaRoute(t, srv, "Placeholder Two")

	// idFirst is whichever id domain.Trades' OWN unsorted order (by TradeID)
	// would list FIRST. Naming ITS trade "Zeta" and the other "Alpha" makes
	// the two orderings disagree by construction, not by luck.
	idFirst, idSecond := id1, id2
	if id2 < id1 {
		idFirst, idSecond = id2, id1
	}
	rename := func(id, name string) {
		t.Helper()
		if rec := postForm(t, srv, "/trade/amend", url.Values{"trade_id": {id}, "name": {name}}); rec.Code != http.StatusSeeOther {
			t.Fatalf("test setup: renaming %s to %q = %d, want 303: %s", id, name, rec.Code, rec.Body.String())
		}
	}
	rename(idFirst, "Zeta Crew")
	rename(idSecond, "Alpha Fabrication")

	body := w2Get(t, srv, "/trade", loginCookie(t, srv, testLoginPassword)).Body.String()
	alphaAt := strings.Index(body, "<td>Alpha Fabrication</td>")
	zetaAt := strings.Index(body, "<td>Zeta Crew</td>")
	if alphaAt == -1 || zetaAt == -1 {
		t.Fatalf("trade.go:199-203: GET /trade is missing one of the two seeded rows (Alpha at %d, Zeta at %d):\n%s", alphaAt, zetaAt, body)
	}
	if alphaAt >= zetaAt {
		t.Errorf("trade.go:199-203: \"Alpha Fabrication\" (TradeID sorts LATER: %s) renders at byte %d, \"Zeta Crew\" (TradeID sorts FIRST: %s) at %d — want Alpha before Zeta (name order), not TradeID order", idSecond, alphaAt, idFirst, zetaAt)
	}
}

// --- row 173-180: buildTradePage's own Trades() read failure ---

// TestWitnessU1cTradePageTradesReadFailureAnswers500 kills trade.go:173-180.
// Store.LocalTx (the sanctioned single-connection door, per y3-acks's
// fault-seam report and y6-tradeweb's M1/M2/M3) drops the "trade" table
// alone, isolating buildTradePage's own Trades() read.
//
// ⚠ MEASURED, NOT ASSUMED: handleTradePage answers a fault through plain
// `s.renderPage` (trade.go:210), never `renderPageStatus` — unlike every
// POST handler in this file, which explicitly sets 409/500. A first draft
// of this test asserted 500 and FAILED ON A CLEAN TREE (200) before any
// plant was ever applied — GET /trade's own internal-fault notice renders
// at an unchanged 200, always. Caught by running the assertion before
// reasoning past it, exactly the discipline this phase names. The status
// code is therefore not this arm's signal; the notice text and the log
// line are.
func TestWitnessU1cTradePageTradesReadFailureAnswers500(t *testing.T) {
	srv, st := newVehicleAmendTestServer(t)
	witnessY6DropTable(t, st, "trade")
	logBuf := w5aCaptureWebLog(t)

	rec := w2Get(t, srv, "/trade", loginCookie(t, srv, testLoginPassword))
	if rec.Code != http.StatusOK {
		t.Fatalf("trade.go:173-180: GET /trade with the trade table dropped = %d, want 200 (this route never changes status on a fault): %s", rec.Code, rec.Body.String())
	}
	wantNotice := `<div class="notice notice-warn">This post could not complete that — <strong>nothing was recorded</strong>. Try again, and if it repeats, radio it through.</div>`
	if !strings.Contains(rec.Body.String(), wantNotice) {
		t.Errorf("trade.go:173-180: body does not carry the internal-error notice element %q:\n%s", wantNotice, rec.Body.String())
	}
	if !strings.Contains(logBuf.String(), "trade page: trades:") {
		t.Errorf("trade.go:173-180: got the internal-error notice, but the log never carries \"trade page: trades:\" (log has: %q)", logBuf.String())
	}
}

// row 182-186 (buildTradePage's OWN LoadTradeMergeMap() error branch) is NOT
// witnessed here. MEASURED while building the test above (a direct,
// non-HTTP call, not reasoned past): domain.Trades() ITSELF calls
// readTradeAmendments (refdata.go:728, composing amendments onto the base
// rows) — the exact same table, through the exact same function, that
// domain.LoadTradeMergeMap calls at trade.go:181. Dropping "trade_amendment"
// therefore fails trade.go:172's Trades() call FIRST every time, hitting
// 173-180 and returning before line 181 is ever reached — confirmed
// directly: `domain.Trades(st)` after dropping "trade_amendment" already
// returns `domain: read trade amendments: ... no such table`. There is no
// remaining table to drop that fails the SECOND identical call while the
// FIRST, identical call succeeds — table-level fault injection cannot
// distinguish two calls to the same function over the same table. Unlike
// intake.go's M1/M2 (sequential reads of DIFFERENT tables), this pair reads
// the IDENTICAL table via the IDENTICAL query twice, so line 181's own `if
// err != nil` can only fire from a genuinely transient fault (a connection
// drop mid-request) that no tool in this tree can produce. Reported as
// NOT WITNESSABLE WITH THE TOOLS THIS PACKAGE HAS TODAY, not merely
// "not yet witnessed."

// --- rows 217-220 / 252-255: the two ParseForm guards ---

// TestWitnessU1cTradeCreateParseFormFailureRefuses kills trade.go:217-220.
// "%zz" is not valid percent-encoding, so r.ParseForm() itself errors
// (measured directly: url.QueryUnescape("%zz") -> `invalid URL escape
// "%zz"`) — a plain malformed body, no fault injection.
func TestWitnessU1cTradeCreateParseFormFailureRefuses(t *testing.T) {
	srv, st := newVehicleAmendTestServer(t)
	before, err := domain.Trades(st)
	if err != nil {
		t.Fatal(err)
	}

	rec := postRawTradeForm(t, srv, "/trade/create", "name=%zz")
	if rec.Code != http.StatusConflict {
		t.Fatalf("trade.go:217-220: POST /trade/create with an unparseable body = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	wantNotice := `<div class="notice notice-warn">That form did not arrive intact. Nothing was recorded. Reload the page and enter it again.</div>`
	if !strings.Contains(rec.Body.String(), wantNotice) {
		t.Errorf("trade.go:217-220: 409 body does not carry the notice element %q:\n%s", wantNotice, rec.Body.String())
	}

	after, err := domain.Trades(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Errorf("trade.go:217-220: trade count %d->%d — a ParseForm failure must record nothing", len(before), len(after))
	}
}

// TestWitnessU1cTradeAmendParseFormFailureRefuses kills trade.go:252-255,
// handleTradeAmend's sibling guard.
func TestWitnessU1cTradeAmendParseFormFailureRefuses(t *testing.T) {
	srv, st := newVehicleAmendTestServer(t)
	id := createTradeViaRoute(t, srv, "Alpha Fabrication")

	rec := postRawTradeForm(t, srv, "/trade/amend", "trade_id=%zz")
	if rec.Code != http.StatusConflict {
		t.Fatalf("trade.go:252-255: POST /trade/amend with an unparseable body = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	wantNotice := `<div class="notice notice-warn">That form did not arrive intact. Nothing was recorded. Reload the page and enter it again.</div>`
	if !strings.Contains(rec.Body.String(), wantNotice) {
		t.Errorf("trade.go:252-255: 409 body does not carry the notice element %q:\n%s", wantNotice, rec.Body.String())
	}

	got, err := domain.TradeByID(st, id)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Name != "Alpha Fabrication" {
		t.Errorf("trade.go:252-255: trade %s after the malformed POST = %v, want the original, untouched \"Alpha Fabrication\"", id, got)
	}
}

// --- rows 272-273 / 273-275: the amend clear-list loop ---

// TestWitnessU1cTradeAmendClearListRemovesAPositionalNumber kills the FOR
// loop over r.Form["clear"] (trade.go:272-273) and its non-empty-value body
// (trade.go:273-275): a real "clear this" checkbox tick, un-setting a
// previously-recorded phone_day back to absent (nil, not empty string —
// deref's OTHER arm, trade.go:159-160, already covered by every trade with
// no number recorded at all, so this test's job is only the clear path).
func TestWitnessU1cTradeAmendClearListRemovesAPositionalNumber(t *testing.T) {
	srv, st := newVehicleAmendTestServer(t)
	id := createTradeViaRoute(t, srv, "Alpha Fabrication")
	if rec := postForm(t, srv, "/trade/amend", url.Values{"trade_id": {id}, "phone_day": {"555-0101"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("test setup: recording phone_day = %d, want 303: %s", rec.Code, rec.Body.String())
	}

	rec := postForm(t, srv, "/trade/amend", url.Values{"trade_id": {id}, "clear": {"phone_day"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("trade.go:272-275: POST /trade/amend clear=phone_day = %d, want 303: %s", rec.Code, rec.Body.String())
	}

	got, err := domain.TradeByID(st, id)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.PhoneDay != nil {
		t.Fatalf("trade.go:272-275: store PhoneDay after clearing = %v, want nil (absent)", got)
	}

	body := w2Get(t, srv, "/trade", loginCookie(t, srv, testLoginPassword)).Body.String()
	if !strings.Contains(body, `<div>Day shift: <span class="note">not recorded</span></div>`) {
		t.Errorf("trade.go:272-275: GET /trade does not render the day-shift slot as not recorded after clearing:\n%s", body)
	}
}

// --- row 285-288: handleTradeAmend's TradeAmendRefusal branch ---

// TestWitnessU1cTradeAmendNoOpRefusalRendersReasonAndRecordsNothing kills
// trade.go:285-288 (`errors.As(err, &refusal)`). AmendTrade's own Arm 1
// (trade_amend.go: "the amendment changes nothing (no field set, nothing
// cleared)") is the cheapest *TradeAmendRefusal to reach: a syntactically
// valid amendment — a real trade id, every field left blank, no clear —
// that the domain refuses on its OWN merits, nothing to do with G3's
// duplicate-name question.
func TestWitnessU1cTradeAmendNoOpRefusalRendersReasonAndRecordsNothing(t *testing.T) {
	srv, st := newVehicleAmendTestServer(t)
	id := createTradeViaRoute(t, srv, "Alpha Fabrication")

	rec := postForm(t, srv, "/trade/amend", url.Values{"trade_id": {id}})
	if rec.Code != http.StatusConflict {
		t.Fatalf("trade.go:285-288: POST /trade/amend with every field blank = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	wantNotice := `<div class="notice notice-warn">the amendment changes nothing (no field set, nothing cleared); nothing was recorded</div>`
	if !strings.Contains(rec.Body.String(), wantNotice) {
		t.Errorf("trade.go:285-288: 409 body does not carry the refusal notice element %q:\n%s", wantNotice, rec.Body.String())
	}

	got, err := domain.TradeByID(st, id)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Name != "Alpha Fabrication" {
		t.Errorf("trade.go:285-288: trade %s after the refused no-op = %v, want the original, untouched \"Alpha Fabrication\"", id, got)
	}
}

// --- row 295-298: handleTradeAmend's own internal-error fallback ---

// TestWitnessU1cTradeAmendStoreWriteFailureAnswers500 kills
// trade.go:295-298 — the arm reached only when AmendTrade returns an error
// that is NEITHER a *TradeAmendRefusal NOR a *TradeAmendInputError. The
// trade is created BEFORE event_log is dropped, so AmendTrade's own
// TradeByID read (trade_amend.go, "trade" table) and every validation arm
// (1-9) succeed against a genuinely valid amendment; only the write itself
// (st.Author, landing in event_log) fails — the same table M3 used to
// isolate handleIntake's own write, chosen because it is untouched by any
// of AmendTrade's reads.
func TestWitnessU1cTradeAmendStoreWriteFailureAnswers500(t *testing.T) {
	srv, st := newVehicleAmendTestServer(t)
	id := createTradeViaRoute(t, srv, "Alpha Fabrication")
	witnessY6DropTable(t, st, "event_log")
	logBuf := w5aCaptureWebLog(t)

	rec := postForm(t, srv, "/trade/amend", url.Values{"trade_id": {id}, "phone_day": {"555-0101"}})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("trade.go:295-298: POST /trade/amend with event_log dropped = %d, want 500: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(logBuf.String(), "trade amend:") {
		t.Errorf("trade.go:295-298: got 500, but the log never carries \"trade amend:\" (log has: %q)", logBuf.String())
	}
}
