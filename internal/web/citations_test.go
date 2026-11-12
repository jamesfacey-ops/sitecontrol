// citations_test.go — V-11.
//
// THE DEFECT CLASS. Prose that asserts the existence of a verification
// mechanism is a load-bearing, wholly unguarded channel in this repository,
// and it was wrong at six sites at once:
//
//	1  U1 carry-forward note 7   "nothing referenced those classes outside
//	                             reports/" — false; two chunks landed on it
//	2  gate.html                 "a small Node harness pulls the source
//	                             between the markers out of THIS file and
//	                             evals it" — no such harness existed
//	3  picker.go                 cited, as the guarantee for the picker's
//	                             safe-default sentence, a test that has
//	                             never existed anywhere in the repository
//	                             (the dead name is not repeated here — see
//	                             picker.go for why)
//	4  muster.html               described the abandoned fixed-position
//	                             sheet-runner design
//	5  _styles.html              "urgency is carried by the STRIPE WEIGHT" —
//	                             the weight does not vary by urgency at all
//	6  demo-asserts.sh           14 lines of selectors the renderer no
//	                             longer emits
//
// Every one reads as coverage to the next person, and that is precisely why
// nobody checked: a comment naming a test IS the reason a reader stops
// looking for one.
//
// Items 2, 3, 4 and 5 have been corrected in place. This test closes item
// 3's SHAPE permanently, which is the only one of the six that is
// mechanically checkable: a comment that names a Test… identifier must name
// one that exists.
//
// SCOPE, and why it is here now rather than deferred. The adjudication put
// a citation checker at Round 3 on scope grounds, and that was right when
// the round began. It is not right at the end of it: this round ADDED
// roughly a dozen new comments citing test names by hand, including in the
// fixes for V-11 itself. Shipping an unchecked citation channel wider than
// the one being repaired would be the same defect one level up — which is
// the exact shape this whole run exists to catch. Twenty-five lines, and it
// verifies the round's own work.
package web

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var citedTestRe = regexp.MustCompile(`\bTest[A-Z][A-Za-z0-9_]{4,}`)

// wasMarkerRe matches ONE ⟦was: … ⟧ record. `(?s)` because a marker routinely wraps
// across lines; `[^⟧]*` stops at the first closing bracket so an unclosed marker
// cannot swallow the rest of a comment block and exempt everything after it.
var wasMarkerRe = regexp.MustCompile(`(?s)⟦was:[^⟧]*⟧`)

func wasMarkerSpans(text string) [][]int { return wasMarkerRe.FindAllStringIndex(text, -1) }

func inAnySpan(i int, spans [][]int) bool {
	for _, sp := range spans {
		if i >= sp[0] && i < sp[1] {
			return true
		}
	}
	return false
}

func TestEveryTestNameCitedInACommentActuallyExists(t *testing.T) {

	// Every test function that really exists, ACROSS THE WHOLE MODULE — a
	// comment here legitimately cites deadline_test.go's or checkin_test.go's
	// tests, and a checker scoped to one package would call those false.
	declared := map[string]bool{}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return nil // a file that does not parse is not this test's business
		}
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") {
				declared[fn.Name.Name] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(declared) < 200 {
		t.Fatalf("precondition: only %d Test functions found across the module — this checker is not looking at the suite", len(declared))
	}

	cited := 0
	recorded := 0
	check := func(where, text string) {
		spans := wasMarkerSpans(text)
		for _, loc := range citedTestRe.FindAllStringIndex(text, -1) {
			name := text[loc[0]:loc[1]]
			// ⟦LOCK — §137, 2026-09-03⟧ A ⟦was:⟧ MARKER IS A RECORD, NOT A CITATION.
			// This guard exists because "a citation that names nothing stops the next reader
			// looking". ⭐ A ⟦was:⟧ marker does the OPPOSITE — it tells the reader, explicitly,
			// that the name is RETIRED and not to go looking. The guard and the §93/§104
			// recording convention agree on the goal; what the guard could not do was read
			// INTENT, so it read a gravestone as a signpost and failed three of them.
			// ⚠ DELIBERATELY NARROW: ONLY `⟦was: … ⟧`. Not ⟦LOCK⟧, not ⟦ROUTED⟧, not
			// ⟦WIDENED⟧, not a bare mention of the word "was". An exemption that widens
			// beyond the thing it was granted for is how a guard goes blind, and a retired
			// name outside a ⟦was:⟧ marker is still a citation and still reds.
			if inAnySpan(loc[0], spans) {
				recorded++
				continue
			}
			cited++
			if !declared[name] {
				t.Errorf("%s cites %s, which does not exist. A comment naming a test is the reason the next reader stops looking for one — so a citation that names nothing is worse than no citation at all. (This is exactly what picker.go did for the picker's safe-default sentence.) If this is a RECORD of a retired name rather than a pointer to a live test, put it inside a ⟦was: \"…\"⟧ marker, which says so explicitly.", where, name)
			}
		}
	}

	// ⟦LOCK — §141, 2026-09-08⟧ EVERY PACKAGE DIRECTORY, NOT JUST THIS ONE.
	// [MEASURED] This loop used to be `ParseDir(".")` — web's own directory
	// alone. With internal/store's and internal/domain's duplicate copies that
	// covered THREE of the module's NINE package directories — nine AT §141;
	// the module is TEN since G15 (2026-09-09) added cmd/dupreport, and this
	// walker reaches all ten — and the other six OF THAT NINE were carrying live
	// ghosts the whole time: internal/config's
	// witness_y13_p8_p9_test.go cited a truncated form of
	// TestWitnessP8NegativeMusterMinGroupSizeIsRefused
	// ⟦was: "TestWitnessP8" — the truncation, corrected in place at §141; never
	// a declaration anywhere ⟧, and internal/repl's appfix_selfhistory_test.go
	// cited a name that has never been declared in any ref. ⚠ A guard whose
	// whole subject is prose asserting coverage it does not have was itself
	// asserting coverage it did not have.
	//
	// ⭐ And this very comment reds without that marker — measured at §141, on
	// the first run of the widened checker. Naming a dead name to explain the
	// defect IS a citation; §137's exemption is deliberately narrow enough to
	// catch its own author.
	//
	// WHY HERE AND NOT SEVEN MORE COPIES ⟦was: "SIX" — arithmetic, not a
	// literal: the module's directories minus the three that hold a copy. Nine
	// minus three was six at §141; ten minus three is SEVEN since G15 added
	// cmd/dupreport. ⛔ A count reached by arithmetic is invisible to a grep for
	// the old number, which is how this one survived two sweeps.⟧. A shared
	// internal/citecheck package
	// makes `go list ./...` report one MORE directory, which redlist_test.go's
	// module-shape guard hard-stops on by design; that was tried and reverted,
	// and store's and domain's copies exist because of it. ⚠ The stop is on an
	// UNDECLARED change of module shape, not on the number ten as such: G15
	// moved the guard to ten deliberately, updating every site at once, which is
	// the sanctioned path this paragraph describes the absence of. Reverting the
	// citecheck package remains right — it bought nothing and cost a package.
	// Widening THIS copy
	// costs no new package and no new copy of the engine: modulePackages is
	// redlist_test.go's walker, already in package web, and it is the same list
	// the module-shape guard reconciles against `go list ./...`.
	//
	// ⚠ COST, stated because an unstated one is how a guard is later "simplified"
	// away: store's and domain's copies now overlap this one, so a ghost in
	// either reds twice. That is duplicate NOISE, never a missed ghost — the
	// safe direction. Their retirement is a separate, reviewable change; two
	// checkers agreeing is not a defect, and deleting a live guard to tidy up is.
	// ⟦LOCK — G15, 2026-09-09⟧ NINE → TEN, deliberately: cmd/dupreport landed
	// as the tenth package. ⚠ This stop was NOT in the site enumeration the G15
	// ruling inherited — that enumeration was written before §141 added it, and
	// it named only redlist_test.go, the deploy script and DECLARED-REDS.md. A
	// count that lives in two executable places is exactly the shape where one
	// gets moved and the other silently does not.
	dirs := modulePackages(t, root)
	if len(dirs) != 11 { // ⟦LOCK — FIX16 H, 2026-10-06⟧ TEN → ELEVEN: internal/siteadmin, moved with redlist_test.go and deploy/redlist-state-check.sh
		t.Fatalf("precondition: %d Go package directories found, expected 11 — this checker walks every one of them, so a package added or removed changes its reach. Same deliberate stop as redlist_test.go's module-shape guard; widening the number here without widening the register there is the silent widening both exist to catch. Found: %v", len(dirs), dirs)
	}
	rels := make([]string, 0, len(dirs))
	for rel := range dirs {
		rels = append(rels, rel)
	}
	sort.Strings(rels) // deterministic failure output; map order is not

	sawWeb := false
	dirsScanned, filesScanned, citedOutsideWeb := 0, 0, 0
	for _, rel := range rels {
		citedBefore := cited
		fset := token.NewFileSet()
		pkgs, err := parser.ParseDir(fset, filepath.Join(root, rel), func(fi fs.FileInfo) bool { return true }, parser.ParseComments)
		if err != nil {
			t.Fatalf("parsing %s: %v", rel, err)
		}
		if len(pkgs) == 0 {
			t.Fatalf("precondition: no packages parsed in %s — modulePackages found a .go file there, so parsing none means this checker walked the directory and read nothing", rel)
		}
		if rel == "internal/web" {
			if _, ok := pkgs["web"]; !ok {
				t.Fatal("precondition: package web not found in internal/web")
			}
			sawWeb = true
		}
		names := make([]string, 0, len(pkgs))
		for name := range pkgs {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			paths := make([]string, 0, len(pkgs[name].Files))
			for path := range pkgs[name].Files {
				paths = append(paths, path)
			}
			sort.Strings(paths)
			for _, path := range paths {
				filesScanned++
				for _, cg := range pkgs[name].Files[path].Comments {
					// ⟦LOCK — §132, 2026-09-03⟧ [MEASURED] this reported the COLUMN, not the line: the old
					// expression took the text after the LAST ":" of "path:line:col", which is the column.
					// It printed "trips.go:2" for a comment block starting at trips.go:1029 — sending a
					// reader to the file header while the citation sat a thousand lines below. ⚠ A guard
					// whose whole subject is signposts that misdirect was itself misdirecting.
					// ⟦LOCK — §141⟧ and the path is now module-relative, not filepath.Base: this walks
					// TEN directories — ⟦was: "nine" — nine AT §141; TEN since G15 (2026-09-09) added
					// cmd/dupreport, and this loop reaches all ten. The count is the only stale half:
					// THREE is still measured (internal/{domain,store,web}), and the reason below is
					// untouched by the move.⟧ — and THREE of them hold a file called citations_test.go,
					// so a bare base name is the same misdirection one level out.
					where := filepath.ToSlash(filepath.Join(rel, filepath.Base(path)))
					check(fmt.Sprintf("%s:%d", where, fset.Position(cg.Pos()).Line), cg.Text())
				}
			}
		}
		dirsScanned++
		if rel != "internal/web" {
			citedOutsideWeb += cited - citedBefore
		}
	}
	if !sawWeb {
		t.Fatal("precondition: internal/web was not among the scanned directories — this checker did not read its own package")
	}
	// ⟦LOCK — §141⟧ THE WIDENING'S OWN LIVENESS. [MEASURED] The module-wide
	// `cited` total cannot detect a regression to web-only scanning: web alone
	// contributes 504 of 1165 (491 in .go comments, the rest in templates), so
	// `cited` would still clear any global floor
	// while eight directories went unread — eight being §141's nine minus web;
	// the same regression today would leave NINE unread, the hazard having grown
	// by one with the module. ⭐ These three count the reach itself —
	// a zero from a method that cannot produce a one is not a zero.
	// ⟦LOCK — G15, 2026-09-09⟧ NINE → TEN. ⛔ THIS IS THE SECOND HARD-CODED
	// COUNT IN THIS FILE, ~80 lines below the first, and it is the one the G15
	// site sweep MISSED: a grep for `!= 9` beside `len(dirs)` found the
	// precondition above and not this one. It was the INSTRUMENT that caught it,
	// reporting "scanned 10 of 9". ⭐ A count that appears twice in one file is
	// the shape where a sweep fixes the occurrence it was looking for and
	// declares the file done.
	if dirsScanned != 11 { // ⟦LOCK — FIX16 H⟧ TEN → ELEVEN, with the precondition above
		t.Fatalf("precondition: scanned %d of 11 package directories — the loop skipped one, so a ghost in it is invisible and this checker would have passed anyway", dirsScanned)
	}
	if filesScanned < 300 {
		t.Fatalf("precondition: only %d .go files read across the module (418 tracked at §141) — this checker is not reading the tree it claims to cover", filesScanned)
	}
	if citedOutsideWeb < 100 {
		t.Fatalf("precondition: only %d citations found OUTSIDE internal/web (661 at §141) — the whole point of §141 is the other NINE directories — eight when §141 was written, nine since G15 added cmd/dupreport — and a web-only scan passes every other check in this test", citedOutsideWeb)
	}

	// And the templates, which carry the same kind of claim in {{/* */}}
	// and /* */ comments — gate.html's vanished Node harness lived in one.
	entries, err := os.ReadDir("templates")
	if err != nil {
		t.Fatalf("reading templates: %v", err)
	}
	tpls := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".html") {
			continue
		}
		b, err := os.ReadFile(filepath.Join("templates", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		tpls++
		check("templates/"+e.Name(), string(b))
	}
	if tpls < 10 {
		t.Fatalf("precondition: only %d templates scanned", tpls)
	}
	if cited < 5 {
		t.Fatalf("precondition: only %d test citations found across the module — this checker would pass by finding nothing to check", cited)
	}
	// ⚠ `recorded` is the exemption's COST, printed every run: an exemption is a
	// reduction in reach, and the honest measure of it is the number it excused.
	// ⚠ `citedOutsideWeb` is the §141 widening's REACH, printed for the same
	// reason: the number that goes to zero if this ever narrows back to one package.
	t.Logf("checked %d test-name citations (%d of them outside internal/web) against %d declared tests, across %d package directories, %d .go files and %d templates (%d further names were ⟦was:⟧ RECORDS, exempt by §137)", cited, citedOutsideWeb, len(declared), dirsScanned, filesScanned, tpls, recorded)
}
