// w2_f2_apply_clockstate_test.go — W2-F2 (inherited, W1 ledger "OWED TO
// W2", ledger form): a peer envelope carrying an UNKNOWN authored_at_state
// survives Apply untouched, asserted by a test that NAMES CLOCK STATE IN
// ITS FAILURE MESSAGE. The pre-existing incidental kill does NOT discharge
// this: one repl anchor test dies via a seam-echo hash mismatch whose red
// says only "no anchor after two drains" — a check whose failure says
// nothing about clock state cannot be said to test clock-state fidelity,
// and it evaporates if the rig fixtures ever gain valid clock states.
//
// Killed by mutant m6 — the Apply-path normalisation widening (W1-m9's
// twin): an Apply that canonicalises an unrecognised authored_at_state to
// unverified "for consistency with the authoring path". The authoring
// path's normalisation is doc-sanctioned for ITS OWN clock module's
// reading (schema-10 §5B.4 rule 2); a PEER's stored state is that peer's
// claim about ITS clock at authoring, hashed by that peer — rewriting it
// is both a lie and a row_hash break on honest traffic.
// (WINDOW-BUILD-PLAN-v1 §3 W2, criteria LOCKED 2026-08-28.)
package store

import "testing"

func TestW2F2ApplyStoresUnknownAuthoredAtClockStateByteIdentically(t *testing.T) {
	s := newTestStore(t)
	s.Clock = w1Reading(1_800_000_000_000, 5, ClockSynced)

	alienState := ClockState("postbump-clock-state-vNEXT")
	const (
		peerAuthWall = int64(1_700_000_000_111)
		peerMaxErr   = int64(16_000_000)
		peerOccurred = int64(1_650_000_000_222)
	)
	env := w1Envelope("site-p", 1, 1, []byte(`{"type":"tick","k":"w2f2"}`),
		peerAuthWall, peerMaxErr, alienState, peerOccurred, "entered")

	applied, err := s.Apply(env)
	if err != nil {
		t.Fatalf("W2-F2: Apply refused an envelope whose authored_at clock state is the unknown %q — received bytes are stored as sent; an unknown clock state fails closed at READ, never at apply: %v", alienState, err)
	}
	if !applied {
		t.Fatalf("W2-F2: envelope with unknown authored_at clock state %q not applied", alienState)
	}

	evs, err := s.Events("site-p", 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Fatalf("want 1 stored event, got %d", len(evs))
	}
	got := evs[0]
	if got.AuthoredAt.State != alienState {
		t.Fatalf("W2-F2: stored authored_at CLOCK STATE = %q, want the peer's sent CLOCK STATE %q byte-identical — the Apply-path clock-state normalisation widening is mutant m6: a peer's clock-state claim was rewritten on apply", got.AuthoredAt.State, alienState)
	}
	// The rest of the peer's basis rides untouched beside it.
	if got.AuthoredAt.WallMS != peerAuthWall || got.AuthoredAt.MaxErrorUS != peerMaxErr {
		t.Fatalf("W2-F2: stored authoring basis (%d, %d) beside the clock state, want the peer's sent (%d, %d)", got.AuthoredAt.WallMS, got.AuthoredAt.MaxErrorUS, peerAuthWall, peerMaxErr)
	}
	if got.OccurredAt != peerOccurred || got.TimeSource != "entered" {
		t.Fatalf("W2-F2: stored pair (%d, %q), want the peer's sent (%d, entered)", got.OccurredAt, got.TimeSource, peerOccurred)
	}
	if got.Hash() != env.Hash() {
		t.Fatalf("W2-F2: stored envelope hashes %s, sent hashed %s — a clock-state rewrite in flight breaks the digest a second post also holds", got.Hash(), env.Hash())
	}
}
