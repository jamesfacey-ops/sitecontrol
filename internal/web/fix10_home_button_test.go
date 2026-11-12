package web

import (
	"regexp"
	"strings"
	"testing"
)

// FIX10 (James 2026-10-05): "an easy home button for all pages so that the
// operator can just hit one button at the top and go back to the site
// landing page or dispatch landing page whichever they are logged into".
// homelink.go has the design.

var fix10HomeRe = regexp.MustCompile(`<a class="home-btn" href="([^"]*)" data-home( aria-current="page")?>Home</a>`)

// fix10Home returns (href, current, count) of the Home link(s) in a body.
func fix10Home(body string) (string, bool, int) {
	m := fix10HomeRe.FindAllStringSubmatch(body, -1)
	if len(m) == 0 {
		return "", false, 0
	}
	return m[0][1], m[0][2] != "", len(m)
}

func TestFix10HomeOnEveryMainPageWithRightHrefPerHost(t *testing.T) {
	srv, _, _ := fix7Overdue(t)
	postPages := []string{"/gate", "/gate/vehicle", "/trips", "/muster", "/roster", "/status",
		"/search", "/analytics", "/analytics/site/site-b", "/orientation", "/trade",
		"/person/amend", "/company/amend", "/vehicle/amend", "/time/correct", "/trips/amend", "/no-such-page"}
	for _, p := range postPages {
		rec := fix7Do(t, srv, "GET", fix7GateHost, p, nil)
		if rec.Code != 200 {
			t.Logf("post host %s: status %d, skipped", p, rec.Code)
			continue
		}
		href, cur, n := fix10Home(rec.Body.String())
		if n != 1 || href != "/gate" {
			t.Errorf("post host %s: Home count %d href %q, want exactly one to /gate (even from analytics)", p, n, href)
		}
		if cur != (p == "/gate") {
			t.Errorf("post host %s: aria-current = %v", p, cur)
		}
	}
	// the three pages that must be there are really served, not all skipped
	for _, p := range []string{"/gate", "/trips", "/muster", "/status", "/search", "/analytics", "/analytics/site/site-b"} {
		if rec := fix7Do(t, srv, "GET", fix7GateHost, p, nil); rec.Code != 200 {
			t.Errorf("post host %s = %d, expected a served page", p, rec.Code)
		}
	}
	for _, p := range []string{"/analytics", "/analytics/site/site-b", "/search"} {
		rec := fix7Do(t, srv, "GET", fix7DispatchHost, p, nil)
		if rec.Code != 200 {
			t.Fatalf("dispatch host %s = %d", p, rec.Code)
		}
		href, cur, n := fix10Home(rec.Body.String())
		if n != 1 || href != "/analytics" {
			t.Errorf("dispatch host %s: Home count %d href %q, want exactly one to /analytics", p, n, href)
		}
		if cur != (p == "/analytics") {
			t.Errorf("dispatch host %s: aria-current = %v", p, cur)
		}
	}
	// the refusal page is on the dispatch host too; its Home must land on analytics
	rec := fix7Do(t, srv, "POST", fix7DispatchHost, "/trips/dispatch", map[string][]string{})
	if rec.Code != 403 {
		t.Fatalf("dispatch write = %d, want 403", rec.Code)
	}
	if href, _, n := fix10Home(rec.Body.String()); n != 1 || href != "/analytics" {
		t.Errorf("dispatch refusal page: Home count %d href %q", n, href)
	}
	// and a Home click is never bounced: /analytics is served, not redirected
	if rec := fix7Do(t, srv, "GET", fix7DispatchHost, "/analytics", nil); rec.Code != 200 {
		t.Errorf("dispatch Home target = %d", rec.Code)
	}
}

func TestFix10HomeIsAPlainLinkOutsideFormsAndAtLeast44px(t *testing.T) {
	srv, _, _ := fix7Overdue(t)
	body := fix7Do(t, srv, "GET", fix7GateHost, "/trips", nil).Body.String()
	i := strings.Index(body, `class="home-btn"`)
	if i < 0 {
		t.Fatal("no Home on /trips")
	}
	if strings.Count(body[:i], "<form") != strings.Count(body[:i], "</form>") {
		t.Error("Home sits inside a form")
	}
	if !strings.Contains(body[i-3:i+120], "<a ") || strings.Contains(body[i:i+120], "<button") {
		t.Error("Home is not a plain anchor")
	}
	css := regexp.MustCompile(`(?s)\.home-btn \{(.*?)\}`).FindStringSubmatch(body)
	if css == nil || !strings.Contains(css[1], "min-width: 2.75rem") || !strings.Contains(css[1], "min-height: 2.75rem") {
		t.Errorf("home-btn lacks the 44 px floor both ways: %v", css)
	}
	if strings.Contains(strings.ToLower(body), "localstorage") {
		t.Error("storage reference")
	}
}

func TestFix10NoHomeOnLoginPage(t *testing.T) {
	srv, _, _ := fix7Overdue(t)
	for _, host := range []string{fix7GateHost, fix7DispatchHost} {
		rec := fix7Do(t, srv, "GET", host, "/login", nil)
		if _, _, n := fix10Home(rec.Body.String()); n != 0 {
			t.Errorf("%s /login carries Home", host)
		}
	}
}
