// case3_invisible_heldout_test.go — CASE 3 (D87-D92-COHERENCE.md, F-3): a
// person left ORIENTED, UN-ENTERED, and INVISIBLE — dropped from the only
// list an officer with a bus stopped at the gate can consult.
//
// THE SHAPE, under the SAME incomplete self-knowledge R-20's harness poses
// (a post whose boot Rejoin failed to recover its own history, serving and
// authoring anyway): site-b epoch 1 holds Diaz out for lack of site-b's
// orientation (a trip_arrival_not_entered row only that stream holds).
// site-b is re-imaged as epoch 2 with the roster recovered but NOT its own
// epoch-1 history; Diaz completes orientation there — findEntryCandidate
// reads this store's own not-entered rows and sees none, so the completion
// carries no entry (deterministic — a payload built from what the local
// store held at authoring time, not a race). The link returns; the
// self-history retry (r20Replay, reused from r20_visit_order_test.go) lands
// epoch 1 AFTER epoch 2 was authored: the not-entered row is now visible,
// Diaz now holds site-b's orientation, and no visit was ever minted for
// them. heldout.go's exclusion assumed "holds this site's orientation"
// implies "was already entered by the completion that granted it" — case 3
// is exactly the completion where that implication is false.
package domain

import (
	"testing"

	"sitecontrol/internal/store"
)

func TestCase3OrientedButNeverEnteredStaysVisibleOnHeldOutAfterLateSelfHistory(t *testing.T) {
	a := newDomainTestStore(t)
	diaz, err := CreatePerson(a, "site-a", 1, "Diaz Case3", nil, nil, 0, "default")
	if err != nil {
		t.Fatalf("seed Diaz: %v", err)
	}

	// site-b epoch 1: the roster is here; Diaz arrives unsolicited without
	// site-b's orientation and is held out.
	b1 := newDomainTestStore(t)
	r20Replay(t, b1, a, "site-a", 1)
	arr, err := RecordUnsolicitedArrivalOriented(b1, "site-b", 1, "", "site-a",
		[]string{diaz.PersonID}, nil, 0, "default", true, true)
	if err != nil {
		t.Fatalf("epoch-1 unsolicited arrival: %v", err)
	}
	if len(arr.NotEntered) != 1 || arr.NotEntered[0] != diaz.PersonID {
		t.Fatalf("fixture: NotEntered = %v, want [%s]", arr.NotEntered, diaz.PersonID)
	}

	// site-b epoch 2: the re-imaged post. Roster recovered from site-a; its
	// OWN epoch-1 history (the not-entered row above) is NOT — boot Rejoin
	// failed, exactly R-20's premise.
	b2 := newDomainTestStore(t)
	r20Replay(t, b2, a, "site-a", 1)
	if _, err := CompleteOrientation(b2, "site-b", 2, diaz.PersonID, nil, 0, "default"); err != nil {
		t.Fatalf("epoch-2 completion: %v", err)
	}

	// Fixture check: the completion truly carried no entry — this store had
	// nothing to find a candidate from.
	visits, err := dumpSectionWhere[Visit](b2, "visit",
		store.Eq{Column: "site_id", Value: "site-b"},
		store.Eq{Column: "person_id", Value: diaz.PersonID})
	if err != nil {
		t.Fatal(err)
	}
	if len(visits) != 0 {
		t.Fatalf("fixture: entry-on-completion minted %d visit(s) though this store held no not-entered row to find", len(visits))
	}

	// The link returns; the self-history retry drains site-b's OWN epoch-1
	// stream into the post AFTER epoch 2 was authored and folded.
	n := r20Replay(t, b2, b1, "site-b", 1)
	t.Logf("late self-history ingest applied %d epoch-1 event(s) after epoch 2 was authored", n)

	// Deterministic result: Diaz now holds site-b's orientation and was
	// never entered — no divergence to measure, just the one state.
	held, err := OrientationHeld(b2, diaz.PersonID, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if held == nil {
		t.Fatal("fixture: Diaz does not hold site-b's orientation after the completion replicated")
	}
	visits, err = dumpSectionWhere[Visit](b2, "visit",
		store.Eq{Column: "site_id", Value: "site-b"},
		store.Eq{Column: "person_id", Value: diaz.PersonID})
	if err != nil {
		t.Fatal(err)
	}
	if len(visits) != 0 {
		t.Fatalf("fixture: %d visit(s) now exist for Diaz at site-b — this is no longer the un-entered shape", len(visits))
	}

	// THE DEFECT: oriented, un-entered, and (pre-fix) dropped from the only
	// surface an officer with a bus stopped at the gate can consult.
	list, err := HeldOutAt(b2, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range list {
		if h.PersonID == diaz.PersonID {
			found = true
		}
	}
	if !found {
		t.Errorf("CASE 3 (D87-D92-COHERENCE.md): Diaz is oriented, un-entered, and ABSENT from HeldOutAt(site-b) = %+v — invisible to the officer with the bus stopped", list)
	}
}
