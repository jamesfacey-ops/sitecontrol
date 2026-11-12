// Package humantime is the ONE relative-time phrasing rule shared by every
// operator-facing surface in this project.
//
// Before this package existed, three surfaces described the same concept
// three different ways: C7's live-alert staleness face used raw
// time.Duration.String() ("4m32s"), C10's sync-status ages used their own
// abbreviated tiering ("4m", "1.2h"), and the design doc's own literal
// example (§8.9) is "4 minutes ago". A gate officer scanning either page
// under pressure — during an evacuation, per the design's own framing —
// should see the same vocabulary for the same idea, not three of them.
// This package exists so there is exactly one place that decision is made.
// Follows the design's phrasing since it is the stated authority.
package humantime

import (
	"fmt"
	"time"
)

// Ago renders a non-negative duration the way the design's own example does
// ("4 minutes ago", §8.9). Negative durations (clock/arithmetic noise) are
// clamped to zero — never rendered as a negative age. Truncates toward the
// coarser unit (39 seconds stays "39 seconds ago", never rounds up into
// "1 minute ago") so the displayed number and the underlying magnitude never
// disagree at a boundary.
func Ago(d time.Duration) string {
	return Span(d) + " ago"
}

// Span is Ago's magnitude without the " ago": "4 minutes", "2 hours". The
// same clamp and the same truncation, so "by 25 minutes" and "25 minutes
// ago" can never disagree about one figure. For a sentence that is not an
// age — "past its allowance by 25 minutes", "2 hours AHEAD of this post's
// clock" — where Ago's " ago" made the sentence false (FINDINGS-R4 D13's
// "by 25 minutes ago").
func Span(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return unit(int(d/time.Second), "second")
	case d < time.Hour:
		return unit(int(d/time.Minute), "minute")
	case d < 24*time.Hour:
		return unit(int(d/time.Hour), "hour")
	default:
		return unit(int(d/(24*time.Hour)), "day")
	}
}

func unit(n int, name string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", name)
	}
	return fmt.Sprintf("%d %ss", n, name)
}
