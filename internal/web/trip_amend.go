// trip_amend.go — the officer-facing TRIP and ARRIVAL amendment surfaces
// (W9a, item 15; design/trip-fact-correction-v1.md rev 4 §3.5, §4, §7).
// One screen corrects what a trip's record says: who drove, who was aboard,
// the vehicle, the destination and its allowance, the cargo note.
// ⟦was: "…, the phase-override reason" — weather phases removed, James 2026-10-04⟧
//
// THE THREE THINGS THIS SURFACE OWES THE OFFICER, none of which the domain
// edge can supply on its own:
//
//  1. §4's STATE-AWARE Class-B gate, in THREE renderings. A Class-B fact
//     (destination, vehicle) on an OPEN trip is refused — but WHICH refusal
//     the officer reads depends on state this layer holds and the domain
//     does not: whether the trip's alert is live (the deadline scanner), and
//     whether this post is the origin (config). ⟦D-78, 2026-09-05⟧ The copy
//     is not decoration: since [[D-73]]/[[D-78]] the countdown rendering
//     names the WAIT-UNTIL-IT-ARRIVES path as the working one (⟦was: "names
//     cancel+redispatch as the working path"⟧ — that instruction emptied the
//     printed muster and D-73 removed the operation); the OVERDUE rendering
//     still says do not cancel, because for already-replicated history the
//     state persists; and at a post that is not the origin the third
//     rendering names the origin whose trip this is and says the correction
//     can be made here once it arrives (⟦was: "the same instruction is
//     IMPOSSIBLE to follow, because cancel is origin-only, so the third
//     rendering says so and NAMES the post to call"⟧).
//
//  2. §7's UNSIGHTED-REMOVAL gate — since [[D-80]] (2026-09-05) a REFUSAL,
//     no longer a warning with a confirm. ⟦was: "It is a WARNING and not a
//     refusal, deliberately: refusing unless the person is accounted for
//     elsewhere would refuse TRUE corrections."⟧ James: "they would not mark
//     as never aboard they would just remove them from the list the same way
//     as if they left" / "no one would be standing at a site without a
//     vehicle as its -65F" — the true correction the warning protected does
//     not occur at this site, and the operation's only real effect was the
//     person-on-no-list hole: an occupant whose return has not reached this
//     post is listed NOWHERE but this manifest (origin visit closed at
//     departure, no visit since), so removing them puts them on no sheet at
//     either post. The gate stays CONDITIONAL on the same sightedSince the
//     muster's four categories share: a SIGHTED person's removal is
//     record-only (they are on a sheet by their visit) and authors without
//     comment — so the closure cannot strand anyone and cannot refuse a
//     correction about someone demonstrably elsewhere. The domain author and
//     the fold are untouched: replicated removals still compose (T2).
//
//     ⚠ THIS DOES NOT CLOSE THE [[D-59]] REMOVAL GAP. An `occupants_remove`
//     naming a MERGED-AWAY person id still composes to a silent no-op where
//     the gate passes it: nothing happens. That gap is booked as an
//     obligation on W9a's GATE and may not be recorded as discharged by this
//     gate, by the merge resolution on the read path, or by any composition
//     change.
//
//  3. VF §5's CONFIRMATION, computed AT RENDER TIME from the recorded key
//     (§4.3 rule 2: read-time truth, never a POST-time cache) — including
//     §3.4's INVARIANT-OVERRIDE sentence, which no other surface can
//     produce: a removal can WIN its per-fact contest and still be negated
//     by the structural driver union, and a bare "Recorded." there is worse
//     than an adverse rendering, because the officer walks away believing
//     the person is off the record.
//
// ⚠ NO RAW `err.Error()` REACHES THIS PAGE. The domain says which of the
// three error classes happened BY TYPE — a fact-domain refusal (authored
// copy, safe to render), an input-validity refusal (the officer named a
// record this post cannot act on), or an internal fault (logged; the page
// carries authored copy only). Reading the string would be the same guess
// in a different place.
package web

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"sitecontrol/internal/config"
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

func (s *Server) registerTripAmendRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /trips/amend", s.requireLogin(s.handleTripAmendPage))
	mux.HandleFunc("POST /trips/amend", s.requireLogin(s.handleTripAmend))
	mux.HandleFunc("POST /arrivals/amend", s.requireLogin(s.handleArrivalAmend))
}

// The form field names are F4 §3.1's frozen PAYLOAD KEYS — the wire
// spelling IS the form spelling, so nothing translates. ⟦D-80⟧ ⟦was: "plus
// one field the payload has no counterpart for: §7's explicit confirm, which
// is an authoring-time affordance and is never replicated"⟧ — that confirm
// is retired; every field below is a payload key.
const (
	fieldTripID          = "trip_id"
	fieldArrivalID       = "arrival_id"
	fieldDriver          = "driver_person_id"
	fieldVehicle         = "vehicle_entered"
	fieldDestKind        = "destination_kind"
	fieldDestValue       = "destination_value"
	fieldDeadlineMinutes = "deadline_minutes"
	fieldCargoNote       = "cargo_note"
	// fieldRemovedOverrideReason is the struck phase-override field: a
	// cached form still asserting it is refused, never silently dropped
	// (weather phases removed, James 2026-10-04).
	fieldRemovedOverrideReason = "phase_override_reason"
	fieldOccupantsAdd          = "occupants_add"
	fieldOccupantsRemove       = "occupants_remove"
	fieldClear                 = "clear"
	fieldOriginSite            = "origin_site"
	// ⟦D-80, 2026-09-05⟧ ⟦was: fieldConfirmUnsighted = "confirm_unsighted"⟧ —
	// §7's confirm is retired with the warning it used to pass. A body still
	// carrying it is ignored, and the unsighted removal it unlocked is refused.
)

type tripAmendOutcomeRow struct {
	Fact  string
	Label string
	// Effective: this assertion is what every surface shows.
	Effective bool
	// Overridden: the assertion WON its per-fact contest and the §3.4
	// structural union negated it anyway. Rendered instead of the terse
	// success line, never beside it.
	Overridden bool
	PersonName string
	// Removed: an occupant assertion in the REMOVAL direction. The success
	// line says so (L09 T-164): "Effective: who was aboard (Name)" read the
	// same for a removal as for an addition, so the officer could not tell
	// from the confirmation which way the record moved.
	Removed bool
	// SupersededPost: VF §5 rendering 2 — this assertion stands and took
	// the place of one made there. "this post" when it is this post (L-1).
	SupersededPost string
	// WinnerPost / WinnerValue: the adverse register — where the assertion
	// that stands was made, and WHAT the record now shows. The value half
	// is the one that tells the officer what IS true rather than merely
	// that they lost.
	WinnerPost       string
	WinnerValue      string
	WinnerAtThisPost bool
}

type tripAmendOutcomeView struct {
	// NotThisKind ⟦D22, fix2-fj 2026-10-04⟧: the key names an event of
	// another kind — see amendOutcomeView.NotThisKind.
	NotThisKind bool
	Key         string
	// Failed: VF §5 rendering 4 — the append DID succeed; what failed is
	// the read-back, and the officer must know the confirmation is not
	// vouching for the outcome.
	Failed bool
	Rows   []tripAmendOutcomeRow
}

// tripAmendPage is the render model. The typed values ride the CONTROLS so
// a refusal re-renders the form with the officer's typing intact: a refusal
// that discards it makes the officer re-enter everything.
type tripAmendPage struct {
	pageShell // top bar: post, alert banner (pageshell.go)

	SiteID string
	Cfg    *config.Config
	Post   string

	TripID          string
	Driver          string
	Vehicle         string
	DestKind        string
	DestValue       string
	DeadlineMinutes string
	CargoNote       string
	OccupantsAdd    string
	OccupantsRemove string
	Clear           string

	// Error / InputError / InternalError: three classes, three arms — the
	// officer's next action differs in each (fix the amendment, fix the id,
	// raise a fault). Never flattened into one.
	Error         string
	InputError    string
	InternalError bool

	// ⟦D-80⟧ ⟦was: `Unsighted []domain.OccupantRef` — "§7's warning: the people
	// this removal would take off the not-accounted-for section with no
	// recorded return. Non-empty means the page IS the warning"⟧ The warning
	// page is gone; an unsighted removal is a REFUSAL, rendered through Error.

	Destinations []config.Destination

	// ArrivalAmend: the refusal came from POST /arrivals/amend, which has no
	// form of its own on this page — so the REFUSED notice must not promise
	// "your entry is still on the form below" (L09 T-165): the trip form
	// re-renders with no arrival id anywhere, and re-sending is impossible
	// from here.
	ArrivalAmend bool

	// Record is the trip's CURRENT effective record, rendered when the page
	// is opened for a named trip (L09 T-141: person ids were visible on no
	// page, so the Driver / Aboard / Remove fields could only be fed from a
	// database dump). Nil when no trip id was given or none is known here.
	Record *tripAmendRecordView

	Outcome *tripAmendOutcomeView
}

// tripAmendRecordView is what the record says RIGHT NOW about the named
// trip — composed (EffectiveTrip), names resolved, ids shown beside them so
// the officer can copy one into the fields below.
type tripAmendRecordView struct {
	TripID      string
	Vehicle     string
	State       string
	Destination string
	Driver      tripAmendPersonView
	Aboard      []tripAmendPersonView
}

type tripAmendPersonView struct {
	Name string
	ID   string
}

func (s *Server) buildTripAmendPage(typed tripAmendPage, outcome *tripAmendOutcomeView) tripAmendPage {
	typed.pageShell = s.shell()
	typed.SiteID = s.Cfg.SiteID
	typed.Cfg = s.Cfg
	typed.Post = s.Cfg.PostName()
	typed.Destinations = s.Cfg.DestinationList()
	typed.Outcome = outcome
	return typed
}

func (s *Server) handleTripAmendPage(w http.ResponseWriter, r *http.Request) {
	var outcome *tripAmendOutcomeView
	if rec := noticeQuery(r).Get(recordedQueryKey); rec != "" {
		outcome = s.buildTripAmendOutcome(rec)
	}
	typed := tripAmendPage{TripID: strings.TrimSpace(r.URL.Query().Get(fieldTripID))}
	if typed.TripID != "" {
		typed.Record = s.tripAmendRecord(typed.TripID)
	}
	s.renderPage(w, "trip amend", "trip_amend.html", s.buildTripAmendPage(typed, outcome))
}

// tripAmendRecord composes the named trip for the page's "what the record
// says now" block (L09 T-141). Nil for a trip this post has no record of —
// the form still renders, and the POST's own rung-1 refusal says so in
// words. A read failure is logged and rendered as "no record shown", never
// as a page error: the block is an aid, not the surface.
func (s *Server) tripAmendRecord(tripID string) *tripAmendRecordView {
	c, err := domain.EffectiveTrip(s.Store, tripID)
	if err != nil {
		log.Printf("trip amend: record %s: %v", tripID, err)
		return nil
	}
	if c == nil {
		return nil
	}
	v := &tripAmendRecordView{TripID: tripID, State: "open"}
	if st, serr := domain.TripStateByID(s.Store, tripID); serr == nil && st != nil {
		v.State = st.State
	}
	if live, lerr := domain.NewLiveVehicles(s.Store); lerr == nil {
		if display, _, cerr := live.Compose(*c); cerr == nil {
			v.Vehicle = display
		}
	}
	switch {
	case c.Trip.DestinationSite != nil:
		v.Destination = s.peerDisplayName(*c.Trip.DestinationSite)
		if c.Trip.DeadlineMinutes != nil {
			v.Destination += fmt.Sprintf(" (%d min allowance)", *c.Trip.DeadlineMinutes)
		}
	case c.Trip.DestinationUncontrolled != nil:
		v.Destination = s.padName(*c.Trip.DestinationUncontrolled) + " (uncontrolled pad)"
	case c.RoadWork:
		v.Destination = "working on the road"
	default:
		v.Destination = "none — a gate check-in, not a journey"
	}
	v.Driver = tripAmendPersonView{Name: s.personDisplayName(c.Trip.DriverPersonID), ID: c.Trip.DriverPersonID}
	for _, id := range c.OccupantIDs {
		v.Aboard = append(v.Aboard, tripAmendPersonView{Name: s.personDisplayName(id), ID: id})
	}
	return v
}

// knownDestinationSiteIDs is rung 7's whitelist at this edge. James,
// 2026-10-03 (FIX-PLAN M-4 / L09 T-157): a destination correction must name
// a KNOWN site id even when this post's own destination list is empty.
// ⟦was: `s.controlledDestinationIDs()` alone⟧ — at NPF/NDB/STP that list is
// `destinations = []`, the domain skips membership on an empty list, and
// "banana" was recorded and replicated to all five posts. The set is this
// post's configured controlled destinations ∪ every replication peer: the
// posts this network consists of, which is what a corrected destination can
// truthfully be. This post itself is deliberately NOT in it — a trip does
// not travel to the post it is corrected at unless that post is configured
// as a destination (T-156 at Ice Pad refuses "ice-pad", as before).
func (s *Server) knownDestinationSiteIDs() []string {
	seen := map[string]bool{}
	out := []string{} // non-nil even when empty: the domain reads empty as "no config"
	for _, id := range s.controlledDestinationIDs() {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, p := range s.Cfg.Peers {
		if p.SiteID != "" && !seen[p.SiteID] {
			seen[p.SiteID] = true
			out = append(out, p.SiteID)
		}
	}
	return out
}

// buildTripAmendOutcome computes the confirmation block for one recorded
// key, AT RENDER TIME (VF §4.3 rule 2: a status stamped at write goes
// permanently wrong the moment the deferred winner arrives by replication).
func (s *Server) buildTripAmendOutcome(recorded string) *tripAmendOutcomeView {
	// ⟦D22's class⟧ this page authors both trip and arrival amendments.
	// F11 (fix round 3, D→E): asked BEFORE parseRecordedKey, so another
	// kind's confirmation whose `recorded` is not a key at all (a minted
	// trade id) reads NOTHING TO CONFIRM rather than a failed read-back.
	// ⟦was: recordedOtherKind(k, …) after a successful parse⟧
	if s.recordedNamesOtherKind(recorded, domain.EventTripAmended, domain.EventArrivalAmended) {
		return &tripAmendOutcomeView{NotThisKind: true, Key: recorded}
	}
	k, ok := parseRecordedKey(recorded)
	if !ok {
		return &tripAmendOutcomeView{Failed: true}
	}
	outs, err := domain.Outcome(s.Store, k)
	if err != nil || len(outs) == 0 {
		if err != nil {
			log.Printf("trip amend: outcome %v: %v", k, err)
		}
		return &tripAmendOutcomeView{Failed: true}
	}
	competitors, cerr := domain.Competitors(s.Store, k)
	if cerr != nil {
		// The outcome resolved; only the competitor enumeration did not. A
		// confirmation that quietly drops half of what it was asked to say
		// is the silent-OK this surface exists to refuse.
		log.Printf("trip amend: competitors %v: %v", k, cerr)
		return &tripAmendOutcomeView{Failed: true}
	}
	// §3.4's invariant override is derived by comparing the per-fact
	// resolution against the POST-UNION composed record — no stored status.
	overridden := map[string]bool{}
	if tripID, terr := domain.TripAmendmentTarget(s.Store, k); terr == nil && tripID != "" {
		if c, cerr := domain.EffectiveTrip(s.Store, tripID); cerr == nil && c != nil {
			for _, id := range c.OverriddenRemovals {
				overridden[id] = true
			}
		}
	}
	view := &tripAmendOutcomeView{}
	for _, o := range outs {
		row := tripAmendOutcomeRow{
			Fact: o.Fact, Label: s.tripFactLabel(o.Fact),
			Effective: o.Status == domain.Effective,
		}
		if id, isOccupant := domain.OccupantFactPersonID(o.Fact); isOccupant {
			row.PersonName = s.personDisplayName(id)
			row.Removed = domain.OccupantAssertionIsRemoval(o.Asserted)
			if overridden[id] {
				row.Overridden = true
			}
		}
		if row.Effective {
			if peers := competitors[o.Fact]; len(peers) > 0 {
				row.SupersededPost = s.supersededPostName(peers[len(peers)-1].SiteID)
			}
		} else {
			row.WinnerPost = s.peerDisplayName(o.Winner.SiteID)
			row.WinnerAtThisPost = o.Winner.SiteID == s.Cfg.SiteID
			row.WinnerValue = s.winningAssertion(o.Winner, o.Fact)
			if o.Fact == fieldDriver {
				// The driver fact's canonical assertion is a person id; the
				// officer reads a name (L09 T-164: "the record shows
				// ice-pad-person-d832…"). Resolved at render, like every
				// other person on this page.
				row.WinnerValue = s.personDisplayName(row.WinnerValue)
			}
		}
		view.Rows = append(view.Rows, row)
	}
	return view
}

// winningAssertion renders WHAT THE RECORD NOW SHOWS for a fact this
// amendment lost — read from the winning event's own outcome, so the value
// carried on the page is the registry's canonical rendering of the
// assertion that stands and not a second derivation of it.
func (s *Server) winningAssertion(winner store.Key, fact string) string {
	outs, err := domain.Outcome(s.Store, winner)
	if err != nil {
		return ""
	}
	for _, o := range outs {
		if o.Fact == fact {
			return o.Asserted
		}
	}
	return ""
}

// tripFactLabel is the officer-facing name of a fact. The occupant half is
// derived from the fact-name PATTERN (§6), never from a second copy of a
// list that cannot be enumerated.
func (s *Server) tripFactLabel(fact string) string {
	if id, ok := domain.OccupantFactPersonID(fact); ok {
		return "who was aboard (" + s.personDisplayName(id) + ")"
	}
	switch fact {
	case fieldDriver:
		return "driver"
	case fieldVehicle:
		return "vehicle"
	case "destination":
		return "destination"
	case fieldCargoNote:
		return "cargo note"
	case fieldOriginSite:
		// The arrival carrier's second claim (L09 T-165: the wire name
		// "origin_site" reached the officer as a label).
		return "origin post, as claimed"
	}
	return fact
}

func (s *Server) personDisplayName(personID string) string {
	p, err := domain.PersonByID(s.Store, personID)
	if err != nil || p == nil || p.Name == "" {
		return personID
	}
	return p.Name
}

// handleTripAmend authors one trip amendment, or refuses PRE-APPEND. Every
// refusal path authors NOTHING.
func (s *Server) handleTripAmend(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	// A cached form TYPING a phase override reason is refused, by name —
	// never dropped while the rest of the correction is recorded as if the
	// officer's words had landed (weather phases removed, James 2026-10-04).
	// An empty submission asserted nothing, exactly as optionalField reads it.
	if strings.TrimSpace(r.FormValue(fieldRemovedOverrideReason)) != "" {
		http.Error(w, "the phase override reason no longer exists — weather phases were removed. Reload the correction page and submit it without that field. Nothing was recorded.", http.StatusBadRequest)
		return
	}
	// ⚠ THE ABSENT TEST IS MADE ONCE, GENERICALLY, AND IT READS NO FACT'S
	// CONTENT. `optionalField` does not know which field it is looking at:
	// an HTML control is always submitted, so "the officer did not touch
	// this one" can only be an empty submission, and that is the nil/absent
	// distinction — never a judgement about what a value SAYS. This matters
	// most for the cargo note, where W5e-2 forbids any flag, taxonomy or
	// code path derived from content [[D-21.3]]: nothing here, and nothing
	// downstream of here, reads what the note says.
	//
	// ⚠ FREE TEXT IS TAKEN VERBATIM. The vehicle (M8) and the cargo note
	// (W5e-1) are stored exactly as entered — no trimming, no case change,
	// no normalisation. Ids and numbers are trimmed because whitespace
	// around an identifier is a typing artefact, not part of the value.
	driverID, hasDriver := optionalField(r, fieldDriver, trimmed)
	vehicle, hasVehicle := optionalField(r, fieldVehicle, verbatim)
	cargo, hasCargo := optionalField(r, fieldCargoNote, verbatim)
	destKind, _ := optionalField(r, fieldDestKind, trimmed)
	destValue, hasDestValue := optionalField(r, fieldDestValue, trimmed)
	deadline, hasDeadline := optionalField(r, fieldDeadlineMinutes, trimmed)

	typed := tripAmendPage{
		TripID:          strings.TrimSpace(r.FormValue(fieldTripID)),
		Driver:          driverID,
		Vehicle:         vehicle,
		DestKind:        destKind,
		DestValue:       destValue,
		DeadlineMinutes: deadline,
		CargoNote:       cargo,
		OccupantsAdd:    strings.TrimSpace(r.FormValue(fieldOccupantsAdd)),
		OccupantsRemove: strings.TrimSpace(r.FormValue(fieldOccupantsRemove)),
		Clear:           strings.TrimSpace(r.FormValue(fieldClear)),
	}

	in := domain.TripAmendment{
		TripID:                typed.TripID,
		OccupantsAdd:          formIDList(r, fieldOccupantsAdd),
		OccupantsRemove:       formIDList(r, fieldOccupantsRemove),
		Clear:                 formIDList(r, fieldClear),
		KnownDestinationSites: s.knownDestinationSiteIDs(),
		KnownUncontrolledPads: s.uncontrolledDestinationIDs(),
		// The driver-handover refusals name posts (James, 2026-10-03,
		// NEW-b); they read as the officer's pages read them.
		SiteName: s.peerDisplayName,
	}
	if hasDriver {
		in.DriverPersonID = &driverID
	}
	if hasVehicle {
		in.VehicleEntered = &vehicle
	}
	if hasCargo {
		in.CargoNote = &cargo
	}
	if destKind != "" || hasDestValue {
		d := domain.DestinationAmend{Kind: destKind, Value: destValue}
		if hasDeadline {
			m, err := strconv.ParseInt(deadline, 10, 64)
			if err != nil {
				// The REFUSED arm, not the input-error arm (L09 T-155): the
				// officer typed a number wrong, not an id, and the id arm's
				// "Check the id against the record" sent them hunting for
				// a mistake they had not made.
				typed.Error = "the travel allowance must be a whole number of minutes"
				s.renderPageStatus(w, http.StatusConflict, "trip amend", "trip_amend.html",
					s.buildTripAmendPage(typed, nil))
				return
			}
			d.DeadlineMinutes = &m
		}
		in.Destination = &d
	}

	// --- §4's STATE-AWARE Class-B gate, the three renderings ------------
	if refusal, refused := s.classBRefusal(in); refused {
		typed.Error = refusal
		s.renderPageStatus(w, http.StatusConflict, "trip amend", "trip_amend.html",
			s.buildTripAmendPage(typed, nil))
		return
	}

	// --- §7's unsighted-removal gate — a REFUSAL since [[D-80]] ----------
	// ⟦was: `if r.FormValue(fieldConfirmUnsighted) == "" && len(in.OccupantsRemove) > 0 {`
	// … `typed.Unsighted = unsighted` — the WARNING page, with a confirm that
	// re-sent the same body past it⟧
	// No confirm can pass it now: a stale body still carrying
	// confirm_unsighted=1 is refused like any other. The PREDICATE is
	// unchanged — the same sightedSince the muster's four categories share —
	// so exactly the removals that would leave a person accounted for only
	// as NOT TRACKED are the ones refused (⟦was: "on no sheet at either
	// post" — untrue since the D1 ruling, FINDINGS-R5 F10⟧), and a SIGHTED
	// person's record-only removal still authors below.
	if len(in.OccupantsRemove) > 0 {
		unsighted, err := domain.UnsightedRemovals(s.Store, in.TripID, in.OccupantsRemove)
		if err != nil {
			log.Printf("trip amend: unsighted removals: %v", err)
			typed.InternalError = true
			s.renderPageStatus(w, http.StatusInternalServerError, "trip amend", "trip_amend.html",
				s.buildTripAmendPage(typed, nil))
			return
		}
		if len(unsighted) > 0 {
			typed.Error = unsightedRemovalRefusal(unsighted)
			s.renderPageStatus(w, http.StatusConflict, "trip amend", "trip_amend.html",
				s.buildTripAmendPage(typed, nil))
			return
		}
	}

	env, err := domain.AmendTrip(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, in, 0, "default")
	if err != nil {
		s.renderTripAmendError(w, typed, err)
		return
	}
	http.Redirect(w, r, sealNotices(fmt.Sprintf("/trips/amend?recorded=%s.%d.%d",
		env.SiteID, env.NodeEpoch, env.Seq)), http.StatusSeeOther)
}

// renderTripAmendError puts the domain's THREE error classes on three arms,
// plus the store's D-87 refusal on the REFUSED arm. A raw fault string never
// reaches the page.
func (s *Server) renderTripAmendError(w http.ResponseWriter, typed tripAmendPage, err error) {
	var refusal *domain.TripAmendRefusal
	if errors.As(err, &refusal) {
		typed.Error = refusal.Reason
		s.renderPageStatus(w, http.StatusConflict, "trip amend", "trip_amend.html",
			s.buildTripAmendPage(typed, nil))
		return
	}
	// D-87: the choke point refused a modification of another site's
	// presence row (an arrival amended from a post that did not record it).
	// A refusal the officer can act on — radio the post whose record it is
	// — not an internal fault.
	var foreign *store.OwnershipError
	if errors.As(err, &foreign) {
		// Authored copy from the typed fields (L09 T-165c) — ⟦was:
		// `typed.Error = foreign.Error()`⟧, which put "store: author:
		// refused — arrival_amended would modify …" in front of an officer,
		// against this file's own header rule. The D-87 sentence itself
		// (store.OwnershipRule) is kept verbatim: it is the one line a
		// surface and a test name the refusal by. (It no longer carries the
		// "(D-87)" id — FIX5: no decision id on an officer-visible refusal.)
		typed.Error = s.ownershipRefusal(foreign)
		s.renderPageStatus(w, http.StatusConflict, "trip amend", "trip_amend.html",
			s.buildTripAmendPage(typed, nil))
		return
	}
	var bad *domain.TripAmendInputError
	if errors.As(err, &bad) {
		typed.InputError = bad.Reason
		s.renderPageStatus(w, http.StatusConflict, "trip amend", "trip_amend.html",
			s.buildTripAmendPage(typed, nil))
		return
	}
	log.Printf("trip amend: %v", err)
	typed.InternalError = true
	s.renderPageStatus(w, http.StatusInternalServerError, "trip amend", "trip_amend.html",
		s.buildTripAmendPage(typed, nil))
}

// ownershipRefusal renders a D-87 refusal for the officer: whose record it
// is, that this post cannot change it, and who to radio. Both halves of
// store.OwnershipError's shape are covered — the row is another post's, or
// no such row is known here at all.
func (s *Server) ownershipRefusal(e *store.OwnershipError) string {
	if e.TargetSiteID == "" {
		return fmt.Sprintf("this post has no record of %s, so it cannot be shown to be this post's own and cannot be corrected here — %s. Check the id, or ask the post that took the record to correct it there",
			e.Target, store.OwnershipRule)
	}
	owner := s.peerDisplayName(e.TargetSiteID)
	if owner != e.TargetSiteID {
		owner += " (" + e.TargetSiteID + ")"
	}
	return fmt.Sprintf("this record (%s) belongs to %s, and %s. Radio %s to correct it there",
		e.Target, owner, store.OwnershipRule, s.peerDisplayName(e.TargetSiteID))
}

// classBWaitSentence is [[D-78]]'s instruction for a Class-B fact on an
// open, counting-down trip — the sentence the countdown rendering carries
// and the ONE string the tests hold it to (W9a-9 asserts its presence on the
// rendered refusal; y5 Q19 asserts its absence on the typo path). One
// constant, so a negative cannot die by drift or by case. The advice is
// TRUE: after arrival the vehicle, destination and allowance are all
// amendable from either post [MEASURED — reports/witness/d73-cancel-surface.md §3].
const classBWaitSentence = "Wait until it arrives and the arrival is recorded"

// unsightedRemovalRefusalSentence is [[D-80]]'s reason, pinned: the
// never-aboard correction is REFUSED for anyone with no recorded sighting
// since the trip left, because the manifest is then the only place they are
// accounted for. W9a-10 asserts it on the refusal and its absence on the
// sighted arm's confirmation.
//
// Fix round 3 (FINDINGS-R5 F10; F-L06-2): since the D1 ruling (James,
// 2026-10-04) a person taken off a run is listed as NOT TRACKED on it, so
// "no sheet" is no longer what would happen. ⟦was: "taking them off it would
// leave them on no sheet at either post"⟧
const unsightedRemovalRefusalSentence = "taking them off it would only move them to NOT TRACKED on this run at every post, where nothing is watching for them"

// unsightedRemovalRefusal composes [[D-80]]'s refusal for the officer: the
// named people, what THIS POST knows about them (L-1's honesty direction —
// never a claim about the world), and why the removal is refused. It renders
// through the page's REFUSED slot, which appends "Nothing was recorded; your
// entry is still on the form below."
//
// ⟦fix4 H, 2026-10-05 (L03-02 observation)⟧ the same sentence renders at BOTH
// ends of the run, and at the destination "here" was not where a returning
// vehicle is checked in — that is the origin. ⟦was: "…, or here if it comes
// back"⟧ trip_amend.html's Remove help says the same.
func unsightedRemovalRefusal(unsighted []domain.OccupantRef) string {
	names := make([]string, 0, len(unsighted))
	for _, p := range unsighted {
		names = append(names, p.Name)
	}
	return strings.Join(names, ", ") +
		": no return has reached this post, so this trip's list is the only place they are accounted for — " +
		unsightedRemovalRefusalSentence +
		". Nobody is marked never aboard; a person leaves the list when they are checked in — at the destination when the vehicle arrives there, or back at the post it left from if it comes back"
}

// classBRefusal is §4's gate: the Class-B facts (destination, vehicle) are
// correctable only on a COMPLETED trip, and the refusal an officer reads
// depends on the trip's live state and on which post they are standing at.
//
// It returns ("", false) for anything it cannot decide here — the domain
// edge refuses again as the backstop, in its own words. A degenerate gate
// check-in is deliberately NOT answered here: F4 §3.5 owes it the check-in
// copy, which is the domain's, and answering it with a countdown sentence
// would tell the officer to cancel and re-dispatch a walk through a gate.
func (s *Server) classBRefusal(in domain.TripAmendment) (string, bool) {
	if in.Destination == nil && in.VehicleEntered == nil {
		return "", false
	}
	trip, err := domain.TripByID(s.Store, in.TripID)
	if err != nil || trip == nil {
		return "", false
	}
	if trip.DestinationSite == nil && trip.DestinationUncontrolled == nil && trip.DeadlineMinutes == nil {
		return "", false // §10's degenerate trip — the domain's check-in copy
	}
	state, err := domain.TripStateByID(s.Store, in.TripID)
	if err != nil || state == nil || state.State != domain.TripStateOpen {
		return "", false
	}

	fact := "destination and its travel allowance"
	if in.VehicleEntered != nil {
		fact = "vehicle"
		if in.Destination != nil {
			fact = "vehicle and destination"
		}
	}
	var b strings.Builder
	if s.tripAlertIsOverdue(in.TripID) {
		// RENDERING 2. It must NOT name cancel+redispatch: cancelling
		// removes the alert AND the board row entirely, with nothing left on
		// any screen for a vehicle that may be missing, and a re-dispatch
		// mints a fresh full countdown. That copy channels an officer into
		// silencing a live alert at 03:00.
		// ⟦L09 T-151, 2026-10-03⟧ ⟦was: "… alert is live. Do not cancel it
		// to fix the record: cancelling takes the alert and the board row
		// off every screen for a vehicle that may be missing. Verify …"⟧
		// Since [[D-78]] there is no cancel operation on any post, so
		// warning the officer off one reads as a stale instruction. The
		// working path is stated alone.
		b.WriteString("this vehicle is OVERDUE and its alert is live. Verify the vehicle by radio and resolve the alert first — the ")
		b.WriteString(fact)
		b.WriteString(" can be corrected once the trip is completed")
	} else {
		// RENDERING 1: the working path, named. ⟦D-78, 2026-09-05⟧
		// ⟦was: "this trip is open and counting down, and the ⟨fact⟩ drives
		// the running timer and the destination's board while it is. Cancel it
		// and re-dispatch with the correct ⟨fact⟩ — the timer and the board
		// must match the record"⟧
		// [[D-73]]: "no trip will be cancled they will continue on to the next
		// post". [[D-78]]: the refusal says WAIT UNTIL IT ARRIVES, then correct
		// it — and that path is TRUE: after arrival the vehicle, destination
		// and allowance are all amendable, from either post [MEASURED,
		// d73-cancel-surface §3]. classBWaitSentence is the pinned constant.
		b.WriteString("this trip is open and counting down, and the ")
		b.WriteString(fact)
		b.WriteString(" drives the running timer and the destination's board while it is. ")
		b.WriteString(classBWaitSentence)
		b.WriteString(", then correct the ")
		b.WriteString(fact)
		b.WriteString(" here — the timer and the board must keep matching the record while it runs")
	}
	if trip.OriginSite != s.Cfg.SiteID {
		// RENDERING 3. ⟦D-78, 2026-09-05⟧ Since D-78 there is nothing to
		// cancel at any post, so this rendering no longer names the impossible
		// cancel-at-the-origin instruction. ⟦was: ". Cancellation is recorded
		// at the origin post only — this post cannot cancel ⟨origin⟩'s trip;
		// radio ⟨origin⟩ if it must be stood down"⟧ What the officer here is
		// owed instead: whose trip this is (the origin, named), and that once
		// it arrives the correction can be made HERE — it need not go through
		// the origin (MEASURED — a destination amended an arrived trip's
		// vehicle, d73-cancel-surface §3).
		origin := s.peerDisplayName(trip.OriginSite)
		b.WriteString(". The vehicle is on the road from its origin post, ")
		b.WriteString(origin)
		b.WriteString("; once its arrival is recorded here, the ")
		b.WriteString(fact)
		b.WriteString(" can be corrected at this post")
	}
	return b.String(), true
}

// tripAlertIsOverdue asks the deadline scanner — the persisted, site-local
// authority on whether a trip's deadline has passed (§9.4). With no scanner
// wired (web-layer-only tests) the answer is NO, which is the honest one:
// this post cannot see an alert it is not running.
func (s *Server) tripAlertIsOverdue(tripID string) bool {
	if s.Alerts == nil {
		return false
	}
	board, err := s.Alerts.Evaluate()
	if err != nil {
		log.Printf("trip amend: deadline scan: %v", err)
		return false
	}
	for _, a := range board.Alerts {
		if a.TripID != tripID {
			continue
		}
		if a.Status == deadline.StatusOverdueLive || a.Status == deadline.StatusOverdueUnresolved {
			return true
		}
	}
	return false
}

// handleArrivalAmend corrects what an unsolicited arrival's officer TYPED —
// the vehicle and the origin they CLAIMED. §5 rule 4 makes it RECORD-ONLY:
// it never re-runs correlation, never binds and never un-binds. Correcting
// a claim to the trip's own vehicle must NOT silence that trip's live alert.
func (s *Server) handleArrivalAmend(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	typed := tripAmendPage{
		TripID:       strings.TrimSpace(r.FormValue(fieldTripID)),
		Vehicle:      r.FormValue(fieldVehicle),
		ArrivalAmend: true, // no form for this POST on the page: the notice must not promise one
	}
	in := domain.ArrivalAmendment{ArrivalID: strings.TrimSpace(r.FormValue(fieldArrivalID)), KnownOriginSites: s.peerSiteIDs()}
	if v := r.FormValue(fieldVehicle); strings.TrimSpace(v) != "" {
		in.VehicleEntered = &v
	}
	if o := strings.TrimSpace(r.FormValue(fieldOriginSite)); o != "" {
		in.OriginSite = &o
	}
	env, err := domain.AmendArrival(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, in, 0, "default")
	if err != nil {
		s.renderTripAmendError(w, typed, err)
		return
	}
	http.Redirect(w, r, sealNotices(fmt.Sprintf("/trips/amend?recorded=%s.%d.%d",
		env.SiteID, env.NodeEpoch, env.Seq)), http.StatusSeeOther)
}

// optionalField is the ABSENT test, made once for every optional control.
// It returns the submitted value and whether the officer typed anything —
// and it is deliberately FIELD-AGNOSTIC: it never inspects, classifies or
// branches on what any particular fact SAYS, so no fact's content can
// acquire a meaning here by accident.
//
// `keep` decides whether the value is taken VERBATIM (free text: the
// vehicle under M8, the cargo note under W5e-1) or with surrounding
// whitespace removed (ids and numbers, where whitespace is a typing
// artefact rather than part of the value).
func optionalField(r *http.Request, field string, keep func(string) string) (string, bool) {
	v := keep(r.FormValue(field))
	return v, v != ""
}

func verbatim(s string) string { return s }
func trimmed(s string) string  { return strings.TrimSpace(s) }

// formIDList reads a repeatable id field. Officers get one control per
// person on the form, but a comma-separated single control is accepted too
// so the surface degrades honestly rather than silently dropping entries.
func formIDList(r *http.Request, field string) []string {
	var out []string
	for _, v := range r.Form[field] {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func (s *Server) controlledDestinationIDs() []string {
	var out []string
	for _, d := range s.Cfg.DestinationList() {
		if d.Controlled {
			out = append(out, d.ID)
		}
	}
	return out
}

func (s *Server) uncontrolledDestinationIDs() []string {
	out := []string{} // non-nil: the domain reads nil as "no config" (James, 2026-10-04, R12)
	for _, d := range s.Cfg.DestinationList() {
		if !d.Controlled {
			out = append(out, d.ID)
		}
	}
	return out
}
