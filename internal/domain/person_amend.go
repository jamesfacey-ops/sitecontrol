// person_amend.go — item 4: the `person_amended` event, its INSERT-only
// `person_amendment` side table, the ascending-key composed person read
// path, the amendment authoring edge and its refusals, and the derived
// merge-resolution snapshot (`same_as`, P-3 option 1a). This is W3's
// mechanism-defining build — W5d (company), W7 (vehicle) and W8 (trade)
// clone this shape (WINDOW-BUILD-PLAN-v1 §3 W3, criteria LOCKED
// 2026-08-27; design/P3-merge-mechanism.md rev 3).
//
// The load-bearing rules, in one place:
//
//   - Composition is CAUSAL, then ascending (site_id, node_epoch, seq) —
//     DETERMINISTIC, never chronological, never arrival order, never rowid
//     (W3-1/W3-5 as RE-LOCKED at SchemaVersion 11 under [[D-92]]). Every
//     amendment carries `seen`: the keys of the assertions its author had
//     ALREADY SEEN standing for the facts it asserts. Per fact, an
//     assertion named by another contestant's `seen` is DOMINATED and never
//     wins; among the undominated the greatest key wins. So a correction
//     made in knowledge of an earlier one wins regardless of site_id, and
//     site_id breaks ties ONLY between genuinely concurrent edits — the
//     partition case, where neither author saw the other. The clock stays
//     out of it entirely (the design guarantees a wrong-clock boot per post
//     per season). ⟦was: "Composition is ascending (site_id, node_epoch,
//     seq)" — [[D-28]] Part B (fixed order, one post always wins) is
//     REVERSED by [[D-92]]; re-ruled, not silently edited⟧. The rule is
//     spelled ONCE, in resolveContest below; composition, Outcome,
//     Competitors and the merge snapshot all read it from there — and, as
//     of step 3 of the increment, so do the six carriers that clone this
//     file: resolveContest is generic over contest.go's `contestant`, so
//     the clones gain the FIELD and read the RULE from here. The SQL
//     half of the one ordering spec is still the single canonical
//     `ORDER BY site_id, node_epoch, seq` spelling below; the Go half is
//     store.Key.Less (envelope.go), and W3-11's T-A oracle proves the two
//     agree.
//   - Edge refusals are PRE-APPEND input validation and live ONLY here in
//     AmendPerson; the fold NEVER refuses a wire-received amendment (W3-2)
//     — refusing at fold time would fork the two posts' logs permanently.
//   - Clear un-sets: a cleared field composes as ABSENT (nil), never as
//     empty-string (W3-3). Clear: ["same_as"] IS the un-merge (P-3 §5) —
//     no separate un-merge event type exists.
//   - "Merged?" is answered ONLY by the composed entity state; the
//     resolution snapshot (LoadPersonMergeMap) is derived per read from
//     the composed rows and holds no state of its own — a cached snapshot
//     is the second mechanism P-3 exists to prevent (W3-4/m9).
//   - Supersession is NEVER stored: the side table carries no
//     status-shaped column and is INSERT-only by trigger (W3-13/W3-16;
//     visible-failure-v1 §4.3 rule 2). `seen` is not a status: it is an
//     immutable reference WRITTEN AT AUTHORING recording what the author
//     saw, never who won — who wins is still computed at every read.
//
// The shared legacy reference-merge mechanism stayed alive in this tree for
// the muster's company/trade grouping until W5d and W8 re-pointed it, and W8
// deleted it — the schedule is stated once, at P-3 §5, and never restated
// here. Person resolution stopped consulting it as of this file.
package domain

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"sitecontrol/internal/store"
)

// --- event payload -------------------------------------------------------

// PersonAmended is the wire payload (type "person_amended"). Lowercase
// snake_case keys per the measured house convention; `clear` is lowercase
// on the wire (DECIDED at P-3 §7 rev 3). The amendable set is CONSOLIDATED
// §2 item 4's, as locked at W3 and amended at SchemaVersion 11: phone,
// name, last_name, first_name, company_id, trade_id, role, position,
// retired, same_as — NEVER person_id (the target, not an amendable fact),
// site_id or recorded_by (D-5: origin is structural; amendment carriers
// carry no officer name).
//
// ⟦SchemaVersion 11⟧ `pad_oriented` is STRUCK from this set, not migrated
// ([[D-88]] + ERRATA, [[D-91]]): it was one flag on the ENROLLING site's
// person row, named after a site, and per-site orientation makes that row
// structurally wrong — orientation is minted by the ORIENTING post as its
// own record, in a later stage of this same increment. Nothing real was
// ever recorded under the old key ([[D-29]], [[D-92]]), so there is nothing
// to carry across. ⟦was: "…role, pad_oriented, position…"⟧
//
// ⟦SchemaVersion 11⟧ `seen` is ADDED ([[D-92]]): the keys of the earlier
// amendments this author had already seen standing for the facts asserted
// here. It is what lets a correction made in knowledge of an earlier one
// WIN regardless of site_id (see resolveContest). Stamped by AmendPerson,
// never by a caller; omitted when the author saw nothing standing.
type PersonAmended struct {
	Type      string    `json:"type"` // "person_amended"
	PersonID  string    `json:"person_id"`
	Name      *string   `json:"name,omitempty"`
	LastName  *string   `json:"last_name,omitempty"`
	FirstName *string   `json:"first_name,omitempty"`
	Phone     *string   `json:"phone,omitempty"`
	CompanyID *string   `json:"company_id,omitempty"`
	TradeID   *string   `json:"trade_id,omitempty"`
	Role      *string   `json:"role,omitempty"`
	Position  *string   `json:"position,omitempty"`
	Retired   *string   `json:"retired,omitempty"`
	SameAs    *string   `json:"same_as,omitempty"`
	Clear     []string  `json:"clear,omitempty"`
	Seen      []SeenRef `json:"seen,omitempty"`
}

const evtPersonAmended = "person_amended"

// SeenRef is one entry of an amendment's `seen` list ([[D-92]]): the key of
// an earlier amendment row that the author had ALREADY SEEN as the standing
// assertion of a fact this amendment asserts or clears. Its wire keys are
// the envelope's own three (`site_id`, `node_epoch`, `seq`) so the wire
// gains exactly ONE new literal, `seen`. It is a reference, not a status:
// written once at authoring, replicated verbatim, never recomputed.
type SeenRef struct {
	SiteID    string `json:"site_id"`
	NodeEpoch int64  `json:"node_epoch"`
	Seq       int64  `json:"seq"`
}

func seenRefOf(k store.Key) SeenRef {
	return SeenRef{SiteID: k.SiteID, NodeEpoch: k.NodeEpoch, Seq: k.Seq}
}

func (r SeenRef) key() store.Key {
	return store.Key{SiteID: r.SiteID, NodeEpoch: r.NodeEpoch, Seq: r.Seq}
}

// personAmendableFacts is the amendable set, in canonical rendering order.
// Fact names ARE the payload field spellings (the Outcome vocabulary,
// visible-failure-v1 §4.2's convention).
var personAmendableFacts = []string{
	"name", "last_name", "first_name", "phone", "company_id",
	"trade_id", "role", "position", "retired", "same_as",
}

func isAmendableFact(f string) bool {
	for _, k := range personAmendableFacts {
		if k == f {
			return true
		}
	}
	return false
}

// PersonAmendment is the authoring-edge input (SEAM W3-S-INPUT). All value
// fields are pointers: nil = "this amendment does not touch that field",
// which is a different thing from asserting an empty value. Clear lists
// fact names to UN-SET; Clear: ["same_as"] is the un-merge.
type PersonAmendment struct {
	PersonID  string
	Name      *string
	LastName  *string
	FirstName *string
	Phone     *string
	CompanyID *string
	TradeID   *string
	Role      *string
	Position  *string
	Retired   *string
	SameAs    *string
	Clear     []string
}

// setFields returns the asserted (fact → value) pairs of the input.
func (in PersonAmendment) setFields() map[string]*string {
	return map[string]*string{
		"name": in.Name, "last_name": in.LastName, "first_name": in.FirstName,
		"phone": in.Phone, "company_id": in.CompanyID, "trade_id": in.TradeID,
		"role": in.Role, "position": in.Position,
		"retired": in.Retired, "same_as": in.SameAs,
	}
}

// --- the INSERT-only side table (projection) -----------------------------

// personAmendmentProjection registers the side table the fold maintains and
// the composition reads. Keyed by the amendment event's OWN key — the one
// ordering spec's subject. The declared column set is EXACT (W3-13): the
// key triple, the target, one column per amendable fact, `cleared` (the
// Clear list, JSON-encoded) and — since SchemaVersion 11 ([[D-92]]) —
// `seen` (the author's seen-list, JSON-encoded, NULL when empty). NO
// status-shaped column may ever join this table: supersession is computed
// at read, never stored (§4.3 rule 2) — the two triggers make INSERT-only a
// mechanism rather than a convention. `seen` passes that rule on its
// merits: it records what the AUTHOR saw at authoring, never who WON, and
// it is as immutable as the payload it came from.
//
// ⟦SchemaVersion 11⟧ `pad_oriented` is STRUCK from this table (see
// PersonAmended). The shape change rides the 10 → 11 increment
// (store.go's bump log) — the v11 log starts empty, so nothing migrates.
func personAmendmentProjection() store.Projection {
	return store.Projection{
		Name:    "person_amendment",
		Table:   "person_amendment",
		OrderBy: "site_id, node_epoch, seq",
		Schema: `CREATE TABLE IF NOT EXISTS person_amendment (
			site_id      TEXT NOT NULL,
			node_epoch   INTEGER NOT NULL,
			seq          INTEGER NOT NULL,
			person_id    TEXT,
			name         TEXT,
			last_name    TEXT,
			first_name   TEXT,
			phone        TEXT,
			company_id   TEXT,
			trade_id     TEXT,
			role         TEXT,
			position     TEXT,
			retired      TEXT,
			same_as      TEXT,
			cleared      TEXT,
			seen         TEXT,
			PRIMARY KEY (site_id, node_epoch, seq)
		) STRICT;
		CREATE TRIGGER IF NOT EXISTS person_amendment_insert_only_update
			BEFORE UPDATE ON person_amendment BEGIN
			SELECT RAISE(ABORT, 'person_amendment is INSERT-only: supersession is computed at read, never rewritten in place');
		END;
		CREATE TRIGGER IF NOT EXISTS person_amendment_insert_only_delete
			BEFORE DELETE ON person_amendment BEGIN
			SELECT RAISE(ABORT, 'person_amendment is INSERT-only: amendments are superseded, never merely absent');
		END;`,
		Apply: applyPersonAmended,
	}
}

// applyPersonAmended is the fold. IT NEVER REFUSES (W3-2): a wire-received
// amendment passed its authoring post's edge (or was minted by a divergent
// one) and refusing it here would fork the two logs forever. Unknown trade
// ids, cycle-forming merges, cross-kind targets, empty names, no-ops — all
// fold; edge validation happened (or failed to happen) at the AUTHORING
// post, pre-append. Values are coerced tolerantly for the same reason: a
// payload this decoder finds odd must still land.
func applyPersonAmended(tx *sql.Tx, env store.Envelope) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(env.Payload, &raw); err != nil {
		return nil // not an object-shaped payload; not this fold's event
	}
	var typ string
	if err := json.Unmarshal(raw["type"], &typ); err != nil || typ != evtPersonAmended {
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
				return fmt.Errorf("domain: person_amended fold: cleared: %w", err)
			}
			cleared = string(b)
		}
	}
	// `seen` ([[D-92]]) lands on the same tolerant terms as `clear`: a
	// well-formed list is stored re-encoded in canonical form; anything
	// else — absent, empty, malformed — lands as NULL, which composition
	// reads as "this author saw nothing standing" (the pre-11 meaning of
	// every row, and the genuinely-concurrent case). A stale key in the
	// object (a `pad_oriented` from a divergent peer, say) is simply not
	// asked for and is dropped: the fold never refuses. The codec is
	// seenColumnValue (contest.go), one spelling for every side table.
	seen, err := seenColumnValue(raw)
	if err != nil {
		return fmt.Errorf("domain: person_amended fold: seen: %w", err)
	}
	_, err = tx.Exec(`INSERT INTO person_amendment
		(site_id, node_epoch, seq, person_id, name, last_name, first_name, phone,
		 company_id, trade_id, role, position, retired, same_as, cleared, seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		env.SiteID, env.NodeEpoch, env.Seq,
		asString("person_id"), asString("name"), asString("last_name"), asString("first_name"),
		asString("phone"), asString("company_id"), asString("trade_id"), asString("role"),
		asString("position"), asString("retired"), asString("same_as"),
		cleared, seen)
	if err != nil {
		return fmt.Errorf("domain: person_amended fold: insert %v: %w", env.Key(), err)
	}
	return nil
}

// --- reading the side table (the composition's own SQL) ------------------

// personAmendmentRow is one decoded side-table row: the amendment's key,
// its target, its asserted facts, its cleared facts, and (SchemaVersion 11,
// [[D-92]]) the keys its author had already seen standing — the dominance
// input to resolveContest. A row with no Seen is one whose author saw
// nothing standing (or a pre-11-shaped row from a divergent peer), and it
// competes on key order alone.
type personAmendmentRow struct {
	Key      store.Key
	PersonID string
	Set      map[string]string
	Cleared  []string
	Seen     []store.Key
}

func (r personAmendmentRow) asserts(fact string) bool {
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

// The contestant / amendmentRow surface (contest.go): what the ONE rule and
// the shared scope/stamp helpers read off a row.
func (r personAmendmentRow) contestKey() store.Key    { return r.Key }
func (r personAmendmentRow) contestSeen() []store.Key { return r.Seen }
func (r personAmendmentRow) targetID() string         { return r.PersonID }

// personAmendmentCompositionSQL is THE one composition read, carrying THE
// one SQL ordering spelling (W3-11): `ORDER BY site_id, node_epoch, seq`,
// epoch-major, seq last — the same total order store.Key.Less defines, and
// deliberately spelled once so a second spelling cannot drift from it.
// Composed from two halves (M-1 fix) so the single-person variant below
// SHARES the column list and the ordering spelling rather than duplicating
// either: the WHERE narrows the row set, nothing else differs.
const (
	personAmendmentCompositionSelect = `SELECT site_id, node_epoch, seq, person_id,
	name, last_name, first_name, phone, company_id, trade_id, role,
	position, retired, same_as, cleared, seen
	FROM person_amendment`
	// THE ONE ordering spelling, shared by every composition read in the
	// app: company_amend.go's two company constants concatenate THIS
	// constant rather than respelling it, so a drift would have to change
	// both side tables' ordering at once.
	//
	// ⚠ Corrected at W5d (W5d-4 rule (d), SEAM-1). This comment used to
	// read "Any second key-column ORDER BY spelling anywhere in the app
	// trips TestW3OneOrderBySpellingInComposition" — a guarantee
	// MEASUREMENT REFUTES. That test's AST filter admits only string
	// literals containing `person_amendment`, and the M-1 constant split
	// leaves the ORDER BY in a literal carrying no table name, so it
	// examines no ordering literal at all: W3-m5's epoch-blind spelling
	// planted in the live constant below makes it PASS [MEASURED]. W3's
	// criterion is not reopened — the implementation falls short of it.
	// The instrument that DOES catch a second spelling is W5d's
	// TestW5dOneOrderBySpellingInCompositionByResolvedValue
	// (internal/store/w5d_source_scans_test.go), which resolves these
	// constants through go/types and scans the JOINED value, over both
	// scoped side tables. A scan's comment is not evidence of its scope;
	// read the filter.
	personAmendmentCompositionOrderBy = ` ORDER BY site_id, node_epoch, seq`

	personAmendmentCompositionSQL = personAmendmentCompositionSelect +
		personAmendmentCompositionOrderBy

	// personAmendmentCompositionByPersonSQL is the SAME read scoped to one
	// person (W3 critique M-1): PersonByID must not pay a whole-table
	// decode per lookup — gate check-in pays one call per occupant slot and
	// the muster's in-transit block one per distinct person per render.
	personAmendmentCompositionByPersonSQL = personAmendmentCompositionSelect +
		` WHERE person_id = ?` + personAmendmentCompositionOrderBy
)

// readPersonAmendments reads EVERY amendment row in ascending key order —
// the substrate of composition, Outcome, and the derived merge snapshot,
// so all three answer from the same record set in the same order. It rides
// store.LocalTx, the exported *sql.Tx seam, because this query IS the SQL
// half of the one ordering spec and must exist as one literal spelling
// here (the projection dump's generic OrderBy plumbing would hide it from
// the drift scan that guards it).
func readPersonAmendments(st *store.Store) ([]personAmendmentRow, error) {
	return queryPersonAmendments(st, personAmendmentCompositionSQL)
}

// readPersonAmendmentsFor reads ONLY the amendment rows targeting personID
// — same columns, same ascending-key order (both spellings shared with
// readPersonAmendments above, never respelled), so folding the result over
// one person is identical to filtering the full read in Go. This is M-1's
// fix: the scoped read for point lookups; whole-record consumers
// (Persons, Outcome, LoadPersonMergeMap) legitimately keep the full read.
func readPersonAmendmentsFor(st *store.Store, personID string) ([]personAmendmentRow, error) {
	return queryPersonAmendments(st, personAmendmentCompositionByPersonSQL, personID)
}

// queryPersonAmendments is the shared decode loop under both reads above.
// It rides store.LocalTx, the exported *sql.Tx seam — see
// readPersonAmendments' comment for why the SQL lives here as literal
// spellings rather than behind the projection dump's generic OrderBy.
func queryPersonAmendments(st *store.Store, query string, args ...any) ([]personAmendmentRow, error) {
	var out []personAmendmentRow
	err := st.LocalRead(func(tx *sql.Tx) error {
		rows, err := tx.Query(query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				k        store.Key
				personID sql.NullString
				vals     = make([]sql.NullString, len(personAmendableFacts))
				cleared  sql.NullString
				seen     sql.NullString
			)
			scan := []any{&k.SiteID, &k.NodeEpoch, &k.Seq, &personID}
			for i := range vals {
				scan = append(scan, &vals[i])
			}
			scan = append(scan, &cleared, &seen)
			if err := rows.Scan(scan...); err != nil {
				return err
			}
			r := personAmendmentRow{Key: k, PersonID: personID.String, Set: map[string]string{}}
			for i, fact := range personAmendableFacts {
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
		return nil, fmt.Errorf("domain: read person amendments: %w", err)
	}
	return out, nil
}

// --- the ONE resolution rule (D-92: causal, then ascending key) ----------

// resolveContest decides ONE fact's contest. `contest` is every amendment
// row that asserts or clears the fact within the contest's scope, already
// in ascending key order (the substrate reads deliver it so). It returns
// the winner's key and the undominated survivors (the "heads"), in
// ascending key order.
//
// THE RULE, spelled here and nowhere else ([[D-92]]):
//
//  1. A contestant is DOMINATED if any other contestant's `seen` names its
//     key — the other author had already seen it standing when they
//     corrected it, so the correction is causally later, no clock needed.
//     Domination is checked against EVERY member, not only against
//     survivors: in a chain A ← B ← C each link's direct reference is
//     enough, and nothing is inferred through a row the author did not
//     name (a post can hold B without holding everything B's author held).
//  2. Among the undominated survivors the GREATEST (site_id, node_epoch,
//     seq) wins — [[D-28]] Part B's fixed-order tiebreak, now confined to
//     genuinely concurrent edits, where it is the right answer.
//  3. If every member is dominated (only a wire-formed reference cycle can
//     do this; no honest edge mints one), the greatest key over the whole
//     contest wins, so the read stays total and deterministic.
//
// It is a pure function of the record set — order-independent across
// delivery interleavings (W3-5), invariant under any time-basis scramble
// (W3-1), and identical at every post holding the same rows.
//
// ⟦SchemaVersion 11, step 3⟧ Generic over contest.go's `contestant`, so the
// six carriers that clone this file's shape (company, trade, vehicle, trip,
// arrival, and the correction chain) read THIS function and none carries a
// second spelling of the rule. ⟦was: "func resolveContest(contest
// []personAmendmentRow)"⟧
func resolveContest[R contestant](contest []R) (winner store.Key, survivors []store.Key) {
	if len(contest) == 0 {
		return store.Key{}, nil
	}
	dominated := map[store.Key]bool{}
	for _, r := range contest {
		k := r.contestKey()
		for _, s := range r.contestSeen() {
			if s != k {
				dominated[s] = true
			}
		}
	}
	for _, r := range contest {
		if k := r.contestKey(); !dominated[k] {
			survivors = append(survivors, k)
		}
	}
	if len(survivors) == 0 {
		return contest[len(contest)-1].contestKey(), nil
	}
	return survivors[len(survivors)-1], survivors
}

// personRowsAsserting narrows rows to the contest for ONE fact: the rows
// that set or clear it, in the order given (rowsAsserting, contest.go).
func personRowsAsserting(rows []personAmendmentRow, fact string) []personAmendmentRow {
	return rowsAsserting(rows, fact)
}

// --- composition (the ONE composed read path, W3-8) ----------------------

// composePerson folds the amendment rows targeting ONE person into its
// composed record, fact by fact: the row resolveContest names as the
// winner of each fact's target-scope contest supplies that fact — a set
// value lands, a clear makes the fact ABSENT (nil), never empty-string
// (W3-3). `name` is the one non-pointer field (a person always has a name)
// and is not clearable. Under a record set with no `seen` anywhere this is
// exactly the pre-11 fold ("later keys win per field"); with `seen` it is
// [[D-92]]'s "the correction made in knowledge of the earlier one wins".
//
// ⟦was: applyAmendmentRow — one row folded at a time in key order⟧. A
// per-row fold cannot honour dominance, because whether a row's value
// stands depends on rows that come AFTER it in key order.
func composePerson(p *Person, rows []personAmendmentRow) {
	target := func(fact string) **string {
		switch fact {
		case "last_name":
			return &p.LastName
		case "first_name":
			return &p.FirstName
		case "phone":
			return &p.Phone
		case "company_id":
			return &p.CompanyID
		case "trade_id":
			return &p.TradeID
		case "role":
			return &p.Role
		case "position":
			return &p.Position
		case "retired":
			return &p.Retired
		case "same_as":
			return &p.SameAs
		}
		return nil
	}
	for _, fact := range personAmendableFacts {
		contest := personRowsAsserting(rows, fact)
		if len(contest) == 0 {
			continue
		}
		winner, _ := resolveContest(contest)
		var w personAmendmentRow
		for _, r := range contest {
			if r.Key == winner {
				w = r
				break
			}
		}
		if fact == "name" {
			if v, ok := w.Set["name"]; ok {
				p.Name = v
			}
			continue
		}
		dst := target(fact)
		if dst == nil {
			continue
		}
		if v, ok := w.Set[fact]; ok {
			val := v
			*dst = &val
		} else {
			*dst = nil // the winner cleared it
		}
	}
}

// composePersons applies every amendment over the base created-state rows,
// one composed person at a time (composePerson). Rows amending a person
// this store has no base row for compose to nothing here (the record set
// keeps them; a reader cannot show a person it has never been told exists).
func composePersons(base []Person, amendments []personAmendmentRow) []Person {
	byID := make(map[string]*Person, len(base))
	for i := range base {
		byID[base[i].PersonID] = &base[i]
	}
	perPerson := map[string][]personAmendmentRow{}
	for _, r := range amendments {
		perPerson[r.PersonID] = append(perPerson[r.PersonID], r)
	}
	for id, rows := range perPerson {
		if p, ok := byID[id]; ok {
			composePerson(p, rows)
		}
	}
	return base
}

// --- the derived merge-resolution snapshot (P-3 §5) ----------------------

// LoadPersonMergeMap loads the per-read derived resolution snapshot over
// the surviving AliasMap read shape (SEAM W3-S-RESOLVER). It is a pure
// projection of composed state — each person's EFFECTIVE same_as (the
// winner of that person's same_as contest under resolveContest: causal
// first, then record order; a winning Clear removes the edge) — plus the
// one derived rule the snapshot layer owns: wire-formed cycles (legal,
// because edges refuse cycles locally and folds never refuse) COLLAPSE to
// one pinned canonical representative: the target of the cycle-forming
// assertion LATEST IN RECORD ORDER over the composed cycle set (W3-17; P-3
// §6 rev 2 — deliberately NOT a min rule, so it cannot collide with the
// retired min(canonical_id) tiebreak). The representative rule stays
// key-ordered at SchemaVersion 11: the cycle's edges belong to DIFFERENT
// persons' contests, and dominance is defined within one fact's contest,
// never across them. Nothing here is cached: the label is recomputed from
// composed state on every load, so a later-keyed re-assertion of a cycle
// edge flips the representative at both posts identically (the AR2-2
// carve-out).
func LoadPersonMergeMap(st *store.Store) (AliasMap, error) {
	rows, err := readPersonAmendments(st)
	if err != nil {
		return nil, fmt.Errorf("domain: load person merge map: %w", err)
	}
	return mergeMapFromRows(rows), nil
}

func mergeMapFromRows(rows []personAmendmentRow) AliasMap {
	type edge struct {
		target string
		key    store.Key
	}
	// Effective same_as per person: the winner of each person's same_as
	// contest (resolveContest — the ONE rule); a winning Clear is no edge.
	// Rows arrive already in ascending key order.
	perPerson := map[string][]personAmendmentRow{}
	var order []string
	for _, r := range rows {
		if !r.asserts("same_as") {
			continue
		}
		if _, ok := perPerson[r.PersonID]; !ok {
			order = append(order, r.PersonID)
		}
		perPerson[r.PersonID] = append(perPerson[r.PersonID], r)
	}
	eff := map[string]edge{}
	for _, id := range order {
		contest := perPerson[id]
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
	// Cycle collapse. Walk each node once; a node revisited within the
	// current walk closes a cycle, whose members' composed edges are the
	// "composed cycle set" the representative rule quantifies over.
	done := map[string]bool{}
	nodes := make([]string, 0, len(m))
	for n := range m {
		nodes = append(nodes, n)
	}
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
				// path[i:] is the cycle. The representative: the target of
				// the record-order-latest cycle edge.
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

// --- the authoring edge (SEAM W3-S-EDGE) ---------------------------------

// maxMergeWalk bounds the edge's cycle-check walk, mirroring the read
// path's maxAliasHops: the check must terminate even over data a divergent
// peer folded in.
const maxMergeWalk = maxAliasHops

// ⟦SchemaVersion 11⟧ The `padOrientedValue` const and W5c-1's value-domain
// guard that stood here are STRUCK with the fact ([[D-88]] ERRATA, [[D-91]]):
// there is no `pad_oriented` to guard. The edge now refuses it the way it
// refuses any unknown field — Clear:["pad_oriented"] is "not an amendable
// field", and no input field exists to set it. Per-site orientation is a
// later stage of this increment, as the ORIENTING post's own record.

// personContest returns ONE fact's contested rows for a target person, in
// the scope Outcome and Competitors have always used (W3-9/W3-10, spelled
// once here for the read AND the authoring edge):
//   - same_as (the merge pointer) is contested among assertions targeting
//     the SAME record — resolving it through the merge map first would
//     erase the very fact under judgement;
//   - every VALUE fact is contested across the assertion sets a merge
//     JOINS (§6 case 5): the rows whose target resolves, through m, to the
//     same representative the target does.
//
// rows must already be in ascending key order; the result keeps it.
//
// ⟦SchemaVersion 11, step 3⟧ The scope rule itself now lives in
// amendmentContest (contest.go), shared with the company, trade and vehicle
// passes; this is its person-typed name, kept for the template's readers.
func personContest(rows []personAmendmentRow, m AliasMap, rep, personID, fact string) []personAmendmentRow {
	return amendmentContest(rows, m, rep, personID, fact)
}

// personSeen computes the `seen` list AmendPerson stamps ([[D-92]]): for
// every fact the amendment asserts or clears, the undominated heads of that
// fact's contest as this post holds it right now — at the target's own
// scope (what composePerson shows for this record) AND at the merge-group
// scope (what Outcome judges), unioned, because the two contests can have
// different heads and the author has seen both. Referencing every head,
// not only the current winner, is what keeps a tiebreak loser from
// resurfacing later: everything present here is either a head (named
// directly) or already dominated by one.
//
// The result is sorted ascending by key and de-duplicated, so two authors
// who saw the same state stamp byte-identical lists.
//
// ⟦SchemaVersion 11, step 3⟧ The stamp rule lives in amendmentSeen
// (contest.go), shared by every carrier's edge; this is its person-typed
// name.
func personSeen(rows []personAmendmentRow, m AliasMap, personID string, facts []string) []SeenRef {
	return amendmentSeen(rows, m, personID, facts)
}

// AmendPerson authors one person_amended event. EVERY refusal below is
// PRE-APPEND (W3-2): a refusal returns before Author is called, so the log
// length is unchanged and no envelope exists. These checks live at this
// edge and ONLY at this edge — the fold never refuses (see
// applyPersonAmended).
//
// ⟦SchemaVersion 11, [[D-92]]⟧ The edge is also where `seen` is stamped:
// after every refusal has had its chance and before the payload is
// marshalled, the amendment is given the heads of each asserted fact's
// contest as THIS post holds it. This is the knowledge d90-supersession
// measured as "computed at the authoring edge and thrown away"; it is now
// written into the event. A caller cannot supply it — PersonAmendment has
// no field for it — so a `seen` list is always the authoring post's own
// honest reading of its own record set.
func AmendPerson(st *store.Store, siteID string, nodeEpoch int64, in PersonAmendment, occurredAtMS int64, timeSource string) (store.Envelope, error) {
	if in.PersonID == "" {
		return store.Envelope{}, fmt.Errorf("domain: amend person: person_id is required")
	}
	p, err := PersonByID(st, in.PersonID)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: amend person: %w", err)
	}
	if p == nil {
		return store.Envelope{}, fmt.Errorf("domain: amend person: %q is not on the roster — nothing was recorded", in.PersonID)
	}

	set := in.setFields()
	assertedCount := 0
	for _, v := range set {
		if v != nil {
			assertedCount++
		}
	}
	// The no-op refusal: an amendment that asserts nothing and clears
	// nothing records nothing, and recording nothing must be refused, not
	// minted (W3-2).
	if assertedCount == 0 && len(in.Clear) == 0 {
		return store.Envelope{}, fmt.Errorf("domain: amend person: refused — the amendment changes nothing (no field set, nothing cleared); nothing was recorded")
	}
	// name-to-empty: a person always has a name; blanking it is refused at
	// the edge (and `name` is not clearable, below, for the same reason).
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return store.Envelope{}, fmt.Errorf("domain: amend person: refused — a person's name cannot be amended to empty; nothing was recorded")
	}
	// ⟦SchemaVersion 11⟧ W5c-1's pad-orientation value-domain guard stood
	// here; it is struck with the fact (see the note above AmendPerson's
	// helpers). Its three load-bearing properties — refuse never coerce,
	// pre-append and only here, never reaching Clear — are the properties
	// every guard on this edge keeps, and the fold below it stays untouched.
	// unknown trade_id (seam S10). ⟦W8⟧ THE REFUSAL IS NARROWED, NEVER
	// DELETED, and the distinction is the whole of this limb.
	//
	// Before W8 there was no trade register, so every trade_id assertion was
	// refused here — an always-refuse edge with a box on a person's file that
	// an officer could type into AND WHICH ALWAYS ANSWERED NO. W8 builds the
	// register and this limb becomes what it always meant: refuse ids that
	// are not in it, accept ids that are.
	//
	// ⚠ A BUILDER WHO READS "W8 BUILDS THE ACCEPT PATH" AND DELETES THIS
	// BLOCK SATISFIES A NAIVE W8 CHECK AND REDS A LOCKED W3 CRITERION — W3's
	// edge-refusal criterion produces the unknown-trade-id arm and still
	// does.
	//
	// ⚠ A MERGED-AWAY ID IS ACCEPTED, and this is the arm that is easy to get
	// wrong: the row of a merged-away duplicate IS STILL IN THE REGISTER —
	// `same_as` is a per-field assertion, not a deletion — so the question
	// here is "is this id a `trade` row?" and the RESOLUTION happens at the
	// READ. A guard that rejected an id whose row exists but has been merged
	// away would refuse an officer for picking the name they were shown.
	//
	// ⚠ AND THERE IS NO RETIRED-TRADE ARM. D-30(b) rules that a trade is
	// never retired, so A §10.5's "the authoring edge refuses a RETIRED
	// trade_id" has no subject and is not built.
	//
	// ⚠ THE FOLD IS NOT TOUCHED. It already documents that unknown trade ids
	// all fold, and a W8 change there would red W3's fold-never-refuses
	// criterion.
	if in.TradeID != nil {
		known, err := TradeByID(st, *in.TradeID)
		if err != nil {
			return store.Envelope{}, fmt.Errorf("domain: amend person: %w", err)
		}
		if known == nil {
			return store.Envelope{}, fmt.Errorf("domain: amend person: refused — trade id %q is not in the trade register; pick a trade from the list, or add it there first; nothing was recorded", *in.TradeID)
		}
	}
	// ⟦R-113 / M-19, 2026-10-03⟧ unknown company_id — the SAME arm as the
	// trade one above, which this edge had and the company limb did not: a
	// company_id naming no `company` row was accepted, confirmed "Effective",
	// and the roster then showed the raw id as the person's company (L06
	// R-113, measured twice). The question is the trade arm's — "is this id
	// a `company` row?" — so a MERGED-AWAY company id is accepted for the
	// same reason a merged-away trade id is: its row survives (`same_as` is
	// a per-field assertion, never a deletion) and the resolution happens
	// at the read. The fold is untouched.
	if in.CompanyID != nil {
		known, err := CompanyByID(st, *in.CompanyID)
		if err != nil {
			return store.Envelope{}, fmt.Errorf("domain: amend person: %w", err)
		}
		if known == nil {
			return store.Envelope{}, fmt.Errorf("domain: amend person: refused — company id %q is not in the company register; pick a company from the roster, or add it there first; nothing was recorded", *in.CompanyID)
		}
	}
	// ⟦R-118 / M-8, 2026-10-03 — [[D-21.5]] built⟧ `retired`. The fact had
	// been recorded, confirmed "Effective" and read by NOTHING (L06 R-118,
	// measured twice): the person stayed in the gate picker, the roster and
	// the orientation list. D-21.5's rule — "the person stops being
	// selectable … Retirement affects selection, not the record … a retired
	// person who is still on site must still appear … Reversible" — is now
	// read by the selection surfaces through Person.IsRetired (refdata.go),
	// and this edge guards its two preconditions:
	//   - the VALUE DOMAIN: "true" (or yes/1) retires; anything else is
	//     refused rather than recorded as an opaque string nobody reads.
	//     Un-retiring is Clear:["retired"] — the un-set, like every other
	//     fact — never a "false" value, so the composed record is either
	//     retired or has no retired fact at all.
	//   - NEVER ANYONE ON SITE: a person holding an OPEN visit anywhere this
	//     post knows of cannot be retired — they are on a muster right now,
	//     and the officer must check them out first. Refused pre-append, so
	//     the record is untouched (nothing is ever removed from a muster by
	//     this edge in any case; the guard is about the officer's intent).
	if in.Retired != nil {
		if !isRetiredValue(in.Retired) {
			return store.Envelope{}, fmt.Errorf("domain: amend person: refused — retired takes the value \"true\" (to un-retire, put retired in the Clear box); nothing was recorded")
		}
		open, err := openVisitsOfPerson(st, in.PersonID)
		if err != nil {
			return store.Envelope{}, fmt.Errorf("domain: amend person: %w", err)
		}
		if len(open) > 0 {
			return store.Envelope{}, fmt.Errorf("domain: amend person: refused — %s is on site at %s right now; a person on site cannot be retired — check them out first, then retire them; nothing was recorded", p.Name, siteNameAt(st, open[0].SiteID))
		}
		canonical := "true"
		in.Retired = &canonical
	}
	for _, c := range in.Clear {
		if c == "name" {
			return store.Envelope{}, fmt.Errorf("domain: amend person: refused — a person's name cannot be cleared; nothing was recorded")
		}
		if !isAmendableFact(c) {
			return store.Envelope{}, fmt.Errorf("domain: amend person: refused — %q is not an amendable field; nothing was recorded", c)
		}
		if v := set[c]; v != nil {
			return store.Envelope{}, fmt.Errorf("domain: amend person: refused — %q is both set and cleared in one amendment; nothing was recorded", c)
		}
	}
	// The re-hosted merge-edge checks (P-3 §5: the deleted edge's refusals
	// move to THIS edge) plus the NEW kind check (W3-18).
	if in.SameAs != nil {
		target := *in.SameAs
		if target == in.PersonID {
			return store.Envelope{}, fmt.Errorf("domain: amend person: refused — a record cannot be merged with itself; nothing was recorded")
		}
		// Kind homogeneity (W3-18): same_as joins PERSON records only. The
		// id-kind-agnostic mechanism it replaced never checked this — the measured hole
		// this closes (P-3 §2 fact 5).
		tp, err := PersonByID(st, target)
		if err != nil {
			return store.Envelope{}, fmt.Errorf("domain: amend person: %w", err)
		}
		if tp == nil {
			return store.Envelope{}, fmt.Errorf("domain: amend person: refused — %q is not a person record; same_as joins person records only; nothing was recorded", target)
		}
		// Standing prior merge: this store already composes an answer for
		// this record; a second, contradictory one is refused here. (Two
		// not-yet-synced posts can still legally author competing merges —
		// record-order composition converges them once replicated.)
		if p.SameAs != nil {
			return store.Envelope{}, fmt.Errorf("domain: amend person: refused — %s is already merged with %s; clear same_as (the un-merge) first; nothing was recorded", in.PersonID, *p.SameAs)
		}
		// Cycle: would this edge close a loop back to the record being
		// amended? Walk the composed resolution state (never a private
		// snapshot) from the target.
		m, err := LoadPersonMergeMap(st)
		if err != nil {
			return store.Envelope{}, fmt.Errorf("domain: amend person: %w", err)
		}
		cur := target
		for i := 0; i < maxMergeWalk; i++ {
			next, ok := m[cur]
			if !ok {
				break
			}
			if next == in.PersonID {
				return store.Envelope{}, fmt.Errorf("domain: amend person: refused — merging %s into %s would close a cycle; nothing was recorded", in.PersonID, target)
			}
			if next == cur {
				break
			}
			cur = next
		}
	}

	// The `seen` stamp ([[D-92]]): every fact this amendment asserts or
	// clears, resolved over the record set THIS post holds right now. It is
	// computed after every refusal above, so a refused amendment never
	// reads the table for it, and before Author, so the stamp describes the
	// state the author actually saw.
	rows, err := readPersonAmendments(st)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: amend person: %w", err)
	}
	var facts []string
	for _, fact := range personAmendableFacts {
		if set[fact] != nil {
			facts = append(facts, fact)
		}
	}
	facts = append(facts, in.Clear...)
	seen := personSeen(rows, mergeMapFromRows(rows), in.PersonID, facts)

	payload, err := json.Marshal(PersonAmended{
		Type: evtPersonAmended, PersonID: in.PersonID,
		Name: in.Name, LastName: in.LastName, FirstName: in.FirstName,
		Phone: in.Phone, CompanyID: in.CompanyID, TradeID: in.TradeID,
		Role: in.Role, Position: in.Position,
		Retired: in.Retired, SameAs: in.SameAs, Clear: in.Clear,
		Seen: seen,
	})
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: amend person: %w", err)
	}
	env, err := st.Author(siteID, nodeEpoch, payload, occurredAtMS, timeSource)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: amend person: %w", err)
	}
	return env, nil
}

// openVisitsOfPerson lists personID's OPEN visits at EVERY site this post
// holds rows for — the "is this person on a muster anywhere right now"
// question the retire guard asks (⟦R-118 / D-21.5⟧). Indexed per person,
// never a season-wide scan; it reads the same `is_open` the muster reads.
func openVisitsOfPerson(st *store.Store, personID string) ([]Visit, error) {
	return dumpSectionWhere[Visit](st, "visit",
		store.Eq{Column: "person_id", Value: personID}, store.Eq{Column: "is_open", Value: 1})
}
