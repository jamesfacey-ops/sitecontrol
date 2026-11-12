// analytics_test.go — GET /analytics: the figures over a seeded event log,
// the login gate, and the §8.4 staleness rule (a stalled peer's figures are
// flagged and NOT counted as current). Every scenario authors real events
// through the domain doors under an injected store.Clock and reads the page
// through srv.Handler(), the way a browser would.
package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/repl"
	"sitecontrol/internal/store"
)

// anLocal is an instant on the fixture's local day (the page's "today" is the
// local day of the store clock).
func anLocal(hour, min int) int64 {
	return time.Date(2026, 9, 29, hour, min, 0, 0, time.Local).UnixMilli()
}

// anStore opens a registered store whose clock the caller moves.
func anStore(t *testing.T) (*store.Store, func(int64)) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	var now int64
	st.Clock = func() store.ClockReading {
		return store.ClockReading{WallMS: now, MaxErrorUS: 500, State: store.ClockSynced}
	}
	return st, func(ms int64) { now = ms }
}

// anCheckIn is the ordinary gate check-in at siteID for the named people.
func anCheckIn(t *testing.T, st *store.Store, siteID, truckNo string, driver string, occupants ...string) {
	t.Helper()
	veh, err := domain.CreateVehicle(st, siteID, 1, &truckNo, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	trip, err := domain.DispatchLocalCheckIn(st, siteID, 1, domain.DispatchInput{
		VehicleID: veh.VehicleID, DriverPersonID: driver, OccupantPersonIDs: occupants,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.RecordExpectedArrival(st, siteID, 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
}

func anRoster(t *testing.T, st *store.Store, siteID string, names ...string) []domain.Person {
	t.Helper()
	co, err := domain.CreateCompany(st, siteID, 1, "Kakivik Asset Management", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	tr, err := domain.CreateTrade(st, siteID, 1, "Inspection", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	var out []domain.Person
	for _, n := range names {
		p, err := domain.CreatePerson(st, siteID, 1, n, &co.CompanyID, &tr.TradeID, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

func anServer(st *store.Store) *Server {
	cfg := &config.Config{
		SiteID: "site-b", DisplayName: "Ice Camp B", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 10,
		Peers:         []config.Peer{{SiteID: "site-a", BaseURL: "http://example.invalid"}},
		LoginPassword: testLoginPassword,
	}
	return New(cfg, st)
}

func anGet(t *testing.T, srv *Server, login bool) (int, string) {
	t.Helper()
	req := httptest.NewRequest("GET", "/analytics", nil)
	if login {
		req.AddCookie(loginCookie(t, srv, testLoginPassword))
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// anTile pulls the numeral of the overview tile whose label is label.
func anTile(t *testing.T, body, label string) string {
	t.Helper()
	m := regexp.MustCompile(`(?s)<div class="an-num">\s*(\d+|\?)[^<]*(?:<small>[^<]*</small>)?\s*</div>\s*<div class="an-label">` + regexp.QuoteMeta(label)).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no overview tile labelled %q in page:\n%s", label, body)
	}
	return m[1]
}

// anSiteCard returns the card html of one site.
func anSiteCard(t *testing.T, body, siteID string) string {
	t.Helper()
	for _, part := range strings.Split(body, `<article class="card an-site`)[1:] {
		card := part[:strings.Index(part, "</article>")]
		// James, 2026-10-03: the site name is a link to its read-only live
		// detail (analytics_site.go) — so a card is found by that link, which
		// every card-reading test below therefore also pins.
		// ⟦was: strings.Contains(card, "<h3>"+siteID)⟧
		// James, 2026-10-04 (cluster I): the link TEXT is the site's display
		// name, never its raw id; the href still carries the id, so the card
		// is found by the href. ⟦was: ...">`+siteID+`</a>⟧
		if strings.Contains(card, `<h3><a class="an-site-link" href="/analytics/site/`+siteID+`">`) {
			return card
		}
	}
	t.Fatalf("no card for %s in page", siteID)
	return ""
}

// TestAnalyticsPageShowsTheFiguresOfTheSeededLog — site-b's own log: Dana and
// Pat check in at 10:00, Pat signs out at 11:30; it is 12:00.
//
//	persons on site now 1 (Dana); visits today 2 / season 2;
//	avg time on site, today and season: Pat's 90 min (rendered 1h 30m) over n=1; no travel.
func TestAnalyticsPageShowsTheFiguresOfTheSeededLog(t *testing.T) {
	st, setClock := anStore(t)
	srv := anServer(st)
	setClock(anLocal(9, 0))
	ps := anRoster(t, st, "site-b", "Dana Driver", "Pat Passenger")
	setClock(anLocal(10, 0))
	anCheckIn(t, st, "site-b", "TRUCK 7", ps[0].PersonID, ps[1].PersonID)
	visits, err := domain.OpenVisitsAt(st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	setClock(anLocal(11, 30))
	for _, v := range visits {
		if v.PersonID == ps[1].PersonID {
			if err := domain.CheckOutVisit(st, "site-b", 1, v.VisitID, nil, 0, "default"); err != nil {
				t.Fatal(err)
			}
		}
	}
	setClock(anLocal(12, 0))

	code, body := anGet(t, srv, true)
	if code != http.StatusOK {
		t.Fatalf("GET /analytics = %d", code)
	}
	if got := anTile(t, body, "persons on site now"); got != "1" {
		t.Errorf("overview persons on site now = %s, want 1", got)
	}
	card := anSiteCard(t, body, "site-b")
	for _, want := range []string{
		`<dt>Persons on site now</dt><dd class="an-big">1</dd>`,
		`<dt>Visits today / season</dt><dd>2 / 2</dd>`,
		`<dt>Avg time on site, today</dt><dd>1h 30m (n=1)</dd>`,
		`<dt>Avg time on site, season</dt><dd>1h 30m (n=1)</dd>`,
		`<dt>Travel out, today / season</dt><dd>0 / 0</dd>`,
		`<dt>Travel in, today / season</dt><dd>0 / 0</dd>`,
		`this post — records authored here, live`,
	} {
		if !strings.Contains(card, want) {
			t.Errorf("site-b card lacks %q:\n%s", want, card)
		}
	}
	if strings.Contains(body, "STALE DATA") {
		t.Errorf("a single-site log must raise no stale warning:\n%s", body)
	}
}

// TestAnalyticsIsBehindLogin — without a session the page is the login form
// and carries no figure; with one it is the analytics page.
func TestAnalyticsIsBehindLogin(t *testing.T) {
	st, _ := anStore(t)
	srv := anServer(st)
	_, anon := anGet(t, srv, false)
	if strings.Contains(anon, "persons on site now") || !strings.Contains(anon, `name="password"`) {
		t.Errorf("unauthenticated GET /analytics must serve the login form and no figures:\n%s", anon)
	}
	_, authed := anGet(t, srv, true)
	if !strings.Contains(authed, "persons on site now") {
		t.Errorf("authenticated GET /analytics lacks the overview:\n%s", authed)
	}
}

// anWithPeerAnn builds site-b's server holding site-a's replicated stream, in
// which Ann is checked in at site-a and still on site. contactAgoSec /
// lastError shape the sync verdict for site-a; snapshot=false leaves the
// engine unwired.
func anWithPeerAnn(t *testing.T, snapshot bool, contactAgoSec float64, lastError string) *Server {
	t.Helper()
	a, setA := anStore(t)
	setA(anLocal(9, 0))
	ann := anRoster(t, a, "site-a", "Ann Arrival")
	setA(anLocal(10, 0))
	anCheckIn(t, a, "site-a", "TRUCK 1", ann[0].PersonID)

	b, setB := anStore(t)
	evs, err := a.Events("site-a", 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if _, err := b.Apply(ev); err != nil {
			t.Fatal(err)
		}
	}
	setB(anLocal(12, 0))
	srv := anServer(b)
	if snapshot {
		wm := int64(len(evs))
		srv.SyncSnapshot = func() repl.Snapshot {
			return repl.Snapshot{
				SiteID: "site-b",
				Peers:  []repl.PeerSnapshot{{SiteID: "site-a", LastContactAgoSec: &contactAgoSec, LastError: lastError}},
				Streams: []repl.StreamStatus{{
					StreamKey: repl.StreamKey{SiteID: "site-a", NodeEpoch: 1},
					Watermark: wm, Head: wm, State: repl.StateOK,
				}},
			}
		}
	}
	return srv
}

// TestAnalyticsStalePeerIsFlaggedAndNotCounted — the §8.4 rule, four ways.
// Ann is on site at site-a, which this post (site-b) knows only through
// replication.
//
//	peer heard 2 s ago, no error        → CURRENT: counted (1), no warning
//	peer last heard 600 s ago (>3×10 s) → STALE: NOT counted (0), warning names site-a and the 1 excluded person
//	peer's last attempt FAILED          → STALE, same
//	sync engine not wired               → STALE ("cannot tell"), same
func TestAnalyticsStalePeerIsFlaggedAndNotCounted(t *testing.T) {
	t.Run("healthy", func(t *testing.T) {
		_, body := anGet(t, anWithPeerAnn(t, true, 2, ""), true)
		if got := anTile(t, body, "persons on site now"); got != "1" {
			t.Errorf("healthy peer: total = %s, want 1", got)
		}
		if strings.Contains(body, "STALE") {
			t.Errorf("healthy peer must raise no stale marker:\n%s", body)
		}
		if card := anSiteCard(t, body, "site-a"); !strings.Contains(card, "CURRENT") {
			t.Errorf("healthy peer's card must read CURRENT:\n%s", card)
		}
	})
	stale := map[string]*Server{
		"heard too long ago":  anWithPeerAnn(t, true, 600, ""),
		"last attempt failed": anWithPeerAnn(t, true, 2, "connection refused"),
		"engine not wired":    anWithPeerAnn(t, false, 0, ""),
	}
	for name, srv := range stale {
		srv := srv
		t.Run(name, func(t *testing.T) {
			_, body := anGet(t, srv, true)
			if got := anTile(t, body, "persons on site now"); got != "0" {
				t.Errorf("stale peer: total = %s, want 0 — a stalled peer's persons must not read as current", got)
			}
			for _, want := range []string{"STALE DATA — site-a", "<strong>1</strong> person(s) are recorded on site only at"} {
				if !strings.Contains(body, want) {
					t.Errorf("stale peer: page lacks %q", want)
				}
			}
			card := anSiteCard(t, body, "site-a")
			if !strings.Contains(card, "STALE — LAST KNOWN") || strings.Contains(card, ">CURRENT<") {
				t.Errorf("stale peer's card must be flagged LAST KNOWN and not CURRENT:\n%s", card)
			}
			// The figure is still shown — demoted, not hidden — so the
			// officer can see what was last known.
			if !strings.Contains(card, `<dt>Persons on site now</dt><dd class="an-big">1</dd>`) {
				t.Errorf("stale peer's last-known figure must still be visible:\n%s", card)
			}
		})
	}
}

// TestAnalyticsAConfiguredPeerThatNeverDeliveredIsAStaleCardNotAnAbsence — a
// peer with no records at all is the plainest outage there is; if the page
// only listed sites that HAD records it would simply not mention the dead
// one. This post and the never-contacted peer each get a card, the peer's
// flagged STALE with the sync verdict's own words.
func TestAnalyticsAConfiguredPeerThatNeverDeliveredIsAStaleCardNotAnAbsence(t *testing.T) {
	st, setClock := anStore(t)
	setClock(anLocal(12, 0))
	srv := anServer(st)
	srv.SyncSnapshot = func() repl.Snapshot {
		return repl.Snapshot{SiteID: "site-b", Peers: []repl.PeerSnapshot{{SiteID: "site-a"}}}
	}
	_, body := anGet(t, srv, true)
	own := anSiteCard(t, body, "site-b")
	if !strings.Contains(own, "CURRENT") || !strings.Contains(own, `<dt>Persons on site now</dt><dd class="an-big">0</dd>`) {
		t.Errorf("this post's own empty card must be present and current:\n%s", own)
	}
	peer := anSiteCard(t, body, "site-a")
	// FIX2 cluster K (L08 T-074): ⟦was: "never contacted"⟧ — contact is
	// in-memory, so the verdict scopes itself to this process's start.
	if !strings.Contains(peer, "STALE — LAST KNOWN") || !strings.Contains(peer, "not heard from since this post started") {
		t.Errorf("a never-contacted peer must show as STALE with the verdict's words:\n%s", peer)
	}
	if !strings.Contains(body, "STALE DATA — site-a") {
		t.Errorf("page lacks the stale warning naming site-a")
	}
}
