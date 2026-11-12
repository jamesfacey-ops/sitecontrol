// w3_merge_readers_test.go — W3-4's domain-level one-composed-path reader
// check (contested-merge fixture: the retired-rule-is-really-gone proof,
// P-3 §8 seam 1) and W3-17 (the wire-formed cycle: pinned canonical
// representative + termination via W4-9's fixed step-budget form).
// design/WINDOW-BUILD-PLAN-v1.md §3 W3, criteria LOCKED 2026-08-27.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD — that IS the clean-tree red.
//
// KILLER MUTANTS: m9 (second-path reader — a reader resolving via a
// privately-loaded stale snapshot; the legacy-ref_alias divergence fixture
// and W3-17's carve-out both produce it), m26 (min(canonical_id) tiebreak
// resurrected — the contested fixture pins the record-order winner and the
// min rival provably names the OTHER id), m23 (the termination guarantee's
// delivery removed — collapse removed/bypassed AND walk bound removed; the
// harness-side step budget is the test's own, retained), m27 (wrong or
// nondeterministic representative — identity, min-of-cycle-ids,
// arrival-order, clock-order and map-iteration picks each return a
// non-pinned id on the cross-site fixture).
package domain

import (
	"encoding/json"
	"testing"

	"sitecontrol/internal/store"
)

// w3ContestedMergeFixture: site-a authors X→Y, site-b authors X→Z, unseen
// (both minted on primed scratch edges, delivered by wire), with values
// chosen so the retired min(canonical_id) rule, arrival order and clock
// order each name Y while record order names Z — the composed answer is
// PINNED to the record-order winner, site-b's Z.
type w3ContestedMergeFixture struct {
	st       *store.Store
	x, y, z  string
	w        string         // the legacy-ref_alias divergence target (m9's fixture)
	mergeToY store.Envelope // site-a's merge (record-order LOSER; min/arrival/clock rivals' winner)
	mergeToZ store.Envelope // site-b's merge (record-order WINNER, pinned)
}

func w3BuildContestedMerge(t *testing.T) w3ContestedMergeFixture {
	t.Helper()
	origin := w3NewStore(t)
	x, p1, _ := w3RosterPair(t, origin, "Contested X", "Contested P1")
	p2, err := CreatePerson(origin, "site-a", 1, "Contested P2", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w, err := CreatePerson(origin, "site-a", 1, "Contested W", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	fx := w3ContestedMergeFixture{x: x, w: w.PersonID}
	// Role assignment AFTER minting: y is the LEXICALLY SMALLER id, so the
	// retired min(canonical_id) rule names y — and y is the record-order
	// LOSER by construction (site-a authors x→y).
	fx.y, fx.z = p1, p2.PersonID
	if fx.z < fx.y {
		fx.y, fx.z = fx.z, fx.y
	}
	roster := w3Events(t, origin, "site-a", 1)

	// Both merges authored on real edges at partitioned scratch posts.
	// site-a's (→y) carries the LATER clock; site-b's (→z) the earlier.
	aMerge := w3AuthorRemote(t, roster, "site-a", 1, w3Base+2*w3Hour, func(rs *store.Store) {
		if _, err := AmendPerson(rs, "site-a", 1, PersonAmendment{PersonID: fx.x, SameAs: w3Str(fx.y)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	bMerge := w3AuthorRemote(t, roster, "site-b", 1, w3Base+1*w3Hour, func(rs *store.Store) {
		if _, err := AmendPerson(rs, "site-b", 1, PersonAmendment{PersonID: fx.x, SameAs: w3Str(fx.z)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	fx.mergeToY, fx.mergeToZ = aMerge[0], bMerge[0]

	// One post store; ARRIVAL order: site-b's merge FIRST, site-a's second
	// (so an arrival-latest compose names y, not the pinned z).
	fx.st = w3NewStore(t)
	w3ApplyAll(t, fx.st, roster)
	w3Apply(t, fx.st, fx.mergeToZ)
	w3Apply(t, fx.st, fx.mergeToY)

	// Runtime non-degeneracy (standing rule 3), rivals recomputed from the
	// fixture's own data:
	minRule := fx.y
	if fx.z < minRule {
		minRule = fx.z
	}
	arrivalLatest := fx.y // applied second, by construction just above
	clockLatest := fx.y
	if fx.mergeToZ.AuthoredAt.WallMS > fx.mergeToY.AuthoredAt.WallMS {
		clockLatest = fx.z
	}
	w3RequireDistinct(t, "W3-4 contested merge", fx.z, map[string]string{
		"retired min(canonical_id) [m26]": minRule,
		"arrival-order latest [m10]":      arrivalLatest,
		"clock-order latest [m1]":         clockLatest,
	})
	return fx
}

// TestW3ContestedMergeOneComposedPath — W3-4: every reader whose composed
// path exists at W3 resolves through the ONE composed path, identically —
// asserted here for the domain readers (composed person, the derived
// resolution snapshot, Persons()/dump person data); the web surfaces
// (picker, C-9 display, intake's unconfirmed clearing) assert the same
// fixture in internal/web's w3_reader_surfaces_test.go. The reader
// enumeration is HAND-KEPT — stated honestly, per the criterion: nothing
// structural detects an unenumerated reader; each clone chunk extends it.
func TestW3ContestedMergeOneComposedPath(t *testing.T) {
	fx := w3BuildContestedMerge(t)

	if p := w3Person(t, fx.st, fx.x); p.SameAs == nil || *p.SameAs != fx.z {
		t.Fatalf("W3-4 (m26): composed same_as = %v, want the PINNED record-order winner %q — %q is the retired min(canonical_id) rule's answer, and its resurrection is exactly what this fixture exists to refuse", p.SameAs, fx.z, fx.y)
	}
	if got := w3MergeMap(t, fx.st).Resolve(fx.x); got != fx.z {
		t.Fatalf("W3-4 (m9/m26): derived snapshot Resolve(%s) = %s, want %s — a second or stale resolution path disagrees with the composed state", fx.x, got, fx.z)
	}
	for _, q := range mustPersons(t, fx.st) {
		if q.PersonID == fx.x {
			if q.SameAs == nil || *q.SameAs != fx.z {
				t.Fatalf("W3-4: Persons() (dump person data) shows same_as %v for X, want %q — every listed reader must answer identically", q.SameAs, fx.z)
			}
		}
	}
}

// ⟦was: "TestW3LegacyRefAliasRowDoesNotSteerPersonResolution"⟧ — W3-4's
// second-path-would-differ fixture in its sharpest form: a LEGACY
// ref_alias row (the mechanism still alive in the tree until W8 deletes
// it, P-3 §5) maps X somewhere the composed same_as does not. Person
// resolution must answer from the composed path ONLY. Kills m9: a reader
// still consulting the ref_alias map for persons returns W here.
// ⚠⚠ ⟦RETIRED — §60, 2026-09-03⟧ KIND B (site 12) — (3) ATTEMPTED, THEN (1) RETIRE.
// Superseded lines preserved verbatim below; nothing deleted.
//
// WHAT IT PROVED: W3-4's sharpest second-path fixture, and the named producer for m9 —
// that with a DIVERGENT legacy row mapping X somewhere the composed same_as does not,
// PERSON resolution still answers the composed winner. Its own words: "a reader still
// consulting the ref_alias map for persons returns W here."
// WHICH RULING REMOVED ITS SUBJECT: P-3 §5 deletes the mechanism the divergent row was
// authored through, so the distractor is unconstructible and the test would pass
// vacuously and permanently.
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
// ⭐ COVERAGE COST: m9's PERSON producer is gone. ⚠ Its sibling arm survives untouched —
// the composed-path assertion immediately above this test ("every listed reader must
// answer identically") still runs and is unaffected.

// func ⟦was: "TestW3LegacyRefAliasRowDoesNotSteerPersonResolution"⟧(t *testing.T) {
// 	fx := w3BuildContestedMerge(t)
// 	if _, err := CreateAlias(fx.st, "site-a", 1, fx.x, fx.w, 0, "default"); err != nil {
// 		t.Fatalf("fixture: legacy ref_alias authoring failed: %v", err)
// 	}
// 	if fx.w == fx.z {
// 		t.Fatalf("fixture defect: the legacy target equals the composed winner — the second path cannot differ (standing rule 3)")
// 	}
// 	if got := w3MergeMap(t, fx.st).Resolve(fx.x); got != fx.z {
// 		t.Fatalf("W3-4 (m9): with a divergent legacy ref_alias row (X→%s) present, person resolution answered %s, want the composed winner %s — person aliasing via ref_alias is superseded at W3 (P-3 §5) and must steer NOTHING", fx.w, got, fx.z)
// 	}
// 	if p := w3Person(t, fx.st, fx.x); p.SameAs == nil || *p.SameAs != fx.z {
// 		t.Fatalf("W3-4 (m9): composed same_as changed under a legacy ref_alias row: %v", p.SameAs)
// 	}
// }

// --- W3-17: the wire-formed cycle ---------------------------------------

// w3CycleEdge is one parsed same_as assertion from the log.
type w3CycleEdge struct {
	env    store.Envelope
	source string
	target string
	order  int // arrival index at this post
}

// TestW3WireFormedCycleCollapsesToPinnedRepresentative — W3-17, the
// criterion-specified cross-site 2-cycle fixture: members {a, b}, a < b;
// E1: a.same_as→b authored AT SITE-B, earlier AuthoredAt, earlier arrival
// at both posts, yet LATEST in record order ("site-b" > "site-a" decides);
// E2: b.same_as→a authored AT SITE-A, later clock, later arrival.
// Resolve(a) and Resolve(b) both return the PINNED literal b (E1's
// target — the record-order-latest cycle-forming assertion) at BOTH
// posts, across repeated snapshot loads, under the harness's own step
// budget (W4-9's fixed mechanical form). The discrimination table is
// re-verified at RUNTIME from the read-back data (standing rule 3 — this
// exact fixture WAS degenerate at rev 2 and three lenses were needed).
func TestW3WireFormedCycleCollapsesToPinnedRepresentative(t *testing.T) {
	origin := w3NewStore(t)
	p1, p2, roster := w3RosterPair(t, origin, "Cycle Member One", "Cycle Member Two")
	a, b := p1, p2
	if b < a {
		a, b = b, a
	}

	// E1 — a→b, authored at site-b on a real primed edge, EARLIER clock.
	e1 := w3AuthorRemote(t, roster, "site-b", 1, w3Base+1*w3Hour, func(rs *store.Store) {
		if _, err := AmendPerson(rs, "site-b", 1, PersonAmendment{PersonID: a, SameAs: w3Str(b)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})[0]
	// E2 — b→a, authored at site-a on a real primed edge that has NEVER
	// seen E1 (edges refuse cycles locally; folds never refuse — the cycle
	// forms only on the wire), LATER clock.
	e2 := w3AuthorRemote(t, roster, "site-a", 1, w3Base+2*w3Hour, func(rs *store.Store) {
		if _, err := AmendPerson(rs, "site-a", 1, PersonAmendment{PersonID: b, SameAs: w3Str(a)}, 0, "default"); err != nil {
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

	// --- runtime discrimination table (standing rule 3) -----------------
	// Recomputed from one post's READ-BACK log, not from the constants.
	edges := w3CycleEdges(t, posts["post-a"], map[string]bool{a: true, b: true})
	if len(edges) != 2 {
		t.Fatalf("fixture defect: %d cycle edges read back, want 2", len(edges))
	}
	// Arrival order is not recoverable from the log (it is exactly what the
	// key does NOT encode); it is fixed by construction above — E1 applied
	// first at both posts — and stamped here by key match.
	for i := range edges {
		switch edges[i].env.Key() {
		case e1.Key():
			edges[i].order = 0
		case e2.Key():
			edges[i].order = 1
		default:
			t.Fatalf("fixture defect: unexpected cycle edge %v", edges[i].env.Key())
		}
	}
	recordLatest, recordEarliest := edges[0], edges[1]
	if w3KeyBefore(recordLatest.env, recordEarliest.env) {
		recordLatest, recordEarliest = recordEarliest, recordLatest
	}
	arrivalLatest := edges[0]
	if edges[1].order > edges[0].order {
		arrivalLatest = edges[1]
	}
	clockLatest := edges[0]
	if edges[1].env.AuthoredAt.WallMS > edges[0].env.AuthoredAt.WallMS {
		clockLatest = edges[1]
	}
	pinned := recordLatest.target // the mandated rule — must equal b
	if pinned != b {
		t.Fatalf("fixture defect: the record-order-latest cycle edge targets %q, want the pinned %q — the fixture no longer encodes the criterion", pinned, b)
	}
	if recordLatest.env.AuthoredAt.WallMS >= recordEarliest.env.AuthoredAt.WallMS {
		t.Fatalf("fixture defect: the record-order-latest edge must carry the EARLIER AuthoredAt (clock contradicts key), got %d >= %d", recordLatest.env.AuthoredAt.WallMS, recordEarliest.env.AuthoredAt.WallMS)
	}
	if arrivalLatest.env.Key() == recordLatest.env.Key() {
		t.Fatalf("fixture defect: the record-order-latest edge also arrived last — arrival order cannot disagree (standing rule 3)")
	}
	minRule := a
	if b < minRule {
		minRule = b
	}
	w3RequireDistinct(t, "W3-17 cycle representative", pinned, map[string]string{
		"min-of-cycle-ids [m27]":            minRule,
		"identity/no-collapse, Resolve(a)":  a,
		"arrival-order latest target [m27]": arrivalLatest.target,
		"clock-order latest target [m27]":   clockLatest.target,
		"source-of-latest [m27]":            recordLatest.source,
		"target-of-earliest [m27]":          recordEarliest.target,
	})

	// --- the pinned assertion, both posts, repeated loads (m27's
	// map-iteration pick reds deterministically in practice), each under
	// the harness's own step budget (m23 reds as budget exhaustion
	// attributed to THIS test, never a suite-timeout abort) --------------
	for name, st := range posts {
		for i := 0; i < 8; i++ {
			m := w3MergeMap(t, st)
			if got := w3BudgetedResolve(t, m, a); got != b {
				t.Fatalf("W3-17 (m27) at %s, load %d: Resolve(%s) = %q, want the PINNED representative %q — the identity shape returns a, min-of-cycle-ids returns %q, and every order rival returns a non-pinned id on this fixture", name, i, a, got, b, minRule)
			}
			if got := w3BudgetedResolve(t, m, b); got != b {
				t.Fatalf("W3-17 (m27) at %s, load %d: Resolve(%s) = %q, want %q", name, i, b, got, b)
			}
		}
	}

	// --- the AR2-2 carve-out, recorded in the criterion hunk: the
	// representative is stable only UP TO the latest cycle-edge key — an
	// idempotent re-assertion of the b→a edge with a later key (site-b,
	// wire-shaped: the local edge would refuse it as standing/no-op, the
	// fold never refuses) flips it, partition unchanged, both posts
	// identical. A CACHED label (m9's shape) fails to flip.
	e3 := w3WireAmendment(t, "site-b", 1, e1.Seq+1, w3Base+3*w3Hour, b, map[string]any{"same_as": a})
	for name, st := range posts {
		w3Apply(t, st, e3)
		m := w3MergeMap(t, st)
		if got := w3BudgetedResolve(t, m, a); got != a {
			t.Fatalf("W3-17 carve-out (AR2-2 / m9's caching shape) at %s: after a later-keyed re-assertion of the b→a edge, Resolve(a) = %q, want the flipped representative %q — the label must be recomputed from composed state, never cached", name, got, a)
		}
		if got := w3BudgetedResolve(t, m, b); got != a {
			t.Fatalf("W3-17 carve-out at %s: Resolve(b) = %q, want %q", name, got, a)
		}
	}
}

// w3CycleEdges parses the same_as assertions among members from the post's
// full log, with arrival order.
func w3CycleEdges(t *testing.T, st *store.Store, members map[string]bool) []w3CycleEdge {
	t.Helper()
	var out []w3CycleEdge
	for _, stream := range [][2]interface{}{{"site-a", int64(1)}, {"site-b", int64(1)}} {
		for _, e := range w3Events(t, st, stream[0].(string), stream[1].(int64)) {
			var pl struct {
				Type     string  `json:"type"`
				PersonID string  `json:"person_id"`
				SameAs   *string `json:"same_as"`
			}
			if err := json.Unmarshal(e.Payload, &pl); err != nil {
				continue
			}
			if pl.Type == w3EvtPersonAmended && members[pl.PersonID] && pl.SameAs != nil && members[*pl.SameAs] {
				out = append(out, w3CycleEdge{env: e, source: pl.PersonID, target: *pl.SameAs})
			}
		}
	}
	// Arrival order is stamped by the CALLER (it is precisely what the log
	// key does not encode).
	return out
}

// w3KeyBefore is a test-local lexicographic (site, epoch, seq) compare —
// deliberately NOT store.Key.Less, so this fixture's record-order
// derivation cannot be circular with the comparator under test (W3-11
// certifies Less separately).
func w3KeyBefore(a, b store.Envelope) bool {
	if a.SiteID != b.SiteID {
		return a.SiteID < b.SiteID
	}
	if a.NodeEpoch != b.NodeEpoch {
		return a.NodeEpoch < b.NodeEpoch
	}
	return a.Seq < b.Seq
}
