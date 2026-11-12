// d92_dominance_oracle_test.go — schema-reconcile.md §4 item 3a: the T-A
// oracle SIBLING for the dominance layer (d90-supersession.md §6, "a sibling
// to the W3-11 T-A oracle for the dominance layer", a named consequence of
// [[D-90]]/[[D-92]]). W3-11's oracle proves the Go comparator (store.Key.Less)
// and the SQL ORDER BY agree; it is LOCKED for that comparator. At
// SchemaVersion 11 the winner is decided ABOVE key order, by resolveContest
// (person_amend.go), and every surface that could have cross-checked it —
// composition, Outcome, Competitors, the merge snapshot, the `seen` stamp —
// reads the rule from that one function. They agree BY CONSTRUCTION, so
// their agreement certifies nothing about the rule (reviewer-step2: "they'd
// agree if both wrong"). This file supplies the second, INDEPENDENT
// implementation and contests the two on every case.
//
// HOW THE TWO ARE KEPT INDEPENDENT (the headline):
//
//  1. THE REFERENCE IS A CHECKER, NOT A COMPUTER, written from the RULE'S
//     SENTENCES ([[D-92]]: "a correction made in full knowledge of an
//     earlier one wins; site_id breaks ties only for genuinely concurrent
//     edits"; resolveContest's doc rules 1–3 as SPECIFICATION), never from
//     its body. Production filters the dominated out of an already-sorted
//     slice and takes the LAST element. The reference decides, per member,
//     the predicate "no OTHER member's `seen` names me" and then requires
//     the winner to be the UNIQUE pool member that every other pool member
//     compares below — and it FAILS if that member is not unique (a
//     totality/antisymmetry check production never performs).
//  2. ITS OWN KEY SPELLING AND COMPARATOR. d92RefKey/d92RefCompare are
//     written from §8.3's sentence ("lexicographic (SiteID, NodeEpoch,
//     Seq), epoch-major"); the reference never calls store.Key.Less, never
//     sorts with it, and never relies on input order — it is fed the log in
//     CLOCK order (d92RefReadLog), the one order the rule must ignore.
//  3. ITS OWN INPUT PIPELINE. Production reads `person_amendment` — the
//     side table the FOLD wrote, re-encoded, decoded by
//     queryPersonAmendments, scoped by personContest/personRowsAsserting,
//     grouped by mergeMapFromRows/AliasMap.Resolve. The reference reads the
//     RAW `event_log` payload bytes with its own JSON shape, derives the
//     fact list from PersonAmended's wire tags by reflection (not from
//     personAmendableFacts), builds both scopes itself, and follows the
//     merge chain itself. A fold that drops `seen`, a decode that mangles
//     it, a scope filter that forgets the group, a snapshot that resolves
//     wrongly — each is a DISAGREEMENT here, not a shared blind spot.
//  4. TWO HALVES, so neither half's blindness is inherited: the truth table
//     (§A) feeds resolveContest DIRECTLY with every seen-configuration of
//     2- and 3-member contests over five key layouts (~12k cases: A-saw-B,
//     B-saw-A, concurrent, mutual/rule 3, chains, all-dominated cycles,
//     self- and foreign references, cross-site, cross-epoch) — a blind spot
//     no fixture author would think to pose is enumerated anyway; the
//     fixture oracle (§B) drives REAL stores through the public surfaces
//     (Outcome, Competitors, PersonByID, LoadPersonMergeMap, AmendPerson's
//     stamp), which is where a clock smuggled in by a side channel (FA1) or
//     a lost `seen` (fold/decode) can only be seen.
//
// THE TWO MEASURED BLIND SPOTS THIS ORACLE DOES NOT INHERIT:
//   - GROUP SCOPE (STEP3-BLINDSPOTS §3: dropping the stamp's merge-group
//     union left all step-2 witnesses green). The reference computes the
//     stamp as the UNION of target-scope and group-scope heads and checks
//     it at authoring time (d92OracleAmend); Outcome/Competitors are
//     checked against the GROUP contest and the composed read against the
//     TARGET contest, per surface. That is also why this sibling is not
//     W3-11's iff: the W5d merged-away corner, which W3-11 must EXCLUDE
//     ("deliberately NOT fed to the T-A oracle", w5d_merge_readers_test.go)
//     because Outcome and composition are RULED to differ there, is
//     INCLUDED here (§B "merge-group scope"), each surface judged at its
//     own scope.
//   - A CLOCK IN THE TIEBREAK (TESTER-step2 §6 FA1: passed the scramble
//     and the source scan). §B's concurrent pairs put the GREATER key on
//     the EARLIER clock, guarded from the read-back log, and re-run with
//     the two time bases SWAPPED; the truth table feeds no clock at all.
//     THE CLOCK ORDERS NOTHING ([[D-90]]/[[D-92]]).
//
// KILLER MUTANTS, each PRODUCED red against this tree before commit
// (reports/witness/STEP3A-ORACLE-SIBLING.md §4): resolveContest ignores
// `seen`; least-key tiebreak; survivor-only domination; rule 3 to the FIRST
// key; personSeen drops the group half; Outcome contests at target scope;
// a clock shadow in the tiebreak (FA1); the fold drops `seen`.
//
// TESTS ONLY. The three tests here are
// TestD92DominanceOracleTruthTableOverEveryContestedCase,
// TestD92DominanceOracleOverContestedFixtures and
// TestD92DominanceOracleOverTheStandingFixtures.
package domain

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// =======================================================================
// The reference implementation — the rule as STATED, in its own terms
// =======================================================================

// d92RefKey is the reference's own key spelling. It is deliberately NOT
// store.Key: a conversion boundary (d92ToRef/d92FromRef) is the only place
// the two meet, and the reference never compares through store.Key.Less.
type d92RefKey struct {
	Site  string
	Epoch int64
	Seq   int64
}

func (k d92RefKey) String() string { return fmt.Sprintf("%s/e%d/s%d", k.Site, k.Epoch, k.Seq) }

func d92ToRef(k store.Key) d92RefKey { return d92RefKey{k.SiteID, k.NodeEpoch, k.Seq} }
func d92FromRef(k d92RefKey) store.Key {
	return store.Key{SiteID: k.Site, NodeEpoch: k.Epoch, Seq: k.Seq}
}

// d92RefCompare is the reference's three-way comparison, written from §8.3's
// sentence — lexicographic (site_id, node_epoch, seq), epoch-major — and
// from nothing in envelope.go. W3-11's truth table is the control on the
// production comparator; this one is the control's own spelling.
func d92RefCompare(a, b d92RefKey) int {
	if c := strings.Compare(a.Site, b.Site); c != 0 {
		return c
	}
	switch {
	case a.Epoch < b.Epoch:
		return -1
	case a.Epoch > b.Epoch:
		return 1
	}
	switch {
	case a.Seq < b.Seq:
		return -1
	case a.Seq > b.Seq:
		return 1
	}
	return 0
}

func d92RefSortKeys(ks []d92RefKey) []d92RefKey {
	out := append([]d92RefKey(nil), ks...)
	sort.Slice(out, func(i, j int) bool { return d92RefCompare(out[i], out[j]) < 0 })
	return out
}

// d92RefRow is one amendment as the reference reads it off the LOG payload:
// its key, its target, the facts it asserts (nil value = cleared), and the
// keys its author named. Clock is carried for the FIXTURE GUARDS only (the
// clock rival must be recomputable from the log); the resolver never reads
// it, and the truth table never sets it.
type d92RefRow struct {
	Key    d92RefKey
	Target string
	Facts  map[string]*string
	Seen   []d92RefKey
	Clock  int64
}

func (r d92RefRow) asserts(fact string) bool { _, ok := r.Facts[fact]; return ok }

// d92RefFacts derives the amendable fact list from PersonAmended's WIRE
// tags — every `*string` field's json name — not from personAmendableFacts.
func d92RefFacts() []string {
	var out []string
	typ := reflect.TypeOf(PersonAmended{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Type != reflect.TypeOf((*string)(nil)) {
			continue
		}
		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		if tag != "" {
			out = append(out, tag)
		}
	}
	return out
}

// d92RefSeenOf decodes a `seen` list off raw payload bytes in the
// reference's own shape. An unreadable list is "saw nothing standing" —
// the LIST is the unit (the step-2 pinned contract,
// TestW3WireFormedSeenCycleAndMalformedSeenKeepTheReadTotal); a partial
// entry decodes to a key that names nothing.
func d92RefSeenOf(payload []byte) []d92RefKey {
	var raw struct {
		Seen json.RawMessage `json:"seen"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil || len(raw.Seen) == 0 {
		return nil
	}
	var refs []struct {
		Site  string `json:"site_id"`
		Epoch int64  `json:"node_epoch"`
		Seq   int64  `json:"seq"`
	}
	if err := json.Unmarshal(raw.Seen, &refs); err != nil {
		return nil
	}
	var out []d92RefKey
	for _, r := range refs {
		out = append(out, d92RefKey{r.Site, r.Epoch, r.Seq})
	}
	return out
}

// d92RefDecode reads one log payload; false when it is not a person
// amendment.
func d92RefDecode(k d92RefKey, clock int64, payload []byte, facts []string) (d92RefRow, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		return d92RefRow{}, false
	}
	var typ string
	if err := json.Unmarshal(raw["type"], &typ); err != nil || typ != "person_amended" {
		return d92RefRow{}, false
	}
	row := d92RefRow{Key: k, Clock: clock, Facts: map[string]*string{}}
	if err := json.Unmarshal(raw["person_id"], &row.Target); err != nil {
		return d92RefRow{}, false
	}
	known := map[string]bool{}
	for _, f := range facts {
		known[f] = true
		if b, ok := raw[f]; ok {
			var v string
			if err := json.Unmarshal(b, &v); err == nil {
				row.Facts[f] = &v
			}
		}
	}
	if b, ok := raw["clear"]; ok {
		var list []string
		if err := json.Unmarshal(b, &list); err == nil {
			for _, c := range list {
				if known[c] {
					if _, set := row.Facts[c]; !set {
						row.Facts[c] = nil
					}
				}
			}
		}
	}
	row.Seen = d92RefSeenOf(payload)
	return row, true
}

// d92RefVerdict is one contest's answer under the reference.
type d92RefVerdict struct {
	Winner   d92RefKey
	Heads    []d92RefKey // undominated members, ascending under d92RefCompare
	Fallback bool        // rule 3: every member dominated
}

// d92RefResolve is THE RULE as stated, decided by predicate:
//
//	dominated(m)  := some OTHER member's `seen` names m.Key        (rule 1)
//	pool          := {m : !dominated(m)}, or every member if that is empty (rule 3)
//	winner        := the UNIQUE p in pool with compare(o, p) < 0 for every other o in pool (rule 2)
//
// It fails (never guesses) if the winner is not unique. It reads no clock,
// no delivery order and no input order.
func d92RefResolve(t *testing.T, contest []d92RefRow) (d92RefVerdict, bool) {
	t.Helper()
	if len(contest) == 0 {
		return d92RefVerdict{}, false
	}
	dominated := func(m d92RefRow) bool {
		for _, o := range contest {
			if o.Key == m.Key {
				continue
			}
			for _, s := range o.Seen {
				if s == m.Key {
					return true
				}
			}
		}
		return false
	}
	var v d92RefVerdict
	var pool []d92RefKey
	for _, m := range contest {
		if !dominated(m) {
			pool = append(pool, m.Key)
		}
	}
	v.Heads = d92RefSortKeys(pool)
	if len(pool) == 0 {
		v.Fallback = true
		for _, m := range contest {
			pool = append(pool, m.Key)
		}
	}
	var winners []d92RefKey
	for _, c := range pool {
		greatest := true
		for _, o := range pool {
			if o != c && d92RefCompare(o, c) >= 0 {
				greatest = false
				break
			}
		}
		if greatest {
			winners = append(winners, c)
		}
	}
	if len(winners) != 1 {
		t.Fatalf("REFERENCE DEFECT: %d greatest members in pool %v — the order is not total/antisymmetric on this contest", len(winners), pool)
	}
	v.Winner = winners[0]
	return v, true
}

// d92RefLabel names a contest's shape so a disagreement says WHICH case.
func d92RefLabel(contest []d92RefRow) string {
	in := map[d92RefKey]bool{}
	for _, m := range contest {
		in[m.Key] = true
	}
	names := func(a, b d92RefRow) bool {
		for _, s := range a.Seen {
			if s == b.Key {
				return true
			}
		}
		return false
	}
	var parts []string
	sites, epochs := map[string]bool{}, map[string]map[int64]bool{}
	selfRef, foreignRef := false, false
	for _, m := range contest {
		sites[m.Site()] = true
		if epochs[m.Site()] == nil {
			epochs[m.Site()] = map[int64]bool{}
		}
		epochs[m.Site()][m.Key.Epoch] = true
		for _, s := range m.Seen {
			if s == m.Key {
				selfRef = true
			} else if !in[s] {
				foreignRef = true
			}
		}
	}
	if len(contest) == 2 {
		a, b := contest[0], contest[1]
		switch {
		case names(a, b) && names(b, a):
			parts = append(parts, "MUTUAL (both name each other, rule 3)")
		case names(a, b):
			parts = append(parts, fmt.Sprintf("%v SAW %v", a.Key, b.Key))
		case names(b, a):
			parts = append(parts, fmt.Sprintf("%v SAW %v", b.Key, a.Key))
		default:
			parts = append(parts, "CONCURRENT (neither saw the other, tiebreak)")
		}
	} else {
		edges := 0
		for _, a := range contest {
			for _, b := range contest {
				if a.Key != b.Key && names(a, b) {
					edges++
					parts = append(parts, fmt.Sprintf("%v saw %v", a.Key, b.Key))
				}
			}
		}
		if edges == 0 {
			parts = append(parts, "ALL CONCURRENT (tiebreak)")
		}
	}
	if len(sites) > 1 {
		parts = append(parts, "cross-site")
	}
	for _, es := range epochs {
		if len(es) > 1 {
			parts = append(parts, "cross-epoch")
			break
		}
	}
	if selfRef {
		parts = append(parts, "self-reference")
	}
	if foreignRef {
		parts = append(parts, "foreign reference")
	}
	return strings.Join(parts, "; ")
}

func (r d92RefRow) Site() string { return r.Key.Site }

// d92RefState is the reference's view of ONE post's whole record set.
type d92RefState struct {
	rows    []d92RefRow
	facts   []string
	persons []string
}

// d92RefReadLog reads the RAW log — every stream, every epoch — in CLOCK
// order, deliberately not key order: the reference must not care.
func d92RefReadLog(t *testing.T, st *store.Store) []d92RefRow {
	t.Helper()
	facts := d92RefFacts()
	var out []d92RefRow
	err := st.LocalTx(func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT site_id, node_epoch, seq, authored_at_wall_ms, payload
		                         FROM event_log
		                        ORDER BY authored_at_wall_ms, site_id DESC, node_epoch DESC, seq DESC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var k d92RefKey
			var clock int64
			var payload string
			if err := rows.Scan(&k.Site, &k.Epoch, &k.Seq, &clock, &payload); err != nil {
				return err
			}
			if r, ok := d92RefDecode(k, clock, []byte(payload), facts); ok {
				out = append(out, r)
			}
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("reference: read event_log: %v", err)
	}
	return out
}

func d92RefFromStore(t *testing.T, st *store.Store) d92RefState {
	t.Helper()
	s := d92RefState{rows: d92RefReadLog(t, st), facts: d92RefFacts()}
	ps, err := Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		s.persons = append(s.persons, p.PersonID)
	}
	return s
}

func (s d92RefState) row(k d92RefKey) (d92RefRow, bool) {
	for _, r := range s.rows {
		if r.Key == k {
			return r, true
		}
	}
	return d92RefRow{}, false
}

// targetContest: the rows targeting `target` that assert or clear `fact`.
func (s d92RefState) targetContest(target, fact string) []d92RefRow {
	var out []d92RefRow
	for _, r := range s.rows {
		if r.Target == target && r.asserts(fact) {
			out = append(out, r)
		}
	}
	return out
}

// effectiveSameAs: the winner of p's own same_as contest, when it is a SET
// to another record (a winning clear is no edge; a self-loop is no edge).
func (s d92RefState) effectiveSameAs(t *testing.T, p string) (string, bool) {
	t.Helper()
	v, ok := d92RefResolve(t, s.targetContest(p, "same_as"))
	if !ok {
		return "", false
	}
	w, _ := s.row(v.Winner)
	val := w.Facts["same_as"]
	if val == nil || *val == p {
		return "", false
	}
	return *val, true
}

// rep follows effective same_as edges to the representative. A cycle is a
// FIXTURE defect for this oracle: collapsing one is W3-17's derived rule,
// not [[D-92]]'s, and this file contests only the latter.
func (s d92RefState) rep(t *testing.T, p string) string {
	t.Helper()
	cur, hops := p, map[string]bool{}
	for {
		if hops[cur] {
			t.Fatalf("fixture not posed for this oracle: same_as cycle through %q (cycle collapse is W3-17's rule, not D-92's)", cur)
		}
		hops[cur] = true
		next, ok := s.effectiveSameAs(t, cur)
		if !ok {
			return cur
		}
		cur = next
	}
}

// groupContest is Outcome's scope: same_as among rows targeting the SAME
// record; every value fact across the rows whose target shares the
// target's representative.
func (s d92RefState) groupContest(t *testing.T, target, fact string) []d92RefRow {
	t.Helper()
	if fact == "same_as" {
		return s.targetContest(target, fact)
	}
	rep := s.rep(t, target)
	var out []d92RefRow
	for _, r := range s.rows {
		if r.asserts(fact) && s.rep(t, r.Target) == rep {
			out = append(out, r)
		}
	}
	return out
}

// stamp is what AmendPerson must write: for every asserted fact, the heads
// of the TARGET contest and the heads of the GROUP contest, unioned, sorted,
// de-duplicated.
func (s d92RefState) stamp(t *testing.T, target string, facts []string) []d92RefKey {
	t.Helper()
	set := map[d92RefKey]bool{}
	for _, f := range facts {
		for _, c := range [][]d92RefRow{s.targetContest(target, f), s.groupContest(t, target, f)} {
			if v, ok := d92RefResolve(t, c); ok {
				for _, h := range v.Heads {
					set[h] = true
				}
			}
		}
	}
	var out []d92RefKey
	for k := range set {
		out = append(out, k)
	}
	return d92RefSortKeys(out)
}

func d92RefKeysEqual(a, b []d92RefKey) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func d92RefKeysOf(ks []store.Key) []d92RefKey {
	var out []d92RefKey
	for _, k := range ks {
		out = append(out, d92ToRef(k))
	}
	return out
}

// =======================================================================
// §A — the truth table: every seen-configuration of small contests
// =======================================================================

// TestD92DominanceOracleTruthTableOverEveryContestedCase feeds
// resolveContest DIRECTLY (the way W3-11's truth table feeds Key.Less)
// with EVERY assignment of `seen` lists over 2- and 3-member contests on
// five key layouts — each member may name any subset of {the others,
// itself, a key outside the contest} — and requires winner AND heads to
// equal the reference's on every one. The enumeration is proven to have
// reached each named shape (A-saw-B, B-saw-A, concurrent, mutual/rule 3,
// chain, all-dominated) by counting, so a green cannot be a green over an
// empty class.
func TestD92DominanceOracleTruthTableOverEveryContestedCase(t *testing.T) {
	k := func(s string, e, q int64) d92RefKey { return d92RefKey{s, e, q} }
	layouts := []struct {
		name string
		keys []d92RefKey
	}{
		{"one stream (a/1: seq 1,2,3)", []d92RefKey{k("site-a", 1, 1), k("site-a", 1, 2), k("site-a", 1, 3)}},
		{"three sites (a,b,c each e1/s1)", []d92RefKey{k("site-a", 1, 1), k("site-b", 1, 1), k("site-c", 1, 1)}},
		{"cross-epoch + cross-site (a/1/900, a/2/3, b/1/1)", []d92RefKey{k("site-a", 1, 900), k("site-a", 2, 3), k("site-b", 1, 1)}},
		{"the standing cross-epoch CONTROL pair (a/1/900 vs a/2/3)", []d92RefKey{k("site-a", 1, 900), k("site-a", 2, 3)}},
		{"two sites (a/1/1 vs b/1/1)", []d92RefKey{k("site-a", 1, 1), k("site-b", 1, 1)}},
	}
	foreign := k("site-zz", 9, 9)
	classes := map[string]int{}
	total := 0
	for _, lay := range layouts {
		n := len(lay.keys)
		// Per member: the candidate references are the OTHER members, itself,
		// and the foreign key — 2^(n+1) subsets each.
		candidates := make([][]d92RefKey, n)
		for i := range lay.keys {
			for j := range lay.keys {
				if i != j {
					candidates[i] = append(candidates[i], lay.keys[j])
				}
			}
			candidates[i] = append(candidates[i], lay.keys[i], foreign)
		}
		per := 1 << (n + 1)
		combos := 1
		for i := 0; i < n; i++ {
			combos *= per
		}
		for c := 0; c < combos; c++ {
			contest := make([]d92RefRow, n)
			rem := c
			for i := 0; i < n; i++ {
				mask := rem % per
				rem /= per
				var seen []d92RefKey
				for b := 0; b < n+1; b++ {
					if mask&(1<<b) != 0 {
						seen = append(seen, candidates[i][b])
					}
				}
				val := fmt.Sprintf("v-%v", lay.keys[i])
				contest[i] = d92RefRow{Key: lay.keys[i], Target: "p", Facts: map[string]*string{"phone": &val}, Seen: seen}
			}
			ref, _ := d92RefResolve(t, contest)
			label := d92RefLabel(contest)

			// Production, on its own contract: rows in ascending Key.Less order.
			prodRows := make([]personAmendmentRow, 0, n)
			for _, m := range contest {
				r := personAmendmentRow{Key: d92FromRef(m.Key), PersonID: m.Target, Set: map[string]string{"phone": *m.Facts["phone"]}}
				for _, s := range m.Seen {
					r.Seen = append(r.Seen, d92FromRef(s))
				}
				prodRows = append(prodRows, r)
			}
			sort.Slice(prodRows, func(i, j int) bool { return prodRows[i].Key.Less(prodRows[j].Key) })
			winner, survivors := resolveContest(prodRows)

			if d92ToRef(winner) != ref.Winner {
				t.Fatalf("DOMINANCE ORACLE DISAGREEMENT (truth table) — layout %q, case [%s]: %s\n  resolveContest winner = %v\n  reference winner      = %v (heads %v, rule-3 fallback=%v)",
					lay.name, label, d92DescribeSeen(contest), d92ToRef(winner), ref.Winner, ref.Heads, ref.Fallback)
			}
			if !d92RefKeysEqual(d92RefKeysOf(survivors), ref.Heads) {
				t.Fatalf("DOMINANCE ORACLE DISAGREEMENT (truth table, heads) — layout %q, case [%s]: %s\n  resolveContest survivors = %v\n  reference heads          = %v",
					lay.name, label, d92DescribeSeen(contest), d92RefKeysOf(survivors), ref.Heads)
			}
			total++
			// Classes are counted NON-exclusively: a case may be several.
			if ref.Fallback {
				classes["all-dominated (rule 3)"]++
			}
			if strings.HasPrefix(label, "CONCURRENT") || strings.HasPrefix(label, "ALL CONCURRENT") {
				classes["concurrent (tiebreak)"]++
			}
			if strings.HasPrefix(label, "MUTUAL") {
				classes["mutual pair (rule 3)"]++
			}
			if n == 2 && strings.Contains(label, " SAW ") {
				classes["one saw the other"]++
			}
			if n == 3 && strings.Contains(label, " saw ") {
				classes["3-member with references (chains included)"]++
			}
			if n == 3 && d92IsChain(contest) {
				classes["strict chain A<-B<-C"]++
			}
		}
	}
	for _, want := range []string{"all-dominated (rule 3)", "concurrent (tiebreak)", "mutual pair (rule 3)", "one saw the other", "3-member with references (chains included)", "strict chain A<-B<-C"} {
		if classes[want] == 0 {
			t.Fatalf("truth table defect: class %q was never generated — the enumeration does not reach it", want)
		}
	}
	t.Logf("truth table: %d contested cases agreed; classes %v", total, classes)
}

// d92IsChain reports a 3-member contest in which exactly two references
// exist and they form a path (x names y, y names z, x does not name z).
func d92IsChain(contest []d92RefRow) bool {
	if len(contest) != 3 {
		return false
	}
	names := map[[2]d92RefKey]bool{}
	edges := 0
	for _, a := range contest {
		for _, s := range a.Seen {
			if s != a.Key {
				for _, b := range contest {
					if b.Key == s {
						names[[2]d92RefKey{a.Key, b.Key}] = true
						edges++
					}
				}
			}
		}
	}
	if edges != 2 {
		return false
	}
	for _, x := range contest {
		for _, y := range contest {
			for _, z := range contest {
				if x.Key != y.Key && y.Key != z.Key && x.Key != z.Key &&
					names[[2]d92RefKey{x.Key, y.Key}] && names[[2]d92RefKey{y.Key, z.Key}] && !names[[2]d92RefKey{x.Key, z.Key}] {
					return true
				}
			}
		}
	}
	return false
}

func d92DescribeSeen(contest []d92RefRow) string {
	var parts []string
	for _, m := range contest {
		parts = append(parts, fmt.Sprintf("%v.seen=%v", m.Key, m.Seen))
	}
	return strings.Join(parts, " ")
}

// =======================================================================
// §B — the fixture oracle: real stores, public surfaces, both scopes
// =======================================================================

// d92PersonField reads one composed fact off a Person by its wire name.
func d92PersonField(p *Person, fact string) (string, bool) {
	switch fact {
	case "name":
		return p.Name, true
	case "last_name":
		return d92Deref(p.LastName)
	case "first_name":
		return d92Deref(p.FirstName)
	case "phone":
		return d92Deref(p.Phone)
	case "company_id":
		return d92Deref(p.CompanyID)
	case "trade_id":
		return d92Deref(p.TradeID)
	case "role":
		return d92Deref(p.Role)
	case "position":
		return d92Deref(p.Position)
	case "retired":
		return d92Deref(p.Retired)
	case "same_as":
		return d92Deref(p.SameAs)
	}
	return "", false
}

func d92Deref(s *string) (string, bool) {
	if s == nil {
		return "", false
	}
	return *s, true
}

// d92RunDominanceOracle contests every production surface of one post
// against the reference: Outcome and Competitors (GROUP scope) for every
// asserted fact of every amendment; the composed read (TARGET scope) for
// every person and fact; the merge snapshot for every person. It also
// checks §5's C-8 precondition at runtime: values pairwise distinct per
// fact, so a composed value identifies its winning key.
func d92RunDominanceOracle(t *testing.T, st *store.Store, label string) {
	t.Helper()
	ref := d92RefFromStore(t, st)
	if len(ref.rows) == 0 {
		t.Fatalf("%s: oracle fed no amendments — nothing contested", label)
	}
	// Precondition: pairwise-distinct SET values per fact (a clear is one
	// more distinct outcome: absent).
	seenVal := map[string]map[string]d92RefKey{}
	for _, r := range ref.rows {
		for f, v := range r.Facts {
			if v == nil {
				continue
			}
			if seenVal[f] == nil {
				seenVal[f] = map[string]d92RefKey{}
			}
			if prev, dup := seenVal[f][*v]; dup {
				t.Fatalf("%s: ORACLE FIXTURE DEFECT: fact %q value %q asserted twice (%v, %v) — the composed value could not identify its winner", label, f, *v, prev, r.Key)
			}
			seenVal[f][*v] = r.Key
		}
	}

	// (1) Outcome + Competitors, group scope.
	for _, r := range ref.rows {
		k := d92FromRef(r.Key)
		outs, err := Outcome(st, k)
		if err != nil {
			t.Fatalf("%s: Outcome(%v): %v", label, r.Key, err)
		}
		comp, err := Competitors(st, k)
		if err != nil {
			t.Fatalf("%s: Competitors(%v): %v", label, r.Key, err)
		}
		for fact := range r.Facts {
			scope := ref.groupContest(t, r.Target, fact)
			v, _ := d92RefResolve(t, scope)
			var out *FactOutcome
			for i := range outs {
				if outs[i].Fact == fact {
					out = &outs[i]
				}
			}
			if out == nil {
				t.Fatalf("%s: Outcome(%v) reported no %q fact (got %+v) — cannot-match is a FAILURE, never a skip", label, r.Key, fact, outs)
			}
			wantEffective := v.Winner == r.Key
			if (out.Status == Effective) != wantEffective || d92ToRef(out.Winner) != v.Winner {
				t.Fatalf("%s: DOMINANCE ORACLE DISAGREEMENT (Outcome, group scope) — case [%s]\n  Outcome(%v).%s = (%v, winner %v)\n  reference: winner %v, heads %v, rule-3 fallback=%v\n  contest: %s",
					label, d92RefLabel(scope), r.Key, fact, out.Status, d92ToRef(out.Winner), v.Winner, v.Heads, v.Fallback, d92DescribeSeen(scope))
			}
			var wantComp []d92RefKey
			for _, m := range scope {
				if m.Key != r.Key {
					wantComp = append(wantComp, m.Key)
				}
			}
			wantComp = d92RefSortKeys(wantComp)
			if got := d92RefKeysOf(comp[fact]); !d92RefKeysEqual(got, wantComp) {
				t.Fatalf("%s: DOMINANCE ORACLE DISAGREEMENT (Competitors, group scope) — case [%s]: Competitors(%v)[%s] = %v, reference contest minus self = %v",
					label, d92RefLabel(scope), r.Key, fact, got, wantComp)
			}
		}
	}

	// (2) The composed read, target scope, every person and fact.
	for _, p := range ref.persons {
		pp, err := PersonByID(st, p)
		if err != nil || pp == nil {
			t.Fatalf("%s: PersonByID(%s): %v %v", label, p, pp, err)
		}
		for _, fact := range ref.facts {
			contest := ref.targetContest(p, fact)
			v, ok := d92RefResolve(t, contest)
			if !ok {
				continue
			}
			w, _ := ref.row(v.Winner)
			want := w.Facts[fact]
			got, present := d92PersonField(pp, fact)
			if fact == "name" && want == nil {
				continue // name is not clearable; a wire clear of it changes nothing
			}
			switch {
			case want == nil && present:
				t.Fatalf("%s: DOMINANCE ORACLE DISAGREEMENT (composed read, target scope) — case [%s]: %s.%s composes %q; the reference winner %v CLEARED it (want absent)\n  contest: %s",
					label, d92RefLabel(contest), p, fact, got, v.Winner, d92DescribeSeen(contest))
			case want != nil && (!present || got != *want):
				t.Fatalf("%s: DOMINANCE ORACLE DISAGREEMENT (composed read, target scope) — case [%s]: %s.%s composes %q (present=%v); the reference winner %v asserted %q (heads %v)\n  contest: %s",
					label, d92RefLabel(contest), p, fact, got, present, v.Winner, *want, v.Heads, d92DescribeSeen(contest))
			}
		}
	}

	// (3) The merge snapshot.
	m, err := LoadPersonMergeMap(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ref.persons {
		if got, want := m.Resolve(p), ref.rep(t, p); got != want {
			t.Fatalf("%s: DOMINANCE ORACLE DISAGREEMENT (merge snapshot): Resolve(%s) = %s, reference representative = %s", label, p, got, want)
		}
	}
}

// d92OracleAmend authors through the REAL edge and checks the stamp
// against the reference's union-of-heads computed BEFORE authoring, off the
// raw log. Blind spot 1 lives here: the group half of the union.
func d92OracleAmend(t *testing.T, st *store.Store, siteID string, epoch, clockMS int64, in PersonAmendment) store.Envelope {
	t.Helper()
	ref := d92RefFromStore(t, st)
	facts := d92AmendmentFacts(in)
	want := ref.stamp(t, in.PersonID, facts)
	env := w3AmendLocal(t, st, siteID, epoch, clockMS, in)
	got := d92RefSortKeys(d92RefSeenOf(env.Payload))
	if !d92RefKeysEqual(got, want) {
		t.Fatalf("DOMINANCE ORACLE DISAGREEMENT (the `seen` stamp): AmendPerson(%s, facts %v) at %s stamped %v; the reference (target-scope heads ∪ group-scope heads over the log as held) says %v",
			in.PersonID, facts, siteID, got, want)
	}
	return env
}

// d92AmendmentFacts maps a PersonAmendment's set fields to wire fact names
// through PersonAmended's tags (same field names), plus its Clear list.
func d92AmendmentFacts(in PersonAmendment) []string {
	wire := reflect.TypeOf(PersonAmended{})
	tagOf := map[string]string{}
	for i := 0; i < wire.NumField(); i++ {
		tagOf[wire.Field(i).Name] = strings.Split(wire.Field(i).Tag.Get("json"), ",")[0]
	}
	var out []string
	v := reflect.ValueOf(in)
	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		if f.Type == reflect.TypeOf((*string)(nil)) && !v.Field(i).IsNil() {
			out = append(out, tagOf[f.Name])
		}
	}
	return append(out, in.Clear...)
}

// d92OracleRemote runs author at a scratch post primed with primer (a real
// remote edge) with the stamp check on, and returns the minted envelopes.
func d92OracleRemote(t *testing.T, primer []store.Envelope, siteID string, epoch, clockMS int64, ins ...PersonAmendment) []store.Envelope {
	t.Helper()
	return w3AuthorRemote(t, primer, siteID, epoch, clockMS, func(rs *store.Store) {
		for _, in := range ins {
			d92OracleAmend(t, rs, siteID, epoch, clockMS, in)
		}
	})
}

// d92OracleAtEveryPost runs the oracle at st and at fresh posts that receive
// the given streams in each listed order.
func d92OracleAtEveryPost(t *testing.T, st *store.Store, roster []store.Envelope, orders [][]store.Envelope, label string) {
	t.Helper()
	d92RunDominanceOracle(t, st, label+" @ authoring post")
	for i, evs := range orders {
		d92RunDominanceOracle(t, d92Fresh(t, roster, evs), fmt.Sprintf("%s @ fresh post, delivery order %d", label, i))
	}
}

// d92RequireGreaterKeyCarriesTheEarlierClock is blind spot 2's guard: read
// both rows back from event_log and require the GREATER key's
// authored_at_wall_ms STRICTLY less, so a tiebreak reading the clock —
// greatest or least — must disagree with the rule on this pair.
func d92RequireGreaterKeyCarriesTheEarlierClock(t *testing.T, st *store.Store, a, b store.Envelope) {
	t.Helper()
	ref := d92RefFromStore(t, st)
	ra, oka := ref.row(d92ToRef(a.Key()))
	rb, okb := ref.row(d92ToRef(b.Key()))
	if !oka || !okb {
		t.Fatalf("fixture defect: %v/%v not both in the log", a.Key(), b.Key())
	}
	greater, lesser := ra, rb
	if d92RefCompare(ra.Key, rb.Key) < 0 {
		greater, lesser = rb, ra
	}
	if greater.Clock >= lesser.Clock {
		t.Fatalf("fixture defect: the greater key %v carries authored_at_wall_ms %d, not strictly earlier than %v's %d — the clock rival would agree with the tiebreak and certify nothing", greater.Key, greater.Clock, lesser.Key, lesser.Clock)
	}
}

// d92SwapTimeBases exchanges the two envelopes' time bases (AuthoredAt,
// OccurredAt, TimeSource); payloads, keys and `seen` untouched.
func d92SwapTimeBases(a, b store.Envelope) (store.Envelope, store.Envelope) {
	a2, b2 := a, b
	a2.AuthoredAt, b2.AuthoredAt = b.AuthoredAt, a.AuthoredAt
	a2.OccurredAt, b2.OccurredAt = b.OccurredAt, a.OccurredAt
	a2.TimeSource, b2.TimeSource = b.TimeSource, a.TimeSource
	return a2, b2
}

// TestD92DominanceOracleOverContestedFixtures — the fixture half: each
// family builds a real record set through real edges and wire envelopes,
// then the oracle contests every surface against the reference at the
// authoring post and at fresh posts under other delivery orders.
func TestD92DominanceOracleOverContestedFixtures(t *testing.T) {
	t.Run("A saw B: the lesser key, on the earlier clock, wins because it saw", func(t *testing.T) {
		st := w3NewStore(t)
		x, _, roster := w3RosterPair(t, st, "Oracle A1", "Oracle A2")
		b := d92OracleRemote(t, roster, "site-b", 1, w3Base+3*w3Hour, PersonAmendment{PersonID: x, Phone: w3Str("555-0901")})
		w3ApplyAll(t, st, b)
		a := d92OracleAmend(t, st, "site-a", 1, w3Base+1*w3Hour, PersonAmendment{PersonID: x, Phone: w3Str("555-0902")})
		if !d92RefKeysEqual(d92RefSortKeys(d92RefSeenOf(a.Payload)), []d92RefKey{d92ToRef(b[0].Key())}) {
			t.Fatalf("fixture: a must name exactly b")
		}
		d92OracleAtEveryPost(t, st, roster, [][]store.Envelope{{b[0], a}, {a, b[0]}}, "A-saw-B")
	})

	t.Run("B saw A: the greater key wins because it saw, not because it is greater", func(t *testing.T) {
		st := w3NewStore(t)
		x, _, roster := w3RosterPair(t, st, "Oracle B1", "Oracle B2")
		a := d92OracleAmend(t, st, "site-a", 1, w3Base+3*w3Hour, PersonAmendment{PersonID: x, Phone: w3Str("555-0911")})
		held := w3Events(t, st, "site-a", 1)
		b := d92OracleRemote(t, held, "site-b", 1, w3Base+1*w3Hour, PersonAmendment{PersonID: x, Phone: w3Str("555-0912")})
		w3ApplyAll(t, st, b)
		if !d92RefKeysEqual(d92RefSortKeys(d92RefSeenOf(b[0].Payload)), []d92RefKey{d92ToRef(a.Key())}) {
			t.Fatalf("fixture: b must name exactly a")
		}
		d92OracleAtEveryPost(t, st, roster, [][]store.Envelope{{a, b[0]}, {b[0], a}}, "B-saw-A")
	})

	t.Run("genuinely concurrent: greater key on the EARLIER clock; then the bases swapped", func(t *testing.T) {
		st := w3NewStore(t)
		x, _, roster := w3RosterPair(t, st, "Oracle C1", "Oracle C2")
		p0 := d92OracleAmend(t, st, "site-a", 1, w3Base, PersonAmendment{PersonID: x, Phone: w3Str("555-0920")})
		held := w3Events(t, st, "site-a", 1)
		a1 := d92OracleAmend(t, st, "site-a", 1, w3Base+3*w3Hour, PersonAmendment{PersonID: x, Phone: w3Str("555-0921")})
		b1 := d92OracleRemote(t, held, "site-b", 1, w3Base+1*w3Hour, PersonAmendment{PersonID: x, Phone: w3Str("555-0922")})
		w3ApplyAll(t, st, b1)
		for _, e := range []store.Envelope{a1, b1[0]} {
			if !d92RefKeysEqual(d92RefSortKeys(d92RefSeenOf(e.Payload)), []d92RefKey{d92ToRef(p0.Key())}) {
				t.Fatalf("fixture: %v must name exactly p0 (concurrent with the other)", e.Key())
			}
		}
		d92RequireGreaterKeyCarriesTheEarlierClock(t, st, a1, b1[0])
		d92OracleAtEveryPost(t, st, roster, [][]store.Envelope{{p0, a1, b1[0]}, {p0, b1[0], a1}}, "concurrent, clock inverted")
		trueBytes := w3ComposedBytes(t, st)

		// The same record set under the OTHER clock assignment.
		a1s, b1s := d92SwapTimeBases(a1, b1[0])
		for i, order := range [][]store.Envelope{{p0, a1s, b1s}, {p0, b1s, a1s}} {
			post := d92Fresh(t, roster, order)
			ref := d92RefFromStore(t, post)
			ga, _ := ref.row(d92ToRef(a1s.Key()))
			gb, _ := ref.row(d92ToRef(b1s.Key()))
			if !(gb.Clock > ga.Clock) {
				t.Fatalf("fixture: after the swap the greater key %v must carry the LATER clock (%d vs %d)", gb.Key, gb.Clock, ga.Clock)
			}
			d92RunDominanceOracle(t, post, fmt.Sprintf("concurrent, bases SWAPPED, order %d", i))
			if got := w3ComposedBytes(t, post); got != trueBytes {
				t.Fatalf("concurrent, bases SWAPPED, order %d: composed state differs from the true-basis state — the same record set must compose identically under either clock assignment (THE CLOCK ORDERS NOTHING)\n swapped: %s\n true:    %s", i, got, trueBytes)
			}
		}
	})

	t.Run("mutual reference (rule 3), wire-formed, both delivery orders", func(t *testing.T) {
		origin := w3NewStore(t)
		x, _, roster := w3RosterPair(t, origin, "Oracle M1", "Oracle M2")
		refB := map[string]any{"site_id": "site-b", "node_epoch": 1, "seq": 1}
		refC := map[string]any{"site_id": "site-c", "node_epoch": 1, "seq": 1}
		eb := w3WireAmendment(t, "site-b", 1, 1, w3Base+2*w3Hour, x, map[string]any{"phone": "555-0931", "seen": []any{refC}})
		ec := w3WireAmendment(t, "site-c", 1, 1, w3Base+1*w3Hour, x, map[string]any{"phone": "555-0932", "seen": []any{refB}})
		for i, order := range [][]store.Envelope{{eb, ec}, {ec, eb}} {
			d92RunDominanceOracle(t, d92Fresh(t, roster, order), fmt.Sprintf("mutual (rule 3), order %d", i))
		}
	})

	t.Run("chain A<-B<-C where A holds the greatest key and latest clock", func(t *testing.T) {
		st := w3NewStore(t)
		x, _, roster := w3RosterPair(t, st, "Oracle K1", "Oracle K2")
		a := d92OracleRemote(t, roster, "site-c", 1, w3Base+5*w3Hour, PersonAmendment{PersonID: x, Phone: w3Str("555-0941")})
		b := d92OracleRemote(t, append(append([]store.Envelope{}, roster...), a...), "site-b", 1, w3Base+2*w3Hour, PersonAmendment{PersonID: x, Phone: w3Str("555-0942")})
		w3ApplyAll(t, st, a)
		w3ApplyAll(t, st, b)
		c := d92OracleAmend(t, st, "site-a", 1, w3Base+1*w3Hour, PersonAmendment{PersonID: x, Phone: w3Str("555-0943")})
		// c names only b (a was already dominated when c was authored).
		if !d92RefKeysEqual(d92RefSortKeys(d92RefSeenOf(c.Payload)), []d92RefKey{d92ToRef(b[0].Key())}) {
			t.Fatalf("fixture: c names %v, want exactly [b] — a is not a head", d92RefSeenOf(c.Payload))
		}
		d92OracleAtEveryPost(t, st, roster, [][]store.Envelope{{a[0], b[0], c}, {c, b[0], a[0]}, {b[0], c, a[0]}}, "chain A<-B<-C")
	})

	t.Run("merge-group scope: the stamp names the group's head; Outcome judges the group, composition the target", func(t *testing.T) {
		st := w3NewStore(t)
		x, y, roster := w3RosterPair(t, st, "Oracle G-X", "Oracle G-Y")
		remote := d92OracleRemote(t, roster, "site-b", 1, w3Base+3*w3Hour,
			PersonAmendment{PersonID: x, SameAs: w3Str(y)},
			PersonAmendment{PersonID: x, Phone: w3Str("555-0951")})
		if len(remote) != 2 {
			t.Fatalf("fixture: site-b minted %d events, want 2", len(remote))
		}
		m1, bx := remote[0], remote[1]
		w3ApplyAll(t, st, remote)
		// No row targets Y's phone: the target-scope heads are EMPTY and only
		// the group scope can name bx. The stamp check inside is the instrument.
		ay := d92OracleAmend(t, st, "site-a", 1, w3Base+1*w3Hour, PersonAmendment{PersonID: y, Phone: w3Str("555-0952")})
		if !d92RefKeysEqual(d92RefSortKeys(d92RefSeenOf(ay.Payload)), []d92RefKey{d92ToRef(bx.Key())}) {
			t.Fatalf("fixture: ay names %v, want exactly [bx]", d92RefSeenOf(ay.Payload))
		}
		// The W5d merged-away corner (M7), person edition — INCLUDED here:
		// X, merged away, then carries the greatest-keyed name assertion.
		cx := d92OracleRemote(t, append(append([]store.Envelope{}, roster...), append(remote, ay)...), "site-c", 1, w3Base+9*w3Hour,
			PersonAmendment{PersonID: x, Name: w3Str("Oracle G-X Renamed After Merge")})
		w3ApplyAll(t, st, cx)
		d92OracleAtEveryPost(t, st, roster, [][]store.Envelope{{ay, m1, bx, cx[0]}, {m1, bx, ay, cx[0]}, {cx[0], m1, bx, ay}}, "merge-group scope")
		// Say what the two scopes gave, so the reader sees the two answers
		// this oracle held apart rather than excluded.
		if p := w3Person(t, st, y); p.Name != "Oracle G-Y" {
			t.Fatalf("merge-group scope: the representative's composed name is %q, want its OWN (target scope; the W5d M7 pin)", p.Name)
		}
		if got := w3Fact(t, st, cx[0].Key(), w3FactName); got.Status != Effective {
			t.Fatalf("merge-group scope: Outcome(merged-away X's name) = %v, want Effective in the GROUP scope", got.Status)
		}
	})

	t.Run("un-merge made in knowledge of the merge dissolves the group", func(t *testing.T) {
		st := w3NewStore(t)
		x, y, roster := w3RosterPair(t, st, "Oracle U-X", "Oracle U-Y")
		remote := d92OracleRemote(t, roster, "site-b", 1, w3Base+3*w3Hour,
			PersonAmendment{PersonID: x, SameAs: w3Str(y)},
			PersonAmendment{PersonID: x, Phone: w3Str("555-0961")})
		w3ApplyAll(t, st, remote)
		ay := d92OracleAmend(t, st, "site-a", 1, w3Base+1*w3Hour, PersonAmendment{PersonID: y, Phone: w3Str("555-0962")})
		un := d92OracleAmend(t, st, "site-a", 1, w3Base+2*w3Hour, PersonAmendment{PersonID: x, Clear: []string{"same_as"}})
		if !d92RefKeysEqual(d92RefSortKeys(d92RefSeenOf(un.Payload)), []d92RefKey{d92ToRef(remote[0].Key())}) {
			t.Fatalf("fixture: the un-merge names %v, want exactly the merge", d92RefSeenOf(un.Payload))
		}
		// After the un-merge X and Y are separate again: X's phone contest is
		// bx alone, Y's is ay alone — at every post.
		// Same-stream envelopes keep their seq order in every delivery (ay
		// then un on site-a; the merge then bx on site-b); the streams
		// interleave both ways.
		d92OracleAtEveryPost(t, st, roster, [][]store.Envelope{{remote[0], remote[1], ay, un}, {ay, un, remote[0], remote[1]}}, "un-merge in knowledge")
		if m := w3MergeMap(t, st); m.Resolve(x) != x {
			t.Fatalf("un-merge: Resolve(X) = %s, want X", m.Resolve(x))
		}
	})

	t.Run("cross-epoch: concurrent pair falls to epoch-major; a causal reference beats epoch order", func(t *testing.T) {
		st := w3NewStore(t)
		x, _, roster := w3RosterPair(t, st, "Oracle E1", "Oracle E2")
		e1a := d92OracleAmend(t, st, "site-a", 1, w3Base+1*w3Hour, PersonAmendment{PersonID: x, Phone: w3Str("555-0971")})
		e1b := d92OracleAmend(t, st, "site-a", 1, w3Base+3*w3Hour, PersonAmendment{PersonID: x, Phone: w3Str("555-0972")})
		e2 := w3WireAmendment(t, "site-a", 2, 1, w3Base+2*w3Hour, x, map[string]any{"phone": "555-0973"})
		w3Apply(t, st, e2)
		d92RequireGreaterKeyCarriesTheEarlierClock(t, st, e1b, e2)
		d92OracleAtEveryPost(t, st, roster, [][]store.Envelope{{e1a, e1b, e2}, {e2, e1a, e1b}}, "cross-epoch concurrent")

		// The inversion: a later row on the OLD epoch names e2 — the lesser
		// epoch wins by dominance over the epoch-major key.
		e1c := w3WireAmendment(t, "site-a", 1, e1b.Seq+1, w3Base+4*w3Hour, x, map[string]any{
			"phone": "555-0974", "seen": []any{map[string]any{"site_id": "site-a", "node_epoch": 2, "seq": 1}}})
		w3Apply(t, st, e1c)
		d92OracleAtEveryPost(t, st, roster, [][]store.Envelope{{e1a, e1b, e1c, e2}, {e2, e1a, e1b, e1c}}, "cross-epoch causal inversion")
		if p := w3Person(t, st, x); p.Phone == nil || *p.Phone != "555-0974" {
			t.Fatalf("cross-epoch causal inversion: composed phone = %v, want the old-epoch row that NAMED the re-imaged post's row", p.Phone)
		}
	})

	t.Run("malformed and partial seen: garbage, empty, partial entry, mixed list, self-reference, foreign reference", func(t *testing.T) {
		st := w3NewStore(t)
		x, _, roster := w3RosterPair(t, st, "Oracle P1", "Oracle P2")
		p0 := d92OracleAmend(t, st, "site-a", 1, w3Base, PersonAmendment{PersonID: x, Phone: w3Str("555-0980")})
		ref0 := map[string]any{"site_id": "site-a", "node_epoch": 1, "seq": p0.Seq}
		wire := []store.Envelope{
			w3WireAmendment(t, "site-b", 1, 1, w3Base+1*w3Hour, x, map[string]any{"phone": "555-0981", "seen": "garbage"}),
			w3WireAmendment(t, "site-c", 1, 1, w3Base+2*w3Hour, x, map[string]any{"phone": "555-0982", "seen": []any{}}),
			w3WireAmendment(t, "site-d", 1, 1, w3Base+3*w3Hour, x, map[string]any{"phone": "555-0983", "seen": []any{map[string]any{"site_id": "site-a"}}}),
			// mixed: one valid reference beside one wrongly-typed entry — the LIST is the unit
			w3WireAmendment(t, "site-e", 1, 1, w3Base+4*w3Hour, x, map[string]any{"phone": "555-0984", "seen": []any{ref0, map[string]any{"site_id": "site-a", "node_epoch": 1, "seq": "one"}}}),
			// self-reference only
			w3WireAmendment(t, "site-f", 1, 1, w3Base+5*w3Hour, x, map[string]any{"phone": "555-0985", "seen": []any{map[string]any{"site_id": "site-f", "node_epoch": 1, "seq": 1}}}),
			// foreign reference: a key this post has never held
			w3WireAmendment(t, "site-g", 1, 1, w3Base+6*w3Hour, x, map[string]any{"phone": "555-0986", "seen": []any{map[string]any{"site_id": "site-zz", "node_epoch": 7, "seq": 7}}}),
			// a valid reference to p0 beside a valid reference to site-g's row — the one row here that dominates anything
			w3WireAmendment(t, "site-h", 1, 1, w3Base+7*w3Hour, x, map[string]any{"phone": "555-0987", "seen": []any{ref0, map[string]any{"site_id": "site-g", "node_epoch": 1, "seq": 1}}}),
		}
		w3ApplyAll(t, st, wire)
		reversed := make([]store.Envelope, 0, len(wire)+1)
		reversed = append(reversed, p0)
		for i := len(wire) - 1; i >= 0; i-- {
			reversed = append(reversed, wire[i])
		}
		d92OracleAtEveryPost(t, st, roster, [][]store.Envelope{append([]store.Envelope{p0}, wire...), reversed}, "malformed/partial seen")
	})

	t.Run("multi-fact: a reference dominates in one fact's contest and not in another's", func(t *testing.T) {
		st := w3NewStore(t)
		x, _, roster := w3RosterPair(t, st, "Oracle F1", "Oracle F2")
		k := d92OracleRemote(t, roster, "site-b", 1, w3Base+2*w3Hour, PersonAmendment{PersonID: x, Phone: w3Str("555-0991"), Role: w3Str("role-k")})
		w3ApplyAll(t, st, k)
		// r asserts phone only and (through the edge) names k — k's ROLE still stands.
		r := d92OracleAmend(t, st, "site-a", 1, w3Base+1*w3Hour, PersonAmendment{PersonID: x, Phone: w3Str("555-0992")})
		if !d92RefKeysEqual(d92RefSortKeys(d92RefSeenOf(r.Payload)), []d92RefKey{d92ToRef(k[0].Key())}) {
			t.Fatalf("fixture: r names %v, want exactly [k]", d92RefSeenOf(r.Payload))
		}
		// s clears the role at a lesser key WITHOUT seeing k's role (a wire row
		// naming nothing): concurrent on role, so k's greater key keeps the role.
		s := w3WireAmendment(t, "site-a", 2, 1, w3Base+3*w3Hour, x, map[string]any{"clear": []any{"role"}})
		w3Apply(t, st, s)
		d92OracleAtEveryPost(t, st, roster, [][]store.Envelope{{k[0], r, s}, {s, r, k[0]}}, "multi-fact")
		if p := w3Person(t, st, x); p.Role == nil || *p.Role != "role-k" || p.Phone == nil || *p.Phone != "555-0992" {
			t.Fatalf("multi-fact: composed (phone, role) = (%v, %v), want (555-0992, role-k)", p.Phone, p.Role)
		}
	})
}

// TestD92DominanceOracleOverTheStandingFixtures runs the sibling over the
// three fixture families W3-11's oracle is mandated to cover (W3-1's
// cross-epoch pair, W3-4's contested merge, W3-10's merge supersession), so
// the two oracles are seen to stand on the same ground.
func TestD92DominanceOracleOverTheStandingFixtures(t *testing.T) {
	t.Run("cross-epoch (W3-1)", func(t *testing.T) {
		fx := w3BuildCrossEpochFixture(t)
		d92RunDominanceOracle(t, fx.st, "W3-1 cross-epoch")
	})
	t.Run("contested merge (W3-4)", func(t *testing.T) {
		fx := w3BuildContestedMerge(t)
		d92RunDominanceOracle(t, fx.st, "W3-4 contested merge")
	})
	t.Run("merge supersession (W3-10)", func(t *testing.T) {
		fx := w3BuildMergeSupersession(t)
		d92RunDominanceOracle(t, fx.st, "W3-10 merge supersession")
	})
}
