// witness_r18_unsol_vehicle_unidentified_test.go — SchemaVersion 14 (R-18,
// James: "Fix it — same bump."): the unsolicited-arrival door's own share
// of R-17 ("there are no doors only vehicles"), the movement R-17's build
// measured only four named doors and never put to him.
//
// SCOPE, on witness_r17_vehicle_unidentified_test.go's own template: this
// file proves (1) the contradictory combination (a named vehicle claimed
// unidentified) is refused at THIS door's own choke point, in Go AND at
// the storage layer; (2) an unidentified arrival authors cleanly, with
// origin_site preserved and no vehicle record minted; (3) the CHECK
// constraint's teeth, demonstrated by bypassing
// RecordUnsolicitedArrivalOriented entirely; (4) replication of the new
// fact to a THIRD store that never saw the original authoring; (5) D-4 —
// no crashed-check-in lookalike.
package domain

import (
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// --- 1. THE CONTRADICTION IS REFUSED --------------------------------------

// TestR18RecordUnsolicitedArrivalRefusesANamedVehicleClaimedUnidentified
// proves the refusal this ruling adds to RecordUnsolicitedArrivalOriented:
// vehicleUnidentified alongside a real id OR alongside a resolver is
// refused, authoring nothing.
func TestR18RecordUnsolicitedArrivalRefusesANamedVehicleClaimedUnidentified(t *testing.T) {
	const wantLiteral = "a vehicle cannot be both named and marked unidentified"

	for _, tc := range []struct {
		name        string
		vehicleID   string
		resolveFunc func() (string, error)
	}{
		{name: "a real vehicle id", vehicleID: "site-b-vehicle-real"},
		{name: "a resolver", resolveFunc: func() (string, error) { return "site-b-vehicle-resolved", nil }},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			st := newDomainTestStore(t)
			p1, p2 := seedTwoPersons(t, st, "site-b")
			before, err := st.Watermark("site-b", 1)
			if err != nil {
				t.Fatal(err)
			}

			var resolvers []func() (string, error)
			if tc.resolveFunc != nil {
				resolvers = append(resolvers, tc.resolveFunc)
			}
			_, err = RecordUnsolicitedArrivalOriented(st, "site-b", 1, tc.vehicleID, "site-a",
				[]string{p1.PersonID, p2.PersonID}, nil, 0, "default", false, true, resolvers...)
			if err == nil {
				t.Fatalf("%s: RecordUnsolicitedArrivalOriented ACCEPTED a vehicle simultaneously named and marked unidentified", tc.name)
			}
			if !strings.Contains(err.Error(), wantLiteral) {
				t.Fatalf("%s: refused with %q, want it to contain %q", tc.name, err.Error(), wantLiteral)
			}
			after, err := st.Watermark("site-b", 1)
			if err != nil {
				t.Fatal(err)
			}
			if after != before {
				t.Fatalf("%s: a refused arrival authored %d event(s) (watermark %d -> %d)", tc.name, after-before, before, after)
			}
		})
	}
}

// --- 2. AN UNIDENTIFIED ARRIVAL AUTHORS CLEANLY, ORIGIN SURVIVES, AND
//        NOTHING IS MINTED ------------------------------------------------

// TestR18UnsolicitedArrivalAuthorsCleanlyWithNoVehicleAndOriginSurvives
// proves the coverage and accumulation criteria together, plus D-4: an
// unidentified unsolicited arrival succeeds with NO vehicle number, its
// origin_site is preserved (the whole reason this door matters over the
// gate-checkin workaround — R-18's own text), domain.Vehicles() is
// unchanged (nothing minted), and the arrival never appears on
// IncompleteCheckIns (D-4 — it cannot, structurally: this door never
// authors a `trip` row at all, only `trip_arrival`, but that is asserted
// directly rather than assumed).
func TestR18UnsolicitedArrivalAuthorsCleanlyWithNoVehicleAndOriginSurvives(t *testing.T) {
	st := newDomainTestStore(t)
	p1, p2 := seedTwoPersons(t, st, "site-b")

	before, err := Vehicles(st)
	if err != nil {
		t.Fatal(err)
	}

	arr, err := RecordUnsolicitedArrivalOriented(st, "site-b", 1, "", "site-a",
		[]string{p1.PersonID, p2.PersonID}, nil, 0, "default", false, true)
	if err != nil {
		t.Fatalf("RecordUnsolicitedArrivalOriented: %v", err)
	}
	if !arr.VehicleIsUnidentified() {
		t.Error("returned Arrival does not report VehicleIsUnidentified()")
	}
	if arr.VehicleID != nil {
		t.Errorf("returned Arrival.VehicleID = %q, want nil", *arr.VehicleID)
	}
	if arr.VehicleEntered != nil {
		t.Errorf("returned Arrival.VehicleEntered = %q, want nil", *arr.VehicleEntered)
	}
	if arr.OriginSite == nil || *arr.OriginSite != "site-a" {
		t.Fatalf("returned Arrival.OriginSite = %v, want \"site-a\" — this is the whole reason the door matters over the gate workaround (R-18)", arr.OriginSite)
	}

	// Re-read via the projection — the int64 decode (Arrival.VehicleUnidentified's
	// own comment) must agree with what the function handed back directly.
	all, err := Arrivals(st)
	if err != nil {
		t.Fatal(err)
	}
	var reread *Arrival
	for i := range all {
		if all[i].ArrivalID == arr.ArrivalID {
			reread = &all[i]
		}
	}
	if reread == nil {
		t.Fatal("the arrival is not present in domain.Arrivals()")
	}
	if !reread.VehicleIsUnidentified() {
		t.Error("the re-read Arrival does not report VehicleIsUnidentified() — the projection-side int64 decode disagrees with what the function returned")
	}
	if reread.VehicleID != nil {
		t.Errorf("the re-read Arrival.VehicleID = %q, want nil", *reread.VehicleID)
	}
	if reread.OriginSite == nil || *reread.OriginSite != "site-a" {
		t.Fatalf("the re-read Arrival.OriginSite = %v, want \"site-a\"", reread.OriginSite)
	}

	after, err := Vehicles(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("an unidentified unsolicited arrival changed the vehicle count %d -> %d — nothing should ever be minted for this fact", len(before), len(after))
	}

	// Both occupants actually landed on the muster — the door works, not
	// merely "authors without erroring".
	openVisits, err := OpenVisitsAt(st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(openVisits) != 2 {
		t.Fatalf("open visits at site-b = %d, want 2 — the unidentified arrival must still check both occupants in", len(openVisits))
	}

	// ⛔ D-4: this door never authors a `trip` row at all (only
	// `trip_arrival`), so an unidentified arrival cannot, structurally,
	// appear on the crash-recovery list — asserted directly rather than
	// assumed.
	inc, err := IncompleteCheckIns(st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(inc) != 0 {
		t.Fatalf("an unidentified unsolicited arrival appears on IncompleteCheckIns: %+v", inc)
	}
}

// TestR18ANamedVehicleUnsolicitedArrivalStillWorksExactlyAsBefore is the
// negative control: an ordinary, real-vehicle unsolicited arrival is
// unaffected by this bump — same behaviour byte for byte.
func TestR18ANamedVehicleUnsolicitedArrivalStillWorksExactlyAsBefore(t *testing.T) {
	st := newDomainTestStore(t)
	p1, p2 := seedTwoPersons(t, st, "site-b")
	veh := w7SeedVehicleID(t, st, "site-b", "TRK 99")

	arr, err := RecordUnsolicitedArrivalOriented(st, "site-b", 1, veh, "site-a",
		[]string{p1.PersonID, p2.PersonID}, nil, 0, "default", false, false)
	if err != nil {
		t.Fatalf("RecordUnsolicitedArrivalOriented: %v", err)
	}
	if arr.VehicleIsUnidentified() {
		t.Error("a named-vehicle arrival reports VehicleIsUnidentified()")
	}
	if arr.VehicleID == nil || *arr.VehicleID != veh {
		t.Errorf("arr.VehicleID = %v, want %q", arr.VehicleID, veh)
	}
}

// --- 3. THE CHECK CONSTRAINT'S OWN TEETH ---------------------------------

// TestR18TheCheckConstraintRefusesWhatRecordUnsolicitedArrivalAlreadyRefuses
// proves the storage-level half of the guarantee directly, by going AROUND
// RecordUnsolicitedArrivalOriented entirely — authoring a raw trip_arrival
// event whose payload already carries the contradictory combination, the
// shape a future, different build (or a replicated row from one) could
// produce without ever calling this door's Go refusal at all.
func TestR18TheCheckConstraintRefusesWhatRecordUnsolicitedArrivalAlreadyRefuses(t *testing.T) {
	st := newDomainTestStore(t)
	_, _ = seedTwoPersons(t, st, "site-b")

	vehicleID := "site-b-vehicle-contradiction"
	origin := "site-a"
	payload := mustJSON(t, TripArrival{
		Type: "trip_arrival", ArrivalID: "site-b-arrival-contradiction",
		VehicleID:           &vehicleID,
		VehicleUnidentified: true, // the contradiction: a real id AND the flag
		OriginSite:          &origin,
	})
	if _, err := st.Author("site-b", 1, payload, 0, "default"); err == nil {
		t.Fatal("store.Author ACCEPTED a trip_arrival payload naming a real vehicle_id AND claiming vehicle_unidentified — the CHECK constraint on trip_arrival.vehicle_unidentified did not fire")
	} else if !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Fatalf("store.Author refused, but not with a CHECK constraint failure: %v", err)
	}
}

// TestR18TheCheckConstraintPermitsAnExpectedArrivalWithNoVehicleAtAll
// proves the CHECK is ONE-DIRECTIONAL (R-18's own store.go comment): an
// EXPECTED arrival — trip_ref set, vehicle_id NULL, vehicle_unidentified 0
// — is the ORDINARY shape of the overwhelming majority of this table's
// rows, and must NOT be refused by a symmetric CHECK the way a first pass
// at `trip`'s own CHECK broke real fold-robustness fixtures.
func TestR18TheCheckConstraintPermitsAnExpectedArrivalWithNoVehicleAtAll(t *testing.T) {
	st := newDomainTestStore(t)
	tripID := "site-b-trip-expected"
	payload := mustJSON(t, TripArrival{
		Type: "trip_arrival", ArrivalID: "site-b-arrival-expected",
		TripRef: &tripID, // VehicleID/VehicleUnidentified both zero value
	})
	if _, err := st.Author("site-b", 1, payload, 0, "default"); err != nil {
		t.Fatalf("store.Author REFUSED an ordinary expected-arrival shape (vehicle_id NULL, vehicle_unidentified 0): %v — the CHECK must be one-directional, not symmetric", err)
	}
}

// --- 4. REPLICATION -------------------------------------------------------

// TestR18UnidentifiedVehicleReplicatesToAThirdPostThatNeverSawTheArrival
// proves VehicleUnidentified survives real cross-post replication for the
// arrival side, on witness_r17's own instrument
// (schema12_entry_replication_test.go's "stand up scratch posts and prove
// it" pattern): B and C never call RecordUnsolicitedArrivalOriented
// themselves — they replay A's own event log, store.Apply, one envelope at
// a time, in stream order.
func TestR18UnidentifiedVehicleReplicatesToAThirdPostThatNeverSawTheArrival(t *testing.T) {
	a := newDomainTestStore(t)
	b := newDomainTestStore(t)
	c := newDomainTestStore(t)

	p1, p2 := seedTwoPersons(t, a, "site-b")
	arr, err := RecordUnsolicitedArrivalOriented(a, "site-b", 1, "", "site-a",
		[]string{p1.PersonID, p2.PersonID}, nil, 0, "default", false, true)
	if err != nil {
		t.Fatalf("RecordUnsolicitedArrivalOriented at A: %v", err)
	}

	replicateR18Stream(t, b, a, "site-b")
	replicateR18Stream(t, c, a, "site-b")

	for name, dst := range map[string]*store.Store{"B": b, "C": c} {
		all, err := Arrivals(dst)
		if err != nil {
			t.Fatalf("%s: Arrivals: %v", name, err)
		}
		var got *Arrival
		for i := range all {
			if all[i].ArrivalID == arr.ArrivalID {
				got = &all[i]
			}
		}
		if got == nil {
			t.Fatalf("%s never received the arrival at all", name)
		}
		if !got.VehicleIsUnidentified() {
			t.Errorf("%s: replicated arrival does not report VehicleIsUnidentified()", name)
		}
		if got.VehicleID != nil {
			t.Errorf("%s: replicated arrival.VehicleID = %q, want nil", name, *got.VehicleID)
		}
		if got.OriginSite == nil || *got.OriginSite != "site-a" {
			t.Errorf("%s: replicated arrival.OriginSite = %v, want \"site-a\"", name, got.OriginSite)
		}
	}

	stored, err := c.StoredSchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	// SchemaVersion 15 (R-20 + R-23, the rejoin path): re-pinned from "14"
	// — unrelated to this R-18 proof; the literal tracks the CURRENT version.
	// SchemaVersion 16 (R3 alert_kind, 2026-10-04): re-pinned; ⟦was: "15"⟧
	if stored != "16" {
		t.Errorf("C's own schema_version = %q, want \"16\" — this whole proof is only meaningful post-bump", stored)
	}
}

// replicateR18Stream is witness_r17's own replicateR17Stream, cloned rather
// than shared: a helper shared across witness files for two different
// rulings is a dependency that can be deleted out from under either one
// (that file's own stated reasoning).
func replicateR18Stream(t *testing.T, dst, src *store.Store, siteID string) {
	t.Helper()
	evs, err := src.Events(siteID, 1, 1, 0)
	if err != nil {
		t.Fatalf("replicateR18Stream(%s): reading source events: %v", siteID, err)
	}
	if len(evs) == 0 {
		t.Fatalf("replicateR18Stream(%s): source authored nothing — fixture defect", siteID)
	}
	for _, ev := range evs {
		if _, err := dst.Apply(ev); err != nil {
			t.Fatalf("replicateR18Stream(%s): peer refused envelope %v: %v", siteID, ev.Key(), err)
		}
	}
}
