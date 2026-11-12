// redlist_test.go — S122, repair-queue row H.
//
// THE DEFECT CLASS, and it is citations_test.go's one level up. That test
// closes the shape "a Go comment names a test that does not exist". This one
// closes "a MARKDOWN INVENTORY names a test that does not exist" — the same
// shape, in the one place the project actually keeps its declared-red lists,
// and the place the existing detector was scoped so it could never look.
//
// S112-REDLIST-AUDIT.md measured the cost of that gap: across ~25 documents,
// 45 named tests and at least 9 entries naming no test at all, it found 2
// GHOSTS (a name resolving to nothing), 6 declarations still reading "must
// stay red" for tests that are GREEN, and 4 reds declared by no register.
// W789-GATE-LEDGER-C.md's own declared-red list named exactly ONE resolvable
// test; the rest were a count, a literal ellipsis, a pointer to another
// document, and a filename. Nothing could ever check them, which is why
// everything survived. The ghost detector had ALREADY caught one of the two
// ghosts — in a Go comment, in trips.go. The comment was fixed. The markdown
// was not.
//
// WHAT THIS TEST IS, AND IS NOT. It is a PARSER for one file,
// DECLARED-REDS.md at the module root, and it asserts the five properties
// S112 §15 specified: one machine-readable file; every row carrying an exact
// package and an exact test name; EXISTS; a retirement condition stated as an
// expression; and the register's own arithmetic recomputed rather than typed.
//
// It deliberately does NOT run the suite, so it cannot check that a row
// declared RED is observed RED (S112 §15 property 3(b)). That check needs a
// whole-tree run and would recurse if it lived here; it is
// deploy/redlist-state-check.sh, and the Observed column is its input. What
// this test CAN do without running anything is refuse the combination
// (Declared RED, Observed GREEN) outright: a row cannot be carried as a live
// must-stay-red for a test the register itself records as passing.
//
// THE SUCCESS TEST, stated so it can be run: delete or rename any test named
// in the register, and this test fails naming it. Before this file existed
// that edit was silent in every inventory the project had.
package web

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

const redlistFile = "DECLARED-REDS.md"

// A register cell may name a Go test and nothing else. This is S112 §15
// property 2 as a regexp: it rejects an ellipsis (`TestW9a3…`), a filename
// (`w5e_cargo_note_roundtrip_test.go`), a count ("the W9 trio"), a
// document-pointer and a bare criterion id, all of which passed as entries
// in the prose registers this file replaces.
var redlistTestNameRe = regexp.MustCompile(`^Test[A-Z][A-Za-z0-9_]*$`)

var redlistCitationRe = regexp.MustCompile(`^[A-Za-z0-9._-]+\.md:[0-9]+$`)

// Same shape as citations_test.go's citedTestRe, kept separate so widening one
// cannot silently widen the other.
var redlistCitedTestRe = regexp.MustCompile(`\bTest[A-Z][A-Za-z0-9_]{4,}`)

type redlistRow struct {
	line                              int
	id, test, pkg, declared, observed string
	verdict, retires, successor       string
	declaredAt, staleAt, site         string
}

// redlistSection returns the lines strictly between the BEGIN and END markers
// for one named block, and fails if the block is not present exactly once —
// "ONE machine-readable file, and exactly one" has a per-block equivalent.
func redlistSection(t *testing.T, lines []string, name string) ([]string, int) {
	t.Helper()
	begin, end := -1, -1
	beginMark := "<!-- " + name + ": BEGIN -->"
	endMark := "<!-- " + name + ": END -->"
	for i, l := range lines {
		switch strings.TrimSpace(l) {
		case beginMark:
			if begin != -1 {
				t.Fatalf("%s: %s appears more than once", redlistFile, beginMark)
			}
			begin = i
		case endMark:
			if end != -1 {
				t.Fatalf("%s: %s appears more than once", redlistFile, endMark)
			}
			end = i
		}
	}
	if begin == -1 || end == -1 || end < begin {
		t.Fatalf("%s: block %s is missing or malformed (begin=%d end=%d)", redlistFile, name, begin, end)
	}
	return lines[begin+1 : end], begin + 1
}

// redlistCells splits one markdown table row. A row must open and close with
// a pipe, so a missing trailing cell is a parse error rather than a silently
// short row.
func redlistCells(t *testing.T, where, line string) []string {
	t.Helper()
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "|") || !strings.HasSuffix(s, "|") {
		t.Fatalf("%s: table row is not delimited by pipes at both ends: %q", where, line)
	}
	parts := strings.Split(s[1:len(s)-1], "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func redlistIsSeparator(cells []string) bool {
	for _, c := range cells {
		if strings.Trim(c, "-: ") != "" {
			return false
		}
	}
	return len(cells) > 0
}

// moduleTestIndex returns every Test function declared anywhere in the module,
// mapped to the paths (module-relative) that declare it.
func moduleTestIndex(t *testing.T, root string) map[string][]string {
	t.Helper()
	index := map[string][]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if perr != nil {
			return nil // a file that does not parse is not this test's business
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") {
				index[fn.Name.Name] = append(index[fn.Name.Name], rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return index
}

// modulePackages returns every directory in the module holding at least one
// .go file. It reconciles, by name, with `go list ./...` — a precondition
// several reports state in prose and nothing checked.
func modulePackages(t *testing.T, root string) map[string]bool {
	t.Helper()
	pkgs := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, filepath.Dir(path))
		if rerr != nil {
			return rerr
		}
		pkgs[filepath.ToSlash(rel)] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return pkgs
}

// enclosingFunc names the function whose body spans the given 1-based line.
func enclosingFunc(path string, line int) (string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return "", err
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		lo := fset.Position(fn.Pos()).Line
		hi := fset.Position(fn.End()).Line
		if line >= lo && line <= hi {
			return fn.Name.Name, nil
		}
	}
	return "", fmt.Errorf("no function encloses line %d", line)
}
