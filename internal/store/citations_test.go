// citations_test.go — repair-queue row X. Extends
// TestEveryTestNameCitedInACommentActuallyExists (internal/web's V-11
// ghost detector — see that file for the full defect-class history) to
// internal/store, which the original detector never covered: it only ever
// parsed the `web` package's own directory.
//
// ⚠ SUPERSEDED IN PART — §141, 2026-09-08. The sentence above is history,
// not present tense: web's copy now walks EVERY package directory
// (modulePackages, redlist_test.go's walker, already in package web — no new
// package, no fourth copy of the engine). It was widened because the three
// copies covered three of nine directories and two live ghosts sat outside
// them. ⟦was: "ALL NINE" / "three of nine" — nine was the module at §141;
// G15 (2026-09-09) landed cmd/dupreport as the tenth. The walker is derived
// and needed no edit; the hard-coded preconditions it feeds did.⟧
// ⭐ THIS COPY IS THEREFORE REDUNDANT for .go comments and now reds in
// parallel with web's — duplicate NOISE, never a missed ghost. It is kept
// deliberately: deleting a live guard to tidy up is a separate, reviewable
// change, and two checkers agreeing is not a defect.
//
// This is a DUPLICATE of the scanning engine, not a shared import. A
// shared `internal/citecheck` package was tried first and reverted: it
// makes `go list ./...` report one MORE package directory, which
// redlist_test.go's own module-shape guard
// (`⟦was: TestTheDeclaredRedRegisterNamesOnlyTestsThatExist — not in the public repository⟧`) hard-stops on by
// design — "a package added or removed changes what the register must
// cover, so this is a deliberate stop rather than a silent widening".
// Editing that guard's expected count SILENTLY
// is the exact widening it exists to catch, so the fix is here
// instead: no new package, three small copies of one ~90-line walk.
//
// ⭐ G15, 2026-09-09 — WHAT THAT SENTENCE LEFT OUT. The guard bans DRIFT,
// not growth. Moving the count deliberately, with every assertion site
// moved in the SAME change, is a different act and is sanctioned — that is
// how cmd/dupreport became the tenth package. What stays banned is editing
// the number to make a red go away. The citecheck revert was still correct
// on its own merits: it bought nothing and cost a package.
//
// ⛔ internal/store splits into TWO packages in one directory: `store` (38
// files) and the external test package `store_test` (23 files — the
// w10_desk_documents suite and most of the w5–w9 source-scan tests). A
// checker that looks up only `pkgs["store"]` — the naive port of web's own
// pattern, which never needed this because web has no such split — would
// read 38 files and silently skip the other 23. checkDir below walks
// EVERY package ParseDir returns for the directory for exactly this
// reason: 5 of this package's 12 ghosts (measured before repair) lived in
// store_test.
package store

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var storeCitedTestRe = regexp.MustCompile(`\bTest[A-Z][A-Za-z0-9_]{4,}`)

// storeWasMarkerRe matches ONE ⟦was: … ⟧ record — see
// internal/web/citations_test.go's §137 lock comment for why a retirement
// record is not a citation. `(?s)` because a marker routinely wraps across
// lines; `[^⟧]*` stops at the first closing bracket so an unclosed marker
// cannot swallow the rest of a comment block and exempt everything after it.
var storeWasMarkerRe = regexp.MustCompile(`(?s)⟦was:[^⟧]*⟧`)

func storeWasMarkerSpans(text string) [][]int { return storeWasMarkerRe.FindAllStringIndex(text, -1) }

func storeInAnySpan(i int, spans [][]int) bool {
	for _, sp := range spans {
		if i >= sp[0] && i < sp[1] {
			return true
		}
	}
	return false
}

// storeDeclaredTests walks moduleRoot for every real
// `func TestXxx(t *testing.T)` across the WHOLE MODULE, not just this
// package — a comment in internal/store legitimately cites a test declared
// in internal/domain or internal/web, and a checker scoped to one package
// would call that a ghost.
func storeDeclaredTests(moduleRoot string) (map[string]bool, error) {
	declared := map[string]bool{}
	err := filepath.WalkDir(moduleRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, ferr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if ferr != nil {
			return nil // a file that does not parse is not this checker's business
		}
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") {
				declared[fn.Name.Name] = true
			}
		}
		return nil
	})
	return declared, err
}

// storeCheckDir parses every .go file in dir — across EVERY package
// ParseDir finds there, per the trap in the file doc above — and fails t
// for any comment that cites a Test... name absent from declared, unless
// the citation sits inside a ⟦was: …⟧ retirement marker (recorded, not
// cited). Returns the cited/recorded counts so the caller can enforce its
// own "found nothing to check" precondition.
func storeCheckDir(t *testing.T, dir string, declared map[string]bool) (cited, recorded int) {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi fs.FileInfo) bool { return true }, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing %s: %v", dir, err)
	}
	if len(pkgs) == 0 {
		t.Fatalf("precondition: no packages found in %s", dir)
	}

	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			for _, cg := range file.Comments {
				where := fmt.Sprintf("%s:%d", filepath.Base(path), fset.Position(cg.Pos()).Line)
				text := cg.Text()
				spans := storeWasMarkerSpans(text)
				for _, loc := range storeCitedTestRe.FindAllStringIndex(text, -1) {
					name := text[loc[0]:loc[1]]
					if storeInAnySpan(loc[0], spans) {
						recorded++
						continue
					}
					cited++
					if !declared[name] {
						t.Errorf("%s cites %s, which does not exist. A comment naming a test is the reason the next reader stops looking for one — so a citation that names nothing is worse than no citation at all. If this is a RECORD of a retired name rather than a pointer to a live test, put it inside a ⟦was: \"…\"⟧ marker, which says so explicitly.", where, name)
					}
				}
			}
		}
	}
	return cited, recorded
}

func TestEveryTestNameCitedInACommentActuallyExists(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	declared, err := storeDeclaredTests(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(declared) < 200 {
		t.Fatalf("precondition: only %d Test functions found across the module — this checker is not looking at the suite", len(declared))
	}

	cited, recorded := storeCheckDir(t, ".", declared)
	if cited < 5 {
		t.Fatalf("precondition: only %d test citations found across the package — this checker would pass by finding nothing to check", cited)
	}
	t.Logf("checked %d test-name citations against %d declared tests (%d further names were ⟦was:⟧ RECORDS, exempt per §137)", cited, len(declared), recorded)
}
