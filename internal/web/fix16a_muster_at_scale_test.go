package web

// fix16a_muster_at_scale_test.go — FIX16a's speed measurement for a
// season-long replay, run on demand:
//
//	FIX16A_SCALE=700 FIX16A_ROUNDS=20 go test ./internal/web -run TestFix16aMusterAtScale -v
//
// Seeds N people over two posts; each ROUND checks everyone in, sends one
// person in ten from site-b to site-a on a run (arrived), and checks the
// rest out — N×ROUNDS visits. Then times /muster/at early, mid and late in
// the season and /last-seen through the served surface. Skipped unless
// FIX16A_SCALE is set: it measures, and asserts only 200s.

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

func TestFix16aMusterAtScale(t *testing.T) {
	n, _ := strconv.Atoi(os.Getenv("FIX16A_SCALE"))
	if n <= 0 {
		t.Skip("set FIX16A_SCALE=<people> (and FIX16A_ROUNDS) to measure")
	}
	rounds, _ := strconv.Atoi(os.Getenv("FIX16A_ROUNDS"))
	if rounds <= 0 {
		rounds = 3
	}
	st, setClock := anStore(t)
	began := time.Now()
	ms := time.Date(2026, 9, 1, 6, 0, 0, 0, time.Local).UnixMilli()
	first := ms
	tick := func() { ms += 7_000; setClock(ms) }
	tick()
	sites := []string{"site-a", "site-b"}
	var people []domain.Person
	vehicles := map[string]string{}
	vehicle := func(site, no string) string {
		if id, ok := vehicles[site+no]; ok {
			return id
		}
		id := w7SeedVehicleID(t, st, site, no)
		vehicles[site+no] = id
		return id
	}
	for i := 0; i < n; i++ {
		people = append(people, fix9Person(t, st, sites[i%2], fmt.Sprintf("Surname%04d", i), fmt.Sprintf("Given%04d", i),
			fmt.Sprintf("Company %02d", i%40), fmt.Sprintf("(907) 555-%04d", i)))
		tick()
	}
	dest, mins := "site-a", int64(90)
	visits, runs := 0, 0
	for round := 0; round < rounds; round++ {
		for i, p := range people {
			trip, err := domain.DispatchLocalCheckIn(st, sites[i%2], 1, domain.DispatchInput{
				VehicleID: vehicle(sites[i%2], fmt.Sprintf("TRK-%03d", i%120)), DriverPersonID: p.PersonID}, 0, "default")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := domain.RecordExpectedArrival(st, sites[i%2], 1, trip.TripID, nil, 0, "default"); err != nil {
				t.Fatal(err)
			}
			visits++
			tick()
		}
		openB, err := domain.OpenVisitsAt(st, "site-b")
		if err != nil {
			t.Fatal(err)
		}
		for i, v := range openB {
			if i%10 == 0 {
				trip, err := domain.Dispatch(st, "site-b", 1, domain.DispatchInput{
					VehicleID: vehicle("site-b", fmt.Sprintf("RUN-%03d", i%60)), DriverPersonID: v.PersonID,
					DestinationSite: &dest, DeadlineMinutes: &mins}, 0, "default")
				if err != nil {
					t.Fatal(err)
				}
				tick()
				if _, err := domain.RecordExpectedArrival(st, "site-a", 1, trip.TripID, nil, 0, "default"); err != nil {
					t.Fatal(err)
				}
				visits++
				runs++
			} else if err := domain.CheckOutVisit(st, "site-b", 1, v.VisitID, nil, 0, "default"); err != nil {
				t.Fatal(err)
			}
			tick()
		}
		openA, err := domain.OpenVisitsAt(st, "site-a")
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range openA {
			if err := domain.CheckOutVisit(st, "site-a", 1, v.VisitID, nil, 0, "default"); err != nil {
				t.Fatal(err)
			}
			tick()
		}
		ms += 6 * time.Hour.Milliseconds()
	}
	last := ms
	cfg := &config.Config{
		SiteID: "site-b", DisplayName: "Ice Camp B", ListenAddr: ":0", NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 10,
		Peers:         []config.Peer{{SiteID: "site-a", BaseURL: "http://example.invalid"}},
		Destinations:  []config.Destination{{ID: "site-a", DisplayName: "Ice Pad", Controlled: true, TravelMinutes: 65}},
		LoginPassword: testLoginPassword,
	}
	srv := New(cfg, st)
	t.Logf("seeded %d people, %d visits, %d runs over %d rounds in %s", n, visits, runs, rounds, time.Since(began).Round(time.Millisecond))
	cookie := loginCookie(t, srv, testLoginPassword)
	at := func(ms int64) url.Values {
		tm := time.UnixMilli(ms)
		return url.Values{"site": {"site-b"}, "date": {tm.Format("2006-01-02")}, "time": {tm.Format("15:04")}}
	}
	for _, c := range []struct {
		name, path string
		q          url.Values
	}{
		{"muster at, season start", "/muster/at", at(first + (last-first)/50)},
		{"muster at, mid season", "/muster/at", at(first + (last-first)/2)},
		{"muster at, season end", "/muster/at", at(last)},
		{"last seen", "/last-seen", url.Values{"person": {people[n/2].PersonID}}},
	} {
		best := time.Hour
		var code int
		for i := 0; i < 3; i++ {
			req := httptest.NewRequest("GET", c.path+"?"+c.q.Encode(), nil)
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
			t.Fatalf("%s = %d", c.name, code)
		}
		t.Logf("%-26s best of 3: %s", c.name, best.Round(time.Millisecond))
	}
}
