// w5d_source_scans_test.go — W5d-4's RESPECIFIED ordering scan (scoped by
// RESOLVED CONSTANT VALUE through go/types, controlled by literals planted
// THROUGH THE WALK AND THE FILTER, with per-identifier liveness over string
// literals), W5d-15's company consumer scan, W5d-6's structural rider,
// A-4 branch (b)'s package-level staleness scan, and W5-X1's DDL scan.
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, W5d LOCKED 2026-08-31.
//
// package store, DELIBERATELY (beside W1's and W3's scan machinery, which
// this file REUSES): these are file-system source scans referencing no
// unbuilt Go symbol, so they COMPILE AND RUN against the clean tree — the
// company-side liveness assertion is genuinely RED there, and every scan
// proves it detects a PLANTED violation before any clean pass counts.
//
// ⚠⚠ WHY THIS SCAN EXISTS RATHER THAN A WIDENING OF W3's.
// TestW3OneOrderBySpellingInComposition's filter admits only
// *ast.BasicLits containing the side-table identifier
// (w3_source_scans_test.go:213). The house's own M-1 constant split puts
// the table name in ONE literal and the ORDER BY in ANOTHER
// (person_amend.go:255–261), and the `+` join is a BinaryExpr over Idents —
// never a BasicLit — so **that filter selects NO ORDER-BY-BEARING LITERAL
// AT ALL**, and its whole-file-text liveness fallback is satisfied by the
// canonical spelling appearing in two COMMENTS (person_amend.go:14, :248).
// Measured here at rev-of-writing: resolving the same package's constants
// through go/types yields
//
//	personAmendmentCompositionSQL = "SELECT … FROM person_amendment ORDER BY site_id, node_epoch, seq"
//
// — the joined SQL the reader actually executes, which is what this scan
// examines. Rules (a), (b) and (c) below are the LOCKED respecification;
// rule (d) (the stale caller comment at person_amend.go:259–260) is a
// PRODUCTION edit and is the BUILDER's, reported rather than made here.
//
// KILLER MUTANTS: (m4) (CompanyByID reading the whole company_amendment
// table — the M-1 shape — or a company_amendment composition literal
// carrying a deviant/epoch-blind ORDER BY), (m9) (a non-domain package
// reading company_amendment or company_amended directly), (m3)'s
// structural rider (workgroup gaining a composed path), (m5-X1) (a schema
// change outside a single CREATE-containing literal).
package store

import (
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// w5dScopedSideTables is W5d-4's identifier SET: person + company. The
// landed const pair w3SideTableIdent/w3EventTypeLit
// (w3_source_scans_test.go:33–34) is widened for W3-14's consumer scan
// under the DR-5/A-12 grant; this scan carries the set for the ORDERING
// half, whose FILTER, CONTROL and LIVENESS arms are all respecified.
// ⟦LOCK — W8-18(b) discharge, 2026-09-03⟧ `trade_amendment` JOINS THIS SET.
// W8-18(b)'s whole discharge is that the RESPECIFIED ordering instrument —
// TestW5dOneOrderBySpellingInCompositionByResolvedValue, which scopes by
// RESOLVED CONSTANT VALUE through go/types — gains trade's side table to its
// scope. Until it does, a deviant or epoch-blind ORDER BY inside a
// trade_amendment composition constant is examined by NOTHING.
// ⚠ Widening w3SideTableIdent instead is NOT the discharge and satisfies
// nothing: that filter admits only *ast.BasicLits containing the table
// identifier, and the house's M-1 constant split leaves the ORDER BY in a
// literal carrying no table name.
// ⚠ ASSERT MEMBERSHIP OF THIS SET, NEVER ITS LENGTH: W7 adds
// "vehicle_amendment" to the same variable, so a gate observing three members
// at W8 may be observing W7's fix.
// ⟦was: `var w5dScopedSideTables = []string{"person_amendment", "company_amendment"}`⟧
var w5dScopedSideTables = []string{"person_amendment", "company_amendment", "trade_amendment"}

// w5dCompanySideTableIdent / w5dCompanyEventTypeLit are W5d-15's surface.
const (
	w5dCompanySideTableIdent = "company_amendment"
	w5dCompanyEventTypeLit   = "company_amended"
)

// --- (a) scope by RESOLVED CONSTANT VALUE (go/types is the DEFAULT) -----

// w5dConstValue is one package-level string constant with its RESOLVED
// value — the joined SQL, not the fragment as written.
type w5dConstValue struct {
	Name  string
	Pos   string
	Value string
}

// w5dResolvedStringConsts type-checks the non-test files of one directory
// as a package and returns every string constant with its RESOLVED value.
//
// The type-checker runs with errors SWALLOWED and no importer: the
// composition constants are untyped string constants built from string
// literals and other constants of the same package, so they resolve fully
// without any import being available. That is what makes this scan immune
// to WHERE a declaration sits — the defect const-group (co-location)
// scoping has, which a one-line hoist defeats.
func w5dResolvedStringConsts(t *testing.T, dir string) ([]w5dConstValue, s56Diags) {
	t.Helper()
	dg := s56Diags{Where: dir}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			// ⟦LOCK — §56(1), 2026-09-03⟧ ⚠ THIS WAS THE ONE SILENT SKIP LEFT
			// IN THE NINE, and it is §56's defect arriving by a different
			// door: a plant that does not PARSE never reaches go/types, so
			// the type-check bar cannot see it, and the file was dropped
			// with no record at all. The scan then reports zero constants,
			// the control reports "the plant was NOT reported", and the
			// reader is sent to debug a scan that was fine.
			//
			// ⚠ RECORDED, NOT MADE A TRACKED-TREE BAR. The original comment
			// ("an unparseable file is another agent's in-flight work;
			// scoped around, not repaired") is a real consideration and is
			// preserved: this walk covers EVERY package directory in the
			// module, so failing on a parse error would red the suite over
			// somebody else's half-saved file (rule 18). Landing the fault
			// in the SUBSTANTIVE stream gets both — `s56PlantTypeChecked`
			// fires on a PLANT that will not parse, while the tracked tree
			// only reports it in the calibration line and in failure text.
			dg.Substantive = append(dg.Substantive,
				filepath.Join(dir, e.Name())+": UNPARSEABLE — this file was NOT scanned, so this scan's "+
					"coverage is smaller than its stated scope: "+err.Error())
			continue
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		return nil, dg
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}}
	// ⟦LOCK — §56(1), 2026-09-03⟧ RECORDED, never discarded. ⟦was:
	// "Error: func(error) {},"⟧
	conf := types.Config{
		Error:                    dg.Record,
		Importer:                 nil,
		DisableUnusedImportCheck: true,
		IgnoreFuncBodies:         true,
	}
	_, _ = conf.Check("w5dscan", fset, files, info)

	var out []w5dConstValue
	for id, obj := range info.Defs {
		c, ok := obj.(*types.Const)
		if !ok {
			continue
		}
		v := c.Val()
		if v == nil || v.Kind() != constant.String {
			continue
		}
		out = append(out, w5dConstValue{
			Name:  id.Name,
			Pos:   fset.Position(id.Pos()).String(),
			Value: constant.StringVal(v),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pos < out[j].Pos })
	return out, dg
}

// w5dOrderByFindings is THE judgement function: the deviants it reports and
// the per-table liveness counts it computes. The real pass AND every
// control drive THIS function over a DIRECTORY, so a control red proves the
// same walk + filter + detector the clean pass runs.
type w5dOrderByFindings struct {
	Deviants []string
	Liveness map[string]int // scoped table → count of scoped consts carrying the canonical spelling

	// ⟦LOCK — §56(1), 2026-09-03⟧ every go/types diagnostic raised while
	// checking the directories walked. ⚠ IT EXISTS BECAUSE THE CONFIG USED TO
	// SWALLOW THEM: this scan is RESOLVED-VALUE, so a plant that does not
	// type-check yields no *types.Const, and the scan then reports zero
	// deviants and zero liveness — indistinguishable from a blind scan.
	Diags s56Set
}

func w5dScanOrderBy(t *testing.T, dirs ...string) w5dOrderByFindings {
	t.Helper()
	f := w5dOrderByFindings{Liveness: map[string]int{}}
	for _, tbl := range w5dScopedSideTables {
		f.Liveness[tbl] = 0
	}
	for _, dir := range dirs {
		consts, dg := w5dResolvedStringConsts(t, dir)
		f.Diags = append(f.Diags, dg)
		for _, c := range consts {
			for _, tbl := range w5dScopedSideTables {
				if !strings.Contains(c.Value, tbl) {
					continue
				}
				// (c) LIVENESS is a per-identifier counter over STRING
				// LITERALS' resolved values — never whole-file text, which
				// a comment satisfies.
				if w3CanonicalOrderBy.MatchString(c.Value) {
					f.Liveness[tbl]++
				}
				for _, d := range w3DeviantOrderBy(c.Value) {
					f.Deviants = append(f.Deviants, c.Pos+" ("+c.Name+", scoped by resolved value on "+tbl+"): "+d)
				}
			}
		}
	}
	sort.Strings(f.Deviants)
	return f
}

// w5dPlantDir writes named sources into a fresh scratch directory the scan
// is then run over BY DIRECTORY — the control is never handed the planted
// file's name.
func w5dPlantDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestW5dOneOrderBySpellingInCompositionByResolvedValue — W5d-4,
// RESPECIFIED. RED on the clean tree: no non-test constant resolves to a
// value that both reads company_amendment and carries the canonical
// `ORDER BY site_id, node_epoch, seq`, because the composition is unbuilt.
func TestW5dOneOrderBySpellingInCompositionByResolvedValue(t *testing.T) {
	// --- CONTROL 1 (b): the deviant literal planted IN A SCOPED CONST
	// GROUP and run THROUGH THE WALK AND THE FILTER — one per scoped
	// table. A control that calls w3DeviantOrderBy directly exercises the
	// DETECTOR, which is the one layer the scoping does NOT change, and is
	// NOT a control for this criterion. -----------------------------------
	for _, tbl := range w5dScopedSideTables {
		src := `package planted

const (
	compositionSelect = "SELECT site_id, node_epoch, seq FROM ` + tbl + `"
	compositionOrderBy = " ORDER BY node_epoch, site_id, seq"
	compositionSQL = compositionSelect + compositionOrderBy
)
`
		got := w5dScanOrderBy(t, w5dPlantDir(t, map[string]string{"zz_plant.go": src}))
		// ⟦LOCK — §56(1)⟧ ASKED BEFORE THE DEVIANTS ARE READ.
		if !s56PlantTypeChecked(t, "W5d-4 CONTROL 1 ("+tbl+")", got.Diags) {
			continue
		}
		if len(got.Deviants) == 0 {
			t.Fatalf("W5d-4 CONTROL 1 FAILED for %s: a deviant `ORDER BY node_epoch, site_id, seq` planted in a scoped const group was NOT reported BY THE SCAN. The landed W3 filter admits only *ast.BasicLits containing the table identifier, and the ORDER BY literal contains no table name — so it examines ZERO ordering literals. Scoping by RESOLVED VALUE is what closes that, and this control is the only one that exercises it", tbl)
		}
	}

	// --- CONTROL 2 (R2-W3): the OUT-OF-GROUP plant — the one shape the
	// const-group FALLBACK cannot see, and exactly the shape the house's
	// own M-1 fix produced. The ORDER BY is declared OUTSIDE any
	// table-naming group, concatenated in, with a CANONICAL DECOY left
	// inside the group. A scan that passes this plant has the co-location
	// defect and rule (a)'s go/types default is not in force. -------------
	outOfGroup := `package planted

// The deviant, declared OUTSIDE any table-naming const group (a one-line
// hoist is all it takes).
const hoistedOrderBy = " ORDER BY node_epoch, site_id, seq"

const (
	// A canonical DECOY sits inside the group, so co-location scoping sees
	// only a compliant spelling.
	decoyOrderBy = " ORDER BY site_id, node_epoch, seq"
	compositionSelect = "SELECT site_id, node_epoch, seq FROM company_amendment"
	compositionSQL = compositionSelect + hoistedOrderBy
)
`
	hoist := w5dScanOrderBy(t, w5dPlantDir(t, map[string]string{"zz_hoist.go": outOfGroup}))
	if !s56PlantTypeChecked(t, "W5d-4 CONTROL 2 (the out-of-group hoist)", hoist.Diags) {
		return
	}
	if len(hoist.Deviants) == 0 {
		t.Fatalf("W5d-4 CONTROL 2 FAILED (R2-W3): an ORDER BY literal declared OUTSIDE the table-naming const group and concatenated in was NOT reported — only the RESOLVED value is immune to where the declaration sits, and the const-group fallback is defeated by a one-line hoist plus a canonical decoy")
	}

	// --- CONTROL 3 (c): the LIVENESS plant — a source whose canonical
	// spelling appears ONLY IN A COMMENT must FAIL the liveness assertion.
	// That is the exact defeat measured on the landed whole-file-text
	// fallback (person_amend.go carries the canonical spelling in two
	// comments besides the const). ---------------------------------------
	commentOnly := `package planted

// The ONE ordering spelling: ORDER BY site_id, node_epoch, seq.
// Any second key-column ORDER BY spelling anywhere in the app trips this.
const (
	compositionSelect = "SELECT site_id, node_epoch, seq FROM company_amendment"
	compositionSQL = compositionSelect + " ORDER BY seq"
)
`
	comment := w5dScanOrderBy(t, w5dPlantDir(t, map[string]string{"zz_comment.go": commentOnly}))
	if !s56PlantTypeChecked(t, "W5d-4 CONTROL 3 (the comment-only liveness plant)", comment.Diags) {
		return
	}
	if comment.Liveness[w5dCompanySideTableIdent] != 0 {
		t.Fatalf("W5d-4 CONTROL 3 FAILED: a source whose canonical spelling appears ONLY IN A COMMENT counted %d toward liveness, want 0 — liveness is a per-identifier counter over STRING LITERALS' resolved values, never whole-file text", comment.Liveness[w5dCompanySideTableIdent])
	}
	// …and a compliant plant must COUNT, or "liveness is zero" would be
	// true by construction for every input.
	compliant := `package planted

const (
	compositionSelect = "SELECT site_id, node_epoch, seq FROM company_amendment"
	compositionOrderBy = " ORDER BY site_id, node_epoch, seq"
	compositionSQL = compositionSelect + compositionOrderBy
)
`
	ok := w5dScanOrderBy(t, w5dPlantDir(t, map[string]string{"zz_ok.go": compliant}))
	if !s56PlantTypeChecked(t, "W5d-4 CONTROL 3b (the compliant plant must COUNT)", ok.Diags) {
		return
	}
	if ok.Liveness[w5dCompanySideTableIdent] == 0 {
		t.Fatalf("W5d-4 CONTROL 3b FAILED: a compliant scoped const group carrying the canonical spelling counted ZERO toward liveness — the counter cannot rise, so the real assertion could never pass honestly")
	}

	// --- the real pass, over every package directory in the module -------
	got := w5dScanOrderBy(t, w5dGoPackageDirs(t)...)
	s56LogTree(t, "w5dScanOrderBy", got.Diags)
	if len(got.Deviants) != 0 {
		t.Fatalf("W5d-4 (m4): deviant key-column ORDER BY spelling(s) in a scoped composition constant's RESOLVED value — one ordering SPEC means ONE SQL spelling (`ORDER BY site_id, node_epoch, seq`) beside the one Go comparator:\n%s", strings.Join(got.Deviants, "\n"))
	}
	for _, tbl := range w5dScopedSideTables {
		if got.Liveness[tbl] == 0 {
			t.Fatalf("W5d-4: no non-test constant RESOLVES to a value that both reads %s and carries the canonical `ORDER BY site_id, node_epoch, seq` — the ascending-key composition for that side table is unbuilt or spelled differently. (Liveness counts: %v)\n"+
				"⟦§56⟧ what this scan type-checked, so a ZERO LIVENESS is never confused with a tree that did not compile:\n%s", tbl, got.Liveness, got.Diags.Report())
		}
	}
}

// w5dGoPackageDirs lists every directory under the module root holding at
// least one non-test .go file — the scan UNIVERSE, taken as a directory
// glob so a plant can only be discovered, never pointed at.
func w5dGoPackageDirs(t *testing.T) []string {
	t.Helper()
	root := w1ModuleRoot(t)
	seen := map[string]bool{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "node_modules" || (strings.HasPrefix(info.Name(), ".") && path != root) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			seen[filepath.Dir(path)] = true
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
	if len(dirs) == 0 {
		t.Fatal("W5d-4: scan universe empty — the walk found no non-test Go package directories, which cannot be right")
	}
	return dirs
}

// --- W5d-15: the consumer scan's surface, company side ------------------

// TestW5dCompanyConsumerScan — W5d-15: no package outside internal/domain
// reads the company_amendment side table or its event-type constants (SQL
// identifiers and Go constants both). The scan FIRST proves it detects a
// planted violation, then the clean pass counts. Kills (m9).
//
// ⚠ DR-5's owed confirmation, MEASURED before this surface was widened:
// `grep -an 'person_amended|person_amendment' internal/web/*.go | grep -av
// _test.go` returns ZERO, and the identifier `PersonAmended` appears only
// in internal/domain — so the person-side clone needs neither literal in a
// non-domain package, and W5d-23's new web route needs neither company
// literal either. If the builder finds it does, that is a FINDING for the
// gate, not a workaround.
func TestW5dCompanyConsumerScan(t *testing.T) {
	forbiddenIdents := map[string]bool{
		w5dCompanySideTableIdent: true, // a Go identifier named after the table
		"CompanyAmended":         true, // the payload type — building payloads outside domain bypasses the edge
	}
	forbiddenInStrings := []string{w5dCompanySideTableIdent, w5dCompanyEventTypeLit}
	allowlist := map[string]bool{} // EMPTY — extensions are gate rulings, not edits made in passing

	// --- CONTROL FIRST: the planted consumer must be detected -----------
	planted := `package sneaky

const q = "SELECT name FROM company_amendment WHERE company_id = ?"

func decide(typ string) bool { return typ == "company_amended" }
`
	hits := w1ScanGoTokens(t, "planted/company_consumer.go", []byte(planted), forbiddenIdents, forbiddenInStrings)
	if len(hits) < 2 {
		t.Fatalf("W5d-15 CONTROL FAILED (m9): the scan found %d hit(s) in the planted company consumer, want >= 2 (the SQL identifier and the event-type constant) — a scan that cannot detect its plant proves nothing. Hits: %v", len(hits), hits)
	}
	clean := "package fine\n\nvar ok = true\n"
	if extra := w1ScanGoTokens(t, "planted/clean.go", []byte(clean), forbiddenIdents, forbiddenInStrings); len(extra) != 0 {
		t.Fatalf("W5d-15 CONTROL: the scan flagged a clean file: %v", extra)
	}

	// --- the real pass over every non-domain package --------------------
	var found []w1Hit
	for _, path := range w3NonDomainGoFiles(t) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range w1ScanGoTokens(t, path, src, forbiddenIdents, forbiddenInStrings) {
			if !allowlist[h.File] {
				found = append(found, h)
			}
		}
	}
	if len(found) != 0 {
		t.Fatalf("W5d-15 (m9): package(s) outside internal/domain read the company_amendment side table or its event-type constants — every consumer resolves through the resolver / composed read path, never the table:\n%v", found)
	}
}

// --- W5d-6's structural rider (DR-4) ------------------------------------

// TestW5dTradeGainsNoComposedPath — W5d-6's structural half, riding
// BESIDE the behavioural fixture in internal/domain (never instead of it:
// a structural assertion that muster.go:133 still names "an alias map" is
// satisfied by a build that passes the SAME map to both lines, which is
// precisely the failure). Here the assertion is that the workgroup
// mechanism does not exist AT ALL: no workgroup_amended event type, no
// workgroup_amendment side table, no LoadWorkgroupMergeMap.
func TestW5dTradeGainsNoComposedPath(t *testing.T) {
	forbiddenIdents := map[string]bool{
		"WorkgroupAmended":      true,
		"workgroup_amendment":   true,
		"LoadWorkgroupMergeMap": true,
	}
	forbiddenInStrings := []string{"workgroup_amended", "workgroup_amendment"}

	// --- CONTROL FIRST --------------------------------------------------
	planted := `package sneaky

const t = "workgroup_amended"

func LoadWorkgroupMergeMap() {}
`
	if hits := w1ScanGoTokens(t, "planted/workgroup.go", []byte(planted), forbiddenIdents, forbiddenInStrings); len(hits) < 2 {
		t.Fatalf("W5d-6 CONTROL FAILED (m3): the scan found %d hit(s) in the planted workgroup composed path, want >= 2", len(hits))
	}

	// --- the real pass, whole tree --------------------------------------
	root := w1ModuleRoot(t)
	var found []w1Hit
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "node_modules" || (strings.HasPrefix(info.Name(), ".") && path != root) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		found = append(found, w1ScanGoTokens(t, path, src, forbiddenIdents, forbiddenInStrings)...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("W5d-6 (m3): workgroup gained a composed amendment path at W5d — DR-4 re-points the COMPANY line only; workgroup is deleted at the bump by W8 and keeps the ref_alias map until then (P-3 §5's schedule, cited never restated):\n%v", found)
	}
}

// --- A-4 branch (b): the package-level staleness scan -------------------

// w5dPackageLevelAliasMapVars reports package-level VARIABLES whose
// declared type or initialiser names AliasMap. Loader FUNCTIONS
// (LoadAliasMap, LoadPersonMergeMap, LoadCompanyMergeMap) are the allowed
// shape and are NOT flagged — the predicate is written out because W5d is
// about to add the second loader, and unwritten the criterion was ambiguous
// exactly where W5d changes the code.
//
// ⚠ WHAT THIS DOES NOT BUY, stated so nobody reads it as more: it is a
// structural tooth for PACKAGE-LEVEL staleness only. A muster.go:122 that
// calls a loader locally PER REQUEST passes it, so W3-m9 is NOT covered
// structurally. W3-m9's real producer is behavioural — W5d-5's
// legacy-ref_alias divergence fixture — and W5d-12's AR2-2 carve-out is its
// second.
func w5dPackageLevelAliasMapVars(t *testing.T, path string, src []byte) []w1Hit {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		// ⟦LOCK — §139, 2026-09-03 (S102D)⟧ NAMED, never dropped. ⟦was: "return nil"⟧
		// ⚠ A PLANT RUNS THROUGH HERE (:570, `planted/stale.go`), so a silent
		// drop made this control blind: the plant would contribute no hit,
		// which is byte-identical to the scan correctly finding none.
		// ⚠ t.Errorf rather than a recorded stream — matching w7's four
		// siblings — because this corpus is `internal/domain` ALONE, not
		// every package directory in the module, so it cannot red over
		// another agent's in-flight file (rule 18) the way w5dResolvedStringConsts could.
		t.Errorf("A-4(b) FAULT — %s was NOT scanned, so this scan's coverage is smaller than its stated "+
			"scope: %v", path, err)
		return nil
	}
	var hits []w1Hit
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			mentions := false
			check := func(n ast.Node) {
				ast.Inspect(n, func(m ast.Node) bool {
					if id, ok := m.(*ast.Ident); ok && id.Name == "AliasMap" {
						mentions = true
					}
					if sel, ok := m.(*ast.SelectorExpr); ok && sel.Sel.Name == "AliasMap" {
						mentions = true
					}
					return true
				})
			}
			if vs.Type != nil {
				check(vs.Type)
			}
			for _, v := range vs.Values {
				check(v)
			}
			if mentions {
				hits = append(hits, w1Hit{
					File: path, Line: fset.Position(vs.Pos()).Line,
					Detail: "package-level AliasMap-typed variable " + vs.Names[0].Name,
				})
			}
		}
	}
	return hits
}

// TestW5dNoPackageLevelAliasMapState — A-4 branch (b), ADOPTED at W5d's
// lock: no package-level AliasMap-typed state in internal/domain outside
// the loaders. W3-m9's registered shape is "a privately-loaded stale
// snapshot", and this is its structural tooth.
func TestW5dNoPackageLevelAliasMapState(t *testing.T) {
	// --- CONTROL FIRST: the planted package-level variable --------------
	planted := `package domain

var cachedMergeMap AliasMap

var otherCache = AliasMap{}

func LoadCompanyMergeMap() (AliasMap, error) { return nil, nil }
`
	hits := w5dPackageLevelAliasMapVars(t, "planted/stale.go", []byte(planted))
	if len(hits) != 2 {
		t.Fatalf("A-4(b) CONTROL FAILED: the staleness scan found %d planted package-level AliasMap variable(s), want 2 (one typed, one initialised) — and it must NOT flag the loader FUNCTION beside them. Hits: %v", len(hits), hits)
	}

	// --- the real pass over internal/domain -----------------------------
	root := w1ModuleRoot(t)
	var found []w1Hit
	for _, path := range w1GoFiles(t, filepath.Join(root, "internal", "domain")) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		found = append(found, w5dPackageLevelAliasMapVars(t, path, src)...)
	}
	if len(found) != 0 {
		t.Fatalf("A-4(b) (W3-m9's structural tooth): package-level AliasMap-typed state in internal/domain — the resolution snapshot is DERIVED PER READ and holds no state of its own; a cached snapshot is the second mechanism P-3 exists to prevent:\n%v", found)
	}
}

// --- W5-X1: every schema change inside a single CREATE-containing literal

// w5dConcatenatedDDL reports `+` expressions whose combined string literals
// look like assembled DDL — the "column list concatenated onto a prefix"
// shape W5-X1 forbids.
func w5dConcatenatedDDL(t *testing.T, path string, src []byte) []w1Hit {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		// ⟦LOCK — §139, 2026-09-03 (S102D)⟧ NAMED, never dropped. ⟦was: "return nil"⟧
		// ⚠ TWO PLANTS RUN THROUGH HERE — the positive at :651 and the
		// NEGATIVE at :654 — and the negative is the worse case: a dead plant
		// adds no hit, which is exactly what the negative arm demands, so it
		// reported SILENT SUCCESS.
		t.Errorf("W5-X1 FAULT — %s was NOT scanned, so this scan's coverage is smaller than its stated "+
			"scope: %v", path, err)
		return nil
	}
	var hits []w1Hit
	ast.Inspect(f, func(n ast.Node) bool {
		be, ok := n.(*ast.BinaryExpr)
		if !ok || be.Op != token.ADD {
			return true
		}
		var joined strings.Builder
		ast.Inspect(be, func(m ast.Node) bool {
			if lit, ok := m.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				joined.WriteString(lit.Value)
			}
			return true
		})
		up := strings.ToUpper(joined.String())
		if strings.Contains(up, "CREATE TABLE") || strings.Contains(up, "CREATE TRIGGER") || strings.Contains(up, "ALTER TABLE") {
			hits = append(hits, w1Hit{File: path, Line: fset.Position(be.Pos()).Line,
				Detail: "DDL assembled by `+` concatenation"})
		}
		return true
	})
	return hits
}

// TestW5dSchemaIsDeclaredInOneCreateLiteral — W5-X1 for W5d: every schema
// change is declared inside a SINGLE `CREATE `-containing Go string
// literal. No ALTER TABLE, no ADD COLUMN, no DDL assembled by
// concatenating a column list onto a prefix.
//
// ⟦NO PRE-CHUNK RED — ALTER TABLE/ADD COLUMN are ZERO non-test hits at
// 88dd6a6, so this criterion passes on arrival and its clean pass is NOT
// evidence. Its two CONTROLS are.⟧
func TestW5dSchemaIsDeclaredInOneCreateLiteral(t *testing.T) {
	forbiddenInStrings := []string{"ALTER TABLE", "ADD COLUMN", "alter table", "add column"}

	// --- CONTROL 1: a planted ALTER TABLE … ADD COLUMN ------------------
	plantAlter := `package planted

const migrate = "ALTER TABLE company_amendment ADD COLUMN superseded INTEGER"
`
	if hits := w1ScanGoTokens(t, "planted/alter.go", []byte(plantAlter), nil, forbiddenInStrings); len(hits) == 0 {
		t.Fatalf("W5-X1 CONTROL 1 FAILED (m5-X1): a planted `ALTER TABLE … ADD COLUMN` was not detected")
	}
	// --- CONTROL 2: a DDL string built by `+` concatenation -------------
	plantConcat := `package planted

const cols = "site_id TEXT NOT NULL, seq INTEGER NOT NULL"

const schema = "CREATE TABLE IF NOT EXISTS company_amendment (" + cols + ") STRICT;"
`
	if hits := w5dConcatenatedDDL(t, "planted/concat.go", []byte(plantConcat)); len(hits) == 0 {
		t.Fatalf("W5-X1 CONTROL 2 FAILED (m5-X1): a DDL string built by `+` concatenation was not detected — W4 narrowed W1's guard 3a to scan only literals containing \"CREATE \", leaving DDL outside such a literal invisible where the whole-file scan saw it")
	}
	if hits := w5dConcatenatedDDL(t, "planted/clean.go", []byte("package p\n\nconst q = \"SELECT a\" + \" FROM b\"\n")); len(hits) != 0 {
		t.Fatalf("W5-X1 CONTROL 2: a plain concatenated SELECT was flagged as DDL: %v", hits)
	}

	// --- the real pass over W5d's touched package -----------------------
	root := w1ModuleRoot(t)
	var found []w1Hit
	for _, path := range w1GoFiles(t, filepath.Join(root, "internal", "domain"), filepath.Join(root, "internal", "store")) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		found = append(found, w1ScanGoTokens(t, path, src, nil, forbiddenInStrings)...)
		found = append(found, w5dConcatenatedDDL(t, path, src)...)
	}
	if len(found) != 0 {
		t.Fatalf("W5-X1 (m5-X1): a schema change outside a single `CREATE `-containing string literal — every W5 sub-chunk adds projection state, which is exactly the moment someone reaches for ALTER TABLE:\n%v", found)
	}
}

// --- ⟦LOCK — §139, 2026-09-03 (S102D)⟧ THE PARSE DOOR'S CONTROL ------------
//
// ⭐ §124: BUILT IN, NOT PERFORMED ONCE. Both scans below used to `return nil`
// on a parse error with no record of any kind, and PLANTS RUN THROUGH BOTH —
// so both controls were blind and both read green. The repair was produced by
// hand against a deliberately broken plant; this test is that production made
// permanent, so the guard cannot decay back to silence unnoticed.
//
// ⚠ WHY BOTH IN ONE TEST: the defect is one class in one file, and a reader
// who fixes one door should be shown the other in the same failure.
func TestW5dAnUnparseableSourceIsNamedByBothScans(t *testing.T) {
	// Parses as a file, fails as Go — the shape a rotted plant actually has.
	const broken = "package planted\n\nconst ( {\n"

	for _, c := range []struct {
		name string
		run  func(*testing.T) []w1Hit
	}{
		{"w5dPackageLevelAliasMapVars (plant at :570)", func(tt *testing.T) []w1Hit {
			return w5dPackageLevelAliasMapVars(tt, "planted/broken.go", []byte(broken))
		}},
		{"w5dConcatenatedDDL (plants at :651 positive and :654 NEGATIVE)", func(tt *testing.T) []w1Hit {
			return w5dConcatenatedDDL(tt, "planted/broken.go", []byte(broken))
		}},
	} {
		// A nested *testing.T so the scan's own t.Errorf is CAPTURED rather
		// than reddening this test — the assertion is that it FIRED.
		// ⚠ A zero-value testing.T is not documented API. If that ever stops
		// working the goroutine PANICS, which fails loudly; it cannot make
		// this control pass in silence, which is the only outcome that would
		// matter.
		sub := &testing.T{}
		var hits []w1Hit
		done := make(chan struct{})
		go func() { defer close(done); hits = c.run(sub) }()
		<-done
		if !sub.Failed() {
			t.Errorf("⛔ THE PARSE DOOR IS OPEN AGAIN — %s: a source that does not parse was accepted in "+
				"SILENCE (%d hits, no failure raised).\n"+
				"  ⚠ A plant runs through this scan. A plant that does not parse contributes no hit, which is "+
				"byte-identical to the scan correctly finding none — so the control reads GREEN while blind. "+
				"⟦§139⟧ is that door; ⟦§56(1)⟧ was the same defect one stage later.", c.name, len(hits))
		}
		if len(hits) != 0 {
			t.Errorf("%s: an unparseable source produced %d hit(s), want 0 — a fault is not a finding, and "+
				"folding it into the hit list would turn a coverage gap into a false violation", c.name, len(hits))
		}
	}
}
