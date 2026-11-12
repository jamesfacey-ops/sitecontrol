// w5a_source_scans_test.go — W5a's SOURCE-LEVEL halves, all four of them:
//
//   - W5a-1 (scan half): nothing ever parses a name. AST walk over production
//     Go, four lettered shapes, each planted in DIRECT and
//     SINGLE-ASSIGNMENT-ALIASED form.
//   - W5a-8 (structural half): composition is one-way — no reader assigns to
//     LastName/FirstName/Position from Name.
//   - W5a-9 (scan half): `Position` is free text with no taxonomy — no enum,
//     no validation against a list, no branch on its content.
//   - W5-X1: every W5 schema change is declared inside a single
//     `CREATE `-containing Go string literal — no ALTER TABLE, no ADD COLUMN,
//     no DDL assembled by `+` concatenation.
//
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, criteria LOCKED 2026-08-31.
//
// package store_test, and in internal/store DELIBERATELY: these are
// file-system source scans referencing no unbuilt Go symbol, so they COMPILE
// AND RUN against `88dd6a6`, and the store suite runs in ~11s where
// internal/domain measures 205–572s. The scans do not read the store; they
// live here for speed and for company with W1's and W3's scans.
//
// ⚠⚠ ALL FOUR OF THESE CRITERIA ARE ALREADY TRUE AT 88dd6a6 — they are marked
// ⟦NO PRE-CHUNK RED⟧ in the lock and enumerated in its already-true table.
// **A CLEAN PASS HERE IS NOT EVIDENCE.** The evidence each scan produces is
// the PLANTED-VIOLATION CONTROL — run first, inside the same test, through
// the same detector the real pass uses — plus the named mutant. W5a-1's
// control is four lettered shapes × two forms = EIGHT plants, because "a
// narrow filter with one matching plant is a fully green ledger row proving
// nothing" is this criterion's own stated hazard.
//
// KILLER MUTANTS: (m5a-1) scan half · (m5a-8) · (m5a-3) shares W5a-8's kill ·
// (m5a-9) · (m5-X1).
package store_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// --- scan universe -----------------------------------------------------

// w5aModuleRoot ascends from the package directory to the go.mod directory.
func w5aModuleRoot(t *testing.T) string {
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
			t.Fatal("no go.mod above the package directory — scan root unresolvable")
		}
		dir = parent
	}
}

// w5aProductionGoFiles is the scan universe: every non-test .go file in the
// module. "Production Go" is W5a-1's own phrase; _test.go is excluded because
// a test may legitimately construct a split name as a fixture (this file
// does exactly that, eight times).
func w5aProductionGoFiles(t *testing.T) []string {
	t.Helper()
	root := w5aModuleRoot(t)
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
		t.Fatal(err)
	}
	sort.Strings(files)
	// Rule 6: an empty or truncated scan universe is a silently green scan.
	// The three files W5a is stated to edit (W5-X3's collision list) are
	// asserted present by name, so a walk that mis-roots reds loudly instead
	// of passing over nothing.
	if len(files) < 20 {
		t.Fatalf("fixture defect: the scan universe holds %d production Go files, which cannot be right for this module", len(files))
	}
	for _, want := range []string{
		filepath.Join("internal", "domain", "refdata.go"),
		filepath.Join("internal", "web", "intake.go"),
		filepath.Join("internal", "store", "store.go"),
	} {
		found := false
		for _, f := range files {
			if strings.HasSuffix(f, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("fixture defect: %s is absent from the scan universe — it is one of W5a's own edit sites (W5-X3's collision list), so a scan that cannot see it proves nothing about W5a", want)
		}
	}
	return files
}

// w5aHit is one scan finding.
type w5aHit struct {
	File   string
	Line   int
	Shape  string
	Detail string
}

func (h w5aHit) String() string {
	return fmt.Sprintf("%s:%d [%s] %s", h.File, h.Line, h.Shape, h.Detail)
}

func w5aFormat(hits []w5aHit) string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, "  "+h.String())
	}
	return strings.Join(out, "\n")
}

// w5aShapes returns the distinct shape labels in hits.
func w5aShapes(hits []w5aHit) []string {
	seen := map[string]bool{}
	var out []string
	for _, h := range hits {
		if !seen[h.Shape] {
			seen[h.Shape] = true
			out = append(out, h.Shape)
		}
	}
	sort.Strings(out)
	return out
}

// w5aParse parses one source file (or a planted synthetic one).
func w5aParse(t *testing.T, name string, src any) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return fset, f
}

// w5aScanAll runs one detector over the whole production universe.
func w5aScanAll(t *testing.T, detect func(*testing.T, string, any) []w5aHit) []w5aHit {
	t.Helper()
	var all []w5aHit
	for _, path := range w5aProductionGoFiles(t) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, detect(t, path, src)...)
	}
	return all
}

// =======================================================================
// W5a-1 — nothing ever parses a name
// =======================================================================

// w5aNameParsingCalls is shape (a): the exact verb set W5a-1 enumerates.
var w5aNameParsingCalls = map[string]bool{
	"Split": true, "SplitN": true, "Cut": true,
	"Fields": true, "Index": true, "LastIndex": true,
}

// w5aRegexpMethods is shape (d)'s operand set: the regexp methods that take a
// subject string. A `regexp` method call with a name as its operand is a name
// parse whatever it does with the result.
var w5aRegexpMethods = map[string]bool{
	"Split": true, "FindString": true, "FindStringIndex": true,
	"FindStringSubmatch": true, "FindAllString": true,
	"FindAllStringSubmatch": true, "FindAllStringIndex": true,
	"ReplaceAllString": true, "ReplaceAllStringFunc": true,
	"MatchString": true, "SubexpIndex": true,
}

// w5aPassThrough are the calls that carry a name through unchanged, so an
// expression wrapped in one is still name-bearing. Without these,
// `strings.Split(strings.TrimSpace(p.Name), " ")` escapes the filter.
var w5aPassThrough = map[string]bool{
	"TrimSpace": true, "ToLower": true, "ToUpper": true, "Clone": true,
	"Trim": true, "TrimLeft": true, "TrimRight": true, "TrimPrefix": true,
	"TrimSuffix": true, "collapseSpaces": true, "Title": true,
}

// w5aNameIdent decides whether a bare identifier or a selector's field name
// denotes a person display name or the `name` column.
//
// ⚠ THE FILTER IS DELIBERATELY A SUPERSET of "Person.Name". It admits any
// `.Name` / `.*Name` selector and any identifier spelled `name` or ending
// `Name`, so it also covers company, workgroup and vehicle names and the
// `name` column read out of SQL. A superset can only ever produce a FALSE
// RED — a real parse of some other name, reported — never a false green,
// which is the direction that matters for a criterion whose clean pass is
// not evidence. It is measured clean over the whole production tree at
// `88dd6a6`.
func w5aNameIdent(s string) bool {
	// The name PARTS are excluded, and this is a tightening TOWARD the
	// criterion rather than away from it: W5a-1 is about `Person.Name` and
	// the `name` column, and `p.LastName`/`p.FirstName` are separate facts
	// under D-48 — a part is not the composed name, so carrying one around
	// is not parsing one. Without the exclusion the filter reports
	// person_amend.go:631's `PersonAmended{… LastName: in.LastName …}` as a
	// reverse composition, which it plainly is not [MEASURED — it did].
	switch s {
	case "LastName", "FirstName", "lastName", "firstName", "last_name", "first_name":
		return false
	case "name", "Name", "Display":
		return true
	}
	return strings.HasSuffix(s, "Name") || strings.HasSuffix(s, "name")
}

// w5aNameScope tracks, within one function, the identifiers that alias a
// name-bearing expression through a SINGLE assignment hop (`n := p.Name`).
//
// ⚠ DEEPER DATAFLOW IS EXPLICITLY OUT OF SCOPE — the lock says so in terms
// ("a criteria lock does not commission a dataflow analyser"), and the
// behaviour half of W5a-1 carries what this cannot see.
type w5aNameScope struct{ alias map[string]bool }

func (s w5aNameScope) bearing(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return w5aNameIdent(x.Name) || s.alias[x.Name]
	case *ast.SelectorExpr:
		return w5aNameIdent(x.Sel.Name)
	case *ast.ParenExpr:
		return s.bearing(x.X)
	case *ast.StarExpr: // *p.LastName-style dereference of a *string name
		return s.bearing(x.X)
	case *ast.CallExpr:
		// A pass-through wrapper carries the name; conversions like
		// string(x) do too.
		switch fn := x.Fun.(type) {
		case *ast.SelectorExpr:
			if !w5aPassThrough[fn.Sel.Name] {
				return false
			}
		case *ast.Ident:
			if fn.Name != "string" && !w5aPassThrough[fn.Name] {
				return false
			}
		default:
			return false
		}
		for _, a := range x.Args {
			if s.bearing(a) {
				return true
			}
		}
	}
	return false
}

// bearingSubject is `bearing` narrowed for shapes (b) and (c), where the
// expression is the thing being INDEXED, SLICED or RANGED rather than an
// argument.
//
// ⚠ THE NARROWING IS MEASURED, NOT COSMETIC, and the first draft of this file
// did not have it. This codebase indexes maps NAMED after a name —
// `personName[v.PersonID]`, `companyName[c.CompanyID]`,
// `workgroupName[r.workgroupID]` — at 29 production sites (gate.go:331,
// intake.go:331, trips.go:300, muster.go:115 …). Those are containers keyed
// by an id, not names being taken apart, and the wide filter reported every
// one of them. A bare identifier merely ENDING in "Name" is therefore not a
// subject; a field selector (`p.Name`), a single-assignment alias, or an
// identifier spelled exactly `name` is. Argument positions — shapes (a) and
// (d) — keep the wide filter, where a map cannot appear because
// `strings.Split` would not compile against one.
func (s w5aNameScope) bearingSubject(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return s.alias[x.Name] || x.Name == "name" || x.Name == "Name" || x.Name == "Display"
	case *ast.SelectorExpr:
		return w5aNameIdent(x.Sel.Name)
	case *ast.ParenExpr:
		return s.bearingSubject(x.X)
	case *ast.StarExpr:
		return s.bearingSubject(x.X)
	case *ast.CallExpr:
		switch fn := x.Fun.(type) {
		case *ast.SelectorExpr:
			if !w5aPassThrough[fn.Sel.Name] {
				return false
			}
		case *ast.Ident:
			if fn.Name != "string" && !w5aPassThrough[fn.Name] {
				return false
			}
		default:
			return false
		}
		for _, a := range x.Args {
			if s.bearingSubject(a) {
				return true
			}
		}
	}
	return false
}

// w5aCollectAliases walks a function body for single-hop aliases of a
// name-bearing expression. An identifier assigned more than once anywhere in
// the function is NOT treated as an alias — one assignment, one hop.
func w5aCollectAliases(fn ast.Node) w5aNameScope {
	base := w5aNameScope{alias: map[string]bool{}}
	assignCount := map[string]int{}
	candidate := map[string]bool{}
	ast.Inspect(fn, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != len(as.Rhs) {
			return true
		}
		for i, lhs := range as.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok || id.Name == "_" {
				continue
			}
			assignCount[id.Name]++
			if base.bearing(as.Rhs[i]) {
				candidate[id.Name] = true
			}
		}
		return true
	})
	for name := range candidate {
		if assignCount[name] == 1 {
			base.alias[name] = true
		}
	}
	return base
}

// w5aDetectNameParsing is W5a-1's detector: the four lettered shapes, over
// name-bearing expressions, direct and single-assignment-aliased.
func w5aDetectNameParsing(t *testing.T, path string, src any) []w5aHit {
	t.Helper()
	fset, f := w5aParse(t, path, src)
	var hits []w5aHit
	add := func(pos token.Pos, shape, detail string) {
		hits = append(hits, w5aHit{File: path, Line: fset.Position(pos).Line, Shape: shape, Detail: detail})
	}
	// Aliases are collected per top-level declaration so one function's
	// `n := p.Name` does not leak into the next.
	for _, decl := range f.Decls {
		scope := w5aCollectAliases(decl)
		ast.Inspect(decl, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				sel, ok := x.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				// (a) strings.Split/SplitN/Cut/Fields/Index/LastIndex on it.
				if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "strings" && w5aNameParsingCalls[sel.Sel.Name] {
					for _, arg := range x.Args {
						if scope.bearing(arg) {
							add(x.Pos(), "a", fmt.Sprintf("strings.%s over a name-bearing expression", sel.Sel.Name))
							break
						}
					}
				}
				// (d) a regexp method call with it as the operand.
				if w5aRegexpMethods[sel.Sel.Name] {
					if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "strings" {
						return true // strings.Split is shape (a), already handled
					}
					for _, arg := range x.Args {
						if scope.bearing(arg) {
							add(x.Pos(), "d", fmt.Sprintf("regexp method %s over a name-bearing expression", sel.Sel.Name))
							break
						}
					}
				}
			case *ast.IndexExpr:
				// (b) an IndexExpr whose X is it.
				if scope.bearingSubject(x.X) {
					add(x.Pos(), "b", "index into a name-bearing expression")
				}
			case *ast.SliceExpr:
				// (b) a SliceExpr whose X is it.
				if scope.bearingSubject(x.X) {
					add(x.Pos(), "b", "slice of a name-bearing expression")
				}
			case *ast.RangeStmt:
				// (c) a RangeStmt whose X is it.
				if scope.bearingSubject(x.X) {
					add(x.Pos(), "c", "range over a name-bearing expression")
				}
			}
			return true
		})
	}
	return hits
}

// w5aNameParsePlants are W5a-1's CONTROLs: one per lettered shape, EACH in
// direct form AND in single-assignment-aliased form (⟦compl. R2-7⟧ — the
// four shapes as written are direct-form while the criterion says "derives
// from", so one assignment hop would survive all four plants).
var w5aNameParsePlants = []struct {
	shape  string
	form   string
	source string
}{
	{"a", "direct", `package p
import "strings"
type Person struct{ Name string }
func f(pp Person) string { return strings.Split(pp.Name, ", ")[0] }
`},
	{"a", "aliased", `package p
import "strings"
type Person struct{ Name string }
func f(pp Person) string { n := pp.Name; return strings.Split(n, ", ")[0] }
`},
	{"b", "direct", `package p
type Person struct{ Name string }
func f(pp Person) byte { return pp.Name[0] }
`},
	{"b", "aliased", `package p
type Person struct{ Name string }
func f(pp Person) string { n := pp.Name; return n[:3] }
`},
	{"c", "direct", `package p
type Person struct{ Name string }
func f(pp Person) int {
	c := 0
	for _, r := range pp.Name {
		if r == ',' { c++ }
	}
	return c
}
`},
	{"c", "aliased", `package p
type Person struct{ Name string }
func f(pp Person) int {
	n := pp.Name
	c := 0
	for range n { c++ }
	return c
}
`},
	{"d", "direct", `package p
import "regexp"
var surnameRe = regexp.MustCompile("^([^,]+),")
type Person struct{ Name string }
func f(pp Person) []string { return surnameRe.FindStringSubmatch(pp.Name) }
`},
	{"d", "aliased", `package p
import "regexp"
var surnameRe = regexp.MustCompile("^([^,]+),")
type Person struct{ Name string }
func f(pp Person) []string { n := pp.Name; return surnameRe.FindStringSubmatch(n) }
`},
}

// TestW5aNothingParsesAName — W5a-1's scan half.
//
// ⟦NO PRE-CHUNK RED⟧ — TRUE at 88dd6a6 ("No strings.Split on a name exists
// anywhere in the tree" [MEASURED, the lock's own two-pages-above
// measurement]). **The clean pass below is not evidence.** The evidence is
// the eight planted CONTROLs, each detected by the same detector, before the
// clean pass counts.
//
// EXPECTED RED (m5a-1): a strings.Split/SplitN/Cut on Person.Name feeding
// LastName/FirstName — flagged as shape (a), naming file and line.
func TestW5aNothingParsesAName(t *testing.T) {
	// --- CONTROL FIRST: four lettered shapes × two forms.
	for _, plant := range w5aNameParsePlants {
		name := fmt.Sprintf("planted/w5a_name_parse_%s_%s.go", plant.shape, plant.form)
		hits := w5aDetectNameParsing(t, name, plant.source)
		var got bool
		for _, h := range hits {
			if h.Shape == plant.shape {
				got = true
			}
		}
		if !got {
			t.Fatalf("W5a-1 CONTROL FAILED: shape (%s), %s form, was NOT detected (hits: %v). A narrow filter with one matching plant is a fully green ledger row proving nothing — this criterion is already true at 88dd6a6, so these plants ARE its evidence (m5a-1)", plant.shape, plant.form, hits)
		}
	}
	// The clean-file control: a detector that flags everything proves nothing
	// either.
	clean := `package p
import "strings"
type Person struct{ Name string }
func f(pp Person, typed string) bool { return strings.EqualFold(strings.TrimSpace(pp.Name), typed) }
func g(pp Person) string { return pp.Name }
`
	if hits := w5aDetectNameParsing(t, "planted/w5a_name_clean.go", clean); len(hits) != 0 {
		t.Fatalf("W5a-1 CONTROL: the detector flagged a clean file (an EqualFold/TrimSpace compare and a bare return of Name):\n%s", w5aFormat(hits))
	}

	// --- the real pass over production Go.
	if hits := w5aScanAll(t, w5aDetectNameParsing); len(hits) != 0 {
		t.Fatalf("W5a-1 (m5a-1): production Go splits, tokenises, indexes into or regexes a name — nothing may ever parse a name (D-48: Name is authoritative and the parts are separate facts, so a derived part is not evidence about anybody):\n%s", w5aFormat(hits))
	}
}

// =======================================================================
// W5a-8 (structural half) — composition is one-way
// =======================================================================

// w5aPartFields are the three name-part destinations.
var w5aPartFields = map[string]bool{"LastName": true, "FirstName": true, "Position": true}

// w5aCollectDerivedAliases is w5aCollectAliases widened for W5a-8: an
// identifier assigned exactly once from an expression that CONTAINS a
// name-bearing sub-expression is an alias. W5a-1 keeps the strict
// direct-RHS rule (its lock scopes it to "n := p.Name"); W5a-8 needs the
// wider one because the natural reverse-composition mutant is
// `last := strings.Split(p.Name, ", ")[0]; p.LastName = &last`, where the
// derivation runs through a call and an index before it reaches the part.
func w5aCollectDerivedAliases(fn ast.Node) w5aNameScope {
	base := w5aNameScope{alias: map[string]bool{}}
	assignCount := map[string]int{}
	// ⚠ ITERATED TO A FIXED POINT, bounded at w5aAliasRounds. A single pass
	// missed the real reverse-composition mutant, MEASURED: the planted
	// `n := p.Name; last := strings.SplitN(n, ", ", 2)[0]; p.LastName = &last`
	// SURVIVED, because `last`'s derivation runs through `n`, which the pass
	// had not yet recorded when it reached `last`. That chain is the natural
	// shape of the mutant, so the detector follows it. The bound keeps this
	// short of a dataflow analyser — which W5a-1's lock explicitly declines
	// to commission — and W5a-1's own filter keeps its ONE-hop scope.
	for round := 0; round < w5aAliasRounds; round++ {
		before := len(base.alias)
		assignCount = map[string]int{}
		candidate := map[string]bool{}
		ast.Inspect(fn, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok || len(as.Lhs) != len(as.Rhs) {
				return true
			}
			for i, lhs := range as.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok || id.Name == "_" {
					continue
				}
				assignCount[id.Name]++
				if w5aExprDerivesFromName(base, as.Rhs[i]) {
					candidate[id.Name] = true
				}
			}
			return true
		})
		for name := range candidate {
			if assignCount[name] == 1 {
				base.alias[name] = true
			}
		}
		if len(base.alias) == before {
			break
		}
	}
	return base
}

// w5aAliasRounds bounds the alias fixed point above.
const w5aAliasRounds = 5

// w5aDetectReverseComposition flags Name -> parts in three shapes:
//
//	(i)   an assignment whose LHS is `.LastName`/`.FirstName`/`.Position` and
//	      whose RHS derives from a name-bearing expression;
//	(ii)  a composite-literal field of one of those names, ditto;
//	(iii) a call taking `&x.LastName` (etc.) together with the "name" fact
//	      literal — the shape THIS codebase actually uses
//	      (person_amend.go:361's `set(&p.LastName, "last_name")`), where a
//	      one-word edit to `"name"` is a reverse composition that shapes (i)
//	      and (ii) cannot see.
func w5aDetectReverseComposition(t *testing.T, path string, src any) []w5aHit {
	t.Helper()
	fset, f := w5aParse(t, path, src)
	var hits []w5aHit
	add := func(pos token.Pos, shape, detail string) {
		hits = append(hits, w5aHit{File: path, Line: fset.Position(pos).Line, Shape: shape, Detail: detail})
	}
	partTarget := func(e ast.Expr) (string, bool) {
		if sel, ok := e.(*ast.SelectorExpr); ok && w5aPartFields[sel.Sel.Name] {
			return sel.Sel.Name, true
		}
		return "", false
	}
	for _, decl := range f.Decls {
		scope := w5aCollectDerivedAliases(decl)
		ast.Inspect(decl, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				for i, lhs := range x.Lhs {
					field, ok := partTarget(lhs)
					if !ok || i >= len(x.Rhs) {
						continue
					}
					if w5aExprDerivesFromName(scope, x.Rhs[i]) {
						add(x.Pos(), "i", "assignment to "+field+" from a name-bearing expression")
					}
				}
			case *ast.KeyValueExpr:
				if id, ok := x.Key.(*ast.Ident); ok && w5aPartFields[id.Name] {
					if w5aExprDerivesFromName(scope, x.Value) {
						add(x.Pos(), "ii", "composite-literal field "+id.Name+" set from a name-bearing expression")
					}
				}
			case *ast.CallExpr:
				var addressedPart string
				var namesTheNameFact bool
				for _, arg := range x.Args {
					if un, ok := arg.(*ast.UnaryExpr); ok && un.Op == token.AND {
						if field, ok := partTarget(un.X); ok {
							addressedPart = field
						}
					}
					if lit, ok := arg.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if v, err := strconv.Unquote(lit.Value); err == nil && v == "name" {
							namesTheNameFact = true
						}
					}
				}
				if addressedPart != "" && namesTheNameFact {
					add(x.Pos(), "iii", "a call fills "+addressedPart+" from the \"name\" fact")
				}
			}
			return true
		})
	}
	return hits
}

// w5aExprDerivesFromName reports whether any sub-expression of e is
// name-bearing — one hop, per W5a-1's stated scope bound.
func w5aExprDerivesFromName(scope w5aNameScope, e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if found {
			return false
		}
		if ex, ok := n.(ast.Expr); ok && scope.bearing(ex) {
			found = true
			return false
		}
		return true
	})
	return found
}

// TestW5aCompositionIsOneWayStructurally — W5a-8's structural half.
//
// ⟦NO PRE-CHUNK RED⟧ — the lock calls this "the sharpest already-true
// instance" and measures its truth itself ("composeName already exists and is
// already one-way"). **The clean pass is not evidence**; the planted reverse
// assignment is.
//
// EXPECTED RED (m5a-8): composeName made two-way, or an edge path assigning
// parts from Name. EXPECTED RED (m5a-3) shares this kill.
func TestW5aCompositionIsOneWayStructurally(t *testing.T) {
	// --- CONTROL FIRST: one plant per shape.
	plants := []struct{ shape, source string }{
		{"i", `package p
type Person struct{ Name string; LastName *string }
func f(pp *Person) { pp.LastName = &pp.Name }
`},
		{"i", `package p
import "strings"
type Person struct{ Name string; LastName *string }
func f(pp *Person) { last := strings.Split(pp.Name, ", ")[0]; pp.LastName = &last }
`},
		{"ii", `package p
type Amendment struct{ LastName *string }
type Person struct{ Name string }
func f(pp Person) Amendment { return Amendment{LastName: &pp.Name} }
`},
		{"iii", `package p
type Person struct{ LastName *string }
func set(dst **string, fact string) {}
func f(pp *Person) { set(&pp.LastName, "name") }
`},
	}
	for _, plant := range plants {
		hits := w5aDetectReverseComposition(t, "planted/w5a_reverse_"+plant.shape+".go", plant.source)
		var got bool
		for _, h := range hits {
			if h.Shape == plant.shape {
				got = true
			}
		}
		if !got {
			t.Fatalf("W5a-8 CONTROL FAILED: the planted reverse composition, shape (%s), was NOT detected (hits: %v) — this criterion is already true at 88dd6a6, so the plant IS its evidence (m5a-8)", plant.shape, hits)
		}
	}
	clean := `package p
type Person struct{ Name string; LastName *string }
func set(dst **string, fact string) {}
func f(pp *Person) { set(&pp.LastName, "last_name") }
func g(last, first string) string { return last + ", " + first }
`
	if hits := w5aDetectReverseComposition(t, "planted/w5a_reverse_clean.go", clean); len(hits) != 0 {
		t.Fatalf("W5a-8 CONTROL: the detector flagged a clean file (the codebase's own `set(&p.LastName, \"last_name\")` idiom and a forward composition):\n%s", w5aFormat(hits))
	}

	// --- the real pass.
	if hits := w5aScanAll(t, w5aDetectReverseComposition); len(hits) != 0 {
		t.Fatalf("W5a-8 (m5a-8/m5a-3): production Go derives a name PART from the composed Name. Composition is one-way: parts -> Name via composeName at the authoring edge, NEVER Name -> parts anywhere:\n%s", w5aFormat(hits))
	}
}

// =======================================================================
// W5a-9 (scan half) — Position has no taxonomy
// =======================================================================

// w5aDetectPositionTaxonomy flags a taxonomy imposed on `Position`:
//
//	(e) a comparison of a Position-bearing expression against anything that is
//	    not `nil` and not the empty string — a literal, a constant, or a loop
//	    variable over an allow-list (an enum test, however it is spelled);
//	(f) a switch whose tag is a Position-bearing expression;
//	(g) a membership test — slices.Contains / a map or slice index keyed on
//	    it / strings.Contains over it.
//
// ⚠ A comparison against the EMPTY string is deliberately NOT flagged, and
// this is a narrowing with a reason: `*p.Position == ""` is a PRESENCE test,
// indistinguishable in kind from the `p.Position != nil` test the nullable
// pointer already invites. A taxonomy needs a named value. Stated so a later
// reader does not mistake it for an oversight — see the report's limits
// section.
func w5aDetectPositionTaxonomy(t *testing.T, path string, src any) []w5aHit {
	t.Helper()
	fset, f := w5aParse(t, path, src)
	var hits []w5aHit
	add := func(pos token.Pos, shape, detail string) {
		hits = append(hits, w5aHit{File: path, Line: fset.Position(pos).Line, Shape: shape, Detail: detail})
	}
	bearing := func(e ast.Expr) bool { return w5aPositionBearing(e) }
	// A comparison operand that makes the comparison a PRESENCE test rather
	// than a taxonomy: `nil` (the pointer is absent) or the empty string.
	// Everything else — a literal, a constant, a loop variable over an
	// allow-list — is a named value, and comparing Position against a named
	// value is an enum test however it is spelled.
	//
	// ⚠ MEASURED CORRECTION: the first draft required a STRING LITERAL on the
	// other side, and the planted enum
	// `for _, a := range allowed { if position == a … }` SURVIVED it — the
	// commonest hand-rolled membership test in Go compares against a loop
	// variable, not a literal. The narrow rule made W5a-9's only evidence
	// miss its own named mutant.
	presenceOperand := func(e ast.Expr) bool {
		if id, ok := e.(*ast.Ident); ok && id.Name == "nil" {
			return true
		}
		lit, ok := e.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return false
		}
		v, err := strconv.Unquote(lit.Value)
		return err == nil && v == ""
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BinaryExpr:
			if x.Op != token.EQL && x.Op != token.NEQ {
				return true
			}
			if (bearing(x.X) && !presenceOperand(x.Y)) || (bearing(x.Y) && !presenceOperand(x.X)) {
				add(x.Pos(), "e", "Position compared against a named value — an enum test, however it is spelled")
			}
		case *ast.SwitchStmt:
			if x.Tag != nil && bearing(x.Tag) {
				add(x.Pos(), "f", "switch on Position's content")
			}
		case *ast.IndexExpr:
			if bearing(x.Index) {
				add(x.Pos(), "g", "Position used as a map/slice key — a membership test against an allowed list")
			}
		case *ast.CallExpr:
			sel, ok := x.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, _ := sel.X.(*ast.Ident)
			member := (pkg != nil && pkg.Name == "slices" && sel.Sel.Name == "Contains") ||
				(pkg != nil && pkg.Name == "strings" && (sel.Sel.Name == "Contains" || sel.Sel.Name == "EqualFold" || sel.Sel.Name == "HasPrefix" || sel.Sel.Name == "HasSuffix"))
			if !member {
				return true
			}
			for _, arg := range x.Args {
				if bearing(arg) {
					add(x.Pos(), "g", "membership/content test over Position via "+pkg.Name+"."+sel.Sel.Name)
					break
				}
			}
		}
		return true
	})
	return hits
}

// w5aPositionBearing recognises an expression denoting Position's VALUE.
// `p.Position` (the pointer) and `*p.Position` both count; the bare
// identifier `position` counts; a string literal "position" (a FACT NAME,
// which is what person_amend.go switches on) deliberately does not.
func w5aPositionBearing(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name == "position" || x.Name == "Position"
	case *ast.SelectorExpr:
		return x.Sel.Name == "Position"
	case *ast.ParenExpr:
		return w5aPositionBearing(x.X)
	case *ast.StarExpr:
		return w5aPositionBearing(x.X)
	case *ast.CallExpr:
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok && w5aPassThrough[sel.Sel.Name] {
			for _, a := range x.Args {
				if w5aPositionBearing(a) {
					return true
				}
			}
		}
	}
	return false
}

// TestW5aPositionIsFreeTextWithNoTaxonomy — W5a-9's scan half.
//
// ⟦NO PRE-CHUNK RED⟧ — `Position` has no consumer to branch on it at
// 88dd6a6. **The clean pass is not evidence**; the planted enum is.
// D-21.4: "nullable, free text".
//
// EXPECTED RED (m5a-9): Position validated against an enum, or a code path
// branching on its content.
func TestW5aPositionIsFreeTextWithNoTaxonomy(t *testing.T) {
	// --- CONTROL FIRST: one plant per shape, the (e) plant being the
	// literal "planted enum as the CONTROL" the criterion names.
	plants := []struct{ shape, source string }{
		{"e", `package p
type Person struct{ Position *string }
func valid(pp Person) bool {
	return pp.Position != nil && (*pp.Position == "Foreman" || *pp.Position == "Operator")
}
`},
		{"f", `package p
type Person struct{ Position *string }
func rank(pp Person) int {
	switch *pp.Position {
	case "Foreman":
		return 1
	}
	return 0
}
`},
		{"g", `package p
import "slices"
var allowed = []string{"Foreman", "Operator"}
func valid(position string) bool { return slices.Contains(allowed, position) }
`},
		// The HAND-ROLLED membership loop — the commonest Go spelling of an
		// enum test, and the one that survived the first draft of this
		// detector. It compares against a LOOP VARIABLE, not a literal.
		{"e", `package p
var allowed = []string{"Foreman", "Operator"}
func valid(position string) bool {
	for _, a := range allowed {
		if position == a {
			return true
		}
	}
	return false
}
`},
	}
	for _, plant := range plants {
		hits := w5aDetectPositionTaxonomy(t, "planted/w5a_position_"+plant.shape+".go", plant.source)
		var got bool
		for _, h := range hits {
			if h.Shape == plant.shape {
				got = true
			}
		}
		if !got {
			t.Fatalf("W5a-9 CONTROL FAILED: the planted taxonomy, shape (%s), was NOT detected (hits: %v) — this criterion is already true at 88dd6a6, so the plant IS its evidence (m5a-9)", plant.shape, hits)
		}
	}
	clean := `package p
type Person struct{ Position *string }
func show(pp Person) string {
	if pp.Position == nil {
		return ""
	}
	if *pp.Position == "" {
		return ""
	}
	return *pp.Position
}
func clearFact(pp *Person, fact string) {
	switch fact {
	case "position":
		pp.Position = nil
	}
}
`
	if hits := w5aDetectPositionTaxonomy(t, "planted/w5a_position_clean.go", clean); len(hits) != 0 {
		t.Fatalf("W5a-9 CONTROL: the detector flagged a clean file (a nil check, a bare read, and the codebase's own switch on the FACT NAME at person_amend.go:386):\n%s", w5aFormat(hits))
	}

	// --- the real pass.
	if hits := w5aScanAll(t, w5aDetectPositionTaxonomy); len(hits) != 0 {
		t.Fatalf("W5a-9 (m5a-9): production Go imposes a taxonomy on Position. D-21.4 rules it \"nullable, free text\" — no enum, no validation against a list, no branch on its content:\n%s", w5aFormat(hits))
	}
}

// =======================================================================
// W5-X1 — every schema change inside one CREATE-containing literal
// =======================================================================

// w5aDetectLooseDDL flags:
//
//	(h) a string literal containing ALTER TABLE or ADD COLUMN;
//	(i) DDL assembled by `+` concatenation — a `+` expression one of whose
//	    operands is a string literal containing CREATE / ALTER TABLE /
//	    ADD COLUMN.
func w5aDetectLooseDDL(t *testing.T, path string, src any) []w5aHit {
	t.Helper()
	fset, f := w5aParse(t, path, src)
	var hits []w5aHit
	add := func(pos token.Pos, shape, detail string) {
		hits = append(hits, w5aHit{File: path, Line: fset.Position(pos).Line, Shape: shape, Detail: detail})
	}
	ddlText := func(e ast.Expr) (string, bool) {
		lit, ok := e.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return "", false
		}
		v, err := strconv.Unquote(lit.Value)
		if err != nil {
			// A raw backquoted literal Unquote handles; anything else is
			// not our business.
			return "", false
		}
		return v, true
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BasicLit:
			v, ok := ddlText(x)
			if !ok {
				return true
			}
			up := strings.ToUpper(v)
			if strings.Contains(up, "ALTER TABLE") {
				add(x.Pos(), "h", "string literal contains ALTER TABLE")
			}
			if strings.Contains(up, "ADD COLUMN") {
				add(x.Pos(), "h", "string literal contains ADD COLUMN")
			}
		case *ast.BinaryExpr:
			if x.Op != token.ADD {
				return true
			}
			for _, operand := range []ast.Expr{x.X, x.Y} {
				v, ok := ddlText(operand)
				if !ok {
					continue
				}
				up := strings.ToUpper(v)
				if strings.Contains(up, "CREATE ") || strings.Contains(up, "ALTER TABLE") || strings.Contains(up, "ADD COLUMN") {
					add(x.Pos(), "i", "DDL assembled by + concatenation")
					break
				}
			}
		}
		return true
	})
	return hits
}

// TestW5aSchemaChangesStayInsideOneCreateLiteral — W5-X1, W5a's control.
//
// ⟦NO PRE-CHUNK RED⟧ — ALTER TABLE / ADD COLUMN are ZERO non-test hits at
// 88dd6a6 [MEASURED, re-run here]. **The clean pass is not evidence**; the
// two plants are. W5-X1's own text requires "one control per sub-chunk that
// touches schema" — this file is W5a's.
//
// EXPECTED RED (m5-X1): a projection column added by ALTER TABLE / ADD
// COLUMN, or a DDL string built by concatenating a column list onto a prefix,
// which is exactly the reach W4's narrowed guard-3a literal filter cannot see.
func TestW5aSchemaChangesStayInsideOneCreateLiteral(t *testing.T) {
	// --- CONTROL FIRST: both plants.
	altered := `package p
const migrate = "ALTER TABLE person ADD COLUMN last_name TEXT"
`
	hits := w5aDetectLooseDDL(t, "planted/w5a_ddl_alter.go", altered)
	if shapes := w5aShapes(hits); len(shapes) != 1 || shapes[0] != "h" {
		t.Fatalf("W5-X1 CONTROL FAILED: the planted ALTER TABLE … ADD COLUMN was not detected as shape (h) (hits: %v) (m5-X1)", hits)
	}
	concatenated := `package p
var cols = "last_name TEXT, first_name TEXT"
var schema = "CREATE TABLE IF NOT EXISTS person (person_id TEXT PRIMARY KEY, " + cols + ") STRICT;"
`
	hits = w5aDetectLooseDDL(t, "planted/w5a_ddl_concat.go", concatenated)
	if shapes := w5aShapes(hits); len(shapes) != 1 || shapes[0] != "i" {
		t.Fatalf("W5-X1 CONTROL FAILED: the planted +-concatenated DDL was not detected as shape (i) (hits: %v) (m5-X1)", hits)
	}
	clean := `package p
const schema = ` + "`" + `CREATE TABLE IF NOT EXISTS person (
	person_id TEXT PRIMARY KEY,
	last_name TEXT
) STRICT;` + "`" + `
var msg = "person " + "created"
`
	if hits := w5aDetectLooseDDL(t, "planted/w5a_ddl_clean.go", clean); len(hits) != 0 {
		t.Fatalf("W5-X1 CONTROL: the detector flagged a clean file (a single CREATE literal and an unrelated string concatenation):\n%s", w5aFormat(hits))
	}

	// --- the real pass.
	if hits := w5aScanAll(t, w5aDetectLooseDDL); len(hits) != 0 {
		t.Fatalf("W5-X1 (m5-X1): a schema change in W5 is declared outside a single CREATE-containing Go string literal. W4 narrowed W1's guard 3a to scan only literals containing \"CREATE \", so DDL outside such a literal is invisible to that guard — and every W5 sub-chunk adds nullable projection columns, which is exactly the moment somebody reaches for ALTER TABLE:\n%s", w5aFormat(hits))
	}
}
