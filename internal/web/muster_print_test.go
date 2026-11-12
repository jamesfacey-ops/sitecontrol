// muster_print_test.go — U3. The muster's PRINT path is its primary output:
// this sheet is read aloud during an evacuation. Everything here is about
// what survives onto paper and about the three categories staying distinct
// there, where there is no colour and no scrolling.
//
// The three categories, and why they may never blur:
//
//	on site      counted in the total.
//	away         recorded as gone out to a pad with no gate and no timer.
//	             NOT on site, NOT in the total — and, from 2026-08-21, NOT
//	             ON THE PAPER. James's ruling: this operation keeps track of
//	             them but is not responsible for them, so the printed sheet
//	             carries a count and a line saying whose they are, never
//	             their names. The names stay on screen.
//	not tracked  left on a vehicle, not aboard at arrival; the system cannot
//	             say where they got off. A gap in KNOWLEDGE, not a status —
//	             nothing is watching and no timer will fire. STILL PRINTS,
//	             names and all: an unknown about somebody who left on our
//	             vehicle from our site is not a handoff to anybody, and
//	             under D-6 they are still ours to account for.
//
// Class-name trap (F19, and its reverse in auth_test.go): a class name in
// the ONE shared inlined stylesheet is not a rendered element. Tests here
// that assert on the page assert on `class="x"` attributes or on data-*
// hooks; tests that are ABOUT the stylesheet extract it first and say so.
package web

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
)

// --- the structural hooks the grouping tests stand on ---

// ⟦LOCK — §110, 2026-09-03⟧ ONE LIST OF (hook, direction) PAIRS, not two lists.
//
// DIRECTION OF EXPECTATION IS A REQUIRED, ADJACENT FIELD. With two lists an entry can
// be filed in the wrong one and still read perfectly; with pairs the direction sits
// beside the name and a reviewer sees both at once. It also makes the retired hook
// self-documenting: `Present: false` states mechanically what a prose annotation could
// only assert.
//
// ⚠ WHY THIS EXISTS. Until 2026-09-03 this list named the pre-W8 grouping attribute and
// did NOT name `data-trade-group`, which muster.html:315 has rendered since 19eafc4.
// A list entry naming something that no longer exists CANNOT FAIL, and its presence
// implies coverage that is not there. [MEASURED, reports/window-build/S100-ALIAS.md §8]
// styling `[data-trade-group]` and running the style guard returned PASS: the F19 defect
// went straight through and nothing anywhere went red. ⛔ A blind guard is worse than no
// guard — no guard prompts someone to write one; a blind guard says the job is done.
type structuralHook struct {
	Name string
	// Present is the DIRECTION OF EXPECTATION and is what decides whether an entry can
	// fail. `true`: the corpus must render it. `false`: the corpus must NOT — a retired
	// hook whose absence is asserted deliberately, which is a LIVE assertion (it fires
	// the moment the thing comes back), not a ghost.
	Present bool
	Why     string
}

var musterStructuralHooks = []structuralHook{
	{"data-muster-section", true, "scopes a grep to one board section"},
	{"data-company-group", true, "names one company section; muster_test.go's grouping assertions read it"},
	{"data-trade-group", true, "names one listed trade section — the hook this list was BLIND to until §106"},
	{"data-tally-count", true, "the on-site/away counts"},
	{"data-tally-row", true, "one tally row"},
	{"data-away-pad", true, "an away pad's block"},
	{"data-muster-mode", true, "live vs reprint"},
	{"data-alert-row", true, "V-01: lets a scoped grep find one alert block"},
	{"data-review-kind", true, "V-08: one review row"},

	// ⚠ The retired pre-W8 grouping attribute. §106 ruled it is KEPT rather than swapped
	// out, because removing it as part of the swap would silently discard a check somebody
	// may have intended. As a PAIR it is no longer a ghost: `Present: false` is a live
	// assertion that the attribute stays GONE, and it reds if anything renders it again.
	{"data-workgroup-group", false, "retired at 19eafc4 when the page moved to data-trade-group; asserted ABSENT so a revival is loud"},
}

// structuralHookCorpus is what the pairs are checked against: every template plus every
// PRODUCTION .go file under internal/ — so a hook written from Go rather than from a
// template still counts as present.
//
// ⚠ WHAT THIS IS NOT, stated so the check is not over-trusted: it is a SOURCE census, not
// a render. [MEASURED 2026-09-03] the obvious render-based form — "assert each hook appears
// in the page this guard already fetches" — CANNOT be used: only 4 of the 9 present hooks
// appear on a bare /muster (the rest need a company, a trade, an away pad, an alert or a
// review row), so that form would red on five HEALTHY hooks. A hook that is present in a
// template but only reachable from a dead branch therefore still passes here. That is a
// weaker failure than the rename this catches, and it is named rather than hidden.
func structuralHookCorpus(t *testing.T) string {
	t.Helper()
	root := w2ModuleRoot(t)
	var b strings.Builder
	err := filepath.Walk(filepath.Join(root, "internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		name := info.Name()
		if !strings.HasSuffix(name, ".html") && (!strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go")) {
			return nil
		}
		c, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		b.Write(c)
		b.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatalf("fixture defect: walking the rendered-source corpus under %s: %v", root, err)
	}
	if b.Len() == 0 {
		t.Fatalf("fixture defect: the rendered-source corpus is EMPTY, so every Present:false pair would pass vacuously")
	}
	return b.String()
}

// TestStructuralHooksExistAsDeclared is §110's mechanism: the watch-list must ASSERT ITS
// ENTRIES EXIST, so a rename breaks it LOUDLY instead of blinding it.
//
// ⭐ It carries its own CONTROL, in the shape w8_gate_controls_test.go:241 and
// w6_print_render_test.go:385 already use independently: the check is applied to a
// deliberately broken input and must fire, IN BOTH DIRECTIONS, before its silence on the
// real corpus is believed. ⛔ That condition is not decoration — the pass that wrote this
// test first "proved" a guard was blind with a mutant that had itself been injected into a
// template comment and styled nothing.
func TestStructuralHooksExistAsDeclared(t *testing.T) {
	corpus := structuralHookCorpus(t)

	// --- THE CONTROL, FIRST: prove the probe bites before trusting its silence. ---
	const plantedPresent = "data-s110-planted-hook-that-does-not-exist"
	if strings.Contains(corpus, plantedPresent) {
		t.Fatalf("fixture defect: the planted name %q is really in the corpus, so the control proves nothing", plantedPresent)
	}
	if hookIsAsDeclared(corpus, structuralHook{plantedPresent, true, "control"}) {
		t.Fatal("CONTROL FAILED (present arm): a hook that is NOT in the corpus was reported as present. " +
			"The check cannot see a renamed hook, so its clean pass on the real list means nothing")
	}
	// The absent arm needs a name the corpus really does contain.
	const plantedAbsent = "data-muster-section"
	if !strings.Contains(corpus, plantedAbsent) {
		t.Fatalf("fixture defect: %q is not in the corpus, so the absent-arm control proves nothing", plantedAbsent)
	}
	if hookIsAsDeclared(corpus, structuralHook{plantedAbsent, false, "control"}) {
		t.Fatal("CONTROL FAILED (absent arm): a hook that IS in the corpus satisfied an ABSENT expectation. " +
			"A revived retired hook would pass unnoticed")
	}

	// --- the real list ---
	for _, h := range musterStructuralHooks {
		if hookIsAsDeclared(corpus, h) {
			continue
		}
		if h.Present {
			t.Errorf("STRUCTURAL HOOK %q IS DECLARED PRESENT AND IS NOT RENDERED ANYWHERE.\n"+
				"  its job: %s\n"+
				"  Either it was renamed and this list was not re-pointed — in which case every assertion "+
				"reading it has gone VACUOUS and is passing for a reason unrelated to its subject — or it "+
				"is genuinely retired, and its pair must be flipped to Present:false with a reason.",
				h.Name, h.Why)
			continue
		}
		t.Errorf("STRUCTURAL HOOK %q IS DECLARED ABSENT AND HAS COME BACK.\n"+
			"  the basis of that pair: %s\n"+
			"  A retired hook reappearing is either a revival nobody ruled, or a rename landing on a "+
			"spelling this project deliberately retired.", h.Name, h.Why)
	}
}

func hookIsAsDeclared(corpus string, h structuralHook) bool {
	return strings.Contains(corpus, h.Name) == h.Present
}

// The whole point of data-muster-section / data-company-group /
// data-trade-group / data-tally-count is that the stylesheet never
// mentions them: a restyle then cannot change what a structural test proves,
// and a stylesheet substring can never stand in for a rendered element. If
// somebody ever styles one of these, the grouping assertions in
// muster_test.go quietly become stylesheet matches — which is exactly the
// defect (F19) this repo has now been bitten by twice.
//
// ⟦LOCK — §110⟧ The loop now reads the SAME pairs list, and style-checks only the hooks
// declared Present. A hook asserted ABSENT is not style-checked, deliberately: styling a
// name the page never renders is not the F19 defect, and including it would re-create the
// entry-that-cannot-fail this list was rebuilt to remove.
func TestMusterStructuralHooksAreNeverStyled(t *testing.T) {
	srv, _ := newMusterTestServer(t, 0)
	css := stylesheetOf(t, rawPage(t, srv, "/muster"))
	checked := 0
	for _, h := range musterStructuralHooks {
		if !h.Present {
			continue
		}
		checked++
		if strings.Contains(css, h.Name) {
			t.Errorf("the stylesheet selects on %s — it is a test hook and must stay style-free (%s)", h.Name, h.Why)
		}
	}
	if checked == 0 {
		t.Fatal("fixture defect: no hook was style-checked, so a clean pass here means nothing")
	}
}

// --- print: what may and may not be hidden ---

// One @media print block. This USED to be load-bearing for the wrong
// reason: the old printBlock() helper scanned only the first block, so a
// second one was an unscanned place to hide .incomplete-warn, .away or
// .not-tracked with the F1 guard still green. The parser now reads every
// print block and resolves the cascade across all of them, so that hole is
// closed at the mechanism rather than by this count.
//
// The test is KEPT, and it is kept deliberately rather than deleted with
// its reason: one block is the house rule that makes the print path
// reviewable in one place, and the assertion below now says so instead of
// standing in for a guard. Both halves are asserted — the count, and (the
// part that actually matters now) that every print declaration in the sheet
// really is inside that one block.
func TestTheStylesheetHasExactlyOnePrintBlock(t *testing.T) {
	srv, _ := newMusterTestServer(t, 0)
	body := rawPage(t, srv, "/muster/print")
	css := stylesheetOf(t, body)
	if got := strings.Count(css, "@media print"); got != 1 {
		t.Fatalf("stylesheet has %d @media print blocks, want exactly 1 — the print path must be reviewable in one place", got)
	}
	// Precondition made explicit: the parser must actually be finding print
	// rules, or "they are all in one block" is true of nothing.
	if got := len(printRules(paperStylesheet(t, body))); got < 5 {
		t.Fatalf("the parser found only %d rules inside @media print — this test is not looking at the print path", got)
	}
}

// F1, and U3's third category. .incomplete-warn and .not-tracked are
// deliberately excluded from the chrome-hiding rule: a printed roster that
// may be short people, or that omits the people nothing is watching, must
// say so on the paper itself. Under D-6 (100% accountability — an OSHA rule
// applied on the Slope, enforced for insurance reasons, with no regulation
// number) both are accountability instruments — they are about people this
// operation IS responsible for.
//
// INVERTED for .away on 2026-08-21. This test used to name all three in one
// loop, which encoded the assumption the ruling overturned: that "the sheet
// must carry it" follows from "the system knows it". James, who works this
// site and owns the liability question, ruled that people away at an
// uncontrolled destination are tracked here but are the destination
// operator's responsibility, not this operation's, and must not be printed.
// So .away is now asserted HIDDEN, and the two questions are deliberately
// asserted apart rather than in one list — lumping them together is what
// made the wrong answer look obvious.
func TestPrintNeverHidesTheExceptionSectionsThisOperationOwns(t *testing.T) {
	srv, _ := newMusterTestServer(t, 0)
	rules := paperStylesheet(t, rawPage(t, srv, "/muster/print"))
	// Precondition: the print path really does hide SOMETHING, or "nothing
	// hides these" is true by looking at the wrong stylesheet.
	if _, hidden := hiddenOnPaper(rules, "topbar"); !hidden {
		t.Fatal("precondition: print hides nothing at all — this test is not looking at the chrome-hiding rules")
	}
	// All four hiding forms, resolved through the cascade — not one spelling
	// of one property on one line.
	for _, class := range []string{"incomplete-warn", "not-tracked"} {
		if why, hidden := hiddenOnPaper(rules, class); hidden {
			t.Fatalf("print hides .%s — it is about people THIS operation is responsible for, and the paper itself must carry it. Resolved as %s", class, why)
		}
	}
}

// The other half of the 2026-08-21 ruling, asserted as its own question
// because it IS its own question: the away names must not reach paper.
//
// AMENDED by James, 2026-10-01: print = roster only (no sync verdicts, no
// counts/summary pages). ⟦was: TestPrintHidesTheAwayNamesAndPrintsTheScopeLineInstead⟧
// — it required the count-only stand-in (.away-scope) to reach paper, so the
// sheet stated its responsibility boundary. That stand-in is a count, so
// it is now asserted OFF the paper as well; the block is still rendered
// (inert — screen-hidden and paper-hidden, kept for the day a ruling puts
// the boundary back) and must still never carry a name.
func TestPrintHidesTheAwayNamesAndTheAwayCount(t *testing.T) {
	// A fixture with somebody ACTUALLY away at a configured pad: the scope
	// line only has anything to disclaim when the category is populated,
	// and a stylesheet-only assertion would pass on a sheet with no away
	// people on it at all.
	srv := musterWithAllThreeCategories(t)
	body := rawPage(t, srv, "/muster/print")
	rules := paperStylesheet(t, body)

	// Precondition: this stylesheet can express "not hidden" at all, or the
	// assertion below is true of a sheet with no print rules in it.
	if why, hidden := hiddenOnPaper(rules, "incomplete-warn"); hidden {
		t.Fatalf("precondition: .incomplete-warn is hidden on paper (%s) — this test is not reading a working print cascade", why)
	}
	if _, hidden := hiddenOnPaper(rules, "away"); !hidden {
		t.Fatalf("print does not hide .away — Conoco's people at Conoco's pad would be named on this operation's evacuation sheet.\nprint block was:\n%s", printBlockText(rules))
	}
	if _, hidden := hiddenOnPaper(rules, "away-scope"); !hidden {
		t.Fatalf("print does NOT hide .away-scope — James, 2026-10-01: print = roster only, and the away stand-in is a count.\nprint block was:\n%s", printBlockText(rules))
	}
	// Never on screen either: the named list is right there, and two
	// overlapping statements of the same fact is how the categories on this
	// page got blurred before.
	if got, ok := screenClassValue(rules, "away-scope", "display"); !ok || strings.TrimSpace(got) != "none" {
		t.Errorf("on screen .away-scope resolves display=%q (found=%v), want none", got, ok)
	}
	// The (inert) block keeps saying the two things it exists to say — how
	// many, and whose they are — so it is correct the day it prints again.
	if !strings.Contains(body, `data-muster-section="away-scope"`) {
		t.Fatalf("no away-scope block rendered at all:\n%s", body)
	}
	if !strings.Contains(body, "not this operation") {
		t.Fatal("the away-scope block never disclaims responsibility in words — a count with no reason attached reads as a defect in the sheet")
	}

	// The part James actually ruled on: no names in the stand-in. Read the
	// block itself, not the page — a page-wide scan would pass simply
	// because the hidden screen panel is elsewhere in the markup.
	at := strings.Index(body, `data-muster-section="away-scope"`)
	scope := body[at:]
	if end := strings.Index(scope, "</section>"); end > 0 {
		scope = scope[:end]
	}
	for _, name := range []string{"Pat Passenger", "Conneco Pad"} {
		if strings.Contains(scope, name) {
			t.Errorf("the away-scope block names %q — were it ever to print again, this would put an uncontrolled-destination name straight back onto the paper", name)
		}
	}
	// ...but it must carry the COUNT, which is the fact the sheet keeps.
	if !strings.Contains(scope, ">1 departed") {
		t.Errorf("the away-scope block does not state how many people it is disclaiming; block was:\n%s", scope)
	}
}

// Colour cannot carry a distinction on a one-colour sheet, and colour-
// blindness is not controlled for here. The categories that reach paper must
// therefore differ in BOX FORM: not-tracked is dashed ("nothing is watching"
// as a shape), and the panels that state something the sheet KNOWS are solid.
//
// RE-POINTED 2026-08-21. This used to compare .away against .not-tracked,
// which is no longer a question the paper can answer: .away is hidden on
// paper by James's ruling, so `paperClassValue(rules, "away", "border")`
// now resolves to the SCREEN box and comparing it would assert nothing
// about the sheet. The pairing that matters on paper is now the away SCOPE
// LINE (solid — a statement the sheet is making) against not-tracked
// (dashed — a gap in what it knows). Losing that distinction is the same
// defect this test was always about: a knowledge gap that reads as a
// recorded status.
//
// RE-POINTED AGAIN by James, 2026-10-01: print = roster only (no sync
// verdicts, no counts/summary pages). The away scope line no longer prints
// (it is a count), so its border is no longer a paper fact. The solid
// panel paired against not-tracked is now .on-road — a STATEMENT the sheet
// makes about named people this post dispatched — alongside the F1 box.
func TestPrintDistinguishesTheOffSiteCategoriesByFormNotColour(t *testing.T) {
	srv, _ := newMusterTestServer(t, 0)
	rules := paperStylesheet(t, rawPage(t, srv, "/muster/print"))

	// Precondition: .away really is off the paper, or the pairing below is
	// the wrong pairing and this test is measuring a sheet that no longer
	// exists.
	if _, hidden := hiddenOnPaper(rules, "away"); !hidden {
		t.Fatal("precondition: .away is NOT hidden on paper — the named away list is back on the sheet and this test is comparing the wrong two boxes")
	}

	// The RESOLVED border for each category on paper — the last rule that
	// wins the cascade, not the first line whose text happens to match.
	// `strings.Index(block, sel+" {")` could not see a later override, nor a
	// wrapped selector list, nor a value set from outside the print block.
	border := func(class string) string {
		v, ok := paperClassValue(rules, class, "border")
		if !ok {
			t.Fatalf("no border resolves for .%s on paper at all — that category would print as an unmarked panel", class)
		}
		return v
	}
	// Precondition: the panel paired below really is on the paper, or the
	// comparison is between a printed box and a screen-only one.
	if why, hidden := hiddenOnPaper(rules, "on-road"); hidden {
		t.Fatalf("precondition: .on-road is hidden on paper (%s) — this test would be comparing against a box that never prints", why)
	}
	scope, notTracked, incomplete := border("on-road"), border("not-tracked"), border("incomplete-warn")
	if !strings.Contains(scope, "solid") {
		t.Errorf(".on-road does not print a solid box; border resolves to %q", scope)
	}
	if !strings.Contains(incomplete, "solid") {
		t.Errorf(".incomplete-warn does not print a solid box; border resolves to %q", incomplete)
	}
	if !strings.Contains(notTracked, "dashed") {
		t.Errorf(".not-tracked does not print a dashed box — on paper it would be indistinguishable from a recorded status; border resolves to %q", notTracked)
	}
	if strings.Contains(notTracked, "solid") {
		t.Errorf(".not-tracked prints a solid box like the panels that state what the sheet knows; border resolves to %q", notTracked)
	}
	// The distinction is only real if they differ. Asserting each side
	// separately would pass if all three ever became dashed.
	if notTracked == scope || notTracked == incomplete {
		t.Errorf(".not-tracked prints the SAME box as a solid panel (%q) — the one distinction that survives onto a one-colour sheet is gone", notTracked)
	}
}

// --- print: what the paper actually says ---

// musterWithAllThreeCategories puts one person on site, one away at a
// configured pad, one dropped off en route (not tracked) and one unfinished
// check-in, so a single rendered sheet carries every category at once. That
// combination is the whole point: the sections are only capable of blurring
// when they are all on the page together.
func musterWithAllThreeCategories(t *testing.T) *Server {
	t.Helper()
	srv, w := newDestinationsTestServer(t)

	// On site: Dana walks in at site-b and stays.
	sendPatToThePad(t, w) // Dana on site, Pat away at Conneco Pad

	// Not tracked: a third person rides a controlled run to site-a and is
	// marked not aboard on arrival.
	third, err := domain.CreatePerson(w.st, "site-b", 1, "Sam Sidedrop", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// A driver who is NOT the on-site returnee: dispatching Dana again would
	// check Dana out of this site and leave the on-site count at zero, and
	// this fixture needs all four categories populated at once.
	runner, err := domain.CreatePerson(w.st, "site-b", 1, "Rob Runner", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	dest, dl := "site-a", int64(60)
	trip, err := domain.Dispatch(w.st, "site-b", 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "BUS 9", DriverPersonID: runner.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.st, "site-b", "BUS 9"), DriverPersonID: runner.PersonID,
		OccupantPersonIDs: []string{third.PersonID},
		DestinationSite:   &dest, DeadlineMinutes: &dl,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.RecordExpectedArrivalExcluding(w.st, "site-a", 1, trip.TripID,
		[]string{third.PersonID}, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}

	// Unfinished check-in (row 31's crash window).
	late, err := domain.CreatePerson(w.st, "site-b", 1, "Lee Late", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	crashCheckIn(t, w.st, "site-b", "TRUCK 9", late.PersonID)
	return srv
}

// The categories mean different things and must be legible as different
// things on paper: each gets its OWN section (never one nested inside the
// other's box), and each states in WORDS what it means.
func TestPrintedSheetKeepsTheThreeCategoriesDistinct(t *testing.T) {
	srv := musterWithAllThreeCategories(t)
	body := rawPage(t, srv, "/muster/print")

	// Precondition: the fixture really did produce all three, or "they are
	// distinct" is true because two of them are missing.
	for _, sec := range []string{"on-site", "away", "not-tracked", "incomplete"} {
		if !strings.Contains(body, `data-muster-section="`+sec+`"`) {
			t.Fatalf("precondition: the sheet has no %q section, so this test proves nothing about them staying distinct:\n%s", sec, body)
		}
	}

	// Each off-site category is its own section, not a sub-heading of the
	// other. Before U3 the not-tracked table rendered INSIDE class="away",
	// under a heading that said "Away at uncontrolled destinations" — a
	// knowledge gap presented as a recorded status.
	awayAt := strings.Index(body, `data-muster-section="away"`)
	ntAt := strings.Index(body, `data-muster-section="not-tracked"`)
	awayClose := strings.Index(body[awayAt:], "</section>")
	if awayClose < 0 {
		t.Fatal("the away section is not closed")
	}
	if ntAt > awayAt && ntAt < awayAt+awayClose {
		t.Fatalf("the not-tracked section is nested INSIDE the away section — the two categories are blurred:\n%s", body)
	}

	// ...and each says, in words that survive one-colour printing, what it
	// means. No assertion here depends on a colour.
	for _, want := range []string{
		// M-16 re-pointed this from "AWAY — NOT on site, not in the total
		// above". The old sentence claimed a disjointness the domain
		// deliberately refuses (OutAtUncontrolled's documented asymmetry),
		// so the sheet contradicted itself in a reachable state. The
		// assertion is not weakened — it still pins the away panel's
		// category word to an exact sentence; it now pins a true one.
		"AWAY — out at a pad, counted separately from the total above",
		// 2026-08-21: the paper's own away category word. The tag above
		// belongs to the screen panel (still in this markup, hidden by the
		// print block); this one is what a reader holding the sheet sees,
		// and it has to name the boundary, not just the category.
		"AWAY — NOT THIS OPERATION’S RESPONSIBILITY",
		"NOT TRACKED — whereabouts not recorded, nothing is watching",
		"MAY BE INCOMPLETE",
		"not an error",       // not tracked is routine...
		"no timer will fire", // ...and unwatched. Both halves.
		// RE-POINTED 2026-08-21 from "account for these people by radio".
		// That exact phrase lived in the AWAY section's note, which no
		// longer reaches paper — and the away tally note no longer says it
		// either, because instructing the sheet's reader to radio for
		// somebody else's crew is the duty James's ruling disclaims. The
		// not-tracked note is where the radio instruction belongs now, and
		// it is the one that must survive: those people are still ours.
		"Account for them by radio",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the printed sheet does not say %q:\n%s", want, body)
		}
	}

	// The headcount states all three, and only the on-site one is the total.
	for _, want := range []string{
		`data-tally-count="on-site">1<`,
		`data-tally-count="away">1<`,
		`data-tally-count="not-tracked">1<`,
		`data-tally-count="incomplete">1<`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the headcount block is missing %s:\n%s", want, body)
		}
	}
	// Nobody appears twice: an away or untracked person on the roster would
	// be counted as present during an evacuation.
	for _, person := range []string{"Pat Passenger", "Sam Sidedrop"} {
		if got := strings.Count(body, person); got != 1 {
			t.Fatalf("%s appears %d times on the sheet, want exactly 1 (their own category only)", person, got)
		}
	}
}

// Reading order IS the design: the incomplete warning qualifies the
// headcount, the headcount answers "who is on site", the roster backs it,
// and the exceptions follow. On paper there is no scrolling back.
func TestPrintedSheetReadsInSafetyOrder(t *testing.T) {
	srv := musterWithAllThreeCategories(t)
	body := rawPage(t, srv, "/muster/print")

	order := []string{"incomplete", "tally", "on-site", "off-site", "away", "not-tracked"}
	prev := -1
	for _, sec := range order {
		at := strings.Index(body, `data-muster-section="`+sec+`"`)
		if at < 0 {
			t.Fatalf("printed sheet has no %q section:\n%s", sec, body)
		}
		if at < prev {
			t.Fatalf("section %q reads out of order — the sheet must run: %v", sec, order)
		}
		prev = at
	}
}

// A sheet on a table during an evacuation must be able to say which sheet it
// is: which site, WHICH KIND OF SHEET, and when it was produced.
//
// V-02 strengthened this from two routes to THREE, and the third is the one
// the finding was about. /muster/print with no ?visits= used to fall
// through to Mode "reprint" — so a fresh snapshot stamped itself REPRINT
// and carried the reprint's promise that it "reproduces the same roster no
// matter when it is printed again", which is false on a route that
// re-queries the open visits on every GET.
//
// The map below is the pin. It may be strengthened; it may NOT be relaxed
// to make a mode change pass. This test's own comment already stated the
// principle the two-mode scheme violated.
func TestEverySheetIdentifiesItselfOnPaper(t *testing.T) {
	srv, st := newMusterTestServer(t, 0)
	// A person on site, so the snapshot route has a visit set to name and
	// its self-minted reprint link is a real one rather than empty.
	co, err := domain.CreateCompany(st, "site-a", 1, "Kakivik", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	p, err := domain.CreatePerson(st, "site-a", 1, "Dana Driver", &co.CompanyID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	checkIn(t, st, "site-a", p.PersonID, nil)

	// The third route needs a real ?visits= set, taken from the snapshot's
	// own self-minted link — which is also the proof that the link exists.
	snap := rawPage(t, srv, "/muster/print")
	href := regexp.MustCompile(`href="(/muster/print\?visits=[^"]+)"`).FindStringSubmatch(snap)
	if href == nil {
		t.Fatalf("the fresh-snapshot sheet does not mint its own reprint link — whoever is holding the paper has no way to ask for this exact sheet again, and the live page's link names a different set taken at a different moment:\n%s", snap)
	}
	reprintPath := strings.ReplaceAll(href[1], "&amp;", "&")

	for path, mode := range map[string]string{
		"/muster":       "LIVE",
		"/muster/print": "PRINTED FROM LIVE",
		reprintPath:     "REPRINT",
	} {
		body := rawPage(t, srv, path)
		if !strings.Contains(body, `class="sheet-runner"`) {
			t.Fatalf("%s carries no per-page sheet identity:\n%s", path, body)
		}
		runner := body[strings.Index(body, `class="sheet-runner"`):]
		runner = runner[:strings.Index(runner, "</div>")]
		for _, want := range []string{srv.Cfg.SiteName("site-a") /* James, 2026-10-04 (cluster I): display name ⟦was: "site-a"⟧ */, mode} {
			if !strings.Contains(runner, want) {
				t.Fatalf("%s: the printed sheet identity %q does not name %q", path, runner, want)
			}
		}
		// M-05 (test-efficacy S1-10). This used to be
		// `strings.Contains(body, "class=\"sheet-time\"")` and nothing
		// more: deleting {{.RenderedAt}} from ALL THREE renderings left an
		// empty span, the class still present, and the whole suite green.
		// The failure that recurs behind that hole is U3's own
		// problem-as-found item 4 — TWO SHEETS ON A TABLE AND NO WAY TO
		// TELL WHICH IS CURRENT — and V-02 sharpened it: with three modes
		// there are three stamps, and a sheet that says PRINTED FROM LIVE
		// beside a blank time is worse than one with no masthead at all,
		// because it looks stamped.
		//
		// So the stamp is asserted to BE THE RENDER TIME, not to exist. A
		// literal date frozen into the template fails this as surely as an
		// empty span does, which the class check could never distinguish.
		stampRe := regexp.MustCompile(`<span class="sheet-time">([^<]*)</span>`)
		sm := stampRe.FindStringSubmatch(body)
		if sm == nil {
			t.Fatalf("%s does not stamp the sheet with a time:\n%s", path, body)
		}
		stamp := strings.TrimSpace(sm[1])
		// The prose that qualifies the stamp differs per mode ("as at",
		// "snapshot taken", "sheet produced") and is deliberately not
		// asserted here — what must hold is that a real render time
		// follows it.
		if !stampNamesRenderTime(stamp) {
			t.Fatalf("%s: the %s sheet's time stamp reads %q, which is not this render's time. Two sheets on a table with no way to tell which is current is the exact failure the stamp exists to prevent — an empty or frozen stamp looks authoritative and is not", path, mode, stamp)
		}
		// The masthead carries it too: it is the line that survives onto
		// paper at the top of page 1, and it is the one a reader holding
		// the sheet actually looks at.
		if !stampNamesRenderTime(runner) {
			t.Fatalf("%s: the printed masthead %q carries no render time", path, runner)
		}
	}
	// The runner is print-only chrome on screen, so it must NOT be swept up
	// by the chrome-hiding rule that removes the top bar on paper.
	rules := paperStylesheet(t, rawPage(t, srv, "/muster/print"))
	if why, hidden := hiddenOnPaper(rules, "sheet-runner"); hidden {
		t.Fatalf("print hides the sheet identity — a sheet on a table could not say which sheet it is. Resolved as %s", why)
	}
}

// The stamp format is pinned: a bare 14:32 on a sheet carried between sites
// is ambiguous, so the zone is part of it.
func TestSheetStampCarriesTheDateAndTheZone(t *testing.T) {
	got := musterSheetStamp(time.Date(2026, 8, 20, 14, 32, 0, 0, time.FixedZone("AKDT", -8*3600)))
	if got != "2026-08-20 14:32 AKDT" {
		t.Fatalf("sheet stamp = %q, want %q", got, "2026-08-20 14:32 AKDT")
	}
}

// --- the derived headcounts ---

// len(Away) counts PADS and len(NotTracked) counts TRIPS. Neither is a count
// of people, and a muster that prints one as if it were the other is a quiet
// wrong number on a life-safety document.
func TestOffSiteHeadcountsCountPeopleNotPadsOrTrips(t *testing.T) {
	const wantAway, wantNotTracked = 5, 2
	d := musterData{
		Away: []awayPadRow{
			{Pad: "Conneco Pad", Persons: []awayPerson{{Name: "A"}, {Name: "B"}, {Name: "C"}}},
			{Pad: "Empty Pad"}, // configured, holds nobody
			{Pad: "Far Pad", Persons: []awayPerson{{Name: "D"}, {Name: "E"}}},
		},
		// ⟦was: []domain.NotTrackedRow{{Vehicle: …}} — the sheet now carries
		// the display-resolved row (fix round 3, FINDINGS-R5 F3a/F10); the
		// count under test is unchanged⟧
		NotTracked: []notTrackedRow{
			{NotTrackedRow: domain.NotTrackedRow{Vehicle: "BUS 7", Persons: []domain.OccupantRef{{Name: "F"}, {Name: "G"}}}},
		},
	}
	// Precondition (rule 6). The fixture has to make a row count and a
	// person count DIFFERENT numbers, or `len(rows)` and `sum(len(people))`
	// agree by coincidence and the test cannot tell them apart. An earlier
	// version of this fixture had 3 pads holding 3 people and passed a
	// mutation that counted pads — caught by mutation testing, fixed here.
	if len(d.Away) == wantAway {
		t.Fatalf("precondition: %d pads and %d away people are the same number — a pad count would pass this test", len(d.Away), wantAway)
	}
	if len(d.NotTracked) == wantNotTracked {
		t.Fatalf("precondition: %d not-tracked trips and %d people are the same number — a trip count would pass this test", len(d.NotTracked), wantNotTracked)
	}

	d.countOffSite()
	if d.AwayCount != wantAway {
		t.Errorf("AwayCount = %d, want %d people across %d pads (one of them empty)", d.AwayCount, wantAway, len(d.Away))
	}
	if d.NotTrackedCount != wantNotTracked {
		t.Errorf("NotTrackedCount = %d, want %d people on %d trip", d.NotTrackedCount, wantNotTracked, len(d.NotTracked))
	}
	// Recounting must not accumulate: both handlers call this on a value
	// they have just filled, and a += over a stale field would inflate the
	// number a person reads off the paper.
	d.countOffSite()
	if d.AwayCount != wantAway || d.NotTrackedCount != wantNotTracked {
		t.Errorf("counting twice inflated the headcounts to %d/%d", d.AwayCount, d.NotTrackedCount)
	}
}

// The headcount block says what it knows, INCLUDING that it knows of nobody
// — F1's lesson, applied to the two off-site categories. A blank where a
// zero belongs reads as "not checked", not as "nobody".
func TestHeadcountReportsZeroesRatherThanOmittingACategory(t *testing.T) {
	srv, st := newMusterTestServer(t, 0)
	p, err := domain.CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	checkIn(t, st, "site-a", p.PersonID, nil)

	for _, path := range []string{"/muster", "/muster/print"} {
		body := rawPage(t, srv, path)
		for _, want := range []string{
			`data-tally-count="on-site">1<`,
			`data-tally-count="away">0<`,
			`data-tally-count="not-tracked">0<`,
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s: headcount missing %s:\n%s", path, want, body)
			}
		}
		// Nothing is unfinished, so that row is absent rather than a zero:
		// an "0 check-ins unfinished" row on a clean sheet is noise, and the
		// warning box it points at is not there to be read.
		if strings.Contains(body, `data-tally-count="incomplete"`) {
			t.Fatalf("%s: an unfinished-check-in row appeared with nothing unfinished:\n%s", path, body)
		}
	}
}

// An all-folded muster (every crew below the minimum size) has an empty
// Companies list and a non-empty Summary. The sheet must not then claim
// nobody is on site — and must not print an empty roster section either.
func TestAnAllFoldedMusterNeverClaimsNobodyIsOnSite(t *testing.T) {
	srv, st := newMusterTestServer(t, 3)
	co, err := domain.CreateCompany(st, "site-a", 1, "Small Crew Co", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	p, err := domain.CreatePerson(st, "site-a", 1, "Solo Worker", &co.CompanyID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	checkIn(t, st, "site-a", p.PersonID, nil)

	body := rawPage(t, srv, "/muster/print")
	if got := companyGroups(body); len(got) != 0 {
		t.Fatalf("precondition: the crew did not fold (company groups %v), so the all-folded case is not posed", got)
	}
	if !strings.Contains(body, "Solo Worker") {
		t.Fatalf("the folded person is not on the sheet at all:\n%s", body)
	}
	if strings.Contains(body, "Nobody recorded on site") {
		t.Fatalf("the sheet claims nobody is on site while listing a folded crew:\n%s", body)
	}
	if !strings.Contains(body, `data-tally-count="on-site">1<`) {
		t.Fatalf("the folded crew is not in the on-site total:\n%s", body)
	}
}

// A genuinely empty muster still says so — affirmatively, not by rendering
// nothing.
func TestAnEmptyMusterSaysNobodyIsOnSite(t *testing.T) {
	srv, _ := newMusterTestServer(t, 0)
	body := rawPage(t, srv, "/muster/print")
	if !strings.Contains(body, "Nobody recorded on site") {
		t.Fatalf("an empty muster does not say so:\n%s", body)
	}
	if !strings.Contains(body, `data-tally-count="on-site">0<`) {
		t.Fatalf("an empty muster does not report a zero headcount:\n%s", body)
	}
}

// The away section is rendered from CONFIGURED destinations, so a site with
// none of them still prints a coherent sheet rather than a dangling
// "Not on site" heading over nothing.
func TestASiteWithNoUncontrolledDestinationsPrintsNoEmptyOffSiteHeading(t *testing.T) {
	srv, _ := newMusterTestServer(t, 0)
	srv.Cfg.Destinations = []config.Destination{{ID: "site-b", DisplayName: "Pad Site Control", Controlled: true}}
	body := rawPage(t, srv, "/muster/print")
	if strings.Contains(body, `data-muster-section="off-site"`) {
		t.Fatalf("a site with no uncontrolled destinations rendered an empty Not-on-site section:\n%s", body)
	}
	// The headcount still states both categories, so the sheet is never
	// silent about them.
	if !strings.Contains(body, `data-tally-count="away">0<`) {
		t.Fatalf("the headcount dropped the away category entirely:\n%s", body)
	}
}

// --- V-02: no sheet may carry a guarantee it cannot honour ------------

// THE DEFECT. `/muster/print` with no ?visits= re-queries this site's open
// visits on every GET, so two GETs five minutes apart produce two different
// rosters. Both branches of handleMusterPrint nonetheless fell through to
// one `Mode: "reprint"`, so that route stamped the sheet REPRINT and
// printed the reprint's copy:
//
//	"This reproduces the same roster no matter when it is printed again."
//
// It is false there, and — the part that makes it a print regression rather
// than a screen one — it PRINTS. The print block hides only .topbar,
// .banner-notice, .actions, nav.screen-only and .pagenav, and merely
// de-chromes the mode boxes; .mode-detail is explicitly re-coloured INSIDE
// the print block, which is proof rather than inference that it lands on
// paper. A false completeness guarantee on the one document read aloud
// during an evacuation is exactly what "the muster must never carry a
// guarantee it cannot honour" forbids.
//
// This asserts the claim's placement AND that placement is resolved through
// the print cascade, so "it does not print" cannot become true by accident
// and false again by a stylesheet edit.
func TestThePointInTimeGuaranteeAppearsOnlyOnASheetThatCanHonourIt(t *testing.T) {
	srv, st := newMusterTestServer(t, 0)
	co, err := domain.CreateCompany(st, "site-a", 1, "Kakivik", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	p, err := domain.CreatePerson(st, "site-a", 1, "Dana Driver", &co.CompanyID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	checkIn(t, st, "site-a", p.PersonID, nil)

	// ⟦LOCK amendment — James, 2026-10-03 (M-5 / M-19)⟧ ⟦was: `const guarantee =
	// "reproduces the same roster no matter when it is printed again"`⟧ That
	// sentence was FALSE on the one route that carried it: a visit closed
	// after the sheet was minted vanished from the reprint (buildMuster lists
	// open visits) — MEASURED on L10, M-19, twice. The ruling keeps the route
	// and replaces the promise with what the route can honour: the SAME
	// VISITS are named every time, with the departed listed as such. The
	// property this test proves is unchanged — only the reprint route may
	// carry the point-in-time claim — and the pinned literal is the new true
	// sentence's load-bearing clause.
	const guarantee = "names the same visits every time it is printed"
	// The copy wraps across source lines, so compare on whitespace-collapsed
	// text: a sentence that broke differently would otherwise read as
	// absent, and this test would pass by not looking.
	flat := func(s string) string { return regexp.MustCompile(`\s+`).ReplaceAllString(s, " ") }

	live := flat(rawPage(t, srv, "/muster"))
	snapshot := rawPage(t, srv, "/muster/print")

	// Precondition: the sentence must exist somewhere, or this test passes
	// by the copy simply having been deleted.
	href := regexp.MustCompile(`href="(/muster/print\?visits=[^"]+)"`).FindStringSubmatch(snapshot)
	if href == nil {
		t.Fatalf("the snapshot sheet mints no reprint link:\n%s", snapshot)
	}
	reprint := rawPage(t, srv, strings.ReplaceAll(href[1], "&amp;", "&"))
	if !strings.Contains(flat(reprint), guarantee) {
		t.Fatalf("precondition: the reprint route does not carry the point-in-time guarantee at all, so this test proves nothing:\n%s", reprint)
	}

	// THE ASSERTION. Only the route that was handed an explicit visit set
	// may make the claim.
	if strings.Contains(flat(snapshot), guarantee) {
		t.Fatal("/muster/print (no ?visits=) claims it \"names the same visits every time it is printed\". That route calls openVisitsAtThisSite() fresh on every GET, so the claim is false — and it prints, onto the sheet read aloud during an evacuation.")
	}
	if strings.Contains(live, guarantee) {
		t.Fatal("/muster (live) claims to name the same visits every time — the live board changes as people check in and out")
	}
	// James, 2026-10-03: the OLD sentence must be gone from every route — it
	// was false on the reprint (the departed vanished) and is the one this
	// amendment replaces. A later edit restoring it reds here.
	const falsePromise = "reproduces the same roster no matter when it is printed again"
	for route, body := range map[string]string{"/muster": live, "/muster/print": flat(snapshot), "reprint": flat(reprint)} {
		if strings.Contains(body, falsePromise) {
			t.Errorf("%s carries %q — the sentence M-19 measured as false (a check-out after minting silently shrank the reprint); James, 2026-10-03: replaced with true text", route, falsePromise)
		}
	}

	// Each sheet must say which KIND it is, and no two may share a label.
	labels := map[string]string{"/muster": "LIVE", "/muster/print": "PRINTED FROM LIVE", "reprint": "REPRINT"}
	bodies := map[string]string{"/muster": live, "/muster/print": snapshot, "reprint": reprint}
	for route, want := range labels {
		runner := bodies[route]
		i := strings.Index(runner, `class="sheet-runner"`)
		if i < 0 {
			t.Fatalf("%s carries no sheet identity", route)
		}
		runner = runner[i : i+strings.Index(runner[i:], "</div>")]
		// The masthead reads "Muster · <site> · <LABEL> · <time>", so the
		// label is compared as a whole delimited field. A plain substring
		// check would report "PRINTED FROM LIVE" as also saying "LIVE".
		if !strings.Contains(runner, "· "+want+" ·") {
			t.Errorf("%s stamps itself %q, want the label %q", route, runner, want)
		}
		for other, label := range labels {
			if other != route && strings.Contains(runner, "· "+label+" ·") {
				t.Errorf("%s's sheet identity also says %q — two kinds of sheet must never be confusable on a table", route, label)
			}
		}
	}

	// The snapshot must say what it CAN say: which moment it froze.
	if !strings.Contains(snapshot, "snapshot taken") {
		t.Error("the fresh-snapshot sheet does not say which moment it froze — that is the one guarantee it CAN make")
	}

	// RECONSIDERED DELIBERATELY, as this guard asked — James, 2026-10-01:
	// print = roster only (no sync verdicts, no counts/summary pages). The
	// mode copy used to reach paper, which is why the sentence mattered on
	// the sheet. It is now SCREEN-ONLY: it lives inside .sheet-head, and the
	// print block hides .sheet-head whole. The guarantee still must not be
	// claimed by the wrong route on screen (asserted above); on paper the
	// runner line's label (LIVE / PRINTED FROM LIVE / REPRINT) is what says
	// which kind of sheet it is.
	//
	// Asserted as the ancestor that actually does the hiding: a class-level
	// check on .mode-detail alone reads "not hidden" (no rule names it) and
	// would have kept claiming the copy prints after the ruling — measured,
	// it did.
	rules := paperStylesheet(t, reprint)
	head := regexp.MustCompile(`(?s)<header class="sheet-head">.*?</header>`).FindString(reprint)
	if !strings.Contains(head, `class="mode-detail"`) {
		t.Fatalf("precondition: the mode copy is not inside .sheet-head any more — re-derive whether it reaches paper:\n%s", head)
	}
	if _, hidden := hiddenOnPaper(rules, "sheet-head"); !hidden {
		t.Fatalf("print does NOT hide .sheet-head — the mode copy is back on the roster-only sheet (James, 2026-10-01).\nprint block was:\n%s", printBlockText(rules))
	}
	if why, hidden := hiddenOnPaper(rules, "sheet-runner"); hidden {
		t.Fatalf("print hides .sheet-runner (%s) — with .sheet-head hidden too, nothing on paper would say which kind of sheet this is", why)
	}
}

// The fresh-snapshot sheet must mint its OWN reprint link, from the set it
// actually rendered — not inherit the live board's, which names a different
// set taken at a different moment. Without this, V-02's fix would DESTROY
// the reprint mechanism rather than relocate it.
func TestTheSnapshotSheetMintsAReprintLinkForTheSheetThatActuallyPrinted(t *testing.T) {
	srv, st := newMusterTestServer(t, 0)
	co, err := domain.CreateCompany(st, "site-a", 1, "Kakivik", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	first, err := domain.CreatePerson(st, "site-a", 1, "Dana Driver", &co.CompanyID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	checkIn(t, st, "site-a", first.PersonID, nil)

	snapshot := rawPage(t, srv, "/muster/print")
	href := regexp.MustCompile(`href="(/muster/print\?visits=[^"]+)"`).FindStringSubmatch(snapshot)
	if href == nil {
		t.Fatalf("no self-minted reprint link on the snapshot sheet:\n%s", snapshot)
	}
	frozen := strings.ReplaceAll(href[1], "&amp;", "&")
	if !strings.Contains(snapshot, "Dana Driver") {
		t.Fatal("precondition: the snapshot did not render the person on site")
	}

	// Somebody else checks in AFTER the sheet came off the printer.
	second, err := domain.CreatePerson(st, "site-a", 1, "Pat Passenger", &co.CompanyID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	checkIn(t, st, "site-a", second.PersonID, nil)

	// The frozen link must still reproduce the sheet that printed: Dana
	// only. This is the whole reprint guarantee, and it is what makes the
	// snapshot's claim about itself true.
	again := rawPage(t, srv, frozen)
	if !strings.Contains(again, "Dana Driver") {
		t.Fatal("the self-minted reprint link lost somebody who was on the printed sheet")
	}
	if strings.Contains(again, "Pat Passenger") {
		t.Fatal("the self-minted reprint link added somebody who checked in AFTER the sheet printed — a reprint must reproduce the paper, not the present")
	}
	// And a fresh snapshot now DOES include them, which is the difference
	// between the two modes stated as behaviour rather than as copy.
	if !strings.Contains(rawPage(t, srv, "/muster/print"), "Pat Passenger") {
		t.Fatal("a fresh snapshot did not pick up the new arrival — then it is not a snapshot of live at all")
	}
}

// V-02's third part. The print page's only control used to be
// `onclick="window.print()"`, which does nothing with scripting off — on a
// page whose entire purpose is to be printed, on contract-locked ground.
// TestEveryFormPostsToARealActionWithoutJavaScript sweeps <form> tags and
// could never see it.
func TestThePrintSheetOffersAPrintControlThatWorksWithoutJavaScript(t *testing.T) {
	srv, _ := newMusterTestServer(t, 0)
	for _, path := range []string{"/muster/print"} {
		body := rawPage(t, srv, path)
		if !strings.Contains(body, `class="print-instruction"`) {
			t.Errorf("%s offers no print control that works with scripting off:\n%s", path, body)
		}
		// The one-click convenience must be served HIDDEN and revealed by
		// the script — the pattern U2 established and asserts elsewhere, so
		// a JS-off browser never sees a control that looks live and does
		// nothing.
		if !strings.Contains(body, `id="print-sheet" hidden`) {
			t.Errorf("%s serves its script-only Print button visible — with JS off it looks live and does nothing", path)
		}
		if strings.Contains(body, `onclick="window.print()"`) {
			t.Errorf("%s still carries an inline onclick print control — its only route to paper would be script-only", path)
		}
	}
}

// --- M-07 (the one surviving surface) ---------------------------------
//
// "State in form, never hue alone" is a stated criterion, and on the
// muster's three exception panels it is carried ENTIRELY by operator-facing
// prose: .incomplete-warn, .away and .not-tracked differ in CSS by hue
// (--critical-bg / --ice-bg / the dashed box) and by nothing else that a
// reader without colour vision can use. Their real distinction is the
// uppercase category tag each one leads with.
//
// Which would be fine — the criterion IS met that way — except that nothing
// asserted it. Reword any one of the three and the criterion died silently
// with the whole suite green, on the life-safety sheet, where the panels
// are adjacent and the reader may be holding a one-colour photocopy.
//
// R16 fences this out of the print block: the fix is NOT to add print
// styling. It is to make the prose that carries the distinction
// unrewordable by accident, on BOTH surfaces it has to survive.
func TestTheThreeExceptionPanelsAreDistinguishedByWordsOnScreenAndOnPaper(t *testing.T) {
	srv := musterWithAllThreeCategories(t)

	// Every panel's leading tag must be uppercase, distinct, and name the
	// thing the panel is about.
	type panel struct {
		section string
		must    []string // words that carry the MEANING, not just the shape
	}
	panels := []panel{
		{"incomplete", []string{"INCOMPLETE"}},
		{"away", []string{"AWAY", "out at a pad"}}, // M-16: see the re-pointing note above
		{"not-tracked", []string{"NOT TRACKED", "nothing is watching"}},
	}

	tagRe := regexp.MustCompile(`<span class="cat-tag[^"]*">([^<]*)</span>`)

	for _, route := range []string{"/muster", "/muster/print"} {
		body := styleRe.ReplaceAllString(rawPage(t, srv, route), "")
		seen := map[string]string{}
		for _, p := range panels {
			// Locate the panel by its structural hook, then read the tag
			// INSIDE it — a page-wide scan would let one panel's tag stand
			// in for another's.
			at := strings.Index(body, `data-muster-section="`+p.section+`"`)
			if at < 0 {
				t.Fatalf("%s: no %q section — this fixture is built to carry all three", route, p.section)
			}
			rest := body[at:]
			if end := strings.Index(rest, "</section>"); end > 0 {
				rest = rest[:end]
			}
			m := tagRe.FindStringSubmatch(rest)
			if m == nil {
				t.Fatalf("%s: the %q panel leads with no category tag. These three panels differ in CSS by HUE and by nothing a colour-blind reader or a one-colour photocopy can use — the uppercase word IS the distinction.", route, p.section)
			}
			tag := strings.TrimSpace(m[1])
			for _, want := range p.must {
				if !strings.Contains(tag, want) {
					t.Errorf("%s: the %q panel's tag is %q and no longer says %q — the word is the mechanism, not a caption on it", route, p.section, tag, want)
				}
			}
			// Distinct from the other two, and unmistakably a label.
			if prev, dupe := seen[tag]; dupe {
				t.Errorf("%s: the %q and %q panels carry the SAME tag %q — two categories that must never blur", route, p.section, prev, tag)
			}
			seen[tag] = p.section
			lead := strings.SplitN(tag, " ", 2)[0]
			if lead != strings.ToUpper(lead) {
				t.Errorf("%s: the %q panel's tag %q does not lead with an unmistakable uppercase word", route, p.section, tag)
			}
		}
		if len(seen) != 3 {
			t.Errorf("%s: %d distinct category tags across three panels", route, len(seen))
		}
	}

	// And the tags must survive onto paper — they are the half of the
	// distinction that does not depend on hue, so hiding them would leave
	// the panels differing by colour alone on a one-colour sheet.
	rules := paperStylesheet(t, rawPage(t, srv, "/muster/print"))
	for _, class := range []string{"cat-line", "cat-tag"} {
		if why, hidden := hiddenOnPaper(rules, class); hidden {
			t.Errorf("print hides .%s (%s) — on paper the three exception panels would differ by nothing but a box colour", class, why)
		}
	}
}

// stampNamesRenderTime reports whether text contains a muster sheet stamp
// (musterSheetStamp's format) naming the current minute. A one-minute
// window either side absorbs a render that straddles a minute boundary; it
// is deliberately NOT wider, because "some plausible timestamp" is what the
// class-name check already effectively accepted.
func stampNamesRenderTime(text string) bool {
	now := time.Now()
	for _, d := range []time.Duration{0, -time.Minute, time.Minute} {
		if strings.Contains(text, musterSheetStamp(now.Add(d))) {
			return true
		}
	}
	return false
}

// --- M-14: a split printed sheet never loses its category --------------

// TestEveryPrintedRosterBlockCarriesItsCategoryOnPaper.
//
// accessibility A7, measured: the printed muster runs to three pages, page 1
// ends with the heading "On site (17)" alone at its foot, and page 2 opens
// directly with a company name — 17 names with nothing on the page saying
// they are the on-site population rather than the away list. A sheet read
// aloud during an evacuation gets SPLIT BETWEEN READERS; the page-2 reader
// has names and no category.
//
// The fix is content, not styling, because both styling routes were
// measured and both fail (see muster.html's note: Firefox ignores
// break-after on headings, and a position:fixed running header prints on
// top of the names). So each roster block carries its own category label,
// screen-hidden and paper-shown.
//
// This asserts the property in the two places it can actually fail:
// the label must EXIST inside every block, and it must survive onto paper.
func TestEveryPrintedRosterBlockCarriesItsCategoryOnPaper(t *testing.T) {
	srv := musterWithAllThreeCategories(t)

	for _, route := range []string{"/muster", "/muster/print"} {
		body := styleRe.ReplaceAllString(rawPage(t, srv, route), "")

		// Every company block on the on-site roster.
		companies := regexp.MustCompile(`(?s)<div class="company" data-company-group="([^"]*)">(.*?)<h3`).FindAllStringSubmatch(body, -1)
		if len(companies) == 0 {
			t.Fatalf("%s: precondition — no company blocks rendered, so nothing is being checked:\n%s", route, body)
		}
		for _, c := range companies {
			if !strings.Contains(c[2], `<span class="page-cat-tag">ON SITE</span>`) {
				t.Errorf("%s: the company block %q carries no printed category label before its heading. On a sheet that breaks across pages this block can open page 2 with names and nothing saying they are the ON-SITE population", route, c[1])
			}
		}

		// Every pad block on the away list.
		//
		// 2026-08-21: this half is now asserting DORMANT markup, and says
		// so rather than pretending otherwise. The away list is hidden on
		// paper by James's ruling, so this label — screen-hidden and
		// paper-shown — currently renders nowhere. It is kept, with the
		// element, because it is correct the moment the away list ever
		// prints again, and because silently deleting a page-break
		// safeguard is how this project loses one. What this half proves
		// today is only that the safeguard has not rotted away unnoticed;
		// it no longer proves anything about what reaches paper. The
		// paper-side claim now lives in
		// TestPrintHidesTheAwayNamesAndTheAwayCount.
		pads := regexp.MustCompile(`(?s)<div class="pad" data-away-pad="([^"]*)">(.*?)<h4`).FindAllStringSubmatch(body, -1)
		if len(pads) == 0 {
			t.Fatalf("%s: precondition — no pad blocks rendered, so the away half is not being checked:\n%s", route, body)
		}
		for _, p := range pads {
			if !strings.Contains(p[2], `<span class="page-cat-tag">AWAY — NOT ON SITE</span>`) {
				t.Errorf("%s: the pad block %q carries no printed category label. Dormant while the away list is print-scoped out, but it must survive for the day it is not", route, p[1])
			}
		}
	}

	// And it must reach the paper. Screen-hidden is the whole design; hidden
	// on PAPER would make the element pure dead weight and silently undo the
	// fix, which is exactly the failure the print-CSS parser exists to catch.
	rules := paperStylesheet(t, rawPage(t, srv, "/muster/print"))
	for _, class := range []string{"page-cat", "page-cat-tag"} {
		if why, hidden := hiddenOnPaper(rules, class); hidden {
			t.Errorf("print hides .%s (%s) — the per-block category label exists only to survive a page break, so hiding it on paper removes the whole fix while leaving the markup in place", class, why)
		}
	}
	// The other direction: it must NOT show on screen, where the section
	// heading is a few lines up. A visible duplicate is noise, and noise on
	// this page is how the categories got blurred in the first place.
	if got, ok := screenClassValue(rules, "page-cat", "display"); !ok || strings.TrimSpace(got) != "none" {
		t.Errorf("on screen .page-cat resolves display=%q (found=%v), want none", got, ok)
	}
}

// --- M-16: the sheet stops claiming a disjointness the domain refuses ---

// TestTheHeadcountAdmitsItsCategoriesCanOverlap.
//
// architecture M2. Every exception row used to read "NOT on site and NOT in
// the total", and OutAtUncontrolled's own contract says the opposite in the
// case that matters: "a person collected from the pad who turns up at the
// peer unannounced stays on the pad list — an extra name beside the peer's
// own on-site listing of them (their local open visit), never an omission."
// NotTracked documents the same asymmetry for its own scope. The direction
// is over-report, which is the safe one and must NOT be filtered out — so
// the fix is the sheet's wording, and the thing that had to stop was a
// safety sheet teaching its reader that one of the two lines in front of
// them must be a bug.
//
// Asserted through the tally's structural hooks rather than by scanning the
// page for prose: the row must be IN the tally block (not a footnote
// somewhere below the fold). It used to be required to print too; since
// James, 2026-10-01 (print = roster only) the tally and its overlap row are
// screen-only, and the test asserts that instead.
// overlapClaimsThatMustNotReturn is the disjointness claim architecture M2
// removed from the sheet, in both spellings it was written in.
var overlapClaimsThatMustNotReturn = []string{
	"NOT on site and NOT in the total",
	"NOT on site, not in the total above",
}

func TestTheHeadcountAdmitsItsCategoriesCanOverlap(t *testing.T) {
	srv := musterWithAllThreeCategories(t)

	for _, route := range []string{"/muster", "/muster/print"} {
		body := styleRe.ReplaceAllString(rawPage(t, srv, route), "")
		at := strings.Index(body, `data-muster-section="tally"`)
		if at < 0 {
			t.Fatalf("%s: no tally block:\n%s", route, body)
		}
		rest := body[at:]
		if end := strings.Index(rest, "</section>"); end > 0 {
			rest = rest[:end]
		}
		if !strings.Contains(rest, `data-tally-row="overlap"`) {
			t.Fatalf("%s: the headcount block states three numbers and never says they can overlap. The domain deliberately over-reports — a person can be counted on site AND named as away — so without this the sheet contradicts itself in a state reachable through the ordinary UI:\n%s", route, rest)
		}
		// The two exception rows must no longer make the absolute claim.
		//
		// Matched through phraseSeen, not strings.Contains: this sheet's copy
		// is hand-wrapped mid-sentence, and the claim was reintroducible
		// verbatim across a line break with this guard still green (MEASURED
		// — S103-WATCHLISTS.md §2). assertPhraseWatchBites runs the matcher
		// against known-broken input FIRST, so a clean pass here cannot mean
		// the instrument went blind again.
		assertPhraseWatchBites(t, overlapClaimsThatMustNotReturn)
		for _, gone := range overlapClaimsThatMustNotReturn {
			if phraseSeen(body, gone) {
				t.Errorf("%s: the sheet still says %q. That is the disjointness OutAtUncontrolled refuses by design", route, gone)
			}
		}
	}

	// AMENDED by James, 2026-10-01: print = roster only (no sync verdicts,
	// no counts/summary pages). This used to assert .tally-overlap reaches
	// paper. The whole headcount block is off the paper now, so there are
	// no counts on the sheet for the overlap row to qualify; the row stays
	// on screen with the counts it explains (asserted above on /muster).
	// Checked on the CONTAINER: a class-level check on .tally-overlap alone
	// reads "not hidden" (no rule names it) and would keep claiming the row
	// prints — measured, it did.
	rules := paperStylesheet(t, rawPage(t, srv, "/muster/print"))
	if _, hidden := hiddenOnPaper(rules, "tally"); !hidden {
		t.Errorf("print does NOT hide .tally — the headcount is back on the roster-only sheet (James, 2026-10-01).\nprint block was:\n%s", printBlockText(rules))
	}
}
