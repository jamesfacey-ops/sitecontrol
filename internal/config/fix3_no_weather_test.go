package config

// fix3_no_weather_test.go — weather phases removed (James, 2026-10-04: "the
// phase conditions can be removed they will not effect site control i
// thought it would effect the speed limit but phase 1 and 2 are already
// going to be 25 MPH so that wont change and phase 3 is no movement anyway
// so go ahead and delete the phase conditions").
//
// What is pinned here:
//   - a config still carrying [[weather.phase]] (or any [weather] table) is
//     REFUSED at Load, by name, with what to do — D-77: a stale key refuses,
//     never guesses;
//   - the old per-phase travel_minutes table is refused the same way, naming
//     the single-number replacement;
//   - every committed ice-road toml carries ONE travel time, 65 minutes, in
//     both directions, and every demo5 toml loads.
//
// ⟦was: config_weather_test.go, config_sparsetable_repro_test.go,
// b6_phase1_struck_test.go and james_20261003_phases_test.go — the phase
// table, its validation, the sparse-table fallback and the governed-post
// rule; all deleted with the feature. loadConfigText was loadWeather.⟧

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const configTextBase = `
site_id = "site-a"
display_name = "Ice Road"
listen_addr = "127.0.0.1:0"
node_epoch = 1
db_path = "x.db"
sync_interval_seconds = 10
login_password = "pw"
orientation = false
site_type = "text_message"
[[peers]]
site_id = "site-b"
base_url = "http://example.invalid"
`

// loadConfigText writes configTextBase plus extra to a temp file and Loads it.
func loadConfigText(t *testing.T, extra string) (*Config, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(configTextBase+extra), 0o644); err != nil {
		t.Fatal(err)
	}
	return Load(p)
}

func TestFix3AConfigCarryingWeatherPhaseTablesIsRefusedByName(t *testing.T) {
	for name, extra := range map[string]string{
		"the shipped shape": `
[[weather.phase]]
phase            = 2
display_name     = "Phase 2 — 25 mph"
dispatch_allowed = true

[[weather.phase]]
phase             = 3
display_name      = "Phase 3 — nobody moves"
dispatch_allowed  = false
review_in_transit = true
`,
		"an empty weather table": "\n[weather]\n",
	} {
		c, err := loadConfigText(t, extra)
		if err == nil {
			t.Fatalf("%s: a config carrying [[weather.phase]] LOADED (%+v) — a stale key must refuse, never be ignored (D-77)", name, c)
		}
		for _, want := range []string{"[[weather.phase]]", "weather phases were removed", "Delete every [[weather.phase]] block"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: refusal %q does not say %q", name, err, want)
			}
		}
	}
}

func TestFix3APerPhaseTravelTableIsRefusedNamingTheSingleNumber(t *testing.T) {
	_, err := loadConfigText(t, `
[[destinations]]
id             = "site-b"
controlled     = true
travel_minutes = { 0 = 65, 2 = 65, 3 = 240 }
`)
	if err == nil {
		t.Fatal("a per-phase travel_minutes table LOADED — it must be refused, never read as some one of its entries")
	}
	for _, want := range []string{"destinations[0] (site-b)", "per-phase table", "travel_minutes = 65"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not say %q", err, want)
		}
	}
}

func TestFix3TravelMinutesIsOneNumberAndNegativeIsRefused(t *testing.T) {
	c, err := loadConfigText(t, `
[[destinations]]
id             = "site-b"
controlled     = true
travel_minutes = 65
`)
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := c.TravelMinutesTo("site-b"); !ok || m != 65 {
		t.Fatalf("TravelMinutesTo(site-b) = %d/%v, want 65", m, ok)
	}
	if _, ok := c.TravelMinutesTo("nowhere"); ok {
		t.Fatal("TravelMinutesTo answered for a destination that is not configured")
	}
	c, err = loadConfigText(t, "\n[[destinations]]\nid = \"site-b\"\ncontrolled = true\n")
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := c.TravelMinutesTo("site-b"); ok {
		t.Fatalf("an omitted travel_minutes answered %d — omitted means no prefill", m)
	}
	if _, err := loadConfigText(t, "\n[[destinations]]\nid = \"site-b\"\ncontrolled = true\ntravel_minutes = -1\n"); err == nil || !strings.Contains(err.Error(), "travel_minutes must be a positive number") {
		t.Fatalf("a negative travel_minutes: err = %v, want the refusal", err)
	}
}
