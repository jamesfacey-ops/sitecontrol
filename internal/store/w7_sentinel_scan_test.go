// w7_sentinel_scan_test.go — W7-5 (ZERO sentinel strings) and W7-12(e) (the
// `dumpcompare` compare set).
//
// design/WINDOW-BUILD-PLAN-v1.md `### W7 —` at :7421, criteria LOCKED
// 2026-09-03. Written from the CRITERION TEXT ONLY.
//
// ============================== W7-5 ====================================
//
// THE PROPERTY: *no string literal anywhere in production code, and no
// rendered string anywhere in a template, stands in for "this vehicle has no
// number".* [DOC — [[D-11]]: "'No number available' must be a distinct
// recorded state, NEVER A MAGIC STRING IN THE VEHICLE FIELD"; plan:7042: "no
// magic string anywhere (scan for sentinels)"; PLAN-SEC-A-schema.md:721.]
//
// ⚠ THE SUBJECT AND THE FILTER ARE STATED HERE, NOT LEFT AS AMBITION — this
// project has found a scan narrower than its subject SEVEN times, and a scan
// that says "anywhere" while walking one package is the eighth. THE SCAN'S
// INPUT IS:
//
//	(i)   every production `.go` file under `internal/` and `cmd/`
//	      (`_test.go` excluded), AND
//	(ii)  every file under `internal/web/templates/`, AND
//	(iii) every `.toml` under `deploy/` — ⚠ KEPT AS A LIMB, BUT NOT COUNTED
//	      AS REACH (⟦LOCK — FIL-F11⟧): [MEASURED] no `deploy/*.toml` key
//	      reaches a vehicle-bearing field on this tree, so the limb is CHEAP
//	      INSURANCE AGAINST A FUTURE CONFIG DEFAULT, NOT PRESENT COVERAGE,
//	      and `W7-X6` must not book it as a covered surface. ⚠ The `deploy/`
//	      shell and PS1 files are DELIBERATELY OUT — they compose no operator
//	      copy about a vehicle — and saying so is the point, since an
//	      unexplained exclusion is the shape a builder widens.
//
// THE EXCLUSIONS ARE LISTED AND EVERY ONE IS DECIDED — THERE ARE EXACTLY
// THREE:
//
//	(1) Go comments and Go-template comments `{{/* … */}}` — they never
//	    reach an operator.
//	(2) ⚠ `<style>` ONLY — because the `<script>` half's PREMISE IS FALSE ON
//	    THIS TREE (⟦LOCK — FIL-F3 / verdict §4.1⟧). [MEASURED —
//	    `gate.html:969` and `:987`] the page computes
//	    `… .value.trim() || 'this vehicle'` and assigns it to `textContent`,
//	    SO A `<script>` BODY'S STRING LITERAL IS DISPLAYED AS COPY.
//	    `<script>` bodies are ADMITTED; `<style>` stays excluded on the
//	    original reasoning, which is true of style bodies.
//	(3) the `placeholderVehicleIDs` MAP LITERAL ITSELF — it is a list of
//	    strings that must be REFUSED, not a sentinel that stands for
//	    anything, and a scan that flags it flags THE GUARD as the defect.
//	    ⚠ Exclusion (3) is W7-specific and is the one most likely to be got
//	    wrong.
//
// NOTHING ELSE IS EXCLUDED — no package filter, no element filter, no
// edited-file filter.
//
// ⚠⚠ THE FORBIDDEN LIST IS THE **SECOND** LIMB AND THE FIRST IS DERIVED
// (⟦LOCK — FIX-F8 / FAL-F2 / ENU-F4 / B-7⟧). Revision 1's list REDDED AGAINST
// THE CORRECT PRE-CHUNK TREE at five measured production sites and its
// NEGATIVE ARM COULD NOT FAIL; `"no number"`, `"not recorded"`,
// `"none recorded"`, `"no id"`, `"noid"`, `"--"` and `"n/a"` are STRUCK from
// limb 2, each with its reason, and `"no number"` is FOLDED INTO LIMB 1 so
// the helper's own declaration is exempt BY CONSTRUCTION rather than by a
// fourth exclusion. ⭐ Both remedies take the scan's output on the current
// tree to zero; only the folding leaves A NEGATIVE ARM THAT CAN FAIL.
//
// ⚠ THE LIST IS ILLUSTRATIVE OF A PROPERTY, NOT EXHAUSTIVE, AND SAYING SO IS
// THE POINT: this project has measured hand-kept enumerations going short
// four or more times, ALWAYS BY GOING SHORT.
//
// ============================= W7-12(e) =================================
//
// `cmd/dumpcompare`'s `compareSet` gains `vehicle` and does NOT gain
// `vehicle_amendment` (`DR-W7-9`), asserted as an EXACT SET LITERAL —
// thirteen members after W7, pinned in order.
//
// ⟦LOCK — D-74, 2026-09-05⟧ ⭐ THE "does NOT gain `vehicle_amendment`" HALF IS
// SUPERSEDED. James's ruling D-74 (reports/JAMES-DECISIONS.md:3910) confirms
// that the eight amendment / correction / retraction side tables ARE compared
// between the posts — S127 struck the class exclusion (bbff17e) after
// producing that, with them excluded, the tool printed IDENTICAL over
// "Robert Stone" / "Bob Stone". W7-12(e)'s `vehicle` half stands. Its test
// was CORRECTED to assert the replacement rule (PROPERTY 4 arm 2 and
// PROPERTY 5 inverted, PROPERTY 6 added) — not deleted, skipped or weakened.
// §219's hold ("must NOT be greened by an edit") was about authority, not
// about the old assertion being right, and D-74 supplies the authority.
// (m7-12b)'s second arm is now `vehicle_amendment` EXCLUDED from the set.
//
// ⚠⚠ AN OWNERSHIP CONFLICT, REPORTED AND NOT RESOLVED. ⟦LOCK — FIL-F12⟧
// RULES the check "an IN-PACKAGE test in `cmd/dumpcompare`, reading the live
// `compareSet` variable", and states why the choice has an observable
// consequence: "a set built by `append` in `init()` reads correctly to the
// in-package test and is INVISIBLE TO A TEXT SCAN of the literal". **This
// pass's own bounds confine it to `app/internal/**/w7_*_test.go`, so the
// in-package test CANNOT be written here.** The criterion anticipates the
// fallback in terms — "if a later hand makes it a source scan instead, RULE
// 20 ATTACHES (real on-disk copy, revert proved by `cmp`) AND THAT MUST BE
// RECORDED RATHER THAN ASSUMED" — so the source-scan form is written below,
// rule 20 IS attached, and the divergence is recorded here and at
// reports/window-build/W7-tests.md §5. ⚠ **IT DOES NOT DISCHARGE THE
// CRITERION'S RULED FORM**, and a build that assembles `compareSet` by
// `append` would be invisible to it.
//
// EXPECTED RED at e9cc5f5 (runtime):
//   - TestW7DumpCompareSetCarriesVehicle: TWELVE members, want thirteen, and
//     `vehicle` absent.
//
// ⟦LOCK — §64, 2026-09-03⟧ ⭐ `SEAM-W7-17` IS CLOSED. The W8 red this file
// booked against itself ARRIVED, on schedule and for the booked reasons
// (`workgroup` -> `trade`; `ref_alias` deleted by P-3 §5 / [[D-47]]), and the
// re-pin is recorded at the assertion below. The booking cost its author a
// paragraph and saved the W8 gate an investigation: the gate met a red that
// named itself, named the window that would cause it, and named the fix.
//
// ⟦NO PRE-CHUNK RED⟧:
//   - TestW7NoSentinelStandsInForAMissingVehicleNumber — its evidence is the
//     FOUR PLANTS, in FOUR DIFFERENT TREES, produced below.
//
// KILLER MUTANTS (§5):
//
//	(m7-5)   four arms: a Go constant in `internal/domain`; a rendered
//	         sentinel in a template; a sentinel assigned to `textContent`
//	         inside `gate.html`'s `<script>`; and the limb-1 positive plant
//	                                                 -> W7-5
//	(m7-12b) `vehicle` omitted from `compareSet`, or `vehicle_amendment`
//	         added to it — a replication comparator BLIND TO A REPLICATED
//	         CORRELATION KEY                         -> W7-12(e)
package store_test

import (
	"encoding/json"
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

	// ⟦LOCK — §67, 2026-09-03⟧ W7-12(e) now reads the LIVE REGISTRY instead of a
	// literal in a source file. This is `package store_test` — an EXTERNAL test
	// package — so importing domain/deadline/store is not a cycle.
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// w7Sentinels is LIMB 2 — the list AS IT NOW STANDS, with the colliding
// tokens struck.
//
// ⚠ STRUCK, EACH WITH ITS REASON, so a later hand does not "restore" them:
//
//	"no number"      -> FOLDED INTO LIMB 1 (below)
//	"not recorded"   -> FIVE production collisions, none of them a sentinel
//	"none recorded"  -> the same
//	"no id" / "noid" -> collide with `vehicle id` prose and with
//	                    `placeholderVehicleIDs`' own members
//	"--"             -> IT IS A PLACEHOLDER-MAP MEMBER (DR-W7-5's twelve), so
//	                    flagging it flags the guard
//	"n/a"            -> the same
//
// ⚠ `"--"` and `"n/a"` must ALSO be re-checked against HTML comments
// (`<!-- -->`) and Go comments before any future re-add — which is how `"--"`
// got onto revision 1's list in the first place.
//
// ⚠⚠⚠ A LOCK INTERACTION, MEASURED HERE AND REPORTED RATHER THAN ACCOMMODATED
// BY QUIETLY DROPPING A TOKEN.
//
// ⟦LOCK — FIL-F3⟧ ADMITS `<script>` bodies (the premise that a script body is
// never displayed as copy is measured FALSE at `gate.html:969`/`:987`).
// ⟦LOCK — FIX-F8 / B-7⟧ KEEPS `"(none)"` on limb 2. **The two amendments were
// not measured against each other, and together they REDDED THIS SCAN AGAINST
// THE CORRECT PRE-CHUNK TREE** — [MEASURED 2026-09-03]
// `internal/web/templates/_picker.html:167` reads `if (none) none.hidden =
// shown !== 0;`, a bare JavaScript IDENTIFIER preceded by `(`, which a
// whole-text match over an admitted script body flags as `(none)`.
//
// **That is the exact defect the criterion's own paragraph condemns two lines
// above itself**, and the end state of shipping it is the one the first
// standing rule refuses: a builder adds exclusions until it is green, each one
// narrowing the subject, while the CONTROL row stays satisfied because the
// plants still fire.
//
// ⭐ THE REMEDY TAKEN IS THE CRITERION'S OWN PROPERTY, NOT A NARROWING OF IT.
// W7-5 says "*no STRING LITERAL anywhere in production code, and no RENDERED
// STRING anywhere in a template*". A bare JS identifier is neither. So inside
// a `<script>` body **only string literals count**; outside one, all rendered
// text does. `"(none)"` STAYS ON THE LIST, plant 3 (a sentinel assigned to
// `textContent`) still fires, and `if (none)` is correctly not copy.
// ⚠ **NO TOKEN WAS STRUCK TO MAKE THIS GREEN.**
var w7Sentinels = []string{
	"nonumber", "NO_NUMBER", "no-number", "numberless", "unnumbered",
	"NONUM", "n/n", "UNKNOWN_VEHICLE", "NO_VEHICLE", "(none)", "(blank)",
}

// w7LimbOneTokens is LIMB 1's listed-token half. ⚠ THE LIMB-1 PREDICATE:
// *a listed token is flagged ONLY WHERE the string is a constant assigned to,
// compared against, or rendered in place of a vehicle-number-typed value.* So
// the helper's own declaration — a constant whose value happens to contain
// `no number` but which IS THE HONEST RENDER rather than a stand-in for it —
// is exempt BY CONSTRUCTION, and the negative arm becomes PRODUCIBLE instead
// of being deleted.
var w7LimbOneTokens = []string{"no number"}

// w7NumberBearingIdents are the Go identifiers that carry a vehicle number.
//
// ⚠⚠ A NARROWING STATED ON ITS FACE. The criterion's limb 1 flags a string
// constant assigned into a **`number`-typed value**, i.e. `vehicle.number` —
// which does not exist at e9cc5f5, so a limb-1 control CANNOT BE PLANTED
// against it and the limb would have no producible failure mode at all. The
// set below therefore ALSO carries the pre-chunk carrier of the same
// operator-facing datum, `VehicleEntered` — which `W7-8`'s ⟦LOCK — FIX-F12⟧
// clause names as "a SECOND READ PATH to the same operator-facing datum … the
// one every muster and board actually renders". ⚠ **When `vehicle.number`
// lands, `Number` below is what makes the limb reach it**, and a build that
// carries the number on a THIRD identifier is a gap in this scan, never a
// pass.
var w7NumberBearingIdents = []string{"VehicleEntered", "Vehicle", "Number", "number"}

// =========================================================================
// W7-5 — the sentinel scan
// =========================================================================
func TestW7NoSentinelStandsInForAMissingVehicleNumber(t *testing.T) {
	// --- CONTROL FIRST — FOUR PLANTS, AND THEIR LOCATIONS ARE SPECIFIED.
	//
	// ⚠ A plant inside the file the scan was written against proves
	// LOCATION-WITHIN-FILTER and never FILTER-COVERS-SUBJECT — the exact
	// defect W6's revision 1 committed. So the four land in four different
	// trees.

	// PLANT 1 — a sentinel CONSTANT in `internal/domain`, where a builder
	// would put it.
	goPlant := w7WriteSource(t, map[string]string{
		filepath.Join("domain", "vehicle.go"): "package domain\n\nconst missing = \"UNKNOWN_VEHICLE\"\n",
	})
	if hits := w7SentinelHitsInGo(t, goPlant); len(hits) != 1 {
		t.Fatalf("W7-5 CONTROL FAILED (plant 1): a sentinel constant in `internal/domain` produced %v, "+
			"want exactly one hit — (m7-5) arm A", hits)
	}

	// PLANT 2 — a rendered sentinel in a TEMPLATE. ⚠ `banner.html` is the
	// named location: a file W7 edits for `W7-26` and which sits OUTSIDE any
	// plausible domain-only filter. Three of §2.5's fourteen render sites are
	// in it, and it is THE LOUDEST SURFACE IN THE APPLICATION.
	tmplPlant := w7WriteSource(t, map[string]string{
		"banner.html": "<p>VEHICLE OVERDUE: <code>NO_VEHICLE</code></p>\n",
	})
	if hits := w7SentinelHitsInTemplates(t, tmplPlant); len(hits) != 1 {
		t.Fatalf("W7-5 CONTROL FAILED (plant 2): a rendered sentinel in `banner.html` produced %v, want exactly "+
			"one hit — (m7-5) arm B", hits)
	}

	// PLANT 3 — ⭐ A SENTINEL ASSIGNED TO `textContent` INSIDE A `<script>`
	// BODY. This is the plant revision 1's filter was STRUCTURALLY BLIND TO,
	// and it is producible today: [MEASURED — `gate.html:969`, `:987`] the
	// page already computes `… .value.trim() || 'this vehicle'` and assigns
	// the result to `textContent`, so a script body's string literal REACHES
	// AN OPERATOR'S EYE.
	scriptPlant := w7WriteSource(t, map[string]string{
		"gate.html": "<p id=\"v\"></p>\n<script>\n  document.getElementById('v').textContent = 'NO_VEHICLE';\n</script>\n",
	})
	if hits := w7SentinelHitsInTemplates(t, scriptPlant); len(hits) != 1 {
		t.Fatalf("W7-5 CONTROL FAILED (plant 3): a sentinel assigned to `textContent` inside a `<script>` body "+
			"produced %v, want exactly one hit. ⚠ The superseded filter excluded `<script>` on the premise that "+
			"\"a script body is executed and neither is displayed as copy\" — MEASURED FALSE on this tree.", hits)
	}

	// PLANT 3-NEGATIVE — a `<style>` body is STILL excluded, on the original
	// reasoning, which is true of style bodies.
	stylePlant := w7WriteSource(t, map[string]string{
		"styles.html": "<style>\n  .veh::after { content: 'NO_VEHICLE'; }\n</style>\n",
	})
	if hits := w7SentinelHitsInTemplates(t, stylePlant); len(hits) != 0 {
		t.Fatalf("W7-5 CONTROL FAILED (exclusion 2): a `<style>` body was flagged %v — exclusion (2) narrows to "+
			"`<style>` ONLY and stays on the original reasoning", hits)
	}

	// PLANT 4 — ⭐ THE LIMB-1 POSITIVE PLANT, WHICH IS THE ONE THAT PROVES
	// THE NEGATIVE ARM IS A CONTROL: `const noNumber = "no number"` ASSIGNED
	// INTO a number-bearing field. It separates *the words appearing* from
	// *the words standing in for the value*.
	limb1Plant := w7WriteSource(t, map[string]string{
		filepath.Join("domain", "render.go"): "package domain\n\n" +
			"const noNumber = \"no number\"\n\n" +
			"type row struct{ VehicleEntered string }\n\n" +
			"func f(r *row) { r.VehicleEntered = noNumber }\n",
	})
	if hits := w7LimbOneHits(t, limb1Plant); len(hits) != 1 {
		t.Fatalf("W7-5 CONTROL FAILED (plant 4, LIMB 1): `const noNumber = \"no number\"` assigned into a "+
			"number-bearing field produced %v, want exactly one hit. Without this plant the negative arm below "+
			"is a DESCRIPTION, not a control.", hits)
	}

	// NEGATIVE ARM (i) — THE HONEST RENDER MUST NOT BE FLAGGED. A constant
	// whose value contains `no number` but which is the helper's own output
	// is exempt BY CONSTRUCTION under limb 1's predicate.
	honest := w7WriteSource(t, map[string]string{
		filepath.Join("domain", "helper.go"): "package domain\n\n" +
			"const vehicleNoNumberSuffix = \" — no number recorded\"\n\n" +
			"func render(role string) string { return role + vehicleNoNumberSuffix }\n",
	})
	if hits := w7LimbOneHits(t, honest); len(hits) != 0 {
		t.Fatalf("W7-5 CONTROL FAILED (NEGATIVE ARM i): the honest render `‹role› — no number recorded` was "+
			"flagged %v. ⚠ A list broad enough to fire on the correct render is a list a builder will WEAKEN — "+
			"and each weakening narrows the subject. That is OW-1's recorded consequence.", hits)
	}
	if hits := w7SentinelHitsInGo(t, honest); len(hits) != 0 {
		t.Fatalf("W7-5 CONTROL FAILED (NEGATIVE ARM i, limb 2): the honest render was flagged by limb 2: %v — "+
			"`\"no number\"` is FOLDED INTO LIMB 1 and must not appear in limb 2's list", hits)
	}

	// --- THE ASSERTION, over all three trees -----------------------------
	root := w7ModuleRoot(t)
	roots := w7ProductionRoots(t)

	if hits := w7SentinelHitsInGo(t, roots...); len(hits) > 0 {
		t.Errorf("W7-5 (m7-5) arm A: sentinel string(s) in production Go:\n  %s\n"+
			"[[D-11]]: \"'No number available' must be a distinct recorded state, NEVER A MAGIC STRING IN THE "+
			"VEHICLE FIELD.\"", strings.Join(hits, "\n  "))
	}
	if hits := w7LimbOneHits(t, roots...); len(hits) > 0 {
		t.Errorf("W7-5 (m7-5) LIMB 1: a string constant stands in for a vehicle number:\n  %s",
			strings.Join(hits, "\n  "))
	}
	tmplDir := filepath.Join(root, "internal", "web", "templates")
	if hits := w7SentinelHitsInTemplates(t, tmplDir); len(hits) > 0 {
		t.Errorf("W7-5 (m7-5) arm B: sentinel string(s) rendered by a template:\n  %s",
			strings.Join(hits, "\n  "))
	}

	// NEGATIVE ARM (ii) — THE FIVE MEASURED PRODUCTION SITES MUST NOT BE
	// FLAGGED. [MEASURED 2026-09-02, the lock's own census] `gate.go:1227`,
	// `login.html:7`, `company_amend.html:145` (⚠ THE TEMPLATE W7 CLONES, so
	// the vehicle build adds a SIXTH), `muster.html:579` and `muster.html:586`
	// (⚠ on the SAME LINE as `{{$row.Vehicle}}`) all carry `not recorded`, and
	// NO STATED EXCLUSION REACHES ANY OF THEM.
	//
	// ⚠ THE FLOOR IS DERIVED, NEVER PINNED AT FIVE: a pinned figure here would
	// be this project's hand-kept-enumeration class committed inside the
	// remedy for it.
	decoys := w7NotRecordedOccurrences(t, roots, tmplDir)
	if decoys == 0 {
		t.Errorf("W7-5 NEGATIVE ARM (ii) IS PROTECTING NOTHING: the corpus contains ZERO occurrences of the " +
			"struck tokens (`not recorded` / `none recorded`), so the arm cannot fail and is not a control. " +
			"[MEASURED at the lock: FIVE non-comment production sites.] ⚠ The end state of a list that fires on " +
			"the correct build is the one the first standing rule refuses: a builder adds exclusions until it is " +
			"green, EACH ONE NARROWING THE SUBJECT, and the CONTROL row stays satisfied because the plants " +
			"still fire.")
	}

	// LIMB (iii) — `deploy/*.toml`. ⚠ KEPT AS A LIMB, NOT COUNTED AS REACH.
	deployDir := filepath.Join(root, "deploy")
	if hits := w7SentinelHitsInTOML(t, deployDir); len(hits) > 0 {
		t.Errorf("W7-5 limb (iii): sentinel string(s) in a deploy config:\n  %s\n"+
			"⚠ This limb is CHEAP INSURANCE AGAINST A FUTURE CONFIG DEFAULT, not present coverage — no "+
			"`deploy/*.toml` key reaches a vehicle-bearing field on this tree — and `W7-X6` must not book it as "+
			"a covered surface.", strings.Join(hits, "\n  "))
	}
}

// w7SentinelHitsInGo is LIMB 2 over production Go. ⚠ Its corpus is STRING
// LITERALS ONLY: exclusion (1) removes Go comments, and a comment narrating a
// sentinel is not a sentinel.
//
// ⚠ EXCLUSION (3) — the `placeholderVehicleIDs` map literal — is applied by
// SKIPPING literals inside that declaration, because it is a list of strings
// that must be REFUSED and a scan that flags it FLAGS THE GUARD AS THE
// DEFECT. This is the W7-specific exclusion and the one most likely to be got
// wrong.
func w7SentinelHitsInGo(t *testing.T, roots ...string) []string {
	t.Helper()
	var hits []string
	for _, path := range w7ProductionGoFiles(t, roots...) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("W7-5 FAULT — %s was NOT scanned, so this scan's coverage is smaller than its stated "+
				"scope: %v", path, err)
			continue
		}
		for _, lit := range w7GoStringLiterals(t, path, src) {
			if w7InsidePlaceholderMap(src, lit.off) {
				continue // exclusion (3)
			}
			for _, s := range w7Sentinels {
				if strings.Contains(strings.ToLower(lit.text), strings.ToLower(s)) {
					hits = append(hits, w7Located(path, src, lit.off)+" "+s)
				}
			}
		}
	}
	sort.Strings(hits)
	return hits
}

// w7LimbOneHits is LIMB 1: a string CONSTANT carrying a listed token AND
// assigned to / compared against a number-bearing identifier.
//
// ⚠ `""` is EXEMPT BY CONSTRUCTION, and that exemption is exactly `DR-W7-15`'s
// subject: THE EMPTY STRING IS THE ABSENCE, NOT A SENTINEL FOR IT.
func w7LimbOneHits(t *testing.T, roots ...string) []string {
	t.Helper()
	var hits []string
	for _, path := range w7ProductionGoFiles(t, roots...) {
		src, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		text := string(src)
		// Which constants in this file carry a listed token?
		var carrying []string
		for _, m := range w7ConstDecl.FindAllStringSubmatch(text, -1) {
			name, val := m[1], m[2]
			if val == `""` {
				continue // DR-W7-15: the empty string is the ABSENCE
			}
			for _, tok := range w7LimbOneTokens {
				if strings.Contains(strings.ToLower(val), strings.ToLower(tok)) {
					carrying = append(carrying, name)
				}
			}
		}
		// Is any of them assigned to / compared against a number-bearing
		// identifier? THAT is what separates *the words appearing* from *the
		// words standing in for the value*.
		for _, name := range carrying {
			for _, ident := range w7NumberBearingIdents {
				re := regexp.MustCompile(`(?m)\b` + regexp.QuoteMeta(ident) + `\b\s*(=|==|!=|:=)\s*` +
					regexp.QuoteMeta(name) + `\b`)
				if loc := re.FindStringIndex(text); loc != nil {
					hits = append(hits, w7Located(path, src, loc[0])+" "+name+" -> "+ident)
				}
			}
		}
	}
	sort.Strings(hits)
	return hits
}

var w7ConstDecl = regexp.MustCompile(`(?m)^\s*(?:const\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*(?:=|:=)\s*(` + "`" + `[^` + "`" + `]*` + "`" + `|"(?:[^"\\]|\\.)*")`)

// w7SentinelHitsInTemplates walks every file under a template directory.
//
// ⚠ EXCLUSION (2) IS `<style>` ONLY: `<script>` BODIES ARE ADMITTED, because
// [MEASURED — `gate.html:969`, `:987`] a script body's string literal reaches
// `textContent` and therefore an operator's eye. ⚠ Go-template comments
// `{{/* … */}}` are excluded under exclusion (1).
func w7SentinelHitsInTemplates(t *testing.T, dir string) []string {
	t.Helper()
	var hits []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("W7-5 FAULT — %s was NOT scanned, so this scan's coverage is smaller than its stated "+
				"scope: %v", path, err)
			return nil
		}
		text := w7StripTemplateComments.ReplaceAllString(string(src), "")
		text = w7StripStyleBodies.ReplaceAllString(text, "")
		// ⚠⚠ INSIDE A `<script>` BODY ONLY STRING LITERALS COUNT, AND THAT
		// IS THE CRITERION'S OWN PROPERTY RATHER THAN A NARROWING: W7-5 says
		// "no STRING LITERAL anywhere in production code, and no RENDERED
		// STRING anywhere in a template, stands in for …". A bare JavaScript
		// IDENTIFIER is neither. See the LOCK-INTERACTION note above
		// w7Sentinels: a coarser reading REDS AGAINST THE CORRECT PRE-CHUNK
		// TREE, which is the defect this criterion's own paragraph condemns.
		text = w7ScriptBodies.ReplaceAllStringFunc(text, func(body string) string {
			// ⟦instrument repair, 2026-09-05, found by the D-84 pass⟧ ⟦was:
			// `for _, r := range body { … kept.WriteRune(' ') }`⟧ — that
			// wrote ONE BYTE per RUNE, so a script body holding any
			// multi-byte rune (an em dash, a middle dot) produced an `out`
			// SHORTER than `body`, and the BYTE offsets spliced in below
			// overran it: `slice bounds out of range`, a panic, on a file
			// that carries no sentinel. The blank-out is now byte-for-byte,
			// which is what the offsets below assume. What the scan asserts
			// is unchanged.
			var kept strings.Builder
			for i := 0; i < len(body); i++ {
				if body[i] == '\n' {
					kept.WriteByte('\n')
				} else {
					kept.WriteByte(' ')
				}
			}
			out := kept.String()
			for _, loc := range w7JSStringLit.FindAllStringIndex(body, -1) {
				out = out[:loc[0]] + body[loc[0]:loc[1]] + out[loc[1]:]
			}
			return out
		})
		for _, s := range w7Sentinels {
			for _, loc := range regexp.MustCompile(`(?i)`+regexp.QuoteMeta(s)).FindAllStringIndex(text, -1) {
				hits = append(hits, path+":"+itoa(1+strings.Count(text[:loc[0]], "\n"))+" "+s)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("W7-5: walking %s: %v", dir, err)
	}
	sort.Strings(hits)
	return hits
}

var (
	w7StripTemplateComments = regexp.MustCompile(`(?s)\{\{/\*.*?\*/\}\}`)
	// ⚠ `<style>` ONLY. `<script>` is NOT stripped — that is the whole of
	// ⟦LOCK — FIL-F3⟧.
	w7StripStyleBodies = regexp.MustCompile(`(?is)<style\b[^>]*>.*?</style>`)
	// ⚠ `<script>` bodies are ADMITTED — but only their STRING LITERALS are
	// copy. `if (none) …` is a JS identifier, not a rendered string.
	w7ScriptBodies = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>`)
	w7JSStringLit  = regexp.MustCompile(`'(?:[^'\\\n]|\\.)*'|"(?:[^"\\\n]|\\.)*"|` + "`[^`]*`")
)

func w7SentinelHitsInTOML(t *testing.T, dir string) []string {
	t.Helper()
	var hits []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("W7-5 limb (iii): read %s: %v", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		src, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, s := range w7Sentinels {
			if regexp.MustCompile(`(?i)` + regexp.QuoteMeta(s)).Match(src) {
				hits = append(hits, path+" "+s)
			}
		}
	}
	sort.Strings(hits)
	return hits
}

// w7NotRecordedOccurrences counts the STRUCK tokens across the corpus — the
// decoys the negative arm protects. DERIVED, never pinned.
func w7NotRecordedOccurrences(t *testing.T, goRoots []string, tmplDir string) int {
	t.Helper()
	re := regexp.MustCompile(`(?i)not recorded|none recorded`)
	n := 0
	for _, path := range w7ProductionGoFiles(t, goRoots...) {
		src, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		n += len(re.FindAllIndex(src, -1))
	}
	_ = filepath.Walk(tmplDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		n += len(re.FindAllIndex(src, -1))
		return nil
	})
	return n
}

type w7Lit struct {
	text string
	off  int
}

func w7GoStringLiterals(t *testing.T, path string, src []byte) []w7Lit {
	t.Helper()
	var out []w7Lit
	// Reuse the token walk shape from the DDL scan: parse, then collect
	// BasicLits. Comments are excluded BY CONSTRUCTION here — exclusion (1).
	for _, loc := range w7GoStringLit.FindAllIndex(src, -1) {
		out = append(out, w7Lit{text: string(src[loc[0]:loc[1]]), off: loc[0]})
	}
	return out
}

var w7GoStringLit = regexp.MustCompile("`[^`]*`" + `|"(?:[^"\\\n]|\\.)*"`)

// w7InsidePlaceholderMap implements EXCLUSION (3): a literal inside the
// `placeholderVehicleIDs` declaration is a string that must be REFUSED, not a
// sentinel standing for anything.
func w7InsidePlaceholderMap(src []byte, off int) bool {
	text := string(src)
	start := strings.Index(text, "placeholderVehicleIDs")
	if start < 0 || off < start {
		return false
	}
	// The declaration ends at the first line that is exactly a closing brace
	// at column 0 after the start.
	end := strings.Index(text[start:], "\n}\n")
	if end < 0 {
		return false
	}
	return off < start+end
}

// =========================================================================
// W7-12(e) — `cmd/dumpcompare`'s compare set
// =========================================================================
//
// ⚠ THIS IS A CUTOVER CRITERION, NOT A TIDINESS ONE. `dumpcompare` is the
// PRINTED OPERATOR STEP at gate B-4 that proves the two posts agree during the
// outage; a replicated projection outside the set has its divergence INVISIBLE
// TO THE ONLY CHECK THAT WOULD CATCH IT — and `vehicle` is the key an arrival
// is CORRELATED ON, so an undetectable divergence in it is a divergence in WHO
// IS BOUND TO WHICH TRIP.
//
// ⚠ THIS IS THE ONE CRITERION IN W7 THAT EXISTS BECAUSE A DOCUMENT ASKED
// NOTHING — the brief found the question in no plan, spec or census.
//
// ⚠ AND THE LITERAL ENTRENCHES `ref_alias`, WHICH W8 IS BOOKED TO DELETE
// (⟦LOCK — ENU-F16⟧): P-3 §5's deletion schedule removes it at W8, **so this
// criterion WILL RED AT W8 and the re-pin is FORESEEABLE RATHER THAN A
// REGRESSION.** Recorded at `SEAM-W7-17` so W8's lock finds a booking instead
// of a surprise, and restated here so a W8 gate meeting the red knows it.
//
// ⚠ RULE 20 ATTACHES, and it is attached rather than assumed — see this
// file's header for why this is a source scan and not the ruled in-package
// test.
func TestW7DumpCompareSetCarriesVehicle(t *testing.T) {
	// ⟦LOCK — §67, 2026-09-03⟧ ⭐ REWRITTEN FROM AN ENUMERATION TO A PROPERTY.
	//
	// ⟦was: "\t// THE THIRTEEN AFTER W7, PINNED IN ORDER."⟧
	// ⟦was: "\twant := []string{"⟧
	// ⟦was: "\t\t\"company\", \"trade\", \"person\","⟧
	// ⟦was: "\t\t\"trip\", \"trip_occupant\", \"trip_arrival\", \"trip_cancel\", \"trip_state\","⟧
	// ⟦was: "\t\t\"arrival_state\","⟧
	// ⟦was: "\t\t\"visit\", \"visit_checkout\","⟧
	// ⟦was: "\t\t\"vehicle\","⟧
	// ⟦was: "\t}"⟧
	// ⟦was: "\tmembers := w7CompareSetMembers(t, path)"⟧
	// ⟦was: "\tif strings.Join(members, \",\") != strings.Join(want, \",\") {"⟧
	//
	// ⚠ WHY THE PIN HAD TO GO, and it is NOT a weakening. The pin asserted the
	// twelve names that ARE compared, IN ORDER. [PRODUCED, S64 R1] it was green
	// while two posts genuinely disagreed about a projection outside those
	// twelve — because it said nothing whatever about the eleven that are not
	// compared. ⟦§67 R1⟧ replaced the hand-kept list with a DERIVATION, so
	// there is no longer an order to pin and no longer a list to go short.
	//
	// ⭐ WHAT REPLACES IT IS STRICTLY STRONGER: the old pin could only catch an
	// edit to the list; these properties catch a projection the list never
	// knew about, which is the case that could diverge two SITES silently and
	// forever.
	//
	// ⚠ THE ORDER ASSERTION IS GONE AND THAT IS STATED RATHER THAN GLOSSED.
	// Order was a property of a hand-maintained literal — a thing that could be
	// reordered by hand and needed guarding. The derived set is sorted by
	// construction, so there is no hand-kept order left to rot.
	// ⟦LOCK — D-74, 2026-09-05⟧ ⭐ CORRECTED TO ASSERT THE SUPERSEDING RULE.
	// DR-W7-9's exclusion half — "reference-data projections are IN, amendment
	// side tables are OUT" — is SUPERSEDED by James's ruling D-74
	// (reports/JAMES-DECISIONS.md:3910): the eight amendment / correction /
	// retraction side tables ARE compared between the posts. S127 (bbff17e)
	// struck the class rule and PRODUCED why: with the eight excluded the tool
	// printed IDENTICAL while domain.Persons returned "Robert Stone" at one
	// post and "Bob Stone" at the other — the difference lived in an amendment
	// row it was not looking at.
	//
	// ⚠ §219 held this test red — "it must NOT be greened by an edit" — because
	// the edit was UNAUTHORISED, not because the old assertion was right. D-74
	// supplies the authority and attaches the constraint this rewrite obeys:
	// NOT deleted, NOT skipped, NOT weakened until it passes. The three arms
	// that asserted the old rule are INVERTED (the §82 "empty class map" arm,
	// PROPERTY 4 arm 2, PROPERTY 5's exclusion clause) and PROPERTY 6 asserts
	// the replacement. Every struck line is preserved as ⟦was:⟧.
	//
	// ⚠ THE READER CHANGED WITH THE RULE, FOR A MEASURED REASON. The old
	// `w7TopLevelMapLiteral` silently DROPPED any element it could not read as
	// string → string, so a class map restructured under the reader read as
	// EMPTY — indistinguishable from the struck map, which after D-74 is the
	// PASSING shape. A positive assertion ("the side tables are compared") on
	// a lossy reader passes on a broken instrument. The two live maps are now
	// read through `w10DcReadToolExclusions` (this package, §219(3)), which
	// refuses an unreadable map and an empty map with NO strike record, and
	// returns the struck suffixes so PROPERTY 6 can derive what the strike
	// released.
	path := filepath.Join(w7ModuleRoot(t), "cmd", "dumpcompare", "main.go")

	// --- CONTROL FIRST, PRODUCED: both instruments must discriminate ------
	// ⟦was: a two-entry control on w7TopLevelMapLiteral⟧ ⟦LOCK — D-74⟧ the
	// reader this test now uses is controlled on the ONE shape that is legal
	// after D-74 — an empty class map WITH a strike record — and on a live rule.
	ctlDir := w7WriteSource(t, map[string]string{"main.go": "package main\n\n" +
		"var ignoredProjections = map[string]string{\n\t\"a\": \"reason a\",\n\t\"b\": \"reason b\",\n}\n\n" +
		"// ⟦was: " + strconv.Quote("\t\"_x\": \"struck reason\",") + "⟧\n" +
		"var ignoredProjectionClasses = map[string]string{}\n\n" +
		"var handKept = []string{\"x\"}\n"})
	ctlNamed, ctlClasses, ctlStruck, err := w10DcReadToolExclusions(filepath.Join(ctlDir, "main.go"))
	if err != nil || len(ctlNamed) != 2 || ctlNamed["a"] != "reason a" || len(ctlClasses) != 0 ||
		strings.Join(ctlStruck, ",") != "_x" {
		t.Fatalf("W7-12(e) CONTROL FAILED: the exclusion reader returned named=%v classes=%v struck=%v err=%v "+
			"from a planted two-entry named map and a struck class map", ctlNamed, ctlClasses, ctlStruck, err)
	}
	liveDir := w7WriteSource(t, map[string]string{"main.go": "package main\n\n" +
		"var ignoredProjections = map[string]string{\n\t\"a\": \"reason a\",\n}\n\n" +
		"var ignoredProjectionClasses = map[string]string{\n\t\"_x\": \"live reason\",\n}\n"})
	if _, c, _, err := w10DcReadToolExclusions(filepath.Join(liveDir, "main.go")); err != nil || c["_x"] != "live reason" {
		t.Fatalf("W7-12(e) CONTROL FAILED: the exclusion reader missed a planted LIVE class rule (classes=%v, err=%v)", c, err)
	}
	if !w7TopLevelSliceLiteralExists(t, filepath.Join(ctlDir, "main.go"), "handKept") {
		t.Fatal("W7-12(e) CONTROL FAILED: the slice-literal detector missed a planted `handKept = []string{...}`")
	}
	if w7TopLevelSliceLiteralExists(t, filepath.Join(ctlDir, "main.go"), "noSuchVar") {
		t.Fatal("W7-12(e) CONTROL FAILED (negative): the slice-literal detector reported a variable that is not there")
	}

	// --- the live registry and the stated exclusions ----------------------
	registered := w7RegisteredProjections(t)
	inRegistry := map[string]bool{}
	for _, n := range registered {
		inRegistry[n] = true
	}
	// ⟦LOCK — §82, 2026-09-03⟧ THE EXCLUSIONS ARE NOW TWO MAPS, and this test
	// must read BOTH or its model of the compare set silently diverges from the
	// tool's. [MEASURED at the moment of the §82 repair] reading only the named
	// map reported "20 compared" where the tool compares 12 — a test that had
	// stopped describing the thing it pins.
	//
	// ⚠ WHY THERE ARE TWO. Three standing architectural scans (W3-14, W4-13b,
	// W5d-15) forbid any package outside internal/domain from naming a side
	// table, in an identifier or a string literal. §67's exclusion map was keyed
	// BY SIDE-TABLE NAME and breached all three. The side tables are now
	// excluded by CLASS SUFFIX, so the consumer names none of them.
	//
	// ⟦was: "\tignored := w7TopLevelMapLiteral(t, path, \"ignoredProjections\")"⟧
	// ⟦was: "\tclasses := w7TopLevelMapLiteral(t, path, \"ignoredProjectionClasses\")"⟧
	// ⟦LOCK — D-74⟧ READ THROUGH THE §219(3) READER, NOT THE LOSSY ONE — see
	// the note atop this function for the measured reason.
	ignored, classes, struck, err := w10DcReadToolExclusions(path)
	if err != nil {
		t.Fatalf("W7-12(e) INSTRUMENT BROKEN — this is NOT a criterion failure: %v\n"+
			"  If the tool's exclusion policy has been renamed or restructured again, RE-POINT THIS "+
			"READER — do not read this red as a finding about the compare set.", err)
	}
	isIgnored := func(name string) (string, bool) {
		if why, ok := ignored[name]; ok {
			return why, true
		}
		for suffix, why := range classes {
			if strings.HasSuffix(name, suffix) {
				return why, true
			}
		}
		return "", false
	}
	// ⟦was: "\tif len(classes) == 0 {"⟧
	// ⟦was: "\t\tt.Error(\"W7-12(e) ⟦§82⟧: `ignoredProjectionClasses` is empty or unreadable. The side-table exclusions \" +"⟧
	// ⟦was: "\t\t\t\"live there now; an empty class map means every side table is being COMPARED between posts.\")"⟧
	// ⟦LOCK — D-74⟧ INVERTED: "every side table is being COMPARED" is no longer
	// the alarm, it is the RULING. The reader above has already refused the two
	// shapes that merely look like an empty map.
	if len(classes) == 0 {
		t.Logf("W7-12(e) ⟦D-74⟧: `ignoredProjectionClasses` is EMPTY BY A RECORDED STRIKE of %v (S127, bbff17e) — "+
			"no class rule is in force", struck)
	}
	if len(registered) == 0 || len(ignored) == 0 {
		t.Fatalf("W7-12(e) INSTRUMENT BROKEN: registry=%d, ignore list=%d — a zero on either side would make "+
			"every property below vacuously true", len(registered), len(ignored))
	}
	// ⟦was: "\t\tif _, skip := ignored[n]; !skip {"⟧ ⟦LOCK — §82, 2026-09-03⟧
	var compared []string
	for _, n := range registered {
		if _, skip := isIgnored(n); !skip {
			compared = append(compared, n)
		}
	}

	// --- PROPERTY 1: the set is DERIVED, never enumerated ------------------
	if w7TopLevelSliceLiteralExists(t, path, "compareSet") {
		t.Errorf("W7-12(e) ⟦§67 R1⟧: `compareSet` is a HAND-KEPT SLICE LITERAL again in %s.\n"+
			"⚠ It was derived from the registry precisely because a hand-kept list of 12 against a registry "+
			"of 23 let two posts DISAGREE about the other 11 while the tool printed IDENTICAL and exited 0. "+
			"A list here governs whether two SITES are allowed to disagree; it must be the registry minus a "+
			"STATED exclusion, never a set of names someone remembered to type.", path)
	}

	// --- PROPERTY 2: the exclusion list's SHRINK TOOTH ---------------------
	// ⟦LOCK — §82, 2026-09-03⟧ the tooth extends to the CLASS rules: a suffix
	// matching no registered projection is a dead rule, and a dead rule is how
	// the exclusion list rots in its new shape.
	for suffix, reason := range classes {
		matched := false
		for _, n := range registered {
			if strings.HasSuffix(n, suffix) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("W7-12(e) ⟦§82⟧ SHRINK TOOTH: exclusion CLASS %q (%s) matches no registered projection. "+
				"A class rule that excludes nothing is dead, and a dead rule hides the day its members are "+
				"renamed out from under it.", suffix, reason)
		}
		if strings.TrimSpace(reason) == "" {
			t.Errorf("W7-12(e) ⟦§82⟧: exclusion class %q states no reason", suffix)
		}
	}
	for name, reason := range ignored {
		if !inRegistry[name] {
			t.Errorf("W7-12(e) ⟦§67 R1⟧ SHRINK TOOTH: `ignoredProjections` excludes %q (%s), which is NOT a "+
				"registered projection. A stale exclusion is a rotted list — the same defect one layer down. "+
				"⚠ Do not simply delete the entry: a projection that was DELIBERATELY excluded and has now "+
				"vanished is a change that needs a ruling.", name, reason)
		}
	}

	// --- PROPERTY 3: every exclusion STATES ITS REASON ---------------------
	for name, reason := range ignored {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("W7-12(e) ⟦§67 R1⟧: `ignoredProjections[%q]` has no stated reason. Excluding a projection "+
				"from the two-post comparison is a deliberate act and must read as one — an unexplained entry "+
				"is indistinguishable from an oversight, which is how the list it replaced went wrong.", name)
		}
	}

	// --- PROPERTY 4: the criterion's OWN subject, unchanged ----------------
	// ⚠ `vehicle` is the key an arrival is CORRELATED on, so an undetectable
	// divergence in it is a divergence in WHO IS BOUND TO WHICH TRIP.
	if !inRegistry["vehicle"] {
		t.Error("W7-12(e) (m7-12b): `vehicle` is not a registered projection at all — W7 item 9 is not landed")
	} else if _, skipped := isIgnored("vehicle"); skipped {
		t.Errorf("W7-12(e) (m7-12b): `vehicle` is EXCLUDED from the compare set (%q).\n"+
			"⚠ `dumpcompare` is the printed operator step at gate B-4 that proves the two posts agree during "+
			"the outage, and `vehicle` is the key an arrival is CORRELATED on — so a replicated vehicle "+
			"divergence left outside this set is a silent disagreement between the posts about WHO IS BOUND "+
			"TO WHICH TRIP, in exactly the window where agreement is the point.", w7Why(isIgnored, "vehicle"))
	}
	// ⟦was: "\tif _, skipped := isIgnored(\"vehicle_amendment\"); inRegistry[\"vehicle_amendment\"] && !skipped {"⟧
	// ⟦was: "\t\tt.Error(\"W7-12(e) (m7-12b) arm 2: `vehicle_amendment` is COMPARED. It follows `person_amendment` and \" +"⟧
	// ⟦was: "\t\t\t\"`company_amendment` OUT — reference-data projections are IN, amendment side tables are OUT (DR-W7-9).\")"⟧
	// ⟦LOCK — D-74, 2026-09-05⟧ (m7-12b) ARM 2 INVERTED. DR-W7-9's exclusion half
	// is superseded: `vehicle_amendment` carries site_id, node_epoch, seq and the
	// amended facts — replicated content about the correlation key itself, one
	// table over — and a per-post disagreement in it is a disagreement about WHO
	// IS BOUND TO WHICH TRIP that the old rule made invisible.
	if !inRegistry["vehicle_amendment"] {
		t.Error("W7-12(e) (m7-12b) arm 2 ⟦D-74⟧: `vehicle_amendment` is not a registered projection. A ruling's " +
			"subject has vanished; that is a change that needs a ruling, not a quiet pass.")
	} else if why, skipped := isIgnored("vehicle_amendment"); skipped {
		t.Errorf("W7-12(e) (m7-12b) arm 2 ⟦D-74⟧: `vehicle_amendment` is EXCLUDED from the compare set (%q). "+
			"D-74 supersedes DR-W7-9's exclusion half: the side tables ARE compared.", why)
	}

	// --- PROPERTY 5: §36's clause, which the derivation must preserve ------
	// ⟦§36⟧ recorded compareSet as "projection names only — contains `company`,
	// NOT `company_amendment`", and that W8-13(a)'s required entry is `trade`.
	// ⟦LOCK — D-74⟧ the `company` / `trade` IN half STANDS; the "NOT
	// `company_amendment`" half is superseded and its arm is inverted below.
	for _, name := range []string{"company", "trade"} {
		if _, skipped := isIgnored(name); !inRegistry[name] || skipped {
			t.Errorf("W7-12(e) ⟦§36⟧: %q must be COMPARED (registered=%v, excluded=%v)", name, inRegistry[name], skipped)
		}
	}
	// ⟦was: "\tif _, skipped := isIgnored(\"company_amendment\"); inRegistry[\"company_amendment\"] && !skipped {"⟧
	// ⟦was: "\t\tt.Error(\"W7-12(e) ⟦§36⟧: `company_amendment` is COMPARED — §36 records this set as projection names only\")"⟧
	if why, skipped := isIgnored("company_amendment"); inRegistry["company_amendment"] && skipped {
		t.Errorf("W7-12(e) ⟦§36 as superseded by D-74⟧: `company_amendment` is EXCLUDED from the compare set (%q). "+
			"Its base `company` is compared and it is one of the eight D-74 rules compared with it.", why)
	}

	// --- PROPERTY 6: ⟦D-74, 2026-09-05⟧ THE SIDE TABLES ARE COMPARED --------
	// Two arms, DERIVED and NAMED, and both are needed.
	//
	//   (a) DERIVED — every registered projection carrying a suffix the S127
	//       strike released is COMPARED, unless it is named individually AND
	//       its base projection is named too: a side table may leave the
	//       compare set only in the company of its base. `alert_ack_retraction`
	//       follows `alert_ack` out on site-locality (§218: "acknowledgment is
	//       per-post operator state, not replicated content"); nothing else
	//       qualifies today. This arm sees a NINTH side table the day it is
	//       registered, and it sees a class rule re-added under any name.
	//
	//   (b) NAMED FLOOR — D-74's own subject: the eight S127 released,
	//       preserved verbatim at reports/window-build/S82-PASS2-BOUNDARY.md
	//       §P3, each REGISTERED and each COMPARED. A floor cannot go short in
	//       the dangerous direction — at worst it fails to mention a ninth,
	//       which (a) covers — and it carries the SHRINK TOOTH: one of the
	//       eight renamed out of the registry is a ruling's subject vanishing,
	//       which needs a ruling. ⚠ The eight are NAMED HERE, in a _test.go
	//       file, which the W3-14 / W4-13b / W5d-15 scans exempt; the
	//       production tool still names none of them.
	//
	// ⭐ WHY THIS PROPERTY EXISTS, in one line: with the eight excluded the
	// tool printed "IDENTICAL: 12 compared projections match" and exited 0
	// while domain.Persons returned "Robert Stone" at one post and "Bob
	// Stone" at the other (S126, S127) — the all-clear was blind to exactly
	// the class of difference that corrections create.
	if len(struck) == 0 {
		t.Errorf("W7-12(e) ⟦D-74⟧ (a) has nothing to derive from: the class map's declaration preserves no struck " +
			"suffix. With a non-empty class map that means the strike record was deleted (§78's pointer rule), " +
			"which is itself the finding; arm (b) still runs.")
	}
	struckSuffix := func(n string) (string, bool) {
		for _, suf := range struck {
			if strings.HasSuffix(n, suf) {
				return suf, true
			}
		}
		return "", false
	}
	var sideTables, comparedSide []string
	for _, n := range registered {
		suf, isSide := struckSuffix(n)
		if !isSide {
			continue
		}
		sideTables = append(sideTables, n)
		why, out := isIgnored(n)
		base := strings.TrimSuffix(n, suf)
		_, baseOut := ignored[base]
		switch {
		case !out:
			comparedSide = append(comparedSide, n)
		case baseOut:
			t.Logf("W7-12(e) ⟦D-74⟧ (a): %q follows its base %q OUT (%s) — the one shape D-74 leaves outside", n, base, why)
		default:
			t.Errorf("W7-12(e) ⟦D-74⟧ (a): side table %q is EXCLUDED from the compare set (%q) while its base %q is not. "+
				"D-74 rules the side tables COMPARED; a side table may leave only in the company of its base. "+
				"Excluding it again needs a ruling, not an entry.", n, why, base)
		}
	}
	if len(struck) > 0 && len(sideTables) == 0 {
		t.Errorf("W7-12(e) ⟦D-74⟧ (a) is VACUOUS: no registered projection carries any struck suffix %v — either the "+
			"registry lost its side-table naming or the reader is pointed at the wrong declaration", struck)
	}
	eight := []string{
		"person_amendment", "company_amendment", "trip_amendment", "arrival_amendment",
		"trade_amendment", "vehicle_amendment", "arrival_ref_retraction", "occurred_at_correction",
	}
	for _, n := range eight {
		if !inRegistry[n] {
			t.Errorf("W7-12(e) ⟦D-74⟧ (b) SHRINK TOOTH: %q — one of the eight D-74 rules COMPARED — is not a registered "+
				"projection. A ruling's subject has vanished; that needs a ruling, not a quiet deletion from this list.", n)
			continue
		}
		if why, out := isIgnored(n); out {
			t.Errorf("W7-12(e) ⟦D-74⟧ (b): %q is EXCLUDED from the compare set (%q). D-74: the eight side tables ARE "+
				"compared — with them excluded the tool printed IDENTICAL over \"Robert Stone\" / \"Bob Stone\".", n, why)
		}
	}

	t.Logf("W7-12(e) [MEASURED]: %d registered projections; %d excluded by name and %d exclusion CLASSES (struck: %v), "+
		"all with stated reasons; %d compared: %v\n  ⟦D-74⟧ %d side table(s) carry a struck suffix, %d compared: %v",
		len(registered), len(ignored), len(classes), struck, len(compared), compared,
		len(sideTables), len(comparedSide), comparedSide)
}

// ⟦LOCK — §67, 2026-09-03⟧ --- W7-12(e)'s NEW INSTRUMENTS ------------------
//
// ⚠ THESE PARSE, THEY DO NOT GREP. The old extractor was a regex over bytes,
// and when `compareSet` became a derivation the regex began matching the
// ⟦was:⟧ comment recording the old literal — reporting `[⟧  (a hand-kept list
// of 12 against a` as the compare set. ⭐ That is exactly the defect §63 ruled
// on in the W8 census: a text scan cannot tell CODE from COMMENT. Both helpers
// below use go/parser, so a comment can never be mistaken for a declaration.

// w7Why returns the stated reason an exclusion applies, for a diagnostic.
// ⟦LOCK — §82, 2026-09-03⟧
func w7Why(f func(string) (string, bool), name string) string {
	why, _ := f(name)
	return why
}

// w7RegisteredProjections is the registry, built the way `sitectl -dump` builds
// it — BOTH Register calls (cmd/sitectl/main.go:45,51). An empty, freshly
// registered store dumps exactly the registered projection NAMES.
//
// ⚠ THIS IS THE CRITERION'S RULED FORM ARRIVING AT LAST. This file's header
// records that the source-scan form "DOES NOT DISCHARGE THE CRITERION'S RULED
// FORM, and a build that assembles `compareSet` by `append` would be invisible
// to it". W8 assembled it by derivation, the source scan went blind exactly as
// predicted, and the read-the-live-thing form is what replaces it.
func w7RegisteredProjections(t *testing.T) []string {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("W7-12(e) INSTRUMENT: opening an in-memory store: %v", err)
	}
	defer st.Close()
	if err := domain.Register(st); err != nil {
		t.Fatalf("W7-12(e) INSTRUMENT: domain.Register: %v", err)
	}
	if err := deadline.Register(st); err != nil {
		t.Fatalf("W7-12(e) INSTRUMENT: deadline.Register: %v", err)
	}
	b, err := st.DumpProjections()
	if err != nil {
		t.Fatalf("W7-12(e) INSTRUMENT: DumpProjections: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("W7-12(e) INSTRUMENT: decoding the dump: %v", err)
	}
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// w7TopLevelMapLiteral reads `var <name> = map[string]string{...}` from a Go
// file and returns it. Parsed, never grepped.
//
// ⟦LOCK — D-74, 2026-09-05⟧ RETAINED, UNUSED BY THE ASSERTION, so the
// supersession is legible (the w7CompareSetDecl precedent below). It was
// replaced because it silently DROPS any element it cannot read as
// string → string, so a restructured map read as EMPTY — and after D-74 an
// empty class map is the PASSING shape, which made this reader unsafe under
// a positive assertion. W7-12(e) now reads through w10DcReadToolExclusions.
func w7TopLevelMapLiteral(t *testing.T, path, name string) map[string]string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("W7-12(e) INSTRUMENT: parsing %s: %v", path, err)
	}
	out := map[string]string{}
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
				for _, e := range cl.Elts {
					kv, ok := e.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					k, kok := kv.Key.(*ast.BasicLit)
					v, vok := kv.Value.(*ast.BasicLit)
					if !kok || !vok {
						continue
					}
					out[strings.Trim(k.Value, `"`)] = strings.Trim(v.Value, `"`)
				}
			}
		}
	}
	return out
}

// w7TopLevelSliceLiteralExists answers "has someone put the hand-kept list
// back?". ⟦LOCK — §67, 2026-09-03⟧ It is the regression guard on the
// derivation itself: reverting to an enumerated `compareSet` is the defect
// §67 R1 closed, and it must not be able to return quietly.
func w7TopLevelSliceLiteralExists(t *testing.T, path, name string) bool {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("W7-12(e) INSTRUMENT: parsing %s: %v", path, err)
	}
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
				if id.Name == name && i < len(vs.Values) {
					if _, isLit := vs.Values[i].(*ast.CompositeLit); isLit {
						found = true
					}
				}
			}
		}
	}
	return found
}

// ⟦was: "var w7CompareSetDecl = regexp.MustCompile(`(?s)compareSet\s*=\s*\[\]string\{(.*?)\n\}`)"⟧
// Retained (unused by the assertion) only so the supersession is legible; the
// regex form is what §63's ruling says a scan must not be.
var w7CompareSetDecl = regexp.MustCompile(`(?s)compareSet\s*=\s*\[\]string\{(.*?)\n\}`)

func w7CompareSetMembers(t *testing.T, path string) []string {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("W7-12(e): reading %s: %v", path, err)
	}
	m := w7CompareSetDecl.FindSubmatch(src)
	if m == nil {
		return nil
	}
	var out []string
	for _, lit := range regexp.MustCompile(`"([^"]*)"`).FindAllStringSubmatch(string(m[1]), -1) {
		out = append(out, lit[1])
	}
	return out
}
