// w8_mobile_a11y_test.go — James, 2026-10-03: the mobile and accessibility
// sweep (M-20..M-25, A-04, R2-A29n, L15-07, JS-off phone check-out list).
//
// These pin the geometry and markup facts the sweep changed. The real
// measurements (Playwright at 320-1920 px, axe-core, elementFromPoint) live
// in evidence/fork/w8; these guard the rules that produced them.
package web

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

func w8Template(t *testing.T, name string) string {
	t.Helper()
	b, err := fs.ReadFile(templateFS, "templates/"+name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The one-screen height cap is a desktop rule, and it must NOT sit on the
// .gate-layout flex column. James, 2026-10-04 (fix2 FH, D19/D20): a max-height
// on the column is overflowed by its own content, so the footer painted over
// Confirm/Cancel and the time radio (D20) and, with scripting OFF, the roster
// was the one thing allowed to shrink and collapsed to 2 px (D19). The cap now
// lives on the list alone, width-gated, with a floor of 18rem (~5 rows + head).
func TestW8GateLayoutHeightCapIsDesktopOnly(t *testing.T) {
	css := w8Template(t, "_styles.html")
	if regexp.MustCompile(`\.gate-layout \{[^}]*max-height`).MatchString(css) {
		t.Fatal("a max-height is back on .gate-layout: the column overflows its own box (D19/D20)")
	}
	re := regexp.MustCompile(`(?s)@media ([^{]*)\{\s*\.gate-roster \.roster-table \{ max-height: max\(18rem, calc\(100vh - [0-9.]+rem\)\); \}\s*\}`)
	m := re.FindStringSubmatch(css)
	if m == nil {
		t.Fatal("the roster list's floored desktop cap is not in its own media block")
	}
	if !strings.Contains(m[1], "min-width") {
		t.Fatalf("the roster list's height cap is not width-gated: @media %s", m[1])
	}
}

// fix2 FH: every landmark on /trips/detail has its own name. The record-chain
// region is rendered once per trip, so its label must carry the trip id.
func TestFix2FHTripsDetailRegionsAreUniquelyLabelled(t *testing.T) {
	src := w8Template(t, "trips_detail.html")
	if strings.Contains(src, "Trips detail table") {
		t.Error("a generic numbered label is back; the per-trip region repeats it")
	}
	if !strings.Contains(src, `aria-label="Record chain for trip {{.TripID}} (scrolls sideways)"`) {
		t.Error("the per-trip record-chain region does not carry its trip id in its label")
	}
}

// fix2 FH (D28, L15): navigation-style text links reach a 44 px target by
// padding, and the phone alert strip drops its long sentence to stay one row.
func TestFix2FHTapTargetsAndPhoneAlertStrip(t *testing.T) {
	css := w8Template(t, "_styles.html")
	// ⟦was: `.alert-count a, …` and the phone's hidden .alert-count-more
	// sentence — FIX13 (James, 2026-10-05): the count sentence is gone; the
	// strip's links are inside its .notice lines, and a phone shows two
	// items per line and its own "+N more" instead of the wide one.⟧
	if !regexp.MustCompile(`\.note a, \.notice a, [^{]*\{ padding-block: 15px; \}`).MatchString(css) {
		t.Error("the standalone text links have no 44 px hit-area padding rule")
	}
	if !strings.Contains(css, ".topbar-alerts .alert-item-wide, .topbar-alerts .alert-more-wide { display: none; }") {
		t.Error("the phone strip does not drop its wide-only items")
	}
	if strings.Contains(css, "scroll-padding-top: 16rem") {
		t.Error("the phone scroll-padding still reserves the old 16rem strip")
	}
	if !strings.Contains(css, ".roster-table { scroll-padding-top:") {
		t.Error("the roster scroller does not reserve its sticky head's height")
	}
	if !strings.Contains(w8Template(t, "banner.html"), `alert-more-phone`) {
		t.Error("banner.html lost the phone's own \"+N more\"") // ⟦was: the alert-count-more wrapper — FIX13⟧
	}
}

// A wide table sits in a keyboard-focusable overflow container, so the page
// body never scrolls sideways (M-20/M-21) and axe's scrollable-region-
// focusable rule holds.
func TestW8WideTablesAreFocusableScrollRegions(t *testing.T) {
	for _, f := range []string{"trade.html", "orientation.html", "intake.html", "muster.html", "status.html", "sync_status.html", "trips_detail.html"} {
		src := w8Template(t, f)
		if n := strings.Count(src, `<div class="table-wrap">`); n != 0 {
			t.Errorf("%s: %d table-wrap container(s) without tabindex/role/aria-label", f, n)
		}
		if f == "trade.html" || f == "orientation.html" {
			tables := strings.Count(src, "<table")
			wraps := strings.Count(src, `class="table-wrap" tabindex="0" role="region" aria-label=`)
			if tables == 0 || wraps != tables {
				t.Errorf("%s: %d tables but %d focusable scroll wrappers", f, tables, wraps)
			}
		}
	}
}

// A-04: no header cell is empty.
func TestW8NoEmptyTableHeaderCell(t *testing.T) {
	files, _ := fs.Glob(templateFS, "templates/*.html")
	empty := regexp.MustCompile(`<th[^>]*>\s*</th>`)
	for _, f := range files {
		b, _ := fs.ReadFile(templateFS, f)
		if empty.Match(b) {
			t.Errorf("%s has an empty <th>", f)
		}
	}
}

// M-22: controls are 44px (2.75rem), the nav links included.
func TestW8TouchTargetsAre44px(t *testing.T) {
	css := w8Template(t, "_styles.html")
	if strings.Contains(css, "min-height: 2.25rem") {
		t.Error("a 36px control floor (2.25rem) is back")
	}
	if i := strings.Index(css, "\n.picker-row {"); i < 0 || !strings.Contains(css[i:min(i+300, len(css))], "min-height: 2.75rem") {
		t.Error(".picker-row has no 44px floor")
	}
	css = css[strings.Index(css, "W8 (James, 2026-10-03: mobile"):]
	for _, sel := range []string{".pagenav a", ".an-site-link", "details > summary"} {
		i := strings.Index(css, sel)
		if i < 0 {
			t.Errorf("no rule for %q", sel)
			continue
		}
		if !strings.Contains(css[i:min(i+400, len(css))], "2.75rem") {
			t.Errorf("%q does not carry the 2.75rem floor", sel)
		}
	}
}

// M-23: below 40rem only the alerts stay stuck to the top of the screen.
func TestW8PhoneTopBarDoesNotStickTheWholeStrip(t *testing.T) {
	css := w8Template(t, "_styles.html")
	i := strings.Index(css, "@media (max-width: 40rem) {\n  .topbar { display: contents; }")
	if i < 0 {
		t.Fatal("no phone top-bar block")
	}
	blk := css[i:min(i+1500, len(css))]
	if !strings.Contains(blk, ".topbar-alerts {\n    position: sticky") {
		t.Errorf("the alerts are not the sticky part on a phone:\n%s", blk)
	}
}

// L15-07: the inbound countdown takes the countdown token, not 15px.
func TestW8InboundCountdownUsesTheCountdownToken(t *testing.T) {
	css := w8Template(t, "_styles.html")
	i := strings.Index(css, ".inbound-row .inbound-countdown {")
	if i < 0 {
		t.Fatal("no inbound countdown size rule")
	}
	if !strings.Contains(css[i:min(i+300, len(css))], "font-size: var(--fs-countdown)") {
		t.Error("the inbound countdown is not at the countdown token")
	}
}

// M-25: elements the page script fills with a hint or result are live
// regions in the served markup.
func TestW8ScriptFilledHintsAreLiveRegions(t *testing.T) {
	for _, tc := range []struct{ file, class string }{
		{"gate.html", "add-hint"}, {"gate.html", "picked-confirm"}, {"gate.html", "adopt-confirm"},
		{"gate_vehicle.html", "add-hint"}, {"gate_vehicle.html", "picked-confirm"}, {"gate_vehicle.html", "adopt-confirm"},
		{"gate_vehicle.html", "commit-blocked"}, {"gate_vehicle.html", "list-count"},
		{"_picker.html", "picker-tally"}, {"gate_vehicle.html", "picker-tally"},
	} {
		src := w8Template(t, tc.file)
		re := regexp.MustCompile(`<(?:p|div|span)[^>]*class="[^"]*\b` + tc.class + `\b[^"]*"[^>]*>`)
		tags := re.FindAllString(src, -1)
		if len(tags) == 0 {
			t.Errorf("%s: no element with class %s", tc.file, tc.class)
		}
		for _, tag := range tags {
			if !strings.Contains(tag, `role="status"`) && !strings.Contains(tag, `role="alert"`) {
				t.Errorf("%s: %s is script-filled but not a live region: %s", tc.file, tc.class, tag)
			}
		}
	}
}

// R2-A29n: the disabled commit button names its reason element.
func TestW8DisabledCommitNamesItsReason(t *testing.T) {
	src := w8Template(t, "gate_vehicle.html")
	if !strings.Contains(src, `aria-describedby="commit-blocked-{{$f.Direction}}"`) ||
		!strings.Contains(src, `id="commit-blocked-{{$f.Direction}}"`) {
		t.Error("the commit button does not point aria-describedby at its commit-blocked reason")
	}
}
