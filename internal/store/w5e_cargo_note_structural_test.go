// w5e_cargo_note_structural_test.go — W5e's structural half, item 3 (the
// cargo note). Criteria LOCKED 2026-08-31,
// design/WINDOW-BUILD-PLAN-v1.md "#### W5e — item 3: cargo note".
//
// Rows discharged here:
//
//	W5e-1   (introspection half)  — trip.cargo_note exists, TEXT, NULLABLE,
//	                                no default; STRICT preserved.
//	W5e-1b  (column-literal half) — the projection column is EXACTLY the
//	                                literal `cargo_note`; no rival spelling
//	                                (`cargo`, `cargonote`, `note`, …) exists
//	                                anywhere in the database.
//	W5-X4   (a) and (b)           — the two-part projection column assertion
//	                                for the `trip` table.
//	W5e-4                         — cargo rides departures ONLY (D-21.3):
//	                                no cargo-shaped column on trip_arrival
//	                                and no cargo-shaped field or JSON tag on
//	                                ANY arrival payload struct.
//
// package store_test, DELIBERATELY (W3-13's and W4-1's stated precedent):
// this file names no unbuilt Go symbol, so it COMPILES AND RUNS against the
// clean tree and its clean-tree failure is a genuine RUNTIME red — "trip has
// no cargo_note column" — rather than a compile error that would bury it.
//
// EXPECTED RED, stated per rule 5:
//   - pre-chunk (88dd6a6): TestW5eCargoNoteColumnStructural FAILS at the
//     first real assertion — the column does not exist. Runtime, not compile.
//   - (m5e-1): the column dropped        -> part (a) presence assertion.
//   - (m5e-1) the column made NOT NULL / given a DEFAULT -> the nullability
//     and default assertions.
//   - (m5e-1b): the column spelled `cargo` / `cargoNote` / `note`
//     -> the literal assertion AND the
//     rival-spelling census.
//   - (m5-X4): any column on `trip` outside the permitted union
//     -> part (b).
//   - (m5e-4): a cargo_note column on trip_arrival, or a CargoNote field on
//     an arrival payload -> TestW5eNoCargoOnArrivalStructural, which is
//     ⟦NO PRE-CHUNK RED⟧ and whose evidence is the planted controls below
//     plus that mutant, NEVER its clean pass.
//
// SEAM W5e-S-COL — bound here, ONCE. No document declares the projection
// column's SQL TYPE. The locked text pins the NAME (`cargo_note`, W5e-1b)
// and pins nullability; the type is `TEXT`, on the tree's own precedent for
// every free-text nullable column on this table (vehicle_entered,
// destination_site, phase_override_reason, recorded_by are all TEXT in
// internal/store/store.go's `trip` literal). A builder dispute goes to the
// gate, not into an edit of this file.
package store_test

import (
	"database/sql"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// w5eCargoNoteColumn is the PINNED wire/column literal (W5e-1b). It is a
// literal here on purpose: the criterion asserts a literal against the
// introspected column name, never a Go identifier.
const w5eCargoNoteColumn = "cargo_note"

// w5ePreChunkTripColumns is the `trip` projection's column set at 88dd6a6,
// in cid order [MEASURED: pragma_table_info('trip') on a freshly created
// store, and internal/store/store.go's `trip` CREATE literal read line by
// line]. It is W5-X4 part (b)'s pre-chunk half.
var w5ePreChunkTripColumns = []string{
	"trip_id", "origin_site", "node_epoch", "seq",
	"vehicle_entered", "driver_person_id",
	"destination_site", "destination_uncontrolled", "deadline_minutes",
	"phase_at_dispatch", "phase_override_reason", "recorded_by",
}

// w5eTripDeclaredAdditions is W5-X4 part (b)'s other half for THIS table.
// W5a/W5b/W5c declare columns on `person` and W5d on company tables; none of
// them touches `trip`, so the permitted union for `trip` is the pre-chunk set
// plus W5e's single declared addition, and it is landing-order-independent by
// construction (W5-X3: "W5d and W5e stay parallel").
var w5eTripDeclaredAdditions = []string{w5eCargoNoteColumn}

// TWO detectors, deliberately, and the split is MEASURED rather than tidy.
//
// w5eCargoStrict matches any name carrying "cargo" (snake or camel). It is
// the whole-schema detector: it can be pointed at every table in the
// database without false-redding.
//
// w5eRivalSpelling additionally matches (m5e-1b)'s and (m5e-4)'s bare
// misspellings — `note`, `manifest`, `haul`. It is applied ONLY where no
// legitimate column or field of that name exists: the `trip` and
// `trip_arrival` tables, and the arrival payload structs. It is NOT the
// whole-schema detector, because a legitimate `note` column may exist
// elsewhere, and a schema-wide `note` ban would report it as a cargo
// violation — a false RED that would be "fixed" by weakening the detector,
// which is how a tooth gets pulled. ⟦was: "because `weather_phase.note` is a
// legitimate, landed column [MEASURED …]" — the weather_phase table went
// with weather phases, James 2026-10-04; the detector split is kept, because
// the reason it guards against outlives the one example⟧
var (
	w5eCargoStrict   = regexp.MustCompile(`(?i)cargo`)
	w5eRivalSpelling = regexp.MustCompile(`(?i)(cargo|^notes?$|^manifests?$|^haul(age)?$)`)
)

// --- introspection helpers (local: package store's w1/w3 helpers are in a
// different package and deliberately not reached across) --------------------

type w5eColumn struct {
	Name     string
	Type     string
	NotNull  bool
	HasDflt  bool
	DfltText string
	PK       int
}

func w5eOpenRaw(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw %s: %v", path, err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func w5eColumns(t *testing.T, db *sql.DB, table string) []w5eColumn {
	t.Helper()
	rows, err := db.Query(`SELECT name, type, "notnull", dflt_value, pk FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		t.Fatalf("pragma_table_info(%s): %v", table, err)
	}
	defer rows.Close()
	var out []w5eColumn
	for rows.Next() {
		var c w5eColumn
		var dflt sql.NullString
		if err := rows.Scan(&c.Name, &c.Type, &c.NotNull, &dflt, &c.PK); err != nil {
			t.Fatalf("scan pragma_table_info(%s): %v", table, err)
		}
		c.HasDflt = dflt.Valid
		c.DfltText = dflt.String
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("pragma_table_info(%s) rows: %v", table, err)
	}
	return out
}

func w5eColumnNames(cols []w5eColumn) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		out = append(out, c.Name)
	}
	return out
}

func w5eFindColumn(cols []w5eColumn, name string) (w5eColumn, bool) {
	for _, c := range cols {
		if c.Name == name {
			return c, true
		}
	}
	return w5eColumn{}, false
}

// w5eCargoShapedColumns is THE cargo-column detector: every column of `table`
// whose name is cargo-shaped, minus the one canonical spelling when `exempt`
// names it. Shared by the controls and the real assertions (a detector the
// controls do not exercise proves nothing).
func w5eCargoShapedColumns(t *testing.T, db *sql.DB, table string, det *regexp.Regexp, exempt map[string]bool) []string {
	t.Helper()
	var bad []string
	for _, c := range w5eColumns(t, db, table) {
		if det.MatchString(c.Name) && !exempt[c.Name] {
			bad = append(bad, c.Name)
		}
	}
	sort.Strings(bad)
	return bad
}

// w5eIsStrict reports whether `table` was declared STRICT, read from its
// stored DDL in sqlite_master (W5-X4 part (a): "STRICT is preserved").
func w5eIsStrict(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var ddl string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&ddl); err != nil {
		t.Fatalf("read DDL for %s: %v", table, err)
	}
	tail := ddl
	if i := strings.LastIndex(ddl, ")"); i >= 0 {
		tail = ddl[i:]
	}
	return regexp.MustCompile(`(?i)\bSTRICT\b`).MatchString(tail)
}

func w5eUserTables(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		out = append(out, n)
	}
	return out
}

// w5eFreshStore creates a store with every domain projection registered and
// returns the closed database's path — the shape the running system produces.
func w5eFreshStore(t *testing.T) string {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "w5e-structural.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := domain.Register(st); err != nil {
		t.Fatalf("domain.Register: %v", err)
	}
	path := st.Path
	if err := st.Close(); err != nil {
		t.Fatalf("store.Close: %v", err)
	}
	return path
}

// TestW5eCargoNoteColumnStructural — W5e-1 (introspection half), W5e-1b
// (column-literal half) and W5-X4 (a)+(b) over the `trip` projection.
//
// CONTROLS FIRST, then the real table. Every control drives THE SAME
// detector the real assertion below uses.
func TestW5eCargoNoteColumnStructural(t *testing.T) {
	db := w5eOpenRaw(t, w5eFreshStore(t))

	// --- CONTROL 1: a rival-spelling table. The rival-spelling census must
	// flag `cargo`, and the presence detector must report cargo_note ABSENT.
	if _, err := db.Exec(`CREATE TABLE w5e_ctl_rival (
		trip_id TEXT PRIMARY KEY,
		cargo   TEXT
	) STRICT`); err != nil {
		t.Fatalf("CONTROL 1 setup: %v", err)
	}
	if bad := w5eCargoShapedColumns(t, db, "w5e_ctl_rival", w5eRivalSpelling, map[string]bool{w5eCargoNoteColumn: true}); len(bad) != 1 || bad[0] != "cargo" {
		t.Fatalf("W5e-1b CONTROL 1 FAILED: the rival-spelling census missed a planted `cargo` column (got %v) — a detector that cannot detect its plant proves nothing (m5e-1b)", bad)
	}
	if _, ok := w5eFindColumn(w5eColumns(t, db, "w5e_ctl_rival"), w5eCargoNoteColumn); ok {
		t.Fatalf("W5e-1 CONTROL 1 FAILED: the presence detector reported %q on a table that has no such column", w5eCargoNoteColumn)
	}

	// --- CONTROL 2: NOT NULL + DEFAULT. The nullability and default
	// detectors must both fire (m5e-1's "trimmed/defaulted" family).
	if _, err := db.Exec(`CREATE TABLE w5e_ctl_notnull (
		trip_id    TEXT PRIMARY KEY,
		cargo_note TEXT NOT NULL DEFAULT ''
	) STRICT`); err != nil {
		t.Fatalf("CONTROL 2 setup: %v", err)
	}
	ctl2, ok := w5eFindColumn(w5eColumns(t, db, "w5e_ctl_notnull"), w5eCargoNoteColumn)
	if !ok {
		t.Fatalf("CONTROL 2 setup: planted cargo_note not seen by the presence detector")
	}
	if !ctl2.NotNull {
		t.Fatalf("W5e-1 CONTROL 2 FAILED: the nullability detector did not see NOT NULL on the planted column — the nullable assertion below could not fail (m5e-1)")
	}
	if !ctl2.HasDflt {
		t.Fatalf("W5-X4(a) CONTROL 2 FAILED: the default detector did not see DEFAULT '' on the planted column — the no-default assertion below could not fail")
	}

	// --- CONTROL 3: a non-STRICT table. The STRICT detector must say so.
	if _, err := db.Exec(`CREATE TABLE w5e_ctl_loose (trip_id TEXT PRIMARY KEY, cargo_note TEXT)`); err != nil {
		t.Fatalf("CONTROL 3 setup: %v", err)
	}
	if w5eIsStrict(t, db, "w5e_ctl_loose") {
		t.Fatalf("W5-X4(a) CONTROL 3 FAILED: the STRICT detector called a non-STRICT table STRICT — the STRICT-preserved assertion below could not fail")
	}
	if !w5eIsStrict(t, db, "w5e_ctl_notnull") {
		t.Fatalf("W5-X4(a) CONTROL 3 FAILED (mirror): the STRICT detector called a STRICT table loose — it would false-red the real assertion")
	}

	// --- CONTROL 4: W5-X4 part (b)'s out-of-union detector, and part (a)'s
	// dropped-column tooth, both driven against planted column sets.
	permitted := map[string]bool{}
	for _, c := range w5ePreChunkTripColumns {
		permitted[c] = true
	}
	for _, c := range w5eTripDeclaredAdditions {
		permitted[c] = true
	}
	plantedRogue := append(append([]string{}, w5ePreChunkTripColumns...), w5eCargoNoteColumn, "cargo_class")
	if out := w5eOutsideUnion(plantedRogue, permitted); len(out) != 1 || out[0] != "cargo_class" {
		t.Fatalf("W5-X4(b) CONTROL 4 FAILED: the out-of-union detector missed a planted `cargo_class` column (got %v) — (m5-X4) could not be caught", out)
	}
	plantedDropped := append([]string{}, w5ePreChunkTripColumns[1:]...)
	if missing := w5eMissingFrom(plantedDropped, w5ePreChunkTripColumns); len(missing) != 1 || missing[0] != "trip_id" {
		t.Fatalf("W5-X4(a) CONTROL 4 FAILED: the dropped-column detector missed a planted drop (got %v) — the dropped-column tooth (m5e-1) could not bite", missing)
	}

	// --- the real table -------------------------------------------------
	cols := w5eColumns(t, db, "trip")
	if len(cols) == 0 {
		t.Fatalf("W5e-1: the `trip` projection table does not exist — fixture defect, not a W5e result")
	}
	names := w5eColumnNames(cols)

	// Rule 6 — assert the ARRANGEMENT this criterion is written against: the
	// pre-chunk twelve must all still be present, or W5-X4 part (b)'s
	// permitted union is stale and every verdict below is measured against
	// the wrong baseline. This is also part (a)'s dropped-column tooth.
	//
	// ⟦was: checked against w5ePreChunkTripColumns whole. Two of the twelve,
	// phase_at_dispatch and phase_override_reason, were STRUCK by a ruling —
	// weather phases removed, James 2026-10-04 — not removed by W5e; the
	// measured pre-chunk set stays as measured and the two are excused here,
	// by name, from the dropped-column tooth.⟧
	struck := map[string]bool{"phase_at_dispatch": true, "phase_override_reason": true}
	var livePreChunk []string
	for _, c := range w5ePreChunkTripColumns {
		if !struck[c] {
			livePreChunk = append(livePreChunk, c)
		}
	}
	if missing := w5eMissingFrom(names, livePreChunk); len(missing) != 0 {
		t.Fatalf("W5-X4(a) / W5e-1: the pre-chunk `trip` columns %v are MISSING — W5e adds a column, it never removes one, and the permitted union in this file is measured against 88dd6a6. Got: %v", missing, names)
	}

	// W5e-1b — the column literal, asserted as a literal against the
	// INTROSPECTED name (never against a Go identifier).
	col, ok := w5eFindColumn(cols, w5eCargoNoteColumn)
	if !ok {
		t.Fatalf("W5e-1: the trip projection has no %q column — item 3's cargo note is unbuilt (columns present: %v)", w5eCargoNoteColumn, names)
	}
	// W5e-1b — and NO rival spelling anywhere on this table.
	if bad := w5eCargoShapedColumns(t, db, "trip", w5eRivalSpelling, map[string]bool{w5eCargoNoteColumn: true}); len(bad) != 0 {
		t.Fatalf("W5e-1b (m5e-1b): `trip` carries cargo-shaped column(s) %v beside the pinned literal %q. The spelling is frozen into hashed wire bytes and into W9a's LOCKED `Clear` vocabulary (plan:2254, design/trip-fact-correction-v1.md:211) — a second spelling is a both-posts-down respell later", bad, w5eCargoNoteColumn)
	}

	// W5e-1 / W5-X4(a) — declared SQL type, nullable, no default.
	if !strings.EqualFold(col.Type, "TEXT") {
		t.Fatalf("W5-X4(a) (SEAM W5e-S-COL): trip.%s has SQL type %q, want TEXT — the note is free text stored byte-faithfully", w5eCargoNoteColumn, col.Type)
	}
	if col.NotNull {
		t.Fatalf("W5e-1 (m5e-1): trip.%s is NOT NULL — the criterion is `nullable`, and absent must stay representable as SQL NULL (absent = not hauling, W5e-2)", w5eCargoNoteColumn)
	}
	if col.HasDflt {
		t.Fatalf("W5-X4(a) (m5e-1): trip.%s carries DEFAULT %q — a default silently rewrites an absent note into a present one, which is exactly the normalisation W5e-1 forbids", w5eCargoNoteColumn, col.DfltText)
	}
	if col.PK != 0 {
		t.Fatalf("W5-X4(a): trip.%s is part of the PRIMARY KEY (pk=%d) — it is a recorded note, not a key", w5eCargoNoteColumn, col.PK)
	}
	if !w5eIsStrict(t, db, "trip") {
		t.Fatalf("W5-X4(a): the `trip` table is no longer declared STRICT — STRICT must be preserved across the shape change")
	}

	// ⟦LOCK — §82, 2026-09-03⟧ ⛔ PART (b) IS DISCHARGED. It is a LOCK-TIME
	// criterion and its window's gate has passed.
	//
	// ⟦was: "\tif out := w5eOutsideUnion(names, permitted); len(out) != 0 {"⟧
	// ⟦was: "\t\tt.Fatalf(\"W5-X4(b) (m5-X4): `trip` carries column(s) %v outside the permitted union …\", out, w5ePreChunkTripColumns, w5eTripDeclaredAdditions)"⟧
	//
	// WHAT IT PROVED, and whose lock it served: at **W5e's gate**, that W5e's
	// change to `trip` was ADDITIVE and bounded — the pre-chunk twelve plus
	// exactly `cargo_note`, and nothing else smuggled in alongside. That is a
	// statement about W5e's DIFF, verified against the base W5e was built on,
	// and it did its job the moment W5e was gated and committed.
	//
	// ⚠ WHY IT CANNOT STAND: held forever it does not say "W5e was additive",
	// it says "`trip` MAY NEVER GAIN A COLUMN AGAIN" ⟦§82⟧. [MEASURED] W7 has
	// since added `vehicle_id` to `trip` — a sanctioned change, not a
	// regression — and this arm reported it as a violation.
	//
	// ⭐ THE ARM IS DISCHARGED, THE TEST IS NOT. Everything above — that
	// `cargo_note` is TEXT, nullable, defaultless, not part of the PK, and that
	// `trip` is still STRICT — is a STANDING property of the shipped column and
	// still asserted. [MEASURED, S82-W5-CLUSTER] every one of those arms passes
	// today, so discharging the whole test to clear one stale arm would have
	// deleted live coverage.
	//
	// ⚠ THE OBSERVATION IS KEPT AND PRINTED, not deleted: a reader still sees
	// which columns `trip` has gained beyond W5's union, without that being an
	// assertion. ⛔ Do NOT convert this back into a failure by adding the new
	// column to the union — that rebuilds the hand-kept enumeration this
	// project has now caught eleven times.
	if out := w5eOutsideUnion(names, permitted); len(out) != 0 {
		t.Logf("W5-X4(b) DISCHARGED ⟦§82⟧: `trip` carries %v beyond W5's permitted union (the pre-chunk twelve %v plus W5's declared additions %v). Recorded, not asserted — later windows may add columns to `trip`.",
			out, w5ePreChunkTripColumns, w5eTripDeclaredAdditions)
	}
}

// w5eOutsideUnion returns the members of got that are not in permitted.
func w5eOutsideUnion(got []string, permitted map[string]bool) []string {
	var out []string
	for _, g := range got {
		if !permitted[g] {
			out = append(out, g)
		}
	}
	sort.Strings(out)
	return out
}

// w5eMissingFrom returns the members of want absent from got.
func w5eMissingFrom(got []string, want []string) []string {
	have := map[string]bool{}
	for _, g := range got {
		have[g] = true
	}
	var out []string
	for _, w := range want {
		if !have[w] {
			out = append(out, w)
		}
	}
	return out
}

// --- W5e-4: cargo rides departures ONLY (D-21.3) ---------------------------

// w5eArrivalPayloadTypes is the REFLECTION SAMPLE — the arrival-side structs
// CONTROL A plants against and the real pass sweeps by reflection.
//
// ⟦LOCK — W9 post-lock amendment §37.1, plan owner, 2026-09-03⟧ It is NO LONGER
// required to be exhaustive. ⟦was: "are the arrival-side wire and projection
// structs reflected below. The list is NOT trusted on its own: the AST census
// in the same test derives every `*Arrival*` struct in internal/domain from
// source and fails if this list does not cover it (rule 6 — a hand list that
// silently stops covering its subject is the defect this project keeps
// finding)."⟧ The alarm was right and fired (W9a's and W9b's five new arrival
// structs); the REMEDY it implied — type five more names — re-arms the same
// defect every window. Completeness is now DERIVED, by
// `w5eArrivalStructHitsInTree`, from the property stated at rule 6 below.
// ⚠️ NOT a relaxation: the source census is strictly WIDER than this list — it
// also reaches the UNEXPORTED arrival structs a store-package reflection list
// can never name, and it gained the embedded-field arm so the one channel
// reflection had and it did not is now held by both.
var w5eArrivalPayloadTypes = []any{
	domain.TripArrival{},
	domain.ArrivalVisit{},
	domain.Arrival{},
	domain.ArrivalState{},
}

// w5eCargoShapedFields reports the fields of a struct type whose Go name OR
// whose json tag name is cargo-shaped. THE detector, shared by the plant and
// the real pass.
func w5eCargoShapedFields(rt reflect.Type) []string {
	var bad []string
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		if w5eRivalSpelling.MatchString(f.Name) {
			bad = append(bad, rt.Name()+"."+f.Name+" (field name)")
			continue
		}
		if tag != "" && w5eRivalSpelling.MatchString(tag) {
			bad = append(bad, rt.Name()+"."+f.Name+" (json tag "+tag+")")
		}
	}
	sort.Strings(bad)
	return bad
}

// w5ePlantedArrival is the CONTROL for the reflection half: a struct shaped
// exactly like the mutant (m5e-4) — an arrival payload that grew a cargo
// note. It exists ONLY to be detected.
type w5ePlantedArrival struct {
	Type      string  `json:"type"`
	ArrivalID string  `json:"arrival_id"`
	CargoNote *string `json:"cargo_note,omitempty"`
}

// TestW5eNoCargoOnArrivalStructural — W5e-4, [[D-21]].3 verbatim: "Cargo on
// unsolicited arrivals — LEAVE ALONE… we won't have manifests anyway so moot.
// Departures record cargo; unsolicited arrivals do not. Closed."
//
// ⟦NO PRE-CHUNK RED⟧ — cargo does not exist at 88dd6a6, so this test PASSES
// on arrival and its clean pass is NOT evidence. Its ledger evidence is the
// three planted controls below plus (m5e-4).
func TestW5eNoCargoOnArrivalStructural(t *testing.T) {
	// --- CONTROL A (reflection): the planted arrival payload must be flagged
	// on BOTH channels the real pass uses — the Go field name and the json tag.
	plantHits := w5eCargoShapedFields(reflect.TypeOf(w5ePlantedArrival{}))
	if len(plantHits) != 1 {
		t.Fatalf("W5e-4 CONTROL A FAILED: the struct-field detector found %d hit(s) in the planted arrival payload, want 1 (got %v) — a detector that cannot detect its plant proves nothing (m5e-4)", len(plantHits), plantHits)
	}
	plantTagOnly := w5eCargoShapedFields(reflect.TypeOf(struct {
		Manifest *string `json:"cargo_note,omitempty"`
	}{}))
	if len(plantTagOnly) == 0 {
		t.Fatalf("W5e-4 CONTROL A FAILED (tag channel): a field whose JSON TAG is cargo_note but whose Go name is not cargo-shaped went undetected — the wire is what D-21.3 governs, so the tag channel is the load-bearing one")
	}
	if clean := w5eCargoShapedFields(reflect.TypeOf(struct {
		Type       string  `json:"type"`
		RecordedBy *string `json:"recorded_by,omitempty"`
	}{})); len(clean) != 0 {
		t.Fatalf("W5e-4 CONTROL A (negative): the detector flagged a clean struct %v — it would false-red the real pass", clean)
	}

	// --- CONTROL B (introspection): a planted arrival table with the column.
	db := w5eOpenRaw(t, w5eFreshStore(t))
	if _, err := db.Exec(`CREATE TABLE w5e_ctl_arrival (
		arrival_id TEXT PRIMARY KEY,
		cargo_note TEXT
	) STRICT`); err != nil {
		t.Fatalf("CONTROL B setup: %v", err)
	}
	if bad := w5eCargoShapedColumns(t, db, "w5e_ctl_arrival", w5eCargoStrict, nil); len(bad) != 1 || bad[0] != w5eCargoNoteColumn {
		t.Fatalf("W5e-4 CONTROL B FAILED: the arrival-column detector missed a planted %q column on an arrival-shaped table (got %v) — (m5e-4)'s column half could not be caught", w5eCargoNoteColumn, bad)
	}

	// --- CONTROL C (AST census): a planted source file declaring an arrival
	// struct with the field must be reported by the same source census the
	// real pass runs.
	planted := `package domain

type ArrivalPlanted struct {
	Type      string  ` + "`json:\"type\"`" + `
	CargoNote *string ` + "`json:\"cargo_note,omitempty\"`" + `
}
`
	if hits := w5eArrivalStructHits(t, "planted/arrival.go", []byte(planted)); len(hits) != 1 {
		t.Fatalf("W5e-4 CONTROL C FAILED: the source census found %d hit(s) in the planted arrival struct, want 1 (got %v)", len(hits), hits)
	}
	cleanSrc := `package domain

type ArrivalClean struct {
	Type      string  ` + "`json:\"type\"`" + `
	ArrivalID string  ` + "`json:\"arrival_id\"`" + `
}
`
	if hits := w5eArrivalStructHits(t, "planted/clean.go", []byte(cleanSrc)); len(hits) != 0 {
		t.Fatalf("W5e-4 CONTROL C (negative): the source census flagged a clean arrival struct: %v", hits)
	}

	// --- rule 6 ⟦LOCK — W9 post-lock amendment §37.1, plan owner, 2026-09-03⟧
	// ⟦was: "the reflected hand-list must COVER every arrival struct the source
	// census can see. A hand list that silently stops covering its subject is
	// the failure this project keeps finding." — that requirement, and the
	// `uncovered` sweep that enforced it, are SUPERSEDED, not deleted quietly.⟧
	//
	// THE PROPERTY, stated once, with the list DERIVED from it: EVERY
	// arrival-shaped struct declared in internal/domain — exported or not,
	// named fields or EMBEDDED — carries no cargo-shaped field, by Go name or
	// by json tag. `w5eArrivalStructHitsInTree` below IS that derivation; it
	// walks the source and types no name, so it cannot go short.
	//
	// WHY the direction changed: the hand list went short on W9 (W9a's
	// ArrivalAmended / ArrivalAmendment / ComposedArrival and W9b's
	// ArrivalRefRetraction / ArrivalRefRetractionRow), and "add five entries"
	// re-arms the identical defect for W9c and every window after. This is the
	// ninth hand-kept enumeration in this project to go short — all nine short,
	// never long. `W5-X6`, W9's already-true block and `W6-X6` were each
	// repaired the same way: state the property, derive the list.
	//
	// ⚠️ WHAT SURVIVES, so this is a re-pointing and not a hole: CONTROL A
	// still plants against the reflection detector; the reflection sweep below
	// still runs over the sample; the empty-universe fixture guard still fires;
	// and the ROTTED-SAMPLE direction is still checked, below.
	reflected := map[string]bool{}
	for _, v := range w5eArrivalPayloadTypes {
		rt := reflect.TypeOf(v)
		if rt.Kind() != reflect.Struct {
			t.Fatalf("W5e-4 fixture defect: %T is not a struct", v)
		}
		reflected[rt.Name()] = true
	}
	declared := w5eArrivalStructNames(t)
	if len(declared) == 0 {
		t.Fatalf("W5e-4 fixture defect: the source census found NO arrival-shaped struct in internal/domain — the scan universe is empty and every verdict below would be vacuous")
	}
	// The surviving direction: an entry the source census CANNOT see is a
	// rotted sample — renamed, unexported or moved out of internal/domain —
	// and it means CONTROL A is planting against a type the real pass no
	// longer reaches. That is still a fixture defect and still fatal.
	declaredSet := map[string]bool{}
	for _, n := range declared {
		declaredSet[n] = true
	}
	var rotted []string
	for n := range reflected {
		if !declaredSet[n] {
			rotted = append(rotted, n)
		}
	}
	sort.Strings(rotted)
	if len(rotted) != 0 {
		t.Fatalf("W5e-4 (rule 6, as amended §37.1): w5eArrivalPayloadTypes reflects %v, which the source census does NOT find declared in internal/domain — the reflection sample has ROTTED, so CONTROL A plants against a type the real pass no longer reaches. Declared: %v", rotted, declared)
	}

	// --- the real assertions --------------------------------------------
	for _, v := range w5eArrivalPayloadTypes {
		rt := reflect.TypeOf(v)
		if bad := w5eCargoShapedFields(rt); len(bad) != 0 {
			t.Fatalf("W5e-4 (m5e-4) [[D-21]].3: arrival payload %s carries cargo-shaped field(s) %v. \"Departures record cargo; unsolicited arrivals do not. Closed.\" — cargo rides TripDispatched only", rt.Name(), bad)
		}
	}
	if hits := w5eArrivalStructHitsInTree(t); len(hits) != 0 {
		t.Fatalf("W5e-4 (m5e-4) [[D-21]].3: source census found cargo-shaped field(s) on arrival struct(s) in internal/domain:\n%v", hits)
	}
	if bad := w5eCargoShapedColumns(t, db, "trip_arrival", w5eRivalSpelling, nil); len(bad) != 0 {
		t.Fatalf("W5e-4 (m5e-4) [[D-21]].3: `trip_arrival` carries cargo-shaped column(s) %v — the arrival side records no cargo", bad)
	}

	// Whole-database census: a cargo-shaped column may exist ONLY on a table
	// that projects the trip fact vocabulary, and only as that vocabulary's
	// own cargo fact. This is the tooth that catches a cargo column added to
	// `visit`, `arrival_state` or a new side table.
	//
	// --- ⟦LOCK — W9 post-lock amendment §38, plan owner, 2026-09-03⟧
	// THE PERMITTED SET IS DERIVED FROM THE FACT VOCABULARY, NEVER FROM TABLE
	// NAMES. ⟦was: "want := []string{"trip." + w5eCargoNoteColumn}" with
	// "for _, f := range found { if f != want[0] { … cargo-shaped column %q
	// exists outside `trip` … } }" — a ONE-ENTRY hand list, preserved here at
	// the point of use.⟧
	//
	// WHY the direction changed, and what the one-entry list could not say:
	// W9a's `trip_amendment` projection carries `cargo_note` because the
	// LOCKED trip fact vocabulary CONTAINS `cargo_note` — the column is
	// entailed by the vocabulary, not chosen — so the list was red against a
	// table it had no way to name, and deleting the column would make cargo
	// the one trip fact that cannot be amended. §38 measured three
	// name-shaped repairs and rejected all three; this is the surviving one.
	//
	//   THE PROPERTY, stated once: a cargo-shaped column may exist only on a
	//   table whose COLUMN SET projects EVERY fact in the trip carrier's
	//   scalar vocabulary (domain.TripFactVocabulary), and the column must BE
	//   that vocabulary's cargo fact (domain.TripCargoFact). No table name is
	//   typed, here or in the accessor.
	//
	// ⚠ NOT a relaxation. Three directions, each MEASURED:
	//   - `visit.cargo_note` REDS. `visit` carries `trip_id` and no
	//     `arrival_id`, so §38's rejected derivation (1) would have PERMITTED
	//     it — silently, guard still green — on a table the clause's own
	//     comment names as a target. Here it reds, because `visit` projects
	//     none of driver_person_id / vehicle_entered / destination /
	//     phase_override_reason.
	//   - A RIVAL SPELLING reds even ON `trip`: `trip.cargo_class` is
	//     cargo-shaped and is not the cargo fact, so the first arm fires.
	//     The one-entry list caught that only by accident of inequality.
	//   - A FUTURE `trip_correction` joins the permitted set with NO edit
	//     here iff it really does project the vocabulary, and is excluded
	//     with no edit here if it does not. That is what §38's rejected
	//     derivation (2) — `trip` plus "<trip table>_amendment" — could not
	//     do without going short, which is the ninth instance of that rot.
	type w5eCargoHit struct{ table, column string }
	var found []string
	var hits []w5eCargoHit
	for _, tbl := range w5eUserTables(t, db) {
		if strings.HasPrefix(tbl, "w5e_ctl_") {
			continue // this test's own plants
		}
		for _, c := range w5eCargoShapedColumns(t, db, tbl, w5eCargoStrict, nil) {
			found = append(found, tbl+"."+c)
			hits = append(hits, w5eCargoHit{table: tbl, column: c})
		}
	}
	sort.Strings(found)

	// The derivation's OWN integrity, and it can fail: the pinned projection
	// literal (W5e-1b) and the vocabulary's cargo fact must be the same
	// string. They are two independent sources for one name, and the permitted
	// set now reads the vocabulary — so a rename there would re-point this
	// criterion at a different column while leaving it green, which is the
	// silent-weakening failure mode §38 rejected derivation (1) for.
	if domain.TripCargoFact() != w5eCargoNoteColumn {
		t.Fatalf("W5e-4 (§38) DERIVATION DEFECT: the trip carrier's cargo fact is %q but the pinned projection literal (W5e-1b) is %q. The permitted set is derived from the vocabulary, so these two must not drift — a rename on one side would silently re-point this criterion at a column D-21.3 never ruled on", domain.TripCargoFact(), w5eCargoNoteColumn)
	}

	// Pre-chunk no cargo-shaped column exists anywhere and the census is
	// EMPTY; that is lawful and always was. What is never lawful is a
	// cargo-shaped column that is not the cargo fact, or a cargo fact on a
	// table that does not project the vocabulary it belongs to.
	for _, h := range hits {
		qual := h.table + "." + h.column
		if h.column != w5eCargoNoteColumn {
			t.Fatalf("W5e-4 (m5e-1b/m5e-4) [[D-21]].3: cargo-shaped column %q is not the trip carrier's cargo fact %q. Cargo rides departures ONLY, and it rides them under ONE spelling — the spelling is frozen into hashed wire bytes and into W9a's LOCKED `Clear` vocabulary. The whole-schema census found: %v", qual, domain.TripCargoFact(), found)
		}
		if missing := domain.TripFactsNotProjectedBy(w5eColumnNames(w5eColumns(t, db, h.table))); len(missing) != 0 {
			t.Fatalf("W5e-4 (m5e-4) [[D-21]].3: cargo-shaped column %q sits on a table that does NOT project the trip fact vocabulary — it projects no %v (the vocabulary is %v). \"Departures record cargo; unsolicited arrivals do not. Closed.\" ⟦§38⟧ A cargo-shaped column may exist only on a table that projects the TRIP fact vocabulary, and the permitted set is derived from that vocabulary, NEVER from table names — so this is not repaired by naming the table here. The whole-schema census found: %v", qual, missing, domain.TripFactVocabulary(), found)
		}
	}
}

// --- the arrival source census (AST) --------------------------------------

func w5eModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the package directory — scan root unresolvable")
		}
		dir = parent
	}
}

func w5eDomainSources(t *testing.T) []string {
	t.Helper()
	dir := filepath.Join(w5eModuleRoot(t), "internal", "domain")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	sort.Strings(out)
	return out
}

var w5eArrivalTypeName = regexp.MustCompile(`(?i)arrival`)

// w5eArrivalStructHits parses one source and reports every cargo-shaped field
// on a struct type whose NAME is arrival-shaped.
func w5eArrivalStructHits(t *testing.T, path string, src []byte) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var hits []string
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || !w5eArrivalTypeName.MatchString(ts.Name.Name) {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok || st.Fields == nil {
			return true
		}
		for _, fld := range st.Fields.List {
			tagName := ""
			if fld.Tag != nil {
				if raw, err := strconv.Unquote(fld.Tag.Value); err == nil {
					tagName = strings.Split(reflect.StructTag(raw).Get("json"), ",")[0]
				}
			}
			// ⟦LOCK — W9 post-lock amendment §37.1, plan owner, 2026-09-03⟧
			// EMBEDDED fields, NEW. go/ast gives an anonymous field an EMPTY
			// `fld.Names`, so the loop below never ran for one and
			// `type ArrivalX struct{ CargoNote }` went UNDETECTED by this
			// census [MEASURED — the plant produced `[]` before this arm and
			// `[ArrivalAmended.CargoNote (embedded)]` after]. `reflect` reports
			// an embedded field under its type's own name and always caught it,
			// so this arm is precisely what lets the reflection sample stop
			// being the completeness authority WITHOUT narrowing the check.
			if len(fld.Names) == 0 {
				if nm := w5eEmbeddedName(fld.Type); nm != "" &&
					(w5eRivalSpelling.MatchString(nm) || (tagName != "" && w5eRivalSpelling.MatchString(tagName))) {
					hits = append(hits, path+":"+strconv.Itoa(fset.Position(fld.Pos()).Line)+" "+ts.Name.Name+"."+nm+" (embedded)")
				}
				continue
			}
			for _, nm := range fld.Names {
				if w5eRivalSpelling.MatchString(nm.Name) || (tagName != "" && w5eRivalSpelling.MatchString(tagName)) {
					hits = append(hits, path+":"+strconv.Itoa(fset.Position(nm.Pos()).Line)+" "+ts.Name.Name+"."+nm.Name)
				}
			}
		}
		return true
	})
	sort.Strings(hits)
	return hits
}

// w5eEmbeddedName returns the PROMOTED field name of an embedded struct field
// — the final identifier of `T`, `*T`, `pkg.T` or `*pkg.T` — or "" when the
// expression carries no embeddable name.
// ⟦LOCK — W9 post-lock amendment §37.1, plan owner, 2026-09-03⟧ NEW.
func w5eEmbeddedName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	case *ast.StarExpr:
		return w5eEmbeddedName(x.X)
	}
	return ""
}

func w5eArrivalStructHitsInTree(t *testing.T) []string {
	t.Helper()
	var all []string
	for _, p := range w5eDomainSources(t) {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		all = append(all, w5eArrivalStructHits(t, p, src)...)
	}
	sort.Strings(all)
	return all
}

// w5eArrivalStructNames lists every arrival-shaped struct type declared in
// internal/domain — the completeness check behind the reflected hand list.
func w5eArrivalStructNames(t *testing.T) []string {
	t.Helper()
	var names []string
	for _, p := range w5eDomainSources(t) {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok || !w5eArrivalTypeName.MatchString(ts.Name.Name) {
				return true
			}
			if _, ok := ts.Type.(*ast.StructType); !ok {
				return true
			}
			if !ts.Name.IsExported() {
				return true
			}
			names = append(names, ts.Name.Name)
			return true
		})
	}
	sort.Strings(names)
	return names
}
