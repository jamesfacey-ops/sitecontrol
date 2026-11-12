package web

import (
	"regexp"
	"strings"
	"testing"
)

// alertRowFlat undoes the R6-1.8a span split (fix4 H) so a test that pins the
// strip row's markup ("<code>TRK-19</code> inbound — OVERDUE 3m0s") still
// reads it as one run of text. Only the three alert spans are removed.
var alertRowSpanRe = regexp.MustCompile(`<span class="alert-(?:lead|sep|verdict)">|</span>(?:<span class="alert-(?:sep|verdict)">|</strong>)`)

func alertRowFlat(s string) string {
	return alertRowSpanRe.ReplaceAllStringFunc(s, func(m string) string {
		if strings.HasSuffix(m, "</strong>") {
			return "</strong>"
		}
		return ""
	})
}

// TestFix4HStripRowKeepsTheVerdictOnItsOwnPhoneLine — R6-1.8a (fix4 H). At
// 320 px an outbound red strip row kept only "TRK-62 outbound —" inside the
// 2-line clamp; OVERDUE fell on line 3 (and with a long vehicle id, at every
// phone width). The lead and the verdict are now separate spans, each one
// ellipsised line on a phone. Playwright on a peered scratch pair, 320/360/
// 375/414: before "TRK-62 outbound —" at 320 and "LOWBOY-TRAILER-UNIT-4471"
// alone at 320-375; after "TRK-62 outbound / OVERDUE 8s" and
// "LOWBOY-TRAILER-U… / OVERDUE 37s". FAIL BEFORE: no spans, no phone rule.
func TestFix4HStripRowKeepsTheVerdictOnItsOwnPhoneLine(t *testing.T) {
	b, err := templateFS.ReadFile("templates/banner.html")
	if err != nil {
		t.Fatal(err)
	}
	banner := string(b)
	for _, want := range []string{
		`<strong><span class="alert-lead">{{template "alert_lead" .}}</span><span class="alert-sep"> — </span><span class="alert-verdict">OVERDUE {{.OverdueByText}}</span></strong>`,
		`<strong><span class="alert-lead">{{template "alert_lead" .}}</span><span class="alert-sep"> — </span><span class="alert-verdict">VERIFY BY RADIO`,
	} {
		if !strings.Contains(banner, want) {
			t.Errorf("banner.html alert_row lacks the lead/verdict split:\n%s", want)
		}
	}
	// ⟦was: the phone block's ".topbar-alerts .banner-notice .alert-verdict
	// { display: block; … }" and ".alert-sep { position: absolute; …" — the
	// strip's one row on a phone. FIX13 (James, 2026-10-05): the strip
	// carries no rows, only the compact vehicle list, so the split survives
	// on the full row (above, on /trips) and the phone keeps the
	// vehicle-first rule by showing each item's id first, ellipsised.⟧
	s, err := templateFS.ReadFile("templates/_styles.html")
	if err != nil {
		t.Fatal(err)
	}
	phone := string(s)
	i := strings.Index(phone, "@media (max-width: 40rem)")
	if i < 0 {
		t.Fatal("no phone block in _styles.html")
	}
	phone = phone[i:]
	phone = phone[:strings.Index(phone, "\n}\n")]
	if !strings.Contains(phone, ".topbar-alerts .alert-items code { display: inline-block; max-width: 8ch; overflow: hidden; text-overflow: ellipsis;") {
		t.Errorf("the phone strip does not keep each item's vehicle id first, ellipsised")
	}
	if got := alertRowFlat(`<strong><span class="alert-lead"><code>T</code> inbound</span><span class="alert-sep"> — </span><span class="alert-verdict">OVERDUE 3m</span></strong>`); got != `<strong><code>T</code> inbound — OVERDUE 3m</strong>` {
		t.Errorf("alertRowFlat = %q", got)
	}
}

// TestFix4HJSOffAlertRowScrollPaddingCoversTheTallBar — F14 residue (R6-5,
// fix4 H). With scripting off and an alert row open, the ≥641 px fallback was
// the no-alert 9rem (144 px) while the sticky bar measured 210-371 px, so up
// to 19 of 37 Shift+Tab stops on /gate/vehicle landed under it (91 of 118 at
// 768 on the lane). Playwright JS-off sweep, peered scratch pair, /gate,
// /gate/vehicle, /trips at 641-1920: after, 0 stops under the bar. FAIL
// BEFORE: there was no ≥641 px :has() rule.
func TestFix4HJSOffAlertRowScrollPaddingCoversTheTallBar(t *testing.T) {
	b, err := templateFS.ReadFile("templates/_styles.html")
	if err != nil {
		t.Fatal(err)
	}
	css := string(b)
	rem := func(block, sel string) float64 {
		i := strings.Index(css, block)
		if i < 0 {
			t.Fatalf("no %q block", block)
		}
		body := css[i:]
		body = body[:strings.Index(body, "\n}\n")]
		m := regexp.MustCompile(regexp.QuoteMeta(sel) + `\s*\{\s*scroll-padding-top:\s*var\(--topbar-h,\s*([\d.]+)rem\)`).FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("%s has no %s scroll-padding-top fallback", block, sel)
		}
		var f float64
		for _, c := range m[1] {
			if c == '.' {
				break
			}
			f = f*10 + float64(c-'0')
		}
		return f
	}
	// ⟦was: ≥ 371 px at 641+ and ≥ 255 px at 1024+, the bar measured with the
	// old full VERIFY row. FIX13 (2026-10-05) re-measured the compact strip,
	// JS off, worst case (both lines, six long ids, "+128 more"): 267 px at
	// 768-1023 and 215 px at 1024-1920 (fix13-run/wide.js); the 641-767 band
	// is TestFix5JSOffNoAlertFallbackCoversTheWrappedNav's.⟧
	if got := rem("@media (min-width: 40.0625rem)", "html:has(.topbar-alerts .notice)"); got*16 < 267 {
		t.Errorf("768+ px alert-row fallback %vrem = %vpx, under the 267 px bar measured at 768", got, got*16)
	}
	if got := rem("@media (min-width: 64rem)", "html:has(.topbar-alerts .notice)"); got*16 < 215 {
		t.Errorf("1024+ px alert-row fallback %vrem = %vpx, under the 215 px bar measured at 1024", got, got*16)
	}
	// and the no-alert fallback clears the 149 px no-alert bar at 641.
	if got := rem("@media (min-width: 40.0625rem)", "  html"); got*16 < 149 {
		t.Errorf("641+ px no-alert fallback %vrem = %vpx, under the 149 px bar measured at 641", got, got*16)
	}
}
