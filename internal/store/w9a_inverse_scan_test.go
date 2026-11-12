// w9a_inverse_scan_test.go — W9a-8 (T-scan): F4 §9.3's INVERSE source scan.
//
// The claim: no `Effective*` identifier and no `trip_amendment` /
// `arrival_amendment` reference inside `internal/domain`'s three RECOMPUTE
// paths — `computeStates`, `recomputeTripStates` and the F28 scoped
// recompute `recomputeTripStatesScoped`. C-1's regression is an *effective*
// read *inside* the domain package, exactly where the OUTWARD scan cannot
// look.
//
// ⚠ W9a-8 IS MARKED ⟦NO PRE-CHUNK RED⟧ IN THE LOCKED CRITERIA, AND THE
// REASON IS THAT ITS SUBJECT DOES NOT EXIST. On the pre-chunk tree
// `grep -ra 'EffectiveTrip\|EffectiveArrival' --include=*.go app/` and
// `grep -ra 'trip_amendment\|arrival_amendment' --include=*.go app/` both
// return zero. **The scan passes on arrival; its clean green is worth
// nothing and, booked as a pass, credits work nobody did.** Its ledger
// evidence is the TWO PLANTED CONTROLS plus T-scan's meta-control — never a
// clean pass. Every one of those is produced HERE, on every run.
//
// ⚠ rule 20: `go test -overlay` does NOT reach a source scan. Every control
// below is a REAL on-disk scratch tree written by the test itself and handed
// to the SAME walk the clean pass uses — never a hand-fed detector call.
//
// ⚠ WHY TWO PLANTS AND NOT ONE (W9R2-FA2-10): the scan polices TWO classes.
// An identifier-grep implementation passes the `Effective*` plant while
// staying green forever over a raw `JOIN arrival_amendment`. Until the
// second plant existed the table-reference half was UNPROVEN under the
// standing rule that a check is invalid until its failure mode has been
// produced.
//
// ⚠ THE FILTER IS SPECIFIED, NOT ITS AMBITION. "No table reference anywhere
// in these paths" must reach every way a table name can be written, so the
// scan checks (a) each string literal, (b) each `+`-concatenated chain's
// joined value, and (c) the function's whole concatenated literal stream —
// (c) is what sees `"arrival_" + "amendment"` written apart. It also follows
// package-local CALLS out of the three roots, so a recompute path cannot
// launder an effective read through a helper.
//
// ⚠ WHAT THIS SCAN DOES NOT REACH, stated rather than implied (F4 §9.3 says
// the same of itself): it polices only these three paths. "A raw
// `trip_occupant` read living inside `internal/domain` and outside those
// three paths is invisible to both scans by construction" — and §9.2's
// consumers 3, 6, 7 and 8 sit exactly there. NEITHER SCAN SUBSTITUTES FOR
// PER-CONSUMER OBSERVATION, which is W9a-2's job, not this file's. It also
// follows only plain-identifier calls, so an effective read reached through
// a METHOD value on a domain type is outside its filter; recorded here
// because an unstated limit is the defect this project keeps finding.
//
// ⚠ W9a-8 IS THE ONLY STRUCTURAL ENFORCEMENT ROUND 1'S C-1 CRITICAL HAS.
//
// ⚠ V-4 of reports/W9-R3-VERIFICATION.md: the OUTWARD scan carries a build
// obligation and a ledger obligation but NO acceptance criterion — W9a-8 and
// T-scan are inverse-only. This file is therefore inverse-only too. That is
// the criteria's shape, not an omission of mine; it is reported in
// reports/window-build/W9a-tests.md.
package store_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// w9aRecomputeRoots are F4 §9.3's three named paths, by name. A rename must
// make this scan FAIL LOUDLY rather than scan an empty set — a scan whose
// subject has silently vanished is the greenest possible green.
var w9aRecomputeRoots = []string{"computeStates", "recomputeTripStates", "recomputeTripStatesScoped"}

// w9aPolicedTables is the second policed class.
var w9aPolicedTables = []string{"trip_amendment", "arrival_amendment"}

// w9aSanctionedRawInput is named IN WORDS in the allow-list, per the
// criterion: `arrival_ref_retraction` is a SANCTIONED raw input to these
// paths (F4 §3.6), so the scan cannot false-red W9b's un-bind.
const w9aSanctionedRawInput = "arrival_ref_retraction"

// w9aEffectiveIdent is the first policed class: any `Effective*` identifier.
var w9aEffectiveIdent = regexp.MustCompile(`^Effective[A-Z]\w*$`)

type w9aFinding struct {
	Function string
	Class    string // "effective-identifier" | "amendment-table-reference"
	Detail   string
}

func (f w9aFinding) String() string {
	return fmt.Sprintf("%s in %s: %s", f.Class, f.Function, f.Detail)
}

func w9aDomainDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd() // …/app/internal/store
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("fixture defect: no go.mod at %s — the scan cannot locate the files it is scoped to: %v", root, err)
	}
	return filepath.Join(root, "internal", "domain")
}

// w9aInverseScan is THE walk. Both the plants and the clean pass go through
// this one function — a control handed to a different judgement proves
// nothing about the judgement that runs in production.
func w9aInverseScan(t *testing.T, dir string) (findings []w9aFinding, reached []string) {
	t.Helper()
	fset := token.NewFileSet()
	funcs := map[string]*ast.FuncDecl{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("w9a scan: reading %s: %v", dir, err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("w9a scan: parsing %s: %v", name, err)
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			funcs[fd.Name.Name] = fd
		}
	}
	for _, root := range w9aRecomputeRoots {
		if _, ok := funcs[root]; !ok {
			t.Fatalf("W9a-8 SCAN DEFECT: `%s` is not a function in %s. F4 §9.3 names the three recompute paths BY NAME; "+
				"a rename must make this scan fail loudly, never scan an empty set", root, dir)
		}
	}

	// Transitive closure over package-local calls from the three roots, so
	// a recompute path cannot launder an effective read through a helper.
	seen := map[string]bool{}
	queue := append([]string(nil), w9aRecomputeRoots...)
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if seen[name] {
			continue
		}
		fd, ok := funcs[name]
		if !ok {
			continue // not package-local: outside this scan's filter
		}
		seen[name] = true
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if id, ok := call.Fun.(*ast.Ident); ok {
				queue = append(queue, id.Name)
			}
			return true
		})
	}
	for name := range seen {
		reached = append(reached, name)
	}
	sort.Strings(reached)

	for _, name := range reached {
		fd := funcs[name]
		var literals []string
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.Ident:
				if w9aEffectiveIdent.MatchString(v.Name) {
					findings = append(findings, w9aFinding{name, "effective-identifier", v.Name})
				}
			case *ast.SelectorExpr:
				if w9aEffectiveIdent.MatchString(v.Sel.Name) {
					findings = append(findings, w9aFinding{name, "effective-identifier", v.Sel.Name})
				}
			case *ast.BasicLit:
				if v.Kind == token.STRING {
					if s, err := strconv.Unquote(v.Value); err == nil {
						literals = append(literals, s)
					}
				}
			}
			return true
		})
		// (a) each literal on its own, (b) the concatenated stream — which
		// is what sees a table name written apart across a `+`.
		joined := strings.Join(literals, "")
		for _, tbl := range w9aPolicedTables {
			for _, lit := range literals {
				if strings.Contains(lit, tbl) {
					findings = append(findings, w9aFinding{name, "amendment-table-reference", strconv.Quote(lit)})
				}
			}
			if !w9aAnyLiteralContains(literals, tbl) && strings.Contains(joined, tbl) {
				findings = append(findings, w9aFinding{name, "amendment-table-reference",
					fmt.Sprintf("%q, SPLIT ACROSS LITERALS in this function", tbl)})
			}
		}
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].String() < findings[j].String() })
	return findings, reached
}

func w9aAnyLiteralContains(literals []string, needle string) bool {
	for _, l := range literals {
		if strings.Contains(l, needle) {
			return true
		}
	}
	return false
}

// w9aScratchDomain copies internal/domain's production sources into a fresh
// scratch tree and applies `patch` to one file. Rule 20: an overlay does not
// reach a source scan, so the control must be a REAL tree on disk.
func w9aScratchDomain(t *testing.T, patchFile string, patch func(string) string) string {
	t.Helper()
	src := w9aDomainDir(t)
	dst := t.TempDir()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			t.Fatal(err)
		}
		body := string(b)
		if name == patchFile && patch != nil {
			out := patch(body)
			if out == body {
				t.Fatalf("W9a-8 CONTROL FAILED (instrument): the plant did not change %s — the anchor it edits has moved, so the control below would prove nothing", name)
			}
			body = out
		}
		if err := os.WriteFile(filepath.Join(dst, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}

// w9aPlantInBody inserts `line` immediately after the opening brace of the
// named function.
func w9aPlantInBody(fn, line string) func(string) string {
	return func(src string) string {
		i := strings.Index(src, "\nfunc "+fn+"(")
		if i < 0 {
			return src
		}
		j := strings.Index(src[i:], " {\n")
		if j < 0 {
			return src
		}
		at := i + j + len(" {\n")
		return src[:at] + line + "\n" + src[at:]
	}
}

// TestW9a8InverseScanOverTheThreeRecomputePaths — W9a-8 / T-scan.
func TestW9a8InverseScanOverTheThreeRecomputePaths(t *testing.T) {
	// --- META-CONTROL (T-scan's own): an UNPLANTED copy scans CLEAN. ----
	// Without it, a scan that reddened on everything would "pass" both
	// plants below and prove nothing.
	cleanCopy := w9aScratchDomain(t, "", nil)
	if f, reached := w9aInverseScan(t, cleanCopy); len(f) != 0 {
		t.Fatalf("W9a-8 META-CONTROL FAILED: an UNPLANTED copy of internal/domain produced findings %v (over %d reached functions). "+
			"The scan reds on the copying itself, so neither plant below would mean anything", f, len(reached))
	}

	// --- CONTROL 1 — the FIRST policed class: an `Effective*` call ------
	plant1 := w9aScratchDomain(t, "trip.go", w9aPlantInBody("computeStates",
		"\t_, _ = EffectiveTrip(nil, \"planted\")"))
	f1, _ := w9aInverseScan(t, plant1)
	if !w9aHasClass(f1, "effective-identifier") {
		t.Fatalf("W9a-8 CONTROL 1 FAILED: an `Effective*` call planted in `computeStates` was NOT detected. Findings: %v.\n"+
			"C-1's regression is exactly this: an effective read INSIDE the domain package, where the outward scan cannot look", f1)
	}

	// --- CONTROL 2 — the SECOND policed class: a raw table reference ----
	// ⚠ This is the plant an identifier-grep implementation survives.
	plant2 := w9aScratchDomain(t, "trip.go", w9aPlantInBody("recomputeTripStates",
		"\t_ = \"SELECT 1 FROM trip_arrival JOIN arrival_amendment USING (arrival_id)\""))
	f2, _ := w9aInverseScan(t, plant2)
	if !w9aHasClass(f2, "amendment-table-reference") {
		t.Fatalf("W9a-8 CONTROL 2 FAILED: a raw `JOIN arrival_amendment` planted in `recomputeTripStates` was NOT detected. Findings: %v.\n"+
			"An identifier-grep implementation passes CONTROL 1 while staying green forever over this. Until this plant existed the "+
			"table-reference half was UNPROVEN (W9R2-FA2-10)", f2)
	}

	// --- CONTROL 3 — the FILTER'S OWN WIDTH: a SPLIT literal ------------
	// Not required by the criterion; here because "a filter narrower than
	// its subject" is this project's most-found defect and a table name
	// written apart across a `+` is the cheapest way to hide one.
	plant3 := w9aScratchDomain(t, "trip.go", w9aPlantInBody("recomputeTripStatesScoped",
		"\t_ = \"SELECT 1 FROM trip_\" + \"amendment\""))
	f3, _ := w9aInverseScan(t, plant3)
	if !w9aHasClass(f3, "amendment-table-reference") {
		t.Fatalf("W9a-8 CONTROL 3 FAILED: a table name SPLIT across two literals (`\"trip_\" + \"amendment\"`) inside a recompute path was NOT detected. Findings: %v", f3)
	}

	// --- FALSE-RED CONTROL: the sanctioned raw input ---------------------
	// `arrival_ref_retraction` is named in the allow-list IN WORDS. The
	// scan must not red W9b's un-bind.
	plant4 := w9aScratchDomain(t, "trip.go", w9aPlantInBody("computeStates",
		"\t_ = \"SELECT trip_id FROM "+w9aSanctionedRawInput+"\""))
	if f4, _ := w9aInverseScan(t, plant4); len(f4) != 0 {
		t.Fatalf("W9a-8 FALSE-RED CONTROL FAILED: a read of the SANCTIONED raw input %q inside a recompute path was flagged: %v.\n"+
			"F4 §3.6 makes it a sanctioned raw input to these paths and the criterion names it in the allow-list IN WORDS, "+
			"so the scan must not false-red W9b", w9aSanctionedRawInput, f4)
	}

	// --- the real pass, over the live tree -------------------------------
	// ⚠ THIS GREEN IS NOT THE EVIDENCE. See this file's header.
	findings, reached := w9aInverseScan(t, w9aDomainDir(t))
	if len(findings) != 0 {
		t.Fatalf("W9a-8 (T-scan): the §9.3 INVERSE scan found %d violation(s) in internal/domain's recompute paths:\n  %s\n"+
			"F4 §9.1 rules `computeStates`, `recomputeTripStates` and `recomputeTripStatesScoped` RAW, ALWAYS. "+
			"Feeding effective values into them would redden W9a-7 and silence a live alert by record edit",
			len(findings), strings.Join(w9aFindingStrings(findings), "\n  "))
	}
	if len(reached) < len(w9aRecomputeRoots) {
		t.Fatalf("W9a-8 SCAN DEFECT: the closure reached %d functions, fewer than the %d roots", len(reached), len(w9aRecomputeRoots))
	}
	t.Logf("W9a-8: inverse scan clean over %d reachable functions from %v. "+
		"⚠ THIS CLEAN PASS IS NOT LEDGER EVIDENCE — the four controls above are.", len(reached), w9aRecomputeRoots)
}

func w9aHasClass(fs []w9aFinding, class string) bool {
	for _, f := range fs {
		if f.Class == class {
			return true
		}
	}
	return false
}

func w9aFindingStrings(fs []w9aFinding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.String())
	}
	return out
}
