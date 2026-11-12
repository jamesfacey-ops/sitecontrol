// w5d_scoped_read_test.go — G-1's durable fix (W5d gate, reports/
// window-build/W5d-falsification.md §6 G-1 and §9): W5d-4's OTHER disjunct.
//
// (m4) is written as a DISJUNCTION — "`CompanyByID` reading the whole
// `company_amendment` table (the M-1 shape), OR a `company_amendment`
// composition literal carrying a deviant/epoch-blind ORDER BY" — and the
// register books its kill against
// `TestW5dOneOrderBySpellingInCompositionByResolvedValue`, which is an
// ORDER-BY instrument. The gate
// produced the M-1 half on its own (`readCompanyAmendmentsFor` rewritten to
// read the whole table and filter in Go, the scoped constant left declared
// and unused) and MEASURED that the entire suite stayed green: the ORDER-BY
// scan cannot see it, and nothing else could either. The criterion's own
// headline sentence — "`CompanyByID` reads ONLY its own company's amendment
// rows" — had no failable check anywhere in the tree.
//
// The gate discharged the criterion's PERFORMANCE claim by one-off
// measurement (1.96 ms vs 46.78 ms at 10,000 rows, ~24x) and ruled that
// insufficient as an instrument, for the reason the builder gave first: a
// wall-clock threshold in the suite is a flake generator, and this
// environment's own standing rule is never diagnose by stopwatch —
// `internal/domain` alone measures 205-572s here. So this file asserts the
// same proposition WITHOUT a clock: not how LONG a lookup takes, but how
// many `company_amendment` rows it DECODES. That number is a property of
// the SQL the read issues, is identical on a fast machine and a loaded one,
// and is exactly what separates the two disjuncts.
//
// KILLER MUTANT: (m4) half 1 — the M-1 shape. `readCompanyAmendmentsFor`
// reads `companyAmendmentCompositionSQL` (the whole table) and filters by
// companyID in Go. It returns byte-identical rows, composes an identical
// company, and is invisible to every other test in the tree; here it
// decodes every row in the table instead of this company's, and this test
// reds naming it. PRODUCED, both directions — see "the produced red" below.
//
// ⚠ W7 (vehicle) and W8 (trade) clone W5d-4 verbatim and inherit the hole
// with it. This file is the shape to clone: the criterion's scoped-read
// sentence gets a rows-decoded assertion of its own, not a second ORDER-BY
// scan and not a stopwatch.
//
// --- how the instrument works, and why it is not a stopwatch -------------
//
// `queryCompanyAmendments` decodes one `companyAmendmentRow` per row the
// query EMITS. There is no seam in production to count that (adding one
// would be a production change, which this pass is not permitted to make
// and which G-1 does not need), so the count is taken one level down, in
// SQLite itself:
//
//   - a scalar SQL function `w5d_rows_decoded_probe(x)` is registered on
//     the modernc driver; it returns its argument unchanged and increments
//     a counter;
//   - a TEMP VIEW named `company_amendment` shadows `main.company_amendment`
//     on the store's single connection, projecting the same seven columns in
//     the same order, with the probe wrapped around ONE of them.
//
// Production's own SQL — unmodified, still naming `company_amendment` —
// then runs against the view, and the counter reports rows emitted.
//
// ⚠ The probe wraps `cleared` DELIBERATELY, and the choice is load-bearing:
//
//   - wrapping `company_id` would make the outer `WHERE company_id = ?`
//     read `WHERE probe(company_id) = ?`, which fires once per row SCANNED.
//     Rows scanned is the wrong number: there is no index on company_id, so
//     the scoped read scans the whole table too. Rows DECODED is the number
//     that differs.
//   - wrapping a key column (`site_id`, `node_epoch`, `seq`) would put the
//     probe in the ORDER BY sorter as well as the output row.
//   - `cleared` appears in neither the WHERE nor the ORDER BY. It is a pure
//     output column, so the probe fires exactly once per emitted row.
//
// That is an assumption about SQLite's query flattener, so it is NOT
// assumed: CONTROL 1 and CONTROL 2 calibrate the instrument at both ends
// against independently known row counts before the measured call runs, and
// each fails with a message saying the INSTRUMENT is broken rather than
// letting a mis-calibrated probe red the production assertion.
//
// ⚠ The view is installed only AFTER the fixture is complete. A view cannot
// be INSERTed into, so the fold would fail against it; every write in this
// test happens before the shadow goes in, and nothing writes after.
//
// --- the produced red [MEASURED, this pass] ------------------------------
//
// Plant (m4) half 1 on a scratch copy of the tree — `readCompanyAmendments
// For` rewritten to `queryCompanyAmendments(st, companyAmendment
// CompositionSQL)` with a Go-side `if r.CompanyID != companyID { continue }`
// — and this test reds:
//
//	--- FAIL: TestW5dCompanyByIDDecodesOnlyItsOwnCompanysRows
//	    w5d_scoped_read_test.go: W5d-4 (m4, half 1 — the M-1 shape):
//	    CompanyByID decoded 12 company_amendment rows, want 3 (this
//	    company's own) ... it read the WHOLE table and filtered in Go
//
// Revert (`cmp` byte-identical) and it is green again. Both directions
// produced; the tree the report was written against was never edited.
package domain

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	msqlite "modernc.org/sqlite"

	"sitecontrol/internal/store"
)

// w5dRowsDecoded counts rows EMITTED by any query that reads through the
// shadow view below. It is a package-level counter because the SQL function
// is registered on the driver, not on a store; only this file's view names
// the probe, so no other test can move it. Readings are taken as DELTAS
// around one call, never as absolutes.
var w5dRowsDecoded atomic.Int64

var w5dProbeOnce sync.Once

// w5dRegisterRowProbe registers the counting scalar function. modernc
// exposes new functions to connections opened AFTER registration, so this
// must run before the store is opened — hence the call at the top of the
// test, not in a fixture helper.
func w5dRegisterRowProbe(t *testing.T) {
	t.Helper()
	w5dProbeOnce.Do(func() {
		msqlite.MustRegisterScalarFunction(w5dRowProbeFn, 1,
			func(_ *msqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
				w5dRowsDecoded.Add(1)
				return args[0], nil
			})
	})
}

const w5dRowProbeFn = "w5d_rows_decoded_probe"

// w5dInstallRowProbeView shadows the company_amendment table with a view of
// the same shape whose `cleared` column runs through the probe. Unqualified
// names resolve to the temp schema first, so production's SQL reaches the
// view without production knowing it exists.
func w5dInstallRowProbeView(t *testing.T, st *store.Store) {
	t.Helper()
	err := st.LocalTx(func(tx *sql.Tx) error {
		// ⟦SchemaVersion 11, 2026-09-06⟧ The shadow view mirrors the table's
		// EXACT column set (w5dDeclaredColumns), so `seen` ([[D-92]]) is
		// projected through — an instrument that omitted it would red every
		// read with "no such column" and judge nothing. ⟦was: "…same_as,
		// probe(cleared) AS cleared"⟧
		_, err := tx.Exec(`CREATE TEMP VIEW company_amendment AS
			SELECT site_id, node_epoch, seq, company_id, name, same_as,
			       ` + w5dRowProbeFn + `(cleared) AS cleared, seen
			FROM main.company_amendment`)
		return err
	})
	if err != nil {
		t.Fatalf("CONTROL FAILED (instrument): could not install the rows-decoded shadow view: %v", err)
	}
}

// w5dMeasureRowsDecoded runs fn and returns how many company_amendment rows
// were decoded while it ran.
func w5dMeasureRowsDecoded(t *testing.T, fn func()) int64 {
	t.Helper()
	before := w5dRowsDecoded.Load()
	fn()
	return w5dRowsDecoded.Load() - before
}

// w5dCountAmendmentRows counts rows in the REAL table (main-qualified, so it
// never goes through the view and never disturbs the counter).
func w5dCountAmendmentRows(t *testing.T, st *store.Store, where string, args ...any) int {
	t.Helper()
	var n int
	err := st.LocalTx(func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT count(*) FROM main.company_amendment `+where, args...).Scan(&n)
	})
	if err != nil {
		t.Fatalf("counting company_amendment rows: %v", err)
	}
	return n
}

// TestW5dCompanyByIDDecodesOnlyItsOwnCompanysRows — W5d-4's scoped-read
// sentence, asserted as ROWS DECODED per lookup.
//
// This is the check (m4)'s first disjunct never had. It is deliberately NOT
// a source scan: the M-1 shape has many spellings and only one signature —
// a lookup whose decode cost is the whole table's.
func TestW5dCompanyByIDDecodesOnlyItsOwnCompanysRows(t *testing.T) {
	w5dRegisterRowProbe(t)

	st := w3NewStore(t)
	target, err := CreateCompany(st, "site-a", 1, "Target Ltd", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	other, err := CreateCompany(st, "site-a", 1, "Other Ltd", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// A third company that is never amended: its lookup must decode ZERO
	// rows, which the whole-table shape cannot do while any amendment
	// exists anywhere.
	unamended, err := CreateCompany(st, "site-a", 1, "Unamended Ltd", 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	const targetAmendments, otherAmendments = 3, 9
	for i := 0; i < targetAmendments; i++ {
		w5dAmendLocal(t, st, "site-a", 1, w3Base+int64(i+1)*w3Hour, CompanyAmendment{
			CompanyID: target.CompanyID,
			Name:      w3Str(fmt.Sprintf("Target Ltd rev %d", i+1)),
		})
	}
	for i := 0; i < otherAmendments; i++ {
		w5dAmendLocal(t, st, "site-a", 1, w3Base+int64(i+20)*w3Hour, CompanyAmendment{
			CompanyID: other.CompanyID,
			Name:      w3Str(fmt.Sprintf("Other Ltd rev %d", i+1)),
		})
	}

	// --- the fixture asserts its own precondition (BUILDER_BRIEF_RULES 6)
	// A rows-decoded assertion defends nothing unless the two arms differ:
	// the target must own SOME rows (or a read that returns nothing scores
	// the same as a scoped one) and the table must hold MORE than the
	// target's (or whole-table and scoped are the same number). ----------
	totalRows := w5dCountAmendmentRows(t, st, "")
	targetRows := w5dCountAmendmentRows(t, st, "WHERE company_id = ?", target.CompanyID)
	otherRows := w5dCountAmendmentRows(t, st, "WHERE company_id = ?", other.CompanyID)
	if targetRows != targetAmendments || otherRows != otherAmendments || totalRows != targetAmendments+otherAmendments {
		t.Fatalf("PRECONDITION: fixture did not land as written — company_amendment holds %d rows (%d target, %d other), want %d (%d, %d). "+
			"The fold, not this assertion, is what changed",
			totalRows, targetRows, otherRows, targetAmendments+otherAmendments, targetAmendments, otherAmendments)
	}
	if totalRows <= targetRows {
		t.Fatalf("PRECONDITION: the fixture poses no case — the table holds %d rows and the target owns %d of them, "+
			"so a whole-table read and a scoped read decode the same number and this test could never fail", totalRows, targetRows)
	}

	w5dInstallRowProbeView(t, st)

	// --- CONTROL 1: the instrument is live, and reads HIGH correctly. ----
	// The whole-table read is the M-1 shape's own query. It must count
	// every row in the table. If it counts zero the shadow view is not in
	// effect; if it counts anything else the probe is not firing once per
	// emitted row, and every number below is meaningless.
	var whole []companyAmendmentRow
	wholeDecoded := w5dMeasureRowsDecoded(t, func() {
		whole, err = readCompanyAmendments(st)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(whole) != totalRows || wholeDecoded != int64(totalRows) {
		t.Fatalf("CONTROL FAILED (instrument, high end): the whole-table read returned %d rows and the probe counted %d, want %d for both. "+
			"Either the temp view is not shadowing company_amendment or the probe is not firing once per emitted row — "+
			"this is the INSTRUMENT, not CompanyByID", len(whole), wholeDecoded, totalRows)
	}

	// --- CONTROL 2: the instrument reads LOW correctly. ------------------
	// The same decode loop, driven by the scoped SQL by hand. If SQLite
	// stopped flattening the view the probe would fire for every scanned
	// row and this would report totalRows — which is what a mis-calibrated
	// instrument would report for CompanyByID too, indistinguishable from
	// a real regression. Proving it HERE is what keeps the measured call
	// below honest.
	var scoped []companyAmendmentRow
	scopedDecoded := w5dMeasureRowsDecoded(t, func() {
		scoped, err = queryCompanyAmendments(st, companyAmendmentCompositionByCompanySQL, other.CompanyID)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped) != otherRows || scopedDecoded != int64(otherRows) {
		t.Fatalf("CONTROL FAILED (instrument, low end): a scoped read of %s returned %d rows and the probe counted %d, want %d for both. "+
			"The probe cannot distinguish a scoped read from a whole-table one, so it cannot judge CompanyByID", other.CompanyID, len(scoped), scopedDecoded, otherRows)
	}

	// --- the real judgement ---------------------------------------------
	var composed *Company
	decoded := w5dMeasureRowsDecoded(t, func() {
		composed, err = CompanyByID(st, target.CompanyID)
	})
	if err != nil {
		t.Fatal(err)
	}
	if decoded != int64(targetRows) {
		msg := ""
		if decoded == int64(totalRows) {
			msg = " — that is EVERY row in the table: it read the WHOLE table and filtered in Go"
		}
		t.Fatalf("W5d-4 (m4, half 1 — the M-1 shape): CompanyByID decoded %d company_amendment rows, want %d (this company's own)%s. "+
			"W5d-4 requires the composed company read be SCOPED — 'CompanyByID reads only its own company's amendment rows' — "+
			"and the cost of one lookup must not grow with the size of the side table. The gate MEASURED that shape at 46.78ms "+
			"against the scoped read's 1.96ms over 10,000 rows (~24x); this assertion is the same claim without a clock",
			decoded, targetRows, msg)
	}

	// The count above is only worth having if the read it counts is the
	// read that ANSWERS. A scoped read that returned nothing would score a
	// perfect 3 on a fixture of 3 — so the composed value is pinned too.
	name, ok := w5dCompanyName(composed)
	if !ok || name != fmt.Sprintf("Target Ltd rev %d", targetAmendments) {
		t.Fatalf("W5d-4: CompanyByID decoded the right NUMBER of rows but composed name=%q (present=%v), want %q — "+
			"the scoped read must still be the read that answers", name, ok, fmt.Sprintf("Target Ltd rev %d", targetAmendments))
	}

	// --- the zero arm ----------------------------------------------------
	// A company with no amendments of its own decodes NO amendment rows,
	// while 12 sit in the table. This is the same proposition stated where
	// the whole-table shape is at its most obvious.
	unamendedDecoded := w5dMeasureRowsDecoded(t, func() {
		_, err = CompanyByID(st, unamended.CompanyID)
	})
	if err != nil {
		t.Fatal(err)
	}
	if unamendedDecoded != 0 {
		t.Fatalf("W5d-4 (m4, half 1 — the M-1 shape): CompanyByID on a company with NO amendments decoded %d company_amendment rows, want 0 "+
			"(%d rows belonging to other companies are in the table). A scoped read has nothing to decode here", unamendedDecoded, totalRows)
	}
}
