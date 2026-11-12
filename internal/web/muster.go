// muster.go — C9: the muster view and print (design §11). Two routes:
//
//   - GET /muster        the LIVE board for THIS site: open visits grouped
//     Company→Trade, resolved through the alias read path (§8.8-ii), the
//     numeric MusterMinGroupSize fold applied. This is the on-glass
//     operational view — it can change from one load to the next as check-
//     ins/outs happen and as aliases are added.
//   - GET /muster/print   a PRINT-STYLED render, grouped by SNAPSHOT ONLY
//     (design §8.8-iv: "historical reprints group by snapshot only"). Two
//     forms:
//   - ?visits=<ids>  restricted to EXACTLY that visit set. /muster's own
//     "Print" link builds this URL from the visit ids on the page at
//     render time — that querystring IS "the sheet": no client storage,
//     nothing persisted server-side, because a visit's snapshot fields
//     never change after arrival (§8.8-i), so the same URL reproduces
//     the same sheet forever, alias or no alias. Visiting /muster/print
//     again later with the same ids (a bookmark, or browser back) is
//     exactly what "reprint" means here.
//   - no ?visits= at all: a DIRECT, fresh print of THIS site's currently
//     open visits, snapshotted right now — still through the same
//     snapshot-only grouping (every printed sheet groups by snapshot,
//     first print or reprint alike). This is what lets /trips and /
//     reach a working print in a single click each (verification
//     finding: routing print through the live /muster page first cost
//     3 interactions, over the §6 budget of ≤2 "from any screen").
//
// Both routes are read-only: no event is authored by loading or printing a
// muster.
package web

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
)

// registerMusterRoutes mounts the C9 muster surface. Called from Handler().
func (s *Server) registerMusterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /muster", s.handleMusterLive)
	mux.HandleFunc("GET /muster/print", s.handleMusterPrint)
}

type musterData struct {
	SiteID    string
	Mode      string // "live" | "reprint" — the template labels the page accordingly
	Board     domain.MusterBoard
	PrintHref string // live only: freezes the current open-visit set into a reprintable URL

	// RenderedAt (U3): when THIS sheet was produced, in the box's local
	// zone. A printed muster is read aloud during an evacuation and there
	// may be more than one sheet on the table; without a time on the paper
	// nobody can say which one is current. It is the render time and is
	// labelled as such on both routes ("as at" on the live board, "sheet
	// produced" on a reprint) — deliberately NOT presented as a data
	// as-of time, because a reprint's ROSTER is frozen (§8.8-i) while its
	// exception sections are live. Nothing here is derived from it; it is
	// display only.
	RenderedAt string

	// RestoredVisits / RestoredCount (SchemaVersion 15, R-23 — James,
	// 2026-09-13: "Restore them, but mark them as restored"): the visit ids
	// on this board that THIS post never observed — rows under our own
	// site tag authored by an EARLIER incarnation of this post (node_epoch
	// below the running config epoch), which can only have reached this
	// database through the §8.4 self-history path, i.e. the peer's copy of
	// our history (domain.Visit.RestoredFromPeer states the rule once).
	// The board still COUNTS them — they are on site as far as the record
	// knows, and a post wiped mid-shift with twenty people inside must get
	// all twenty back without re-entry — but the sheet tags each one and
	// says how many, so second-hand presence is never read as first-hand:
	// the muster never claims more than it knows. Both routes: a reprint's
	// frozen rows carry the same key, so the marking reproduces on paper.
	// Keyed by visit id because MusterMember carries the id and the
	// classification belongs to the ROW, not to the grouping. MEASURED
	// hazard this answers: CLEAN-NEGATIVES-RECHECK.md plant C2b — a peer's
	// forged self-history put a stranger's OPEN VISIT under our tag; it
	// still lands (the peer's copy is our only backup) and now lands MARKED.
	RestoredVisits map[string]bool
	RestoredCount  int

	// Elsewhere / ElsewhereCount (M-6 / T-L08-C — James, 2026-10-03, ruling:
	// a read-time "also recorded on site at <peer> since HH:MM" flag on the
	// muster and roster at BOTH posts; no cross-site write; both stay
	// counted). Keyed by visit id like RestoredVisits, for the same reason:
	// the classification belongs to the ROW. The domain states the rule once
	// (visit_elsewhere.go); this is its display shape — peer label and the
	// peer visit's sign-in time in this box's zone. Live AT RENDER TIME on
	// every route, a reprint included: the frozen set says who was on the
	// sheet, and this says what the record holds about them NOW — which is
	// exactly what the person holding the paper needs to know. The count is
	// over names the sheet SHOWS that carry at least one tag.
	Elsewhere      map[string][]elsewhereTag
	ElsewhereCount int

	// CheckedOutSince / CheckedOutCount (M-5 / M-19 — James, 2026-10-03,
	// ruling: "keep the route; never drop silently — print 'N on the
	// original sheet have checked out since: <names, time>'"). REPRINT only:
	// the frozen link's visits whose record has since CLOSED. buildMuster
	// lists open visits, so these people are no longer on the roster block —
	// they used to vanish from it in silence (MEASURED, L10 M-19 twice).
	// Now they are named here, with how and when the record closed
	// (domain.ClosedVisits), and the count says how many. PRINTED: it is
	// about whether the names on the paper are the names on the original
	// sheet, which is the one thing a reprint exists to answer.
	CheckedOutSince []checkedOutRow
	CheckedOutCount int

	// DuplicateIDs (M-22): how many visit ids the reprint link named MORE
	// THAN ONCE. The app never mints such a link; a hand-edited one used to
	// list — and count — one person twice. Each id is now read once and the
	// sheet says the link was malformed rather than silently repairing it.
	DuplicateIDs int

	// Incomplete (F1): row-31 crash-recovery trips at this site — a
	// GateCheckIn that authored its dispatch but never its arrival. Until
	// now this only rendered on /gate; an officer looking at the muster
	// during an evacuation saw no indication at all that it might be short
	// people. Populated on BOTH live and print (both print forms — the
	// direct snapshot and a frozen ?visits= reprint) via
	// Server.incompleteCheckInRows (web.go), current AT RENDER TIME: a
	// reprint's frozen VISIT set never changes (§8.8-i), but this warning is
	// deliberately NOT frozen with it, because an incomplete check-in has no
	// visit row yet (it hasn't arrived) and so can never be part of any
	// frozen set — it can only ever be reported live, which is exactly the
	// point: whoever is holding a sheet right now learns whether the site
	// currently knows of anyone still mid-check-in. Rendered so it is NOT
	// hidden by @media print (muster.html) — a printed roster that is short
	// must say so on the paper itself.
	Incomplete []incompleteRow

	// Away (2026-08-20): who the record says is out at each uncontrolled
	// destination — the evacuation question has two halves, ON SITE and OUT
	// AT A PAD, and a printed roster that omits the pad crew answers only
	// one. Same posture as Incomplete above, for the same reasons: current
	// AT RENDER TIME on BOTH live and print, deliberately NOT frozen with a
	// reprint's visit set — an away person has no visit row (that is what
	// away MEANS), so they can never be part of any frozen set; the sheet in
	// hand reports what the site knows right now. Every CONFIGURED
	// uncontrolled destination gets a row even when empty — the muster must
	// say what it knows, including that it knows of nobody (the F1 lesson).
	// Rendered NOT hidden by @media print: the paper itself must carry it.
	Away []awayPadRow

	// NotTracked (operator ruling, 2026-08-20): the away-view's third
	// category — people who left ON a vehicle and did NOT arrive with it,
	// whereabouts NOT recorded (domain.NotTracked). Shown as exactly that: a
	// gap in knowledge, not a status. Deliberately NOT a defect banner —
	// dropping someone at a turnoff is routine, and if this read as an error
	// officers would avoid the workflow that produces the record. Equally
	// deliberately not reassuring: nobody is watching and no timer will
	// fire. Same render posture as Away above (live on both routes, prints).
	// ⟦was: []domain.NotTrackedRow — the row printed the raw destination id,
	// no time and the as-dispatched truck (FINDINGS-R5 F3a, F10)⟧
	NotTracked []notTrackedRow

	// InTransit (D-6, 2026-08-21): the fourth population — people this post
	// put on the road and has not heard back about. THE GAP: before this
	// field a crew dispatched from here and not yet arrived at the peer was
	// on NEITHER post's muster, on screen or on paper, while the system held
	// their vehicle, driver, manifest and destination. Measured on a live
	// two-site instance: 6 checked in at site-a, 3 dispatched to site-b;
	// site-a read 3 on site / 0 away / 0 not tracked, site-b read 0 / 0 / 0.
	//
	// OWNED BY THE ORIGIN, and the reasoning is in domain/in_transit.go's
	// file comment because it is a domain decision, not a rendering one:
	// the origin authored the dispatch so its own stream always holds it
	// (a destination-owned section is empty exactly when the link is down),
	// and the dispatch fold closes the origin visit in the SAME transaction,
	// so a person moves from "on site" to "on the road" with no instant in
	// between at which they are nowhere.
	//
	// Same render posture as Away/NotTracked above: live AT RENDER TIME on
	// both routes and deliberately NOT frozen into a reprint's visit set —
	// an in-transit person has no visit row anywhere (that is what in
	// transit MEANS), so they can never be part of any frozen set. NOT in
	// the on-site total: they are not here. PRINTED, names and all: under
	// D-6 they are unambiguously this operation's responsibility — it put
	// them on the road — which is the exact test the away ruling applies in
	// the other direction.
	InTransit []inTransitRow

	// RoadWork (James, 2026-10-03): the fifth population — the crews THIS
	// post sent out WORKING ON THE ROAD (domain.RoadWorkFrom) and has not
	// recorded back. Origin-owned for in_transit.go's reasons and one more:
	// there is no destination post to own them instead. Same render posture
	// as InTransit — live at render time on both routes, never frozen into a
	// reprint's visit set, NOT in the on-site total, PRINTED with names (this
	// operation put them on the road). Its own section rather than rows
	// inside "On the road": these people are not going anywhere and will not
	// arrive anywhere; a reader scanning the on-the-road block for who is
	// expected at the peer must not count them. RoadWorkLabel names the
	// category in this post's own words (config [road_work].display_name).
	RoadWork      []inTransitRow
	RoadWorkLabel string
	// RoadWorkElsewhere (D27, James 2026-10-04 — M-6's ruling applied to
	// the road-work list): person id on a road-work row → the open visits
	// the same human holds at another post, rendered "ALSO ON SITE AT <post>"
	// beside the name. Both listings stay; the flag ends the silence.
	RoadWorkElsewhere map[string][]elsewhereTag
	// RoadWorkCount / RoadWorkVehicleCount: PEOPLE out working, and DISTINCT
	// RESOLVED VEHICLE IDENTITIES — the same two numbers, derived the same
	// way, as InTransitCount / InTransitVehicleCount (W7-34(b)).
	RoadWorkCount        int
	RoadWorkVehicleCount int

	// AwayCount / NotTrackedCount (U3): how many PEOPLE each of the two
	// off-site categories holds, summed across pads and across trips
	// respectively. Nothing new is known here — these are sums of the rows
	// already rendered below them — but the headcount block has to state
	// them next to the on-site total, and a Go template cannot sum a nested
	// range. len(Away) is a count of PADS and len(NotTracked) a count of
	// TRIPS; neither is a count of people, and printing either as if it
	// were is exactly the kind of quiet wrong number this document cannot
	// carry.
	AwayCount       int
	NotTrackedCount int
	// NotTrackedTripCount: DISTINCT trips among the NotTracked rows. A run
	// can carry two rows ("taken off" and "not aboard at arrival"), so
	// len(NotTracked) over-counts trips (R6 L03-03: "6 on 4 trips" for 3).
	NotTrackedTripCount int
	// InTransitCount: PEOPLE on the road, summed across runs. len(InTransit)
	// is a count of ROWS and is not this number — see the note above.
	InTransitCount int
	// ⟦W7-34(b)⟧ InTransitVehicleCount: DISTINCT RESOLVED VEHICLE IDENTITIES
	// on the road.
	//
	// ⚠⚠ IT IS NOT len(InTransit), AND THAT WAS THE DEFECT. The headline used
	// to read `{{len .InTransit}} vehicle`, which counts ROWS and calls them
	// vehicles — so one truck with two open runs was reported as TWO TRUCKS
	// ON THE ICE, on the sheet an officer carries when the board cannot be
	// reached. Two runs of one truck now print as TWO BLOCKS and ONE vehicle,
	// and that asymmetry is the whole observable: merging the blocks would
	// give one and one, and merging blocks is the one operation that can drop
	// a person.
	InTransitVehicleCount int

	// C7 (§9.4): the persistent banner rides every screen, this one included
	// (mounted at C9's hook marker in the template; excluded from print by
	// the partial itself).
	Banner    []deadline.Alert
	BannerErr string

	// Sync (SYNC-HONESTY, 2026-09-09): the REAL replication state, from the
	// same snapshot / and /roster render (Server.buildSyncSection) — never a
	// signal invented for this page. Until this field existed the sync chip
	// rendered on exactly two pages, / and /roster, and an officer reading a
	// muster learned NOTHING about whether the data behind it was current:
	// records made at the other post reach this sheet only through sync,
	// and this is the document that may reach an insurer after an incident.
	// The shared top bar draws its chip from it on screen; because the bar
	// is hidden on paper, muster.html ALSO prints a per-peer verdict block
	// from it (the same verdict, the same summary — one vocabulary). A post
	// with no engine wired reports Available=false, rendered as "cannot
	// tell", never as "healthy". Populated on BOTH routes, live at render
	// time like Incomplete/Away/InTransit: freshness is a property of the
	// moment the sheet was produced, never of a frozen visit set. The BLOCK
	// itself adds no number and regroups nothing; the sheet's own numbers
	// DO depend on peer rows, and the printed footnote says which — ON SITE
	// and AWAY from this post's own records, ON THE ROAD and NOT TRACKED
	// peer-dependent (review fixture, 2026-09-09: one undelivered peer
	// arrival held on-the-road at 2 instead of 0 and not-tracked at 0
	// instead of 1, chip healthy throughout). Never restate this as "the
	// sheet is unaffected".
	Sync syncStatusView
}

// awayPerson / awayPadRow: one uncontrolled destination's "who is out
// there", grouped per pad because that is the shape of the question.
type awayPerson struct {
	Name    string
	Vehicle string // the run they went out on, verbatim (M8)
	// ⟦was: Override / OverrideBy — the run's phase-override reason and its
	// recording post; weather phases removed, James 2026-10-04⟧
}

type awayPadRow struct {
	Pad     string // display name (config) or the recorded id
	Persons []awayPerson
}

// awayPadRows groups this post's uncontrolled runs per destination:
// configured uncontrolled destinations first (in config order, present even
// when empty), then any pad ids appearing in THIS SITE's runs that it does
// not configure. The predicate lives in the domain — one answer, however
// many surfaces render it.
//
// domain.OutAtUncontrolledFrom, not OutAtUncontrolled (2026-08-21, James):
// "on the pad muster roster it should not display the persons going to the
// other site — the only one that would have any concern about them is the
// ice road site control." Trips replicate; the unscoped predicate put Ice
// Road's pad crew on the Pad's muster, where the sheet's reader has no
// relationship to them at all. The reasoning and the deliberate limits
// (why /trips and the picker were NOT narrowed, why NotTracked was not)
// are on OutAtUncontrolledFrom itself.
//
// The empty-pad rows survive this, and must: a post that configures an
// uncontrolled destination still says "nobody recorded away at this
// destination" for it (the F1 lesson). Scoping removes other posts'
// PEOPLE, never this post's categories.
func (s *Server) awayPadRows() ([]awayPadRow, error) {
	outs, err := domain.OutAtUncontrolledFrom(s.Store, s.Cfg.SiteID)
	if err != nil {
		return nil, err
	}
	byPad := map[string][]awayPerson{}
	var extras []string
	known := map[string]string{} // pad id -> display name
	for _, d := range s.Cfg.DestinationList() {
		if !d.Controlled {
			known[d.ID] = d.DisplayName
		}
	}
	for _, o := range outs {
		if _, ok := byPad[o.Pad]; !ok && known[o.Pad] == "" {
			extras = append(extras, o.Pad)
		}
		for _, p := range o.Persons {
			byPad[o.Pad] = append(byPad[o.Pad], awayPerson{Name: p.Name, Vehicle: o.Vehicle})
		}
	}
	var rows []awayPadRow
	for _, d := range s.Cfg.DestinationList() {
		if !d.Controlled {
			rows = append(rows, awayPadRow{Pad: d.DisplayName, Persons: byPad[d.ID]})
		}
	}
	sort.Strings(extras)
	for _, pad := range extras {
		rows = append(rows, awayPadRow{Pad: s.padName(pad), Persons: byPad[pad]})
	}
	return rows, nil
}

// inTransitRow is one domain.InTransitRow with its destination resolved for
// a human to read aloud. The domain row carries the peer SITE ID, which is
// the load-bearing identity (D-5: peers, envelopes and every stored row key
// are keyed off it) and is exactly the wrong thing to shout across a room
// during an evacuation. The configured DISPLAY NAME is what an officer says
// out loud — "Pad Site Control", not "site-b" — so the sheet carries that,
// and falls back to the recorded id for a peer this box does not configure
// rather than rendering an empty cell.
type inTransitRow struct {
	domain.InTransitRow
	Destination string // display name where configured, the site id otherwise
}

// inTransitRows is the muster's share of domain.InTransitFrom: this post's
// open outbound runs, destination display-resolved. Origin-scoped — see
// musterData.InTransit for why the ORIGIN owns them and not the peer that
// is expecting them.
func (s *Server) inTransitRows() ([]inTransitRow, error) {
	rows, err := domain.InTransitFrom(s.Store, s.Cfg.SiteID)
	if err != nil {
		return nil, err
	}
	display := map[string]string{}
	for _, d := range s.Cfg.DestinationList() {
		if d.Controlled {
			display[d.ID] = d.DisplayName
		}
	}
	out := make([]inTransitRow, 0, len(rows))
	for _, r := range rows {
		name := s.Cfg.SiteName(r.Destination)
		if dn, ok := display[r.Destination]; ok && dn != "" {
			name = dn
		}
		out = append(out, inTransitRow{InTransitRow: r, Destination: name})
	}
	return out, nil
}

// notTrackedRow is one domain.NotTrackedRow as the sheet prints it (fix
// round 3, FINDINGS-R5 F3a and F10): the destination by display name — no
// screen or sheet shows a raw site id (config.go:425) — the run's LIVE
// vehicle with the as-dispatched number beside it when the two differ, and
// the time the people became not tracked.
type notTrackedRow struct {
	domain.NotTrackedRow
	Destination  string // display name; a road-work run's fixed text as is
	Vehicle      string // live (domain.NotTrackedDetail.Vehicle)
	DispatchedAs string // verbatim as-dispatched number when it differs
	Since        string // elsewhereSince: time by <post>'s clock, "" when this post cannot resolve it
}

func (s *Server) notTrackedRows() ([]notTrackedRow, error) {
	rows, err := domain.NotTracked(s.Store)
	if err != nil {
		return nil, err
	}
	details, err := domain.NotTrackedDetails(s.Store, rows)
	if err != nil {
		return nil, err
	}
	out := make([]notTrackedRow, len(rows))
	for i, r := range rows {
		dest := r.Destination
		if dest != domain.RoadWorkDestinationText {
			dest = s.siteDisplay(dest)
		}
		row := notTrackedRow{NotTrackedRow: r, Destination: dest,
			Vehicle: details[i].Vehicle, DispatchedAs: details[i].DispatchedAs}
		if details[i].AtMS != 0 {
			// Whose clock (B's F9 rule, elsewhereSince): usually the
			// destination's arrival, not this post's. ⟦was: the time alone⟧
			row.Since = elsewhereSince(details[i].AtMS, s.siteDisplay(details[i].AtSite))
		}
		out[i] = row
	}
	return out, nil
}

// musterSheetStamp is the "when was this paper produced" string both routes
// put on the sheet. One place, one format, so a live board and the reprint
// it mints cannot disagree about how a time is written. The zone
// abbreviation is included deliberately: a bare 14:32 on a sheet carried
// between sites is ambiguous.
func musterSheetStamp(t time.Time) string { return t.Format("2006-01-02 15:04 MST") }

// countOffSite fills the two derived headcounts (see musterData.AwayCount).
// Called after Away and NotTracked are populated, on BOTH routes.
func (d *musterData) countOffSite() {
	d.AwayCount = 0
	for _, pad := range d.Away {
		d.AwayCount += len(pad.Persons)
	}
	d.NotTrackedCount = 0
	trips := map[string]bool{}
	for _, trip := range d.NotTracked {
		d.NotTrackedCount += len(trip.Persons)
		trips[trip.TripID] = true
	}
	d.NotTrackedTripCount = len(trips)
	d.InTransitCount = 0
	// ⟦W7-34(b)/(e)⟧ Counted here, on the ONE path both routes take, so the
	// board and the printed sheet cannot disagree about how many trucks are
	// out. The set is over the RESOLVED identity, so two runs of one truck
	// count once and two runs of two trucks count twice.
	identities := map[string]bool{}
	for _, run := range d.InTransit {
		d.InTransitCount += len(run.Persons)
		identities[run.VehicleIdentity] = true
	}
	d.InTransitVehicleCount = len(identities)
	// James, 2026-10-03: the road-work pair, by the same rule.
	d.RoadWorkCount = 0
	rwIdentities := map[string]bool{}
	for _, run := range d.RoadWork {
		d.RoadWorkCount += len(run.Persons)
		rwIdentities[run.VehicleIdentity] = true
	}
	d.RoadWorkVehicleCount = len(rwIdentities)
}

// roadWorkMusterRows is the muster's share of domain.RoadWorkFrom (James,
// 2026-10-03): this post's open road-work runs. Destination stays "" — there
// is none — and the template names the category instead.
func (s *Server) roadWorkMusterRows() ([]inTransitRow, error) {
	rows, err := domain.RoadWorkFrom(s.Store, s.Cfg.SiteID)
	if err != nil {
		return nil, err
	}
	out := make([]inTransitRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, inTransitRow{InTransitRow: r})
	}
	return out, nil
}

func (s *Server) musterFail(w http.ResponseWriter, what string, err error) {
	log.Printf("muster: %s: %v", what, err)
	http.Error(w, dbFailBody(err), http.StatusInternalServerError)
}

// openVisitsAtThisSite is "the muster is the sheet someone carries outside"
// of THIS site (design §11) — scoped to local open visits only, unlike the
// multi-site operational view on /trips. Shared by the live board and the
// direct (no ?visits=) print path, which snapshots the same query.
func (s *Server) openVisitsAtThisSite() ([]domain.Visit, error) {
	// Indexed open-set read (F30): cost follows current on-site headcount,
	// not the season's cumulative visit count.
	return domain.OpenVisitsAt(s.Store, s.Cfg.SiteID)
}

// restoredVisitSet applies domain.Visit.RestoredFromPeer to the rows a board
// was built from, for THIS post's identity (musterData.RestoredVisits), and
// counts how many of the board's listed visits it marks — the count is over
// what the sheet SHOWS, never over rows the board did not list.
func (s *Server) restoredVisitSet(visits []domain.Visit, board domain.MusterBoard) (map[string]bool, int) {
	set := make(map[string]bool, len(visits))
	for _, v := range visits {
		if v.RestoredFromPeer(s.Cfg.SiteID, s.Cfg.NodeEpoch) {
			set[v.VisitID] = true
		}
	}
	n := 0
	for _, id := range board.VisitIDs() {
		if set[id] {
			n++
		}
	}
	return set, n
}

// elsewhereTag is one "also recorded on site at <peer> since HH:MM" flag
// (musterData.Elsewhere). SiteID is the test hook; Site is what an officer
// reads; Since is the peer visit's recorded sign-in time in the roster's own
// `hh:nn mm/dd` form, or "" when this store cannot resolve it — rendered as
// an absence, never a guess.
type elsewhereTag struct {
	SiteID string
	Site   string
	Since  string
}

// elsewhereSince is a tag's "since" — the peer's recorded sign-in time,
// with the clock it was read from (FIX3 F9; r4 L16-SKb, R5 L03-8): the stamp
// is THAT post's, so a post whose clock ran 30 min fast showed "since 10:04"
// in this post's future with no word whose time it was. ⟦was: the time
// alone⟧
func elsewhereSince(ms int64, site string) string {
	return time.UnixMilli(ms).In(time.Local).Format(signInLayout) + " by " + site + "'s clock"
}

// siteDisplay is the configured display name for a site id this post knows
// as a destination, else its label (config.SiteLabel) — the same fallback
// inTransitRows uses: a peer this box does not configure still gets a
// readable name, never an empty cell.
func (s *Server) siteDisplay(id string) string {
	return s.Cfg.SiteName(id)
}

// padName is padLabel at this post: an uncontrolled pad by its configured
// name, else its id made readable — never the raw id.
func (s *Server) padName(id string) string {
	return padLabel(s.Cfg.SiteName, id)
}

// elsewhereTags is domain.AlsoOnSiteElsewhere in display shape, for THIS
// post's open visits (musterData.Elsewhere). Deterministic across posts:
// the domain's order is (site, visit id), and the labels are per-post
// config, which is the one thing that legitimately differs between them.
func (s *Server) elsewhereTags(siteID string, open []domain.Visit) (map[string][]elsewhereTag, error) {
	twins, err := domain.AlsoOnSiteElsewhere(s.Store, siteID, open)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]elsewhereTag, len(twins))
	for id, list := range twins {
		for _, e := range list {
			tag := elsewhereTag{SiteID: e.SiteID, Site: s.siteDisplay(e.SiteID)}
			if e.SinceOK {
				tag.Since = elsewhereSince(e.SinceMS, tag.Site)
			}
			out[id] = append(out[id], tag)
		}
	}
	return out, nil
}

// roadWorkElsewhereTags is domain.RoadWorkCrewOnSiteElsewhere in display
// shape (musterData.RoadWorkElsewhere) — the same tag, words and hook as the
// on-site roster's M-6 flag.
func (s *Server) roadWorkElsewhereTags(rows []inTransitRow) (map[string][]elsewhereTag, error) {
	plain := make([]domain.InTransitRow, 0, len(rows))
	for _, r := range rows {
		plain = append(plain, r.InTransitRow)
	}
	twins, err := domain.RoadWorkCrewOnSiteElsewhere(s.Store, s.Cfg.SiteID, plain)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]elsewhereTag, len(twins))
	for id, list := range twins {
		for _, e := range list {
			tag := elsewhereTag{SiteID: e.SiteID, Site: s.siteDisplay(e.SiteID)}
			if e.SinceOK {
				tag.Since = elsewhereSince(e.SinceMS, tag.Site)
			}
			out[id] = append(out[id], tag)
		}
	}
	return out, nil
}

// countElsewhere counts the board's listed visits that carry a tag — over
// what the sheet SHOWS, as restoredVisitSet counts.
func countElsewhere(board domain.MusterBoard, tags map[string][]elsewhereTag) int {
	n := 0
	for _, id := range board.VisitIDs() {
		if len(tags[id]) > 0 {
			n++
		}
	}
	return n
}

// checkedOutRow is one person on a reprint's original sheet whose record
// has closed since (musterData.CheckedOutSince).
type checkedOutRow struct {
	VisitID string
	Name    string
	// How is the closing fact as a sentence fragment after the name —
	// "checked out 14:02 10/03", "left on TRK-7 to Pad Site Control 14:10
	// 10/03" — built by checkedOutRows from domain.VisitClose. A time this
	// store cannot resolve is said to be missing, never invented.
	How string
}

// checkedOutRows renders domain.ClosedVisits for the closed rows of a
// reprint's frozen set. The NAME follows the reprint's own three arms
// (domain.MusterReprint: snapshot, then live roster name, then the raw id)
// so a departed person is named exactly as the original sheet named them.
func (s *Server) checkedOutRows(frozen []domain.Visit, personName map[string]string) ([]checkedOutRow, error) {
	closes, err := domain.ClosedVisits(s.Store, frozen)
	if err != nil {
		return nil, err
	}
	out := make([]checkedOutRow, 0, len(closes))
	for _, c := range closes {
		v := c.Visit
		name := ""
		if v.PersonSnapshotName != nil {
			name = *v.PersonSnapshotName
		}
		if name == "" {
			name = personName[v.PersonID]
		}
		if name == "" {
			name = v.PersonID
		}
		when := "— time not on record at this post"
		if c.InstantOK {
			when = time.UnixMilli(c.InstantMS).In(time.Local).Format(signInLayout)
		}
		var how string
		switch c.How {
		case domain.ClosedByCheckout:
			how = "checked out " + when
		case domain.ClosedByDispatch:
			vehicle := c.Vehicle
			if vehicle == "" {
				vehicle = "a vehicle with no number recorded"
			}
			switch {
			case c.RoadWork:
				how = "left on " + vehicle + " — " + s.Cfg.RoadWorkLabel() + " — " + when
			case c.Destination != "":
				how = "left on " + vehicle + " to " + s.siteDisplay(c.Destination) + " " + when
			case c.Uncontrolled != "":
				how = "left on " + vehicle + " to " + s.padName(c.Uncontrolled) + " (uncontrolled) " + when
			default:
				how = "left on " + vehicle + " " + when
			}
		case domain.ClosedByReCheckin:
			how = "signed in again at this gate " + when + " — no departure was recorded; the line on the original sheet was closed by the new check-in"
		case domain.ClosedByNextArrival:
			how = "arrived here again " + when + " — no departure was recorded; the line on the original sheet was closed by the new arrival"
		default:
			how = "record closed — how and when is not on record at this post"
		}
		out = append(out, checkedOutRow{VisitID: v.VisitID, Name: name, How: how})
	}
	// Name order, visit id as the total tiebreak — the roster's own rule.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].VisitID < out[j].VisitID
	})
	return out, nil
}

func (s *Server) handleMusterLive(w http.ResponseWriter, r *http.Request) {
	open, err := s.openVisitsAtThisSite()
	if err != nil {
		s.musterFail(w, "visits", err)
		return
	}
	persons, err := domain.Persons(s.Store)
	if err != nil {
		s.musterFail(w, "persons", err)
		return
	}
	companies, err := domain.Companies(s.Store)
	if err != nil {
		s.musterFail(w, "companies", err)
		return
	}
	trades, err := domain.Trades(s.Store)
	if err != nil {
		s.musterFail(w, "trades", err)
		return
	}
	// ⟦W5d, DR-4⟧ TWO resolvers, and they must be able to disagree. The
	// COMPANY grouping resolves through the composed company_amended.same_as
	// snapshot; ⟦W8⟧ the TRADE grouping now resolves through its OWN composed
	// trade_amended.same_as snapshot — the shared legacy map it used to ride
	// is deleted (P-3 §5's schedule, discharged here). Both are loaded PER
	// REQUEST — a cached or package-level snapshot is the second mechanism
	// P-3 exists to prevent.
	companyMerge, err := domain.LoadCompanyMergeMap(s.Store)
	if err != nil {
		s.musterFail(w, "company merge map", err)
		return
	}
	tradeMerge, err := domain.LoadTradeMergeMap(s.Store)
	if err != nil {
		s.musterFail(w, "trade merge map", err)
		return
	}

	board := domain.MusterLive(open, persons, companies, trades, companyMerge, tradeMerge, s.Cfg.MusterMinGroupSize)

	// Freeze the exact visit set this render covers into the print link's
	// querystring — this is the whole reprint mechanism (file comment).
	ids := board.VisitIDs()
	sort.Strings(ids) // stable querystring regardless of internal bucket-iteration order
	data := musterData{SiteID: s.Cfg.SiteID, Mode: "live", Board: board,
		PrintHref:  "/muster/print?visits=" + strings.Join(ids, ","),
		RenderedAt: musterSheetStamp(time.Now())}
	data.RestoredVisits, data.RestoredCount = s.restoredVisitSet(open, board) // R-23
	// M-6 / T-L08-C (James, 2026-10-03): who here is ALSO recorded on site
	// at a peer. Read-time, from the replicated rows; nothing is written.
	if data.Elsewhere, err = s.elsewhereTags(s.Cfg.SiteID, open); err != nil {
		s.musterFail(w, "also on site elsewhere", err)
		return
	}
	data.ElsewhereCount = countElsewhere(board, data.Elsewhere)
	data.Banner, data.BannerErr = s.banner() // C7: the banner rides every screen (§9.4)
	data.Sync = s.buildSyncSection()         // SYNC-HONESTY: the freshness signal, on screen and on paper

	// F1: surface row-31 crash-recovery trips here too, not only on /gate.
	incomplete, err := s.incompleteCheckInRows(personNameIndex(persons))
	if err != nil {
		s.musterFail(w, "incomplete check-ins", err)
		return
	}
	data.Incomplete = incomplete

	// 2026-08-20: the away-at-pad half of the evacuation question, live at
	// render time (see musterData.Away).
	if data.Away, err = s.awayPadRows(); err != nil {
		s.musterFail(w, "away at uncontrolled", err)
		return
	}
	if data.NotTracked, err = s.notTrackedRows(); err != nil {
		s.musterFail(w, "not tracked", err)
		return
	}
	// D-6, 2026-08-21: the people this post put on the road. Origin-scoped
	// (see musterData.InTransit) and, like the two sections above, live at
	// render time on both routes rather than frozen into a visit set — an
	// in-transit person holds no visit row anywhere to freeze.
	if data.InTransit, err = s.inTransitRows(); err != nil {
		s.musterFail(w, "in transit", err)
		return
	}
	// James, 2026-10-03: the crews this post sent out working on the road.
	data.RoadWorkLabel = s.Cfg.RoadWorkLabel()
	if data.RoadWork, err = s.roadWorkMusterRows(); err != nil {
		s.musterFail(w, "road work", err)
		return
	}
	if data.RoadWorkElsewhere, err = s.roadWorkElsewhereTags(data.RoadWork); err != nil {
		s.musterFail(w, "road work also on site elsewhere", err)
		return
	}
	data.countOffSite() // headcounts for the tally block; sums only, nothing new

	s.renderPage(w, "muster", "muster.html", data)
}

// personNameIndex builds the same id->name lookup shape gate.go's
// handleGatePage already builds inline, so incompleteCheckInRows (web.go)
// can resolve a driver's display name without another Persons() dump.
func personNameIndex(persons []domain.Person) map[string]string {
	m := make(map[string]string, len(persons))
	for _, p := range persons {
		m[p.PersonID] = p.Name
	}
	return m
}

// handleMusterPrint serves TWO of the three muster modes, and which one it
// is turns entirely on whether ?visits= is present (V-02).
//
// THE DEFECT THIS SHAPE FIXES. Both branches used to fall through to one
// `Mode: "reprint"`, so the no-?visits route — the fresh-snapshot path,
// reached by the live board's own Print action — printed a sheet stamped
// REPRINT carrying the reprint's copy:
//
//	"This reproduces the same roster no matter when it is printed again."
//
// On that route the sentence is FALSE. With no ?visits= the handler calls
// openVisitsAtThisSite() fresh on every GET, so a second GET five minutes
// later produces a different roster. And it is false ON PAPER, not merely
// on screen: the print block hides only .topbar/.banner-notice/.actions/
// nav.screen-only/.pagenav and merely de-chromes .mode-live/.mode-reprint,
// while .mode-detail is explicitly re-coloured INSIDE the print block — so
// that sentence prints, on the one document read aloud during an
// evacuation, claiming a completeness guarantee the sheet cannot honour.
//
// Three modes now, each saying only what its own route can support:
//
//	live      /muster                  — changes as people move; print to freeze
//	snapshot  /muster/print            — frozen at the moment it was produced,
//	                                     and it says WHICH moment. Mints its
//	                                     own ?visits= link so the exact sheet
//	                                     that came out can be reproduced.
//	reprint   /muster/print?visits=…   — the named set, reproducible forever.
//	                                     This is the only route on which the
//	                                     point-in-time sentence is true.
func (s *Server) handleMusterPrint(w http.ResponseWriter, r *http.Request) {
	var frozen []domain.Visit
	// mode is "reprint" only when the caller named the visit set. A fresh
	// snapshot is its OWN mode; it must never borrow reprint's label or
	// reprint's promise.
	mode := "snapshot"
	duplicateIDs := 0
	if v := r.URL.Query().Get("visits"); v != "" {
		mode = "reprint"
		// M-22 (James, 2026-10-03): a hand-edited link can name one id twice.
		// The app never mints such a link; read each id ONCE, in first-seen
		// order, and say on the sheet how many were repeated — never list and
		// count one person twice, never silently repair the link either.
		var ids []string
		seen := map[string]bool{}
		for _, id := range strings.Split(v, ",") {
			// M-21 (fix2-fj, 2026-10-04): an EMPTY id — a trailing or doubled
			// comma — is refused in its own words. It used to fall through to
			// VisitsByID's "1 visit id(s) not found: " with nothing after the
			// colon (the id it could not find was ""), which officerText
			// could not rewrite. Still refused, never a partial sheet.
			if strings.TrimSpace(id) == "" {
				log.Printf("muster print: empty visit id in %q", v)
				http.Error(w, "This print link has an empty visit id in it (a stray comma), so it does not say who belongs on the sheet. "+
					"The sheet was not printed, so nobody is silently missing from it — open the muster and print it again from there.", http.StatusGone)
				return
			}
			if seen[id] {
				duplicateIDs++
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}
		// Point lookups per frozen id (F30) — a reprint names its own rows,
		// so it never needs the season's whole visit table.
		var found []domain.Visit
		var foreign []string
		foreignSites := map[string]bool{}
		for _, id := range ids {
			row, err := domain.VisitByID(s.Store, id)
			if err != nil {
				s.musterFail(w, "visits", err)
				return
			}
			if row == nil {
				continue
			}
			// M-23 (James, 2026-10-03): a visit of ANOTHER post's site is
			// not this sheet's to print. It used to render under THIS
			// post's identity — "Muster · ice-road · REPRINT" naming a
			// person on site at the pad — so it is refused as loudly as an
			// unknown id, naming the post whose sheet it belongs on.
			if row.SiteID != s.Cfg.SiteID {
				foreign = append(foreign, id)
				foreignSites[row.SiteID] = true
				continue
			}
			found = append(found, *row)
		}
		if len(foreign) > 0 {
			sites := make([]string, 0, len(foreignSites))
			for id := range foreignSites {
				sites = append(sites, s.siteDisplay(id))
			}
			sort.Strings(sites)
			// James, 2026-10-04 (L10 M-23 LOW): the officer's sentence carries no
			// internal "muster print:" prefix; the log line keeps it.
			msg := fmt.Sprintf("%d visit id(s) belong to another post's records (%s): %s — a muster sheet names only this post's own visits; print that sheet at that post",
				len(foreign), strings.Join(sites, ", "), strings.Join(foreign, ", "))
			log.Print("muster print: " + msg)
			http.Error(w, msg, http.StatusGone)
			return
		}
		var err error
		// VisitsByID, not a fresh open-visits query: the frozen id list IS
		// the sheet. A visit that has since been checked out (or a new one
		// that checked in since) must neither vanish from nor be added to a
		// reprint. Passing only the found rows keeps its loud-failure
		// contract: any id that resolved to nothing above is reported by
		// name, never silently dropped from the sheet.
		//
		// M-5 (James, 2026-10-03): the CLOSED rows stay in frozen. The
		// board below lists only the open ones (§11), and the closed ones
		// are named under it with how and when they closed
		// (musterData.CheckedOutSince) — a departure since the sheet was
		// minted is now a line on the reprint, not a silent omission.
		frozen, err = domain.VisitsByID(found, ids)
		if err != nil {
			// A print link naming a visit id this store cannot resolve
			// (stale, hand-edited, or from a different database) must fail
			// LOUDLY — never silently render a muster sheet missing whoever
			// it couldn't find (verification finding: the worst failure
			// mode this feature could have).
			log.Printf("muster print: %v", err)
			http.Error(w, refusalText(err), http.StatusGone)
			return
		}
	} else {
		// No ?visits= at all: the direct, one-click-from-anywhere print path
		// (file comment) — snapshot THIS site's currently open visits right
		// now. Still grouped by snapshot only via MusterReprint below: every
		// printed sheet groups by snapshot, first print or reprint alike.
		open, err := s.openVisitsAtThisSite()
		if err != nil {
			s.musterFail(w, "visits", err)
			return
		}
		frozen = open
	}
	persons, err := domain.Persons(s.Store)
	if err != nil {
		s.musterFail(w, "persons", err)
		return
	}

	// MusterReprint — snapshot-only grouping, no alias map in scope at all
	// (domain/muster.go: the type-level guarantee a reprint never re-resolves).
	board := domain.MusterReprint(frozen, persons, s.Cfg.MusterMinGroupSize)
	data := musterData{SiteID: s.Cfg.SiteID, Mode: mode, Board: board, RenderedAt: musterSheetStamp(time.Now()), DuplicateIDs: duplicateIDs}
	data.RestoredVisits, data.RestoredCount = s.restoredVisitSet(frozen, board) // R-23: the frozen rows carry the key too
	// M-6 / T-L08-C (James, 2026-10-03): live at render time on the print
	// too — the sheet in hand says what the record holds about these names
	// NOW, like every exception section below.
	if data.Elsewhere, err = s.elsewhereTags(s.Cfg.SiteID, frozen); err != nil {
		s.musterFail(w, "also on site elsewhere", err)
		return
	}
	data.ElsewhereCount = countElsewhere(board, data.Elsewhere)
	if mode == "reprint" {
		// M-5 (James, 2026-10-03): name everyone on the original sheet whose
		// record has closed since, with how and when. Open rows produce
		// nothing here; the list is exactly the frozen set minus the board.
		if data.CheckedOutSince, err = s.checkedOutRows(frozen, personNameIndex(persons)); err != nil {
			s.musterFail(w, "checked out since", err)
			return
		}
		data.CheckedOutCount = len(data.CheckedOutSince)
	}
	if mode == "snapshot" {
		// The snapshot mints its OWN reprint link, from the set it just
		// rendered — never from the live board's, which is a different set
		// taken at a different moment. Without this the fresh-print route
		// would DESTROY the reprint mechanism rather than relocate it:
		// whoever is holding the paper would have no way to ask for this
		// exact sheet again, and the live page's link names whatever was
		// on site when that page loaded.
		//
		// Same six lines as the live board's (handleMusterLive), and the
		// same reason for sorting: a stable querystring regardless of
		// internal bucket-iteration order.
		ids := board.VisitIDs()
		sort.Strings(ids)
		data.PrintHref = "/muster/print?visits=" + strings.Join(ids, ",")
	}
	data.Banner, data.BannerErr = s.banner() // C7: on-glass even on the print-styled page; the partial hides itself from actual print
	// SYNC-HONESTY: the freshness signal. Live at render time on the print
	// too — a reprint's ROSTER is frozen (§8.8-i) but the sheet's claim about
	// what has reached this post is about the moment it was produced, and
	// this block, unlike the top bar, survives onto the paper.
	data.Sync = s.buildSyncSection()

	// F1: current, live incomplete-check-in state — deliberately NOT frozen
	// with the visit set above (see the musterData.Incomplete doc comment):
	// an incomplete check-in has no visit row yet, so it can never be part
	// of a frozen ?visits= set either way, and reporting it live is the
	// whole point — whoever is holding this printed sheet right now learns
	// whether the site currently knows of anyone still mid-check-in.
	incomplete, err := s.incompleteCheckInRows(personNameIndex(persons))
	if err != nil {
		s.musterFail(w, "incomplete check-ins", err)
		return
	}
	data.Incomplete = incomplete

	// 2026-08-20: like Incomplete just above, the away-at-pad section is
	// live at render time on the print too — never frozen with the visit
	// set (an away person has no visit row to freeze).
	if data.Away, err = s.awayPadRows(); err != nil {
		s.musterFail(w, "away at uncontrolled", err)
		return
	}
	if data.NotTracked, err = s.notTrackedRows(); err != nil {
		s.musterFail(w, "not tracked", err)
		return
	}
	// D-6, 2026-08-21: the people this post put on the road. Origin-scoped
	// (see musterData.InTransit) and, like the two sections above, live at
	// render time on both routes rather than frozen into a visit set — an
	// in-transit person holds no visit row anywhere to freeze.
	if data.InTransit, err = s.inTransitRows(); err != nil {
		s.musterFail(w, "in transit", err)
		return
	}
	// James, 2026-10-03: the crews this post sent out working on the road —
	// live on the print too, like the three sections above.
	data.RoadWorkLabel = s.Cfg.RoadWorkLabel()
	if data.RoadWork, err = s.roadWorkMusterRows(); err != nil {
		s.musterFail(w, "road work", err)
		return
	}
	if data.RoadWorkElsewhere, err = s.roadWorkElsewhereTags(data.RoadWork); err != nil {
		s.musterFail(w, "road work also on site elsewhere", err)
		return
	}
	data.countOffSite() // headcounts for the tally block; sums only, nothing new

	s.renderPage(w, "muster print", "muster.html", data)
}
