// w7_causal_compose_test.go — the vehicle carrier under [[D-92]] at
// SchemaVersion 11 (step 3: the causal `seen` reference cloned onto vehicle
// THIRD, after company and trade). The fixtures are the vehicle twins of
// w3_causal_compose_test.go's; the rule is resolveContest, read once.
//
// KILLER MUTANTS, each PRODUCED red once against this tree before commit
// (reports/witness/STEP3-CLONE.md): m-D92a (shared), m-D92b-vehicle
// (AmendVehicle drops the stamp), m-D92c-vehicle (composeVehicle falls back
// to last key), m-D92d-vehicle (vehicleMergeMapFromRows falls back to last
// key).
package domain

import (
	"testing"

	"sitecontrol/internal/store"
)

func w7VehiclePairForCausal(t *testing.T, st *store.Store) (idA, idB string, stream []store.Envelope) {
	t.Helper()
	a, err := CreateVehicle(st, "site-a", 1, w3Str("TRUCK 41"), nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	b, err := CreateVehicle(st, "site-a", 1, w3Str("TRUCK 42"), nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	return a.VehicleID, b.VehicleID, w3Events(t, st, "site-a", 1)
}

func w7AmendVehicleLocal(t *testing.T, st *store.Store, clockMS int64, in VehicleAmendment) store.Envelope {
	t.Helper()
	st.Clock = w3Reading(clockMS)
	env, err := AmendVehicle(st, "site-a", 1, in, 0, "default")
	if err != nil {
		t.Fatalf("fixture vehicle amendment refused: %v", err)
	}
	return env
}

func w7Vehicle(t *testing.T, st *store.Store, id string) *Vehicle {
	t.Helper()
	v, err := VehicleByID(st, id)
	if err != nil {
		t.Fatal(err)
	}
	if v == nil {
		t.Fatalf("vehicle %s: no row", id)
	}
	return v
}

func w7SeenRowOf(t *testing.T, st *store.Store, k store.Key, vehicleID string) []store.Key {
	t.Helper()
	rows, err := readVehicleAmendmentsFor(st, vehicleID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Key == k {
			return r.Seen
		}
	}
	t.Fatalf("no vehicle_amendment row at %v for %s", k, vehicleID)
	return nil
}

// TestW7CausalVehicleCorrectionMadeInKnowledgeWins — site-b asserts a
// number over the bare record; site-a receives it and corrects it. The
// lesser-keyed correction wins: composed number, Outcome both ways, and a
// third post in either delivery order.
func TestW7CausalVehicleCorrectionMadeInKnowledgeWins(t *testing.T) {
	const numB, numA = "TRUCK 41B", "TRUCK 41A"
	st := w3NewStore(t)
	x, _, roster := w7VehiclePairForCausal(t, st)

	b1 := d92Remote(t, roster, "site-b", w3Base+3*w3Hour, func(rs *store.Store) {
		if _, err := AmendVehicle(rs, "site-b", 1, VehicleAmendment{VehicleID: x, Number: w3Str(numB)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	w3Apply(t, st, b1)
	a1 := w7AmendVehicleLocal(t, st, w3Base+1*w3Hour, VehicleAmendment{VehicleID: x, Number: w3Str(numA)})
	d92RequireCausalArrangement(t, a1, b1)

	if v := w7Vehicle(t, st, x); v.Number == nil || *v.Number != numA {
		t.Fatalf("D-92 (m-D92a/m-D92c-vehicle): composed number = %v, want site-a's %q — the correction made IN KNOWLEDGE of site-b's wins regardless of site_id; %q is D-28 Part B's answer", v.Number, numA, numB)
	}
	if ob := w3Fact(t, st, b1.Key(), "number"); ob.Status != Superseded || ob.Winner != a1.Key() {
		t.Fatalf("D-92: site-b's assertion is (%v, winner %v), want (Superseded, %v)", ob.Status, ob.Winner, a1.Key())
	}
	if oa := w3Fact(t, st, a1.Key(), "number"); oa.Status != Effective {
		t.Fatalf("D-92: site-a's correction is %v, want Effective", oa.Status)
	}
	for i, order := range [][]store.Envelope{{a1, b1}, {b1, a1}} {
		third := w3NewStore(t)
		w3ApplyAll(t, third, roster)
		w3ApplyAll(t, third, order)
		if q := w7Vehicle(t, third, x); q.Number == nil || *q.Number != numA {
			t.Fatalf("W3-5 under D-92: a third post receiving order %d composes number %v, want %q", i, q.Number, numA)
		}
	}
}

// TestW7VehicleSeenIsStampedFromEveryHeadAndConcurrentEditsTiebreak — a
// concurrent pair over p0 on the `role` fact falls to the site_id tiebreak;
// a third local amendment names BOTH heads; the row reads the list back;
// the first assertion omits the key; VehicleAmendment has no Seen field.
func TestW7VehicleSeenIsStampedFromEveryHeadAndConcurrentEditsTiebreak(t *testing.T) {
	const r0, rA, rB, rC = "bus", "van", "pickup", "sled"
	st := w3NewStore(t)
	x, _, _ := w7VehiclePairForCausal(t, st)
	p0 := w7AmendVehicleLocal(t, st, w3Base, VehicleAmendment{VehicleID: x, Role: w3Str(r0)})
	held := w3Events(t, st, "site-a", 1)
	a1 := w7AmendVehicleLocal(t, st, w3Base+3*w3Hour, VehicleAmendment{VehicleID: x, Role: w3Str(rA)})
	b1 := d92Remote(t, held, "site-b", w3Base+1*w3Hour, func(rs *store.Store) {
		if _, err := AmendVehicle(rs, "site-b", 1, VehicleAmendment{VehicleID: x, Role: w3Str(rB)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	w3Apply(t, st, b1)
	d92RequireConcurrentArrangement(t, a1, b1, []store.Key{p0.Key()})
	if v := w7Vehicle(t, st, x); v.Role == nil || *v.Role != rB {
		t.Fatalf("D-92 tiebreak: composed role = %v, want the greater site_id's %q (chronology and the lesser key both say %q)", v.Role, rB, rA)
	}
	if got := w3Fact(t, st, a1.Key(), "role"); got.Status != Superseded || got.Winner != b1.Key() {
		t.Fatalf("D-92 tiebreak: site-a's concurrent edit is (%v, %v), want (Superseded, %v)", got.Status, got.Winner, b1.Key())
	}

	c := w7AmendVehicleLocal(t, st, w3Base+4*w3Hour, VehicleAmendment{VehicleID: x, Role: w3Str(rC)})
	want := []store.Key{a1.Key(), b1.Key()}
	if !want[0].Less(want[1]) {
		want[0], want[1] = want[1], want[0]
	}
	if seen := d92SeenOf(t, c); !w3KeysEqual(seen, want) {
		t.Fatalf("D-92 (m-D92b-vehicle): the third amendment carries seen=%v, want exactly the two heads %v", seen, want)
	}
	if row := w7SeenRowOf(t, st, c.Key(), x); !w3KeysEqual(row, want) {
		t.Fatalf("D-92: vehicle_amendment.seen reads back %v, want %v", row, want)
	}
	if d92HasSeenKey(t, p0) {
		t.Fatalf("D-92: the FIRST assertion carries a `seen` key — an author who saw nothing standing must omit it")
	}
	d92RequireNoSeenField(t, VehicleAmendment{})
	if v := w7Vehicle(t, st, x); v.Role == nil || *v.Role != rC {
		t.Fatalf("D-92: composed role = %v, want %q", v.Role, rC)
	}
}

// TestW7VehicleUnMergeMadeInKnowledgeOfThePeersMergeTakesEffect — the join
// pointer on the vehicle carrier: the un-merge made in knowledge of site-b's
// join wins at the lesser key; LoadVehicleMergeMap and ResolveVehicleID
// resolve X to itself.
func TestW7VehicleUnMergeMadeInKnowledgeOfThePeersMergeTakesEffect(t *testing.T) {
	st := w3NewStore(t)
	x, y, roster := w7VehiclePairForCausal(t, st)
	merge := d92Remote(t, roster, "site-b", w3Base+2*w3Hour, func(rs *store.Store) {
		if _, err := AmendVehicle(rs, "site-b", 1, VehicleAmendment{VehicleID: x, SameAs: w3Str(y)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	w3Apply(t, st, merge)
	if v := w7Vehicle(t, st, x); v.SameAs == nil || *v.SameAs != y {
		t.Fatalf("fixture defect: the peer's join did not compose before the un-merge (same_as=%v)", v.SameAs)
	}
	unmerge := w7AmendVehicleLocal(t, st, w3Base+1*w3Hour, VehicleAmendment{VehicleID: x, Clear: []string{"same_as"}})
	d92RequireCausalArrangement(t, unmerge, merge)

	for i, post := range []*store.Store{st, func() *store.Store {
		p := w3NewStore(t)
		w3ApplyAll(t, p, roster)
		w3Apply(t, p, unmerge)
		w3Apply(t, p, merge)
		return p
	}()} {
		if v := w7Vehicle(t, post, x); v.SameAs != nil {
			t.Fatalf("D-92 (m-D92a/m-D92c-vehicle) at post %d: composed same_as = %q, want absent", i, *v.SameAs)
		}
		m, err := LoadVehicleMergeMap(post)
		if err != nil {
			t.Fatal(err)
		}
		if got := ResolveVehicleID(m, x); got != x {
			t.Fatalf("D-92 (m-D92d-vehicle) at post %d: ResolveVehicleID(%s) = %s, want identity — the vehicle merge snapshot reads the same rule", i, x, got)
		}
		if got := w3Fact(t, post, unmerge.Key(), "same_as"); got.Status != Effective {
			t.Fatalf("D-92 at post %d: Outcome(un-merge) = %v, want Effective", i, got.Status)
		}
		if got := w3Fact(t, post, merge.Key(), "same_as"); got.Status != Superseded || got.Winner != unmerge.Key() {
			t.Fatalf("D-92 at post %d: Outcome(peer join) = (%v, %v), want (Superseded, %v)", i, got.Status, got.Winner, unmerge.Key())
		}
	}
}
