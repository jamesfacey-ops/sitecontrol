// w8_scoped_read_test.go — W8-18(a): M-1, the ROWS-DECODED instrument,
// ⚠ NEVER A STOPWATCH.
//
// Written from the LOCKED W8 criteria only. No built implementation was read.
//
// [DOC — W5-POSTLOCK-AMENDMENTS.md:1643-1650] "NOT closed, and it is W7's and
// W8's to close … Owner for the W7/W8 clone: W7's and W8's TEST-WRITERS, AT
// CRITERIA-LOCK TIME. Trigger: THE LOCK OF EACH CHUNK'S CRITERIA — each will
// clone W5d-4 verbatim, and the gate's finding is that cloning the criterion
// WITHOUT CLONING THIS FILE reproduces the gap exactly."
//
// ⚠ NEVER DIAGNOSE BY STOPWATCH. The prescription is "a row-count assertion,
// not a stopwatch", and this environment's own standing rule says why —
// internal/domain alone measures 205-572 s here, and under four concurrent
// `go test` one package measured 1430 s against 29.9 s.
//
// ⭐⭐ THE `[GAP]` THIS FILE CLOSES. The lock leaves the rows-decoded LITERAL
// as an open [GAP] — "the test-writer's to establish BY MEASUREMENT and
// deliberately not guessed here… What is fixed is the FORM" — and its own
// ⟦LOCK — FAL-F9 + FIX-F15⟧ reduces the gap to "THE EXPECTATION IS A LITERAL
// DERIVED BY ARITHMETIC OVER THE FIXTURE'S OWN CONSTANTS", never an absolute
// counter reading, never a before/after CONFIGURATION delta, and NEVER READ
// OFF WHAT THE BUILT IMPLEMENTATION HAPPENS TO PRINT.
//
// The value is established below by MEASURING THE SHIPPED ANALOGUE with W8's
// own constants — see TestW8RowsDecodedLiteralIsCalibratedOnTheShippedAnalogue,
// which is the measurement, and §3 of reports/window-build/W8-tests.md, which
// records what it returned.
package domain

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	msqlite "modernc.org/sqlite"

	"sitecontrol/internal/store"
)

// --- W8's own probe names and its own sync.Once --------------------------
//
// ⚠ CONCRETE BUILDER HAZARD, MEASURED: msqlite.MustRegisterScalarFunction
// PANICS ON A DUPLICATE NAME, and both existing files guard registration with
// a package-level sync.Once and DISTINCT names [MEASURED —
// w5d_scoped_read_test.go:115,123-124; w6_scoped_read_test.go:49,53-54,62-68].
// W8 mints its OWN w8_* names and its OWN Once.
//
// ⚠ THE CRITERION ASSERTS THE PROPERTY — "no probe name collides with any name
// already registered in the package", derived by scan — AND ASSERTS NO COUNT.
// [MEASURED: two registrations exist today; W7's draft claims a third; A
// HAND-KEPT "W8 IS THE Nth" WOULD BE A NUMBER THAT ROTS BETWEEN TWO UNLOCKED
// DRAFTS, which is this project's recurring class.]
// ⟦W7-RECONCILE — W7 and W8 must not choose colliding prefixes.⟧
const (
	w8SideRowProbeFn = "w8_side_rows_decoded_probe"
	w8BaseRowProbeFn = "w8_base_rows_decoded_probe"
)

var (
	w8SideRowsDecoded atomic.Int64
	w8BaseRowsDecoded atomic.Int64
	w8ProbeOnce       sync.Once
)

func w8RegisterRowProbes(t *testing.T) {
	t.Helper()
	w8ProbeOnce.Do(func() {
		msqlite.MustRegisterScalarFunction(w8SideRowProbeFn, 1,
			func(_ *msqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
				w8SideRowsDecoded.Add(1)
				return args[0], nil
			})
		msqlite.MustRegisterScalarFunction(w8BaseRowProbeFn, 1,
			func(_ *msqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
				w8BaseRowsDecoded.Add(1)
				return args[0], nil
			})
	})
}

// w8MeasureDecoded runs fn and returns (side rows, base rows) decoded while
// it ran.
//
// ⭐ THE READING IS A PER-CALL DELTA AROUND ONE MEASURED CALL — the landed
// form, and THE ONLY SAFE ONE AGAINST A CUMULATIVE COUNTER [MEASURED —
// w5d_scoped_read_test.go:105-113: "It is a package-level counter … Readings
// are taken as DELTAS around one call, never as absolutes."]. ⚠ Revision 2's
// sentence FORBADE THE MANDATED CLONE'S OWN READING FORM.
func w8MeasureDecoded(fn func()) (side, base int64) {
	s0, b0 := w8SideRowsDecoded.Load(), w8BaseRowsDecoded.Load()
	fn()
	return w8SideRowsDecoded.Load() - s0, w8BaseRowsDecoded.Load() - b0
}

// w8InstallShadowViews shadows a base table and its amendment side table with
// views of the same shape, one column of each running through a probe.
//
// ⚠ THE PROBE COLUMNS ARE CHOSEN, NOT PICKED, and the choice is load-bearing:
// the probe must wrap a column appearing in NEITHER the WHERE NOR the ORDER
// BY, so it fires once per row EMITTED rather than once per row SCANNED.
// W8-18(a) names them: `cleared` for the amendment table (the composition
// filters on the entity id and orders on site_id/node_epoch/seq; `cleared` is
// in neither) and `site_id` for the base table (the base read filters on the
// entity id and has no ORDER BY).
//
// ⚠ TWO TABLES ARE SHADOWED, NOT ONE: if the composed read composes over a
// base-row read the way PersonByID does [MEASURED — refdata.go:668-685:
// oneRow[Person] then readPersonAmendmentsFor], SHADOWING ONE LEAVES THE
// OTHER'S DECODE INVISIBLE.
func w8InstallShadowViews(t *testing.T, st *store.Store, baseTable string, baseCols []string, sideTable string, sideCols []string) error {
	t.Helper()
	proj := func(cols []string, probeCol, fn string) string {
		out := make([]string, 0, len(cols))
		for _, c := range cols {
			if c == probeCol {
				out = append(out, fn+"("+c+") AS "+c)
				continue
			}
			out = append(out, c)
		}
		return strings.Join(out, ", ")
	}
	return st.LocalTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`CREATE TEMP VIEW ` + sideTable + ` AS SELECT ` +
			proj(sideCols, "cleared", w8SideRowProbeFn) + ` FROM main.` + sideTable); err != nil {
			return err
		}
		_, err := tx.Exec(`CREATE TEMP VIEW ` + baseTable + ` AS SELECT ` +
			proj(baseCols, "site_id", w8BaseRowProbeFn) + ` FROM main.` + baseTable)
		return err
	})
}

// --- W8-18(a)'s fixture constants, and the two guards --------------------
//
// ⟦LOCK — FAL-F9 + FIX-F15: "AND THE FIXTURE ASSERTS ITS OWN PRECONDITIONS,
// in the landed idiom" [MEASURED — w5d_scoped_read_test.go:214-232].⟧
const (
	w8TargetAmendments = 3
	w8OtherAmendments  = 9
	w8Trades           = 4
)

// ⭐⭐ THE ROWS-DECODED LITERALS, DERIVED BY ARITHMETIC OVER THE CONSTANTS
// ABOVE — which is what reduces the lock's [GAP] to "the count follows from
// the constants above, WHICH IS NOT A GAP".
//
//	side rows decoded by one scoped composed read = w8TargetAmendments = 3
//	base rows decoded by one scoped composed read = 1  (the entity's own row)
//
// ⚠ ESTABLISHED BY MEASUREMENT, not by reading the built implementation:
// TestW8RowsDecodedLiteralIsCalibratedOnTheShippedAnalogue runs THIS SAME
// INSTRUMENT against the shipped company analogue with THESE SAME CONSTANTS
// and pins what it returns. That is the strongest evidence available while
// the trade entity is unbuilt, and it is stated rather than assumed.
const (
	w8WantSideRowsDecoded = int64(w8TargetAmendments)
	w8WantBaseRowsDecoded = int64(1)
)

// TestW8RowsDecodedLiteralIsCalibratedOnTheShippedAnalogue ESTABLISHES the
// [GAP]'s value BY MEASUREMENT.
//
// ⚠ THE MEASUREMENT IS OF THE SHIPPED ANALOGUE, AND THAT BOUND IS STATED: the
// trade entity does not exist, so the literal cannot be measured on trade. It
// is measured on `company`, whose composed read is the shape W8-18(a) names
// (oneRow + a scoped side-table read), with W8's OWN constants — so the
// arithmetic that produces 3 and 1 is exercised end to end rather than
// reasoned about.
//
// ⚠ IT IS NOT "READ OFF WHAT THE BUILT IMPLEMENTATION HAPPENS TO PRINT": the
// expectation is fixed by the constants BEFORE the call, and the measurement
// either meets it or reds. A calibration that adopted whatever number came
// back would be the defect FIX-F15 forbids.
func TestW8RowsDecodedLiteralIsCalibratedOnTheShippedAnalogue(t *testing.T) {
	w8RegisterRowProbes(t)
	st := w3NewStore(t)

	target, err := CreateCompany(st, "site-a", 1, "Target Ltd", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	other, err := CreateCompany(st, "site-a", 1, "Other Ltd", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// w8Trades - 2 further entities, so the base table holds more than one
	// row and the base-row guard (4 > 1) is met.
	for i := 0; i < w8Trades-2; i++ {
		if _, err := CreateCompany(st, "site-a", 1, fmt.Sprintf("Filler %d Ltd", i), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < w8TargetAmendments; i++ {
		w8AmendCompany(t, st, target.CompanyID, fmt.Sprintf("Target Ltd rev %d", i+1), w3Base+int64(i+1)*w3Hour)
	}
	for i := 0; i < w8OtherAmendments; i++ {
		w8AmendCompany(t, st, other.CompanyID, fmt.Sprintf("Other Ltd rev %d", i+1), w3Base+int64(i+20)*w3Hour)
	}

	// --- THE TWO GUARDS, each failing as `PRECONDITION:`, never as a
	// production red, and each carrying its own "the fixture poses no case"
	// message.
	totalSide := w8CountRows(t, st, "company_amendment", "")
	targetSide := w8CountRows(t, st, "company_amendment", "WHERE company_id = ?", target.CompanyID)
	totalBase := w8CountRows(t, st, "company", "")
	if totalSide != w8TargetAmendments+w8OtherAmendments || targetSide != w8TargetAmendments {
		t.Fatalf("PRECONDITION: the fixture did not land as written — company_amendment holds %d rows (%d the "+
			"target's), want %d (%d). The fold, not this assertion, is what changed",
			totalSide, targetSide, w8TargetAmendments+w8OtherAmendments, w8TargetAmendments)
	}
	if totalSide <= targetSide {
		t.Fatalf("PRECONDITION: the fixture poses no case — the side table holds %d rows and the target owns %d of "+
			"them, so a whole-table read and a scoped read decode the same number and this could never fail (%d > %d)",
			totalSide, targetSide, w8TargetAmendments+w8OtherAmendments, w8TargetAmendments)
	}
	if totalBase <= 1 {
		t.Fatalf("PRECONDITION: the fixture poses no case on the SECOND shadowed table — it holds %d row(s), so a "+
			"whole-table base read and a scoped one decode the same number (%d > 1)", totalBase, w8Trades)
	}

	// ⟦SchemaVersion 11, 2026-09-06⟧ The shadow view mirrors company_amendment's
	// EXACT column set, so `seen` ([[D-92]]) is projected through — an
	// instrument that omitted it would red every read with "no such column"
	// and calibrate nothing. ⟦was: "…\"same_as\", \"cleared\"}"⟧
	if err := w8InstallShadowViews(t, st,
		"company", []string{"company_id", "site_id", "name", "recorded_by"},
		"company_amendment", []string{"site_id", "node_epoch", "seq", "company_id", "name", "same_as", "cleared", "seen"},
	); err != nil {
		t.Fatalf("CONTROL FAILED (instrument): could not install the rows-decoded shadow views: %v", err)
	}

	// --- CONTROL 1, the HIGH end: a WHOLE-TABLE read must count every row.
	// If it counts zero the views are not in effect; if it counts anything
	// else the probes are not firing once per emitted row.
	var whole []companyAmendmentRow
	sideHigh, _ := w8MeasureDecoded(func() { whole, err = readCompanyAmendments(st) })
	if err != nil {
		t.Fatal(err)
	}
	if len(whole) != totalSide || sideHigh != int64(totalSide) {
		t.Fatalf("CONTROL FAILED (instrument, high end): the whole-table read returned %d rows and the probe "+
			"counted %d, want %d for both. Either the temp view is not shadowing company_amendment or the probe "+
			"is not firing once per emitted row — THIS IS THE INSTRUMENT, NOT THE COMPOSED READ",
			len(whole), sideHigh, totalSide)
	}

	// --- CONTROL 2, the LOW end: the scoped SQL, driven by hand.
	var scoped []companyAmendmentRow
	sideLow, _ := w8MeasureDecoded(func() { scoped, err = readCompanyAmendmentsFor(st, other.CompanyID) })
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped) != w8OtherAmendments || sideLow != int64(w8OtherAmendments) {
		t.Fatalf("CONTROL FAILED (instrument, low end): a scoped read returned %d rows and the probe counted %d, "+
			"want %d for both. The probe cannot distinguish a scoped read from a whole-table one, so it cannot "+
			"judge the composed read", len(scoped), sideLow, w8OtherAmendments)
	}

	// --- THE MEASUREMENT that establishes the [GAP]'s value. ---------------
	var composed *Company
	side, base := w8MeasureDecoded(func() { composed, err = CompanyByID(st, target.CompanyID) })
	if err != nil {
		t.Fatal(err)
	}
	if side != w8WantSideRowsDecoded {
		t.Fatalf("W8-18(a) CALIBRATION: one composed read decoded %d side-table rows, want the LITERAL %d "+
			"(= w8TargetAmendments). ⚠ The expectation is DERIVED BY ARITHMETIC OVER THE FIXTURE'S OWN CONSTANTS "+
			"and fixed BEFORE the call — never read off what the implementation happens to print. If this number "+
			"is genuinely different, the arithmetic is wrong and the LITERAL must be re-derived, not adjusted",
			side, w8WantSideRowsDecoded)
	}
	if base != w8WantBaseRowsDecoded {
		t.Fatalf("W8-18(a) CALIBRATION: one composed read decoded %d BASE rows, want the LITERAL %d. "+
			"⚠ TWO tables are shadowed, not one: shadowing only the side table leaves the base read's decode "+
			"INVISIBLE, and PersonByID's shape (oneRow then a scoped side read) is what makes that matter",
			base, w8WantBaseRowsDecoded)
	}

	// ⭐ AND THE COMPOSED VALUE IS PINNED BESIDE THE COUNT, so A SCOPED READ
	// THAT RETURNS NOTHING CANNOT SCORE: a read returning zero rows would
	// score a perfect 3 on a fixture of 3 if only the count were checked.
	if composed == nil || composed.Name != fmt.Sprintf("Target Ltd rev %d", w8TargetAmendments) {
		got := "<nil>"
		if composed != nil {
			got = composed.Name
		}
		t.Fatalf("W8-18(a) CALIBRATION: the composed value is %q, want %q — the scoped read must still be THE READ "+
			"THAT ANSWERS", got, fmt.Sprintf("Target Ltd rev %d", w8TargetAmendments))
	}

	t.Logf("W8-18(a) [GAP] ESTABLISHED BY MEASUREMENT on the shipped analogue, with W8's own constants "+
		"(target=%d, other=%d, entities=%d): ONE composed read decodes %d side-table rows and %d base row. "+
		"⚠ Measured on `company` because trade is unbuilt; the bound is stated rather than hidden.",
		w8TargetAmendments, w8OtherAmendments, w8Trades, side, base)
}

// TestW8TradeByIDDecodesOnlyItsOwnTradesRows is W8-18(a) itself, now that the
// trade authoring path exists.
//
// ⭐ DELETE-AND-REPLACE, not an inventory edit (S125-DISPOSITIONS.md §O.4;
// W5-POSTLOCK-AMENDMENTS.md §205 row O): the PRECONDITION sentinel this test
// used to be is gone. This is
// TestW8RowsDecodedLiteralIsCalibratedOnTheShippedAnalogue's exact
// instrument — same fixture constants, same two PRECONDITION guards, both
// shadow views, CONTROL 1 (12 rows) and CONTROL 2 (9 rows) — re-pointed at
// `trade`/`trade_amendment` instead of `company`/`company_amendment`.
//
// RETIRES DECLARED-REDS.md R05: its stated condition is "the unconditional
// t.Fatal at w8_scoped_read_test.go:357 is removed — the gap it held is
// FILLED, so no tree can green it." The gap is filled below (a real
// measurement against TradeByID), not merely removed.
func TestW8TradeByIDDecodesOnlyItsOwnTradesRows(t *testing.T) {
	w8RegisterRowProbes(t)
	st := w3NewStore(t)

	target, err := CreateTrade(st, "site-a", 1, "Target Trades", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	other, err := CreateTrade(st, "site-a", 1, "Other Trades", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// w8Trades - 2 further entities, so the base table holds more than one
	// row and the base-row guard (4 > 1) is met.
	for i := 0; i < w8Trades-2; i++ {
		if _, err := CreateTrade(st, "site-a", 1, fmt.Sprintf("Filler %d Trades", i), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < w8TargetAmendments; i++ {
		w8AmendTrade(t, st, target.TradeID, fmt.Sprintf("Target Trades rev %d", i+1), w3Base+int64(i+1)*w3Hour)
	}
	for i := 0; i < w8OtherAmendments; i++ {
		w8AmendTrade(t, st, other.TradeID, fmt.Sprintf("Other Trades rev %d", i+1), w3Base+int64(i+20)*w3Hour)
	}

	// --- THE TWO GUARDS, each failing as `PRECONDITION:`, never as a
	// production red, and each carrying its own "the fixture poses no case"
	// message.
	totalSide := w8CountRows(t, st, "trade_amendment", "")
	targetSide := w8CountRows(t, st, "trade_amendment", "WHERE trade_id = ?", target.TradeID)
	totalBase := w8CountRows(t, st, "trade", "")
	if totalSide != w8TargetAmendments+w8OtherAmendments || targetSide != w8TargetAmendments {
		t.Fatalf("PRECONDITION: the fixture did not land as written — trade_amendment holds %d rows (%d the "+
			"target's), want %d (%d). The fold, not this assertion, is what changed",
			totalSide, targetSide, w8TargetAmendments+w8OtherAmendments, w8TargetAmendments)
	}
	if totalSide <= targetSide {
		t.Fatalf("PRECONDITION: the fixture poses no case — the side table holds %d rows and the target owns %d of "+
			"them, so a whole-table read and a scoped read decode the same number and this could never fail (%d > %d)",
			totalSide, targetSide, w8TargetAmendments+w8OtherAmendments, w8TargetAmendments)
	}
	if totalBase <= 1 {
		t.Fatalf("PRECONDITION: the fixture poses no case on the SECOND shadowed table — it holds %d row(s), so a "+
			"whole-table base read and a scoped one decode the same number (%d > 1)", totalBase, w8Trades)
	}

	if err := w8InstallShadowViews(t, st,
		"trade", w8TradeColumns,
		"trade_amendment", w8TradeAmendmentColumns,
	); err != nil {
		t.Fatalf("CONTROL FAILED (instrument): could not install the rows-decoded shadow views: %v", err)
	}

	// --- CONTROL 1, the HIGH end: a WHOLE-TABLE read must count every row.
	// If it counts zero the views are not in effect; if it counts anything
	// else the probes are not firing once per emitted row.
	var whole []tradeAmendmentRow
	sideHigh, _ := w8MeasureDecoded(func() { whole, err = readTradeAmendments(st) })
	if err != nil {
		t.Fatal(err)
	}
	if len(whole) != totalSide || sideHigh != int64(totalSide) {
		t.Fatalf("CONTROL FAILED (instrument, high end): the whole-table read returned %d rows and the probe "+
			"counted %d, want %d for both. Either the temp view is not shadowing trade_amendment or the probe "+
			"is not firing once per emitted row — THIS IS THE INSTRUMENT, NOT THE COMPOSED READ",
			len(whole), sideHigh, totalSide)
	}

	// --- CONTROL 2, the LOW end: the scoped SQL, driven by hand.
	var scoped []tradeAmendmentRow
	sideLow, _ := w8MeasureDecoded(func() { scoped, err = readTradeAmendmentsFor(st, other.TradeID) })
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped) != w8OtherAmendments || sideLow != int64(w8OtherAmendments) {
		t.Fatalf("CONTROL FAILED (instrument, low end): a scoped read returned %d rows and the probe counted %d, "+
			"want %d for both. The probe cannot distinguish a scoped read from a whole-table one, so it cannot "+
			"judge the composed read", len(scoped), sideLow, w8OtherAmendments)
	}

	// --- THE MEASUREMENT — W8-18(a) itself. ---------------------------------
	var composed *Trade
	side, base := w8MeasureDecoded(func() { composed, err = TradeByID(st, target.TradeID) })
	if err != nil {
		t.Fatal(err)
	}
	if side != w8WantSideRowsDecoded {
		t.Fatalf("W8-18(a): TradeByID decoded %d side-table rows, want the LITERAL %d (= w8TargetAmendments, "+
			"calibrated on the shipped `company` analogue by "+
			"TestW8RowsDecodedLiteralIsCalibratedOnTheShippedAnalogue). A number higher than the target's own "+
			"amendment count means TradeByID is decoding rows belonging to OTHER trades",
			side, w8WantSideRowsDecoded)
	}
	if base != w8WantBaseRowsDecoded {
		t.Fatalf("W8-18(a): TradeByID decoded %d BASE rows, want the LITERAL %d. TWO tables are shadowed, not "+
			"one: shadowing only the side table leaves the base read's decode INVISIBLE",
			base, w8WantBaseRowsDecoded)
	}

	// ⭐ AND THE COMPOSED VALUE IS PINNED BESIDE THE COUNT, so A SCOPED READ
	// THAT RETURNS NOTHING CANNOT SCORE: a read returning zero rows would
	// score a perfect 3 on a fixture of 3 if only the count were checked.
	if composed == nil || composed.Name != fmt.Sprintf("Target Trades rev %d", w8TargetAmendments) {
		got := "<nil>"
		if composed != nil {
			got = composed.Name
		}
		t.Fatalf("W8-18(a): the composed value is %q, want %q — the scoped read must still be THE READ THAT "+
			"ANSWERS", got, fmt.Sprintf("Target Trades rev %d", w8TargetAmendments))
	}

	t.Logf("W8-18(a): ONE composed TradeByID read decodes %d side-table rows and %d base row — matching the "+
		"literal calibrated on `company` (target=%d, other=%d, entities=%d).",
		side, base, w8TargetAmendments, w8OtherAmendments, w8Trades)
}

func w8AmendCompany(t *testing.T, st *store.Store, companyID, name string, atMS int64) {
	t.Helper()
	if _, err := AmendCompany(st, "site-a", 1, CompanyAmendment{CompanyID: companyID, Name: &name}, atMS, "default"); err != nil {
		t.Fatalf("fixture defect: amending %s: %v", companyID, err)
	}
}

func w8AmendTrade(t *testing.T, st *store.Store, tradeID, name string, atMS int64) {
	t.Helper()
	if _, err := AmendTrade(st, "site-a", 1, TradeAmendment{TradeID: tradeID, Name: &name}, atMS, "default"); err != nil {
		t.Fatalf("fixture defect: amending %s: %v", tradeID, err)
	}
}

// w8CountRows counts rows in the REAL table (main-qualified, so it never goes
// through a shadow view and never disturbs a counter).
func w8CountRows(t *testing.T, st *store.Store, table, where string, args ...any) int {
	t.Helper()
	var n int
	if err := st.LocalTx(func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT count(*) FROM main.`+table+` `+where, args...).Scan(&n)
	}); err != nil {
		t.Fatalf("counting %s rows: %v", table, err)
	}
	return n
}

// TestW8ProbeNamesDoNotCollide is W8-18(a)'s stated builder hazard, asserted
// as THE PROPERTY and with NO COUNT.
//
// ⚠ msqlite.MustRegisterScalarFunction PANICS ON A DUPLICATE NAME. Both
// existing files guard registration with a package-level sync.Once and
// DISTINCT names; W8 mints its own w8_* names and its own Once.
//
// ⚠ NO COUNT IS ASSERTED: "two registrations exist today; W7's draft claims a
// third; A HAND-KEPT 'W8 IS THE Nth' WOULD BE A NUMBER THAT ROTS BETWEEN TWO
// UNLOCKED DRAFTS."
//
// ⟦NO PRE-CHUNK RED — the names are distinct today.⟧ Its evidence is the
// PLANT produced below.
func TestW8ProbeNamesDoNotCollide(t *testing.T) {
	names := w8RegisteredProbeNames(t)
	if len(names) == 0 {
		t.Fatal("PRECONDITION (instrument): the scan found NO MustRegisterScalarFunction name literals in " +
			"internal/domain's tests — it would report a clean pass over nothing")
	}

	// --- CONTROL FIRST, produced: a duplicated name must be reported.
	if dup := w8FirstDuplicate(append(append([]string(nil), names...), names[0])); dup == "" {
		t.Fatalf("W8-18(a) CONTROL FAILED: a deliberately duplicated probe name was NOT reported. " +
			"MustRegisterScalarFunction PANICS on a duplicate, so a check that cannot see one protects nothing")
	}

	if dup := w8FirstDuplicate(names); dup != "" {
		t.Errorf("W8-18(a): probe name %q is registered more than once in package domain's tests (%v). "+
			"msqlite.MustRegisterScalarFunction PANICS on a duplicate name. ⚠ The criterion asserts THE PROPERTY — "+
			"no probe name collides with any name already registered in the package — and asserts NO COUNT: a "+
			"hand-kept \"W8 is the Nth\" is a number that rots between two unlocked drafts.\n"+
			"⟦W7-RECONCILE — W7 and W8 must not choose colliding prefixes.⟧", dup, names)
	}
	// And W8's own two are present, so this scan is measuring W8's clone and
	// not somebody else's.
	for _, mine := range []string{w8SideRowProbeFn, w8BaseRowProbeFn} {
		var found bool
		for _, n := range names {
			if n == mine {
				found = true
			}
		}
		if !found {
			t.Errorf("W8-18(a): W8's own probe name %q is not among the registered names %v — the scan is not "+
				"reaching this file, so its verdict about collisions does not cover W8's clone", mine, names)
		}
	}
}

// w8RegisteredProbeNames derives every scalar-function name registered by
// package domain's tests — from the SOURCE, never from a hand-kept list.
func w8RegisteredProbeNames(t *testing.T) []string {
	t.Helper()
	dir := filepath.Join(w8AppRoot(t), "internal", "domain")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	consts := map[string]string{}
	var callArgs []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		// Two passes over one token stream: collect `ident = "literal"`
		// constant bindings, and collect the identifier or literal that
		// follows MustRegisterScalarFunction(.
		fset := token.NewFileSet()
		file := fset.AddFile(e.Name(), fset.Base(), len(src))
		var sc scanner.Scanner
		sc.Init(file, src, nil, 0)
		var prev2, prev1 string
		var prev1Tok token.Token
		wantArg := 0
		for {
			_, tok, lit := sc.Scan()
			if tok == token.EOF {
				break
			}
			switch {
			case wantArg == 1 && tok == token.LPAREN:
				wantArg = 2
			case wantArg == 2:
				if tok == token.STRING {
					if v, err := strconv.Unquote(lit); err == nil {
						callArgs = append(callArgs, v)
					}
				} else if tok == token.IDENT {
					callArgs = append(callArgs, "$"+lit)
				}
				wantArg = 0
			case tok == token.IDENT && lit == "MustRegisterScalarFunction":
				wantArg = 1
			case tok == token.STRING && prev1Tok == token.ASSIGN && prev2 != "":
				if v, err := strconv.Unquote(lit); err == nil {
					consts[prev2] = v
				}
			}
			prev2, prev1 = prev1, lit
			prev1Tok = tok
			_ = prev1
		}
	}
	var out []string
	for _, a := range callArgs {
		if strings.HasPrefix(a, "$") {
			if v, ok := consts[a[1:]]; ok {
				out = append(out, v)
				continue
			}
			continue
		}
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

func w8FirstDuplicate(names []string) string {
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			return n
		}
		seen[n] = true
	}
	return ""
}
