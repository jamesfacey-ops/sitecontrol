package web

// fix12_search_filters_test.go — FIX12 (James, 2026-10-05): "include filters
// in the searching so that we can filter the results after searching and
// site should be included on the search and filters too". search.go has the
// Site term; search_refine.go has the "Narrow results" panel. Every test
// here fails on c566829 (no Site term: site= alone is not a search; no
// panel, no in_* parameters) and passes after.
//
// The world is fix9World: site-a "Ice Pad", site-b "Ice Camp B", viewed from
// site-b. Its searches here:
//
//	q=polar      people Dana (no visit), Pat (on site at Ice Pad);
//	             visits Pat at Ice Camp B (closed, out time corrected by
//	             hand), Pat at Ice Pad (open); trip TRK-12 Ice Camp B ->
//	             Ice Pad, arrived, dispatch corrected by hand.     "2 2 1"
//	site=site-a  people Joss, Pat; visits Joss, Pat at Ice Pad; TRK-12.
//	                                                               "2 2 1"

import (
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/domain"
)

// fix12Option reads one panel option's count; ok=false when it is absent.
func fix12Option(body, name, value string) (n int, checked, ok bool) {
	re := regexp.MustCompile(`<input type="checkbox" name="` + regexp.QuoteMeta(name) + `" id="[^"]*" value="` +
		regexp.QuoteMeta(value) + `" data-n="(\d+)"( checked)?`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		return 0, false, false
	}
	n, _ = strconv.Atoi(m[1])
	return n, m[2] != "", true
}

func fix12Sum(t *testing.T, body string) int {
	t.Helper()
	sum := 0
	for _, f := range strings.Fields(fix9Counts(t, body)) {
		n, _ := strconv.Atoi(f)
		sum += n
	}
	return sum
}

// fix12PanelValues is what submitting the panel's form as rendered sends:
// its hidden inputs and its ticked boxes.
func fix12PanelValues(t *testing.T, body string) url.Values {
	t.Helper()
	i := strings.Index(body, `class="search-narrow-form"`)
	if i < 0 {
		t.Fatalf("no narrow panel:\n%s", body)
	}
	form := body[i : i+strings.Index(body[i:], "</form>")]
	v := url.Values{}
	for _, m := range regexp.MustCompile(`<input type="hidden" name="([^"]*)" value="([^"]*)">`).FindAllStringSubmatch(form, -1) {
		v.Add(m[1], fix12Unescape(m[2]))
	}
	for _, m := range regexp.MustCompile(`<input type="checkbox" name="([^"]*)" id="[^"]*" value="([^"]*)" data-n="\d+" checked`).FindAllStringSubmatch(form, -1) {
		v.Add(m[1], fix12Unescape(m[2]))
	}
	return v
}

func fix12Unescape(s string) string {
	return strings.NewReplacer("&amp;", "&", "&#39;", "'", "&#34;", `"`, "&lt;", "<", "&gt;", ">").Replace(s)
}

func fix12Canon(v url.Values) string {
	for k := range v {
		sort.Strings(v[k])
	}
	return v.Encode()
}

// Site in the MAIN search, for each result type.
func TestFix12SiteInTheMainSearch(t *testing.T) {
	w := newFix9World(t)
	body := fix9Must(t, w, url.Values{})
	for _, want := range []string{`<select id="search-site" name="site"`, `<option value="">All sites</option>`,
		`<option value="site-a">Ice Pad</option>`, `<option value="site-b">Ice Camp B</option>`} {
		if !strings.Contains(body, want) {
			t.Errorf("the search form lacks %s", want)
		}
	}
	// Ice Pad: Joss and Pat on site there now; their two visits there; the
	// run that went there.
	body = fix9Must(t, w, url.Values{"site": {"site-a"}})
	if got := fix9Counts(t, body); got != "2 2 1" {
		t.Errorf("site=Ice Pad: counts = %q, want 2 2 1", got)
	}
	if v := fix9Group(body, "visits"); strings.Contains(v, ">Ice Camp B</a>") {
		t.Errorf("site=Ice Pad lists a visit at Ice Camp B:\n%s", v)
	}
	if !strings.Contains(body, `<option value="site-a" selected>Ice Pad</option>`) {
		t.Errorf("the chosen site is not echoed back into the select")
	}
	// Ice Camp B: nobody there now or last seen there (Pat moved on to Ice
	// Pad); Pat's visit there; the run that left from there.
	body = fix9Must(t, w, url.Values{"site": {"site-b"}})
	if got := fix9Counts(t, body); got != "0 1 1" {
		t.Errorf("site=Ice Camp B: counts = %q, want 0 1 1", got)
	}
	// With another term: AND.
	body = fix9Must(t, w, url.Values{"site": {"site-a"}, "last": {"ashton"}})
	if got := fix9Counts(t, body); got != "1 1 0" {
		t.Errorf("site=Ice Pad + ashton: counts = %q, want 1 1 0", got)
	}
	// An unknown site is refused in words, not searched.
	body = fix9Must(t, w, url.Values{"site": {"nowhere"}})
	if !strings.Contains(body, "NOT SEARCHED") || strings.Contains(body, "data-search-counts") {
		t.Errorf("an unknown site must be refused")
	}
}

// "Last seen there": a person whose last visit (now closed) was at a post.
func TestFix12SiteFindsPeopleLastSeenThere(t *testing.T) {
	st, setClock := anStore(t)
	setClock(anLocal(8, 0))
	lee := fix9Person(t, st, "site-b", "Lund", "Lee", "Polar Haul", "")
	setClock(anLocal(9, 0))
	anCheckIn(t, st, "site-b", "TRK-3", lee.PersonID)
	open, err := domain.OpenVisitsAt(st, "site-b")
	if err != nil || len(open) != 1 {
		t.Fatalf("open visits: %v (%d)", err, len(open))
	}
	setClock(anLocal(10, 0))
	if err := domain.CheckOutVisit(st, "site-b", 1, open[0].VisitID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	srv := anServer(st)
	_, body := fix9Search(t, srv, "", url.Values{"site": {"site-b"}}, true)
	if got := fix9Counts(t, body); got != "1 1 0" {
		t.Errorf("site=Ice Camp B: counts = %q, want 1 1 0 (Lee, last seen there)", got)
	}
	if p := fix9Group(body, "people"); !strings.Contains(p, "Lund, Lee") || !strings.Contains(p, "last seen at Ice Camp B") {
		t.Errorf("Lee, last seen at Ice Camp B, is not found:\n%s", p)
	}
	_, body = fix9Search(t, srv, "", url.Values{"site": {"site-a"}}, true)
	if got := fix9Counts(t, body); got != "0 0 0" {
		t.Errorf("site=Ice Pad: counts = %q, want 0 0 0", got)
	}
}

// Each refinement, applied on top of the search terms.
func TestFix12EachRefinement(t *testing.T) {
	w := newFix9World(t)
	polar := func(extra url.Values) url.Values {
		v := url.Values{"q": {"polar"}}
		for k, vals := range extra {
			v[k] = vals
		}
		return v
	}
	kakivik := searchKey("Kakivik Asset Management")
	cases := []struct {
		name   string
		q      url.Values
		counts string
	}{
		{"no refinement", polar(nil), "2 2 1"},
		{"site Ice Camp B", polar(url.Values{"in_site": {"site-b"}}), "0 1 1"},
		{"site Ice Pad", polar(url.Values{"in_site": {"site-a"}}), "1 1 1"},
		{"either site", polar(url.Values{"in_site": {"site-a", "site-b"}}), "1 2 1"},
		{"type visits", polar(url.Values{"in_type": {"visits"}}), "0 2 0"},
		{"type people + trips", polar(url.Values{"in_type": {"people", "trips"}}), "2 0 1"},
		{"company (from the results)", url.Values{"site": {"site-a"}, "in_company": {kakivik}}, "1 1 0"},
		{"status people on site", polar(url.Values{"in_status": {"person-onsite"}}), "1 0 0"},
		{"status people not on site", polar(url.Values{"in_status": {"person-offsite"}}), "1 0 0"},
		{"status visits open", polar(url.Values{"in_status": {"visit-open"}}), "0 1 0"},
		{"status visits closed", polar(url.Values{"in_status": {"visit-closed"}}), "0 1 0"},
		{"status trips arrived", polar(url.Values{"in_status": {"trip-arrived"}}), "0 0 1"},
		{"status trips on the road", polar(url.Values{"in_status": {"trip-road"}}), "0 0 0"},
		{"entered by hand only", polar(url.Values{"in_hand": {"1"}}), "2 1 1"},
		{"refinements combine (AND)", polar(url.Values{"in_site": {"site-b"}, "in_type": {"visits"}}), "0 1 0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body := fix9Must(t, w, c.q)
			if got := fix9Counts(t, body); got != c.counts {
				t.Errorf("%s: counts = %q, want %q", c.q.Encode(), got, c.counts)
			}
		})
	}
	// Type hides the other groups outright.
	body := fix9Must(t, w, polar(url.Values{"in_type": {"visits"}}))
	if strings.Contains(body, `id="results-people"`) || strings.Contains(body, `id="results-trips"`) || !strings.Contains(body, `id="results-visits"`) {
		t.Errorf("type=visits must list only the visits group")
	}
	if !strings.Contains(body, "Results: 2 visits</h2>") {
		t.Errorf("the results heading must count what is listed:\n%s", body[strings.Index(body, `id="results"`):][:300])
	}
	// Entered by hand: the corrected out time and the corrected dispatch.
	body = fix9Must(t, w, polar(url.Values{"in_hand": {"1"}}))
	if v := fix9Group(body, "visits"); !strings.Contains(v, "corrected by hand") || strings.Contains(v, "still on site") {
		t.Errorf("hand-only must keep the corrected visit and drop the open one:\n%s", v)
	}
}

// The refinements narrow the search; they never widen it.
func TestFix12RefinementsCombineWithTheSearchTerms(t *testing.T) {
	w := newFix9World(t)
	kakivik := searchKey("Kakivik Asset Management")
	// polar AND company Kakivik: nobody is both.
	body := fix9Must(t, w, url.Values{"q": {"polar"}, "in_company": {kakivik}})
	if got := fix9Counts(t, body); got != "0 0 0" {
		t.Errorf("polar + in_company kakivik: counts = %q, want 0 0 0", got)
	}
	// Main site Ice Pad, narrowed to Ice Camp B: only the run touches both.
	body = fix9Must(t, w, url.Values{"site": {"site-a"}, "in_site": {"site-b"}})
	if got := fix9Counts(t, body); got != "0 0 1" {
		t.Errorf("site=Ice Pad + in_site Ice Camp B: counts = %q, want 0 0 1", got)
	}
	// Refinements alone are not a search.
	body = fix9Must(t, w, url.Values{"in_site": {"site-b"}})
	if strings.Contains(body, "data-search-counts") {
		t.Errorf("refinements with no search terms must not search")
	}
}

// Every option's count is exactly what ticking it alone (on top of what is
// already ticked) leaves.
func TestFix12CountsAreWhatEachOptionLeaves(t *testing.T) {
	w := newFix9World(t)
	for _, base := range []url.Values{
		{"q": {"polar"}},
		{"site": {"site-a"}},
		{"q": {"polar"}, "in_site": {"site-b"}},
		{"q": {"polar"}, "in_type": {"visits", "trips"}, "in_hand": {"1"}},
	} {
		body := fix9Must(t, w, base)
		re := regexp.MustCompile(`<input type="checkbox" name="(in_[a-z]+)" id="[^"]*" value="([^"]*)" data-n="(\d+)"( checked)?`)
		opts := re.FindAllStringSubmatch(body, -1)
		if len(opts) < 12 {
			t.Fatalf("%s: only %d options in the panel", base.Encode(), len(opts))
		}
		for _, m := range opts {
			name, value, want, checked := m[1], fix12Unescape(m[2]), m[3], m[4] != ""
			if checked {
				continue
			}
			q := url.Values{}
			for k, v := range base {
				q[k] = append([]string(nil), v...)
			}
			if name == "in_hand" {
				q.Set(name, value)
			} else {
				// Within a facet the ticks are OR: the count is the option ON
				// ITS OWN in that facet, the other facets as they stand.
				q[name] = []string{value}
			}
			got := fix12Sum(t, fix9Must(t, w, q))
			if strconv.Itoa(got) != want {
				t.Errorf("%s: option %s=%s says (%s) but leaves %d", base.Encode(), name, value, want, got)
			}
		}
	}
	// And the labels read "Ice Pad (3)" — the count beside the post's name.
	body := fix9Must(t, w, url.Values{"q": {"polar"}})
	if !regexp.MustCompile(`>Ice Pad <span class="search-n">\(3\)</span></label>`).MatchString(body) {
		t.Errorf("the site option does not read \"Ice Pad (3)\"")
	}
	if n, _, ok := fix12Option(body, "in_company", searchKey("Polar Haul")); !ok || n != 5 {
		t.Errorf("company Polar Haul = (%d, present %v), want (5): 2 people, 2 visits, 1 trip", n, ok)
	}
}

// Clear filters keeps the search; each chip removes one filter.
func TestFix12ClearFiltersAndChips(t *testing.T) {
	w := newFix9World(t)
	body := fix9Must(t, w, url.Values{"q": {"polar"}, "in_site": {"site-b"}, "in_type": {"visits"}})
	m := regexp.MustCompile(`<a href="([^"]*)" id="search-clear-filters">Clear filters</a>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no Clear filters link")
	}
	u, _ := url.Parse(fix12Unescape(m[1]))
	if u.Path != "/search" || fix12Canon(u.Query()) != "q=polar" {
		t.Errorf("Clear filters goes to %q, want the search alone (q=polar)", m[1])
	}
	chips := regexp.MustCompile(`<a class="search-chip" href="([^"]*)">([^<]*) <span`).FindAllStringSubmatch(body, -1)
	if len(chips) != 2 {
		t.Fatalf("chips = %d, want 2 (site, type):\n%v", len(chips), chips)
	}
	want := map[string]string{
		"Site: Ice Camp B": "in_type=visits&q=polar",
		"Type: Visits":     "in_site=site-b&q=polar",
	}
	for _, c := range chips {
		u, _ := url.Parse(fix12Unescape(c[1]))
		if got := fix12Canon(u.Query()); got != want[c[2]] {
			t.Errorf("chip %q removes to %q, want %q", c[2], got, want[c[2]])
		}
	}
	// Following the site chip: the type filter stands, the site one is gone.
	u, _ = url.Parse(fix12Unescape(chips[0][1]))
	if got := fix9Counts(t, fix9Must(t, w, u.Query())); got != "0 2 0" {
		t.Errorf("after removing the site chip: counts = %q, want 0 2 0", got)
	}
	// No filters: no chips and no Clear filters.
	body = fix9Must(t, w, url.Values{"q": {"polar"}})
	if strings.Contains(body, `class="search-chip"`) || strings.Contains(body, "search-clear-filters") {
		t.Errorf("an unnarrowed search shows chips or Clear filters")
	}
}

// The URL is the whole state: the panel's form, submitted as rendered,
// reproduces the request it was rendered for.
func TestFix12URLRoundTrips(t *testing.T) {
	w := newFix9World(t)
	for _, q := range []url.Values{
		{"q": {"polar"}},
		{"q": {"polar"}, "site": {"site-a"}, "in_site": {"site-b", "site-a"}, "in_type": {"trips"}},
		{"last": {"passenger"}, "date_from": {"2026-09-29"}, "time_from": {"09:00"}, "in_company": {searchKey("Polar Haul")},
			"in_status": {"visit-closed", "trip-arrived"}, "in_hand": {"1"}},
	} {
		body := fix9Must(t, w, q)
		if got, want := fix12Canon(fix12PanelValues(t, body)), fix12Canon(q); got != want {
			t.Errorf("panel resubmits\n  %s\nwant\n  %s", got, want)
		}
		// GET only, and its action lands on the results heading.
		if !strings.Contains(body, `<form method="get" action="/search#results" class="search-narrow-form"`) {
			t.Errorf("the panel is not a GET form to /search#results")
		}
	}
}

// Turned back and overdue, each from its own fixture.
func TestFix12TripStatusTurnedBackAndOverdue(t *testing.T) {
	st, setClock := anStore(t)
	setClock(anLocal(8, 0))
	ray := fix9Person(t, st, "site-b", "Ross", "Ray", "Polar Haul", "")
	setClock(anLocal(9, 0))
	dest, mins := "site-a", int64(90)
	trip, err := domain.Dispatch(st, "site-b", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, st, "site-b", "TRK-77"), DriverPersonID: ray.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &mins,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	setClock(anLocal(9, 30))
	if _, err := domain.RecordReturnToOrigin(st, "site-b", 1, trip.TripID, nil, nil, 0, "default", false); err != nil {
		t.Fatal(err)
	}
	srv := anServer(st)
	_, body := fix9Search(t, srv, "", url.Values{"truck": {"TRK-77"}, "in_status": {"trip-back"}}, true)
	if got := fix9Counts(t, body); got != "0 0 1" {
		t.Errorf("turned back: counts = %q, want 0 0 1", got)
	}
	_, body = fix9Search(t, srv, "", url.Values{"truck": {"TRK-77"}, "in_status": {"trip-arrived"}}, true)
	if got := fix9Counts(t, body); got != "0 0 0" {
		t.Errorf("a run that turned back is not 'arrived': counts = %q", got)
	}

	osrv, _, tripID := fix7Overdue(t)
	q := url.Values{"truck": {"TRK-DSP-1"}}
	code, body := fix9Search(t, osrv, "", q, true)
	if code != http.StatusOK {
		t.Fatalf("GET /search = %d", code)
	}
	if n, _, ok := fix12Option(body, "in_status", "trip-overdue"); !ok || n != 1 {
		t.Errorf("overdue option = (%d, %v), want (1)", n, ok)
	}
	if trips := fix9Group(body, "trips"); !strings.Contains(trips, "overdue") || !strings.Contains(trips, tripID) {
		t.Errorf("the overdue run is not shown as overdue:\n%s", trips)
	}
	q.Set("in_status", "trip-overdue")
	if _, body = fix9Search(t, osrv, "", q, true); fix9Counts(t, body) != "0 0 1" {
		t.Errorf("in_status=trip-overdue: counts = %q, want 0 0 1", fix9Counts(t, body))
	}
	q.Set("in_status", "trip-road")
	if _, body = fix9Search(t, osrv, "", q, true); fix9Counts(t, body) != "0 0 0" {
		t.Errorf("an overdue run counts as on the road too: %q", fix9Counts(t, body))
	}
}

// tripStatusKey: one value per run, overdue and turned back taking
// precedence.
func TestFix12TripStatusKey(t *testing.T) {
	for _, c := range []struct {
		state             string
		returned, overdue bool
		want              string
	}{
		{domain.TripStateOpen, false, false, "trip-road"},
		{domain.TripStateOpen, false, true, "trip-overdue"},
		{domain.TripStateArrived, false, false, "trip-arrived"},
		{domain.TripStateArrived, true, false, "trip-back"},
		{"", false, false, ""},
	} {
		if got := tripStatusKey(c.state, c.returned, c.overdue); got != c.want {
			t.Errorf("tripStatusKey(%q, %v, %v) = %q, want %q", c.state, c.returned, c.overdue, got, c.want)
		}
	}
}

// The dispatch host: the same filters, GET only, nothing linking into the
// console; writes still 403; anonymous gets the login form only.
func TestFix12FiltersOnTheDispatchHost(t *testing.T) {
	w := newFix9World(t)
	q := url.Values{"q": {"polar"}, "site": {"site-a"}, "in_site": {"site-b"}, "in_type": {"trips"}}
	code, body := fix9Search(t, w.srv, fix7DispatchHost, q, true)
	if code != http.StatusOK {
		t.Fatalf("GET /search on the dispatch host = %d", code)
	}
	if got := fix9Counts(t, body); got != "0 0 1" {
		t.Errorf("dispatch host: counts = %q, want 0 0 1", got)
	}
	if !strings.Contains(body, `class="search-narrow-form"`) || !strings.Contains(body, `class="search-chip"`) {
		t.Errorf("the dispatch host lacks the panel or its chips")
	}
	if m := fix7ConsoleHref.FindString(body); m != "" || strings.Contains(body, "/trips/detail") {
		t.Errorf("the dispatch host's filtered search links into the console (%s)", m)
	}
	for _, href := range regexp.MustCompile(`href="([^"]*)"`).FindAllStringSubmatch(body, -1) {
		// FIX16f: the history reports are dispatch-readable too (dispatchReadable).
		if h := href[1]; !(strings.HasPrefix(h, "/analytics") || strings.HasPrefix(h, "/search") || strings.HasPrefix(h, "/theme") || strings.HasPrefix(h, "/history/")) {
			t.Errorf("the dispatch host's filtered search links to %q", h)
		}
	}
	for _, m := range regexp.MustCompile(`<form[^>]*>`).FindAllString(body, -1) {
		if !strings.Contains(m, `method="get"`) {
			t.Errorf("a non-GET form on the dispatch host's search: %s", m)
		}
	}
	if rec := fix7Do(t, w.srv, "POST", fix7DispatchHost, "/search", q); rec.Code != http.StatusForbidden {
		t.Errorf("POST /search with filters on the dispatch host = %d, want 403", rec.Code)
	}
	for _, host := range []string{"", fix7DispatchHost} {
		_, anon := fix9Search(t, w.srv, host, q, false)
		if !strings.Contains(anon, `name="password"`) || strings.Contains(anon, `class="search-narrow-form"`) ||
			strings.Contains(anon, "Polar") || strings.Contains(anon, "TRK-12") || strings.Contains(anon, "data-search-counts") {
			t.Errorf("host %q: anonymous filtered /search must be the login form only", host)
		}
	}
}

// The panel is accessible: fieldsets with legends, a label for every box,
// a <details> "Filter results", and no live region.
func TestFix12PanelMarkup(t *testing.T) {
	w := newFix9World(t)
	body := fix9Must(t, w, url.Values{"q": {"polar"}})
	i := strings.Index(body, `<details class="search-narrow"`)
	if i < 0 {
		t.Fatalf("no <details> panel")
	}
	panel := body[i : i+strings.Index(body[i:], "</details>\n\n")]
	if !strings.Contains(panel, "<summary>Filter results</summary>") {
		t.Errorf("the panel's summary is not \"Filter results\"")
	}
	for _, legend := range []string{"Site", "Result type", "Company", "Status", "People", "Visits", "Trips"} {
		if !strings.Contains(panel, "<legend>"+legend+"</legend>") {
			t.Errorf("no fieldset legend %q", legend)
		}
	}
	for _, m := range regexp.MustCompile(`<input type="checkbox"[^>]* id="([^"]*)"`).FindAllStringSubmatch(panel, -1) {
		if !strings.Contains(panel, `<label for="`+m[1]+`">`) {
			t.Errorf("checkbox %s has no label", m[1])
		}
	}
	if strings.Contains(panel, "aria-live") || strings.Contains(panel, `role="status"`) ||
		regexp.MustCompile(`<h2 id="results"[^>]*aria-live`).MatchString(body) {
		t.Errorf("the panel must not use a live region")
	}
	if strings.Contains(body, "localStorage") || strings.Contains(body, "sessionStorage") {
		t.Errorf("the search page must not store filter state outside the URL")
	}
}

// One pass: the panel adds nothing measurable to a search (the counts are
// computed from the rows already found, not by more queries).
func TestFix12NarrowingCostsNoExtraSearch(t *testing.T) {
	w := newFix9World(t)
	plain, narrowed := time.Hour, time.Hour
	for i := 0; i < 5; i++ {
		s := time.Now()
		fix9Must(t, w, url.Values{"q": {"polar"}})
		if d := time.Since(s); d < plain {
			plain = d
		}
		s = time.Now()
		fix9Must(t, w, url.Values{"q": {"polar"}, "in_site": {"site-b"}, "in_status": {"visit-closed"}, "in_hand": {"1"}})
		if d := time.Since(s); d < narrowed {
			narrowed = d
		}
	}
	if narrowed > 3*plain+50*time.Millisecond {
		t.Errorf("narrowing costs %s against %s for the plain search", narrowed, plain)
	}
}
