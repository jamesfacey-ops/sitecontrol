// vehicle_nfc_test.go — F12 (campaign finding): NormalizeVehicleID (trip.go)
// casefolded and collapsed whitespace but never Unicode-normalised, so a
// vehicle id containing a combining-character glyph entered in NFC at
// dispatch and the VISUALLY IDENTICAL string entered in NFD at arrival
// failed to correlate — Go string equality is byte-for-byte, and NFC/NFD
// encode the same glyph as different byte sequences. New coverage only;
// TestNormalizeVehicleID and TestNormalisedVehicleFormAppearsNowhereInStorage
// (trip_test.go) are untouched.
package domain

import (
	"bytes"
	"testing"

	"golang.org/x/text/unicode/norm"

	"sitecontrol/internal/store"
)

// TestNormalizeVehicleIDCollapsesNFCAndNFDToTheSameComparisonForm is the
// unit-level half: the two byte-distinct encodings of "TRÜCK-1" must
// normalise identically.
func TestNormalizeVehicleIDCollapsesNFCAndNFDToTheSameComparisonForm(t *testing.T) {
	nfc := norm.NFC.String("TRÜCK-1")
	nfd := norm.NFD.String("TRÜCK-1")
	if nfc == nfd {
		t.Fatalf("test precondition broken: NFC and NFD encodings of %q must differ in bytes", "TRÜCK-1")
	}
	got := NormalizeVehicleID(nfc)
	if want := NormalizeVehicleID(nfd); got != want {
		t.Fatalf("NormalizeVehicleID(NFC) = %q, NormalizeVehicleID(NFD) = %q — must be equal", got, want)
	}
	if got != "trück-1" {
		t.Fatalf("NormalizeVehicleID(%q) = %q, want %q", nfc, got, "trück-1")
	}
}

// TestVehicleCorrelationAcrossUnicodeNFCAndNFD is the end-to-end
// reproduction: dispatch "TRÜCK-1" with its entry stored in NFC, then send
// an unsolicited arrival for the SAME visual string entered in NFD. Before
// the fix this landed `plain` (uncorrelated) and the trip stayed open
// forever; it must now correlate.
func TestVehicleCorrelationAcrossUnicodeNFCAndNFD(t *testing.T) {
	w := newC6World(t)
	dest := "site-b"
	deadlineMin := int64(60)

	nfc := norm.NFC.String("TRÜCK-1")
	nfd := norm.NFD.String("TRÜCK-1")
	if nfc == nfd {
		t.Fatalf("test precondition broken: NFC and NFD encodings of %q must differ in bytes", "TRÜCK-1")
	}

	trip, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ UN-ROUTED. The §47 route below is DISCHARGED and is
		// PRESERVED VERBATIM UNDERNEATH rather than deleted — it is HISTORY, not a live routing.
		// §50 rules that the ARRIVAL half receives the same transformation via the same helper;
		// this pass applied it at this fixture's `RecordUnsolicitedArrival` call, so the arrival now
		// carries a vehicle RECORD id, `vehicleNumberSnapshot` populates `trip_arrival.vehicle_entered`
		// again, and the correlation this fixture asserts is TRUE for the reason it always meant.
		// ⟦was ROUTED — §47 condition 3, 2026-09-03 — DISCHARGED by §50⟧ NOT RE-SEEDED. Re-seeding the TRIP half
		// makes this fixture's arrival assertion FALSE, not merely uncompilable.
		// `W7-4(d)(i)` re-typed `RecordUnsolicitedArrival`'s FOURTH POSITIONAL PARAMETER from
		// the number-as-typed to a vehicle RECORD id WITHOUT changing arity or type
		// (tripauthor.go, its own ⟦was: "…, vehicleEntered, originSite string, …"⟧), so the
		// unsolicited call in this fixture still COMPILES while passing a number.
		// ⚠ MEASURED on production code alone, no test file involved:
		//     number as the 4th arg -> vehicle_id="TRUCK 12"  snapshot=<nil>  disposition=PLAIN
		//     record id as the 4th arg -> vehicle_id="site-b-vehicle-…"  snapshot="TRUCK 12"  CORRELATED
		// `vehicleNumberSnapshot` finds no record for the number, so the arrival's snapshot is
		// NIL and it contributes no matching key (trip.go's own comment does the rest).
		// Repairing the arrival half is ONE root cause across 27 call sites and is the PLAN
		// OWNER's. See reports/window-build/RESEED-164.md §3.7.
		// ⟦was: "VehicleEntered: nfc, DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", nfc), DriverPersonID: w.driver.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &deadlineMin,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w.syncBFromA()
	// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ ⟦was: "arr, err := RecordUnsolicitedArrival(w.b, "site-b", 1, nfd, "site-a","⟧
	arr, err := RecordUnsolicitedArrival(w.b, "site-b", 1, w7SeedVehicleID(t, w.b, "site-b", nfd), "site-a",
		[]string{w.driver.PersonID}, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	replayAllOrders(t, w.events(w.a, "site-a"), w.events(w.b, "site-b"), func(t *testing.T, st *store.Store) {
		d := dispositionOf(t, st, arr.ArrivalID)
		if d.Disposition != ArrivalCorrelated || d.TripID == nil || *d.TripID != trip.TripID {
			t.Fatalf("NFC-dispatched vs NFD-arrived vehicle id failed to correlate: %+v", d)
		}
		// Same storage-verbatim discipline as the ASCII case/whitespace test
		// (trip_test.go): NFC normalisation is comparison-only. Each entry
		// must be stored exactly as typed, in whichever encoding form that
		// was — never rewritten to NFC, NFD, or any other canonical form.
		dump, err := st.DumpProjections()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(dump, []byte(nfc)) {
			t.Fatal("trip's verbatim NFC-entered vehicle id is not in storage")
		}
		if !bytes.Contains(dump, []byte(nfd)) {
			t.Fatal("arrival's verbatim NFD-entered vehicle id is not in storage")
		}
		// Added after the re-test round flagged that this test asserted only
		// that the verbatim forms are PRESENT, never that the comparison form
		// is ABSENT.
		//
		// Honest scope, measured rather than assumed: the two checks above
		// already catch normalisation REPLACING a verbatim value (mutating the
		// trip fold to store NormalizeVehicleID(p.VehicleEntered) fails at the
		// "verbatim is not in storage" assertion, confirmed by running it).
		// What they cannot catch is normalisation appearing ALONGSIDE the
		// verbatim value — e.g. someone later adding a vehicle_norm column to
		// index on, which the performance work makes a plausible next step.
		// That is the shape this assertion defends, and it is defensive: it is
		// NOT currently falsifiable by a minimal mutation, because producing
		// that state needs a schema change rather than a one-line edit.
		if cmp := NormalizeVehicleID(nfc); bytes.Contains(dump, []byte(cmp)) {
			t.Fatalf("normalised comparison form %q leaked into storage", cmp)
		}
	})
}
