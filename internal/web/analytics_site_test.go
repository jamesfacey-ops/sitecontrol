// analytics_site_test.go — James, 2026-10-03: the site name on /analytics is
// a link to that site's READ-ONLY, LIVE detail (analytics_site.go), and the
// analytics pages fill the screen at every size (_styles.html "FILL THE
// SCREEN"). What is pinned here:
//
//   - the link exists on every card, and the detail is behind the same login;
//   - read-only: no <form>, no <button>, no ack banner; every writing method
//     refused 405 AND the log unchanged by the attempt;
//   - live: a meta refresh and an "as at" time, and only on this page;
//   - the SAME people and vehicles as the site's own /muster and /trips;
//   - a stale site carries the STALE / verify-by-radio warning, a current one
//     does not;
//   - PII: a recorded phone number never reaches the page;
//   - the responsive CSS contract (no fixed width wider than a phone, the
//     72rem cap lifted, type scaled by clamp()).
package web

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/repl"
	"sitecontrol/internal/store"
)

func anSiteGet(t *testing.T, srv *Server, method, path string, login bool) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if login {
		req.AddCookie(loginCookie(t, srv, testLoginPassword))
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// anNames lists the <li> person names inside the element that carries attr,
// sorted. Name text only: anything after the name inside the <li> (a tag, a
// company label) is cut at the first "<" or " <".
func anNames(t *testing.T, body, attr string) []string {
	t.Helper()
	page := styleRe.ReplaceAllString(body, "")
	at := strings.Index(page, attr)
	if at < 0 {
		t.Fatalf("page has no element %s:\n%s", attr, page)
	}
	sec := page[at:]
	// The section ends at its own closing tag; both hooks used here sit on a
	// <section> or <div> whose content holds no nested element of that kind
	// before the names, so the first matching close is the right one.
	closer := "</section>"
	if strings.HasPrefix(page[strings.LastIndex(page[:at], "<"):], "<div") {
		closer = "</div>"
	}
	if end := strings.Index(sec, closer); end >= 0 {
		sec = sec[:end]
	}
	var out []string
	for _, m := range regexp.MustCompile(`<li>([^<]+)`).FindAllStringSubmatch(sec, -1) {
		out = append(out, strings.TrimSpace(m[1]))
	}
	sort.Strings(out)
	return out
}

func personNames(ps []domain.Person) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Name)
	}
	sort.Strings(out)
	return out
}

// anRoadWorld is roadWorld (three on site at site-a, three on TRK-100 to
// site-b) with a login password and site-b configured as a peer, so the
// analytics pages list both sites.
func anRoadWorld(t *testing.T) *roadWorld {
	t.Helper()
	w := newRoadWorld(t)
	w.srv.Cfg.LoginPassword = testLoginPassword
	w.srv.Cfg.Peers = []config.Peer{{SiteID: "site-b", BaseURL: "http://example.invalid"}}
	contact := 1.0
	w.srv.SyncSnapshot = anHealthySnapshot("site-a", "site-b", &contact)
	return w
}

func TestAnalyticsEverySiteNameLinksToItsDetail(t *testing.T) {
	w := anRoadWorld(t)
	_, body := anSiteGet(t, w.srv, "GET", "/analytics", true)
	for _, site := range []string{"site-a", "site-b"} {
		anSiteCard(t, body, site) // fails unless the card's name IS the link
	}
	if n := strings.Count(body, `class="an-site-link"`); n != 2 {
		t.Errorf("want exactly one detail link per site card (2), found %d", n)
	}
}

func TestAnalyticsSiteDetailIsBehindLogin(t *testing.T) {
	w := anRoadWorld(t)
	_, anon := anSiteGet(t, w.srv, "GET", "/analytics/site/site-a", false)
	if !strings.Contains(anon, `name="password"`) {
		t.Errorf("unauthenticated detail must serve the login form:\n%s", anon)
	}
	for _, p := range append(append([]domain.Person{}, w.staying...), w.riding...) {
		if strings.Contains(anon, p.Name) {
			t.Errorf("unauthenticated detail leaked the name %q", p.Name)
		}
	}
}

// TestAnalyticsSiteDetailIsReadOnly — nothing on the page can record
// anything, and nothing sent to the route does. The event count is read
// before and after every attempt: a 405 that still authored would pass a
// status-code check and fail this one.
func TestAnalyticsSiteDetailIsReadOnly(t *testing.T) {
	w := anRoadWorld(t)
	code, body := anSiteGet(t, w.srv, "GET", "/analytics/site/site-a", true)
	if code != http.StatusOK {
		t.Fatalf("GET detail = %d", code)
	}
	// Precondition: the page is populated, so "no form" is not vacuous.
	if !strings.Contains(body, w.staying[0].Name) || !strings.Contains(body, "TRK-100") {
		t.Fatalf("precondition: the detail must show site-a's people and its run:\n%s", body)
	}
	markup := styleRe.ReplaceAllString(body, "") // the shared stylesheet names every class
	// The alert strip is looked for as an ELEMENT (class="topbar-alerts"):
	// F14's layout script (_layout.html, topbar-offset-script) names the
	// ".topbar-alerts" selector on every page, so the bare string is no
	// longer evidence of alert UI. ⟦was: "topbar-alerts"⟧
	for _, bad := range []string{"<form", "<button", "<input", "<select", "<textarea", "/alerts/ack", `class="topbar-alerts"`} {
		if strings.Contains(markup, bad) {
			t.Errorf("read-only detail page carries %q", bad)
		}
	}
	before := anEventCount(t, w)
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		for _, login := range []bool{false, true} {
			code, _ := anSiteGet(t, w.srv, m, "/analytics/site/site-a", login)
			if code != http.StatusMethodNotAllowed {
				t.Errorf("%s detail (login=%v) = %d, want 405", m, login, code)
			}
		}
	}
	if after := anEventCount(t, w); after != before {
		t.Errorf("writing methods against the detail changed the log: %d -> %d events", before, after)
	}
}

func anEventCount(t *testing.T, w *roadWorld) int {
	t.Helper()
	evs, err := w.st.Events("site-a", 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	return len(evs)
}

// TestAnalyticsSiteDetailIsLive — a meta refresh no slower than the 10 s
// sync's useful window and an "as at" time; and the refresh is NOT on a page
// with forms (a refresh there would throw away typing).
func TestAnalyticsSiteDetailIsLive(t *testing.T) {
	w := anRoadWorld(t)
	_, body := anSiteGet(t, w.srv, "GET", "/analytics/site/site-a", true)
	m := regexp.MustCompile(`<meta http-equiv="refresh" content="(\d+)">`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("detail has no meta refresh:\n%s", body)
	}
	// James, 2026-10-03: 60 s, like /analytics ⟦was: 15⟧.
	if m[1] != "60" {
		t.Errorf("refresh = %s s, want 60", m[1])
	}
	if !regexp.MustCompile(`as at <span data-an-rendered>\d{4}-\d\d-\d\d \d\d:\d\d:\d\d</span>`).MatchString(body) {
		t.Errorf("detail lacks its 'as at' render time")
	}
	for _, path := range []string{"/gate", "/trips", "/muster"} {
		_, other := anSiteGet(t, w.srv, "GET", path, true)
		if strings.Contains(other, `http-equiv="refresh"`) {
			t.Errorf("%s must not auto-refresh", path)
		}
	}
}

// TestAnalyticsSiteDetailShowsWhatTheSitesOwnMusterAndTripsShow — the
// detail is the site's own reads pointed at its id, so the names must be
// EQUAL, not merely present: site-a's on-site roster and on-the-road
// manifest against site-a's /muster; site-b's inbound against site-b's own
// /trips inbound board (a second Server on the same store as site-b).
func TestAnalyticsSiteDetailShowsWhatTheSitesOwnMusterAndTripsShow(t *testing.T) {
	w := anRoadWorld(t)
	_, detA := anSiteGet(t, w.srv, "GET", "/analytics/site/site-a", true)
	_, muster := getMuster(t, w.srv, "/muster")

	wantOn, wantRoad := personNames(w.staying), personNames(w.riding)
	if got := anNames(t, muster, `data-muster-section="on-site"`); !equalStrings(got, wantOn) {
		t.Fatalf("precondition: site-a's /muster on-site = %v, want %v", got, wantOn)
	}
	if got := anNames(t, detA, `data-an-section="on-site"`); !equalStrings(got, wantOn) {
		t.Errorf("detail on-site = %v, want the muster's %v", got, wantOn)
	}
	if got := anNames(t, muster, `data-muster-section="in-transit"`); !equalStrings(got, wantRoad) {
		t.Fatalf("precondition: site-a's /muster on the road = %v, want %v", got, wantRoad)
	}
	if got := anNames(t, detA, `data-an-runs="outbound"`); !equalStrings(got, wantRoad) {
		t.Errorf("detail on the road from site-a = %v, want the muster's %v", got, wantRoad)
	}
	if !strings.Contains(detA, `data-an-count="on-site">3<`) || !strings.Contains(detA, `data-an-count="outbound">3<`) {
		t.Errorf("detail headcounts must read 3 on site / 3 on the road")
	}

	_, detB := anSiteGet(t, w.srv, "GET", "/analytics/site/site-b", true)
	if got := anNames(t, detB, `data-an-runs="inbound"`); !equalStrings(got, wantRoad) {
		t.Errorf("detail on the road to site-b = %v, want %v", got, wantRoad)
	}
	if got := anNames(t, detB, `data-an-section="on-site"`); len(got) != 0 {
		t.Errorf("site-b has nobody on site, detail lists %v", got)
	}
	siteB := New(&config.Config{SiteID: "site-b", DisplayName: "Pad Site Control", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: w.st.Path, SyncIntervalSeconds: 10, LoginPassword: testLoginPassword,
		Peers: []config.Peer{{SiteID: "site-a", BaseURL: "http://example.invalid"}}}, w.st)
	_, trips := anSiteGet(t, siteB, "GET", "/trips", true)
	for _, p := range w.riding {
		if !strings.Contains(trips, p.Name) {
			t.Fatalf("precondition: site-b's own /trips does not show inbound rider %q", p.Name)
		}
	}
	if !strings.Contains(trips, "TRK-100") || !strings.Contains(detB, "TRK-100") {
		t.Errorf("both site-b's /trips and the detail must name the inbound vehicle TRK-100")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestAnalyticsSiteDetailOfAStaleSiteSaysSo — the same §8.4 verdict as the
// card: healthy peer → CURRENT, no warning; stalled → the STALE / verify by
// radio warning, with the last-known person still listed (demoted, never
// hidden).
func TestAnalyticsSiteDetailOfAStaleSiteSaysSo(t *testing.T) {
	_, ok := anSiteGet(t, anWithPeerAnn(t, true, 2, ""), "GET", "/analytics/site/site-a", true)
	if strings.Contains(ok, "STALE") || !strings.Contains(ok, ">CURRENT<") {
		t.Errorf("a healthy peer's detail must read CURRENT with no STALE warning")
	}
	if !strings.Contains(ok, "Ann Arrival") {
		t.Errorf("precondition: Ann is on site at site-a")
	}
	_, stale := anSiteGet(t, anWithPeerAnn(t, true, 600, ""), "GET", "/analytics/site/site-a", true)
	for _, want := range []string{"data-an-stale", "STALE — LAST KNOWN — verify by radio.", "Ann Arrival"} {
		if !strings.Contains(stale, want) {
			t.Errorf("a stalled peer's detail lacks %q", want)
		}
	}
	if strings.Contains(stale, ">CURRENT<") {
		t.Errorf("a stalled peer's detail must not read CURRENT")
	}
}

// TestAnalyticsSiteDetailRunVerdicts — a run NOT bound for this post never
// reads OVERDUE: inside its allowance (by the origin's clock) it says so;
// past it, PAST ALLOWANCE + verify by radio. The run's destination is a
// healthy peer so the stale branch cannot pre-empt the case.
func TestAnalyticsSiteDetailRunVerdicts(t *testing.T) {
	w := anRoadWorld(t) // site-b: a healthy peer
	_, body := anSiteGet(t, w.srv, "GET", "/analytics", true)
	if card := anSiteCard(t, body, "site-b"); !strings.Contains(card, ">CURRENT<") {
		t.Fatalf("precondition: site-b must be current for this case:\n%s", card)
	}
	_, body = anSiteGet(t, w.srv, "GET", "/analytics/site/site-a", true)
	run := body[strings.Index(body, `data-an-runs="outbound"`):]
	run = run[:strings.Index(run, "</article>")]
	if strings.Contains(run, "OVERDUE") {
		t.Errorf("a run bound for another post must never read OVERDUE here:\n%s", run)
	}
	// newRoadWorld's store clock is the real one, and the run was dispatched
	// just now with a 90-minute allowance: inside it.
	if !strings.Contains(run, "WITHIN ALLOWANCE") || !strings.Contains(run, "allowance 90 minutes") {
		t.Errorf("fresh 90-minute run must read WITHIN ALLOWANCE with its allowance:\n%s", run)
	}
	// Two hours on (this post's clock), the 90-minute run is past its
	// allowance — and still never OVERDUE here.
	later := time.Now().Add(2 * time.Hour).UnixMilli()
	w.st.Clock = func() store.ClockReading {
		return store.ClockReading{WallMS: later, MaxErrorUS: 500, State: store.ClockSynced}
	}
	_, body = anSiteGet(t, w.srv, "GET", "/analytics/site/site-a", true)
	run = body[strings.Index(body, `data-an-runs="outbound"`):]
	run = run[:strings.Index(run, "</article>")]
	// NEW-a (James, 2026-10-03): the origin judges its own outbound runs
	// too, so the sentence names BOTH posts on the run. ⟦was: "only site-b
	// can call it overdue"⟧ — this fixture's post runs no scanner
	// (DeadlineNote), which is why its own run still reads PAST ALLOWANCE
	// here rather than the scan's verdict; the scanned case is
	// TestAnalyticsSiteDetailShowsThisPostsOwnOutboundVerdict.
	// FIX3 F3d (FINDINGS-R5 L11-RAW-1 / L15 TEXT-2; "no screen shows a raw
	// site id"): both posts by display name. ⟦was: "only site-a or site-b
	// can call it overdue"⟧
	whose := "only " + w.srv.Cfg.SiteName("site-a") + " or " + w.srv.Cfg.SiteName("site-b") + " can call it overdue"
	if !strings.Contains(run, "PAST ALLOWANCE") || !strings.Contains(run, "verify by radio") || !strings.Contains(run, whose) || strings.Contains(run, "OVERDUE") {
		t.Errorf("a run past its allowance must say PAST ALLOWANCE, verify by radio, and whose call overdue is — never OVERDUE:\n%s", run)
	}
}

// TestAnalyticsSiteDetailCarriesNoPhoneNumber — PII-1: a phone recorded on
// a person on site never reaches the page, while the name does.
func TestAnalyticsSiteDetailCarriesNoPhoneNumber(t *testing.T) {
	w := anRoadWorld(t)
	ph := "907-555-0142"
	if _, err := domain.AmendPerson(w.st, "site-a", 1, domain.PersonAmendment{PersonID: w.staying[0].PersonID, Phone: &ph}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if p, err := domain.PersonByID(w.st, w.staying[0].PersonID); err != nil || p == nil || p.Phone == nil || *p.Phone != ph {
		t.Fatalf("precondition: the phone must be on record (%v)", err)
	}
	_, body := anSiteGet(t, w.srv, "GET", "/analytics/site/site-a", true)
	if !strings.Contains(body, w.staying[0].Name) {
		t.Fatalf("precondition: the person must be on the page")
	}
	for _, leak := range []string{ph, "9075550142", "555-0142"} {
		if strings.Contains(body, leak) {
			t.Errorf("detail page carries the phone number (%q) — PII-1", leak)
		}
	}
}

func TestAnalyticsSiteDetailOfAnUnknownSiteIsNotFound(t *testing.T) {
	w := anRoadWorld(t)
	code, body := anSiteGet(t, w.srv, "GET", "/analytics/site/nowhere", true)
	if code != http.StatusNotFound || !strings.Contains(body, "NO SUCH SITE") {
		t.Errorf("unknown site = %d, want 404 NO SUCH SITE", code)
	}
	if strings.Contains(body, `data-an-count="on-site"`) {
		t.Errorf("an unknown site must not render a confident headcount")
	}
}

// TestAnalyticsPagesFillTheScreen — the CSS contract behind "auto resize":
// both analytics pages open their column as an-page; that column lifts the
// 72rem cap and scales its type tokens with clamp(); no analytics rule sets
// a fixed minimum width a 360 px phone cannot hold. (The rendered check at
// 360/768/1366/1920/3840 is the live evidence; this is the structural half.)
func TestAnalyticsPagesFillTheScreen(t *testing.T) {
	w := anRoadWorld(t)
	for _, path := range []string{"/analytics", "/analytics/site/site-a"} {
		_, body := anSiteGet(t, w.srv, "GET", path, true)
		if !strings.Contains(body, `<main class="page an-page">`) {
			t.Errorf("%s does not open its column as an-page", path)
		}
	}
	_, body := anSiteGet(t, w.srv, "GET", "/analytics", true)
	css := regexp.MustCompile(`(?s)<style>(.*?)</style>`).FindStringSubmatch(body)[1]
	var rules string
	for _, m := range regexp.MustCompile(`(?s)\.page\.an-page[^{]*\{(.*?)\}`).FindAllStringSubmatch(css, -1) {
		rules += m[1]
	}
	if rules == "" {
		t.Fatal("no .page.an-page rule")
	}
	for _, want := range []string{"max-width: none", "--fs-base: clamp(", "--fs-countdown: clamp(", "font-size: var(--fs-base)"} {
		if !strings.Contains(rules, want) {
			t.Errorf(".page.an-page rules lack %q", want)
		}
	}
	// Every analytics rule: no px/rem/em minimum or fixed width above a phone.
	for _, rule := range regexp.MustCompile(`(?m)^[^{}\n]*\.an-[^{}]*\{[^}]*\}`).FindAllString(css, -1) {
		for _, m := range regexp.MustCompile(`(?:^|[;{\s])(min-width|width):\s*([\d.]+)(px|rem|em)`).FindAllStringSubmatch(rule, -1) {
			if m[2] != "0" {
				t.Errorf("analytics rule sets a fixed %s %s%s — a phone may not hold it: %s", m[1], m[2], m[3], rule)
			}
		}
		if strings.Contains(rule, "minmax(") && !strings.Contains(rule, "minmax(min(100%") && !strings.Contains(rule, "minmax(0") {
			t.Errorf("analytics grid track without a min(100%%, …) guard can overflow a phone: %s", rule)
		}
	}
}

// anHealthySnapshot is a sync snapshot in which every named peer was heard
// from contactAgo seconds ago with no error and one caught-up stream — the
// Healthy verdict (syncstatus.go wants observed stream state for it).
func anHealthySnapshot(own string, peer string, contactAgo *float64) func() repl.Snapshot {
	return func() repl.Snapshot {
		return repl.Snapshot{SiteID: own,
			Peers: []repl.PeerSnapshot{{SiteID: peer, LastContactAgoSec: contactAgo}},
			Streams: []repl.StreamStatus{{StreamKey: repl.StreamKey{SiteID: peer, NodeEpoch: 1},
				Watermark: 0, Head: 0, State: repl.StateOK}}}
	}
}
