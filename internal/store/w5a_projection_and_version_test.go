// ⚠️ READ FIRST — W5-X5's BUMP REQUIREMENT IS STRUCK (2026-09-01).
// reports/W5-SCHEMAVERSION-ADJUDICATION.md rules that the schema-10 window
// carries exactly ONE SchemaVersion increment — W1's 9 → 10 — and that NO W5
// sub-chunk bumps it. Every mention of "the shape change bumps
// store.SchemaVersion" below is SUPERSEDED text, kept so the record stays
// diffable; the removal record further down this file states what went and why.
// The criteria this file still carries live — W5-X4 and its controls — are
// UNAFFECTED.

// w5a_projection_and_version_test.go — W5a's STORE-LEVEL halves:
//
//   - W5a-2 (schema half) in W5-X4's two-part form: the `person` projection
//     gains EXACTLY last_name/first_name/position as nullable TEXT with no
//     default, STRICT preserved, and NO column outside the permitted union.
//   - W5-X5: the projection SHAPE CHANGE bumps store.SchemaVersion, proved by
//     an open-an-old-store test with its pre-bump CONTROL.
//
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, criteria LOCKED 2026-08-31.
//
// package store_test DELIBERATELY (w3_sidetable_structural_test.go's own
// reason, restated because it is load-bearing): these assertions need
// domain.Register to create the projection, and `package store` cannot import
// internal/domain — domain imports store. This file therefore references NO
// unbuilt Go symbol, so it COMPILES AND RUNS against `88dd6a6` and its
// clean-tree failures are genuine PRODUCED RUNTIME REDS, not compile errors.
//
// EXPECTED RED at 88dd6a6 (both tests, runtime):
//   - TestW5aPersonProjectionColumnsW5X4: "person is missing declared
//     column(s) [last_name first_name position]" — the projection is still the
//     pre-chunk six (refdata.go:197–204).
//   - ⟦was: "TestW5aSchemaVersionBumpedForThePersonShapeChange"⟧: "Open SUCCEEDED over
//     a store stamped '10'" — store.SchemaVersion is still 10
//     (store.go:102), so the shape change has not been declared.
//
// KILLER MUTANTS:
//
//	(m5a-2)  a projection column dropped, renamed, or declared NOT NULL; the
//	         fold silently not writing a part      -> part (a) below
//	(m5-X4)  a column outside the permitted union added and not caught
//	                                               -> part (b) below
//	(m5-X5)  a column added with no SchemaVersion bump: green at Open, red at
//	         the first enrolment                   -> the version test below
//
// CONTROLS (W5-X6 (a): W5a-2 introspection, W5-X4 both parts, W5-X5's
// pre-bump control). Each detector is shown detecting a PLANTED violation on
// a scratch table before the real table's clean pass counts — one plant per
// tooth, so a relaxed detector cannot pass by having only one tooth left.
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

// --- SEAMS bound here, once; disputes go to the gate --------------------
//
// SEAM W5a-S-COLS: the three new `person` column spellings are
// last_name, first_name, position. These are NOT a free choice — W5-X4 part
// (b) names them verbatim ("plus `last_name, first_name, position` (W5a)"),
// and they are the spellings W3 already landed on person_amendment
// (refdata.go:143/:144/:150). Recorded as a seam only because this file
// asserts them as literals.
//
// SEAM W5a-S-PREBUMP — VOID (W5-SCHEMAVERSION-ADJUDICATION.md, 2026-09-01):
// W5a has no predecessor of its own; it rides W1's 9 → 10. ⟦was:⟧
// ⟦the old-store fixture stamps the LITERAL predecessor⟧
// '10' (W5-X5(ii) in terms: "never SchemaVersion-1"). 10 is store.go:102's
// measured pre-W5 value at 88dd6a6.

// w5aPersonPreChunkColumns is the MEASURED pre-chunk `person` column set
// (refdata.go:197–204 at 88dd6a6) — W5-X4 part (b)'s base.
var w5aPersonPreChunkColumns = []string{
	// ⟦LOCK — §82, 2026-09-03⟧ ⭐ THE PRE-CHUNK BASE IS RE-POINTED, NOT DISCHARGED.
	// ⟦was: "\t\"person_id\", \"site_id\", \"name\", \"company_id\", \"workgroup_id\", \"recorded_by\","⟧
	//
	// ⚠ THIS TEST'S OWN ASSERTION WAS NEVER BROKEN. It failed at the Rule 6
	// arrangement guard above/below — before its subject ran — because W8
	// renamed `person.workgroup_id` to `person.trade_id` (DR-W8-2 / P-3, and
	// the rename is sanctioned). [MEASURED, S82-W5-CLUSTER §D2] with the base
	// retargeted and NOTHING else changed, this test PASSES. Its subject is
	// intact; only the literal it measures against had gone stale.
	//
	// ⛔ SO IT IS NOT DISCHARGED. §82 offers two dispositions — a LOCK-TIME
	// criterion is discharged, a STANDING one the rename BROKE is a W8 defect.
	// This is neither: the assertion still holds and still has work to do.
	// Discharging it would delete live coverage to tidy away a stale literal.
	//
	// ⭐ THE BASE IS SCAFFOLDING, THE ARRANGEMENT IS THE CRITERION ⟦§90⟧: what
	// this list carries is "the six columns `person` had before W5, unchanged
	// underneath W5's additions" — six columns, one of them the trade/workgroup
	// reference. It does not carry "that column is spelled `workgroup_id`".
	// `trade_id` is the DIRECT SUCCESSOR AT THE SAME POSITION ⟦§61⟧: at 88dd6a6
	// `PersonCreated` field 5 was `WorkgroupID string `+"`"+`json:"workgroup_id"`+"`"+`;
	// today it is `TradeID string `+"`"+`json:"trade_id"`+"`"+` in the same slot.
	"person_id", "site_id", "name", "company_id", "trade_id", "recorded_by",
}

// w5aDeclaredAdditions is W5a's own declared addition set (SEAM W5a-S-COLS).
var w5aDeclaredAdditions = []string{"last_name", "first_name", "position"}

// w5aW5DeclaredAdditions is W5's FULL declared addition set for `person`, in
// the order W5-X4 part (b)'s enumeration sentence names them: last_name,
// first_name, position (W5a), phone (W5b). It is part (b)'s subject; part
// (a)'s subject is w5aDeclaredAdditions above, which is W5a's OWN three and
// is deliberately NOT widened here.
// ⟦SchemaVersion 11, 2026-09-05⟧ W5c's `pad_oriented` is STRUCK ([[D-88]]
// ERRATA / [[D-91]]) and so leaves the permitted union: if it comes back it
// is a STRAY, and part (b) reds on it by name. ⟦was: "…, \"phone\",
// \"pad_oriented\"}"⟧
var w5aW5DeclaredAdditions = []string{"last_name", "first_name", "position", "phone"}

// w5aPermittedUnion is W5-X4 part (b)'s permitted union: the pre-chunk six
// PLUS W5's declared additions — the ELEVEN columns the enumeration sentence
// names, with NO scoping by landing order.
//
// ⟦LOCK — W5a-G-2, post-lock amendment 2026-09-01. THE TIGHTENING IS STRUCK.⟧
// This file previously built the NINE-column tightened union ("this sub-chunk
// and every earlier-ordered sub-chunk"). That reading was ruled a defect in
// the CRITERION — not in W5a's build, which passes part (a), and not in this
// test, which implemented the operative reading correctly and loudly. The
// tightened union is a statement about the WHOLE TREE's columns, which
// coincides with a statement about THIS sub-chunk's bytes only if each
// sub-chunk is gated before the next one lands, and nothing in the plan ever
// required that: all three person sub-chunks built into one uncommitted
// working tree, so the row was unsatisfiable by construction at W5a's own
// gate and unrepairable there, because what it objected to was another
// sub-chunk's correct work. Authority: reports/window-build/
// W5a-falsification.md §2.2 and §9's G-2; the amendment and its measured
// discharge are at reports/W5-POSTLOCK-AMENDMENTS.md §15.2 / §16.3 / §18.
//
// ⚠ WHAT PART (b) NO LONGER CARRIES, stated rather than left to be found:
// anti-OVER-build. A sub-chunk that builds its own columns AND a later
// sub-chunk's now passes part (b). Anti-SUBSTITUTION — W5a's row cannot go
// green on another sub-chunk's column — is carried by part (a) below, which
// names W5a's own three as literals; that is where (m5a-2), (m5b-5) and
// (m5c-4) all bite, and (m5a-2)'s arms have produced kills. Anti-over-build
// is not discarded: it reverts to W5-X3's words, "no sub-chunk's green counts
// toward another's", enforced by the gate reading the ledger.
// ⟦was: "W5-X4 part (b)'s permitted union AT W5a'S GATE, under the rev-7
// TIGHTENING … W5a is FIRST, so its union is six + three = nine — it does NOT
// include `phone` (W5b) or `pad_oriented` (W5c)"⟧
var w5aPermittedUnion = append(append([]string{}, w5aPersonPreChunkColumns...), w5aW5DeclaredAdditions...)

// w5aLaterSubChunkColumns are the columns W5b and W5c declare. Under the
// amended eleven-column union they are PERMITTED, so the branch that consults
// this map is unreachable by construction. It is KEPT rather than deleted, on
// the amendment's own instruction: it records what the reading used to be, and
// if it ever fires it means w5aPermittedUnion has drifted from W5-X4's
// enumeration sentence — a defect in this file, not a serialisation violation.
var w5aLaterSubChunkColumns = map[string]string{"phone": "W5b"} // ⟦was: + "pad_oriented": "W5c" — struck at SchemaVersion 11⟧

// --- introspection machinery (no assertions) ---------------------------

func w5aOpenRaw(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// w5aColumn is one pragma_table_info row.
type w5aColumn struct {
	Name     string
	Type     string
	NotNull  bool
	HasDflt  bool
	DfltText string
}

func w5aTableInfo(t *testing.T, db *sql.DB, table string) []w5aColumn {
	t.Helper()
	rows, err := db.Query(`SELECT name, type, "notnull", dflt_value FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []w5aColumn
	for rows.Next() {
		var c w5aColumn
		var nn int
		var dflt sql.NullString
		if err := rows.Scan(&c.Name, &c.Type, &nn, &dflt); err != nil {
			t.Fatal(err)
		}
		c.NotNull = nn != 0
		c.HasDflt = dflt.Valid
		c.DfltText = dflt.String
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func w5aColumnNames(cols []w5aColumn) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		out = append(out, c.Name)
	}
	return out
}

func w5aFindColumn(cols []w5aColumn, name string) (w5aColumn, bool) {
	for _, c := range cols {
		if c.Name == name {
			return c, true
		}
	}
	return w5aColumn{}, false
}

// w5aMissing returns the members of want absent from cols.
func w5aMissing(cols []w5aColumn, want []string) []string {
	var missing []string
	for _, w := range want {
		if _, ok := w5aFindColumn(cols, w); !ok {
			missing = append(missing, w)
		}
	}
	return missing
}

// w5aOutsideUnion returns the columns of cols that are not in permitted —
// W5-X4 part (b)'s detector, shared by the control and the real assertion.
func w5aOutsideUnion(cols []w5aColumn, permitted []string) []string {
	ok := map[string]bool{}
	for _, p := range permitted {
		ok[p] = true
	}
	var stray []string
	for _, c := range cols {
		if !ok[c.Name] {
			stray = append(stray, c.Name)
		}
	}
	sort.Strings(stray)
	return stray
}

// w5aShapeFaults returns part (a)'s faults for one declared column: absent,
// wrong declared type, NOT NULL, or carrying a default. Shared by the
// control and the real assertion so the control exercises the same code.
func w5aShapeFaults(cols []w5aColumn, name string) []string {
	c, ok := w5aFindColumn(cols, name)
	if !ok {
		return []string{name + ": absent"}
	}
	var faults []string
	if !strings.EqualFold(strings.TrimSpace(c.Type), "TEXT") {
		faults = append(faults, fmt.Sprintf("%s: declared type %q, want TEXT", name, c.Type))
	}
	if c.NotNull {
		faults = append(faults, name+": declared NOT NULL, want nullable")
	}
	if c.HasDflt {
		faults = append(faults, fmt.Sprintf("%s: carries a default (%q), want none", name, c.DfltText))
	}
	return faults
}

var w5aStrictRe = regexp.MustCompile(`(?is)\)\s*strict\s*;?\s*$`)

// w5aTableDDL returns the CREATE statement sqlite recorded for table.
func w5aTableDDL(t *testing.T, db *sql.DB, table string) string {
	t.Helper()
	var ddl string
	err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&ddl)
	if err != nil {
		t.Fatalf("reading DDL for %s: %v", table, err)
	}
	return ddl
}

// w5aStrictBehaviourHolds is the BEHAVIOURAL half of "STRICT preserved": in a
// STRICT table a BLOB cannot be stored in a TEXT column. A declaration regex
// alone can be satisfied by a comment; this cannot. Returns true when the
// table behaves as STRICT.
//
// ⚠ The probe value is a BLOB, deliberately, and the first draft of this file
// used an INTEGER and had to be corrected: TEXT *affinity* converts a numeric
// value to text, so `VALUES (1, …)` is accepted by a STRICT table and the
// probe reported a STRICT table as loose. BLOB is not converted by TEXT
// affinity, so it is the value that discriminates. The control below proves
// the discrimination rather than asserting it.
func w5aStrictBehaviourHolds(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	_, err = tx.Exec(fmt.Sprintf(
		`INSERT INTO %s (person_id, site_id, name) VALUES ('w5a-strict-probe', 'site-a', x'00ff')`, table))
	return err != nil
}

// --- W5a-2 / W5-X4 -----------------------------------------------------

// TestW5aPersonProjectionColumnsW5X4 — W5a-2's schema half, asserted in
// W5-X4's two-part form.
//
// EXPECTED RED (pre-chunk, runtime): part (a) fails with the three declared
// columns absent — the `person` projection is the pre-chunk six at 88dd6a6.
// EXPECTED RED (m5a-2): drop/rename a column, or declare one NOT NULL, or
// give it a default -> part (a). EXPECTED RED (m5-X4): add any column outside
// the union -> part (b).
func TestW5aPersonProjectionColumnsW5X4(t *testing.T) {
	// --- CONTROL FIRST (W5-X6 (a)). Every tooth of the detector is shown
	// biting on a PLANTED violation before the real table is looked at. A
	// single plant against a four-tooth detector is a green ledger row
	// proving nothing (W5a-1's own note, applied here).
	ctlPath := filepath.Join(t.TempDir(), "w5a-x4-control.db")
	ctl := w5aOpenRaw(t, ctlPath)

	// (a1) a DROPPED/renamed declared column.
	if _, err := ctl.Exec(`CREATE TABLE person_ctl_dropped (
		person_id TEXT PRIMARY KEY, site_id TEXT NOT NULL, name TEXT NOT NULL,
		company_id TEXT, trade_id TEXT, recorded_by TEXT,
		last_name TEXT, first_name TEXT
	) STRICT`); err != nil {
		t.Fatal(err)
	}
	dropped := w5aTableInfo(t, ctl, "person_ctl_dropped")
	if miss := w5aMissing(dropped, w5aDeclaredAdditions); len(miss) != 1 || miss[0] != "position" {
		t.Fatalf("W5-X4 CONTROL FAILED (a1): the missing-column detector reported %v on a table with `position` dropped, want exactly [position] — a detector that cannot see its own plant proves nothing (m5a-2)", miss)
	}

	// (a2) a declared column made NOT NULL, and (a3) one given a default.
	if _, err := ctl.Exec(`CREATE TABLE person_ctl_shape (
		person_id TEXT PRIMARY KEY, site_id TEXT NOT NULL, name TEXT NOT NULL,
		company_id TEXT, trade_id TEXT, recorded_by TEXT,
		last_name TEXT NOT NULL, first_name TEXT DEFAULT '', position INTEGER
	) STRICT`); err != nil {
		t.Fatal(err)
	}
	shape := w5aTableInfo(t, ctl, "person_ctl_shape")
	if f := w5aShapeFaults(shape, "last_name"); len(f) != 1 || !strings.Contains(f[0], "NOT NULL") {
		t.Fatalf("W5-X4 CONTROL FAILED (a2): the nullability tooth reported %v on a planted NOT NULL column (m5a-2)", f)
	}
	if f := w5aShapeFaults(shape, "first_name"); len(f) != 1 || !strings.Contains(f[0], "default") {
		t.Fatalf("W5-X4 CONTROL FAILED (a3): the default tooth reported %v on a planted DEFAULT '' column (m5a-2)", f)
	}
	if f := w5aShapeFaults(shape, "position"); len(f) != 1 || !strings.Contains(f[0], "want TEXT") {
		t.Fatalf("W5-X4 CONTROL FAILED (a4): the declared-type tooth reported %v on a planted INTEGER column (m5a-2)", f)
	}

	// (b) a column OUTSIDE the union.
	if _, err := ctl.Exec(`CREATE TABLE person_ctl_extra (
		person_id TEXT PRIMARY KEY, site_id TEXT NOT NULL, name TEXT NOT NULL,
		company_id TEXT, trade_id TEXT, recorded_by TEXT,
		last_name TEXT, first_name TEXT, position TEXT, nickname TEXT
	) STRICT`); err != nil {
		t.Fatal(err)
	}
	extra := w5aTableInfo(t, ctl, "person_ctl_extra")
	if stray := w5aOutsideUnion(extra, w5aPermittedUnion); len(stray) != 1 || stray[0] != "nickname" {
		t.Fatalf("W5-X4 CONTROL FAILED (b): the outside-the-union detector reported %v on a table carrying a planted `nickname`, want exactly [nickname] — this is the tooth the silent repair to a superset check deletes (m5-X4)", stray)
	}
	if stray := w5aOutsideUnion(dropped, w5aPermittedUnion); len(stray) != 0 {
		t.Fatalf("W5-X4 CONTROL: the outside-the-union detector flagged %v on a table with no stray column — a detector that flags a clean table proves nothing either", stray)
	}

	// (c) STRICT: the behavioural probe must ACCEPT the integer on a
	// non-STRICT table (proving it can discriminate) and the regex must not
	// match its DDL.
	if _, err := ctl.Exec(`CREATE TABLE person_ctl_loose (
		person_id TEXT PRIMARY KEY, site_id TEXT NOT NULL, name TEXT NOT NULL,
		company_id TEXT, trade_id TEXT, recorded_by TEXT,
		last_name TEXT, first_name TEXT, position TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	if w5aStrictBehaviourHolds(t, ctl, "person_ctl_loose") {
		t.Fatal("W5-X4 CONTROL FAILED (c): the STRICT probe reported a NON-strict table as strict — it stores an INTEGER in a TEXT column, so the probe cannot discriminate and its clean pass would prove nothing")
	}
	if w5aStrictRe.MatchString(w5aTableDDL(t, ctl, "person_ctl_loose")) {
		t.Fatal("W5-X4 CONTROL FAILED (c): the STRICT declaration regex matched a non-STRICT CREATE statement")
	}
	if !w5aStrictBehaviourHolds(t, ctl, "person_ctl_extra") {
		t.Fatal("W5-X4 CONTROL FAILED (c): the STRICT probe reported a STRICT table as loose")
	}

	// --- the real projection ------------------------------------------
	st, err := store.Open(filepath.Join(t.TempDir(), "w5a-x4.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	path := st.Path
	st.Close()
	db := w5aOpenRaw(t, path)

	cols := w5aTableInfo(t, db, "person")
	// Rule 6 — assert the arrangement this test needs actually holds: the
	// projection exists and still carries the pre-chunk six. Without this a
	// missing table would report as "columns absent" and read like W5a-2's
	// ordinary red rather than a broken fixture.
	if len(cols) == 0 {
		t.Fatalf("fixture defect: the `person` projection table does not exist after domain.Register — W5a-2 cannot be measured")
	}
	if miss := w5aMissing(cols, w5aPersonPreChunkColumns); len(miss) != 0 {
		t.Fatalf("fixture defect: the pre-chunk `person` columns %v are absent — W5-X4 part (b)'s base is measured at refdata.go:197–204 and this tree does not match it", miss)
	}

	// PART (a): W5a's own three columns, TEXT, nullable, no default.
	var faults []string
	for _, name := range w5aDeclaredAdditions {
		faults = append(faults, w5aShapeFaults(cols, name)...)
	}
	if len(faults) != 0 {
		t.Errorf("W5-X4(a) / W5a-2 (m5a-2): the `person` projection does not carry W5a's three declared columns in the declared shape — nullable TEXT, no default:\n  %s\n  actual columns: %v",
			strings.Join(faults, "\n  "), w5aColumnNames(cols))
	}

	// PART (a), STRICT preserved — declaration AND behaviour.
	if ddl := w5aTableDDL(t, db, "person"); !w5aStrictRe.MatchString(ddl) {
		t.Errorf("W5-X4(a): the `person` CREATE statement no longer ends in STRICT — %q", ddl)
	}
	if !w5aStrictBehaviourHolds(t, db, "person") {
		t.Errorf("W5-X4(a): the `person` table accepted an INTEGER in a TEXT column — STRICT was not preserved across W5a's shape change")
	}

	// PART (b): nothing outside the permitted union.
	if stray := w5aOutsideUnion(cols, w5aPermittedUnion); len(stray) != 0 {
		var later []string
		for _, s := range stray {
			if owner, ok := w5aLaterSubChunkColumns[s]; ok {
				later = append(later, s+" ("+owner+")")
			}
		}
		msg := fmt.Sprintf("W5-X4(b) (m5-X4): `person` carries column(s) %v outside the permitted union %v", stray, w5aPermittedUnion)
		if len(later) != 0 {
			msg += fmt.Sprintf("\n  NOTE: %v are W5's OWN declared additions and are INSIDE part (b)'s eleven-column union (post-lock amendment 2026-09-01, W5a-G-2). Reaching this branch means w5aPermittedUnion has drifted from W5-X4's enumeration sentence — a defect in this test file, not in the tree.", later)
		}
		t.Error(msg)
	}
}

// --- W5-X5 — RE-POINTED to the '9' edge ----------------------------------
//
// ⟦LOCK — W5a-G-1, post-lock amendment 2026-09-01⟧
// W5a's mutant gate (reports/window-build/W5a-falsification.md §4.3, §9 G-1)
// found W5a's W5-X5 row BLANK: the bump requirement was struck, this file's
// bump test was removed, and the only checks that would catch the re-pointed
// mutant were ⟦was: "TestW1SchemaVersionIsTen", then "TestW1SchemaVersionIsEleven"⟧ (now TestW1SchemaVersionIsSixteen) and internal/repl's pin — W1's tests.
// W5-X3 forbids reading another SUB-CHUNK's green as W5a's, and the same
// reasoning forbids reading another CHUNK's. The gate's words: "What may not
// stand is a blank."
//
// RULED: RE-POINT, not borrow. The test below is W5a's own row. What it
// asserts is W5a-SPECIFIC and is not a copy of W1's: that W5a's three columns
// are inside the v10 shape — the store that carries last_name/first_name/
// position reads back the increment's exit version — and that a store stamped
// the literal predecessor is REFUSED at Open. The fixture machinery
// (w5aStampSchemaVersion, retained caller-less by the adjudication for exactly
// this) is re-pointed from the struck '10' edge to the '9' edge.

// ── REMOVED by reports/W5-SCHEMAVERSION-ADJUDICATION.md (2026-09-01) ──────────
// ⟦was: "TestW5aSchemaVersionBumpedForThePersonShapeChange"⟧
// asserted a WITHIN-WINDOW `store.SchemaVersion` bump for W5a. That
// requirement is STRUCK. The schema-10 window carries exactly ONE increment —
// W1's 9 → 10 at 49abf65 — and every W5 sub-chunk rides it: the v10 log starts
// empty (D-29), §7 step 2 deletes every database at the cutover, and one
// hash-verified binary ships to both posts, so no stored database and no peer
// meets a differently-shaped binary inside the window (plan §8 bump-gate rows
// 3, 8, 10 price these columns as "window-priced"). The error was in W5-X5 AS
// LOCKED, not introduced by post-lock amendment §1, which propagated it.
// Its arms were (iii) `SchemaVersion > 10`, and (i)/(ii) a store stamped the
// literal '10' refused at Open — both of which now assert the opposite of the
// ruling. The '9'-edge form the ruling keeps is ALREADY landed, in
// ⟦was: "TestW1V10BinaryRefusesAV9DatabaseAtOpen and TestW1SchemaVersionIsTen",
// then "TestW1V11BinaryRefusesAV10DatabaseAtOpen and TestW1SchemaVersionIsEleven"⟧
// (both re-pinned and renamed again at the 11 -> 12 increment, R-16:
// TestW1V16BinaryRefusesAV15DatabaseAtOpen, TestW1SchemaVersionIsSixteen).
// W5a's real column protection is W5-X4, which introspects the landed
// column set and is UNAFFECTED by this ruling — see
// `TestW5aPersonProjectionColumnsW5X4` in this file.
// Removed by the plan-owner pass, not by a builder (the route by which
// muster_test.go's live half was amended, post-lock amendments §5/§6).
// ─────────────────────────────────────────────────────────────────────────────

// TestW5aColumnsRideTheV12ShapeAndAV11StoreIsRefused is W5a's OWN W5-X5 row,
// re-pointed to the '9' edge (⟦LOCK — W5a-G-1, post-lock amendment
// 2026-09-01⟧). W5-X5's three-part form, with W5a's columns as the subject:
//
//	(iii) a store that carries W5a's three columns reads back
//	      StoredSchemaVersion() == fmt.Sprint(store.SchemaVersion) AND == "12"
//	      ⟦re-pinned 2026-09-11 at the 11 -> 12 increment (R-16); was "11"⟧
//	      — the window's exit version, pinned. THIS IS THE KILLING ARM.
//	(i)   a store stamped the literal predecessor '11' is refused at Open by a
//	      non-nil UNTYPED error naming both versions and the word "refusing".
//	(ii)  the fixture stamps the LITERAL '11', never store.SchemaVersion-1.
//
// KILLER MUTANT: (m5-X5) as RE-POINTED by post-lock amendment §9 — "a
// sub-chunk that BUMPS SchemaVersion". Under it store.SchemaVersion is 12, the
// store W5a's columns land in stamps "12", and arm (iii) reds. Arms (i)/(ii)
// stay green under that mutant (a '9' store is refused by a 12 binary too),
// which is exactly why (iii) is pinned to a literal and not only to a relation
// — an unpinned relation-only read-back is vacuous, as W5-X5 says in terms.
//
// ⚠️ NOT A COPY OF W1's TESTS. TestW1SchemaVersionIsSixteen reads the constant;
// this reads the version STAMPED IN THE STORE THAT CARRIES W5a's COLUMNS, so
// the column shape and the version are asserted of the same artefact. That is
// the assertion W5a owns and W1's tests do not make.
//
// CONTROL (W5-X6 (a), "W5-X5's pre-bump control"): the same fixture machinery,
// stamped with the CURRENT version, must Open SUCCESSFULLY. Without it a
// fixture that refuses for any reason — a corrupted rewrite, a wrong path —
// reads as a pass.
// ⟦was: TestW5aColumnsRideTheV11ShapeAndAV10StoreIsRefused — re-pinned 11 -> 12⟧
func TestW5aColumnsRideTheV12ShapeAndAV11StoreIsRefused(t *testing.T) {
	dir := t.TempDir()

	// --- CONTROL, first: the pre-bump control ------------------------------
	// A store stamped the CURRENT version must Open. This proves the fixture
	// machinery below builds an OPENABLE store, so the '9' refusal that
	// follows is about the version and not about a broken rewrite.
	ctlPath := filepath.Join(dir, "w5a-x5-control.db")
	w5aStampSchemaVersion(t, ctlPath, fmt.Sprint(store.SchemaVersion))
	ctl, err := store.Open(ctlPath)
	if err != nil {
		t.Fatalf("W5-X5 CONTROL FAILED (pre-bump control): a store stamped the CURRENT version %d did not Open: %v — the fixture rewrite is broken, so the '9' refusal below would prove nothing", store.SchemaVersion, err)
	}
	if err := ctl.Close(); err != nil {
		t.Fatal(err)
	}

	// --- (iii) W5a's columns are inside the v10 shape ----------------------
	st, err := store.Open(filepath.Join(dir, "w5a-x5.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	stored, err := st.StoredSchemaVersion()
	if err != nil {
		t.Fatalf("fixture defect: reading back schema_version: %v", err)
	}
	livePath := st.Path
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	// Rule 6 — the arrangement this arm needs must actually hold: the store
	// read above must be the one carrying W5a's three columns. Without this
	// the version read is about some store, not about W5a's.
	live := w5aOpenRaw(t, livePath)
	if miss := w5aMissing(w5aTableInfo(t, live, "person"), w5aDeclaredAdditions); len(miss) != 0 {
		t.Fatalf("fixture defect: the store whose schema_version was read does not carry W5a's columns %v — W5-X5's subject here is W5a's OWN shape at the window's version, and this store is not it", miss)
	}

	if want := fmt.Sprint(store.SchemaVersion); stored != want {
		t.Errorf("W5-X5(iii): the store carrying W5a's columns reads back schema_version %q, want %q (= fmt.Sprint(store.SchemaVersion))", stored, want)
	}
	// Re-pinned 15 -> 16 (R3 alert_kind, 2026-10-04): this literal tracks the
	// CURRENT version. ⟦was: `stored != "15"`⟧
	if stored != "16" {
		t.Errorf("W5-X5(iii) (m5-X5, RE-POINTED AGAIN): the store carrying W5a's columns reads back schema_version %q, want \"16\" — the 15 -> 16 increment's version (R3 alert_kind, 2026-10-04, the sole bump of fix round 2), re-pinned from the 14 -> 15 increment's EXIT version (R-20 + R-23, 2026-09-13; the rejoin path — visit carries its minting key, is_open is derived, restored presence is marked). W5a's own columns rode the 9->10 window and have not moved since; this literal just tracks whatever the CURRENT version is, re-pointed at the 11->12 bump (R-16), again at 12->13 (R-17), again at 13->14 (R-18), and again here. A stray bump here silently falsifies design/WINDOW-CHECKLIST-v1.md:171 (the PRINTED operator checkbox, now stale at \"14\" until a later pass updates it too), TestW1SchemaVersionIsSixteen, and internal/repl's v14 refusal pin", stored)
	}

	// --- (i)/(ii) a store stamped the LITERAL '11' is refused at Open ------
	// '11' is the literal predecessor — the version the schema-11 window
	// actually ran, so the only stale database a v12 binary can meet (W5-X5(ii):
	// "never SchemaVersion-1"). SchemaVersion-1 would be 11 today and would
	// silently become 12 under the mutant above, at which point this arm would
	// stop testing the edge it names. ⟦re-pinned from '10' at the 11 -> 12
	// increment, 2026-09-11⟧
	oldPath := filepath.Join(dir, "w5a-x5-v11.db")
	w5aStampSchemaVersion(t, oldPath, "11")
	if _, err := store.Open(oldPath); err == nil {
		t.Error("W5-X5(i): Open SUCCEEDED over a store stamped schema_version '11'; the v12 binary must refuse it")
	} else if !strings.Contains(err.Error(), "schema_version 11 ") || !strings.Contains(err.Error(), fmt.Sprint(store.SchemaVersion)) || !strings.Contains(err.Error(), "refusing") {
		// Assertion cloned from TestW1V16BinaryRefusesAV15DatabaseAtOpen
		// (w1_version_gate_test.go:36-56), never a type switch:
		// store.SchemaVersionError is the per-envelope REPLICATION refusal and
		// is not on the Open path, so errors.As here books a kill that can
		// never be produced (W5-X5(i), stated in terms).
		t.Errorf("W5-X5(i): the refusal must be a non-nil UNTYPED error naming BOTH versions and the word \"refusing\", got: %v", err)
	}
}

// w5aStampSchemaVersion was RETAINED caller-less when the struck bump test was
// removed above: W5-SCHEMAVERSION-ADJUDICATION.md §7.2 strikes W5-X5's
// requirement but keeps its assertion MACHINERY as sound, "so the fixture stays
// available should W5a's owner re-point it to the '9' edge".
// ⟦LOCK — W5a-G-1, post-lock amendment 2026-09-01: RE-POINTED. Its caller is
// TestW5aColumnsRideTheV12ShapeAndAV11StoreIsRefused above, which stamps the
// literal predecessor '11' — never store.SchemaVersion-1 (re-pinned from '10'
// at the 11 -> 12 increment).⟧
// (`strconv` left the import block with the removed test; nothing else in this
// file used it.)
//
// w5aStampSchemaVersion creates a store at path and rewrites its meta
// schema_version row to version, then closes it — W5-X5(ii)'s fixture
// construction ("by rewriting meta on a freshly opened store … not by
// obtaining a pre-W5 binary").
func w5aStampSchemaVersion(t *testing.T, path, version string) {
	t.Helper()
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("fixture defect: opening %s: %v", path, err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("fixture defect: closing %s: %v", path, err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("fixture defect: raw open of %s: %v", path, err)
	}
	defer db.Close()
	res, err := db.Exec(`UPDATE meta SET value = ? WHERE key = 'schema_version'`, version)
	if err != nil {
		t.Fatalf("fixture defect: stamping schema_version=%s: %v", version, err)
	}
	// Rule 6: an UPDATE touching zero rows would leave the fixture stamped
	// with the binary's own version and every assertion below vacuous.
	n, err := res.RowsAffected()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("fixture defect: stamping schema_version=%s touched %d rows, want 1", version, n)
	}
}
