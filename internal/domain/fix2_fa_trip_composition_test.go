// fix2_fa_trip_composition_test.go — FINDINGS-R4 cluster A (raw versus
// effective trip composition): D1 (CRITICAL), D10, D17 and ruling R2.
//
// D1: an arrival — the destination's "Arrived" and the origin's "Arrived
// back here" alike — read the trip's RAW dispatch occupant rows, so a driver
// named by correction, or anyone added by "Aboard after all", got no visit:
// on no muster as present, on every muster as "not aboard at arrival". The
// arrival form listed them (it composes), the arrival did not.
//
// Every test here is two posts, two stores: the origin authors, the events
// replicate, and BOTH posts must read the same visits — the error in D1
// replicated identically, so agreement alone proves nothing; each test also
// counts the person on the destination's own on-site set.
package domain

import (
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// fix2Ship applies every event of siteID's stream that dst has not yet
// seen, in order — the contiguous delivery replication performs.
func fix2Ship(t *testing.T, src, dst *store.Store, siteID string) {
	t.Helper()
	wm, err := dst.Watermark(siteID, 1)
	if err != nil {
		t.Fatal(err)
	}
	envs, err := src.Events(siteID, 1, wm+1, 10000)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range envs {
		if _, err := dst.Apply(e); err != nil {
			t.Fatalf("applying %v: %v", e.Key(), err)
		}
	}
}

// fix2OpenAt lists the person ids with an open visit at siteID, sorted.
func fix2OpenAt(t *testing.T, st *store.Store, siteID string) []string {
	t.Helper()
	visits, err := dumpSectionWhere[Visit](st, "visit",
		store.Eq{Column: "site_id", Value: siteID}, store.Eq{Column: "is_open", Value: 1})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, v := range visits {
		out = append(out, v.PersonID)
	}
	sort.Strings(out)
	return out
}

type fix2World struct {
	origin, dest   *store.Store // site-a's store and site-b's store
	a, p, b, c     Person       // A drives, P rides, B and C are on the roster, nowhere
	trip           Trip
	truck, spareID string
}

// newFix2World: site-a dispatches TRK-28 to site-b with A driving and P
// aboard; B and C are on the roster with no visit and no trip. The
// destination store holds everything the origin authored.
func newFix2World(t *testing.T) *fix2World {
	t.Helper()
	w := &fix2World{origin: newDomainTestStore(t), dest: newDomainTestStore(t)}
	person := func(name string) Person {
		p, err := CreatePerson(w.origin, "site-a", 1, name, nil, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	w.a, w.p, w.b, w.c = person("Torstad, Owen"), person("Ravton, Val"), person("Dunby, Logan"), person("Quillan, Ada")
	for _, n := range []string{"TRK-28", "TRK-29"} {
		num := n
		v, err := CreateVehicle(w.origin, "site-a", 1, &num, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		if w.truck == "" {
			w.truck = v.VehicleID
		} else {
			w.spareID = v.VehicleID
		}
	}
	dest, allowance := "site-b", int64(65)
	var err error
	w.trip, err = Dispatch(w.origin, "site-a", 1, DispatchInput{
		VehicleID: w.truck, DriverPersonID: w.a.PersonID,
		OccupantPersonIDs: []string{w.p.PersonID},
		DestinationSite:   &dest, DeadlineMinutes: &allowance,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w.sync(t)
	for _, id := range []string{w.a.PersonID, w.p.PersonID, w.b.PersonID, w.c.PersonID} {
		if n := handoverOpenVisits(t, w.origin, id); n != 0 {
			t.Fatalf("PRECONDITION: %s has %d open visit(s)", id, n)
		}
	}
	return w
}

// sync ships both posts' streams both ways.
func (w *fix2World) sync(t *testing.T) {
	t.Helper()
	fix2Ship(t, w.origin, w.dest, "site-a")
	fix2Ship(t, w.dest, w.origin, "site-b")
}

func (w *fix2World) amend(t *testing.T, in TripAmendment) {
	t.Helper()
	in.TripID = w.trip.TripID
	if _, err := AmendTrip(w.origin, "site-a", 1, in, 0, "default"); err != nil {
		t.Fatalf("correction %+v was refused: %v", in, err)
	}
	w.sync(t)
}

// agree asserts both posts read the same open visits at siteID, and that
// they are want.
func (w *fix2World) agree(t *testing.T, siteID string, want []string) {
	t.Helper()
	want = w9aSortedCopy(want)
	for name, st := range map[string]*store.Store{"origin": w.origin, "destination": w.dest} {
		if got := fix2OpenAt(t, st, siteID); !w9aEqualStrings(got, want) {
			t.Errorf("at the %s post, open visits at %s = %v, want %v", name, siteID, got, want)
		}
	}
}

// TestD1ArrivalChecksInTheCorrectedDriver is the FINDINGS-R4 D1 repro (L17-28).
// FAIL BEFORE: B had no visit at site-b; only A and P were checked in.
func TestD1ArrivalChecksInTheCorrectedDriver(t *testing.T) {
	w := newFix2World(t)
	w.amend(t, TripAmendment{DriverPersonID: &w.b.PersonID})
	if got := handoverOccupants(t, w.dest, w.trip.TripID); !w9aEqualStrings(got, w9aSortedCopy([]string{w.a.PersonID, w.p.PersonID, w.b.PersonID})) {
		t.Fatalf("PRECONDITION: composed occupants at the destination = %v, want {A, P, B}", got)
	}
	if _, err := RecordExpectedArrivalExcluding(w.dest, "site-b", 1, w.trip.TripID, nil, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	w.sync(t)
	w.agree(t, "site-b", []string{w.a.PersonID, w.p.PersonID, w.b.PersonID})
}

// TestD1ArrivalChecksInAnyoneAddedAboardAfterAll — the occupants_add arm,
// and the stayed-behind tick may name the added person.
// FAIL BEFORE: C got no visit; ticking C "stayed" was refused as "not an
// occupant" although the arrival form listed them.
func TestD1ArrivalChecksInAnyoneAddedAboardAfterAll(t *testing.T) {
	w := newFix2World(t)
	w.amend(t, TripAmendment{OccupantsAdd: []string{w.c.PersonID, w.b.PersonID}})
	if _, err := RecordExpectedArrivalExcluding(w.dest, "site-b", 1, w.trip.TripID, []string{w.b.PersonID}, nil, 0, "default"); err != nil {
		t.Fatalf("an arrival ticking an added occupant as stayed was refused: %v", err)
	}
	w.sync(t)
	w.agree(t, "site-b", []string{w.a.PersonID, w.p.PersonID, w.c.PersonID})
}

// TestD1ArrivalDoesNotCheckInSomeoneRemovedFromTheList — the composition
// works in both directions: a person corrected OFF the list was not aboard.
func TestD1ArrivalDoesNotCheckInSomeoneRemovedFromTheList(t *testing.T) {
	w := newFix2World(t)
	w.amend(t, TripAmendment{OccupantsRemove: []string{w.p.PersonID}})
	if _, err := RecordExpectedArrivalExcluding(w.dest, "site-b", 1, w.trip.TripID, nil, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	w.sync(t)
	w.agree(t, "site-b", []string{w.a.PersonID})
}

// TestD1ArrivedBackHereChecksInTheCorrectedDriver — L17-29: the same root
// cause through the origin's "Arrived back here".
func TestD1ArrivedBackHereChecksInTheCorrectedDriver(t *testing.T) {
	w := newFix2World(t)
	w.amend(t, TripAmendment{DriverPersonID: &w.b.PersonID, OccupantsAdd: []string{w.c.PersonID}})
	if _, err := RecordReturnToOrigin(w.origin, "site-a", 1, w.trip.TripID, nil, nil, 0, "default", false); err != nil {
		t.Fatal(err)
	}
	w.sync(t)
	w.agree(t, "site-a", []string{w.a.PersonID, w.p.PersonID, w.b.PersonID, w.c.PersonID})
	w.agree(t, "site-b", nil)
}

// TestD17DestinationArrivalAfterReturnSaysWhereItWasRecorded — L17-11:
// after "Arrived back here" at the origin, the destination's Arrived is
// still refused (the trip is closed), but in true words: it was recorded
// back at its origin, nobody was checked in here.
// FAIL BEFORE: "trip … has already arrived here (state arrived)".
func TestD17DestinationArrivalAfterReturnSaysWhereItWasRecorded(t *testing.T) {
	w := newFix2World(t)
	if _, err := RecordReturnToOrigin(w.origin, "site-a", 1, w.trip.TripID, nil, nil, 0, "default", false); err != nil {
		t.Fatal(err)
	}
	w.sync(t)
	before, _ := w.dest.Watermark("site-b", 1)
	_, err := RecordExpectedArrivalExcluding(w.dest, "site-b", 1, w.trip.TripID, nil, nil, 0, "default")
	if err == nil {
		t.Fatal("the destination's arrival of a trip already recorded back at its origin was ACCEPTED")
	}
	msg := err.Error()
	for _, bad := range []string{"already arrived here", "(state ", w.trip.TripID} {
		if strings.Contains(msg, bad) {
			t.Errorf("refusal %q still contains %q", msg, bad)
		}
	}
	for _, want := range []string{"TRK-28", "recorded back at " + SiteLabel("site-a"), "turned back", "nobody was checked in here"} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal %q does not contain %q", msg, want)
		}
	}
	if after, _ := w.dest.Watermark("site-b", 1); after != before {
		t.Fatalf("refused but authored %d event(s)", after-before)
	}
	// The ordinary second press at the SAME post keeps its own sentence.
	_, err = RecordReturnToOrigin(w.origin, "site-a", 1, w.trip.TripID, nil, nil, 0, "default", false)
	if err == nil || !strings.Contains(err.Error(), "has already been recorded as arrived here") {
		t.Fatalf("second return at the origin: %v", err)
	}
}

// --- R2 / D10 -----------------------------------------------------------

func fix2Refused(t *testing.T, st *store.Store, siteID string, before int64, err error, want ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("the correction was ACCEPTED, want a refusal")
	}
	ref, ok := err.(*TripAmendRefusal)
	if !ok {
		t.Fatalf("refusal is %T, want *TripAmendRefusal: %v", err, err)
	}
	for _, w := range want {
		if !strings.Contains(ref.Reason, w) {
			t.Errorf("refusal %q does not contain %q", ref.Reason, w)
		}
	}
	if after, _ := st.Watermark(siteID, 1); after != before {
		t.Fatalf("refused but authored %d event(s)", after-before)
	}
}

// fix2CheckInAt gives personID an open visit at siteID (a walk-in).
func fix2CheckInAt(t *testing.T, st *store.Store, siteID, personID string) {
	t.Helper()
	walk, err := DispatchLocalCheckIn(st, siteID, 1, DispatchInput{
		VehicleID: w7SeedVehicleID(t, st, siteID, "gate walk-in"), DriverPersonID: personID,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordExpectedArrival(st, siteID, 1, walk.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
}

// TestR2NewlyNamedPersonOnSiteIsRefused — L16-05a, L09 NEW-b-DOUBLE (N1,
// N2): a new driver, or "Aboard after all", who has an open visit anywhere.
// FAIL BEFORE: both were Recorded; the person was then on a muster AND a
// truck's list (and, after D1, would have had two open visits).
func TestR2NewlyNamedPersonOnSiteIsRefused(t *testing.T) {
	w := newFix2World(t)
	fix2CheckInAt(t, w.origin, "site-a", w.b.PersonID)
	if n := handoverOpenVisits(t, w.origin, w.b.PersonID); n != 1 {
		t.Fatalf("PRECONDITION: B has %d open visits, want 1", n)
	}
	names := func(id string) string { return map[string]string{"site-a": "Ice Road", "site-b": "Ice Pad"}[id] }
	for _, in := range []TripAmendment{
		{TripID: w.trip.TripID, DriverPersonID: &w.b.PersonID, SiteName: names},
		{TripID: w.trip.TripID, OccupantsAdd: []string{w.b.PersonID}, SiteName: names},
	} {
		before, _ := w.origin.Watermark("site-a", 1)
		_, err := AmendTrip(w.origin, "site-a", 1, in, 0, "default")
		fix2Refused(t, w.origin, "site-a", before, err, "Dunby, Logan", "on site at Ice Road", "check them out")
	}
}

// TestR2NewlyNamedPersonOnAnotherOpenTripIsRefused — L17-18, NEW-b-DOUBLE
// (S1): a person already on another open trip's list.
// FAIL BEFORE: Recorded; one person on two trucks, and the Remove that
// would undo it was refused with a false "only place they are accounted for".
func TestR2NewlyNamedPersonOnAnotherOpenTripIsRefused(t *testing.T) {
	w := newFix2World(t)
	dest, allowance := "site-b", int64(65)
	second, err := Dispatch(w.origin, "site-a", 1, DispatchInput{
		VehicleID: w.spareID, DriverPersonID: w.c.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &allowance,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []TripAmendment{
		{TripID: w.trip.TripID, DriverPersonID: &w.c.PersonID},
		{TripID: w.trip.TripID, OccupantsAdd: []string{w.c.PersonID}},
	} {
		before, _ := w.origin.Watermark("site-a", 1)
		_, err := AmendTrip(w.origin, "site-a", 1, in, 0, "default")
		fix2Refused(t, w.origin, "site-a", before, err, "Quillan, Ada", "aboard TRK-29", second.TripID)
	}
	// Someone who is nowhere is still accepted, at the destination too.
	if _, err := AmendTrip(w.dest, "site-b", 1, TripAmendment{TripID: w.trip.TripID, DriverPersonID: &w.b.PersonID}, 0, "default"); err != nil {
		t.Fatalf("the destination's driver correction to a person who is nowhere was refused: %v", err)
	}
}

// TestR2OnlyTheOriginOrTheDestinationMayChangeTheDriver — L13 D-96 open
// question. FAIL BEFORE: a third post's driver correction was Recorded.
func TestR2OnlyTheOriginOrTheDestinationMayChangeTheDriver(t *testing.T) {
	w := newFix2World(t)
	third := newDomainTestStore(t)
	fix2Ship(t, w.origin, third, "site-a")
	before, _ := third.Watermark("site-c", 1)
	_, err := AmendTrip(third, "site-c", 1, TripAmendment{TripID: w.trip.TripID, DriverPersonID: &w.b.PersonID}, 0, "default")
	fix2Refused(t, third, "site-c", before, err, "only "+SiteLabel("site-a")+" or "+SiteLabel("site-b"))
	// Facts that are not the driver stay correctable from anywhere (D-96).
	note := "spare tyre"
	if _, err := AmendTrip(third, "site-c", 1, TripAmendment{TripID: w.trip.TripID, CargoNote: &note}, 0, "default"); err != nil {
		t.Fatalf("a third post's cargo-note correction was refused: %v", err)
	}
}

// --- D1 ruling (James, 2026-10-04): nobody on no muster ------------------
//
// People in raw∖effective are listed in NotTracked from the moment the
// correction is recorded and are never entered at the destination; they
// leave NotTracked when checked in, or when a re-correction puts them back
// aboard. Invariant: every person in raw ∪ effective is in EXACTLY ONE of a
// destination visit, an origin visit, in-transit, or NotTracked — at BOTH
// posts. (In the fork, rung 9 keeps a replaced driver aboard as a passenger
// unless the officer removes them in the same correction, so the "replaced
// driver" case of the ruling is the driver change WITH that removal.)

func fix2Where(t *testing.T, st *store.Store, personID string) string {
	t.Helper()
	var at []string
	for _, site := range []string{"site-b", "site-a"} {
		open, err := OpenVisitsAt(st, site)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range open {
			if v.PersonID == personID {
				at = append(at, map[string]string{"site-a": "origin visit", "site-b": "destination visit"}[site])
			}
		}
	}
	rows, err := InTransitFrom(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		for _, o := range r.Persons {
			if o.PersonID == personID {
				at = append(at, "in-transit")
			}
		}
	}
	nt, err := NotTracked(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range nt {
		for _, o := range r.Persons {
			if o.PersonID == personID {
				at = append(at, "NotTracked")
			}
		}
	}
	if len(at) == 0 {
		return "NOWHERE"
	}
	return strings.Join(at, " + ")
}

// invariant checks the exactly-one rule for raw ∪ effective at both posts,
// and that each named person is where want says.
func (w *fix2World) invariant(t *testing.T, point string, want map[string]string) {
	t.Helper()
	for name, st := range map[string]*store.Store{"origin": w.origin, "destination": w.dest} {
		union := map[string]bool{}
		raw, err := TripOccupantsByTrip(st, w.trip.TripID)
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range raw {
			union[o.PersonID] = true
		}
		for _, id := range handoverOccupants(t, st, w.trip.TripID) {
			union[id] = true
		}
		for id := range want {
			if !union[id] {
				t.Fatalf("[%s @%s] PRECONDITION: %s is not in raw ∪ effective", point, name, id)
			}
		}
		for id := range union {
			got := fix2Where(t, st, id)
			if strings.Contains(got, " + ") || got == "NOWHERE" {
				t.Errorf("[%s @%s] %s is in %s — must be in exactly one place", point, name, personNameOrID(st, id), got)
			}
			if wnt, ok := want[id]; ok && got != wnt {
				t.Errorf("[%s @%s] %s is in %s, want %s", point, name, personNameOrID(st, id), got, wnt)
			}
		}
	}
}

func (w *fix2World) arrive(t *testing.T, stayed ...string) {
	t.Helper()
	if _, err := RecordExpectedArrivalExcluding(w.dest, "site-b", 1, w.trip.TripID, stayed, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	w.sync(t)
}

// TestD1RulingDriverChangeKeptAboardUnticked: A→B, A kept aboard (rung 9),
// nobody ticked stayed — both entered at the destination.
func TestD1RulingDriverChangeKeptAboardUnticked(t *testing.T) {
	w := newFix2World(t)
	w.amend(t, TripAmendment{DriverPersonID: &w.b.PersonID})
	w.invariant(t, "after correction", map[string]string{w.a.PersonID: "in-transit", w.b.PersonID: "in-transit", w.p.PersonID: "in-transit"})
	w.arrive(t)
	w.invariant(t, "after arrival", map[string]string{w.a.PersonID: "destination visit", w.b.PersonID: "destination visit", w.p.PersonID: "destination visit"})
}

// TestD1RulingDriverChangeKeptAboardTickedStayed: A ticked "stayed" at the
// arrival — NotTracked, as any stayed-behind occupant.
func TestD1RulingDriverChangeKeptAboardTickedStayed(t *testing.T) {
	w := newFix2World(t)
	w.amend(t, TripAmendment{DriverPersonID: &w.b.PersonID})
	w.arrive(t, w.a.PersonID)
	w.invariant(t, "after arrival", map[string]string{w.a.PersonID: "NotTracked", w.b.PersonID: "destination visit", w.p.PersonID: "destination visit"})
}

// TestD1RulingReplacedDriverTakenOffIsNotTrackedFromCorrection: A→B with A
// removed in the same correction. FAIL BEFORE: A was NOWHERE — not in
// transit, never entered, listed by no NotTracked row.
func TestD1RulingReplacedDriverTakenOffIsNotTrackedFromCorrection(t *testing.T) {
	w := newFix2World(t)
	w.amend(t, TripAmendment{DriverPersonID: &w.b.PersonID, OccupantsRemove: []string{w.a.PersonID}})
	w.invariant(t, "after correction, in transit", map[string]string{w.a.PersonID: "NotTracked", w.b.PersonID: "in-transit"})
	// A re-correction that puts A back aboard returns them to in-transit.
	w.amend(t, TripAmendment{OccupantsAdd: []string{w.a.PersonID}})
	w.invariant(t, "after re-correction", map[string]string{w.a.PersonID: "in-transit", w.b.PersonID: "in-transit"})
	w.amend(t, TripAmendment{OccupantsRemove: []string{w.a.PersonID}})
	w.arrive(t)
	w.invariant(t, "after arrival", map[string]string{w.a.PersonID: "NotTracked", w.b.PersonID: "destination visit", w.p.PersonID: "destination visit"})
}

// TestD1RulingOccupantsRemoveIsNotTrackedUntilCheckedIn: P removed — never
// entered at the destination, NotTracked from the correction until P is
// checked in at a gate. FAIL BEFORE: P was NOWHERE.
func TestD1RulingOccupantsRemoveIsNotTrackedUntilCheckedIn(t *testing.T) {
	w := newFix2World(t)
	w.amend(t, TripAmendment{OccupantsRemove: []string{w.p.PersonID}})
	w.invariant(t, "after correction, in transit", map[string]string{w.p.PersonID: "NotTracked", w.a.PersonID: "in-transit"})
	w.arrive(t)
	w.invariant(t, "after arrival", map[string]string{w.p.PersonID: "NotTracked", w.a.PersonID: "destination visit"})
	fix2CheckInAt(t, w.origin, "site-a", w.p.PersonID)
	w.sync(t)
	w.invariant(t, "after check-in at the origin", map[string]string{w.p.PersonID: "origin visit"})
}

// TestD1RulingTakenOffRowSaysSo: the dropped person's row is flagged so the
// muster can say "taken off this run by a trip correction", not "not aboard
// at arrival" (false of a run that has not arrived).
func TestD1RulingTakenOffRowSaysSo(t *testing.T) {
	w := newFix2World(t)
	w.amend(t, TripAmendment{OccupantsRemove: []string{w.p.PersonID}})
	nt, err := NotTracked(w.dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(nt) != 1 || !nt[0].TakenOffByCorrection || len(nt[0].Persons) != 1 || nt[0].Persons[0].PersonID != w.p.PersonID {
		t.Fatalf("NotTracked = %+v, want one TakenOffByCorrection row naming P", nt)
	}
}
