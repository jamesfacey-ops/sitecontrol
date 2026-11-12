// last_seen.go — GET /last-seen?person=<id>: where the record last saw one
// person, across every post (FIX16a, James 2026-10-06). Their last sign-in,
// their last sign-out, their last trip (truck, route, times, who was
// aboard), which post, and how long ago — plus where the record places them
// right now (domain.MomentLog.At at this post's clock).
//
// Reached from a search result, a name on the muster for a moment, and the
// live muster's Not tracked rows. READ-ONLY: GET only, behind the login on
// every host, allowed on the dispatch host (dispatch_surface.go) with no
// console links. The phone is shown in full, as search shows it (PII-1:
// the page is login-only everywhere).
//
// Each time is the one its own post's clock recorded, labelled so; "ago"
// is measured on THIS post's clock, and the page says that too.
package web

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
)

type lastSeenData struct {
	Cfg            *config.Config
	SiteID         string
	Banner         []deadline.Alert
	BannerErr      string
	AlertsReadOnly bool
	DispatchPast   dispatchPastView

	Dispatch bool
	PersonID string
	Found    bool
	Err      string
	Elapsed  string

	Name, Company, Trade, Phone, EnrolledAt string
	Retired                                 bool
	Merged                                  int // other records folded into this one

	Now     string
	SignIn  *lastSeenVisit
	SignOut *lastSeenVisit
	Trip    *lastSeenTrip
}

type lastSeenVisit struct {
	Site      string
	In, Out   string
	InHand    string
	OutHand   string
	Open      bool
	How       string
	Ago       string
	ByVehicle string
}

type lastSeenTrip struct {
	TripID     string
	Vehicle    string
	From       string
	To         string
	ToSite     string
	Left       string
	LeftHand   string
	Arrived    string
	ArrHand    string
	Status     string
	Driver     string
	Aboard     []string
	Ago        string
	WasDriving bool
}

// agoText: how long before nowMS the instant was, in words.
func agoText(nowMS, ms int64) string {
	d := time.Duration(nowMS-ms) * time.Millisecond
	switch {
	case d < 0:
		return "after this post's clock reads now — the clocks disagree"
	case d < time.Minute:
		return "less than a minute ago"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h %d min ago", int(d/time.Hour), int(d%time.Hour/time.Minute))
	default:
		return fmt.Sprintf("%d days %d h ago", int(d/(24*time.Hour)), int(d%(24*time.Hour)/time.Hour))
	}
}

func (s *Server) handleLastSeen(w http.ResponseWriter, r *http.Request) {
	// person is a form field (which record to look up), read as search.go
	// reads its fields — not a notice: nothing is confirmed off it, and an
	// unknown id answers NOT FOUND.
	q := r.URL.Query()
	data := lastSeenData{Cfg: s.Cfg, SiteID: s.Cfg.SiteID, Dispatch: dispatchHost(r.Host),
		PersonID: strings.TrimSpace(q.Get("person"))}
	data.AlertsReadOnly = data.Dispatch
	data.Banner, data.BannerErr = s.banner()
	nowMS := s.clockReading().WallMS
	if data.Dispatch {
		var err error
		if data.DispatchPast, err = s.dispatchPastAllowance(time.UnixMilli(nowMS)); err != nil {
			log.Printf("last seen: all-sites overdue: %v", err)
		}
		data.Banner = append(data.Banner, data.DispatchPast.Rows...)
		sortAlertsWorstFirst(data.Banner)
	}
	began := time.Now()
	if data.PersonID != "" {
		if err := s.buildLastSeen(&data, nowMS); err != nil {
			log.Printf("last seen: %v", err)
			data.Err = refusalText(err)
			data.Found = false
		}
	}
	data.Elapsed = time.Since(began).Round(time.Millisecond).String()
	s.renderPage(w, "last seen", "last_seen.html", data)
}

func (s *Server) buildLastSeen(data *lastSeenData, nowMS int64) error {
	merge, err := domain.LoadPersonMergeMap(s.Store)
	if err != nil {
		return err
	}
	canon := merge.Resolve(data.PersonID)
	ids := map[string]bool{canon: true, data.PersonID: true}
	for from := range merge {
		if merge.Resolve(from) == canon {
			ids[from] = true
		}
	}
	data.Merged = len(ids) - 1
	if data.PersonID != canon {
		data.Merged = len(ids) - 2
	}
	amended, err := domain.AmendedTripIDs(s.Store)
	if err != nil {
		return err
	}
	l, err := domain.LoadMomentLog(s.Store, func(r domain.MomentRun) bool {
		if amended[r.TripID] {
			return true
		}
		for _, id := range r.RawAboard {
			if ids[id] {
				return true
			}
		}
		return false
	})
	if err != nil {
		return err
	}
	names, _, err := s.loadLastSeenNames(l)
	if err != nil {
		return err
	}
	p, onRoster := names.byID[canon]
	visits := l.PersonVisits(ids)
	if !onRoster && len(visits) == 0 {
		return nil // Found stays false: nobody by that id here
	}
	data.Found = true
	data.PersonID = canon
	if onRoster {
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
		pi := newPersonInfo(p, companyName, tradeName)
		data.Name, data.Company, data.Trade = p.Name, pi.company, pi.trade
		data.EnrolledAt, data.Retired = p.SiteID, p.IsRetired()
		if p.Phone != nil {
			data.Phone = strings.TrimSpace(*p.Phone)
		}
	} else {
		data.Name = names.person(canon).Name
	}

	// Last sign-in / sign-out, by each visit's own recorded instant.
	var in, out *domain.VisitRecord
	for i := range visits {
		v := &visits[i]
		if a := v.ArrivalInstant(); a.OK && (in == nil || a.MS > in.ArrivalInstant().MS) {
			in = v
		}
		if c := v.CloseInstant(); c.OK && c.Label != "" && (out == nil || c.MS > out.CloseInstant().MS) {
			out = v
		}
	}
	if in != nil {
		a := in.ArrivalInstant()
		lv := &lastSeenVisit{Site: in.SiteID, Open: in.ClosedBy == domain.VisitStillOpen, Ago: agoText(nowMS, a.MS)}
		lv.In, lv.InHand = s.searchStamp(a)
		if lv.Open {
			lv.Out = "still on site"
		} else if c := in.CloseInstant(); c.OK {
			lv.Out, lv.OutHand = s.searchStamp(c)
		} else {
			lv.Out = "closed — time not on record here"
		}
		data.SignIn = lv
	}
	if out != nil {
		c := out.CloseInstant()
		lv := &lastSeenVisit{Site: out.SiteID, Ago: agoText(nowMS, c.MS)}
		lv.In, lv.InHand = s.searchStamp(out.ArrivalInstant())
		lv.Out, lv.OutHand = s.searchStamp(c)
		switch out.ClosedBy {
		case domain.ClosedByCheckout:
			lv.How = "signed out"
		case domain.ClosedByDispatch:
			lv.How = "left on a truck"
		default:
			lv.How = "no sign-out recorded — the record was closed by their next check-in"
		}
		data.SignOut = lv
	}

	// Last trip: the latest run with them aboard as the record now stands.
	var last *domain.MomentRun
	for i := range l.Runs {
		r := &l.Runs[i]
		if r.Composed == nil || !r.Dispatch.OK {
			continue
		}
		aboard := ids[r.Composed.Trip.DriverPersonID]
		for _, id := range r.Composed.OccupantIDs {
			aboard = aboard || ids[id]
		}
		if aboard && (last == nil || r.Dispatch.MS > last.Dispatch.MS) {
			last = r
		}
	}
	if last != nil {
		data.Trip, err = s.lastSeenTrip(last, ids, names, nowMS)
		if err != nil {
			return err
		}
	}

	// Where the record places them now.
	state := l.At(nowMS, musterAtClockMargin)
	var now []string
	for _, mv := range state.OnSite {
		if ids[mv.Rec.VisitPersonID] {
			now = append(now, "on site at "+s.Cfg.SiteName(mv.Rec.SiteID))
		}
	}
	for _, a := range state.OnRoad {
		for _, id := range a.PersonIDs {
			if ids[id] {
				now = append(now, "on the road from "+s.Cfg.SiteName(a.Run.OriginSite))
				break
			}
		}
	}
	for _, g := range state.NotTracked {
		if ids[g.PersonID] {
			now = append(now, "not tracked — left "+s.Cfg.SiteName(g.Run.OriginSite)+" on a run and has no recorded whereabouts since")
		}
	}
	if len(now) == 0 {
		now = append(now, "not on site at any post, as far as the record here shows")
	}
	sort.Strings(now)
	data.Now = strings.Join(now, "; ")
	return nil
}

// lastSeenNames resolves raw person ids (last seen is a per-HUMAN read: every
// record linked to the person counts, so it merge-resolves; it is not the
// muster's roster read path) to canonical ids and names.
type lastSeenNames struct {
	merge  domain.AliasMap
	byID   map[string]domain.Person
	snapOf map[string]string // raw id → a name as a visit recorded it
}

func (n lastSeenNames) person(raw string) musterAtPerson {
	canon := n.merge.Resolve(raw)
	if p, ok := n.byID[canon]; ok && p.Name != "" {
		return musterAtPerson{PersonID: canon, Name: p.Name}
	}
	if p, ok := n.byID[raw]; ok && p.Name != "" {
		return musterAtPerson{PersonID: canon, Name: p.Name}
	}
	if name := n.snapOf[raw]; name != "" {
		return musterAtPerson{PersonID: canon, Name: name}
	}
	return musterAtPerson{PersonID: canon, Name: raw}
}

func (s *Server) loadLastSeenNames(l *domain.MomentLog) (lastSeenNames, []domain.Person, error) {
	persons, err := domain.Persons(s.Store)
	if err != nil {
		return lastSeenNames{}, nil, err
	}
	merge, err := domain.LoadPersonMergeMap(s.Store)
	if err != nil {
		return lastSeenNames{}, nil, err
	}
	n := lastSeenNames{merge: merge, byID: make(map[string]domain.Person, len(persons)), snapOf: map[string]string{}}
	for _, p := range persons {
		n.byID[p.PersonID] = p
	}
	for _, v := range l.Visits {
		if v.PersonName != "" {
			n.snapOf[v.VisitPersonID] = v.PersonName
		}
	}
	return n, persons, nil
}

func (s *Server) lastSeenTrip(r *domain.MomentRun, ids map[string]bool, names lastSeenNames, nowMS int64) (*lastSeenTrip, error) {
	c := r.Composed
	t := &lastSeenTrip{TripID: r.TripID, From: r.OriginSite, Ago: agoText(nowMS, r.Dispatch.MS),
		WasDriving: ids[c.Trip.DriverPersonID]}
	if r.End == nil {
		live, err := domain.NewLiveVehicles(s.Store)
		if err != nil {
			return nil, err
		}
		if t.Vehicle, _, err = live.Compose(*c); err != nil {
			return nil, err
		}
	} else {
		t.Vehicle = tripVehicleDisplay(c.Trip.VehicleIsUnidentified(), c.Trip.VehicleEntered)
	}
	switch r.Kind {
	case domain.RunRoadWork:
		t.To = s.Cfg.RoadWorkLabel()
	case domain.RunToPad:
		t.To = s.padName(r.Pad()) + " (uncontrolled — no timer)"
	default:
		t.To, t.ToSite = s.Cfg.SiteName(r.Destination()), r.Destination()
	}
	t.Left, t.LeftHand = s.searchStamp(r.Dispatch)
	switch {
	case r.End == nil:
		t.Status = "still out — no arrival recorded"
	case r.End.Cancelled:
		t.Status = "cancelled"
		t.Arrived, _ = s.searchStamp(r.End.At)
	case r.TurnedBack():
		t.Status = "turned back — returned to " + s.Cfg.SiteName(r.OriginSite)
		t.Arrived, t.ArrHand = s.searchStamp(r.End.At)
	default:
		t.Status = "arrived"
		t.Arrived, t.ArrHand = s.searchStamp(r.End.At)
	}
	if c.Trip.DriverPersonID != "" {
		t.Driver = names.person(c.Trip.DriverPersonID).Name
	}
	driver := names.merge.Resolve(c.Trip.DriverPersonID)
	seen := map[string]bool{}
	for _, id := range c.OccupantIDs {
		p := names.person(id)
		if p.PersonID == driver || seen[p.PersonID] {
			continue
		}
		seen[p.PersonID] = true
		t.Aboard = append(t.Aboard, p.Name)
	}
	sort.Strings(t.Aboard)
	return t, nil
}
