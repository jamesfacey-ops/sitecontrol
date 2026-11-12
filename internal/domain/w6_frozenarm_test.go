// w6_frozen_arm_test.go — THE FROZEN ARM, constructed end to end.
//
// This is the chain the W6 arbitration re-derived, and the reason W6-2,
// W6-3 and W6-9(a) carry an explicit SCOPE instead of an unqualified claim:
//
//	a peer's `person_created` with an EMPTY name folds UNCHECKED
//	  (refdata.go:396-417 — "The fold NEVER refuses (W3-2)")
//	    -> that person CHECKS IN
//	      (tripauthor.go:406-408 refuses only p == nil)
//	        -> person_snapshot_name is "" (or, under omitempty, the key is
//	           absent and the column is NULL)
//	          -> DR-W6-3 rules that ABSENT
//	            -> the frozen closure falls to ARM 2, the LIVE name
//	              -> an amendment later fixes the name
//	                -> THE SAME VISIT SET REPRINTS UNDER A DIFFERENT NAME,
//	                   IN A DIFFERENT SORT POSITION (muster.go:254, :277)
//
// ⚠ THAT IS CERTIFIED BEHAVIOUR, NOT A DEFECT. Adjudication #7's ruled
// middle arm is deliberate: "still show the id, never drop the person"
// outranks freezing a blank. What the criteria do about it is SCOPE the
// invariance claims (W6-2, W6-3), mint (m6-1e), and make the printed sheet
// SAY SO (W6-9) — narrowing a criterion does not narrow a sentence on paper.
//
// So this test asserts the class POSITIVELY. It is the end-to-end killer of
// (m6-1e) — a TWO-ARM frozen build (snapshot -> raw id) renders the raw
// PersonID here after the amendment, where a name was available.
//
// EXPECTED RED at f33c4c7: NONE for the pre-amendment half (the pre-chunk
// code answers identically — plan §2.5 row 12). The post-amendment half also
// passes pre-chunk, for the same reason. ⚠ ITS CLEAN PASS IS THEREFORE NOT
// EVIDENCE; its evidence is (m6-1e), the two-arm build, which this fixture is
// the only end-to-end arrangement that can kill.
package domain

import (
	"encoding/json"
	"testing"

	"sitecontrol/internal/store"
)

// w6WirePersonCreated hand-builds a peer's person_created — the events no
// authoring edge will mint. createPerson REFUSES name == ""
// (refdata.go:599-601) and AmendPerson refuses blanking (:589-591); the FOLD
// never refuses, which is this project's stated asymmetry and the only door
// an empty roster name comes through.
func w6WirePersonCreated(t *testing.T, siteID string, epoch, seq int64, personID, name string, fields map[string]any) store.Envelope {
	t.Helper()
	payload := map[string]any{"type": "person_created", "person_id": personID, "name": name}
	for k, v := range fields {
		payload[k] = v
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	env := store.Envelope{
		SiteID: siteID, NodeEpoch: epoch, Seq: seq,
		SchemaVersion: store.SchemaVersion, OriginAuth: siteID,
		OccurredAt: w3Base, TimeSource: "default",
		Payload: json.RawMessage(raw),
	}
	env.AuthoredAt.WallMS = w3Base
	env.AuthoredAt.MaxErrorUS = 42
	env.AuthoredAt.State = store.ClockSynced
	return env
}

func TestW6AnEmptySnapshotFallsToTheLiveNameAndIsNotAmendmentInvariant(t *testing.T) {
	const (
		peerID      = "pb-1"
		peerFixed   = "Abbot, Cy" // sorts BEFORE the other member
		otherName   = "Mills, Rae"
		peerRawShow = peerID // what an unnameable person renders as today
	)
	st := w6NewStore(t)
	co, err := CreateCompany(st, "site-a", 1, "Northwind Drilling", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ DR-W8-2 arity: the company argument is dropped — a trade is a site-wide open set, not scoped to a company. ⟦was: "wg, err := CreateTrade(st, "site-a", 1, co.CompanyID, "Rig Crew", 0, "default")"⟧
	wg, err := CreateTrade(st, "site-a", 1, "Rig Crew", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	other, err := CreatePerson(st, "site-a", 1, otherName, &co.CompanyID, &wg.TradeID, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	// --- the first link: the fold accepts an EMPTY name from a peer ------
	w3Apply(t, st, w6WirePersonCreated(t, "site-b", 1, 1, peerID, "", map[string]any{
		"company_id": co.CompanyID, "workgroup_id": wg.TradeID,
	}))

	// Rule 6 — the chain's first link must actually have landed, or every
	// assertion below is about a person who does not exist.
	p := w3Person(t, st, peerID)
	if p.Name != "" {
		t.Fatalf("PRECONDITION: the peer's person_created did not land an EMPTY name (got %q). The whole chain "+
			"rests on refdata.go:396-417 — \"The fold NEVER refuses (W3-2)\" — and if the fold has started "+
			"refusing, that is a W3 finding and this class is unreachable", p.Name)
	}

	// --- the second link: an unnameable person CHECKS IN -----------------
	w6CheckInUnsolicited(t, st, "site-a", 1, []string{peerID, other.PersonID})
	v := w6VisitFor(t, st, "site-a", peerID)
	if snap, present := w6SnapshotOf(t, v); present && snap != "" {
		t.Fatalf("PRECONDITION: the peer's visit carries a NON-EMPTY snapshot %q. This test's whole subject is the "+
			"class whose snapshot is absent or empty; a non-empty one puts it in W6-2's ordinary case instead", snap)
	}

	persons := w6AllPersons(t, st)
	visits := w6OpenVisits(t, st, "site-a")

	// --- BEFORE the amendment: arm 3, the raw id -------------------------
	wantBefore := []string{otherName, peerRawShow}
	if got := w6BoardNames(w6FrozenBoard(visits, persons)); !w6Equal(got, wantBefore) {
		t.Errorf("W6-5 frozen row (iv) / adjudication #7's LAST RESORT: the frozen board renders %s, want %s. "+
			"An unnameable person must still show the raw id — \"still show the id, never drop the person\" — "+
			"never a blank line on an evacuation sheet.", w6Fmt(got), w6Fmt(wantBefore))
	}

	// --- the amendment fixes the name ------------------------------------
	w6AmendName(t, st, "site-a", 1, peerID, peerFixed)
	persons = w6AllPersons(t, st)
	visits = w6OpenVisits(t, st, "site-a")

	// --- AFTER: arm 2, the LIVE name, in a DIFFERENT sort position -------
	wantAfter := []string{peerFixed, otherName}
	got := w6BoardNames(w6FrozenBoard(visits, persons))
	if !w6Equal(got, wantAfter) {
		hint := ""
		if w6Equal(got, wantBefore) {
			hint = " — that is (m6-1e): a TWO-ARM frozen closure (snapshot -> raw id). Adjudication #7's ruled " +
				"MIDDLE ARM has been removed, so a person whose name is now known prints as a raw id on an " +
				"evacuation sheet. Without this mutant a two-arm frozen build passed all sixteen of revision 1's " +
				"criteria [MEASURED by the round-1 falsifiability lens]"
		}
		t.Errorf("W6-5 frozen row (i) / (m6-1e), END TO END: after the amendment the frozen board renders %s, "+
			"want %s%s.\n"+
			"  ⚠ THIS IS THE SCOPE, ASSERTED POSITIVELY. For a visit whose snapshot is absent or empty the frozen "+
			"board renders the LIVE name and is NOT amendment-invariant — deliberately, by adjudication #7's ruled "+
			"mechanism. W6-2's and W6-3's frozen halves are scoped to visits carrying a NON-EMPTY snapshot for "+
			"exactly this reason, and the sheet is made to say so (W6-9).",
			w6Fmt(got), w6Fmt(wantAfter), hint)
	}

	// --- and the class is REAL: the same visit set moved -----------------
	// Rule 6 — if the amendment did not move anyone, this fixture proves
	// nothing about "a different name in a different sort position".
	if w6Equal(wantBefore, wantAfter) {
		t.Fatalf("FIXTURE DEFECT: the pinned before/after orders are identical, so this fixture cannot show the " +
			"same visit set reprinting under a different name IN A DIFFERENT SORT POSITION — which is the " +
			"whole of the chain this test exists to construct")
	}

	// The LIVE board answers identically here, and that is the point: for
	// this class there is nothing to freeze, so the two boards agree.
	if live := w6BoardNames(w6LiveBoard(visits, persons)); !w6Equal(live, wantAfter) {
		t.Errorf("W6-4: the LIVE board renders %s, want %s — for a visit with no snapshot the two closures must "+
			"agree, because the frozen closure's middle arm IS the live name", w6Fmt(live), w6Fmt(wantAfter))
	}
}
