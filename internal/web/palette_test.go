// palette_test.go — the two palette properties that are about what an
// operator can actually SEE, measured rather than asserted about.
//
//	M-10  OVERDUE and VERIFY-BY-RADIO must stay distinguishable without
//	      colour vision. Measured: in the LIGHT theme, --critical #B0241C
//	      and --warning #8A5600 simulated to dE 2.34 with dL* 0.37 under
//	      deuteranopia — the two states' chrome on the busiest screen
//	      became the same mark. Light is what a stock gate PC delivers: no
//	      template sets data-theme and there is no theme control in the UI.
//
//	M-11  --line is the only thing separating a form field from its
//	      container, and in the dark theme it measured 1.25-1.61 : 1 — a
//	      hairline nobody can find in 24-hour glare on a cheap panel.
//	      M-13 rides on the same token: .not-tracked's 2px dashed box is
//	      drawn in it, and that box is the form the whole
//	      away-vs-not-tracked distinction rests on.
//
// Both are computed from the SERVED stylesheet's own token values, so a
// future edit is measured rather than trusted.
package web

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

// --- colour maths ---

func srgbToLinear(c float64) float64 {
	c /= 255
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func linearToSRGB(c float64) float64 {
	c = math.Max(0, math.Min(1, c))
	if c <= 0.0031308 {
		return 12.92 * c
	}
	return 1.055*math.Pow(c, 1/2.4) - 0.055
}

func parseHex(t *testing.T, s string) [3]float64 {
	t.Helper()
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "#"))
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		t.Fatalf("not a hex colour: %q", s)
	}
	var out [3]float64
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseUint(s[i*2:i*2+2], 16, 8)
		if err != nil {
			t.Fatalf("not a hex colour: %q", s)
		}
		out[i] = float64(v)
	}
	return out
}

func relLuminance(rgb [3]float64) float64 {
	return 0.2126*srgbToLinear(rgb[0]) + 0.7152*srgbToLinear(rgb[1]) + 0.0722*srgbToLinear(rgb[2])
}

// contrastRatio is WCAG 2.x (L1+0.05)/(L2+0.05).
func contrastRatio(a, b [3]float64) float64 {
	la, lb := relLuminance(a), relLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func matmul(m [3][3]float64, v [3]float64) [3]float64 {
	var out [3]float64
	for i := 0; i < 3; i++ {
		out[i] = m[i][0]*v[0] + m[i][1]*v[1] + m[i][2]*v[2]
	}
	return out
}

// Vienot/Brettel/Mollon dichromat simulation: sRGB -> linear -> LMS ->
// projection onto the dichromat plane -> back. The same method the
// accessibility measurement used.
var (
	rgbToLMS = [3][3]float64{
		{17.8824, 43.5161, 4.11935},
		{3.45565, 27.1554, 3.86714},
		{0.0299566, 0.184309, 1.46709},
	}
	lmsToRGB = [3][3]float64{
		{0.080944, -0.130504, 0.116721},
		{-0.0102485, 0.0540194, -0.113615},
		{-0.000365294, -0.00412163, 0.693513},
	}
	deuteranopia = [3][3]float64{{1, 0, 0}, {0.494207, 0, 1.24827}, {0, 0, 1}}
	protanopia   = [3][3]float64{{0, 2.02344, -2.52581}, {0, 1, 0}, {0, 0, 1}}
)

func simulate(rgb [3]float64, proj [3][3]float64) [3]float64 {
	var lin [3]float64
	for i := range rgb {
		lin[i] = srgbToLinear(rgb[i]) * 255
	}
	out := matmul(lmsToRGB, matmul(proj, matmul(rgbToLMS, lin)))
	var back [3]float64
	for i := range out {
		back[i] = linearToSRGB(out[i]/255) * 255
	}
	return back
}

func toLab(rgb [3]float64) [3]float64 {
	r, g, b := srgbToLinear(rgb[0]), srgbToLinear(rgb[1]), srgbToLinear(rgb[2])
	x := (0.4124*r + 0.3576*g + 0.1805*b) / 0.95047
	y := 0.2126*r + 0.7152*g + 0.0722*b
	z := (0.0193*r + 0.1192*g + 0.9505*b) / 1.08883
	f := func(t float64) float64 {
		if t > 0.008856 {
			return math.Cbrt(t)
		}
		return 7.787*t + 16.0/116.0
	}
	fx, fy, fz := f(x), f(y), f(z)
	return [3]float64{116*fy - 16, 500 * (fx - fy), 200 * (fy - fz)}
}

// deltaE76 and the lightness difference. dL* is reported separately because
// it is the half that survives EVERY form of colour vision loss: two marks
// separated only in hue can collapse, two separated in lightness cannot.
func deltaE(a, b [3]float64) (float64, float64) {
	la, lb := toLab(a), toLab(b)
	d := 0.0
	for i := 0; i < 3; i++ {
		d += (la[i] - lb[i]) * (la[i] - lb[i])
	}
	return math.Sqrt(d), math.Abs(la[0] - lb[0])
}

// --- the palettes, read from the served stylesheet ---

func themeTokens(t *testing.T, rules []cssRule, sel string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, r := range rules {
		if r.inPrint {
			continue
		}
		for _, s := range r.selectors {
			if s != sel {
				continue
			}
			for _, d := range r.decls {
				if strings.HasPrefix(d.prop, "--") {
					out[d.prop] = d.value
				}
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("no tokens found for %s", sel)
	}
	return out
}

func TestTheAlertHuesStaySeparableWithoutColourVision(t *testing.T) {
	rules := paperStylesheet(t, getStatusPage(t, newTestServer(t)))

	// The board's two most safety-critical states. They sit next to each
	// other on the busiest screen, and the operator scans for the
	// difference.
	for _, theme := range []struct{ name, sel string }{
		{"light", ":root"},
		{"dark", `:root:not([data-theme="light"])`},
	} {
		tokens := themeTokens(t, rules, theme.sel)
		crit := parseHex(t, tokens["--critical"])
		warn := parseHex(t, tokens["--warning"])

		// Precondition: they must be different colours at all.
		if e, _ := deltaE(crit, warn); e < 10 {
			t.Fatalf("%s: --critical and --warning are barely different even in full colour (dE %.2f)", theme.name, e)
		}

		for _, vision := range []struct {
			name string
			proj [3][3]float64
		}{{"deuteranopia", deuteranopia}, {"protanopia", protanopia}} {
			sc, sw := simulate(crit, vision.proj), simulate(warn, vision.proj)
			e, dl := deltaE(sc, sw)
			// dL* is the binding one. A pair that separates only in hue is
			// a pair that can collapse; a pair separated in LIGHTNESS
			// survives any dichromacy, and greyscale printing besides.
			if dl < 3 {
				t.Errorf("%s theme, %s: OVERDUE (--critical %s) and VERIFY BY RADIO (--warning %s) differ by only dL* %.2f (dE %.2f) — on the board's two most urgent states, on the theme a stock gate PC delivers, that is the same mark. Separate them in LIGHTNESS, not in hue.",
					theme.name, vision.name, tokens["--critical"], tokens["--warning"], dl, e)
			}
			t.Logf("%s / %-13s: dE %5.2f  dL* %5.2f", theme.name, vision.name, e, dl)
		}
	}
}

// M-11 + M-13 + E5. --line is the only thing separating a form field from
// its container and the only thing drawing .not-tracked's dashed box, so a
// hairline nobody can see is a distinction nobody can read.
//
// SCOPE, stated so it is not mistaken for more than it is: this asserts the
// DARK theme only. The dark palette is brief-stated (UI_BRIEF.md lists it
// in full), so a dark value can be derived against the brief's own grounds.
// The LIGHT palette is not stated anywhere, so a light threshold would be a
// number invented here rather than derived — and inventing one is how a
// guard starts asserting the author's taste. Light --line is therefore a
// KNOWN, UNASSERTED GAP, recorded rather than papered over.
func TestTheDarkHairlineIsVisibleAgainstEveryGroundItIsDrawnOn(t *testing.T) {
	rules := paperStylesheet(t, getStatusPage(t, newTestServer(t)))

	for _, sel := range []string{`:root:not([data-theme="light"])`, `:root[data-theme="dark"]`} {
		tokens := themeTokens(t, rules, sel)
		line := parseHex(t, tokens["--line"])
		// Every ground a border is actually drawn against in this app:
		// the page, cards/panels, and the raised/hover surface (picker
		// group heads and .picker-row:hover, which sits under a row whose
		// separator IS --line).
		for _, ground := range []string{"--ground", "--surface", "--surface-2"} {
			got := contrastRatio(line, parseHex(t, tokens[ground]))
			// WCAG 2.1 SC 1.4.11 sets 3:1 for non-text UI component
			// boundaries. This runs on a cheap panel in 24-hour glare, so
			// the floor is the floor, not the target.
			if got < 3.0 {
				t.Errorf("%s: --line (%s) measures %.2f:1 against %s (%s) — below the 3:1 floor for a UI boundary. This token is the ONLY thing separating a form field from its container, and the only thing drawing .not-tracked's dashed box, which is the form the whole away-vs-not-tracked distinction rests on.",
					sel, tokens["--line"], got, ground, tokens[ground])
			}
			t.Logf("%s: --line vs %-12s %.2f:1", sel, ground, got)
		}
	}
}
