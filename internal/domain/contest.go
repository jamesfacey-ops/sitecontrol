// contest.go — the D-92 machinery every amendment carrier SHARES at
// SchemaVersion 11: what a side-table row must expose to be judged by
// resolveContest (person_amend.go — THE ONE rule, still spelled there), the
// contest-scope and `seen`-stamp helpers the seven authoring edges and the
// seven resolution passes read, and the one codec for the `seen` column.
//
// Why this file exists, stated once so a clone author does not undo it:
// person_amend.go's file comment says the rule is "spelled ONCE, in
// resolveContest below; composition, Outcome, Competitors and the merge
// snapshot all read it from there". Step 3 of the 10 → 11 increment
// (reports/witness/schema-reconcile.md §4) clones the causal FIELD onto
// company → trade → vehicle → trip + arrival, then `occurred_at_corrected`.
// A byte-level clone of resolveContest per carrier would leave the ordering
// that decides a muster heading spelled SEVEN times — the same drift surface
// the one `ORDER BY` spelling and W3-11's T-A oracle exist to refuse, one
// layer up. So the rule is made generic over the small interface below and
// each carrier's row type implements it in three one-line methods. This is
// deliberately NOT DR-7's carrier-registration generic (ruled not spent at
// §42.3): that licence is about the rendering/extraction pairs on
// carrierRegistration; this is about the rule itself, which was always
// meant to be one.
//
// The stamp rule (what `seen` names) is spelled once here too, for the same
// reason: two authors at two posts who saw the same state must stamp
// byte-identical lists whatever entity they were correcting.
package domain

import (
	"database/sql"
	"encoding/json"
	"sort"

	"sitecontrol/internal/store"
)

// contestant is what resolveContest needs of a row: its own key, and the
// keys of the assertions its author had already seen standing ([[D-92]]).
// A row whose contestSeen is empty is one whose author saw nothing standing
// (or a pre-11-shaped row from a divergent peer) and competes on key order
// alone — the genuinely-concurrent case, and the pre-11 meaning of every row.
type contestant interface {
	contestKey() store.Key
	contestSeen() []store.Key
}

// amendmentRow is what the shared contest-scope and stamp helpers need of an
// AMENDMENT row beyond contestant: which record it targets, and whether it
// asserts (sets or clears) a given fact. The correction carrier is a
// contestant but not an amendmentRow — its contest is W4-11's target chain,
// not a target-id scope (time_correct.go).
type amendmentRow interface {
	contestant
	targetID() string
	asserts(fact string) bool
}

// rowsAsserting narrows rows to the contest for ONE fact: the rows that set
// or clear it, in the order given (the substrate reads deliver ascending key
// order, and this keeps it).
func rowsAsserting[R amendmentRow](rows []R, fact string) []R {
	var out []R
	for _, r := range rows {
		if r.asserts(fact) {
			out = append(out, r)
		}
	}
	return out
}

// amendmentContest returns ONE fact's contested rows for a target record, in
// the scope Outcome and Competitors have always used (W3-9/W3-10, spelled
// once here for the read AND the authoring edge of every carrier that has a
// merge pointer):
//   - same_as (the merge pointer) is contested among assertions targeting
//     the SAME record — resolving it through the merge map first would
//     erase the very fact under judgement;
//   - every VALUE fact is contested across the assertion sets a merge
//     JOINS (§6 case 5): the rows whose target resolves, through m, to the
//     same representative the target does.
//
// A carrier with no merge pointer (trip, arrival) passes a nil map: Resolve
// is then the identity and the scope is the record's own rows, which is
// F4's contest scope exactly. rows must already be in ascending key order;
// the result keeps it.
func amendmentContest[R amendmentRow](rows []R, m AliasMap, rep, target, fact string) []R {
	var out []R
	for _, r := range rows {
		if !r.asserts(fact) {
			continue
		}
		if fact == "same_as" {
			if r.targetID() != target {
				continue
			}
		} else if m.Resolve(r.targetID()) != rep {
			continue
		}
		out = append(out, r)
	}
	return out
}

// amendmentSeen computes the `seen` list an authoring edge stamps ([[D-92]]):
// for every fact the amendment asserts or clears, the undominated heads of
// that fact's contest as this post holds it right now — at the target's own
// scope (what the composed read shows for this record) AND at the
// merge-group scope (what Outcome judges), unioned, because the two contests
// can have different heads and the author has seen both. Referencing every
// head, not only the current winner, is what keeps a tiebreak loser from
// resurfacing later: everything present here is either a head (named
// directly) or already dominated by one.
//
// The result is sorted ascending by key and de-duplicated, so two authors
// who saw the same state stamp byte-identical lists.
func amendmentSeen[R amendmentRow](rows []R, m AliasMap, target string, facts []string) []SeenRef {
	rep := m.Resolve(target)
	var own []R
	for _, r := range rows {
		if r.targetID() == target {
			own = append(own, r)
		}
	}
	var heads []store.Key
	for _, fact := range facts {
		_, h := resolveContest(rowsAsserting(own, fact))
		heads = append(heads, h...)
		_, h = resolveContest(amendmentContest(rows, m, rep, target, fact))
		heads = append(heads, h...)
	}
	return seenRefsOf(heads)
}

// seenRefsOf renders keys as wire references: ascending by key,
// de-duplicated, nil when there is nothing to name (so `omitempty` drops the
// key from an author who saw nothing standing — the pre-11 wire shape).
func seenRefsOf(keys []store.Key) []SeenRef {
	set := map[store.Key]bool{}
	for _, k := range keys {
		set[k] = true
	}
	sorted := make([]store.Key, 0, len(set))
	for k := range set {
		sorted = append(sorted, k)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Less(sorted[j]) })
	if len(sorted) == 0 {
		return nil
	}
	out := make([]SeenRef, 0, len(sorted))
	for _, k := range sorted {
		out = append(out, seenRefOf(k))
	}
	return out
}

// seenColumnValue is the fold's half of the `seen` codec, one spelling for
// every side table. `seen` lands on the same tolerant terms as `clear`: a
// well-formed list is stored re-encoded in canonical form; anything else —
// absent, empty, malformed — lands as NULL, which every read decodes as
// "this author saw nothing standing". The fold never refuses (W3-2).
func seenColumnValue(raw map[string]json.RawMessage) (any, error) {
	r, ok := raw["seen"]
	if !ok {
		return nil, nil
	}
	var refs []SeenRef
	if err := json.Unmarshal(r, &refs); err != nil || len(refs) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(refs)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// decodeSeenColumn is the read's half of the codec: a NULL, empty or
// unreadable column is no references, never an error — the row is present
// (the record set keeps it) and competes on key order alone.
func decodeSeenColumn(col sql.NullString) []store.Key {
	if !col.Valid || col.String == "" {
		return nil
	}
	var refs []SeenRef
	if err := json.Unmarshal([]byte(col.String), &refs); err != nil {
		return nil
	}
	var out []store.Key
	for _, ref := range refs {
		out = append(out, ref.key())
	}
	return out
}
