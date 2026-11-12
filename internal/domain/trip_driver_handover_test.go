// trip_driver_handover_test.go — James, 2026-10-03 (fork FIX-PLAN NEW-b):
// "correcting an open trip's driver must never make the original driver
// vanish from every muster."
//
// The defect these pin (CAMPAIGN-NOTES, fork/truck §3.4 note): a bare
// `driver: B` on an open trip composed to {B, passengers}; the previous
// driver A was then on NO sheet at either post — not on the origin's
// in-transit block, not counted in the destination's expectation, and with
// no visit anywhere because dispatch had closed it. Every test here reads
// the two readers the musters are built from, InTransitFrom (origin) and
// OpenExpectations (destination), and counts A on each: the invariant is
// "exactly once", never "present".
package domain

import (
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

type handoverWorld struct {
	st                  *store.Store
	prev, pax, next     Person
	trip                Trip
	origin, dest        string
	watermarkAtDispatch int64
}

// newHandoverWorld dispatches TRK-72 from site-a to site-b with A driving and
// P aboard, and B — the person who actually drove — on the roster with no
// visit and no trip. Every arrangement the tests lean on is asserted here
// (rule 6), not stated.
func newHandoverWorld(t *testing.T) *handoverWorld {
	t.Helper()
	st := newDomainTestStore(t)
	person := func(name string) Person {
		p, err := CreatePerson(st, "site-a", 1, name, nil, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	w := &handoverWorld{st: st, origin: "site-a", dest: "site-b"}
	w.prev, w.pax, w.next = person("Torstad, Owen"), person("Ravton, Val"), person("Dunby, Logan")
	num := "TRK-72"
	truck, err := CreateVehicle(st, "site-a", 1, &num, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	dest, allowance := w.dest, int64(65)
	w.trip, err = Dispatch(st, "site-a", 1, DispatchInput{
		VehicleID: truck.VehicleID, DriverPersonID: w.prev.PersonID,
		OccupantPersonIDs: []string{w.pax.PersonID},
		DestinationSite:   &dest, DeadlineMinutes: &allowance,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if w.watermarkAtDispatch, err = st.Watermark("site-a", 1); err != nil {
		t.Fatal(err)
	}

	// PRECONDITIONS: the scenario poses its case.
	if s, _ := TripStateByID(st, w.trip.TripID); s == nil || s.State != TripStateOpen {
		t.Fatalf("PRECONDITION: trip %s is %+v, want open", w.trip.TripID, s)
	}
	if got := handoverOccupants(t, st, w.trip.TripID); !w9aEqualStrings(got, w9aSortedCopy([]string{w.prev.PersonID, w.pax.PersonID})) {
		t.Fatalf("PRECONDITION: dispatched occupant set %v, want {A, P}", got)
	}
	if o, d := w.listed(t, w.prev.PersonID); o != 1 || d != 1 {
		t.Fatalf("PRECONDITION: before any correction A is listed %d× on the origin's road list and %d× in the destination's expectation, want 1 and 1", o, d)
	}
	if n := handoverOpenVisits(t, st, w.next.PersonID); n != 0 {
		t.Fatalf("PRECONDITION: B has %d open visit(s); the handover must be tested against a B who is nowhere", n)
	}
	if n := handoverOpenVisits(t, st, w.prev.PersonID); n != 0 {
		t.Fatalf("PRECONDITION: A has %d open visit(s) after dispatch; dispatch is supposed to close them", n)
	}
	return w
}

// listed counts how many times personID appears on the ORIGIN's in-transit
// block (every row, every person) and in the DESTINATION's open
// expectations — the two readers the two musters are built from.
func (w *handoverWorld) listed(t *testing.T, personID string) (origin, dest int) {
	return handoverListedAt(t, w.st, w.origin, w.dest, personID)
}

func handoverListedAt(t *testing.T, st *store.Store, originSite, destSite, personID string) (origin, dest int) {
	t.Helper()
	rows, err := InTransitFrom(st, originSite)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		for _, p := range r.Persons {
			if p.PersonID == personID {
				origin++
			}
		}
	}
	exps, err := OpenExpectations(st, destSite)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range exps {
		for _, p := range e.OccupantList {
			if p.PersonID == personID {
				dest++
			}
		}
	}
	return origin, dest
}

func handoverOccupants(t *testing.T, st *store.Store, tripID string) []string {
	t.Helper()
	c, err := EffectiveTrip(st, tripID)
	if err != nil || c == nil {
		t.Fatalf("EffectiveTrip(%s) = %v, %v", tripID, c, err)
	}
	return c.OccupantIDs
}

func handoverOpenVisits(t *testing.T, st *store.Store, personID string) int {
	t.Helper()
	visits, err := dumpSectionWhere[Visit](st, "visit",
		store.Eq{Column: "person_id", Value: personID}, store.Eq{Column: "is_open", Value: 1})
	if err != nil {
		t.Fatal(err)
	}
	return len(visits)
}

// handoverRefused asserts a *TripAmendRefusal carrying every want fragment,
// and that the refusal AUTHORED NOTHING.
func handoverRefused(t *testing.T, st *store.Store, before int64, err error, want ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("the correction was ACCEPTED, want a refusal")
	}
	var ref *TripAmendRefusal
	if r, ok := err.(*TripAmendRefusal); ok {
		ref = r
	} else {
		t.Fatalf("refusal is %T, want *TripAmendRefusal (authored copy the officer may read): %v", err, err)
	}
	for _, w := range want {
		if !strings.Contains(ref.Reason, w) {
			t.Errorf("refusal %q does not contain %q", ref.Reason, w)
		}
	}
	if after, _ := st.Watermark("site-a", 1); after != before {
		t.Fatalf("refused but authored %d event(s) — a refusal must record nothing", after-before)
	}
}

// TestDriverCorrectionOnAnOpenTripKeepsThePreviousDriverAboardAsAPassenger
// is NEW-b's main case, at the origin AND replayed at the destination.
//
// FAIL BEFORE: EffectiveTrip composed {B, P}; A was listed 0× at both posts.
func TestDriverCorrectionOnAnOpenTripKeepsThePreviousDriverAboardAsAPassenger(t *testing.T) {
	w := newHandoverWorld(t)
	st := w.st

	if _, err := AmendTrip(st, "site-a", 1, TripAmendment{TripID: w.trip.TripID, DriverPersonID: &w.next.PersonID}, 0, "default"); err != nil {
		t.Fatalf("a plain driver correction on an open trip was refused: %v", err)
	}
	// ONE event, not two: the kept-aboard add rides the same amendment.
	if after, _ := st.Watermark("site-a", 1); after != w.watermarkAtDispatch+1 {
		t.Fatalf("the correction authored %d event(s), want exactly 1 — the handover must be one amendment, one transaction", after-w.watermarkAtDispatch)
	}
	rows, err := readTripAmendmentsFor(st, w.trip.TripID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("trip_amendment rows = %d (%v), want 1", len(rows), err)
	}
	if !containsString(rows[0].OccupantsAdd, w.prev.PersonID) {
		t.Fatalf("the authored amendment's occupants_add = %v does not carry the previous driver %s — the fold at the other post would compose without them", rows[0].OccupantsAdd, w.prev.PersonID)
	}

	c, err := EffectiveTrip(st, w.trip.TripID)
	if err != nil || c == nil {
		t.Fatal(err)
	}
	if c.Trip.DriverPersonID != w.next.PersonID {
		t.Fatalf("effective driver = %q, want B %q", c.Trip.DriverPersonID, w.next.PersonID)
	}
	want := w9aSortedCopy([]string{w.prev.PersonID, w.pax.PersonID, w.next.PersonID})
	if !w9aEqualStrings(c.OccupantIDs, want) {
		t.Fatalf("composed occupants = %v, want {A, P, B} = %v — the previous driver must stay aboard as a passenger", c.OccupantIDs, want)
	}
	for _, p := range []struct {
		who string
		id  string
	}{{"A (previous driver)", w.prev.PersonID}, {"B (corrected driver)", w.next.PersonID}, {"P (passenger)", w.pax.PersonID}} {
		if o, d := w.listed(t, p.id); o != 1 || d != 1 {
			t.Errorf("%s is listed %d× on the origin's road list and %d× in the destination's expectation, want exactly 1 and 1", p.who, o, d)
		}
	}

	// REPLAY at a separate destination store: the same single event lands
	// and composes to the same set, so the two posts cannot disagree.
	dest := newDomainTestStore(t)
	truckShip(t, st, dest)
	if got := handoverOccupants(t, dest, w.trip.TripID); !w9aEqualStrings(got, want) {
		t.Fatalf("at the DESTINATION the composed occupants are %v, want %v — the posts have diverged", got, want)
	}
	if o, d := handoverListedAt(t, dest, w.origin, w.dest, w.prev.PersonID); o != 1 || d != 1 {
		t.Errorf("at the destination A is listed %d× (origin reader) and %d× (destination reader), want 1 and 1", o, d)
	}
}

// TestDriverCorrectionToTheRecordedDriverAddsNobody: re-asserting the same
// driver is not a handover; nothing is folded into occupants_add.
func TestDriverCorrectionToTheRecordedDriverAddsNobody(t *testing.T) {
	w := newHandoverWorld(t)
	if _, err := AmendTrip(w.st, "site-a", 1, TripAmendment{TripID: w.trip.TripID, DriverPersonID: &w.prev.PersonID}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	rows, _ := readTripAmendmentsFor(w.st, w.trip.TripID)
	if len(rows) != 1 || len(rows[0].OccupantsAdd) != 0 {
		t.Fatalf("occupants_add = %v, want none when the driver is unchanged", rows[0].OccupantsAdd)
	}
}

// TestDriverCorrectionIsRefusedWhenThePreviousDriverIsOnSite: A checked back
// in at the origin (the crew turned back without them). Keeping A aboard
// would list them twice; the refusal names the post and the way out, and
// the way out — remove A in the same correction — then lists A exactly once.
func TestDriverCorrectionIsRefusedWhenThePreviousDriverIsOnSite(t *testing.T) {
	w := newHandoverWorld(t)
	st := w.st
	walk, err := DispatchLocalCheckIn(st, "site-a", 1, DispatchInput{
		VehicleID: w7SeedVehicleID(t, st, "site-a", "gate walk-in"), DriverPersonID: w.prev.PersonID,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordExpectedArrival(st, "site-a", 1, walk.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if n := handoverOpenVisits(t, st, w.prev.PersonID); n != 1 {
		t.Fatalf("PRECONDITION: A has %d open visit(s), want 1 at the origin", n)
	}
	before, _ := st.Watermark("site-a", 1)

	_, err = AmendTrip(st, "site-a", 1, TripAmendment{
		TripID: w.trip.TripID, DriverPersonID: &w.next.PersonID,
		SiteName: func(id string) string { return map[string]string{"site-a": "Ice Road"}[id] },
	}, 0, "default")
	handoverRefused(t, st, before, err, "Torstad, Owen", "recorded on site at Ice Road", `"Remove from the trip's list"`)

	// The way out the sentence names: A is sighted, so the removal is
	// record-only and A is on exactly one sheet — the origin's, by visit.
	if _, err := AmendTrip(st, "site-a", 1, TripAmendment{
		TripID: w.trip.TripID, DriverPersonID: &w.next.PersonID, OccupantsRemove: []string{w.prev.PersonID},
	}, 0, "default"); err != nil {
		t.Fatalf("driver + explicit removal of the sighted previous driver was refused: %v", err)
	}
	if got := handoverOccupants(t, st, w.trip.TripID); !w9aEqualStrings(got, w9aSortedCopy([]string{w.pax.PersonID, w.next.PersonID})) {
		t.Fatalf("composed occupants = %v, want {P, B}", got)
	}
	if o, d := w.listed(t, w.prev.PersonID); o != 0 || d != 0 {
		t.Errorf("A is still listed %d× on the road and %d× in the expectation while on site; want 0 and 0 (listed once, by visit)", o, d)
	}
}

// TestDriverCorrectionIsRefusedWhenThePreviousDriverIsAboardAnotherOpenTrip.
func TestDriverCorrectionIsRefusedWhenThePreviousDriverIsAboardAnotherOpenTrip(t *testing.T) {
	w := newHandoverWorld(t)
	st := w.st
	num := "TRK-99"
	other, err := CreateVehicle(st, "site-a", 1, &num, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	dest, allowance := w.dest, int64(65)
	second, err := Dispatch(st, "site-a", 1, DispatchInput{
		VehicleID: other.VehicleID, DriverPersonID: w.prev.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &allowance,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := TripStateByID(st, second.TripID); s == nil || s.State != TripStateOpen {
		t.Fatalf("PRECONDITION: the second trip is %+v, want open", s)
	}
	before, _ := st.Watermark("site-a", 1)

	_, err = AmendTrip(st, "site-a", 1, TripAmendment{TripID: w.trip.TripID, DriverPersonID: &w.next.PersonID}, 0, "default")
	handoverRefused(t, st, before, err, "Torstad, Owen", "also recorded aboard TRK-99", second.TripID)
}

// TestDriverCorrectionOnAnArrivedTripMustSayWhatHappenedToThePreviousDriver:
// on a closed trip nothing "kept aboard" can be derived, so a bare driver
// correction is refused in words; with A's fate stated it is recorded.
func TestDriverCorrectionOnAnArrivedTripMustSayWhatHappenedToThePreviousDriver(t *testing.T) {
	w := newHandoverWorld(t)
	st := w.st
	if _, err := RecordExpectedArrival(st, "site-b", 1, w.trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if s, _ := TripStateByID(st, w.trip.TripID); s == nil || s.State != TripStateArrived {
		t.Fatalf("PRECONDITION: trip is %+v, want arrived", s)
	}
	before, _ := st.Watermark("site-a", 1)

	_, err := AmendTrip(st, "site-a", 1, TripAmendment{TripID: w.trip.TripID, DriverPersonID: &w.next.PersonID}, 0, "default")
	handoverRefused(t, st, before, err, "Torstad, Owen", "already arrived", `"Aboard after all"`, `"Remove from the trip's list"`)

	if _, err := AmendTrip(st, "site-a", 1, TripAmendment{
		TripID: w.trip.TripID, DriverPersonID: &w.next.PersonID, OccupantsAdd: []string{w.prev.PersonID},
	}, 0, "default"); err != nil {
		t.Fatalf("driver + 'aboard after all' on an arrived trip was refused: %v", err)
	}
	if got := handoverOccupants(t, st, w.trip.TripID); !w9aEqualStrings(got, w9aSortedCopy([]string{w.prev.PersonID, w.pax.PersonID, w.next.PersonID})) {
		t.Fatalf("composed occupants = %v, want {A, P, B}", got)
	}

	// Rung 8 asks about the EFFECTIVE driver: A, superseded as driver, can
	// now be removed on their own; B, the effective driver, cannot. ⟦was:
	// the raw row's driver, so A stayed "the recorded driver" for ever⟧
	if _, err := AmendTrip(st, "site-a", 1, TripAmendment{TripID: w.trip.TripID, OccupantsRemove: []string{w.prev.PersonID}}, 0, "default"); err != nil {
		t.Fatalf("removing the SUPERSEDED driver on their own was refused: %v", err)
	}
	before, _ = st.Watermark("site-a", 1)
	_, err = AmendTrip(st, "site-a", 1, TripAmendment{TripID: w.trip.TripID, OccupantsRemove: []string{w.next.PersonID}}, 0, "default")
	handoverRefused(t, st, before, err, "Dunby, Logan is the recorded driver of this trip")
}

// TestDriverRemovalRefusalNamesThePerson — L09 T-146: every other sentence
// on the surface names people; this one quoted a raw id.
func TestDriverRemovalRefusalNamesThePerson(t *testing.T) {
	w := newHandoverWorld(t)
	before, _ := w.st.Watermark("site-a", 1)
	_, err := AmendTrip(w.st, "site-a", 1, TripAmendment{TripID: w.trip.TripID, OccupantsRemove: []string{w.prev.PersonID}}, 0, "default")
	handoverRefused(t, w.st, before, err, "Torstad, Owen is the recorded driver of this trip")
	if strings.Contains(err.Error(), `"`+w.prev.PersonID+`"`) {
		t.Fatalf("the refusal still quotes the raw id: %v", err)
	}
}

// TestUnsettingAnUnknownWordIsRefusedAsCannotBeUnset — L09 T-159: an officer
// who types "driver" into "Un-set a field" was told they asserted nothing.
// The whitelist sentence is the honest one; the genuine no-op keeps its own.
func TestUnsettingAnUnknownWordIsRefusedAsCannotBeUnset(t *testing.T) {
	w := newHandoverWorld(t)
	before, _ := w.st.Watermark("site-a", 1)
	_, err := AmendTrip(w.st, "site-a", 1, TripAmendment{TripID: w.trip.TripID, Clear: []string{"driver"}}, 0, "default")
	handoverRefused(t, w.st, before, err, `"driver" cannot be un-set`, "Only cargo_note can be un-set") // ⟦was: "Only cargo_note and phase_override_reason can be un-set" — weather phases removed, James 2026-10-04⟧
	if strings.Contains(err.Error(), "asserts nothing") {
		t.Fatalf("the unknown Clear token still lands on the no-op refusal: %v", err)
	}
	_, err = AmendTrip(w.st, "site-a", 1, TripAmendment{TripID: w.trip.TripID}, 0, "default")
	handoverRefused(t, w.st, before, err, "nothing was corrected")
}
