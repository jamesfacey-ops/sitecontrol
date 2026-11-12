// w1_hash_test.go — W1-1: "Hash covers all five new values (WallMS,
// MaxErrorUS, State, OccurredAt, TimeSource): for each field individually,
// changing it changes Hash()." (WINDOW-BUILD-PLAN-v1 §3 W1, LOCKED
// 2026-08-27; schema-10 §5B: inside Hash(), therefore inside
// event_log.row_hash, therefore inside IntegrityCheck and the C3a seam echo.)
//
// ⚠ COMPILE-BLOCKED PENDING BUILD (the new Envelope fields).
//
// KILLER MUTANT: m1 — remove each new field from Hash(), one at a time; five
// applications, one register row. Each sub-test below is the red for exactly
// one application.
package store

import "testing"

func TestW1HashCoversEachOfTheFiveNewValuesIndividually(t *testing.T) {
	base := func() Envelope {
		return w1Envelope("site-a", 1, 1, []byte(`{"type":"tick","k":"a"}`),
			1_700_000_000_000, 250, ClockSynced, 1_699_000_000_000, "entered")
	}

	// Observation-channel control (standing rule): two identically-built
	// envelopes hash identically, and a change to a v9 field still changes
	// the hash — without this, every "differs" below could be reading noise.
	if base().Hash() != base().Hash() {
		t.Fatal("hash control: two identical envelopes hash differently — Hash() is not deterministic, nothing below is meaningful")
	}
	{
		e := base()
		e.OriginAuth = "site-b"
		if e.Hash() == base().Hash() {
			t.Fatal("hash control: changing a v9 field (OriginAuth) did not change Hash() — the channel is broken")
		}
	}

	t.Run("AuthoredAt.WallMS", func(t *testing.T) {
		e := base()
		e.AuthoredAt.WallMS++ // m1 application 1
		if e.Hash() == base().Hash() {
			t.Fatal("W1-1: AuthoredAt.WallMS is outside Hash() — a rewritten authoring time would not disagree with row_hash (mutant m1)")
		}
	})
	t.Run("AuthoredAt.MaxErrorUS", func(t *testing.T) {
		e := base()
		e.AuthoredAt.MaxErrorUS++ // m1 application 2
		if e.Hash() == base().Hash() {
			t.Fatal("W1-1: AuthoredAt.MaxErrorUS is outside Hash() — the trust bound could be altered without detection (mutant m1)")
		}
	})
	t.Run("AuthoredAt.State", func(t *testing.T) {
		e := base()
		e.AuthoredAt.State = ClockUnsynced // m1 application 3
		if e.Hash() == base().Hash() {
			t.Fatal("W1-1: AuthoredAt.State is outside Hash() — a peer's clock honesty could be laundered to 'synced' without detection (mutant m1; schema-10 §5B.4: trust travels with the time)")
		}
	})
	t.Run("OccurredAt", func(t *testing.T) {
		e := base()
		e.OccurredAt++ // m1 application 4
		if e.Hash() == base().Hash() {
			t.Fatal("W1-1: OccurredAt is outside Hash() — an entered true time could be altered without detection (mutant m1)")
		}
	})
	t.Run("TimeSource", func(t *testing.T) {
		e := base()
		e.TimeSource = "default" // m1 application 5
		if e.Hash() == base().Hash() {
			t.Fatal("W1-1: TimeSource is outside Hash() — a claim could be reclassified as an observation without detection (mutant m1; D-35 obligation 1)")
		}
	})
}
