package web

// fix9_search_test.go — FIX9 (James, 2026-10-05): "now i would like to add a
// searching feature as well to all sites and dispatch it should be able to
// search by last name, first name, company, date, time, phone number and
// truck number". search.go has the design. Every test here fails on 06e2249
// (no /search: the GET falls through to the status page) and passes after.

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// fix9World is two posts' records held at site-b ("Ice Camp B"): site-a
// ("Ice Pad") authored its own roster and a check-in, which replicated in.
//
//	site-a  08:00  roster: Ashton, Joss (Kakivik, (907) 555-4471),
//	               Driver, Dana (Polar Haul, 907.555.1234)
//	site-a  09:00  Joss checks in at Ice Pad on TRK-7
//	site-b  08:00  roster: Passenger, Pat (Polar Haul, no phone)
//	site-b  10:00  Pat checks in at Ice Camp B on TRUCK 9
//	site-b  11:00  run TRK-12 Ice Camp B -> Ice Pad, driver Pat
//	site-a  12:00  the run arrives at Ice Pad
//	site-b  12:30  the run's dispatch time is corrected to 10:45, and Dana is
//	               added aboard ("aboard after all") — the EFFECTIVE record.
//	               A correction is record-only: it opens no visit for Dana.
type fix9World struct {
	srv          *Server
	joss, dana   domain.Person
	pat          domain.Person
	runID        string
	jossVisitKey string
}

func fix9Person(t *testing.T, st *store.Store, site, last, first, company, phone string) domain.Person {
	t.Helper()
	co, err := domain.CreateCompany(st, site, 1, company, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	d := domain.PersonDetails{LastName: &last, FirstName: &first}
	if phone != "" {
		d.Phone = &phone
	}
	p, err := domain.CreatePersonWithDetails(st, site, 1, last+", "+first, &co.CompanyID, nil, nil, d, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func newFix9World(t *testing.T) fix9World {
	t.Helper()
	a, setA := anStore(t)
	b, setB := anStore(t)
	var w fix9World

	setA(anLocal(8, 0))
	w.joss = fix9Person(t, a, "site-a", "Ashton", "Joss", "Kakivik Asset Management", "(907) 555-4471")
	w.dana = fix9Person(t, a, "site-a", "Driver", "Dana", "Polar Haul", "907.555.1234")
	setA(anLocal(9, 0))
	anCheckIn(t, a, "site-a", "TRK-7", w.joss.PersonID)
	fgReplicate(t, b, a, "site-a")

	setB(anLocal(8, 0))
	w.pat = fix9Person(t, b, "site-b", "Passenger", "Pat", "Polar Haul", "")
	setB(anLocal(10, 0))
	anCheckIn(t, b, "site-b", "TRUCK 9", w.pat.PersonID)
	setB(anLocal(11, 0))
	dest, mins := "site-a", int64(90)
	trip, err := domain.Dispatch(b, "site-b", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, b, "site-b", "TRK-12"), DriverPersonID: w.pat.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &mins,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w.runID = trip.TripID
	fgReplicate(t, a, b, "site-b")
	setA(anLocal(12, 0))
	if _, err := domain.RecordExpectedArrival(a, "site-a", 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	fgReplicate(t, b, a, "site-a")

	setB(anLocal(12, 30))
	if _, err := domain.CorrectOccurredAt(b, "site-b", 1, domain.TimeCorrection{
		Target: store.Key{SiteID: "site-b", NodeEpoch: 1, Seq: trip.Seq}, CorrectedOccurredAtMS: anLocal(10, 45), RecordedBy: "Officer Kay",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := domain.AmendTrip(b, "site-b", 1, domain.TripAmendment{TripID: trip.TripID, OccupantsAdd: []string{w.dana.PersonID}}, 0, "default"); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		SiteID: "site-b", DisplayName: "Ice Camp B", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: b.Path, SyncIntervalSeconds: 10,
		Peers:         []config.Peer{{SiteID: "site-a", BaseURL: "http://example.invalid"}},
		Destinations:  []config.Destination{{ID: "site-a", DisplayName: "Ice Pad", Controlled: true, TravelMinutes: 65}},
		LoginPassword: testLoginPassword,
	}
	w.srv = New(cfg, b)
	return w
}

// fix9Search GETs /search with the given query through Root (the served
// surface, dispatch guard included).
func fix9Search(t *testing.T, srv *Server, host string, q url.Values, login bool) (int, string) {
	t.Helper()
	req := httptest.NewRequest("GET", "/search?"+q.Encode(), nil)
	if host != "" {
		req.Host = host
	}
	if login {
		req.AddCookie(loginCookie(t, srv, testLoginPassword))
	}
	rec := httptest.NewRecorder()
	srv.Root().ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// fix9Group returns one result group's table, "" when the group is empty.
func fix9Group(body, group string) string {
	i := strings.Index(body, `data-search-group="`+group+`"`)
	if i < 0 {
		return ""
	}
	rest := body[i:]
	return rest[:strings.Index(rest, "</table>")]
}

// fix9Counts reads the results heading's "people visits trips" counts.
func fix9Counts(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`data-search-counts="([^"]*)"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no results heading:\n%s", body)
	}
	return m[1]
}

func fix9Must(t *testing.T, w fix9World, q url.Values) string {
	t.Helper()
	code, body := fix9Search(t, w.srv, "", q, true)
	if code != http.StatusOK {
		t.Fatalf("GET /search?%s = %d", q.Encode(), code)
	}
	return body
}

// Each filter alone, plus the normalisations it promises.
func TestFix9SearchEachFilterAlone(t *testing.T) {
	w := newFix9World(t)
	cases := []struct {
		name   string
		q      url.Values
		counts string // people visits trips
		people []string
		absent []string
	}{
		{"last name", url.Values{"last": {"ashton"}}, "1 1 0", []string{"Ashton, Joss"}, []string{"Driver, Dana", "Passenger, Pat"}},
		{"first name", url.Values{"first": {"DANA"}}, "1 0 1", []string{"Driver, Dana"}, []string{"Ashton, Joss"}},
		{"company", url.Values{"company": {"polar"}}, "2 2 1", []string{"Driver, Dana", "Passenger, Pat"}, []string{"Ashton, Joss"}},
		{"phone by digits", url.Values{"phone": {"555-44"}}, "1 1 0", []string{"Ashton, Joss"}, []string{"Driver, Dana"}},
		{"phone formatted differently", url.Values{"phone": {"(907) 555 1234"}}, "1 0 1", []string{"Driver, Dana"}, []string{"Ashton, Joss"}},
		{"truck ignoring case, spaces, hyphens", url.Values{"truck": {"trk 12"}}, "2 1 1", []string{"Driver, Dana", "Passenger, Pat"}, []string{"Ashton, Joss"}},
		{"name normalised like the retired-name check", url.Values{"q": {"Ashton,  JOSS"}}, "1 1 0", []string{"Ashton, Joss"}, nil},
		{"free text truck", url.Values{"q": {"truck9"}}, "0 1 0", nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body := fix9Must(t, w, c.q)
			if got := fix9Counts(t, body); got != c.counts {
				t.Errorf("counts (people visits trips) = %q, want %q", got, c.counts)
			}
			people := fix9Group(body, "people")
			for _, n := range c.people {
				if !strings.Contains(people, n) {
					t.Errorf("people group lacks %q:\n%s", n, people)
				}
			}
			for _, n := range c.absent {
				if strings.Contains(people, n) {
					t.Errorf("people group carries %q, which does not match:\n%s", n, people)
				}
			}
		})
	}
}

// Filters combine with AND; on a trip the person filters must hold for ONE
// person aboard.
func TestFix9SearchCombinesFiltersWithAnd(t *testing.T) {
	w := newFix9World(t)
	body := fix9Must(t, w, url.Values{"company": {"polar"}, "last": {"passenger"}})
	if got := fix9Counts(t, body); got != "1 2 1" { // Pat: in at Ice Camp B, then off TRK-12 at Ice Pad
		t.Errorf("polar + passenger: counts = %q, want 1 2 1", got)
	}
	if p := fix9Group(body, "people"); !strings.Contains(p, "Passenger, Pat") || strings.Contains(p, "Driver, Dana") {
		t.Errorf("polar + passenger: wrong people:\n%s", p)
	}
	// Ashton is not aboard TRK-12: truck AND last name finds nothing.
	body = fix9Must(t, w, url.Values{"truck": {"TRK-12"}, "last": {"ashton"}})
	if got := fix9Counts(t, body); got != "0 0 0" {
		t.Errorf("TRK-12 + ashton: counts = %q, want 0 0 0", got)
	}
	// Last name of one person and first name of another, both aboard: not
	// one person, so not a match.
	body = fix9Must(t, w, url.Values{"last": {"passenger"}, "first": {"dana"}})
	if got := fix9Counts(t, body); got != "0 0 0" {
		t.Errorf("passenger + dana: counts = %q, want 0 0 0 (no one person is both)", got)
	}
}

// Date and time filter visits and trips by their recorded instants, the
// corrected one where a correction stands.
func TestFix9SearchDateAndTimeRanges(t *testing.T) {
	w := newFix9World(t)
	// Joss signed in at Ice Pad at 09:00; nothing else happened 08:55-09:05.
	body := fix9Must(t, w, url.Values{"date_from": {"2026-09-29"}, "time_from": {"08:55"}, "time_to": {"09:05"}})
	if got := fix9Counts(t, body); got != "1 1 0" {
		t.Errorf("09:00 window: counts = %q, want 1 1 0", got)
	}
	if v := fix9Group(body, "visits"); !strings.Contains(v, "Ashton, Joss") || !strings.Contains(v, "2026-09-29 09:00 (Ice Pad&#39;s clock)") {
		t.Errorf("09:00 window: Joss's visit, by Ice Pad's clock, missing:\n%s", v)
	}
	// The run was dispatched at 11:00, corrected to 10:45, and arrived at
	// 12:00. 10:46-10:50 finds it only through the correction, marked as
	// corrected by hand; 10:30-10:44 is before it left.
	body = fix9Must(t, w, url.Values{"time_from": {"10:46"}, "time_to": {"10:50"}})
	trips := fix9Group(body, "trips")
	if !strings.Contains(trips, "TRK-12") || !strings.Contains(trips, "2026-09-29 10:45 (Ice Camp B&#39;s clock)") || !strings.Contains(trips, "corrected by hand") {
		t.Errorf("10:46 window: the corrected run is missing or unmarked:\n%s", trips)
	}
	body = fix9Must(t, w, url.Values{"time_from": {"10:30"}, "time_to": {"10:44"}})
	if strings.Contains(fix9Group(body, "trips"), "TRK-12") {
		t.Errorf("10:30-10:44 finds the run before its (corrected) departure")
	}
	// OVERLAP (lead's ruling): a window wholly inside a visit or a run finds
	// it. 11:30-11:40 is mid-run; Joss, checked in at 09:00 and never out,
	// is on site then too.
	body = fix9Must(t, w, url.Values{"date_from": {"2026-09-29"}, "time_from": {"11:30"}, "time_to": {"11:40"}})
	if !strings.Contains(fix9Group(body, "trips"), "TRK-12") {
		t.Errorf("a window inside the run does not find it")
	}
	if v := fix9Group(body, "visits"); !strings.Contains(v, "Ashton, Joss") {
		t.Errorf("a window inside Joss's open visit does not find it:\n%s", v)
	}
	// 08:00-09:30 is before Pat checked in anywhere.
	body = fix9Must(t, w, url.Values{"last": {"passenger"}, "date_from": {"2026-09-29"}, "time_from": {"08:00"}, "time_to": {"09:30"}})
	if got := fix9Counts(t, body); got != "0 0 0" {
		t.Errorf("Pat before any check-in: counts = %q, want 0 0 0", got)
	}
	// A different day finds nothing.
	body = fix9Must(t, w, url.Values{"date_from": {"2026-09-30"}})
	if got := fix9Counts(t, body); got != "0 0 0" {
		t.Errorf("next day: counts = %q, want 0 0 0", got)
	}
	// A malformed time is refused in words, nothing searched.
	body = fix9Must(t, w, url.Values{"time_from": {"9am"}})
	if !strings.Contains(body, "NOT SEARCHED") || strings.Contains(body, "data-search-counts") {
		t.Errorf("a malformed time must be refused, not guessed:\n%s", body)
	}
}

// Results from every post, each labelled with its post's display name, and
// never a raw site id in prose.
func TestFix9SearchCrossPostResultsLabelledByPost(t *testing.T) {
	w := newFix9World(t)
	body := fix9Must(t, w, url.Values{"company": {"polar"}})
	visits := fix9Group(body, "visits")
	for _, want := range []string{
		`<a href="/analytics/site/site-b">Ice Camp B</a>`, // Pat's visit, here
		`<a href="/analytics/site/site-a">Ice Pad</a>`,    // Pat off TRK-12 at the other post
	} {
		if !strings.Contains(visits, want) {
			t.Errorf("visits lack the post label %s:\n%s", want, visits)
		}
	}
	text := regexp.MustCompile(`<[^>]*>`).ReplaceAllString(fix9Group(body, "people")+visits+fix9Group(body, "trips"), " ")
	if strings.Contains(text, "site-a") || strings.Contains(text, "site-b") {
		t.Errorf("a raw site id is shown in the results:\n%s", text)
	}
}

// Phones are searched by digits and shown IN FULL to a logged-in viewer, on
// a post and on the dispatch host alike (James, 2026-10-05: "show the full
// phone number"). PII-1 stands: anonymous /search is the login form and
// anonymous /gate carries no phone.
func TestFix9SearchShowsFullPhonesBehindTheLogin(t *testing.T) {
	w := newFix9World(t)
	for _, host := range []string{"", fix7DispatchHost} {
		code, body := fix9Search(t, w.srv, host, url.Values{"phone": {"4471"}}, true)
		if code != http.StatusOK {
			t.Fatalf("host %q: GET /search = %d", host, code)
		}
		if people := fix9Group(body, "people"); !strings.Contains(people, `<span class="search-phone">(907) 555-4471</span>`) {
			t.Errorf("host %q: Joss's full phone is not shown:\n%s", host, people)
		}
		if strings.Contains(body, "•••") {
			t.Errorf("host %q: a phone is still masked", host)
		}
	}
	for _, host := range []string{"", fix7DispatchHost} {
		_, anon := fix9Search(t, w.srv, host, url.Values{"phone": {"4471"}}, false)
		if strings.Contains(anon, "4471") || strings.Contains(anon, "1234") || !strings.Contains(anon, `name="password"`) {
			t.Errorf("host %q: anonymous /search must be the login form and carry no phone", host)
		}
	}
	req := httptest.NewRequest("GET", "/gate", nil)
	rec := httptest.NewRecorder()
	w.srv.Root().ServeHTTP(rec, req)
	if b := rec.Body.String(); strings.Contains(b, "555-4471") || strings.Contains(b, "555.1234") || strings.Contains(b, "907.555") {
		t.Errorf("anonymous /gate carries a phone number (PII-1)")
	}
}

// FIX8's hand-entered times (per-person Time out on the roster, a typed
// Time in at the individual check-in) are envelope claims, and the search
// shows them as recorded, marked "entered by hand".
func TestFix9SearchShowsHandEnteredTimes(t *testing.T) {
	st, setClock := anStore(t)
	setClock(anLocal(8, 0))
	kim := fix9Person(t, st, "site-b", "Kerr", "Kim", "Polar Haul", "")
	setClock(anLocal(14, 0))
	trip, err := domain.DispatchLocalCheckIn(st, "site-b", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, st, "site-b", "TRK-40"), DriverPersonID: kim.PersonID,
	}, anLocal(13, 5), "entered")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.RecordExpectedArrival(st, "site-b", 1, trip.TripID, nil, anLocal(13, 5), "entered"); err != nil {
		t.Fatal(err)
	}
	open, err := domain.OpenVisitsAt(st, "site-b")
	if err != nil || len(open) != 1 {
		t.Fatalf("open visits: %v (%d)", err, len(open))
	}
	setClock(anLocal(17, 0))
	if err := domain.CheckOutVisit(st, "site-b", 1, open[0].VisitID, nil, anLocal(16, 20), "entered"); err != nil {
		t.Fatal(err)
	}
	srv := anServer(st)
	_, body := fix9Search(t, srv, "", url.Values{"last": {"kerr"}}, true)
	v := fix9Group(body, "visits")
	for _, want := range []string{
		`2026-09-29 13:05 (Ice Camp B&#39;s clock) <strong class="search-hand">entered by hand</strong>`,
		`2026-09-29 16:20 (Ice Camp B&#39;s clock) <strong class="search-hand">entered by hand</strong>`,
	} {
		if !strings.Contains(v, want) {
			t.Errorf("visit lacks %q:\n%s", want, v)
		}
	}
	_, body = fix9Search(t, srv, "", url.Values{"time_from": {"16:15"}, "time_to": {"16:25"}}, true)
	if !strings.Contains(fix9Group(body, "visits"), "Kerr, Kim") {
		t.Errorf("the hand-entered time out does not find the visit")
	}
}

// Anonymous: the login form, and nothing found.
func TestFix9SearchRequiresLogin(t *testing.T) {
	w := newFix9World(t)
	code, body := fix9Search(t, w.srv, "", url.Values{"last": {"ashton"}}, false)
	if code != http.StatusOK || !strings.Contains(body, `name="password"`) {
		t.Fatalf("anonymous GET /search = %d, want the login form:\n%s", code, body)
	}
	for _, leak := range []string{"Ashton", "Joss", "4471", "TRK-12", "data-search-counts"} {
		if strings.Contains(body, leak) {
			t.Errorf("anonymous GET /search leaks %q", leak)
		}
	}
}

// On the dispatch host: the search works, links into nothing but the
// analytics pages, and every write is still refused 403.
func TestFix9SearchOnTheDispatchHost(t *testing.T) {
	w := newFix9World(t)
	code, body := fix9Search(t, w.srv, fix7DispatchHost, url.Values{"q": {"polar"}}, true)
	if code != http.StatusOK {
		t.Fatalf("GET /search on the dispatch host = %d, want 200", code)
	}
	if got := fix9Counts(t, body); got != "2 2 1" {
		t.Errorf("dispatch-host counts = %q, want 2 2 1", got)
	}
	if m := fix7ConsoleHref.FindString(body); m != "" || strings.Contains(body, "operator console") || strings.Contains(body, "/trips/detail") {
		t.Errorf("the dispatch host's search links into the operator console (%s)", m)
	}
	for _, href := range regexp.MustCompile(`href="([^"]*)"`).FindAllStringSubmatch(body, -1) {
		h := href[1]
		// FIX16a: a person's "last seen" is a dispatch-allowed read too;
		// FIX16f: so are the history reports (dispatchReadable).
		if !(strings.HasPrefix(h, "/analytics") || strings.HasPrefix(h, "/search") || strings.HasPrefix(h, "/theme") || strings.HasPrefix(h, "/last-seen?") || strings.HasPrefix(h, "/history/")) {
			t.Errorf("the dispatch host's search links to %q", h)
		}
	}
	// A gate host still gets the console links.
	_, gateBody := fix9Search(t, w.srv, fix7GateHost, url.Values{"q": {"polar"}}, true)
	if !strings.Contains(gateBody, `href="/trips/detail#trip-`+w.runID+`"`) || !strings.Contains(gateBody, `href="/roster#person-`+w.pat.PersonID+`"`) {
		t.Errorf("a gate host's results must link the trip and the roster row")
	}
	// Writes: still 403 on the dispatch host, including a POST to /search.
	for _, p := range []string{"/search", "/gate/checkin", "/trips/dispatch"} {
		if rec := fix7Do(t, w.srv, "POST", fix7DispatchHost, p, url.Values{"q": {"x"}}); rec.Code != http.StatusForbidden {
			t.Errorf("POST %s on the dispatch host = %d, want 403", p, rec.Code)
		}
	}
	// The analytics nav offers the search on the dispatch host.
	if rec := fix7Do(t, w.srv, "GET", fix7DispatchHost, "/analytics", nil); !strings.Contains(rec.Body.String(), `<a href="/search">search records</a>`) {
		t.Errorf("/analytics on the dispatch host has no search link")
	}
}

// A corrected trip shows its EFFECTIVE people aboard: Dana was added after
// the dispatch and is found aboard TRK-12.
func TestFix9SearchCorrectedTripShowsEffectiveOccupants(t *testing.T) {
	w := newFix9World(t)
	body := fix9Must(t, w, url.Values{"truck": {"TRK-12"}})
	trips := fix9Group(body, "trips")
	if !strings.Contains(trips, "Driver, Dana") || !strings.Contains(trips, "Passenger, Pat") {
		t.Errorf("TRK-12 must show its driver and the person added aboard by correction:\n%s", trips)
	}
	if !strings.Contains(trips, "arrived") || !strings.Contains(trips, "2026-09-29 12:00 (Ice Pad&#39;s clock)") {
		t.Errorf("TRK-12's arrival at Ice Pad is missing:\n%s", trips)
	}
	body = fix9Must(t, w, url.Values{"first": {"dana"}})
	if !strings.Contains(fix9Group(body, "trips"), "TRK-12") {
		t.Errorf("Dana, aboard by correction, does not find the run")
	}
}

// The page nav on a post offers the search.
func TestFix9SearchIsInThePostNav(t *testing.T) {
	w := newFix9World(t)
	for _, p := range []string{"/trips", "/trips/detail", "/muster", "/status"} {
		if body := getAs(t, w.srv, p); !strings.Contains(body, `<a href="/search">search</a>`) {
			t.Errorf("%s: page nav has no search link", p)
		}
	}
}

// The times-only (every day) window under the overlap rule, including one
// that wraps midnight.
func TestFix9SearchDailyWindowOverlap(t *testing.T) {
	night := &timeWindow{daily: true, fromTOD: "22:00", toTOD: "06:00"}
	day := &timeWindow{daily: true, fromTOD: "02:00", toTOD: "03:00"}
	cases := []struct {
		w        *timeWindow
		from, to int64
		want     bool
		name     string
	}{
		{day, anLocal(20, 0), anLocal(20, 0) + 12*3600_000, true, "overnight span covers 02:00-03:00"},
		{night, anLocal(9, 0), anLocal(10, 0), false, "daytime visit, night window"},
		{night, anLocal(5, 0), anLocal(5, 30), true, "05:00-05:30 inside the wrapped window"},
		{night, anLocal(21, 0), anLocal(21, 30), false, "21:00-21:30, before the night window"},
		{night, anLocal(21, 0), anLocal(22, 30), true, "runs into the night window"},
	}
	for _, c := range cases {
		if got := c.w.overlaps(c.from, c.to); got != c.want {
			t.Errorf("%s: overlaps = %v, want %v", c.name, got, c.want)
		}
	}
}
