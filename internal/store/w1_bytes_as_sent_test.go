// w1_bytes_as_sent_test.go — W1-8's behavioural halves: "received bytes
// stored as sent — an unknown time_source value survives write and read
// untouched; peer-applied envelopes never re-stamped (the
// ⟦was: "TestApplyNeverStampsAPeersEvent"⟧ contract carried to the new fields)."
// (WINDOW-BUILD-PLAN-v1 §3 W1, LOCKED 2026-08-27; TP §4.1 rules 1–2;
// PLAN-SEC-A §6: normalise at READ, never at write, for inbound envelopes.)
//
// ⚠ COMPILE-BLOCKED PENDING BUILD (the new Envelope fields).
//
// KILLER MUTANTS: m3 — normalise unknown time_source at write (the
// eventtime.go writeEventTime normalisation was the in-repo pattern; copying
// it here is the mutant); m9 — apply-path re-stamp: a peer envelope's pair
// overwritten from local readings. (m4, the CHECK mutant, is
// w1_schema_shape_test.go's.)
package store

import "testing"

// An unknown time_source survives write and read untouched. This is also
// the pricing property: a third enum value added post-bump must pass through
// old binaries as stored bytes, failing closed only at READ (TP §4.1's
// pricing consequence) — a write-side rewrite would also break row_hash on
// untampered records, since the value sits inside Hash().
func TestW1UnknownTimeSourceSurvivesApplyWriteAndReadUntouched(t *testing.T) {
	s := newTestStore(t)
	s.Clock = w1Reading(1_800_000_000_000, 10, ClockSynced)

	const alien = "postbump-third-value-vNEXT"
	env := w1Envelope("site-p", 1, 1, []byte(`{"type":"tick","k":"a"}`),
		1_700_000_000_000, 9_000_000, ClockUnsynced, 1_690_000_000_000, alien)

	applied, err := s.Apply(env)
	if err != nil {
		t.Fatalf("W1-8: an unknown time_source must never refuse the event (bytes stored as sent): %v", err)
	}
	if !applied {
		t.Fatal("W1-8: envelope with unknown time_source not applied")
	}
	evs, err := s.Events("site-p", 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Fatalf("want 1 stored event, got %d", len(evs))
	}
	if evs[0].TimeSource != alien {
		t.Fatalf("W1-8: stored time_source = %q, want %q byte-identical — write-side normalisation is mutant m3 (and would break IntegrityCheck on honest records)", evs[0].TimeSource, alien)
	}
	if evs[0].OccurredAt != 1_690_000_000_000 {
		t.Fatalf("W1-8: stored OccurredAt = %d, want 1690000000000 as sent", evs[0].OccurredAt)
	}
	// And the row's stored hash still verifies: re-open runs IntegrityCheck
	// over row_hash; a write-side rewrite of any of the five values would
	// make an honest row read as corrupt.
	path := s.Path
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("W1-8: reopen after storing an unknown time_source failed integrity: %v — the value was rewritten between hash and row (mutant m3)", err)
	}
	defer s2.Close()
}

// Peer-applied envelopes are never re-stamped: Apply persists the peer's
// sent time basis and never substitutes local receipt readings — the
// ⟦was: "TestApplyNeverStampsAPeersEvent"⟧ contract inverted onto the new fields
// (PLAN-SEC-A §6), WITH its discriminating precondition asserted: the
// fixture's sent values differ from the local clock at apply time, so
// "stored == sent" cannot pass with the guarantee inverted.
func TestW1ApplyNeverRestampsAPeersTimeBasis(t *testing.T) {
	s := newTestStore(t)
	const localWall = 1_800_000_000_000
	s.Clock = w1Reading(localWall, 5, ClockSynced)

	const (
		peerAuthWall   = 1_700_000_000_111
		peerAuthMaxErr = int64(16_000_000)
		peerOccurred   = 1_650_000_000_222
	)
	// Discriminating precondition (the fixed-clock false-green PLAN-SEC-A
	// names): peer sent-time != local clock at apply time.
	if peerAuthWall == localWall || peerOccurred == localWall {
		t.Fatal("fixture defect: peer times must differ from the local clock or a re-stamp is indistinguishable from fidelity")
	}

	env := w1Envelope("site-p", 1, 1, []byte(`{"type":"tick","k":"b"}`),
		peerAuthWall, peerAuthMaxErr, ClockUnsynced, peerOccurred, "entered")
	if _, err := s.Apply(env); err != nil {
		t.Fatal(err)
	}
	evs, err := s.Events("site-p", 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Fatalf("want 1 stored event, got %d", len(evs))
	}
	got := evs[0]
	if got.AuthoredAt.WallMS != peerAuthWall {
		t.Fatalf("W1-8/m9: stored AuthoredAt.WallMS = %d, want the peer's sent %d — Apply re-stamped a peer's authoring time from the local clock", got.AuthoredAt.WallMS, peerAuthWall)
	}
	if got.AuthoredAt.MaxErrorUS != peerAuthMaxErr || got.AuthoredAt.State != ClockUnsynced {
		t.Fatalf("W1-8/m9: stored trust half (%d, %q), want the peer's honest (%d, unsynced) — B must not launder A's clock confidence (schema-10 §5B.4)", got.AuthoredAt.MaxErrorUS, got.AuthoredAt.State, peerAuthMaxErr)
	}
	if got.OccurredAt != peerOccurred || got.TimeSource != "entered" {
		t.Fatalf("W1-8/m9: stored pair (%d, %q), want the peer's sent (%d, entered)", got.OccurredAt, got.TimeSource, peerOccurred)
	}
	if got.Hash() != env.Hash() {
		t.Fatalf("W1-8/m9: stored envelope hashes %s, sent hashed %s — some value was rewritten in flight", got.Hash(), env.Hash())
	}
}
