// w6_visit_projection_test.go — W6's STORE-LEVEL halves:
//
//   - W6-6(a) / W6-X4 part (a): the `visit` projection carries
//     `person_snapshot_name`, TEXT, nullable, no default, STRICT preserved,
//     asserted by INTROSPECTION.
//   - W6-6(b) = W6-X4 part (b): NO column outside the permitted union — the
//     pre-chunk TEN plus one = ELEVEN, listed as literals, in W5a-G-2's
//     post-amendment reading (no scoping by landing order; part (a) carries
//     anti-substitution, NOT anti-over-build).
//   - W6-7(a): the `visit` column set is EXACTLY those eleven, so first_name,
//     last_name, position, phone, pad_oriented and any person_snapshot_*
//     variant are absent by construction ([[D-60]]).
//   - W6-6's GHOST ARM (DR-W6-4): `person_name` must NOT be a `visit` column.
//   - W6-6(a)'s Go-shape half: `Visit.PersonSnapshotName *string` with tag
//     `person_snapshot_name`, and `ArrivalVisit.PersonName *string` with tag
//     `person_name,omitempty` — asserted by REFLECTION, so this file names no
//     unbuilt Go symbol and RUNS at f33c4c7.
//   - W6-X5 / ⟦DR-W6-8⟧: W6 does NOT bump SchemaVersion, in W5a-G-1's
//     re-pointed '9'-edge form.
//
// design/WINDOW-BUILD-PLAN-v1.md `### W6 —`, criteria LOCKED 2026-09-02.
//
// package store_test DELIBERATELY (w5a_projection_and_version_test.go's own
// reason, which is w3_sidetable_structural_test.go's): these assertions need
// domain.Register to create the projection, and `package store` cannot import
// internal/domain — domain imports store.
//
// EXPECTED RED at f33c4c7 (runtime, not compile):
//   - TestW6VisitProjectionCarriesThePersonSnapshotColumn: "person_snapshot_name:
//     absent" — the `visit` DDL is the pre-chunk TEN (store.go:231-242).
//   - TestW6VisitColumnSetIsExactlyTheEleven: the same absence, from the
//     exact-set side.
//   - TestW6VisitAndArrivalVisitCarryTheDeclaredGoShape: the two fields do not
//     exist on the structs.
//   - TestW6ColumnRidesTheV12ShapeAndAV11StoreIsRefused: PASSES. It is
//     ⟦NO PRE-CHUNK RED⟧ (plan §2.5 row 11) and its evidence is (m6-X5).
package store_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// --- SEAMS bound here, once ---------------------------------------------
//
// SEAM W6-S-SPELLINGS: the four literals are NOT a free choice — DR-W6-4
// freezes them and §7 carries them to gate B-2's freeze list at plan:101:
//
//	`person_snapshot_name`   the `visit` COLUMN and the `Visit` JSON tag
//	`PersonSnapshotName`     the `Visit` Go field
//	`person_name`            the `ArrivalVisit` PAYLOAD key (omitempty)
//	`PersonName`             the `ArrivalVisit` Go field
//
// ⚠ THE GHOST RULE, and it is why the asymmetry is asserted rather than
// tidied: the payload spelling and the column spelling ALREADY differ on this
// table (`company_name` on the wire vs `company_snapshot_name` in the column,
// trip.go:159-166 vs store.go:236-238). Carrying BOTH spellings on BOTH sides
// would freeze a rejected spelling beside an accepted one — the
// `ref_alias_created` ghost shape, and the `cargo`/`cargo_note` recurrence B-2
// exists to prevent.

const (
	w6Column        = "person_snapshot_name" // the `visit` column + Visit's tag
	w6GoField       = "PersonSnapshotName"   // Visit's Go field
	w6PayloadKey    = "person_name"          // ArrivalVisit's payload key
	w6PayloadGoName = "PersonName"           // ArrivalVisit's Go field
)

// w6VisitPreChunkColumns is the MEASURED pre-chunk `visit` column set
// (store.go:231-242 at f33c4c7) — part (b)'s base, TEN columns.
var w6VisitPreChunkColumns = []string{
	"visit_id", "site_id", "person_id", "trip_id",
	"company_snapshot_id", "company_snapshot_name",
	"workgroup_snapshot_id", "workgroup_snapshot_name",
	"is_open", "recorded_by",
}

// w6DeclaredAdditions is W6's declared addition set: exactly one column.
var w6DeclaredAdditions = []string{w6Column}

// w6PermittedUnion is W6-6(b)'s permitted union — the pre-chunk ten PLUS
// W6's one declared addition = ELEVEN, with no scoping by landing order
// (W5a-G-2's post-amendment reading, plan:3106-3111).
var w6PermittedUnion = append(append([]string{}, w6VisitPreChunkColumns...), w6DeclaredAdditions...)

// w6ArrivalVisitGoFields is the EXACT permitted Go field set of
// domain.ArrivalVisit — the pre-chunk SIX plus W6's one declared addition =
// SEVEN. It is the reflection twin of w6PermittedUnion, and it exists
// because a payload key set asserted over MARSHALLED BYTES cannot see an
// `omitempty` field whose source is nil (G-2; see the exact-set arm in
// TestW6VisitAndArrivalVisitCarryTheDeclaredGoShape).
var w6ArrivalVisitGoFields = []string{
	"VisitID", "PersonID",
	"CompanyID", "CompanyName",
	"WorkgroupID", "WorkgroupName",
	w6PayloadGoName,
}

// w6ForbiddenOnVisit is [[D-60]]'s violation set plus DR-W6-4's ghost.
// ⚠ `person_name` is here because it is the PAYLOAD spelling: a builder who
// used one spelling on both sides breaks the ghost rule, and nothing else in
// the tree would notice.
var w6ForbiddenOnVisit = []string{
	"first_name", "last_name", "position", "phone", "pad_oriented",
	w6PayloadKey, "person_snapshot_first_name", "person_snapshot_last_name",
}

// --- introspection machinery (no assertions) ----------------------------

func w6OpenRaw(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

type w6Col struct {
	Name     string
	Type     string
	NotNull  bool
	HasDflt  bool
	DfltText string
}

func w6TableInfo(t *testing.T, db *sql.DB, table string) []w6Col {
	t.Helper()
	rows, err := db.Query(`SELECT name, type, "notnull", dflt_value FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []w6Col
	for rows.Next() {
		var c w6Col
		var nn int
		var dflt sql.NullString
		if err := rows.Scan(&c.Name, &c.Type, &nn, &dflt); err != nil {
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

func w6ColNames(cols []w6Col) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		out = append(out, c.Name)
	}
	return out
}

func w6Find(cols []w6Col, name string) (w6Col, bool) {
	for _, c := range cols {
		if c.Name == name {
			return c, true
		}
	}
	return w6Col{}, false
}

func w6MissingCols(cols []w6Col, want []string) []string {
	var missing []string
	for _, w := range want {
		if _, ok := w6Find(cols, w); !ok {
			missing = append(missing, w)
		}
	}
	return missing
}

// w6OutsideUnion is part (b)'s detector — shared by the control and the real
// assertion, so the control exercises the same code.
func w6OutsideUnion(cols []w6Col, permitted []string) []string {
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

// w6ShapeFaults is part (a)'s detector: absent, wrong declared type, NOT
// NULL, or carrying a default.
func w6ShapeFaults(cols []w6Col, name string) []string {
	c, ok := w6Find(cols, name)
	if !ok {
		return []string{name + ": absent"}
	}
	var faults []string
	if !strings.EqualFold(strings.TrimSpace(c.Type), "TEXT") {
		faults = append(faults, fmt.Sprintf("%s: declared type %q, want TEXT", name, c.Type))
	}
	if c.NotNull {
		faults = append(faults, name+": declared NOT NULL, want nullable — the snapshot is ABSENT for a person the roster cannot name")
	}
	if c.HasDflt {
		faults = append(faults, fmt.Sprintf("%s: carries a default (%q), want none", name, c.DfltText))
	}
	return faults
}

var w6StrictRe = regexp.MustCompile(`(?is)\)\s*strict\s*;?\s*$`)

func w6TableDDL(t *testing.T, db *sql.DB, table string) string {
	t.Helper()
	var ddl string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&ddl); err != nil {
		t.Fatalf("reading DDL for %s: %v", table, err)
	}
	return ddl
}

// w6StrictBehaviourHolds is the BEHAVIOURAL half of "STRICT preserved": in a
// STRICT table a BLOB cannot be stored in a TEXT column. The probe value is a
// BLOB deliberately — TEXT affinity converts a numeric value to text, so an
// INTEGER probe reports a STRICT table as loose (W5a's file records paying
// for that mistake). The control below proves the discrimination.
func w6StrictBehaviourHolds(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	_, err = tx.Exec(fmt.Sprintf(
		`INSERT INTO %s (visit_id, site_id, person_id, is_open) VALUES ('w6-strict-probe', 'site-a', x'00ff', 1)`, table))
	return err != nil
}

// w6RegisteredVisitDB opens a store, registers the domain projections, closes
// it, and returns a raw handle on the same file.
func w6RegisteredVisitDB(t *testing.T, name string) *sql.DB {
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
	return w6OpenRaw(t, path)
}

// --- W6-6(a) / W6-X4 part (a) + part (b) --------------------------------
//
// EXPECTED RED at f33c4c7 (runtime): "person_snapshot_name: absent", with the
// actual ten columns printed.
// EXPECTED RED (m6-6) arms 1-3: the column dropped, renamed, or declared NOT
// NULL. EXPECTED RED (m6-X4): a column outside the union.
func TestW6VisitProjectionCarriesThePersonSnapshotColumn(t *testing.T) {
	// --- CONTROL FIRST (W6-X6(a)). Every tooth of the detector is shown
	// biting a PLANTED violation on a scratch table before the real table is
	// looked at. One plant against a four-tooth detector is a green ledger
	// row proving nothing.
	ctl := w6OpenRaw(t, filepath.Join(t.TempDir(), "w6-control.db"))

	// (a1) the column DROPPED — (m6-6) arm 1. This is also (m6-6) arm 2
	// (RENAMED) from the detector's point of view, and arm 2 is separated by
	// the stray-column tooth below, which sees the new name.
	if _, err := ctl.Exec(`CREATE TABLE visit_ctl_dropped (
		visit_id TEXT PRIMARY KEY, site_id TEXT NOT NULL, person_id TEXT NOT NULL,
		trip_id TEXT, company_snapshot_id TEXT, company_snapshot_name TEXT,
		workgroup_snapshot_id TEXT, workgroup_snapshot_name TEXT,
		is_open INTEGER NOT NULL, recorded_by TEXT
	) STRICT`); err != nil {
		t.Fatal(err)
	}
	dropped := w6TableInfo(t, ctl, "visit_ctl_dropped")
	if miss := w6MissingCols(dropped, w6DeclaredAdditions); len(miss) != 1 || miss[0] != w6Column {
		t.Fatalf("W6-6(a) CONTROL FAILED (a1): the missing-column detector reported %v on a table with `%s` dropped, "+
			"want exactly [%s] — a detector that cannot see its own plant proves nothing (m6-6 arm 1)", miss, w6Column, w6Column)
	}

	// (a2) NOT NULL, (a3) a DEFAULT, (a4) the wrong declared type.
	if _, err := ctl.Exec(`CREATE TABLE visit_ctl_shape (
		visit_id TEXT PRIMARY KEY, site_id TEXT NOT NULL, person_id TEXT NOT NULL,
		trip_id TEXT, company_snapshot_id TEXT, company_snapshot_name TEXT,
		workgroup_snapshot_id TEXT, workgroup_snapshot_name TEXT,
		is_open INTEGER NOT NULL, recorded_by TEXT,
		person_snapshot_name TEXT NOT NULL
	) STRICT`); err != nil {
		t.Fatal(err)
	}
	if f := w6ShapeFaults(w6TableInfo(t, ctl, "visit_ctl_shape"), w6Column); len(f) != 1 || !strings.Contains(f[0], "NOT NULL") {
		t.Fatalf("W6-6(a) CONTROL FAILED (a2): the nullability tooth reported %v on a planted NOT NULL column (m6-6 arm 3)", f)
	}
	if _, err := ctl.Exec(`CREATE TABLE visit_ctl_dflt (
		visit_id TEXT PRIMARY KEY, site_id TEXT NOT NULL, person_id TEXT NOT NULL,
		trip_id TEXT, company_snapshot_id TEXT, company_snapshot_name TEXT,
		workgroup_snapshot_id TEXT, workgroup_snapshot_name TEXT,
		is_open INTEGER NOT NULL, recorded_by TEXT,
		person_snapshot_name TEXT DEFAULT ''
	) STRICT`); err != nil {
		t.Fatal(err)
	}
	if f := w6ShapeFaults(w6TableInfo(t, ctl, "visit_ctl_dflt"), w6Column); len(f) != 1 || !strings.Contains(f[0], "default") {
		t.Fatalf("W6-6(a) CONTROL FAILED (a3): the default tooth reported %v on a planted DEFAULT '' column — "+
			"⚠ a DEFAULT '' would make DR-W6-3's empty-is-ABSENT rule the DEFAULT STATE of every row written by "+
			"a build that forgot to populate it", f)
	}
	if _, err := ctl.Exec(`CREATE TABLE visit_ctl_type (
		visit_id TEXT PRIMARY KEY, site_id TEXT NOT NULL, person_id TEXT NOT NULL,
		trip_id TEXT, company_snapshot_id TEXT, company_snapshot_name TEXT,
		workgroup_snapshot_id TEXT, workgroup_snapshot_name TEXT,
		is_open INTEGER NOT NULL, recorded_by TEXT,
		person_snapshot_name INTEGER
	) STRICT`); err != nil {
		t.Fatal(err)
	}
	if f := w6ShapeFaults(w6TableInfo(t, ctl, "visit_ctl_type"), w6Column); len(f) != 1 || !strings.Contains(f[0], "want TEXT") {
		t.Fatalf("W6-6(a) CONTROL FAILED (a4): the declared-type tooth reported %v on a planted INTEGER column", f)
	}

	// (b) a column OUTSIDE the union — (m6-X4), and (m6-7)'s planted part.
	if _, err := ctl.Exec(`CREATE TABLE visit_ctl_extra (
		visit_id TEXT PRIMARY KEY, site_id TEXT NOT NULL, person_id TEXT NOT NULL,
		trip_id TEXT, company_snapshot_id TEXT, company_snapshot_name TEXT,
		workgroup_snapshot_id TEXT, workgroup_snapshot_name TEXT,
		is_open INTEGER NOT NULL, recorded_by TEXT,
		person_snapshot_name TEXT, person_snapshot_last_name TEXT
	) STRICT`); err != nil {
		t.Fatal(err)
	}
	extra := w6TableInfo(t, ctl, "visit_ctl_extra")
	if stray := w6OutsideUnion(extra, w6PermittedUnion); len(stray) != 1 || stray[0] != "person_snapshot_last_name" {
		t.Fatalf("W6-X4(b) CONTROL FAILED: the outside-the-union detector reported %v on a table carrying a planted "+
			"`person_snapshot_last_name`, want exactly [person_snapshot_last_name] — this is [[D-60]]'s violation "+
			"(m6-7) and the tooth a silent repair to a superset check deletes (m6-X4)", stray)
	}
	if stray := w6OutsideUnion(w6TableInfo(t, ctl, "visit_ctl_shape"), w6PermittedUnion); len(stray) != 0 {
		t.Fatalf("W6-X4(b) CONTROL: the detector flagged %v on a table with no stray column — a detector that "+
			"flags a clean table proves nothing either", stray)
	}

	// (c) STRICT: the behavioural probe must ACCEPT the BLOB on a non-STRICT
	// table (proving it discriminates) and the regex must not match its DDL.
	if _, err := ctl.Exec(`CREATE TABLE visit_ctl_loose (
		visit_id TEXT PRIMARY KEY, site_id TEXT NOT NULL, person_id TEXT NOT NULL,
		trip_id TEXT, company_snapshot_id TEXT, company_snapshot_name TEXT,
		workgroup_snapshot_id TEXT, workgroup_snapshot_name TEXT,
		is_open INTEGER NOT NULL, recorded_by TEXT, person_snapshot_name TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	if w6StrictBehaviourHolds(t, ctl, "visit_ctl_loose") {
		t.Fatal("W6-6(a) CONTROL FAILED (c): the STRICT probe reported a NON-strict table as strict — its clean pass would prove nothing")
	}
	if w6StrictRe.MatchString(w6TableDDL(t, ctl, "visit_ctl_loose")) {
		t.Fatal("W6-6(a) CONTROL FAILED (c): the STRICT declaration regex matched a non-STRICT CREATE statement")
	}
	if !w6StrictBehaviourHolds(t, ctl, "visit_ctl_extra") {
		t.Fatal("W6-6(a) CONTROL FAILED (c): the STRICT probe reported a STRICT table as loose")
	}

	// --- the real projection ------------------------------------------
	db := w6RegisteredVisitDB(t, "w6-visit.db")
	cols := w6TableInfo(t, db, "visit")

	// Rule 6 — the arrangement this test needs must actually hold.
	if len(cols) == 0 {
		t.Fatalf("fixture defect: the `visit` projection table does not exist after domain.Register — W6-6 cannot be measured")
	}
	if miss := w6MissingCols(cols, w6VisitPreChunkColumns); len(miss) != 0 {
		t.Fatalf("fixture defect: the pre-chunk `visit` columns %v are absent — part (b)'s base is MEASURED at "+
			"store.go:231-242 and this tree does not match it", miss)
	}

	// PART (a).
	if faults := w6ShapeFaults(cols, w6Column); len(faults) != 0 {
		t.Errorf("W6-6(a) / W6-X4(a) (m6-6): the `visit` projection does not carry `%s` in the declared shape — "+
			"nullable TEXT, no default:\n  %s\n  actual columns: %v\n"+
			"  ⚠ NULLABLE is load-bearing: the snapshot is ABSENT for a person the roster cannot name, and "+
			"DR-W6-3 rules NULL and \"\" alike ABSENT at the render.",
			w6Column, strings.Join(faults, "\n  "), w6ColNames(cols))
	}
	if ddl := w6TableDDL(t, db, "visit"); !w6StrictRe.MatchString(ddl) {
		t.Errorf("W6-6(a): the `visit` CREATE statement no longer ends in STRICT — %q", ddl)
	}
	if !w6StrictBehaviourHolds(t, db, "visit") {
		t.Errorf("W6-6(a): the `visit` table accepted a BLOB in a TEXT column — STRICT was not preserved across W6's shape change")
	}

	// PART (b).
	if stray := w6OutsideUnion(cols, w6PermittedUnion); len(stray) != 0 {
		t.Errorf("W6-6(b) / W6-X4(b) (m6-X4): `visit` carries column(s) %v outside the permitted union %v",
			stray, w6PermittedUnion)
	}
}

// --- W6-7(a): the EXACT column set, and the ghost arm -------------------
//
// ⚠ THE GREENFIELD TRAP, named because [[D-60]]'s absence criterion is its
// clearest instance: "the snapshot carries no name parts" is true at f33c4c7
// because NOTHING EXISTS. What distinguishes "built, and correctly without
// parts" from "not built" is that the EXACT set has ELEVEN members and the
// pre-chunk table has TEN — so this test REDS pre-chunk, by introspection.
// A green here is therefore anchored to the built artefact.
//
// EXPECTED RED at f33c4c7 (runtime): the set is the ten, want the eleven.
// EXPECTED RED (m6-7): a planted `last_name` (or any person_snapshot_*
// variant) on the `visit` table.
// EXPECTED RED (DR-W6-4 ghost): a `person_name` COLUMN.
func TestW6VisitColumnSetIsExactlyTheEleven(t *testing.T) {
	db := w6RegisteredVisitDB(t, "w6-exact.db")
	got := w6ColNames(w6TableInfo(t, db, "visit"))
	sort.Strings(got)
	want := append([]string(nil), w6PermittedUnion...)
	sort.Strings(want)

	if len(want) != 11 {
		t.Fatalf("fixture defect: the permitted union holds %d columns, want 11 (the pre-chunk ten plus one)", len(want))
	}
	if !w6SameStrings(got, want) {
		t.Errorf("W6-7(a) (m6-7): the `visit` column set is\n  %v\nwant EXACTLY the eleven\n  %v\n"+
			"  [[D-60]]: the visit name snapshot freezes the DISPLAY NAME and nothing else. Asserted "+
			"STRUCTURALLY so a later addition goes red rather than landing quietly.\n"+
			"  ⚠ At f33c4c7 this reds because the eleventh member is ABSENT — that is the produced pre-chunk "+
			"red, and it is what distinguishes 'built without parts' from 'not built at all'.", got, want)
	}
	// The ghost arm and D-60's parts, asserted as literal ABSENCES.
	for _, forbidden := range w6ForbiddenOnVisit {
		if _, ok := w6Find(w6TableInfo(t, db, "visit"), forbidden); ok {
			why := "[[D-60]]: the snapshot carries `Name` ONLY"
			if forbidden == w6PayloadKey {
				why = "DR-W6-4's GHOST RULE: `" + w6PayloadKey + "` is the PAYLOAD spelling and must NOT appear as a column. " +
					"Carrying a rejected spelling beside an accepted one is the `ref_alias_created` ghost shape"
			}
			t.Errorf("W6-7(a) / W6-6 ghost arm (m6-7): `visit` carries a `%s` column — %s", forbidden, why)
		}
	}
}

func w6SameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- W6-6(a)'s Go-shape half, by REFLECTION -----------------------------
//
// Asserted by reflection rather than by naming the field, so this file
// compiles and RUNS at f33c4c7 and its failure is a produced runtime red.
//
// EXPECTED RED at f33c4c7: both fields absent.
// EXPECTED RED (m6-6) arm 4: the ArrivalVisit tag misspelled.
func TestW6VisitAndArrivalVisitCarryTheDeclaredGoShape(t *testing.T) {
	for _, c := range []struct {
		what  string
		typ   reflect.Type
		field string
		tag   string
		why   string
	}{
		{
			what: "domain.Visit", typ: reflect.TypeOf(domain.Visit{}), field: w6GoField, tag: w6Column,
			why: "the projection struct's tags ARE its column names (trip.go:259-270), so a tag drift is a " +
				"column drift that no DDL introspection would see",
		},
		{
			what: "domain.ArrivalVisit", typ: reflect.TypeOf(domain.ArrivalVisit{}), field: w6PayloadGoName,
			tag: w6PayloadKey + ",omitempty",
			why: "sibling of CompanyName/WorkgroupName, whose tags are company_name/workgroup_name. The wire " +
				"names WHAT THE FIELD IS; the column names WHAT THE ROW IS. ⚠ This is a FROZEN WIRE LITERAL — " +
				"ArrivalVisit sits inside TripArrival.Visits inside Hash()ed bytes (trip.go:173-181), so it " +
				"reaches gate B-2's freeze list (plan:101, §7)",
		},
	} {
		c := c
		t.Run(c.what, func(t *testing.T) {
			f, ok := c.typ.FieldByName(c.field)
			if !ok {
				var have []string
				for i := 0; i < c.typ.NumField(); i++ {
					have = append(have, c.typ.Field(i).Name)
				}
				t.Fatalf("W6-6(a) (m6-6): %s has no field %s. It carries %v.\n  %s",
					c.what, c.field, have, c.why)
			}
			if f.Type.Kind() != reflect.Ptr || f.Type.Elem().Kind() != reflect.String {
				t.Errorf("W6-6(a): %s.%s is %s, want *string — DR-W6-4 freezes the Go type of both as *string, "+
					"nullable at both ends", c.what, c.field, f.Type)
			}
			if got := f.Tag.Get("json"); got != c.tag {
				t.Errorf("W6-6(a) (m6-6 arm 4): %s.%s carries json tag %q, want %q.\n  %s",
					c.what, c.field, got, c.tag, c.why)
			}
		})
	}

	// The GHOST arm on the Go side: neither struct may carry the other's
	// spelling.
	if _, ok := reflect.TypeOf(domain.Visit{}).FieldByName(w6PayloadGoName); ok {
		t.Errorf("W6-6 ghost arm: domain.Visit carries a %s field — the column spelling is %s", w6PayloadGoName, w6GoField)
	}
	if _, ok := reflect.TypeOf(domain.ArrivalVisit{}).FieldByName(w6GoField); ok {
		t.Errorf("W6-6 ghost arm: domain.ArrivalVisit carries a %s field — the payload spelling is %s", w6GoField, w6PayloadGoName)
	}

	// --- W6-7(b)'s STRUCTURAL half: ArrivalVisit's EXACT Go field set ---
	//
	// ⚠ WHY THIS ARM EXISTS, and it is a GAP CLOSED rather than a test
	// improved. W6-7(b)'s locked CONTROL is "a planted `last_name` on EACH
	// shape must be detected before either clean pass counts" — the `visit`
	// column set AND the marshalled ArrivalVisit payload. The payload half
	// was carried ONLY by the byte-level key-set assertion in
	// w6_populate_test.go, and [MEASURED by the W6 mutant gate, G-2] that
	// assertion is BLIND to the realistic plant: a field added with
	// `omitempty` and populated from a source that is nil on the fixture's
	// person is never marshalled, so the byte-level set is unchanged and all
	// 22 W6 tests stay GREEN over it. [[D-60]]'s promise that "a later
	// addition goes red rather than landing quietly" was therefore FALSE of
	// the payload shape.
	//
	// This arm is the reflection equivalent of W6-7(a)'s exact COLUMN set,
	// and it is immune to BOTH escapes: to `omitempty`, because it reads the
	// STRUCT and not a payload, and to fixture accident, because it has no
	// fixture. The populate-side fixture was fixed too (both halves of the
	// locked control now hold), but only this arm holds unconditionally.
	//
	// ⚠ AN EXACT SET, NOT A FORBIDDEN LIST. [[D-60]] is an ABSENCE criterion
	// and a forbidden list refuses only the parts someone thought of:
	// first_name/last_name/position/phone/pad_oriented are named in
	// w6ForbiddenOnVisit, `middle_initial` is not, and an exact set refuses
	// it anyway.
	//
	// GREENFIELD ANCHOR: pre-chunk ArrivalVisit carries SIX fields and the
	// permitted set is SEVEN, so this REDS at f33c4c7 by absence — which is
	// what distinguishes "built, and correctly without parts" from "not
	// built at all".
	t.Run("ArrivalVisit exact Go field set", func(t *testing.T) {
		typ := reflect.TypeOf(domain.ArrivalVisit{})
		got := make([]string, 0, typ.NumField())
		for i := 0; i < typ.NumField(); i++ {
			got = append(got, typ.Field(i).Name)
		}
		sort.Strings(got)
		want := append([]string(nil), w6ArrivalVisitGoFields...)
		sort.Strings(want)
		if len(want) != 7 {
			t.Fatalf("fixture defect: the permitted ArrivalVisit field set holds %d fields, want 7 "+
				"(the pre-chunk six plus one)", len(want))
		}
		if !w6SameStrings(got, want) {
			t.Errorf("W6-7(b) / W6-6(a) (m6-7): domain.ArrivalVisit's Go field set is\n  %v\nwant EXACTLY the seven\n  %v\n"+
				"  [[D-60]]: the visit name snapshot freezes the DISPLAY NAME and nothing else, on BOTH shapes — "+
				"the column set AND the payload. first_name, last_name, position, phone, pad_oriented and any "+
				"person_snapshot_* variant are absent BY CONSTRUCTION.\n"+
				"  ⚠ This arm is asserted over the STRUCT because the byte-level key set cannot see an "+
				"`omitempty` field whose source is nil for the fixture's person — the plant that survived the "+
				"mutant gate (G-2). An EXTRA field here is (m6-7) whether or not it ever marshals.\n"+
				"  ⚠ At f33c4c7 this reds because %s is ABSENT (six fields, not seven) — the produced pre-chunk "+
				"red that anchors this absence criterion to a built artefact.", got, want, w6PayloadGoName)
		}
	})
}

// --- W6-X5 / ⟦DR-W6-8⟧: W6 does NOT bump SchemaVersion ------------------
//
// The row is closed by a RULING rather than left blank: at W6's lock
// store.go was `const SchemaVersion = 10`, and store.go's own note said
// "SchemaVersion stays 10 — this column rides W1's 9 -> 10 bump" / "⚠ THE
// NEXT WINDOW MUST BUMP" — next WINDOW, not next chunk. That next window is
// the D-92/D-88/D-91/D-98 increment, which bumped 10 -> 11 once; the row was
// re-pinned to it (2026-09-05), and again to the 11 -> 12 increment (R-16,
// 2026-09-11: entry-on-completion + the import grandfathering marker).
// reports/W5-SCHEMAVERSION-ADJUDICATION.md STRIKES the per-sub-chunk bump
// requirement and its reasoning is window-wide.
//
// W6 takes the FIRST option of W5a-G-1's ruling — its own '9'-edge instance,
// never "W1's two tests discharge it", which is what W5a's gate refused.
//
// ⚠ INSTRUMENT NOTE, and it is a KNOWN weakness recorded rather than
// discovered: store.go:334's refusal message is "store: %s has schema_version
// %s but this binary requires %d — refusing to open" and the first %s is the
// STORE PATH, which can itself contain a `9`. The lock PERMITS (and does not
// require) asserting on the substring "schema_version 9 " instead, and this
// test takes that option — a W6 gate meeting a path containing 9 is then
// looking at a real result, not at a known instrument weakness.
//
// EXPECTED RED at f33c4c7: NONE (plan §2.5 row 11, ⟦NO PRE-CHUNK RED⟧ — all
// three arms). Its evidence is (m6-X5): a chunk that BUMPS SchemaVersion.
// Under that mutant arm (iii) reds; arms (i)/(ii) stay green, which is
// exactly why (iii) is pinned to the LITERAL "12" and not only to a relation.
// ⟦was: TestW6ColumnRidesTheV11ShapeAndAV10StoreIsRefused — re-pinned 11 -> 12⟧
func TestW6ColumnRidesTheV12ShapeAndAV11StoreIsRefused(t *testing.T) {
	dir := t.TempDir()

	// --- CONTROL, first: the pre-bump control --------------------------
	ctlPath := filepath.Join(dir, "w6-x5-control.db")
	w6StampSchemaVersion(t, ctlPath, fmt.Sprint(store.SchemaVersion))
	ctl, err := store.Open(ctlPath)
	if err != nil {
		t.Fatalf("W6-X5 CONTROL FAILED (pre-bump control): a store stamped the CURRENT version %d did not Open: %v "+
			"— the fixture rewrite is broken, so the '9' refusal below would prove nothing", store.SchemaVersion, err)
	}
	if err := ctl.Close(); err != nil {
		t.Fatal(err)
	}

	// --- (iii) W6's column is inside the v10 shape ----------------------
	st, err := store.Open(filepath.Join(dir, "w6-x5.db"))
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

	// Rule 6 — the version read must be OF THE SAME ARTEFACT that carries
	// W6's column, which is the assertion W1's constant-read does not make.
	// ⚠ Pre-chunk the column is absent, so this is a LOG, not a Fatal: the
	// absence is W6-6(a)'s red, and duplicating it here would book one
	// observation twice.
	live := w6OpenRaw(t, livePath)
	if miss := w6MissingCols(w6TableInfo(t, live, "visit"), w6DeclaredAdditions); len(miss) != 0 {
		t.Logf("W6-X5 NOTE: the store whose schema_version is read below does not yet carry %v. At f33c4c7 that "+
			"is the pre-chunk state and W6-6(a) is the criterion that reds for it; after the build it means this "+
			"arm's subject is not W6's shape.", miss)
	}

	if want := fmt.Sprint(store.SchemaVersion); stored != want {
		t.Errorf("W6-X5(iii): the store carrying W6's column reads back schema_version %q, want %q "+
			"(= fmt.Sprint(store.SchemaVersion))", stored, want)
	}
	// Re-pinned 15 -> 16 (R3 alert_kind, 2026-10-04): this literal tracks the
	// CURRENT version. ⟦was: `stored != "15"`⟧
	if stored != "16" {
		t.Errorf("W6-X5(iii) (m6-X5, RE-POINTED AGAIN): the store carrying W6's column reads back schema_version %q, want \"16\" — "+
			"the 15 -> 16 increment's version (re-pinned 2026-10-04, R3 alert_kind, the sole bump of fix round 2; before that the 14 -> 15 increment, re-pinned 2026-09-13, R-20 + R-23, the rejoin path; was re-pinned to 14 by R-18, to 13 by R-17, and to 12 by R-16). ⟦DR-W6-8⟧ W6 did NOT bump: store.go said "+
			"\"SchemaVersion stays 10 — this column rides W1's 9 -> 10 bump\" and \"⚠ THE NEXT WINDOW MUST BUMP\", "+
			"next WINDOW not next chunk — and the next window (D-92/D-88/D-91/D-98) is the one that bumped; this literal "+
			"just tracks whatever the CURRENT version is, unrelated to W6's own shape which has not moved since. "+
			"A stray bump here silently falsifies design/WINDOW-CHECKLIST-v1.md:171, the PRINTED operator checkbox "+
			"\"GET / shows schema_version 12\" (already stale, until a later pass updates it too).", stored)
	}

	// --- (i)/(ii) a store stamped the LITERAL '10' is refused at Open ----
	// '11' is the literal predecessor — the version the schema-11 window
	// actually ran (W6-X5(ii): "never SchemaVersion-1", which would silently
	// become 12 under the mutant above and stop testing the edge it names).
	// ⟦re-pinned from '10' at the 11 -> 12 increment, 2026-09-11⟧
	oldPath := filepath.Join(dir, "w6-x5-v11.db")
	w6StampSchemaVersion(t, oldPath, "11")
	if _, err := store.Open(oldPath); err == nil {
		t.Error("W6-X5(i): Open SUCCEEDED over a store stamped schema_version '11'; the v12 binary must refuse it")
	} else if !strings.Contains(err.Error(), "schema_version 11 ") ||
		!strings.Contains(err.Error(), fmt.Sprint(store.SchemaVersion)) ||
		!strings.Contains(err.Error(), "refusing") {
		// Never errors.As(&store.SchemaVersionError{}): that is the
		// per-envelope REPLICATION refusal and is not on the Open path
		// (plan:3276-3284), so a type assertion books a kill that can never
		// be produced.
		t.Errorf("W6-X5(i): the refusal must be a non-nil UNTYPED error naming BOTH versions and the word "+
			"\"refusing\", got: %v", err)
	}
}

// w6StampSchemaVersion creates a store at path and rewrites its stamped
// schema_version. W6 writes its own rather than calling W5a's: W6-X3 forbids
// another chunk's green counting toward W6's, and a helper shared across
// chunk boundaries is a dependency that can be deleted out from under it.
func w6StampSchemaVersion(t *testing.T, path, version string) {
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
	// with the binary's own version and every assertion vacuous.
	n, err := res.RowsAffected()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("fixture defect: stamping schema_version=%s touched %d rows, want 1", version, n)
	}
}
