// w5e_content_scan_test.go — W5e-2's SCAN half: "Absent = not hauling. No
// flag, no taxonomy, no code branches on cargo content."
// design/WINDOW-BUILD-PLAN-v1.md "#### W5e — item 3: cargo note", LOCKED
// 2026-08-31.
//
// ⟦NO PRE-CHUNK RED for the scan half⟧ — `cargo` does not exist at 88dd6a6,
// so the clean pass is TRUE ON ARRIVAL and is NOT evidence. The locked text
// is explicit about this: "the plant is the whole of the evidence, and a
// narrow filter with a matching plant is a fully green row proving nothing."
// The ledger row is therefore the FOUR planted-violation CONTROLs — one per
// lettered shape, each also produced in single-assignment-ALIASED form — plus
// (m5e-2). Never the clean pass.
//
// THE FILTER, verbatim from the lock. An AST walk over production Go
// flagging, for any expression that reads TripDispatched.CargoNote,
// Trip.CargoNote or the `cargo_note` column:
//
//	(a) its use as an operand of ==/!= against any literal other than the
//	    nil/absent test;
//	(b) its use as an argument to strings.Contains / HasPrefix / HasSuffix /
//	    EqualFold / ToLower or any regexp method;
//	(c) its use as a switch tag or case value;
//	(d) its use in a len(…) comparison other than against zero.
//
// TWO deliberate strengthenings, stated so neither reads as drift:
//
//  1. ALIAS TRACKING. `cn := *t.CargoNote; if strings.Contains(cn, …)` is
//     shape (b) with one assignment in front of it. A selector-only walk
//     misses it, and "a narrow filter with a matching plant is a fully green
//     row proving nothing" is the defect this criterion was rewritten to
//     close. Single-assignment aliases (an identifier assigned EXACTLY once
//     in its function, from an expression that reads the cargo note) are
//     resolved to a fixpoint and treated as the cargo expression. Every
//     lettered plant below is produced in BOTH forms.
//  2. Shape (b)'s call set is the whole of `strings`, `bytes`, `regexp` and
//     `unicode` rather than only the five named verbs. A widened filter can
//     only catch MORE than the criterion names; and there is no lawful reason
//     for production code to hand the note to any of them, since every such
//     call is either a content branch (W5e-2) or a normalisation (W5e-1).
//
// AND ONE CORRECTION, 2026-09-01, from W5e's mutant gate (finding G7 —
// `reports/window-build/W5e-falsification.md` §4.4 escape 3, §9):
//
//  3. SHAPE (a) NOW READS "ANY LITERAL" THE WAY THE LOCK WRITES IT. As first
//     built, shape (a) tested `*ast.BasicLit`, so `*p.CargoNote ==
//     someNamedConst` — an `*ast.Ident` — was NOT flagged. The gate planted
//     exactly that taxonomy in `applyTripDispatched` and BOTH halves of
//     W5e-2 stayed green: a live hazmat classification, compiling, vetting
//     and passing. And all four of shape (a)'s plants compared against
//     `*ast.BasicLit`s, so the control fired over a filter one AST node type
//     narrower than its own subject — "a narrow filter with a matching
//     plant", the shape this lock was rewritten to prevent, found here for
//     the fourth time on this project. The filter is now INVERTED rather
//     than re-enumerated (w5eIsLiteralComparand), and THREE plants the old
//     filter MISSED are part of shape (a)'s control row, so the control can
//     no longer pass over a narrow filter.
//
// AND A SECOND CORRECTION, 2026-09-10 (reports/witness/W5E-CARGO-HOLE.md —
// the same class of hole guard 3b closed the day before, GUARD3B-FRAGMENT.md):
//
//  4. THE SQL HALF READS THE SQL A FUNCTION BUILDS, NOT ONLY THE LITERALS IT
//     CONTAINS. As first built it judged each string literal on its own, so
//     `q += " AND cargo_note IS NOT NULL"` after a WHERE literal — a
//     fragment carrying the column and no keyword — was nothing, while the
//     same predicate inside the one literal was RED. MEASURED at ca9fa2b by
//     planting exactly that in the real trip.go scoped-recompute read: the
//     clean-tree test PASSED; the one-literal spelling FAILED at
//     trip.go:1334. Layer 1 (per literal) is unchanged; layer 2 judges every
//     string a function assembles as ONE SQL text, through guard 3b's
//     w1SQLReadsInFunc. See w5eScanCargoBranches and w5eBuiltSQLConsts.
//
// package store, DELIBERATELY (W1's / W3's / W4's scan precedent, and it
// reuses their machinery: w1ModuleRoot, w1SQLReadContext, w1SQLReadsInFunc,
// w1PackageStringConsts). It names no unbuilt Go symbol, so it COMPILES AND
// RUNS against the clean tree.
//
// EXPECTED RED, per rule 5:
//   - pre-chunk: PASSES (⟦NO PRE-CHUNK RED⟧). The four controls are produced
//     and are the evidence.
//   - (m5e-2), the plan's named mutant — "an `if strings.Contains(cargo, …)`
//     branch", or any taxonomy/flag derived from content: shape (b) fires
//     with the file, line and the enclosing function named.
//   - the same mutant written as `switch *t.CargoNote`, `== "none"`,
//     `== someNamedConst` (G7) or `len(note) > 512`: shapes (c), (a), (d).
//   - the same mutant written in SQL — `WHERE cargo_note = ?`,
//     `CASE WHEN cargo_note …`, `ORDER BY length(cargo_note)`: the SQL shape.
//   - the same SQL mutant BUILT across Go statements — `q += " AND
//     cargo_note = ?"` after a WHERE literal, a Sprintf'd predicate, a
//     strings.Builder, a joined clause list, a same-package const fragment:
//     the SQL shape, layer 2 (correction 4), naming the construction.
package store

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// w5eCargoSelector is the Go field name the scan follows. It is the ONE
// Go-identifier spelling W5e-1b pins on the wire as `cargo_note`.
const w5eCargoSelector = "CargoNote"

// w5eCargoColumn is the SQL identifier half of the same subject.
const w5eCargoColumn = "cargo_note"

// w5eBranchHit is one flagged content branch.
type w5eBranchHit struct {
	File   string
	Line   int
	Shape  string // "a" | "b" | "c" | "d" | "sql"
	Detail string
}

func (h w5eBranchHit) String() string {
	return h.File + ":" + w5eItoa(h.Line) + " [" + h.Shape + "] " + h.Detail
}

func w5eItoa(n int) string {
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

// w5eStringInspectionPkgs is shape (b)'s widened call set (see the header).
var w5eStringInspectionPkgs = map[string]bool{
	"strings": true, "bytes": true, "regexp": true, "unicode": true,
}

// w5eRegexpMethods is the *regexp.Regexp method set, matched by NAME because
// the scan is syntactic and has no type information. "any regexp method" is
// the criterion's own wording.
var w5eRegexpMethods = map[string]bool{
	"Match": true, "MatchString": true, "Find": true, "FindString": true,
	"FindStringIndex": true, "FindStringSubmatch": true, "FindAllString": true,
	"FindAllStringSubmatch": true, "FindAllStringIndex": true,
	"ReplaceAllString": true, "ReplaceAllStringFunc": true, "ReplaceAll": true,
	"Split": true, "SubexpIndex": true,
}

// w5eNamedVerbs is the criterion's literal five, kept as a named set so a
// reader can see the lettered shape is covered exactly as written even though
// the implementation admits more.
var w5eNamedVerbs = map[string]bool{
	"Contains": true, "HasPrefix": true, "HasSuffix": true,
	"EqualFold": true, "ToLower": true,
}

// --- the walk ---------------------------------------------------------------

// w5eScanCargoBranches parses one Go source and returns every content branch
// on the cargo note. THE detector — the controls and the real pass both drive
// this one function. consts is the enclosing package's string-const table
// (w5eBuiltSQLConsts over w1PackageStringConsts in the real pass;
// w5ePlantConsts for a planted source), from which the SQL half's layer 2
// splices a fragment named by a package-level identifier.
func w5eScanCargoBranches(t *testing.T, path string, src []byte, consts map[string][]w1SQLPiece) []w5eBranchHit {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var hits []w5eBranchHit
	add := func(pos token.Pos, shape, detail string) {
		hits = append(hits, w5eBranchHit{File: path, Line: fset.Position(pos).Line, Shape: shape, Detail: detail})
	}

	// SQL half, layer 1 — every string literal in the file, regardless of
	// where it sits (package const, struct field, function body), judged on
	// its own. UNCHANGED by layer 2 below, so no literal that was RED before
	// correction 4 can be GREEN after it.
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		for _, detail := range w5eSQLCargoReads(lit.Value) {
			add(lit.Pos(), "sql", detail)
		}
		return true
	})

	// SQL half, layer 2 (correction 4) — the SQL a function BUILDS across
	// several statements. `q += " AND cargo_note = ?"` after a WHERE literal
	// is the same read as the one-literal spelling, and per-literal judgement
	// cannot see it: the fragment carries the column and no keyword. Guard
	// 3b's machinery (w1SQLReadsInFunc, w1_scanutil_test.go) expands each
	// function's assembled strings — `+`, `+=`, locals, append + strings.Join,
	// a strings.Builder, fmt.Sprintf with verbs substituted in place, and
	// same-package consts by name — into streams and judges each stream as
	// ONE SQL text with a clause-context machine. Only its built-stream
	// findings (rank 2) are taken here: its own per-literal layer is the raw
	// w1SQLReadContext, without the comment strip and declaration drop
	// w5eSQLCargoReads applies, and layer 1 above already owns every single
	// literal. Inside w1SQLReadsInFunc a rank-1 finding suppresses a rank-2
	// one on the same line, so a declaration-only DDL literal inside a
	// function is as green under layer 2 as under layer 1; a declaration
	// spliced by NAME has no rank-1 line to hide behind, which is what
	// w5eBuiltSQLConsts is for.
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		for _, r := range w1SQLReadsInFunc(fset, fn, []string{w5eCargoColumn}, consts) {
			if r.rank != 2 {
				continue
			}
			hits = append(hits, w5eBranchHit{File: path, Line: r.Line, Shape: "sql",
				Detail: "SQL filtering/ordering/branching read of " + r.Col + " in " + fn.Name.Name + r.Detail})
		}
	}

	// Go half — per function, so aliases are scoped and the message can name
	// the enclosing function.
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		aliases := w5eCargoAliases(fn)
		where := " in " + fn.Name.Name
		reads := func(e ast.Expr) bool { return w5eReadsCargo(e, aliases) }

		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.BinaryExpr:
				if v.Op != token.EQL && v.Op != token.NEQ &&
					v.Op != token.LSS && v.Op != token.GTR && v.Op != token.LEQ && v.Op != token.GEQ {
					return true
				}
				// (d) len(cargo) compared against anything but the literal 0.
				if lenSide, other, ok := w5eLenComparison(v, aliases); ok {
					if !w5eIsZeroLiteral(other) {
						add(v.Pos(), "d", "len("+lenSide+") compared against a non-zero operand"+where)
					}
					return true
				}
				if v.Op != token.EQL && v.Op != token.NEQ {
					return true
				}
				// (a) equality against a LITERAL. The test is INVERTED
				// rather than an *ast.BasicLit one — see
				// w5eIsLiteralComparand, and gate finding G7 for what the
				// narrow version let through. The nil/absent test is the one
				// operand the criterion exempts, and it is exempted by name.
				if reads(v.X) && w5eIsLiteralComparand(v.Y, aliases) {
					add(v.Pos(), "a", "cargo note compared with "+v.Op.String()+" against "+w5eComparandText(v.Y)+where)
				}
				if reads(v.Y) && w5eIsLiteralComparand(v.X, aliases) {
					add(v.Pos(), "a", "cargo note compared with "+v.Op.String()+" against "+w5eComparandText(v.X)+where)
				}
			case *ast.CallExpr:
				// (b) inspection call with the note as an argument.
				sel, ok := v.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				var why string
				if pkg, ok := sel.X.(*ast.Ident); ok && w5eStringInspectionPkgs[pkg.Name] {
					why = pkg.Name + "." + sel.Sel.Name
				} else if w5eRegexpMethods[sel.Sel.Name] {
					why = "regexp method ." + sel.Sel.Name
				} else if w5eNamedVerbs[sel.Sel.Name] {
					why = "." + sel.Sel.Name
				}
				if why == "" {
					return true
				}
				for _, arg := range v.Args {
					if reads(arg) {
						add(v.Pos(), "b", "cargo note passed to "+why+where)
						break
					}
				}
			case *ast.SwitchStmt:
				// (c) switch tag.
				if v.Tag != nil && reads(v.Tag) {
					add(v.Pos(), "c", "cargo note used as a switch tag"+where)
				}
			case *ast.CaseClause:
				// (c) case value.
				for _, e := range v.List {
					if reads(e) {
						add(v.Pos(), "c", "cargo note used as a case value"+where)
						break
					}
				}
			}
			return true
		})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].File != hits[j].File {
			return hits[i].File < hits[j].File
		}
		return hits[i].Line < hits[j].Line
	})
	return hits
}

// --- the SQL sub-detector ---------------------------------------------------
//
// It is NOT w1SQLReadContext applied to the whole literal, and the reason is
// a FALSE POSITIVE that was produced and fixed during this writing
// [MEASURED]: w1SQLReadContext uppercases a literal, finds the first
// WHERE/ORDER BY/GROUP BY/HAVING/CASE/ON keyword, and then looks for the
// column anywhere after it. `internal/store/store.go`'s `schema` const is ONE
// literal holding the whole database, and its prose comments contain ordinary
// English "on" — so once `cargo_note` is declared in the `trip` CREATE, the
// whole schema literal reads as an ordering branch. That false RED would have
// been "fixed" by weakening the detector, which is how a tooth gets pulled.
//
// So: strip SQL comments, split on the statement terminator, drop
// declaration-only statements (CREATE TABLE / INDEX / TRIGGER carrying no
// SELECT — those are W5-X1's territory, not W5e-2's), and only then ask
// w1SQLReadContext about each remaining statement. A CREATE VIEW, or a
// CREATE TABLE … AS SELECT, is NOT dropped: a view that classifies trips by
// what their note says is exactly the taxonomy D-21.3 declined.
var (
	w5eSQLLineComment  = regexp.MustCompile(`--[^\n]*`)
	w5eSQLBlockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	w5eSQLDeclOnly     = regexp.MustCompile(`(?is)^\s*` + "`?" + `\s*CREATE\s+(TABLE|INDEX|TRIGGER)\b`)
	w5eSQLHasSelect    = regexp.MustCompile(`(?i)\bSELECT\b`)
)

func w5eSQLCargoReads(rawLit string) []string {
	if !strings.Contains(rawLit, w5eCargoColumn) {
		return nil
	}
	body := w5eSQLBlockComment.ReplaceAllString(rawLit, " ")
	body = w5eSQLLineComment.ReplaceAllString(body, " ")
	var out []string
	for _, stmt := range strings.Split(body, ";") {
		if !strings.Contains(stmt, w5eCargoColumn) {
			continue
		}
		if w5eSQLDeclOnly.MatchString(stmt) && !w5eSQLHasSelect.MatchString(stmt) {
			continue // a declaration, not a read
		}
		for _, c := range w1SQLReadContext(stmt, []string{w5eCargoColumn}) {
			out = append(out, "SQL filtering/ordering/branching read of "+c)
		}
	}
	return out
}

// w5eBuiltSQLConsts is the package string-const table layer 2 splices from,
// with layer 1's declaration policy applied to it: a const in which every
// statement naming cargo_note is a declaration w5eSQLCargoReads would drop
// (CREATE TABLE / INDEX / TRIGGER carrying no SELECT — store.go's `schema`)
// is removed, so `db.Exec(schema)` in store.Open is judged the way layer 1
// judges the literal itself: a declaration, not a read. The clause machine
// has no declaration drop, and inside a CREATE an `ON CONFLICT` column clause
// or an index's `ON trip (…)` opens context that nothing closes before the
// column, so without this the splice line reds [MEASURED with this function
// stubbed to the identity: the (iii) negative control FAILS]. A const that
// names the column outside a declaration (a CREATE VIEW, a SELECT list) is
// kept — a fragment splices from it, and layer 1 reds its literal in any
// case. A const that never names the column is kept too: it may open the
// context a later fragment carries the column into. ⚠️ STATED BOUND, layer
// 1's and now layer 2's: a trigger whose WHEN names the note and whose body
// holds no SELECT is a declaration under this policy, in either layer.
func w5eBuiltSQLConsts(consts map[string][]w1SQLPiece) map[string][]w1SQLPiece {
	out := make(map[string][]w1SQLPiece, len(consts))
	for name, pieces := range consts {
		var b strings.Builder
		for _, p := range pieces {
			b.WriteString(p.Text)
		}
		if w5eDeclarationConst(b.String()) {
			continue
		}
		out[name] = pieces
	}
	return out
}

// w5eDeclarationConst reports whether text names cargo_note (outside SQL
// comments) and every statement that names it is declaration-only.
func w5eDeclarationConst(text string) bool {
	body := w5eSQLBlockComment.ReplaceAllString(text, " ")
	body = w5eSQLLineComment.ReplaceAllString(body, " ")
	if !strings.Contains(body, w5eCargoColumn) {
		return false
	}
	for _, stmt := range strings.Split(body, ";") {
		if !strings.Contains(stmt, w5eCargoColumn) {
			continue
		}
		if !w5eSQLDeclOnly.MatchString(stmt) || w5eSQLHasSelect.MatchString(stmt) {
			return false
		}
	}
	return true
}

// w5ePlantConsts is the const table for planted sources — the seam the
// controls drive, so a same-package const fragment resolves in a plant
// exactly as it does in the real pass.
func w5ePlantConsts(t *testing.T, srcs ...[]byte) map[string][]w1SQLPiece {
	t.Helper()
	return w5eBuiltSQLConsts(w1StringConstsFromSources(t, srcs...))
}

// w5eReadsCargo reports whether e reads the cargo note — directly through a
// `.CargoNote` selector, or through a single-assignment alias of one.
func w5eReadsCargo(e ast.Expr, aliases map[string]bool) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if found {
			return false
		}
		switch v := n.(type) {
		case *ast.SelectorExpr:
			if v.Sel.Name == w5eCargoSelector {
				found = true
				return false
			}
		case *ast.Ident:
			if aliases[v.Name] {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// w5eCargoAliases resolves the function's single-assignment aliases of the
// cargo note to a fixpoint. An identifier assigned MORE THAN ONCE is NOT an
// alias: it may hold something else by the time it is read, and calling it
// cargo would be a false positive that gets "fixed" by weakening the scan.
func w5eCargoAliases(fn *ast.FuncDecl) map[string]bool {
	type binding struct {
		rhs   ast.Expr
		count int
	}
	bind := map[string]*binding{}
	record := func(lhs, rhs ast.Expr) {
		id, ok := lhs.(*ast.Ident)
		if !ok || id.Name == "_" {
			return
		}
		b := bind[id.Name]
		if b == nil {
			b = &binding{}
			bind[id.Name] = b
		}
		b.count++
		b.rhs = rhs
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.AssignStmt:
			if len(v.Lhs) == len(v.Rhs) {
				for i := range v.Lhs {
					record(v.Lhs[i], v.Rhs[i])
				}
			} else {
				for _, l := range v.Lhs { // multi-value: unresolvable, but count it
					record(l, nil)
				}
			}
		case *ast.ValueSpec:
			for i, name := range v.Names {
				var rhs ast.Expr
				if i < len(v.Values) {
					rhs = v.Values[i]
				}
				record(name, rhs)
			}
		case *ast.IncDecStmt:
			record(v.X, nil)
		}
		return true
	})
	aliases := map[string]bool{}
	for i := 0; i < 8; i++ { // fixpoint; 8 is far past any real chain
		grew := false
		for name, b := range bind {
			if aliases[name] || b.count != 1 || b.rhs == nil {
				continue
			}
			if w5eReadsCargo(b.rhs, aliases) {
				aliases[name] = true
				grew = true
			}
		}
		if !grew {
			break
		}
	}
	return aliases
}

// w5eLenComparison reports whether one side of v is `len(<cargo>)`, returning
// the printed argument and the OTHER operand.
func w5eLenComparison(v *ast.BinaryExpr, aliases map[string]bool) (string, ast.Expr, bool) {
	isLenOfCargo := func(e ast.Expr) (string, bool) {
		call, ok := e.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return "", false
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok || id.Name != "len" {
			return "", false
		}
		if !w5eReadsCargo(call.Args[0], aliases) {
			return "", false
		}
		return "cargo note", true
	}
	if s, ok := isLenOfCargo(v.X); ok {
		return s, v.Y, true
	}
	if s, ok := isLenOfCargo(v.Y); ok {
		return s, v.X, true
	}
	return "", nil, false
}

// w5eIsLiteralComparand reports whether e, the OTHER operand of an ==/!=
// against the cargo note, makes that comparison shape (a).
//
// ⚠️ WIDENED 2026-09-01, gate finding G7 — read this before narrowing it back.
// This function replaces `w5eIsBasicLit`, which tested `*ast.BasicLit` and
// nothing else. The criterion's own words are "against ANY LITERAL other than
// the nil/absent test", and a NAMED CONSTANT is a literal by any reading — so
// `*p.CargoNote == hazmatToken` was a live content taxonomy the scan did not
// flag. The gate planted exactly that in applyTripDispatched and both halves
// of W5e-2 stayed green [W5e-falsification.md §4.4 escape 3].
//
// The fix is deliberately NOT "add `case *ast.Ident`". Enumerating the AST
// node types a literal may wear is what went narrow the first time, and a
// second enumeration would go narrow again at the first shape nobody listed
// (`pkg.Const`, `Kind("x")`, an untyped const expression). So the test is
// INVERTED: every operand is a comparand EXCEPT the two that shape (a)'s own
// text puts outside it —
//
//   - `nil`, the nil/absent test, exempt in the criterion's own words; and
//   - another read of the cargo note. `*a.CargoNote == *b.CargoNote` is not a
//     comparison "against a literal" at all, so it is outside shape (a)'s
//     subject. ⚠️ STATED BOUND, not an oversight: the pre-G7 filter did not
//     flag it either, so exempting it introduces no hole this fix created —
//     but it IS a branch on cargo content in the criterion's HEADLINE sense,
//     and if anyone ever writes one, shape (a) will not see it.
//
// Like shape (b)'s widened call set, this can only catch MORE than the lock's
// words name; it cannot catch less.
func w5eIsLiteralComparand(e ast.Expr, aliases map[string]bool) bool {
	if id, ok := e.(*ast.Ident); ok && id.Name == "nil" {
		return false
	}
	return !w5eReadsCargo(e, aliases)
}

// w5eComparandText renders the flagged operand for the failure message. It
// names the SHAPE of the comparand as well as its text, because "against
// named constant hazmatToken" is the message that would have made G7 visible.
func w5eComparandText(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.BasicLit:
		return "literal " + v.Value
	case *ast.Ident:
		return "named constant/identifier " + v.Name
	case *ast.SelectorExpr:
		if x, ok := v.X.(*ast.Ident); ok {
			return "named constant/identifier " + x.Name + "." + v.Sel.Name
		}
	}
	return "a non-nil, non-cargo operand"
}

func w5eIsZeroLiteral(e ast.Expr) bool {
	l, ok := e.(*ast.BasicLit)
	return ok && l.Kind == token.INT && l.Value == "0"
}

// --- the plants -------------------------------------------------------------

// One planted violation PER LETTERED SHAPE — four in total, as the lock
// requires — each in DIRECT and single-assignment-ALIASED form.
var w5ePlants = []struct {
	Shape string
	Name  string
	Want  int // exact hits this plant must produce for its own shape
	Src   string
}{
	// ⚠️ Shape (a) carries FIVE plants, not two, and the reason is gate
	// finding G7. The first two compare against *ast.BasicLit string
	// literals — which is what the ORIGINAL filter tested, so those two
	// passed over a filter narrower than the criterion they were quoting.
	// The last three are the shapes that filter MISSED: a direct named
	// constant, the same through an alias, and a package-qualified constant.
	// They are here so this control can never again go green over a narrow
	// (a). Measured: with the pre-fix `w5eIsBasicLit` filter this plant
	// yields 2 hits against a Want of 5 and the control FAILS.
	{"a", "equality against a literal, INCLUDING a named constant (G7)", 5, `package planted

import "sitecontrol/internal/domain"

const hazmatToken = "fuel"

func directA(t *domain.TripDispatched) bool {
	if *t.CargoNote == "empty" {
		return true
	}
	return false
}

func aliasedA(t *domain.TripDispatched) bool {
	cn := *t.CargoNote
	return cn != "none"
}

// G7: an *ast.Ident comparand, invisible to the pre-fix *ast.BasicLit filter.
func namedConstA(t *domain.TripDispatched) bool {
	return *t.CargoNote == hazmatToken
}

// G7, aliased form — the file's standing discipline that every lettered plant
// exists both directly and behind a single-assignment alias.
func aliasedNamedConstA(t *domain.TripDispatched) bool {
	cn := *t.CargoNote
	return cn != hazmatToken
}

// G7: a PACKAGE-QUALIFIED constant, an *ast.SelectorExpr. Present so the fix
// cannot be read as "one more node type added to a list" — an enumeration is
// what went narrow the first time, and this operand is not on any list.
func qualifiedConstA(t *domain.TripDispatched) bool {
	return *t.CargoNote == domain.HazmatToken
}
`},
	{"b", "inspection call on the note", 2, `package planted

import (
	"regexp"
	"strings"

	"sitecontrol/internal/domain"
)

var hazmat = regexp.MustCompile("fuel|acid")

func directB(t *domain.TripDispatched) bool {
	return strings.Contains(*t.CargoNote, "fuel")
}

func aliasedB(t *domain.TripDispatched) bool {
	cn := *t.CargoNote
	return hazmat.MatchString(cn)
}
`},
	{"c", "switch tag / case value", 2, `package planted

import "sitecontrol/internal/domain"

func directC(t *domain.TripDispatched) string {
	switch *t.CargoNote {
	case "fuel":
		return "hazmat"
	}
	return ""
}

func aliasedC(t *domain.TripDispatched, other string) string {
	cn := *t.CargoNote
	switch other {
	case cn:
		return "same"
	}
	return ""
}
`},
	{"d", "len comparison against non-zero", 2, `package planted

import "sitecontrol/internal/domain"

func directD(t *domain.TripDispatched) bool {
	return len(*t.CargoNote) > 512
}

func aliasedD(t *domain.TripDispatched) bool {
	cn := *t.CargoNote
	return len(cn) >= 1024
}
`},
	{"sql", "SQL branch on the column", 4, `package planted

const (
	qEq    = "SELECT trip_id FROM trip WHERE cargo_note = ?"
	qLike  = "SELECT trip_id FROM trip WHERE cargo_note LIKE '%fuel%'"
	qCase  = "SELECT CASE WHEN cargo_note IS NOT NULL THEN 1 ELSE 0 END AS hauling FROM trip"
	qOrder = "SELECT trip_id FROM trip ORDER BY length(cargo_note)"
)
`},
	// ⚠️ Shape "sql", BUILT form — the per-literal hole (W5E-CARGO-HOLE,
	// 2026-09-10; the same class as guard 3b's, GUARD3B-FRAGMENT.md). Every
	// function below spells one of the four consts above ACROSS Go statements,
	// so the literal that carries cargo_note holds no WHERE / ORDER BY / CASE
	// keyword and the per-literal judgement is silent on it. MEASURED at
	// ca9fa2b: this plant yields 0 hits against a Want of 10 and the control
	// FAILS — and the first shape, planted in the real trip.go scoped-recompute
	// read, passed the clean-tree test the same day. Each function is exactly
	// ONE hit, attributed to the line carrying the column — asserted per
	// function name below the count, in w5eBuiltPlantFuncs.
	{"sql", "SQL branch BUILT across statements (+= / + / call arg / local / Sprintf / Builder / Join / const splice / return)", 10, w5eBuiltPlantSrc},
}

// w5eBuiltPlantFuncs names the ten functions of w5eBuiltPlantSrc; the control
// requires EXACTLY one hit in each, so a construction shape layer 2 misses
// cannot hide behind one it counts twice.
var w5eBuiltPlantFuncs = []string{
	"plusAssign", "plusEquals", "callArg", "viaLocal", "sprintfWhere",
	"sprintfOrder", "builder", "joined", "constSplice", "returned",
}

const w5eBuiltPlantSrc = `package planted

import (
	"database/sql"
	"fmt"
	"strings"
)

const base = "SELECT trip_id FROM trip WHERE origin_site = ?"

// A same-package const fragment, spliced by name in constSplice.
const cargoPred = " AND cargo_note = ?"

// The store.EachEvent shape, and the one planted in the real tree.
func plusAssign(db *sql.DB) {
	q := "SELECT trip_id FROM trip WHERE origin_site = ?"
	q += " AND cargo_note IS NOT NULL"
	db.Query(q)
}

func plusEquals(db *sql.DB) {
	q := base
	q = q + " AND cargo_note LIKE '%fuel%'"
	db.Query(q)
}

func callArg(db *sql.DB) {
	db.Query(base + " AND cargo_note <> ''")
}

func viaLocal(db *sql.DB) {
	frag := " AND cargo_note = ?"
	q := "SELECT trip_id FROM trip WHERE origin_site = ?" + frag
	db.Query(q)
}

func sprintfWhere(db *sql.DB) {
	db.Query(fmt.Sprintf("SELECT trip_id FROM trip WHERE %s", "cargo_note = ?"))
}

func sprintfOrder(db *sql.DB) {
	db.Query(fmt.Sprintf("SELECT trip_id FROM trip ORDER BY %s", "length(cargo_note)"))
}

func builder(db *sql.DB) {
	var b strings.Builder
	b.WriteString("SELECT trip_id FROM trip WHERE 1 = 1")
	b.WriteString(" AND cargo_note = ?")
	db.Query(b.String())
}

func joined(db *sql.DB) {
	var where []string
	where = append(where, "origin_site = ?")
	where = append(where, "cargo_note = ?")
	db.Query("SELECT trip_id FROM trip WHERE " + strings.Join(where, " AND "))
}

func constSplice(db *sql.DB) {
	db.Query(base + cargoPred)
}

func returned() string {
	return base + " AND cargo_note = 'none'"
}
`

// w5eCleanPlant is the NEGATIVE control: every LAWFUL use of the note the
// criterion explicitly permits. The scan must find NOTHING here, or it would
// false-red a correct build and get weakened in response.
const w5eCleanPlant = `package planted

import "sitecontrol/internal/domain"

// Carriage: assignment, dereference into a payload, INSERT args.
func carry(t *domain.TripDispatched) *string { return t.CargoNote }

// The nil/absent test — explicitly exempt.
func absent(t *domain.TripDispatched) bool  { return t.CargoNote == nil }
func present(t *domain.TripDispatched) bool { return t.CargoNote != nil }

// A len comparison AGAINST ZERO — explicitly exempt.
func lenZero(t *domain.TripDispatched) bool {
	cn := ""
	if t.CargoNote != nil {
		cn = *t.CargoNote
	}
	return len(cn) == 0
}

// Storage plumbing: the column named in an INSERT column list and a SELECT
// list, never after a filtering/ordering keyword.
const (
	qInsert = "INSERT INTO trip (trip_id, cargo_note) VALUES (?, ?)"
	qSelect = "SELECT trip_id, cargo_note FROM trip"
)
`

// TestW5eContentScanDetectsEveryPlantedShape — W5e-2's CONTROL row. This test
// IS the ledger evidence for W5e-2's scan half; the clean pass below is not.
func TestW5eContentScanDetectsEveryPlantedShape(t *testing.T) {
	for _, p := range w5ePlants {
		hits := w5eScanCargoBranches(t, "planted/"+p.Shape+".go", []byte(p.Src), w5ePlantConsts(t, []byte(p.Src)))
		var forShape []w5eBranchHit
		for _, h := range hits {
			if h.Shape == p.Shape {
				forShape = append(forShape, h)
			}
		}
		want := p.Want
		if len(forShape) != want {
			t.Fatalf("W5e-2 CONTROL shape (%s) FAILED — %s: the scan found %d hit(s), want %d. A scan that cannot detect its own plant is a fully green row proving nothing (the lock's own words). All hits: %v",
				p.Shape, p.Name, len(forShape), want, hits)
		}
	}

	// The built plant again, PER FUNCTION (correction 4): ten shapes, one hit
	// each, by name — the count above cannot tell a missed shape from a
	// double-counted one.
	builtHits := w5eScanCargoBranches(t, "planted/built.go", []byte(w5eBuiltPlantSrc), w5ePlantConsts(t, []byte(w5eBuiltPlantSrc)))
	for _, fn := range w5eBuiltPlantFuncs {
		n := 0
		for _, h := range builtHits {
			if h.Shape == "sql" && strings.Contains(h.Detail, " in "+fn+" ") {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("W5e-2 CONTROL (built SQL, %s) FAILED: %d hit(s) in that function, want exactly 1 — a construction shape layer 2 misses, or one it counts twice. All hits: %v", fn, n, builtHits)
		}
	}

	// --- SQL sub-detector controls, both produced because ONE OF THEM WAS A
	// REAL FALSE POSITIVE during this writing (see w5eSQLCargoReads).
	//
	// (i) a whole-database DDL literal — declarations plus prose comments
	// containing ordinary English "on" — must produce NOTHING.
	schemaShaped := "package planted\n\nconst schema = `\n" +
		"CREATE TABLE IF NOT EXISTS trip (\n" +
		"    trip_id TEXT PRIMARY KEY,\n" +
		"    -- every structural exclusion keyed on destination_site IS NULL\n" +
		"    destination_site TEXT,\n" +
		"    cargo_note TEXT,\n" +
		"    recorded_by TEXT\n" +
		") STRICT;\n" +
		"CREATE TRIGGER IF NOT EXISTS t BEFORE UPDATE ON trip\n" +
		"BEGIN SELECT RAISE(ABORT, 'append-only'); END;\n" +
		"CREATE INDEX IF NOT EXISTS idx ON trip (origin_site) WHERE destination_site IS NOT NULL;\n" +
		"`\n"
	if h := w5eScanCargoBranches(t, "planted/schema.go", []byte(schemaShaped), w5ePlantConsts(t, []byte(schemaShaped))); len(h) != 0 {
		t.Fatalf("W5e-2 CONTROL (SQL negative) FAILED: the scan flagged a DECLARATION-ONLY schema literal: %v. This exact shape false-redded during this suite's writing — the `trip` CREATE lives inside one literal holding the whole database, whose prose comments contain the word \"on\", and a keyword-then-anywhere-after filter reads that as an ordering branch. A false RED here gets the detector weakened, and the weakening is what removes the tooth", h)
	}
	// (ii) a taxonomy hidden inside a CREATE VIEW must STILL be flagged —
	// dropping declarations must not become a hiding place.
	viewPlant := "package planted\n\nconst v = `CREATE VIEW hauling AS SELECT trip_id FROM trip WHERE cargo_note <> ''`\n"
	if h := w5eScanCargoBranches(t, "planted/view.go", []byte(viewPlant), w5ePlantConsts(t, []byte(viewPlant))); len(h) != 1 {
		t.Fatalf("W5e-2 CONTROL (SQL, CREATE VIEW) FAILED: a view classifying trips by what their note says produced %d hit(s), want 1 (%v) — that view IS the manifest system D-21.3 declined, and skipping declarations must not hide it", len(h), h)
	}

	// NEGATIVE control: the lawful uses must survive untouched.
	if hits := w5eScanCargoBranches(t, "planted/clean.go", []byte(w5eCleanPlant), w5ePlantConsts(t, []byte(w5eCleanPlant))); len(hits) != 0 {
		t.Fatalf("W5e-2 CONTROL (negative) FAILED: the scan flagged LAWFUL cargo handling — carriage, the nil/absent test, a len-against-zero comparison, or an INSERT/SELECT column list: %v. A filter that false-reds a correct build gets weakened, and the weakening is what removes the tooth", hits)
	}

	// --- layer 2 negatives (correction 4). Each is a LAWFUL shape the
	// built-stream layer now sees and must leave alone — a false RED here is
	// how a layer gets weakened or dropped (REVIEW-GUARD3B.md dropped one).
	//
	// (iii) the tip's own two const shapes, reached through functions. The
	// store schema — one whole-database DDL const handed to db.Exec in Open —
	// here carrying the ON-bearing declaration shapes: a REFERENCES … ON
	// DELETE and a NOT NULL … ON CONFLICT column clause AHEAD of cargo_note
	// inside the CREATE, the append-only trigger, the partial index. And
	// domain's composition chain — cargo_note in a SELECT list, the WHERE and
	// a cross-file ORDER BY const spliced AFTER it — plus an INSERT column
	// list and a SELECT list each continued by `+=`.
	orderByFile := []byte(`package planted

const orderBy = " ORDER BY site_id, node_epoch, seq"
`)
	spliceFile := []byte(`package planted

import "database/sql"

const schema = "CREATE TABLE IF NOT EXISTS trip (\n" +
	"    trip_id          TEXT PRIMARY KEY,\n" +
	"    origin_site      TEXT REFERENCES site(site_id) ON DELETE RESTRICT,\n" +
	"    vehicle_entered  TEXT NOT NULL ON CONFLICT ABORT,\n" +
	"    -- every structural exclusion keyed on destination_site IS NULL\n" +
	"    destination_site TEXT,\n" +
	"    cargo_note       TEXT,\n" +
	"    recorded_by      TEXT\n" +
	") STRICT;\n" +
	"CREATE TRIGGER IF NOT EXISTS t BEFORE UPDATE ON trip\n" +
	"BEGIN SELECT RAISE(ABORT, 'append-only'); END;\n" +
	"CREATE INDEX IF NOT EXISTS idx ON trip (origin_site) WHERE destination_site IS NOT NULL;\n"

const (
	compositionSelect = "SELECT site_id, node_epoch, seq, trip_id, cargo_note, cleared FROM trip_amendment"
	compositionByTrip = compositionSelect + " WHERE trip_id = ?" + orderBy
)

func open(db *sql.DB) error {
	_, err := db.Exec(schema)
	return err
}

func byTrip(db *sql.DB, id string) (*sql.Rows, error) {
	return db.Query(compositionByTrip, id)
}

func insertBuilt(db *sql.DB, note *string) {
	cols := "INSERT INTO trip (trip_id"
	cols += ", cargo_note"
	cols += ") VALUES (?, ?)"
	db.Exec(cols, "t1", note)
}

func selectBuilt(db *sql.DB) {
	q := "SELECT trip_id"
	q += ", cargo_note"
	q += " FROM trip WHERE origin_site = ?"
	db.Query(q, "A")
}
`)
	if hits := w5eScanCargoBranches(t, "planted/splice.go", spliceFile, w5ePlantConsts(t, orderByFile, spliceFile)); len(hits) != 0 {
		t.Fatalf("W5e-2 CONTROL (layer 2 negative, const splice) FAILED: the built-stream layer flagged LAWFUL SQL — a declaration-only schema handed to db.Exec, a SELECT-list carriage whose WHERE / ORDER BY are spliced after the column, or a column list continued by +=: %v. A false RED here is how a layer gets weakened", hits)
	}
	// (iv) keyword-less non-SQL strings naming the column — what a handler or
	// a fold writes. Layers 1 and 2 are silent on all of them; pinned so a
	// "bare predicate" layer cannot come back unnoticed (REVIEW-GUARD3B.md
	// measured twelve such strings redding under one).
	keywordless := []byte(`package planted

import (
	"errors"
	"fmt"
	"net/http"
)

const fieldCargoNote = "cargo_note"

func form(r *http.Request) string { return r.FormValue(fieldCargoNote) }

func reject(site string) error {
	return fmt.Errorf("%s: %s must be recorded at dispatch, not on arrival", site, fieldCargoNote)
}

var errLen = errors.New("cargo_note > 4096 bytes is not admissible")

const tripsByNote = "/trips?cargo_note=set&sort=cargo_note desc"
`)
	if hits := w5eScanCargoBranches(t, "planted/keywordless.go", keywordless, w5ePlantConsts(t, keywordless)); len(hits) != 0 {
		t.Fatalf("W5e-2 CONTROL (layer 2 negative, keyword-less strings) FAILED: a form field name, an error string with an operator, or a URL naming the column was flagged as a SQL read: %v. That is a bare-predicate layer with no notion of SQL-ness, the one REVIEW-GUARD3B.md removed", hits)
	}

	// Rule 6 — assert the ARRANGEMENT: alias tracking must actually be doing
	// work, or the "aliased form" plants above prove only what the direct
	// ones do. The aliased-only source below has NO direct selector inside
	// the branch itself.
	aliasOnly := `package planted

import "sitecontrol/internal/domain"

func onlyAliased(t *domain.TripDispatched) bool {
	first := *t.CargoNote
	second := first
	third := second
	return len(third) > 7
}
`
	if hits := w5eScanCargoBranches(t, "planted/aliasonly.go", []byte(aliasOnly), nil); len(hits) != 1 {
		t.Fatalf("W5e-2 CONTROL (alias fixpoint) FAILED: a three-hop single-assignment alias chain ending in a len() branch produced %d hit(s), want 1 — alias tracking is not resolving to a fixpoint and every 'aliased' plant above is really testing the direct form: %v", len(hits), hits)
	}
	reassigned := `package planted

import "sitecontrol/internal/domain"

func reassigned(t *domain.TripDispatched, fallback string) bool {
	cn := *t.CargoNote
	cn = fallback
	return cn == "anything"
}
`
	if hits := w5eScanCargoBranches(t, "planted/reassigned.go", []byte(reassigned), nil); len(hits) != 0 {
		t.Fatalf("W5e-2 CONTROL (alias soundness) FAILED: an identifier REASSIGNED away from the cargo note was still treated as cargo: %v — that is a false positive, and false positives are what get scans weakened", hits)
	}
}

// TestW5eNoProductionCodeBranchesOnCargoContent — W5e-2's scan half over the
// real tree. ⟦NO PRE-CHUNK RED⟧: this passes on arrival and its PASS IS NOT
// EVIDENCE. Read it beside TestW5eContentScanDetectsEveryPlantedShape.
func TestW5eNoProductionCodeBranchesOnCargoContent(t *testing.T) {
	files := w5eProductionGoFiles(t)
	// Rule 6 — the scan universe must be real. An empty or truncated walk is
	// the shape that made "0 hits where there were 48" a standing rule here.
	if len(files) < 20 {
		t.Fatalf("W5e-2 fixture defect: the production-Go walk found only %d file(s) — the scan universe is truncated and a clean result would mean nothing", len(files))
	}
	sawDomainTrip := false
	for _, f := range files {
		if strings.HasSuffix(f, filepath.Join("internal", "domain", "trip.go")) {
			sawDomainTrip = true
		}
	}
	if !sawDomainTrip {
		t.Fatalf("W5e-2 fixture defect: the walk did not include internal/domain/trip.go — the one file the cargo note is certain to live in")
	}

	// Layer 2 splices a fragment named by a package-level identifier from the
	// enclosing package's string-const table (correction 4) — one table per
	// package directory, built from that package's production files only.
	byDir := map[string][]string{}
	for _, path := range files {
		byDir[filepath.Dir(path)] = append(byDir[filepath.Dir(path)], path)
	}
	constsByDir := map[string]map[string][]w1SQLPiece{}
	for dir, pkgFiles := range byDir {
		constsByDir[dir] = w5eBuiltSQLConsts(w1PackageStringConsts(t, pkgFiles))
	}
	var found []w5eBranchHit
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		found = append(found, w5eScanCargoBranches(t, path, src, constsByDir[filepath.Dir(path)])...)
	}
	if len(found) != 0 {
		var lines []string
		for _, h := range found {
			lines = append(lines, h.String())
		}
		t.Fatalf("W5e-2 (m5e-2): production code BRANCHES ON CARGO CONTENT. Absent = not hauling: no flag, no taxonomy, no code path may read what the note SAYS — the note is a record for a human, and any classification derived from it is a manifest system nobody agreed to build (D-21.3: \"we won't have manifests anyway\"). Hits:\n%s", strings.Join(lines, "\n"))
	}
}

// w5eProductionGoFiles walks the module for every non-test .go file. Unlike
// W3's and W4's consumer scans this one does NOT exclude internal/domain:
// W5e-2 forbids the branch everywhere, and domain is where it would land.
func w5eProductionGoFiles(t *testing.T) []string {
	t.Helper()
	root := w1ModuleRoot(t)
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "node_modules" || (strings.HasPrefix(info.Name(), ".") && path != root) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(files)
	return files
}
