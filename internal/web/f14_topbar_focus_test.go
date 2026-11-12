// f14_topbar_focus_test.go — James, 2026-10-04 (fix3 stream F, R5 F14): the
// sticky top bar must not cover a keyboard-focused control (WCAG 2.2 SC
// 2.4.11), and the two shortest nav links must reach a 44 px target width.
//
// The real measurement is Playwright's Shift+Tab sweep on /gate at 320 / 375 /
// 768 / 1366 / 1920 px, JS on and off (BASE: 2-4 obscured stops at 768-1920;
// after: 0). These pin the rules that produce it.
package web

import (
	"regexp"
	"strings"
	"testing"
)

func TestF14ScrollPaddingTracksTheStickyTopBar(t *testing.T) {
	css := w8Template(t, "_styles.html")
	// Above the phone breakpoint the whole bar is sticky: a static fallback
	// that clears the tallest measured no-alert bar (131 px at 768) ...
	m := regexp.MustCompile(`@media \(min-width: 40\.0625rem\) \{\s*html \{ scroll-padding-top: var\(--topbar-h, (\d+)rem\); \}`).FindStringSubmatch(css)
	if m == nil {
		t.Fatal("no desktop/tablet html scroll-padding-top rule driven by --topbar-h")
	}
	if rem := atoiOrZero(m[1]); rem*16 < 131 {
		t.Errorf("fallback %d rem (%d px) does not clear the 131 px bar measured at 768 px", rem, rem*16)
	}
	// ... a phone with no alert row still has the ~9 px stuck shell ...
	if !strings.Contains(css, "html { scroll-padding-top: var(--topbar-h, 1rem); }") {
		t.Error("no phone base scroll-padding-top: the empty .topbar-alerts shell can clip a focused control")
	}
	// ... and the phone alert strip keeps measuring too.
	if !strings.Contains(css, "html:has(.topbar-alerts .notice) { scroll-padding-top: var(--topbar-h, 8rem); }") {
		t.Error("the phone alert-strip scroll-padding no longer follows the measured --topbar-h")
	}
}

func TestF14LayoutMeasuresTheTopBarIntoTheCustomProperty(t *testing.T) {
	l := w8Template(t, "_layout.html")
	i := strings.Index(l, `<script id="topbar-offset-script">`)
	if i < 0 {
		t.Fatal("page_close has no topbar-offset script")
	}
	js := l[i:]
	for _, want := range []string{`setProperty("--topbar-h"`, "ResizeObserver", `"contents"`} {
		if !strings.Contains(js, want) {
			t.Errorf("topbar-offset script lacks %s", want)
		}
	}
	if strings.Contains(js, "localStorage") || strings.Contains(js, "sessionStorage") {
		t.Error("topbar-offset script must not use client-side storage")
	}
}

func TestF14ShortNavLinksReach44pxWide(t *testing.T) {
	css := w8Template(t, "_styles.html")
	if !regexp.MustCompile(`\.pagenav a, [^{]*\{[^}]*min-width: 2\.75rem;`).MatchString(css) {
		t.Error("no 44 px (2.75rem) min-width on the pagenav links: 'gate' and 'trips' measured 42 px")
	}
}

func atoiOrZero(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}
