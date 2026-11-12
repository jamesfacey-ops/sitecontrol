// arrival_resolve.go — officer resolution of a PARKED arrival.
// James, 2026-09-30: officer resolution of parked arrivals (Option 1).
//
// An unsolicited arrival that matched more than one open trip is parked in
// the ambiguous review lane and binds nothing (§9.2, I-2). Since C-1
// (fa904c2 / option b) that row never resolves itself by elimination, so a
// human has to be able to say what the fold would not guess: "this was
// trip X", or "none of these — the people are here as a plain visit". That
// is the replicated event `arrival_resolved`, its STRICT INSERT-only side
// table `arrival_resolution`, the FIFTH raw input computeStates loads
// beside trips, arrivals, cancels and retractions, and the authoring edge
// with its pre-append refusals.
//
// It is F4 §3.6's "fresh recorded human selection" made on the review row
// instead of the board: the board's "Arrived" path re-enters the occupants
// (they are already on site through the unsolicited visits) and, being an
// explicit arrival, it could only ever close a trip — it cannot say "none
// of these". The visit table is never touched (I-1): the people ARE here;
// what was undecided was the trip.
//
// ⚠ IT IS A RAW INPUT TO computeStates, exactly as the retraction is: both
// callers load the whole table (arrivalResolutions) and pass it in, Pass 1
// honours a resolution as a human selection in canonical order with the
// explicit references (first binder wins a doubly-bound trip), and Pass 2
// skips a resolved arrival. computeStates stays a pure function of raw
// tables, so every post and every replay order agrees.
//
// ⚠ THE FOLD NEVER REFUSES. A resolution delivered before its arrival folds
// inert (the recompute reaches the same state once the arrival lands); a
// peer's resolution naming a trip that is not a candidate binds it just as
// an explicit reference to any trip would. Every refusal is at the
// authoring edge, PRE-APPEND, typed, and authors nothing.
//
// ⚠ A STANDING resolution refuses a second (ResolveAlreadyResolved). James,
// 2026-09-30: unref undoes a resolution — `POST /arrivals/unref` on a
// resolved arrival appends the ordinary `arrival_ref_retracted`, and the
// fold then reads the resolution as WITHDRAWN (standingResolutionOf): the
// trip reverts to whatever the surviving raw facts say, and the arrival
// falls back to Pass 2 — parked again with its candidates recomputed — so
// an officer can resolve it again, correctly. Last mark in the arrival's
// own stream wins; insert-only both ways; never a deletion.
//
// D-87 / D-89: a resolution modifies the arrival's binding — the
// destination's own presence row — so it is GATED at the store's choke
// point like `arrival_ref_retracted` (ownership.go): only the post that
// recorded the arrival may resolve it. Another post radios that post.
package domain

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"sitecontrol/internal/store"
)

// --- the wire ------------------------------------------------------------

const evtArrivalResolved = "arrival_resolved"

// ArrivalResolution is the wire payload: EXACTLY four fields. TripID nil is
// "none of these" — the arrival is a plain visit.
type ArrivalResolution struct {
	Type       string  `json:"type"` // "arrival_resolved"
	ArrivalID  string  `json:"arrival_id"`
	TripID     *string `json:"trip_id"`
	RecordedBy *string `json:"recorded_by,omitempty"` // post attribution (D-5)
}

// ArrivalResolutionRow is the projection row, tags = column names.
type ArrivalResolutionRow struct {
	SiteID     string  `json:"site_id"`
	NodeEpoch  int64   `json:"node_epoch"`
	Seq        int64   `json:"seq"`
	ArrivalID  *string `json:"arrival_id"`
	TripID     *string `json:"trip_id"`
	RecordedBy *string `json:"recorded_by"`
}

// --- the projection ------------------------------------------------------

// arrivalResolutionProjection is the side table: STRICT, INSERT-only by
// trigger, PK the envelope's OWN key, dump-participating. A NEW table, so
// IF NOT EXISTS heals a stored database on next open and no SchemaVersion
// bump is owed — the way arrival_ref_retraction landed. Its name sorts
// before "trip_state", so a batch's resolution row is in the table before
// the recompute reads it (register.go).
func arrivalResolutionProjection() store.Projection {
	return store.Projection{
		Name:    "arrival_resolution",
		Table:   "arrival_resolution",
		OrderBy: "site_id, node_epoch, seq",
		Schema: `CREATE TABLE IF NOT EXISTS arrival_resolution (
			site_id     TEXT    NOT NULL,
			node_epoch  INTEGER NOT NULL,
			seq         INTEGER NOT NULL,
			arrival_id  TEXT,
			trip_id     TEXT,
			recorded_by TEXT,
			PRIMARY KEY (site_id, node_epoch, seq)
		) STRICT;
		CREATE INDEX IF NOT EXISTS idx_arrival_resolution_arrival ON arrival_resolution (arrival_id);
		CREATE TRIGGER IF NOT EXISTS arrival_resolution_insert_only_update
			BEFORE UPDATE ON arrival_resolution BEGIN
			SELECT RAISE(ABORT, 'arrival_resolution is INSERT-only: a resolution is a recorded human selection, never rewritten in place');
		END;
		CREATE TRIGGER IF NOT EXISTS arrival_resolution_insert_only_delete
			BEFORE DELETE ON arrival_resolution BEGIN
			SELECT RAISE(ABORT, 'arrival_resolution is INSERT-only: a deletable resolution row is a silent re-opening of a trip nobody re-recorded (D-1)');
		END;`,
		Apply: applyArrivalResolved,
	}
}

// applyArrivalResolved is the fold: a BARE INSERT, no lookup, no validation
// — a resolution delivered before the arrival it names folds inert and the
// recompute reaches the same state once the arrival lands.
func applyArrivalResolved(tx *sql.Tx, env store.Envelope) error {
	var p ArrivalResolution
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return fmt.Errorf("domain: resolution fold: %w", err)
	}
	if p.Type != evtArrivalResolved {
		return nil
	}
	if _, err := tx.Exec(
		`INSERT INTO arrival_resolution (site_id, node_epoch, seq, arrival_id, trip_id, recorded_by)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		env.SiteID, env.NodeEpoch, env.Seq, p.ArrivalID, p.TripID, p.RecordedBy,
	); err != nil {
		return fmt.Errorf("domain: resolution fold: insert %v: %w", env.Key(), err)
	}
	return nil
}

// --- the raw set computeStates loads ------------------------------------

// arrivalResolution is the in-memory form computeStates consumes: the trip
// an officer selected for an arrival, "" for "none of these".
type arrivalResolution struct {
	tripID string
}

// resolutionMark is one resolution or retraction of an arrival with its
// canonical key — the two interleave to decide what stands.
type resolutionMark struct {
	site       string
	epoch, seq int64
	tripID     string
	retraction bool
}

func markBefore(x, y resolutionMark) bool {
	if x.site != y.site {
		return x.site < y.site
	}
	if x.epoch != y.epoch {
		return x.epoch < y.epoch
	}
	return x.seq < y.seq
}

// standingResolutionOf is THE rule, shared by the fold loader and the read
// helpers: an arrival's STANDING resolution is its LAST resolution in
// canonical order, unless a retraction of that arrival comes later in
// canonical order — James, 2026-09-30: unref undoes a resolution. Every
// mark on one arrival is authored at the arrival's own post (D-87 gates
// both acts), so canonical order IS authoring order and the answer is the
// same at every post and under every delivery order. Returns ok=false when
// nothing stands.
func standingResolutionOf(marks []resolutionMark) (tripID string, ok bool) {
	var lastRes, lastRet *resolutionMark
	for i := range marks {
		m := &marks[i]
		if m.retraction {
			if lastRet == nil || markBefore(*lastRet, *m) {
				lastRet = m
			}
		} else if lastRes == nil || markBefore(*lastRes, *m) {
			lastRes = m
		}
	}
	if lastRes == nil {
		return "", false
	}
	if lastRet != nil && markBefore(*lastRes, *lastRet) {
		return "", false
	}
	return lastRes.tripID, true
}

// arrivalResolutions is computeStates' FIFTH raw input: every arrival with
// a STANDING resolution and the trip chosen for it. Loaded WHOLE — the
// resolution table and the retraction table, both human corrections and
// tiny — so the scoped and wholesale recomputes read the same set.
func arrivalResolutions(tx *sql.Tx) (map[string]arrivalResolution, error) {
	marks := map[string][]resolutionMark{}
	rows, err := tx.Query(`SELECT site_id, node_epoch, seq, arrival_id, trip_id FROM arrival_resolution`)
	if err != nil {
		return nil, fmt.Errorf("domain: recompute: resolutions: %w", err)
	}
	for rows.Next() {
		var m resolutionMark
		var id, trip sql.NullString
		if err := rows.Scan(&m.site, &m.epoch, &m.seq, &id, &trip); err != nil {
			rows.Close()
			return nil, fmt.Errorf("domain: recompute: scan resolution: %w", err)
		}
		if id.Valid && id.String != "" {
			m.tripID = trip.String
			marks[id.String] = append(marks[id.String], m)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("domain: recompute: resolutions: %w", err)
	}
	rows, err = tx.Query(`SELECT site_id, node_epoch, seq, arrival_id FROM arrival_ref_retraction`)
	if err != nil {
		return nil, fmt.Errorf("domain: recompute: retractions: %w", err)
	}
	for rows.Next() {
		var m resolutionMark
		var id sql.NullString
		if err := rows.Scan(&m.site, &m.epoch, &m.seq, &id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("domain: recompute: scan retraction: %w", err)
		}
		if id.Valid && id.String != "" {
			if _, resolvedEver := marks[id.String]; resolvedEver {
				m.retraction = true
				marks[id.String] = append(marks[id.String], m)
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("domain: recompute: retractions: %w", err)
	}
	out := map[string]arrivalResolution{}
	for id, ms := range marks {
		if trip, ok := standingResolutionOf(ms); ok {
			out[id] = arrivalResolution{tripID: trip}
		}
	}
	return out, nil
}

// StandingResolution is the read-side twin of arrivalResolutions for one
// arrival: the resolution that stands, or nil when none does (never
// resolved, or the last resolution was withdrawn through /arrivals/unref).
func StandingResolution(st *store.Store, arrivalID string) (*ArrivalResolutionRow, error) {
	res, err := ResolutionsOf(st, arrivalID)
	if err != nil || len(res) == 0 {
		return nil, err
	}
	ret, err := RefRetractionsOf(st, arrivalID)
	if err != nil {
		return nil, err
	}
	var marks []resolutionMark
	byKey := map[resolutionMark]int{}
	for i, r := range res {
		m := resolutionMark{site: r.SiteID, epoch: r.NodeEpoch, seq: r.Seq}
		if r.TripID != nil {
			m.tripID = *r.TripID
		}
		byKey[resolutionMark{site: r.SiteID, epoch: r.NodeEpoch, seq: r.Seq}] = i
		marks = append(marks, m)
	}
	for _, r := range ret {
		marks = append(marks, resolutionMark{site: r.SiteID, epoch: r.NodeEpoch, seq: r.Seq, retraction: true})
	}
	if _, ok := standingResolutionOf(marks); !ok {
		return nil, nil
	}
	// The standing one is the last resolution in canonical order.
	last := marks[0]
	for _, m := range marks {
		if !m.retraction && markBefore(last, m) {
			last = m
		}
	}
	row := res[byKey[resolutionMark{site: last.site, epoch: last.epoch, seq: last.seq}]]
	return &row, nil
}

// LatestResolutionOf is the last resolution ever recorded for an arrival,
// standing or withdrawn — what an unref confirmation names as the trip the
// withdrawn resolution had bound. nil when never resolved.
func LatestResolutionOf(st *store.Store, arrivalID string) (*ArrivalResolutionRow, error) {
	res, err := ResolutionsOf(st, arrivalID)
	if err != nil || len(res) == 0 {
		return nil, err
	}
	last := 0
	for i := range res {
		if markBefore(resolutionMark{site: res[last].SiteID, epoch: res[last].NodeEpoch, seq: res[last].Seq},
			resolutionMark{site: res[i].SiteID, epoch: res[i].NodeEpoch, seq: res[i].Seq}) {
			last = i
		}
	}
	return &res[last], nil
}

// addResolutionToScope is the F28 scope entry for one resolution — the same
// three hops as a retraction (arrival_unref.go): the named arrival's own
// derived row, its own correlation keys (every other candidate under them
// may change once this arrival stops being ambiguous), and the chosen trip
// via addTripToScope. A resolution whose arrival is not here yet scopes
// nothing — the dangling case; the arrival's own later delivery carries its
// own scope entry.
func addResolutionToScope(tx *sql.Tx, sc *recomputeScope, arrivalID, tripID string) error {
	if arrivalID == "" {
		return nil
	}
	sc.arrivals[arrivalID] = true
	var vehicleID, vehicle, origin sql.NullString
	err := tx.QueryRow(
		`SELECT vehicle_id, vehicle_entered, origin_site FROM trip_arrival WHERE arrival_id = ?`,
		arrivalID).Scan(&vehicleID, &vehicle, &origin)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("domain: trip_state finalize: scope resolution of %s: %w", arrivalID, err)
	}
	if origin.Valid {
		sc.addValueKeys(origin.String, vehicleID.String, NormalizeVehicleID(vehicle.String))
	}
	if tripID != "" {
		return addTripToScope(tx, sc, tripID)
	}
	return nil
}

// --- reads ---------------------------------------------------------------

// ResolutionsOf lists every resolution naming one arrival, in canonical
// order. Empty means the arrival is unresolved.
func ResolutionsOf(st *store.Store, arrivalID string) ([]ArrivalResolutionRow, error) {
	return dumpSectionWhere[ArrivalResolutionRow](st, "arrival_resolution",
		store.Eq{Column: "arrival_id", Value: arrivalID})
}

// ResolutionAt resolves the resolution recorded at one log key — the PK IS
// the envelope's key, so a confirmation surface gets from
// "?recorded=site.epoch.seq" back to the arrival without the POST carrying
// anything forward.
func ResolutionAt(st *store.Store, k store.Key) (*ArrivalResolutionRow, error) {
	rows, err := dumpSectionWhere[ArrivalResolutionRow](st, "arrival_resolution",
		store.Eq{Column: "site_id", Value: k.SiteID},
		store.Eq{Column: "node_epoch", Value: k.NodeEpoch},
		store.Eq{Column: "seq", Value: k.Seq})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// ArrivalSiteOf is the RAW site tag of one arrival — the post that recorded
// it — and nothing else: "" when the arrival is unknown here. A point read
// of `trip_arrival` that decodes no amendment rows, for the review lane's
// "may this post resolve it?" question (D-87): the lane already composes
// each arrival once for its claim text, and W9a-16 holds it to exactly
// once.
func ArrivalSiteOf(st *store.Store, arrivalID string) (string, error) {
	a, err := rawArrivalByID(st, arrivalID)
	if err != nil || a == nil {
		return "", err
	}
	return a.SiteID, nil
}

// AmbiguousCandidates splits an ambiguous arrival's candidate list as the
// fold wrote it (comma-joined, key-consultation order). Nil for a row that
// carries none.
func AmbiguousCandidates(as *ArrivalState) []string {
	if as == nil || as.Candidates == nil || *as.Candidates == "" {
		return nil
	}
	return strings.Split(*as.Candidates, ",")
}

// ResolveOutcome is the recomputed world one resolution left behind — READ
// at render time, never predicted at append time (D-58).
type ResolveOutcome struct {
	ArrivalID   string
	Disposition string // the arrival's recomputed disposition, verbatim
	// TripID is the selected trip ("" for "none of these").
	TripID string
	// TripState / BoundTo are the selected trip's recomputed state and
	// binder; BoundTo != ArrivalID means another arrival holds the trip.
	TripState string
	BoundTo   string
}

// LoadResolveOutcome reads the world a resolution left behind. nil when the
// arrival has no recomputed state row here (a resolution delivered before
// its arrival — the dangling case).
func LoadResolveOutcome(st *store.Store, arrivalID string) (*ResolveOutcome, error) {
	as, err := ArrivalStateByID(st, arrivalID)
	if err != nil || as == nil {
		return nil, err
	}
	out := &ResolveOutcome{ArrivalID: arrivalID, Disposition: as.Disposition}
	if as.TripID == nil {
		return out, nil
	}
	out.TripID = *as.TripID
	ts, err := TripStateByID(st, out.TripID)
	if err != nil {
		return nil, err
	}
	if ts != nil {
		out.TripState = ts.State
		if ts.ArrivalID != nil {
			out.BoundTo = *ts.ArrivalID
		}
	}
	return out, nil
}

// --- the authoring edge --------------------------------------------------

// ResolveRefusalCode names WHICH pre-append refusal fired, so the officer-
// facing surface composes its own sentence rather than rendering a Go
// error string.
type ResolveRefusalCode int

const (
	// ResolveArrivalUnknown: this post has never heard of that arrival.
	ResolveArrivalUnknown ResolveRefusalCode = iota + 1
	// ResolveNotParked: the arrival is not in the ambiguous review lane —
	// it carries an explicit reference, or the fold already correlated it,
	// or it is plain. There is nothing for a human to pick.
	ResolveNotParked
	// ResolveAlreadyResolved: a resolution already stands for it.
	ResolveAlreadyResolved
	// ResolveTripNotCandidate: the chosen trip is not one the fold parked
	// this arrival against.
	ResolveTripNotCandidate
	// ResolveTripAlreadyBound: the chosen trip has since been closed by
	// another arrival (an explicit one at some post, or a correlation).
	ResolveTripAlreadyBound
)

// ResolveRefusal is one pre-append refusal. NOTHING is authored on any.
type ResolveRefusal struct {
	Code      ResolveRefusalCode
	ArrivalID string
	TripID    string
	// BoundTo names the arrival that holds the trip (ResolveTripAlreadyBound).
	BoundTo string
}

func (e *ResolveRefusal) Error() string {
	switch e.Code {
	case ResolveArrivalUnknown:
		return fmt.Sprintf("domain: resolve: arrival %s is not known at this site", e.ArrivalID)
	case ResolveNotParked:
		return fmt.Sprintf("domain: resolve: arrival %s is not parked in the ambiguous review lane — nothing to pick", e.ArrivalID)
	case ResolveAlreadyResolved:
		return fmt.Sprintf("domain: resolve: arrival %s has already been resolved", e.ArrivalID)
	case ResolveTripNotCandidate:
		return fmt.Sprintf("domain: resolve: trip %s is not a candidate for arrival %s", e.TripID, e.ArrivalID)
	case ResolveTripAlreadyBound:
		return fmt.Sprintf("domain: resolve: trip %s is already closed by arrival %s", e.TripID, e.BoundTo)
	}
	return fmt.Sprintf("domain: resolve: arrival %s refused", e.ArrivalID)
}

// ResolveParkedArrival appends ONE `arrival_resolved` naming the trip an
// officer chose for a parked arrival — or, with tripID nil, "none of these".
// Every refusal is PRE-APPEND and typed; the store's choke point adds D-87's
// own refusal (another post's arrival) underneath.
func ResolveParkedArrival(st *store.Store, siteID string, nodeEpoch int64, arrivalID string, tripID *string, recordedBy *string, occurredAtMS int64, timeSource string) (store.Envelope, error) {
	if arrivalID == "" {
		return store.Envelope{}, &ResolveRefusal{Code: ResolveArrivalUnknown}
	}
	arrival, err := unrefRawArrival(st, arrivalID)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: resolve: %w", err)
	}
	if arrival == nil {
		return store.Envelope{}, &ResolveRefusal{Code: ResolveArrivalUnknown, ArrivalID: arrivalID}
	}
	// A STANDING resolution refuses a second; one withdrawn through
	// /arrivals/unref does not (James, 2026-09-30: unref undoes a
	// resolution) — the arrival is parked again and may be resolved again.
	standing, err := StandingResolution(st, arrivalID)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: resolve: %w", err)
	}
	if standing != nil {
		return store.Envelope{}, &ResolveRefusal{Code: ResolveAlreadyResolved, ArrivalID: arrivalID}
	}
	as, err := ArrivalStateByID(st, arrivalID)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: resolve: %w", err)
	}
	if as == nil || as.Disposition != ArrivalAmbiguous {
		return store.Envelope{}, &ResolveRefusal{Code: ResolveNotParked, ArrivalID: arrivalID}
	}
	var chosen *string
	if tripID != nil && strings.TrimSpace(*tripID) != "" {
		id := strings.TrimSpace(*tripID)
		candidate := false
		for _, c := range AmbiguousCandidates(as) {
			if c == id {
				candidate = true
			}
		}
		if !candidate {
			return store.Envelope{}, &ResolveRefusal{Code: ResolveTripNotCandidate, ArrivalID: arrivalID, TripID: id}
		}
		ts, err := TripStateByID(st, id)
		if err != nil {
			return store.Envelope{}, fmt.Errorf("domain: resolve: %w", err)
		}
		if ts != nil && ts.ArrivalID != nil {
			return store.Envelope{}, &ResolveRefusal{Code: ResolveTripAlreadyBound, ArrivalID: arrivalID, TripID: id, BoundTo: *ts.ArrivalID}
		}
		chosen = &id
	}
	payload, err := json.Marshal(ArrivalResolution{
		Type: evtArrivalResolved, ArrivalID: arrivalID, TripID: chosen, RecordedBy: recordedBy,
	})
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: resolve: %w", err)
	}
	env, err := st.Author(siteID, nodeEpoch, payload, occurredAtMS, timeSource)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: resolve: %w", err)
	}
	return env, nil
}
