// history.go — the three history reports James approved 2026-10-06 (FIX16f):
//
//	GET /history/overdue   trips that went past their deadline: how late,
//	                       who acknowledged it, when and from which post, and
//	                       how it ended
//	GET /history/signouts  missed sign-outs, following the person and the
//	                       company, per week, with repeat offenders
//	GET /history/signal    which sites this post lost signal with, when, and
//	                       for how long (signal_history.go)
//
// each with a .csv twin carrying the same rows (csvdownload.go).
//
// SCOPE BY HOST. On the dispatch host every report covers all sites, with a
// site filter and, for sign-outs, a per-site breakdown. On a post's own host
// the overdue history is that post's own trips (sent from it or bound for
// it) and the sign-outs are that site's only. The signal history is always
// the SERVING post's own record: which peers it lost contact with.
//
// READ-ONLY: GET only, nothing authored, behind the login (the reports name
// people). Allowed on the dispatch host (dispatch_surface.go); there the
// pages link only to the other read pages.
package web

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/humantime"
)

func (s *Server) registerHistoryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /history/overdue", s.requireLogin(s.handleOverdueHistory))
	mux.HandleFunc("GET /history/overdue.csv", s.requireLogin(s.handleOverdueHistory))
	mux.HandleFunc("GET /history/signouts", s.requireLogin(s.handleSignoutHistory))
	mux.HandleFunc("GET /history/signouts.csv", s.requireLogin(s.handleSignoutHistory))
	mux.HandleFunc("GET /history/signal", s.requireLogin(s.handleSignalHistory))
	mux.HandleFunc("GET /history/signal.csv", s.requireLogin(s.handleSignalHistory))
}

// historyFilter is the form every report shares: a date range (local days,
// both ends inclusive), a site and — for the overdue history — a route.
type historyFilter struct {
	From, To, Site, Route string
	fromMS, toMS          int64
	Problems              []string
}

// Query is the filter as a query string ("" when nothing is set), for the
// CSV link, so the download is the rows on screen.
func (f historyFilter) Query() string {
	v := url.Values{}
	for k, val := range map[string]string{"from": f.From, "to": f.To, "site": f.Site, "route": f.Route} {
		if val != "" {
			v.Set(k, val)
		}
	}
	if len(v) == 0 {
		return ""
	}
	return "?" + v.Encode()
}

// Active reports whether anything narrows the report.
func (f historyFilter) Active() bool {
	return f.From != "" || f.To != "" || f.Site != "" || f.Route != ""
}

func parseHistoryFilter(r *http.Request, sites []string) historyFilter {
	q := r.URL.Query()
	get := func(k string) string { return strings.TrimSpace(q.Get(k)) }
	f := historyFilter{From: get("from"), To: get("to"), Site: get("site"), Route: get("route")}
	day := func(label, v string) (time.Time, bool) {
		if v == "" {
			return time.Time{}, false
		}
		t, err := time.ParseInLocation("2006-01-02", v, time.Local)
		if err != nil {
			f.Problems = append(f.Problems, label+": write the date as YYYY-MM-DD.")
			return time.Time{}, false
		}
		return t, true
	}
	if t, ok := day("From", f.From); ok {
		f.fromMS = t.UnixMilli()
	}
	if t, ok := day("To", f.To); ok {
		f.toMS = t.AddDate(0, 0, 1).UnixMilli()
	}
	if f.fromMS != 0 && f.toMS != 0 && f.toMS <= f.fromMS {
		f.Problems = append(f.Problems, "To is before From.")
	}
	if f.Site != "" && !contains(sites, f.Site) {
		f.Problems = append(f.Problems, "Site: choose a site from the list.")
	}
	return f
}

// inRange reports whether an instant falls in the filter's days.
func (f historyFilter) inRange(ms int64, ok bool) bool {
	if f.fromMS == 0 && f.toMS == 0 {
		return true
	}
	if !ok {
		return false
	}
	return (f.fromMS == 0 || ms >= f.fromMS) && (f.toMS == 0 || ms < f.toMS)
}

// historyLink is one entry of the reports' shared nav.
type historyLink struct {
	Href, Title string
	Current     bool
}

// historyPage is what every history page carries besides its own rows.
type historyPage struct {
	Cfg            *config.Config
	SiteID         string
	Banner         []deadline.Alert
	BannerErr      string
	AlertsReadOnly bool
	Dispatch       bool
	// Scope says, in one sentence, whose records the report covers.
	Scope      string
	RenderedAt string
	Filter     historyFilter
	Sites      []facetOption
	CSVHref    string
	Links      []historyLink
	Err        string
}

var historyReports = []historyLink{
	{Href: "/history/overdue", Title: "Overdue history"},
	{Href: "/history/signouts", Title: "Missed sign-outs"},
	{Href: "/history/signal", Title: "Lost-signal history"},
}

// historyBase fills the shared part. sites are the filter's choices.
func (s *Server) historyBase(r *http.Request, href string, sites []string, now time.Time) historyPage {
	p := historyPage{Cfg: s.Cfg, SiteID: s.Cfg.SiteID, Dispatch: dispatchHost(r.Host),
		RenderedAt: now.In(time.Local).Format("2006-01-02 15:04")}
	p.AlertsReadOnly = p.Dispatch
	p.Banner, p.BannerErr = s.banner()
	if p.Dispatch {
		past, err := s.dispatchPastAllowance(now)
		if err != nil {
			log.Printf("history: all-sites overdue: %v", err)
		}
		p.Banner = append(p.Banner, past.Rows...)
		sortAlertsWorstFirst(p.Banner)
	}
	p.Filter = parseHistoryFilter(r, sites)
	for _, id := range sites {
		p.Sites = append(p.Sites, facetOption{Value: id, Label: s.siteFilterLabel(id), Checked: id == p.Filter.Site})
	}
	p.CSVHref = href + ".csv" + p.Filter.Query()
	for _, l := range historyReports {
		l.Current = l.Href == href
		p.Links = append(p.Links, l)
	}
	return p
}

// allSiteIDs is this post, every configured peer and every retired one
// ([[former_peers]], FIX17b), sorted by id, plus any extra ids seen in the
// records.
func (s *Server) allSiteIDs(extra ...string) []string {
	seen := map[string]bool{s.Cfg.SiteID: true}
	out := []string{s.Cfg.SiteID}
	for _, p := range s.Cfg.Peers {
		if !seen[p.SiteID] {
			seen[p.SiteID] = true
			out = append(out, p.SiteID)
		}
	}
	for _, p := range s.Cfg.FormerPeers {
		if !seen[p.SiteID] {
			seen[p.SiteID] = true
			out = append(out, p.SiteID)
		}
	}
	for _, id := range extra {
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// historyWhen renders an instant on this post's local clock.
func historyWhen(ms int64) string {
	return time.UnixMilli(ms).In(time.Local).Format("2006-01-02 15:04")
}

// spanText is a duration as hours and minutes ("2 hours 5 minutes", "40
// minutes", "12 seconds").
func spanText(d time.Duration) string {
	if d < time.Minute {
		return humantime.Span(d)
	}
	return hoursMinutes(d.Truncate(time.Minute))
}

// csvRefused answers a download that cannot be the page's rows: 400 for a
// filter that does not parse (nothing was built), 404 when this post keeps
// no such record, 500 when the read failed. ok=true: go ahead.
func csvRefused(w http.ResponseWriter, what, errText string, notRecorded bool, problems []string) (ok bool) {
	switch {
	case len(problems) > 0:
		http.Error(w, what+" was not built — "+strings.Join(problems, " ")+" No file was produced.", http.StatusBadRequest)
	case notRecorded:
		http.Error(w, what+": this post keeps no such record yet. No file was produced.", http.StatusNotFound)
	case errText != "":
		http.Error(w, what+" could not be built — "+errText+". No file was produced.", http.StatusInternalServerError)
	default:
		return true
	}
	return false
}

// ---- overdue history ----

type overdueAckView struct {
	Text      string // "OVERDUE acknowledged by Officer Reyes at Ice Pad, 2026-10-05 14:32"
	Overdue   bool
	Retracted bool
}

type overdueRow struct {
	TripID   string
	Vehicle  string
	Route    string // "Ice Road → Ice Pad"
	RoadWork bool
	// Dispatched/Allowance/Due: when it left (the origin's clock), the
	// allowance, when it was due.
	Dispatched, Allowance, Due string
	// Late: how late, in words; LateMin: the same in whole minutes for the
	// CSV ("" when not measured).
	Late    string
	LateMin string
	Open    bool
	Acks    []overdueAckView
	Acked   bool // an effective OVERDUE ack stands
	Outcome string
	// csv columns
	origin, dest, outcomeCode, dispatchedUTC, dueUTC, endUTC, ackCSV string
}

type overdueHistoryData struct {
	historyPage
	Routes []facetOption
	Rows   []overdueRow
	// Considered: runs with a deadline in scope and range; the rows are
	// the ones that went past it.
	Considered int
	Counts     struct{ Arrived, TurnedBack, Back, Open, Cancelled, Acked int }
}

func routeKey(r domain.DeadlineRun) string {
	if r.RoadWork {
		return r.Origin + ">road"
	}
	return r.Origin + ">" + r.Destination
}

func (s *Server) routeLabel(r domain.DeadlineRun) string {
	if r.RoadWork {
		return s.Cfg.SiteName(r.Origin) + " → working on the road"
	}
	return s.Cfg.SiteName(r.Origin) + " → " + s.Cfg.SiteName(r.Destination)
}

var overdueOutcomeWords = map[string]string{
	domain.RunArrived:    "Arrived",
	domain.RunTurnedBack: "Turned back",
	domain.RunBack:       "Back from the road",
	domain.RunOpen:       "Still open",
	domain.RunCancelled:  "Cancelled",
}

// buildOverdueHistory is the page's rows and the download's.
func (s *Server) buildOverdueHistory(r *http.Request, now time.Time) overdueHistoryData {
	runs, err := domain.DeadlineRuns(s.Store)
	var acks map[string][]deadline.AckRecord
	if err == nil {
		acks, err = deadline.AckHistory(s.Store)
	}
	dispatch := dispatchHost(r.Host)
	own := s.Cfg.SiteID
	var extra []string
	scoped := runs[:0:0]
	for _, run := range runs {
		if !dispatch && run.Origin != own && run.Destination != own {
			continue // a post's own history: runs it sent or was sent
		}
		scoped = append(scoped, run)
		extra = append(extra, run.Origin, run.Destination)
	}
	sites := s.allSiteIDs(extra...)
	if !dispatch {
		sites = nil // no site filter on a post: its own trips are the scope
	}
	d := overdueHistoryData{historyPage: s.historyBase(r, "/history/overdue", sites, now)}
	if dispatch {
		d.Scope = "Every post's runs that reached " + s.Cfg.PostName() + "."
	} else {
		d.Scope = "Runs sent from or bound for " + s.Cfg.PostName() + "."
	}
	if err != nil {
		log.Printf("overdue history: %v", err)
		d.Err = refusalText(err)
		return d
	}
	routes := map[string]string{}
	for _, run := range scoped {
		routes[routeKey(run)] = s.routeLabel(run)
	}
	if d.Filter.Route != "" {
		if _, ok := routes[d.Filter.Route]; !ok {
			d.Filter.Problems = append(d.Filter.Problems, "Route: choose a route from the list.")
		}
	}
	for k, l := range routes {
		d.Routes = append(d.Routes, facetOption{Value: k, Label: l, Checked: k == d.Filter.Route})
	}
	sort.Slice(d.Routes, func(i, j int) bool { return d.Routes[i].Label < d.Routes[j].Label })
	if len(d.Filter.Problems) > 0 {
		return d
	}
	for _, run := range scoped {
		f := d.Filter
		if f.Site != "" && run.Origin != f.Site && run.Destination != f.Site {
			continue
		}
		if f.Route != "" && routeKey(run) != f.Route {
			continue
		}
		leftMS, leftOK := run.Left()
		if !f.inRange(leftMS, leftOK) {
			continue
		}
		d.Considered++
		row, include := s.overdueRowOf(run, acks[run.TripID], now)
		if !include {
			continue
		}
		switch run.Outcome {
		case domain.RunArrived:
			d.Counts.Arrived++
		case domain.RunTurnedBack:
			d.Counts.TurnedBack++
		case domain.RunBack:
			d.Counts.Back++
		case domain.RunOpen:
			d.Counts.Open++
		case domain.RunCancelled:
			d.Counts.Cancelled++
		}
		if row.Acked {
			d.Counts.Acked++
		}
		d.Rows = append(d.Rows, row)
	}
	// Newest dispatch first.
	sort.SliceStable(d.Rows, func(i, j int) bool { return d.Rows[i].dispatchedUTC > d.Rows[j].dispatchedUTC })
	return d
}

// overdueRowOf judges one run: it is in the history when its end (or now,
// while open) is past its due time, or when an end logged an OVERDUE
// acknowledgment for it — that end's own verdict.
func (s *Server) overdueRowOf(run domain.DeadlineRun, acks []deadline.AckRecord, now time.Time) (overdueRow, bool) {
	utc := func(ms int64, ok bool) string {
		if !ok {
			return ""
		}
		return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05Z")
	}
	row := overdueRow{
		TripID: run.TripID, Vehicle: run.Vehicle, Route: s.routeLabel(run), RoadWork: run.RoadWork,
		Allowance: allowanceText(&run.Allowance), Open: run.Outcome == domain.RunOpen,
		origin: run.Origin, dest: run.Destination, outcomeCode: run.Outcome,
		dispatchedUTC: utc(run.Dispatched.MS, run.Dispatched.OK), dueUTC: utc(run.DueMS, run.DueOK),
		endUTC: utc(run.End.MS, run.End.OK),
	}
	if row.Vehicle == "" {
		row.Vehicle = "(no number recorded)"
	}
	if run.RoadWork {
		row.dest = "road"
	}
	row.Dispatched = "not resolved"
	if run.Dispatched.OK {
		row.Dispatched = historyWhen(run.Dispatched.MS)
	}
	row.Due = "not known here"
	if run.DueOK {
		row.Due = historyWhen(run.DueMS)
		// The allowance counts from the entered time out (James 2026-10-06:
		// from the typed time — the scan's own basis); say so when it differs
		// from when the dispatch was recorded. ⟦was: FINDINGS-R4 D13, counted
		// from the recording⟧
		if run.TimedFromMS != 0 && run.RecordedMS-run.TimedFromMS >= 60_000 {
			row.Due += " (timed from the entered time, " + historyWhen(run.TimedFromMS) + ")"
		}
	}
	endMS, endOK := run.End.MS, run.End.OK
	if row.Open {
		endMS, endOK = now.UnixMilli(), true
	}
	past := false
	switch {
	case !run.DueOK || !endOK:
		row.Late = "not measured — " + map[bool]string{true: "the due time", false: "the end time"}[!run.DueOK] + " does not resolve here"
	default:
		late := time.Duration(endMS-run.DueMS) * time.Millisecond
		past = late > 0
		row.LateMin = strconv.FormatInt(int64(late/time.Minute), 10)
		switch {
		case past && row.Open:
			row.Late = spanText(late) + " past due, and still open"
		case past:
			row.Late = spanText(late) + " late"
		default:
			row.Late = "within the allowance by these records (" + spanText(-late) + " to spare)"
		}
	}
	var ackCSV []string
	for _, a := range acks {
		kind := "VERIFY BY RADIO"
		if a.Kind != deadline.AckKindAmber {
			kind = "OVERDUE"
		}
		when := "time not known here"
		if a.AtOK {
			when = historyWhen(a.AtMS)
		}
		v := overdueAckView{Overdue: kind == "OVERDUE", Retracted: a.Retracted,
			Text: fmt.Sprintf("%s acknowledged by %s at %s, %s", kind, a.By, s.Cfg.SiteName(a.Site), when)}
		if a.Retracted {
			v.Text += " — later withdrawn"
		}
		row.Acks = append(row.Acks, v)
		if a.OverdueAck() {
			row.Acked = true
		}
		ackCSV = append(ackCSV, fmt.Sprintf("%s|%s|%s|%s|%t", kind, a.By, a.Site, utc(a.AtMS, a.AtOK), a.Retracted))
	}
	row.ackCSV = strings.Join(ackCSV, "; ")
	word := overdueOutcomeWords[run.Outcome]
	switch run.Outcome {
	case domain.RunArrived:
		row.Outcome = word + " at " + s.Cfg.SiteName(run.Destination)
	case domain.RunTurnedBack:
		row.Outcome = word + " — back at " + s.Cfg.SiteName(run.Origin)
	case domain.RunBack:
		row.Outcome = word + " at " + s.Cfg.SiteName(run.Origin)
	default:
		row.Outcome = word
	}
	if run.End.OK {
		row.Outcome += ", " + historyWhen(run.End.MS)
	}
	return row, past || row.Acked
}

var overdueCSVHeader = []string{"trip_id", "vehicle", "origin", "destination", "allowance_min", "dispatched_utc", "due_utc",
	"late_min", "outcome", "ended_utc", "acknowledged", "acknowledgments"}

func (d overdueHistoryData) csvRows() [][]string {
	out := make([][]string, 0, len(d.Rows))
	for _, r := range d.Rows {
		acked := "no"
		if r.Acked {
			acked = "yes"
		}
		allow := strings.TrimSuffix(strings.TrimSuffix(r.Allowance, " minutes"), " minute")
		out = append(out, []string{r.TripID, r.Vehicle, r.origin, r.dest, allow, r.dispatchedUTC, r.dueUTC,
			r.LateMin, r.outcomeCode, r.endUTC, acked, r.ackCSV})
	}
	return out
}

func (s *Server) handleOverdueHistory(w http.ResponseWriter, r *http.Request) {
	now := time.UnixMilli(s.clockReading().WallMS)
	d := s.buildOverdueHistory(r, now)
	if strings.HasSuffix(r.URL.Path, ".csv") {
		if !csvRefused(w, "the overdue history", d.Err, false, d.Filter.Problems) {
			return
		}
		s.writeCSVDownload(w, "overdue-history", overdueCSVHeader, d.csvRows())
		return
	}
	s.renderPage(w, "overdue history", "history_overdue.html", d)
}

// ---- missed sign-outs ----

type signoutRowView struct {
	Site, Person, Company, SignedIn, ClosedBy, Week string
	csv                                             []string
}

type signoutGroupView struct {
	Name   string
	Count  int
	Repeat bool
	Sites  string
	Weeks  string // "week of Oct 5: 2, week of Sep 28: 1"
	People int
	Last   string
}

type signoutSiteView struct {
	SiteID, Name, Kind   string
	Known                bool
	Missed, Exempt, Uncl int
	People, Companies    int
}

type signoutHistoryData struct {
	historyPage
	Rows      []signoutRowView
	BySite    []signoutSiteView
	ByPerson  []signoutGroupView
	ByCompany []signoutGroupView
	ByWeek    []domain.WeekSignouts
	Repeat    struct{ People, Companies int }
	Untyped   []string
	Total     int
	Summary   []string
}

var signoutClosedWords = map[domain.VisitClosure]string{
	domain.ClosedByReCheckin:   "signed in again here, no sign-out",
	domain.ClosedByNextArrival: "arrived here again, no sign-out",
}

func weekLabel(wk string) string {
	t, err := time.ParseInLocation("2006-01-02", wk, time.Local)
	if err != nil {
		return wk
	}
	return "week of " + t.Format("Mon Jan 2")
}

func (s *Server) groupView(g domain.SignoutGroup, nameOf func(domain.SignoutGroup) string) signoutGroupView {
	v := signoutGroupView{Name: nameOf(g), Count: g.Count, Repeat: g.Repeat(), People: g.People, Last: historyWhen(g.LastMS)}
	var names []string
	for _, id := range g.Sites {
		names = append(names, s.Cfg.SiteName(id))
	}
	v.Sites = strings.Join(names, ", ")
	var wks []string
	for wk := range g.ByWeek {
		wks = append(wks, wk)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(wks)))
	var parts []string
	for _, wk := range wks {
		parts = append(parts, fmt.Sprintf("%s: %d", weekLabel(wk), g.ByWeek[wk]))
	}
	v.Weeks = strings.Join(parts, "; ")
	return v
}

func (s *Server) buildSignoutHistory(r *http.Request, now time.Time) signoutHistoryData {
	dispatch := dispatchHost(r.Host)
	var sites []string
	types := s.Cfg.KnownSiteTypes()
	if dispatch {
		sites = s.allSiteIDs()
	} else {
		types = domain.SiteTypes{s.Cfg.SiteID: s.Cfg.SiteType}
	}
	d := signoutHistoryData{historyPage: s.historyBase(r, "/history/signouts", sites, now)}
	if dispatch {
		d.Scope = "Every site's visits that reached " + s.Cfg.PostName() + "."
	} else {
		d.Scope = "Visits at " + s.Cfg.PostName() + " only."
	}
	if len(d.Filter.Problems) > 0 {
		return d
	}
	f := domain.SignoutFilter{Site: d.Filter.Site, FromMS: d.Filter.fromMS, ToMS: d.Filter.toMS}
	if !dispatch {
		f.Site = s.Cfg.SiteID
	}
	h, err := domain.BuildSignoutHistory(s.Store, types, f, time.Local)
	if err != nil {
		log.Printf("signout history: %v", err)
		d.Err = refusalText(err)
		return d
	}
	d.Summary = h.Summary.Lines()
	d.Total = len(h.Rows)
	for _, m := range h.Rows {
		v := signoutRowView{Site: s.Cfg.SiteName(m.SiteID), Person: m.PersonName, Company: m.Company,
			ClosedBy: signoutClosedWords[m.ClosedBy], Week: weekLabel(m.WeekStart)}
		if v.Person == "" {
			v.Person = "(no name recorded)"
		}
		if v.Company == "" {
			v.Company = "(no company)"
		}
		in := m.ArrivalInstant()
		v.SignedIn = "not resolved"
		inUTC := ""
		if in.OK {
			v.SignedIn = historyWhen(in.MS)
			inUTC = time.UnixMilli(in.MS).UTC().Format("2006-01-02T15:04:05Z")
		}
		closed := m.CloseInstant()
		closedUTC := ""
		if closed.OK {
			closedUTC = time.UnixMilli(closed.MS).UTC().Format("2006-01-02T15:04:05Z")
		}
		v.csv = []string{m.SiteID, m.VisitID, m.PersonID, m.PersonName, m.Company, inUTC, m.WeekStart, string(m.ClosedBy), closedUTC}
		d.Rows = append(d.Rows, v)
	}
	// Per site: every site in scope gets a line, even with nothing in it —
	// a site that is quiet and a site whose type is unknown read differently.
	have := map[string]domain.SiteSignouts{}
	for _, ss := range h.BySite {
		have[ss.SiteID] = ss
	}
	scope := sites
	if !dispatch {
		scope = []string{s.Cfg.SiteID}
	} else if d.Filter.Site != "" {
		scope = []string{d.Filter.Site}
	}
	for _, id := range scope {
		ss := have[id]
		typ, known := types[id]
		v := signoutSiteView{SiteID: id, Name: s.Cfg.SiteName(id), Known: known && typ != "", Missed: ss.Missed,
			Exempt: ss.Exempt, Uncl: ss.Unclassified, People: ss.People, Companies: ss.Companies}
		switch typ {
		case domain.SiteTypeTextMessage:
			v.Kind = "text-message site"
		case domain.SiteTypeCamp:
			v.Kind = "camp (exempt)"
		default:
			v.Kind = "type not known at this post"
		}
		d.BySite = append(d.BySite, v)
	}
	for _, g := range h.ByPerson {
		gv := s.groupView(g, func(g domain.SignoutGroup) string {
			if g.Name == "" {
				return "(no name recorded)"
			}
			return g.Name
		})
		if gv.Repeat {
			d.Repeat.People++
		}
		d.ByPerson = append(d.ByPerson, gv)
	}
	for _, g := range h.ByCompany {
		gv := s.groupView(g, func(g domain.SignoutGroup) string {
			if g.Name == "" {
				return "(no company)"
			}
			return g.Name
		})
		if gv.Repeat {
			d.Repeat.Companies++
		}
		d.ByCompany = append(d.ByCompany, gv)
	}
	d.ByWeek = h.ByWeek
	for _, id := range h.Untyped {
		d.Untyped = append(d.Untyped, s.Cfg.SiteName(id))
	}
	return d
}

var signoutCSVHeader = []string{"site_id", "visit_id", "person_id", "person_name", "company", "signed_in_utc", "week_start", "closed_by", "closed_utc"}

func (s *Server) handleSignoutHistory(w http.ResponseWriter, r *http.Request) {
	now := time.UnixMilli(s.clockReading().WallMS)
	d := s.buildSignoutHistory(r, now)
	if strings.HasSuffix(r.URL.Path, ".csv") {
		if !csvRefused(w, "the missed sign-outs report", d.Err, false, d.Filter.Problems) {
			return
		}
		rows := make([][]string, 0, len(d.Rows))
		for _, r := range d.Rows {
			rows = append(rows, r.csv)
		}
		s.writeCSVDownload(w, "missed-signouts", signoutCSVHeader, rows)
		return
	}
	s.renderPage(w, "signout history", "history_signouts.html", d)
}

// ---- lost-signal history ----

type signalRowView struct {
	Site, From, Detected, Ended, Duration string
	Open, NeverHeard, Restart             bool
	csv                                   []string
}

type signalHistoryData struct {
	historyPage
	Rows        []signalRowView
	NotRecorded bool
	Rule        string
	OpenNow     int
}

// buildSignalRows renders the record, filtered. Shared by the history page
// and the status page's section.
func (s *Server) buildSignalRows(f historyFilter, now time.Time, limit int) ([]signalRowView, bool, error) {
	outages, err := s.signalOutages()
	if err != nil {
		return nil, err == errSignalNotRecorded, err
	}
	var out []signalRowView
	for _, o := range outages {
		if f.Site != "" && o.Peer != f.Site {
			continue
		}
		end := now.UnixMilli()
		if !o.Open() {
			end = o.EndedMS
		}
		// In range when the outage overlaps the chosen days.
		if (f.toMS != 0 && o.LostFromMS >= f.toMS) || (f.fromMS != 0 && end < f.fromMS) {
			continue
		}
		v := signalRowView{Site: s.Cfg.SiteName(o.Peer), From: historyWhen(o.LostFromMS), Detected: historyWhen(o.DetectedMS),
			Open: o.Open(), NeverHeard: o.NeverHeard, Restart: o.SpansRestart()}
		dur := time.Duration(end-o.LostFromMS) * time.Millisecond
		v.Duration = spanText(dur)
		endUTC := ""
		if o.Open() {
			v.Ended = "still lost"
			v.Duration += " so far"
		} else {
			v.Ended = historyWhen(o.EndedMS)
			endUTC = time.UnixMilli(o.EndedMS).UTC().Format("2006-01-02T15:04:05Z")
		}
		v.csv = []string{o.Peer, time.UnixMilli(o.LostFromMS).UTC().Format("2006-01-02T15:04:05Z"),
			strconv.FormatBool(o.NeverHeard), time.UnixMilli(o.DetectedMS).UTC().Format("2006-01-02T15:04:05Z"),
			endUTC, strconv.FormatInt(int64(dur/time.Second), 10), strconv.FormatBool(o.Open()), strconv.FormatBool(v.Restart)}
		out = append(out, v)
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out, false, nil
}

func (s *Server) buildSignalHistory(r *http.Request, now time.Time) signalHistoryData {
	var extra []string
	if outages, err := s.signalOutages(); err == nil {
		for _, o := range outages {
			extra = append(extra, o.Peer)
		}
	}
	sites := s.allSiteIDs(extra...)
	sites = without(sites, s.Cfg.SiteID)
	d := signalHistoryData{historyPage: s.historyBase(r, "/history/signal", sites, now)}
	d.Scope = "As recorded by " + s.Cfg.PostName() + ": the sites it lost contact with."
	d.Rule = signalRule(s.buildSyncSection(), s.Cfg.PostName())
	if len(d.Filter.Problems) > 0 {
		return d
	}
	rows, notRecorded, err := s.buildSignalRows(d.Filter, now, 0)
	switch {
	case notRecorded:
		d.NotRecorded = true
	case err != nil:
		log.Printf("signal history: %v", err)
		d.Err = refusalText(err)
	}
	d.Rows = rows
	for _, v := range rows {
		if v.Open {
			d.OpenNow++
		}
	}
	return d
}

func without(ids []string, drop string) []string {
	var out []string
	for _, id := range ids {
		if id != drop {
			out = append(out, id)
		}
	}
	return out
}

var signalCSVHeader = []string{"site_id", "lost_from_utc", "never_heard_since_start", "noticed_utc", "back_utc", "duration_s", "still_lost", "post_restarted_during"}

func (s *Server) handleSignalHistory(w http.ResponseWriter, r *http.Request) {
	now := time.UnixMilli(s.clockReading().WallMS)
	d := s.buildSignalHistory(r, now)
	if strings.HasSuffix(r.URL.Path, ".csv") {
		if !csvRefused(w, "the lost-signal history", d.Err, d.NotRecorded, d.Filter.Problems) {
			return
		}
		rows := make([][]string, 0, len(d.Rows))
		for _, v := range d.Rows {
			rows = append(rows, v.csv)
		}
		s.writeCSVDownload(w, "lost-signal-history", signalCSVHeader, rows)
		return
	}
	s.renderPage(w, "signal history", "history_signal.html", d)
}
