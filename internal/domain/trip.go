// trip.go — the trip event family and THE one cross-site fold (design §9.1,
// chunk-plan C6): an immutable A-authored trip, a B-authored arrival, an
// A-authored cancel, and trip state as a deterministic fold over whichever of
// those events are present. Both facts always survive replication; every site
// computes the same fold.
//
// Fold architecture: raw tables (trip / trip_occupant / trip_arrival /
// trip_cancel) are written per event, and the DERIVED state (trip_state /
// arrival_state) is recomputed WHOLESALE from the raw tables inside the same
// apply transaction on every trip-family event. A pure function of table
// contents is order-independent across streams by construction — the store's
// fold contract (store/projection.go) — and the permuted-delivery tests in
// trip_test.go then measure that property instead of assuming it. At this
// system's volumes (design §4: ~194 check-ins/day) the recompute is free.
//
// Two safety rules this file exists to uphold (chunk-plan C6 not-done-ifs):
//   - NEVER AUTO-PICK: an unsolicited arrival that matches more than one open
//     trip lands in the ambiguous review lane and binds nothing (§9.2 — "a
//     wrong match would cancel a genuinely overdue vehicle's alert").
//   - NORMALISE FOR COMPARISON ONLY: the vehicle-id compare normalises (trim,
//     collapse internal whitespace, casefold) but the entered text is stored
//     verbatim and displayed verbatim. There is deliberately NO normalised
//     column anywhere in the schema — the normalised form exists only inside
//     the in-memory recompute, so it has no place to leak into storage.
//   - PARKED STAYS PARKED (⟦C-1 / T-092⟧, 2026-09-29): an arrival in the
//     ambiguous lane is never bound BY ELIMINATION when an officer closes
//     one of its candidates with an explicit arrival — see parkedAgainst.
//     Only a human action can resolve it.
//
// Folds here never call Author (C2 review row 9: SetMaxOpenConns(1) + a
// separate transaction would deadlock forever); §8.4 counter-events are
// human-triggered. Everything below works on the *sql.Tx it is handed.
package domain

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"

	"sitecontrol/internal/store"
)

// Trip fold states (design §9.1). arrived_after_cancel is a REVIEW state,
// surfaced to a human, never auto-resolved — for a muster that is the only
// defensible policy.
const (
	TripStateOpen               = "open"
	TripStateArrived            = "arrived"
	TripStateCancelled          = "cancelled"
	TripStateArrivedAfterCancel = "arrived_after_cancel"
)

// Arrival dispositions (derived, arrival_state).
const (
	ArrivalExpected   = "expected"   // carried an explicit trip reference
	ArrivalCorrelated = "correlated" // unsolicited, bound to the oldest open trip
	ArrivalPlain      = "plain"      // unsolicited, no open trip matched — stays a plain visit (§9.2)
	ArrivalAmbiguous  = "ambiguous"  // unsolicited, >1 open trip matched — human review lane, NEVER auto-picked
	// ArrivalCancelledMatch: unsolicited, no OPEN trip matched but one or
	// more CANCELLED trips did — the §9.1 contradiction (both a cancel and
	// an arrival exist) on the path where it actually happens: an
	// unsolicited author can never have heard the cancel, since hearing the
	// trip at all would have made the arrival an expected one. Human review
	// lane; binds NOTHING (the trip stays cancelled — review lives on the
	// arrival). C6 review, Ruling 2.
	ArrivalCancelledMatch = "cancelled_match"
	// ⟦W9b, item 16⟧ ArrivalRefRetracted: an EXPLICIT arrival whose TripRef
	// selection a human withdrew (F4 §3.6, `arrival_ref_retracted` — see
	// arrival_unref.go). Pass 1 SKIPS it, so it binds NOTHING and the trip
	// it named reverts to whatever the surviving raw facts say; it keeps
	// its `trip_id` — the REPUDIATED trip — because the review-lane row has
	// to name what was withdrawn. Review lane, never auto-resolved: the
	// people ARE here (the visits stand, I-1); it is the selection that was
	// wrong.
	ArrivalRefRetracted = "ref_retracted"
	// ArrivalResolved — James, 2026-09-30: officer resolution of parked
	// arrivals (Option 1). An UNSOLICITED arrival that was parked as
	// ambiguous and that an officer then resolved to one of its candidate
	// trips on the review lane (`arrival_resolved`, arrival_resolve.go). A
	// human selection, so it binds in Pass 1 beside the explicit references
	// and keeps its `trip_id`. "None of these" resolves to ArrivalPlain.
	ArrivalResolved = "resolved"
)

// NormalizeVehicleID is the COMPARISON form of an entered vehicle id
// (chunk-plan C6 M8): Unicode-normalise (NFC), trim, collapse internal
// whitespace, casefold. It is used only inside the correlation recompute and
// in authoring-time UI convenience — never written to the database (the
// entered text is stored and displayed VERBATIM, see the file comment; there
// is deliberately no normalised column anywhere in the schema). Casefolding
// stays strings.ToLower — vehicle ids at these sites are ASCII
// plates/callsigns ("TRUCK 12"); full Unicode casefolding is still not
// pulled in.
//
// F12: the norm.NFC step was added after a reproduced correlation break —
// "TRÜCK-1" dispatched with its entry stored in NFC, then an unsolicited
// arrival for the visually-identical string entered in NFD, failed to
// correlate (landed `plain`, the trip stayed open forever) because Go string
// equality is byte-for-byte and NFC/NFD encode the same glyph as different
// byte sequences. This file previously carried an ASCII-only assumption in
// this comment; nothing at the HTTP layer enforced it, and it was false.
func NormalizeVehicleID(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(norm.NFC.String(s)), " "))
}

// --- event payloads ---

// TripDispatched is the immutable, origin-site-authored trip (design §7, §9.1).
// The authoring site IS the origin site (env.SiteID) — the payload does not
// carry an origin field, so a trip structurally cannot claim a foreign origin.
// DestinationSite and DeadlineMinutes are BOTH nil on the degenerate local
// check-in trip (§10) — the ordinary gate check-in is this same event with
// nothing extra; C8's check-in form authors it and C7's deadline scanner and
// replay gate skip deadline-less trips entirely.
type TripDispatched struct {
	Type   string `json:"type"` // "trip_dispatched"
	TripID string `json:"trip_id"`
	// ⟦W7, item 9⟧ VehicleID names the vehicle RECORD this run went out on.
	// It is the FIRST correlation key and it is origin-scoped like the second
	// (DR-W7-19): an arrival claiming one origin must not bind to the run from
	// another, which is a wrong-crew binding rather than a statistics
	// annoyance.
	VehicleID string `json:"vehicle_id"`
	// VehicleUnidentified (SchemaVersion 13, R-17 — James: "there are no
	// doors only vehicles"): the explicit, decided fact that no vehicle
	// could be named for this movement at all. `omitempty` because false is
	// every pre-13 trip's own truth, not a gap — an old fixture that never
	// heard of this field decodes to exactly the fact it always had.
	// dispatchTrip refuses the combination of a named vehicle AND this flag
	// (see its own comment); VehicleID is always "" exactly when this is
	// true, and the store's CHECK constraint on `trip.vehicle_unidentified`
	// makes that pairing structural, not merely a convention this struct's
	// callers are trusted to keep.
	VehicleUnidentified bool `json:"vehicle_unidentified,omitempty"`
	// ⟦W7, item 8⟧ VehicleEntered is the frozen SNAPSHOT of the number as it
	// stood at dispatch — the role visit.person_snapshot_name plays for people
	// in item 5 — and NOT the identity of the truck, which VehicleID now
	// carries. Its comment could not stay as it was: `_entered` stops being
	// literally true once the operator picks rather than types.
	//
	// ⚠ ABSENCE IS SPELLED AS OMISSION, NEVER AS null (§7 row 2b): `omitempty`
	// on a *string omits the key, so a numberless run carries no vehicle key
	// into its payload bytes and therefore none into its row hash. A fixture
	// that seeds this field as a raw wire string literal compiles identically
	// before and after this change and CHANGES MEANING AT RUN TIME, which is
	// why the wire-literal set was measured rather than derived from the
	// compiler.
	VehicleEntered    *string  `json:"vehicle_entered,omitempty"`
	DriverPersonID    string   `json:"driver_person_id"`
	OccupantPersonIDs []string `json:"occupant_person_ids"` // the driver is always an occupant row (§7)
	DestinationSite   *string  `json:"destination_site,omitempty"`
	// DestinationUncontrolled (SchemaVersion 7) records a run to a configured
	// pad this company does NOT control — no gate, no officer, nobody who can
	// record an arrival there. The destination and occupants are recorded and
	// the occupants checked out of the origin (who is out there must be
	// answerable), but there is NO deadline, NO timer and NO alert: a promise
	// nobody can close would only ever produce a false alarm. Mutually
	// exclusive with DestinationSite; Dispatch refuses a deadline alongside
	// it. Because DestinationSite stays nil on these trips, every structural
	// exclusion keyed on destination_site IS NULL — no board row, no scan, no
	// correlation candidacy (§10 degenerate reasoning) — covers them without
	// any change to the fold arithmetic.
	DestinationUncontrolled *string `json:"destination_uncontrolled,omitempty"`
	DeadlineMinutes         *int64  `json:"deadline_minutes,omitempty"`
	// ⟦was: PhaseAtDispatch / PhaseOverrideReason (`phase_at_dispatch`,
	// `phase_override_reason`, SchemaVersion 8) — removed with weather phases,
	// James 2026-10-04, inside the same unreleased v16. A legacy payload that
	// still carries either key decodes cleanly: the keys are simply ignored.⟧
	// CargoNote (item 3, W5e; SchemaVersion stayed 10 — this field rode W1's
	// 9 → 10 bump, W5-SCHEMAVERSION-ADJUDICATION.md; 11 since the D-92 increment) is what the officer chose
	// to write about the load, carried VERBATIM — the same posture as
	// VehicleEntered: stored exactly as typed and TO BE DISPLAYED the same way
	// once a surface renders it (C-7/c10, B3 — nothing renders it today), never
	// trimmed, case-folded, normalised or truncated. It is a RECORD, not a
	// rule and not a field anything computes with: nil means the officer wrote
	// nothing, and that is the whole of its meaning — there is no "hauling"
	// flag, no taxonomy, and no code path may branch on what the note says.
	// D-21.3 settled the arrival side in terms ("Departures record cargo;
	// unsolicited arrivals do not. Closed."), so no arrival payload carries
	// a counterpart.
	CargoNote *string `json:"cargo_note,omitempty"`
	// RoadWork (James, 2026-10-03: "another category for equipment working
	// on the ice road so that they are not listed as overdue when they will
	// spend up to 12 hours on the road working") marks this dispatch as
	// equipment sent out WORKING ON THE ROAD — a trip KIND, not a destination.
	// DestinationSite and DestinationUncontrolled are BOTH nil on it, like
	// the §10 degenerate trip, and unlike that trip it CARRIES A DEADLINE:
	// the origin post's configured working limit (config [road_work]), so
	// IncompleteCheckIns (which selects deadline-less destination-less open
	// trips) never mistakes it for a crashed check-in. `omitempty`: every
	// pre-existing payload marshals byte-identically, so no stored hash
	// moves. The fold writes the fact to the trip_road_work side table
	// (road_work.go) — a NEW table, IF NOT EXISTS, no SchemaVersion bump —
	// and every surface reads it from there, never from a destination
	// sentinel.
	RoadWork   bool    `json:"road_work,omitempty"`
	RecordedBy *string `json:"recorded_by,omitempty"`
}

// ArrivalVisit is one occupant's visit as minted AT AUTHORING TIME and
// carried inside the arrival payload. Both the visit_id and the
// point-in-time company/trade snapshot (design §8.8-i) are fixed when
// the arrival is recorded, NOT looked up at fold time: a fold minting
// random ids or reading roster state mid-fold would diverge across sites
// (reference events may be delivered in a different order there). The
// snapshot is the reference ACTUALLY USED — the roster row's own
// company/trade id plus its then-current name, with NO alias
// resolution (C5 review row 23: two partitioned sites can mint
// identical-looking trades; the snapshot must record which one was
// used, and an alias merge later must never rewrite it).
// ⟦W6, item 5⟧ PersonName is the visit name snapshot on the wire. The tag is
// `person_name`, NOT the column's `person_snapshot_name`: it is the sibling of
// CompanyName/TradeName, whose tags are already `company_name`/
// `trade_name` while their columns are `company_snapshot_name`/
// `trade_snapshot_name`. The wire names WHAT THE FIELD IS; the column
// names WHAT THE ROW IS. Carrying both spellings on both sides would freeze a
// rejected spelling beside an accepted one — the frozen-ghost shape [[D-47]]
// forbids by name — so `person_snapshot_name` must never appear as a payload
// key.
type ArrivalVisit struct {
	VisitID     string  `json:"visit_id"`
	PersonID    string  `json:"person_id"`
	CompanyID   *string `json:"company_id,omitempty"`
	CompanyName *string `json:"company_name,omitempty"`
	TradeID     *string `json:"trade_id,omitempty"`
	TradeName   *string `json:"trade_name,omitempty"`
	PersonName  *string `json:"person_name,omitempty"`
	// PredatesOrientationRule (SchemaVersion 12, R-8-REVISED/R-16): CAPACITY
	// ONLY for the deferred import — see visit's own column comment
	// (store.go). *int64 rather than bool: this struct's other nullable
	// facts follow the same pointer idiom, and the value that survives a
	// round trip through the store's generic dump-to-JSON path (an SQLite
	// INTEGER scans as a Go int64/nil, never a bool) must match the
	// projection row's own type (Visit.PredatesOrientationRule, below) or
	// decoding one from the other's JSON breaks. 1 means TRUE; nil means
	// ABSENT; never 0 ([[D-10]]'s value domain). No live authoring edge
	// sets this — buildArrivalVisits leaves it nil for every occupant it
	// mints, always.
	PredatesOrientationRule *int64 `json:"predates_orientation_rule,omitempty"`
}

// TripArrival is the B-authored arrival (§9.1). Two forms:
//   - expected: TripRef set — the gate checked in a trip B already knows.
//   - unsolicited: TripRef nil, VehicleEntered + OriginSite set — the vehicle
//     outran the sync or the link is down (§9.2); the fold correlates it (or
//     refuses to) on catch-up.
type TripArrival struct {
	Type      string  `json:"type"` // "trip_arrival"
	ArrivalID string  `json:"arrival_id"`
	TripRef   *string `json:"trip_ref,omitempty"`
	// ⟦W7, item 9⟧ VehicleID is the id-first correlation key's B-side half.
	// Nil when this post could not identify the truck as a record it holds —
	// the partitioned case §9.2 exists for. The SAME SPELLING as the trip
	// side, deliberately: this is one fact about one truck seen from two ends,
	// unlike W6's person_name / person_snapshot_name split, where the two
	// spellings name two different facts.
	VehicleID      *string `json:"vehicle_id,omitempty"`
	VehicleEntered *string `json:"vehicle_entered,omitempty"` // the number as recorded at B's gate
	OriginSite     *string `json:"origin_site,omitempty"`     // claimed origin, the §9.2 correlation key half
	// VehicleUnidentified (SchemaVersion 14, R-18 — this door's own share of
	// R-17's "there are no doors only vehicles"): an explicit, decided fact
	// that this post could not name a vehicle for the truck that arrived at
	// all — never blended with VehicleID: RecordUnsolicitedArrivalOriented
	// refuses the combination before ever marshalling this payload, the
	// same choke-point discipline dispatchTrip enforces for TripDispatched.
	// omitempty: every arrival this build authors that is NOT this new
	// case — every expected arrival (TripRef set, VehicleID always nil
	// already) and every ordinary unsolicited arrival naming a real vehicle
	// — marshals byte-identically to before this bump.
	VehicleUnidentified bool           `json:"vehicle_unidentified,omitempty"`
	Visits              []ArrivalVisit `json:"visits"`
	// NotEntered (D-98/D-91, SCHEMA-11 step 5): person ids only — aboard at
	// arrival, but NOT entered because they do not (yet) hold this site's
	// orientation. `omitempty`: at an orientation=false post, and for any
	// all-oriented arrival, the payload is byte-identical to today. Rendered
	// on ONE screen only — /orientation's held-out list (heldout.go; R-11,
	// 2026-09-09), never a muster or printed sheet (D-10) ⟦was: "Never
	// rendered anywhere (D-98 §0)"⟧ — the officer's observation that these
	// people got off the bus, nothing more. Disjoint from Visits by
	// construction (buildArrivalVisits partitions personIDs into the two).
	NotEntered []string `json:"not_entered,omitempty"`
	RecordedBy *string  `json:"recorded_by,omitempty"`
}

// TripCancelled is the A-authored cancel (§9.1). It never alters visit state:
// the occupants were checked out at dispatch and STAY checked out — a cancel
// can also mean "went elsewhere"; auto-reinstate is explicitly wrong.
type TripCancelled struct {
	Type       string  `json:"type"` // "trip_cancelled"
	TripID     string  `json:"trip_id"`
	RecordedBy *string `json:"recorded_by,omitempty"`
}

// --- projection row shapes (mirror the schemas; used by dumpSection) ---

type Trip struct {
	TripID     string `json:"trip_id"`
	OriginSite string `json:"origin_site"`
	// NodeEpoch/Seq are the trip event's SOURCE KEY, carried so "oldest open
	// trip" can be ordered by (node_epoch, seq) — never rowid, never
	// wall-time (C2 review row 10; §8.3). An anticipated schema extension,
	// not a workaround.
	NodeEpoch int64 `json:"node_epoch"`
	Seq       int64 `json:"seq"`
	// ⟦W7⟧ VehicleID / VehicleEntered: see TripDispatched. The id is the
	// record; the entered string is the frozen snapshot of its number at
	// dispatch, nil when the vehicle had none. ⚠ Every surface that answers
	// "which truck is out RIGHT NOW" composes the vehicle LIVE and does not
	// read the snapshot (DR-W7-18); the snapshot is for surfaces answering
	// "what was recorded THEN".
	VehicleID string `json:"vehicle_id"`
	// VehicleUnidentified (SchemaVersion 13, R-17): see TripDispatched.
	//
	// ⚠ int64, NOT bool, on THIS (the projection-read) struct specifically.
	// dumpSection/oneRow (dump.go) decode a row via store.dumpQuery's
	// generic map[string]any → json.Marshal → json.Unmarshal round trip,
	// which reads a SQLite INTEGER column back as a JSON NUMBER — 0 or 1 —
	// never as a JSON bool, so `bool` here fails to decode every row (json:
	// cannot unmarshal number into Go value of type bool — MEASURED).
	// TripDispatched's own field stays `bool`: that struct decodes straight
	// from authored payload bytes Go itself marshaled, never through this
	// pipeline. Mirrors ArrivalVisit.PredatesOrientationRule's *int64 fix
	// for the identical reason, one door up.
	VehicleUnidentified int64   `json:"vehicle_unidentified"`
	VehicleEntered      *string `json:"vehicle_entered"`
	DriverPersonID      string  `json:"driver_person_id"`
	DestinationSite     *string `json:"destination_site"`
	// DestinationUncontrolled: see TripDispatched. Never set together with
	// DestinationSite; nil on both ordinary inter-site trips and degenerate
	// local check-ins.
	DestinationUncontrolled *string `json:"destination_uncontrolled"`
	DeadlineMinutes         *int64  `json:"deadline_minutes"`
	// CargoNote: see TripDispatched. Carried through to the row so a human can
	// read it ONCE A SURFACE RENDERS IT (C-7/c10, B3); no view renders it
	// today. Nothing derives anything from it.
	CargoNote  *string `json:"cargo_note"`
	RecordedBy *string `json:"recorded_by"`
}

// VehicleIsUnidentified is the readable form of VehicleUnidentified's 0/1
// storage (SchemaVersion 13, R-17) — every consumer outside this file reads
// the fact through here rather than re-deriving the != 0 comparison.
func (t Trip) VehicleIsUnidentified() bool { return t.VehicleUnidentified != 0 }

// VehicleUnidentifiedText is the ONE display string wherever a trip's
// vehicle was explicitly recorded as not identified — never a blank, and
// never confused with a role-only vehicle record's absent number
// (vehicleSnapshotText's own "" — DR-W7-15, a different fact). Matches the
// existing operator-facing checkbox wording (gate.html's "Vehicle unknown")
// so nothing on screen introduces a second phrase for one fact. Exported so
// domain-level display strings (tripVehicleEnteredDisplay below) and the web
// layer's own render helper (vehicle_render.go) share one spelling.
const VehicleUnidentifiedText = "Vehicle unknown"

// tripVehicleEnteredDisplay is the "what was recorded THEN" reading of a
// trip's frozen vehicle_entered snapshot, honest about VehicleUnidentified —
// the ONE place OutAtUncontrolled and NotTracked (tripauthor.go) turn a
// composed Trip into away-view display text, so the two categories cannot
// drift on this from one another. Mirrors the web layer's own
// tripVehicleDisplay (vehicle_render.go); the two are not shared code only
// because they live in different packages reading different struct shapes.
func tripVehicleEnteredDisplay(t Trip) string {
	if t.VehicleIsUnidentified() {
		return VehicleUnidentifiedText
	}
	return derefString(t.VehicleEntered)
}

type TripOccupant struct {
	TripID   string `json:"trip_id"`
	PersonID string `json:"person_id"`
}

// ArrivalNotEntered is one row of trip_arrival_not_entered (D-98/D-91): an
// occupant this arrival's payload named `not_entered` — aboard, but not
// entered because they do not hold this site's orientation. Written
// verbatim by applyTripArrival, alongside the visit rows for the same
// arrival; read by HeldOutAt (heldout.go) for /orientation's held-out list
// and by nothing else — never a muster or printed sheet, D-10's own ground
// ⟦was: "nothing renders it (never a muster or printed sheet — D-98 §0, same
// ground as `orientation` itself)" — D-98's absolute superseded in part by
// R-11, 2026-09-09⟧.
type ArrivalNotEntered struct {
	ArrivalID string `json:"arrival_id"`
	PersonID  string `json:"person_id"`
}

type Arrival struct {
	ArrivalID      string  `json:"arrival_id"`
	SiteID         string  `json:"site_id"`
	NodeEpoch      int64   `json:"node_epoch"`
	Seq            int64   `json:"seq"`
	TripRef        *string `json:"trip_ref"`
	VehicleID      *string `json:"vehicle_id"` // ⟦W7, item 9⟧ see TripArrival
	VehicleEntered *string `json:"vehicle_entered"`
	OriginSite     *string `json:"origin_site"`
	// VehicleUnidentified (SchemaVersion 14, R-18): see TripArrival's own
	// comment for the fact. int64, NOT bool, on THIS projection-read struct
	// specifically — the identical reason Trip.VehicleUnidentified is
	// int64 (trip.go, R-17): dumpSection/oneRow decode a row through
	// store.dumpQuery's generic map[string]any → json.Marshal →
	// json.Unmarshal round trip, which reads a SQLite INTEGER column back
	// as a JSON NUMBER, never a JSON bool. TripArrival's own field stays
	// `bool`: that struct decodes straight from authored payload bytes Go
	// itself marshaled, never through this pipeline.
	VehicleUnidentified int64   `json:"vehicle_unidentified"`
	RecordedBy          *string `json:"recorded_by"`
	// NotEntered ⟦gate-orientation-block, 2026-09-09⟧ is what the AUTHOR
	// partitioned out (buildArrivalVisits): the occupants aboard this arrival
	// who were NOT entered for lack of this site's orientation, person ids in
	// occupant order. Returned to the caller only — `json:"-"` because this
	// struct is also trip_arrival's projection row (Arrivals reads it through
	// dumpSection), and the projection keeps the rows in their own table,
	// trip_arrival_not_entered (ArrivalNotEntered above). The gate doors read
	// this to tell the officer who was held out (web/notentered.go).
	NotEntered []string `json:"-"`
}

// VehicleIsUnidentified is the readable form of Arrival.VehicleUnidentified's
// 0/1 int64 — Trip.VehicleIsUnidentified's own counterpart, R-18.
func (a Arrival) VehicleIsUnidentified() bool { return a.VehicleUnidentified != 0 }

type TripCancel struct {
	TripID     string  `json:"trip_id"`
	RecordedBy *string `json:"recorded_by"`
}

type TripState struct {
	TripID    string  `json:"trip_id"`
	State     string  `json:"state"`
	ArrivalID *string `json:"arrival_id"`
}

type ArrivalState struct {
	ArrivalID   string  `json:"arrival_id"`
	Disposition string  `json:"disposition"`
	TripID      *string `json:"trip_id"`
	// Candidates lists the matched open trips of an AMBIGUOUS arrival, in
	// (node_epoch, seq) order, comma-joined — the review lane shows a human
	// exactly which trips the fold refused to choose between.
	Candidates *string `json:"candidates"`
}

type Visit struct {
	VisitID             string  `json:"visit_id"`
	SiteID              string  `json:"site_id"`
	PersonID            string  `json:"person_id"`
	TripID              *string `json:"trip_id"`
	CompanySnapshotID   *string `json:"company_snapshot_id"`
	CompanySnapshotName *string `json:"company_snapshot_name"`
	TradeSnapshotID     *string `json:"trade_snapshot_id"`
	TradeSnapshotName   *string `json:"trade_snapshot_name"`
	// ⟦W6, item 5⟧ the display name as recorded at check-in. The projection
	// struct's tags ARE its column names, so this one is the COLUMN spelling
	// and differs from ArrivalVisit's payload spelling on purpose.
	PersonSnapshotName *string `json:"person_snapshot_name"`
	// NodeEpoch/Seq (SchemaVersion 15, R-20): the MINTING ENVELOPE's key —
	// store.go's column comment. int64 for the same reason Arrival's key
	// fields are: this struct decodes through store.dumpQuery's generic
	// map → JSON round trip. They are what makes IsOpen a DERIVED value
	// (visit_open.go) and what RestoredFromPeer reads (R-23).
	NodeEpoch int64 `json:"node_epoch"`
	Seq       int64 `json:"seq"`
	// IsOpen is DERIVED (SchemaVersion 15): re-computed per (site, person)
	// in the visit projection's Finalize from the raw tables, never folded
	// in place. Read it; never write it from a fold.
	IsOpen     int64   `json:"is_open"`
	RecordedBy *string `json:"recorded_by"`
	// PredatesOrientationRule: see store.go's column comment and
	// ArrivalVisit's own field comment (trip.go) — capacity for the
	// deferred import, unwritten by any live authoring edge today.
	PredatesOrientationRule *int64 `json:"predates_orientation_rule"`
}

// RestoredFromPeer (SchemaVersion 15, R-23 — James, 2026-09-13: "Restore
// them, but mark them as restored") answers, for the post running as
// (siteID, nodeEpoch): is this one of OUR visits that THIS incarnation of
// the post never observed? A row under our own site tag whose node_epoch
// is BELOW the running config epoch was authored by an earlier incarnation
// and reached this database through exactly one door — repl/engine.go's
// §8.4 self-history exception, i.e. the peer's copy of our history (a peer
// cannot deliver our-site rows at or above our current epoch; that bound
// is tested there). The muster and the gate roster mark such rows RESTORED
// rather than presenting second-hand presence as first-hand: the peer's
// copy IS our only backup, so the data is kept — what changes is what the
// sheet claims to know. Rows of OTHER sites are never "restored" in this
// sense (every copy of another site's presence is second-hand by nature),
// so the classification is scoped to our own tag.
//
// MEASURED hazard this answers (reports/witness/CLEAN-NEGATIVES-RECHECK.md,
// plant C2b): a peer serving forged self-history landed an OPEN VISIT FOR
// A STRANGER under our tag. Only the epoch was bounded; the content was
// not, and cannot be — the wipe destroyed the evidence a post would need
// to tell genuine recovered history from forged. So the data is not
// rejected; it is stopped from masquerading as locally observed.
//
// The one case this over-marks: an operator who bumps node_epoch in config
// WITHOUT wiping the database (§8.6: a re-image announces epoch+1; a bump
// without a wipe is a config error). Its genuinely observed epoch-1 rows
// then read RESTORED — the claims-less-than-it-knows direction, never the
// reverse.
func (v Visit) RestoredFromPeer(siteID string, nodeEpoch int64) bool {
	return v.SiteID == siteID && v.NodeEpoch < nodeEpoch
}

// --- projections ---

// tripProjection folds trip_dispatched: the immutable trip row, its occupant
// rows, and the dispatch-time check-out of every occupant from the origin
// site's on-site list (design §10 — {trip} alone does this, with no arrival
// or cancel present; chunk-plan Cm-6 rider). trip/visit tables themselves are
// created by store's first schema (nullable recorded_by is unbackfillable,
// §10), so Schema here is empty.
func tripProjection() store.Projection {
	return store.Projection{
		Name:    "trip",
		Table:   "trip",
		OrderBy: "trip_id",
		// F28: the key recompute reads "all NON-DEGENERATE trips of one
		// origin" — the candidate universe for a correlation key. The
		// partial index bounds that read by the inter-site trip count
		// instead of the season's check-in count (the table itself is
		// store-schema-owned; an index is not dumped/replicated shape, so
		// it rides Schema here like trip_arrival's, no SchemaVersion bump).
		Schema: `CREATE INDEX IF NOT EXISTS idx_trip_intersite
		         ON trip (origin_site, node_epoch, seq) WHERE destination_site IS NOT NULL;`,
		Apply: applyTripDispatched,
	}
}

func applyTripDispatched(tx *sql.Tx, env store.Envelope) error {
	var p TripDispatched
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return fmt.Errorf("domain: trip fold: %w", err)
	}
	if p.Type != "trip_dispatched" {
		return nil
	}
	// Origin is the authoring site, structurally: env.SiteID, never a payload
	// field. vehicle_entered is stored VERBATIM — no trimming, no case change.
	if _, err := tx.Exec(
		`INSERT INTO trip (trip_id, origin_site, node_epoch, seq, vehicle_id, vehicle_unidentified,
		                   vehicle_entered, driver_person_id, destination_site,
		                   destination_uncontrolled, deadline_minutes, cargo_note, recorded_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.TripID, env.SiteID, env.NodeEpoch, env.Seq, p.VehicleID, p.VehicleUnidentified, p.VehicleEntered,
		p.DriverPersonID, p.DestinationSite, p.DestinationUncontrolled,
		p.DeadlineMinutes, p.CargoNote, p.RecordedBy,
	); err != nil {
		return fmt.Errorf("domain: trip fold: insert %s: %w", p.TripID, err)
	}
	// James, 2026-10-03: the road-work KIND lands in its own raw side table
	// (road_work.go) — INSERT OR IGNORE on the trip id, written once by this
	// fold and never revisited, the same raw-input discipline as
	// trip_occupant. A set keyed by trip id is order-independent across
	// streams by construction, and a trip row is immutable, so the fact is
	// identical on every post and on every replay.
	if p.RoadWork {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO trip_road_work (trip_id) VALUES (?)`, p.TripID); err != nil {
			return fmt.Errorf("domain: trip fold: road work %s: %w", p.TripID, err)
		}
	}
	// The driver is ALWAYS an occupant row (§7: a label-only driver would
	// vanish from every muster) — enforced here as well as at authoring, so
	// an injected/foreign trip event cannot produce a driverless occupant set.
	occupants := append([]string{p.DriverPersonID}, p.OccupantPersonIDs...)
	for _, personID := range occupants {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO trip_occupant (trip_id, person_id) VALUES (?, ?)`,
			p.TripID, personID,
		); err != nil {
			return fmt.Errorf("domain: trip fold: occupant %s: %w", personID, err)
		}
		// Dispatch checks each occupant out of the ORIGIN site's visit set
		// (§10) — but NOT here, and not by an in-place UPDATE any more.
		// ⟦SchemaVersion 15, R-20⟧ This fold used to run
		// `UPDATE visit SET is_open = 0 WHERE site_id = ? AND person_id = ?
		// AND is_open = 1` per occupant, "deterministic across sites because
		// every visit row at a site is only ever mutated by that same
		// site's own stream, which is contiguous (C2)" — carry-forward ROW
		// 28, now FALSIFIED for the second time: that sentence is true of a
		// STREAM and false of a SITE, and a re-imaged site owns two streams.
		// Delivered after the site's own later epoch (the §8.6 self-history
		// retry), this predicate closed a visit minted AFTER it. The check-
		// out is now a comparison of keys in visit_open.go's derivation:
		// the trip row above (origin_site, node_epoch, seq) and the
		// occupant rows are the raw facts, and "a dispatch with a GREATER
		// key than the visit closes it" holds in any delivery order. The
		// visit projection's Finalize re-derives every occupant named here
		// before this batch commits.
	}
	// Derived-state recompute is the "trip_state" projection's job — it runs
	// after this fold for the same event (name ordering), so calling it here
	// too would just run it twice per trip event (C6 review, LOW).
	return nil
}

func tripOccupantProjection() store.Projection {
	return store.Projection{
		Name:    "trip_occupant",
		Table:   "trip_occupant",
		OrderBy: "trip_id, person_id",
		Schema: `CREATE TABLE IF NOT EXISTS trip_occupant (
			trip_id   TEXT NOT NULL,
			person_id TEXT NOT NULL,
			PRIMARY KEY (trip_id, person_id)
		) STRICT;
		-- SchemaVersion 15 (R-20): visit_open.go's derivation asks "the
		-- latest dispatch of THIS person from THIS site" per touched pair;
		-- the PK leads with trip_id, so without this the read is a full
		-- scan of the season's occupant rows per re-derived person.
		-- Indexes are not dumped/replicated shape (F30 precedent).
		CREATE INDEX IF NOT EXISTS idx_trip_occupant_person ON trip_occupant (person_id, trip_id);`,
		// Written by the trip fold; dump-only here.
	}
}

// tripArrivalProjection folds trip_arrival: the raw arrival row plus one
// visit per payload-carried occupant, each with its authoring-time
// point-in-time snapshot (§8.8-i). Visit creation is INDEPENDENT of
// correlation: whether the arrival turns out expected, correlated, plain or
// ambiguous, the people are on this site and belong on its muster.
func tripArrivalProjection() store.Projection {
	return store.Projection{
		Name:    "trip_arrival",
		Table:   "trip_arrival",
		OrderBy: "arrival_id",
		Schema: `CREATE TABLE IF NOT EXISTS trip_arrival (
			arrival_id      TEXT PRIMARY KEY,
			site_id         TEXT    NOT NULL,
			node_epoch      INTEGER NOT NULL,
			seq             INTEGER NOT NULL,
			trip_ref        TEXT,
			-- ⟦W7, item 9⟧ vehicle_id: which vehicle RECORD the arriving
			-- truck was identified as, when this post could identify it.
			-- NULLABLE, and the nullability is the ruled shape (DR-W7-2):
			-- an arrival whose vehicle is not yet known HERE carries no id,
			-- which is exactly the partitioned case §9.2 exists for — the
			-- vehicle outran the sync, or the link is down. The trip side is
			-- NOT NULL and this side is not; the asymmetry is the criterion.
			--
			-- ⚠ It is the id-first correlation key's other half. Id-first
			-- correlation is IMPOSSIBLE without an id on BOTH halves of the
			-- comparison, which is why this column exists at all.
			vehicle_id      TEXT,
			-- vehicle_unidentified (SchemaVersion 14, R-18 — this door's own
			-- share of R-17, "there are no doors only vehicles"): the
			-- explicit, decided fact that THIS site could not name a
			-- vehicle for the arriving truck at all — distinct from
			-- vehicle_id IS NULL on an EXPECTED arrival (trip_ref set),
			-- which means "not carried, because the trip already names the
			-- vehicle" and is the ordinary, majority shape of this column
			-- today. Decided once, at RecordUnsolicitedArrivalOriented
			-- (tripauthor.go), the one production caller of this door.
			--
			-- ⚠ ONE-DIRECTIONAL, on trip.vehicle_unidentified's own
			-- precedent and for the SAME measured reason: a symmetric CHECK
			-- (also forbidding vehicle_unidentified=0 with vehicle_id IS
			-- NULL) would refuse every EXPECTED arrival ever folded — that
			-- is the overwhelmingly common shape of this table, not an edge
			-- case, and refusing to fold an already-durably-logged event is
			-- not available as an option (F4 §3.2, same citation trip's own
			-- CHECK comment gives). What must be impossible, and is, is the
			-- dangerous direction: a real, populated vehicle_id on this row
			-- simultaneously claimed unidentified.
			vehicle_unidentified INTEGER NOT NULL DEFAULT 0
				CHECK (NOT (vehicle_unidentified = 1 AND vehicle_id IS NOT NULL)),
			vehicle_entered TEXT,
			origin_site     TEXT,
			recorded_by     TEXT
		) STRICT;
		-- F28: the scoped recompute's two arrival reads. idx_arrival_trip_ref
		-- serves the majority path (every degenerate check-in's point
		-- recompute asks "which explicit arrivals reference this trip") —
		-- without it that read is a full scan per event and the replay goes
		-- quadratic through this table instead. The partial index serves the
		-- rare key recompute (its query spells the same literal IS NULL, so
		-- it is planner-eligible). Indexes are not dumped/replicated shape:
		-- no SchemaVersion bump (fix-perf precedent), IF NOT EXISTS applies
		-- them to existing databases on next open.
		CREATE INDEX IF NOT EXISTS idx_arrival_trip_ref ON trip_arrival (trip_ref);
		CREATE INDEX IF NOT EXISTS idx_arrival_unsolicited ON trip_arrival (origin_site) WHERE trip_ref IS NULL;`,
		Apply: applyTripArrival,
	}
}

// mintVisitFromArrivalVisit is the ONE place an ArrivalVisit becomes a
// `visit` row — extracted (SchemaVersion 12) so entry-on-completion's fold
// (orientation.go's applyOrientationCompleted) can mint one visit the
// identical way trip_arrival's own fold mints many, rather than a second,
// drifting copy of the same statement. visit.trip_id records only the
// authoring-time EXPLICIT reference — a derived correlation must not be
// baked into the raw row.
//
// ⟦SchemaVersion 15, R-20⟧ A BARE INSERT. It used to close the person's
// still-open visit first (`UPDATE visit SET is_open = 0 WHERE site_id = ?
// AND person_id = ? AND is_open = 1`, "same-stream determinism") — the
// same row-28 premise the dispatch fold leaned on, false across a site's
// own epochs. The row now carries its minting envelope's key
// (env.NodeEpoch, env.Seq — never a payload field), and "a later visit
// supersedes the earlier one" is a comparison of keys in visit_open.go's
// derivation, which runs in this projection family's Finalize before the
// batch commits. is_open is inserted as 1 and then DERIVED; the 1 is the
// safe resting value if a derivation were ever skipped (over-report, not
// a hidden person — store.go's column comment). The site is the
// envelope's, as it always was: the whole envelope is taken so a caller
// cannot pass a site and a key that disagree.
func mintVisitFromArrivalVisit(tx *sql.Tx, env store.Envelope, tripID *string, v ArrivalVisit, recordedBy *string) error {
	if _, err := tx.Exec(
		`INSERT INTO visit (visit_id, site_id, person_id, trip_id,
		                    company_snapshot_id, company_snapshot_name,
		                    trade_snapshot_id, trade_snapshot_name,
		                    person_snapshot_name, node_epoch, seq,
		                    is_open, recorded_by, predates_orientation_rule)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		v.VisitID, env.SiteID, v.PersonID, tripID,
		v.CompanyID, v.CompanyName, v.TradeID, v.TradeName,
		v.PersonName, env.NodeEpoch, env.Seq,
		recordedBy, v.PredatesOrientationRule,
	); err != nil {
		return fmt.Errorf("visit %s: %w", v.VisitID, err)
	}
	return nil
}

func applyTripArrival(tx *sql.Tx, env store.Envelope) error {
	var p TripArrival
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return fmt.Errorf("domain: arrival fold: %w", err)
	}
	if p.Type != "trip_arrival" {
		return nil
	}
	if _, err := tx.Exec(
		`INSERT INTO trip_arrival (arrival_id, site_id, node_epoch, seq, trip_ref,
		                           vehicle_id, vehicle_unidentified, vehicle_entered,
		                           origin_site, recorded_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ArrivalID, env.SiteID, env.NodeEpoch, env.Seq, p.TripRef,
		p.VehicleID, p.VehicleUnidentified, p.VehicleEntered, p.OriginSite, p.RecordedBy,
	); err != nil {
		return fmt.Errorf("domain: arrival fold: insert %s: %w", p.ArrivalID, err)
	}
	for _, v := range p.Visits {
		if err := mintVisitFromArrivalVisit(tx, env, p.TripRef, v, p.RecordedBy); err != nil {
			return fmt.Errorf("domain: arrival fold: %w", err)
		}
	}
	// D-98/D-91: the split's other half. No visit, no is_open touch, no
	// snapshot — just the raw fact "this occupant was aboard and did not
	// enter", INSERT-only, same discipline as trip_occupant (raw input,
	// never revisited by a later fold).
	for _, personID := range p.NotEntered {
		if _, err := tx.Exec(
			`INSERT INTO trip_arrival_not_entered (arrival_id, person_id) VALUES (?, ?)`,
			p.ArrivalID, personID,
		); err != nil {
			return fmt.Errorf("domain: arrival fold: not_entered %s: %w", personID, err)
		}
	}
	return nil
}

// tripArrivalNotEnteredProjection registers trip_arrival_not_entered so it
// self-creates on Open and is dumped/compared like every other raw table
// (§67 inversion: registered ⇒ compared by dumpcompare by default). Its
// rows are written by applyTripArrival above, not by an Apply of its own —
// the same "written by [another] fold; dump-only here" shape as
// tripOccupantProjection.
func tripArrivalNotEnteredProjection() store.Projection {
	return store.Projection{
		Name:    "trip_arrival_not_entered",
		Table:   "trip_arrival_not_entered",
		OrderBy: "arrival_id, person_id",
		Schema: `CREATE TABLE IF NOT EXISTS trip_arrival_not_entered (
			arrival_id TEXT NOT NULL,
			person_id  TEXT NOT NULL,
			PRIMARY KEY (arrival_id, person_id)
		) STRICT;`,
	}
}

func tripCancelProjection() store.Projection {
	return store.Projection{
		Name:    "trip_cancel",
		Table:   "trip_cancel",
		OrderBy: "trip_id",
		Schema: `CREATE TABLE IF NOT EXISTS trip_cancel (
			trip_id     TEXT PRIMARY KEY,
			recorded_by TEXT
		) STRICT;`,
		Apply: applyTripCancelled,
	}
}

func applyTripCancelled(tx *sql.Tx, env store.Envelope) error {
	var p TripCancelled
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return fmt.Errorf("domain: cancel fold: %w", err)
	}
	if p.Type != "trip_cancelled" {
		return nil
	}
	// OR IGNORE: a second cancel for the same trip is a no-op. Cancels for a
	// trip are only ever legitimately authored by its origin site (§9.1), so
	// they share one stream and "first in stream wins" is deterministic.
	// NOTE this fold touches trip_cancel ONLY — it never goes near the visit
	// table. "A cancel never alters visit state" (§9.1) is structural here,
	// not a conditional someone could get wrong.
	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO trip_cancel (trip_id, recorded_by) VALUES (?, ?)`,
		p.TripID, p.RecordedBy,
	); err != nil {
		return fmt.Errorf("domain: cancel fold: %s: %w", p.TripID, err)
	}
	return nil
}

// tripStateProjection recomputes the derived trip_state (and arrival_state)
// tables after every trip-family event. Registration-name ordering guarantees
// it runs AFTER the raw folds for the same event ("trip", "trip_arrival",
// "trip_cancel" all sort before "trip_state").
func tripStateProjection() store.Projection {
	return store.Projection{
		Name:    "trip_state",
		Table:   "trip_state",
		OrderBy: "trip_id",
		Schema: `CREATE TABLE IF NOT EXISTS trip_state (
			trip_id    TEXT PRIMARY KEY,
			state      TEXT NOT NULL,
			arrival_id TEXT
		) STRICT;`,
		// F28: the recompute is a batch Finalize, not a per-event Apply —
		// once per applied batch (a replication drain page, or one Author),
		// scoped to the correlation keys the batch touched. Still a pure
		// set function of the raw tables inside the apply transaction; see
		// tripStateFinalize for why the scoping is sound.
		Finalize: tripStateFinalize,
	}
}

func arrivalStateProjection() store.Projection {
	return store.Projection{
		Name:    "arrival_state",
		Table:   "arrival_state",
		OrderBy: "arrival_id",
		Schema: `CREATE TABLE IF NOT EXISTS arrival_state (
			arrival_id  TEXT PRIMARY KEY,
			disposition TEXT NOT NULL,
			trip_id     TEXT,
			candidates  TEXT
		) STRICT;`,
		// Rebuilt by trip_state's recompute; dump-only here.
	}
}

func visitProjection() store.Projection {
	return store.Projection{
		Name:    "visit",
		Table:   "visit",
		OrderBy: "visit_id",
		// Rows are written by the trip_arrival fold and by
		// orientation_completed's entry-on-completion, both through the ONE
		// mint above (INSERT-only). Table comes from store's first schema
		// (§10 recorded_by rationale).
		//
		// ⟦SchemaVersion 15, R-20⟧ is_open is DERIVED here, in Finalize
		// (visit_open.go) — once per applied batch, after every fold, from
		// the raw tables alone — so the open set is a pure function of table
		// contents under any delivery order, a site's own earlier epoch
		// after its later one included. Name order puts this Finalize after
		// "orientation" and "trip_state"; it reads neither.
		Finalize: visitOpenFinalize,
	}
}

// corrKey is the §9.2 STRING correlation key: the origin site plus the
// NORMALISED vehicle form. In-memory only, like vehicleNorm (M8) — it exists
// to name which slice of the fold a batch touched, never to be stored.
type corrKey struct {
	origin      string
	vehicleNorm string
}

// ⟦W7, item 9⟧ corrIDKey is the SECOND value key: the origin site plus the
// vehicle RECORD id. It is a separate type from corrKey deliberately — the two
// key spaces are disjoint and a single struct with one value field would let a
// vehicle id and a normalised number collide silently in the scope map.
//
// ⚠ IT CARRIES THE ORIGIN, and that is a ruling rather than a symmetry
// (DR-W7-19(i)): the spec's rule table names the string key with its origin
// term and the id key without one, and that silence is under-specification,
// not a decision. A truck that ran Ice Road→Pad in the morning and Pad→Ice
// Road in the afternoon has two open trips; an UNSCOPED id key lets an arrival
// claiming one origin bind to the run from the other — a wrong-crew binding,
// which is an accountability defect and not a statistics annoyance.
type corrIDKey struct {
	origin    string
	vehicleID string
}

// recomputeScope is the set of derived rows a batch can have changed —
// correlation keys needing the full §9.2 pass, plus point trips/arrivals
// whose state is independent of any correlation (degenerate trips, and
// arrivals that are explicit or structurally uncorrelatable).
//
// SOUNDNESS (the invariant the scoping rests on, proven against the
// wholesale fold below and enforced by
// TestScopedRecomputeMatchesWholesaleOnRandomSoups).
//
// ⚠⚠ ⟦W7, item 9⟧ THIS COMMENT WAS REWRITTEN IN THE SAME CHANGE THAT
// FALSIFIED ITS PREDECESSOR, because a comment asserting a falsified
// invariant is worse than none. It used to read: "pass-2 candidacy REQUIRES
// vehicleNorm+origin equality, so no event under one key can change any row
// under another." ADDING THE ID KEY FALSIFIES THAT ANTECEDENT.
//
// It falsifies it on ONE AXIS AND NOT TWO. Candidacy is now
//
//	origin equality AND (vehicle_id equality OR vehicleNorm equality)
//
// so ORIGIN REMAINS A REQUIRED CONJUNCT and only the VALUE axis widens. The
// scope therefore carries BOTH value keys under the shared origin — `keys`
// and `idKeys` — and the loader takes their CLOSURE over each origin's own
// rows before selecting a working set: a trip reached by one key contributes
// its OTHER key, because an arrival matching that other key is a row this
// batch can change. Scoping only the string key is mutant (m7-2d) and would
// silently exclude rows the wholesale fold rewrites.
//
// The rest of the argument is unchanged: explicit references and cancels are
// trip-addressed, so their effect is bounded by the referenced trip's keys
// (looked up here); degenerate trips are never candidates, so their state
// depends only on their own explicit arrivals and cancels. Over-approximation
// is always safe — any row included in scope is recomputed to its correct
// value, a pure function of the raw tables — and scoping can only be wrong by
// EXCLUDING a row an event could change.
type recomputeScope struct {
	keys     map[corrKey]bool
	idKeys   map[corrIDKey]bool // ⟦W7⟧ the second value key, same origin conjunct
	tripIDs  map[string]bool    // point-scoped trips (degenerate)
	arrivals map[string]bool    // point-scoped arrivals
}

// inScope reports whether a row's vehicle keys put it inside the scope. Either
// key suffices, matching the candidacy rule itself: `origin AND (id OR norm)`.
// ⚠ An empty key never matches — it is not a key.
func (sc *recomputeScope) inScope(origin, vehicleID, vehicleNorm string) bool {
	if vehicleNorm != "" && sc.keys[corrKey{origin: origin, vehicleNorm: vehicleNorm}] {
		return true
	}
	if vehicleID != "" && sc.idKeys[corrIDKey{origin: origin, vehicleID: vehicleID}] {
		return true
	}
	return false
}

// addValueKeys books whatever vehicle keys a row actually carries, under its
// origin. ⚠ An EMPTY key is never booked, on either axis: an empty vehicle key
// is not a matching value (DR-W7-19(ii)), so booking one would scope a slice
// that can never be a candidate universe — and on the string axis it would
// scope every numberless trip at that post at once.
func (sc *recomputeScope) addValueKeys(origin, vehicleID, vehicleNorm string) bool {
	booked := false
	if vehicleNorm != "" {
		sc.keys[corrKey{origin: origin, vehicleNorm: vehicleNorm}] = true
		booked = true
	}
	if vehicleID != "" {
		sc.idKeys[corrIDKey{origin: origin, vehicleID: vehicleID}] = true
		booked = true
	}
	return booked
}

// tripStateFinalize recomputes derived trip/arrival state for everything the
// batch touched — the F28 replacement for the per-event wholesale recompute.
// Runs once per applied batch on the apply transaction, AFTER every
// envelope's raw folds (store.finalize's contract), so a trip referenced by
// a same-batch arrival or cancel is already in the trip table when its key
// is resolved.
func tripStateFinalize(tx *sql.Tx, applied []store.Envelope) error {
	sc := recomputeScope{
		keys:     map[corrKey]bool{},
		idKeys:   map[corrIDKey]bool{},
		tripIDs:  map[string]bool{},
		arrivals: map[string]bool{},
	}
	touched := false
	for _, env := range applied {
		var peek struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(env.Payload, &peek); err != nil {
			return fmt.Errorf("domain: trip_state finalize: %w", err)
		}
		switch peek.Type {
		case "trip_dispatched":
			var p TripDispatched
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				return fmt.Errorf("domain: trip_state finalize: %w", err)
			}
			if p.DestinationSite == nil {
				// Degenerate (§10) — or an uncontrolled-destination run,
				// which shares the same NULL destination_site and the same
				// reasoning: never a correlation candidate, so its
				// appearance cannot change any other row — point scope.
				sc.tripIDs[p.TripID] = true
			} else if !sc.addValueKeys(env.SiteID, p.VehicleID, NormalizeVehicleID(derefString(p.VehicleEntered))) {
				// ⟦W7⟧ A non-degenerate trip carrying NEITHER key can never be
				// a candidate for anything, so no key names it — but it still
				// needs its own trip_state row. Point-scope it, exactly as a
				// degenerate trip is point-scoped and for the same reason.
				sc.tripIDs[p.TripID] = true
			}
			touched = true
		case "trip_arrival":
			var p TripArrival
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				return fmt.Errorf("domain: trip_state finalize: %w", err)
			}
			sc.arrivals[p.ArrivalID] = true
			if p.TripRef != nil {
				// Explicit: the effect beyond this arrival's own row is
				// bounded by the referenced trip (it may consume it out of
				// candidacy) — scope that trip's key.
				if err := addTripToScope(tx, &sc, *p.TripRef); err != nil {
					return err
				}
			} else if p.OriginSite != nil {
				// ⟦W7⟧ BOTH value keys, under the claimed origin. Scoping only
				// the string half is (m7-2d), and after item 8 the string half
				// is absent on exactly the arrivals the id key exists for.
				sc.addValueKeys(*p.OriginSite,
					derefString(p.VehicleID), NormalizeVehicleID(derefString(p.VehicleEntered)))
			}
			// Unsolicited with no origin claim, or with no vehicle key of
			// EITHER kind: structurally uncorrelatable — the point scope alone
			// yields ArrivalPlain, as wholesale did.
			touched = true
		case "trip_cancelled":
			var p TripCancelled
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				return fmt.Errorf("domain: trip_state finalize: %w", err)
			}
			// A cancel changes the cancelled trip's own state and (for a
			// non-degenerate trip) pass-2 candidacy under its key. A cancel
			// for a trip not yet delivered scopes nothing: no derived row
			// exists for it, and the trip's own later event recomputes its
			// key with the cancel visible.
			if err := addTripToScope(tx, &sc, p.TripID); err != nil {
				return err
			}
			touched = true
		case evtArrivalResolved:
			// ⟦Option 1 — James, 2026-09-30⟧ the same three hops as a
			// retraction: the named arrival, its own correlation keys, and
			// the chosen trip (arrival_resolve.go, addResolutionToScope).
			var p ArrivalResolution
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				return fmt.Errorf("domain: trip_state finalize: %w", err)
			}
			if err := addResolutionToScope(tx, &sc, p.ArrivalID, derefString(p.TripID)); err != nil {
				return err
			}
			touched = true
		case evtArrivalRefRetracted:
			// ⟦W9b, item 16⟧ THREE HOPS, not one — the named arrival, its
			// own corrKey where it has one, and the referenced trip. See
			// addRetractionToScope (arrival_unref.go) for why a literal
			// clone of the cancel case above is mutant m25.
			var p ArrivalRefRetraction
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				return fmt.Errorf("domain: trip_state finalize: %w", err)
			}
			if err := addRetractionToScope(tx, &sc, p.ArrivalID); err != nil {
				return err
			}
			touched = true
		}
	}
	if !touched {
		return nil
	}
	return recomputeTripStatesScoped(tx, sc)
}

// addTripToScope resolves a trip reference to its scope: the trip's
// correlation key if it is a real inter-site trip, the trip itself if
// degenerate, nothing if unknown here.
func addTripToScope(tx *sql.Tx, sc *recomputeScope, tripID string) error {
	var origin, vehicleID string
	var vehicle, dest sql.NullString
	err := tx.QueryRow(
		`SELECT origin_site, vehicle_id, vehicle_entered, destination_site FROM trip WHERE trip_id = ?`,
		tripID).Scan(&origin, &vehicleID, &vehicle, &dest)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("domain: trip_state finalize: scope trip %s: %w", tripID, err)
	}
	if !dest.Valid {
		sc.tripIDs[tripID] = true
		return nil
	}
	if !sc.addValueKeys(origin, vehicleID, NormalizeVehicleID(vehicle.String)) {
		sc.tripIDs[tripID] = true
	}
	return nil
}

// derefString reads an optional string as its value, with absence and the
// empty string reading alike. ⚠ That collapse is DR-W7-15's own subject and is
// correct HERE and only here: this function feeds key computation, and an
// empty key and an absent key are the same non-key. Nothing that RENDERS a
// vehicle may use it — a surface that cannot tell "no number recorded" from
// "the number is blank" is telling an officer something it does not know.
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// tripLite / arrivalLite are the in-memory working sets of the recompute.
// vehicleNorm exists ONLY here — never in a table (M8).
type tripLite struct {
	id, origin, vehicleNorm string
	// ⟦W7, item 9⟧ vehicleID is the FIRST correlation key. Unlike vehicleNorm
	// it IS a stored column — it is the record's identity, not a comparison
	// form of a typed string — and "" means the trip named no entity, which is
	// not a key at all.
	vehicleID  string
	epoch, seq int64
	degenerate bool // destination NULL (§10) — never a correlation candidate
}

type arrivalLite struct {
	id                                       string
	site                                     string
	epoch, seq                               int64
	tripRef, vehicleID, vehicle, originClaim *string
}

// sortArrivalsCanonical orders arrivals by the fold's canonical processing
// order (site_id, node_epoch, seq) — the same order the wholesale SQL read
// used, applied in Go because the scoped loader merges several queries.
func sortArrivalsCanonical(as []arrivalLite) {
	sort.Slice(as, func(i, j int) bool {
		if as[i].site != as[j].site {
			return as[i].site < as[j].site
		}
		if as[i].epoch != as[j].epoch {
			return as[i].epoch < as[j].epoch
		}
		return as[i].seq < as[j].seq
	})
}

// sortBySourceKey orders trips oldest-first by the trip event's
// (node_epoch, seq) source key — never rowid, never wall-time (row 10).
func sortBySourceKey(ts []tripLite) {
	sort.Slice(ts, func(i, j int) bool {
		if ts[i].epoch != ts[j].epoch {
			return ts[i].epoch < ts[j].epoch
		}
		if ts[i].seq != ts[j].seq {
			return ts[i].seq < ts[j].seq
		}
		return ts[i].id < ts[j].id
	})
}

func joinIDs(ts []tripLite) string {
	ids := make([]string, len(ts))
	for i, t := range ts {
		ids[i] = t.id
	}
	return strings.Join(ids, ",")
}

// parkedAgainst reports whether a trip consumed by `binder` still counts as
// a candidate for the unsolicited arrival `a` (⟦C-1 / T-092⟧): true only
// when the binder is an EXPLICIT arrival authored LATER than `a` in `a`'s
// OWN stream — `a` was already parked against that trip when the officer
// closed it, and closing one candidate must never resolve the ambiguity for
// them.
//
//   - `explicit == false`: the trip was consumed by an unsolicited
//     correlation. Pass 2 runs in canonical order, so that consumer is
//     earlier than `a` — consumed first, excluded, as always.
//   - Another stream: (node_epoch, seq) orders only WITHIN a stream (§8.3),
//     and arrivals carry no causal reference, so whether the binder came
//     before or after `a` in real time is unknowable here. ⭐ James,
//     2026-09-30: cross-site binders never exclude (option b). An explicit
//     arrival authored at ANOTHER post must never shrink a parked
//     arrival's candidate set: if closing a candidate cross-site would
//     leave a singleton, the parked arrival stays ambiguous in the review
//     lane for an officer to resolve. The accepted price is one extra
//     click in the cross-site SEQUENTIAL case (a same-key predecessor trip
//     closed explicitly elsewhere makes the next unsolicited arrival on
//     that key ambiguous rather than a unique match) — over-alerting,
//     never a silenced alert. A lone cross-bound candidate still falls to
//     plain (the explicit reference keeps the trip), exactly as before.
//   - Same stream: epoch-major, then seq — true authoring order.
func parkedAgainst(a, binder arrivalLite, explicit bool) bool {
	if !explicit {
		return false
	}
	if binder.site != a.site {
		return true
	}
	if binder.epoch != a.epoch {
		return binder.epoch > a.epoch
	}
	return binder.seq > a.seq
}

// recomputeTripStates rebuilds trip_state and arrival_state as a pure
// function of the raw trip / trip_arrival / trip_cancel tables — the §9.1
// fold. Order-independence across streams follows from being set-based; the
// canonical processing order for arrivals is (site_id, node_epoch, seq),
// which every site computes identically, and which within one authoring
// site's stream IS true authoring order.
func recomputeTripStates(tx *sql.Tx) error {
	// Load trips, oldest-first per stream by SOURCE KEY (row 10: never
	// rowid, never wall-time). Candidates for one correlation share an
	// origin site, hence a stream, so (node_epoch, seq) is their true
	// authoring order and "oldest open trip" (§9.2) is well-defined.
	trips := []tripLite{}
	rows, err := tx.Query(`SELECT trip_id, origin_site, node_epoch, seq, vehicle_id, vehicle_entered, destination_site
	                         FROM trip ORDER BY node_epoch, seq, trip_id`)
	if err != nil {
		return fmt.Errorf("domain: recompute: trips: %w", err)
	}
	for rows.Next() {
		var t tripLite
		var vehicle, dest sql.NullString
		if err := rows.Scan(&t.id, &t.origin, &t.epoch, &t.seq, &t.vehicleID, &vehicle, &dest); err != nil {
			rows.Close()
			return fmt.Errorf("domain: recompute: scan trip: %w", err)
		}
		t.vehicleNorm = NormalizeVehicleID(vehicle.String)
		t.degenerate = !dest.Valid
		trips = append(trips, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("domain: recompute: trips: %w", err)
	}

	cancelled := map[string]bool{}
	rows, err = tx.Query(`SELECT trip_id FROM trip_cancel`)
	if err != nil {
		return fmt.Errorf("domain: recompute: cancels: %w", err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("domain: recompute: scan cancel: %w", err)
		}
		cancelled[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("domain: recompute: cancels: %w", err)
	}

	// ⟦C-1 / T-092, 2026-09-29⟧ The stream key (site_id, node_epoch, seq) is
	// loaded here too, not just by the scoped loader: computeStates now
	// compares an unsolicited arrival's position in its OWN stream against
	// the explicit arrival that consumed a candidate (parkedAgainst). A
	// wholesale path that left those fields zero would disagree with the
	// scoped path, and the differential tests hold the two to identical
	// output.
	arrivals := []arrivalLite{}
	rows, err = tx.Query(`SELECT arrival_id, site_id, node_epoch, seq, trip_ref, vehicle_id, vehicle_entered, origin_site
	                        FROM trip_arrival ORDER BY site_id, node_epoch, seq`)
	if err != nil {
		return fmt.Errorf("domain: recompute: arrivals: %w", err)
	}
	if err := scanArrivalLites(rows, func(a arrivalLite) { arrivals = append(arrivals, a) }); err != nil {
		return fmt.Errorf("domain: recompute: arrivals: %w", err)
	}

	// ⟦W9b, item 16⟧ the FOURTH raw input (F4 §3.6). Loaded whole, exactly
	// as the cancel set above is — see retractedArrivalIDs.
	retracted, err := retractedArrivalIDs(tx)
	if err != nil {
		return err
	}
	// ⟦Option 1⟧ the FIFTH raw input, loaded whole (arrival_resolve.go).
	resolved, err := arrivalResolutions(tx)
	if err != nil {
		return err
	}

	tripStates, arrivalStates := computeStates(trips, arrivals, cancelled, retracted, resolved)

	// Pass 3 write-out, wholesale form: clear and rewrite both derived
	// tables in full. Retained as the REFERENCE implementation — the scoped
	// recompute (recomputeTripStatesScoped) must produce identical tables,
	// and the differential tests hold it to this function's output.
	if _, err := tx.Exec(`DELETE FROM trip_state`); err != nil {
		return fmt.Errorf("domain: recompute: clear trip_state: %w", err)
	}
	for _, t := range trips {
		st := tripStates[t.id]
		if _, err := tx.Exec(
			`INSERT INTO trip_state (trip_id, state, arrival_id) VALUES (?, ?, ?)`,
			t.id, st.state, st.arrivalID,
		); err != nil {
			return fmt.Errorf("domain: recompute: trip_state %s: %w", t.id, err)
		}
	}
	if _, err := tx.Exec(`DELETE FROM arrival_state`); err != nil {
		return fmt.Errorf("domain: recompute: clear arrival_state: %w", err)
	}
	for _, a := range arrivals {
		st := arrivalStates[a.id]
		if _, err := tx.Exec(
			`INSERT INTO arrival_state (arrival_id, disposition, trip_id, candidates)
			 VALUES (?, ?, ?, ?)`,
			a.id, st.disposition, st.tripID, st.candidates,
		); err != nil {
			return fmt.Errorf("domain: recompute: arrival_state %s: %w", a.id, err)
		}
	}
	return nil
}

// arrState / tripStateRow are computeStates' outputs — one derived row each.
type arrState struct {
	disposition string
	tripID      *string
	candidates  *string
}

type tripStateRow struct {
	state     string
	arrivalID *string
}

// computeStates is THE §9.1/§9.2 fold arithmetic, extracted so the wholesale
// reference and the scoped recompute share it verbatim: given a working set
// of trips (any order), arrivals in canonical (site_id, node_epoch, seq)
// order, the cancel set and ⟦W9b⟧ the retracted-reference set, it returns
// every trip's and arrival's derived row. A pure in-memory function — all
// determinism arguments about the fold live here, loading and write-out
// strategy live with the callers.
//
// ⟦W9b, item 16⟧ `retracted` is F4 §3.6's FOURTH RAW INPUT: the arrival ids
// whose explicit reference a human withdrew. It is a raw table like the
// other three — never an effective read — so this function stays the pure
// function of raw tables that I-2's determinism proof and the §9.3 inverse
// scan both rest on.
//
// ⟦Option 1 — James, 2026-09-30⟧ `resolved` is the FIFTH raw input
// (arrival_resolve.go): unsolicited arrivals an officer resolved on the
// review lane, each to a chosen trip or to "none of these". Raw table, never
// an effective read, for the same determinism reasons as `retracted`.
func computeStates(trips []tripLite, arrivals []arrivalLite, cancelled, retracted map[string]bool, resolved map[string]arrivalResolution) (map[string]tripStateRow, map[string]arrState) {
	// boundTo[tripID] = the arrival that closed it.
	boundTo := map[string]string{}
	states := map[string]arrState{}
	// ⟦C-1 / T-092, 2026-09-29⟧ explicitBinder[tripID] = the EXPLICIT (Pass
	// 1) arrival that closed it, for trips bound in Pass 1 only. Pass 2
	// consults it to tell "consumed before this unsolicited arrival was
	// recorded" from "closed AFTER it was already parked against it" — see
	// parkedAgainst. A trip bound in Pass 2 never appears here.
	explicitBinder := map[string]arrivalLite{}

	// Pass 1 — explicit references bind unconditionally (an explicit arrival
	// for a cancelled trip is exactly the arrived-after-cancel review case,
	// §9.1). First arrival in canonical order wins a doubly-referenced trip.
	//
	// ⟦Option 1 — James, 2026-09-30⟧ An UNSOLICITED arrival an officer
	// RESOLVED on the review lane is a human selection and binds HERE, in
	// canonical order with the explicit references — first binder still
	// wins a doubly-bound trip — and never re-enters Pass 2. "None of
	// these" lands it as plain: the people are here, no trip is closed.
	for _, a := range arrivals {
		if a.tripRef == nil {
			res, ok := resolved[a.id]
			if !ok {
				continue
			}
			if res.tripID == "" {
				states[a.id] = arrState{disposition: ArrivalPlain}
				continue
			}
			id := res.tripID
			if _, taken := boundTo[id]; !taken {
				boundTo[id] = a.id
				explicitBinder[id] = a
			}
			states[a.id] = arrState{disposition: ArrivalResolved, tripID: &id}
			continue
		}
		ref := *a.tripRef
		if retracted[a.id] {
			// ⟦W9b, item 16⟧ F4 §3.6: a withdrawn reference BINDS NOTHING —
			// it is skipped here, never counted in boundTo, so the trip it
			// named reverts in Pass 3 to whatever the surviving raw facts
			// say (`open`, `cancelled`, or still `arrived` where something
			// else BINDS it — [[D-58]]). Because the runner-up of a
			// doubly-referenced trip is simply the next arrival to reach
			// this line, the promotion in T12b is the SAME rule, not a
			// special case.
			//
			// ⚠ It keeps its trip_id: the review-lane row names the
			// REPUDIATED trip, and `arrival_state.trip_id` is where that
			// lives. It is not a binding — Pass 3 reads boundTo, never
			// this field.
			//
			// ⚠ It does NOT fall through to Pass 2. That is F28 scope
			// soundness, not tidiness: re-correlating it would bind under
			// the arrival's OWN corrKey, which a mis-click's whole point is
			// that it may differ from the trip's, and only the trip's key
			// is reliably scoped. Structural here — Pass 2 skips every
			// arrival with a non-nil tripRef, and a retraction never
			// clears the raw column.
			states[a.id] = arrState{disposition: ArrivalRefRetracted, tripID: &ref}
			continue
		}
		if _, taken := boundTo[ref]; !taken {
			boundTo[ref] = a.id
			explicitBinder[ref] = a
		}
		states[a.id] = arrState{disposition: ArrivalExpected, tripID: &ref}
	}

	// Pass 2 — unsolicited correlation (§9.2). Candidates: same normalised
	// vehicle + same origin, not cancelled, not already bound, trip actually
	// known here. Exactly one → bind (necessarily the oldest open, since
	// trips are enumerated oldest-first and earlier canonical arrivals have
	// already consumed earlier trips). Zero → plain visit. Two or more →
	// AMBIGUOUS: bind NOTHING, land in the review lane with the candidate
	// list — never auto-pick, a wrong match would silence a real alert.
	//
	// ⟦C-1 / T-092, 2026-09-29⟧ "NOT ALREADY BOUND" IS QUALIFIED: a trip
	// closed by an EXPLICIT arrival authored LATER IN THIS ARRIVAL'S OWN
	// STREAM still counts as a candidate (parkedAgainst). Without that, an
	// arrival parked as ambiguous over [T1, T2] was re-evaluated the moment
	// an officer pressed "Arrived" on T1's row, saw the singleton {T2}, and
	// BOUND BY ELIMINATION — closing T2 at every post with none of its
	// occupants aboard, deleting its expectation and overdue alert, clearing
	// the review row that said "verify which crew this is", and filing T2's
	// crew "not aboard at arrival" on both evacuation sheets. Measured on L13
	// (sc-test L08 T-092). The candidate set an arrival is judged against is
	// therefore the set as it stood AT ITS OWN POSITION in its stream: a
	// same-stream explicit arrival BEFORE it consumed a trip (the sequential
	// case — bind the survivor); one AFTER it did not (the parked case —
	// stay parked). Such a still-counted candidate is never itself bound
	// here: it already has its explicit binder. The parked arrival binds
	// nothing until a human acts on it, and every post computes the same
	// answer because stream order is in the raw table.
	for _, a := range arrivals {
		if a.tripRef != nil {
			continue
		}
		if _, ok := resolved[a.id]; ok {
			// ⟦Option 1⟧ a human already decided; Pass 1 holds its state.
			continue
		}
		// ⟦W7, item 9 + item 8⟧ THE SHORT-CIRCUIT IS RE-POINTED AT "NO VEHICLE
		// KEY OF EITHER KIND" (DR-W7-19(ii)). It used to read
		// `a.vehicle == nil`, which falls to `plain` BEFORE the id key is ever
		// consulted — and that is exactly the case the id key exists for,
		// because after item 8 a numberless arrival is the NORMAL shape for a
		// role-only vehicle. Leaving it as it was is mutant (m7-2g) arm B.
		//
		// ⚠ AN EMPTY OR ABSENT VEHICLE STRING CONTRIBUTES NO STRING-KEY
		// CANDIDACY, in either direction. NormalizeVehicleID("") is "", and a
		// plain equality with no empty guard makes EVERY numberless trip from
		// one origin a candidate for EVERY numberless arrival from it — so if
		// exactly one is open the system binds that crew to that run,
		// silently. That is (m7-2g) arm A, and it is a wrong-crew binding.
		arrNorm := ""
		if a.vehicle != nil {
			arrNorm = NormalizeVehicleID(*a.vehicle)
		}
		arrID := derefString(a.vehicleID)
		if a.originClaim == nil || (arrNorm == "" && arrID == "") {
			// Structurally not correlatable; stays a plain visit.
			states[a.id] = arrState{disposition: ArrivalPlain}
			continue
		}
		// ⚠ THE TWO CANDIDATE GROUPS ARE KEPT SEPARATE AND CONCATENATED, NOT
		// UNIONED AND SORTED. The id key is consulted FIRST — this criterion's
		// own headline rule — so its candidates are listed first, and the
		// candidate list a review-lane row shows an officer is in
		// key-consultation order rather than alphabetical order. A trip hit by
		// BOTH keys is appended ONCE, in the id group: each trip is visited
		// once by this loop, which is what makes the un-deduped union
		// impossible to write here (m7-2e) rather than merely unwritten.
		var byID, byString, cancelledMatches []tripLite
		// ⟦C-1⟧ openCount is how many candidates are genuinely unbound; the
		// rest (if any) are parked-against trips a later same-stream explicit
		// arrival has since closed. Only an unbound singleton is ever bound.
		openCount := 0
		for _, t := range trips {
			// Degenerate trips (destination_site NULL, §10) are NEVER
			// candidates: a local check-in is not an inbound expectation, it
			// appears on no board, and a stale open one (crash between the
			// check-in's two events — C8's window) would otherwise invisibly
			// poison every future (vehicle, origin) correlation. C6 review,
			// Fix 2. Uncontrolled-destination runs carry the same NULL
			// destination_site DELIBERATELY: nobody at that pad can confirm
			// anything, so binding an unsolicited arrival elsewhere to one
			// would be exactly the wrong match this rule exists to refuse.
			// ORIGIN IS A REQUIRED CONJUNCT ON BOTH KEYS (DR-W7-19(i)) — see
			// corrIDKey for why the id key carries it too.
			if t.degenerate || t.origin != *a.originClaim {
				continue
			}
			// ⚠⚠ THE ID KEY COMPARES THE RAW `vehicle_id`, NEVER THE
			// MERGE-RESOLVED ONE, and that is `W7-3`'s whole subject:
			// `same_as` changes COUNTING and DISPLAY and CANNOT change
			// correlation. Resolving here would let an officer joining two
			// duplicate records retroactively re-bind arrivals to trips —
			// a merge silently reaching into the accountability record —
			// and it is also how two role-only trucks sharing one joined
			// record would bind the wrong crew to the wrong arrival, which
			// is the case §5.6 rule 2 names in terms.
			idHit := arrID != "" && t.vehicleID == arrID
			strHit := arrNorm != "" && t.vehicleNorm == arrNorm
			if !idHit && !strHit {
				continue
			}
			if cancelled[t.id] {
				// A bound cancelled trip (arrived_after_cancel) is out, as
				// it always was; only an unbound cancelled match is the
				// §9.1 contradiction surfaced below.
				if boundTo[t.id] == "" {
					cancelledMatches = append(cancelledMatches, t)
				}
				continue
			}
			if boundTo[t.id] != "" {
				// Consumed. Excluded — UNLESS the consumer is an explicit
				// arrival later in this arrival's own stream, in which case
				// this arrival was already parked against the trip and the
				// trip stays in its candidate set (⟦C-1⟧ above).
				binder, explicit := explicitBinder[t.id]
				if !parkedAgainst(a, binder, explicit) {
					continue
				}
			} else {
				openCount++
			}
			if idHit {
				byID = append(byID, t)
			} else {
				byString = append(byString, t)
			}
		}
		sortBySourceKey(byID)
		sortBySourceKey(byString)
		candidates := append(append([]tripLite{}, byID...), byString...)
		switch {
		case len(candidates) >= 2:
			// AMBIGUOUS — whether the candidates are all still open or some
			// were since closed by later explicit arrivals. ⟦C-1⟧ The parked
			// row keeps its full candidate list, so what the officer reads
			// after acting on one row is what they read before.
			joined := joinIDs(candidates)
			states[a.id] = arrState{disposition: ArrivalAmbiguous, candidates: &joined}
		case openCount == 1:
			// Exactly one candidate and it is unbound (a lone parked-against
			// candidate falls through to the case below, as it did when the
			// old filter excluded it outright).
			id := candidates[0].id
			boundTo[id] = a.id
			states[a.id] = arrState{disposition: ArrivalCorrelated, tripID: &id}
		default:
			// No open trip. If a CANCELLED trip matches, this is §9.1's
			// contradiction on the realistic path — the unsolicited author
			// can never have heard the cancel — so it goes to the review
			// lane, still binding NOTHING: the trip stays cancelled, and a
			// human decides what actually happened. Plain only when nothing
			// at all matched. C6 review, Ruling 2.
			if len(cancelledMatches) > 0 {
				sortBySourceKey(cancelledMatches)
				joined := joinIDs(cancelledMatches)
				states[a.id] = arrState{disposition: ArrivalCancelledMatch, candidates: &joined}
			} else {
				states[a.id] = arrState{disposition: ArrivalPlain}
			}
		}
	}

	// Pass 3 — trip states, set-based (§9.1): both facts always survive, so
	// cancel+arrival yields arrived_after_cancel no matter which was
	// delivered (or even authored) first.
	tripStates := make(map[string]tripStateRow, len(trips))
	for _, t := range trips {
		arrivalID, arrived := boundTo[t.id]
		state := TripStateOpen
		var arrIDPtr *string
		switch {
		case arrived && cancelled[t.id]:
			state = TripStateArrivedAfterCancel
			arrIDPtr = &arrivalID
		case arrived:
			state = TripStateArrived
			arrIDPtr = &arrivalID
		case cancelled[t.id]:
			state = TripStateCancelled
		}
		tripStates[t.id] = tripStateRow{state: state, arrivalID: arrIDPtr}
	}

	for _, a := range arrivals {
		if _, ok := states[a.id]; !ok {
			// An explicit arrival whose trip has not been delivered yet still
			// has a state from pass 1; this branch is unreachable, kept as a
			// guard against a future edit breaking the invariant silently.
			//
			// (An explicit reference to a trip we have not heard of yet stays
			// "expected" with the trip id recorded; the state row for the
			// trip itself appears when the trip event lands and the
			// recompute reruns.)
			states[a.id] = arrState{disposition: ArrivalPlain}
		}
	}
	return tripStates, states
}

// recomputeTripStatesScoped is the F28 scoped recompute: the identical
// computeStates arithmetic run over the WORKING SET of a batch's scope
// instead of the whole database, writing only the scope's derived rows.
//
// Loading rule (soundness per recomputeScope's invariant note):
//   - every key K in scope loads ALL non-degenerate trips of K plus ALL
//     unsolicited arrivals of K — the full candidate universe for that key;
//   - every trip in the working set loads ALL explicit arrivals referencing
//     it (wherever authored) — pass 1's bindings on scope trips must be
//     complete or pass 2 could bind an already-consumed trip;
//   - point trips/arrivals load individually;
//   - the cancel set loads whole, as the wholesale fold does (cancels are
//     rare human actions; the table is orders of magnitude smaller than the
//     trip-family tables).
//
// Rows outside the scope are untouched: by the invariant no in-batch event
// can have changed them, and trip/arrival rows are immutable so a row's key
// never changes after insert. Over-inclusion is safe (any included row is
// recomputed to its correct pure-function value); the differential tests
// hold this function to recomputeTripStates' byte-identical output.
func recomputeTripStatesScoped(tx *sql.Tx, sc recomputeScope) error {
	// -- working-set trips --
	var trips []tripLite
	seenTrip := map[string]bool{}
	origins := map[string]bool{}
	for k := range sc.keys {
		origins[k.origin] = true
	}
	for k := range sc.idKeys {
		origins[k.origin] = true
	}
	// ⟦W7, item 9⟧ THE CLOSURE, and it is the half a one-key scope does not
	// have. Candidacy is `origin AND (id OR norm)`, so a trip reached by ONE
	// key drags its OTHER key into scope: an arrival matching that other key
	// is a row this batch can change, and a scope that stopped at the first
	// key would EXCLUDE it — which is the only direction scoping can be wrong
	// in (see recomputeScope's soundness note). The closure runs over each
	// origin's own rows, which the loader reads anyway, so it costs one extra
	// in-memory pass and no extra query.
	//
	// ⚠ It is a FIXED POINT, not a single expansion: trip P shares an id with
	// arrival Q, Q shares a number with trip R, R shares an id with arrival S.
	// One round would stop at R and leave S's row stale.
	pooledTrips := map[string][]tripLite{}
	pooledArrivals := map[string][]arrivalLite{}
	for _, origin := range sortedKeys(origins) {
		// Non-degenerate trips of this origin; the literal IS NOT NULL
		// predicate keeps this eligible for a partial index. Key membership
		// (the normalised vehicle) is decided in memory — the normalised
		// form has no column, by design (M8).
		rows, err := tx.Query(
			`SELECT trip_id, origin_site, node_epoch, seq, vehicle_id, vehicle_entered, destination_site
			   FROM trip WHERE destination_site IS NOT NULL AND origin_site = ?
			  ORDER BY node_epoch, seq, trip_id`, origin)
		if err != nil {
			return fmt.Errorf("domain: scoped recompute: trips of %s: %w", origin, err)
		}
		if err := scanTripLites(rows, func(t tripLite) {
			pooledTrips[origin] = append(pooledTrips[origin], t)
		}); err != nil {
			return fmt.Errorf("domain: scoped recompute: trips of %s: %w", origin, err)
		}
		rows, err = tx.Query(
			`SELECT arrival_id, site_id, node_epoch, seq, trip_ref, vehicle_id, vehicle_entered, origin_site
			   FROM trip_arrival WHERE trip_ref IS NULL AND origin_site = ?`, origin)
		if err != nil {
			return fmt.Errorf("domain: scoped recompute: unsolicited of %s: %w", origin, err)
		}
		if err := scanArrivalLites(rows, func(a arrivalLite) {
			pooledArrivals[origin] = append(pooledArrivals[origin], a)
		}); err != nil {
			return fmt.Errorf("domain: scoped recompute: unsolicited of %s: %w", origin, err)
		}
	}
	// The fixed point, per origin.
	for _, origin := range sortedKeys(origins) {
		for {
			grew := false
			note := func(vehicleID, vehicleNorm string) {
				if vehicleNorm != "" {
					k := corrKey{origin: origin, vehicleNorm: vehicleNorm}
					if !sc.keys[k] {
						sc.keys[k] = true
						grew = true
					}
				}
				if vehicleID != "" {
					k := corrIDKey{origin: origin, vehicleID: vehicleID}
					if !sc.idKeys[k] {
						sc.idKeys[k] = true
						grew = true
					}
				}
			}
			for _, t := range pooledTrips[origin] {
				if sc.inScope(origin, t.vehicleID, t.vehicleNorm) {
					note(t.vehicleID, t.vehicleNorm)
				}
			}
			for _, a := range pooledArrivals[origin] {
				norm := ""
				if a.vehicle != nil {
					norm = NormalizeVehicleID(*a.vehicle)
				}
				if sc.inScope(origin, derefString(a.vehicleID), norm) {
					note(derefString(a.vehicleID), norm)
				}
			}
			if !grew {
				break
			}
		}
	}
	for _, origin := range sortedKeys(origins) {
		for _, t := range pooledTrips[origin] {
			if sc.inScope(origin, t.vehicleID, t.vehicleNorm) && !seenTrip[t.id] {
				seenTrip[t.id] = true
				trips = append(trips, t)
			}
		}
	}
	for _, id := range sortedKeys(sc.tripIDs) {
		if seenTrip[id] {
			continue
		}
		rows, err := tx.Query(
			`SELECT trip_id, origin_site, node_epoch, seq, vehicle_id, vehicle_entered, destination_site
			   FROM trip WHERE trip_id = ?`, id)
		if err != nil {
			return fmt.Errorf("domain: scoped recompute: trip %s: %w", id, err)
		}
		if err := scanTripLites(rows, func(t tripLite) {
			seenTrip[t.id] = true
			trips = append(trips, t)
		}); err != nil {
			return fmt.Errorf("domain: scoped recompute: trip %s: %w", id, err)
		}
	}
	sortBySourceKey(trips)

	// -- cancel set (whole table, as wholesale) --
	cancelled := map[string]bool{}
	rows, err := tx.Query(`SELECT trip_id FROM trip_cancel`)
	if err != nil {
		return fmt.Errorf("domain: scoped recompute: cancels: %w", err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("domain: scoped recompute: scan cancel: %w", err)
		}
		cancelled[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("domain: scoped recompute: cancels: %w", err)
	}

	// -- working-set arrivals --
	var arrivals []arrivalLite
	seenArr := map[string]bool{}
	collect := func(a arrivalLite) {
		if !seenArr[a.id] {
			seenArr[a.id] = true
			arrivals = append(arrivals, a)
		}
	}
	for _, origin := range sortedKeys(origins) {
		// Unsolicited arrivals claiming this origin, selected from the pool
		// the closure above already read; membership is decided in memory
		// because the normalised form has no column, by design (M8).
		for _, a := range pooledArrivals[origin] {
			norm := ""
			if a.vehicle != nil {
				norm = NormalizeVehicleID(*a.vehicle)
			}
			if sc.inScope(origin, derefString(a.vehicleID), norm) {
				collect(a)
			}
		}
	}
	for _, t := range trips {
		// Every explicit arrival referencing a working-set trip: pass 1's
		// bindings on scope trips must be complete (see the loading rule).
		rows, err := tx.Query(
			`SELECT arrival_id, site_id, node_epoch, seq, trip_ref, vehicle_id, vehicle_entered, origin_site
			   FROM trip_arrival WHERE trip_ref = ?`, t.id)
		if err != nil {
			return fmt.Errorf("domain: scoped recompute: refs of %s: %w", t.id, err)
		}
		if err := scanArrivalLites(rows, collect); err != nil {
			return fmt.Errorf("domain: scoped recompute: refs of %s: %w", t.id, err)
		}
	}
	for _, id := range sortedKeys(sc.arrivals) {
		if seenArr[id] {
			continue
		}
		rows, err := tx.Query(
			`SELECT arrival_id, site_id, node_epoch, seq, trip_ref, vehicle_id, vehicle_entered, origin_site
			   FROM trip_arrival WHERE arrival_id = ?`, id)
		if err != nil {
			return fmt.Errorf("domain: scoped recompute: arrival %s: %w", id, err)
		}
		if err := scanArrivalLites(rows, collect); err != nil {
			return fmt.Errorf("domain: scoped recompute: arrival %s: %w", id, err)
		}
	}
	sortArrivalsCanonical(arrivals)

	// ⟦W9b, item 16⟧ the FOURTH raw input (F4 §3.6), loaded whole here as it
	// is in the wholesale reference — so the ONLY thing the scope decides is
	// which derived ROWS are rewritten, which is what recomputeScope's
	// soundness argument is about and what mutant m25 attacks.
	retracted, err := retractedArrivalIDs(tx)
	if err != nil {
		return err
	}
	// ⟦Option 1⟧ the FIFTH raw input, loaded whole here as in the wholesale
	// reference — the scope decides only which derived ROWS are rewritten.
	resolved, err := arrivalResolutions(tx)
	if err != nil {
		return err
	}

	tripStates, arrivalStates := computeStates(trips, arrivals, cancelled, retracted, resolved)

	// Scoped write-out: replace exactly the working set's rows. trip_state /
	// arrival_state rows exist iff their trip/arrival row exists and those
	// are append-only, so there is never a row to delete that is not being
	// replaced.
	for _, t := range trips {
		st := tripStates[t.id]
		if _, err := tx.Exec(
			`INSERT OR REPLACE INTO trip_state (trip_id, state, arrival_id) VALUES (?, ?, ?)`,
			t.id, st.state, st.arrivalID,
		); err != nil {
			return fmt.Errorf("domain: scoped recompute: trip_state %s: %w", t.id, err)
		}
	}
	for _, a := range arrivals {
		st := arrivalStates[a.id]
		if _, err := tx.Exec(
			`INSERT OR REPLACE INTO arrival_state (arrival_id, disposition, trip_id, candidates)
			 VALUES (?, ?, ?, ?)`,
			a.id, st.disposition, st.tripID, st.candidates,
		); err != nil {
			return fmt.Errorf("domain: scoped recompute: arrival_state %s: %w", a.id, err)
		}
	}
	return nil
}

// sortedKeys returns a string-keyed set's members sorted — scope iteration
// stays deterministic (final state would be identical regardless, since every
// write is keyed by primary key, but deterministic query order keeps runs
// reproducible).
func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// scanTripLites drains a trip query's rows into tripLite via yield.
func scanTripLites(rows *sql.Rows, yield func(tripLite)) error {
	defer rows.Close()
	for rows.Next() {
		var t tripLite
		var vehicle, dest sql.NullString
		if err := rows.Scan(&t.id, &t.origin, &t.epoch, &t.seq, &t.vehicleID, &vehicle, &dest); err != nil {
			return err
		}
		t.vehicleNorm = NormalizeVehicleID(vehicle.String)
		t.degenerate = !dest.Valid
		yield(t)
	}
	return rows.Err()
}

// scanArrivalLites drains an arrival query's rows into arrivalLite via yield.
func scanArrivalLites(rows *sql.Rows, yield func(arrivalLite)) error {
	defer rows.Close()
	for rows.Next() {
		var a arrivalLite
		if err := rows.Scan(&a.id, &a.site, &a.epoch, &a.seq, &a.tripRef, &a.vehicleID, &a.vehicle, &a.originClaim); err != nil {
			return err
		}
		yield(a)
	}
	return rows.Err()
}

// --- placeholder vehicle ids (F43) ---

// placeholderVehicleIDs is the set of entries that are NOT AN ANSWER to
// "which vehicle is this?", in NormalizeVehicleID form (lowercased,
// NFC, whitespace-collapsed).
//
// WHY THIS EXISTS. Until 2026-08-21 the gate form (web/templates/gate.html)
// shipped `value="walk-in"` prefilled into the vehicle field, and nothing
// anywhere treated that string as special: it was stored verbatim as the
// vehicle id like any plate. So every check-in where a hurried operator did
// not touch the field recorded a truck as "walk-in" — and the operator who
// works this post reports that at -30 there are NO walk-ins, everyone
// arrives in a vehicle, so the default was wrong on 100% of arrivals. What
// it destroyed is the vehicle->people association a muster needs during an
// evacuation: you cannot ask "who came in on TRK-14, is TRK-14 accounted
// for?" of a record that says "walk-in".
//
// Blanking the form field alone would not have been enough, and that is the
// reason this list lives in the DOMAIN rather than in the markup. The
// failure mode is an operator under time pressure submitting whatever will
// satisfy the form — "x", "-", "n/a" — and every path that authors a trip
// (the gate form, the dispatch console, a raw POST, any future caller) has
// to be closed, not just the one input that happened to carry the bad
// default.
//
// WHY EXACT WHOLE-VALUE MATCH, not substring. "TRK-NA-3" is a plausible
// unit number; refusing it because it contains "na" would be an
// unpredictable rule that an operator cannot reason about with a truck
// idling. The rule is legible: the WHOLE entry must not be a non-answer.
// Note this deliberately still admits "gate walk-in" and similar — the
// value is then at least an operator's own words rather than a default
// nobody chose.
//
// WHY "walk-in" IS ON THIS LIST even though some site somewhere might have
// real pedestrians. vehicle_entered is the §9.2 CORRELATION KEY. A magic
// string shared by every pedestrian collides with itself: deploy's own
// walkthrough already records that "a vehicle string does NOT identify one
// trip... any lookup keyed on vehicle alone resolves to the wrong trip".
// So "walk-in" is not a working way to record arriving on foot even where
// people do; that would need a first-class representation with its own
// muster semantics, not a sentinel smuggled through a free-text key. Until
// such a thing exists, refusing the sentinel is strictly better than
// accepting a colliding non-identifier. If a site ever genuinely needs
// pedestrian arrivals, change THAT, not this list.
var placeholderVehicleIDs = map[string]bool{
	// The specific defect this rule was written for, and its spellings.
	"walk-in": true, "walk in": true, "walkin": true, "walkins": true,
	"walk-ins": true, "foot": true, "on foot": true,
	// General non-answers. Each of these records that the operator declined
	// to say which vehicle, which is the same information as an empty field
	// and is already refused in that form.
	"n/a": true, "na": true, "n a": true, "none": true, "nil": true,
	"null": true, "nothing": true, "unknown": true, "unk": true,
	"tbd": true, "tba": true, "?": true, "??": true, "???": true,
	"-": true, "--": true, "---": true, ".": true, "..": true, "...": true,
	"x": true, "xx": true, "xxx": true, "n/k": true, "nk": true,
	// The field's own label typed back into it — what a browser autofill or
	// a copy-paste of the prompt produces.
	"vehicle": true, "vehicle id": true, "vehicle-id": true, "plate": true,
	"test": true, "asdf": true,
}

// IsPlaceholderVehicleID reports whether an entered vehicle id is a
// non-answer rather than an identification of a vehicle. See
// placeholderVehicleIDs for why this is a domain rule and not form
// validation. The empty string is NOT reported here — callers refuse that
// separately, with their own message, and have done since C6.
func IsPlaceholderVehicleID(entered string) bool {
	// Local name is not `norm`: that would shadow the norm package this
	// file imports.
	key := NormalizeVehicleID(entered)
	return key != "" && placeholderVehicleIDs[key]
}

// vehicleIDRefusal is the shared refusal text, so the gate form and the
// dispatch console tell the operator the same thing for the same reason.
func vehicleIDRefusal(entered string) error {
	return fmt.Errorf(
		"%q does not identify a vehicle. Enter the plate or unit number of the vehicle these people arrived in — "+
			"it is what links them to that vehicle on the muster, and an evacuation reads it. "+
			"(Everyone arrives in a vehicle at this post; there is no placeholder for 'no vehicle'.)",
		entered)
}
