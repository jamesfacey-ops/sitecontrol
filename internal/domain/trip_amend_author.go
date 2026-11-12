// trip_amend_author.go — W9a (item 15): the AUTHORING EDGE of the amendment
// family and F4 §3.5's pre-append refusal ladder.
//
// Every refusal here is PRE-APPEND and lives ONLY here (W3-2's rule,
// restated by F4 §3.2): the FOLD never refuses a wire-received amendment,
// because refusing at fold time would fork the two posts' logs permanently.
// A refused amendment authors NOTHING — there is no half-recorded state.
//
// ⚠ THE EDGE IS NOT THE GUARANTEE (§3.3a). A divergent writer reaches the
// side table without passing this file, so composition filters `Clear` and
// occupant ids independently (trip_amend.go). These refusals exist so the
// officer gets a LEGIBLE LOCAL answer, not so the record is safe.
//
// ⚠ REFUSALS ARE TYPED, AND THAT IS LOAD-BEARING. `TripAmendRefusal` is
// AUTHORED COPY an officer may read; `TripAmendInputError` is the officer
// naming a record this post cannot act on; anything else is an internal
// fault whose text must NEVER reach a page (W5d-24's rule, inherited: the
// measured residue of raw `err.Error()` on officer-facing refusals is 24
// sites and must not become 25).
package domain

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"sitecontrol/internal/store"
)

// TripAmendment is the authoring-edge input (SEAM W9A-S-INPUT). All value
// fields are pointers: nil = "this amendment does not touch that fact",
// which is a different thing from asserting an empty value.
type TripAmendment struct {
	TripID          string
	DriverPersonID  *string
	VehicleEntered  *string
	Destination     *DestinationAmend
	CargoNote       *string
	OccupantsAdd    []string
	OccupantsRemove []string
	Clear           []string

	// KnownDestinationSites / KnownUncontrolledPads are the caller's
	// configured destinations — the same shape `DispatchInput` uses, and
	// for the same reason: a raw POST can name a destination no form
	// control would ever offer. Empty means "this caller has no config to
	// check against" and the check is skipped (a replicated amendment never
	// reaches this edge at all).
	//
	// ⚠ James, 2026-10-03 (FIX-PLAN M-4 / L09 T-157): "empty" must therefore
	// never be what a post with `destinations = []` passes. The web edge
	// supplies its configured destinations ∪ its replication peers
	// (knownDestinationSiteIDs), so at NPF/NDB/STP a correction to "banana"
	// is refused and one to a real post is accepted — before, the empty list
	// skipped the check and the nonsense replicated to all five posts.
	KnownDestinationSites []string
	KnownUncontrolledPads []string

	// SiteName renders a site id the way the officer reads it on the caller's
	// pages (the configured display name). It is consulted only when a
	// refusal has to NAME a post — the driver-handover rung below — and nil
	// falls back to SiteLabel, so a raw caller still gets a legible sentence.
	SiteName func(siteID string) string
}

// ArrivalAmendment is the authoring-edge input for the two arrival CLAIMS.
type ArrivalAmendment struct {
	ArrivalID      string
	VehicleEntered *string
	OriginSite     *string

	// KnownOriginSites: the posts an arrival can truthfully have come from
	// (the caller's replication peers). nil = no check (test fixtures).
	// James, 2026-10-04 (R12, FINDINGS-R4 D24): origin_site=banana was
	// recorded; it is now refused in the M-4 wording.
	KnownOriginSites []string
}

// TripAmendRefusal is a FACT-DOMAIN refusal: authored copy, addressed to
// the officer, safe to render.
type TripAmendRefusal struct {
	Reason string
}

func (e *TripAmendRefusal) Error() string {
	return "domain: amend trip: refused — " + e.Reason + "; nothing was recorded"
}

func refuseTripAmend(format string, args ...any) error {
	return &TripAmendRefusal{Reason: fmt.Sprintf(format, args...)}
}

// TripAmendInputError is the officer naming a record this post cannot act
// on — a different mistake with a different next action (fix the id, not
// the amendment), so it is a different type and not a flattened refusal.
type TripAmendInputError struct {
	Reason string
}

func (e *TripAmendInputError) Error() string {
	return "domain: amend trip: " + e.Reason + "; nothing was recorded"
}

// unknownOriginSite is the M-4 rule for an arrival's origin (R12, FINDINGS-R4
// D24): an origin that is not one of the posts this post replicates with
// cannot truthfully be where a vehicle came from. Returns the refusal
// sentence, or "" when origin is known. Shared by the arrival correction
// (AmendArrival) and the unsolicited arrival door (UnsolicitedOriginRefusal)
// so the two cannot drift apart on rule or wording.
func unknownOriginSite(origin string, known []string) string {
	if containsString(known, origin) {
		return ""
	}
	return fmt.Sprintf("%q is not a post this post has an arrival from", origin)
}

// UnsolicitedOriginRefusal is the unsolicited arrival door's half of
// unknownOriginSite (fix round 4, L05 2b: a crafted origin_site=banana was
// RECORDED at every post, though /arrivals/amend already refused it). nil
// when origin is a known post, or empty (the door's own "origin site is
// required" refusal answers that).
//
// FIX5 (R7 L03-04): the sentence closes the way /arrivals/amend's notice
// does — "<reason>. Nothing was recorded." ⟦was: "<reason>; nothing was
// recorded"⟧ — so the two doors read alike past their page's lead word.
func UnsolicitedOriginRefusal(origin string, known []string) error {
	if origin == "" {
		return nil
	}
	if reason := unknownOriginSite(origin, known); reason != "" {
		return fmt.Errorf("domain: unsolicited arrival: %s. Nothing was recorded.", reason)
	}
	return nil
}

func refuseTripAmendInput(format string, args ...any) error {
	return &TripAmendInputError{Reason: fmt.Sprintf(format, args...)}
}

// TripAmendClassB is the class discriminator §4 turns on: the facts that
// drive LIVE MACHINERY (the running timer, which post's scanner watches the
// trip, the correlation key) as against the facts that only describe who
// and what was aboard.
//
// ⚠ STATE THE PROPERTY, DERIVE THE LIST. Class B is not a hand-kept
// enumeration of field names — it is the answer to "does this fact feed the
// deadline scanner's selection or the correlation key?", asked of the
// amendment itself.
func (in TripAmendment) assertsClassB() bool {
	return in.Destination != nil || in.VehicleEntered != nil
}

// assertedFacts lists the fact names this input asserts — the same derived
// vocabulary the side-table row exposes (§6), taken from the input so the
// refusal ladder and the composition can never disagree about what an
// amendment claimed.
func (in TripAmendment) assertedFacts() []string {
	r := tripAmendmentRow{
		TripID: in.TripID, Driver: in.DriverPersonID, Vehicle: in.VehicleEntered,
		Destination: in.Destination, CargoNote: in.CargoNote,
		OccupantsAdd:    in.OccupantsAdd,
		OccupantsRemove: in.OccupantsRemove,
		Cleared:         in.Clear,
	}
	return r.facts()
}

// AmendTrip authors one `trip_amended` event, or refuses PRE-APPEND.
//
// The ladder, in the order F4 §3.5 states it — and the ORDER is part of the
// specification, because each rung's copy tells the officer a different
// working path and the first true rung is the one they read:
//
//  1. the target exists here                (input error — fix the id)
//  2. it asserts something                  (no-op: recording nothing must
//     not look like recording)
//  3. `Clear` names only whitelisted facts  (D-4)
//  4. the DEGENERATE-trip refusals          (the check-in copy)
//  5. the Class-B state gate                (§4; the three renderings are
//     the surface's, this is the
//     backstop)
//  6. every named person is on the roster
//  7. the destination is one this post knows
//  8. the driver is not removed without a replacement (§3.4's legible
//     local refusal — the union is the guarantee)
//  9. a driver correction leaves the PREVIOUS driver accounted for
//     (James, 2026-10-03 — see driverHandover): on an open trip they stay
//     aboard as a passenger in this same amendment, or the correction is
//     refused in words
//
// and two rungs added by ruling R2 (James, 2026-10-04; FINDINGS-R4 D10),
// numbered so the nine above keep their names:
//
//	8a. only the trip's origin or destination may change its driver, or
//	    add anyone aboard (James, 2026-10-04, Q-R5-2)
//	10. on an open trip, every NEWLY named person — a new driver, anyone
//	    put "Aboard after all" — is not already on site anywhere or aboard
//	    another open trip
func AmendTrip(st *store.Store, siteID string, nodeEpoch int64, in TripAmendment, occurredAtMS int64, timeSource string) (store.Envelope, error) {
	if strings.TrimSpace(in.TripID) == "" {
		return store.Envelope{}, refuseTripAmendInput("a trip must be named")
	}
	trip, err := TripByID(st, in.TripID)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: amend trip: %w", err)
	}
	if trip == nil {
		return store.Envelope{}, refuseTripAmendInput("%q is not a trip this post has a record of", in.TripID)
	}

	// --- rung 2: the no-op refusal --------------------------------------
	// A `Clear` naming a word the vocabulary does not know ("driver") asserts
	// no fact, so on its own it would land here and tell the officer they
	// "asserted nothing" when they plainly typed something (L09 T-159). A
	// non-empty Clear is therefore never a no-op: it falls through to rung 3,
	// whose whitelist sentence names the token and says what CAN be un-set.
	if len(in.assertedFacts()) == 0 && len(in.Clear) == 0 {
		return store.Envelope{}, refuseTripAmend("nothing was corrected — an amendment that asserts nothing records nothing, and a recorded nothing is indistinguishable on every later screen from a correction that was never made")
	}

	// --- rung 3: `Clear` is a WHITELIST ---------------------------------
	for _, c := range in.Clear {
		if !clearIsWhitelisted(c) {
			return store.Envelope{}, refuseTripAmend("%q cannot be un-set. Destination, driver and vehicle may be CHANGED, never CLEARED [[D-4]]: a departure must name where it went, and a cleared destination would compose the crashed-check-in shape. Only %s can be un-set",
				c, strings.Join(tripClearWhitelist, " and "))
		}
	}

	// --- rung 4: the degenerate gate check-in trip ----------------------
	// §10's degenerate trip is a walk through the gate, not a journey. A
	// destination assertion on it would say a crew departed for a
	// destination on a "trip" that was a walk through the gate; it would
	// surface in the away sections; and because destination may be CHANGED,
	// NEVER CLEARED it would be UNCORRECTABLE by this design's own rules.
	if isDegenerateTrip(*trip) && (in.Destination != nil || in.VehicleEntered != nil) {
		return store.Envelope{}, refuseTripAmend("this record is a gate check-in, not a trip — it has no destination, no vehicle journey and no timer, so destination and vehicle cannot be corrected on it. A check-in is corrected on the visit side (re-check-in with the true time, or a ticked checkout), not here. Who was recorded on it — the driver and the occupants — can still be corrected below")
	}

	// --- rung 5: the Class-B state gate (§4) ----------------------------
	if in.assertsClassB() {
		state, serr := TripStateByID(st, in.TripID)
		if serr != nil {
			return store.Envelope{}, fmt.Errorf("domain: amend trip: %w", serr)
		}
		if state == nil || state.State == TripStateOpen {
			return store.Envelope{}, refuseTripAmend("this trip is still open, and the destination, the deadline and the vehicle drive live machinery while it is — the running countdown, which post's board watches it, and the correlation that binds an arrival to it. Correcting them record-only would make the record disagree with the machinery still running on the original values. They can be corrected once the trip is completed")
		}
	}

	// --- rung 6: every named person is on the roster --------------------
	named := append([]string(nil), in.OccupantsAdd...)
	named = append(named, in.OccupantsRemove...)
	if in.DriverPersonID != nil {
		named = append(named, *in.DriverPersonID)
	}
	for _, id := range named {
		ok, rerr := occupantIDIsOnRoster(st, id)
		if rerr != nil {
			return store.Envelope{}, fmt.Errorf("domain: amend trip: %w", rerr)
		}
		if !ok {
			return store.Envelope{}, refuseTripAmendInput("%q is not a person on this post's roster", id)
		}
	}

	// --- rung 7: the destination is one this post knows -----------------
	if in.Destination != nil {
		if derr := validateDestinationAmend(*in.Destination, in); derr != nil {
			return store.Envelope{}, derr
		}
	}

	// --- rung 8: the driver is not removed without a replacement --------
	// §3.4's legible local refusal. The structural union is the guarantee;
	// this exists so the officer is told here, in words, rather than
	// discovering at read time that the removal was overridden.
	// The EFFECTIVE driver, not the raw row's (2026-10-03, found by W2's
	// T-164 test): ⟦was: `driver := trip.DriverPersonID`⟧ compared against
	// the dispatch-time driver, so once the driver had been corrected the
	// superseded driver could never be removed on their own — "X is the
	// recorded driver" about a person the composition no longer treats as
	// one. The structural union composes from the effective driver; this
	// rung must ask the same question or it refuses what the union allows.
	driver := trip.DriverPersonID
	effectiveTrip, effectiveOccupants := *trip, []string(nil)
	if c, cerr := EffectiveTrip(st, trip.TripID); cerr != nil {
		return store.Envelope{}, fmt.Errorf("domain: amend trip: %w", cerr)
	} else if c != nil {
		driver = c.Trip.DriverPersonID
		effectiveTrip, effectiveOccupants = c.Trip, c.OccupantIDs
	}
	siteName := func(id string) string {
		if in.SiteName != nil {
			return in.SiteName(id)
		}
		return SiteLabel(id)
	}
	if in.DriverPersonID != nil {
		driver = *in.DriverPersonID
	}
	for _, id := range in.OccupantsRemove {
		if id == driver {
			// Named, not quoted by id (L09 T-146): every other sentence on
			// the surface names people.
			return store.Envelope{}, refuseTripAmend("%s is the recorded driver of this trip, and the driver is always aboard. Name the person who actually drove in the same correction, and the removal will stand", personNameOrID(st, id))
		}
	}

	// --- rung 8a: only the trip's own two ends may change its driver or add aboard
	// James, 2026-10-04: only the origin or the destination may hand over a
	// driver, not a third post such as npf (ruling R2; L13 D-96 open
	// question). A third post never saw the truck leave or arrive, so it
	// cannot know who drove. The other trip facts stay D-96 shared. A trip
	// with no destination post (a gate check-in, road work, an uncontrolled
	// run) has one end: its origin.
	//
	// James, 2026-10-04: only the trip's origin or destination may add
	// someone aboard (occupants_add, "Aboard after all"), the same rule as a
	// driver change (Q-R5-2; L14-1 — npf could add a crew member to an
	// iceroad→pad trip). The R2 reason applies equally to who was aboard. A
	// third post is refused in plain words and nothing is recorded.
	if in.DriverPersonID != nil || len(in.OccupantsAdd) > 0 {
		ends := []string{effectiveTrip.OriginSite}
		if effectiveTrip.DestinationSite != nil && *effectiveTrip.DestinationSite != effectiveTrip.OriginSite {
			ends = append(ends, *effectiveTrip.DestinationSite)
		}
		if !containsString(ends, siteID) {
			labels := make([]string, len(ends))
			for i, e := range ends {
				labels[i] = siteName(e)
			}
			what, who := "who drove this trip", "who was driving"
			if in.DriverPersonID == nil {
				what, who = "who was aboard this trip", "who was aboard"
			}
			return store.Envelope{}, refuseTripAmend("only %s may change %s — the post%s that saw the vehicle leave or come in. Ask %s to make this correction; a post the trip never passed through cannot know %s", strings.Join(labels, " or "), what, plural(len(ends)), strings.Join(labels, " or "), who)
		}
	}

	// --- rung 9: the previous driver stays accounted for ----------------
	// James, 2026-10-03: correcting an open trip's driver must never make
	// the previous driver vanish from every muster. The kept-aboard add is
	// folded INTO THIS amendment — one event, one payload — so both posts
	// compose it identically and no interleaving can separate the two facts.
	if in.DriverPersonID != nil {
		keep, kerr := driverHandover(st, *trip, in)
		if kerr != nil {
			return store.Envelope{}, kerr
		}
		if keep != "" {
			in.OccupantsAdd = append(append([]string(nil), in.OccupantsAdd...), keep)
		}
	}

	// --- rung 10: every NEWLY named person is not already elsewhere ------
	// (newlyNamedAccountedFor; James, 2026-10-04, ruling R2.)
	if err := newlyNamedAccountedFor(st, *trip, in, effectiveOccupants, siteName); err != nil {
		return store.Envelope{}, err
	}

	// The `seen` stamp ([[D-92]], SchemaVersion 11): every fact this
	// amendment asserts — the scalar facts and each `occupant/<id>` member
	// fact, the same derived vocabulary the ladder used — resolved over the
	// record set THIS post holds for this trip right now, after every rung
	// above and before Author. amendmentSeen (contest.go) is the one
	// spelling; the scope is the trip's own rows (nil merge map: Resolve is
	// the identity), which is F4's contest scope. TripAmendment has no field
	// a caller could supply it through.
	rows, err := readTripAmendmentsFor(st, in.TripID)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: amend trip: %w", err)
	}
	seen := amendmentSeen(rows, nil, in.TripID, in.assertedFacts())

	payload, err := json.Marshal(TripAmended{
		Type: evtTripAmended, TripID: in.TripID,
		DriverPersonID: in.DriverPersonID,
		// NO TrimSpace on the vehicle: the entered text is stored VERBATIM
		// (M8), exactly as dispatch stores it — comparison alone normalises.
		VehicleEntered:  in.VehicleEntered,
		Destination:     in.Destination,
		CargoNote:       in.CargoNote,
		OccupantsAdd:    in.OccupantsAdd,
		OccupantsRemove: in.OccupantsRemove,
		Clear:           in.Clear,
		Seen:            seen,
	})
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: amend trip: %w", err)
	}
	env, err := st.Author(siteID, nodeEpoch, payload, occurredAtMS, timeSource)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: amend trip: %w", err)
	}
	return env, nil
}

// isDegenerateTrip is §10's shape, asked as a property rather than
// remembered as a list of three field names in three places: no controlled
// destination, no pad, no deadline.
func isDegenerateTrip(t Trip) bool {
	return t.DestinationSite == nil && t.DestinationUncontrolled == nil && t.DeadlineMinutes == nil
}

// driverHandover is rung 9 — James, 2026-10-03: "correcting an open trip's
// driver must never make the original driver vanish from every muster".
//
// WHY A BARE DRIVER CORRECTION USED TO DO EXACTLY THAT. composeOccupants
// derives the driver's membership from the EFFECTIVE driver (W9a-14's
// arithmetic), so `driver: B` alone composed to {B, passengers} — the
// previous driver A was on no sheet at either post: the origin's in-transit
// block no longer listed them, the destination's expectation no longer
// counted them, and A had no visit anywhere (dispatch closed it). Measured
// live on the fork (CAMPAIGN-NOTES, fork/truck §3.4 note): Ice Road's muster
// dropped the trip while Ice Pad still showed it overdue.
//
// WHAT THIS RUNG DOES. It returns the id to fold into THIS amendment's
// `occupants_add` — the previous effective driver, kept aboard as a
// passenger — or "" when nothing needs adding, or a refusal when keeping
// them aboard would put them on a sheet twice or assert something the trip
// can no longer carry. It never adds behind the officer's back when the
// officer has already said what happened to A: an explicit `occupants_add`
// or `occupants_remove` naming A is the officer's own statement and stands
// (the removal still has to pass the web edge's [[D-80]] unsighted gate).
//
// THE THREE REFUSALS, each a plain sentence with the way out in it:
//
//   - A has an OPEN VISIT anywhere: they are on that post's muster already,
//     so keeping them aboard too would show them twice. The sentence names
//     the post and says to put A in "Remove from the trip's list" in the
//     same correction if they were never aboard — A is sighted, so D-80
//     lets that removal through and A stays on exactly one sheet.
//   - A is aboard ANOTHER OPEN TRIP: already on that run's sheet; the
//     sentence names the vehicle and the trip.
//   - the trip is CLOSED (arrived): nothing "kept aboard" can be derived
//     — whoever rode arrived, or stayed behind, and the record cannot say
//     which. The driver is still correctable (§4: Class A, any time; the
//     incident read needs it), but the officer must say in the same
//     correction what happened to A: "Aboard after all" or "Remove". Without
//     that, A would silently leave NOT TRACKED, which is the same vanishing
//     on a different sheet.
//
// Deterministic at every post and under replay: the decision is made ONCE,
// here, before Author, and what replicates is a plain amendment carrying
// both facts — the fold and the composition are untouched.
func driverHandover(st *store.Store, trip Trip, in TripAmendment) (string, error) {
	next := strings.TrimSpace(*in.DriverPersonID)
	c, err := EffectiveTrip(st, trip.TripID)
	if err != nil {
		return "", fmt.Errorf("domain: amend trip: %w", err)
	}
	prev := trip.DriverPersonID
	if c != nil {
		prev = c.Trip.DriverPersonID
	}
	if prev == "" || prev == next {
		return "", nil
	}
	if containsString(in.OccupantsAdd, prev) || containsString(in.OccupantsRemove, prev) {
		return "", nil // the officer said what happened to them
	}
	name := personNameOrID(st, prev)
	siteName := func(id string) string {
		if in.SiteName != nil {
			return in.SiteName(id)
		}
		return SiteLabel(id)
	}

	state, err := TripStateByID(st, trip.TripID)
	if err != nil {
		return "", fmt.Errorf("domain: amend trip: %w", err)
	}
	if state == nil || state.State != TripStateOpen {
		return "", refuseTripAmend("%s is the recorded driver of this trip and the trip has already arrived, so this correction cannot keep them aboard on its own. Say in the same correction what happened to them: put %s in \"Aboard after all\" if they rode as a passenger, or in \"Remove from the trip's list\" if they were never aboard", name, name)
	}

	where, err := accountedForElsewhere(st, prev, trip.TripID, siteName)
	if err != nil {
		return "", err
	}
	switch {
	case where.onSitePost != "":
		return "", refuseTripAmend("%s is the recorded driver of this trip but is recorded on site at %s%s, so they cannot be kept aboard it as well. If they were never aboard, name the person who drove and put %s in \"Remove from the trip's list\" in the same correction — they stay on %s's muster by their visit", name, where.onSitePost, where.since, name, where.onSitePost)
	case where.otherTripID != "":
		return "", refuseTripAmend("%s is the recorded driver of this trip but is also recorded aboard %s (trip %s, %s), so they cannot be kept aboard here as well. Correct that trip's record first, or wait for it to arrive", name, where.otherVehicle, where.otherTripID, where.otherRoute)
	}
	return prev, nil
}

// elsewhere is where a person is ALREADY accounted for, other than on the
// trip being corrected: an open visit (on that post's muster) or another
// open trip's effective list (on that run's sheet). Zero value: nowhere.
type elsewhere struct {
	onSitePost, since                     string
	otherTripID, otherVehicle, otherRoute string
}

// accountedForElsewhere is the "already accounted for" question rung 9
// asks of the previous driver and rung 10 asks of every newly named person
// (FINDINGS-R4 D10, ruling R2) — one spelling, so the two rungs can never
// disagree about where a person is.
func accountedForElsewhere(st *store.Store, personID, tripID string, siteName func(string) string) (elsewhere, error) {
	// An open visit anywhere: on a muster already.
	visits, err := dumpSectionWhere[Visit](st, "visit",
		store.Eq{Column: "person_id", Value: personID}, store.Eq{Column: "is_open", Value: 1})
	if err != nil {
		return elsewhere{}, fmt.Errorf("domain: amend trip: %w", err)
	}
	if len(visits) > 0 {
		v := visits[0]
		since := ""
		if et, found, terr := st.EventTimeAt(store.Key{SiteID: v.SiteID, NodeEpoch: v.NodeEpoch, Seq: v.Seq}); terr == nil && found {
			since = " since " + time.UnixMilli(et.AuthoredAtMS).In(time.Local).Format("15:04")
		}
		return elsewhere{onSitePost: siteName(v.SiteID), since: since}, nil
	}

	// Aboard another open trip: on that run's sheet already.
	open, err := openTripStates(st)
	if err != nil {
		return elsewhere{}, fmt.Errorf("domain: amend trip: %w", err)
	}
	for _, s := range open {
		if s.TripID == tripID {
			continue
		}
		other, oerr := EffectiveTrip(st, s.TripID)
		if oerr != nil {
			return elsewhere{}, fmt.Errorf("domain: amend trip: %w", oerr)
		}
		if other == nil || !containsString(other.OccupantIDs, personID) {
			continue
		}
		live, lerr := NewLiveVehicles(st)
		if lerr != nil {
			return elsewhere{}, fmt.Errorf("domain: amend trip: %w", lerr)
		}
		vehicle, _, verr := live.Compose(*other)
		if verr != nil {
			return elsewhere{}, fmt.Errorf("domain: amend trip: %w", verr)
		}
		route := siteName(other.Trip.OriginSite)
		if other.Trip.DestinationSite != nil {
			route += " to " + siteName(*other.Trip.DestinationSite)
		}
		return elsewhere{otherTripID: other.Trip.TripID, otherVehicle: vehicle, otherRoute: route}, nil
	}
	return elsewhere{}, nil
}

// newlyNamedAccountedFor is rung 10. James, 2026-10-04: refuse a new
// driver or added occupant who has an open visit anywhere or is on another
// open trip (ruling R2, FINDINGS-R4 D10). Rung 9 asked that question of
// the PREVIOUS driver only; a NEW driver, or anyone put "Aboard after all",
// who was already on a muster or another truck's list was accepted and then
// listed twice (L09 NEW-b-DOUBLE, L17-18) — and, now that an arrival checks
// in the EFFECTIVE occupants (D1), would have been given a second open visit
// (L16-05a). Asked only on an OPEN trip: a closed trip's list is history,
// and the person has since moved on through the gate like anyone else.
//
// "Newly named" is a person this amendment puts on the list who is not on
// the effective list already; the previous driver rung 9 folds in is on it.
func newlyNamedAccountedFor(st *store.Store, trip Trip, in TripAmendment, effective []string, siteName func(string) string) error {
	state, err := TripStateByID(st, trip.TripID)
	if err != nil {
		return fmt.Errorf("domain: amend trip: %w", err)
	}
	if state != nil && state.State != TripStateOpen {
		return nil
	}
	var named []string
	if in.DriverPersonID != nil {
		named = append(named, strings.TrimSpace(*in.DriverPersonID))
	}
	named = append(named, in.OccupantsAdd...)
	checked := map[string]bool{}
	for _, id := range named {
		if id == "" || checked[id] || containsString(effective, id) {
			continue
		}
		checked[id] = true
		where, err := accountedForElsewhere(st, id, trip.TripID, siteName)
		if err != nil {
			return err
		}
		name := personNameOrID(st, id)
		switch {
		case where.onSitePost != "":
			return refuseTripAmend("%s is recorded on site at %s%s, so they cannot also be put aboard this trip — they would be on two sheets at once. If they really are on this truck, check them out at %s's gate first, then make this correction", name, where.onSitePost, where.since, where.onSitePost)
		case where.otherTripID != "":
			return refuseTripAmend("%s is already recorded aboard %s (trip %s, %s), so they cannot be put aboard this trip as well — they would be on two trucks' lists at once. Take them off that trip's list first, or wait for it to arrive", name, where.otherVehicle, where.otherTripID, where.otherRoute)
		}
	}
	return nil
}

// personNameOrID is the refusal sentences' name resolution: the roster
// name when this post holds one, the id otherwise — never a blank.
func personNameOrID(st *store.Store, personID string) string {
	p, err := PersonByID(st, personID)
	if err != nil || p == nil || p.Name == "" {
		return personID
	}
	return p.Name
}

// validateDestinationAmend enforces the compound fact's own invariant
// (§3.4): controlled REQUIRES a deadline, uncontrolled REFUSES one, and the
// two kinds are mutually exclusive because the fact carries exactly one
// kind. Validated WHOLE at authoring, which is what lets composition pick
// one whole assertion and stay coherent under every interleaving.
func validateDestinationAmend(d DestinationAmend, in TripAmendment) error {
	switch d.Kind {
	case "controlled":
		if d.Value == "" {
			return refuseTripAmend("a controlled destination must name the post the vehicle is going to")
		}
		if len(in.KnownDestinationSites) > 0 && !containsString(in.KnownDestinationSites, d.Value) {
			return refuseTripAmendInput("%q is not a destination this post is configured for", d.Value)
		}
		if d.DeadlineMinutes == nil || *d.DeadlineMinutes <= 0 {
			return refuseTripAmend("a controlled destination must carry the travel allowance in minutes — the countdown the destination's board runs is the whole point of a controlled run")
		}
	case "uncontrolled":
		if d.Value == "" {
			return refuseTripAmend("an uncontrolled run must name the pad")
		}
		// James, 2026-10-04 (R12, D24): free text stays allowed, but it is
		// checked against the known pad list in the M-4 wording. nil = no
		// config to check against; the web edge passes non-nil, so a post
		// with no configured pads refuses "banana" instead of replicating it.
		if in.KnownUncontrolledPads != nil && !containsString(in.KnownUncontrolledPads, d.Value) {
			return refuseTripAmendInput("%q is not a pad this post is configured for", d.Value)
		}
		if d.DeadlineMinutes != nil {
			return refuseTripAmend("an uncontrolled run carries no travel allowance — there is no gate at the far end to check the vehicle in, so a countdown against it would be a guaranteed false alarm")
		}
	default:
		return refuseTripAmend("a destination correction must say whether the destination is a controlled post or an uncontrolled pad")
	}
	return nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func containsString(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// AmendArrival authors one `arrival_amended` event, or refuses PRE-APPEND.
// It corrects what an unsolicited arrival's officer TYPED — the vehicle and
// the origin they claimed — and nothing else: §5 rule 4 makes the
// correction RECORD-ONLY, so it never re-runs correlation, never binds and
// never un-binds. The un-reference is a separate event (§3.6, item 16).
func AmendArrival(st *store.Store, siteID string, nodeEpoch int64, in ArrivalAmendment, occurredAtMS int64, timeSource string) (store.Envelope, error) {
	if strings.TrimSpace(in.ArrivalID) == "" {
		return store.Envelope{}, refuseTripAmendInput("an arrival must be named")
	}
	a, err := rawArrivalByID(st, in.ArrivalID)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: amend arrival: %w", err)
	}
	if a == nil {
		return store.Envelope{}, refuseTripAmendInput("%q is not an arrival this post has a record of", in.ArrivalID)
	}
	if in.OriginSite != nil && in.KnownOriginSites != nil {
		if reason := unknownOriginSite(*in.OriginSite, in.KnownOriginSites); reason != "" {
			return store.Envelope{}, refuseTripAmendInput("%s", reason)
		}
	}
	if in.VehicleEntered == nil && in.OriginSite == nil {
		return store.Envelope{}, refuseTripAmend("nothing was corrected — an amendment that asserts nothing records nothing")
	}
	// The `seen` stamp ([[D-92]], SchemaVersion 11), as AmendTrip's: the
	// heads of each asserted claim's contest over this arrival's own rows.
	rows, err := readArrivalAmendmentsFor(st, in.ArrivalID)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: amend arrival: %w", err)
	}
	var facts []string
	if in.VehicleEntered != nil {
		facts = append(facts, factArrivalVehicle)
	}
	if in.OriginSite != nil {
		facts = append(facts, factArrivalOrigin)
	}
	seen := amendmentSeen(rows, nil, in.ArrivalID, facts)

	payload, err := json.Marshal(ArrivalAmended{
		Type: evtArrivalAmended, ArrivalID: in.ArrivalID,
		VehicleEntered: in.VehicleEntered, OriginSite: in.OriginSite,
		Seen: seen,
	})
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: amend arrival: %w", err)
	}
	env, err := st.Author(siteID, nodeEpoch, payload, occurredAtMS, timeSource)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: amend arrival: %w", err)
	}
	return env, nil
}

// UnsightedRemovals is §7's warning predicate: of the people this
// amendment would REMOVE from a trip's occupant set, which have had no
// recorded return reach this post.
//
// The hazard is one-directional and the design chooses a WARNING, not a
// refusal, deliberately: refusing the removal unless the person is
// accounted for elsewhere would refuse TRUE corrections — the
// refusing-to-record-reality defect class. And it is CONDITIONAL, because a
// warning on every removal trains officers to click past the one that
// matters (the cry-wolf control, tested).
//
// "Sighted" is what THIS POST can honestly claim about a NAMED human: an
// open visit anywhere in this replica, or a visit this trip's own arrival
// created. Everything else is "no return has reached this post" — the
// conservative direction, the same asymmetry the away views take.
func UnsightedRemovals(st *store.Store, tripID string, personIDs []string) ([]OccupantRef, error) {
	if len(personIDs) == 0 {
		return nil, nil
	}
	arrived := map[string]bool{}
	visits, err := VisitsByTrip(st, tripID)
	if err != nil {
		return nil, err
	}
	for _, v := range visits {
		arrived[v.PersonID] = true
	}
	var out []OccupantRef
	seen := map[string]bool{}
	for _, id := range personIDs {
		if seen[id] || arrived[id] {
			continue
		}
		seen[id] = true
		open, verr := dumpSectionWhere[Visit](st, "visit", store.Eq{Column: "person_id", Value: id})
		if verr != nil {
			return nil, verr
		}
		sighted := false
		for _, v := range open {
			if v.IsOpen == 1 {
				sighted = true
				break
			}
		}
		if sighted {
			continue
		}
		name := id
		p, perr := PersonByID(st, id)
		if perr != nil {
			return nil, perr
		}
		if p != nil && p.Name != "" {
			name = p.Name
		}
		out = append(out, OccupantRef{PersonID: id, Name: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
