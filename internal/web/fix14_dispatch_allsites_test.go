package web

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// FIX14 (James, 2026-10-05: "can we make the primary server of the dispatch
// page NDB, it will be the most likely to stay the longest"). Measured on a
// lane before the change: served by a post on neither end of the late runs,
// the dispatch strip was EMPTY and "overdue now" said 0. James's ruling, same
// day: such a run is a red OVERDUE on the dispatch host once past its
// deadline by the origin's recorded time. dispatch_allsites.go has the design.

// fix14ThirdPost is a post (site-c) that holds the replicated copy of a run
// between two OTHER posts: TRK-100, three aboard, site-a -> site-b, a
// 90-minute allowance, dispatched now.
func fix14ThirdPost(t *testing.T) (*Server, *store.Store, domain.Trip) {
	t.Helper()
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
	var riding []domain.Person
	for _, n := range []string{"Nadeau, Pierre", "Ohara, Wendy", "Petrov, Sasha"} {
		p, err := domain.CreatePerson(st, "site-a", 1, n, nil, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		riding = append(riding, p)
	}
	var ids []string
	for _, p := range riding {
		ids = append(ids, p.PersonID)
	}
	checkIn(t, st, "site-a", ids[0], ids[1:])
	trip := dispatchToPad(t, st, "TRK-100", riding, 90)
	cfg := &config.Config{
		SiteID: "site-c", DisplayName: "North Dock", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 10, LoginPassword: testLoginPassword,
		Peers: []config.Peer{
			{SiteID: "site-a", BaseURL: "http://example.invalid", DisplayName: "Road Post"},
			{SiteID: "site-b", BaseURL: "http://example.invalid", DisplayName: "Pad Post"},
		},
	}
	srv := New(cfg, st)
	// site-c runs its own deadline scan, as every post does; the run is
	// not bound for or sent from it, so the scan holds nothing for it.
	d, err := deadline.New(st, "site-c", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	srv.Alerts = d
	return srv, st, trip
}

// fix14Later moves the serving post's clock two hours on: the 90-minute run
// is past its deadline by its origin's recorded time.
func fix14Later(st *store.Store) {
	later := time.Now().Add(2 * time.Hour).UnixMilli()
	st.Clock = func() store.ClockReading {
		return store.ClockReading{WallMS: later, MaxErrorUS: 500, State: store.ClockSynced}
	}
}

var fix14StripRe = regexp.MustCompile(`(?s)<div class="alert-strip".*?</ul>\s*</div>\s*</div>`)

func fix14Strip(body string) string { return fix14StripRe.FindString(body) }

func TestFix14DispatchServedByThirdPostShowsOtherPostsOverdueRunsRed(t *testing.T) {
	srv, st, trip := fix14ThirdPost(t)

	// Inside the allowance: nothing to list.
	rec := fix7Do(t, srv, "GET", fix7DispatchHost, "/analytics", nil)
	if strings.Contains(rec.Body.String(), trip.TripID) || !strings.Contains(rec.Body.String(), `data-analytics-overdue-other="0"`) {
		t.Fatalf("a run inside its allowance is listed or counted:\n%s", fix14Strip(rec.Body.String()))
	}

	fix14Later(st)
	for _, p := range []string{"/analytics", "/search"} {
		body := fix7Do(t, srv, "GET", fix7DispatchHost, p, nil).Body.String()
		strip := fix14Strip(body)
		red := strip[strings.Index(strip, `data-alert-line="overdue"`)+1:]
		if !strings.Contains(strip, `data-alert-line="overdue"`) || !strings.Contains(red, "OVERDUE:") ||
			!strings.Contains(red, `data-trip-id="`+trip.TripID+`"`) || !strings.Contains(red, "<code>TRK-100</code> (3)") {
			t.Errorf("%s: dispatch served by a third post must name TRK-100 (3) on the red OVERDUE line:\n%s", p, strip)
		}
		if strings.Contains(strip, "PAST ALLOWANCE") || strings.Contains(strip, `data-alert-line="verify"`) ||
			strings.Contains(strip, "<form") || strings.Contains(strip, "<a ") {
			t.Errorf("%s: strip is amber/past-allowance or carries a link/form:\n%s", p, strip)
		}
	}
	body := fix7Do(t, srv, "GET", fix7DispatchHost, "/analytics", nil).Body.String()
	if !strings.Contains(body, `<div class="an-num">1</div>`) || !strings.Contains(body, `data-analytics-overdue-other="1"`) ||
		!strings.Contains(body, "judged by the origin post's recorded dispatch time") {
		t.Errorf("overdue now must count the run as 1 overdue, other posts' run, by the origin's recorded time")
	}
	body = fix7Do(t, srv, "GET", fix7DispatchHost, "/search?truck=TRK-100", nil).Body.String()
	if !strings.Contains(body, "overdue — on the road past its deadline") {
		t.Errorf("dispatch search must word the run as overdue")
	}
}

func TestFix14PostHostIsUnchanged(t *testing.T) {
	srv, st, trip := fix14ThirdPost(t)
	fix14Later(st)
	for _, p := range []string{"/analytics", "/search", "/search?truck=TRK-100"} {
		body := fix7Do(t, srv, "GET", fix7GateHost, p, nil).Body.String()
		if strings.Contains(fix14Strip(body), trip.TripID) || strings.Contains(body, "data-analytics-overdue-other") ||
			strings.Contains(body, "overdue — on the road") {
			t.Errorf("%s on a post host shows the dispatch-only all-sites verdict", p)
		}
	}
	// The site page keeps its third-post wording on every host.
	body := fix7Do(t, srv, "GET", fix7DispatchHost, "/analytics/site/site-a", nil).Body.String()
	if !strings.Contains(body, "TRK-100") || !strings.Contains(body, "VERIFY BY RADIO") || strings.Contains(body, "OVERDUE") {
		t.Errorf("the site page must keep its third-post wording (never OVERDUE) for a run this post is not on")
	}
}

// An end's RED acknowledgment replicates and proves the run was OVERDUE
// there; the run is handled (silenced at both ends), so it leaves the strip
// as at the posts, and the overview and search say who acknowledged it.
func TestFix14RedAckAtAnEndShowsAsAcknowledgedOverdue(t *testing.T) {
	srv, st, trip := fix14ThirdPost(t)
	fix14Later(st)
	if err := deadline.AcknowledgeKind(st, "site-b", 1, trip.TripID, deadline.AckKindRed, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	body := fix7Do(t, srv, "GET", fix7DispatchHost, "/analytics", nil).Body.String()
	if strings.Contains(fix14Strip(body), trip.TripID) {
		t.Errorf("an acknowledged run stays on the strip:\n%s", fix14Strip(body))
	}
	if !strings.Contains(body, `data-analytics-overdue-other="0"`) ||
		!strings.Contains(body, "acknowledged as OVERDUE</strong> at a post on the run: <code>TRK-100</code> (Pad Post)") {
		t.Errorf("the overview must name TRK-100 as acknowledged OVERDUE at Pad Post")
	}
	body = fix7Do(t, srv, "GET", fix7DispatchHost, "/search?truck=TRK-100", nil).Body.String()
	if !strings.Contains(body, "on the road — acknowledged as OVERDUE at Pad Post") {
		t.Errorf("dispatch search must say acknowledged as OVERDUE at Pad Post")
	}

	// An AMBER-only ack does not silence a red: still on the red line.
	srv2, st2, trip2 := fix14ThirdPost(t)
	fix14Later(st2)
	if err := deadline.AcknowledgeKind(st2, "site-b", 1, trip2.TripID, deadline.AckKindAmber, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if strip := fix14Strip(fix7Do(t, srv2, "GET", fix7DispatchHost, "/analytics", nil).Body.String()); !strings.Contains(strip, trip2.TripID) {
		t.Errorf("an amber-only ack must not take the run off the red line:\n%s", strip)
	}
}
