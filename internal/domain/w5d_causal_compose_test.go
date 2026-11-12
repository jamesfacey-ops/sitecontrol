// w5d_causal_compose_test.go — the company carrier under [[D-92]] at
// SchemaVersion 11 (step 3 of the 10 → 11 increment: the causal `seen`
// reference cloned onto company FIRST, in the order the merge field was
// cloned). The person witnesses in w3_causal_compose_test.go are the
// template; the fixtures here are the company twins — composition is
// CAUSAL, then ascending (site_id, node_epoch, seq), through the ONE rule
// (resolveContest), and site_id breaks ties ONLY between authors who did
// not see each other.
//
// KILLER MUTANTS, each PRODUCED red once against this tree before commit
// (reports/witness/STEP3-CLONE.md):
//   - m-D92a: resolveContest ignores `seen` (shared with every carrier).
//   - m-D92b-company: AmendCompany drops the stamp.
//   - m-D92c-company: composeCompany falls back to "last key wins".
//   - m-D92d-company: companyMergeMapFromRows falls back to "last key wins".
package domain

import (
	"encoding/json"
	"testing"

	"sitecontrol/internal/store"
)

// w5dSeenRowOf reads the `seen` list back off the company_amendment row —
// the fold/read round trip.
func w5dSeenRowOf(t *testing.T, st *store.Store, k store.Key, companyID string) []store.Key {
	t.Helper()
	rows, err := readCompanyAmendmentsFor(st, companyID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Key == k {
			return r.Seen
		}
	}
	t.Fatalf("no company_amendment row at %v for %s", k, companyID)
	return nil
}

// w5dNameRivals recomputes the named rival rules over the name-carrying
// company envelopes as READ BACK, never from fixture constants.
func w5dNameRivals(t *testing.T, evs []store.Envelope) map[string]string {
	t.Helper()
	type named struct {
		env  store.Envelope
		name string
	}
	var names []named
	for _, e := range evs {
		var pl CompanyAmended
		if err := json.Unmarshal(e.Payload, &pl); err == nil && pl.Type == w5dEvtCompanyAmended && pl.Name != nil {
			names = append(names, named{e, *pl.Name})
		}
	}
	if len(names) < 2 {
		t.Fatalf("fixture defect: %d name-carrying envelopes, want a contest", len(names))
	}
	byKey, byClock := names[0], names[0]
	for _, n := range names[1:] {
		if byKey.env.Key().Less(n.env.Key()) {
			byKey = n
		}
		if n.env.AuthoredAt.WallMS > byClock.env.AuthoredAt.WallMS {
			byClock = n
		}
	}
	return map[string]string{
		"fixed order — greatest key wins [D-28 Part B, REVERSED by D-92]": byKey.name,
		"chronological max(AuthoredAt) [m1, refused by D-90/D-92]":        byClock.name,
	}
}

// TestW5dCausalCompanyCorrectionMadeInKnowledgeWins — D-92's first sentence
// on the company carrier: site-b (the greater site_id, and the later clock)
// asserts a name over the bare record; site-a receives it, reads it, and
// corrects it. The LESSER-keyed correction wins in the composed read, in
// Outcome (both ways) and at a third post receiving both by wire in either
// order.
func TestW5dCausalCompanyCorrectionMadeInKnowledgeWins(t *testing.T) {
	const nameB, nameA = "Peer Spelling Ltd", "Local Spelling Ltd"
	st := w3NewStore(t)
	x, _, roster := w5dCompanyPair(t, st, "Causal X Ltd", "Causal Y Ltd")

	b1 := d92Remote(t, roster, "site-b", w3Base+3*w3Hour, func(rs *store.Store) {
		if _, err := AmendCompany(rs, "site-b", 1, CompanyAmendment{CompanyID: x, Name: w3Str(nameB)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	w3Apply(t, st, b1)
	a1 := w5dAmendLocal(t, st, "site-a", 1, w3Base+1*w3Hour, CompanyAmendment{CompanyID: x, Name: w3Str(nameA)})
	d92RequireCausalArrangement(t, a1, b1)
	w3RequireDistinct(t, "W5d causal (D-92)", nameA,
		w5dNameRivals(t, append(w3Events(t, st, "site-a", 1), b1)))

	if c := w5dCompany(t, st, x); c.Name != nameA {
		t.Fatalf("D-92 (m-D92a/m-D92c): composed company name = %q, want site-a's %q — the correction made IN KNOWLEDGE of site-b's wins regardless of site_id; %q is D-28 Part B's answer", c.Name, nameA, nameB)
	}
	if ob := w3Fact(t, st, b1.Key(), w5dFactName); ob.Status != Superseded || ob.Winner != a1.Key() {
		t.Fatalf("D-92: site-b's assertion is (%v, winner %v), want (Superseded, %v) — superseded by a key LESS than its own, because that key's author saw it", ob.Status, ob.Winner, a1.Key())
	}
	if oa := w3Fact(t, st, a1.Key(), w5dFactName); oa.Status != Effective || oa.Winner != a1.Key() {
		t.Fatalf("D-92: site-a's correction is (%v, winner %v), want (Effective, itself)", oa.Status, oa.Winner)
	}
	for i, order := range [][]store.Envelope{{a1, b1}, {b1, a1}} {
		third := w3NewStore(t)
		w3ApplyAll(t, third, roster)
		w3ApplyAll(t, third, order)
		if c := w5dCompany(t, third, x); c.Name != nameA {
			t.Fatalf("W3-5 under D-92: a third post receiving order %d composes name %q, want %q — the rule is a pure function of the record set", i, c.Name, nameA)
		}
		if got := w3Fact(t, third, b1.Key(), w5dFactName); got.Status != Superseded || got.Winner != a1.Key() {
			t.Fatalf("W3-5 under D-92: at a third post (order %d) site-b's assertion is (%v, %v), want (Superseded, %v)", i, got.Status, got.Winner, a1.Key())
		}
	}
}

// TestW5dGenuinelyConcurrentCompanyEditsFallToTheSiteIDTiebreak — the
// partition case: both posts hold p0; each corrects it without seeing the
// other's correction. Both `seen` lists name p0 and neither names the
// other; the greater site_id wins at both posts.
func TestW5dGenuinelyConcurrentCompanyEditsFallToTheSiteIDTiebreak(t *testing.T) {
	const name0, nameA, nameB = "Original Ltd", "Local Ltd", "Peer Ltd"
	st := w3NewStore(t)
	x, _, _ := w5dCompanyPair(t, st, "Concurrent X Ltd", "Concurrent Y Ltd")
	p0 := w5dAmendLocal(t, st, "site-a", 1, w3Base, CompanyAmendment{CompanyID: x, Name: w3Str(name0)})
	held := w3Events(t, st, "site-a", 1)

	a1 := w5dAmendLocal(t, st, "site-a", 1, w3Base+3*w3Hour, CompanyAmendment{CompanyID: x, Name: w3Str(nameA)})
	b1 := d92Remote(t, held, "site-b", w3Base+1*w3Hour, func(rs *store.Store) {
		if _, err := AmendCompany(rs, "site-b", 1, CompanyAmendment{CompanyID: x, Name: w3Str(nameB)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	w3Apply(t, st, b1)
	d92RequireConcurrentArrangement(t, a1, b1, []store.Key{p0.Key()})
	w3RequireDistinct(t, "D-92 company tiebreak", nameB, map[string]string{
		"chronological max(AuthoredAt) [m1]":     nameA,
		"lesser key wins (an inverted tiebreak)": nameA,
	})

	if c := w5dCompany(t, st, x); c.Name != nameB {
		t.Fatalf("D-92 tiebreak: composed name = %q, want the greater site_id's %q — neither author saw the other, so site_id decides", c.Name, nameB)
	}
	viewB := w3NewStore(t)
	w3ApplyAll(t, viewB, held)
	w3Apply(t, viewB, b1)
	w3Apply(t, viewB, a1)
	if c := w5dCompany(t, viewB, x); c.Name != nameB {
		t.Fatalf("D-92 tiebreak at site-b's post: composed name = %q, want %q", c.Name, nameB)
	}
	for _, k := range []store.Key{p0.Key(), a1.Key()} {
		if got := w3Fact(t, st, k, w5dFactName); got.Status != Superseded || got.Winner != b1.Key() {
			t.Fatalf("D-92 tiebreak: %v is (%v, %v), want (Superseded, %v)", k, got.Status, got.Winner, b1.Key())
		}
	}
}

// TestW5dCompanySeenIsStampedAtTheEdgeFromEveryHead — after a concurrent
// pair (two heads) a third local amendment names BOTH heads, ascending,
// and not the dominated row beneath them; the side-table row reads the same
// list back; the first assertion carries no `seen` key on the wire; the
// authoring input has no field a caller could supply it through.
func TestW5dCompanySeenIsStampedAtTheEdgeFromEveryHead(t *testing.T) {
	st := w3NewStore(t)
	x, _, _ := w5dCompanyPair(t, st, "Stamp X Ltd", "Stamp Y Ltd")
	p0 := w5dAmendLocal(t, st, "site-a", 1, w3Base, CompanyAmendment{CompanyID: x, Name: w3Str("Stamp 0 Ltd")})
	held := w3Events(t, st, "site-a", 1)
	a1 := w5dAmendLocal(t, st, "site-a", 1, w3Base+1*w3Hour, CompanyAmendment{CompanyID: x, Name: w3Str("Stamp A Ltd")})
	b1 := d92Remote(t, held, "site-b", w3Base+2*w3Hour, func(rs *store.Store) {
		if _, err := AmendCompany(rs, "site-b", 1, CompanyAmendment{CompanyID: x, Name: w3Str("Stamp B Ltd")}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	w3Apply(t, st, b1)
	c := w5dAmendLocal(t, st, "site-a", 1, w3Base+3*w3Hour, CompanyAmendment{CompanyID: x, Name: w3Str("Stamp C Ltd")})

	want := []store.Key{a1.Key(), b1.Key()}
	if !want[0].Less(want[1]) {
		want[0], want[1] = want[1], want[0]
	}
	if seen := d92SeenOf(t, c); !w3KeysEqual(seen, want) {
		t.Fatalf("D-92 (m-D92b-company): the third amendment carries seen=%v, want exactly the two heads %v — every undominated survivor, ascending, and not %v which %v had already superseded", seen, want, p0.Key(), a1.Key())
	}
	if row := w5dSeenRowOf(t, st, c.Key(), x); !w3KeysEqual(row, want) {
		t.Fatalf("D-92: company_amendment.seen reads back %v, want %v — the fold or the read lost the reference", row, want)
	}
	if d92HasSeenKey(t, p0) {
		t.Fatalf("D-92: the FIRST assertion carries a `seen` key — an author who saw nothing standing must omit it")
	}
	d92RequireNoSeenField(t, CompanyAmendment{})
	if got := w5dCompany(t, st, x); got.Name != "Stamp C Ltd" {
		t.Fatalf("D-92: composed name = %q, want %q", got.Name, "Stamp C Ltd")
	}
	for _, k := range []store.Key{a1.Key(), b1.Key(), p0.Key()} {
		if got := w3Fact(t, st, k, w5dFactName); got.Status != Superseded || got.Winner != c.Key() {
			t.Fatalf("D-92: %v is (%v, %v), want (Superseded, %v)", k, got.Status, got.Winner, c.Key())
		}
	}
}

// TestW5dCompanyUnMergeMadeInKnowledgeOfThePeersMergeTakesEffect — the
// merge pointer under D-92 on the company carrier: site-b's merge X→Y
// arrives; site-a's officer, seeing it, un-merges. The un-merge WINS at
// the lesser key: composed same_as absent, the company snapshot resolves X
// to itself, Outcome(un-merge) Effective, Outcome(merge) Superseded by the
// un-merge — at site-a and at a post receiving both by wire in either
// order. ⟦was: at 10 this exact construction was W5d-8's LOSING fixture⟧
func TestW5dCompanyUnMergeMadeInKnowledgeOfThePeersMergeTakesEffect(t *testing.T) {
	st := w3NewStore(t)
	x, y, roster := w5dCompanyPair(t, st, "Unmerge X Ltd", "Unmerge Y Ltd")
	merge := d92Remote(t, roster, "site-b", w3Base+2*w3Hour, func(rs *store.Store) {
		if _, err := AmendCompany(rs, "site-b", 1, CompanyAmendment{CompanyID: x, SameAs: w3Str(y)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	w3Apply(t, st, merge)
	if c := w5dCompany(t, st, x); c.SameAs == nil || *c.SameAs != y {
		t.Fatalf("fixture defect: the peer's merge did not compose before the un-merge (same_as=%v)", c.SameAs)
	}
	unmerge := w5dAmendLocal(t, st, "site-a", 1, w3Base+1*w3Hour, CompanyAmendment{CompanyID: x, Clear: []string{w5dFactSameAs}})
	d92RequireCausalArrangement(t, unmerge, merge)

	for i, post := range []*store.Store{st, func() *store.Store {
		p := w3NewStore(t)
		w3ApplyAll(t, p, roster)
		w3Apply(t, p, unmerge)
		w3Apply(t, p, merge)
		return p
	}()} {
		if c := w5dCompany(t, post, x); c.SameAs != nil {
			t.Fatalf("D-92 (m-D92a/m-D92c) at post %d: composed same_as = %q, want absent — the un-merge made in knowledge of the merge takes effect", i, *c.SameAs)
		}
		if got := w5dMergeMap(t, post).Resolve(x); got != x {
			t.Fatalf("D-92 (m-D92d-company) at post %d: Resolve(%s) = %s, want identity — the company merge snapshot reads the same rule", i, x, got)
		}
		if got := w3Fact(t, post, unmerge.Key(), w5dFactSameAs); got.Status != Effective {
			t.Fatalf("D-92 at post %d: Outcome(un-merge) = %v, want Effective", i, got.Status)
		}
		if got := w3Fact(t, post, merge.Key(), w5dFactSameAs); got.Status != Superseded || got.Winner != unmerge.Key() {
			t.Fatalf("D-92 at post %d: Outcome(peer merge) = (%v, %v), want (Superseded, %v)", i, got.Status, got.Winner, unmerge.Key())
		}
	}
}
