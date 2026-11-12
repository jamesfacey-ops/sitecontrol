// w8_source_scans_test.go — W8 (item 10, the trade register): the source-
// and schema-shaped criteria that live in `internal/store`.
//
// Written from the LOCKED W8 criteria only (design/WINDOW-BUILD-PLAN-v1.md
// §3, the W8 section folded verbatim from reports/window-build/
// W8-criteria-draft.md). NO built implementation was read: W8 is unbuilt at
// the tree these tests were written against, and every assertion here is
// traced to a criterion's own words at the point of use.
//
// ⚠ NOTHING HERE NAMES AN UNBUILT GO SYMBOL. W8's own §2.7 requires it —
// "written over Go symbols it will not compile; written over table names as
// strings through SQLite introspection it reds at f33c4c7" — and the tree
// this file lands in carries three other windows' in-flight builds, so one
// non-compiling test file would take internal/store down for all of them.
//
// The criteria covered here:
//
//	W8-18(b)  the ORDER-BY one-spelling scan, RE-POINTED at the respecified
//	          instrument (w5dScopedSideTables) and widened THERE, by MEMBER
//	          and never by count, with FOUR controls driven THROUGH the scan.
//	W8-28     the consumer scan's surface EXTENDED, not re-created.
//
// ⚠ W8-X1 and W8-X5 live in w8_schema_scan_test.go, which is `package
// store_test`: they need domain.Register and the w6* plant helpers, and
// `package store` cannot import internal/domain (import cycle — measured).
package store

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// W8-18(b) — the widening, asserted by MEMBER
// ---------------------------------------------------------------------------

// TestW8ScopedSideTablesGainsTradeAmendment is W8-18(b)'s discharge, stated
// exactly as the criterion states it and not as its superseded form.
//
// The criterion's own words: "The instrument is
// TestW5dOneOrderBySpellingInCompositionByResolvedValue
// (internal/store/w5d_source_scans_test.go), which
// scopes by RESOLVED CONSTANT VALUE through go/types rather than by literal
// containment. W8 widens w5dScopedSideTables (w5d_source_scans_test.go:59) —
// today {"person_amendment", "company_amendment"} — by the member
// "trade_amendment"."
//
// ⚠ AND IT IS ASSERTED BY MEMBER, NEVER BY COUNT ⟦W7-RECONCILE point 9⟧:
// W7 adds "vehicle_amendment" to the SAME variable. Three members after W7,
// four after W8 — "a gate observing three members at W8 is observing W7's
// fix". So this test says nothing about the length of the slice.
//
// ⚠ STATED ON THE CRITERION'S FACE, and repeated here so a builder meeting
// this red does not fix the wrong thing: widening w3SideTableIdent is NOT the
// discharge. That constant is a single string, its neighbouring comment is a
// warning AGAINST this widening, and the W3 filter examines no ordering
// literal at all — "widening a filter that selects nothing selects nothing
// for company too".
//
// PRE-CHUNK RED: produced. w5dScopedSideTables holds two members today and
// neither is trade's; this test reds naming the criterion.
func TestW8ScopedSideTablesGainsTradeAmendment(t *testing.T) {
	const want = "trade_amendment"

	// PRECONDITION (the landed w5d_scoped_read_test.go:216-228 idiom): the
	// variable this assertion is about must be the ORDERING scan's set and
	// must already carry the two members the criterion quotes. If it does
	// not, the subject moved and this assertion is about something else.
	// ⚠ This is stated as a MEMBERSHIP precondition, not a length one.
	for _, must := range []string{"person_amendment", "company_amendment"} {
		if !w8Contains(w5dScopedSideTables, must) {
			t.Fatalf("PRECONDITION: w5dScopedSideTables = %v, which does not carry %q — "+
				"W8-18(b) names this variable as the respecified ORDER-BY instrument's scope set "+
				"and quotes its two members; if the variable no longer means that, the widening "+
				"below is being asserted of the wrong thing. This is the INSTRUMENT, not W8",
				w5dScopedSideTables, must)
		}
	}

	if !w8Contains(w5dScopedSideTables, want) {
		t.Fatalf("W8-18(b): w5dScopedSideTables = %v and does not contain %q. "+
			"W8-18(b)'s discharge is that the RESPECIFIED ORDER-BY instrument — "+
			"TestW5dOneOrderBySpellingInCompositionByResolvedValue, which scopes by RESOLVED "+
			"CONSTANT VALUE through go/types — gains trade's side table to its scope. Until it "+
			"does, a deviant or epoch-blind ORDER BY inside a trade_amendment composition "+
			"constant is examined by NOTHING.\n"+
			"⚠ Widening w3SideTableIdent instead is NOT the discharge and satisfies nothing: "+
			"that filter admits only *ast.BasicLits containing the table identifier, and the "+
			"house's M-1 constant split leaves the ORDER BY in a literal carrying no table name.\n"+
			"⚠ Assert MEMBERSHIP when you fix this, never a count: W7 adds \"vehicle_amendment\" "+
			"to this same variable.",
			w5dScopedSideTables, want)
	}
}

func w8Contains(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// W8-18(b) — the FOUR controls, all driven THROUGH the scan
// ---------------------------------------------------------------------------

// w8CanonicalOrderBy is THE one composition ordering spelling W8 inherits:
// site_id, node_epoch, seq — SITE-MAJOR.
//
// ⚠ R2-W13's NAMING DISCIPLINE IS CARRIED VERBATIM, and it is the two words
// that decide which rule W8 locks in: the comparators are named by their
// SPELLINGS, never by the word "epoch-major". The certified rule is
// (site_id, node_epoch, seq); the rival puts node_epoch FIRST. Naming the
// certified rule "epoch-major" names it by the property that distinguishes
// its rival, and a test-writer who reads it the natural way builds the
// fixture BACKWARDS while every distinctness check still passes.
var w8CanonicalOrderBy = regexp.MustCompile(`(?i)ORDER\s+BY\s+(?:[a-z_]+\.)?site_id\s*,\s*(?:[a-z_]+\.)?node_epoch\s*,\s*(?:[a-z_]+\.)?seq\b`)

var w8AnyOrderBy = regexp.MustCompile(`(?i)ORDER\s+BY\s+([a-z_.,\s()]+)`)

// w8ScanResult is one run of the trade ORDER-BY scan: the faults it reported
// and how many COMPLIANT trade-scoped ordering literals it examined.
//
// ⚠ The liveness counter is CONTROL 3b's whole subject and is the arm the
// other routes did not name: three controls that all assert "the scan reports
// a fault" are jointly satisfied by an instrument that reports a fault for
// the wrong reason and NEVER COUNTS A HEALTHY LITERAL.
type w8ScanResult struct {
	Faults []string
	Live   int

	// ⟦LOCK — §56(1), 2026-09-03⟧ every go/types diagnostic raised while
	// checking the scanned directory. ⚠ IT EXISTS BECAUSE THE CONFIG USED TO
	// SWALLOW THEM: a plant that does not type-check contributes no *types.Const,
	// so the scan reports ZERO faults and ZERO live — byte-identical to the
	// output of a scan that is simply blind. See s56_plant_typecheck_test.go.
	Diags s56Set
}

// w8ScanTradeOrderBy is W8's own driver over the respecified instrument's
// mechanism: package-level string CONSTANTS resolved through go/types, scoped
// by RESOLVED VALUE (never by literal containment), selected by carrying the
// trade side table's identifier, and judged on their ORDER BY spelling.
//
// ⚠ THE RESIDUAL IS STATED RATHER THAN DISCOVERED LATER ⟦LOCK — FIL-F1⟧: the
// resolved-value instrument reports only *types.Const, so a deviant ORDER BY
// in a `var`, in a function-local const, or assembled by Sprintf is OUTSIDE
// this instrument. W8 asserts its own composition SQL is a package-level
// const, so the residual is BOUNDED — and a build that moves it to a `var`
// has left the scan's reach, which is a FINDING, never a pass.
func w8ScanTradeOrderBy(t *testing.T, dir string) w8ScanResult {
	t.Helper()
	var res w8ScanResult

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	var parsed []*ast.File
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		// ⚠ Comments are DISCARDED by the parser's default mode, which is
		// CONTROL 3's whole mechanism: a comment narrating a deviant ORDER BY
		// is not an ordering literal and must count ZERO — neither a fault
		// nor a live examination.
		f, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			res.Faults = append(res.Faults, fmt.Sprintf("%s: UNPARSEABLE — this file was NOT scanned, so the scan's coverage is smaller than its stated scope: %v", path, err))
			continue
		}
		parsed = append(parsed, f)
	}
	if len(parsed) == 0 {
		return res
	}

	info := &types.Info{Defs: map[*ast.Ident]types.Object{}}
	// ⟦LOCK — §56(1), 2026-09-03⟧ RECORDED, never discarded. ⟦was:
	// "conf := types.Config{Error: func(error) {}, Importer: nil, DisableUnusedImportCheck: true}"⟧
	dg := s56Diags{Where: dir}
	conf := types.Config{Error: dg.Record, Importer: nil, DisableUnusedImportCheck: true}
	_, _ = conf.Check("w8orderbyscan", fset, parsed, info)
	res.Diags = s56Set{dg}

	for ident, obj := range info.Defs {
		c, ok := obj.(*types.Const)
		if !ok || c.Parent() == nil || c.Parent().Parent() != types.Universe {
			continue // package-level constants only
		}
		v := c.Val()
		if v == nil || v.Kind() != constant.String {
			continue
		}
		// SCOPED BY RESOLVED VALUE, never by literal containment: this is
		// the whole difference between the respecified instrument and the
		// W3 one it replaces.
		sql := constant.StringVal(v)
		if !strings.Contains(sql, "trade_amendment") {
			continue
		}
		pos := fset.Position(ident.Pos())
		clauses := w8AnyOrderBy.FindAllString(sql, -1)
		if len(clauses) == 0 {
			continue
		}
		for _, clause := range clauses {
			tail := strings.ToLower(clause)
			touched := 0
			for _, col := range []string{"site_id", "node_epoch", "seq"} {
				if strings.Contains(tail, col) {
					touched++
				}
			}
			if touched < 2 {
				continue
			}
			if w8CanonicalOrderBy.MatchString(clause) {
				res.Live++
				continue
			}
			res.Faults = append(res.Faults, fmt.Sprintf(
				"%s (const %s): ORDER BY spelling %q is not the canonical (site_id, node_epoch, seq)",
				pos, c.Name(), strings.TrimSpace(clause)))
		}
	}
	sort.Strings(res.Faults)
	return res
}

// TestW8TradeOrderByScanHasFourProducedControls is W8-18(b)'s control set.
//
// The criterion: "FOUR CONTROLS, all re-produced against the TRADE spelling
// and all driven THROUGH THE SCAN, never by calling a detector directly —
// revision 2's prescribed controls called w3DeviantOrderBy on hardcoded
// literals, which bypasses the one layer the widening changes."
//
//	CONTROL 1  the reordered spelling ORDER BY node_epoch, site_id, seq
//	           planted in the trade composition const group, REPORTED.
//	CONTROL 2  an OUT-OF-GROUP HOIST: the same deviant spelling in a constant
//	           declared outside the composition group, still reported.
//	CONTROL 3  a COMMENT-ONLY occurrence of the deviant spelling counts ZERO.
//	CONTROL 3b a COMPLIANT plant makes the LIVENESS COUNTER RISE.
//
// Every control runs on a PLANTED SCRATCH TREE, never on the real one
// ⚠ [DOC] rule 20: `go test -overlay` does NOT reach a source scan.
func TestW8TradeOrderByScanHasFourProducedControls(t *testing.T) {
	const deviant = "ORDER BY node_epoch, site_id, seq"
	const canonical = "ORDER BY site_id, node_epoch, seq"

	// --- CONTROL 1: the reordered spelling, in the composition const group.
	c1 := w8ScanTradeOrderBy(t, w8PlantTree(t, map[string]string{
		"pkg/compose.go": "package pkg\n\nconst (\n\ttradeAmendmentCompositionSQL = \"SELECT site_id, node_epoch, seq, trade_id, cleared FROM trade_amendment WHERE trade_id = ? " + deviant + "\"\n)\n",
	})+"/pkg")
	if !s56PlantTypeChecked(t, "W8-18(b) CONTROL 1 (reordered spelling in the composition group)", c1.Diags) {
		return
	}
	if len(c1.Faults) == 0 {
		t.Fatalf("W8-18(b) CONTROL 1 FAILED: the reordered spelling %q planted in a trade composition constant was NOT reported. "+
			"A scan that cannot see its own plant proves nothing, and this is the spelling W3-m17 names", deviant)
	}

	// --- CONTROL 2: the OUT-OF-GROUP HOIST. --------------------------------
	// The same deviant spelling, declared in a constant OUTSIDE the
	// composition group and in a different file of the same package. A scan
	// that walks only one const block, or only one file, misses it.
	c2 := w8ScanTradeOrderBy(t, w8PlantTree(t, map[string]string{
		"pkg/compose.go": "package pkg\n\nconst tradeAmendmentCompositionSQL = tradeSelect + \" \" + tradeOrder\n",
		"pkg/order.go":   "package pkg\n\nconst tradeSelect = \"SELECT site_id, node_epoch, seq FROM trade_amendment WHERE trade_id = ?\"\n\nconst tradeOrder = \"" + deviant + "\"\n",
	})+"/pkg")
	if !s56PlantTypeChecked(t, "W8-18(b) CONTROL 2 (out-of-group hoist)", c2.Diags) {
		return
	}
	if len(c2.Faults) == 0 {
		t.Fatalf("W8-18(b) CONTROL 2 FAILED: the deviant spelling HOISTED into a constant outside the composition group was NOT reported. " +
			"This is the constant-split escape the RESOLVED-VALUE instrument exists for: the composition constant carries no ORDER BY text of its own, " +
			"so a literal-containment filter joins nothing and passes")
	}

	// --- CONTROL 3: a COMMENT-ONLY occurrence counts ZERO. ------------------
	c3 := w8ScanTradeOrderBy(t, w8PlantTree(t, map[string]string{
		"pkg/compose.go": "package pkg\n\n// A trade_amendment read must never be spelled " + deviant + " — see W3-m17.\nvar unrelated = 1\n",
	})+"/pkg")
	if !s56PlantTypeChecked(t, "W8-18(b) CONTROL 3 (comment-only occurrence)", c3.Diags) {
		return
	}
	if len(c3.Faults) != 0 || c3.Live != 0 {
		t.Fatalf("W8-18(b) CONTROL 3 FAILED: a COMMENT narrating the deviant spelling was scored %d fault(s) and %d live literal(s), want 0 and 0. "+
			"A comment is not an ordering literal; a scan that counts one has a filter wider than its subject, which is the same class from the other side. Faults: %v",
			len(c3.Faults), c3.Live, c3.Faults)
	}

	// --- CONTROL 3b: a COMPLIANT plant makes the LIVENESS COUNTER RISE. -----
	// ⚠ THIS IS THE ARM THAT CATCHES A SCAN SELECTING NOTHING. Controls 1
	// and 2 both assert "a fault is reported"; jointly they are satisfied by
	// an instrument that reports faults for the wrong reason and never
	// examines a healthy literal at all.
	c3b := w8ScanTradeOrderBy(t, w8PlantTree(t, map[string]string{
		"pkg/compose.go": "package pkg\n\nconst tradeAmendmentCompositionSQL = \"SELECT site_id, node_epoch, seq, trade_id, cleared FROM trade_amendment WHERE trade_id = ? " + canonical + "\"\n",
	})+"/pkg")
	if !s56PlantTypeChecked(t, "W8-18(b) CONTROL 3b (compliant plant raises liveness)", c3b.Diags) {
		return
	}
	if c3b.Live == 0 {
		t.Fatalf("W8-18(b) CONTROL 3b FAILED: a COMPLIANT trade composition constant spelled %q did NOT raise the liveness counter (live=%d, faults=%v). "+
			"The scan is selecting nothing: it would report zero faults over a tree with no trade code in it at all, and that green is indistinguishable from a correct one",
			canonical, c3b.Live, c3b.Faults)
	}
	if len(c3b.Faults) != 0 {
		t.Fatalf("W8-18(b) CONTROL 3b FAILED (the other direction): the CANONICAL spelling %q was reported as a fault: %v", canonical, c3b.Faults)
	}
}

// TestW8TradeCompositionSQLIsExaminedByTheScan is the liveness assertion over
// the REAL tree — W8-18(b)'s per-table liveness arm.
//
// PRE-CHUNK RED: produced. internal/domain declares no package-level constant
// whose RESOLVED value names trade_amendment, so the scan examines nothing and
// this reds. ⚠ That is the point: a scan whose subject does not exist yet is
// not evidence, and the criterion's residual clause — "a build that moves it
// to a var has left the scan's reach, which is a FINDING, never a pass" —
// needs a liveness arm to be checkable at all.
func TestW8TradeCompositionSQLIsExaminedByTheScan(t *testing.T) {
	root := w1ModuleRoot(t)
	res := w8ScanTradeOrderBy(t, filepath.Join(root, "internal", "domain"))
	s56LogTree(t, "w8ScanTradeOrderBy", res.Diags)
	if len(res.Faults) != 0 {
		t.Errorf("W8-18(b): trade_amendment composition constants carry a non-canonical ORDER BY:\n%s",
			strings.Join(res.Faults, "\n"))
	}
	if res.Live == 0 {
		t.Fatalf("W8-18(b) LIVENESS: the resolved-value scan examined ZERO trade_amendment ordering literals in internal/domain. "+
			"The composition SQL W8-18(b) asserts is a PACKAGE-LEVEL CONST does not exist there, so this scan currently "+
			"reports a clean pass over nothing at all. ⚠ Under the criterion's own residual clause a composition SQL held "+
			"in a `var`, in a function-local const, or assembled by Sprintf is OUTSIDE this instrument — and that is a "+
			"FINDING, never a pass.\n⟦§56⟧ what this scan type-checked, so a ZERO LIVE is never confused with a tree that did "+
			"not compile:\n%s", res.Diags.Report())
	}
}

// ---------------------------------------------------------------------------
// W8-28 — the consumer scan's surface is EXTENDED, not re-created
// ---------------------------------------------------------------------------

// w8TradeConsumerIdents / w8TradeConsumerStrings are W8-28's additions,
// stated here as the criterion states them: "trade_amendment and TradeAmended
// join TestW3ConsumerScan's forbiddenIdents; trade_amendment and trade_amended
// join forbiddenInStrings."
var (
	w8TradeConsumerIdents  = map[string]bool{"trade_amendment": true, "TradeAmended": true}
	w8TradeConsumerStrings = []string{"trade_amendment", "trade_amended"}
)

// TestW8NoNonDomainPackageReadsTheTradeSideTable is W8-28's PROPERTY.
//
// "No package outside internal/domain reads the trade side table or its
// event-type constants — the sanctioned readers are the fold and the
// resolver, both in internal/domain. The allowlist stays EMPTY and
// load-bearing: any exception is argued at the gate, in that list, in the
// open."
//
// ⚠ WHY W8 IS THE CHUNK THAT MOST NEEDS THIS: it builds FOUR non-domain
// consumers — a web authoring surface, a merge surface, a gate trade control,
// and an edit to cmd/dumpcompare. Absent the extension, any of them may read
// trade_amendment or the trade_amended constant directly and nothing in the
// project detects it.
//
// ⟦NO PRE-CHUNK RED — the identifiers do not exist yet, so the clean pass
// below is VACUOUS and is not evidence.⟧ Its evidence is the (m9) plant,
// produced FIRST, below.
func TestW8NoNonDomainPackageReadsTheTradeSideTable(t *testing.T) {
	// --- CONTROL FIRST — (m9)'s trade analogue, produced. ------------------
	planted := "package sneaky\n\n" +
		"const q = \"SELECT phone_day FROM trade_amendment WHERE trade_id = ?\"\n\n" +
		"func decide(typ string) bool { return typ == \"trade_amended\" }\n"
	hits := w1ScanGoTokens(t, "planted/tradeconsumer.go", []byte(planted), w8TradeConsumerIdents, w8TradeConsumerStrings)
	if len(hits) < 2 {
		t.Fatalf("W8-28 CONTROL FAILED ((m9)'s trade analogue): the scan found %d hit(s) in the planted non-domain consumer, want >= 2 "+
			"(the SQL identifier and the event-type constant). A scan that cannot detect its plant proves nothing, and the clean pass "+
			"below would be a green over a subject that does not exist. Hits: %v", len(hits), hits)
	}
	if extra := w1ScanGoTokens(t, "planted/clean.go", []byte("package fine\n\nvar ok = true\n"), w8TradeConsumerIdents, w8TradeConsumerStrings); len(extra) != 0 {
		t.Fatalf("W8-28 CONTROL: the scan flagged a clean file: %v", extra)
	}

	// --- the real pass over every non-domain package -----------------------
	allowlist := map[string]bool{} // EMPTY and LOAD-BEARING (W5d-15, carried)
	var found []w1Hit
	for _, path := range w3NonDomainGoFiles(t) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range w1ScanGoTokens(t, path, src, w8TradeConsumerIdents, w8TradeConsumerStrings) {
			if !allowlist[h.File] {
				found = append(found, h)
			}
		}
	}
	if len(found) != 0 {
		t.Fatalf("W8-28 ((m9)'s trade analogue): package(s) outside internal/domain read the trade_amendment side table or its "+
			"event-type constants. Every consumer resolves through the resolver/composed read path, never the table. "+
			"⚠ The allowlist is EMPTY and load-bearing: an exception is argued at the gate, in that list, in the open — "+
			"never an entry made in passing:\n%v", found)
	}
}

// TestW8ConsumerScanSurfaceIsExtendedNotRecreated asserts W8-28's OTHER half:
// that the landed TestW3ConsumerScan's own surface gained trade, rather than
// W8 standing up a second scan beside it.
//
// The criterion is explicit that this is an EXTENSION of a locked chunk's
// instrument (W3-14 → W5d-15 → W8) under W8-A5's bounded grant — "the const
// pair at internal/store/w3_source_scans_test.go:62-67 and TestW3ConsumerScan's
// use of it, and NOTHING ELSE in that file".
//
// ⚠⚠ THE ORDERING SCAN IS NOT WIDENED THERE — W8-A5(ii), same decision as
// W5d's: "widening a filter that selects nothing selects nothing for trade
// too". That surface is W8-18(b)'s (w5dScopedSideTables), a different edit in
// a different file. A BUILDER WHO WIDENS BOTH HAS EXCEEDED THIS GRANT, and
// this test asserts the boundary in both directions.
//
// ⚠ This test may not edit w3_source_scans_test.go (a test-writer modifies no
// existing test), so it asserts the widening from OUTSIDE, over the file's
// bytes. PRE-CHUNK RED: produced — the file carries person and company and no
// trade at all.
func TestW8ConsumerScanSurfaceIsExtendedNotRecreated(t *testing.T) {
	root := w1ModuleRoot(t)
	path := filepath.Join(root, "internal", "store", "w3_source_scans_test.go")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("PRECONDITION: W8-28 names %s as the file whose const pair and TestW3ConsumerScan are extended, and it could not be read: %v", path, err)
	}
	text := string(src)

	// PRECONDITION: the file is the one the criterion names — it carries the
	// two landed pairs the grant describes. Without this the absence below
	// could mean the surface moved rather than that it was never widened.
	for _, must := range []string{"w3SideTableIdent", "w5dSideTableIdentInScan", "TestW3ConsumerScan"} {
		if !strings.Contains(text, must) {
			t.Fatalf("PRECONDITION: %s does not carry %q — W8-28's grant is bounded to THIS file's const pair and "+
				"TestW3ConsumerScan's use of it; if that surface moved, this assertion is about the wrong file. "+
				"This is the INSTRUMENT, not W8", path, must)
		}
	}

	var missing []string
	for _, must := range []string{"trade_amendment", "trade_amended", "TradeAmended"} {
		if !strings.Contains(text, must) {
			missing = append(missing, must)
		}
	}
	if len(missing) != 0 {
		t.Errorf("W8-28: %s does not carry %v. The criterion is that trade_amendment and TradeAmended join "+
			"TestW3ConsumerScan's forbiddenIdents, and trade_amendment and trade_amended join forbiddenInStrings — "+
			"the consumer scan's surface is EXTENDED, not re-created. W8 builds FOUR non-domain consumers "+
			"(a web authoring surface, a merge surface, a gate trade control, and an edit to cmd/dumpcompare); "+
			"absent the extension any of them may read the side table directly and nothing detects it.\n"+
			"⚠ The grant (W8-A5) is bounded to the const pair and TestW3ConsumerScan's use of it, and NOTHING ELSE "+
			"in that file — and the ORDERING scan in it is deliberately NOT widened (that is W8-18(b)'s "+
			"w5dScopedSideTables, a different edit in a different file). A builder who widens both has EXCEEDED the grant",
			path, missing)
	}
}

// w8PlantTree writes named sources into a fresh scratch tree, preserving the
// relative directories, and returns its root. The scan is then run over the
// ROOT — the control is never handed the planted file's name.
//
// ⚠ [DOC] BUILDER_BRIEF_RULES rule 20: `go test -overlay` does NOT reach a
// source scan, so every control here mutates a REAL ON-DISK COPY. The real
// tree is never edited by this file.
func w8PlantTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, src := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
