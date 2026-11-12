// witness_r17_vehicle_unidentified_test.go — SchemaVersion 13 (R-17, James:
// "there are no doors only vehicles"): proves the choke-point fact at the
// layer that actually enforces it.
//
// SCOPE: this file proves (1) the contradictory combination (a named
// vehicle claimed unidentified) is refused at BOTH doors the choke point
// serves, in Go AND at the storage layer; (2) an unidentified movement
// authors cleanly through each door, with no vehicle record minted; (3) the
// CHECK constraint's teeth, demonstrated by bypassing dispatchTrip
// entirely; (4) replication of the new fact to a THIRD store that never
// saw the original authoring, on the SCHEMA-BUMP.md pattern.
package domain

import (
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// --- 1. THE CONTRADICTION IS REFUSED, AT BOTH DOORS ----------------------

// TestR17DispatchRefusesANamedVehicleClaimedUnidentified proves the refusal
// this ruling adds to dispatchTrip: VehicleUnidentified alongside a real id
// OR alongside a resolver is refused, by the pinned literal, authoring
// nothing.
func TestR17DispatchRefusesANamedVehicleClaimedUnidentified(t *testing.T) {
	const wantLiteral = "a vehicle cannot be both named and marked unidentified"
	dest := "site-b"
	deadline := int64(60)

	for _, tc := range []struct {
		name string
		with func(*DispatchInput)
	}{
		{"a real vehicle id", func(in *DispatchInput) {
			in.VehicleID = "site-a-vehicle-real"
		}},
		{"a resolver", func(in *DispatchInput) {
			in.ResolveVehicle = func() (string, error) { return "site-a-vehicle-resolved", nil }
		}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			st := newDomainTestStore(t)
			driver, _ := seedTwoPersons(t, st, "site-a")
			before, err := st.Watermark("site-a", 1)
			if err != nil {
				t.Fatal(err)
			}

			in := DispatchInput{
				DriverPersonID:      driver.PersonID,
				DestinationSite:     &dest,
				DeadlineMinutes:     &deadline,
				VehicleUnidentified: true,
			}
			tc.with(&in)

			_, err = Dispatch(st, "site-a", 1, in, 0, "default")
			if err == nil {
				t.Fatalf("%s: Dispatch ACCEPTED a vehicle simultaneously named and marked unidentified", tc.name)
			}
			if !strings.Contains(err.Error(), wantLiteral) {
				t.Fatalf("%s: refused with %q, want it to contain %q", tc.name, err.Error(), wantLiteral)
			}
			after, err := st.Watermark("site-a", 1)
			if err != nil {
				t.Fatal(err)
			}
			if after != before {
				t.Fatalf("%s: a refused Dispatch authored %d event(s) (watermark %d -> %d)", tc.name, after-before, before, after)
			}
		})
	}
}

// TestR17GateCheckInRefusesANamedVehicleClaimedUnidentified is
// TestR17DispatchRefusesANamedVehicleClaimedUnidentified's own copy at the
// OTHER door the choke point serves — GateCheckIn's
// resolveVehicleIDBeforeMinting, which mirrors dispatchTrip's refusal by
// hand (checkin.go's own comment on why it is a deliberate duplicate, not a
// shared call).
func TestR17GateCheckInRefusesANamedVehicleClaimedUnidentified(t *testing.T) {
	const wantLiteral = "a vehicle cannot be both named and marked unidentified"

	for _, tc := range []struct {
		name string
		with func(*GateCheckInInput)
	}{
		{"a real vehicle id", func(in *GateCheckInInput) {
			in.VehicleID = "site-a-vehicle-real"
		}},
		{"a resolver", func(in *GateCheckInInput) {
			in.ResolveVehicle = func() (string, error) { return "site-a-vehicle-resolved", nil }
		}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			st := newDomainTestStore(t)
			driver, passenger := seedTwoPersons(t, st, "site-a")
			before, err := st.Watermark("site-a", 1)
			if err != nil {
				t.Fatal(err)
			}

			in := GateCheckInInput{
				VehicleUnidentified: true,
				Driver:              PersonSlot{ExistingID: driver.PersonID},
				Occupants:           []PersonSlot{{ExistingID: passenger.PersonID}},
			}
			tc.with(&in)

			_, _, err = GateCheckIn(st, "site-a", 1, in, 0, "default")
			if err == nil {
				t.Fatalf("%s: GateCheckIn ACCEPTED a vehicle simultaneously named and marked unidentified", tc.name)
			}
			if !strings.Contains(err.Error(), wantLiteral) {
				t.Fatalf("%s: refused with %q, want it to contain %q", tc.name, err.Error(), wantLiteral)
			}
			after, err := st.Watermark("site-a", 1)
			if err != nil {
				t.Fatal(err)
			}
			if after != before {
				t.Fatalf("%s: a refused GateCheckIn authored %d event(s) (watermark %d -> %d) — R-12's own \"the refusal must leave no trace\" rule", tc.name, after-before, before, after)
			}
		})
	}
}

// --- 2. AN UNIDENTIFIED MOVEMENT AUTHORS CLEANLY, AT BOTH DOORS, AND
//        MINTS NOTHING ------------------------------------------------

// TestR17DispatchAuthorsAnUnidentifiedDepartureAndMintsNoVehicle proves
// coverage (departure) and the accumulation criterion together: a real
// inter-site departure with no vehicle named succeeds, the trip carries
// VehicleUnidentified with an empty VehicleID and a nil snapshot, and
// domain.Vehicles() — the merge picker's one non-test caller — is
// unchanged, because nothing was ever minted.
func TestR17DispatchAuthorsAnUnidentifiedDepartureAndMintsNoVehicle(t *testing.T) {
	st := newDomainTestStore(t)
	driver, _ := seedTwoPersons(t, st, "site-a")
	dest := "site-b"
	deadline := int64(60)

	before, err := Vehicles(st)
	if err != nil {
		t.Fatal(err)
	}

	trip, err := Dispatch(st, "site-a", 1, DispatchInput{
		DriverPersonID:      driver.PersonID,
		DestinationSite:     &dest,
		DeadlineMinutes:     &deadline,
		VehicleUnidentified: true,
	}, 0, "default")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !trip.VehicleIsUnidentified() {
		t.Error("returned Trip does not report VehicleIsUnidentified()")
	}
	if trip.VehicleID != "" {
		t.Errorf("returned Trip.VehicleID = %q, want empty", trip.VehicleID)
	}
	if trip.VehicleEntered != nil {
		t.Errorf("returned Trip.VehicleEntered = %q, want nil", *trip.VehicleEntered)
	}

	// Re-read from the store — the projection-side decode (int64, not
	// bool; see Trip.VehicleUnidentified's own comment) must agree with
	// what Dispatch handed back directly.
	reread, err := TripByID(st, trip.TripID)
	if err != nil || reread == nil {
		t.Fatalf("TripByID: %v", err)
	}
	if !reread.VehicleIsUnidentified() {
		t.Error("the re-read Trip does not report VehicleIsUnidentified() — the projection-side int64 decode disagrees with what Dispatch returned")
	}
	if reread.VehicleID != "" {
		t.Errorf("the re-read Trip.VehicleID = %q, want empty", reread.VehicleID)
	}

	after, err := Vehicles(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("an unidentified departure changed the vehicle count %d -> %d — nothing should ever be minted for this fact", len(before), len(after))
	}
}

// TestR17GateCheckInAuthorsAnUnidentifiedArrivalAndMintsNoVehicle is the
// same proof at check-in, PLUS the D-4 assertion: an unidentified-vehicle
// check-in still completes as an ordinary, COMPLETE check-in — it must
// never appear on IncompleteCheckIns, because GateCheckIn always authors
// both the dispatch and the arrival half in the same call (barring an
// actual crash, which VehicleUnidentified has nothing to do with).
func TestR17GateCheckInAuthorsAnUnidentifiedArrivalAndMintsNoVehicle(t *testing.T) {
	st := newDomainTestStore(t)
	driver, passenger := seedTwoPersons(t, st, "site-a")

	before, err := Vehicles(st)
	if err != nil {
		t.Fatal(err)
	}

	trip, arrival, err := GateCheckIn(st, "site-a", 1, GateCheckInInput{
		VehicleUnidentified: true,
		Driver:              PersonSlot{ExistingID: driver.PersonID},
		Occupants:           []PersonSlot{{ExistingID: passenger.PersonID}},
	}, 0, "default")
	if err != nil {
		t.Fatalf("GateCheckIn: %v", err)
	}
	if !trip.VehicleIsUnidentified() || trip.VehicleID != "" {
		t.Errorf("check-in trip VehicleIsUnidentified=%v VehicleID=%q, want true/\"\"", trip.VehicleIsUnidentified(), trip.VehicleID)
	}
	if arrival.ArrivalID == "" {
		t.Fatal("no arrival authored")
	}

	after, err := Vehicles(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("an unidentified check-in changed the vehicle count %d -> %d — nothing should ever be minted for this fact", len(before), len(after))
	}

	// ⛔ D-4: an unidentified-vehicle check-in must complete as ORDINARY —
	// never read as a crashed, incomplete check-in. It cannot, structurally
	// (GateCheckIn authors both halves in one call and only crashes between
	// them on a real process death, which VehicleUnidentified does not
	// simulate), but this is asserted directly rather than assumed.
	inc, err := IncompleteCheckIns(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(inc) != 0 {
		t.Fatalf("an unidentified-vehicle check-in appears on IncompleteCheckIns: %+v", inc)
	}
}

// --- 3. THE CHECK CONSTRAINT'S OWN TEETH ---------------------------------

// TestR17TheCheckConstraintRefusesWhatDispatchTripAlreadyRefuses proves the
// storage-level half of the guarantee directly, by going AROUND
// dispatchTrip entirely — authoring a raw trip_dispatched event whose
// payload already carries the contradictory combination, exactly the shape
// a future, different build (or a replicated row from one) could produce
// without ever calling dispatchTrip at all. The application-level refusal
// (proved above) stops today's callers; this proves the combination is
// impossible in STORAGE regardless of caller.
func TestR17TheCheckConstraintRefusesWhatDispatchTripAlreadyRefuses(t *testing.T) {
	st := newDomainTestStore(t)
	driver, _ := seedTwoPersons(t, st, "site-a")

	payload := mustJSON(t, TripDispatched{
		Type:                "trip_dispatched",
		TripID:              "site-a-trip-contradiction",
		VehicleID:           "site-a-vehicle-real",
		VehicleUnidentified: true, // the contradiction: a real id AND the flag
		DriverPersonID:      driver.PersonID,
		OccupantPersonIDs:   []string{driver.PersonID},
	})
	// store.Author runs the fold synchronously and returns its error — the
	// same path a real caller's authored-and-committed event would take.
	// A CHECK failure here is a FOLD failure, not a refusal any caller
	// asked for; that is the point being proved.
	if _, err := st.Author("site-a", 1, payload, 0, "default"); err == nil {
		t.Fatal("store.Author ACCEPTED a trip_dispatched payload naming a real vehicle_id AND claiming vehicle_unidentified — the CHECK constraint on trip.vehicle_unidentified did not fire")
	} else if !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Fatalf("store.Author refused, but not with a CHECK constraint failure: %v", err)
	}
}

// --- 4. REPLICATION, ON THE SCHEMA-BUMP.md PATTERN -----------------------

// TestR17UnidentifiedVehicleReplicatesToAThirdPostThatNeverSawTheArrival
// proves VehicleUnidentified survives real cross-post replication, not
// just a local fold — the same "stand up scratch posts and prove it"
// instrument schema12_entry_replication_test.go used for the 11->12 bump's
// own wire-shape change, applied to this bump's.
//
// A (origin) dispatches an unidentified-vehicle departure to B. C never
// sees A's or B's events directly — it replays A's own event log,
// store.Apply, one envelope at a time, in stream order, the way a real
// peer would. If C's own fold reconstructs the SAME fact from bytes alone,
// the wire shape (TripDispatched.VehicleUnidentified, omitempty, bool) and
// the projection shape (trip.vehicle_unidentified, the CHECK constraint)
// both survived the trip through JSON and back.
func TestR17UnidentifiedVehicleReplicatesToAThirdPostThatNeverSawTheArrival(t *testing.T) {
	a := newDomainTestStore(t)
	b := newDomainTestStore(t)
	c := newDomainTestStore(t)

	driver, _ := seedTwoPersons(t, a, "site-a")
	dest := "site-b"
	deadline := int64(60)
	trip, err := Dispatch(a, "site-a", 1, DispatchInput{
		DriverPersonID:      driver.PersonID,
		DestinationSite:     &dest,
		DeadlineMinutes:     &deadline,
		VehicleUnidentified: true,
	}, 0, "default")
	if err != nil {
		t.Fatalf("Dispatch at A: %v", err)
	}

	// Replay A's own stream into B and into C — a peer's own view of one
	// stream, built the same way store.Events/store.Apply are the
	// sanctioned primitives for it.
	replicateR17Stream(t, b, a, "site-a")
	replicateR17Stream(t, c, a, "site-a")

	for name, dst := range map[string]*store.Store{"B": b, "C": c} {
		got, err := TripByID(dst, trip.TripID)
		if err != nil {
			t.Fatalf("%s: TripByID: %v", name, err)
		}
		if got == nil {
			t.Fatalf("%s never received the trip at all", name)
		}
		if !got.VehicleIsUnidentified() {
			t.Errorf("%s: replicated trip does not report VehicleIsUnidentified()", name)
		}
		if got.VehicleID != "" {
			t.Errorf("%s: replicated trip.VehicleID = %q, want empty", name, got.VehicleID)
		}
		if got.VehicleEntered != nil {
			t.Errorf("%s: replicated trip.VehicleEntered = %q, want nil", name, *got.VehicleEntered)
		}
	}

	stored, err := c.StoredSchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	// SchemaVersion 14 (R-18): re-pinned from "13" — this R-17 proof is
	// unrelated to the unsolicited-arrival vehicle change that landed 14;
	// the literal just needs to track whatever the CURRENT version is.
	// SchemaVersion 15 (R-20 + R-23, the rejoin path): re-pinned from "14"
	// — unrelated to this R-17 proof; the literal tracks the CURRENT version.
	// SchemaVersion 16 (R3 alert_kind, 2026-10-04): re-pinned; ⟦was: "15"⟧
	if stored != "16" {
		t.Errorf("C's own schema_version = %q, want \"16\" — this whole proof is only meaningful post-bump", stored)
	}
}

// replicateR17Stream pulls every envelope siteID's own store has authored
// and applies it to dst, in order — cloned from
// schema12_entry_replication_test.go's replicateStream (that file's own
// helper is unexported to its own package scope and this file intentionally
// does not share it, on that file's own reasoning: a helper shared across
// witness files for two different rulings is a dependency that can be
// deleted out from under either one).
func replicateR17Stream(t *testing.T, dst, src *store.Store, siteID string) {
	t.Helper()
	evs, err := src.Events(siteID, 1, 1, 0)
	if err != nil {
		t.Fatalf("replicateR17Stream(%s): reading source events: %v", siteID, err)
	}
	if len(evs) == 0 {
		t.Fatalf("replicateR17Stream(%s): source authored nothing — fixture defect", siteID)
	}
	for _, ev := range evs {
		if _, err := dst.Apply(ev); err != nil {
			t.Fatalf("replicateR17Stream(%s): peer refused envelope %v: %v", siteID, ev.Key(), err)
		}
	}
}
