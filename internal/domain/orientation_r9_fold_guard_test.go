// orientation_r9_fold_guard_test.go — closes RA1-OWNMINTED-RERUN.md's
// latent R-9 hole: "with visit.person_id != the completion's person_id, a
// completion FOR ONE PERSON OPENS A VISIT FOR A DIFFERENT PERSON who has no
// orientation at that site." Own-site, so not a D-87 breach — but any
// future store.Author caller that bypasses the edge (the only PLANNED one
// being the legacy-visit importer, ~1,927 orientation-less rows) could
// carry a mismatched visit.person_id and put someone on the muster with
// ZERO orientation check. Closed AT THE FOLD (applyOrientationCompleted,
// orientation.go): the minted visit's person is forced to the completion's
// own PersonID, never trusted from the payload.
//
// This is a FIX proof, not a survivor-mutant witness — it demonstrates the
// hole is closed today, not that a hypothetical regression would be
// caught (though it would: reverting the fold guard turns this RED).
package domain

import "testing"

func TestOrientationCompletionNeverOpensAVisitForADifferentPerson(t *testing.T) {
	st := w9bNewStore(t)
	w9bSeedPersons(t, st, "site-a", 1, "diaz", "quinn")

	// A hand-authored completion for diaz, bypassing the real edge (the
	// shape a future importer — or any other direct store.Author caller —
	// would take), whose visit sub-payload names QUINN instead. Quinn holds
	// no orientation anywhere.
	p := map[string]any{
		"type": evtOrientationCompleted, "person_id": "diaz", "recorded_by": "importer",
		"visit": map[string]any{"visit_id": "site-b-visit-mismatch", "person_id": "quinn", "person_name": "quinn"},
	}
	env, err := ra1Author(t, st, "site-b", p)
	if err != nil {
		t.Fatalf("funnel: %v", err)
	}

	// diaz — the completing person — must hold the derived orientation AND
	// the minted visit, exactly as an ordinary completion would.
	if orientHeld(t, st, "diaz", "site-b") == nil {
		t.Fatal("diaz does not hold site-b's orientation after completing it")
	}
	diazOpen, err := OpenVisitsAt(st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	sawDiaz := false
	for _, v := range diazOpen {
		if v.PersonID == "quinn" {
			t.Fatalf("R-9 HOLE STILL OPEN: quinn has an open visit at site-b (visit_id=%s) despite holding NO orientation there — diaz's completion opened it for a different person", v.VisitID)
		}
		if v.PersonID == "diaz" {
			sawDiaz = true
			if v.VisitID != "site-b-visit-mismatch" {
				t.Errorf("diaz's visit id = %q, want the payload's own visit_id %q (only the PERSON is corrected, not the identity of the visit)", v.VisitID, "site-b-visit-mismatch")
			}
		}
	}
	if !sawDiaz {
		t.Fatal("diaz (the completing person) has no open visit at site-b — the fold guard over-corrected and dropped the entry entirely")
	}
	if orientHeld(t, st, "quinn", "site-b") != nil {
		t.Fatal("quinn unexpectedly holds site-b's orientation despite never completing it")
	}
	if got := ra1Count(t, st, `SELECT count(*) FROM visit WHERE person_id='quinn'`); got != 0 {
		t.Fatalf("quinn has %d visit row(s) anywhere, want 0 — the mismatched person_id must never reach the visit table", got)
	}
	if got := ra1Count(t, st, `SELECT count(*) FROM visit WHERE visit_id='site-b-visit-mismatch' AND person_id='diaz'`); got != 1 {
		t.Fatalf("the minted visit row is not tagged to diaz: %d", got)
	}
	t.Logf("MEASURED: completion env %v for diaz, carrying visit.person_id=quinn, minted a visit for diaz only — quinn holds no orientation and no visit anywhere", env.Key())
}
