// w7_source_scans_test.go — W7's SOURCE-SCAN halves (item 9 then item 8).
//
// design/WINDOW-BUILD-PLAN-v1.md `### W7 —` at :7421, criteria LOCKED
// 2026-09-03. Written from the CRITERION TEXT ONLY.
//
// Criteria carried here:
//
//   - W7-6(a) — no foreign key anywhere, ONE ROOT stated once (`internal/`
//     and `cmd/`, production `.go` only), the match CASE-INSENSITIVE and
//     WHITESPACE-TOLERANT, and a NEGATIVE ARM with real content.
//   - W7-X1 — every schema change declared inside a single `CREATE`-containing
//     Go string literal: no `ALTER TABLE`, no `ADD COLUMN`, no
//     `DROP TABLE`/`RENAME TO` rebuild, no `PRAGMA writable_schema` /
//     `sqlite_master` rewrite, no DDL assembled by `+` concatenation — plus
//     the NEGATIVE arm, a `+`-concatenated `SELECT` that must NOT be flagged.
//   - W7-9(c) — the ORDER-BY scan on the RESPECIFIED instrument: scoped by
//     RESOLVED CONSTANT VALUE through `go/types`, over the VEHICLE spelling,
//     with FOUR controls re-produced on it and the LIVENESS FLOOR.
//   - W7-11(a) — `vehicleFoldKey` is DECLARED ONCE and shared.
//   - W7-25(e) — `IsPlaceholderVehicleID` has EXACTLY ONE production call
//     site and it is not at an authoring door.
//
// ⚠ RULE 20 IS OBEYED IN THE STRONGER DIRECTION. The rule says `go test
// -overlay` does not reach a source scan, so such a scan is mutation-tested
// on a real on-disk copy with the revert proved by `cmp`. Every control below
// writes its plant into a REAL ON-DISK DIRECTORY (`t.TempDir()`) and runs the
// same walk + filter + judgement function over it. Nothing under `app/` is
// ever written to, so there is no revert to prove and no window in which
// another agent's `go test` sees a mutated tree — which matters tonight,
// because W9a's, W9b's and W9c's builds are in this working tree uncommitted.
// The judgement functions are shared by the control and the assertion, so a
// control red proves the SAME walk the clean pass runs.
//
// ⚠ THIS FILE NAMES NO UNBUILT GO SYMBOL — every subject is named as a
// STRING and resolved by the walk — so it compiles and runs at e9cc5f5.
//
// EXPECTED RED at e9cc5f5 (all runtime):
//   - TestW7VehicleAmendmentOrderBySpellingByResolvedValue: LIVENESS FLOOR —
//     `Liveness["vehicle_amendment"] == 0`, i.e. the vehicle composition is
//     unbuilt or spelled differently.
//   - TestW7VehicleFoldKeyIsDeclaredExactlyOnce: 0 declarations, want 1.
//   - TestW7PlaceholderGuardHasOneCallSiteAndItIsNotAnAuthoringDoor: TWO call
//     sites, both at authoring doors — the relocation has not happened.
//
// ⟦NO PRE-CHUNK RED⟧, named with the plant that would falsify each:
//   - TestW7NoForeignKeyAnywhere (W7-6(a), §2.4 row 1) — plant a
//     `REFERENCES vehicle(vehicle_id)` clause in the `trip` DDL literal.
//   - TestW7SchemaChangesLiveInCreateLiterals (W7-X1, §2.4 row 6) — plant an
//     `ALTER TABLE ... ADD COLUMN` in a production `.go` file.
//
// KILLER MUTANTS (§5):
//
//	(m7-6)   an FK-style refusal / an FK declared    -> W7-6(a)
//	(m7-9b)  a deviant or epoch-blind ORDER BY inside a `vehicle_amendment`
//	         literal                                 -> W7-9(c)
//	(m7-11)  NormalizeVehicleID widened into the fold key
//	                                                 -> W7-11 (structural)
//	(m7-11b) the fold key stored / used as a correlation or merge input
//	(m7-25d) the relocated guard ALSO left at a dispatch or arrival site
//	                                                 -> W7-25(e)
//	(m7-X1)  five arms, four positive and one negative
//	                                                 -> W7-X1
package store_test

import (
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// w7ModuleRoot walks up from the working directory to the directory holding
// go.mod. W7 mints its own rather than calling W1's: W7-X3 forbids another
// chunk's green counting toward W7's, and a helper shared across chunk
// boundaries is a dependency that can be deleted out from under it.
func w7ModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("fixture defect: no go.mod found above the working directory")
		}
		dir = parent
	}
}

// w7ProductionRoots is THE ROOT, STATED ONCE (W7-6(a), W7-5, W7-11, W7-X1 all
// say "`internal/` and `cmd/`, production `.go` only"). ⚠ Two packages is not
// the application: revision 1's `internal/domain`+`internal/web` root was
// struck at ⟦LOCK — FIL-F5 / FIL-F10⟧ precisely because `internal/deadline`
// is a live consumer of the vehicle value and sits outside it.
func w7ProductionRoots(t *testing.T) []string {
	t.Helper()
	root := w7ModuleRoot(t)
	return []string{filepath.Join(root, "internal"), filepath.Join(root, "cmd")}
}

// w7ProductionGoFiles walks the roots RECURSIVELY for non-test `.go` files.
// ⚠ Recursion is load-bearing: a flat ReadDir over two named directories is
// the "filter narrower than its subject" class, and this project has measured
// that class seven times in code and eleven more in W7's own draft.
func w7ProductionGoFiles(t *testing.T, roots ...string) []string {
	t.Helper()
	var out []string
	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			out = append(out, path)
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", root, err)
		}
	}
	if len(out) == 0 {
		t.Fatalf("fixture defect: the production walk over %v found no files — a scan over an empty corpus "+
			"reports zero violations for the wrong reason", roots)
	}
	sort.Strings(out)
	return out
}

// w7WriteSource writes one planted source file into a REAL on-disk directory
// and returns the directory. Rule 20's remedy, in the form that never touches
// `app/`.
func w7WriteSource(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// =========================================================================
// W7-6(a) — no foreign key anywhere
// =========================================================================
//
// ⟦NO PRE-CHUNK RED — §2.4 row 1⟧ There is no `FOREIGN KEY` and no
// `REFERENCES` clause anywhere in the schema: analytics-v1.md:932-933 states it
// in terms, and every `CREATE TABLE` literal read at the drafting confirmed
// it. So the evidence for this criterion is the PLANTED FK-style construct —
// (m7-6) — and never the clean census.
//
// ⚠ THE NEGATIVE ARM IS THE HALF THAT WAS BROKEN AND IS FIXED HERE
// (⟦LOCK — FIL-F7 / FAL-F8⟧). Revision 1 stated the root as production `.go`
// only and then required two hits in `_test.go` files not to be flagged —
// a corpus the root never reads, so the required control COULD NOT FIRE.
// A control that cannot fire is not a control.
//
// ⚠⚠ MEASURED DISAGREEMENT WITH THE LOCK, REPORTED RATHER THAN SILENTLY
// ACCOMMODATED. The lock states the in-corpus decoys as "8 hits of
// `references`, every one of them comment prose", naming
// `internal/deadline/ack.go:20`; `internal/domain/time_correct.go:31, :315,
// :330, :348, :411`; `internal/domain/trip.go:560, :853`.
// [MEASURED 2026-09-03, over the same corpus, `grep -arni "references"]:
// NINE — the eight above plus `internal/domain/time_correct.go:32`, and
// `trip.go`'s two are at `:569` and `:889` on this working tree, not `:560`
// and `:853`. `SEAM-W7-14` predicts the line drift (W6's locked text and the
// working tree are different artefacts) but the COUNT is short by one too.
// ⚠ THERE IS ALSO A DECOY THE LOCK DID NOT NAME: `internal/store/store.go`'s
// DSN carries `_pragma=foreign_keys(1)` — a PRAGMA, not a constraint — which
// a case-insensitive `foreign key` match flags and which must not be.
//
// So the negative arm is asserted as a PROPERTY and the decoys are DERIVED,
// never pinned as a count: *no comment-prose or PRAGMA occurrence is
// flagged*, with a FLOOR (the corpus must actually contain some, or the arm
// is protecting nothing). A pinned "eight" here would be this project's
// hand-kept-enumeration class committed inside the remedy for it.
func TestW7NoForeignKeyAnywhere(t *testing.T) {
	// --- CONTROL FIRST, three arms, each alone (W7-6(a)'s own list) -------

	// (i) a planted UPPER-CASE `REFERENCES vehicle(vehicle_id)` in a DDL
	// literal is detected.
	upper := w7WriteSource(t, map[string]string{"ddl.go": "package p\n\n" +
		"const tripDDL = `CREATE TABLE trip (\n" +
		"  trip_id TEXT PRIMARY KEY,\n" +
		"  vehicle_id TEXT NOT NULL REFERENCES vehicle(vehicle_id)\n" +
		") STRICT`\n"})
	if hits := w7ForeignKeyHits(t, upper); len(hits) != 1 {
		t.Fatalf("W7-6(a) CONTROL FAILED (i): a planted upper-case REFERENCES clause produced %v, want exactly one "+
			"hit ((m7-6))", hits)
	}

	// (ii) the LOWER-CASE spelling is ALSO detected — the arm a
	// case-sensitive match misses, and `references vehicle(vehicle_id)` is
	// legal SQL.
	lower := w7WriteSource(t, map[string]string{"ddl.go": "package p\n\n" +
		"const tripDDL = `create table trip (vehicle_id text references vehicle(vehicle_id))`\n"})
	if hits := w7ForeignKeyHits(t, lower); len(hits) != 1 {
		t.Fatalf("W7-6(a) CONTROL FAILED (ii): a planted LOWER-CASE references clause produced %v, want exactly "+
			"one hit — a case-sensitive match ships this green", hits)
	}

	// (ii-b) WHITESPACE TOLERANCE: `FOREIGN\nKEY (` split across a line is
	// legal SQL and ships green under a single-space match.
	split := w7WriteSource(t, map[string]string{"ddl.go": "package p\n\n" +
		"const tripDDL = `CREATE TABLE trip (\n  vehicle_id TEXT,\n  FOREIGN\n  KEY (vehicle_id) REFERENCES vehicle(vehicle_id)\n)`\n"})
	if hits := w7ForeignKeyHits(t, split); len(hits) == 0 {
		t.Fatalf("W7-6(a) CONTROL FAILED (ii-b): a FOREIGN KEY split across a line was not detected — " +
			"the match must be WHITESPACE-TOLERANT")
	}

	// (iii) THE NEGATIVE ARM, with a production decoy: comment prose that
	// says "references" and a PRAGMA that says "foreign_keys" must NOT be
	// flagged.
	decoys := w7WriteSource(t, map[string]string{"prose.go": "package p\n\n" +
		"// TripID references the trip whose deadline this is.\n" +
		"// correctionOriginal follows target references to the ORIGINAL first (TP 4.7).\n" +
		"// explicit references bind unconditionally (an explicit arrival wins).\n" +
		"const dsn = `file:x?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)`\n"})
	if hits := w7ForeignKeyHits(t, decoys); len(hits) != 0 {
		t.Fatalf("W7-6(a) CONTROL FAILED (iii, NEGATIVE ARM): comment prose and a `foreign_keys(1)` PRAGMA were "+
			"flagged as constructs: %v. A scan that flags the correct build is a scan a builder weakens until it "+
			"is green, and each exclusion narrows the subject.", hits)
	}

	// --- THE ASSERTION ---------------------------------------------------
	roots := w7ProductionRoots(t)
	if hits := w7ForeignKeyHits(t, roots...); len(hits) > 0 {
		t.Errorf("W7-6(a) (m7-6): foreign-key construct(s) in production source:\n  %s\n"+
			"analytics-v1.md:932-933: \"There are no foreign keys anywhere in this schema today and none is added.\" "+
			"W7-6(b) is the behavioural half — the fold NEVER refuses a trip whose `vehicle_id` it does not hold.",
			strings.Join(hits, "\n  "))
	}

	// --- THE NEGATIVE ARM'S FLOOR, over the REAL corpus -------------------
	//
	// ⚠ The arm protects the decoys only if the corpus actually contains
	// some. Deriving the count rather than pinning it is deliberate: the
	// lock pins EIGHT and the tree measures NINE plus an unnamed PRAGMA, and
	// a pinned figure inside the remedy for short lists is the defect class
	// arriving inside its own fix.
	decoyCount := w7ForeignKeyProseOccurrences(t, roots...)
	if decoyCount == 0 {
		t.Errorf("W7-6(a) NEGATIVE ARM IS PROTECTING NOTHING: the production corpus contains zero comment-prose "+
			"or PRAGMA occurrences of `references`/`foreign key`, so the arm cannot fail and is not a control. "+
			"[MEASURED 2026-09-03 the corpus held %d.] This is ⟦LOCK — FIL-F7⟧'s finding, restated as a check.",
			decoyCount)
	}
}

var (
	// w7FKConstraint matches a FOREIGN KEY table constraint, case-insensitively
	// and whitespace-tolerantly. ⚠ It requires the `(` so that
	// `_pragma=foreign_keys(1)` — a connection PRAGMA, plural, underscored —
	// is NOT matched.
	w7FKConstraint = regexp.MustCompile(`(?is)\bforeign\s+key\s*\(`)
	// w7FKReferences matches a REFERENCES column constraint: the keyword, an
	// identifier, then `(`. Comment prose puts ordinary words after
	// "references", never `ident(`, which is what makes the negative arm
	// real rather than a filter that excludes comments by construction.
	w7FKReferences = regexp.MustCompile(`(?is)\breferences\s+[a-z_][a-z0-9_]*\s*\(`)
	// w7FKProse is the DECOY detector — the loose spelling the criterion's
	// negative arm protects. It exists only to give the floor something to
	// count and is never used as a violation detector.
	w7FKProse = regexp.MustCompile(`(?i)\breferences\b|\bforeign[_ ]keys?\b`)
)

// w7ForeignKeyHits is THE JUDGEMENT FUNCTION for W7-6(a) — the real pass and
// every control drive it, so a control red proves the same walk + filter the
// clean pass runs.
//
// ⚠ THE CORPUS IS RAW FILE TEXT, COMMENTS INCLUDED, ON PURPOSE. Filtering to
// string literals would exclude the comment-prose decoys BY CONSTRUCTION and
// the negative arm would pass vacuously — which is exactly the shape
// ⟦LOCK — FIL-F7⟧ struck. The discrimination is carried by the PATTERN
// (a construct is `references ident(`), not by the corpus.
func w7ForeignKeyHits(t *testing.T, roots ...string) []string {
	t.Helper()
	var hits []string
	for _, path := range w7ProductionGoFiles(t, roots...) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("W7-6(a): this file was NOT scanned, so this scan's coverage is smaller than its stated "+
				"scope: %s: %v", path, err)
		}
		for _, m := range w7FKConstraint.FindAllIndex(src, -1) {
			hits = append(hits, w7Located(path, src, m[0])+" FOREIGN KEY")
		}
		for _, m := range w7FKReferences.FindAllIndex(src, -1) {
			hits = append(hits, w7Located(path, src, m[0])+" REFERENCES")
		}
	}
	sort.Strings(hits)
	return hits
}

// w7ForeignKeyProseOccurrences counts LOOSE occurrences that are NOT
// constructs — the decoys the negative arm protects. Derived, never pinned.
func w7ForeignKeyProseOccurrences(t *testing.T, roots ...string) int {
	t.Helper()
	n := 0
	for _, path := range w7ProductionGoFiles(t, roots...) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		loose := len(w7FKProse.FindAllIndex(src, -1))
		strict := len(w7FKConstraint.FindAllIndex(src, -1)) + len(w7FKReferences.FindAllIndex(src, -1))
		if loose > strict {
			n += loose - strict
		}
	}
	return n
}

func w7Located(path string, src []byte, off int) string {
	return path + ":" + itoa(1+strings.Count(string(src[:off]), "\n"))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// =========================================================================
// W7-X1 — every schema change lives inside a CREATE-containing literal
// =========================================================================
//
// ⟦NO PRE-CHUNK RED — zero constructs, §2.4 row 6⟧ [MEASURED 2026-09-03 over
// this exact corpus] the production tree holds ONE hit of the five arms and
// it is a COMMENT (`internal/domain/refdata.go:307`) — zero constructs. So
// the evidence is the FIVE PLANTED SHAPES and never the clean pass.
//
// ⚠ W7's exposure is LARGER than W6's: W7 adds two `CREATE TABLE` literals,
// adds a column to `trip`, adds a column to `trip_arrival`, AND RELAXES A NOT
// NULL. SQLite has no `ALTER TABLE … ALTER COLUMN`, so a relaxation attempted
// in place is a four-statement table REBUILD of which three statements are
// not `CREATE` at all — and W1's guard 3a admits a literal only if it
// contains `CREATE `, so the rebuild is structurally invisible to it.
//
// ⚠ AND THE ONE REBUILD PATH THAT USES NONE OF THE FOUR TOKENS
// (⟦LOCK — FIL-F8⟧): `PRAGMA writable_schema=ON; UPDATE sqlite_master SET
// sql=…` targets EXACTLY the column W7 relaxes and carries no `ALTER TABLE`,
// no `ADD COLUMN`, no `DROP TABLE` and no `RENAME TO`. Both are arms.
//
// ⚠ THE ALTER TOKEN IS BARE `ALTER TABLE`, not `ALTER TABLE … ADD COLUMN`:
// the rebuild's own `RENAME TO` statement is an `ALTER TABLE` carrying no
// `ADD COLUMN`.
//
// ⚠ This does NOT discharge `H-B` (SEAM-W7-7). `OWED-CENSUS.md:58` names
// W6–W9 as VICTIMS of H-B, not owners; `W7-X1` removes W7's EXPOSURE and
// leaves the obligation homeless, visibly.
func TestW7SchemaChangesLiveInCreateLiterals(t *testing.T) {
	// --- CONTROL FIRST: five arms, each applied ALONE ---------------------
	for _, arm := range []struct {
		name string
		src  string
		want int
	}{
		{"ALTER TABLE ... ADD COLUMN", "package p\n\nconst mig = `ALTER TABLE trip ADD COLUMN vehicle_id TEXT`\n", 2},
		{"DROP TABLE / RENAME TO rebuild", "package p\n\nconst mig = `CREATE TABLE trip_new (trip_id TEXT); " +
			"INSERT INTO trip_new SELECT trip_id FROM trip; DROP TABLE trip; ALTER TABLE trip_new RENAME TO trip`\n", 3},
		{"PRAGMA writable_schema / sqlite_master", "package p\n\nconst mig = `PRAGMA writable_schema=ON`\n" +
			"const upd = `UPDATE sqlite_master SET sql = 'CREATE TABLE trip (vehicle_entered TEXT)'`\n", 2},
	} {
		dir := w7WriteSource(t, map[string]string{"mig.go": arm.src})
		hits := w7DDLTokenHits(t, dir)
		if len(hits) < 1 {
			t.Fatalf("W7-X1 CONTROL FAILED (%s): the token walk detected NOTHING in a planted %s — (m7-X1)",
				arm.name, arm.name)
		}
	}

	// (iv) a DDL string built by `+` CONCATENATION — the arm no token match
	// can see, because the tokens are split across operands.
	concat := w7WriteSource(t, map[string]string{"concat.go": "package p\n\n" +
		"const cols = `trip_id TEXT`\n" +
		"var mig = `CREATE TABLE trip (` + cols + `)`\n"})
	if n := w7ConcatenatedDDL(t, concat); n != 1 {
		t.Fatalf("W7-X1 CONTROL FAILED (iv): the concatenation detector reported %d on a planted `+`-built DDL "+
			"string, want 1 — (m7-X1) arm 4", n)
	}

	// (v) THE NEGATIVE ARM (W5d-G-2 item 3): a plain `+`-concatenated SELECT
	// must NOT be flagged. Without it the detector flags every SQL builder in
	// the tree and a builder narrows it until it is green.
	sel := w7WriteSource(t, map[string]string{"sel.go": "package p\n\n" +
		"const tbl = `trip`\n" +
		"var q = `SELECT trip_id FROM ` + tbl + ` WHERE origin_site = ?`\n"})
	if n := w7ConcatenatedDDL(t, sel); n != 0 {
		t.Fatalf("W7-X1 CONTROL FAILED (v, NEGATIVE ARM): a `+`-concatenated SELECT was flagged %d time(s) as DDL", n)
	}

	// ⚠ AT LEAST ONE POSITIVE PLANT MUST LAND OUTSIDE THE PACKAGE W7 EDITS
	// MOST — otherwise the controls prove only that the walk reaches
	// `internal/store`, which is the narrow-filter class again. W7 edits
	// `internal/store` AND `internal/domain`, so the plant goes in a THIRD
	// package: `cmd/`.
	cmdPlant := w7WriteSource(t, map[string]string{
		filepath.Join("cmd", "somecmd", "main.go"): "package main\n\nconst mig = `ALTER TABLE vehicle ADD COLUMN retired INTEGER`\n",
	})
	if hits := w7DDLTokenHits(t, cmdPlant); len(hits) == 0 {
		t.Fatalf("W7-X1 CONTROL FAILED: a plant under `cmd/` was not reached — the walk must be RECURSIVE over " +
			"both roots, or the controls prove only that it reaches the package W7 edits most")
	}

	// --- THE ASSERTION ---------------------------------------------------
	roots := w7ProductionRoots(t)
	if hits := w7DDLTokenHits(t, roots...); len(hits) > 0 {
		t.Errorf("W7-X1 (m7-X1): DDL outside a `CREATE`-containing literal:\n  %s",
			strings.Join(hits, "\n  "))
	}
	if n := w7ConcatenatedDDL(t, roots...); n > 0 {
		t.Errorf("W7-X1 (m7-X1) arm 4: %d DDL string(s) assembled by `+` concatenation. W1's guard 3a admits a "+
			"literal only if it contains `CREATE `, so a concatenated one is invisible to it.", n)
	}
}

// w7DDLTokens are W7-X1's FIVE ARMS as literals. ⚠ Stated as a set here and
// iterated, so a red names WHICH arm fired.
var w7DDLTokens = []*regexp.Regexp{
	regexp.MustCompile(`(?is)\balter\s+table\b`),
	regexp.MustCompile(`(?is)\badd\s+column\b`),
	regexp.MustCompile(`(?is)\bdrop\s+table\b`),
	regexp.MustCompile(`(?is)\brename\s+to\b`),
	regexp.MustCompile(`(?is)\bpragma\s+writable_schema\b|\bsqlite_master\b`),
}

// w7DDLTokenHits is W7-X1's judgement function. Its corpus is STRING
// LITERALS ONLY (comments excluded), because §2.4 row 6's baseline is "one
// hit, and it is a COMMENT — zero CONSTRUCTS": the criterion counts
// constructs. ⚠ That is the OPPOSITE polarity from W7-6(a)'s corpus, and the
// difference is deliberate — W7-6(a)'s negative arm is explicitly about
// comment prose not being flagged, W7-X1's baseline explicitly discounts a
// comment.
//
// ⚠ AN UNPARSEABLE FILE IS EMITTED AS A NAMED FAULT, never silently skipped
// (W7-X6's unparseable-file fault arm, W6's post-lock G-6). The in-tree
// precedent at `w5d_source_scans_test.go:99–101` does `continue // another
// agent's in-flight work`, which is the SILENT SKIP that arm forbids; a clone
// copying the idiom without this sentence copies the silent skip with it.
func w7DDLTokenHits(t *testing.T, roots ...string) []string {
	t.Helper()
	var hits []string
	fset := token.NewFileSet()
	for _, path := range w7ProductionGoFiles(t, roots...) {
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			hits = append(hits, path+": FAULT — this file was NOT scanned, so this scan's coverage is smaller "+
				"than its stated scope: "+err.Error())
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			for i, re := range w7DDLTokens {
				if re.MatchString(lit.Value) {
					hits = append(hits, fset.Position(lit.Pos()).String()+" arm "+itoa(i+1))
				}
			}
			return true
		})
	}
	sort.Strings(hits)
	return hits
}

// w7ConcatenatedDDL counts binary `+` expressions in which any operand is a
// string literal containing a DDL verb — the arm a token match cannot see.
// ⚠ The NEGATIVE arm depends on the verb set being DDL and not SQL: a
// `SELECT` built by `+` is ordinary and must not be flagged.
var w7DDLVerb = regexp.MustCompile(`(?is)\b(create\s+table|create\s+index|create\s+trigger|alter\s+table|drop\s+table)\b`)

func w7ConcatenatedDDL(t *testing.T, roots ...string) int {
	t.Helper()
	n := 0
	fset := token.NewFileSet()
	for _, path := range w7ProductionGoFiles(t, roots...) {
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Errorf("W7-X1 FAULT — this file was NOT scanned, so this scan's coverage is smaller than its "+
				"stated scope: %s: %v", path, err)
			continue
		}
		ast.Inspect(f, func(node ast.Node) bool {
			be, ok := node.(*ast.BinaryExpr)
			if !ok || be.Op != token.ADD {
				return true
			}
			for _, operand := range []ast.Expr{be.X, be.Y} {
				lit, ok := operand.(*ast.BasicLit)
				if ok && lit.Kind == token.STRING && w7DDLVerb.MatchString(lit.Value) {
					n++
					return true
				}
			}
			return true
		})
	}
	return n
}

// =========================================================================
// W7-9(c) — the ORDER-BY spelling, scoped by RESOLVED CONSTANT VALUE
// =========================================================================
//
// ⚠⚠ THIS LIMB IS RE-POINTED AT A DIFFERENT INSTRUMENT FROM THE ONE REVISION
// 1 PRESCRIBED, BECAUSE THAT ONE IS A MEASURED NO-OP. `w3_source_scans_test.
// go`'s own comment records it: the W3 ORDERING scan's AST filter admits only
// `*ast.BasicLit`s CONTAINING the table identifier, and the house's M-1
// constant split leaves the ORDER BY in a literal that carries no table name
// — "so it examines no ordering literal at all. Widening a filter that
// selects nothing selects nothing for company too." That diagnosis is carried
// here verbatim so the NEXT clone does not re-make the mistake.
//
// The respecified instrument scopes by RESOLVED CONSTANT VALUE through
// `go/types` — the JOINED SQL a reader executes, not the fragment as written.
//
// ⚠⚠ OWNERSHIP, STATED AND NOT EXERCISED. The criterion grants W7's
// TEST-WRITER the edit adding `"vehicle_amendment"` as a third member of
// `w5dScopedSideTables` (`internal/store/w5d_source_scans_test.go:59`). **That
// edit is NOT made here** — this pass's own bounds forbid modifying an
// existing test file, and the two instructions conflict. The conflict is
// REPORTED at reports/window-build/W7-tests.md §5, not resolved. W7's own
// instance below is a SEPARATE instrument in a W7-owned file; it does not
// discharge the `w5dScopedSideTables` edit, and CONTROLS 2, 3 and 3b of that
// file remain hardcoded on the COMPANY spelling exactly as ⟦LOCK — F9⟧
// measured.
//
// EXPECTED RED at e9cc5f5 (runtime): the LIVENESS FLOOR —
// `Liveness["vehicle_amendment"] == 0`.
func TestW7VehicleAmendmentOrderBySpellingByResolvedValue(t *testing.T) {
	// --- FOUR CONTROLS, ALL RE-PRODUCED ON THE VEHICLE SPELLING, each alone.
	//
	// ⚠ "A widened scan whose controls were only ever produced on the person
	// or company side is the same self-confirming shape one level down",
	// which is why all four are re-produced rather than inherited.

	// (i) the REORDERED spelling, detected THROUGH THE WALK.
	reordered := w7WriteSource(t, map[string]string{"compose.go": "package p\n\n" +
		"const vehicleAmendmentCompositionSQL = `SELECT number, role, same_as, cleared FROM vehicle_amendment " +
		"WHERE vehicle_id = ? ORDER BY node_epoch, site_id, seq`\n"})
	got := w7ScanVehicleOrderBy(t, reordered)
	// ⟦LOCK — §56(1)⟧ ASKED BEFORE THE DEVIANTS ARE READ.
	if !s56PlantTypeChecked(t, "W7-9(c) control (i): the REORDERED spelling", got.Diags) {
		return
	}
	if len(got.Deviants) != 1 {
		t.Fatalf("W7-9(c) CONTROL FAILED (i): a REORDERED ORDER BY on the vehicle spelling produced deviants %v, "+
			"want exactly one — (m7-9b)", got.Deviants)
	}

	// (ii) the same detected THROUGH THE FILTER — i.e. the finding is
	// attributed to `vehicle_amendment` and not merely found somewhere.
	if !strings.Contains(got.Deviants[0], w7AmendmentTable) {
		t.Fatalf("W7-9(c) CONTROL FAILED (ii): the deviant %q is not attributed to `%s` — the filter is what makes "+
			"this a vehicle finding rather than a tree-wide one", got.Deviants[0], w7AmendmentTable)
	}

	// (iii) the EPOCH-BLIND `(site_id, seq)` spelling — `W3-m5`'s — detected.
	// ⚠ This is the arm that matters most: an epoch-blind comparator can be
	// perfectly permutation-invariant and still wrong.
	epochBlind := w7WriteSource(t, map[string]string{"compose.go": "package p\n\n" +
		"const vehicleAmendmentCompositionSQL = `SELECT number FROM vehicle_amendment WHERE vehicle_id = ? " +
		"ORDER BY site_id, seq`\n"})
	blind := w7ScanVehicleOrderBy(t, epochBlind)
	if !s56PlantTypeChecked(t, "W7-9(c) control (iii): the EPOCH-BLIND spelling", blind.Diags) {
		return
	}
	if d := blind.Deviants; len(d) != 1 {
		t.Fatalf("W7-9(c) CONTROL FAILED (iii): the EPOCH-BLIND (site_id, seq) spelling produced %v, want one "+
			"deviant — `W3-m5`", d)
	}

	// (iv) THE LIVENESS PLANT: a source whose canonical spelling appears ONLY
	// IN A COMMENT counts ZERO toward Liveness["vehicle_amendment"]. This is
	// the CONTROL 3 shape, and it is what stops a comment satisfying the
	// floor.
	commentOnly := w7WriteSource(t, map[string]string{"compose.go": "package p\n\n" +
		"// ORDER BY site_id, node_epoch, seq over vehicle_amendment — described, not executed.\n" +
		"const somethingElse = `SELECT 1`\n"})
	comment := w7ScanVehicleOrderBy(t, commentOnly)
	if !s56PlantTypeChecked(t, "W7-9(c) control (iv): the comment-only liveness plant", comment.Diags) {
		return
	}
	if n := comment.Liveness[w7AmendmentTable]; n != 0 {
		t.Fatalf("W7-9(c) CONTROL FAILED (iv): a canonical ORDER BY appearing ONLY IN A COMMENT counted %d toward "+
			"liveness, want 0 — a comment is not evidence of a scan's scope; read the filter", n)
	}

	// (iv-b) the POSITIVE half of the liveness control: a real canonical
	// constant must COUNT. A liveness counter that is always zero makes the
	// floor below a permanent red for the wrong reason.
	canonical := w7WriteSource(t, map[string]string{"compose.go": "package p\n\n" +
		"const vehicleAmendmentCompositionSQL = `SELECT number, role, same_as, cleared FROM vehicle_amendment " +
		"WHERE vehicle_id = ? ORDER BY site_id, node_epoch, seq`\n"})
	canon := w7ScanVehicleOrderBy(t, canonical)
	if !s56PlantTypeChecked(t, "W7-9(c) control (iv-b): the canonical constant must COUNT", canon.Diags) {
		return
	}
	if canon.Liveness[w7AmendmentTable] != 1 {
		t.Fatalf("W7-9(c) CONTROL FAILED (iv-b): a canonical vehicle composition constant counted %d, want 1",
			canon.Liveness[w7AmendmentTable])
	}
	if len(canon.Deviants) != 0 {
		t.Fatalf("W7-9(c) CONTROL FAILED (negative arm): the CORRECT canonical spelling was reported as a "+
			"deviant: %v", canon.Deviants)
	}

	// --- THE ASSERTION, over the real composition's package ----------------
	root := w7ModuleRoot(t)
	live := w7ScanVehicleOrderBy(t,
		filepath.Join(root, "internal", "domain"),
		filepath.Join(root, "internal", "store"))
	s56LogTree(t, "w7ScanVehicleOrderBy", live.Diags)

	if len(live.Deviants) > 0 {
		t.Errorf("W7-9(c) (m7-9b): deviant or epoch-blind ORDER BY inside a `%s` composition:\n  %s",
			w7AmendmentTable, strings.Join(live.Deviants, "\n  "))
	}
	// THE LIVENESS FLOOR.
	if live.Liveness[w7AmendmentTable] == 0 {
		t.Errorf("W7-9(c) LIVENESS FLOOR: Liveness[%q] == 0 — the vehicle composition is unbuilt or spelled "+
			"differently, so this scan is examining nothing and its clean deviant list is worth nothing. "+
			"⚠ \"Widening a filter that selects nothing selects nothing\" — `w3_source_scans_test.go:51-52`'s "+
			"own diagnosis, carried here so the next clone does not re-make it.\n"+
			"⟦§56⟧ what this scan type-checked, so a ZERO LIVENESS is never confused with a tree that did not "+
			"compile:\n%s", w7AmendmentTable, live.Diags.Report())
	}
}

// w7CanonicalOrderBy is the house's M-1 ordering, in the composition's own
// key order.
var w7CanonicalOrderBy = regexp.MustCompile(
	`(?i)ORDER\s+BY\s+(?:[a-z_]+\.)?site_id\s*,\s*(?:[a-z_]+\.)?node_epoch\s*,\s*(?:[a-z_]+\.)?seq\b`)

// w7AnyOrderBy captures each ORDER BY clause's column tail.
var w7AnyOrderBy = regexp.MustCompile(`(?i)ORDER\s+BY\s+([a-z_.,\s()]+)`)

type w7OrderByFindings struct {
	Deviants []string
	Liveness map[string]int

	// ⟦LOCK — §56(1), 2026-09-03⟧ every go/types diagnostic raised while
	// checking the directories this scan walked. ⚠ IT EXISTS BECAUSE THE
	// CONFIG USED TO SWALLOW THEM: this scan is RESOLVED-VALUE, so a plant
	// that does not type-check yields no *types.Const and the scan returns
	// zero deviants and zero liveness — the same output a blind scan gives.
	Diags s56Set
}

// w7ScanVehicleOrderBy is THE JUDGEMENT FUNCTION — the real pass and all four
// controls drive it, over a DIRECTORY, so a control red proves the same walk,
// the same filter and the same detector.
func w7ScanVehicleOrderBy(t *testing.T, dirs ...string) w7OrderByFindings {
	t.Helper()
	f := w7OrderByFindings{Liveness: map[string]int{w7AmendmentTable: 0}}
	for _, dir := range dirs {
		consts, dg := w7ResolvedStringConsts(t, dir)
		f.Diags = append(f.Diags, dg)
		for _, c := range consts {
			if !strings.Contains(c.Value, w7AmendmentTable) {
				continue
			}
			if w7CanonicalOrderBy.MatchString(c.Value) {
				f.Liveness[w7AmendmentTable]++
				continue
			}
			for _, m := range w7AnyOrderBy.FindAllStringSubmatch(c.Value, -1) {
				// ⚠ The finding carries the TABLE it is attributed to, not
				// only its position: control (ii) asserts the attribution,
				// and a finding that names no table cannot be told from a
				// tree-wide one.
				f.Deviants = append(f.Deviants,
					w7AmendmentTable+" @ "+c.Pos+" ("+c.Name+"): ORDER BY "+strings.TrimSpace(m[1]))
			}
		}
	}
	sort.Strings(f.Deviants)
	return f
}

type w7ConstValue struct {
	Name  string
	Pos   string
	Value string
}

// w7ResolvedStringConsts type-checks the non-test files of ONE DIRECTORY as a
// package and returns every string constant with its RESOLVED value — the
// joined SQL, not the fragment as written. That is what makes this scan
// immune to WHERE a declaration sits, which is the defect a co-location
// filter has and a one-line hoist defeats.
//
// ⚠ `IgnoreFuncBodies: true` is CORRECT HERE and would be wrong for a callee
// resolution: this walk resolves package-level CONSTANTS, and a constant is
// not declared inside a function body. (⟦LOCK — F6a⟧ records that the same
// setting is exactly what makes a CALL resolution impossible, which is why
// W7-30's walk must not copy this configuration.)
//
// ⚠ AN UNPARSEABLE FILE IS A NAMED FAULT, never a silent skip.
func w7ResolvedStringConsts(t *testing.T, dir string) ([]w7ConstValue, s56Diags) {
	t.Helper()
	dg := s56Diags{Where: dir}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Errorf("W7-9(c) FAULT — %s was NOT scanned, so this scan's coverage is smaller than its stated "+
				"scope: %v", path, err)
			continue
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		return nil, dg
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}}
	// ⟦LOCK — §56(1), 2026-09-03⟧ RECORDED, never discarded. ⟦was:
	// "Error: func(error) {},"⟧
	conf := types.Config{
		Error:                    dg.Record,
		Importer:                 nil,
		DisableUnusedImportCheck: true,
		IgnoreFuncBodies:         true,
	}
	_, _ = conf.Check("w7scan", fset, files, info)

	var out []w7ConstValue
	for id, obj := range info.Defs {
		c, ok := obj.(*types.Const)
		if !ok {
			continue
		}
		v := c.Val()
		if v == nil || v.Kind() != constant.String {
			continue
		}
		out = append(out, w7ConstValue{
			Name:  id.Name,
			Pos:   fset.Position(id.Pos()).String(),
			Value: constant.StringVal(v),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pos < out[j].Pos })
	return out, dg
}

// =========================================================================
// W7-11(a) — vehicleFoldKey is DEFINED ONCE and shared
// =========================================================================
//
// [DOC — item 9, "defined once, shared by C's picker and D's duplicates
// list"]. PLANT (a): a SECOND `vehicleFoldKey` declaration in another package
// — the drift the spec's own "two consumers" makes likely, and the reason
// this limb exists.
//
// EXPECTED RED at e9cc5f5 (runtime): ZERO declarations, want exactly one.
func TestW7VehicleFoldKeyIsDeclaredExactlyOnce(t *testing.T) {
	// --- CONTROL FIRST: the census must discriminate 0 / 1 / 2, and ONE
	// PLANT MUST LAND IN A PACKAGE OUTSIDE `internal/domain`+`internal/web`
	// — ⟦LOCK — FIL-F5⟧ names `internal/deadline`, which W7-26 measures as a
	// LIVE CONSUMER of the vehicle value (`scan.go:177`, `:244`).
	none := w7WriteSource(t, map[string]string{"a.go": "package p\n\nfunc other(s string) string { return s }\n"})
	if n := w7DeclarationsOf(t, "vehicleFoldKey", none); n != 0 {
		t.Fatalf("W7-11(a) CONTROL FAILED: the declaration census reported %d on a package with none, want 0", n)
	}
	one := w7WriteSource(t, map[string]string{
		filepath.Join("domain", "vehicle.go"): "package domain\n\nfunc vehicleFoldKey(s string) string { return s }\n",
	})
	if n := w7DeclarationsOf(t, "vehicleFoldKey", one); n != 1 {
		t.Fatalf("W7-11(a) CONTROL FAILED: the census reported %d on a single declaration, want 1", n)
	}
	two := w7WriteSource(t, map[string]string{
		filepath.Join("domain", "vehicle.go"): "package domain\n\nfunc vehicleFoldKey(s string) string { return s }\n",
		filepath.Join("deadline", "scan.go"):  "package deadline\n\nfunc vehicleFoldKey(s string) string { return s }\n",
	})
	if n := w7DeclarationsOf(t, "vehicleFoldKey", two); n != 2 {
		t.Fatalf("W7-11(a) CONTROL FAILED (PLANT (a)): a SECOND declaration in another package "+
			"(`internal/deadline`, ⟦LOCK — FIL-F5⟧'s named site) was counted %d, want 2 — this is the drift the "+
			"spec's \"two consumers\" makes likely and the reason this limb exists", n)
	}

	// --- THE ASSERTION ---------------------------------------------------
	roots := w7ProductionRoots(t)
	if n := w7DeclarationsOf(t, "vehicleFoldKey", roots...); n != 1 {
		t.Errorf("W7-11(a): `vehicleFoldKey` has %d production declaration(s) over `internal/`+`cmd/`, want "+
			"EXACTLY ONE. It is \"defined once, shared by C's picker and D's duplicates list\" — and it is a "+
			"DIFFERENT function from `NormalizeVehicleID`, which W7-11 asserts neither becomes.", n)
	}
}

// w7DeclarationsOf counts top-level func declarations of `name` over the
// production corpus. Shared by the control and the assertion.
func w7DeclarationsOf(t *testing.T, name string, roots ...string) int {
	t.Helper()
	n := 0
	fset := token.NewFileSet()
	for _, path := range w7ProductionGoFiles(t, roots...) {
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Errorf("W7-11(a) FAULT — %s was NOT scanned, so this scan's coverage is smaller than its stated "+
				"scope: %v", path, err)
			continue
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Name != nil && fd.Name.Name == name {
				n++
			}
		}
	}
	return n
}

// =========================================================================
// W7-25(e) — the relocated guard fires ONCE, at creation
// =========================================================================
//
// ⚠⚠ RESTATED AS A ONE-CALL-SITE CENSUS (⟦LOCK — FAL-F16⟧), because "no
// refusal path is consulted" IS NOT OBSERVABLE and killed nothing: a dispatch
// that is not refused looks identical whether the predicate was consulted and
// passed or never consulted at all — so under DR-W7-4, where the number is
// not typed at dispatch anyway, the superseded clause was VACUOUS.
//
// ⭐ THIS IS A PRODUCED PRE-CHUNK RED, naming no unbuilt symbol: the pre-chunk
// tree calls `IsPlaceholderVehicleID` at TWO sites and neither is a creation
// path.
//
// ⚠ The second limb — that the one surviving site is NOT at an authoring door
// — is what separates RELOCATION from DUPLICATION, which is (m7-25d) exactly.
func TestW7PlaceholderGuardHasOneCallSiteAndItIsNotAnAuthoringDoor(t *testing.T) {
	// --- CONTROL FIRST: the census must see a call site, must attribute it
	// to its ENCLOSING FUNCTION, and must not count a mere mention.
	plant := w7WriteSource(t, map[string]string{"author.go": "package p\n\n" +
		"func dispatchTrip() error {\n\tif IsPlaceholderVehicleID(\"x\") {\n\t\treturn nil\n\t}\n\treturn nil\n}\n" +
		"func createVehicle() error {\n\tif IsPlaceholderVehicleID(\"y\") {\n\t\treturn nil\n\t}\n\treturn nil\n}\n"})
	sites := w7CallSitesOf(t, "IsPlaceholderVehicleID", plant)
	if len(sites) != 2 {
		t.Fatalf("W7-25(e) CONTROL FAILED: the call-site census found %d sites in a file with two, got %v",
			len(sites), sites)
	}
	var enclosing []string
	for _, s := range sites {
		enclosing = append(enclosing, s.Func)
	}
	sort.Strings(enclosing)
	if strings.Join(enclosing, ",") != "createVehicle,dispatchTrip" {
		t.Fatalf("W7-25(e) CONTROL FAILED: the census did not attribute call sites to their ENCLOSING FUNCTION "+
			"(got %v) — without that attribution it cannot tell RELOCATION from DUPLICATION, which is (m7-25d)",
			enclosing)
	}
	mention := w7WriteSource(t, map[string]string{"prose.go": "package p\n\n" +
		"// IsPlaceholderVehicleID is relocated to vehicle creation by DR-W7-5.\n" +
		"const s = \"IsPlaceholderVehicleID\"\n"})
	if got := w7CallSitesOf(t, "IsPlaceholderVehicleID", mention); len(got) != 0 {
		t.Fatalf("W7-25(e) CONTROL FAILED (negative arm): a comment and a string literal were counted as CALL "+
			"SITES: %v — a spelling match is defeated by prose and cannot tell a call from a mention", got)
	}

	// --- THE ASSERTION ---------------------------------------------------
	roots := w7ProductionRoots(t)
	live := w7CallSitesOf(t, "IsPlaceholderVehicleID", roots...)

	if len(live) != 1 {
		var desc []string
		for _, s := range live {
			desc = append(desc, s.Pos+" (in "+s.Func+")")
		}
		t.Errorf("W7-25(e): `IsPlaceholderVehicleID` has %d production call site(s), want EXACTLY ONE, on the "+
			"vehicle-CREATION path:\n  %s\n[DOC] \"strictly better than today: the refusal fires once, when the "+
			"record is created, instead of on every trip forever.\" The design sentence is now a check.",
			len(live), strings.Join(desc, "\n  "))
	}
	// ⚠ (m7-25d): the relocated guard ALSO left at a dispatch or arrival site
	// — the relocation becoming a duplication. Asserted over the ENCLOSING
	// FUNCTION, because a count of one is satisfied by one call left at the
	// wrong door.
	for _, s := range live {
		if w7AuthoringDoors[s.Func] {
			t.Errorf("W7-25(e) (m7-25d): `IsPlaceholderVehicleID` is called from `%s` at %s — an AUTHORING DOOR. "+
				"DR-W7-5 relocates the guard to vehicle creation; [[D-11]] says \"the placeholder refusal stays\", "+
				"so it is RELOCATED, never removed and never duplicated.", s.Func, s.Pos)
		}
	}
}

// w7AuthoringDoors are the four doors W7-4(b) names — `dispatchTrip` is the
// shared core behind BOTH `Dispatch` and `DispatchLocalCheckIn`, and
// `GateCheckIn` is the fourth door ⟦LOCK — F6b⟧ found and revision 1 never
// mentioned.
var w7AuthoringDoors = map[string]bool{
	"dispatchTrip":             true,
	"Dispatch":                 true,
	"DispatchLocalCheckIn":     true,
	"RecordUnsolicitedArrival": true,
	"GateCheckIn":              true,
}

type w7CallSite struct {
	Pos  string
	Func string // the enclosing function's name
}

// w7CallSitesOf finds CALL EXPRESSIONS of `name` (bare or as a selector's
// field) and attributes each to its enclosing function declaration. It counts
// calls, never mentions — a comment or a string literal naming the function
// is not a call site.
func w7CallSitesOf(t *testing.T, name string, roots ...string) []w7CallSite {
	t.Helper()
	var out []w7CallSite
	fset := token.NewFileSet()
	for _, path := range w7ProductionGoFiles(t, roots...) {
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Errorf("W7-25(e) FAULT — %s was NOT scanned, so this scan's coverage is smaller than its stated "+
				"scope: %v", path, err)
			continue
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				var called string
				switch fn := call.Fun.(type) {
				case *ast.Ident:
					called = fn.Name
				case *ast.SelectorExpr:
					called = fn.Sel.Name
				}
				if called == name {
					out = append(out, w7CallSite{Pos: fset.Position(call.Pos()).String(), Func: fd.Name.Name})
				}
				return true
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pos < out[j].Pos })
	return out
}
