// trips.go — C6's minimal trip UI: dispatch / arrival / cancel forms and the
// inbound board (open expectations + countdown + staleness; contents
// invented per chunk-plan finding F3). Enough to drive demo steps 2/4/5 by
// hand; the polished gate check-in form is C8's, not this file's.
//
// Countdown and staleness are PRESENTATION ONLY, computed at render time
// from an in-memory first-seen anchor — no wall-clock column exists in the
// store, by design (§8.3: ordering is (node_epoch, seq)). This anchor lives
// and dies with the process and a tab; that is acceptable for C6's board
// precisely because C7 builds the real machinery — the server-side DB scan
// with the persisted (receipt wall-time, remaining-at-receipt) reboot
// bridge, the X−G rule, and the overdue-unresolved review lane (§9.3/§9.4).
// Nothing ALERT-critical is allowed to live only here.
package web

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// registerTripRoutes mounts the C6 trip surface. Called from Handler().
// GET /trips stays UNGATED (C11, design §12: accountability, not
// confidentiality — no reason to gate a read); every POST that authors an
// event is wrapped in C11's requireLogin. D-5 retired the second wrapper
// (requireOfficer): each of these calls postAttribution, which answers from
// config and never needed a session choice to be made first.
func (s *Server) registerTripRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /trips", s.handleTripsPage)
	// U2: /trips is now the OPERATOR CONSOLE (the truck at the window) and
	// /trips/detail is the reference/audit view (all trips, visits, the
	// out-at-pad panel). Every pre-existing POST target, form field name
	// and route is unchanged — this is a re-composition of the same
	// surfaces onto two pages, not a change to what the system does.
	mux.HandleFunc("GET /trips/detail", s.handleTripsDetail)
	mux.HandleFunc("GET /trips/detail.csv", s.requireLogin(s.handleTripsDetail)) // FIX17d
	mux.HandleFunc("POST /trips/dispatch", s.requireLogin(s.handleDispatch))
	mux.HandleFunc("POST /trips/arrive", s.requireLogin(s.handleExpectedArrival))
	mux.HandleFunc("POST /trips/arrive-unsolicited", s.requireLogin(s.handleUnsolicitedArrival))
	// James, 2026-10-03: a turned-back truck, recorded back at its origin
	// from the origin's OUTBOUND board (domain/trip_return.go).
	mux.HandleFunc("POST /trips/returned", s.requireLogin(s.handleReturnedToOrigin))
	// ⟦D-78, 2026-09-05⟧ THE CANCEL ROUTE IS CLOSED.
	// ⟦was: mux.HandleFunc("POST /trips/cancel", s.requireLogin(s.handleCancel))⟧
	// [[D-73]] (James, verbatim): "no trip will be cancled they will continue
	// on to the next post then leave that post with a second trip". [[D-78]]:
	// close the route, keep the state. There is no officer-facing path to
	// domain.CancelTrip any more — handleCancel is removed with this line.
	// The `cancelled` / `arrived_after_cancel` states and their fold are
	// UNTOUCHED (already-replicated history carrying them must still fold);
	// only the door an operator could open is gone. What used to be reached
	// here is now the wait-until-it-arrives path the trip-amend refusal names
	// (trip_amend.go classBRefusal) and, for a return journey, a second
	// dispatch from the destination post (no code — MEASURED, d73-cancel-surface §5).
}

type boardRow struct {
	TripID    string
	Vehicle   string // VERBATIM entered form, always (M8)
	Origin    string
	Driver    string
	Occupants int
	// OccupantRows: one row per occupant for the per-person arrival
	// disposition (2026-08-20) — each can be marked "stayed behind" instead
	// of the arrival being one check-in-everyone action. Default is everyone
	// in (operator ruling: most people come back; the exception costs one
	// click, the common case zero).
	OccupantRows []domain.OccupantRef
	Deadline     string
	// Countdown is the FULL sentence the C7 scan produces. It is split for
	// display into two parts, because rendering a prose status at countdown
	// size produced a four-line headline reading "overdue-unresolved —
	// verify by radio" in 2.5rem type (seen in Firefox, 2026-08-20):
	//   CountdownValue — the NUMERAL, and only ever a numeral or an em-dash.
	//                    This is the thing somebody scanning the room reads
	//                    from their chair, so it gets --fs-countdown.
	//   CountdownNote  — the words that qualify it ("remaining", "OVERDUE
	//                    by", "verify by radio"), at ordinary size.
	// Both come from the same scan; nothing is invented and nothing is
	// dropped — the two concatenated are the sentence Countdown holds.
	Countdown      string
	CountdownValue string
	CountdownNote  string
	Staleness      string
	// ⟦was: PhaseFace (the weather rescale face) and Override (the
	// phase-override marker) — weather phases removed, James 2026-10-04⟧
	// Repudiation ⟦W9c, item 17 — F6 §5 R2⟧: "previously acknowledged by X,
	// un-acknowledged by Y", carried on ANY row for a trip whose ack was
	// withdrawn, acked or not. The acking post's name itself rides the
	// countdown suffix above (R1), which is what keeps the attribution in
	// the same breath as the urgency word on an alerting acked row —
	// "OVERDUE by 4m (acknowledged by Pad Site Control)".
	Repudiation string
	// Unackable ⟦W9c, item 17 — F6 §3.4⟧: a standing (unretracted)
	// acknowledgment exists for this trip, so the un-ack control is offered
	// and `POST /alerts/unack` would author rather than refuse. This board
	// row is the only place on /trips an ACKED expectation appears at all
	// (Banner() drops acked rows), so it is where the correction has to be
	// reachable from.
	Unackable bool
	// VerifyOnlyAck (FINDINGS-R5 F4): "acknowledged at Ice Pad as VERIFY
	// only — this red is not acknowledged" on a red row whose only standing
	// ack is an amber one; such a row is NOT Unackable.
	VerifyOnlyAck string

	// Urgency / UrgencyLabel (U2): the row's state as a STRUCTURED value,
	// so the board can encode it in FORM — an uppercase word, a stripe
	// weight, a sort position — and not in hue alone (UI brief: colour
	// blindness is uncontrolled for here). Derived from the SAME C7 scan
	// status the countdown text comes from; it never adds a judgement the
	// scan did not make.
	//   "overdue"    — StatusOverdueLive, the deadline has passed on a live link
	//   "unresolved" — StatusOverdueUnresolved, verify by radio
	//   "due"        — StatusCountdown, a timer is running
	//   "none"       — no deadline promised (an uncontrolled run has no timer)
	Urgency      string
	UrgencyLabel string
	// remaining orders the board within a band: soonest first in the "due"
	// band and — T-005 (L07), 2026-10-03 — LATEST FIRST in the "overdue"
	// band, carried as the negated OverdueBy so one comparator orders both.
	// Not rendered — the rendered figure is Countdown, which is the scan's
	// own text.
	remaining time.Duration

	// Outbound / Destination — James, 2026-10-03 (NEW-a): a row on the
	// OUTBOUND board, a run THIS post sent toward Destination. Same scan,
	// same four verdicts (the origin counts on its own clock from its own
	// dispatch instant — deadline/scan.go), same ack scope; the label reads
	// "OUTBOUND — …" so the two boards cannot be confused when a row from
	// each is on screen. No arrival form: the arrival is recorded at the far
	// end and closes this row when it replicates here.
	Outbound    bool
	Destination string
}

// urgencyRank orders the board so the row that needs a human first is
// first. Presentation only: the same rows, in an order that matches what
// the operator is scanning for.
func urgencyRank(u string) int {
	switch u {
	case "overdue":
		return 0
	case "unresolved":
		return 1
	case "due":
		return 2
	default:
		return 3
	}
}

type tripListRow struct {
	TripID      string
	Vehicle     string
	Origin      string
	Destination string
	State       string
	// Returned: arrived, and the arrival was recorded back at the trip's
	// own origin — a turned-back truck (James, 2026-10-03; derived,
	// domain.TripsReturnedToOrigin).
	Returned bool
	// Driver is the display name of the person the trip recorded as
	// driving it — TripDispatched.DriverPersonID, resolved through the
	// roster.
	//
	// WHY IT IS HERE. The driver flag exists for exactly one purpose, in
	// James's words: "in case something happens we can log that in our
	// report". Until this column, the flag was rendered in four places and
	// EVERY ONE of them was in-flight or crashed — the inbound board (open
	// expectations only), the alert rows (open trips with a live deadline),
	// /gate's "Incomplete check-ins" and the muster's MAY BE INCOMPLETE
	// table (crashed check-ins only). The moment a trip ARRIVED, its driver
	// disappeared from every human-readable view, and the only recovery was
	// GET /debug/projection-dump: raw JSON, where driver_person_id is an
	// opaque id that has to be hand-joined against the same dump's person
	// section. An incident report is written days later, by somebody who
	// will not do that. (That route no longer exists at all — D-56 removed
	// it — so the pre-table state of this finding is now unrecoverable
	// through the UI, not merely inconvenient.) This table — "All trips
	// known here", covering open, arrived and cancelled — is where "who was
	// driving vehicle X on that trip" is asked, so it is where the answer
	// belongs.
	Driver string
	// ReturnManifest is non-empty exactly for an OPEN uncontrolled run
	// authored at THIS site: the "Returned" action's per-person list, same
	// default and exception as the inbound board's arrival rows. Pad is the
	// run's destination display name for the row's labels.
	ReturnManifest []domain.OccupantRef
	Pad            string
	// ⟦was: Override — the phase-override marker; weather phases removed, James 2026-10-04⟧

	// ⟦W9a-1, W9a-2 consumer 11⟧ Chain is the trip's RECORD CHAIN: what it
	// was dispatched as, then every correction, each with the post that
	// made it. It is why an amendment is a NEW EVENT and never an edit
	// ([[D-1]]): the incident read answers D-5's question from the
	// EFFECTIVE record above, and the chain preserves what the record said
	// BEFORE. An amendment that erased its predecessor is the editing fold,
	// on a page. Attribution is STRUCTURAL — the authoring post's site id,
	// carried on the row — because the amendment payloads carry no
	// recorded_by by design (D-5).
	Chain []tripChainEntryView
}

// tripChainEntryView is one post's assertion in a trip's record chain.
type tripChainEntryView struct {
	Post     string
	Dispatch bool
	Facts    []tripChainFactView
}

// tripChainFactView is one asserted fact, rendered. Person-shaped facts
// carry the DISPLAY NAME: an incident report is read by a human, and an
// opaque person id hand-joined against a dump is exactly the recovery this
// column exists to remove.
type tripChainFactView struct {
	Label string
	Value string
}

type visitRow struct {
	Person  string
	Site    string
	Company string
	Trade   string
	Open    bool
}

type reviewRow struct {
	// Kind is the row's own short name for what happened, rendered BOTH as
	// the visible label and as `data-review-kind` (the structural handle
	// muster_print_test.go keeps style-free). ⚠ It is deliberately NOT
	// enumerated here: the previous list said
	// `"arrived-after-cancel" | "ambiguous-arrival"` and was already wrong —
	// buildReviewLane emitted a third kind. The property is what holds: one
	// distinct Kind per loop in buildReviewLane, which is the only producer.
	Kind string
	// Label is the visible heading, in words (FINDINGS-R5 F13: the review
	// lane led with the raw "ambiguous-arrival:" token). Kind stays the
	// structural hook. ⟦was: the template rendered <strong>{{.Kind}}</strong>⟧
	Label  string
	Detail string
	// Resolve — James, 2026-09-30: officer resolution of parked arrivals
	// (Option 1) — is the resolution control of an ambiguous-arrival row
	// (resolve.go): nil on every other kind, and nil at a post that did not
	// record the arrival (D-87: that post may only radio).
	Resolve *resolveControl
}

// tripsData is the OPERATOR CONSOLE (/trips): what somebody with a truck
// at the window needs, and nothing else. The reference tables that used to
// be stacked underneath it (all trips, visits, the out-at-pad panel) moved
// to tripsDetailData below — same rows, same routes, a page you go to
// rather than one you scroll past while a driver waits.
type tripsData struct {
	SiteID string
	// TimeWarnings are TP §4.6's warn-accept notices, carried across the
	// accept path's redirect as query codes (timecapture.go) and rendered
	// as visible time-warning notices on this, the page the operator sees
	// next.
	TimeWarnings []string
	// NotEntered ⟦H-8 / T-084 / T-093, 2026-09-29⟧ is the ORIENTATION
	// hold-out notice (notentered.go) for the two console arrival doors —
	// POST /trips/arrive and POST /trips/arrive-unsolicited both land back
	// here. The ids rode the redirect from the day the first door was wired
	// (handleExpectedArrival's notEnteredRedirect) and this page never read
	// them: the officer who had just held a person out at the gate was
	// told nothing, while the SAME key rendered on /gate and /gate/vehicle.
	// Resolved off the current roster read, never off the query string,
	// exactly as gateData.NotEntered is (R-9: a refusal MUST tell the
	// officer the next action).
	NotEntered []notEnteredNotice
	// Cfg carries the site's DISPLAY NAME to the shared top bar (U1
	// carry-forward note 1: the operator pages showed a bare "site-a" with
	// no human name, and adding it was left as a deliberate handler change
	// for this chunk). The bar reads Cfg.DisplayName only; nothing else on
	// this page touches it.
	Cfg   *config.Config
	Board []boardRow
	// Outbound (NEW-a, James 2026-10-03): the controlled runs THIS post sent
	// out, with the scan's own verdict — the sending post's half of every
	// deadline. ShowOutbound renders the section (with its explicit empty
	// state) at a post that can dispatch to a controlled destination, and
	// whenever a row exists; a post with no road has nothing to list and
	// gets no always-empty heading.
	Outbound     []boardRow
	ShowOutbound bool
	Review       []reviewRow
	// Returns: open uncontrolled runs authored HERE, each with its
	// per-person return manifest. This is an ARRIVAL action, so it belongs
	// on the console with the inbound board rather than in the trip list it
	// used to be buried in.
	Returns []tripListRow
	// Destinations is the PEER-SITE id list — it feeds the arrival form's
	// "came from" <select> (an unsolicited arrival's origin is always a
	// controlled site; a pad cannot dispatch anything). The dispatch form
	// no longer uses it: it renders one button per configured destination
	// from DispatchDestinations instead.
	Destinations []string
	// DispatchDestinations: config.DestinationList() — one dispatch button
	// each; controlled ones require the deadline, uncontrolled ones must not
	// carry one (both directions enforced in domain.Dispatch). Adding a pad
	// is config-only.
	DispatchDestinations []config.Destination
	Banner               []deadline.Alert // C7 (§9.4): rendered by the alert_banner partial
	BannerErr            string
	Post                 string // D-5: this box's post — rendered by the post_chip partial

	// RoadWork (James, 2026-10-03): every open WORKING-ON-THE-ROAD run this
	// post knows — its own first, with the scan's verdict and the "back from
	// the road" check-in; other posts' after, with the "arrived here instead"
	// door (web/road_work.go). RoadWorkChoice is the dispatch form's button:
	// present only where this post's config offers the category.
	RoadWork       []roadWorkRow
	RoadWorkChoice roadWorkChoice

	// DispatchPrefill: per-destination deadline_minutes prefill from the
	// configured travel minutes (config.TravelMinutesTo) — the officer may
	// still adjust it. Missing destination = no prefill. ⟦was: read "at the
	// CURRENT phase", beside Phase/PhaseErr, the dispatch form's weather
	// gate framing — weather phases removed, James 2026-10-04⟧
	DispatchPrefill map[string]int64

	// --- U2 person pickers (picker.go). These replaced the two
	// <select multiple size="5"> controls and the full-roster driver
	// <select>. Field names are unchanged. ---
	DriverPicker   personPicker
	OccupantPicker personPicker
	ArrivalPicker  personPicker
	OnSiteCount    int
	RosterCount    int

	// Sync (SYNC-HONESTY, 2026-09-09): the REAL replication state, from the
	// same snapshot / and /roster render (Server.buildSyncSection) — the
	// shared top bar draws its chip from it. This console's inbound board
	// and return manifests are records the OTHER post authored, which reach
	// this one only through sync; until this field existed a dispatcher
	// sitting here learned nothing about whether that link was current. A
	// post with no engine wired reports Available=false, rendered as
	// "cannot tell", never as "healthy".
	Sync syncStatusView
}

// tripsDetailData is the reference/audit view (/trips/detail): every trip
// this box knows about, every visit, and the out-at-pad panel. Nothing here
// is new and nothing was dropped — it is the half of the old /trips that
// answers "what does the record say?" rather than "what do I do with this
// truck?".
type tripsDetailData struct {
	SiteID string
	Cfg    *config.Config // see tripsData.Cfg
	Trips  []tripListRow
	Visits []visitRow
	// Range: which trips Trips holds and the links around it (FIX17d).
	Range tripsRange
	// OutAt: who the record says is still out at each uncontrolled pad
	// (domain.OutAtUncontrolled) — the "if something happens at that pad" answer.
	OutAt     []domain.UncontrolledOut
	Banner    []deadline.Alert
	BannerErr string
	Post      string
}

// firstSeen returns (and records on first sight) the render-time anchor for
// an open expectation. Presentation only — see the file comment.
// peerSiteIDs is the caller-known set of real peer sites (config.Peers) —
// who this post REPLICATES with: the unsolicited-arrival form's origin
// <select> (an arrival can genuinely come from any peer) and the picker's
// per-peer on-site sections. It is NOT the dispatch set. handleDispatch and
// the gate's outbound door hand Dispatch config.ControlledDestinationIDs()
// as DispatchInput.KnownDestinationSites — who this post may DISPATCH TO —
// which is startup-validated to be a subset of this list and, since D-77, a
// strict subset at any post with more than one peer. Handing Dispatch THIS
// list let a raw POST dispatch to a peer no form offers (R3-LINKAGE.md).
func (s *Server) peerSiteIDs() []string {
	out := make([]string, 0, len(s.Cfg.Peers))
	for _, p := range s.Cfg.Peers {
		out = append(out, p.SiteID)
	}
	return out
}

func (s *Server) firstSeen(tripID string, now time.Time) time.Time {
	s.tripSeenMu.Lock()
	defer s.tripSeenMu.Unlock()
	if s.tripFirstSeen == nil {
		s.tripFirstSeen = map[string]time.Time{}
	}
	if t, ok := s.tripFirstSeen[tripID]; ok {
		return t
	}
	s.tripFirstSeen[tripID] = now
	return now
}

// buildTripRowsWith is the "all trips known here" list — unchanged rows,
// used by both pages: the console keeps only the open uncontrolled runs
// (their return manifests are an ARRIVAL action; buildReturnRows), the
// detail view shows all of them with the cancel action.
// ⟦W9a-2 consumers 1 and 11⟧ withChain is set only by /trips/detail, the
// reference/audit view: the chain is one extra scoped read per trip and the
// operator console does not render it.
func (s *Server) buildTripRowsWith(personName map[string]string, withChain bool) ([]tripListRow, error) {
	return s.buildTripRowsScoped(personName, withChain, false, nil)
}

// buildReturnRows is the console's "Returns from the pads" list: exactly the
// rows of buildTripRows that carry a return manifest, in the same order.
//
// FIX17 (perf, 2026-10-06): a manifest is only ever built for a trip whose
// state is OPEN (buildTripRowsScoped's own condition), so the trips that are
// not open are skipped BEFORE their composition is read. A season holds a
// trip per gate check-in (~14k at season scale), and composing every one of
// them — four scoped reads each — to keep the handful of open pad runs made
// /trips take 16.8 s at Ice Road. Skipping them changes no row: the kept
// rows are built by the same code from the same reads. The RETURNED fact is
// not read either — it is only ever true for an arrived trip, never an open
// one, so every kept row's Returned is false either way.
func (s *Server) buildReturnRows(personName map[string]string) ([]tripListRow, error) {
	rows, err := s.buildTripRowsScoped(personName, false, true, nil)
	if err != nil {
		return nil, err
	}
	var out []tripListRow
	for _, row := range rows {
		if len(row.ReturnManifest) > 0 {
			out = append(out, row)
		}
	}
	return out, nil
}

// buildTripRowsScoped is buildTripRowsWith, optionally limited to the trips
// whose state is open (openOnly; see buildReturnRows), and optionally to the
// trips keep accepts (FIX17d: /trips/detail's day range). keep is asked of
// the raw trip BEFORE its row is composed, so a trip outside the range costs
// nothing; a kept trip's row is built by the same code from the same reads.
func (s *Server) buildTripRowsScoped(personName map[string]string, withChain, openOnly bool, keep func(domain.Trip) bool) ([]tripListRow, error) {
	trips, err := domain.Trips(s.Store)
	if err != nil {
		return nil, err
	}
	states, err := domain.TripStates(s.Store)
	if err != nil {
		return nil, err
	}
	stateOf := make(map[string]string, len(states))
	for _, st := range states {
		stateOf[st.TripID] = st.State
	}
	returned := map[string]bool{}
	if !openOnly {
		if returned, err = domain.TripsReturnedToOrigin(s.Store); err != nil {
			return nil, err
		}
	}
	destDisplay := map[string]string{}
	for _, d := range s.Cfg.DestinationList() {
		destDisplay[d.ID] = d.DisplayName
	}
	live, err := domain.NewLiveVehicles(s.Store)
	if err != nil {
		return nil, err
	}
	var out []tripListRow
	for _, raw := range trips {
		if openOnly && stateOf[raw.TripID] != domain.TripStateOpen {
			continue
		}
		if keep != nil && !keep(raw) {
			continue
		}
		// ⟦W9a-2 consumer 1⟧ THE D-5 INCIDENT READ COMPOSES. This is the one
		// table that covers open, arrived AND cancelled trips, so it is
		// where "who was driving vehicle X on that trip" is asked — and the
		// answer owed is the EFFECTIVE one, with the superseded assertions
		// still readable in the chain beneath it.
		t := raw
		vehicle := ""
		roadWork := false
		if c, cerr := domain.EffectiveTrip(s.Store, raw.TripID); cerr != nil {
			return nil, cerr
		} else if c != nil {
			t = c.Trip
			roadWork = c.RoadWork
			if stateOf[t.TripID] == domain.TripStateOpen {
				// ⟦James, 2026-10-03 — DR-W7-18⟧ an OPEN trip answers "which
				// truck is out now": composed LIVE, the same truck the alert,
				// the inbound board and the origin's sheet name. A closed trip
				// keeps the number it went out with (the FROZEN row).
				if vehicle, _, err = live.Compose(*c); err != nil {
					return nil, err
				}
			} else {
				vehicle = tripVehicleDisplay(t.VehicleIsUnidentified(), t.VehicleEntered)
			}
		} else {
			vehicle = tripVehicleDisplay(t.VehicleIsUnidentified(), t.VehicleEntered)
		}
		row := tripListRow{
			// ⟦LOCK — W7 seam repair, 2026-09-03⟧ ⟦was: "Vehicle: t.VehicleEntered"⟧
			TripID: t.TripID, Vehicle: vehicle, Origin: t.OriginSite,
			Destination: "— (local)", State: stateOf[t.TripID], Returned: returned[t.TripID],
			Driver: personName[t.DriverPersonID],
		}
		if withChain {
			chain, cerr := s.tripChainView(t.TripID, personName)
			if cerr != nil {
				return nil, cerr
			}
			row.Chain = chain
		}
		if row.Driver == "" && t.DriverPersonID != "" {
			// The roster row is missing or has not replicated here yet.
			// Show the id rather than an empty cell: an empty cell reads as
			// "no driver was recorded", which is a different and much worse
			// claim than "this box cannot name them yet".
			row.Driver = t.DriverPersonID
		}
		if roadWork {
			// Cluster D (James, 2026-10-04): a road-work run is destination-
			// less but is NOT a local check-in. ⟦was: "— (local)"⟧
			row.Destination = "— " + domain.RoadWorkDestinationText
		} else if t.DestinationSite != nil {
			row.Destination = s.Cfg.SiteName(*t.DestinationSite)
		} else if t.DestinationUncontrolled != nil {
			// A replicated trip can name a pad this site does not configure;
			// fall back to the recorded id so the row never reads "local".
			// ⟦fix4 H⟧ ⟦was: the raw id when this post does not configure it⟧
			name := *t.DestinationUncontrolled
			if dn, ok := destDisplay[name]; ok {
				name = dn
			} else {
				name = s.padName(name)
			}
			row.Destination = name + " (uncontrolled — no timer)"
			// An OPEN run authored HERE gets the per-person "Returned"
			// manifest (2026-08-20): each occupant defaults to check-in, any
			// of them can be marked as stayed at the pad with one click.
			if row.State == domain.TripStateOpen && t.OriginSite == s.Cfg.SiteID {
				row.Pad = name
				// FINDINGS-R4 D1: the EFFECTIVE occupants, as the arrival
				// this manifest feeds now checks in ⟦was: TripOccupantsByTrip,
				// the raw dispatch rows — a corrected driver or an "Aboard
				// after all" person could not be ticked as stayed at the pad⟧.
				c, err := domain.EffectiveTrip(s.Store, t.TripID)
				if err != nil {
					return nil, err
				}
				// F15 (James, 2026-10-05): less anyone back at this post
				// since the run left — off the run everywhere, as the
				// muster's pad list already has them.
				occ, err := domain.StillAboard(s.Store, c)
				if err != nil {
					return nil, err
				}
				for _, id := range occ {
					n := id
					if p, err := domain.PersonByID(s.Store, id); err != nil {
						return nil, err
					} else if p != nil {
						n = p.Name
					}
					row.ReturnManifest = append(row.ReturnManifest, domain.OccupantRef{PersonID: id, Name: n})
				}
				sortOccupantRefs(row.ReturnManifest)
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// sortOccupantRefs puts a manifest in name order. The list is read aloud
// against the people standing in front of the operator, so it has to be in
// the order a human scans, not the order the occupant rows were authored.
func sortOccupantRefs(refs []domain.OccupantRef) {
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Name != refs[j].Name {
			return refs[i].Name < refs[j].Name
		}
		return refs[i].PersonID < refs[j].PersonID
	})
}

// buildReviewLane renders C7/§9.1/§9.2's "needs a human" rows.
func (s *Server) buildReviewLane() ([]reviewRow, error) {
	lane, err := domain.LoadReviewLane(s.Store)
	if err != nil {
		return nil, err
	}
	var out []reviewRow
	for _, t := range lane.ArrivedAfterCancel {
		out = append(out, reviewRow{
			Kind:   "arrived-after-cancel",
			Label:  "Arrived after it was called off",
			Detail: fmt.Sprintf("trip %s arrived after its cancel — confirm with the origin and resolve by hand (§9.1)", t.TripID),
		})
	}
	for _, a := range lane.Ambiguous {
		candidates := ""
		if a.Candidates != nil {
			candidates = *a.Candidates
		}
		claim, err := s.arrivalClaim(a.ArrivalID)
		if err != nil {
			return nil, err
		}
		// ⟦Option 1⟧ the copy says WHEN the match was made: a candidate may
		// have been closed since (the row annotates it), and the parked
		// arrival never re-binds by itself.
		ctl, err := s.resolveControlFor(a)
		if err != nil {
			return nil, err
		}
		// James, 2026-10-04 (FINDINGS-R4 D25): "resolve it here" is only true
		// at the post that recorded the arrival — only it holds the control.
		// A peer shows "awaiting <post> (resolve there)" and no controls.
		tail := "verify which crew this is and resolve it here (§9.2)"
		if ctl == nil {
			if owner, oerr := domain.ArrivalSiteOf(s.Store, a.ArrivalID); oerr == nil && owner != "" && owner != s.Cfg.SiteID {
				tail = fmt.Sprintf("awaiting %s (resolve there) (§9.2)", s.peerDisplayName(owner))
			}
		}
		out = append(out, reviewRow{
			Kind:  "ambiguous-arrival",
			Label: "Matches more than one trip",
			Detail: fmt.Sprintf("arrival %s%s matched more than one open trip when it was recorded [%s] — the system will NOT pick; %s",
				a.ArrivalID, claim, candidates, tail),
			Resolve: ctl,
		})
	}
	for _, a := range lane.CancelledMatches {
		candidates := ""
		if a.Candidates != nil {
			candidates = *a.Candidates
		}
		claim, err := s.arrivalClaim(a.ArrivalID)
		if err != nil {
			return nil, err
		}
		out = append(out, reviewRow{
			Kind:  "arrived-after-cancel (unsolicited)",
			Label: "Matches a trip that was called off",
			Detail: fmt.Sprintf("arrival %s%s matches CANCELLED trip(s) [%s] — the origin called this off, but the vehicle showed up; confirm with the origin (§9.1)",
				a.ArrivalID, claim, candidates),
		})
	}
	// ⟦W9b, item 16⟧ the FOURTH loop, over the fourth ReviewLane list
	// (`tripauthor.go`'s `RefRetracted`, filled by `loadRefRetractedLane`).
	// It is F4 §3.6's THIRD and last operator-facing surface, and the one a
	// form-only build misses: `arrival_unref.html` tells the officer in three
	// places that the arrival "goes to the review lane on the trips board",
	// so without this loop that page states a record condition this one does
	// not have.
	//
	// ⚠ ALL THREE of F4 §3.6's named things land in the SAME row, on purpose
	// (W9b-5 is scoped to the row, not the page). The repudiated trip id is
	// already on the board row below and this post's own name is already in
	// the topbar, so a row naming only the arrival — or three rows naming one
	// thing each — is indistinguishable from the disposition never having
	// been rendered at all. That is mutant m22, the invisible disposition.
	//
	// ⚠ The sentence states the RECORD and stops. It does not say the trip is
	// back on the board: the retraction unbinds, and what the trip reverts to
	// is whatever the surviving raw facts say — `open`, `cancelled`, or still
	// `arrived` where something else binds it ([[D-58]]). Asserting a reopen
	// here would be right on the common case and a copy-lie on the other two.
	for _, r := range lane.RefRetracted {
		// r.TripID is the REPUDIATED trip, and there is no empty arm to
		// write: `computeStates`' Pass-1 skip sets the disposition and the
		// trip_id in the same assignment ("⚠ It keeps its trip_id: the
		// review-lane row names the REPUDIATED trip" — trip.go), and this
		// lane's scan selects on that disposition, so the two arrive
		// together or neither does.
		by := r.RetractedBy
		if by == "" {
			// Not a dead arm: `recorded_by` is `omitempty` on the wire and
			// nullable in the projection, so an unattributed retraction is
			// representable from a peer, and `loadRefRetractedLane` maps that
			// nil to "". Rendering "withdrawn by ;" would put a blank where
			// an attribution belongs. Same choice, for the same nil, as
			// arrival_unref.html's "at another post" arm.
			by = "a post that recorded no attribution"
		}
		// Fix round 3 (FINDINGS-R5 F13, L06-09): the row named the arrival and
		// the trip by raw id only and ended "resolve through review (F4
		// §3.6)" — a design-doc citation, with no control on any post (by
		// design: nothing here re-points an arrival, I-2). It now names the
		// truck, the posts and the people's sheet, and gives the one true
		// next step, a radio check. The ids stay last, for copying.
		// ⟦was: "arrival %s checked in against trip %s — reference withdrawn
		// by %s; the people are here; resolve through review (F4 §3.6)"⟧
		detail, err := s.refRetractedDetail(r, by)
		if err != nil {
			return nil, err
		}
		out = append(out, reviewRow{
			Kind:   "reference-withdrawn",
			Label:  "Arrival no longer matched to its trip",
			Detail: detail,
		})
	}
	return out, nil
}

// refRetractedDetail is the reference-withdrawn row's sentence (see
// buildReviewLane). It states the record and stops: it does not say the
// trip is back on the board, because what the trip reverts to is whatever
// its surviving facts say ([[D-58]]).
func (s *Server) refRetractedDetail(r domain.RefRetractedRow, by string) (string, error) {
	claim, err := s.arrivalClaim(r.ArrivalID)
	if err != nil {
		return "", err
	}
	at := "this post"
	if owner, oerr := domain.ArrivalSiteOf(s.Store, r.ArrivalID); oerr == nil && owner != "" {
		at = s.peerDisplayName(owner)
	}
	truck, from := "the trip", "its origin"
	if c, cerr := domain.EffectiveTrip(s.Store, r.TripID); cerr != nil {
		return "", cerr
	} else if c != nil {
		live, lerr := domain.NewLiveVehicles(s.Store)
		if lerr != nil {
			return "", lerr
		}
		if display, _, verr := live.Compose(*c); verr == nil && display != "" {
			truck = display
		}
		from = s.peerDisplayName(c.Trip.OriginSite)
	}
	return fmt.Sprintf("an arrival recorded at %s%s had been checked in against %s from %s — reference withdrawn by %s. "+
		"The people it checked in stay on %s's muster. This arrival closes no trip now, and nothing on this screen matches it to another one: "+
		"confirm by radio with %s which run this crew came on, and whether %s is still out (arrival %s, trip %s)",
		at, claim, truck, from, by, at, from, truck, r.ArrivalID, r.TripID), nil
}

// arrivalClaim renders what an unsolicited arrival CLAIMED — the EFFECTIVE
// vehicle and origin (§9.2 consumer 9), with §4's record-only sentence
// attached when a correction has been made.
//
// ⚠ The claim is a RECORD, not a decision. §5 rule 4: correcting it never
// re-runs correlation, never binds and never un-binds — so this row's
// candidate list is unchanged by a correction, and the sentence says so
// rather than leaving a human to assume the system re-matched.
func (s *Server) arrivalClaim(arrivalID string) (string, error) {
	c, err := domain.EffectiveArrival(s.Store, arrivalID)
	if err != nil || c == nil {
		return "", err
	}
	var parts []string
	// ⟦SchemaVersion 14, R-18, 2026-09-13⟧ An unidentified arrival is
	// STRUCTURALLY unreachable through this function today — correlation
	// (domain.computeStates) treats "no id, no entered string" as
	// uncorrelatable and always lands it as a plain visit, never in the
	// ambiguous/cancelled-match review lanes this claim renders for — but
	// the rule R-17 established for `trip` ("never a blank cell, always
	// say Vehicle unknown") is asserted here directly rather than assumed
	// to hold merely because nothing exercises it yet.
	if c.Arrival.VehicleIsUnidentified() {
		parts = append(parts, domain.VehicleUnidentifiedText)
	} else if c.Arrival.VehicleEntered != nil && *c.Arrival.VehicleEntered != "" {
		parts = append(parts, "vehicle "+*c.Arrival.VehicleEntered)
	}
	if c.Arrival.OriginSite != nil && *c.Arrival.OriginSite != "" {
		parts = append(parts, "from "+s.Cfg.SiteName(*c.Arrival.OriginSite))
	}
	if len(parts) == 0 {
		return "", nil
	}
	// The record-only note rides INSIDE the parenthesis (L09 T-165f): the
	// review-lane rows append their own verb straight after this string, and
	// a trailing clause here read as "…did not re-run the matching matched
	// more than one open trip…" on a live board. ⟦was: `out := " (claimed: " +
	// … + ")"; if c.Corrected { out += " — the claim was corrected; the
	// correction is record-only and did not re-run the matching" }`⟧
	out := " (claimed: " + strings.Join(parts, ", ")
	if c.Corrected {
		out += "; this claim was corrected — record-only, the matching was not re-run"
	}
	return out + ")", nil
}

// tripChainView renders one trip's record chain for the audit view.
func (s *Server) tripChainView(tripID string, personName map[string]string) ([]tripChainEntryView, error) {
	entries, err := domain.TripAmendmentChain(s.Store, tripID)
	if err != nil {
		return nil, err
	}
	var out []tripChainEntryView
	for _, e := range entries {
		view := tripChainEntryView{Post: s.supersededPostName(e.SiteID), Dispatch: e.Dispatch}
		for _, f := range e.Facts {
			fv := tripChainFactView{Label: s.tripFactLabel(f.Fact), Value: f.Asserted}
			if f.PersonID != "" {
				if n := personName[f.PersonID]; n != "" {
					fv.Value = n
				} else if n := s.personDisplayName(f.PersonID); n != "" {
					fv.Value = n
				}
				if id, ok := domain.OccupantFactPersonID(f.Fact); ok {
					fv.Label = "aboard"
					if f.Asserted == "not aboard" {
						// ⟦D-80⟧ ⟦was: "never aboard"⟧ — the chain reads a past
						// removal as what the record asserted, not as the
						// concept D-80 retired. History carrying it still renders.
						fv.Label = "removed from list"
					}
					_ = id
				}
			}
			view.Facts = append(view.Facts, fv)
		}
		out = append(out, view)
	}
	return out, nil
}

// sortedPersons is the roster in NAME order with a name lookup alongside.
// domain.Persons returns rows in person_id order, and person ids are
// crypto/rand hex — measured during the bulk-movement scoping as effectively
// RANDOM (0 of 200 sampled positions matched name order). Sorting is done at
// the display layer deliberately: domain.Persons is a shared read and this
// codebase keeps ordering changes away from anything a fold can observe.
func sortedPersons(st *store.Store) ([]domain.Person, map[string]string, error) {
	persons, err := domain.Persons(st)
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(persons, func(i, j int) bool { return persons[i].Name < persons[j].Name })
	byID := make(map[string]string, len(persons))
	for _, p := range persons {
		byID[p.PersonID] = p.Name
	}
	return persons, byID, nil
}

func (s *Server) handleTripsPage(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	data := tripsData{SiteID: s.Cfg.SiteID, Cfg: s.Cfg}
	data.TimeWarnings = timeWarningsFrom(r) // TP §4.6 warn-accepts, post-redirect
	data.Post = s.Cfg.PostName()            // D-5
	data.Sync = s.buildSyncSection()        // SYNC-HONESTY: the link's state, on the console that reads the other post's records
	data.Destinations = s.peerSiteIDs()
	data.DispatchDestinations = s.Cfg.DestinationList()
	data.RoadWorkChoice = roadWorkChoiceFor(s.Cfg) // James, 2026-10-03

	// The deadline prefill: one configured number per controlled
	// destination. ⟦was: the weather-phase framing, and the prefill read at
	// the current phase — weather phases removed, James 2026-10-04⟧
	data.DispatchPrefill = map[string]int64{}
	for _, d := range data.DispatchDestinations {
		if !d.Controlled {
			continue // uncontrolled runs carry no deadline
		}
		if m, ok := s.Cfg.TravelMinutesTo(d.ID); ok {
			data.DispatchPrefill[d.ID] = m
		}
	}

	// C7: one scan feeds both the banner and the board's countdown /
	// staleness columns. alertOf is nil exactly when Alerts is unwired
	// (web-layer-only tests) — the C6 render-time stand-in then remains.
	var alertOf map[string]deadline.Alert
	if s.Alerts != nil {
		board, err := s.Alerts.Evaluate()
		if err != nil {
			log.Printf("trips: deadline scan: %v", err)
			data.BannerErr = refusalText(err)
		} else {
			// This handler reuses its own scan rather than calling
			// s.banner(), so it must apply the same worst-first ordering
			// s.banner() does — the sticky strip renders Banner[0] and
			// nothing else, so an unsorted slice here would show whichever
			// alert the scan emitted first on the ONE page where the
			// operator is most likely to be looking. (Caught by
			// TestTheStickyAlertStripStaysBoundedHoweverManyAlertsAreOutstanding,
			// which sweeps all six pages that mount the surface.)
			data.Banner = board.Banner()
			sortAlertsWorstFirst(data.Banner)
			alertOf = make(map[string]deadline.Alert, len(board.Alerts))
			for _, a := range board.Alerts {
				alertOf[a.TripID] = a
			}
		}
	}

	exps, err := domain.OpenExpectations(s.Store, s.Cfg.SiteID)
	if err != nil {
		s.tripsFail(w, "open expectations", err)
		return
	}
	for _, e := range exps {
		data.Board = append(data.Board, s.boardRowFor(e, alertOf, now, false))
	}
	sortBoard(data.Board)
	// James, 2026-10-03 (NEW-a): the OUTBOUND board — the controlled runs
	// this post sent out, off the SAME scan (alertOf holds this post's own
	// outbound verdicts: deadline/scan.go reads domain.OpenOutbound for its
	// own site), through the SAME row builder, so the two boards cannot
	// disagree about what a verdict looks like.
	outs, err := domain.OpenOutbound(s.Store, s.Cfg.SiteID)
	if err != nil {
		s.tripsFail(w, "open outbound", err)
		return
	}
	for _, e := range outs {
		data.Outbound = append(data.Outbound, s.boardRowFor(e, alertOf, now, true))
	}
	sortBoard(data.Outbound)
	data.ShowOutbound = len(data.Outbound) > 0 || len(s.Cfg.ControlledDestinationIDs()) > 0
	// James, 2026-10-03: the working-on-the-road list, off the SAME scan the
	// board above read (alertOf carries this post's own road-work verdicts;
	// a nil map when the scanner is unwired reads every own row as NO TIMER).
	if data.RoadWork, err = s.roadWorkRows(alertOf); err != nil {
		s.tripsFail(w, "road work", err)
		return
	}

	persons, personName, err := sortedPersons(s.Store)
	if err != nil {
		s.tripsFail(w, "persons", err)
		return
	}
	data.RosterCount = len(persons)
	// ⟦H-8⟧ the held-out notice, off this same roster read (see tripsData.NotEntered).
	data.NotEntered = notEnteredNoticesFrom(r, personName, s.Cfg.DisplayName)

	// Returns from the pads: the open uncontrolled runs authored here.
	data.Returns, err = s.buildReturnRows(personName)
	if err != nil {
		s.tripsFail(w, "trips", err)
		return
	}

	data.Review, err = s.buildReviewLane()
	if err != nil {
		s.tripsFail(w, "review lane", err)
		return
	}

	// --- the three person pickers (U2). Read picker.go's file comment for
	// what they replaced and why. The safe default is DERIVED PER ACTION
	// below, never inherited from the widget next to it. ---

	// Indexed open-set read (F30), the same one the gate page uses: this
	// site's OPEN visits only, at headcount cost rather than season cost.
	// The rows come from buildOnSiteRows (gate.go) — ONE definition of "who
	// is on site here", shared by the gate page and this one, carrying the
	// history guarantee its signature enforces.
	visits, err := domain.OpenVisitsAt(s.Store, s.Cfg.SiteID)
	if err != nil {
		s.tripsFail(w, "on-site visits", err)
		return
	}
	onSite := buildOnSiteRows(visits, s.Cfg.SiteID, personName)
	data.OnSiteCount = len(onSite)
	// One unlabelled band: this list IS the scope, so there is nothing to
	// order within it.
	onSitePicked := []pickerSection{section("", "", false, onSiteGroups(onSite, pickByPerson))}

	// DISPATCH — driver. SAFE DEFAULT: nobody pre-selected, and the control
	// is `required`.
	//   Direction: a dispatch REMOVES people from this site's muster. A
	//   driver field that arrived pre-filled (which is exactly what the old
	//   `<select required>` did — a browser pre-selects the first option, so
	//   whoever sorted first was the default driver) attributes a departure
	//   to somebody who may not have been in the vehicle. Radios with
	//   nothing chosen cannot do that: the form is refused until a human
	//   picks, and a refused form authors nothing, which leaves everybody on
	//   the muster. Over-reporting the muster wastes search time; a wrongly
	//   attributed departure hides a live person.
	data.DriverPicker = personPicker{
		FieldName: "driver_person_id", Mode: "radio", IDPrefix: "driver-picker", Required: true,
		Verb:        "record as the driver of",
		SafeDefault: "not the driver. Nobody is pre-selected — the dispatch is refused until you pick one, because a pre-filled driver would attribute a departure to whoever happened to sort first.",
		// Two boxes, one name — see gate.go's check-out picker for the
		// defect. A radio cannot be "both", so the misreading here is
		// picking either on a guess; the safe error is still the name left
		// on the muster.
		SameName: "These records share one name, and each shows the tail of its person id so they can be told apart. Pick only the one who is actually driving. If you cannot tell which is which, pick neither and hold the dispatch until you can: a name left on the muster is the safe error, and a departure recorded against the wrong record hides a person who is still here.",
		Empty:    "Nobody is recorded on site here, so there is nobody to dispatch. Check the crew in at the gate first — the gate page adds people who are not on the roster too.",
		Sections: onSitePicked,
	}

	// DISPATCH — other occupants. SAFE DEFAULT: unticked, i.e. NOT on the
	// vehicle.
	//   Direction: this action REMOVES people from the muster. A missed tick
	//   leaves that person recorded as still on site at the origin — the
	//   muster OVER-reports, which wastes search time but never hides a live
	//   person. The opposite default (pre-ticked = everybody leaves) would
	//   let one hurried submit clear a whole shift off the muster. Do NOT
	//   copy the inbound board's default here: that one adds people.
	data.OccupantPicker = personPicker{
		FieldName: "occupant_person_ids", Mode: "checkbox", IDPrefix: "occupant-picker",
		Verb:        "check out of this site with this vehicle",
		SafeDefault: "not on this vehicle — they stay recorded on site here. A missed tick over-reports the muster, which is the safe direction; a tick you did not mean removes a person who is still standing here.",
		// Two boxes, one name — the same rows as check-out, the same
		// direction (this REMOVES people from the muster here), so the
		// same advice.
		SameName: "These records share one name, and each box shows the tail of its person id so they can be told apart. Tick only the one who is actually on this vehicle — never both. If you cannot tell which is which, tick neither: a name left on the muster is the safe error, and a name wrongly removed is a person nobody goes looking for.",
		Empty:    "Nobody is recorded on site here. Check people in at the gate first.",
		Sections: onSitePicked,
	}

	// UNSOLICITED ARRIVAL — people aboard. SAFE DEFAULT: unticked, and
	// there is no defensible "all ticked" alternative.
	//   Direction: this action ADDS people to the muster, so the dangerous
	//   failure is OMISSION — a person who is physically here gets no visit
	//   row and the muster leaves them out entirely. The inbound board can
	//   default to "everyone in" because it has a MANIFEST: the trip already
	//   named exactly who is aboard. An unsolicited arrival has no manifest
	//   — that is what makes it unsolicited — so the candidate list is the
	//   roster, and pre-ticking the roster would check in the whole season.
	//   The dangerous direction is defended a different way instead: the
	//   confirm names every person about to be checked in and states the
	//   count, so "I meant 26 and it says 25" is visible before the POST,
	//   and domain.RecordUnsolicitedArrival refuses an empty selection
	//   outright rather than authoring an arrival with nobody in it.
	//   The list is NOT filtered to a guess at who might be aboard: anyone
	//   on the roster can walk in off any vehicle, and a person missing from
	//   an arrival picker is exactly the omission this action must not make.
	companyName, tradeName, err := refDataNames(s)
	if err != nil {
		s.tripsFail(w, "companies/trades", err)
		return
	}
	arrivalSections, err := s.arrivalSections(persons, personName, companyName, tradeName)
	if err != nil {
		s.tripsFail(w, "arrival candidates", err)
		return
	}
	data.ArrivalPicker = personPicker{
		FieldName: "person_ids", Mode: "checkbox", IDPrefix: "arrival-picker",
		Verb:        "check in at this site",
		SafeDefault: "not aboard, so NOT checked in. This action adds people to the muster, so the dangerous mistake is leaving somebody out — check the count in the confirmation against the people in front of you before you accept it.",
		// Two boxes, one name — and this is the ADD direction, so the
		// check-out advice ("tick neither") is exactly wrong here: it is
		// the omission that leaves a person who is here on no list. Do NOT
		// copy the remove-direction sentence because it is the same widget.
		SameName: "These roster records share one name, and each box shows the tail of its person id so they can be told apart. Tick the one you mean. If you cannot tell which is which, still tick one of them: this action adds people to the muster, the muster carries this name either way, and leaving the person off it is the mistake that hides somebody who is here.",
		Empty:    "The roster is empty at this site. Use the gate check-in, which can add people who are not on the roster.",
		Sections: arrivalSections,
	}

	s.renderPage(w, "trips", "trips.html", data)
}

// boardRowFor renders ONE open expectation as a board row carrying the C7
// scan's verdict — the inbound board's row and (NEW-a, James 2026-10-03) the
// outbound board's row are the same translation of deadline.Status, so the
// two lists can never disagree about what a verdict looks like. `outbound`
// only changes the label prefix and the destination carried on the row.
func (s *Server) boardRowFor(e domain.Expectation, alertOf map[string]deadline.Alert, now time.Time, outbound bool) boardRow {
	row := boardRow{
		// ⟦James, 2026-10-03 — DR-W7-18 / W7-36(a)⟧ the open board composes
		// the truck LIVE, as the alert above it and the origin's sheet do.
		// ⟦was: "Vehicle: tripVehicleDisplay(e.Trip.VehicleIsUnidentified(), e.Trip.VehicleEntered)"⟧
		TripID: e.Trip.TripID, Vehicle: e.Vehicle,
		Origin: e.Trip.OriginSite, Driver: e.DriverName, Occupants: e.Occupants,
		OccupantRows: e.OccupantList,
		Deadline:     "—", Countdown: "—", Staleness: "—",
		Urgency: "none", UrgencyLabel: "NO TIMER",
		Outbound: outbound,
	}
	if outbound && e.Trip.DestinationSite != nil {
		row.Destination = *e.Trip.DestinationSite
	}
	sortOccupantRefs(row.OccupantRows)
	if e.Trip.DeadlineMinutes != nil {
		row.Deadline = fmt.Sprintf("%d min", *e.Trip.DeadlineMinutes)
	}
	if a, ok := alertOf[e.Trip.TripID]; ok {
		// C7 authoritative: sourced from the persisted DB scan (§9.4),
		// not from anything this process or tab remembers.
		switch a.Status {
		case deadline.StatusCountdown:
			row.Countdown = fmt.Sprintf("%s remaining", a.RemainingText())
			row.CountdownValue, row.CountdownNote = a.RemainingText(), "remaining"
			row.Urgency, row.UrgencyLabel, row.remaining = "due", "DUE IN", a.Remaining
		case deadline.StatusOverdueLive:
			row.Countdown = fmt.Sprintf("OVERDUE by %s", a.OverdueByText())
			row.CountdownValue, row.CountdownNote = a.OverdueByText(), "OVERDUE by"
			row.Urgency, row.UrgencyLabel = "overdue", "OVERDUE"
			// T-005 (L07): the latest truck first within the overdue band —
			// the negated figure sorts most-overdue first on the same key
			// the "due" band uses. ⟦was: remaining left at zero, so the band
			// fell through to trip-id order⟧
			row.remaining = -a.OverdueBy
		case deadline.StatusOverdueUnresolved:
			row.Countdown = "overdue-unresolved — verify by radio"
			// No numeral exists for this state: the deadline passed
			// while the link was down, so there IS no honest figure.
			// An em-dash says that; a number would be a fabrication.
			row.CountdownValue, row.CountdownNote = "—", "overdue-unresolved — verify by radio"
			// Q-FE2 (James, 2026-10-04): the label is the scan's own —
			// a row this post cannot count reads VERIFY BY RADIO — no
			// receipt timing at this post, never OVERDUE. ⟦was: always
			// "OVERDUE — UNRESOLVED"⟧
			row.Urgency, row.UrgencyLabel = "unresolved", a.ReviewLabel()
			if a.DeadlineUnknown && !a.ClockChanged {
				row.Countdown = "no receipt timing at this post — verify by radio"
				row.CountdownNote = row.Countdown
			}
			if a.ClockChanged { // R9, James 2026-10-04: never a fabricated OVERDUE
				row.Countdown, row.CountdownNote = deadline.ClockChangedNote, deadline.ClockChangedNote
			}
			if due := s.alertDueText(a); due != "" {
				row.Countdown += "; " + due
				row.CountdownNote += "; " + due
			}
		}
		// ⟦W9c, item 17 — F6 §5 R1⟧ The suffix NAMES the acking post.
		// It used to read a bare " (acknowledged)" with no attribution
		// at all, which is visible-failure attack 1's imperceptible
		// absence: a post that un-acked and then watched the row go
		// quiet again — because the peer's independent re-ack crossed
		// in flight — could not tell that from "my un-ack did nothing".
		// The condition is AckFace's own emptiness rather than a.Acked,
		// so there is no arm that could render " ()": the scan sets
		// AckFace exactly when the row is acked (deadline/scan.go's
		// stampAckFaces).
		if a.AckFace != "" {
			row.Countdown += " (" + a.AckFace + ")"
			row.CountdownNote += " (" + a.AckFace + ")"
		}
		row.Repudiation = a.RepudiationFace
		row.Unackable = a.Unackable() // F4: only where the standing ack silences this row ⟦was: a.HasStandingAck⟧
		row.VerifyOnlyAck = a.VerifyOnlyAckFace
		if a.StalenessFace != "" {
			row.Staleness = a.StalenessFace
		}
	}
	if _, scanned := alertOf[e.Trip.TripID]; !scanned && s.Alerts == nil && !outbound {
		// C6 render-time stand-in, kept only for web-layer tests that
		// run without the scanner. Never reached in the wired binary. An
		// outbound row gets no stand-in: there is nothing older than the
		// scan that ever judged one, and "NO TIMER" is the honest reading.
		seen := s.firstSeen(e.Trip.TripID, now)
		age := now.Sub(seen).Round(time.Second)
		row.Staleness = fmt.Sprintf("held %s", age)
		if e.Trip.DeadlineMinutes != nil {
			x := time.Duration(*e.Trip.DeadlineMinutes) * time.Minute
			if remaining := x - age; remaining > 0 {
				row.Countdown = fmt.Sprintf("%s remaining", remaining.Round(time.Second))
				row.CountdownValue, row.CountdownNote = remaining.Round(time.Second).String(), "remaining"
				row.Urgency, row.UrgencyLabel, row.remaining = "due", "DUE IN", remaining
			} else {
				row.Countdown = fmt.Sprintf("overdue %s (verify by radio; C7 owns alerting)", (-remaining).Round(time.Second))
				row.CountdownValue = (-remaining).Round(time.Second).String()
				row.CountdownNote = "overdue (verify by radio; C7 owns alerting)"
				row.Urgency, row.UrgencyLabel = "overdue", "OVERDUE"
			}
		}
	}
	if outbound {
		// The word first, so a row from either board is unambiguous on
		// its own: "OUTBOUND — OVERDUE" is a truck this post sent out.
		row.UrgencyLabel = "OUTBOUND — " + row.UrgencyLabel
	}
	return row
}

// sortBoard: the row that needs a human first goes first; within a band by
// `remaining` (soonest due first; T-005: latest overdue first); ties break
// on the trip id so the board never reshuffles between two identical
// renders. One comparator for both boards.
func sortBoard(rows []boardRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		ri, rj := urgencyRank(rows[i].Urgency), urgencyRank(rows[j].Urgency)
		if ri != rj {
			return ri < rj
		}
		if rows[i].remaining != rows[j].remaining {
			return rows[i].remaining < rows[j].remaining
		}
		return rows[i].TripID < rows[j].TripID
	})
}

// handleTripsDetail is the reference/audit half of the old /trips: every
// trip this box knows about (with the cancel action), the out-at-pad panel,
// and the visits table. Same rows, same routes, same fields — a page an
// operator visits deliberately instead of scrolling past with a driver
// waiting.
func (s *Server) handleTripsDetail(w http.ResponseWriter, r *http.Request) {
	data := tripsDetailData{SiteID: s.Cfg.SiteID, Cfg: s.Cfg}
	data.Post = s.Cfg.PostName()             // D-5
	data.Banner, data.BannerErr = s.banner() // C7: the banner rides every screen (§9.4)

	_, personName, err := sortedPersons(s.Store)
	if err != nil {
		s.tripsFail(w, "persons", err)
		return
	}

	// FIX17d: the page lists the last seven days unless asked for more.
	rng := s.parseTripsRange(r)
	csvOut := strings.HasSuffix(r.URL.Path, ".csv")
	if csvOut && len(rng.Problems) > 0 {
		csvRefused(w, "the trips download", "", false, rng.Problems)
		return
	}
	visitsCSV := csvOut && rng.VisitsOnly
	var rows []tripListRow
	if !visitsCSV {
		keep, err := rng.keep(s, csvOut)
		if err != nil {
			s.tripsFail(w, "trip times", err)
			return
		}
		rows, err = s.buildTripRowsScoped(personName, !csvOut, false, keep)
		if err != nil {
			s.tripsFail(w, "trips", err)
			return
		}
	}
	data.Trips = rows
	vkeep, arrived, err := rng.keepVisits(s)
	if err != nil {
		s.tripsFail(w, "visit times", err)
		return
	}
	data.Range = rng
	if csvOut && !visitsCSV {
		data.Range.finish(s, len(rows))
		s.writeCSVDownload(w, "trips", tripsCSVHeader, s.tripsCSVRows(rows, rng.when))
		return
	}

	// Who the record says is still out at the pads (2026-08-20). No timer
	// watches these, by design — the panel is the answerability, not an alert.
	outAt, err := domain.OutAtUncontrolled(s.Store)
	if err != nil {
		s.tripsFail(w, "out at uncontrolled", err)
		return
	}
	data.OutAt = outAt

	visits, err := domain.Visits(s.Store)
	if err != nil {
		s.tripsFail(w, "visits", err)
		return
	}
	var visitCSV [][]string
	for _, v := range visits {
		if vkeep != nil && !vkeep(v) {
			continue
		}
		row := visitRow{Person: personName[v.PersonID], Site: v.SiteID, Open: v.IsOpen == 1}
		if row.Person == "" {
			row.Person = v.PersonID
		}
		if v.CompanySnapshotName != nil {
			row.Company = *v.CompanySnapshotName
		}
		if v.TradeSnapshotName != nil {
			row.Trade = *v.TradeSnapshotName
		}
		data.Visits = append(data.Visits, row)
		if visitsCSV {
			in := ""
			if ms, ok := arrived[v.VisitID]; ok {
				in = time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05Z")
			}
			visitCSV = append(visitCSV, []string{in, row.Person, s.Cfg.SiteName(row.Site), row.Company, row.Trade, visitStatusText(row.Open)})
		}
	}
	data.Range.VisitCount = len(data.Visits)
	data.Range.finish(s, len(data.Trips))
	if visitsCSV {
		s.writeCSVDownload(w, "visits", visitsCSVHeader, visitCSV)
		return
	}

	s.renderPage(w, "trips detail", "trips_detail.html", data)
}

func (s *Server) tripsFail(w http.ResponseWriter, what string, err error) {
	log.Printf("trips: %s: %v", what, err)
	http.Error(w, dbFailBody(err), http.StatusInternalServerError)
}

// refuse surfaces a domain refusal to the operator without authoring
// anything. The sentence is written to be read; the "domain: dispatch:"
// chain in front of it is not, so it goes through the one mapper
// (refusal.go), and Root's shared refusal page gives it the full page.
func refuse(w http.ResponseWriter, err error) {
	http.Error(w, refusalText(err), http.StatusConflict)
}

// applyDispatchDestination resolves the destination and deadline fields of
// a dispatch body onto a DispatchInput. It is SHARED by the two
// request-reachable Dispatch callers — the operator console's dispatch
// buttons (POST /trips/dispatch) and the gate's outbound vehicle form
// (POST /gate/checkout/vehicle) — so the two cannot drift apart on which
// destinations exist, which of them is controlled, or what a deadline is
// allowed to be. Two copies of this resolution would be two chances for
// one surface to author a trip the other would have refused.
//
// It returns the HTTP status a refusal should carry, so both callers report
// a malformed body as 400 and an unknown destination as 409 exactly as the
// console always has.
//
// SAFE DEFAULT — the destination control, and it is the same derivation in
// both directions because a dispatch is a REMOVE-direction action on both
// surfaces: NOTHING IS PRE-CHOSEN, and naming a destination is MANDATORY.
//
// Nothing pre-chosen, because defaulting to a real destination would create
// an inbound expectation at a peer, and (for a controlled one) a DEADLINE —
// a promise a timer will raise an alarm about, that nobody chose. The
// failure is a false OVERDUE, which is the alarm-fatigue direction on a
// warning that must stay believed.
//
// Mandatory, because of D-4 (JAMES-DECISIONS.md). This function used to
// treat an empty destination as "the degenerate local trip" and author it,
// which is what the console's "Local check-out only (no destination)"
// button did. That trip — open, no destination, no deadline, no arrival —
// is byte-identical to a check-in that crashed between its two events, so
// every routine departure landed on /gate as an incomplete check-in with a
// Resume button that would put the departed crew back on the muster, and
// made the printed muster read MAY BE INCOMPLETE. The refusal is repeated
// here, in front of the operator, so the console reports it in its own
// words with the request body intact; domain.Dispatch refuses the same
// shape unconditionally, which is what closes the raw POST.
func applyDispatchDestination(cfg *config.Config, destID, destSite, deadline string, in *domain.DispatchInput) (int, error) {
	// Two ways to name a destination:
	//   - destination_id: the per-destination dispatch buttons (one per
	//     configured destination). The config entry decides which field the
	//     trip carries — controlled → DestinationSite (deadline required,
	//     timer runs), uncontrolled → DestinationUncontrolled (deadline
	//     refused). Empty is REFUSED (D-4) — there is no destination-less
	//     departure, and no button emits one.
	//   - destination_site: the pre-destinations field, kept for
	//     compatibility; always the controlled path, and since D-77
	//     resolved against the CONTROLLED destination list (never the
	//     peer list), exactly as destination_id is.
	// A raw POST carrying both is malformed — no button produces it, and
	// guessing which one the sender meant is exactly what this system never
	// does.
	switch {
	case destID != "" && destSite != "":
		return http.StatusBadRequest, fmt.Errorf("destination_id and destination_site are mutually exclusive")
	case destID == config.RoadWorkChoice:
		// James, 2026-10-03: WORKING ON THE ROAD rides the same destination
		// control as a real destination — one button set, one <select>, one
		// resolver — and resolves to the road-work KIND (domain/road_work.go)
		// with THIS post's configured working limit as its deadline. The
		// limit is config, never typed: a deadline in the body is accepted
		// only when it already equals the limit (a form that echoed the
		// prefill back), and refused otherwise — silently discarding a typed
		// number would be the discarded-box defect under another name, and
		// honouring it would make the limit per-vehicle, which it is not.
		if !cfg.RoadWorkEnabled() {
			return http.StatusConflict, fmt.Errorf("this post does not record equipment working on the road — record a destination instead, or ask whoever sets up this post to turn road work on") // fix round 3 (F13, crafted-POST copy): ⟦was: "… (its config has no [road_work] block) … add the block to this site's config"⟧
		}
		limit := cfg.RoadWorkLimitMinutes()
		if v := strings.TrimSpace(deadline); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return http.StatusBadRequest, fmt.Errorf("the travel allowance must be a whole number of minutes") // fix round 3 (FINDINGS-R5 F13, from C): ⟦was: "deadline_minutes must be …" — a config key⟧
			}
			if n != limit {
				return http.StatusConflict, fmt.Errorf("%s has a fixed working limit of %s (%d min), set in this post's config and not typed per vehicle — leave the deadline blank; the vehicle is overdue once it has been out longer than that", cfg.RoadWorkLabel(), formatLimit(limit), limit)
			}
		}
		in.RoadWork = true
		in.DeadlineMinutes = &limit
		return http.StatusOK, nil
	case destID == "" && destSite == "":
		// D-4: no destination-less departure. See the block comment above.
		return http.StatusConflict, fmt.Errorf("a departing vehicle must record where it is going — nothing has been recorded. Nobody leaves this site except in a vehicle, to somewhere; a trip with no destination is indistinguishable from a check-in that crashed half-way, and would show on the gate as an incomplete check-in offering to put these people back on the muster while the printed muster read MAY BE INCOMPLETE. To correct a check-in that should not have been recorded, use the gate's check-in correction list instead")
	case destID != "":
		resolved := false
		for _, d := range cfg.DestinationList() {
			if d.ID != destID {
				continue
			}
			if d.Controlled {
				in.DestinationSite = &d.ID
			} else {
				in.DestinationUncontrolled = &d.ID
			}
			resolved = true
			break
		}
		if !resolved {
			// The F4 shape one layer up: a destination no button offers.
			return http.StatusConflict, fmt.Errorf("that destination is not configured at this site") // fix round 3 (F13, from C): ⟦was: "destination %q is not configured …" quoting the raw id⟧
		}
	case destSite != "":
		// The legacy field names a site directly, so it is the one way a
		// raw POST can reach a destination no button offers. Resolve it
		// against the CONTROLLED destination list — the list the buttons
		// render from — never the peer list: since D-77 a post with more
		// than one peer replicates with sites it has no road to, and a
		// dispatch toward one of those arms an in-transit deadline nobody
		// can honour and queues a vehicle on an inbound list nobody is
		// pairing with (R3-LINKAGE.md, 2026-09-10: accepted 303 and queued
		// at the non-destination peer before this check existed).
		// domain.Dispatch repeats the refusal with ControlledDestinationIDs
		// as its F4 set.
		resolved := false
		for _, d := range cfg.DestinationList() {
			if d.Controlled && d.ID == destSite {
				resolved = true
				break
			}
		}
		if !resolved {
			return http.StatusConflict, fmt.Errorf("that site is not a destination this post dispatches to — choose one of the destinations on the form") // fix round 3 (F13, crafted-POST copy): ⟦was: "destination %q is not a controlled destination … [[destinations]] says where …" — a raw id and a config key⟧
		}
		in.DestinationSite = &destSite
	}
	if v := strings.TrimSpace(deadline); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return http.StatusBadRequest, fmt.Errorf("the travel allowance must be a whole number of minutes") // fix round 3 (FINDINGS-R5 F13, from C): ⟦was: "deadline_minutes must be …" — a config key⟧
		}
		in.DeadlineMinutes = &n
	}
	return http.StatusOK, nil
}

func (s *Server) handleDispatch(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	// ⟦W7-4(d)(i), 2026-09-03⟧ ⟦was: "// NO TrimSpace on the vehicle: the
	// entered text is stored verbatim (M8); comparison alone normalises." /
	// "VehicleEntered: r.FormValue(\"vehicle\"),"⟧ — see gate.go's identical
	// note: M8's verbatim storage moved to the vehicle RECORD with the typing.
	// ⟦LOCK — §93, 2026-09-03⟧ ⟦was: the vehicle co-mint stood HERE, above every validation:
	// "vehicleID, vehErr, vehStatus := s.vehicleIDForTypedNumber(r.FormValue(\"vehicle\"))" with its
	// refusal, and "VehicleID: vehicleID," in the literal below⟧
	//
	// ⚠⚠ IT AUTHORED BEFORE IT VALIDATED. `vehicleIDForTypedNumber` CO-MINTS a `vehicle_created`
	// event, so a POST refused further down had already written one — and the operator was told
	// nothing was recorded. [MEASURED, before the move] THREE of four refusal modes wrote a record:
	// no time signal (400), no destination / D-4 (409), and driver-not-on-the-roster (409). Only
	// the placeholder-vehicle case wrote nothing, because it is refused inside the co-mint itself.
	// Every malformed OR mistaken dispatch left a real vehicle on the muster's vehicle list.
	//
	// ⭐ THE MOVE IS TO THE ORDER THE SIBLING DOORS ALREADY USE, not to a new invention:
	// `handleUnsolicitedArrival` captures the time signal and co-mints nine lines later; ALL THREE
	// of gate.go's co-mint sites (`handleGateCheckIn`, `handleGateVehicleCheckOut`,
	// `legacyGateCheckIn`) call `captureTime` first. This door was the only one of the four that
	// authored first. ⟦LOCK — §93 verification, 2026-09-03⟧ ⟦was: "captures the time signal at
	// :1153 and co-mints at :1162"⟧ — [MEASURED] those line numbers were already 21 lines stale
	// when written (the real pair is :1174/:1183), so the citation is given as a RELATION between
	// the two calls, which cannot rot, rather than as absolute lines, which had rotted inside one
	// commit. §93's own closing class: a stale signpost misdirects exactly when someone is lost.
	//
	// ⚠ THE CO-MINT NOW SITS BELOW `captureTimeOrRefuse`, and it must stay there. The guard is
	// `TestARefusedPostAuthorsNothing` (trips_test.go), which asserts across four refusal
	// modes at three different depths precisely so that moving this call back up reds immediately.
	// ⟦LOCK — §132, 2026-09-03⟧ re-pointed: the guard was WIDENED at §104 and lost "Dispatch" from
	// its name, because the guarantee is not one door's. ⭐ The paragraph directly above is about a
	// citation that had already rotted inside one commit — and it was followed by another. A stale
	// signpost misdirects exactly when someone is lost, and that is true of the sentence that says so.
	in := domain.DispatchInput{
		DriverPersonID:    r.FormValue("driver_person_id"),
		OccupantPersonIDs: r.Form["occupant_person_ids"],
		// F4 + F11: this is the one Dispatch caller reachable by a raw,
		// hand-crafted POST — the <select> elements this form's own inputs
		// come from only ever submit valid roster ids and real peer sites,
		// but nothing stops a direct POST from carrying anything else.
		ValidateRoster: true,
		// F4's membership set is the CONTROLLED DESTINATION list, not the
		// peer list. The two coincided at two posts; since D-77 a post with
		// more than one peer replicates with sites it cannot dispatch to,
		// and handing Dispatch the peer list let the legacy destination_site
		// field author a trip toward one of them (R3-LINKAGE.md, 2026-09-10).
		// Non-nil even when empty, so membership is always enforced here.
		KnownDestinationSites: s.Cfg.ControlledDestinationIDs(),
		// Uncontrolled analogue — the same always-non-nil posture.
		KnownUncontrolledDestinations: s.Cfg.UncontrolledDestinationIDs(),
		// ⟦was: PhaseGate — the weather-phase dispatch gate (§4) with its
		// override reason; weather phases removed, James 2026-10-04⟧
	}
	// Two ways to name a destination:
	//   - destination_id: the per-destination dispatch buttons (one per
	//     configured destination). The config entry decides which field the
	//     trip carries — controlled → DestinationSite (deadline required,
	//     timer runs), uncontrolled → DestinationUncontrolled (deadline
	//     refused). Empty is REFUSED (D-4) — there is no destination-less
	//     departure, and no button emits one.
	//   - destination_site: the pre-destinations field, kept for
	//     compatibility; always the controlled path, and since D-77
	//     resolved against the CONTROLLED destination list (never the
	//     peer list), exactly as destination_id is.
	// A raw POST carrying both is malformed — no button produces it, and
	// guessing which one the sender meant is exactly what this system never
	// does.
	if status, err := applyDispatchDestination(s.Cfg, r.FormValue("destination_id"), r.FormValue("destination_site"), r.FormValue("deadline_minutes"), &in); err != nil {
		http.Error(w, refusalText(err), status)
		return
	}
	in.RecordedBy = s.postAttribution() // D-5: this box's post, never a person
	// TP §4.5/§4.6: the explicit time signal and its bounds (TP §4.8 row 5
	// — entered value → trip_dispatched).
	tc, ok := s.captureTimeOrRefuse(w, r)
	if !ok {
		return
	}
	// ⟦LOCK — §93, 2026-09-03⟧ THE CO-MINT, MOVED HERE FROM ABOVE — see the note at the top of this
	// handler. Everything that can refuse this request without authoring has now run.
	// ⟦TRIAL — §97⟧ the co-mint is handed to Dispatch, which runs it after its own validations.
	//
	// ⟦SchemaVersion 13, R-17, 2026-09-11⟧ VEHICLE UNKNOWN, now on the
	// operator console too — the third of the choke point's callers,
	// alongside the gate's inbound and outbound forms. Ticked, the typed
	// box is ignored and no resolver runs; in.VehicleUnidentified is
	// enforced by dispatchTrip itself, the same as every other caller.
	var vehErr string
	var vehStatus int
	if r.FormValue("vehicle_unidentified") != "" {
		in.VehicleUnidentified = true
	} else {
		in.ResolveVehicle = func() (string, error) {
			id, e, code := s.vehicleIDForTypedNumber(s.Store, r.FormValue("vehicle"))
			if e != "" {
				vehErr, vehStatus = e, code
				return "", fmt.Errorf("vehicle: %s", e)
			}
			return id, nil
		}
	}
	if _, err := domain.Dispatch(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, in, tc.OccurredAtMS, tc.TimeSource); err != nil {
		if vehErr != "" {
			http.Error(w, vehErr, vehStatus)
			return
		}
		refuse(w, err)
		return
	}
	http.Redirect(w, r, timeWarnRedirect("/trips", tc), http.StatusSeeOther)
}

func (s *Server) handleExpectedArrival(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	tripID := r.FormValue("trip_id")
	if tripID == "" {
		http.Error(w, "trip_id is required", http.StatusBadRequest)
		return
	}
	recordedBy := s.postAttribution() // D-5: this box's post, never a person
	// The per-person disposition, in whichever of the two rendered forms
	// arrived — see arrivalDisposition (inbound.go) for the full safety
	// derivation and for why both shapes exist:
	//
	//	/gate   manifest_person_ids + arriving_person_ids — TICKED means
	//	        "this person is coming in" (James, 2026-08-21). Ticked by
	//	        default, server-rendered, so the default holds with
	//	        scripting off.
	//	/trips  stayed_person_ids — the console's older shape, where a ticked
	//	        box EXCLUDES ("stayed behind — do not check in").
	//
	// Both reduce to the same stayed-behind set and the same domain call,
	// and only one is ever rendered on a given page, so no operator sees
	// two meanings of a checkbox on one screen. Absent from both = everyone
	// in, exactly the pre-manifest behaviour.
	// TP §4.5/§4.6 (TP §4.8 row 6 — entered value → trip_arrival).
	tc, ok := s.captureTimeOrRefuse(w, r)
	if !ok {
		return
	}
	// D-98/D-91 (SCHEMA-11 step 5): this site's own config.HasOrientation()
	// — never a peer's, never the trip's origin (orientation.go's file
	// header: "the site is the envelope's, never the payload's") — is what
	// makes the choke point's split live. Omitting this argument (the
	// variadic's zero length) silently reads as false at every site,
	// orientation=true ones included.
	// ⟦gate-orientation-block, 2026-09-09⟧ ⟦was: "there is no other reachable
	// caller of this door (MEASURED: RecordExpectedArrival/Excluding's only
	// production call site)"⟧ — FALSE when written: domain.GateCheckIn and
	// domain.ResumeCheckIn (checkin.go) both call RecordExpectedArrival and
	// are reached from POST /gate/checkin and POST /gate/resume; for three
	// weeks they passed nothing, so the gate entered whom this door held out
	// (MEASURED at f7afc3a, reports/witness/ORIENTATION-ENFORCEMENT.md). All
	// four request-reachable callers now carry the flag: this door, the
	// unsolicited door below, and the two gate doors (web/gate.go).
	stayed := arrivalDisposition(r)
	arrival, err := domain.RecordExpectedArrivalExcluding(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, tripID,
		stayed, recordedBy, tc.OccurredAtMS, tc.TimeSource, s.Cfg.HasOrientation())
	if err != nil {
		refuse(w, err)
		return
	}
	// Back to whichever surface the arrival was recorded from. The gate
	// officer stays at the gate: bouncing them to the operator console
	// after every truck would put a different page in front of them than
	// the one the next truck needs, and with scripting off there is no way
	// back except the nav.
	target := notEnteredRedirect(timeWarnRedirect(arrivalReturnTo(r), tc), arrival.NotEntered)
	// ⟦R-28, team-lead, 2026-09-16⟧ the vehicle-mode page's one-click
	// result line ("TRK-14 arrived: 10 checked in") — only carried for an
	// arrival recorded from the gate's own inbound board (from=gate); the
	// operator console's board already shows its own result inline and
	// gate_vehicle.html is the only template that ever reads ArrivedNotice
	// (gate.html never renders the inbound board at all). checked is the
	// manifest minus whoever stayed behind minus whoever this call just
	// held out for orientation — never negative by construction (NotEntered
	// is always a subset of "arriving" = manifest-stayed), floored at 0
	// defensively rather than trusted blind.
	if r.FormValue("from") == "gate" {
		checked := len(r.Form["manifest_person_ids"]) - len(stayed) - len(arrival.NotEntered)
		if checked < 0 {
			checked = 0
		}
		// Arrival itself (above) carries no vehicle text — RecordExpected
		// ArrivalExcluding's own return literal never populates VehicleEntered
		// / VehicleUnidentified (domain/tripauthor.go); read the trip's own
		// frozen snapshot back, the same fields inboundVehicleRows already
		// reads to build the row this button was on. A read failure here
		// costs only the result line's vehicle text (falls back to "the
		// vehicle"), never the arrival itself, which already succeeded above.
		//
		// ⟦James, 2026-10-03⟧ composed LIVE, as the inbound row it was on now
		// is (LiveVehicles) — the result line names the truck the officer just
		// clicked, not the number it was dispatched under.
		// ⟦was: "TripByID … tripVehicleDisplay(trip.VehicleIsUnidentified(), trip.VehicleEntered)"⟧
		vehicleText := "the vehicle"
		if c, terr := domain.EffectiveTrip(s.Store, tripID); terr == nil && c != nil {
			if live, lerr := domain.NewLiveVehicles(s.Store); lerr == nil {
				if v, _, cerr := live.Compose(*c); cerr == nil {
					vehicleText = v
				}
			}
		}
		target = arrivedRedirect(target, vehicleText, checked)
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// handleReturnedToOrigin is the outbound board's "Arrived back here" door
// (James, 2026-10-03): the truck this post sent out turned back and is here
// again. The same per-person disposition and time capture as the arrival
// door above, and the same event underneath (domain.RecordReturnToOrigin —
// an ordinary explicit arrival on this post's stream); the domain refuses
// a trip this post did not send, or one that already arrived anywhere this
// post knows of.
func (s *Server) handleReturnedToOrigin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	tripID := r.FormValue("trip_id")
	if tripID == "" {
		http.Error(w, "trip_id is required", http.StatusBadRequest)
		return
	}
	tc, ok := s.captureTimeOrRefuse(w, r)
	if !ok {
		return
	}
	arrival, err := domain.RecordReturnToOrigin(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, tripID,
		arrivalDisposition(r), s.postAttribution(), tc.OccurredAtMS, tc.TimeSource, s.Cfg.HasOrientation())
	if err != nil {
		refuse(w, err)
		return
	}
	http.Redirect(w, r, notEnteredRedirect(timeWarnRedirect("/trips", tc), arrival.NotEntered), http.StatusSeeOther)
}

// arrivalReturnTo picks the post-arrival redirect from the form's own
// declared origin. It is a fixed two-way choice, never a URL taken from the
// request: an attacker-supplied `return_to` would be an open redirect, and
// a form field naming a path is exactly how that ships by accident.
func arrivalReturnTo(r *http.Request) string {
	// ⟦R-27, James, 2026-09-16⟧ the inbound board this form's "from=gate"
	// marker names moved, unchanged, to /gate/vehicle when /gate became
	// the individual page — see gate_vehicle.html's own file comment. The
	// individual page never renders this board at all, so there is no
	// "from=gate meaning the individual page" case to disambiguate.
	if r.FormValue("from") == "gate" {
		return "/gate/vehicle"
	}
	return "/trips"
}

func (s *Server) handleUnsolicitedArrival(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	recordedBy := s.postAttribution() // D-5: this box's post, never a person
	// TP §4.5/§4.6 (TP §4.8 row 7 — the arrival event carries the entered
	// value; fold-minted visit rows have no envelope of their own).
	tc, ok := s.captureTimeOrRefuse(w, r)
	if !ok {
		return
	}
	// ⟦W7-4(d)(i), 2026-09-03⟧ ⟦was: "r.FormValue(\"vehicle\"), // verbatim, as
	// entered (M8)"⟧ — the B-side of the two-key correlation is now the vehicle
	// RECORD's id, so an arrival that outran its trip correlates on the id the
	// moment the vehicle record has replicated, without waiting for the trip.
	// trip_arrival.vehicle_entered is derived from the record, inside the door.
	// ⟦TRIAL — §97/§107⟧ the co-mint is handed to the door, which runs it after its own validations.
	// ⟦was: "vehicleID, vehErr, vehStatus := s.vehicleIDForTypedNumber(r.FormValue(\"vehicle\"))" with
	// its refusal, and `vehicleID` passed positionally below⟧
	//
	// ⟦SchemaVersion 14, R-18, 2026-09-13⟧ VEHICLE UNKNOWN, now on this door
	// too — R-17's own wording ("there are no doors only vehicles") always
	// covered it; the build that shipped R-17 measured only the four doors
	// an assessment had found, and this one was never on that list. Ticked,
	// the typed box is ignored and no resolver runs — the identical
	// "no resolver at all, not merely an unused one" posture the operator
	// console's own Dispatch handler uses (above), so the co-mint cannot
	// fire even if VehicleUnidentified were somehow blended with a leftover
	// closure: there is no closure to blend with.
	var vehErr string
	var vehStatus int
	var resolvers []func() (string, error)
	vehicleUnidentified := r.FormValue("vehicle_unidentified") != ""
	if !vehicleUnidentified {
		resolvers = append(resolvers, func() (string, error) {
			id, e, code := s.vehicleIDForTypedNumber(s.Store, r.FormValue("vehicle"))
			if e != "" {
				vehErr, vehStatus = e, code
				return "", fmt.Errorf("vehicle: %s", e)
			}
			return id, nil
		})
	}
	// D-98/D-91 (SCHEMA-11 step 5): this door's own orientation gate, wired
	// through RecordUnsolicitedArrivalOriented — same rule as
	// handleExpectedArrival above, this site's own config.HasOrientation(),
	// never the origin's.
	// L05 2b (fix round 4): an origin that is not one of this post's peers
	// is refused, in /arrivals/amend's M-4 wording, before anything runs.
	// FIX5 (R7 L03-04): the SAME known list as /arrivals/amend — this
	// post's peers only. An arrival "from" this post itself was recorded
	// here (and replicated ×5) while the correction page refused that very
	// value as "not a post this post has an arrival from"; the form's
	// "Came from" choices never offer it, and a truck that left here and
	// came back is "Arrived back here" on its own run. No test relied on
	// the allowance (every door test passes without it). ⟦was:
	// append(s.peerSiteIDs(), s.Cfg.SiteID) — "this post itself stays a
	// known origin"⟧
	if err := domain.UnsolicitedOriginRefusal(r.FormValue("origin_site"), s.peerSiteIDs()); err != nil {
		refuse(w, err)
		return
	}
	arrival, err := domain.RecordUnsolicitedArrivalOriented(
		s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch,
		"",
		r.FormValue("origin_site"),
		r.Form["person_ids"],
		recordedBy,
		tc.OccurredAtMS, tc.TimeSource,
		s.Cfg.HasOrientation(),
		vehicleUnidentified,
		resolvers...,
	)
	if err != nil {
		if vehErr != "" {
			http.Error(w, vehErr, vehStatus)
			return
		}
		refuse(w, err)
		return
	}
	// ⟦H-8 / T-093, 2026-09-29⟧ ⟦was: "_, err := domain.RecordUnsolicited
	// ArrivalOriented(" … "http.Redirect(w, r, timeWarnRedirect("/trips",
	// tc), http.StatusSeeOther)"⟧ — this door discarded arrival.NotEntered
	// outright, so unlike the expected-arrival door above the held-out ids
	// never even reached the redirect: the officer who held a person out
	// through this form was told nothing by any page. Same carriage as the
	// three other doors (notentered.go); /trips renders it off the roster.
	http.Redirect(w, r, notEnteredRedirect(timeWarnRedirect("/trips", tc), arrival.NotEntered), http.StatusSeeOther)
}

// ⟦D-78, 2026-09-05⟧ handleCancel IS REMOVED. It was the only non-test
// caller of domain.CancelTrip (MEASURED — d73-cancel-surface §1a). [[D-73]]
// ruled no trip is cancelled and [[D-78]] closed the route; its registration
// is struck in registerTripRoutes above, carrying the ⟦was:⟧ line. The
// domain author stays for the fold; nothing an officer can reach calls it.
