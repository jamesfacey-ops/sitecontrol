// w8_deletions_test.go — W8 (item 10, the trade register): the DELETION
// criteria. W8-6 (the workgroup census over a DERIVED surface), W8-9(b)/(c)/(d)
// (the ref_alias mechanism), and W8-10(a) (the compile-forced re-homing).
//
// Written from the LOCKED W8 criteria only. No built implementation was read.
//
// ⚠ W8-9 is "the one seam where the chunk can go fully green and still ship
// the defect". Its authority is TWO-PART and both parts are stated: P-3 §5 is
// a DESIGN RULING and it sets the schedule; D-47 is JAMES and it sets the bar
// — "ref_alias_created must NOT be carried into v10's frozen event-type
// vocabulary… A freeze list that carries it forward re-creates the retired
// mechanism as a frozen ghost."
package domain

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner" // ⟦LOCK — §63, 2026-09-03⟧ the census filter is lexical, not textual
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// w8AppRoot is the `app/` directory — the census's surface root.
func w8AppRoot(t *testing.T) string {
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
			t.Fatal("no go.mod found above the package directory — the census surface is unresolvable")
		}
		dir = parent
	}
}

// --- W8-6(a): THE PROPERTY, from which the surface is DERIVED --------------
//
// ⟦LOCK — B-3 / FIL-F5: ONE surface, stated once.⟧
//
//	Every file PRESENT UNDER `app/`, read as BYTES and CASE-FOLDED, contains
//	no `workgroup` reference outside the asserted allowlist.
//
// ⚠ THE SURFACE IS THE FILESYSTEM, AND UNTRACKED FILES ARE INCLUDED. The only
// exclusions are `.git/` and the paths `.gitignore` names as build products,
// BOTH ASSERTED AS LITERALS. ⚠ The choice is load-bearing and is why (m8-3)
// gains a FIFTH arm: a planted survivor in an UNTRACKED file passes a
// `git ls-files` walk and fails a filesystem walk, "so the two readings are
// not interchangeable and a draft that uses both spellings has not decided".
//
// ⚠ THE WALK TAKES NO EXTENSION FILTER. The plan's own enumeration listed
// `SQL`, of which there are ZERO files, and omitted `.md`, which has three
// live hits — "the filter narrower than its subject class, in the criterion
// whose whole subject sentence was already right". The class's own remedy is
// applied instead of the correction: STATE THE PROPERTY, DERIVE THE LIST.
var w8CensusExcludedDirs = []string{".git"}

// w8CensusFiles walks `app/` and returns every file present, excluding only
// .git/ and the .gitignore build products, asserted as literals.
//
// ⚠ FOUND 2026-09-04, HOST ARTIFACT, NOT A CRITERION CHANGE: `ignored` (the
// .gitignore literal set) was being consulted ONLY at the file check below —
// never at the directory branch above it. `dist` and `bin` ARE members of
// that same literal set (the .gitignore lines "dist/" and "bin/", trimmed),
// but a directory is never rel-equal or basename-equal to a FILE it
// contains, so `filepath.Walk` kept descending into them and every file
// underneath (e.g. `dist\sitectl.exe`, a pre-deletion build left on a
// Windows host with no `dist/`-cleaning step) was scanned and reported as a
// live offender — a build artifact answering for a SOURCE census. Pruning
// the SAME already-computed literal set at the directory branch (not a new
// exclusion mechanism, not a glob) closes this without touching what the
// criterion asserts: .gitignore's build-product paths, asserted as
// literals, stay excluded whether they are files or the directories that
// hold them.
func w8CensusFiles(t *testing.T, root string) []string {
	t.Helper()
	ignored := w8GitignoreBuildProducts(t, root)
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if info.IsDir() {
			for _, ex := range w8CensusExcludedDirs {
				if rel == ex || strings.HasPrefix(rel, ex+string(filepath.Separator)) {
					return filepath.SkipDir
				}
			}
			if rel != "." && (ignored[rel] || ignored[info.Name()]) {
				return filepath.SkipDir
			}
			return nil
		}
		if ignored[rel] || ignored[filepath.Base(rel)] {
			return nil
		}
		out = append(out, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walking the census surface %s: %v", root, err)
	}
	sort.Strings(out)
	return out
}

// w8GitignoreBuildProducts reads .gitignore and returns the paths it names as
// build products — ASSERTED AS LITERALS, per the criterion, rather than
// implied by a glob.
func w8GitignoreBuildProducts(t *testing.T, root string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	b, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		// No .gitignore is not a failure of this criterion; the exclusion set
		// is then empty and the surface is wider, which is the safe direction.
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out[strings.TrimSuffix(strings.TrimPrefix(line, "/"), "/")] = true
	}
	return out
}

// --- W8-6(c): THE LIVENESS ASSERTION --------------------------------------
//
// ⟦LOCK — B-2 / FAL-F2, with FIX-F1 merged. ⚠ Revision 2's form was CIRCULAR:
// "the extension set derived by the walk itself" evaluates
// walk.extensions ⊆ walk.extensions. A walk narrowed to `.go` computes
// {.go}, observes it reached a `.go` file, and PASSES.⟧
//
// THE INVENTORY IS AN INDEPENDENT ENUMERATION: the extension set comes from
// `git ls-files` SUFFIXES, computed by code that SHARES NOTHING with the
// census walk — not the walk's own visited set, not a filter the walk
// consults. The walk then asserts it REACHED AT LEAST ONE FILE of every
// member.
//
// ⚠ THE VALUE IS PINNED AS A SET, NEVER AS A COUNT. The arbiter measured 240
// .go files and the criteria drafter 250 on the same tree one day later.
// The SET is stable; the counts are not — and W8 ADDS FILES.
//
// ⚠ THE TWO EXTENSION-LESS TRACKED DOTFILES `.gitattributes` and `.gitignore`
// are members: a filepath.Ext-shaped inventory reports them as their own whole
// name and a naive strings.Split(name, ".") DROPS THEM SILENTLY.
func w8TrackedExtensionInventory(t *testing.T, root string) []string {
	t.Helper()
	cmd := exec.Command("git", "ls-files")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("PRECONDITION (instrument): `git ls-files` failed in %s: %v — the liveness inventory must be an "+
			"INDEPENDENT enumeration; deriving it from the walk's own visited set is circular and passes a "+
			"walk narrowed to .go", root, err)
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		base := filepath.Base(name)
		ext := filepath.Ext(base)
		if ext == "" || ext == base {
			// An extension-less tracked dotfile: .gitattributes, .gitignore.
			ext = base
		}
		seen[ext] = true
	}
	var exts []string
	for e := range seen {
		exts = append(exts, e)
	}
	sort.Strings(exts)
	if len(exts) == 0 {
		t.Fatal("PRECONDITION (instrument): the tracked extension inventory is EMPTY — the liveness assertion below could not fail")
	}
	return exts
}

func w8ExtensionOf(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	if ext == "" || ext == base {
		return base
	}
	return ext
}

// --- W8-6(d): THE EXCLUSION SIDE — FOUR MECHANISMS, NOT ONE LIST ----------

// w8AllowRow is (d-ii): the allowlist is a SET OF (FILE, CRITERION-ID) PAIRS,
// each naming the LOCKED CRITERION that requires the string to stay.
type w8AllowRow struct {
	File      string // repo-relative
	Criterion string // the locked criterion that keeps the string, or "" for a historical record
	Ground    string
}

// w8WorkgroupAllowlist is the criterion's own table, verbatim, PLUS the rows
// W8's own test files force — see the FINDING recorded beside them.
//
// ⚠ `deploy/demo-walkthrough.md` is NOT allowed and is REWIRED by W8: it is
// live operator procedure, and after W8 `:143` tells an operator to expect a
// snapshot column that no longer exists. (m8-3) arm (C) plants there
// DELIBERATELY, and a plant inside an allowlisted file proves nothing.
//
// ⚠ `internal/store/w5d_source_scans_test.go` is NOT allowlisted either:
// W8-A3(iii) RETIRES the workgroup-named predecessor of what is now
// TestW5dTradeGainsNoComposedPath on exactly this collision — its source
// carries 18 `workgroup` hits, so retaining it forces either allowlisting a
// file containing all four forbidden strings, GUTTING THIS CRITERION WHERE
// IT IS SHARPEST, or a permanent red.
var w8WorkgroupAllowlist = []w8AllowRow{
	{File: "reports/fix-attribution.md", Ground: "historical build report; a record of what was done, not an instruction"},
	{File: "reports/fix-officer-default.md", Ground: "historical build report; a record of what was done, not an instruction"},

	// ⚠⚠ A FINDING AGAINST THE LOCK, recorded here rather than resolved:
	// W8's OWN TEST FILES CARRY THE STRING AND THE LOCK'S TABLE HAS NO ROW
	// FOR THEM. Every one of these asserts the DELETION — that `workgroup_id`
	// is gone from the person wire keys, that `workgroup_snapshot_*` is gone
	// from `visit` — and cannot make that assertion without naming what must
	// be absent. That is the same collision W8-A3(iii) met and dispositioned
	// by RETIREMENT; retirement is not available here, because these ARE the
	// criteria. Each row names the criterion that keeps the string, in
	// (d-ii)'s own shape, and the shrink tooth (d-iii) guards them.
	// ⚠ ROUTED TO THE PLAN OWNER: either these rows join W8-6(d-ii)'s table
	// at the lock, or W8-2 / W8-X4 / W8-23(a) must be re-expressed without
	// the literal — which would make them invisible to the census that is
	// supposed to see everything, and is the worse of the two.
	{File: "internal/domain/w8_absences_test.go", Criterion: "W8-2 / W8-X4 / W8-23(a)", Ground: "asserts workgroup_id is ABSENT from the person column set and from person_created's wire keys, and workgroup_snapshot_* ABSENT from visit"},
	{File: "internal/web/w8_gate_controls_test.go", Criterion: "W8-23(c) / W8-23(g)", Ground: "asserts the five gate.html trade controls REPLACE the workgroup ones"},
	{File: "internal/web/w8_copy_test.go", Criterion: "W8-22", Ground: "asserts the operator copy stops naming the deleted mechanism"},
	{File: "internal/domain/w8_carrier_test.go", Criterion: "W8-27(b)", Ground: "asserts workgroup_created has LEFT the event-type vocabulary — the half of the NET-ZERO swap D-47 forbids"},
	// ⚠ internal/config/w8_config_key_test.go is deliberately NOT a row: it
	// asserts the muster_min_crew_size -> muster_min_group_size rename and
	// carries no `workgroup` spelling at all. The shrink tooth would have
	// reported it, which is the row's own point.
}

// TestW8NoWorkgroupReferenceSurvives is W8-6 — the census.
//
// ⭐ GENUINE PRE-CHUNK RED. ⚠ NO BASELINE COUNT IS ASSERTED ⟦LOCK — ENU-F15,
// adopted as its OBLIGATION rather than its prescription⟧: the assertion is
// NON-ZERO BEFORE AND ZERO AFTER. The figures have moved three times in three
// days — 73 files / 697 hits at revision 2, 81 / 1007 at the criteria pass —
// and "a figure that decays between revisions is worse than none".
//
// ⚠ THE PREDICTABLE REPAIR UNDER TIME PRESSURE *IS* THE MUTANT, named here so
// a gate recognises it: the cheapest green is to exclude `_test.go` from the
// walk. THAT IS (m8-19) INSTALLED AS THE FIX, and it blinds (m8-3) arm (B).
func TestW8NoWorkgroupReferenceSurvives(t *testing.T) {
	root := w8AppRoot(t)
	files := w8CensusFiles(t, root)
	if len(files) == 0 {
		t.Fatal("PRECONDITION (instrument): the census walk reached NO files — it would report a clean pass over nothing")
	}

	// --- (c) THE LIVENESS ASSERTION, on an INDEPENDENT inventory. ----------
	want := w8TrackedExtensionInventory(t, root)
	reached := map[string]bool{}
	for _, f := range files {
		reached[w8ExtensionOf(f)] = true
	}
	var unreached []string
	for _, ext := range want {
		if !reached[ext] {
			unreached = append(unreached, ext)
		}
	}
	if len(unreached) != 0 {
		t.Fatalf("W8-6(c) LIVENESS ((m8-19)): the census walk reached NO file of extension(s) %v, which `git ls-files` "+
			"reports as present. The inventory is an INDEPENDENT enumeration precisely so this can fail: a walk "+
			"narrowed to .go computes {.go}, observes it reached a .go file, and passes — which is the circular "+
			"form this replaces. Tracked inventory: %v", unreached, want)
	}

	// --- (d-i) SELF-EXCLUSION, BY PATH EQUALITY, never by a glob. ----------
	// ⚠ A glob or a strings.Contains exclusion is the narrowing this criterion
	// exists to catch, wearing a self-exclusion's clothes.
	_, selfPath, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("PRECONDITION (instrument): runtime.Caller could not resolve this file's path, so (d-i)'s self-exclusion is unavailable")
	}

	// --- (d-ii) the allowlist, resolved to absolute paths. -----------------
	allowed := map[string]w8AllowRow{}
	for _, row := range w8WorkgroupAllowlist {
		allowed[filepath.Join(root, filepath.FromSlash(row.File))] = row
	}

	const term = "workgroup"
	hitsPerAllowRow := map[string]int{}
	var offenders []string
	for _, f := range files {
		if f == selfPath {
			continue // (d-i), by PATH EQUALITY
		}
		b, err := os.ReadFile(f)
		if err != nil {
			// A file present but unreadable is a coverage hole, reported.
			offenders = append(offenders, fmt.Sprintf("%s: UNREADABLE — not scanned, so the census's coverage is smaller than its stated scope: %v", f, err))
			continue
		}
		n := strings.Count(strings.ToLower(string(b)), term)
		if n == 0 {
			continue
		}
		if row, isAllowed := allowed[f]; isAllowed {
			hitsPerAllowRow[row.File] += n
			continue
		}
		rel, _ := filepath.Rel(root, f)
		offenders = append(offenders, fmt.Sprintf("%s (%d hits)", rel, n))
	}

	// --- (d-iii) THE SHRINK TOOTH: an allowlist row with NO SURVIVING HIT is
	// a census FAILURE — never a skip, never a dormant row. In
	// w4_registry_test.go's landed CONTROL-2 form.
	// ⚠ THIS IS WHAT STOPS THE ALLOWLIST BECOMING THE NARROWING BY ANOTHER
	// NAME, and revision 2 had no analogue of it anywhere in 2,530 lines.
	for _, row := range w8WorkgroupAllowlist {
		if hitsPerAllowRow[row.File] == 0 {
			t.Errorf("W8-6(d-iii) THE SHRINK TOOTH: allowlist row %q (kept for %s: %s) has NO SURVIVING HIT. "+
				"A dead allowlist row is a census FAILURE, never a skip — it is how the allowlist quietly becomes "+
				"the narrowing this criterion exists to catch", row.File, w8CriterionOrHistorical(row), row.Ground)
		}
	}

	if len(offenders) != 0 {
		sort.Strings(offenders)
		shown := offenders
		if len(shown) > 40 {
			shown = shown[:40]
		}
		t.Fatalf("W8-6 ((m8-3), (m8-19)): %d file(s) under app/ still contain a `workgroup` reference outside the "+
			"asserted allowlist. The assertion is NON-ZERO BEFORE AND ZERO AFTER — no baseline count is asserted, "+
			"because the figure has decayed three times in three days.\n"+
			"⚠ THE SURFACE IS THE FILESYSTEM AND UNTRACKED FILES ARE INCLUDED: a planted survivor in an untracked "+
			"file passes a `git ls-files` walk and fails this one.\n"+
			"⚠ THE PREDICTABLE REPAIR UNDER TIME PRESSURE *IS* THE MUTANT: excluding _test.go from the walk is "+
			"(m8-19) INSTALLED AS THE FIX, and it blinds (m8-3) arm (B).\n"+
			"first %d of %d:\n  %s", len(offenders), len(shown), len(offenders), strings.Join(shown, "\n  "))
	}
}

func w8CriterionOrHistorical(r w8AllowRow) string {
	if r.Criterion == "" {
		return "no criterion — historical record"
	}
	return r.Criterion
}

// TestW8CensusHasItsFivePlants produces (m8-3)'s five arms.
//
// ⟦LOCK — F-4 / FIL-F5: FIVE plants, not four — the fifth is the
// UNTRACKED-FILE plant, and it is the one that SEPARATES THE TWO READINGS OF
// THE SURFACE: it passes a `git ls-files` walk and fails a filesystem walk.⟧
//
//	(A) a workgroup_snapshot_name read in a TEMPLATE
//	(B) a person.workgroup_id COLUMN REFERENCE in a _test.go file
//	(C) a hit in a `.md` file — planted in deploy/demo-walkthrough.md, which
//	    is EXPLICITLY NOT ALLOWLISTED
//	(D) a hit in a `.toml` COMMENT
//	(E) a hit in an UNTRACKED file
//
// ⚠ Arms (C) and (D) are the arms that FAIL against the plan's original
// extension enumeration and PASS against the derived surface — they are the
// control FOR THE FIX, not decoration. Arm (E) is the control for the
// FILESYSTEM reading of the surface.
//
// ⚠ EVERY PLANT LANDS OUTSIDE THE ALLOWLIST (d-iv). A plant inside an
// allowlisted file proves nothing.
//
// The plants run on a SCRATCH TREE ⚠ [DOC] rule 20 — `go test -overlay` does
// NOT reach a source scan, so the real tree is never edited.
func TestW8CensusHasItsFivePlants(t *testing.T) {
	arms := []struct{ name, rel, src string }{
		{"(A) template read", "internal/web/templates/plant.html", "<h4>{{.WorkgroupSnapshotName}}</h4>\n"},
		{"(B) column reference in a _test.go", "internal/domain/plant_test.go", "package domain\n\nconst c = \"person.workgroup_id\"\n"},
		{"(C) a .md hit — deliberately in a NON-allowlisted file", "deploy/demo-walkthrough.md", "Expect a workgroup column on the sheet.\n"},
		{"(D) a .toml COMMENT", "deploy/site-a.toml", "# the workgroup picker is dev-only\nsite_id = \"a\"\n"},
		{"(E) an UNTRACKED file", "scratch/notes.txt", "workgroup\n"},
	}
	for _, arm := range arms {
		root := t.TempDir()
		p := filepath.Join(root, filepath.FromSlash(arm.rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(arm.src), 0o644); err != nil {
			t.Fatal(err)
		}
		found := 0
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			b, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			if strings.Contains(strings.ToLower(string(b)), "workgroup") {
				found++
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if found == 0 {
			t.Errorf("W8-6 CONTROL FAILED, (m8-3) arm %s: the plant in %s was NOT detected by the derived surface. "+
				"⚠ Arms (C) and (D) are the ones that fail against the plan's original extension enumeration "+
				"(which listed SQL, of which there are ZERO files, and omitted .md, which has three live hits); "+
				"arm (E) is the one that separates the filesystem reading from the `git ls-files` one", arm.name, arm.rel)
		}
	}
}

// ---------------------------------------------------------------------------
// W8-9 — the ref_alias mechanism is DELETED
// ---------------------------------------------------------------------------

// TestW8RefAliasProjectionIsGone is W8-9(b) — PROJECTION ABSENT, a schema
// assertion with a PLANTED-TABLE positive control (FA-3's, migrating with the
// limb and mirroring W3-13's).
//
// ⭐ Reds today — the projection is registered.
func TestW8RefAliasProjectionIsGone(t *testing.T) {
	st := newDomainTestStore(t)

	// CONTROL FIRST (FA-3's): a PLANTED `ref_alias` table must be detected
	// before the clean assertion counts. Without it a build that created no
	// tables at all would score a pass.
	w8Exec(t, st, `CREATE TABLE w8_planted_ref_alias (duplicate_id TEXT PRIMARY KEY, canonical_id TEXT) STRICT`)
	if !w8TableExists(t, st, "w8_planted_ref_alias") {
		t.Fatalf("W8-9(b) CONTROL FAILED (FA-3's planted table): the introspection cannot see a table that was just " +
			"created, so its report that `ref_alias` is absent would mean nothing")
	}
	w8Exec(t, st, `DROP TABLE w8_planted_ref_alias`)

	if w8TableExists(t, st, "ref_alias") {
		t.Errorf("W8-9(b) ((m8-21)): the `ref_alias` table is created. P-3 §5 schedules the deletion onto W8 and "+
			"D-47 sets the bar: \"ref_alias_created must NOT be carried into v10's frozen event-type vocabulary… "+
			"A freeze list that carries it forward re-creates the retired mechanism as a frozen ghost.\"\n"+
			"sqlite_master: %v", w8TableNames(t, st))
	}
	for _, name := range w8ProjectionNames(t, st) {
		if name == "ref_alias" {
			t.Errorf("W8-9(b) ((m8-21)): `ref_alias` is still in the registered projection set. " +
				"Its registration site is internal/domain/register.go, and it is COUPLED to the route removal " +
				"at internal/web/devroutes.go — neither can land alone")
		}
	}
}

func w8TableExists(t *testing.T, st *store.Store, name string) bool {
	t.Helper()
	for _, n := range w8TableNames(t, st) {
		if n == name {
			return true
		}
	}
	return false
}

// w8ProjectionNames derives the registered projection set from a store that
// Register() has been run against — never from a hand-kept list.
func w8ProjectionNames(t *testing.T, st *store.Store) []string {
	t.Helper()
	b, err := st.DumpProjections()
	if err != nil {
		t.Fatalf("PRECONDITION (instrument): the store cannot dump its projections, so the registered set cannot be derived: %v", err)
	}
	var dump map[string][]map[string]any
	if err := json.Unmarshal(b, &dump); err != nil {
		t.Fatalf("PRECONDITION (instrument): the projection dump did not decode: %v", err)
	}
	var names []string
	for k := range dump {
		names = append(names, k)
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatal("PRECONDITION (instrument): the projection dump is EMPTY — the assertion below could not fail")
	}
	return names
}

// --- W8-9(c): THE MECHANISM CENSUS, term set DERIVED FROM THE DELETED
//
//	SYMBOLS, not hand-typed --------------------------------------------
//
// P-3 §6 requires it in terms: "W8's census term set is widened beyond the
// literal `ref_alias` (e.g. `/dev/alias`) so these cannot silently survive".
//
// ⭐ THE LIMB IS SPLIT IN TWO, BECAUSE THE TWO TERM SETS HAVE DIFFERENT
// SURVIVORS ⟦LOCK — B-3 / FIL-F6⟧.
var (
	// (c-i) THE EXACT-TERM ARM — over EVERY file, NO exclusion set. These are
	// spellings of a DELETED mechanism; a survivor is a defect, not prose.
	w8AliasExactTerms = []string{
		"ref_alias", "refAlias", "RefAlias", "/dev/alias", "alias_inject",
		"CreateAlias", "LoadAliasMap", "dev_alias",
	}
	// ⟦LOCK — FIL-F15⟧ `duplicate_id` and `canonical_id` join this arm, with
	// an ASSERTED allowlist naming the five W3-m26 sites and their reason —
	// so a NEW occurrence is a red and the known ones are a stated exemption
	// rather than a silent one.
	w8AliasKeyTerms = []string{"duplicate_id", "canonical_id"}
	// (c-ii) THE WORD ARM — case-insensitive `alias` MINUS AN ASSERTED
	// EXCLUSION SET. ⚠ The exclusion set NAMES THE SURVIVING SPELLINGS, which
	// is what makes W8-9(d)'s negative arm checkable rather than a caution:
	// AliasMap, Resolve's alias-derived doc text and maxAliasHops SURVIVE by
	// W8-10.
	w8AliasSurvivingSpellings = []string{"AliasMap", "maxAliasHops"}
)

// --- ⟦LOCK — §63, 2026-09-03⟧ THE FILTER, NARROWED TO MATCH THE SUBJECT ----
//
// ⚠ THE DEFECT §63 RULES ON: the census's SUBJECT is REACHABILITY — that the
// deleted mechanism cannot be reached. Its FILTER was `strings.Contains` over
// raw bytes. A comment cannot make anything reachable; a test's NAME cannot; a
// literal asserting absence makes the opposite of reachable. The arm's own
// rationale says it — "a survivor is a defect, NOT PROSE" — and then scanned
// prose anyway. [MEASURED 2026-09-03] of ~200 occurrences under app/: 159 in Go
// comments, 27 in string literals (every one in a _test.go), 13 in markdown, and
// 4 identifiers — ALL FOUR OF WHICH ARE TEST FUNCTION NAMES. Twenty-two files
// reported; ZERO reachable references among them.
//
// ⭐ THE NARROWING IS TO THE FILTER ONLY. The subject is unchanged: any
// reachable reference anywhere under app/ still reds. That claim is PRODUCED,
// not asserted — TestW8CensusFilterIsNarrowedToReachableCode below plants each
// direction and is the reason this narrowing may be trusted.
//
// ⚠ A CENSUS REPORTING "0 OFFENDERS" IS INDISTINGUISHABLE FROM A CENSUS WHOSE
// FILTER MATCHES NOTHING ⟦§56⟧. That is why the narrowing carries its own
// positive control rather than being verified once by hand and left.
//
// WHAT COUNTS AS REACHABLE, per position:
//
//	Go COMMENT              ✗ cannot make anything reachable.
//	Go IDENT, EXACTLY equal ✓ this is "an identifier that could RESOLVE to the
//	  to a term                deleted mechanism" ⟦§63(a)⟧.
//	Go IDENT merely CONTAINING ✗ `TestW3IntakeUnconfirmedIgnoresLegacyRefAlias`
//	  a term                     declares a NEW symbol; it resolves to a test,
//	                             not to the mechanism. ⟦§63(d)⟧ — and the 9
//	                             locked test names are NOT renamed: a locked
//	                             test's name is evidence of what it proved.
//	Go STRING in a _test.go ✗ fixture, absence assertion, or diagnostic text.
//	                          It cannot ship, so it cannot make the mechanism
//	                          reachable in the built binary.
//	Go STRING in PRODUCTION ✓ ⚠ THE ONE PLACE THIS DEVIATES FROM §63(a)'s
//	                          WORDING, DELIBERATELY AND NARROWLY. Four of the
//	                          eight terms — `ref_alias`, `dev_alias`,
//	                          `alias_inject`, `/dev/alias` — are NEVER Go
//	                          identifiers. They are a table name and a route
//	                          path, and they can only ever appear as LITERALS.
//	                          Excluding literals outright would delete FOUR OF
//	                          THE EIGHT TERMS from the census, and a
//	                          reintroduced `CREATE TABLE ref_alias` or
//	                          `mux.HandleFunc("/dev/alias", …)` would sail
//	                          through. That is exactly the outcome §63(b) exists
//	                          to catch — "a narrowed filter that no longer
//	                          catches a real reintroduction has converted a
//	                          noisy check into a silent one, STRICTLY WORSE" —
//	                          and exactly what §63's "do not weaken the SUBJECT"
//	                          forbids. Keeping production literals is therefore
//	                          the narrowing that honours BOTH clauses, and it is
//	                          produced as its own direction below.
//	.md                     ✗ prose, entirely. The 5 precedent citations live
//	                          here and in comments.
//	other files, comment    ✗ a `#` / `//` / `--` line.
//	other files, live line  ✓ a TOML key or a shell command can register a
//	                          route; deploy/*.sh and *.toml are executable
//	                          surface, not prose.
//	UNLEXABLE Go file       ✓ CONSERVATIVE FALLBACK to the raw byte scan. ⚠ A
//	                          file the tokenizer cannot read must NEVER become
//	                          silently invisible to the census — that is the
//	                          §56 dead-plant shape, one door along.
func w8ReachablePositionHits(path string, src []byte, terms []string) []string {
	base := filepath.Base(path)
	switch {
	case strings.HasSuffix(base, ".md"):
		return nil
	case strings.HasSuffix(base, ".go"):
		return w8GoReachableHits(path, src, terms)
	default:
		return w8LineReachableHits(src, terms)
	}
}

func w8GoReachableHits(path string, src []byte, terms []string) []string {
	isTest := strings.HasSuffix(filepath.Base(path), "_test.go")
	fset := token.NewFileSet()
	f := fset.AddFile(path, fset.Base(), len(src))
	lexBroken := false
	var sc scanner.Scanner
	sc.Init(f, src, func(token.Position, string) { lexBroken = true }, scanner.ScanComments)
	found := map[string]bool{}
	for {
		_, tok, lit := sc.Scan()
		if tok == token.EOF || lexBroken {
			break
		}
		switch tok {
		case token.COMMENT:
			// out of subject: a comment cannot make anything reachable.
		case token.IDENT:
			for _, term := range terms {
				if lit == term { // EXACT — "could RESOLVE to", not "mentions"
					found[term] = true
				}
			}
		case token.STRING, token.CHAR:
			if isTest {
				continue
			}
			for _, term := range terms {
				if strings.Contains(lit, term) {
					found[term] = true
				}
			}
		}
	}
	if lexBroken {
		return w8ByteHits(src, terms) // ⚠ conservative, never silent
	}
	return w8SortedTerms(found)
}

func w8LineReachableHits(src []byte, terms []string) []string {
	found := map[string]bool{}
	for _, line := range strings.Split(string(src), "\n") {
		tr := strings.TrimSpace(line)
		if strings.HasPrefix(tr, "#") || strings.HasPrefix(tr, "//") || strings.HasPrefix(tr, "--") {
			continue
		}
		for _, term := range terms {
			if strings.Contains(line, term) {
				found[term] = true
			}
		}
	}
	return w8SortedTerms(found)
}

func w8ByteHits(src []byte, terms []string) []string {
	found := map[string]bool{}
	for _, term := range terms {
		if strings.Contains(string(src), term) {
			found[term] = true
		}
	}
	return w8SortedTerms(found)
}

func w8SortedTerms(found map[string]bool) []string {
	var out []string
	for t := range found {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// TestW8AliasMechanismCensus is W8-9(c). ⭐ Reds today.
//
// ⚠ NO BASELINE COUNT IS ASSERTED ⟦LOCK — ENU-F15⟧: 43 files at revision 2,
// 42 by the arbiter, 46 at the criteria pass — THREE DIFFERENT NUMBERS IN
// THREE DAYS FOR ONE COMMAND. The assertion is NON-ZERO BEFORE, ZERO AFTER.
func TestW8AliasMechanismCensus(t *testing.T) {
	root := w8AppRoot(t)
	files := w8CensusFiles(t, root)
	_, selfPath, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("PRECONDITION (instrument): runtime.Caller could not resolve this file's path")
	}

	// (c-iii) THIS LIMB HAS ITS OWN ALLOWLIST, carrying W8-6(d)'s FOUR
	// MECHANISMS UNCHANGED: self-exclusion by path equality, (file,
	// criterion-id) pairs, the SHRINK TOOTH, and every plant landing outside.
	allow := map[string]w8AllowRow{}
	for _, row := range []w8AllowRow{
		// ⟦LOCK — §63, 2026-09-03⟧ ⚠ ALL THREE ROWS REMOVED, BY THE SHRINK
		// TOOTH'S OWN RULE. §63(c) keeps a small allowlist for genuine CODE
		// references that must exist — and says "DO NOT ADD A ROW BEFORE ITS
		// HIT EXISTS". [MEASURED] under the narrowed filter NONE of the three
		// has a surviving hit, so all three are dead rows, and a dead row is a
		// census FAILURE rather than a skip:
		//   reports/*.md              — markdown is prose; out of subject.
		//   w8_devroutes_test.go      — its `/dev/alias` and `ref_alias_created`
		//                               are STRING LITERALS IN A _test.go, which
		//                               is the absence assertion itself. §63(c)
		//                               anticipated exactly this: the rows
		//                               survive only "if they reference the term
		//                               as code rather than as a literal". They
		//                               do not.
		// ⚠ THE TOOTH ITSELF STAYS WIRED, per §63(c), and is PRODUCED against a
		// deliberately-planted dead row — see the report. An empty allowlist is
		// the correct state, not a disabled mechanism.
		// ⟦was: "\t\t{File: \"reports/fix-attribution.md\", Ground: \"historical build report\"},"⟧
		// ⟦was: "\t\t{File: \"reports/build-c11.md\", Ground: \"historical build report; carries /dev/alias and ZERO workgroup hits, so the workgroup .md measurement could not surface it\"},"⟧
		// ⚠ A ROW REMOVED BECAUSE THE SHRINK TOOTH FIRED ON IT, and the
		// firing is recorded as this limb's produced control:
		// internal/store/w8_source_scans_test.go was allowlisted here on the
		// assumption it carried an alias spelling. It does not — it names
		// `trade_amendment` and nothing alias-shaped — and (d-iii) reported
		// it as a census FAILURE rather than letting a dormant row sit.
		// [PRODUCED 2026-09-03: "W8-9(c-iii) THE SHRINK TOOTH: allowlist row
		// \"internal/store/w8_source_scans_test.go\" … has NO SURVIVING HIT".]
		// ⟦was: "\t\t{File: \"internal/web/w8_devroutes_test.go\", Criterion: \"W8-9(a-4)\", Ground: \"asserts POST /dev/alias appends nothing — the route-absence limb cannot be written without naming the route\"},"⟧
	} {
		allow[filepath.Join(root, filepath.FromSlash(row.File))] = row
	}
	hits := map[string]int{}

	var offenders []string
	for _, f := range files {
		if f == selfPath {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			offenders = append(offenders, fmt.Sprintf("%s: UNREADABLE — not scanned: %v", f, err))
			continue
		}
		// ⟦LOCK — §63, 2026-09-03⟧ the FILTER is narrowed to reachable positions.
		// ⟦was: "\t\ttext := string(b)"⟧
		// ⟦was: "\t\tvar found []string"⟧
		// ⟦was: "\t\tfor _, term := range w8AliasExactTerms {"⟧
		// ⟦was: "\t\t\tif strings.Contains(text, term) {"⟧
		// ⟦was: "\t\t\t\tfound = append(found, term)"⟧
		// ⟦was: "\t\t\t}"⟧
		// ⟦was: "\t\t}"⟧
		found := w8ReachablePositionHits(f, b, w8AliasExactTerms)
		if len(found) == 0 {
			continue
		}
		if row, isAllowed := allow[f]; isAllowed {
			hits[row.File]++
			continue
		}
		rel, _ := filepath.Rel(root, f)
		offenders = append(offenders, fmt.Sprintf("%s: %v", rel, found))
	}

	// THE SHRINK TOOTH, on this limb's own allowlist.
	for p, row := range allow {
		if hits[row.File] == 0 {
			if _, err := os.Stat(p); err == nil {
				t.Errorf("W8-9(c-iii) THE SHRINK TOOTH: allowlist row %q (%s) has NO SURVIVING HIT — a dead row is a "+
					"census FAILURE, never a skip", row.File, row.Ground)
			}
		}
	}

	if len(offenders) != 0 {
		sort.Strings(offenders)
		shown := offenders
		if len(shown) > 30 {
			shown = shown[:30]
		}
		t.Fatalf("W8-9(c-i) ((m8-20)): %d file(s) under app/ still carry a spelling of the DELETED alias mechanism.\n"+
			"The term set is DERIVED FROM THE DELETED SYMBOLS, not hand-typed: %v.\n"+
			"⚠ It reaches C-1.4: the four site-*.toml files carry dev-alias text in COMMENTS, not keys, so a census "+
			"that PARSES TOML instead of READING BYTES misses all four.\n"+
			"⚠ NO BASELINE COUNT IS ASSERTED — three roles measured this command three times in three days and got "+
			"43, 42 and 46. The assertion is NON-ZERO BEFORE, ZERO AFTER.\n"+
			"first %d of %d:\n  %s", len(offenders), w8AliasExactTerms, len(shown), len(offenders), strings.Join(shown, "\n  "))
	}
}

// TestW8CensusFilterIsNarrowedToReachableCode is ⟦§63(b)⟧ — ⭐ THE PRODUCED
// CONTROL FOR THE NARROWING, IN BOTH DIRECTIONS.
//
// ⚠ WITHOUT THIS TEST THE NARROWING IS UNVERIFIABLE, AND WORSE THAN THE NOISE
// IT REPLACED. §63(b): "a narrowed filter that no longer catches a real
// reintroduction has converted a noisy check into a silent one — STRICTLY
// WORSE". A census reporting "0 offenders" looks identical whether the tree is
// clean or the filter matches nothing, which is §56's dead-plant shape wearing
// a different hat: the same green, two opposite meanings.
//
// So the filter is exercised on synthetic sources whose ANSWER IS KNOWN, in
// both directions, every run — never verified once by hand and trusted after.
// The end-to-end wiring (this filter actually driving W8-9(c-i) over the real
// tree) is produced separately, by planting into a copy of app/ and watching
// the census red; that cannot be a unit test, because the census's surface IS
// the tree.
func TestW8CensusFilterIsNarrowedToReachableCode(t *testing.T) {
	terms := w8AliasExactTerms
	for _, c := range []struct {
		Name    string
		Path    string
		Src     string
		WantHit bool
		Why     string
	}{
		// --- DIRECTION 1: a REACHABLE reference must RED --------------
		{
			Name:    "D1 live code reference in PRODUCTION — a call to the deleted loader",
			Path:    "internal/domain/refdata.go",
			Src:     "package domain\n\nfunc f(st *store.Store) { _, _ = LoadAliasMap(st) }\n",
			WantHit: true,
			Why: "The reintroduction the census exists to catch. If this does not red, the narrowing has " +
				"converted a noisy check into a silent one and MUST be reverted.",
		},
		{
			Name:    "D1b live code reference in a TEST file — identifiers count everywhere",
			Path:    "internal/domain/x_test.go",
			Src:     "package domain\n\nfunc f() { _ = CreateAlias }\n",
			WantHit: true,
			Why: "⚠ The _test.go exemption is for STRING LITERALS ONLY. An identifier that resolves to the " +
				"deleted mechanism is a reachable reference wherever it is written, or the subject would be " +
				"weakened to `production only` — which §63 does not authorise.",
		},
		{
			Name:    "D4 PRODUCTION route literal — the deleted dev route re-registered",
			Path:    "internal/web/devroutes.go",
			Src:     "package web\n\nfunc r(mux *http.ServeMux) { mux.HandleFunc(\"/dev/alias\", nil) }\n",
			WantHit: true,
			Why: "⭐ THE DEVIATION FROM §63(a)'s WORDING, PRODUCED. `/dev/alias` is NEVER a Go identifier — " +
				"it is a route path and can only appear as a literal. Excluding literals outright would make " +
				"this reintroduction INVISIBLE, which is the exact failure §63(b) exists to detect.",
		},
		{
			Name:    "D5 PRODUCTION SQL literal — the deleted table re-created",
			Path:    "internal/store/projection.go",
			Src:     "package store\n\nconst q = `CREATE TABLE ref_alias (duplicate_id TEXT)`\n",
			WantHit: true,
			Why: "Same class as D4 and the more serious one: `ref_alias` is the deleted TABLE. A census that " +
				"cannot see a CREATE TABLE for it is not a census of the mechanism.",
		},
		{
			Name:    "D7 non-Go LIVE line — a config key naming the deleted mechanism",
			Path:    "deploy/site-a.toml",
			Src:     "site_id = \"a\"\ndev_alias = true\n",
			WantHit: true,
			Why: "deploy/*.toml and deploy/*.sh are executable surface, not prose. A key can make a route " +
				"reachable.",
		},
		{
			Name:    "D10 UNLEXABLE Go file — the conservative fallback, in a _test.go so only the fallback can find it",
			Path:    "internal/domain/broken_test.go",
			Src:     "package domain\n\nvar x = \"ref_alias\n",
			WantHit: true,
			Why: "⚠ A file the tokenizer cannot read must NEVER become silently invisible to the census. The " +
				"term here sits in a string literal in a _test.go, which the narrowed filter would normally " +
				"skip — so ONLY the raw-byte fallback can produce this hit, which is what makes the fallback " +
				"checkable rather than decorative. This is §56's lesson one door along: the failure mode of a " +
				"scanner that cannot parse its input must be LOUD, never a quiet zero.",
		},

		// --- DIRECTION 2: NON-reachable material must NOT red ---------
		{
			Name:    "D2 the same term in a Go COMMENT",
			Path:    "internal/domain/refdata.go",
			Src:     "package domain\n\n// LoadAliasMap and ref_alias were deleted by P-3 §5.\nfunc f() {}\n",
			WantHit: false,
			Why: "⚠ THE HEADLINE OF §63. A comment cannot make anything reachable. [MEASURED] 159 of ~200 " +
				"occurrences under app/ are exactly this, and the arm's own rationale already said so — " +
				"\"a survivor is a defect, NOT PROSE\" — while scanning prose anyway.",
		},
		{
			Name:    "D3 a LOCKED TEST'S NAME containing the term",
			Path:    "internal/web/w3_reader_surfaces_test.go",
			Src:     "package web\n\nfunc TestW3IntakeUnconfirmedIgnoresLegacyRefAlias(t *testing.T) {}\n",
			WantHit: false,
			Why: "⟦§63(d)⟧ The 9 locked test names are NOT renamed: a locked test's name is EVIDENCE OF WHAT " +
				"IT PROVED, and renaming to satisfy a scan is destroying the record to please the instrument. " +
				"The identifier arm matches EXACTLY, never as a substring — which is also what \"could RESOLVE " +
				"to the deleted mechanism\" actually means: this name resolves to a test, not to the mechanism.",
		},
		{
			Name:    "D6 a STRING LITERAL IN A TEST asserting the term's ABSENCE",
			Path:    "internal/store/w7_sentinel_scan_test.go",
			Src:     "package store\n\nvar forbidden = []string{\"ref_alias\", \"/dev/alias\"}\n",
			WantHit: false,
			Why: "⚠ A literal asserting absence makes the OPPOSITE of reachable — it is the census's own " +
				"subject matter. This is the group §63(c) anticipated might need allowlist rows; measured, it " +
				"does not, because these are literals rather than code, so no row is added. ⟦§63(c): do not " +
				"add a row before its hit exists.⟧",
		},
		{
			Name:    "D8 non-Go COMMENT line",
			Path:    "deploy/site-a.toml",
			Src:     "site_id = \"a\"\n# dev_alias was removed by P-3 §5; do not reinstate\n",
			WantHit: false,
			Why:     "The same rule as D2, in the file type that motivated byte-reading in the first place.",
		},
		{
			Name:    "D9 MARKDOWN prose — a precedent citation",
			Path:    "reports/build-c11.md",
			Src:     "The `ref_alias_created` ghost shape is cited by DR-W6-4 and W7-12(d).\n",
			WantHit: false,
			Why:     "The 5 precedent citations. Markdown is prose entire; it ships to no one and executes nowhere.",
		},
	} {
		got := w8ReachablePositionHits(c.Path, []byte(c.Src), terms)
		switch {
		case c.WantHit && len(got) == 0:
			t.Errorf("⟦§63(b)⟧ FILTER CONTROL FAILED (%s): a REACHABLE reference was NOT detected.\n"+
				"  path: %s\n  source:\n%s\n"+
				"  ⛔ THE NARROWING HAS GONE TOO FAR. A filter that no longer catches a real reintroduction "+
				"has converted a noisy check into a SILENT one, which is strictly worse than the 22 false "+
				"positives it replaced. Widen the filter or revert it; do NOT relax this control.\n  %s",
				c.Name, c.Path, c.Src, c.Why)
		case !c.WantHit && len(got) != 0:
			t.Errorf("⟦§63(b)⟧ FILTER CONTROL FAILED (%s) (negative arm): NON-reachable material was flagged as "+
				"%v.\n  path: %s\n  source:\n%s\n"+
				"  ⚠ The census's SUBJECT is reachability. Flagging this is the filter-wider-than-its-subject "+
				"defect §63 exists to close, and a checker that fires on correct material gets waved through.\n  %s",
				c.Name, got, c.Path, c.Src, c.Why)
		case c.WantHit:
			t.Logf("⟦§63(b)⟧ FILTER OK (%s): detected %v", c.Name, got)
		default:
			t.Logf("⟦§63(b)⟧ FILTER OK (%s): correctly silent", c.Name)
		}
	}
}

// TestW8AliasCensusDoesNotFlagTheSurvivors is W8-9(d) — THE NEGATIVE ARM.
//
// "What survives, so the census cannot be satisfied by OVER-DELETING.
// AliasMap, Resolve and maxAliasHops MUST still exist — W8-10. The census's
// term set therefore EXCLUDES the surviving three BY NAME, and that exclusion
// is ASSERTED, with a NEGATIVE arm: a census that flags `AliasMap` is broken,
// and a build that deletes it does not compile."
//
// ⚠ This is the arm that stops W8-9(c) being satisfied by deleting more than
// the criterion asks for.
func TestW8AliasCensusDoesNotFlagTheSurvivors(t *testing.T) {
	// --- CONTROL FIRST, PRODUCED: a term set that DID reach a survivor must
	// be reported by this same predicate. ⚠ Without it the clean pass below
	// is a green over a comparison that may never match anything, which is
	// the "check that cannot fail" shape one door along.
	if !w8TermReachesSurvivor("Alias", w8AliasSurvivingSpellings) {
		t.Fatal("W8-9(d) CONTROL FAILED: a census term `Alias` — which is a prefix of the SURVIVING `AliasMap` — " +
			"was NOT reported as reaching a survivor. The negative arm cannot fire, so its clean pass says nothing")
	}
	if w8TermReachesSurvivor("ref_alias", w8AliasSurvivingSpellings) {
		t.Fatal("W8-9(d) CONTROL FAILED (negative): the term `ref_alias` was reported as reaching a survivor. " +
			"ref_alias is a spelling of the DELETED mechanism and must be flagged BY the census, not exempted")
	}

	for _, survivor := range w8AliasSurvivingSpellings {
		for _, term := range w8AliasExactTerms {
			if w8TermReachesSurvivor(term, []string{survivor}) {
				t.Errorf("W8-9(d) THE NEGATIVE ARM: the census term %q matches the SURVIVING spelling %q. "+
					"A census that flags %q is broken: W8-10 requires AliasMap, Resolve and maxAliasHops to "+
					"SURVIVE the deletion, and a build that deletes them does not compile — it fails in TWO "+
					"LOCKED CHUNKS' files (person_amend.go's maxMergeWalk and company_amend.go's bounded loop)",
					term, survivor, survivor)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// W8-10(a) — the compile-forced re-homing
// ---------------------------------------------------------------------------

// TestW8SurvivingAliasSymbolsAreNotDeclaredInAnAliasNamedFile is W8-10(a).
//
// ⟦LOCK — FAL-F15: the filename clause is STATED PRECISELY OR IT IS NOT
// CHECKABLE — *the declaring file's BASE NAME does not contain `alias`,
// resolved through go/types TOKEN POSITIONS* (never a grep over paths, which
// cannot tell a declaration from a mention). Its plant: one of the three
// symbols left declared in a file whose base name contains `alias`.⟧
//
// ⚠ The criterion asserts the PROPERTY, not the filename — a plan owner who
// prefers a destination other than DR-W8-10's `merge.go` changes one word in
// §3 and no criterion here.
//
// ⭐ ⟦LOCK — ENU-F12: W8-10(a) IS BOOKED AS A PRE-CHUNK RED, and it fell
// through all three of §6's accounts. It is a SOURCE-SCAN red, not a runtime
// one, stated so a gate does not book it as a runtime failure.⟧
func TestW8SurvivingAliasSymbolsAreNotDeclaredInAnAliasNamedFile(t *testing.T) {
	root := w8AppRoot(t)
	dir := filepath.Join(root, "internal", "domain")

	decls, diags := w8DeclarationFiles(t, dir, []string{"AliasMap", "maxAliasHops", "Resolve"})
	s56LogTree(t, "w8DeclarationFiles", diags)

	// PRECONDITION: the three symbols must EXIST. W8-10's whole content is
	// that they SURVIVE the deletion; an absence here is a different (and
	// worse) failure than a mis-placed declaration, and must not be reported
	// as this one.
	for _, sym := range []string{"AliasMap", "maxAliasHops"} {
		if decls[sym] == "" {
			t.Fatalf("PRECONDITION / W8-10(a): %q is not declared anywhere in internal/domain. The criterion is that "+
				"it SURVIVES the deletion: P-3 §5 says \"the AliasMap shape and hop-capped Resolve survive as the "+
				"read API over a new loader\", and W3's LOCKED cycle guard compiles against maxAliasHops "+
				"(person_amend.go's `const maxMergeWalk = maxAliasHops`). Deleting them does not fail a test — "+
				"IT FAILS TO COMPILE, IN TWO LOCKED CHUNKS' FILES, which is (m8-23)\n"+
				"⟦§56⟧ what this scan type-checked — an ABSENT symbol and a source that did not compile are "+
				"different findings and must not be rendered as the same one:\n%s", sym, diags.Report())
		}
	}

	for sym, file := range decls {
		if file == "" {
			continue
		}
		if strings.Contains(strings.ToLower(filepath.Base(file)), "alias") {
			t.Errorf("W8-10(a) ((m8-23)/(m8-24)): %q is declared in %q, whose BASE NAME contains `alias` — a file "+
				"named for the DELETED mechanism. ⟦DR-W8-10⟧ re-homes AliasMap, Resolve and maxAliasHops to "+
				"internal/domain's merge surface. ⚠ The criterion asserts the PROPERTY, not the filename: a plan "+
				"owner who prefers another destination changes one word in §3 and no criterion.\n"+
				"⚠ P-3 says \"and alias.go\" is DELETED and says AliasMap/Resolve SURVIVE. Together those mean a "+
				"MOVE, and no document assigns it — the move is COMPILE-FORCED", sym, filepath.Base(file))
		}
	}
}

// w8DeclarationFiles resolves each named symbol to the file that DECLARES it,
// through go/types token positions — never through a grep over paths, which
// cannot tell a declaration from a mention.
func w8DeclarationFiles(t *testing.T, dir string, symbols []string) (map[string]string, s56Set) {
	t.Helper()
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
		src, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), src, 0)
		if err != nil {
			t.Fatalf("PRECONDITION (instrument): %s did not parse, so this scan's coverage is smaller than its stated scope: %v", e.Name(), err)
		}
		parsed = append(parsed, f)
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}}
	// ⟦LOCK — §56(1), 2026-09-03⟧ RECORDED, never discarded. ⟦was:
	// "conf := types.Config{Error: func(error) {}, Importer: nil, DisableUnusedImportCheck: true}"⟧
	// This scan reads `info.Defs`; a source that does not type-check
	// contributes NO Defs, so the symbol reads as UNDECLARED — which this
	// test then reports as a criterion failure rather than as what it is.
	dg := s56Diags{Where: dir}
	conf := types.Config{Error: dg.Record, Importer: nil, DisableUnusedImportCheck: true}
	_, _ = conf.Check("w8homescan", fset, parsed, info)
	diags := s56Set{dg}

	want := map[string]bool{}
	for _, s := range symbols {
		want[s] = true
	}
	out := map[string]string{}
	for _, s := range symbols {
		out[s] = ""
	}
	for ident, obj := range info.Defs {
		if obj == nil || !want[ident.Name] {
			continue
		}
		// Package-level declarations only — a local variable named Resolve is
		// not the symbol this criterion is about.
		switch o := obj.(type) {
		case *types.TypeName, *types.Const:
			if o.Parent() != nil && o.Parent().Parent() == types.Universe {
				out[ident.Name] = fset.Position(ident.Pos()).Filename
			}
		case *types.Func:
			out[ident.Name] = fset.Position(ident.Pos()).Filename
		}
	}
	return out, diags
}

// w8TermReachesSurvivor reports whether a census term would match one of the
// spellings W8-10 requires to SURVIVE.
func w8TermReachesSurvivor(term string, survivors []string) bool {
	for _, s := range survivors {
		if strings.Contains(s, term) {
			return true
		}
	}
	return false
}
