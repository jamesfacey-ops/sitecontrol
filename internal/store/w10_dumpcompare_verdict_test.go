// w10_dumpcompare_verdict_test.go — W10-2(c) and W10-2(d)(ii), run against
// the REAL cmd/dumpcompare binary rather than argued from its source.
//
// Criteria source: design/WINDOW-BUILD-PLAN-v1.md §3, `### W10 — Window
// integration and rehearsal`, LOCKED 2026-09-03 (reports/W10-FOLD-RECORD.md,
// gate P-4). Written from the LOCKED CRITERION TEXT ONLY.
//
// Criteria carried here:
//
//   - W10-2(c) — "the dumpcompare verdict line is RECONCILED, because the
//     printed number is not the compared count". Its CONTROL, quoted in
//     full: "Take one dump pair, delete one compare-set key from BOTH JSON
//     files, re-run: observe exit 0, a stderr warning, and a verdict count
//     that exceeds the number compared. ⚠ This is stated as a control that
//     needs two files and seconds, precisely because a control that needs
//     the lab tends not to be run. Without it the reconciliation step is a
//     ritual."
//     ⟦G17, 2026-09-09⟧ ⟦was: "⭐ THAT IS WHAT THIS FILE IS. The control the
//     criterion specifies is executed here, on every run, by machine."⟧
//     ⭐ THE PROCEDURE STILL RUNS, ON EVERY RUN, BY MACHINE: deleting one
//     compare-set key from both dumps and re-running the real binary is
//     exactly what TestW10TheVerdictWordIsWithheldWhenTheCompareSetWasNotFullyCompared
//     does below. ⛔ THE OBSERVATION CLAUSE QUOTED ABOVE DOES NOT. Against
//     the hardened tool the run exits 2, not 0, and prints no verdict count
//     to exceed anything — that half of the CONTROL is unreproducible as
//     written and needs amending in the design doc (a document change this
//     file does not make; see the ⟦G17⟧ note on the test itself). What
//     still runs, and is what the test now pins, is the stderr warning and
//     the reconciliation arithmetic.
//
//   - W10-2(d)(ii) — the STALENESS discriminator. The criterion reads the
//     mechanism in source: "the warning is emitted from inside a loop over
//     compareSet (`for _, key := range compareSet`), so the line can only
//     ever name a projection the RUNNING TOOL still carries." That is a
//     BEHAVIOURAL claim about the tool, and it is the reason sheet §3(c)'s
//     pre-authorisation is dangerous. It is asserted here by running the
//     tool, not by grepping for the loop.
//
// ⚠ WHAT THIS FILE DOES NOT DO, and it is the larger half of both criteria.
// It does not check that the rehearsal record carries the reconciliation
// (W10-2(c)'s ASSERTION), and it does not check what the printed sheet says
// (W10-2(d)(ii)'s ASSERTION and FALSIFIER). Those are properties of a
// document and of a run that has not happened — see the split at
// reports/window-build/W10-tests.md §2. What is mechanised is the MEASURED
// PREMISE each assertion rests on, which is the half that can go quietly
// false when the tool changes.
//
// ⚠ A RED HERE IS NOT AUTOMATICALLY BAD NEWS — READ IT BEFORE FIXING IT.
// TestW10TheVerdictWordIsWithheldWhenTheCompareSetWasNotFullyCompared pins
// the tool's behaviour on W10-2(c)'s subject. If someone changes
// dumpcompare's arithmetic, this test reds, and the correct response is to
// re-read W10-2(c) and re-derive its reconciliation formula — not to
// restore the old arithmetic. The test says so in its own failure message.
//
// ⟦G17, 2026-09-09⟧ ⟦was: "TestW10TheVerdictCountExceedsTheNumberActuallyCompared
// pins a defect the criterion deliberately does NOT require to be fixed"⟧
// ⭐ THAT INSTRUCTION WAS FOLLOWED RATHER THAN REVERTED. James, the plan
// owner W10-2(c)'s Narrowness clause names, ruled the tool be hardened; the
// criterion re-read is recorded on the test itself. The defect the old name
// described — IDENTICAL printed at exit 0 over a compare set that was not
// fully compared — no longer exists, so the name no longer described
// anything. See reports/witness/G17-HARDEN-VERDICT.md.
//
// ⟦REPAIR — §219(3), 2026-09-04⟧ the source reader below tells an EMPTY class
// map by a RECORDED STRIKE (S127, bbff17e — no class rule in force) from an
// empty or UNREAD one that means the instrument is broken. See w10DcMapDecl;
// the control is TestW10TheStoreReaderTellsAStruckClassMapFromABrokenOne, and
// TestW10TheBinarysIgnoredLineIsExactlyTheReadersExclusions pins the reader to
// the shipped binary's own `ignored` line (repair-queue row B2).
//
// ⚠ RULE 20 / MACHINE LOAD. The binary is built ONCE per package run into
// t.TempDir() via sync.Once. Nothing under app/ is written. Measured build
// cost on this box: ~0.3s warm.
package store_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	// ⟦REPAIR — S96B, 2026-09-03⟧ the compare set is DERIVED from the registry
	// now (⟦LOCK — §67⟧), so this file must build the registry the way
	// cmd/dumpcompare does — BOTH registration sites, exactly as
	// cmd/sitectl/main.go calls them.
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

func w10DcModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %q", dir)
		}
		dir = parent
	}
}

var (
	w10DcOnce sync.Once
	w10DcPath string
	w10DcErr  error
)

// w10Dumpcompare builds cmd/dumpcompare and returns the binary path.
//
// It FAILS LOUDLY rather than skipping when the toolchain is unavailable. A
// t.Skip here would be the exact shape this window exists to refuse: a check
// that reports ok while never having run.
func w10Dumpcompare(t *testing.T) string {
	t.Helper()
	w10DcOnce.Do(func() {
		root := w10DcModuleRoot(t)
		out := filepath.Join(os.TempDir(), "w10-dumpcompare-"+strconv.Itoa(os.Getpid()))
		// ⟦REPAIR — §219(3), 2026-09-04⟧ Windows will not execute a file without an
		// extension: `go build -o` writes exactly the name given, and exec then
		// reports "executable file not found". [PRODUCED on this box] both W10-2
		// tests died here the moment the reader repoint let them reach the binary
		// — the old INSTRUMENT BROKEN guard had been masking it.
		if runtime.GOOS == "windows" {
			out += ".exe"
		}
		goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
		cmd := exec.Command(goBin, "build", "-o", out, "./cmd/dumpcompare")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			w10DcErr = err
			t.Logf("go build ./cmd/dumpcompare failed: %v\n%s", err, stderr.String())
			return
		}
		w10DcPath = out
	})
	if w10DcErr != nil || w10DcPath == "" {
		t.Fatalf("could not build cmd/dumpcompare (%v) — this check FAILS rather than skips: "+
			"an unrunnable control that reports ok is the failure mode W10 exists to refuse", w10DcErr)
	}
	return w10DcPath
}

// w10RunDumpcompare writes the two dumps to disk and runs the real binary.
func w10RunDumpcompare(t *testing.T, a, b map[string][]map[string]any) (exitCode int, stdout, stderr string) {
	t.Helper()
	dir := t.TempDir()
	write := func(name string, v map[string][]map[string]any) string {
		p := filepath.Join(dir, name)
		blob, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal %s: %v", name, err)
		}
		if err := os.WriteFile(p, blob, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return p
	}
	pa, pb := write("dump-a.json", a), write("dump-b.json", b)

	cmd := exec.Command(w10Dumpcompare(t), pa, pb)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running dumpcompare: %v\nstderr: %s", err, se.String())
	}
	return code, so.String(), se.String()
}

// w10VerdictCount extracts N from "IDENTICAL: N compared projections match".
// Returns -1 when the line is absent.
func w10VerdictCount(stdout string) int {
	for _, line := range strings.Split(stdout, "\n") {
		rest, ok := strings.CutPrefix(line, "IDENTICAL: ")
		if !ok {
			continue
		}
		numText, _, _ := strings.Cut(rest, " ")
		n, err := strconv.Atoi(numText)
		if err != nil {
			return -1
		}
		return n
	}
	return -1
}

func w10AbsentWarnings(stderr string) []string {
	var out []string
	for _, line := range strings.Split(stderr, "\n") {
		if !strings.Contains(line, "absent from both dumps") {
			continue
		}
		// warning: projection "NAME" absent from both dumps (old binary?)
		_, after, ok := strings.Cut(line, `"`)
		if !ok {
			continue
		}
		name, _, ok := strings.Cut(after, `"`)
		if ok {
			out = append(out, name)
		}
	}
	return out
}

// w10FullDump builds a dump carrying one row for every name given.
func w10FullDump(names []string) map[string][]map[string]any {
	d := map[string][]map[string]any{}
	for _, n := range names {
		d[n] = []map[string]any{{"id": n + "-1"}}
	}
	return d
}

// ⟦REPAIR — S96B, 2026-09-03⟧ ⛔ THIS FILE PARSED `var compareSet = []string{…}`
// UNTIL TODAY, AND THAT VAR NO LONGER EXISTS. ⟦LOCK — §67⟧ replaced the
// hand-kept list with a derivation (`buildCompareSet` in cmd/dumpcompare — the
// registry minus stated exclusions), because a hand-kept list of 12 against a
// registry of 23 let two posts genuinely DISAGREE about the other 11 while the
// tool printed "IDENTICAL" and exited 0.
//
// ⚠⚠ THREE TESTS IN THIS PACKAGE DIED AT 0.00s ON THAT PARSE, AND THE HARM IS
// NOT THE STALE SYMBOL — it is that an instrument fault and a criterion failure
// wore the same colour. The wording below is therefore load-bearing: every
// instrument failure says INSTRUMENT BROKEN in those words and names the symbol
// it could not read. The same repair landed in internal/deadline; the two are
// one defect in two packages and must be checked together.
//
// ⚠ The superseded parser is preserved verbatim in
// reports/window-build/S96B-W10-CONTROL.md §9 (§78's pointer rule).

// w10DcRegisteredProjections builds the projection registry from BOTH
// registration sites, the way cmd/sitectl and cmd/dumpcompare do.
//
// ⚠ Minted here rather than calling w7RegisteredProjections, which is in this
// same package and does the same job. A W10 instrument that dies with a
// "W7-12(e) INSTRUMENT" message, or that breaks when a W7 file is deleted, is a
// coupling across two windows' criteria for the sake of twenty lines. The
// duplication is recorded, not hidden — the same call this file's own
// superseded parser note made.
func w10DcRegisteredProjections(t *testing.T) []string {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("W10 INSTRUMENT BROKEN — not a criterion failure: opening an in-memory store: %v", err)
	}
	defer st.Close()
	if err := domain.Register(st); err != nil {
		t.Fatalf("W10 INSTRUMENT BROKEN — not a criterion failure: domain.Register: %v", err)
	}
	if err := deadline.Register(st); err != nil {
		t.Fatalf("W10 INSTRUMENT BROKEN — not a criterion failure: deadline.Register: %v", err)
	}
	b, err := st.DumpProjections()
	if err != nil {
		t.Fatalf("W10 INSTRUMENT BROKEN — not a criterion failure: DumpProjections: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("W10 INSTRUMENT BROKEN — not a criterion failure: decoding the dump: %v", err)
	}
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// w10DcConstString renders a string-literal expression, following `+`
// concatenation. ⚠ Deliberately more than strings.Trim: a reason written as
// "a" + "b" is a BinaryExpr, and a reader that skips it reports the entry as
// stating NO REASON — a false red on the property the exclusion list carries.
func w10DcConstString(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		out, err := strconv.Unquote(v.Value)
		return out, err == nil
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		l, lok := w10DcConstString(v.X)
		r, rok := w10DcConstString(v.Y)
		if !lok || !rok {
			return "", false
		}
		return l + r, true
	case *ast.ParenExpr:
		return w10DcConstString(v.X)
	}
	return "", false
}

// w10DcMapDecl is one top-level `var <name> = map[string]string{…}`, read with
// enough of its surroundings to tell apart EMPTY (no elements), UNREAD
// (elements this reader could not render as string → string — the shape has
// changed under it) and STRUCK (empty, AND the declaration's own doc comment
// preserves the entries it used to carry as ⟦was: "…"⟧ lines — §78's pointer
// rule, the shape of a deliberate deletion in this tree).
//
// ⟦REPAIR — §219(3), 2026-09-04⟧ ⛔ Until today all three read as "EMPTY" and
// EMPTY was INSTRUMENT BROKEN. S127 (bbff17e) struck all three class rules on
// purpose (their reasons were false — reports/window-build/S126-RETRACTION.md)
// and left the class map declared, empty, and with its struck entries
// preserved above it; this reader died on it before any criterion ran. The
// guard is NARROWED, not removed: only EMPTY-BY-A-RECORDED-STRIKE reads as a
// policy. The duplicate of this reader in internal/deadline carries the full
// account; the two must keep telling the same three states apart, and
// TestW10TheStoreReaderTellsAStruckClassMapFromABrokenOne produces each here.
type w10DcMapDecl struct {
	Entries    map[string]string // every element rendered as string → string
	Unreadable int               // elements in the literal this reader could NOT render
	Struck     []string          // keys the doc comment preserves as ⟦was: "…"⟧ map-entry lines, sorted
}

// w10DcTopLevelMapDecl reads a top-level `var <name> = map[string]string{…}`
// from PATH with its doc comment, returning an ERROR rather than fataling so
// a control can assert the failure text.
func w10DcTopLevelMapDecl(path, name string) (w10DcMapDecl, error) {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
	if err != nil {
		return w10DcMapDecl{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	decl := w10DcMapDecl{Entries: map[string]string{}}
	found := false
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
			for i, id := range vs.Names {
				if id.Name != name || i >= len(vs.Values) {
					continue
				}
				cl, ok := vs.Values[i].(*ast.CompositeLit)
				if !ok {
					continue
				}
				found = true
				for _, e := range cl.Elts {
					kv, ok := e.(*ast.KeyValueExpr)
					if !ok {
						decl.Unreadable++
						continue
					}
					k, kok := w10DcConstString(kv.Key)
					v, vok := w10DcConstString(kv.Value)
					if !kok || !vok {
						decl.Unreadable++
						continue
					}
					decl.Entries[k] = v
				}
				// `var x = …` carries its doc on the GenDecl; a spec inside a
				// `var ( … )` block carries it on the ValueSpec. Read both.
				decl.Struck = w10DcStruckEntries(gd.Doc, vs.Doc)
			}
		}
	}
	if !found {
		return w10DcMapDecl{}, fmt.Errorf("no top-level `var %s = map[string]string{…}` in %s", name, path)
	}
	return decl, nil
}

// w10DcStruckEntries reads the ⟦was: "…"⟧ lines of a declaration's doc comment
// and returns the KEYS of those that read back as map entries — unquoted,
// wrapped back into a map literal and PARSED, so a preserved comment or the
// bare word "was" is not evidence that an entry was struck (§94's rider).
func w10DcStruckEntries(groups ...*ast.CommentGroup) []string {
	var out []string
	for _, g := range groups {
		if g == nil {
			continue
		}
		for _, c := range g.List {
			text := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
			rest, ok := strings.CutPrefix(text, "⟦was:")
			if !ok {
				continue
			}
			rest, _, ok = strings.Cut(rest, "⟧")
			if !ok {
				continue
			}
			line, err := strconv.Unquote(strings.TrimSpace(rest))
			if err != nil {
				continue
			}
			expr, err := parser.ParseExpr("map[string]string{" + line + "}")
			if err != nil {
				continue
			}
			cl, ok := expr.(*ast.CompositeLit)
			if !ok || len(cl.Elts) != 1 {
				continue
			}
			kv, ok := cl.Elts[0].(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if k, kok := w10DcConstString(kv.Key); kok {
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}

// w10DcReadToolExclusions reads BOTH exclusion maps. ⚠ THERE ARE TWO AND BOTH
// ARE REQUIRED: ⟦LOCK — §82⟧ moved the side-table exclusions to CLASS SUFFIXES
// because three standing scans (W3-14, W4-13b, W5d-15) forbid a package outside
// internal/domain from naming a side table. [MEASURED at the §82 repair] reading
// only the named map reported "20 compared" where the tool compares 12.
//
// ⟦REPAIR — §219(3), 2026-09-04⟧ The class map may be EMPTY in exactly one
// way — by a RECORDED STRIKE (S127, bbff17e) — and is then returned as an
// empty map WITH the struck keys, so the caller can say so. Empty with no
// record, and a map whose elements cannot be read, are still INSTRUMENT
// BROKEN; the second used to read as the first, which is why the empty guard
// could not simply be deleted to let the struck map through.
func w10DcReadToolExclusions(mainGoPath string) (named, classes map[string]string, struckClasses []string, err error) {
	n, err := w10DcTopLevelMapDecl(mainGoPath, "ignoredProjections")
	if err != nil {
		return nil, nil, nil, err
	}
	c, err := w10DcTopLevelMapDecl(mainGoPath, "ignoredProjectionClasses")
	if err != nil {
		return nil, nil, nil, err
	}
	if n.Unreadable > 0 {
		return nil, nil, nil, fmt.Errorf("`ignoredProjections` in %s carries %d element(s) this reader could NOT read as "+
			"string → string (%d read) — the map has been restructured under the reader", mainGoPath, n.Unreadable, len(n.Entries))
	}
	if c.Unreadable > 0 {
		return nil, nil, nil, fmt.Errorf("`ignoredProjectionClasses` in %s carries %d element(s) this reader could NOT read as "+
			"string → string (%d read) — the map has been restructured under the reader", mainGoPath, c.Unreadable, len(c.Entries))
	}
	if len(n.Entries) == 0 {
		return nil, nil, nil, fmt.Errorf("`ignoredProjections` is EMPTY in %s", mainGoPath)
	}
	if len(c.Entries) == 0 && len(c.Struck) == 0 {
		return nil, nil, nil, fmt.Errorf("`ignoredProjectionClasses` is EMPTY in %s and its declaration preserves NO struck entry — "+
			"no ⟦was: \"…\"⟧ line above it reads back as a map entry. An empty class map with no strike record is not a "+
			"policy this reader knows: either the reader is parsing the wrong declaration, or a class rule was deleted "+
			"without being preserved (§78's pointer rule). An empty map WITH its struck entries preserved is the one "+
			"shape that reads as a policy (S127, bbff17e)", mainGoPath)
	}
	return n.Entries, c.Entries, c.Struck, nil
}

// w10CompareSetFromSource is the set cmd/dumpcompare actually compares:
// the registry from both registration sites, minus the tool's stated exclusions.
//
// ⚠ IT IS NOT A FULL RE-IMPLEMENTATION OF `buildCompareSet`. That function also
// carries the SHRINK TOOTH — a dead exclusion makes the tool ERROR and compare
// nothing — which this does not. The two agree on the CONTENTS of a healthy
// compare set, which is what every caller reads, and disagree only about a
// rotted exclusion list. ⭐ The tooth is asserted directly by
// TestW7DumpCompareSetCarriesVehicle's PROPERTY 2, in this same package.
// w10ExcludedProjections returns the NAMED exclusions the tool actually
// carries — read from cmd/dumpcompare's own `ignoredProjections`, never
// hardcoded here. ⛔ A hardcoded copy of the list would go stale silently the
// day the list changed, and the non-degeneracy control that depends on it
// would then be proving nothing while still passing.
//
// ⚠ Only the NAMED exclusions are returned, and only those that are actually
// registered: an entry naming an unregistered projection cannot appear in a
// dump, so it cannot exercise the absent-from-both path either way.
func w10ExcludedProjections(t *testing.T) []string {
	t.Helper()
	root := w10DcModuleRoot(t)
	mainGo := filepath.Join(root, "cmd", "dumpcompare", "main.go")
	named, _, _, err := w10DcReadToolExclusions(mainGo)
	if err != nil {
		t.Fatalf("W10 INSTRUMENT BROKEN — this is NOT a criterion failure: %v", err)
	}
	registered := map[string]bool{}
	for _, n := range w10DcRegisteredProjections(t) {
		registered[n] = true
	}
	var out []string
	for n := range named {
		if registered[n] {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

func w10CompareSetFromSource(t *testing.T) []string {
	t.Helper()
	root := w10DcModuleRoot(t)
	mainGo := filepath.Join(root, "cmd", "dumpcompare", "main.go")
	named, classes, struck, err := w10DcReadToolExclusions(mainGo)
	if err != nil {
		t.Fatalf("W10 INSTRUMENT BROKEN — this is NOT a criterion failure: %v\n"+
			"  The compare set is DERIVED from the tool's registry minus its stated exclusions\n"+
			"  (⟦LOCK — §67⟧, buildCompareSet in cmd/dumpcompare). If the tool's exclusion policy has\n"+
			"  been renamed or restructured again, RE-POINT THIS READER — do NOT read this red as a\n"+
			"  finding about the compare set.", err)
	}
	if len(classes) == 0 {
		// ⟦REPAIR — §219(3)⟧ said in the output, not left to be inferred from
		// a larger compare set: the class map is empty ON PURPOSE.
		t.Logf("W10 READER: `ignoredProjectionClasses` is EMPTY BY A RECORDED STRIKE of %v (S127, bbff17e) — "+
			"no class rule is in force, so every side table is in the compare set", struck)
	}
	registered := w10DcRegisteredProjections(t)
	var out []string
	for _, n := range registered {
		if _, skip := named[n]; skip {
			continue
		}
		skip := false
		for suffix := range classes {
			if strings.HasSuffix(n, suffix) {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		t.Fatalf("W10 INSTRUMENT BROKEN — this is NOT a criterion failure: EVERY one of the %d "+
			"registered projections is excluded, so every property below would be vacuously true",
			len(registered))
	}
	return out
}

// TestW10TheVerdictWordIsWithheldWhenTheCompareSetWasNotFullyCompared is
// W10-2(c)'s CONTROL, re-derived after ⟦G17, 2026-09-09⟧ hardened the tool.
//
// ⟦was: TestW10TheVerdictCountExceedsTheNumberActuallyCompared — it asserted
// exit 0, the word IDENTICAL, and a verdict count STRICTLY GREATER than the
// number compared⟧
//
// ⛔⛔ THIS RENAME IS THE ONE THIS FILE'S HEADER PRE-AUTHORISED, AND ONLY ON
// THE CONDITION IT SET. The header says: "If someone changes dumpcompare's
// arithmetic, this test reds, and the correct response is to re-read W10-2(c)
// and re-derive its reconciliation formula — not to restore the old
// arithmetic." ⭐ THE RE-READ WAS PERFORMED. What W10-2(c) actually says,
// quoted from design/WINDOW-BUILD-PLAN-v1.md:19933:
//
//	"Narrowness. This criterion does not require the tool to be changed.
//	 DR-W10-4/W10-10 recommend it; if the plan owner declines, the
//	 reconciliation stays a recorded operator step and the sheet gains it
//	 at B-4 — and that narrower discharge is stated on the face of the
//	 criterion rather than assumed."
//
// ⭐ So the criterion PERMITS the tool change and names it as a recommendation
// held open for the plan owner. James, the plan owner, did not decline: he
// ruled to harden. The criterion is therefore NOT contradicted, and this test
// is not being edited to green a red — it is being re-derived, which is the
// response the header demands.
//
// ⛔ ONE CLAUSE OF W10-2(c) IS NOW FALSE AND NEEDS AMENDING IN THE DESIGN DOC,
// which is a document change this lane did NOT make: the CONTROL reads "Take
// one dump pair, delete one compare-set key from BOTH JSON files, re-run:
// observe exit 0, a stderr warning, and a verdict count that exceeds the
// number compared." ⚠ Against the hardened binary the exit is 2 and there is
// no verdict count at all, so the CONTROL as written is unreproducible. The
// half that survives — the stderr warning, and the reconciliation arithmetic
// being performable from the tool's own output — is what this test now pins.
// See reports/witness/G17-HARDEN-VERDICT.md.
//
// ⚠ The ASSERTION half of W10-2(c) is untouched by all of this: the operator
// still records the `ignored` line, the warnings, and the arithmetic. What
// changed is that the tool no longer lets a silent pass through when the
// operator skips it.
func TestW10TheVerdictWordIsWithheldWhenTheCompareSetWasNotFullyCompared(t *testing.T) {
	set := w10CompareSetFromSource(t)
	if len(set) < 2 {
		t.Fatalf("compareSet has %d members; this control needs at least 2", len(set))
	}
	// W10-2(c)'s control, executed verbatim as far as it still applies:
	// "delete one compare-set key from BOTH JSON files".
	deleted := set[0]
	present := set[1:]
	dump := w10FullDump(present)

	code, stdout, stderr := w10RunDumpcompare(t, dump, dump)

	// (1) ⭐ THE HARDENING. Absent-from-both is now a refusal, not a pass.
	if code != 2 {
		t.Errorf("exit %d, want 2 — ⟦G17⟧ a compare-set projection absent from BOTH dumps "+
			"means the assert never ran over the whole compare set, which is this tool's "+
			"exit-2 class (the same one D-85's VACUOUS refusal uses)\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
	}
	// (2) ⛔ THE WORD THE SHEET HAS THE OPERATOR WRITE DOWN MUST NOT APPEAR.
	//     This is the whole point of the ruling: the defect was never the
	//     arithmetic on its own, it was IDENTICAL being printed at exit 0.
	if strings.Contains(stdout, "IDENTICAL") {
		t.Errorf("stdout still contains the word IDENTICAL on a run that did not compare "+
			"the whole compare set — that is the exact defect ⟦G17⟧ removed\nstdout:\n%s", stdout)
	}
	// (3) the refusal names its verdict word AND the projections it could not
	//     compare, so the operator is not left to infer them from stderr.
	if !strings.Contains(stdout, "INCOMPLETE: ") {
		t.Errorf("want an INCOMPLETE verdict line on stdout\nstdout:\n%s", stdout)
	}
	if !strings.Contains(stdout, deleted) {
		t.Errorf("the INCOMPLETE line does not name %q, the projection that was absent "+
			"from both dumps\nstdout:\n%s", deleted, stdout)
	}
	// (4) UNCHANGED, and deliberately so: the stderr warning is W10-2(d)(ii)'s
	//     staleness discriminator. ⛔ The hardening must not have eaten it.
	warned := w10AbsentWarnings(stderr)
	if len(warned) != 1 || warned[0] != deleted {
		t.Errorf("stderr warnings = %v, want exactly [%s]\nstderr:\n%s", warned, deleted, stderr)
	}
	// (5) W10-2(c)'s arithmetic `len(compareSet) − absent-from-both = actually
	//     compared` must STILL be performable from the tool's own output —
	//     that requirement outlives the verdict line it was written against.
	compared := len(set) - 1
	if got := len(set) - len(warned); got != compared {
		t.Errorf("reconciliation `len(compareSet) - absent-from-both` = %d, want %d", got, compared)
	}
	wantLine := fmt.Sprintf("%d compare-set projections absent from both, not compared", len(warned))
	if !strings.Contains(stdout, wantLine) {
		t.Errorf("the `compared:` reconciliation line does not carry %q\nstdout:\n%s", wantLine, stdout)
	}
	// ⚠ GUARDED: this summary asserts what the checks above just proved, so
	// it must not print when any of them failed. [MEASURED 2026-09-09 under a
	// planted mutant: it printed "no IDENTICAL" on a run whose stdout carried
	// IDENTICAL three times.]
	if !t.Failed() {
		t.Logf("⟦G17⟧ REFUSAL REPRODUCED: exit %d · warnings %d · actually compared %d · no IDENTICAL",
			code, len(warned), compared)
	}
}

// TestG17TheHardFailureDoesNotFireOnTheLegitimateExclusions is the
// NON-DEGENERACY control for the hardening, and it is the one that decides
// whether the new check is worth having.
//
// ⛔ THE RISK: three projections are LEGITIMATELY excluded from the compare set
// (alert_ack, alert_ack_retraction, weather_phase — each with a stated reason
// in cmd/dumpcompare's ignoredProjections). A hard failure that fired on THOSE
// would red every ordinary run and be disabled within a week, which is the
// decay that makes a check worthless.
//
// ⭐ The separation is STRUCTURAL, not a property of today's exclusion list:
// buildCompareSet removes the excluded names BEFORE the absent-from-both loop
// is entered, so an excluded projection cannot reach the counter no matter
// which dumps it is or is not in. This test proves that by putting the
// exclusions in the hardest position for the check — ABSENT FROM BOTH DUMPS,
// the exact condition that fires on a compare-set member.
func TestG17TheHardFailureDoesNotFireOnTheLegitimateExclusions(t *testing.T) {
	set := w10CompareSetFromSource(t)
	inSet := map[string]bool{}
	for _, k := range set {
		inSet[k] = true
	}

	// The exclusions, taken from the tool's own list rather than hardcoded
	// here — a hardcoded copy would go stale silently the moment the list
	// changes, and would then be testing nothing.
	excluded := w10ExcludedProjections(t)
	if len(excluded) == 0 {
		t.Fatal("W10 INSTRUMENT BROKEN — this is NOT a criterion failure: the exclusion " +
			"list read from cmd/dumpcompare is EMPTY, so this control would pass vacuously")
	}
	for _, e := range excluded {
		if inSet[e] {
			t.Fatalf("W10 INSTRUMENT BROKEN: %q is both excluded and in compareSet", e)
		}
	}

	// Every compare-set member present on both sides; every EXCLUDED
	// projection absent from both. If the check keyed off the dump rather
	// than off compareSet, this is the run that would red.
	dump := w10FullDump(set)
	code, stdout, stderr := w10RunDumpcompare(t, dump, dump)

	if code != 0 {
		t.Errorf("exit %d, want 0 — the %d legitimate exclusions %v are absent from both "+
			"dumps, and ⟦G17⟧'s hard failure MUST NOT fire on them. A guard that reds an "+
			"ordinary run is a guard that gets disabled\nstdout:\n%s\nstderr:\n%s",
			code, len(excluded), excluded, stdout, stderr)
	}
	if !strings.Contains(stdout, "IDENTICAL") {
		t.Errorf("want the IDENTICAL verdict on an ordinary run\nstdout:\n%s", stdout)
	}
	if strings.Contains(stdout, "INCOMPLETE") {
		t.Errorf("⟦G17⟧'s refusal fired on a run whose only absences are the LEGITIMATE "+
			"exclusions %v — the check is keyed off the wrong set\nstdout:\n%s", excluded, stdout)
	}
	// ⭐ CORRECT-DIAGNOSIS CHECK. A green here is only meaningful if the
	// absent-from-both counter genuinely saw zero; a tool that had stopped
	// counting altogether would also pass the three assertions above.
	if !strings.Contains(stdout, "0 compare-set projections absent from both, not compared") {
		t.Errorf("the reconciliation line does not report ZERO absent-from-both, so this "+
			"control's green does not mean what it claims\nstdout:\n%s", stdout)
	}
	if w := w10AbsentWarnings(stderr); len(w) != 0 {
		t.Errorf("stderr warned about %v on a run whose only absences are excluded "+
			"projections\nstderr:\n%s", w, stderr)
	}
	if !t.Failed() {
		t.Logf("⟦G17⟧ NON-DEGENERACY: %d exclusions %v absent from both dumps, refusal did NOT fire, exit %d",
			len(excluded), excluded, code)
	}
}

func TestW10TheAbsentFromBothWarningCanOnlyNameACompareSetMember(t *testing.T) {
	set := w10CompareSetFromSource(t)
	inSet := map[string]bool{}
	for _, k := range set {
		inSet[k] = true
	}

	// NON-DEGENERACY, and it is the discriminator W10-2(d)(ii) is about.
	// Two keys, both ABSENT FROM BOTH DUMPS, differing only in compare-set
	// membership. The certified rule (the loop is over compareSet) warns
	// about one and not the other; the named rival — "the tool warns about
	// anything missing from both dumps" — would warn about both.
	inSetAbsent := set[0]
	outOfSetAbsent := "w10_not_in_compare_set"
	if inSet[outOfSetAbsent] {
		t.Fatalf("fixture collision: %q is in compareSet", outOfSetAbsent)
	}

	// A dump pair that holds every compare-set member EXCEPT inSetAbsent,
	// and holds outOfSetAbsent on NEITHER side either.
	var present []string
	for _, k := range set[1:] {
		present = append(present, k)
	}
	dump := w10FullDump(present)

	_, stdout, stderr := w10RunDumpcompare(t, dump, dump)
	warned := w10AbsentWarnings(stderr)

	sawInSet, sawOutOfSet := false, false
	for _, w := range warned {
		if w == inSetAbsent {
			sawInSet = true
		}
		if w == outOfSetAbsent {
			sawOutOfSet = true
		}
	}
	if !sawInSet {
		t.Errorf("no `absent from both dumps` warning for %q, which IS a compareSet member — "+
			"the staleness discriminator W10-2(d)(ii) rests on does not exist\nstderr:\n%s",
			inSetAbsent, stderr)
	}
	if sawOutOfSet {
		t.Errorf("the tool warned about %q, which is NOT a compareSet member — the warning is "+
			"NOT bounded by the running tool's own compare set, so it does not discriminate "+
			"a stale tool at all\nstderr:\n%s", outOfSetAbsent, stderr)
	}
	// The IGNORED line is the other half of the same mechanism and is
	// asserted so "no warning" cannot mean "the key vanished silently".
	if strings.Contains(stdout, outOfSetAbsent) {
		t.Errorf("%q appears on stdout though it is in NEITHER dump — the IGNORED line is a "+
			"union over the two dumps, not over the compare set\nstdout:\n%s", outOfSetAbsent, stdout)
	}
	t.Logf("NON-DEGENERACY, observed: warnings emitted = %v · in-compareSet-and-absent %q warned=%v · "+
		"not-in-compareSet-and-absent %q warned=%v",
		warned, inSetAbsent, sawInSet, outOfSetAbsent, sawOutOfSet)
}

// ---------------------------------------------------------------------------
// ⟦REPAIR — §219(3), 2026-09-04⟧ the reader's control, and the binary ↔ reader
// pin (repair-queue row B2).
// ---------------------------------------------------------------------------

// w10DcPlantMainGo writes a synthetic cmd/dumpcompare/main.go into a fresh
// t.TempDir() and returns its path. RULE 20: a REAL on-disk file, because the
// reader parses from disk. Nothing under app/ is written.
func w10DcPlantMainGo(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(path, []byte("package main\n\n"+body), 0o644); err != nil {
		t.Fatalf("planting %s: %v", path, err)
	}
	return path
}

// TestW10TheStoreReaderTellsAStruckClassMapFromABrokenOne is the control for
// the §219(3) repoint IN THIS PACKAGE. The reader here is a deliberate
// duplicate of internal/deadline's (recorded at w10DcRegisteredProjections),
// so its discriminators are produced here too rather than assumed from the
// other package's control — "the two are one defect in two packages and must
// be checked together".
//
// ⭐ EVERY ARM PRODUCES A FAILURE OR A READ. The three states below used to be
// ONE state ("EMPTY", INSTRUMENT BROKEN) to this reader; the repair is only a
// repair if each is now told from the others. ⛔ Arm 1 is the one that proves
// the guard was narrowed and not deleted.
func TestW10TheStoreReaderTellsAStruckClassMapFromABrokenOne(t *testing.T) {
	const named = "var ignoredProjections = map[string]string{\"a\": \"reason a\"}\n"

	// ARM 1 — EMPTY, NO STRIKE RECORD: still INSTRUMENT BROKEN, by symbol, by
	// EMPTY, and by the record it could not find.
	if _, _, _, err := w10DcReadToolExclusions(w10DcPlantMainGo(t, named+
		"var ignoredProjectionClasses = map[string]string{}\n")); err == nil {
		t.Fatal("CONTROL FAILED (arm 1): an EMPTY `ignoredProjectionClasses` with NO strike record read as healthy — " +
			"the guard is gone, not narrowed")
	} else if !strings.Contains(err.Error(), "ignoredProjectionClasses") || !strings.Contains(err.Error(), "EMPTY") ||
		!strings.Contains(err.Error(), "strike") {
		t.Fatalf("CONTROL FAILED (arm 1): the failure must name the symbol, say EMPTY and say the strike record is "+
			"missing: %v", err)
	}

	// ARM 2 — EMPTY BY A RECORDED STRIKE: the one shape that reads as a policy,
	// and the reader says which rule was struck.
	_, classes, struck, err := w10DcReadToolExclusions(w10DcPlantMainGo(t, named+"\n"+
		`// ⟦was: "\t\"_side\": \"class reason\","⟧`+"\n"+
		"var ignoredProjectionClasses = map[string]string{}\n"))
	if err != nil {
		t.Fatalf("CONTROL FAILED (arm 2): an empty class map WITH its struck entry preserved did not read as a "+
			"policy: %v", err)
	}
	if len(classes) != 0 || len(struck) != 1 || struck[0] != "_side" {
		t.Fatalf("CONTROL FAILED (arm 2): classes=%v struck=%v, want no class in force and exactly [_side] struck",
			classes, struck)
	}

	// ARM 3 — UNREAD: an element the reader cannot render. Until today this
	// read as EMPTY; it must now fail saying it could not READ, and not EMPTY.
	if _, _, _, err := w10DcReadToolExclusions(w10DcPlantMainGo(t, "var reason = \"r\"\n"+named+
		"var ignoredProjectionClasses = map[string]string{\"_x\": reason}\n")); err == nil {
		t.Fatal("CONTROL FAILED (arm 3): a class map with an element this reader cannot render read as healthy — " +
			"a restructured policy would be compared as if it were empty")
	} else if !strings.Contains(err.Error(), "could NOT read") || strings.Contains(err.Error(), "EMPTY") {
		t.Fatalf("CONTROL FAILED (arm 3): UNREAD must be reported as could-NOT-read and never as EMPTY: %v", err)
	}

	// ARM 4 — THE RECORD IS A PROPERTY: a ⟦was:⟧ line preserving a COMMENT is
	// not a struck entry, and the map above which only it stands is still broken.
	if _, _, s, err := w10DcReadToolExclusions(w10DcPlantMainGo(t, named+"\n"+
		`// ⟦was: "\t// the side tables are excluded by class"⟧`+"\n"+
		"var ignoredProjectionClasses = map[string]string{}\n")); err == nil {
		t.Fatalf("CONTROL FAILED (arm 4): a ⟦was:⟧ line that preserves a comment was accepted as a strike record "+
			"(struck=%v)", s)
	}

	// ARM 5 — NEGATIVE: a populated, well-formed policy reads back exactly,
	// `+` concatenation followed, with nothing struck.
	n, c, s, err := w10DcReadToolExclusions(w10DcPlantMainGo(t,
		"var ignoredProjections = map[string]string{\"alpha\": \"reason \" + \"alpha\"}\n"+
			"var ignoredProjectionClasses = map[string]string{\"_side\": \"class reason\"}\n"))
	if err != nil || len(n) != 1 || n["alpha"] != "reason alpha" || len(c) != 1 || c["_side"] != "class reason" || len(s) != 0 {
		t.Fatalf("CONTROL FAILED (arm 5, negative): a well-formed policy read as named=%v classes=%v struck=%v err=%v",
			n, c, s, err)
	}
}

// w10IgnoredLine extracts the names from the tool's
// "ignored (not in the §13.3 compare set): a, b, c" line, sorted. nil when the
// line is absent — which is what the tool prints when nothing is ignored.
func w10IgnoredLine(stdout string) []string {
	for _, line := range strings.Split(stdout, "\n") {
		if !strings.HasPrefix(line, "ignored (") {
			continue
		}
		_, rest, ok := strings.Cut(line, "): ")
		if !ok {
			continue
		}
		var out []string
		for _, n := range strings.Split(rest, ",") {
			if n = strings.TrimSpace(n); n != "" {
				out = append(out, n)
			}
		}
		sort.Strings(out)
		return out
	}
	return nil
}

// TestW10TheBinarysIgnoredLineIsExactlyTheReadersExclusions — the other half
// of repair-queue row B2, under §219(3).
//
// §219 derived the sentinel's magnitude from the SHIPPED BINARY's own
// `ignored` line because the reader could not state it: DUMP 23 − compareSet
// 20 − {alert_ack} = TWO. The magnitude itself is asserted in internal/deadline
// through the sentinel's own derivation; what remained unasserted was the step
// §219 took by hand — that the binary's printed exclusions ARE the exclusions
// the source reader models. This pins the two to each other, as a PROPERTY over
// the live registry rather than a second copy of the names: if the reader ever
// drifts from `w67Ignored`, every W10 number derived through it is off by the
// difference, and this is where that shows.
func TestW10TheBinarysIgnoredLineIsExactlyTheReadersExclusions(t *testing.T) {
	registered := w10DcRegisteredProjections(t)
	set := w10CompareSetFromSource(t)
	inSet := map[string]bool{}
	for _, k := range set {
		inSet[k] = true
	}
	var readerIgnored []string
	for _, n := range registered {
		if !inSet[n] {
			readerIgnored = append(readerIgnored, n)
		}
	}

	// NON-DEGENERACY, PRODUCED FIRST: the line is read from the binary's real
	// output and names what the binary really ignored. A key registered
	// NOWHERE, present in both dumps, must appear on it — so a parser that
	// returned the reader's own list, or nil, cannot pass.
	foreign := "w10_not_registered_anywhere"
	if inSet[foreign] {
		t.Fatalf("fixture collision: %q is in compareSet", foreign)
	}
	withForeign := w10FullDump(append(append([]string{}, registered...), foreign))
	_, stdout, _ := w10RunDumpcompare(t, withForeign, withForeign)
	seen := false
	for _, n := range w10IgnoredLine(stdout) {
		if n == foreign {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("CONTROL FAILED: the binary's ignored line did not name %q, which is in both dumps and in no "+
			"compare set — the line reader is not reading the binary\nstdout:\n%s", foreign, stdout)
	}

	// THE PROPERTY: over a dump carrying exactly the registry, the binary's
	// ignored line is exactly registry − the reader's compare set.
	dump := w10FullDump(registered)
	code, stdout, stderr := w10RunDumpcompare(t, dump, dump)
	if code != 0 {
		t.Fatalf("exit %d on two identical full dumps, want 0\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	got := w10IgnoredLine(stdout)
	if strings.Join(got, ",") != strings.Join(readerIgnored, ",") {
		t.Errorf("the binary ignores %v but the source reader models the exclusions as %v — the reader has drifted "+
			"from w67Ignored, and every W10 number derived through it is off by the difference\nstdout:\n%s",
			got, readerIgnored, stdout)
	}
	if v := w10VerdictCount(stdout); v != len(set) {
		t.Errorf("verdict count %d, reader's compare set %d", v, len(set))
	}
	t.Logf("BINARY ↔ READER [MEASURED]: registered=%d · binary ignores %v · compared=%d", len(registered), got, len(set))
}
