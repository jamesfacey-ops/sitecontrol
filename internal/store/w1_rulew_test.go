// w1_rulew_test.go — W1-7: "Rule W exact equality from ONE clock reading;
// the `default` invariant is an equality, never a tolerance."
// (WINDOW-BUILD-PLAN-v1 §3 W1, LOCKED 2026-08-27; TP §4.1 rule 3: copy,
// never re-read.)
//
// ⚠ COMPILE-BLOCKED PENDING BUILD (the pair-bearing Author + new fields).
//
// KILLER MUTANT: m2 — a second clock read in rule W. The stepping clock
// below makes every second read visible: two readings can never be equal, so
// OccurredAt := time-of-second-read != AuthoredAt.WallMS and the EQUALITY
// (never a tolerance — a tolerance of even 1ms would let m2 survive on a
// fast box) goes red. A second read through raw time.Now() instead of
// s.Clock fails the same equality against the fixed first reading.
package store

import "testing"

func TestW1RuleWDefaultPathCopiesFromOneClockReadingExactEquality(t *testing.T) {
	s := newTestStore(t)
	var calls int
	s.Clock = func() ClockReading {
		calls++
		// Strictly stepping: reading n and reading n+1 differ by 7777ms, so
		// any two-reading implementation produces a visible inequality.
		return ClockReading{WallMS: 1_700_000_000_000 + int64(calls-1)*7_777, MaxErrorUS: 42, State: ClockSynced}
	}

	// The default path: the officer left the time field untouched. The
	// caller's occurredAtMS echo (999 here — a deliberately wrong value) must
	// be IGNORED: rule W copies from the server's own single reading, never
	// stores the form's echoed default (TP §7.6: the echoed default value is
	// never stored).
	env := w1Author(t, s, "site-a", 1, []byte(`{"type":"tick","k":"a"}`), 999, "default")

	if calls != 1 {
		t.Fatalf("W1-7: Author took %d clock readings; rule W is defined from ONE reading (mutant m2)", calls)
	}
	if env.TimeSource != "default" {
		t.Fatalf("W1-7: default-path envelope carries TimeSource %q, want \"default\"", env.TimeSource)
	}
	// THE invariant — exact equality, never a tolerance.
	if env.OccurredAt != env.AuthoredAt.WallMS {
		t.Fatalf("W1-7: default invariant broken: OccurredAt=%d != AuthoredAt.WallMS=%d — rule W must COPY, not re-read (mutant m2)", env.OccurredAt, env.AuthoredAt.WallMS)
	}
	if env.AuthoredAt.WallMS != 1_700_000_000_000 {
		t.Fatalf("W1-7: AuthoredAt.WallMS=%d, want the injected clock's first reading 1700000000000 — a raw time.Now() bypassed the audit clock", env.AuthoredAt.WallMS)
	}
	if env.OccurredAt == 999 {
		t.Fatal("W1-7: the caller's echoed default value was stored — rule W copies the server's reading, never the form echo (TP §7.6)")
	}

	// The same equality must hold ON THE STORED ROW, not only the returned
	// struct — the row is what replicates and what row_hash covers.
	evs, err := s.Events("site-a", 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Fatalf("want 1 stored event, got %d", len(evs))
	}
	got := evs[0]
	if got.OccurredAt != got.AuthoredAt.WallMS || got.OccurredAt != 1_700_000_000_000 {
		t.Fatalf("W1-7 at rest: stored OccurredAt=%d AuthoredAt.WallMS=%d, want both exactly 1700000000000", got.OccurredAt, got.AuthoredAt.WallMS)
	}
	if got.AuthoredAt.MaxErrorUS != 42 || got.AuthoredAt.State != ClockSynced {
		t.Fatalf("W1-7 at rest: the reading's trust half must ride the same row: MaxErrorUS=%d State=%q, want 42/synced", got.AuthoredAt.MaxErrorUS, got.AuthoredAt.State)
	}
}
