// w8_schema_scan_test.go — W8 (item 10, the trade register): W8-X1 and
// W8-X5, the two W8-wide criteria whose subject is the STORE's schema.
//
// Written from the LOCKED W8 criteria only. No built implementation was read.
//
// ⚠ THIS FILE IS `package store_test` AND w8_source_scans_test.go IS
// `package store`, and the split is forced rather than stylistic: W8-X1(3)
// and W8-X5 need `domain.Register`, and `package store` CANNOT import
// internal/domain — [MEASURED] "imports sitecontrol/internal/domain from
// w8_source_scans_test.go … import cycle not allowed in test". The scan
// helpers this file reuses (w6PlantTree, w6ProductionGoFiles, w6Unquote,
// w6HasFormatVerb, w6SyntacticJoin, w6ResolvedConstString, w6DDLKeywords,
// w5aStampSchemaVersion) are all `package store_test` too; the ones
// w8_source_scans_test.go reuses (w5dScopedSideTables, w1ScanGoTokens,
// w3NonDomainGoFiles) are all `package store`. No existing test was modified.
package store_test

import (
	"database/sql"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// ---------------------------------------------------------------------------
// W8-X1 — one CREATE literal per table, and nothing mutates a live table
// ---------------------------------------------------------------------------

// w8DDLKeywords is W8's OWN forbidden-keyword set ⟦LOCK — B-6 / FIL-F3(1)⟧.
//
// The landed w6DDLKeywords is exactly {CREATE TABLE, CREATE TRIGGER,
// CREATE INDEX, ALTER TABLE, ADD COLUMN} and BOTH assembly arms decide
// through it. ⚠ NONE of the four constructs W8 newly forbids is in it:
// DROP TABLE, DROP COLUMN, RENAME TO, RENAME COLUMN. So
//
//	sql := "DROP TABLE " + tbl
//	fmt.Sprintf("DROP TABLE %s;", tbl)
//
// — the two spellings W8-X1's own [INFERENCE] paragraph says a SQLite rename
// is tempted into, since SQLite has no ALTER COLUMN at all — BOTH PASS THE
// LANDED INSTRUMENT. W8's set is the landed five PLUS those four, and the `+`
// arm AND the format-template arm must both consult it.
var w8DDLKeywords = []string{
	"CREATE TABLE", "CREATE TRIGGER", "CREATE INDEX", "ALTER TABLE", "ADD COLUMN",
	"DROP TABLE", "DROP COLUMN", "RENAME TO", "RENAME COLUMN",
}

// w8MutatingDDL are the constructs that operate on an EXISTING table. W8-X1(4)
// forbids every one of them in production `internal/` and `cmd/`.
var w8MutatingDDL = []string{"ALTER TABLE", "ADD COLUMN", "DROP COLUMN", "DROP TABLE", "RENAME TO", "RENAME COLUMN"}

func w8DDLKeywordIn(sql string, set []string) string {
	up := strings.ToUpper(sql)
	for _, bad := range set {
		if strings.Contains(up, bad) {
			return bad
		}
	}
	return ""
}

// TestW8ForbiddenKeywordSetIsWiderThanTheLandedOne is W8-X1(1)'s own check:
// the four constructs W8 newly forbids must be IN W8's set, and the landed
// W6 set is measured here so the gap is visible rather than asserted.
//
// ⚠ This is not pedantry. It is the criterion's stated ground: a scan that
// greps ALTER TABLE|ADD COLUMN "would be a further instance of filter
// narrower than its subject, INSIDE THE GUARD WHOSE WHOLE PURPOSE IS TO CATCH
// THIS".
func TestW8ForbiddenKeywordSetIsWiderThanTheLandedOne(t *testing.T) {
	for _, must := range []string{"DROP TABLE", "DROP COLUMN", "RENAME TO", "RENAME COLUMN"} {
		if w8DDLKeywordIn(must+" x", w8DDLKeywords) == "" {
			t.Errorf("W8-X1(1): W8's forbidden-keyword set does not contain %q. W8 renames two live columns "+
				"(visit.workgroup_snapshot_{id,name}), drops one (person.workgroup_id) and stops creating two tables; "+
				"SQLite has no ALTER COLUMN, so the tempting spellings are DROP+recreate or a table rebuild, and NONE "+
				"of them contains ADD COLUMN", must)
		}
		// The landed set is measured, not assumed — this is the gap's evidence.
		if w8DDLKeywordIn(must+" x", w6DDLKeywords) != "" {
			t.Errorf("W8-X1(1) PREMISE MOVED: the landed w6DDLKeywords now contains %q. W8's set was minted "+
				"because it did not. Re-derive the set rather than carrying this one", must)
		}
	}
}

// w8DDLFault is one W8-X1 violation.
type w8DDLFault struct{ Where, Why string }

// w8ScanDDL walks a tree and reports every W8-X1 violation, over W8's OWN
// keyword set, on THREE arms that cannot see each other's shapes:
//
//	LITERAL         a string literal containing a MUTATING DDL construct;
//	`+` ASSEMBLY    a BinaryExpr whose SYNTACTIC join or whose go/types
//	                RESOLVED value carries a DDL keyword — both readings
//	                computed, either can raise;
//	FORMAT TEMPLATE a string constant carrying BOTH a DDL keyword AND a
//	                printf verb.
//
// ⚠ THE PRINTF MECHANISM IS H-3's, AND THE SUPERSEDED ACCOUNT OF IT IS FALSE:
// "go/types folds CONSTANTS. fmt.Sprintf is a CALL and is never constant", so
// a go/types derivation produces NOTHING for a Sprintf. The landed scan
// catches that shape by a DIFFERENT mechanism — keyword AND verb in one
// constant — added after a compiling production
// fmt.Sprintf("CREATE TABLE IF NOT EXISTS visit (%s) STRICT;", cols) returned
// ZERO faults. ⚠ A builder implementing the superseded words rebuilds the
// pre-H-3 scan.
func w8ScanDDL(t *testing.T, base string, roots []string) ([]w8DDLFault, s56Set) {
	t.Helper()
	// ⟦LOCK — §56(1), 2026-09-03⟧ every directory this scan type-checked.
	var diags s56Set
	byDir := map[string][]string{}
	var dirs []string
	for _, path := range w6ProductionGoFiles(t, base, roots) {
		dir := filepath.Dir(path)
		if _, seen := byDir[dir]; !seen {
			dirs = append(dirs, dir)
		}
		byDir[dir] = append(byDir[dir], path)
	}
	sort.Strings(dirs)

	var out []w8DDLFault
	seen := map[string]bool{}
	emit := func(where, why string) {
		k := where + "|" + why
		if seen[k] {
			return
		}
		seen[k] = true
		out = append(out, w8DDLFault{Where: where, Why: why})
	}

	for _, dir := range dirs {
		fset := token.NewFileSet()
		var parsed []*ast.File
		for _, path := range byDir[dir] {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			f, err := parser.ParseFile(fset, path, src, 0)
			if err != nil {
				emit(path, fmt.Sprintf("UNPARSEABLE — NOT scanned, so this scan's coverage is smaller than its stated scope: %v", err))
				continue
			}
			parsed = append(parsed, f)
		}
		if len(parsed) == 0 {
			continue
		}
		info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
		// ⟦LOCK — §56(1), 2026-09-03⟧ RECORDED, never discarded. ⟦was:
		// "conf := types.Config{Error: func(error) {}, Importer: nil, DisableUnusedImportCheck: true}"⟧
		// The RESOLVED-CONSTANT limb below reads `info`; a plant that does not
		// type-check yields no constant value, so the assembled-DDL arm goes
		// quiet and the harness reports "the plant was NOT reported".
		dg := s56Diags{Where: dir}
		conf := types.Config{Error: dg.Record, Importer: nil, DisableUnusedImportCheck: true}
		_, _ = conf.Check("w8ddlscan", fset, parsed, info)
		diags = append(diags, dg)

		for _, f := range parsed {
			ast.Inspect(f, func(n ast.Node) bool {
				switch e := n.(type) {
				case *ast.BasicLit:
					if e.Kind != token.STRING {
						return true
					}
					raw := w6Unquote(e.Value)
					if bad := w8DDLKeywordIn(raw, w8MutatingDDL); bad != "" {
						emit(fset.Position(e.Pos()).String(), "string literal containing "+bad+" — DDL against an EXISTING table")
						return true
					}
					// THE FORMAT-TEMPLATE ARM (H-3's mechanism).
					if w6HasFormatVerb(raw) {
						if bad := w8DDLKeywordIn(raw, w8DDLKeywords); bad != "" {
							emit(fset.Position(e.Pos()).String(),
								"DDL assembled from a FORMAT TEMPLATE ("+bad+") — the statement carries a printf verb, so its text arrives from OUTSIDE the literal")
						}
					}
				case *ast.BinaryExpr:
					if e.Op != token.ADD {
						return true
					}
					for _, reading := range []struct{ sql, how string }{
						{w6SyntacticJoin(e), "joined from the literal fragments inside the `+`"},
						{w6ResolvedConstString(info, e), "RESOLVED through go/types — its fragments are NAMED CONSTANTS declared elsewhere"},
					} {
						if bad := w8DDLKeywordIn(reading.sql, w8DDLKeywords); bad != "" {
							emit(fset.Position(e.Pos()).String(), "DDL ASSEMBLED by concatenation ("+bad+"), "+reading.how)
						}
					}
				}
				return true
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Where < out[j].Where })
	return out, diags
}

// TestW8SchemaScanHasSevenProducedArms is W8-X1's control set.
//
// ⟦LOCK — B-6 / FIL-F3(3): SEVEN arms, not five, applied ONE AT A TIME.
// ⚠ The five could not detect either blindness — a blind implementation and a
// correct one produce BYTE-IDENTICAL output on all five.⟧
//
//	1  ALTER TABLE … ADD COLUMN
//	2  a `+`-concatenated DDL string
//	3  a DROP COLUMN
//	4  a REBUILD SEQUENCE (the arm with no precedent)
//	5  a FORMAT-TEMPLATE CREATE — fmt.Sprintf("CREATE TABLE %s …"), the H-3
//	   shape, for which the landed instrument returns ZERO faults
//	6  an ASSEMBLED DROP — "DROP TABLE " + tbl — which the landed keyword set
//	   does not contain
//	7  the NEGATIVE arm — a plain concatenated SELECT that must NOT be flagged
//
// ⚠ Arms 5 and 6 FAIL AGAINST THE LANDED INSTRUMENT TODAY, which is exactly
// what makes them controls and not decoration. This test MEASURES that, so
// the claim is evidence rather than assertion.
func TestW8SchemaScanHasSevenProducedArms(t *testing.T) {
	arms := []struct {
		name      string
		src       string
		wantFault bool
	}{
		{"1 ALTER TABLE ... ADD COLUMN", "package pkg\n\nconst s = \"ALTER TABLE visit ADD COLUMN trade_snapshot_id TEXT\"\n", true},
		{"2 `+`-concatenated DDL", "package pkg\n\nconst head = \"CREATE TABLE IF NOT EXISTS trade (\"\nconst tail = \"trade_id TEXT PRIMARY KEY) STRICT;\"\nconst s = head + tail\n", true},
		{"3 DROP COLUMN", "package pkg\n\nconst s = \"ALTER TABLE person DROP COLUMN workgroup_id\"\n", true},
		{"4 REBUILD SEQUENCE (no precedent)", "package pkg\n\nconst a = \"CREATE TABLE visit_new (visit_id TEXT)\"\nconst b = \"INSERT INTO visit_new SELECT visit_id FROM visit\"\nconst c = \"DROP TABLE visit\"\nconst d = \"ALTER TABLE visit_new RENAME TO visit\"\n", true},
		{"5 FORMAT-TEMPLATE CREATE (H-3)", "package pkg\n\nconst s = \"CREATE TABLE IF NOT EXISTS trade (%s) STRICT;\"\n", true},
		{"6 ASSEMBLED DROP", "package pkg\n\nconst head = \"DROP TABLE \"\nconst tbl = \"workgroup\"\nconst s = head + tbl\n", true},
		{"7 NEGATIVE — plain concatenated SELECT", "package pkg\n\nconst head = \"SELECT * FROM trade WHERE \"\nconst tail = \"trade_id = ?\"\nconst s = head + tail\n", false},
	}
	for _, arm := range arms {
		root := w6PlantTree(t, map[string]string{"internal/pkg/plant.go": arm.src})
		got, pdiags := w8ScanDDL(t, root, []string{"internal"})
		// ⟦LOCK — §56(1)⟧ ASKED BEFORE THE ARM IS SCORED.
		if !s56PlantTypeChecked(t, "W8-X1 control arm "+arm.name, pdiags) {
			continue
		}
		if arm.wantFault && len(got) == 0 {
			t.Errorf("W8-X1 CONTROL FAILED, arm %s: the plant was NOT reported. A scan that cannot see this arm is a "+
				"filter narrower than its subject, inside the guard whose whole purpose is to catch that.\nPlant:\n%s"+
				"⟦§56⟧ the plant DID type-check, so this is the scan and not the fixture:\n%s", arm.name, arm.src, pdiags.Report())
		}
		if !arm.wantFault && len(got) != 0 {
			t.Errorf("W8-X1 CONTROL FAILED, arm %s (NEGATIVE): a plain concatenated SELECT was flagged: %v. "+
				"A guard that flags every `+` on a string is not a DDL guard", arm.name, got)
		}
	}

	// --- THE MEASURED PREMISE, stated precisely rather than over-claimed ---
	//
	// ⚠ THE CLAIM IS ABOUT THE KEYWORD SET, NOT ABOUT WHOLE ARMS. The lock
	// says arms 5 and 6 "FAIL against the landed instrument today, which is
	// exactly what makes them controls". Measured, that is TRUE OF THE
	// KEYWORD SET and only partly true of the arms: the landed
	// w6DDLKeywords carries CREATE TABLE, so arm 5's format template and
	// arm 4's rebuild CREATE are visible to it through that keyword. What
	// is invisible to it is every DROP/RENAME construct — which is arm 6
	// whole, and the half of arm 4 that makes a rebuild a rebuild.
	//
	// This is measured here rather than asserted, so the premise is
	// checkable and its correction is visible.
	for _, ddl := range []string{"DROP TABLE workgroup", "ALTER TABLE visit_new RENAME TO visit", "ALTER TABLE person DROP COLUMN workgroup_id"} {
		if w8DDLKeywordIn(ddl, w8DDLKeywords) == "" {
			t.Errorf("W8-X1(1): W8's own set does not see %q", ddl)
		}
	}
	if w8DDLKeywordIn("DROP TABLE workgroup", w6DDLKeywords) != "" {
		t.Errorf("W8-X1(1) PREMISE MOVED: the landed w6DDLKeywords now sees a bare DROP TABLE. W8's widened set was " +
			"minted because it did not — re-derive the set rather than carrying this one")
	}
	if w8DDLKeywordIn("ALTER TABLE visit_new RENAME TO visit", w6DDLKeywords) == "" {
		t.Errorf("W8-X1(1) NOTE: the landed set does not see a RENAME TO at all, not even through ALTER TABLE — " +
			"if this fires the measurement below is wrong and must be re-taken")
	}
}

// TestW8NoProductionStatementMutatesALiveTable is W8-X1(4) over the real tree.
//
// ⟦NO PRE-CHUNK RED — §2.6 row 1: zero DDL-mutation constructs at f33c4c7;
// the one ALTER TABLE-shaped hit in the tree is a COMMENT.⟧ Its evidence is
// the SEVEN produced arms above, never this clean pass.
func TestW8NoProductionStatementMutatesALiveTable(t *testing.T) {
	root := w6ModuleRoot(t)
	faults, diags := w8ScanDDL(t, root, []string{"internal", "cmd"})
	s56LogTree(t, "w8ScanDDL", diags)
	if len(faults) != 0 {
		var b strings.Builder
		for _, f := range faults {
			fmt.Fprintf(&b, "  %s: %s\n", f.Where, f.Why)
		}
		t.Fatalf("W8-X1(4) ((m8-X1)): production Go under internal/ or cmd/ executes DDL against an EXISTING table, "+
			"or assembles DDL rather than declaring it in one CREATE-containing literal:\n%s"+
			"⚠ W8 renames two live columns, drops one and stops creating two tables. SQLite has no ALTER COLUMN, so the "+
			"tempting spellings are DROP+recreate or a table rebuild — and none of them contains ADD COLUMN, which is why "+
			"this scan's keyword set is wider than the landed one", b.String())
	}
}

// TestW8EveryTableSchemaIsOneCreateLiteral is W8-X1(3), the DECLARATION half.
//
// ⟦LOCK — FIL-F8⟧ "THE DECLARATION HALF'S TABLE SET COMES FROM sqlite_master,
// NOT FROM THE PROJECTION SET … ⚠ the projection-scoped reading CANNOT SEE
// `visit`, which is the table W8 actually changes."
//
// THE TWO EXEMPTIONS ARE NAMED IN THE CRITERION WITH THEIR REASON, so a
// builder does not mint them under time pressure: `visit` and `trip`, whose
// Projection.Schema is empty because the store's FIRST-SCHEMA LITERAL owns
// them. Two rows a gate can check, rather than an exemption invented at build
// time.
func TestW8EveryTableSchemaIsOneCreateLiteral(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "w8-x1.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}

	// The table set is sqlite_master's, excluding only sqlite_*.
	type tbl struct{ name, sql string }
	var tables []tbl
	if err := st.LocalTx(func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT name, sql FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var name, s string
			if err := rows.Scan(&name, &s); err != nil {
				return err
			}
			tables = append(tables, tbl{name, s})
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("PRECONDITION: sqlite_master is unreadable, so this criterion's table set cannot be derived: %v", err)
	}
	if len(tables) == 0 {
		t.Fatal("PRECONDITION: sqlite_master reports no tables on a registered store — the instrument, not W8")
	}

	// The named exemptions, with their reason, as literals.
	exempt := map[string]string{
		"visit": "the store's FIRST-SCHEMA literal owns it; its Projection.Schema is empty",
		"trip":  "the store's FIRST-SCHEMA literal owns it; its Projection.Schema is empty",
	}
	// --- CONTROL FIRST, PRODUCED: the predicate is driven over a SYNTHETIC
	// table list carrying the two faults it exists to see. ⚠ Without this the
	// clean pass below is a green over a predicate that may report nothing —
	// "an un-controlled green is not coverage, and a check that cannot fail
	// can be ACTIVE COVER".
	for _, ctl := range []struct{ name, sql, why string }{
		{"w8_plant_no_create", "trade_id TEXT PRIMARY KEY", "no CREATE in the declaration"},
		{"w8_plant_drop", "CREATE TABLE w8_plant_drop (x TEXT); DROP TABLE workgroup", "a MUTATING construct in the schema text"},
	} {
		if faults := w8SchemaTextFaults(ctl.name, ctl.sql); len(faults) == 0 {
			t.Fatalf("W8-X1(3) CONTROL FAILED (%s): the planted schema text %q was NOT reported. A predicate that "+
				"cannot see its own plant proves nothing about the real tables below", ctl.why, ctl.sql)
		}
	}
	if faults := w8SchemaTextFaults("w8_plant_clean", "CREATE TABLE w8_plant_clean (x TEXT) STRICT;"); len(faults) != 0 {
		t.Fatalf("W8-X1(3) CONTROL FAILED (negative): a clean CREATE was flagged: %v", faults)
	}

	for _, tb := range tables {
		for _, f := range w8SchemaTextFaults(tb.name, tb.sql) {
			t.Errorf("W8-X1(3): %s", f)
		}
	}
	// The exemption rows are asserted to still BE exemptions — a dead
	// exemption is the shrink-tooth shape one criterion over.
	for name, why := range exempt {
		found := false
		for _, tb := range tables {
			if tb.name == name {
				found = true
			}
		}
		if !found {
			t.Errorf("W8-X1(3): the criterion names %q as an exemption (%s) and sqlite_master does not report it. "+
				"An exemption row with no subject is a row nobody will remove", name, why)
		}
	}
}

// ---------------------------------------------------------------------------
// W8-X5 — W8 does NOT bump store.SchemaVersion
// ---------------------------------------------------------------------------

// TestW8RidesTheV16ShapeAndAV15StoreIsRefused is ⟦DR-W8-15⟧'s row,
// closed by a RULING rather than left blank, on W8's OWN argument.
//
// ⚠ W5d's ground is NOT cloned and the criterion says why: register.go's
// ground is "a NEW table, so IF NOT EXISTS heals a stored database on next
// open". IF NOT EXISTS heals an ADDED table; it heals NEITHER a dropped
// column, NOR a renamed column, NOR a table that is no longer created — and
// W8 does all three. W8's ground is the DEPLOYMENT-BOUNDARY one, which is
// indifferent to direction and therefore reaches W8's removals: the v10 log
// starts empty (D-26 part 2, in those words; D-29 for the roster half), §7
// step 2 deletes every database at the cutover, one hash-verified binary
// ships to both posts.
//
//	(i)   a store stamped the literal predecessor '11' is refused at Open by a
//	      non-nil UNTYPED error naming both versions and "refusing";
//	(ii)  the fixture stamps the LITERAL '11', never store.SchemaVersion-1;
//	(iii) StoredSchemaVersion() == fmt.Sprint(store.SchemaVersion) AND == "12"
//	      ⟦re-pinned 2026-09-11 at the 11 -> 12 increment (R-16); was "11"⟧.
//
// ⚠ ASSERTION FORM: Contains(…) && Contains(…, "refusing") — NEVER
// errors.As(&store.SchemaVersionError{}), which is not on the Open path.
// ⚠ W6 recorded a known instrument weakness and W8 inherits it KNOWINGLY: the
// refusal message interpolates the STORE PATH, which can itself contain a 9,
// so Contains(err.Error(), "9") can pass on a path. W8 asserts the substring
// "schema_version 9 " — this is OW-7 arm (i)'s STRONGER form, and the
// residue (w1_version_gate_test.go:53 asserts a bare "9") is declined and
// booked to P-4 for W10.
//
// ⟦NO PRE-CHUNK RED — §2.6 row 2: store.SchemaVersion is already 10.⟧ Its evidence
// is the pre-bump CONTROL and (m8-X5).
// ⟦was: TestW8RidesTheV11ShapeAndAV10StoreIsRefused — re-pinned 11 -> 12 (R-16,
// 2026-09-11); W8 still bumped nothing, the NEXT WINDOW did, again⟧
// ⟦was: TestW8RidesTheV14ShapeAndAV13StoreIsRefused — re-pinned 14 -> 15 (R-20 +
// R-23, 2026-09-13); W8 still bumped nothing, the rejoin path did⟧
// ⟦was: TestW8RidesTheV15ShapeAndAV14StoreIsRefused — re-pinned 15 -> 16 (R3
// alert_kind, 2026-10-04); W8 still bumped nothing, R3 did⟧
func TestW8RidesTheV16ShapeAndAV15StoreIsRefused(t *testing.T) {
	dir := t.TempDir()

	// --- CONTROL FIRST: a CURRENT-version store must Open successfully. ----
	// Without it a fixture that refuses for ANY reason — a corrupted rewrite,
	// a wrong path — reads as a pass.
	ctlPath := filepath.Join(dir, "w8-x5-control.db")
	w5aStampSchemaVersion(t, ctlPath, fmt.Sprint(store.SchemaVersion))
	ctl, err := store.Open(ctlPath)
	if err != nil {
		t.Fatalf("W8-X5 CONTROL FAILED (pre-bump control): a store stamped the CURRENT version %d did not Open: %v — "+
			"the fixture rewrite is broken, so the '9' refusal below would prove nothing", store.SchemaVersion, err)
	}
	if err := ctl.Close(); err != nil {
		t.Fatal(err)
	}

	// --- (iii) the KILLING ARM, pinned to the LITERAL, of the same artefact.
	livePath := filepath.Join(dir, "w8-x5.db")
	st, err := store.Open(livePath)
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
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprint(store.SchemaVersion); stored != want {
		t.Errorf("W8-X5(iii): the registered store reads back schema_version %q, want %q (= fmt.Sprint(store.SchemaVersion))", stored, want)
	}
	if stored != "16" {
		t.Errorf("W8-X5(iii) ((m8-X5, RE-POINTED AGAIN): a chunk that BUMPS store.SchemaVersion): the store reads back schema_version %q, want \"16\" — "+
			"the 15 -> 16 increment's EXIT version (re-pinned 2026-10-04, R3 alert_kind; was re-pinned to 15 by R-20 + R-23, to 14 by R-18, to 13 by R-17, and to 12 by R-16; W8 itself bumped nothing any time). ⟦DR-W8-15⟧ rules that W8 does NOT bump: the v10 log starts empty, the cutover deletes "+
			"every database, and one hash-verified binary ships to both posts, so W8's removals cross no deployment boundary; this literal just tracks whatever the CURRENT version is. "+
			"⚠ W5d's ground (IF NOT EXISTS heals a NEW table) does NOT reach W8 and must not be cited for it", stored)
	}

	// --- (i)/(ii) the '15' edge, stamped as the LITERAL. -------------------
	// ⟦re-pinned from '11' at the 12 -> 13 increment, 2026-09-11; from '12' at
	// the 13 -> 14 increment, 2026-09-13; from '13' at the 14 -> 15 increment,
	// 2026-09-13; from '14' at the 15 -> 16 increment, 2026-10-04: the literal
	// predecessor is the version the schema-15 window actually ran⟧
	tenPath := filepath.Join(dir, "w8-x5-fifteen.db")
	w5aStampSchemaVersion(t, tenPath, "15")
	_, err = store.Open(tenPath)
	if err == nil {
		t.Fatalf("W8-X5(i): a store stamped the literal predecessor '15' OPENED. It must be refused")
	}
	msg := err.Error()
	if !strings.Contains(msg, "schema_version 15 ") {
		t.Errorf("W8-X5(i): the '15' refusal message %q does not contain the substring \"schema_version 15 \". "+
			"⚠ This is asserted rather than a bare \"9\" for a MEASURED reason W6 recorded and W8 inherits knowingly: "+
			"the message interpolates the STORE PATH, so Contains(err.Error(), \"9\") can pass on a path that merely "+
			"contains the digit — a refusal a store path can satisfy", msg)
	}
	if !strings.Contains(msg, fmt.Sprint(store.SchemaVersion)) {
		t.Errorf("W8-X5(i): the refusal message %q does not name the binary's version %d — it must name BOTH versions", msg, store.SchemaVersion)
	}
	if !strings.Contains(msg, "refusing") {
		t.Errorf("W8-X5(i): the refusal message %q does not contain \"refusing\"", msg)
	}
}

// w8SchemaTextFaults is W8-X1(3)'s predicate over ONE table's stored schema
// text, factored out so its own controls can drive it.
func w8SchemaTextFaults(name, sqlText string) []string {
	var out []string
	if !strings.Contains(strings.ToUpper(sqlText), "CREATE ") {
		out = append(out, fmt.Sprintf("table %q's stored schema text carries no CREATE — its declaration is not one CREATE-containing literal", name))
	}
	// ⚠⚠ A DEFECT THIS FILE'S OWN CONTROL FOUND, recorded rather than tidied
	// away. The first version of this predicate suppressed the mutating-DDL
	// arm whenever the schema text STARTED WITH `CREATE TABLE` — which every
	// stored sqlite_master.sql does — SO THE ARM COULD NOT FIRE AT ALL. The
	// plant `CREATE TABLE w8_plant_drop (x TEXT); DROP TABLE workgroup` was
	// scored CLEAN [PRODUCED 2026-09-03: "W8-X1(3) CONTROL FAILED (a MUTATING
	// construct in the schema text): the planted schema text … was NOT
	// reported"]. A stored table declaration never legitimately contains a
	// DROP / ALTER / RENAME, so the guard bought nothing and cost the arm.
	if bad := w8DDLKeywordIn(sqlText, w8MutatingDDL); bad != "" {
		out = append(out, fmt.Sprintf("table %q's schema text carries %q — DDL against an EXISTING table", name, bad))
	}
	return out
}
