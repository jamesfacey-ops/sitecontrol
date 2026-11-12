// w3_sidetable_structural_test.go — W3-13: the structural side-table
// assertion (VF T11 applied to person_amendment): schema introspection
// asserts the exact declared column list, that NO status-shaped column
// exists, and that INSERT-only enforcement is present — an attempted
// UPDATE (and DELETE) fails on a real row.
// design/WINDOW-BUILD-PLAN-v1.md §3 W3, criteria LOCKED 2026-08-27.
//
// package store_test, DELIBERATELY: this file references no unbuilt Go
// symbol, so it COMPILES AND RUNS against the clean tree — its clean-tree
// red is the genuine runtime failure "person_amendment side table absent",
// captured for the ledger. (The domain package's W3 files are
// compile-blocked; placing this one there would bury a producible runtime
// red under a compile error.)
//
// KILLER MUTANT: m19 — the fold-maintained status column, behaviourally
// correct at every read (it passes W3-16/T7) and catchable ONLY
// structurally. The planted-status-column control below proves the
// detector detects before the clean pass counts (§5's CONTROL row: a
// schema assertion is scan-shaped).
//
// SEAM W3-S-COLS (bound HERE, once — the criterion demands "a declared
// exact column set" and no design document declares it in full; schema-10
// §5A.2 declares the base and the LOCKED W3 Changes block widens the
// amendable set, so the union is bound below and any builder dispute goes
// to the gate): site_id, node_epoch, seq (the PK, in that order),
// person_id, name, last_name, first_name, phone, company_id, trade_id,
// role, pad_oriented, position, retired, same_as, cleared.
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

// w3DeclaredColumns is SEAM W3-S-COLS.
//
// ⟦SchemaVersion 11, 2026-09-05⟧ `pad_oriented` is STRUCK ([[D-88]] ERRATA /
// [[D-91]]) and `seen` is ADDED ([[D-92]]: the author's seen-list, an
// immutable reference written at authoring — it is deliberately NOT a
// status-shaped name, and w3StatusShaped below still refuses one). The set
// stays EXACT: an undeclared column is refused, and so is the struck one
// coming back. ⟦was: "…, \"role\", \"pad_oriented\", \"position\", …, \"same_as\",
// \"cleared\","⟧
var w3DeclaredColumns = []string{
	"site_id", "node_epoch", "seq",
	"person_id",
	"name", "last_name", "first_name", "phone", "company_id",
	"trade_id", "role", "position", "retired",
	"same_as", "cleared", "seen",
}

// w3StatusShaped flags a status-shaped column name (§4.3 rule 2: no
// `status`, `superseded`, `effective` — supersession is computed at read,
// never stored).
var w3StatusShaped = regexp.MustCompile(`(?i)^(status|superseded|effective|is_superseded|is_effective|stale|current|winner)$`)

func w3OpenRaw(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func w3TableColumns(t *testing.T, db *sql.DB, table string) []string {
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

// w3StatusColumns returns the status-shaped columns of a table — the m19
// detector, shared by the control and the real assertion.
func w3StatusColumns(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	var bad []string
	for _, c := range w3TableColumns(t, db, table) {
		if w3StatusShaped.MatchString(c) {
			bad = append(bad, c)
		}
	}
	return bad
}

// TestW3PersonAmendmentStructural — W3-13. RED on the clean tree: the
// table does not exist.
func TestW3PersonAmendmentStructural(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "w3-structural.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	path := st.Path
	st.Close()
	db := w3OpenRaw(t, path)

	// --- CONTROL FIRST (§5's CONTROL row; FA-3's mirrored shape): a
	// planted status-shaped column and a trigger-less table must be
	// DETECTED by the same detectors the real assertions use. ------------
	if _, err := db.Exec(`CREATE TABLE person_amendment_ctl (
		site_id TEXT NOT NULL, node_epoch INTEGER NOT NULL, seq INTEGER NOT NULL,
		person_id TEXT NOT NULL, superseded INTEGER,
		PRIMARY KEY (site_id, node_epoch, seq)) STRICT`); err != nil {
		t.Fatal(err)
	}
	if bad := w3StatusColumns(t, db, "person_amendment_ctl"); len(bad) != 1 || bad[0] != "superseded" {
		t.Fatalf("W3-13 CONTROL FAILED: the status-column detector missed the planted `superseded` column (got %v) — a detector that cannot detect its plant proves nothing (m19)", bad)
	}
	if n := w3TriggerCount(t, db, "person_amendment_ctl"); n != 0 {
		t.Fatalf("W3-13 CONTROL: unexpected triggers on the planted table")
	}
	if _, err := db.Exec(`INSERT INTO person_amendment_ctl (site_id, node_epoch, seq, person_id, superseded) VALUES ('site-a',1,1,'p',0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE person_amendment_ctl SET superseded = 1`); err != nil {
		t.Fatalf("W3-13 CONTROL: UPDATE on the trigger-less planted table should SUCCEED (proving the failed-UPDATE assertion below can discriminate): %v", err)
	}

	// --- the real table -------------------------------------------------
	cols := w3TableColumns(t, db, "person_amendment")
	if len(cols) == 0 {
		t.Fatalf("W3-13: the person_amendment side table does not exist — W3's INSERT-only side table keyed (site_id, node_epoch, seq) is unbuilt")
	}
	gotSorted := append([]string{}, cols...)
	wantSorted := append([]string{}, w3DeclaredColumns...)
	sort.Strings(gotSorted)
	sort.Strings(wantSorted)
	if strings.Join(gotSorted, ",") != strings.Join(wantSorted, ",") {
		t.Fatalf("W3-13: person_amendment columns = %v, want EXACTLY the declared set %v (SEAM W3-S-COLS) — any undeclared column is refused, status-shaped or not", cols, w3DeclaredColumns)
	}
	if bad := w3StatusColumns(t, db, "person_amendment"); len(bad) != 0 {
		t.Fatalf("W3-13 (m19): status-shaped column(s) %v on person_amendment — supersession is computed at read, NEVER stored; a fold-maintained status column is behaviourally correct at every read and only this assertion catches it", bad)
	}

	// PK is (site_id, node_epoch, seq) in that order.
	pk := w3PrimaryKey(t, db, "person_amendment")
	if strings.Join(pk, ",") != "site_id,node_epoch,seq" {
		t.Fatalf("W3-13: person_amendment PRIMARY KEY = %v, want (site_id, node_epoch, seq)", pk)
	}

	// INSERT-only BY TRIGGER: triggers present, and an attempted UPDATE
	// and DELETE on a REAL row both fail (an UPDATE touching zero rows
	// fires no trigger — the row makes the assertion capable of failing).
	if n := w3TriggerCount(t, db, "person_amendment"); n == 0 {
		t.Fatalf("W3-13: no triggers on person_amendment — INSERT-only must be enforced BY TRIGGER (VF §4.3 rule 2 / T11), not by convention")
	}
	if _, err := db.Exec(`INSERT INTO person_amendment (site_id, node_epoch, seq, person_id) VALUES ('site-a', 1, 999901, 'w3-structural-probe')`); err != nil {
		t.Fatalf("W3-13: probe INSERT refused (%v) — the side table must accept inserts", err)
	}
	if _, err := db.Exec(`UPDATE person_amendment SET person_id = 'rewritten' WHERE seq = 999901`); err == nil {
		t.Fatalf("W3-13: UPDATE on person_amendment SUCCEEDED — the table is not INSERT-only; supersession state could be rewritten in place")
	}
	if _, err := db.Exec(`DELETE FROM person_amendment WHERE seq = 999901`); err == nil {
		t.Fatalf("W3-13: DELETE on person_amendment SUCCEEDED — the table is not INSERT-only")
	}
}

func w3TriggerCount(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='trigger' AND tbl_name=?`, table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func w3PrimaryKey(t *testing.T, db *sql.DB, table string) []string {
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
