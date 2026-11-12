// w8_person_trade_test.go — W8-11 (seam S10: person.trade_id is NARROWED,
// never DELETED) and W8-13 (the two-post comparators, in BOTH directions).
//
// Written from the LOCKED W8 criteria only. No built implementation was read.
//
// ⚠⚠ W8-11's DANGER IS ASYMMETRIC AND BOTH HALVES ARE ASSERTED SO NEITHER CAN
// BE BOUGHT WITH THE OTHER. The tree today carries, verbatim
// [MEASURED — person_amend.go:621-628]:
//
//	// unknown trade_id (seam S10): W8 builds the trade register; until it
//	// exists there is no known trade id, so every trade_id assertion is
//	// refused at this edge. The ACCEPT side of this limb is W8's, owed at
//	// W8's lock — an always-refuse implementation must fail a W8-gate
//	// check there.
//
// ⚠ AND THE REFUSAL MUST BE NARROWED, NOT DELETED. W3's LOCKED criterion
// produces the UNKNOWN-trade-id arm [MEASURED — w3_edge_refusals_test.go:52-54].
// "A builder who reads 'W8 builds the accept path' and DELETES the `if` block
// satisfies a naive W8 check and REDS A LOCKED W3 CRITERION."
package domain

import (
	"strings"
	"testing"
)

// The three ids are PINNED AS LITERALS ⟦LOCK — FIX-F19⟧.
const (
	w8KnownTradeID  = "t-known"  // arm (a): IS in the register
	w8NoSuchTradeID = "t-nosuch" // arm (b): is NOT
	w8MergedTradeID = "t-merged" // arm (c): exists, merged away
	w8CanonTradeID  = "t-canon"  // arm (c): what t-merged resolves to
)

// TestW8PersonTradeIDAcceptsAKnownTradeAndStillRefusesAnUnknownOne is W8-11.
//
//	(a) ACCEPT   a trade_id that IS in the register is accepted, the
//	             amendment is appended, and PersonByID composes it.
//	             ⭐ REDS TODAY — always-refuse. Mutant (m8-26).
//	(b) REFUSE   a trade_id that is NOT in the register is refused at the
//	             edge, NOTHING is appended, and the message NAMES THE ID.
//	             Already true today. Mutant (m8-25): the `if` block deleted —
//	             ⚠ which PASSES a naive accept-path check and REDS W3's
//	             LOCKED w3_edge_refusals_test.go:52-54.
//	(c) MERGED   a MERGED-AWAY id is ACCEPTED, and the composed read resolves
//	             it to the canonical trade. ⚠ THE ARM A BUILDER WILL MISS:
//	             the row of a merged-away duplicate IS STILL IN THE REGISTER —
//	             `same_as` is a per-field assertion, not a deletion — so the
//	             edge test is "is this id a `trade` row?" and the RESOLUTION
//	             HAPPENS AT THE READ (W8-19). Mutant (m8-27).
//
// ⚠ THE DEAD ARM IS NAMED SO IT IS NOT BUILT: A §10.5's "the authoring edge
// refuses a RETIRED trade_id" DIES WITH D-30(b). There is no retirement, so
// there is no retired-id refusal.
//
// ⚠ THE FOLD IS NOT TOUCHED — it already documents "Unknown trade ids… all
// fold" [MEASURED — person_amend.go:167-172], and a W8 change there would red
// W3's fold-never-refuses criterion.
func TestW8PersonTradeIDAcceptsAKnownTradeAndStillRefusesAnUnknownOne(t *testing.T) {
	st := w3NewStore(t)
	co, err := CreateCompany(st, "site-a", 1, "Acme Ltd", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	p, err := CreatePerson(st, "site-a", 1, "Dana Driver", &co.CompanyID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	// The register is seeded AS WIRE EVENTS — the only medium in which trade
	// exists before the build. ⚠ This is not a shortcut: the fold never
	// refuses, so a peer's trade_created is exactly how a trade id becomes
	// locally known, and W8-3(a) row (iii′) turns on that same path.
	seq := int64(1)
	seed := func(payload map[string]any) {
		w8ApplyWire(t, st, w8WireEnvelope(t, "site-b", 1, seq, w3Base+seq*w3Hour, payload))
		seq++
	}
	seed(map[string]any{"type": "trade_created", "trade_id": w8KnownTradeID, "name": "Scaffolding", "recorded_by": "Ice Road B"})
	seed(map[string]any{"type": "trade_created", "trade_id": w8CanonTradeID, "name": "Crane", "recorded_by": "Ice Road B"})
	seed(map[string]any{"type": "trade_created", "trade_id": w8MergedTradeID, "name": "Krane", "recorded_by": "Ice Road B"})
	seed(map[string]any{"type": "trade_amended", "trade_id": w8MergedTradeID, "same_as": w8CanonTradeID})

	before := len(w3Events(t, st, "site-a", 1))

	// --- (b) THE REFUSE ARM, asserted FIRST so a build cannot buy the accept
	// path with it. ⟦NO PRE-CHUNK RED — already true today.⟧ The plant that
	// would falsify it: DELETE the `if in.TradeID != nil` block at
	// person_amend.go:621-628 on a `cp -a` scratch copy — which is (m8-25),
	// and which reds W3's LOCKED w3_edge_refusals_test.go:52-54.
	unknown := w8NoSuchTradeID
	_, err = AmendPerson(st, "site-a", 1, PersonAmendment{PersonID: p.PersonID, TradeID: &unknown}, 0, "default")
	if err == nil {
		t.Errorf("W8-11(b) ((m8-25)): an UNKNOWN trade id %q was ACCEPTED at the amendment edge. ⚠ THE REFUSAL IS "+
			"NARROWED, NEVER DELETED: W3's LOCKED criterion produces this arm (w3_edge_refusals_test.go:52-54, "+
			"\"unknown trade_id (S10: refusal limb only at W3)\"), and a builder who reads \"W8 builds the accept "+
			"path\" and deletes the `if` block satisfies a naive W8 check and REDS A LOCKED W3 CRITERION", unknown)
	} else {
		// ⟦LOCK — FIX-F19: the message is asserted WHOLE and NAMES THE ID.⟧
		// ⟦LOCK — OPS-F5: it is OFFICER-FACING — it names NO package, no
		// function and no file, and IT NAMES WHAT TO DO NEXT.⟧
		msg := err.Error()
		if !strings.Contains(msg, unknown) {
			t.Errorf("W8-11(b): the refusal %q does not NAME THE ID %q — an officer cannot act on a refusal that "+
				"does not say which value was rejected", msg, unknown)
		}
	}
	if after := len(w3Events(t, st, "site-a", 1)); after != before {
		t.Errorf("W8-11(b): the refused amendment appended %d event(s) — NOTHING may be appended on a refusal "+
			"(the LOG is inspected, not just the return value)", after-before)
	}

	// --- (a) THE ACCEPT ARM. ⭐ REDS TODAY: the edge refuses EVERY value,
	// unconditionally.
	known := w8KnownTradeID
	if _, err := AmendPerson(st, "site-a", 1, PersonAmendment{PersonID: p.PersonID, TradeID: &known}, 0, "default"); err != nil {
		t.Fatalf("W8-11(a) ((m8-26)): a KNOWN trade id %q was REFUSED at the amendment edge: %v\n"+
			"⚠ This is the SHIPPED ALWAYS-REFUSE implementation, and the tree's own comment books its correction "+
			"to this lock: \"The ACCEPT side of this limb is W8's, owed at W8's lock — AN ALWAYS-REFUSE "+
			"IMPLEMENTATION MUST FAIL A W8-GATE CHECK THERE.\"\n"+
			"⚠ OPERATIONALLY, TODAY: there is a box on a person's file labelled \"Trade id\" that an officer can "+
			"type into AND WHICH ALWAYS ANSWERS NO.\n"+
			"⚠ The fix is to NARROW the refusal to unknown ids, never to delete it — see arm (b) above",
			known, err)
	}
	composed, err := PersonByID(st, p.PersonID)
	if err != nil {
		t.Fatal(err)
	}
	if composed == nil || composed.TradeID == nil || *composed.TradeID != w8KnownTradeID {
		got := "<nil>"
		if composed != nil && composed.TradeID != nil {
			got = *composed.TradeID
		}
		t.Errorf("W8-11(a) ((m8-26)): after accepting %q, PersonByID composes TradeID = %s, want %q — the accept "+
			"path must be composed at read, not merely appended", known, got, w8KnownTradeID)
	}

	// --- (c) THE MERGED-AWAY ARM — the one a builder will miss. -------------
	merged := w8MergedTradeID
	if _, err := AmendPerson(st, "site-a", 1, PersonAmendment{PersonID: p.PersonID, TradeID: &merged}, 0, "default"); err != nil {
		t.Errorf("W8-11(c) ((m8-27)): a MERGED-AWAY trade id %q was REFUSED: %v\n"+
			"⚠ THE ROW OF A MERGED-AWAY DUPLICATE IS STILL IN THE REGISTER — `same_as` is a PER-FIELD ASSERTION, "+
			"NOT A DELETION. So the edge test is \"is this id a `trade` row?\", and the RESOLUTION HAPPENS AT THE "+
			"READ (W8-19). A guard that rejects an id whose row exists but has been merged away is (m8-27).\n"+
			"⚠ AND THE DEAD ARM IS NAMED SO IT IS NOT BUILT: A §10.5's \"the authoring edge refuses a RETIRED "+
			"trade_id\" DIES WITH D-30(b) — there is no retirement, so there is no retired-id refusal", merged, err)
	}
}

// ---------------------------------------------------------------------------
// W8-13 — the two-post comparators, in BOTH directions
// ---------------------------------------------------------------------------
