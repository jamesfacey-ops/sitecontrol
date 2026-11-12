// search.go — GET /search: find people, visits and trips across every
// post's replicated records (FIX9, James 2026-10-05: "now i would like to
// add a searching feature as well to all sites and dispatch it should be
// able to search by last name, first name, company, date, time, phone
// number and truck number").
//
// READ-ONLY, STRUCTURALLY. GET only; the form is a plain GET form that works
// with scripting off; nothing is authored by loading the page and the page
// holds no other form. It is behind the login (requireLogin), like every
// page that names people. On the DISPATCH host (dispatch_surface.go) it is
// one of the allow-listed reads, renders with no link into the operator
// console (post names link to /analytics/site/{id} only) and its alert strip
// is the read-only one; the guard still refuses every write there.
//
// WHAT IT SEARCHES — every record this post holds, which is every post's
// (replication): the composed roster (domain.Persons), the visit report
// (domain.VisitReport — every visit, its effective time in and time out,
// corrections applied), and every run (domain.Trips composed through
// domain.EffectiveTrip, so a corrected trip shows its EFFECTIVE driver,
// truck and people aboard), with each dispatch and arrival instant from
// domain.LoadTripInstants. Nothing is cached and no new record is read.
//
// MATCHING. Every filter is a case-insensitive substring and the filters
// combine with AND. Names are compared under domain.RetiredNameKey (commas
// and runs of spaces are one separator), phones by their digits only, truck
// numbers ignoring case, spaces and hyphens. The free-text box splits into
// words and every word must match something on the record. On a trip the
// person filters must all hold for ONE person aboard (driver or occupant).
// Date and time match a visit whose person was on site, or a trip that was
// on the road, at ANY point in the window (timeWindow.spanMatches; lead's
// ruling 2026-10-05). Times with no date are the same hours on every day; a
// time with no date on its side takes the other side's date. Each instant is
// the one its own post's clock recorded, and is labelled so.
//
// PRIVACY (PII-1). The page is behind the login on every host, dispatch
// included, so results carry the FULL phone number as recorded (James,
// 2026-10-05: "show the full phone number"); an anonymous request gets the
// login form and nothing else. ⟦was: masked to the last four digits⟧
package web

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
)

// searchCap is how many rows of one group render before "show all".
// searchCapAll is the ceiling once a group is expanded.
const (
	searchCap    = 50
	searchCapAll = 1000
)

func (s *Server) registerSearchRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /search", s.requireLogin(s.handleSearch))
	mux.HandleFunc("GET /search.csv", s.requireLogin(s.handleSearchCSV)) // FIX16: search_csv.go
}

// searchForm is the query as typed, echoed back into the form.
// Site is a post's site id (FIX12): the main search's "Site" select.
type searchForm struct {
	Q, Last, First, Company, Phone, Truck string
	DateFrom, DateTo, TimeFrom, TimeTo    string
	Site                                  string
}

func (f searchForm) active() bool {
	return f.Q != "" || f.Last != "" || f.First != "" || f.Company != "" || f.Phone != "" ||
		f.Truck != "" || f.DateFrom != "" || f.DateTo != "" || f.TimeFrom != "" || f.TimeTo != "" || f.Site != ""
}

// searchFormKeys is the form's parameters in the form's own order — the
// order the refinement panel's hidden inputs carry them in.
var searchFormKeys = []string{"q", "last", "first", "company", "phone", "truck", "site", "date_from", "time_from", "date_to", "time_to"}

func (f searchForm) values() url.Values {
	v := url.Values{}
	for k, val := range map[string]string{"q": f.Q, "last": f.Last, "first": f.First, "company": f.Company,
		"phone": f.Phone, "truck": f.Truck, "date_from": f.DateFrom, "date_to": f.DateTo,
		"time_from": f.TimeFrom, "time_to": f.TimeTo, "site": f.Site} {
		if val != "" {
			v.Set(k, val)
		}
	}
	return v
}

// searchFilter is the compiled query.
type searchFilter struct {
	words                       []string // free text, each word must match
	last, first, company, phone string   // normalised; "" = not filtering
	truck                       string
	site                        string // a post's site id; "" = all sites
	window                      *timeWindow
}

func (f searchFilter) personFiltered() bool {
	return f.last != "" || f.first != "" || f.company != "" || f.phone != ""
}

// eventFiltered: a filter only a visit or a trip can satisfy. With one set,
// the People group lists only people on a matching visit or trip.
func (f searchFilter) eventFiltered() bool { return f.truck != "" || f.window != nil }

// searchKey is the retired-name check's comparison key (domain.RetiredNameKey).
func searchKey(s string) string { return domain.RetiredNameKey(s) }

func digitsOf(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// truckKey: case, spaces, hyphens (and the other separators people type in a
// truck number) do not count.
func truckKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch r {
		case ' ', '\t', '-', '_', '.', '/', '#':
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// timeWindow is the date/time filter. daily: only times were given, so the
// window is a time of day on every day (it may wrap midnight). Otherwise it
// is an instant range, either end open.
type timeWindow struct {
	daily            bool
	fromTOD, toTOD   string // "15:04"; "" = open
	start, end       int64  // ms, inclusive
	hasStart, hasEnd bool
	Text             string
}

func (w *timeWindow) contains(ms int64) bool {
	if w.daily {
		tod := time.UnixMilli(ms).In(time.Local).Format("15:04")
		switch {
		case w.fromTOD != "" && w.toTOD != "" && w.fromTOD > w.toTOD: // wraps midnight
			return tod >= w.fromTOD || tod <= w.toTOD
		default:
			return (w.fromTOD == "" || tod >= w.fromTOD) && (w.toTOD == "" || tod <= w.toTOD)
		}
	}
	return (!w.hasStart || ms >= w.start) && (!w.hasEnd || ms <= w.end)
}

// spanMatches is the date/time rule (lead's ruling, 2026-10-05): a visit
// matches when the person was on site at ANY point in the window — time in
// at or before the window's end AND (time out at or after its start, or
// still on site) — and a trip when it was on the road at any point (dispatch
// ≤ end AND (arrival ≥ start, or still open)). from is the opening instant;
// to is the closing one, open=true when the visit or run is still open (it
// then runs to now). An opening instant that cannot be read matches nothing;
// a closing one that cannot be read leaves only the opening instant to test.
func (w *timeWindow) spanMatches(from, to domain.RecordedInstant, open bool, nowMS int64) bool {
	if !from.OK || from.Label == "" {
		return false
	}
	end := nowMS
	switch {
	case open:
		if end < from.MS {
			end = from.MS
		}
	case to.OK && to.Label != "" && to.MS >= from.MS:
		end = to.MS
	default:
		return w.contains(from.MS)
	}
	return w.overlaps(from.MS, end)
}

// overlaps: does [a, b] share any instant with the window?
func (w *timeWindow) overlaps(a, b int64) bool {
	if !w.daily {
		return (!w.hasEnd || a <= w.end) && (!w.hasStart || b >= w.start)
	}
	if w.contains(a) || w.contains(b) {
		return true
	}
	if b-a >= (24 * time.Hour).Milliseconds() {
		return true // spans a whole day, so every time of day
	}
	// The window's own start on each day the span touches (or the day's
	// start, when the window has no From) lying inside [a, b].
	tod := func(s string) time.Duration {
		if s == "" {
			return 0
		}
		t, _ := time.Parse("15:04", s)
		return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute
	}
	at := time.UnixMilli(a).In(time.Local)
	day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.Local)
	for d := day; d.UnixMilli() <= b; d = d.AddDate(0, 0, 1) {
		ws := d.Add(tod(w.fromTOD)).UnixMilli()
		if ws >= a && ws <= b {
			return true
		}
	}
	return false
}

// compile checks the form and builds the filter. Problems are sentences for
// the officer; with any problem nothing is searched.
func (f searchForm) compile() (searchFilter, []string) {
	var out searchFilter
	var problems []string
	for _, w := range strings.Fields(strings.ReplaceAll(strings.ToLower(f.Q), ",", " ")) {
		out.words = append(out.words, w)
	}
	out.last, out.first, out.company = searchKey(f.Last), searchKey(f.First), searchKey(f.Company)
	if f.Phone != "" {
		if out.phone = digitsOf(f.Phone); out.phone == "" {
			problems = append(problems, "Phone number: type some of its digits — for example the last four.")
		}
	}
	if f.Truck != "" {
		if out.truck = truckKey(f.Truck); out.truck == "" {
			problems = append(problems, "Truck number: type the number, for example TRK-12.")
		}
	}
	out.site = f.Site
	w, wp := f.window()
	out.window = w
	return out, append(problems, wp...)
}

func (f searchForm) window() (*timeWindow, []string) {
	if f.DateFrom == "" && f.DateTo == "" && f.TimeFrom == "" && f.TimeTo == "" {
		return nil, nil
	}
	var problems []string
	parseDate := func(label, v string) (time.Time, bool) {
		if v == "" {
			return time.Time{}, false
		}
		d, err := time.ParseInLocation("2006-01-02", v, time.Local)
		if err != nil {
			problems = append(problems, label+": use a date like 2026-10-05.")
			return time.Time{}, false
		}
		return d, true
	}
	parseTOD := func(label, v string) (time.Duration, bool) {
		if v == "" {
			return 0, false
		}
		t, err := time.Parse("15:04", v)
		if err != nil {
			problems = append(problems, label+": use a 24-hour time like 07:30.")
			return 0, false
		}
		return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute, true
	}
	df, okDF := parseDate("Date from", f.DateFrom)
	dt, okDT := parseDate("Date to", f.DateTo)
	tf, okTF := parseTOD("Time from", f.TimeFrom)
	tt, okTT := parseTOD("Time to", f.TimeTo)
	if len(problems) > 0 {
		return nil, problems
	}
	w := &timeWindow{}
	if !okDF && !okDT {
		// Times only: the same hours on every day.
		w.daily = true
		if okTF {
			w.fromTOD = f.TimeFrom
		}
		if okTT {
			w.toTOD = f.TimeTo
		}
		switch {
		case okTF && okTT:
			w.Text = fmt.Sprintf("between %s and %s on any day", f.TimeFrom, f.TimeTo)
		case okTF:
			w.Text = fmt.Sprintf("at or after %s on any day", f.TimeFrom)
		default:
			w.Text = fmt.Sprintf("at or before %s on any day", f.TimeTo)
		}
		return w, nil
	}
	// A time with no date on its own side takes the other side's date: one
	// date and two times is "that day, between those hours".
	if !okDF && okTF {
		df, okDF = dt, true
	}
	if !okDT && okTT {
		dt, okDT = df, true
	}
	if okDF {
		start := df.Add(tf)
		w.start, w.hasStart = start.UnixMilli(), true
	}
	if okDT {
		end := dt.Add(24*time.Hour - time.Millisecond)
		if okTT {
			end = dt.Add(tt + time.Minute - time.Millisecond)
		}
		w.end, w.hasEnd = end.UnixMilli(), true
	}
	if w.hasStart && w.hasEnd && w.start > w.end {
		return nil, []string{"The From date and time is after the To date and time."}
	}
	stamp := func(ms int64) string { return time.UnixMilli(ms).In(time.Local).Format("2006-01-02 15:04") }
	switch {
	case w.hasStart && w.hasEnd:
		w.Text = fmt.Sprintf("between %s and %s", stamp(w.start), stamp(w.end))
	case w.hasStart:
		w.Text = fmt.Sprintf("at or after %s", stamp(w.start))
	default:
		w.Text = fmt.Sprintf("at or before %s", stamp(w.end))
	}
	return w, nil
}

// --- page data ---

type searchData struct {
	Cfg            *config.Config
	SiteID         string
	Banner         []deadline.Alert
	BannerErr      string
	AlertsReadOnly bool
	// DispatchPast (FIX14, dispatch host only): other posts' late runs,
	// merged into Banner and into the overdue status (dispatch_allsites.go).
	DispatchPast dispatchPastView

	// Dispatch: the request came in on the read-only dispatch host — no
	// result links into the operator console.
	Dispatch bool
	Form     searchForm
	Sites    []facetOption // the main search's Site select: every post, by display name
	Searched bool
	Problems []string
	Err      string
	Window   string
	Elapsed  string

	People     []searchPersonRow
	PeopleN    int
	PeopleMore string // "show all" URL when the group was cut short
	Visits     []searchVisitRow
	VisitsN    int
	VisitsMore string
	Trips      []searchTripRow
	TripsN     int
	TripsMore  string

	// The "Narrow results" panel (FIX12, search_refine.go).
	Refine searchRefine
	Narrow searchNarrow

	// CSV: each group's "Download CSV" link — this exact search, filters
	// and "show all" included (FIX16, search_csv.go).
	CSV searchCSVHrefs
}

type searchPersonRow struct {
	PersonID   string
	Name       string
	Company    string
	Trade      string
	Phone      string // as recorded
	Where      string // on site now / last seen, in words
	WhereSites []string
	EnrolledAt string // site id
	Retired    bool
	sortKey    string
	keys       searchRowKeys
}

type searchVisitRow struct {
	PersonID string
	Person   string
	Company  string
	Site     string // site id
	In       string
	InHand   string // "entered by hand" / "corrected by hand" / ""
	Out      string
	OutHand  string
	Open     bool
	Vehicle  string
	TripID   string // the run it arrived on, when that is a run
	inMS     int64
	keys     searchRowKeys
}

type searchTripRow struct {
	TripID      string
	Vehicle     string
	Origin      string // site id
	Destination string // words
	DestSite    string // site id, "" when not a post
	Dispatched  string
	DispHand    string
	Arrived     string
	ArrHand     string
	Driver      string
	Aboard      []string
	Status      string
	dispMS      int64
	keys        searchRowKeys
	personIDs   []string // canonical ids of everyone aboard
}

// personInfo is one roster person, normalised once for matching.
type personInfo struct {
	p                                         domain.Person
	company, trade                            string
	nameK, lastK, firstK, companyK, tradeK, d string // d: phone digits
}

func newPersonInfo(p domain.Person, companyName, tradeName map[string]string) *personInfo {
	pi := &personInfo{p: p}
	if p.CompanyID != nil {
		pi.company = companyName[*p.CompanyID]
	}
	if p.TradeID != nil {
		pi.trade = tradeName[*p.TradeID]
	}
	// D-48: the name parts are separate facts. The last/first filters read
	// the STORED parts only; nothing derives a part from the whole name.
	// ⟦was: splitName — "Last, First", else the final word — which parsed
	// the name (TestW5aNothingParsesAName)⟧
	pi.nameK = searchKey(p.Name)
	if p.LastName != nil {
		pi.lastK = searchKey(*p.LastName)
	}
	if p.FirstName != nil {
		pi.firstK = searchKey(*p.FirstName)
	}
	pi.companyK, pi.tradeK = searchKey(pi.company), searchKey(pi.trade)
	if p.Phone != nil {
		pi.d = digitsOf(*p.Phone)
	}
	return pi
}

// snapshotPerson stands in for a person this post has no roster row for:
// the name as the visit recorded it.
func snapshotPerson(name string) *personInfo {
	return &personInfo{p: domain.Person{Name: name}, nameK: searchKey(name)}
}

// matches applies the person filters; extraCompanyK is a further company
// the record names (a visit's company as recorded at check-in).
//
// A last or first filter matches the stored part when the person has one;
// a person with no such part on record (an older roster entry, or a visit's
// name snapshot) is matched on the whole name instead — the typed text
// against the name as written, never a part guessed out of it (D-48).
func (pi *personInfo) matches(f searchFilter, extraCompanyK string) bool {
	if f.last != "" && !partHit(pi.lastK, pi.nameK, f.last) {
		return false
	}
	if f.first != "" && !partHit(pi.firstK, pi.nameK, f.first) {
		return false
	}
	if f.company != "" && !strings.Contains(pi.companyK, f.company) && !strings.Contains(extraCompanyK, f.company) {
		return false
	}
	if f.phone != "" && !strings.Contains(pi.d, f.phone) {
		return false
	}
	return true
}

// partHit matches a name-part filter: the stored part when there is one,
// else the whole name.
func partHit(partK, nameK, want string) bool {
	if partK != "" {
		return strings.Contains(partK, want)
	}
	return strings.Contains(nameK, want)
}

// wordHit: does one free-text word match this person?
func (pi *personInfo) wordHit(w string) bool {
	if strings.Contains(pi.nameK, w) || strings.Contains(pi.companyK, w) || strings.Contains(pi.tradeK, w) {
		return true
	}
	return phoneWordHit(w, pi.d)
}

// phoneWordHit: a word made of digits (and phone punctuation), at least
// three digits long, matches a phone containing those digits.
func phoneWordHit(w, phoneDigits string) bool {
	if phoneDigits == "" {
		return false
	}
	d := digitsOf(w)
	if len(d) < 3 || strings.Trim(w, "0123456789-().+ ") != "" {
		return false
	}
	return strings.Contains(phoneDigits, d)
}

func textHit(w string, texts ...string) bool {
	for _, t := range texts {
		if t != "" && strings.Contains(t, w) {
			return true
		}
	}
	return false
}

func truckHit(w string, vehicles ...string) bool {
	k := truckKey(w)
	if k == "" {
		return false
	}
	for _, v := range vehicles {
		if v != "" && strings.Contains(truckKey(v), k) {
			return true
		}
	}
	return false
}

// --- the handler ---

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	data := s.buildSearch(r)
	s.renderPage(w, "search", "search.html", data)
}

// buildSearch is the whole page's data for this request — what /search
// renders and what /search.csv (search_csv.go) writes, so the download can
// never hold a row the page does not.
func (s *Server) buildSearch(r *http.Request) searchData {
	q := r.URL.Query()
	get := func(k string) string { return strings.TrimSpace(q.Get(k)) }
	data := searchData{Cfg: s.Cfg, SiteID: s.Cfg.SiteID, Dispatch: dispatchHost(r.Host)}
	data.AlertsReadOnly = data.Dispatch
	data.Banner, data.BannerErr = s.banner()
	if data.Dispatch {
		var err error
		if data.DispatchPast, err = s.dispatchPastAllowance(time.UnixMilli(s.clockReading().WallMS)); err != nil {
			log.Printf("search: all-sites overdue: %v", err)
		}
		data.Banner = append(data.Banner, data.DispatchPast.Rows...)
		sortAlertsWorstFirst(data.Banner)
	}
	data.Form = searchForm{Q: get("q"), Last: get("last"), First: get("first"), Company: get("company"),
		Phone: get("phone"), Truck: get("truck"), DateFrom: get("date_from"), DateTo: get("date_to"),
		TimeFrom: get("time_from"), TimeTo: get("time_to"), Site: get("site")}
	data.Refine = parseSearchRefine(q)
	known := s.searchSiteIDs()
	for _, id := range known {
		data.Sites = append(data.Sites, facetOption{Value: id, Label: s.siteFilterLabel(id), Checked: id == data.Form.Site})
	}
	if !data.Form.active() {
		return data
	}
	data.Searched = true
	f, problems := data.Form.compile()
	if f.site != "" && !contains(known, f.site) {
		problems = append(problems, "Site: choose a post from the list.")
	}
	if len(problems) > 0 {
		data.Problems = problems
		return data
	}
	if f.window != nil {
		data.Window = f.window.Text
	}
	began := time.Now()
	if err := s.runSearch(&data, f, get("all")); err != nil {
		// Loud, never a partial result list: an empty group would read as
		// "nobody matched".
		log.Printf("search: %v", err)
		data.Err = refusalText(err)
		data.People, data.Visits, data.Trips = nil, nil, nil
		data.Narrow = searchNarrow{}
	}
	data.Elapsed = time.Since(began).Round(time.Millisecond).String()
	if data.Err == "" {
		data.CSV = searchCSVLinks(q)
	}
	return data
}

// stamp renders an instant the way the analytics pages do: the post's
// local time, labelled with the post whose clock recorded it.
func (s *Server) searchStamp(in domain.RecordedInstant) (text, hand string) {
	if in.Label == "" {
		return "", ""
	}
	if !in.OK {
		return "time cannot be shown — its correction record is incomplete here", ""
	}
	text = fmt.Sprintf("%s (%s's clock)", time.UnixMilli(in.MS).In(time.Local).Format("2006-01-02 15:04"), s.Cfg.SiteName(in.SiteID))
	switch {
	case in.Corrected():
		hand = "corrected by hand"
	case in.ByHand():
		hand = "entered by hand"
	}
	return text, hand
}

func (s *Server) runSearch(data *searchData, f searchFilter, showAll string) error {
	nowMS := s.clockReading().WallMS // "still on site" / "still on the road" run to now
	overdue, err := s.overdueTrips()
	if err != nil {
		return err
	}
	ackedOverdue := map[string][]string{}
	for _, a := range data.DispatchPast.Rows {
		overdue[a.TripID] = true // FIX14: judged by the origin's recorded time
	}
	for _, a := range data.DispatchPast.AckedOverdue {
		for _, id := range a.By {
			ackedOverdue[a.TripID] = append(ackedOverdue[a.TripID], s.Cfg.SiteName(id))
		}
	}
	persons, err := domain.Persons(s.Store)
	if err != nil {
		return err
	}
	companies, err := domain.Companies(s.Store)
	if err != nil {
		return err
	}
	trades, err := domain.Trades(s.Store)
	if err != nil {
		return err
	}
	companyName := make(map[string]string, len(companies))
	for _, c := range companies {
		companyName[c.CompanyID] = c.Name
	}
	tradeName := make(map[string]string, len(trades))
	for _, t := range trades {
		tradeName[t.TradeID] = t.Name
	}
	merge, err := domain.LoadPersonMergeMap(s.Store)
	if err != nil {
		return err
	}
	people := make(map[string]*personInfo, len(persons))
	for _, p := range persons {
		people[p.PersonID] = newPersonInfo(p, companyName, tradeName)
	}
	// person resolves an id to its canonical roster record (a merged
	// duplicate reads as the record it was merged into).
	person := func(id, snapshot string) *personInfo {
		if pi := people[merge.Resolve(id)]; pi != nil {
			return pi
		}
		if pi := people[id]; pi != nil {
			return pi
		}
		if snapshot == "" {
			snapshot = id
		}
		return snapshotPerson(snapshot)
	}

	// --- trips: every run, composed ---
	instants, err := domain.LoadTripInstants(s.Store)
	if err != nil {
		return err
	}
	live, err := domain.NewLiveVehicles(s.Store)
	if err != nil {
		return err
	}
	destDisplay := map[string]string{}
	for _, d := range s.Cfg.DestinationList() {
		destDisplay[d.ID] = d.DisplayName
	}
	// vehicleOf: a trip's truck as a visit row shows it — the composed run's
	// when it is a run, else the number recorded at the check-in.
	vehicleOf := make(map[string]string, len(instants.Vehicle))
	for id, v := range instants.Vehicle {
		vehicleOf[id] = v
	}
	var tripRows []searchTripRow
	matchedPeople := map[string]bool{} // canonical ids on a matching visit/trip
	// Only the RUNS are composed (a point read each); the check-in halves
	// are the visits below.
	for tripID := range instants.Run {
		c, err := domain.EffectiveTrip(s.Store, tripID)
		if err != nil {
			return err
		}
		if c == nil {
			continue
		}
		t := c.Trip
		var state domain.TripState
		if ts, err := domain.TripStateByID(s.Store, tripID); err != nil {
			return err
		} else if ts != nil {
			state = *ts
		}
		vehicle := tripVehicleDisplay(t.VehicleIsUnidentified(), t.VehicleEntered)
		if state.State == domain.TripStateOpen {
			if vehicle, _, err = live.Compose(*c); err != nil {
				return err
			}
		}
		vehicleOf[t.TripID] = vehicle
		row := searchTripRow{TripID: t.TripID, Vehicle: vehicle, Origin: t.OriginSite}
		switch {
		case c.RoadWork:
			row.Destination = domain.RoadWorkDestinationText
		case t.DestinationSite != nil:
			row.Destination, row.DestSite = s.Cfg.SiteName(*t.DestinationSite), *t.DestinationSite
		case t.DestinationUncontrolled != nil:
			name := *t.DestinationUncontrolled
			if dn, ok := destDisplay[name]; ok {
				name = dn
			} else {
				name = s.padName(name)
			}
			row.Destination = name + " (uncontrolled — no timer)"
		}
		disp := instants.Dispatch[t.TripID]
		disp.SiteID = t.OriginSite
		var arr domain.RecordedInstant
		if state.ArrivalID != nil {
			arr = instants.Arrival[*state.ArrivalID]
		}
		// Turned back: the arrival that closed the run was recorded at its
		// own origin (domain.TripsReturnedToOrigin's rule, read off the
		// arrival this replay already holds).
		returned := state.State == domain.TripStateArrived && arr.SiteID != "" && arr.SiteID == t.OriginSite
		row.Dispatched, row.DispHand = s.searchStamp(disp)
		row.Arrived, row.ArrHand = s.searchStamp(arr)
		if disp.OK {
			row.dispMS = disp.MS
		}
		switch state.State {
		case domain.TripStateOpen:
			row.Status = "on the road"
			if c.RoadWork {
				row.Status = "out working on the road"
			}
			if overdue[t.TripID] {
				row.Status = "overdue — " + row.Status + " past its deadline"
			} else if by := ackedOverdue[t.TripID]; len(by) > 0 {
				// FIX14 (dispatch_allsites.go): an end's red ack is the proof.
				row.Status = row.Status + " — acknowledged as OVERDUE at " + strings.Join(by, ", ")
			}
		case domain.TripStateArrived:
			row.Status = "arrived"
			if returned {
				row.Status = "turned back — returned to " + s.Cfg.SiteName(t.OriginSite)
			} else if c.RoadWork {
				row.Status = "back from the road"
			}
		case "":
			row.Status = "state not yet known here"
		default:
			row.Status = state.State
		}

		// Who: the EFFECTIVE driver and people aboard (corrections applied).
		aboard := c.OccupantIDs
		ids := append([]string{t.DriverPersonID}, aboard...)
		var who []*personInfo
		seen := map[string]bool{}
		for _, id := range ids {
			if id == "" {
				continue
			}
			pi := person(id, "")
			key := pi.p.PersonID
			if key == "" {
				key = id
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			who = append(who, pi)
		}
		if t.DriverPersonID != "" {
			row.Driver = person(t.DriverPersonID, "").p.Name
		}
		driverKey := ""
		if t.DriverPersonID != "" {
			driverKey = person(t.DriverPersonID, "").p.PersonID
		}
		for _, pi := range who {
			if pi.p.PersonID != "" && pi.p.PersonID == driverKey {
				continue
			}
			row.Aboard = append(row.Aboard, pi.p.Name)
		}
		sort.Strings(row.Aboard)

		// Filters.
		if f.site != "" && t.OriginSite != f.site && row.DestSite != f.site {
			continue
		}
		if f.truck != "" && !strings.Contains(truckKey(vehicle), f.truck) && !(t.VehicleEntered != nil && strings.Contains(truckKey(*t.VehicleEntered), f.truck)) {
			continue
		}
		if f.window != nil && !f.window.spanMatches(disp, arr, state.State == domain.TripStateOpen, nowMS) {
			continue
		}
		var hits []*personInfo
		for _, pi := range who {
			if pi.matches(f, "") {
				hits = append(hits, pi)
			}
		}
		if f.personFiltered() && len(hits) == 0 {
			continue
		}
		postK := searchKey(s.Cfg.SiteName(t.OriginSite) + " " + row.Destination)
		ok := true
		for _, w := range f.words {
			hit := truckHit(w, vehicle) || textHit(w, postK) || (t.VehicleEntered != nil && truckHit(w, *t.VehicleEntered))
			for _, pi := range who {
				if hit {
					break
				}
				hit = pi.wordHit(w)
			}
			if !hit {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		row.keys = searchRowKeys{status: tripStatusKey(state.State, returned, overdue[t.TripID]),
			hand: row.DispHand != "" || row.ArrHand != ""}
		row.keys.addSite(t.OriginSite)
		row.keys.addSite(row.DestSite)
		for _, pi := range who {
			row.keys.addCompany(searchKey(pi.company), pi.company)
			if pi.p.PersonID != "" {
				row.personIDs = append(row.personIDs, pi.p.PersonID)
			}
		}
		tripRows = append(tripRows, row)
		if f.eventFiltered() {
			for _, pi := range hits {
				if pi.p.PersonID != "" {
					matchedPeople[pi.p.PersonID] = true
				}
			}
		}
	}

	// --- visits: the visit report (effective in/out instants) ---
	recs, _, err := domain.VisitReport(s.Store, nil)
	if err != nil {
		return err
	}
	var visitRows []searchVisitRow
	openAt := map[string][]string{} // canonical person -> sites on site now
	type lastSeen struct {
		site string
		ms   int64
		out  bool
	}
	seenLast := map[string]lastSeen{}
	for _, rec := range recs {
		pi := person(rec.VisitPersonID, rec.PersonName)
		pid := pi.p.PersonID
		in, out := rec.ArrivalInstant(), rec.CloseInstant()
		if pid != "" {
			if rec.ClosedBy == domain.VisitStillOpen {
				openAt[pid] = append(openAt[pid], rec.SiteID)
			} else {
				ms, isOut := in.MS, false
				if out.OK {
					ms, isOut = out.MS, true
				}
				if prev, ok := seenLast[pid]; !ok || ms > prev.ms {
					seenLast[pid] = lastSeen{site: rec.SiteID, ms: ms, out: isOut}
				}
			}
		}
		// Company and trade as recorded at check-in; the truck as entered at
		// that gate, else the one its trip went out on.
		asRec := instants.Visit[rec.VisitID]
		company, trade, vehicle, runID := asRec.Company, asRec.Trade, asRec.Vehicle, ""
		if company == "" {
			company = pi.company
		}
		if rec.TripRef != "" {
			if vehicle == "" {
				vehicle = vehicleOf[rec.TripRef]
			}
			if instants.Run[rec.TripRef] {
				runID = rec.TripRef
			}
		}
		if f.site != "" && rec.SiteID != f.site {
			continue
		}
		if f.truck != "" && !strings.Contains(truckKey(vehicle), f.truck) {
			continue
		}
		if f.window != nil && !f.window.spanMatches(in, out, rec.ClosedBy == domain.VisitStillOpen, nowMS) {
			continue
		}
		if !pi.matches(f, searchKey(company)) {
			continue
		}
		siteK := searchKey(s.Cfg.SiteName(rec.SiteID))
		companyK, tradeK := searchKey(company), searchKey(trade)
		ok := true
		for _, w := range f.words {
			if !(pi.wordHit(w) || textHit(w, siteK, companyK, tradeK, searchKey(rec.PersonName)) || truckHit(w, vehicle)) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		row := searchVisitRow{PersonID: pid, Person: pi.p.Name, Company: company, Site: rec.SiteID,
			Open: rec.ClosedBy == domain.VisitStillOpen, Vehicle: vehicle, TripID: runID}
		if row.Person == "" {
			row.Person = rec.PersonName
		}
		row.In, row.InHand = s.searchStamp(in)
		if in.OK {
			row.inMS = in.MS
		}
		if row.Open {
			row.Out = "still on site"
		} else {
			row.Out, row.OutHand = s.searchStamp(out)
			if row.Out == "" {
				row.Out = "closed — time not on record here"
			}
		}
		row.keys = searchRowKeys{status: "visit-closed", hand: row.InHand != "" || row.OutHand != ""}
		if row.Open {
			row.keys.status = "visit-open"
		}
		row.keys.addSite(rec.SiteID)
		row.keys.addCompany(searchKey(company), company)
		visitRows = append(visitRows, row)
		if f.eventFiltered() && pid != "" {
			matchedPeople[pid] = true
		}
	}

	// --- people ---
	var personRows []searchPersonRow
	if f.personFiltered() || len(f.words) > 0 || f.eventFiltered() || f.site != "" {
		for _, p := range persons {
			if p.SameAs != nil {
				continue // merged: its record is the one it was merged into
			}
			pi := people[p.PersonID]
			if !pi.matches(f, "") {
				continue
			}
			ok := true
			for _, w := range f.words {
				if !pi.wordHit(w) {
					ok = false
					break
				}
			}
			if !ok || (f.eventFiltered() && !matchedPeople[p.PersonID]) {
				continue
			}
			row := searchPersonRow{PersonID: p.PersonID, Name: p.Name, Company: pi.company, Trade: pi.trade,
				EnrolledAt: p.SiteID, Retired: p.IsRetired(), sortKey: pi.nameK}
			if p.Phone != nil {
				row.Phone = strings.TrimSpace(*p.Phone)
			}
			if sites := openAt[p.PersonID]; len(sites) > 0 {
				sort.Strings(sites)
				row.WhereSites = sites
				names := make([]string, len(sites))
				for i, id := range sites {
					names[i] = s.Cfg.SiteName(id)
				}
				row.Where = "on site now at " + strings.Join(names, ", ")
			} else if ls, ok := seenLast[p.PersonID]; ok {
				row.WhereSites = []string{ls.site}
				verb := "signed in"
				if ls.out {
					verb = "left"
				}
				row.Where = fmt.Sprintf("last seen at %s — %s %s (%s's clock)", s.Cfg.SiteName(ls.site), verb,
					time.UnixMilli(ls.ms).In(time.Local).Format("2006-01-02 15:04"), s.Cfg.SiteName(ls.site))
			} else {
				row.Where = "no visit on record"
			}
			// Site: on site there now, or last seen there.
			if f.site != "" && !contains(row.WhereSites, f.site) {
				continue
			}
			row.keys = searchRowKeys{status: "person-offsite"}
			if len(openAt[p.PersonID]) > 0 {
				row.keys.status = "person-onsite"
			}
			for _, id := range row.WhereSites {
				row.keys.addSite(id)
			}
			row.keys.addCompany(pi.companyK, pi.company)
			personRows = append(personRows, row)
		}
	}

	sort.SliceStable(personRows, func(i, j int) bool {
		if personRows[i].sortKey != personRows[j].sortKey {
			return personRows[i].sortKey < personRows[j].sortKey
		}
		return personRows[i].PersonID < personRows[j].PersonID
	})
	// Newest first by the recorded instant — DISPLAY order only; the record's
	// own order is each post's stream (D-90).
	sort.SliceStable(visitRows, func(i, j int) bool { return visitRows[i].inMS > visitRows[j].inMS })
	sort.Slice(tripRows, func(i, j int) bool {
		if tripRows[i].dispMS != tripRows[j].dispMS {
			return tripRows[i].dispMS > tripRows[j].dispMS
		}
		return tripRows[i].TripID < tripRows[j].TripID
	})

	// "Entered by hand": a person qualifies through a found visit or trip
	// with a hand-entered or corrected time.
	byHand := map[string]bool{}
	for _, r := range visitRows {
		if r.keys.hand && r.PersonID != "" {
			byHand[r.PersonID] = true
		}
	}
	for _, r := range tripRows {
		if r.keys.hand {
			for _, id := range r.personIDs {
				byHand[id] = true
			}
		}
	}
	for i := range personRows {
		personRows[i].keys.hand = byHand[personRows[i].PersonID]
	}
	personRows, visitRows, tripRows = s.narrowSearch(data, personRows, visitRows, tripRows)

	more := func(group string) string {
		v := data.Form.values()
		data.Refine.addTo(v)
		v.Set("all", group)
		return "/search?" + v.Encode() + "#results-" + group
	}
	limit := func(group string) int {
		if showAll == group {
			return searchCapAll
		}
		return searchCap
	}
	data.PeopleN, data.VisitsN, data.TripsN = len(personRows), len(visitRows), len(tripRows)
	if n := limit("people"); len(personRows) > n {
		personRows = personRows[:n]
		if n == searchCap {
			data.PeopleMore = more("people")
		}
	}
	if n := limit("visits"); len(visitRows) > n {
		visitRows = visitRows[:n]
		if n == searchCap {
			data.VisitsMore = more("visits")
		}
	}
	if n := limit("trips"); len(tripRows) > n {
		tripRows = tripRows[:n]
		if n == searchCap {
			data.TripsMore = more("trips")
		}
	}
	data.People, data.Visits, data.Trips = personRows, visitRows, tripRows
	return nil
}
