// shared_box_test.go — James, 2026-10-03: "on the site pages the boxes on the
// top are all different formatting and heights — can we standardise that
// please?"
//
// The boxes at the top of a post page (top-bar chips, alert rows, notices,
// analytics tiles, the muster mode box, the sync
// peer boxes) used to each carry their own padding, border, radius and font
// size. They now share ONE rule, ".box" in _styles.html; the legacy class
// names stay (other tests and the print block address them) and carry only
// layout and a severity-to-token mapping. These tests pin both halves: no
// markup uses a legacy box class without "box", and no legacy rule
// re-declares the geometry the shared rule owns.
package web

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// ⟦was: also "phase-panel", "phase-gap" — the status page's weather-phase panel, removed with weather phases, James 2026-10-04⟧
var legacyBoxClasses = []string{"chip", "notice", "an-tile", "mode-live", "mode-reprint", "mode-snapshot", "peer"}

var classAttr = regexp.MustCompile(`class="([^"]*)"`)

func TestEveryTopBoxIsCoveredByTheSharedBoxRule(t *testing.T) {
	b, err := fs.ReadFile(templateFS, "templates/_styles.html")
	if err != nil {
		t.Fatal(err)
	}
	sel := regexp.MustCompile(`(?s)\n(\.box[^{]*)\{`).FindStringSubmatch(string(b))
	if sel == nil {
		t.Fatal("no shared .box rule in _styles.html")
	}
	for _, lc := range legacyBoxClasses {
		if !regexp.MustCompile(`\.` + regexp.QuoteMeta(lc) + `\b`).MatchString(sel[1]) {
			t.Errorf("legacy box class .%s is not in the shared .box selector list: it would draw its own shape", lc)
		}
	}
	// And the markup still uses them (precondition: the scan means something).
	files, _ := fs.Glob(templateFS, "templates/*.html")
	seen := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_styles.html") {
			continue
		}
		m, _ := fs.ReadFile(templateFS, f)
		for _, c := range classAttr.FindAllStringSubmatch(string(m), -1) {
			for _, tk := range strings.Fields(c[1]) {
				for _, lc := range legacyBoxClasses {
					if tk == lc {
						seen++
					}
				}
			}
		}
	}
	if seen < 50 {
		t.Fatalf("precondition: only %d legacy box elements found in templates", seen)
	}
}

func TestSharedBoxOwnsTheGeometry(t *testing.T) {
	b, err := fs.ReadFile(templateFS, "templates/_styles.html")
	if err != nil {
		t.Fatal(err)
	}
	css := string(b)
	boxRule := regexp.MustCompile(`(?s)\n\.box[^{]*\{(.*?)\n\}`).FindStringSubmatch(css)
	if boxRule == nil {
		t.Fatal("no .box rule in _styles.html")
	}
	for _, want := range []string{"padding:", "border:", "border-left:", "border-radius:", "font-size:"} {
		if !strings.Contains(boxRule[1], want) {
			t.Errorf(".box rule does not declare %s", want)
		}
	}
	// No legacy rule may re-declare the shape: that is how they drifted apart.
	for _, lc := range legacyBoxClasses {
		re := regexp.MustCompile(`(?m)^\.` + regexp.QuoteMeta(lc) + `(?:-(?:ok|bad|warn|crit|critical|info|fault|blocked))? \{([^}]*)\}`)
		for _, m := range re.FindAllStringSubmatch(css, -1) {
			for _, bad := range []string{"padding:", "border:", "border-radius:", "border-left:", "font-size:"} {
				if strings.Contains(m[1], bad) {
					t.Errorf(".%s rule re-declares %s, which belongs to .box: %s", lc, bad, strings.TrimSpace(m[0]))
				}
			}
		}
	}
	// A warning is still a warning: the severity maps still exist.
	for _, want := range []string{".notice-warn", ".notice-critical", ".notice-crit", ".notice-ok", ".chip-crit", ".an-tile-crit", ".notice-fault"} {
		if !strings.Contains(css, want) {
			t.Errorf("severity class %s lost its mapping", want)
		}
	}
	// Equal heights in a row: the top bar stretches, the tile grid is 1fr.
	if !regexp.MustCompile(`(?s)\.topbar-strip \{[^}]*align-items: stretch`).MatchString(css) {
		t.Error("top bar strip must align-items: stretch so chips in a row share a height")
	}
	if !strings.Contains(css, "grid-auto-rows: 1fr") {
		t.Error("the analytics tile grid must use grid-auto-rows: 1fr")
	}
}
