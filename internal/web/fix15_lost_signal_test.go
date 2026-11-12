package web

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/repl"
	"sitecontrol/internal/store"
)

// FIX15 (James, 2026-10-06: "i need a lost signal notification on the sites
// to notify on the dispatch page if signal is dropped"). lostsignal.go has
// the design. These tests drive the served surface (Root) with a hand-built
// sync snapshot: a peer is LOST once its contact age passes the STALE
// ceiling (3 × the 10 s interval = 30 s here), and not before.

// fix15Peer is one peer in a fix15 snapshot: contact age in seconds (nil =
// none since start) and the last pull's error kind ("" = no error).
type fix15Peer struct {
	id      string
	contact *float64
	errKind string
}

func fix15Ago(sec float64) *float64 { return &sec }

// fix15Snapshot builds site-c's snapshot over the given peers. A peer with
// an error kind carries a LastError of that kind; each has an ok stream.
func fix15Snapshot(own string, uptime float64, peers ...fix15Peer) func() repl.Snapshot {
	return func() repl.Snapshot {
		snap := repl.Snapshot{SiteID: own, UptimeSec: uptime}
		for _, p := range peers {
			ps := repl.PeerSnapshot{SiteID: p.id, LastContactAgoSec: p.contact}
			if p.errKind != "" {
				ps.LastError = "sync " + p.id + ": " + p.errKind
				ps.ErrorKind = p.errKind
			}
			snap.Peers = append(snap.Peers, ps)
			snap.Streams = append(snap.Streams, repl.StreamStatus{StreamKey: repl.StreamKey{SiteID: p.id, NodeEpoch: 1}, State: repl.StateOK})
		}
		return snap
	}
}

var fix15LineRe = regexp.MustCompile(`(?s)<div class="notice notice-fault banner-notice alert-line lost-signal".*?</div>`)

func fix15Line(body string) string { return fix15LineRe.FindString(body) }

// The dispatch host names each lost site with its last-heard time, on every
// dispatch page, as plain text; the line is gone once contact is back.
func TestFix15DispatchStripShowsLostSignalForAStalePeerAndClearsOnHeal(t *testing.T) {
	srv, _, _ := fix14ThirdPost(t)
	// site-a: last heard 8 minutes ago, the pulls since then refused at its
	// address — past the 30 s ceiling. site-b: heard 2 s ago.
	srv.SyncSnapshot = fix15Snapshot("site-c", 3600,
		fix15Peer{"site-a", fix15Ago(480), repl.ErrorKindUnreachable}, fix15Peer{"site-b", fix15Ago(2), ""})
	heard := time.Now().Add(-480 * time.Second).Format("15:04")
	for _, p := range []string{"/analytics", "/analytics/site/site-b", "/search"} {
		body := fix7Do(t, srv, "GET", fix7DispatchHost, p, nil).Body.String()
		line := fix15Line(body)
		if line == "" {
			t.Fatalf("%s: no LOST SIGNAL line on the dispatch host with site-a 8 minutes silent", p)
		}
		for _, want := range []string{"LOST SIGNAL:", `data-lost-site="site-a"`, "Road Post — last heard " + heard + " (8 minutes ago)",
			"Road Post, lost signal, last heard 8 minutes ago", "more than 3× the sync interval (10s), i.e. 30s — the same ceiling the sync status uses to call a site stale"} {
			if !strings.Contains(line, want) {
				t.Errorf("%s: LOST SIGNAL line lacks %q:\n%s", p, want, line)
			}
		}
		if strings.Contains(line, "site-b") || strings.Contains(line, "Pad Post") {
			t.Errorf("%s: a peer heard 2 s ago is listed as lost:\n%s", p, line)
		}
		if strings.Contains(line, "<a ") || strings.Contains(line, "<form") || strings.Contains(line, `role="alert"`) || strings.Contains(line, "aria-live") {
			t.Errorf("%s: the line must be plain text with no link, form or live region:\n%s", p, line)
		}
		// Inside the stuck top bar (the alert area, or the signal box on a
		// page with no alert strip), so it rides every scroll position.
		bar := body[strings.Index(body, `<header class="topbar">`):strings.Index(body, "</header>")]
		if !strings.Contains(bar, "LOST SIGNAL:") {
			t.Errorf("%s: the line is not inside the sticky top bar", p)
		}
		if p == "/analytics" && !strings.Contains(body, `<meta http-equiv="refresh" content="60">`) {
			t.Errorf("/analytics must keep its 60 s refresh, which keeps the line current")
		}
	}
	// Inside the ceiling it is not lost yet: one failed pull 12 s after the
	// last contact is not a dropped signal.
	srv.SyncSnapshot = fix15Snapshot("site-c", 3600,
		fix15Peer{"site-a", fix15Ago(12), repl.ErrorKindUnreachable}, fix15Peer{"site-b", fix15Ago(2), ""})
	if line := fix15Line(fix7Do(t, srv, "GET", fix7DispatchHost, "/analytics", nil).Body.String()); line != "" {
		t.Errorf("12 s silent is inside the 30 s ceiling and must not read LOST SIGNAL:\n%s", line)
	}
	// Healed: both heard just now — the line clears by itself.
	srv.SyncSnapshot = fix15Snapshot("site-c", 3600, fix15Peer{"site-a", fix15Ago(1), ""}, fix15Peer{"site-b", fix15Ago(2), ""})
	for _, p := range []string{"/analytics", "/analytics/site/site-b", "/search"} {
		body := fix7Do(t, srv, "GET", fix7DispatchHost, p, nil).Body.String()
		if strings.Contains(body, "LOST SIGNAL") {
			t.Errorf("%s: LOST SIGNAL still shown after the peer came back:\n%s", p, fix15Line(body))
		}
		if strings.Contains(body, "sc:lost-signal") {
			t.Errorf("%s: the slot marker leaked into the page", p)
		}
	}
}

// The serving post cut off from EVERY peer is one line about the dispatch
// server, not one item per site.
func TestFix15DispatchAllPeersLostIsOneLine(t *testing.T) {
	srv, _, _ := fix14ThirdPost(t)
	srv.SyncSnapshot = fix15Snapshot("site-c", 3600,
		fix15Peer{"site-a", fix15Ago(300), repl.ErrorKindUnreachable}, fix15Peer{"site-b", nil, repl.ErrorKindUnreachable})
	body := fix7Do(t, srv, "GET", fix7DispatchHost, "/analytics", nil).Body.String()
	line := fix15Line(body)
	if !strings.Contains(line, `data-lost-signal="all"`) || !strings.Contains(line, "LOST SIGNAL:") ||
		!strings.Contains(line, "Dispatch server (North Dock) can't reach any site — figures may be out of date") {
		t.Fatalf("all peers lost must read as ONE dispatch-server line:\n%s", line)
	}
	if strings.Contains(line, "data-lost-site=") || strings.Count(body, "LOST SIGNAL:") != 1 {
		t.Errorf("all peers lost must not list the sites one by one:\n%s", line)
	}
	if !strings.Contains(line, "Road Post, lost signal, last heard 5 minutes ago") ||
		!strings.Contains(line, "Pad Post, lost signal, not heard since North Dock started 1 hour ago") {
		t.Errorf("the one line must still give each site's spoken last-heard:\n%s", line)
	}
	// A restart inside the ceiling is not a lost signal: nothing heard yet,
	// but the post has only been up 5 s.
	srv.SyncSnapshot = fix15Snapshot("site-c", 5, fix15Peer{"site-a", nil, ""}, fix15Peer{"site-b", nil, ""})
	if line := fix15Line(fix7Do(t, srv, "GET", fix7DispatchHost, "/analytics", nil).Body.String()); line != "" {
		t.Errorf("5 s after start, with no pull finished yet, is not lost signal:\n%s", line)
	}
}

// A post's own pages say which sites its entries are not reaching, on the
// post's own wording, and keep the /status detail.
func TestFix15PostHostShowsItsOwnLostSignalLine(t *testing.T) {
	srv, _, _ := fix14ThirdPost(t)
	srv.SyncSnapshot = fix15Snapshot("site-c", 3600,
		fix15Peer{"site-a", fix15Ago(600), repl.ErrorKindUnreachable}, fix15Peer{"site-b", fix15Ago(3), ""})
	for _, p := range []string{"/trips", "/status", "/muster", "/analytics", "/analytics/site/site-a", "/search"} {
		body := fix7Do(t, srv, "GET", fix7GateHost, p, nil).Body.String()
		line := fix15Line(body)
		if !strings.Contains(line, "LOST SIGNAL:") || !strings.Contains(line, "can't reach") ||
			!strings.Contains(line, `data-lost-site="site-a"`) || !strings.Contains(line, "Road Post") ||
			!strings.Contains(line, "your entries reach them when the link returns") {
			t.Errorf("%s: post host must show its own LOST SIGNAL line naming Road Post:\n%s", p, line)
		}
		if strings.Contains(line, "Dispatch server") || strings.Contains(line, "Pad Post") {
			t.Errorf("%s: post line uses dispatch wording or lists a peer in contact:\n%s", p, line)
		}
	}
	// /status keeps the full verdict.
	if body := fix7Do(t, srv, "GET", fix7GateHost, "/status", nil).Body.String(); !strings.Contains(body, "the most recent sync attempt FAILED") {
		t.Errorf("/status must keep its per-peer detail")
	}
	// Every peer lost: the post's one-sentence form.
	srv.SyncSnapshot = fix15Snapshot("site-c", 3600,
		fix15Peer{"site-a", fix15Ago(600), repl.ErrorKindUnreachable}, fix15Peer{"site-b", fix15Ago(600), repl.ErrorKindUnreachable})
	line := fix15Line(fix7Do(t, srv, "GET", fix7GateHost, "/trips", nil).Body.String())
	if !strings.Contains(line, "can't reach any other site — your entries reach them when the link returns") {
		t.Errorf("a post cut off from every peer must say so in one sentence:\n%s", line)
	}
	// The login page carries no line (nobody signed in).
	srv.SyncSnapshot = fix15Snapshot("site-c", 3600, fix15Peer{"site-a", fix15Ago(600), repl.ErrorKindUnreachable})
	if body := rawPage(t, srv, "/login"); strings.Contains(body, "LOST SIGNAL") {
		t.Errorf("the login page must not carry the line")
	}
}

// The Signal tile on /analytics lists every peer as OK / LOST / CHECK with
// when it was last heard.
func TestFix15AnalyticsSignalTile(t *testing.T) {
	srv, _, _ := fix14ThirdPost(t)
	srv.SyncSnapshot = fix15Snapshot("site-c", 3600,
		fix15Peer{"site-a", fix15Ago(480), repl.ErrorKindUnreachable}, fix15Peer{"site-b", fix15Ago(4), ""})
	body := fix7Do(t, srv, "GET", fix7DispatchHost, "/analytics", nil).Body.String()
	i := strings.Index(body, `data-analytics-tile="signal"`)
	if i < 0 {
		t.Fatalf("/analytics has no Signal tile")
	}
	tile := body[i:]
	tile = tile[:strings.Index(tile, "</ul>")]
	for _, want := range []string{`data-analytics-signal-lost="1"`, ">Signal<",
		`data-signal-site="site-a" data-signal="lost"><strong class="an-signal-state">LOST</strong> Road Post — last heard`,
		`data-signal-site="site-b" data-signal="ok"><strong class="an-signal-state">OK</strong> Pad Post — last heard`, "(4 seconds ago)"} {
		if !strings.Contains(tile, want) {
			t.Errorf("Signal tile lacks %q:\n%s", want, tile)
		}
	}
	if !strings.Contains(body[i-200:i], "an-tile-crit") {
		t.Errorf("a tile with a LOST site must be marked critical")
	}
	srv.SyncSnapshot = fix15Snapshot("site-c", 3600, fix15Peer{"site-a", fix15Ago(1), ""}, fix15Peer{"site-b", fix15Ago(4), ""})
	body = fix7Do(t, srv, "GET", fix7DispatchHost, "/analytics", nil).Body.String()
	if !strings.Contains(body, `data-analytics-signal-lost="0"`) || strings.Contains(body, `data-signal="lost"`) {
		t.Errorf("healed: the tile must show every site OK")
	}
}

// fix15DispatchConfig loads a dispatch post's config the way sitectl does,
// with the peer that sends road work telling this post its label.
func fix15DispatchConfig(t *testing.T, dbPath, peerExtra string) *config.Config {
	t.Helper()
	text := `site_id = "site-c"
display_name = "North Dock"
listen_addr = "127.0.0.1:0"
db_path = "` + dbPath + `"
node_epoch = 1
peer_key = "k-site-c"
login_password = "` + testLoginPassword + `"
sync_interval_seconds = 10
orientation = false
site_type = "text_message"
destinations = []

[[peers]]
site_id = "site-a"
display_name = "Road Post"
base_url = "http://127.0.0.1:9"
key = "k-site-a"
` + peerExtra + `

[[peers]]
site_id = "site-b"
display_name = "Pad Post"
base_url = "http://127.0.0.1:9"
key = "k-site-b"
`
	p := filepath.Join(t.TempDir(), "site.toml")
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatalf("load the dispatch post's config: %v", err)
	}
	return cfg
}

// From the dispatch post (no [road_work] of its own) the tile is named by
// the post that sends the road work, not by the serving post's default.
func TestFix15RoadWorkTileLabelFromTheSendingPost(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	if err := deadline.Register(st); err != nil {
		t.Fatal(err)
	}
	cfg := fix15DispatchConfig(t, st.Path, `road_work_label = "Working on the ice road"`)
	srv := New(cfg, st)
	label := func() string {
		body := fix7Do(t, srv, "GET", fix7DispatchHost, "/analytics", nil).Body.String()
		i := strings.Index(body, `data-analytics-tile="road-work"`)
		if i < 0 {
			t.Fatalf("no road-work tile")
		}
		m := regexp.MustCompile(`<div class="an-label">([^<]*)</div>`).FindStringSubmatch(body[i:])
		return m[1]
	}
	if got := label(); got != "Working on the ice road" {
		t.Errorf("before any road work, the one peer that names its road work names the tile: got %q", got)
	}
	// A grader sent out working on the road by site-a, replicated here.
	driver, err := domain.CreatePerson(st, "site-a", 1, "Quill, Ada", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	checkIn(t, st, "site-a", driver.PersonID, nil)
	limit := int64(720)
	if _, err := domain.Dispatch(st, "site-a", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, st, "site-a", "GRADER 7"), DriverPersonID: driver.PersonID,
		RoadWork: true, DeadlineMinutes: &limit,
	}, time.Now().UnixMilli(), "entered"); err != nil {
		t.Fatal(err)
	}
	if got := label(); got != "Working on the ice road" {
		t.Errorf("site-a's road work must be named by site-a's label from the dispatch post: got %q", got)
	}
	// Without the peer's label the default stands (no guess at a name).
	srv2 := New(fix15DispatchConfig(t, st.Path, ""), st)
	body := fix7Do(t, srv2, "GET", fix7DispatchHost, "/analytics", nil).Body.String()
	i := strings.Index(body, `data-analytics-tile="road-work"`)
	if i < 0 || !strings.Contains(body[i:i+400], `<div class="an-label">Working on the road</div>`) {
		t.Errorf("no peer label configured: the tile keeps the default")
	}
}

// A peer that answers but refuses this post's key is a KEY FAULT, worded as
// one — never LOST SIGNAL, however long since the last good exchange.
func TestFix15KeyMismatchIsNotLostSignal(t *testing.T) {
	srv, _, _ := fix14ThirdPost(t)
	for _, kind := range []string{repl.ErrorKindKeyRejected, repl.ErrorKindIdentityRejected} {
		srv.SyncSnapshot = fix15Snapshot("site-c", 3600,
			fix15Peer{"site-a", fix15Ago(900), kind}, fix15Peer{"site-b", nil, kind})
		for _, host := range []string{fix7DispatchHost, fix7GateHost} {
			body := fix7Do(t, srv, "GET", host, "/analytics", nil).Body.String()
			if strings.Contains(body, "LOST SIGNAL") {
				t.Errorf("%s/%s: a key refusal must not read as LOST SIGNAL:\n%s", kind, host, fix15Line(body))
			}
			if strings.Contains(body, `data-signal="lost"`) || !strings.Contains(body, `data-signal="check"`) ||
				!strings.Contains(body, "key mismatch — see the sync status") {
				t.Errorf("%s/%s: the Signal tile must show the key fault as CHECK, not LOST", kind, host)
			}
		}
		if body := fix7Do(t, srv, "GET", fix7GateHost, "/status", nil).Body.String(); !strings.Contains(body, "PEER REJECTS THIS POST") {
			t.Errorf("%s: /status must keep the key-fault wording", kind)
		}
	}
	// The same peer with the link actually cut IS lost.
	srv.SyncSnapshot = fix15Snapshot("site-c", 3600,
		fix15Peer{"site-a", fix15Ago(900), repl.ErrorKindUnreachable}, fix15Peer{"site-b", fix15Ago(1), ""})
	if line := fix15Line(fix7Do(t, srv, "GET", fix7DispatchHost, "/analytics", nil).Body.String()); !strings.Contains(line, `data-lost-site="site-a"`) {
		t.Errorf("an unreachable peer past the ceiling must be LOST:\n%s", line)
	}
}
