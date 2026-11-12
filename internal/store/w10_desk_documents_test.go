// w10_desk_documents_test.go — W10-11 and W10-13: the criteria whose subject
// is A DOCUMENT, mechanised as commands over that document.
//
// Criteria source: design/WINDOW-BUILD-PLAN-v1.md §3, `### W10 — Window
// integration and rehearsal`, LOCKED 2026-09-03 (reports/W10-FOLD-RECORD.md,
// gate P-4). Written from the LOCKED CRITERION TEXT ONLY.
//
// Criteria carried here:
//
//   - W10-11(a) — the FIVE desk-document commands. "B-4 is not signed while
//     any of these returns its today value."
//   - W10-11(b) — the SHEET half: rows 1, 2, 3, 5 and 6 of the seven. Rows 4
//     and 7 are NOT written here and the reasons are stated below.
//   - W10-13 — G8's work cell carries the tripwire AND the repair rule, by
//     four greps on a row extracted BY LABEL and never by line.
//
// ⚠⚠ THESE CHECKS ARE RED BY DESIGN, AND THE RED IS THE CRITERION'S STATE.
// W10-11(a)'s five and W10-11(b)'s rows 1, 2, 3 and 5 all return their
// "today value" on this tree, which is precisely the condition under which
// B-4 MUST NOT BE SIGNED. A green here would mean the sweep landed. Each red
// prints the command, the value found, and the discharge condition, so the
// failure output is usable as B-4's worklist.
//
// ⚠⚠ THE THREE ANCHORED NUMBERS ARE NOT RE-TAKEN ANYWHERE IN THIS FILE.
// W10-0's "49 uncommitted paths at 01:32Z", W10-10's "17 / 12 / 5 at
// e9cc5f5" and W10-0b's BEFORE row are the criteria's, and a fresher
// instance of any of them is the defect, not the fix. Where this file needs
// a historical value it embeds it AS A LITERAL (see w10G8RowBefore) rather
// than re-deriving it from a tree that has moved.
//
// ⚠ WHAT IS DELIBERATELY NOT WRITTEN HERE.
//   - W10-11(b) ROW 4 is three clauses ("build.ps1 builds dumpcompare, the
//     deploy places it somewhere named on the sheet, and the sheet names
//     that machine"). Only clause ONE is mechanical; the other two need a
//     judgement about what counts as naming a machine. The test below is
//     named for the ONE clause it proves, because W10-11's own split exists
//     to stop half a row reading as the whole row.
//   - W10-11(b) ROW 7 is "NOT ESTABLISHED — this pass did not walk all of
//     them", roughly twenty sheet-to-code citations. NOTHING here turns it
//     green. Writing a check over the subset that happens to parse would
//     convert NOT ESTABLISHED into a pass, which is the one thing the
//     criterion's disposition forbids.
//   - Everything about the REHEARSAL. See reports/window-build/W10-tests.md
//     §2 for the split between what a Go test can see and what it cannot.
//
// ⚠ RULE 20 AND WHERE THE FILES LIVE. Every subject here is a markdown file
// OUTSIDE the Go module (SiteControl/design/), read from disk. `go test
// -overlay` therefore does not reach it, so every control plants into a REAL
// on-disk copy under t.TempDir(). Nothing under app/ or design/ is written.
//
// ⚠ IF design/ IS ABSENT THESE TESTS FAIL, THEY DO NOT SKIP. A skip would
// report ok while never having run, which is the shape this window exists to
// refuse.
//
// ⭐ THE LIVE-DOCUMENT CLASS — §218, W5-POSTLOCK-AMENDMENTS.md, ruling on
// queue row D (recommendation option (e), S124-TOOLING.md §D4). Row D's
// original success test — "the same commit produces the same result on a
// machine where design/ differs" — is UNSATISFIABLE for these checks: their
// subject is deliberately TODAY's document, so anything that makes the
// result pinnable from a past commit FREEZES THE SUBJECT, and a frozen
// subject makes the check false-GREEN. The ruling resolves that conflict by
// declaring, rather than pretending these are ordinary reproducible tests:
//
//   - the dependency is EXPLICIT AND STEERABLE: w10DesignDir resolves
//     SITECONTROL_DESIGN_DIR first, falling back to today's sibling design/
//     only when the env var is unset;
//   - the hard failure is KEPT in both cases — env var unset with no sibling,
//     or env var set to a path that does not resolve. NOT a skip, per rule 20
//     above: "a document check that reports ok without reading the document
//     is the failure mode W10 refuses";
//   - design/ is NOT vendored into this module, NOT skipped when absent, and
//     NOT pinned by a content hash — all three were costed and rejected
//     (S124-TOOLING.md §D3: vendoring and hashing both move the failure to
//     the false-green side; hashing also rubber-stamps a document edited
//     mid-window);
//   - these EIGHT tests are the class —
//     TestW10B4IsNotSignableWhileTheFiveDeskDocumentCommandsReturnTheirTodayValues,
//     TestW10TheSheetCarriesTheTwoNewProbesTheRejectionLimitAndThePhaseLine,
//     TestW10TheSheetDoesNotPreAuthoriseIgnoringAStaleToolsOwnDiscriminator,
//     TestW10NoLiveChecklistLinePointerSurvivesInThePlan,
//     TestW10TheLivePointerCheckCanRed,
//     TestW10G8sWorkCellCarriesTheRepairRuleAndTheTripwire,
//     TestW10TheG8FalsifierRepliesDifferentlyToTheBeforeAndAfterRows,
//     TestW10TheG8TripwireGrepCanGoGreenOnAPlantedRow
//     — every call site of w10DesignDir(t) in this file, no more and no
//     fewer. THREE of the eight (rows 1, 2 and G8's tripwire — R13, R14, R16
//     in DECLARED-REDS.md) are ALSO separately declared red there; that is a
//     different axis (today's document is unfinished) and is not touched by
//     this note or by this row's change;
//     ⟦LOCK — W10-SHEET-PROBES amendment, 2026-09-06, the plan owner
//     (reports/witness/W10-SHEET-PROBES.md): the class gains a NINTH member,
//     TestW10TheSheetCarriesTheWindowContentProbesInSection3, in its own file
//     w10_sheet_probes_test.go — W10-3(c) as amended, for the two
//     WINDOW-CONTENT probes (e) and (f). "In this file, no more and no fewer"
//     still holds for THIS file; the class is now nine members across two
//     files, and the operative half below binds the ninth exactly as it
//     binds the eight. ⟦was: eight members, one file⟧⟧
//   - ⚠️ THE OPERATIVE HALF: these eight are EXCLUDED FROM ANY PASS/FAIL
//     COUNT. Do not fold their result into a suite total, a "definitive N"
//     regression baseline, or a gate green/red tally — counted, their red is
//     indistinguishable from a real regression (this is exactly how a
//     worktree lacking the sibling produced five false reds, §193/§212's D2)
//     and their green is not a property of the code. Uncounted, their output
//     is what they actually are: a live worklist, read on its own, always
//     naming the design/ directory (or SITECONTROL_DESIGN_DIR) it was
//     measured against. A citation of one of these tests' results must name
//     that directory; a bare PASS/FAIL count with no directory named is not
//     a citation of this class, it is the defect the ruling exists to stop.
package store_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// w10DesignDir resolves SiteControl/design. The subject is OUTSIDE the module and
// OUTSIDE its git repository (SiteControl/ and SiteControl/app/ are two separate
// git repositories — the outer .gitignore excludes app/), and no commit here can
// pin it — so name it explicitly rather than derive it unconditionally from where
// the test happens to be sitting. §218 (W5-POSTLOCK-AMENDMENTS.md) rules this
// steerable-but-still-hard-failing shape; see the LIVE-DOCUMENT CLASS note atop
// this file.
func w10DesignDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("SITECONTROL_DESIGN_DIR")
	if dir == "" {
		root := w10DcModuleRoot(t) // app/
		dir = filepath.Join(filepath.Dir(root), "design")
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		t.Fatalf("PRECONDITION UNMET — design/ not found at %s (%v). This is NOT a criterion "+
			"result: the check never ran. Set SITECONTROL_DESIGN_DIR to the canonical tree and "+
			"re-run before reading any red in this file as a finding. These checks FAIL rather "+
			"than skip: a document check that reports ok without reading the document is the "+
			"failure mode W10 refuses.", dir, err)
	}
	return dir
}

func w10ReadDoc(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	if len(b) == 0 {
		t.Fatalf("%s is EMPTY — every count over it would be 0 for the wrong reason", name)
	}
	return string(b)
}

// w10CountLines is `grep -c`: the number of LINES containing needle, which is
// what the criteria's commands measure. Not the number of occurrences.
func w10CountLines(text, needle string) int {
	n := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, needle) {
			n++
		}
	}
	return n
}

// w10LinesReadingAsGenuineAssertion is the proximity/context test rows 3, 4,
// 5a and 5b apply to every line a bare `grep -c` matched, before counting it
// toward the row's discharge. A raw substring hit tells you the TOKEN is
// present; it does not tell you the DOCUMENT ASSERTS the fact the token
// names — a distinction this file already draws twice: row 1 above requires
// a recording_gap-carrying line to also read as "a tombstone citing D-35",
// not merely to contain the string "D-35" somewhere in the file, and
// W10-11(b) row 6 (`design/WINDOW-BUILD-PLAN-v1.md` — "is it a LIVE
// citation, or is it inside a supersession record?") draws the identical
// line between a document ASSERTING something and a document merely
// RECORDING that it once said something else. This helper generalises that
// same distinction to rows whose stated discharge is a bare "B-4's own
// stated discharge" — the row's own text names ">=1" as the bar, and a line
// that only APPEARS to clear it while reading as a disconnected fragment or
// a correction record has not actually cleared it.
//
// Excluded, because they RECORD rather than ASSERT:
//   - a struck-through span (`~~...~~`) or a line inside this corpus's own
//     correction brackets (`⟦was:`, `⟦LOCK`) or dated strike convention
//     (`Struck `) — exactly W10-11(b) row 6's "supersession record" carve-out;
//   - a dangling fragment: real prose and real Markdown bullets in this
//     corpus start capitalised (or with a list marker) and end on sentence
//     punctuation. A bare append with a literal ellipsis on either side
//     (`"... never renders a superseded value ..."`) is neither — it is a
//     clause with nothing attached to it, which is exactly the shape the
//     false-PASS bait used (reports/witness/DOCS-LIVE-READ-HAZARD.md §2b).
//
// A line that is a genuine heading, list item, or complete sentence still
// counts, so a real fix — prose actually integrated into the document —
// still discharges the row; see proof 3 in reports/witness/B4-GREP-PROXIMITY.md.
func w10LinesReadingAsGenuineAssertion(text, needle string) (genuine, bare []string) {
	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, needle) {
			continue
		}
		t := strings.TrimSpace(line)
		switch {
		case strings.Contains(t, "~~"),
			strings.Contains(t, "⟦was:"),
			strings.Contains(t, "⟦LOCK"),
			strings.HasPrefix(t, "Struck "):
			// A correction record, not an assertion.
			bare = append(bare, line)
		case strings.Contains(t, "..."):
			// A dangling, disconnected fragment — the bait's own shape.
			bare = append(bare, line)
		case strings.HasPrefix(t, "#"), strings.HasPrefix(t, "-"), strings.HasPrefix(t, "*"),
			strings.HasSuffix(t, "."), strings.HasSuffix(t, "!"), strings.HasSuffix(t, "?"),
			strings.HasSuffix(t, `."`), strings.HasSuffix(t, `.**`), strings.HasSuffix(t, `**.`):
			// A heading, a list item, or a complete sentence: genuine.
			genuine = append(genuine, line)
		default:
			bare = append(bare, line)
		}
	}
	return genuine, bare
}

// ---------------------------------------------------------------------------
// W10-11(a) — the five desk-document commands.
// ---------------------------------------------------------------------------

type w10DocCheck struct {
	Row       string
	Doc       string
	Command   string
	TodayIs   string // the criterion's recorded today value, quoted
	Discharge string
	// Discharged reports whether the row has been discharged. It is given
	// the document text.
	Discharged func(text string) (ok bool, got string)
}

func w10Rows11a() []w10DocCheck {
	return []w10DocCheck{
		{
			Row: "W10-11(a) row 1", Doc: "schema-10-batch-v1.md",
			Command:   "grep -c recording_gap design/schema-10-batch-v1.md",
			TodayIs:   "1",
			Discharge: "0, or the line reads as a tombstone citing D-35",
			Discharged: func(text string) (bool, string) {
				var carriers []string
				for _, line := range strings.Split(text, "\n") {
					if strings.Contains(line, "recording_gap") {
						carriers = append(carriers, line)
					}
				}
				if len(carriers) == 0 {
					return true, "0 lines"
				}
				// The alternative discharge the criterion states: a tombstone
				// citing D-35. Every carrying line must cite it, or the
				// bullet is still live.
				for _, line := range carriers {
					if !strings.Contains(line, "D-35") {
						return false, fmt.Sprintf("%d line(s), and at least one cites no D-35 tombstone: %q",
							len(carriers), strings.TrimSpace(line))
					}
				}
				return true, fmt.Sprintf("%d line(s), every one a D-35 tombstone", len(carriers))
			},
		},
		{
			Row: "W10-11(a) row 2", Doc: "schema-10-batch-v1.md",
			Command:   "grep -n 'once a vehicle retire lever exists' design/schema-10-batch-v1.md",
			TodayIs:   ":2005",
			Discharge: "no hit; the probe vehicle is noted by number unconditionally (D-65)",
			Discharged: func(text string) (bool, string) {
				n := w10CountLines(text, "once a vehicle retire lever exists")
				return n == 0, fmt.Sprintf("%d line(s)", n)
			},
		},
		{
			Row: "W10-11(a) row 3", Doc: "PLAN-SEC-D-analytics-contacts.md",
			Command:   `grep -c "never renders a superseded value" design/PLAN-SEC-D-analytics-contacts.md`,
			TodayIs:   "0",
			Discharge: ">=1 line reading as a genuine assertion, not a bare/disconnected mention — B-4's own stated discharge",
			Discharged: func(text string) (bool, string) {
				genuine, bare := w10LinesReadingAsGenuineAssertion(text, "never renders a superseded value")
				if len(genuine) >= 1 {
					return true, fmt.Sprintf("%d genuine line(s) (%d bare mention(s) also present)", len(genuine), len(bare))
				}
				return false, fmt.Sprintf("%d line(s) total, 0 genuine (%d bare/disconnected mention(s))", len(genuine)+len(bare), len(bare))
			},
		},
		{
			Row: "W10-11(a) row 4", Doc: "schema-10-batch-v1.md",
			Command:   `grep -c "supersed" design/schema-10-batch-v1.md`,
			TodayIs:   "0",
			Discharge: ">=1 line reading as a genuine assertion, not a bare/disconnected mention — B-4's own stated discharge",
			Discharged: func(text string) (bool, string) {
				genuine, bare := w10LinesReadingAsGenuineAssertion(text, "supersed")
				if len(genuine) >= 1 {
					return true, fmt.Sprintf("%d genuine line(s) (%d bare mention(s) also present)", len(genuine), len(bare))
				}
				return false, fmt.Sprintf("%d line(s) total, 0 genuine (%d bare/disconnected mention(s))", len(genuine)+len(bare), len(bare))
			},
		},
		{
			Row: "W10-11(a) row 5a", Doc: "usb-provisioning-v1.md",
			Command:   "grep -c 'D-55' design/usb-provisioning-v1.md",
			TodayIs:   "0",
			Discharge: ">=1 line reading as a genuine assertion, not a bare/disconnected mention",
			Discharged: func(text string) (bool, string) {
				genuine, bare := w10LinesReadingAsGenuineAssertion(text, "D-55")
				if len(genuine) >= 1 {
					return true, fmt.Sprintf("%d genuine line(s) (%d bare mention(s) also present)", len(genuine), len(bare))
				}
				return false, fmt.Sprintf("%d line(s) total, 0 genuine (%d bare/disconnected mention(s))", len(genuine)+len(bare), len(bare))
			},
		},
		{
			Row: "W10-11(a) row 5b", Doc: "schema-10-batch-v1.md",
			Command:   "grep -c 'D-55' design/schema-10-batch-v1.md",
			TodayIs:   "0",
			Discharge: ">=1 line reading as a genuine assertion, not a bare/disconnected mention",
			Discharged: func(text string) (bool, string) {
				genuine, bare := w10LinesReadingAsGenuineAssertion(text, "D-55")
				if len(genuine) >= 1 {
					return true, fmt.Sprintf("%d genuine line(s) (%d bare mention(s) also present)", len(genuine), len(bare))
				}
				return false, fmt.Sprintf("%d line(s) total, 0 genuine (%d bare/disconnected mention(s))", len(genuine)+len(bare), len(bare))
			},
		},
	}
}

func TestW10B4IsNotSignableWhileTheFiveDeskDocumentCommandsReturnTheirTodayValues(t *testing.T) {
	dir := w10DesignDir(t)
	cache := map[string]string{}
	doc := func(name string) string {
		if _, ok := cache[name]; !ok {
			cache[name] = w10ReadDoc(t, dir, name)
		}
		return cache[name]
	}

	rows := w10Rows11a()
	if len(rows) != 6 {
		t.Fatalf("W10-11(a) is five commands over six (document, command) pairs — row 5 names two "+
			"documents. Got %d pairs; the enumeration has drifted from the criterion.", len(rows))
	}
	outstanding := 0
	for _, r := range rows {
		ok, got := r.Discharged(doc(r.Doc))
		if !ok {
			outstanding++
			t.Errorf("%s NOT DISCHARGED — B-4 must not be signed.\n"+
				"  command:   %s\n  criterion's today value: %s\n  measured now: %s\n  discharged when: %s",
				r.Row, r.Command, r.TodayIs, got, r.Discharge)
		} else {
			t.Logf("%s DISCHARGED (%s)", r.Row, got)
		}
	}
	t.Logf("W10-11(a): %d of %d document commands still at their today value, measured at %s",
		outstanding, len(rows), dir)
}

// ---------------------------------------------------------------------------
// W10-11(b) rows 1, 2, 5 — the sheet half.
// ---------------------------------------------------------------------------

func TestW10TheSheetCarriesTheTwoNewProbesTheRejectionLimitAndThePhaseLine(t *testing.T) {
	dir := w10DesignDir(t)
	sheet := w10ReadDoc(t, dir, "WINDOW-CHECKLIST-v1.md")

	// Row 1 — W10-3(c)'s two probes folded into §3. The criterion's command
	// is one alternation; the discharge is ">=1 EACH", so the two are
	// counted separately here or one probe could cover for the other.
	if n := w10CountLines(sheet, "time_source"); n < 1 {
		t.Errorf("W10-11(b) row 1 NOT DISCHARGED: `time_source` appears on %d line(s) of "+
			"WINDOW-CHECKLIST-v1.md, want >=1 — W10-3(a)'s entered-time probe is not on the "+
			"sheet the operator will hold, so the rehearsal would rehearse a document nobody holds", n)
	}
	if n := w10CountLines(sheet, "occurred_at_correct"); n < 1 {
		t.Errorf("W10-11(b) row 1 NOT DISCHARGED: `occurred_at_correct` appears on %d line(s), "+
			"want >=1 — W10-3(b)'s correction probe is not on the sheet", n)
	}

	// Row 2 — DR-W10-3's headline finding reaches the operator's document.
	if n := w10CountLines(sheet, "cannot detect a missed bump"); n < 1 {
		t.Errorf("W10-11(b) row 2 NOT DISCHARGED: `cannot detect a missed bump` appears on %d "+
			"line(s), want >=1 — §2's rejected-records line does not state what it CANNOT detect", n)
	}

	// Row 5 — the weather-board phase line. The criterion pairs the sheet's
	// count with the program's free numeric input; the program half is
	// asserted so the sheet half cannot be read as a program fact.
	sheetPhase := w10CountLines(sheet, "phase")
	statusHTML := w10ReadStatusTemplate(t)
	programPhase := w10CountLines(statusHTML, `name="phase"`)
	if programPhase < 1 {
		t.Errorf("W10-11(b) row 5's PROGRAM half has changed: `name=\"phase\"` appears %d time(s) "+
			"in internal/web/templates/status.html, and the criterion measured a FREE NUMERIC "+
			"input that accepts a 1. Re-read the row before treating the sheet half below.", programPhase)
	}
	if sheetPhase < 1 {
		t.Errorf("W10-11(b) row 5 NOT DISCHARGED: `phase` appears on %d line(s) of the sheet "+
			"while the program has %d free numeric phase input(s) — the operator has no line for "+
			"an input the program will accept", sheetPhase, programPhase)
	}
	t.Logf("W10-11(b) rows 1/2/5 measured at %s (sheet phase=%d, program name=\"phase\"=%d)",
		dir, sheetPhase, programPhase)
}

func w10ReadStatusTemplate(t *testing.T) string {
	t.Helper()
	root := w10DcModuleRoot(t)
	b, err := os.ReadFile(filepath.Join(root, "internal", "web", "templates", "status.html"))
	if err != nil {
		t.Fatalf("reading status.html: %v", err)
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// W10-11(b) row 3 — the staleness discriminator the sheet pre-authorises.
// ---------------------------------------------------------------------------

// w10SheetPreAuthorisedAbsentNames returns the projection names the sheet's
// §3(c) tells the operator NOT to abort on. The extraction is over the
// pre-authorisation BLOCK, not the whole sheet: line 234's general rule
// ("Any OTHER name in an 'absent from both dumps' line is a real problem")
// names no projection and must not be swept in.
func w10SheetPreAuthorisedAbsentNames(sheet string) []string {
	lines := strings.Split(sheet, "\n")
	quoted := regexp.MustCompile(`projection "([a-z_]+)" absent from both dumps`)
	var out []string
	inBlock := false
	for _, line := range lines {
		if strings.Contains(line, "DO NOT abort on them") {
			inBlock = true
			continue
		}
		if inBlock {
			m := quoted.FindStringSubmatch(line)
			if m != nil {
				out = append(out, m[1])
				continue
			}
			if strings.TrimSpace(line) == "" {
				inBlock = false
			}
		}
	}
	sort.Strings(out)
	return out
}

func TestW10TheSheetDoesNotPreAuthoriseIgnoringAStaleToolsOwnDiscriminator(t *testing.T) {
	dir := w10DesignDir(t)
	t.Logf("LIVE-DOCUMENT: measured against design/ at %s", dir)
	sheet := w10ReadDoc(t, dir, "WINDOW-CHECKLIST-v1.md")
	compareSet := w10CompareSetFromSource(t)
	inSet := map[string]bool{}
	for _, k := range compareSet {
		inSet[k] = true
	}

	preAuthorised := w10SheetPreAuthorisedAbsentNames(sheet)

	// THE MECHANISM, from W10-2(d)(ii): the warning is emitted from inside a
	// loop over compareSet, so it "can only ever name a projection the
	// RUNNING TOOL still carries". A pre-authorised name that IS in the
	// running tool's compare set is therefore exactly a staleness
	// discriminator the operator has been told to ignore.
	var discriminators []string
	for _, n := range preAuthorised {
		if inSet[n] {
			discriminators = append(discriminators, n)
		}
	}
	if len(discriminators) != 0 {
		t.Errorf("W10-11(b) row 3 NOT DISCHARGED: the sheet pre-authorises ignoring %v, and %v "+
			"are STILL in the running tool's compareSet — so those lines are the only printed "+
			"signal that separates a stale dumpcompare from a current one, and the sheet has "+
			"told the operator not to abort on them.\n"+
			"  sheet pre-authorises: %v\n  compareSet (%d): %v\n"+
			"  discharged when: §3(c) is struck or inverted by B-4, so that ANY `absent from both "+
			"dumps` line sends the operator to §5",
			preAuthorised, discriminators, preAuthorised, len(compareSet), compareSet)
	}

	// NON-DEGENERACY, and it holds in BOTH directions the criterion names.
	// The named rival is "the sheet pre-authorises SOME names" — a bare
	// count, which cannot distinguish a dangerous pre-authorisation from a
	// harmless one. It is RUN, not described.
	rival := len(preAuthorised) > 0
	certified := len(discriminators) > 0
	if len(preAuthorised) == 0 {
		t.Errorf("the extractor found NO pre-authorised names at all; on a sheet still carrying "+
			"a §3(c) block that is an extraction failure, not a discharge. sheet is %d lines",
			len(strings.Split(sheet, "\n")))
	}
	t.Logf("NON-DEGENERACY: rival(any pre-authorised name at all)=%v · certified(pre-authorised "+
		"AND still in compareSet)=%v · pre-authorised=%v · discriminators=%v",
		rival, certified, preAuthorised, discriminators)
}

// TestW10B6sDumpcompareDischargeClauseOneOfThreeBuildPs1BuildsTheTool is ONE
// CLAUSE of W10-11(b) row 4, and the name says so. B-6's stated discharge is three
// clauses: "build.ps1 builds dumpcompare, the deploy places it somewhere
// named on the sheet, and the sheet names that machine". Only the first is
// mechanical. The other two are named in the failure message and in
// reports/window-build/W10-tests.md §2, and NOTHING here should be read as
// covering them.
func TestW10B6sDumpcompareDischargeClauseOneOfThreeBuildPs1BuildsTheTool(t *testing.T) {
	root := w10DcModuleRoot(t)
	b, err := os.ReadFile(filepath.Join(root, "deploy", "build.ps1"))
	if err != nil {
		t.Fatalf("reading deploy/build.ps1: %v", err)
	}
	// ⟦tester-go, 2026-09-06⟧ ⟦was: `if !strings.Contains(string(b), "cmd/dumpcompare") {`⟧
	// The bare containment was satisfied by the script's header COMMENT alone:
	// PRODUCED — with the `go build … cmd/dumpcompare` line deleted the old
	// assertion stayed GREEN (`grep -c cmd/dumpcompare` still ≥ 1). The
	// clause is "builds", so the witness now requires a NON-COMMENT line that
	// invokes `go build` on `cmd/dumpcompare`.
	builds := false
	for _, line := range strings.Split(string(b), "\n") {
		s := strings.TrimSpace(line)
		if strings.HasPrefix(s, "#") {
			continue
		}
		if strings.Contains(s, "go build") && strings.Contains(s, "cmd/dumpcompare") {
			builds = true
			break
		}
	}
	if !builds {
		t.Errorf("W10-11(b) row 4, CLAUSE 1 OF 3, NOT DISCHARGED: deploy/build.ps1 does not build " +
			"cmd/dumpcompare. The only thing in the tree that builds it is " +
			"deploy/demo/push-harness.ps1, and the only place it is placed is /root/c12/dumpcompare " +
			"INSIDE CONTAINER 501 — the lab's router container, not a post and not a gate PC.\n" +
			"  CLAUSES 2 AND 3 ARE NOT TESTED HERE: \"the deploy places it somewhere named on the " +
			"sheet\" and \"the sheet names that machine\". They need a judgement about what counts " +
			"as naming a machine and are recorded in reports/window-build/W10-tests.md §2.\n" +
			"  owner: B-6, trigger: \"THIS row, before B-4 can sign the sheet correct\".")
	}
}

// ---------------------------------------------------------------------------
// W10-11(b) row 6 — document->sheet citations resolve. GREEN today, and the
// criterion says green is a RESULT here, not a reason to drop the check.
// ---------------------------------------------------------------------------

// w10ChecklistLinePointers finds `WINDOW-CHECKLIST-v1.md:<n>` pointers and
// classifies each as LIVE or as a supersession record.
//
// ⚠ ONE DELIBERATE DEPARTURE FROM THE CRITERION'S LITERAL COMMAND, and it is
// a finding rather than a liberty. The criterion writes
// `grep -o 'WINDOW-CHECKLIST-v1\.md:[0-9]*'` and records SIX hits. `[0-9]*`
// matches ZERO digits, so on today's plan that same command returns NINE:
// the six real pointers plus three bare `WINDOW-CHECKLIST-v1.md:` occurrences
// introduced by W10's own fold. This function requires at least one digit,
// which reproduces the criterion's six exactly. The discrepancy is recorded
// in reports/window-build/W10-tests.md §3, not silently absorbed.
func w10ChecklistLinePointers(plan string) (live []string, superseded []string) {
	lines := strings.Split(plan, "\n")
	pat := regexp.MustCompile(`WINDOW-CHECKLIST-v1\.md:([0-9]+)`)
	// The plan's own supersession vocabulary. `⟦was:` is the convention;
	// the prose forms are the correction sentences that accompany it.
	markers := []string{"⟦was:", "today reads", "measured dead", "re-anchored", "Struck", "struck"}
	for i, line := range lines {
		for _, m := range pat.FindAllString(line, -1) {
			lo := i - 4
			if lo < 0 {
				lo = 0
			}
			hi := i + 2
			if hi > len(lines) {
				hi = len(lines)
			}
			window := strings.Join(lines[lo:hi], "\n")
			excused := false
			for _, mk := range markers {
				if strings.Contains(window, mk) {
					excused = true
					break
				}
			}
			entry := fmt.Sprintf("plan line %d: %s", i+1, m)
			if excused {
				superseded = append(superseded, entry)
			} else {
				live = append(live, entry)
			}
		}
	}
	return live, superseded
}

func TestW10NoLiveChecklistLinePointerSurvivesInThePlan(t *testing.T) {
	dir := w10DesignDir(t)
	t.Logf("LIVE-DOCUMENT: measured against design/ at %s", dir)
	plan := w10ReadDoc(t, dir, "WINDOW-BUILD-PLAN-v1.md")

	live, superseded := w10ChecklistLinePointers(plan)
	if len(live) != 0 {
		t.Errorf("W10-11(b) row 6 RED: %d LIVE WINDOW-CHECKLIST-v1.md:<n> pointer(s) in the plan:\n  %s\n"+
			"The sheet is scheduled to move at least three more times (B-4's edits, B-6's two "+
			"booked lines, W10-3(c)'s two probes), each shifting every line number below it. "+
			"Re-anchor to section-plus-box-text.", len(live), strings.Join(live, "\n  "))
	}
	if len(superseded) == 0 {
		t.Errorf("the extractor found NO pointers at all. Row 6 is green today because all six " +
			"surviving hits are supersession records; zero hits means the extraction broke, not " +
			"that the plan is clean.")
	}

	// NON-DEGENERACY. W10-11(b) row 6's own words: a check that COUNTED the
	// supersession records "would red forever, on the very supersession
	// records this project's ⟦was:⟧ convention REQUIRES be kept — a check
	// that can never go green, which is the mirror image of W10-13's check
	// that could never go red." The rival is RUN.
	rivalReds := len(live)+len(superseded) > 0
	certifiedReds := len(live) > 0
	if rivalReds == certifiedReds {
		t.Fatalf("DEGENERATE: the counting rival and the live-only certified rule return the same "+
			"answer on today's plan (%v), so this fixture does not separate them. live=%v superseded=%v",
			rivalReds, live, superseded)
	}
	t.Logf("W10-11(b) row 6 GREEN, and green is a RESULT: %d pointer(s), ALL supersession records, "+
		"NONE live.\n  %s\n  NON-DEGENERACY: rival(count all)=RED · certified(live only)=GREEN",
		len(superseded), strings.Join(superseded, "\n  "))
}

// TestW10TheLivePointerCheckCanRed plants a live pointer into a REAL on-disk
// copy and runs the SAME judgement function. Without it, row 6's green is a
// green with no evidence it could ever have been otherwise — which is L-M's
// own words about W10-13.
func TestW10TheLivePointerCheckCanRed(t *testing.T) {
	dir := w10DesignDir(t)
	t.Logf("LIVE-DOCUMENT: measured against design/ at %s", dir)
	plan := w10ReadDoc(t, dir, "WINDOW-BUILD-PLAN-v1.md")

	scratch := filepath.Join(t.TempDir(), "WINDOW-BUILD-PLAN-v1.md")
	planted := plan + "\n\nSee `design/WINDOW-CHECKLIST-v1.md:214` for the sha256 step.\n"
	if err := os.WriteFile(scratch, []byte(planted), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(scratch)
	if err != nil {
		t.Fatal(err)
	}

	live, superseded := w10ChecklistLinePointers(string(b))
	if len(live) != 1 || !strings.HasSuffix(live[0], "WINDOW-CHECKLIST-v1.md:214") {
		t.Fatalf("the plant did not red the certified rule: live=%v superseded=%d", live, len(superseded))
	}
	t.Logf("PRODUCED RED for row 6: the same judgement function reports %v on a planted live "+
		"pointer, so row 6's green is a state of the document and not a check that cannot fail",
		live)
}

// ---------------------------------------------------------------------------
// W10-13 — G8's work cell, extracted BY LABEL and never by line.
// ---------------------------------------------------------------------------

// w10ExtractG8Row is the criterion's own extraction, verbatim:
//
//	awk -F'|' '$2 ~ /\*\*G8\*\*/' design/CONSOLIDATED-PLAN-v1.md
//
// "⚠ The remedy must NOT pin a line number. Revision 1 cited
// CONSOLIDATED-PLAN-v1.md:466; the plan's rows have moved twice today.
// Extract by **G8**, grep that."
func w10ExtractG8Row(text string) string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Split(line, "|")
		if len(fields) < 2 {
			continue
		}
		if strings.Contains(fields[1], "**G8**") {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// w10G8Greps is the criterion's FOUR-grep falsifier. The criterion is FALSE
// if any returns 0. The fourth is the TRIPWIRE half, added at the pre-fold
// pass because "the replacement falsifier was NARROWER THAN ITS OWN
// ASSERTION".
//
// The third token is written as an escape ON PURPOSE, quoting the criterion:
// "Writing the literal open-bracket token into this draft would unbalance its
// own marker walk — a remedy that breaks the discipline it is written to
// enforce."
func w10G8Greps() []struct {
	Token string
	What  string
} {
	return []struct {
		Token string
		What  string
	}{
		{"L-G", "the repair rule is named"},
		{"mechanical", "its first condition is carried"},
		{"⟦was:", "its recording convention is carried"},
		{"mechanism working", "the TRIPWIRE half: the red is the mechanism working"},
	}
}

// w10G8RowBefore is the BEFORE half of W10-13's control pair, embedded AS A
// LITERAL rather than re-derived from git.
//
// ⚠⚠ DO NOT COLLAPSE THIS PAIR. W10-13's control is a before/after pair,
// each half naming the tree it measured, and the fold record says in terms
// why: "that pair is the only thing that lets a later reader tell the
// criterion is green because a fix landed rather than green because it never
// worked. The green looks identical either way. Repairing either half at the
// fold would destroy exactly the evidence the pair preserves."
//
// THE TREE THIS HALF MEASURED: design/CONSOLIDATED-PLAN-v1.md at commit
// 3f5f150 (= 71e15f9^, "L-I addendum: sibling sub-chunks share files by
// design", 2026-09-03 01:47:37) — the tree before the plan owner booked
// L-G's repair rule into G8's cell at 71e15f9, 01:49:10.
//
// Its measured values, from the LOCKED criterion: row size 529 bytes; L-G 0;
// mechanical 0; the was-token 0; mechanism working 0; seam repair 0;
// locked criterion 0; and L-G over the WHOLE file 0.
// ⛔ AND revision 1's falsifier — grep -c 'W6-3\|locked criterion' — returned
// 1 on this same row. That is the produced failure in its sharpest form: the
// OLD check said GREEN where all four of the NEW checks said 0.
const w10G8RowBefore = "| **G8** | Reprint deletion (B0) + sheet set on `/muster/print` (B2: paginator, master + index, NO COMPANY RECORDED, identity-line guard, §1.5 defect closures, **case-folded surname sort with `PersonID` tiebreak** ⟦LOCK — H-A rehomed 2026-09-02, plan owner; ruled at [[D-12]]/J-12 and declined by five packages as not-theirs; verified by W6-3's ordered literal, which turns a silent re-order into a loud red⟧) | B | G1; **one landing on the route with B0** | AUDIT-P0 §5 item 13's coordination; timed inside the window |"

func TestW10G8sWorkCellCarriesTheRepairRuleAndTheTripwire(t *testing.T) {
	dir := w10DesignDir(t)
	t.Logf("LIVE-DOCUMENT: measured against design/ at %s", dir)
	consolidated := w10ReadDoc(t, dir, "CONSOLIDATED-PLAN-v1.md")

	row := w10ExtractG8Row(consolidated)
	if row == "" {
		t.Fatalf("no row whose second pipe-field contains **G8** in CONSOLIDATED-PLAN-v1.md — " +
			"the label extraction failed, and a check over an empty row returns 0 for every " +
			"token for the wrong reason")
	}

	for _, g := range w10G8Greps() {
		if n := w10CountLines(row, g.Token); n == 0 {
			t.Errorf("W10-13 RED: G8's work cell does not carry %q (%s).\n  extracted row (%d bytes): %s",
				g.Token, g.What, len(row), row)
		}
	}
	// The criterion records 2142 bytes; awk's output carries a trailing
	// newline and this extraction does not, so 2141 here is the same row.
	t.Logf("W10-13 measured on the WORKING TREE: G8 row is %d bytes (the criterion's 2142 "+
		"includes awk's trailing newline)", len(row))
}

// TestW10TheG8FalsifierRepliesDifferentlyToTheBeforeAndAfterRows is the
// control pair, FOLDED AND NOT COLLAPSED. Both halves run through the SAME
// judgement functions, and the old falsifier is run alongside the new one on
// both, because the whole finding is that the old one could not tell them
// apart.
func TestW10TheG8FalsifierRepliesDifferentlyToTheBeforeAndAfterRows(t *testing.T) {
	dir := w10DesignDir(t)
	t.Logf("LIVE-DOCUMENT: measured against design/ at %s", dir)
	after := w10ExtractG8Row(w10ReadDoc(t, dir, "CONSOLIDATED-PLAN-v1.md"))
	before := w10G8RowBefore

	// The BEFORE half must still be the row it claims to be, or the literal
	// has been transcribed wrongly and the pair proves nothing.
	if len(before) != 528 {
		t.Fatalf("the embedded BEFORE row is %d bytes; the criterion measured 529 (528 without "+
			"its trailing newline). The literal has drifted from what it records.", len(before))
	}
	if !strings.Contains(before, "**G8**") {
		t.Fatal("the embedded BEFORE row is not a G8 row")
	}

	// revision 1's falsifier, RUN on both halves.
	oldFalsifier := func(row string) int {
		return w10CountLines(row, "W6-3") + w10CountLines(row, "locked criterion")
	}
	oldBefore, oldAfter := oldFalsifier(before), oldFalsifier(after)
	if oldBefore == 0 {
		t.Errorf("revision 1's falsifier returns %d on the BEFORE row; the criterion measured 1, "+
			"and that 1 IS the finding — \"the OLD check said GREEN on a row where all four of "+
			"the NEW checks said 0\"", oldBefore)
	}
	if oldAfter == 0 {
		t.Errorf("revision 1's falsifier returns %d on the AFTER row; the criterion measured 1", oldAfter)
	}

	// the four NEW greps, RUN on both halves.
	var beforeHits, afterHits []string
	for _, g := range w10G8Greps() {
		if w10CountLines(before, g.Token) > 0 {
			beforeHits = append(beforeHits, g.Token)
		}
		if w10CountLines(after, g.Token) > 0 {
			afterHits = append(afterHits, g.Token)
		}
	}
	if len(beforeHits) != 0 {
		t.Errorf("the four replacement greps hit %v on the BEFORE row; the criterion measured "+
			"ZERO for all four, which is what makes this half the PRODUCED ABSENCE", beforeHits)
	}
	if len(afterHits) == 0 {
		t.Errorf("the four replacement greps hit NOTHING on the AFTER row; the criterion measured " +
			"three of four satisfied, so either the fix has been reverted or the extraction broke")
	}

	// NON-DEGENERACY, stated as values: the old rule cannot tell the two
	// rows apart; the new rule can.
	if oldBefore != oldAfter {
		t.Errorf("revision 1's falsifier DID distinguish the two rows (%d vs %d) — if that were "+
			"so, W10-13's replacement would not have been necessary and this control proves "+
			"something other than what it claims", oldBefore, oldAfter)
	}
	if len(beforeHits) == len(afterHits) {
		t.Errorf("the replacement greps do NOT distinguish the two rows (%v vs %v)", beforeHits, afterHits)
	}
	t.Logf("W10-13 CONTROL PAIR, BOTH HALVES KEPT:\n"+
		"  BEFORE (3f5f150, %d bytes): old falsifier=%d (SAID GREEN) · new greps hit %v\n"+
		"  AFTER  (working tree, %d bytes): old falsifier=%d · new greps hit %v\n"+
		"  TRIPWIRE ARM `mechanism working` on AFTER = %d — RED, and the criterion says so.",
		len(before), oldBefore, beforeHits,
		len(after), oldAfter, afterHits,
		w10CountLines(after, "mechanism working"))
}

// ---------------------------------------------------------------------------
// GREEN-DIRECTION CONTROLS.
//
// ⚠ Every check above that is RED today needs its OTHER direction produced,
// or it is a check that can only ever be red — the mirror of W10-13's check
// that could never go red, and W10-11(b) row 6 names both failure modes in
// the same sentence. Each control below plants the DISCHARGE into a REAL
// on-disk copy under t.TempDir() (rule 20) and runs the SAME judgement
// function the assertion runs. Nothing under design/ or app/ is written.
// ---------------------------------------------------------------------------

// w10PlantDoc writes text to a real file under t.TempDir() and reads it back,
// so the judgement function is fed bytes off a disk it did not write in the
// same expression.
func w10PlantDoc(t *testing.T, name, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestW10EachDeskDocumentCheckCanGoGreenOnAPlantedDischarge(t *testing.T) {
	// One planted document per row, carrying exactly that row's stated
	// discharge and nothing else that could satisfy it by accident.
	plants := map[string]string{
		"W10-11(a) row 1":  "- the [[recording_gap]] key is a tombstone: [[D-35]] rules it NOT built.\n",
		"W10-11(a) row 2":  "- note the probe vehicle by number, unconditionally ([[D-65]]).\n",
		"W10-11(a) row 3":  "The board never renders a superseded value.\n",
		"W10-11(a) row 4":  "A superseded row is shown struck, never silently replaced.\n",
		"W10-11(a) row 5a": "Step cost is a continuous curve, not a cliff ([[D-55]]).\n",
		"W10-11(a) row 5b": "Step cost is a continuous curve, not a cliff ([[D-55]]).\n",
	}
	for _, r := range w10Rows11a() {
		text, ok := plants[r.Row]
		if !ok {
			t.Fatalf("no green-direction plant written for %s — an unproved direction is a "+
				"shortfall, not an omission", r.Row)
		}
		planted := w10PlantDoc(t, "planted.md", text)
		got, detail := r.Discharged(planted)
		if !got {
			t.Errorf("%s CANNOT GO GREEN on its own stated discharge (%s): the judgement "+
				"function still returns not-discharged (%s). The check is permanently red, "+
				"which makes it useless as a signal.", r.Row, r.Discharge, detail)
		}
		// And the same predicate must still be RED on a document that does
		// not carry the discharge, or "green on a plant" proves nothing.
		empty := w10PlantDoc(t, "empty.md", "nothing relevant here\n")
		if r.Row == "W10-11(a) row 1" || r.Row == "W10-11(a) row 2" {
			// These two discharge on ABSENCE, so an unrelated document is
			// legitimately green. Their red direction is the tree itself,
			// which the assertion above already produces.
			continue
		}
		if stillOK, _ := r.Discharged(empty); stillOK {
			t.Errorf("%s reports DISCHARGED on a document carrying nothing — the predicate is "+
				"not bound to its subject", r.Row)
		}
	}
}

func TestW10TheSheetChecksCanGoGreenOnAPlantedSheet(t *testing.T) {
	planted := w10PlantDoc(t, "WINDOW-CHECKLIST-v1.md", strings.Join([]string{
		"## 3 · Verify",
		"- [ ] Probe 3: author one event with time_source = entered and a typed instant 24h back.",
		"- [ ] Probe 4: author one occurred_at_correction at A and read it at B.",
		"- [ ] The rejected-records line cannot detect a missed bump at the peer.",
		"- [ ] Weather board: set the phase to 1 at each post and read it back.",
		"",
	}, "\n"))
	for _, needle := range []string{"time_source", "occurred_at_correct", "cannot detect a missed bump", "phase"} {
		if n := w10CountLines(planted, needle); n < 1 {
			t.Errorf("the sheet counting function cannot see %q on a sheet that carries it "+
				"(count %d) — the row is permanently red", needle, n)
		}
	}
	// The red direction on the same function, so green-on-a-plant is not a
	// function that returns >=1 for everything.
	bare := w10PlantDoc(t, "bare.md", "## 3 · Verify\n- [ ] Both posts serve.\n")
	for _, needle := range []string{"time_source", "occurred_at_correct", "cannot detect a missed bump", "phase"} {
		if n := w10CountLines(bare, needle); n != 0 {
			t.Errorf("the counting function returns %d for %q on a sheet that does not carry it", n, needle)
		}
	}
}

// TestW10TheStalenessRowSeparatesADangerousPreAuthorisationFromAHarmlessOne
// is row 3's non-degeneracy proof, and it is needed because on TODAY's sheet
// the rival and the certified rule happen to agree (both RED). Agreement on
// one instance is not degeneracy; being unable to disagree is. This produces
// the instance where they disagree.
func TestW10TheStalenessRowSeparatesADangerousPreAuthorisationFromAHarmlessOne(t *testing.T) {
	compareSet := w10CompareSetFromSource(t)
	inSet := map[string]bool{}
	for _, k := range compareSet {
		inSet[k] = true
	}
	if !inSet["workgroup"] {
		t.Fatalf("fixture assumption broken: `workgroup` is no longer in compareSet (%v) — "+
			"re-derive this control before trusting it", compareSet)
	}

	build := func(name string) string {
		return w10PlantDoc(t, "sheet.md", strings.Join([]string{
			"  **(c) You may see these two lines. DO NOT abort on them:**",
			`  ` + "`" + `warning: projection "` + name + `" absent from both dumps (old binary?)` + "`",
			"",
			"  Any OTHER name is a real problem.",
			"",
		}, "\n"))
	}

	// HARMLESS: the pre-authorised name is NOT in the running tool's
	// compare set, so the tool can never emit that line and ignoring it
	// discriminates nothing.
	harmless := build("person_amendment")
	if inSet["person_amendment"] {
		t.Fatalf("fixture collision: person_amendment is in compareSet")
	}
	hNames := w10SheetPreAuthorisedAbsentNames(harmless)
	hRival := len(hNames) > 0
	hCertified := false
	for _, n := range hNames {
		if inSet[n] {
			hCertified = true
		}
	}

	// DANGEROUS: the pre-authorised name IS in the compare set.
	dangerous := build("workgroup")
	dNames := w10SheetPreAuthorisedAbsentNames(dangerous)
	dRival := len(dNames) > 0
	dCertified := false
	for _, n := range dNames {
		if inSet[n] {
			dCertified = true
		}
	}

	if len(hNames) != 1 || len(dNames) != 1 {
		t.Fatalf("the extractor did not find exactly one name per fixture: harmless=%v dangerous=%v", hNames, dNames)
	}
	if hRival != dRival {
		t.Fatalf("the rival distinguished the two fixtures (%v vs %v); it is supposed to be blind to the difference", hRival, dRival)
	}
	if hCertified == dCertified {
		t.Fatalf("the certified rule did NOT distinguish them (%v vs %v) — row 3's check is a "+
			"bare presence count wearing a mechanism", hCertified, dCertified)
	}
	if hCertified || !dCertified {
		t.Fatalf("the certified rule answered backwards: harmless=%v dangerous=%v", hCertified, dCertified)
	}
	t.Logf("NON-DEGENERACY PRODUCED: rival(any pre-authorised name)=%v for BOTH fixtures · "+
		"certified(pre-authorised AND in compareSet)= harmless:%v dangerous:%v",
		hRival, hCertified, dCertified)
}

func TestW10TheG8TripwireGrepCanGoGreenOnAPlantedRow(t *testing.T) {
	dir := w10DesignDir(t)
	t.Logf("LIVE-DOCUMENT: measured against design/ at %s", dir)
	consolidated := w10ReadDoc(t, dir, "CONSOLIDATED-PLAN-v1.md")

	// Plant the ONE missing sentence into a real on-disk copy of the whole
	// document, then re-extract BY LABEL — so the plant is exercised through
	// the same extraction the assertion uses, not injected past it.
	var out []string
	planted := false
	for _, line := range strings.Split(consolidated, "\n") {
		fields := strings.Split(line, "|")
		if len(fields) >= 2 && strings.Contains(fields[1], "**G8**") && !planted {
			// strings.Replace does NOT report a no-op match: if "| AUDIT-P0"
			// has drifted out of this row, Replace silently returns the line
			// unchanged and `planted` would still flip true below, leaving the
			// failure message downstream describing a sentence that was never
			// inserted. Compare before/after so a silent no-op plant is
			// impossible, not just unlikely.
			before := line
			line = strings.Replace(line, "| AUDIT-P0",
				"⟦LOCK — G8's B0 deletion WILL red W6's locked reprint criteria, and the red is "+
					"the mechanism working.⟧| AUDIT-P0", 1)
			if line == before {
				t.Fatalf("plant into the G8 row was a no-op — %q not found in the row; the row's shape has changed", "| AUDIT-P0")
			}
			planted = true
		}
		out = append(out, line)
	}
	if !planted {
		t.Fatal("could not plant into the G8 row — the row's shape has changed")
	}
	text := w10PlantDoc(t, "CONSOLIDATED-PLAN-v1.md", strings.Join(out, "\n"))
	row := w10ExtractG8Row(text)
	if row == "" {
		t.Fatal("the planted document no longer yields a G8 row")
	}
	var missing []string
	for _, g := range w10G8Greps() {
		if w10CountLines(row, g.Token) == 0 {
			missing = append(missing, g.Token)
		}
	}
	if len(missing) != 0 {
		t.Fatalf("W10-13's four greps still report %v missing on a row carrying the tripwire "+
			"sentence — the check is permanently red", missing)
	}
	t.Logf("GREEN DIRECTION PRODUCED for W10-13: all four greps hit on a G8 row with the "+
		"tripwire sentence planted (%d bytes)", len(row))
}
