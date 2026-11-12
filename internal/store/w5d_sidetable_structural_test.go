// w5d_sidetable_structural_test.go — W5d-1 (W3-13's own instance against
// `company_amendment`: the exact declared column list, NO status-shaped
// column, STRICT, the PK triple, and INSERT-only enforcement BY TRIGGER)
// and W5d-2's schema half (`retired` is absent from the side table's
// columns — D-41.1: a company can be corrected but never retired).
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, W5d LOCKED 2026-08-31.
//
// package store_test, DELIBERATELY: this file references no unbuilt Go
// symbol, so it COMPILES AND RUNS against the clean tree — its clean-tree
// red is the genuine runtime failure "company_amendment side table absent",
// captured for the ledger. (The domain package's W5d files are
// compile-blocked; placing this one there would bury a producible runtime
// red under a compile error.)
//
// KILLER MUTANTS: company-m19 (the fold-maintained `superseded` column —
// behaviourally correct at every read, it passes W5d-21/T7, and it is
// catchable ONLY structurally), (m2) (`Retired` resurrected on company as a
// column). Both plants are produced BEFORE the clean pass counts.
//
// SEAM W5D-S-COLS (bound HERE, once — the criterion demands "the exact
// declared column list" and no design document declares it; the person-side
// shape to reproduce is person_amend.go:132–166, and DR-2 / W5d-22(a) fix
// the amendable set at {name, same_as}, so the union is: the PK triple, the
// target, one column per amendable fact, and `cleared`): site_id,
// node_epoch, seq (the PK, in that order), company_id, name, same_as,
// cleared. Any builder dispute goes to the gate.
package store_test

import (
	"database/sql"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// w5dDeclaredColumns is SEAM W5D-S-COLS.
//
// ⟦SchemaVersion 11, 2026-09-06⟧ `seen` is ADDED ([[D-92]], step 3 of the
// increment — the causal reference cloned onto company first, in the order
// the merge field was cloned): the author's seen-list, an immutable
// reference written at authoring — deliberately NOT a status-shaped name,
// and the status-shaped detector below still refuses one. The set stays
// EXACT. ⟦was: "…\"name\", \"same_as\", \"cleared\","⟧
var w5dDeclaredColumns = []string{
	"site_id", "node_epoch", "seq",
	"company_id",
	"name", "same_as",
	"cleared", "seen",
}

// w5dForbiddenColumns are the person/trip-side columns that must have NO
// company limb — `retired` above all (D-41.1, W5d-2).
var w5dForbiddenColumns = regexp.MustCompile(`(?i)^(retired|trade_id|phone|role|pad_oriented|position|last_name|first_name|workgroup_id|person_id)$`)

func w5dOpenRaw(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func w5dTableColumns(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		cols = append(cols, c)
	}
	return cols
}

// w5dStatusColumns is the company-m19 detector, shared by the control and
// the real assertion (the status-shaped register is W3-13's, cloned).
func w5dStatusColumns(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	shaped := regexp.MustCompile(`(?i)^(status|superseded|effective|is_superseded|is_effective|stale|current|winner)$`)
	var bad []string
	for _, c := range w5dTableColumns(t, db, table) {
		if shaped.MatchString(c) {
			bad = append(bad, c)
		}
	}
	return bad
}

// w5dForbiddenColumnsFound is the (m2) detector: any column with no company
// limb, `retired` above all.
func w5dForbiddenColumnsFound(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	var bad []string
	for _, c := range w5dTableColumns(t, db, table) {
		if w5dForbiddenColumns.MatchString(c) {
			bad = append(bad, c)
		}
	}
	return bad
}

func w5dTriggerCount(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='trigger' AND tbl_name=?`, table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func w5dPrimaryKey(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?) WHERE pk > 0 ORDER BY pk`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var pk []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		pk = append(pk, c)
	}
	return pk
}

func w5dTableDDL(t *testing.T, db *sql.DB, table string) string {
	t.Helper()
	var ddl sql.NullString
	err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&ddl)
	if err == sql.ErrNoRows {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return ddl.String
}

// TestW5dCompanyAmendmentStructural — W5d-1 and W5d-2's schema half. RED on
// the clean tree: the table does not exist (company_amendment is ZERO hits
// tree-wide at 88dd6a6 [MEASURED]).
func TestW5dCompanyAmendmentStructural(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "w5d-structural.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	path := st.Path
	st.Close()
	db := w5dOpenRaw(t, path)

	// --- CONTROLS FIRST (§5's CONTROL row; a schema assertion is
	// scan-shaped): a planted status-shaped column, a planted `retired`
	// column, and a trigger-less table must all be DETECTED by the same
	// detectors the real assertions use. --------------------------------
	if _, err := db.Exec(`CREATE TABLE company_amendment_ctl (
		site_id TEXT NOT NULL, node_epoch INTEGER NOT NULL, seq INTEGER NOT NULL,
		company_id TEXT NOT NULL, superseded INTEGER, retired TEXT,
		PRIMARY KEY (site_id, node_epoch, seq)) STRICT`); err != nil {
		t.Fatal(err)
	}
	if bad := w5dStatusColumns(t, db, "company_amendment_ctl"); len(bad) != 1 || bad[0] != "superseded" {
		t.Fatalf("W5d-1 CONTROL FAILED (company-m19): the status-column detector missed the planted `superseded` column (got %v) — a detector that cannot detect its plant proves nothing", bad)
	}
	if bad := w5dForbiddenColumnsFound(t, db, "company_amendment_ctl"); len(bad) != 1 || bad[0] != "retired" {
		t.Fatalf("W5d-2 CONTROL FAILED (m2): the forbidden-column detector missed the planted `retired` column (got %v) — D-41.1 rules company retirement OUT, and the schema assertion is one of the two places that is enforced", bad)
	}
	if n := w5dTriggerCount(t, db, "company_amendment_ctl"); n != 0 {
		t.Fatalf("W5d-1 CONTROL: unexpected triggers on the planted table")
	}
	if _, err := db.Exec(`INSERT INTO company_amendment_ctl (site_id, node_epoch, seq, company_id, superseded, retired) VALUES ('site-a',1,1,'c',0,'no')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE company_amendment_ctl SET superseded = 1`); err != nil {
		t.Fatalf("W5d-1 CONTROL: UPDATE on the trigger-less planted table should SUCCEED (proving the failed-UPDATE assertion below can discriminate): %v", err)
	}

	// --- the real table -------------------------------------------------
	cols := w5dTableColumns(t, db, "company_amendment")
	if len(cols) == 0 {
		t.Fatalf("W5d-1: the company_amendment side table does not exist — W5d's INSERT-only side table keyed (site_id, node_epoch, seq) is unbuilt")
	}
	gotSorted := append([]string{}, cols...)
	wantSorted := append([]string{}, w5dDeclaredColumns...)
	sort.Strings(gotSorted)
	sort.Strings(wantSorted)
	if strings.Join(gotSorted, ",") != strings.Join(wantSorted, ",") {
		t.Fatalf("W5d-1: company_amendment columns = %v, want EXACTLY the declared set %v (SEAM W5D-S-COLS) — any UNDECLARED column is refused, status-shaped or not", cols, w5dDeclaredColumns)
	}
	if bad := w5dStatusColumns(t, db, "company_amendment"); len(bad) != 0 {
		t.Fatalf("W5d-1 (company-m19): status-shaped column(s) %v on company_amendment — supersession is computed at READ, NEVER stored; a fold-maintained status column is behaviourally correct at every read and only this assertion catches it", bad)
	}
	if bad := w5dForbiddenColumnsFound(t, db, "company_amendment"); len(bad) != 0 {
		t.Fatalf("W5d-2 (m2): column(s) %v on company_amendment have no company limb — `retired` in particular is ruled out by D-41.1 (\"A company can be corrected but never retired\"), and the same answer holds for trade_id/phone/role/pad_oriented/position/name-parts", bad)
	}

	// STRICT is part of the person-side shape being reproduced.
	ddl := w5dTableDDL(t, db, "company_amendment")
	if !strings.Contains(strings.ToUpper(ddl), "STRICT") {
		t.Fatalf("W5d-1: company_amendment is not declared STRICT — the person-side shape to reproduce is person_amend.go:132–166 (PK triple, STRICT, two triggers). DDL: %s", ddl)
	}

	// PK is (site_id, node_epoch, seq) in that order — the ordering spec's
	// own subject.
	pk := w5dPrimaryKey(t, db, "company_amendment")
	if strings.Join(pk, ",") != "site_id,node_epoch,seq" {
		t.Fatalf("W5d-1: company_amendment PRIMARY KEY = %v, want (site_id, node_epoch, seq)", pk)
	}

	// INSERT-only BY TRIGGER: triggers present, and an attempted UPDATE and
	// DELETE on a REAL row both fail (an UPDATE touching zero rows fires no
	// trigger — the probe row is what makes the assertion capable of
	// failing).
	if n := w5dTriggerCount(t, db, "company_amendment"); n == 0 {
		t.Fatalf("W5d-1: no triggers on company_amendment — INSERT-only must be enforced BY TRIGGER (VF §4.3 rule 2 / T11), not by convention")
	}
	if _, err := db.Exec(`INSERT INTO company_amendment (site_id, node_epoch, seq, company_id) VALUES ('site-a', 1, 999901, 'w5d-structural-probe')`); err != nil {
		t.Fatalf("W5d-1: probe INSERT refused (%v) — the side table must accept inserts", err)
	}
	if _, err := db.Exec(`UPDATE company_amendment SET company_id = 'rewritten' WHERE seq = 999901`); err == nil {
		t.Fatalf("W5d-1: UPDATE on company_amendment SUCCEEDED — the table is not INSERT-only; supersession state could be rewritten in place")
	}
	if _, err := db.Exec(`DELETE FROM company_amendment WHERE seq = 999901`); err == nil {
		t.Fatalf("W5d-1: DELETE on company_amendment SUCCEEDED — the table is not INSERT-only")
	}
}

// TestW5dNoRetiredOnTheCompanyProjection — W5d-2's second schema
// limb: `retired` is absent from the `company` projection as well as from
// the side table. D-41.1 is a ruling about the ENTITY, not about one table,
// and the picker-only-grows cost was accepted with it.
func TestW5dNoRetiredOnTheCompanyProjection(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "w5d-company-projection.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	path := st.Path
	st.Close()
	db := w5dOpenRaw(t, path)

	cols := w5dTableColumns(t, db, "company")
	if len(cols) == 0 {
		t.Fatalf("W5d-2: the company projection does not exist")
	}
	// Liveness (rule 6): the pre-chunk shape must be the one this assertion
	// was written against, or "no retired column" is true of the wrong
	// table.
	want := map[string]bool{"company_id": true, "site_id": true, "name": true, "recorded_by": true}
	for _, c := range cols {
		if !want[c] {
			t.Fatalf("W5d-2: the company projection gained an undeclared column %q (columns: %v) — the pre-chunk shape is the four columns at refdata.go:106–113 and W5d adds a SIDE TABLE, never a column here", c, cols)
		}
	}
	for _, c := range cols {
		if strings.EqualFold(c, "retired") {
			t.Fatalf("W5d-2 (m2): the company projection carries a `retired` column — D-41.1: no company retirement, ever; it is never removed from the picker")
		}
	}
}
