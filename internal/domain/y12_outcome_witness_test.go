// y12_outcome_witness_test.go — witness phase, worktree y12-outcome
// (reports/witness/_BRIEF.md). internal/domain/outcome.go carried 25
// unreached blocks that the row-Y census never probed (no prior plant), so
// this file does the missing step first — the plants live at
// C:\Users\james\sc-wt\y12-outcome\plants_y12.py (V1-V12, NOT checked into
// this tree) — then witnesses them here.
//
// Scope: TESTS ONLY. No production code is touched by this file.
package domain

import (
	"testing"

	"sitecontrol/internal/store"
)

// --- fixture machinery: vehicle & trade twins of w3Amend*/w5dAmend* -------

func y12NewVehicle(t *testing.T, st *store.Store, number string) string {
	t.Helper()
	v, err := CreateVehicle(st, "site-a", 1, w3Str(number), nil, 0, "default")
	if err != nil {
		t.Fatalf("fixture: create vehicle %q: %v", number, err)
	}
	return v.VehicleID
}

func y12AmendVehicleLocal(t *testing.T, st *store.Store, clockMS int64, in VehicleAmendment) store.Envelope {
	t.Helper()
	st.Clock = w3Reading(clockMS)
	env, err := AmendVehicle(st, "site-a", 1, in, 0, "default")
	if err != nil {
		t.Fatalf("fixture: amend vehicle %+v refused: %v", in, err)
	}
	return env
}

func y12NewTrade(t *testing.T, st *store.Store, name string) string {
	t.Helper()
	tr, err := CreateTrade(st, "site-a", 1, name, 0, "default")
	if err != nil {
		t.Fatalf("fixture: create trade %q: %v", name, err)
	}
	return tr.TradeID
}

func y12AmendTradeLocal(t *testing.T, st *store.Store, clockMS int64, in TradeAmendment) store.Envelope {
	t.Helper()
	st.Clock = w3Reading(clockMS)
	env, err := AmendTrade(st, "site-a", 1, in, 0, "default")
	if err != nil {
		t.Fatalf("fixture: amend trade %+v refused: %v", in, err)
	}
	return env
}

// --- CarrierFactMatches: the closed/open §4.4 vocabulary predicate -------
//
// CarrierFactMatches has ZERO callers anywhere in this tree (production or
// test) as of this pass — every existing call site asks OccupantFactPersonID
// directly instead. Recorded in the report as an observation, not a defect:
// it is exported and documented as the single source of truth for "does this
// fact belong to this carrier's vocabulary", so it is witnessed here as a
// public API in its own right, independent of whether anything calls it yet.

// TestY12CarrierFactMatchesClosedVocabulary witnesses plant V3
// (outcome.go:299, the closed-vocabulary equality check).
func TestY12CarrierFactMatchesClosedVocabulary(t *testing.T) {
	if !CarrierFactMatches(evtVehicleAmended, "number") {
		t.Fatalf("CarrierFactMatches(%q, %q) = false, want true — %q is in vehicleAmendableFacts, the closed vocabulary", evtVehicleAmended, "number", "number")
	}
	if CarrierFactMatches(evtVehicleAmended, "not_a_real_fact") {
		t.Fatalf("CarrierFactMatches(%q, %q) = true, want false (control)", evtVehicleAmended, "not_a_real_fact")
	}
}

// TestY12CarrierFactMatchesOpenVocabularyPattern witnesses plant V4
// (outcome.go:304, the open/pattern vocabulary check).
func TestY12CarrierFactMatchesOpenVocabularyPattern(t *testing.T) {
	if !CarrierFactMatches(evtTripAmended, "occupant/person-1") {
		t.Fatalf("CarrierFactMatches(%q, %q) = false, want true — occupant/<id> is the trip carrier's OPEN pattern vocabulary (factOccupantPrefix)", evtTripAmended, "occupant/person-1")
	}
}

// TestY12CarrierFactMatchesFailsClosedOnUnregisteredEventType witnesses
// plant V6 (outcome.go:296, the registry-miss default).
func TestY12CarrierFactMatchesFailsClosedOnUnregisteredEventType(t *testing.T) {
	const bogus = "gizmo_amended_y12_never_registered"
	if IsRegisteredCarrier(bogus) {
		t.Fatalf("fixture: %q must not already be a registered carrier", bogus)
	}
	if CarrierFactMatches(bogus, "number") {
		t.Fatalf("CarrierFactMatches(%q, %q) = true, want false — an UNREGISTERED event type must fail CLOSED, never silently claim a fact belongs to a carrier that does not exist", bogus, "number")
	}
}

// TestY12CarrierFactMatchesFailsClosedOnUnmatchedFact witnesses plant V5
// (outcome.go:308, the final fallthrough default).
func TestY12CarrierFactMatchesFailsClosedOnUnmatchedFact(t *testing.T) {
	if CarrierFactMatches(evtVehicleAmended, "totally_bogus_fact_y12") {
		t.Fatalf("CarrierFactMatches(%q, %q) = true, want false — a fact matching neither the closed vocabulary nor any pattern must fail CLOSED", evtVehicleAmended, "totally_bogus_fact_y12")
	}
}

// --- vehicleFactResolutions: the W7 clone, never once run by any test ----

// TestY12VehicleAmendedOutcomeAnswersPositively witnesses plant V1
// (outcome.go:239, AssertedVehicle's closure) and exercises the whole
// FUNC-NEVER-ENTERED vehicleFactResolutions entry path.
func TestY12VehicleAmendedOutcomeAnswersPositively(t *testing.T) {
	if !IsRegisteredCarrier(evtVehicleAmended) {
		t.Fatalf("IsRegisteredCarrier(%q) = false", evtVehicleAmended)
	}
	st := w3NewStore(t)
	vid := y12NewVehicle(t, st, "SEED-0")

	ev := y12AmendVehicleLocal(t, st, w3Base+1*w3Hour, VehicleAmendment{
		VehicleID: vid, Number: w3Str("ABC-123"), Role: w3Str("tractor"),
	})

	outs, err := Outcome(st, ev.Key())
	if err != nil {
		t.Fatalf("Outcome(%v) on a REAL, registered vehicle_amended key errored: %v", ev.Key(), err)
	}
	want := []FactOutcome{
		{Fact: "number", Asserted: "ABC-123", Status: Effective, Winner: ev.Key()},
		{Fact: "role", Asserted: "tractor", Status: Effective, Winner: ev.Key()},
	}
	if len(outs) != len(want) {
		t.Fatalf("Outcome(%v) = %+v, want %+v", ev.Key(), outs, want)
	}
	for i, w := range want {
		if outs[i] != w {
			t.Fatalf("Outcome(%v)[%d] = %+v, want %+v — AssertedVehicle's closure must render the fact's REAL set value, never silently blank it", ev.Key(), i, outs[i], w)
		}
	}
}

// TestY12VehicleOutcomeValueFactContestScopedToTheMergedGroupOnly witnesses
// plant V8 (outcome.go:753, the value-fact merge-scope guard) and
// incidentally plant V2 (VehicleTargetID) since the merge map is keyed off it.
func TestY12VehicleOutcomeValueFactContestScopedToTheMergedGroupOnly(t *testing.T) {
	st := w3NewStore(t)
	x := y12NewVehicle(t, st, "X-SEED")
	y := y12NewVehicle(t, st, "Y-SEED")
	z := y12NewVehicle(t, st, "Z-SEED") // never merged with x or y

	evXNum := y12AmendVehicleLocal(t, st, w3Base+1*w3Hour, VehicleAmendment{VehicleID: x, Number: w3Str("X-1")})
	_ = y12AmendVehicleLocal(t, st, w3Base+2*w3Hour, VehicleAmendment{VehicleID: x, SameAs: w3Str(y)})
	evYNum := y12AmendVehicleLocal(t, st, w3Base+3*w3Hour, VehicleAmendment{VehicleID: y, Number: w3Str("Y-2")})
	// Z's own amendment sorts LAST in ascending key order but Z was never
	// merged with x or y — it must not be able to contaminate their contest.
	_ = y12AmendVehicleLocal(t, st, w3Base+4*w3Hour, VehicleAmendment{VehicleID: z, Number: w3Str("Z-3")})

	// X's OWN "number" amendment (evXNum) is joined into Y's assertion set by
	// the same_as merge at hour 2 (VF §6 case 5 / W3-10): Y's LATER "Y-2"
	// must supersede it.
	outs, err := Outcome(st, evXNum.Key())
	if err != nil {
		t.Fatalf("Outcome(%v): %v", evXNum.Key(), err)
	}
	if len(outs) != 1 || outs[0].Fact != "number" {
		t.Fatalf("Outcome(%v) = %+v, want exactly one 'number' fact", evXNum.Key(), outs)
	}
	if outs[0].Status != Superseded || outs[0].Winner != evYNum.Key() {
		t.Fatalf("Outcome(%v).number = %+v, want {Status:Superseded Winner:%v} — Y's later amendment must win X's number fact because the same_as merge JOINED their assertion sets; Z (never merged) must not be able to win it", evXNum.Key(), outs[0], evYNum.Key())
	}

	comp, err := Competitors(st, evXNum.Key())
	if err != nil {
		t.Fatalf("Competitors(%v): %v", evXNum.Key(), err)
	}
	if len(comp["number"]) != 1 || comp["number"][0] != evYNum.Key() {
		t.Fatalf("Competitors(%v)[\"number\"] = %v, want [%v] — Z must NOT appear: it was never joined into X/Y's merge group", evXNum.Key(), comp["number"], evYNum.Key())
	}
}

// TestY12VehicleSameAsContestScopedToTheSameRecordOnly witnesses plant V7
// (outcome.go:750, the same_as same-record contest guard).
func TestY12VehicleSameAsContestScopedToTheSameRecordOnly(t *testing.T) {
	st := w3NewStore(t)
	x := y12NewVehicle(t, st, "X-SEED2")
	y := y12NewVehicle(t, st, "Y-SEED2")
	z := y12NewVehicle(t, st, "Z-SEED2")

	evXSame := y12AmendVehicleLocal(t, st, w3Base+1*w3Hour, VehicleAmendment{VehicleID: x, SameAs: w3Str(y)})
	// Y's OWN, independent same_as assertion (a different join, Y->Z) sorts
	// AFTER X's — it must never be able to supersede X's own same_as fact:
	// the same_as contest scope is "assertions targeting the SAME record"
	// (x), never the record x merged into.
	_ = y12AmendVehicleLocal(t, st, w3Base+2*w3Hour, VehicleAmendment{VehicleID: y, SameAs: w3Str(z)})

	outs, err := Outcome(st, evXSame.Key())
	if err != nil {
		t.Fatalf("Outcome(%v): %v", evXSame.Key(), err)
	}
	if len(outs) != 1 || outs[0].Fact != "same_as" {
		t.Fatalf("Outcome(%v) = %+v, want exactly one 'same_as' fact", evXSame.Key(), outs)
	}
	if outs[0].Status != Effective || outs[0].Winner != evXSame.Key() || outs[0].Asserted != y {
		t.Fatalf("Outcome(%v).same_as = %+v, want {Asserted:%q Status:Effective Winner:%v} — Y's OWN separate same_as assertion (Y->Z) must not compete for X's same_as fact", evXSame.Key(), outs[0], y, evXSame.Key())
	}
}

// --- Outcome/Competitors dispatch: "registering it here is half the job" -
//
// outcome.go's own comments (⟦W7-10⟧, ⟦W8-14⟧) warn TWICE that a carrier
// missed in one of the two `switch typ` dispatches does not fail loudly — it
// falls to outcomeMiss's SANCTIONED "registered carrier … has no amendment
// record" arm, disguising a real amendment as an absent one. (m8)-style.

// TestY12VehicleOutcomeDispatchDisguisesAsNoAmendmentRecord witnesses plant
// V9 (outcome.go:395, Outcome's vehicle case).
func TestY12VehicleOutcomeDispatchDisguisesAsNoAmendmentRecord(t *testing.T) {
	st := w3NewStore(t)
	vid := y12NewVehicle(t, st, "DISPATCH-SEED")
	ev := y12AmendVehicleLocal(t, st, w3Base+1*w3Hour, VehicleAmendment{VehicleID: vid, Role: w3Str("tanker")})

	outs, err := Outcome(st, ev.Key())
	if err != nil {
		t.Fatalf("Outcome(%v) on a REAL, registered vehicle_amended key errored: %v — the vehicle case must not silently fall through to outcomeMiss's disguised 'has no amendment record' arm", ev.Key(), err)
	}
	if len(outs) != 1 || outs[0].Fact != "role" || outs[0].Asserted != "tanker" || outs[0].Status != Effective || outs[0].Winner != ev.Key() {
		t.Fatalf("Outcome(%v) = %+v, want exactly one Effective 'role' fact won by %v", ev.Key(), outs, ev.Key())
	}
}

// TestY12VehicleCompetitorsDispatchDisguisesAsNoAmendmentRecord witnesses
// plant V10 (outcome.go:692, Competitors' vehicle case).
func TestY12VehicleCompetitorsDispatchDisguisesAsNoAmendmentRecord(t *testing.T) {
	st := w3NewStore(t)
	vid := y12NewVehicle(t, st, "DISPATCH-SEED-2")
	ev := y12AmendVehicleLocal(t, st, w3Base+1*w3Hour, VehicleAmendment{VehicleID: vid, Role: w3Str("tanker")})

	comp, err := Competitors(st, ev.Key())
	if err != nil {
		t.Fatalf("Competitors(%v) on a REAL, registered vehicle_amended key errored: %v", ev.Key(), err)
	}
	if len(comp) != 0 {
		t.Fatalf("Competitors(%v) = %v, want empty map (no other vehicle amendment exists to compete)", ev.Key(), comp)
	}
}

// TestY12TradeOutcomeDispatchDisguisesAsNoAmendmentRecord witnesses plant
// V11 (outcome.go:398, Outcome's trade case) and incidentally reaches the
// AssertedTrade/TradeTargetID closures (outcome.go:270-284) via a real
// tradeFactResolutions call.
func TestY12TradeOutcomeDispatchDisguisesAsNoAmendmentRecord(t *testing.T) {
	st := w3NewStore(t)
	tid := y12NewTrade(t, st, "Y12 Trade Co")
	ev := y12AmendTradeLocal(t, st, w3Base+1*w3Hour, TradeAmendment{TradeID: tid, Name: w3Str("Y12 Trade Co Renamed")})

	outs, err := Outcome(st, ev.Key())
	if err != nil {
		t.Fatalf("Outcome(%v) on a REAL, registered trade_amended key errored: %v — the trade case must not silently fall through to outcomeMiss's disguised 'has no amendment record' arm", ev.Key(), err)
	}
	if len(outs) != 1 || outs[0].Fact != "name" || outs[0].Asserted != "Y12 Trade Co Renamed" || outs[0].Status != Effective || outs[0].Winner != ev.Key() {
		t.Fatalf("Outcome(%v) = %+v, want exactly one Effective 'name' fact won by %v", ev.Key(), outs, ev.Key())
	}
}

// TestY12TradeCompetitorsDispatchDisguisesAsNoAmendmentRecord witnesses
// plant V12 (outcome.go:695, Competitors' trade case).
func TestY12TradeCompetitorsDispatchDisguisesAsNoAmendmentRecord(t *testing.T) {
	st := w3NewStore(t)
	tid := y12NewTrade(t, st, "Y12 Trade Co Two")
	ev := y12AmendTradeLocal(t, st, w3Base+1*w3Hour, TradeAmendment{TradeID: tid, Name: w3Str("Y12 Trade Co Two Renamed")})

	comp, err := Competitors(st, ev.Key())
	if err != nil {
		t.Fatalf("Competitors(%v) on a REAL, registered trade_amended key errored: %v", ev.Key(), err)
	}
	if len(comp) != 0 {
		t.Fatalf("Competitors(%v) = %v, want empty map (no other trade amendment exists to compete)", ev.Key(), comp)
	}
}
