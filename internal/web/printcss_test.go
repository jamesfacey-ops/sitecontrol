// printcss_test.go — the stylesheet parser the print guards stand on.
//
// WHY THIS FILE EXISTS (V-03 + M-02, one rewrite).
//
// The print guards used to be same-line substring scans:
//
//	for _, line := range strings.Split(block, "\n") {
//	    if strings.Contains(line, sel) && strings.Contains(line, "display: none") { ... }
//	}
//
// That mechanism cannot see three things, and each of them hides a person
// from the printed evacuation sheet with the whole suite green:
//
//  1. A MULTI-LINE SELECTOR LIST. The file's own house style already writes
//     one (the :root palette reset at the head of the print block spans four
//     lines), so hiding `.away` by adding it to a wrapped selector list is
//     a legal, idiomatic edit that no same-line scan can catch.
//  2. WHITESPACE AND SPELLING VARIANTS. `display:none` (no space) passes a
//     `Contains(line, "display: none")` scan, as do `visibility: hidden`,
//     `font-size: 0` and `height: 0` — four different ways to take a section
//     off the paper.
//  3. SOURCE ORDER. A print declaration only wins because of where it sits.
//     Relocating the whole @media print block (M-02) leaves every substring
//     assertion true while the sheet prints wrong. The old
//     TestTheStylesheetHasExactlyOnePrintBlock existed purely to compensate
//     for the first-block-only scan, and said so in its own comment.
//
// So the guards now run on a real parse: every rule in the served
// stylesheet, its selector list flattened, its declarations in source order,
// resolved through an actual cascade (importance, then specificity, then
// source order) restricted to the rules that apply on PAPER. What the
// guards ask is "what value does this property actually resolve to when
// this sheet is printed", which is the question they were always meant to
// ask.
//
// Scope, stated honestly: this is a cascade over one flat inlined
// stylesheet, not a browser. It resolves the cases the print guards need —
// class-targeting rules and the :root palette blocks — and it deliberately
// OVER-matches on class selectors (`.sheet .away` counts as targeting
// `.away`), because for a "this must never be hidden" guard the
// over-matching direction is the safe one.
package web

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// --- the parsed shape ---

type cssDecl struct {
	prop      string
	value     string
	important bool
}

// cssRule is one style rule, flattened out of whatever at-rules enclose it.
type cssRule struct {
	selectors []string // the comma-separated list, each trimmed, newlines collapsed
	decls     []cssDecl
	inPrint   bool // lexically inside an @media whose query names print
	forPaper  bool // applies when printing (i.e. not inside a screen-only @media)
	order     int  // source order across the whole stylesheet
	raw       string
}

var (
	cssCommentRe = regexp.MustCompile(`(?s)/\*.*?\*/`)
	cssWSRe      = regexp.MustCompile(`\s+`)
)

// declarationAtRules carry declarations rather than nested style rules, so
// their bodies must not be parsed as selectors.
var declarationAtRules = map[string]bool{"@page": true, "@font-face": true, "@counter-style": true, "@property": true}

// parseCSS flattens the stylesheet into rules. It tracks the at-rule stack
// so a rule knows whether it is inside an @media print, and whether it
// applies on paper at all.
func parseCSS(t *testing.T, css string) []cssRule {
	t.Helper()
	css = cssCommentRe.ReplaceAllString(css, " ")

	type frame struct{ isPrint, screenOnly bool }
	var stack []frame
	var out []cssRule

	inPrint := func() bool {
		for _, f := range stack {
			if f.isPrint {
				return true
			}
		}
		return false
	}
	forPaper := func() bool {
		for _, f := range stack {
			if f.screenOnly {
				return false
			}
		}
		return true
	}

	var prelude strings.Builder
	for i := 0; i < len(css); i++ {
		switch css[i] {
		case '{':
			head := strings.TrimSpace(prelude.String())
			prelude.Reset()
			if strings.HasPrefix(head, "@") {
				name := head
				if sp := strings.IndexAny(head, " \t\n("); sp >= 0 {
					name = head[:sp]
				}
				if declarationAtRules[strings.ToLower(name)] {
					// Body is declarations, not rules — skip it whole.
					i = skipBlock(t, css, i)
					continue
				}
				q := strings.ToLower(head)
				isPrint := strings.HasPrefix(q, "@media") && mediaWord(q, "print")
				screenOnly := strings.HasPrefix(q, "@media") && !mediaWord(q, "print") &&
					(mediaWord(q, "screen") || mediaWord(q, "speech"))
				stack = append(stack, frame{isPrint: isPrint, screenOnly: screenOnly})
				continue
			}
			// A style rule. Its body runs to the matching brace.
			end := skipBlock(t, css, i)
			body := css[i+1 : end]
			out = append(out, cssRule{
				selectors: splitSelectors(head),
				decls:     parseDecls(body),
				inPrint:   inPrint(),
				forPaper:  forPaper(),
				order:     len(out),
				raw:       strings.TrimSpace(head) + " {" + body + "}",
			})
			i = end
		case '}':
			prelude.Reset()
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		default:
			prelude.WriteByte(css[i])
		}
	}
	if len(out) == 0 {
		t.Fatal("parseCSS found no rules at all — the parser is not looking at a stylesheet")
	}
	return out
}

// mediaWord reports whether a media query names a bare media TYPE (print,
// screen), rather than merely containing the letters somewhere.
func mediaWord(query, word string) bool {
	re := regexp.MustCompile(`(^|[^-\w])` + word + `([^-\w]|$)`)
	return re.MatchString(query)
}

// skipBlock returns the index of the '}' matching the '{' at open.
func skipBlock(t *testing.T, css string, open int) int {
	t.Helper()
	depth := 0
	for i := open; i < len(css); i++ {
		switch css[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	t.Fatalf("unterminated block starting at offset %d", open)
	return len(css)
}

func splitSelectors(head string) []string {
	var out []string
	for _, s := range strings.Split(head, ",") {
		s = strings.TrimSpace(cssWSRe.ReplaceAllString(s, " "))
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func parseDecls(body string) []cssDecl {
	var out []cssDecl
	for _, chunk := range strings.Split(body, ";") {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		k := strings.Index(chunk, ":")
		if k < 0 {
			continue
		}
		prop := strings.ToLower(strings.TrimSpace(chunk[:k]))
		val := strings.TrimSpace(cssWSRe.ReplaceAllString(chunk[k+1:], " "))
		important := false
		if lv := strings.ToLower(val); strings.HasSuffix(lv, "!important") {
			important = true
			val = strings.TrimSpace(val[:len(val)-len("!important")])
		}
		if prop == "" {
			continue
		}
		out = append(out, cssDecl{prop: prop, value: val, important: important})
	}
	return out
}

// --- specificity ---

var (
	specIDRe    = regexp.MustCompile(`#[-\w]+`)
	specClassRe = regexp.MustCompile(`\.[-\w]+|\[[^\]]*\]|:[-\w]+(\([^)]*\))?`)
	specElemRe  = regexp.MustCompile(`(^|[\s>+~])([a-zA-Z][-\w]*)`)
	specNotRe   = regexp.MustCompile(`:(not|is|has)\(([^)]*)\)`)
)

// specificity returns CSS specificity as a comparable triple. :not()/:is()
// take the specificity of their most specific argument and contribute
// nothing themselves, which is what makes `:root:not([data-theme="light"])`
// and `:root[data-theme="dark"]` tie — the tie is exactly what M-02's
// relocation mutation turns on.
func specificity(sel string) [3]int {
	var extra [3]int
	inner := sel
	for _, m := range specNotRe.FindAllStringSubmatch(sel, -1) {
		best := [3]int{}
		for _, arg := range strings.Split(m[2], ",") {
			if s := specificity(strings.TrimSpace(arg)); s != best && less(best, s) {
				best = s
			}
		}
		for i := range extra {
			extra[i] += best[i]
		}
		inner = strings.Replace(inner, m[0], " ", 1)
	}
	a := len(specIDRe.FindAllString(inner, -1))
	b := len(specClassRe.FindAllString(specIDRe.ReplaceAllString(inner, " "), -1))
	c := 0
	for _, m := range specElemRe.FindAllStringSubmatch(" "+specClassRe.ReplaceAllString(specIDRe.ReplaceAllString(inner, " "), " "), -1) {
		if m[2] != "" {
			c++
		}
	}
	return [3]int{a + extra[0], b + extra[1], c + extra[2]}
}

func less(x, y [3]int) bool {
	for i := range x {
		if x[i] != y[i] {
			return x[i] < y[i]
		}
	}
	return false
}

// --- matching ---

// targetsClass reports whether a selector targets elements carrying class.
// It matches the class as a whole token anywhere in the selector, so
// `.away`, `.sheet .away` and `.away.foo` all count. Over-matching is the
// safe direction for a guard whose failure mode is "this section silently
// left the paper".
func targetsClass(sel, class string) bool {
	re := regexp.MustCompile(`\.` + regexp.QuoteMeta(class) + `([^-\w]|$)`)
	return re.MatchString(sel + " ")
}

func matchesExact(sel, want string) bool {
	return sel == want
}

// paperValue resolves one property for one selector as it lands ON PAPER:
// every rule that applies when printing, ordered by importance, then
// specificity, then source order. Returns the winning declaration and the
// rule it came from.
func paperValue(rules []cssRule, match func(sel string) bool, prop string) (cssDecl, *cssRule, bool) {
	return resolveValue(rules, func(r *cssRule) bool { return r.forPaper }, match, prop)
}

// screenValue is paperValue's ON-SCREEN twin: the same cascade, over the
// rules that apply to a display rather than to paper.
//
// It exists for R7c. The console's countdown guard asserted its rule by
// `strings.Contains(css, ".countdown { font-size: var(--fs-countdown)")` —
// a SAME-LINE SUBSTRING SCAN, which is the exact mechanism V-03 was raised
// to remove from this package and which the print guards were rewritten
// away from. Its sibling at muster_print_test.go was fixed by deletion when
// the `rule()` helper went; this one was logged and left, and this is it.
//
// Three ways the substring scan was wrong, none of them hypothetical in a
// file whose own house style is multi-line selector lists:
//
//	.countdown,\n.board-lead .countdown { font-size: var(--fs-countdown); }
//	  — reformatted onto two lines, the scan misses and reports a defect
//	    that is not there;
//	.countdown { font-size: var(--fs-countdown); }  … later …
//	.board-lead .countdown { font-size: 0.75rem; }
//	  — the scan still matches, but the countdown now prints at 0.75rem on
//	    the board. A rule LATER and MORE SPECIFIC wins, and a substring
//	    scan has no concept of either.
//
// screenValue resolves the cascade, so what is asserted is what the
// element actually gets.
func screenValue(rules []cssRule, match func(sel string) bool, prop string) (cssDecl, *cssRule, bool) {
	return resolveValue(rules, func(r *cssRule) bool { return !r.inPrint }, match, prop)
}

// screenClassValue is the class-selector form of screenValue.
func screenClassValue(rules []cssRule, class, prop string) (string, bool) {
	d, _, ok := screenValue(rules, func(sel string) bool { return targetsClass(sel, class) }, prop)
	if !ok {
		return "", false
	}
	return d.value, true
}

// resolveValue is the shared cascade: importance, then specificity, then
// source order, over whichever rules `applies` admits.
func resolveValue(rules []cssRule, applies func(*cssRule) bool, match func(sel string) bool, prop string) (cssDecl, *cssRule, bool) {
	type cand struct {
		decl cssDecl
		spec [3]int
		ord  int
		rule *cssRule
	}
	var best *cand
	for i := range rules {
		r := &rules[i]
		if !applies(r) {
			continue
		}
		for _, sel := range r.selectors {
			if !match(sel) {
				continue
			}
			spec := specificity(sel)
			for _, d := range r.decls {
				if d.prop != prop {
					continue
				}
				c := cand{decl: d, spec: spec, ord: r.order, rule: r}
				if best == nil || winsOver(c.decl.important, c.spec, c.ord, best.decl.important, best.spec, best.ord) {
					cp := c
					best = &cp
				}
			}
		}
	}
	if best == nil {
		return cssDecl{}, nil, false
	}
	return best.decl, best.rule, true
}

func winsOver(imp bool, spec [3]int, ord int, oimp bool, ospec [3]int, oord int) bool {
	if imp != oimp {
		return imp
	}
	if spec != ospec {
		return less(ospec, spec)
	}
	return ord > oord
}

// --- the question the print guards actually ask ---

// hidingProps are the four ways a section leaves the paper. A guard that
// knows only `display: none` is a guard three edits wide.
var hidingProps = []struct{ prop, value string }{
	{"display", "none"},
	{"visibility", "hidden"},
	{"font-size", "0"},
	{"height", "0"},
}

// hiddenOnPaper reports whether elements carrying class resolve, in print,
// to any of the four hiding forms — naming which one and the rule that did
// it, so a failure is diagnosable rather than merely red.
func hiddenOnPaper(rules []cssRule, class string) (string, bool) {
	match := func(sel string) bool { return targetsClass(sel, class) }
	for _, h := range hidingProps {
		d, r, ok := paperValue(rules, match, h.prop)
		if !ok {
			continue
		}
		if normValue(d.value) == h.value {
			return h.prop + ": " + d.value + " (from: " + strings.TrimSpace(r.raw) + ")", true
		}
	}
	return "", false
}

// normValue strips units that make a zero look like a value ("0px", "0pt",
// "0rem", "0%") so `font-size: 0px` cannot slip past a check for `0`.
func normValue(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	for _, unit := range []string{"px", "pt", "rem", "em", "%", "mm", "cm", "in", "pc", "ex", "ch", "vh", "vw"} {
		if strings.HasSuffix(v, unit) {
			if base := strings.TrimSpace(strings.TrimSuffix(v, unit)); base == "0" || base == "0.0" || base == ".0" {
				return "0"
			}
		}
	}
	if v == "0.0" || v == ".0" {
		return "0"
	}
	return v
}

// paperClassValue is the class-selector form of paperValue, for the
// properties the print guards read directly (border-style, and so on).
func paperClassValue(rules []cssRule, class, prop string) (string, bool) {
	d, _, ok := paperValue(rules, func(sel string) bool { return targetsClass(sel, class) }, prop)
	if !ok {
		return "", false
	}
	return d.value, true
}

// printRules returns the rules lexically inside an @media print block —
// used only where a test is genuinely about the print block's own content
// rather than about what resolves onto paper.
func printRules(rules []cssRule) []cssRule {
	var out []cssRule
	for _, r := range rules {
		if r.inPrint {
			out = append(out, r)
		}
	}
	return out
}

// paperStylesheet parses the stylesheet as served on a real page.
func paperStylesheet(t *testing.T, body string) []cssRule {
	t.Helper()
	return parseCSS(t, stylesheetOf(t, body))
}

// --- the parser's own tests. A guard mechanism nobody tests is the defect
// class this whole rewrite exists to remove, so the parser is held to the
// four evasions it was built to close, plus the cascade. ---

func TestPrintCSSParserSeesTheEvasionsASubstringScanCannot(t *testing.T) {
	const sheet = `
.away { border: 1px solid var(--line); }
.not-tracked { border: 2px dashed var(--line); }
@media screen {
  .screen-only-hidden { display: none; }
}
@media print {
  @page { margin: 10mm; }
  .topbar,
  .banner-notice,
  .away { display: none; }
  .squashed { font-size: 0px; }
  .flattened { height: 0; }
  .invisible { visibility: hidden; }
  .tight{display:none}
  .not-tracked { border: 2px dashed #000; }
}
`
	rules := parseCSS(t, sheet)

	// 1. the multi-line selector list — the house style's own shape.
	if why, hidden := hiddenOnPaper(rules, "away"); !hidden {
		t.Error("parser missed .away hidden by a WRAPPED selector list — this is exactly the V-03 evasion")
	} else if !strings.Contains(why, "display") {
		t.Errorf("wrong reason for .away: %s", why)
	}
	// 2-4. the three non-display hiding forms, plus the whitespace-free one.
	for _, class := range []string{"squashed", "flattened", "invisible", "tight"} {
		if _, hidden := hiddenOnPaper(rules, class); !hidden {
			t.Errorf(".%s is hidden on paper and the parser did not see it", class)
		}
	}
	// A screen-only rule must NOT count as hiding something on paper.
	if _, hidden := hiddenOnPaper(rules, "screen-only-hidden"); hidden {
		t.Error("a rule inside @media screen was treated as applying on paper")
	}
	// @page carries declarations, not rules — its body must not be read as
	// a selector list.
	for _, r := range rules {
		for _, sel := range r.selectors {
			if strings.Contains(sel, "margin") {
				t.Errorf("@page's body was parsed as a style rule: %q", sel)
			}
		}
	}
	// The cascade: the print border wins over the screen one by source order.
	if v, ok := paperClassValue(rules, "not-tracked", "border"); !ok || !strings.Contains(v, "#000") {
		t.Errorf(".not-tracked border on paper = %q, want the print block's value", v)
	}
}

// M-02's mutation, executed rather than asserted about: relocating the whole
// @media print block ahead of the rules it exists to override must change
// what resolves on paper. A count of print blocks, or any substring scan,
// is blind to this by construction.
func TestPrintCSSParserIsOrderAwareSoRelocatingThePrintBlockIsVisible(t *testing.T) {
	const printBlk = `@media print {
  :root, :root:not([data-theme="light"]), :root[data-theme="dark"] { --ground: #FFFFFF; }
}
`
	const darkBlk = `@media (prefers-color-scheme: dark) {
  :root:not([data-theme="light"]) { --ground: #0F1214; }
}
`
	const light = `:root { --ground: #F2F6F8; }
`
	dark := func(sel string) func(string) bool {
		return func(s string) bool { return matchesExact(s, sel) }
	}

	correct := parseCSS(t, light+darkBlk+printBlk)
	d, _, ok := paperValue(correct, dark(`:root:not([data-theme="light"])`), "--ground")
	if !ok || d.value != "#FFFFFF" {
		t.Fatalf("with the print block last, --ground on paper = %q, want #FFFFFF", d.value)
	}

	relocated := parseCSS(t, printBlk+light+darkBlk)
	d, _, ok = paperValue(relocated, dark(`:root:not([data-theme="light"])`), "--ground")
	if !ok || d.value == "#FFFFFF" {
		t.Fatal("M-02: the print block was relocated ahead of the dark palette and the parser still reported a white ground — the guard is order-blind and the sheet would print dark on dark")
	}
}

func TestPrintCSSSpecificityTiesResolveBySourceOrder(t *testing.T) {
	// The two selectors the print palette reset has to beat are exactly
	// equal in specificity, so ONLY order separates them. If this ever
	// stops being true the palette test's guarantee changes shape.
	a := specificity(`:root:not([data-theme="light"])`)
	b := specificity(`:root[data-theme="dark"]`)
	if a != b {
		t.Errorf("specificity(:root:not([data-theme=light])) = %v, specificity(:root[data-theme=dark]) = %v — the print reset's guarantee no longer rests on source order alone", a, b)
	}
	if got := specificity(".away"); got != [3]int{0, 1, 0} {
		t.Errorf("specificity(.away) = %v, want [0 1 0]", got)
	}
	if got := specificity("nav.screen-only"); got != [3]int{0, 1, 1} {
		t.Errorf("specificity(nav.screen-only) = %v, want [0 1 1]", got)
	}
}

// A convenience the guards use to report the print block's own text when
// they fail. Kept small and honest: it is for diagnostics, never for
// assertions.
func printBlockText(rules []cssRule) string {
	var sb strings.Builder
	pr := printRules(rules)
	sort.Slice(pr, func(i, j int) bool { return pr[i].order < pr[j].order })
	for _, r := range pr {
		sb.WriteString(r.raw)
		sb.WriteString("\n")
	}
	return sb.String()
}

// --- T1 #5: the literal-colour ban ------------------------------------
//
// This is one of the eight stated brief criteria that had no asserting
// test of any kind, and it is the one with a safety chain behind it.
//
// F1b: the printed muster is guaranteed black-on-white whatever the
// viewer's theme is, and that guarantee is TOTAL only because every
// component colour in this stylesheet resolves through one of the sixteen
// palette tokens that the @media print block resets. A single hard-coded
// `color: #B0241C` on a component is a colour the reset cannot reach — it
// survives onto paper, and on a mono laser it survives as a grey that may
// or may not be legible. The criterion holds today (verified: every literal
// in the sheet is either a token declaration in a palette root or one of
// the print block's own resets), and nothing would have announced it if it
// broke.
//
// The palette roots and the print block are exempt BY CONSTRUCTION, not by
// exception: they are where literals are supposed to be. Everything else
// goes through var().
var (
	hexLiteralRe   = regexp.MustCompile(`#[0-9A-Fa-f]{3,8}\b`)
	funcColourRe   = regexp.MustCompile(`\b(rgba?|hsla?|lab|lch|oklab|oklch|color)\(`)
	namedColourRe  = regexp.MustCompile(`(^|[^-\w])(aliceblue|antiquewhite|aqua|aquamarine|azure|beige|bisque|black|blanchedalmond|blue|blueviolet|brown|burlywood|cadetblue|chartreuse|chocolate|coral|cornflowerblue|cornsilk|crimson|cyan|darkblue|darkcyan|darkgoldenrod|darkgray|darkgrey|darkgreen|darkkhaki|darkmagenta|darkolivegreen|darkorange|darkorchid|darkred|darksalmon|darkseagreen|darkslateblue|darkslategray|darkslategrey|darkturquoise|darkviolet|deeppink|deepskyblue|dimgray|dimgrey|dodgerblue|firebrick|floralwhite|forestgreen|fuchsia|gainsboro|ghostwhite|gold|goldenrod|gray|grey|green|greenyellow|honeydew|hotpink|indianred|indigo|ivory|khaki|lavender|lavenderblush|lawngreen|lemonchiffon|lightblue|lightcoral|lightcyan|lightgoldenrodyellow|lightgray|lightgrey|lightgreen|lightpink|lightsalmon|lightseagreen|lightskyblue|lightslategray|lightslategrey|lightsteelblue|lightyellow|lime|limegreen|linen|magenta|maroon|mediumaquamarine|mediumblue|mediumorchid|mediumpurple|mediumseagreen|mediumslateblue|mediumspringgreen|mediumturquoise|mediumvioletred|midnightblue|mintcream|mistyrose|moccasin|navajowhite|navy|oldlace|olive|olivedrab|orange|orangered|orchid|palegoldenrod|palegreen|paleturquoise|palevioletred|papayawhip|peachpuff|peru|pink|plum|powderblue|purple|rebeccapurple|red|rosybrown|royalblue|saddlebrown|salmon|sandybrown|seagreen|seashell|sienna|silver|skyblue|slateblue|slategray|slategrey|snow|springgreen|steelblue|tan|teal|thistle|tomato|turquoise|violet|wheat|white|whitesmoke|yellow|yellowgreen)([^-\w]|$)`)
	paletteRootSel = map[string]bool{
		`:root`:                           true,
		`:root:not([data-theme="light"])`: true,
		`:root[data-theme="dark"]`:        true,
		`:root[data-theme="light"]`:       true,
	}
)

func colourLiteralIn(value string) string {
	if m := hexLiteralRe.FindString(value); m != "" {
		return m
	}
	if m := funcColourRe.FindString(value); m != "" {
		return m
	}
	if m := namedColourRe.FindStringSubmatch(value); m != nil {
		return m[2]
	}
	return ""
}

func TestNoComponentRuleHardCodesAColourOutsideThePalette(t *testing.T) {
	srv, _ := newMusterTestServer(t, 0)
	rules := paperStylesheet(t, rawPage(t, srv, "/muster/print"))

	// Precondition: the palette roots really do carry literals, or the
	// exemption below is exempting nothing and this test is vacuous.
	palettes := 0
	for _, r := range rules {
		if r.inPrint {
			continue
		}
		for _, sel := range r.selectors {
			if !paletteRootSel[sel] {
				continue
			}
			for _, d := range r.decls {
				if colourLiteralIn(d.value) != "" {
					palettes++
				}
			}
		}
	}
	if palettes < 16 {
		t.Fatalf("precondition: only %d colour literals found in the palette roots — this test is not looking at the palette, so its exemption proves nothing", palettes)
	}

	checked := 0
	for _, r := range rules {
		if r.inPrint {
			continue // the print block's literals ARE the reset
		}
		isPalette := false
		for _, sel := range r.selectors {
			if paletteRootSel[sel] {
				isPalette = true
			}
		}
		if isPalette {
			continue
		}
		for _, d := range r.decls {
			checked++
			if lit := colourLiteralIn(d.value); lit != "" {
				t.Errorf("%s hard-codes the colour %q in `%s: %s`. Every component colour must resolve through a palette token: the printed muster is black-on-white ONLY because @media print resets those sixteen tokens, and a literal is a colour that reset cannot reach — it survives onto the evacuation sheet. Use var(--token).",
					strings.Join(r.selectors, ", "), lit, d.prop, d.value)
			}
		}
	}
	if checked < 100 {
		t.Fatalf("precondition: only %d component declarations were checked — this sweep is not covering the stylesheet", checked)
	}
}
