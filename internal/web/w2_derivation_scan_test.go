// w2_derivation_scan_test.go — W2-3: no code path derives `time_source`
// from value equality. Scan + mutant (the scan below; the behavioural kill
// is the dwell fixture in every W2-6 untouched arm — m3 ≡ mTP-4; the
// ledger records the pairing per plan §5's CONTROL rule).
//
// UNIVERSE (the criterion's own, verdict R-9): the authoring path —
// internal/web, internal/domain, internal/store (W1's three-package
// precedent) — targeting write-side derivation of time_source only, with
// EXACTLY ONE named exemption: domain.ProvenanceClass (provenance.go:66),
// the sanctioned READ-side classification — a consumer of stored bytes,
// not a capture mechanism. Written reason, here, once — never a growing
// allowlist (TP §7.1b's discipline).
//
// WHAT COUNTS AS A DERIVATION, made mechanical: an `if`/`switch`-guarded
// branch whose CONDITION compares two time-valued expressions for
// (in)equality (identifiers/selectors matching occurred/authored/WallMS)
// and whose branches produce time-source vocabulary (a "default"/"entered"
// string literal, a timeSource-named identifier, or a Provenance value).
// That is exactly the shape of the forbidden capture derivation AND of the
// one sanctioned read-side classifier — which is why the exemption is
// load-bearing: the scan DOES flag ProvenanceClass and exempts it by name.
// An implementation that evades this shape is caught behaviourally by the
// dwell fixtures (mTP-4); the scan and the mutant are a pair, not
// alternatives.
// (WINDOW-BUILD-PLAN-v1 §3 W2, criteria LOCKED 2026-08-28.)
package web

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// w2TimeValuedRe matches the names that carry a per-event instant on the
// authoring path.
var w2TimeValuedRe = regexp.MustCompile(`(?i)^(occurred|authored)|^WallMS$`)

// w2SourceVocabRe matches time-source vocabulary identifiers.
var w2SourceVocabRe = regexp.MustCompile(`(?i)(timesource|time_source|^Provenance)`)

// w2TheOneExemption is W2-3's single named exemption: the read-side
// classifier. A fixed pair, not a growing allowlist.
var w2TheOneExemption = struct{ file, fn string }{"provenance.go", "ProvenanceClass"}

type w2DerivationHit struct {
	File string
	Line int
	Func string
}

func w2ExprIsTimeValued(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.Ident:
		return w2TimeValuedRe.MatchString(v.Name)
	case *ast.SelectorExpr:
		return w2TimeValuedRe.MatchString(v.Sel.Name)
	case *ast.ParenExpr:
		return w2ExprIsTimeValued(v.X)
	}
	return false
}

// w2CondComparesTimes reports an ==/!= between two time-valued operands
// anywhere inside a condition expression.
func w2CondComparesTimes(cond ast.Expr) bool {
	found := false
	ast.Inspect(cond, func(n ast.Node) bool {
		if b, ok := n.(*ast.BinaryExpr); ok && (b.Op == token.EQL || b.Op == token.NEQ) {
			if w2ExprIsTimeValued(b.X) && w2ExprIsTimeValued(b.Y) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// w2NodeCarriesSourceVocab reports time-source vocabulary in a subtree.
func w2NodeCarriesSourceVocab(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(m ast.Node) bool {
		switch v := m.(type) {
		case *ast.BasicLit:
			if v.Kind == token.STRING && (strings.Contains(v.Value, `"default"`) || strings.Contains(v.Value, `"entered"`) || v.Value == `"default"` || v.Value == `"entered"`) {
				found = true
				return false
			}
		case *ast.Ident:
			if w2SourceVocabRe.MatchString(v.Name) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// w2DerivationHits scans one Go source file for the forbidden shape.
func w2DerivationHits(t *testing.T, path string, src []byte) []w2DerivationHit {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var hits []w2DerivationHit
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.IfStmt:
				if v.Cond != nil && w2CondComparesTimes(v.Cond) {
					if w2NodeCarriesSourceVocab(v.Body) || (v.Else != nil && w2NodeCarriesSourceVocab(v.Else)) {
						hits = append(hits, w2DerivationHit{File: path, Line: fset.Position(v.Pos()).Line, Func: fn.Name.Name})
					}
				}
			case *ast.SwitchStmt:
				if v.Tag != nil && w2CondComparesTimes(v.Tag) && w2NodeCarriesSourceVocab(v.Body) {
					hits = append(hits, w2DerivationHit{File: path, Line: fset.Position(v.Pos()).Line, Func: fn.Name.Name})
				}
			}
			return true
		})
	}
	return hits
}

// w2ScanDirsForDerivation runs the scan over every non-test .go file in
// the given dirs, applying the one named exemption.
func w2ScanDirsForDerivation(t *testing.T, dirs ...string) []w2DerivationHit {
	t.Helper()
	var out []w2DerivationHit
	for _, dir := range dirs {
		paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range paths {
			if strings.HasSuffix(p, "_test.go") {
				continue
			}
			src, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			for _, h := range w2DerivationHits(t, p, src) {
				if filepath.Base(h.File) == w2TheOneExemption.file && h.Func == w2TheOneExemption.fn {
					continue // THE one sanctioned read-side classifier
				}
				out = append(out, h)
			}
		}
	}
	return out
}

func TestW2NoWriteSideDerivationOfTimeSourceFromValueEquality(t *testing.T) {
	root := w2ModuleRoot(t)
	hits := w2ScanDirsForDerivation(t,
		filepath.Join(root, "internal", "web"),
		filepath.Join(root, "internal", "domain"),
		filepath.Join(root, "internal", "store"),
	)
	for _, h := range hits {
		t.Errorf("W2-3 forbidden derivation: %s:%d in %s — time_source is set by the UI interaction and read from the POST; it is NEVER computed by comparing values, at either end (TP §4.5; mutant m3/mTP-4)", h.File, h.Line, h.Func)
	}
	if len(hits) != 0 {
		t.Fatal("W2-3: equality-derivation of time_source found on the authoring path")
	}
}

// Positive control (plan §5 CONTROL row, paired with m3): the scan must
// detect a planted derivation placed IN the scan's named universe — the
// scan is handed a copied package directory holding the plant, never the
// plant's own path.
func TestW2DerivationScanControlDetectsPlantedDerivation(t *testing.T) {
	root := w2ModuleRoot(t)
	tmp := t.TempDir()
	// A copied slice of the real universe (internal/domain), plant added.
	real, err := filepath.Glob(filepath.Join(root, "internal", "domain", "*.go"))
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
	plant := []byte(`package domain

// planted fixture: the literal m3 shape — the equality-derivation of
// time_source the criterion forbids.
func plantedDeriveTimeSource(occurredAtMS, authoredAtMS int64) string {
	if occurredAtMS == authoredAtMS {
		return "default"
	}
	return "entered"
}
`)
	if err := os.WriteFile(filepath.Join(tmp, "zz_planted_derivation.go"), plant, 0o644); err != nil {
		t.Fatal(err)
	}
	hits := w2ScanDirsForDerivation(t, tmp)
	for _, h := range hits {
		if h.Func == "plantedDeriveTimeSource" {
			return // failure mode produced
		}
	}
	t.Fatalf("CONTROL FAILED: the scan did not detect a planted equality-derivation in its own universe — its clean pass counts for nothing. Hits: %v", hits)
}

// The exemption is LOAD-BEARING, and that is asserted rather than assumed:
// the scan's detector DOES flag ProvenanceClass's shape (the sanctioned
// read-side classifier), and only the named exemption keeps it out of the
// clean run. If a refactor moves or renames the classifier, this goes red
// and the exemption is re-pointed at a criteria lock — never silently
// widened.
func TestW2DerivationScanExemptionIsLoadBearing(t *testing.T) {
	root := w2ModuleRoot(t)
	src, err := os.ReadFile(filepath.Join(root, "internal", "domain", w2TheOneExemption.file))
	if err != nil {
		t.Fatal(err)
	}
	hits := w2DerivationHits(t, w2TheOneExemption.file, src)
	for _, h := range hits {
		if h.Func == w2TheOneExemption.fn {
			return
		}
	}
	t.Fatalf("the detector no longer flags %s.%s — the one named exemption exempts nothing, so either the detector went blind (and the control above should have caught it) or the classifier moved; re-point the exemption at a criteria lock", w2TheOneExemption.file, w2TheOneExemption.fn)
}
