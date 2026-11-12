// dispatch_reports.go — GET /analytics/reports…: the dispatch reports (FIX16 stream
// E, James's approved list). Four reports, each a plain GET form and its
// result on one page:
//
//   - /analytics/reports/daily     one site, one day: peak and low headcount
//     (with times), sign-ins, sign-outs, trips out / in / turned back, runs
//     past their allowance, missed sign-outs.
//   - /analytics/reports/hours     contractor hours per person and company
//     over a date range, with company totals; visits closed by a missed
//     sign-out, or still open, are FLAGGED and listed apart, never counted.
//   - /analytics/reports/weekly    one week across every site.
//   - /analytics/reports/headcount hourly headcount per site over a range:
//     an inline SVG chart (no script, no CDN) and the same figures as a
//     table.
//
// ONE COUNT, NOT TWO. Every visit / travel / turned-back figure for a day or
// a week is domain.AnalyticsWindow — the computation behind the /analytics
// tiles, with "today" replaced by the chosen window — so a report and the
// tiles for the same day cannot disagree (TestFix16eReportsAgreeWithTheTiles
// cross-checks it). Headcount, hours and late runs are domain/reports.go,
// over the same VisitReport replay.
//
// A DAY is midnight to midnight local time (the lead's default: on the
// site's clock); every instant is the one the recording post's own clock
// gave it, as on every analytics page.
//
// LOST-SIGNAL GAPS come from the serving post's own lost-signal record
// (FIX16f signal_outage, dispatch_reports_signal.go), which exists only from
// the first time a post ran the recorder: before that the pages say "not
// available" rather than print a zero. ⟦was: always not available — no post
// recorded a history of its contact⟧
//
// READ-ONLY, behind the login, GET only — every writing method on these
// paths is refused 405 explicitly, the pages hold only GET forms, and on the
// dispatch host they are allow-listed reads (dispatch_surface.go) that link
// only to other analytics pages.
package web

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
)

// reportHeadcountMaxDays caps the headcount report's range. See
// TestFix16eHeadcountRangeIsCapped and the measurement in the FIX16 E report.
const reportHeadcountMaxDays = 14

const reportDay = "2006-01-02"

func (s *Server) registerDispatchReportRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /analytics/reports", s.requireLogin(s.handleReportsIndex))
	mux.HandleFunc("GET /analytics/reports/daily", s.requireLogin(s.handleReportDaily))
	mux.HandleFunc("GET /analytics/reports/hours", s.requireLogin(s.handleReportHours))
	mux.HandleFunc("GET /analytics/reports/weekly", s.requireLogin(s.handleReportWeekly))
	mux.HandleFunc("GET /analytics/reports/headcount", s.requireLogin(s.handleReportHeadcount))
	// Every writing method: refused explicitly, so the refusal does not rest
	// on which other patterns happen to be registered.
	refuse := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "reports are read-only — nothing can be recorded here", http.StatusMethodNotAllowed)
	}
	for _, path := range []string{"/analytics/reports", "/analytics/reports/daily", "/analytics/reports/hours",
		"/analytics/reports/weekly", "/analytics/reports/headcount"} {
		for _, m := range []string{"POST", "PUT", "PATCH", "DELETE"} {
			mux.HandleFunc(m+" "+path, refuse)
		}
	}
}

// reportForm is every report's query as typed, echoed back.
type reportForm struct {
	Site, Date, From, To, Week string
}

type reportsData struct {
	Cfg            *config.Config
	SiteID         string
	Banner         []deadline.Alert
	BannerErr      string
	AlertsReadOnly bool
	Dispatch       bool

	Kind       string // "" (the index), "daily", "hours", "weekly", "headcount"
	Title      string
	Form       reportForm
	Sites      []facetOption
	Problems   []string
	Err        string
	RenderedAt string
	Elapsed    string
	MaxDays    int
	// CSV: the request is the report's download (format=csv); CSVHref is
	// the page's "Download CSV" link — the same path and query plus that.
	CSV     bool
	CSVHref string

	siteGiven bool // the query named a site (the hours report defaults to every site)

	Daily     *dailyReportView
	Hours     *hoursReportView
	Weekly    *weeklyReportView
	Headcount *headcountReportView
}

func (s *Server) reportsBase(r *http.Request, kind string) reportsData {
	d := reportsData{Cfg: s.Cfg, SiteID: s.Cfg.SiteID, Kind: kind, AlertsReadOnly: true,
		Dispatch: dispatchHost(r.Host), MaxDays: reportHeadcountMaxDays}
	d.Title = map[string]string{"": "Reports", "daily": "Daily site report", "hours": "Contractor hours by company",
		"weekly": "Weekly roll-up", "headcount": "Headcount over time"}[kind]
	now := s.reportNow()
	d.RenderedAt = now.Format("2006-01-02 15:04:05")
	d.Banner, d.BannerErr = s.banner()
	if d.Dispatch {
		past, err := s.dispatchPastAllowance(now)
		if err != nil {
			log.Printf("reports: all-sites overdue: %v", err)
		}
		d.Banner = append(d.Banner, past.Rows...)
		sortAlertsWorstFirst(d.Banner)
	}
	q := r.URL.Query()
	get := func(k string) string { return strings.TrimSpace(q.Get(k)) }
	if kind != "" {
		// The download is the same route with format=csv, so the report and
		// its file can never be two handlers.
		d.CSV = get("format") == "csv"
		dl := url.Values{}
		for k, v := range q {
			if k != "format" {
				dl[k] = v
			}
		}
		dl.Set("format", "csv")
		d.CSVHref = r.URL.Path + "?" + dl.Encode()
	}
	d.siteGiven = get("site") != ""
	d.Form = reportForm{Site: get("site"), Date: get("date"), From: get("from"), To: get("to"), Week: get("week")}
	today := now.Format(reportDay)
	if d.Form.Date == "" {
		d.Form.Date = today
	}
	if d.Form.To == "" {
		d.Form.To = today
	}
	if d.Form.From == "" {
		if kind == "headcount" {
			d.Form.From = now.AddDate(0, 0, -1).Format(reportDay)
		} else {
			d.Form.From = weekStart(now).Format(reportDay)
		}
	}
	if d.Form.Week == "" {
		d.Form.Week = weekStart(now).Format(reportDay)
	}
	if d.Form.Site == "" {
		d.Form.Site = s.Cfg.SiteID
	}
	for _, id := range s.searchSiteIDs() {
		d.Sites = append(d.Sites, facetOption{Value: id, Label: s.siteFilterLabel(id), Checked: id == d.Form.Site})
	}
	return d
}

func (s *Server) reportNow() time.Time {
	return time.UnixMilli(s.clockReading().WallMS).In(time.Local)
}

// weekStart is the Monday 00:00 of t's week, local time.
func weekStart(t time.Time) time.Time {
	t = t.In(time.Local)
	back := (int(t.Weekday()) + 6) % 7
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
	return d.AddDate(0, 0, -back)
}

// parseReportDay reads a yyyy-mm-dd as local midnight.
func parseReportDay(label, v string) (time.Time, string) {
	t, err := time.ParseInLocation(reportDay, v, time.Local)
	if err != nil {
		return time.Time{}, fmt.Sprintf("%s: enter a date as yyyy-mm-dd (for example %s).", label, time.Now().Format(reportDay))
	}
	return t, ""
}

func (s *Server) reportSiteKnown(id string) bool { return contains(s.searchSiteIDs(), id) }

func (s *Server) handleReportsIndex(w http.ResponseWriter, r *http.Request) {
	s.reportOut(w, s.reportsBase(r, ""))
}

// reportOut sends the report as its page or, for a format=csv request, as a
// download of the same rows the page shows. A download never stands in for
// a refusal: a bad query is a 400 and a failed read a 500, with no file.
func (s *Server) reportOut(w http.ResponseWriter, d reportsData) {
	if !d.CSV {
		s.renderPage(w, "reports", "dispatch_reports.html", d)
		return
	}
	switch {
	case len(d.Problems) > 0:
		http.Error(w, "no report — "+strings.Join(d.Problems, " "), http.StatusBadRequest)
		return
	case d.Err != "":
		http.Error(w, "the report could not be built — "+d.Err+". No file was produced.", http.StatusInternalServerError)
		return
	}
	header, rows, what := d.csvRows()
	s.writeCSVDownload(w, what, header, rows)
}

// csvRows is the report's table as CSV rows: the page's own values, cell for
// cell.
func (d reportsData) csvRows() (header []string, rows [][]string, what string) {
	n := func(i int) string { return fmt.Sprint(i) }
	switch {
	case d.Daily != nil:
		v := d.Daily
		header = []string{"Site", "Day", "Section", "Item", "Value", "Detail"}
		add := func(section, item, value, detail string) {
			rows = append(rows, []string{v.SiteName, v.Date, section, item, value, detail})
		}
		add("figures", "Peak headcount", n(v.Peak), "first reached at "+v.PeakAt)
		add("figures", "Lowest headcount", n(v.Low), "first reached at "+v.LowAt)
		add("figures", "On site at midnight (start of day)", n(v.StartCount), "")
		add("figures", "Sign-ins", n(v.SignIns), "visits that began this day")
		add("figures", "Sign-outs", n(v.SignOuts), fmt.Sprintf("%d signed out at the gate, %d left aboard a trip or road-work run", v.CheckOuts, v.LeftTrip))
		add("figures", "Trips out", n(v.TripsOut), "")
		add("figures", "Trips in", n(v.TripsIn), "")
		add("figures", "Turned back", n(v.TurnedBack), "")
		add("figures", "Working on the road", n(v.RoadWork), "")
		add("figures", "Runs past their allowance", n(len(v.Late)), "")
		add("figures", "Missed sign-outs", n(len(v.Missed)), v.MissedLabel)
		sv := v.Signal.Value
		if !v.Signal.Available {
			sv = ""
		}
		add("figures", "Lost-signal gaps", sv, v.Signal.Detail)
		for _, l := range v.Late {
			add("run past its allowance", l.Vehicle+" "+l.Route, l.Allowance, "due "+l.Due+"; "+l.Outcome)
		}
		for _, m := range v.Missed {
			add("missed sign-out", m.Person, "", "signed in "+m.In+"; record closed by their return at "+m.ClosedAt)
		}
		return header, rows, "daily-" + v.Site + "-" + v.Date
	case d.Hours != nil:
		header = []string{"Counted", "Company", "Person", "Site", "In", "Closed", "Hours", "Decimal hours", "Visits counted", "Flagged", "Why not counted"}
		for _, c := range d.Hours.Companies {
			rows = append(rows, []string{"yes", c.Company, "(company total)", "", "", "", c.Hours, c.Decimal, n(c.Visits), n(c.Flagged), ""})
			for _, p := range c.People {
				rows = append(rows, []string{"yes", c.Company, p.Name, "", "", "", p.Hours, p.Decimal, n(p.Visits), n(p.Flagged), ""})
			}
		}
		rows = append(rows, []string{"yes", "(all companies)", "(total)", "", "", "", d.Hours.Total, d.Hours.TotalDecimal, n(d.Hours.TotalVisits), n(len(d.Hours.Flagged)), ""})
		for _, f := range d.Hours.Flagged {
			why := f.Why
			if f.Bound != "" {
				why += " (" + f.Bound + ", an upper bound)"
			}
			rows = append(rows, []string{"NO — flagged", f.Company, f.Person, f.Site, f.In, f.Out, "", "", "", "", why})
		}
		return header, rows, "hours"
	case d.Weekly != nil:
		v := d.Weekly
		header = []string{"Week", "Site", "Visits", "People", "Companies", "Trips out", "Trips in", "Turned back", "Road work", "Past allowance", "Missed sign-outs", "Signal outages"}
		for _, r := range v.Sites {
			rows = append(rows, []string{v.Range, r.Name, n(r.Visits), n(r.People), n(r.Companies), n(r.TripsOut), n(r.TripsIn), n(r.TurnedBack), n(r.RoadWork), n(r.Late), n(r.Missed), csvSignal(r.Signal)})
		}
		rows = append(rows, []string{v.Range, "(all sites)", n(v.Visits), n(v.People), n(v.Companies), n(v.Dispatched), n(v.Arrived), n(v.TurnedBack), n(v.RoadWork), n(v.Late), n(v.Missed), csvSignal(v.Signal)})
		return header, rows, "weekly-" + d.Form.Week
	case d.Headcount != nil:
		header = []string{"Site", "Hour starting", "On site at the start", "Highest in the hour"}
		for _, p := range d.Headcount.Points {
			rows = append(rows, []string{d.Headcount.SiteName, p.Label, n(p.AtStart), n(p.Peak)})
		}
		return header, rows, "headcount-" + d.Headcount.Site
	}
	return []string{"Report"}, nil, "report"
}

// reportFail renders the page loud and empty: never a partial report.
func (s *Server) reportFail(w http.ResponseWriter, d reportsData, err error) {
	log.Printf("reports %s: %v", d.Kind, err)
	d.Err = refusalText(err)
	d.Daily, d.Hours, d.Weekly, d.Headcount = nil, nil, nil, nil
	s.reportOut(w, d)
}

// ---- formatting ----

func clock(ms int64) string { return time.UnixMilli(ms).In(time.Local).Format("15:04") }
func stamp(ms int64) string { return time.UnixMilli(ms).In(time.Local).Format("Mon 2006-01-02 15:04") }

// hoursText renders whole minutes as "12h 05m".
func hoursText(min int64) string {
	return fmt.Sprintf("%dh %02dm", min/60, min%60)
}

// decimalHours renders minutes as hours to two places, the figure a
// timesheet wants.
func decimalHours(min int64) string { return fmt.Sprintf("%.2f", float64(min)/60) }

// missedLabel names the closed-by-return count by what this post knows of
// the site's type (visit_report.go R-4/R-5): only a text-message site's are
// failures; a camp's are normal; another post's type is not known here.
func (s *Server) missedLabel(site string) string {
	if site == s.Cfg.SiteID {
		switch s.Cfg.SiteType {
		case domain.SiteTypeCamp:
			return "returns with no sign-out (normal at a camp site, not a failure)"
		case domain.SiteTypeTextMessage:
			return "missed sign-outs"
		}
	}
	return "missed sign-outs (visits closed by the person's return with no sign-out; this post is not told whether that site is a camp, where that is normal)"
}

// ---- 1. daily site report ----

type lateRunView struct {
	Vehicle, Route, Allowance, Due, Outcome string
	TripID                                  string
}

type missedView struct {
	Person, In, ClosedAt string
}

type dailyReportView struct {
	Site, SiteName, Day string
	Date                string // yyyy-mm-dd
	Partial             bool   // the day is today: figures run to the report's instant
	StartCount          int
	Peak, Low           int
	PeakAt, LowAt       string
	SignIns, SignOuts   int
	CheckOuts, LeftTrip int
	TripsOut, TripsIn   int
	TurnedBack          int
	RoadWork            int
	MissedLabel         string
	Missed              []missedView
	Late                []lateRunView
	Unjudged            int
	UnknownTimes        int
	HeadcountLink       string
	Signal              signalGapView // this post's lost-signal record for the day
}

func (s *Server) handleReportDaily(w http.ResponseWriter, r *http.Request) {
	began := time.Now()
	d := s.reportsBase(r, "daily")
	day, p := parseReportDay("Date", d.Form.Date)
	if p != "" {
		d.Problems = append(d.Problems, p)
	}
	if !s.reportSiteKnown(d.Form.Site) {
		d.Problems = append(d.Problems, "Site: choose a post from the list.")
	}
	now := s.reportNow()
	if p == "" && day.After(now) {
		d.Problems = append(d.Problems, "Date: that day has not started yet.")
	}
	if len(d.Problems) > 0 {
		s.reportOut(w, d)
		return
	}
	v, err := s.buildDaily(d.Form.Site, day, now)
	if err != nil {
		s.reportFail(w, d, err)
		return
	}
	d.Daily = v
	d.Elapsed = time.Since(began).Round(time.Millisecond).String()
	s.reportOut(w, d)
}

func (s *Server) buildDaily(site string, day, now time.Time) (*dailyReportView, error) {
	from, to := day.UnixMilli(), day.AddDate(0, 0, 1).UnixMilli()
	fig, err := domain.AnalyticsWindow(s.Store, from, to)
	if err != nil {
		return nil, err
	}
	recs, _, err := domain.VisitReport(s.Store, nil)
	if err != nil {
		return nil, err
	}
	late, unjudged, err := domain.RunsPastAllowance(s.Store, now.UnixMilli())
	if err != nil {
		return nil, err
	}
	v := &dailyReportView{Site: site, SiteName: s.Cfg.SiteName(site), Day: day.Format("Monday 2 January 2006"), Date: day.Format(reportDay),
		MissedLabel: s.missedLabel(site), Unjudged: unjudged,
		HeadcountLink: "/analytics/reports/headcount?site=" + site + "&from=" + day.Format(reportDay) + "&to=" + day.Format(reportDay)}
	for _, sf := range fig.Sites {
		if sf.SiteID != site {
			continue
		}
		v.SignIns = sf.VisitsToday
		v.CheckOuts, v.LeftTrip = sf.CheckOutsToday, sf.LeftOnTripToday
		v.SignOuts = sf.CheckOutsToday + sf.LeftOnTripToday
		v.TripsOut, v.TripsIn, v.TurnedBack = sf.TravelOutToday, sf.TravelInToday, sf.TurnedBackToday
		v.RoadWork = sf.RoadWorkOutToday
	}
	spans, unknown := domain.PresenceSpans(recs, site)
	v.UnknownTimes = unknown
	hc := domain.NewHeadcount(spans)
	end := to
	if now.UnixMilli() < end {
		end, v.Partial = now.UnixMilli()+1, true
	}
	x := hc.Extremes(from, end)
	v.StartCount, v.Peak, v.Low = hc.At(from), x.Peak, x.Low
	v.PeakAt, v.LowAt = clock(x.PeakMS), clock(x.LowMS)
	for _, rec := range recs {
		if rec.SiteID != site || rec.ClosedBy == domain.VisitStillOpen || rec.ClosedBy.DepartureRecorded() {
			continue
		}
		c := rec.CloseInstant()
		if !c.OK || c.MS < from || c.MS >= to {
			continue
		}
		name := rec.PersonName
		mv := missedView{Person: name, ClosedAt: clock(c.MS)}
		if in := rec.ArrivalInstant(); in.OK {
			mv.In = stamp(in.MS)
		}
		v.Missed = append(v.Missed, mv)
	}
	for _, l := range late {
		if !l.Touches(site) || l.LeftMS < from || l.LeftMS >= to {
			continue
		}
		v.Late = append(v.Late, s.lateView(l))
	}
	v.Signal = s.signalGapsFor(site, from, to)
	return v, nil
}

func (s *Server) lateView(l domain.LateRun) lateRunView {
	lv := lateRunView{TripID: l.TripID, Vehicle: l.Vehicle, Allowance: fmt.Sprintf("%d min", l.AllowanceMin), Due: stamp(l.DueMS)}
	switch {
	case l.RoadWork:
		lv.Route = s.Cfg.SiteName(l.Origin) + " — working on the road"
		lv.Allowance += " working limit"
	case l.DestinationSite != "":
		lv.Route = s.Cfg.SiteName(l.Origin) + " → " + s.Cfg.SiteName(l.DestinationSite)
	default:
		lv.Route = s.Cfg.SiteName(l.Origin) + " → " + l.Destination
	}
	over := hoursMinutes(time.Duration(l.OverByMS) * time.Millisecond)
	switch {
	case l.Open:
		lv.Outcome = "STILL OUT — " + over + " past its allowance now"
	case l.TurnedBack:
		lv.Outcome = "turned back, arrived at " + stamp(l.ArrivedMS) + " — " + over + " late"
	default:
		lv.Outcome = "arrived " + stamp(l.ArrivedMS) + " — " + over + " late"
	}
	return lv
}

// ---- 2. contractor hours by company ----

type hoursPersonView struct {
	Name, Hours, Decimal string
	Visits, Flagged      int
}

type hoursCompanyView struct {
	Company, Hours, Decimal string
	Visits, Flagged         int
	People                  []hoursPersonView
}

type hoursFlagView struct {
	Person, Company, Site, In, Out, Why, Bound string
}

type hoursReportView struct {
	Range        string
	SiteName     string // "" = every site
	Companies    []hoursCompanyView
	Flagged      []hoursFlagView
	Total        string
	TotalDecimal string
	TotalVisits  int
}

func (s *Server) handleReportHours(w http.ResponseWriter, r *http.Request) {
	began := time.Now()
	d := s.reportsBase(r, "hours")
	if !d.siteGiven {
		d.Form.Site = "" // every site, unless one was chosen
		for i := range d.Sites {
			d.Sites[i].Checked = false
		}
	}
	from, p1 := parseReportDay("From", d.Form.From)
	to, p2 := parseReportDay("To", d.Form.To)
	for _, p := range []string{p1, p2} {
		if p != "" {
			d.Problems = append(d.Problems, p)
		}
	}
	if p1 == "" && p2 == "" && to.Before(from) {
		d.Problems = append(d.Problems, "To: the end date is before the start date.")
	}
	if d.Form.Site != "" && !s.reportSiteKnown(d.Form.Site) {
		d.Problems = append(d.Problems, "Site: choose a post from the list.")
	}
	if len(d.Problems) > 0 {
		s.reportOut(w, d)
		return
	}
	recs, _, err := domain.VisitReport(s.Store, nil)
	if err != nil {
		s.reportFail(w, d, err)
		return
	}
	rep, err := domain.ContractorHours(s.Store, recs, d.Form.Site, from.UnixMilli(), to.AddDate(0, 0, 1).UnixMilli())
	if err != nil {
		s.reportFail(w, d, err)
		return
	}
	d.Hours = s.hoursView(rep, from, to, d.Form.Site)
	d.Elapsed = time.Since(began).Round(time.Millisecond).String()
	s.reportOut(w, d)
}

func (s *Server) hoursView(rep domain.HoursReport, from, to time.Time, site string) *hoursReportView {
	v := &hoursReportView{Range: from.Format("Mon 2 Jan 2006") + " to " + to.Format("Mon 2 Jan 2006"),
		Total: hoursText(rep.TotalMinutes), TotalDecimal: decimalHours(rep.TotalMinutes), TotalVisits: rep.TotalVisits}
	if site != "" {
		v.SiteName = s.Cfg.SiteName(site)
	}
	for _, c := range rep.Companies {
		cv := hoursCompanyView{Company: c.Company, Hours: hoursText(c.Minutes), Decimal: decimalHours(c.Minutes), Visits: c.Visits, Flagged: c.Flagged}
		if cv.Company == "" {
			cv.Company = "(no company recorded)"
		}
		for _, p := range c.People {
			cv.People = append(cv.People, hoursPersonView{Name: p.Name, Hours: hoursText(p.Minutes), Decimal: decimalHours(p.Minutes), Visits: p.Visits, Flagged: p.Flagged})
		}
		v.Companies = append(v.Companies, cv)
	}
	for _, f := range rep.Flagged {
		fv := hoursFlagView{Person: f.PersonName, Company: f.Company, Site: s.Cfg.SiteName(f.SiteID), In: stamp(f.ArrivedMS)}
		if fv.Company == "" {
			fv.Company = "(no company recorded)"
		}
		switch f.Flag {
		case domain.HoursFlagStillOpen:
			fv.Why, fv.Out = "STILL OPEN — no sign-out yet", "—"
		case domain.HoursFlagMissedSignOut:
			fv.Why, fv.Out = "MISSED SIGN-OUT — closed by the person's next arrival, no departure recorded", stamp(f.ClosedMS)
			if f.OpenMinutes != nil {
				fv.Bound = "at most " + hoursText(*f.OpenMinutes)
			}
		default:
			fv.Why, fv.Out = "TIME UNRESOLVED — a time on this visit could not be resolved", "—"
		}
		v.Flagged = append(v.Flagged, fv)
	}
	return v
}

// ---- 3. weekly roll-up ----

type weeklySiteView struct {
	Site, Name                    string
	Visits, People, Companies     int
	TripsOut, TripsIn, TurnedBack int
	RoadWork, Late, Missed        int
	Signal                        signalGapView
}

type weeklyReportView struct {
	Range                                string
	Sites                                []weeklySiteView
	Visits, People, Companies            int
	Dispatched, Arrived, TurnedBack      int
	RoadWork, Late, Missed, UnknownTimes int
	Unjudged                             int
	Signal                               signalGapView // every peer, this post's record
}

func (s *Server) handleReportWeekly(w http.ResponseWriter, r *http.Request) {
	began := time.Now()
	d := s.reportsBase(r, "weekly")
	day, p := parseReportDay("Week", d.Form.Week)
	if p != "" {
		d.Problems = append(d.Problems, p)
		s.reportOut(w, d)
		return
	}
	start := weekStart(day)
	d.Form.Week = start.Format(reportDay)
	v, err := s.buildWeekly(start)
	if err != nil {
		s.reportFail(w, d, err)
		return
	}
	d.Weekly = v
	d.Elapsed = time.Since(began).Round(time.Millisecond).String()
	s.reportOut(w, d)
}

func (s *Server) buildWeekly(start time.Time) (*weeklyReportView, error) {
	end := start.AddDate(0, 0, 7)
	from, to := start.UnixMilli(), end.UnixMilli()
	fig, err := domain.AnalyticsWindow(s.Store, from, to)
	if err != nil {
		return nil, err
	}
	recs, _, err := domain.VisitReport(s.Store, nil)
	if err != nil {
		return nil, err
	}
	ti, err := domain.LoadTripInstants(s.Store)
	if err != nil {
		return nil, err
	}
	late, unjudged, err := domain.RunsPastAllowance(s.Store, s.reportNow().UnixMilli())
	if err != nil {
		return nil, err
	}
	v := &weeklyReportView{Range: start.Format("Mon 2 Jan 2006") + " to " + end.AddDate(0, 0, -1).Format("Sun 2 Jan 2006"),
		Dispatched: fig.TripsDispatchedToday, Arrived: fig.TripsArrivedToday, TurnedBack: fig.TripsTurnedBackToday,
		RoadWork: fig.RoadWorkDispatchedToday, Unjudged: unjudged}
	people, companies := map[string]map[string]bool{}, map[string]map[string]bool{}
	allPeople, allCompanies := map[string]bool{}, map[string]bool{}
	for _, rec := range recs {
		in := rec.ArrivalInstant()
		if !in.OK || in.MS < from || in.MS >= to {
			if !in.OK {
				v.UnknownTimes++
			}
			continue
		}
		if people[rec.SiteID] == nil {
			people[rec.SiteID], companies[rec.SiteID] = map[string]bool{}, map[string]bool{}
		}
		who := rec.PersonID
		if who == "" {
			who = "visit:" + rec.VisitID
		}
		people[rec.SiteID][who], allPeople[who] = true, true
		if co := strings.TrimSpace(ti.Visit[rec.VisitID].Company); co != "" {
			companies[rec.SiteID][strings.ToLower(co)], allCompanies[strings.ToLower(co)] = true, true
		}
	}
	lateBy := map[string]int{}
	for _, l := range late {
		if l.LeftMS < from || l.LeftMS >= to {
			continue
		}
		v.Late++
		lateBy[l.Origin]++
		if l.DestinationSite != "" && l.DestinationSite != l.Origin {
			lateBy[l.DestinationSite]++
		}
	}
	bySite := map[string]domain.SiteFigures{}
	for _, sf := range fig.Sites {
		bySite[sf.SiteID] = sf
	}
	ids := s.searchSiteIDs()
	for _, sf := range fig.Sites {
		if !contains(ids, sf.SiteID) && sf.VisitsToday+sf.TravelOutToday+sf.TravelInToday > 0 {
			ids = append(ids, sf.SiteID) // a site with records this week but not in config: still shown
		}
	}
	for _, id := range ids {
		sf := bySite[id]
		row := weeklySiteView{Site: id, Name: s.Cfg.SiteName(id), Visits: sf.VisitsToday,
			People: len(people[id]), Companies: len(companies[id]),
			TripsOut: sf.TravelOutToday, TripsIn: sf.TravelInToday, TurnedBack: sf.TurnedBackToday,
			RoadWork: sf.RoadWorkOutToday, Late: lateBy[id], Missed: sf.ReturnsClosedToday,
			Signal: s.signalGapsFor(id, from, to)}
		v.Visits += row.Visits
		v.Missed += row.Missed
		v.Sites = append(v.Sites, row)
	}
	v.People, v.Companies = len(allPeople), len(allCompanies)
	v.Signal = s.signalGapsFor("", from, to)
	return v, nil
}

// ---- 4. headcount over time ----

type headcountPointView struct {
	Label    string // "Mon 06 14:00"
	AtStart  int
	Peak     int
	X, Y, H  int // the chart bar (peak) in SVG user units
	DayStart bool
	DayLabel string
}

type headcountReportView struct {
	Site, SiteName, Range string
	Points                []headcountPointView
	Max                   int
	Peak                  int
	PeakAt                string
	UnknownTimes          int
	// chart geometry
	Width, Height, PlotTop, PlotBottom, PlotLeft int
	AxisX, TickBottom, DayLabelY                 int
	GridLines                                    []headcountGrid
}

type headcountGrid struct {
	Y     int
	Label int
}

func (s *Server) handleReportHeadcount(w http.ResponseWriter, r *http.Request) {
	began := time.Now()
	d := s.reportsBase(r, "headcount")
	from, p1 := parseReportDay("From", d.Form.From)
	to, p2 := parseReportDay("To", d.Form.To)
	for _, p := range []string{p1, p2} {
		if p != "" {
			d.Problems = append(d.Problems, p)
		}
	}
	if p1 == "" && p2 == "" {
		if to.Before(from) {
			d.Problems = append(d.Problems, "To: the end date is before the start date.")
		} else if days := int(to.Sub(from).Hours()/24+0.5) + 1; days > reportHeadcountMaxDays {
			d.Problems = append(d.Problems, fmt.Sprintf("The range is %d days; this report covers at most %d days at a time. Choose a shorter range.", days, reportHeadcountMaxDays))
		}
	}
	if !s.reportSiteKnown(d.Form.Site) {
		d.Problems = append(d.Problems, "Site: choose a post from the list.")
	}
	if len(d.Problems) > 0 {
		s.reportOut(w, d)
		return
	}
	recs, _, err := domain.VisitReport(s.Store, nil)
	if err != nil {
		s.reportFail(w, d, err)
		return
	}
	now := s.reportNow()
	end := to.AddDate(0, 0, 1).UnixMilli()
	if n := now.UnixMilli(); n < end {
		end = n + 1 // nothing after the report's instant: it has not happened
	}
	spans, unknown := domain.PresenceSpans(recs, d.Form.Site)
	hc := domain.NewHeadcount(spans)
	d.Headcount = headcountView(hc.Hourly(from.UnixMilli(), end), d.Form.Site, s.Cfg.SiteName(d.Form.Site),
		from.Format("Mon 2 Jan 2006")+" to "+to.Format("Mon 2 Jan 2006"), unknown)
	d.Elapsed = time.Since(began).Round(time.Millisecond).String()
	s.reportOut(w, d)
}

func headcountView(hours []domain.HeadcountHour, site, name, rng string, unknown int) *headcountReportView {
	v := &headcountReportView{Site: site, SiteName: name, Range: rng, UnknownTimes: unknown}
	for _, h := range hours {
		if h.Peak > v.Max || v.PeakAt == "" {
			v.Max, v.Peak = h.Peak, h.Peak
			v.PeakAt = time.UnixMilli(h.StartMS).In(time.Local).Format("Mon 2 Jan 15:04")
		}
	}
	// Chart geometry: one bar per hour, the bar is the hour's peak.
	const bar, gap = 6, 2
	v.PlotLeft, v.PlotTop = 36, 8
	v.PlotBottom = v.PlotTop + 160
	v.Height = v.PlotBottom + 28
	v.AxisX, v.TickBottom, v.DayLabelY = v.PlotLeft-6, v.PlotBottom+4, v.PlotBottom+18
	v.Width = v.PlotLeft + len(hours)*(bar+gap) + 8
	scale := v.Max
	if scale < 1 {
		scale = 1
	}
	step := 1
	for scale/step > 5 {
		step *= 2
	}
	for g := 0; g <= scale; g += step {
		v.GridLines = append(v.GridLines, headcountGrid{Y: v.PlotBottom - g*160/scale, Label: g})
	}
	for i, h := range hours {
		t := time.UnixMilli(h.StartMS).In(time.Local)
		ht := h.Peak * 160 / scale
		p := headcountPointView{Label: t.Format("Mon 2 Jan 15:04"), AtStart: h.AtStart, Peak: h.Peak,
			X: v.PlotLeft + i*(bar+gap), Y: v.PlotBottom - ht, H: ht, DayStart: t.Hour() == 0 || i == 0}
		if p.DayStart {
			p.DayLabel = t.Format("Mon 2")
		}
		v.Points = append(v.Points, p)
	}
	return v
}

// csvSignal is the weekly CSV's signal-outage cell: the count, or "not
// available" when this post's record does not cover the period.
func csvSignal(v signalGapView) string {
	if !v.Available {
		return "not available"
	}
	return v.Value
}
