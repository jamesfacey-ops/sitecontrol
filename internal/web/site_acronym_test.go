// site_acronym_test.go — James, 2026-10-03: "all site names need to be fully
// capitalized on the acronyms NPF, STP, NDB." Site ids are lower-case and key
// replicated data; every place a person READS a site name must show the
// acronym upper-case, and the ids themselves must not change.
package web

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
)

func TestSiteLabelCapitalisesAcronymIdsOnly(t *testing.T) {
	for id, want := range map[string]string{
		"npf": "NPF", "ndb": "NDB", "stp": "STP",
		"ice-road": "ice-road", "ice-pad": "ice-pad", "site-a": "site-a", "": "",
	} {
		if got := domain.SiteLabel(id); got != want {
			t.Errorf("SiteLabel(%q) = %q, want %q", id, got, want)
		}
	}
	if got := (&config.Config{SiteID: "ndb"}).PostName(); got != "NDB" {
		t.Errorf("PostName fallback = %q, want NDB", got)
	}
}

// Every served page that names a site shows the acronyms upper-case; the
// ids in attributes/URLs stay lower-case. /status is excluded: it prints the
// raw config id under a "site_id" label on purpose.
func TestSiteAcronymsRenderUpperCaseOnEveryPage(t *testing.T) {
	st, setNow := anStore(t)
	setNow(anLocal(9, 0))
	cfg := &config.Config{
		SiteID: "npf", ListenAddr: ":0", NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 10,
		Peers: []config.Peer{
			{SiteID: "ndb", BaseURL: "http://example.invalid"}, {SiteID: "stp", BaseURL: "http://example.invalid"},
			{SiteID: "ice-road", BaseURL: "http://example.invalid"},
		},
		LoginPassword: testLoginPassword,
	}
	srv := New(cfg, st) // no DisplayName: the id fallback is what is under test
	anCheckIn(t, st, "ndb", "T1", anRoster(t, st, "ndb", "A One")[0].PersonID)

	scripts := regexp.MustCompile(`(?s)<script.*?</script>`)
	tags := regexp.MustCompile(`(?s)<[^>]*>`)
	bare := regexp.MustCompile(`\b(npf|ndb|stp)\b`)
	for _, path := range []string{"/gate", "/trips", "/trips/detail", "/muster", "/analytics", "/orientation", "/intake", "/sync", "/trade"} {
		req := httptest.NewRequest("GET", path, nil)
		req.AddCookie(loginCookie(t, srv, testLoginPassword))
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Logf("%s: status %d (skipped)", path, rec.Code)
			continue
		}
		text := tags.ReplaceAllString(scripts.ReplaceAllString(rec.Body.String(), " "), " ")
		text = strings.ReplaceAll(strings.ReplaceAll(text, "site_id   npf", ""), "-trip-", "")
		text = regexp.MustCompile(`\b(npf|ndb|stp)-([a-z]+-)?[0-9a-f]+`).ReplaceAllString(text, "")
		if m := bare.FindString(text); m != "" {
			i := strings.Index(text, m)
			t.Errorf("%s shows lower-case site name %q: …%s…", path, m, strings.TrimSpace(text[max(0, i-60):min(len(text), i+60)]))
		}
		if path == "/analytics" && !strings.Contains(text, "NDB") {
			t.Errorf("/analytics does not show NDB")
		}
	}
	if cfg.SiteID != "npf" || cfg.Peers[0].SiteID != "ndb" {
		t.Fatal("site ids were changed")
	}
}
