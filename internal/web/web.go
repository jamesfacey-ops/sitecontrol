// Package web holds the HTTP handlers and embedded templates. Server-rendered
// HTML only (design §6); templates ship inside the binary via embed.FS so the
// one-file deployment claim survives contact with the UI.
package web

import (
	"bytes"
	"embed"
	"html/template"
	"log"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/repl"
	"sitecontrol/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

var tmpl = template.Must(template.New("sitecontrol").Funcs(templateFuncs).ParseFS(templateFS, "templates/*.html"))

// renderPage renders a template to a BUFFER and only then writes it, which
// is the whole point (M-15).
//
// Demonstrated by execution, not reasoned about: `ExecuteTemplate(w, ...)`
// streams. html/template writes as it goes, so by the time a render error
// occurs the response has already begun — the status line is already 200
// and the bytes already sent are already on the wire. The handler's
// `log.Printf` then records the failure somewhere the operator will never
// look, and the browser is handed a page that simply STOPS.
//
// On the muster that is the catastrophic shape: HTTP 200, a sheet that
// reads as complete down to the on-site headcount, the roster truncated
// mid-way, no closing tags, and the print link live and clickable. The
// locked guard is "the muster must never be silently short", and this is
// the one failure mode in the whole system that produces exactly that with
// no cue on the page at all.
//
// Buffering makes the failure total instead of partial: either the whole
// page is written, or the operator gets a 500 that says something is
// wrong. A truthful error beats a plausible half-sheet.
//
// Applied at ALL EIGHT ExecuteTemplate sites, deliberately. Fixing the two
// muster routes alone would leave six handlers with the same defect and the
// same shape, and the next page to grow a conditional partial would
// reintroduce it silently.
//
// Cost: the largest page measured in this app is /trips at 275 KB at the
// documented 1,279-person season scale, so the buffer is not a
// consideration next to what it prevents.
func (s *Server) renderPage(w http.ResponseWriter, what, name string, data any) {
	s.renderInto(w, what, s.templates(), name, data)
}

// renderPageStatus is renderPage with an explicit status code, for a page
// that IS the refusal rather than a redirect away from it.
//
// The gate's vehicle forms need this: a refused commit must come back as
// the same page, carrying every accumulated passenger, with the reason at
// the top — losing a bus's worth of typing to a validation error would be a
// far worse failure than the one being prevented. The status is still an
// honest 409, so a raw POST or a test sees a refusal rather than a 200 that
// looks like success.
//
// It shares renderIntoStatus with renderPage, so the M-15 buffering
// contract below applies identically: nothing is written until the whole
// page rendered.
func (s *Server) renderPageStatus(w http.ResponseWriter, status int, what, name string, data any) {
	s.renderIntoStatus(w, status, what, s.templates(), name, data)
}

// renderInto is renderPage with the template set passed in, so the
// buffering contract above can be exercised against a deliberately failing
// template without touching the embedded set (html/template refuses Parse
// after Execute, so a test cannot add one to `tmpl`).
func (s *Server) renderInto(w http.ResponseWriter, what string, t *template.Template, name string, data any) {
	s.renderIntoStatus(w, http.StatusOK, what, t, name, data)
}

func (s *Server) renderIntoStatus(w http.ResponseWriter, status int, what string, t *template.Template, name string, data any) {
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		// Nothing has been written yet, so this can still be an honest
		// status code rather than a truncated 200.
		log.Printf("%s render: %v", what, err)
		http.Error(w, "the page could not be rendered — nothing here is a complete record; do not rely on it", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(buf.Bytes()); err != nil {
		// The client went away mid-write. Nothing to do but say so — the
		// page itself was complete when it left here.
		log.Printf("%s write: %v", what, err)
	}
}

type Server struct {
	Cfg     *config.Config
	Store   *store.Store
	Started time.Time

	// inflight counts the requests being served through Root (countRequests,
	// refusal.go); importMu lets one roster import commit at a time
	// (roster_import.go). FIX17b, sc-stress F14.
	inflight atomic.Int64
	importMu sync.Mutex

	// Sync and SyncStatus are the replication engine's handlers (C3a),
	// injected as plain http.Handlers so web never imports repl. Nil is
	// legal (tests of the web layer alone).
	Sync       http.Handler // mounted at /sync/ (patterns carry their own prefix)
	SyncStatus http.Handler // mounted at /debug/sync-status

	// SyncSnapshot, if set, returns the replication engine's diagnostic
	// snapshot (C3a/C3b) for the C10 sync-status render (§8.9). This is a
	// typed accessor, not another opaque http.Handler like Sync/SyncStatus
	// above: those two are pure transport mounts web never introspects, but
	// C10's render needs actual per-peer/per-stream state, so it follows the
	// SAME precedent Alerts *deadline.Deadlines already set — a typed
	// dependency imported directly when the page needs to read its data, not
	// merely mount its routes. Nil is legal (web-layer-only tests, and any
	// site.go composition that hasn't wired an engine yet): the sync-status
	// section then renders an explicit "unavailable" refusal — never a
	// silent omission that could be misread as "nothing to report".
	SyncSnapshot func() repl.Snapshot

	// Alerts is C7's deadline machinery. When set, every page renders the
	// §9.4 banner from a fresh Evaluate() — a server-side DB scan per
	// request, so no tab or process memory ever holds the only copy of an
	// alert — and the board's countdown/staleness columns come from the
	// persisted scan instead of C6's render-time stand-in. Nil is legal
	// (web-layer-only tests): the banner is absent and the C6 stand-in
	// columns remain.
	Alerts *deadline.Deadlines

	// Backup, if set, reports the nightly backup's facts for /status (FIX16
	// D: cmd/sitectl wires its scheduler's Facts; backup_status.go judges
	// them). Nil is legal (web-layer tests): the Backup line and tile are
	// then absent.
	Backup func() BackupFacts

	// tripFirstSeen anchors the inbound board's render-time countdown and
	// staleness columns (C6; PRESENTATION ONLY — no wall-clock column exists
	// in the store, by design). Process-memory is acceptable here precisely
	// because nothing alert-critical lives in it: C7 builds the persisted
	// server-side scan that owns real overdue evaluation (§9.4).
	tripSeenMu    sync.Mutex
	tripFirstSeen map[string]time.Time

	// recentSubmits / SubmitNow — James, 2026-10-03: duplicate-submit lock
	// covers deliberate declines (declinelock.go). Guarded by gateCommitMu.
	// SubmitNow is the window's clock (nil = time.Now; tests set it).
	recentSubmits map[string]recentSubmit
	SubmitNow     func() time.Time

	// Sessions is C11's site-login state (auth.go): logged-in browsers and
	// their picked session officer. Process-memory only, same acceptable
	// posture as tripFirstSeen above — see sessionStore's doc comment in
	// auth.go for why it holds no expiry of any kind, by design.
	Sessions *sessionStore

	// pages is the mux Handler() last built, read by isPage (auth.go) to
	// tell a page from a POST-only route when choosing where a re-login
	// returns to. Atomic because Handler() may be called again while a
	// previous mux is serving (tests do).
	pages atomic.Pointer[http.ServeMux]

	tmplOnce sync.Once
	tmplSet  *template.Template
}

// templates is the page template set bound to THIS server's config: the
// package-level tmpl carries the config-free sitelabel (id -> SiteLabel),
// which cannot know a display name; this clone-by-reparse overrides it with
// Cfg.SiteName so every page, title and heading names a site by its display
// name. James, 2026-10-04 (FINDINGS-R4 cluster I): no screen shows a raw site
// id. Built lazily, once per Server; tests that use the bare tmpl keep the
// old behaviour.
func (s *Server) templates() *template.Template {
	s.tmplOnce.Do(func() {
		funcs := template.FuncMap{}
		for k, v := range templateFuncs {
			funcs[k] = v
		}
		funcs["sitelabel"] = func(id string) string { return s.Cfg.SiteName(id) }
		funcs["assertedprose"] = func(v string) string { return destinationAssertedProse(s.Cfg.SiteName, v) }
		funcs["padname"] = s.padName
		s.tmplSet = template.Must(template.New("sitecontrol").Funcs(funcs).ParseFS(templateFS, "templates/*.html"))
	})
	return s.tmplSet
}

func New(cfg *config.Config, st *store.Store) *Server {
	if st != nil && st.SiteNamer == nil {
		st.SiteNamer = cfg.SiteName
	}
	return &Server{Cfg: cfg, Store: st, Started: time.Now(), Sessions: newSessionStore()}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	// THE FRONT DOOR IS THE OFFICER'S SCREEN. James, 2026-09-10: "the
	// default page for all sites other than the dispatch info only page
	// should be the sign in and out page". `GET /{$}` is Go's exact-root
	// pattern — it matches "/" and nothing else — so the catch-all below
	// keeps every other unmatched path. A REDIRECT rather than serving the
	// gate at two addresses: every gate POST already sends the officer back
	// to /gate, so /gate is the one URL they ever see. A 302, not a 301: a
	// browser caches a 301 for as long as it likes, and a future post type
	// whose front door is elsewhere (R-6's read-only dispatch surface — not
	// built, and no configuration for it is built here) must be able to
	// change its root without fighting a cached redirect.
	mux.HandleFunc("GET /{$}", s.handleDefaultPage)
	// THE STATUS PAGE — the box's own diagnostics (site id, epoch, schema,
	// db path, listen address, peers, sync state). ⟦was: "…, the weather board"⟧ It
	// used to BE the root; it now has an address of its own so it stays
	// reachable.
	mux.HandleFunc("GET /status", s.handleStatus)
	// THE CATCH-ALL. Go 1.22 ServeMux's "GET /" answers every GET no more
	// specific pattern claims. Deliberately STILL the status page — not the
	// gate, not a 404: a mistyped or unregistered path (a road post's
	// /orientation, which is NOT registered there — orientation.go) lands on
	// the diagnostic surface, which now SAYS the path has no page on this
	// post (statusData.FallThrough) instead of silently, and never on the
	// officer's working screen, where a wrong URL would look like a right
	// one. Every measured claim that "an unregistered GET falls through to
	// the status page" (web_test.go, orientation_test.go, heldout_test.go,
	// the W2/W9 route-exists helpers) stays true.
	mux.HandleFunc("GET /", s.handleStatus)
	// ⚠️ There is deliberately NO projection-dump route here, and none may be
	// added. D-56 (2026-09-01): "this program will not ever have a data dump
	// it will pull from the other nodes if we replace a node but that is a
	// full database propergation." GET /debug/projection-dump used to be
	// registered on this line, unauthenticated and outside cfg.DevRoutes, and
	// served every registered projection verbatim — every enrolled worker's
	// name, phone, employer and full travel history — to anyone who could open
	// a TCP connection to a post. Gating it behind cfg.DevRoutes would have
	// been a NO-OP: dev_routes = true in all four shipped deploy TOMLs.
	// The two-post determinism comparison that route existed for did NOT go
	// with it — it moved off the network entirely: `sitectl -config <toml>
	// -dump` writes the same canonical bytes on the post itself, and
	// cmd/dumpcompare diffs the two saved files (which it already supported).
	// Nothing is suppressed from that dump, so no column is blinded to the
	// comparison. See cmd/sitectl/main.go and reports/JAMES-DECISIONS.md D-56.
	if s.Sync != nil {
		// Method-split on purpose: a bare "/sync/" (all methods, specific
		// path) CONFLICTS with "GET /" (one method, less specific path)
		// under Go 1.22 ServeMux precedence — ServeMux panics at
		// registration, i.e. the binary died AT STARTUP whenever Sync was
		// wired (found by C7's E2E rehearsal, the first thing to ever boot
		// the fully-wired Handler; no unit test had wired Sync before —
		// TestHandlerBootsWithSyncWiredAndRoutesSyncTraffic now does).
		mux.Handle("GET /sync/", s.Sync)  // /sync/streams, /sync/pull
		mux.Handle("POST /sync/", s.Sync) // /sync/poke
	}
	if s.SyncStatus != nil {
		mux.Handle("GET /debug/sync-status", s.SyncStatus)
	}
	s.registerAuthRoutes(mux) // C11: login/logout/officer picker, see auth.go
	s.registerTripRoutes(mux) // C6: dispatch/arrival/cancel forms + inbound board, see trips.go
	// ⟦was: s.registerWeatherRoutes(mux) — POST /weather/declare; removed with
	// weather phases, James 2026-10-04. The route now answers 404.⟧
	if s.Alerts != nil {
		mux.HandleFunc("POST /alerts/ack", s.requireLogin(s.handleAckAlert)) // C7, see alerts.go; C11/F42 gate it
	}
	// ⟦W8⟧ The C5 dev-only reference-merge injection route USED TO BE
	// REGISTERED HERE and is DELETED, not gated: P-3 §5 schedules the
	// mechanism's deletion onto W8, and gating it behind cfg.DevRoutes would
	// have been a NO-OP — `dev_routes = true` in all four shipped deploy
	// TOMLs, which is [[D-56]]'s ERRATUM as a general fact about this
	// program. Only removal closes it. ⚠ cfg.DevRoutes itself SURVIVES and
	// the four TOMLs keep the key: it is what a future dev route would be
	// gated on, and W8 asserts the shipped value rather than changing it.
	s.registerAnalyticsRoutes(mux)      // dispatch analytics page, see analytics.go
	s.registerMusterRoutes(mux)         // C9: muster view + print, see muster.go
	s.registerGateRoutes(mux)           // C8: gate check-in/check-out UI, see gate.go
	s.registerIntakeRoutes(mux)         // person intake + roster, see intake.go
	s.registerRosterImportRoutes(mux)   // FIX16 H: bulk roster import from CSV, see roster_import.go
	s.registerPersonAmendRoutes(mux)    // W3: person amendment + merge (D-46), see person_amend.go
	s.registerTimeCorrectRoutes(mux)    // W4: the time-correction surface (item 14), see time_correct.go
	s.registerCompanyAmendRoutes(mux)   // W5d: company amendment + merge (D-51), see company_amend.go
	s.registerTripAmendRoutes(mux)      // W9a: trip/arrival fact correction (item 15), see trip_amend.go
	s.registerArrivalUnrefRoutes(mux)   // W9b: F4 §3.6's un-reference (item 16), see unref.go
	s.registerArrivalResolveRoutes(mux) // James, 2026-09-30: officer resolution of parked arrivals (Option 1), see resolve.go
	s.registerAlertUnackRoutes(mux)     // W9c: F6 §3.4's alert un-acknowledgment (item 17), see unack.go
	s.registerVehicleAmendRoutes(mux)   // W7: the vehicle entity (item 9) — DR-W7-17, see vehicle_amend.go
	s.registerTradeRoutes(mux)          // W8: the trade register (item 10) — naming and numbers, see trade.go
	// SCHEMA-11 step 4 (D-88/D-95/D-91): the orientation marking surface
	// exists ONLY at a post whose own config says it administers an
	// orientation. Elsewhere the routes are NOT REGISTERED — never merely
	// "disabled" — the dev_routes pattern; and the condition is this post's
	// own key, never a site name (D-77). See orientation.go.
	if s.Cfg.HasOrientation() {
		s.registerOrientationRoutes(mux)
	}
	s.registerThemeRoutes(mux)          // the light/dark switch's one route, see theme.go
	s.registerSearchRoutes(mux)         // FIX9: read-only records search, see search.go
	s.registerMusterAtRoutes(mux)       // FIX16a: muster for any moment + last seen, see muster_at.go
	s.registerReportRoutes(mux)         // FIX16: the sitectl -report reports as pages + CSV, see reports.go
	s.registerDispatchReportRoutes(mux) // FIX16 E: read-only dispatch reports, see dispatch_reports.go
	s.registerHistoryRoutes(mux)        // FIX16f: overdue, missed sign-out and lost-signal history, see history.go
	s.pages.Store(mux)
	// Handler() itself stays a bare *http.ServeMux — several tests
	// (w9a/w9b/w9c's routeExists helpers among them) type-assert this
	// return value to *http.ServeMux to interrogate its own routing table
	// without invoking a handler, so nothing may wrap it here. The theme
	// rewrite (ThemeMiddleware, theme.go) is layered on OUTSIDE this
	// method instead — see cmd/sitectl/main.go's http.Server wiring.
	return mux
}

// banner runs the C7 scan for a page render. A scan failure is NOT silent:
// the partial renders a loud "fall back to radio" strip (§9.5 — the system
// shows its own failure state rather than failing silently).
//
// The rows come back SORTED WORST FIRST (V-01). The sticky alert strip
// shows exactly one of them, so which one is at the front is not a
// presentation detail — it is the whole guarantee that the bar shows the
// worst outstanding thing rather than whatever the scan happened to emit
// first. See alertSeverity.
func (s *Server) banner() (rows []deadline.Alert, bannerErr string) {
	if s.Alerts == nil {
		return nil, ""
	}
	board, err := s.Alerts.Evaluate()
	if err != nil {
		log.Printf("alerts: scan failed: %v", err)
		return nil, refusalText(err)
	}
	rows = board.Banner()
	sortAlertsWorstFirst(rows)
	return rows, ""
}

// alertSeverity ranks the alert states the banner can carry. Higher is
// worse. This is a PRESENTATION order over states the scan already
// classified — it invents no judgement of its own.
//
//	overdue_live       a deadline has passed with the link up: the vehicle
//	                   is late and this box knows it. The worst thing on
//	                   the board.
//	overdue_unresolved the deadline passed while the link was down. Also
//	                   overdue, but the box cannot say by how much, so it
//	                   ranks below the one it can measure.
//	⟦was: phase_review — the weather VERIFY row; removed with weather
//	phases, James 2026-10-04⟧
func alertSeverity(a deadline.Alert) int {
	switch a.Status {
	case deadline.StatusOverdueLive:
		return 3
	case deadline.StatusOverdueUnresolved:
		return 2
	default:
		return 0
	}
}

// sortAlertsWorstFirst orders the banner so the sticky strip's single slot
// always holds the most serious outstanding alert: reds first, longest
// overdue first; then the ambers by SOONEST RECORDED DUE TIME (Q-FE2, James
// 2026-10-04), an amber with no due time on record after those; then by
// vehicle, so the order is total and a page reload never reshuffles two
// equal rows. ⟦was: ambers tied on OverdueBy (always zero for an amber) and
// fell to vehicle order — FINDINGS-R5 L09-03: "the most urgent of 2" put a
// 65-min run ahead of a 1-min run whose allowance had truly passed⟧
func sortAlertsWorstFirst(rows []deadline.Alert) {
	sort.SliceStable(rows, func(i, j int) bool {
		si, sj := alertSeverity(rows[i]), alertSeverity(rows[j])
		if si != sj {
			return si > sj
		}
		if rows[i].OverdueBy != rows[j].OverdueBy {
			return rows[i].OverdueBy > rows[j].OverdueBy
		}
		di, dj := rows[i].DueAt, rows[j].DueAt
		if di.IsZero() != dj.IsZero() {
			return !di.IsZero()
		}
		if !di.Equal(dj) {
			return di.Before(dj)
		}
		return rows[i].Vehicle < rows[j].Vehicle
	})
}

// alertTally is the persistent, non-collapsible count the sticky strip
// carries beside its one row. It is what makes showing ONE alert honest:
// the operator is never left to infer how many there are from how many they
// can see.
//
// The split is by the WORDS THE ROWS THEMSELVES USE, not by the underlying
// severity ranking. banner.html renders overdue_live as "VEHICLE OVERDUE"
// and everything else — including overdue_unresolved — as "VERIFY BY
// RADIO", because an expectation whose deadline passed while the link was
// down cannot be reported as a measured overdue. A count that grouped
// overdue_unresolved under "OVERDUE" would therefore say "2 OVERDUE" over a
// list showing one, which is the kind of disagreement between a summary and
// its own rows that makes a summary untrustworthy.
type alertTally struct {
	Total   int
	Overdue int // overdue_live — rendered "VEHICLE OVERDUE"
	Verify  int // everything else — rendered "VERIFY BY RADIO"
}

// FIX13 (James, 2026-10-05: "the overdue banner at the top should show a
// concise shortened list of the overdue trucks and number of pax instead of
// the large banner just a small list with truck number and number of
// people"). The sticky strip names every outstanding vehicle with its
// people aboard, one red line and one amber line, split by the same words
// alertTally splits by. A line shows at most stripItemsWide vehicles (and
// stripItemsPhone on a phone, where it must stay within two lines), then
// "+N more"; the full rows, with their acks, are on /trips.
const (
	stripItemsWide  = 6
	stripItemsPhone = 2
)

// alertStripLine is one colour's line. MoreWide and MorePhone are the
// "+N more" counts at each width — both rendered, one shown by CSS, so the
// count is right with scripting off.
type alertStripLine struct {
	Items     []alertStripItem
	MoreWide  int
	MorePhone int
}

// alertStripItem is one vehicle on a line. WideOnly marks the items past
// stripItemsPhone, which a phone does not show (its "+N more" counts them).
type alertStripItem struct {
	deadline.Alert
	WideOnly bool
}

type alertStrip struct {
	Overdue alertStripLine
	Verify  alertStripLine
}

// templateAlertStrip splits a banner slice (already worst-first) into the
// strip's two lines, keeping that order within each.
func templateAlertStrip(rows []deadline.Alert) alertStrip {
	var s alertStrip
	for _, a := range rows {
		l := &s.Verify
		if a.Status == deadline.StatusOverdueLive {
			l = &s.Overdue
		}
		n := len(l.Items)
		switch {
		case n < stripItemsWide:
			l.Items = append(l.Items, alertStripItem{Alert: a, WideOnly: n >= stripItemsPhone})
			if n >= stripItemsPhone {
				l.MorePhone++
			}
		default:
			l.MoreWide++
			l.MorePhone++
		}
	}
	return s
}

// templateAlertTally counts a banner slice for the strip. Exposed as a
// template function rather than a field so that all six pages that mount
// the alert surface keep their existing handler code unchanged — the strip
// derives the count from the same slice it is already given, and cannot
// disagree with it.
func templateAlertTally(rows []deadline.Alert) alertTally {
	t := alertTally{Total: len(rows)}
	for _, a := range rows {
		if a.Status == deadline.StatusOverdueLive {
			t.Overdue++
		} else {
			t.Verify++
		}
	}
	return t
}

// incompleteCheckInRows lists this site's row-31 crash-recovery trips (an
// interrupted GateCheckIn: dispatched, never arrived — checkin.go's CRASH
// WINDOW) as render-ready rows. Shared by gate.go (the recovery surface,
// where Resume lives) and muster.go (F1: the muster — live AND printed —
// must say when it knows it is incomplete, not only /gate). personName is
// an id->name lookup the caller already loaded (F30: avoids a second full
// Persons() dump per render).
//
// ⟦orientation-doors, gap 1, 2026-09-11⟧ NeedsOrientation is computed here,
// live, for every occupant — the SAME read decorateRows makes for the
// accumulate form's own warning (domain.OrientationHeld against THIS
// post's s.Cfg.SiteID, never a peer's) — but only when
// s.Cfg.HasOrientation(): a road post gets an empty slice on every row, by
// construction, so its /gate page can render nothing at all (case (d)'s
// same guarantee, extended to this door). muster.go's callers ignore the
// field; it costs them one skipped-by-the-template slice, not a second
// query shape, since it is built inside the SAME per-trip occupant loop
// already running here for Occupants — see F30 above.
func (s *Server) incompleteCheckInRows(personName map[string]string) ([]incompleteRow, error) {
	incomplete, err := domain.IncompleteCheckIns(s.Store, s.Cfg.SiteID)
	if err != nil {
		return nil, err
	}
	checkOrientation := s.Cfg.HasOrientation()
	var out []incompleteRow
	for _, t := range incomplete {
		// Per-trip PK-prefix lookup (F30), not the season's whole occupant
		// table — the incomplete set is always small (row 31's crash window
		// is millisecond-wide).
		occ, err := domain.TripOccupantsByTrip(s.Store, t.TripID)
		if err != nil {
			return nil, err
		}
		row := incompleteRow{
			// ⟦LOCK — W7 seam repair, 2026-09-03⟧ ⟦was: "Vehicle: t.VehicleEntered"⟧
			TripID: t.TripID, Vehicle: tripVehicleDisplay(t.VehicleIsUnidentified(), t.VehicleEntered),
			Driver: personName[t.DriverPersonID], Occupants: len(occ),
		}
		if checkOrientation {
			for _, o := range occ {
				held, err := domain.OrientationHeld(s.Store, o.PersonID, s.Cfg.SiteID)
				if err != nil {
					return nil, err
				}
				if held == nil {
					row.NeedsOrientation = append(row.NeedsOrientation, resumeOrientationRow{
						PersonID: o.PersonID, Name: personName[o.PersonID],
					})
				}
			}
		}
		out = append(out, row)
	}
	return out, nil
}

type statusData struct {
	Cfg           *config.Config
	SchemaVersion string
	DBPath        string
	Uptime        string
	Banner        []deadline.Alert
	BannerErr     string
	Sync          syncStatusView // C10, §8.9
	Shift         shiftTotalsView // FIX16 G
	// ⟦was: Phase phaseView / PhaseErr — the weather-phase panel and its
	// declaration buttons; removed with weather phases, James 2026-10-04⟧
	// FallThrough is the request path when this page was reached through
	// the "GET /" catch-all rather than at /status — i.e. the path has no
	// page on this post. Rendered as a notice naming the path, so a road
	// post's /orientation (not registered there) explains itself instead of
	// silently showing diagnostics. Empty at /status.
	FallThrough string
	// Backup is the nightly backup's state (FIX16 D), nil when not wired.
	// BackupTile: the compact tile shows on a post's own status page, not
	// when the page is reached through the dispatch host.
	Backup     *backupView
	BackupTile bool
	// FIX16f: the newest lost-signal outages this post recorded
	// (signal_history.go), with the full history at /history/signal.
	SignalHistory     []signalRowView
	SignalNotRecorded bool
	SignalErr         string
}

// buildSyncSection produces the C10 view model for one page render. A nil
// SyncSnapshot (engine not wired) is reported as Available=false, rendered
// by the template as an explicit refusal — see syncstatus.go's doc comment.
// The configured sync interval goes in with the snapshot: it is the one
// input the Healthy verdict's age ceiling is derived from (syncstatus.go's
// syncStaleMultiple), so the ceiling follows sync_interval_seconds instead
// of a number of seconds written anywhere in this package.
func (s *Server) buildSyncSection() syncStatusView {
	if s.SyncSnapshot == nil {
		return syncStatusView{Available: false}
	}
	return buildSyncStatusView(s.SyncSnapshot(), s.Cfg.SyncInterval())
}

// handleStatus renders the trivial status page: proof the server is up, the
// config loaded, and the self-created DB answers a real query.
// handleDefaultPage is "GET /{$}": the front door sends the officer to the
// sign in/out page. See Handler() for why a redirect and why 302.
func (s *Server) handleDefaultPage(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/gate", http.StatusFound)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	sv, err := s.Store.StoredSchemaVersion()
	if err != nil {
		log.Printf("status: %v", err)
		http.Error(w, dbFailBody(err), http.StatusInternalServerError)
		return
	}
	data := statusData{
		Cfg:           s.Cfg,
		SchemaVersion: sv,
		DBPath:        s.Store.Path,
		Uptime:        time.Since(s.Started).Round(time.Second).String(),
	}
	if r.URL.Path != "/status" {
		data.FallThrough = r.URL.Path // reached through the catch-all: say so
	}
	data.Banner, data.BannerErr = s.banner()
	data.Sync = s.buildSyncSection()
	if s.Backup != nil {
		b := judgeBackup(s.Backup())
		data.Backup = &b
		data.BackupTile = !dispatchHost(r.Host)
	}
	if rows, notRecorded, err := s.buildSignalRows(historyFilter{}, time.UnixMilli(s.clockReading().WallMS), statusSignalRows); notRecorded {
		data.SignalNotRecorded = true
	} else if err != nil {
		log.Printf("status: %v", err)
		data.SignalErr = refusalText(err)
	} else {
		data.SignalHistory = rows
	}
	data.Shift = s.shiftTotalsFor(r)
	s.renderPage(w, "status", "status.html", data)
}
