package web

// fix2_fi_names_test.go — FIX2 stream FI (cluster I, James 2026-10-04): site
// display names and origin labels. Every assertion is on a served page.
//
// Fixture: this post is STP (display "STP", long name "Salt Water Treatment
// Plant") with FOUR peers and NO destinations — the shape that used to fall
// back to raw ids because the display-name lookup read Destinations only.

import (
	"encoding/json"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// fiPeerTrade applies a trade_created authored by a PEER — the only way a
// same-name trade can exist (the create door refuses a listed name locally).
func fiPeerTrade(t *testing.T, st *store.Store, site, name string) {
	t.Helper()
	pl, err := json.Marshal(domain.TradeCreated{Type: "trade_created", TradeID: site + "-trade-aaaaaaaaaaaaaaaa", Name: name})
	if err != nil {
		t.Fatal(err)
	}
	wm, err := st.Watermark(site, 1)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := st.Apply(store.Envelope{SiteID: site, NodeEpoch: 1, Seq: wm + 1, SchemaVersion: store.SchemaVersion, OriginAuth: site, Payload: pl}); err != nil || !ok {
		t.Fatalf("apply peer trade: %v %v", ok, err)
	}
}

func fiServer(t *testing.T) *Server {
	t.Helper()
	st, setNow := anStore(t)
	setNow(anLocal(9, 0))
	cfg := &config.Config{
		SiteID: "stp", DisplayName: "STP", LongName: "Salt Water Treatment Plant",
		ListenAddr: ":0", NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 10,
		Peers: []config.Peer{
			{SiteID: "ice-road", DisplayName: "2P Ice Road Entrance", BaseURL: "http://example.invalid"},
			{SiteID: "ice-pad", DisplayName: "Ice Pad", BaseURL: "http://example.invalid"},
			{SiteID: "npf", DisplayName: "NPF", BaseURL: "http://example.invalid"},
			{SiteID: "ndb", DisplayName: "NDB", BaseURL: "http://example.invalid"},
		},
		LoginPassword: testLoginPassword,
	}
	return New(cfg, st)
}

func fiGet(t *testing.T, srv *Server, path string) string {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.AddCookie(loginCookie(t, srv, testLoginPassword))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("GET %s: %d", path, rec.Code)
	}
	return rec.Body.String()
}

var fiTags = regexp.MustCompile(`(?s)<[^>]*>`)
var fiScripts = regexp.MustCompile(`(?s)<script.*?</script>`)

func fiText(body string) string {
	return fiTags.ReplaceAllString(fiScripts.ReplaceAllString(body, " "), " ")
}

// A post with no destinations still names its peers by display name.
func TestFIPeerNamesDoNotDependOnDestinations(t *testing.T) {
	cfg := &config.Config{SiteID: "stp", DisplayName: "STP", Peers: []config.Peer{
		{SiteID: "ice-pad", DisplayName: "Ice Pad"}, {SiteID: "ice-road"}}}
	if got := cfg.SiteName("ice-pad"); got != "Ice Pad" {
		t.Errorf("SiteName(ice-pad) = %q, want Ice Pad", got)
	}
	if got := cfg.SiteName("stp"); got != "STP" {
		t.Errorf("SiteName(stp) = %q", got)
	}
	if got := cfg.SiteName("ice-road"); got != "ice-road" {
		t.Errorf("unnamed peer falls back to the label, got %q", got)
	}
}

func TestFINoRawSiteIdsOnTheScreens(t *testing.T) {
	srv := fiServer(t)
	st := srv.Store
	// People authored at two posts, so "Enrolled at" has ids to name.
	for site, name := range map[string]string{"ice-pad": "Pad, Pat", "ice-road": "Road, Rae"} {
		if _, err := domain.CreatePerson(st, site, 1, name, nil, nil, 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	raw := regexp.MustCompile(`\b(ice-road|ice-pad)\b`)
	for _, path := range []string{"/roster", "/trade", "/gate", "/gate/vehicle", "/trips", "/muster", "/analytics", "/analytics/site/stp"} {
		body := fiGet(t, srv, path)
		text := fiText(body)
		text = regexp.MustCompile(`\b(ice-road|ice-pad)-[a-z]+-[0-9a-f]+`).ReplaceAllString(text, "")
		if m := raw.FindString(text); m != "" {
			i := strings.Index(text, m)
			t.Errorf("%s shows the raw site id %q: …%s…", path, m, strings.TrimSpace(text[max(0, i-60):min(len(text), i+60)]))
		}
	}
	rost := fiText(fiGet(t, srv, "/roster"))
	if !strings.Contains(rost, "Ice Pad") || !strings.Contains(rost, "2P Ice Road Entrance") {
		t.Errorf("/roster 'Enrolled at' does not use display names")
	}
	if h := regexp.MustCompile(`\s+`).ReplaceAllString(fiText(fiGet(t, srv, "/trade")), " "); !strings.Contains(h, "Trades (STP)") {
		t.Errorf("/trade heading is not 'Trades (STP)'")
	}
}

func TestFISitePageHeadingAndTopBarNameOnce(t *testing.T) {
	srv := fiServer(t)
	for _, path := range []string{"/analytics/site/stp", "/gate"} {
		body := fiGet(t, srv, path)
		text := regexp.MustCompile(`\s+`).ReplaceAllString(fiText(body), " ")
		if strings.Contains(text, "STP STP") {
			t.Errorf("%s names STP twice in a row", path)
		}
	}
	site := fiGet(t, srv, "/analytics/site/stp")
	if !strings.Contains(site, "<title>STP detail") {
		t.Errorf("site-page tab title is not 'STP detail': %s", regexp.MustCompile(`<title>[^<]*`).FindString(site))
	}
	if !strings.Contains(fiText(site), "Salt Water Treatment Plant") {
		t.Errorf("the site page does not show the long name")
	}
	if !strings.Contains(fiText(fiGet(t, srv, "/gate")), "Salt Water Treatment Plant") {
		t.Errorf("the top bar does not show the long name")
	}
}

// D21: same-name records carry the origin label at the gate.
func TestFID21GateLabelsSameNameRecordsWithTheirOrigin(t *testing.T) {
	srv := fiServer(t)
	st := srv.Store
	for _, site := range []string{"ice-pad", "ndb"} {
		co, err := domain.CreateCompany(st, site, 1, "Halcyon Rigging", 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		if site == "ice-pad" {
			if _, err := domain.CreateTrade(st, site, 1, "Pipefitter", 0, "default"); err != nil {
				t.Fatal(err)
			}
		} else {
			fiPeerTrade(t, st, site, "Pipefitter")
		}
		if _, err := domain.CreatePerson(st, site, 1, "Quarrel, Pat", &co.CompanyID, nil, 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	// A unique name is NOT decorated.
	uco, _ := domain.CreateCompany(st, "ndb", 1, "Kakivik Asset Management", 0, "default")
	if _, err := domain.CreateTrade(st, "ndb", 1, "Inspection", 0, "default"); err != nil {
		t.Fatal(err)
	}
	if _, err := domain.CreatePerson(st, "ndb", 1, "Solo, Sam", &uco.CompanyID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	body := fiGet(t, srv, "/gate/vehicle")
	for _, want := range []string{
		"Halcyon Rigging (added at Ice Pad)", "Halcyon Rigging (added at NDB)",
		"Pipefitter (added at Ice Pad)", "Pipefitter (added at NDB)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/gate/vehicle lacks %q", want)
		}
	}
	if strings.Contains(body, "Kakivik Asset Management (added at") || strings.Contains(body, "Inspection (added at") {
		t.Errorf("a unique company/trade name was decorated with an origin")
	}
	blob := sectionBetween(t, body, `id="roster-data"`, "</script>")
	if n := strings.Count(blob, `"origin":"Ice Pad"`) + strings.Count(blob, `"origin":"NDB"`); n != 2 {
		t.Errorf("the type-ahead payload carries %d origin labels for the same-name pair, want 2:\n%s", n, blob)
	}
	if strings.Contains(blob, `"name":"Solo, Sam","origin"`) {
		t.Errorf("a unique person name carries an origin")
	}
	if !strings.Contains(body, "' (added at '") {
		t.Errorf("the type-ahead label does not render the origin")
	}
	// /gate (single check-in) company list too.
	if g := fiGet(t, srv, "/gate"); !strings.Contains(g, "Halcyon Rigging (added at Ice Pad)") {
		t.Errorf("/gate company list lacks the origin label")
	}
}

// Domain refusals name the post by display name.
func TestFIRefusalsNameThePostByDisplayName(t *testing.T) {
	srv := fiServer(t)
	ps := anRoster(t, srv.Store, "ice-pad", "Away, Al")
	anCheckIn(t, srv.Store, "ice-pad", "T9", ps[0].PersonID)
	open, err := domain.OpenVisitsAt(srv.Store, "ice-pad")
	if err != nil || len(open) == 0 {
		t.Fatalf("fixture: open visits %v, err %v", open, err)
	}
	err = domain.CheckOutVisit(srv.Store, "stp", 1, open[0].VisitID, nil, 0, "default")
	if err == nil {
		t.Fatal("checking out another post's visit must be refused")
	}
	if !strings.Contains(err.Error(), "belongs to Ice Pad") {
		t.Errorf("refusal does not use the display name: %v", err)
	}
}
