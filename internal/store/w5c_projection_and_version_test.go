// ⚠️ READ FIRST — W5-X5's BUMP REQUIREMENT IS STRUCK (2026-09-01).
// reports/W5-SCHEMAVERSION-ADJUDICATION.md rules that the schema-10 window
// carries exactly ONE SchemaVersion increment — W1's 9 → 10 — and that NO W5
// sub-chunk bumps it. Every mention of "the shape change bumps
// store.SchemaVersion" below is SUPERSEDED text, kept so the record stays
// diffable; the removal record further down this file states what went and why.
// The criteria this file still carries live — W5-X4 and its controls — are
// UNAFFECTED.

// w5c_projection_and_version_test.go — W5c's STORE-LEVEL halves — CORRECTED
// TO THE SCHEMA-11 SHAPE (2026-09-05; [[D-88]] + ERRATA, [[D-91]]):
//
//   - W5c-4 (schema half) in W5-X4's TWO-PART form: ⟦was: the `person`
//     projection GAINS `pad_oriented` as nullable TEXT⟧ — the `person`
//     projection carries NO `pad_oriented` column (part a, INVERTED: the
//     struck column is absent, STRICT preserved), and no column outside the
//     permitted union, from which the struck column is REMOVED (part b).
//   - W5c-3: ⟦was: NO KEYED ORIENTATION TABLE EXISTS ([[D-21]].1)⟧ — D-88
//     REVERSES D-21.1: the keyed, per-site `orientation` record minted by the
//     ORIENTING post is now the RULED shape (a later stage of this
//     increment). What this row asserts at SchemaVersion 11 is the half that
//     survives the reversal: NO orientation COLUMN exists on any table — the
//     column-shaped flag is the reversed shape, and the struck one must not
//     come back under `pad_oriented` or any `*_oriented` spelling.
//   - W5-X5 at W5c: the store from which W5c's column is STRUCK reads back
//     the increment's version, and a schema-10 store is refused at Open.
//
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, criteria LOCKED 2026-08-31.
//
// MUTANTS THIS FILE KILLS:
//
//	(m5c-3)  a keyed `person_orientation` table, or a second orientation
//	         column                                        -> W5c-3
//	(m5c-4)  the projection column dropped, renamed or declared NOT NULL
//	                                                       -> W5c-4 part (a)
//	(m5c-5)  the column declared INTEGER (which makes `false` representable
//	         and reopens W5c-1's value domain through the created half)
//	                                                       -> W5c-4 part (a)
//	(m5-X4)  a column outside the permitted union          -> W5c-4 part (b)
//	(m5-X5)  a column added with no SchemaVersion bump: green at Open, red at
//	         the first enrolment                           -> W5-X5 below
//
// CONTROLS (W5-X6 (a)): every detector is shown detecting a PLANTED
// violation — on a scratch table or a scratch database, never on the real
// one — before the real object's clean pass counts. One plant per tooth, so
// a relaxed detector cannot pass on its one surviving tooth.
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
// SEAM W5c-S-COL: the new `person` column is spelled `pad_oriented` and is
// declared TEXT. NOT a free choice — W5-X4 part (b) names the spelling
// verbatim ("`pad_oriented` (W5c)") and DR-8 rules the declared type
// ("the `person.pad_oriented` column is TEXT nullable under STRICT, matching
// `person_amendment.pad_oriented`"). Recorded only because this file asserts
// both as literals.
//
// SEAM W5c-S-PREBUMP — VOID (W5-SCHEMAVERSION-ADJUDICATION.md, 2026-09-01).
// ⟦was: "the old-store fixture stamps a DERIVED literal predecessor, not
// `store.SchemaVersion-1` … and not a pinned successor literal … See
// w5cDerivedPredecessor for the arithmetic and why W5a's flat `'10'` cannot
// be reused at a gate that is not the first."⟧ There is no W5c predecessor to
// derive: the window's only edge is 9 → 10, paid by W1, and W5c rides it.

// w5cPersonPreChunkColumns is the MEASURED pre-chunk `person` column set
// (refdata.go:197–204 at 88dd6a6) — W5-X4 part (b)'s base.
var w5cPersonPreChunkColumns = []string{
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

// w5cSubChunkColumns maps each W5 person-touching sub-chunk to its declared
// additions, in W5-X3's LANDING ORDER (W5a, then W5b, then W5c).
var w5cSubChunkColumns = []struct {
	SubChunk string
	Columns  []string
}{
	{"W5a", []string{"last_name", "first_name", "position"}},
	{"W5b", []string{"phone"}},
	// ⟦SchemaVersion 11⟧ W5c's own addition is STRUCK ([[D-88]] ERRATA) and
	// therefore contributes NOTHING to the permitted union: if it returns it
	// is a stray. ⟦was: {"W5c", []string{"pad_oriented"}}⟧
	{"W5c", []string{}},
}

// w5cStruckColumn is the column SchemaVersion 11 struck from `person` and
// `person_amendment`. Its ABSENCE is now W5c-4's subject.
const w5cStruckColumn = "pad_oriented"

// w5cPermittedUnion is W5-X4 part (b)'s permitted union AT W5c'S GATE.
//
// ⚠ THE LOCK'S TWO READINGS CONVERGE HERE, and W5c is the one gate where
// they do. The enumeration sentence listed all eleven columns; the tightening
// clause scopes the union to "this sub-chunk and every earlier-ordered
// sub-chunk" (⟦R3-LOW⟧, which the lock records as an open contradiction).
// W5c is LAST in W5-X3's order, so "this sub-chunk and every earlier one" IS
// the whole set. ⟦SchemaVersion 11⟧ That set is TEN, not eleven: W5c's own
// column is struck and leaves the union. Recorded because a reader comparing
// the three files will otherwise think W5c relaxed it.
var w5cPermittedUnion = func() []string {
	out := append([]string{}, w5cPersonPreChunkColumns...)
	for _, sc := range w5cSubChunkColumns {
		out = append(out, sc.Columns...)
	}
	return out
}()

// --- introspection machinery (no assertions) ---------------------------

func w5cOpenRaw(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// w5cRegisteredStore opens a fresh registered store and returns it with its
// path, so the same database can be introspected raw.
func w5cRegisteredStore(t *testing.T, name string) (*store.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatalf("register: %v", err)
	}
	return st, path
}

type w5cColumn struct {
	Name     string
	Type     string
	NotNull  bool
	HasDflt  bool
	DfltText string
}

func w5cTableInfo(t *testing.T, db *sql.DB, table string) []w5cColumn {
	t.Helper()
	rows, err := db.Query(`SELECT name, type, "notnull", dflt_value FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []w5cColumn
	for rows.Next() {
		var c w5cColumn
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

func w5cColumnNames(cols []w5cColumn) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		out = append(out, c.Name)
	}
	return out
}

func w5cFindColumn(cols []w5cColumn, name string) (w5cColumn, bool) {
	for _, c := range cols {
		if c.Name == name {
			return c, true
		}
	}
	return w5cColumn{}, false
}

// w5cTableNames lists every user table in the database.
func w5cTableNames(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// w5cShapeFaults returns part (a)'s faults for the declared column: absent,
// wrong declared type, NOT NULL, or carrying a default. Shared by the
// control and the real assertion, so the control exercises the same code.
//
// ⚠ THE DECLARED TYPE IS THE POINT, not a formality (DR-8, (m5c-5)): an
// INTEGER column makes `false` REPRESENTABLE and reopens W5c-1's value
// domain through the created half — the back door DR-8 exists to shut.
func w5cShapeFaults(cols []w5cColumn, name string) []string {
	c, ok := w5cFindColumn(cols, name)
	if !ok {
		return []string{name + ": ABSENT"}
	}
	var faults []string
	if !strings.EqualFold(strings.TrimSpace(c.Type), "TEXT") {
		faults = append(faults, fmt.Sprintf("%s: declared type %q, want TEXT (DR-8 — an INTEGER column makes `false` representable, which is (m5c-5))", name, c.Type))
	}
	if c.NotNull {
		faults = append(faults, name+": declared NOT NULL, want nullable — absent is the ONLY \"not oriented\", so the column must be able to hold it")
	}
	if c.HasDflt {
		faults = append(faults, fmt.Sprintf("%s: carries a default (%q), want none — a default would author a value nobody asserted", name, c.DfltText))
	}
	return faults
}

func w5cOutsideUnion(cols []w5cColumn, permitted []string) []string {
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

var w5cStrictRe = regexp.MustCompile(`(?is)\)\s*strict\s*;?\s*$`)

func w5cTableDDL(t *testing.T, db *sql.DB, table string) string {
	t.Helper()
	var ddl string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&ddl); err != nil {
		t.Fatalf("reading DDL for %s: %v", table, err)
	}
	return ddl
}

// w5cStrictBehaviourHolds is the BEHAVIOURAL half of "STRICT preserved": a
// STRICT table refuses a BLOB in a TEXT column. A declaration regex alone is
// satisfied by a comment; this is not.
//
// ⚠ The probe value is a BLOB deliberately. TEXT *affinity* silently
// converts a number to text, so an INTEGER probe reports a STRICT table as
// loose — the control below proves the discrimination rather than asserting
// it.
func w5cStrictBehaviourHolds(t *testing.T, db *sql.DB, table, col, pkCol string) bool {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	_, err = tx.Exec(fmt.Sprintf(`INSERT INTO %s (%s, %s) VALUES (?, ?)`, table, pkCol, col), "w5c-strict-probe", []byte{0x01, 0x02})
	return err != nil
}

// =======================================================================
// W5c-4 — THE CREATED HALF LANDS (schema half), in W5-X4's two-part form
// =======================================================================

// w5cStruckFaults is part (a)'s detector at SchemaVersion 11: the STRUCK
// column is a fault by its PRESENCE, under any declared type. It shares the
// old detector's shape (a list of faults, empty when compliant) so the
// control below exercises the same code the real assertion uses.
func w5cStruckFaults(cols []w5cColumn, name string) []string {
	c, ok := w5cFindColumn(cols, name)
	if !ok {
		return nil
	}
	return []string{fmt.Sprintf("%s: PRESENT (declared %s) — struck at SchemaVersion 11 ([[D-88]] ERRATA / [[D-91]]): one flag on the enrolling site's person row cannot be a per-site orientation", name, c.Type)}
}

// TestW5cPersonProjectionCarriesNoPadOrientedColumn — W5c-4 (schema half)
// and W5-X4, INVERTED at SchemaVersion 11.
// ⟦was: TestW5cPersonProjectionCarriesPadOriented — part (a) asserted the
// column PRESENT as nullable TEXT, no default; (m5c-4)/(m5c-5) were its
// dropped/renamed/INTEGER mutants⟧
//
// Part (a) now asserts the column ABSENT — its presence under ANY type is
// the fault — with the detector shown biting a planted presence first. Part
// (b) is unchanged in form and tighter in content: the permitted union lost
// the struck column, so its return is a stray. The STRICT tooth is unchanged.
//
// Kills (m5c-4′) — the column re-added under the old spelling — and
// (m5-X4) — any column outside the ten-column union.
func TestW5cPersonProjectionCarriesNoPadOrientedColumn(t *testing.T) {
	_, path := w5cRegisteredStore(t, "w5c-person.db")
	db := w5cOpenRaw(t, path)

	// --- CONTROL 1: part (a)'s tooth, on a scratch table that HAS the
	// struck column (under the old type and under an INTEGER, so the
	// detector is provably type-blind) ---
	scratch := `CREATE TABLE w5c_scratch_shape (
		person_id    TEXT PRIMARY KEY,
		pad_oriented TEXT,
		pad_int      INTEGER
	) STRICT;`
	if _, err := db.Exec(scratch); err != nil {
		t.Fatalf("fixture defect: scratch shape table: %v", err)
	}
	scratchCols := w5cTableInfo(t, db, "w5c_scratch_shape")
	if faults := w5cStruckFaults(scratchCols, w5cStruckColumn); len(faults) == 0 {
		t.Fatal("CONTROL FAILED: the part (a) detector reported NO fault for a planted `pad_oriented` column — a detector blind to its own mutant makes the real assertion below worthless")
	}
	if faults := w5cStruckFaults(scratchCols, "pad_int"); len(faults) == 0 {
		t.Fatal("CONTROL FAILED: the part (a) detector is not type-blind — it must report the struck column under ANY declared type")
	}
	if faults := w5cStruckFaults(scratchCols, "pad_absent"); len(faults) != 0 {
		t.Fatalf("CONTROL FAILED: the part (a) detector reported faults %v for a column that is not there — it would red every correct build", faults)
	}

	// --- CONTROL 2: part (b)'s tooth, on a scratch table ---
	loose := append(append([]w5cColumn{}, scratchCols...), w5cColumn{Name: "nickname", Type: "TEXT"})
	stray := w5cOutsideUnion(loose, w5cPermittedUnion)
	if len(stray) == 0 {
		t.Fatal("CONTROL FAILED: the part (b) detector reported NO stray column for a planted `nickname` — (m5-X4) would survive it")
	}
	if !w5cContains(stray, w5cStruckColumn) {
		t.Fatalf("CONTROL FAILED: the part (b) detector did not call the STRUCK column %q a stray (got %v) — it has left the permitted union at SchemaVersion 11 and must be reported if it returns", w5cStruckColumn, stray)
	}
	if stray := w5cOutsideUnion([]w5cColumn{{Name: "phone"}, {Name: "person_id"}}, w5cPermittedUnion); len(stray) != 0 {
		t.Fatalf("CONTROL FAILED: the part (b) detector called permitted columns stray: %v", stray)
	}

	// --- CONTROL 3: the STRICT probe discriminates ---
	if _, err := db.Exec(`CREATE TABLE w5c_scratch_loose (person_id TEXT PRIMARY KEY, probe TEXT);`); err != nil {
		t.Fatalf("fixture defect: scratch loose table: %v", err)
	}
	if w5cStrictBehaviourHolds(t, db, "w5c_scratch_loose", "probe", "person_id") {
		t.Fatal("CONTROL FAILED: the STRICT probe called a DELIBERATELY NON-STRICT table strict — it cannot then vouch for the real one")
	}
	if !w5cStrictBehaviourHolds(t, db, "w5c_scratch_shape", "pad_oriented", "person_id") {
		t.Fatal("CONTROL FAILED: the STRICT probe called a STRICT table loose — the probe value is not discriminating (an INTEGER probe does exactly this; the probe must be a BLOB)")
	}
	for _, drop := range []string{"w5c_scratch_shape", "w5c_scratch_loose"} {
		if _, err := db.Exec(`DROP TABLE ` + drop); err != nil {
			t.Fatalf("fixture defect: dropping %s: %v", drop, err)
		}
	}

	// --- the real table ---
	cols := w5cTableInfo(t, db, "person")
	if len(cols) == 0 {
		t.Fatal("fixture defect: the `person` table has no columns — Register did not create the projection")
	}
	// Part (b) is silent about DELETIONS, so the pre-chunk six AND the
	// surviving W5a/W5b additions are asserted present separately. Without
	// this, an empty table satisfies part (b) — and a strike that took the
	// neighbours with it would read as compliance.
	for _, want := range w5cPermittedUnion {
		if _, ok := w5cFindColumn(cols, want); !ok {
			t.Fatalf("W5-X4: the column %q has been REMOVED from `person` — SchemaVersion 11 strikes exactly ONE column, %q, and reshapes nothing else (actual: %v)", want, w5cStruckColumn, w5cColumnNames(cols))
		}
	}
	if faults := w5cStruckFaults(cols, w5cStruckColumn); len(faults) != 0 {
		t.Errorf("W5-X4(a) / W5c-4 (m5c-4′): the `person` projection still carries the struck column:\n  %s\n  actual columns: %v",
			strings.Join(faults, "\n  "), w5cColumnNames(cols))
	}
	if stray := w5cOutsideUnion(cols, w5cPermittedUnion); len(stray) != 0 {
		t.Errorf("W5-X4(b) (m5-X4): `person` carries %d column(s) outside the permitted union.\n  stray: %v\n  permitted union at W5c's gate (the pre-chunk six plus every SURVIVING W5 addition — ten at SchemaVersion 11, the struck column excluded): %v",
			len(stray), stray, w5cPermittedUnion)
	}
	ddl := w5cTableDDL(t, db, "person")
	if !w5cStrictRe.MatchString(strings.TrimSpace(ddl)) {
		t.Errorf("W5-X4(a): the `person` table is no longer declared STRICT:\n%s", ddl)
	}
	if !w5cStrictBehaviourHolds(t, db, "person", "name", "person_id") {
		t.Errorf("W5-X4(a): the `person` table does not BEHAVE as STRICT — a BLOB was accepted in a TEXT column. A STRICT declaration in a comment is not STRICT.")
	}
}

// =======================================================================
// W5c-3 — NO KEYED ORIENTATION TABLE EXISTS ([[D-21]].1, verbatim)
// =======================================================================

// w5cOrientationColumnFindings is W5c-3's detector at SchemaVersion 11, run
// over a whole database: every COLUMN whose name speaks of orientation, on
// any table. ⟦was: w5cOrientationFindings — it ALSO flagged every TABLE whose
// name spoke of orientation, and permitted exactly the pair
// person.pad_oriented / person_amendment.pad_oriented⟧. [[D-88]] reverses
// D-21.1: a keyed, per-site orientation TABLE minted by the orienting post is
// now the RULED shape and is not a finding (it lands in a later stage of
// this increment, and this detector says nothing about whether it is there
// yet); the column-shaped flag on a person row is the REVERSED shape, and no
// spelling of it — the struck one included — may exist anywhere.
//
// ⚠️ SchemaVersion 12 (R-16) ONE NARROW, EXACT-NAME EXCEPTION:
// `visit.predates_orientation_rule` (store.go). It is NOT a per-row
// orientation-STATUS flag — it answers "does this visit predate the
// orientation rule's existence at all" (the deferred import's grandfathering
// marker, R-8-REVISED), never "does this person hold this site's
// orientation". It cannot disagree with the keyed `orientation` table
// because it makes no claim about anyone's standing under it. Excluded by
// EXACT table+column name, not by table or by a looser substring — proven
// narrow by TestW5cNoOrientationColumnSurvivesTheStrike's planted
// `visit.decoy_site_oriented`, which this same function must still catch.
func w5cOrientationColumnFindings(t *testing.T, db *sql.DB) []string {
	t.Helper()
	var out []string
	for _, table := range w5cTableNames(t, db) {
		for _, c := range w5cTableInfo(t, db, table) {
			if !strings.Contains(strings.ToLower(c.Name), "orient") {
				continue
			}
			if table == "visit" && c.Name == "predates_orientation_rule" {
				continue
			}
			out = append(out, fmt.Sprintf("COLUMN %s.%s — an orientation flag on a row; per-site orientation is the ORIENTING post's own keyed record ([[D-88]]), never a column on somebody's row", table, c.Name))
		}
	}
	sort.Strings(out)
	return out
}

// TestW5cNoOrientationColumnSurvivesTheStrike — W5c-3, re-verified at
// SchemaVersion 12 (R-16) with one new narrow exception; see
// w5cOrientationColumnFindings's own comment.
// ⟦was: TestW5cNoKeyedOrientationTableExists — [[D-21]].1 verbatim, REVERSED
// by [[D-88]]: "do not build a keyed person_orientation table" is now "build
// exactly that, minted by the orienting post"⟧
//
// The evidence is the planted CONTROLs below, in a SCRATCH database: the
// struck `person.pad_oriented`, a second-site `crew.ice_road_oriented`, and
// a keyed `person_orientation` TABLE — the first two must be reported, the
// third must NOT (it is the ruled shape). Then the same function reads the
// real database and must find nothing.
//
// EXPECTED RED: a builder who re-adds the struck column, or who "implements
// D-88" as a column per site on the person row (`pad_oriented`,
// `npf_oriented`, …) instead of the keyed record. Kills (m5c-3′).
func TestW5cNoOrientationColumnSurvivesTheStrike(t *testing.T) {
	// --- CONTROLs, in a scratch database ---
	plantPath := filepath.Join(t.TempDir(), "w5c-plant.db")
	plant, err := sql.Open("sqlite", plantPath)
	if err != nil {
		t.Fatal(err)
	}
	defer plant.Close()
	for _, ddl := range []string{
		`CREATE TABLE person (person_id TEXT PRIMARY KEY, pad_oriented TEXT) STRICT;`,
		`CREATE TABLE person_orientation (person_id TEXT, site_id TEXT, value TEXT) STRICT;`,
		`CREATE TABLE crew (person_id TEXT PRIMARY KEY, ice_road_oriented TEXT) STRICT;`,
		// SchemaVersion 12 (R-16): a decoy planted ON THE SAME TABLE as the
		// one real exception below (visit.predates_orientation_rule), under
		// a DIFFERENT name — proves the detector's narrow-by-exact-name
		// exclusion does not accidentally whitelist the whole `visit`
		// table. A per-row status flag under any other spelling must still
		// be caught here.
		`CREATE TABLE visit (visit_id TEXT PRIMARY KEY, decoy_site_oriented TEXT) STRICT;`,
	} {
		if _, err := plant.Exec(ddl); err != nil {
			t.Fatalf("fixture defect: planting %q: %v", ddl, err)
		}
	}
	found := w5cOrientationColumnFindings(t, plant)
	var sawStruck, sawColumn, sawTable, sawVisitDecoy bool
	for _, f := range found {
		if strings.HasPrefix(f, "COLUMN person.pad_oriented") {
			sawStruck = true
		}
		if strings.HasPrefix(f, "COLUMN crew.ice_road_oriented") {
			sawColumn = true
		}
		if strings.Contains(f, "person_orientation") {
			sawTable = true
		}
		if strings.HasPrefix(f, "COLUMN visit.decoy_site_oriented") {
			sawVisitDecoy = true
		}
	}
	if !sawStruck {
		t.Fatalf("CONTROL FAILED: the detector did not see the planted STRUCK column `person.pad_oriented` — its return would survive it. findings: %v", found)
	}
	if !sawColumn {
		t.Fatalf("CONTROL FAILED: the detector did not see a planted per-site orientation COLUMN — the reversed shape would survive it. findings: %v", found)
	}
	if sawTable {
		t.Fatalf("CONTROL FAILED: the detector flagged the keyed `person_orientation` TABLE — that is [[D-88]]'s RULED shape, and a detector that refuses it reds the later stage of this very increment. findings: %v", found)
	}
	if !sawVisitDecoy {
		t.Fatalf("CONTROL FAILED: the detector did not see the planted decoy `visit.decoy_site_oriented` — the SchemaVersion 12 exception for `visit.predates_orientation_rule` would be reading as a whole-table exemption instead of an exact-name one. findings: %v", found)
	}

	// --- the real database ---
	_, path := w5cRegisteredStore(t, "w5c-orientation.db")
	db := w5cOpenRaw(t, path)
	tables := w5cTableNames(t, db)
	if len(tables) < 5 {
		t.Fatalf("fixture defect: the registered store holds only %d tables (%v) — the table list cannot be right, so a clean scan over it proves nothing", len(tables), tables)
	}
	if findings := w5cOrientationColumnFindings(t, db); len(findings) != 0 {
		t.Fatalf("W5c-3 (m5c-3′): [[D-88]] — orientation is the ORIENTING post's own keyed record, never a column on a person row. %d finding(s):\n  %s\n  tables: %v",
			len(findings), strings.Join(findings, "\n  "), tables)
	}
}

// =======================================================================
// W5-X5 at W5c — STRUCK. The shape change does NOT bump
// store.SchemaVersion; it rides W1's single 9 → 10
// (W5-SCHEMAVERSION-ADJUDICATION.md, 2026-09-01).
// =======================================================================

// w5cLandedSubChunks counts the W5 person-touching sub-chunks whose declared
// columns are ALL present, read off the table rather than assumed.
func w5cLandedSubChunks(cols []w5cColumn) (landed int, names []string) {
	for _, sc := range w5cSubChunkColumns {
		all := true
		for _, c := range sc.Columns {
			if _, ok := w5cFindColumn(cols, c); !ok {
				all = false
				break
			}
		}
		if all {
			landed++
			names = append(names, sc.SubChunk)
		}
	}
	return landed, names
}

// w5cDerivedPredecessor — REMOVED (W5-SCHEMAVERSION-ADJUDICATION.md, 2026-09-01).
// It computed `10 + landed - 1`: the per-sub-chunk bump arithmetic, which is the
// arm the ruling strikes by name (§7.3, W5b row: "its derived-predecessor arm
// (ii-b) must be removed — [it exists] only to enforce per-sub-chunk bumping").
// Its own doc block recorded the FALSE-RED direction and told a builder to take
// it to the gate rather than relax it. That gate ruled: it IS a false red, and
// the sub-chunks land under ONE shared bump. `strconv` left the import block
// with it — this was its only use in the file.

// w5cStampSchemaVersion creates a store at path and rewrites its `meta` row
// to `version` — W5-X5(ii)'s stated construction ("by rewriting meta on a
// freshly opened store … not by obtaining a pre-W5 binary").
func w5cStampSchemaVersion(t *testing.T, path, version string) {
	t.Helper()
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("fixture defect: creating the old-store fixture: %v", err)
	}
	if err := domain.Register(st); err != nil {
		t.Fatalf("fixture defect: registering the old-store fixture: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("fixture defect: closing the old-store fixture: %v", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	res, err := db.Exec(`UPDATE meta SET value = ? WHERE key = 'schema_version'`, version)
	if err != nil {
		db.Close()
		t.Fatalf("fixture defect: stamping schema_version=%s: %v", version, err)
	}
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		db.Close()
		t.Fatalf("fixture defect: stamping schema_version=%s affected %d rows (err=%v) — the fixture did not become an old store, so a refusal below would prove nothing", version, n, err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("fixture defect: closing the raw handle: %v", err)
	}
}

// ── REMOVED by reports/W5-SCHEMAVERSION-ADJUDICATION.md (2026-09-01) ──────────
// ⟦was: "TestW5cSchemaVersionBumpedForThePersonShapeChange"⟧
// asserted a WITHIN-WINDOW `store.SchemaVersion` bump for W5c. That
// requirement is STRUCK. The schema-10 window carries exactly ONE increment —
// W1's 9 → 10 at 49abf65 — and every W5 sub-chunk rides it: the v10 log starts
// empty (D-29), §7 step 2 deletes every database at the cutover, and one
// hash-verified binary ships to both posts, so no stored database and no peer
// meets a differently-shaped binary inside the window (plan §8 bump-gate rows
// 3, 8, 10 price these columns as "window-priced"). The error was in W5-X5 AS
// LOCKED, not introduced by post-lock amendment §1, which propagated it.
// Its arm (0b) `10 + landed` and its derived-predecessor fixture existed only
// to enforce per-sub-chunk bumping. At removal it was red at its PRECONDITION
// (pad_oriented absent, W5c unbuilt), not yet at the struck arms.
// W5c's real column protection is W5-X4, which introspects the landed
// column set and is UNAFFECTED by this ruling — see
// ⟦was: "`TestW5cPersonProjectionCarriesPadOriented`"⟧ (now
// TestW5cPersonProjectionCarriesNoPadOrientedColumn) in this file.
// Removed by the plan-owner pass, not by a builder (the route by which
// muster_test.go's live half was amended, post-lock amendments §5/§6).
// ─────────────────────────────────────────────────────────────────────────────

// =======================================================================
// W5-X5 at W5c — THE ROW, LANDED.
// ⟦LOCK — W5c's gate G-1, closed by a plan-owner pass 2026-09-01, on the
// pattern post-lock amendment §15.3 landed for W5a.⟧
//
// The removal record above is CORRECT and stands: the WITHIN-WINDOW BUMP
// requirement is struck and W5c does not bump. What §15.3 then re-pointed
// W5-X5 to is a DIFFERENT assertion — the '9' edge — and W5c owes it. Its
// gate planted (m5-X5) (`const SchemaVersion = 10` -> `11`) and measured that
// all nineteen W5c tests PASSED under it: the row was a BLANK, which §15.3
// forbids in terms ("recorded as a ruling, never left blank"). This is that
// row.
// =======================================================================

// TestW5cColumnRidesTheV12ShapeAndAV11StoreIsRefused is W5c's OWN W5-X5 row.
// W5-X5's three-part form, with W5c's column as the subject:
//
//	(iii) a store from which W5c's `pad_oriented` column is STRUCK reads back
//	      StoredSchemaVersion() == fmt.Sprint(store.SchemaVersion) AND == "12"
//	      ⟦re-pinned 2026-09-05 at the 10 -> 11 increment; was "11"⟧
//	      — the window's exit version, pinned. THIS IS THE KILLING ARM.
//	(i)   a store stamped the literal predecessor '11' is refused at Open by a
//	      non-nil UNTYPED error naming both versions and the word "refusing".
//	(ii)  the fixture stamps the LITERAL '10', never store.SchemaVersion-1.
//
// KILLER MUTANT: (m5-X5) as RE-POINTED — "a sub-chunk that BUMPS
// SchemaVersion". Under it store.SchemaVersion is 12, the store W5c's column
// lands in stamps "12", and arm (iii) reds. Arms (i)/(ii) stay green under
// that mutant (a '9' store is refused by a 12 binary too), which is exactly
// why (iii) is pinned to a LITERAL and not only to a relation — an unpinned
// relation-only read-back is vacuous, as W5-X5 says in terms.
//
// ⚠️ NOT A COPY OF W1's TESTS, and not a borrow of W5a's or W5b's.
// TestW1SchemaVersionIsSixteen reads the CONSTANT; this reads the version STAMPED
// IN THE STORE THAT CARRIES `pad_oriented`, so the column shape and the
// version are asserted of the SAME artefact. W5-X3 is in terms — "no
// sub-chunk's green counts toward another's" — and the same reasoning
// forbids reading another CHUNK's green, which is why W1's tests do not
// discharge this row either. W5c's gate ruled exactly that (§1.2) and
// declined to close the row by writing the test it would then bless.
//
// ⚠️ SEAM W5c-S-PREBUMP is recorded VOID above, and it stays void: there is
// no DERIVED W5c predecessor, and none is computed here. The window's only
// edge is 9 -> 10, paid by W1, and this arm stamps that 9 as a LITERAL.
// w5cLandedSubChunks likewise stays CALLER-LESS on purpose — its arithmetic
// fed the STRUCK per-sub-chunk bump arm (0b) and nothing here resurrects it.
//
// CONTROL (W5-X6 (a), "W5-X5's pre-bump control"): the same fixture
// machinery, stamped with the CURRENT version, must Open SUCCESSFULLY, and it
// runs FIRST. Without it a fixture that refuses for any other reason — a
// corrupted rewrite, a wrong path — reads as a pass.
// ⟦was: TestW5cColumnRidesTheV11ShapeAndAV10StoreIsRefused — re-pinned 11 -> 12⟧
func TestW5cColumnRidesTheV12ShapeAndAV11StoreIsRefused(t *testing.T) {
	dir := t.TempDir()

	// --- CONTROL, first: the pre-bump control ------------------------------
	// A store stamped the CURRENT version must Open. This proves the fixture
	// machinery below builds an OPENABLE store, so the '9' refusal that
	// follows is about the version and not about a broken rewrite.
	ctlPath := filepath.Join(dir, "w5c-x5-control.db")
	w5cStampSchemaVersion(t, ctlPath, fmt.Sprint(store.SchemaVersion))
	ctl, err := store.Open(ctlPath)
	if err != nil {
		t.Fatalf("W5-X5 CONTROL FAILED (pre-bump control): a store stamped the CURRENT version %d did not Open: %v — the fixture rewrite is broken, so the '9' refusal below would prove nothing", store.SchemaVersion, err)
	}
	if err := ctl.Close(); err != nil {
		t.Fatal(err)
	}

	// --- (iii) W5c's column is inside the v10 shape ------------------------
	st, livePath := w5cRegisteredStore(t, "w5c-x5.db")
	stored, err := st.StoredSchemaVersion()
	if err != nil {
		t.Fatalf("fixture defect: reading back schema_version: %v", err)
	}

	// Rule 6 — the arrangement this arm needs must actually hold: the store
	// whose version was just read must be the one W5c's column is STRUCK
	// from (⟦was: "the one carrying `pad_oriented`"⟧ — inverted with the
	// strike). Without this the version read is about SOME store, not about
	// W5c's shape at the increment's version.
	live := w5cOpenRaw(t, livePath)
	cols := w5cTableInfo(t, live, "person")
	if _, ok := w5cFindColumn(cols, w5cStruckColumn); ok {
		t.Fatalf("fixture defect: the store whose schema_version was read still carries the struck column %q — W5-X5's subject here is W5c's shape at the increment's version, and this store is not it\n  actual columns: %v", w5cStruckColumn, w5cColumnNames(cols))
	}

	if want := fmt.Sprint(store.SchemaVersion); stored != want {
		t.Errorf("W5-X5(iii): the store from which W5c's column is struck reads back schema_version %q, want %q (= fmt.Sprint(store.SchemaVersion))", stored, want)
	}
	// Re-pinned 15 -> 16 (R3 alert_kind, 2026-10-04): this literal tracks the
	// CURRENT version. ⟦was: `stored != "15"`⟧
	if stored != "16" {
		t.Errorf("W5-X5(iii) (m5-X5, RE-POINTED AGAIN): the store from which W5c's column is struck reads back schema_version %q, want \"16\" — the 15 -> 16 increment's version (R3 alert_kind, 2026-10-04, the sole bump of fix round 2), re-pinned from the 14 -> 15 increment's EXIT version (R-20 + R-23, 2026-09-13; the rejoin path — visit carries its minting key, is_open is derived, restored presence is marked). W5c's own strike rode the 9->10 window and has not moved since; this literal just tracks whatever the CURRENT version is, re-pointed at the 11->12 bump (R-16), again at 12->13 (R-17), again at 13->14 (R-18), and again here. A stray bump here silently falsifies design/WINDOW-CHECKLIST-v1.md:171 (the PRINTED operator checkbox, now stale at \"14\" until a later pass updates it too), TestW1SchemaVersionIsSixteen, and internal/repl's v14 refusal pin", stored)
	}

	// --- (i)/(ii) a store stamped the LITERAL '10' is refused at Open ------
	// '11' is the literal predecessor — the version the schema-11 window
	// actually ran, so the only stale database a v12 binary can meet (W5-X5(ii):
	// "never SchemaVersion-1"). SchemaVersion-1 would be 11 today and would
	// silently become 12 under the mutant above, at which point this arm would
	// stop testing the edge it names. ⟦re-pinned from '10' at the 11 -> 12
	// increment, 2026-09-11⟧
	oldPath := filepath.Join(dir, "w5c-x5-v11.db")
	w5cStampSchemaVersion(t, oldPath, "11")
	if old, err := store.Open(oldPath); err == nil {
		old.Close()
		t.Errorf("W5-X5(i) (m5-X5): Open SUCCEEDED over a store stamped schema_version '11' while this binary is %d; the v12 binary must refuse it. The alternative is a clean start and a failure at the first enrolment, with 'no such column' and a person standing at the window", store.SchemaVersion)
	} else if msg := err.Error(); !strings.Contains(msg, "schema_version 11 ") || !strings.Contains(msg, fmt.Sprint(store.SchemaVersion)) || !strings.Contains(msg, "refusing") {
		// Assertion cloned from TestW1V16BinaryRefusesAV15DatabaseAtOpen
		// (w1_version_gate_test.go:36-56), NEVER a type switch:
		// store.SchemaVersionError is the per-envelope REPLICATION refusal
		// (envelope.go:170-174, constructed only at log.go:202) and is NOT on
		// the Open path, so errors.As here books a kill that can never be
		// produced (W5-X5(i), stated in terms).
		t.Errorf("W5-X5(i): the refusal must be a non-nil UNTYPED error naming BOTH versions and the word \"refusing\", got: %v", err)
	}
}
