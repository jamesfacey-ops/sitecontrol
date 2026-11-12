// analytics_site.go — GET /analytics/site/{site}: one site's READ-ONLY, LIVE
// detail, reached by clicking the site's name on /analytics.
//
// James, 2026-10-03: "On the dispatch screen the site name needs to be a
// link to the detailed information on a read-only basis but have it live."
//
// WHAT IT SHOWS, and where each part comes from — nothing here is a new
// derivation; every section is the SAME read the site's own post renders,
// pointed at that site's id over this post's replicated copy of the log:
//
//   - On site: domain.OpenVisitsAt(site) through domain.MusterLive — the
//     site's own /muster board (Company → Trade).
//   - On the road FROM the site: domain.InTransitFrom(site) — the site's own
//     muster "in transit" section (D-6: the origin owns them).
//   - On the road TO the site: domain.OpenExpectations(site) — the site's
//     own /trips inbound board, and the deadline scanner's input.
//   - Out at uncontrolled destinations: domain.OutAtUncontrolledFrom(site).
//   - Working on the road FROM the site: domain.RoadWorkFrom(site) — the
//     site's own muster road-work section. ⟦D4, James 2026-10-04: "every
//     reader that switches on trip kind needs a road-work arm"⟧ ⟦was: no
//     such section — InTransitFrom excludes road work by design, so a crew
//     out working on the road was on no list and in no count of this page
//     while the /analytics tile and the site's muster both had them
//     (FINDINGS-R4 D4)⟧
//   - Freshness: the SAME §8.4 verdict /analytics gives the site's card
//     (fillAnalytics is called, not re-implemented), so the two pages can
//     never disagree about whether a site is current.
//
// DEADLINES — WHO MAY SAY "OVERDUE". C7's verdict is minted at the two posts
// ON the run: the DESTINATION (deadline.OnApplied captures trips bound here)
// and — NEW-a, James 2026-10-03 — the ORIGIN, which scans the runs it sent
// out from its own dispatch instant. So a run bound for OR dispatched from
// THIS post carries the scan's own verdict (countdown / OVERDUE / verify by
// radio). Any other run carries only what the record supports: the
// allowance, the dispatch instant BY THE ORIGIN'S CLOCK, and, once that
// instant plus the allowance has passed on this post's clock, "PAST
// ALLOWANCE — verify by radio", labelled as a comparison across two posts'
// clocks and never as OVERDUE. A stale site turns every one of its rows to
// verify-by-radio: a stale copy may simply be missing the arrival. ⟦was:
// "minted only at the DESTINATION post … a run bound for THIS post" —
// re-ruled with NEW-a, not silently edited⟧
//
// READ-ONLY, STRUCTURALLY: the route is GET only, every other method on the
// path is refused 405 by an explicit handler (so no catch-all can ever
// answer it), the template has no <form> and no <button>, and the page data
// carries no Banner field, so the top bar's alert strip — which holds the
// acknowledgment forms — is never mounted here. Nothing is authored by
// loading it. Behind the same login as /analytics (PII-1: names to an
// authenticated viewer only). The ONE place a phone number appears is the
// response block on an OVERDUE / PAST ALLOWANCE run (FIX16, James
// 2026-10-06), in full, for the people aboard that run; nobody merely on
// site has theirs shown. ⟦was: "no phone number is carried by any field
// here"⟧
//
// LIVE: a <meta http-equiv="refresh"> every analyticsLiveSeconds (60 s,
// James 2026-10-03; ⟦was: 15 s⟧), pausable by ?live=0 like /analytics —
// works with scripting off, needs no storage — and an "as at" render time.
package web

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/humantime"
	"sitecontrol/internal/store"
)

// registerAnalyticsSiteRoutes mounts the detail view. Called from
// registerAnalyticsRoutes.
func (s *Server) registerAnalyticsSiteRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /analytics/site/{site}", s.requireLogin(s.handleAnalyticsSite))
	// Every writing method: refused, explicitly, so the refusal does not rest
	// on which other patterns happen to be registered. (A method-less
	// pattern here would collide with web.go's "GET /" and panic at mount.)
	refuse := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "the site detail view is read-only — nothing can be recorded here", http.StatusMethodNotAllowed)
	}
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		mux.HandleFunc(m+" /analytics/site/{site}", refuse)
	}
}

// anRunView is one vehicle run on the detail page, in either direction.
type anRunView struct {
	TripID    string
	Vehicle   string
	Other     string // the far end, with its direction: "→ <destination>" or "from <origin>"
	Driver    string
	Persons   []domain.OccupantRef
	Allowance string // "" = none recorded
	// Dispatched: the dispatch instant by the ORIGIN's clock, labelled as
	// such; "" when it could not be resolved here.
	Dispatched string
	// Urgency is the row's state as a word AND a class, never hue alone:
	//   overdue    C7 OVERDUE (live)         — only for runs bound for THIS post
	//   unresolved C7 verify by radio        — only for runs bound for THIS post
	//   due        C7 countdown              — only for runs bound for THIS post
	//   past       allowance passed by the origin's clock — verify by radio
	//   stale      the site's copy is stale — verify by radio
	//   within     inside the allowance by the origin's clock
	//   unknown    nothing on record to judge it by
	Urgency      string
	UrgencyLabel string
	Detail       string
	// ⟦was: Override string — the "PHASE OVERRIDE: …" marker; removed with
	// weather phases, James 2026-10-04⟧
	RoadWork     bool // D4: Allowance is the run's WORKING LIMIT, and there is no far end

	// FIX16 stream B (James, 2026-10-06: overdue response info "fold into
	// the site page, no need for a paper copy, so if it's not new leave it
	// out"). On an OVERDUE or PAST-ALLOWANCE row only, the response
	// essentials this card did not already carry: the route with BOTH ends
	// named (the card names only the far end), the deadline as a clock time
	// (the card gave only the allowance), and each person aboard with their
	// company and full phone. Truck, driver, dispatch time and how long
	// overdue were already on the card and are not repeated.
	Late     bool
	Route    string         // "<origin> → <destination>" by display name
	Deadline string         // "" when it cannot be told here
	Contacts []anRunContact // everyone aboard, driver included, in Persons order
}

// anRunContact is one person aboard a late run, as a responder needs them.
// The page is behind the login (PII-1: the phone goes to an authenticated
// viewer only, in full — James 2026-10-05 "show the full phone number").
type anRunContact struct {
	Name    string
	Company string // "" = none recorded
	Phone   string // "" = none on file
}

// Count is how many PEOPLE the run carries (see domain.InTransitRow.Count).
func (r anRunView) Count() int { return len(r.Persons) }

type anAwayView struct {
	Pad     string
	Vehicle string
	Persons []domain.OccupantRef
}

type analyticsSiteData struct {
	Cfg    *config.Config
	SiteID string         // THIS post, for the top bar
	Sync   syncStatusView // the top bar's sync chip
	// deliberately NO Banner field: see the file comment.

	Refresh    int  // analyticsLiveSeconds; 0 when paused
	Paused     bool // ?live=0 (James, 2026-10-03)
	RenderedAt string
	Err        string
	NotFound   bool

	Site analyticsSiteView // the same card /analytics renders for this site
	Name string            // configured display name, "" when this post configures none

	Board domain.MusterBoard
	// RestoredVisits / RestoredCount (James, 2026-10-03: RESTORED markers
	// like the muster) — web/muster.go's restoredVisitSet over this board.
	// Non-empty only when the site IS this post (R-23's own-tag scope).
	RestoredVisits map[string]bool
	RestoredCount  int
	// Elsewhere / ElsewhereCount (M-6 / T-L08-C): the same "also recorded
	// on site at <peer>" flags the site's own muster carries, from the same
	// replicated rows, so this page and that one never disagree.
	Elsewhere      map[string][]elsewhereTag
	ElsewhereCount int
	Outbound       []anRunView
	Inbound       []anRunView
	Away          []anAwayView
	OutboundCount int
	InboundCount  int
	AwayCount     int
	// RoadWork / RoadWorkCount (D4, James 2026-10-04): the site's crews out
	// WORKING ON THE ROAD — PEOPLE, counted apart from travel, as the
	// site's own muster counts them.
	RoadWork      []anRunView
	RoadWorkCount int
	DeadlineNote  string // why runs bound for this post carry no C7 verdict, when they do not
}

func (s *Server) handleAnalyticsSite(w http.ResponseWriter, r *http.Request) {
	site := r.PathValue("site")
	now := time.UnixMilli(s.clockReading().WallMS)
	data := analyticsSiteData{Cfg: s.Cfg, SiteID: s.Cfg.SiteID,
		RenderedAt: now.In(time.Local).Format("2006-01-02 15:04:05")}
	data.Refresh, data.Paused = analyticsLive(r)
	data.Sync = s.buildSyncSection()
	fail := func(what string, err error) {
		// Loud, never a partial page (analytics.go's rule).
		log.Printf("analytics site %s: %s: %v", site, what, err)
		data.Err = refusalText(err)
		s.renderPage(w, "analytics site", "analytics_site.html", data)
	}

	fig, err := domain.BuildAnalytics(s.Store, now, time.Local)
	if err != nil {
		fail("figures", err)
		return
	}
	var all analyticsData
	fillAnalytics(&all, fig, data.Sync, s.Cfg.SiteID, now)
	found := false
	stale := map[string]bool{}
	for _, v := range all.Sites {
		if v.Stale {
			stale[v.SiteID] = true
		}
		if v.SiteID == site {
			data.Site, found = v, true
		}
	}
	if !found {
		// Only a site /analytics itself lists has a detail page — never a
		// typed id that would render as an empty, confident "0 on site".
		data.NotFound = true
		data.Site.SiteID = site
		s.renderPageStatus(w, http.StatusNotFound, "analytics site", "analytics_site.html", data)
		return
	}
	// James, 2026-10-04 (FINDINGS-R4 cluster I): the heading is the site's
	// display name (sitelabel) plus, only when one is configured, its LONG
	// name — never the display name twice ("STP STP") and never the raw id
	// beside the name ("ice-road 2P Ice Road Entrance").
	data.Name = s.Cfg.SiteLongName(site)
	nameOr := func(id string) string {
		return s.Cfg.SiteName(id)
	}

	// On site — the site's own muster board.
	open, err := domain.OpenVisitsAt(s.Store, site)
	if err != nil {
		fail("visits", err)
		return
	}
	persons, err := domain.Persons(s.Store)
	if err != nil {
		fail("persons", err)
		return
	}
	companies, err := domain.Companies(s.Store)
	if err != nil {
		fail("companies", err)
		return
	}
	trades, err := domain.Trades(s.Store)
	if err != nil {
		fail("trades", err)
		return
	}
	companyMerge, err := domain.LoadCompanyMergeMap(s.Store)
	if err != nil {
		fail("company merge map", err)
		return
	}
	tradeMerge, err := domain.LoadTradeMergeMap(s.Store)
	if err != nil {
		fail("trade merge map", err)
		return
	}
	data.Board = domain.MusterLive(open, persons, companies, trades, companyMerge, tradeMerge, s.Cfg.MusterMinGroupSize)
	// contacts: a late run's people with company and phone (FIX16), from
	// the same roster and company reads as the board above. The person is
	// looked up by the id the run carries, NOT through the person merge map:
	// D-50 / W3-7 forbid person merge resolution on this read path
	// (TestW6TheWebMusterReadPathNeverResolvesThePersonKey).
	byPerson := make(map[string]domain.Person, len(persons))
	for _, p := range persons {
		byPerson[p.PersonID] = p
	}
	companyName := make(map[string]string, len(companies))
	for _, c := range companies {
		companyName[c.CompanyID] = c.Name
	}
	contacts := func(ps []domain.OccupantRef) []anRunContact {
		out := make([]anRunContact, 0, len(ps))
		for _, o := range ps {
			c := anRunContact{Name: o.Name}
			if p, ok := byPerson[o.PersonID]; ok {
				if p.CompanyID != nil {
					id := companyMerge.Resolve(*p.CompanyID)
					if c.Company = companyName[id]; c.Company == "" {
						c.Company = companyName[*p.CompanyID]
					}
				}
				if p.Phone != nil {
					c.Phone = strings.TrimSpace(*p.Phone)
				}
			}
			out = append(out, c)
		}
		return out
	}
	// James, 2026-10-03: the muster's two per-name markers, from the same
	// reads — RESTORED (R-23, own site only by construction) and ALSO ON
	// SITE AT <peer> (M-6).
	data.RestoredVisits, data.RestoredCount = s.restoredVisitSet(open, data.Board)
	if data.Elsewhere, err = s.elsewhereTags(site, open); err != nil {
		fail("also on site elsewhere", err)
		return
	}
	data.ElsewhereCount = countElsewhere(data.Board, data.Elsewhere)

	// C7's verdicts, for the runs THIS post is party to: bound for it, or
	// (NEW-a) sent out from it.
	verdict := map[string]deadline.Alert{}
	if s.Alerts != nil {
		if board, err := s.Alerts.Evaluate(); err == nil {
			for _, a := range board.Alerts {
				verdict[a.TripID] = a
			}
		} else {
			log.Printf("analytics site %s: deadline scan: %v", site, err)
			data.DeadlineNote = "The deadline scan failed at this post, so no run carries an overdue verdict — verify by radio."
		}
	} else {
		data.DeadlineNote = "This post runs no deadline scan, so no run carries an overdue verdict — verify by radio."
	}
	judge := func(rv *anRunView, t domain.Trip, dest string) {
		defer func() { lateEssentials(rv, s.Cfg.SiteName(t.OriginSite), dest, s.Cfg.SiteName(dest), contacts) }()
		rv.Allowance = allowanceText(t.DeadlineMinutes)
		// FIX3 F3d (FINDINGS-R5 L11-RAW-1, L15 TEXT-2; config.go's rule "no
		// screen shows a raw site id"): every post named in a run row is
		// named by its display name. ⟦was: t.OriginSite / dest, raw — "by
		// ice-road's clock", "only ice-road or ice-pad can call it overdue".⟧
		origin, destName := s.Cfg.SiteName(t.OriginSite), s.Cfg.SiteName(dest)
		at, okAt := s.dispatchInstant(t)
		if okAt {
			rv.Dispatched = fmt.Sprintf("%s (%s's clock)", at.In(time.Local).Format("15:04"), origin)
		}
		if (dest == s.Cfg.SiteID || t.OriginSite == s.Cfg.SiteID) && data.DeadlineNote == "" {
			a, ok := verdict[t.TripID]
			switch {
			case !ok:
				rv.Urgency, rv.UrgencyLabel = "unknown", "NO VERDICT"
				rv.Detail = "this post's deadline scan holds no expectation for this run"
			case a.Status == deadline.StatusOverdueLive:
				rv.Urgency, rv.UrgencyLabel = "overdue", "OVERDUE"
				rv.Detail = "by " + a.OverdueByText()
				// The deadline as a clock time (FIX16): the recorded due
				// instant on the origin's clock when the scan holds one,
				// else the instant this post's own count ran out.
				if !a.DueAt.IsZero() {
					rv.Deadline = fmt.Sprintf("%s (%s's clock)", a.DueClock(), origin)
				} else {
					rv.Deadline = fmt.Sprintf("%s (%s's clock)", now.Add(-a.OverdueBy).In(time.Local).Format("15:04"), s.Cfg.SiteName(s.Cfg.SiteID))
				}
			case a.Status == deadline.StatusOverdueUnresolved:
				rv.Urgency, rv.UrgencyLabel = "unresolved", "VERIFY BY RADIO"
				rv.Detail = a.Reason
			default:
				rv.Urgency, rv.UrgencyLabel = "due", "DUE"
				rv.Detail = a.RemainingText() + " remaining"
			}
			return
		}
		switch {
		case stale[t.OriginSite] || stale[dest]:
			rv.Urgency, rv.UrgencyLabel = "stale", "VERIFY BY RADIO"
			rv.Detail = "this post's copy of a site on this run is STALE — the arrival may already be recorded there"
		case !okAt || t.DeadlineMinutes == nil:
			rv.Urgency, rv.UrgencyLabel = "unknown", "NO ALLOWANCE ON RECORD"
			rv.Detail = "nothing here to judge this run by — verify by radio"
		default:
			// ONE time basis with the two ends of the run (FINDINGS-R4 D13).
			// James 2026-10-06: from the typed time. Every judge counts the
			// allowance from the dispatch's effective departure
			// (domain.EffectiveDeparture: its typed time, else its
			// authored_at — recordedBasis), and the ends take the same typed
			// lead off their own countdowns. ⟦was: D13, from the instant
			// the dispatch was RECORDED, never a backdated occurred-at;
			// before that, due := at.Add(X) from the corrected
			// occurred-at⟧ The resolved occurred-at stays on the face as
			// "dispatched"; the basis is shown beside it when the two
			// differ (a later time correction, or a future typed time).
			basis := s.recordedBasis(t, at)
			if d := basis.Sub(at); d >= time.Minute || d <= -time.Minute {
				rv.Dispatched = fmt.Sprintf("%s, recorded %s (%s's clock)", at.In(time.Local).Format("15:04"),
					basis.In(time.Local).Format("15:04"), origin)
			}
			due := basis.Add(time.Duration(*t.DeadlineMinutes) * time.Minute)
			if now.After(due) {
				rv.Deadline = fmt.Sprintf("%s (%s's clock)", due.In(time.Local).Format("15:04"), origin) // FIX16
				rv.Urgency, rv.UrgencyLabel = "past", "PAST ALLOWANCE"
				rv.Detail = fmt.Sprintf("by %s (%s's clock against this post's) — verify by radio; only %s or %s can call it overdue",
					humantime.Span(now.Sub(due)), origin, origin, destName)
				if dest == "" { // road work (D4): no destination — the origin alone owns its clock
					rv.UrgencyLabel = "PAST WORKING LIMIT"
					rv.Detail = fmt.Sprintf("by %s (%s's clock against this post's) — verify by radio; only %s can call it overdue",
						humantime.Span(now.Sub(due)), origin, origin)
				}
			} else {
				rv.Urgency, rv.UrgencyLabel = "within", "WITHIN ALLOWANCE"
				rv.Detail = fmt.Sprintf("due about %s by %s's clock", due.In(time.Local).Format("15:04"), origin)
				if dest == "" { // road work (D4): a working limit, not a journey allowance
					rv.UrgencyLabel = "WITHIN WORKING LIMIT"
					rv.Detail = fmt.Sprintf("limit reached about %s by %s's clock", due.In(time.Local).Format("15:04"), origin)
				}
			}
		}
	}

	// On the road FROM the site — the site's own muster section.
	outs, err := domain.InTransitFrom(s.Store, site)
	if err != nil {
		fail("in transit", err)
		return
	}
	for _, row := range outs {
		c, err := domain.EffectiveTrip(s.Store, row.TripID)
		if err != nil {
			fail("trip", err)
			return
		}
		rv := anRunView{TripID: row.TripID, Vehicle: row.Vehicle, Other: "→ " + nameOr(row.Destination),
			Driver: row.Driver, Persons: row.Persons}
		if c != nil {
			judge(&rv, c.Trip, row.Destination)
		}
		data.Outbound = append(data.Outbound, rv)
		data.OutboundCount += rv.Count()
	}

	// On the road TO the site — the site's own inbound board.
	exps, err := domain.OpenExpectations(s.Store, site)
	if err != nil {
		fail("inbound", err)
		return
	}
	for _, e := range exps {
		t := e.Trip
		// FINDINGS-R4 D11 (L18 SITE-3): the inbound run names the truck LIVE
		// (Expectation.Vehicle, LiveVehicles — DR-W7-18), the same truck the
		// origin's outbound list above and this site's own /trips inbound
		// board name. ⟦was: tripVehicleDisplay(…, t.VehicleEntered)⟧ — the
		// as-dispatched snapshot, which kept the old number after a renumber.
		rv := anRunView{TripID: t.TripID, Vehicle: e.Vehicle,
			Other: "from " + nameOr(t.OriginSite), Driver: e.DriverName, Persons: e.OccupantList}
		if rv.Driver == "" {
			rv.Driver = t.DriverPersonID
		}
		judge(&rv, t, site)
		data.Inbound = append(data.Inbound, rv)
		data.InboundCount += rv.Count()
	}

	// Out at an uncontrolled destination, dispatched from the site.
	aways, err := domain.OutAtUncontrolledFrom(s.Store, site)
	if err != nil {
		fail("away", err)
		return
	}
	for _, o := range aways {
		pad := s.padName(o.Pad)
		data.Away = append(data.Away, anAwayView{Pad: pad, Vehicle: o.Vehicle, Persons: o.Persons})
		data.AwayCount += len(o.Persons)
	}

	// Working on the road FROM the site (D4) — the site's own muster
	// section, judged like an outbound run with no destination: the scan's
	// verdict when THIS post is the origin, else the limit against the
	// origin's dispatch instant.
	rws, err := domain.RoadWorkFrom(s.Store, site)
	if err != nil {
		fail("road work", err)
		return
	}
	for _, row := range rws {
		c, err := domain.EffectiveTrip(s.Store, row.TripID)
		if err != nil {
			fail("trip", err)
			return
		}
		rv := anRunView{TripID: row.TripID, Vehicle: row.Vehicle, Other: "— " + domain.RoadWorkDestinationText,
			Driver: row.Driver, Persons: row.Persons, RoadWork: true}
		if c != nil {
			judge(&rv, c.Trip, "")
		}
		data.RoadWork = append(data.RoadWork, rv)
		data.RoadWorkCount += rv.Count()
	}

	s.renderPage(w, "analytics site", "analytics_site.html", data)
}

// lateEssentials fills the FIX16 response block on an OVERDUE or PAST
// ALLOWANCE row; any other row is left as it was.
func lateEssentials(rv *anRunView, originName, dest, destName string, contacts func([]domain.OccupantRef) []anRunContact) {
	if rv.Urgency != "overdue" && rv.Urgency != "past" {
		return
	}
	rv.Late = true
	if dest == "" {
		destName = domain.RoadWorkDestinationText
	}
	rv.Route = originName + " → " + destName
	rv.Contacts = contacts(rv.Persons)
}

// dispatchInstant is the trip's dispatch instant as its ORIGIN recorded it
// (time corrections applied) — the same resolution the analytics figures
// read every instant through. ok=false when it cannot be resolved here.
func (s *Server) dispatchInstant(t domain.Trip) (time.Time, bool) {
	rt, _, err := domain.ResolveOccurredAt(s.Store, store.Key{SiteID: t.OriginSite, NodeEpoch: t.NodeEpoch, Seq: t.Seq})
	if err != nil {
		return time.Time{}, false
	}
	return time.UnixMilli(rt.InstantUTCms), true
}

// recordedBasis is the instant a third post counts a run's allowance from:
// the dispatch's effective departure on its origin's clock — James
// 2026-10-06: from the typed time (domain.EffectiveDeparture, the one basis
// the deadline scan at both ends uses too) ⟦was: FINDINGS-R4 D13, the
// envelope's authored_at⟧ — falling back to the resolved dispatch instant
// at when the envelope is not in this store. Shared by the site page's PAST
// ALLOWANCE verdict and the dispatch view's all-sites list
// (dispatch_allsites.go), so the two can never disagree about a due time.
func (s *Server) recordedBasis(t domain.Trip, at time.Time) time.Time {
	if dep, _, found, err := domain.DispatchDeparture(s.Store, store.Key{SiteID: t.OriginSite, NodeEpoch: t.NodeEpoch, Seq: t.Seq}); err == nil && found {
		return dep
	}
	return at
}

// allowanceText is domain.InTransitRow.Allowance for a bare pointer.
func allowanceText(m *int64) string {
	return domain.InTransitRow{DeadlineMinutes: m}.Allowance()
}
