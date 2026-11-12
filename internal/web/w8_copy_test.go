// w8_copy_test.go — W8-22: the operator copy stops naming a mechanism that
// no longer exists.
//
// Written from the LOCKED W8 criteria only. No built implementation was read.
//
// ⚠ THIS CRITERION LANDS INSIDE A LOCKED W6 PIN AND IS ROUTED AS AN AMENDMENT
// — SEAM-W8-1, the PLAN OWNER's, triggered BEFORE W8's builder.
// [MEASURED — internal/web/w6_operator_copy_test.go] W6-9(a) pins
//
//	const asRecordedClause = "Grouped exactly as it was recorded at check-in
//	(point-in-time snapshot), never re-resolved through the alias table."
//
// as an EXACT LITERAL, and its failure message records "Half of (m6-9) is
// 'the as-recorded clause removed'." AFTER W8 THERE IS NO ALIAS TABLE. So the
// sentence either survives naming a deleted mechanism, or W8 AMENDS A PINNED
// LITERAL INSIDE A LOCKED CRITERION. The second is correct and it is a LOCK
// AMENDMENT, not a build detail — WITH THE REQUIREMENT THAT W6's MUTANT
// SURVIVES IT: the as-recorded claim must still be there to remove, or
// (m6-9) loses half its kill.
//
// ⚠ This file asserts W8's half and NOT the amendment. A test-writer does not
// amend a locked criterion, and this file modifies no existing test.
package web

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// w8AliasNamingPhrases are the operator-facing spellings of the DELETED
// mechanism. ⚠ The subject is the MECHANISM, not the word: `AliasMap` and
// `maxAliasHops` SURVIVE by W8-10 and are Go identifiers, never operator copy.
var w8AliasNamingPhrases = []string{"alias table", "alias mechanism", "the aliases"}

// TestW8NoTemplateNamesTheAliasTable is W8-22(i).
//
// "No operator-facing string in ANY template names the alias table or the
// alias mechanism, over the template surface W6-9(c)'s scan already defines."
//
// ⭐ GENUINE PRE-CHUNK RED. [MEASURED] internal/web/templates/muster.html
// names "the alias table" in THREE places of operator-facing copy — :94,
// :108, :135 — and :94 additionally contains the word `workgroup`, so it is a
// W8-6 census hit as well as a copy change.
func TestW8NoTemplateNamesTheAliasTable(t *testing.T) {
	dir := w8TemplateDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("PRECONDITION: the template surface %s is unreadable: %v", dir, err)
	}

	// PRECONDITION: the surface is non-empty. A scan over no templates
	// reports a clean pass over nothing.
	var scanned int
	var hits []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".html") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("PRECONDITION: template %s unreadable: %v", e.Name(), err)
		}
		scanned++
		// Normalised before matching, and NOT scanned line by line: these
		// templates wrap copy mid-sentence, so "alias mechanism" written
		// across a line break was invisible to this scan while reading
		// perfectly to an operator (MEASURED — S103-WATCHLISTS.md
		// Amendment D, MUTANT J). The line map keeps a hit reportable by
		// line, which is what makes it actionable.
		text, lineOf := collapseWSWithLines(strings.ToLower(string(b)))
		for _, phrase := range w8AliasNamingPhrases {
			needle := collapseWS(phrase)
			for at := 0; ; {
				j := strings.Index(text[at:], needle)
				if j < 0 {
					break
				}
				hits = append(hits, e.Name()+":"+strconv.Itoa(lineOf[at+j])+" — "+phrase)
				at += j + len(needle)
			}
		}
	}
	if scanned == 0 {
		t.Fatal("PRECONDITION (instrument): the template scan reached NO .html files — it would report a clean pass over nothing")
	}
	// CONTROL, built in rather than performed once: the matcher is run
	// against known-broken input, so "no hits" cannot mean "the matcher
	// stopped working".
	assertPhraseWatchBites(t, w8AliasNamingPhrases)

	if len(hits) != 0 {
		t.Fatalf("W8-22(i) ((m8-48)): %d operator-facing string(s) still name the DELETED alias mechanism:\n  %s\n"+
			"⚠ ONE OF THESE IS PINNED AS AN EXACT LITERAL BY A LOCKED W6 CRITERION (W6-9(a)'s asRecordedClause), "+
			"so W8 amends a pinned literal inside a locked criterion. THAT IS A LOCK AMENDMENT, NOT A BUILD "+
			"DETAIL — routed to the plan owner at SEAM-W8-1, WITH THE REQUIREMENT THAT W6's (m6-9) SURVIVES IT: "+
			"half of that mutant is \"the as-recorded clause removed\", so the as-recorded CLAIM must still be "+
			"there to remove.\n"+
			"⚠ W6-9(c)'s forbidden-claim template scan runs over muster.html with a planted control, so W8's edits "+
			"land inside a LOCKED scan's surface and the gate must re-run it",
			len(hits), strings.Join(hits, "\n  "))
	}
}

// TestW8OneSpellingForTheNoTradeIdea is W8-22(ii).
//
// "The two '(no workgroup recorded)' / 'No workgroup recorded' spellings —
// TWO DIFFERENT SPELLINGS OF ONE IDEA [MEASURED — muster.go:198, :253;
// picker.go:157-158] — become ONE spelling of the trade idea, asserted as a
// SINGLE LITERAL so the divergence does not survive the rename."
//
// ⭐ GENUINE PRE-CHUNK RED: two spellings exist today and neither is trade's.
//
// ⚠ THE SPELLINGS ARE DERIVED, NOT LISTED: the scan collects every
// production-Go string literal matching the "no <something> recorded" shape
// and asserts the SET of them is exactly one, and that the one is trade's.
// A hand-kept list would rot the first time a third spelling appeared —
// which is how this defect arrived in the first place.
func TestW8OneSpellingForTheNoTradeIdea(t *testing.T) {
	root := w8WebModuleRoot(t)
	found := map[string][]string{}
	for _, dir := range []string{
		filepath.Join(root, "internal", "web"),
		filepath.Join(root, "internal", "domain"),
	} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("PRECONDITION: %s unreadable: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range w8NoXRecordedRe.FindAllString(string(b), -1) {
				key := strings.ToLower(m)
				found[key] = append(found[key], e.Name())
			}
		}
	}

	if len(found) == 0 {
		t.Fatalf("PRECONDITION (instrument): the derived scan found NO \"no … recorded\" spelling anywhere in " +
			"production Go. [MEASURED at the lock: muster.go:198 and :253 carry two, and picker.go:157-158 a " +
			"third rendering.] A scan finding none is reporting a clean pass over nothing, not a fixed defect")
	}

	var spellings []string
	for k, files := range found {
		spellings = append(spellings, k+" ("+strings.Join(dedupe(files), ", ")+")")
	}
	if len(found) != 1 {
		t.Fatalf("W8-22(ii) ((m8-49)): the \"no X recorded\" idea has %d DIFFERENT SPELLINGS in production Go:\n  %s\n"+
			"⚠ These are TWO SPELLINGS OF ONE IDEA, and W8 renames the idea. The criterion is that they become "+
			"ONE spelling of the trade idea, asserted as a SINGLE LITERAL, SO THE DIVERGENCE DOES NOT SURVIVE THE "+
			"RENAME — a rename applied to two spellings produces two new ones.\n"+
			"⚠ The spellings are DERIVED here, never listed: a hand-kept list rots the first time a third appears, "+
			"which is how this defect arrived", len(found), strings.Join(sortedCopy(spellings), "\n  "))
	}
	for k := range found {
		if !strings.Contains(k, "trade") {
			t.Errorf("W8-22(ii): the one surviving spelling is %q and it does not name the TRADE idea. "+
				"W8 renames the concept, not only the column", k)
		}
	}
}

func w8TemplateDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "templates")
}

// w8NoTradeRecordedRe is the DERIVATION, scoped to this criterion's SUBJECT.
//
// ⚠ THE SCOPE IS THE ENTITY, NOT THE SENTENCE SHAPE, and the first draft of
// this test got it wrong in the instructive direction: an unscoped
// `no [a-z]+ recorded` derivation returned SIX spellings, four of them about
// other entities — "(no company recorded)", "no company recorded",
// "no allowance recorded", "no number recorded". W8-22(ii)'s subject is
// the WORKGROUP/TRADE idea, and a filter WIDER than its subject fails on a
// correct tree exactly as a narrower one passes on a broken tree.
//
// ⚠ RECORDED AS A FINDING RATHER THAN ASSERTED: the COMPANY idea has the
// SAME divergence — "(no company recorded)" in muster.go and
// "no company recorded" in picker.go — two spellings of one idea, one entity
// over. It is NOT W8's and no assertion here touches it.
var w8NoXRecordedRe = regexp.MustCompile(`(?i)\(?no (?:workgroup|trade) recorded\)?`)

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
