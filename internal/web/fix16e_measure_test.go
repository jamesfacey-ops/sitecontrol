// fix16e_measure_test.go — FIX16 E: a season-sized log for timing the
// reports' replay, and for driving a scratch post in a browser. Skipped
// unless F16E_MEASURE=1; F16E_SEED_DB keeps the seeded store at that path.
package web

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

func TestFix16eMeasureSeasonReplay(t *testing.T) {
	if os.Getenv("F16E_MEASURE") != "1" {
		t.Skip("set F16E_MEASURE=1 to seed a season and time the reports")
	}
	path := os.Getenv("F16E_SEED_DB")
	if path == "" {
		path = filepath.Join(t.TempDir(), "season.db")
	}
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	var now int64
	st.Clock = func() store.ClockReading {
		return store.ClockReading{WallMS: now, MaxErrorUS: 500, State: store.ClockSynced}
	}
	days := 120
	if v := os.Getenv("F16E_DAYS"); v != "" {
		fmt.Sscan(v, &days)
	}
	end := time.Now().In(time.Local)
	start := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.Local).AddDate(0, 0, -days+1)
	now = start.UnixMilli()
	rng := rand.New(rand.NewSource(16))
	var cos []string
	for _, n := range []string{"Kakivik Asset Management", "Polar Haul", "North Slope Welding", "Tundra Camp Services", "Arctic Survey"} {
		c, err := domain.CreateCompany(st, "site-a", 1, n, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		cos = append(cos, c.CompanyID)
	}
	var people []string
	for i := 0; i < 150; i++ {
		co := cos[i%len(cos)]
		p, err := domain.CreatePerson(st, "site-a", 1, fmt.Sprintf("Worker %03d", i), &co, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		people = append(people, p.PersonID)
	}
	veh := w7SeedVehicleID(t, st, "site-a", "TRK-SEASON")
	began := time.Now()
	visits := 0
	for d := 0; d < days; d++ {
		day := start.AddDate(0, 0, d)
		perm := rng.Perm(len(people))[:40]
		type out struct {
			at     int64
			person string
		}
		var leaves []out
		for i, idx := range perm {
			now = day.Add(time.Duration(6*60+rng.Intn(10*60)) * time.Minute).UnixMilli()
			if now > end.UnixMilli() {
				break
			}
			trip, err := domain.DispatchLocalCheckIn(st, "site-a", 1, domain.DispatchInput{VehicleID: veh, DriverPersonID: people[idx]}, 0, "default")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := domain.RecordExpectedArrival(st, "site-a", 1, trip.TripID, nil, 0, "default"); err != nil {
				t.Fatal(err)
			}
			visits++
			if i%10 != 0 { // one in ten never signs out
				leaves = append(leaves, out{now + int64(30+rng.Intn(9*60))*60000, people[idx]})
			}
		}
		// Three runs a day to site-b, 60-minute allowance, 40-100 min on the road.
		for r := 0; r < 3 && len(perm) > r+1; r++ {
			now = day.Add(time.Duration(16*60+r*30) * time.Minute).UnixMilli()
			if now > end.UnixMilli() {
				break
			}
			open, err := domain.OpenVisitsAt(st, "site-a")
			if err != nil {
				t.Fatal(err)
			}
			if len(open) == 0 {
				break
			}
			dest, allow := "site-b", int64(60)
			trip, err := domain.Dispatch(st, "site-a", 1, domain.DispatchInput{VehicleID: veh, DriverPersonID: open[0].PersonID,
				DestinationSite: &dest, DeadlineMinutes: &allow}, 0, "default")
			if err != nil {
				t.Fatal(err)
			}
			now += int64(40+rng.Intn(60)) * 60000
			if now > end.UnixMilli() {
				continue
			}
			if _, err := domain.RecordExpectedArrival(st, "site-b", 1, trip.TripID, nil, 0, "default"); err != nil {
				t.Fatal(err)
			}
		}
		for _, l := range leaves {
			if l.at > end.UnixMilli() {
				continue
			}
			open, err := domain.OpenVisitsAt(st, "site-a")
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range open {
				if v.PersonID == l.person {
					now = l.at
					if err := domain.CheckOutVisit(st, "site-a", 1, v.VisitID, nil, 0, "default"); err != nil {
						t.Fatal(err)
					}
					break
				}
			}
		}
	}
	now = end.UnixMilli()
	t.Logf("seeded %d days, %d visits in %s", days, visits, time.Since(began).Round(time.Millisecond))

	timeIt := func(what string, f func() error) {
		b := time.Now()
		if err := f(); err != nil {
			t.Fatal(err)
		}
		t.Logf("%-44s %s", what, time.Since(b).Round(time.Millisecond))
	}
	var recs []domain.VisitRecord
	timeIt("VisitReport (the shared replay), first", func() (err error) { recs, _, err = domain.VisitReport(st, nil); return })
	timeIt("VisitReport (the shared replay), again", func() (err error) { recs, _, err = domain.VisitReport(st, nil); return })
	timeIt("BuildAnalytics (the /analytics page)", func() error { _, err := domain.BuildAnalytics(st, end, time.Local); return err })
	for _, n := range []int{1, 14, days} {
		from := end.AddDate(0, 0, -n).UnixMilli()
		timeIt(fmt.Sprintf("headcount sweep + hourly, %d days", n), func() error {
			spans, _ := domain.PresenceSpans(recs, "site-a")
			domain.NewHeadcount(spans).Hourly(from, end.UnixMilli())
			return nil
		})
	}
	timeIt("RunsPastAllowance", func() error { _, _, err := domain.RunsPastAllowance(st, end.UnixMilli()); return err })
	timeIt("ContractorHours, season", func() error {
		_, err := domain.ContractorHours(st, recs, "", start.UnixMilli(), end.UnixMilli())
		return err
	})

	cfg := &config.Config{SiteID: "site-a", DisplayName: "Ice Road", ListenAddr: ":0", NodeEpoch: 1, DBPath: st.Path,
		SyncIntervalSeconds: 10, LoginPassword: testLoginPassword, SiteType: domain.SiteTypeTextMessage,
		Peers: []config.Peer{{SiteID: "site-b", BaseURL: "http://example.invalid"}}}
	srv := New(cfg, st)
	today := end.Format("2006-01-02")
	for _, p := range []string{"/analytics", "/analytics/reports/daily?site=site-a&date=" + today,
		"/analytics/reports/hours?from=" + start.Format("2006-01-02") + "&to=" + today,
		"/analytics/reports/weekly?week=" + today,
		"/analytics/reports/headcount?site=site-a&from=" + end.AddDate(0, 0, -13).Format("2006-01-02") + "&to=" + today} {
		b := time.Now()
		code, body := f16eGet(t, srv, "", p, true)
		t.Logf("GET %-60.60s %d %6d bytes %s", p, code, len(body), time.Since(b).Round(time.Millisecond))
	}
}
