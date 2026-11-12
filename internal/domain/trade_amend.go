// trade_amend.go — W8, item 10: the `trade_amended` event, its INSERT-only
// `trade_amendment` side table, the ascending-key composed trade read path,
// the amendment authoring edge and its refusals, the derived trade merge
// snapshot (`same_as`, P-3 option 1a), and THE ONE POSITIONAL RESOLVER.
//
// This is the THIRD clone of W3's mechanism-defining build in
// person_amend.go, after W5d (company) and W7 (vehicle). The load-bearing
// rules are W3's, unchanged, and are not restated here: composition order,
// the edge/fold asymmetry, Clear-composes-as-absent, the derived-per-read
// snapshot, and the never-stored supersession rule. See person_amend.go.
// ⟦SchemaVersion 11, [[D-92]], step 3⟧ The causal `seen` reference is cloned
// here second, after company: payload field, side-table column, edge stamp,
// and every read (composition, the merge snapshot, Outcome/Competitors)
// resolves through resolveContest — the ONE rule, read through contest.go,
// never respelled.
//
// ⚠ DR-7's REFACTOR LICENCE IS NOT SPENT AT W8. The generic "with all five
// classes visible" is NOT built here: the licence's own precondition was
// never met (there are SEVEN classes, not five, and the trip carrier's
// `occupant/<person_id>` vocabulary is a PATTERN whose suffix is data, so it
// cannot be a static []string). This chunk CLONES THE EXISTING PAIR, exactly
// as W5d and W7 did, and the clone cost is accepted and stated: a sixth pair
// of rendering/extraction funcs on carrierRegistration, of the shape already
// there three times. See the ruling at W5-POSTLOCK-AMENDMENTS §42.3.
//
// What differs from the company side, and why — stated here because a clone
// author's first instinct is to copy the whole amendable set across:
//
//   - The amendable set is EXACTLY {name, phone_day, phone_night,
//     phone_shared, same_as}. Three of the five are the POSITIONAL handset
//     numbers, which exist ONLY here: `trade_created` carries no phone key,
//     so a trade's numbers arrive by amendment or not at all.
//   - `retired` does NOT exist, as a column or as a fact: D-30(b) rules
//     there is no retirement for a trade. So there is no retired-trade
//     refusal at the edge either, and A §10.5's clause asking for one is a
//     DEAD ARM.
//   - There is NO `recorded_by` on this carrier ⟦DR-W8-5⟧, while
//     `trade_created` HAS one. The two trade shapes deliberately differ, so
//     a scan forbidding the literal "everywhere on trade" is wrong on the
//     created half. ⚠ The absence encodes the UNRULED branch of §10(a) Q6
//     (nothing records who changed a positional handset number); it is not a
//     ruling against attribution.
//   - There is NO free-text reason/note/comment column (D-49 binding
//     consequence 1, which names "the vehicle/trade carriers W7/W8 clone").
//   - The kind check is trade-sided (W5d-13's shape): `same_as` joins TRADE
//     records only.
//
// ⚠⚠ A LAYOUT RULE THAT LOOKS LIKE STYLE AND IS NOT. Every SQL literal on
// this file naming a phone column is a PACKAGE-LEVEL CONST, and every
// per-fact assignment goes through a PACKAGE-LEVEL table of setters/readers
// rather than being written out field by field inside a function. W8-4(a)
// counts every FUNCTION BODY that can see a shift slot AND the shared slot
// as a POSITIONAL RESOLVER and requires EXACTLY ONE — and it is right to:
// a function that can see all three slots is a function that can silently
// choose between them, which is the second implementation the criterion
// exists to forbid. TradePhone below is the one. Nothing else here may
// name two slots.
package domain

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"sitecontrol/internal/store"
)

// --- event payloads ------------------------------------------------------

const (
	evtTradeCreated = "trade_created"
	evtTradeAmended = "trade_amended"
)

// TradeAmended is the wire payload (type "trade_amended"). Lowercase
// snake_case keys per the measured house convention; `clear` is lowercase on
// the wire (DECIDED at P-3 §7 rev 3). The frozen literals for gate B-2 are
// the type name `trade_amended` and the field names `trade_id`, `name`,
// `phone_day`, `phone_night`, `phone_shared`, `same_as`, plus `clear` — and
// each fact name freezes TWICE over, because the same spellings are
// simultaneously the Clear VALUE vocabulary (S3).
//
// ⟦SchemaVersion 11⟧ `seen` is ADDED ([[D-92]]): the keys of the earlier
// amendments this author had already seen standing for the facts asserted
// here (resolveContest). Stamped by AmendTrade, never by a caller; omitted
// when the author saw nothing standing. Its spelling is person_amended's
// SeenRef — no new literal for B-2 beyond step 2's `seen`.
type TradeAmended struct {
	Type        string    `json:"type"` // "trade_amended"
	TradeID     string    `json:"trade_id"`
	Name        *string   `json:"name,omitempty"`
	PhoneDay    *string   `json:"phone_day,omitempty"`
	PhoneNight  *string   `json:"phone_night,omitempty"`
	PhoneShared *string   `json:"phone_shared,omitempty"`
	SameAs      *string   `json:"same_as,omitempty"`
	Clear       []string  `json:"clear,omitempty"`
	Seen        []SeenRef `json:"seen,omitempty"`
}

// tradeAmendableFacts is the amendable set, in canonical rendering order.
// Fact names ARE the payload field spellings — visible-failure-v1 §4.2's
// convention, cloned.
var tradeAmendableFacts = []string{"name", "phone_day", "phone_night", "phone_shared", "same_as"}

func isTradeAmendableFact(f string) bool {
	for _, k := range tradeAmendableFacts {
		if k == f {
			return true
		}
	}
	return false
}

// TradeAmendment is the authoring-edge input. Value fields are pointers:
// nil = "this amendment does not touch that field", which is a different
// thing from asserting an empty value. Clear lists fact names to UN-SET;
// Clear: ["same_as"] is the un-merge — there is no separate un-merge event
// and no separate merge affordance (DR-6 / S6).
type TradeAmendment struct {
	TradeID     string
	Name        *string
	PhoneDay    *string
	PhoneNight  *string
	PhoneShared *string
	SameAs      *string
	Clear       []string
}

// tradeFactReaders / tradeFactSetters are the per-fact accessors, held at
// PACKAGE LEVEL rather than written out inside setFields and
// applyTradeAmendmentRow.
//
// ⚠ THE PLACEMENT IS THE POINT (see the file comment): a func literal in a
// package-level var initialiser is not inside any function body, so no
// function on this file can see two phone slots at once. That is a mechanical
// guarantee for W8-4(a)'s "implemented ONCE", not a convention someone has to
// remember — and it is checkable by the same census that checks the rule.
var tradeFactReaders = map[string]func(TradeAmendment) *string{
	"name":         func(in TradeAmendment) *string { return in.Name },
	"phone_day":    func(in TradeAmendment) *string { return in.PhoneDay },
	"phone_night":  func(in TradeAmendment) *string { return in.PhoneNight },
	"phone_shared": func(in TradeAmendment) *string { return in.PhoneShared },
	"same_as":      func(in TradeAmendment) *string { return in.SameAs },
}

// tradePayloadSetters fills one wire-payload field. Package-level for the
// same reason tradeFactReaders and tradeFactSetters are.
var tradePayloadSetters = map[string]func(*TradeAmended, *string){
	"name":         func(p *TradeAmended, v *string) { p.Name = v },
	"phone_day":    func(p *TradeAmended, v *string) { p.PhoneDay = v },
	"phone_night":  func(p *TradeAmended, v *string) { p.PhoneNight = v },
	"phone_shared": func(p *TradeAmended, v *string) { p.PhoneShared = v },
	"same_as":      func(p *TradeAmended, v *string) { p.SameAs = v },
}

var tradeFactSetters = map[string]func(*Trade, *string){
	"name": func(tr *Trade, v *string) {
		// `name` is the one non-pointer field, mirroring the company side's.
		// The edge refuses Clear:["name"], so the ONLY way a trade reaches
		// the nil branch nameless is a wire-received clear the fold may not
		// refuse — and the empty string is the only representable absent on
		// a bare string.
		if v == nil {
			tr.Name = ""
			return
		}
		tr.Name = *v
	},
	"phone_day":    func(tr *Trade, v *string) { tr.PhoneDay = v },
	"phone_night":  func(tr *Trade, v *string) { tr.PhoneNight = v },
	"phone_shared": func(tr *Trade, v *string) { tr.PhoneShared = v },
	"same_as":      func(tr *Trade, v *string) { tr.SameAs = v },
}

// setFields returns the asserted (fact → value) pairs of the input. Note
// what this makes UNREPRESENTABLE, because a criterion turns on it: an
// out-of-set fact cannot be ASSERTED at all (the input is a struct and this
// map is built from its own fields), so the out-of-set arm is reachable
// through the `Clear` vocabulary only.
func (in TradeAmendment) setFields() map[string]*string {
	out := make(map[string]*string, len(tradeFactReaders))
	for fact, read := range tradeFactReaders {
		out[fact] = read(in)
	}
	return out
}

// --- THE POSITIONAL RESOLVER — stated once, implemented ONCE -------------

// Shift names which handset a caller wants. The two values are the two
// shifts a trade's positional numbers are kept for.
type Shift string

const (
	ShiftDay   Shift = "day"
	ShiftNight Shift = "night"
)

// TradePhone is `positional(row, shift) = coalesce(phone_<shift>,
// phone_shared)` — W8-4(a), and THE ONLY function in this program permitted
// to select among the three slots.
//
// ⚠⚠ IT IS THE ONLY ONE BY MEASUREMENT, NOT BY CONVENTION. W8-4(a)'s census
// is an AST walk over every production Go file under internal/ and cmd/, and
// it counts EXACTLY ONE selection site — a function whose body names a shift
// slot and the shared slot. A second implementation anywhere, INCLUDING an
// open-coded `if tr.PhoneDay != nil { … } else { … }` at a call site, is the
// mutant, and an open-coded one contains none of the strings a spelling
// filter would look for. ⚠ A TEMPLATE COALESCE COUNTS TOO and is caught by
// the criterion's separate template arm: `{{with .PhoneDay}}…{{else}}…{{end}}`
// is a second implementation an AST walk over Go cannot see.
//
// ⚠ THE SCOPE BOUND, stated so it cannot be read wider: W8-4 asserts the
// resolver EXISTS ONCE and ANSWERS CORRECTLY. It asserts NOTHING about what
// is PRINTED ⟦DR-W8-16⟧ — a caller decides whether to show the number, label
// it, or say nothing at all.
//
// nil out means the trade has neither the shift's own number nor a shared
// one — ABSENT, never an empty string.
func TradePhone(tr Trade, shift Shift) *string {
	if shift == ShiftNight {
		if tr.PhoneNight != nil {
			return tr.PhoneNight
		}
		return tr.PhoneShared
	}
	if tr.PhoneDay != nil {
		return tr.PhoneDay
	}
	return tr.PhoneShared
}

// --- the INSERT-only side table (projection) -----------------------------

// tradeAmendmentSchemaSQL is the side table and its two INSERT-only
// triggers. PACKAGE-LEVEL for the reason the file comment gives.
//
// The declared column set is EXACT: the key triple, the target, one column
// per amendable fact, `cleared`, and — since SchemaVersion 11 ([[D-92]]) —
// `seen` (the author's seen-list, JSON, NULL when empty: a reference written
// at authoring, never a status) — ELEVEN; DR-W8-4 pinned TEN and is re-pinned
// with the field ⟦was: "and `cleared` — TEN, and DR-W8-4 pins them"⟧. NO
// status-shaped column may ever join it (supersession is computed at read,
// never stored); no `retired` column may ever join it (D-30(b)); no
// `recorded_by` (⟦DR-W8-5⟧); and no reason/note column (D-49 bc 1). The two
// triggers make INSERT-only a mechanism rather than a convention.
const tradeAmendmentSchemaSQL = `CREATE TABLE IF NOT EXISTS trade_amendment (
			site_id      TEXT NOT NULL,
			node_epoch   INTEGER NOT NULL,
			seq          INTEGER NOT NULL,
			trade_id     TEXT,
			name         TEXT,
			phone_day    TEXT,
			phone_night  TEXT,
			phone_shared TEXT,
			same_as      TEXT,
			cleared      TEXT,
			seen         TEXT,
			PRIMARY KEY (site_id, node_epoch, seq)
		) STRICT;
		CREATE TRIGGER IF NOT EXISTS trade_amendment_insert_only_update
			BEFORE UPDATE ON trade_amendment BEGIN
			SELECT RAISE(ABORT, 'trade_amendment is INSERT-only: supersession is computed at read, never rewritten in place');
		END;
		CREATE TRIGGER IF NOT EXISTS trade_amendment_insert_only_delete
			BEFORE DELETE ON trade_amendment BEGIN
			SELECT RAISE(ABORT, 'trade_amendment is INSERT-only: amendments are superseded, never merely absent');
		END;`

// tradeAmendmentInsertSQL is the fold's INSERT. Its value placeholders after
// the target are filled from tradeAmendableFacts IN ORDER, so the column list
// and the argument list cannot drift apart without the fact list changing
// too.
const tradeAmendmentInsertSQL = `INSERT INTO trade_amendment
		(site_id, node_epoch, seq, trade_id, name, phone_day, phone_night, phone_shared, same_as, cleared, seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

func tradeAmendmentProjection() store.Projection {
	return store.Projection{
		Name:    "trade_amendment",
		Table:   "trade_amendment",
		OrderBy: "site_id, node_epoch, seq",
		Schema:  tradeAmendmentSchemaSQL,
		Apply:   applyTradeAmended,
	}
}

// applyTradeAmended is the fold. IT NEVER REFUSES (W3-2): a wire-received
// amendment passed its authoring post's edge (or was minted by a divergent
// one) and refusing it here would fork the two logs forever. Cross-kind
// same_as targets, cycle-forming merges, empty names, cleared names, no-ops,
// numbers in shapes this post would not have authored — all fold; edge
// validation happened (or failed to happen) at the AUTHORING post,
// pre-append. Values are coerced tolerantly for the same reason: a payload
// this decoder finds odd must still land.
func applyTradeAmended(tx *sql.Tx, env store.Envelope) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(env.Payload, &raw); err != nil {
		return nil // not an object-shaped payload; not this fold's event
	}
	var typ string
	if err := json.Unmarshal(raw["type"], &typ); err != nil || typ != evtTradeAmended {
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
				return fmt.Errorf("domain: trade_amended fold: cleared: %w", err)
			}
			cleared = string(b)
		}
	}
	// The fact columns are filled FROM tradeAmendableFacts, in order — the
	// same list the composition scans by and the carrier renders by. A
	// hand-written argument list here is the drift this loop removes.
	// `seen` ([[D-92]]) through the one codec, seenColumnValue (contest.go):
	// absent, empty or malformed lands as NULL — the fold never refuses.
	seen, err := seenColumnValue(raw)
	if err != nil {
		return fmt.Errorf("domain: trade_amended fold: seen: %w", err)
	}
	args := []any{env.SiteID, env.NodeEpoch, env.Seq, asString("trade_id")}
	for _, fact := range tradeAmendableFacts {
		args = append(args, asString(fact))
	}
	args = append(args, cleared, seen)
	if _, err := tx.Exec(tradeAmendmentInsertSQL, args...); err != nil {
		return fmt.Errorf("domain: trade_amended fold: insert %v: %w", env.Key(), err)
	}
	return nil
}

// --- reading the side table (the composition's own SQL) ------------------

// tradeAmendmentRow is one decoded side-table row: the amendment's key, its
// target, its asserted facts, its cleared facts, and (SchemaVersion 11,
// [[D-92]]) the keys its author had already seen standing — the dominance
// input to resolveContest. A row with no Seen competes on key order alone.
type tradeAmendmentRow struct {
	Key     store.Key
	TradeID string
	Set     map[string]string
	Cleared []string
	Seen    []store.Key
}

func (r tradeAmendmentRow) asserts(fact string) bool {
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

// The contestant / amendmentRow surface (contest.go).
func (r tradeAmendmentRow) contestKey() store.Key    { return r.Key }
func (r tradeAmendmentRow) contestSeen() []store.Key { return r.Seen }
func (r tradeAmendmentRow) targetID() string         { return r.TradeID }

// The trade composition read, in the house's own two-half shape (the M-1
// fix): a shared SELECT half, a shared `WHERE trade_id = ?`, and THE ONE
// ordering spelling — which is not respelled here at all.
//
// ⚠ personAmendmentCompositionOrderBy is REUSED VERBATIM rather than cloned,
// as company_amend.go and vehicle_amend.go reuse it. Sharing the constant
// makes "one canonical ORDER BY spelling" structural instead of conventional:
// a drift would have to change the spelling for EVERY amendment side table at
// once. The name reads person-sided because W3 declared it; renaming it is
// W3's code, not this chunk's.
//
// ⚠ THESE ARE PACKAGE-LEVEL CONSTS AND MUST STAY SO. W8-18(b)'s respecified
// ORDER-BY instrument resolves *types.Const values through go/types; a
// composition SQL held in a `var`, in a function-local const, or assembled by
// Sprintf is OUTSIDE that instrument's reach, and a build that moves it there
// has left the scan — which is a FINDING, never a pass.
const (
	tradeAmendmentCompositionSelect = `SELECT site_id, node_epoch, seq, trade_id,
	name, phone_day, phone_night, phone_shared, same_as, cleared, seen
	FROM trade_amendment`

	tradeAmendmentCompositionSQL = tradeAmendmentCompositionSelect +
		personAmendmentCompositionOrderBy

	// tradeAmendmentCompositionByTradeSQL is the SAME read scoped to ONE
	// trade (the M-1 shape, cloned): TradeByID must not pay a whole-table
	// decode per lookup — the gate's check-in path and the muster's grouping
	// both resolve trades per row.
	tradeAmendmentCompositionByTradeSQL = tradeAmendmentCompositionSelect +
		` WHERE trade_id = ?` + personAmendmentCompositionOrderBy
)

// readTradeAmendments reads EVERY trade amendment row in ascending key order
// — the substrate of composition, Outcome and the derived merge snapshot, so
// all three answer from the same record set in the same order.
func readTradeAmendments(st *store.Store) ([]tradeAmendmentRow, error) {
	return queryTradeAmendments(st, tradeAmendmentCompositionSQL)
}

// readTradeAmendmentsFor reads ONLY the rows targeting tradeID — same
// columns, same ascending-key order, both spellings shared with the read
// above and never respelled.
func readTradeAmendmentsFor(st *store.Store, tradeID string) ([]tradeAmendmentRow, error) {
	return queryTradeAmendments(st, tradeAmendmentCompositionByTradeSQL, tradeID)
}

// queryTradeAmendments is the shared decode loop under both reads. It rides
// store.LocalTx, the exported *sql.Tx seam, for the reason
// readPersonAmendments states: this query IS the SQL half of the one ordering
// spec and must exist here as a literal spelling, where the drift scan that
// guards it can see it.
func queryTradeAmendments(st *store.Store, query string, args ...any) ([]tradeAmendmentRow, error) {
	var out []tradeAmendmentRow
	err := st.LocalRead(func(tx *sql.Tx) error {
		rows, err := tx.Query(query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				k       store.Key
				tradeID sql.NullString
				vals    = make([]sql.NullString, len(tradeAmendableFacts))
				cleared sql.NullString
				seen    sql.NullString
			)
			scan := []any{&k.SiteID, &k.NodeEpoch, &k.Seq, &tradeID}
			for i := range vals {
				scan = append(scan, &vals[i])
			}
			scan = append(scan, &cleared, &seen)
			if err := rows.Scan(scan...); err != nil {
				return err
			}
			r := tradeAmendmentRow{Key: k, TradeID: tradeID.String, Set: map[string]string{}}
			for i, fact := range tradeAmendableFacts {
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
		return nil, fmt.Errorf("domain: read trade amendments: %w", err)
	}
	return out, nil
}

// --- composition (the ONE composed read path) ----------------------------

// composeTrade folds the amendment rows targeting ONE trade into its
// composed record, fact by fact: the row resolveContest names as the winner
// of each fact's target-scope contest supplies that fact — a set value
// lands, a clear makes the fact ABSENT, never empty-string.
//
// ⚠ It names no field: every assignment goes through tradeFactSetters, for
// the reason the file comment gives (W8-4(a): no function body on this file
// may see two phone slots). It iterates the amendable set, so a divergent
// peer's out-of-set Clear token never reaches a setter — ignored, as before.
//
// ⟦SchemaVersion 11, [[D-92]]⟧ ⟦was: applyTradeAmendmentRow — one row folded
// at a time in key order, "later keys win per fact"⟧. A per-row fold cannot
// honour dominance, because whether a row's value stands depends on rows
// that come AFTER it in key order; under a record set with no `seen`
// anywhere this is exactly the pre-11 fold.
func composeTrade(tr *Trade, rows []tradeAmendmentRow) {
	for _, fact := range tradeAmendableFacts {
		contest := rowsAsserting(rows, fact)
		if len(contest) == 0 {
			continue
		}
		winner, _ := resolveContest(contest)
		var w tradeAmendmentRow
		for _, r := range contest {
			if r.Key == winner {
				w = r
				break
			}
		}
		if v, ok := w.Set[fact]; ok {
			val := v
			tradeFactSetters[fact](tr, &val)
		} else {
			tradeFactSetters[fact](tr, nil) // the winner cleared it
		}
	}
}

// composeTrades applies every amendment over the base created-state rows,
// one composed trade at a time (composeTrade). Rows amending a trade this
// store has no base row for compose to nothing here (the record set keeps
// them; a reader cannot show a trade it has never been told exists).
func composeTrades(base []Trade, amendments []tradeAmendmentRow) []Trade {
	byID := make(map[string]*Trade, len(base))
	for i := range base {
		byID[base[i].TradeID] = &base[i]
	}
	perTrade := map[string][]tradeAmendmentRow{}
	for _, r := range amendments {
		perTrade[r.TradeID] = append(perTrade[r.TradeID], r)
	}
	for id, rows := range perTrade {
		if tr, ok := byID[id]; ok {
			composeTrade(tr, rows)
		}
	}
	return base
}

// --- the derived merge-resolution snapshot (P-3 §5) ----------------------

// LoadTradeMergeMap loads the per-read derived trade resolution snapshot over
// the surviving AliasMap read shape, the trade analogue of
// LoadCompanyMergeMap. It is a pure projection of composed state — each
// trade's EFFECTIVE same_as (the winner of that trade's same_as contest under
// resolveContest: causal first, then record order; a winning Clear removes
// the edge ⟦was: "last in record order"⟧)
// — plus the one derived rule the snapshot layer owns: wire-formed cycles
// COLLAPSE to one pinned canonical representative, the target of the
// cycle-forming assertion LATEST IN RECORD ORDER over the composed cycle set.
//
// ⚠ It is DERIVED PER READ and caches NOTHING.
//
// ⚠⚠ THIS IS WHAT REPLACES THE DELETED SHARED REFERENCE-MERGE MECHANISM FOR
// TRADE, and P-3 §5 rules the succession explicitly: only that mechanism
// dies; "the AliasMap shape and hop-capped Resolve survive as the read API
// over a new loader". A merged-away trade's ROW SURVIVES — `same_as` is a per-field
// assertion, never a deletion — which is why the authoring edge's question is
// "is this id a `trade` row?" and the RESOLUTION happens at the read (W8-19).
func LoadTradeMergeMap(st *store.Store) (AliasMap, error) {
	rows, err := readTradeAmendments(st)
	if err != nil {
		return nil, fmt.Errorf("domain: load trade merge map: %w", err)
	}
	return tradeMergeMapFromRows(rows), nil
}

func tradeMergeMapFromRows(rows []tradeAmendmentRow) AliasMap {
	// ⟦SchemaVersion 11, [[D-92]]⟧ The effective edge per trade is the winner
	// of that trade's same_as contest under resolveContest — the ONE rule —
	// and a winning Clear is no edge; only the effective SET edges reach
	// mergeMapFromEdges, whose record-order selection is then vacuous (one
	// edge per source) and whose cycle collapse is unchanged: a cycle's edges
	// belong to DIFFERENT trades' contests, and dominance is defined within
	// one fact's contest, never across them. ⟦was: every set/clear edge
	// handed over, "last in record order wins"⟧
	perTrade := map[string][]tradeAmendmentRow{}
	var order []string
	for _, r := range rows {
		if !r.asserts("same_as") {
			continue
		}
		if _, ok := perTrade[r.TradeID]; !ok {
			order = append(order, r.TradeID)
		}
		perTrade[r.TradeID] = append(perTrade[r.TradeID], r)
	}
	edges := make([]mergeEdge, 0, len(order))
	for _, id := range order {
		contest := perTrade[id]
		winner, _ := resolveContest(contest)
		for _, r := range contest {
			if r.Key != winner {
				continue
			}
			if v, ok := r.Set["same_as"]; ok {
				edges = append(edges, mergeEdge{Source: id, Target: v, Key: r.Key})
			}
			break
		}
	}
	return mergeMapFromEdges(edges)
}

// --- the authoring edge --------------------------------------------------

// TradeAmendRefusal is an EDGE REFUSAL, distinguished from an internal
// failure BY TYPE rather than by reading an error string — the split W5d
// introduced and W7 cloned. A refusal's text is AUTHORED COPY and belongs on
// the page; an internal error's text is not and does not.
type TradeAmendRefusal struct {
	// Reason is the officer-facing sentence, without prefix or suffix.
	Reason string
}

func (e *TradeAmendRefusal) Error() string {
	return "domain: amend trade: refused — " + e.Reason + "; nothing was recorded"
}

func refuseTradeAmend(format string, args ...any) error {
	return &TradeAmendRefusal{Reason: fmt.Sprintf(format, args...)}
}

// TradeAmendInputError is the edge's THIRD error class: the officer named a
// record this post cannot act on. It is neither a fact-domain refusal nor an
// internal fault, and collapsing it into either one tells an officer
// something false — a mistyped id answered with "this post could not complete
// the amendment, radio it through" is a lie about a post that worked
// perfectly. See CompanyAmendInputError for the measured incident this type
// exists because of.
type TradeAmendInputError struct {
	Reason string
}

func (e *TradeAmendInputError) Error() string {
	return "domain: amend trade: " + e.Reason + "; nothing was recorded"
}

func refuseTradeInput(format string, args ...any) error {
	return &TradeAmendInputError{Reason: fmt.Sprintf(format, args...)}
}

// TradeRenameRefusal is the edge's [[D-82]] (2026-09-05) refusal: the
// corrected name is already ANOTHER trade's name on this site's list, under
// the same fold creating uses ([[D-76]] sameTradeName — case and
// leading/trailing space). Two trades with one name are two identical
// headings on the evacuation muster sheet; before D-82 this edge had no
// duplicate check at all and renamed Catering onto a listed Pipefitter with
// err = nil.
//
// It is a FACT-DOMAIN refusal that also carries the holder AS DATA: Unwrap
// yields the *TradeAmendRefusal that class is matched by, so web/trade.go's
// three-way errors.As partition lands it on the refusal arm with no fourth
// arm, and a caller that wants to name the trade to pick reads Existing by
// TYPE, never by parsing this text.
//
// ⛔ REFUSED, NEVER MERGED. Merging two trades is `same_as`, a separate
// amendment with its own ruling ([[D-30]](a)); doing it implicitly through a
// rename would let an officer merge two crews by mistyping a name.
type TradeRenameRefusal struct {
	// Renamed is the trade the officer was renaming, as composed.
	Renamed Trade
	// Typed is the corrected name as asserted.
	Typed string
	// Existing is the OTHER trade already holding that name, as composed —
	// Existing.Name is the ON-LIST spelling, the row the officer should pick.
	Existing Trade
}

// Reason is the officer-facing sentence, without prefix or suffix — the
// same contract as TradeAmendRefusal.Reason.
func (e *TradeRenameRefusal) Reason() string {
	return fmt.Sprintf("trade %s is already named %q; pick it instead of renaming %q onto that name — two trades cannot share one muster heading (merging them is the separate same_as amendment)",
		e.Existing.TradeID, e.Existing.Name, e.Renamed.Name)
}

func (e *TradeRenameRefusal) Error() string {
	return "domain: amend trade: refused — " + e.Reason() + "; nothing was recorded"
}

// Unwrap places the rename refusal in the fact-domain class BY TYPE: an
// errors.As for *TradeAmendRefusal walks to it.
func (e *TradeRenameRefusal) Unwrap() error {
	return &TradeAmendRefusal{Reason: e.Reason()}
}

// AmendTrade authors one trade_amended event. EVERY refusal below is
// PRE-APPEND (W3-2): a refusal returns before Author is called, so the log
// length is unchanged and no envelope exists. These checks live at this edge
// and ONLY at this edge — the fold never refuses (see applyTradeAmended).
//
// ⚠ THERE IS NO RETIRED-TRADE ARM, and its absence is deliberate rather than
// forgotten: D-30(b) rules that a trade is never retired, so A §10.5's "the
// authoring edge refuses a RETIRED trade_id" has no subject.
func AmendTrade(st *store.Store, siteID string, nodeEpoch int64, in TradeAmendment, occurredAtMS int64, timeSource string) (store.Envelope, error) {
	if in.TradeID == "" {
		return store.Envelope{}, refuseTradeInput("a trade id is required")
	}
	tr, err := TradeByID(st, in.TradeID)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: amend trade: %w", err)
	}
	if tr == nil {
		return store.Envelope{}, refuseTradeInput("%q is not a trade on this post", in.TradeID)
	}

	set := in.setFields()
	assertedCount := 0
	for _, v := range set {
		if v != nil {
			assertedCount++
		}
	}
	// Arm 1 — the no-op refusal: an amendment that asserts nothing and clears
	// nothing records nothing, and recording nothing must be refused, not
	// minted.
	if assertedCount == 0 && len(in.Clear) == 0 {
		return store.Envelope{}, refuseTradeAmend("the amendment changes nothing (no field set, nothing cleared)")
	}
	// Arm 2 — name-to-empty. A trade always has a name (trade.name is TEXT
	// NOT NULL), and blanking it is refused here exactly as clearing it is
	// below.
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return store.Envelope{}, refuseTradeAmend("a trade's name cannot be amended to empty")
	}
	for _, cf := range in.Clear {
		// Arm 3 — Clear:["name"], mirroring the person and company rules.
		if cf == "name" {
			return store.Envelope{}, refuseTradeAmend("a trade's name cannot be cleared")
		}
		// Arm 4 — a Clear naming a fact outside the amendable set. This is
		// the ONLY reachable form of "an out-of-set fact": setFields() builds
		// its map from the input struct's own fields, so a Set-side assertion
		// of an out-of-set fact is unrepresentable.
		if !isTradeAmendableFact(cf) {
			return store.Envelope{}, refuseTradeAmend("%q is not an amendable trade field", cf)
		}
		// Arm 5 — the same fact both set and cleared in one amendment.
		if v := set[cf]; v != nil {
			return store.Envelope{}, refuseTradeAmend("%q is both set and cleared in one amendment", cf)
		}
	}
	if in.SameAs != nil {
		target := *in.SameAs
		// Arm 6 — the trivial self-loop.
		if target == in.TradeID {
			return store.Envelope{}, refuseTradeAmend("a record cannot be merged with itself")
		}
		// Arm 7 — kind homogeneity (W5d-13's shape, trade-sided): `same_as`
		// joins TRADE records only. The id-kind-agnostic mechanism this
		// replaces never checked this at all (P-3 §2 fact 5).
		tt, err := TradeByID(st, target)
		if err != nil {
			return store.Envelope{}, fmt.Errorf("domain: amend trade: %w", err)
		}
		if tt == nil {
			return store.Envelope{}, refuseTradeAmend("%q is not a trade record; same_as joins trade records only", target)
		}
		// Arm 8 — a standing prior merge. This store already composes an
		// answer for this record; a second, contradictory one is refused
		// here. (Two not-yet-synced posts can still legally author competing
		// merges — record-order composition converges them once replicated.)
		if tr.SameAs != nil {
			return store.Envelope{}, refuseTradeAmend("%s is already merged with %s; clear same_as (the un-merge) first", in.TradeID, *tr.SameAs)
		}
		// Arm 9 — the CYCLE. Would this edge close a loop back to the record
		// being amended? The walk is over the COMPOSED resolution state,
		// never a private snapshot.
		m, err := LoadTradeMergeMap(st)
		if err != nil {
			return store.Envelope{}, fmt.Errorf("domain: amend trade: %w", err)
		}
		cur := target
		for i := 0; i < maxMergeWalk; i++ {
			next, ok := m[cur]
			if !ok {
				break
			}
			if next == in.TradeID {
				return store.Envelope{}, refuseTradeAmend("merging %s into %s would close a cycle", in.TradeID, target)
			}
			if next == cur {
				break
			}
			cur = next
		}
	}
	// Arm 10 — [[D-82]] the rename collision: a corrected name that is
	// already ANOTHER trade's name, under the fold creating uses ([[D-76]]
	// sameTradeName). Compared against the COMPOSED list — the headings the
	// muster prints — because a name that itself arrived by rename exists
	// only there, and a name freed by a rename is free only there; and never
	// against the trade being renamed, which holds its own name and every
	// case variant of it. Refused, never merged (see TradeRenameRefusal).
	if in.Name != nil {
		listed, err := Trades(st)
		if err != nil {
			return store.Envelope{}, fmt.Errorf("domain: amend trade: %w", err)
		}
		for _, other := range listed {
			if other.TradeID == in.TradeID || !sameTradeName(other.Name, *in.Name) {
				continue
			}
			return store.Envelope{}, &TradeRenameRefusal{Renamed: *tr, Typed: *in.Name, Existing: other}
		}
	}

	// ⚠ THE PAYLOAD IS FILLED THROUGH tradePayloadSetters, NOT FIELD BY
	// FIELD, and the reason is the file comment's: written out, this
	// statement would name a shift slot AND the shared slot inside one
	// function body, which is a SECOND POSITIONAL RESOLVER by W8-4(a)'s
	// census — and the census is right that a function which can see all
	// three slots can choose between them.
	// The `seen` stamp ([[D-92]], SchemaVersion 11): every fact this
	// amendment asserts or clears, resolved over the record set THIS post
	// holds right now — after every refusal above, before Author.
	// amendmentSeen (contest.go) is the one spelling of what it names;
	// TradeAmendment has no field a caller could supply it through.
	rows, err := readTradeAmendments(st)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: amend trade: %w", err)
	}
	var facts []string
	for _, fact := range tradeAmendableFacts {
		if set[fact] != nil {
			facts = append(facts, fact)
		}
	}
	facts = append(facts, in.Clear...)
	seen := amendmentSeen(rows, tradeMergeMapFromRows(rows), in.TradeID, facts)

	p := TradeAmended{Type: evtTradeAmended, TradeID: in.TradeID, Clear: in.Clear, Seen: seen}
	for _, fact := range tradeAmendableFacts {
		if v := set[fact]; v != nil {
			tradePayloadSetters[fact](&p, v)
		}
	}
	payload, err := json.Marshal(p)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: amend trade: %w", err)
	}
	env, err := st.Author(siteID, nodeEpoch, payload, occurredAtMS, timeSource)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: amend trade: %w", err)
	}
	return env, nil
}

// tradeFactResolutions is the trade class's pass — the sixth class pair in
// use. Same contest scope, same order, over the trade side table: the merge
// pointer is contested among assertions targeting the SAME record (resolving
// it first would erase the fact under judgement), and the value facts are
// contested across the assertion sets a merge UNITES.
func tradeFactResolutions(st *store.Store, k store.Key) ([]factResolution, bool, error) {
	rows, err := readTradeAmendments(st)
	if err != nil {
		return nil, false, fmt.Errorf("domain: outcome: %w", err)
	}
	var target *tradeAmendmentRow
	for i := range rows {
		if rows[i].Key == k {
			target = &rows[i]
			break
		}
	}
	if target == nil {
		return nil, false, nil
	}
	reg := carrierRegistry[evtTradeAmended]
	m := tradeMergeMapFromRows(rows)
	rep := m.Resolve(reg.TradeTargetID(*target))

	var out []factResolution
	for _, fact := range reg.Facts {
		if !target.asserts(fact) {
			continue
		}
		// ⟦SchemaVersion 11, [[D-92]]⟧ The scope is amendmentContest's
		// (same_as among assertions on the same record, value facts across
		// the merged group) and the winner is resolveContest's — the ONE
		// rule. ⟦was: "the LAST assertion of this fact in ascending key
		// order over the contested set"⟧
		contest := amendmentContest(rows, m, rep, target.TradeID, fact)
		winner, _ := resolveContest(contest)
		res := factResolution{Fact: fact, Asserted: reg.AssertedTrade(*target, fact), Winner: winner}
		for _, r := range contest {
			if r.Key != k {
				res.Competitors = append(res.Competitors, r.Key)
			}
		}
		out = append(out, res)
	}
	return out, true, nil
}
