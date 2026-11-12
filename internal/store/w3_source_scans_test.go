// w3_source_scans_test.go — W3-14 (the §7.6 consumer scan, landing WITH
// this chunk) and W3-11's source-level halves (exactly one ORDER BY
// spelling in composition; store.Key carries exactly one comparator).
// design/WINDOW-BUILD-PLAN-v1.md §3 W3, criteria LOCKED 2026-08-27.
//
// package store, DELIBERATELY (beside W1's scan machinery, which it
// reuses): these are file-system source scans referencing no unbuilt Go
// symbol, so they COMPILE AND RUN against the clean tree — the ORDER BY
// and Key-comparator checks are genuinely RED there (no composition file,
// no Less), and every scan proves it detects a PLANTED violation before
// any clean pass counts (§5's CONTROL row).
//
// KILLER MUTANTS: m20 (a planted read of person_amendment outside
// internal/domain — the scan's own control), m17's source half (a deviant
// ORDER BY spelling / a second Go comparator on Key).
package store

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// w3SideTableIdent is the side table's SQL identifier; w3EventTypeLit is
// the event-type constant's string value (SEAM W3-S-PAYLOAD's spelling).
//
// ⟦W5d, DR-5 / A-12 grant⟧ W5d's builder-and-test-writer grant covers this
// const pair and the two scans' use of it, and nothing else in this file.
// The identifier SET is widened to person + COMPANY for the CONSUMER scan
// below, which is W5d-15's "W3-14's landed scan gains company_amendment to
// its surface list".
//
// ⚠ The ORDERING scan in this file is deliberately NOT widened, and that is
// a decision rather than an omission: W5d-4 RESPECIFIES that instrument
// (scope by RESOLVED constant value through go/types; controls run through
// the walk AND the filter; liveness as a per-identifier counter over string
// literals), because this one's AST filter admits only *ast.BasicLits
// containing the table identifier and the house's M-1 constant split leaves
// the ORDER BY in a literal that carries no table name — so it examines no
// ordering literal at all. Widening a filter that selects nothing selects
// nothing for company too. The respecified scan is
// internal/store/w5d_source_scans_test.go's
// TestW5dOneOrderBySpellingInCompositionByResolvedValue, and it covers BOTH
// scoped tables. Measured: with W3-m5's epoch-blind spelling planted in the
// LIVE constant person_amend.go:261, the test below PASSES and the
// respecified scan reds on both composition constants.
//
// ⚠ DR-5's owed confirmation, MEASURED at 88dd6a6 BEFORE this widening:
// `grep -an 'person_amended|person_amendment' internal/web/*.go |
// grep -av _test.go` returns ZERO, and the identifier PersonAmended appears
// only in internal/domain — so the person-side surface needs neither
// literal in a non-domain package, and W5d-23's new web route needs neither
// company literal either. If a builder finds it does, that is a FINDING for
// the gate, not a workaround.
const (
	w3SideTableIdent = "person_amendment"
	w3EventTypeLit   = "person_amended"

	// W5d's side of the widened consumer surface.
	w5dSideTableIdentInScan = "company_amendment"
	w5dEventTypeLitInScan   = "company_amended"

	// ⟦LOCK — W8-28 discharge (the W8-A5 grant), 2026-09-03⟧ W8's side of the
	// same surface. The consumer scan is EXTENDED, never re-created: W8 builds
	// FOUR non-domain consumers of trade — a web authoring surface, a merge
	// surface, a gate trade control and an edit to cmd/dumpcompare — and
	// absent the extension any of them may read the side table directly and
	// NOTHING detects it.
	// ⚠ THE GRANT IS BOUNDED to this const pair and TestW3ConsumerScan's use
	// of it, and to nothing else in this file. The ORDERING scan is
	// deliberately NOT widened here — that is W8-18(b)'s w5dScopedSideTables,
	// a different edit in a different file — and a builder who widens both has
	// EXCEEDED the grant.
	w8SideTableIdentInScan = "trade_amendment"
	w8EventTypeLitInScan   = "trade_amended"
)

// w3NonDomainGoFiles walks the module for non-test .go files OUTSIDE
// internal/domain (the sanctioned readers — the fold and the resolver —
// live there and nowhere else).
func w3NonDomainGoFiles(t *testing.T) []string {
	t.Helper()
	root := w1ModuleRoot(t)
	domainDir := filepath.Join(root, "internal", "domain")
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if path == domainDir || info.Name() == "node_modules" || strings.HasPrefix(info.Name(), ".") && path != root {
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
	if len(files) == 0 {
		t.Fatal("W3-14: scan universe empty — the walk found no non-domain Go files, which cannot be right")
	}
	return files
}

// TestW3ConsumerScan — W3-14: no package outside internal/domain reads the
// person_amendment side table or its event-type constants (SQL
// identifiers and Go constants both). Sanctioned readers are the fold and
// the resolver (both in internal/domain); everything else resolves
// through Outcome / the composed read API or goes red. The scan FIRST
// proves it detects a planted violation (m20 — the control), then the
// clean pass counts. The allowlist below is EMPTY and load-bearing: any
// sanctioned exception must be argued at the gate, in this list, in the
// open.
func TestW3ConsumerScan(t *testing.T) {
	forbiddenIdents := map[string]bool{
		w3SideTableIdent:        true, // a Go identifier named after the table
		"PersonAmended":         true, // the payload type — building payloads outside domain bypasses the edge
		w5dSideTableIdentInScan: true, // ⟦W5d, DR-5/A-12 grant⟧ the company side table
		"CompanyAmended":        true, // ⟦W5d⟧ the company payload type
		w8SideTableIdentInScan:  true, // ⟦W8-28, the W8-A5 grant⟧ the trade side table
		"TradeAmended":          true, // ⟦W8-28⟧ the trade payload type
	}
	forbiddenInStrings := []string{
		w3SideTableIdent, w3EventTypeLit,
		w5dSideTableIdentInScan, w5dEventTypeLitInScan, // ⟦W5d, DR-5/A-12 grant⟧
		w8SideTableIdentInScan, w8EventTypeLitInScan, // ⟦W8-28, the W8-A5 grant⟧
	}
	allowlist := map[string]bool{} // EMPTY at W3 — extensions are gate rulings, not edits made in passing

	// --- CONTROL FIRST (m20): the planted consumer must be detected. ----
	planted := `package sneaky

const q = "SELECT phone FROM person_amendment WHERE person_id = ?"

func decide(typ string) bool { return typ == "person_amended" }
`
	hits := w1ScanGoTokens(t, "planted/consumer.go", []byte(planted), forbiddenIdents, forbiddenInStrings)
	if len(hits) < 2 {
		t.Fatalf("W3-14 CONTROL FAILED: the scan found %d hit(s) in the planted consumer, want >= 2 (the SQL identifier and the event-type constant) — a scan that cannot detect its plant proves nothing (m20). Hits: %v", len(hits), hits)
	}
	clean := "package fine\n\nvar ok = true\n"
	if extra := w1ScanGoTokens(t, "planted/clean.go", []byte(clean), forbiddenIdents, forbiddenInStrings); len(extra) != 0 {
		t.Fatalf("W3-14 CONTROL: the scan flagged a clean file: %v", extra)
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
		t.Fatalf("W3-14 (m20): package(s) outside internal/domain read the person_amendment side table or its event-type constants — every consumer resolves through the resolver/composed read path, never the table:\n%v", found)
	}
}

// --- W3-11's source halves ----------------------------------------------

// w3CanonicalOrderBy matches THE one composition ordering spelling: SQL
// ORDER BY site_id, node_epoch, seq (optionally table-qualified),
// epoch-major, seq last.
var w3CanonicalOrderBy = regexp.MustCompile(`(?i)ORDER\s+BY\s+(?:[a-z_]+\.)?site_id\s*,\s*(?:[a-z_]+\.)?node_epoch\s*,\s*(?:[a-z_]+\.)?seq\b`)

// w3AnyOrderBy captures each ORDER BY clause's column tail.
var w3AnyOrderBy = regexp.MustCompile(`(?i)ORDER\s+BY\s+([a-z_.,\s()]+)`)

// w3DeviantOrderBy reports ORDER BY clauses in lit that touch two or more
// of the key columns but are NOT the canonical spelling — the drift shape
// m17 names (epoch-blind "site_id, seq" included).
func w3DeviantOrderBy(lit string) []string {
	var bad []string
	for _, m := range w3AnyOrderBy.FindAllString(lit, -1) {
		tail := strings.ToLower(m)
		n := 0
		for _, col := range []string{"site_id", "node_epoch", "seq"} {
			if strings.Contains(tail, col) {
				n++
			}
		}
		if n >= 2 && !w3CanonicalOrderBy.MatchString(m) {
			bad = append(bad, m)
		}
	}
	return bad
}

// TestW3OneOrderBySpellingInComposition — W3-11's SQL half: the
// composition (the code that reads person_amendment) carries the
// canonical `ORDER BY site_id, node_epoch, seq` spelling, and NO Go
// string anywhere in the app orders over the key columns any other way.
// RED on the clean tree: no non-test file reads person_amendment at all.
// Control: both planted deviants are detected before the clean pass
// counts.
func TestW3OneOrderBySpellingInComposition(t *testing.T) {
	// CONTROL: a reordered spelling and the epoch-blind spelling must trip
	// the deviant detector; the canonical one must not.
	if got := w3DeviantOrderBy(`"SELECT * FROM person_amendment ORDER BY node_epoch, site_id, seq"`); len(got) == 0 {
		t.Fatalf("W3-11 CONTROL FAILED: the deviant detector missed a reordered ORDER BY — it cannot catch m17's drift")
	}
	if got := w3DeviantOrderBy(`"SELECT * FROM person_amendment ORDER BY site_id, seq"`); len(got) == 0 {
		t.Fatalf("W3-11 CONTROL FAILED: the deviant detector missed the EPOCH-BLIND ordering (site_id, seq) — m5's spelling")
	}
	if got := w3DeviantOrderBy(`"SELECT * FROM person_amendment ORDER BY site_id, node_epoch, seq"`); len(got) != 0 {
		t.Fatalf("W3-11 CONTROL: the detector flagged the canonical spelling itself: %v", got)
	}

	root := w1ModuleRoot(t)
	var goFiles []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			goFiles = append(goFiles, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(goFiles)

	canonicalInComposition := 0
	var deviants []string
	for _, path := range goFiles {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(src)
		if strings.Contains(text, w3SideTableIdent) && w3CanonicalOrderBy.MatchString(text) {
			canonicalInComposition++
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			// Scope: the COMPOSITION's own SQL — string literals that read
			// the side table. (Whole-app ordering uniformity is NOT
			// claimable: site-scoped queries legitimately order by
			// (node_epoch, seq) with site_id already bound — measured at
			// trip.go:704/970 on the clean tree. The criterion's scope is
			// "in composition", and that is what this enforces.)
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING && strings.Contains(lit.Value, w3SideTableIdent) {
				for _, d := range w3DeviantOrderBy(lit.Value) {
					deviants = append(deviants, path+": "+d)
				}
			}
			return true
		})
	}
	if len(deviants) != 0 {
		t.Fatalf("W3-11 (m17): deviant key-column ORDER BY spelling(s) — one ordering SPEC means ONE SQL spelling (`ORDER BY site_id, node_epoch, seq`) beside the one Go comparator:\n%s", strings.Join(deviants, "\n"))
	}
	if canonicalInComposition == 0 {
		t.Fatalf("W3-11: no non-test file both reads %s and carries the canonical `ORDER BY site_id, node_epoch, seq` — the ascending-key composition is unbuilt or spelled differently", w3SideTableIdent)
	}
}

// TestW3KeyCarriesExactlyOneComparator — W3-11's Go half at source level:
// store.Key's method set is EXACTLY {String, Less} — Less exists (RED on
// the clean tree) and no second comparator (Before/Compare/LessEq/…) ever
// joins it. The census's own liveness control is String: a walker that
// finds no methods at all is a broken walker, not a clean pass.
func TestW3KeyCarriesExactlyOneComparator(t *testing.T) {
	root := w1ModuleRoot(t)
	storeDir := filepath.Join(root, "internal", "store")
	methods := map[string]bool{}
	for _, path := range w1GoFiles(t, storeDir) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
				continue
			}
			recv := fn.Recv.List[0].Type
			if star, ok := recv.(*ast.StarExpr); ok {
				recv = star.X
			}
			if ident, ok := recv.(*ast.Ident); ok && ident.Name == "Key" {
				methods[fn.Name.Name] = true
			}
		}
	}
	if !methods["String"] {
		t.Fatalf("W3-11: the Key-method census found no String method — the census walker itself is broken (its liveness control), or Key changed shape")
	}
	if !methods["Less"] {
		t.Fatalf("W3-11 (m17/m5): store.Key.Less does not exist — the ONE Go comparator of the one ordering spec is unbuilt (VF §4.2: Less is added to store.Key, no other key type, no other comparator)")
	}
	var names []string
	for m := range methods {
		names = append(names, m)
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "Less,String" {
		t.Fatalf("W3-11 (m17): store.Key's method set is %v, want exactly [Less String] — a second comparator-shaped method on the ordering key is the two-implementations drift surface T-A exists to refuse", names)
	}
}
