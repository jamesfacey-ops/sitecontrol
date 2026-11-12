// resolve.go — James, 2026-09-30: officer resolution of parked arrivals
// (Option 1). The review lane's resolution controls (rendered on /trips by
// resolveControlFor), the authoring edge `POST /arrivals/resolve`, and the
// POST-RECOMPUTE CONFIRMATION at `GET /arrivals/resolve?recorded=…`.
//
// ⚠ THE CONFIRMATION IS A READ, NOT A PREDICTION [[D-58]] — the same shape
// as unref.go. The POST appends and returns 303 to this surface carrying the
// recorded key; the GET resolves the key to the resolution row and reads the
// arrival's and the chosen trip's RECOMPUTED state at render time. In the
// crossing case (an explicit arrival for the chosen trip lands from a peer
// at the same moment) the page reports whatever is true, including on a
// later re-read.
//
// ⚠ THE CONTROLS ARE PLAIN FORMS — one per candidate plus "None of these" —
// with hidden `arrival_id` / `trip_id` (or `none`) fields and no script, so
// the surface works with JS off. A candidate the fold shows as SINCE ARRIVED
// gets no button (domain refuses it anyway, ResolveTripAlreadyBound); the row
// says who closed it instead. At a post that did not record the arrival the
// row renders no controls at all (D-87: that post may only radio).
//
// ⚠ THE REFUSALS RENDER COMPOSED COPY, NEVER `err.Error()`; every refusal is
// PRE-APPEND and authors nothing.
//
// Route classification: EXEMPT with written reason (w2_route_walk_test.go) —
// the selection is payload, no instant is typed, default capture.
package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// resolvePath is the one route this surface owns, in one place.
const resolvePath = "/arrivals/resolve"

func (s *Server) registerArrivalResolveRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET "+resolvePath, s.requireLogin(s.handleResolvePage))
	mux.HandleFunc("POST "+resolvePath, s.requireLogin(s.handleResolve))
}

// resolveControl is the review row's resolution control: the parked arrival
// and its candidates, one button each unless since arrived.
type resolveControl struct {
	ArrivalID  string
	Candidates []resolveCandidate
}

type resolveCandidate struct {
	TripID string
	// Label is "driver <name>, <vehicle> from <origin>, dispatched <t>".
	Label string
	// Arrived: the fold shows this candidate since closed by another
	// arrival; the button is withheld and ArrivedAt names the closing post.
	Arrived   bool
	ArrivedAt string
}

// resolveControlFor builds the control for one ambiguous review row, or nil
// when this post did not record the arrival (it may only radio the post
// that did) or the arrival cannot be read.
func (s *Server) resolveControlFor(a domain.ArrivalState) (*resolveControl, error) {
	// A RAW site read, not a second composition: the lane composes each
	// arrival once for its claim text and W9a-16 holds it to exactly once.
	site, err := domain.ArrivalSiteOf(s.Store, a.ArrivalID)
	if err != nil || site == "" {
		return nil, err
	}
	if site != s.Cfg.SiteID {
		return nil, nil
	}
	ctl := &resolveControl{ArrivalID: a.ArrivalID}
	for _, tripID := range domain.AmbiguousCandidates(&a) {
		cand := resolveCandidate{TripID: tripID}
		trip, err := domain.TripByID(s.Store, tripID)
		if err != nil {
			return nil, err
		}
		if trip != nil {
			var parts []string
			if p, err := domain.PersonByID(s.Store, trip.DriverPersonID); err == nil && p != nil {
				parts = append(parts, "driver "+p.Name)
			}
			if trip.VehicleEntered != nil && *trip.VehicleEntered != "" {
				parts = append(parts, *trip.VehicleEntered+" from "+s.Cfg.SiteName(trip.OriginSite))
			} else {
				parts = append(parts, "from "+s.Cfg.SiteName(trip.OriginSite))
			}
			if evs, err := s.Store.Events(trip.OriginSite, trip.NodeEpoch, trip.Seq, 1); err == nil && len(evs) == 1 && evs[0].OccurredAt > 0 {
				parts = append(parts, "dispatched "+time.UnixMilli(evs[0].OccurredAt).In(time.Local).Format("2006-01-02 15:04"))
			}
			cand.Label = strings.Join(parts, ", ")
		}
		ts, err := domain.TripStateByID(s.Store, tripID)
		if err != nil {
			return nil, err
		}
		if ts != nil && ts.ArrivalID != nil {
			cand.Arrived = true
			if at, err := domain.ArrivalSiteOf(s.Store, *ts.ArrivalID); err == nil {
				cand.ArrivedAt = at
			}
		}
		ctl.Candidates = append(ctl.Candidates, cand)
	}
	return ctl, nil
}

// resolvePage is the render model of the confirmation/refusal surface.
type resolvePage struct {
	SiteID string
	Cfg    *config.Config
	Post   string

	Refusal *resolveRefusalView
	Confirm *resolveConfirmView

	// The alert strip rides this page (James, 2026-10-04, L01 F-8: it was
	// missing here and on /orientation while an alert was outstanding).
	Banner    []deadline.Alert
	BannerErr string
}

// resolveRefusalView is one composed refusal; exactly one flag is set.
type resolveRefusalView struct {
	ArrivalID string
	TripID    string
	BoundTo   string

	Unknown         bool
	NotParked       bool
	AlreadyResolved bool
	NotCandidate    bool
	AlreadyBound    bool
	NoChoice        bool
	ReadFailed      bool
	Foreign         bool
	ForeignSite     string
}

// resolveConfirmView is the post-recompute READ.
type resolveConfirmView struct {
	Failed bool
	// NotThisKind ⟦F11, fix3 2026-10-04⟧: the sealed ?recorded= names a
	// record of another kind (recordedNamesOtherKind) — NOTHING TO CONFIRM,
	// never "Recorded, but the outcome could not be determined".
	NotThisKind bool
	Key         string
	Pending   bool
	ArrivalID string

	// Dismissed: "none of these" — the arrival now reads plain.
	Dismissed bool
	// Bound: the chosen trip is closed by THIS arrival.
	Bound bool
	// HeldByOther: the chosen trip is closed, but by another arrival (a
	// crossing explicit arrival); the selection stands on the record.
	HeldByOther bool
	OtherState  bool

	TripID  string
	State   string
	BoundTo string
	// StillParked: the arrival still reads ambiguous — the resolution
	// has not reached the fold's view of it (should not happen at the
	// authoring post; said rather than hidden).
	StillParked bool
}

func (s *Server) buildResolvePage(refusal *resolveRefusalView, confirm *resolveConfirmView) resolvePage {
	p := resolvePage{
		SiteID: s.Cfg.SiteID, Cfg: s.Cfg, Post: s.Cfg.PostName(),
		Refusal: refusal, Confirm: confirm,
	}
	p.Banner, p.BannerErr = s.banner()
	return p
}

func (s *Server) buildResolveConfirm(recorded string) *resolveConfirmView {
	if s.recordedNamesOtherKind(recorded, domain.EventArrivalResolved) {
		return &resolveConfirmView{NotThisKind: true, Key: recorded}
	}
	k, ok := parseRecordedKey(recorded)
	if !ok {
		return &resolveConfirmView{Failed: true}
	}
	row, err := domain.ResolutionAt(s.Store, k)
	if err != nil || row == nil || row.ArrivalID == nil || *row.ArrivalID == "" {
		return &resolveConfirmView{Failed: true}
	}
	v := &resolveConfirmView{ArrivalID: *row.ArrivalID}
	out, err := domain.LoadResolveOutcome(s.Store, v.ArrivalID)
	if err != nil {
		return &resolveConfirmView{Failed: true, ArrivalID: v.ArrivalID}
	}
	if out == nil {
		v.Pending = true
		return v
	}
	switch out.Disposition {
	case domain.ArrivalPlain:
		v.Dismissed = true
	case domain.ArrivalAmbiguous:
		v.StillParked = true
	case domain.ArrivalResolved:
		v.TripID, v.State, v.BoundTo = out.TripID, out.TripState, out.BoundTo
		switch {
		case out.TripState == "":
			v.Pending = true
		case out.BoundTo == v.ArrivalID:
			v.Bound = true
		case out.BoundTo != "":
			v.HeldByOther = true
		default:
			v.OtherState = true
		}
	default:
		v.OtherState = true
		v.State = out.Disposition
	}
	return v
}

func (s *Server) handleResolvePage(w http.ResponseWriter, r *http.Request) {
	var confirm *resolveConfirmView
	if rec := noticeQuery(r).Get(recordedQueryKey); rec != "" {
		confirm = s.buildResolveConfirm(rec)
	}
	s.renderPage(w, "arrival resolve", "arrival_resolve.html", s.buildResolvePage(nil, confirm))
}

// handleResolve appends ONE arrival_resolved, or refuses PRE-APPEND with an
// honest 409 carrying the composed sentence.
func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	arrivalID := strings.TrimSpace(r.FormValue("arrival_id"))
	tripID := strings.TrimSpace(r.FormValue("trip_id"))
	none := strings.TrimSpace(r.FormValue("none")) != ""
	var chosen *string
	switch {
	case none && tripID != "":
		s.renderPageStatus(w, http.StatusConflict, "arrival resolve", "arrival_resolve.html",
			s.buildResolvePage(&resolveRefusalView{ArrivalID: arrivalID, NoChoice: true}, nil))
		return
	case none:
		chosen = nil
	case tripID != "":
		chosen = &tripID
	default:
		s.renderPageStatus(w, http.StatusConflict, "arrival resolve", "arrival_resolve.html",
			s.buildResolvePage(&resolveRefusalView{ArrivalID: arrivalID, NoChoice: true}, nil))
		return
	}
	// No typed instant feeds the envelope: the selection is payload and the
	// act's own envelope pair is default-captured.
	env, err := domain.ResolveParkedArrival(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch,
		arrivalID, chosen, s.postAttribution(), 0, "default")
	if err != nil {
		s.renderPageStatus(w, http.StatusConflict, "arrival resolve", "arrival_resolve.html",
			s.buildResolvePage(resolveRefusalOf(arrivalID, tripID, err), nil))
		return
	}
	http.Redirect(w, r, sealNotices(fmt.Sprintf("%s?recorded=%s.%d.%d",
		resolvePath, env.SiteID, env.NodeEpoch, env.Seq)), http.StatusSeeOther)
}

// resolveRefusalOf maps domain's typed refusal (or the store's D-87 refusal)
// to the view; anything else is a read failure, never the nearest refusal.
func resolveRefusalOf(arrivalID, tripID string, err error) *resolveRefusalView {
	v := &resolveRefusalView{ArrivalID: arrivalID, TripID: tripID}
	var foreign *store.OwnershipError
	if errors.As(err, &foreign) {
		v.Foreign = true
		v.ForeignSite = foreign.TargetSiteID
		if foreign.Target != "" {
			v.ArrivalID = foreign.Target
		}
		return v
	}
	ref, ok := err.(*domain.ResolveRefusal)
	if !ok {
		v.ReadFailed = true
		return v
	}
	if ref.ArrivalID != "" {
		v.ArrivalID = ref.ArrivalID
	}
	if ref.TripID != "" {
		v.TripID = ref.TripID
	}
	v.BoundTo = ref.BoundTo
	switch ref.Code {
	case domain.ResolveArrivalUnknown:
		v.Unknown = true
	case domain.ResolveNotParked:
		v.NotParked = true
	case domain.ResolveAlreadyResolved:
		v.AlreadyResolved = true
	case domain.ResolveTripNotCandidate:
		v.NotCandidate = true
	case domain.ResolveTripAlreadyBound:
		v.AlreadyBound = true
	default:
		v.ReadFailed = true
	}
	return v
}
