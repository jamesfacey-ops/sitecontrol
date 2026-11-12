// w1_departure_lead_test.go — the guard on W1-5's DepartureLead exemption
// (FIX16 integration, lead ruling 2026-10-06; James 2026-10-06: overdue runs
// from the typed time). DepartureLead may compare one event's own typed time
// to its own recorded time; it may never be the means of ordering or
// comparing two events. Mechanically, over all production Go under internal/
// and cmd/:
//   - DepartureLead takes exactly one store.Envelope and nothing else, so it
//     cannot see a second event;
//   - DepartureLead itself is called only inside domain/departure.go (the two
//     functions that wrap it), so the exemption cannot spread;
//   - no call to DepartureLead, EffectiveDeparture or DispatchDeparture sits
//     inside a sort/slices call's arguments, or directly as an operand of a
//     comparison operator (<, <=, >, >=, ==, !=) or of a .Before/.After/
//     .Equal/.Compare method call.
//
// A planted ordering of two envelopes by EffectiveDeparture is the positive
// control: the detector must flag it before the real pass means anything.
package store

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var w1DepartureFuncs = map[string]bool{"DepartureLead": true, "EffectiveDeparture": true, "DispatchDeparture": true}

type w1DepHit struct {
	File   string
	Line   int
	Detail string
}

// w1DepCallName is the called function's bare name ("" when not a plain or
// package-qualified call).
func w1DepCallName(e ast.Expr) string {
	c, ok := e.(*ast.CallExpr)
	if !ok {
		return ""
	}
	switch f := c.Fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

// w1DepContains reports whether n contains a call to a departure function.
func w1DepContains(n ast.Node) (string, bool) {
	found := ""
	ast.Inspect(n, func(x ast.Node) bool {
		if e, ok := x.(ast.Expr); ok && w1DepartureFuncs[w1DepCallName(e)] {
			found = w1DepCallName(e)
			return false
		}
		return found == ""
	})
	return found, found != ""
}

// w1DepIsResult reports whether e is a departure call or a selector/method
// chain rooted in one (EffectiveDeparture(env).UnixMilli()).
func w1DepIsResult(e ast.Expr) bool {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
		case *ast.CallExpr:
			if w1DepartureFuncs[w1DepCallName(x)] {
				return true
			}
			sel, ok := x.Fun.(*ast.SelectorExpr)
			if !ok {
				return false
			}
			e = sel.X
		case *ast.SelectorExpr:
			e = x.X
		default:
			return false
		}
	}
}

func w1DepScan(t *testing.T, path string, src []byte) []w1DepHit {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var hits []w1DepHit
	add := func(n ast.Node, d string) { hits = append(hits, w1DepHit{path, fset.Position(n.Pos()).Line, d}) }
	inDeparture := filepath.Base(path) == "departure.go" && filepath.Base(filepath.Dir(path)) == "domain"
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncDecl:
			if x.Name.Name == "DepartureLead" && x.Recv == nil {
				ps := x.Type.Params.List
				if len(ps) != 1 || len(ps[0].Names) > 1 {
					add(x, "DepartureLead must take exactly one store.Envelope")
				} else if sel, ok := ps[0].Type.(*ast.SelectorExpr); !ok || sel.Sel.Name != "Envelope" {
					add(x, "DepartureLead's one parameter must be a store.Envelope")
				}
			}
		case *ast.CallExpr:
			name := w1DepCallName(x)
			if name == "DepartureLead" && !inDeparture {
				add(x, "DepartureLead called outside domain/departure.go — the W1-5 exemption does not travel")
			}
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
				if pkg, ok := sel.X.(*ast.Ident); ok && (pkg.Name == "sort" || pkg.Name == "slices") {
					for _, a := range x.Args {
						if fn, ok := w1DepContains(a); ok {
							add(x, fn+" inside "+pkg.Name+"."+sel.Sel.Name+" — the typed time orders nothing")
						}
					}
				}
				switch sel.Sel.Name {
				case "Before", "After", "Equal", "Compare":
					if len(x.Args) == 1 && w1DepIsResult(sel.X) && w1DepIsResult(x.Args[0]) {
						add(x, "two departures compared with ."+sel.Sel.Name)
					}
				}
			}
		case *ast.BinaryExpr:
			switch x.Op {
			case token.LSS, token.LEQ, token.GTR, token.GEQ, token.EQL, token.NEQ:
				if w1DepIsResult(x.X) || w1DepIsResult(x.Y) {
					add(x, "a departure call is an operand of "+x.Op.String())
				}
			}
		}
		return true
	})
	return hits
}

func TestW1DepartureLeadNeverOrdersEvents(t *testing.T) {
	// --- CONTROL FIRST: each forbidden shape is detected.
	plants := map[string]string{
		"sort": `package p
import ("sort"; "sitecontrol/internal/domain")
func f(evs []E) { sort.Slice(evs, func(i, j int) bool { return domain.EffectiveDeparture(evs[i]).UnixMilli() < 0 }) }`,
		"binary": `package p
import "sitecontrol/internal/domain"
func f(a, b E) bool { return domain.EffectiveDeparture(a).UnixMilli() < domain.EffectiveDeparture(b).UnixMilli() }`,
		"before": `package p
import "sitecontrol/internal/domain"
func f(a, b E) bool { return domain.EffectiveDeparture(a).Before(domain.EffectiveDeparture(b)) }`,
		"lead-elsewhere": `package p
import "sitecontrol/internal/domain"
func f(a E) int64 { return int64(domain.DepartureLead(a)) }`,
		"two-envelopes": `package domain
func DepartureLead(a, b store.Envelope) int64 { return 0 }`,
	}
	for name, src := range plants {
		if hits := w1DepScan(t, "planted/"+name+".go", []byte(src)); len(hits) == 0 {
			t.Fatalf("CONTROL FAILED: the planted %q shape was not detected — the real pass below would prove nothing", name)
		}
	}
	clean := `package p
import "sitecontrol/internal/domain"
func f(a E) int64 { due := domain.EffectiveDeparture(a).UnixMilli() + 60_000; return due }`
	if hits := w1DepScan(t, "planted/clean.go", []byte(clean)); len(hits) != 0 {
		t.Fatalf("CONTROL: a plain due-time computation was flagged: %v", hits)
	}

	// --- the real pass over production Go.
	root := w1ModuleRoot(t)
	var hits []w1DepHit
	for _, top := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			hits = append(hits, w1DepScan(t, p, src)...)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, h := range hits {
		t.Errorf("W1-5 DepartureLead exemption: %s:%d — %s", h.File, h.Line, h.Detail)
	}
}
