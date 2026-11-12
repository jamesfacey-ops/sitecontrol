// render_test.go — M-15's guard. Two halves, and they are different kinds
// of assertion on purpose:
//
//  1. renderPage itself must never ship a half page. That is BEHAVIOUR and
//     is measured by making a real render fail after it has already
//     produced output.
//  2. Every handler must go through renderPage. That is a STRUCTURAL
//     property of the package and is checked by PARSING it — see the note
//     on the second test for why that is not the substring-blacklist
//     mechanism this repo has ruled against elsewhere.
package web

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// halfPageData renders some output and THEN fails, which is the shape that
// matters: a template that fails before writing anything would be caught by
// any implementation.
type halfPageData struct{ Site string }

func (halfPageData) Boom() (string, error) {
	return "", errors.New("simulated mid-render failure")
}

const halfPageTemplate = `<!doctype html><html><body>
<h1>Muster ({{.Site}})</h1>
<p class="tally-num">17</p>
<section class="roster">{{.Boom}}</section>
</body></html>`

func TestRenderPageNeverShipsAHalfPageUnderA200(t *testing.T) {
	set := template.Must(template.New("zz_half_page_test").Parse(halfPageTemplate))

	// Precondition, and it is the whole reason this test exists: the OLD
	// mechanism really does emit a truncated page under a 200. Measured
	// here rather than asserted about, so the fix below is proved against
	// the actual failure rather than a description of it.
	direct := httptest.NewRecorder()
	err := set.ExecuteTemplate(direct, "zz_half_page_test", halfPageData{Site: "site-a"})
	if err == nil {
		t.Fatal("precondition: the fixture template did not fail, so nothing below is being tested")
	}
	if direct.Code != http.StatusOK {
		t.Fatalf("precondition: streaming render returned %d, expected the silent 200 that makes this dangerous", direct.Code)
	}
	if !strings.Contains(direct.Body.String(), "Muster (site-a)") {
		t.Fatal("precondition: streaming render wrote nothing before failing — this fixture is not reproducing the mid-stream case")
	}
	if strings.Contains(direct.Body.String(), "</html>") {
		t.Fatal("precondition: streaming render somehow produced a complete page")
	}

	// The fix: same template, same failure, through renderPage.
	srv := &Server{}
	rec := httptest.NewRecorder()
	srv.renderInto(rec, "half page", set, "zz_half_page_test", halfPageData{Site: "site-a"})

	if rec.Code == http.StatusOK {
		t.Fatalf("a failed render returned 200. The muster's failure shape is a sheet that reads as complete down to the headcount and then stops, with the print link live — the locked guard is that the muster must NEVER be silently short. Body was:\n%s", rec.Body.String())
	}
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("a failed render returned %d, want 500", rec.Code)
	}
	// And not one byte of the half page may have escaped.
	for _, leaked := range []string{"Muster (site-a)", "tally-num", "17"} {
		if strings.Contains(rec.Body.String(), leaked) {
			t.Errorf("the failed render leaked %q into the response — a partial roster under any status code is the thing this prevents. Body was:\n%s", leaked, rec.Body.String())
		}
	}
	// The refusal must say what to do with it, like every other refusal.
	if !strings.Contains(rec.Body.String(), "do not rely on it") {
		t.Errorf("the 500 does not tell the reader the page is not a complete record: %q", rec.Body.String())
	}

	// The success path must still be a normal, complete page.
	okRec := httptest.NewRecorder()
	srv.renderInto(okRec, "half page", set, "zz_half_page_test", struct {
		Site string
		Boom string
	}{Site: "site-a", Boom: "everyone"})
	if okRec.Code != http.StatusOK {
		t.Fatalf("a successful render returned %d", okRec.Code)
	}
	if !strings.Contains(okRec.Body.String(), "</html>") {
		t.Error("a successful render did not produce a complete page")
	}
	if ct := okRec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
}

// TestNoHandlerStreamsATemplateDirectlyToTheResponseWriter.
//
// WHY THIS IS A PARSE AND NOT A SUBSTRING SCAN. There are eight render
// sites and all eight had the identical defect; fixing two would have left
// six. The property "no handler streams" cannot be observed from outside
// without engineering a mid-render failure on each of the eight routes
// separately, which is eight fixtures for one rule.
//
// So it is checked structurally — but by PARSING the package with go/ast,
// not by grepping it. That distinction is the same one the print guards
// turn on (printcss_test.go): a substring blacklist is defeated by
// whitespace, by renaming, or by moving the call one line away, whereas a
// parse sees the call expression whatever it looks like. What is asserted
// is a real syntactic fact about the package: no call passes an
// http.ResponseWriter to ExecuteTemplate.
func TestNoHandlerStreamsATemplateDirectlyToTheResponseWriter(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing the package: %v", err)
	}
	pkg, ok := pkgs["web"]
	if !ok {
		t.Fatal("precondition: package web not found by the parser")
	}

	// Collect, per function, the names of its http.ResponseWriter
	// parameters — so this follows the TYPE rather than assuming the
	// parameter is always called w.
	sites, writers := 0, 0
	for path, file := range pkg.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Type.Params == nil {
				return true
			}
			rw := map[string]bool{}
			for _, p := range fn.Type.Params.List {
				sel, ok := p.Type.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "ResponseWriter" {
					continue
				}
				if x, ok := sel.X.(*ast.Ident); !ok || x.Name != "http" {
					continue
				}
				for _, name := range p.Names {
					rw[name.Name] = true
				}
			}
			if len(rw) == 0 {
				return true
			}
			writers++
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) == 0 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "ExecuteTemplate" {
					return true
				}
				sites++
				arg, ok := call.Args[0].(*ast.Ident)
				if !ok || !rw[arg.Name] {
					return true
				}
				t.Errorf("%s: %s streams a template straight to the response writer (%s.ExecuteTemplate(%s, ...)). html/template writes as it renders, so a failure part-way through delivers HTTP 200 and a page that simply STOPS — on the muster that is a sheet reading as complete down to the headcount, roster truncated, print link live. Render through s.renderPage instead.",
					fset.Position(call.Pos()), fn.Name.Name, sel.X, arg.Name)
				return true
			})
			return true
		})
		_ = path
	}
	if writers < 8 {
		t.Fatalf("precondition: found only %d functions taking an http.ResponseWriter — this sweep is not looking at the handlers", writers)
	}
	// renderPage itself is the ONE place ExecuteTemplate may be called, and
	// it must still be there: a version of this test that passed because
	// nothing renders at all would be worthless.
	if sites == 0 {
		t.Fatal("precondition: no ExecuteTemplate call found in any handler-shaped function — this test would pass trivially")
	}
}
