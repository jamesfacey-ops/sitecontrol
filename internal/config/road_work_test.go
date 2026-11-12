package config

// road_work_test.go — the [road_work] table (James, 2026-10-03): presence is
// what offers the category, the limit defaults to twelve hours, a negative
// limit and a destination named after the form choice are refused.

import (
	"strings"
	"testing"
)

func TestRoadWorkTableOffersTheCategoryWithATwelveHourDefault(t *testing.T) {
	// Rule 6: the base fixture must NOT already carry the table, or the
	// "absent means not offered" half below proves nothing.
	base, err := loadConfigText(t, "")
	if err != nil {
		t.Fatalf("load base: %v", err)
	}
	if base.RoadWorkEnabled() || base.RoadWorkLimitMinutes() != 0 {
		t.Fatalf("precondition: the base fixture must not offer road work (enabled=%v limit=%d)", base.RoadWorkEnabled(), base.RoadWorkLimitMinutes())
	}
	if base.RoadWorkLabel() == "" {
		t.Fatal("RoadWorkLabel must never be empty — a replicated road-work trip is named at every post, offered or not")
	}

	c, err := loadConfigText(t, "\n[road_work]\n")
	if err != nil {
		t.Fatalf("load with a bare [road_work] table: %v", err)
	}
	if !c.RoadWorkEnabled() {
		t.Fatal("a bare [road_work] table must offer the category")
	}
	if got := c.RoadWorkLimitMinutes(); got != 720 {
		t.Fatalf("default limit = %d min, want 720 (twelve hours — James's number)", got)
	}
	if c.RoadWorkLabel() != "Working on the road" {
		t.Fatalf("default label = %q, want \"Working on the road\"", c.RoadWorkLabel())
	}

	c, err = loadConfigText(t, "\n[road_work]\nlimit_minutes = 2\ndisplay_name = \"Working on the ice road\"\n")
	if err != nil {
		t.Fatalf("load with an explicit limit: %v", err)
	}
	if c.RoadWorkLimitMinutes() != 2 || c.RoadWorkLabel() != "Working on the ice road" {
		t.Fatalf("explicit values not honoured: limit=%d label=%q", c.RoadWorkLimitMinutes(), c.RoadWorkLabel())
	}
}

func TestRoadWorkRefusesANegativeLimitAndAReservedDestinationID(t *testing.T) {
	if _, err := loadConfigText(t, "\n[road_work]\nlimit_minutes = -5\n"); err == nil || !strings.Contains(err.Error(), "limit_minutes must be a positive number") {
		t.Fatalf("negative limit: err = %v, want the limit refusal", err)
	}
	_, err := loadConfigText(t, `
[[destinations]]
id = "`+RoadWorkChoice+`"
controlled = false
`)
	if err == nil || !strings.Contains(err.Error(), "reserved for the working-on-the-road choice") {
		t.Fatalf("destination id %q: err = %v, want the reserved-id refusal", RoadWorkChoice, err)
	}
}
