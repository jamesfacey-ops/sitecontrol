// seed.go — the seed fixture (chunk-plan C5 Builds, finding F2: "no seed data
// specified... roster autocomplete and the muster need companies/trades/
// persons on day one"). Fixed, hardcoded data on purpose: this is a demo
// fixture, not a general import tool, and "counted, not eyeballed" (the
// chunk's own done-when wording) requires a fixture whose exact resulting
// counts a test can assert against.
package domain

import (
	"fmt"

	"sitecontrol/internal/store"
)

// SeedCounts is what a fresh store looks like after SeedFixture — the exact
// numbers the done-when's "expected person/company counts" test asserts.
type SeedCounts struct {
	Companies int
	Trades    int
	Persons   int
}

// seedPerson names one fixture row: which company+trade (by index into the
// companies/trades authored just before it) a person defaults to.
//
// ⚠ tradeIdx indexes the SITE-WIDE trade list, not a per-company one. W8's
// trade register is not scoped to a company ⟦DR-W8-2⟧, so a person's company
// and a person's trade are two independent defaults — which is also why two
// companies' people can share one trade here, and do.
type seedPerson struct {
	name       string
	companyIdx int
	tradeIdx   int
}

// SeedFixture authors company/trade/person creation events for a small,
// fixed roster: 3 companies, 6 site-wide trades, 10 persons distributed
// across them. Idempotency is the CALLER's concern (this simply
// authors N events on siteID/nodeEpoch's stream every call) — intended use is
// once, against a fresh store, e.g. at first provisioning or in a test.
func SeedFixture(st *store.Store, siteID string, nodeEpoch int64) (SeedCounts, error) {
	companyNames := []string{"Kakivik Asset Management", "ASRC Energy Services", "Doyon Drilling"}
	// SIX trades, site-wide and unique on name ⟦DR-W8-11⟧ — the same six the
	// fixture has always carried, no longer partitioned by company.
	tradeNames := []string{"Inspection", "Environmental", "Catering", "Camp Services", "Drill Crew", "Roustabouts"}
	people := []seedPerson{
		{"Alice Anderson", 0, 0}, {"Ben Bartlett", 0, 0},
		{"Carla Chen", 0, 1}, {"Dana Douglas", 0, 1},
		{"Ed Espinoza", 1, 2}, {"Fay Foster", 1, 2},
		{"Gil Garrison", 1, 3},
		{"Hana Hunt", 2, 4}, {"Ian Ibarra", 2, 4},
		{"Jo Jacobs", 2, 5},
	}

	companyIDs := make([]string, len(companyNames))
	for i, name := range companyNames {
		c, err := CreateCompany(st, siteID, nodeEpoch, name, 0, "default")
		if err != nil {
			return SeedCounts{}, fmt.Errorf("domain: seed fixture: company %q: %w", name, err)
		}
		companyIDs[i] = c.CompanyID
	}

	tradeIDs := make([]string, len(tradeNames))
	for i, name := range tradeNames {
		w, err := CreateTrade(st, siteID, nodeEpoch, name, 0, "default")
		if err != nil {
			return SeedCounts{}, fmt.Errorf("domain: seed fixture: trade %q: %w", name, err)
		}
		tradeIDs[i] = w.TradeID
	}

	for _, p := range people {
		companyID := companyIDs[p.companyIdx]
		tradeID := tradeIDs[p.tradeIdx]
		if _, err := CreatePerson(st, siteID, nodeEpoch, p.name, &companyID, &tradeID, 0, "default"); err != nil {
			return SeedCounts{}, fmt.Errorf("domain: seed fixture: person %q: %w", p.name, err)
		}
	}

	return SeedCounts{Companies: len(companyNames), Trades: len(tradeNames), Persons: len(people)}, nil
}
