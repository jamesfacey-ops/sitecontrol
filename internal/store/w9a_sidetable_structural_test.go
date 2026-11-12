// w9a_sidetable_structural_test.go — W9a-12 (T11-ext): VF T11's structural
// introspection extended to W9a's TWO new side tables, `trip_amendment` and
// `arrival_amendment`: no status-shaped column, INSERT-only enforcement BY
// TRIGGER, STRICT, the PK triple, and the exact declared column list.
//
// ⚠ The criterion says the sweep spans W9b's `arrival_ref_retraction` too and
// that "each ledger records its own tables' pass". W9b's table is NOT
// asserted here — it belongs to W9b's ledger, and a W9a test that reddened
// on a table W9a does not build would be this project's own
// cross-sub-chunk-interference failure.
//
// package store_test, DELIBERATELY: this file references no unbuilt Go
// symbol, so it COMPILES AND RUNS against the pre-chunk tree — its red is the
// genuine runtime failure "the side table is absent", captured for the
// ledger.
//
// KILLER MUTANTS: the fold-maintained `superseded`/`status` column (the
// eager-re-stamp variant of stored status — behaviourally correct at every
// read, catchable ONLY structurally, VF §4.3 rule 2's second tooth), and an
// UPDATE- or DELETE-able amendment table (D-1: nothing is ever edited or
// deleted; the audit chain must still render what was asserted).
//
// SEAM W9A-S-COLS, bound HERE, once, because the criterion demands "the
// exact declared column list" and NO design document declares it. Derived
// from F4 §3.1 (the payload) and §3.2 ("PK (site_id, node_epoch, seq) — the
// envelope's own key — plus the payload columns"):
//
//	trip_amendment:     the PK triple, trip_id, driver_person_id,
//	                    vehicle_entered, destination_kind, destination_value,
//	                    deadline_minutes, cargo_note, phase_override_reason,
//	                    occupants_add, occupants_remove, cleared
//	arrival_amendment:  the PK triple, arrival_id, vehicle_entered, origin_site
//
// ⚠ TWO OF THOSE SPELLINGS ARE THIS SEAM'S JUDGEMENT AND NOT A DOCUMENT'S:
// `destination_kind` and `destination_value`. F4 §3.1 fixes the PAYLOAD's
// `destination:{kind, value, deadline_minutes}` and §3.2 says only "plus the
// payload columns"; the compound fact's COLUMN spelling is nowhere declared.
// A builder who spells them otherwise should take it to the GATE rather than
// edit this file — that is exactly what `w5d_sidetable_structural_test.go`'s
// SEAM W5D-S-COLS did, and the failure message below says so.
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

// ⟦SchemaVersion 11, 2026-09-06⟧ `seen` is ADDED to BOTH tables ([[D-92]],
// step 3 of the increment — the causal reference cloned onto trip + arrival
// fourth and fifth, after company, trade and vehicle): the author's
// seen-list, an immutable reference written at authoring, not a status —
// w9aStatusShaped below still refuses one, and w9aForbiddenColumns is
// untouched. The sets stay EXACT. ⟦was: trip_amendment "…\"cleared\","⟧
// ⟦was: arrival_amendment "…\"vehicle_entered\", \"origin_site\","⟧
var w9aDeclaredColumns = map[string][]string{
	"trip_amendment": {
		"site_id", "node_epoch", "seq",
		"trip_id",
		"driver_person_id", "vehicle_entered",
		"destination_kind", "destination_value", "deadline_minutes",
		"cargo_note", // ⟦was: "cargo_note", "phase_override_reason", — struck with weather phases, James 2026-10-04⟧
		"occupants_add", "occupants_remove",
		"cleared", "seen",
	},
	"arrival_amendment": {
		"site_id", "node_epoch", "seq",
		"arrival_id",
		"vehicle_entered", "origin_site", "seen",
	},
}

// w9aStatusShaped is the status-shaped-column detector. It is deliberately
// WIDER than the one word "superseded": VF §4.3 rule 2 forbids STORED
// STATUS, and a column called `winner`, `effective`, `current` or `state`
// stores the same thing under a different name. The filter must reach every
// spelling of the subject, not the one the last mutant happened to use.
var w9aStatusShaped = regexp.MustCompile(`(?i)(superseded|status|effective|winner|current|active|resolved|latest|is_[a-z_]*current|state)`)

// w9aForbiddenColumns are fields F4 asserts ABSENT from these payloads —
// carrying one forward would freeze a field the design says does not exist.
// F4 §3.1: "No `recorded_by` on either payload"; "No reason/note field".
var w9aForbiddenColumns = regexp.MustCompile(`(?i)^(recorded_by|reason|note|retired|trip_ref|visit_id|person_snapshot_name)$`)

func w9aOpenRegistered(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "w9a-structural.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	return st
}

func w9aOpenRaw(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func w9aTableColumns(t *testing.T, db *sql.DB, table string) []string {
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

func w9aTableSQL(t *testing.T, db *sql.DB, table string) string {
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

func w9aTriggerSQL(t *testing.T, db *sql.DB, table string) []string {
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

func w9aPKColumns(t *testing.T, db *sql.DB, table string) []string {
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

// TestW9a12AmendmentSideTablesAreStrictInsertOnlyAndStatusFree — W9a-12.
func TestW9a12AmendmentSideTablesAreStrictInsertOnlyAndStatusFree(t *testing.T) {
	st := w9aOpenRegistered(t)
	db := w9aOpenRaw(t, st.Path)

	for _, table := range []string{"trip_amendment", "arrival_amendment"} {
		t.Run(table, func(t *testing.T) {
			ddl := w9aTableSQL(t, db, table)
			if ddl == "" {
				t.Fatalf("W9a-12: table %q DOES NOT EXIST after domain.Register.\n"+
					"F4 §3.2 requires STRICT, INSERT-only side tables `trip_amendment` and `arrival_amendment`, "+
					"PK (site_id, node_epoch, seq) — the envelope's own key — with dump-participating projections. "+
					"On the pre-chunk tree neither exists, and THIS is the produced red", table)
			}

			// --- STRICT (schema-10's discipline for every new table) ----
			if !strings.Contains(strings.ToUpper(ddl), "STRICT") {
				t.Fatalf("W9a-12: %s is not declared STRICT.\nDDL: %s", table, ddl)
			}

			// --- the exact declared column list (SEAM W9A-S-COLS) -------
			got := w9aTableColumns(t, db, table)
			want := w9aDeclaredColumns[table]
			gotSorted, wantSorted := append([]string(nil), got...), append([]string(nil), want...)
			sort.Strings(gotSorted)
			sort.Strings(wantSorted)
			if strings.Join(gotSorted, ",") != strings.Join(wantSorted, ",") {
				t.Fatalf("W9a-12: %s's column list is\n  got:  %v\n  want: %v  (SEAM W9A-S-COLS)\n"+
					"The seam is bound in this file's header, derived from F4 §3.1's payload and §3.2's rule. "+
					"⚠ `destination_kind`/`destination_value` are the SEAM's judgement and no document's — a builder who spells the "+
					"compound destination fact's columns otherwise takes it to the GATE, not to this file",
					table, got, want)
			}

			// --- the PK is the envelope's own key, in order -------------
			pk := w9aPKColumns(t, db, table)
			if strings.Join(pk, ",") != "site_id,node_epoch,seq" {
				t.Fatalf("W9a-12: %s's PRIMARY KEY is %v, want (site_id, node_epoch, seq) — the envelope's OWN key (F4 §3.2). "+
					"A surrogate key would let one event's assertion be stored twice", table, pk)
			}
			// The declared column ORDER puts the key first, as every other
			// side table in this tree does.
			if len(got) < 3 || got[0] != "site_id" || got[1] != "node_epoch" || got[2] != "seq" {
				t.Fatalf("W9a-12: %s's first three declared columns are %v, want site_id, node_epoch, seq in that order", table, got[:w9aMin(3, len(got))])
			}

			// --- no status-shaped column --------------------------------
			for _, c := range got {
				if w9aStatusShaped.MatchString(c) {
					t.Fatalf("W9a-12: %s carries the status-shaped column %q. VF §4.3 rule 2: supersession is RECOMPUTED at read, never stored. "+
						"A fold-maintained status column is behaviourally correct at every read and is catchable ONLY structurally — which is why this check exists", table, c)
				}
				if w9aForbiddenColumns.MatchString(c) {
					t.Fatalf("W9a-12: %s carries %q, a field F4 §3.1 asserts ABSENT (\"No `recorded_by` on either payload\"; \"No reason/note field\"). "+
						"Attribution is the side table's `site_id`, structural and unforgeable", table, c)
				}
			}

			// --- INSERT-only, BY TRIGGER, and behaviourally --------------
			trigs := w9aTriggerSQL(t, db, table)
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
				t.Fatalf("W9a-12: %s has no UPDATE trigger (%v) and/or no DELETE trigger (%v). Triggers found: %v.\n"+
					"F4 §3.2: STRICT, INSERT-only (UPDATE/DELETE abort triggers, T11-conformant [DOC — VF §4.3 rule 2])", table, hasUpdate, hasDelete, trigs)
			}

			// The structural half above is not enough on its own: a trigger
			// that exists and does not abort is a green. So the refusal is
			// PRODUCED, with a real row.
			w9aInsertProbeRow(t, db, table)
			for _, stmt := range []string{
				fmt.Sprintf("UPDATE %s SET site_id = 'tampered'", table),
				fmt.Sprintf("DELETE FROM %s", table),
			} {
				if _, err := db.Exec(stmt); err == nil {
					t.Fatalf("W9a-12: %q SUCCEEDED on %s. The table must be INSERT-only: [[D-1]] keeps every assertion in the audit chain, "+
						"and an editable amendment row is the editing fold (m1b) wearing SQL", stmt, table)
				}
			}
		})
	}
}

// w9aInsertProbeRow writes the minimum legal row so the UPDATE/DELETE
// refusals are produced against a real row rather than an empty table (a
// DELETE from an empty table succeeds and proves nothing).
func w9aInsertProbeRow(t *testing.T, db *sql.DB, table string) {
	t.Helper()
	target := "trip_id"
	if table == "arrival_amendment" {
		target = "arrival_id"
	}
	q := fmt.Sprintf("INSERT INTO %s (site_id, node_epoch, seq, %s) VALUES ('site-probe', 1, 1, 'probe-target')", table, target)
	if _, err := db.Exec(q); err != nil {
		t.Fatalf("W9a-12: could not write the probe row into %s: %v.\nQuery: %s\n"+
			"Every other column must be nullable — nil means NOT AMENDED (F4 §3.1), so a NOT NULL payload column would refuse a legal amendment", table, err, q)
	}
}

// w9aMin is spelled with the file prefix rather than shadowing the `min`
// builtin for the whole store_test package.
func w9aMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
