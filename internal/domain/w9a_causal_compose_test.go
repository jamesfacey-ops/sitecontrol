// w9a_causal_compose_test.go — the trip and arrival carriers under [[D-92]]
// at SchemaVersion 11 (step 3: the causal `seen` reference cloned onto
// trip + arrival FOURTH and FIFTH, after company, trade and vehicle). The
// fixtures are F4's twins of w3_causal_compose_test.go's; the rule is
// resolveContest, read once, and the contest scope is unchanged — the
// record's own rows, which F4 already scopes in SQL. The trip carrier's
// OPEN vocabulary is covered too: an `occupant/<person_id>` member fact is
// resolved the same way, and the stamp names that member's heads.
//
// KILLER MUTANTS, each PRODUCED red once against this tree before commit
// (reports/witness/STEP3-CLONE.md): m-D92a (shared), m-D92b-trip /
// m-D92b-arrival (the edge drops the stamp), m-D92c-trip-scalar /
// m-D92c-trip-member / m-D92c-arrival (a composer falls back to last key).
package domain

import (
	"sort"
	"testing"

	"sitecontrol/internal/store"
)

// w9aCausalWorld seeds the roster and dispatches one open trip from site-a
// with two occupants, returning the trip and the site-a stream as the
// primer a peer post is given.
func w9aCausalWorld(t *testing.T, st *store.Store, vehicle string) (Trip, []store.Envelope) {
	t.Helper()
	w9aSeedPersons(t, st, "site-a", 1, "drv-start", "drv-okafor", "drv-vance", "p-brand", "p-chen")
	trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, vehicle, "drv-start", []string{"p-brand", "p-chen"}, 240)
	return trip, w3Events(t, st, "site-a", 1)
}

func w9aAmendTripLocal(t *testing.T, st *store.Store, clockMS int64, in TripAmendment) store.Envelope {
	t.Helper()
	st.Clock = w3Reading(clockMS)
	env, err := AmendTrip(st, "site-a", 1, in, 0, "default")
	if err != nil {
		t.Fatalf("fixture trip amendment refused: %v", err)
	}
	return env
}

func w9aComposed(t *testing.T, st *store.Store, tripID string) *ComposedTrip {
	t.Helper()
	c, err := EffectiveTrip(st, tripID)
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatalf("trip %s: no composed record", tripID)
	}
	return c
}

func w9aTripSeenRowOf(t *testing.T, st *store.Store, k store.Key, tripID string) []store.Key {
	t.Helper()
	rows, err := readTripAmendmentsFor(st, tripID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Key == k {
			return r.Seen
		}
	}
	t.Fatalf("no trip_amendment row at %v for %s", k, tripID)
	return nil
}

func w9aContainsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// w9aOwnSiteRemote is d92Remote's shape for a carrier D-87 gates
// (arrival_amended: ownership.go classifies it ownPresence, and its
// resolver refuses any siteID that is not trip_arrival's own tag —
// unconditionally, so a genuinely different site_id can never author a
// second correction over this record; store.Author would refuse it, and
// correctly). The arrival's "another post" is therefore not another
// site_id — it is the SAME owning site on a SECOND node_epoch: a re-image
// mid-season, or a second desk terminal that has not yet resynced,
// authoring independently before the two streams are replicated together
// (orientation.go's header documents the identical shape for exactly this
// reason: "a site re-imaged mid-season authors on a new node_epoch ... two
// streams a peer may apply in either order"). Key.Less compares
// (SiteID, NodeEpoch, Seq): a tie on SiteID falls to NodeEpoch, so the pair
// is still a genuine, cross-post tiebreak contest — just on epoch instead
// of site_id.
func w9aOwnSiteRemote(t *testing.T, primer []store.Envelope, siteID string, epoch, clockMS int64, author func(rs *store.Store)) store.Envelope {
	t.Helper()
	evs := w3AuthorRemote(t, primer, siteID, epoch, clockMS, author)
	if len(evs) != 1 {
		t.Fatalf("fixture defect: %s epoch %d minted %d events, want exactly 1", siteID, epoch, len(evs))
	}
	return evs[0]
}

// TestW9aCausalTripCorrectionMadeInKnowledgeWins — on a SCALAR fact (the
// driver) and on a MEMBER fact (`occupant/p-brand`): site-b asserts over
// the history it holds; site-a receives it and corrects it; the
// lesser-keyed correction wins in the composed trip, in Outcome both ways,
// and at a third post in either delivery order.
func TestW9aCausalTripCorrectionMadeInKnowledgeWins(t *testing.T) {
	t.Run("scalar fact — driver", func(t *testing.T) {
		st := w9aNewStore(t)
		trip, primer := w9aCausalWorld(t, st, "TRK-C1")
		b1 := d92Remote(t, primer, "site-b", w3Base+3*w3Hour, func(rs *store.Store) {
			if _, err := AmendTrip(rs, "site-b", 1, TripAmendment{TripID: trip.TripID, DriverPersonID: w3Str("drv-okafor")}, 0, "default"); err != nil {
				t.Fatal(err)
			}
		})
		w3Apply(t, st, b1)
		a1 := w9aAmendTripLocal(t, st, w3Base+1*w3Hour, TripAmendment{TripID: trip.TripID, DriverPersonID: w3Str("drv-vance")})
		d92RequireCausalArrangement(t, a1, b1)

		if c := w9aComposed(t, st, trip.TripID); c.Trip.DriverPersonID != "drv-vance" {
			t.Fatalf("D-92 (m-D92a/m-D92c-trip-scalar): composed driver = %q, want site-a's %q — the correction made IN KNOWLEDGE of site-b's wins regardless of site_id; %q is D-28 Part B's answer", c.Trip.DriverPersonID, "drv-vance", "drv-okafor")
		}
		if ob := w9aFact(t, st, b1.Key(), factTripDriver); ob.Status != Superseded || ob.Winner != a1.Key() {
			t.Fatalf("D-92: site-b's assertion is (%v, winner %v), want (Superseded, %v)", ob.Status, ob.Winner, a1.Key())
		}
		if oa := w9aFact(t, st, a1.Key(), factTripDriver); oa.Status != Effective {
			t.Fatalf("D-92: site-a's correction is %v, want Effective", oa.Status)
		}
		for i, order := range [][]store.Envelope{{a1, b1}, {b1, a1}} {
			third := w9aNewStore(t)
			w3ApplyAll(t, third, primer)
			w3ApplyAll(t, third, order)
			if c := w9aComposed(t, third, trip.TripID); c.Trip.DriverPersonID != "drv-vance" {
				t.Fatalf("W3-5 under D-92: a third post receiving order %d composes driver %q, want %q", i, c.Trip.DriverPersonID, "drv-vance")
			}
		}
	})

	t.Run("member fact — occupant/p-brand", func(t *testing.T) {
		st := w9aNewStore(t)
		trip, primer := w9aCausalWorld(t, st, "TRK-C2")
		// site-b removes p-brand; site-a, seeing the removal, puts him back.
		b1 := d92Remote(t, primer, "site-b", w3Base+3*w3Hour, func(rs *store.Store) {
			if _, err := AmendTrip(rs, "site-b", 1, TripAmendment{TripID: trip.TripID, OccupantsRemove: []string{"p-brand"}}, 0, "default"); err != nil {
				t.Fatal(err)
			}
		})
		w3Apply(t, st, b1)
		if c := w9aComposed(t, st, trip.TripID); w9aContainsID(c.OccupantIDs, "p-brand") {
			t.Fatalf("fixture defect: the peer's removal did not compose before the re-add (%v)", c.OccupantIDs)
		}
		a1 := w9aAmendTripLocal(t, st, w3Base+1*w3Hour, TripAmendment{TripID: trip.TripID, OccupantsAdd: []string{"p-brand"}})
		d92RequireCausalArrangement(t, a1, b1)

		const fact = factOccupantPrefix + "p-brand"
		if c := w9aComposed(t, st, trip.TripID); !w9aContainsID(c.OccupantIDs, "p-brand") {
			t.Fatalf("D-92 (m-D92a/m-D92c-trip-member): composed occupants = %v, want p-brand ABOARD — the re-add made IN KNOWLEDGE of the removal wins regardless of site_id", c.OccupantIDs)
		}
		if ob := w9aFact(t, st, b1.Key(), fact); ob.Status != Superseded || ob.Winner != a1.Key() {
			t.Fatalf("D-92: the peer's removal is (%v, winner %v), want (Superseded, %v)", ob.Status, ob.Winner, a1.Key())
		}
		if oa := w9aFact(t, st, a1.Key(), fact); oa.Status != Effective {
			t.Fatalf("D-92: the re-add is %v, want Effective", oa.Status)
		}
		for i, order := range [][]store.Envelope{{a1, b1}, {b1, a1}} {
			third := w9aNewStore(t)
			w3ApplyAll(t, third, primer)
			w3ApplyAll(t, third, order)
			if c := w9aComposed(t, third, trip.TripID); !w9aContainsID(c.OccupantIDs, "p-brand") {
				t.Fatalf("W3-5 under D-92: a third post receiving order %d composes occupants %v, want p-brand aboard", i, c.OccupantIDs)
			}
		}
	})
}

// TestW9aTripSeenIsStampedFromEveryHeadAndConcurrentEditsTiebreak — a
// concurrent driver pair over p0 falls to the site_id tiebreak at both
// posts; a third local amendment names BOTH heads, ascending; the stamp
// for a MEMBER fact names that member's head and nothing of the driver's
// contest; the row reads the list back; the first amendment omits the key;
// TripAmendment has no Seen field.
func TestW9aTripSeenIsStampedFromEveryHeadAndConcurrentEditsTiebreak(t *testing.T) {
	st := w9aNewStore(t)
	trip, _ := w9aCausalWorld(t, st, "TRK-C3")
	p0 := w9aAmendTripLocal(t, st, w3Base, TripAmendment{TripID: trip.TripID, DriverPersonID: w3Str("drv-okafor")})
	held := w3Events(t, st, "site-a", 1)
	a1 := w9aAmendTripLocal(t, st, w3Base+3*w3Hour, TripAmendment{TripID: trip.TripID, DriverPersonID: w3Str("drv-vance")})
	b1 := d92Remote(t, held, "site-b", w3Base+1*w3Hour, func(rs *store.Store) {
		if _, err := AmendTrip(rs, "site-b", 1, TripAmendment{TripID: trip.TripID, DriverPersonID: w3Str("drv-start")}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	w3Apply(t, st, b1)
	d92RequireConcurrentArrangement(t, a1, b1, []store.Key{p0.Key()})
	if c := w9aComposed(t, st, trip.TripID); c.Trip.DriverPersonID != "drv-start" {
		t.Fatalf("D-92 tiebreak: composed driver = %q, want the greater site_id's %q (chronology and the lesser key both say %q)", c.Trip.DriverPersonID, "drv-start", "drv-vance")
	}
	if got := w9aFact(t, st, a1.Key(), factTripDriver); got.Status != Superseded || got.Winner != b1.Key() {
		t.Fatalf("D-92 tiebreak: site-a's concurrent edit is (%v, %v), want (Superseded, %v)", got.Status, got.Winner, b1.Key())
	}

	c := w9aAmendTripLocal(t, st, w3Base+4*w3Hour, TripAmendment{TripID: trip.TripID, DriverPersonID: w3Str("drv-okafor")})
	// James, 2026-10-03 (NEW-b): on an OPEN trip a driver correction folds
	// the previous driver into its own occupants_add, so this amendment
	// asserts TWO facts — the driver, and `occupant/drv-start` (drv-start is
	// the effective driver it displaces). Its stamp therefore names the
	// driver contest's two heads AND that member fact's one head, p0 (the
	// first amendment, whose handover of drv-start was the only earlier
	// assertion about that member). ⟦was: want = {a1, b1}, "exactly the two
	// heads"⟧ — the driver contest's heads are unchanged; the extra key is
	// the extra fact's, exactly as D-92 asks.
	want := []store.Key{p0.Key(), a1.Key(), b1.Key()}
	sort.Slice(want, func(i, j int) bool { return want[i].Less(want[j]) })
	if seen := d92SeenOf(t, c); !w3KeysEqual(seen, want) {
		t.Fatalf("D-92 (m-D92b-trip): the third amendment carries seen=%v, want exactly the driver contest's two heads plus the kept-aboard member fact's head %v", seen, want)
	}
	if row := w9aTripSeenRowOf(t, st, c.Key(), trip.TripID); !w3KeysEqual(row, want) {
		t.Fatalf("D-92: trip_amendment.seen reads back %v, want %v", row, want)
	}
	// A MEMBER fact's stamp names that member's contest, not the driver's.
	m1 := w9aAmendTripLocal(t, st, w3Base+5*w3Hour, TripAmendment{TripID: trip.TripID, OccupantsRemove: []string{"p-chen"}})
	if seen := d92SeenOf(t, m1); len(seen) != 0 {
		t.Fatalf("D-92: the first assertion about occupant/p-chen carries seen=%v, want none — no earlier amendment asserted that member", seen)
	}
	m2 := w9aAmendTripLocal(t, st, w3Base+6*w3Hour, TripAmendment{TripID: trip.TripID, OccupantsAdd: []string{"p-chen"}})
	if seen := d92SeenOf(t, m2); !w3KeysEqual(seen, []store.Key{m1.Key()}) {
		t.Fatalf("D-92: the re-add of p-chen carries seen=%v, want exactly [%v] — the member's one head, and nothing of the driver's contest", seen, m1.Key())
	}
	if d92HasSeenKey(t, p0) {
		t.Fatalf("D-92: the FIRST assertion carries a `seen` key — an author who saw nothing standing must omit it")
	}
	d92RequireNoSeenField(t, TripAmendment{})
}

// TestW9aCausalArrivalClaimCorrectionMadeInKnowledgeWins — the arrival
// carrier: a peer corrects an unsolicited arrival's vehicle claim over the
// history it holds; the recording post, seeing it, corrects it again. Both
// corrections are authored at site-a — D-87/ownership.go gates
// arrival_amended to trip_arrival's own tag, so no other site_id could ever
// author one — but on two different node_epochs (w9aOwnSiteRemote), which
// is what makes them a genuinely independent, replicated pair: the greater
// epoch is the peer's, exactly as the greater site_id was before D-87. The
// lesser-keyed correction wins in EffectiveArrival and in Outcome both
// ways; the stamp names the peer's key; the first correction omits the
// key; ArrivalAmendment has no Seen field.
func TestW9aCausalArrivalClaimCorrectionMadeInKnowledgeWins(t *testing.T) {
	st := w9aNewStore(t)
	w9aSeedPersons(t, st, "site-a", 1, "p-chen")
	arr := w9aUnsolicited(t, st, "site-a", 1, "TRK-U1", "site-b", []string{"p-chen"})
	primer := w3Events(t, st, "site-a", 1)

	c1 := w9aOwnSiteRemote(t, primer, "site-a", 2, w3Base+3*w3Hour, func(rs *store.Store) {
		if _, err := AmendArrival(rs, "site-a", 2, ArrivalAmendment{ArrivalID: arr.ArrivalID, VehicleEntered: w3Str("TRK-U9")}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	w3Apply(t, st, c1)
	st.Clock = w3Reading(w3Base + 1*w3Hour)
	a1, err := AmendArrival(st, "site-a", 1, ArrivalAmendment{ArrivalID: arr.ArrivalID, VehicleEntered: w3Str("TRK-U7")}, 0, "default")
	if err != nil {
		t.Fatalf("fixture arrival amendment refused: %v", err)
	}
	d92RequireCausalArrangement(t, a1, c1)
	if d92HasSeenKey(t, c1) {
		t.Fatalf("D-92: the FIRST correction carries a `seen` key — an author who saw nothing standing must omit it")
	}
	d92RequireNoSeenField(t, ArrivalAmendment{})

	eff, err := EffectiveArrival(st, arr.ArrivalID)
	if err != nil || eff == nil {
		t.Fatalf("EffectiveArrival: %v (%v)", err, eff)
	}
	if eff.Arrival.VehicleEntered == nil || *eff.Arrival.VehicleEntered != "TRK-U7" {
		t.Fatalf("D-92 (m-D92a/m-D92c-arrival): composed vehicle claim = %v, want the recording post's %q — the correction made IN KNOWLEDGE of the peer's wins regardless of site_id; %q is D-28 Part B's answer", eff.Arrival.VehicleEntered, "TRK-U7", "TRK-U9")
	}
	if oc := w9aFact(t, st, c1.Key(), factArrivalVehicle); oc.Status != Superseded || oc.Winner != a1.Key() {
		t.Fatalf("D-92: the peer's claim is (%v, winner %v), want (Superseded, %v)", oc.Status, oc.Winner, a1.Key())
	}
	if oa := w9aFact(t, st, a1.Key(), factArrivalVehicle); oa.Status != Effective {
		t.Fatalf("D-92: the recording post's correction is %v, want Effective", oa.Status)
	}
	for i, order := range [][]store.Envelope{{a1, c1}, {c1, a1}} {
		third := w9aNewStore(t)
		w3ApplyAll(t, third, primer)
		w3ApplyAll(t, third, order)
		e, err := EffectiveArrival(third, arr.ArrivalID)
		if err != nil || e == nil || e.Arrival.VehicleEntered == nil || *e.Arrival.VehicleEntered != "TRK-U7" {
			t.Fatalf("W3-5 under D-92: a third post receiving order %d composes the claim as %v (%v), want %q", i, e, err, "TRK-U7")
		}
	}
}
