package web

// fix6_f15_off_trip_test.go — F15 (James, 2026-10-05): "If someone aboard a
// truck turns out never to have left and checks back in at the origin gate,
// they come off that trip EVERYWHERE." The web half: every surface at BOTH
// posts shows the reduced count, and the counts agree with the muster.
//
// FAIL BEFORE: the origin's outbound /trips row and its alert strip read
// "2"/"2 aboard" while its own muster listed one person on the road; at the
// destination (after replication) the inbound row, the strip, the arrival
// form's stayed ticks and the analytics inbound run all still named Pat.

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// f15CheckInAt gives personID an open visit at siteID — a gate walk-in, as
// the gate's own check-in records it (a degenerate local trip + arrival).
func f15CheckInAt(t *testing.T, st *store.Store, siteID, personID string) {
	t.Helper()
	walk, err := domain.DispatchLocalCheckIn(st, siteID, 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, st, siteID, "gate walk-in"), DriverPersonID: personID,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.RecordExpectedArrival(st, siteID, 1, walk.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
}

// f15Ship applies every event of siteID's stream dst has not yet seen.
func f15Ship(t *testing.T, src, dst *store.Store, siteID string) {
	t.Helper()
	wm, err := dst.Watermark(siteID, 1)
	if err != nil {
		t.Fatal(err)
	}
	envs, err := src.Events(siteID, 1, wm+1, 10000)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range envs {
		if _, err := dst.Apply(e); err != nil {
			t.Fatalf("applying %v: %v", e.Key(), err)
		}
	}
}

var f15OccupantsDD = regexp.MustCompile(`<dt>Occupants</dt><dd>(\d+)</dd>`)

func f15BoardCount(t *testing.T, section, vehicle string) string {
	t.Helper()
	i := strings.Index(section, vehicle)
	if i < 0 {
		t.Fatalf("%s is not on the board:\n%s", vehicle, section)
	}
	m := f15OccupantsDD.FindStringSubmatch(section[i:])
	if m == nil {
		t.Fatalf("no Occupants count after %s:\n%s", vehicle, section[i:])
	}
	return m[1]
}

// TestF15OriginSurfacesAgreeAfterAnOccupantChecksBackIn — the sending post:
// Pat rides out with Dana, turns back and checks in at this gate. The
// outbound row, the alert's "N aboard", the muster and the analytics
// outbound run all read Dana alone; the run (and its red) still stands.
func TestF15OriginSurfacesAgreeAfterAnOccupantChecksBackIn(t *testing.T) {
	srv, st, d, persons := newOriginServer(t)
	dana, pat := persons[0], persons[1]
	dest, mins := "site-a", int64(1)
	trip, err := domain.Dispatch(st, "site-b", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, st, "site-b", "TRK-F15"), DriverPersonID: dana.PersonID,
		OccupantPersonIDs: []string{pat.PersonID}, DestinationSite: &dest, DeadlineMinutes: &mins,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	envs, err := st.Events("site-b", 1, trip.Seq, 1)
	if err != nil || len(envs) != 1 {
		t.Fatalf("fetch envelope: %v", err)
	}
	d.WallNow = func() time.Time { return time.UnixMilli(envs[0].AuthoredAt.WallMS).Add(2 * time.Minute) }

	if got := f15BoardCount(t, boardSection(t, getAs(t, srv, "/trips"), "Outbound — on the road from"), "TRK-F15"); got != "2" {
		t.Fatalf("PRECONDITION: outbound row reads %s aboard, want 2", got)
	}
	f15CheckInAt(t, st, "site-b", pat.PersonID)

	body := getAs(t, srv, "/trips")
	if got := f15BoardCount(t, boardSection(t, body, "Outbound — on the road from"), "TRK-F15"); got != "1" {
		t.Errorf("outbound /trips row reads %s aboard after Pat checked back in, want 1", got)
	}
	for _, path := range []string{"/trips", "/muster", "/analytics"} {
		bar := alertRowFlat(topBarOf(t, getAs(t, srv, path)))
		if !strings.Contains(bar, "TRK-F15") {
			t.Fatalf("GET %s: the run's red left the strip — it is still a vehicle trip with a deadline:\n%s", path, bar)
		}
		// ⟦was: "1 aboard" / "2 aboard" in the strip's full row — FIX13
		// (James, 2026-10-05): the strip item is "TRK-F15 (1)"⟧
		if !strings.Contains(bar, "<code>TRK-F15</code> (1)") || strings.Contains(bar, "<code>TRK-F15</code> (2)") {
			t.Errorf("GET %s: strip must say TRK-F15 (1):\n%s", path, bar)
		}
	}
	// The analytics outbound run names Dana only.
	site := getAs(t, srv, "/analytics/site/site-b")
	run := site[strings.Index(site, `data-an-runs="outbound"`):]
	run = run[:strings.Index(run, "</article>")]
	if !strings.Contains(run, "Dana Driver") || strings.Contains(run, "Pat Passenger") {
		t.Errorf("analytics outbound run must name Dana and not Pat:\n%s", run)
	}
}

// TestF15DestinationSurfacesAgreeOnceTheCheckInReplicates — the receiving
// post: until Pat's origin check-in replicates it still expects two (the
// accepted window); once it does, the inbound row, the strip, the arrival
// form and the analytics inbound run all read Dana alone, and the arrival
// enters only Dana.
func TestF15DestinationSurfacesAgreeOnceTheCheckInReplicates(t *testing.T) {
	srv, st, persons := newTripsTestServer(t) // site-b: the DESTINATION here
	if err := deadline.Register(st); err != nil {
		t.Fatal(err)
	}
	d, err := deadline.New(st, "site-b", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	srv.Alerts = d
	dana, pat := persons[0], persons[1]

	origin, err := store.Open(t.TempDir() + "/origin.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { origin.Close() })
	if err := domain.Register(origin); err != nil {
		t.Fatal(err)
	}
	f15Ship(t, st, origin, "site-b") // the roster
	dest, mins := "site-b", int64(1)
	trip, err := domain.Dispatch(origin, "site-a", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, origin, "site-a", "TRK-F15"), DriverPersonID: dana.PersonID,
		OccupantPersonIDs: []string{pat.PersonID}, DestinationSite: &dest, DeadlineMinutes: &mins,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	f15Ship(t, origin, st, "site-a")
	envs, err := origin.Events("site-a", 1, trip.Seq, 1)
	if err != nil || len(envs) != 1 {
		t.Fatalf("fetch envelope: %v", err)
	}
	d.WallNow = func() time.Time { return time.UnixMilli(envs[0].AuthoredAt.WallMS).Add(30 * time.Minute) }

	f15CheckInAt(t, origin, "site-a", pat.PersonID)
	if got := f15BoardCount(t, boardSection(t, getAs(t, srv, "/trips"), "Inbound — open expectations"), "TRK-F15"); got != "2" {
		t.Fatalf("PRECONDITION: before replication the destination expects %s, want 2 (the accepted window)", got)
	}
	f15Ship(t, origin, st, "site-a")

	body := getAs(t, srv, "/trips")
	in := boardSection(t, body, "Inbound — open expectations")
	if got := f15BoardCount(t, in, "TRK-F15"); got != "1" {
		t.Errorf("inbound row reads %s after Pat checked back in at the origin, want 1", got)
	}
	if strings.Contains(in, pat.PersonID) || strings.Contains(in, "Pat Passenger") {
		t.Errorf("the arrival form still offers Pat:\n%s", in)
	}
	bar := alertRowFlat(topBarOf(t, body))
	if !strings.Contains(bar, "TRK-F15") {
		t.Fatalf("the run's red must still stand on the destination strip:\n%s", bar)
	}
	// ⟦was: "1 aboard" / "2 aboard" — FIX13: the strip item is "TRK-F15 (1)"⟧
	if !strings.Contains(bar, "<code>TRK-F15</code> (1)") || strings.Contains(bar, "<code>TRK-F15</code> (2)") {
		t.Errorf("destination strip must say TRK-F15 (1):\n%s", bar)
	}
	site := getAs(t, srv, "/analytics/site/site-b")
	run := site[strings.Index(site, `data-an-runs="inbound"`):]
	run = run[:strings.Index(run, "</article>")]
	if !strings.Contains(run, "Dana Driver") || strings.Contains(run, "Pat Passenger") {
		t.Errorf("analytics inbound run must name Dana and not Pat:\n%s", run)
	}

	if _, err := domain.RecordExpectedArrivalExcluding(st, "site-b", 1, trip.TripID, nil, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	open, err := domain.OpenVisitsAt(st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range open {
		if v.PersonID == pat.PersonID {
			t.Fatal("Pat, back at the origin, was entered at the destination by the arrival")
		}
	}
}
