// shift_starts_test.go — FIX16 G: `shift_starts` is OPTIONAL. The build ships
// before any config line, so an old toml (no key) must still start, and
// sitectl must keep rejecting unknown keys and a malformed list.
package config

import (
	"reflect"
	"strings"
	"testing"
)

const shiftBase = orientationKeyTestBase + "orientation = false\n"

func TestShiftStartsAbsentLoadsAndDefaults(t *testing.T) {
	c, err := loadOrientationKeyTOML(t, shiftBase)
	if err != nil {
		t.Fatalf("an old toml with no shift_starts must load: %v", err)
	}
	mins, configured := c.ShiftStartMinutes()
	if configured || !reflect.DeepEqual(mins, []int{360, 1080}) {
		t.Errorf("default = %v configured=%v, want [360 1080] false", mins, configured)
	}
}

func TestShiftStartsConfiguredIsSortedAndUsed(t *testing.T) {
	c, err := loadOrientationKeyTOML(t, `shift_starts = ["19:30", "07:30"]`+"\n"+shiftBase)
	if err != nil {
		t.Fatal(err)
	}
	mins, configured := c.ShiftStartMinutes()
	if !configured || !reflect.DeepEqual(mins, []int{450, 1170}) {
		t.Errorf("got %v configured=%v, want [450 1170] true", mins, configured)
	}
}

func TestShiftStartsMalformedIsRefusedNamingTheEntry(t *testing.T) {
	for body, want := range map[string]string{
		`shift_starts = ["6am"]`:           "shift_starts[0]",
		`shift_starts = ["06:00","25:00"]`: "shift_starts[1]",
		`shift_starts = ["06:00","06:00"]`: "listed twice",
	} {
		if _, err := loadOrientationKeyTOML(t, body+"\n"+shiftBase); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want it to mention %q", body, err, want)
		}
	}
}

func TestUnknownKeysStillRefusedBesideShiftStarts(t *testing.T) {
	if _, err := loadOrientationKeyTOML(t, shiftBase+"shift_start = [\"06:00\"]\n"); err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Errorf("a typoed shift_start must be an unknown-key refusal, got %v", err)
	}
}
