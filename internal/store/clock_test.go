// clock_test.go — the real clock read (clock.go + clock_linux.go /
// clock_notlinux.go). These tests assert only what is true on EVERY
// platform the tree is built or tested on; the point of the build-tagged
// split is that a platform with no adjtimex says "unverified", and that is
// a correct answer rather than a skipped one.
package store

import (
	"testing"
	"time"
)

// SystemClock must always produce a reading in one of the three declared
// states — never the empty string, which is what a half-wired build would
// hand the CHECK constraint, and never a state outside the set.
func TestSystemClockAlwaysReportsADeclaredState(t *testing.T) {
	r := SystemClock()
	switch r.State {
	case ClockSynced, ClockUnsynced, ClockUnverified:
	default:
		t.Fatalf("SystemClock state = %q, outside the declared set", r.State)
	}
	if r.WallMS <= 0 {
		t.Fatalf("SystemClock WallMS = %d, want a real Unix-millisecond reading", r.WallMS)
	}
	// It is a wall-clock read, so it must track time.Now rather than being a
	// constant someone wired in.
	if delta := time.Now().UnixMilli() - r.WallMS; delta < 0 || delta > 60_000 {
		t.Fatalf("SystemClock WallMS is %d ms away from time.Now — that is not this box's wall clock", delta)
	}
}

// The trust half must never claim confidence it did not get. Whatever the
// state, an 'unverified' reading must carry the negative sentinel so no
// "error <= tolerance" comparison anywhere can pass on it by accident —
// passing is the dangerous direction.
func TestUnverifiedReadingsCarryTheNegativeSentinel(t *testing.T) {
	maxErr, state := readClockTrust()
	if state == ClockUnverified && maxErr != MaxErrorUnknown {
		t.Fatalf("unverified reading carries max_error %d, want the %d sentinel", maxErr, MaxErrorUnknown)
	}
	if state != ClockUnverified && maxErr == MaxErrorUnknown {
		t.Fatalf("state %q carries the unknown sentinel; the sentinel must mean exactly 'we could not ask'", state)
	}
}

// A ClockReading is only ever trusted when the kernel actually vouched. This
// is the single check every rendering surface will lean on, so it is pinned
// here rather than left implicit at each call site.
func TestOnlySyncedReadingsAreTrusted(t *testing.T) {
	for _, tc := range []struct {
		state ClockState
		want  bool
	}{
		{ClockSynced, true},
		{ClockUnsynced, false},
		{ClockUnverified, false},
	} {
		got := EventTime{State: tc.state}.Trusted()
		if got != tc.want {
			t.Fatalf("EventTime{State:%q}.Trusted() = %v, want %v", tc.state, got, tc.want)
		}
	}
}
