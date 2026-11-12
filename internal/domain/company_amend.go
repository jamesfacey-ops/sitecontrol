// company_amend.go — item 12: the `company_amended` event, its INSERT-only
// `company_amendment` side table, the ascending-key composed company read
// path, the amendment authoring edge and its refusals, and the derived
// company merge-resolution snapshot (`same_as`, P-3 option 1a). This is
// W5d, the FIRST clone of W3's mechanism-defining build in person_amend.go
// (WINDOW-BUILD-PLAN-v1 `### W5 —`, W5d criteria LOCKED 2026-08-31); W7
// (vehicle) and W8 (trade) clone the same shape after it.
//
// What differs from the person side, and why — stated here because a clone
// author's first instinct is to copy the whole amendable set across:
//
//   - The amendable set is EXACTLY {name, same_as} (W5d-22(a), as
//     literals). A company has one content fact — `name` (refdata.go's
//     Company) — and the merge pointer. `retired` does NOT exist: D-41.1
//     rules company retirement out ("a company can be corrected but never
//     retired"), so there is no column, no fact and no edge control for it
//     (W5d-2). trade_id/phone/role/pad_oriented/position/last_name/
//     first_name have no company limb at all.
//     ⚠ CompanyCreated DOES carry RecordedBy (refdata.go:26); the
//     amendment carrier deliberately does NOT (W5d-2b) — attribution on an
//     amendment is STRUCTURAL, env.SiteID, never a payload field (D-5, S7).
//   - The edge refusal set is NINE arms, not the person side's ten: person
//     arm 3 (unknown trade_id, person_amend.go:569) has no company
//     instance, because there is no trade_id limb and no trade register.
//     That measurement is also why W3-m2 is not ridden at W5d (SEAM-5).
//   - The kind check INVERTS (W5d-13): `same_as` joins COMPANY records
//     only, so a company→person target is refused here where the person
//     edge refuses person→company.
//
// The load-bearing rules are W3's, unchanged, and are not restated: see
// person_amend.go's file comment for composition order, the edge/fold
// asymmetry, Clear-composes-as-absent, the derived-per-read snapshot, and
// the never-stored supersession rule. ⟦SchemaVersion 11, [[D-92]], step 3
// of the 10 → 11 increment⟧ The causal `seen` reference is cloned here in
// the order the merge field was cloned (company first): the payload gains
// `seen`, the side table gains the column, the edge stamps it, and the
// composed read, Outcome/Competitors and the merge snapshot all resolve
// through resolveContest — the ONE rule, read from person_amend.go through
// contest.go, never respelled. Two consequences of them are worth
// naming HERE because they are company-specific:
//
//   - `company.name` is TEXT NOT NULL and CompanyCreated.Name is a bare
//     string, so the composed Company.Name stays a bare string too. The
//     edge refuses name-to-empty and refuses Clear:["name"]; the fold NEVER
//     refuses; a cleared fact composes as ABSENT. So a WIRE-received
//     Clear:["name"] composes a company with NO NAME — the empty string is
//     the only representable "absent" on a non-pointer field — and the
//     muster's third preference arm renders its raw company id during a
//     roll call (W5d-22(d), pinned rather than discovered).
//   - The company merge map is what domain/muster.go's company line
//     resolves through as of this chunk, and it is ALSO what this edge's
//     cycle check walks (W5d-5b: two consumers, both W5d's). The legacy
//     shared legacy reference-merge table kept the TRADE line until W8
//     deleted it — the schedule is stated once, at P-3 §5, and is not
//     restated here.
package domain

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"sitecontrol/internal/store"
)

// --- event payload -------------------------------------------------------

// CompanyAmended is the wire payload (type "company_amended"). Lowercase
// snake_case keys per the measured house convention; `clear` is lowercase
// on the wire (DECIDED at P-3 §7 rev 3). The frozen literals for gate
// B-2's wire review are the type name `company_amended` and the field
// names `company_id`, `name`, `same_as`, plus `clear` — and each field name
// freezes TWICE over, because the same spellings are simultaneously the
// Clear VALUE vocabulary (S3). `recorded_by` is deliberately absent and was
// STRUCK from that freeze list at rev 4 (W5d-2b).
//
// ⟦SchemaVersion 11⟧ `seen` is ADDED ([[D-92]]): the keys of the earlier
// amendments this author had already seen standing for the facts asserted
// here — the reference that lets a correction made in knowledge of an
// earlier one WIN regardless of site_id (resolveContest). Stamped by
// AmendCompany, never by a caller; omitted when the author saw nothing
// standing. Its wire spelling is person_amended's SeenRef, so B-2's list
// gains no literal here that step 2 did not already add.
type CompanyAmended struct {
	Type      string    `json:"type"` // "company_amended"
	CompanyID string    `json:"company_id"`
	Name      *string   `json:"name,omitempty"`
	SameAs    *string   `json:"same_as,omitempty"`
	Clear     []string  `json:"clear,omitempty"`
	Seen      []SeenRef `json:"seen,omitempty"`
}

const evtCompanyAmended = "company_amended"

// companyAmendableFacts is the amendable set, in canonical rendering order
// (W5d-22(a), as literals). Fact names ARE the payload field spellings —
// visible-failure-v1 §4.2's convention, cloned.
var companyAmendableFacts = []string{"name", "same_as"}

func isCompanyAmendableFact(f string) bool {
	for _, k := range companyAmendableFacts {
		if k == f {
			return true
		}
	}
	return false
}

// CompanyAmendment is the authoring-edge input (SEAM W5D-S-INPUT). Value
// fields are pointers: nil = "this amendment does not touch that field",
// which is a different thing from asserting an empty value. Clear lists
// fact names to UN-SET; Clear: ["same_as"] is the un-merge — there is no
// separate un-merge event and no separate merge affordance (DR-6 / S6: one
// POST for every company fact, never a parallel author).
type CompanyAmendment struct {
	CompanyID string
	Name      *string
	SameAs    *string
	Clear     []string
}

// setFields returns the asserted (fact → value) pairs of the input. Note
// what this makes UNREPRESENTABLE, because a criterion turns on it: an
// out-of-set fact cannot be ASSERTED at all (the input is a struct and this
// map is built from its own fields), so W5d-22(b) arm 5 is reachable
// through the `Clear` vocabulary only.
func (in CompanyAmendment) setFields() map[string]*string {
	return map[string]*string{"name": in.Name, "same_as": in.SameAs}
}

// --- the INSERT-only side table (projection) -----------------------------

// companyAmendmentProjection registers the side table the fold maintains
// and the composition reads — person_amend.go:132–166's shape, cloned. The
// declared column set is EXACT (W5d-1): the key triple, the target, one
// column per amendable fact, `cleared`, and — since SchemaVersion 11
// ([[D-92]]) — `seen` (the author's seen-list, JSON, NULL when empty: a
// reference WRITTEN AT AUTHORING, never a status). NO status-shaped column may
// ever join it (supersession is computed at read, never stored) and no
// `retired` column may ever join it (D-41.1) — the two triggers make
// INSERT-only a mechanism rather than a convention.
func companyAmendmentProjection() store.Projection {
	return store.Projection{
		Name:    "company_amendment",
		Table:   "company_amendment",
		OrderBy: "site_id, node_epoch, seq",
		Schema: `CREATE TABLE IF NOT EXISTS company_amendment (
			site_id     TEXT NOT NULL,
			node_epoch  INTEGER NOT NULL,
			seq         INTEGER NOT NULL,
			company_id  TEXT,
			name        TEXT,
			same_as     TEXT,
			cleared     TEXT,
			seen        TEXT,
			PRIMARY KEY (site_id, node_epoch, seq)
		) STRICT;
		CREATE TRIGGER IF NOT EXISTS company_amendment_insert_only_update
			BEFORE UPDATE ON company_amendment BEGIN
			SELECT RAISE(ABORT, 'company_amendment is INSERT-only: supersession is computed at read, never rewritten in place');
		END;
		CREATE TRIGGER IF NOT EXISTS company_amendment_insert_only_delete
			BEFORE DELETE ON company_amendment BEGIN
			SELECT RAISE(ABORT, 'company_amendment is INSERT-only: amendments are superseded, never merely absent');
		END;`,
		Apply: applyCompanyAmended,
	}
}

// applyCompanyAmended is the fold. IT NEVER REFUSES (W3-2, ridden at
// W5d-17): a wire-received amendment passed its authoring post's edge (or
// was minted by a divergent one) and refusing it here would fork the two
// logs forever. Cross-kind same_as targets, cycle-forming merges, empty
// names, cleared names, no-ops — all fold; edge validation happened (or
// failed to happen) at the AUTHORING post, pre-append. Values are coerced
// tolerantly for the same reason: a payload this decoder finds odd must
// still land.
func applyCompanyAmended(tx *sql.Tx, env store.Envelope) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(env.Payload, &raw); err != nil {
		return nil // not an object-shaped payload; not this fold's event
	}
	var typ string
	if err := json.Unmarshal(raw["type"], &typ); err != nil || typ != evtCompanyAmended {
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
				return fmt.Errorf("domain: company_amended fold: cleared: %w", err)
			}
			cleared = string(b)
		}
	}
	// `seen` ([[D-92]]) through the one codec, seenColumnValue (contest.go):
	// absent, empty or malformed lands as NULL — the fold never refuses.
	seen, err := seenColumnValue(raw)
	if err != nil {
		return fmt.Errorf("domain: company_amended fold: seen: %w", err)
	}
	_, err = tx.Exec(`INSERT INTO company_amendment
		(site_id, node_epoch, seq, company_id, name, same_as, cleared, seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		env.SiteID, env.NodeEpoch, env.Seq,
		asString("company_id"), asString("name"), asString("same_as"), cleared, seen)
	if err != nil {
		return fmt.Errorf("domain: company_amended fold: insert %v: %w", env.Key(), err)
	}
	return nil
}

// --- reading the side table (the composition's own SQL) ------------------

// companyAmendmentRow is one decoded side-table row: the amendment's key,
// its target, its asserted facts, its cleared facts, and (SchemaVersion 11,
// [[D-92]]) the keys its author had already seen standing — the dominance
// input to resolveContest. A row with no Seen competes on key order alone.
type companyAmendmentRow struct {
	Key       store.Key
	CompanyID string
	Set       map[string]string
	Cleared   []string
	Seen      []store.Key
}

func (r companyAmendmentRow) asserts(fact string) bool {
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
func (r companyAmendmentRow) contestKey() store.Key    { return r.Key }
func (r companyAmendmentRow) contestSeen() []store.Key { return r.Seen }
func (r companyAmendmentRow) targetID() string         { return r.CompanyID }

// The company composition read, in the house's own two-half shape (the M-1
// fix): a shared SELECT half, a shared `WHERE company_id = ?`, and THE ONE
// ordering spelling — which is not respelled here at all.
//
// ⚠ personAmendmentCompositionOrderBy is REUSED verbatim rather than
// cloned. W5d-4 requires "the shared canonical ORDER BY site_id,
// node_epoch, seq spelling, never a second spelling"; sharing the constant
// makes that structural instead of conventional, so there is exactly ONE
// key-column ordering literal ACROSS THE AMENDMENT COMPOSITIONS and a drift
// would have to change the spelling for BOTH side tables at once. (Not "the
// whole application": site-scoped queries legitimately order by
// (node_epoch, seq) with site_id already bound — the same over-claim the
// W5d build critique struck here, and the one W5d-4's own rev-4 correction
// was about.) The name reads
// person-sided because W3 declared it; renaming it is W3's code, not this
// chunk's.
const (
	companyAmendmentCompositionSelect = `SELECT site_id, node_epoch, seq, company_id,
	name, same_as, cleared, seen
	FROM company_amendment`

	companyAmendmentCompositionSQL = companyAmendmentCompositionSelect +
		personAmendmentCompositionOrderBy

	// companyAmendmentCompositionByCompanySQL is the SAME read scoped to
	// ONE company (the M-1 shape, cloned): CompanyByID must not pay a
	// whole-table decode per lookup — the gate's check-in path and the
	// muster's grouping both resolve companies per row.
	companyAmendmentCompositionByCompanySQL = companyAmendmentCompositionSelect +
		` WHERE company_id = ?` + personAmendmentCompositionOrderBy
)

// readCompanyAmendments reads EVERY company amendment row in ascending key
// order — the substrate of composition, Outcome and the derived merge
// snapshot, so all three answer from the same record set in the same order.
func readCompanyAmendments(st *store.Store) ([]companyAmendmentRow, error) {
	return queryCompanyAmendments(st, companyAmendmentCompositionSQL)
}

// readCompanyAmendmentsFor reads ONLY the rows targeting companyID — same
// columns, same ascending-key order, both spellings shared with the read
// above and never respelled (W5d-4: the M-1 shape is not cloned).
func readCompanyAmendmentsFor(st *store.Store, companyID string) ([]companyAmendmentRow, error) {
	return queryCompanyAmendments(st, companyAmendmentCompositionByCompanySQL, companyID)
}

// queryCompanyAmendments is the shared decode loop under both reads. It
// rides store.LocalTx, the exported *sql.Tx seam, for the reason
// readPersonAmendments states: this query IS the SQL half of the one
// ordering spec and must exist here as a literal spelling, where the drift
// scan that guards it can see it.
func queryCompanyAmendments(st *store.Store, query string, args ...any) ([]companyAmendmentRow, error) {
	var out []companyAmendmentRow
	err := st.LocalRead(func(tx *sql.Tx) error {
		rows, err := tx.Query(query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				k         store.Key
				companyID sql.NullString
				vals      = make([]sql.NullString, len(companyAmendableFacts))
				cleared   sql.NullString
				seen      sql.NullString
			)
			scan := []any{&k.SiteID, &k.NodeEpoch, &k.Seq, &companyID}
			for i := range vals {
				scan = append(scan, &vals[i])
			}
			scan = append(scan, &cleared, &seen)
			if err := rows.Scan(scan...); err != nil {
				return err
			}
			r := companyAmendmentRow{Key: k, CompanyID: companyID.String, Set: map[string]string{}}
			for i, fact := range companyAmendableFacts {
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
		return nil, fmt.Errorf("domain: read company amendments: %w", err)
	}
	return out, nil
}

// --- composition (the ONE composed read path, W5d-3) ---------------------

// composeCompany folds the amendment rows targeting ONE company into its
// composed record, fact by fact: the row resolveContest names as the winner
// of each fact's target-scope contest supplies that fact — a set value
// lands, a clear makes the fact ABSENT, never empty-string (W5d-18).
//
// `name` is the one non-pointer field, mirroring the person side's own
// non-pointer `name`. The edge refuses Clear:["name"], so the ONLY way a
// company reaches the cleared branch nameless is a wire-received clear the
// fold may not refuse — and the empty string is the only representable
// absent on a bare string. W5d-22(d) pins that composition, and the
// muster's third preference arm pins how it renders.
//
// ⟦SchemaVersion 11, [[D-92]]⟧ ⟦was: applyCompanyAmendmentRow — one row
// folded at a time in key order, "later keys win per fact"⟧. A per-row fold
// cannot honour dominance, because whether a row's value stands depends on
// rows that come AFTER it in key order; under a record set with no `seen`
// anywhere this is exactly the pre-11 fold.
func composeCompany(c *Company, rows []companyAmendmentRow) {
	for _, fact := range companyAmendableFacts {
		contest := rowsAsserting(rows, fact)
		if len(contest) == 0 {
			continue
		}
		winner, _ := resolveContest(contest)
		var w companyAmendmentRow
		for _, r := range contest {
			if r.Key == winner {
				w = r
				break
			}
		}
		v, set := w.Set[fact]
		switch fact {
		case "name":
			c.Name = v // a winning clear lands as "" — the only absent a bare string has
		case "same_as":
			if set {
				val := v
				c.SameAs = &val
			} else {
				c.SameAs = nil // the winner cleared it
			}
		}
	}
}

// composeCompanies applies every amendment over the base created-state
// rows, one composed company at a time (composeCompany). Rows amending a
// company this store has no base row for compose to nothing here (the
// record set keeps them; a reader cannot show a company it has never been
// told exists).
func composeCompanies(base []Company, amendments []companyAmendmentRow) []Company {
	byID := make(map[string]*Company, len(base))
	for i := range base {
		byID[base[i].CompanyID] = &base[i]
	}
	perCompany := map[string][]companyAmendmentRow{}
	for _, r := range amendments {
		perCompany[r.CompanyID] = append(perCompany[r.CompanyID], r)
	}
	for id, rows := range perCompany {
		if c, ok := byID[id]; ok {
			composeCompany(c, rows)
		}
	}
	return base
}

// --- the derived merge-resolution snapshot (P-3 §5) ----------------------

// LoadCompanyMergeMap loads the per-read derived company resolution
// snapshot over the surviving AliasMap read shape (SEAM W5D-S-RESOLVER),
// the company analogue of LoadPersonMergeMap. It is a pure projection of
// composed state — each company's EFFECTIVE same_as (the winner of that
// company's same_as contest under resolveContest: causal first, then
// record order; a winning Clear removes the edge ⟦was: "last in record
// order"⟧) — plus the one derived rule the snapshot layer
// owns: wire-formed cycles COLLAPSE to one pinned canonical representative,
// the target of the cycle-forming assertion LATEST IN RECORD ORDER over the
// composed cycle set (W3-17, cloned at W5d-12).
//
// ⚠ It is DERIVED PER READ and caches NOTHING. Two mutants live here and
// both are booked: (m6), a cached or package-level map — which is also the
// cheapest way to make "muster.go:122 is the single line that changes"
// literally true, and is W3-m9's registered shape; and (m7), a map built
// from the composed Company.SameAs scalar alone, which resolves single
// edges correctly and silently loses cycle collapse, because composition
// discards each edge's store.Key and the representative rule needs it.
func LoadCompanyMergeMap(st *store.Store) (AliasMap, error) {
	rows, err := readCompanyAmendments(st)
	if err != nil {
		return nil, fmt.Errorf("domain: load company merge map: %w", err)
	}
	return companyMergeMapFromRows(rows), nil
}

func companyMergeMapFromRows(rows []companyAmendmentRow) AliasMap {
	type edge struct {
		target string
		key    store.Key
	}
	// Effective same_as per company: the winner of each company's same_as
	// contest (resolveContest — the ONE rule, [[D-92]]); a winning Clear is
	// no edge. Rows arrive already in ascending key order. The cycle
	// representative below stays key-ordered at SchemaVersion 11: a cycle's
	// edges belong to DIFFERENT companies' contests, and dominance is
	// defined within one fact's contest, never across them. ⟦was: "last
	// assertion in record order wins"⟧
	perCompany := map[string][]companyAmendmentRow{}
	var order []string
	for _, r := range rows {
		if !r.asserts("same_as") {
			continue
		}
		if _, ok := perCompany[r.CompanyID]; !ok {
			order = append(order, r.CompanyID)
		}
		perCompany[r.CompanyID] = append(perCompany[r.CompanyID], r)
	}
	eff := map[string]edge{}
	for _, id := range order {
		contest := perCompany[id]
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

// --- the authoring edge (SEAM W5D-S-EDGE) --------------------------------

// CompanyAmendRefusal is an EDGE REFUSAL, distinguished from an internal
// failure BY TYPE rather than by reading an error string.
//
// This exists for W5d-24, and the reason is worth stating: the surface this
// chunk clones renders `err.Error()` on ANY error, so an internal fault —
// a failed read of the merge state, say — reaches an officer as a raw
// `sql: database is closed` under the word REFUSED [MEASURED on the clean
// tree]. A refusal's text is AUTHORED COPY and belongs on the page; an
// internal error's text is not and does not. The two are only separable if
// the domain says which is which, so it does.
type CompanyAmendRefusal struct {
	// Reason is the officer-facing sentence, without prefix or suffix.
	Reason string
}

func (e *CompanyAmendRefusal) Error() string {
	return "domain: amend company: refused — " + e.Reason + "; nothing was recorded"
}

func refuseCompanyAmend(format string, args ...any) error {
	return &CompanyAmendRefusal{Reason: fmt.Sprintf(format, args...)}
}

// CompanyAmendInputError is the edge's THIRD error class: the officer named
// a record this post cannot act on. It is neither of the other two, and
// collapsing it into either one tells an officer something false.
//
// ⚠ It is NOT a fact-domain refusal, and it deliberately does not use
// refuseCompanyAmend: W5d-22(b)'s company set is NINE arms, derived from the
// person edge's, and these two guards are the ones that criterion names as
// explicitly OUTSIDE it — their person-side analogues (person_amend.go's
// `person_id is required` and `%q is not on the roster`) carry no
// "refused —" spelling either. Folding them in would make the count eleven.
//
// ⚠ It is NOT an internal fault either, and that is why the type exists.
// Measured on the build this replaces: a mistyped company id — the single
// most likely mistake on a surface whose id is typed by hand with no picker
// — fell through `errors.As(err, &CompanyAmendRefusal)` to the web layer's
// internal-error arm and was answered with HTTP 500 and "this post could
// not complete the amendment … try again, and if it repeats, radio it
// through". Every clause of that is false here: the post completed its work
// correctly, retrying an unknown id fails identically forever, and the
// officer was sent to the radio over a typo. That is the copy-lie shape
// this project has already paid for once, on the newest officer-facing
// surface in the tree — and W7 and W8 clone this file.
//
// The authored sentence below is the one the officer should have been
// reading all along; it was never missing, it was being discarded into the
// log. What changed is where it is ROUTED, not what it says.
type CompanyAmendInputError struct {
	// Reason is the officer-facing sentence: what is wrong with what they
	// named, with no prefix and no trailing "nothing was recorded" — the
	// Error() string and the page's own copy each add that themselves.
	Reason string
}

func (e *CompanyAmendInputError) Error() string {
	return "domain: amend company: " + e.Reason + "; nothing was recorded"
}

func refuseCompanyInput(format string, args ...any) error {
	return &CompanyAmendInputError{Reason: fmt.Sprintf(format, args...)}
}

// CompanyRenameRefusal is the company edge's [[D-82]]-parity refusal
// (⟦R-145 / M-18, 2026-10-03⟧): the corrected name is already ANOTHER
// company's name, under the same fold the create doors use (checkin.go's
// sameCompanyName — case and leading/trailing space, intake's own
// pre-check). Two companies with one name are two identical company
// headings on the evacuation muster sheet — L06 R-145 measured exactly that
// (Glacier Line Electric renamed onto "Kestrel Survey Group": "Recorded.
// Effective: name.", then two "Kestrel Survey Group" blocks on one sheet
// and three same-named rows in the roster picker). The trade edge has
// refused this since D-82; the company edge now does the same.
//
// It is a FACT-DOMAIN refusal that also carries the holder AS DATA: Unwrap
// yields the *CompanyAmendRefusal that web/company_amend.go's three-way
// errors.As partition matches, so it lands on the refusal arm with no fourth
// arm, and a caller that wants to name the company to pick reads Existing by
// TYPE, never by parsing this text.
//
// ⛔ REFUSED, NEVER MERGED. Merging two companies is `same_as`, a separate
// amendment; doing it implicitly through a rename would let an officer merge
// two crews by mistyping a name — D-82's own reasoning, which applies to the
// company heading at least as much (the company representative works their
// own sheet, D-16b/D-23/D-26).
type CompanyRenameRefusal struct {
	// Renamed is the company the officer was renaming, as composed.
	Renamed Company
	// Typed is the corrected name as asserted.
	Typed string
	// Existing is the OTHER company already holding that name, as composed —
	// Existing.Name is the on-list spelling, the row the officer should pick
	// (or merge into, with same_as).
	Existing Company
}

// Reason is the officer-facing sentence, without prefix or suffix — the
// same contract as CompanyAmendRefusal.Reason.
func (e *CompanyRenameRefusal) Reason() string {
	return fmt.Sprintf("company %s is already named %q; pick it instead of renaming %q onto that name — two companies cannot share one muster heading (merging them is the separate same_as amendment)",
		e.Existing.CompanyID, e.Existing.Name, e.Renamed.Name)
}

func (e *CompanyRenameRefusal) Error() string {
	return "domain: amend company: refused — " + e.Reason() + "; nothing was recorded"
}

// Unwrap places the rename refusal in the fact-domain class BY TYPE: an
// errors.As for *CompanyAmendRefusal walks to it.
func (e *CompanyRenameRefusal) Unwrap() error {
	return &CompanyAmendRefusal{Reason: e.Reason()}
}

// AmendCompany authors one company_amended event. EVERY refusal below is
// PRE-APPEND (W3-2, ridden at W5d-17): a refusal returns before Author is
// called, so the log length is unchanged and no envelope exists. These
// checks live at this edge and ONLY at this edge — the fold never refuses
// (see applyCompanyAmended).
//
// The nine fact-domain arms are W5d-22(b)'s enumerated set, derived from
// AmendPerson's ten with person arm 3 (unknown trade_id) having no company
// instance. The two guards above them — company_id required, and the
// company must exist — are INPUT-VALIDITY refusals: they carry no
// "refused —" spelling (their person-side analogues do not either), they
// are deliberately NOT counted among the nine, and they return
// *CompanyAmendInputError so a caller can tell them from both a fact-domain
// refusal and an internal fault. See that type for why the third class is
// not optional.
func AmendCompany(st *store.Store, siteID string, nodeEpoch int64, in CompanyAmendment, occurredAtMS int64, timeSource string) (store.Envelope, error) {
	if in.CompanyID == "" {
		return store.Envelope{}, refuseCompanyInput("a company id is required")
	}
	c, err := CompanyByID(st, in.CompanyID)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: amend company: %w", err)
	}
	if c == nil {
		return store.Envelope{}, refuseCompanyInput("%q is not a company on this post", in.CompanyID)
	}

	set := in.setFields()
	assertedCount := 0
	for _, v := range set {
		if v != nil {
			assertedCount++
		}
	}
	// Arm 1 — the no-op refusal: an amendment that asserts nothing and
	// clears nothing records nothing, and recording nothing must be
	// refused, not minted.
	if assertedCount == 0 && len(in.Clear) == 0 {
		return store.Envelope{}, refuseCompanyAmend("the amendment changes nothing (no field set, nothing cleared)")
	}
	// Arm 2 — name-to-empty. Company's ONLY content refusal, where person
	// has two: a company always has a name (company.name is TEXT NOT NULL),
	// and blanking it is refused here exactly as clearing it is below.
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return store.Envelope{}, refuseCompanyAmend("a company's name cannot be amended to empty")
	}
	// ⚠ Person arm 3 — unknown trade_id — has NO company instance: the
	// company amendable set is {name, same_as}, there is no trade_id limb
	// and no trade register. Recorded rather than silently skipped, because
	// it is also why W3-m2 is not ridden at W5d (SEAM-5), and because W7
	// and W8 must ask this question of their OWN entity rather than
	// inheriting the answer.
	for _, cf := range in.Clear {
		// Arm 4 — Clear:["name"], mirroring the person rule.
		if cf == "name" {
			return store.Envelope{}, refuseCompanyAmend("a company's name cannot be cleared")
		}
		// Arm 5 — a Clear naming a fact outside the amendable set. This is
		// the ONLY reachable form of "an out-of-set fact": setFields()
		// builds its map from the input struct's own fields, so a Set-side
		// assertion of an out-of-set fact is unrepresentable.
		if !isCompanyAmendableFact(cf) {
			return store.Envelope{}, refuseCompanyAmend("%q is not an amendable company field", cf)
		}
		// Arm 6 — the same fact both set and cleared in one amendment.
		if v := set[cf]; v != nil {
			return store.Envelope{}, refuseCompanyAmend("%q is both set and cleared in one amendment", cf)
		}
	}
	if in.SameAs != nil {
		target := *in.SameAs
		// Arm 7 — the trivial self-loop.
		if target == in.CompanyID {
			return store.Envelope{}, refuseCompanyAmend("a record cannot be merged with itself")
		}
		// Arm 8 — kind homogeneity, INVERTED against W3-18 (W5d-13):
		// company `same_as` joins COMPANY records only, so the target that
		// must be refused here is a NON-company. The id-kind-agnostic
		// deleted reference-merge table never checked this at all
		// (P-3 §2 fact 5).
		tc, err := CompanyByID(st, target)
		if err != nil {
			return store.Envelope{}, fmt.Errorf("domain: amend company: %w", err)
		}
		if tc == nil {
			return store.Envelope{}, refuseCompanyAmend("%q is not a company record; same_as joins company records only", target)
		}
		// Arm 9 — a standing prior merge. This store already composes an
		// answer for this record; a second, contradictory one is refused
		// here. (Two not-yet-synced posts can still legally author
		// competing merges — record-order composition converges them once
		// replicated, which is W5d-5's contested-merge fixture.)
		if c.SameAs != nil {
			return store.Envelope{}, refuseCompanyAmend("%s is already merged with %s; clear same_as (the un-merge) first", in.CompanyID, *c.SameAs)
		}
		// Arm 10 — the CYCLE. Would this edge close a loop back to the
		// record being amended? The walk is over the COMPOSED resolution
		// state, never a private snapshot — which is this loader's second
		// consumer (W5d-5b).
		//
		// ⚠ This arm is W5d-12's own stated premise: W3-17's cycle fixture
		// authors on primed scratch edges precisely because edges refuse
		// cycles locally and folds never refuse, so the cycle forms only on
		// the wire. Without this arm that premise is false for company.
		m, err := LoadCompanyMergeMap(st)
		if err != nil {
			return store.Envelope{}, fmt.Errorf("domain: amend company: %w", err)
		}
		cur := target
		for i := 0; i < maxMergeWalk; i++ {
			next, ok := m[cur]
			if !ok {
				break
			}
			if next == in.CompanyID {
				return store.Envelope{}, refuseCompanyAmend("merging %s into %s would close a cycle", in.CompanyID, target)
			}
			if next == cur {
				break
			}
			cur = next
		}
	}
	// Arm 11 — ⟦R-145, 2026-10-03⟧ the [[D-82]]-parity rename collision: a
	// corrected name that is already ANOTHER company's name, under the fold
	// the create doors use (sameCompanyName). Compared against the COMPOSED
	// list — the headings the muster prints — because a name that itself
	// arrived by rename exists only there, and a name freed by a rename is
	// free only there; and never against the company being renamed, which
	// holds its own name and every case variant of it. Refused, never merged
	// (see CompanyRenameRefusal). This is the tenth fact-domain arm beside
	// W5d-22(b)'s nine; none of the nine moved.
	if in.Name != nil {
		listed, err := Companies(st)
		if err != nil {
			return store.Envelope{}, fmt.Errorf("domain: amend company: %w", err)
		}
		for _, other := range listed {
			if other.CompanyID == in.CompanyID || !sameCompanyName(other.Name, *in.Name) {
				continue
			}
			return store.Envelope{}, &CompanyRenameRefusal{Renamed: *c, Typed: *in.Name, Existing: other}
		}
	}

	// The `seen` stamp ([[D-92]], SchemaVersion 11): every fact this
	// amendment asserts or clears, resolved over the record set THIS post
	// holds right now — computed after every refusal above (a refused
	// amendment never reads the table for it) and before Author (so the
	// stamp describes the state the author actually saw). amendmentSeen
	// (contest.go) is the one spelling of what it names; CompanyAmendment
	// has no field a caller could supply it through.
	rows, err := readCompanyAmendments(st)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: amend company: %w", err)
	}
	var facts []string
	for _, fact := range companyAmendableFacts {
		if set[fact] != nil {
			facts = append(facts, fact)
		}
	}
	facts = append(facts, in.Clear...)
	seen := amendmentSeen(rows, companyMergeMapFromRows(rows), in.CompanyID, facts)

	payload, err := json.Marshal(CompanyAmended{
		Type: evtCompanyAmended, CompanyID: in.CompanyID,
		Name: in.Name, SameAs: in.SameAs, Clear: in.Clear,
		Seen: seen,
	})
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: amend company: %w", err)
	}
	env, err := st.Author(siteID, nodeEpoch, payload, occurredAtMS, timeSource)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: amend company: %w", err)
	}
	return env, nil
}
