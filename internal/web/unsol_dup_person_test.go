// unsol_dup_person_test.go — the REAL HTTP unsolicited-arrival door
// (POST /trips/arrive-unsolicited) and a DUPLICATED person id
// (reports/witness/UNSOL-DUP-PERSON.md; the domain-level witness is
// internal/domain/unsol_dup_person_test.go).
//
// Two things are proven through the handler rather than the domain door:
//  1. the duplicate no longer mints two visits when it arrives as a form
//     field repeated (`person_ids=p&person_ids=p`), which is exactly what a
//     checkbox list submits when two boxes carry one value;
//  2. the duplicate IS reachable from the page, not only from a hand-built
//     payload: arrivalSections lists a person under EVERY peer the record
//     says they are open at (it marks `seen` but never consults it inside
//     the peer loop), so one person open at two peers is two tick-boxes with
//     one value. The third test collects every box the rendered page offers
//     for one person, ticks them all, and requires one visit — a guard that
//     keeps holding if the picker is later de-duplicated too.
package web

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// newUnsolDupWorld is site-b's box with TWO peers (site-a, site-c) — the
// second peer is what lets one person be "recorded on site at" two places
// on the arrival picker — and two people on the roster.
func newUnsolDupWorld(t *testing.T) (*Server, *store.Store, domain.Person, domain.Person) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "unsoldup.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	dana, err := domain.CreatePerson(st, "site-b", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	pat, err := domain.CreatePerson(st, "site-b", 1, "Pat Passenger", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		SiteID: "site-b", DisplayName: "Ice Camp B", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 10,
		Peers: []config.Peer{
			{SiteID: "site-a", BaseURL: "http://example.invalid"},
			{SiteID: "site-c", BaseURL: "http://example.invalid"},
		},
		LoginPassword: testLoginPassword,
	}
	return New(cfg, st), st, dana, pat
}

// unsolDupVisitRows counts every visit row — open and closed — for personID
// at siteID in the fold's whole table. OpenVisitsAt would hide the
// closed-at-birth row the defect makes.
func unsolDupVisitRows(t *testing.T, st *store.Store, siteID, personID string) (rows, open int) {
	t.Helper()
	all, err := domain.Visits(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range all {
		if v.SiteID != siteID || v.PersonID != personID {
			continue
		}
		rows++
		if v.IsOpen == 1 {
			open++
		}
	}
	return rows, open
}

// unsolDupOpenAtPeer records personID as currently on site at peer, the way
// the record would read after that peer's own arrival replicated here.
func unsolDupOpenAtPeer(t *testing.T, st *store.Store, peer, personID string) {
	t.Helper()
	n := "TRK " + peer
	v, err := domain.CreateVehicle(st, peer, 1, &n, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.RecordUnsolicitedArrival(st, peer, 1, v.VehicleID, "site-b",
		[]string{personID}, nil, 0, "default"); err != nil {
		t.Fatalf("recording %s on site at %s: %v", personID, peer, err)
	}
}

func TestUnsolDupPersonPostedTwiceMintsOneVisit(t *testing.T) {
	srv, st, dana, _ := newUnsolDupWorld(t)
	rec := postForm(t, srv, "/trips/arrive-unsolicited", url.Values{
		"time_source": {"default"},
		"vehicle":     {"TRK 12"},
		"origin_site": {"site-a"},
		"person_ids":  {dana.PersonID, dana.PersonID},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /trips/arrive-unsolicited with a repeated person_ids = %d: %s", rec.Code, rec.Body.String())
	}
	rows, open := unsolDupVisitRows(t, st, "site-b", dana.PersonID)
	t.Logf("MEASURED: person_ids=[p, p] through the HTTP door left %d visit row(s), %d open", rows, open)
	if open != 1 || rows != 1 {
		t.Fatalf("one person posted twice: %d visit row(s), %d open — want exactly 1 and 1", rows, open)
	}
}

func TestUnsolDupPersonTwoDifferentPeopleBothLand(t *testing.T) {
	srv, st, dana, pat := newUnsolDupWorld(t)
	rec := postForm(t, srv, "/trips/arrive-unsolicited", url.Values{
		"time_source": {"default"},
		"vehicle":     {"TRK 12"},
		"origin_site": {"site-a"},
		"person_ids":  {dana.PersonID, pat.PersonID},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /trips/arrive-unsolicited = %d: %s", rec.Code, rec.Body.String())
	}
	for _, p := range []domain.Person{dana, pat} {
		if rows, open := unsolDupVisitRows(t, st, "site-b", p.PersonID); rows != 1 || open != 1 {
			t.Fatalf("%s: %d row(s), %d open — a genuine two-person arrival must land both", p.Name, rows, open)
		}
	}
}

// TestUnsolDupPersonEveryBoxThePageOffersForOnePersonYieldsOneVisit — the
// reachability half. Dana is open at BOTH peers; the page is rendered and
// every person_ids box carrying Dana's id is ticked, exactly as an officer
// who ticks the name wherever it appears would submit it.
func TestUnsolDupPersonEveryBoxThePageOffersForOnePersonYieldsOneVisit(t *testing.T) {
	srv, st, dana, _ := newUnsolDupWorld(t)
	unsolDupOpenAtPeer(t, st, "site-a", dana.PersonID)
	unsolDupOpenAtPeer(t, st, "site-c", dana.PersonID)

	body := getTrips(t, srv)
	box := `name="person_ids" value="` + dana.PersonID + `"`
	n := strings.Count(body, box)
	t.Logf("MEASURED: the rendered arrival picker offers %d tick-box(es) for one person open at two peers", n)
	if n < 1 {
		t.Fatalf("the arrival picker offers no box at all for %s — ordering-not-filtering says nobody is dropped", dana.PersonID)
	}
	ticked := make([]string, n)
	for i := range ticked {
		ticked[i] = dana.PersonID
	}
	rec := postForm(t, srv, "/trips/arrive-unsolicited", url.Values{
		"time_source": {"default"},
		"vehicle":     {"TRK 12"},
		"origin_site": {"site-a"},
		"person_ids":  ticked,
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST with every offered box ticked = %d: %s", rec.Code, rec.Body.String())
	}
	rows, open := unsolDupVisitRows(t, st, "site-b", dana.PersonID)
	if rows != 1 || open != 1 {
		t.Fatalf("ticking all %d box(es) the page offers for one person left %d visit row(s), %d open — want exactly 1 and 1", n, rows, open)
	}
}
