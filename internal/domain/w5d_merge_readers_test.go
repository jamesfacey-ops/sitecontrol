// w5d_merge_readers_test.go — W5d-5 (W3-4 cloned verbatim against company:
// the one-composed-path reader check, the contested-merge fixture and the
// legacy-ref_alias divergence fixture), W5d-5b (LoadCompanyMergeMap exists,
// is derived per read and caches nothing) and W5d-12 (W3-17's wire-formed
// cycle, REBUILT for company — this project's degenerate-fixture instance
// #1, so the discrimination table is recomputed at RUNTIME over all SIX
// rivals and carries its own positive control).
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, W5d LOCKED 2026-08-31.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD — that IS the clean-tree red.
//
// KILLER MUTANTS: W3-m9 (second-path reader — a reader resolving via a
// privately-loaded stale snapshot; the legacy-ref_alias divergence fixture
// and W5d-12's AR2-2 carve-out both produce it), W3-m26 (min(canonical_id)
// tiebreak resurrected), W3-m23 (the termination guarantee's delivery
// removed), W3-m27 (wrong/nondeterministic representative), (m6) (a cached
// or package-level company merge map), (m7) (a merge map built from
// composed Company.SameAs ALONE — it resolves single edges correctly and
// diverges only on cycles, so only W5d-12's fixture sees it).
package domain

import (
	"encoding/json"
	"fmt"
	"sort"
	"testing"

	"sitecontrol/internal/store"
)

// --- the shared fixture-defect judgement (M3's control path) ------------

// w5dDistinctProblems is the ONE judgement function behind every
// non-degeneracy check in W5d: it reports each rival that agrees with the
// certified answer. The real fixtures AND the positive controls drive
// THIS function, so a control red proves the same code path the clean pass
// runs (the w4CoverageProblems idiom).
func w5dDistinctProblems(certified string, rivals map[string]string) []string {
	var problems []string
	for name, got := range rivals {
		if got == certified {
			problems = append(problems, fmt.Sprintf("DEGENERATE — rival %q returns %q, the same answer as the certified rule; a fixture where the rule and a rival agree certifies nothing (standing rule 3; the W3-17 rev-2 lesson)", name, got))
		}
	}
	sort.Strings(problems)
	return problems
}

// w5dRequireDistinct fails as a FIXTURE DEFECT (never as a criterion red)
// if any named rival agrees with the certified answer.
func w5dRequireDistinct(t *testing.T, criterion, certified string, rivals map[string]string) {
	t.Helper()
	if problems := w5dDistinctProblems(certified, rivals); len(problems) != 0 {
		t.Fatalf("%s fixture defect: %v", criterion, problems)
	}
}

// --- W5d-5: the contested-merge fixture ---------------------------------

// w5dContestedMerge: site-a authors X→Y, site-b authors X→Z, unseen (both
// minted on primed scratch edges, delivered by wire), with values chosen so
// the retired min(canonical_id) rule [W3-m26], arrival order [W3-m10] and
// clock order [W3-m1] each name Y while RECORD order names Z — the composed
// answer is PINNED to the record-order winner, site-b's Z.
type w5dContestedMerge struct {
	st      *store.Store
	x, y, z string
	w       string         // the legacy-ref_alias divergence target (W3-m9's fixture)
	nameZ   string         // Z's own pinned composed name (M7's corner)
	toY     store.Envelope // site-a's merge — record-order LOSER; the three rivals' winner
	toZ     store.Envelope // site-b's merge — record-order WINNER, pinned
}

func w5dBuildContestedMerge(t *testing.T) w5dContestedMerge {
	t.Helper()
	const nameZ = "Contested Z Ltd"
	origin := w3NewStore(t)
	x, c1, _ := w5dCompanyPair(t, origin, "Contested X Ltd", "Contested P1 Ltd")
	c2, err := CreateCompany(origin, "site-a", 1, nameZ, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w, err := CreateCompany(origin, "site-a", 1, "Contested W Ltd", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	fx := w5dContestedMerge{x: x, w: w.CompanyID}
	// Role assignment AFTER minting: y is the LEXICALLY SMALLER id, so the
	// retired min(canonical_id) rule names y — and y is the record-order
	// LOSER by construction (site-a authors x→y).
	fx.y, fx.z = c1, c2.CompanyID
	if fx.z < fx.y {
		fx.y, fx.z = fx.z, fx.y
	}
	// The pinned name belongs to whichever id ended up as z.
	fx.nameZ = nameZ
	if fx.z == c1 {
		fx.nameZ = "Contested P1 Ltd"
	}
	roster := w3Events(t, origin, "site-a", 1)

	// Both merges authored on real edges at partitioned scratch posts.
	// site-a's (→y) carries the LATER clock; site-b's (→z) the earlier.
	aMerge := w3AuthorRemote(t, roster, "site-a", 1, w3Base+2*w3Hour, func(rs *store.Store) {
		if _, err := AmendCompany(rs, "site-a", 1, CompanyAmendment{CompanyID: fx.x, SameAs: w3Str(fx.y)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	bMerge := w3AuthorRemote(t, roster, "site-b", 1, w3Base+1*w3Hour, func(rs *store.Store) {
		if _, err := AmendCompany(rs, "site-b", 1, CompanyAmendment{CompanyID: fx.x, SameAs: w3Str(fx.z)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	if len(aMerge) != 1 || len(bMerge) != 1 {
		t.Fatalf("fixture defect: the two posts minted %d and %d events, want 1 each", len(aMerge), len(bMerge))
	}
	fx.toY, fx.toZ = aMerge[0], bMerge[0]

	// One post store; ARRIVAL order: site-b's merge FIRST, site-a's second
	// (so an arrival-latest compose names y, not the pinned z).
	fx.st = w3NewStore(t)
	w3ApplyAll(t, fx.st, roster)
	w3Apply(t, fx.st, fx.toZ)
	w3Apply(t, fx.st, fx.toY)

	// Runtime non-degeneracy (standing rule 3): all three rivals recomputed
	// from the fixture's OWN read-back data, never asserted by comment.
	minRule := fx.y
	if fx.z < minRule {
		minRule = fx.z
	}
	arrivalLatest := fx.y // applied second, by construction just above
	clockLatest := fx.y
	if fx.toZ.AuthoredAt.WallMS > fx.toY.AuthoredAt.WallMS {
		clockLatest = fx.z
	}
	w5dRequireDistinct(t, "W5d-5 contested merge", fx.z, map[string]string{
		"retired min(canonical_id) [W3-m26]": minRule,
		"arrival-order latest [W3-m10]":      arrivalLatest,
		"clock-order latest [W3-m1]":         clockLatest,
	})
	return fx
}

// TestW5dContestedMergeOneComposedPath — W5d-5 (W3-4 cloned against
// company): every reader whose composed path exists at W5d resolves through
// the ONE composed path, identically. The obligation is a PREDICATE — every
// non-test caller of Companies()/CompanyByID(), plus MusterLive's companies
// slice consumer, plus W5d's own merge-map loader — whose current VALUE at
// 88dd6a6 is six call sites / seven enumerated entries [MEASURED]. The
// domain half is asserted here; the web callers (picker, intake ×2, gate,
// muster) assert the same fixture in internal/web's
// w5d_reader_surfaces_test.go, and the muster's own consumer in
// w5d_muster_test.go. Readers are pinned by FILE + CALL EXPRESSION, never
// by file:line (C-10 — W5b inserts a refusal into intake.go above :431).
func TestW5dContestedMergeOneComposedPath(t *testing.T) {
	fx := w5dBuildContestedMerge(t)

	if c := w5dCompany(t, fx.st, fx.x); c.SameAs == nil || *c.SameAs != fx.z {
		t.Fatalf("W5d-5 (W3-m26): composed same_as = %s, want the PINNED record-order winner %q — %q is the retired min(canonical_id) rule's answer, and its resurrection is exactly what this fixture exists to refuse", w5dSameAsOf(c), fx.z, fx.y)
	}
	if got := w5dMergeMap(t, fx.st).Resolve(fx.x); got != fx.z {
		t.Fatalf("W5d-5 (W3-m9/W3-m26): derived snapshot Resolve(%s) = %s, want %s — a second or stale resolution path disagrees with the composed state", fx.x, got, fx.z)
	}
	// Companies() — the list reader every web surface goes through.
	seen := false
	for _, c := range w5dCompanies(t, fx.st) {
		if c.CompanyID == fx.x {
			seen = true
			if c.SameAs == nil || *c.SameAs != fx.z {
				t.Fatalf("W5d-5: Companies() shows same_as %s for X, want %q — every listed reader must answer identically", w5dSameAsOf(&c), fx.z)
			}
		}
	}
	if !seen {
		t.Fatalf("W5d-5 fixture defect: Companies() does not contain X (%s) at all", fx.x)
	}
}

// ⟦was: "TestW5dLegacyRefAliasRowDoesNotSteerCompanyResolution"⟧ — W5d-5(b),
// W3-m9's company-side producer, and the ONLY red that distinguishes a
// re-pointed muster.go:122 from an unmoved one that happened to agree. At
// W5d the legacy ref_alias mechanism is still the LIVE company mechanism
// (P-3 §5 defers its deletion to W8), so a divergent legacy row is
// constructible NOW: it maps X somewhere the composed same_as does not.
// Company resolution must answer from the composed path ONLY.
// ⚠⚠ ⟦RETIRED — §60, 2026-09-03⟧ KIND B (sites 13–14) — (3) ATTEMPTED, THEN (1) RETIRE.
// Superseded lines preserved verbatim below. Retired on its OWN reasoning, not with its
// person-side twin: what it uniquely carried is named below.
//
// WHAT IT PROVED: W5d-5(b), W3-m9's COMPANY-side producer — and, in its own words, "the
// ONLY red that distinguishes a re-pointed muster.go:122 from an unmoved one that
// happened to agree". It also carried a fixture-defect guard asserting the two maps
// ACTUALLY diverge ("the fixture is not posing its case").
// WHICH RULING REMOVED ITS SUBJECT: P-3 §5. ⚠ Note the schedule ran out from under it:
// its own header says the legacy mechanism "is still the LIVE company mechanism" and
// that a divergent row "is constructible NOW" — NOW was W5d, and W8 ended it.
//
// ⭐ DISPOSITION (3) WAS ATTEMPTED FIRST, AS §60 REQUIRES, AND IT FAILS — the attempt is
// recorded here rather than skipped, because "no rival exists" is a claim that must be
// checkable. A surviving rival would have to be a merge map, OTHER than the composed one,
// that can hold a divergent edge for the SAME id. ⚠ NONE CAN EXIST: `LoadAliasMap` was
// ONE map over ALL entity kinds; its four successors are KIND-PARTITIONED, and the
// partition is enforced at every authoring door — AmendPerson refuses an id that "is not
// on the roster" (person_amend.go:570-572), AmendCompany refuses one that "is not a
// company on this post" (company_amend.go:572-573), AmendTrade likewise (trade_amend.go:
// 584-585). So an id of this kind can be keyed in ITS OWN map and nowhere else, and the
// second path this test exists to catch is no longer constructible — the rule it enforces
// is now true by construction rather than by assertion. §60: prove no rival can exist,
// then (1).
//
// ⭐ COVERAGE COST, and it is the sharper of the two: this was the ONLY red separating a
// re-pointed company line from an unmoved one that agreed by luck. ⚠ That specific
// discrimination is NOT lost — it survives with `trade` in the deleted mechanism's place,
// and is re-expressed under §60 (3) in w5d_muster_test.go and w5d_reader_surfaces_test.go,
// where the composed COMPANY map must diverge from the composed TRADE map. This site is
// retired; the discrimination it guarded is re-armed there.

// func ⟦was: "TestW5dLegacyRefAliasRowDoesNotSteerCompanyResolution"⟧(t *testing.T) {
// 	fx := w5dBuildContestedMerge(t)
// 	if _, err := CreateAlias(fx.st, "site-a", 1, fx.x, fx.w, 0, "default"); err != nil {
// 		t.Fatalf("fixture: legacy ref_alias authoring failed: %v", err)
// 	}
// 	// Fixture-defect guard (rule 6): the second path must actually differ.
// 	if fx.w == fx.z {
// 		t.Fatalf("fixture defect: the legacy target equals the composed winner — the second path cannot differ (standing rule 3)")
// 	}
// 	legacy, err := LoadAliasMap(fx.st)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	if got := legacy.Resolve(fx.x); got != fx.w {
// 		t.Fatalf("fixture defect: the legacy ref_alias map resolves X to %s, want the divergent %s — the fixture is not posing its case", got, fx.w)
// 	}
//
// 	if got := w5dMergeMap(t, fx.st).Resolve(fx.x); got != fx.z {
// 		t.Fatalf("W5d-5 (W3-m9): with a divergent legacy ref_alias row (X→%s) present, COMPANY resolution answered %s, want the composed winner %s — company aliasing via ref_alias is superseded at W5d and must steer NOTHING", fx.w, got, fx.z)
// 	}
// 	if c := w5dCompany(t, fx.st, fx.x); c.SameAs == nil || *c.SameAs != fx.z {
// 		t.Fatalf("W5d-5 (W3-m9): composed same_as changed under a legacy ref_alias row: %s", w5dSameAsOf(c))
// 	}
// }

// TestW5dMergedAwayCompanyDoesNotDriveTheRepresentativesName — the
// inherited residual "builder call 5's corner", RULED: PIN (M7). A company
// that has been merged away carries the LATEST-KEYED name assertion; the
// surviving representative's composed name must still be ITS OWN, pinned as
// a literal. If the design later wants the joined set to drive the composed
// value, that is a change to a pinned literal and will be visible as a
// failing test — which is the point of pinning it.
//
// ⚠ This construction is deliberately NOT fed to the T-A oracle (W5d-10):
// it is precisely the case where the per-fact Outcome (which contests
// across the merge-joined set) and the composed read (which folds only rows
// targeting the record) are ruled to disagree. Feeding it to the iff would
// manufacture a false red against a pinned design decision.
func TestW5dMergedAwayCompanyDoesNotDriveTheRepresentativesName(t *testing.T) {
	const mergedAwayName = "X Renamed After Being Merged Away"
	fx := w5dBuildContestedMerge(t)

	// X is merged away into Z; X then carries the LATEST-KEYED name.
	later := w5dAmendLocal(t, fx.st, "site-z", 1, w3Base+9*w3Hour,
		CompanyAmendment{CompanyID: fx.x, Name: w3Str(mergedAwayName)})
	// Precondition (rule 6): the new assertion must really be latest in
	// record order over the joined set, or the corner is not posed.
	if !fx.toZ.Key().Less(later.Key()) {
		t.Fatalf("fixture defect: the merged-away company's name assertion %v is not later in record order than the surviving merge %v — the corner is not posed", later.Key(), fx.toZ.Key())
	}
	if got := w5dMergeMap(t, fx.st).Resolve(fx.x); got != fx.z {
		t.Fatalf("fixture defect: X no longer resolves to Z (%s) — the merged-away premise is gone", got)
	}

	repName, ok := w5dCompanyName(w5dCompany(t, fx.st, fx.z))
	if !ok || repName != fx.nameZ {
		t.Fatalf("W5d-5 (M7's pinned corner): the surviving representative Z's composed name = %q (present=%v), want the PINNED literal %q — a merged-away record's later-keyed name assertion must NOT drive the representative's composed name", repName, ok, fx.nameZ)
	}
	ownName, ok := w5dCompanyName(w5dCompany(t, fx.st, fx.x))
	if !ok || ownName != mergedAwayName {
		t.Fatalf("W5d-5 (M7): the merged-away company X's OWN composed name = %q (present=%v), want %q — its assertion is kept on its own record, never erased", ownName, ok, mergedAwayName)
	}
}

// --- W5d-5b: the loader itself ------------------------------------------

// TestW5dCompanyMergeMapIsDerivedPerReadAndCachesNothing — W5d-5b: the
// thing muster.go:122 re-points TO must exist, be derived per read, drop
// self-loops the same way, and cache NOTHING. Kills (m6) — a cached or
// package-level company merge map (W3-m9's shape, and the cheapest way to
// make "the single line that changes" literally true).
func TestW5dCompanyMergeMapIsDerivedPerReadAndCachesNothing(t *testing.T) {
	st := w3NewStore(t)
	x, y, _ := w5dCompanyPair(t, st, "Loader X Ltd", "Loader Y Ltd")

	// Before any merge: identity.
	if got := w5dMergeMap(t, st).Resolve(x); got != x {
		t.Fatalf("W5d-5b: Resolve(%s) on an unmerged store = %s, want identity", x, got)
	}
	// A merge lands; a map loaded AFTER it must see it — a snapshot taken
	// once at package init or cached across calls cannot.
	w5dAmendLocal(t, st, "site-a", 1, w3Base+1*w3Hour,
		CompanyAmendment{CompanyID: x, SameAs: w3Str(y)})
	if got := w5dMergeMap(t, st).Resolve(x); got != y {
		t.Fatalf("W5d-5b (m6): after the merge, a freshly loaded map resolves %s to %s, want %s — the loader is caching, or is a package-level snapshot", x, got, y)
	}
	// The un-merge lands; a map loaded after it must see THAT too.
	w5dAmendLocal(t, st, "site-a", 1, w3Base+2*w3Hour,
		CompanyAmendment{CompanyID: x, Clear: []string{w5dFactSameAs}})
	if got := w5dMergeMap(t, st).Resolve(x); got != x {
		t.Fatalf("W5d-5b (m6): after the un-merge, a freshly loaded map resolves %s to %s, want the identity %s — the loader retained state across reads", x, got, x)
	}

	// Self-loops are DROPPED (LoadPersonMergeMap's own rule): a wire-formed
	// self-merge resolves nothing rather than looping.
	selfLoop := w5dWireAmendment(t, "site-b", 1, 1, w3Base+3*w3Hour, y, map[string]any{"same_as": y})
	w3Apply(t, st, selfLoop)
	m := w5dMergeMap(t, st)
	if got := w3BudgetedResolve(t, m, y); got != y {
		t.Fatalf("W5d-5b: with a wire-formed SELF-loop on %s, Resolve = %s, want the identity — a self-edge resolves nothing and must be dropped from the map", y, got)
	}
	if _, present := m[y]; present {
		t.Fatalf("W5d-5b: the self-loop edge %s→%s survives in the map — LoadPersonMergeMap drops it and the clone must too", y, y)
	}
}

// --- W5d-12: the wire-formed cycle, REBUILT for company -----------------

// w5dCycleEdge is one parsed company same_as assertion from the log.
type w5dCycleEdge struct {
	env    store.Envelope
	source string
	target string
	order  int // arrival index at this post
}

// w5dCycleEdges parses the same_as assertions among members from the post's
// full log. Arrival order is stamped by the CALLER — it is precisely what
// the log key does not encode.
func w5dCycleEdges(t *testing.T, st *store.Store, members map[string]bool) []w5dCycleEdge {
	t.Helper()
	var out []w5dCycleEdge
	for _, stream := range [][2]interface{}{{"site-a", int64(1)}, {"site-b", int64(1)}} {
		for _, e := range w3Events(t, st, stream[0].(string), stream[1].(int64)) {
			var pl struct {
				Type      string  `json:"type"`
				CompanyID string  `json:"company_id"`
				SameAs    *string `json:"same_as"`
			}
			if err := json.Unmarshal(e.Payload, &pl); err != nil {
				continue
			}
			if pl.Type == w5dEvtCompanyAmended && members[pl.CompanyID] && pl.SameAs != nil && members[*pl.SameAs] {
				out = append(out, w5dCycleEdge{env: e, source: pl.CompanyID, target: *pl.SameAs})
			}
		}
	}
	return out
}

// w5dCycleRivals recomputes W3-17's FULL SIX-rival discrimination table
// from read-back data. The landed person-side table
// (w3_merge_readers_test.go:250–257) computes exactly these six; revision 5
// of the W5d criteria named only four, and the lock restored the last two.
func w5dCycleRivals(edges []w5dCycleEdge, a, b string) (certified string, rivals map[string]string, recordLatest, recordEarliest, arrivalLatest, clockLatest w5dCycleEdge) {
	recordLatest, recordEarliest = edges[0], edges[1]
	if w5dKeyBefore(recordLatest.env, recordEarliest.env) {
		recordLatest, recordEarliest = recordEarliest, recordLatest
	}
	arrivalLatest = edges[0]
	if edges[1].order > edges[0].order {
		arrivalLatest = edges[1]
	}
	clockLatest = edges[0]
	if edges[1].env.AuthoredAt.WallMS > edges[0].env.AuthoredAt.WallMS {
		clockLatest = edges[1]
	}
	minRule := a
	if b < minRule {
		minRule = b
	}
	certified = recordLatest.target
	rivals = map[string]string{
		"min-of-cycle-ids [W3-m27]":            minRule,
		"identity/no-collapse, Resolve(a)":     a,
		"arrival-order latest target [W3-m27]": arrivalLatest.target,
		"clock-order latest target [W3-m27]":   clockLatest.target,
		"source-of-latest [W3-m27]":            recordLatest.source,
		"target-of-earliest [W3-m27]":          recordEarliest.target,
	}
	return
}

// w5dKeyBefore is a test-local lexicographic (site, epoch, seq) compare —
// deliberately NOT store.Key.Less, so this fixture's record-order
// derivation cannot be circular with the comparator under test (W5d-10
// certifies the comparator separately).
func w5dKeyBefore(a, b store.Envelope) bool {
	if a.SiteID != b.SiteID {
		return a.SiteID < b.SiteID
	}
	if a.NodeEpoch != b.NodeEpoch {
		return a.NodeEpoch < b.NodeEpoch
	}
	return a.Seq < b.Seq
}

// TestW5dWireFormedCycleCollapsesToPinnedRepresentative — W5d-12
// (W3-17 cloned WITH W3-m27, the fixture REBUILT for company; standing
// rule 3 says the person-side fixture is not reusable and a reused one
// proves nothing — the runtime table below is the mechanism that enforces
// that sentence).
//
// The cross-site 2-cycle: members {a, b} with a < b; E1 a.same_as→b
// authored AT SITE-B carrying the EARLIER AuthoredAt and arriving FIRST at
// both posts, yet LATEST in record order ("site-b" > "site-a" decides);
// E2 b.same_as→a authored AT SITE-A with the LATER clock, arriving second.
// Resolve(a) and Resolve(b) both return the PINNED literal b at both posts,
// across 8 repeated snapshot loads, each under the harness's own step
// budget (W4-9's fixed mechanical form — m23's red is budget exhaustion
// attributed to THIS named test, never a suite-timeout abort).
func TestW5dWireFormedCycleCollapsesToPinnedRepresentative(t *testing.T) {
	// --- M3's POSITIVE CONTROL, run FIRST: a deliberately DEGENERATE cycle
	// fixture on which a rival agrees must be reported as a fixture defect
	// by the same judgement function the real fixture uses. Without this,
	// "the fixture is non-degenerate" is a claim nothing can fail. --------
	w5dCycleSelfCheckControl(t)

	origin := w3NewStore(t)
	c1, c2, roster := w5dCompanyPair(t, origin, "Cycle Member One Ltd", "Cycle Member Two Ltd")
	a, b := c1, c2
	if b < a {
		a, b = b, a
	}

	// E1 — a→b, authored at site-b on a real primed edge, EARLIER clock.
	e1 := w3AuthorRemote(t, roster, "site-b", 1, w3Base+1*w3Hour, func(rs *store.Store) {
		if _, err := AmendCompany(rs, "site-b", 1, CompanyAmendment{CompanyID: a, SameAs: w3Str(b)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})[0]
	// E2 — b→a, authored at site-a on a real primed edge that has NEVER
	// seen E1. Edges refuse cycles locally (W5d-22(b) arm 10) and folds
	// never refuse — the cycle forms only on the wire. LATER clock.
	e2 := w3AuthorRemote(t, roster, "site-a", 1, w3Base+2*w3Hour, func(rs *store.Store) {
		if _, err := AmendCompany(rs, "site-a", 1, CompanyAmendment{CompanyID: b, SameAs: w3Str(a)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})[0]

	// Both posts, same arrival order: E1 earlier, E2 later.
	posts := map[string]*store.Store{}
	for _, name := range []string{"post-a", "post-b"} {
		st := w3NewStore(t)
		w3ApplyAll(t, st, roster)
		w3Apply(t, st, e1)
		w3Apply(t, st, e2)
		posts[name] = st
	}

	// --- the runtime discrimination table, recomputed from ONE post's
	// READ-BACK log, never from the constants (standing rule 3) ----------
	edges := w5dCycleEdges(t, posts["post-a"], map[string]bool{a: true, b: true})
	if len(edges) != 2 { // fixture defect 1
		t.Fatalf("W5d-12 fixture defect: %d cycle edges read back, want 2", len(edges))
	}
	for i := range edges { // arrival order stamped by key match
		switch edges[i].env.Key() {
		case e1.Key():
			edges[i].order = 0
		case e2.Key():
			edges[i].order = 1
		default:
			t.Fatalf("W5d-12 fixture defect: unexpected cycle edge %v", edges[i].env.Key())
		}
	}
	pinned, rivals, recordLatest, recordEarliest, arrivalLatest, _ := w5dCycleRivals(edges, a, b)
	if pinned != b { // fixture defect 2
		t.Fatalf("W5d-12 fixture defect: the record-order-latest cycle edge targets %q, want the pinned %q — the fixture no longer encodes the criterion", pinned, b)
	}
	if recordLatest.env.AuthoredAt.WallMS >= recordEarliest.env.AuthoredAt.WallMS { // fixture defect 3
		t.Fatalf("W5d-12 fixture defect: the record-order-latest edge must carry the EARLIER AuthoredAt (clock contradicts key), got %d >= %d", recordLatest.env.AuthoredAt.WallMS, recordEarliest.env.AuthoredAt.WallMS)
	}
	if arrivalLatest.env.Key() == recordLatest.env.Key() { // fixture defect 4
		t.Fatalf("W5d-12 fixture defect: the record-order-latest edge also arrived last — arrival order cannot disagree (standing rule 3)")
	}
	if recordLatest.env.SiteID == recordEarliest.env.SiteID { // fixture defect 5
		t.Fatalf("W5d-12 fixture defect: both cycle edges were authored at %q — the criterion's fixture is CROSS-SITE (\"site-b\" > \"site-a\" is what decides record order here)", recordLatest.env.SiteID)
	}
	w5dRequireDistinct(t, "W5d-12 cycle representative", pinned, rivals) // fixture defect 6

	// --- the pinned assertion, both posts, 8 repeated loads (W3-m27's
	// map-iteration pick reds deterministically in practice), each under
	// the harness's own step budget (W3-m23) --------------------------
	for name, st := range posts {
		for i := 0; i < 8; i++ {
			m := w5dMergeMap(t, st)
			if got := w3BudgetedResolve(t, m, a); got != b {
				t.Fatalf("W5d-12 (W3-m27) at %s, load %d: Resolve(%s) = %q, want the PINNED representative %q — the identity shape returns a, and every named rival returns a non-pinned id on this fixture (%v). (m7) — a map built from composed Company.SameAs alone — reds HERE and only here: it resolves single edges correctly and loses cycle collapse", name, i, a, got, b, rivals)
			}
			if got := w3BudgetedResolve(t, m, b); got != b {
				t.Fatalf("W5d-12 (W3-m27) at %s, load %d: Resolve(%s) = %q, want %q", name, i, b, got, b)
			}
		}
	}

	// --- the AR2-2 carve-out, cloned WITH the criterion (M14: dropping it
	// at the FIRST clone is how a carve-out dies): the representative is
	// stable only UP TO the latest cycle-edge key — an idempotent,
	// wire-shaped re-assertion of the b→a edge with a later key flips it,
	// partition unchanged, both posts identical. A CACHED label (W3-m9's
	// shape, and (m6)'s) fails to flip. -------------------------------
	e3 := w5dWireAmendment(t, "site-b", 1, e1.Seq+1, w3Base+3*w3Hour, b, map[string]any{"same_as": a})
	for name, st := range posts {
		w3Apply(t, st, e3)
		m := w5dMergeMap(t, st)
		if got := w3BudgetedResolve(t, m, a); got != a {
			t.Fatalf("W5d-12 carve-out (AR2-2 / W3-m9's caching shape) at %s: after a later-keyed re-assertion of the b→a edge, Resolve(a) = %q, want the flipped representative %q — the label must be recomputed from composed state, never cached", name, got, a)
		}
		if got := w3BudgetedResolve(t, m, b); got != a {
			t.Fatalf("W5d-12 carve-out at %s: Resolve(b) = %q, want %q", name, got, a)
		}
	}
}

// w5dCycleSelfCheckControl is M3's positive control for W5d-12's runtime
// self-check: a deliberately DEGENERATE cycle fixture — both edges authored
// at the SAME site so arrival order and record order agree — must be
// reported as a fixture defect by w5dDistinctProblems, the same judgement
// function the real fixture drives. Run ONCE, before the real fixture's
// pass counts.
func w5dCycleSelfCheckControl(t *testing.T) {
	t.Helper()
	a, b := "company-aaa", "company-bbb"
	mk := func(site string, seq, clock int64, src, dst string) w5dCycleEdge {
		env := w5dWireAmendment(t, site, 1, seq, clock, src, map[string]any{"same_as": dst})
		return w5dCycleEdge{env: env, source: src, target: dst}
	}
	// Same site, ascending seq, ascending clock, arrival = record order:
	// arrival-order-latest and clock-order-latest BOTH agree with the
	// record-order winner. That is the degeneracy the real fixture must
	// not have.
	degenerate := []w5dCycleEdge{
		mk("site-a", 1, w3Base+1*w3Hour, a, b),
		mk("site-a", 2, w3Base+2*w3Hour, b, a),
	}
	degenerate[0].order, degenerate[1].order = 0, 1
	pinned, rivals, _, _, _, _ := w5dCycleRivals(degenerate, a, b)
	problems := w5dDistinctProblems(pinned, rivals)
	if len(problems) == 0 {
		t.Fatalf("W5d-12 SELF-CHECK CONTROL FAILED: a deliberately degenerate cycle fixture (both edges at site-a, arrival order == record order) produced NO fixture-defect report — the runtime self-check cannot detect a degenerate fixture, so its clean verdict on the real fixture proves nothing (M3)")
	}
	agreed := 0
	for name, got := range rivals {
		if got == pinned {
			agreed++
			_ = name
		}
	}
	if agreed < 2 {
		t.Fatalf("W5d-12 SELF-CHECK CONTROL: the degenerate fixture makes only %d rival(s) agree with the certified answer, want >= 2 (M3 asks for a fixture on which TWO rivals agree). Rivals: %v, certified %q", agreed, rivals, pinned)
	}
}
