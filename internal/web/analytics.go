// analytics.go — GET /analytics: the dispatch-analytics page. Overview across
// every site this post holds records for, then one card per site, each with
// its own data-freshness line.
//
// THE RULE THE PAGE EXISTS TO KEEP (design/analytics-v1.md §8.4): a figure
// from a stalled peer is not shown as current. A site is CURRENT only when it
// is this post itself, or a configured peer whose sync verdict is Healthy
// (syncstatus.go — the one place Healthy is minted). Every other site — a
// stalled or unreachable peer, or a site this post holds records for but has
// no sync verdict on (engine not wired, or not a configured peer) — is STALE:
// its card is marked LAST KNOWN, its figures are visibly demoted, and its
// persons are LEFT OUT of the overview's "on site now" total, which names
// them separately instead. An outage therefore reads as a warning on the
// page, never as a quietly lower number.
//
// Read-only and compute-on-request (§7.3): no event is authored, nothing is
// cached. Figures come from domain.BuildAnalytics.
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
	"sitecontrol/internal/humantime"
)

// analyticsLiveSeconds — James, 2026-10-03: ONLY /analytics and
// /analytics/site/{site} auto-refresh, both every 60 seconds, and each
// carries a visible "Pause live updates" link. ⟦was: /analytics 30 s
// (orchestrator default), the site page 15 s⟧ A wall screen by
// <meta http-equiv="refresh"> (works with scripting off, no storage). Safe
// to refresh: /analytics' only form is the alert strip's single-click
// Acknowledge POST, and the site page has no form at all — nothing typed is
// ever lost between loads. No other page refreshes itself.
const analyticsLiveSeconds = 60

// analyticsLive reads the pause switch both live pages share. A plain link
// (?live=0) — no script, no storage, so it works with scripting off and is
// remembered only by the URL the viewer is on: the page then renders with
// NO meta refresh and says "Live updates paused — Resume". Anything else
// (absent, or any other value) is live. James, 2026-10-03.
func analyticsLive(r *http.Request) (refresh int, paused bool) {
	if r.URL.Query().Get("live") == "0" {
		return 0, true
	}
	return analyticsLiveSeconds, false
}

// registerAnalyticsRoutes mounts the analytics page. Called from Handler().
func (s *Server) registerAnalyticsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /analytics", s.requireLogin(s.handleAnalytics))
	s.registerAnalyticsSiteRoutes(mux) // one site's read-only live detail, analytics_site.go
}

type analyticsSiteView struct {
	SiteID string
	Own    bool
	Stale  bool
	// Freshness is the ONE sentence about how current this site's figures
	// are: this post's own live state, or the sync verdict for a peer.
	Freshness string
	LastEvent string // "" when no event of this site resolved an instant

	OnSiteNow       int
	VisitsToday     int
	VisitsSeason    int
	AvgToday        string
	AvgSeason       string
	TravelOutToday  int
	TravelOutSeason int
	TravelInToday   int
	TravelInSeason  int
	// RoadWorkOutToday / RoadWorkOutSeason (James, 2026-10-03): equipment
	// this site sent out working on the road — its own line, never travel.
	RoadWorkOutToday  int
	RoadWorkOutSeason int
	Events            int64
	// Restored (James, 2026-10-03, orchestrator default: "analytics pages
	// show RESTORED markers like the muster"): how many of THIS post's own
	// open visits this incarnation never observed — rows under our tag at a
	// node_epoch below the running one, recovered from the peer's copy
	// (domain.Visit.RestoredFromPeer; R-23). Set on the OWN card only: the
	// classification is scoped to our own tag — every copy of another
	// site's presence is second-hand by nature, so a peer's card carries no
	// such count (and says nothing, rather than a misleading 0).
	Restored int
}

type analyticsData struct {
	Cfg       *config.Config
	SiteID    string
	Banner    []deadline.Alert
	BannerErr string
	Sync      syncStatusView

	RenderedAt string
	Day        string
	Err        string
	Refresh    int  // analyticsLiveSeconds — page_open's meta refresh; 0 when paused
	Paused     bool // ?live=0: rendered without the meta refresh (James, 2026-10-03)
	// AlertsReadOnly (FIX7, James 2026-10-05: Dispatch reads, never
	// edits): the top bar's alert strip renders each alert's information
	// with no ack form and no link into the operator console. Always true
	// here; dispatch_surface.go enforces the same on the dispatch host.
	AlertsReadOnly bool

	// Overview.
	TotalOnSite                           int      // distinct persons at CURRENT sites only
	ExcludedOnSite                        int      // distinct persons last-known at STALE sites, not counted above
	StaleSites                            []string // ids of the stale sites, for the warning
	StaleSiteNames                        []string // the same sites by display name, same order
	TripsDispatched, TripsDispatchedToday int
	TripsArrived, TripsArrivedToday       int
	TripsTurnedBack, TripsTurnedBackToday int // James, 2026-10-04 (R8): shown apart from arrived
	TripsInTransit                        int
	// RoadWork… (James, 2026-10-03): the working-on-the-road figures — sent
	// out today/season, out now, and how many of THIS post's own are past
	// their limit (the scan's own verdict, counted apart from inbound
	// overdue because the two answer different questions).
	RoadWorkDispatched, RoadWorkDispatchedToday int
	RoadWorkOut                                 int
	RoadWorkOverdue                             int
	RoadWorkLabel                               string
	OverdueKnown                                bool
	// OverdueLive: deadline passed on a live link, this post can measure it;
	// OverdueUnresolved: the review lane — verify by radio. Of those,
	// OverdueUncounted (FINDINGS-R4 D15) are runs this post cannot count at
	// all (no sync anchor at receipt, a changed clock, replay) — NOT known
	// to be past their deadline, and the tile must not say they are. BOTH count every run this post is party to — inbound
	// to it AND (NEW-a, James 2026-10-03) outbound from it — because the
	// scan now judges both halves; the inbound/outbound split is shown
	// beside the total so the figure is never ambiguous about which it is.
	OverdueLive       int
	OverdueUnresolved int
	OverdueUncounted  int // of OverdueUnresolved, deadline not known to have passed (a.DeadlineUnknown)
	OverduePastRadio  int // of OverdueUnresolved, known past (passed during a partition): the rest
	OverdueOutbound   int // of OverdueLive + OverdueUnresolved, the runs this post SENT OUT
	OverdueInbound    int // … and the runs bound for this post
	// … and RoadWorkOverdue (above): this post's own road-work runs past
	// their working limit — inbound + outbound + road work = the total.
	TimesUnresolved   int
	// RestoredOwn: the own card's Restored, repeated beside the overview
	// total so a projector reader sees it without scrolling to the cards.
	RestoredOwn int

	// FIX14 (dispatch_allsites.go): on the dispatch host only, the late
	// runs this post is NOT on, judged by the origin's recorded time —
	// merged into Banner and counted in OverdueLive/OverdueUnresolved;
	// OverdueOther is how many of those are such runs. DispatchPastErr:
	// that read failed; the tile says so.
	DispatchView    bool
	DispatchPast    dispatchPastView
	DispatchPastErr string
	OverdueOther    int

	Sites []analyticsSiteView

	// FIX15 (James, 2026-10-06): the Signal tile — every configured peer as
	// OK, LOST or CHECK with when it was last heard (lostsignal.go; the
	// same judgement as the top bar's LOST SIGNAL line).
	Signal          []signalSiteView
	SignalOK        int
	SignalLost      int
	SignalRule      string // the full threshold sentence, the tile's title
	SignalRuleShort string // the same threshold, briefly, under the list
}

func (s *Server) handleAnalytics(w http.ResponseWriter, r *http.Request) {
	now := time.UnixMilli(s.clockReading().WallMS)
	data := analyticsData{Cfg: s.Cfg, SiteID: s.Cfg.SiteID, RoadWorkLabel: s.Cfg.RoadWorkLabel(), AlertsReadOnly: true}
	data.Refresh, data.Paused = analyticsLive(r)
	data.Banner, data.BannerErr = s.banner()
	data.Sync = s.buildSyncSection()
	fillSignal(&data, s.Cfg.SiteName, s.Cfg.PostName(), now)
	if data.DispatchView = dispatchHost(r.Host); data.DispatchView {
		var err error
		if data.DispatchPast, err = s.dispatchPastAllowance(now); err != nil {
			log.Printf("analytics: all-sites overdue: %v", err)
			data.DispatchPastErr = refusalText(err)
		}
		data.Banner = append(data.Banner, data.DispatchPast.Rows...)
		sortAlertsWorstFirst(data.Banner)
	}

	fig, err := domain.BuildAnalytics(s.Store, now, time.Local)
	if err != nil {
		// Loud, never a partial page: a half-computed dashboard reads as a
		// complete one.
		log.Printf("analytics: %v", err)
		data.Err = refusalText(err)
		s.renderPage(w, "analytics", "analytics.html", data)
		return
	}
	fillAnalytics(&data, fig, data.Sync, s.Cfg.SiteID, now)
	data.RoadWorkLabel = roadWorkTileLabel(s.Cfg, fig)

	// James, 2026-10-03: RESTORED markers here like the muster. The same
	// classification the muster and the gate roster apply (R-23), over this
	// post's own open visits; loud, never a partial page, on a read failure.
	own, err := domain.OpenVisitsAt(s.Store, s.Cfg.SiteID)
	if err != nil {
		log.Printf("analytics: own visits: %v", err)
		data.Err = err.Error()
		s.renderPage(w, "analytics", "analytics.html", data)
		return
	}
	for _, v := range own {
		if v.RestoredFromPeer(s.Cfg.SiteID, s.Cfg.NodeEpoch) {
			data.RestoredOwn++
		}
	}
	for i := range data.Sites {
		if data.Sites[i].Own {
			data.Sites[i].Restored = data.RestoredOwn
		}
	}

	if s.Alerts != nil {
		if board, err := s.Alerts.Evaluate(); err == nil {
			data.OverdueKnown = true
			for _, a := range board.Alerts {
				switch a.Status {
				case deadline.StatusOverdueLive:
					data.OverdueLive++
				case deadline.StatusOverdueUnresolved:
					data.OverdueUnresolved++
					if a.DeadlineUnknown {
						data.OverdueUncounted++
					} else {
						data.OverduePastRadio++
					}
				default:
					continue
				}
				// NEW-a: which half of the run this post holds — or neither:
				// equipment past its working limit (James, 2026-10-03) has
				// no far end, so it is the split's own third part.
				// ⟦D12-related, James 2026-10-04: "every reader that switches
				// on trip kind needs a road-work arm"⟧ ⟦was: road work
				// skipped "overdue now" entirely (`continue`), so the tile
				// said nothing was overdue while the red bar above it named
				// an overdue grader (FINDINGS-R4 L20)⟧
				if a.RoadWork {
					data.RoadWorkOverdue++
				} else if a.Outbound {
					data.OverdueOutbound++
				} else {
					data.OverdueInbound++
				}
			}
		} else {
			log.Printf("analytics: deadline scan: %v", err)
		}
	}
	// FIX14: the dispatch view's other posts' runs, counted like the scan's.
	for _, a := range data.DispatchPast.Rows {
		data.OverdueOther++
		if a.Status == deadline.StatusOverdueLive {
			data.OverdueLive++
		} else {
			data.OverdueUnresolved++
			data.OverdueUncounted++
		}
	}
	s.renderPage(w, "analytics", "analytics.html", data)
}

// fillAnalytics is the pure join of the log-derived figures with the sync
// verdicts — the §8.4 decision lives here and nowhere else, so a test can
// drive it with a hand-built syncStatusView.
func fillAnalytics(d *analyticsData, fig domain.AnalyticsFigures, sync syncStatusView, own string, now time.Time) {
	d.RenderedAt = now.In(time.Local).Format("2006-01-02 15:04:05")
	d.Day = time.UnixMilli(fig.DayStartMS).In(time.Local).Format("Mon 2006-01-02")
	d.TripsDispatched, d.TripsDispatchedToday = fig.TripsDispatched, fig.TripsDispatchedToday
	d.TripsArrived, d.TripsArrivedToday = fig.TripsArrived, fig.TripsArrivedToday
	d.TripsTurnedBack, d.TripsTurnedBackToday = fig.TripsTurnedBack, fig.TripsTurnedBackToday
	d.TripsInTransit = fig.TripsInTransit
	d.RoadWorkDispatched, d.RoadWorkDispatchedToday, d.RoadWorkOut = fig.RoadWorkDispatched, fig.RoadWorkDispatchedToday, fig.RoadWorkOut // James, 2026-10-03
	d.TimesUnresolved = fig.TimesUnresolved

	peer := map[string]peerView{}
	for _, p := range sync.Peers {
		peer[p.SiteID] = p
	}
	// This post and every configured peer always get a row, even with no
	// records held: a peer that has never delivered a single event is the
	// clearest outage there is, and a missing card would hide it.
	sites := append([]domain.SiteFigures(nil), fig.Sites...)
	have := map[string]bool{}
	for _, sf := range sites {
		have[sf.SiteID] = true
	}
	for _, id := range append([]string{own}, peerIDs(sync)...) {
		if !have[id] {
			have[id] = true
			sites = append(sites, domain.SiteFigures{SiteID: id})
		}
	}
	sort.Slice(sites, func(i, j int) bool { return sites[i].SiteID < sites[j].SiteID })
	current := map[string]bool{}
	stale := map[string]bool{}
	for _, sf := range sites {
		v := analyticsSiteView{
			SiteID: sf.SiteID, Own: sf.SiteID == own,
			OnSiteNow: sf.OnSiteNow, VisitsToday: sf.VisitsToday, VisitsSeason: sf.VisitsSeason,
			AvgToday: fmtAvg(sf.AvgOnSiteToday), AvgSeason: fmtAvg(sf.AvgOnSiteSeason),
			TravelOutToday: sf.TravelOutToday, TravelOutSeason: sf.TravelOutSeason,
			TravelInToday: sf.TravelInToday, TravelInSeason: sf.TravelInSeason,
			RoadWorkOutToday: sf.RoadWorkOutToday, RoadWorkOutSeason: sf.RoadWorkOutSeason, // James, 2026-10-03
			Events: sf.EventCount,
		}
		switch pv, isPeer := peer[sf.SiteID]; {
		case v.Own:
			v.Freshness = "this post — records authored here, live"
			// James, 2026-10-04 (R11, D26): still current HERE (never Stale —
			// that would drop this post's own people from the totals), but
			// no longer "live" without a word when peers cannot read it.
			// FIX3 F6 (L04-N1): "NOT receiving" only for peers known not to
			// be — a peer that refuses this post's key on ITS side may still
			// pull this post with its own key, and did (≤ 6 s, measured).
			// ⟦was: KeyRejectedCount + CannotReadCount, all "NOT receiving"⟧
			if n := sync.NotReceivingCount(); n > 0 {
				v.Freshness += fmt.Sprintf(" — but %d peer(s) are NOT receiving them (see the sync status)", n)
			}
			if n := sync.KeyRejectedCount() + sync.CannotReadCount() - sync.NotReceivingCount(); n > 0 {
				v.Freshness += fmt.Sprintf(" — %d peer(s) refuse this post's key; they get these records only on their own routine pulls (see the sync status)", n)
			}
		case !sync.Available:
			v.Stale = true
			v.Freshness = "STALE — this post cannot tell whether it is in contact with this site (no sync status available)"
		case !isPeer:
			v.Stale = true
			v.Freshness = "STALE — this site is not a configured peer of this post, so nothing tells this post its records are current"
		case !pv.Healthy:
			v.Stale = true
			v.Freshness = pv.Verdict
		default:
			v.Freshness = pv.Verdict
		}
		if sf.LastEventOK {
			v.LastEvent = lastEventText(now, sf.LastEventMS, sf.LastEventBackMS)
		}
		if v.Stale {
			stale[sf.SiteID] = true
			d.StaleSites = append(d.StaleSites, sf.SiteID)
		} else {
			current[sf.SiteID] = true
		}
		d.Sites = append(d.Sites, v)
	}
	sort.Strings(d.StaleSites)
	for _, id := range d.StaleSites {
		d.StaleSiteNames = append(d.StaleSiteNames, d.Cfg.SiteName(id))
	}
	d.TotalOnSite = fig.DistinctOnSite(current)
	// Persons present ONLY at stale sites: the ones the total would have
	// counted had the peers been trusted.
	d.ExcludedOnSite = fig.DistinctOnSite(unionSites(current, stale)) - d.TotalOnSite
}

func peerIDs(v syncStatusView) []string {
	var out []string
	for _, p := range v.Peers {
		out = append(out, p.SiteID)
	}
	return out
}

func unionSites(a, b map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}

// fmtAvg renders a mean with the n behind it; no sample reads as words, never
// as "0 min" (a zero that came from having nothing to average is not a zero).
func fmtAvg(m domain.MinutesAvg) string {
	if m.N == 0 {
		return "— (no recorded departures)"
	}
	mins := int(m.RoundedMinutes()) // exact half-up (L18 AVG-1) ⟦was: int(m.Mean + 0.5) over a drifting running mean⟧
	if mins >= 60 {
		return fmt.Sprintf("%dh %02dm (n=%d)", mins/60, mins%60, m.N)
	}
	return fmt.Sprintf("%d min (n=%d)", mins, m.N)
}

// staleSitesText is the warning's site list, e.g. "site-b, site-c".
func (d analyticsData) StaleSitesText() string {
	if len(d.StaleSiteNames) == len(d.StaleSites) {
		return strings.Join(d.StaleSiteNames, ", ")
	}
	labels := make([]string, len(d.StaleSites))
	for i, id := range d.StaleSites {
		labels[i] = config.SiteLabel(id)
	}
	return strings.Join(labels, ", ")
}

// lastEventText is a site card's "Latest event from this site" — the newest
// event this post holds from that site, at the time that post's own clock
// gave it (domain.SiteFigures.LastEventMS). It says what the stamp shows and
// never more: FINDINGS-R4 D16 — a stamp AHEAD of this post's clock used to
// read "0 seconds ago" (humantime.Ago clamps negatives), hiding the skew; FIX3
// F9 (FINDINGS-R5 L03-8a, L09-05) — ⟦was: "… AHEAD of this post's clock; the
// two posts' clocks disagree"⟧ asserted a PRESENT disagreement this post
// cannot know (the stamp was only made while that clock ran fast), and a
// clock set BACK was invisible. backMS > 0 is the store's witness of the
// latter: the newest event is stamped earlier than one recorded before it.
// Hours AND minutes in the skew: Span alone truncates 1h59m to "1 hour".
func lastEventText(now time.Time, ms, backMS int64) string {
	ago := now.Sub(time.UnixMilli(ms))
	at := time.UnixMilli(ms).In(time.Local).Format("2006-01-02 15:04")
	var out string
	if ago <= -time.Second {
		out = fmt.Sprintf("%s (by that post's clock — stamped %s AHEAD of this post's clock now)", at, hoursMinutes(-ago))
	} else {
		out = fmt.Sprintf("%s (%s, by that post's clock)", at, humantime.Ago(ago))
	}
	if backMS > 0 {
		out += fmt.Sprintf(" — stamped %s EARLIER than an event that post recorded before it: that post's clock was set back, so its stamps since then read early", hoursMinutes(time.Duration(backMS)*time.Millisecond))
	}
	return out
}

// hoursMinutes is humantime.Span with the minutes kept past the hour.
func hoursMinutes(d time.Duration) string {
	if h, m := d/time.Hour, (d%time.Hour)/time.Minute; h > 0 && m > 0 {
		return humantime.Span(h*time.Hour) + " " + humantime.Span(m*time.Minute)
	}
	return humantime.Span(d)
}

// fillSignal is the Signal tile's data (FIX15): the peers in config order,
// each judged exactly as the top bar's LOST SIGNAL line judges it.
func fillSignal(d *analyticsData, name func(string) string, serving string, now time.Time) {
	if !d.Sync.Available {
		return
	}
	d.Signal = buildSignalSites(d.Sync, name, serving, now)
	for _, sv := range d.Signal {
		switch {
		case sv.Lost:
			d.SignalLost++
		case sv.OK:
			d.SignalOK++
		}
	}
	d.SignalRule = signalRule(d.Sync, serving)
	if d.Sync.StaleMultiple > 0 && d.Sync.StaleAfter > 0 {
		d.SignalRuleShort = fmt.Sprintf("LOST = no completed sync for more than %d× the sync interval (%s)", d.Sync.StaleMultiple, d.Sync.StaleAfter)
	}
}

// roadWorkTileLabel names the overview's working-on-the-road tile (FIX15,
// James 2026-10-06). A post that offers the category itself keeps its own
// [road_work] display_name. One that does not — the dispatch post, NDB —
// shows OTHER posts' road work, so the label comes from the posts that
// actually send it: the road_work_label configured for each peer that has
// sent road work this season (or, before any has, for every peer that has
// one configured). Exactly one label among them is used; none, or two that
// differ, fall back to this post's default ("Working on the road") rather
// than call one post's runs by another's name. The label is not in the
// replicated record (a trip carries road_work: true and nothing more), so
// config is where the dispatch post learns it — no schema change.
func roadWorkTileLabel(cfg *config.Config, fig domain.AnalyticsFigures) string {
	if cfg.RoadWorkEnabled() {
		return cfg.RoadWorkLabel()
	}
	peerLabel := map[string]string{}
	for _, p := range cfg.Peers {
		peerLabel[p.SiteID] = strings.TrimSpace(p.RoadWorkLabel)
	}
	labels := map[string]bool{}
	for _, sf := range fig.Sites {
		if sf.RoadWorkOutSeason > 0 && sf.SiteID != cfg.SiteID {
			labels[peerLabel[sf.SiteID]] = true
		}
	}
	if len(labels) == 0 {
		for _, p := range cfg.Peers {
			if l := peerLabel[p.SiteID]; l != "" {
				labels[l] = true
			}
		}
	}
	if len(labels) == 1 {
		for l := range labels {
			if l != "" {
				return l
			}
		}
	}
	return cfg.RoadWorkLabel()
}
