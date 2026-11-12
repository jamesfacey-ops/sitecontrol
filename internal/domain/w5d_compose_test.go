// w5d_compose_test.go — W5d-3 (the zero-composition analogue, W3-8's own
// instance against company), W5d-16 (= W3-1: ascending-key determinism plus
// the standing same-site cross-epoch CONTROL fixture), W5d-18 (= W3-3:
// Clear un-sets to ABSENT, on `name` per DR-2(b)), and W5d-10's
// CROSS-SITE-AND-CROSS-EPOCH fixture (H1's residual, closed at the lock).
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, W5d LOCKED 2026-08-31.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD — that IS these tests' clean-tree red.
//
// KILLER MUTANTS: company-m14 (zero composition — Companies()/CompanyByID()
// ignore amendments; "the twice-produced CRITICAL class"), W3-m1 (compose
// sorted by OccurredAt/AuthoredAt), W3-m5 (epoch-blind comparator,
// (site_id, seq)-only or seq-major — provably invariant under the scramble
// and still wrong on the pinned cross-epoch winner), W3-m7 (Clear composes
// as empty-string rather than absent), W3-m17 (comparator/ordering drift —
// the REORDERED spelling `ORDER BY node_epoch, site_id, seq`, which is
// behaviourally indistinguishable from canonical unless a fixture is BOTH
// cross-site and cross-epoch).
package domain

import (
	"encoding/json"
	"testing"

	"sitecontrol/internal/store"
)

// --- W5d-3: the zero-composition analogue -------------------------------

// TestW5dComposedCompanyReadPass — W5d-3 (W3-8's own instance): both
// Companies() and CompanyByID() return the COMPOSED company. Goes red under
// company-m14: company_amended ships, appends, folds — and the readers
// return the created-state company untouched. At 88dd6a6 both functions
// "compose nothing" (refdata.go:351, :396 return raw projection rows), which
// is precisely the pre-W3 shape of Persons()/PersonByID() [MEASURED].
func TestW5dComposedCompanyReadPass(t *testing.T) {
	st := w3NewStore(t)
	c, err := CreateCompany(st, "site-a", 1, "Created Name Ltd", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	other, err := CreateCompany(st, "site-a", 1, "Merge Target Ltd", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w5dAmendLocal(t, st, "site-a", 1, w3Base+1*w3Hour, CompanyAmendment{
		CompanyID: c.CompanyID,
		Name:      w3Str("Composed Name Ltd"),
	})
	w5dAmendLocal(t, st, "site-a", 1, w3Base+2*w3Hour, CompanyAmendment{
		CompanyID: c.CompanyID,
		SameAs:    w3Str(other.CompanyID),
	})

	one := w5dCompany(t, st, c.CompanyID)
	name, ok := w5dCompanyName(one)
	if !ok || name != "Composed Name Ltd" {
		t.Fatalf("W5d-3 (company-m14, zero composition): CompanyByID returned name=%q (present=%v), want the composed %q — the created-state row untouched is this chunk's cheapest deadly mutant", name, ok, "Composed Name Ltd")
	}
	if one.SameAs == nil || *one.SameAs != other.CompanyID {
		t.Fatalf("W5d-3 (company-m14): CompanyByID returned same_as=%s, want the composed %q", w5dSameAsOf(one), other.CompanyID)
	}

	found := false
	for _, q := range w5dCompanies(t, st) {
		if q.CompanyID != c.CompanyID {
			continue
		}
		found = true
		n, ok := w5dCompanyName(&q)
		if !ok || n != "Composed Name Ltd" {
			t.Fatalf("W5d-3 (company-m14): Companies() returned name=%q (present=%v) for the amended company, want the composed value", n, ok)
		}
		if n == "Created Name Ltd" {
			t.Fatalf("W5d-3 (company-m14): Companies() still shows the created-state name")
		}
		if q.SameAs == nil || *q.SameAs != other.CompanyID {
			t.Fatalf("W5d-3 (company-m14): Companies() returned same_as=%s, want %q", w5dSameAsOf(&q), other.CompanyID)
		}
	}
	if !found {
		t.Fatalf("W5d-3: amended company missing from Companies()")
	}
}

// --- W5d-16 (= W3-1): the same-site cross-epoch fixture -----------------

// w5dCrossEpochFixture is W5d-16's same-site cross-epoch conflict, shared
// with W5d-10's T-A oracle run (the §5 standing cross-epoch CONTROL row).
type w5dCrossEpochFixture struct {
	st        *store.Store
	companyID string
	e1a, e1b  store.Envelope // epoch 1, ascending seq; e1b carries the HIGH seq and the LATEST clock
	e2        store.Envelope // epoch 2, seq 1 (LOW), earlier clock than e1b
	nameA1    string
	nameA2    string // the epoch-blind AND chronological rivals' winner
	nameB     string // the PINNED winner under `ORDER BY site_id, node_epoch, seq`
}

func w5dBuildCrossEpochFixture(t *testing.T) w5dCrossEpochFixture {
	t.Helper()
	fx := w5dCrossEpochFixture{
		nameA1: "Epoch One Early Ltd",
		nameA2: "Epoch One Late Ltd",
		nameB:  "Epoch Two Winner Ltd",
	}
	fx.st = w3NewStore(t)
	c, err := CreateCompany(fx.st, "site-a", 1, "Cross Epoch Ltd", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	fx.companyID = c.CompanyID
	// Epoch 1: two amendments, raising seq; the LAST carries the LATEST
	// clock so a chronological compose picks it.
	fx.e1a = w5dAmendLocal(t, fx.st, "site-a", 1, w3Base+1*w3Hour,
		CompanyAmendment{CompanyID: fx.companyID, Name: w3Str(fx.nameA1)})
	fx.e1b = w5dAmendLocal(t, fx.st, "site-a", 1, w3Base+3*w3Hour,
		CompanyAmendment{CompanyID: fx.companyID, Name: w3Str(fx.nameA2)})
	// Epoch 2 (the re-image), seq 1 — LOW seq, clock EARLIER than e1b's.
	fx.e2 = w5dAmendLocal(t, fx.st, "site-a", 2, w3Base+2*w3Hour,
		CompanyAmendment{CompanyID: fx.companyID, Name: w3Str(fx.nameB)})

	// --- runtime non-degeneracy (standing rule 3): every named rival
	// recomputed from the READ-BACK log, never from the constants above.
	amendments := w5dNameAssertions(t, fx.st, fx.companyID,
		[][2]interface{}{{"site-a", int64(1)}, {"site-a", int64(2)}})
	if len(amendments) != 3 {
		t.Fatalf("W5d-16 fixture defect: %d name amendments in log, want 3", len(amendments))
	}
	rivals := map[string]string{
		"chronological max(AuthoredAt) [W3-m1]": w5dPick(amendments, func(a, b store.Envelope) bool {
			return a.AuthoredAt.WallMS < b.AuthoredAt.WallMS
		}),
		"chronological max(OccurredAt) [W3-m1]": w5dPick(amendments, func(a, b store.Envelope) bool {
			return a.OccurredAt < b.OccurredAt
		}),
		"seq-major (epoch-blind) [W3-m5]": w5dPick(amendments, func(a, b store.Envelope) bool {
			return a.Seq < b.Seq
		}),
		"ORDER BY site_id, seq (epoch-blind) [W3-m5]": w5dPick(amendments, func(a, b store.Envelope) bool {
			if a.SiteID != b.SiteID {
				return a.SiteID < b.SiteID
			}
			return a.Seq < b.Seq
		}),
	}
	if fx.e2.NodeEpoch <= fx.e1b.NodeEpoch || fx.e2.Seq >= fx.e1b.Seq {
		t.Fatalf("W5d-16 fixture defect: the cross-epoch pair is not (epoch N+1, LOW seq) vs (epoch N, HIGH seq): e2=%v e1b=%v", fx.e2.Key(), fx.e1b.Key())
	}
	if fx.e1b.SiteID != fx.e2.SiteID {
		t.Fatalf("W5d-16 fixture defect: the pair must be SAME-SITE (that is what makes node_epoch, not site_id, the deciding column): %q vs %q", fx.e1b.SiteID, fx.e2.SiteID)
	}
	w5dRequireDistinct(t, "W5d-16 cross-epoch", fx.nameB, rivals)
	return fx
}

// w5dNameAssertion is one name-asserting company amendment read back out of
// the log, with the value it claimed.
type w5dNameAssertion struct {
	env  store.Envelope
	name string
}

// w5dNameAssertions parses every company_amended event in the given streams
// that asserts `name` for companyID, in stream/seq read order.
func w5dNameAssertions(t *testing.T, st *store.Store, companyID string, streams [][2]interface{}) []w5dNameAssertion {
	t.Helper()
	var out []w5dNameAssertion
	for _, s := range streams {
		for _, e := range w3Events(t, st, s[0].(string), s[1].(int64)) {
			var pl struct {
				Type      string  `json:"type"`
				CompanyID string  `json:"company_id"`
				Name      *string `json:"name"`
			}
			if err := json.Unmarshal(e.Payload, &pl); err != nil {
				continue
			}
			if pl.Type == w5dEvtCompanyAmended && pl.CompanyID == companyID && pl.Name != nil {
				out = append(out, w5dNameAssertion{env: e, name: *pl.Name})
			}
		}
	}
	return out
}

// w5dPick returns the name asserted by the event a given "less" relation
// ranks last — the generic rival evaluator.
func w5dPick(as []w5dNameAssertion, less func(a, b store.Envelope) bool) string {
	best := as[0]
	for _, c := range as[1:] {
		if less(best.env, c.env) {
			best = c
		}
	}
	return best.name
}

// TestW5dCrossEpochComposePinsTheCertifiedWinner — W5d-16 and the §5
// standing cross-epoch CONTROL row against company: epoch N+1 / LOW seq
// beats epoch N / HIGH seq under the certified `ORDER BY site_id,
// node_epoch, seq`, the winner PINNED as a literal, epoch-blind mutant
// W3-m5 named. Three consecutive designs shipped cross-site-only conflict
// fixtures on which an epoch-blind comparator agreed with the real one on
// every test; this fixture is the standing remedy, and its non-degeneracy
// is asserted at runtime rather than by comment.
func TestW5dCrossEpochComposePinsTheCertifiedWinner(t *testing.T) {
	fx := w5dBuildCrossEpochFixture(t)
	got, ok := w5dCompanyName(w5dCompany(t, fx.st, fx.companyID))
	if !ok {
		t.Fatalf("W5d-16: composed name absent, want the pinned winner %q", fx.nameB)
	}
	if got != fx.nameB {
		t.Fatalf("W5d-16 (W3-m1/W3-m5): composed name = %q, want the PINNED winner %q (epoch N+1, LOW seq, under `ORDER BY site_id, node_epoch, seq`) — %q is the epoch-blind and chronological rivals' answer", got, fx.nameB, fx.nameA2)
	}
}

// TestW5dComposeScrambleInvariance — W5d-16's first clause: permuting
// AuthoredAt/OccurredAt VALUES across the amendments (over a FIXED stream —
// arrival order is untouched; the arrival-order mutant W3-m10 is W5d-7's)
// changes nothing about the composed output, byte-compared.
func TestW5dComposeScrambleInvariance(t *testing.T) {
	fx := w5dBuildCrossEpochFixture(t)
	epoch1 := w3Events(t, fx.st, "site-a", 1)
	epoch2 := w3Events(t, fx.st, "site-a", 2)

	amendKeys := map[store.Key]bool{fx.e1a.Key(): true, fx.e1b.Key(): true, fx.e2.Key(): true}
	type basis struct {
		auth store.AuthoredAt
		occ  int64
		src  string
	}
	var bases []basis
	collect := func(evs []store.Envelope) {
		for _, e := range evs {
			if amendKeys[e.Key()] {
				bases = append(bases, basis{e.AuthoredAt, e.OccurredAt, e.TimeSource})
			}
		}
	}
	collect(epoch1)
	collect(epoch2)
	if len(bases) != 3 {
		t.Fatalf("W5d-16 fixture defect: %d permutable bases, want 3", len(bases))
	}

	composed := func(perm []int) []byte {
		st := w3NewStore(t)
		i := 0
		applyPermuted := func(evs []store.Envelope) {
			for _, e := range evs {
				if amendKeys[e.Key()] {
					b := bases[perm[i]]
					e.AuthoredAt, e.OccurredAt, e.TimeSource = b.auth, b.occ, b.src
					i++
				}
				w3Apply(t, st, e)
			}
		}
		applyPermuted(epoch1)
		applyPermuted(epoch2)
		one := w5dCompany(t, st, fx.companyID)
		all := w5dCompanies(t, st)
		out, err := json.Marshal(struct {
			One *Company
			All []Company
		}{one, all})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	want := composed([]int{0, 1, 2})
	for _, perm := range [][]int{{1, 2, 0}, {2, 0, 1}, {2, 1, 0}, {0, 2, 1}, {1, 0, 2}} {
		if got := composed(perm); string(got) != string(want) {
			t.Fatalf("W5d-16 (W3-m1): composed output changed under time-basis permutation %v —\nwant %s\ngot  %s\ncomposition read a clock; it must be ascending-key only", perm, want, got)
		}
	}
}

// --- W5d-10's CROSS-SITE AND CROSS-EPOCH fixture ------------------------

// w5dCrossSiteEpochFixture is the fixture H1's residual forced into
// existence: TWO SITES **and** TWO EPOCHS, constructed so that the
// certified spelling `ORDER BY site_id, node_epoch, seq` and the REORDERED
// spelling `ORDER BY node_epoch, site_id, seq` name DIFFERENT winners.
//
// ⚠ The two comparators are named by their SPELLINGS here, never by the
// word "epoch-major" (R2-W13): the certified rule puts site_id FIRST; the
// rival puts node_epoch first. Naming the certified rule by the property
// that distinguishes its rival is how a test-writer builds this fixture
// backwards, pins the wrong winner, and still passes the distinctness
// check.
//
// ⚠ This fixture is NOT guaranteed to split the certified rule from the
// EPOCH-BLIND `(site_id, seq)` rival — that is what the same-site
// cross-epoch pair above exists to do, and it stays beside this one.
type w5dCrossSiteEpochFixture struct {
	st         *store.Store
	companyID  string
	siteA2     store.Envelope // site-a, epoch 2 — the REORDERED spelling's winner
	siteB1     store.Envelope // site-b, epoch 1 — the CERTIFIED spelling's winner
	nameSiteA2 string
	nameSiteB1 string // PINNED: the certified `ORDER BY site_id, node_epoch, seq` answer
}

func w5dBuildCrossSiteEpochFixture(t *testing.T) w5dCrossSiteEpochFixture {
	t.Helper()
	fx := w5dCrossSiteEpochFixture{
		nameSiteA2: "Site A Epoch Two Ltd",
		nameSiteB1: "Site B Epoch One Ltd",
	}
	origin := w3NewStore(t)
	c, err := CreateCompany(origin, "site-a", 1, "Cross Site Epoch Ltd", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	fx.companyID = c.CompanyID
	roster := w3Events(t, origin, "site-a", 1)

	// site-a at epoch 2 (the HIGHER epoch, the LOWER site id).
	fx.siteA2 = w3AuthorRemote(t, roster, "site-a", 2, w3Base+4*w3Hour, func(rs *store.Store) {
		if _, err := AmendCompany(rs, "site-a", 2, CompanyAmendment{CompanyID: fx.companyID, Name: w3Str(fx.nameSiteA2)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})[0]
	// site-b at epoch 1 (the LOWER epoch, the HIGHER site id).
	fx.siteB1 = w3AuthorRemote(t, roster, "site-b", 1, w3Base+1*w3Hour, func(rs *store.Store) {
		if _, err := AmendCompany(rs, "site-b", 1, CompanyAmendment{CompanyID: fx.companyID, Name: w3Str(fx.nameSiteB1)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})[0]

	fx.st = w3NewStore(t)
	w3ApplyAll(t, fx.st, roster)
	w3Apply(t, fx.st, fx.siteA2)
	w3Apply(t, fx.st, fx.siteB1)

	// --- runtime recomputation of BOTH comparators from the read-back
	// keys; a TIE is a fatal fixture defect, not a comment. --------------
	as := w5dNameAssertions(t, fx.st, fx.companyID, [][2]interface{}{
		{"site-a", int64(1)}, {"site-a", int64(2)}, {"site-b", int64(1)},
	})
	if len(as) != 2 {
		t.Fatalf("W5d-10 fixture defect: %d name assertions read back, want 2", len(as))
	}
	certified := w5dPick(as, func(a, b store.Envelope) bool { // ORDER BY site_id, node_epoch, seq
		if a.SiteID != b.SiteID {
			return a.SiteID < b.SiteID
		}
		if a.NodeEpoch != b.NodeEpoch {
			return a.NodeEpoch < b.NodeEpoch
		}
		return a.Seq < b.Seq
	})
	reordered := w5dPick(as, func(a, b store.Envelope) bool { // ORDER BY node_epoch, site_id, seq
		if a.NodeEpoch != b.NodeEpoch {
			return a.NodeEpoch < b.NodeEpoch
		}
		if a.SiteID != b.SiteID {
			return a.SiteID < b.SiteID
		}
		return a.Seq < b.Seq
	})
	if certified != fx.nameSiteB1 {
		t.Fatalf("W5d-10 fixture defect: the certified spelling `ORDER BY site_id, node_epoch, seq` names %q, want the pinned %q — the fixture no longer encodes the criterion (R2-W13: build it by the SPELLING, never by the word \"epoch-major\")", certified, fx.nameSiteB1)
	}
	if reordered != fx.nameSiteA2 {
		t.Fatalf("W5d-10 fixture defect: the reordered spelling `ORDER BY node_epoch, site_id, seq` names %q, want %q", reordered, fx.nameSiteA2)
	}
	w5dRequireDistinct(t, "W5d-10 cross-site AND cross-epoch", certified, map[string]string{
		"ORDER BY node_epoch, site_id, seq [W3-m17]": reordered,
	})
	// The fixture must genuinely be BOTH (rule 6).
	if fx.siteA2.SiteID == fx.siteB1.SiteID {
		t.Fatalf("W5d-10 fixture defect: the two assertions share a site (%q) — the fixture must be CROSS-SITE", fx.siteA2.SiteID)
	}
	if fx.siteA2.NodeEpoch == fx.siteB1.NodeEpoch {
		t.Fatalf("W5d-10 fixture defect: the two assertions share a node_epoch (%d) — the fixture must be CROSS-EPOCH, or the reordered spelling is indistinguishable from canonical", fx.siteA2.NodeEpoch)
	}
	return fx
}

// TestW5dCrossSiteCrossEpochComposePinsTheCertifiedSpelling — W5d-10's
// H1-residual fixture: the composed name is PINNED to the winner the
// certified `ORDER BY site_id, node_epoch, seq` names, and the REORDERED
// `ORDER BY node_epoch, site_id, seq` provably names the other one on this
// same data. Without a fixture that is cross-site AND cross-epoch, W3-m17's
// drift half has no behavioural killer at all — site_id breaks every tie
// either way whenever the events share an epoch.
func TestW5dCrossSiteCrossEpochComposePinsTheCertifiedSpelling(t *testing.T) {
	fx := w5dBuildCrossSiteEpochFixture(t)
	got, ok := w5dCompanyName(w5dCompany(t, fx.st, fx.companyID))
	if !ok || got != fx.nameSiteB1 {
		t.Fatalf("W5d-10 (W3-m17): composed name = %q (present=%v), want the PINNED %q — the certified spelling is `ORDER BY site_id, node_epoch, seq` (site_id FIRST); %q is what `ORDER BY node_epoch, site_id, seq` answers on this fixture", got, ok, fx.nameSiteB1, fx.nameSiteA2)
	}
}

// --- W5d-18 (= W3-3): Clear un-sets to ABSENT ---------------------------

// TestW5dClearComposesAsAbsent — W5d-18, discharged on `name` per DR-2(b)
// (same_as-as-Clear is W5d-7's un-merge round trip, so the ride would
// otherwise collapse into it). A wire-received Clear:["name"] — the fold
// never refuses, so this is the only way the case exists — composes as
// ABSENT, never as a value. The reader-visible half, where the
// empty-vs-absent distinction is actually observable, is asserted at the
// muster's third preference arm (W5d-14(b-i) / W5d-22(d)).
func TestW5dClearComposesAsAbsent(t *testing.T) {
	st := w3NewStore(t)
	c, err := CreateCompany(st, "site-a", 1, "Named Ltd", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w5dAmendLocal(t, st, "site-a", 1, w3Base+1*w3Hour,
		CompanyAmendment{CompanyID: c.CompanyID, Name: w3Str("Renamed Ltd")})
	// Fixture precondition (rule 6): the name must compose BEFORE the Clear,
	// or the assertion below is vacuous.
	if got, ok := w5dCompanyName(w5dCompany(t, st, c.CompanyID)); !ok || got != "Renamed Ltd" {
		t.Fatalf("W5d-18 fixture: name did not compose before the Clear (got %q, present=%v)", got, ok)
	}

	// The edge REFUSES Clear:["name"] (W5d-22(b) arm 4), so the case can
	// only arrive by wire — and the fold never refuses (W5d-22(c)).
	clearEnv := w5dWireAmendment(t, "site-b", 1, 1, w3Base+2*w3Hour, c.CompanyID,
		map[string]any{"clear": []string{w5dFactName}})
	w3Apply(t, st, clearEnv)

	got, present := w5dCompanyName(w5dCompany(t, st, c.CompanyID))
	if present {
		if got == "Renamed Ltd" {
			t.Fatalf("W5d-18 (W3-m7): the cleared name still composes as %q — Clear un-sets; a Clear that composes as the prior value did not un-set at all", got)
		}
		t.Fatalf("W5d-18 (W3-m7): the cleared name composes as the present value %q, want ABSENT — Clear un-sets, it does not blank and it does not keep", got)
	}
}
