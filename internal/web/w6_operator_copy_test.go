// w6_operator_copy_test.go — W6-9: operator copy is a criterion, not
// decoration. The printed sheet says what the code does.
//
// ⚠ THIS CRITERION EXISTS BECAUSE W5d's COMMIT WAS BLOCKED at a build
// critique over a message that told an officer to radio a working post, and
// the same class was found five times. W6 changes what an officer reads on a
// life-safety document; the copy is therefore in scope.
//
//	(a) the existing claim becomes TRUE — for the class the mechanism can
//	    deliver, and the sheet must say WHICH.
//	(b) the honest clause DR-W6-5 requires: the sheet must state that its
//	    NAMES, not only its grouping, are as recorded at check-in — because
//	    under DR-W6-5 the screen and the paper can legitimately show different
//	    names for the same person, and an officer who is not told that reads
//	    it as a fault. It must cover the WHOLE SHEET: after W6 the roster body
//	    renders the SNAPSHOT (muster.html:317, :333) while the exception
//	    sections render the LIVE roster name at THREE sites (:415, :551,
//	    :586), and the sheet already tells its reader that "a name can appear
//	    in more than one of them" (:270-272).
//	(c) no copy on any muster surface claims what the mechanism cannot
//	    deliver.
//
// ⚠ (a) MAY NOT PIN :137 AS IT STANDS. After W6 the reproduction promise is
// true for every visit carrying a non-empty snapshot and STILL FALSE for a
// visit whose snapshot is absent or empty — adjudication #7's ruled middle
// arm renders the live name there. An unqualified reproduction promise,
// pinned as a locked literal, is a FALSE SENTENCE CERTIFIED ON THE DOCUMENT
// READ ALOUD DURING AN EVACUATION — the class that blocked W5d's commit.
package web

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// --- W6-9(c): the forbidden-claim scan ----------------------------------
//
// THE SUBJECT IS THE WHOLE FILE, and the exclusions are LISTED — there are
// exactly THREE, every one decided at the lock:
//
//	(1) template actions {{ … }} elided — they carry interpolated DATA, and a
//	    person may legitimately be named "Verified";
//	(2) Go template comments {{/* … */}} elided — they never reach the output;
//	(3) the CONTENT of <script>/<style> elements elided — a script body is
//	    executed and a style body styles, and neither is ever displayed to an
//	    operator as copy. The elements' own TAGS stay in scope.
//
// NOTHING ELSE IS EXCLUDED: no class filter, no element filter, no
// edited-paragraph filter. Eliding CATEGORIES THAT RENDER NO OPERATOR COPY —
// rather than naming the elements that do — is what makes the filter provably
// not narrower than its subject.
//
// ⚠ A TEST-WRITER WHO ADDS A FOURTH EXCLUSION TO MAKE THE SCAN PASS IS
// RELAXING A LOCKED CRITERION, which is OW-1's recorded consequence — the
// consequence this criterion cites against itself. None is added here.
var w6ForbiddenClaims = []string{
	"what the sheet said", "what was printed", "as printed", "correct as of",
	"confirmed as of", "verified", "the current name", "names are current",
	"up to date", "up-to-date", "the name on file", "re-checked", "accurate",
	"guarantee",
}

var (
	w6TmplCommentRe = regexp.MustCompile(`(?s)\{\{/\*.*?\*/\}\}`)
	w6TmplActionRe  = regexp.MustCompile(`(?s)\{\{.*?\}\}`)
	w6ScriptBodyRe  = regexp.MustCompile(`(?s)(<script[^>]*>)(.*?)(</script>)`)
	w6StyleBodyRe   = regexp.MustCompile(`(?s)(<style[^>]*>)(.*?)(</style>)`)
	w6ModeDetailRe  = regexp.MustCompile(`(?s)<p class="mode-detail">(.*?)</p>`)
	w6ModeBlockRe   = regexp.MustCompile(`(?s)<div class="mode-(live|snapshot|reprint)" data-muster-mode="[^"]*">(.*?)</div>`)
)

// w6FilterTemplate applies the three exclusions. Each is switchable so the
// controls can prove the exclusion is DOING WORK rather than assert it.
//
// ⚠ Elided regions are replaced by their OWN NEWLINES, not removed: a hit's
// reported line number must be the line in muster.html, or a control that
// names a line is naming a line in a document nobody has.
func w6FilterTemplate(src string, comments, actions, scriptStyle bool) string {
	out := src
	if comments {
		out = w6BlankSpans(out, w6TmplCommentRe, 0)
	}
	if actions {
		out = w6BlankSpans(out, w6TmplActionRe, 0)
	}
	if scriptStyle {
		out = w6BlankSpans(out, w6ScriptBodyRe, 2)
		out = w6BlankSpans(out, w6StyleBodyRe, 2)
	}
	return out
}

// w6BlankSpans replaces submatch `group` of every match (group 0 = the whole
// match) with an equal number of newlines, preserving line positions.
func w6BlankSpans(s string, re *regexp.Regexp, group int) string {
	locs := re.FindAllStringSubmatchIndex(s, -1)
	if locs == nil {
		return s
	}
	var b strings.Builder
	prev := 0
	for _, m := range locs {
		lo, hi := m[2*group], m[2*group+1]
		if lo < 0 {
			continue
		}
		b.WriteString(s[prev:lo])
		b.WriteString(strings.Repeat("\n", strings.Count(s[lo:hi], "\n")))
		prev = hi
	}
	b.WriteString(s[prev:])
	return b.String()
}

// w6ClaimHits reports every forbidden literal found in the filtered text,
// with the line it sits on, so a red names the sentence and not just the word.
func w6ClaimHits(filtered string) []string {
	var hits []string
	lower := strings.ToLower(filtered)
	for _, claim := range w6ForbiddenClaims {
		idx := 0
		for {
			i := strings.Index(lower[idx:], claim)
			if i < 0 {
				break
			}
			at := idx + i
			line := 1 + strings.Count(filtered[:at], "\n")
			start := strings.LastIndex(filtered[:at], "\n") + 1
			end := strings.Index(filtered[at:], "\n")
			if end < 0 {
				end = len(filtered) - at
			}
			hits = append(hits, fmt.Sprintf("line %d: %q in %q", line, claim,
				strings.TrimSpace(filtered[start:at+end])))
			idx = at + len(claim)
		}
	}
	return hits
}

func w6MusterTemplate(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("templates/muster.html")
	if err != nil {
		t.Fatalf("fixture defect: cannot read the muster template, which IS this scan's subject: %v", err)
	}
	return string(b)
}

// TestW6NoMusterCopyClaimsWhatTheMechanismCannotDeliver — W6-9(c).
//
// ⚠ ALREADY TRUE at f33c4c7 (plan §2.5 row 7) — and its already-true status
// DEPENDS ON EXCLUSIONS (2) AND (3) ACTUALLY WORKING, so both are CHECKED
// properties here rather than asserted ones. [MEASURED] thirteen of the
// fourteen literals occur ZERO times; "guarantee" occurs TWICE, in two
// DIFFERENT non-rendering categories: :16 inside a Go template comment
// ({{/* opens at :5, */}} closes at :59) and :151 inside a JavaScript
// /* … */ comment inside the <script> opened at :147 — which is OUTSIDE
// every template comment. Under all three exclusions the census returns
// ZERO; under two it returns ONE.
//
// EXPECTED RED at f33c4c7: NONE. Its evidence is the plant.
func TestW6NoMusterCopyClaimsWhatTheMechanismCannotDeliver(t *testing.T) {
	src := w6MusterTemplate(t)

	// --- CONTROL: the plant, and its LOCATION is specified --------------
	// A forbidden literal planted in the UN-CLASSED <p> at muster.html:126-127
	// — a paragraph W6 does NOT edit AND one that lies outside revision 1's
	// filter ("mode-detail/mode-headline elements and the <small> footnotes").
	//
	// ⚠ A PLANT IN A mode-detail, A mode-headline OR A <small> DOES NOT
	// DISCHARGE THIS CONTROL, and the reason is exactly why revision 1's
	// control was defective: the live mode-detail at :94 is a paragraph W6
	// does not edit AND is INSIDE the old filter, so planting there proves
	// location-within-filter and never filter-covers-subject. It defeats the
	// narrowest misreading and leaves the real one intact.
	const target = `reproduce these same people, whatever happens on site afterwards.`
	if !strings.Contains(src, target) {
		t.Fatalf("CONTROL SETUP: the un-classed <p> at muster.html:126-127 no longer carries %q, so the plant "+
			"cannot be placed where the criterion specifies", target)
	}
	// Rule 6 — the plant site must actually be OUTSIDE the narrow filter, or
	// the control proves the wrong thing.
	planted := strings.Replace(src, target, target+" These names are verified.", 1)
	if strings.Contains(w6EnclosingOpenTag(src, target), "class=") {
		t.Fatalf("CONTROL SETUP: the plant site's <p> now carries a class (%q), so it is no longer OUTSIDE "+
			"revision 1's class-scoped filter and this control would prove location-within-filter",
			w6EnclosingOpenTag(src, target))
	}
	if hits := w6ClaimHits(w6FilterTemplate(planted, true, true, true)); len(hits) == 0 {
		t.Fatalf("W6-9(c) CONTROL FAILED (m6-9): a forbidden claim planted in the un-classed <p> at " +
			"muster.html:126-127 — outside revision 1's mode-detail/mode-headline/<small> filter — was NOT " +
			"detected. This is the SIXTH instance on this project of a scan whose filter is narrower than its " +
			"subject, and this criterion invokes that rule by name")
	}

	// --- NEGATIVE ARM: the existing sentence must NOT be flagged --------
	// "reproduce these same people, whatever happens on site afterwards" is
	// TRUE OF MEMBERSHIP (VisitsByID freezes the exact set). A list broad
	// enough to fire on true copy is a list a builder will weaken, which is
	// OW-1's recorded consequence.
	if hits := w6ClaimHits(w6FilterTemplate(src, true, true, true)); len(hits) != 0 {
		t.Errorf("W6-9(c) (m6-9): muster.html carries copy claiming what the mechanism cannot deliver:\n  %s\n"+
			"  [DOC] S10-R1-A3-architecture-readpath.md:74 (A3-2): the snapshot freezes \"the name as this post's "+
			"replica knew it at arrival\", which is NOT \"what the sheet said\". ⟦DR-W6-9⟧ that second-order "+
			"residual STANDS — with the link down, an amendment authored at the other post BEFORE arrival is "+
			"invisible to the receiving post, so the snapshot can freeze a name already corrected estate-wide.\n"+
			"  ⚠ If a later edit added a non-rendering region, this reds and THAT IS CORRECT: widening the "+
			"exclusion list is a LOCK DECISION, never a build-time repair.",
			strings.Join(hits, "\n  "))
	}

	// --- the two exclusions are CHECKED, not asserted -------------------
	//
	// ⚠ PINNED BY REGION, NEVER BY LINE NUMBER, and that is a GAP CLOSED
	// (G-4) rather than a matter of taste. [MEASURED by the W6 mutant gate]
	// the previous form asserted `w6HitsOnLine(hits, 16)` and
	// `w6HitsOnLine(hits, 151)`: ONE BLANK LINE inserted at the top of
	// muster.html — an edit with no bearing on any W6 criterion — turned both
	// RED, each announcing that the exclusion was "NOT DOING WORK" while the
	// hit list printed one line below it showed the exclusion doing exactly
	// that. The message asserted the opposite of what was true.
	//
	// ⚠ AND IT HAD ALREADY COST SOMETHING REAL. [DOC] W6-build.md §2.3: the
	// builder's first draft of the W6-9(b) copy added seven lines above :151,
	// reddened this control, and the builder RE-WRAPPED BOTH mode-detail
	// paragraphs to their exact original physical line counts (snapshot 6
	// lines, reprint 3; longest line 193 chars) to keep it green. A test
	// dictated the line-wrapping of a document read aloud during an
	// evacuation. It may not do that again.
	//
	// WHAT IS ASSERTED INSTEAD is the property the criterion actually names,
	// in two halves: disabling the exclusion SURFACES a forbidden literal
	// that the full filter suppresses, AND that literal lies INSIDE the
	// region the exclusion elides. The regions are computed from the VERY
	// REGEXPS w6FilterTemplate uses, so the control cannot drift from the
	// filter it certifies. Re-wrapping muster.html moves the region and the
	// hit together and this stays green; deleting the region — the only
	// thing that actually stops the exclusion excluding — reds. The
	// suppressed-by-the-full-filter half is STRONGER than the line pin it
	// replaces, which never checked that the hit was one the exclusion
	// removes.
	full := w6ClaimHits(w6FilterTemplate(src, true, true, true))

	// (2) — the Go-template-comment region. ⚠ Noted honestly: a Go template
	// comment IS syntactically a {{ … }} action, so exclusions (1) and (2)
	// overlap on this region and this control disables BOTH — claiming (2)
	// alone elides it would be a claim this instrument cannot separate.
	commentSpans := w6Spans(src, w6TmplCommentRe, 0)
	commentSurfaced := w6HitsNotIn(w6ClaimHits(w6FilterTemplate(src, false, false, true)), full)
	if inside := w6HitsInsideSpans(commentSurfaced, commentSpans); len(inside) == 0 {
		t.Errorf("W6-9(c) EXCLUSIONS (1)/(2) ARE NOT EXCLUDING ANYTHING: with {{ … }} left IN, no forbidden "+
			"literal surfaced from INSIDE a {{/* … */}} span that the full filter suppresses. This criterion's "+
			"already-true status DEPENDS on that exclusion, so an exclusion with nothing to exclude means the "+
			"census's ZERO is unearned.\n"+
			"  [MEASURED at f33c4c7] \"guarantee\" sits inside the {{/* … */}} opened at muster.html:5. The LINE "+
			"is deliberately NOT pinned: re-wrapping the template moves the region and its hit together, and only "+
			"DELETING the commented region makes this red.\n"+
			"  {{/* … */}} spans, as lines of muster.html: %s\n"+
			"  hits surfaced by disabling the exclusion: %v", w6FmtSpans(commentSpans), commentSurfaced)
	}
	// (3) — the <script>/<style> body. This exclusion is a CORRECTION, not an
	// addition: under two exclusions the criterion was RED against the
	// correct tree, because the "guarantee" in the JavaScript /* … */ comment
	// inside the <script> is OUTSIDE every template comment.
	scriptSpans := append(w6Spans(src, w6ScriptBodyRe, 2), w6Spans(src, w6StyleBodyRe, 2)...)
	scriptSurfaced := w6HitsNotIn(w6ClaimHits(w6FilterTemplate(src, true, true, false)), full)
	if inside := w6HitsInsideSpans(scriptSurfaced, scriptSpans); len(inside) == 0 {
		t.Errorf("W6-9(c) EXCLUSION (3) IS NOT EXCLUDING ANYTHING: with <script>/<style> CONTENT left IN, no "+
			"forbidden literal surfaced from INSIDE a <script> or <style> body that the full filter suppresses. "+
			"That hit is the ONE revision 2's two-exclusion filter returned, and it is why exclusion (3) is a "+
			"correction rather than an addition — without it the criterion was RED against the CORRECT tree.\n"+
			"  [MEASURED at f33c4c7] the hit is a JavaScript /* … */ comment inside the <script> element, which "+
			"lies outside every template comment. The LINE is deliberately NOT pinned.\n"+
			"  <script>/<style> body spans, as lines of muster.html: %s\n"+
			"  hits surfaced by disabling the exclusion: %v", w6FmtSpans(scriptSpans), scriptSurfaced)
	}
}

// --- REGION pinning, so a control names a REGION and not a LINE ---------
//
// w6Span is an INCLUSIVE 1-based line range of muster.html.
type w6Span struct{ First, Last int }

// w6Spans returns the line ranges covered by submatch `group` of every match
// of re — the SAME spans w6FilterTemplate elides, computed from the SAME
// regexps, so a control pinned to a span cannot drift from the filter it
// certifies.
func w6Spans(src string, re *regexp.Regexp, group int) []w6Span {
	var out []w6Span
	for _, m := range re.FindAllStringSubmatchIndex(src, -1) {
		lo, hi := m[2*group], m[2*group+1]
		if lo < 0 {
			continue
		}
		first := 1 + strings.Count(src[:lo], "\n")
		out = append(out, w6Span{First: first, Last: first + strings.Count(src[lo:hi], "\n")})
	}
	return out
}

func w6SpansContain(spans []w6Span, line int) bool {
	for _, s := range spans {
		if line >= s.First && line <= s.Last {
			return true
		}
	}
	return false
}

func w6FmtSpans(spans []w6Span) string {
	parts := make([]string, 0, len(spans))
	for _, s := range spans {
		parts = append(parts, fmt.Sprintf("%d-%d", s.First, s.Last))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// w6HitLine reads back the line number w6ClaimHits reported a hit on.
func w6HitLine(hit string) int {
	var n int
	if _, err := fmt.Sscanf(hit, "line %d:", &n); err != nil {
		return 0
	}
	return n
}

// w6HitsInsideSpans returns the hits reported on a line inside any span.
func w6HitsInsideSpans(hits []string, spans []w6Span) []string {
	var out []string
	for _, h := range hits {
		if w6SpansContain(spans, w6HitLine(h)) {
			out = append(out, h)
		}
	}
	return out
}

// w6HitsNotIn returns the hits present in a and absent from b — the hits one
// exclusion is responsible for suppressing.
func w6HitsNotIn(a, b []string) []string {
	in := make(map[string]bool, len(b))
	for _, h := range b {
		in[h] = true
	}
	var out []string
	for _, h := range a {
		if !in[h] {
			out = append(out, h)
		}
	}
	return out
}

// w6EnclosingOpenTag returns the opening tag of the element containing needle.
func w6EnclosingOpenTag(src, needle string) string {
	at := strings.Index(src, needle)
	if at < 0 {
		return ""
	}
	start := strings.LastIndex(src[:at], "<")
	for start > 0 {
		end := strings.Index(src[start:], ">")
		if end > 0 && !strings.HasPrefix(src[start:], "</") && !strings.HasPrefix(src[start:], "<a ") {
			return src[start : start+end+1]
		}
		start = strings.LastIndex(src[:start], "<")
	}
	return ""
}

// --- W6-9(a) and (b): what the mode-detail paragraphs must say ----------

// w6Sentences splits a rendered paragraph into sentence literals, in order.
func w6Sentences(p string) []string {
	flat := strings.Join(strings.Fields(w6Unescape(p)), " ")
	var out []string
	for _, s := range strings.SplitAfter(flat, ". ") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func w6ModeDetail(t *testing.T, body, mode string) string {
	t.Helper()
	for _, blk := range w6ModeBlockRe.FindAllStringSubmatch(body, -1) {
		if blk[1] != mode {
			continue
		}
		if m := w6ModeDetailRe.FindStringSubmatch(blk[2]); m != nil {
			return m[1]
		}
	}
	t.Fatalf("fixture defect: no <p class=\"mode-detail\"> inside the %q mode block — W6-9's subject is not on "+
		"the page it is asserted against", mode)
	return ""
}

// TestW6PrintedSheetSaysItsNamesAreAsRecorded — W6-9(a)'s copy pin and
// W6-9(b)'s honest clause, on BOTH frozen surfaces.
//
// ⚠ WHAT IS PINNED, and what a test-writer who never sees the build CAN pin:
//
//   - THE AS-RECORDED CLAUSE, as an ORDERED SENTENCE LITERAL. Half of (m6-9)
//     is "the as-recorded clause removed", and the reprint paragraph's FIRST
//     sentence is pinned verbatim. This is ALREADY TRUE at f33c4c7 and its
//     clean pass is not evidence; its evidence is the planted removal.
//   - THE PROPERTY W6-9(a) states in terms: the paragraph contains NO
//     UNQUALIFIED PROMISE that the names reproduce — the reproduction promise
//     is either qualified in the same sentence or scoped by the names clause
//     (b) introduces, in the same paragraph.
//   - THE NAMES CLAUSE (b) requires, and the TWO-REGION distinction.
//
// The builder writes the wording (SEAM-5); this pins whatever lands and
// forbids the unqualified form. ⚠ SHORTFALL, reported in W6-tests.md §6: the
// full "ordered list of sentence literals as it reads AFTER W6's edit" cannot
// be authored before the edit exists. The ordered list IS extracted and
// printed in every failure message so a gate can pin it at the build.
//
// EXPECTED RED at f33c4c7 (runtime, produced):
//   - the reprint paragraph carries "This reproduces the same roster no matter
//     when it is printed again." with no qualifier and no names clause;
//   - neither frozen paragraph says anything about NAMES.
func TestW6PrintedSheetSaysItsNamesAreAsRecorded(t *testing.T) {
	// Three people on site. W6-9's subject is the COPY, but the snapshot
	// sheet only mints its ?visits= reprint link when it has a roster to
	// name, and the reprint mode-detail is the paragraph (a) pins.
	fx := w6BuildOrderFixture(t, []string{"aaron, Zed", "Brown, Ann", "Carter, Mo"},
		[]string{"Adams, Bea", "Yates, Cal", "Novak, Sam"})
	snapshot, reprint, _ := w6PrintPages(t, fx.srv)

	// (a) THE AS-RECORDED CLAUSE, pinned as an ordered sentence literal.
	// ⟦LOCK — §87, 2026-09-03⟧ THE PINNED LITERAL IS AMENDED. This is the lock amendment
	// w8_copy_test.go:6-22 routed to the plan owner and declined to make itself ("a
	// test-writer does not amend a locked criterion"). W8-22 states the direction there:
	// "the sentence either survives naming a deleted mechanism, or W8 AMENDS A PINNED
	// LITERAL INSIDE A LOCKED CRITERION. The second is correct."
	//
	// ⭐ THE PROMISE TO THE OPERATOR IS IDENTICAL, and that is the condition §87 set —
	// not merely that the words changed for a sanctioned reason:
	//   • The FIRST CLAUSE is unchanged, verbatim: "Grouped exactly as it was recorded at
	//     check-in (point-in-time snapshot)". That clause IS the promise — what you are
	//     holding is the site as it stood at check-in.
	//   • The second clause is the guarantee that the promise HOLDS LATER. The old wording
	//     named the MECHANISM ("the alias table"); the new names the EVENT CLASS ("later
	//     merges of reference data"). The operator's actual question — "can this sheet
	//     change under me after somebody corrects a record?" — gets the same answer: no.
	// ⚠ AND THE OLD WORDING WOULD NO LONGER DELIVER IT. W8 deletes the alias table but
	// merging SURVIVES through `same_as` composition. So the pinned sentence would have
	// become literally true and operationally VACUOUS — promising the absence of a
	// mechanism that cannot occur, while silent about the merges that still can. The new
	// wording is the one that still answers the question, and it answers it in the
	// operator's terms rather than in ours: "the alias table" was internal jargon on a
	// page a person reads under pressure.
	//
	// ⚠ W8-22's CONDITION IS MET: "the as-recorded claim must still be there to remove, or
	// (m6-9) loses half its kill." The amended literal RETAINS the as-recorded claim, and
	// (m6-9) is PRODUCED against it below — see reports/window-build/ALIAS-DELETION-REPAIR.md §4.12.
	// ⟦was: "\tconst asRecordedClause = \"Grouped exactly as it was recorded at check-in (point-in-time snapshot), never re-resolved through the alias table.\""⟧
	const asRecordedClause = "Grouped exactly as it was recorded at check-in (point-in-time snapshot), never re-resolved against later merges of reference data."
	rep := w6Sentences(w6ModeDetail(t, reprint, "reprint"))
	if len(rep) == 0 || rep[0] != asRecordedClause {
		t.Errorf("W6-9(a) (m6-9): the reprint mode-detail's FIRST sentence is %q, want the pinned literal %q.\n"+
			"  Half of (m6-9) is \"the as-recorded clause removed\". Adjudication #6 records that once "+
			"visit.person_snapshot_name lands, \"as it was recorded at check-in\" becomes true of NAMES without a "+
			"single word changing — so this sentence is what W6 makes true, and removing it would delete the "+
			"claim instead of honouring it.\n  the paragraph as it landed, in order: %q",
			w6First(rep), asRecordedClause, rep)
	}

	// (a) THE PROPERTY: no unqualified reproduction promise.
	for _, surface := range []struct {
		mode, body string
	}{{"reprint", reprint}, {"snapshot", snapshot}} {
		sents := w6Sentences(w6ModeDetail(t, surface.body, surface.mode))
		hasNamesClause := w6ParagraphHasNamesClause(sents)
		for _, s := range sents {
			if !strings.Contains(strings.ToLower(s), "reproduce") {
				continue
			}
			if w6SentenceIsQualified(s) || hasNamesClause {
				continue
			}
			t.Errorf("W6-9(a) (m6-9), %s sheet: an UNQUALIFIED reproduction promise — %q — with no qualifier in "+
				"the sentence and no names clause in the paragraph.\n"+
				"  After W6 that promise is true FOR EVERY VISIT CARRYING A NON-EMPTY SNAPSHOT and STILL FALSE "+
				"for a visit whose snapshot is absent or empty: adjudication #7's ruled middle arm renders the "+
				"LIVE name there, so the sheet DOES follow the amendment for that class — deliberately, because "+
				"\"still show the id, never drop the person\" outranks freezing a blank.\n"+
				"  ⚠ An unqualified reproduction promise pinned as a locked literal is a FALSE SENTENCE CERTIFIED "+
				"ON THE DOCUMENT READ ALOUD DURING AN EVACUATION — the class that blocked W5d's commit.\n"+
				"  the paragraph as it landed, in order: %q", surface.mode, s, sents)
		}
	}

	// (b) THE HONEST CLAUSE, on BOTH frozen mode-detail paragraphs.
	for _, surface := range []struct {
		mode, body string
	}{{"reprint", reprint}, {"snapshot", snapshot}} {
		sents := w6Sentences(w6ModeDetail(t, surface.body, surface.mode))
		if !w6ParagraphHasNamesClause(sents) {
			t.Errorf("W6-9(b), %s sheet: the mode-detail says nothing about NAMES.\n"+
				"  Under DR-W6-5 the screen and the paper can legitimately show DIFFERENT NAMES for the same "+
				"person, and an officer who is not told that reads it as a fault. The sheet must state that its "+
				"names, not only its grouping, are as recorded at check-in.\n"+
				"  the paragraph as it landed, in order: %q", surface.mode, sents)
		}
		if !w6ParagraphDistinguishesTheTwoRegions(sents) {
			t.Errorf("W6-9(b), %s sheet: the clause does not cover the WHOLE SHEET.\n"+
				"  [MEASURED] after W6 the roster body renders the SNAPSHOT (muster.html:317, :333) while the "+
				"exception sections render the LIVE roster name at THREE sites — :415 (in-transit), :551 (\"went "+
				"out, no recorded return\"), :586 (\"not tracked\") — all from domain.OccupantRef.Name. The sheet "+
				"already tells its reader that \"a name can appear in more than one of them\" (:270-272), so "+
				"AFTER W6 THE SAME PERSON CAN APPEAR ON ONE PAGE UNDER TWO DIFFERENT NAMES, by any of three "+
				"routes.\n  It WIDENS :138's existing exception-sections sentence from frozen-GROUPING to "+
				"frozen-NAMES; it does not create a disclosure that was absent.\n"+
				"  the paragraph as it landed, in order: %q", surface.mode, sents)
		}
	}
}

func w6First(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

// w6ParagraphHasNamesClause: some sentence talks about NAMES and ties them to
// the moment of recording.
func w6ParagraphHasNamesClause(sents []string) bool {
	for _, s := range sents {
		l := strings.ToLower(s)
		if !strings.Contains(l, "name") {
			continue
		}
		if strings.Contains(l, "recorded at check-in") || strings.Contains(l, "as recorded") ||
			strings.Contains(l, "at check-in") {
			return true
		}
	}
	return false
}

// w6ParagraphDistinguishesTheTwoRegions: some sentence names the exception
// sections AND ties them to names being current.
func w6ParagraphDistinguishesTheTwoRegions(sents []string) bool {
	for _, s := range sents {
		l := strings.ToLower(s)
		if strings.Contains(l, "exception section") && strings.Contains(l, "name") {
			return true
		}
	}
	return false
}

// w6SentenceIsQualified: the reproduction promise carries its own scope.
func w6SentenceIsQualified(s string) bool {
	l := strings.ToLower(s)
	for _, q := range []string{"grouping", "grouped", "unless", "except", "as recorded", "at check-in", "where a name"} {
		if strings.Contains(l, q) {
			return true
		}
	}
	return false
}
