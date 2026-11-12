// determinism_test.go — "reference folds replay deterministically" (chunk-
// plan C5 done-when), exercised the same way C2's own harness proves it for
// the framework generally (internal/store/c2_test.go
// TestReplayFromZeroPermutedOrderDeterminism): build a reference store
// incrementally, replay the same events into blank stores in several
// permuted delivery orders, assert byte-identical dumps. Covers all four
// domain projections (company, workgroup, person, ref_alias) together,
// across three synthetic sites/streams — nothing here assumes exactly two
// peers, matching the framework-wide convention.
package domain

import (
	"bytes"
	"encoding/json"
	"math/rand"
	"testing"

	"sitecontrol/internal/store"
)

// sharedConflictDuplicateID is a duplicate_id that BOTH synthetic foreign
// streams (site-x and site-y) independently alias to their OWN, DIFFERENT
// canonical — exactly the "two sites, partitioned, each author an alias for
// the same duplicate" case a reviewer found this harness could not detect
// (each stream previously only ever aliased its own site-prefixed IDs, so
// duplicate_id never collided across streams). Both authorings are legal at
// creation time (CreateAlias only refuses re-aliasing a duplicate the LOCAL
// store already knows about); convergence across delivery order is the
// fold's job, tested below.
// ⚠ ⟦RETIRED — §60, 2026-09-03⟧ Part of sites 7–8's disposition, recorded here rather
// than left as a dangling fossil: this id existed ONLY to key the cross-site alias
// conflict the two RefAliasCreated payloads posed. With those gone (D-47), nothing
// references it. It is KEPT, not deleted, because the paragraph above it is the
// written record of WHY that conflict was authorable at all, and Go does not fault
// an unused package-level const.
// ⟦was: (unchanged — the declaration below is the superseded line, preserved live)⟧
const sharedConflictDuplicateID = "shared-conflict-company-dup"

// foreignRefStream builds a self-contained stream of company/workgroup/
// person/ref_alias events AS IF authored entirely by siteID — the injected-
// foreign-event mechanism (chunk-plan C2 done-when (d)) that makes this
// testable before replication exists. IDs are hand-built (not newID) so the
// stream is reproducible across calls in a single test.
func foreignRefStream(t *testing.T, siteID string, epoch int64) []store.Envelope {
	t.Helper()
	company1 := siteID + "-company-1111111111111111"
	company2 := siteID + "-company-2222222222222222"
	trade1 := siteID + "-workgroup-3333333333333333"
	person1 := siteID + "-person-4444444444444444"

	payloads := []any{
		CompanyCreated{Type: "company_created", CompanyID: company1, Name: "Foreign Co"},
		CompanyCreated{Type: "company_created", CompanyID: company2, Name: "Foreign Co Dup"},
		// ⟦LOCK — §60, 2026-09-03⟧ DISPOSITION (2), SITES 7–8: the stream no longer
		// injects a RETIRED event type. [[D-47]]: "ref_alias_created must NOT be carried
		// into v10's frozen event-type vocabulary… A freeze list that carries it forward
		// re-creates the retired mechanism as a frozen ghost." A determinism fixture that
		// keeps authoring one is that freeze list in miniature — so the live criterion is
		// determinism over a vocabulary that NO LONGER CONTAINS the term, asserted below.
		//
		// ⚠ TWO FURTHER DEFECTS IN THIS ONE LITERAL, BOTH MASKED BY THE ERRORS ABOVE:
		//   (a) `CompanyID` is not a field of TradeCreated (refdata.go:53-58) — ⟦DR-W8-2⟧,
		//       the same company-scoping deletion as the arity class. This is scope §8's
		//       29th error, which no measurement in this pass could see until now.
		//   (b) `workgroup_created` is ITSELF a retired spelling — trade_amend.go:67 pins
		//       `trade_created`. w8_carrier_test.go:303 names the retired pair as
		//       {"ref_alias_created", "workgroup_created"}, so this fixture was injecting
		//       BOTH halves of what D-47 forbids while the compiler reported neither.
		// ⟦was: "TradeCreated{Type: \"workgroup_created\", TradeID: trade1, CompanyID: company1, Name: \"Crew\"},"⟧
		TradeCreated{Type: "trade_created", TradeID: trade1, Name: "Crew"},
		PersonCreated{Type: "person_created", PersonID: person1, Name: "Foreign Person", CompanyID: &company1, TradeID: &trade1},
		// ⟦was: "RefAliasCreated{Type: \"ref_alias_created\", DuplicateID: company2, CanonicalID: company1},"⟧
		// ⟦was: "// The conflicting pair: siteID's own alias of the SHARED duplicate to"⟧
		// ⟦was: "// siteID's OWN company1 — site-x and site-y disagree about what the"⟧
		// ⟦was: "// shared duplicate's canonical should be."⟧
		// ⟦was: "RefAliasCreated{Type: \"ref_alias_created\", DuplicateID: sharedConflictDuplicateID, CanonicalID: company1},"⟧
	}
	out := make([]store.Envelope, len(payloads))
	for i, p := range payloads {
		b, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("marshal foreign stream payload %d: %v", i, err)
		}
		out[i] = store.Envelope{
			SiteID: siteID, NodeEpoch: epoch, Seq: int64(i + 1),
			SchemaVersion: store.SchemaVersion, OriginAuth: siteID,
			Payload: b,
		}
	}
	return out
}

// interleave merges streams into one delivery order, preserving each
// stream's internal order (mirrors internal/store/c2_test.go's helper of the
// same name and purpose; not importable across packages, so reimplemented
// here at the same size).
func interleave(rng *rand.Rand, streams ...[]store.Envelope) []store.Envelope {
	idx := make([]int, len(streams))
	var out []store.Envelope
	for {
		var live []int
		for i, s := range streams {
			if idx[i] < len(s) {
				live = append(live, i)
			}
		}
		if len(live) == 0 {
			return out
		}
		pick := live[rng.Intn(len(live))]
		out = append(out, streams[pick][idx[pick]])
		idx[pick]++
	}
}

func TestReferenceFoldsReplayDeterministically(t *testing.T) {
	ref := newDomainTestStore(t)
	c, err := CreateCompany(ref, "site-a", 1, "Local Co", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ DR-W8-2 arity: the company argument is dropped — a trade is a site-wide open set, not scoped to a company. ⟦was: "if _, err := CreateTrade(ref, "site-a", 1, c.CompanyID, "Local Crew", 0, "default"); err != nil {"⟧
	if _, err := CreateTrade(ref, "site-a", 1, "Local Crew", 0, "default"); err != nil {
		t.Fatal(err)
	}
	if _, err := CreatePerson(ref, "site-a", 1, "Local Person", &c.CompanyID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}

	sx := foreignRefStream(t, "site-x", 1)
	sy := foreignRefStream(t, "site-y", 2) // a later-epoch stream, for variety
	for _, ev := range sx {
		if _, err := ref.Apply(ev); err != nil {
			t.Fatalf("apply sx %v: %v", ev.Key(), err)
		}
	}
	for _, ev := range sy {
		if _, err := ref.Apply(ev); err != nil {
			t.Fatalf("apply sy %v: %v", ev.Key(), err)
		}
	}
	refDump, err := ref.DumpProjections()
	if err != nil {
		t.Fatal(err)
	}

	sa, err := ref.Events("site-a", 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}

	orders := [][]store.Envelope{
		append(append(append([]store.Envelope{}, sa...), sx...), sy...),
		append(append(append([]store.Envelope{}, sy...), sx...), sa...),
	}
	for seed := int64(0); seed < 5; seed++ {
		orders = append(orders, interleave(rand.New(rand.NewSource(seed)), sa, sx, sy))
	}

	for i, order := range orders {
		blank := newDomainTestStore(t)
		for _, ev := range order {
			if _, err := blank.Apply(ev); err != nil {
				t.Fatalf("order %d: apply %v: %v", i, ev.Key(), err)
			}
		}
		gotDump, err := blank.DumpProjections()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(refDump, gotDump) {
			t.Fatalf("order %d: replayed dump diverges from incrementally-built dump\n ref: %s\n got: %s", i, refDump, gotDump)
		}
	}

	// ⚠⚠ ⟦RETIRED — §60, 2026-09-03⟧ KIND A (site 9) — DISPOSITION (1), RETIRE.
	// Superseded lines preserved verbatim:
	//   ⟦was: "// Pin the conflict-resolution mechanism explicitly, not just \"some byte"⟧
	//   ⟦was: "// somewhere matched\": the shared duplicate must resolve to the"⟧
	//   ⟦was: "// lexicographically smaller of the two conflicting canonicals"⟧
	//   ⟦was: "// (site-x's company1 < site-y's company1, since \"site-x-...\" < \"site-y-...\")"⟧
	//   ⟦was: "// REGARDLESS of which site's alias event was delivered first."⟧
	//   ⟦was: "refAliases, err := LoadAliasMap(ref)"⟧
	//   ⟦was: "if err != nil {"⟧
	//   ⟦was: "\tt.Fatal(err)"⟧
	//   ⟦was: "}"⟧
	//   ⟦was: "wantCanonical := \"site-x-company-1111111111111111\""⟧
	//   ⟦was: "if got := refAliases[sharedConflictDuplicateID]; got != wantCanonical {"⟧
	//   ⟦was: "\tt.Fatalf(\"shared duplicate %s resolves to %s, want %s (min(canonical_id) tiebreak, order-independent)\", sharedConflictDuplicateID, got, wantCanonical)"⟧
	//   ⟦was: "}"⟧
	//
	// WHAT IT PROVED: that `applyRefAliasCreated` resolved a cross-site canonical_id
	// conflict by min(), independently of delivery order.
	// WHICH RULING REMOVED ITS SUBJECT: P-3 §5 deletes the fold; the conflict it posed
	// cannot be authored any more, so the pin has no subject.
	// ⭐ COVERAGE COST — NIL ON THIS LIMB, and that is measured rather than assumed: the
	// composed merge is order-independent by a DIFFERENT mechanism (ascending record
	// order, company_amend.go:398-415), and that property has its own live test,
	// TestW5dReversalOrderIndependentAcrossStreams (w5d_reversal_test.go:90), which
	// permutes cross-stream delivery of one event set and requires byte-identical state
	// AND identical composed reads. The property survives; only this fold's spelling of
	// it is gone.

	// --- ⟦LOCK — §60, 2026-09-03⟧ DISPOSITION (2): RE-EXPRESS AS ABSENCE -------
	//
	// The live criterion D-47 leaves behind: the vocabulary this determinism property is
	// proven OVER must no longer contain a retired type. Asserted against the DURABLE
	// surface — the wire `type` in the envelopes actually applied — never a Go symbol
	// and never a shim (§58: a shim answers the assertion instead of the world).

	// CONTROL FIRST, AND PRODUCED: the predicate must see its own plant, or the clean
	// pass below says nothing at all.
	plant := store.Envelope{
		SiteID: "site-plant", NodeEpoch: 1, Seq: 1,
		SchemaVersion: store.SchemaVersion, OriginAuth: "site-plant",
		Payload: json.RawMessage(`{"type":"ref_alias_created","duplicate_id":"d","canonical_id":"c"}`),
	}
	if n := retiredVocabIn([]store.Envelope{plant}); n != 1 {
		t.Fatalf("§60 (2) CONTROL FAILED: the predicate found %d planted retired-vocabulary envelope(s), want 1. "+
			"A check that cannot see its own plant proves nothing — which is how the arm this replaces stayed "+
			"green while its subject was unguarded (§55/§58)", n)
	}
	if n := retiredVocabIn([]store.Envelope{{Payload: json.RawMessage(`{"type":"company_created"}`)}}); n != 0 {
		t.Fatalf("§60 (2) CONTROL FAILED (negative arm): a live event type was counted as retired")
	}

	// --- THE ASSERTION ---------------------------------------------------------
	applied := append(append(append([]store.Envelope{}, sa...), sx...), sy...)
	if n := retiredVocabIn(applied); n != 0 {
		t.Errorf("§60 (2) ((D-47)): %d envelope(s) in the replayed stream set carry a RETIRED event type %v. "+
			"Replay determinism proven over a vocabulary that still contains a retired term is exactly the "+
			"\"freeze list that carries it forward\" D-47 forbids — it re-creates the retired mechanism as a "+
			"frozen ghost, and the determinism assertion above would certify the ghost as reproducible.",
			n, retiredEventTypes)
	}
}

// retiredEventTypes is the retired pair, cited from W8's own list rather than
// hand-typed here: w8_carrier_test.go:303 declares
// `deleted := []string{"ref_alias_created", "workgroup_created"}`.
// ⟦LOCK — §60, 2026-09-03⟧
var retiredEventTypes = []string{"ref_alias_created", "workgroup_created"}

// retiredVocabIn counts envelopes whose payload `type` is a retired spelling.
// It reads the WIRE surface, never a Go symbol. ⟦LOCK — §60, 2026-09-03⟧
func retiredVocabIn(evs []store.Envelope) int {
	n := 0
	for _, e := range evs {
		var p struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(e.Payload, &p) != nil {
			continue
		}
		for _, r := range retiredEventTypes {
			if p.Type == r {
				n++
			}
		}
	}
	return n
}
