// vehicle_amend.go — W7 (item 9): the `vehicle_amended` event, its INSERT-only
// `vehicle_amendment` side table, the ascending-key composed read, the
// amendment authoring edge and its refusals, and the derived vehicle
// merge-resolution snapshot (`same_as`, P-3 option 1a).
//
// This is the THIRD clone of W3's mechanism-defining build in person_amend.go,
// after W5d's company clone in company_amend.go (WINDOW-BUILD-PLAN-v1
// `### W7 —`, criteria LOCKED 2026-09-03).
//
// What differs from the company side, and why:
//
//   - The amendable set is EXACTLY {number, role, same_as} (DR-W7-6), where
//     company has {name, same_as}. `retired` does not exist as a column, a
//     fact or an edge control (DR-W7-8), and neither does `recorded_by`
//     (DR-W7-7) — attribution on an amendment is STRUCTURAL, env.SiteID,
//     never a payload field ([[D-5]]).
//   - BOTH content facts are CLEARABLE, where company's `name` is not. A
//     company always has a name (`company.name` is TEXT NOT NULL) so blanking
//     it is refused; a vehicle may legitimately lose its number — "the number
//     we were told turned out to be wrong and we do not have another" — and
//     the column is nullable precisely so that state is representable. What is
//     refused is clearing BOTH, which would leave a record identifying
//     nothing.
//   - The kind check INVERTS again (W7-13): `same_as` joins VEHICLE records
//     only, so a vehicle to person and a vehicle to company target are both
//     refused here. ⚠ Vehicle is the first entity with TWO wrong-kind
//     neighbours already in the tree, and the wrong-kind refusal is worded so
//     it is DISTINGUISHABLE IN TEXT from the unknown-target one — otherwise a
//     kind check and an existence check cannot be told apart (m7-13b).
//
// The load-bearing rules are W3's, unchanged and not restated: see
// person_amend.go's file comment for composition order, the edge/fold
// asymmetry, Clear-composes-as-absent, the derived-per-read snapshot, and the
// never-stored supersession rule. ⟦SchemaVersion 11, [[D-92]], step 3⟧ The
// causal `seen` reference is cloned here third, after company and trade:
// payload field, side-table column, edge stamp, and every read (composition,
// the merge snapshot, Outcome/Competitors) resolves through resolveContest —
// the ONE rule, read through contest.go, never respelled.
package domain

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"sitecontrol/internal/store"
)

// --- event payload -------------------------------------------------------

// VehicleAmended is the wire payload (type "vehicle_amended"). `clear` is
// lowercase on the wire (DECIDED at P-3 §7 rev 3). The frozen literals for
// gate B-2's review are the type name and the field names `vehicle_id`,
// `number`, `role`, `same_as` and `clear` — each fact name freezing TWICE
// over, because the same spellings are simultaneously the Clear VALUE
// vocabulary (§7 row 3).
//
// ⚠ `recorded_by` is deliberately absent (DR-W7-7) and so is any reason, note
// or comment field — [[D-49]] reaches this payload by its own text and rules
// exactly that (DR-W7-20).
//
// ⟦SchemaVersion 11⟧ `seen` is ADDED ([[D-92]]): the keys of the earlier
// amendments this author had already seen standing for the facts asserted
// here (resolveContest). Stamped by AmendVehicle, never by a caller; omitted
// when the author saw nothing standing. Its spelling is person_amended's
// SeenRef — no new literal for B-2 beyond step 2's `seen`.
type VehicleAmended struct {
	Type      string    `json:"type"` // "vehicle_amended"
	VehicleID string    `json:"vehicle_id"`
	Number    *string   `json:"number,omitempty"`
	Role      *string   `json:"role,omitempty"`
	SameAs    *string   `json:"same_as,omitempty"`
	Clear     []string  `json:"clear,omitempty"`
	Seen      []SeenRef `json:"seen,omitempty"`
}

const evtVehicleAmended = "vehicle_amended"

// vehicleAmendableFacts is the amendable set, in canonical rendering order
// (DR-W7-6, as literals). Fact names ARE the payload field spellings —
// visible-failure-v1 §4.2's convention, cloned.
var vehicleAmendableFacts = []string{"number", "role", "same_as"}

func isVehicleAmendableFact(f string) bool {
	for _, k := range vehicleAmendableFacts {
		if k == f {
			return true
		}
	}
	return false
}

// VehicleAmendment is the authoring-edge input. Value fields are pointers:
// nil = "this amendment does not touch that field", which is a different thing
// from asserting an empty value. Clear lists fact names to UN-SET;
// Clear: ["same_as"] is the un-merge — there is no separate un-merge event and
// no separate merge affordance (DR-6 / S6: one POST for every vehicle fact,
// never a parallel author).
type VehicleAmendment struct {
	VehicleID string
	Number    *string
	Role      *string
	SameAs    *string
	Clear     []string
}

// setFields returns the asserted (fact to value) pairs of the input. Note what
// this makes UNREPRESENTABLE, because a criterion turns on it: an out-of-set
// fact cannot be ASSERTED at all — the input is a struct and this map is built
// from its own fields — so the out-of-set arm is reachable through the `Clear`
// vocabulary only.
func (in VehicleAmendment) setFields() map[string]*string {
	return map[string]*string{"number": in.Number, "role": in.Role, "same_as": in.SameAs}
}

// --- the INSERT-only side table (projection) -----------------------------

// vehicleAmendmentProjection registers the side table the fold maintains and
// the composition reads — person_amend.go's shape, cloned through W5d's. The
// declared column set is EXACT (DR-W7-1, W7-7(a)): the key triple, the target,
// one column per amendable fact, `cleared`, and — since SchemaVersion 11
// ([[D-92]]) — `seen` (the author's seen-list, JSON, NULL when empty: a
// reference written at authoring, never a status) — NINE; DR-W7-1 pinned
// EIGHT and is re-pinned with the field ⟦was: "and `cleared`"⟧.
//
// ⚠ NO status-shaped column may ever join it — supersession is computed at
// read, never stored — and no `retired` column may ever join it (DR-W7-8).
// The two triggers make INSERT-only a MECHANISM rather than a convention, and
// W7-7(b) asserts them BEHAVIOURALLY: a trigger asserted by name is satisfied
// by a trigger whose body does nothing.
func vehicleAmendmentProjection() store.Projection {
	return store.Projection{
		Name:    "vehicle_amendment",
		Table:   "vehicle_amendment",
		OrderBy: "site_id, node_epoch, seq",
		Schema: `CREATE TABLE IF NOT EXISTS vehicle_amendment (
			site_id    TEXT NOT NULL,
			node_epoch INTEGER NOT NULL,
			seq        INTEGER NOT NULL,
			vehicle_id TEXT,
			number     TEXT,
			role       TEXT,
			same_as    TEXT,
			cleared    TEXT,
			seen       TEXT,
			PRIMARY KEY (site_id, node_epoch, seq)
		) STRICT;
		CREATE TRIGGER IF NOT EXISTS vehicle_amendment_insert_only_update
			BEFORE UPDATE ON vehicle_amendment BEGIN
			SELECT RAISE(ABORT, 'vehicle_amendment is INSERT-only: supersession is computed at read, never rewritten in place');
		END;
		CREATE TRIGGER IF NOT EXISTS vehicle_amendment_insert_only_delete
			BEFORE DELETE ON vehicle_amendment BEGIN
			SELECT RAISE(ABORT, 'vehicle_amendment is INSERT-only: amendments are superseded, never merely absent');
		END;`,
		Apply: applyVehicleAmended,
	}
}

// applyVehicleAmended is the fold. IT NEVER REFUSES (W3-2, ridden at W7-17): a
// wire-received amendment passed its authoring post's edge, or was minted by a
// divergent one, and refusing it here would fork the two logs forever.
// Cross-kind `same_as` targets, cycle-forming merges, cleared numbers, no-ops —
// all fold. Values are coerced tolerantly for the same reason: a payload this
// decoder finds odd must still land.
func applyVehicleAmended(tx *sql.Tx, env store.Envelope) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(env.Payload, &raw); err != nil {
		return nil // not an object-shaped payload; not this fold's event
	}
	var typ string
	if err := json.Unmarshal(raw["type"], &typ); err != nil || typ != evtVehicleAmended {
		return nil
	}
	asString := func(key string) any {
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
	var cleared any
	if r, ok := raw["clear"]; ok {
		var list []string
		if err := json.Unmarshal(r, &list); err == nil && len(list) > 0 {
			b, err := json.Marshal(list)
			if err != nil {
				return fmt.Errorf("domain: vehicle_amended fold: cleared: %w", err)
			}
			cleared = string(b)
		}
	}
	// `seen` ([[D-92]]) through the one codec, seenColumnValue (contest.go):
	// absent, empty or malformed lands as NULL — the fold never refuses.
	seen, err := seenColumnValue(raw)
	if err != nil {
		return fmt.Errorf("domain: vehicle_amended fold: seen: %w", err)
	}
	_, err = tx.Exec(`INSERT INTO vehicle_amendment
		(site_id, node_epoch, seq, vehicle_id, number, role, same_as, cleared, seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		env.SiteID, env.NodeEpoch, env.Seq,
		asString("vehicle_id"), asString("number"), asString("role"), asString("same_as"), cleared, seen)
	if err != nil {
		return fmt.Errorf("domain: vehicle_amended fold: insert %v: %w", env.Key(), err)
	}
	return nil
}

// --- reading the side table (the composition's own SQL) ------------------

// vehicleAmendmentRow is one decoded side-table row: the amendment's key, its
// target, its asserted facts, its cleared facts, and (SchemaVersion 11,
// [[D-92]]) the keys its author had already seen standing — the dominance
// input to resolveContest. A row with no Seen competes on key order alone.
type vehicleAmendmentRow struct {
	Key       store.Key
	VehicleID string
	Set       map[string]string
	Cleared   []string
	Seen      []store.Key
}

// The contestant / amendmentRow surface (contest.go).
func (r vehicleAmendmentRow) contestKey() store.Key    { return r.Key }
func (r vehicleAmendmentRow) contestSeen() []store.Key { return r.Seen }
func (r vehicleAmendmentRow) targetID() string         { return r.VehicleID }

// asserts reports whether this row makes a claim about `fact` — set or
// cleared. A CLEAR is an assertion: it says the fact is now absent, which is a
// different claim from saying nothing about it, and the outcome resolution
// contests the two together.
func (r vehicleAmendmentRow) asserts(fact string) bool {
	if _, ok := r.Set[fact]; ok {
		return true
	}
	for _, c := range r.Cleared {
		if c == fact {
			return true
		}
	}
	return false
}

// The vehicle composition read, in the house's own two-half shape (the M-1
// fix): a shared SELECT half, a shared scoping clause, and THE ONE ordering
// spelling — which is not respelled here at all.
//
// ⚠ personAmendmentCompositionOrderBy is REUSED VERBATIM rather than cloned,
// exactly as W5d reused it. Sharing the constant makes "one canonical ordering
// spelling" structural instead of conventional: there is exactly ONE
// key-column ordering literal across the amendment compositions, and a drift
// would have to change the spelling for every side table at once. The name
// reads person-sided because W3 declared it; renaming it is W3's code.
//
// ⚠ W7-9(a) DOES clone the M-1 scoped shape, where W5d chose not to: a vehicle
// is resolved per authored trip and per rendered picker row, which are the two
// hottest paths in the application, so a whole-table amendment decode per
// lookup is exactly the cost M-1 removed.
const (
	vehicleAmendmentCompositionSelect = `SELECT site_id, node_epoch, seq, vehicle_id,
	number, role, same_as, cleared, seen
	FROM vehicle_amendment`

	vehicleAmendmentCompositionSQL = vehicleAmendmentCompositionSelect +
		personAmendmentCompositionOrderBy

	vehicleAmendmentCompositionByVehicleSQL = vehicleAmendmentCompositionSelect +
		` WHERE vehicle_id = ?` + personAmendmentCompositionOrderBy
)

// readVehicleAmendments reads EVERY vehicle amendment row in ascending key
// order — the substrate of composition and of the derived merge snapshot, so
// both answer from the same record set in the same order.
func readVehicleAmendments(st *store.Store) ([]vehicleAmendmentRow, error) {
	return queryVehicleAmendments(st, vehicleAmendmentCompositionSQL)
}

// readVehicleAmendmentsFor reads ONLY the rows targeting vehicleID — same
// columns, same ascending-key order, both spellings shared with the read above
// and never respelled.
func readVehicleAmendmentsFor(st *store.Store, vehicleID string) ([]vehicleAmendmentRow, error) {
	return queryVehicleAmendments(st, vehicleAmendmentCompositionByVehicleSQL, vehicleID)
}

// queryVehicleAmendments is the shared decode loop under both reads. It rides
// store.LocalTx, the exported *sql.Tx seam, for the reason readPersonAmendments
// states: this query IS the SQL half of the one ordering spec and must exist
// here as a literal spelling, where the drift scan that guards it can see it.
func queryVehicleAmendments(st *store.Store, query string, args ...any) ([]vehicleAmendmentRow, error) {
	var out []vehicleAmendmentRow
	err := st.LocalRead(func(tx *sql.Tx) error {
		rows, err := tx.Query(query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				k         store.Key
				vehicleID sql.NullString
				vals      = make([]sql.NullString, len(vehicleAmendableFacts))
				cleared   sql.NullString
				seen      sql.NullString
			)
			scan := []any{&k.SiteID, &k.NodeEpoch, &k.Seq, &vehicleID}
			for i := range vals {
				scan = append(scan, &vals[i])
			}
			scan = append(scan, &cleared, &seen)
			if err := rows.Scan(scan...); err != nil {
				return err
			}
			r := vehicleAmendmentRow{Key: k, VehicleID: vehicleID.String, Set: map[string]string{}}
			for i, fact := range vehicleAmendableFacts {
				if vals[i].Valid {
					r.Set[fact] = vals[i].String
				}
			}
			if cleared.Valid && cleared.String != "" {
				var list []string
				if err := json.Unmarshal([]byte(cleared.String), &list); err == nil {
					r.Cleared = list
				}
			}
			r.Seen = decodeSeenColumn(seen)
			out = append(out, r)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("domain: read vehicle amendments: %w", err)
	}
	return out, nil
}

// --- composition (the ONE composed read path, W7-19) ---------------------

// composeVehicle folds the amendment rows targeting ONE vehicle into its
// composed record, fact by fact: the row resolveContest names as the winner
// of each fact's target-scope contest supplies that fact — a set value
// lands, a clear makes the fact ABSENT, never empty-string (W3-3's rule on
// this entity).
//
// ⟦SchemaVersion 11, [[D-92]]⟧ ⟦was: applyVehicleAmendmentRow — one row
// folded at a time in key order, "later keys win per fact"⟧. A per-row fold
// cannot honour dominance, because whether a row's value stands depends on
// rows that come AFTER it in key order; under a record set with no `seen`
// anywhere this is exactly the pre-11 fold. AmendVehicle's arm 6 still
// evaluates its would-be result through THIS function (a one-row contest,
// which that row wins outright), so the edge and the read run the same fold.
func composeVehicle(v *Vehicle, rows []vehicleAmendmentRow) {
	target := func(fact string) **string {
		switch fact {
		case "number":
			return &v.Number
		case "role":
			return &v.Role
		case "same_as":
			return &v.SameAs
		}
		return nil
	}
	for _, fact := range vehicleAmendableFacts {
		contest := rowsAsserting(rows, fact)
		if len(contest) == 0 {
			continue
		}
		winner, _ := resolveContest(contest)
		var w vehicleAmendmentRow
		for _, r := range contest {
			if r.Key == winner {
				w = r
				break
			}
		}
		dst := target(fact)
		if dst == nil {
			continue
		}
		if val, ok := w.Set[fact]; ok {
			s := val
			*dst = &s
		} else {
			*dst = nil // the winner cleared it
		}
	}
}

// composeVehicles applies every amendment over the base created-state rows,
// one composed vehicle at a time (composeVehicle). Rows amending a vehicle
// this store has no base row for compose to nothing here — the record set
// keeps them; a reader cannot show a vehicle it has never been told exists.
func composeVehicles(base []Vehicle, amendments []vehicleAmendmentRow) []Vehicle {
	byID := make(map[string]*Vehicle, len(base))
	for i := range base {
		byID[base[i].VehicleID] = &base[i]
	}
	perVehicle := map[string][]vehicleAmendmentRow{}
	for _, r := range amendments {
		perVehicle[r.VehicleID] = append(perVehicle[r.VehicleID], r)
	}
	for id, rows := range perVehicle {
		if v, ok := byID[id]; ok {
			composeVehicle(v, rows)
		}
	}
	return base
}

// --- the derived merge-resolution snapshot (P-3 §5) ----------------------

// LoadVehicleMergeMap loads the per-read derived vehicle resolution snapshot —
// the vehicle analogue of LoadCompanyMergeMap. It is a pure projection of
// composed state: each vehicle's EFFECTIVE `same_as` (the winner of that
// vehicle's same_as contest under resolveContest: causal first, then record
// order; a winning Clear removes the edge ⟦was: "last in record order"⟧),
// plus the one derived rule the snapshot layer owns —
// wire-formed cycles COLLAPSE to one pinned canonical representative.
//
// ⚠⚠ THE REPRESENTATIVE IS THE TARGET OF THE CYCLE-FORMING ASSERTION LATEST IN
// RECORD ORDER (W3-17, cloned at W5d-12, carried here by SEAM-W7-20). It is
// NOT the smallest id. `design/analytics-v1.md` and `design/PLAN-SEC-A-schema.md`
// both asserted a smallest-id rule until 2026-09-03; both now carry the
// supersession with their superseded sentence preserved, naming W7's lock as
// the authority. A build implementing the struck rule reds against W7-14.
//
// ⚠ It is DERIVED PER READ and caches NOTHING. Two mutants live here and both
// are booked: a cached or package-level map, and a map built from the composed
// Vehicle.SameAs scalar alone — which resolves single edges correctly and
// silently loses cycle collapse, because composition discards each edge's
// store.Key and the representative rule needs it.
func LoadVehicleMergeMap(st *store.Store) (AliasMap, error) {
	rows, err := readVehicleAmendments(st)
	if err != nil {
		return nil, fmt.Errorf("domain: load vehicle merge map: %w", err)
	}
	return vehicleMergeMapFromRows(rows), nil
}

func vehicleMergeMapFromRows(rows []vehicleAmendmentRow) AliasMap {
	type edge struct {
		target string
		key    store.Key
	}
	// Effective same_as per vehicle: the winner of each vehicle's same_as
	// contest (resolveContest — the ONE rule, [[D-92]]); a winning Clear is
	// no edge. Rows arrive already in ascending key order. The cycle
	// representative below stays key-ordered at SchemaVersion 11: a cycle's
	// edges belong to DIFFERENT vehicles' contests, and dominance is defined
	// within one fact's contest, never across them. ⟦was: "last assertion in
	// record order wins"⟧
	perVehicle := map[string][]vehicleAmendmentRow{}
	var order []string
	for _, r := range rows {
		if !r.asserts("same_as") {
			continue
		}
		if _, ok := perVehicle[r.VehicleID]; !ok {
			order = append(order, r.VehicleID)
		}
		perVehicle[r.VehicleID] = append(perVehicle[r.VehicleID], r)
	}
	eff := map[string]edge{}
	for _, id := range order {
		contest := perVehicle[id]
		winner, _ := resolveContest(contest)
		for _, r := range contest {
			if r.Key != winner {
				continue
			}
			if v, ok := r.Set["same_as"]; ok {
				eff[id] = edge{target: v, key: r.Key}
			}
			break
		}
	}
	m := make(AliasMap, len(eff))
	edgeKey := make(map[string]store.Key, len(eff))
	for src, e := range eff {
		if e.target == src {
			continue // a wire-formed self-loop resolves nothing; drop the edge
		}
		m[src] = e.target
		edgeKey[src] = e.key
	}
	// Cycle collapse. Walk each node once; a node revisited within the current
	// walk closes a cycle, whose members' composed edges are the "composed
	// cycle set" the representative rule quantifies over.
	done := map[string]bool{}
	nodes := make([]string, 0, len(m))
	for n := range m {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	for _, start := range nodes {
		if done[start] {
			continue
		}
		pathIdx := map[string]int{}
		var path []string
		cur := start
		for {
			if done[cur] {
				break
			}
			if i, in := pathIdx[cur]; in {
				// path[i:] is the cycle. The representative: the target of the
				// record-order-latest cycle edge.
				cycle := path[i:]
				var rep string
				var latest store.Key
				first := true
				for _, member := range cycle {
					k := edgeKey[member]
					if first || latest.Less(k) {
						rep = m[member]
						latest = k
						first = false
					}
				}
				for _, member := range cycle {
					if member == rep {
						delete(m, member) // the representative resolves to itself
					} else {
						m[member] = rep
					}
				}
				break
			}
			pathIdx[cur] = len(path)
			path = append(path, cur)
			next, ok := m[cur]
			if !ok {
				break
			}
			cur = next
		}
		for _, n := range path {
			done[n] = true
		}
	}
	return m
}

// ResolveVehicleID walks the merge map to the record a vehicle id resolves to.
// It is the ONE resolution entry point every consumer uses — the muster's
// in-transit block, the picker, the duplicates list — so "which record is this
// truck" has exactly one answer per read.
func ResolveVehicleID(m AliasMap, id string) string {
	cur := id
	for i := 0; i < maxMergeWalk; i++ {
		next, ok := m[cur]
		if !ok || next == cur {
			return cur
		}
		cur = next
	}
	return cur
}

// --- the authoring edge --------------------------------------------------

// VehicleAmendRefusal is an EDGE REFUSAL, distinguished from an internal
// failure BY TYPE rather than by reading an error string (W7-15's class (a)).
type VehicleAmendRefusal struct {
	// Reason is the officer-facing sentence, without prefix or suffix.
	Reason string
}

func (e *VehicleAmendRefusal) Error() string {
	return "domain: amend vehicle: refused — " + e.Reason + "; nothing was recorded"
}

func refuseVehicleAmend(format string, args ...any) error {
	return &VehicleAmendRefusal{Reason: fmt.Sprintf(format, args...)}
}

// VehicleAmendInputError is the edge's THIRD error class: the officer named a
// record this post cannot act on (W7-15's class (b)). It is neither a
// fact-domain refusal nor an internal fault, and collapsing it into either one
// tells an officer something false.
//
// ⚠ The reason this type exists is MEASURED, on the surface this chunk clones:
// at W5d a mistyped id — the single most likely mistake on a surface whose id
// is typed by hand — fell through to the web layer's internal-error arm and was
// answered with HTTP 500 and copy telling the officer to radio a post that was
// working perfectly. Every clause of that was false: the post completed its
// work correctly, retrying an unknown id fails identically forever, and the
// officer was sent to the radio over a typo. W7-15(b) is the check that stops
// the third instance of it.
type VehicleAmendInputError struct {
	// Reason is the officer-facing sentence: what is wrong with what they
	// named, with no prefix and no trailing clause — Error() and the page's own
	// copy each add that themselves.
	Reason string
}

func (e *VehicleAmendInputError) Error() string {
	return "domain: amend vehicle: " + e.Reason + "; nothing was recorded"
}

func refuseVehicleInput(format string, args ...any) error {
	return &VehicleAmendInputError{Reason: fmt.Sprintf(format, args...)}
}

// AmendVehicle authors one `vehicle_amended` event. EVERY refusal below is
// PRE-APPEND (W3-2, ridden at W7-17): a refusal returns before Author is
// called, so the log length is unchanged and no envelope exists. These checks
// live at this edge and ONLY at this edge — the fold never refuses (see
// applyVehicleAmended).
//
// The two guards above the fact-domain arms are INPUT-VALIDITY refusals: they
// carry no "refused —" spelling (their person- and company-side analogues do
// not either) and they return *VehicleAmendInputError so a caller can tell them
// from both a fact-domain refusal and an internal fault.
func AmendVehicle(st *store.Store, siteID string, nodeEpoch int64, in VehicleAmendment, occurredAtMS int64, timeSource string) (store.Envelope, error) {
	if in.VehicleID == "" {
		return store.Envelope{}, refuseVehicleInput("a vehicle id is required")
	}
	v, err := VehicleByID(st, in.VehicleID)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: amend vehicle: %w", err)
	}
	if v == nil {
		return store.Envelope{}, refuseVehicleInput("%q is not a vehicle on this post", in.VehicleID)
	}

	set := in.setFields()
	assertedCount := 0
	for _, val := range set {
		if val != nil {
			assertedCount++
		}
	}
	// Arm 1 — the no-op refusal: an amendment that asserts nothing and clears
	// nothing records nothing, and recording nothing must be refused, not
	// minted.
	if assertedCount == 0 && len(in.Clear) == 0 {
		return store.Envelope{}, refuseVehicleAmend("the amendment changes nothing (no field set, nothing cleared)")
	}
	// Arm 2 — an EMPTY assertion. Both content facts are nullable, so absence
	// has a first-class spelling already: Clear. Asserting a blank string would
	// be a second, silent way to say the same thing, and DR-W7-15 rules the
	// empty string IS the absence rather than a stand-in for it — so the edge
	// sends the officer to the affordance that says what they mean.
	for _, fact := range []string{"number", "role"} {
		if val := set[fact]; val != nil && strings.TrimSpace(*val) == "" {
			return store.Envelope{}, refuseVehicleAmend("a vehicle's %s cannot be amended to blank — clear it instead, which records that it is not known", fact)
		}
	}
	// Arm 3 — the placeholder guard, at the OTHER typing moment. DR-W7-5
	// relocates it to vehicle CREATION; an amendment is the same typing moment
	// for the same field on the same record, and admitting `walk-in` here would
	// reopen at the amendment door exactly what creation refuses.
	//
	// ⚠ It is asserted through the SAME predicate rather than a second copy of
	// the list, and it keys on NormalizeVehicleID, never on vehicleFoldKey.
	if err := refuseIfPlaceholderNumber(in.Number); err != nil {
		return store.Envelope{}, &VehicleAmendRefusal{Reason: err.Error()}
	}
	clearing := map[string]bool{}
	for _, cf := range in.Clear {
		// Arm 4 — a Clear naming a fact outside the amendable set. This is the
		// ONLY reachable form of "an out-of-set fact": setFields() builds its
		// map from the input struct's own fields, so a Set-side assertion of an
		// out-of-set fact is unrepresentable.
		if !isVehicleAmendableFact(cf) {
			return store.Envelope{}, refuseVehicleAmend("%q is not an amendable vehicle field", cf)
		}
		// Arm 5 — the same fact both set and cleared in one amendment.
		if val := set[cf]; val != nil {
			return store.Envelope{}, refuseVehicleAmend("%q is both set and cleared in one amendment", cf)
		}
		clearing[cf] = true
	}
	// Arm 6 — the amendment would leave the record identifying NOTHING. A
	// vehicle may lose its number and keep its role, or lose its role and keep
	// its number; losing both leaves a row nobody can pick, search for or read
	// on a muster. This is the amendment-side counterpart of the creation
	// refusal, and it is computed over the RESULT rather than over the input:
	// clearing one fact is only safe if the other survives.
	after := *v
	composeVehicle(&after, []vehicleAmendmentRow{{
		VehicleID: in.VehicleID,
		Set:       assertedStrings(set),
		Cleared:   in.Clear,
	}})
	if after.Number == nil && after.Role == nil {
		return store.Envelope{}, refuseVehicleAmend("that would leave the record with neither a number nor a role, identifying nothing — set the other fact in the same amendment, or leave this one as it is")
	}
	if in.SameAs != nil {
		target := *in.SameAs
		// Arm 7 — the trivial self-loop.
		if target == in.VehicleID {
			return store.Envelope{}, refuseVehicleAmend("a record cannot be merged with itself")
		}
		// Arm 8 — kind homogeneity, INVERTED against W3-18 (W7-13): vehicle
		// `same_as` joins VEHICLE records only.
		//
		// ⚠⚠ THE WRONG-KIND ANSWER AND THE UNKNOWN-TARGET ANSWER ARE
		// DIFFERENT SENTENCES, deliberately. Both a person id and an id seeded
		// nowhere make VehicleByID return nil, so a single refusal cannot tell
		// a kind check from an existence check — which is mutant (m7-13b), and
		// vehicle is the first entity with TWO wrong-kind neighbours already in
		// the tree to be confused with.
		tv, err := VehicleByID(st, target)
		if err != nil {
			return store.Envelope{}, fmt.Errorf("domain: amend vehicle: %w", err)
		}
		if tv == nil {
			kind, err := recordKindOf(st, target)
			if err != nil {
				return store.Envelope{}, fmt.Errorf("domain: amend vehicle: %w", err)
			}
			if kind != "" {
				return store.Envelope{}, refuseVehicleAmend("%q is a %s record, not a vehicle; same_as joins vehicle records only", target, kind)
			}
			return store.Envelope{}, refuseVehicleAmend("%q is not a record this post has ever been told about", target)
		}
		// Arm 9 — a standing prior merge. This store already composes an answer
		// for this record; a second, contradictory one is refused here. (Two
		// not-yet-synced posts can still legally author competing merges —
		// record-order composition converges them once replicated.)
		if v.SameAs != nil {
			return store.Envelope{}, refuseVehicleAmend("%s is already merged with %s; clear same_as (the un-merge) first", in.VehicleID, *v.SameAs)
		}
		// Arm 10 — the CYCLE. Would this edge close a loop back to the record
		// being amended? The walk is over the COMPOSED resolution state, never
		// a private snapshot — which is this loader's second consumer.
		//
		// ⚠ This arm is W7-14's own stated premise: the cycle fixture authors
		// on primed scratch edges precisely because edges refuse cycles locally
		// and folds never refuse, so a cycle forms only on the wire. Without
		// this arm that premise is false for vehicle.
		m, err := LoadVehicleMergeMap(st)
		if err != nil {
			return store.Envelope{}, fmt.Errorf("domain: amend vehicle: %w", err)
		}
		cur := target
		for i := 0; i < maxMergeWalk; i++ {
			next, ok := m[cur]
			if !ok {
				break
			}
			if next == in.VehicleID {
				return store.Envelope{}, refuseVehicleAmend("merging %s into %s would close a cycle", in.VehicleID, target)
			}
			if next == cur {
				break
			}
			cur = next
		}
	}

	// The `seen` stamp ([[D-92]], SchemaVersion 11): every fact this
	// amendment asserts or clears, resolved over the record set THIS post
	// holds right now — after every refusal above, before Author.
	// amendmentSeen (contest.go) is the one spelling of what it names;
	// VehicleAmendment has no field a caller could supply it through.
	rows, err := readVehicleAmendments(st)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: amend vehicle: %w", err)
	}
	var facts []string
	for _, fact := range vehicleAmendableFacts {
		if set[fact] != nil {
			facts = append(facts, fact)
		}
	}
	facts = append(facts, in.Clear...)
	seen := amendmentSeen(rows, vehicleMergeMapFromRows(rows), in.VehicleID, facts)

	payload, err := json.Marshal(VehicleAmended{
		Type: evtVehicleAmended, VehicleID: in.VehicleID,
		Number: in.Number, Role: in.Role, SameAs: in.SameAs, Clear: in.Clear,
		Seen: seen,
	})
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: amend vehicle: %w", err)
	}
	env, err := st.Author(siteID, nodeEpoch, payload, occurredAtMS, timeSource)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: amend vehicle: %w", err)
	}
	return env, nil
}

// assertedStrings flattens the edge input's asserted pointers into the
// composition row shape, so arm 6 evaluates the SAME fold the read path will
// run rather than a second hand-written copy of it.
func assertedStrings(set map[string]*string) map[string]string {
	out := map[string]string{}
	for k, v := range set {
		if v != nil {
			out[k] = *v
		}
	}
	return out
}

// recordKindOf names the kind of record an id turns out to be, or "" when this
// post has never been told about it. It exists so arm 8 above can say WHICH
// wrong thing the officer named — the difference between "that is a person" and
// "I have never heard of that" is the difference between a kind check and an
// existence check, and a criterion that cannot separate them is checking one
// property while claiming two.
func recordKindOf(st *store.Store, id string) (string, error) {
	p, err := PersonByID(st, id)
	if err != nil {
		return "", err
	}
	if p != nil {
		return "person", nil
	}
	c, err := CompanyByID(st, id)
	if err != nil {
		return "", err
	}
	if c != nil {
		return "company", nil
	}
	w, err := TradeByID(st, id)
	if err != nil {
		return "", err
	}
	if w != nil {
		return "trade", nil
	}
	return "", nil
}
