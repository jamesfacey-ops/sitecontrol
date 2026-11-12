// w4_sidetable_structural_test.go — W4-1's STRUCTURAL half (VF T11 applied
// to occurred_at_correction): schema introspection asserts the exact
// declared column set, that NO status-shaped column exists, and that
// INSERT-only enforcement is present — an attempted UPDATE (and DELETE)
// fails on a real row. Plus the wire-path half of W4-1's byte-identity:
// the fold populates the side table from applied corrections and NEVER
// touches the target row.
// design/WINDOW-BUILD-PLAN-v1.md "### W4", criteria LOCKED 2026-08-27.
//
// package store_test, DELIBERATELY (W3-13's precedent): this file
// references no unbuilt Go symbol, so it COMPILES AND RUNS against the
// clean tree — its clean-tree red is the genuine runtime failure "the
// occurred_at_correction side table does not exist", captured for the
// ledger. (The domain package's W4 files are compile-blocked; placing
// these assertions there would bury a producible runtime red under a
// compile error.)
//
// KILLER MUTANTS: (m9) — the eager-re-stamp variant: a fold-maintained
// status-shaped column on occurred_at_correction, updated on every apply;
// behaviourally invisible (correct at every read), only THIS introspection
// catches it. The planted-status-column control below (the lock's CONTROL
// row 5) proves the detector detects before the clean pass counts. Also
// (m1)'s trigger half (apply-as-edit / any in-place rewrite).
//
// SEAM W4-ST-COLS (bound HERE, once — Changes delta 1 declares the table,
// key and column MEANINGS but spells only the target-key and recorded_by
// columns; the corrected-instant column spelling follows the pinned
// payload literal `corrected_occurred_at`, addendum C-1): site_id,
// node_epoch, seq (the PK — the correction event's OWN key, in that
// order), target_site_id, target_node_epoch, target_seq,
// corrected_occurred_at, recorded_by.
package store_test

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// w4DeclaredColumns is SEAM W4-ST-COLS.
//
// ⟦SchemaVersion 11, 2026-09-06⟧ `seen` is ADDED ([[D-92]], step 3 of the
// increment — the SEVENTH payload to carry the causal reference, after the
// six amendment carriers): the corrections in the original's chain this
// author had already seen standing — an immutable reference written at
// authoring, not a status; the status-shaped detector below still refuses
// one. The set stays EXACT. ⟦was: "…\"corrected_occurred_at\",
// \"recorded_by\","⟧
var w4DeclaredColumns = []string{
	"site_id", "node_epoch", "seq",
	"target_site_id", "target_node_epoch", "target_seq",
	"corrected_occurred_at", "recorded_by", "seen",
}

const w4SideTable = "occurred_at_correction" // DOC — VF §7.5 obligation 1

// w4WireEnv hand-builds one wire envelope on the site-b stream.
func w4WireEnv(t *testing.T, seq, authoredMS int64, payload string) store.Envelope {
	t.Helper()
	env := store.Envelope{
		SiteID: "site-b", NodeEpoch: 1, Seq: seq,
		SchemaVersion: store.SchemaVersion, OriginAuth: "site-b",
		OccurredAt: authoredMS, TimeSource: "default",
		Payload: json.RawMessage(payload),
	}
	env.AuthoredAt.WallMS = authoredMS
	env.AuthoredAt.MaxErrorUS = 42
	env.AuthoredAt.State = store.ClockSynced
	return env
}

// w4RawRow reads one event_log row as a single quoted concatenation of
// EVERY column (column list taken from pragma_table_info, so a schema
// widening cannot silently shrink the comparison).
func w4RawRow(t *testing.T, db *sql.DB, siteID string, epoch, seq int64) string {
	t.Helper()
	cols := w3TableColumns(t, db, "event_log")
	if len(cols) == 0 {
		t.Fatal("event_log has no columns?")
	}
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = "quote(" + c + ")"
	}
	q := "SELECT " + strings.Join(parts, " || '\x1f' || ") + " FROM event_log WHERE site_id=? AND node_epoch=? AND seq=?"
	var row string
	if err := db.QueryRow(q, siteID, epoch, seq).Scan(&row); err != nil {
		t.Fatalf("raw row (%s,%d,%d): %v", siteID, epoch, seq, err)
	}
	return row
}

// TestW4CorrectionSideTableStructural — W4-1's structural half (T11's
// correction-table half; mutant m9). RED on the clean tree: the table
// does not exist.
func TestW4CorrectionSideTableStructural(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "w4-structural.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	path := st.Path
	st.Close()
	db := w3OpenRaw(t, path)

	// --- CONTROL FIRST (the lock's CONTROL row 5): a planted
	// status-shaped column and a trigger-less table must be DETECTED by
	// the same detectors the real assertions use. -------------------------
	if _, err := db.Exec(`CREATE TABLE occurred_at_correction_ctl (
		site_id TEXT NOT NULL, node_epoch INTEGER NOT NULL, seq INTEGER NOT NULL,
		target_site_id TEXT NOT NULL, target_node_epoch INTEGER NOT NULL, target_seq INTEGER NOT NULL,
		corrected_occurred_at INTEGER NOT NULL, recorded_by TEXT NOT NULL,
		superseded INTEGER,
		PRIMARY KEY (site_id, node_epoch, seq)) STRICT`); err != nil {
		t.Fatal(err)
	}
	if bad := w3StatusColumns(t, db, "occurred_at_correction_ctl"); len(bad) != 1 || bad[0] != "superseded" {
		t.Fatalf("W4-1 CONTROL FAILED: the status-column detector missed the planted `superseded` column (got %v) — a detector that cannot detect its plant proves nothing (m9)", bad)
	}
	if n := w3TriggerCount(t, db, "occurred_at_correction_ctl"); n != 0 {
		t.Fatalf("W4-1 CONTROL: unexpected triggers on the planted table")
	}
	if _, err := db.Exec(`INSERT INTO occurred_at_correction_ctl VALUES ('site-a',1,1,'site-a',1,7,1000,'x',0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE occurred_at_correction_ctl SET superseded = 1`); err != nil {
		t.Fatalf("W4-1 CONTROL: UPDATE on the trigger-less planted table should SUCCEED (proving the failed-UPDATE assertion below can discriminate): %v", err)
	}

	// --- the real table -------------------------------------------------
	cols := w3TableColumns(t, db, w4SideTable)
	if len(cols) == 0 {
		t.Fatalf("W4-1: the %s side table does not exist — the INSERT-only correction side table keyed (site_id, node_epoch, seq) (VF §7.5 obligation 1 / W4 Changes delta 1) is unbuilt", w4SideTable)
	}
	gotSorted := append([]string{}, cols...)
	wantSorted := append([]string{}, w4DeclaredColumns...)
	sort.Strings(gotSorted)
	sort.Strings(wantSorted)
	if strings.Join(gotSorted, ",") != strings.Join(wantSorted, ",") {
		t.Fatalf("W4-1: %s columns = %v, want EXACTLY the declared set %v (SEAM W4-ST-COLS) — any undeclared column is refused, status-shaped or not", w4SideTable, cols, w4DeclaredColumns)
	}
	if bad := w3StatusColumns(t, db, w4SideTable); len(bad) != 0 {
		t.Fatalf("W4-1 (m9): status-shaped column(s) %v on %s — supersession is computed at read, NEVER stored; the eager-re-stamp variant is behaviourally correct at every read and only this assertion catches it", bad, w4SideTable)
	}
	pk := w3PrimaryKey(t, db, w4SideTable)
	if strings.Join(pk, ",") != "site_id,node_epoch,seq" {
		t.Fatalf("W4-1: %s PRIMARY KEY = %v, want (site_id, node_epoch, seq) — the correction event's OWN key", w4SideTable, pk)
	}
	if n := w3TriggerCount(t, db, w4SideTable); n == 0 {
		t.Fatalf("W4-1: no triggers on %s — INSERT-only must be enforced BY TRIGGER (VF §4.3 rule 2 / T11), not by convention", w4SideTable)
	}
	if _, err := db.Exec(`INSERT INTO occurred_at_correction (site_id, node_epoch, seq, target_site_id, target_node_epoch, target_seq, corrected_occurred_at, recorded_by) VALUES ('site-a', 1, 999901, 'site-a', 1, 7, 1000, 'w4-structural-probe')`); err != nil {
		t.Fatalf("W4-1: probe INSERT refused (%v) — the side table must accept inserts", err)
	}
	if _, err := db.Exec(`UPDATE occurred_at_correction SET corrected_occurred_at = 2000 WHERE seq = 999901`); err == nil {
		t.Fatalf("W4-1: UPDATE on %s SUCCEEDED — the table is not INSERT-only; a correction could be rewritten in place", w4SideTable)
	}
	if _, err := db.Exec(`DELETE FROM occurred_at_correction WHERE seq = 999901`); err == nil {
		t.Fatalf("W4-1: DELETE on %s SUCCEEDED — the table is not INSERT-only", w4SideTable)
	}
}

// TestW4FoldPopulatesSideTableAndNeverEditsTarget — the wire-path halves
// of W4-1 (m1) and of the side table's population contract (VF §7.5-1:
// "populated by the fold"): two corrections applied over the wire land as
// side-table rows carrying the pinned columns, and the TARGET event_log
// row — every column, compared raw — is byte-identical before and after.
// RED on the clean tree: no projection populates the (absent) table.
func TestW4FoldPopulatesSideTableAndNeverEditsTarget(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "w4-fold.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	const base = int64(1_800_000_000_000)
	const originalMS = base - 10*3_600_000
	target := w4WireEnv(t, 1, base-2*3_600_000, `{"type":"person_created","person_id":"person-w4-fold","name":"Fold Target"}`)
	target.OccurredAt = originalMS
	target.TimeSource = "entered"
	if applied, err := st.Apply(target); err != nil || !applied {
		t.Fatalf("apply target: applied=%v err=%v", applied, err)
	}

	db := w3OpenRaw(t, st.Path)
	before := w4RawRow(t, db, "site-b", 1, 1)

	mk := func(seq, correctedMS int64, by string) store.Envelope {
		return w4WireEnv(t, seq, base+seq*3_600_000, `{"type":"occurred_at_corrected","target_site_id":"site-b","target_node_epoch":1,"target_seq":1,"corrected_occurred_at":`+
			jsonInt(correctedMS)+`,"recorded_by":"`+by+`"}`)
	}
	c1 := mk(2, base-9*3_600_000, "Officer One")
	c2 := mk(3, base-8*3_600_000, "Officer Two")
	for _, env := range []store.Envelope{c1, c2} {
		if applied, err := st.Apply(env); err != nil || !applied {
			t.Fatalf("apply correction %v: applied=%v err=%v — folds never refuse", env.Key(), applied, err)
		}
	}

	// The fold populated the side table — one row per correction, keyed by
	// the correction event's OWN key, carrying the target key, instant and
	// officer.
	rows, err := db.Query(`SELECT seq, target_site_id, target_node_epoch, target_seq, corrected_occurred_at, recorded_by FROM occurred_at_correction ORDER BY site_id, node_epoch, seq`)
	if err != nil {
		t.Fatalf("W4-1/VF §7.5-1: reading %s: %v — the fold must populate the INSERT-only side table from applied corrections (on the clean tree this is the runtime red: the table is unbuilt)", w4SideTable, err)
	}
	defer rows.Close()
	type sideRow struct {
		Seq, TEpoch, TSeq, MS int64
		TSite, By             string
	}
	var got []sideRow
	for rows.Next() {
		var r sideRow
		if err := rows.Scan(&r.Seq, &r.TSite, &r.TEpoch, &r.TSeq, &r.MS, &r.By); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	want := []sideRow{
		{Seq: 2, TSite: "site-b", TEpoch: 1, TSeq: 1, MS: base - 9*3_600_000, By: "Officer One"},
		{Seq: 3, TSite: "site-b", TEpoch: 1, TSeq: 1, MS: base - 8*3_600_000, By: "Officer Two"},
	}
	if len(got) != len(want) {
		t.Fatalf("W4-1: %d side-table row(s) after 2 applied corrections, want 2: %+v", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("W4-1: side-table row %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	// The target row is byte-identical — every event_log column, raw.
	after := w4RawRow(t, db, "site-b", 1, 1)
	if before != after {
		t.Fatalf("W4-1 (m1, apply-as-edit): the target event_log row changed under wire-applied corrections —\nbefore: %s\nafter:  %s", before, after)
	}
}

// jsonInt renders an int64 for a hand-built JSON literal.
func jsonInt(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}
