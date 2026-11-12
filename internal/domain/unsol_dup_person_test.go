// unsol_dup_person_test.go — the unsolicited arrival door and a DUPLICATED
// person id (reports/witness/UNSOL-DUP-PERSON.md).
//
// The defect surfaced incidentally (REVIEW-ANALYTICS.md §"the one shape where
// they differ"): `RecordUnsolicitedArrival` passed its person list VERBATIM to
// buildArrivalVisits, so `[p, p]` minted TWO ArrivalVisit rows for one human
// in ONE trip_arrival. The fold's own "a person cannot be on the same site
// twice" rule then closed the first while inserting the second — one visit
// closed at birth, with no departure event behind it.
//
// This door is the ONLY arrival door whose person list is caller-supplied.
// Both siblings are de-duplicated by construction: RecordExpectedArrival
// derives its list from trip_occupant rows, whose PRIMARY KEY (trip_id,
// person_id) makes it a set, and Dispatch's `[p, p]` collapses at the fold's
// `INSERT OR IGNORE INTO trip_occupant`. Neither refuses. The fix follows
// that precedent — de-duplicate, first occurrence wins, order kept — and it
// sits in the door, BEFORE the event is minted, so the payload never carries
// the shape and the fold is untouched (a fold-side refusal forks the logs,
// W3-2).
package domain

import (
	"testing"
)

// unsolDupRows counts every visit row for personID at siteID — open and
// closed — in a full Visits dump. The muster's own read, OpenVisitsAt, would
// hide the closed-at-birth row this defect makes, so the count is taken on
// the fold's whole table.
func unsolDupRows(t *testing.T, all []Visit, siteID, personID string) (rows, open int) {
	t.Helper()
	for _, v := range all {
		if v.SiteID != siteID || v.PersonID != personID {
			continue
		}
		rows++
		if v.IsOpen == 1 {
			open++
		}
	}
	return rows, open
}

// TestUnsolicitedArrivalDuplicatedPersonIDMintsExactlyOneVisit is the
// reproduction and the guard. RED at cf1d17c: 2 rows, 1 open.
func TestUnsolicitedArrivalDuplicatedPersonIDMintsExactlyOneVisit(t *testing.T) {
	st := newDomainTestStore(t)
	driver, _ := seedTwoPersons(t, st, "site-b")
	veh := w7SeedVehicleID(t, st, "site-b", "TRK 12")

	arr, err := RecordUnsolicitedArrival(st, "site-b", 1, veh, "site-a",
		[]string{driver.PersonID, driver.PersonID}, nil, 0, "default")
	if err != nil {
		t.Fatalf("unsolicited arrival with a duplicated id must still be recordable (the door is not weakened): %v", err)
	}
	all, err := Visits(st)
	if err != nil {
		t.Fatal(err)
	}
	rows, open := unsolDupRows(t, all, "site-b", driver.PersonID)
	t.Logf("MEASURED: arrival %s for [p, p] left %d visit row(s) for p at site-b, %d open", arr.ArrivalID, rows, open)
	if open != 1 {
		t.Fatalf("muster: %d open visit(s) for one person, want exactly 1", open)
	}
	if rows != 1 {
		t.Fatalf("one person on one arrival left %d visit rows (want 1) — the duplicated id minted a second visit, which the fold closed at birth: a departure the record shows that never happened", rows)
	}
}

// TestUnsolicitedArrivalTwoDifferentPeopleBothLandOnTheMuster is the property
// a careless de-duplication breaks. GREEN at cf1d17c and must stay so.
func TestUnsolicitedArrivalTwoDifferentPeopleBothLandOnTheMuster(t *testing.T) {
	st := newDomainTestStore(t)
	driver, passenger := seedTwoPersons(t, st, "site-b")
	veh := w7SeedVehicleID(t, st, "site-b", "TRK 12")

	if _, err := RecordUnsolicitedArrival(st, "site-b", 1, veh, "site-a",
		[]string{driver.PersonID, passenger.PersonID}, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	openVisits, err := OpenVisitsAt(st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(openVisits) != 2 {
		t.Fatalf("two different people arrived; muster holds %d open visit(s), want 2", len(openVisits))
	}
	seen := map[string]bool{}
	for _, v := range openVisits {
		seen[v.PersonID] = true
	}
	if !seen[driver.PersonID] || !seen[passenger.PersonID] {
		t.Fatalf("muster holds %v, want both %s and %s", seen, driver.PersonID, passenger.PersonID)
	}
}

// TestUnsolicitedArrivalDuplicatedIDKeepsOrderAndEveryOtherPerson pins the
// de-duplication's shape: first occurrence wins, order is kept, nobody else
// is dropped — `[a, b, a, c]` enters a, b, c once each.
func TestUnsolicitedArrivalDuplicatedIDKeepsOrderAndEveryOtherPerson(t *testing.T) {
	st := newDomainTestStore(t)
	a, b := seedTwoPersons(t, st, "site-b")
	c, err := CreatePerson(st, "site-b", 1, "Chris Crew", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	veh := w7SeedVehicleID(t, st, "site-b", "TRK 12")

	if _, err := RecordUnsolicitedArrival(st, "site-b", 1, veh, "site-a",
		[]string{a.PersonID, b.PersonID, a.PersonID, c.PersonID}, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	all, err := Visits(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []Person{a, b, c} {
		rows, open := unsolDupRows(t, all, "site-b", p.PersonID)
		if rows != 1 || open != 1 {
			t.Fatalf("%s: %d row(s), %d open — want exactly one open visit per distinct person", p.Name, rows, open)
		}
	}
}

// TestUnsolicitedArrivalDuplicatedUnorientedIDDoesNotRefuseTheWholeArrival is
// the SECOND arm, at an orientation=true post. There the duplicated id lands
// in notEntered twice, and trip_arrival_not_entered's PRIMARY KEY
// (arrival_id, person_id) is a plain INSERT — the fold fails, store.Author
// rolls the whole event back, and the door REFUSES an arrival that also
// carried a perfectly good oriented person. That is the under-count arm:
// the vehicle came, the oriented occupant is standing at the gate, and the
// muster leaves them out until the officer notices an SQL constraint error.
// RED at cf1d17c.
func TestUnsolicitedArrivalDuplicatedUnorientedIDDoesNotRefuseTheWholeArrival(t *testing.T) {
	st := newDomainTestStore(t)
	unoriented, oriented := seedTwoPersons(t, st, "site-b")
	if _, err := CompleteOrientation(st, "site-b", 1, oriented.PersonID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	veh := w7SeedVehicleID(t, st, "site-b", "TRK 12")

	arr, err := RecordUnsolicitedArrivalOriented(st, "site-b", 1, veh, "site-a",
		[]string{unoriented.PersonID, unoriented.PersonID, oriented.PersonID}, nil, 0, "default", true, false)
	if err != nil {
		t.Fatalf("orientation=true: a duplicated un-oriented id refused the WHOLE arrival, oriented occupant included: %v", err)
	}
	if len(arr.NotEntered) != 1 || arr.NotEntered[0] != unoriented.PersonID {
		t.Fatalf("not-entered = %v, want exactly [%s] once", arr.NotEntered, unoriented.PersonID)
	}
	openVisits, err := OpenVisitsAt(st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(openVisits) != 1 || openVisits[0].PersonID != oriented.PersonID {
		t.Fatalf("muster after the arrival = %+v, want exactly the oriented occupant's one open visit", openVisits)
	}
}
