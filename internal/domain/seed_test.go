// seed_test.go — "a fresh start with the seed fixture yields the expected
// person/company counts (counted, not eyeballed)" (chunk-plan C5 done-when).
// Asserts exact COUNT(*)-equivalent lengths against the seeded store itself,
// not merely the function's own return value — the measurement-discipline
// lesson (n_live_tup reporting 0 where COUNT(*) was 78) applies here too.
package domain

import "testing"

func TestSeedFixtureYieldsExactCounts(t *testing.T) {
	st := newDomainTestStore(t)
	counts, err := SeedFixture(st, "site-a", 1)
	if err != nil {
		t.Fatalf("seed fixture: %v", err)
	}

	if counts.Companies != 3 {
		t.Fatalf("counts.Companies = %d, want 3", counts.Companies)
	}
	if counts.Trades != 6 {
		t.Fatalf("counts.Workgroups = %d, want 6", counts.Trades)
	}
	if counts.Persons != 10 {
		t.Fatalf("counts.Persons = %d, want 10", counts.Persons)
	}

	companies, err := Companies(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(companies) != 3 {
		t.Fatalf("Companies() = %d rows, want 3 (counted against the store, not the function's return value)", len(companies))
	}

	trades, err := Trades(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(trades) != 6 {
		t.Fatalf("Workgroups() = %d rows, want 6", len(trades))
	}

	persons, err := Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(persons) != 10 {
		t.Fatalf("Persons() = %d rows, want 10", len(persons))
	}

	companyIDs := map[string]bool{}
	for _, c := range companies {
		companyIDs[c.CompanyID] = true
	}
	for _, p := range persons {
		if p.CompanyID == nil || !companyIDs[*p.CompanyID] {
			t.Fatalf("person %q has no valid seeded company default: %v", p.Name, p.CompanyID)
		}
	}

	tradeIDs := map[string]bool{}
	for _, w := range trades {
		tradeIDs[w.TradeID] = true
	}
	for _, p := range persons {
		if p.TradeID == nil || !tradeIDs[*p.TradeID] {
			t.Fatalf("person %q has no valid seeded workgroup default: %v", p.Name, p.TradeID)
		}
	}
}
