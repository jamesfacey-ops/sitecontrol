// search_refine.go — the "Narrow results" panel on GET /search (FIX12, James
// 2026-10-05: "include filters in the searching so that we can filter the
// results after searching and site should be included on the search and
// filters too"). search.go finds the records; this file narrows what it
// found.
//
// WHAT IT IS. A refinement ANDed onto the search terms: Site (any of the
// ticked posts), Result type, Company (from the results themselves), Status
// (per result type) and "Entered by hand". Within one facet the ticked
// values combine with OR; the facets combine with AND. Every option shows
// the count it would leave: the rows that pass every OTHER facet and carry
// that value (disjunctive counts, so ticking a second site never shows a
// misleading zero beside the first).
//
// ONE PASS. Counts and the narrowed lists come from a single walk over the
// rows the search already built (each row carries its facet keys,
// searchRowKeys) — no query per option, no second search.
//
// STATE IN THE URL. Everything is a GET parameter (in_site, in_type,
// in_company, in_status, in_hand), so a narrowed search is a link: it can be
// shared, Back undoes a filter, and it works with scripting off. Nothing is
// stored anywhere else. A new search from the main form starts unnarrowed.
package web

import (
	"hash/fnv"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"

	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
)

// The refinement parameters.
const (
	refineSite    = "in_site"
	refineType    = "in_type"
	refineCompany = "in_company"
	refineStatus  = "in_status"
	refineHand    = "in_hand"
)

// searchCompanyTop is how many companies the panel lists before "more".
const searchCompanyTop = 8

var searchTypes = []struct{ key, label string }{
	{"people", "People"}, {"visits", "Visits"}, {"trips", "Trips"},
}

// searchStatuses: each result type's status values, in panel order.
var searchStatuses = []struct {
	typ, legend string
	opts        []struct{ key, label string }
}{
	{"people", "People", []struct{ key, label string }{{"person-onsite", "On site now"}, {"person-offsite", "Not on site"}}},
	{"visits", "Visits", []struct{ key, label string }{{"visit-open", "Open"}, {"visit-closed", "Closed"}}},
	{"trips", "Trips", []struct{ key, label string }{{"trip-road", "On the road"}, {"trip-arrived", "Arrived"},
		{"trip-back", "Turned back"}, {"trip-overdue", "Overdue"}}},
}

func searchStatusLabel(key string) string {
	for _, g := range searchStatuses {
		for _, o := range g.opts {
			if o.key == key {
				return g.legend + " — " + strings.ToLower(o.label)
			}
		}
	}
	return key
}

func searchTypeLabel(key string) string {
	for _, t := range searchTypes {
		if t.key == key {
			return t.label
		}
	}
	return key
}

// tripStatusKey is a run's Status facet value: overdue (its deadline has
// passed at this post's scanner) before on the road; turned back before
// arrived. "" while its state is not yet known here.
func tripStatusKey(state string, returned, overdue bool) string {
	switch {
	case state == domain.TripStateOpen && overdue:
		return "trip-overdue"
	case state == domain.TripStateOpen:
		return "trip-road"
	case state == domain.TripStateArrived && returned:
		return "trip-back"
	case state == domain.TripStateArrived:
		return "trip-arrived"
	}
	return ""
}

func contains(list []string, v string) bool { return slices.Contains(list, v) }

// searchRowKeys is one result row's facet values.
type searchRowKeys struct {
	sites     []string // site ids
	companies []string // searchKey of each company; "" = none recorded
	names     []string // display name per companies entry
	status    string
	hand      bool
}

func (k *searchRowKeys) addSite(id string) {
	if id != "" && !contains(k.sites, id) {
		k.sites = append(k.sites, id)
	}
}

func (k *searchRowKeys) addCompany(key, name string) {
	if !contains(k.companies, key) {
		k.companies = append(k.companies, key)
		k.names = append(k.names, strings.TrimSpace(name))
	}
}

// searchRefine is the narrowing as the URL carries it.
type searchRefine struct {
	Sites, Types, Companies, Statuses []string
	Hand                              bool
}

func parseSearchRefine(q url.Values) searchRefine {
	list := func(k string, valid func(string) bool) []string {
		var out []string
		for _, v := range q[k] {
			v = strings.TrimSpace(v)
			if (valid == nil || valid(v)) && !contains(out, v) {
				out = append(out, v)
			}
		}
		return out
	}
	r := searchRefine{
		Sites:     list(refineSite, func(v string) bool { return v != "" }),
		Types:     list(refineType, func(v string) bool { return searchTypeLabel(v) != v }),
		Companies: list(refineCompany, nil), // "" is "no company recorded"
		Statuses:  list(refineStatus, func(v string) bool { return searchStatusLabel(v) != v }),
		Hand:      q.Get(refineHand) != "",
	}
	return r
}

func (r searchRefine) Active() bool {
	return len(r.Sites)+len(r.Types)+len(r.Companies)+len(r.Statuses) > 0 || r.Hand
}

func (r searchRefine) addTo(v url.Values) {
	for _, s := range r.Sites {
		v.Add(refineSite, s)
	}
	for _, s := range r.Types {
		v.Add(refineType, s)
	}
	for _, s := range r.Companies {
		v.Add(refineCompany, s)
	}
	for _, s := range r.Statuses {
		v.Add(refineStatus, s)
	}
	if r.Hand {
		v.Set(refineHand, "1")
	}
}

// facetOption is one checkbox (or, in Sites, one select option).
type facetOption struct {
	ID, Value, Label string
	Count            int
	Checked          bool
}

type facetGroup struct {
	Legend  string
	Options []facetOption
}

type searchChip struct{ Label, Href string }

type searchHidden struct{ Name, Value string }

// searchNarrow is the panel and its chips, as rendered.
type searchNarrow struct {
	Hidden        []searchHidden // the search terms, carried by the panel's form
	Sites         []facetOption
	Types         []facetOption
	Companies     []facetOption
	CompaniesMore []facetOption
	Statuses      []facetGroup
	Hand          facetOption
	Chips         []searchChip
	ClearHref     string // the same search, unnarrowed
	// Before: the search's own counts, shown beside the narrowed ones.
	BeforeN [3]int
	// Shown: the result types listed (all three unless Type is ticked).
	ShowPeople, ShowVisits, ShowTrips bool
}

// searchSiteIDs is every post this one knows by name: itself, its peers,
// its controlled destinations (each a post) and the posts retired from the
// mesh, by display name. ⟦FIX17b, sc-stress F15⟧ a retired post's records
// stay in every replica, so search, the reports and the past-moment muster
// must still be able to name it (siteFilterLabel marks it retired). ⟦was:
// itself, peers and destinations only — a retired post vanished from every
// filter while its visits still appeared in all-sites search⟧
func (s *Server) searchSiteIDs() []string {
	var ids []string
	add := func(id string) {
		if id != "" && !contains(ids, id) {
			ids = append(ids, id)
		}
	}
	add(s.Cfg.SiteID)
	for _, p := range s.Cfg.Peers {
		add(p.SiteID)
	}
	for _, p := range s.Cfg.FormerPeers {
		add(p.SiteID)
	}
	for _, id := range s.Cfg.ControlledDestinationIDs() {
		add(id)
	}
	sort.SliceStable(ids, func(i, j int) bool { return s.Cfg.SiteName(ids[i]) < s.Cfg.SiteName(ids[j]) })
	return ids
}

// siteFilterLabel is a site's name in a history, report or search filter:
// its display name, marked "(retired)" for a post retired from the mesh.
func (s *Server) siteFilterLabel(id string) string {
	if s.Cfg.IsFormerPeer(id) {
		return s.Cfg.SiteName(id) + " (retired)"
	}
	return s.Cfg.SiteName(id)
}

// overdueTrips is the deadline scanner's overdue set — the same authority
// tripAlertIsOverdue asks. No scanner wired: none is overdue here.
func (s *Server) overdueTrips() (map[string]bool, error) {
	out := map[string]bool{}
	if s.Alerts == nil {
		return out, nil
	}
	board, err := s.Alerts.Evaluate()
	if err != nil {
		return nil, err
	}
	for _, a := range board.Alerts {
		if a.Status == deadline.StatusOverdueLive || a.Status == deadline.StatusOverdueUnresolved {
			out[a.TripID] = true
		}
	}
	return out, nil
}

// narrowSearch applies data.Refine to the found rows in one pass, fills
// data.Narrow (options, counts, chips) and returns the rows that remain.
func (s *Server) narrowSearch(data *searchData, people []searchPersonRow, visits []searchVisitRow, trips []searchTripRow) ([]searchPersonRow, []searchVisitRow, []searchTripRow) {
	r := data.Refine
	n := &data.Narrow
	n.BeforeN = [3]int{len(people), len(visits), len(trips)}

	const (
		fSite = iota
		fType
		fCompany
		fStatus
		fHand
		nFacets
	)
	siteN, typeN, companyN, statusN, handN := map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}, 0
	// Company labels: the commonest spelling of each key across the found
	// rows (ties to the first seen; rows arrive sorted, so it is stable).
	spell := map[string]map[string]int{}
	var spellOrder []string
	anyOf := func(have, want []string) bool {
		for _, v := range have {
			if contains(want, v) {
				return true
			}
		}
		return false
	}
	// pass decides one row: keep it, and count it into each facet it
	// passes every OTHER facet for.
	pass := func(typ string, k *searchRowKeys) bool {
		for i, key := range k.companies {
			if spell[key] == nil {
				spell[key] = map[string]int{}
				spellOrder = append(spellOrder, key)
			}
			spell[key][k.names[i]]++
		}
		var ok [nFacets]bool
		ok[fSite] = len(r.Sites) == 0 || anyOf(k.sites, r.Sites)
		ok[fType] = len(r.Types) == 0 || contains(r.Types, typ)
		ok[fCompany] = len(r.Companies) == 0 || anyOf(k.companies, r.Companies)
		ok[fStatus] = len(r.Statuses) == 0 || contains(r.Statuses, k.status)
		ok[fHand] = !r.Hand || k.hand
		failed := -1
		for i, v := range ok {
			if !v {
				if failed >= 0 {
					return false // fails two facets: counts toward none
				}
				failed = i
			}
		}
		if failed < 0 || failed == fSite {
			for _, id := range k.sites {
				siteN[id]++
			}
		}
		if failed < 0 || failed == fType {
			typeN[typ]++
		}
		if failed < 0 || failed == fCompany {
			for _, c := range k.companies {
				companyN[c]++
			}
		}
		if failed < 0 || failed == fStatus {
			statusN[k.status]++
		}
		if (failed < 0 || failed == fHand) && k.hand {
			handN++
		}
		return failed < 0
	}
	var keptP []searchPersonRow
	for i := range people {
		if pass("people", &people[i].keys) {
			keptP = append(keptP, people[i])
		}
	}
	var keptV []searchVisitRow
	for i := range visits {
		if pass("visits", &visits[i].keys) {
			keptV = append(keptV, visits[i])
		}
	}
	var keptT []searchTripRow
	for i := range trips {
		if pass("trips", &trips[i].keys) {
			keptT = append(keptT, trips[i])
		}
	}

	// --- the panel ---
	// Each checkbox's id is stable across narrowings (a hash of its value),
	// so the script can hand focus back to the box just ticked.
	id := func(prefix, value string) string {
		h := fnv.New32a()
		h.Write([]byte(value))
		return prefix + "-" + strconv.FormatUint(uint64(h.Sum32()), 36)
	}
	siteIDs := s.searchSiteIDs()
	for sid := range siteN {
		if !contains(siteIDs, sid) {
			siteIDs = append(siteIDs, sid)
		}
	}
	for _, sid := range r.Sites {
		if !contains(siteIDs, sid) {
			siteIDs = append(siteIDs, sid)
		}
	}
	sort.SliceStable(siteIDs, func(i, j int) bool { return s.Cfg.SiteName(siteIDs[i]) < s.Cfg.SiteName(siteIDs[j]) })
	for _, sid := range siteIDs {
		n.Sites = append(n.Sites, facetOption{ID: id("nf-site", sid), Value: sid, Label: s.siteFilterLabel(sid),
			Count: siteN[sid], Checked: contains(r.Sites, sid)})
	}
	for _, t := range searchTypes {
		n.Types = append(n.Types, facetOption{ID: id("nf-type", t.key), Value: t.key, Label: t.label,
			Count: typeN[t.key], Checked: contains(r.Types, t.key)})
	}
	companyLabel := func(key string) string {
		if key == "" {
			return "No company recorded"
		}
		best, bestN := key, 0
		if m := spell[key]; m != nil {
			// Deterministic: highest count, then alphabetical.
			names := make([]string, 0, len(m))
			for name := range m {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				if m[name] > bestN {
					best, bestN = name, m[name]
				}
			}
		}
		return best
	}
	var cos []facetOption
	for _, key := range spellOrder {
		if companyN[key] > 0 || contains(r.Companies, key) {
			cos = append(cos, facetOption{Value: key, Label: companyLabel(key), Count: companyN[key], Checked: contains(r.Companies, key)})
		}
	}
	for _, key := range r.Companies { // ticked, but in none of the found rows
		if spell[key] == nil {
			cos = append(cos, facetOption{Value: key, Label: companyLabel(key), Checked: true})
		}
	}
	sort.SliceStable(cos, func(i, j int) bool {
		if cos[i].Checked != cos[j].Checked {
			return cos[i].Checked
		}
		if cos[i].Count != cos[j].Count {
			return cos[i].Count > cos[j].Count
		}
		return strings.ToLower(cos[i].Label) < strings.ToLower(cos[j].Label)
	})
	for i := range cos {
		cos[i].ID = id("nf-company", cos[i].Value)
	}
	top := searchCompanyTop
	for top < len(cos) && cos[top].Checked { // a ticked company is never hidden under "more"
		top++
	}
	if len(cos) > top {
		n.Companies, n.CompaniesMore = cos[:top], cos[top:]
	} else {
		n.Companies = cos
	}
	for _, g := range searchStatuses {
		fg := facetGroup{Legend: g.legend}
		for _, o := range g.opts {
			fg.Options = append(fg.Options, facetOption{ID: id("nf-status", o.key), Value: o.key, Label: o.label,
				Count: statusN[o.key], Checked: contains(r.Statuses, o.key)})
		}
		n.Statuses = append(n.Statuses, fg)
	}
	n.Hand = facetOption{ID: "nf-hand", Value: "1", Label: "Entered by hand only", Count: handN, Checked: r.Hand}

	// --- the search terms, the chips, Clear filters ---
	terms := data.Form.values()
	for _, key := range searchFormKeys {
		if v := terms.Get(key); v != "" {
			n.Hidden = append(n.Hidden, searchHidden{Name: key, Value: v})
		}
	}
	link := func(rf searchRefine) string {
		v := data.Form.values()
		rf.addTo(v)
		return "/search?" + v.Encode() + "#results"
	}
	n.ClearHref = link(searchRefine{})
	without := func(list []string, v string) []string {
		var out []string
		for _, x := range list {
			if x != v {
				out = append(out, x)
			}
		}
		return out
	}
	for _, v := range r.Sites {
		rf := r
		rf.Sites = without(r.Sites, v)
		n.Chips = append(n.Chips, searchChip{Label: "Site: " + s.Cfg.SiteName(v), Href: link(rf)})
	}
	for _, v := range r.Types {
		rf := r
		rf.Types = without(r.Types, v)
		n.Chips = append(n.Chips, searchChip{Label: "Type: " + searchTypeLabel(v), Href: link(rf)})
	}
	for _, v := range r.Companies {
		rf := r
		rf.Companies = without(r.Companies, v)
		n.Chips = append(n.Chips, searchChip{Label: "Company: " + companyLabel(v), Href: link(rf)})
	}
	for _, v := range r.Statuses {
		rf := r
		rf.Statuses = without(r.Statuses, v)
		n.Chips = append(n.Chips, searchChip{Label: "Status: " + searchStatusLabel(v), Href: link(rf)})
	}
	if r.Hand {
		rf := r
		rf.Hand = false
		n.Chips = append(n.Chips, searchChip{Label: "Entered by hand only", Href: link(rf)})
	}
	n.ShowPeople = len(r.Types) == 0 || contains(r.Types, "people")
	n.ShowVisits = len(r.Types) == 0 || contains(r.Types, "visits")
	n.ShowTrips = len(r.Types) == 0 || contains(r.Types, "trips")
	return keptP, keptV, keptT
}
