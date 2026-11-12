// witness_y13_p12_q3_test.go — witness phase (see reports/witness/_BRIEF.md),
// worktree y13-copy. Kills census survivors P12 and Q3.
//
// P12: internal/domain/checkin.go:338 `if tr.Name != name {` inside
// resolveOrCreateTradeByName, the gate check-in "add person" escape's trade
// resolution step. [[D-71]] obligation 2 is "the escape MUST NOT REFUSE",
// and the comment immediately above the guard names the exact failure this
// witnesses: "CreateTrade refuses a duplicate name ... and a refusal here
// FAILED THE WHOLE CHECK-IN — the mutant (m8-74) names exactly this."
//
// Q3: internal/domain/refdata.go:564 `if name == "" {` inside createTrade —
// the empty-name refusal. Every web caller (handleTradeCreate, the intake
// form) already refuses an empty trade name before reaching this guard, so
// this is a defence-in-depth invariant rather than one an officer can
// currently trip over through the shipped UI — recorded as a MEASURED
// finding in the report, not asserted as an officer-reachable path.
package domain

import (
	"testing"
)

// TestWitnessP12CheckInAddOnDemandTradeResolvesToExistingRatherThanRefusing
// is P12's witness. It lists two trades, "Inspection" and "Welding". An
// officer adding NEW people at the gate and typing those SAME trade names
// into the add-on-demand box must have each resolve silently to its listed
// row — not be told the whole check-in failed because the trade "already
// exists," which is nonsensical to an officer who never asked to CREATE
// one.
//
// ⚠ [[reports/witness/u1b-domain.md finding 🔴1]] MEASURED: with only
// "Inspection" seeded, "the first trade returned by Trades()" and "the trade
// whose name matches" are the SAME trade, so this witness could not tell
// resolveOrCreateTradeByName's name-equality check (checkin.go:338) apart
// from no check at all — a mutant that resolves by POSITION rather than by
// NAME stayed GREEN. Trades() iterates in trade_id order, and trade_id is a
// random hash, not creation order and not name order.
//
// The first repair seeded decoys in a loop until Inspection was provably
// not first, `Fatal`ing after 100. ⚠ [[reports/witness/F0-FLAKE-FIX.md]]
// MEASURED: that escape hatch fired whenever Inspection's random id was the
// minimum of 101 — 7/600 runs — a phantom unlisted red in the redlist
// instrument. The shape now is TWO names resolved to TWO rows: a mutant that
// resolves by any fixed position sends Andy and Bella to the same trade, so
// one of them lands wrong on every run, with no dependence on ordering.
func TestWitnessP12CheckInAddOnDemandTradeResolvesToExistingRatherThanRefusing(t *testing.T) {
	st := newDomainTestStore(t)
	driver, err := CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := CreateTrade(st, "site-a", 1, "Inspection", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	welding, err := CreateTrade(st, "site-a", 1, "Welding", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	tradesBefore, err := Trades(st)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = GateCheckIn(st, "site-a", 1, GateCheckInInput{
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK-14"),
		Driver:    PersonSlot{ExistingID: driver.PersonID},
		Occupants: []PersonSlot{
			{NewName: "Add On Demand Andy", NewTrade: "Inspection"},
			{NewName: "Add On Demand Bella", NewTrade: "Welding"},
		},
	}, 0, "default")
	if err != nil {
		t.Fatalf("check-in adding people on ALREADY-LISTED trades was refused — D-71 obligation 2 "+
			"is \"the escape MUST NOT REFUSE\": %v", err)
	}

	tradesAfter, err := Trades(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(tradesAfter) != len(tradesBefore) {
		t.Errorf("%d trade(s) after the check-in, want %d (Inspection and Welding — 'the resolve half' — should "+
			"author NOTHING, not a second trade_created)", len(tradesAfter), len(tradesBefore))
	}

	persons, err := Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct {
		person string
		trade  Trade
	}{
		{"Add On Demand Andy", inspection},
		{"Add On Demand Bella", welding},
	} {
		var got *Person
		for i := range persons {
			if persons[i].Name == want.person {
				got = &persons[i]
			}
		}
		if got == nil {
			t.Fatalf("%s was not created", want.person)
		}
		if got.TradeID == nil || *got.TradeID != want.trade.TradeID {
			t.Errorf("%s's trade id = %s, want the EXISTING %s trade %q (resolved by NAME — not re-created, and not by position)",
				want.person, d76Deref(got.TradeID), want.trade.Name, want.trade.TradeID)
		}
	}
}

// TestWitnessQ3CreateTradeRefusesAnEmptyName is Q3's witness.
//
// MEASURED (this test file): every web caller of CreateTrade/
// CreateTradeRecordedBy — handleTradeCreate (internal/web/trade.go:220) and
// the intake form's add-on-demand trade (internal/web/intake.go, guarded at
// the `newTrade == ""` case a few lines above its call) — already refuses an
// empty name before this domain guard is ever reached. This witness
// therefore documents a defence-in-depth invariant, not a currently
// officer-reachable path: what it guards against is a nameless trade
// authored by any OTHER or FUTURE caller (a migration, a replication fold,
// a new form) rendering as a blank, unpickable row on every trade list and
// printed muster this site has.
func TestWitnessQ3CreateTradeRefusesAnEmptyName(t *testing.T) {
	st := newDomainTestStore(t)
	wmBefore, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}

	_, err = CreateTrade(st, "site-a", 1, "", 0, "default")
	if err == nil {
		t.Fatal("CreateTrade(\"\") succeeded — a nameless trade can now be authored, and it would " +
			"render as a blank row wherever the trade list is read")
	}
	if err.Error() != "domain: a trade name is required" {
		t.Errorf(`CreateTrade("") error = %q, want "domain: a trade name is required"`, err.Error())
	}

	wmAfter, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if wmAfter != wmBefore {
		t.Fatalf("watermark advanced on a refused creation: before=%d after=%d — refusal must never call Author", wmBefore, wmAfter)
	}
}
