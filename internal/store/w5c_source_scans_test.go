// w5c_source_scans_test.go — W5c's SOURCE-LEVEL halves — CORRECTED TO THE
// SCHEMA-11 SHAPE (2026-09-05; [[D-88]] + ERRATA, [[D-91]]):
//
//   - W5c-2(a): the CONSULTATION scan. ⟦was: no production Go OUTSIDE the
//     fold, the composition and the authoring/render paths names the fact⟧
//     — NO production Go names the fact AT ALL. The permitted set is EMPTY:
//     the fact is struck, so the fold, the composition and the render paths
//     lost their licence with it, and the former "permitted" spelling is now
//     a plant that must fire. The detector is unchanged; only its exemption
//     list is.
//   - W5c-1 (scan half): the AUTHORING-PATH scan. ⟦was: no production surface
//     can supply a value other than "true"⟧ — no production surface can bind
//     ANY value to the fact, and no HTML control of ANY shape may answer to
//     it. The old two-state plants still fire; the old "clean" `true` binding
//     and the bare free-text box are plants now.
//   - W5-X1 at W5c: every schema change this increment makes is declared
//     inside a single `CREATE `-containing Go string literal — no ALTER
//     TABLE, no ADD COLUMN, no DDL assembled by `+` concatenation. W5-X1
//     requires ONE CONTROL PER CHUNK THAT TOUCHES SCHEMA; the SchemaVersion-11
//     strike-and-add on `person`/`person_amendment` touches schema, so the
//     plants are re-pointed at the column this increment ADDS (`seen`, D-92).
//
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, criteria LOCKED 2026-08-31.
//
// package store_test, and in internal/store DELIBERATELY, on W5a's and W5b's
// precedent: these are file-system source scans naming no unbuilt Go symbol,
// so they COMPILE AND RUN against `88dd6a6`, and the store suite runs in
// seconds where internal/domain measures 205–572s.
//
// ⚠⚠ W5c-2 IS ALREADY TRUE AT 88dd6a6 — it is marked ⟦NO PRE-CHUNK RED⟧ in
// the lock and sits in the already-true table ("zero consumers of
// PadOriented"). W5c-1's scan half is likewise clean on arrival: the
// pre-chunk defect is at the EDGE (an unchecked free-text box), not in a
// planted literal. **A CLEAN PASS IN THIS FILE IS NOT EVIDENCE FOR EITHER.**
// The evidence is the PLANTED-VIOLATION CONTROL, run first, inside the same
// test, through the same detector the real pass uses — plus the named mutant
// produced against a real on-disk tree (rule 20: `go test -overlay` does not
// reach a source scan).
//
// KILLER MUTANTS: (m5c-2) consultation scan · (m5c-1) authoring-path scan ·
// (m5-X1).
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

// --- SEAMS bound here, once; disputes go to the gate --------------------
//
// SEAM W5c-S-FACT: the fact's two spellings are the Go field `PadOriented`
// and the wire/column key `pad_oriented`. NOT a free choice — W3 already
// landed both (person_amend.go:66, :149) and W5-X4 part (b) names
// `pad_oriented` verbatim for the column. Recorded only because this file
// matches on them as literals.
//
// SEAM W5c-S-PERMITTED: the consultation scan's permitted file set is the
// list below, derived from the criterion's own three roles (fold,
// composition, authoring/render) by measurement, not by preference. A
// genuine new authoring or render path that this list does not name is a
// FINDING FOR THE GATE, not a list for a builder to edit.

// w5cPermittedFiles are the files W5c-2 permits to reference the fact at
// all. ⟦SchemaVersion 11⟧ EMPTY: the fact is struck, so no production file
// has a role that permits naming it. ⟦was: person_amend.go (fold,
// composition, edge), refdata.go (shape, projection, created half), seed.go
// (an authoring path), web/person_amend.go (render + edge), web/intake.go
// (the second door)⟧ Kept as a map so the detector's shape is unchanged and
// a future exemption has to be written here, with a role, in the open.
var w5cPermittedFiles = map[string]string{}

// w5cFormerlyPermittedFiles are the files that USED to hold the fact and
// must still be in the scan universe, so a scan that cannot see them proves
// nothing about their being clean (rule 6).
var w5cFormerlyPermittedFiles = []string{
	filepath.Join("internal", "domain", "person_amend.go"),
	filepath.Join("internal", "domain", "refdata.go"),
	filepath.Join("internal", "web", "person_amend.go"),
	filepath.Join("internal", "web", "intake.go"),
}

// w5cConsultationPaths are files the criterion names by ROLE — check-in,
// dispatch, arrival, muster, alert. Asserted PRESENT in the scan universe so
// a mis-rooted or truncated walk reds loudly instead of passing over nothing
// (rule 6). These are exactly the files (m5c-2) would be planted in.
var w5cConsultationPaths = []string{
	filepath.Join("internal", "domain", "checkin.go"),
	filepath.Join("internal", "domain", "tripauthor.go"),
	filepath.Join("internal", "domain", "trip.go"),
	filepath.Join("internal", "domain", "muster.go"),
	filepath.Join("internal", "web", "gate.go"),
	filepath.Join("internal", "web", "trips.go"),
	filepath.Join("internal", "web", "alerts.go"),
	filepath.Join("internal", "web", "muster.go"),
}

// --- scan universe ------------------------------------------------------

func w5cModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the package directory — scan root unresolvable")
		}
		dir = parent
	}
}

// w5cProductionGoFiles is the scan universe: every non-test .go file in the
// module. _test.go is excluded because a test may legitimately name the fact
// as a fixture — this file does, many times.
func w5cProductionGoFiles(t *testing.T) []string {
	t.Helper()
	root := w5cModuleRoot(t)
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	// Rule 6: an empty or truncated universe is a silently green scan.
	if len(files) < 20 {
		t.Fatalf("fixture defect: the scan universe holds %d production Go files, which cannot be right for this module", len(files))
	}
	for _, want := range w5cConsultationPaths {
		if !w5cUniverseHas(files, want) {
			t.Fatalf("fixture defect: %s is absent from the scan universe — it is one of the check-in/dispatch/muster/alert paths W5c-2 is ABOUT, so a scan that cannot see it proves nothing", want)
		}
	}
	for want := range w5cPermittedFiles {
		if !w5cUniverseHas(files, want) {
			t.Fatalf("fixture defect: %s is named in W5c's permitted set (SEAM W5c-S-PERMITTED) but is absent from the scan universe — the permitted list has gone stale against the tree", want)
		}
	}
	for _, want := range w5cFormerlyPermittedFiles {
		if !w5cUniverseHas(files, want) {
			t.Fatalf("fixture defect: %s — a file that USED to hold the struck fact — is absent from the scan universe, so a clean scan cannot vouch for it", want)
		}
	}
	return files
}

func w5cUniverseHas(files []string, suffix string) bool {
	for _, f := range files {
		if strings.HasSuffix(f, suffix) {
			return true
		}
	}
	return false
}

// w5cRel renders an absolute scan path relative to the module root.
func w5cRel(t *testing.T, path string) string {
	t.Helper()
	rel, err := filepath.Rel(w5cModuleRoot(t), path)
	if err != nil {
		return path
	}
	return rel
}

func w5cPermitted(rel string) bool {
	_, ok := w5cPermittedFiles[rel]
	return ok
}

// --- hit machinery ------------------------------------------------------

type w5cHit struct {
	File   string
	Line   int
	Shape  string
	Detail string
}

func (h w5cHit) String() string {
	return fmt.Sprintf("%s:%d [%s] %s", h.File, h.Line, h.Shape, h.Detail)
}

func w5cFormat(hits []w5cHit) string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, "  "+h.String())
	}
	return strings.Join(out, "\n")
}

func w5cShapes(hits []w5cHit) []string {
	seen := map[string]bool{}
	var out []string
	for _, h := range hits {
		if !seen[h.Shape] {
			seen[h.Shape] = true
			out = append(out, h.Shape)
		}
	}
	sort.Strings(out)
	return out
}

func w5cParse(t *testing.T, name string, src any) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return fset, f
}

// w5cScanAll runs one detector over the whole production universe. `name` is
// the module-relative path, which is what the permitted set is keyed on.
func w5cScanAll(t *testing.T, detect func(*testing.T, string, any) []w5cHit) []w5cHit {
	t.Helper()
	var all []w5cHit
	for _, path := range w5cProductionGoFiles(t) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, detect(t, w5cRel(t, path), src)...)
	}
	return all
}

// --- the fact's spellings -----------------------------------------------

// w5cNamesTheFact reports whether a Go identifier denotes pad orientation.
// It matches by CONTAINMENT, deliberately: a helper called
// `IsPadOriented(...)` or `padOrientedFor(...)` is a read of the fact by any
// honest reading, and a scan that only matched the bare field name would let
// a consulting path launder itself behind a one-line accessor.
func w5cNamesTheFact(ident string) bool {
	return strings.Contains(ident, "PadOriented") || strings.Contains(ident, "padOriented")
}

// w5cLiteralNamesTheFact reports whether a string literal names the wire key
// or the column — which is how a SQL read or a map lookup spells it.
func w5cLiteralNamesTheFact(lit string) bool {
	return strings.Contains(lit, "pad_oriented")
}

func w5cStringLit(e ast.Expr) (string, bool) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", false
	}
	v, err := strconv.Unquote(bl.Value)
	if err != nil {
		return "", false
	}
	return v, true
}

// =======================================================================
// W5c-2 (a) — THE CONSULTATION SCAN
// =======================================================================

// w5cDetectConsultation flags every reference to the fact in a file outside
// the permitted set. Both shapes the criterion names are covered:
//
//	(go)  a Go identifier — the field, a selector on it, or a helper whose
//	      name carries it (the laundering shape)
//	(sql) a string literal carrying the column/wire key — a SELECT, a map
//	      lookup, a fact-name switch
func w5cDetectConsultation(t *testing.T, name string, src any) []w5cHit {
	t.Helper()
	if w5cPermitted(name) {
		return nil
	}
	fset, f := w5cParse(t, name, src)
	var hits []w5cHit
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Ident:
			if w5cNamesTheFact(x.Name) {
				hits = append(hits, w5cHit{name, fset.Position(x.Pos()).Line, "go",
					fmt.Sprintf("identifier %q names pad orientation", x.Name)})
			}
		case *ast.BasicLit:
			if v, ok := w5cStringLit(x); ok && w5cLiteralNamesTheFact(v) {
				hits = append(hits, w5cHit{name, fset.Position(x.Pos()).Line, "sql",
					fmt.Sprintf("string literal %q carries the pad_oriented key", w5cClipLit(v))})
			}
		}
		return true
	})
	return hits
}

func w5cClipLit(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 90 {
		return s[:90] + "…"
	}
	return s
}

// TestW5cNoProductionCodeNamesPadOrientation — W5c-2, the (a) SCAN half,
// at SchemaVersion 11. ⟦was: TestW5cNoSoftwarePathConsultsPadOrientation —
// "no path OUTSIDE the permitted five"⟧ The permitted set is empty, so the
// criterion is now "no production Go names the fact, anywhere": the struck
// fact has no fold to be written by, no composition to be read by and no
// surface to be rendered on.
//
// The evidence is the four planted CONTROLs run first, through this same
// detector, PLUS the former permitted spelling — the PersonAmended field
// itself — which used to be a clean control and must now FIRE.
//
// EXPECTED RED: any production file that re-introduces the identifier or the
// wire/column literal. The failure names the file, the line and the shape.
func TestW5cNoProductionCodeNamesPadOrientation(t *testing.T) {
	// --- CONTROLs, before the real scan (W5-X6 (a)) ---
	controls := []struct {
		name, path, src, wantShape string
	}{
		{
			name: "a dispatch path branching on the flag — (m5c-2) verbatim",
			path: filepath.Join("internal", "domain", "tripauthor.go"),
			src: `package domain
func dispatchTrip(p *Person) error {
	if p.PadOriented != nil && *p.PadOriented == "true" {
		return nil
	}
	return errNotOriented
}
`,
			wantShape: "go",
		},
		{
			name: "a check-in path laundering the read through a helper",
			path: filepath.Join("internal", "domain", "checkin.go"),
			src: `package domain
func resolvePersonSlot(id string) error {
	if !IsPadOriented(id) {
		return errRefused
	}
	return nil
}
`,
			wantShape: "go",
		},
		{
			name: "a muster path reading the column out of SQL",
			path: filepath.Join("internal", "domain", "muster.go"),
			src: `package domain
const musterQuery = "SELECT person_id, pad_oriented FROM person WHERE site_id = ?"
`,
			wantShape: "sql",
		},
		{
			name: "an alert path switching on the fact name",
			path: filepath.Join("internal", "web", "alerts.go"),
			src: `package web
func flagFor(fact string) string {
	switch fact {
	case "pad_oriented":
		return "ORIENTED"
	}
	return ""
}
`,
			wantShape: "sql",
		},
	}
	for _, c := range controls {
		hits := w5cDetectConsultation(t, c.path, c.src)
		if len(hits) == 0 {
			t.Fatalf("CONTROL FAILED (%s): the consultation detector saw NOTHING in a planted violation. A detector that cannot see its own mutant makes the clean pass below worthless (standing rule 5)", c.name)
		}
		if got := w5cShapes(hits); !w5cContains(got, c.wantShape) {
			t.Fatalf("CONTROL FAILED (%s): planted violation detected as shapes %v, want the %q shape — the plant fired the wrong tooth", c.name, got, c.wantShape)
		}
	}
	// A clean file of the same shape must NOT fire: without this the detector
	// could be "return one hit always" and every control above would pass.
	clean := `package domain
func dispatchTrip(p *Person) error {
	if p.Name == "" {
		return errNoName
	}
	return nil
}
`
	if hits := w5cDetectConsultation(t, filepath.Join("internal", "domain", "tripauthor.go"), clean); len(hits) != 0 {
		t.Fatalf("CONTROL FAILED: the detector fired on a clean dispatch path:\n%s", w5cFormat(hits))
	}
	// The FORMER permitted spelling — the payload field in the amendment
	// mechanism's own file — must now FIRE: ⟦was: "must NOT fire — the
	// criterion permits the fold, the composition and the authoring/render
	// paths in terms"⟧. At SchemaVersion 11 there is no permitted file, and a
	// detector that still exempted person_amend.go would let the strike be
	// quietly undone in the one place it matters most.
	former := `package domain
type PersonAmended struct {
	PadOriented *string ` + "`json:\"pad_oriented,omitempty\"`" + `
}
`
	if hits := w5cDetectConsultation(t, filepath.Join("internal", "domain", "person_amend.go"), former); len(hits) == 0 {
		t.Fatal("CONTROL FAILED: the detector stayed silent on the struck field re-declared in person_amend.go — the permitted set must be EMPTY at SchemaVersion 11 (SEAM W5c-S-PERMITTED)")
	}

	// --- the real scan ---
	hits := w5cScanAll(t, w5cDetectConsultation)
	if len(hits) != 0 {
		var roles []string
		for f, role := range w5cPermittedFiles {
			roles = append(roles, "  "+f+" — "+role)
		}
		sort.Strings(roles)
		t.Fatalf("W5c-2 (m5c-2′): `pad_oriented` is STRUCK at SchemaVersion 11 ([[D-88]] ERRATA / [[D-91]]) — no production Go may name it, under the identifier or the wire/column literal. %d reference(s):\n%s\n\nPermitted (EMPTY by ruling; a role written here is the only way in):\n%s\n\nPer-site orientation is the ORIENTING post's own keyed record, built in a later stage of this increment under its own names.",
			len(hits), w5cFormat(hits), strings.Join(roles, "\n"))
	}
}

func w5cContains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// =======================================================================
// W5c-1 (scan half) — THE AUTHORING-PATH SCAN
// =======================================================================
//
// W5-X6 (a) names this "W5c-1's authoring-path scan". It carries the half of
// W5c-1 that no fixture can carry: the criterion asserts the value domain
// over EVERY authoring path (intake, amendment edge, dev routes), and a
// fixture can only drive the paths that exist. The scan asserts the property
// structurally, in two arms:
//
//	(go)   no production Go binds a string literal other than "true" to the
//	       fact — the "edge that writes the unchecked value" arm of (m5c-1)
//	(tmpl) no HTML control can POST a second state — the "checkbox that posts
//	       both states" arm of (m5c-1), verbatim
//
// ⚠ WHAT THIS SCAN DELIBERATELY DOES NOT REQUIRE. It does not require the
// amendment form's control to stop being a free-text box. W5c-1's own RED
// row is "that POST — accepted before the build, refused after it", so the
// criterion's tooth is the EDGE REFUSAL, and a build that guards only the
// edge is compliant. Pinning a control shape here would red such a build.
// The edge refusal is asserted where it belongs: internal/domain
// (TestW5cAmendmentEdgeCannotAuthorPadOrientationAtAll,
// TestW5cAmendmentEdgeStillAuthorsTheSurvivingFacts) and internal/web
// (TestW5cAmendmentFormStillRecordsASurvivingFact). ⟦was:
// "TestW5cAmendmentEdgeRefusesEveryValueOutsideTheDomain,
// TestW5cAmendmentEdgeStillAuthorsTheOneLegalValue,
// TestW5cAmendmentFormStillRecordsAnOrientation" — renamed with the
// SchemaVersion-11 strike⟧

// w5cAliases resolves file-local identifiers to string literals through a
// BOUNDED FIXED POINT (5 rounds), never a single pass.
//
// ⚠ The single pass is a MEASURED defect, twice over: W5a's (m5a-8) and
// W5b's (m5-X1)-concat plant both survived a one-hop collector, because the
// commonest spelling of each mutant binds through an intermediate. Recorded
// so nobody "simplifies" this back.
func w5cAliases(f *ast.File) map[string]string {
	alias := map[string]string{}
	assign := func(lhs, rhs ast.Expr) {
		id, ok := lhs.(*ast.Ident)
		if !ok {
			return
		}
		if v, ok := w5cResolveLit(rhs, alias); ok {
			alias[id.Name] = v
		}
	}
	for round := 0; round < 5; round++ {
		before := len(alias)
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				// `+=` is DELIBERATELY skipped: recording the appended
				// fragment would OVERWRITE the identifier's CREATE-bearing
				// value and hide the `+=` spelling of (m5-X1) from the
				// detector below [MEASURED — the control failed exactly so].
				if x.Tok == token.ADD_ASSIGN {
					return true
				}
				for i := range x.Lhs {
					if i < len(x.Rhs) {
						assign(x.Lhs[i], x.Rhs[i])
					}
				}
			case *ast.ValueSpec:
				for i := range x.Names {
					if i < len(x.Values) {
						assign(x.Names[i], x.Values[i])
					}
				}
			}
			return true
		})
		if len(alias) == before {
			break
		}
	}
	return alias
}

// w5cResolveLit resolves an expression to a string literal where it can,
// through &x, *x, (x), a one-argument call (ptr("…"), string(x)) and the
// alias map.
func w5cResolveLit(e ast.Expr, alias map[string]string) (string, bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		return w5cStringLit(x)
	case *ast.Ident:
		v, ok := alias[x.Name]
		return v, ok
	case *ast.ParenExpr:
		return w5cResolveLit(x.X, alias)
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return w5cResolveLit(x.X, alias)
		}
	case *ast.StarExpr:
		return w5cResolveLit(x.X, alias)
	case *ast.CallExpr:
		if len(x.Args) == 1 {
			return w5cResolveLit(x.Args[0], alias)
		}
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			l, lok := w5cResolveLit(x.X, alias)
			r, rok := w5cResolveLit(x.Y, alias)
			if lok || rok {
				return l + r, true
			}
		}
	}
	return "", false
}

// w5cTargetsTheFact reports whether an assignment target names pad
// orientation — `p.PadOriented`, `in.PadOriented`, `*dst` where dst is
// PadOriented-named, or a bare `padOriented` local.
func w5cTargetsTheFact(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return w5cNamesTheFact(x.Name)
	case *ast.SelectorExpr:
		return w5cNamesTheFact(x.Sel.Name)
	case *ast.StarExpr:
		return w5cTargetsTheFact(x.X)
	case *ast.ParenExpr:
		return w5cTargetsTheFact(x.X)
	case *ast.IndexExpr:
		if v, ok := w5cStringLit(x.Index); ok {
			return w5cLiteralNamesTheFact(v)
		}
	}
	return false
}

// w5cKeyNamesTheFact reports whether a composite-literal key denotes the
// fact — the Go field name or the wire key as a map key.
func w5cKeyNamesTheFact(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return w5cNamesTheFact(x.Name)
	case *ast.BasicLit:
		if v, ok := w5cStringLit(x); ok {
			return w5cLiteralNamesTheFact(v)
		}
	}
	return false
}

// w5cDetectAuthoredValue is the (go) arm: ANY string literal bound to the
// fact. ⟦was: "a string literal OTHER THAN \"true\""⟧ — at SchemaVersion 11
// there is no legal value, so the `v != "true"` exemption is gone from all
// three shapes and the old clean control (`ptr("true")`) is a plant.
func w5cDetectAuthoredValue(t *testing.T, name string, src any) []w5cHit {
	t.Helper()
	fset, f := w5cParse(t, name, src)
	alias := w5cAliases(f)
	var hits []w5cHit
	flag := func(pos token.Pos, how, value string) {
		hits = append(hits, w5cHit{name, fset.Position(pos).Line, "go",
			fmt.Sprintf("%s binds pad orientation to %q — the fact is STRUCK at SchemaVersion 11; nothing may be bound to it", how, value)})
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.KeyValueExpr:
			if !w5cKeyNamesTheFact(x.Key) {
				return true
			}
			if v, ok := w5cResolveLit(x.Value, alias); ok {
				flag(x.Pos(), "a composite-literal field", v)
			}
		case *ast.AssignStmt:
			for i := range x.Lhs {
				if i >= len(x.Rhs) || !w5cTargetsTheFact(x.Lhs[i]) {
					continue
				}
				if v, ok := w5cResolveLit(x.Rhs[i], alias); ok {
					flag(x.Lhs[i].Pos(), "an assignment", v)
				}
			}
		case *ast.ValueSpec:
			for i := range x.Names {
				if i >= len(x.Values) || !w5cNamesTheFact(x.Names[i].Name) {
					continue
				}
				if v, ok := w5cResolveLit(x.Values[i], alias); ok {
					flag(x.Names[i].Pos(), "a declaration", v)
				}
			}
		}
		return true
	})
	return hits
}

// --- the template arm ---------------------------------------------------

var (
	w5cTagRe    = regexp.MustCompile(`(?is)<(input|select|textarea|option)\b[^>]*>`)
	w5cAttrRe   = regexp.MustCompile(`(?is)([a-zA-Z_:][-a-zA-Z0-9_:.]*)\s*=\s*"([^"]*)"`)
	w5cSelectRe = regexp.MustCompile(`(?is)<select\b[^>]*>.*?</select>`)
	w5cOptionRe = regexp.MustCompile(`(?is)<option\b([^>]*)>(.*?)</option>`)
)

func w5cTagAttrs(tag string) map[string]string {
	attrs := map[string]string{}
	for _, m := range w5cAttrRe.FindAllStringSubmatch(tag, -1) {
		attrs[strings.ToLower(m[1])] = m[2]
	}
	return attrs
}

func w5cTagKind(tag string) string {
	m := w5cTagRe.FindStringSubmatch(tag)
	if len(m) < 2 {
		return ""
	}
	return strings.ToLower(m[1])
}

// w5cLineOf returns the 1-based line of offset within src.
func w5cLineOf(src string, off int) int {
	if off > len(src) {
		off = len(src)
	}
	return 1 + strings.Count(src[:off], "\n")
}

// w5cDetectPostableStates is the (tmpl) arm. ⟦was: three teeth — pair /
// value / option — over a control that was ALLOWED to exist, with a BARE
// free-text box explicitly not a hit⟧. At SchemaVersion 11 the control
// itself is the finding, whatever its shape: every <input>/<select>/
// <textarea> whose name or id answers to the struck key is reported under
// the one tooth `control`. Every old plant still fires (each of them is a
// control answering to the key); the bare box is a plant now too.
//
// Applied to template source AND, in internal/web, to RENDERED pages — the
// rendered application is what sees through `name="{{.Key}}"`, which no
// static scan can resolve.
func w5cDetectPostableStates(name, src string) []w5cHit {
	var hits []w5cHit
	for _, loc := range w5cTagRe.FindAllStringIndex(src, -1) {
		tag := src[loc[0]:loc[1]]
		kind := w5cTagKind(tag)
		if kind == "option" {
			continue
		}
		attrs := w5cTagAttrs(tag)
		if attrs["name"] != "pad_oriented" && attrs["id"] != "pad_oriented" && attrs["id"] != "amend-pad_oriented" {
			continue
		}
		hits = append(hits, w5cHit{name, w5cLineOf(src, loc[0]), "control",
			fmt.Sprintf("a <%s> answers to `pad_oriented` (value=%q) — the fact is struck; no control of any shape may offer it", kind, attrs["value"])})
	}
	return hits
}

func w5cTemplateFiles(t *testing.T) []string {
	t.Helper()
	root := w5cModuleRoot(t)
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".html") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	// Rule 6: the two forms that could ever carry this control must be in
	// the universe, or the scan passes over nothing.
	for _, want := range []string{
		filepath.Join("internal", "web", "templates", "person_amend.html"),
		filepath.Join("internal", "web", "templates", "intake.html"),
	} {
		if !w5cUniverseHas(files, want) {
			t.Fatalf("fixture defect: %s is absent from the template universe (%d files) — the scan cannot see the form W5c-1 is about", want, len(files))
		}
	}
	return files
}

// TestW5cNoSurfaceCanAuthorPadOrientationAtAll — W5c-1, the SCAN half, at
// SchemaVersion 11. ⟦was: TestW5cNoSurfaceCanAuthorASecondState — "no value
// other than true, no two-state control"⟧ Now: NO value, NO control.
//
// Its evidence is the planted CONTROLs below — the old plants, which all
// still fire, plus the two old CLEAN controls that are plants now: the
// `ptr("true")` binding and the bare free-text box.
//
// W5c-1's behavioural half lives at the edge and the form:
// TestW5cAmendmentEdgeCannotAuthorPadOrientationAtAll (internal/domain) and
// TestW5cOfficerCannotAuthorPadOrientationThroughTheAmendmentForm
// (internal/web).
//
// EXPECTED RED: a builder who re-introduces the field and binds anything to
// it, or who renders any control for it. Kills (m5c-1′)'s both arms.
func TestW5cNoSurfaceCanAuthorPadOrientationAtAll(t *testing.T) {
	// --- CONTROLs for the (go) arm ---
	goPlants := []struct{ name, src string }{
		{"a direct write of the unchecked value at the edge", `package web
func handle(in *domain.PersonAmendment) {
	in.PadOriented = ptr("false")
}
`},
		{"a composite-literal field carrying it", `package domain
func mint(id string) PersonAmended {
	return PersonAmended{PersonID: id, PadOriented: ptr("false")}
}
`},
		{"the value bound through ONE alias hop", `package web
func handle(in *domain.PersonAmendment) {
	unchecked := "false"
	in.PadOriented = &unchecked
}
`},
		{"the value bound through TWO alias hops — the shape that survived a one-pass collector in W5a and W5b", `package web
func handle(in *domain.PersonAmendment) {
	off := "0"
	unchecked := off
	in.PadOriented = &unchecked
}
`},
		{"the wire key written as a map entry", `package domain
func facts() map[string]string {
	return map[string]string{"pad_oriented": "false"}
}
`},
	}
	for _, p := range goPlants {
		if hits := w5cDetectAuthoredValue(t, filepath.Join("internal", "web", "person_amend.go"), p.src); len(hits) == 0 {
			t.Fatalf("CONTROL FAILED (%s): the authored-value detector saw NOTHING in a planted violation — the clean pass below would prove nothing (standing rule 5)", p.name)
		}
	}
	// ⟦was: two CLEAN controls — the `true` binding and the pass-through
	// setField spelling⟧. The `true` binding is a PLANT now (no value is
	// legal); the pass-through spelling binds no literal, so it is not this
	// detector's tooth — it is the CONSULTATION scan's, which fires on the
	// identifier. A genuinely clean edge (a surviving fact) must stay clean.
	if hits := w5cDetectAuthoredValue(t, filepath.Join("internal", "web", "person_amend.go"), `package web
func handle(in *domain.PersonAmendment) {
	in.PadOriented = ptr("true")
}
`); len(hits) == 0 {
		t.Fatal("CONTROL FAILED: the authored-value detector stayed silent on `in.PadOriented = ptr(\"true\")` — at SchemaVersion 11 the former legal value is a binding to a struck fact and must fire")
	}
	if hits := w5cDetectConsultation(t, filepath.Join("internal", "web", "person_amend.go"), `package web
func handle(in *domain.PersonAmendment, v string) {
	setField(&in.PadOriented, "pad_oriented")
	_ = v
}
`); len(hits) == 0 {
		t.Fatal("CONTROL FAILED: the pass-through `setField(&in.PadOriented, \"pad_oriented\")` spelling escaped BOTH scans — the consultation scan must catch the identifier where the value scan cannot")
	}
	if hits := w5cDetectAuthoredValue(t, filepath.Join("internal", "web", "person_amend.go"), `package web
func handle(in *domain.PersonAmendment) {
	in.Position = ptr("Rig Manager")
}
`); len(hits) != 0 {
		t.Fatalf("CONTROL FAILED: the authored-value detector fired on a binding to a SURVIVING fact:\n%s", w5cFormat(hits))
	}

	// --- CONTROLs for the (tmpl) arm: every old plant, plus the two old
	// clean controls that are plants at SchemaVersion 11 ---
	tmplPlants := []struct{ name, src string }{
		{"the hidden+checkbox pair", `<form><input type="hidden" name="pad_oriented" value="false"><input type="checkbox" name="pad_oriented" value="true"></form>`},
		{"a lone hidden field", `<form><input type="hidden" name="pad_oriented" value="false"></form>`},
		{"a yes/no select", `<form><select name="pad_oriented"><option value="">—</option><option value="true">Yes</option><option value="false">No</option></select></form>`},
		{"a select whose options post their own text", `<form><select name="pad_oriented"><option>true</option><option>No</option></select></form>`},
		{"a checkbox posting only true — ⟦was⟧ clean", `<form><input type="checkbox" name="pad_oriented" value="true"></form>`},
		{"the bare free-text box — ⟦was⟧ clean", `<form><input id="amend-pad_oriented" name="pad_oriented"></form>`},
	}
	for _, p := range tmplPlants {
		hits := w5cDetectPostableStates("planted.html", p.src)
		if len(hits) == 0 {
			t.Fatalf("CONTROL FAILED (%s): the control detector saw NOTHING in a planted pad_oriented control", p.name)
		}
		if got := w5cShapes(hits); !w5cContains(got, "control") {
			t.Fatalf("CONTROL FAILED (%s): detected as %v, want the \"control\" tooth", p.name, got)
		}
	}
	for _, clean := range []string{
		`<form><input id="amend-{{.Key}}" name="{{.Key}}"></form>`,
		`<form><input id="amend-position" name="position"></form>`,
	} {
		if hits := w5cDetectPostableStates("clean.html", clean); len(hits) != 0 {
			t.Fatalf("CONTROL FAILED: the control detector fired on a control that does not answer to the struck key %q:\n%s", clean, w5cFormat(hits))
		}
	}

	// --- the real scans ---
	if hits := w5cScanAll(t, w5cDetectAuthoredValue); len(hits) != 0 {
		t.Fatalf("W5c-1 (m5c-1′): a production authoring path binds a value to the struck fact — nothing may be authored against `pad_oriented` at SchemaVersion 11 ([[D-88]] ERRATA):\n%s", w5cFormat(hits))
	}
	var tmplHits []w5cHit
	for _, path := range w5cTemplateFiles(t) {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		tmplHits = append(tmplHits, w5cDetectPostableStates(w5cRel(t, path), string(b))...)
	}
	if len(tmplHits) != 0 {
		t.Fatalf("W5c-1 (m5c-1′): a template offers a control for the struck fact — orientation is the ORIENTING post's own record ([[D-88]]), not a box on the person form:\n%s", w5cFormat(tmplHits))
	}
}

// =======================================================================
// W5-X1 at W5c — schema changes stay inside ONE `CREATE ` literal
// =======================================================================

// w5cDetectDDL is W5-X1's detector: an ALTER TABLE / ADD COLUMN string
// anywhere in production Go, or DDL assembled by `+` concatenation.
func w5cDetectDDL(t *testing.T, name string, src any) []w5cHit {
	t.Helper()
	fset, f := w5cParse(t, name, src)
	alias := w5cAliases(f)
	var hits []w5cHit
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BasicLit:
			v, ok := w5cStringLit(x)
			if !ok {
				return true
			}
			up := strings.ToUpper(strings.Join(strings.Fields(v), " "))
			if strings.Contains(up, "ALTER TABLE") || strings.Contains(up, "ADD COLUMN") {
				hits = append(hits, w5cHit{name, fset.Position(x.Pos()).Line, "alter",
					fmt.Sprintf("DDL outside a CREATE literal: %q", w5cClipLit(v))})
			}
		case *ast.BinaryExpr:
			if x.Op != token.ADD {
				return true
			}
			joined, ok := w5cResolveLit(x, alias)
			if !ok {
				return true
			}
			if strings.Contains(strings.ToUpper(joined), "CREATE ") {
				hits = append(hits, w5cHit{name, fset.Position(x.Pos()).Line, "concat",
					fmt.Sprintf("DDL assembled by `+` concatenation: %q", w5cClipLit(joined))})
			}
		case *ast.AssignStmt:
			if x.Tok != token.ADD_ASSIGN {
				return true
			}
			for i := range x.Rhs {
				lhs, lok := w5cResolveLit(x.Lhs[i], alias)
				rhs, rok := w5cResolveLit(x.Rhs[i], alias)
				if (lok || rok) && strings.Contains(strings.ToUpper(lhs+rhs), "CREATE ") {
					hits = append(hits, w5cHit{name, fset.Position(x.Pos()).Line, "concat",
						fmt.Sprintf("DDL assembled by `+=`: %q", w5cClipLit(lhs+rhs))})
				}
			}
		}
		return true
	})
	return hits
}

// TestW5cSchemaChangesStayInsideOneCreateLiteral — W5-X1, this increment's
// OWN control on the person tables. ⟦was: W5c's — its plants named
// `person.pad_oriented`, the column W5c ADDED⟧. At SchemaVersion 11 the
// person tables change again — `pad_oriented` struck, `seen` added to
// `person_amendment` ([[D-92]]) — and W5-X1 requires "one control per chunk
// that touches schema", so the plants are re-pointed at the column this
// increment adds. The detector is unchanged.
//
// EXPECTED RED: a builder who adds `seen` with `ALTER TABLE person_amendment
// ADD COLUMN seen TEXT` — exactly the reach a nullable column addition
// invites — or who drops the struck column with ALTER, or who splits the
// CREATE literal to append. Kills (m5-X1).
func TestW5cSchemaChangesStayInsideOneCreateLiteral(t *testing.T) {
	plants := []struct{ name, src, wantShape string }{
		{"the column added by ALTER TABLE", `package domain
const migrate = "ALTER TABLE person_amendment ADD COLUMN seen TEXT"
`, "alter"},
		{"the struck column dropped by ALTER TABLE", `package domain
const migrate = "ALTER TABLE person DROP COLUMN pad_oriented"
`, "alter"},
		{"the CREATE literal split and the column appended by +", `package domain
const amendHead = "CREATE TABLE IF NOT EXISTS person_amendment ("
const amendDDL = amendHead + "site_id TEXT NOT NULL, seen TEXT) STRICT;"
`, "concat"},
		{"the same, assembled with +=", `package domain
func schema() string {
	ddl := "CREATE TABLE IF NOT EXISTS person_amendment ("
	ddl += "seen TEXT) STRICT;"
	return ddl
}
`, "concat"},
	}
	for _, p := range plants {
		hits := w5cDetectDDL(t, filepath.Join("internal", "domain", "refdata.go"), p.src)
		if len(hits) == 0 {
			t.Fatalf("CONTROL FAILED (%s): the DDL detector saw NOTHING in a planted violation", p.name)
		}
		if got := w5cShapes(hits); !w5cContains(got, p.wantShape) {
			t.Fatalf("CONTROL FAILED (%s): detected as %v, want the %q tooth", p.name, got, p.wantShape)
		}
	}
	clean := `package domain
const amendDDL = ` + "`" + `CREATE TABLE IF NOT EXISTS person_amendment (
	site_id TEXT NOT NULL,
	seen    TEXT
) STRICT;` + "`" + `
`
	if hits := w5cDetectDDL(t, filepath.Join("internal", "domain", "refdata.go"), clean); len(hits) != 0 {
		t.Fatalf("CONTROL FAILED: the DDL detector fired on a compliant single-literal CREATE:\n%s", w5cFormat(hits))
	}

	if hits := w5cScanAll(t, w5cDetectDDL); len(hits) != 0 {
		t.Fatalf("W5-X1 (m5-X1): this increment's schema change must be declared inside the single `CREATE `-containing literal — W4 narrowed W1's guard to scan only such literals, so DDL outside one is INVISIBLE to the guard:\n%s", w5cFormat(hits))
	}
}
