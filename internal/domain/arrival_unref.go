// arrival_unref.go — W9b (item 16): F4's un-reference. The replicated event
// `arrival_ref_retracted`, its STRICT INSERT-only side table
// `arrival_ref_retraction`, the raw set `computeStates` loads beside trips,
// arrivals and cancels, the fourth review-lane scan, and the authoring edge
// with F4 §3.6's three pre-append refusals.
// (design/trip-fact-correction-v1.md rev 4 §3.6; WINDOW-BUILD-PLAN-v1 §3 W9b,
// criteria RE-LOCKED 2026-09-02.)
//
// This is F6's CANCELLATION algebra, not W3/W9a's amendment family: the event
// names the arrival whose `TripRef` selection is withdrawn and NOTHING else —
// no replacement trip, ever — and there is no value slot for `Outcome` to
// resolve, which is why §8 registers no carrier for it and W9b-4 exempts it
// instead (`w4ExemptNonCarriers`).
//
// ⚠ THE EVENT IS A FOURTH RAW INPUT TO `computeStates`, not a projection
// added beside an old table (F4 §3.6's "⚠ It is a FOURTH RAW INPUT" bullet,
// W9R2-A-9). Both callers of `computeStates` load the retracted set through
// `retractedArrivalIDs` below and pass it in; Pass 1 SKIPS any arrival whose
// id is in that set, so the trip it referenced reverts in Pass 3 to whatever
// the surviving raw facts say. `computeStates` stays a PURE function of raw
// tables — the retraction table is raw, never an effective read — so I-2's
// determinism proof keeps its exact form and the §9.3 inverse scan stays
// satisfiable.
//
// ⚠ THE RETRACTED ARRIVAL MUST NOT FALL THROUGH TO PASS 2, and that is an F28
// SCOPE-SOUNDNESS requirement rather than an I-2 preference (F4 §3.6, the
// "Binds nothing" bullet). A mis-click's whole point is that the arrival was
// bound to the WRONG trip, so the arrival's own `(origin_claim, vehicle)`
// corrKey may differ from the trip's; only the trip's key is reliably in
// scope. Re-correlating the retracted arrival would bind under a corrKey the
// batch never scoped and disagree with the wholesale fold on the typical
// mis-click — a wrong `arrival_state` AT ONE POST ONLY. It is structural
// here: a retracted arrival still carries `trip_ref != nil`, and Pass 2's
// first line skips referenced arrivals.
//
// ⚠ THE SCOPE ENTRY IS THREE HOPS, not one (`addRetractionToScope` below).
// A cancel names its trip: one hop. A retraction names only an ARRIVAL, so
// bounding it is `arrival_id -> trip_arrival.trip_ref -> trip -> corrKey`,
// and `addTripToScope` scopes NOTHING on a miss. Mutant m25 is exactly the
// clone of the `trip_cancelled` case that omits the arrival's own corrKey.
//
// ⚠ A RETRACTION NAMING AN UNSOLICITED ARRIVAL IS INERT, deliberately. F4
// §3.6 refuses it at the authoring surface ("an unsolicited arrival's binding
// is fold-derived — correcting THAT is the review lane, and §5 rule 4 forbids
// touching it"), but a peer's wire event reaches the fold without passing the
// surface, and the fold NEVER refuses. So the fold-side rule is stated where
// it cannot be bypassed: the disposition and the un-bind apply to an arrival
// that carries a `TripRef`, and an arrival with none keeps whatever Pass 2
// derives for it. Both branches are reachable — the refusal covers the local
// path, this covers the replicated one.
package domain

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"sitecontrol/internal/store"
)

// --- the frozen wire (F4 §3.6, gate B-2's bytes) -------------------------

const evtArrivalRefRetracted = "arrival_ref_retracted"

// ArrivalRefRetraction is the wire payload. F4 §3.6 writes it out: EXACTLY
// three fields — `type`, `arrival_id`, `recorded_by` — and NO trip field of
// any kind, so a re-point is not merely absent from the surfaces, it is
// unrepresentable on the wire (W9b-2's filter-independent half).
//
// ⚠ NAMING, and it diverges from F4 §3.6's code block on purpose. The design
// names BOTH the payload struct and the new disposition `ArrivalRefRetracted`;
// Go has one identifier space for the two. The DISPOSITION keeps the design's
// name, because that is the name the criteria use throughout (W9b-1's
// "the arrival takes the `ArrivalRefRetracted` disposition", W9b-5's
// "`ArrivalRefRetracted` review-lane row"), and the payload takes the
// projection's spelling instead. Reported in the build report.
type ArrivalRefRetraction struct {
	Type       string  `json:"type"` // "arrival_ref_retracted"
	ArrivalID  string  `json:"arrival_id"`
	RecordedBy *string `json:"recorded_by,omitempty"` // post attribution (D-5)
}

// ArrivalRefRetractionRow is the projection row, tags = column names.
type ArrivalRefRetractionRow struct {
	SiteID     string  `json:"site_id"`
	NodeEpoch  int64   `json:"node_epoch"`
	Seq        int64   `json:"seq"`
	ArrivalID  *string `json:"arrival_id"`
	RecordedBy *string `json:"recorded_by"`
}

// --- the projection ------------------------------------------------------

// arrivalRefRetractionProjection is F4 §3.6's side table: STRICT, INSERT-only
// by trigger, PK the envelope's OWN key, dump-participating. A NEW table, so
// IF NOT EXISTS heals a stored database on next open and no SchemaVersion
// bump is owed — the way person_amendment, company_amendment and W9a's pair
// landed.
//
// `recorded_by` is `omitempty` on the wire, so the column is NULLABLE: a
// NOT NULL there would refuse a legal retraction at fold time, and the fold
// is never allowed to refuse.
func arrivalRefRetractionProjection() store.Projection {
	return store.Projection{
		Name:    "arrival_ref_retraction",
		Table:   "arrival_ref_retraction",
		OrderBy: "site_id, node_epoch, seq",
		Schema: `CREATE TABLE IF NOT EXISTS arrival_ref_retraction (
			site_id     TEXT    NOT NULL,
			node_epoch  INTEGER NOT NULL,
			seq         INTEGER NOT NULL,
			arrival_id  TEXT,
			recorded_by TEXT,
			PRIMARY KEY (site_id, node_epoch, seq)
		) STRICT;
		-- The two reads that are not full scans: "is this arrival already
		-- retracted?" (the third refusal) and the review lane's per-arrival
		-- attribution. Indexes are not dumped/replicated shape, so no
		-- SchemaVersion bump (the fix-perf precedent trip_arrival cites).
		CREATE INDEX IF NOT EXISTS idx_ref_retraction_arrival ON arrival_ref_retraction (arrival_id);
		CREATE TRIGGER IF NOT EXISTS arrival_ref_retraction_insert_only_update
			BEFORE UPDATE ON arrival_ref_retraction BEGIN
			SELECT RAISE(ABORT, 'arrival_ref_retraction is INSERT-only: a withdrawal is a recorded assertion, never rewritten in place');
		END;
		CREATE TRIGGER IF NOT EXISTS arrival_ref_retraction_insert_only_delete
			BEFORE DELETE ON arrival_ref_retraction BEGIN
			SELECT RAISE(ABORT, 'arrival_ref_retraction is INSERT-only: a deletable retraction row is a silent re-binding of a trip nobody re-recorded (D-1)');
		END;`,
		Apply: applyArrivalRefRetracted,
	}
}

// applyArrivalRefRetracted is the fold: a BARE INSERT. No lookup, no
// validation at apply time, so a retraction delivered BEFORE the arrival it
// names folds inert and the recompute reaches the same state once the
// arrival lands (F4 §3.6's dangling tolerance; mutant m14b is apply-time
// validation dropping it).
func applyArrivalRefRetracted(tx *sql.Tx, env store.Envelope) error {
	var p ArrivalRefRetraction
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return fmt.Errorf("domain: ref-retraction fold: %w", err)
	}
	if p.Type != evtArrivalRefRetracted {
		return nil
	}
	if _, err := tx.Exec(
		`INSERT INTO arrival_ref_retraction (site_id, node_epoch, seq, arrival_id, recorded_by)
		 VALUES (?, ?, ?, ?, ?)`,
		env.SiteID, env.NodeEpoch, env.Seq, p.ArrivalID, p.RecordedBy,
	); err != nil {
		return fmt.Errorf("domain: ref-retraction fold: insert %v: %w", env.Key(), err)
	}
	return nil
}

// --- the raw set computeStates loads ------------------------------------

// retractedArrivalIDs is `computeStates`' fourth raw input: the set of
// arrival ids whose explicit reference has been withdrawn.
//
// It loads the WHOLE table, exactly as the cancel set does and for the same
// reason: retractions are rare human corrections and the table is orders of
// magnitude smaller than the trip-family tables. Loading it whole also keeps
// the scoped and wholesale recomputes reading the SAME set, so the only thing
// the scope decides is which derived ROWS are rewritten — which is what
// `recomputeScope`'s soundness argument is about, and what mutant m25 attacks.
func retractedArrivalIDs(tx *sql.Tx) (map[string]bool, error) {
	out := map[string]bool{}
	rows, err := tx.Query(`SELECT arrival_id FROM arrival_ref_retraction`)
	if err != nil {
		return nil, fmt.Errorf("domain: recompute: retractions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id sql.NullString
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("domain: recompute: scan retraction: %w", err)
		}
		// A row naming no arrival withdraws no reference. The column is
		// nullable because `recorded_by` is (the schema keeps both data
		// columns uniform); the fold always writes the payload's string.
		if id.Valid && id.String != "" {
			out[id.String] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("domain: recompute: retractions: %w", err)
	}
	return out, nil
}

// addRetractionToScope is the F28 scope entry for one retraction, and it is
// THREE HOPS (F4 §3.6, W9R2-A-8 / W9R2-HR-6):
//
//  1. point-scope the named arrival — its own `arrival_state` row changes;
//  2. its own `(origin_claim, vehicle)` corrKey WHERE IT HAS ONE — an
//     explicit arrival may still carry both claims, and the retraction can
//     move rows under that key;
//  3. the referenced trip, via `addTripToScope` — the trip whose binding is
//     released, plus every candidate under its correlation key, which is how
//     Pass 2 gets the chance to re-bind it inside the same recompute.
//
// A retraction whose arrival is not here yet scopes NOTHING — the dangling
// case. That is correct rather than lossy: the arrival's own later delivery
// carries its own scope entry, and the recompute then sees both rows.
//
// ⚠ Mutant m25 is this function minus hop 2. Cloning the `trip_cancelled`
// case literally gives hop 3 alone; today the retracted arrival happens to be
// reached anyway because the scoped loader separately pulls every explicit
// arrival referencing a working-set trip — the chain would work by accident
// of another rule, so it is written down instead.
func addRetractionToScope(tx *sql.Tx, sc *recomputeScope, arrivalID string) error {
	if arrivalID == "" {
		return nil
	}
	sc.arrivals[arrivalID] = true

	var tripRef, vehicle, origin sql.NullString
	err := tx.QueryRow(
		`SELECT trip_ref, vehicle_entered, origin_site FROM trip_arrival WHERE arrival_id = ?`,
		arrivalID).Scan(&tripRef, &vehicle, &origin)
	if err == sql.ErrNoRows {
		return nil // dangling: the arrival is not here yet
	}
	if err != nil {
		return fmt.Errorf("domain: trip_state finalize: scope retraction of %s: %w", arrivalID, err)
	}
	if vehicle.Valid && origin.Valid {
		sc.keys[corrKey{origin: origin.String, vehicleNorm: NormalizeVehicleID(vehicle.String)}] = true
	}
	if tripRef.Valid {
		return addTripToScope(tx, sc, tripRef.String)
	}
	// James, 2026-09-30: unref undoes a resolution — for an unsolicited
	// arrival the trip released is the one its resolution(s) named, which
	// the arrival row does not carry. Every resolution's trip is scoped
	// (over-inclusion is the safe direction).
	trips, err := tx.Query(`SELECT trip_id FROM arrival_resolution WHERE arrival_id = ? AND trip_id IS NOT NULL`, arrivalID)
	if err != nil {
		return fmt.Errorf("domain: trip_state finalize: scope retraction of %s: %w", arrivalID, err)
	}
	var ids []string
	for trips.Next() {
		var id string
		if err := trips.Scan(&id); err != nil {
			trips.Close()
			return fmt.Errorf("domain: trip_state finalize: scope retraction of %s: %w", arrivalID, err)
		}
		ids = append(ids, id)
	}
	trips.Close()
	if err := trips.Err(); err != nil {
		return fmt.Errorf("domain: trip_state finalize: scope retraction of %s: %w", arrivalID, err)
	}
	for _, id := range ids {
		if err := addTripToScope(tx, sc, id); err != nil {
			return err
		}
	}
	return nil
}

// --- the fourth review-lane scan (F4 §3.6's THREE-PART BUILD, part ii) ---

// RefRetractedRow is one `ArrivalRefRetracted` review-lane row: the arrival
// whose reference was withdrawn, the trip it repudiated, and the post that
// withdrew it — the three things F4 §3.6's disposition text names ("checked
// in against ⟨trip⟩ — reference withdrawn by ⟨post⟩; the people are here;
// resolve through review").
//
// It is a row type of its own rather than a bare `ArrivalState` because the
// retracting post lives in `arrival_ref_retraction.recorded_by`, which no
// `arrival_state` column carries.
type RefRetractedRow struct {
	ArrivalID string
	// TripID is the repudiated trip. Empty only if the arrival's own row is
	// missing, which the fold cannot produce.
	TripID string
	// RetractedBy is the retracting post's display name (D-5 attribution),
	// empty when the wire event carried none.
	RetractedBy string
}

// loadRefRetractedLane is the FOURTH filtered scan (`LoadReviewLane`'s other
// three are hardcoded `store.Eq` literals in the same shape), joined to the
// retraction table's attribution.
//
// Two reads, never one per row: the lane scan, plus the retraction table
// whole (it is human-authored and tiny — the same reasoning that loads the
// cancel set whole). Where two posts independently retracted the same
// arrival, the FIRST in canonical (site_id, node_epoch, seq) order is the
// attribution rendered, which is the projection's own OrderBy and therefore
// identical at every post.
func loadRefRetractedLane(st *store.Store) ([]RefRetractedRow, error) {
	states, err := dumpSectionWhere[ArrivalState](st, "arrival_state",
		store.Eq{Column: "disposition", Value: ArrivalRefRetracted})
	if err != nil {
		return nil, err
	}
	if len(states) == 0 {
		return nil, nil
	}
	retractions, err := dumpSection[ArrivalRefRetractionRow](st, "arrival_ref_retraction")
	if err != nil {
		return nil, err
	}
	by := map[string]string{}
	for _, r := range retractions {
		if r.ArrivalID == nil {
			continue
		}
		if _, seen := by[*r.ArrivalID]; seen {
			continue // first in canonical order wins
		}
		if r.RecordedBy != nil {
			by[*r.ArrivalID] = *r.RecordedBy
		} else {
			by[*r.ArrivalID] = ""
		}
	}
	out := make([]RefRetractedRow, 0, len(states))
	for _, s := range states {
		row := RefRetractedRow{ArrivalID: s.ArrivalID, RetractedBy: by[s.ArrivalID]}
		if s.TripID != nil {
			row.TripID = *s.TripID
		}
		out = append(out, row)
	}
	return out, nil
}

// RefRetractionsOf lists every retraction naming one arrival, in canonical
// order. Empty means the arrival's reference stands.
func RefRetractionsOf(st *store.Store, arrivalID string) ([]ArrivalRefRetractionRow, error) {
	return dumpSectionWhere[ArrivalRefRetractionRow](st, "arrival_ref_retraction",
		store.Eq{Column: "arrival_id", Value: arrivalID})
}

// RefRetractionAt resolves the retraction recorded at one log key — the PK
// of this table IS the envelope's own key, so this is a point lookup. It is
// how a confirmation surface gets from "?recorded=site.epoch.seq" back to
// the arrival, without the POST having to carry anything forward.
func RefRetractionAt(st *store.Store, k store.Key) (*ArrivalRefRetractionRow, error) {
	rows, err := dumpSectionWhere[ArrivalRefRetractionRow](st, "arrival_ref_retraction",
		store.Eq{Column: "site_id", Value: k.SiteID},
		store.Eq{Column: "node_epoch", Value: k.NodeEpoch},
		store.Eq{Column: "seq", Value: k.Seq})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// ArrivalStateByID resolves one arrival's derived disposition row.
func ArrivalStateByID(st *store.Store, arrivalID string) (*ArrivalState, error) {
	return oneRow[ArrivalState](st, "arrival_state", store.Eq{Column: "arrival_id", Value: arrivalID})
}

// ⚠ THE RAW ARRIVAL POINT READ IS SPELT INLINE, NOT THROUGH A NAMED HELPER.
// `package domain` has carried an `ArrivalByID` and now carries W9a's
// unexported `rawArrivalByID`; W9b's dependency line is "W9b → W9a: NONE for
// the build", so this file neither declares a colliding name nor calls a
// symbol another sub-chunk owns and has already renamed once mid-build. It
// is one generic point lookup either way.
//
// It is deliberately the RAW row: §9.1 keeps correlation RAW always, so the
// confirmation names the vehicle and origin the FOLD actually matched on. An
// EFFECTIVE read here would put a display on the screen that disagrees with
// the derived value it is explaining — W9a-7's mutant, on a new surface.
func unrefRawArrival(st *store.Store, arrivalID string) (*Arrival, error) {
	return oneRow[Arrival](st, "trip_arrival", store.Eq{Column: "arrival_id", Value: arrivalID})
}

// --- the post-recompute READ the confirmation renders (F4 §3.6) ----------

// BinderKind names HOW a trip that stayed closed is still bound. [[D-58]]:
// "binds" is `boundTo`, and `boundTo` has two fillers.
type BinderKind int

const (
	// BinderNone: nothing binds the trip.
	BinderNone BinderKind = iota
	// BinderReference: a surviving EXPLICIT reference — an officer at some
	// post checked a crew in against this trip.
	BinderReference
	// BinderCorrelation: Pass 2's auto-correlation, which carries NO
	// reference and NO human selection. The copy must say so: the officer
	// has said "this trip did not arrive" and the fold has answered "yes it
	// did" with nobody's judgement behind it ([[D-58]] cost 3).
	BinderCorrelation
)

// UnrefOutcome is the recomputed world one un-reference left behind — the
// rows F4 §3.6 requires the confirmation to READ rather than predict.
//
// Every field is derived at call time from `trip_state`, `arrival_state` and
// the raw arrival rows. Nothing here is cached at append time, which is what
// makes the crossing case resolve itself: the peer's arrival may land after
// the POST, and the next render simply reports what is true then (mutant m20
// is the string composed before the append).
type UnrefOutcome struct {
	// ArrivalID is the arrival whose reference was withdrawn.
	ArrivalID string
	// TripID is the REPUDIATED trip, read off the retracted arrival's own
	// recomputed `arrival_state.trip_id`.
	TripID string
	// TripState is the repudiated trip's recomputed state word, verbatim.
	TripState string
	// BoundTo is `trip_state.arrival_id` AFTER the recompute — empty when
	// nothing binds the trip. It is the quantity [[D-58]] rules on, never
	// "does another arrival reference it".
	BoundTo string

	// WasResolution — James, 2026-09-30: unref undoes a resolution — the
	// withdrawn selection was a review-lane RESOLUTION of an unsolicited
	// arrival, not an explicit reference; TripID is the trip it had bound
	// (read off the resolution row, since the arrival's recomputed row no
	// longer names it), and Disposition / Candidates are what the arrival
	// now reads after falling back to Pass 2 — `ambiguous` over Candidates
	// when it is parked again for the officer to resolve correctly.
	WasResolution bool
	Disposition   string
	Candidates    string
	// WasNone (R3-RES-3): the withdrawn resolution was "None of these" —
	// it had bound no trip, so TripID is empty and nothing reopens; the
	// arrival simply falls back to Pass 2. Rendered as its own outcome,
	// never as "pending" — the record is here, and it is the post's own.
	WasNone bool

	Binder        BinderKind
	BinderPost    string // BinderReference: the post that recorded the binding arrival
	BinderVehicle string // BinderCorrelation: the RAW vehicle the fold matched on
	BinderOrigin  string // BinderCorrelation: the RAW origin claim the fold matched on
}

// Reopened reports whether the repudiated trip came back to `open`.
func (o UnrefOutcome) Reopened() bool { return o.TripState == TripStateOpen }

// StillBound reports whether something OTHER than the retracted arrival
// still holds the trip closed — the [[D-58]] case.
func (o UnrefOutcome) StillBound() bool {
	return o.BoundTo != "" && o.BoundTo != o.ArrivalID
}

// LoadUnrefOutcome reads the world one retraction left behind. It returns
// nil when the arrival has no recomputed state row here (a retraction
// delivered before its arrival — the dangling case), so the surface can say
// so rather than render a sentence about nothing.
func LoadUnrefOutcome(st *store.Store, arrivalID string) (*UnrefOutcome, error) {
	as, err := ArrivalStateByID(st, arrivalID)
	if err != nil {
		return nil, err
	}
	if as == nil {
		return nil, nil
	}
	out := &UnrefOutcome{ArrivalID: arrivalID, Disposition: as.Disposition}
	if as.Candidates != nil {
		out.Candidates = *as.Candidates
	}
	if as.TripID != nil {
		out.TripID = *as.TripID
	} else {
		// A withdrawn RESOLUTION: the arrival's row no longer names the
		// trip; the resolution row does.
		last, err := LatestResolutionOf(st, arrivalID)
		if err != nil {
			return nil, err
		}
		if last == nil {
			return nil, nil
		}
		out.WasResolution = true
		if last.TripID == nil || *last.TripID == "" {
			// R3-RES-3: a withdrawn "None of these" — an outcome in its own
			// right, with no trip to report on.
			out.WasNone = true
			return out, nil
		}
		out.TripID = *last.TripID
	}
	ts, err := TripStateByID(st, out.TripID)
	if err != nil {
		return nil, err
	}
	if ts == nil {
		// The trip itself has not been delivered here. The retraction is
		// recorded and correct; there is simply no state to report yet.
		return out, nil
	}
	out.TripState = ts.State
	if ts.ArrivalID != nil {
		out.BoundTo = *ts.ArrivalID
	}
	if !out.StillBound() {
		return out, nil
	}
	// WHICH filler of boundTo — read off the binder's own recomputed
	// disposition, never guessed from whether some row carries a trip_ref.
	bs, err := ArrivalStateByID(st, out.BoundTo)
	if err != nil {
		return nil, err
	}
	binder, err := unrefRawArrival(st, out.BoundTo)
	if err != nil {
		return nil, err
	}
	switch {
	case bs != nil && bs.Disposition == ArrivalCorrelated:
		out.Binder = BinderCorrelation
		if binder != nil {
			if binder.VehicleEntered != nil {
				out.BinderVehicle = *binder.VehicleEntered
			}
			if binder.OriginSite != nil {
				out.BinderOrigin = *binder.OriginSite
			}
		}
	case bs != nil && (bs.Disposition == ArrivalExpected || bs.Disposition == ArrivalResolved):
		// ⟦Option 1⟧ a review-lane resolution is a human selection too — the
		// officer at the arrival's own post chose the trip (D-87 gating).
		out.Binder = BinderReference
		if binder != nil && binder.RecordedBy != nil {
			out.BinderPost = *binder.RecordedBy
		}
	}
	return out, nil
}

// --- the authoring edge (F4 §3.6) ---------------------------------------

// UnrefRefusalCode names WHICH of F4 §3.6's three refusals fired. It is a
// typed code rather than a message so the officer-facing surface composes its
// own copy: a refusal rendered as a raw `err.Error()` puts a Go package
// prefix and an internal identifier in front of the person who has to act on
// it, and this house has 24 such sites already.
type UnrefRefusalCode int

const (
	// UnrefArrivalUnknown: this post has never heard of that arrival.
	UnrefArrivalUnknown UnrefRefusalCode = iota + 1
	// UnrefNoReference: the arrival carries no `TripRef` — its binding, if
	// any, is Pass 2's and is fold-derived. §5 rule 4 forbids touching it;
	// the officer's path is the review lane.
	UnrefNoReference
	// UnrefAlreadyRetracted: the reference has already been withdrawn. A
	// second retraction on the wire is not harmless — it is a second row in
	// a dump-participating INSERT-only table.
	UnrefAlreadyRetracted
)

// UnrefRefusal is one pre-append refusal. NOTHING is authored on any of them
// (mutant m15b is the refusal that appends and then renders the refusal).
type UnrefRefusal struct {
	Code      UnrefRefusalCode
	ArrivalID string
}

func (e *UnrefRefusal) Error() string {
	switch e.Code {
	case UnrefArrivalUnknown:
		return fmt.Sprintf("domain: unref: arrival %s is not known at this site", e.ArrivalID)
	case UnrefNoReference:
		return fmt.Sprintf("domain: unref: arrival %s carries no trip reference — its binding is fold-derived (§5 rule 4); resolve it through the review lane", e.ArrivalID)
	case UnrefAlreadyRetracted:
		return fmt.Sprintf("domain: unref: arrival %s's reference has already been retracted", e.ArrivalID)
	}
	return fmt.Sprintf("domain: unref: arrival %s refused", e.ArrivalID)
}

// RetractArrivalRef appends one `arrival_ref_retracted` (F4 §3.6). Only
// the RECORDING post — the destination, whose own trip_arrival row this
// modifies — may withdraw; an origin-authored retraction is refused at the
// D-87 choke point (ownership.go, ownPresence on trip_arrival.site_id).
// ⟦was: "from EITHER post", F4 §3.6's pre-D-87 allowance⟧ When the mistake
// is discovered at the origin ("our truck is still out"), the origin radios
// the destination, which withdraws its own record.
//
// Every refusal is PRE-APPEND and typed. The returned envelope is the
// recorded key the confirmation surface reads back at render time.
func RetractArrivalRef(st *store.Store, siteID string, nodeEpoch int64, arrivalID string, recordedBy *string, occurredAtMS int64, timeSource string) (store.Envelope, error) {
	if arrivalID == "" {
		return store.Envelope{}, &UnrefRefusal{Code: UnrefArrivalUnknown}
	}
	arrival, err := unrefRawArrival(st, arrivalID)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: unref: %w", err)
	}
	if arrival == nil {
		return store.Envelope{}, &UnrefRefusal{Code: UnrefArrivalUnknown, ArrivalID: arrivalID}
	}
	if arrival.TripRef == nil {
		// James, 2026-09-30: unref undoes a resolution. An unsolicited
		// arrival carries no reference to withdraw — UNLESS an officer
		// resolved it on the review lane and that resolution STANDS: then
		// this same act withdraws the resolution (arrival_resolve.go,
		// standingResolutionOf), and the fold parks the arrival again with
		// its candidates recomputed. With nothing standing the refusal is
		// exactly what it always was.
		standing, err := StandingResolution(st, arrivalID)
		if err != nil {
			return store.Envelope{}, fmt.Errorf("domain: unref: %w", err)
		}
		if standing == nil {
			return store.Envelope{}, &UnrefRefusal{Code: UnrefNoReference, ArrivalID: arrivalID}
		}
	} else {
		prior, err := RefRetractionsOf(st, arrivalID)
		if err != nil {
			return store.Envelope{}, fmt.Errorf("domain: unref: %w", err)
		}
		if len(prior) > 0 {
			return store.Envelope{}, &UnrefRefusal{Code: UnrefAlreadyRetracted, ArrivalID: arrivalID}
		}
	}
	payload, err := json.Marshal(ArrivalRefRetraction{
		Type: evtArrivalRefRetracted, ArrivalID: arrivalID, RecordedBy: recordedBy,
	})
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: unref: %w", err)
	}
	env, err := st.Author(siteID, nodeEpoch, payload, occurredAtMS, timeSource)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: unref: %w", err)
	}
	return env, nil
}
