// w5b_source_scans_test.go — W5-X1 for W5b: every schema change this
// sub-chunk makes is declared inside a single `CREATE `-containing Go string
// literal. No `ALTER TABLE`, no `ADD COLUMN`, no DDL assembled by
// concatenating a column list onto a prefix.
//
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, criteria LOCKED 2026-08-31.
//
// ⟦NO PRE-CHUNK RED⟧ — `ALTER TABLE`/`ADD COLUMN` are ZERO non-test hits at
// 88dd6a6 [MEASURED, re-run here], so this criterion PASSES ON ARRIVAL and
// its clean pass is not evidence (H3). Its ledger evidence is the two PLANTED
// violations below — W5-X1's own words, "a planted `ALTER TABLE … ADD
// COLUMN`, and separately a DDL string built by `+` concatenation, each
// detected before the clean pass counts — ONE CONTROL PER SUB-CHUNK that
// touches schema" (W5-X6). W5b touches schema; this file is W5b's control.
//
// WHY THIS EXISTS: W4 narrowed W1's guard 3a to scan only string literals
// containing `"CREATE "` (W4-falsification.md:672–684), leaving DDL OUTSIDE
// such a literal invisible where the whole-file scan saw it. Every W5
// sub-chunk adds a nullable projection column, which is exactly the moment
// someone reaches for `ALTER TABLE`.
//
// THE DETECTOR READS THE AST, NOT THE TEXT, and that is deliberate: it
// inspects STRING LITERAL VALUES only, so a comment that documents the
// prohibition — and `refdata.go` is full of comments about schema shape — can
// never be reported as a violation. The fourth control below proves that
// discrimination rather than asserting it.
package store_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// w5bTouchedFiles are W5b's touched non-test files — the scan's scope, per
// W5-X1's "asserted by scan over EACH SUB-CHUNK's touched files".
//
// Scoped rather than whole-tree deliberately: a whole-tree scan would red
// W5b's ledger for a violation another sub-chunk's builder landed, and W5-X3
// says no sub-chunk's result counts for another's — in either direction.
var w5bTouchedFiles = []string{
	"internal/domain/refdata.go", // PersonCreated, the `person` CREATE literal, applyPersonCreated, createPerson
	"internal/web/intake.go",     // the intake edge, the refusal, the form
	"internal/store/store.go",    // const SchemaVersion
	"internal/domain/checkin.go", // D-52's second door — it must NOT gain a schema edit either
}

// w5bRepoRoot resolves the app module root from this package's directory.
func w5bRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd() // …/app/internal/store
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("fixture defect: no go.mod at %s — the scan cannot locate the files it is scoped to: %v", root, err)
	}
	return root
}

// w5bDDLFault is one detected violation.
type w5bDDLFault struct {
	Shape string // "ALTER TABLE", "ADD COLUMN", "concatenated DDL"
	Where string
	Text  string
}

// w5bScanDDL parses one Go source and reports every W5-X1 violation in it.
//
// Three shapes, and only string LITERAL VALUES are ever examined:
//
//	(a) a literal containing "ALTER TABLE"
//	(b) a literal containing "ADD COLUMN"
//	(c) a literal containing DDL that takes part in a `+` concatenation or a
//	    `+=` — "DDL assembled by concatenating a column list onto a prefix"
func w5bScanDDL(t *testing.T, filename, src string) []w5bDDLFault {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", filename, err)
	}
	var out []w5bDDLFault
	lit := func(n ast.Node) (string, bool) {
		bl, ok := n.(*ast.BasicLit)
		if !ok || bl.Kind != token.STRING {
			return "", false
		}
		v, err := strconv.Unquote(bl.Value)
		if err != nil {
			return bl.Value, true // raw backtick strings unquote fine; be permissive
		}
		return v, true
	}
	// Any string literal in the tree that carries DDL.
	isDDL := func(s string) bool {
		u := strings.ToUpper(s)
		return strings.Contains(u, "CREATE TABLE") || strings.Contains(u, "CREATE ") ||
			strings.Contains(u, "ALTER TABLE") || strings.Contains(u, "ADD COLUMN")
	}
	// Shapes (a) and (b): scan every literal.
	ast.Inspect(f, func(n ast.Node) bool {
		v, ok := lit(n)
		if !ok {
			return true
		}
		u := strings.ToUpper(v)
		if strings.Contains(u, "ALTER TABLE") {
			out = append(out, w5bDDLFault{"ALTER TABLE", fset.Position(n.Pos()).String(), w5bFirstLine(v)})
		}
		if strings.Contains(u, "ADD COLUMN") {
			out = append(out, w5bDDLFault{"ADD COLUMN", fset.Position(n.Pos()).String(), w5bFirstLine(v)})
		}
		return true
	})
	// --- the alias table, and it is why this detector is not a one-hop
	// scanner. The commonest way to assemble DDL is
	//
	//	const base = "CREATE TABLE IF NOT EXISTS person ("
	//	return base + cols + ") STRICT;"
	//
	// where NO literal inside the concatenation carries the word CREATE.
	// W5a's writer found the same hole in its own structural scan and fixed
	// it the same way — identifiers are resolved to the literals assigned to
	// them, iterated to a BOUNDED FIXED POINT so a two-hop launder
	// (a := lit; b := a; b + …) is still seen. Five rounds is the bound; a
	// deeper launder escapes, and that limit is reported rather than hidden
	// (see §8 of reports/window-build/W5b-tests.md).
	alias := map[string]string{}
	for round := 0; round < 5; round++ {
		grew := false
		record := func(name string, rhs ast.Expr) {
			if name == "" || name == "_" {
				return
			}
			if v, ok := w5bResolve(rhs, lit, alias); ok {
				if prev, seen := alias[name]; !seen || len(v) > len(prev) {
					alias[name] = v
					grew = true
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.ValueSpec:
				for i, nm := range x.Names {
					if i < len(x.Values) {
						record(nm.Name, x.Values[i])
					}
				}
			case *ast.AssignStmt:
				if x.Tok != token.ASSIGN && x.Tok != token.DEFINE {
					return true
				}
				for i, lhs := range x.Lhs {
					id, ok := lhs.(*ast.Ident)
					if !ok || i >= len(x.Rhs) {
						continue
					}
					record(id.Name, x.Rhs[i])
				}
			}
			return true
		})
		if !grew {
			break
		}
	}

	// Shape (c): DDL taking part in a concatenation, direct or through an
	// alias.
	report := func(n ast.Node, expr ast.Node) {
		found := false
		var text string
		ast.Inspect(expr, func(m ast.Node) bool {
			if v, ok := w5bResolve(m, lit, alias); ok && isDDL(v) {
				found, text = true, w5bFirstLine(v)
				return false
			}
			return true
		})
		if found {
			out = append(out, w5bDDLFault{"concatenated DDL", fset.Position(n.Pos()).String(), text})
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BinaryExpr:
			if x.Op == token.ADD {
				report(n, x)
			}
		case *ast.AssignStmt:
			if x.Tok == token.ADD_ASSIGN {
				// The DDL may sit on either side: `s += ", phone TEXT)"`
				// carries it on the LHS alias, `s += createFragment` on the
				// RHS. Both are reported.
				for _, e := range append(append([]ast.Expr{}, x.Lhs...), x.Rhs...) {
					report(n, e)
				}
			}
		}
		return true
	})
	return out
}

// w5bResolve returns the string value of a node that is either a string
// literal or an identifier bound to one.
func w5bResolve(n ast.Node, lit func(ast.Node) (string, bool), alias map[string]string) (string, bool) {
	if v, ok := lit(n); ok {
		return v, true
	}
	if id, ok := n.(*ast.Ident); ok {
		if v, seen := alias[id.Name]; seen {
			return v, true
		}
	}
	return "", false
}

func w5bFirstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:120] + " …"
	}
	return s
}

// TestW5bSchemaChangesStayInsideOneCreateLiteral — W5-X1 at W5b.
//
// EXPECTED RED: NOT the clean pass. The evidence is the four controls below,
// each a real planted violation detected before the real files are read.
// After the build, (m5-X1) is producible by adding W5b's column with
// `ALTER TABLE person ADD COLUMN phone TEXT` — which passes every behavioural
// test in this sub-chunk, because the column really is there.
//
// Kills (m5-X1).
func TestW5bSchemaChangesStayInsideOneCreateLiteral(t *testing.T) {
	// --- CONTROL 1: a planted ALTER TABLE … ADD COLUMN.
	const plantAlter = `package p
func widen() string {
	return "ALTER TABLE person ADD COLUMN phone TEXT"
}`
	faults := w5bScanDDL(t, "plant_alter.go", plantAlter)
	if !w5bHasShape(faults, "ALTER TABLE") || !w5bHasShape(faults, "ADD COLUMN") {
		t.Fatalf("CONTROL FAILED: the planted `ALTER TABLE … ADD COLUMN` was reported as %v — the detector has no tooth for it, so the clean pass below would prove nothing (m5-X1)", faults)
	}

	// --- CONTROL 2: a DDL string built by `+` concatenation. This is the
	// shape W4's narrowing made invisible: the literal that reaches the
	// database contains "CREATE ", but no single literal in the source does
	// the whole job.
	const plantConcat = `package p
const base = "CREATE TABLE IF NOT EXISTS person ("
func schema(extra string) string {
	return base + "person_id TEXT PRIMARY KEY, " + extra + ") STRICT;"
}`
	faults = w5bScanDDL(t, "plant_concat.go", plantConcat)
	if !w5bHasShape(faults, "concatenated DDL") {
		t.Fatalf("CONTROL FAILED: the planted `+`-concatenated DDL was reported as %v — W4 narrowed W1's guard to CREATE-containing literals and left exactly this shape invisible; W5-X1 exists to close it", faults)
	}

	// --- CONTROL 3: the same, spelled with `+=`, because a builder widening
	// a column list writes it that way as often as with `+`.
	const plantAppend = `package p
func schema() string {
	s := "CREATE TABLE IF NOT EXISTS person (person_id TEXT PRIMARY KEY"
	s += ", phone TEXT) STRICT;"
	return s
}`
	if faults = w5bScanDDL(t, "plant_append.go", plantAppend); !w5bHasShape(faults, "concatenated DDL") {
		t.Fatalf("CONTROL FAILED: the planted `+=`-assembled DDL was reported as %v", faults)
	}

	// --- CONTROL 4 (the NEGATIVE control, and it is not optional): a file
	// that names ALTER TABLE only in a COMMENT must scan CLEAN. `refdata.go`
	// is dense with comments about schema shape, and a naive text grep would
	// red the builder for documenting the very rule this criterion states.
	const plantComment = `package p
// Deliberately NOT an ALTER TABLE: IF NOT EXISTS cannot heal a changed shape,
// so widening means ADD COLUMN nowhere and a new CREATE literal instead.
func schema() string {
	return "CREATE TABLE IF NOT EXISTS person (person_id TEXT PRIMARY KEY, phone TEXT) STRICT;"
}`
	if faults = w5bScanDDL(t, "plant_comment.go", plantComment); len(faults) != 0 {
		t.Fatalf("CONTROL FAILED (negative): a file naming ALTER TABLE/ADD COLUMN only in a COMMENT was reported as violating: %v. The detector must read string literals, not text, or it reds honest documentation", faults)
	}

	// --- THE REAL SCAN, over W5b's touched files.
	root := w5bRepoRoot(t)
	for _, rel := range w5bTouchedFiles {
		path := filepath.Join(root, rel)
		b, err := os.ReadFile(path)
		if err != nil {
			// Rule 6: a file that has moved makes this scan silently
			// narrower, which is the failure mode the criterion exists to
			// prevent one level up.
			t.Fatalf("fixture defect: W5b's touched file %s could not be read: %v — the scan is scoped to a list, and a list that no longer names the code is a scan of nothing", rel, err)
		}
		if len(b) == 0 {
			t.Fatalf("fixture defect: %s is empty", rel)
		}
		for _, f := range w5bScanDDL(t, path, string(b)) {
			t.Errorf("W5-X1 (m5-X1): %s at %s — %s\n  Every W5 schema change is declared inside a SINGLE `CREATE `-containing Go string literal. DDL outside such a literal is invisible to W1's guard 3a as W4 narrowed it, and a widened `person` table declared by ALTER means the CREATE literal and the real table disagree from that moment on.",
				f.Shape, f.Where, f.Text)
		}
	}
}

func w5bHasShape(faults []w5bDDLFault, shape string) bool {
	for _, f := range faults {
		if f.Shape == shape {
			return true
		}
	}
	return false
}
