// w10_compare_set_test.go — W10-10, the reconciliation that runs DUMP → LISTS.
//
// Criteria source: design/WINDOW-BUILD-PLAN-v1.md §3, `### W10 — Window
// integration and rehearsal`, LOCKED 2026-09-03 (reports/W10-FOLD-RECORD.md,
// gate P-4). Written from the LOCKED CRITERION TEXT ONLY.
//
// Criterion carried here:
//
//	W10-10 — "Every key the dump emits on the pinned tree is either in
//	cmd/dumpcompare's compareSet or in a named EXCLUDE list carrying one
//	stated reason per member."
//	DIRECTION, on the face of the criterion: "The reconciliation runs
//	DUMP → LISTS, never LISTS → DUMP. The dump is the subject."
//	FALSIFIER: "A key in the dump that is in neither list."
//	CONTROL, quoted: "An exact-set-literal test cannot detect a projection
//	registered at a SECOND site — it compares a literal to a literal. The
//	control that can: register a throwaway projection in a test store and
//	assert the reconciliation reds. If the check is written instead as
//	'compare compareSet to a hand-typed list of seventeen', it is the same
//	enumeration one level up and the control will not red."
//
// ⚠ THIS FILE IS IN `internal/deadline` ON PURPOSE, AND THAT IS THE WHOLE
// POINT OF DR-W10-4. The dump's key set is produced by TWO registration
// sites — internal/domain/register.go and internal/deadline/ack.go — and
// cmd/sitectl/main.go calls both. `internal/deadline` is the only internal
// package that can import both, so it is the only place the real dump key
// set can be built. A test living in `internal/domain` would measure
// sixteen names and miss `alert_ack`, which is the brief's own measured
// error ("the registered set is sixteen") reproduced one level down.
//
// ⚠ WHAT IS ANCHORED AND MUST NOT BE REFRESHED. W10-10's numbers are
// `registered: 17 · compared: 12 · unaccounted: 5` AT THE PIN e9cc5f5, and
// the criterion says in terms that this "is the only tree this criterion's
// subject may be". THIS TEST RUNS ON THE WORKING TREE, which at the time of
// writing carries more (W7's `vehicle` in compareSet; W9a/W9b/W9c's
// projections uncommitted). So NOTHING HERE ASSERTS A COUNT. It asserts the
// PROPERTY — set emptiness — and REPORTS the tree it measured, per L-K. The
// pin's 17/12/5 is quoted above as a literal and is never re-derived.
//
// ⚠ RULE 20. The tool's STATED EXCLUSIONS are read from
// cmd/dumpcompare/main.go ON DISK, so `go test -overlay` does not reach
// them. The controls below therefore plant into a REAL on-disk copy
// (t.TempDir()); nothing under app/ is written.
//
// ⟦REPAIR — S96B, 2026-09-03⟧ ⛔ THIS FILE PARSED `var compareSet = []string{…}`
// UNTIL TODAY, AND THE VAR NO LONGER EXISTS. ⟦LOCK — §67⟧ replaced the
// hand-kept list with a DERIVATION (`buildCompareSet` in cmd/dumpcompare — the
// registry minus stated exclusions), because a hand-kept list of 12 against a
// registry of 23 let two posts genuinely DISAGREE about the other 11 while the
// tool printed "IDENTICAL" and exited 0.
//
// ⚠⚠ THE SECOND-ORDER HARM IS WHY THIS WAS A GATE BLOCKER, AND IT IS NOT THE
// STALE SYMBOL. Both tests below died at `t.Fatalf` on their FIRST STATEMENT,
// in 0.00s, BEFORE the plant was registered — so the control had never tested
// anything, while the failure list showed a W10-10 red that reads exactly like
// a criterion failure. ⭐ SAME COLOUR, DIFFERENT FACT (§75).
//
// ⭐ SO THE REPAIR IS TWO THINGS, NOT ONE:
//  1. the base is RE-POINTED to the derivation, so the control reaches its plant;
//  2. every instrument failure now says "INSTRUMENT BROKEN" IN THOSE WORDS and
//     is a `t.Fatalf` that names the symbol it could not read — so the NEXT
//     shape change is legible as a broken instrument instead of a criterion red.
//     `TestW10TheInstrumentAnnouncesItselfBrokenRatherThanRedding` plants this
//     exact defect (a main.go in the superseded `var compareSet` shape) and
//     proves the reader says so.
//
// ⟦REPAIR — §219(3), 2026-09-04⟧ THE READER WAS RE-POINTED A SECOND TIME, for
// the opposite reason. S127 (bbff17e) struck all three class exclusions — the
// eight side tables are COMPARED now — and left `ignoredProjectionClasses`
// declared and EMPTY with the struck entries preserved above it. The reader's
// empty-map guard, written when empty could only mean "restructured", killed
// four tests (two here) before the criterion ran, so the sentinel could not
// state its own magnitude and S127 had to derive it BY HAND from the binary's
// stdout. The guard is NARROWED, not removed — see w10MapDecl — and the
// magnitude is now ASSERTED (repair-queue row B2), at the bottom of this file.
//
// ⚠ THE SUPERSEDED PARSER AND THE SUPERSEDED HAND-TYPED EXCLUDE MAP are
// preserved verbatim in reports/window-build/S96B-W10-CONTROL.md §5, not here
// (§78's pointer rule).
//
// PRODUCED REDS (transcripts in reports/window-build/W10-tests.md §4):
//   - TestW10EveryDumpedProjectionIsComparedOrExcludedWithAStatedReason is
//     RED on this tree, and that is the criterion's own state: category (C)
//     REGISTERED-NOWHERE is non-empty. Its members are printed by name.
//   - TestW10TheReconciliationSeesAProjectionRegisteredAtASecondSite is the
//     criterion's OWN named control, run: a throwaway projection registered
//     after both Register calls reds the certified rule while the named
//     rival (literal-vs-literal) stays green.
package deadline

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// w10ModuleRoot walks up from the working directory to the directory holding
// go.mod. Minted here rather than shared: W10 writes only w10_* files and
// must not couple to another window's helper.
func w10ModuleRoot(t *testing.T) string {
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

// ⭐ THE LIST SIDE IS DERIVED, NEVER PARSED FROM A LITERAL — ⟦LOCK — §67⟧.
//
// W10-10's CONTROL says a hand-typed list "is the same enumeration one level up
// and the control will not red", and this file used to honour that by PARSING
// the tool's `compareSet` literal. ⛔ That was coupling to a SYNTACTIC SHAPE,
// not to the tool's meaning: §67 kept the meaning and changed the shape, and the
// parse did not follow. It is the §89 class — a string-keyed coupling the
// compiler cannot see. ⚠ And the premise this file carried until today — that
// the list side is "derived from the tool's own source and moves when the tool
// moves" — had quietly become FALSE while still reading like a guarantee. (The
// superseded text is quoted in reports/window-build/S96B-W10-CONTROL.md §5;
// naming a LINE for it here would rot the moment this file is edited again.)
//
// ⚠ THE COUPLING IS NARROWED, NOT REMOVED, AND SAYING SO IS THE POINT. The
// REGISTRY half is now a real Go call — `w10DumpKeys`, the same both-Register
// dump `registeredProjections` (cmd/dumpcompare/main.go) builds — so it cannot go stale
// silently. Only the EXCLUSION half is still read from source, because
// `ignoredProjections` lives in `package main` and Go cannot import a main
// package. ⭐ Two symbols instead of one, but both now fail LOUDLY AS AN
// INSTRUMENT rather than as a criterion. The cost of removing the last of it —
// moving the exclusion policy into an importable package — is priced in
// reports/window-build/S96B-W10-CONTROL.md §6.

// w10ConstString renders a string-literal expression, following `+`
// concatenation. ⚠ Deliberately more than `strings.Trim(v, "\"")`: a reason
// string written as "a" + "b" is a BinaryExpr, and a reader that skips it
// reports the entry as having NO STATED REASON — a false red on the one
// property this exclusion list exists to carry.
func w10ConstString(e ast.Expr) (string, bool) {
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
		l, lok := w10ConstString(v.X)
		r, rok := w10ConstString(v.Y)
		if !lok || !rok {
			return "", false
		}
		return l + r, true
	case *ast.ParenExpr:
		return w10ConstString(v.X)
	}
	return "", false
}

// w10MapDecl is one top-level `var <name> = map[string]string{…}`, read with
// enough of its surroundings to tell apart three states a bare map cannot:
//
//   - EMPTY: the literal carries no elements.
//   - UNREAD: the literal carries elements this reader could not render as
//     string → string — the map's shape has changed under the reader.
//   - STRUCK: the literal is empty AND the declaration's own doc comment
//     preserves the entries it used to carry, as ⟦was: "…"⟧ lines (§78's
//     pointer rule) — which is what a deliberate deletion looks like in this
//     tree, and the only shape in which an empty class map reads as a policy.
//
// ⟦REPAIR — §219(3), 2026-09-04⟧ ⛔ UNTIL TODAY THE READER COLLAPSED ALL THREE
// INTO "EMPTY" AND CALLED EMPTY INSTRUMENT-BROKEN. That was right while the
// class map could only be empty by accident. S127 (bbff17e) struck all three
// class rules ON PURPOSE — their stated reasons were false, measured in
// reports/window-build/S126-RETRACTION.md — leaving the map declared, empty,
// and with its three struck entries preserved above it. The reader died on it
// before the criterion ran, so the W10-10 sentinel could not state its own
// magnitude and S127 had to derive it by hand from the binary's stdout.
// ⚠ THE GUARD IS NARROWED, NOT REMOVED: EMPTY-with-no-record and UNREAD are
// still INSTRUMENT BROKEN, and the control below produces both.
type w10MapDecl struct {
	Entries    map[string]string // every element rendered as string → string
	Unreadable int               // elements in the literal this reader could NOT render
	Struck     []string          // keys the doc comment preserves as ⟦was: "…"⟧ map-entry lines, sorted
}

// w10TopLevelMapDecl reads a top-level `var <name> = map[string]string{…}`
// out of PATH, with its doc comment. It returns an ERROR rather than calling
// t.Fatalf, so the controls below can drive it over planted trees and ASSERT
// the failure text.
//
// ⚠ Duplicated from the landed W7-12(e) reader rather than shared, for the
// reason this file already records at the w10ParseCompareSetFile note in
// internal/store: W10 writes only w10_* TEST files, so a shared helper would
// mean a non-test file. The duplication is recorded, not hidden.
func w10TopLevelMapDecl(path, name string) (w10MapDecl, error) {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
	if err != nil {
		return w10MapDecl{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	decl := w10MapDecl{Entries: map[string]string{}}
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
					k, kok := w10ConstString(kv.Key)
					v, vok := w10ConstString(kv.Value)
					if !kok || !vok {
						decl.Unreadable++
						continue
					}
					decl.Entries[k] = v
				}
				// `var x = …` carries its doc on the GenDecl; a spec inside a
				// `var ( … )` block carries it on the ValueSpec. Read both.
				decl.Struck = w10StruckEntries(gd.Doc, vs.Doc)
			}
		}
	}
	if !found {
		return w10MapDecl{}, fmt.Errorf("no top-level `var %s = map[string]string{…}` in %s", name, path)
	}
	return decl, nil
}

// w10StruckEntries reads the ⟦was: "…"⟧ lines of a declaration's doc comment
// and returns the KEYS of those that read back as map entries.
//
// ⚠ A PROPERTY, NOT A WORD. The text inside ⟦was: …⟧ is a Go string literal
// of the superseded source line. It is unquoted, wrapped back into a map
// literal and PARSED, so only a line that really was an entry counts: a
// preserved comment, a preserved brace, or the word "was" on its own is not
// evidence that an entry was struck. A reader satisfied by the marker alone
// would accept an empty map above which someone had preserved anything at
// all — §94's rider one more time.
func w10StruckEntries(groups ...*ast.CommentGroup) []string {
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
			if k, kok := w10ConstString(kv.Key); kok {
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}

// w10ReadToolExclusions reads BOTH of the tool's exclusion maps.
//
// ⚠ THERE ARE TWO AND BOTH ARE REQUIRED. ⟦LOCK — §82⟧ split them: three
// standing architectural scans (W3-14, W4-13b, W5d-15) forbid any package
// outside internal/domain from naming a side table, so the side-table
// exclusions became CLASS SUFFIXES and the consumer names none of them.
// [MEASURED at the §82 repair, recorded in w7_sentinel_scan_test.go] reading
// only the named map reported "20 compared" where the tool compares 12 — a
// test that had stopped describing its subject. Reading one map here would
// reproduce that silently.
//
// ⟦REPAIR — §219(3), 2026-09-04⟧ ⛔ THE CLASS MAP MAY NOW BE EMPTY, IN EXACTLY
// ONE WAY. S127 (bbff17e) struck all three class rules and left the map
// declared and empty with the struck entries preserved above it as ⟦was:⟧
// lines. Three states that used to read identically are told apart here, and
// only the first is a policy:
//
//   - EMPTY BY A RECORDED STRIKE — no elements, and the doc comment preserves
//     the entries that were struck. No class rule is in force; returned as an
//     empty map WITH the struck keys, so the caller can say so in its output.
//   - EMPTY WITH NO STRIKE RECORD — either this reader is looking at the wrong
//     declaration, or a class rule was deleted without being preserved (§78's
//     pointer rule). INSTRUMENT BROKEN, as before.
//   - UNREAD — the literal has elements this reader cannot render as
//     string → string: the policy has been restructured under the reader,
//     the §89 class that killed this file once already. INSTRUMENT BROKEN.
//     ⚠ Until today this case READ AS EMPTY, so a restructured map and a
//     struck one were the same map to this reader — which is why the empty
//     guard could not simply be deleted to let the struck one through.
func w10ReadToolExclusions(mainGoPath string) (named, classes map[string]string, struckClasses []string, err error) {
	n, err := w10TopLevelMapDecl(mainGoPath, "ignoredProjections")
	if err != nil {
		return nil, nil, nil, err
	}
	c, err := w10TopLevelMapDecl(mainGoPath, "ignoredProjectionClasses")
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

// w10ToolExclusions is w10ReadToolExclusions with the INSTRUMENT-BROKEN
// failure attached.
//
// ⛔ THE WORDING IS THE REPAIR. Until today this instrument's failure was
// indistinguishable from a criterion failure: the gate saw a W10-10 red and
// would have concluded the criterion was violated, when in fact the test died
// before measuring anything. A t.Fatalf that says INSTRUMENT BROKEN, names the
// symbol, and names the ruling that moved it cannot be misread that way.
func w10ToolExclusions(t *testing.T, mainGoPath string) (named, classes map[string]string) {
	t.Helper()
	named, classes, struck, err := w10ReadToolExclusions(mainGoPath)
	if err != nil {
		t.Fatalf("W10-10 INSTRUMENT BROKEN — this is NOT a criterion failure: %v\n"+
			"  The list side of this reconciliation is DERIVED from the tool's registry minus its\n"+
			"  stated exclusions (⟦LOCK — §67⟧, buildCompareSet at cmd/dumpcompare/main.go). If the\n"+
			"  tool's exclusion policy has been renamed or restructured again, RE-POINT THIS READER —\n"+
			"  do NOT read this red as 'a dumped projection is unaccounted for'.", err)
	}
	if len(classes) == 0 {
		// ⟦REPAIR — §219(3)⟧ said in the output, not left to be inferred from
		// a larger compare set: the class map is empty ON PURPOSE.
		t.Logf("W10-10 READER: `ignoredProjectionClasses` is EMPTY BY A RECORDED STRIKE of %v (S127, bbff17e) — "+
			"no class rule is in force, so every side table is in the compare set", struck)
	}
	for k, why := range named {
		if strings.TrimSpace(why) == "" {
			t.Errorf("W10-10: exclusion %q states NO reason — the criterion requires "+
				"\"one stated reason per member\", and a bare name satisfies the shape, not the requirement", k)
		}
	}
	for k, why := range classes {
		if strings.TrimSpace(why) == "" {
			t.Errorf("W10-10: exclusion CLASS %q states NO reason", k)
		}
	}
	return named, classes
}

// w10Excluded builds the tool's own exclusion predicate — the named map, then
// the class suffixes. It mirrors `w67Ignored` (cmd/dumpcompare/main.go) and
// must keep mirroring it.
func w10Excluded(named, classes map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		if why, ok := named[name]; ok {
			return why, true
		}
		for suffix, why := range classes {
			if strings.HasSuffix(name, suffix) {
				return why, true
			}
		}
		return "", false
	}
}

// w10DeriveCompareSet is the tool's compare set: the registry from BOTH
// registration sites, minus the stated exclusions.
//
// ⚠ IT IS NOT A FULL RE-IMPLEMENTATION OF `buildCompareSet`, AND THE GAP IS
// NAMED RATHER THAN IMPLIED — §108's third point, the class where a judgement
// helper cites a rule on its face and does not implement it. `buildCompareSet`
// also carries the SHRINK TOOTH: an exclusion naming a projection that is no
// longer registered, or a class suffix matching nothing, makes the TOOL RETURN
// AN ERROR and compare nothing at all. ⭐ This helper would instead return a
// perfectly ordinary set. So the two agree on the CONTENTS of a healthy compare
// set — which is what every assertion here reads — and DISAGREE about a rotted
// exclusion list, where the tool refuses and this does not.
//
// ⭐ THAT GAP IS COVERED, AND NOT BY THIS FILE: the tooth is asserted directly
// by `TestW7DumpCompareSetCarriesVehicle`'s PROPERTY 2
// (internal/store/w7_sentinel_scan_test.go), which reds on a dead name and on a
// dead class. Duplicating it here would be a second hand-kept copy of a rule,
// which is the defect one layer up. ⚠ If that W7 test is ever deleted, the
// tooth loses its only assertion and this comment becomes the pointer to a
// hole.
func w10DeriveCompareSet(t *testing.T, mainGoPath string) []string {
	t.Helper()
	registered := w10DumpKeys(t)
	if len(registered) == 0 {
		t.Fatalf("W10-10 INSTRUMENT BROKEN — this is NOT a criterion failure: the registry dump is " +
			"EMPTY, so every property below would be vacuously true")
	}
	named, classes := w10ToolExclusions(t, mainGoPath)
	excluded := w10Excluded(named, classes)
	var out []string
	for _, n := range registered {
		if _, skip := excluded(n); skip {
			continue
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		t.Fatalf("W10-10 INSTRUMENT BROKEN — this is NOT a criterion failure: EVERY one of the %d "+
			"registered projections is excluded, so the compare set is empty and no red below could "+
			"mean anything", len(registered))
	}
	return out
}

// ⟦SENTINEL — restored by S96B after §108, 2026-09-03⟧ w10ExcludeWithStatedReason
// is W10-10's EXCLUDE list, category (A) EXCLUDED-WITH-A-REASON-IN-THE-TOOL.
//
// ⛔⛔ ONE ENTRY IS NOT A STALE MEASUREMENT. IT IS THE FINDING. The LOCKED
// criterion measured the pin and recorded, in a table, that of the five
// unaccounted keys EXACTLY ONE has a stated reason:
//
//	alert_ack             — the tool's own header, by name — the only one
//	person_amendment      — reason stated NOWHERE
//	occurred_at_correction— reason stated NOWHERE
//	company_amendment     — reason stated NOWHERE
//	weather_phase         — reason stated NOWHERE, IN ANY DOCUMENT
//
// ⭐ THIS TEST IS THEREFORE A SENTINEL (§82's fourth disposition): it is
// authored RED to hold open WINDOW-CHECKLIST row 14, "A reason, anywhere, for
// FOUR dumped-but-uncompared projections". ⛔ ITS RED IS THE OBLIGATION'S ONLY
// VISIBLE MARKER. Making it green DISCHARGES A LOCKED CHECKLIST ITEM, which is
// a ruling and is not a repair's to make.
//
// ⚠⚠ S96B DID EXACTLY THAT AND IS UNDOING IT. This map was replaced with the
// tool's own §67/§82 exclusion policy (3 named + 3 class suffixes) and the test
// went GREEN. The reasoning looked sound — the tool now states a reason for all
// four — and it was wrong twice over:
//
//  1. ⛔ IT SETTLES A QUESTION THE PLAN FORBIDS W10 FROM SETTLING.
//     `arrival_ref_retraction` matches the `_retraction` class, so the derived
//     policy silently EXCLUDES it. The criterion says of it, in terms: "an
//     UNCOMPARED RAW INPUT to the COMPARED trip_state recompute — a CLASS that
//     DR-W7-9 and DR-W8-8 do not reach … NAMED AS UNRULED … W10 records it as
//     open; W10 must not settle it." ⭐ THE NUMBER IS NOT THE FINDING; THAT KEY
//     IS. A green here erases it.
//
//     ⟦LOCK — S127/D-74, struck 2026-09-04/05; noted here 2026-09-09⟧ ⛔ THIS
//     PARAGRAPH DESCRIBES THE S96B-ERA MECHANISM, NOT TODAY'S, AND READS AS
//     LIVE FACT IF LEFT UNMARKED. S127 (bbff17e, 2026-09-04) struck all three
//     class suffixes and D-74 ratified it (2026-09-05); `ignoredProjectionClasses`
//     is an EMPTY MAP at this tree's tip (integration/witness-2026-09-05 @
//     d6f0db0) — there is no `_retraction` class left for `arrival_ref_retraction`
//     to match. [MEASURED — `go test ./internal/deadline/... -run TestW10 -v`
//     at that tip: the DERIVED compare set (24 members) carries
//     `arrival_ref_retraction`; the sentinel's own unaccounted set (2 members,
//     below) does not. It is COMPARED, not excluded.] ⭐ THE ARCHITECTURAL POINT
//     SURVIVES EVEN THOUGH THE FACT DID NOT: a reinstated class rule could
//     re-absorb this key again without W10 ever settling anything, which is
//     exactly why `w10SentinelExcluded` below stays name-only rather than
//     folded into the general `w10Excluded` reader. ⛔ Do not read "class rule"
//     in this paragraph as describing the present — re-check
//     `ignoredProjectionClasses` at whatever tip you are reading this from.
//
//  2. ⚠ "STATES A REASON" WAS CHECKED AS VOCABULARY, NOT AS PROPERTY (§94's
//     rider, §108). The check was `strings.TrimSpace(why) != ""`. But
//     `weather_phase`'s stated reason is "named in DR-W7-9's precedent list as
//     ABSENT from the landed compare set at W7" — that asserts THAT it is
//     absent, not WHY it should be. Non-empty text is not a reason, and the
//     criterion asks for a reason.
//
// ⭐ SO THE BASE IS RE-POINTED AND THE SENTINEL IS LEFT STANDING. The compare
// set is derived (that half IS a property match — the tool really does compare
// those twelve); the EXCLUDE side stays the criterion's own measured category
// (A). Whether §67/§82's added reasons discharge row 14 is a RULING, and it is
// routed in reports/window-build/S96B-W10-CONTROL.md §8, not taken here.
var w10ExcludeWithStatedReason = map[string]string{
	"alert_ack": "cmd/dumpcompare/main.go's doc comment, by name: " +
		"\"the replicated-but-deadline-owned alert_ack fold is excluded here because " +
		"§13.3's compare set names trips, visits, and reference data + aliases ONLY\"",
}

// w10SentinelExcluded is the criterion's category (A) as a predicate — EXACT
// NAMES ONLY, no class suffixes. ⛔ A suffix rule here would re-absorb
// arrival_ref_retraction, which is the whole reason this is separate from
// w10Excluded.
func w10SentinelExcluded(name string) (string, bool) {
	why, ok := w10ExcludeWithStatedReason[name]
	return why, ok
}

// w10DumpKeys opens a scratch store, runs BOTH registration sites the way
// cmd/sitectl/main.go does, applies any extra projections, and returns the
// dump's top-level key set. THIS IS THE SUBJECT: the dump, not a registry
// literal.
func w10DumpKeys(t *testing.T, extra ...store.Projection) []string {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "w10.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatalf("domain.Register: %v", err)
	}
	if err := Register(st); err != nil {
		t.Fatalf("deadline.Register: %v", err)
	}
	for _, p := range extra {
		if err := st.RegisterProjection(p); err != nil {
			t.Fatalf("RegisterProjection(%s): %v", p.Name, err)
		}
	}
	b, err := st.DumpProjections()
	if err != nil {
		t.Fatalf("DumpProjections: %v", err)
	}
	var dump map[string]json.RawMessage
	if err := json.Unmarshal(b, &dump); err != nil {
		t.Fatalf("dump is not an object: %v", err)
	}
	var keys []string
	for k := range dump {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// w10Unaccounted is W10-10's CERTIFIED rule: DUMP minus (compareSet ∪
// EXCLUDE). Its output is category (C) REGISTERED-NOWHERE.
func w10Unaccounted(dumpKeys, compareSet []string, excluded func(string) (string, bool)) []string {
	in := map[string]bool{}
	for _, k := range compareSet {
		in[k] = true
	}
	var out []string
	for _, k := range dumpKeys {
		if in[k] {
			continue
		}
		if _, ok := excluded(k); ok {
			continue
		}
		out = append(out, k)
	}
	return out
}

// w10ListsOnlyReviewIsSatisfied is the NAMED RIVAL W10-10's CONTROL calls
// out: LISTS → DUMP, i.e. "compare compareSet to a hand-typed list". It
// checks only that every compareSet member is present in the dump — the
// direction the criterion forbids — and it is run, not described.
func w10ListsOnlyReviewIsSatisfied(dumpKeys, compareSet []string) bool {
	present := map[string]bool{}
	for _, k := range dumpKeys {
		present[k] = true
	}
	for _, k := range compareSet {
		if !present[k] {
			return false
		}
	}
	return true
}

func TestW10EveryDumpedProjectionIsComparedOrExcludedWithAStatedReason(t *testing.T) {
	root := w10ModuleRoot(t)
	mainGo := filepath.Join(root, "cmd", "dumpcompare", "main.go")
	compareSet := w10DeriveCompareSet(t, mainGo)
	dumpKeys := w10DumpKeys(t)

	// L-K: state the tree this result measured. A count without its tree is
	// a snapshot presented as a state, and W10-10 was struck once for
	// exactly that.
	t.Logf("MEASURED ON THE WORKING TREE at %s (NOT the pin e9cc5f5): "+
		"dumped=%d compared=%d", root, len(dumpKeys), len(compareSet))
	t.Logf("ANCHORED, quoted from the LOCKED criterion and NOT re-derived: " +
		"registered 17 · compared 12 · unaccounted 5, at e9cc5f5")

	// ⚠⚠ WHAT THIS ASSERTION CAN AND CANNOT CATCH SINCE ⟦LOCK — §67⟧ — stated
	// here rather than left for a reader to discover.
	//
	// The compare set is now `registry − exclusions` and the dump IS the
	// registry, so on a tree where every projection is registered through the
	// two Register calls this set is EMPTY BY CONSTRUCTION. ⛔ A green here is
	// therefore NOT evidence on its own — it is the arithmetic.
	//
	// ⭐ THE LIVE FALSIFIER IS THE ONE THE CRITERION NAMED ALL ALONG: a
	// projection registered at a SECOND SITE reaches the dump WITHOUT reaching
	// the tool's registry, and lands here unaccounted. That is exactly what
	// TestW10TheReconciliationSeesAProjectionRegisteredAtASecondSite plants.
	//
	// ⚠ SO THE PAIR IS NOW LOAD-BEARING IN A WAY IT WAS NOT BEFORE: after §67
	// this test's non-degeneracy rests ENTIRELY on that control. A control that
	// dies at 0.00s before its plant — which is what was happening until this
	// repair — leaves this assertion with no evidence behind it at all.
	unaccounted := w10Unaccounted(dumpKeys, compareSet, w10SentinelExcluded)
	if len(unaccounted) != 0 {
		t.Errorf("W10-10 category (C) REGISTERED-NOWHERE is NON-EMPTY — ⭐ THIS RED IS THE CRITERION'S "+
			"OWN FINDING AND IS A SENTINEL (§82 disposition 4). It holds WINDOW-CHECKLIST row 14 open: "+
			"\"A reason, anywhere, for FOUR dumped-but-uncompared projections\". ⛔ DO NOT GREEN IT BY "+
			"WIDENING THE EXCLUDE SIDE — that discharges a LOCKED checklist item and silently settles "+
			"arrival_ref_retraction, which the criterion names as UNRULED and forbids W10 to settle.\n"+
			"  %d key(s) the dump emits that are in neither list: %v\n"+
			"  TWO CAUSES PRODUCE THIS, AND THEY NEED OPPOSITE RESPONSES:\n"+
			"   (a) ⭐ THE STANDING ONE, and what this sentinel is FOR: the key is deliberately not\n"+
			"       compared and NO REASON IS STATED FOR IT ANYWHERE. The response is a RULING that\n"+
			"       states one — never a widening of the list above.\n"+
			"   (b) A SECOND REGISTRATION SITE: a projection registered outside\n"+
			"       domain.Register/deadline.Register reaches the dump but not the tool's registry,\n"+
			"       so the two posts would never compare it. TestW10TheReconciliationSeesAProjection\n"+
			"       RegisteredAtASecondSite is the control for that arm.\n"+
			"  dump keys (%d): %v\n"+
			"  compare set, DERIVED (registry minus the tool's stated exclusions) (%d): %v\n"+
			"  EXCLUDE-with-a-stated-reason, the criterion's category (A), read at %s (%d): %v",
			len(unaccounted), unaccounted,
			len(dumpKeys), dumpKeys,
			len(compareSet), compareSet,
			mainGo, len(w10ExcludeWithStatedReason), w10ExcludeKeys())
	}
}

func w10ExcludeKeys() []string {
	var out []string
	for k := range w10ExcludeWithStatedReason {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// w10ThrowawayProjection is the plant W10-10's CONTROL names in its own
// words: "register a throwaway projection in a test store and assert the
// reconciliation reds". It is registered on a scratch store in t.TempDir()
// only — nothing under app/ is modified, so no other agent's `go test` can
// see it.
func w10ThrowawayProjection() store.Projection {
	return store.Projection{
		Name:    "w10_throwaway_second_site",
		Table:   "w10_throwaway_second_site",
		OrderBy: "id",
		Schema:  `CREATE TABLE IF NOT EXISTS w10_throwaway_second_site (id TEXT PRIMARY KEY)`,
	}
}

func TestW10TheReconciliationSeesAProjectionRegisteredAtASecondSite(t *testing.T) {
	root := w10ModuleRoot(t)
	mainGo := filepath.Join(root, "cmd", "dumpcompare", "main.go")
	// ⚠ Named excludeNames/excludeClasses, not named/classes: the assertion
	// below already uses `named` for "the throwaway was named in the delta",
	// and shadowing it here would be a rename made by a repair rather than by
	// the assertion's author.
	// ⚠ THE CONTROL USES THE TOOL'S POLICY, NOT THE SENTINEL MAP, AND THE SPLIT
	// IS DELIBERATE. This test asks "can the reconciliation SEE a projection
	// registered at a second site" — a question about the MECHANISM. The
	// criterion test above asks "does every dumped key have a stated reason" — a
	// question about the RECORD, whose answer is the sentinel. Same helper, two
	// different subjects.
	excludeNames, excludeClasses := w10ToolExclusions(t, mainGo)
	excluded := w10Excluded(excludeNames, excludeClasses)
	compareSet := w10DeriveCompareSet(t, mainGo)

	before := w10DumpKeys(t)
	after := w10DumpKeys(t, w10ThrowawayProjection())

	if len(after) != len(before)+1 {
		t.Fatalf("the plant did not reach the dump: %d keys before, %d after — "+
			"the control cannot red for the reason it claims", len(before), len(after))
	}

	// The NAMED RIVAL, RUN. W10-10: "An exact-set-literal test cannot detect
	// a projection registered at a SECOND site — it compares a literal to a
	// literal." Both directions of the rival must be UNMOVED by the plant.
	rivalBefore := w10ListsOnlyReviewIsSatisfied(before, compareSet)
	rivalAfter := w10ListsOnlyReviewIsSatisfied(after, compareSet)
	if rivalBefore != rivalAfter {
		t.Fatalf("DEGENERATE FIXTURE: the LISTS → DUMP rival changed answer (%v → %v) when the "+
			"throwaway was registered, so this fixture does not separate the two rules",
			rivalBefore, rivalAfter)
	}

	// The CERTIFIED rule must red, and must red BY NAME.
	deltaBefore := w10Unaccounted(before, compareSet, excluded)
	deltaAfter := w10Unaccounted(after, compareSet, excluded)
	if len(deltaAfter) != len(deltaBefore)+1 {
		t.Fatalf("the certified DUMP → LISTS rule did not red on the throwaway: "+
			"unaccounted %d before, %d after (%v → %v)",
			len(deltaBefore), len(deltaAfter), deltaBefore, deltaAfter)
	}
	named := false
	for _, k := range deltaAfter {
		if k == w10ThrowawayProjection().Name {
			named = true
		}
	}
	if !named {
		t.Fatalf("the certified rule reported a count change but did not NAME the throwaway: %v", deltaAfter)
	}

	t.Logf("NON-DEGENERACY, pinned: rival(LISTS→DUMP) = %v both before and after the plant · "+
		"certified(DUMP→LISTS) = %d → %d, naming %q",
		rivalBefore, len(deltaBefore), len(deltaAfter), w10ThrowawayProjection().Name)
}

// w10PlantMainGo writes a synthetic cmd/dumpcompare/main.go into a fresh
// t.TempDir() and returns its path. RULE 20: a REAL on-disk file, because the
// reader parses from disk and `go test -overlay` would not reach it. Nothing
// under app/ is written.
func w10PlantMainGo(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(path, []byte("package main\n\n"+body), 0o644); err != nil {
		t.Fatalf("planting %s: %v", path, err)
	}
	return path
}

// TestW10TheInstrumentAnnouncesItselfBrokenRatherThanRedding is the control
// for THIS REPAIR, and it plants the exact defect the repair closes.
//
// ⛔ THE DEFECT WAS NOT THE STALE SYMBOL. It was that the instrument's death
// was indistinguishable from the criterion's failure: both W10-10 tests died at
// t.Fatalf in 0.00s, before any measurement, and the gate would have read
// "W10-10 red" as "a dumped projection is unaccounted for". §75's own shape —
// SAME COLOUR, DIFFERENT FACT — and the one shape a red cannot announce.
//
// ⭐ ARM 1 IS THE HISTORICAL DEFECT ITSELF: a main.go in the superseded shape,
// carrying `var compareSet = []string{…}` and no exclusion policy. The reader
// must fail, and must fail SAYING WHICH SYMBOL IT COULD NOT READ.
func TestW10TheInstrumentAnnouncesItselfBrokenRatherThanRedding(t *testing.T) {
	// ARM 1 — the superseded shape: the literal is present, the policy is not.
	// This is byte-for-byte the situation at 6e14308 that killed both tests.
	superseded := w10PlantMainGo(t, "var compareSet = []string{\n\t\"company\", \"person\",\n}\n")
	if _, _, _, err := w10ReadToolExclusions(superseded); err == nil {
		t.Fatal("CONTROL FAILED (arm 1): a main.go in the SUPERSEDED `var compareSet` shape read " +
			"as healthy — the instrument cannot tell that its subject has moved, which is the whole defect")
	} else if !strings.Contains(err.Error(), "no top-level") || !strings.Contains(err.Error(), "ignoredProjections") {
		// ⚠ THE "no top-level" HALF IS LOAD-BEARING AND WAS ADDED AFTER A MUTANT
		// SURVIVED. Asserting only that the message names `ignoredProjections`
		// is ALSO satisfied by the EMPTY-map guard below, so a build that
		// deleted the missing-symbol check entirely still passed this arm —
		// [PRODUCED 2026-09-03, S96B mutant B2] `--- PASS` with the check gone.
		// The arm now pins the SYMBOL-ABSENT path specifically, which is the one
		// the historical defect took.
		t.Fatalf("CONTROL FAILED (arm 1): the instrument failed but did not report a MISSING SYMBOL "+
			"by name, so the next reader cannot tell an instrument fault from a criterion red: %v", err)
	}

	// ARM 2 — the §82 half-read: the named map present, the CLASS map absent.
	// [MEASURED at the §82 repair] reading only the named map reported "20
	// compared" where the tool compares 12. Half-reading must be an error, not
	// a smaller answer.
	halfRead := w10PlantMainGo(t, "var ignoredProjections = map[string]string{\n\t\"a\": \"reason a\",\n}\n")
	if _, _, _, err := w10ReadToolExclusions(halfRead); err == nil {
		t.Fatal("CONTROL FAILED (arm 2): a main.go with NO `ignoredProjectionClasses` read as healthy — " +
			"the side-table exclusions live there, and a silent half-read is how this test stops " +
			"describing its subject")
	} else if !strings.Contains(err.Error(), "ignoredProjectionClasses") {
		t.Fatalf("CONTROL FAILED (arm 2): the failure did not name `ignoredProjectionClasses`: %v", err)
	}

	// ARM 3 — the EMPTY policy. An empty exclusion map is not "nothing
	// excluded"; on this reader's own terms it means the policy could not be
	// read, and every property downstream would be vacuously true.
	empty := w10PlantMainGo(t, "var ignoredProjections = map[string]string{}\n"+
		"var ignoredProjectionClasses = map[string]string{\"_x\": \"why\"}\n")
	if _, _, _, err := w10ReadToolExclusions(empty); err == nil {
		t.Fatal("CONTROL FAILED (arm 3): an EMPTY `ignoredProjections` read as healthy")
	} else if !strings.Contains(err.Error(), "EMPTY") {
		t.Fatalf("CONTROL FAILED (arm 3): the failure did not say EMPTY: %v", err)
	}

	// ARM 4 — THE NEGATIVE HALF, without which arms 1-3 are satisfied by a
	// reader that fails on everything. A well-formed policy must read back
	// EXACTLY, reasons included, with `+` concatenation followed.
	good := w10PlantMainGo(t, "var ignoredProjections = map[string]string{\n"+
		"\t\"alpha\": \"reason \" + \"alpha\",\n\t\"beta\": \"reason beta\",\n}\n"+
		"var ignoredProjectionClasses = map[string]string{\n\t\"_side\": \"class reason\",\n}\n")
	named, classes, struck, err := w10ReadToolExclusions(good)
	if err != nil {
		t.Fatalf("CONTROL FAILED (arm 4, negative): a well-formed policy did not read: %v", err)
	}
	if len(named) != 2 || named["alpha"] != "reason alpha" || named["beta"] != "reason beta" {
		t.Fatalf("CONTROL FAILED (arm 4): named exclusions read back as %v — the `+` concatenation arm "+
			"matters: a reason built by concatenation that reads as empty is a FALSE 'states no reason' red", named)
	}
	if len(classes) != 1 || classes["_side"] != "class reason" {
		t.Fatalf("CONTROL FAILED (arm 4): class exclusions read back as %v", classes)
	}
	if len(struck) != 0 {
		t.Fatalf("CONTROL FAILED (arm 4): a populated policy with no ⟦was:⟧ lines reported struck entries %v", struck)
	}

	// ARM 5 — the predicate mirrors `w67Ignored`: named first, then suffix.
	excluded := w10Excluded(named, classes)
	if _, ok := excluded("alpha"); !ok {
		t.Fatal("CONTROL FAILED (arm 5): a NAMED exclusion was not excluded")
	}
	if _, ok := excluded("trip_side"); !ok {
		t.Fatal("CONTROL FAILED (arm 5): a CLASS-SUFFIX exclusion was not excluded — the side tables " +
			"would be compared between posts")
	}
	if _, ok := excluded("trip"); ok {
		t.Fatal("CONTROL FAILED (arm 5, negative): a projection matching NEITHER rule was excluded — " +
			"a predicate that excludes everything makes the compare set empty and every red meaningless")
	}

	// ⟦REPAIR — §219(3), 2026-09-04⟧ ARMS 6–9 PRODUCE THE THREE STATES THE
	// REPOINT TELLS APART. Until today arms 6, 7 and 8 were ONE arm to this
	// reader — all "EMPTY", all INSTRUMENT BROKEN — and the live tree sits in
	// arm 7's state. ⛔ Arm 6 is the one that proves the guard was narrowed and
	// not deleted: a repair that "fixed" the four reds by removing the check
	// passes arms 1–5 and 7 and fails here.

	// ARM 6 — THE EMPTY CLASS MAP WITH NO STRIKE RECORD. Byte-for-byte what a
	// reader looking at the wrong declaration, or a class rule deleted without
	// being preserved, produces. Still INSTRUMENT BROKEN, and it must say so
	// by symbol, by EMPTY, and by the record it could not find.
	bare := w10PlantMainGo(t, "var ignoredProjections = map[string]string{\"a\": \"reason a\"}\n"+
		"var ignoredProjectionClasses = map[string]string{}\n")
	if _, _, _, err := w10ReadToolExclusions(bare); err == nil {
		t.Fatal("CONTROL FAILED (arm 6): an EMPTY `ignoredProjectionClasses` with NO strike record read as healthy — " +
			"the §219 repoint has removed the guard instead of narrowing it")
	} else if !strings.Contains(err.Error(), "ignoredProjectionClasses") || !strings.Contains(err.Error(), "EMPTY") ||
		!strings.Contains(err.Error(), "strike") {
		t.Fatalf("CONTROL FAILED (arm 6): the failure must name the symbol, say EMPTY and say the strike record "+
			"is missing: %v", err)
	}

	// ARM 7 — THE SAME EMPTY MAP WITH ITS STRUCK ENTRY PRESERVED ABOVE IT, the
	// way S127 left the real one (§78's pointer rule). The ONE shape that reads
	// as a policy: no class rule in force, and the reader can say which rule
	// was struck. And the struck rule is really GONE from the predicate — a
	// name carrying the struck suffix is compared, not excluded.
	struckSrc := "var ignoredProjections = map[string]string{\"a\": \"reason a\"}\n\n" +
		"// the class rule, struck on purpose\n" +
		`// ⟦was: "\t\"_side\": \"class reason\","⟧` + "\n" +
		"var ignoredProjectionClasses = map[string]string{}\n"
	sNamed, sClasses, sStruck, err := w10ReadToolExclusions(w10PlantMainGo(t, struckSrc))
	if err != nil {
		t.Fatalf("CONTROL FAILED (arm 7): an empty class map WITH its struck entry preserved did not read as a "+
			"policy: %v", err)
	}
	if len(sClasses) != 0 || len(sStruck) != 1 || sStruck[0] != "_side" {
		t.Fatalf("CONTROL FAILED (arm 7): read classes=%v struck=%v, want no class in force and exactly "+
			"[_side] struck", sClasses, sStruck)
	}
	if _, ok := w10Excluded(sNamed, sClasses)("trip_side"); ok {
		t.Fatal("CONTROL FAILED (arm 7): a name carrying a STRUCK class suffix was still excluded — the strike " +
			"record leaked back into the predicate")
	}

	// ARM 8 — UNREAD, the case the old guard could not tell from EMPTY. A map
	// whose element is not string → string (a value that is an identifier, as
	// a restructuring to a rule struct would leave it) used to read as an
	// EMPTY map and die with arm 6's words — or, had arm 6's guard simply been
	// deleted, would have read as "no class rule in force" and SILENTLY
	// compared every side table. It must fail saying it could not READ, and
	// must not say EMPTY. The same for the named map, which the old reader
	// silently read as smaller than it is.
	unread := w10PlantMainGo(t, "var reason = \"r\"\n"+
		"var ignoredProjections = map[string]string{\"a\": \"reason a\"}\n"+
		"var ignoredProjectionClasses = map[string]string{\"_x\": reason}\n")
	if _, _, _, err := w10ReadToolExclusions(unread); err == nil {
		t.Fatal("CONTROL FAILED (arm 8): a class map whose element this reader cannot render read as healthy — " +
			"a restructured policy would be compared as if it were empty")
	} else if !strings.Contains(err.Error(), "ignoredProjectionClasses") || !strings.Contains(err.Error(), "could NOT read") {
		t.Fatalf("CONTROL FAILED (arm 8): the failure must name the symbol and say it could NOT read the "+
			"element: %v", err)
	} else if strings.Contains(err.Error(), "EMPTY") {
		t.Fatalf("CONTROL FAILED (arm 8): an UNREAD map was reported as EMPTY — the two states have collapsed "+
			"again: %v", err)
	}
	unreadNamed := w10PlantMainGo(t, "var reason = \"r\"\n"+
		"var ignoredProjections = map[string]string{\"a\": \"reason a\", \"b\": reason}\n"+
		"var ignoredProjectionClasses = map[string]string{\"_x\": \"why\"}\n")
	if _, _, _, err := w10ReadToolExclusions(unreadNamed); err == nil ||
		!strings.Contains(err.Error(), "`ignoredProjections`") || !strings.Contains(err.Error(), "could NOT read") {
		t.Fatalf("CONTROL FAILED (arm 8, named): a named map with one unreadable element must fail saying so, "+
			"not read as one entry shorter; got: %v", err)
	}

	// ARM 9 — THE STRIKE RECORD IS A PROPERTY, NOT A WORD. A ⟦was:⟧ line that
	// preserves something OTHER than a map entry (here, a comment line) is not
	// evidence that an entry was struck, so an empty map above which only that
	// line stands is still INSTRUMENT BROKEN. A reader satisfied by the marker
	// alone would pass this plant.
	wordOnly := w10PlantMainGo(t, "var ignoredProjections = map[string]string{\"a\": \"reason a\"}\n\n"+
		`// ⟦was: "\t// the side tables are excluded by class"⟧`+"\n"+
		"var ignoredProjectionClasses = map[string]string{}\n")
	if _, _, wStruck, err := w10ReadToolExclusions(wordOnly); err == nil {
		t.Fatalf("CONTROL FAILED (arm 9): a ⟦was:⟧ line preserving a COMMENT, not an entry, was accepted as a "+
			"strike record (struck=%v)", wStruck)
	}
}

// ---------------------------------------------------------------------------
// ⟦REPAIR-QUEUE row B2 — under §219(3), 2026-09-04⟧ THE SENTINEL'S OWN
// MAGNITUDE, ASSERTED.
// ---------------------------------------------------------------------------

// w10RuledMagnitude is the W10-10 sentinel's magnitude as RULED at §219(3)
// (reports/window-build/S127-CLASSFIX.md §3): the keys the dump emits that are
// neither compared nor in the criterion's category (A). ⚠ Pinned BY NAME,
// never as a count — see the test below for what may move it.
// ⟦was: {"alert_ack_retraction", "weather_phase"} — RE-SET WITH THE RULING IN
// HAND, as the test below requires: James, 2026-10-04, removed weather phases
// ("go ahead and delete the phase conditions"), so the weather_phase
// projection no longer exists and no longer dumps. The magnitude is ONE.⟧
var w10RuledMagnitude = []string{"alert_ack_retraction"}

// w10MagnitudeDrift is the sentinel's reported set against the ruled one, both
// ways: MISSING is ruled-but-not-reported, EXTRA is reported-but-not-ruled.
func w10MagnitudeDrift(reported, ruled []string) (missing, extra []string) {
	have := map[string]bool{}
	for _, k := range reported {
		have[k] = true
	}
	want := map[string]bool{}
	for _, k := range ruled {
		want[k] = true
		if !have[k] {
			missing = append(missing, k)
		}
	}
	for _, k := range reported {
		if !want[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return missing, extra
}

func w10SortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// w10RenderPolicy writes an exclusion policy back out in the source shape the
// reader parses — both maps, and an empty class map with its strike record
// preserved exactly as S127 left the real one — so a control can plant a
// ONE-NAME departure from the LIVE policy rather than a hand-typed copy of it.
func w10RenderPolicy(named, classes map[string]string, struck []string) string {
	var b strings.Builder
	b.WriteString("var ignoredProjections = map[string]string{\n")
	for _, k := range w10SortedKeys(named) {
		fmt.Fprintf(&b, "\t%q: %q,\n", k, named[k])
	}
	b.WriteString("}\n\n")
	if len(classes) == 0 {
		for _, s := range struck {
			fmt.Fprintf(&b, "// ⟦was: %q⟧\n", "\t"+strconv.Quote(s)+": \"struck\",")
		}
	}
	b.WriteString("var ignoredProjectionClasses = map[string]string{\n")
	for _, k := range w10SortedKeys(classes) {
		fmt.Fprintf(&b, "\t%q: %q,\n", k, classes[k])
	}
	b.WriteString("}\n")
	return b.String()
}

// TestW10TheSentinelStatesItsOwnMagnitudeAndItIsOne — repair-queue row B2,
// ⟦was: "TestW10TheSentinelStatesItsOwnMagnitudeAndItIsTwo" — re-pinned with
// James's 2026-10-04 removal of weather phases, which retired weather_phase⟧
// under §219(3) (2026-09-04).
//
// ⛔ THE HARNESS NEVER ASSERTED A SENTINEL'S MAGNITUDE. That is how 4, 5, 9 and
// 10 could all circulate as "the" W10 number until §205 reconciled them as
// four different real properties, and how S127's TWO reached the record as a
// by-hand derivation from the binary's stdout — labelled, correctly, "my
// derivation, not the test's verdict". The sentinel above prints its magnitude
// inside a red; nothing checked that the number it prints is the number the
// record carries, so the next drift would have been re-derived by hand again.
//
// ⭐ WHAT IS PINNED IS THE SENTINEL'S OWN VERDICT, BY NAME. This test runs the
// sentinel's own helpers — the same registry dump, the same derived compare
// set, the same category-(A) predicate — so the set it asserts IS what
// TestW10EveryDumpedProjectionIsComparedOrExcludedWithAStatedReason reports,
// not a parallel derivation. The count follows from the names: §219(3) RULED
// the magnitude TWO — `alert_ack_retraction` and `weather_phase`, the two keys
// the criterion always named as unruled/unreasoned; James's 2026-10-04
// ruling removing weather phases took `weather_phase` out of the dump, so
// the magnitude is now ONE.
//
// ⚠ THIS FILE'S HEADER SAYS "NOTHING HERE ASSERTS A COUNT", AND THIS DOES —
// the difference is that this one is RULED and is asserted BY NAME, so it
// moves only when a NAME moves. A projection registered tomorrow and compared
// by default does not move it. What moves it is a RULING (a reason stated for
// one of the two, retiring it from the sentinel's set — §205's "weather_phase
// ruled INTO compareSet" would do exactly that the day it lands in main.go) or
// a DRIFT (a new exclusion nobody reasoned, or a restructuring that changes
// what the reader sees). Both are the right time to read the red; neither is
// a reason to delete this pin so the number can drift by hand again.
//
// ⚠ IT DOES NOT ASSERT THAT THE SENTINEL IS RED, and it does not green it:
// R01 stays red on its own terms (DECLARED-REDS.md).
func TestW10TheSentinelStatesItsOwnMagnitudeAndItIsOne(t *testing.T) {
	root := w10ModuleRoot(t)
	mainGo := filepath.Join(root, "cmd", "dumpcompare", "main.go")
	dumpKeys := w10DumpKeys(t)

	// --- THE LIVE MAGNITUDE, the sentinel's own derivation, taken first and
	// asserted last: the controls between are stated RELATIVE TO IT, so they
	// hold whether or not the live tree still matches the ruling — a genuine
	// drift is then reported below AS A DRIFT, never as a failed control.
	// [PRODUCED, 2026-09-04] the first draft pinned the controls to the RULED
	// set instead, and a real one-name drift planted into main.go tripped
	// "CONTROL FAILED (LOW)" before the drift line was ever printed.
	compareSet := w10DeriveCompareSet(t, mainGo)
	reported := w10Unaccounted(dumpKeys, compareSet, w10SentinelExcluded)

	// --- CONTROL, PRODUCED BEFORE THE ASSERTION: the arithmetic must MOVE
	// when the policy does, and must name the mover. Two planted policies,
	// each a ONE-NAME departure from the LIVE one (rendered from it, not
	// hand-typed), and each must differ from the LIVE magnitude by exactly
	// that name. Without this the assertion could be satisfied by a
	// comparison that never looks at its inputs — §219's own cautionary tale:
	// two EMPTY dumps agree.
	liveNamed, liveClasses, liveStruck, err := w10ReadToolExclusions(mainGo)
	if err != nil {
		t.Fatalf("W10-10 INSTRUMENT BROKEN — this is NOT a criterion failure: %v", err)
	}
	magnitudeOf := func(policy string) []string {
		set := w10DeriveCompareSet(t, w10PlantMainGo(t, policy))
		return w10Unaccounted(dumpKeys, set, w10SentinelExcluded)
	}

	// LOW: one key the sentinel reports today is COMPARED instead of
	// excluded — it must leave the set, and nothing else may move.
	mover := ""
	for _, k := range append(append([]string{}, reported...), w10RuledMagnitude...) {
		if _, ok := liveNamed[k]; ok {
			mover = k
			break
		}
	}
	if mover == "" {
		t.Fatalf("control fixture assumption broken: neither a reported key %v nor a ruled key %v is a NAMED "+
			"exclusion (%v) — re-derive this control before trusting it", reported, w10RuledMagnitude, w10SortedKeys(liveNamed))
	}
	low := map[string]string{}
	for k, v := range liveNamed {
		if k != mover {
			low[k] = v
		}
	}
	if missing, extra := w10MagnitudeDrift(magnitudeOf(w10RenderPolicy(low, liveClasses, liveStruck)), reported); len(missing) != 1 || missing[0] != mover || len(extra) != 0 {
		t.Fatalf("CONTROL FAILED (LOW): a policy that COMPARES %q must remove exactly it from the sentinel's set "+
			"(%v); got missing=%v extra=%v", mover, reported, missing, extra)
	}

	// HIGH: `vehicle` — a compared projection — excluded by name with no
	// category-(A) reason. It must enter the set, and nothing else may move.
	if _, ok := liveNamed["vehicle"]; ok {
		t.Fatal("control fixture assumption broken: `vehicle` is already a named exclusion")
	}
	high := map[string]string{"vehicle": "planted by the control: a compared projection excluded with no category-(A) reason"}
	for k, v := range liveNamed {
		high[k] = v
	}
	if missing, extra := w10MagnitudeDrift(magnitudeOf(w10RenderPolicy(high, liveClasses, liveStruck)), reported); len(extra) != 1 || extra[0] != "vehicle" || len(missing) != 0 {
		t.Fatalf("CONTROL FAILED (HIGH): a policy that EXCLUDES `vehicle` must add exactly it to the sentinel's set "+
			"(%v); got missing=%v extra=%v", reported, missing, extra)
	}

	// --- THE ASSERTION: the live magnitude against the RULED one, by name.
	missing, extra := w10MagnitudeDrift(reported, w10RuledMagnitude)
	if len(missing) != 0 || len(extra) != 0 {
		t.Errorf("W10-10 SENTINEL MAGNITUDE HAS DRIFTED from the RULED ONE (§219(3), 2026-09-04; weather_phase retired 2026-10-04): "+
			"ruled-but-not-reported %v · reported-but-not-ruled %v\n"+
			"  ruled (S127-CLASSFIX.md §3): %v\n"+
			"  the sentinel reports (%d): %v\n"+
			"  dumped %d · compared %d · category (A) %v\n"+
			"  ⚠ READ THIS BEFORE TOUCHING THE PIN. A key ruled-but-not-reported means a reason was stated for it or it\n"+
			"  entered the compare set — that is a RULING, and the pin is re-set with the ruling in hand. A key\n"+
			"  reported-but-not-ruled means the dump carries something neither compared nor reasoned — the sentinel's\n"+
			"  own finding, made loud here instead of being re-derived by hand from the binary's stdout.",
			missing, extra, w10RuledMagnitude, len(reported), reported, len(dumpKeys), len(compareSet), w10ExcludeKeys())
	}
	t.Logf("W10-10 SENTINEL MAGNITUDE [MEASURED on the working tree at %s]: dumped=%d compared=%d unaccounted=%d %v "+
		"— RULED ONE (§219(3) TWO, less weather_phase, 2026-10-04)", root, len(dumpKeys), len(compareSet), len(reported), reported)
}
