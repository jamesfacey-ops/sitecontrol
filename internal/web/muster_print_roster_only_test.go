// muster_print_roster_only_test.go — James, 2026-10-01: print = roster only
// (no sync verdicts, no counts/summary pages).
//
// MEASURED before the ruling, on the ice-pad post of a five-post cluster:
// the printed sheet was 7 pages, page 1 the header copy and the per-peer
// sync verdicts, page 2 the headcount block, and the roster began on page
// 3. The ruling takes those blocks off the PAPER only — the live on-screen
// /muster keeps every one of them — and the paper becomes the runner line
// and the names.
//
// Three properties, asserted apart because each can fail on its own:
//
//  1. the non-roster blocks are hidden on paper and present on screen;
//  2. nothing that prints a NAME is hidden on paper, and no count escapes
//     the hidden blocks (a data-tally-count outside .tally would print);
//  3. the set of names on paper is exactly the set /muster lists, less the
//     away names that have been off paper since 2026-08-21.
//
// Stylesheet facts use printcss_test.go's cascade-aware parser; page facts
// use data-* hooks (F19).
package web

import (
	"html"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// rosterOnlyPaperHidden are the classes the print block hides under the
// 2026-10-01 ruling, each with the block it carries.
var rosterOnlyPaperHidden = map[string]string{
	"sheet-head":        "the h1, mode box and its copy, print controls",
	"sync-scope":        "the per-peer sync verdicts",
	"sync-scope-bad":    "the per-peer sync verdicts (degraded form)",
	"tally":             "the headcount block",
	"away-scope":        "the away count stand-in",
	"offsite-unprinted": "an empty Not-on-site heading",
	"summary-lede":      "copy pointing at the removed total",
	"road-lede":         "copy pointing at the removed total",
	"muster-aside":      "notes and suffixes pointing at the removed total",
}

// rosterOnlyPaperShown carry names, or say which sheet a page belongs to,
// or say the names may be incomplete — none may leave the paper.
var rosterOnlyPaperShown = []string{
	"sheet-runner", "roster", "company", "trade", "names", "summary",
	"on-road", "road-trip", "not-tracked", "incomplete-warn", "restored-tag", "restored-note",
	// James, 2026-10-03 (W6): the departed-since list on a reprint (M-5),
	// the "also recorded on site at <peer>" tag and its legend (M-6) and the
	// per-table category header row (M-46) are all about whether the NAMES
	// on the paper are complete and true — they print.
	"checked-out-since", "elsewhere-tag", "elsewhere-note", "page-cat-row",
}

func TestThePrintedMusterIsRosterOnly(t *testing.T) {
	srv := musterWithAllThreeCategories(t)
	snapshot := rawPage(t, srv, "/muster/print")
	href := regexp.MustCompile(`href="(/muster/print\?visits=[^"]+)"`).FindStringSubmatch(snapshot)
	if href == nil {
		t.Fatalf("precondition: the snapshot mints no reprint link, so the reprint route is not exercised:\n%s", snapshot)
	}
	routes := map[string]string{
		"/muster":       rawPage(t, srv, "/muster"),
		"/muster/print": snapshot,
		"reprint":       rawPage(t, srv, strings.ReplaceAll(href[1], "&amp;", "&")),
	}
	for route, body := range routes {
		rules := paperStylesheet(t, body)
		// Precondition (rule 6): a working print cascade that hides chrome
		// AND shows something, or every answer below is vacuous.
		if _, hidden := hiddenOnPaper(rules, "topbar"); !hidden {
			t.Fatalf("%s: precondition: print hides no chrome — not a working print cascade", route)
		}
		for class, what := range rosterOnlyPaperHidden {
			if _, hidden := hiddenOnPaper(rules, class); !hidden {
				t.Errorf("%s: .%s (%s) is NOT hidden on paper — James, 2026-10-01: print = roster only.\nprint block was:\n%s", route, class, what, printBlockText(rules))
			}
		}
		for _, class := range rosterOnlyPaperShown {
			if why, hidden := hiddenOnPaper(rules, class); hidden {
				t.Errorf("%s: .%s is hidden on paper (%s) — the roster-only sheet must keep it", route, class, why)
			}
		}

		// Element-level, not class-level: every element carrying a hidden
		// class must RESOLVE to display:none on paper through ALL of its
		// selectors — tag-qualified ones included. hiddenOnPaper asks about
		// one class, and `small.note { display: block }` outranks a bare
		// `.muster-aside`; measured, the notes kept printing with the
		// class-level check green.
		elemRe := regexp.MustCompile(`<([a-z0-9]+) class="([^"]*)"`)
		for _, m := range elemRe.FindAllStringSubmatch(body, -1) {
			tag, classes := m[1], strings.Fields(m[2])
			var carries string
			for _, c := range classes {
				if _, ok := rosterOnlyPaperHidden[c]; ok {
					carries = c
				}
			}
			if carries == "" {
				continue
			}
			matches := func(sel string) bool {
				for _, c := range classes {
					last := sel[strings.LastIndexAny(sel, " >+~")+1:]
					if targetsClass(last, c) && (!regexp.MustCompile(`^[a-z]`).MatchString(last) || strings.HasPrefix(last, tag+".")) {
						return true
					}
				}
				return false
			}
			if d, r, ok := paperValue(rules, matches, "display"); !ok || strings.TrimSpace(d.value) != "none" {
				from := ""
				if r != nil {
					from = r.raw
				}
				t.Errorf("%s: <%s class=%q> carries .%s but resolves display=%q on paper (from: %s)", route, tag, m[2], carries, d.value, from)
			}
		}

		// The hidden classes are the ones the blocks actually carry: a
		// rename of the element's class would leave the rule hiding nothing.
		for section, class := range map[string]string{"sync": "sync-scope", "tally": "tally", "away-scope": "away-scope"} {
			re := regexp.MustCompile(`<section class="([^"]*)" data-muster-section="` + section + `"`)
			m := re.FindStringSubmatch(body)
			if m == nil {
				t.Fatalf("%s: precondition: no %q section rendered — the fixture is built to carry it", route, section)
			}
			if !hasClass(m[1], class) {
				t.Errorf("%s: the %q section's class is %q, not .%s — the print rule would miss it", route, section, m[1], class)
			}
		}
		if !regexp.MustCompile(`<header class="sheet-head">`).MatchString(body) {
			t.Errorf("%s: the sheet header no longer carries .sheet-head — the print rule would miss it", route)
		}

		// No count escapes onto paper: every data-tally-count lives inside
		// the (paper-hidden) .tally section.
		bare := styleRe.ReplaceAllString(body, "")
		tally := sectionBody(bare, "tally")
		if got, inTally := strings.Count(bare, "data-tally-count="), strings.Count(tally, "data-tally-count="); got != inTally || inTally == 0 {
			t.Errorf("%s: %d data-tally-count hooks on the page, %d inside .tally — a count outside the hidden block would print", route, got, inTally)
		}
	}

	// The screen half: /muster keeps the sync verdict and the counts, and
	// on screen nothing hides them.
	live := routes["/muster"]
	for _, section := range []string{"sync", "tally"} {
		if !strings.Contains(live, `data-muster-section="`+section+`"`) {
			t.Errorf("/muster lost its %q section — the ruling changed the paper, not the live page", section)
		}
	}
	rules := paperStylesheet(t, live)
	for _, class := range []string{"sheet-head", "sync-scope", "tally"} {
		if got, ok := screenClassValue(rules, class, "display"); ok && strings.TrimSpace(got) == "none" {
			t.Errorf("/muster: .%s is hidden ON SCREEN — the ruling changed the paper only", class)
		}
	}
}

// The empty "Not on site" heading is dropped from paper only when nothing
// under it prints — never when the not-tracked names are there.
func TestTheNotOnSiteHeadingPrintsOnlyOverNamesThatPrint(t *testing.T) {
	withNotTracked := rawPage(t, musterWithAllThreeCategories(t), "/muster/print")
	srv, _ := newDestinationsTestServer(t) // a configured pad, nobody not-tracked
	awayOnly := rawPage(t, srv, "/muster/print")

	offsiteClass := func(body string) string {
		m := regexp.MustCompile(`<section class="([^"]*)" data-muster-section="off-site"`).FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("precondition: no off-site section rendered:\n%s", body)
		}
		return m[1]
	}
	if !strings.Contains(withNotTracked, `data-muster-section="not-tracked"`) {
		t.Fatal("precondition: the not-tracked fixture renders no not-tracked section")
	}
	if strings.Contains(awayOnly, `data-muster-section="not-tracked"`) {
		t.Fatal("precondition: the away-only fixture renders a not-tracked section")
	}
	if hasClass(offsiteClass(withNotTracked), "offsite-unprinted") {
		t.Error("the off-site section is marked offsite-unprinted while not-tracked names are under it — those names would leave the paper")
	}
	if !hasClass(offsiteClass(awayOnly), "offsite-unprinted") {
		t.Error("with nothing under it that prints, the off-site section is not marked offsite-unprinted — the sheet would print a bare \"Not on site\" heading")
	}
}

// Every person the live muster lists is on the printed sheet — same set,
// nobody dropped by the roster-only ruling — except the away names, which
// have been off paper since 2026-08-21 and are asserted absent.
func TestThePrintedRosterNamesEveryoneTheLiveMusterLists(t *testing.T) {
	type fixture struct {
		name     string
		srv      *Server
		mustHave []string // sections this fixture must populate (rule 6)
	}
	road := newRoadWorld(t)
	folded := newRoadWorld(t)
	folded.srv.Cfg.MusterMinGroupSize = 99 // every crew folds into the summary table
	fixtures := []fixture{
		{"on site + on the road", road.srv, []string{"on-site", "in-transit"}},
		{"everyone folded into the summary group", folded.srv, []string{"folded", "in-transit"}},
		{"away + not tracked + incomplete", musterWithAllThreeCategories(t), []string{"on-site", "away", "not-tracked"}},
	}
	for _, f := range fixtures {
		live := styleRe.ReplaceAllString(rawPage(t, f.srv, "/muster"), "")
		paper := styleRe.ReplaceAllString(rawPage(t, f.srv, "/muster/print"), "")
		rules := paperStylesheet(t, rawPage(t, f.srv, "/muster/print"))

		liveBy := musterNamesBySection(live)
		for _, s := range f.mustHave {
			if len(liveBy[s]) == 0 {
				t.Fatalf("%s: precondition: /muster lists nobody under %q — this fixture does not pose its case", f.name, s)
			}
		}
		var want []string
		for s, names := range liveBy {
			if s == "away" {
				continue
			}
			want = append(want, names...)
		}

		// Paper: names in sections whose element (or enclosing off-site
		// section) is not hidden on paper.
		var got []string
		for s, names := range musterNamesBySection(paper) {
			if musterSectionHiddenOnPaper(t, paper, rules, s) {
				if s != "away" && len(names) > 0 {
					t.Errorf("%s: section %q carries %d name(s) and is hidden on paper", f.name, s, len(names))
				}
				continue
			}
			if s == "away" && len(names) > 0 {
				t.Errorf("%s: the away names reach paper: %v", f.name, names)
			}
			got = append(got, names...)
		}
		sort.Strings(want)
		sort.Strings(got)
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("%s: names on paper differ from /muster (away excluded)\n paper: %v\n live:  %v", f.name, got, want)
		}
	}
}

func hasClass(attr, class string) bool {
	for _, c := range strings.Fields(attr) {
		if c == class {
			return true
		}
	}
	return false
}

// sectionBody returns the markup of the element carrying
// data-muster-section=name, to its matching close (nesting-aware over
// <section> and <div>, which are the only containers these hooks sit on).
func sectionBody(body, name string) string {
	at := strings.Index(body, `data-muster-section="`+name+`"`)
	if at < 0 {
		return ""
	}
	start := strings.LastIndex(body[:at], "<")
	tag := body[start+1 : start+1+strings.IndexAny(body[start+1:], " >")]
	depth, i := 0, start
	for i < len(body) {
		o := strings.Index(body[i:], "<"+tag)
		c := strings.Index(body[i:], "</"+tag+">")
		if c < 0 {
			return body[start:]
		}
		if o >= 0 && o < c {
			depth++
			i += o + 1
			continue
		}
		depth--
		i += c + len("</"+tag+">")
		if depth == 0 {
			return body[start:i]
		}
	}
	return body[start:]
}

var (
	musterLiRe   = regexp.MustCompile(`(?s)<ul class="names">(.*?)</ul>`)
	musterItemRe = regexp.MustCompile(`(?s)<li>(.*?)</li>`)
	musterRowRe  = regexp.MustCompile(`(?s)<tbody>(.*?)</tbody>`)
	musterTdRe   = regexp.MustCompile(`(?s)<tr>\s*<td>(.*?)</td>`)
	musterTagRe  = regexp.MustCompile(`(?s)<span class="restored-tag"[^>]*>.*?</span>|<[^>]+>`)
)

// musterNamesBySection reads the person names each roster section lists.
// "on-site" excludes the summary table, which is read as "folded".
func musterNamesBySection(body string) map[string][]string {
	clean := func(s string) string {
		return strings.TrimSpace(html.UnescapeString(musterTagRe.ReplaceAllString(s, "")))
	}
	out := map[string][]string{}
	lis := func(sec string) (names []string) {
		for _, ul := range musterLiRe.FindAllStringSubmatch(sec, -1) {
			for _, li := range musterItemRe.FindAllStringSubmatch(ul[1], -1) {
				names = append(names, clean(li[1]))
			}
		}
		return
	}
	firstCells := func(sec string) (names []string) {
		for _, tb := range musterRowRe.FindAllStringSubmatch(sec, -1) {
			for _, td := range musterTdRe.FindAllStringSubmatch(tb[1], -1) {
				names = append(names, clean(td[1]))
			}
		}
		return
	}
	onSite := sectionBody(body, "on-site")
	folded := sectionBody(body, "folded")
	out["on-site"] = lis(strings.Replace(onSite, folded, "", 1))
	out["folded"] = firstCells(folded)
	out["in-transit"] = lis(sectionBody(body, "in-transit"))
	out["not-tracked"] = firstCells(sectionBody(body, "not-tracked"))
	out["away"] = firstCells(sectionBody(body, "away"))
	return out
}

// musterSectionHiddenOnPaper reports whether the section's own element, the
// on-site section around the summary table, or the off-site section around
// the away and not-tracked lists, carries a class the print cascade hides.
func musterSectionHiddenOnPaper(t *testing.T, body string, rules []cssRule, section string) bool {
	t.Helper()
	classOf := func(sec string) string {
		m := regexp.MustCompile(`class="([^"]*)" data-muster-section="` + sec + `"`).FindStringSubmatch(body)
		if m == nil {
			return ""
		}
		return m[1]
	}
	chain := []string{section}
	switch section {
	case "folded":
		chain = append(chain, "on-site")
	case "away", "not-tracked":
		chain = append(chain, "off-site")
	}
	for _, s := range chain {
		for _, c := range strings.Fields(classOf(s)) {
			if _, hidden := hiddenOnPaper(rules, c); hidden {
				return true
			}
		}
	}
	return false
}

// Every printed page identifies itself: with the header blocks off paper,
// the roster runs from page 1 onto later pages, and a loose page must say
// which sheet it is from and which page it is. The @page margin boxes
// carry it; their text comes from --muster-sheet-id, which must name the
// post, the sheet's kind and this render's time — and must not be
// html/template's #ZgotmplZ refusal, which an earlier draft of this very
// rule printed on every page (measured).
func TestEveryPrintedMusterPageCarriesTheSheetIdentityAndAPageNumber(t *testing.T) {
	srv := musterWithAllThreeCategories(t)
	for route, mode := range map[string]string{"/muster": "LIVE", "/muster/print": "PRINTED FROM LIVE"} {
		css := stylesheetOf(t, rawPage(t, srv, route))
		m := regexp.MustCompile(`--muster-sheet-id: "([^"]*)"`).FindStringSubmatch(css)
		if m == nil {
			t.Fatalf("%s: no --muster-sheet-id in the stylesheet — later printed pages would not say which sheet they belong to", route)
		}
		// CSS-escaped by html/template (":" -> "\3a "); unescape the one
		// escape a stamp contains before comparing.
		id := strings.ReplaceAll(m[1], `\3a `, ":")
		for _, want := range []string{"Muster · " + srv.Cfg.SiteName("site-b") + " · " + mode + " · "} /* James, 2026-10-04 (cluster I): the stamp names the post by display name ⟦was: site-b⟧ */ {
			if !strings.Contains(id, want) {
				t.Errorf("%s: sheet id %q does not carry %q", route, id, want)
			}
		}
		if strings.Contains(id, "ZgotmplZ") || !stampNamesRenderTime(id) {
			t.Errorf("%s: sheet id %q does not carry this render's time", route, id)
		}
		for _, want := range []string{`@top-left { content: var(--muster-sheet-id`, `@top-right { content: "page " counter(page) " of " counter(pages)`} {
			if !strings.Contains(css, want) {
				t.Errorf("%s: the print block lacks %q", route, want)
			}
		}
	}
	// Only the muster sets it: other pages get no sheet identity.
	other, _ := newMusterTestServer(t, 0)
	if strings.Contains(stylesheetOf(t, rawPage(t, other, "/trips")), "--muster-sheet-id: \"") {
		t.Error("/trips carries a muster sheet identity")
	}
}
