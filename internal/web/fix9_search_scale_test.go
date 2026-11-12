package web

// fix9_search_scale_test.go — FIX9's performance measurement, run on demand
// (FIX12 adds the Site term and the narrowing panel's queries):
//
//	FIX9_SCALE=270 go test ./internal/web -run TestFix9SearchScale -v
//
// Seeds N people over two posts — each checked in and out three times on a
// truck, and one run in five people's worth from one post to the other,
// arrived — then times representative searches through the served surface.
// Skipped unless FIX9_SCALE is set: it measures, it asserts only that every
// search answered 200.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
)

func TestFix9SearchScale(t *testing.T) {
	n, _ := strconv.Atoi(os.Getenv("FIX9_SCALE"))
	if n <= 0 {
		t.Skip("set FIX9_SCALE=<people> to measure")
	}
	st, setClock := anStore(t)
	began := time.Now()
	ms := anLocal(6, 0)
	tick := func() { ms += 7_000; setClock(ms) }
	tick()
	sites := []string{"site-a", "site-b"}
	var people []domain.Person
	for i := 0; i < n; i++ {
		site := sites[i%2]
		last, first, phone := fmt.Sprintf("Surname%04d", i), fmt.Sprintf("Given%04d", i), fmt.Sprintf("(907) 555-%04d", i)
		people = append(people, fix9Person(t, st, site, last, first, fmt.Sprintf("Company %02d", i%40), phone))
		tick()
	}
	for round := 0; round < 3; round++ {
		for i, p := range people {
			site := sites[i%2]
			anCheckIn(t, st, site, fmt.Sprintf("TRK-%03d", i%120), p.PersonID)
			tick()
		}
		for i := range people {
			open, err := domain.OpenVisitsAt(st, sites[i%2])
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range open {
				if v.PersonID == people[i].PersonID {
					if err := domain.CheckOutVisit(st, sites[i%2], 1, v.VisitID, nil, 0, "default"); err != nil {
						t.Fatal(err)
					}
				}
			}
			tick()
		}
	}
	dest, mins := "site-a", int64(90)
	runs := 0
	for i := 1; i+4 < len(people); i += 10 { // site-b people (odd indices)
		var occ []string
		for j := i + 2; j <= i+8 && j < len(people); j += 2 {
			occ = append(occ, people[j].PersonID)
		}
		trip, err := domain.Dispatch(st, "site-b", 1, domain.DispatchInput{
			VehicleID: w7SeedVehicleID(t, st, "site-b", fmt.Sprintf("RUN-%03d", i%60)), DriverPersonID: people[i].PersonID,
			OccupantPersonIDs: occ, DestinationSite: &dest, DeadlineMinutes: &mins,
		}, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		tick()
		if _, err := domain.RecordExpectedArrival(st, "site-a", 1, trip.TripID, nil, 0, "default"); err != nil {
			t.Fatal(err)
		}
		tick()
		runs++
	}
	cfg := &config.Config{
		SiteID: "site-b", DisplayName: "Ice Camp B", ListenAddr: ":0", NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 10,
		Peers:         []config.Peer{{SiteID: "site-a", BaseURL: "http://example.invalid"}},
		Destinations:  []config.Destination{{ID: "site-a", DisplayName: "Ice Pad", Controlled: true, TravelMinutes: 65}},
		LoginPassword: testLoginPassword,
	}
	srv := New(cfg, st)
	t.Logf("seeded %d people, %d visits, %d runs in %s", n, n*3, runs, time.Since(began).Round(time.Millisecond))
	cookie := loginCookie(t, srv, testLoginPassword)
	for _, q := range []url.Values{
		{"last": {"surname0042"}},
		{"q": {"given01"}},
		{"company": {"company 07"}},
		{"phone": {"5550042"}},
		{"truck": {"trk-042"}},
		{"truck": {"run 011"}},
		{"date_from": {"2026-09-29"}, "time_from": {"07:00"}, "time_to": {"07:30"}},
		// FIX12: the Site term, and the panel narrowing a broad search.
		{"site": {"site-a"}},
		{"company": {"company 0"}},
		{"company": {"company 0"}, "in_site": {"site-b"}, "in_type": {"visits", "trips"}},
		{"company": {"company 0"}, "in_status": {"visit-closed", "trip-arrived"}, "in_company": {"company 07"}},
		{"site": {"site-a"}, "in_hand": {"1"}},
	} {
		best := time.Hour
		var code int
		for i := 0; i < 3; i++ {
			req := httptest.NewRequest("GET", "/search?"+q.Encode(), nil)
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			s := time.Now()
			srv.Root().ServeHTTP(rec, req)
			if d := time.Since(s); d < best {
				best = d
			}
			code = rec.Code
		}
		if code != http.StatusOK {
			t.Errorf("%s = %d", q.Encode(), code)
		}
		t.Logf("search %-60s best of 3: %s", q.Encode(), best.Round(time.Millisecond))
	}
}
