// muster_at.go — GET /muster/at: the muster for any moment (FIX16a, James
// 2026-10-06). Pick a post (or, on the dispatch host, every post), a date
// and a time; the page lists who the record places ON SITE and ON THE ROAD
// at that moment, printed like the live muster — roster only, grouped by
// company then trade, stamped "as of HH:MM, <post>'s clock".
//
// THE READ is domain.LoadMomentLog + MomentLog.At (domain/muster_at.go):
// the visit report's replay with every correction, FIX8 hand-entered time,
// F15 turn-back and D1 taken-off-by-correction applied. The on-site names
// are grouped by domain.MusterLive over the moment's visit rows, so the
// grouping and the company/trade labels are exactly the live board's.
//
// NEVER HIDDEN. A record the moment cannot place with confidence — a time
// that cannot be read, or one read off an unsynced clock close to the
// moment — is listed under "Could not be placed", with the reason. A name
// on site whose record was closed only by their next check-in carries "no
// sign-out recorded". Hand-entered and corrected times are marked. A peer
// whose records stop short of the moment is named at the top.
//
// SPEED (measured, fix16a_muster_at_scale_test.go): one replay of the whole
// log plus one composition per run dispatched at or before the moment —
// runs after it are never composed. MEASURED 2026-10-06 on claude-remote at
// load ~20: a 14,700-visit, 700-run season (700 people × 20 rounds) answers
// in ~0.6 s at the season's start, middle and end alike (the replay is the
// cost, and it is linear); /last-seen ~0.4 s. Not scoped further: stopping
// the replay at the moment would order records by a clock (§4.4). The
// elapsed time prints on the page.
//
// READ-ONLY, STRUCTURALLY. GET only, a plain GET form (works with
// scripting off), behind the login on every host. Allowed on the dispatch
// host (dispatch_surface.go) with no console links: names link only to
// /last-seen, posts to /analytics/site/{id}.
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

// musterAtClockMargin: an instant read off an unsynced clock this close to
// the moment, on the far side of it, puts the record under "Could not be
// placed" instead of leaving it off.
const musterAtClockMargin = 2 * time.Hour

const musterAtAllSites = "all"

func (s *Server) registerMusterAtRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /muster/at", s.requireLogin(s.handleMusterAt))
	mux.HandleFunc("GET /muster/at.csv", s.requireLogin(s.handleMusterAtCSV))
	mux.HandleFunc("GET /last-seen", s.requireLogin(s.handleLastSeen))
}

type musterAtForm struct {
	Site, Date, Time string
}

type musterAtData struct {
	Cfg            *config.Config
	SiteID         string
	Banner         []deadline.Alert
	BannerErr      string
	AlertsReadOnly bool
	DispatchPast   dispatchPastView

	Dispatch bool
	Form     musterAtForm
	Sites    []facetOption
	AllowAll bool
	Asked    bool
	Problems []string
	Err      string
	Moment   string // "2026-10-05 14:30"
	Margin   string
	Elapsed  string
	Sheets   []musterAtSheet
	CSVHref  string // the download of exactly these sheets
}

// musterAtSheet is one post's list at the moment.
type musterAtSheet struct {
	SiteID   string
	AsOf     string
	Board    domain.MusterBoard
	Marks    map[string][]string // visit id → markers
	PersonOf map[string]string   // visit id → canonical person id
	Coverage string              // a peer whose records stop short of the moment
	Road     []musterAtRun
	RoadN    int
	Gaps     []musterAtGap
	Unplaced []musterAtPlace
}

type musterAtPerson struct {
	PersonID string // the record the "last seen" link opens
	Name     string
	Marks    []string
}

type musterAtRun struct {
	TripID   string
	Vehicle  string
	Where    string // "to Ice Pad" / "out at Conoco pad — no gate, no timer" / "working on the road"
	Left     string
	LeftHand string
	Later    string // what the record says happened after the moment
	Persons  []musterAtPerson
}

type musterAtGap struct {
	musterAtPerson
	Text string
}

type musterAtPlace struct {
	musterAtPerson
	Site string
	In   string
	Out  string
	Why  string
}

func (s *Server) handleMusterAt(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, "muster at", "muster_at.html", s.musterAtPage(r))
}

// musterAtPage reads the form and, when it names a moment, builds the
// sheets — shared by the page and its CSV download so the two cannot list
// different people.
func (s *Server) musterAtPage(r *http.Request) musterAtData {
	q := r.URL.Query()
	get := func(k string) string { return strings.TrimSpace(q.Get(k)) }
	data := musterAtData{Cfg: s.Cfg, SiteID: s.Cfg.SiteID, Dispatch: dispatchHost(r.Host),
		Margin: "2 hours"}
	data.AlertsReadOnly = data.Dispatch
	data.AllowAll = data.Dispatch
	data.Banner, data.BannerErr = s.banner()
	if data.Dispatch {
		var err error
		if data.DispatchPast, err = s.dispatchPastAllowance(time.UnixMilli(s.clockReading().WallMS)); err != nil {
			log.Printf("muster at: all-sites overdue: %v", err)
		}
		data.Banner = append(data.Banner, data.DispatchPast.Rows...)
		sortAlertsWorstFirst(data.Banner)
	}
	data.Form = musterAtForm{Site: get("site"), Date: get("date"), Time: get("time")}
	if data.Form.Site == "" {
		data.Form.Site = s.Cfg.SiteID
		if data.AllowAll {
			data.Form.Site = musterAtAllSites
		}
	}
	known := s.searchSiteIDs()
	for _, id := range known {
		data.Sites = append(data.Sites, facetOption{Value: id, Label: s.siteFilterLabel(id), Checked: id == data.Form.Site})
	}
	if data.Form.Date == "" && data.Form.Time == "" {
		return data
	}
	data.Asked = true
	if data.Form.Site == musterAtAllSites && !data.AllowAll || data.Form.Site != musterAtAllSites && !contains(known, data.Form.Site) {
		data.Problems = append(data.Problems, "Site: choose a post from the list.")
	}
	day, err := time.ParseInLocation("2006-01-02", data.Form.Date, time.Local)
	if err != nil {
		data.Problems = append(data.Problems, "Date: use a date like 2026-10-05.")
	}
	tod, err := time.Parse("15:04", data.Form.Time)
	if err != nil {
		data.Problems = append(data.Problems, "Time: use a 24-hour time like 07:30.")
	}
	if len(data.Problems) > 0 {
		return data
	}
	at := time.Date(day.Year(), day.Month(), day.Day(), tod.Hour(), tod.Minute(), 0, 0, time.Local)
	data.Moment = at.Format("2006-01-02 15:04")
	// The whole minute counts: someone signed in at 14:30:40 was there "at 14:30".
	atMS := at.Add(time.Minute - time.Millisecond).UnixMilli()
	sites := []string{data.Form.Site}
	if data.Form.Site == musterAtAllSites {
		sites = known
	}
	began := time.Now()
	if err := s.buildMusterAt(&data, sites, atMS, at); err != nil {
		log.Printf("muster at: %v", err)
		data.Err = refusalText(err)
		data.Sheets = nil
	}
	data.Elapsed = time.Since(began).Round(time.Millisecond).String()
	if data.Err == "" {
		data.CSVHref = "/muster/at.csv?" + url.Values{"site": {data.Form.Site}, "date": {data.Form.Date}, "time": {data.Form.Time}}.Encode()
	}
	return data
}

// musterAtCSVHeader is the download's columns, in order.
var musterAtCSVHeader = []string{"as_of", "post", "section", "person", "company", "trade", "detail", "marks"}

// handleMusterAtCSV is GET /muster/at.csv: the same sheets as /muster/at,
// one row per person listed, through writeCSVDownload (BOM, formula guard,
// attachment name). Loud on every failure — never an empty file that reads
// as "nobody was there".
func (s *Server) handleMusterAtCSV(w http.ResponseWriter, r *http.Request) {
	data := s.musterAtPage(r)
	switch {
	case !data.Asked:
		http.Error(w, "no moment chosen — pick a site, a date and a time on /muster/at first, then download", http.StatusBadRequest)
		return
	case len(data.Problems) > 0:
		http.Error(w, "not built — "+strings.Join(data.Problems, " "), http.StatusBadRequest)
		return
	case data.Err != "":
		http.Error(w, "could not build the muster — "+data.Err+". No file was produced.", http.StatusInternalServerError)
		return
	}
	var rows [][]string
	for _, sh := range data.Sheets {
		post := s.Cfg.SiteName(sh.SiteID)
		add := func(section, person, company, trade, detail string, marks []string) {
			rows = append(rows, []string{data.Moment, post, section, person, company, trade, detail, strings.Join(marks, "; ")})
		}
		member := func(m domain.MusterMember) {
			add("on site", m.PersonName, m.CompanyName, m.TradeName, "", sh.Marks[m.VisitID])
		}
		for _, c := range sh.Board.Companies {
			for _, t := range c.Trades {
				for _, m := range t.Members {
					m.CompanyName, m.TradeName = c.CompanyName, t.TradeName
					member(m)
				}
			}
		}
		for _, m := range sh.Board.Summary {
			member(m)
		}
		for _, run := range sh.Road {
			for _, p := range run.Persons {
				add("on the road", p.Name, "", "", run.Vehicle+" — "+run.Where+"; left "+run.Left, p.Marks)
			}
		}
		for _, g := range sh.Gaps {
			add("not tracked", g.Name, "", "", g.Text, nil)
		}
		for _, u := range sh.Unplaced {
			add("could not be placed", u.Name, "", "", "in "+u.In+"; out "+u.Out, []string{u.Why})
		}
	}
	// sitecontrol-muster-at-<site>-<moment>-<downloaded>.csv
	moment := strings.NewReplacer("-", "", ":", "", " ", "-").Replace(data.Moment)
	s.writeCSVDownload(w, "muster-at-"+data.Form.Site+"-"+moment, musterAtCSVHeader, rows)
}

// handMark is the FIX8/FIX9 wording for a typed or corrected instant.
func handMark(in domain.RecordedInstant, what string) string {
	switch {
	case in.Corrected():
		return what + " corrected by hand"
	case in.ByHand():
		return what + " entered by hand"
	}
	return ""
}

func (s *Server) buildMusterAt(data *musterAtData, sites []string, atMS int64, at time.Time) error {
	l, err := domain.LoadMomentLog(s.Store, func(r domain.MomentRun) bool {
		return !r.Dispatch.OK || r.Dispatch.MS <= atMS
	})
	if err != nil {
		return err
	}
	persons, err := domain.Persons(s.Store)
	if err != nil {
		return err
	}
	// rosterName is the on-site roster's own naming for a record that the
	// board does not cover (Could not be placed): the roster name of THAT
	// record, else the name the visit recorded, else the id. The person key
	// is never resolved through a merge here ([[D-50]] / W3-7).
	byID := make(map[string]string, len(persons))
	for _, p := range persons {
		byID[p.PersonID] = p.Name
	}
	snapOf := map[string]string{}
	for _, v := range l.Visits {
		if v.PersonName != "" {
			snapOf[v.VisitPersonID] = v.PersonName
		}
	}
	rosterName := func(raw string) string {
		if n := byID[raw]; n != "" {
			return n
		}
		if n := snapOf[raw]; n != "" {
			return n
		}
		return raw
	}
	visits, err := domain.Visits(s.Store)
	if err != nil {
		return err
	}
	visitRow := make(map[string]domain.Visit, len(visits))
	for _, v := range visits {
		visitRow[v.VisitID] = v
	}
	companies, err := domain.Companies(s.Store)
	if err != nil {
		return err
	}
	trades, err := domain.Trades(s.Store)
	if err != nil {
		return err
	}
	companyMerge, err := domain.LoadCompanyMergeMap(s.Store)
	if err != nil {
		return err
	}
	tradeMerge, err := domain.LoadTradeMergeMap(s.Store)
	if err != nil {
		return err
	}
	live, err := domain.NewLiveVehicles(s.Store)
	if err != nil {
		return err
	}
	state := l.At(atMS, musterAtClockMargin)

	// Who is on site where at the moment (person record → sites), for the
	// "also on site at" mark — the live board's M-6 tag, at the moment, by
	// the record as recorded (no merge: D-50 / W3-7).
	onSiteAt := map[string][]string{}
	for _, mv := range state.OnSite {
		if !contains(onSiteAt[mv.Rec.VisitPersonID], mv.Rec.SiteID) {
			onSiteAt[mv.Rec.VisitPersonID] = append(onSiteAt[mv.Rec.VisitPersonID], mv.Rec.SiteID)
		}
	}
	alsoAt := func(raw, except string) []string {
		var out []string
		for _, id := range onSiteAt[raw] {
			if id != except {
				out = append(out, "also on site at "+s.Cfg.SiteName(id))
			}
		}
		return out
	}
	lastOf := map[string]domain.RecordedInstant{}
	for _, m := range l.Streams {
		lastOf[m.SiteID] = m.Last // epoch-major: the last stream wins
	}
	stamp := func(in domain.RecordedInstant) (string, string) { return s.searchStamp(in) }

	for _, site := range sites {
		sh := musterAtSheet{SiteID: site, Marks: map[string][]string{}, PersonOf: map[string]string{},
			AsOf: fmt.Sprintf("as of %s on %s, %s's clock", at.Format("15:04"), at.Format("2006-01-02"), s.Cfg.SiteName(site))}
		if site != s.Cfg.SiteID {
			if last, ok := lastOf[site]; !ok || last.Label == "" {
				sh.Coverage = fmt.Sprintf("No records from %s have reached %s — this list for %s is empty because nothing is here, not because nobody was there.",
					s.Cfg.SiteName(site), s.Cfg.PostName(), s.Cfg.SiteName(site))
			} else if last.OK && last.MS < atMS {
				t, _ := stamp(last)
				sh.Coverage = fmt.Sprintf("%s's records here end at %s. Anything recorded there after that has not reached %s yet, so this list for %s may be incomplete.",
					s.Cfg.SiteName(site), t, s.Cfg.PostName(), s.Cfg.SiteName(site))
			}
		}
		var rows []domain.Visit
		for _, mv := range state.OnSite {
			if mv.Rec.SiteID != site {
				continue
			}
			v, ok := visitRow[mv.Rec.VisitID]
			if !ok {
				// Not yet folded here: stand the row up from the report.
				v = domain.Visit{VisitID: mv.Rec.VisitID, SiteID: site, PersonID: mv.Rec.VisitPersonID}
			}
			v.IsOpen = 1 // open AT THE MOMENT — the board lists only open rows
			rows = append(rows, v)
			sh.PersonOf[mv.Rec.VisitID] = mv.Rec.VisitPersonID
			var marks []string
			if h := handMark(mv.In, "time in"); h != "" {
				marks = append(marks, h)
			}
			if mv.In.UntrustedClock() {
				marks = append(marks, "time in uncertain")
			}
			if mv.NoSignOut {
				marks = append(marks, "no sign-out recorded — the record was closed by their next check-in, so they may have left earlier")
			} else if mv.Out.Label != "" {
				if h := handMark(mv.Out, "time out"); h != "" {
					marks = append(marks, h)
				}
				if mv.Out.UntrustedClock() {
					marks = append(marks, "time out uncertain")
				}
			}
			marks = append(marks, alsoAt(mv.Rec.VisitPersonID, site)...)
			if len(marks) > 0 {
				sh.Marks[mv.Rec.VisitID] = marks
			}
		}
		sh.Board = domain.MusterLive(rows, persons, companies, trades, companyMerge, tradeMerge, s.Cfg.MusterMinGroupSize)

		// On the road: runs this post put out (the live sheet's ownership —
		// the origin's; in_transit.go).
		for _, a := range state.OnRoad {
			r := a.Run
			if r.OriginSite != site {
				continue
			}
			c := r.Composed
			row := musterAtRun{TripID: r.TripID}
			if r.End == nil {
				if row.Vehicle, _, err = live.Compose(*c); err != nil {
					return err
				}
			} else {
				row.Vehicle = tripVehicleDisplay(c.Trip.VehicleIsUnidentified(), c.Trip.VehicleEntered)
			}
			switch r.Kind {
			case domain.RunRoadWork:
				row.Where = "working on the road"
			case domain.RunToPad:
				row.Where = "out at " + s.padName(r.Pad()) + " — no gate, no timer"
			default:
				row.Where = "on the way to " + s.Cfg.SiteName(r.Destination())
			}
			row.Left, row.LeftHand = stamp(r.Dispatch)
			switch {
			case a.EndUnknown:
				row.Later = "the record of its end cannot be read here"
			case r.End == nil:
				row.Later = "still out — no arrival recorded yet"
			case r.End.Cancelled:
				t, _ := stamp(r.End.At)
				row.Later = "later cancelled, " + t
			case r.TurnedBack():
				t, _ := stamp(r.End.At)
				row.Later = "later turned back — returned to " + s.Cfg.SiteName(r.OriginSite) + " " + t
			case r.Kind == domain.RunToPad && r.End.At.OK && r.End.At.MS <= atMS:
				row.Later = "its return had been recorded without these people"
			default:
				t, _ := stamp(r.End.At)
				row.Later = "arrived " + t
			}
			seen := map[string]bool{}
			for _, raw := range a.PersonIDs {
				ref, err := l.OccupantRef(s.Store, raw)
				if err != nil {
					return err
				}
				if seen[ref.PersonID] {
					continue
				}
				seen[ref.PersonID] = true
				row.Persons = append(row.Persons, musterAtPerson{PersonID: ref.PersonID, Name: ref.Name, Marks: alsoAt(raw, "")})
			}
			sort.SliceStable(row.Persons, func(i, j int) bool { return row.Persons[i].Name < row.Persons[j].Name })
			sh.RoadN += len(row.Persons)
			sh.Road = append(sh.Road, row)
		}

		// Not tracked: runs from or to this post.
		for _, g := range state.NotTracked {
			r := g.Run
			if r.OriginSite != site && r.Destination() != site {
				continue
			}
			ref, err := l.OccupantRef(s.Store, g.PersonID)
			if err != nil {
				return err
			}
			p := musterAtPerson{PersonID: ref.PersonID, Name: ref.Name}
			vehicle := tripVehicleDisplay(r.Composed.Trip.VehicleIsUnidentified(), r.Composed.Trip.VehicleEntered)
			dest := s.Cfg.SiteName(r.Destination())
			switch r.Kind {
			case domain.RunRoadWork:
				dest = s.Cfg.RoadWorkLabel()
			case domain.RunToPad:
				dest = s.padName(r.Pad())
			}
			since, _ := stamp(g.Since)
			var why string
			switch g.Why {
			case domain.GapTakenOffByCorrection:
				why = "taken off this run by a trip correction"
			case domain.GapRunCancelled:
				why = "the run was cancelled " + since
			default:
				why = "not aboard when it arrived " + since
			}
			text := fmt.Sprintf("left %s on %s to %s, %s — whereabouts not recorded", s.Cfg.SiteName(r.OriginSite), vehicle, dest, why)
			sh.Gaps = append(sh.Gaps, musterAtGap{musterAtPerson: p, Text: text})
		}
		sort.SliceStable(sh.Gaps, func(i, j int) bool { return sh.Gaps[i].Name < sh.Gaps[j].Name })

		for _, mv := range state.Unplaced {
			if mv.Rec.SiteID != site {
				continue
			}
			raw := mv.Rec.VisitPersonID
			p := musterAtPerson{PersonID: raw, Name: rosterName(raw)}
			pl := musterAtPlace{musterAtPerson: p, Site: site, Why: mv.Why}
			pl.In, _ = stamp(mv.In)
			if mv.Rec.ClosedBy == domain.VisitStillOpen {
				pl.Out = "still on site"
			} else {
				pl.Out, _ = stamp(mv.Out)
			}
			sh.Unplaced = append(sh.Unplaced, pl)
		}
		sort.SliceStable(sh.Unplaced, func(i, j int) bool { return sh.Unplaced[i].Name < sh.Unplaced[j].Name })
		data.Sheets = append(data.Sheets, sh)
	}
	return nil
}
