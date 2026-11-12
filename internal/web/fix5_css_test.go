package web

// fix5_css_test.go — FIX5 layout residues (R7 L01 R7-2, R7 L05 F14 A/B).
// The figures are Playwright measurements on a scratch post (headless
// Chromium, DejaVu Sans; ~/sc-fix/fix5-run/strip.js, occ.js, f14b.js); the
// tests hold the stylesheet to them. Each FAILS on 35a0024.

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func fix5CSS(t *testing.T, name string) string {
	t.Helper()
	b, err := templateFS.ReadFile("templates/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fix5Block returns the body of the first block opened by header.
func fix5Block(t *testing.T, css, header string) string {
	t.Helper()
	i := strings.Index(css, header)
	if i < 0 {
		t.Fatalf("no %q block", header)
	}
	body := css[i:]
	if j := strings.Index(body, "\n  }\n"); j >= 0 && strings.HasPrefix(header, "  ") {
		return body[:j]
	}
	return body[:strings.Index(body, "\n}\n")]
}

func fix5Rem(t *testing.T, body, pattern string) float64 {
	t.Helper()
	m := regexp.MustCompile(pattern).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no match for %s in:\n%s", pattern, body)
	}
	f, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// TestFix5PhoneStripFitsVerifyByRadioAt320 — R7-2: at 320 px the strip
// row's lines were 145 px (320 − 136 px button column − 39 px of gutters,
// border and left padding) and "VERIFY BY RADIO…" needs 161 px, so the
// amber verdict read "VERIFY BY RAD…" and "TRK-72 outbound" (146 px)
// read "TRK-72 outbou…". A ≤ 340 px rule must give the line ≥ 161 px
// without the button overlapping it.
func TestFix5PhoneStripFitsVerifyByRadioAt320(t *testing.T) {
	// ⟦was: the ≤ 340 px rule's button column (padding-right / button width)
	// must leave the strip row's line ≥ 161 px for "VERIFY BY RADIO…".
	// FIX13 (James, 2026-10-05): the strip has no row and no button; its
	// amber line opens "VERIFY BY RADIO:" (152 px at 320, measured) and the
	// ≤ 340 px rule tightens that label so it shares line 1 with the first
	// 8ch item (fix13-run/stress.js).⟧
	css := fix5CSS(t, "_styles.html")
	phone := css[strings.Index(css, "@media (max-width: 40rem)"):]
	i := strings.Index(phone, "@media (max-width: 21.25rem)")
	if i < 0 {
		t.Fatal("no ≤ 340 px rule in the phone block")
	}
	narrow := phone[i : i+strings.Index(phone[i:], "\n")]
	if !strings.Contains(narrow, ".topbar-alerts .alert-line-kind { letter-spacing: -0.01em; }") {
		t.Errorf("the ≤ 340 px rule does not tighten the strip's line label:\n%s", narrow)
	}
	if stripItemsPhone != 2 {
		t.Errorf("stripItemsPhone = %d — two 8ch items and \"+N more\" are what fit two lines at 320 behind \"VERIFY BY RADIO:\"", stripItemsPhone)
	}
}

// TestFix5JSOffNoAlertFallbackCoversTheWrappedNav — F14 residue B: with
// scripting off and no alert row, a long post name wraps the nav and the
// bar measured 201 px at 641-643 px against the 160 px fallback; with an
// alert row the same post measured 400 px against 400. The 641-767 band
// carries its own fallbacks.
func TestFix5JSOffNoAlertFallbackCoversTheWrappedNav(t *testing.T) {
	band := fix5Block(t, fix5CSS(t, "_styles.html"), "@media (min-width: 40.0625rem) and (max-width: 47.9375rem)")
	if got := fix5Rem(t, band, `\n  html \{ scroll-padding-top: var\(--topbar-h, ([\d.]+)rem\); \}`) * 16; got <= 201 {
		t.Errorf("641-767 px no-alert fallback %.0f px does not clear the 201 px bar measured at 641", got)
	}
	// ⟦was: > 400 px, the old full alert row. FIX13 (2026-10-05): the compact
	// strip's worst case measured 285 px at 641 (fix13-run/wide.js), plus
	// the long post name's 52 px nav wrap above = 337 px.⟧
	if got := fix5Rem(t, band, `html:has\(\.topbar-alerts \.notice\) \{ scroll-padding-top: var\(--topbar-h, ([\d.]+)rem\); \}`) * 16; got <= 337 {
		t.Errorf("641-767 px alert-row fallback %.0f px does not clear the 337 px bar (285 measured + 52 long-name wrap) at 641", got)
	}
}

// TestFix5FocusedControlIsSettledBelowTheBar — F14 residue A: a
// datetime-local focuses an inner field ~12 px inside its border, so
// scroll-padding (bar + 8 px) left the /trips occurred_at input's top 2-4 px
// under the bar with scripting on. The offset script now settles any
// focused control whose box starts under the sticky bar.
func TestFix5FocusedControlIsSettledBelowTheBar(t *testing.T) {
	layout := fix5CSS(t, "_layout.html")
	i := strings.Index(layout, `<script id="topbar-offset-script">`)
	if i < 0 {
		t.Fatal("no topbar-offset-script")
	}
	script := layout[i : i+strings.Index(layout[i:], "</script>")]
	for _, want := range []string{`addEventListener("focusin"`, "getBoundingClientRect().top", "window.scrollBy("} {
		if !strings.Contains(script, want) {
			t.Errorf("the offset script has no %q — a focused control can still start under the bar", want)
		}
	}
}
