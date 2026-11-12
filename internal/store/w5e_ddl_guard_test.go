// w5e_ddl_guard_test.go — W5-X1 for W5e: "Every schema change in W5 is
// declared inside a single `CREATE `-containing Go string literal. No
// ALTER TABLE, no ADD COLUMN, no DDL assembled by concatenating a column
// list onto a prefix."
// design/WINDOW-BUILD-PLAN-v1.md "#### W5-wide criteria", W5-X1, LOCKED
// 2026-08-31. W5e touches schema (`trip.cargo_note`), so it owes W5-X1's
// controls — "one control per sub-chunk that touches schema" (W5-X6).
//
// W5-X1 has TWO halves and they have DIFFERENT pre-chunk states, which is
// worth saying because the lock marks the criterion ⟦NO PRE-CHUNK RED⟧ on
// the strength of the first half alone:
//
//	(i) the PROHIBITION — no ALTER TABLE / ADD COLUMN / concatenated DDL.
//	    `ALTER TABLE`/`ADD COLUMN` are ZERO non-test hits at 88dd6a6
//	    [MEASURED], so this half passes on arrival and its clean pass is not
//	    evidence. Its ledger evidence is the two planted controls plus
//	    (m5-X1).
//	(ii) the POSITIVE — the change IS declared inside a CREATE-containing
//	    literal. This half CANNOT pass pre-chunk: no literal anywhere
//	    declares `cargo_note`. It produces a genuine runtime RED, and it is
//	    the half that catches a builder who declares the column somewhere
//	    the prohibition scan is not looking.
//
// package store, DELIBERATELY: names no unbuilt symbol, compiles and runs
// against the clean tree.
//
// EXPECTED RED:
//   - pre-chunk: TestW5eSchemaChangeDeclaredInsideCreateLiteral FAILS
//     ("no CREATE-containing string literal declares cargo_note");
//     TestW5eSchemaChangeCarriesNoAlterOrConcatenatedDDL PASSES with both
//     plants produced.
//   - (m5-X1): the column added by `ALTER TABLE trip ADD COLUMN cargo_note
//     TEXT`, or the `trip` DDL rebuilt by `+`-concatenating a column list
//     onto a prefix -> the prohibition scan, naming file, line and shape.
package store

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var (
	w5eAlterTable = regexp.MustCompile(`(?i)\bALTER\s+TABLE\b`)
	w5eAddColumn  = regexp.MustCompile(`(?i)\bADD\s+COLUMN\b`)
	w5eAnyDDL     = regexp.MustCompile(`(?i)\b(CREATE\s+(TABLE|INDEX|TRIGGER|VIEW)|ALTER\s+TABLE|ADD\s+COLUMN)\b`)
	w5eCreateWord = regexp.MustCompile(`(?i)\bCREATE\s`)
)

// w5eDDLHit is one W5-X1 violation.
type w5eDDLHit struct {
	File   string
	Line   int
	Shape  string // "alter" | "addcolumn" | "concat" | "sprintf"
	Detail string
}

func (h w5eDDLHit) String() string {
	return h.File + ":" + w5eItoa(h.Line) + " [" + h.Shape + "] " + h.Detail
}

// w5eScanDDL is THE detector — driven by both plants and by the real pass.
func w5eScanDDL(t *testing.T, path string, src []byte) []w5eDDLHit {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var hits []w5eDDLHit
	add := func(pos token.Pos, shape, detail string) {
		hits = append(hits, w5eDDLHit{File: path, Line: fset.Position(pos).Line, Shape: shape, Detail: detail})
	}
	litText := func(e ast.Expr) (string, bool) {
		l, ok := e.(*ast.BasicLit)
		if !ok || l.Kind != token.STRING {
			return "", false
		}
		if s, err := strconv.Unquote(l.Value); err == nil {
			return s, true
		}
		return l.Value, true // raw backquoted strings unquote fine; belt and braces
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.BasicLit:
			s, ok := litText(v)
			if !ok {
				return true
			}
			if w5eAlterTable.MatchString(s) {
				add(v.Pos(), "alter", "string literal contains ALTER TABLE")
			}
			if w5eAddColumn.MatchString(s) {
				add(v.Pos(), "addcolumn", "string literal contains ADD COLUMN")
			}
		case *ast.BinaryExpr:
			if v.Op != token.ADD {
				return true
			}
			for _, side := range []ast.Expr{v.X, v.Y} {
				if s, ok := litText(side); ok && w5eAnyDDL.MatchString(s) {
					add(v.Pos(), "concat", "DDL string assembled by + concatenation")
					return true
				}
			}
		case *ast.CallExpr:
			sel, ok := v.Fun.(*ast.SelectorExpr)
			if !ok || len(v.Args) == 0 {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "fmt" || (sel.Sel.Name != "Sprintf" && sel.Sel.Name != "Sprint") {
				return true
			}
			if s, ok := litText(v.Args[0]); ok && w5eAnyDDL.MatchString(s) {
				add(v.Pos(), "sprintf", "DDL string assembled by fmt."+sel.Sel.Name)
			}
		}
		return true
	})
	sort.Slice(hits, func(i, j int) bool { return hits[i].Line < hits[j].Line })
	return hits
}

// w5eTouchSurface is W5e's declared schema touch surface: the two packages
// whose non-test sources can carry the `trip` table's DDL. `internal/store`
// holds the `trip` CREATE literal (store.go's `schema` const) and
// `internal/domain` holds every Projection.Schema literal.
func w5eTouchSurface(t *testing.T) []string {
	t.Helper()
	root := w1ModuleRoot(t)
	var out []string
	for _, dir := range []string{filepath.Join(root, "internal", "store"), filepath.Join(root, "internal", "domain")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

// w5eRequireSurface is rule 6: assert the arrangement this scan needs, and
// fail loudly rather than scanning an empty or truncated universe.
func w5eRequireSurface(t *testing.T) []string {
	t.Helper()
	files := w5eTouchSurface(t)
	need := map[string]bool{
		filepath.Join("internal", "store", "store.go"): false,
		filepath.Join("internal", "domain", "trip.go"): false,
	}
	for _, f := range files {
		for k := range need {
			if strings.HasSuffix(f, k) {
				need[k] = true
			}
		}
	}
	for k, ok := range need {
		if !ok {
			t.Fatalf("W5-X1 fixture defect: the touch surface does not include %s — the `trip` table's DDL lives there and a scan that cannot see it proves nothing", k)
		}
	}
	return files
}

// TestW5eSchemaChangeCarriesNoAlterOrConcatenatedDDL — W5-X1 half (i).
// ⟦NO PRE-CHUNK RED⟧: passes on arrival. The two plants below are the
// evidence; the clean pass is not.
func TestW5eSchemaChangeCarriesNoAlterOrConcatenatedDDL(t *testing.T) {
	// --- CONTROL 1: a planted ALTER TABLE … ADD COLUMN.
	plantAlter := "package planted\n\nconst migrate = `ALTER TABLE trip ADD COLUMN cargo_note TEXT;`\n"
	hits := w5eScanDDL(t, "planted/alter.go", []byte(plantAlter))
	var sawAlter, sawAdd bool
	for _, h := range hits {
		sawAlter = sawAlter || h.Shape == "alter"
		sawAdd = sawAdd || h.Shape == "addcolumn"
	}
	if !sawAlter || !sawAdd {
		t.Fatalf("W5-X1 CONTROL 1 FAILED: the planted `ALTER TABLE trip ADD COLUMN cargo_note TEXT` was not detected (alter=%v addColumn=%v, hits %v) — a scan that cannot detect its plant proves nothing (m5-X1)", sawAlter, sawAdd, hits)
	}

	// --- CONTROL 2 (SEPARATELY, as the lock requires): DDL built by
	// concatenating a column list onto a prefix.
	plantConcat := "package planted\n\nvar cols = \"cargo_note TEXT\"\n\nvar ddl = \"CREATE TABLE trip (\" + cols + \")\"\n\nvar ddl2 = fmt.Sprintf(\"CREATE TABLE trip (%s)\", cols)\n"
	hits2 := w5eScanDDL(t, "planted/concat.go", []byte(plantConcat))
	var sawConcat, sawSprintf bool
	for _, h := range hits2 {
		sawConcat = sawConcat || h.Shape == "concat"
		sawSprintf = sawSprintf || h.Shape == "sprintf"
	}
	if !sawConcat {
		t.Fatalf("W5-X1 CONTROL 2 FAILED: a DDL string built by `+` concatenation went undetected (hits %v) — this is the separate control the lock names, not a variant of control 1", hits2)
	}
	if !sawSprintf {
		t.Fatalf("W5-X1 CONTROL 2 FAILED: a DDL string built by fmt.Sprintf went undetected (hits %v)", hits2)
	}

	// --- NEGATIVE control: a single CREATE-containing literal is LAWFUL.
	clean := "package planted\n\nconst schema = `CREATE TABLE IF NOT EXISTS trip (\n\ttrip_id TEXT PRIMARY KEY,\n\tcargo_note TEXT\n) STRICT;`\n"
	if h := w5eScanDDL(t, "planted/clean.go", []byte(clean)); len(h) != 0 {
		t.Fatalf("W5-X1 CONTROL (negative) FAILED: the scan flagged a lawful single CREATE literal: %v", h)
	}

	// --- the real pass over W5e's touch surface -------------------------
	var found []w5eDDLHit
	for _, path := range w5eRequireSurface(t) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		found = append(found, w5eScanDDL(t, path, src)...)
	}
	if len(found) != 0 {
		var lines []string
		for _, h := range found {
			lines = append(lines, h.String())
		}
		t.Fatalf("W5-X1 (m5-X1): W5e's touch surface carries ALTER TABLE / ADD COLUMN / assembled DDL. Every schema change in W5 is declared inside a single CREATE-containing Go string literal — a migration statement here is invisible to W1's literal-scanning guards:\n%s", strings.Join(lines, "\n"))
	}
}

// TestW5eSchemaChangeDeclaredInsideCreateLiteral — W5-X1 half (ii). This is
// the half with a genuine pre-chunk RED.
func TestW5eSchemaChangeDeclaredInsideCreateLiteral(t *testing.T) {
	// CONTROL: the finder must find a declaration when one exists, and must
	// NOT accept a declaration that is not inside a CREATE literal.
	good := "package planted\n\nconst schema = `CREATE TABLE IF NOT EXISTS trip (\n\tcargo_note TEXT\n) STRICT;`\n"
	if n := w5eCreateLiteralsDeclaring(t, "planted/good.go", []byte(good), w5eCargoColumn); n != 1 {
		t.Fatalf("W5-X1(ii) CONTROL FAILED: the finder saw %d CREATE literal(s) declaring %q in a source that has exactly one", n, w5eCargoColumn)
	}
	bad := "package planted\n\nconst migrate = `ALTER TABLE trip ADD COLUMN cargo_note TEXT;`\n\nconst sel = \"SELECT cargo_note FROM trip\"\n"
	if n := w5eCreateLiteralsDeclaring(t, "planted/bad.go", []byte(bad), w5eCargoColumn); n != 0 {
		t.Fatalf("W5-X1(ii) CONTROL FAILED: the finder counted %d CREATE literal(s) in a source whose only declarations are an ALTER TABLE and a SELECT — it would green a migration-shaped build", n)
	}

	total := 0
	for _, path := range w5eRequireSurface(t) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		total += w5eCreateLiteralsDeclaring(t, path, src, w5eCargoColumn)
	}
	if total == 0 {
		t.Fatalf("W5-X1(ii): no CREATE-containing string literal in W5e's touch surface declares %q — item 3's projection column is either unbuilt (the pre-chunk state) or declared somewhere the W1 literal guards and the W5-X1 prohibition scan cannot see", w5eCargoColumn)
	}
}

// w5eCreateLiteralsDeclaring counts the string literals in one source that
// contain BOTH a `CREATE ` keyword and the given column identifier — "a
// single CREATE-containing Go string literal", made checkable.
func w5eCreateLiteralsDeclaring(t *testing.T, path string, src []byte, column string) int {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	colWord := regexp.MustCompile(`\b` + regexp.QuoteMeta(column) + `\b`)
	n := 0
	ast.Inspect(f, func(node ast.Node) bool {
		lit, ok := node.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil {
			s = lit.Value
		}
		if w5eCreateWord.MatchString(s) && colWord.MatchString(s) && !w5eAlterTable.MatchString(s) {
			n++
		}
		return true
	})
	return n
}
