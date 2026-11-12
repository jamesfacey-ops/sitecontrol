// gate_mobile_layout_test.go — R2-L15-33/34/30: phone-width layout defects.
// Runtime geometry cannot be measured in Go, so these assert the structural
// causes the browser measurements found (~/sc-test/reports/lanes-r2/L15.md):
// a flex-shrunk roster row (checkbox hit areas overlapping the next row), an
// unshrinkable flex item in the confirm box (page widened to 798/1483 px),
// and autofocus dragging the incomplete-check-in notice off-screen.
package web

import (
	"regexp"
	"strings"
	"testing"
)

func mobileCSSRule(t *testing.T, page, selector string) string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(selector) + `\s*\{([^}]*)\}`)
	m := re.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no CSS rule with selector %q in the served stylesheet", selector)
	}
	return m[1]
}

func TestCheckoutRosterRowsGrowWithTheirContentAndNeverShrink(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	_, page := getGate(t, srv)
	rule := mobileCSSRule(t, page, ".roster-table > .roster-row, .roster-table > label.picker-row")
	if !strings.Contains(rule, "flex: 0 0 auto") {
		t.Errorf("roster rows can flex-shrink to their min-height, so wrapped text overflows into the next row's hit area: %q", rule)
	}
	if regexp.MustCompile(`(?m)^\s*height:`).MatchString(mobileCSSRule(t, page, ".roster-row, .gate-roster label.picker-row")) {
		t.Error("roster rows carry a fixed height; they must use min-height only")
	}
}

func TestCompactConfirmBoxItemsMayShrinkToThePhone(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	_, page := getGate(t, srv)
	for _, sel := range []string{".vehicle-form-compact .deadline-compact", ".vehicle-form-compact fieldset > .roster-time"} {
		if r := mobileCSSRule(t, page, sel); strings.Contains(r, "flex: 0 0 auto") {
			t.Errorf("%s is flex:0 0 auto and cannot shrink below its content: %q", sel, r)
		}
	}
	if r := mobileCSSRule(t, page, "fieldset.time-control input"); !strings.Contains(r, "max-width: 100%") {
		t.Errorf("time-control inputs are not capped to their container: %q", r)
	}
}

func TestIncompleteCheckInNoticeClearsTheStickyBarOnJump(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	_, page := getGate(t, srv)
	if !regexp.MustCompile(`(?m)^main section\[id\] \{[^}]*scroll-margin-top`).MatchString(page) {
		t.Error("the incomplete-check-in notice has no scroll-margin-top (via main section[id]) to clear the sticky bar")
	}
}
