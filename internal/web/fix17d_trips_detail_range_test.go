package web

// fix17d_trips_detail_range_test.go — FIX17d (James 2026-10-07): /trips/detail
// lists the last seven days by default, a chosen date range (GET form, no
// script), or everything on request, and offers the listed trips as a CSV.
// A trip belongs to the day of its effective (typed) departure.
//
// Fail-before: on 0f7a604 the page lists every trip of the season, has no
// range form and /trips/detail.csv is not a route.

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

var fix17dRowRe = regexp.MustCompile(`(?s)<tr id="trip-.*?</tr>`)

func fix17dDay(daysAgo int) time.Time {
	n := time.Now().In(time.Local)
	return time.Date(n.Year(), n.Month(), n.Day(), 12, 0, 0, 0, time.Local).AddDate(0, 0, -daysAgo)
}

// fix17dTrip dispatches one run, typed out at typed and recorded now.
func fix17dTrip(t *testing.T, st *store.Store, name, truck string, typed time.Time) domain.Trip {
	t.Helper()
	var ids []string
	for _, n := range []string{name + ", Driver", name + ", Rider"} {
		p, err := domain.CreatePerson(st, "site-a", 1, n, nil, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.PersonID)
	}
	checkIn(t, st, "site-a", ids[0], ids[1:])
	dest, x := "site-b", int64(90)
	trip, err := domain.Dispatch(st, "site-a", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, st, "site-a", truck), DriverPersonID: ids[0], OccupantPersonIDs: ids[1:],
		DestinationSite: &dest, DeadlineMinutes: &x,
	}, typed.UnixMilli(), "entered")
	if err != nil {
		t.Fatal(err)
	}
	return trip
}

type fix17dWorld struct {
	srv                         *Server
	one, six, seven, nine, back domain.Trip // days ago; back = recorded now, typed 20 days ago
}

func fix17dSetup(t *testing.T) fix17dWorld {
	t.Helper()
	srv, st, _ := fix14ThirdPost(t)
	return fix17dWorld{
		srv:   srv,
		one:   fix17dTrip(t, st, "Aone", "TRK-201", fix17dDay(1)),
		six:   fix17dTrip(t, st, "Bsix", "TRK-206", fix17dDay(6)),
		seven: fix17dTrip(t, st, "Cseven", "TRK-207", fix17dDay(7)),
		nine:  fix17dTrip(t, st, "Dnine", "TRK-209", fix17dDay(9)),
		back:  fix17dTrip(t, st, "Eback", "TRK-220", fix17dDay(20)),
	}
}

func fix17dRows(body string) map[string]string {
	out := map[string]string{}
	for _, r := range fix17dRowRe.FindAllString(body, -1) {
		id := regexp.MustCompile(`id="trip-([^"]*)"`).FindStringSubmatch(r)[1]
		out[id] = r
	}
	return out
}

func fix17dHas(rows map[string]string, trips ...domain.Trip) (in []bool) {
	for _, tr := range trips {
		_, ok := rows[tr.TripID]
		in = append(in, ok)
	}
	return
}

func TestFix17dDefaultIsTheLastSevenDaysByTypedDeparture(t *testing.T) {
	w := fix17dSetup(t)
	rec := fix16Get(t, w.srv, "/trips/detail", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /trips/detail = %d", rec.Code)
	}
	rows := fix17dRows(rec.Body.String())
	got := fix17dHas(rows, w.one, w.six, w.seven, w.nine, w.back)
	want := []bool{true, true, false, false, false}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("default page: trip #%d listed=%v, want %v (typed 1/6/7/9/20 days ago, all recorded now)", i, got[i], want[i])
		}
	}
	body := rec.Body.String()
	// (the world's check-ins are trips recorded today, so they are listed too)
	if line := fmt.Sprintf("(7 days) · %d trips", len(rows)); !strings.Contains(body, line) {
		t.Errorf("summary line missing %q", line)
	}
	if !strings.Contains(body, "Previous 7 days") || strings.Contains(body, "Next 7 days") {
		t.Errorf("the default week must link to the previous 7 days and not past today")
	}
	// The record chain follows the listed trips only.
	if strings.Contains(body, w.nine.TripID) || strings.Contains(body, w.back.TripID) {
		t.Errorf("a trip outside the days appears somewhere on the page")
	}
}

func TestFix17dRangeFormWorksWithoutScriptAndNavigates(t *testing.T) {
	w := fix17dSetup(t)
	from, to := fix17dDay(21).Format("2006-01-02"), fix17dDay(8).Format("2006-01-02")
	rec := fix16Get(t, w.srv, "/trips/detail?from="+from+"&to="+to, false)
	rows := fix17dRows(rec.Body.String())
	got := fix17dHas(rows, w.one, w.six, w.seven, w.nine, w.back)
	want := []bool{false, false, false, true, true}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("range %s..%s: trip #%d listed=%v, want %v", from, to, i, got[i], want[i])
		}
	}
	body := rec.Body.String()
	if !regexp.MustCompile(`<form[^>]*method="get"[^>]*action="/trips/detail"`).MatchString(body) ||
		!strings.Contains(body, `name="from" value="`+from+`"`) || !strings.Contains(body, `name="to" value="`+to+`"`) {
		t.Errorf("the range is not a plain GET form carrying the chosen dates")
	}
	// A 14-day window steps by 14 days; its end is before today, so there is a next.
	if !strings.Contains(body, "Previous 14 days") || !strings.Contains(body, "Next 14 days") {
		t.Errorf("missing previous/next links")
	}
	// Whole-day, inclusive: one day.
	d := fix17dDay(6).Format("2006-01-02")
	rows = fix17dRows(fix16Get(t, w.srv, "/trips/detail?from="+d+"&to="+d, false).Body.String())
	if len(rows) != 1 || rows[w.six.TripID] == "" {
		t.Errorf("a single day lists %d trips, want just the one typed on it", len(rows))
	}
	// A bad date is said, not ignored.
	if b := fix16Get(t, w.srv, "/trips/detail?from=yesterday", false).Body.String(); !strings.Contains(b, "NOT FILTERED") {
		t.Errorf("bad date not reported")
	}
}

func TestFix17dShowAllRowsAreByteIdenticalToTheDefaultPagesRows(t *testing.T) {
	w := fix17dSetup(t)
	all := fix16Get(t, w.srv, "/trips/detail?all=1", false).Body.String()
	def := fix16Get(t, w.srv, "/trips/detail", false).Body.String()
	ra, rd := fix17dRows(all), fix17dRows(def)
	if len(ra) < 5 {
		t.Fatalf("show all lists %d trips, want every one of them (>= 5)", len(ra))
	}
	for id, row := range rd {
		if ra[id] != row {
			t.Errorf("trip %s: row differs between the default page and show all", id)
		}
	}
	if !strings.Contains(all, "Showing every trip of the season") {
		t.Errorf("show-all summary missing")
	}
	if !strings.Contains(def, `href="/trips/detail?all=1"`) || !strings.Contains(def, "slow") {
		t.Errorf("the Show all link must be there and say, in plain words, that it is slow")
	}
}

func TestFix17dCSVIsTheChosenRangeThroughTheSharedDownloadHelper(t *testing.T) {
	w := fix17dSetup(t)
	rec := fix16Get(t, w.srv, "/trips/detail.csv", true)
	recs := fix16CSV(t, rec, "trips default")
	listed := func(rs [][]string, tr domain.Trip) bool {
		for _, r := range rs[1:] {
			if r[0] == tr.TripID {
				return true
			}
		}
		return false
	}
	if strings.Join(recs[0], ",") != strings.Join(tripsCSVHeader, ",") {
		t.Errorf("header %v", recs[0])
	}
	for i, tr := range []domain.Trip{w.one, w.six, w.seven, w.nine, w.back} {
		if got, want := listed(recs, tr), i < 2; got != want {
			t.Errorf("default CSV: trip #%d listed=%v, want %v", i, got, want)
		}
	}
	wantDep := fix17dDay(6).UTC().Format("2006-01-02T15:04:05Z")
	found := false
	for _, r := range recs[1:] {
		if r[0] == w.six.TripID && r[1] == wantDep {
			found = true
		}
	}
	if !found {
		t.Errorf("CSV lacks %s departed %s: %v", w.six.TripID, wantDep, recs)
	}
	all := fix16CSV(t, fix16Get(t, w.srv, "/trips/detail.csv?all=1", true), "trips all")
	if len(all) < 1+5 {
		t.Errorf("whole-season CSV has %d records, want every trip", len(all))
	}
	from, to := fix17dDay(21).Format("2006-01-02"), fix17dDay(8).Format("2006-01-02")
	rg := fix16CSV(t, fix16Get(t, w.srv, fmt.Sprintf("/trips/detail.csv?from=%s&to=%s", from, to), true), "trips range")
	if !listed(rg, w.nine) || !listed(rg, w.back) || listed(rg, w.one) || listed(rg, w.seven) {
		t.Errorf("range CSV lists the wrong trips: %v", rg)
	}
	if rec := fix16Get(t, w.srv, "/trips/detail.csv?from=nope", true); rec.Code != http.StatusBadRequest {
		t.Errorf("bad date CSV = %d, want 400 and no file", rec.Code)
	}
	// Login: a file only for a logged-in viewer, like the other downloads.
	anon := fix16Get(t, w.srv, "/trips/detail.csv", false)
	if strings.HasPrefix(anon.Header().Get("Content-Type"), "text/csv") || anon.Header().Get("Content-Disposition") != "" {
		t.Errorf("anonymous GET of the CSV got a file")
	}
}

func TestFix17dDispatchHostStillCannotOpenTheDetailPage(t *testing.T) {
	w := fix17dSetup(t)
	for _, p := range []string{"/trips/detail", "/trips/detail.csv", "/trips/detail?all=1"} {
		rec := fix7Do(t, w.srv, "GET", fix7DispatchHost, p, nil)
		if rec.Code != http.StatusSeeOther {
			t.Errorf("dispatch host GET %s = %d, want the redirect to /analytics it always had", p, rec.Code)
		}
	}
}

// The bulk placement reads the same instant the point read does.
func TestFix17dBulkDeparturesEqualDispatchDeparture(t *testing.T) {
	w := fix17dSetup(t)
	trips, err := domain.Trips(w.srv.Store)
	if err != nil {
		t.Fatal(err)
	}
	got, err := w.srv.tripDepartures(trips)
	if err != nil {
		t.Fatal(err)
	}
	for _, tr := range trips {
		dep, _, found, err := domain.DispatchDeparture(w.srv.Store, store.Key{SiteID: tr.OriginSite, NodeEpoch: tr.NodeEpoch, Seq: tr.Seq})
		if err != nil || !found {
			t.Fatalf("trip %s: dispatch not found (%v)", tr.TripID, err)
		}
		if got[tr.TripID] != dep.UnixMilli() {
			t.Errorf("trip %s: bulk %d, point read %d", tr.TripID, got[tr.TripID], dep.UnixMilli())
		}
	}
}

// fix17dVisitAt checks one person in with the sign-in typed at `typed`.
func fix17dVisitAt(t *testing.T, st *store.Store, name string, typed time.Time) {
	t.Helper()
	p, err := domain.CreatePerson(st, "site-a", 1, name, nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	trip, err := domain.DispatchLocalCheckIn(st, "site-a", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK-14"), DriverPersonID: p.PersonID,
	}, typed.UnixMilli(), "entered")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.RecordExpectedArrival(st, "site-a", 1, trip.TripID, nil, typed.UnixMilli(), "entered"); err != nil {
		t.Fatal(err)
	}
}

func fix17dVisitRows(body string) []string {
	i := strings.Index(body, "<h2>Visits</h2>")
	if i < 0 {
		return nil
	}
	return regexp.MustCompile(`(?s)<tr [^>]*>\s*<td>.*?</tr>`).FindAllString(body[i:], -1)
}

func TestFix17dVisitsFollowTheSameDaysAndKeepTheirRows(t *testing.T) {
	srv, st, _ := fix14ThirdPost(t)
	fix17dVisitAt(t, st, "Vrecent, Rae", fix17dDay(2))
	fix17dVisitAt(t, st, "Vold, Ola", fix17dDay(12))
	def := fix16Get(t, srv, "/trips/detail", false).Body.String()
	all := fix16Get(t, srv, "/trips/detail?all=1", false).Body.String()
	if !strings.Contains(def, "Vrecent, Rae") || strings.Contains(def, "Vold, Ola") {
		t.Errorf("default page: visits must be the last 7 days' (recent in, old out)")
	}
	if !strings.Contains(all, "Vrecent, Rae") || !strings.Contains(all, "Vold, Ola") {
		t.Errorf("show all must list every visit")
	}
	ra := strings.Join(fix17dVisitRows(all), "\n")
	for _, r := range fix17dVisitRows(def) {
		if !strings.Contains(ra, r) {
			t.Errorf("visit row differs between the default page and show all:\n%s", r)
		}
	}
	old := fix17dDay(12).Format("2006-01-02")
	if b := fix16Get(t, srv, "/trips/detail?from="+old+"&to="+old, false).Body.String(); !strings.Contains(b, "Vold, Ola") || strings.Contains(b, "Vrecent, Rae") {
		t.Errorf("a range around the old visit must list it alone")
	}
	recs := fix16CSV(t, fix16Get(t, srv, "/trips/detail.csv?table=visits", true), "visits csv")
	got := fmt.Sprint(recs)
	if !strings.Contains(got, "Vrecent, Rae") || strings.Contains(got, "Vold, Ola") {
		t.Errorf("visits CSV default: %v", recs)
	}
	recs = fix16CSV(t, fix16Get(t, srv, "/trips/detail.csv?table=visits&all=1", true), "visits csv all")
	if !strings.Contains(fmt.Sprint(recs), "Vold, Ola") {
		t.Errorf("visits CSV all lacks the old visit")
	}
}
