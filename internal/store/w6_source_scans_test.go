// w6_source_scans_test.go — W6's scan- and structure-shaped criteria:
//
//	W6-1(a) limb (i)   the go/types-resolved, JOINED-value `INSERT INTO visit`
//	                   census
//	W6-1(a) limb (ii)  the AST census of ArrivalVisit CONSTRUCTIONS, asserted
//	                   over the ENCLOSING FUNCTION BODY
//	W6-X1              every schema change inside a single `CREATE `-containing
//	                   literal, walked from internal/ and cmd/
//	W6-8 structural    buildMuster's open-visit filter unchanged by W6's diff
//	W6-10(i)/(ii)      no merge resolution on the person key, in either closure
//
// ⚠ THE SCAN'S FILTER IS SPECIFIED, NOT ITS AMBITION. This project has found
// "a scan whose filter is narrower than its subject" SIX times. Two of those
// findings are inside W6's own criteria (W6-1(a) limb (i), W6-9(c)), and both
// are honoured here rather than re-decided:
//
//   - limb (ii) CANNOT be a grep: a string filter cannot see an ArrivalVisit
//     value assembled field by field.
//   - limb (i) has the same defect ONE LEVEL DOWN: an AST walk resolves a Go
//     TYPE for ArrivalVisit, but `INSERT INTO visit` is a STRING, and a
//     `+`-concatenated or fmt.Sprintf-assembled INSERT is as invisible to a
//     literal walk as to a grep. So limb (i) resolves string constants
//     through go/types and scans the JOINED value — the in-tree technique
//     TestW5dOneOrderBySpellingInCompositionByResolvedValue established.
//
// [DOC] person_amend.go:277: "A scan's comment is not evidence of its scope;
// read the filter."
//
// ⚠ rule 20: `go test -overlay` does NOT reach a source scan. Every control
// below is a REAL on-disk scratch tree written by the test itself and handed
// to the SAME walk the clean pass uses — never a hand-fed detector call,
// which exercises the one layer the scoping does not change.
//
// EXPECTED RED at f33c4c7:
//   - TestW6EveryVisitAuthoringSiteCarriesTheSnapshot: BOTH limbs. The one
//     production `INSERT INTO visit` (trip.go:430) is TEN columns and names
//     no snapshot; the one production ArrivalVisit construction
//     (tripauthor.go:411) sets no PersonName.
//   - the other three tests PASS. They are ⟦NO PRE-CHUNK RED⟧ (plan §2.5
//     rows 1, 6, 8, 9) and their evidence is their plants.
package store_test

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/importer"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// --- the walk's ROOT, named because revision 1 left it unspecified -------
//
// W6-X1's walk is rooted at `internal/` and `cmd/`, production `.go` only
// (_test.go excluded) — exactly the scope §2.5 row 9's zero-construct
// baseline was measured over. A walk scoped to internal/store satisfies
// every control and is blind to nine files, which is the narrow-filter class
// again.
var w6WalkRoots = []string{"internal", "cmd"}

func w6ModuleRoot(t *testing.T) string {
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

// w6ProductionGoFiles walks the named roots and returns every production
// .go file under them. `_test.go` is excluded; nothing else is.
func w6ProductionGoFiles(t *testing.T, base string, roots []string) []string {
	t.Helper()
	var out []string
	for _, r := range roots {
		dir := filepath.Join(base, r)
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			out = append(out, path)
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", dir, err)
		}
	}
	sort.Strings(out)
	return out
}

// w6PlantTree writes named sources into a fresh scratch tree, preserving the
// relative directories, and returns its root. The scan is then run over the
// ROOT — the control is never handed the planted file's name.
func w6PlantTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, src := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func w6Src(t *testing.T, fset *token.FileSet, n ast.Node) string {
	t.Helper()
	var b strings.Builder
	if err := printer.Fprint(&b, fset, n); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// --- W6-1(a) limb (i): the JOINED-value `INSERT INTO visit` census ------

// w6InsertSite is one enumerated `INSERT INTO visit` statement.
type w6InsertSite struct {
	Where string
	SQL   string
}

// w6VisitInserts enumerates every `INSERT INTO visit` in one directory,
// over THREE shapes, because a string filter sees only the first:
//
//  1. a single string literal
//  2. a `+` concatenation of literals (the JOINED value)
//  3. a package-level string constant whose RESOLVED value is the joined SQL
//
// Shape 3 is the go/types half and is what makes the census immune to WHERE
// the fragments are declared.
func w6VisitInserts(t *testing.T, dir string) ([]w6InsertSite, s56Set) {
	t.Helper()
	// ⟦LOCK — §56(1), 2026-09-03⟧ the diagnostics this census's type-check
	// raises, returned so a caller can tell a DEAD PLANT from a blind census.
	dg := s56Diags{Where: dir}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, s56Set{dg}
	}
	fset := token.NewFileSet()
	var files []*ast.File
	var unreadable []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			// ⚠ REPORTED as a site the census could not read, never silently
			// dropped: an unscanned file is indistinguishable from a clean
			// one, and that is how a census goes green over a plant.
			unreadable = append(unreadable, filepath.Join(dir, e.Name()))
			continue
		}
		files = append(files, f)
	}
	if len(files) == 0 && len(unreadable) == 0 {
		return nil, s56Set{dg}
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}}
	// ⟦LOCK — §56(1), 2026-09-03⟧ RECORDED, never discarded. ⟦was:
	// "conf := types.Config{Error: func(error) {}, Importer: nil, DisableUnusedImportCheck: true, IgnoreFuncBodies: true}"⟧
	// Shape 3 below is the RESOLVED package-level constant; a plant that does
	// not type-check contributes none, and the census then reports zero sites
	// — byte-identical to what a blind census reports.
	conf := types.Config{Error: dg.Record, Importer: nil, DisableUnusedImportCheck: true, IgnoreFuncBodies: true}
	_, _ = conf.Check("w6scan", fset, files, info)

	// Keyed by file:LINE, keeping the LONGEST resolved value: the same
	// statement is reachable as a resolved constant (the full joined SQL) and
	// as the BinaryExpr under it (literal fragments only), and booking one
	// statement twice would let a census "find" a site it cannot read.
	best := map[string]w6InsertSite{}
	add := func(pos token.Pos, sql string) {
		if !w6IsVisitInsert(sql) {
			return
		}
		p := fset.Position(pos)
		key := fmt.Sprintf("%s:%d", p.Filename, p.Line)
		if prev, ok := best[key]; ok && len(prev.SQL) >= len(sql) {
			return
		}
		best[key] = w6InsertSite{Where: key, SQL: sql}
	}
	// shape 3 — resolved package-level string constants.
	for id, obj := range info.Defs {
		c, ok := obj.(*types.Const)
		if !ok {
			continue
		}
		if v := c.Val(); v != nil && v.Kind() == constant.String {
			add(id.Pos(), constant.StringVal(v))
		}
	}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch e := n.(type) {
			case *ast.BinaryExpr: // shape 2 — the JOINED value
				if e.Op != token.ADD {
					return true
				}
				var joined strings.Builder
				ast.Inspect(e, func(m ast.Node) bool {
					if lit, ok := m.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						joined.WriteString(w6Unquote(lit.Value))
					}
					return true
				})
				add(e.Pos(), joined.String())
			case *ast.BasicLit: // shape 1 — a single literal
				if e.Kind == token.STRING {
					add(e.Pos(), w6Unquote(e.Value))
				}
			}
			return true
		})
	}
	var out []w6InsertSite
	for _, u := range unreadable {
		out = append(out, w6InsertSite{Where: u, SQL: "UNPARSEABLE — this file was NOT scanned"})
	}
	for _, v := range best {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Where < out[j].Where })
	return out, s56Set{dg}
}

func w6Unquote(s string) string {
	if len(s) >= 2 && (s[0] == '`' || s[0] == '"') {
		s = s[1 : len(s)-1]
	}
	return s
}

// w6IsVisitInsert matches `INSERT INTO visit` and NOT `INSERT INTO visit_x`.
func w6IsVisitInsert(sql string) bool {
	up := strings.ToUpper(sql)
	i := strings.Index(up, "INSERT")
	for i >= 0 {
		rest := strings.Fields(strings.ReplaceAll(up[i:], "(", " ( "))
		if len(rest) >= 3 && rest[0] == "INSERT" && rest[1] == "INTO" && rest[2] == "VISIT" {
			return true
		}
		next := strings.Index(up[i+1:], "INSERT")
		if next < 0 {
			return false
		}
		i = i + 1 + next
	}
	return false
}

// --- W6-1(a) limb (ii): the ArrivalVisit CONSTRUCTION census ------------
//
// ⚠ THE ASSERTION IS TAKEN OVER THE ENCLOSING FUNCTION BODY, NOT THE
// LITERAL'S ELEMENT LIST. The sole production composite literal is
// `v := ArrivalVisit{VisitID: …, PersonID: id}` — TWO fields; every other
// field is a LATER ASSIGNMENT, and this lock's own stated natural
// implementation (`v.PersonName = &p.Name`) is another one. A walk that
// asserted "carries the snapshot in its element list" would be RED against
// the build this lock intends, and the escapes from that are to prescribe
// the build, to widen the walk, or to RELAX the assertion — OW-1's recorded
// consequence.
type w6ConstructionSite struct {
	Where string
	Func  string
	Set   bool
	How   string
}

const w6ArrivalVisitType = "ArrivalVisit"

// w6ArrivalVisitSites enumerates every production construction of an
// ArrivalVisit value in one directory — composite literal OR declared
// variable — and reports, per site, whether the snapshot is set anywhere in
// the enclosing function body before the value escapes: literal element,
// later assignment, or a call to a same-package helper that assigns it.
func w6ArrivalVisitSites(t *testing.T, dir string) []w6ConstructionSite {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	fset := token.NewFileSet()
	var files []*ast.File
	var unreadable []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			// ⚠ REPORTED, never silently dropped — see w6VisitInserts.
			unreadable = append(unreadable, filepath.Join(dir, e.Name()))
			continue
		}
		files = append(files, f)
	}

	// Pass 1: which same-package functions ASSIGN the snapshot? A
	// construction site whose enclosing function hands the value to one of
	// these satisfies "set before the value escapes".
	setters := map[string]bool{}
	for _, f := range files {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if w6BodyAssignsSnapshot(fn.Body) {
				setters[fn.Name.Name] = true
			}
		}
	}

	var out []w6ConstructionSite
	for _, f := range files {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			assigns := w6BodyAssignsSnapshot(fn.Body)
			var callsSetter string
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := ""
				switch c := call.Fun.(type) {
				case *ast.Ident:
					name = c.Name
				case *ast.SelectorExpr:
					name = c.Sel.Name
				}
				if setters[name] && name != fn.Name.Name {
					callsSetter = name
				}
				return true
			})
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				where, isSite := "", false
				switch e := n.(type) {
				case *ast.CompositeLit:
					if w6TypeNameOf(e.Type) == w6ArrivalVisitType {
						where, isSite = fset.Position(e.Pos()).String(), true
						// A literal element counts on its own.
						for _, el := range e.Elts {
							if kv, ok := el.(*ast.KeyValueExpr); ok {
								if id, ok := kv.Key.(*ast.Ident); ok && id.Name == w6PayloadGoName {
									out = append(out, w6ConstructionSite{where, fn.Name.Name, true, "literal element"})
									return true
								}
							}
						}
					}
				case *ast.ValueSpec: // `var av ArrivalVisit`
					if w6TypeNameOf(e.Type) == w6ArrivalVisitType {
						where, isSite = fset.Position(e.Pos()).String(), true
					}
				}
				if !isSite {
					return true
				}
				switch {
				case assigns:
					out = append(out, w6ConstructionSite{where, fn.Name.Name, true, "later assignment in the enclosing function"})
				case callsSetter != "":
					out = append(out, w6ConstructionSite{where, fn.Name.Name, true, "helper " + callsSetter + "()"})
				default:
					out = append(out, w6ConstructionSite{where, fn.Name.Name, false, ""})
				}
				return true
			})
		}
	}
	for _, u := range unreadable {
		out = append(out, w6ConstructionSite{Where: u, Func: "UNPARSEABLE — this file was NOT scanned", Set: false})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Where < out[j].Where })
	return out
}

// w6BodyAssignsSnapshot reports whether a function body assigns `.PersonName`
// anywhere — the "later assignment" form.
func w6BodyAssignsSnapshot(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range as.Lhs {
			if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == w6PayloadGoName {
				found = true
			}
		}
		return true
	})
	return found
}

// w6TypeNameOf resolves `T`, `domain.T`, `*T` and `[]T` to "T".
func w6TypeNameOf(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.StarExpr:
		return w6TypeNameOf(t.X)
	case *ast.ArrayType:
		return w6TypeNameOf(t.Elt)
	}
	return ""
}

// TestW6EveryVisitAuthoringSiteCarriesTheSnapshot — W6-1(a), both limbs.
//
// ⚠ THE LIST IS AN INDEX, NOT THE CLAIM. The census is DERIVED AT TEST TIME
// and asserts no count: a site found during the build and absent from the
// plan's measured enumeration is a gap in the lock, never a pass. What is
// asserted is the PROPERTY — every enumerated site carries the snapshot.
//
// THREE PLANTS, not two (W6-X6(a)): a composite literal; a
// `var av ArrivalVisit` field-by-field form; and a `+`-concatenated
// `INSERT INTO visit`.
//
// EXPECTED RED at f33c4c7: both limbs. Limb (i) — the one production INSERT
// (trip.go:430) is ten columns. Limb (ii) — the one production construction
// (tripauthor.go:411) never sets PersonName.
// EXPECTED RED (m6-2): a second visit-authoring path that omits the snapshot.
func TestW6EveryVisitAuthoringSiteCarriesTheSnapshot(t *testing.T) {
	// --- CONTROL, first, through the SAME walk and filter ---------------
	plantRoot := w6PlantTree(t, map[string]string{
		// PLANT 1 — a `+`-CONCATENATED INSERT INTO visit, invisible to a grep
		// and to a literal walk alike.
		"planted/concat.go": `package planted

const visitCols = "(visit_id, site_id, person_id, is_open)"

const insertVisit = "INSERT INTO visit " + visitCols + " VALUES (?, ?, ?, 1)"
`,
		// PLANT 2 — a composite literal with no snapshot.
		// PLANT 3 — a field-by-field assembled ` + "`var av ArrivalVisit`" + ` form.
		"planted/second_path.go": `package planted

type ArrivalVisit struct {
	VisitID  string
	PersonID string
}

func secondAuthoringPathLiteral(id string) ArrivalVisit {
	return ArrivalVisit{VisitID: "v", PersonID: id}
}

func secondAuthoringPathAssembled(id string) ArrivalVisit {
	var av ArrivalVisit
	av.VisitID = "v"
	av.PersonID = id
	return av
}
`,
	})
	plantDir := filepath.Join(plantRoot, "planted")

	inserts, insertDiags := w6VisitInserts(t, plantDir)
	// ⟦LOCK — §56(1)⟧ ASKED BEFORE THE SITES ARE COUNTED.
	if !s56PlantTypeChecked(t, "W6-1(a) limb (i) control: the joined-value INSERT plant", insertDiags) {
		return
	}
	if len(inserts) != 1 {
		t.Fatalf("W6-1(a) limb (i) CONTROL FAILED: the JOINED-value census found %d `INSERT INTO visit` sites in "+
			"the planted tree, want exactly 1 (the `+`-concatenated one).\n"+
			"  ⚠ THIS IS THE NARROW-FILTER CLASS, found SIX times on this project: `INSERT INTO visit` is a "+
			"STRING, and a `+`-concatenated or fmt.Sprintf-assembled INSERT is as invisible to a literal walk as "+
			"to a grep. A census that cannot see this plant proves nothing about (m6-2).\n  found: %+v", len(inserts), inserts)
	}
	if strings.Contains(inserts[0].SQL, w6Column) {
		t.Fatalf("W6-1(a) limb (i) CONTROL FAILED: the planted INSERT was reported as CARRYING the snapshot")
	}

	sites := w6ArrivalVisitSites(t, plantDir)
	if len(sites) != 2 {
		t.Fatalf("W6-1(a) limb (ii) CONTROL FAILED: the construction census found %d ArrivalVisit sites in the "+
			"planted tree, want 2 — a composite literal AND a `var av ArrivalVisit` field-by-field form.\n"+
			"  ⚠ Limb (ii) CANNOT be a grep: a string filter cannot see a value assembled field by field.\n  found: %+v",
			len(sites), sites)
	}
	for _, s := range sites {
		if s.Set {
			t.Fatalf("W6-1(a) limb (ii) CONTROL FAILED: planted site %s (%s) was reported as CARRYING the snapshot (%s) "+
				"— a detector that cannot see its own plant proves nothing (m6-2)", s.Where, s.Func, s.How)
		}
	}
	// The censuses must NOT flag a clean site either, or their clean pass is
	// as meaningless as their red.
	cleanDir := filepath.Join(w6PlantTree(t, map[string]string{
		"clean/ok.go": `package clean

type ArrivalVisit struct {
	VisitID    string
	PersonID   string
	PersonName *string
}

func authorLiteral(id string, name *string) ArrivalVisit {
	return ArrivalVisit{VisitID: "v", PersonID: id, PersonName: name}
}

func authorLater(id string, name *string) ArrivalVisit {
	v := ArrivalVisit{VisitID: "v", PersonID: id}
	v.PersonName = name
	return v
}

func setSnapshot(v *ArrivalVisit, name *string) { v.PersonName = name }

func authorViaHelper(id string, name *string) ArrivalVisit {
	var av ArrivalVisit
	av.PersonID = id
	setSnapshot(&av, name)
	return av
}
`,
	}), "clean")
	for _, s := range w6ArrivalVisitSites(t, cleanDir) {
		if !s.Set {
			t.Fatalf("W6-1(a) limb (ii) CONTROL FAILED (negative arm): a CLEAN construction at %s (%s) was reported "+
				"as missing the snapshot. The assertion is over the ENCLOSING FUNCTION BODY — literal element, "+
				"later assignment, or a helper call taking a pointer to it — and a detector that only accepts the "+
				"literal-element form is RED against the build this lock intends", s.Where, s.Func)
		}
	}

	// --- the real census -------------------------------------------------
	root := w6ModuleRoot(t)
	var allInserts []w6InsertSite
	var allSites []w6ConstructionSite
	dirs := map[string]bool{}
	for _, f := range w6ProductionGoFiles(t, root, w6WalkRoots) {
		dirs[filepath.Dir(f)] = true
	}
	var dirList []string
	for d := range dirs {
		dirList = append(dirList, d)
	}
	sort.Strings(dirList)
	for _, d := range dirList {
		ins, dg := w6VisitInserts(t, d)
		allInserts = append(allInserts, ins...)
		insertDiags = append(insertDiags, dg...)
		allSites = append(allSites, w6ArrivalVisitSites(t, d)...)
	}

	s56LogTree(t, "w6VisitInserts", insertDiags)

	// Rule 6 — the arrangement this census needs must actually hold. A
	// census that enumerates NOTHING passes vacuously, which is exactly how
	// a narrowed walk goes green.
	if len(allInserts) == 0 {
		t.Fatalf("PRECONDITION: the `INSERT INTO visit` census enumerated ZERO sites over %v. At f33c4c7 there is "+
			"exactly one (trip.go:430). A census that enumerates nothing passes vacuously — this is the walk, "+
			"not the build\n"+
			"⟦§56⟧ what the census type-checked, so a ZERO is never confused with a tree that did not compile:\n%s",
			w6WalkRoots, insertDiags.Report())
	}
	if len(allSites) == 0 {
		t.Fatalf("PRECONDITION: the ArrivalVisit construction census enumerated ZERO sites over %v. At f33c4c7 "+
			"there is exactly one (tripauthor.go:411)", w6WalkRoots)
	}

	for _, s := range allInserts {
		if !strings.Contains(s.SQL, w6Column) {
			t.Errorf("W6-1(a) limb (i) (m6-2, m6-1b): the `INSERT INTO visit` at %s does not write `%s`.\n"+
				"  Every code path that creates a `visit` row must populate the snapshot from the roster row it "+
				"already resolved. The statement must be ELEVEN columns.\n  SQL: %s",
				s.Where, w6Column, strings.Join(strings.Fields(s.SQL), " "))
		}
	}
	for _, s := range allSites {
		if !s.Set {
			t.Errorf("W6-1(a) limb (ii) (m6-2, m6-1b): the ArrivalVisit constructed at %s (in %s) never sets "+
				"%s anywhere in its enclosing function body.\n"+
				"  The assertion is over the enclosing function — literal element, later assignment, or a helper "+
				"call taking a pointer to it — so the build shape is NOT prescribed.",
				s.Where, s.Func, w6PayloadGoName)
		}
	}
}

// --- W6-X1: every schema change inside one CREATE-containing literal ----
//
// Why W6 must adopt it, and it is the highest-value adoption on the list:
// W1's guard 3a admits a literal only if it contains `CREATE `
// (w1_eventtime_gone_test.go:223, :389), so an `ALTER TABLE` literal is
// INVISIBLE to it. W5's cover lapsed with W5. W6 ADDS A COLUMN. Without
// W6-X1, W6 is the first chunk in the window to add a column with no guard.
//
// EXPECTED RED at f33c4c7: NONE — `grep -arn "ALTER TABLE\|ADD COLUMN"` over
// production Go returns ONE hit and it is a COMMENT (refdata.go:307), so
// ZERO constructs (plan §2.5 row 9). Its clean pass is NOT evidence; its
// CONTROL arms are — EIGHT of them, four positive and four negative.
// ⟦LOCK — H-3, post-lock amendment 2026-09-02. The count is stated because
// it had already drifted: the text said "three CONTROL arms" while five were
// written, and the lock's own W6-X6 entry says "three arms (two positive
// plants and the negative arm)". Arms 6-8 are H-3's.
// ⟦was: "its three CONTROL arms are."⟧⟧
func TestW6SchemaIsDeclaredInOneCreateLiteral(t *testing.T) {
	// --- CONTROL: eight arms, and at least ONE POSITIVE PLANT OUTSIDE the
	// package W6 edits — otherwise the controls prove only that the walk
	// reaches internal/store, which is the narrow-filter class again.
	// ⟦was: "CONTROL: three arms, and at least ONE POSITIVE PLANT OUTSIDE"⟧
	plantRoot := w6PlantTree(t, map[string]string{
		// arm 1 — ALTER TABLE … ADD COLUMN, planted in internal/DOMAIN.
		"internal/domain/planted_alter.go": `package planted

const migrate = "ALTER TABLE visit ADD COLUMN person_snapshot_name TEXT"
`,
		// arm 2 — DDL assembled by ` + "`+`" + ` concatenation, planted in CMD.
		"cmd/sitectl/planted_concat.go": `package planted

const cols = "visit_id TEXT PRIMARY KEY, person_snapshot_name TEXT"

const schema = "CREATE TABLE IF NOT EXISTS visit (" + cols + ") STRICT;"
`,
		// arm 3 (NEGATIVE) — a plain `+`-concatenated SELECT, in internal/store,
		// which must NOT be flagged. A list broad enough to fire on true code
		// is a list a builder will weaken (OW-1's recorded consequence).
		"internal/store/planted_clean.go": `package planted

const q = "SELECT person_snapshot_name" + " FROM visit WHERE is_open = 1"
`,
		// arm 4 — DDL assembled from TWO NAMED CONSTANTS, planted in a THIRD
		// package (internal/WEB). ⚠ THIS IS THE ARM THE PREVIOUS READING
		// MISSED, and it is here because arms 1-3 all plant a spelling the
		// narrow reading already caught: arm 2's two literals sit INSIDE the
		// `+`, so a syntactic join sees them. Neither literal here contains a
		// DDL keyword on its own — `ddlHead` carries `CREATE TABLE` but the
		// `+` expression contains no BasicLit at all — so the joined value is
		// EMPTY under the old reading. A control that plants only what the
		// old filter caught proves location-within-filter and never
		// filter-covers-subject: the class one level down.
		"internal/web/planted_split.go": `package planted

const ddlHead = "CREATE TABLE IF NOT EXISTS visit ("
const ddlTail = "visit_id TEXT PRIMARY KEY, last_name TEXT) STRICT;"

const schema = ddlHead + ddlTail
`,
		// arm 5 (NEGATIVE, and it guards the WIDENING) — a NON-DDL statement
		// assembled from two named constants, in internal/domain. The wide
		// reading resolves it exactly as it resolves arm 4; if it fired here
		// the widening would be "flag every constant concatenation", which is
		// a detector that fires on true code.
		"internal/domain/planted_split_clean.go": `package planted

const selHead = "SELECT person_snapshot_name FROM visit "
const selTail = "WHERE is_open = 1 ORDER BY visit_id"

const listQuery = selHead + selTail
`,
		// arm 6 — DDL assembled from a FORMAT TEMPLATE, planted in
		// internal/domain. ⚠ ⟦LOCK — H-3, post-lock amendment 2026-09-02;
		// NEW ARM, nothing superseded.⟧ THIS IS THE SHAPE THAT WAS INSIDE
		// THE CRITERION'S STATED SUBJECT AND OUTSIDE ITS FILTER: [MEASURED
		// by W6's re-run gate, reports/window-build/W6-falsification-2.md
		// §8.1 H-3] a `fmt.Sprintf("CREATE TABLE … (%s) STRICT;", cols)` in
		// a COMPILING production file was NOT flagged. Neither the ALTER/ADD
		// literal arm (the format string contains neither) nor the `+` arm
		// (there is no `+`) sees it, and W1's guard 3a admits it because the
		// literal does contain `CREATE `. This file's own header names the
		// shape — "a `+`-concatenated or fmt.Sprintf-assembled INSERT is as
		// invisible to a literal walk as to a grep" — and the scan then
		// covered only the first half of that sentence.
		"internal/domain/planted_sprintf.go": `package planted

import "fmt"

func schemaFor(cols string) string {
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS visit (%s) STRICT;", cols)
}
`,
		// arm 7 (NEGATIVE, and it guards the TEMPLATE arm) — a NON-DDL
		// statement built from the same printf shape, in internal/store. If
		// this fired, the arm would be "flag every Sprintf", which is a
		// detector that fires on true code — OW-1's recorded consequence.
		"internal/store/planted_sprintf_clean.go": `package planted

import "fmt"

func listQuery(cols string) string {
	return fmt.Sprintf("SELECT %s FROM visit WHERE is_open = 1", cols)
}
`,
		// arm 8 (NEGATIVE, on the OTHER side of the template arm) — the
		// shape the criterion EXPLICITLY PERMITS: a whole schema statement
		// inside ONE `CREATE `-containing literal, with no verb and nothing
		// arriving from outside. W6-X1 says "every schema change inside one
		// CREATE literal"; an arm that reddened here would forbid the
		// criterion's own compliant form, and the production schema is
		// exactly this shape.
		"cmd/sitectl/planted_literal_clean.go": `package planted

const visitSchema = "CREATE TABLE IF NOT EXISTS visit (visit_id TEXT PRIMARY KEY, person_snapshot_name TEXT) STRICT;"
`,
	})

	// ⚠ RULE 6 ON THE PLANT ITSELF: arm 4 must be a case the OLD, NARROW
	// reading MISSES, or this control re-proves what arm 2 already proved.
	// The narrow reading is kept switchable for exactly this assertion.
	narrow, _ := w6ScanDDLTreeMode(t, plantRoot, w6WalkRoots, false)
	for _, f := range narrow {
		if strings.Contains(f, "planted_split.go") {
			t.Fatalf("CONTROL SETUP (arm 4): the constant-split DDL plant is detected by the NARROW, "+
				"literal-fragments-only reading — so it is NOT a case that reading missed, and detecting it "+
				"below would prove nothing about the widening. Re-pose the plant.\n  narrow faults: %v", narrow)
		}
	}

	ctlFaults, ctlDiags := w6ScanDDLTree(t, plantRoot, w6WalkRoots)
	// ⟦LOCK — §56(1)⟧ ASKED BEFORE ANY ARM IS SCORED. Eight arms are read
	// below; a plant that does not type-check silences the RESOLVED-CONSTANT
	// limb, and every one of them would then report "NOT detected".
	if !s56PlantTypeChecked(t, "W6-X1 control tree (eight arms)", ctlDiags) {
		return
	}
	var sawAlter, sawConcat, sawClean, sawSplit, sawSplitClean bool
	var sawSprintf, sawSprintfClean, sawLiteralClean bool
	for _, f := range ctlFaults {
		switch {
		case strings.Contains(f, "planted_alter.go"):
			sawAlter = true
		case strings.Contains(f, "planted_concat.go"):
			sawConcat = true
		case strings.Contains(f, "planted_sprintf_clean.go"):
			sawSprintfClean = true
		case strings.Contains(f, "planted_sprintf.go"):
			sawSprintf = true
		case strings.Contains(f, "planted_literal_clean.go"):
			sawLiteralClean = true
		case strings.Contains(f, "planted_clean.go"):
			sawClean = true
		case strings.Contains(f, "planted_split_clean.go"):
			sawSplitClean = true
		case strings.Contains(f, "planted_split.go"):
			sawSplit = true
		}
	}
	if !sawAlter {
		t.Fatalf("W6-X1 CONTROL FAILED (arm 1, m6-X1): a planted `ALTER TABLE … ADD COLUMN` in internal/domain was "+
			"NOT detected. ⚠ The plant is deliberately OUTSIDE internal/store: a walk scoped to the package W6 "+
			"edits satisfies every other control and is blind to nine files.\n  faults: %v", ctlFaults)
	}
	if !sawConcat {
		t.Fatalf("W6-X1 CONTROL FAILED (arm 2, m6-X1): a DDL string built by `+` concatenation in cmd/ was NOT "+
			"detected — W1's guard 3a admits a literal only if it contains `CREATE `, leaving DDL outside such a "+
			"literal invisible.\n  faults: %v", ctlFaults)
	}
	if sawClean {
		t.Fatalf("W6-X1 CONTROL FAILED (arm 3, the NEGATIVE arm): a plain `+`-concatenated SELECT was flagged as "+
			"DDL. A detector that fires on true code is a detector a builder will weaken.\n  faults: %v", ctlFaults)
	}
	if !sawSplit {
		t.Fatalf("W6-X1 CONTROL FAILED (arm 4, m6-X1): a `CREATE TABLE` assembled from TWO NAMED CONSTANTS in "+
			"internal/web was NOT detected. ⚠ This is the arm that proves FILTER-COVERS-SUBJECT rather than "+
			"location-within-filter: it is a case the previous, literal-fragments-only reading MISSED (asserted "+
			"directly above), and W6-X1's subject is \"no DDL assembled by concatenating a column list onto a "+
			"prefix\" — WHERE the fragments are declared is no part of that subject. W1's guard 3a admits a "+
			"literal only if it contains `CREATE `, so a column list in a separate constant is invisible to BOTH "+
			"guards at once.\n  faults: %v", ctlFaults)
	}
	if sawSplitClean {
		t.Fatalf("W6-X1 CONTROL FAILED (arm 5, the NEGATIVE arm ON THE WIDENING): a NON-DDL query assembled from "+
			"two named constants was flagged. The go/types resolution must widen the scan to DDL that a literal "+
			"join cannot see, NOT to every constant concatenation.\n  faults: %v", ctlFaults)
	}
	if !sawSprintf {
		t.Fatalf("W6-X1 CONTROL FAILED (arm 6, H-3): a `CREATE TABLE` assembled from a FORMAT TEMPLATE — "+
			"fmt.Sprintf(\"CREATE TABLE … (%%s) STRICT;\", cols) in internal/domain — was NOT detected. "+
			"⚠ This shape is squarely inside W6-X1's stated subject (\"no DDL assembled by concatenating a "+
			"column list onto a prefix\"; a printf verb IS that concatenation) and was outside its filter until "+
			"this arm landed: the ALTER/ADD literal arm does not fire because the format string contains "+
			"neither, the `+` arm does not fire because there is no `+`, and W1's guard 3a admits the literal "+
			"because it contains `CREATE `. INVISIBLE TO ALL THREE AT ONCE.\n  faults: %v", ctlFaults)
	}
	if sawSprintfClean {
		t.Fatalf("W6-X1 CONTROL FAILED (arm 7, the NEGATIVE arm ON THE TEMPLATE ARM): a NON-DDL query built by "+
			"fmt.Sprintf was flagged. The template arm must fire on DDL-carrying templates, NOT on every "+
			"formatted query.\n  faults: %v", ctlFaults)
	}
	if sawLiteralClean {
		t.Fatalf("W6-X1 CONTROL FAILED (arm 8, the NEGATIVE arm on the criterion's OWN COMPLIANT FORM): a whole "+
			"schema statement inside ONE `CREATE `-containing literal, with no verb, was flagged. That is the "+
			"shape W6-X1 EXISTS TO PERMIT — the production schema is exactly it — and a detector that reds on "+
			"it is a detector a builder must weaken.\n  faults: %v", ctlFaults)
	}

	// --- the real walk ---------------------------------------------------
	root := w6ModuleRoot(t)
	files := w6ProductionGoFiles(t, root, w6WalkRoots)
	// Rule 6 — the walk must actually reach all three of the places the
	// controls prove it CAN reach.
	for _, must := range []string{
		filepath.Join(root, "internal", "domain", "trip.go"),
		filepath.Join(root, "internal", "web", "muster.go"),
		filepath.Join(root, "internal", "store", "store.go"),
		filepath.Join(root, "cmd", "sitectl", "main.go"),
	} {
		found := false
		for _, f := range files {
			if f == must {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("PRECONDITION: the walk over %v did not reach %s — a walk that misses a package is the "+
				"narrow-filter class, and W6-X1's whole point is that W1's guard cannot see an ALTER TABLE anywhere",
				w6WalkRoots, must)
		}
	}
	faults, treeDiags := w6ScanDDLTree(t, root, w6WalkRoots)
	s56LogTree(t, "w6ScanDDLTree", treeDiags)
	if len(faults) != 0 {
		t.Errorf("W6-X1 (m6-X1): a schema change outside a single `CREATE `-containing string literal. W6 ADDS A "+
			"COLUMN, which is exactly the moment someone reaches for ALTER TABLE, and W1's guard 3a cannot see "+
			"one:\n  %s", strings.Join(faults, "\n  "))
	}
}

// w6DDLKeywords are the DDL markers W6-X1's subject names. `ADD COLUMN` and
// `CREATE INDEX` sit on the CONCATENATION arm as well as the literal arm:
// a concatenation is precisely how a keyword leaves a literal.
var w6DDLKeywords = []string{"CREATE TABLE", "CREATE TRIGGER", "CREATE INDEX", "ALTER TABLE", "ADD COLUMN"}

// w6DDLKeywordIn returns the first DDL marker present in sql, or "".
func w6DDLKeywordIn(sql string) string {
	up := strings.ToUpper(sql)
	for _, bad := range w6DDLKeywords {
		if strings.Contains(up, bad) {
			return bad
		}
	}
	return ""
}

// w6FormatVerbRe matches a printf verb — the mark of a string that is a
// TEMPLATE rather than a statement. `%%` is stripped before matching, so a
// literal percent sign is not a verb.
var w6FormatVerbRe = regexp.MustCompile(`%[-+# 0-9.*\[\]]*[vTtbcdoOqxXUeEfFgGsp]`)

// w6HasFormatVerb reports whether s carries at least one printf verb.
func w6HasFormatVerb(s string) bool {
	return w6FormatVerbRe.MatchString(strings.ReplaceAll(s, "%%", ""))
}

// w6SyntacticJoin is the NARROW reading of a `+` expression: the *ast.BasicLit
// fragments lying syntactically inside it, joined. It cannot see an operand
// that is an identifier.
func w6SyntacticJoin(e ast.Expr) string {
	var joined strings.Builder
	ast.Inspect(e, func(m ast.Node) bool {
		if lit, ok := m.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			joined.WriteString(w6Unquote(lit.Value))
		}
		return true
	})
	return joined.String()
}

// w6ResolvedConstString is the WIDE reading: the constant value go/types
// folded the whole expression to, which exists whenever every operand is a
// constant — including named constants declared elsewhere in the package.
func w6ResolvedConstString(info *types.Info, e ast.Expr) string {
	tv, ok := info.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return ""
	}
	return constant.StringVal(tv.Value)
}

// w6ScanDDLTree walks a tree and reports every W6-X1 violation: a string
// literal containing ALTER TABLE or ADD COLUMN, DDL assembled by `+`, or DDL
// carried in a FORMAT TEMPLATE.
//
// ⚠ THE FORMAT-TEMPLATE ARM IS A GAP CLOSED (H-3), not a preference, and it
// is the SAME defect one door further along from the `+` arm below. [MEASURED
// by W6's re-run gate, reports/window-build/W6-falsification-2.md §8.1 H-3]
//
//	fmt.Sprintf("CREATE TABLE IF NOT EXISTS visit (%s) STRICT;", cols)
//
// in a COMPILING production file returned ZERO faults. The ALTER/ADD literal
// arm does not fire (the format string contains neither); the `+` arm does
// not fire (there is no `+`); and W1's guard 3a ADMITS the literal, because
// it contains `CREATE `. Invisible to all three at once — the same both-guards
// -blind exposure the constant-split escape had, reached by a different door.
// The subject W6-X1 states is "no DDL assembled by concatenating a column
// list onto a prefix"; a printf verb IS that concatenation, with the join
// written as a template instead of a `+`.
//
// ⚠ THE ARM'S BOUND, stated rather than implied: it fires on a string
// CONSTANT that carries BOTH a DDL keyword AND a printf verb — it does not
// try to decide whether that string is ever executed, and it does not read
// the call it is passed to. So `fmt.Fprintf`, `fmt.Appendf` and a template
// held in a named constant are all covered, while DDL assembled by a
// non-printf helper (strings.Join, a bytes.Buffer, text/template) is NOT.
// That residue is recorded, not papered over.
//
// ⚠ THE `+` ARM READS THE RESOLVED VALUE, NOT ONLY THE LITERAL FRAGMENTS, and
// that widening is a GAP CLOSED (G-3), not a preference. W6-X1's subject is
// "no DDL assembled by concatenating a column list onto a prefix"; WHERE the
// fragments are declared is no part of that subject. [MEASURED by the W6
// mutant gate] the previous reading joined only the *ast.BasicLit fragments
// lying syntactically inside the `+`, so
//
//	const ddlHead = "CREATE TABLE IF NOT EXISTS visit ("
//	const ddlTail = "…) STRICT;"
//	const schema  = ddlHead + ddlTail
//
// put NO BasicLit inside the `+` at all, joined to the EMPTY STRING, and the
// scan returned ZERO faults with the plant sitting in the file. That escape
// is not decorative: [DOC] W1's guard 3a admits a literal only if it contains
// `CREATE `, so a column list living in a separate constant was invisible to
// BOTH guards at once — the precise exposure W6-X1 was adopted to cover.
//
// ⚠ IT IS THE SEVENTH INSTANCE OF "a scan whose filter is narrower than its
// subject" ON THIS PROJECT, and the remedy is not invented here: W6-1(a) limb
// (i), in this same file, already resolves string constants through go/types
// and scans the JOINED value (w6VisitInserts shape 3), and it CAUGHT the
// analogous constant-to-constant `INSERT INTO visit` that this scan could not
// see. Two scans in one file, one strong and one weak; this is the weak one
// brought up to the strong one.
//
// The wide reading is a SUPERSET of the narrow one — BOTH are computed and
// either can raise the fault — so this can only add faults, never drop one.
// Type checking is best-effort and its errors are DISCARDED: the importer is
// nil, so cross-package identifiers do not resolve, which does not matter
// because a SAME-PACKAGE string constant folds regardless, and that is the
// shape the criterion is about.
func w6ScanDDLTree(t *testing.T, base string, roots []string) ([]string, s56Set) {
	t.Helper()
	return w6ScanDDLTreeMode(t, base, roots, true)
}

// w6ScanDDLTreeMode is w6ScanDDLTree with the go/types resolution switchable.
//
// ⚠ resolveConstants=false IS THE PREVIOUS, NARROW READING, and it is kept
// for exactly one purpose: so the control can PROVE that its constant-split
// plant is a case the old filter MISSED. A widened filter whose control still
// exercises only the old shape reproduces the narrow-filter class one level
// down — which is how this class has survived seven findings.
func w6ScanDDLTreeMode(t *testing.T, base string, roots []string, resolveConstants bool) ([]string, s56Set) {
	t.Helper()
	// ⟦LOCK — §56(1), 2026-09-03⟧ every directory this scan type-checked.
	// ⚠ EMPTY under resolveConstants=false, because that mode deliberately
	// runs no type-check at all — a caller must not read an empty set as
	// "the plant compiled".
	var diags s56Set

	// A Go package is a directory, and go/types resolves a named constant
	// only against the OTHER FILES OF ITS OWN PACKAGE — so the walk's files
	// are grouped by directory and type-checked together, rather than parsed
	// one at a time as the narrow reading did.
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

	var out []string
	seen := map[string]bool{}
	emit := func(msg string) {
		if seen[msg] {
			// A nested `+` reports at its leftmost operand's position, which
			// is its parent's position too; one statement is one fault.
			return
		}
		seen[msg] = true
		out = append(out, msg)
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
				// ⚠ REPORTED, NEVER SILENTLY SKIPPED. A scan that drops an
				// unparseable file is a scan whose coverage is smaller than
				// its stated scope — the narrow-filter class again, arrived
				// at by a different door. [MEASURED during this pass: a
				// malformed plant made muster.go unparseable and the DDL scan
				// reported ZERO faults, GREEN, with the plant sitting in the
				// file.] If this is another agent's in-flight work (rule 18),
				// scope the run around it and say so — do not read the green.
				emit(fmt.Sprintf("%s: UNPARSEABLE — this file was NOT scanned, so this scan's coverage "+
					"is smaller than its stated scope: %v", path, err))
				continue
			}
			parsed = append(parsed, f)
		}
		if len(parsed) == 0 {
			continue
		}

		info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
		if resolveConstants {
			// ⟦LOCK — §56(1), 2026-09-03⟧ RECORDED, never discarded. ⟦was:
			// "conf := types.Config{Error: func(error) {}, Importer: nil, DisableUnusedImportCheck: true}"⟧
			// The RESOLVED-CONSTANT limb is the whole widening this scan
			// exists for; a plant that does not type-check silences it and
			// the harness then says "the plant was NOT detected".
			dg := s56Diags{Where: dir}
			conf := types.Config{Error: dg.Record, Importer: nil, DisableUnusedImportCheck: true}
			_, _ = conf.Check("w6ddlscan", fset, parsed, info)
			diags = append(diags, dg)
		}

		for _, f := range parsed {
			ast.Inspect(f, func(n ast.Node) bool {
				switch e := n.(type) {
				case *ast.BasicLit:
					if e.Kind != token.STRING {
						return true
					}
					raw := w6Unquote(e.Value)
					up := strings.ToUpper(raw)
					named := false
					for _, bad := range []string{"ALTER TABLE", "ADD COLUMN"} {
						if strings.Contains(up, bad) {
							emit(fmt.Sprintf("%s: string literal containing %s", fset.Position(e.Pos()), bad))
							named = true
						}
					}
					// THE FORMAT-TEMPLATE ARM. A DDL string carrying a printf
					// verb is DDL ASSEMBLED FROM A TEMPLATE: the column list
					// arrives through the verb, exactly as it arrives through
					// a `+` on the arm above, and the statement is therefore
					// not "declared in one CREATE-containing literal".
					if !named && w6HasFormatVerb(raw) {
						if bad := w6DDLKeywordIn(raw); bad != "" {
							emit(fmt.Sprintf("%s: DDL assembled from a FORMAT TEMPLATE (%s) — the statement "+
								"carries a printf verb, so its column list arrives from OUTSIDE the literal",
								fset.Position(e.Pos()), bad))
						}
					}
				case *ast.BinaryExpr:
					if e.Op != token.ADD {
						return true
					}
					for _, reading := range []struct{ sql, how string }{
						{w6SyntacticJoin(e), "joined from the literal fragments inside the `+`"},
						{w6ResolvedConstString(info, e), "RESOLVED through go/types — its fragments are NAMED " +
							"CONSTANTS and are invisible to a literal join"},
					} {
						if reading.sql == "" {
							continue
						}
						if bad := w6DDLKeywordIn(reading.sql); bad != "" {
							emit(fmt.Sprintf("%s: DDL assembled by `+` concatenation (%s), %s",
								fset.Position(e.Pos()), bad, reading.how))
							break
						}
					}
				}
				return true
			})
		}
	}
	sort.Strings(out)
	return out, diags
}

// --- W6-8 structural half: the open-visit filter is UNTOUCHED -----------
//
// The measured hazard this exists for: muster.go:229 sits on the SHARED
// path, so a reprint of a visit set whose people have all checked out
// renders an EMPTY roster. W6-3's fixture will meet this, and the tempting
// repair is to relax the predicate.
//
// EXPECTED RED at f33c4c7: NONE (plan §2.5 row 6). Its evidence is the two
// plants — the RELAXATION and, separately, the INVERSION.
func TestW6BuildMusterOpenVisitFilterIsUnchanged(t *testing.T) {
	const wantCond = "v.IsOpen != 1"

	// --- CONTROL: two plants, through the same detector -----------------
	relaxed := `package planted

type Visit struct{ IsOpen int64 }

func buildMuster(visits []Visit) {
	for _, v := range visits {
		_ = v
	}
}
`
	inverted := `package planted

type Visit struct{ IsOpen int64 }

func buildMuster(visits []Visit) {
	for _, v := range visits {
		if v.IsOpen == 1 {
			continue
		}
	}
}
`
	clean := `package planted

type Visit struct{ IsOpen int64 }

func buildMuster(visits []Visit) {
	for _, v := range visits {
		if v.IsOpen != 1 {
			continue
		}
	}
}
`
	if got := w6OpenVisitGuard(t, "relaxed.go", relaxed); got != "" {
		t.Fatalf("W6-8 CONTROL FAILED (relaxation): the detector reported the guard %q on a buildMuster with the "+
			"`continue` REMOVED, want none (m6-8)", got)
	}
	if got := w6OpenVisitGuard(t, "inverted.go", inverted); got != "" && got == wantCond {
		t.Fatalf("W6-8 CONTROL FAILED (inversion): the detector accepted `v.IsOpen == 1` as the certified guard " +
			"(m6-8). ⚠ A pinned COUNT is blind to inversion; this is the structural half that is not")
	}
	if got := w6OpenVisitGuard(t, "clean.go", clean); got != wantCond {
		t.Fatalf("W6-8 CONTROL FAILED (negative arm): the detector reported %q on a CLEAN buildMuster, want %q — "+
			"a detector that cannot see the correct guard would red on the real build for the wrong reason", got, wantCond)
	}

	// --- the real filter -------------------------------------------------
	root := w6ModuleRoot(t)
	path := filepath.Join(root, "internal", "domain", "muster.go")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := w6OpenVisitGuard(t, path, string(src)); got != wantCond {
		t.Errorf("W6-8 structural half (m6-8): buildMuster's open-visit filter is %q, want %q.\n"+
			"  W6 does not change WHO is on the board. [[D-61]] rules the reprint defect MOOT and the historical "+
			"roster inherits [[D-15]]b's membership rule — \"Still on site at the window's end (an open visit) → "+
			"INCLUDED … A person is excluded only if their presence and the window share no instant at all\" — and "+
			"W6 does not build the historical roster and asserts nothing about it.\n"+
			"  ⚠ SCOPE: this is asserted for the reprint route AS IT EXISTS AT f33c4c7, and is SUPERSEDED when the "+
			"historical roster lands under D-15b; (m6-8) retires with it.", got, wantCond)
	}
}

// w6OpenVisitGuard returns the source text of the condition of the `if …
// { continue }` guarding buildMuster's visit loop, or "" when there is none.
func w6OpenVisitGuard(t *testing.T, path, src string) string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	var out string
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "buildMuster" || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			ifs, ok := n.(*ast.IfStmt)
			if !ok || ifs.Body == nil || len(ifs.Body.List) != 1 {
				return true
			}
			if _, isCont := ifs.Body.List[0].(*ast.BranchStmt); !isCont {
				return true
			}
			cond := w6Src(t, fset, ifs.Cond)
			if strings.Contains(cond, "IsOpen") {
				out = cond
			}
			return true
		})
	}
	return out
}

// --- W6-10: no merge resolution on the person key ------------------------
//
// [[D-50]] consequence 5: "The person boundary is UNCHANGED … nothing here
// licenses person resolution on the muster or the historical roster."
// W3-7: "The violation is resolving a PERSON id through any merge path on the
// muster or historical roster."
//
// ⚠ THE FILTER IS A go/types RESOLUTION, NOT A SPELLING MATCH: a spelling
// match on `.Resolve(` is defeated by a local alias and cannot tell a person
// id from a company id. The receiver's TYPE is resolved.
//
// ⚠ THE CONTROL IS RE-SPECIFIED because the obvious one is NOT CONSTRUCTIBLE:
// MusterReprint takes NO AliasMap (muster.go:177) and muster.go:118-119
// forbids one in terms, so the plant cannot be written in the frozen closure
// WITHOUT the signature change that IS the violation. So:
//
//	(i)  LIVE closure — a SOURCE plant, `.Resolve(v.PersonID)`, flagged by the
//	     go/types scan. Behaviour is not the instrument: AliasMap.Resolve
//	     returns the id unchanged, so a bare plant is behaviourally inert.
//	(ii) FROZEN closure — the assertion that IS available: MusterReprint's
//	     parameter list contains NO AliasMap, planted by ADDING one.
//
// EXPECTED RED at f33c4c7: NONE (plan §2.5 row 8). Evidence: (m6-10).
func TestW6NoMergeResolutionOnThePersonKey(t *testing.T) {
	// --- CONTROL (i): the source plant, through the same scan -----------
	plantDir := filepath.Join(w6PlantTree(t, map[string]string{
		"planted/alias.go": `package planted

type AliasMap map[string]string

func (m AliasMap) Resolve(id string) string { return id }

type Visit struct{ PersonID string }

func MusterLive(visits []Visit, personMerge AliasMap) {
	for _, v := range visits {
		local := personMerge
		_ = local.Resolve(v.PersonID)
	}
}
`,
	}), "planted")
	aliasHits, aliasDiags := w6PersonResolveHits(t, plantDir, []string{"MusterLive"})
	// ⟦LOCK — §56(1)⟧ ASKED BEFORE THE HITS ARE READ. ⚠ This is the exact
	// shape §61 measured dead five times over: the plant stops compiling
	// after a rename, produces no hit, and the harness blames the detector.
	if !s56PlantTypeChecked(t, "W6-10(i) control (i): the LOCAL-ALIAS resolve plant", aliasDiags) {
		return
	}
	if len(aliasHits) == 0 {
		t.Fatalf("W6-10(i) CONTROL FAILED (m6-10): a planted `.Resolve(v.PersonID)` — reached through a LOCAL "+
			"ALIAS of the map, which is exactly what defeats a spelling match — was not detected.\n"+
			"⟦§56⟧ the plant DID type-check, so this is the scan and not the fixture:\n%s", aliasDiags.Report())
	}
	// --- CONTROL (i-b): THE CASE THE NARROW READING WOULD MISS ----------
	// The resolve moved into a HELPER the closure calls. A filter scoped to
	// the four read-path function names by NAME sees nothing here; the
	// transitive closure does. A control that only exercises the shape the
	// filter already sees proves nothing.
	helperDir := filepath.Join(w6PlantTree(t, map[string]string{
		"planted/helper.go": `package planted

type AliasMap map[string]string

func (m AliasMap) Resolve(id string) string { return id }

type Visit struct{ PersonID string }

func labelFor(v Visit, personMerge AliasMap) string { return personMerge.Resolve(v.PersonID) }

func MusterLive(visits []Visit, personMerge AliasMap) {
	for _, v := range visits {
		_ = labelFor(v, personMerge)
	}
}
`,
	}), "planted")
	helperHits, helperDiags := w6PersonResolveHits(t, helperDir, []string{"MusterLive"})
	if !s56PlantTypeChecked(t, "W6-10(i) control (i-b): the TRANSITIVE helper plant", helperDiags) {
		return
	}
	if len(helperHits) == 0 {
		t.Fatalf("W6-10(i) CONTROL FAILED (m6-10, the transitive arm): a `.Resolve(v.PersonID)` moved into a HELPER " +
			"called from the closure was not detected.\n" +
			"  ⚠ This is the case a filter scoped to the read-path function NAMES would miss, and it is planted " +
			"deliberately: a control that only exercises the shape the filter already sees proves nothing. " +
			"This project has found \"a scan whose filter is narrower than its subject\" SIX times.")
	}

	// --- CONTROL (i-c): THE UNSCANNED FILE IS A NAMED FAULT -------------
	//
	// ⟦LOCK — H-2, post-lock amendment 2026-09-02. NEW ARM AND NEW CONTROL;
	// nothing superseded, and the control did not exist for ANY of the four
	// scans before this pass.⟧ A file this scan cannot parse is
	// indistinguishable from a file with no violation in it, so a scan that
	// drops it silently reports a coverage it does not have. Its three
	// siblings in this file emit a named fault; this one used to `continue`.
	// The plant is a clean read-path file NEXT TO a syntactically broken one:
	// the clean half proves the scan still runs, and the broken half must
	// still be named.
	brokenDir := filepath.Join(w6PlantTree(t, map[string]string{
		"planted/ok.go": `package planted

type AliasMap map[string]string

func (m AliasMap) Resolve(id string) string { return id }

type Visit struct{ PersonID string }

func MusterLive(visits []Visit, personMerge AliasMap) {
	for _, v := range visits {
		_ = v
	}
}
`,
		"planted/broken.go": `package planted

func MusterLive( {
`,
	}), "planted")
	// ⚠ ⟦§56(1)⟧ THE ONE ARM WHERE THE PLANT BAR IS DELIBERATELY NOT APPLIED,
	// stated so the omission is a decision and not an oversight: this arm
	// plants a file that does not PARSE, on purpose, and its whole assertion
	// is that the scan NAMES it. The parse fault never reaches go/types, so
	// the type-check bar is silent here by construction; the clean half
	// (`ok.go`) is what must type-check, and it is checked below.
	brokenHits, brokenDiags := w6PersonResolveHits(t, brokenDir, []string{"MusterLive"})
	if !s56PlantTypeChecked(t, "W6-10(i) control (i-c): the clean half of the unparseable-file plant", brokenDiags) {
		return
	}
	sawUnparseable := false
	for _, h := range brokenHits {
		if strings.Contains(h, "UNPARSEABLE") && strings.Contains(h, "broken.go") {
			sawUnparseable = true
		}
	}
	if !sawUnparseable {
		t.Fatalf("W6-10(i) CONTROL FAILED (unparseable arm): a syntactically BROKEN file sitting in the scanned "+
			"directory was dropped SILENTLY — the scan reported %v and named no fault.\n"+
			"  ⚠ An unscanned file is indistinguishable from a clean one. This is the arm W6-X6's G-6 amendment "+
			"records as living in w6ScanDDLTree \"and its siblings\", and this scan was the exception.",
			brokenHits)
	}

	cleanDir := filepath.Join(w6PlantTree(t, map[string]string{
		"clean/alias.go": `package clean

type AliasMap map[string]string

func (m AliasMap) Resolve(id string) string { return id }

type Visit struct {
	PersonID          string
	CompanySnapshotID string
}

func MusterLive(visits []Visit, companyMerge AliasMap) {
	for _, v := range visits {
		_ = companyMerge.Resolve(v.CompanySnapshotID)
	}
}
`,
	}), "clean")
	cleanHits, cleanDiags := w6PersonResolveHits(t, cleanDir, []string{"MusterLive"})
	if !s56PlantTypeChecked(t, "W6-10(i) control (negative arm): the COMPANY resolve plant", cleanDiags) {
		return
	}
	if len(cleanHits) != 0 {
		t.Fatalf("W6-10(i) CONTROL FAILED (negative arm): a COMPANY resolve was flagged as a person resolve: %v.\n"+
			"  The company grouping DOES resolve through companyMerge (W5d), by design. A scan that cannot tell a "+
			"person id from a company id would force the builder to weaken it", cleanHits)
	}

	// --- the real scan ---------------------------------------------------
	root := w6ModuleRoot(t)
	domainDir := filepath.Join(root, "internal", "domain")
	treeHits, resolveTreeDiags := w6PersonResolveHits(t, domainDir, w6MusterReadPathRoots)
	s56LogTree(t, "w6PersonResolveHits", resolveTreeDiags)
	if len(treeHits) != 0 {
		t.Errorf("W6-10(i) (m6-10): this scan reported a fault in internal/domain — either a person key resolved "+
			"through a merge/alias path, or (H-2) a file the scan could not parse and therefore did NOT cover:\n  %s\n"+
			"  [[D-50]]/W3-7 forbid person merge resolution on the muster. ⚠ [[D-59]]'s \"the muster shows one "+
			"human\" is a DIFFERENT READ PATH: the exception sections read OccupantRef from TRIPS "+
			"(web/muster.go:200-212, tripauthor.go:715-718); the roster body reads Visit through buildMuster "+
			"(muster.go:221-248). W6 touches only the second, and is on the D-50/W3-7 side of that line.",
			strings.Join(treeHits, "\n  "))
	}

	// --- (ii) MusterReprint's parameter list carries NO AliasMap ---------
	plantedSig := `package planted

type AliasMap map[string]string
type Visit struct{}
type Person struct{}
type MusterBoard struct{}

func MusterReprint(visits []Visit, persons []Person, personMerge AliasMap, minCrewSize int) MusterBoard {
	return MusterBoard{}
}
`
	if got := w6ReprintAliasParams(t, "planted/sig.go", plantedSig); len(got) == 0 {
		t.Fatalf("W6-10(ii) CONTROL FAILED (m6-10): a planted AliasMap parameter on MusterReprint was not detected. " +
			"That is the violation stated in the form the frozen seam can actually carry — the plant the criterion " +
			"originally mandated is NOT CONSTRUCTIBLE, because it needs the signature change that IS the violation")
	}
	path := filepath.Join(domainDir, "muster.go")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := w6ReprintAliasParams(t, path, string(src)); len(got) != 0 {
		t.Errorf("W6-10(ii) (m6-10): MusterReprint's parameter list carries AliasMap parameter(s) %v.\n"+
			"  muster.go:118-119 says it in terms: \"⚠ MusterReprint below takes NEITHER map, by design. Do not "+
			"'helpfully' give it one.\" A reprint that CAN consult the alias table has lost the type-level "+
			"guarantee that it never re-resolves.", got)
	}
}

// w6MusterReadPathRoots are the entry points of THE SURFACE D-50/W3-7
// govern — "the muster or historical roster" — in internal/domain.
//
// ⚠ SCOPING BY THE MUSTER READ PATH IS NOT A NARROWING OF THE SUBJECT, and
// the distinction is measured. W3-7's violation is "resolving a PERSON id
// through any merge path ON THE MUSTER or historical roster". Two production
// call sites resolve a person id elsewhere and are CORRECT there [MEASURED at
// f33c4c7]: outcome.go:320 (`m.Resolve(r.PersonID)`) is Outcome's fact
// rendering — [[D-59]]'s surface, the exception sections' OccupantRef path —
// and person_amend.go:667 (`LoadPersonMergeMap`) is AmendPerson's merge-cycle
// check. A scan that flagged those would be a scan a builder must weaken,
// which is OW-1's recorded consequence.
//
// ⚠ THE CLOSURE IS TRANSITIVE, deliberately: a resolve moved into a helper
// the closure calls is the reading a function-name filter would miss, and
// the control below plants exactly that case.
var w6MusterReadPathRoots = []string{"MusterLive", "MusterReprint", "buildMuster", "personNameIndex"}

// w6PersonResolveHits type-checks one directory and reports every call, ON
// THE MUSTER READ PATH, whose RECEIVER TYPE is AliasMap and whose argument
// derives from a PersonID — plus any call to the person merge path by name.
//
// ⚠ The person merge path's own loader and its input are named:
// LoadPersonMergeMap (person_amend.go:446) IS the person merge path and
// Person.SameAs (refdata.go:197) is its input. BOTH were missing from
// revision 1's enumeration, on a criterion whose entire subject is the
// person key — which listed the COMPANY analogue and omitted the person one.
func w6PersonResolveHits(t *testing.T, dir string, roots []string) ([]string, s56Set) {
	t.Helper()
	// ⟦LOCK — §56(1), 2026-09-03⟧ the diagnostics this scan's type-check
	// raises. ⚠ THIS SCAN IS THE `internal/store` TWIN OF THE ONE §56 WAS
	// WRITTEN ABOUT, and §61 measured five of eight controls dead in that
	// one — every plant here resolves `.Resolve(…)` THROUGH go/types, so a
	// plant that does not type-check produces no hit at all.
	dg := s56Diags{Where: dir}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, s56Set{dg}
	}
	fset := token.NewFileSet()
	var files []*ast.File
	var faults []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			// ⚠ REPORTED, NEVER SILENTLY SKIPPED — H-4's sibling arm, landed
			// here so this scan is the FOURTH of four rather than the one
			// exception. W6-X6's G-6 amendment records the unparseable-file
			// fault as living in w6ScanDDLTree "and its siblings"; on
			// measurement that was true of three of four, and the exception
			// was THIS scan — the one whose coverage story was already the
			// weakest. An unscanned file is indistinguishable from a clean
			// one, which is exactly how a census goes green over a plant.
			// If this is another agent's in-flight work (rule 18), scope the
			// run around it and say so — do not read the green.
			faults = append(faults, fmt.Sprintf("%s: UNPARSEABLE — this file was NOT scanned, so this scan's "+
				"coverage is smaller than its stated scope: %v", path, err))
			continue
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		return faults, s56Set{dg}
	}
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	// ⟦LOCK — §56(1), 2026-09-03⟧ RECORDED, never discarded. ⟦was:
	// "conf := types.Config{Error: func(error) {}, Importer: importer.Default(), DisableUnusedImportCheck: true}"⟧
	conf := types.Config{Error: dg.Record, Importer: importer.Default(), DisableUnusedImportCheck: true}
	_, _ = conf.Check("w6resolve", fset, files, info)

	// --- the same-package call graph, and the read path's closure over it
	callees := map[string][]string{}
	bodies := map[string]*ast.FuncDecl{}
	for _, f := range files {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			bodies[fn.Name.Name] = fn
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch c := call.Fun.(type) {
				case *ast.Ident:
					callees[fn.Name.Name] = append(callees[fn.Name.Name], c.Name)
				case *ast.SelectorExpr:
					callees[fn.Name.Name] = append(callees[fn.Name.Name], c.Sel.Name)
				}
				return true
			})
		}
	}
	inPath := map[string]bool{}
	var walk func(name string, depth int)
	walk = func(name string, depth int) {
		if inPath[name] || depth > 4 {
			return
		}
		inPath[name] = true
		for _, c := range callees[name] {
			if _, local := bodies[c]; local {
				walk(c, depth+1)
			}
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
	// A closure literal assigned inside a read-path function is part of that
	// function's body, so it needs no separate root.

	hits := faults
	for _, f := range files {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !inPath[fn.Name.Name] {
				continue
			}
			enclosing := fn.Name.Name
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := ""
				switch c := call.Fun.(type) {
				case *ast.Ident:
					name = c.Name
				case *ast.SelectorExpr:
					name = c.Sel.Name
				}
				if name == "LoadPersonMergeMap" {
					hits = append(hits, fmt.Sprintf("%s (in %s): LoadPersonMergeMap() — the PERSON merge path",
						fset.Position(call.Pos()), enclosing))
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Resolve" {
					return true
				}
				// THE FILTER: the receiver's resolved TYPE, not its spelling.
				// A local alias of the map keeps the type; a spelling match
				// does not survive it.
				tv, ok := info.Types[sel.X]
				if !ok {
					return true
				}
				named, ok := tv.Type.(*types.Named)
				if !ok || named.Obj().Name() != "AliasMap" {
					return true
				}
				for _, a := range call.Args {
					txt := w6Src(t, fset, a)
					if strings.Contains(txt, "PersonID") || strings.Contains(txt, "SameAs") {
						hits = append(hits, fmt.Sprintf("%s (in %s): AliasMap.Resolve(%s) — a PERSON id resolved through a merge path",
							fset.Position(call.Pos()), enclosing, txt))
					}
				}
				return true
			})
		}
	}
	sort.Strings(hits)
	return hits, s56Set{dg}
}

// w6ReprintAliasParams returns the names of MusterReprint's AliasMap-typed
// parameters — asserted STRUCTURALLY over the function's signature.
func w6ReprintAliasParams(t *testing.T, path, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	var out []string
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "MusterReprint" || fn.Type.Params == nil {
			continue
		}
		for _, p := range fn.Type.Params.List {
			if w6TypeNameOf(p.Type) != "AliasMap" {
				continue
			}
			if len(p.Names) == 0 {
				out = append(out, "<unnamed>")
			}
			for _, n := range p.Names {
				out = append(out, n.Name)
			}
		}
	}
	return out
}
