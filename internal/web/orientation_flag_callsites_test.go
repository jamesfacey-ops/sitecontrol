// orientation_flag_callsites_test.go — every production door that authors an
// arrival carries THIS SITE's orientation flag, and a new one that forgets
// fails HERE, by name, instead of entering everyone at an orientation site.
//
// THE RULE (James, 2026-09-09; design/REQUIREMENTS-FROM-JAMES-2026-09-09.md
// R-9): nobody is on site until oriented. It is enforced in ONE place —
// buildArrivalVisits (domain/tripauthor.go) — and reaches it through a flag
// every arrival author must be HANDED: hasOrientation. Three of the five
// authors take it as a trailing VARIADIC (RecordExpectedArrival,
// RecordExpectedArrivalExcluding, ResumeCheckIn) and GateCheckIn takes it as
// a struct field (GateCheckInInput.HasOrientation). In both shapes the
// omission is silent and reads as FALSE — road-post behaviour, everyone
// enters — which is exactly how the two gate doors stood open for three
// weeks (MEASURED at f7afc3a, reports/witness/ORIENTATION-ENFORCEMENT.md).
// 7ee99f4 closed them and each door got its own behavioural test; the
// reviewer then MEASURED that all six production call sites pass the flag
// and named the residue (reports/witness/REVIEW-ORIENT-BLOCK.md §3): "the
// guard is caller discipline: no test scans call sites, so a future door
// that omits the flag enters everyone silently."
//
// ⚠ THE PATTERN, NOT JUST THIS INSTANCE. This is a guard ARMED BY ITS
// CALLERS rather than by the thing it guards: the safe behaviour depends on
// every entry point remembering to switch it on. The same shape was found
// independently the same day in the cross-site ownership refusal
// (reports/witness/G13-TEST.md, REVIEW-G13.md Q1): refuseForeignRow is
// inert until domain.Register has run, so it is armed by cmd/sitectl's
// startup order, not by the store — sound today because exactly one binary
// authors and it registers first, and "a structural fact today, not a
// guarantee for tomorrow." Whenever a default is the UNSAFE direction, the
// property is only as good as the census of callers; this file is that
// census for the orientation flag.
//
// WHY A CENSUS AND NOT A REQUIRED PARAMETER. Inverting the three variadics
// to a required `hasOrientation bool` would make the omission a compile
// error — strictly stronger — but (a) it cannot reach the busiest door,
// GateCheckIn, whose flag is a struct field no signature can make required;
// (b) it cannot reach RecordUnsolicitedArrival, the road-post-only wrapper
// that hardcodes false and is callable from anywhere; and (c) at 9a614cd the
// three variadics have ~85 test call sites across 39 files, a diff that
// lands squarely on three sibling branches in flight. The census covers all
// five authors, is confined to this file, and makes the mistake LOUD; the
// inversion remains the right follow-up once the siblings land, and this
// test keeps passing through it (a present argument satisfies both shapes).
//
// MECHANISM. go/parser over every non-test .go file under the module root;
// every call to one of the named authors is located, its enclosing function
// named, and the flag's position DERIVED from the author's own declaration
// (never a hand-counted arity — w7_authoring_doors_test.go says why). A call
// is clean when it passes an argument in that position that is not the
// literal false; a GateCheckIn call is clean when every GateCheckInInput
// literal in the same function sets HasOrientation to something other than
// false. Anything else is a finding, and a finding is a failure unless it
// is written down in orientationFlagExemptions with a reason. A stale
// exemption also fails, so the list cannot rot.
//
// ⚠ A FORWARDED SPREAD IS CLEAN ONLY INSIDE A NAMED AUTHOR, AND ONLY WHEN
// IT IS THAT AUTHOR'S OWN hasOrientation PARAMETER. The first cut of this
// file accepted any `x...` in the flag's position, and the reviewer showed
// four shapes that pass it while reading false at runtime
// (reports/witness/REVIEW-GUARDS.md): `nil...`; `var none []bool; none...`;
// `[]bool{false}...`; and — the one that matters — a Server method that
// forwards its own `hasOrientation...` to an author, whose CALLERS the
// census never visited, so a caller of the wrapper that omits the flag was
// invisible. "This is the natural shape of the next door in internal/web."
// So: a wrapper that forwards the flag IS a door, and it must be NAMED in
// orientAuthorNames (declared anywhere in the module — the census reads
// declarations module-wide) so that its callers are censused too. A spread
// from anywhere else is a finding that says so.
//
// WHAT IT CATCHES (each proven by a planted control below, or by the
// reviewer's compiled mutants, REVIEW-GUARDS.md): the variadic omitted; a
// spread of nil, an empty slice, a local, or a non-author's parameter; a
// GateCheckInInput literal without the field, or with false; a literal
// false; a call to the road-post-only wrapper; an author taken as a
// function value (`arrive := domain.RecordExpectedArrival`) or called
// through parentheses; a call inside a `go` statement, a closure, or a
// build-tagged file; an aliased or dot import; an input built in another
// function (fails as "cannot see" — loud, never silent).
//
// WHAT IT WOULD MISS: a flag that is PRESENT but WRONG — a peer's flag, the
// origin site's, a config field that reads false at an orientation post;
// the named author's own `hasOrientation` parameter shadowed or reassigned
// before it is forwarded (the spread rule reads the NAME, not the binding);
// a method on some other type that shares an author's bare name is MATCHED
// (a false positive, exemptable, not a miss); and the enforcement itself —
// buildArrivalVisits ignoring the flag it is handed. The behavioural door
// tests in gate_orientation_block_test.go and step5_wire_test.go cover
// those for the doors that exist today.
//
// ⚠ EXEMPTION GRANULARITY IS file#function#author, NOT per call. A second
// offending call of the same author inside an exempt function rides under
// the one entry. Keep exempt functions small, and read them in review.
package web

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// orientationFlagExemptions is the WRITTEN opt-out. Key:
// "<module-relative file>#<enclosing function>#<author called>". Every entry
// must match a live finding or the test fails (stale exemptions are how an
// allowlist becomes a blindfold). A new caller that must legitimately run
// road-post behaviour at every site goes here, with the reason, in review.
var orientationFlagExemptions = map[string]string{
	"internal/domain/tripauthor.go#RecordUnsolicitedArrival#RecordUnsolicitedArrivalOriented": "the road-post-only wrapper itself: RecordUnsolicitedArrival exists for callers that owe no orientation anywhere and passes false by construction; it has no production caller at 9a614cd (the unsolicited door calls RecordUnsolicitedArrivalOriented), and any future production call to it is a finding of its own, not covered by this entry",
}

// orientAuthorShape is one author's flag as declared.
type orientAuthorShape struct {
	Name     string
	Kind     string // "variadic", "required", "struct", "none"
	FlagPos  int    // flattened parameter index of hasOrientation (variadic/required)
	Declared string // "<file>:<line>" of the declaration, "" until found
}

// orientAuthorNames are the arrival authors — the five, plus the wrapper
// that hardcodes the flag false (a production call to it is itself the
// finding). ⚠ A function that FORWARDS hasOrientation to any of these is a
// door too and belongs on this list, so its own callers are censused; the
// spread rule refuses a forward from anything not named here. Matched by
// bare name (function or method), declared anywhere in the module.
var orientAuthorNames = []string{
	"RecordExpectedArrival",
	"RecordExpectedArrivalExcluding",
	"ResumeCheckIn",
	"ResumeCheckInCompleting",
	"RecordUnsolicitedArrivalOriented",
	"GateCheckIn",
	"RecordUnsolicitedArrival",
}

// orientNoProductionCaller names the flag-bearing authors that are declared
// doors with, deliberately, no production caller — WRITTEN, with the reason,
// so the blindness check below still fires for every other door. An entry
// whose author gains a production caller is stale and fails: that caller is
// then censused like any other, and the entry would only hide a later loss
// of sight.
var orientNoProductionCaller = map[string]string{
	"ResumeCheckIn": "the resume door's one production call moved to ResumeCheckInCompleting at cd1d0ef (A3 / H-6: the tick rides inside the door, behind its refusals); ResumeCheckIn stays declared for callers that tick nothing and is still censused — its callers are only in tests at 6a3a0fa. The resume door's sight is proven by ResumeCheckInCompleting's own located call",
}

// orientStructDoor is the author whose flag is a struct field; orientRoadPost
// the one whose calls are findings outright.
const (
	orientStructDoor = "GateCheckIn"
	orientRoadPost   = "RecordUnsolicitedArrival"
	orientFlagParam  = "hasOrientation"
	orientInputType  = "GateCheckInInput"
	orientInputField = "HasOrientation"
)

func orientIsAuthor(name string) bool {
	for _, n := range orientAuthorNames {
		if n == name {
			return true
		}
	}
	return false
}

// orientVariadicFlagParam returns the name of fn's own trailing variadic
// hasOrientation parameter, or "" when it has none.
func orientVariadicFlagParam(fn *ast.FuncDecl) string {
	if fn == nil || fn.Type.Params == nil {
		return ""
	}
	for _, field := range fn.Type.Params.List {
		if _, ok := field.Type.(*ast.Ellipsis); !ok {
			continue
		}
		for _, nm := range field.Names {
			if nm.Name == orientFlagParam {
				return nm.Name
			}
		}
	}
	return ""
}

// orientDeclaredShapes reads the authors' declarations from every non-test
// file in dirs and derives each flag's shape and position. A name declared
// twice is fatal: the census matches by bare name and cannot tell them
// apart.
func orientDeclaredShapes(t *testing.T, root string, dirs []string) map[string]orientAuthorShape {
	t.Helper()
	shapes := map[string]orientAuthorShape{}
	for _, n := range orientAuthorNames {
		shapes[n] = orientAuthorShape{Name: n, Kind: "none", FlagPos: -1}
	}
	structHasField := false
	for _, p := range orientSourceFiles(t, dirs) {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		for _, d := range f.Decls {
			switch decl := d.(type) {
			case *ast.FuncDecl:
				sh, want := shapes[decl.Name.Name]
				if !want {
					continue
				}
				at := fmt.Sprintf("%s:%d", orientDisplayPath(root, p), fset.Position(decl.Pos()).Line)
				if sh.Declared != "" {
					t.Fatalf("author %s is declared twice (%s and %s) — the census matches by bare name; rename one", decl.Name.Name, sh.Declared, at)
				}
				sh.Declared = at
				idx := 0
				for _, field := range decl.Type.Params.List {
					if len(field.Names) == 0 {
						idx++
						continue
					}
					for _, nm := range field.Names {
						if nm.Name == orientFlagParam {
							sh.FlagPos = idx
							if _, ok := field.Type.(*ast.Ellipsis); ok {
								sh.Kind = "variadic"
							} else {
								sh.Kind = "required"
							}
						}
						idx++
					}
				}
				if decl.Name.Name == orientStructDoor {
					sh.Kind = "struct"
				}
				shapes[decl.Name.Name] = sh
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok || ts.Name.Name != orientInputType {
						continue
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok {
						continue
					}
					for _, field := range st.Fields.List {
						for _, nm := range field.Names {
							if nm.Name == orientInputField {
								structHasField = true
							}
						}
					}
				}
			}
		}
	}
	for _, n := range orientAuthorNames {
		sh := shapes[n]
		if sh.Declared == "" {
			t.Fatalf("author %s is declared nowhere in the module — the census cannot see a door it cannot name; if the author was renamed, rename it in orientAuthorNames", n)
		}
		switch n {
		case orientStructDoor:
			if !structHasField {
				t.Fatalf("%s no longer declares %s — the orientation rule has lost its carrier on the gate door", orientInputType, orientInputField)
			}
		case orientRoadPost:
			if sh.Kind != "none" {
				t.Fatalf("%s now declares %s — it is no longer the road-post-only wrapper; move it to the flag-bearing set in this file", n, orientFlagParam)
			}
		default:
			if sh.Kind != "variadic" && sh.Kind != "required" {
				t.Fatalf("author %s (declared at %s) declares no %s parameter — the orientation rule has lost its carrier on this door", n, sh.Declared, orientFlagParam)
			}
		}
	}
	return shapes
}

// orientSourceFiles lists the non-test .go files of dirs, sorted.
func orientSourceFiles(t *testing.T, dirs []string) []string {
	t.Helper()
	var out []string
	for _, dir := range dirs {
		paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range paths {
			if !strings.HasSuffix(p, "_test.go") {
				out = append(out, p)
			}
		}
	}
	sort.Strings(out)
	return out
}

// orientDisplayPath is module-relative with forward slashes, or the base
// name for a file outside the module (the control's copied tree).
func orientDisplayPath(root, p string) string {
	if rel, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.Base(p)
}

// orientCallSite is one located use of an author: a call, or a bare name.
type orientCallSite struct {
	File    string
	Line    int
	Func    string // enclosing function, "<package level>" outside one
	Author  string
	Problem string // empty when the flag is carried
}

func (c orientCallSite) key() string { return c.File + "#" + c.Func + "#" + c.Author }

func (c orientCallSite) String() string {
	return fmt.Sprintf("%s:%d %s → %s", c.File, c.Line, c.Func, c.Author)
}

// orientUnparen strips any parentheses around an expression:
// `(domain.RecordExpectedArrival)(…)` is a call of the author.
func orientUnparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

func orientName(e ast.Expr) string {
	switch x := orientUnparen(e).(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	}
	return ""
}

func orientRecvName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.StarExpr:
		return orientRecvName(x.X)
	case *ast.Ident:
		return x.Name
	}
	return "?"
}

func orientIsFalse(e ast.Expr) bool {
	id, ok := orientUnparen(e).(*ast.Ident)
	return ok && id.Name == "false"
}

// orientCensus locates every use of an author in the non-test files of the
// given directories and judges each against its declared shape. It takes
// ONLY directories — the control hands it a copied tree carrying a plant
// this file never names.
func orientCensus(t *testing.T, root string, shapes map[string]orientAuthorShape, dirs []string) []orientCallSite {
	t.Helper()
	var sites []orientCallSite
	for _, p := range orientSourceFiles(t, dirs) {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		display := orientDisplayPath(root, p)
		for _, d := range f.Decls {
			encl := "<package level>"
			var fn *ast.FuncDecl
			var body ast.Node = d
			if fd, ok := d.(*ast.FuncDecl); ok {
				fn = fd
				encl = fd.Name.Name
				if fd.Recv != nil && len(fd.Recv.List) == 1 {
					encl = orientRecvName(fd.Recv.List[0].Type) + "." + encl
				}
				if fd.Body == nil {
					continue
				}
				body = fd.Body
			}
			// callee marks the expressions that ARE a call's function, and
			// the Sel idents of author selectors already judged, so a bare
			// author name is reported exactly when it is NOT being called.
			callee := map[ast.Node]bool{}
			ast.Inspect(body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.CallExpr:
					fun := orientUnparen(x.Fun)
					author := orientName(fun)
					callee[fun] = true
					if sel, ok := fun.(*ast.SelectorExpr); ok {
						callee[sel.Sel] = true
					}
					sh, want := shapes[author]
					if !want {
						return true
					}
					sites = append(sites, orientCallSite{File: display, Line: fset.Position(x.Pos()).Line, Func: encl, Author: author,
						Problem: orientJudge(sh, x, fn, body, fset)})
				case *ast.SelectorExpr:
					if !orientIsAuthor(x.Sel.Name) {
						return true
					}
					callee[x.Sel] = true // the Sel ident is this selector's, judged here
					if !callee[x] {
						sites = append(sites, orientNotACall(display, fset.Position(x.Pos()).Line, encl, x.Sel.Name))
					}
				case *ast.Ident:
					if orientIsAuthor(x.Name) && !callee[x] {
						sites = append(sites, orientNotACall(display, fset.Position(x.Pos()).Line, encl, x.Name))
					}
				}
				return true
			})
		}
	}
	return sites
}

func orientNotACall(file string, line int, encl, author string) orientCallSite {
	return orientCallSite{File: file, Line: line, Func: encl, Author: author,
		Problem: "names " + author + " without calling it — a function value (`arrive := domain." + author + "`) is a door this census cannot follow to its call; call the author directly, or write an exemption"}
}

// orientJudge returns the problem with one call, or "" when it carries the
// flag. The wording names what is wrong and what to do.
func orientJudge(sh orientAuthorShape, call *ast.CallExpr, fn *ast.FuncDecl, encl ast.Node, fset *token.FileSet) string {
	flagLiteralFalse := "passes the literal false as " + orientFlagParam + " to " + sh.Name + " — road-post behaviour at every site; pass this post's own config flag, or write an exemption"
	switch sh.Kind {
	case "none":
		return "calls " + sh.Name + ", the road-post-only wrapper that hardcodes " + orientFlagParam + "=false — everyone enters at an orientation site; call " + sh.Name + "Oriented with this post's own flag, or write an exemption"
	case "variadic":
		n := len(call.Args)
		if call.Ellipsis.IsValid() && n == sh.FlagPos+1 {
			// A spread is clean ONLY when this function is itself a named
			// author forwarding its OWN hasOrientation parameter. nil, an
			// empty slice, a local, or an unnamed wrapper's parameter all
			// read as false at runtime, and an unnamed wrapper's callers
			// are never censused.
			own := orientVariadicFlagParam(fn)
			operand, isIdent := orientUnparen(call.Args[sh.FlagPos]).(*ast.Ident)
			if fn != nil && orientIsAuthor(fn.Name.Name) && own != "" && isIdent && operand.Name == own {
				return ""
			}
			return "forwards a spread as " + orientFlagParam + " to " + sh.Name + " from a function that is not a named author forwarding its own parameter — nil, an empty slice, a local, or a wrapper's argument all read as false at EVERY site, and a wrapper's callers are never censused; this function is a door: add it to orientAuthorNames with its own " + orientFlagParam + " ...bool parameter, or pass this post's own config flag here, or write an exemption"
		}
		if n <= sh.FlagPos {
			return "omits the trailing " + orientFlagParam + " argument to " + sh.Name + " — the variadic's zero length reads as false at EVERY site, so this door enters the un-oriented at an orientation post (fail-open); pass this post's own config flag, or write an exemption"
		}
		if orientIsFalse(call.Args[sh.FlagPos]) {
			return flagLiteralFalse
		}
		return ""
	case "required":
		if len(call.Args) > sh.FlagPos && orientIsFalse(call.Args[sh.FlagPos]) {
			return flagLiteralFalse
		}
		return ""
	case "struct":
		var lits []*ast.CompositeLit
		ast.Inspect(encl, func(n ast.Node) bool {
			cl, ok := n.(*ast.CompositeLit)
			if ok && orientName(cl.Type) == orientInputType {
				lits = append(lits, cl)
			}
			return true
		})
		if len(lits) == 0 {
			return "calls " + sh.Name + " with a " + orientInputType + " built in another function — this census cannot see whether " + orientInputField + " is set (the zero value is road-post behaviour, fail-open); build the input in this function with " + orientInputField + " set, or write an exemption"
		}
		var problems []string
		for _, cl := range lits {
			set := false
			for _, el := range cl.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if k, ok := kv.Key.(*ast.Ident); ok && k.Name == orientInputField {
					set = true
					if orientIsFalse(kv.Value) {
						problems = append(problems, fmt.Sprintf("the %s literal at line %d sets %s: false — road-post behaviour at every site", orientInputType, fset.Position(cl.Pos()).Line, orientInputField))
					}
				}
			}
			if !set {
				problems = append(problems, fmt.Sprintf("the %s literal at line %d carries no %s — the zero value is road-post behaviour, so this door enters the un-oriented at an orientation post (fail-open); set it to this post's own config flag, or write an exemption", orientInputType, fset.Position(cl.Pos()).Line, orientInputField))
			}
		}
		return strings.Join(problems, "; ")
	}
	return "author of unknown shape " + sh.Kind
}

// orientModuleDirs lists every directory under the module root that holds a
// non-test .go file — internal/, cmd/, and whatever a future binary adds.
func orientModuleDirs(t *testing.T, root string) []string {
	t.Helper()
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if p != root && (strings.HasPrefix(name, ".") || name == "testdata" || name == "node_modules" || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			seen[filepath.Dir(p)] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for d := range seen {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	return dirs
}

// TestEveryArrivalDoorCarriesThisSitesOrientationFlag is the census over the
// real tree. GREEN at 9a614cd: every production call passes the flag and the
// one hardcoded false is written down.
func TestEveryArrivalDoorCarriesThisSitesOrientationFlag(t *testing.T) {
	root := w2ModuleRoot(t)
	dirs := orientModuleDirs(t, root)
	shapes := orientDeclaredShapes(t, root, dirs)
	sites := orientCensus(t, root, shapes, dirs)

	// The census must SEE the doors that exist, or its silence means nothing.
	seen := map[string]int{}
	for _, s := range sites {
		seen[s.Author]++
	}
	for _, n := range orientAuthorNames {
		if n == orientRoadPost {
			continue // no production caller is the correct count here
		}
		if reason, ok := orientNoProductionCaller[n]; ok {
			if seen[n] != 0 {
				t.Errorf("stale no-production-caller entry for %s (%s): the census located %d production call(s) — remove the entry", n, reason, seen[n])
			}
			continue
		}
		if seen[n] == 0 {
			t.Fatalf("the census located no production call of %s — the scan is blind, not the tree clean (a door that exists and is not found here is exactly the failure this file guards against)", n)
		}
	}

	used := map[string]bool{}
	for _, s := range sites {
		if s.Problem == "" {
			t.Logf("carries the flag: %s", s)
			continue
		}
		if reason, ok := orientationFlagExemptions[s.key()]; ok {
			used[s.key()] = true
			t.Logf("EXEMPT (written): %s — %s", s, reason)
			continue
		}
		t.Errorf("ORIENTATION FAIL-OPEN at %s: %s", s, s.Problem)
	}
	for k, reason := range orientationFlagExemptions {
		if !used[k] {
			t.Errorf("stale exemption %q (%s) matches no finding — remove it, or the list is a blindfold", k, reason)
		}
	}
}

// TestOrientationFlagCensusControlDetectsPlantedDoors proves the instrument
// before its green counts (w2_route_walk_test.go's control posture): a copy
// of the real web package plus one planted file carrying every omission
// shape — including the reviewer's four (REVIEW-GUARDS.md) — handed to the
// census as a DIRECTORY, never by name. The declared shapes still come from
// the real module. The plant need only PARSE; nothing compiles it.
func TestOrientationFlagCensusControlDetectsPlantedDoors(t *testing.T) {
	root := w2ModuleRoot(t)
	shapes := orientDeclaredShapes(t, root, orientModuleDirs(t, root))
	tmp := t.TempDir()
	real, err := filepath.Glob(filepath.Join(root, "internal", "web", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range real {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmp, filepath.Base(p)), src, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	plant := []byte(`package web

import "sitecontrol/internal/domain"

// planted fixture: doors that forget the flag, and two that remember.
func plantedForgetsVariadic(s *Server) {
	_, _ = domain.RecordExpectedArrival(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, "trip", nil, 0, "default")
}
func plantedForgetsResume(s *Server) {
	_, _ = domain.ResumeCheckIn(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, "trip", nil, 0, "default")
}
func plantedLiteralFalse(s *Server) {
	_, _ = domain.RecordUnsolicitedArrivalOriented(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, "", "site-a", nil, nil, 0, "default", false)
}
func plantedStructWithoutField(s *Server) {
	in := domain.GateCheckInInput{VehicleID: "v"}
	_, _, _ = domain.GateCheckIn(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, in, 0, "default")
}
func plantedRoadPostWrapper(s *Server) {
	_, _ = domain.RecordUnsolicitedArrival(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, "", "site-a", nil, nil, 0, "default")
}
func plantedNilSpread(s *Server) {
	_, _ = domain.RecordExpectedArrival(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, "trip", nil, 0, "default", nil...)
}
func plantedEmptySliceSpread(s *Server) {
	var none []bool
	_, _ = domain.ResumeCheckIn(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, "trip", nil, 0, "default", none...)
}
func (s *Server) plantedUnnamedWrapper(tripID string, hasOrientation ...bool) error {
	_, err := domain.ResumeCheckIn(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, tripID, nil, 0, "default", hasOrientation...)
	return err
}
func plantedFuncValue(s *Server) {
	arrive := domain.RecordExpectedArrival
	_, _ = arrive(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, "trip", nil, 0, "default")
}
func plantedParenCallee(s *Server) {
	_, _ = (domain.RecordExpectedArrival)(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, "trip", nil, 0, "default")
}
func plantedCompletingLiteralFalse(s *Server) {
	_, _ = domain.ResumeCheckInCompleting(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, "trip", nil, nil, 0, "default", false)
}
func plantedRemembers(s *Server) {
	_, _ = domain.RecordExpectedArrival(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, "trip", nil, 0, "default", s.Cfg.HasOrientation())
}
func ResumeCheckIn(s *Server, tripID string, hasOrientation ...bool) error {
	// a NAMED author forwarding its OWN parameter: the one clean spread
	_, err := domain.RecordExpectedArrival(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, tripID, nil, 0, "default", hasOrientation...)
	return err
}
`)
	if err := os.WriteFile(filepath.Join(tmp, "zz_planted_door.go"), plant, 0o644); err != nil {
		t.Fatal(err)
	}

	sites := orientCensus(t, root, shapes, []string{tmp})
	want := map[string]string{
		"plantedForgetsVariadic":        "omits the trailing hasOrientation argument to RecordExpectedArrival",
		"plantedForgetsResume":          "omits the trailing hasOrientation argument to ResumeCheckIn",
		"plantedLiteralFalse":           "passes the literal false as hasOrientation to RecordUnsolicitedArrivalOriented",
		"plantedStructWithoutField":     "carries no HasOrientation",
		"plantedRoadPostWrapper":        "the road-post-only wrapper",
		"plantedNilSpread":              "forwards a spread as hasOrientation to RecordExpectedArrival from a function that is not a named author",
		"plantedEmptySliceSpread":       "forwards a spread as hasOrientation to ResumeCheckIn from a function that is not a named author",
		"Server.plantedUnnamedWrapper":  "forwards a spread as hasOrientation to ResumeCheckIn from a function that is not a named author",
		"plantedFuncValue":              "names RecordExpectedArrival without calling it",
		"plantedParenCallee":            "omits the trailing hasOrientation argument to RecordExpectedArrival",
		"plantedCompletingLiteralFalse": "passes the literal false as hasOrientation to ResumeCheckInCompleting",
	}
	got := map[string]string{}
	for _, s := range sites {
		if s.File != "zz_planted_door.go" {
			if s.Problem != "" {
				t.Errorf("the copied real tree produced a finding the census over the real tree does not: %s: %s", s, s.Problem)
			}
			continue
		}
		got[s.Func] = s.Problem
	}
	for fn, frag := range want {
		p, ok := got[fn]
		if !ok {
			t.Errorf("CONTROL: the census never located the planted use in %s — it cannot be trusted to find a real one", fn)
			continue
		}
		if !strings.Contains(p, frag) {
			t.Errorf("CONTROL: the planted use in %s was judged %q, want a problem containing %q", fn, p, frag)
		}
	}
	for _, clean := range []string{"plantedRemembers", "ResumeCheckIn"} {
		if p, ok := got[clean]; !ok || p != "" {
			t.Errorf("CONTROL: the planted call in %s that carries the flag was judged %q (found=%v), want clean — a census that flags a correct door will be silenced with exemptions", clean, p, ok)
		}
	}
}
