package web

import (
	"regexp"
	"strings"
	"testing"
)

// FIX11: the dispatch host speaks as Dispatch, post hosts as the post.
var (
	fix11TopSiteRe = regexp.MustCompile(`(?s)<div class="topbar-site"[^>]*>(.*?)</div>`)
	fix11TitleRe   = regexp.MustCompile(`(?s)<title>(.*?)</title>`)
)

func TestFix11DispatchHostTopBarAndTitleSayDispatch(t *testing.T) {
	srv, _, _ := fix7Overdue(t)
	post := srv.Cfg.DisplayName
	for _, p := range []string{"/analytics", "/analytics/site/site-b", "/search"} {
		rec := fix7Do(t, srv, "GET", fix7DispatchHost, p, nil)
		if rec.Code != 200 {
			t.Fatalf("%s = %d", p, rec.Code)
		}
		body := rec.Body.String()
		bar := fix11TopSiteRe.FindStringSubmatch(body)
		if bar == nil || !strings.Contains(bar[1], "Dispatch") || strings.Contains(bar[1], post) {
			t.Errorf("%s top bar identity = %q, want Dispatch and not %q", p, bar, post)
		}
		ti := fix11TitleRe.FindStringSubmatch(body)
		if ti == nil || !strings.Contains(ti[1], "Dispatch") {
			t.Errorf("%s title = %q, want Dispatch", p, ti)
		}
		if p != "/analytics/site/site-b" && ti != nil && strings.Contains(ti[1], post) {
			t.Errorf("%s title %q names the post", p, ti[1])
		}
	}
	rec := fix7Do(t, srv, "GET", fix7DispatchHost, "/analytics", nil)
	if strings.Contains(rec.Body.String(), "this post") {
		t.Errorf("dispatch /analytics still says \"this post\"")
	}
	rec = fix7Do(t, srv, "POST", fix7DispatchHost, "/trips/dispatch", map[string][]string{})
	if bar := fix11TopSiteRe.FindStringSubmatch(rec.Body.String()); rec.Code != 403 || bar == nil || strings.Contains(bar[1], post) || !strings.Contains(bar[1], "Dispatch") {
		t.Errorf("refused page code %d bar %q", rec.Code, bar)
	}
	if ti := fix11TitleRe.FindStringSubmatch(rec.Body.String()); ti == nil || strings.Contains(ti[1], post) {
		t.Errorf("refused title %q", ti)
	}
}

func TestFix11PostHostStillShowsPostName(t *testing.T) {
	srv, _, _ := fix7Overdue(t)
	post := srv.Cfg.DisplayName
	for _, p := range []string{"/analytics", "/analytics/site/site-b", "/search"} {
		rec := fix7Do(t, srv, "GET", fix7GateHost, p, nil)
		body := rec.Body.String()
		bar := fix11TopSiteRe.FindStringSubmatch(body)
		if bar == nil || strings.Contains(bar[1], "Dispatch") || bar[1] == "" {
			t.Errorf("%s post-host top bar = %q", p, bar)
		}
		if p == "/analytics" {
			if ti := fix11TitleRe.FindStringSubmatch(body); ti == nil || !strings.Contains(ti[1], post) {
				t.Errorf("post-host title %q lost %q", ti, post)
			}
		}
	}
}
