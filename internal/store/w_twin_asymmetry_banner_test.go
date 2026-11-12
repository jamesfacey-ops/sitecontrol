// w_twin_asymmetry_banner_test.go — TWIN ASYMMETRY: a ruling's supersession
// state must agree across every place the ruling is DECLARED.
//
// ⭐ WHY THIS EXISTS. This project's design text is DUPLICATED: a numbered
// design-ruling section (`##### DR-W8-8 — …`) is written in FULL, twice — once
// in `design/WINDOW-BUILD-PLAN-v1.md` (the plan) and again, near-verbatim,
// under its own window's criteria draft (`reports/window-build/W8-criteria-
// draft.md`). When a ruling is later superseded, a human bans the LIVE half
// with a banner at ONE copy and, twice now, missed the twin:
//
//	DR-W8-8  — plan ~L13750 had no banner; the criteria draft's copy did.
//	           Fixed 2026-09-09 (commit c593a57).
//	DR-W10-5 — the criteria draft's OWN declaration (~L344) had no banner;
//	           the plan's copy did. Fixed 2026-09-09 (commit c07dd14).
//
// Both were found by a human-directed read, neither by any check, and the
// second was found only incidentally while fixing the first. Two by hand in
// two days is the signature of a class, not of two accidents — hence this
// file.
//
// ⭐⭐ THIS TEST FOUND A THIRD, PREVIOUSLY UNKNOWN INSTANCE ON FIRST RUN:
// `DR-W7-9`'s declaration in `reports/window-build/W7-criteria-draft.md`
// (~L739) carries "⛔ THE EXCLUSION HALF OF THIS RULING IS SUPERSEDED BY
// [[D-74]] …"; its twin in `design/WINDOW-BUILD-PLAN-v1.md` (~L8513) carries
// NONE — it still reads "reference-data projections are IN, amendment side
// tables are OUT" as if that were still the live rule. MEASURED by this test,
// 2026-09-09, on `witness/twin-asymmetry` off `integration/witness-2026-09-05`
// @ c876ea4. See reports/witness/TWIN-ASYMMETRY-DETECTOR.md. ⛔ THIS IS NOT
// FIXED HERE — the brief for this file is detect-and-report, the docs repo
// has an active amendment lane in it, and a docs fix is not this test's job.
//
// ⭐ THE "SAME SECTION" RULE, STATED SO A LATER READER CAN JUDGE IT.
// A ruling is DECLARED at a line matching an ATX heading (2-6 `#`) whose text
// begins with one or more `DR-W<digits>-<digits>` identifiers — optionally
// glossed with a `(…)` aside, optionally two joined by `/` — immediately
// followed (only whitespace or one gloss between) by an em dash. Examples,
// all real, all matched: "##### DR-W6-5 — the read …", "##### DR-W6-1 (Q1) —
// …", "### DR-W8-2 / DR-W8-3 — …". A DECLARATION SITE's "section" is the
// heading line plus every line up to (not including) the next ATX heading of
// ANY depth in the same file, or EOF.
//
// ⚠️ WHY THE GRAMMAR IS NARROWED TO `DR-W\d+-\d+` AND NOT WIDENED TO EVERY
// ID SHAPE THE PROJECT USES (bare `D-74`, `S127`, `W8-13`). MEASURED counter-
// example: `## W4-13 — registration, coverage, consumer scan, route row` is a
// PARENT heading in `reports/window-build/W4-falsification.md`; three lines
// later `### W4-13(a) — …` is its CHILD sub-criterion. A grammar that strips
// the `(a)` as a gloss (the way `DR-W6-1 (Q1)` glosses its DR- id) would read
// "W4-13" as declared TWICE in the SAME FILE — a false twin, not a real one,
// and the false-positive class this file exists to avoid, not produce. The
// `DR-W\d+-\d+` family has no such parent/child heading shape anywhere in the
// corpus (MEASURED: every one of its ~49 distinct ids appears as EXACTLY two
// heading sites, one per twin, never nested) and is exactly the family both
// confirmed real defects belong to. Widening the grammar is future work, not
// silently assumed here.
//
// ⭐ THE "BANNER" RULE. A banner is the presence, ANYWHERE in the section
// body, of one of three phrases: "SUPERSEDED BY", "STALE SINCE", "RETRACTED
// BY". Derived by measurement, not invented: a corpus-wide grep of
// design/*.md and reports/window-build/*.md on 2026-09-09 found "SUPERSEDED
// BY" 28 times, "STALE SINCE" 4 times (always spelled with a space, never
// "STALE-SINCE" despite that spelling appearing in commit MESSAGES) and
// "RETRACTED BY" once; no fourth phrase occurs anywhere in the corpus. `⟦was:
// "…"⟧` is deliberately NOT treated as a banner signal on its own — it marks
// EVERY textual revision in this house style (605 occurrences in the plan
// alone), the overwhelming majority of them cosmetic, so using it as the
// banner signal would fire on nearly every heading that was ever touched.
//
// ⚠️ WHAT THIS MISSES, STATED AGAINST ITSELF.
//   - A twin duplicated OUTSIDE `design/` and `reports/window-build/` (e.g. a
//     third copy pasted into some other reports/ subtree) is invisible to
//     this file's directory scope.
//   - A restatement of a ruling's claim that is NOT under one of that
//     ruling's OWN headings — e.g. a ruling-index TABLE ROW, or the claim
//     re-asserted inside an unrelated criterion's body (`W8-13`'s criterion
//     text, `W10-10`'s criterion body) — is invisible. The `drw88-docs-sweep`
//     pass (commits c593a57, b3fbdb5) had to hand-fix exactly this shape
//     alongside the heading twins; it is a real, related, and definitely
//     broader defect class that this file does NOT attempt, because "does
//     this prose restate a ruling" has no heading-shaped signature to key on
//     and a heuristic loose enough to catch it would fire constantly.
//   - A NEW banner phrase the house convention has not used yet (e.g.
//     "OVERRULED BY") reads as "no banner" on both sides — an asymmetry where
//     one side gains a not-yet-recognised phrasing and the other has nothing
//     would be missed, not caught, because both sides look bannerless to this
//     vocabulary.
//   - Only ids seen as a heading in 2+ sites are compared; an id declared
//     exactly once is out of scope by construction (there is no twin to
//     disagree with).
//
// ⭐ ANTI-VACUITY. Every structural step is t.Fatalf, never a skip and never a
// silent zero: the two directories must exist and be read (via w10DesignDir,
// which already fails rather than skips — see its own LIVE-DOCUMENT CLASS
// note), the heading grammar must match at least one ruling id, and at least
// one of those ids must have 2+ declaration sites — otherwise the corpus has
// changed shape (a rename, a merge, a directory move) and the check would
// otherwise silently stop comparing anything while still reporting `ok`. A
// companion test below
// (TestTwinAsymmetryMechanismDistinguishesAgreeAndDisagree) proves the
// comparison itself on planted fixtures under
// t.TempDir(), independent of the live corpus, so a future corpus that
// happens to have zero live mismatches still has a positive control on
// record.
//
// ⚠️ THIS IS A GUARD, NOT ONE OF THE W10 "RED BY DESIGN, EXCLUDED FROM ANY
// TALLY" CLASS documented in w10_desk_documents_test.go. It is meant to be
// GREEN on a clean tree, the way
// ⟦was: TestW8DemoDuplicateScanKeyfieldsAreLiveProjectionPrimaryKeys — not in the public repository⟧ is. At the
// time of writing it is RED — not by design,
// but because it found DR-W7-9's real, unfixed asymmetry (above). Per this
// witness's brief, that red is reported, NOT registered in DECLARED-REDS.md
// and NOT weakened to pass: "never edit a test to green a declared red, and
// never register a red to make a suite pass" binds equally to a check that
// is merely NEWLY red for a real reason.
package store_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// twinBannerWords — see the banner rule above. Order does not matter; any one
// hit anywhere in the section body counts as bannered.
var twinBannerWords = []string{"SUPERSEDED BY", "STALE SINCE", "RETRACTED BY"}

// twinRulingHeadingRe — see the same-section rule above. Group 1 is always
// populated when the regexp matches; group 2 only for a `/`-joined pair.
var twinRulingHeadingRe = regexp.MustCompile(
	`^#{2,6}\s+(DR-W\d+-\d+)(?:\s*\([^)]*\))?(?:\s*/\s*(DR-W\d+-\d+)(?:\s*\([^)]*\))?)?\s*(?:\x{2014}|--)`,
)

// twinAnyHeadingRe finds section boundaries: any ATX heading, of any depth,
// naming any subject. This is deliberately broader than twinRulingHeadingRe —
// it is what CLOSES a ruling's section, not what opens one.
var twinAnyHeadingRe = regexp.MustCompile(`^#{1,6}\s`)

// twinSite is one place a ruling id is DECLARED (heading + section body).
type twinSite struct {
	dirLabel  string // "design" or "reports/window-build" (or a fixture label in the self-test)
	file      string // basename
	line      int    // 1-based heading line number
	heading   string // trimmed heading line, for failure messages
	hasBanner bool
}

// twinScanDir reads every *.md file directly inside dir (non-recursive — both
// real subject directories are flat) and records every ruling declaration
// site found, keyed by ruling id. t.Fatalf on any read/empty-file error: a
// document check that silently skips an unreadable file is the failure mode
// this whole class of test refuses (see w10DesignDir's own note).
func twinScanDir(t *testing.T, dirLabel, dir string) map[string][]twinSite {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s (%s): %v", dirLabel, dir, err)
	}
	sites := map[string][]twinSite{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		if len(b) == 0 {
			t.Fatalf("%s is EMPTY — every scan over it would find zero rulings for the wrong reason", path)
		}
		lines := strings.Split(string(b), "\n")

		var headingIdx []int
		for i, l := range lines {
			if twinAnyHeadingRe.MatchString(l) {
				headingIdx = append(headingIdx, i)
			}
		}
		for _, i := range headingIdx {
			m := twinRulingHeadingRe.FindStringSubmatch(lines[i])
			if m == nil {
				continue
			}
			var ids []string
			if m[1] != "" {
				ids = append(ids, m[1])
			}
			if m[2] != "" {
				ids = append(ids, m[2])
			}
			end := len(lines)
			for _, h := range headingIdx {
				if h > i {
					end = h
					break
				}
			}
			body := strings.Join(lines[i:end], "\n")
			hasBanner := false
			for _, w := range twinBannerWords {
				if strings.Contains(body, w) {
					hasBanner = true
					break
				}
			}
			site := twinSite{
				dirLabel:  dirLabel,
				file:      e.Name(),
				line:      i + 1,
				heading:   strings.TrimSpace(lines[i]),
				hasBanner: hasBanner,
			}
			for _, id := range ids {
				sites[id] = append(sites[id], site)
			}
		}
	}
	return sites
}

// twinMergeSites combines the per-directory maps produced by twinScanDir.
func twinMergeSites(maps ...map[string][]twinSite) map[string][]twinSite {
	out := map[string][]twinSite{}
	for _, m := range maps {
		for id, s := range m {
			out[id] = append(out[id], s...)
		}
	}
	return out
}

// twinEvaluateAgreement is the PURE comparison: given a ruling id and its 2+
// declaration sites, it reports whether they agree on banner presence and, if
// not, a message naming the ruling and every site. Pure (no *testing.T) on
// purpose — it lets the self-test below assert on the comparison's own output
// with plain conditionals, rather than needing to construct or inspect a
// testing.T from outside the testing package's own machinery.
func twinEvaluateAgreement(id string, sites []twinSite) (agree bool, message string) {
	if len(sites) < 2 {
		return false, "PRECONDITION: called with " + intToStr(len(sites)) + " site(s) for " + id + "; agreement is only meaningful for 2+"
	}
	bannered := 0
	for _, s := range sites {
		if s.hasBanner {
			bannered++
		}
	}
	if bannered == 0 || bannered == len(sites) {
		return true, ""
	}
	var b strings.Builder
	b.WriteString("TWIN ASYMMETRY: ")
	b.WriteString(id)
	b.WriteString(" is declared in ")
	b.WriteString(intToStr(len(sites)))
	b.WriteString(" places and they DISAGREE on supersession state:\n")
	for _, s := range sites {
		status := "NO BANNER"
		if s.hasBanner {
			status = "HAS BANNER"
		}
		b.WriteString("  - ")
		b.WriteString(s.dirLabel)
		b.WriteString("/")
		b.WriteString(s.file)
		b.WriteString(":")
		b.WriteString(intToStr(s.line))
		b.WriteString(" [")
		b.WriteString(status)
		b.WriteString("] ")
		b.WriteString(s.heading)
		b.WriteString("\n")
	}
	b.WriteString("A reader consuming the un-bannered copy sees a superseded ruling asserted as live.")
	return false, b.String()
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}

// TestTwinAsymmetryRulingBannersAgreeAcrossDeclarationSites — the live check.
//
// Subject: design/*.md and reports/window-build/*.md, resolved from
// SITECONTROL_DESIGN_DIR exactly as w10DesignDir resolves it (design/ itself,
// with reports/window-build/ taken as its sibling) — see that function's own
// LIVE-DOCUMENT CLASS note for why this dependency is explicit-but-hard-
// failing rather than vendored, skipped, or hashed. This test is EXCLUDED
// from that note's "no assertion changed, comment-only" framing: it is a
// fresh guard, not a member of the nine-test red-by-design class, and its
// result is NOT to be read the way that class's results are read.
func TestTwinAsymmetryRulingBannersAgreeAcrossDeclarationSites(t *testing.T) {
	designDir := w10DesignDir(t) // fails, does not skip, if absent
	root := filepath.Dir(designDir)
	reportsDir := filepath.Join(root, "reports", "window-build")
	if fi, err := os.Stat(reportsDir); err != nil || !fi.IsDir() {
		t.Fatalf("PRECONDITION UNMET — reports/window-build/ not found at %s (%v). Both known real "+
			"twin-asymmetry instances (DR-W8-8, DR-W10-5) live between design/ and this directory; "+
			"without it this check compares nothing and that must not read as a pass.", reportsDir, err)
	}

	sites := twinMergeSites(
		twinScanDir(t, "design", designDir),
		twinScanDir(t, "reports/window-build", reportsDir),
	)
	if len(sites) == 0 {
		t.Fatalf("ANTI-VACUITY: found ZERO DR-W\\d+-\\d+ ruling headings under %s and %s. Either the "+
			"heading grammar has drifted from the house convention or these directories no longer hold "+
			"the ruling text at all — both are findings, and a check that silently found nothing must "+
			"not report ok.", designDir, reportsDir)
	}

	var ids []string
	pairCount := 0
	for id, s := range sites {
		ids = append(ids, id)
		if len(s) >= 2 {
			pairCount++
		}
	}
	sort.Strings(ids)

	if pairCount == 0 {
		t.Fatalf("ANTI-VACUITY: %d ruling id(s) found under %s and %s, but EVERY one has exactly one "+
			"declaration site. The twin-duplication class this check exists for (a ruling declared in "+
			"BOTH the plan and its criteria draft) was not observed even once — that is either a real "+
			"structural change (the twin relationship itself is gone) or this test's directory/heading "+
			"assumptions no longer hold. Either way this must be a loud failure, not a pass.",
			len(sites), designDir, reportsDir)
	}
	t.Logf("scanned %d ruling id(s), %d with 2+ declaration sites, under %s and %s",
		len(sites), pairCount, designDir, reportsDir)

	for _, id := range ids {
		s := sites[id]
		if len(s) < 2 {
			continue // no twin to disagree with; not this check's business
		}
		id, s := id, s
		t.Run(id, func(t *testing.T) {
			if agree, msg := twinEvaluateAgreement(id, s); !agree {
				t.Fatalf("%s", msg)
			}
		})
	}
}

// TestTwinAsymmetryMechanismDistinguishesAgreeAndDisagree — the positive
// control this file's own header promises: proves the comparison can tell an
// agreeing pair from a disagreeing one, on fixtures this test plants and owns
// under t.TempDir(). Independent of the live corpus's current state, so a
// future run where the live corpus has zero disagreements still carries
// evidence the mechanism was capable of finding one. Modeled on
// TestW10TheLivePointerCheckCanRed's shape (same file class, same reason).
func TestTwinAsymmetryMechanismDistinguishesAgreeAndDisagree(t *testing.T) {
	dir := t.TempDir()
	agree := "##### DR-W99-1 — a fixture ruling\n\nSome ordinary ruling text with nothing special in it.\n\n##### DR-W99-2 — next section\n"
	agreeTwin := "### DR-W99-1 — a fixture ruling, restated\n\nOrdinary restated text, still no banner on this side either.\n"
	disagreeA := "##### DR-W99-3 — a mismatched fixture ruling\n\n⛔ **STALE SINCE 2026-09-09 — reopened.**\n"
	disagreeB := "### DR-W99-3 — a mismatched fixture ruling, restated\n\nNo banner here at all — this copy reads as still live.\n"
	agreeConsistentBoth := "##### DR-W99-4 — bannered on both sides\n\n⛔ **SUPERSEDED BY [[D-99]].**\n"
	agreeConsistentBothTwin := "### DR-W99-4 — bannered on both sides, twin\n\n⛔ **STALE SINCE 2026-09-09.**\n"

	writeFixture(t, dir, "plan.md", agree+"\n"+disagreeA+"\n"+agreeConsistentBoth)
	writeFixture(t, dir, "criteria.md", agreeTwin+"\n"+disagreeB+"\n"+agreeConsistentBothTwin)

	sites := twinScanDir(t, "fixture", dir)
	for _, want := range []string{"DR-W99-1", "DR-W99-3", "DR-W99-4"} {
		if len(sites[want]) < 2 {
			t.Fatalf("PRECONDITION: fixture setup is broken — %s should have 2 declaration sites, found %d",
				want, len(sites[want]))
		}
	}

	// DR-W99-1: neither side bannered -> agree -> must NOT fail.
	if agree, msg := twinEvaluateAgreement("DR-W99-1", sites["DR-W99-1"]); !agree {
		t.Fatalf("DR-W99-1 (both sides unbannered) was reported as a mismatch; it is not one: %s", msg)
	}

	// DR-W99-4: both sides bannered -> agree -> must NOT fail.
	if agree, msg := twinEvaluateAgreement("DR-W99-4", sites["DR-W99-4"]); !agree {
		t.Fatalf("DR-W99-4 (both sides bannered) was reported as a mismatch; it is not one: %s", msg)
	}

	// DR-W99-3: one side bannered, one not -> disagree -> MUST fail, and the
	// failure message must name the ruling id and both sites.
	gotAgree, gotMsg := twinEvaluateAgreement("DR-W99-3", sites["DR-W99-3"])
	if gotAgree {
		t.Fatalf("DR-W99-3 (one side bannered, one not) was reported as agreeing; the mechanism cannot see a real mismatch")
	}
	if !strings.Contains(gotMsg, "DR-W99-3") {
		t.Fatalf("mismatch message does not name the ruling id: %q", gotMsg)
	}
	if !strings.Contains(gotMsg, "plan.md") || !strings.Contains(gotMsg, "criteria.md") {
		t.Fatalf("mismatch message does not name both sites: %q", gotMsg)
	}
}

func writeFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("writing fixture %s: %v", name, err)
	}
}
