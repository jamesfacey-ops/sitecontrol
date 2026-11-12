// orientation_ownership_witness_test.go -- ADVERSARIAL WITNESS, tester-go,
// 2026-09-06. Tests the claim in ownership.go/orientation.go that
// orientation_completed/orientation_retracted are safely classified
// ownMinted (not ownPresence): "neither payload names a site at all --
// the row's tag is env.SiteID... there is no target id that could ever
// resolve to a foreign site's row, so D-87 holds by construction."
//
// This file does not argue the claim; it tries to break it, through every
// door reachable from this package:
//  1. a legitimate authoring (proves the harness can author ANYTHING --
//     a zero from a broken harness is not a zero);
//  2. a payload carrying an INJECTED, forged site-naming field, applied
//     through the real edge -- proving the fold never reads it;
//  3. a HAND-CRAFTED wire envelope (the shape a replicated record would
//     have, built the w9b way -- store.Apply, "deliberately permissive
//     about site_id in tests") retracting a person's orientation from a
//     site that never completed it -- proving the fold is inert
//     cross-site, never merely "refused" (there is nothing here to
//     refuse: the fold cannot refuse, W3-2);
//  4. the same, for a person who legitimately holds BOTH sites'
//     orientation -- proving retraction is scoped to exactly one site's
//     derived row, never the other's, even though both rows key on the
//     identical person_id.
package domain

import (
	"fmt"
	"testing"
)

const (
	ownWitnessSiteA = "site-a"
	ownWitnessSiteB = "site-b"
)

// --- 1: the harness can author a legitimate orientation completion+retraction ---

func TestOwnershipWitness1_HarnessCanAuthorALegitimateOrientationThroughTheRealEdge(t *testing.T) {
	st := w9bNewStore(t)
	w9bSeedPersons(t, st, ownWitnessSiteA, 1, "diaz")

	env, err := CompleteOrientation(st, ownWitnessSiteB, 1, "diaz", w9bStr("Pad Site Control"), 0, "default")
	if err != nil {
		t.Fatalf("harness cannot author a legitimate completion: %v -- every downstream result in this file is void until this passes", err)
	}
	row := orientHeld(t, st, "diaz", ownWitnessSiteB)
	if row == nil || row.NodeEpoch != env.NodeEpoch || row.Seq != env.Seq {
		t.Fatalf("legitimate completion did not land: OrientationHeld(diaz, site-b) = %+v, want the completing event's key %v", row, env.Key())
	}

	if _, err := RetractOrientation(st, ownWitnessSiteB, 1, "diaz", w9bStr("Pad Site Control"), 0, "default"); err != nil {
		t.Fatalf("harness cannot author a legitimate retraction: %v", err)
	}
	if row := orientHeld(t, st, "diaz", ownWitnessSiteB); row != nil {
		t.Fatalf("legitimate retraction did not land: OrientationHeld(diaz, site-b) = %+v, want nil", row)
	}
	t.Log("PROVEN: the harness authors and retracts a legitimate orientation through the real edge -- CompleteOrientation/RetractOrientation both landed and were observed.")
}

// --- 2: a payload with an injected, forged site-naming field is inert ---

func TestOwnershipWitness2_InjectedSiteFieldInThePayloadIsIneffectiveAtTheFold(t *testing.T) {
	st := w9bNewStore(t)
	w9bSeedPersons(t, st, ownWitnessSiteA, 1, "diaz")

	// A payload with every legitimate key PLUS forged fields naming a
	// DIFFERENT site under every name a reviewer might guess the fold
	// reads: "site_id", "target_site", "site". applyOrientationCompleted
	// decodes into OrientationCompleted{Type,PersonID,RecordedBy} -- extra
	// JSON keys are silently dropped by encoding/json -- so if the fold
	// secretly consulted one of these, this reproduces it; if it does not
	// (the header's claim), the row still lands tagged to the AUTHORING
	// site (site-b), never "site-a".
	hostile := map[string]any{
		"type": evtOrientationCompleted, "person_id": "diaz",
		"site_id": ownWitnessSiteA, "target_site": ownWitnessSiteA, "site": ownWitnessSiteA,
	}
	env := w9bApplyNext(t, st, ownWitnessSiteB, 1, w9bBase, hostile)

	if row := orientHeld(t, st, "diaz", ownWitnessSiteA); row != nil {
		t.Fatalf("BROKEN: an injected payload field ('site_id'/'target_site'/'site' = site-a) redirected the row to site-a: %+v -- the wire IS representable after all", row)
	}
	row := orientHeld(t, st, "diaz", ownWitnessSiteB)
	if row == nil || row.NodeEpoch != env.NodeEpoch || row.Seq != env.Seq {
		t.Fatalf("the completion did not land at the authoring site either: OrientationHeld(diaz, site-b) = %+v, want the event's key %v", row, env.Key())
	}
	t.Log("PROVEN: a payload forging a foreign site under three plausible field names has zero effect -- the row is tagged to env.SiteID (site-b) exactly as if the forged fields were absent.")
}

// --- 3: a hand-crafted retraction from a site that never completed is inert, not merely refused ---

func TestOwnershipWitness3_HandCraftedForeignRetractionNeverTouchesAnotherSitesRow(t *testing.T) {
	st := w9bNewStore(t)
	w9bSeedPersons(t, st, ownWitnessSiteA, 1, "diaz")

	// diaz legitimately holds site-b's orientation.
	if _, err := CompleteOrientation(st, ownWitnessSiteB, 1, "diaz", w9bStr("Pad Site Control"), 0, "default"); err != nil {
		t.Fatalf("setup: CompleteOrientation(site-b): %v", err)
	}
	before := orientHeld(t, st, "diaz", ownWitnessSiteB)
	if before == nil {
		t.Fatal("setup: diaz does not hold site-b's orientation after completing it")
	}

	// The REAL edge refuses this outright: RetractOrientation(st, site-a,
	// ..., "diaz", ...) returns OrientationNotHeld because site-a's own
	// (diaz, site-a) row does not exist. Confirm that refusal fires -- the
	// edge is not being skipped by accident -- before going around it.
	if _, err := RetractOrientation(st, ownWitnessSiteA, 1, "diaz", w9bStr("Ice Road Post"), 0, "default"); err == nil {
		t.Fatal("setup: RetractOrientation(site-a, diaz) should have been refused NotHeld through the real edge -- it was not, so the hand-crafted bypass below proves nothing")
	} else if ref, ok := err.(*OrientationRefusal); !ok || ref.Code != OrientationNotHeld {
		t.Fatalf("setup: RetractOrientation(site-a, diaz) refused with %v (%T), want *OrientationRefusal{NotHeld}", err, err)
	}

	// Now go AROUND the edge: hand-build the exact wire shape a replicated
	// orientation_retracted record would carry -- SiteID "site-a", naming
	// diaz -- and apply it the way a peer's fold would (store.Apply, which
	// is "deliberately permissive about site_id" -- w9b_seams_test.go
	// header). This models either (a) a compromised site-a authoring
	// stack that skips its own domain edge, or (b) bytes that reached
	// ApplyBatch after clearing repl's Sec8.4 identity check (engine.go:711;
	// TestRecordBearingForeignIdentityIsRejectedNotApplied,
	// internal/repl/repl_test.go:403, is the witness that door is closed
	// in the ordinary case -- this test assumes past it to see what the
	// FOLD alone would do).
	forged := map[string]any{"type": evtOrientationRetracted, "person_id": "diaz", "recorded_by": "Ice Road Post"}
	_ = w9bApplyNext(t, st, ownWitnessSiteA, 1, w9bBase, forged)

	after := orientHeld(t, st, "diaz", ownWitnessSiteB)
	if after == nil {
		t.Fatalf("BROKEN: a hand-crafted site-a retraction removed site-b's row -- diaz no longer holds site-b's orientation")
	}
	if after.NodeEpoch != before.NodeEpoch || after.Seq != before.Seq {
		t.Fatalf("BROKEN: site-b's row changed identity after a foreign retraction: before=%+v after=%+v", before, after)
	}
	if row := orientHeld(t, st, "diaz", ownWitnessSiteA); row != nil {
		t.Fatalf("BROKEN: the dangling site-a retraction produced a site-a derived row: %+v, want nil (no completion exists there)", row)
	}
	raws, err := dumpSection[OrientationRetractionRow](st, "orientation_retraction")
	if err != nil {
		t.Fatalf("dump orientation_retraction: %v", err)
	}
	if len(raws) != 1 || raws[0].SiteID != ownWitnessSiteA || raws[0].PersonID != "diaz" {
		t.Fatalf("orientation_retraction = %+v, want exactly one dangling site-a row for diaz", raws)
	}
	t.Log("PROVEN: the fold applied the forged site-a retraction (never refuses, W3-2) but it is INERT -- site-b's derived row is byte-identical to before, and site-a's stays absent because no completion exists there.")
}

// --- 4: a person holding BOTH sites' orientation -- retraction is scoped to one row only ---

func TestOwnershipWitness4_PersonHoldingBothSitesOrientationRetractionTouchesOnlyItsOwnSite(t *testing.T) {
	st := w9bNewStore(t)
	w9bSeedPersons(t, st, ownWitnessSiteA, 1, "diaz")

	// diaz legitimately completes orientation at BOTH sites -- person
	// records are shared reference data (D-89): one person_id, visited
	// both posts, oriented at both. Two independent (person, site) rows.
	if _, err := CompleteOrientation(st, ownWitnessSiteA, 1, "diaz", w9bStr("Ice Road Post"), 0, "default"); err != nil {
		t.Fatalf("setup: CompleteOrientation(site-a): %v", err)
	}
	if _, err := CompleteOrientation(st, ownWitnessSiteB, 1, "diaz", w9bStr("Pad Site Control"), 0, "default"); err != nil {
		t.Fatalf("setup: CompleteOrientation(site-b): %v", err)
	}
	if orientHeld(t, st, "diaz", ownWitnessSiteA) == nil || orientHeld(t, st, "diaz", ownWitnessSiteB) == nil {
		t.Fatal("setup: diaz should hold BOTH sites' orientation before the test proceeds")
	}

	// LEGITIMATE retraction, through the real edge, at site-a only.
	if _, err := RetractOrientation(st, ownWitnessSiteA, 1, "diaz", w9bStr("Ice Road Post"), 0, "default"); err != nil {
		t.Fatalf("RetractOrientation(site-a, diaz), who holds both: %v", err)
	}

	if row := orientHeld(t, st, "diaz", ownWitnessSiteA); row != nil {
		t.Fatalf("site-a's retraction did not remove site-a's own row: %+v", row)
	}
	bRow := orientHeld(t, st, "diaz", ownWitnessSiteB)
	if bRow == nil {
		t.Fatal("BROKEN: retracting diaz's orientation AT SITE-A also removed site-b's row -- cross-site leakage through the shared person_id")
	}

	// And the hostile direction: a HAND-CRAFTED retraction claiming to be
	// site-a (again, going around the edge, modelling the same forged-bytes
	// scenario as witness 3) while diaz ALSO currently holds site-b's row.
	// Confirm it still only ever touches (diaz, site-a).
	forged := map[string]any{"type": evtOrientationRetracted, "person_id": "diaz", "recorded_by": "forged"}
	_ = w9bApplyNext(t, st, ownWitnessSiteA, 2, w9bBase, forged) // epoch 2: a fresh stream, so seq starts at 1 regardless of site-a's epoch-1 history above

	if row := orientHeld(t, st, "diaz", ownWitnessSiteB); row == nil || row.NodeEpoch != bRow.NodeEpoch || row.Seq != bRow.Seq {
		t.Fatalf("BROKEN: a second, hand-crafted site-a retraction changed site-b's row: before=%+v after=%+v", bRow, row)
	}
	t.Log("PROVEN: with diaz holding orientation at BOTH sites simultaneously, a site-a retraction (legitimate, then hand-crafted) removes ONLY site-a's derived row in every case; site-b's row is untouched by key and by presence.")
}

// --- 5: a site-a retraction with a HIGHER counter than site-b's completion still leaves site-b held ---

// TestOwnershipWitness5_ARoadPostsAheadCounterNeverDominatesAnotherSitesDerivation
// is FALSIFIABILITY-2.md's M-D: rederiveOrientation's retraction query
// carries `AND site_id = ?` at this tip, but witnesses 3 and 4 above only
// ever check the FOREIGN pair (site-a) is inert -- neither one RE-DERIVES
// the OTHER site's pair (site-b's) after the foreign retraction, so "site-b
// is unaffected" is never actually measured; it passes by construction
// because nothing ever asks. This closes that gap by forcing exactly the
// shape a bare `AND site_id` regression would get wrong: site-a's own
// counter run far AHEAD of site-b's (a road post sees far more traffic),
// so its retraction's (node_epoch, seq) key is numerically GREATER than
// site-b's completion's -- if the retraction query ever stopped scoping by
// site_id, that foreign, numerically-later key would beat site-b's own
// completion in rederiveOrientation's Less() comparison (both Keys are
// built with the same siteID limb, so SiteID never decides -- see
// orientation.go's own comment on this), deleting a row that was never
// written and leaving diaz held out at site-b despite a completion that
// genuinely landed there.
func TestOwnershipWitness5_ARoadPostsAheadCounterNeverDominatesAnotherSitesDerivation(t *testing.T) {
	st := w9bNewStore(t)
	w9bSeedPersons(t, st, ownWitnessSiteA, 1, "diaz")

	// diaz legitimately completes AND retracts site-a's orientation, through
	// the real edges only -- no forged bytes needed for this gap.
	if _, err := CompleteOrientation(st, ownWitnessSiteA, 1, "diaz", w9bStr("Ice Road Post"), 0, "default"); err != nil {
		t.Fatalf("setup: CompleteOrientation(site-a): %v", err)
	}
	// Filler traffic at site-a ONLY, pushing its own counter well ahead of
	// site-b's -- exactly "a road post's counter ahead" (FALSIFIABILITY-2.md).
	for i := 0; i < 12; i++ {
		if _, err := CreatePerson(st, ownWitnessSiteA, 1, fmt.Sprintf("site-a filler %d", i), nil, nil, 0, "default"); err != nil {
			t.Fatalf("setup: site-a filler %d: %v", i, err)
		}
	}
	// The retraction: diaz's site-a row is real and legitimately withdrawn,
	// now carrying a HIGH seq (~14) relative to site-b's completion below.
	if _, err := RetractOrientation(st, ownWitnessSiteA, 1, "diaz", w9bStr("Ice Road Post"), 0, "default"); err != nil {
		t.Fatalf("setup: RetractOrientation(site-a): %v", err)
	}
	if row := orientHeld(t, st, "diaz", ownWitnessSiteA); row != nil {
		t.Fatalf("setup: diaz still holds site-a's orientation after retracting it: %+v", row)
	}

	// A little filler at site-b too, so its completion below is not
	// artificially seq=1 -- the shape holds at any counter values, this
	// merely keeps the two streams' numbering visibly independent.
	for i := 0; i < 4; i++ {
		if _, err := CreatePerson(st, ownWitnessSiteB, 1, fmt.Sprintf("site-b filler %d", i), nil, nil, 0, "default"); err != nil {
			t.Fatalf("setup: site-b filler %d: %v", i, err)
		}
	}

	// diaz has NEVER touched site-b before now. This completion is the
	// FIRST and only event naming (diaz, site-b) at all.
	env, err := CompleteOrientation(st, ownWitnessSiteB, 1, "diaz", w9bStr("Pad Site Control"), 0, "default")
	if err != nil {
		t.Fatalf("CompleteOrientation(site-b, diaz): %v", err)
	}

	row := orientHeld(t, st, "diaz", ownWitnessSiteB)
	if row == nil {
		t.Fatalf("BROKEN: diaz does not hold site-b's orientation despite a completion that just landed there (env key %v) -- site-a's higher-numbered, unrelated retraction dominated the derivation", env.Key())
	}
	if row.NodeEpoch != env.NodeEpoch || row.Seq != env.Seq {
		t.Fatalf("site-b's derived row key = (%d,%d), want the completion's own key (%d,%d)", row.NodeEpoch, row.Seq, env.NodeEpoch, env.Seq)
	}
	// And site-a stays exactly as retracted -- this is a two-way proof, not
	// just "site-b happens to be fine".
	if row := orientHeld(t, st, "diaz", ownWitnessSiteA); row != nil {
		t.Fatalf("BROKEN: re-deriving site-b's pair resurrected site-a's retracted row: %+v", row)
	}
	t.Log("PROVEN: site-a's retraction, authored with a numerically HIGHER (node_epoch, seq) than site-b's later completion, neither prevents nor overwrites site-b's own derivation -- each site's rederivation reads only its own scoped rows.")
}
