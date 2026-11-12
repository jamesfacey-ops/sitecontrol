// w9b_sidetable_structural_test.go — W9b-3 (T11-ext): VF T11's structural
// introspection extended to W9b's ONE new side table,
// `arrival_ref_retraction`: STRICT, INSERT-only BY TRIGGER, the PK triple, no
// status-shaped column, and the exact declared column list.
//
// ⚠ W9a's `trip_amendment` / `arrival_amendment` are NOT asserted here.
// T11-ext spans all three side tables and its own instruction is that "each
// ledger records its own tables' pass"; W9a's ledger already carries its two
// (`w9a_sidetable_structural_test.go`, whose header refuses to assert this
// table for the same reason, in the other direction). A W9b test reddening on
// a table W9b does not build would be this project's own cross-sub-chunk
// interference failure.
//
// ⚠ THERE IS NO COLUMN SEAM HERE, and that is worth saying because W9a's twin
// of this file needed one. F4 §3.6 DECLARES the list in terms — "columns
// `(site_id, node_epoch, seq, arrival_id, recorded_by)`" — so `w9bDeclaredColumns`
// below is a quotation, not a test-writer's judgement.
//
// package store_test, DELIBERATELY: this file references no unbuilt Go symbol,
// so it COMPILES AND RUNS against the pre-chunk tree — its red is the genuine
// runtime failure "the side table is absent", captured for the ledger.
//
// KILLER MUTANTS: the fold-maintained `status`/`superseded` column (the
// eager-re-stamp variant of stored status — behaviourally correct at every
// read, catchable ONLY structurally, VF §4.3 rule 2's second tooth); an
// UPDATE- or DELETE-able retraction table ([[D-1]]); and the m15 variant that
// "adds a trip field to the payload for the review lane's convenience",
// caught here at the COLUMN it would have to land in.
package store_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// w9bRetractionTable is F4 §3.6's projection name.
const w9bRetractionTable = "arrival_ref_retraction"

// w9bDeclaredColumns is F4 §3.6's declared list, verbatim: the PK triple —
// the envelope's own key — plus the payload's two data fields.
var w9bDeclaredColumns = []string{"site_id", "node_epoch", "seq", "arrival_id", "recorded_by"}

// w9bStatusShaped is the status-shaped-column detector. Deliberately wider
// than the three words VF names, because the mutant it exists to catch is
// "whatever the builder called the column that caches an answer the fold is
// supposed to recompute".
var w9bStatusShaped = regexp.MustCompile(`(?i)(superseded|status|effective|winner|current|active|resolved|latest|retracted_at|applied|is_[a-z_]*current)`)

// w9bForbiddenTripColumns is the structural twin of W9b-2's "no trip field of
// any kind". F4 §3.6: the event "names the arrival whose reference is
// withdrawn and NOTHING else — no replacement trip, ever", and a re-point must
// be "unrepresentable on the wire". A column here is where a re-point would
// have to live, so the exact-list check is backed by a named negative that
// says WHY when it fires.
var w9bForbiddenTripColumns = regexp.MustCompile(`(?i)(trip|replacement|reason|note)`)

func w9bOpenRegistered(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "w9b-structural.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	return st
}

func w9bOpenRaw(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func w9bTableColumns(t *testing.T, db *sql.DB, table string) []string {
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

func w9bTableSQL(t *testing.T, db *sql.DB, table string) string {
	t.Helper()
	var ddl string
	err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&ddl)
	if err == sql.ErrNoRows {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return ddl
}

func w9bTriggerSQL(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT sql FROM sqlite_master WHERE type = 'trigger' AND tbl_name = ?`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func w9bPKColumns(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name, pk FROM pragma_table_info(?) ORDER BY pk`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type col struct {
		name string
		pk   int
	}
	var cs []col
	for rows.Next() {
		var c col
		if err := rows.Scan(&c.name, &c.pk); err != nil {
			t.Fatal(err)
		}
		if c.pk > 0 {
			cs = append(cs, c)
		}
	}
	sort.Slice(cs, func(i, j int) bool { return cs[i].pk < cs[j].pk })
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.name)
	}
	return out
}

// TestW9b3RefRetractionTableIsStrictInsertOnlyAndStatusFree — W9b-3.
func TestW9b3RefRetractionTableIsStrictInsertOnlyAndStatusFree(t *testing.T) {
	st := w9bOpenRegistered(t)
	db := w9bOpenRaw(t, st.Path)

	ddl := w9bTableSQL(t, db, w9bRetractionTable)
	if ddl == "" {
		t.Fatalf("W9b-3: table %q DOES NOT EXIST after domain.Register.\n"+
			"F4 §3.6 requires a STRICT, INSERT-only projection `%s`, PK the envelope's own key, columns %v, dump-participating, "+
			"with a bare-INSERT dangling-tolerant fold. On the pre-chunk tree it does not exist, and THIS is the produced red",
			w9bRetractionTable, w9bRetractionTable, w9bDeclaredColumns)
	}

	// --- STRICT ---------------------------------------------------------
	if !strings.Contains(strings.ToUpper(ddl), "STRICT") {
		t.Fatalf("W9b-3: %s is not declared STRICT.\nDDL: %s", w9bRetractionTable, ddl)
	}

	// --- the exact declared column list (F4 §3.6, quoted) ---------------
	got := w9bTableColumns(t, db, w9bRetractionTable)
	gotSorted, wantSorted := append([]string(nil), got...), append([]string(nil), w9bDeclaredColumns...)
	sort.Strings(gotSorted)
	sort.Strings(wantSorted)
	if strings.Join(gotSorted, ",") != strings.Join(wantSorted, ",") {
		t.Fatalf("W9b-3: %s's column list is\n  got:  %v\n  want: %v\n"+
			"F4 §3.6 DECLARES this list in terms — there is no seam here and no judgement of mine in it",
			w9bRetractionTable, got, w9bDeclaredColumns)
	}

	// --- the PK is the envelope's own key, in order ---------------------
	pk := w9bPKColumns(t, db, w9bRetractionTable)
	if strings.Join(pk, ",") != "site_id,node_epoch,seq" {
		t.Fatalf("W9b-3: %s's PRIMARY KEY is %v, want (site_id, node_epoch, seq) — the envelope's OWN key (F4 §3.6). "+
			"A surrogate key would let one retraction be stored twice, and the fold is a bare INSERT with no lookup to notice",
			w9bRetractionTable, pk)
	}
	if len(got) < 3 || got[0] != "site_id" || got[1] != "node_epoch" || got[2] != "seq" {
		t.Fatalf("W9b-3: %s's first three declared columns are %v, want site_id, node_epoch, seq in that order", w9bRetractionTable, got)
	}

	// --- no status-shaped column, and no trip field of any kind ---------
	for _, c := range got {
		if w9bStatusShaped.MatchString(c) {
			t.Fatalf("W9b-3: %s carries the status-shaped column %q. VF §4.3 rule 2: the answer is RECOMPUTED at read, never stored. "+
				"F4 §3.6 puts the state change in `computeStates`, a pure function of raw tables — a column caching whether a retraction 'took effect' "+
				"is behaviourally correct at every read and catchable ONLY structurally", w9bRetractionTable, c)
		}
		if w9bForbiddenTripColumns.MatchString(c) {
			t.Fatalf("W9b-3 (m15's structural variant): %s carries %q. F4 §3.6: the event names the arrival whose reference is withdrawn and NOTHING else — "+
				"no replacement trip, ever, and no reason field. A trip column here is where the 'for the review lane's convenience' re-point would live, "+
				"and W9b-2's claim that a re-point is UNREPRESENTABLE on the wire is only true while this column does not exist",
				w9bRetractionTable, c)
		}
	}

	// --- INSERT-only, BY TRIGGER, and behaviourally ---------------------
	trigs := w9bTriggerSQL(t, db, w9bRetractionTable)
	var hasUpdate, hasDelete bool
	for _, s := range trigs {
		u := strings.ToUpper(s)
		if strings.Contains(u, "BEFORE UPDATE") || strings.Contains(u, "INSTEAD OF UPDATE") {
			hasUpdate = true
		}
		if strings.Contains(u, "BEFORE DELETE") || strings.Contains(u, "INSTEAD OF DELETE") {
			hasDelete = true
		}
	}
	if !hasUpdate || !hasDelete {
		t.Fatalf("W9b-3: %s has no UPDATE trigger (%v) and/or no DELETE trigger (%v). Triggers found: %v.\n"+
			"F4 §3.6: STRICT, INSERT-only, T11-conformant [DOC — VF §4.3 rule 2]", w9bRetractionTable, hasUpdate, hasDelete, trigs)
	}

	// A trigger that EXISTS and does not abort is a green, so the refusal is
	// produced against a real row (a DELETE from an empty table succeeds and
	// proves nothing).
	probe := fmt.Sprintf("INSERT INTO %s (site_id, node_epoch, seq, arrival_id) VALUES ('site-probe', 1, 1, 'probe-arrival')", w9bRetractionTable)
	if _, err := db.Exec(probe); err != nil {
		t.Fatalf("W9b-3: could not write the probe row into %s: %v.\nQuery: %s\n"+
			"`recorded_by` is `omitempty` on the wire (F4 §3.6), so it must be nullable in the table — a NOT NULL there would refuse a legal retraction",
			w9bRetractionTable, err, probe)
	}
	for _, stmt := range []string{
		fmt.Sprintf("UPDATE %s SET arrival_id = 'tampered'", w9bRetractionTable),
		fmt.Sprintf("DELETE FROM %s", w9bRetractionTable),
	} {
		if _, err := db.Exec(stmt); err == nil {
			t.Fatalf("W9b-3: %q SUCCEEDED on %s. The table must be INSERT-only: [[D-1]] keeps every assertion in the audit chain, and a deletable retraction row "+
				"is a silent re-binding of a trip nobody re-recorded", stmt, w9bRetractionTable)
		}
	}
}

// TestW9b3RefRetractionParticipatesInTheDump — F4 §3.6's "dump-participating"
// clause, which the column/trigger checks above cannot see.
//
// It matters for a reason bigger than tidiness: the two-post determinism
// comparison is `sitectl -dump` plus `cmd/dumpcompare` ([[D-56]] took the HTTP
// dump route away), so a projection missing from `DumpProjections` is a
// projection the posts never compare — and W9b-1's own order-independence half
// is asserted on exactly those bytes.
func TestW9b3RefRetractionParticipatesInTheDump(t *testing.T) {
	st := w9bOpenRegistered(t)
	if _, err := st.DumpProjection(w9bRetractionTable); err != nil {
		t.Fatalf("W9b-3: `DumpProjection(%q)` failed: %v.\n"+
			"F4 §3.6 requires the projection to be dump-participating. A table outside the dump is outside the two-post determinism comparison "+
			"([[D-56]]: `sitectl -dump` + `cmd/dumpcompare` is the only comparison left), and W9b-1's permutation half asserts on those very bytes",
			w9bRetractionTable, err)
	}
}
