// register.go — the composition-root entry point domain hands to
// cmd/sitectl/main.go (chunk-carry-forward row 6/8; C2 review L1): call
// domain.Register(st) once, after store.Open, before serving traffic. store
// cannot import domain (domain imports store for Envelope/Projection — an
// import cycle the other way), so registration cannot live inside
// store.Open; this is the one place it can live instead.
//
// NOT wired into main.go by this chunk — see the build report's "MAIN.GO
// WIRING" section for the exact one-line call site. Only C5's own
// projections (company/trade/person) are registered here; C6
// extends this same function with the real trip/visit folds when it lands
// (Open no longer self-registers dump-only trip/visit stubs as of this
// chunk — that self-registration was removed per the L1 carry-forward
// instruction).
package domain

import "sitecontrol/internal/store"

// Register wires every domain projection into st. Safe to call exactly once,
// at startup, before any Apply/Author traffic (store.RegisterProjection's own
// contract: registration is not safe concurrently with the fold loop).
func Register(st *store.Store) error {
	for _, p := range []store.Projection{
		companyProjection(),
		tradeProjection(),
		personProjection(),
		// W3 (item 4): the person_amended side table — INSERT-only by
		// trigger, composed at read (person_amend.go).
		personAmendmentProjection(),
		// W4 (item 14): the occurred_at_corrected side table — INSERT-only
		// by trigger, resolved at read (time_correct.go).
		occurredAtCorrectionProjection(),
		// W5d (item 12): the company_amended side table — INSERT-only by
		// trigger, composed at read (company_amend.go). A NEW table, so
		// IF NOT EXISTS heals a stored database on next open and no
		// SchemaVersion bump is owed (W5-X5 names W5d explicitly exempt;
		// W3's person_amendment landed the same way at 99b6674).
		companyAmendmentProjection(),
		// W9a (item 15): F4's amendment family — the trip_amended /
		// arrival_amended side tables, INSERT-only by trigger, composed at
		// read (trip_amend.go). NEW tables, so IF NOT EXISTS heals a stored
		// database on next open and no SchemaVersion bump is owed — the
		// same way person_amendment and company_amendment landed.
		//
		// ⚠ Registration order does not decide fold order: the store sorts
		// projections by NAME. "arrival_amendment" and "trip_amendment"
		// both fold a bare INSERT into their own table and read nothing
		// else, so no ordering relationship with the trip family exists to
		// get wrong.
		tripAmendmentProjection(),
		arrivalAmendmentProjection(),
		// W9b (item 16): F4's un-reference — the arrival_ref_retraction side
		// table, INSERT-only by trigger, a bare dangling-tolerant INSERT
		// fold (arrival_unref.go). A NEW table, so IF NOT EXISTS heals a
		// stored database on next open and no SchemaVersion bump is owed.
		//
		// ⚠ Its rows are a RAW INPUT to the trip_state recompute, so fold
		// order DOES matter here, and the store's name ordering supplies it:
		// "arrival_ref_retraction" sorts before "trip_state", so a batch's
		// retraction row is in the table before the recompute reads it.
		arrivalRefRetractionProjection(),
		// James, 2026-09-30: officer resolution of parked arrivals (Option
		// 1) — the arrival_resolution side table, INSERT-only by trigger, a
		// bare dangling-tolerant INSERT fold (arrival_resolve.go). A NEW
		// table, so IF NOT EXISTS heals a stored database on next open and
		// no SchemaVersion bump is owed. Its rows are the FIFTH raw input to
		// the trip_state recompute; "arrival_resolution" sorts before
		// "trip_state", so a batch's row is in the table before the
		// recompute reads it.
		arrivalResolutionProjection(),
		// W8 (item 10): the trade register and its `trade_amended` side
		// table — INSERT-only by trigger, composed at read (trade_amend.go).
		// Both are NEW tables, so IF NOT EXISTS heals a stored database on
		// next open for THESE TWO; the person and visit COLUMN renames that
		// land with them cannot be healed that way, and W8 does not close
		// the row by bumping either — SchemaVersion stayed 10 (W8-X5), on
		// the same in-window ruling every W5 sub-chunk rode, and the v10 log
		// started empty (D-29). The NEXT window — the D-92/D-88/D-91/D-98
		// increment — bumped 10 → 11, once (store.go's bump log).
		//
		// ⚠ THE REFERENCE-MERGE PROJECTION THAT USED TO SIT HERE IS DELETED,
		// not moved: P-3 §5 schedules the deletion onto W8, D-47 sets the
		// bar, and each entity now composes its own merge state per read.
		// Its registration site and the dev route that drove it were COUPLED
		// — neither could land alone — and both are gone in this change.
		//
		// ⚠ Registration order does not decide fold order: the store sorts
		// projections by NAME. "trade" and "trade_amendment" both fold a bare
		// INSERT into their own table and read nothing else, so no ordering
		// relationship with any other family exists to get wrong.
		tradeAmendmentProjection(),
		// C6: the trip event family (trip.go). Fold execution is projection-
		// NAME-ordered (store/log.go), and the derived-state recompute is
		// registered as "trip_state", which sorts after every raw trip-family
		// fold ("trip", "trip_arrival", "trip_cancel") — the recompute always
		// sees the raw row of the event that triggered it.
		tripProjection(),
		tripOccupantProjection(),
		tripArrivalProjection(),
		// SCHEMA-11 step 5 (D-98/D-91): the not-entered side table — raw,
		// INSERT-only, written by the trip_arrival fold above (trip.go),
		// dump-only here (same shape as trip_occupant just above). A NEW
		// table, so IF NOT EXISTS heals a stored database on next open; no
		// bump is owed (it rides the 10 -> 11 window already open — store.go
		// names this stage explicitly).
		tripArrivalNotEnteredProjection(),
		// James, 2026-10-03: the road-work KIND side table — raw, INSERT-only,
		// written by the trip fold above (trip.go), dump-only here (the
		// trip_occupant shape). A NEW table, so IF NOT EXISTS heals a stored
		// database on next open and no SchemaVersion bump is owed — the same
		// way trip_arrival_not_entered and arrival_resolution landed. Nothing
		// in the trip_state recompute reads it (a road-work trip is
		// destination-less and point-scoped, exactly as a degenerate one is).
		tripRoadWorkProjection(),
		tripCancelProjection(),
		tripStateProjection(),
		arrivalStateProjection(),
		visitProjection(),
		// C8: the gate check-out event (design §8.1: "replicate as
		// ordinary own-site log events"). Its Apply also closes the
		// visit row directly (checkin.go's row-28 guard note) - name
		// ordering relative to trip/trip_state does not matter here,
		// since it never depends on their output.
		visitCheckoutProjection(),
		// ⟦was: weatherPhaseProjection() — weather phases removed, James
		// 2026-10-04. A legacy weather_phase_declared event has no fold and
		// is inert on replay and ingest (see ownership.go).⟧
		// W7 (item 9): the vehicle entity and its `vehicle_amended` side
		// table — INSERT-only by trigger, composed at read (vehicle.go,
		// vehicle_amend.go). Both are NEW tables, so IF NOT EXISTS heals a
		// stored database on next open for THESE TWO; the trip-side column
		// change that lands with them cannot be healed that way, which is
		// why W7 carries its own '9'-edge instance rather than closing the
		// row by ruling (W7-X5).
		//
		// ⚠ Registration order does not decide fold order: the store sorts
		// projections by NAME. "vehicle" and "vehicle_amendment" both fold a
		// bare INSERT into their own table and read nothing else, so no
		// ordering relationship with the trip family exists to get wrong —
		// and the trip fold deliberately does NOT consult either of them
		// (W7-6: the fold never refuses a trip whose vehicle_id it does not
		// hold, and never resolves one either).
		vehicleProjection(),
		vehicleAmendmentProjection(),
		// SCHEMA-11 step 4 (D-88/D-95/D-91): per-site orientation — two RAW
		// INSERT-only tables (one per event) and the DERIVED
		// `orientation(person_id, site_id)` projection recomputed in
		// Finalize (orientation.go). NEW tables, riding the 10 → 11 bump
		// paid at the head of this train (ruling/schema-11 stage 1a) — no
		// bump here.
		//
		// ⚠ Fold order matters and the store's NAME ordering plus the
		// Apply/Finalize split supplies it: the two raw folds are per-event
		// Apply; the derivation is Finalize, which runs after EVERY Apply of
		// the batch, so it always sees the raw row of the event that
		// triggered it.
		orientationCompletionProjection(),
		orientationRetractionProjection(),
		orientationProjection(),
	} {
		if err := st.RegisterProjection(p); err != nil {
			return err
		}
	}
	// D-87 (2026-09-05): the ownership resolvers for the presence subset
	// (ownership.go) — after the projections, because they read projection
	// tables. This is what turns the tag every row already carries into a
	// permission at store.Author, and nowhere else.
	return registerOwnership(st)
}
