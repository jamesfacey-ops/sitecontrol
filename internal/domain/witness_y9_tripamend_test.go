// witness_y9_tripamend_test.go — witness/y9-tripamend, reports/witness/_BRIEF.md.
//
// internal/domain/trip_amend.go carried 59 unreached branch-blocks (23
// ERR-ARM, 36 LOGIC) and the row-Y census (reports/laptop-pickup/U-row-Y-census.md)
// never probed this file — no plant existed here. This file's five tests are
// witnesses for five plants (Y1-Y5) authored fresh for this pass; the plants
// themselves live in row-y-evidence/plants.py's shape, reported in
// reports/witness/y9-tripamend.md, not committed here.
//
// Y1/Y2/Y3 target the "if err != nil || X == nil { return ..., err }" guards
// on EffectiveTrip, TripAmendmentChain and TripAmendmentTarget. The census
// marks each guard ARM-NOT-TAKEN as a WHOLE: no test exercises even the
// ordinary "no such record" half, which needs no induced SQL failure — a
// trip/key this post has never heard of is enough. Because each function
// dereferences the still-nil pointer a few lines after the guard, bypassing
// it does not misbehave quietly; it panics. Each witness recovers that panic
// into a plain t.Fatalf so the failure text is legible rather than a raw
// runtime trace.
//
// Y4/Y5 target composeTrip's application of an ACCEPTED amendment onto the
// composed record: a vehicle assertion (Y4) and a whitelisted Clear of
// phase_override_reason (Y5). ⟦Y5's witness was deleted with weather phases
// (James, 2026-10-04): phase_override_reason no longer exists, so there is
// no Clear of it to honour.⟧ Both were the "silently accepted, never
// reflected" defect class the assignment brief calls out by name — Y4
// concretely, because internal/domain/in_transit.go:293 reads
// `derefString(t.VehicleEntered)` off exactly this composed field as the
// in-transit muster's vehicle label (D-17: "can we reach this vehicle").
package domain

import (
	"testing"

	"sitecontrol/internal/store"
)

// recoverIntoFatal converts a panic inside fn into a t.Fatalf naming the
// recovered value, so a nil-pointer-dereference witness produces a legible,
// attributable failure line instead of a bare runtime panic trace.
func recoverIntoFatal(t *testing.T, label string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("%s: PANIC: %v (the guard this witnesses was bypassed, and the very next lines dereference the pointer the guard exists to check)", label, r)
		}
	}()
	fn()
}

// TestWitnessY9EffectiveTripReturnsCleanlyForATripThisPostHasNoRecordOf is
// the witness for plant Y1 (trip_amend.go:878,
// `if err != nil || t == nil {`).
//
// EffectiveTrip's own doc comment states the contract: "Returns nil when
// this post has no trip row — a reader cannot show a trip it has never been
// told exists." A trip id nothing has ever dispatched is the ordinary,
// no-error half of that guard, and it is enough to witness it: TripByID
// returns (nil, nil) for an unknown id (oneRow's not-found case), so this
// call needs no seeded fixture and no induced SQL failure at all.
func TestWitnessY9EffectiveTripReturnsCleanlyForATripThisPostHasNoRecordOf(t *testing.T) {
	st := newDomainTestStore(t)

	recoverIntoFatal(t, "EffectiveTrip", func() {
		c, err := EffectiveTrip(st, "site-a-trip-nosuch")
		if err != nil {
			t.Fatalf("EffectiveTrip(unknown trip) = err %v, want nil — TripByID found no row and reported no error", err)
		}
		if c != nil {
			t.Fatalf("EffectiveTrip(unknown trip) = %+v, want nil — the doc comment's own contract: \"a reader cannot show a trip it has never been told exists\"", c)
		}
	})
}

// TestWitnessY9TripAmendmentChainReturnsCleanlyForATripThisPostHasNoRecordOf
// is the witness for plant Y2 (trip_amend.go:1215, the identical guard
// shape inside TripAmendmentChain — the officer-facing "what was this trip
// dispatched as, then every amendment" audit chain, D-5's incident read).
func TestWitnessY9TripAmendmentChainReturnsCleanlyForATripThisPostHasNoRecordOf(t *testing.T) {
	st := newDomainTestStore(t)

	recoverIntoFatal(t, "TripAmendmentChain", func() {
		chain, err := TripAmendmentChain(st, "site-a-trip-nosuch")
		if err != nil {
			t.Fatalf("TripAmendmentChain(unknown trip) = err %v, want nil", err)
		}
		if chain != nil {
			t.Fatalf("TripAmendmentChain(unknown trip) = %+v, want nil — an audit chain for a trip this post has never heard of must not synthesise an entry", chain)
		}
	})
}

// TestWitnessY9TripAmendmentTargetReturnsCleanlyForAKeyWithNoAmendmentRow is
// the witness for plant Y3 (trip_amend.go:1274,
// `if err != nil || row == nil {`).
//
// tripAmendmentAt returns (nil, nil) for a key with no amendment row —
// TripAmendmentTarget's own doc comment: "or \"\" when this post holds no
// amendment row at that key." A key this post was never handed an
// amendment for is the ordinary case, no induced failure needed.
func TestWitnessY9TripAmendmentTargetReturnsCleanlyForAKeyWithNoAmendmentRow(t *testing.T) {
	st := newDomainTestStore(t)
	k := store.Key{SiteID: "site-a", NodeEpoch: 1, Seq: 999}

	recoverIntoFatal(t, "TripAmendmentTarget", func() {
		target, err := TripAmendmentTarget(st, k)
		if err != nil {
			t.Fatalf("TripAmendmentTarget(no such key) = err %v, want nil", err)
		}
		if target != "" {
			t.Fatalf("TripAmendmentTarget(no such key) = %q, want \"\" per the function's own doc comment", target)
		}
	})
}

// TestWitnessY9ComposeTripAppliesAVehicleAmendmentToTheComposedRecord is the
// witness for plant Y4 (trip_amend.go:908, `if r.Vehicle != nil {`).
//
// composeTrip is exercised directly (same package, unexported) rather than
// through the full AmendTrip/EffectiveTrip round trip: composeOccupants
// (called at the end of composeTrip) never touches the store when the input
// rows assert no occupant add/remove, so a bare Trip plus a synthetic
// tripAmendmentRow is the whole fixture and no dispatch/completion
// machinery is needed to isolate this one arm.
//
// THE STAKES: internal/domain/in_transit.go's in-transit muster row reads
//
//	display := derefString(t.VehicleEntered)
//
// off exactly this composed field as the fallback/no-resolvable-vehicle-
// record vehicle label (D-17: the in-transit page's whole job is "can we
// reach this vehicle"). If an accepted vehicle amendment fails to compose,
// the muster keeps showing the dispatch-time vehicle text — a correction
// that was authored and never surfaces, on the one page built to answer
// "which vehicle is this crew on".
func TestWitnessY9ComposeTripAppliesAVehicleAmendmentToTheComposedRecord(t *testing.T) {
	st := newDomainTestStore(t)

	base := Trip{
		TripID:         "site-a-trip-vehicle-amend",
		OriginSite:     "site-a",
		DriverPersonID: "site-a-person-drv-alpha",
		VehicleID:      "site-a-vehicle-original",
		VehicleEntered: y9Str("TRUCK 1 (as dispatched)"),
	}
	const corrected = "TRUCK 9 (corrected by amendment)"
	row := tripAmendmentRow{
		Key:     store.Key{SiteID: "site-a", NodeEpoch: 1, Seq: 1},
		TripID:  base.TripID,
		Vehicle: y9Str(corrected),
	}

	out, err := composeTrip(st, base, nil, []tripAmendmentRow{row})
	if err != nil {
		t.Fatalf("composeTrip: %v", err)
	}
	if out == nil {
		t.Fatalf("composeTrip returned a nil *ComposedTrip for a base trip and one accepted amendment row")
	}
	if out.Trip.VehicleEntered == nil || *out.Trip.VehicleEntered != corrected {
		t.Fatalf("composeTrip did not apply the vehicle amendment: VehicleEntered = %s, want %q.\n"+
			"internal/domain/in_transit.go reads this exact field as the in-transit muster's vehicle label (D-17) — "+
			"an amendment accepted at authoring and not composed here is invisible on the page whose job is "+
			"\"can we reach this vehicle\".", y9Show(out.Trip.VehicleEntered), corrected)
	}
}

func y9Str(s string) *string { return &s }

func y9Show(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
