// w7_projection_test.go — W7's STORE-LEVEL introspection halves (item 9 then
// item 8: the vehicle entity, then the exception through it).
//
// design/WINDOW-BUILD-PLAN-v1.md `### W7 —` at :7421, criteria LOCKED
// 2026-09-03. Written from the CRITERION TEXT ONLY; no built implementation
// was read.
//
// Criteria carried here:
//
//   - W7-7(a) — the `vehicle` and `vehicle_amendment` declared column lists as
//     LITERALS, by introspection (DR-W7-1): vehicle = four, vehicle_amendment
//     = eight, both STRICT, with their primary keys and nullability pinned.
//   - W7-7(b) — no status-shaped column, and INSERT-only triggers present: an
//     attempted UPDATE and an attempted DELETE on `vehicle_amendment` each
//     fail.
//   - W7-7(c) — `retired` asserted ABSENT (DR-W7-8). This asserts the SHAPE
//     the spec specifies; it claims NO ruling on vehicle retirement, and
//     [[D-41]] is cited here for the COMPANY proposition only.
//   - W7-7(d) — `recorded_by` asserted ABSENT on `vehicle`, a DECLARED
//     divergence from `person`/`company`, which both carry it (DR-W7-7).
//   - W7-12(a) — `trip.vehicle_id TEXT NOT NULL`, `trip_arrival.vehicle_id
//     TEXT` nullable (DR-W7-2), with SQL type, nullability and STRICT pinned.
//   - W7-12(b) — STAGE 2: `trip.vehicle_entered` nullable while
//     `trip.vehicle_id` STAYS NOT NULL.
//   - W7-12(c) = W7-X4 — the permitted unions as literals, in W5a-G-2's
//     post-amendment reading (full pre-chunk set plus all declared additions,
//     no scoping by landing order); part (a) carries ANTI-SUBSTITUTION, NOT
//     anti-over-build. ⚠ W7 is the first chunk whose two halves land on
//     different KINDS of table, so the union assertion carries NULLABILITY,
//     not membership alone (W7-X4's own clause).
//   - W7-12(d) — THE GHOST ARMS: `vehicle_number` is not a column anywhere,
//     and `vehicle_entered` is not a column on either new table.
//   - W7-X5 — W7 does NOT bump SchemaVersion, and W7 carries its OWN
//     '9'-edge instance rather than closing the row by ruling, with the
//     TIGHTENED substring ("schema_version 9 ") and the same-artefact
//     PRECONDITION (rule-6: a missing precondition is a FIXTURE DEFECT, never
//     a criterion failure).
//
// package store_test DELIBERATELY (w5a_projection_and_version_test.go's own
// reason, restated because it is load-bearing): these assertions need
// domain.Register to create the projections, and `package store` cannot
// import internal/domain — domain imports store.
//
// ⚠ THIS FILE NAMES NO UNBUILT GO SYMBOL. Every assertion is over SQLite
// introspection or over `store.SchemaVersion`, so the file COMPILES AND RUNS
// against e9cc5f5 and its clean-tree failures are genuine PRODUCED RUNTIME
// REDS rather than one compile error burying the rest. §2.4's THIRD CATEGORY
// (a `vehicle*` symbol reds as a compile error) is thereby avoided wherever
// the criterion permits; see reports/window-build/W7-tests.md §1.
//
// EXPECTED RED at e9cc5f5 (all runtime):
//   - TestW7VehicleProjectionColumnsAreTheDeclaredFour: "`vehicle`: table
//     absent" — §2.1, zero occurrences of the projection anywhere under app/.
//   - TestW7VehicleAmendmentSideTableIsInsertOnlyAndNineColumns: the same.
//   - TestW7RetiredAndRecordedByAreAbsentFromTheVehiclePair: fails on the
//     fixture precondition — an absence asserted over a table that does not
//     exist is VACUOUS, and this test refuses to pass vacuously.
//   - TestW7TripSideVehicleIDColumns: "trip: missing declared column(s)
//     [vehicle_id]" — trip is the pre-chunk THIRTEEN (store.go:195-229).
//   - TestW7TripVehicleEnteredIsNullableAtStageTwo: "trip.vehicle_entered is
//     NOT NULL" — store.go:200 at e9cc5f5.
//   - TestW7PermittedUnionsOverFourTables: the same missing columns, from the
//     union side, plus the nullability limb.
//   - TestW7SchemaVersionIsSixteenAndAV15StoreIsRefused: "FIXTURE DEFECTIVE"
//     — the store under test carries neither `trip.vehicle_id` nor the
//     `vehicle` table, which is W7-X5's rule-6 precondition.
//
// The GHOST-ARM test is ⟦NO PRE-CHUNK RED⟧ — `vehicle_number` is absent
// today — and its evidence is the planted column, applied alone.
//
// KILLER MUTANTS (§5):
//
//	(m7-7)   four arms: a column dropped; renamed; declared NOT NULL; a
//	         payload struct tag misspelled          -> W7-7(a)
//	(m7-7b)  two arms: a status-shaped column added, or an INSERT-only
//	         trigger dropped                        -> W7-7(b)
//	(m7-7c)  `Retired` resurrected on vehicle       -> W7-7(c)
//	(m7-7d)  `recorded_by` added to the vehicle pair-> W7-7(d)
//	(m7-12)  three arms: trip.vehicle_id nullable; trip_arrival.vehicle_id
//	         NOT NULL; trip.vehicle_entered left NOT NULL
//	                                                -> W7-12(a), W7-12(b)
//	(m7-X4)  a column outside the permitted union, on each of four tables
//	                                                -> W7-12(c), W7-7(a)
//	(m7-X5)  a chunk that BUMPS SchemaVersion       -> W7-X5 arm (iii)
//
// ⚠ (m7-X4) IS BOOKED IN TWO `Kills:` LINES (W7-7(a) and W7-12(c)) — ONE
// PLANT WITH TWO DETECTORS, recorded ONCE (W7-X6's ⟦LOCK — ENU-F15⟧ note).
package store_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// --- the frozen literals (DR-W7-1, §7's freeze list) ---------------------
//
// ⚠ These are B-2 freeze-list literals, not a free choice. §7 rows 1-11 send
// them to gate B-2's row at plan:101, and rows 7 and 8 are REMOVE-category
// rows — a spelling that must NOT appear.

const (
	w7VehicleTable   = "vehicle"
	w7AmendmentTable = "vehicle_amendment"
)

// w7VehicleColumns is DR-W7-1's FOUR, in the spec of record's own DDL order
// (analytics-v1.md:525-530, quoted at §1 correction (2)).
//
// ⚠ `site_id` is the one CONSOLIDATED's "nothing else" reads as excluding.
// It is structural attribution under [[D-5]] — cited for "attribution is by
// post", which its text says, and NOT for "therefore vehicle has a site_id
// column", which is the spec's decision and not James's (DR-W7-1).
var w7VehicleColumns = []string{"vehicle_id", "site_id", "number", "role"}

// w7AmendmentColumns is DR-W7-1's EIGHT plus `seen` — NINE.
// ⟦SchemaVersion 11, 2026-09-06⟧ `seen` is ADDED ([[D-92]], step 3 of the
// increment — the causal reference cloned onto vehicle third, after company
// and trade): the author's seen-list, an immutable reference written at
// authoring, not a status; W7-7(b)'s status-shaped set below still refuses
// one. ⟦was: "DR-W7-1's EIGHT" — "…\"same_as\", \"cleared\","⟧
var w7AmendmentColumns = []string{
	"site_id", "node_epoch", "seq", "vehicle_id",
	"number", "role", "same_as", "cleared", "seen",
}

// w7ForbiddenOnVehiclePair is §7's REMOVE category, rows 7, 8 and 9, plus
// W7-7(b)'s status-shaped set.
//
// ⚠ `recorded_by` is here as a DECLARED ASYMMETRY, not as a typo: `person`
// and `company` BOTH carry the column and `PersonCreated`/`CompanyCreated`
// both carry `RecordedBy` (DR-W7-7). ⚠ `retired` is here because
// `person_amendment` DOES carry one (person_amend.go:151, §7 row 8), so the
// clone source contains the field W7 must not copy.
var w7ForbiddenOnVehiclePair = []string{
	"recorded_by", "retired",
	"status", "superseded", "effective",
	"vehicle_entered", "vehicle_number", "vehicle_fold_key",
}

// w7StatusShaped is W7-7(b)'s own set, kept separate from the REMOVE rows so
// a red names which rule it broke.
var w7StatusShaped = []string{"status", "superseded", "effective"}

// --- W7-12(c) / W7-X4's permitted unions --------------------------------

// w7TripPreChunkColumns is the MEASURED pre-chunk `trip` column set
// (store.go:195-229 at e9cc5f5) — W7-12(c)'s base, THIRTEEN.
var w7TripPreChunkColumns = []string{
	"trip_id", "origin_site", "node_epoch", "seq", "vehicle_entered",
	"driver_person_id", "destination_site", "destination_uncontrolled",
	"deadline_minutes", "phase_at_dispatch", "phase_override_reason",
	"cargo_note", "recorded_by",
}

// w7TripStruckByRuling are pre-chunk `trip` columns later STRUCK by a ruling
// rather than by any chunk: phase_at_dispatch and phase_override_reason went
// with weather phases (James, 2026-10-04). The measured pre-chunk set above is
// a historical record and stays as measured; the live table is counted
// without these two.
var w7TripStruckByRuling = []string{"phase_at_dispatch", "phase_override_reason"}

// w7TripArrivalPreChunkColumns is the MEASURED pre-chunk `trip_arrival`
// column set (trip.go:395-404 at e9cc5f5) — EIGHT.
var w7TripArrivalPreChunkColumns = []string{
	"arrival_id", "site_id", "node_epoch", "seq", "trip_ref",
	"vehicle_entered", "origin_site", "recorded_by",
}

// w7DeclaredAddition is W7's declared addition to BOTH trip-side tables:
// exactly one column, the same spelling on both sides deliberately (§7 row 2,
// "unlike W6's person_name/person_snapshot_name split").
const w7DeclaredAddition = "vehicle_id"

// r17DeclaredAddition (SchemaVersion 13, R-17, "unidentified vehicle"): the
// ONE column that ruling adds, and it lands on `trip` ONLY — `trip_arrival`
// carries no vehicle_unidentified counterpart FOR THAT RULING, because a
// check-in's degenerate trip is authored through the SAME dispatchTrip core
// as a departure (this is exactly R-17's "one choke point" framing), so
// nothing about the arrival event itself needed to say it separately.
//
// ⚠⚠ SUPERSEDED IN PART BY R-18 (SchemaVersion 14, 2026-09-13): R-17 was
// stated GENERALLY ("there are no doors only vehicles") but its own build
// measured only the four named doors dispatchTrip serves — the unsolicited-
// arrival door (RecordUnsolicitedArrivalOriented) is a SEPARATE authoring
// path that never touches `trip` at all, so it was never covered by this
// reasoning and never put to James until R-18. See r18DeclaredAddition
// below for trip_arrival's own, independently-declared column.
const r17DeclaredAddition = "vehicle_unidentified"

// r18DeclaredAddition (SchemaVersion 14, R-18, "the unsolicited-arrival
// door is part of R-17"): trip_arrival's OWN vehicle_unidentified column,
// same spelling as trip's (r17DeclaredAddition) because it is the identical
// fact about a truck, seen from the arrival side rather than the departure
// side — distinct constant so this file's own census can name which ruling
// declared which table's column, on r17DeclaredAddition's own precedent for
// the split.
const r18DeclaredAddition = "vehicle_unidentified"

var (
	// FIFTEEN (FOURTEEN at W7, +1 at R-17).
	w7TripPermittedUnion = append(append(append([]string{}, w7TripPreChunkColumns...), w7DeclaredAddition), r17DeclaredAddition)
	// TEN (NINE at W7, +1 at R-18).
	w7TripArrivalPermittedUnion = append(append(append([]string{}, w7TripArrivalPreChunkColumns...), w7DeclaredAddition), r18DeclaredAddition)
)

// --- introspection machinery (NO assertions) ----------------------------
//
// W7 mints its own rather than calling W6's or W5a's: W7-X3 forbids another
// chunk's green counting toward W7's, and a helper shared across chunk
// boundaries is a dependency that can be deleted out from under it.

func w7OpenRaw(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

type w7Col struct {
	Name     string
	Type     string
	NotNull  bool
	HasDflt  bool
	DfltText string
	PKPos    int // 0 = not part of the primary key; 1..n = position within it
}

func w7TableInfo(t *testing.T, db *sql.DB, table string) []w7Col {
	t.Helper()
	rows, err := db.Query(
		`SELECT name, type, "notnull", dflt_value, pk FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []w7Col
	for rows.Next() {
		var c w7Col
		var nn int
		var dflt sql.NullString
		if err := rows.Scan(&c.Name, &c.Type, &nn, &dflt, &c.PKPos); err != nil {
			t.Fatal(err)
		}
		c.NotNull, c.HasDflt, c.DfltText = nn != 0, dflt.Valid, dflt.String
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func w7ColNames(cols []w7Col) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		out = append(out, c.Name)
	}
	return out
}

func w7Find(cols []w7Col, name string) (w7Col, bool) {
	for _, c := range cols {
		if c.Name == name {
			return c, true
		}
	}
	return w7Col{}, false
}

// w7Missing is part (a)'s ANTI-SUBSTITUTION detector.
func w7Missing(cols []w7Col, want []string) []string {
	var missing []string
	for _, w := range want {
		if _, ok := w7Find(cols, w); !ok {
			missing = append(missing, w)
		}
	}
	return missing
}

// w7OutsideUnion is part (b)'s detector — shared by the control and the real
// assertion, so the control exercises the same code.
func w7OutsideUnion(cols []w7Col, permitted []string) []string {
	allowed := make(map[string]bool, len(permitted))
	for _, p := range permitted {
		allowed[p] = true
	}
	var stray []string
	for _, c := range cols {
		if !allowed[c.Name] {
			stray = append(stray, c.Name)
		}
	}
	sort.Strings(stray)
	return stray
}

// w7Present reports which of `forbidden` appear on the table — W7-7(c)/(d)'s
// and W7-12(d)'s detector.
func w7Present(cols []w7Col, forbidden []string) []string {
	var found []string
	for _, f := range forbidden {
		if _, ok := w7Find(cols, f); ok {
			found = append(found, f)
		}
	}
	sort.Strings(found)
	return found
}

func w7TableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

func w7TableDDL(t *testing.T, db *sql.DB, table string) string {
	t.Helper()
	var ddl string
	if err := db.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&ddl); err != nil {
		t.Fatalf("reading DDL for %s: %v", table, err)
	}
	return ddl
}

// w7StrictBehaviourHolds is the BEHAVIOURAL half of "STRICT preserved": in a
// STRICT table a BLOB cannot be stored in a TEXT column. The probe value is a
// BLOB deliberately — TEXT affinity converts a numeric value to text, so an
// INTEGER probe reports a STRICT table as loose (W5a's file records paying
// for that mistake). The control below proves the discrimination.
func w7StrictBehaviourHolds(t *testing.T, db *sql.DB, table, pkCol, textCol string) bool {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	_, err = tx.Exec(fmt.Sprintf(
		`INSERT INTO %s (%s, %s) VALUES ('w7-strict-probe', x'00ff')`, table, pkCol, textCol))
	return err != nil
}

// w7RegisteredDB opens a store, registers the domain projections, closes it,
// and returns a raw handle on the same file. Registration is what creates the
// projections — a bare store.Open would report every projection absent and
// the assertion would be about the wrong artefact.
func w7RegisteredDB(t *testing.T, name string) *sql.DB {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatal(err)
	}
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	path := st.Path
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	return w7OpenRaw(t, path)
}

// =========================================================================
// W7-7(a) — the vehicle projection's declared column list, as literals
// =========================================================================
//
// EXPECTED RED at e9cc5f5 (runtime): "`vehicle`: table absent".
func TestW7VehicleProjectionColumnsAreTheDeclaredFour(t *testing.T) {
	// --- CONTROL FIRST (W7-X6(a)). Every tooth of the detector is shown
	// biting a PLANTED violation on a scratch table before the real table is
	// looked at. One plant against a four-tooth detector is a green ledger
	// row proving nothing.
	ctl := w7OpenRaw(t, filepath.Join(t.TempDir(), "w7-vehicle-control.db"))

	// (a1) (m7-7) arm 1 — a column DROPPED.
	if _, err := ctl.Exec(`CREATE TABLE vehicle_ctl_dropped (
		vehicle_id TEXT PRIMARY KEY, site_id TEXT NOT NULL, number TEXT
	) STRICT`); err != nil {
		t.Fatal(err)
	}
	if miss := w7Missing(w7TableInfo(t, ctl, "vehicle_ctl_dropped"), w7VehicleColumns); len(miss) != 1 || miss[0] != "role" {
		t.Fatalf("W7-7(a) CONTROL FAILED (a1): the missing-column detector reported %v on a table with `role` dropped, "+
			"want exactly [role] — a detector that cannot see its own plant proves nothing ((m7-7) arm 1)", miss)
	}

	// (a2) (m7-7) arm 2 — a column RENAMED. The rename is caught twice over:
	// once as a missing declared column and once as a stray outside the
	// union, and both teeth are exercised because a renamed column is
	// invisible to whichever tooth is relaxed.
	if _, err := ctl.Exec(`CREATE TABLE vehicle_ctl_renamed (
		vehicle_id TEXT PRIMARY KEY, site_id TEXT NOT NULL, number TEXT, vehicle_role TEXT
	) STRICT`); err != nil {
		t.Fatal(err)
	}
	renamed := w7TableInfo(t, ctl, "vehicle_ctl_renamed")
	if miss := w7Missing(renamed, w7VehicleColumns); len(miss) != 1 || miss[0] != "role" {
		t.Fatalf("W7-7(a) CONTROL FAILED (a2, anti-substitution): reported %v, want [role] ((m7-7) arm 2)", miss)
	}
	if stray := w7OutsideUnion(renamed, w7VehicleColumns); len(stray) != 1 || stray[0] != "vehicle_role" {
		t.Fatalf("W7-7(a) CONTROL FAILED (a2, union): reported %v, want [vehicle_role] ((m7-X4) on `vehicle`)", stray)
	}

	// (a3) (m7-7) arm 3 — a column declared NOT NULL where the spec says
	// nullable. DR-W7-1: `number` and `role` are "nullable with no default".
	if _, err := ctl.Exec(`CREATE TABLE vehicle_ctl_notnull (
		vehicle_id TEXT PRIMARY KEY, site_id TEXT NOT NULL, number TEXT NOT NULL, role TEXT
	) STRICT`); err != nil {
		t.Fatal(err)
	}
	if c, ok := w7Find(w7TableInfo(t, ctl, "vehicle_ctl_notnull"), "number"); !ok || !c.NotNull {
		t.Fatalf("W7-7(a) CONTROL FAILED (a3): the nullability tooth did not see a planted NOT NULL on `number` "+
			"((m7-7) arm 3) — got %+v, ok=%v", c, ok)
	}

	// (a4) the STRICT probe DISCRIMINATES. A probe that reports every table
	// as STRICT would pass the real assertion by construction.
	if _, err := ctl.Exec(`CREATE TABLE vehicle_ctl_loose (
		vehicle_id TEXT PRIMARY KEY, site_id TEXT NOT NULL, number TEXT, role TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	if w7StrictBehaviourHolds(t, ctl, "vehicle_ctl_loose", "vehicle_id", "site_id") {
		t.Fatal("W7-7(a) CONTROL FAILED (a4): the STRICT probe reported a NON-STRICT table as STRICT — " +
			"a probe that cannot tell them apart makes the real assertion vacuous")
	}
	if _, err := ctl.Exec(`CREATE TABLE vehicle_ctl_strict (
		vehicle_id TEXT PRIMARY KEY, site_id TEXT NOT NULL, number TEXT, role TEXT
	) STRICT`); err != nil {
		t.Fatal(err)
	}
	if !w7StrictBehaviourHolds(t, ctl, "vehicle_ctl_strict", "vehicle_id", "site_id") {
		t.Fatal("W7-X6(a) CONTROL FAILED (a4, negative arm): the STRICT probe reported a STRICT table as loose — " +
			"a probe that flags the correct build is a probe a builder will weaken")
	}

	// --- THE ASSERTION ---------------------------------------------------
	db := w7RegisteredDB(t, "w7-vehicle.db")
	if !w7TableExists(t, db, w7VehicleTable) {
		t.Fatalf("W7-7(a): `%s`: table absent. DR-W7-1 declares it with EXACTLY %v "+
			"(analytics-v1.md:525-530, the spec of record). Pre-chunk this is §2.1's greenfield finding.",
			w7VehicleTable, w7VehicleColumns)
	}
	cols := w7TableInfo(t, db, w7VehicleTable)

	if miss := w7Missing(cols, w7VehicleColumns); len(miss) > 0 {
		t.Errorf("W7-7(a) (m7-7) arms 1/2: `%s` is missing declared column(s) %v — declared set is %v, got %v",
			w7VehicleTable, miss, w7VehicleColumns, w7ColNames(cols))
	}
	// ⚠ part (a) carries ANTI-SUBSTITUTION; part (b)'s union carries the
	// anti-over-build half, and W7-X4 records that part (a) does NOT
	// (plan:3163-3164, inherited with its recorded loss).
	if stray := w7OutsideUnion(cols, w7VehicleColumns); len(stray) > 0 {
		t.Errorf("W7-X4 / (m7-X4) on `%s`: column(s) outside the permitted union: %v (union is %v)",
			w7VehicleTable, stray, w7VehicleColumns)
	}
	if got, want := len(cols), len(w7VehicleColumns); got != want {
		t.Errorf("W7-7(a): `%s` declares %d columns, want %d — CONSOLIDATED's \"nothing else\" reads as a "+
			"THREE-column table and a criterion asserting three would be RED against the correct build (§1 correction (2))",
			w7VehicleTable, got, want)
	}

	// vehicle_id PRIMARY KEY.
	if c, ok := w7Find(cols, "vehicle_id"); ok && c.PKPos != 1 {
		t.Errorf("W7-7(a): `%s.vehicle_id` has primary-key position %d, want 1 (DR-W7-1: PRIMARY KEY)",
			w7VehicleTable, c.PKPos)
	}
	// site_id NOT NULL.
	if c, ok := w7Find(cols, "site_id"); ok && !c.NotNull {
		t.Errorf("W7-7(a): `%s.site_id` is nullable, want NOT NULL — it is structural attribution under [[D-5]] "+
			"and both sibling reference projections carry it that way (DR-W7-1)", w7VehicleTable)
	}
	// number and role nullable, with NO default.
	for _, name := range []string{"number", "role"} {
		c, ok := w7Find(cols, name)
		if !ok {
			continue
		}
		if c.NotNull {
			t.Errorf("W7-7(a) (m7-7) arm 3: `%s.%s` is NOT NULL, want nullable — item 9 stores it "+
				"\"nullable verbatim\" and item 8's whole subject is its absence", w7VehicleTable, name)
		}
		if c.HasDflt {
			t.Errorf("W7-7(a): `%s.%s` carries a default (%q), want none — a default is a value nobody typed",
				w7VehicleTable, name, c.DfltText)
		}
		if !strings.EqualFold(c.Type, "TEXT") {
			t.Errorf("W7-7(a): `%s.%s` is declared %q, want TEXT", w7VehicleTable, name, c.Type)
		}
	}

	if !w7StrictBehaviourHolds(t, db, w7VehicleTable, "vehicle_id", "site_id") {
		t.Errorf("W7-7(a): `%s` is not STRICT — a BLOB was accepted into a TEXT column. DDL:\n%s",
			w7VehicleTable, w7TableDDL(t, db, w7VehicleTable))
	}
}

// =========================================================================
// W7-7(a)+(b) — the side table: eight columns, STRICT, composite PK, and
// INSERT-ONLY BY TRIGGER
// =========================================================================
//
// The person-side shape to reproduce is person_amend.go:132-166 (two
// triggers, with their own abort messages).
//
// EXPECTED RED at e9cc5f5 (runtime): "`vehicle_amendment`: table absent".
// ⟦was: TestW7VehicleAmendmentSideTableIsInsertOnlyAndEightColumns — renamed 2026-09-06: step 3 added `seen`, so
// w7AmendmentColumns is DR-W7-1's EIGHT plus one = NINE. The literal was already
// correct; only this name said eight. Test-only rename, no DECLARED-REDS row.⟧
func TestW7VehicleAmendmentSideTableIsInsertOnlyAndNineColumns(t *testing.T) {
	// --- CONTROL FIRST: the trigger detector must see a table with NO
	// triggers as mutable. (m7-7b) arm 2 is exactly "an INSERT-only trigger
	// dropped", and a detector that reports every table as guarded would
	// pass the real assertion by construction.
	ctl := w7OpenRaw(t, filepath.Join(t.TempDir(), "w7-amend-control.db"))
	if _, err := ctl.Exec(`CREATE TABLE va_ctl (
		site_id TEXT NOT NULL, node_epoch INTEGER NOT NULL, seq INTEGER NOT NULL,
		vehicle_id TEXT NOT NULL, number TEXT, role TEXT, same_as TEXT, cleared TEXT,
		PRIMARY KEY (site_id, node_epoch, seq)
	) STRICT`); err != nil {
		t.Fatal(err)
	}
	if _, err := ctl.Exec(`INSERT INTO va_ctl (site_id, node_epoch, seq, vehicle_id) VALUES ('site-a', 1, 1, 'v-1')`); err != nil {
		t.Fatal(err)
	}
	if updErr, delErr := w7MutationErrors(t, ctl, "va_ctl"); updErr != nil || delErr != nil {
		t.Fatalf("W7-7(b) CONTROL FAILED: the trigger detector reported an UNGUARDED table as INSERT-only "+
			"(update=%v delete=%v) — (m7-7b) arm 2 could not be produced against it", updErr, delErr)
	}
	// The positive half of the same control: with the two triggers present,
	// the detector must see BOTH mutations abort. A one-sided control on a
	// boolean judgement discriminates nothing (W5d-G-2 item 1's reasoning,
	// applied here as W7-31(a) applies it to gating).
	for _, ddl := range []string{
		`CREATE TRIGGER va_ctl_no_update BEFORE UPDATE ON va_ctl BEGIN SELECT RAISE(ABORT, 'w7 control: insert-only'); END`,
		`CREATE TRIGGER va_ctl_no_delete BEFORE DELETE ON va_ctl BEGIN SELECT RAISE(ABORT, 'w7 control: insert-only'); END`,
	} {
		if _, err := ctl.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	if updErr, delErr := w7MutationErrors(t, ctl, "va_ctl"); updErr == nil || delErr == nil {
		t.Fatalf("W7-7(b) CONTROL FAILED (negative arm): the trigger detector reported a GUARDED table as mutable "+
			"(update=%v delete=%v) — a detector that flags the correct build is one a builder will weaken", updErr, delErr)
	}

	// --- THE ASSERTION ---------------------------------------------------
	db := w7RegisteredDB(t, "w7-amendment.db")
	if !w7TableExists(t, db, w7AmendmentTable) {
		t.Fatalf("W7-7(a): `%s`: table absent. DR-W7-1 declares it with EXACTLY %v, PK (site_id, node_epoch, seq), STRICT.",
			w7AmendmentTable, w7AmendmentColumns)
	}
	cols := w7TableInfo(t, db, w7AmendmentTable)

	if miss := w7Missing(cols, w7AmendmentColumns); len(miss) > 0 {
		t.Errorf("W7-7(a) (m7-7): `%s` is missing declared column(s) %v — got %v",
			w7AmendmentTable, miss, w7ColNames(cols))
	}
	if stray := w7OutsideUnion(cols, w7AmendmentColumns); len(stray) > 0 {
		t.Errorf("W7-X4 / (m7-X4) on `%s`: column(s) outside the permitted union: %v",
			w7AmendmentTable, stray)
	}
	if got, want := len(cols), len(w7AmendmentColumns); got != want {
		t.Errorf("W7-7(a): `%s` declares %d columns, want %d (%v)", w7AmendmentTable, got, want, w7AmendmentColumns)
	}

	// The composite primary key, IN ORDER — a PK of the same three columns in
	// a different order is a different index and a different uniqueness
	// guarantee.
	wantPK := []string{"site_id", "node_epoch", "seq"}
	var gotPK []string
	for pos := 1; pos <= len(wantPK); pos++ {
		for _, c := range cols {
			if c.PKPos == pos {
				gotPK = append(gotPK, c.Name)
			}
		}
	}
	if strings.Join(gotPK, ",") != strings.Join(wantPK, ",") {
		t.Errorf("W7-7(a): `%s` primary key is (%s), want (%s) — the person-side shape to reproduce is "+
			"person_amend.go:132-166", w7AmendmentTable, strings.Join(gotPK, ","), strings.Join(wantPK, ","))
	}

	// W7-7(b): no status-shaped column. §2.4 row 8 records this half as
	// ALREADY TRUE pre-chunk (no status-shaped column exists on any side
	// table), so its evidence is the planted column — (m7-7b) arm 1 — which
	// the union tooth above catches, and the control at (a2) produced.
	if found := w7Present(cols, w7StatusShaped); len(found) > 0 {
		t.Errorf("W7-7(b) (m7-7b) arm 1: `%s` carries status-shaped column(s) %v — the side table is INSERT-only "+
			"and a stored status stamped at write is `W3-m22`'s shape (booked ONCE, at W7-24)",
			w7AmendmentTable, found)
	}

	// W7-7(b): the two INSERT-only triggers, asserted BEHAVIOURALLY. A
	// trigger asserted by name in sqlite_master is satisfied by a trigger
	// whose body does nothing.
	if _, err := db.Exec(`INSERT INTO ` + w7AmendmentTable +
		` (site_id, node_epoch, seq, vehicle_id) VALUES ('site-a', 1, 1, 'v-1')`); err != nil {
		t.Fatalf("W7-7(b): could not seed a row to test the triggers against: %v", err)
	}
	updErr, delErr := w7MutationErrors(t, db, w7AmendmentTable)
	if updErr == nil {
		t.Errorf("W7-7(b) (m7-7b) arm 2: an UPDATE on `%s` SUCCEEDED — the side table must be INSERT-only by "+
			"TRIGGER, not by policy", w7AmendmentTable)
	}
	if delErr == nil {
		t.Errorf("W7-7(b) (m7-7b) arm 2: a DELETE on `%s` SUCCEEDED — the side table must be INSERT-only by "+
			"TRIGGER, not by policy", w7AmendmentTable)
	}
}

// w7MutationErrors returns the errors an UPDATE and a DELETE produce on
// `table`, each inside its own rolled-back transaction. Shared by the control
// and the assertion so the control exercises the same code.
func w7MutationErrors(t *testing.T, db *sql.DB, table string) (updErr, delErr error) {
	t.Helper()
	run := func(stmt string) error {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		_, execErr := tx.Exec(stmt)
		return execErr
	}
	return run(fmt.Sprintf(`UPDATE %s SET number = 'w7-probe'`, table)),
		run(fmt.Sprintf(`DELETE FROM %s`, table))
}

// =========================================================================
// W7-7(c) and W7-7(d) — the two asserted ABSENCES, planted separately
// =========================================================================
//
// ⚠ W7-7(c) asserts the SHAPE the spec specifies and does NOT claim a ruling
// on vehicle retirement. [[D-41]] is cited here for the COMPANY proposition
// only; [[D-65]] rules trucks are not REMOVABLE FROM THE PICKER, which is a
// different artefact from a `retired` column and is not cited as authority
// for this assertion (DR-W7-8).
//
// EXPECTED RED at e9cc5f5 (runtime): the FIXTURE PRECONDITION fails —
// an absence asserted over a table that does not exist is VACUOUS, and §2.4
// row 10 records that "a vacuous predicate's clean pass is worth less than an
// already-true one's". This test refuses to pass vacuously.
func TestW7RetiredAndRecordedByAreAbsentFromTheVehiclePair(t *testing.T) {
	// --- CONTROL FIRST: the absence detector must SEE a planted column.
	// Two plants, applied one at a time — (m7-7c) and (m7-7d) are separate
	// register entries and a single plant would evidence only one.
	ctl := w7OpenRaw(t, filepath.Join(t.TempDir(), "w7-absence-control.db"))
	if _, err := ctl.Exec(`CREATE TABLE vehicle_ctl_retired (
		vehicle_id TEXT PRIMARY KEY, site_id TEXT NOT NULL, number TEXT, role TEXT, retired INTEGER
	) STRICT`); err != nil {
		t.Fatal(err)
	}
	if got := w7Present(w7TableInfo(t, ctl, "vehicle_ctl_retired"), w7ForbiddenOnVehiclePair); len(got) != 1 || got[0] != "retired" {
		t.Fatalf("W7-7(c) CONTROL FAILED: the absence detector reported %v on a table carrying a planted "+
			"`retired`, want exactly [retired] ((m7-7c))", got)
	}
	if _, err := ctl.Exec(`CREATE TABLE vehicle_ctl_recordedby (
		vehicle_id TEXT PRIMARY KEY, site_id TEXT NOT NULL, number TEXT, role TEXT, recorded_by TEXT
	) STRICT`); err != nil {
		t.Fatal(err)
	}
	if got := w7Present(w7TableInfo(t, ctl, "vehicle_ctl_recordedby"), w7ForbiddenOnVehiclePair); len(got) != 1 || got[0] != "recorded_by" {
		t.Fatalf("W7-7(d) CONTROL FAILED: the absence detector reported %v on a table carrying a planted "+
			"`recorded_by`, want exactly [recorded_by] ((m7-7d))", got)
	}
	// The negative arm: a CORRECT vehicle table must produce an empty set. A
	// detector that flags the right build is one a builder will weaken.
	if _, err := ctl.Exec(`CREATE TABLE vehicle_ctl_clean (
		vehicle_id TEXT PRIMARY KEY, site_id TEXT NOT NULL, number TEXT, role TEXT
	) STRICT`); err != nil {
		t.Fatal(err)
	}
	if got := w7Present(w7TableInfo(t, ctl, "vehicle_ctl_clean"), w7ForbiddenOnVehiclePair); len(got) != 0 {
		t.Fatalf("W7-7(c)/(d) CONTROL FAILED (negative arm): the absence detector flagged %v on the CORRECT "+
			"four-column shape", got)
	}

	// --- THE ASSERTION, with its precondition stated as a FIXTURE fault ---
	db := w7RegisteredDB(t, "w7-absences.db")
	for _, table := range []string{w7VehicleTable, w7AmendmentTable} {
		if !w7TableExists(t, db, table) {
			t.Fatalf("W7-7(c)/(d) FIXTURE DEFECTIVE: `%s` does not exist, so every absence below would hold "+
				"VACUOUSLY. §2.4 row 10: a vacuous predicate's clean pass cannot distinguish \"built and clean\" "+
				"from \"there was nothing to look at\".", table)
		}
		cols := w7TableInfo(t, db, table)
		if got := w7Present(cols, []string{"retired"}); len(got) > 0 {
			t.Errorf("W7-7(c) (m7-7c): `%s` carries `retired`. DR-W7-8 asserts it ABSENT — from the table, from the "+
				"amendable-fact set and from every authoring surface. ⚠ `person_amendment` DOES carry one "+
				"(person_amend.go:151), so the clone source contains the field W7 must not copy (§7 row 8).", table)
		}
		if got := w7Present(cols, []string{"recorded_by"}); len(got) > 0 {
			t.Errorf("W7-7(d) (m7-7d): `%s` carries `recorded_by`. DR-W7-7 asserts it ABSENT — a DECLARED "+
				"divergence from `person`/`company`, which both carry it. It is on §7's freeze list (row 7) in its "+
				"own row so the B-2 review sees a decision rather than a typo.", table)
		}
	}
}

// =========================================================================
// W7-12(a) — the trip-side columns
// =========================================================================
//
// ⚠ Three separate assertions, and the plan's W7 text carries none of them: a
// NOT NULL column here, no foreign key (W7-6(a), w7_source_scans_test.go),
// and a fold that never refuses (W7-6(b), the domain side).
//
// EXPECTED RED at e9cc5f5 (runtime): "trip: missing declared column(s)
// [vehicle_id]" and the same on trip_arrival.
func TestW7TripSideVehicleIDColumns(t *testing.T) {
	// --- CONTROL FIRST: the nullability tooth must discriminate in BOTH
	// directions, because (m7-12) arms 1 and 2 are OPPOSITE errors on the two
	// tables — nullable where NOT NULL is wanted, and NOT NULL where nullable
	// is wanted. A one-directional control catches only one of them.
	ctl := w7OpenRaw(t, filepath.Join(t.TempDir(), "w7-tripcols-control.db"))
	if _, err := ctl.Exec(`CREATE TABLE trip_ctl (
		trip_id TEXT PRIMARY KEY, vehicle_id TEXT
	) STRICT`); err != nil {
		t.Fatal(err)
	}
	if c, ok := w7Find(w7TableInfo(t, ctl, "trip_ctl"), "vehicle_id"); !ok || c.NotNull {
		t.Fatalf("W7-12(a) CONTROL FAILED (arm 1): a planted NULLABLE `trip.vehicle_id` was not seen as nullable "+
			"— got %+v ok=%v ((m7-12) arm 1)", c, ok)
	}
	if _, err := ctl.Exec(`CREATE TABLE trip_arrival_ctl (
		arrival_id TEXT PRIMARY KEY, vehicle_id TEXT NOT NULL
	) STRICT`); err != nil {
		t.Fatal(err)
	}
	if c, ok := w7Find(w7TableInfo(t, ctl, "trip_arrival_ctl"), "vehicle_id"); !ok || !c.NotNull {
		t.Fatalf("W7-12(a) CONTROL FAILED (arm 2): a planted NOT NULL `trip_arrival.vehicle_id` was not seen as "+
			"NOT NULL — got %+v ok=%v ((m7-12) arm 2)", c, ok)
	}

	// --- THE ASSERTION ---------------------------------------------------
	db := w7RegisteredDB(t, "w7-tripcols.db")

	trip := w7TableInfo(t, db, "trip")
	c, ok := w7Find(trip, "vehicle_id")
	if !ok {
		t.Errorf("W7-12(a): `trip` is missing declared column `vehicle_id` — got %v. "+
			"PLAN-SEC-A-schema.md:723: \"every dispatch names an entity\".", w7ColNames(trip))
	} else {
		if !c.NotNull {
			t.Errorf("W7-12(a) (m7-12) arm 1: `trip.vehicle_id` is nullable, want TEXT NOT NULL")
		}
		if !strings.EqualFold(c.Type, "TEXT") {
			t.Errorf("W7-12(a): `trip.vehicle_id` is declared %q, want TEXT", c.Type)
		}
	}

	arr := w7TableInfo(t, db, "trip_arrival")
	ac, ok := w7Find(arr, "vehicle_id")
	if !ok {
		t.Errorf("W7-12(a): `trip_arrival` is missing declared column `vehicle_id` — got %v. "+
			"DR-W7-2 (CARRIED, analytics-v1.md:551-556 and PLAN-SEC-A-schema.md:820-822): id-first correlation is "+
			"IMPOSSIBLE without an id on both halves of the comparison.", w7ColNames(arr))
	} else {
		if ac.NotNull {
			t.Errorf("W7-12(a) (m7-12) arm 2: `trip_arrival.vehicle_id` is NOT NULL, want nullable — an arrival " +
				"whose vehicle is not yet known at this post carries no id, which is the partitioned case " +
				"analytics-v1.md:855-871 exists for")
		}
		if !strings.EqualFold(ac.Type, "TEXT") {
			t.Errorf("W7-12(a): `trip_arrival.vehicle_id` is declared %q, want TEXT", ac.Type)
		}
	}

	// STRICT is preserved on both — a rebuild that relaxes the NOT NULL
	// (W7-X1's own subject) is the shape most likely to lose it.
	if !w7StrictBehaviourHolds(t, db, "trip", "trip_id", "origin_site") {
		t.Errorf("W7-12(a): `trip` is no longer STRICT after W7's change")
	}
	if !w7StrictBehaviourHolds(t, db, "trip_arrival", "arrival_id", "site_id") {
		t.Errorf("W7-12(a): `trip_arrival` is no longer STRICT after W7's change")
	}
}

// =========================================================================
// W7-12(b) — STAGE 2: vehicle_entered nullable, vehicle_id STAYS NOT NULL
// =========================================================================
//
// ⚠ This is the pair that must be asserted TOGETHER. The two halves point in
// opposite directions and a build that relaxes both, or neither, satisfies
// either half read alone.
//
// EXPECTED RED at e9cc5f5 (runtime): "trip.vehicle_entered is NOT NULL"
// (store.go:200).
func TestW7TripVehicleEnteredIsNullableAtStageTwo(t *testing.T) {
	db := w7RegisteredDB(t, "w7-stage2.db")
	cols := w7TableInfo(t, db, "trip")

	ve, ok := w7Find(cols, "vehicle_entered")
	if !ok {
		t.Fatalf("W7-12(b) FIXTURE DEFECTIVE: `trip.vehicle_entered` is absent — the column is RELAXED by item 8, "+
			"never removed (DR-W7-4 makes it a derived snapshot, the role `visit.person_snapshot_name` plays for "+
			"people in item 5). Got %v", w7ColNames(cols))
	}
	if ve.NotNull {
		t.Errorf("W7-12(b) (m7-12) arm 3: `trip.vehicle_entered` is NOT NULL. Item 8 makes it nullable — " +
			"PLAN-SEC-A-schema.md:719-723: its meaning narrows to \"the number as at dispatch\", " +
			"\"honestly NULL when the vehicle has no number\".")
	}

	vid, ok := w7Find(cols, "vehicle_id")
	if !ok {
		t.Errorf("W7-12(b): `trip.vehicle_id` is absent at the stage-2 gate — W7-12(a)'s column must still be there")
	} else if !vid.NotNull {
		t.Errorf("W7-12(b): `trip.vehicle_id` became nullable. It STAYS NOT NULL: PLAN-SEC-A-schema.md:723, " +
			"\"every dispatch names an entity\". ⚠ Relaxing BOTH is the shape that reads as \"item 8 made the " +
			"vehicle optional\" — item 8 makes the NUMBER optional, not the vehicle (W7-28's split clause).")
	}
}

// =========================================================================
// W7-12(c) = W7-X4 — the permitted unions over FOUR tables, WITH nullability
// =========================================================================
//
// ⚠ W7-X4's own clause: "A part-(b) assertion that reads only the column
// NAMES would pass a build that kept `vehicle_entered` NOT NULL — so the
// union assertion carries NULLABILITY, not membership alone."
//
// EXPECTED RED at e9cc5f5 (runtime): the same missing columns from the union
// side, plus the nullability limb on trip.vehicle_entered.
func TestW7PermittedUnionsOverFourTables(t *testing.T) {
	// --- CONTROL FIRST (m7-X4): a fifteenth `trip` column is seen as stray.
	ctl := w7OpenRaw(t, filepath.Join(t.TempDir(), "w7-union-control.db"))
	if _, err := ctl.Exec(`CREATE TABLE trip_ctl_over (
		trip_id TEXT PRIMARY KEY, origin_site TEXT, node_epoch INTEGER, seq INTEGER,
		vehicle_entered TEXT, driver_person_id TEXT, destination_site TEXT,
		destination_uncontrolled TEXT, deadline_minutes INTEGER, phase_at_dispatch INTEGER,
		phase_override_reason TEXT, cargo_note TEXT, recorded_by TEXT, vehicle_id TEXT,
		vehicle_number TEXT
	) STRICT`); err != nil {
		t.Fatal(err)
	}
	if stray := w7OutsideUnion(w7TableInfo(t, ctl, "trip_ctl_over"), w7TripPermittedUnion); len(stray) != 1 || stray[0] != "vehicle_number" {
		t.Fatalf("W7-X4 CONTROL FAILED: the union detector reported %v on a table carrying a fifteenth column, "+
			"want exactly [vehicle_number] ((m7-X4) on `trip`)", stray)
	}
	// The NEGATIVE arm — the correct fourteen must NOT be flagged
	// (W5d-G-2 item 3's shape). A union detector that flags the right build
	// is a detector a builder widens until it is green.
	if _, err := ctl.Exec(`CREATE TABLE trip_ctl_exact (
		trip_id TEXT PRIMARY KEY, origin_site TEXT, node_epoch INTEGER, seq INTEGER,
		vehicle_entered TEXT, driver_person_id TEXT, destination_site TEXT,
		destination_uncontrolled TEXT, deadline_minutes INTEGER, phase_at_dispatch INTEGER,
		phase_override_reason TEXT, cargo_note TEXT, recorded_by TEXT, vehicle_id TEXT
	) STRICT`); err != nil {
		t.Fatal(err)
	}
	if stray := w7OutsideUnion(w7TableInfo(t, ctl, "trip_ctl_exact"), w7TripPermittedUnion); len(stray) != 0 {
		t.Fatalf("W7-X4 CONTROL FAILED (negative arm): the union detector flagged %v on the CORRECT fourteen", stray)
	}

	// --- THE ASSERTION, four tables ---------------------------------------
	db := w7RegisteredDB(t, "w7-unions.db")

	for _, tc := range []struct {
		table     string
		union     []string
		declared  []string
		wantCount int
	}{
		// ⟦was: wantCount len(w7TripPermittedUnion) — less the two columns
		// struck with weather phases, James 2026-10-04⟧
		{"trip", w7TripPermittedUnion, []string{w7DeclaredAddition, r17DeclaredAddition}, len(w7TripPermittedUnion) - len(w7TripStruckByRuling)},
		{"trip_arrival", w7TripArrivalPermittedUnion, []string{w7DeclaredAddition, r18DeclaredAddition}, len(w7TripArrivalPermittedUnion)},
		{w7VehicleTable, w7VehicleColumns, w7VehicleColumns, len(w7VehicleColumns)},
		{w7AmendmentTable, w7AmendmentColumns, w7AmendmentColumns, len(w7AmendmentColumns)},
	} {
		if !w7TableExists(t, db, tc.table) {
			t.Errorf("W7-X4 part (b): `%s`: table absent — the union is asserted over a table that does not exist",
				tc.table)
			continue
		}
		cols := w7TableInfo(t, db, tc.table)
		if miss := w7Missing(cols, tc.declared); len(miss) > 0 {
			t.Errorf("W7-X4 part (a) (ANTI-SUBSTITUTION): `%s` is missing declared column(s) %v — got %v",
				tc.table, miss, w7ColNames(cols))
		}
		if stray := w7OutsideUnion(cols, tc.union); len(stray) > 0 {
			t.Errorf("W7-X4 part (b) (m7-X4): `%s` carries column(s) outside the permitted union: %v. "+
				"The union is the full pre-chunk set plus all declared additions, with NO scoping by landing "+
				"order (W5a-G-2's post-amendment reading).", tc.table, stray)
		}
		if got := len(cols); got != tc.wantCount {
			t.Errorf("W7-X4: `%s` declares %d columns, want %d", tc.table, got, tc.wantCount)
		}
	}

	// --- W7-X4's NULLABILITY LIMB, which membership alone cannot see ------
	//
	// ⚠ "W7 is the first chunk where the two halves land on different KINDS
	// of table" — for `vehicle`/`vehicle_amendment` part (b) is trivially
	// their own CREATE literal; for `trip` and `trip_arrival` it is a real
	// union over a pre-existing set, and `trip` is ALSO the table whose NOT
	// NULL stage 2 relaxes.
	type nullPin struct {
		table, column string
		wantNotNull   bool
		why           string
	}
	for _, p := range []nullPin{
		{"trip", "vehicle_id", true, "PLAN-SEC-A-schema.md:723, \"every dispatch names an entity\""},
		{"trip", "vehicle_entered", false, "item 8: honestly NULL when the vehicle has no number"},
		{"trip_arrival", "vehicle_id", false, "DR-W7-2: nullable is the ruled shape"},
		{w7VehicleTable, "number", false, "item 9 stores it nullable verbatim"},
		{w7VehicleTable, "role", false, "item 9 stores it nullable"},
		{w7VehicleTable, "site_id", true, "DR-W7-1: structural attribution, NOT NULL"},
	} {
		if !w7TableExists(t, db, p.table) {
			continue
		}
		c, ok := w7Find(w7TableInfo(t, db, p.table), p.column)
		if !ok {
			continue // already reported by the membership limb above
		}
		if c.NotNull != p.wantNotNull {
			t.Errorf("W7-X4 (nullability limb): `%s.%s` NotNull=%v, want %v — %s",
				p.table, p.column, c.NotNull, p.wantNotNull, p.why)
		}
	}
}

// =========================================================================
// W7-12(d) — THE GHOST ARMS
// =========================================================================
//
// The `ref_alias_created` ghost shape, which W6 met one table over: a
// rejected spelling must not appear beside the accepted one. §7 row 9 is the
// REMOVE-category row that carries it to B-2.
//
// ⟦NO PRE-CHUNK RED⟧ — `vehicle_number` is absent from every table today, so
// this test's evidence is the PLANT below and never its clean pass. The
// property is DERIVED over every table in the schema rather than checked on a
// hand-kept list of tables: a hand-kept list is short the moment a table is
// added, which is exactly what W7 does twice.
func TestW7GhostSpellingsAppearOnNoTable(t *testing.T) {
	// --- CONTROL FIRST: the whole-schema walk must see a planted ghost.
	ctl := w7OpenRaw(t, filepath.Join(t.TempDir(), "w7-ghost-control.db"))
	if _, err := ctl.Exec(`CREATE TABLE ghost_ctl (id TEXT PRIMARY KEY, vehicle_number TEXT) STRICT`); err != nil {
		t.Fatal(err)
	}
	if hits := w7GhostHits(t, ctl, []string{"vehicle_number"}); len(hits) != 1 {
		t.Fatalf("W7-12(d) CONTROL FAILED: the whole-schema ghost walk reported %v on a schema carrying a planted "+
			"`vehicle_number` column, want exactly one hit", hits)
	}
	if hits := w7GhostHits(t, ctl, []string{"vehicle_fold_key"}); len(hits) != 0 {
		t.Fatalf("W7-12(d) CONTROL FAILED (negative arm): the walk flagged %v for a spelling that is not there", hits)
	}

	// --- THE ASSERTION, derived over every table the schema declares ------
	db := w7RegisteredDB(t, "w7-ghosts.db")

	// `vehicle_number` is not a column ANYWHERE (W7-12(d)).
	if hits := w7GhostHits(t, db, []string{"vehicle_number"}); len(hits) > 0 {
		t.Errorf("W7-12(d): the ghost spelling `vehicle_number` appears at %v. The accepted spellings are "+
			"`vehicle_entered` (the snapshot) and `number` (the vehicle's own column); freezing a rejected "+
			"spelling beside an accepted one is the `ref_alias_created` ghost shape (§7 row 9).", hits)
	}
	// `vehicle_fold_key` is not a column anywhere — W7-11(b), the storage
	// half: "in memory only … It never merges, never correlates, never
	// reaches a database column" (analytics-v1.md:657-661), and
	// NormalizeVehicleID's own comment says "there is deliberately no
	// normalised column anywhere in the schema" (trip.go:71-75).
	if hits := w7GhostHits(t, db, []string{"vehicle_fold_key", "vehicle_norm", "vehicle_normalised", "vehicle_normalized"}); len(hits) > 0 {
		t.Errorf("W7-11(b) (m7-11b): a normalised vehicle form reached storage at %v", hits)
	}
	// `vehicle_entered` is not a column on either NEW table (§7 row 9's
	// other half — the payload-key ghost, asserted on the projection because
	// the projection's tags ARE its column names).
	for _, table := range []string{w7VehicleTable, w7AmendmentTable} {
		if !w7TableExists(t, db, table) {
			continue
		}
		if got := w7Present(w7TableInfo(t, db, table), []string{"vehicle_entered"}); len(got) > 0 {
			t.Errorf("W7-12(d): `%s` carries `vehicle_entered`. It is the TRIP-side snapshot and must not appear "+
				"on the entity or its side table (§7 row 9).", table)
		}
	}
}

// w7GhostHits walks EVERY table sqlite_master declares and returns
// "table.column" for each forbidden spelling found. The list of tables is
// DERIVED, never transcribed: a transcribed list is short the moment a table
// is added, and W7 adds two.
func w7GhostHits(t *testing.T, db *sql.DB, forbidden []string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		tables = append(tables, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(tables) == 0 {
		t.Fatal("W7-12(d) FIXTURE DEFECTIVE: sqlite_master lists no tables, so the ghost walk covers nothing")
	}
	var hits []string
	for _, tbl := range tables {
		for _, f := range w7Present(w7TableInfo(t, db, tbl), forbidden) {
			hits = append(hits, tbl+"."+f)
		}
	}
	sort.Strings(hits)
	return hits
}

// =========================================================================
// W7-X5 — W7 does NOT bump SchemaVersion, in its OWN '9'-edge instance
// =========================================================================
//
// ⟦NO PRE-CHUNK RED — all three arms, §2.4 row 7⟧ for the VERSION half; its
// evidence is (m7-X5), the planted bump, plus the pre-bump CONTROL.
//
// ⚠ W7 takes the INSTANCE option rather than W6's ruling option, and the
// reason is measured rather than stylistic: W6 added a column to `visit` and
// its new shape healed under IF NOT EXISTS. W7 changes the shape of `trip` —
// an existing table with stored rows in any developer's local database — and
// relaxes a NOT NULL, which IF NOT EXISTS cannot heal (refdata.go:100-105).
//
// ⚠ W7 takes the TIGHTENED substring, which W6 permitted but did not require:
// store.go:334's message is "store: %s has schema_version %s but this binary
// requires %d — refusing to open" and %s is the STORE PATH, which can itself
// contain a `9`, so Contains(err, "9") can pass on a path.
//
// ⚠ THE SAME-ARTEFACT CLAUSE IS A PRECONDITION, NOT AN ASSERTION
// (⟦LOCK — XCH-F19⟧): the store under test is checked for `trip.vehicle_id`
// and the `vehicle` table FIRST; if either is missing the test fails as
// "FIXTURE DEFECTIVE", naming which — never as a criterion failure.
//
// EXPECTED RED at e9cc5f5 (runtime): "FIXTURE DEFECTIVE ... trip.vehicle_id
// absent; vehicle table absent".
// ⟦was: TestW7SchemaVersionIsElevenAndAV10StoreIsRefused — re-pinned 11 -> 12
// (R-16, 2026-09-11); W7 still bumped nothing, the NEXT WINDOW did, again⟧
// ⟦was: TestW7SchemaVersionIsThirteenAndAV12StoreIsRefused — re-pinned 13 -> 14
// (R-18, 2026-09-13); W7 still bumped nothing, the unsolicited-arrival door did⟧
// ⟦was: TestW7SchemaVersionIsFourteenAndAV13StoreIsRefused — re-pinned 14 -> 15
// (R-20 + R-23, 2026-09-13); W7 still bumped nothing, the rejoin path did⟧
// ⟦was: TestW7SchemaVersionIsFifteenAndAV14StoreIsRefused — re-pinned 15 -> 16
// (R3 alert_kind, 2026-10-04, fix round 2); W7 still bumped nothing, R3 did⟧
func TestW7SchemaVersionIsSixteenAndAV15StoreIsRefused(t *testing.T) {
	// (iii) the constant itself. This arm names no unbuilt symbol and is the
	// direct kill for (m7-X5).
	if store.SchemaVersion != 16 {
		t.Errorf("W7-X5 (m7-X5, RE-POINTED AGAIN): store.SchemaVersion is %d, want 16 (re-pinned at the 15 -> 16 increment, R3 alert_kind, 2026-10-04 — was re-pinned to 15 by R-20 + R-23, to 14 by R-18, to 13 by R-17, and to 12 by R-16). W5-SCHEMAVERSION-ADJUDICATION.md strikes the "+
			"per-sub-chunk bump requirement and its reasoning — one cutover, every database deleted, one "+
			"hash-verified binary — is WINDOW-wide, so it reaches W7 unchanged; this literal just tracks "+
			"whatever the CURRENT version is, unrelated to W7's own shape which has not moved since.", store.SchemaVersion)
	}

	// --- CONTROL: the pre-bump control, and the tightened substring's own
	// discrimination. A path containing a '9' must NOT satisfy the match.
	looseOnly := "store: /tmp/w11-run/db.sqlite has schema_version 16 but this binary requires 16 — refusing to open"
	if w7SchemaRefusalMatches(looseOnly) {
		t.Fatalf("W7-X5 CONTROL FAILED: the TIGHTENED matcher accepted a message whose only `10` is in the STORE "+
			"PATH (%q). Contains(err, \"11\") passing on a path is the known instrument weakness W7 closes rather "+
			"than re-records.", looseOnly)
	}
	realShape := "store: /tmp/x/db.sqlite has schema_version 15 but this binary requires 16 — refusing to open"
	if !w7SchemaRefusalMatches(realShape) {
		t.Fatalf("W7-X5 CONTROL FAILED (negative arm): the matcher rejected the real refusal shape %q", realShape)
	}

	// --- THE PRECONDITION (rule 6) ---------------------------------------
	dir := t.TempDir()
	path := filepath.Join(dir, "w7-version.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("fixture defect: opening %s: %v", path, err)
	}
	if err := domain.Register(st); err != nil {
		t.Fatalf("fixture defect: registering projections: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("fixture defect: closing %s: %v", path, err)
	}
	raw := w7OpenRaw(t, path)
	var faults []string
	if _, ok := w7Find(w7TableInfo(t, raw, "trip"), "vehicle_id"); !ok {
		faults = append(faults, "trip.vehicle_id absent")
	}
	if !w7TableExists(t, raw, w7VehicleTable) {
		faults = append(faults, "`vehicle` table absent")
	}
	if len(faults) > 0 {
		t.Fatalf("W7-X5 FIXTURE DEFECTIVE: %s. W5-X5's amended form (§15.3) makes the same-artefact property a "+
			"rule-6 PRECONDITION — the store whose version was read must actually carry W7's shape, or the "+
			"FIXTURE is declared defective rather than the criterion failed.", strings.Join(faults, "; "))
	}

	// (iii) StoredSchemaVersion agrees with the constant AND with "12".
	st2, err := store.Open(path)
	if err != nil {
		t.Fatalf("W7-X5 CONTROL: a CURRENT-version store must Open successfully, got %v", err)
	}
	got, err := st2.StoredSchemaVersion()
	if err != nil {
		t.Fatalf("reading StoredSchemaVersion: %v", err)
	}
	// Re-pinned 13 -> 14 (R-18, 2026-09-13, unsolicited-arrival door), and
	// 14 -> 15 (R-20 + R-23, 2026-09-13, the rejoin path), and 15 -> 16 (R3
	// alert_kind, 2026-10-04): this literal
	// tracks the CURRENT version, unrelated to W7's own shape.
	if want := fmt.Sprint(store.SchemaVersion); got != want || got != "16" {
		t.Errorf("W7-X5 arm (iii): StoredSchemaVersion()=%q, want %q and want \"16\"", got, want)
	}
	if err := st2.Close(); err != nil {
		t.Fatal(err)
	}

	// (i)+(ii) a store stamped the LITERAL PREDECESSOR '10' is refused at Open
	// by a non-nil UNTYPED error naming both versions and "refusing".
	// ⚠ The fixture stamps the literal '10', never SchemaVersion-1: a fixture
	// computed from the constant tracks a bump instead of catching it.
	// ⟦re-pinned from '10' at the 11 -> 12 increment, 2026-09-11; from '12' at
	// the 13 -> 14 increment, 2026-09-13; from '13' at the 14 -> 15 increment,
	// 2026-09-13; from '14' at the 15 -> 16 increment, 2026-10-04⟧
	old := filepath.Join(dir, "w7-old.db")
	w7StampSchemaVersion(t, old, "15")
	_, openErr := store.Open(old)
	if openErr == nil {
		t.Fatalf("W7-X5 arm (i): Open SUCCEEDED over a store stamped the literal predecessor '15'")
	}
	if !w7SchemaRefusalMatches(openErr.Error()) {
		t.Errorf("W7-X5 arm (i): the refusal is %q — it must name BOTH versions and \"refusing\". "+
			"⚠ Assertion form is the substring pair, never errors.As(&store.SchemaVersionError{}), which is not "+
			"on the Open path (plan:3276-3284).", openErr.Error())
	}
}

// w7SchemaRefusalMatches is W7-X5's assertion form, TIGHTENED:
// `strings.Contains(err, "schema_version 11 ")` — with the trailing space, so
// the match cannot be satisfied by a `11` inside the store path — plus
// "refusing". Shared by the control and the assertion. ⟦was "schema_version
// 10 "; re-pinned at the 11 -> 12 increment, R-16, 2026-09-11⟧ ⟦re-pinned to
// "schema_version 13 " at the 13 -> 14 increment, R-18, 2026-09-13⟧ ⟦re-pinned
// to "schema_version 14 " at the 14 -> 15 increment, R-20 + R-23, 2026-09-13⟧
// ⟦re-pinned to "schema_version 15 " at the 15 -> 16 increment, R3
// alert_kind, 2026-10-04⟧
func w7SchemaRefusalMatches(msg string) bool {
	return strings.Contains(msg, "schema_version 15 ") && strings.Contains(msg, "refusing")
}

// w7StampSchemaVersion creates a store at `path` and rewrites its
// schema_version. W7 writes its own rather than calling W5a's or W6's:
// W7-X3 forbids another chunk's green counting toward W7's, and a helper
// shared across chunk boundaries is a dependency that can be deleted out from
// under it.
func w7StampSchemaVersion(t *testing.T, path, version string) {
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
