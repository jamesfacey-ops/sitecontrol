// heldout_not_tracked_test.go — ⟦H-8 / T-084 / M-37, 2026-09-29⟧ a person
// held out at the destination's gate for lack of its orientation (D-91/D-98:
// no visit, a trip_arrival_not_entered row) WAS aboard at arrival and is on
// NO muster sheet (D-91's accepted risk; d91-arrival-respec.md §3.3 "aboard
// ⇐ VisitsByTrip ∪ trip_arrival_not_entered"; R-11: /orientation is their
// one surface, "a hedged NOT TRACKED on the muster page … STILL STANDS"
// rejected). Before this fix NotTracked built aboard from VisitsByTrip alone,
// so every held-out person was printed at every post as "not aboard at
// arrival — whereabouts not recorded" — false on both counts.
//
// RED on fa904c2 (Diaz listed alongside Evans), GREEN after. The genuine
// not-aboard case (Evans, ticked stayed behind, no row) is asserted in the
// same run so the fix cannot pass by emptying the category.
package domain

import (
	"testing"

	"sitecontrol/internal/store"
)

// notEnteredRowsFor reads trip_arrival_not_entered for one person directly —
// the precondition oracle (BUILDER_BRIEF_RULES rule 6), never the surface
// under test.
func notEnteredRowsFor(t *testing.T, st *store.Store, personID string) []ArrivalNotEntered {
	t.Helper()
	rows, err := dumpSectionWhere[ArrivalNotEntered](st, "trip_arrival_not_entered",
		store.Eq{Column: "person_id", Value: personID})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// personsOnNotTracked flattens NotTracked's rows to raw person ids.
func personsOnNotTracked(t *testing.T, st *store.Store) (rows []NotTrackedRow, ids []string) {
	t.Helper()
	rows, err := NotTracked(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		for _, p := range r.Persons {
			ids = append(ids, p.PersonID)
		}
	}
	return rows, ids
}

func TestNotTrackedNeverListsAPersonHeldOutAtTheGateForOrientation(t *testing.T) {
	w := newD98World(t)
	w.syncBFromA() // the roster must reach B before B can complete anyone's orientation
	diaz, evans := w.ids(3)[0], w.ids(4)[0]
	// Driver, Adams, Baker, Clark hold B's orientation; Diaz and Evans do
	// not. Evans is ticked as STAYED BEHIND (the genuine not-aboard case —
	// never reaches the orientation split at all); Diaz arrives, is held
	// out at B's gate, and gets a not_entered row.
	for _, id := range append(w.ids(0, 1, 2), w.driver.PersonID) {
		if _, err := CompleteOrientation(w.b, "site-b", 1, id, nil, 0, "default"); err != nil {
			t.Fatalf("CompleteOrientation(%s): %v", id, err)
		}
	}
	trip := w.dispatchBus7(t)
	arrival, err := RecordExpectedArrivalExcluding(w.b, "site-b", 1, trip.TripID,
		[]string{evans}, nil, 0, "default", true)
	if err != nil {
		t.Fatalf("RecordExpectedArrivalExcluding: %v", err)
	}

	// --- preconditions: the scenario must actually be posed (rule 6) ---
	if len(arrival.NotEntered) != 1 || arrival.NotEntered[0] != diaz {
		t.Fatalf("precondition: arrival.NotEntered = %v, want exactly Diaz %s", arrival.NotEntered, diaz)
	}
	if rows := notEnteredRowsFor(t, w.b, diaz); len(rows) != 1 || rows[0].ArrivalID != arrival.ArrivalID {
		t.Fatalf("precondition: Diaz's trip_arrival_not_entered rows = %+v, want one for arrival %s", rows, arrival.ArrivalID)
	}
	if rows := notEnteredRowsFor(t, w.b, evans); len(rows) != 0 {
		t.Fatalf("precondition: Evans (stayed behind) must have NO not_entered row, got %+v", rows)
	}
	visits, err := VisitsByTrip(w.b, trip.TripID)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range visits {
		if v.PersonID == diaz || v.PersonID == evans {
			t.Fatalf("precondition: neither Diaz nor Evans may hold a visit on the trip, got %+v", v)
		}
	}
	if len(visits) != 4 {
		t.Fatalf("precondition: visits on BUS 7 = %d, want 4 (driver, Adams, Baker, Clark)", len(visits))
	}

	// --- the case: at the destination -------------------------------------
	rows, ids := personsOnNotTracked(t, w.b)
	if len(rows) != 1 || len(ids) != 1 || ids[0] != evans {
		t.Fatalf("NotTracked at site-b = %+v (persons %v), want exactly one row naming Evans alone — Diaz was aboard (held out at this gate, on /orientation) and is on no muster sheet", rows, ids)
	}
	if rows[0].Vehicle != "BUS 7" || rows[0].Destination != "site-b" {
		t.Fatalf("the genuine not-aboard row changed shape: %+v", rows[0])
	}

	// --- identical at the origin after replay -------------------------------
	evs, err := w.b.Events("site-b", 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if _, err := w.a.Apply(ev); err != nil {
			t.Fatal(err)
		}
	}
	if _, aids := personsOnNotTracked(t, w.a); len(aids) != 1 || aids[0] != evans {
		t.Fatalf("NotTracked at site-a after replay = %v, want exactly Evans — the origin's sheet must agree with the destination's", aids)
	}

	// --- identical on a rebuild from both logs into a fresh store ------------
	fresh := newDomainTestStore(t)
	for _, site := range []string{"site-a", "site-b"} {
		evs, err := w.a.Events(site, 1, 1, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, ev := range evs {
			if _, err := fresh.Apply(ev); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, fids := personsOnNotTracked(t, fresh); len(fids) != 1 || fids[0] != evans {
		t.Fatalf("NotTracked on a fresh rebuild = %v, want exactly Evans", fids)
	}

	// --- and it does not flip when Diaz's orientation is completed -----------
	// (entry-on-completion mints the visit off the held-out arrival, R-16;
	// Diaz was aboard before and is aboard after — never "not tracked" at
	// any instant in between).
	if _, err := CompleteOrientation(w.b, "site-b", 1, diaz, nil, 0, "default"); err != nil {
		t.Fatalf("CompleteOrientation(Diaz): %v", err)
	}
	if _, bids := personsOnNotTracked(t, w.b); len(bids) != 1 || bids[0] != evans {
		t.Fatalf("NotTracked after Diaz's completion = %v, want still exactly Evans", bids)
	}
}
