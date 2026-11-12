// w2_f1_author_fidelity_test.go — W2-F1 (inherited from W1's acceptance,
// reports/window-build/W1-falsification.md "OWED TO W2", stated in the
// ledger's exact form): an event authored through Store.Author with a
// time_source OUTSIDE the known set is stored and read back
// byte-identically, asserted on the returned envelope AND the stored row.
//
// log.go:37–38's own sentence — "no write-side normalisation, ever" — was
// untested on the exact path it annotates: nothing anywhere authored an
// unknown source, so the Author-path timeSource normalisation switch
// (mutant m5, W1-m3's authoring twin) was MEASURED TO SURVIVE the entire
// suite, three independent times (the mutant gate's Probe B, the build
// critique's scratch-copy repeat, the round-1 falsifiability audit). W2 is
// the chunk that makes the hole live: real capture means a caller CAN
// supply an unknown source. This criterion's failure mode is pre-produced;
// the ledger RED row cites W1's file rather than re-earning it.
//
// The ClockState normalisation at log.go:70–74 is the doc-sanctioned
// EXCEPTION (schema-10 §5B.4 rule 2) and a DIFFERENT fact: reading.State
// is a trusted clock-module value canonicalised at source; time_source is
// a caller's recorded claim whose bytes ARE the fact.
// (WINDOW-BUILD-PLAN-v1 §3 W2, criteria LOCKED 2026-08-28.)
package store

import "testing"

func TestW2F1AuthorPathStoresUnknownTimeSourceByteIdentically(t *testing.T) {
	s := newTestStore(t)
	s.Clock = w1Reading(1_800_000_000_000, 42, ClockSynced)

	const alien = "postbump-third-value-vNEXT"
	const occ = int64(1_234_567_890_123)
	env, err := s.Author("site-a", 1, []byte(`{"type":"tick","k":"w2f1"}`), occ, alien)
	if err != nil {
		t.Fatalf("W2-F1: Author must never refuse an unknown time_source (no write-side normalisation and no write-side vetting — the read side is where honesty happens): %v", err)
	}

	// The RETURNED envelope.
	if env.TimeSource != alien {
		t.Fatalf("W2-F1: returned envelope carries time_source=%q, want %q byte-identical — the Author-path normalisation switch is mutant m5, measured to survive the whole pre-W2 suite (W1 ledger, OWED TO W2)", env.TimeSource, alien)
	}
	if env.OccurredAt != occ {
		t.Fatalf("W2-F1: returned envelope OccurredAt=%d, want %d — a non-\"default\" source stores occurredAtMS byte-exactly (log.go:37–38)", env.OccurredAt, occ)
	}

	// The STORED row.
	evs, err := s.Events("site-a", 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Fatalf("want 1 stored event, got %d", len(evs))
	}
	got := evs[0]
	if got.TimeSource != alien {
		t.Fatalf("W2-F1: STORED row carries time_source=%q, want %q byte-identical — write-side normalisation between return and row is mutant m5's other landing spot (and would break row_hash on honest records: the value sits inside Hash())", got.TimeSource, alien)
	}
	if got.OccurredAt != occ {
		t.Fatalf("W2-F1: stored OccurredAt=%d, want %d as authored", got.OccurredAt, occ)
	}

	// And the row's stored hash still verifies on reopen — a rewrite
	// between hash and row reads as corruption on an honest record.
	path := s.Path
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("W2-F1: reopen after authoring an unknown time_source failed integrity: %v — the value was rewritten between hash and row (mutant m5)", err)
	}
	defer s2.Close()
}
