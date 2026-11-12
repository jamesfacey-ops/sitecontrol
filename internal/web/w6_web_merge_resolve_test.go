// w6_web_merge_resolve_test.go — W6-10's `internal/web` HALF, which W6's
// test-writer declared as a shortfall against itself and did not build.
//
// [DOC] W6-10 (plan:6443-6500) specifies the merge-resolve scan as running
// "through `go/types` over `internal/domain` AND `internal/web`". The
// delivered scan (internal/store/w6_source_scans_test.go:1159-1160) is handed
// `internal/domain` and nothing else. [DOC] W6-tests.md §7.3 says so in terms,
// and says why it stopped: "cross-package type resolution needs an importer
// the control tree cannot cheaply supply … a green from an un-controlled
// cross-package scan should not be read as coverage."
//
// [DOC] W6-falsification-2.md §4.2 then PRODUCED the violation —
// `companyMerge.Resolve(v.PersonID)` in `handleMusterLive`, no signature
// change needed because `internal/web/muster.go` already holds a
// `domain.AliasMap` — and watched ALL 22 W6 TESTS STAY GREEN. This file is
// the check that was missing. It is not the domain scan pointed at a second
// directory: it is the same FILTER — a `go/types` resolution of the
// receiver's TYPE, never a spelling match — carried ACROSS A PACKAGE
// BOUNDARY, which is the part that did not exist.
//
// WHY THE WEB HALF IS A REAL SURFACE and not a formality. The violation
// [[D-50]] consequence 5 and W3-7 forbid is "resolving a PERSON id through
// any merge path ON THE MUSTER". [MEASURED] `handleMusterLive` holds two live
// `domain.AliasMap` values in scope (`companyMerge`, `aliasMap` —
// `internal/web/muster.go:333` and `:338`) ALONGSIDE the `[]domain.Visit` it
// is about to hand to `domain.MusterLive`. One line —
// `open[i].PersonID = aliasMap.Resolve(open[i].PersonID)` — commits the
// violation from OUTSIDE the closure, where W6-10(i)'s `internal/domain` walk
// cannot see it, and `domain.MusterLive` never learns it happened. The frozen
// route is the same story with a loader instead of a parameter: `MusterReprint`
// takes no `AliasMap` (W6-10(ii)'s structural assertion), but nothing stops
// `handleMusterPrint` calling `domain.LoadPersonMergeMap` itself.
//
// ⚠ THE IMPORTER IS THE WHOLE DIFFICULTY, and it is solved rather than
// declared impossible. `internal/web` names the type as `domain.AliasMap`, so
// the receiver cannot be resolved without importing
// `sitecontrol/internal/domain`. w610Importer type-checks module-local
// packages FROM SOURCE, recursively, off the same root the walk is given;
// non-module imports go to `importer.Default()` and, only if that fails, to a
// synthetic empty package, so one unresolvable stdlib import cannot silence
// the whole scan. Stdlib only — no new dependency.
//
// ⚠ AND AN UNCONTROLLED GREEN IS NOT COVERAGE — §7.3's own warning — so this
// file does three things a probe does not:
//
//  1. Every control is planted into a COPY OF THE REAL PRODUCTION SOURCE
//     (every `internal/**/*.go`, `_test.go` excluded), not into a synthetic
//     two-file tree. The cross-package resolution a control exercises is the
//     same one the clean run depends on, over the same files. [DOC] rule 20:
//     `go test -overlay` does NOT reach a source scan, so these are real
//     on-disk edits on a real on-disk copy. Each plant is applied ALONE,
//     reverted, and the revert proved byte-for-byte.
//  2. The clean pass is GATED ON CALIBRATION. Before any green counts the
//     scan must show cross-package resolution is live ON THE TRACKED TREE:
//     the domain package imported, `AliasMap` found in it WITH its `Resolve`
//     method, and at least one expression in `internal/web` actually typed as
//     a `sitecontrol/internal/domain` type. A broken importer therefore fails
//     as INSTRUMENT BROKEN and can never present itself as a clean pass.
//  3. The read-path roots are DERIVED FROM THE SURFACE, not hand-kept.
//     [DOC] W6-falsification-2.md §5.2 grades `w6MusterReadPathRoots` — the
//     domain half's four hand-written names — a live instance of "hand-kept
//     enumerations rot". Here the roots are computed at test time from the
//     registered `/muster` routes AND from every function that calls
//     `domain.MusterLive` / `domain.MusterReprint`, with the two known
//     handlers kept only as a FLOOR that the derivation must still contain.
//     Two controls plant a new muster entry point and prove the derivation
//     sees what a hand-kept list would have missed.
//
// EXPECTED RED at f33c4c7: NONE. W6-10 is ⟦NO PRE-CHUNK RED⟧ and this half is
// compliant by construction — [MEASURED] `internal/web` contains ZERO
// `.Resolve(` call sites in production code at all. Its evidence is (m6-10),
// planted ten ways below.
package web

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	w610ModulePath = "sitecontrol"
	w610DomainPkg  = w610ModulePath + "/internal/domain"
	w610WebPkg     = "internal/web"
)

// w610RootFloor is the pair of muster entry points in `internal/web` that are
// KNOWN to exist. It is a floor the derivation must still contain, NOT the
// scan's scope: a derivation that stopped working would otherwise silently
// shrink to this list and still look green.
var w610RootFloor = []string{"handleMusterLive", "handleMusterPrint"}

// w610Reading is one way of reading the tree. The controls run the full
// reading against a deliberately narrower one, so a plant can be PROVED to be
// a case the narrower reading misses — the discipline W6-X1's repair
// established (W5-POSTLOCK-AMENDMENTS.md §30.2). A widened filter whose
// control only exercises the shape the old filter already saw is the
// narrow-filter class one level down.
type w610Reading struct {
	FollowArgAliases bool // an argument reached through a local still derives from the person id
	DeriveRoots      bool // the read path is computed from the surface, not the floor list

	// ⟦LOCK — §56, 2026-09-03⟧ THE DERIVATION HAS TWO LIMBS, and until now
	// they could not be run apart. §56 part (3) requires W-C6 and W-C7 to be
	// shown to distinguish DIFFERENT things; with only a floor-only rival,
	// both controls merely proved "not the floor" and their distinction from
	// EACH OTHER was prose. These two flags make each limb a rival reading in
	// its own right, so the claim becomes a measurement:
	//
	//	W-C6 is found by the ROUTE limb ALONE   (callers-only must miss it)
	//	W-C7 is found by the CALLER limb ALONE  (routes-only must miss it)
	//
	// A fixture where the certified rule and its rivals return DIFFERENT
	// answers — standing rule 3, fixture non-degeneracy, discharged by
	// construction rather than asserted.
	RootsFromRoutes  bool // limb (a): handlers registered on a route naming the muster
	RootsFromCallers bool // limb (b): functions that call MusterLive / MusterReprint

	// ⟦LOCK — §56, 2026-09-03⟧ §61 condition 2 requires each control to be
	// shown DISTINCT from its siblings. A control is distinct exactly when
	// some rival reading MISSES it that finds the others — so an axis with no
	// rival cannot be checked at all, only asserted. Before §61 this file had
	// rivals for TWO axes (argument aliasing, root derivation) and none for
	// the other three, which is why W-C2, W-C3 and W-C5 could claim to test
	// different things with nothing able to contradict them.
	//
	//	SpellReceiver  match the receiver by NAME instead of by resolved type —
	//	               the exact filter W6-10 says "is defeated by a local
	//	               alias". Defeats W-C2.
	//	NoTransitive   walk only the root bodies, never their callees. Defeats
	//	               W-C3, whose whole subject is one call of depth.
	//	NoByName       drop the `LoadPersonMergeMap` by-name limb. Defeats
	//	               W-C5, the only control that rides it.
	SpellReceiver bool
	NoTransitive  bool
	NoByName      bool
}

var (
	// ⟦was: "w610Full      = w610Reading{FollowArgAliases: true, DeriveRoots: true}"⟧
	// ⟦was: "w610NarrowArg = w610Reading{FollowArgAliases: false, DeriveRoots: true}"⟧
	// ⟦was: "w610FloorOnly = w610Reading{FollowArgAliases: true, DeriveRoots: false}"⟧
	// ⟦LOCK — §56, 2026-09-03⟧ the limbs are named explicitly on every reading;
	// the full reading is BOTH limbs, and is the only one that is.
	w610Full      = w610Reading{FollowArgAliases: true, DeriveRoots: true, RootsFromRoutes: true, RootsFromCallers: true}
	w610NarrowArg = w610Reading{FollowArgAliases: false, DeriveRoots: true, RootsFromRoutes: true, RootsFromCallers: true}
	w610FloorOnly = w610Reading{FollowArgAliases: true, DeriveRoots: false}
	// ⟦LOCK — §56, 2026-09-03⟧ the two new rivals, each ONE limb of the
	// derivation. Neither is a reading anyone would ship; both exist purely so
	// a control can be PROVED to be the case the other limb misses.
	w610RoutesOnly  = w610Reading{FollowArgAliases: true, DeriveRoots: true, RootsFromRoutes: true}
	w610CallersOnly = w610Reading{FollowArgAliases: true, DeriveRoots: true, RootsFromCallers: true}
	// ⟦LOCK — §56, 2026-09-03⟧ §61: the three rivals for the previously
	// unrivalled axes. Each is the full reading with exactly one limb narrowed.
	w610SpellReceiver = w610Reading{FollowArgAliases: true, DeriveRoots: true, RootsFromRoutes: true, RootsFromCallers: true, SpellReceiver: true}
	w610NoTransitive  = w610Reading{FollowArgAliases: true, DeriveRoots: true, RootsFromRoutes: true, RootsFromCallers: true, NoTransitive: true}
	w610NoByName      = w610Reading{FollowArgAliases: true, DeriveRoots: true, RootsFromRoutes: true, RootsFromCallers: true, NoByName: true}
)

// w610Rivals is EVERY narrowed reading, run against EVERY positive plant. The
// set of rivals that miss a plant is that control's SIGNATURE, and two controls
// with the same signature are — to this scan — the same check written twice.
// ⟦LOCK — §56, 2026-09-03⟧ §61 condition 2.
var w610Rivals = []struct {
	Name string
	Rd   w610Reading
}{
	{"narrow-argument", w610NarrowArg},
	{"floor-only", w610FloorOnly},
	{"routes-only", w610RoutesOnly},
	{"callers-only", w610CallersOnly},
	{"receiver-by-spelling", w610SpellReceiver},
	{"no-transitive", w610NoTransitive},
	{"no-by-name", w610NoByName},
}

// w610NamesAMergeMap is the RIVAL filter: what a scan that matched the receiver
// by SPELLING would accept. [DOC] W6-10: "A spelling match on `.Resolve(` is
// defeated by a local alias" — this is that spelling match, made runnable so
// the claim can be tested instead of quoted.
func w610NamesAMergeMap(src string) bool {
	return strings.Contains(src, "Merge") || strings.Contains(src, "lias")
}

// --- the importer -------------------------------------------------------

// w610Importer resolves module-local packages from SOURCE, recursively.
// Non-module paths go to the shared gc importer, then to a synthetic empty
// package. An unresolvable import is never fatal: a scan that dies on one is
// a scan that reports zero faults for the wrong reason.
type w610Importer struct {
	root       string
	fset       *token.FileSet
	cache      map[string]*types.Package
	inProgress map[string]bool
	fallback   types.Importer
	stubbed    []string
	// ⟦LOCK — §139, 2026-09-03⟧ files under the copy root that did NOT PARSE.
	// ⚠ SEPARATE FROM errs BELOW ON PURPOSE: a type error and a parse failure
	// are different stages and a parse failure is the worse one — see
	// w610ScanResult.ParseErrors.
	parseErrs []string
	// ⟦LOCK — §56, 2026-09-03⟧ diagnostics from checking module-local packages
	// FROM SOURCE. A plant may add a file to ANY package under the copy root,
	// not only `internal/web`; without this, a plant that broke
	// `internal/domain` would be swallowed here instead of at the top level,
	// and §56's defect would survive one directory to the left.
	errs []string
}

func w610NewImporter(root string, fset *token.FileSet) *w610Importer {
	return &w610Importer{
		root:       root,
		fset:       fset,
		cache:      map[string]*types.Package{},
		inProgress: map[string]bool{},
		fallback:   importer.Default(),
	}
}

func (im *w610Importer) Import(path string) (*types.Package, error) {
	if p, ok := im.cache[path]; ok {
		return p, nil
	}
	if (path == w610ModulePath || strings.HasPrefix(path, w610ModulePath+"/")) && !im.inProgress[path] {
		im.inProgress[path] = true
		defer delete(im.inProgress, path)
		rel := strings.TrimPrefix(strings.TrimPrefix(path, w610ModulePath), "/")
		if files := w610ParsePkg(im.fset, filepath.Join(im.root, rel), &im.parseErrs); len(files) > 0 {
			// ⟦LOCK — §56, 2026-09-03⟧ record, never swallow.
			// ⟦was: "conf := types.Config{Error: func(error) {}, Importer: im, DisableUnusedImportCheck: true}"⟧
			conf := types.Config{
				Error:                    func(err error) { im.errs = append(im.errs, path+": "+err.Error()) },
				Importer:                 im,
				DisableUnusedImportCheck: true,
			}
			pkg, _ := conf.Check(path, im.fset, files, nil)
			if pkg != nil {
				pkg.MarkComplete()
				im.cache[path] = pkg
				return pkg, nil
			}
		}
	}
	if im.fallback != nil {
		if p, err := im.fallback.Import(path); err == nil {
			im.cache[path] = p
			return p, nil
		}
	}
	name := path
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	p := types.NewPackage(path, name)
	p.MarkComplete()
	im.cache[path] = p
	im.stubbed = append(im.stubbed, path)
	return p, nil
}

// w610ParsePkg parses one directory's PRODUCTION .go files. `_test.go` is
// excluded and nothing else is.
//
// ⭐ ⟦LOCK — §139, 2026-09-03 (S102D)⟧ THE PARSE DOOR, CLOSED. It used to
// `continue` on a parse error with no record of any kind.
//
// ⚠ THAT IS §56's DEFECT ONE STAGE EARLIER, AND IT IS THE WORSE STAGE.
// A type error leaves the plant IN THE CORPUS — it has an AST, every
// syntactic limb still walks it, and only the type-resolved limbs go quiet.
// A file that does not PARSE is not in the corpus at all: no AST, no limb,
// nothing. [MEASURED, S102C: one plant broken both ways through one scan —
// the type-broken plant was still DETECTED by the syntactic limb; the
// parse-broken one was not detected by anything.] So a type swallow's blast
// radius is bounded by how much a scan leans on go/types, and a parse
// swallow's is TOTAL.
//
// ⛔ AND THIS IS THE HARNESS §56(1) WAS WRITTEN AGAINST. Its plants land in
// a COPY OF REAL PRODUCTION SOURCE, which is the arrangement §111 proved
// rots — five of eight controls here died to a production rename and every
// one read green. A plant that stopped PARSING would have vanished from
// every limb at once and this file never said the word UNPARSEABLE.
func w610ParsePkg(fset *token.FileSet, dir string, parseErrs *[]string) []*ast.File {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []*ast.File
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			// ⟦LOCK — §139⟧ RECORDED, never dropped. ⟦was: "continue"⟧
			// ⚠ The `continue` STAYS: one unparseable file must not stop the
			// other files in the directory from being scanned. What changes
			// is that its absence is now SAYABLE.
			if parseErrs != nil {
				*parseErrs = append(*parseErrs, filepath.Join(dir, e.Name())+": UNPARSEABLE — this file was NOT parsed, so it is absent from every limb of this scan: "+err.Error())
			}
			continue
		}
		files = append(files, f)
	}
	return files
}

// --- the scan -----------------------------------------------------------

// w610Unit is one body the scan may walk: a named function, or an anonymous
// handler registered directly on a muster route.
type w610Unit struct {
	Name string
	Body *ast.BlockStmt
}

// w610ScanResult carries the hits AND the calibration facts, because a result
// that reports only "no hits" cannot be told apart from a scan that resolved
// nothing at all.
type w610ScanResult struct {
	Hits []string

	Files            int      // production files parsed in the scanned directory
	DomainTypedExprs int      // expressions typed as an `internal/domain` named type
	AliasMapMethod   string   // proof that domain.AliasMap resolved, with its method
	RootsDerived     []string // read-path entry points computed from the surface
	RootsMissing     []string // floor entries the derivation or the package lost
	OnPath           []string // the transitive closure actually walked

	// ⟦LOCK — §56, 2026-09-03⟧ TypeErrors is every diagnostic go/types raised
	// while checking the scanned package, PLUS every one raised while
	// importing a module-local package from source.
	//
	// ⚠ IT EXISTS BECAUSE BOTH CONFIGS USED TO SWALLOW THEM. A plant that
	// does not type-check produces NO HIT — which is byte-identical to what a
	// BLIND SCANNER produces. The harness printed "the planted violation was
	// NOT detected" for both, so a DEAD PLANT (a fixture defect) and a BLIND
	// SCANNER (a real, serious finding) were rendered IDENTICALLY, and the
	// wording sent the next reader to debug the one part that was working.
	// ⟦§56⟧ These fields are what make the two distinguishable.
	//
	// ⚠ THE TWO STREAMS ARE KEPT APART BECAUSE THEY ANSWER DIFFERENT
	// QUESTIONS, and [MEASURED] one of them is NEVER EMPTY on a healthy tree:
	//
	//	TypeErrors   diagnostics in the SCANNED directory's own files. On a
	//	             healthy tree this is EMPTY, and it must be, because every
	//	             hit depends on go/types typing an expression in THESE
	//	             files. A required-zero bar.
	//	ImportErrors diagnostics from checking module-local packages FROM
	//	             SOURCE. [MEASURED] this is 9 on a clean tree and that is
	//	             BY DESIGN, not rot: `w610ParsePkg` honours no build
	//	             constraints, so `internal/store`'s clock_linux.go and
	//	             clock_notlinux.go are BOTH parsed (`readClockTrust
	//	             redeclared`); and a non-module import that
	//	             `importer.Default()` cannot supply falls back to a
	//	             synthetic EMPTY package, so `norm.NFC`, `unix.Adjtimex`
	//	             and `toml.Decode` read as undefined. Requiring zero here
	//	             would red the whole file for the importer's own designed
	//	             fallbacks. A baseline-relative bar instead.
	TypeErrors   []string
	ImportErrors []string

	// ⟦LOCK — §139, 2026-09-03⟧ ParseErrors is every file under the copy
	// root that did not PARSE — from the scanned directory AND from every
	// module-local package the importer read from source.
	//
	// ⚠ IT IS A THIRD STREAM, NOT A THIRD KIND OF TypeError, and the bar is
	// different from both of the others:
	//
	//	TypeErrors   required-zero on an unplanted tree.
	//	ImportErrors baseline-relative (9 on a clean tree, by design).
	//	ParseErrors  ⭐ REQUIRED-ZERO ON ANY TREE, planted or not. [MEASURED]
	//	             it is 0 on the tracked tree and 0 under every plant this
	//	             file runs, because the tree compiles and every plant is
	//	             well-formed Go. There is no designed-nonzero case, so
	//	             unlike ImportErrors it needs no baseline to be read
	//	             against — a single entry is a defect, always.
	ParseErrors []string
}

// w610ResolveScan is THE instrument. It type-checks one directory of the tree
// rooted at `root` and reports every call, ON THE MUSTER READ PATH, that
// resolves a PERSON id through a merge/alias path.
//
// THE FILTER, stated so it can be read rather than inferred:
//
//	receiver — the call is `X.Resolve(…)` and X's RESOLVED TYPE is a named
//	           `AliasMap`. Resolved through go/types across the package
//	           boundary, so `domain.AliasMap`, a local alias of it, a struct
//	           field of that type and a conversion to it are all one case. A
//	           spelling match on `.Resolve(` is defeated by any of them.
//	argument — the argument DERIVES from a person id: its source text names
//	           `PersonID` or `SameAs`, or (under the full reading) it is a
//	           local whose assignment in the same body does. ⚠ "derives from"
//	           is the criterion's own word; a filter requiring the argument to
//	           be SPELLED `v.PersonID` would be narrower than its subject,
//	           which is the class this file exists to repair.
//	by name  — plus any call to `LoadPersonMergeMap`, which IS the person
//	           merge path (internal/domain/person_amend.go:446) and whose
//	           input is `Person.SameAs` (refdata.go:197). Both were missing
//	           from revision 1's enumeration ⟦R-19 / F-10⟧.
//	scope    — the transitive same-package closure from the DERIVED muster
//	           entry points. Scoping is the criterion's own ("on the muster"),
//	           not a narrowing: a scan that flagged every `.Resolve` in
//	           `internal/web` would be a scan a builder must weaken, which is
//	           OW-1's recorded consequence.
func w610ResolveScan(t *testing.T, root, relDir string, rd w610Reading) w610ScanResult {
	t.Helper()
	var res w610ScanResult
	fset := token.NewFileSet()
	files := w610ParsePkg(fset, filepath.Join(root, relDir), &res.ParseErrors)
	res.Files = len(files)
	if len(files) == 0 {
		return res
	}
	im := w610NewImporter(root, fset)
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	// ⟦LOCK — §56, 2026-09-03⟧ ⭐ THE DURABLE FIX. Diagnostics are RECORDED, not
	// discarded, so a plant that fails to type-check can be NAMED as such
	// instead of presenting itself as a scanner that saw nothing.
	// ⟦was: "conf := types.Config{Error: func(error) {}, Importer: im, DisableUnusedImportCheck: true}"⟧
	conf := types.Config{
		Error:                    func(err error) { res.TypeErrors = append(res.TypeErrors, err.Error()) },
		Importer:                 im,
		DisableUnusedImportCheck: true,
	}
	_, _ = conf.Check(w610ModulePath+"/"+filepath.ToSlash(relDir), fset, files, info)
	// Diagnostics raised while importing module-local packages from source
	// answer the SAME question — did the tree under this scan compile? — so
	// they join the same list. Collected AFTER Check, because imports are
	// resolved lazily during it.
	res.ImportErrors = append(res.ImportErrors, im.errs...)
	// ⟦LOCK — §139⟧ and the parse faults, from the same lazily-resolved walk.
	res.ParseErrors = append(res.ParseErrors, im.parseErrs...)

	// --- calibration: is cross-package resolution actually LIVE? --------
	for _, tv := range info.Types {
		if named, ok := tv.Type.(*types.Named); ok {
			if o := named.Obj(); o.Pkg() != nil && o.Pkg().Path() == w610DomainPkg {
				res.DomainTypedExprs++
			}
		}
	}
	if dom := im.cache[w610DomainPkg]; dom != nil && dom.Scope() != nil {
		if obj := dom.Scope().Lookup("AliasMap"); obj != nil {
			if named, ok := obj.Type().(*types.Named); ok {
				for i := 0; i < named.NumMethods(); i++ {
					if named.Method(i).Name() == "Resolve" {
						res.AliasMapMethod = named.Method(i).Type().String()
					}
				}
			}
		}
	}

	// --- the read path, DERIVED from the surface ------------------------
	bodies := map[string]*ast.FuncDecl{}
	callees := map[string][]string{}
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
	var extra []w610Unit
	rootSet := map[string]bool{}
	if rd.DeriveRoots {
		derived, lits := w610DerivedMusterRoots(t, fset, files, rd) // ⟦LOCK — §56, 2026-09-03⟧ ⟦was: "w610DerivedMusterRoots(t, fset, files)"⟧
		for _, n := range derived {
			rootSet[n] = true
		}
		extra = lits
	}
	for _, n := range w610RootFloor {
		if _, ok := bodies[n]; !ok {
			res.RootsMissing = append(res.RootsMissing, n+" (no such function in the package)")
			continue
		}
		if rd.DeriveRoots && !rootSet[n] {
			res.RootsMissing = append(res.RootsMissing, n+" (present, but the derivation did not find it)")
		}
		rootSet[n] = true
	}
	for n := range rootSet {
		res.RootsDerived = append(res.RootsDerived, n)
	}
	sort.Strings(res.RootsDerived)

	inPath := map[string]bool{}
	var walk func(name string, depth int)
	walk = func(name string, depth int) {
		if inPath[name] || depth > 6 {
			return
		}
		// ⟦LOCK — §56, 2026-09-03⟧ the no-transitive rival: roots only.
		if rd.NoTransitive && depth > 0 {
			return
		}
		inPath[name] = true
		for _, c := range callees[name] {
			if _, local := bodies[c]; local {
				walk(c, depth+1)
			}
		}
	}
	for n := range rootSet {
		if _, ok := bodies[n]; ok {
			walk(n, 0)
		}
	}
	units := extra
	for name := range inPath {
		if fn, ok := bodies[name]; ok {
			units = append(units, w610Unit{Name: name, Body: fn.Body})
		}
	}
	sort.Slice(units, func(i, j int) bool { return units[i].Name < units[j].Name })
	for _, u := range units {
		res.OnPath = append(res.OnPath, u.Name)
	}

	// --- the hits -------------------------------------------------------
	for _, u := range units {
		body := u.Body
		ast.Inspect(body, func(n ast.Node) bool {
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
			// ⟦LOCK — §56, 2026-09-03⟧ ⟦was: "\t\t\tif name == \"LoadPersonMergeMap\" {"⟧
			if name == "LoadPersonMergeMap" && !rd.NoByName {
				res.Hits = append(res.Hits, fmt.Sprintf("%s (in %s): LoadPersonMergeMap() — the PERSON merge path, loaded on the muster read path",
					fset.Position(call.Pos()), u.Name))
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Resolve" {
				return true
			}
			// ⟦LOCK — §56, 2026-09-03⟧ the receiver-by-spelling rival.
			if rd.SpellReceiver && !w610NamesAMergeMap(w610Src(t, fset, sel.X)) {
				return true
			}
			tv, ok := info.Types[sel.X]
			if !ok {
				return true
			}
			named, ok := tv.Type.(*types.Named)
			if !ok || named.Obj().Name() != "AliasMap" {
				return true
			}
			owner := "<local>"
			if p := named.Obj().Pkg(); p != nil {
				owner = p.Path()
			}
			for _, a := range call.Args {
				if why, hit := w610ArgIsPersonID(t, fset, info, body, a, rd.FollowArgAliases); hit {
					res.Hits = append(res.Hits, fmt.Sprintf("%s (in %s): %s.Resolve(%s) — a PERSON id resolved through a merge path [receiver type %s.AliasMap, %s]",
						fset.Position(call.Pos()), u.Name, w610Src(t, fset, sel.X), w610Src(t, fset, a), owner, why))
				}
			}
			return true
		})
	}
	sort.Strings(res.Hits)
	return res
}

// w610DerivedMusterRoots computes the muster read path from the SURFACE
// instead of from a hand-written list, two ways:
//
//	(a) every handler registered on a route whose pattern names the muster —
//	    the routes ARE the entry points, so a new muster page cannot be added
//	    without this seeing it;
//	(b) every function that calls `domain.MusterLive` or `domain.MusterReprint`
//	    — the closures W6-10 governs, so a caller that assembles a board
//	    without a route (a job, an export, a test helper promoted to
//	    production) is an entry point too.
//
// A handler registered as a literal function has no name; its body is
// returned directly so it is walked rather than silently skipped.
// ⟦LOCK — §56, 2026-09-03⟧ ⟦was: "func w610DerivedMusterRoots(t *testing.T, fset *token.FileSet, files []*ast.File) ([]string, []w610Unit) {"⟧
// The reading is now a parameter, because the two limbs must be runnable
// APART — see w610Reading.
func w610DerivedMusterRoots(t *testing.T, fset *token.FileSet, files []*ast.File, rd w610Reading) ([]string, []w610Unit) {
	t.Helper()
	seen := map[string]bool{}
	var lits []w610Unit
	for _, f := range files {
		// (a) route registrations
		if rd.RootsFromRoutes { // ⟦LOCK — §56, 2026-09-03⟧
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) < 2 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || (sel.Sel.Name != "HandleFunc" && sel.Sel.Name != "Handle") {
					return true
				}
				pat, ok := call.Args[0].(*ast.BasicLit)
				if !ok || !strings.Contains(strings.ToLower(pat.Value), "muster") {
					return true
				}
				for _, name := range w610HandlerNames(call.Args[1]) {
					seen[name] = true
				}
				if lit := w610HandlerLiteral(call.Args[1]); lit != nil {
					lits = append(lits, w610Unit{Name: "<literal handler at " + fset.Position(lit.Pos()).String() + ">", Body: lit.Body})
				}
				return true
			})
		}
		// (b) callers of the closures W6-10 governs
		if !rd.RootsFromCallers { // ⟦LOCK — §56, 2026-09-03⟧
			continue
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if sel.Sel.Name == "MusterLive" || sel.Sel.Name == "MusterReprint" {
					seen[fn.Name.Name] = true
				}
				return true
			})
		}
	}
	var out []string
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, lits
}

// w610HandlerNames pulls the handler identifiers out of a registration
// argument, descending through wrappers such as `s.requireLogin(s.handleX)`.
func w610HandlerNames(e ast.Expr) []string {
	switch v := e.(type) {
	case *ast.SelectorExpr:
		return []string{v.Sel.Name}
	case *ast.Ident:
		return []string{v.Name}
	case *ast.CallExpr:
		var out []string
		for _, a := range v.Args {
			out = append(out, w610HandlerNames(a)...)
		}
		return out
	}
	return nil
}

func w610HandlerLiteral(e ast.Expr) *ast.FuncLit {
	switch v := e.(type) {
	case *ast.FuncLit:
		return v
	case *ast.CallExpr:
		for _, a := range v.Args {
			if l := w610HandlerLiteral(a); l != nil {
				return l
			}
		}
	}
	return nil
}

// w610ArgIsPersonID answers "does this argument DERIVE from a person id".
//
// Two readings, and the difference between them is controlled below:
//
//	direct        the argument's own source text names PersonID / SameAs.
//	single-assign the argument is a LOCAL whose assignment in the same body
//	              does. `pid := v.PersonID; m.Resolve(pid)` is the same
//	              violation with one more line in it, and the direct reading
//	              cannot see it. The local is matched by go/types OBJECT
//	              IDENTITY, not by name, so shadowing cannot fool it.
func w610ArgIsPersonID(t *testing.T, fset *token.FileSet, info *types.Info, body *ast.BlockStmt, arg ast.Expr, follow bool) (string, bool) {
	t.Helper()
	if w610NamesPersonID(w610Src(t, fset, arg)) {
		return "argument names the person id directly", true
	}
	if !follow {
		return "", false
	}
	id, ok := arg.(*ast.Ident)
	if !ok {
		return "", false
	}
	obj := info.Uses[id]
	if obj == nil {
		obj = info.Defs[id]
	}
	if obj == nil {
		return "", false
	}
	found := ""
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range as.Lhs {
			lid, ok := lhs.(*ast.Ident)
			if !ok || (info.Defs[lid] != obj && info.Uses[lid] != obj) {
				continue
			}
			if i < len(as.Rhs) && w610NamesPersonID(w610Src(t, fset, as.Rhs[i])) {
				found = fmt.Sprintf("argument is a local assigned from %s at %s", w610Src(t, fset, as.Rhs[i]), fset.Position(as.Pos()))
			}
		}
		return true
	})
	return found, found != ""
}

func w610NamesPersonID(src string) bool {
	return strings.Contains(src, "PersonID") || strings.Contains(src, "SameAs")
}

func w610Src(t *testing.T, fset *token.FileSet, n ast.Node) string {
	t.Helper()
	var b strings.Builder
	if err := printer.Fprint(&b, fset, n); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// --- the control tree ---------------------------------------------------

func w610ModuleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd() // …/app/internal/web
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("fixture defect: no go.mod at %s — the scan cannot locate the files it is scoped to: %v", root, err)
	}
	return root
}

// w610CopyProduction copies every production .go file under `internal/` into a
// temp root, module-shaped, so the copy type-checks EXACTLY as the tracked
// tree does. The controls are then planted into REAL PRODUCTION SOURCE rather
// than into a synthetic two-file package — which is the difference between
// proving the scan can detect a cross-package resolve and proving it can
// detect one in a tree that has no cross-package problem to solve.
func w610CopyProduction(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	n := 0
	err := filepath.Walk(filepath.Join(src, "internal"), func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		n++
		return os.WriteFile(out, b, 0o644)
	})
	if err != nil {
		t.Fatalf("CONTROL SETUP FAILED: copying production source: %v", err)
	}
	if n == 0 {
		t.Fatal("CONTROL SETUP FAILED: copied zero production files — the control tree would prove nothing")
	}
	return dst
}

// w610Plant is one violation, applied ALONE to the production copy.
type w610Plant struct {
	Name string
	// Anchor is a substring of internal/web/muster.go; Insert goes in
	// immediately BEFORE the line containing it. A STRUCTURAL anchor (the
	// call to the closure, the route registration), never a physical line
	// number — [DOC] W5-POSTLOCK-AMENDMENTS.md §30.3: a control pinned to
	// line numbers made a test dictate the line-wrapping of a life-safety
	// template.
	Anchor  string
	Insert  string
	NewFile string // rel path under the copy root, "" for none
	NewSrc  string

	WantHit           bool
	NarrowArgMustMiss bool // the argument-alias widening must be what finds it
	FloorOnlyMustMiss bool // the root DERIVATION must be what finds it
	// ⟦LOCK — §56, 2026-09-03⟧ WHICH LIMB of the derivation must be what finds
	// it. FloorOnlyMustMiss says only "not the hand-kept list"; these two say
	// WHICH of the two derivation limbs is load-bearing, which is what stops
	// two controls that both clear the floor from being the same control
	// twice.
	RoutesOnlyMustMiss  bool // the CALLER limb must be what finds it (it has no route)
	CallersOnlyMustMiss bool // the ROUTE limb must be what finds it (it calls neither closure)
	// ⟦LOCK — §56, 2026-09-03⟧ §61 condition 2: the three previously unrivalled
	// axes. Each names the ONE narrowing that must fail to find this plant.
	SpellReceiverMustMiss bool // the RESOLVED TYPE must be what finds it, not the receiver's name
	NoTransitiveMustMiss  bool // the TRANSITIVE walk must be what finds it
	NoByNameMustMiss      bool // the BY-NAME limb must be what finds it
	Why                   string
}

func w610MusterPath(root string) string { return filepath.Join(root, "internal", "web", "muster.go") }

func w610ApplyPlant(t *testing.T, root string, p w610Plant, musterOrig []byte) {
	t.Helper()
	if p.Anchor != "" {
		src := string(musterOrig)
		i := strings.Index(src, p.Anchor)
		if i < 0 {
			t.Fatalf("CONTROL SETUP FAILED (%s): anchor %q is not present in internal/web/muster.go.\n"+
				"  The anchor is a structural site — the call to the closure, or the route registration — not a "+
				"line number. If it was renamed the plant must be re-aimed: a control that cannot find its anchor "+
				"must FATAL, never silently plant nothing and then report a clean pass.", p.Name, p.Anchor)
		}
		lineStart := strings.LastIndex(src[:i], "\n") + 1
		if err := os.WriteFile(w610MusterPath(root), []byte(src[:lineStart]+p.Insert+src[lineStart:]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if p.NewFile != "" {
		if err := os.WriteFile(filepath.Join(root, p.NewFile), []byte(p.NewSrc), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func w610RevertPlant(t *testing.T, root string, p w610Plant, musterOrig []byte) {
	t.Helper()
	if err := os.WriteFile(w610MusterPath(root), musterOrig, 0o644); err != nil {
		t.Fatal(err)
	}
	if p.NewFile != "" {
		if err := os.Remove(filepath.Join(root, p.NewFile)); err != nil {
			t.Fatal(err)
		}
	}
	// the revert PROVED, not assumed
	back, err := os.ReadFile(w610MusterPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back, musterOrig) {
		t.Fatalf("CONTROL FAILED (%s): the revert did not restore internal/web/muster.go byte-for-byte", p.Name)
	}
	if p.NewFile != "" {
		if _, err := os.Stat(filepath.Join(root, p.NewFile)); err == nil {
			t.Fatalf("CONTROL FAILED (%s): %s survived the revert", p.Name, p.NewFile)
		}
	}
}

// --- the plants ---------------------------------------------------------
//
// ⭐⭐ ⟦LOCK — §139, 2026-09-03 (S102D)⟧ THE PLANT ARRANGEMENT, CHOSEN AND
// WRITTEN DOWN HERE RATHER THAN LEFT TO BE INFERRED.
//
// §111 states the fork: a plant that references PRODUCTION SYMBOLS can die
// from a rename; a SELF-CONTAINED plant cannot die that way, but also cannot
// detect drift that only shows against real source. Every plant below is the
// first kind — applied to a COPY OF REAL PRODUCTION SOURCE, anchored to a
// structural site in the real internal/web/muster.go.
//
// ⚠ AND THAT IS THE ARRANGEMENT THAT KILLED FIVE OF EIGHT CONTROLS HERE
// (§61): W8's trade-merge rename moved a parameter and the plants died with
// it, every one of them reading green.
//
// ⭐ KEPT ANYWAY, DELIBERATELY, and the reason is that the alternative cannot
// pose this file's case at all. W-C6 plants a NEW MUSTER ROUTE and W-C7 a NEW
// CALLER; both exist to prove the read-path DERIVATION finds an entry point
// the floor list does not name. A self-contained plant has no route table and
// no caller graph to be found in — it would prove the scan can walk a
// synthetic file, which is not the claim. ⚠ The drift-detection §111 names as
// this arrangement's ADVANTAGE is the same property that makes it rot: the
// plant is coupled to production because the criterion is.
//
// ⛔ SO THE ANSWER IS NOT TO DECOUPLE THE PLANT. IT IS TO MAKE ITS DEATH
// LOUD, which is what ⟦§56(1)⟧ did for a plant that does not TYPE-CHECK and
// ⟦§139⟧ has now done for one that does not PARSE. A plant that rots here can
// no longer report itself as a scanner that found nothing — at either stage.

// ⟦was: "func w6PlantPersonLabel(v domain.Visit, m domain.AliasMap) string {"⟧
// ⟦was: "\treturn m.Resolve(v.PersonID)"⟧
// w610HelperSrc is the resolve moved ONE CALL DEEP. Planted twice, WITH the
// call (W-C3, must be flagged) and WITHOUT it (W-C7, must not) — the only
// difference between the two is reachability.
const w610HelperSrc = `package web

import "sitecontrol/internal/domain"

func w6PlantPersonLabel(v domain.Visit, companyMerge domain.AliasMap) string {
	return companyMerge.Resolve(v.PersonID)
}
`

// w610RouteHandlerSrc is a NEW muster route's handler. Nothing on the floor
// list calls it and its name is in no hand-kept enumeration; only the
// derivation from the registered routes reaches it.
// ⟦LOCK — §56, 2026-09-03⟧ RE-ARMED. It named `domain.LoadAliasMap`, which W8
// DELETED (`internal/domain/alias.go` is gone; `w8_deletions_test.go:506` lists
// `LoadAliasMap` among the deleted spellings). The plant stopped compiling and
// the harness printed "the planted violation was NOT detected".
// ⟦was: "	m, err := domain.LoadAliasMap(s.Store)"⟧
//
// ⟦was: "\tm, err := domain.LoadCompanyMergeMap(s.Store)"⟧
// ⟦was: "\t\topen[i].PersonID = m.Resolve(open[i].PersonID)"⟧
// ⚠ THE REPLACEMENT IS `LoadCompanyMergeMap`, NOT `LoadPersonMergeMap`, and the
// choice is load-bearing — §56 part (3). `LoadPersonMergeMap` is W-C5's symbol
// AND is matched by the scan's BY-NAME limb, so a plant using it would fire
// without go/types resolving anything: W-C5, W-C6 and W-C7 would all become
// the same by-name check with different roots, and the cross-package TYPE
// resolution this whole file exists to prove would go unexercised by two of
// its three route controls. `LoadCompanyMergeMap` is in NO by-name list, so
// this plant's hit is necessarily a RECEIVER-TYPE hit, resolved across the
// package boundary — the limb under test. It is also the live production
// idiom: `internal/web/muster.go:352` calls exactly this.
const w610RouteHandlerSrc = `package web

import (
	"net/http"

	"sitecontrol/internal/domain"
)

func (s *Server) handleMusterExport(w http.ResponseWriter, r *http.Request) {
	open, err := s.openVisitsAtThisSite()
	if err != nil {
		return
	}
	companyMerge, err := domain.LoadCompanyMergeMap(s.Store)
	if err != nil {
		return
	}
	for i := range open {
		open[i].PersonID = companyMerge.Resolve(open[i].PersonID)
	}
}
`

// w610BoardBuilderSrc assembles a muster board with NO route at all. Only the
// second derivation — every caller of MusterLive/MusterReprint — reaches it.
// ⟦LOCK — §56, 2026-09-03⟧ RE-ARMED, and it was dead TWICE OVER — a second
// death the brief and §56 both missed, and which repairing only the loader
// would have left in place.
// ⟦was: "	m, _ := domain.LoadAliasMap(s.Store)"⟧
// ⟦was: "	return domain.MusterReprint(open, persons, s.Cfg.MusterMinCrewSize)"⟧
//
// ⟦was: "\tm, _ := domain.LoadCompanyMergeMap(s.Store)"⟧
// ⟦was: "\t\topen[i].PersonID = m.Resolve(open[i].PersonID)"⟧
//
// The loader: same reasoning as w610RouteHandlerSrc above.
//
// The crew-size argument: the field was renamed `MusterMinCrewSize` ->
// `MusterMinGroupSize` by a different pass. ⚠ IT IS REPLACED BY A LITERAL, not
// by the new field name. This control asserts NOTHING about configuration —
// its subject is that a muster board assembled with no route is still on the
// read path — so binding the fixture to a config field's spelling bought
// nothing and cost exactly one silent death. `MusterReprint`'s third parameter
// is `minGroupSize int`; any int reaches the same call site.
const w610BoardBuilderSrc = `package web

import "sitecontrol/internal/domain"

func (s *Server) w6PlantBoardForExport() domain.MusterBoard {
	open, _ := s.openVisitsAtThisSite()
	persons, _ := domain.Persons(s.Store)
	companyMerge, _ := domain.LoadCompanyMergeMap(s.Store)
	for i := range open {
		open[i].PersonID = companyMerge.Resolve(open[i].PersonID)
	}
	return domain.MusterReprint(open, persons, 2)
}
`

var w610LivePlants = []w610Plant{
	{
		Name:    "W-C0 THE GATE'S OWN PLANT — companyMerge.Resolve(v.PersonID) in handleMusterLive",
		Anchor:  "board := domain.MusterLive(",
		Insert:  "\tfor _, v := range open {\n\t\t_ = companyMerge.Resolve(v.PersonID)\n\t}\n",
		WantHit: true,
		Why: "[DOC] W6-falsification-2.md §4.2 planted exactly this and watched all 22 W6 tests stay green. It is " +
			"reproduced verbatim as the first control so this check is measured against the escape it exists to " +
			"close, not against a plant of its own choosing.",
	},
	{
		Name:   "W-C1 the assignment form — the person key OVERWRITTEN before the closure sees it",
		Anchor: "board := domain.MusterLive(",
		// ⟦LOCK — §56, 2026-09-03⟧ ⟦was: "\t\tInsert:  \"\\tfor i := range open {\\n\\t\\topen[i].PersonID = aliasMap.Resolve(open[i].PersonID)\\n\\t}\\n\","⟧
		// §61: the replacement is the DIRECT SUCCESSOR AT THE SAME POSITION, not
		// the most convenient symbol that compiles. `aliasMap` was the SIXTH
		// argument of `domain.MusterLive`; W8's trade-merge split renamed that
		// same slot to `tradeMerge` (e9cc5f5: "…companyMerge, aliasMap,
		// s.Cfg.MusterMinCrewSize)"; now: "…companyMerge, tradeMerge, …").
		Insert:  "\tfor i := range open {\n\t\topen[i].PersonID = tradeMerge.Resolve(open[i].PersonID)\n\t}\n",
		WantHit: true,
		Why: "The version that actually changes the sheet: the rows handed to domain.MusterLive carry resolved " +
			"person keys and the closure never learns it happened, so W6-10(i)'s internal/domain walk sees a " +
			"clean closure operating on pre-merged data.",
	},
	{
		Name:   "W-C2 local alias — the receiver reached through a local, which defeats a spelling match",
		Anchor: "board := domain.MusterLive(",
		// ⟦LOCK — §56, 2026-09-03⟧ ⟦was: "\t\tInsert:  \"\\tlocalMap := aliasMap\\n\\t_ = localMap.Resolve(open[0].PersonID)\\n\","⟧ §61: direct successor at the same position.
		Insert:  "\tlocalMap := tradeMerge\n\t_ = localMap.Resolve(open[0].PersonID)\n",
		WantHit: true,
		// ⟦LOCK — §56, 2026-09-03⟧ §61: what W-C2 distinguishes. The receiver is
		// spelled `localMap`, which names no merge map, so a filter keying on
		// the receiver's NAME misses it and only the RESOLVED TYPE finds it.
		SpellReceiverMustMiss: true,
		Why: "[DOC] W6-10: \"A spelling match on `.Resolve(` is defeated by a local alias.\" The receiver is no " +
			"longer spelled `aliasMap`; its TYPE is unchanged, and the type is what the filter reads.",
	},
	{
		Name:   "W-C3 transitive — the resolve moved into a helper the handler calls",
		Anchor: "board := domain.MusterLive(",
		// ⟦LOCK — §56, 2026-09-03⟧ ⟦was: "\t\tInsert:  \"\\t_ = w6PlantPersonLabel(open[0], aliasMap)\\n\","⟧
		// ⚠ §61's table lists W-C3 as LIVE. It is not, and the reason is exactly
		// the defect §61 rules on: on the UNMODIFIED harness this control
		// reported CONTROL OK, so the census that produced that table inherited
		// the very blindness it was ruling about. The hit came from
		// `w6plant_helper.go`, which type-checks standalone; what did not
		// compile is THIS CALL SITE — and the call site IS the control, since
		// W-C9 plants a byte-identical helper WITHOUT it. Repaired under §61's
		// own rule: same slot, direct successor.
		Insert:  "\t_ = w6PlantPersonLabel(open[0], tradeMerge)\n",
		NewFile: "internal/web/w6plant_helper.go",
		NewSrc:  w610HelperSrc,
		WantHit: true,
		// ⟦LOCK — §56, 2026-09-03⟧ §61: what W-C3 distinguishes. Its hit is one
		// call DEEP, so the roots-only rival cannot reach it; every other
		// positive control plants directly into a root body.
		NoTransitiveMustMiss: true,
		Why: "The case a filter scoped to the handler NAMES would miss. A control that only exercises the shape " +
			"the filter already sees proves nothing — this project has found \"a scan whose filter is narrower " +
			"than its subject\" NINE times.",
	},
	{
		Name:   "W-C4 aliased ARGUMENT — the person id reached through a local",
		Anchor: "board := domain.MusterLive(",
		// ⟦LOCK — §56, 2026-09-03⟧ ⟦was: "\t\tInsert:            \"\\tpid := open[0].PersonID\\n\\t_ = aliasMap.Resolve(pid)\\n\","⟧ §61: direct successor at the same position.
		Insert:            "\tpid := open[0].PersonID\n\t_ = tradeMerge.Resolve(pid)\n",
		WantHit:           true,
		NarrowArgMustMiss: true,
		Why: "W6-10's filter is \"whose argument DERIVES FROM `v.PersonID`\". One assignment defeats a filter " +
			"requiring the argument to be SPELLED `v.PersonID`, and the narrow reading is run alongside to PROVE " +
			"this plant is a case it misses — the W6-X1 repair's own discipline (W5-POSTLOCK-AMENDMENTS.md §30.2).",
	},
	{
		Name:    "W-C5 by name, on the FROZEN route — handleMusterPrint loads the person merge map itself",
		Anchor:  "board := domain.MusterReprint(",
		Insert:  "\t_, _ = domain.LoadPersonMergeMap(s.Store)\n",
		WantHit: true,
		// ⟦LOCK — §56, 2026-09-03⟧ §61: what W-C5 distinguishes. It is the ONLY
		// control that rides the BY-NAME limb — no receiver is typed and no
		// argument is inspected — so dropping that limb must lose it and lose
		// nothing else.
		NoByNameMustMiss: true,
		Why: "The web-side analogue of W6-10(ii). `MusterReprint` takes no `AliasMap`, but nothing stops its " +
			"CALLER loading one. This plant also proves `handleMusterPrint` is a LIVE root: without it the frozen " +
			"route would be un-exercised and a green would cover one handler while implying two.",
	},
	{
		Name:              "W-C6 a NEW muster route, in no hand-kept list",
		Anchor:            "mux.HandleFunc(\"GET /muster/print\"",
		Insert:            "\tmux.HandleFunc(\"GET /muster/export\", s.handleMusterExport)\n",
		NewFile:           "internal/web/w6plant_route.go",
		NewSrc:            w610RouteHandlerSrc,
		WantHit:           true,
		FloorOnlyMustMiss: true,
		// ⟦LOCK — §56, 2026-09-03⟧ WHAT W-C6 DISTINGUISHES, mechanised. It must
		// be found by the ROUTE limb and by nothing else: the callers-only
		// rival must MISS it, because `handleMusterExport` calls neither
		// closure. With FloorOnlyMustMiss alongside, that pins this control to
		// exactly one limb of the derivation.
		CallersOnlyMustMiss: true,
		Why: "[DOC] W6-falsification-2.md §5.2 grades the domain half's hand-kept four-name root list a live " +
			"instance of \"hand-kept enumerations rot\": \"a future muster read-path entry point that is not in " +
			"the list is invisible to W6-10\". Here the roots are DERIVED from the registered routes, and the " +
			"floor-only reading is run alongside to prove this entry point is exactly what a hand-kept list misses. " +
			"⟦§56⟧ WHAT THIS DISTINGUISHES THAT W-C5 DOES NOT: W-C5's root is `handleMusterPrint`, which is ON " +
			"w610RootFloor — the floor-only reading finds W-C5 unaided, so W-C5 says NOTHING WHATEVER about the " +
			"derivation. This control's root is reachable only because limb (a) reads the route registration; and " +
			"unlike W-C7 it is found by limb (a) ALONE, which the callers-only rival proves.",
	},
	{
		Name:              "W-C7 a muster board assembled with NO route at all",
		NewFile:           "internal/web/w6plant_board.go",
		NewSrc:            w610BoardBuilderSrc,
		WantHit:           true,
		FloorOnlyMustMiss: true,
		// ⟦LOCK — §56, 2026-09-03⟧ WHAT W-C7 DISTINGUISHES, mechanised. It must
		// be found by the CALLER limb and by nothing else: the routes-only
		// rival must MISS it, because it has no route at all.
		//
		// ⚠ THIS IS THE ASSERTION THAT SEPARATES W-C7 FROM W-C6. Before it,
		// both controls proved only "not the hand-kept floor", and the claim
		// that they exercise DIFFERENT limbs was prose the harness never
		// checked — two controls one rename away from being one control.
		RoutesOnlyMustMiss: true,
		Why: "The second derivation, and the one a route walk alone would miss: an entry point that reaches " +
			"`domain.MusterReprint` without being a route. The closures W6-10 governs are the surface, so their " +
			"CALLERS are the read path however they are reached. " +
			"⟦§56⟧ WHAT THIS DISTINGUISHES THAT W-C5 DOES NOT — and that W-C6 does not either: W-C5 rides the " +
			"hand-kept floor; W-C6 rides limb (a), the route registrations. This entry point has NO route, so limb " +
			"(a) cannot see it, and only limb (b) — every caller of MusterLive/MusterReprint — reaches it. The " +
			"routes-only rival is run alongside to PROVE that, so W-C5/W-C6/W-C7 are three different limbs rather " +
			"than one check written out three times.",
	},
	{
		Name:    "W-C8 NEGATIVE — a COMPANY id resolved through the same AliasMap must NOT be flagged",
		Anchor:  "board := domain.MusterLive(",
		Insert:  "\t_ = companyMerge.Resolve(*open[0].CompanySnapshotID)\n",
		WantHit: false,
		Why: "The company grouping DOES resolve, by design (W5d, DR-4) — `domain/muster.go:189` does it inside " +
			"the closure. A scan that cannot tell a person id from a company id on an IDENTICAL receiver type is " +
			"a scan a builder must weaken, which is OW-1's recorded consequence.",
	},
	{
		Name:    "W-C9 NEGATIVE — the same resolve in a helper NOTHING on the muster path reaches",
		NewFile: "internal/web/w6plant_helper.go",
		NewSrc:  w610HelperSrc,
		WantHit: false,
		Why: "Byte-identical to W-C3's helper, planted WITHOUT the call. The only difference is reachability, so " +
			"the pair proves the scoping is live in both directions: unreached code is not flagged, and the walk " +
			"is not ignoring newly added files — which would make this negative arm vacuous, and W-C3 fails if " +
			"it does.",
	},
}

// TestW6TheWebMusterReadPathNeverResolvesThePersonKey — W6-10's `internal/web`
// half.
//
// [DOC] W6-10: "neither closure resolves a person id through `companyMerge`,
// `workgroupAlias`, `LoadCompanyMergeMap`, `LoadPersonMergeMap`,
// `Person.SameAs`, `same_as` or any successor … the scan flags any call whose
// receiver is an `AliasMap`/merge-map value and whose argument derives from
// `v.PersonID`, resolved through `go/types` over `internal/domain` AND
// `internal/web`."
//
// EXPECTED RED at f33c4c7: NONE. Evidence: the ten plants above — eight positive, two negative.
func TestW6TheWebMusterReadPathNeverResolvesThePersonKey(t *testing.T) {
	root := w610ModuleRoot(t)

	// --- THE REAL ASSERTION FIRST, so a production violation is reported
	// --- under W6-10's name and never as an instrument complaint.
	//
	// ⚠ ORDER IS DELIBERATE ⟦W6-falsification-2.md §1.4 / G-4⟧: a control that
	// reds announcing the INSTRUMENT is broken, while the hit list printed
	// below it shows the instrument working, is the exact defect the W6
	// baseline gate found in W6-9(c). The controls below are therefore
	// BASELINE-RELATIVE — each asks whether its plant ADDS a hit — so they
	// keep working, and keep saying what is true, on a tree already in
	// violation.
	live := w610ResolveScan(t, root, w610WebPkg, w610Full)
	w610RequireCalibrated(t, "the tracked tree", live)
	w610RequireCompiles(t, "the tracked tree", live) // ⟦LOCK — §56, 2026-09-03⟧
	if len(live.Hits) != 0 {
		t.Errorf("W6-10, `internal/web` half (m6-10): the person key is resolved through a merge/alias path on "+
			"the muster read path in internal/web:\n  %s\n"+
			"  [[D-50]] consequence 5 / W3-7 forbid person merge resolution ON THE MUSTER, and a caller that "+
			"resolves the key BEFORE handing rows to the closure breaches that from outside the closure, where "+
			"W6-10(i)'s internal/domain walk cannot see it.\n"+
			"  ⚠ [[D-59]]'s \"the muster shows one human\" is a DIFFERENT READ PATH: it governs the exception "+
			"sections' OccupantRef, read from TRIPS; the roster body reads Visit through buildMuster. W6 touches "+
			"only the second and is on the D-50/W3-7 side of that line.\n"+
			"  read path walked: %v",
			strings.Join(live.Hits, "\n  "), live.OnPath)
	}
	// ⟦LOCK — §56, 2026-09-03⟧ the type-error count joins the MEASURED line, so
	// "this scan compiled what it scanned" is a printed fact rather than an
	// assumption the reader has to make.
	// ⟦was: "\t\t\"resolved as %s; roots DERIVED from the surface: %v; %d bodies on the muster read path: %v\","⟧
	t.Logf("W6-10 web half [MEASURED]: %d production files; %d expressions typed from %s; AliasMap.Resolve "+
		"resolved as %s; %d type errors in %s and %d in module-local imports (see w610ScanResult: a nonzero "+
		"import count is the importer's designed fallback, not rot); roots DERIVED from the surface: %v; "+
		"%d bodies on the muster read path: %v",
		live.Files, live.DomainTypedExprs, w610DomainPkg, live.AliasMapMethod,
		len(live.TypeErrors), w610WebPkg, len(live.ImportErrors),
		live.RootsDerived, len(live.OnPath), live.OnPath)

	// --- the control tree: real production source, on disk --------------
	copyRoot := w610CopyProduction(t, root)
	musterOrig, err := os.ReadFile(w610MusterPath(copyRoot))
	if err != nil {
		t.Fatalf("CONTROL SETUP FAILED: %v", err)
	}

	// The copy, unplanted, under all three readings. These are the baselines
	// every plant is measured against.
	base := w610ResolveScan(t, copyRoot, w610WebPkg, w610Full)
	baseNarrowArg := w610ResolveScan(t, copyRoot, w610WebPkg, w610NarrowArg)
	baseFloorOnly := w610ResolveScan(t, copyRoot, w610WebPkg, w610FloorOnly)
	// ⟦LOCK — §56, 2026-09-03⟧ baselines for the two single-limb rivals.
	baseRoutesOnly := w610ResolveScan(t, copyRoot, w610WebPkg, w610RoutesOnly)
	baseCallersOnly := w610ResolveScan(t, copyRoot, w610WebPkg, w610CallersOnly)
	w610RequireCalibrated(t, "the unplanted production copy", base)
	// ⟦LOCK — §56, 2026-09-03⟧ The baseline must compile CLEANLY, or "the errors
	// this plant introduced" is measured against a moving floor and the
	// did-not-compile check below could itself go false-green.
	w610RequireCompiles(t, "the unplanted production copy", base)
	w610RequireCompiles(t, "the unplanted production copy, narrow-argument reading", baseNarrowArg)
	w610RequireCompiles(t, "the unplanted production copy, floor-only reading", baseFloorOnly)
	w610RequireCompiles(t, "the unplanted production copy, routes-only reading", baseRoutesOnly)
	w610RequireCompiles(t, "the unplanted production copy, callers-only reading", baseCallersOnly)
	// ⟦LOCK — §56, 2026-09-03⟧ §61: every rival gets a baseline, and every
	// baseline is held to the same compile bar as the full reading.
	baseRival := map[string]w610ScanResult{
		"narrow-argument": baseNarrowArg,
		"floor-only":      baseFloorOnly,
		"routes-only":     baseRoutesOnly,
		"callers-only":    baseCallersOnly,
	}
	for _, rv := range w610Rivals {
		if _, done := baseRival[rv.Name]; done {
			continue
		}
		r := w610ResolveScan(t, copyRoot, w610WebPkg, rv.Rd)
		w610RequireCompiles(t, "the unplanted production copy, "+rv.Name+" reading", r)
		baseRival[rv.Name] = r
	}
	// signature[control] = the rivals that MISSED it. Filled in the loop below.
	signature := map[string]string{}
	if len(base.Hits) != len(live.Hits) {
		t.Fatalf("INSTRUMENT BROKEN: the production copy reports %d hits where the tracked tree reports %d. The "+
			"copy is supposed to type-check EXACTLY as the tracked tree does; if it does not, no control planted "+
			"in it says anything about the tracked tree.\n  copy: %v\n  tree: %v",
			len(base.Hits), len(live.Hits), base.Hits, live.Hits)
	}

	// --- the plants, each applied ALONE and each revert PROVED ----------
	for _, p := range w610LivePlants {
		w610ApplyPlant(t, copyRoot, p, musterOrig)
		got := w610ResolveScan(t, copyRoot, w610WebPkg, w610Full)
		w610RequireCalibrated(t, p.Name, got)
		// ⟦LOCK — §56, 2026-09-03⟧ ⭐ A PLANT THAT DID NOT COMPILE IS ITS OWN
		// NAMED FAILURE — and it is decided HERE, BEFORE the hit arithmetic,
		// so it can never be reported as a detection failure.
		//
		// ⚠ THIS APPLIES TO EVERY PLANT, positive and negative alike. A dead
		// NEGATIVE control is the worse of the two cases: it adds no hit,
		// which is exactly what the negative arm demands, so it reports
		// SILENT SUCCESS and its whole assertion is vacuous.
		dead := w610Added(got.TypeErrors, base.TypeErrors)
		// ⚠ BOTH streams, because a plant's `NewFile` may land in ANY package
		// under the copy root, not only the scanned one.
		dead = append(dead, w610Added(got.ImportErrors, base.ImportErrors)...)
		// ⭐ ⟦LOCK — §139, 2026-09-03⟧ AND THE THIRD: a plant that does not
		// PARSE. It is the same verdict — the plant did not become a program —
		// arrived at one stage earlier, and it used to be reported by nothing
		// at all: the file was dropped, the plant added no hit, and a positive
		// control read "NOT detected" while a negative control read as a pass.
		dead = append(dead, w610Added(got.ParseErrors, base.ParseErrors)...)
		if len(dead) > 0 {
			t.Errorf("CONTROL DEAD (%s): ⛔ THE PLANT DID NOT COMPILE. This is a FIXTURE defect, and it is NOT "+
				"a finding about the scanner — the scanner was never given a well-typed violation to find.\n"+
				"  type errors this plant introduced:\n    %s\n"+
				"  ⚠ A dead plant produces NO HIT, which is byte-identical to what a BLIND SCANNER produces. "+
				"Before ⟦§56⟧ both printed as \"the planted violation was NOT detected\", which sent the reader "+
				"to debug the one part that was working.\n"+
				"  ⚠ FIX BY RE-AIMING THE PLANT at symbols that exist. Do NOT weaken the scan, and do NOT drop "+
				"the control: an untested positive control is a FAIL, not a skip.\n  %s",
				p.Name, strings.Join(dead, "\n    "), p.Why)
			w610RevertPlant(t, copyRoot, p, musterOrig)
			continue
		}
		switch {
		case p.WantHit && len(got.Hits) <= len(base.Hits):
			t.Errorf("CONTROL FAILED (%s) (m6-10): the planted violation was NOT detected — %d hits, the same as "+
				"the unplanted baseline.\n  %s\n  read path walked: %v\n"+
				"  ⚠ An un-controlled green is indistinguishable from coverage. This check does not pass until "+
				"this plant makes it fail.", p.Name, len(got.Hits), p.Why, got.OnPath)
		case !p.WantHit && len(got.Hits) != len(base.Hits):
			t.Errorf("CONTROL FAILED (%s) (negative arm): a non-violation was flagged.\n  hits: %v\n  baseline: %v\n  %s",
				p.Name, got.Hits, base.Hits, p.Why)
		case p.WantHit:
			t.Logf("CONTROL OK (%s): %v", p.Name, w610Added(got.Hits, base.Hits))
		}
		// ⟦LOCK — §56, 2026-09-03⟧ §61 condition 2. EVERY rival is run against
		// EVERY positive plant, once, and the same measurement serves both the
		// MustMiss assertions below and the distinctness census after the loop.
		// ⚠ Running them only where a flag asks is how an axis with no rival
		// stays unmeasured — which is precisely how W-C2, W-C3 and W-C5 came to
		// assert distinctness that nothing could contradict.
		detected := map[string]bool{}
		added := map[string][]string{}
		if p.WantHit {
			var missed []string
			for _, rv := range w610Rivals {
				n := w610ResolveScan(t, copyRoot, w610WebPkg, rv.Rd)
				added[rv.Name] = w610Added(n.Hits, baseRival[rv.Name].Hits)
				detected[rv.Name] = len(n.Hits) != len(baseRival[rv.Name].Hits)
				if !detected[rv.Name] {
					missed = append(missed, rv.Name)
				}
			}
			signature[w610ControlID(p.Name)] = strings.Join(missed, "+")
			if len(missed) == 0 {
				t.Logf("CONTROL SIGNATURE (%s): NO rival misses it — every narrowed reading finds this plant",
					w610ControlID(p.Name))
			} else {
				t.Logf("CONTROL SIGNATURE (%s): defeated ONLY by %v", w610ControlID(p.Name), missed)
			}
		}
		// ⟦was: "\t\t\tn := w610ResolveScan(t, copyRoot, w610WebPkg, w610NarrowArg)"⟧
		// ⟦was: "\t\t\tif len(n.Hits) != len(baseNarrowArg.Hits) {"⟧
		// ⟦was: "\t\t\t\t\tp.Name, w610Added(n.Hits, baseNarrowArg.Hits), p.Why)"⟧
		if p.NarrowArgMustMiss {
			if detected["narrow-argument"] {
				t.Errorf("CONTROL FAILED (%s): the NARROW-ARGUMENT reading also detected this plant, so it "+
					"re-proves what W-C0 already proved and the widening is uncontrolled: %v\n  %s",
					p.Name, added["narrow-argument"], p.Why)
			}
		}
		// ⟦LOCK — §56, 2026-09-03⟧ the single-limb rivals. FloorOnlyMustMiss
		// below says "not the hand-kept list"; these say WHICH limb, which is
		// what keeps two floor-clearing controls from being one control twice.
		// ⟦was: "\t\t\tn := w610ResolveScan(t, copyRoot, w610WebPkg, w610RoutesOnly)"⟧
		// ⟦was: "\t\t\tif len(n.Hits) != len(baseRoutesOnly.Hits) {"⟧
		if p.RoutesOnlyMustMiss {
			if detected["routes-only"] {
				t.Errorf("CONTROL DEGENERATE (%s): the ROUTES-ONLY reading — limb (a), route registrations "+
					"alone — also detected this plant. This control is supposed to be reachable ONLY through "+
					"limb (b), callers of MusterLive/MusterReprint; if limb (a) finds it too, it re-proves what "+
					"W-C6 already proves and the caller derivation is UNCONTROLLED: %v\n  %s",
					p.Name, added["routes-only"], p.Why)
			}
		}
		// ⟦was: "\t\t\tn := w610ResolveScan(t, copyRoot, w610WebPkg, w610CallersOnly)"⟧
		// ⟦was: "\t\t\tif len(n.Hits) != len(baseCallersOnly.Hits) {"⟧
		if p.CallersOnlyMustMiss {
			if detected["callers-only"] {
				t.Errorf("CONTROL DEGENERATE (%s): the CALLERS-ONLY reading — limb (b) alone — also detected "+
					"this plant. This control is supposed to be reachable ONLY through limb (a), the route "+
					"registrations; if limb (b) finds it too, it re-proves what W-C7 already proves and the "+
					"route derivation is UNCONTROLLED: %v\n  %s",
					p.Name, added["callers-only"], p.Why)
			}
		}
		// ⟦was: "\t\t\tn := w610ResolveScan(t, copyRoot, w610WebPkg, w610FloorOnly)"⟧
		// ⟦was: "\t\t\tif len(n.Hits) != len(baseFloorOnly.Hits) {"⟧
		// ⟦was: "\t\t\t\t\tp.Name, w610Added(n.Hits, baseFloorOnly.Hits), p.Why)"⟧
		// ⟦LOCK — §56, 2026-09-03⟧ §61: the three axes that had no rival.
		if p.SpellReceiverMustMiss && detected["receiver-by-spelling"] {
			t.Errorf("CONTROL DEGENERATE (%s): the RECEIVER-BY-SPELLING rival — match the receiver by NAME "+
				"instead of by resolved type — also detected this plant. This control exists to prove that "+
				"go/types resolution is what finds the receiver; if a name match finds it too, the cross-package "+
				"type resolution is UNCONTROLLED here: %v\n  %s", p.Name, added["receiver-by-spelling"], p.Why)
		}
		if p.NoTransitiveMustMiss && detected["no-transitive"] {
			t.Errorf("CONTROL DEGENERATE (%s): the NO-TRANSITIVE rival — walk the root bodies and nothing they "+
				"call — also detected this plant, so the plant is NOT one call deep and the transitive walk is "+
				"UNCONTROLLED: %v\n  %s", p.Name, added["no-transitive"], p.Why)
		}
		if p.NoByNameMustMiss && detected["no-by-name"] {
			t.Errorf("CONTROL DEGENERATE (%s): the NO-BY-NAME rival — drop the LoadPersonMergeMap limb — also "+
				"detected this plant, so this control is not riding the by-name limb it claims and that limb is "+
				"UNCONTROLLED: %v\n  %s", p.Name, added["no-by-name"], p.Why)
		}
		if p.FloorOnlyMustMiss {
			if detected["floor-only"] {
				t.Errorf("CONTROL FAILED (%s): the FLOOR-ONLY reading — the hand-kept two-name root list — also "+
					"detected this plant, so it proves nothing about the derivation: %v\n  %s",
					p.Name, added["floor-only"], p.Why)
			}
		}
		w610RevertPlant(t, copyRoot, p, musterOrig)
	}

	// --- ⟦LOCK — §56, 2026-09-03⟧ §61 CONDITION 2: THE DISTINCTNESS CENSUS ---
	//
	// ⚠ EIGHT CONTROLS DOING THE WORK OF SEVEN IS A FINDING, NOT A TIDINESS
	// COMPLAINT. Two positive controls whose plants are found and missed by
	// exactly the same rivals are, to this scan, ONE CHECK WRITTEN TWICE — and
	// the suite then reports a coverage it does not have. §61 requires this
	// reported rather than quietly accepted, so it is COMPUTED here rather than
	// claimed in a comment: a hand-kept assertion of distinctness is the same
	// species as the hand-kept root list this whole file exists to replace.
	byPattern := map[string][]string{}
	for id, sig := range signature {
		byPattern[sig] = append(byPattern[sig], id)
	}
	var patterns []string
	for sig := range byPattern {
		patterns = append(patterns, sig)
	}
	sort.Strings(patterns)
	for _, sig := range patterns {
		ids := byPattern[sig]
		sort.Strings(ids)
		what := sig
		if what == "" {
			what = "<no rival misses it>"
		}
		t.Logf("DISTINCTNESS [MEASURED]: %v defeated by %s", ids, what)
		if len(ids) < 2 {
			continue
		}
		key := strings.Join(ids, "|")
		if why, exempt := w610SameCheckExemptions[key]; exempt {
			t.Logf("DISTINCTNESS ⚠ ASSERTED EXEMPTION (%s): %s", key, why)
			continue
		}
		t.Errorf("CONTROLS COLLAPSE (%s) (m6-10): these controls are defeated by exactly the same rivals (%s), "+
			"so NO reading this harness can run tells them apart — they are ONE CHECK WRITTEN %d TIMES and the "+
			"suite reports coverage it does not have ⟦§61 condition 2, standing rule 3⟧.\n"+
			"  Either give one of them an axis the others do not have, or record it in "+
			"w610SameCheckExemptions with a stated reason. ⚠ Do NOT delete a control to silence this: an "+
			"untested positive control is a FAIL, not a skip, and so is a redundant one left unremarked.",
			key, what, len(ids))
	}

	// The copy, clean again, through the same walk — the revert proved by
	// RESULT as well as by bytes.
	after := w610ResolveScan(t, copyRoot, w610WebPkg, w610Full)
	if len(after.Hits) != len(base.Hits) {
		t.Fatalf("CONTROL FAILED: the reverted production copy reports %d hits against a baseline of %d: %v",
			len(after.Hits), len(base.Hits), after.Hits)
	}
}

// w610ControlID is the "W-Cn" prefix of a control's name — the handle the
// distinctness census and the reports both use. ⟦LOCK — §56, 2026-09-03⟧
func w610ControlID(name string) string {
	if i := strings.IndexByte(name, ' '); i > 0 {
		return name[:i]
	}
	return name
}

// w610SameCheckExemptions names PAIRS (or larger groups) of positive controls
// that this scan cannot tell apart, together with the reason each is kept
// anyway. ⟦LOCK — §56, 2026-09-03⟧
//
// ⚠ THIS IS AN ASSERTED ALLOWLIST, NOT A SUPPRESSION. A group named here is
// still reported on every run; what the entry buys is that a NEW collapse is a
// RED while the known ones are a STATED exemption rather than a silent one —
// the same discipline W8-9(c) uses for `duplicate_id`/`canonical_id`.
var w610SameCheckExemptions = map[string]string{
	"W-C0|W-C1": "⚠ REPORTED AT §61, NOT ACCEPTED AS HARMLESS. To this scan W-C0 and W-C1 are the same " +
		"check: receiver-type limb, argument spelled directly, floor root, receiver named directly — every " +
		"rival finds both. Their difference is real but lies OUTSIDE what the scan branches on. W-C0's value " +
		"is PROVENANCE: it reproduces [DOC] W6-falsification-2.md §4.2's escape VERBATIM, so the check is " +
		"measured against the escape it exists to close rather than against a plant of its own choosing. " +
		"W-C1's is SEMANTIC: it is the form that actually mutates the sheet handed to the closure. Neither " +
		"reason is expressible as a rival reading, so neither can be mechanised, and dropping either would " +
		"lose something a reader needs. They are kept as two, and the redundancy is stated here rather than " +
		"discovered again later.",
}

// w610Added is the hits a plant introduced over its baseline — printed so a
// control's evidence is the violation it found, not the number it counted.
func w610Added(got, base []string) []string {
	seen := map[string]bool{}
	for _, b := range base {
		seen[b] = true
	}
	var out []string
	for _, g := range got {
		if !seen[g] {
			out = append(out, g)
		}
	}
	return out
}

// w610RequireCalibrated refuses a result whose green could come from a broken
// instrument rather than from clean code.
//
// ⚠ THIS IS THE POINT OF THE FILE. [DOC] W6-tests.md §7.3: "a green from an
// un-controlled cross-package scan should not be read as coverage." A scan
// whose importer failed reports ZERO HITS and looks exactly like compliant
// code. Every condition below fails as INSTRUMENT BROKEN — never as a
// production red.
func w610RequireCalibrated(t *testing.T, where string, r w610ScanResult) {
	t.Helper()
	if r.Files == 0 {
		t.Fatalf("INSTRUMENT BROKEN (%s): the scan parsed ZERO production files in %s", where, w610WebPkg)
	}
	if len(r.RootsMissing) != 0 {
		t.Fatalf("INSTRUMENT BROKEN (%s): the muster read-path floor is not satisfied: %v.\n"+
			"  A root that no longer exists, or a derivation that no longer finds one, silently empties the scan "+
			"— the filter would then be narrower than its subject by the whole of its subject.", where, r.RootsMissing)
	}
	if len(r.RootsDerived) == 0 {
		t.Fatalf("INSTRUMENT BROKEN (%s): no muster read-path entry point was found in %s at all", where, w610WebPkg)
	}
	if r.AliasMapMethod == "" {
		t.Fatalf("INSTRUMENT BROKEN (%s): %s was not imported with a resolvable `AliasMap.Resolve` method, so NO "+
			"receiver in %s can ever be typed as an AliasMap and this scan CANNOT FAIL.",
			where, w610DomainPkg, w610WebPkg)
	}
	if r.DomainTypedExprs == 0 {
		t.Fatalf("INSTRUMENT BROKEN (%s): not one expression in %s resolved to a %s type. Cross-package "+
			"resolution is dead and a clean pass here would mean nothing.", where, w610WebPkg, w610DomainPkg)
	}
}

// w610RequireCompiles refuses an UNPLANTED result that did not type-check.
//
// ⟦LOCK — §56, 2026-09-03⟧ ⚠ THIS IS THE OTHER HALF OF THE §56 FIX, and it is
// deliberately NOT folded into w610RequireCalibrated: calibration runs on
// PLANTED trees too, where a type error means "this plant is dead" and must be
// reported under that name. This runs only where a type error can mean nothing
// else — the tracked tree and the unplanted copy.
//
// Its job is to hold the floor still. The did-not-compile check is
// baseline-relative (`w610Added(got.TypeErrors, base.TypeErrors)`); if the
// baseline were itself allowed to carry errors, a plant could introduce a
// duplicate of one and be silently forgiven — §56's defect rebuilt one level
// down, out of the very mechanism that repairs it.
func w610RequireCompiles(t *testing.T, where string, r w610ScanResult) {
	t.Helper()
	// ⚠ ONLY the scanned directory's own diagnostics are required to be zero.
	// ImportErrors is deliberately NOT barred here — see w610ScanResult: it is
	// 9 on a healthy tree, every one an artefact of the importer's own
	// designed fallbacks (no build-constraint filtering; synthetic empty
	// packages for non-module imports). Barring it would red this file for the
	// instrument working as specified. Those errors are instead held still
	// baseline-relative, and the calibration conditions above already prove
	// the one thing they could have damaged — that `domain.AliasMap.Resolve`
	// still resolves and that expressions in `internal/web` still carry domain
	// types.
	// ⭐ ⟦LOCK — §139, 2026-09-03⟧ THE PARSE BAR, CHECKED FIRST because it is
	// the earlier stage and the more destructive one: a file that did not
	// type-check is still walked by every syntactic limb; a file that did not
	// PARSE is absent from all of them. Required-zero, and not
	// baseline-relative like ImportErrors, because there is no designed
	// nonzero case — [MEASURED] 0 on the tracked tree and 0 under every plant.
	if len(r.ParseErrors) > 0 {
		t.Fatalf("INSTRUMENT BROKEN (%s): %d file(s) under the scanned tree did NOT PARSE:\n    %s\n"+
			"  ⚠ An unparsed file is not in the corpus AT ALL — no AST, so no limb of this scan sees it, "+
			"syntactic or resolved. The scan's coverage is smaller than its stated scope by exactly those "+
			"files, and a clean pass over the remainder looks identical to a clean pass over the whole.\n"+
			"  ⚠ This is NOT a production red under W6-10. It is the instrument refusing to certify a tree "+
			"it could not read — ⟦§139⟧ the parse door, one stage before ⟦§56⟧'s type door.",
			where, len(r.ParseErrors), strings.Join(r.ParseErrors, "\n    "))
	}
	if len(r.TypeErrors) == 0 {
		return
	}
	shown := r.TypeErrors
	if len(shown) > 20 {
		shown = shown[:20] // the whole list is counted; only the head is printed
	}
	t.Fatalf("INSTRUMENT BROKEN (%s): the scanned tree does NOT type-check — %d error(s):\n    %s\n"+
		"  ⚠ Every hit this scan can produce depends on go/types resolving a receiver to "+
		"`domain.AliasMap`. An expression whose type-checking failed has NO type, so it can never "+
		"match, and the scan would report ZERO HITS while looking exactly like compliant code.\n"+
		"  ⚠ This is NOT a production red under W6-10 and must not be recorded as one. It is the "+
		"instrument refusing to certify a tree it could not read.",
		where, len(r.TypeErrors), strings.Join(shown, "\n    "))
}

// --- ⟦LOCK — §139, 2026-09-03 (S102D)⟧ THE PARSE DOOR'S OWN CONTROL --------
//
// ⭐ §124: BUILT IN, NOT PERFORMED ONCE. The parse guard above was produced by
// hand against an injected broken file; a check whose silence was verified by
// hand decays the moment the mechanism changes, and nothing would say so. This
// test is that verification, made permanent and run on every invocation.
//
// ⭐ §138: IT ALSO ASSERTS THE SIMPLER CONSTRUCT FAILS. The obvious objection
// to recording parse faults is "the scan already refuses an empty file list —
// `if len(files) == 0`, w610ResolveScan:378 — so a lost file is caught". Arm
// (b) below is the counter-example, and without it the repair's own extra
// machinery is the argument for deleting it: the directory holds a file that
// parses AND one that does not, so `len(files)` is NON-ZERO and the simpler
// check passes with a file silently missing from every limb of the scan.
func TestW610AParseFaultIsRecordedAndALengthCheckCannotSubstitute(t *testing.T) {
	// One good file and one broken one, in the package the scan reads.
	root := t.TempDir()
	dir := filepath.Join(root, w610WebPkg)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const good = "package web\n\nfunc handleMusterLive() {}\n"
	const broken = "package web\n\nfunc handleMusterPrint( {\n"
	for name, src := range map[string]string{"ok.go": good, "broken.go": broken} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var parseErrs []string
	files := w610ParsePkg(token.NewFileSet(), dir, &parseErrs)

	// --- arm (a): THE FAULT IS NAMED ------------------------------------
	if len(parseErrs) != 1 || !strings.Contains(parseErrs[0], "broken.go") ||
		!strings.Contains(parseErrs[0], "UNPARSEABLE") {
		t.Fatalf("⟦§139⟧ THE PARSE DOOR IS OPEN AGAIN: a file that does not parse produced %d record(s) %v, "+
			"want exactly one naming broken.go as UNPARSEABLE.\n"+
			"  ⚠ A file that does not parse has NO AST, so NO limb of this scan sees it — not the resolved "+
			"limbs and not the syntactic ones. Its silence is indistinguishable from a scan that correctly "+
			"found nothing, which is ⟦§56⟧'s defect one stage earlier and with a wider blast radius.",
			len(parseErrs), parseErrs)
	}

	// --- arm (b): THE SIMPLER CONSTRUCT DOES NOT FIRE ⟦§138⟧ -------------
	if len(files) == 0 {
		t.Fatalf("⟦§138⟧ CONTROL SETUP BROKEN: this arm needs a directory where SOME file parses, so that a " +
			"`len(files) == 0` check would PASS while a file is missing. Every file failed to parse instead, " +
			"and the arm proves nothing — re-pose the fixture.")
	}
	t.Logf("⟦§138⟧ ANTI-SUBSTITUTION, produced: files parsed = %d (non-zero, so `if len(files) == 0` PASSES "+
		"here) while %d file(s) were silently absent. A length check cannot stand in for the record.", len(files), len(parseErrs))

	// --- arm (c): the `continue` is still RIGHT --------------------------
	// One unparseable file must not cost the scan the rest of the directory.
	// If this ever fails, the repair has over-corrected into a hard stop.
	if len(files) != 1 {
		t.Fatalf("⟦§139⟧ the repair OVER-corrected: %d file(s) parsed, want 1. The `continue` must survive — "+
			"a scan that abandons a directory over one bad file has traded a silent gap for a loud one.", len(files))
	}

	// --- arm (d): the WHOLE-SCAN path carries it too ---------------------
	// The record must reach w610ScanResult, or the bar in w610RequireCompiles
	// has nothing to read.
	res := w610ResolveScan(t, root, w610WebPkg, w610Full)
	if len(res.ParseErrors) == 0 {
		t.Fatalf("⟦§139⟧ the record does not reach the RESULT: w610ParsePkg named the fault but " +
			"w610ScanResult.ParseErrors is empty, so w610RequireCompiles' required-zero bar can never fire " +
			"and the door is open at the level that matters.")
	}

	// --- arm (e): THE BAR ITSELF FIRES ⟦§124⟧ ----------------------------
	// ⚠ Arms (a)–(d) prove the fault is recorded and reaches the result.
	// NONE of them proves the BAR reads it. On a healthy tree ParseErrors is
	// EMPTY, so w610RequireCompiles' new branch is never taken in normal
	// running and would rot unnoticed — which is exactly the decay §124
	// names. This arm takes the branch, against a captured *testing.T so the
	// Fatalf is observed rather than ending this test.
	// ⚠ HONEST LIMIT OF THIS TRICK, stated so it is a choice: a zero-value
	// testing.T is not documented API. If a future Go made Errorf/Failed
	// unusable on one, this goroutine would PANIC — which crashes the test
	// loudly rather than passing it silently, so the failure mode is the safe
	// one. The goroutine is required because Fatalf calls runtime.Goexit.
	captured := &testing.T{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		w610RequireCompiles(captured, "⟦§139⟧ built-in control", res)
	}()
	<-done
	if !captured.Failed() {
		t.Errorf("⟦§139⟧ THE BAR DOES NOT READ THE RECORD: w610RequireCompiles accepted a result carrying "+
			"%d ParseError(s) (%v).\n"+
			"  ⚠ A fault being recorded is worth nothing if nothing refuses on it. This arm exists BECAUSE "+
			"ParseErrors is empty on a healthy tree: the branch is never taken in normal running, so it is "+
			"exactly the kind of check that decays in silence.", len(res.ParseErrors), res.ParseErrors)
	}
}
