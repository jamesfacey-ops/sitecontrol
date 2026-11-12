// unref.go — W9b (item 16): the un-reference surface. F4 §3.6's authoring
// edge (`GET`/`POST /arrivals/unref`, form field `arrival_id`, nothing else)
// and the POST-RECOMPUTE CONFIRMATION.
//
// ⚠ THE CONFIRMATION IS A READ, NOT A PREDICTION, and that is the whole
// shape of this file [F4 §3.6; [[D-58]]; mutant m20]. The POST appends and
// returns 303 to this same surface carrying `?recorded=<site>.<epoch>.<seq>`
// — the house PRG shape person_amend.go established — and the GET resolves
// that key back to the retraction row, then reads the REPUDIATED TRIP'S
// recomputed `trip_state` and the binding arrival's recomputed
// `arrival_state` at render time.
//
// Why it may not be composed at POST time: in the CROSSING CASE the officer
// un-refs here while, independently at the peer, the trip's crew is checked
// in — creating the arrival Pass 2 will bind. That event DOES NOT EXIST when
// a pre-append prediction would be composed, so no prediction can be right.
// Because every sentence below is a function of rows that exist after the
// append and its recompute, the page simply reports whatever happened,
// including on a later re-read. There is no world in which the officer is
// told an alert returned and it did not.
//
// ⚠ NO SURFACE HERE OFFERS A REPLACEMENT TRIP [F4 §3.6, I-2; mutant m15].
// The form has ONE field. The confirmation names the repudiated trip and, in
// the still-bound rendering, the arrival that accounts for it — and offers
// no pick, because re-binding is a NEW EXPLICIT ACT through the ordinary
// check-in path, never a re-pointing of the withdrawal. The payload has no
// trip field at all, so it is not merely absent from the screens: it is
// unrepresentable on the wire.
//
// ⚠ THE REFUSALS RENDER COMPOSED COPY, NEVER `err.Error()`. domain returns a
// typed `*UnrefRefusal` whose code names WHICH of §3.6's three refusals
// fired; this file writes the officer's sentence. A refusal rendered as a Go
// error string puts a package prefix and an internal identifier in front of
// the person who has to act on it — this house has 24 such sites and W9b
// does not make it 25. Every refusal is PRE-APPEND: nothing is authored on
// any of them (mutant m15b is the refusal that appends first).
//
// Route classification: EXEMPT with written reason (F4 §9.4) — the
// repudiation IS the live act and no instant is typed. The row is in
// w2_route_walk_test.go, applied with this route as W3/W4/W5d's builders
// applied theirs.
package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"sitecontrol/internal/config"
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// unrefPath is the one route this surface owns, in one place.
const unrefPath = "/arrivals/unref"

func (s *Server) registerArrivalUnrefRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET "+unrefPath, s.requireLogin(s.handleUnrefPage))
	mux.HandleFunc("POST "+unrefPath, s.requireLogin(s.handleUnref))
}

// unrefPage is the render model. Deliberately WITHOUT a Banner field: the
// topbar renders the C7 alert strip only for a page whose handler populated
// it, and this screen's own confirmation is what says whether an alert came
// back — a second, separately-computed alert surface on the same page could
// disagree with it.
type unrefPage struct {
	SiteID string
	Cfg    *config.Config
	Post   string

	Refusal *unrefRefusalView
	Confirm *unrefConfirmView
}

// unrefRefusalView is one composed refusal. Code-driven so the sentence is
// this file's, not a Go error's.
type unrefRefusalView struct {
	ArrivalID string
	// Exactly one of these is set.
	Unknown          bool
	NoReference      bool
	AlreadyRetracted bool
	// ReadFailed: the refusal is not one of the three — the record could not
	// be read at all. Said loudly rather than dressed as one of the three.
	ReadFailed bool
	// Foreign (D-87): the arrival is another post's record, and the store's
	// choke point refused this post's withdrawal of it. ForeignSite names the
	// post whose record it is — the post to radio.
	Foreign     bool
	ForeignSite string
}

// unrefConfirmView is the post-recompute READ. Every field is derived from
// the recomputed tables at render time.
type unrefConfirmView struct {
	// Failed: the recorded key did not parse, or no retraction stands at it.
	// The append DID happen; what failed is the read-back, and the officer
	// must be told the page is not vouching for the outcome (VF §5's
	// rendering 4).
	Failed bool
	// NotThisKind ⟦F11, fix3 2026-10-04⟧: the sealed ?recorded= names a
	// record of another kind (recordedNamesOtherKind) — NOTHING TO CONFIRM.
	NotThisKind bool
	Key         string

	ArrivalID string

	// Pending: the retraction is recorded, but the arrival it names — or the
	// trip that arrival referenced — has not reached this post yet. Legal
	// (the fold is dangling-tolerant); there is simply nothing to report.
	Pending bool

	TripID string
	State  string // the recomputed trip_state word, verbatim

	// Exactly one of the three renderings.
	Reopened   bool
	StillBound bool
	OtherState bool

	// Reopened's alert clause, read off the live board — never asserted.
	AlertLive     bool
	AckSuppressed bool

	// FromResolution — James, 2026-09-30: unref undoes a resolution — the
	// withdrawn selection was a review-lane resolution; ArrivalNow is the
	// arrival's recomputed disposition and Candidates its recomputed
	// candidate list when it is parked again.
	FromResolution bool
	ArrivalNow     string
	Candidates     string
	// NoneWithdrawn (R3-RES-3): the withdrawn resolution was "None of
	// these" — nothing reopens; rendered as its own outcome, never as the
	// pending copy about a record that "has not reached this post".
	NoneWithdrawn bool

	// StillBound's binder clause.
	BinderID        string
	BinderPost      string
	BinderAutomatic bool
	BinderVehicle   string
	BinderOrigin    string
}

func (s *Server) buildUnrefPage(refusal *unrefRefusalView, confirm *unrefConfirmView) unrefPage {
	return unrefPage{
		SiteID:  s.Cfg.SiteID,
		Cfg:     s.Cfg,
		Post:    s.Cfg.PostName(), // D-5
		Refusal: refusal,
		Confirm: confirm,
	}
}

// buildUnrefConfirm resolves ?recorded=<site>.<epoch>.<seq> to the world the
// retraction left behind, AT RENDER TIME.
func (s *Server) buildUnrefConfirm(recorded string) *unrefConfirmView {
	if s.recordedNamesOtherKind(recorded, domain.EventArrivalRefRetracted) {
		return &unrefConfirmView{NotThisKind: true, Key: recorded}
	}
	k, ok := parseRecordedKey(recorded)
	if !ok {
		return &unrefConfirmView{Failed: true}
	}
	row, err := domain.RefRetractionAt(s.Store, k)
	if err != nil || row == nil || row.ArrivalID == nil || *row.ArrivalID == "" {
		return &unrefConfirmView{Failed: true}
	}
	v := &unrefConfirmView{ArrivalID: *row.ArrivalID}

	out, err := domain.LoadUnrefOutcome(s.Store, v.ArrivalID)
	if err != nil {
		return &unrefConfirmView{Failed: true, ArrivalID: v.ArrivalID}
	}
	if out != nil && out.WasResolution && out.WasNone {
		// R3-RES-3: the record IS here and is this post's own; say what it
		// was and where the arrival now is.
		v.NoneWithdrawn, v.FromResolution = true, true
		v.ArrivalNow, v.Candidates = out.Disposition, out.Candidates
		return v
	}
	if out == nil || out.TripState == "" {
		v.Pending = true
		return v
	}
	v.TripID, v.State = out.TripID, out.TripState
	v.FromResolution, v.ArrivalNow, v.Candidates = out.WasResolution, out.Disposition, out.Candidates

	switch {
	case out.StillBound():
		v.StillBound = true
		v.BinderID = out.BoundTo
		switch out.Binder {
		case domain.BinderCorrelation:
			v.BinderAutomatic = true
			v.BinderVehicle = out.BinderVehicle
			v.BinderOrigin = out.BinderOrigin
		case domain.BinderReference:
			v.BinderPost = out.BinderPost
		}
	case out.Reopened():
		v.Reopened = true
		v.AlertLive, v.AckSuppressed = s.unrefAlertFace(out.TripID)
	default:
		// The trip is closed by facts other than a binding — a cancel. Said
		// in its own state word rather than squeezed into one of the two
		// renderings above, because telling an officer a cancelled trip is
		// OPEN would be the copy-lie this whole surface exists to avoid.
		v.OtherState = true
	}
	return v
}

// unrefAlertFace reads the live board for one trip. It answers only what the
// scan already classified: an overdue row that is NOT acknowledged is a live
// alert; an overdue row that IS acknowledged is suppressed at both posts
// (F6 §1: the ack read is a trip-scoped ∃ with no state and no time filter,
// so a trip acked while overdue and reopened later is BORN ACKED).
//
// A trip still counting down is neither: it has an expectation again, and no
// alert. Saying "its alert is live" there would be mutant m16's shape with
// the sign flipped — a reassurance about an alarm that has not fired.
func (s *Server) unrefAlertFace(tripID string) (live, suppressed bool) {
	if s.Alerts == nil {
		return false, false
	}
	board, err := s.Alerts.Evaluate()
	if err != nil {
		// Nothing is claimed about the alert. The scan's own failure surface
		// is the banner strip on the pages that carry it; this page does not
		// invent an answer it does not have.
		return false, false
	}
	for _, a := range board.Alerts {
		if a.TripID != tripID || a.Status == deadline.StatusCountdown {
			continue
		}
		if a.Acked {
			return false, true
		}
		return true, false
	}
	return false, false
}

func (s *Server) handleUnrefPage(w http.ResponseWriter, r *http.Request) {
	var confirm *unrefConfirmView
	if rec := noticeQuery(r).Get(recordedQueryKey); rec != "" {
		confirm = s.buildUnrefConfirm(rec)
	}
	s.renderPage(w, "arrival unref", "arrival_unref.html", s.buildUnrefPage(nil, confirm))
}

// handleUnref appends ONE arrival_ref_retracted, or refuses PRE-APPEND.
//
// A refusal comes back as this same page carrying the composed sentence and
// an honest 409 — the same shape the gate's refused commits use, so a raw
// POST or a test sees a refusal rather than a 200 that looks like success.
func (s *Server) handleUnref(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	arrivalID := strings.TrimSpace(r.FormValue("arrival_id"))

	// No typed value feeds the envelope here: this surface types no instant
	// at all, and the repudiation's own envelope pair is default-captured
	// (the route's EXEMPT classification, F4 §9.4 / VF §7.5 item 4).
	env, err := domain.RetractArrivalRef(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch,
		arrivalID, s.postAttribution(), 0, "default")
	if err != nil {
		s.renderPageStatus(w, http.StatusConflict, "arrival unref", "arrival_unref.html",
			s.buildUnrefPage(unrefRefusalOf(arrivalID, err), nil))
		return
	}
	http.Redirect(w, r, sealNotices(fmt.Sprintf("%s?recorded=%s.%d.%d",
		unrefPath, env.SiteID, env.NodeEpoch, env.Seq)), http.StatusSeeOther)
}

// unrefRefusalOf maps domain's typed refusal to the view. An error that is
// NOT one of the three is rendered as a read failure rather than silently
// mapped onto the nearest one — a refusal that names the wrong reason sends
// the officer to the wrong remedy.
func unrefRefusalOf(arrivalID string, err error) *unrefRefusalView {
	v := &unrefRefusalView{ArrivalID: arrivalID}
	// D-87: the store's choke point refused a withdrawal of another post's
	// arrival. Wrapped by the domain edge, so errors.As, never a type
	// assertion on the outer error.
	var foreign *store.OwnershipError
	if errors.As(err, &foreign) {
		v.Foreign = true
		v.ForeignSite = foreign.TargetSiteID
		if foreign.Target != "" {
			v.ArrivalID = foreign.Target
		}
		return v
	}
	ref, ok := err.(*domain.UnrefRefusal)
	if !ok {
		v.ReadFailed = true
		return v
	}
	if ref.ArrivalID != "" {
		v.ArrivalID = ref.ArrivalID
	}
	switch ref.Code {
	case domain.UnrefArrivalUnknown:
		v.Unknown = true
	case domain.UnrefNoReference:
		v.NoReference = true
	case domain.UnrefAlreadyRetracted:
		v.AlreadyRetracted = true
	default:
		v.ReadFailed = true
	}
	return v
}
