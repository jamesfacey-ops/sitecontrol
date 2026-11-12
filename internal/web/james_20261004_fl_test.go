package web

// Stream FL (fix round 2, 2026-10-04): the leftover MEDIUM/LOW copy and
// small-logic items of FINDINGS-R4 not owned by clusters A-K. Each test
// fails on 2ba9188 and passes after.

import (
	"html"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
)

// D24 / R12: a post with destinations=[] (NPF shape) refuses an uncontrolled
// pad that is not in its known pad list, in the M-4 wording.
func TestFLUncontrolledDestinationFreeTextIsCheckedAgainstTheKnownPadList(t *testing.T) {
	st := w3OpenRegistered(t, "fl-d24.db")
	toml := `
site_id               = "npf"
display_name          = "NPF"
listen_addr           = "127.0.0.1:0"
node_epoch            = 1
db_path               = "` + st.Path + `"
sync_interval_seconds = 10
login_password        = "` + testLoginPassword + `"
orientation           = false
site_type             = "text_message"
destinations          = []

[[peers]]
site_id  = "site-a"
base_url = "http://127.0.0.1:0"
key      = "k"

[[peers]]
site_id  = "site-b"
base_url = "http://127.0.0.1:0"
key      = "k"
`
	path := filepath.Join(t.TempDir(), "npf.toml")
	if err := os.WriteFile(path, []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(cfg, st)
	w9aSeedPerson(t, st, "site-r", 1, "p-fl-d24", "Harmere, Logan")
	trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRK-11", "p-fl-d24", nil, 65)
	if _, err := domain.RecordExpectedArrival(st, "site-b", 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	wm := w2Watermark(t, st, "npf", 1)
	rec, _ := w2PostForm(t, srv, "/trips/amend", url.Values{
		"trip_id": {trip.TripID}, "destination_kind": {"uncontrolled"}, "destination_value": {"banana"},
	})
	body := html.UnescapeString(rec.Body.String())
	if rec.Code != http.StatusConflict || !strings.Contains(body, `"banana" is not a pad this post is configured for`) {
		t.Fatalf("D24: uncontrolled \"banana\" = %d, want 409 with the M-4 wording; page text:\n%s", rec.Code, w3PageText(body))
	}
	if got := w2Watermark(t, st, "npf", 1); got != wm {
		t.Fatalf("D24: refused but authored %d event(s)", got-wm)
	}
}

// D24 (LOW): /arrivals/amend refuses an origin that is not a peer.
func TestFLArrivalAmendRefusesAnOriginThatIsNotAPeer(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	_, own := d87ForeignArrival(t, st, persons)
	wm := w2Watermark(t, st, "site-a", 1)
	rec := postForm(t, srv, "/arrivals/amend", url.Values{"arrival_id": {own}, "origin_site": {"banana"}})
	body := html.UnescapeString(rec.Body.String())
	if rec.Code != http.StatusConflict || !strings.Contains(body, `"banana" is not a post`) {
		t.Fatalf("D24: origin_site=banana = %d; page text:\n%s", rec.Code, w3PageText(body))
	}
	if got := w2Watermark(t, st, "site-a", 1); got != wm {
		t.Fatalf("D24: refused but authored %d event(s)", got-wm)
	}
}

// D25: at a peer, the review row says where to resolve it and carries no
// controls; at the owner it still says "resolve it here".
func TestFLPeerReviewRowSaysAwaitingTheOwningPost(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	co, err := domain.CreateCompany(st, "site-b", 1, "Nanuq Drilling Services", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	var people [3]domain.Person
	for i, name := range []string{"Faraway, Fred", "Faraway, Frances", "Faraway, Finn"} {
		if people[i], err = domain.CreatePerson(st, "site-b", 1, name, &co.CompanyID, nil, 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	foreign, _, _ := c1cParkAt(t, st, "site-b", "TRK-B", [2]domain.Person{people[0], people[1]}, people[2])
	text := html.UnescapeString(w3PageText(c1cGet(t, srv, "/trips")))
	if strings.Contains(text, "resolve it here") {
		t.Errorf("D25: a peer's row says \"resolve it here\" with no control to do it with:\n%s", text)
	}
	if !strings.Contains(text, "awaiting") || !strings.Contains(text, "(resolve there)") || !strings.Contains(text, foreign) {
		t.Errorf("D25: the peer row does not say awaiting <post> (resolve there):\n%s", text)
	}

	srv2, _, _, _, _ := c1cOwnParked(t)
	own := html.UnescapeString(w3PageText(c1cGet(t, srv2, "/trips")))
	if !strings.Contains(own, "resolve it here") || strings.Contains(own, "(resolve there)") {
		t.Errorf("D25: the owning post's row lost \"resolve it here\":\n%s", own)
	}
}

// W-17 / W-26 / ROAD-NOTE / PRINT-COPY / unref footer / 405 / orientation
// copy: strings that were false, gone from the served pages.
func TestFLFalseCopyIsGone(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	_ = st
	// W-17: no "Travel takes longer" on any template.
	entries, _ := filepath.Glob("templates/*.html")
	for _, f := range entries {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := templateComment.ReplaceAllString(string(b), "")
		for _, bad := range []string{"Travel takes longer", "your reason and your name", "Either post may withdraw", "The arrival clock for these runs is kept at the destination post", "the printed sheet carries this count", "The printed sheet carries their number", "recording the orientation on its own enters nobody"} {
			if strings.Contains(src, bad) {
				t.Errorf("%s still says %q", f, bad)
			}
		}
	}
	// 405: the read-only site page's refusal does not promise a form.
	rec := postForm(t, srv, "/analytics/site/site-a", url.Values{})
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST site page = %d, want 405", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "returns to the form") {
		t.Errorf("405 page promises a form: %s", w3PageText(rec.Body.String()))
	}
	// W-26: the domain refusal says whose name is recorded: the post's.
	if _, err := os.Stat("../domain/tripauthor.go"); err == nil {
		b, _ := os.ReadFile("../domain/tripauthor.go")
		if strings.Contains(string(b), "with your reason and name") {
			t.Errorf("W-26: the phase refusal still says the officer's name is logged")
		}
	}
}

var templateComment = regexp.MustCompile(`(?s)\{\{/\*.*?\*/\}\}`)

// L02: the one-person page has no "Passenger 1" and no "list below".
func TestFLSinglePageOverlongNameRefusalHasNoPassengerOrList(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	vals := singleCheckInForm(t, srv, strings.Repeat("x", 130))
	vals.Set("new_company_id", newRef)
	vals.Set("new_company", "FL Test Co")
	rec := postForm(t, srv, "/gate/checkin/single", vals)
	body := html.UnescapeString(w3PageText(rec.Body.String()))
	if rec.Code != http.StatusConflict || !strings.Contains(body, "longer than any real name") {
		t.Fatalf("overlong single name = %d:\n%s", rec.Code, body)
	}
	if strings.Contains(body, "passenger 1") || strings.Contains(body, "the list below") {
		t.Errorf("single page refusal still talks of a passenger / list:\n%s", body)
	}
}

// L01 F-8: /orientation and /arrivals/resolve carry the overdue banner.
func TestFLOrientationAndResolvePagesCarryTheOverdueBanner(t *testing.T) {
	srv, st, ids := newOrientationServer(t, true)
	_ = ids
	if err := deadline.Register(st); err != nil {
		t.Fatal(err)
	}
	d, err := deadline.New(st, "site-b", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	srv.Alerts = d
	dispatchLiveOverdueTrip(t, st, d, "FL OVERDUE TRUCK")
	for _, path := range []string{"/orientation", "/arrivals/resolve?recorded=x.1.1"} {
		rec := c1cGet(t, srv, path)
		// ⟦was: "VEHICLE OVERDUE" — Q-FH1 (James, 2026-10-04): alert rows lead with the vehicle id and direction⟧
		// ⟦was: alertRowFlat(rec) "inbound — OVERDUE" — the strip's one full
		// row; FIX13 (James, 2026-10-05): the strip is the compact list⟧
		if !strings.Contains(fix13Lines(t, rec)["overdue"], "<code>FL OVERDUE TRUCK</code> (") {
			t.Errorf("GET %s carries no overdue banner", path)
		}
	}
}
