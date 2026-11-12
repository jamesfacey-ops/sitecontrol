// phrasewatch_test.go — the matcher every PHRASE watch-list in this package
// needs, and the built-in control that stops it going blind again.
//
// THE DEFECT THIS CLOSES. Three watch-lists in this package forbid a
// multi-word phrase from appearing in operator-facing copy, and all three
// matched with a bare strings.Contains against un-normalised markup. Every
// long copy string in these templates is HAND-WRAPPED mid-sentence, so the
// forbidden sentence could be reintroduced verbatim, wrapped the way its
// neighbours are wrapped, and no guard would say a word. Measured, both
// arms, at three sites — see reports/window-build/S103-WATCHLISTS.md §2 and
// Amendment D.
//
// ⚠ IT IS NOT A GHOST DEFECT AND THE §110 PAIRS DO NOT CLOSE IT. The watch
// words are spelled correctly and their subject is real; the instrument
// simply cannot see them. An entry can be a CORRECT absence assertion and
// still be unable to notice the thing coming back.
//
// ⚠ AND THERE WAS NO EXISTING NORMALISER TO REUSE. w3PageText looked like
// one and is not: it lowercases and strips <script>/<style> and leaves every
// newline in place, so the phrase lists that go through it carry the same
// exposure. That is recorded rather than fixed here — those lists are not in
// this repair's scope.
package web

import (
	"strings"
	"testing"
)

// collapseWS replaces every run of whitespace with a single space. This is
// the whole repair: a watch word is compared against text whose line breaks
// cannot decide the outcome.
func collapseWS(s string) string { return strings.Join(strings.Fields(s), " ") }

// phraseSeen reports whether phrase occurs in text once both sides are
// whitespace-normalised. Case is left to the caller: some of these lists
// match operator copy verbatim and some lowercase the surface first.
func phraseSeen(text, phrase string) bool {
	return strings.Contains(collapseWS(text), collapseWS(phrase))
}

// collapseWSWithLines is collapseWS for a surface where a hit must still be
// reported by line: it returns the normalised text plus, for each byte of it,
// the 1-based source line that byte came from. A phrase watch-list that scans
// a file line by line cannot see a phrase the author wrapped, and dropping to
// a whole-file match would lose the line number that makes a hit actionable.
func collapseWSWithLines(src string) (string, []int) {
	out := make([]byte, 0, len(src))
	lines := make([]int, 0, len(src))
	line := 1
	pendingSpace := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			if c == '\n' {
				line++
			}
			pendingSpace = len(out) > 0
			continue
		}
		if pendingSpace {
			out = append(out, ' ')
			lines = append(lines, line)
			pendingSpace = false
		}
		out = append(out, c)
		lines = append(lines, line)
	}
	return string(out), lines
}

// wrapForProbe rewrites a phrase the way this repo's templates wrap copy:
// a newline plus indentation at the first word boundary. It reports false
// for a single-word phrase, which has no wrap point and therefore no
// exposure to close.
func wrapForProbe(phrase string) (string, bool) {
	i := strings.Index(strings.TrimSpace(phrase), " ")
	p := strings.TrimSpace(phrase)
	if i < 0 {
		return "", false
	}
	return p[:i] + "\n        " + p[i+1:], true
}

// assertPhraseWatchBites is the CONTROL, and it is built IN rather than
// performed once: every caller runs it against known-broken input BEFORE
// reading the real surface, so a clean pass cannot mean "the matcher is
// blind again". Three arms, because one is not enough:
//
//	POSITIVE  the wrapped phrase must be FOUND — fails if collapseWS breaks.
//	NEGATIVE  unrelated text must NOT match — fails if the matcher is
//	          trivially true, which would make every caller vacuously green.
//	REGRESSION the OLD bare-Contains matcher must MISS the wrapped phrase —
//	          fails if somebody "simplifies" phraseSeen back to what it
//	          replaced, which is exactly how this defect would return.
func assertPhraseWatchBites(t *testing.T, phrases []string) {
	t.Helper()
	if len(phrases) == 0 {
		t.Fatal("PHRASE-WATCH CONTROL: the watch list is EMPTY, so the assertions it guards are true of nothing")
	}
	wrappable := 0
	for _, phrase := range phrases {
		if strings.TrimSpace(phrase) == "" {
			t.Fatalf("PHRASE-WATCH CONTROL: the list carries a blank watch word, which matches everything")
		}
		if !phraseSeen("prefix "+phrase+" suffix", phrase) {
			t.Fatalf("PHRASE-WATCH CONTROL (positive, unwrapped): %q was not found in text that plainly contains it — the matcher is broken", phrase)
		}
		if phraseSeen("nothing here resembles the watched copy at all", phrase) {
			t.Fatalf("PHRASE-WATCH CONTROL (negative): %q matched unrelated text — a matcher that always fires makes every caller vacuously green", phrase)
		}
		wrapped, ok := wrapForProbe(phrase)
		if !ok {
			continue
		}
		wrappable++
		hay := "<span class=\"note\">Filler copy before. " + wrapped + " Filler copy after.</span>"
		if !phraseSeen(hay, phrase) {
			t.Fatalf("PHRASE-WATCH CONTROL (positive, WRAPPED): %q was not found when wrapped across a line the way this repo's templates wrap copy. That is the exact defect this matcher exists to close", phrase)
		}
		if strings.Contains(hay, phrase) {
			t.Fatalf("PHRASE-WATCH CONTROL (regression arm): a bare strings.Contains FOUND %q in the wrapped haystack, so this control is no longer discriminating between the old matcher and the new one — the probe has stopped biting", phrase)
		}
	}
	if wrappable == 0 {
		t.Fatal("PHRASE-WATCH CONTROL: no watch word in this list has a word boundary, so the wrapping arms proved nothing about it")
	}
}

// TestThePhraseWatchMatcherSurvivesHandWrapping is the mechanism's own test,
// so the matcher is exercised directly and not only through its callers.
func TestThePhraseWatchMatcherSurvivesHandWrapping(t *testing.T) {
	const phrase = "NOT on site and NOT in the total"
	wrapped := "<span class=\"tally-note\">These people are NOT on site and NOT in the\n        total above.</span>"

	// The measured defect, pinned as an assertion: the old matcher misses it.
	if strings.Contains(wrapped, phrase) {
		t.Fatal("the wrapped fixture is not actually wrapped — this test would prove nothing")
	}
	if !phraseSeen(wrapped, phrase) {
		t.Errorf("phraseSeen missed %q wrapped across a line — the repair does not work", phrase)
	}
	if phraseSeen("<p>an unrelated sentence about vehicles</p>", phrase) {
		t.Errorf("phraseSeen matched text that does not contain %q", phrase)
	}
	// Tabs and runs of spaces are the other two shapes the templates use.
	for _, form := range []string{
		"NOT on site and NOT in the\ttotal",
		"NOT on site and NOT     in the total",
		"NOT on site and NOT in the\r\n\t\ttotal",
	} {
		if !phraseSeen(form, phrase) {
			t.Errorf("phraseSeen missed %q written as %q", phrase, form)
		}
	}
	assertPhraseWatchBites(t, []string{phrase, "off-vehicle departures"})
}
