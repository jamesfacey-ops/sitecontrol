// g12_build_repro_test.go — [[G12]] (2026-09-08): reproduces both
// consequences of createTrade's comparison basis (refdata.go, the
// unqualified call) reading BASE rows (dumpSection[Trade](st, "trade"))
// while Trades() and every other trade-name comparator (AmendTrade's Arm 10,
// the gate's add-on-demand resolve step) compare against the COMPOSED list.
//
// A rename layers an amendment on top of the base row; dumpSection never
// sees it. After Welder -> Pipefitter, the base row still reads "Welder".
//
// The fix lands in this same commit. This file is KEPT, not deleted: both
// tests below still assert real post-fix behaviour (the gate no longer
// refuses the pre-rename name; admin create no longer mints a second
// identical heading), so they stand as permanent regression witnesses
// alongside the admin-door witnesses in witness_g12_admin_basis_test.go,
// rather than being subsumed and removed by them. Originally written to
// PRODUCE the failures James's ruling rests on, per this project's standing
// rule that whoever takes a ruling reproduces it first rather than trusting
// an inherited claim. See reports/witness/G12-CREATE-DOOR-BASIS.md and its
// cost-measurement addendum (measure/g12-basis-cost, never merged).
package domain

import "testing"

// TestG12BuildReproGateRefusesTheOldNameAfterARename is consequence 1: a
// LIVE violation of the locked D-66/D-71 criterion (THE GATE NEVER
// REFUSES), pre-dating D-82.
func TestG12BuildReproGateRefusesTheOldNameAfterARename(t *testing.T) {
	st := newDomainTestStore(t)
	welder, err := CreateTrade(st, "site-a", 1, "Welder", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	newName := "Pipefitter"
	if _, err := AmendTrade(st, "site-a", 1, TradeAmendment{TradeID: welder.TradeID, Name: &newName}, 0, "default"); err != nil {
		t.Fatalf("AmendTrade(Welder -> Pipefitter) onto a FREE name = %v, want success", err)
	}

	driver, err := CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = GateCheckIn(st, "site-a", 1, GateCheckInInput{
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK-G12"),
		Driver:    PersonSlot{ExistingID: driver.PersonID},
		Occupants: []PersonSlot{
			{NewName: "G12 Walkin", NewTrade: "Welder"},
		},
	}, 0, "default")
	if err != nil {
		t.Fatalf("G12 REPRODUCED: gate check-in typing the pre-rename name \"Welder\" was REFUSED = %v — D-66/D-71: the gate never refuses", err)
	}
}

// TestG12BuildReproAdminCreateMintsASecondIdenticalHeading is consequence
// 2: no race, one officer, two clicks — POST /trade/create ("Pipefitter")
// after the same rename succeeds because createTrade's base-row scan has no
// Pipefitter row to collide with, minting a second identical muster
// heading (D-30).
func TestG12BuildReproAdminCreateMintsASecondIdenticalHeading(t *testing.T) {
	st := newDomainTestStore(t)
	welder, err := CreateTrade(st, "site-a", 1, "Welder", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	newName := "Pipefitter"
	if _, err := AmendTrade(st, "site-a", 1, TradeAmendment{TradeID: welder.TradeID, Name: &newName}, 0, "default"); err != nil {
		t.Fatalf("AmendTrade(Welder -> Pipefitter) onto a FREE name = %v, want success", err)
	}

	if _, err := CreateTrade(st, "site-a", 1, "Pipefitter", 0, "default"); err == nil {
		trades, terr := Trades(st)
		if terr != nil {
			t.Fatal(terr)
		}
		var hits int
		for _, tr := range trades {
			if tr.Name == "Pipefitter" {
				hits++
			}
		}
		t.Fatalf("G12 REPRODUCED: CreateTrade(\"Pipefitter\") after the rename succeeded (want a refusal) — Trades() now holds %d rows named Pipefitter", hits)
	}
}
