// vehicle_placeholder_test.go — F43: a check-in may not record a vehicle id
// that is not an identification of a vehicle.
//
// THE DEFECT. web/templates/gate.html shipped the check-in form's vehicle
// field as value="walk-in", pre-filled, with onfocus="this.select()".
// Nothing in this package treated that string as anything but text: it went
// into DispatchInput.VehicleEntered and was stored verbatim as the vehicle
// id. So a check-in where the operator never touched the field recorded a
// pickup as "walk-in" — and the operator who works this post reports there
// ARE no walk-ins: at -30 everyone arrives in a vehicle. The default was
// therefore wrong on 100% of arrivals, and what it silently destroyed is the
// vehicle->people association the muster is read against in an evacuation.
//
// WHY THESE TESTS ARE HERE AND NOT ONLY IN internal/web. The markup fix
// (no default) is necessary and is asserted separately in
// web/gate_vehicle_test.go. It is not sufficient: the failure mode is an
// operator under time pressure entering whatever satisfies the form, and a
// raw POST bypasses the markup entirely. So the RULE is a domain rule, on
// the authoring choke point, and these are its tests.
package domain

import (
	"strings"
	"testing"
)

// placeholdersUnderTest is the fixture list. It is deliberately written out
// here rather than ranged over placeholderVehicleIDs: a test that reads the
// production map cannot fail when an entry is deleted from it, because the
// case simply stops being generated. This list is an INDEPENDENT statement
// of what must be refused.
var placeholdersUnderTest = []string{
	"walk-in",   // the shipped default, verbatim — the defect itself
	"WALK-IN",   // casing: NormalizeVehicleID casefolds, so this is the same answer
	"  walk-in", // leading space, as a hurried entry produces
	"walk in",
	"walkin",
	"n/a",
	"N/A",
	"none",
	"unknown",
	"-",
	"x",
	"?",
	"tbd",
	"vehicle",
}

// TestGateCheckInRefusesAPlaceholderVehicleID is the load-bearing assertion:
// the whole check-in path, end to end, must refuse each of these AND author
// nothing. "Authors nothing" is half the test — a refusal that had already
// written the trip would leave exactly the incomplete-check-in state row 31
// exists to recover, for a request that was never valid.
func TestGateCheckInRefusesAPlaceholderVehicleID(t *testing.T) {
	// ⟦LOCK — §62, 2026-09-03⟧ CLASS R1 — THE GUARD MOVED, SO THE ASSERTION MOVED WITH IT.
	// The ⟦ROUTED⟧ note this supersedes was RIGHT, and is preserved at the call below. Its
	// finding: `W7-25(e)` relocated F43's placeholder refusal to `CreateVehicle`, which
	// `tripauthor.go:35` calls "the ONE place the placeholder list is consulted"; under
	// `W7-4(d)(i)` `GateCheckInInput` carries no vehicle STRING at all, so this door can no
	// longer be handed a placeholder to refuse and the fixture below could not be built.
	//
	// ⚠ THE CRITERION IS UNCHANGED AND THE REFUSAL LITERAL IS PRESERVED BYTE-FOR-BYTE
	// ("does not identify a vehicle", below). Only the LAYER changed: F43's rule is "a
	// placeholder may not be recorded as a vehicle id", and the one moment a vehicle number is
	// typed is now creation. ⚠ THE TEST'S NAME IS LEFT EXACTLY AS IT WAS — it is the record of
	// which door used to carry the guard (§60's bar: never rename away the evidence), and four
	// citations at `reports/window-build/W7-LENS-seams.md:987-990` name it by line.
	//
	// ⚠ THE DOOR'S OWN HALF DID NOT GO UNGUARDED, and it did not weaken. That `GateCheckIn`
	// takes an id and never a string is asserted by
	// `TestW7GateCheckInInputTakesAVehicleIDNotAString` (w7_authoring_doors_test.go:190) — a
	// TYPE-LEVEL guarantee, which is stronger than the runtime refusal it replaces here.
	//
	// ⚠⚠ MEASURED BEFORE THIS EDIT, and it is why this is a load-bearing re-expression rather
	// than a formality: `grep -rn "CreateVehicle" --include=*_test.go internal/` returned NO
	// behavioural assertion that the relocated guard fires. Every hit was either
	// `w7SeedVehicleID`'s routing guard — which its own file labels, in capitals, "NOT AN
	// ASSERTION" — or a ⟦ROUTED⟧ note about the relocation. `W7-25(e)`
	// (internal/store/w7_source_scans_test.go:824) is a SOURCE SCAN and never calls
	// `CreateVehicle`. So until this test the relocation was certified STRUCTURALLY AND NOT
	// BEHAVIOURALLY. §39.4(iv) requires the strike of the w5e placeholder arm and the arrival of
	// its replacement to be ONE ACT; this test is that replacement.
	for _, entered := range placeholdersUnderTest {
		t.Run(entered, func(t *testing.T) {
			st := newDomainTestStore(t)
			// ⟦was: "driver, _ := seedTwoPersons(t, st, \"site-a\")"⟧ — dropped, not
			// forgotten: creating a vehicle record names no person, and a fixture
			// value no assertion reads is how a reader is told something matters
			// when it does not.
			number := entered

			// ⚠ ⟦was ROUTED — §47 condition 3, 2026-09-03 — DISCHARGED by §62 class R1⟧ NOT
			// RE-SEEDED. `entered` ranges over `placeholdersUnderTest`, and CreateVehicle —
			// which is where `W7-25(e)` RELOCATED this very guard — REFUSES every one of them,
			// so the fixture cannot be built by id at all. ⚠ The assertion here has become
			// FALSE, not merely uncompilable: under `W7-4(d)(i)` GateCheckIn takes no number,
			// so it no longer refuses one. That is the PLAN OWNER's.
			// See reports/window-build/RESEED-164.md §3.
			// ⟦was: "_, _, err := GateCheckIn(st, \"site-a\", 1, GateCheckInInput{VehicleEntered: entered, Driver: PersonSlot{ExistingID: driver.PersonID}}, 0, \"default\")"⟧
			_, err := CreateVehicle(st, "site-a", 1, &number, nil, 0, "default")
			if err == nil {
				// ⟦was: "GateCheckIn recorded %q as a vehicle id — a check-in that identifies no vehicle cannot be read against an evacuation muster"⟧
				t.Fatalf("CreateVehicle accepted %q as a vehicle number — a record that identifies no vehicle "+
					"cannot be read against an evacuation muster, and every check-in and dispatch that names "+
					"that record inherits the defect. This is F43's guard at the layer W7-25(e) moved it to", entered)
			}
			// The refusal must name the offending entry, or an operator at
			// -30 has no idea which of the fields on the form is wrong.
			if !strings.Contains(err.Error(), "does not identify a vehicle") {
				t.Errorf("refusal for %q does not explain itself: %v", entered, err)
			}

			// "Authors nothing" is half the test and is PRESERVED at the new layer,
			// not dropped. ⟦was: the same half asserted as "len(trips) != 0" over
			// `Trips(st)` and "len(visits) != 0" over `Visits(st)`⟧ — retired HERE
			// because at this layer no check-in is attempted, so both would read zero
			// for a reason that has nothing to do with the refusal: a check that
			// cannot fail. What CAN fail is a refusal that authored its
			// `vehicle_created` event anyway, which is what this asserts instead.
			vehicles, err := Vehicles(st)
			if err != nil {
				t.Fatal(err)
			}
			if len(vehicles) != 0 {
				t.Fatalf("a refused vehicle creation authored %d record(s) anyway: %+v", len(vehicles), vehicles)
			}
		})
	}
}

// TestGateCheckInStillAcceptsARealVehicleID is the positive control, and it
// is not decoration: without it, IsPlaceholderVehicleID could be mutated to
// `return true` — refusing every check-in at the gate, on the page that
// decides who is on the muster — and the test above would still pass.
func TestGateCheckInStillAcceptsARealVehicleID(t *testing.T) {
	for _, entered := range []string{
		"TRK-14",
		"Truck 12",
		"F350-2",
		"gate walk-in", // an operator's own words, not a default nobody chose
		"NA-7",         // contains a refused token; the WHOLE value is not one
		"BUS 3 (Kakivik)",
	} {
		t.Run(entered, func(t *testing.T) {
			st := newDomainTestStore(t)
			driver, _ := seedTwoPersons(t, st, "site-a")

			// ⟦LOCK — §62, 2026-09-03⟧ CLASS R1 — THE ACCEPTANCE ARM, MOVED WITH ITS SUBJECT.
			// ⚠ THIS IS §62's PRODUCED CONDITION FOR R1 IN PERSON: "show it refusing a
			// placeholder AND accepting a legitimate number, so the check is not vacuously
			// satisfied by refusing everything." The refusing half is the test above; this is
			// the accepting half, and without it `refuseIfPlaceholderNumber` could be mutated
			// to `return err` for every number — refusing every vehicle in the estate — and the
			// test above would still pass.
			//
			// ⚠ `CreateVehicle` IS CALLED DIRECTLY AND NOT THROUGH `w7SeedVehicleID`, on two
			// grounds. §62 bar 2 forbids that helper at an R1 site outright. And the deeper
			// reason the bar exists: the helper answers a refusal with `t.Fatalf` from INSIDE a
			// precondition guard, so routing the acceptance through it would make a helper's
			// guard stand in for this test's own assertion — the acceptance would be asserted
			// by the fixture rather than by the criterion.
			//
			// ⚠ THE END-TO-END HALF IS KEPT, NOT TRADED AWAY. The original assertion — the gate
			// records the number VERBATIM (M8) — is preserved below BYTE-FOR-BYTE against the
			// same door it always named. Only the order changed: the record is created first,
			// because under `W7-4(d)(i)` the check-in names a record instead of typing a number.
			number := entered
			veh, err := CreateVehicle(st, "site-a", 1, &number, nil, 0, "default")
			if err != nil {
				t.Fatalf("CreateVehicle refused the real vehicle number %q: %v — F43's guard must refuse "+
					"the ~40 placeholders and NOTHING ELSE; a guard that refuses a legitimate number stops "+
					"the truck at the gate", entered, err)
			}
			// Stored VERBATIM at the layer that now does the typing, too: the
			// record is what every later surface derives the number from, so a
			// normalisation introduced HERE would reach all of them at once.
			if veh.Number == nil || *veh.Number != entered {
				t.Fatalf("CreateVehicle stored %q for the number %q — the record must hold what was typed",
					derefString(veh.Number), entered)
			}

			// ⚠ ⟦was ROUTED — §47 condition 3, 2026-09-03 — DISCHARGED by §62 class R1⟧ NOT
			// RE-SEEDED, and this one WOULD have transformed mechanically. It is routed anyway
			// because it is the POSITIVE CONTROL for the refusal test above, which is routed: a
			// control re-pointed at a door while the test it controls still names the old one
			// guards nothing. Routed WITH its subject. ⭐ Its subject has now moved to
			// `CreateVehicle` above, so the control follows it there — and the door half below
			// is kept as well.
			// ⟦was: "VehicleEntered: entered,"⟧
			trip, _, err := GateCheckIn(st, "site-a", 1, GateCheckInInput{
				VehicleID: veh.VehicleID,
				Driver:    PersonSlot{ExistingID: driver.PersonID},
			}, 0, "default")
			if err != nil {
				t.Fatalf("GateCheckIn refused the real vehicle id %q: %v", entered, err)
			}
			// Stored VERBATIM (M8) — the placeholder rule compares a
			// normalised form but must not have introduced normalisation
			// into what is written.
			// ⟦LOCK — W7 seam repair, 2026-09-03⟧ ⟦was: "if trip.VehicleEntered != entered {"⟧
			if trip.VehicleEntered == nil || *trip.VehicleEntered != entered {
				t.Fatalf("vehicle stored as %q, want the entered text %q verbatim", derefString(trip.VehicleEntered), entered)
			}
		})
	}
}

// TestPlaceholderVehicleIDIsRefusedOnEveryVehicleBearingAuthoringPath.
// Dispatch is the choke point the gate goes through, but it is not the only
// door: the dispatch console posts straight to Dispatch, and an unsolicited
// arrival carries a vehicle id that IS the §9.2 correlation key. If either
// accepted a placeholder the rule would be defeated by using a different
// form, and the correlation key would collide with every other placeholder
// from the same origin.
func TestPlaceholderVehicleIDIsRefusedOnEveryVehicleBearingAuthoringPath(t *testing.T) {
	st := newDomainTestStore(t)
	// ⟦LOCK — §62, 2026-09-03⟧ ⟦was: "driver, passenger := seedTwoPersons(t, st, \"site-a\")"⟧
	// Both names fell out of use together: the dispatch arm moved to `CreateVehicle`, which
	// names no person, and the arrival arm was retired. ⚠ The CALL is kept and only its results
	// are dropped — it is what makes "site-a" a populated post rather than an empty store, and a
	// refusal measured against an empty store is a weaker fixture than one measured against a
	// post with people on it.
	seedTwoPersons(t, st, "site-a")

	// ⚠⚠ ⟦LOCK — §62, 2026-09-03⟧ THE TWO ARMS OF THIS FUNCTION ARE DIFFERENT CLASSES, AND THAT
	// IS §62 bar 1's NAMED TRAP: "vehicle_placeholder_test.go has an R1 dispatch arm and an R2
	// arrival arm ADJACENT IN THE SAME FUNCTION … a per-file sweep will get them wrong; a
	// per-site pass will not." The dispatch arm below is R1 — RE-EXPRESSED. The arrival arm
	// below it is R2 — RETIRED. Neither disposition was applied to the other.

	// ⚠ ⟦was ROUTED — §47 condition 3, 2026-09-03 — DISCHARGED by §62 class R1⟧ NOT RE-SEEDED.
	// "walk-in" is on the placeholder list, so CreateVehicle refuses it; and Dispatch no longer
	// refuses it either, because the guard MOVED. The assertion is false, not uncompilable.
	// PLAN OWNER's.
	//
	// R1 — RE-EXPRESSED AT THE NEW LAYER, AND COMPOSED SO THE ARM KEEPS ITS OWN SUBJECT. This
	// arm's subject is not "placeholders are refused" (the test above owns that) — it is "the
	// rule cannot be defeated by using a DIFFERENT DOOR", the console posting straight to
	// dispatch. Under `W7-4(d)(i)` the console names a vehicle RECORD, so the only way a
	// placeholder reaches a trip through it is if a record can be created for one. That is what
	// is asserted here, and the door half — that `DispatchInput` takes an id and never a string
	// — is asserted by `TestW7DispatchInputTakesAVehicleIDNotAString`
	// (w7_authoring_doors_test.go:136).
	//
	// ⚠ STATED HONESTLY: this overlaps the test above, which ranges over the whole list
	// including "walk-in". The overlap is the RELOCATION ITSELF — `vehicle.go` says the guard is
	// "consulted ONCE instead of on every trip forever", so two doors that once needed two
	// assertions now share one choke point. It is recorded rather than dressed up as
	// independent coverage.
	// ⟦was: "if _, err := DispatchLocalCheckIn(st, \"site-a\", 1, DispatchInput{VehicleEntered: \"walk-in\", DriverPersonID: driver.PersonID}, 0, \"default\"); err == nil { t.Error(\"Dispatch (the console path) accepted a placeholder vehicle id\") }"⟧
	consoleNumber := "walk-in"
	if _, err := CreateVehicle(st, "site-a", 1, &consoleNumber, nil, 0, "default"); err == nil {
		t.Error("CreateVehicle accepted a placeholder number — the console path names a vehicle RECORD, " +
			"so a placeholder that can be CREATED is a placeholder that can be dispatched, and the " +
			"relocation of F43's guard would be a removal after all ([[D-11]] forbids that in terms)")
	}

	// ⚠ ⟦was ROUTED — §50 arrival re-seed, §47 condition 3 (P1+P5), 2026-09-03⟧ NOT RE-SEEDED, on
	// the SAME two grounds as the dispatch arm above. P1: "walk-in" is on the placeholder list, so
	// `CreateVehicle` REFUSES it and the fixture cannot be built by id at all. P5: this call is
	// asserted to REFUSE, so it authors no arrival — a seed here restores no bind, it only adds an
	// event. ⚠ And the assertion has become FALSE, not uncompilable: the correlation key at this
	// door is now a minted RECORD id, which cannot BE a placeholder, so there is no typed value
	// left here to refuse (tripauthor.go says so in terms). PLAN OWNER's, with its dispatch arm.
	//
	// ⟦LOCK — §62, 2026-09-03⟧ CLASS R2 — RETIRED. THIS IS A COVERAGE REDUCTION AND MUST READ AS
	// ONE (§60 bar 1), so all three things §60 requires are recorded here:
	//
	//   WHAT IT PROVED. That `RecordUnsolicitedArrival` refused a placeholder as the §9.2
	//   correlation key — because a placeholder key "would correlate with every other placeholder
	//   from site-b", binding unrelated arrivals to unrelated trips.
	//
	//   WHICH RULING REMOVED ITS SUBJECT. `W7-4(d)(i)`, which re-typed this door's fourth
	//   positional parameter from the number-as-typed to a vehicle RECORD id
	//   (`tripauthor.go:661`, whose own comment carries the ⟦was:⟧). `tripauthor.go:674-679`
	//   states the consequence in terms: the key "is an id, minted once and unique per record, so
	//   it cannot BE a placeholder".
	//
	//   WHAT PROVES IT INSTEAD, AND WHY THAT IS STRONGER. ⭐ This is NOT a guard that became
	//   false — it is a guard whose INPUT CAN NO LONGER BE EXPRESSED. A runtime refusal became a
	//   TYPE-LEVEL IMPOSSIBILITY, and the fixture cannot be built at all. Two census tests carry
	//   the guarantee: `TestW7DispatchInputTakesAVehicleIDNotAString`
	//   (w7_authoring_doors_test.go:136), which §62 names as the citation for every R2
	//   retirement; and — the one that covers THIS door specifically —
	//   `TestW7RecordUnsolicitedArrivalSignatureTakesAVehicleID` (w7_authoring_doors_test.go:215),
	//   which pins this function's parameter list as an exact ordered literal.
	//
	// ⚠ THE CALL IS PRESERVED, NEVER DELETED. It compiles today — §48's silent break, the shape
	// where the type did not change and the meaning inverted — so it is dead rather than red, and
	// deleting it would leave no trace that it ever existed.
	// ⟦was: "if _, err := RecordUnsolicitedArrival(st, \"site-a\", 1, \"walk-in\", \"site-b\", []string{passenger.PersonID}, nil, 0, \"default\"); err == nil { t.Error(\"RecordUnsolicitedArrival accepted a placeholder as the correlation key — it would correlate with every other placeholder from site-b\") }"⟧
}

// TestEmptyAndPlaceholderVehicleIDsAreRefusedForDistinctStatedReasons keeps
// the two rules separate. Empty was already refused (C6); the placeholder
// rule is new. Collapsing them into one message would lose the thing the
// operator needs to know — that they DID type something and it still is not
// an answer.
func TestEmptyAndPlaceholderVehicleIDsAreRefusedForDistinctStatedReasons(t *testing.T) {
	st := newDomainTestStore(t)
	driver, _ := seedTwoPersons(t, st, "site-a")

	_, err := DispatchLocalCheckIn(st, "site-a", 1, DispatchInput{
		// ⚠ ⟦was ROUTED — §47 condition 3, 2026-09-03 — DISCHARGED by §62, and NOT as class R1⟧
		// NOT RE-SEEDED. Preserving this assertion needs `VehicleID: "   "` — dispatch trims it
		// to "" and raises the same 'required' refusal — but that is a DIFFERENT transformation
		// from the one §47 states, and condition 1 says a site that does not fit the stated form
		// is not transformed.
		//
		// ⟦LOCK — §62, 2026-09-03⟧ ⭐ THE ROUTE WAS RIGHT AND ITS PRESCRIBED READING IS EXACTLY
		// WHAT LANDS HERE: the field is re-pointed and NOTHING ELSE changes.
		//
		// ⚠⚠ THIS SITE IS **NOT** CLASS R1, THOUGH §62's R1 SITE LIST NAMES THE `"  "` ARMS.
		// R1's own heading is "THE GUARD MOVED", and for the EMPTY case it did not.
		// [MEASURED — `tripauthor.go:223-225`] `vehicleID := strings.TrimSpace(in.VehicleID); if
		// vehicleID == "" { return Trip{}, fmt.Errorf("domain: dispatch: vehicle id is
		// required") }` — still at Dispatch — and the comment standing immediately above it
		// reads: "What moves to vehicle creation is the PLACEHOLDER refusal; the names-an-entity
		// refusal STAYS."
		//
		// ⭐ AND THIS IS NOT A NEW JUDGEMENT — §39 (`W7-B14`) ruled these two arms ONE BY ONE,
		// with W7's criteria lock as authority. §39.4(ii): arm 1 is "NOT struck. It is
		// RE-POINTED at the `vehicle_id`, and THE LITERAL IS PRESERVED BYTE-FOR-BYTE." §39.1
		// records that a two-arm form was considered and REJECTED because it "would leave the
		// locked census SHORT BY ONE against the CORRECT build" — a locked criterion red against
		// the right code. Re-expressing this arm at `CreateVehicle` would re-create precisely
		// that error, and would also change what this test DISCRIMINATES: its whole subject is
		// that empty and placeholder are refused FOR DISTINCT STATED REASONS, and at
		// `CreateVehicle` the empty case is refused by a different branch with different words
		// ("a vehicle record needs a number or a role").
		//
		// So: same assertion, same literal, same layer, same window. One line to reverse.
		// ⟦was: "VehicleEntered: \"   \", DriverPersonID: driver.PersonID,"⟧
		VehicleID: "   ", DriverPersonID: driver.PersonID,
	}, 0, "default")
	if err == nil || !strings.Contains(err.Error(), "vehicle id is required") {
		t.Fatalf("blank vehicle id: got %v, want the pre-existing 'required' refusal", err)
	}
	if IsPlaceholderVehicleID("   ") {
		t.Error("whitespace must be refused as EMPTY, not reported as a placeholder — " +
			"the two refusals say different things to the operator")
	}
	if IsPlaceholderVehicleID("") {
		t.Error(`IsPlaceholderVehicleID("") must be false; the empty case belongs to the 'required' rule`)
	}
}
