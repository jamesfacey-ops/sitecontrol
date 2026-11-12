// witness_y6_tradeweb_test.go — witness phase, worktree y6-tradeweb.
//
// Targets Q1 and Q2 from the row-Y census (reports/laptop-pickup/U-row-Y-census.md via
// SiteControl-vm-extras/row-y-evidence/plants.py), both in handleIntake's TRADE block
// (intake.go:544-573):
//
//	Q1  intake.go:559  if strings.EqualFold(strings.TrimSpace(wg.Name), newTrade) {
//	Q2  intake.go:567  if wg.TradeID == form.TradeID {
//
// WHY THESE TWO, stated in the officer's terms rather than the guard's: at 04:10 an
// officer with gloves on does not scroll a long trade picker — they use the "not on the
// list — add it" escape (trade_id="__new__") and type the name they already know, e.g.
// "Welder". If Welder is already on this site's trade list, Q1 is the guard that catches
// the retype (case-insensitive, whitespace-trimmed) and refuses rather than minting a
// second trade with the same name. Q2 is its sibling on the OTHER branch of the same
// switch: when the officer instead POSTS a trade_id that IS on the list (the ordinary
// picker path, no retype involved), Q2 is what confirms the id is real and lets the
// enrolment through. Both guards are exercised by nothing today (census: 34 planted, 34
// survived) despite the accepting side of this same form being exercised constantly.
//
// A duplicated trade identity matters beyond bookkeeping: a trade is a HEADING on the
// evacuation muster sheet (D-30, EIGHT-REDS / muster grouping) — every trade group prints
// its own heading no matter how small. Two "Welder" rows split one crew across two
// headings on a document read aloud during an evacuation, which is exactly how the
// predecessor system reached 43 duplicate identities (see this phase's brief).
//
// ⚠ LIVE DESIGN TENSION, noted and NOT resolved here (per instruction): D-66/D-71 rule
// that the GATE's own "not on the list — add it" trade escape (gate.html / gate.go, a
// DIFFERENT screen — the fast lane) must NEVER refuse, and that its behaviour when the
// typed name already exists must be ASSERTED. Measured while orienting for this witness:
// gate.go's escape (NewTrade fields at gate.go:867,948,979,1005) carries the typed text
// through to a slot and is never itself checked against the existing trade list inside
// this package — whatever guards a duplicate there, if anything does, is downstream of
// this file. INTAKE (this file) is explicitly the OTHER lane — "slow, deliberate... done
// once per person" (intake.go:12-14) — and refusing here is the form's normal, designed
// behaviour throughout (company duplicates, unknown ids, missing phone all refuse the same
// way). Nothing about D-66/D-71 constrains intake.go's own guard; it is cited here only so
// a later reader does not mistake this file for the screen those rulings govern.
package web

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// tradeIDNamed is personNamed's sibling for trades — the trade-side fixture-defect
// guard every test below needs before it can trust which trade a typed name resolves to.
func tradeIDNamed(t *testing.T, srv *Server, name string) string {
	t.Helper()
	trades, err := domain.Trades(srv.Store)
	if err != nil {
		t.Fatal(err)
	}
	var hits []string
	for _, tr := range trades {
		if tr.Name == name {
			hits = append(hits, tr.TradeID)
		}
	}
	if len(hits) != 1 {
		t.Fatalf("test setup: %d trades named %q, want exactly 1 (ids %v)", len(hits), name, hits)
	}
	return hits[0]
}

// TestWitnessY6Q1TradeDuplicateNameGuardRefusesACaseAndWhitespaceRetype kills Q1
// (intake.go:559 — `if strings.EqualFold(strings.TrimSpace(wg.Name), newTrade)`).
//
// newGateTestServer seeds exactly one trade, "Inspection". This test poses the 04:10
// officer scenario: "not on the list — add it" (trade_id="__new__"), typed as
// "  inspection  " — same name, different case, padded with the whitespace a glove or an
// autocomplete leaves behind. The guard is EqualFold(TrimSpace(...)), so this must refuse.
//
// PLANTED (Q1: `if false && strings.EqualFold(...)`): the loop's body never runs, the
// switch falls out having matched nothing, and handleIntake proceeds to
// domain.CreateTradeRecordedBy unconditionally — minting a second "Inspection"-named
// trade and enrolling the person under it. This test then gets 303, not 409, and the exact
// refusal string never renders — which is the failure this witness is built to produce.
func TestWitnessY6Q1TradeDuplicateNameGuardRefusesACaseAndWhitespaceRetype(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	companyID := companyIDNamed(t, srv, "Kakivik Asset Management")

	// Fixture-defect guard (rule 6): confirm the exact seeded name and that it is the
	// only trade on the list, so "already on the list" can only mean this one.
	tradeIDNamed(t, srv, "Inspection")
	pBefore, cBefore, wBefore := countRows(t, srv)

	rec := postForm(t, srv, "/roster/intake",
		intakeForm2("Ferguson", "Mary", companyID, "", "__new__", "  inspection  "))

	if rec.Code != http.StatusConflict {
		t.Fatalf("Q1 (intake.go:559): retyping the existing trade \"Inspection\" as \"  inspection  \" via the not-on-the-list escape = %d, want 409 (refused as a duplicate): %s", rec.Code, rec.Body.String())
	}
	// w5aEscape (w5a_read_paths_test.go) is html/template's own escaping of the quote
	// and apostrophe this refusal sentence contains — the page renders `"` as `&#34;`
	// and `'` as `&#39;`, so the raw sentence never appears verbatim in the body.
	wantRefusal := w5aEscape(`"Inspection" is already on this site's trade list — choose it from the list rather than adding a second one.`)
	if body := rec.Body.String(); !strings.Contains(body, wantRefusal) {
		t.Errorf("Q1 (intake.go:559): 409 body does not carry the officer-facing refusal sentence.\nwant substring: %s\ngot body:       %s", wantRefusal, body)
	}

	pAfter, cAfter, wAfter := countRows(t, srv)
	if pAfter != pBefore || cAfter != cBefore || wAfter != wBefore {
		t.Errorf("Q1: a refused intake wrote rows: persons %d->%d, companies %d->%d, trades %d->%d — a caught duplicate must mint nothing, least of all a second trade with the name it was refused for", pBefore, pAfter, cBefore, cAfter, wBefore, wAfter)
	}
}

// TestWitnessY6Q2TradeIDGuardAcceptsAKnownTradeAndRefusesAnUnknownOne kills Q2
// (intake.go:567 — `if wg.TradeID == form.TradeID`).
//
// The ordinary picker path: the officer chooses an EXISTING trade from the <select>, so
// form.TradeID carries a real id and newTrade is empty. Q2's loop is what confirms that id
// is still on the list; TestARefusedIntakeAuthorsNothingAtAll already exercises its
// negative (an id that is NOT on the list refuses) — see intake_test.go:696-720 — but
// nothing exercises the accepting half of the SAME comparison, which is the half Q2 can
// break without moving that other test's needle at all.
//
// PLANTED (Q2: `if false && wg.TradeID == form.TradeID`): `found` can never become true, so
// EVERY trade_id — including one genuinely on the list — is refused as "no longer on the
// list". This test posts a real, current trade id and asserts the enrolment SUCCEEDS; under
// the plant it is refused instead, which is the failure this witness is built to produce.
func TestWitnessY6Q2TradeIDGuardAcceptsAKnownTradeAndRefusesAnUnknownOne(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	companyID := companyIDNamed(t, srv, "Kakivik Asset Management")
	inspectionID := tradeIDNamed(t, srv, "Inspection")

	rec := postForm(t, srv, "/roster/intake",
		intakeForm2("Ferguson", "Mary", companyID, "", inspectionID, ""))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("Q2 (intake.go:567): enrolling with trade_id=%q, a trade genuinely on this site's list, = %d, want 303: %s", inspectionID, rec.Code, rec.Body.String())
	}

	enrolled := personNamed(t, srv, "Ferguson, Mary")
	if len(enrolled) != 1 {
		t.Fatalf("Q2: %d roster rows named \"Ferguson, Mary\" after enrolment, want 1", len(enrolled))
	}
	if got := enrolled[0].TradeID; got == nil || *got != inspectionID {
		t.Errorf("Q2: enrolled person's TradeID = %v, want %q — the accepted trade id must be the one actually recorded, not merely a 303", got, inspectionID)
	}
}

// =======================================================================
// The three intake.go ERR-ARM blocks the census left unplanted (291-294,
// 498-501, 652-655) — added while "working outward" per the phase brief.
// =======================================================================
//
// My first report on this worktree called these three unwitnessable with the
// only fault-injection pattern I had found (st.Close(), which fails every
// read equally and so can only ever be observed hitting the FIRST read in a
// sequential run — never the second or third). y3-acks's fault-seam pass
// (reports/witness/y3-faultseam.md) found the actual sanctioned tool:
// Store.LocalTx (internal/store/local.go:44, doc comment "the sanctioned
// read/write path"), already used for DROP TABLE in internal/domain
// (w8_absences_test.go:487,500). Each of company/trade/person is its OWN
// SQL table (internal/domain/refdata.go:264,346,371 — "Table: company" /
// "trade" / "person"), and writes land in a FOURTH table, event_log
// (internal/store/log.go:271), separate from all three reads. Dropping ONE
// of these four tables therefore fails exactly the calls that touch it and
// nothing else — which is the granularity st.Close() could not offer.
//
// Each witness below still needs its OWN plant, authored by me (line-
// anchored, exact stripped text asserted, same discipline as
// row-y-evidence/plants.py — script kept at
// <scratch>/y6/y6_own_plants.py, ids M1/M2/M3): dropping a table proves the
// ERR-ARM is REACHABLE, but only disabling the `if err != nil` guard at
// that exact line and watching the SAME test's assertion flip proves the
// witness is looking at THAT arm and not some other error path.

// witnessY6DropTable drops one table on st's own connection via LocalTx —
// the sanctioned single-connection door — isolating exactly the calls that
// touch that table while every other table stays queryable.
func witnessY6DropTable(t *testing.T, st *store.Store, table string) {
	t.Helper()
	if err := st.LocalTx(func(tx *sql.Tx) error {
		_, err := tx.Exec("DROP TABLE " + table)
		return err
	}); err != nil {
		t.Fatalf("fixture defect: dropping table %q: %v", table, err)
	}
}

// TestWitnessY6M1RenderRosterTradesReadFailureAnswers500 kills M1
// (intake.go:291 — the `if err != nil` guarding `renderRoster`'s OWN
// `domain.Trades` read, reached from `GET /roster`, i.e. `handleRosterPage`,
// which does NO read of its own before delegating).
//
// The "trade" table is dropped; "person" and "company" are untouched, so
// renderRoster's Persons() and Companies() reads (which run BEFORE the
// Trades() read at line 290) succeed and the handler reaches its OWN trades
// read specifically. Clean tree: gateFail fires, 500. PLANTED (M1: `if false
// && err != nil`): the guard never fires, `trades` stays a nil slice, and
// the page renders anyway with an empty trade list instead of failing —
// this test then gets 200, not 500, which is the failure this witness is
// built to produce.
//
// u1b-web §3.5 classified M1 AMBIGUOUS-BUT-UNPRODUCED: no sibling form was
// reachable to prove the status-only assertion looks at THIS arm rather than
// some other. It also found the same discriminating shape M3 already uses is
// cheap to add here: intake.go:292 (this arm) and intake.go:499
// (handleIntake's OWN "trades" gateFail) log the IDENTICAL "gate: trades:"
// line, so the label alone does not discriminate IN GENERAL — but on THIS
// route, GET /roster -> handleRosterPage -> renderRoster runs no POST
// processing at all, so intake.go:292 is the only call anywhere in this
// request that can produce that line. Asserting it present pins the 500 to
// this arm on this route, even though the same label would not pin it across
// routes.
func TestWitnessY6M1RenderRosterTradesReadFailureAnswers500(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	witnessY6DropTable(t, st, "trade")
	logBuf := w5aCaptureWebLog(t)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/roster", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("M1 (intake.go:291): GET /roster with the trade table dropped = %d, want 500 (renderRoster's own Trades() read must fail loud): %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(logBuf.String(), "gate: trades:") {
		t.Errorf("M1 (intake.go:291): got 500, but the log never carries \"gate: trades:\" — a 500 from a DIFFERENT arm is not this arm caught (log has: %q)", logBuf.String())
	}
}

// TestWitnessY6M2HandleIntakeTradesReadFailureAnswers500 kills M2
// (intake.go:498 — the `if err != nil` guarding `handleIntake`'s validation
// read `trades, err := domain.Trades(s.Store)` at line 497).
//
// Same table-drop as M1, but the READING function this time is handleIntake
// itself, which reads Companies (line 492, succeeds — different table) BEFORE
// Trades (line 497, fails) and never reaches its own Persons read (line 502)
// or any refusal/authoring logic — this is the middle read of a run of three,
// exactly the position st.Close() could never isolate on its own.
//
// PLANTED (M2): the guard never fires, `trades` stays nil, and handleIntake
// proceeds into the TRADE switch with an empty trade list — a picked
// trade_id can never be found on an empty list, so (depending on the form)
// this either wrongly refuses via Q2's OWN unknown-id path (a different
// symptom entirely) or, for this test's form (no trade at all), sails on to
// enrol the person successfully. Either way this test's want-500 assertion
// fails, which is the point: a genuine store fault must not silently
// degrade into "no trades exist".
//
// u1b-web §3.1 confirmed the bare want-500 assertion WEAK — not because the
// kill is fake, but because the FORM's own success is only an assumption.
// The log-line discriminator that rescues M1/M3 CANNOT rescue M2: this
// route's OTHER "trades" reader, renderRoster's own read at intake.go:292,
// is reachable on the SAME route (POST /roster/intake -> a refusal ->
// s.renderRoster -> its Trades() call) and logs the identical "gate:
// trades:" line — so a 500 carrying that line does not by itself prove
// intake.go:498 caught it rather than a refusal falling through to :292.
// The fix is to check the ASSUMPTION instead: POST this exact form against
// the INTACT store first and require it reaches authoring (303) — which
// proves no refusal path exists for these exact field values — before
// dropping the table and requiring 500. "This form reaches authoring" is
// then a checked fact, not an assumption the test quietly depended on.
//
// ⚠ The precondition runs against its OWN fresh server/store, never the one
// the destructive half uses. Running both POSTs against the SAME store
// would enrol "Ferguson, Mary" via the precondition itself, so the SECOND
// (destructive) POST would trip the DUPLICATE-NAME refusal instead of
// reaching line 497 at all — refuseDuplicate -> s.renderRoster -> that
// function's OWN Trades() read (intake.go:292) — reintroducing, via the
// test's own fixture, exactly the ambiguity this precondition exists to
// close (measured while producing this test: with the M2 guard disabled, a
// shared store made this test pass for the WRONG reason, 500 via :292, not
// :498). Two independent stores means the destructive POST is the form's
// only attempt against that data, so the precondition's "no refusal" fact
// actually applies to it.
func TestWitnessY6M2HandleIntakeTradesReadFailureAnswers500(t *testing.T) {
	buildForm := func(srv *Server) url.Values {
		companyID := companyIDNamed(t, srv, "Kakivik Asset Management")
		return intakeForm2("Ferguson", "Mary", companyID, "", "", "")
	}

	preconditionSrv, _, _ := newGateTestServer(t)
	precondition := postForm(t, preconditionSrv, "/roster/intake", buildForm(preconditionSrv))
	if precondition.Code != http.StatusSeeOther {
		t.Fatalf("M2 precondition: this exact form against an INTACT store = %d, want 303 — it must reach authoring with nothing dropped, or a later 500 cannot be pinned to intake.go:498 rather than a refusal falling through to renderRoster's OWN Trades() read at intake.go:292 (identical \"gate: trades:\" log line): %s", precondition.Code, precondition.Body.String())
	}

	srv, st, _ := newGateTestServer(t)
	witnessY6DropTable(t, st, "trade")
	logBuf := w5aCaptureWebLog(t)

	rec := postForm(t, srv, "/roster/intake", buildForm(srv))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("M2 (intake.go:498): intake with the trade table dropped = %d, want 500 (handleIntake's own Trades() validation read must fail loud, not silently proceed on an empty trade list): %s", rec.Code, rec.Body.String())
	}
	// Belt: with the precondition above having proven the company check
	// never refuses for this exact form, renderRoster's OWN "gate: trades:"
	// (intake.go:292) cannot be reached here either — refuse() is what
	// calls it, and refuse() is never called before line 497's read. So the
	// log line is now a valid confirmation, not merely a repeat of the
	// ambiguous label.
	if !strings.Contains(logBuf.String(), "gate: trades:") {
		t.Errorf("M2 (intake.go:498): got 500, but the log never carries \"gate: trades:\" (log has: %q)", logBuf.String())
	}
}

// TestWitnessY6M3HandleIntakeCreateTradeWriteFailureAnswers500 kills M3
// (intake.go:652 — the `if err != nil` guarding the WRITE
// `domain.CreateTradeRecordedBy(...)`, reached only when the officer used the
// "not on the list — add it" escape).
//
// This one needs a DIFFERENT table than M1/M2: CreateTradeRecordedBy's own
// internal duplicate-name check (refdata.go:567, `dumpSection[Trade](st,
// "trade")`) reads the SAME "trade" table handleIntake's own validation read
// (line 497) already reads — dropping "trade" would fail the EARLIER read
// first and never reach this write at all (exactly the same-table limit
// y3-acks's report warned about). The write itself lands in "event_log"
// (store/log.go:271, `INSERT INTO event_log`), a FOURTH table untouched by
// any of the three projection reads — dropping THAT isolates the write
// alone: every prior read (Companies, Trades, Persons, and
// CreateTradeRecordedBy's own duplicate-check read) succeeds against intact
// projection tables, and only the authoring INSERT fails.
//
// An EXISTING company is used so CreateCompanyRecordedBy is never called —
// the only authoring attempt in this request is the one under test.
//
// PLANTED (M3): the guard never fires, `wg` stays its zero value, and
// `tradeID = &wg.TradeID` takes the address of an EMPTY STRING rather than a
// real trade id — execution falls through into CreatePersonWithDetails
// instead of stopping. ⚠ NON-OBVIOUS, and found only by watching the plant
// actually run rather than reasoning about it (standing rule: produce the
// failure, don't predict it): CreatePersonWithDetails is ALSO an authoring
// call into the SAME dropped event_log table, so it fails too and the
// handler still answers 500 overall — via gateFail("create person", ...)
// firing lower down, NOT M3's own gateFail("create trade", ...). A bare
// want-500 assertion is therefore BLIND to this plant: 500 in, 500 out,
// wrong arm both times. The discriminator is WHICH gateFail fired, so this
// test captures the log (w5aCaptureWebLog, this package) and asserts the
// specific line M3's own arm writes — "gate: create trade:" — is present.
// Under the plant that exact line never appears (only "gate: create
// person:" does), which is the failure this witness is built to produce.
func TestWitnessY6M3HandleIntakeCreateTradeWriteFailureAnswers500(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	companyID := companyIDNamed(t, srv, "Kakivik Asset Management")
	pBefore, _, wBefore := countRows(t, srv)
	witnessY6DropTable(t, st, "event_log")
	logBuf := w5aCaptureWebLog(t)

	rec := postForm(t, srv, "/roster/intake",
		intakeForm2("Ferguson", "Mary", companyID, "", "__new__", "Rigger"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("M3 (intake.go:652): intake naming a genuinely new trade with event_log dropped = %d, want 500 (CreateTradeRecordedBy's write must fail loud, not enrol a person with a corrupted trade id): %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(logBuf.String(), "gate: create trade:") {
		t.Errorf("M3 (intake.go:652): got 500, but the log never carries \"gate: create trade:\" — the request failed through a DIFFERENT gateFail call (the log has: %q). A 500 from the wrong arm is not this arm caught.", logBuf.String())
	}
	pAfter, _, wAfter := countRows(t, srv)
	if pAfter != pBefore || wAfter != wBefore {
		t.Errorf("M3: a 500'd intake still wrote rows: persons %d->%d, trades %d->%d", pBefore, pAfter, wBefore, wAfter)
	}
}
