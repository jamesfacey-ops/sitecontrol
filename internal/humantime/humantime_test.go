package humantime

import (
	"testing"
	"time"
)

// TestAgoPinsFormatAtBoundaries pins the exact wording at the tier
// boundaries used by both C7 (StalenessFace) and C10 (sync-status ages) —
// the two consumers this package exists to unify (see file doc comment).
// A future edit that silently reintroduces divergence (e.g. an abbreviated
// form in one caller) has nowhere to hide: both callers go through here.
func TestAgoPinsFormatAtBoundaries(t *testing.T) {
	cases := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"zero", 0, "0 seconds ago"},
		{"one second", 1 * time.Second, "1 second ago"},
		{"plural seconds", 39 * time.Second, "39 seconds ago"},
		{"just under a minute truncates down, never rounds up", 59*time.Second + 900*time.Millisecond, "59 seconds ago"},
		{"exactly one minute", 60 * time.Second, "1 minute ago"},
		{"plural minutes, the design's own example", 4 * time.Minute, "4 minutes ago"},
		{"just under an hour truncates down", 59*time.Minute + 59*time.Second, "59 minutes ago"},
		{"exactly one hour", time.Hour, "1 hour ago"},
		{"plural hours", 4 * time.Hour, "4 hours ago"},
		{"just under a day truncates down", 23*time.Hour + 59*time.Minute, "23 hours ago"},
		{"exactly one day", 24 * time.Hour, "1 day ago"},
		{"plural days", 3 * 24 * time.Hour, "3 days ago"},
		{"negative clamps to zero, never renders negative", -5 * time.Second, "0 seconds ago"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Ago(tc.d); got != tc.want {
				t.Fatalf("Ago(%s) = %q, want %q", tc.d, got, tc.want)
			}
		})
	}
}
