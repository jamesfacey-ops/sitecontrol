// signout_history.go — the missed sign-outs report, following the person and
// the company (FIX16f, James 2026-10-06: "broken down by site as well as for
// dispatch as a whole, following the person and company").
//
// NOT A FORK of `sitectl -report signout-failures`. The verdict is the one
// VisitReport already stamped on each record (rec.Failure, from
// classifyReturn: R-4 a visit closed by the person's own return with no
// departure recorded, at a text-message site; R-5 camp exempt; a site whose
// type this post was not told gets no verdict). This file only groups those
// records: by site, by person (merge-resolved, as SignoutFailuresByPerson
// keys them), by company, and by week.
//
// THE COMPANY is the one recorded at check-in on the arrival that opened the
// visit (VisitAsRecorded, the search's "Company (at check-in)"), else the
// person's current company on the roster, merge-resolved. Grouped by name,
// case and spacing folded, so a company typed two ways at two gates is one
// row.
//
// THE DATE of a missed sign-out is the day the visit was SIGNED IN — the
// visit nobody signed out of. (It is counted only once the person's next
// pass closes it; visit_report.go "WHAT IS NOT DERIVED".) Weeks start on
// Monday, in the post's local time.
package domain

import (
	"sort"
	"strings"
	"time"

	"sitecontrol/internal/store"
)

// SignoutFilter narrows the report. Zero values do not filter. FromMS/ToMS
// bound the sign-in instant, [From, To).
type SignoutFilter struct {
	Site         string
	FromMS, ToMS int64
}

// MissedSignout is one visit nobody signed out of.
type MissedSignout struct {
	VisitRecord
	Company   string // as recorded at check-in, else the roster's; "" = none
	WeekStart string // "2006-01-02", the Monday of the sign-in's week
}

// SignoutGroup is one person's or one company's tally.
type SignoutGroup struct {
	Key    string // person id, or the folded company name
	Name   string
	Count  int
	Sites  []string       // where, sorted
	ByWeek map[string]int // week start -> count
	People int            // companies only: distinct persons
	// LastMS: the latest sign-in among them.
	LastMS int64
}

// Repeat reports a repeat offender: more than one missed sign-out in the
// period.
func (g SignoutGroup) Repeat() bool { return g.Count > 1 }

// SiteSignouts is one site's line of the per-site breakdown.
type SiteSignouts struct {
	SiteID   string
	SiteType SiteType // "" when this post was not told it
	Missed   int
	// Exempt: returns at a camp (R-5); Unclassified: returns at a site of
	// unknown type — could not be judged here.
	Exempt, Unclassified int
	People, Companies    int
}

// WeekSignouts is one week's line.
type WeekSignouts struct {
	WeekStart         string
	Missed            int
	People, Companies int
}

// SignoutHistory is the whole report for one filter.
type SignoutHistory struct {
	Rows      []MissedSignout // sign-in order, newest first
	BySite    []SiteSignouts
	ByPerson  []SignoutGroup // most first
	ByCompany []SignoutGroup
	ByWeek    []WeekSignouts // oldest first
	Untyped   []string       // sites in range whose type this post does not know
	Summary   VisitReportSummary
}

// companyKey folds a company name for grouping.
func companyKey(name string) string { return strings.ToLower(foldNameSpaces(strings.TrimSpace(name))) }

// weekStart is the Monday of t's week in loc, as a date.
func weekStart(ms int64, loc *time.Location) string {
	t := time.UnixMilli(ms).In(loc)
	off := (int(t.Weekday()) + 6) % 7
	d := time.Date(t.Year(), t.Month(), t.Day()-off, 0, 0, 0, 0, loc)
	return d.Format("2006-01-02")
}

// BuildSignoutHistory runs the visit report with the types this post knows
// and groups its missed sign-outs. Read-only.
func BuildSignoutHistory(st *store.Store, types SiteTypes, f SignoutFilter, loc *time.Location) (SignoutHistory, error) {
	recs, sum, err := VisitReport(st, types)
	if err != nil {
		return SignoutHistory{}, err
	}
	companyOf, err := visitCompanies(st)
	if err != nil {
		return SignoutHistory{}, err
	}
	return GroupSignouts(recs, sum, companyOf, f, loc), nil
}

// visitCompanies returns, for a visit record, its company name: at check-in,
// else the merge-resolved person's roster company.
func visitCompanies(st *store.Store) (func(VisitRecord) string, error) {
	instants, err := LoadTripInstants(st)
	if err != nil {
		return nil, err
	}
	persons, err := Persons(st)
	if err != nil {
		return nil, err
	}
	companies, err := Companies(st)
	if err != nil {
		return nil, err
	}
	merge, err := LoadCompanyMergeMap(st)
	if err != nil {
		return nil, err
	}
	label := map[string]string{} // company id -> its name, looked up whole
	for _, c := range companies {
		label[c.CompanyID] = c.Name
	}
	roster := map[string]string{}
	for _, p := range persons {
		if p.CompanyID != nil {
			roster[p.PersonID] = label[merge.Resolve(*p.CompanyID)]
		}
	}
	return func(r VisitRecord) string {
		if c := strings.TrimSpace(instants.Visit[r.VisitID].Company); c != "" {
			return c
		}
		if c := roster[r.PersonID]; c != "" {
			return c
		}
		return roster[r.VisitPersonID]
	}, nil
}

// GroupSignouts is the pure grouping, for tests and the page alike.
func GroupSignouts(recs []VisitRecord, sum VisitReportSummary, companyOf func(VisitRecord) string, f SignoutFilter, loc *time.Location) SignoutHistory {
	out := SignoutHistory{Summary: sum}
	inRange := func(r VisitRecord) bool {
		if f.Site != "" && r.SiteID != f.Site {
			return false
		}
		if f.FromMS != 0 && (r.ArrivalLabel == timeUnresolvable || r.ArrivedAtMS < f.FromMS) {
			return false
		}
		if f.ToMS != 0 && (r.ArrivalLabel == timeUnresolvable || r.ArrivedAtMS >= f.ToMS) {
			return false
		}
		return true
	}
	site := map[string]*SiteSignouts{}
	sitePeople := map[string]map[string]bool{}
	siteCos := map[string]map[string]bool{}
	persons := map[string]*SignoutGroup{}
	cos := map[string]*SignoutGroup{}
	coPeople := map[string]map[string]bool{}
	weeks := map[string]*WeekSignouts{}
	weekPeople := map[string]map[string]bool{}
	weekCos := map[string]map[string]bool{}
	groupSites := map[*SignoutGroup]map[string]bool{}
	add := func(m map[string]map[string]bool, k, v string) {
		if m[k] == nil {
			m[k] = map[string]bool{}
		}
		m[k][v] = true
	}
	bump := func(g *SignoutGroup, ms int64, wk, siteID string) {
		g.Count++
		if g.ByWeek == nil {
			g.ByWeek = map[string]int{}
		}
		g.ByWeek[wk]++
		if ms > g.LastMS {
			g.LastMS = ms
		}
		if groupSites[g] == nil {
			groupSites[g] = map[string]bool{}
		}
		groupSites[g][siteID] = true
	}
	untyped := map[string]bool{}
	for _, r := range recs {
		if !inRange(r) {
			continue
		}
		s := site[r.SiteID]
		if s == nil {
			s = &SiteSignouts{SiteID: r.SiteID, SiteType: r.SiteType}
			site[r.SiteID] = s
		}
		switch classifyReturn(r.ClosedBy, r.SiteType) {
		case verdictExemptCamp:
			s.Exempt++
		case verdictUnclassified:
			s.Unclassified++
			untyped[r.SiteID] = true
		}
		if r.Failure != FailureMissedSignout {
			continue
		}
		s.Missed++
		wk := weekStart(r.ArrivedAtMS, loc)
		co := strings.TrimSpace(companyOf(r))
		ck := companyKey(co)
		out.Rows = append(out.Rows, MissedSignout{VisitRecord: r, Company: co, WeekStart: wk})
		pk := r.PersonID
		if pk == "" {
			pk = "visit:" + r.VisitID // nobody to name: its own row, never pooled
		}
		add(sitePeople, r.SiteID, pk)
		add(siteCos, r.SiteID, ck)
		p := persons[pk]
		if p == nil {
			p = &SignoutGroup{Key: pk, Name: r.PersonName}
			persons[pk] = p
		}
		bump(p, r.ArrivedAtMS, wk, r.SiteID)
		c := cos[ck]
		if c == nil {
			c = &SignoutGroup{Key: ck, Name: co}
			cos[ck] = c
		}
		bump(c, r.ArrivedAtMS, wk, r.SiteID)
		add(coPeople, ck, pk)
		w := weeks[wk]
		if w == nil {
			w = &WeekSignouts{WeekStart: wk}
			weeks[wk] = w
		}
		w.Missed++
		add(weekPeople, wk, pk)
		add(weekCos, wk, ck)
	}
	for id, s := range site {
		s.People, s.Companies = len(sitePeople[id]), len(siteCos[id])
		out.BySite = append(out.BySite, *s)
	}
	sort.Slice(out.BySite, func(i, j int) bool { return out.BySite[i].SiteID < out.BySite[j].SiteID })
	finish := func(m map[string]*SignoutGroup) []SignoutGroup {
		var gs []SignoutGroup
		for _, g := range m {
			for s := range groupSites[g] {
				g.Sites = append(g.Sites, s)
			}
			sort.Strings(g.Sites)
			gs = append(gs, *g)
		}
		sort.Slice(gs, func(i, j int) bool {
			if gs[i].Count != gs[j].Count {
				return gs[i].Count > gs[j].Count
			}
			if gs[i].Name != gs[j].Name {
				return gs[i].Name < gs[j].Name
			}
			return gs[i].Key < gs[j].Key
		})
		return gs
	}
	for k, g := range cos {
		g.People = len(coPeople[k])
	}
	out.ByPerson, out.ByCompany = finish(persons), finish(cos)
	for wk, w := range weeks {
		w.People, w.Companies = len(weekPeople[wk]), len(weekCos[wk])
		out.ByWeek = append(out.ByWeek, *w)
	}
	sort.Slice(out.ByWeek, func(i, j int) bool { return out.ByWeek[i].WeekStart < out.ByWeek[j].WeekStart })
	sort.SliceStable(out.Rows, func(i, j int) bool { return out.Rows[i].ArrivedAtMS > out.Rows[j].ArrivedAtMS })
	for s := range untyped {
		out.Untyped = append(out.Untyped, s)
	}
	sort.Strings(out.Untyped)
	return out
}
