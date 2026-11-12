// dispatch_scope_test.go — D-77's dispatch scope at the REQUEST path.
//
// config.go's D-77 comment: peers say who this post REPLICATES with;
// destinations say who it can DISPATCH to. The dispatch forms, trip-amend
// and that comment all agreed; the raw-POST path did not
// (reports/witness/R3-LINKAGE.md, 2026-09-10): POST /trips/dispatch carrying
// the legacy destination_site field named a PEER that is not in
// [[destinations]] and was ACCEPTED — the F4 membership check was handed
// config.Peers, not the controlled destination list, and the legacy field
// never consulted DestinationList at all. That is exactly the
// deadline-toward-a-road-this-post-cannot-drive that D-77 refuses at
// startup, reopened one door down.
//
// The world is three posts: site-e replicates with site-d AND site-f and
// dispatches only to site-f. site-d is the case — a peer, not a destination.
// The two lists are deliberately DIFFERENT here; every other web fixture has
// one peer, where they coincide and this defect is invisible.
package web

import (
	"database/sql"
	"net/http"
	"net/url"
	"path/filepath"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

func newDispatchScopeWorld(t *testing.T) (*Server, *store.Store, []domain.Person) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "scope.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	var persons []domain.Person
	for _, name := range []string{"Xena Cross", "Uma Unsol"} {
		p, err := domain.CreatePerson(st, "site-e", 1, name, nil, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		persons = append(persons, p)
	}
	o := false
	cfg := &config.Config{
		SiteID: "site-e", DisplayName: "South Ice Road", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 3600,
		LoginPassword: testLoginPassword,
		Peers: []config.Peer{
			{SiteID: "site-d", BaseURL: "http://example.invalid"},
			{SiteID: "site-f", BaseURL: "http://example.invalid"},
		},
		Destinations: []config.Destination{{ID: "site-f", DisplayName: "South Pad", Controlled: true}},
		Orientation:  &o,
	}
	return New(cfg, st), st, persons
}

// TestDispatchRefusesAPeerThatIsNotAControlledDestination drives the real
// HTTP door with both ways of naming a destination. The first arm is the
// witness: it was ACCEPTED (303, trip authored, queued at site-d's inbound)
// before the fix. The other three are controls — the button path was
// already closed by applyDispatchDestination's DestinationList lookup, and
// the configured pair must keep dispatching through BOTH fields, or the
// fix has over-tightened.
func TestDispatchRefusesAPeerThatIsNotAControlledDestination(t *testing.T) {
	cases := []struct {
		name  string
		field string
		dest  string
		want  int
	}{
		{"legacy destination_site names a peer that is not a destination", "destination_site", "site-d", http.StatusConflict},
		{"destination_id names a peer that is not a destination (control: button path)", "destination_id", "site-d", http.StatusConflict},
		{"legacy destination_site names the configured destination (control: the pair)", "destination_site", "site-f", http.StatusSeeOther},
		{"destination_id names the configured destination (control: the pair)", "destination_id", "site-f", http.StatusSeeOther},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			srv, st, persons := newDispatchScopeWorld(t)
			// Xena is ON SITE at e before she drives out — the console's own
			// happy path (trips_test.go) checks in first, so an accepted arm
			// here is accepted for the same reason it is there. The check-in
			// authors §10's degenerate trip (no destination); the assertions
			// below count only trips that carry one.
			if rec := postForm(t, srv, "/gate/checkin", url.Values{
				"time_source": {"default"}, "vehicle": {"SOUTH-2"},
				"driver_kind": {"existing"}, "driver_value": {persons[0].PersonID},
			}); rec.Code != http.StatusSeeOther {
				t.Fatalf("gate check-in = %d: %s", rec.Code, rec.Body.String())
			}
			form := url.Values{
				"time_source":      {"default"},
				"vehicle":          {"SOUTH-2"},
				"driver_person_id": {persons[0].PersonID},
				"deadline_minutes": {"60"},
				tc.field:           {tc.dest},
			}
			before, err := st.Watermark("site-e", 1)
			if err != nil {
				t.Fatal(err)
			}
			rec := postForm(t, srv, "/trips/dispatch", form)
			if rec.Code != tc.want {
				t.Fatalf("POST /trips/dispatch %s=%s = %d, want %d: %s", tc.field, tc.dest, rec.Code, tc.want, rec.Body.String())
			}
			all, err := domain.Trips(st)
			if err != nil {
				t.Fatal(err)
			}
			var away []domain.Trip
			for _, tr := range all {
				if tr.DestinationSite != nil {
					away = append(away, tr)
				}
			}
			after, err := st.Watermark("site-e", 1)
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == http.StatusConflict {
				if len(away) != 0 || after != before {
					t.Fatalf("refused with %d but authored %d trip(s), %d event(s): a dispatch to %q — a PEER this post replicates with but has no configured road to — would arm an in-transit deadline nobody chose and queue a vehicle on an inbound list nobody is pairing with (D-77 at the request path)", rec.Code, len(away), after-before, tc.dest)
				}
				return
			}
			if len(away) != 1 || *away[0].DestinationSite != tc.dest {
				t.Fatalf("accepted dispatch to %q authored %+v, want exactly one trip with DestinationSite=%q", tc.dest, away, tc.dest)
			}
		})
	}
}

// TestUnsolicitedArrivalFromAPeerThatIsNotAControlledDestinationIsStillAccepted
// is the guard against fixing the wrong thing: an unsolicited arrival's
// origin is peer-wide BY DESIGN — a vehicle that outran the sync can come
// from any post this one replicates with, not only from one it dispatches
// to. Narrowing "who may I dispatch to" must leave "who may an arrival come
// from" alone. site-d is a peer of site-e and not one of its destinations.
func TestUnsolicitedArrivalFromAPeerThatIsNotAControlledDestinationIsStillAccepted(t *testing.T) {
	srv, st, persons := newDispatchScopeWorld(t)
	form := url.Values{
		"time_source": {"default"},
		"vehicle":     {"CROSS-9"},
		"origin_site": {"site-d"},
		"person_ids":  {persons[1].PersonID},
	}
	rec := postForm(t, srv, "/trips/arrive-unsolicited", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /trips/arrive-unsolicited origin_site=site-d = %d, want 303 — an arrival FROM a peer that is not a dispatch destination must still be recordable: %s", rec.Code, rec.Body.String())
	}
	db, err := sql.Open("sqlite", st.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM trip_arrival WHERE trip_ref IS NULL AND origin_site = ?`, "site-d").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("unsolicited trip_arrival rows with origin_site=site-d = %d, want 1", n)
	}
}
