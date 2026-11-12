// store_boundary_scan_test.go — nothing outside internal/store queries
// event_log. The ONE audit read over the log is store.EachEvent, and a
// second one written anywhere else fails HERE, by file and function, instead
// of being found in review.
//
// THE RULE. Three design documents say the same sentence: `store.EachEvent`
// is "the ONE audit read over the log" (CONSOLIDATED-PLAN-v1.md Amendment 2
// and R-11; PLAN-SEC-A-schema.md Amendment 2; PLAN-SEC-B-muster-print.md
// §12.3), and stream.go's own doc comment ends "There must not be a second."
//
// WHY THIS FILE EXISTS NOW. The analytics MVP's first cut read the log with
// a raw query from package domain (reports/witness/ANALYTICS-MVP.md,
// REVIEW-ANALYTICS.md). Nothing saw it: W1's guard 3b — the only reader
// allowlist in the module (w1_eventtime_gone_test.go) — scans internal/store
// ONLY, by design, because it judges which STORE functions may order or
// filter by the wall-time columns. A reader outside the store is outside
// its field of view. The defect was fixed by MOVING the read (visit_report.go
// now replays through EachEvent), not by adding a check, so the next author
// who writes `FROM event_log` in package domain or web gets no signal at
// all. This file is that signal.
//
// RELATION TO GUARD 3b. The two partition the module at the directory line
// and do not overlap: 3b, inside internal/store, allowlists BY FUNCTION NAME
// which readers may use the time columns in a WHERE/ORDER BY; this file,
// outside internal/store, forbids naming the TABLE in SQL at all, with no
// allowlist and an empty exemption map. Neither reads the other's territory.
// (A sibling fix to 3b's blindness to a time filter appended as a separate
// string fragment is a different defect in that guard; this file already
// matches a bare fragment — see plantedFragmentNoVerb in the control.)
//
// MECHANISM. go/parser over every non-test .go file under the module root
// whose directory is not exactly internal/store (cmd/, internal/…, and
// whatever a future binary adds — the walk is derived, not a list). Every
// string literal, raw or interpreted, is unquoted and tested for `event_log`
// in SQL TABLE POSITION: immediately after FROM, JOIN, INTO or UPDATE,
// through whitespace including newlines, as a whole identifier, optionally
// quoted (`"event_log"`, `[event_log]`) or schema-qualified
// (`main.event_log`), identifier case-insensitive. A hit is attributed to
// its enclosing function (receiver.method for a method, "<package level>"
// for a const or var). A finding fails unless it is written down in
// storeBoundaryExemptions with a reason, and a stale exemption fails too, so
// the list cannot rot into a rule that guards nothing.
//
// ⚠ THE KEYWORD IS MATCHED UPPERCASE ON PURPOSE. Every live event_log
// statement in the module writes its keywords in capitals (12 of 12 reads
// and the one INSERT in internal/store's non-test files at 5b7f861, zero in
// lowercase), and cmd/sitectl/main.go carries the words
// "derived from event_log" INSIDE A STRING LITERAL — the -report flag's help
// text. A case-insensitive keyword reds on that prose, and the first
// exemption written for a help string is the moment the guard stops being
// about SQL. So a lowercase `select … from event_log` is a documented miss,
// pinned by the control (plantedLowercaseSQL) so that widening the match
// later forces this paragraph to be rewritten with it.
//
// ANTI-VACUITY. Before the outside scan's silence counts for anything, the
// SAME detector runs over internal/store and must find Store.EachEvent's own
// query; if it cannot see the sanctioned read it cannot see an unsanctioned
// one, and the test is fatal, not green. The walk must find directories on
// both sides of the line, every file must parse, and the control below
// plants every shape it claims to catch into a copied real tree handed to
// the scan as a DIRECTORY, never by name.
//
// WHAT IT CATCHES (each proven by the planted control): SELECT … FROM
// event_log in a raw or interpreted literal, on one line or split across
// lines (the EachEvent shape itself); a FROM fragment carrying no SELECT;
// JOIN event_log; a subquery; DELETE FROM; INSERT INTO; UPDATE; a quoted or
// schema-qualified identifier; a mixed-case identifier; a package-level
// const; a literal inside a closure or `go` statement; a method.
//
// WHAT IT WOULD MISS: lowercase keywords (above); a table name assembled
// from a separate literal, a variable, or fmt.Sprintf("… FROM %s", …); a
// read inside a _test.go file (deliberately — witnesses read the ground
// truth directly, and at 5b7f861 seven do, in internal/deadline/
// w9c_seams_test.go, internal/web/w9c_seams_test.go, internal/domain/
// d92_dominance_oracle_test.go and w4_correction_chain_oracle_test.go); SQL
// held outside Go (a deploy/ shell script driving sqlite3 — none at
// 5b7f861); a VIEW over event_log declared inside the store and read by its
// own name outside it (none exist); schema introspection (PRAGMA,
// sqlite_master); a store method other than EachEvent that hands rows out
// (Events, Epochs) — that is guard 3b's allowlist, not this file's; and a
// SQL comment or prose that writes FROM in capitals before event_log, which
// is the false-positive direction, not a miss. Three obscure shapes the
// reviewer measured (REVIEW-STORE-BOUNDARY.md §2), none plausible here: a
// schema qualifier other than main (`FROM audit.event_log`), a quoted
// schema (`FROM "main"."event_log"`), and a SQL comment between keyword and
// table (`FROM /* c */ event_log`). A future subpackage under
// internal/store is OUTSIDE this boundary and would red here — deliberately,
// because guard 3b does not walk subdirectories either, so it would
// otherwise escape both.
//
// FALSE-POSITIVE COST: a `-- FROM event_log` comment inside a SQL literal, or
// an uppercase FROM in a prose literal. Each is exemptable by
// file#function, and each exemption is a visible, reviewable diff.
//
// ⚠ EXEMPTION GRANULARITY IS file#function, NOT per literal. A second
// offending literal in an exempt function rides under the one entry. The
// control below copies internal/domain and cmd/sitectl as its negative
// corpus and judges any hit there by the SAME map, keyed by module path, so
// one entry greens the whole package wherever the finding sits; the first
// cut red the control regardless of the map, and REVIEW-STORE-BOUNDARY.md
// §1 caught it.
package store

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

// storeBoundaryExemptions is the WRITTEN opt-out. Key:
// "<module-relative file>#<enclosing function>" ("<package level>" for a
// const or var). Every entry must match a live finding or the test fails
// (stale exemptions are how an allowlist becomes a blindfold). EMPTY at
// 5b7f861: no production code outside internal/store names the table in
// SQL, and none should — an entry here is a decision to be read in review,
// with the reason beside it.
var storeBoundaryExemptions = map[string]string{}

// sbStoreDir is the boundary: exactly this directory, not its descendants.
var sbStoreDir = filepath.Join("internal", "store")

// sbTablePosition matches event_log in SQL table position. The keyword is
// case-sensitive (see the header); the identifier is not, because SQLite's
// is not.
var sbTablePosition = regexp.MustCompile(`\b(FROM|JOIN|INTO|UPDATE)\s+(?:(?i:main)\s*\.\s*)?["` + "`" + `\[]?(?i:event_log)\b`)

// sbHit is one string literal naming event_log in table position.
type sbHit struct {
	File    string // module-relative, forward slashes; base name outside the module (the control's copied tree)
	Line    int    // of the matched text inside a raw literal; of the literal for an interpreted one
	Func    string // enclosing function, receiver.method for a method, "<package level>" for a const/var
	Keyword string // FROM, JOIN, INTO or UPDATE
	Snippet string // the matched text, whitespace collapsed
}

func (h sbHit) key() string { return h.File + "#" + h.Func }

func (h sbHit) String() string {
	return fmt.Sprintf("%s:%d %s — `%s`", h.File, h.Line, h.Func, h.Snippet)
}

// sbModuleDirs lists every directory under the module root that holds a
// non-test .go file, sorted. Fatal when the walk fails or finds nothing: a
// scan with no surface is not a clean tree.
func sbModuleDirs(t *testing.T, root string) []string {
	t.Helper()
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if p != root && (strings.HasPrefix(name, ".") || name == "testdata" || name == "node_modules" || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			seen[filepath.Dir(p)] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the module root %s: %v", root, err)
	}
	if len(seen) == 0 {
		t.Fatalf("the walk under %s found no directory holding a non-test .go file — the scan has no surface", root)
	}
	var dirs []string
	for d := range seen {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	return dirs
}

// sbSourceFiles lists the non-test .go files directly inside dirs, sorted.
func sbSourceFiles(t *testing.T, dirs []string) []string {
	t.Helper()
	var out []string
	for _, dir := range dirs {
		paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range paths {
			if !strings.HasSuffix(p, "_test.go") {
				out = append(out, p)
			}
		}
	}
	sort.Strings(out)
	return out
}

// sbDisplayPath is module-relative with forward slashes, or the base name
// for a file outside the module.
func sbDisplayPath(root, p string) string {
	if rel, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.Base(p)
}

func sbRecvName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.StarExpr:
		return sbRecvName(x.X)
	case *ast.Ident:
		return x.Name
	}
	return "?"
}

// sbScan parses every non-test .go file directly inside dirs and returns
// every string literal that names event_log in SQL table position. It takes
// ONLY directories — the control hands it a copied tree carrying a plant
// this file never names. Fatal when a directory list yields no file, or a
// file does not parse.
func sbScan(t *testing.T, root string, dirs []string) []sbHit {
	t.Helper()
	files := sbSourceFiles(t, dirs)
	if len(files) == 0 {
		t.Fatalf("no non-test .go file directly inside %v — nothing was scanned", dirs)
	}
	var hits []sbHit
	for _, p := range files {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		display := sbDisplayPath(root, p)
		for _, d := range f.Decls {
			encl := "<package level>"
			if fd, ok := d.(*ast.FuncDecl); ok {
				encl = fd.Name.Name
				if fd.Recv != nil && len(fd.Recv.List) == 1 {
					encl = sbRecvName(fd.Recv.List[0].Type) + "." + encl
				}
			}
			ast.Inspect(d, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				text, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("%s:%d: the parser accepted a string literal strconv.Unquote rejects: %v", display, fset.Position(lit.Pos()).Line, err)
				}
				for _, m := range sbTablePosition.FindAllStringSubmatchIndex(text, -1) {
					line := fset.Position(lit.Pos()).Line
					if lit.Value[0] == '`' {
						line += strings.Count(text[:m[0]], "\n")
					}
					hits = append(hits, sbHit{File: display, Line: line, Func: encl,
						Keyword: text[m[2]:m[3]], Snippet: strings.Join(strings.Fields(text[m[0]:m[1]]), " ")})
				}
				return true
			})
		}
	}
	return hits
}

// sbReconcile judges hits against the written exemptions: a hit with no
// entry is a failure, a hit with one is exempt, and an entry no hit uses is
// stale. Pure, so the stale rule is provable without touching the tree.
func sbReconcile(hits []sbHit, exemptions map[string]string) (failures, exempt, stale []string) {
	used := map[string]bool{}
	for _, h := range hits {
		if reason, ok := exemptions[h.key()]; ok {
			used[h.key()] = true
			exempt = append(exempt, h.String()+" — "+reason)
			continue
		}
		failures = append(failures, h.String())
	}
	var keys []string
	for k := range exemptions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !used[k] {
			stale = append(stale, k)
		}
	}
	return failures, exempt, stale
}

// TestNoSQLOutsideInternalStoreNamesEventLog is the scan over the real
// tree. GREEN at 5b7f861 with an empty exemption map: the only event_log
// SQL outside internal/store sits in _test.go files, which are out of scope
// (header), and visit_report.go replays through EachEvent.
func TestNoSQLOutsideInternalStoreNamesEventLog(t *testing.T) {
	root := w1ModuleRoot(t)
	dirs := sbModuleDirs(t, root)
	storeDir := filepath.Clean(filepath.Join(root, sbStoreDir))
	var inside, outside []string
	for _, d := range dirs {
		if filepath.Clean(d) == storeDir {
			inside = append(inside, d)
		} else {
			outside = append(outside, d)
		}
	}
	if len(inside) != 1 {
		t.Fatalf("internal/store is not among the directories the walk found (%v) — the boundary this file guards does not exist where it looks", dirs)
	}
	if len(outside) == 0 {
		t.Fatal("no directory outside internal/store holds a non-test .go file — there is nothing to guard, which is not a clean tree")
	}

	// LIVE CONTROL: the detector must see the sanctioned read in the real
	// store before its silence elsewhere counts.
	sanctioned := sbScan(t, root, inside)
	if len(sanctioned) == 0 {
		t.Fatal("the detector found NO event_log SQL inside internal/store itself — it cannot see the shape it guards, so its silence outside means nothing (the keyword match is uppercase FROM/JOIN/INTO/UPDATE; did the store's SQL change style?)")
	}
	readers := map[string]int{}
	for _, h := range sanctioned {
		readers[h.Func]++
	}
	if readers["Store.EachEvent"] == 0 {
		t.Fatalf("the detector sees %d event_log SQL literal(s) in internal/store but none in Store.EachEvent — the ONE sanctioned audit read (stream.go) is gone, renamed, or written in a shape this file cannot see; functions seen: %v", len(sanctioned), readers)
	}
	t.Logf("inside internal/store (guard 3b's territory, not judged here): %d event_log SQL literal(s) in %d function(s): %v", len(sanctioned), len(readers), readers)

	hits := sbScan(t, root, outside)
	failures, exempt, stale := sbReconcile(hits, storeBoundaryExemptions)
	for _, e := range exempt {
		t.Logf("EXEMPT (written): %s", e)
	}
	for _, f := range failures {
		t.Errorf("STORE BOUNDARY: %s — names event_log in SQL table position outside internal/store; the ONE audit read over the log is store.EachEvent (internal/store/stream.go, pre-named on W1 guard 3b's reader allowlist): read through the store, or write an exemption in storeBoundaryExemptions with the reason", f)
	}
	for _, k := range stale {
		t.Errorf("stale exemption %q (%s) matches no finding — remove it, or the list is a blindfold", k, storeBoundaryExemptions[k])
	}
	t.Logf("outside internal/store: %d file(s) in %d directories scanned; %d finding(s), %d exempt, %d stale", len(sbSourceFiles(t, outside)), len(outside), len(failures), len(exempt), len(stale))
}

// TestStoreBoundaryScanControlDetectsPlantedEventLogSQL proves the
// instrument before its green counts: a copy of two real packages' non-test
// files — internal/domain, where the raw read was first found and whose
// comments say "from event_log", and cmd/sitectl, whose -report flag help
// says "derived from event_log" inside a STRING LITERAL — plus one planted
// file carrying every shape the header claims to catch, every shape it must
// not flag, and the two misses it documents, handed to the scan as a
// DIRECTORY. The plant need only PARSE; nothing compiles it. A hit in the
// copied real files is judged by storeBoundaryExemptions under its module
// path, so a written exemption in either corpus package greens this test
// too — otherwise the control would red forever on a decision the real-tree
// test had already accepted.
func TestStoreBoundaryScanControlDetectsPlantedEventLogSQL(t *testing.T) {
	root := w1ModuleRoot(t)
	tmp := t.TempDir()
	// origin maps each copied file's base name back to its module path, so
	// a hit in the corpus can be judged by the same written exemptions as
	// the real tree (the copy flattens the path; the exemption key does not).
	origin := map[string]string{}
	for _, dir := range []string{filepath.Join(root, "internal", "domain"), filepath.Join(root, "cmd", "sitectl")} {
		for _, p := range sbSourceFiles(t, []string{dir}) {
			src, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			base := filepath.Base(p)
			if _, dup := origin[base]; dup {
				t.Fatalf("control corpus: base name %s occurs in both packages; the copied tree would lose one — pick a different negative corpus", base)
			}
			if err := os.WriteFile(filepath.Join(tmp, base), src, 0o644); err != nil {
				t.Fatal(err)
			}
			origin[base] = sbDisplayPath(root, p)
		}
	}
	if len(origin) == 0 {
		t.Fatal("control corpus: nothing copied")
	}
	// The prose negative that fixes the keyword's case must actually be in
	// the corpus, or this control no longer proves what the header says.
	flagSrc, err := os.ReadFile(filepath.Join(tmp, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(flagSrc), `from event_log`) {
		t.Fatal("control corpus: cmd/sitectl/main.go no longer carries the lowercase 'from event_log' prose literal — the negative that pins the keyword's case is gone; find another or rewrite the header")
	}

	plant := []byte(`package planted

import "database/sql"

// planted fixture: readers this file must catch, shapes it must not flag,
// and the two misses it documents.

const plantedConst = "SELECT payload FROM event_log ORDER BY seq"

type planted struct{ db *sql.DB }

func plantedSelect(db *sql.DB) {
	_, _ = db.Query(` + "`" + `SELECT seq FROM event_log WHERE site_id = ?` + "`" + `, "a")
}
func plantedMultilineRaw(db *sql.DB) {
	_, _ = db.Query(` + "`" + `SELECT site_id, seq
	        FROM event_log
	       WHERE seq >= ?` + "`" + `, 1)
}
func plantedInterpreted(db *sql.DB) {
	_ = db.QueryRow("SELECT count(*) FROM event_log")
}
func plantedInterpretedEscapedNewline(db *sql.DB) {
	_ = db.QueryRow("SELECT count(*)\n  FROM event_log")
}
func plantedFragmentNoVerb(db *sql.DB) {
	q := "SELECT seq " + "FROM event_log WHERE seq > ?"
	_, _ = db.Query(q, 1)
}
func plantedJoin(db *sql.DB) {
	_, _ = db.Query(` + "`" + `SELECT t.trip_id FROM trip t JOIN event_log e ON e.seq = t.seq` + "`" + `)
}
func plantedSubquery(db *sql.DB) {
	_, _ = db.Query(` + "`" + `SELECT * FROM (SELECT seq FROM event_log) s` + "`" + `)
}
func plantedDeleteFrom(db *sql.DB) {
	_, _ = db.Exec(` + "`" + `DELETE FROM event_log WHERE seq = ?` + "`" + `, 1)
}
func plantedInsertInto(db *sql.DB) {
	_, _ = db.Exec(` + "`" + `INSERT INTO event_log (seq) VALUES (?)` + "`" + `, 1)
}
func plantedUpdate(db *sql.DB) {
	_, _ = db.Exec(` + "`" + `UPDATE event_log SET payload = ? WHERE seq = ?` + "`" + `, "", 1)
}
func plantedQuotedIdent(db *sql.DB) {
	_, _ = db.Query(` + "`" + `SELECT seq FROM "event_log"` + "`" + `)
}
func plantedSchemaQualified(db *sql.DB) {
	_, _ = db.Query(` + "`" + `SELECT seq FROM main.event_log` + "`" + `)
}
func plantedMixedCaseIdent(db *sql.DB) {
	_, _ = db.Query(` + "`" + `SELECT seq FROM Event_Log` + "`" + `)
}
func plantedInClosure(db *sql.DB) {
	go func() { _, _ = db.Query(` + "`" + `SELECT seq FROM event_log` + "`" + `) }()
}
func (p *planted) plantedMethod() {
	_, _ = p.db.Query(` + "`" + `SELECT seq FROM event_log` + "`" + `)
}

// must NOT be flagged
func plantedCommentOnly(db *sql.DB) {
	// SELECT seq FROM event_log — a comment is not a query
	_ = db
}
func plantedProse() string {
	return "a read-only report derived from event_log, never from the dump"
}
func plantedLongerIdent(db *sql.DB) {
	_, _ = db.Query(` + "`" + `SELECT seq FROM event_log_archive` + "`" + `)
	_, _ = db.Query(` + "`" + `SELECT seq FROM my_event_log` + "`" + `)
}
func plantedOtherTable(db *sql.DB) {
	_, _ = db.Query(` + "`" + `SELECT applied_seq FROM watermark` + "`" + `)
}

// documented MISSES, pinned so that widening the detector rewrites the header
func plantedBareTableName(db *sql.DB) {
	table := "event_log"
	_, _ = db.Query("SELECT seq FROM " + table)
}
func plantedLowercaseSQL(db *sql.DB) {
	_, _ = db.Query(` + "`" + `select seq from event_log` + "`" + `)
}
`)
	if err := os.WriteFile(filepath.Join(tmp, "zz_planted_reader.go"), plant, 0o644); err != nil {
		t.Fatal(err)
	}

	hits := sbScan(t, root, []string{tmp})
	got := map[string][]sbHit{}
	planted := 0 // hits in the plant alone; an exempt corpus hit is not one
	for _, h := range hits {
		if h.File != "zz_planted_reader.go" {
			// A hit in the copied real corpus is judged by the SAME written
			// exemptions as the real tree, keyed by the file's module path:
			// an exempt finding is logged and the control stays green, so an
			// exemption written for internal/domain or cmd/sitectl greens
			// the whole package, not only the real-tree test (the first cut
			// red here regardless of the map — REVIEW-STORE-BOUNDARY.md §1).
			// Anything else is a control failure, and a real-tree failure too.
			if reason, ok := storeBoundaryExemptions[origin[h.File]+"#"+h.Func]; ok {
				t.Logf("EXEMPT (written) in the copied corpus: %s — %s", h, reason)
				continue
			}
			t.Errorf("the copied real corpus (%s) produced a finding with no written exemption: %s — either a real breach, which the real-tree test reds on too, or a false positive on real code, and a scan that reds on comments or prose will be silenced with exemptions", origin[h.File], h)
			continue
		}
		planted++
		got[h.Func] = append(got[h.Func], h)
	}
	want := map[string]string{
		"<package level>":                  "FROM",
		"plantedSelect":                    "FROM",
		"plantedMultilineRaw":              "FROM",
		"plantedInterpreted":               "FROM",
		"plantedInterpretedEscapedNewline": "FROM",
		"plantedFragmentNoVerb":            "FROM",
		"plantedJoin":                      "JOIN",
		"plantedSubquery":                  "FROM",
		"plantedDeleteFrom":                "FROM",
		"plantedInsertInto":                "INTO",
		"plantedUpdate":                    "UPDATE",
		"plantedQuotedIdent":               "FROM",
		"plantedSchemaQualified":           "FROM",
		"plantedMixedCaseIdent":            "FROM",
		"plantedInClosure":                 "FROM",
		"planted.plantedMethod":            "FROM",
	}
	for fn, kw := range want {
		hs, ok := got[fn]
		if !ok {
			t.Errorf("CONTROL: the scan never located the planted read in %s — it cannot be trusted to find a real one", fn)
			continue
		}
		if len(hs) != 1 || hs[0].Keyword != kw {
			t.Errorf("CONTROL: the planted read in %s produced %v, want exactly one hit on %s", fn, hs, kw)
		}
	}
	// The multi-line raw plant must be reported at the line of the FROM,
	// not the line the literal opens on — a finding that points at the
	// wrong line is the citation defect one level down.
	if hs := got["plantedMultilineRaw"]; len(hs) == 1 && hs[0].Line != 17 {
		t.Errorf("CONTROL: the multi-line raw plant was reported at line %d, want 17 (the FROM's own line; the literal opens on 16)", hs[0].Line)
	}
	for _, clean := range []string{"plantedCommentOnly", "plantedProse", "plantedLongerIdent", "plantedOtherTable"} {
		if hs, ok := got[clean]; ok {
			t.Errorf("CONTROL: %s is not a read and was flagged (%v) — a scan that reds on comments, prose or longer identifiers will be silenced with exemptions", clean, hs)
		}
	}
	for _, miss := range []string{"plantedBareTableName", "plantedLowercaseSQL"} {
		if hs, ok := got[miss]; ok {
			t.Errorf("CONTROL: %s is a DOCUMENTED MISS of this detector and was found anyway (%v) — the detector has widened; rewrite WHAT IT WOULD MISS in the header, and re-check that cmd/sitectl's prose literal still passes", miss, hs)
		}
	}
	if planted != len(want) {
		t.Errorf("CONTROL: %d hit(s) in the planted file, want %d — an unexpected function is being reported: %v", planted, len(want), got)
	}
}

// TestStoreBoundaryStaleExemptionIsItselfAFailure pins the reconciliation
// rule the real test enforces: a hit with no entry fails, a hit with an
// entry is exempt, and an entry with no hit is STALE and surfaces — so the
// exemption map cannot silently accumulate into a rule that guards nothing.
func TestStoreBoundaryStaleExemptionIsItselfAFailure(t *testing.T) {
	live := sbHit{File: "internal/planted/reader.go", Line: 7, Func: "readRaw", Keyword: "FROM", Snippet: "FROM event_log"}
	const gone = "internal/planted/gone.go#readRawOnceUponATime"
	failures, exempt, stale := sbReconcile([]sbHit{live}, map[string]string{
		live.key(): "a written reason",
		gone:       "the finding this covered was removed and the entry was not",
	})
	if len(failures) != 0 {
		t.Errorf("an exempt hit was reported as a failure: %v", failures)
	}
	if len(exempt) != 1 {
		t.Errorf("the exempt hit was not reported as exempt: %v", exempt)
	}
	if len(stale) != 1 || stale[0] != gone {
		t.Errorf("the stale entry did not surface: stale=%v, want exactly [%s]", stale, gone)
	}
	failures, exempt, stale = sbReconcile([]sbHit{live}, nil)
	if len(failures) != 1 || len(exempt) != 0 || len(stale) != 0 {
		t.Errorf("an unexempted hit must be exactly one failure: failures=%v exempt=%v stale=%v", failures, exempt, stale)
	}
	failures, exempt, stale = sbReconcile(nil, map[string]string{gone: "nothing left to cover"})
	if len(failures) != 0 || len(exempt) != 0 || len(stale) != 1 {
		t.Errorf("an entry over an empty finding set must be exactly one stale: failures=%v exempt=%v stale=%v", failures, exempt, stale)
	}
}
