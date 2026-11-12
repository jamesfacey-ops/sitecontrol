// tripauthor.go — authoring-time API for the trip event family, plus the
// projection reads the web layer (C6 forms/board) and later chunks (C7
// scanner, C8 gate, C9 muster) consume. Authoring-time refusals here are
// business-rule checks against locally-known state (the CreateTrade
// pattern) — a refusal never calls Author, nothing is written to the log.
package domain

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"sitecontrol/internal/store"
)

// DispatchInput is the dispatch form (design §10: one form, one trip, N
// occupant rows, occupants checked out of the origin's visit set by the
// fold). DestinationSite and DeadlineMinutes are BOTH nil for the degenerate
// local check-in trip (§10) — C8's gate check-in authors exactly that
// through DispatchLocalCheckIn, and NOTHING ELSE may (D-4); C7's deadline
// scanner and replay gate skip deadline-less trips.
type DispatchInput struct {
	// ⟦W7, item 9 — DR-W7-4, W7-4(d)(i)⟧ VehicleID names the vehicle RECORD
	// this run goes out on — "a thing the system knows, selected rather than
	// retyped" ([[D-12]]) — and it is what lands in trip.vehicle_id and in the
	// payload. trip.vehicle_entered is DERIVED from the composed record's own
	// number (dispatchVehicleSnapshot below); NOTHING here carries a number.
	//
	// ⚠⚠ THERE IS DELIBERATELY NO FREE-TEXT VEHICLE FIELD ON THIS STRUCT, AND
	// RE-ADDING ONE UNDOES DR-W7-4. The ruling is that no trip-authoring entry
	// point takes a caller-supplied vehicle STRING, and the guarantee it buys
	// is that "the typing moment IS creation": the ONE place a number can be
	// typed is vehicle creation, which is the ONE place the placeholder list
	// is consulted (refuseIfPlaceholderNumber, vehicle.go). That is what makes
	// the placeholder guard's move a RELOCATION rather than a removal, which
	// is what [[D-11]] requires in terms. A `VehicleEntered string` restored
	// here — under any spelling, and a named string type counts — makes the
	// move a removal again, silently, and D-11 forbids it.
	// TestW7DispatchInputTakesAVehicleIDNotAString is the census that says so.
	VehicleID string
	// ⟦TRIAL — §97, 2026-09-03⟧ ResolveVehicle mints-or-resolves the vehicle record and is called
	// AFTER every validation, immediately before authoring. nil means today's behaviour: the caller
	// supplies VehicleID itself. ⚠ A FUNC, NOT A STRING, deliberately: DR-W7-4 bars a
	// caller-supplied vehicle STRING on this struct under any spelling, and
	// TestW7DispatchInputTakesAVehicleIDNotAString enforces it. ⟦was: "`PhaseGate.Allowed` is the
	// precedent for a behavioural field here" — PhaseGate removed with weather phases, James 2026-10-04⟧
	ResolveVehicle func() (string, error)
	// VehicleUnidentified (SchemaVersion 13, R-17 — James: "there are no
	// doors only vehicles"): an explicit, decided fact that no vehicle
	// could be named for this movement — never blended with VehicleID or
	// ResolveVehicle (dispatchTrip refuses the combination; see its own
	// comment). The zero value (false) is every existing caller's
	// behaviour, unchanged. Covers both doors this struct already serves: a
	// departure (Dispatch) and the degenerate arrival trip
	// (DispatchLocalCheckIn, GateCheckIn's core) — the one place both
	// movements' vehicle naming passes through, per James's framing that
	// there is one vehicle concept, not one per route.
	VehicleUnidentified bool
	DriverPersonID      string
	OccupantPersonIDs   []string // driver auto-included if absent (§7)
	DestinationSite     *string
	// DestinationUncontrolled: a run to a configured pad with no gate (see
	// TripDispatched.DestinationUncontrolled, trip.go). Mutually exclusive
	// with DestinationSite. The deadline rule is enforced in BOTH directions
	// below: a controlled destination REQUIRES a deadline (F4's protection —
	// no unwatchable promises), an uncontrolled one MUST NOT carry one (a
	// deadline nobody can close is a guaranteed false alarm) — refused, not
	// ignored, so the two paths cannot blur into one form with a forgettable
	// field.
	DestinationUncontrolled *string
	DeadlineMinutes         *int64
	// RoadWork (James, 2026-10-03) sends this vehicle out WORKING ON THE
	// ROAD — the trip KIND described in road_work.go. Requires BOTH
	// destinations nil and DeadlineMinutes SET (the caller passes this post's
	// configured working limit, config.RoadWorkLimitMinutes); dispatchTrip
	// refuses every other combination so the kind can never be half-recorded,
	// and refuses it entirely through the check-in door.
	RoadWork bool
	// CargoNote (item 3) is the officer's own words about what is on the
	// truck ONCE A SURFACE COLLECTS THEM (C-7/c10) — no dispatch caller
	// populates this field today, so it is uniformly nil in the shipping
	// binary — carried VERBATIM into the payload and the row, see
	// TripDispatched.CargoNote (trip.go). It is a RECORDED FIELD, NOT A
	// RULE: there is deliberately no refusal for it anywhere below. An
	// empty note, a whitespace note and an eight-kilobyte note are all
	// lawful records of what the officer chose to write, and nil simply
	// means they wrote nothing. Refusing a departure over the note would
	// leave the truck that is leaving anyway with no trip, no timer and no
	// muster trail. Nothing branches on its content (D-21.3).
	CargoNote  *string
	RecordedBy *string

	// ValidateRoster and KnownDestinationSites (F4 + F11 — one root cause,
	// two symptoms, fixed as one thing): Dispatch is the one authoring path
	// that does NOT run through resolvePersonSlot (checkin.go), so it never
	// checked that a person id actually exists, or that a destination
	// string names a real peer. Both were reproduced live over real HTTP: a
	// garbage driver_person_id lands as a trip that can never complete via
	// arrival (409 "not on the roster" at the destination) and becomes a
	// permanent false OVERDUE once its deadline passes; a typo'd
	// destination ("site-bee") authors fine and matches no site's
	// OpenExpectations — dispatched, a deadline promised, scanned nowhere,
	// alerted never.
	//
	// Both checks are OFF BY DEFAULT (nil/false, the zero value) — this is
	// SOLELY FOR TEST-FIXTURE COMPATIBILITY, not a judgment that unvalidated
	// input is ever acceptable in production. ~37 existing Dispatch() call
	// sites across domain/web/deadline inject synthetic ids/sites to build a
	// scenario without seeding a full roster or peer config (internal/deadline
	// in particular, owned by a different chunk); an unconditional check
	// would break every one of them for no safety gain, since none is
	// reachable from an actual HTTP request.
	//
	// ⚠ EVERY CALLER REACHABLE FROM A REQUEST MUST SET BOTH. Leaving either
	// off on a request path silently reopens F11 (unknown person ids) / F4
	// (unknown destinations) one layer down. As of this fix the only such
	// caller is web/trips.go handleDispatch (POST /trips/dispatch — the one
	// endpoint reachable by a raw/malformed POST, since the UI's own
	// <select> elements only ever submit valid ids/known destinations),
	// which sets both. GateCheckIn's own internal Dispatch call (checkin.go,
	// POST /gate/checkin — also request-reachable) is the one legitimate
	// exception: it has ALREADY resolved/created every person id through
	// resolvePersonSlot moments earlier in the SAME request and never sets a
	// destination at all (§10's degenerate local trip), so re-validating
	// here would be redundant, not a gap (it reaches the shared core through
	// DispatchLocalCheckIn, not Dispatch). Any FUTURE request-reachable
	// caller of Dispatch must set both unless it can make the same
	// already-validated argument GateCheckIn does. As of D-4 the second
	// request-reachable Dispatch caller is web/gate.go
	// handleGateVehicleCheckOut (POST /gate/checkout/vehicle), which also
	// sets all three.
	ValidateRoster        bool
	KnownDestinationSites []string // nil: no check; non-nil (incl. empty): DestinationSite must be a member

	// KnownUncontrolledDestinations is the F4 analogue for the uncontrolled
	// list (config.UncontrolledDestinationIDs): nil = no check (test-fixture
	// compatibility, same posture as the two fields above); non-nil (incl.
	// empty) = DestinationUncontrolled must be a member. The same ⚠ applies:
	// every caller reachable from a request must set it.
	KnownUncontrolledDestinations []string

	// RecordChanges ⟦[[D-84]] / A3 H-5, 2026-09-29⟧: the officer-CONFIRMED
	// corrections to KNOWN people's company/trade/position/phone that ride a
	// DEPARTURE — the outbound vehicle form renders the same row as the
	// inbound one and honours the same tick. The same field
	// GateCheckInInput carries, for the same reason it lives INSIDE the door:
	// they are applied (ApplyPersonRecordChanges) only after every refusal
	// dispatchTrip can produce has been ruled out and the vehicle has
	// resolved, immediately before the trip is authored. ⟦was: applied by
	// web/gate.go's handleGateVehicleCheckOut BEFORE calling Dispatch — so a
	// departure Dispatch then refused (blank deadline, an unknown
	// destination) had already amended the person's permanent record, and
	// replicated it, under "Nothing was recorded" (FINDINGS H-5; L03
	// G-079(b))⟧. Nil for every existing caller — nothing to apply.
	RecordChanges []PersonRecordChange
}

// requireRosterPerson refuses if personID does not resolve on the roster —
// the same existence check resolvePersonSlot (checkin.go) already performs
// for every other authoring path. Dispatch calls this only when
// DispatchInput.ValidateRoster is set (F11).
func requireRosterPerson(st *store.Store, personID string) error {
	p, err := PersonByID(st, personID)
	if err != nil {
		return err
	}
	if p == nil {
		return fmt.Errorf("a person named on this request is not on the roster")
	}
	return nil
}

// Dispatch authors one immutable trip event on the origin site's own stream:
// a DEPARTURE. Under D-4 every departure names where it went, and Dispatch
// refuses one that does not — see dispatchTrip's D-4 block for the whole
// reasoning. The destination-less (§10 degenerate) trip is authored through
// the OTHER door, DispatchLocalCheckIn, and only ever as the first half of
// an arrival.
func Dispatch(st *store.Store, siteID string, nodeEpoch int64, in DispatchInput, occurredAtMS int64, timeSource string) (Trip, error) {
	return dispatchTrip(st, siteID, nodeEpoch, in, false, occurredAtMS, timeSource)
}

// DispatchLocalCheckIn authors the §10 degenerate trip — no destination, no
// deadline — which is the FIRST HALF OF A LOCAL CHECK-IN and nothing else.
// It is deliberately a separate exported function rather than a field on
// DispatchInput: a boolean travels inside a struct literal and can be
// copy-pasted onto a departure without anybody reading it, whereas the name
// of the function is at the call site and says what is being authored.
//
// ⚠ IT AUTHORS ONLY THE DISPATCH HALF. On its own it leaves an open,
// never-arrived degenerate trip — which is precisely the shape
// IncompleteCheckIns treats as a CRASHED CHECK-IN, offering a Resume button
// that puts those people (back) onto the muster, and which makes the printed
// muster read MAY BE INCOMPLETE. The caller MUST author the arrival
// immediately; GateCheckIn (checkin.go) is the only production caller and
// does exactly that. Calling this from any check-out or departure path
// recreates the D-4 defect in full, and
// TestOnlyTheCheckInPathAuthorsALocalCheckInDispatch refuses to let that
// happen quietly.
//
// It refuses a destination or a deadline, so the two doors are mutually
// exclusive: Dispatch will not author a trip with no destination, and this
// will not author one with a destination.
func DispatchLocalCheckIn(st *store.Store, siteID string, nodeEpoch int64, in DispatchInput, occurredAtMS int64, timeSource string) (Trip, error) {
	if in.DestinationSite != nil || in.DestinationUncontrolled != nil {
		return Trip{}, fmt.Errorf("domain: local check-in: a local check-in trip has no destination — it is the arrival half of somebody being checked in here, not a departure; author a departure with Dispatch")
	}
	if in.DeadlineMinutes != nil {
		return Trip{}, fmt.Errorf("domain: local check-in: a local check-in trip has no deadline — nobody is travelling anywhere and there is nothing for a timer to close")
	}
	// James, 2026-10-03: the road-work kind is a DEPARTURE (equipment leaves
	// this gate for the road) and only Dispatch may author one. Through this
	// door it would be a check-in half that also claims to have left — two
	// different facts in one event, and the arrival the caller authors next
	// would close it immediately as if the equipment had come straight back.
	if in.RoadWork {
		return Trip{}, fmt.Errorf("domain: local check-in: a local check-in trip is not road work — equipment going out to work on the road is a departure; author it with Dispatch")
	}
	return dispatchTrip(st, siteID, nodeEpoch, in, true, occurredAtMS, timeSource)
}

// dispatchTrip is the shared authoring core behind both doors. localCheckIn
// is NOT a caller-supplied field — it is fixed by which exported function
// was called.
func dispatchTrip(st *store.Store, siteID string, nodeEpoch int64, in DispatchInput, localCheckIn bool, occurredAtMS int64, timeSource string) (Trip, error) {
	// ⟦W7, item 9⟧ THE NAMES-AN-ENTITY REFUSAL, RE-POINTED AT THE ID AND NOT
	// DELETED, with its literal preserved BYTE-FOR-BYTE. The sentence is still
	// true of the new guard, and preserving it keeps the locked census repair a
	// one-entry deletion instead of a re-authoring.
	//
	// ⚠ WHAT A BUILD THAT READ "remove the refusals" LITERALLY WOULD PRODUCE:
	// SQLite accepts "" for a NOT NULL TEXT column, so a trip would be written
	// with NO VEHICLE AT ALL and the muster would render an empty element on
	// the very section [[D-36]] kept the vehicle for. What moves to vehicle
	// creation is the PLACEHOLDER refusal; the names-an-entity refusal STAYS.
	// ⟦TRIAL — §97⟧ ⚠ THE REFUSAL IS NOT WEAKENED, IT IS DEFERRED. A caller with a resolver has no
	// id YET; one that supplies neither is still refused HERE, and one whose resolver yields nothing
	// is refused BELOW by the identical literal. Both paths are produced in the report.
	//
	// ⚠ SchemaVersion 13 (R-17, "there are no doors only vehicles"): a THIRD
	// option alongside "named" and "not yet resolved" — VehicleUnidentified,
	// an explicit, decided fact that no vehicle exists to name for this
	// movement. Refused ALONGSIDE a name or a resolver, never blended with
	// one: a caller cannot both name a vehicle (or ask for one to be
	// resolved) and mark it unidentified, because that pair means two
	// different things at once and there is no rule for which one wins.
	// This is the SAME impossibility the store's CHECK constraint on
	// trip.vehicle_unidentified enforces one layer down — refused here so
	// the officer sees a sentence instead of a constraint-violation error,
	// and enforced there too so the pairing cannot arrive by any other path
	// (a raw POST, a future caller, a replicated row). The zero value
	// (false) is every existing caller's behaviour, unchanged.
	vehicleID := strings.TrimSpace(in.VehicleID)
	if in.VehicleUnidentified {
		if vehicleID != "" || in.ResolveVehicle != nil {
			return Trip{}, fmt.Errorf("domain: dispatch: a vehicle cannot be both named and marked unidentified")
		}
	} else if vehicleID == "" && in.ResolveVehicle == nil {
		return Trip{}, fmt.Errorf("domain: dispatch: vehicle id is required")
	}
	// ⟦W7-4(b) / W7-25(e), 2026-09-03⟧ F43'S PLACEHOLDER REFUSAL STOOD HERE
	// AND HAS BEEN RELOCATED TO VEHICLE CREATION — NOT REMOVED. ⟦was:
	// "if IsPlaceholderVehicleID(in.VehicleEntered) { return Trip{},
	// fmt.Errorf(\"domain: dispatch: %w\", vehicleIDRefusal(in.VehicleEntered)) }"⟧
	//
	// ⚠ THE RATIONALE THAT STOOD WITH IT IS REWRITTEN RATHER THAN DELETED,
	// because a comment justifying a guard that is no longer here is the
	// artefact a future reader restores the guard from. It said the guard sat
	// "at the ONE authoring choke point every vehicle-bearing trip passes
	// through … the failure mode it guards is an operator under time pressure
	// entering whatever satisfies the form". That failure mode has not gone
	// away — it has MOVED, with the typing. Under DR-W7-4 no number is typed
	// at this door: the caller names a vehicle RECORD, and the one moment a
	// number can be typed is when that record is created. So the choke point
	// is now refuseIfPlaceholderNumber (vehicle.go), which CreateVehicle and
	// AmendVehicle both pass through, and it is consulted ONCE instead of on
	// every trip forever. [[D-11]]'s "the placeholder refusal stays" is
	// satisfied by relocation, and W7-25(e)'s one-call-site census is what
	// keeps a re-added copy here from passing as a relocation.
	if in.DriverPersonID == "" {
		return Trip{}, fmt.Errorf("domain: dispatch: driver is required (and is always an occupant, §7)")
	}
	if in.DestinationSite != nil && in.DestinationUncontrolled != nil {
		// One truck, one destination. A trip that is simultaneously an
		// inbound expectation at a peer AND a run to an ungated pad has no
		// coherent meaning — and whichever field a reader trusted, the other
		// would silently lie.
		return Trip{}, fmt.Errorf("domain: dispatch: a trip cannot have both a controlled and an uncontrolled destination")
	}
	// D-4 (JAMES-DECISIONS.md) — A DEPARTURE MUST NAME WHERE IT WENT.
	//
	// The operating environment is the Arctic North Slope, in winter, on
	// private property: no privately owned vehicles, no pedestrians. Every
	// person's presence on site is bounded by a vehicle trip in and a
	// vehicle trip out. A departure with no vehicle and no destination is
	// not a thing that happens here.
	//
	// This is a domain refusal rather than form validation for the same
	// reason IsPlaceholderVehicleID above is: the defect it closes was two
	// buttons in two templates, but the failure mode is a raw POST, or the
	// next surface somebody builds. A destination-less trip with no arrival
	// is BYTE-IDENTICAL to a check-in that crashed between its two events —
	// IncompleteCheckIns (checkin.go) selects exactly {open, this site,
	// DestinationSite nil, DestinationUncontrolled nil, DeadlineMinutes
	// nil} — so authoring one as a departure put every routine departure on
	// /gate under "Incomplete check-ins — needs attention" with a Resume
	// button that would check the departed crew back onto this muster in
	// one click, and made the printed muster read MAY BE INCOMPLETE six
	// days a week. That is alarm fatigue on the single warning that stops a
	// printed roster being silently short.
	//
	// The fix is not to teach the system to tell the two apart. It is that
	// only one of them can be authored: the check-in half, through
	// DispatchLocalCheckIn, which is immediately followed by its arrival.
	//
	// If an exit without a vehicle is ever genuinely needed — medevac by
	// air is the plausible case, and D-4 says it is NOT currently covered
	// anywhere — it must be authored as its own distinct event type with
	// its own name on the muster, never as a trip with a missing field.
	// Ask James before building any such thing.
	// James, 2026-10-03: the road-work kind (road_work.go) is the ONE
	// destination-less departure D-4 now admits, and it is admitted because
	// it is NOT the shape D-4 removed: it carries a deadline, so it is
	// distinguishable from a crashed check-in by construction
	// (IncompleteCheckIns selects deadline-less trips), it appears in its own
	// category on every board, and its own arrival closes it. The two refusals
	// below keep the kind whole: a road-work run with a destination would be
	// two different trips in one event, and one with no deadline would be a
	// crashed check-in again under another name — the exact ambiguity D-4
	// exists to refuse.
	if in.RoadWork && (in.DestinationSite != nil || in.DestinationUncontrolled != nil) {
		return Trip{}, fmt.Errorf("domain: dispatch: equipment working on the road has no destination post — record it as working on the road OR as a trip to a destination, not both")
	}
	if in.RoadWork && in.DeadlineMinutes == nil {
		return Trip{}, fmt.Errorf("domain: dispatch: working on the road needs this post's configured working limit as its deadline — without it the run is indistinguishable from a check-in that crashed half-way, and nothing would ever call it overdue")
	}
	if !localCheckIn && !in.RoadWork && in.DestinationSite == nil && in.DestinationUncontrolled == nil {
		return Trip{}, fmt.Errorf("domain: dispatch: a departure must record where it is going (D-4: nobody leaves this site except in a vehicle, to somewhere). A trip with no destination is indistinguishable from a check-in that crashed half-way: it would appear as an incomplete check-in offering to put these people back on the muster, and the printed muster would read MAY BE INCOMPLETE")
	}
	if in.DestinationUncontrolled != nil && in.DeadlineMinutes != nil {
		// The reverse direction of the F4 rule, enforced — not ignored: at an
		// uncontrolled destination nobody can record the arrival that would
		// close the expectation, so a deadline here is a promise that can
		// only ever fire falsely. Refusing (rather than dropping the value)
		// keeps the officer's mental model honest: if they expected a timer,
		// they must hear that no timer is possible for this destination.
		return Trip{}, fmt.Errorf("domain: dispatch: destination %q is uncontrolled — nobody there can record an arrival, so a deadline cannot be honoured; dispatch without one (occupants are still recorded and checked out)", *in.DestinationUncontrolled)
	}
	if in.DestinationSite != nil && in.DeadlineMinutes == nil {
		// C7 architecture review: a trip WITH a destination is an inbound
		// expectation, but C7's scanner selects on DeadlineMinutes != nil
		// (deadline.go) — a destination-without-deadline trip would enter
		// OpenExpectations yet be silently skipped by the scan, the worst
		// shape this system can produce: dispatched, no alert promise, and
		// no signal anywhere that none exists. A degenerate trip (BOTH nil,
		// §10's local check-in) is unaffected by this check.
		return Trip{}, fmt.Errorf("domain: dispatch: a trip with a destination requires a deadline — otherwise no alert promise exists for it and nothing signals that")
	}
	if in.DestinationSite != nil && in.KnownDestinationSites != nil {
		// F4: refuse a destination that is not one of the caller's known
		// CONTROLLED DESTINATIONS — config.ControlledDestinationIDs, the set
		// the dispatch buttons are built from, and since D-77 a subset of
		// the peer list rather than the peer list itself (a post may
		// replicate with a site it has no road to). Without this, a typo'd
		// destination authors fine and is never scanned by anyone (see the
		// field doc above). The message below is census-pinned
		// (store/w5e_dispatch_refusal_census_test.go) and left as it stands;
		// the web resolver refuses the peer-not-destination case first, in
		// words that name the distinction.
		known := false
		for _, s := range in.KnownDestinationSites {
			if s == *in.DestinationSite {
				known = true
				break
			}
		}
		if !known {
			return Trip{}, fmt.Errorf("domain: dispatch: destination %q is not a known peer site", *in.DestinationSite)
		}
	}
	if in.DestinationUncontrolled != nil && in.KnownUncontrolledDestinations != nil {
		// F4's membership check for the uncontrolled list: a typo'd pad name
		// would author fine and read like a real place forever after.
		known := false
		for _, s := range in.KnownUncontrolledDestinations {
			if s == *in.DestinationUncontrolled {
				known = true
				break
			}
		}
		if !known {
			return Trip{}, fmt.Errorf("domain: dispatch: %q is not a configured uncontrolled destination", *in.DestinationUncontrolled)
		}
	}
	if in.DeadlineMinutes != nil && *in.DeadlineMinutes <= 0 {
		return Trip{}, fmt.Errorf("domain: dispatch: deadline must be a positive number of minutes")
	}
	// ⟦was: the weather-phase gate (§4, weather-phases-v1) — read the phase
	// fold, stamped phase_at_dispatch, refused a blocking phase without an
	// override reason. Removed, James 2026-10-04: "the phase conditions can be
	// removed they will not effect site control". Officers simply do not
	// dispatch in bad weather; that is their call, not the system's.⟧
	if in.ValidateRoster {
		// F11: check driver/occupant ids exist BEFORE authoring — every
		// other authoring path (resolvePersonSlot) already refuses a
		// dangling reference instead of authoring one.
		if err := requireRosterPerson(st, in.DriverPersonID); err != nil {
			return Trip{}, fmt.Errorf("domain: dispatch: driver: %w", err)
		}
		for _, p := range in.OccupantPersonIDs {
			if p == "" {
				continue // the empty-id refusal below already covers this
			}
			if err := requireRosterPerson(st, p); err != nil {
				return Trip{}, fmt.Errorf("domain: dispatch: occupant: %w", err)
			}
		}
	}
	occupants := in.OccupantPersonIDs
	hasDriver := false
	for _, p := range occupants {
		if p == "" {
			return Trip{}, fmt.Errorf("domain: dispatch: empty occupant person id")
		}
		if p == in.DriverPersonID {
			hasDriver = true
		}
	}
	if !hasDriver {
		occupants = append([]string{in.DriverPersonID}, occupants...)
	}
	// ⟦W7, item 8⟧ THE SNAPSHOT IS DERIVED, NOT TYPED. When the run names a
	// vehicle record, trip.vehicle_entered is that record's number AS IT
	// STANDS NOW, frozen here — the role visit.person_snapshot_name plays for
	// people in item 5. It is NIL when the record has no number, which is the
	// ordinary shape for a role-only vehicle, and it is nil rather than "" so
	// that "we were never told a number" and "somebody typed nothing" stay
	// different facts.
	//
	// ⚠ The record is looked up, and a MISS IS NOT A REFUSAL. A post may hold
	// a trip whose vehicle record has not reached it — the same partition
	// §9.2 exists for — and DR-W7-4 says nothing about whether a raw POST may
	// name an id it invented: that is W7-6's territory, and the answer there
	// is that the fold never refuses.
	// ⟦TRIAL — §97⟧ THE CO-MINT, AFTER EVERY VALIDATION AND BEFORE THE FIRST AUTHOR.
	// SchemaVersion 13 (R-17): skipped entirely when VehicleUnidentified — the
	// refusal above already guarantees ResolveVehicle is nil in that case, so
	// this whole branch is naturally a no-op; stated here so a reader does
	// not have to re-derive it.
	if vehicleID == "" && in.ResolveVehicle != nil {
		id, rerr := in.ResolveVehicle()
		if rerr != nil {
			// ⟦TRIAL — §114⟧ WRAPPED, not returned bare: every other error out of this function uses
			// `domain: dispatch: %w`, and an unwrapped return would be a NEW refusal SHAPE in the
			// W5e-3 census rather than a reuse of an existing one. `%w` keeps `errors.Is` working,
			// so the caller can still recognise its own co-mint refusal and restore its wording.
			return Trip{}, fmt.Errorf("domain: dispatch: %w", rerr)
		}
		if vehicleID = strings.TrimSpace(id); vehicleID == "" {
			return Trip{}, fmt.Errorf("domain: dispatch: vehicle id is required")
		}
	}
	// SchemaVersion 13 (R-17): snapshot stays nil when VehicleUnidentified —
	// an unidentified movement has no record to look up and therefore no
	// number to snapshot either. vehicleNumberSnapshot is skipped rather than
	// called with "": that function's own contract is "a miss is not a
	// refusal", for an id that NAMES something this post cannot yet see,
	// which is not this case — here there is no id at all, by the officer's
	// own explicit statement.
	var snapshot *string
	if !in.VehicleUnidentified {
		var err error
		snapshot, err = vehicleNumberSnapshot(st, vehicleID)
		if err != nil {
			return Trip{}, fmt.Errorf("domain: dispatch: %w", err)
		}
	}
	// ⟦[[D-84]] / A3 H-5, 2026-09-29⟧ THE CONFIRMED RECORD CHANGES GO HERE —
	// after every refusal above and the co-mint, immediately before the
	// trip — the position GateCheckIn already gives them, now on the
	// departure door too. Every departure refusal (destination, deadline,
	// roster, the vehicle's own) has been ruled out by this line, so
	// a refused departure can no longer leave an amended record behind it.
	// The departing visit keeps its ARRIVAL snapshot (the sheet says what
	// was true when they arrived); the record carries the correction into
	// every future check-in. Wrapped in this function's own convention, the
	// same reuse of `domain: dispatch: %w` the resolver's failure takes
	// (w5eReusedLiteralArms names both).
	if err := ApplyPersonRecordChanges(st, siteID, nodeEpoch, in.RecordChanges, in.RecordedBy); err != nil {
		return Trip{}, fmt.Errorf("domain: dispatch: %w", err)
	}
	tripID := newID(siteID, "trip")
	payload, err := json.Marshal(TripDispatched{
		Type: "trip_dispatched", TripID: tripID,
		VehicleID:           vehicleID,
		VehicleUnidentified: in.VehicleUnidentified,
		VehicleEntered:      snapshot, DriverPersonID: in.DriverPersonID,
		OccupantPersonIDs: occupants,
		DestinationSite:   in.DestinationSite, DestinationUncontrolled: in.DestinationUncontrolled,
		DeadlineMinutes: in.DeadlineMinutes,
		CargoNote:       in.CargoNote,
		RoadWork:        in.RoadWork,
		RecordedBy:      in.RecordedBy,
	})
	if err != nil {
		return Trip{}, fmt.Errorf("domain: dispatch: %w", err)
	}
	env, err := st.Author(siteID, nodeEpoch, payload, occurredAtMS, timeSource)
	if err != nil {
		return Trip{}, fmt.Errorf("domain: dispatch: %w", err)
	}
	// int64, not bool: Trip.VehicleUnidentified is the projection-read shape
	// (see its own comment — dumpQuery's JSON round trip cannot decode a raw
	// number into a Go bool). This return value is not itself read back
	// through that pipeline, but it must equal what a fresh TripByID call
	// would decode, byte for byte, or a caller comparing "what Dispatch
	// handed back" against "what a re-read returns" would see two different
	// facts for the same trip.
	var unidentifiedFlag int64
	if in.VehicleUnidentified {
		unidentifiedFlag = 1
	}
	return Trip{
		TripID: tripID, OriginSite: siteID, NodeEpoch: env.NodeEpoch, Seq: env.Seq,
		VehicleID:           vehicleID,
		VehicleUnidentified: unidentifiedFlag,
		VehicleEntered:      snapshot, DriverPersonID: in.DriverPersonID,
		DestinationSite: in.DestinationSite, DestinationUncontrolled: in.DestinationUncontrolled,
		DeadlineMinutes: in.DeadlineMinutes,
		CargoNote:       in.CargoNote,
		RecordedBy:      in.RecordedBy,
	}, nil
}

// vehicleNumberSnapshot computes the frozen number snapshot —
// trip.vehicle_entered at dispatch, trip_arrival.vehicle_entered at an
// unsolicited arrival — from the COMPOSED vehicle record and from nothing
// else (W7-4(d)).
//
// ⟦W7-4(d)(i), 2026-09-03⟧ ⟦was: "func dispatchVehicleSnapshot(st
// *store.Store, vehicleID, entered string) (*string, error)", with a second
// branch returning the caller's typed string when no id was given⟧. That arm
// was labelled TRANSITIONAL at the time and said so: it existed only because
// DispatchInput still carried a caller-supplied string. It does not any more,
// so the arm has no argument to read and both doors share this one function.
//
// ⚠ A MISS IS NOT A REFUSAL. A post may hold a trip whose vehicle record has
// not reached it — the same partition §9.2 exists for — so an id naming no
// local record yields a NIL snapshot, not an error. nil rather than "" so that
// "we were never told a number" and "somebody typed nothing" stay different
// facts (DR-W7-15).
func vehicleNumberSnapshot(st *store.Store, vehicleID string) (*string, error) {
	if vehicleID == "" {
		return nil, nil
	}
	v, err := VehicleByID(st, vehicleID)
	if err != nil {
		// Returned RAW and wrapped at the call site, in the file's own
		// style, so the census that pins dispatch's refusal shapes sees a
		// format refusal rather than a bare propagation.
		return nil, err
	}
	if v != nil && v.Number != nil {
		n := *v.Number
		return &n, nil
	}
	// ⟦gate-restack, 2026-09-11⟧ CONSIDERED AND REJECTED: falling back to
	// v.Role here so a "Vehicle unknown" role-only record (vehicleIDForUnknown,
	// web/vehicle_comint.go) reads as explicit text instead of nil. That is
	// EXACTLY the layering DR-W7-15/[[D-11]] rule out at THIS function —
	// see TestW7NumberlessVehicleRendersAsTheEmptyStringNeverASentinel
	// (internal/deadline/s120_vehicle_absence_test.go) and this file's own
	// vehicleSnapshotText sibling in web/vehicle_render.go: "the empty
	// string IS the absence... a stand-in wording is a magic string in the
	// vehicle field... what an officer should SEE is the render helper's
	// business (W7-26), not this scan's [or this snapshot's]." A role-only
	// vehicle's snapshot stays nil here, exactly as it did before this
	// lane touched the file. What is explicit and auditable is the VEHICLE
	// RECORD ITSELF (Role: "Vehicle unknown", never a number) — read back
	// live by VehicleID, never by re-deriving it from this frozen text.
	return nil, nil
}

// buildArrivalVisits mints the per-occupant visit ids and point-in-time
// snapshots AT AUTHORING TIME (§8.8-i) — the fold only materialises them, so
// two sites folding the same arrival can never disagree. The snapshot is the
// roster row's own company/trade id plus its current name, with NO alias
// resolution (C5 review row 23: snapshot the reference actually used; two
// identical-looking trades from partitioned sites must stay
// distinguishable in history, and a later merge assertion changes future
// resolution, never these rows).
//
// D-98/D-91 (SCHEMA-11 step 5): hasOrientation is THIS SITE's own
// config.HasOrientation() — the destination's, never a peer's (orientation.go's
// file header: "the site is the envelope's, never the payload's"; no post
// can read a peer's flag at all, d91-arrival-respec.md §1). When false
// (an ice-road post, or any caller not yet wired to pass it — see the two
// call sites below), every occupant enters exactly as before: the choke
// point never consults OrientationHeld, so a site with no orientation rows
// for anyone (because its marking route is never registered — D-88) cannot
// be misread as "nobody here is oriented" and empty everyone into
// notEntered. When true, an occupant enters (a visit) only if
// OrientationHeld finds their row for THIS site; otherwise they are
// aboard-but-not-entered — returned as a bare id, never a snapshot: its one
// reader (HeldOutAt, heldout.go — R-11, 2026-09-09) composes the person LIVE
// and needs none ⟦was: "because nothing renders it (D-98 §0)"⟧.
func buildArrivalVisits(st *store.Store, siteID string, personIDs []string, hasOrientation bool) ([]ArrivalVisit, []string, error) {
	// Point lookups per occupant (a check-in carries single digits of
	// people), memoised so a shared company/trade is read once — never a
	// full roster/company/trade dump per arrival (F30).
	companyName := map[string]*string{}
	tradeName := map[string]*string{}
	out := make([]ArrivalVisit, 0, len(personIDs))
	var notEntered []string
	// ⟦D1 fix, WINDOW-AUDIT.md⟧ loaded once per arrival, exactly as
	// HeldOutAt (heldout.go) already loads it: a manifest occupant is
	// frequently the RAW id a bus roster was built from, and a merge (D-59)
	// recorded since then can leave that id a duplicate whose canonical
	// record is the one that actually holds this site's orientation. Only
	// consulted when hasOrientation — a road post never reads the orientation
	// table at all (D-88), so this costs it nothing.
	var merge AliasMap
	if hasOrientation {
		var err error
		merge, err = LoadPersonMergeMap(st)
		if err != nil {
			return nil, nil, err
		}
	}
	for _, id := range personIDs {
		p, err := PersonByID(st, id)
		if err != nil {
			return nil, nil, err
		}
		if p == nil {
			return nil, nil, fmt.Errorf("domain: arrival: a person named on this request is not on the roster")
		}
		if hasOrientation {
			// Resolved through the merge BEFORE the read: a duplicate merged
			// into a canonical record that already holds this site's
			// orientation must enter, not be held out on the strength of its
			// own now-superseded id (WINDOW-AUDIT.md D1 — the manifest still
			// names the duplicate; the completion was recorded under the
			// surviving record).
			// ⟦R4, James, 2026-10-04⟧ …and the canonical inherits a
			// completion recorded under any record merged into it.
			held, err := OrientationHeldThroughMerge(st, merge, id, siteID)
			if err != nil {
				return nil, nil, err
			}
			if held == nil {
				notEntered = append(notEntered, id)
				continue
			}
		}
		v := ArrivalVisit{VisitID: newID(siteID, "visit"), PersonID: id}
		// ⟦W6, item 5⟧ The name snapshot, frozen here and never rewritten —
		// ⟦was: "the only three UPDATE visit statements in the tree set
		// is_open and nothing else"⟧ since SchemaVersion 15 the ONLY
		// UPDATEs of `visit` are visit_open.go's two derivation statements,
		// both on is_open, and every other column is immutable BY TRIGGER
		// (store.go, visit_raw_immutable) — so whatever lands here is what
		// every future printed sheet for this visit will say. It costs NO extra read: `p` is the
		// whole composed person, already in hand above, and `p.Name` is the
		// authoritative display name (D-48: the parts may diverge from it and
		// no read path composes Name from them). D-60: the display name and
		// nothing else — no parts on this payload.
		v.PersonName = &p.Name
		if p.CompanyID != nil {
			v.CompanyID = p.CompanyID
			n, ok := companyName[*p.CompanyID]
			if !ok {
				c, err := CompanyByID(st, *p.CompanyID)
				if err != nil {
					return nil, nil, err
				}
				if c != nil {
					n = &c.Name
				}
				companyName[*p.CompanyID] = n
			}
			v.CompanyName = n
		}
		if p.TradeID != nil {
			v.TradeID = p.TradeID
			n, ok := tradeName[*p.TradeID]
			if !ok {
				w, err := TradeByID(st, *p.TradeID)
				if err != nil {
					return nil, nil, err
				}
				if w != nil {
					n = &w.Name
				}
				tradeName[*p.TradeID] = n
			}
			v.TradeName = n
		}
		out = append(out, v)
	}
	return out, notEntered, nil
}

// RecordExpectedArrival authors the B-side arrival for a trip this site
// already knows (§9.1) — the inbound board's "check in" action. One action
// creates one visit per trip occupant, driver included (§10). Refused if the
// trip is unknown here (use RecordUnsolicitedArrival — the vehicle outran the
// sync) or already arrived.
//
// hasOrientation is D-98/D-91's SCHEMA-11 step 5 gate — this site's own
// config.HasOrientation(), variadic (OFF BY DEFAULT, the zero value) for the
// same reason ValidateRoster above is off by default:
// test-fixture compatibility. Positional scalar arguments, unlike
// DispatchInput's struct, cannot grow a new field for free, and this
// function has ~130 existing call sites across domain/web that must keep
// compiling unchanged; a trailing variadic — the same mechanism
// RecordUnsolicitedArrival already uses for resolveVehicle below — costs
// them nothing. ⚠ EVERY CALLER REACHABLE FROM A REQUEST AT AN
// orientation=true SITE MUST PASS true or an un-oriented occupant enters
// silently; wiring the live web-layer callers (web/trips.go, web/gate.go)
// is NOT this step's build (team-lead scope: buildArrivalVisits /
// RecordExpectedArrival only) — see the build report's flagged gap.
func RecordExpectedArrival(st *store.Store, siteID string, nodeEpoch int64, tripID string, recordedBy *string, occurredAtMS int64, timeSource string, hasOrientation ...bool) (Arrival, error) {
	return RecordExpectedArrivalExcluding(st, siteID, nodeEpoch, tripID, nil, recordedBy, occurredAtMS, timeSource, hasOrientation...)
}

// RecordExpectedArrivalExcluding is RecordExpectedArrival with a per-person
// disposition (James, 2026-08-20): stayedPersonIDs marks occupants who did
// NOT come in on this vehicle — dropped at an uncontrolled pad en route, or
// (on an uncontrolled run's return leg) still out at the pad. They get no
// visit, so they stay off this site's muster; everyone else checks in as
// before. The default is the nil/empty set — everyone in — because most
// people come back (operator ruling): the exception costs one click per
// person, the common case zero.
//
// NO NEW EVENT TYPE and no fold change: TripArrival.Visits was always a
// per-person list the fold materialises verbatim, so a person who stayed is
// simply an occupant the arrival's visit set omits. The trip still closes
// (both facts, §9.1); the stayed people remain checked out from dispatch and
// later check in NORMALLY through any ordinary path, any vehicle — people
// are never bound to vehicles, the vehicle is an attribute of the movement.
// Who is still out where remains answerable: see OutAtUncontrolled.
//
// A stayed id that is not an occupant of the trip is refused (the F4 typo
// posture), and marking EVERYONE as stayed is refused — an arrival with no
// visits is not an arrival; if the vehicle never came back there is nothing
// to record.
//
// ⚠ D-98/D-91: that refusal is keyed on ABOARD (len(personIDs), computed
// below BEFORE buildArrivalVisits ever runs), never on entered. A bus of
// five, all un-oriented, IS an arrival — the vehicle came, the trip closes,
// five are not entered (§3.2). Keying this refusal on len(visits) instead
// would wrongly refuse that case; it is deliberately checked first, on the
// aboard set, so the split below can never reach it.
//
// D18 (FINDINGS-R4): the "already arrived" check and the write are ONE
// transaction. They used to run as separate store calls, so two Arrived (or
// "Arrived back here") presses sent together both read the trip as open and
// authored two arrivals — the phantom second one then blocked unref. The
// store has one connection and Transact holds it for the whole body, so a
// second press's check now runs after the first one's write and refuses
// with the ordinary "has already arrived here" text — the serialisation the
// gate's check-in got (web/gate.go gateCommitMu), given here at the door
// every arrival caller shares. Inside an existing Transact (GateCheckIn,
// ResumeCheckIn) this composes flat onto the caller's transaction.
func RecordExpectedArrivalExcluding(st *store.Store, siteID string, nodeEpoch int64, tripID string, stayedPersonIDs []string, recordedBy *string, occurredAtMS int64, timeSource string, hasOrientation ...bool) (Arrival, error) {
	var arrival Arrival
	err := st.Transact("expected arrival", func(tx *store.Store) error {
		plan, err := planExpectedArrival(tx, siteID, tripID, stayedPersonIDs, hasOrientation...)
		if err != nil {
			return err
		}
		arrival, err = authorExpectedArrival(tx, siteID, nodeEpoch, plan, recordedBy, occurredAtMS, timeSource, hasOrientation...)
		return err
	})
	if err != nil {
		return Arrival{}, err
	}
	return arrival, nil
}

// arrivalPlan is what planExpectedArrival establishes and authorExpectedArrival
// consumes: the trip, and the occupants who came in on it (aboard and not
// stayed) — the visit candidates buildArrivalVisits splits into entered and
// held out.
type arrivalPlan struct {
	tripID    string
	personIDs []string
}

// planExpectedArrival is EVERY refusal an expected arrival can produce,
// with no write behind any of them ⟦A3 / H-6, 2026-09-29⟧: the trip is
// known here, has not already arrived, has occupant rows, and not everybody
// is marked as stayed. Split out of RecordExpectedArrivalExcluding so a door
// with MORE to author before the arrival — /gate/resume's ticked
// orientations (ResumeCheckInCompleting, checkin.go) — can rule out the
// arrival's own refusals FIRST and only then write, instead of recording a
// tick the arrival then refuses to honour ("recording it again enters
// nobody" — while an orientation and, through entry-on-completion, a visit
// had just been recorded; FINDINGS H-6, L03 G-121). The refusal texts are
// the ones that always stood here, byte for byte.
func planExpectedArrival(st *store.Store, siteID, tripID string, stayedPersonIDs []string, hasOrientation ...bool) (arrivalPlan, error) {
	trip, err := TripByID(st, tripID)
	if err != nil {
		return arrivalPlan{}, fmt.Errorf("domain: expected arrival: %w", err)
	}
	if trip == nil {
		return arrivalPlan{}, fmt.Errorf("domain: expected arrival: trip %s is not known at this site yet — record an unsolicited arrival instead (§9.2)", tripID)
	}
	state, err := TripStateByID(st, tripID)
	if err != nil {
		return arrivalPlan{}, fmt.Errorf("domain: expected arrival: %w", err)
	}
	if state != nil && (state.State == TripStateArrived || state.State == TripStateArrivedAfterCancel) {
		// ⟦heldout-recovery, 2026-09-10⟧ ⟦was: "trip %s is already %s"⟧. This
		// reaches the officer verbatim via refuse() (POST /gate/resume, POST
		// /trips/arrive), and "already arrived" was read as "already on
		// site" — false when that arrival entered NOBODY, which is exactly
		// what an orientation post's arrival of an un-oriented vehicle does
		// (MEASURED live at 3f46fcb: 0 open visits, trip state `arrived`;
		// reports/witness/HELDOUT-RECOVERY.md). The state word is a fact
		// about the TRIP. Say so, say what recording it again would not do,
		// and — only where this site holds people out — name the one route
		// back in: the completion, then a fresh check-in at the gate. A
		// road post is told nothing about an orientation nobody owes there.
		// SchemaVersion 12 (R-16): the epilogue is no longer unconditional on
		// hasOrientation alone. Entry-on-completion (d91 §3.4) means
		// recording the completion may ALREADY have entered everyone this
		// trip held out — printing "check them in again" after that would
		// be the exact "already arrived" == "already on site" misreading
		// this refusal exists to refuse, aimed at a step that is no longer
		// needed. Checked live, not cached: a completion recorded between
		// requests must be reflected the next time this refusal fires.
		// FINDINGS-R4 D17 (L17-11): the binding arrival may be ANOTHER post's —
		// since "Arrived back here" (trip_return.go) the commonest case is the
		// origin recording the truck back. "Has already arrived here" was then
		// false, and the officer facing that crew at this gate needs to know
		// they are on the other post's muster, not this one's. Still refused —
		// the trip is closed — but in true words.
		if elsewhere := arrivedElsewhereSentence(st, siteID, *trip, state); elsewhere != "" {
			return arrivalPlan{}, fmt.Errorf("domain: expected arrival: %s", elsewhere)
		}
		next := ""
		if len(hasOrientation) > 0 && hasOrientation[0] {
			if still, serr := anyoneStillHeldOutFromTrip(st, siteID, tripID); serr == nil && still {
				next = " Anyone aboard who was held out at that arrival for lack of this site's orientation is not on site: record their completion under Orientation, then check them in again at the gate."
			}
		}
		return arrivalPlan{}, fmt.Errorf("domain: expected arrival: this run has already arrived here: its arrival is on record, and recording it again enters nobody. That is a fact about the run, not about who is on site — the muster says who is.%s", next)
	}
	// NOTE: a CANCELLED trip is deliberately NOT refused — an arrival
	// check-in for a trip the origin cancelled is the §9.1
	// arrived-after-cancel case, and it must be recordable so the fold can
	// surface it for review. The people are physically here either way.
	// FINDINGS-R4 D1 (CRITICAL; L17-28/L17-29) ⟦was: TripOccupantsByTrip —
	// the RAW dispatch rows⟧. Who came in on this trip is the EFFECTIVE
	// occupant set: a driver named by correction and anyone added by
	// "Aboard after all" are aboard, someone corrected off the list is not.
	// Reading the raw rows checked nobody added after dispatch in — they were
	// on every muster as "not aboard at arrival" while the arrival form (which
	// composes) listed them. Both arrival doors come through here (the
	// destination's Arrived and the origin's "Arrived back here"), and the
	// visit set is decided ONCE, at this post, and replicated verbatim in the
	// trip_arrival payload — so every post and every replay reads the same
	// visits.
	composed, err := EffectiveTrip(st, tripID)
	if err != nil {
		return arrivalPlan{}, fmt.Errorf("domain: expected arrival: %w", err)
	}
	var occupants []string
	if composed != nil {
		occupants = composed.OccupantIDs
	}
	isOccupant := make(map[string]bool, len(occupants))
	for _, id := range occupants {
		isOccupant[id] = true
	}
	stayed := make(map[string]bool, len(stayedPersonIDs))
	for _, id := range stayedPersonIDs {
		if !isOccupant[id] {
			return arrivalPlan{}, fmt.Errorf("domain: expected arrival: %s is not an occupant of trip %s — cannot mark them as stayed behind", id, tripID)
		}
		stayed[id] = true
	}
	// F15 (James, 2026-10-05): a person the record shows back at the
	// origin since the run left (StillAboard — the muster's rule) is off
	// the run everywhere, the arrival included: never entered here, and no
	// second open visit at the origin when the truck is "Arrived back here".
	// A stayed tick naming them is accepted and changes nothing (the form
	// that sent it may predate the replication that took them off).
	aboard, err := StillAboard(st, composed)
	if err != nil {
		return arrivalPlan{}, fmt.Errorf("domain: expected arrival: %w", err)
	}
	var personIDs []string
	for _, id := range aboard {
		if !stayed[id] {
			personIDs = append(personIDs, id)
		}
	}
	if len(occupants) == 0 {
		return arrivalPlan{}, fmt.Errorf("domain: expected arrival: trip %s has no occupant rows — refusing to author an empty arrival", tripID)
	}
	if len(personIDs) == 0 && len(aboard) == 0 {
		// Everyone aboard turned back and is checked in at the origin. The
		// vehicle still arrived and its trip must close — it is the
		// deadline's to clear — so the arrival is recorded and enters
		// nobody, which is the truth.
		return arrivalPlan{tripID: tripID}, nil
	}
	if len(personIDs) == 0 {
		// ⟦D-78, 2026-09-05⟧ ⟦was: "…record nothing (or cancel the trip)"⟧
		// This error reaches the officer verbatim via refuse() (trips.go
		// handleExpectedArrival → refuse). [[D-73]]: no trip is cancelled;
		// [[D-78]] closed the route. The advice can no longer offer a cancel —
		// it names the two true options instead: untick whoever did come back,
		// or record nothing and leave the trip open.
		return arrivalPlan{}, fmt.Errorf("domain: expected arrival: every occupant of trip %s is marked as stayed behind — an arrival with nobody aboard is not an arrival; a trip is never cancelled, so if some of the crew did come back, untick those who did, and if the vehicle did not return at all, record nothing and leave the trip open", tripID)
	}
	return arrivalPlan{tripID: tripID, personIDs: personIDs}, nil
}

// arrivedElsewhereSentence is D17's refusal (FINDINGS-R4, L17-11): the
// sentence for an arrival refused because the trip's binding arrival was
// recorded at a DIFFERENT post, or "" when it was recorded here (or cannot
// be located — the pre-existing sentence then stands). It names the truck
// as it reads now (LiveVehicles), the post, the time, and says plainly that
// nobody was checked in here; the raw trip id and state word never reach
// the officer.
func arrivedElsewhereSentence(st *store.Store, siteID string, trip Trip, state *TripState) string {
	if state == nil || state.ArrivalID == nil {
		return ""
	}
	a, err := arrivalByID(st, *state.ArrivalID)
	if err != nil || a == nil || a.SiteID == siteID {
		return ""
	}
	vehicle := liveVehicleName(st, trip.TripID)
	at := ""
	if et, found, terr := st.EventTimeAt(store.Key{SiteID: a.SiteID, NodeEpoch: a.NodeEpoch, Seq: a.Seq}); terr == nil && found {
		at = " at " + time.UnixMilli(et.AuthoredAtMS).In(time.Local).Format("15:04")
	}
	// James, 2026-10-04: no screen shows a raw site id; name the post by its display name.
	post := siteNameAt(st, a.SiteID)
	if a.SiteID == trip.OriginSite && trip.DestinationSite != nil {
		return fmt.Sprintf("%s was recorded back at %s%s (turned back) — its arrival is on record there, so it cannot also be recorded here, and nobody was checked in here. Whoever came back on it is on %s's muster, not this post's; anyone standing at this gate is checked in here at the gate.", vehicle, post, at, post)
	}
	return fmt.Sprintf("%s was recorded as arrived at %s%s — its arrival is on record there, so it cannot also be recorded here, and nobody was checked in here. Whoever came in on it is on %s's muster, not this post's; anyone standing at this gate is checked in here at the gate.", vehicle, post, at, post)
}

// liveVehicleName is the run's LIVE vehicle number for a refusal sentence
// (LiveVehicles.Compose, as the boards print it), or "this vehicle" when it
// cannot be composed here — never the trip id.
func liveVehicleName(st *store.Store, tripID string) string {
	if c, err := EffectiveTrip(st, tripID); err == nil && c != nil {
		if live, lerr := NewLiveVehicles(st); lerr == nil {
			if v, _, verr := live.Compose(*c); verr == nil && v != "" {
				return v
			}
		}
	}
	return "this vehicle"
}

// authorExpectedArrival is the write half: buildArrivalVisits over the
// plan's candidates (which reads OrientationHeld LIVE, so anything a caller
// authored between plan and author — a resume's ticked orientation — is
// seen), then the one trip_arrival event.
func authorExpectedArrival(st *store.Store, siteID string, nodeEpoch int64, plan arrivalPlan, recordedBy *string, occurredAtMS int64, timeSource string, hasOrientation ...bool) (Arrival, error) {
	tripID, personIDs := plan.tripID, plan.personIDs
	// FIX17b (sc-stress F12): a typed arrival may not be earlier than the
	// run's departure. Before anything is written (resume's ticks included:
	// they share this transaction).
	if trip, err := TripByID(st, tripID); err == nil && trip != nil {
		if p := arrivalBeforeDeparture(st, siteID, *trip, occurredAtMS, timeSource); p != "" {
			return Arrival{}, fmt.Errorf("domain: expected arrival: nothing was recorded — %s", p)
		}
	}
	visits, notEntered, err := buildArrivalVisits(st, siteID, personIDs, len(hasOrientation) > 0 && hasOrientation[0])
	if err != nil {
		return Arrival{}, fmt.Errorf("domain: expected arrival: %w", err)
	}
	arrivalID := newID(siteID, "arrival")
	payload, err := json.Marshal(TripArrival{
		Type: "trip_arrival", ArrivalID: arrivalID, TripRef: &tripID,
		Visits: visits, NotEntered: notEntered, RecordedBy: recordedBy,
	})
	if err != nil {
		return Arrival{}, fmt.Errorf("domain: expected arrival: %w", err)
	}
	env, err := st.Author(siteID, nodeEpoch, payload, occurredAtMS, timeSource)
	if err != nil {
		return Arrival{}, fmt.Errorf("domain: expected arrival: %w", err)
	}
	return Arrival{ArrivalID: arrivalID, SiteID: siteID, NodeEpoch: env.NodeEpoch, Seq: env.Seq,
		TripRef: &tripID, RecordedBy: recordedBy, NotEntered: notEntered}, nil
}

// RecordUnsolicitedArrival authors an arrival for a trip this site has NOT
// heard of (§9.2: the vehicle outran the sync, or the link is down). It
// carries the correlation key — the vehicle RECORD's id and the claimed origin
// — and the fold reconciles it with the trip on catch-up: no match → the
// visits simply stand as plain visits; ambiguous → review lane.
//
// ⟦W7-4(d)(i), 2026-09-03⟧ ⟦was: "…, vehicleEntered, originSite string, …",
// the number as typed at B's gate⟧. DR-W7-4 makes this door take a
// `vehicle_id`, and the id is the ID-FIRST correlation key's B-side half — the
// thing that makes a PARTITIONED arrival correlate once the vehicle record has
// replicated but the trip has not. `trip_arrival.vehicle_entered` is now the
// DERIVED snapshot of that record's number (vehicleNumberSnapshot), so both
// halves of the two-key rule are populated from one act.
//
// ⚠ THE NUMBER STRING IS STILL ON THE WIRE AND STILL READ BY THE FOLD. What
// changed is who supplies it: the record, not the caller. An arrival whose
// vehicle record has not replicated here carries a nil snapshot and correlates
// on the id alone — which is the case the id key was added for.
// ⟦TRIAL — §97/§107, 2026-09-03⟧ `resolveVehicle` is the arrival door's half of the co-mint
// deferral. It is VARIADIC so that all existing callers compile untouched — [MEASURED] there are 27
// of them, and this shape changes none.
//
// ⚠ WHY NOT A FIELD, AS ON `DispatchInput`: this door has no input struct. §48 measured that
// (`grep 'UnsolicitedArrivalInput'` returns zero), which is why its census is a SIGNATURE census
// rather than a reflection one.
//
// ⚠ THE SIGNATURE CENSUS PERMITS THIS, and that was checked rather than assumed:
// `TestW7RecordUnsolicitedArrivalSignatureTakesAVehicleID` asserts the list carries NO
// `vehicleEntered string` / `vehicle string` / `vehicleNumber string`, and DOES carry `vehicleID`.
// A trailing func parameter adds no free-text vehicle string and removes nothing.
//
// D-98/D-91 (SCHEMA-11 step 5 wiring): hasOrientation is hardcoded false
// HERE, exactly as it always has been — every one of this door's ~55
// existing call sites keeps compiling and behaving unchanged. A caller that
// wants the split uses RecordUnsolicitedArrivalOriented below, this
// function's own thin wrapper (the same relationship RecordExpectedArrival
// has to RecordExpectedArrivalExcluding). This could not be a second
// trailing variadic — Go permits exactly one, and it must be last, so
// `resolveVehicle ...func() (string, error)` already occupies that slot —
// and a fixed bool ahead of it changes nothing about the 27 callers that
// pass a resolver (§97/§107): they still trail with `resolveVehicle...`.
func RecordUnsolicitedArrival(st *store.Store, siteID string, nodeEpoch int64, vehicleID, originSite string, personIDs []string, recordedBy *string, occurredAtMS int64, timeSource string, resolveVehicle ...func() (string, error)) (Arrival, error) {
	return RecordUnsolicitedArrivalOriented(st, siteID, nodeEpoch, vehicleID, originSite, personIDs, recordedBy, occurredAtMS, timeSource, false, false, resolveVehicle...)
}

// RecordUnsolicitedArrivalOriented is RecordUnsolicitedArrival with the
// D-98/D-91 orientation gate exposed (SCHEMA-11 step 5 wiring, the
// follow-up the step5 build report flagged: this door's hasOrientation was
// hardcoded false with no way for a caller to turn it on). hasOrientation
// is a plain bool, not variadic — resolveVehicle already holds the one
// variadic slot Go allows, and it must stay last, so the new parameter
// goes immediately before it instead. web/trips.go's
// handleUnsolicitedArrival is the only production caller of this entry
// point; every test and every other caller keeps using
// RecordUnsolicitedArrival unchanged (it delegates here with false).
//
// vehicleUnidentified (SchemaVersion 14, R-18 — this door's own share of
// R-17, "there are no doors only vehicles") is a SECOND plain bool added
// the identical way hasOrientation was: resolveVehicle still occupies the
// one variadic slot Go allows and must stay last, so this goes immediately
// before it too. RecordUnsolicitedArrival (above) delegates with false,
// unchanged behaviour for its ~55 existing callers; only
// handleUnsolicitedArrival, this function's one production caller, sets
// it from the form's "Vehicle unknown" checkbox.
func RecordUnsolicitedArrivalOriented(st *store.Store, siteID string, nodeEpoch int64, vehicleID, originSite string, personIDs []string, recordedBy *string, occurredAtMS int64, timeSource string, hasOrientation bool, vehicleUnidentified bool, resolveVehicle ...func() (string, error)) (Arrival, error) {
	// ⟦TRIAL — §97/§107⟧ ⚠ DEFERRED, NOT WEAKENED — the same shape and the same hazard as
	// `Dispatch`'s: a caller with a resolver has no id YET, and one whose resolver yields nothing is
	// refused below by this identical literal. Both paths are asserted in
	// s97_resolver_refusal_test.go.
	vehicleID = strings.TrimSpace(vehicleID)
	// SchemaVersion 14 (R-18): a THIRD option alongside "named" and "not
	// yet resolved" — vehicleUnidentified, an explicit, decided fact that
	// no vehicle exists to name for this arrival at all. Refused ALONGSIDE
	// a name or a resolver, never blended with one — the identical
	// impossibility dispatchTrip enforces for TripDispatched (tripauthor.go
	// above), reused in wording rather than in code because this door has
	// no shared choke point with dispatchTrip to route through (it authors
	// trip_arrival, not trip). The store's own CHECK constraint on
	// trip_arrival.vehicle_unidentified (store.go) enforces the same
	// impossibility one layer down, for a raw POST or a future caller that
	// bypasses this function entirely.
	if vehicleUnidentified {
		if vehicleID != "" || (len(resolveVehicle) > 0 && resolveVehicle[0] != nil) {
			return Arrival{}, fmt.Errorf("domain: unsolicited arrival: a vehicle cannot be both named and marked unidentified")
		}
	} else if vehicleID == "" && len(resolveVehicle) == 0 {
		return Arrival{}, fmt.Errorf("domain: unsolicited arrival: vehicle id is required (it is the correlation key, §9.2)")
	}
	// ⟦W7-4(b)(c) / W7-25(e), 2026-09-03⟧ F43'S PLACEHOLDER REFUSAL STOOD HERE
	// AND IS RELOCATED TO VEHICLE CREATION — NOT REMOVED. ⟦was:
	// "if IsPlaceholderVehicleID(vehicleEntered) { return Arrival{},
	// fmt.Errorf(\"domain: unsolicited arrival: %w\", vehicleIDRefusal(vehicleEntered)) }"⟧
	//
	// ⚠⚠ ITS RATIONALE IS REWRITTEN IN THE SAME CHANGE, BECAUSE W7-4(c) SAYS
	// IT MUST BE AND BECAUSE THAT RATIONALE WAS LOAD-BEARING AND IS NOW FALSE.
	// It read: "here the reason is sharper than at dispatch: this value IS the
	// correlation key. A placeholder correlates with every other placeholder
	// from the same origin … Refusing at both ends also keeps the rule
	// symmetric." Under DR-W7-4 that argument is not overridden — it is
	// DISSOLVED. The correlation key at this door is now a vehicle RECORD's
	// id, minted once and unique per record, so it cannot BE a placeholder;
	// there is no typed value here left to refuse. The symmetry the old
	// comment wanted is still exact, and is now stronger: BOTH ends key on the
	// same record id, and the one place a number is typed — vehicle creation —
	// refuses the whole 38-member list for both of them.
	//
	// ⚠ A future reader tempted to restore the guard should note that
	// IsPlaceholderVehicleID has exactly ONE production call site by census
	// (W7-25(e)), and that a second one here would turn a relocation into a
	// duplication, which is mutant (m7-25d).
	if originSite == "" {
		return Arrival{}, fmt.Errorf("domain: unsolicited arrival: origin site is required (it is the correlation key, §9.2)")
	}
	if len(personIDs) == 0 {
		return Arrival{}, fmt.Errorf("domain: unsolicited arrival: at least one occupant is required")
	}
	// ⟦UNSOL-DUP-PERSON, 2026-09-09⟧ ONE VISIT PER PERSON PER ARRIVAL — the
	// list is made a set HERE, before anything is minted. This is the only
	// arrival door whose person list is caller-supplied, and its two siblings
	// are sets by construction: RecordExpectedArrivalExcluding derives its
	// list from trip_occupant rows (PRIMARY KEY (trip_id, person_id)), and a
	// Dispatch carrying `[p, p]` collapses at the fold's INSERT OR IGNORE
	// into that same table. Neither refuses, so neither does this door — the
	// picker offers one person under EVERY peer the record says they are
	// open at (web/picker.go arrivalSections), so two boxes with one value
	// is a shape an officer can submit, and this is the ADD direction, where
	// a refusal is the omission that leaves someone off the muster.
	// Passed through verbatim, `[p, p]` minted TWO visits for one human and
	// the fold's own "cannot be on the same site twice" rule closed the
	// first at birth (MEASURED at cf1d17c: 2 rows, 1 open — a departure the
	// record shows that never happened); at an orientation=true post the
	// duplicate landed twice in NotEntered, trip_arrival_not_entered's plain
	// INSERT hit its primary key, the fold failed, store.Author rolled back,
	// and the door REFUSED the whole arrival, oriented occupants included
	// (MEASURED: "UNIQUE constraint failed"). First occurrence wins, order
	// is kept, nobody else moves. Not at the fold — a fold-side rule forks
	// the logs (W3-2) — and not in the web handler alone: the door is the
	// contract and its other callers never pass through the handler.
	{
		seen := make(map[string]bool, len(personIDs))
		set := make([]string, 0, len(personIDs))
		for _, id := range personIDs {
			if seen[id] {
				continue
			}
			seen[id] = true
			set = append(set, id)
		}
		personIDs = set
	}
	// hasOrientation reaches the SAME choke point RecordExpectedArrivalExcluding
	// uses (buildArrivalVisits, tripauthor.go:525) — the vehicle outran the
	// sync, but the occupants are physically here just the same, so there is
	// no principled reason for this door to enter an un-oriented occupant
	// that the ordinary arrival door would hold. false (the caller wrapper
	// above, every existing test) reproduces today's unconditional-entry
	// shape byte for byte, exactly as buildArrivalVisits's own header
	// documents for hasOrientation=false.
	visits, notEntered, err := buildArrivalVisits(st, siteID, personIDs, hasOrientation)
	if err != nil {
		return Arrival{}, fmt.Errorf("domain: unsolicited arrival: %w", err)
	}
	// ⟦W7-4(d), 2026-09-03⟧ The B-side snapshot, DERIVED from the composed
	// record exactly as the A-side one is (vehicleNumberSnapshot). Nil when
	// this post does not hold the record — §9.2's partition — and the id
	// carries the correlation on its own in that case.
	// ⟦TRIAL — §97/§107⟧ THE CO-MINT, AFTER EVERY VALIDATION AND BEFORE THE FIRST AUTHOR. Above this
	// line the door has refused everything it can refuse; below it, it writes. That is the whole
	// property — a refused arrival must leave no vehicle record behind.
	// SchemaVersion 14 (R-18): skipped entirely when vehicleUnidentified —
	// the refusal above already guarantees resolveVehicle carries no live
	// resolver in that case, so this whole branch is naturally a no-op;
	// stated here so a reader does not have to re-derive it (dispatchTrip's
	// own comment, above, says the same for its own resolver skip).
	if !vehicleUnidentified && vehicleID == "" && len(resolveVehicle) > 0 && resolveVehicle[0] != nil {
		id, rerr := resolveVehicle[0]()
		if rerr != nil {
			// ⟦TRIAL — §114⟧ wrapped in this door's own convention, for the same reason.
			return Arrival{}, fmt.Errorf("domain: unsolicited arrival: %w", rerr)
		}
		if vehicleID = strings.TrimSpace(id); vehicleID == "" {
			return Arrival{}, fmt.Errorf("domain: unsolicited arrival: vehicle id is required (it is the correlation key, §9.2)")
		}
	}
	// SchemaVersion 14 (R-18): snapshot stays nil when vehicleUnidentified —
	// an unidentified arrival has no record to look up and therefore no
	// number to snapshot either (dispatchTrip's identical skip, above, for
	// the departure side).
	var snapshot *string
	if !vehicleUnidentified {
		var err error
		snapshot, err = vehicleNumberSnapshot(st, vehicleID)
		if err != nil {
			return Arrival{}, fmt.Errorf("domain: unsolicited arrival: %w", err)
		}
	}
	// vehicleIDPtr (SchemaVersion 14, R-18): nil, not &"", when
	// vehicleUnidentified — trip_arrival.vehicle_id is already NULLABLE
	// (unlike trip.vehicle_id's locked NOT NULL/"" convention), and NULL is
	// already this column's existing "no vehicle carried" value for every
	// EXPECTED arrival, so an unidentified UNSOLICITED arrival reuses that
	// same representation rather than inventing a second one alongside it.
	var vehicleIDPtr *string
	if !vehicleUnidentified {
		vehicleIDPtr = &vehicleID
	}
	arrivalID := newID(siteID, "arrival")
	payload, err := json.Marshal(TripArrival{
		Type: "trip_arrival", ArrivalID: arrivalID,
		VehicleID: vehicleIDPtr, VehicleUnidentified: vehicleUnidentified,
		VehicleEntered: snapshot, OriginSite: &originSite,
		Visits: visits, NotEntered: notEntered, RecordedBy: recordedBy,
	})
	if err != nil {
		return Arrival{}, fmt.Errorf("domain: unsolicited arrival: %w", err)
	}
	env, err := st.Author(siteID, nodeEpoch, payload, occurredAtMS, timeSource)
	if err != nil {
		return Arrival{}, fmt.Errorf("domain: unsolicited arrival: %w", err)
	}
	// int64, not bool: Arrival.VehicleUnidentified is the projection-read
	// shape (see its own comment) and must equal what a fresh
	// rawArrivalByID/EffectiveArrival call would decode, byte for byte.
	var unidentifiedFlag int64
	if vehicleUnidentified {
		unidentifiedFlag = 1
	}
	return Arrival{ArrivalID: arrivalID, SiteID: siteID, NodeEpoch: env.NodeEpoch, Seq: env.Seq,
		VehicleID: vehicleIDPtr, VehicleUnidentified: unidentifiedFlag,
		VehicleEntered: snapshot, OriginSite: &originSite, RecordedBy: recordedBy,
		NotEntered: notEntered}, nil
}

// CancelTrip authors the A-side cancel (§9.1). Only the origin site may
// cancel its own trip — structurally A-authored. It never touches visit
// state: the occupants stay checked out, and the UI reminds the officer that
// a crew that turned back re-enters through the ordinary gate check-in.
func CancelTrip(st *store.Store, siteID string, nodeEpoch int64, tripID string, recordedBy *string, occurredAtMS int64, timeSource string) error {
	trip, err := TripByID(st, tripID)
	if err != nil {
		return fmt.Errorf("domain: cancel: %w", err)
	}
	if trip == nil {
		return fmt.Errorf("domain: cancel: trip %s is not known at this site", tripID)
	}
	if trip.OriginSite != siteID {
		return fmt.Errorf("domain: cancel: trip %s was authored by %s — cancel is origin-site-authored only (§9.1)", tripID, siteNameAt(st, trip.OriginSite))
	}
	if s, err := TripStateByID(st, tripID); err != nil {
		return fmt.Errorf("domain: cancel: %w", err)
	} else if s != nil {
		switch s.State {
		case TripStateCancelled:
			return fmt.Errorf("domain: cancel: trip %s is already cancelled", tripID)
		case TripStateArrived, TripStateArrivedAfterCancel:
			return fmt.Errorf("domain: cancel: trip %s has already arrived (%s)", tripID, s.State)
		}
	}
	payload, err := json.Marshal(TripCancelled{Type: "trip_cancelled", TripID: tripID, RecordedBy: recordedBy})
	if err != nil {
		return fmt.Errorf("domain: cancel: %w", err)
	}
	if _, err := st.Author(siteID, nodeEpoch, payload, occurredAtMS, timeSource); err != nil {
		return fmt.Errorf("domain: cancel: %w", err)
	}
	return nil
}

// --- reads ---

func Trips(st *store.Store) ([]Trip, error) { return memoSection[Trip](st, "trip") }
func TripOccupants(st *store.Store) ([]TripOccupant, error) {
	return memoSection[TripOccupant](st, "trip_occupant")
}

// --- scoped reads (F30): point lookups and open-set filters for the hot
// paths, so a check-in, a muster render or a deadline scan never pays for a
// table's whole season of rows. Each returns exactly what the same-named
// full dump would deliver for the same key, in the same order. ---

// TripByID resolves one trip by primary key; nil when unknown here.
func TripByID(st *store.Store, tripID string) (*Trip, error) {
	return oneRow[Trip](st, "trip", store.Eq{Column: "trip_id", Value: tripID})
}

// TripStateByID resolves one trip's derived state row; nil when the trip has
// no state row yet (the recompute has not seen a trip-family event for it).
func TripStateByID(st *store.Store, tripID string) (*TripState, error) {
	return oneRow[TripState](st, "trip_state", store.Eq{Column: "trip_id", Value: tripID})
}

// TripOccupantsByTrip lists one trip's occupant rows (PK-prefix indexed).
func TripOccupantsByTrip(st *store.Store, tripID string) ([]TripOccupant, error) {
	return dumpSectionWhere[TripOccupant](st, "trip_occupant", store.Eq{Column: "trip_id", Value: tripID})
}

// VisitByID resolves one visit by primary key; nil when unknown here.
func VisitByID(st *store.Store, visitID string) (*Visit, error) {
	return oneRow[Visit](st, "visit", store.Eq{Column: "visit_id", Value: visitID})
}

// OpenVisitsAt lists siteID's currently open visits — the muster's working
// set (indexed via idx_visit_site_open; scales with on-site headcount, not
// with the season's total visit count).
func OpenVisitsAt(st *store.Store, siteID string) ([]Visit, error) {
	return dumpSectionWhere[Visit](st, "visit",
		store.Eq{Column: "site_id", Value: siteID}, store.Eq{Column: "is_open", Value: 1})
}

// openTripStates lists the trip_state rows currently in the open state — a
// narrow single-table scan whose result set is bounded by trips actually in
// flight (arrived/cancelled trips leave it), unlike anything keyed on the
// season's cumulative trip count.
func openTripStates(st *store.Store) ([]TripState, error) {
	// FIX17 (perf): reused between writes (store.Memo) — every board, the
	// deadline scan and the shift panel read it, several times a request,
	// and trip_state has a row per trip of the season (no index on state:
	// store.go says why). The caller gets its own slice.
	v, err := st.Memo("domain.openTripStates", func() (any, error) {
		return dumpSectionWhere[TripState](st, "trip_state", store.Eq{Column: "state", Value: TripStateOpen})
	})
	if err != nil {
		return nil, err
	}
	return slices.Clone(v.([]TripState)), nil
}

// oneRow adapts a primary-key dumpSectionWhere to 0-or-1 semantics.
func oneRow[T any](st *store.Store, name string, cond store.Eq) (*T, error) {
	rows, err := dumpSectionWhere[T](st, name, cond)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}
func Arrivals(st *store.Store) ([]Arrival, error) { return memoSection[Arrival](st, "trip_arrival") }
func TripCancels(st *store.Store) ([]TripCancel, error) {
	return dumpSection[TripCancel](st, "trip_cancel")
}
func TripStates(st *store.Store) ([]TripState, error) {
	return memoSection[TripState](st, "trip_state")
}
func ArrivalStates(st *store.Store) ([]ArrivalState, error) {
	return dumpSection[ArrivalState](st, "arrival_state")
}
func Visits(st *store.Store) ([]Visit, error) { return memoSection[Visit](st, "visit") }

// Expectation is one row of the inbound board (chunk-plan C6: open
// expectations; contents invented per finding F3): an open trip destined for
// this site. Countdown and staleness are PRESENTATION, computed at render
// time in the web layer — no wall-clock column exists in the store, by
// design; C7 builds the persisted receipt-time scan that owns real deadline
// evaluation.
type Expectation struct {
	Trip  Trip
	State string
	// Vehicle is the run's truck composed LIVE (LiveVehicles, DR-W7-18):
	// every present-tense surface fed from here — the alert strip, the
	// inbound board, the gate inbound row — renders this and never the
	// frozen Trip.VehicleEntered, so it names the same truck as the origin's
	// in-transit block. ⟦James, 2026-10-03⟧
	Vehicle string
	// DriverName falls back to the person id when the roster record has not
	// replicated here — the in-transit block's rule, so the two posts never
	// disagree about who was driving because one of them shows a blank.
	DriverName string
	Occupants  int
	// OccupantList carries each occupant for the per-person arrival
	// disposition (2026-08-20): the arrival form renders one row per
	// occupant so any of them can be marked "stayed behind" instead of the
	// whole vehicle being one check-in-everyone action.
	OccupantList []OccupantRef
}

// OccupantRef is one person on a manifest, resolved for display.
type OccupantRef struct {
	PersonID string
	Name     string // roster name; falls back to the id if unknown here
}

// OpenExpectations lists open trips whose destination is siteID, oldest
// first by source key. Degenerate local trips (§10: destination nil) never
// appear on any board — the same exclusion C7's replay gate relies on when
// it marks every open trip EXPECTATION overdue-unresolved during a
// from-zero replay while leaving degenerate trips untouched.
func OpenExpectations(st *store.Store, siteID string) ([]Expectation, error) {
	return composeOpenExpectations(st, func(c *ComposedTrip) bool {
		return c.Trip.DestinationSite != nil && *c.Trip.DestinationSite == siteID
	})
}

// composeOpenExpectations is OpenExpectations' body with the SELECTION
// predicate lifted out — James, 2026-10-03 (NEW-a): the ORIGIN now scans the
// controlled runs it sent out (OpenOutbound, in_transit.go), and that list
// must be composed by exactly this code — the same live vehicle, the same
// merge-resolved manifest, the same fallback for a driver whose roster row
// has not replicated — or the two posts' alert rows for ONE truck could name
// two different trucks. `keep` sees the COMPOSED trip (the §4 option (c)
// note below: a corrected destination decides which scanner watches it, so
// the predicate must read the effective record, never the raw row).
func composeOpenExpectations(st *store.Store, keep func(c *ComposedTrip) bool) ([]Expectation, error) {
	// Open-first (F30): enumerate the open trip_state rows — a set bounded
	// by trips actually in flight — then point-look-up each trip and keep
	// the ones destined here. The old shape dumped four whole tables (every
	// trip, state, occupant and person of the season) to answer a question
	// whose result set is a handful of rows; this ran on every page render
	// AND every 10-second deadline scan.
	open, err := openTripStates(st)
	if err != nil {
		return nil, err
	}
	personName := map[string]string{}
	// nameOf memoises roster name lookups (F30 posture: point lookups, single
	// digits of people per trip). Falls back to the id for a person unknown
	// here — the manifest row must never vanish just because the roster
	// reference has not synced yet.
	nameOf := func(personID string) (string, error) {
		name, ok := personName[personID]
		if !ok {
			p, err := PersonByID(st, personID)
			if err != nil {
				return "", err
			}
			if p != nil {
				name = p.Name
			}
			personName[personID] = name
		}
		return name, nil
	}
	// ⟦W9a-2 consumer 2/4/5, W9a-15⟧ THIS PROJECTION IS COMPOSED, and it is
	// the load-bearing one: it is the shared input of the deadline scanner,
	// the inbound console manifest and the /trips board rows (F4 §9.1), and
	// it composes EVERY trip fact it reads — driver, occupant count and
	// list, AND the deadline minutes and destination. ⟦was: "list, phase-override face, AND" — weather phases removed, James 2026-10-04⟧
	//
	// ⚠ The destination is composed BEFORE the "is it destined here?"
	// filter, on purpose: a corrected destination decides WHICH POST'S
	// SCANNER WATCHES THE TRIP (§4 option (c)). A build that migrated only
	// the display fields would leave the board promising the new allowance
	// while Banner() waited out the old one — hours in which every trip
	// surface says the vehicle is overdue and no alert row exists on any
	// screen (W9a-15, mutant m18).
	//
	// ⚠ This is the SELECTION half of §9.1's derivation boundary, and it is
	// NOT the correlation half: `computeStates`' Pass-2 candidacy stays RAW,
	// always (W9a-7). The two were named separately precisely so this one
	// could compose without the other.
	merge, err := LoadPersonMergeMap(st)
	if err != nil {
		return nil, err
	}
	live, err := NewLiveVehicles(st)
	if err != nil {
		return nil, err
	}
	var out []Expectation
	for _, s := range open {
		c, err := EffectiveTrip(st, s.TripID)
		if err != nil {
			return nil, err
		}
		if c == nil || !keep(c) {
			continue
		}
		t := &c.Trip
		// F15 (James, 2026-10-05): StillAboard, not c.OccupantIDs — a
		// person back at the origin is off this run on every surface fed
		// from here, exactly as the muster has them off it.
		aboard, err := StillAboard(st, c)
		if err != nil {
			return nil, err
		}
		occList, err := composedOccupantRefs(st, aboard, merge, nameOf)
		if err != nil {
			return nil, err
		}
		name, err := nameOf(t.DriverPersonID)
		if err != nil {
			return nil, err
		}
		if name == "" {
			name = t.DriverPersonID
		}
		vehicle, _, err := live.Compose(*c)
		if err != nil {
			return nil, err
		}
		out = append(out, Expectation{
			Trip: *t, State: TripStateOpen,
			Vehicle:      vehicle,
			DriverName:   name,
			Occupants:    len(occList),
			OccupantList: occList,
		})
	}
	// Oldest first, by the trip event's source key (row 10) — never rowid,
	// never wall-time. Cross-site epoch/seq comparison is only a display
	// order here, not a correctness decision.
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Trip, out[j].Trip
		if a.NodeEpoch != b.NodeEpoch {
			return a.NodeEpoch < b.NodeEpoch
		}
		if a.Seq != b.Seq {
			return a.Seq < b.Seq
		}
		return a.TripID < b.TripID
	})
	return out, nil
}

// ReviewLane is everything a human must resolve (chunk-plan C6): trips that
// arrived after their cancel (§9.1 — never auto-resolved), ambiguous
// unsolicited arrivals (§9.2 — never auto-picked), and unsolicited arrivals
// whose only match is a CANCELLED trip (the same §9.1 contradiction arriving
// over a severed link; C6 review, Ruling 2).
type ReviewLane struct {
	ArrivedAfterCancel []TripState
	Ambiguous          []ArrivalState
	CancelledMatches   []ArrivalState
	// ⟦W9b, item 16⟧ RefRetracted: arrivals whose TripRef selection a human
	// withdrew (F4 §3.6). A row type of its own, not another []ArrivalState,
	// because the row must name the RETRACTING POST and that lives in
	// arrival_ref_retraction.recorded_by — see loadRefRetractedLane.
	RefRetracted []RefRetractedRow
}

func LoadReviewLane(st *store.Store) (ReviewLane, error) {
	// Filtered scans (F30): only the review-lane rows are decoded; each list
	// arrives in the same per-table OrderBy order the full dump gave it.
	var lane ReviewLane
	var err error
	lane.ArrivedAfterCancel, err = dumpSectionWhere[TripState](st, "trip_state",
		store.Eq{Column: "state", Value: TripStateArrivedAfterCancel})
	if err != nil {
		return lane, err
	}
	lane.Ambiguous, err = dumpSectionWhere[ArrivalState](st, "arrival_state",
		store.Eq{Column: "disposition", Value: ArrivalAmbiguous})
	if err != nil {
		return lane, err
	}
	lane.CancelledMatches, err = dumpSectionWhere[ArrivalState](st, "arrival_state",
		store.Eq{Column: "disposition", Value: ArrivalCancelledMatch})
	if err != nil {
		return lane, err
	}
	// ⟦W9b, item 16⟧ the FOURTH filtered scan, in arrival_unref.go because it
	// joins the retraction table's attribution to the lane row.
	lane.RefRetracted, err = loadRefRetractedLane(st)
	if err != nil {
		return lane, err
	}
	return lane, nil
}

// VisitsByTrip lists the visit rows a trip's explicit arrival created
// (visit.trip_id records only the authoring-time explicit reference).
func VisitsByTrip(st *store.Store, tripID string) ([]Visit, error) {
	return dumpSectionWhere[Visit](st, "visit", store.Eq{Column: "trip_id", Value: tripID})
}

// UncontrolledOut is one uncontrolled run with the people the record says
// are still out at its pad.
type UncontrolledOut struct {
	TripID  string
	Pad     string // destination_uncontrolled, verbatim configured id
	Vehicle string // verbatim entered form (M8)
	// OriginSite: the post that RECORDED the departure — trip.origin_site.
	// Carried (2026-08-21) because it is the only post with any relationship
	// to this run: an uncontrolled destination is not a post, so there is no
	// second site with a stream, a gate or an officer at the far end. See
	// OutAtUncontrolledFrom below, which is what the muster renders.
	OriginSite string
	Persons    []OccupantRef
}

// OutAtUncontrolled answers "who is out at the pads" (James, 2026-08-20:
// "if something happens at that pad, who is out there must be answerable").
// A person is RECORDED AS OUT at a pad when they are an occupant of a run to
// it (trip.destination_uncontrolled + trip_occupant — which is also what
// makes them DISTINGUISHABLE from a person who simply checked out: a plain
// check-out has no run membership) and no RECORDED RETURN exists.
//
// THE "HASN'T COME BACK" PREDICATE, exactly (an uncontrolled run's
// trip_state stays open forever by design, so trip state can NEVER answer
// this — the answer is derived from visits alone). A person on run R
// (origin O, source key K = R's (node_epoch, seq)) is RECORDED BACK iff any
// of:
//  1. a visit with visit.trip_id = R exists, open or since closed — the
//     run's own return manifest checked them in
//     (RecordExpectedArrivalExcluding). Closure must not matter: someone
//     who returned on the bus and later went home is not at the pad.
//  2. a visit exists (any site, open or closed) whose trip_id resolves to
//     a trip with origin O and source key > K — a check-in created by a
//     LATER event on the SAME stream as R (a walk-in at the origin, or a
//     later O-originated trip that carried them onward). Same-stream
//     (node_epoch, seq) is true authoring order (§8.3), so "after that
//     run" is exact here — never rowid, never wall-time, and never a
//     cross-site seq comparison.
//  3. an OPEN visit exists at R's ORIGIN O right now (covers a return
//     recorded only as an unsolicited arrival AT THE ORIGIN, whose visit
//     carries no trip reference to order by). Open visits at O are the one
//     open set that is provably POST-RUN: R's own dispatch fold closed
//     every open O-visit its occupants held, in the same transaction that
//     recorded R (trip.go, applyTripDispatched), and O-site visit rows are
//     only ever mutated by O's own contiguous stream — so any replica that
//     contains R at all contains those closures, and an open O-visit for
//     an occupant postdates R on O's stream. Same-stream order again, just
//     carried by the fold instead of a key comparison. An open visit at
//     any OTHER site proves nothing here: its creation is unorderable
//     against R (cross-stream), and its OPENNESS is exactly what a
//     partition falsifies — the peer's closing event may be stranded on
//     the far side, or never have been recorded at all. Reading such a row
//     as "demonstrably back" hid a pad-run occupant from EVERY muster
//     category at both sites (F35 / t2-muster F-T2M-1, 2026-08-20) — the
//     one direction this view exists to make impossible.
//
// Deliberate asymmetry, the safe direction: a sighting that cannot be
// ordered after the run does not suppress the listing. A return recorded
// ONLY via an unsolicited arrival at the origin (rule 3) stops counting
// once that visit closes — closed and unorderable is not proof they came
// back after the run — so the person RE-APPEARS on the pad list. A
// sighting recorded only at the PEER never counts, open or closed, so a
// person collected from the pad who turns up at the peer unannounced stays
// on the pad list — an extra name beside the peer's own on-site listing of
// them (their local open visit), never an omission. Erring puts someone ON
// the rescue list; the unforgivable error is leaving someone off it. The
// rendering surface states this limit: no gate at the pad, so this is
// "recorded as out there, with no recorded return" — not a live headcount.
//
// SCOPE (2026-08-21): this is EVERY post's pad runs in this replica, which
// is NOT what a muster wants. The muster renders OutAtUncontrolledFrom
// below — scoped to the post that recorded the departure, per James's
// ruling. Read that function's comment before pointing a new life-safety
// surface at this one.
func OutAtUncontrolled(st *store.Store) ([]UncontrolledOut, error) {
	trips, err := Trips(st)
	if err != nil {
		return nil, err
	}
	// ⟦W9a-2 consumer 7 (and 8, transitively via OutAtUncontrolledFrom)⟧
	// COMPOSED, merge-resolved at the read — same posture as NotTracked.
	merge, err := LoadPersonMergeMap(st)
	if err != nil {
		return nil, err
	}
	var out []UncontrolledOut
	personName := map[string]string{}
	nameOf := func(personID string) (string, error) {
		name, ok := personName[personID]
		if !ok {
			p, err := PersonByID(st, personID)
			if err != nil {
				return "", err
			}
			if p != nil {
				name = p.Name
			}
			personName[personID] = name
		}
		return name, nil
	}
	for _, t := range trips {
		if t.DestinationUncontrolled == nil {
			continue
		}
		c, err := EffectiveTrip(st, t.TripID)
		if err != nil {
			return nil, err
		}
		if c == nil {
			continue
		}
		returned := map[string]bool{}
		visits, err := VisitsByTrip(st, t.TripID)
		if err != nil {
			return nil, err
		}
		for _, v := range visits {
			returned[v.PersonID] = true
		}
		var stillRaw []string
		for _, id := range c.OccupantIDs {
			if returned[id] {
				continue // rule 1: the run's own return manifest
			}
			back, err := sightedSince(st, id, t.OriginSite, t.NodeEpoch, t.Seq)
			if err != nil {
				return nil, err
			}
			if back {
				continue // rules 2/3
			}
			stillRaw = append(stillRaw, id)
		}
		still, err := composedOccupantRefs(st, stillRaw, merge, nameOf)
		if err != nil {
			return nil, err
		}
		if len(still) == 0 {
			continue
		}
		// The composed occupant set is emitted in sorted id order — already
		// deterministic; runs are ordered below by source key (row 10).
		// The SELECTION stays raw here (this section is "runs this post
		// recorded to a pad"); only the rendered pad follows a correction,
		// and it falls back to the raw one if an amendment re-pointed the
		// trip to a controlled post — the row must never carry a nil.
		pad := *t.DestinationUncontrolled
		if c.Trip.DestinationUncontrolled != nil {
			pad = *c.Trip.DestinationUncontrolled
		}
		row := UncontrolledOut{
			TripID: t.TripID, Pad: pad,
			// ⟦LOCK — W7 seam repair, 2026-09-03⟧ ⟦was: "Vehicle: t.VehicleEntered,"⟧
			Vehicle: tripVehicleEnteredDisplay(c.Trip), OriginSite: t.OriginSite, Persons: still,
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return outLessKey(trips, out[i].TripID, out[j].TripID) })
	return out, nil
}

// sightedSince is rules 2 and 3 of the "hasn't come back" predicate (see
// OutAtUncontrolled's doc), shared with NotTracked so the two away-view
// categories can never disagree about what counts as a recorded return:
// true iff the person has an OPEN visit at the trip's ORIGIN site right now
// (rule 3: provably post-trip — the trip's own dispatch fold closed every
// earlier origin visit its occupants held, see OutAtUncontrolled's doc), or
// any visit — open or closed, any site — created by a trip on the SAME
// stream as (origin, epoch, seq) with a higher source key (rule 2:
// same-stream (node_epoch, seq) is true authoring order, §8.3; never a
// cross-site comparison). Everything else is unorderable against the trip —
// closed visits with no trip reference, and ALL non-origin visits without
// one, whose openness a partition can falsify (a stranded peer check-out
// left one reading "on site" for the whole outage: F35 / F-T2M-1) — and
// deliberately does NOT count: the asymmetry that errs toward listing.
func sightedSince(st *store.Store, personID, origin string, nodeEpoch, seq int64) (bool, error) {
	visits, err := dumpSectionWhere[Visit](st, "visit",
		store.Eq{Column: "person_id", Value: personID})
	if err != nil {
		return false, err
	}
	for _, v := range visits {
		if v.IsOpen == 1 && v.SiteID == origin {
			return true, nil // rule 3: back at the origin, provably post-trip
		}
		if v.TripID == nil {
			continue // closed and unorderable — not proof
		}
		vt, err := TripByID(st, *v.TripID)
		if err != nil {
			return false, err
		}
		if vt != nil && vt.OriginSite == origin &&
			(vt.NodeEpoch > nodeEpoch ||
				(vt.NodeEpoch == nodeEpoch && vt.Seq > seq)) {
			return true, nil // rule 2: later same-stream check-in
		}
	}
	return false, nil
}

// NotTrackedRow is one trip that arrived without some of its occupants:
// people who left ON a vehicle and did NOT arrive with it, with no recorded
// whereabouts (operator ruling, 2026-08-20: show them as "not tracked",
// never let an officer type a location the system has no basis for — state
// uncertainty rather than absorb it). This is a GAP IN KNOWLEDGE, not a
// status: routine for a drop-off at a turnoff, but nobody is watching and
// no timer will ever fire.
//
// Distinct from a plain check-out BY CONSTRUCTION: membership here requires
// a trip_occupant row on an ARRIVED trip whose visit set omits the person —
// someone who signed out at the gate and drove home has no trip membership
// and can never appear. Distinct from the named-pad category by the trip's
// destination kind: a run to a configured pad records WHERE they are
// (OutAtUncontrolled); this category is exactly the trips where the record
// cannot say.
type NotTrackedRow struct {
	TripID      string
	Vehicle     string // verbatim (M8)
	Destination string // where the vehicle was going (destination_site)
	Persons     []OccupantRef
	// TakenOffByCorrection (James, 2026-10-04: D1 ruling, the demo hotfix's
	// semantics): these persons were dispatch occupants a trip correction
	// took off the record (a replaced driver, an occupants_remove), not
	// people who stayed behind at arrival. Listed from the moment the
	// correction is recorded.
	TakenOffByCorrection bool
}

// NotTracked lists, per arrived controlled trip, the occupants who were not
// aboard at arrival and have no recorded sighting since (sightedSince's
// rules 2/3 — the same recorded-return arithmetic as the pad list). Scope,
// deliberately:
//   - controlled trips only (destination_site set): a degenerate walk-in or
//     a pad run is not a journey that can silently shed people en route —
//     pad runs land in OutAtUncontrolled instead;
//   - arrived trips only (arrived / arrived_after_cancel): an OPEN trip's
//     occupants are the deadline machinery's business, and a CANCELLED
//     trip's occupants never travelled on it;
//   - a trip closed by a CORRELATED or RESOLVED (unsolicited) arrival
//     carries no visit.trip_id link; its aboard set is the people that
//     arrival itself checked in (its own visit rows, by envelope key) or
//     held out (aboardOnTripArrivals, R3-07). Occupants it did NOT enter
//     stay listed once off-site — the conservative direction, same as the
//     pad list's asymmetry. ⟦was: "its aboard set is unknowable here —
//     occupants stay listed once off-site (rule 3 covers them while on
//     site)" — false for the entered ones: rule 3 is an open visit at the
//     ORIGIN, so a person on site at the destination was listed as not
//     aboard on the very sheet that had them on site⟧
//   - EXCEPT a trip's DROPPED occupants. James, 2026-10-04: people in
//     raw∖effective are listed in NotTracked from the moment the correction
//     is recorded and are never entered at the destination (FINDINGS-R4 D1
//     ruling, the demo hotfix's semantics) — so they are listed for any open
//     or arrived trip, controlled, pad or road work; see the loop.
//
// FIX17 (perf): reused between writes (store.Memo) — it reads every trip of
// the season, and the muster pages (live, print, the 30 s refresh) call it
// on every render. The caller gets its own rows and Persons slices.
func NotTracked(st *store.Store) ([]NotTrackedRow, error) {
	v, err := st.Memo("domain.NotTracked", func() (any, error) { return notTracked(st) })
	if err != nil {
		return nil, err
	}
	shared := v.([]NotTrackedRow)
	if shared == nil {
		return nil, nil
	}
	out := make([]NotTrackedRow, len(shared))
	for i, r := range shared {
		r.Persons = slices.Clone(r.Persons)
		out[i] = r
	}
	return out, nil
}

func notTracked(st *store.Store) ([]NotTrackedRow, error) {
	trips, err := Trips(st)
	if err != nil {
		return nil, err
	}
	// ⟦W9a-2 consumer 6, W9a-18⟧ COMPOSED, and merge-RESOLVED at the read.
	// The merge map is loaded ONCE, outside the per-trip loop: it is a
	// whole-table derivation and this loop already runs per trip of the
	// season.
	merge, err := LoadPersonMergeMap(st)
	if err != nil {
		return nil, err
	}
	// James, 2026-10-03: a road-work run (road_work.go) is admitted beside
	// the controlled trips. It is destination-less, but unlike a pad run or a
	// degenerate check-in it IS a journey that closes with an observed
	// arrival (back at the origin, or at another post), so "left on it and
	// was not aboard when it came in" is a real, answerable gap — listed, the
	// conservative direction, with the destination column saying what the
	// run was rather than where it went.
	roadWork, err := roadWorkTripIDs(st)
	if err != nil {
		return nil, err
	}
	// FIX17 (perf): every trip's state in ONE read, not a point lookup per
	// trip — a season holds a trip per gate check-in, so the loop below runs
	// ~14k times at season scale. Same rows (trip_state), same values.
	allStates, err := TripStates(st)
	if err != nil {
		return nil, err
	}
	stateOf := make(map[string]*TripState, len(allStates))
	for i := range allStates {
		stateOf[allStates[i].TripID] = &allStates[i]
	}
	// FIX17 (sc-stress F1/F8): every per-trip read below is served from ONE
	// read of each table, grouped here, instead of a query per arrived run
	// (~6 per run, ~750 runs at season scale: ~4.5k queries on the store's
	// one connection per muster, which under five officers and a check-in
	// queued for seconds). Each map holds exactly the rows, in the order,
	// the scoped read it replaces returns. The trip amendments stay scoped
	// reads, taken only for the trips that have any (notTrackedReads).
	pre, err := loadNotTrackedReads(st, trips)
	if err != nil {
		return nil, err
	}
	var out []NotTrackedRow
	personName := map[string]string{}
	nameOf := func(personID string) (string, error) {
		name, ok := personName[personID]
		if !ok {
			p, err := PersonByID(st, personID)
			if err != nil {
				return "", err
			}
			if p != nil {
				name = p.Name
			}
			personName[personID] = name
		}
		return name, nil
	}
	for _, t := range trips {
		state := stateOf[t.TripID]
		if state == nil {
			continue
		}
		arrived := state.State == TripStateArrived || state.State == TripStateArrivedAfterCancel
		// James, 2026-10-04 (D1 ruling): an OPEN trip, or one to an
		// uncontrolled pad, is read too — but only for its DROPPED occupants
		// (below); an arrived controlled or road-work trip as before.
		if !arrived && state.State != TripStateOpen {
			continue
		}
		journey := t.DestinationSite != nil || roadWork[t.TripID]
		if !journey && t.DestinationUncontrolled == nil {
			continue // a gate check-in is not a journey anyone was dropped from
		}
		// ONE scoped composition read per trip. Two — one for the trip's
		// facts and one for its occupants — would double the decode on the
		// muster path for nothing (F4 §3.3's measured cost).
		c, err := pre.effectiveTrip(st, t.TripID)
		if err != nil {
			return nil, err
		}
		if c == nil {
			continue
		}
		// DROPPED occupants (James, 2026-10-04, D1 ruling — the demo
		// hotfix's semantics): raw dispatch rows the composed record no
		// longer has aboard — a driver replaced by a correction that also
		// took them off the list (rung 9 otherwise keeps them aboard), anyone
		// removed by occupants_remove. The dispatch closed their origin
		// visit, the arrival (which enters the EFFECTIVE set) never enters
		// them, and in-transit lists only the effective set — so without
		// this they were on NO muster from the moment the correction was
		// recorded. They leave by the same recorded-return rule
		// (sightedSince); a re-correction that puts them back aboard makes
		// them effective again — out of this set, back into in-transit.
		raw := pre.occupants[t.TripID]
		isEffective := make(map[string]bool, len(c.OccupantIDs))
		for _, id := range c.OccupantIDs {
			isEffective[id] = true
		}
		var candidates []string
		if arrived && journey {
			candidates = append(candidates, c.OccupantIDs...)
		}
		for _, o := range raw {
			if !isEffective[o.PersonID] {
				candidates = append(candidates, o.PersonID)
			}
		}
		if len(candidates) == 0 {
			continue
		}
		aboard := map[string]bool{}
		for _, v := range pre.visitsByTrip[t.TripID] {
			aboard[v.PersonID] = true
		}
		// ⟦H-8 / T-084 / M-37, 2026-09-29⟧ d91-arrival-respec.md §3.3:
		// "aboard ⇐ VisitsByTrip(trip) ∪ trip_arrival_not_entered rows of
		// the trip's arrival(s)". A person held out at the destination's
		// gate for lack of its orientation (D-91/D-98) WAS aboard — that is
		// the officer's own D-3 observation the row records — and the post
		// knows exactly where they are (its /orientation held-out list,
		// heldout.go). Until this read they were printed on the evacuation
		// sheet at EVERY post as "not aboard at arrival — whereabouts not
		// recorded": the hedged-NOT-TRACKED rendering D-98 rejected and
		// R-11 left rejected. Per D-91/D-10/R-11 they belong on NO muster
		// sheet — /orientation is their one surface — so they are simply
		// aboard here and never reach missingRaw. The genuine not-aboard
		// case (stayed behind, no row) is untouched. ⟦was: aboard built from
		// VisitsByTrip alone⟧
		// ⟦R3-07, 2026-09-30⟧ and the binding arrival's OWN visits: a trip
		// closed by a correlated or resolved UNSOLICITED arrival has its
		// crew's visits under trip_id NULL, so VisitsByTrip above finds
		// nothing and the people that arrival checked in were filed here
		// while standing on the same sheet's on-site list. See
		// aboardOnTripArrivals. ⟦was: notEnteredOnTripArrivals — the
		// not_entered rows alone⟧
		accounted := pre.aboardOnTripArrivals(t.TripID, state)
		for id := range accounted {
			aboard[id] = true
		}
		// Aboard-ness and sighting are judged per RAW id, because visit rows
		// carry raw ids; identity is resolved afterwards, over the survivors.
		// The dropped and the not-aboard are two ROWS of the same trip, so
		// each carries true wording: "not aboard at arrival" is false of a
		// person a correction took off a run that has not arrived yet.
		var missingRaw, droppedRaw []string
		for _, id := range candidates {
			if aboard[id] {
				continue
			}
			if pre.sightedSince(id, t.OriginSite, t.NodeEpoch, t.Seq) {
				continue
			}
			if isEffective[id] {
				missingRaw = append(missingRaw, id)
			} else {
				droppedRaw = append(droppedRaw, id)
			}
		}
		if len(missingRaw) == 0 && len(droppedRaw) == 0 {
			continue
		}
		// The rendered destination follows a correction and falls back to
		// the raw one, so the row can never carry a nil.
		var destination string
		switch {
		case c.Trip.DestinationSite != nil:
			destination = *c.Trip.DestinationSite
		case t.DestinationSite != nil:
			destination = *t.DestinationSite
		case roadWork[t.TripID]:
			destination = RoadWorkDestinationText // a road-work run: there is no post to name
		case c.Trip.DestinationUncontrolled != nil:
			destination = *c.Trip.DestinationUncontrolled
		case t.DestinationUncontrolled != nil:
			destination = *t.DestinationUncontrolled
		}
		for _, part := range []struct {
			raw       []string
			corrected bool
		}{{missingRaw, false}, {droppedRaw, true}} {
			if len(part.raw) == 0 {
				continue
			}
			missing, err := composedOccupantRefs(st, part.raw, merge, nameOf)
			if err != nil {
				return nil, err
			}
			if len(missing) == 0 {
				continue
			}
			row := NotTrackedRow{
				// ⟦LOCK — W7 seam repair, 2026-09-03⟧ ⟦was: "Vehicle: t.VehicleEntered,"⟧
				TripID: t.TripID, Vehicle: tripVehicleEnteredDisplay(c.Trip),
				Destination: destination, Persons: missing,
				TakenOffByCorrection: part.corrected,
			}
			out = append(out, row)
		}
	}
	// Oldest-first by source key, like every other surface (row 10).
	sort.Slice(out, func(i, j int) bool {
		return outLessKey(trips, out[i].TripID, out[j].TripID)
	})
	return out, nil
}

// aboardOnTripArrivals is NotTracked's second aboard source: the RAW person
// ids the trip's arrival(s) themselves account for — d91-arrival-respec.md
// §3.3's trip_arrival_not_entered rows (⟦H-8, 2026-09-29⟧: aboard but held
// out for orientation) and, ⟦R3-07, 2026-09-30⟧, the arrival's OWN VISIT
// ROWS: the visits its fold minted, found by the arrival's envelope key
// (site_id, node_epoch, seq — SchemaVersion 15 stamps every visit with the
// envelope that minted it, trip.go's mintVisitFromArrivalVisit). For an
// EXPECTED arrival those visits already carry trip_id and VisitsByTrip found
// them; for an UNSOLICITED arrival that came to bind the trip — Pass 2
// correlation, or an officer's resolution of a parked row ("This was trip
// X", arrival_resolve.go) — they carry trip_id NULL and a resolution never
// touches visits (I-1), so before this read the very people that arrival
// checked in were ON SITE and, on the same sheet at every post, "not aboard
// at arrival — whereabouts not recorded" (lanes-r3/L08.md R3-07, R3-12 b).
// Occupants the binding arrival did NOT enter still fall through to
// sightedSince and stay listed once off-site: the conservative direction,
// unchanged.
//
// "The trip's arrival(s)" is two lookups, both needed: every trip_arrival
// row whose trip_ref names the trip (an EXPECTED arrival, either console or
// gate door), and the arrival trip_state binds the trip to (state.ArrivalID),
// which for a trip closed by a correlated or resolved unsolicited arrival is
// a row with NO trip_ref — the only way to reach that arrival from the trip.
// The two coincide for an expected arrival and are deduplicated. Read from
// the same projection tables HeldOutAt reads, by point lookup, so this and
// /orientation's list cannot disagree about who was held out by which
// arrival; and from the visit projection every post rebuilds from the same
// raw tables, so every post and every replay order answers alike.
func aboardOnTripArrivals(st *store.Store, tripID string, state *TripState) (map[string]bool, error) {
	arrivalIDs := map[string]bool{}
	refs, err := dumpSectionWhere[Arrival](st, "trip_arrival", store.Eq{Column: "trip_ref", Value: tripID})
	if err != nil {
		return nil, err
	}
	for _, a := range refs {
		arrivalIDs[a.ArrivalID] = true
	}
	if state != nil && state.ArrivalID != nil && *state.ArrivalID != "" {
		arrivalIDs[*state.ArrivalID] = true
	}
	out := map[string]bool{}
	for _, id := range sortedKeys(arrivalIDs) {
		a, err := oneRow[Arrival](st, "trip_arrival", store.Eq{Column: "arrival_id", Value: id})
		if err != nil {
			return nil, err
		}
		if a != nil {
			visits, err := dumpSectionWhere[Visit](st, "visit",
				store.Eq{Column: "site_id", Value: a.SiteID},
				store.Eq{Column: "node_epoch", Value: a.NodeEpoch},
				store.Eq{Column: "seq", Value: a.Seq})
			if err != nil {
				return nil, err
			}
			for _, v := range visits {
				out[v.PersonID] = true
			}
		}
		rows, err := dumpSectionWhere[ArrivalNotEntered](st, "trip_arrival_not_entered", store.Eq{Column: "arrival_id", Value: id})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out[r.PersonID] = true
		}
	}
	return out, nil
}

// OutAtUncontrolledFrom is OutAtUncontrolled narrowed to the runs THIS post
// recorded — the answer the muster renders.
//
// Ruling from James, 2026-08-21: "on the pad muster roster it should not
// display the persons going to the other site — the only one that would have
// any concern about them is the ice road site control." Trips replicate, so
// before this every post rendered every post's pad runs: the Pad's muster
// listed people who left Ice Road for Conoco's pad. That is meaningless
// there. Those people never came to the Pad and never will on that run; the
// Pad has no relationship to the trip at all.
//
// This is the SAME mechanism, for the same class of reason, as
// IncompleteCheckIns (checkin.go): take the site, keep what this site
// ORIGINATED. Two things make origin the right and only anchor:
//
//   - an uncontrolled destination IS NOT A POST. There is no second stream,
//     gate or officer at the far end — it is a config id. So unlike a
//     controlled run, which two posts genuinely share, a pad run is by
//     construction an event of exactly ONE post: the one that recorded the
//     departure. D-4's topology says the same from the other side (Ice Road
//     is the only entrance; the Pad is downstream of it), but the argument
//     does not depend on the topology — it holds for any pad run anywhere.
//   - the origin's own stream always holds the dispatch it authored, so this
//     section is never empty merely because the link is down. That is the
//     same property in_transit.go's InTransitFrom relies on, and it is why
//     origin-owned beats destination-owned for anything a muster prints.
//
// WHY THIS IS A SEPARATE FUNCTION rather than a parameter added to
// OutAtUncontrolled: that predicate has two other callers — /trips' "still
// out at the pads" panel and the person picker's "at a pad" group — and
// James ruled about the MUSTER ROSTER. Silently narrowing those two would
// be a scope decision nobody made: the /trips panel mirrors a trip table
// that is itself unscoped (narrowing one and not the other invents a NEW
// inconsistency), and the picker offers a plausible crew for an arriving
// vehicle, where somebody collected from ANY pad is a legitimate guess.
// Both are flagged in reports/MUSTER-RESPONSIBILITY-SCOPE.md for a ruling.
// The filter still lives here, in the domain, rather than in the muster's
// view model: the scoping is then a property of the ANSWER, and a second
// surface cannot reintroduce the peer's pad crew by forgetting to filter.
//
// NOT applied to NotTracked, deliberately and pending a ruling: that row
// names a trip with a real DESTINATION POST, which authored the "not aboard
// at arrival" observation itself, so both ends have a genuine relationship
// to it. Scoping it by origin would be an assumption, not a consequence of
// this ruling. Same report.
func OutAtUncontrolledFrom(st *store.Store, siteID string) ([]UncontrolledOut, error) {
	all, err := OutAtUncontrolled(st)
	if err != nil {
		return nil, err
	}
	var out []UncontrolledOut
	for _, o := range all {
		if o.OriginSite == siteID {
			out = append(out, o)
		}
	}
	return out, nil
}

// outLessKey orders away-view rows oldest-first by their trip's
// (node_epoch, seq, trip_id) source key — never rowid, never wall-time.
func outLessKey(trips []Trip, idA, idB string) bool {
	key := func(id string) (int64, int64, string) {
		for _, t := range trips {
			if t.TripID == id {
				return t.NodeEpoch, t.Seq, t.TripID
			}
		}
		return 0, 0, id
	}
	ae, as, ai := key(idA)
	be, bs, bi := key(idB)
	if ae != be {
		return ae < be
	}
	if as != bs {
		return as < bs
	}
	return ai < bi
}

// notTrackedReads is NotTracked's per-trip reads, each served from one read
// of its table (FIX17; see notTracked). Every lookup returns exactly what
// the scoped read named beside it returns: the same rows, in the same order
// (each table is read in its projection's OrderBy, and grouping keeps it).
type notTrackedReads struct {
	trips        map[string]*Trip               // TripByID
	occupants    map[string][]TripOccupant      // TripOccupantsByTrip
	roadWork     map[string]bool                // IsRoadWork
	amended      map[string]bool                // trips with any trip_amendment row
	visitsByTrip map[string][]Visit             // VisitsByTrip
	visitsByKey  map[store.Key][]Visit          // visit WHERE site_id, node_epoch, seq
	visitsByWho  map[string][]Visit             // visit WHERE person_id
	arrivalByID  map[string]*Arrival            // trip_arrival WHERE arrival_id
	arrivalsRef  map[string][]Arrival           // trip_arrival WHERE trip_ref
	notEntered   map[string][]ArrivalNotEntered // trip_arrival_not_entered WHERE arrival_id
}

func loadNotTrackedReads(st *store.Store, trips []Trip) (*notTrackedReads, error) {
	r := &notTrackedReads{trips: make(map[string]*Trip, len(trips)), occupants: map[string][]TripOccupant{},
		amended: map[string]bool{}, visitsByTrip: map[string][]Visit{}, visitsByKey: map[store.Key][]Visit{},
		visitsByWho: map[string][]Visit{}, arrivalByID: map[string]*Arrival{}, arrivalsRef: map[string][]Arrival{},
		notEntered: map[string][]ArrivalNotEntered{}}
	for i := range trips {
		r.trips[trips[i].TripID] = &trips[i]
	}
	occ, err := TripOccupants(st)
	if err != nil {
		return nil, err
	}
	for _, o := range occ {
		r.occupants[o.TripID] = append(r.occupants[o.TripID], o)
	}
	if r.roadWork, err = roadWorkTripIDs(st); err != nil {
		return nil, err
	}
	// Which trips carry amendments — the ids only, so no amendment row is
	// decoded here; each such trip's rows are then read scoped, exactly as
	// EffectiveTrip reads them (W9a-16(a): a trip's own rows, never the
	// table's).
	if err := st.LocalRead(func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT DISTINCT trip_id FROM trip_amendment`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id sql.NullString
			if err := rows.Scan(&id); err != nil {
				return err
			}
			r.amended[id.String] = true
		}
		return rows.Err()
	}); err != nil {
		return nil, err
	}
	visits, err := Visits(st)
	if err != nil {
		return nil, err
	}
	for _, v := range visits {
		if v.TripID != nil {
			r.visitsByTrip[*v.TripID] = append(r.visitsByTrip[*v.TripID], v)
		}
		k := store.Key{SiteID: v.SiteID, NodeEpoch: v.NodeEpoch, Seq: v.Seq}
		r.visitsByKey[k] = append(r.visitsByKey[k], v)
		r.visitsByWho[v.PersonID] = append(r.visitsByWho[v.PersonID], v)
	}
	arrivals, err := Arrivals(st)
	if err != nil {
		return nil, err
	}
	for i := range arrivals {
		a := &arrivals[i]
		r.arrivalByID[a.ArrivalID] = a
		if a.TripRef != nil {
			r.arrivalsRef[*a.TripRef] = append(r.arrivalsRef[*a.TripRef], *a)
		}
	}
	ne, err := dumpSection[ArrivalNotEntered](st, "trip_arrival_not_entered")
	if err != nil {
		return nil, err
	}
	for _, n := range ne {
		r.notEntered[n.ArrivalID] = append(r.notEntered[n.ArrivalID], n)
	}
	return r, nil
}

// effectiveTrip is EffectiveTrip over the prefetched rows: the same
// composition (composeTrip) of the same trip row, occupants, amendment rows
// and road-work kind.
func (r *notTrackedReads) effectiveTrip(st *store.Store, tripID string) (*ComposedTrip, error) {
	t := r.trips[tripID]
	if t == nil {
		return nil, nil
	}
	var rows []tripAmendmentRow
	if r.amended[tripID] {
		var err error
		if rows, err = readTripAmendmentsFor(st, tripID); err != nil {
			return nil, err
		}
	}
	rawIDs := make([]string, 0, len(r.occupants[tripID]))
	for _, o := range r.occupants[tripID] {
		rawIDs = append(rawIDs, o.PersonID)
	}
	out, err := composeTrip(st, *t, rawIDs, rows)
	if err != nil || out == nil {
		return out, err
	}
	out.RoadWork = r.roadWork[tripID]
	return out, nil
}

// aboardOnTripArrivals is the function of that name over the prefetched rows.
func (r *notTrackedReads) aboardOnTripArrivals(tripID string, state *TripState) map[string]bool {
	arrivalIDs := map[string]bool{}
	for _, a := range r.arrivalsRef[tripID] {
		arrivalIDs[a.ArrivalID] = true
	}
	if state != nil && state.ArrivalID != nil && *state.ArrivalID != "" {
		arrivalIDs[*state.ArrivalID] = true
	}
	out := map[string]bool{}
	for _, id := range sortedKeys(arrivalIDs) {
		if a := r.arrivalByID[id]; a != nil {
			for _, v := range r.visitsByKey[store.Key{SiteID: a.SiteID, NodeEpoch: a.NodeEpoch, Seq: a.Seq}] {
				out[v.PersonID] = true
			}
		}
		for _, n := range r.notEntered[id] {
			out[n.PersonID] = true
		}
	}
	return out
}

// sightedSince is the function of that name over the prefetched rows.
func (r *notTrackedReads) sightedSince(personID, origin string, nodeEpoch, seq int64) bool {
	for _, v := range r.visitsByWho[personID] {
		if v.IsOpen == 1 && v.SiteID == origin {
			return true // rule 3: back at the origin, provably post-trip
		}
		if v.TripID == nil {
			continue // closed and unorderable — not proof
		}
		vt := r.trips[*v.TripID]
		if vt != nil && vt.OriginSite == origin &&
			(vt.NodeEpoch > nodeEpoch ||
				(vt.NodeEpoch == nodeEpoch && vt.Seq > seq)) {
			return true // rule 2: later same-stream check-in
		}
	}
	return false
}
