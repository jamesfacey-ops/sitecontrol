// witness_d76_trade_fold_test.go — [[D-76]] (2026-09-05): "Welder" and
// "welder" are ONE trade, at BOTH doors.
//
// A trade is a heading on the evacuation muster sheet ([[D-30]]), read aloud
// during a muster; two spellings split one crew across two headings. Before
// this ruling `createTrade`'s duplicate check (refdata.go) and the gate's
// resolve step `resolveOrCreateTradeByName` (checkin.go) both compared names
// EXACTLY, so a case- or padding-variant retype minted a second heading
// silently through either door. Both now key on ONE rule, sameTradeName —
// case and leading/trailing whitespace fold — so no door can mint a second
// spelling.
//
// ⭐ [[D-66]] is the constraint that must not break: THE GATE NEVER REFUSES.
// The fold is D-66 working better, not weaker — MORE inputs resolve to the
// existing trade instead of minting — and the two gate tests below are the
// proof. MEASURED while building this file: folding ONLY createTrade turned
// the gate's silent mint into a REFUSAL (the check-in failed with the
// duplicate error), which is exactly the D-66 break the ruling warns of;
// folding the resolve step too is what closes it. That intermediate failure
// is recorded in reports/witness/d76-trade-fold.md.
package domain

import (
	"errors"
	"fmt"
	"testing"

	"sitecontrol/internal/store"
)

// d76SeedTradePair lists TWO trades, distinct under the fold, and returns
// both plus the list length. The gate tests then resolve a retype of EACH
// and assert each person landed on their OWN row.
//
// Why two, and why not "seed decoys until `name` is not Trades()[0]" as this
// helper did until F0 (reports/witness/F0-FLAKE-FIX.md): Trades() orders by
// trade_id, which is 64 bits of crypto/rand (ids.go), so with ONE trade
// "the first" and "the one whose name matches" are the same row and a
// resolve-by-POSITION mutant stays green (the u1b-domain 🔴1 finding). The
// decoy loop repaired that, but its escape hatch — `Fatalf` after 100
// decoys — was reachable whenever the target's random id was the minimum
// of 101: MEASURED 8/1,200 executions, a phantom unlisted red in the redlist
// instrument about once in 29 full-suite runs. Raising the cap only moves
// that probability; it never reaches zero.
//
// Two names resolved to two rows needs no ordering at all: a mutant that
// returns Trades()[0] (or the last row, or any fixed position) sends BOTH
// people to the same trade, so at least one lands wrong, every run.
func d76SeedTradePair(t *testing.T, st *store.Store, nameA, nameB string) (Trade, Trade, int) {
	t.Helper()
	a, err := CreateTrade(st, "site-a", 1, nameA, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	b, err := CreateTrade(st, "site-a", 1, nameB, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	trades, err := Trades(st)
	if err != nil {
		t.Fatal(err)
	}
	return a, b, len(trades)
}

// d76AssertLandedOn finds the person named `person` and asserts their trade
// is exactly `want` — by identity, not name or count. It Fatals if the
// person was never created.
func d76AssertLandedOn(t *testing.T, st *store.Store, person string, want Trade) {
	t.Helper()
	persons, err := Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	for i := range persons {
		if persons[i].Name != person {
			continue
		}
		if persons[i].TradeID == nil || *persons[i].TradeID != want.TradeID {
			t.Errorf("%s's trade id = %s, want the EXISTING %s row %q (resolved by folded NAME — a position-based resolve puts both people on one row)", person, d76Deref(persons[i].TradeID), want.Name, want.TradeID)
		}
		return
	}
	t.Fatalf("%s was not created", person)
}

// d76Deref prints a nullable id for a failure message.
func d76Deref(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%q", *p)
}

// d76TradeNamed returns the one trade whose CURRENT name is exactly `name`,
// failing if there are zero or several — identity, not count, is what every
// assertion below rests on.
func d76TradeNamed(t *testing.T, st *store.Store, name string) Trade {
	t.Helper()
	trades, err := Trades(st)
	if err != nil {
		t.Fatal(err)
	}
	var hits []Trade
	for _, tr := range trades {
		if tr.Name == name {
			hits = append(hits, tr)
		}
	}
	if len(hits) != 1 {
		t.Fatalf("%d trades named exactly %q, want exactly 1: %+v", len(hits), name, hits)
	}
	return hits[0]
}

// --- door: createTrade, the domain check both web doors rest on -----------

// TestD76CreateTradeRefusesACaseVariantOfAListedNameAndAuthorsNothing is the
// ruling's first half: "Welder" then "welder" is REFUSED, the refusal is a
// TYPED *TradeCreateRefusal naming the on-list spelling, and nothing is
// authored — the watermark does not move and the one Welder row is the same
// row it was.
//
// CORRECTED CITATION (2026-09-06, evidence-auditor): never literally
// producible — this file names *TradeCreateRefusal, which does not exist
// pre-D-76, so it cannot compile there, let alone "produce" a runtime
// result. The underlying claim is nonetheless TRUE: MEASURED via a minimal
// probe (no reference to *TradeCreateRefusal) run on pre-fix ancestor
// `42f45b8`: `CreateTrade("welder") after "Welder" = <nil>` — the second
// spelling was minted as its own row (2 rows total). See
// reports/witness/CITATION-AUDIT.md row 6.
func TestD76CreateTradeRefusesACaseVariantOfAListedNameAndAuthorsNothing(t *testing.T) {
	st := newDomainTestStore(t)
	welder, err := CreateTrade(st, "site-a", 1, "Welder", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	wmBefore, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}

	_, err = CreateTrade(st, "site-a", 1, "welder", 0, "default")
	var refusal *TradeCreateRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("CreateTrade(\"welder\") after \"Welder\" = %v, want a *TradeCreateRefusal (D-76: one trade, two spellings)", err)
	}
	if refusal.Existing != "Welder" {
		t.Errorf("refusal.Existing = %q, want the ON-LIST spelling \"Welder\" — the row the officer should pick", refusal.Existing)
	}
	if refusal.Typed != "welder" {
		t.Errorf("refusal.Typed = %q, want what was typed, \"welder\"", refusal.Typed)
	}

	wmAfter, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if wmAfter != wmBefore {
		t.Errorf("watermark advanced on a refused creation: %d -> %d — a refusal never calls Author", wmBefore, wmAfter)
	}
	trades, err := Trades(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(trades) != 1 || trades[0].TradeID != welder.TradeID {
		t.Errorf("trades after the refused retype = %+v, want exactly the original Welder row %q", trades, welder.TradeID)
	}
}

// TestD76CreateTradeRefusesPaddedAndUpperCasedRetypes covers the whitespace
// half of the ruling and the case half at its extremes. Every web door trims
// before calling the domain, so the padded rows here are the domain's OWN
// guarantee — a future caller that forgets to trim is still refused.
//
// CORRECTED CITATION (2026-09-06, evidence-auditor): same compile-impossible
// shape as line 124's citation above. MEASURED via a minimal probe (no
// reference to *TradeCreateRefusal) run on pre-fix ancestor `42f45b8`: all
// four retypes ("  Welder  ", "WELDER", " wElDeR", "welder\t") returned
// <nil>, and each was minted as its own row — 5 total rows (1 original +
// 4 extra Welder-spellings), exact match. See
// reports/witness/CITATION-AUDIT.md row 7.
func TestD76CreateTradeRefusesPaddedAndUpperCasedRetypes(t *testing.T) {
	st := newDomainTestStore(t)
	welder, err := CreateTrade(st, "site-a", 1, "Welder", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	for _, retype := range []string{"  Welder  ", "WELDER", " wElDeR", "welder\t"} {
		wmBefore, err := st.Watermark("site-a", 1)
		if err != nil {
			t.Fatal(err)
		}
		_, err = CreateTrade(st, "site-a", 1, retype, 0, "default")
		var refusal *TradeCreateRefusal
		if !errors.As(err, &refusal) {
			t.Errorf("CreateTrade(%q) after \"Welder\" = %v, want a *TradeCreateRefusal", retype, err)
			continue
		}
		if refusal.Existing != "Welder" {
			t.Errorf("CreateTrade(%q): refusal.Existing = %q, want \"Welder\"", retype, refusal.Existing)
		}
		wmAfter, err := st.Watermark("site-a", 1)
		if err != nil {
			t.Fatal(err)
		}
		if wmAfter != wmBefore {
			t.Errorf("CreateTrade(%q): watermark advanced %d -> %d on a refusal", retype, wmBefore, wmAfter)
		}
	}
	trades, err := Trades(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(trades) != 1 || trades[0].TradeID != welder.TradeID {
		t.Errorf("trades after four refused retypes = %+v, want exactly the original Welder row %q", trades, welder.TradeID)
	}
}

// TestD76CreateTradeStillMintsAGenuinelyDifferentName is the positive
// control: a fold that refused everything would pass the two tests above.
// "Welders" is not "Welder" under any spelling of the rule.
func TestD76CreateTradeStillMintsAGenuinelyDifferentName(t *testing.T) {
	st := newDomainTestStore(t)
	if _, err := CreateTrade(st, "site-a", 1, "Welder", 0, "default"); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateTrade(st, "site-a", 1, "Welders", 0, "default"); err != nil {
		t.Fatalf("CreateTrade(\"Welders\") after \"Welder\" = %v, want accepted — a different name still mints", err)
	}
	d76TradeNamed(t, st, "Welder")
	d76TradeNamed(t, st, "Welders")
}

// --- door: the gate's add-it escape (checkin.go) — D-66 -------------------

// TestD76GateAddItEscapeResolvesACaseVariantToTheExistingTradeSilently is
// THE assertion this ruling turns on. An officer at 04:10 types "welder" into
// the add-on-demand box while "Welder" is on the list. [[D-66]]: the gate
// never refuses. The check-in must SUCCEED, author NO new trade, and tag the
// person to the EXISTING Welder row — resolved, not re-created, not refused.
// A second walk-in types "electrician" against a listed "Electrician" in the
// same check-in, so that each landing on its OWN row can only be a NAME
// match (see d76SeedTradePair).
//
// CORRECTED CITATION (2026-09-06, evidence-auditor): never literally
// producible — this function itself references no new type, but Go
// compiles per-package, and two other functions in this same file DO
// reference *TradeCreateRefusal, so the whole package fails to build on any
// pre-D-76 tree regardless (file-level, not just line-level, impossibility).
// The underlying claim is nonetheless TRUE: MEASURED by evidence-auditor
// (this pass, not inherited) on pre-fix ancestor `42f45b8` by stripping the
// two type-referencing functions plus the sameTradeName-using one, re-vetting
// clean, and running this test alone: the check-in succeeded but
// `trades after=4,want 2`, both walk-ins minted fresh rows — exact match
// including names. See reports/witness/CITATION-AUDIT.md row 8.
func TestD76GateAddItEscapeResolvesACaseVariantToTheExistingTradeSilently(t *testing.T) {
	st := newDomainTestStore(t)
	driver, err := CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	welder, electrician, before := d76SeedTradePair(t, st, "Welder", "Electrician")

	_, _, err = GateCheckIn(st, "site-a", 1, GateCheckInInput{
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK-14"),
		Driver:    PersonSlot{ExistingID: driver.PersonID},
		Occupants: []PersonSlot{
			{NewName: "Add On Demand Andy", NewTrade: "welder"},
			{NewName: "Add On Demand Ellie", NewTrade: "electrician"},
		},
	}, 0, "default")
	if err != nil {
		t.Fatalf("gate check-in typing \"welder\" and \"electrician\" while \"Welder\" and \"Electrician\" are listed was REFUSED — D-66: the gate never refuses; the fold must RESOLVE, never refuse: %v", err)
	}

	after, err := Trades(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != before {
		t.Errorf("trades after the check-in = %d, want %d — both retypes must resolve to their listed row and author NOTHING, not mint a second heading", len(after), before)
	}
	d76AssertLandedOn(t, st, "Add On Demand Andy", welder)
	d76AssertLandedOn(t, st, "Add On Demand Ellie", electrician)
}

// TestD76GateAddItEscapeResolvesAPaddedUpperCasedRetypeToTheExistingTrade is
// the whitespace half at the gate. web/gate.go trims before building the
// slot, so this is the DOMAIN's own guarantee for the padding — the rule
// lives in one place, and a caller that forgets to trim gets the same answer.
func TestD76GateAddItEscapeResolvesAPaddedUpperCasedRetypeToTheExistingTrade(t *testing.T) {
	st := newDomainTestStore(t)
	driver, err := CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	welder, electrician, before := d76SeedTradePair(t, st, "Welder", "Electrician")

	_, _, err = GateCheckIn(st, "site-a", 1, GateCheckInInput{
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK-15"),
		Driver:    PersonSlot{ExistingID: driver.PersonID},
		Occupants: []PersonSlot{
			{NewName: "Padded Pat", NewTrade: "  WELDER  "},
			{NewName: "Padded Quinn", NewTrade: "\tELECTRICIAN "},
		},
	}, 0, "default")
	if err != nil {
		t.Fatalf("gate check-in typing \"  WELDER  \" and \"\\tELECTRICIAN \" while \"Welder\" and \"Electrician\" are listed was REFUSED — D-66: %v", err)
	}
	after, err := Trades(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != before {
		t.Errorf("trades after the check-in = %d, want %d — a padded retype must resolve, not mint", len(after), before)
	}
	d76AssertLandedOn(t, st, "Padded Pat", welder)
	d76AssertLandedOn(t, st, "Padded Quinn", electrician)
}

// --- door: the seed fixture ------------------------------------------------

// TestD76SeedFixtureTradeNamesArePairwiseDistinctUnderTheFold pins the fourth
// door the D-76 census found. SeedFixture mints its six trades through
// CreateTrade, so under the fold it can only succeed while its names are
// pairwise distinct case-insensitively — this test is the tripwire for the
// day someone adds "camp services" beside "Camp Services" to the fixture.
//
// CORRECTED CITATION (2026-09-06, evidence-auditor): CITATION-AUDIT.md row 9
// grouped this with the impossible CLEAN-TREE citations above (same package
// fails to build on pre-D-76 ancestor `42f45b8`), but that basis does not
// actually apply here — unlike those, this comment never claims a clean-tree
// result; it describes a mutant planted on the CURRENT (post-D-76) tree,
// which is what actually exists to run it against. Re-probed directly by me
// this pass: appending "inspection" to seed.go's tradeNames on integration
// tip `11912c1` and running this test produced, verbatim:
// `SeedFixture = domain: seed fixture: trade "inspection": domain: the
// trade "inspection" is already on this site's list (refused: trade names
// are unique site-wide); pick it from the list instead of adding it again,
// want success — a fixture name collides with another under the D-76 fold`
// — exact match. Mutation reverted; `git status --porcelain` and `go vet`
// both clean afterward.
func TestD76SeedFixtureTradeNamesArePairwiseDistinctUnderTheFold(t *testing.T) {
	st := newDomainTestStore(t)
	counts, err := SeedFixture(st, "site-a", 1)
	if err != nil {
		t.Fatalf("SeedFixture = %v, want success — a fixture name collides with another under the D-76 fold", err)
	}
	trades, err := Trades(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(trades) != counts.Trades {
		t.Fatalf("Trades() = %d rows, SeedFixture reported %d", len(trades), counts.Trades)
	}
	for i := range trades {
		for j := i + 1; j < len(trades); j++ {
			if sameTradeName(trades[i].Name, trades[j].Name) {
				t.Errorf("seed trades %q and %q are one trade under the fold — two headings for one crew", trades[i].Name, trades[j].Name)
			}
		}
	}
}
