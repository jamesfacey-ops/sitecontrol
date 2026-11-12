// trip_amend.go — W9a (item 15): F4's amendment family. The two replicated
// event types `trip_amended` / `arrival_amended`, their STRICT INSERT-only
// side tables `trip_amendment` / `arrival_amendment`, the ascending-key
// per-fact composed read path (`EffectiveTrip` / `EffectiveArrival`), and
// the authoring edge with F4 §3.5's pre-append refusal ladder.
// (design/trip-fact-correction-v1.md rev 4; WINDOW-BUILD-PLAN-v1 §3 W9a,
// criteria RE-LOCKED 2026-09-02.)
//
// This is W3's `person_amended` shape CLONED, not re-invented: same
// INSERT-only side table keyed by the envelope's own key, same ascending
// (site_id, node_epoch, seq) composition, same "the fold NEVER refuses"
// rule, same read-time supersession. What is new here, and only here:
//
//   - THE COMPOUND DESTINATION FACT (§3.4). `(kind, value, deadline)` is
//     ONE fact, not three. Composed per-field they interleave into
//     `uncontrolled + deadline` (the guaranteed-false-alarm pair) or
//     both-set; as one fact every assertion is validated whole at
//     authoring and composition picks one WHOLE assertion.
//   - PER-MEMBER OCCUPANT FACTS (§6). `occupant/<person_id>` is a fact-name
//     PATTERN, not a member of a closed list, so the carrier's vocabulary
//     is DERIVED per row (see carrierRegistration.Patterns in outcome.go).
//     Removing one occupant does not contest with adding another.
//   - THE STRUCTURAL DRIVER UNION (§3.4). The driver is always an occupant.
//     `applyTripDispatched` prepends the driver structurally; composition
//     REPEATS that from the EFFECTIVE driver — see composeOccupants' own
//     comment for the one place this differs from a naive union, and why
//     the criteria pin it that way.
//   - COMPOSITION IS THE GUARANTEE (§3.3a, W9a-17). The fold does not
//     validate against current state (that is what makes it
//     order-independent), so the authoring refusals are NOT the last line:
//     composition IGNORES a `Clear` token outside the whitelist and an
//     occupant id absent from the roster. The raw row stands byte-identical
//     either way — the log is APPEND-ONLY, so the audit chain still shows what was asserted.
//
// ⟦SchemaVersion 11, [[D-92]], step 3⟧ The causal `seen` reference is cloned
// onto BOTH payloads here, fourth and fifth after company, trade and
// vehicle: each gains the field, each side table gains the column, both
// edges stamp it, and every per-fact read (composeTrip's scalars,
// composeOccupants' members, EffectiveArrival's claims, Outcome/Competitors)
// resolves through resolveContest — the ONE rule (person_amend.go, read
// through contest.go), never respelled. The contest scope is unchanged: the
// record's own rows, which F4 already scopes in SQL.
//
// ⚠ THE COMPOSED READERS ARE SCOPED (§3.3, W9a-16(a)). `EffectiveTrip` and
// `EffectiveArrival` are the `PersonByID` shape — only THIS trip's (or
// arrival's) amendment rows, filtered in SQL, in the one ascending-key
// order — never a whole-table decode per call: §9.2 consumer 6 calls them
// in a loop over the season's trips, so an unscoped read is quadratic on
// the muster path, at the cutover, green.
//
// ⚠ THE ORDERING SPELLING IS NOT RESPELLED HERE (§3.3, W9R2-A-6). Both
// composition constants concatenate `personAmendmentCompositionOrderBy`
// (person_amend.go) — the ONE SQL ordering spelling beside the ONE Go
// comparator `store.Key.Less`, whose own doc comment says "No other
// comparator over Key may ever exist". A fourth spelling is the
// two-implementations drift surface VF's T-A oracle refuses.
//
// ⚠ NOTHING IN THIS FILE MAY BE REACHED FROM `computeStates`,
// `recomputeTripStates` OR `recomputeTripStatesScoped` (§9.1: those three
// are RAW, ALWAYS). Feeding effective values into them would silence a live
// alert by record edit — W9a-7's mutants (m7/m12/m12b) — and the §9.3
// inverse scan (W9a-8) reds any `Effective*` identifier or amendment-table
// reference reachable from those three roots.
package domain

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"sitecontrol/internal/store"
)

// --- event payloads (F4 §3.1 — the frozen wire, gate B-2's list) ---------

const (
	evtTripAmended    = "trip_amended"
	evtArrivalAmended = "arrival_amended"
)

// DestinationAmend is the compound destination fact — asserted WHOLE or not
// at all (§3.4). DeadlineMinutes is required iff Kind is controlled and
// refused otherwise; both halves are checked at authoring, and composition
// takes the whole assertion or none of it.
type DestinationAmend struct {
	Kind            string `json:"kind"`
	Value           string `json:"value"`
	DeadlineMinutes *int64 `json:"deadline_minutes,omitempty"`
}

// TripAmended is the wire payload (type "trip_amended"). Every value field
// is a pointer or a slice: NIL/ABSENT MEANS "NOT AMENDED", which is a
// different thing from asserting an empty value. No `recorded_by` and no
// reason/note field exist on either payload (F4 §3.1): attribution is the
// side table's `site_id`, structural and unforgeable (D-5).
//
// ⟦SchemaVersion 11⟧ `seen` is ADDED to both payloads ([[D-92]]): the keys
// of the earlier amendments of the SAME record this author had already seen
// standing for the facts asserted here (resolveContest). Stamped by
// AmendTrip / AmendArrival, never by a caller; omitted when the author saw
// nothing standing. Its spelling is person_amended's SeenRef — no new
// literal for B-2 beyond step 2's `seen`.
type TripAmended struct {
	Type            string            `json:"type"` // "trip_amended"
	TripID          string            `json:"trip_id"`
	DriverPersonID  *string           `json:"driver_person_id,omitempty"`
	VehicleEntered  *string           `json:"vehicle_entered,omitempty"`
	Destination     *DestinationAmend `json:"destination,omitempty"`
	CargoNote       *string           `json:"cargo_note,omitempty"`
	OccupantsAdd    []string          `json:"occupants_add,omitempty"`
	OccupantsRemove []string          `json:"occupants_remove,omitempty"`
	Clear           []string          `json:"clear,omitempty"`
	Seen            []SeenRef         `json:"seen,omitempty"`
}

// ArrivalAmended is the wire payload (type "arrival_amended"): the two
// CLAIMS an unsolicited arrival typed. It carries NO visit field and NO
// trip_ref — the un-reference is a separate event (§3.6, item 16/W9b), and
// this payload structurally cannot re-point an arrival. `seen` as above.
type ArrivalAmended struct {
	Type           string    `json:"type"` // "arrival_amended"
	ArrivalID      string    `json:"arrival_id"`
	VehicleEntered *string   `json:"vehicle_entered,omitempty"`
	OriginSite     *string   `json:"origin_site,omitempty"`
	Seen           []SeenRef `json:"seen,omitempty"`
}

// --- the fact vocabulary (§6) -------------------------------------------

const (
	factTripDriver      = "driver_person_id"
	factTripVehicle     = "vehicle_entered"
	factTripDestination = "destination"
	factTripCargoNote   = "cargo_note"

	factArrivalVehicle = "vehicle_entered"
	factArrivalOrigin  = "origin_site"

	// factOccupantPrefix is the OPEN half of the trip carrier's vocabulary:
	// `occupant/<raw person_id>` (F4 §6). The suffix is DATA, so the list is
	// derived per row and can never be a hand-kept enumeration.
	//
	// ⚠ The id is the RAW one, on the wire and in the fact name ([[D-59]]).
	// Resolving it at compose time would make the fact name UNSTABLE across
	// P-3 §5's un-merge, and `Outcome`'s per-fact supersession is keyed on
	// fact-name equality. Identity is resolved at the READ, by the
	// consumers that render people (§9.2 consumer 6) — never here.
	factOccupantPrefix = "occupant/"
)

// tripAmendableScalarFacts is the CLOSED half of the trip carrier's
// vocabulary, in canonical rendering order. The open half is the
// `occupant/` pattern above.
var tripAmendableScalarFacts = []string{
	factTripDriver, factTripVehicle, factTripDestination,
	factTripCargoNote,
}

// arrivalAmendableFacts is the arrival carrier's vocabulary — closed, and
// exactly the two claims §3.6 leaves correctable.
var arrivalAmendableFacts = []string{factArrivalVehicle, factArrivalOrigin}

// tripClearWhitelist is §3.3a's whitelist, in full: `Clear` is legal for
// exactly this one nullable prose fact. ⟦was: "these two" — the second,
// phase_override_reason, went with weather phases (James, 2026-10-04); a
// legacy `clear:["phase_override_reason"]` is filtered out at composition
// below like any other non-whitelisted name.⟧ Destination, driver and vehicle
// are REFUSED — D-4: a departure must name where it went, and a cleared
// destination composes the crashed-check-in shape D-4 exists to make
// unauthorable.
//
// ⚠ The refusal at authoring is not the guarantee; COMPOSITION is (W9a-17).
// A divergent writer's `clear:["destination"]` folds — the fold never
// validates — so the composed read filters to this set. The raw row is
// preserved verbatim: nothing is deleted and the audit chain still renders
// what was asserted (the log is append-only).
var tripClearWhitelist = []string{factTripCargoNote}

func clearIsWhitelisted(fact string) bool {
	for _, f := range tripClearWhitelist {
		if f == fact {
			return true
		}
	}
	return false
}

// --- the vocabulary, READ as a derivation source (§38) -------------------
//
// ⟦LOCK — W9 post-lock amendment §38, plan owner, 2026-09-03⟧ NEW. §38's
// ruling in terms: "A cargo-shaped column may exist only on a table that
// projects the TRIP FACT VOCABULARY, and the permitted set is DERIVED from
// that vocabulary — tripAmendableScalarFacts / factTripCargoNote — never
// from table names."
//
// The three readers below are what makes that derivation available outside
// this package. `W9-LOCKED-REDS.md` §3.4 names this cost by name — "the
// vocabulary is unexported in internal/domain, so it needs either an
// exported accessor or the check moving packages" — and rules for the
// accessor. Its consumer today is `internal/store`'s W5e whole-schema
// census (`w5e_cargo_note_structural_test.go`), which cannot otherwise
// state a permitted set without hand-keeping table names.
//
// ⚠ THE VOCABULARY ITSELF STAYS UNEXPORTED, and these are READS. It is the
// closed half of a carrier's wire contract (§6): nothing outside this
// package may add to it, and every reader below returns a copy or a scalar
// so no caller can sort, truncate or rewrite the original in place.

// TripFactVocabulary returns the CLOSED half of the trip carrier's fact
// vocabulary (§6), in canonical rendering order. The open half — the
// `occupant/<person_id>` pattern — is deliberately absent: it is a pattern
// whose suffix is data, so it can never be a member of a list.
func TripFactVocabulary() []string {
	return append([]string(nil), tripAmendableScalarFacts...)
}

// TripCargoFact returns the vocabulary's cargo fact — the one fact D-21.3
// confines to the departure side ("Departures record cargo; unsolicited
// arrivals do not. Closed.").
func TripCargoFact() string { return factTripCargoNote }

// TripFactsNotProjectedBy returns the members of the trip carrier's scalar
// vocabulary that `columns` does NOT project, in canonical order. An EMPTY
// result is the property "this column set projects the trip fact
// vocabulary"; a non-empty one names exactly what is missing, so a caller's
// refusal can say why rather than only that.
//
// A column projects a fact when it IS the fact, or when it is one arm of a
// COMPOSITE fact's rendering — `<fact>_<arm>`. `destination` is the only
// composite in the vocabulary today (§3.4: kind, value and deadline are ONE
// fact) and the two tables that carry it render it differently —
// `destination_site` / `destination_uncontrolled` on `trip`,
// `destination_kind` / `destination_value` on `trip_amendment`. That is
// precisely why an arm is matched by the FACT it renders and never by its
// own column name: a third projection may spell its arms a third way and
// still project the same fact.
//
// ⚠ This is a statement about a COLUMN SET, not about a table name. A table
// name is not an argument here and must not become one — §38 measured three
// name-shaped derivations and rejected all three, the most principled-
// looking of which ("carries trip_id and no arrival_id") silently permits
// `visit.cargo_note`.
func TripFactsNotProjectedBy(columns []string) []string {
	var missing []string
	for _, fact := range tripAmendableScalarFacts {
		projected := false
		for _, c := range columns {
			if c == fact || strings.HasPrefix(c, fact+"_") {
				projected = true
				break
			}
		}
		if !projected {
			missing = append(missing, fact)
		}
	}
	return missing
}

// --- the INSERT-only side tables (§3.2) ---------------------------------

// tripAmendmentProjection registers the side table the fold maintains and
// the composition reads. PK = the envelope's OWN key, so one event's
// assertion can never be stored twice and the set of rows is a SET UNION —
// commutative, associative, identical at both posts in any delivery order
// (schema-10 §5A.3). The declared column set is EXACT: the key triple, the
// target, one column per payload field, `cleared`, and — since SchemaVersion
// 11 ([[D-92]]) — `seen` (the author's seen-list, JSON, NULL when empty: a
// reference written at authoring, never a status; the same column joins
// `arrival_amendment` below).
//
// NO STATUS-SHAPED COLUMN MAY EVER JOIN THIS TABLE. Supersession is
// recomputed at read, never stored (VF §4.3 rule 2): a status stamped at
// write goes permanently wrong the moment the deferred winner arrives by
// replication, and a fold-maintained status column is behaviourally correct
// at every read — catchable ONLY structurally, which is what W9a-12 does.
//
// The two triggers make INSERT-only a MECHANISM rather than a convention.
func tripAmendmentProjection() store.Projection {
	return store.Projection{
		Name:    "trip_amendment",
		Table:   "trip_amendment",
		OrderBy: "site_id, node_epoch, seq",
		Schema: `CREATE TABLE IF NOT EXISTS trip_amendment (
			site_id               TEXT NOT NULL,
			node_epoch            INTEGER NOT NULL,
			seq                   INTEGER NOT NULL,
			trip_id               TEXT,
			driver_person_id      TEXT,
			vehicle_entered       TEXT,
			destination_kind      TEXT,
			destination_value     TEXT,
			deadline_minutes      INTEGER,
			cargo_note            TEXT,
			occupants_add         TEXT,
			occupants_remove      TEXT,
			cleared               TEXT,
			seen                  TEXT,
			PRIMARY KEY (site_id, node_epoch, seq)
		) STRICT;
		CREATE TRIGGER IF NOT EXISTS trip_amendment_insert_only_update
			BEFORE UPDATE ON trip_amendment BEGIN
			SELECT RAISE(ABORT, 'trip_amendment is INSERT-only: supersession is computed at read, never rewritten in place');
		END;
		CREATE TRIGGER IF NOT EXISTS trip_amendment_insert_only_delete
			BEFORE DELETE ON trip_amendment BEGIN
			SELECT RAISE(ABORT, 'trip_amendment is INSERT-only: amendments are superseded, never merely absent');
		END;`,
		Apply: applyTripAmended,
	}
}

// arrivalAmendmentProjection is the same shape over the arrival claims.
func arrivalAmendmentProjection() store.Projection {
	return store.Projection{
		Name:    "arrival_amendment",
		Table:   "arrival_amendment",
		OrderBy: "site_id, node_epoch, seq",
		Schema: `CREATE TABLE IF NOT EXISTS arrival_amendment (
			site_id         TEXT NOT NULL,
			node_epoch      INTEGER NOT NULL,
			seq             INTEGER NOT NULL,
			arrival_id      TEXT,
			vehicle_entered TEXT,
			origin_site     TEXT,
			seen            TEXT,
			PRIMARY KEY (site_id, node_epoch, seq)
		) STRICT;
		CREATE TRIGGER IF NOT EXISTS arrival_amendment_insert_only_update
			BEFORE UPDATE ON arrival_amendment BEGIN
			SELECT RAISE(ABORT, 'arrival_amendment is INSERT-only: supersession is computed at read, never rewritten in place');
		END;
		CREATE TRIGGER IF NOT EXISTS arrival_amendment_insert_only_delete
			BEFORE DELETE ON arrival_amendment BEGIN
			SELECT RAISE(ABORT, 'arrival_amendment is INSERT-only: amendments are superseded, never merely absent');
		END;`,
		Apply: applyArrivalAmended,
	}
}

// amendListSep joins a list-valued payload field into its column. The
// column holds the ids as a plain separated list rather than JSON so that a
// single-element list reads back as the bare id — which is what makes the
// RAW WIRE assertion of [[D-59]] readable (`occupants_add` holds `P1`), and
// what lets a divergent writer's hand-written row be honest about what it
// asserted (W9a-17's fixture writes `occupants_add = 'p-ghost'` directly).
const amendListSep = ","

func joinAmendList(in []string) any {
	var kept []string
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			kept = append(kept, s)
		}
	}
	if len(kept) == 0 {
		return nil // absent, never empty-string: nil means NOT AMENDED
	}
	return strings.Join(kept, amendListSep)
}

func splitAmendList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, amendListSep) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// applyTripAmended is the fold. IT NEVER REFUSES and it NEVER VALIDATES
// AGAINST CURRENT STATE (§3.2, W3-2's rule): a dangling amendment for a
// trip not yet delivered folds inert and composes when the trip arrives,
// and refusing a wire-received amendment here would fork the two logs
// forever. It is a BARE INSERT and nothing else — it does not touch `trip`,
// `trip_occupant` or `visit` (I-1), and it does not enter `computeStates`
// or the scoped recompute (I-2).
func applyTripAmended(tx *sql.Tx, env store.Envelope) error {
	raw, typ, ok := amendPayloadFields(env)
	if !ok || typ != evtTripAmended {
		return nil
	}
	dest := amendObject(raw, "destination")
	// `seen` ([[D-92]]) through the one codec, seenColumnValue (contest.go):
	// JSON, not the comma list the id columns use — a reference is a key
	// triple, not an id; absent, empty or malformed lands as NULL.
	seen, err := seenColumnValue(raw)
	if err != nil {
		return fmt.Errorf("domain: trip_amended fold: seen: %w", err)
	}
	_, err = tx.Exec(`INSERT INTO trip_amendment
		(site_id, node_epoch, seq, trip_id, driver_person_id, vehicle_entered,
		 destination_kind, destination_value, deadline_minutes,
		 cargo_note, occupants_add, occupants_remove, cleared, seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		env.SiteID, env.NodeEpoch, env.Seq,
		amendString(raw, "trip_id"),
		amendString(raw, "driver_person_id"),
		amendString(raw, "vehicle_entered"),
		amendString(dest, "kind"),
		amendString(dest, "value"),
		amendInt(dest, "deadline_minutes"),
		amendString(raw, "cargo_note"),
		joinAmendList(amendStrings(raw, "occupants_add")),
		joinAmendList(amendStrings(raw, "occupants_remove")),
		joinAmendList(amendStrings(raw, "clear")),
		seen,
	)
	if err != nil {
		return fmt.Errorf("domain: trip_amended fold: insert %v: %w", env.Key(), err)
	}
	return nil
}

// applyArrivalAmended is the arrival claims' fold — same posture.
func applyArrivalAmended(tx *sql.Tx, env store.Envelope) error {
	raw, typ, ok := amendPayloadFields(env)
	if !ok || typ != evtArrivalAmended {
		return nil
	}
	seen, err := seenColumnValue(raw) // [[D-92]]: the one codec; malformed lands as NULL
	if err != nil {
		return fmt.Errorf("domain: arrival_amended fold: seen: %w", err)
	}
	_, err = tx.Exec(`INSERT INTO arrival_amendment
		(site_id, node_epoch, seq, arrival_id, vehicle_entered, origin_site, seen)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		env.SiteID, env.NodeEpoch, env.Seq,
		amendString(raw, "arrival_id"),
		amendString(raw, "vehicle_entered"),
		amendString(raw, "origin_site"),
		seen,
	)
	if err != nil {
		return fmt.Errorf("domain: arrival_amended fold: insert %v: %w", env.Key(), err)
	}
	return nil
}

// amendPayloadFields decodes the payload's top level. A payload this
// decoder finds odd must still LAND (the fold never refuses), so every
// accessor below is tolerant.
func amendPayloadFields(env store.Envelope) (map[string]json.RawMessage, string, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(env.Payload, &raw); err != nil {
		return nil, "", false // not an object-shaped payload; not this fold's event
	}
	var typ string
	if err := json.Unmarshal(raw["type"], &typ); err != nil {
		return nil, "", false
	}
	return raw, typ, true
}

func amendObject(raw map[string]json.RawMessage, key string) map[string]json.RawMessage {
	r, ok := raw[key]
	if !ok {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(r, &m); err != nil {
		return nil
	}
	return m
}

func amendString(raw map[string]json.RawMessage, key string) any {
	r, ok := raw[key]
	if !ok {
		return nil
	}
	var s string
	if err := json.Unmarshal(r, &s); err == nil {
		return s
	}
	if string(r) == "null" {
		return nil
	}
	return string(r) // tolerant: a non-string value lands as its JSON text
}

func amendInt(raw map[string]json.RawMessage, key string) any {
	r, ok := raw[key]
	if !ok {
		return nil
	}
	var n int64
	if err := json.Unmarshal(r, &n); err == nil {
		return n
	}
	return nil
}

func amendStrings(raw map[string]json.RawMessage, key string) []string {
	r, ok := raw[key]
	if !ok {
		return nil
	}
	var list []string
	if err := json.Unmarshal(r, &list); err == nil {
		return list
	}
	var one string
	if err := json.Unmarshal(r, &one); err == nil && one != "" {
		return []string{one}
	}
	return nil
}

// --- reading the side tables (the composition's own SQL) ----------------

// tripAmendmentRow is one decoded `trip_amendment` row: the amendment's own
// key, its target, and what it asserted.
type tripAmendmentRow struct {
	Key             store.Key
	TripID          string
	Driver          *string
	Vehicle         *string
	Destination     *DestinationAmend
	CargoNote       *string
	OccupantsAdd    []string
	OccupantsRemove []string
	Cleared         []string
	// Seen (SchemaVersion 11, [[D-92]]) is the keys this row's author had
	// already seen standing — the dominance input to resolveContest. A row
	// with no Seen competes on key order alone.
	Seen []store.Key
}

// The contestant / amendmentRow surface (contest.go).
func (r tripAmendmentRow) contestKey() store.Key    { return r.Key }
func (r tripAmendmentRow) contestSeen() []store.Key { return r.Seen }
func (r tripAmendmentRow) targetID() string         { return r.TripID }

// arrivalAmendmentRow is one decoded `arrival_amendment` row.
type arrivalAmendmentRow struct {
	Key        store.Key
	ArrivalID  string
	Vehicle    *string
	OriginSite *string
	Seen       []store.Key // as on tripAmendmentRow
}

func (r arrivalAmendmentRow) contestKey() store.Key    { return r.Key }
func (r arrivalAmendmentRow) contestSeen() []store.Key { return r.Seen }
func (r arrivalAmendmentRow) targetID() string         { return r.ArrivalID }

// tripAmendmentCompositionSelect / …ByTripSQL — the composition read, and
// the SCOPED variant that is the only one a per-trip consumer may use
// (§3.3's `PersonByID` shape; W9a-16(a)).
//
// ⚠ THE ORDER BY IS NOT RESPELLED. Both constants concatenate
// `personAmendmentCompositionOrderBy` — THE one SQL ordering spelling
// (person_amend.go), beside THE one Go comparator `store.Key.Less`. A
// second comparator over Key is the drift surface VF's T-A oracle exists to
// refuse, and `Key.Less`'s own doc comment forbids one in terms.
const (
	tripAmendmentCompositionSelect = `SELECT site_id, node_epoch, seq, trip_id,
	driver_person_id, vehicle_entered, destination_kind, destination_value,
	deadline_minutes, cargo_note,
	occupants_add, occupants_remove, cleared, seen
	FROM trip_amendment`

	tripAmendmentCompositionSQL = tripAmendmentCompositionSelect +
		personAmendmentCompositionOrderBy

	tripAmendmentCompositionByTripSQL = tripAmendmentCompositionSelect +
		` WHERE trip_id = ?` + personAmendmentCompositionOrderBy

	tripAmendmentCompositionByKeySQL = tripAmendmentCompositionSelect +
		` WHERE site_id = ? AND node_epoch = ? AND seq = ?` + personAmendmentCompositionOrderBy

	arrivalAmendmentCompositionSelect = `SELECT site_id, node_epoch, seq, arrival_id,
	vehicle_entered, origin_site, seen
	FROM arrival_amendment`

	arrivalAmendmentCompositionByArrivalSQL = arrivalAmendmentCompositionSelect +
		` WHERE arrival_id = ?` + personAmendmentCompositionOrderBy

	arrivalAmendmentCompositionByKeySQL = arrivalAmendmentCompositionSelect +
		` WHERE site_id = ? AND node_epoch = ? AND seq = ?` + personAmendmentCompositionOrderBy
)

// readTripAmendmentsFor reads ONLY the amendment rows targeting tripID, in
// ascending key order — the scoped read §3.3 requires. It rides
// store.LocalTx, the exported *sql.Tx seam, because this query IS the SQL
// half of the one ordering spec and must exist here as a literal spelling.
func readTripAmendmentsFor(st *store.Store, tripID string) ([]tripAmendmentRow, error) {
	return queryTripAmendments(st, tripAmendmentCompositionByTripSQL, tripID)
}

func tripAmendmentAt(st *store.Store, k store.Key) (*tripAmendmentRow, error) {
	rows, err := queryTripAmendments(st, tripAmendmentCompositionByKeySQL, k.SiteID, k.NodeEpoch, k.Seq)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

func queryTripAmendments(st *store.Store, query string, args ...any) ([]tripAmendmentRow, error) {
	var out []tripAmendmentRow
	err := st.LocalRead(func(tx *sql.Tx) error {
		rows, err := tx.Query(query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				k                                   store.Key
				tripID, driver, vehicle             sql.NullString
				destKind, destValue, cargo, cleared sql.NullString
				add, remove, seen                   sql.NullString
				deadline                            sql.NullInt64
			)
			if err := rows.Scan(&k.SiteID, &k.NodeEpoch, &k.Seq, &tripID,
				&driver, &vehicle, &destKind, &destValue, &deadline,
				&cargo, &add, &remove, &cleared, &seen); err != nil {
				return err
			}
			r := tripAmendmentRow{Key: k, TripID: tripID.String, Seen: decodeSeenColumn(seen)}
			if driver.Valid {
				v := driver.String
				r.Driver = &v
			}
			if vehicle.Valid {
				v := vehicle.String
				r.Vehicle = &v
			}
			// The compound fact is present iff its KIND is (§3.4): one
			// whole assertion, never three fields that can interleave.
			if destKind.Valid {
				d := DestinationAmend{Kind: destKind.String, Value: destValue.String}
				if deadline.Valid {
					m := deadline.Int64
					d.DeadlineMinutes = &m
				}
				r.Destination = &d
			}
			if cargo.Valid {
				v := cargo.String
				r.CargoNote = &v
			}
			if add.Valid {
				r.OccupantsAdd = splitAmendList(add.String)
			}
			if remove.Valid {
				r.OccupantsRemove = splitAmendList(remove.String)
			}
			if cleared.Valid {
				r.Cleared = splitAmendList(cleared.String)
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("domain: read trip amendments: %w", err)
	}
	return out, nil
}

func readArrivalAmendmentsFor(st *store.Store, arrivalID string) ([]arrivalAmendmentRow, error) {
	return queryArrivalAmendments(st, arrivalAmendmentCompositionByArrivalSQL, arrivalID)
}

func arrivalAmendmentAt(st *store.Store, k store.Key) (*arrivalAmendmentRow, error) {
	rows, err := queryArrivalAmendments(st, arrivalAmendmentCompositionByKeySQL, k.SiteID, k.NodeEpoch, k.Seq)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

func queryArrivalAmendments(st *store.Store, query string, args ...any) ([]arrivalAmendmentRow, error) {
	var out []arrivalAmendmentRow
	err := st.LocalRead(func(tx *sql.Tx) error {
		rows, err := tx.Query(query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				k                                  store.Key
				arrivalID, vehicle, originSt, seen sql.NullString
			)
			if err := rows.Scan(&k.SiteID, &k.NodeEpoch, &k.Seq, &arrivalID, &vehicle, &originSt, &seen); err != nil {
				return err
			}
			r := arrivalAmendmentRow{Key: k, ArrivalID: arrivalID.String, Seen: decodeSeenColumn(seen)}
			if vehicle.Valid {
				v := vehicle.String
				r.Vehicle = &v
			}
			if originSt.Valid {
				v := originSt.String
				r.OriginSite = &v
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("domain: read arrival amendments: %w", err)
	}
	return out, nil
}

// --- what one row asserts (§6's derived vocabulary) ---------------------

// facts derives THIS ROW's fact vocabulary. It is DERIVED and not a
// hand-kept list precisely because `occupant/<person_id>` is a PATTERN: the
// suffix is data, so the vocabulary depends on the row and a static
// []string cannot hold it (F4 §11 — the requirement, not only the count).
func (r tripAmendmentRow) facts() []string {
	var out []string
	for _, f := range tripAmendableScalarFacts {
		if r.asserts(f) {
			out = append(out, f)
		}
	}
	for _, id := range r.occupantFactIDs() {
		out = append(out, factOccupantPrefix+id)
	}
	return out
}

// occupantFactIDs is every person id this row named, in canonical order —
// adds and removes alike, deduplicated. The RAW id, never a resolved one.
func (r tripAmendmentRow) occupantFactIDs() []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range append(append([]string(nil), r.OccupantsAdd...), r.OccupantsRemove...) {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func (r tripAmendmentRow) asserts(fact string) bool {
	if strings.HasPrefix(fact, factOccupantPrefix) {
		id := strings.TrimPrefix(fact, factOccupantPrefix)
		return r.addsOccupant(id) || r.removesOccupant(id)
	}
	return r.sets(fact) || r.clears(fact)
}

// sets reports whether this row SETS a scalar fact (a value, never a Clear).
func (r tripAmendmentRow) sets(fact string) bool {
	switch fact {
	case factTripDriver:
		return r.Driver != nil
	case factTripVehicle:
		return r.Vehicle != nil
	case factTripDestination:
		return r.Destination != nil
	case factTripCargoNote:
		return r.CargoNote != nil
	}
	return false
}

// clears reports whether this row's Clear list names a fact — whitelisted or
// not: the raw assertion, as the audit chain and Outcome report it.
func (r tripAmendmentRow) clears(fact string) bool {
	for _, c := range r.Cleared {
		if c == fact {
			return true
		}
	}
	return false
}

func (r tripAmendmentRow) addsOccupant(id string) bool {
	for _, s := range r.OccupantsAdd {
		if s == id {
			return true
		}
	}
	return false
}

func (r tripAmendmentRow) removesOccupant(id string) bool {
	for _, s := range r.OccupantsRemove {
		if s == id {
			return true
		}
	}
	return false
}

// asserted renders WHAT THIS ROW CLAIMED for one fact, canonically — the
// registry's third registration good (VF §4.4).
func (r tripAmendmentRow) asserted(fact string) string {
	if strings.HasPrefix(fact, factOccupantPrefix) {
		id := strings.TrimPrefix(fact, factOccupantPrefix)
		for _, s := range r.OccupantsRemove {
			if s == id {
				return occupantNotAboard
			}
		}
		for _, s := range r.OccupantsAdd {
			if s == id {
				return occupantAboard
			}
		}
		return ""
	}
	switch fact {
	case factTripDriver:
		if r.Driver != nil {
			return *r.Driver
		}
	case factTripVehicle:
		if r.Vehicle != nil {
			return *r.Vehicle
		}
	case factTripDestination:
		if r.Destination != nil {
			return renderDestinationAmend(*r.Destination)
		}
	case factTripCargoNote:
		if r.CargoNote != nil {
			return *r.CargoNote
		}
	}
	for _, c := range r.Cleared {
		if c == fact {
			return "cleared"
		}
	}
	return ""
}

// occupantAboard / occupantNotAboard are the two values an
// `occupant/<person_id>` fact can assert. They are the canonical Asserted
// rendering AND the value the composed read is compared against by VF's T-A
// agreement oracle, so they are spelled once.
const (
	occupantAboard    = "aboard"
	occupantNotAboard = "not aboard"
)

// OccupantAssertionIsRemoval reports whether an `occupant/<id>` fact's
// canonical Asserted rendering (Outcome's) is the REMOVAL half — so a
// confirmation can say which direction was recorded instead of labelling a
// removal exactly like an addition (L09 T-164). A read of the one spelling,
// never a second copy of it.
func OccupantAssertionIsRemoval(asserted string) bool { return asserted == occupantNotAboard }

func renderDestinationAmend(d DestinationAmend) string {
	out := d.Kind + " " + d.Value
	if d.DeadlineMinutes != nil {
		out += ", " + strconv.FormatInt(*d.DeadlineMinutes, 10) + " min"
	}
	return out
}

func (r arrivalAmendmentRow) facts() []string {
	var out []string
	for _, f := range arrivalAmendableFacts {
		if r.asserts(f) {
			out = append(out, f)
		}
	}
	return out
}

func (r arrivalAmendmentRow) asserts(fact string) bool {
	switch fact {
	case factArrivalVehicle:
		return r.Vehicle != nil
	case factArrivalOrigin:
		return r.OriginSite != nil
	}
	return false
}

func (r arrivalAmendmentRow) asserted(fact string) string {
	switch fact {
	case factArrivalVehicle:
		if r.Vehicle != nil {
			return *r.Vehicle
		}
	case factArrivalOrigin:
		if r.OriginSite != nil {
			return *r.OriginSite
		}
	}
	return ""
}

// --- composition (§3.3) --------------------------------------------------

// ComposedTrip is the EFFECTIVE record of one trip: the trip row with every
// amendment applied in ascending key order, plus the effective occupant
// set. The two travel together because they are ONE read: §9.2's consumers
// need both, and issuing two scoped reads per trip would double the decode
// cost on the muster path for nothing.
type ComposedTrip struct {
	Trip Trip
	// OccupantIDs is the effective occupant set — RAW person ids, in
	// canonical (sorted) order. Merge resolution belongs to the READ that
	// renders people ([[D-59]]), not here: resolving at compose time makes
	// the `occupant/<id>` fact name unstable across P-3 §5's un-merge.
	OccupantIDs []string
	// OverriddenRemovals lists person ids whose per-fact REMOVAL won its
	// contest and was then negated by the §3.4 structural union — the
	// effective driver. Wherever a fact's per-fact winner is negated by the
	// union, the confirmation and the audit chain owe the officer the
	// invariant-override sentence instead of a bare, falsely-reassuring
	// `Effective` (§3.4; mutant m6b).
	OverriddenRemovals []string
	// VehicleAmended is true when a trip_amended VEHICLE assertion won its
	// contest, so Trip.VehicleEntered is a human's correction of which truck
	// this run was rather than the dispatch-time snapshot. LiveVehicles reads
	// it: a typed correction names no vehicle record, so the record behind
	// VehicleID is no longer the truck the record says this run was.
	VehicleAmended bool
	// RoadWork (James, 2026-10-03) is the trip's KIND as the raw
	// trip_road_work side table records it (road_work.go) — equipment sent
	// out working on the road, no destination post. Read off the raw table
	// at compose time and NEVER amendable: the kind is what the officer
	// recorded at the gate, and no amendment fact names it.
	RoadWork bool
}

// EffectiveTrip composes one trip. SCOPED: one indexed read of that trip's
// own amendment rows, never a whole-table decode (§3.3, W9a-16(a)).
// Returns nil when this post has no trip row — a reader cannot show a trip
// it has never been told exists, and a dangling amendment composes to
// nothing until the trip arrives.
func EffectiveTrip(st *store.Store, tripID string) (*ComposedTrip, error) {
	t, err := TripByID(st, tripID)
	if err != nil || t == nil {
		return nil, err
	}
	occ, err := TripOccupantsByTrip(st, tripID)
	if err != nil {
		return nil, err
	}
	rows, err := readTripAmendmentsFor(st, tripID)
	if err != nil {
		return nil, err
	}
	rawIDs := make([]string, 0, len(occ))
	for _, o := range occ {
		rawIDs = append(rawIDs, o.PersonID)
	}
	out, err := composeTrip(st, *t, rawIDs, rows)
	if err != nil || out == nil {
		return out, err
	}
	// James, 2026-10-03: the road-work kind rides the composed trip so every
	// consumer that already composes (OpenExpectations, InTransitFrom, the
	// boards) can tell the kind without a second read of its own. One point
	// lookup on the side table's primary key (F30: never a whole-table dump).
	rw, err := IsRoadWork(st, tripID)
	if err != nil {
		return nil, err
	}
	out.RoadWork = rw
	return out, nil
}

// composeTrip is the composition proper, taking its rows as an argument so
// the whole of it is a pure function of (raw row, raw occupants, amendment
// rows) — which is exactly what makes two posts agree.
func composeTrip(st *store.Store, base Trip, rawOccupants []string, rows []tripAmendmentRow) (*ComposedTrip, error) {
	rawDriver := base.DriverPersonID
	out := ComposedTrip{Trip: base}

	// --- scalar facts: per fact, resolveContest's winner ([[D-92]]) -----
	// ⟦was: "last assertion in ascending key order wins" — ONE pass over the
	// rows, each row's values folded in turn⟧. A per-row fold cannot honour
	// dominance, because whether a row's value stands depends on rows that
	// come AFTER it in key order; under a record set with no `seen` anywhere
	// this is exactly the pre-11 fold. The contest is tripComposeContest's:
	// see it for the §3.3a whitelist filter, which is unchanged in effect.
	for _, fact := range tripAmendableScalarFacts {
		contest := tripComposeContest(rows, fact)
		if len(contest) == 0 {
			continue
		}
		winner, _ := resolveContest(contest)
		var w tripAmendmentRow
		for _, r := range contest {
			if r.Key == winner {
				w = r
				break
			}
		}
		switch fact {
		case factTripDriver:
			if w.Driver != nil {
				out.Trip.DriverPersonID = *w.Driver
			}
		case factTripVehicle:
			if w.Vehicle != nil {
				// ⟦LOCK — W7 seam repair, 2026-09-03⟧ trip.vehicle_entered is
				// a nullable derived snapshot under DR-W7-4, so the composed
				// field is a *string. The value asserted by the amendment is
				// unchanged. ⟦was: "out.Trip.VehicleEntered = *r.Vehicle"⟧
				v := *w.Vehicle
				out.Trip.VehicleEntered = &v
				out.VehicleAmended = true
			}
		case factTripDestination:
			if w.Destination != nil {
				applyDestinationAmend(&out.Trip, *w.Destination)
			}
		case factTripCargoNote:
			// A whitelisted Clear within the winning row takes precedence
			// over a value in the same row — the pre-11 per-row order, kept.
			if w.clears(fact) {
				out.Trip.CargoNote = nil
			} else if w.CargoNote != nil {
				v := *w.CargoNote
				out.Trip.CargoNote = &v
			}
		}
	}

	ids, overridden, err := composeOccupants(st, rawOccupants, rawDriver, out.Trip.DriverPersonID, rows)
	if err != nil {
		return nil, err
	}
	out.OccupantIDs = ids
	out.OverriddenRemovals = overridden
	return &out, nil
}

// tripComposeContest is the COMPOSED read's contest for one scalar fact: the
// rows that SET it, plus the rows that clear it when the fact is one `Clear`
// is whitelisted for.
//
// ⚠ COMPOSITION IS THE GUARANTEE (§3.3a): a `Clear` token outside the
// whitelist is NOT a contestant here, not merely refused at the authoring
// edge. The fold does not validate against current state, so a divergent
// writer's `clear:["destination"]` reaches this read — and letting it
// contest, let alone win, would compose the cleared destination [[D-4]]
// exists to make unauthorable. The raw row stands byte-identical (the log is
// append-only); Outcome and the audit chain still report the assertion
// (tripAmendmentRow.asserts is the wider predicate); the EFFECTIVE read is
// filtered — the same filter the per-row fold applied, now applied to the
// contest instead of to the fold step.
func tripComposeContest(rows []tripAmendmentRow, fact string) []tripAmendmentRow {
	var out []tripAmendmentRow
	for _, r := range rows {
		if r.sets(fact) || (clearIsWhitelisted(fact) && r.clears(fact)) {
			out = append(out, r)
		}
	}
	return out
}

// applyDestinationAmend applies the compound destination fact WHOLE (§3.4).
// The three fields are set together and the losing kind is cleared, so no
// interleaving can produce `uncontrolled + deadline` (the guaranteed-false-
// alarm pair) or both destinations set at once. An unrecognised kind is
// ignored rather than half-applied — the same posture as the `Clear`
// whitelist above, and for the same reason.
func applyDestinationAmend(t *Trip, d DestinationAmend) {
	switch d.Kind {
	case "controlled":
		v := d.Value
		t.DestinationSite = &v
		t.DestinationUncontrolled = nil
		t.DeadlineMinutes = nil
		if d.DeadlineMinutes != nil {
			m := *d.DeadlineMinutes
			t.DeadlineMinutes = &m
		}
	case "uncontrolled":
		v := d.Value
		t.DestinationUncontrolled = &v
		t.DestinationSite = nil
		t.DeadlineMinutes = nil // an uncontrolled run has no timer, ever
	}
}

// composeOccupants is §6's per-member composition and §3.4's structural
// union, in that order.
//
// ⚠ THE DRIVER'S MEMBERSHIP IS DERIVED, NOT RAW, and this is the one place
// the composed set is not simply "raw rows ± deltas". `applyTripDispatched`
// PREPENDS the driver into `trip_occupant` structurally, so the raw set
// already carries whoever was recorded as driving. §3.4 says composition
// "repeats it": the effective set therefore drops the RAW driver's
// prepended row and re-derives the membership from the EFFECTIVE driver.
// Otherwise a driver correction would leave BOTH names aboard — the
// criteria pin the arithmetic explicitly (W9a-14: three aboard, correct the
// driver and add one, the banner reads FOUR, not five) and W9a-5's literal
// set agrees because there the driver is unamended and the two readings
// coincide.
//
// ⚠ CRITERIA→DESIGN DELTA, FLAGGED FOR THE GATE: §3.3's pseudocode reads
// "trip_occupant rows … THEN occupants ∪= {effective driver}", which taken
// literally keeps the superseded driver aboard. The LOCKED criterion's
// arithmetic is the one built here. An officer who means "Smith was aboard
// as a passenger, Jones drove" says so with `occupants_add:[Smith]` in the
// same amendment, and that assertion survives — the delta is recorded in
// reports/window-build/W9a-build.md rather than resolved by a builder.
//
// The roster filter (§3.3a) applies to ids an AMENDMENT introduced, never
// to raw membership: a person the roster has not replicated here yet must
// not be dropped off a life-safety list by a read.
func composeOccupants(st *store.Store, rawOccupants []string, rawDriver, effectiveDriver string, rows []tripAmendmentRow) (ids []string, overriddenRemovals []string, err error) {
	aboard := map[string]bool{}
	for _, id := range rawOccupants {
		if id == rawDriver {
			continue // derived below from the EFFECTIVE driver
		}
		aboard[id] = true
	}
	// Per member, resolveContest's winner ([[D-92]]) — ⟦was: "the LAST
	// assertion in ascending key order wins", one pass over the rows⟧.
	// Different raw ids are different fact names (§6), so removing one
	// occupant does not contest with adding another. The contest for the
	// composed read excludes an ADD of an id absent from the roster (§3.3a:
	// ignored by the effective read, as before; the raw row still says what
	// it asserted, the log being append-only) — so a ghost add neither
	// stands nor dominates a real removal beneath it. Within one winning
	// row a removal outranks an add of the same id, as the row's own
	// rendering (asserted) already says.
	removedByWinner := map[string]bool{}
	named := map[string]bool{}
	var members []string
	for _, r := range rows {
		for _, id := range r.occupantFactIDs() {
			if !named[id] {
				named[id] = true
				members = append(members, id)
			}
		}
	}
	sort.Strings(members)
	for _, id := range members {
		fact := factOccupantPrefix + id
		var (
			contest       []tripAmendmentRow
			onRoster      bool
			rosterChecked bool
		)
		for _, r := range rows {
			if !r.asserts(fact) {
				continue
			}
			if r.removesOccupant(id) {
				contest = append(contest, r)
				continue
			}
			if !rosterChecked {
				ok, ferr := occupantIDIsOnRoster(st, id)
				if ferr != nil {
					return nil, nil, ferr
				}
				onRoster, rosterChecked = ok, true
			}
			if onRoster {
				contest = append(contest, r)
			}
		}
		if len(contest) == 0 {
			continue
		}
		winner, _ := resolveContest(contest)
		var w tripAmendmentRow
		for _, r := range contest {
			if r.Key == winner {
				w = r
				break
			}
		}
		if w.removesOccupant(id) {
			aboard[id] = false
			removedByWinner[id] = true
		} else {
			aboard[id] = true
			removedByWinner[id] = false
		}
	}
	// §3.4's structural union, LAST: the driver is always an occupant.
	if effectiveDriver != "" {
		if removedByWinner[effectiveDriver] {
			// The removal WON its per-fact contest and the union negates it
			// anyway. A bare `Effective` here is falsely reassuring — worse
			// than an adverse rendering — so the surfaces owe the officer
			// the invariant-override sentence (§3.4; mutant m6b).
			overriddenRemovals = append(overriddenRemovals, effectiveDriver)
		}
		aboard[effectiveDriver] = true
	}
	for id, in := range aboard {
		if in {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	sort.Strings(overriddenRemovals)
	return ids, overriddenRemovals, nil
}

// occupantIDIsOnRoster is §3.3a's roster filter. It asks the roster, not
// the amendment: a divergent writer can assert any id at all, and an id no
// person row backs would otherwise enter a life-safety count as a name
// nobody can call.
func occupantIDIsOnRoster(st *store.Store, personID string) (bool, error) {
	p, err := PersonByID(st, personID)
	if err != nil {
		return false, err
	}
	return p != nil, nil
}

// ComposedArrival is the EFFECTIVE record of one unsolicited arrival's two
// CLAIMS. It is a RECORD-ONLY correction: §5 rule 4 — a claim correction
// never re-runs correlation, never binds and never un-binds, so nothing
// here may reach `computeStates`.
type ComposedArrival struct {
	Arrival Arrival
	// Corrected reports whether ANY `arrival_amended` row exists for this
	// arrival — the signal §4's record-only sentence turns on. It rides the
	// composed value rather than being a second read, because a caller that
	// asked twice would decode the arrival's rows twice, which is exactly
	// the per-call cost §3.3's scoped-read rule exists to bound.
	Corrected bool
}

// EffectiveArrival composes one arrival. SCOPED, exactly as EffectiveTrip
// is, and for the same measured reason.
func EffectiveArrival(st *store.Store, arrivalID string) (*ComposedArrival, error) {
	a, err := rawArrivalByID(st, arrivalID)
	if err != nil || a == nil {
		return nil, err
	}
	rows, err := readArrivalAmendmentsFor(st, arrivalID)
	if err != nil {
		return nil, err
	}
	out := ComposedArrival{Arrival: *a, Corrected: len(rows) > 0}
	// Per claim, resolveContest's winner ([[D-92]]) — ⟦was: "last assertion
	// in ascending key order", one pass over the rows⟧.
	for _, fact := range arrivalAmendableFacts {
		contest := rowsAsserting(rows, fact)
		if len(contest) == 0 {
			continue
		}
		winner, _ := resolveContest(contest)
		for _, r := range contest {
			if r.Key != winner {
				continue
			}
			switch fact {
			case factArrivalVehicle:
				if r.Vehicle != nil {
					v := *r.Vehicle
					out.Arrival.VehicleEntered = &v
				}
			case factArrivalOrigin:
				if r.OriginSite != nil {
					v := *r.OriginSite
					out.Arrival.OriginSite = &v
				}
			}
			break
		}
	}
	return &out, nil
}

// rawArrivalByID resolves one arrival row by primary key; nil when unknown
// here. Deliberately UNEXPORTED and W9a-local: W9b lands its own exported
// arrival reader in the same window, and two chunks racing to own one
// exported name is a merge conflict dressed as a design decision.
func rawArrivalByID(st *store.Store, arrivalID string) (*Arrival, error) {
	return oneRow[Arrival](st, "trip_arrival", store.Eq{Column: "arrival_id", Value: arrivalID})
}

// --- the read path's identity resolution ([[D-59]]) ----------------------

// composedOccupantRefs turns a composed occupant id set into display rows,
// ONE ROW PER HUMAN.
//
// ⚠ THIS IS THE OTHER HALF OF [[D-59]], and it is NOT "compose amendments"
// — it is "resolve identity", and it must not be booked as done by the
// composition change. The wire half keeps the RAW id (`occupant/<raw id>`
// is a stable fact name across P-3 §5's un-merge, which is what keeps
// `Outcome`'s per-fact supersession resolvable). The READ half is here:
// post A adds P1 to a trip and post B independently and legally merges P1
// into P2 — both ordinary, neither a UI defect — and without this the
// not-accounted-for section renders ONE HUMAN TWICE, under two names, on
// the surface [[D-6]] makes 100%-accountable.
//
// `merge` is loaded ONCE by the caller: it is a whole-table derivation, and
// loading it inside a per-trip loop would put a season's amendment decode
// behind every row of a muster.
func composedOccupantRefs(st *store.Store, ids []string, merge AliasMap, nameOf func(string) (string, error)) ([]OccupantRef, error) {
	out := make([]OccupantRef, 0, len(ids))
	byHuman := map[string]int{}
	for _, id := range ids {
		rep := id
		if merge != nil {
			rep = merge.Resolve(id)
		}
		// The display name is the REPRESENTATIVE's: one human, one name.
		name, err := nameOf(rep)
		if err != nil {
			return nil, err
		}
		if name == "" {
			// A manifest row never vanishes because a roster reference has
			// not replicated here yet — show the id.
			name = rep
		}
		if i, seen := byHuman[rep]; seen {
			// Already listed. Prefer the representative's own id as the
			// row's PersonID when it is itself aboard, so the row a
			// surface posts back names the surviving record.
			if id == rep {
				out[i].PersonID = rep
			}
			continue
		}
		byHuman[rep] = len(out)
		out = append(out, OccupantRef{PersonID: id, Name: name})
	}
	return out, nil
}

// --- the audit chain (append-only; §7's incident read) --------------------------

// TripChainFact is one asserted fact of one entry in a trip's record chain.
type TripChainFact struct {
	// Fact is the registered fact name — `occupant/<raw person_id>` included,
	// so a reader can tell membership assertions from scalar ones without a
	// second copy of the vocabulary.
	Fact string
	// Asserted is what was claimed, canonically rendered by the carrier's
	// own registration (VF §4.4) — the raw value, never a display name.
	Asserted string
	// PersonID is non-empty when the fact names a person (the driver, or an
	// occupant), so the rendering layer can resolve a display name without
	// re-deriving which facts are person-shaped.
	PersonID string
}

// TripChainEntry is one post's assertion about a trip: the dispatch that
// created it, or one amendment.
type TripChainEntry struct {
	Key store.Key
	// SiteID is the post that authored the assertion. Attribution is
	// STRUCTURAL — `env.SiteID`, carried on the row — and it is the whole
	// of what the chain can say about who corrected the record: the
	// amendment payloads carry no `recorded_by` by design (D-5).
	SiteID   string
	Dispatch bool
	Facts    []TripChainFact
}

// TripAmendmentChain is the record chain of one trip: what it was
// DISPATCHED as, then every amendment in ascending key order, each with the
// post that made it.
//
// ⚠ APPEND-ONLY, enforced by SQL trigger: this is why an amendment is a NEW EVENT
// and never an edit. ⟦was: "⚠ [[D-1]]: this is why an amendment is a NEW EVENT and never an edit."⟧
// ⚠ D-1 IS NOT THIS RULE. `JAMES-DECISIONS.md:12` D-1 is "The proposal leads with the
// capability gap" — a funding-proposal ruling. Append-only has NO decision number; it is an
// architectural invariant enforced by the log's SQL trigger. Do not "restore" the D-1 cite. The
// chain preserves what the record said before — an amendment that erased
// its predecessor is the editing fold, on a page. The incident read (D-5)
// answers "who was driving, who was aboard" from the EFFECTIVE record and
// shows the superseded assertions beneath it; both halves are owed.
func TripAmendmentChain(st *store.Store, tripID string) ([]TripChainEntry, error) {
	t, err := TripByID(st, tripID)
	if err != nil || t == nil {
		return nil, err
	}
	occ, err := TripOccupantsByTrip(st, tripID)
	if err != nil {
		return nil, err
	}
	dispatch := TripChainEntry{
		Key:      store.Key{SiteID: t.OriginSite, NodeEpoch: t.NodeEpoch, Seq: t.Seq},
		SiteID:   t.OriginSite,
		Dispatch: true,
		Facts: []TripChainFact{
			{Fact: factTripDriver, Asserted: t.DriverPersonID, PersonID: t.DriverPersonID},
			// ⟦LOCK — W7 seam repair, 2026-09-03⟧ ⟦was: "Asserted: t.VehicleEntered"⟧
			{Fact: factTripVehicle, Asserted: derefString(t.VehicleEntered)},
		},
	}
	if t.DestinationSite != nil {
		d := DestinationAmend{Kind: "controlled", Value: *t.DestinationSite, DeadlineMinutes: t.DeadlineMinutes}
		dispatch.Facts = append(dispatch.Facts, TripChainFact{Fact: factTripDestination, Asserted: renderDestinationAmend(d)})
	} else if t.DestinationUncontrolled != nil {
		d := DestinationAmend{Kind: "uncontrolled", Value: *t.DestinationUncontrolled}
		dispatch.Facts = append(dispatch.Facts, TripChainFact{Fact: factTripDestination, Asserted: renderDestinationAmend(d)})
	}
	for _, o := range occ {
		if o.PersonID == t.DriverPersonID {
			continue // already named as the driver, and named once is enough
		}
		dispatch.Facts = append(dispatch.Facts, TripChainFact{
			Fact: factOccupantPrefix + o.PersonID, Asserted: occupantAboard, PersonID: o.PersonID,
		})
	}
	out := []TripChainEntry{dispatch}

	rows, err := readTripAmendmentsFor(st, tripID)
	if err != nil {
		return nil, err
	}
	reg := carrierRegistry[evtTripAmended]
	for _, r := range rows {
		e := TripChainEntry{Key: r.Key, SiteID: r.Key.SiteID}
		for _, fact := range reg.TripRowFacts(r) {
			f := TripChainFact{Fact: fact, Asserted: reg.AssertedTrip(r, fact)}
			if id, ok := OccupantFactPersonID(fact); ok {
				f.PersonID = id
			} else if fact == factTripDriver {
				f.PersonID = f.Asserted
			}
			e.Facts = append(e.Facts, f)
		}
		out = append(out, e)
	}
	return out, nil
}

// TripAmendmentTarget returns the trip an amendment event targeted, or ""
// when this post holds no amendment row at that key.
func TripAmendmentTarget(st *store.Store, k store.Key) (string, error) {
	row, err := tripAmendmentAt(st, k)
	if err != nil || row == nil {
		return "", err
	}
	return row.TripID, nil
}
