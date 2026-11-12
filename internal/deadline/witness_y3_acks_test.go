// witness_y3_acks_test.go — the witness phase (reports/witness/_BRIEF.md),
// worktree y3-acks, killing four of the U-row-Y census's 34 survivors that
// live in this file: P2, P3, P4 (internal/deadline/unack.go). ⟦was: "P1, P2,
// P3, P4" — P1 (EffectiveAcks scope separation) went with the weather-phase
// ack scope, James 2026-10-04⟧ See
// reports/witness/y3-acks.md for the run-both-ways record (FAILS planted,
// PASSES clean) and reports/laptop-pickup/U-row-Y-census.md for the census
// itself. Plants are defined in row-y-evidence/plants.py.
//
// Each test names its own plant and, in its own comment, the exact
// behaviour the plant destroys — so a reader meets the failure mode before
// the assertion that proves it.
package deadline

import (
	"testing"
)

// TestWitnessP2RetractAcksRefusesAnEmptyTargetSet — P2
// (internal/deadline/unack.go:280):
// `if len(targets) == 0 { return fmt.Errorf(...) }`.
//
// PLANT: `if false && len(targets) == 0` — the refusal never fires. The
// function's own comment states exactly why this guard exists standalone,
// independent of the web handler's own check: "a caller that skipped the
// check cannot turn a refusal into a silent success." This test IS that
// caller.
func TestWitnessP2RetractAcksRefusesAnEmptyTargetSet(t *testing.T) {
	st := newSite(t)
	before := w9cLogCount(t, st)

	err := RetractAcks(st, here, 1, nil, nil, 0, "default")
	if err == nil {
		t.Fatalf("RetractAcks(nil targets) = <nil error>, want a refusal")
	}
	const want = "deadline: retract ack: no acknowledgment to retract"
	if err.Error() != want {
		t.Fatalf("RetractAcks(nil targets) error = %q, want %q", err.Error(), want)
	}
	if got := w9cLogCount(t, st); got != before {
		t.Fatalf("RetractAcks(nil targets) authored %d event(s) despite refusing — a refusal must author NOTHING", got-before)
	}
}

// TestWitnessP3EffectiveAcksRefusesAnEmptyTripID — P3
// (internal/deadline/unack.go:226): `if tripID == "" { return nil,
// fmt.Errorf(...) }`.
//
// PLANT: `if false && tripID == ""` — the refusal never fires. An empty
// trip id then falls through into the dump query, which matches no rows
// (no ack is ever authored against trip_id ""), so the caller silently
// gets an empty, ERROR-FREE target set back instead of being told its
// input was malformed.
func TestWitnessP3EffectiveAcksRefusesAnEmptyTripID(t *testing.T) {
	st := newSite(t)
	_, err := EffectiveAcks(st, "")
	if err == nil {
		t.Fatalf(`EffectiveAcks("") = <nil error>, want a refusal`)
	}
	const want = "deadline: effective acks: trip id is required"
	if err.Error() != want {
		t.Fatalf(`EffectiveAcks("") error = %q, want %q`, err.Error(), want)
	}
}

// TestWitnessP4SortAckKeysOrdersByEpochBeforeSeq — P4
// (internal/deadline/unack.go:261): `if ks[i].NodeEpoch != ks[j].NodeEpoch {
// return ks[i].NodeEpoch < ks[j].NodeEpoch }`, the tiebreak between SiteID
// and Seq in sortAckKeys, which EffectiveAcks documents returning "in
// log-key order".
//
// PLANT: `if false && ks[i].NodeEpoch != ks[j].NodeEpoch` — the epoch
// comparison never fires, so two keys on the same site sort by Seq ALONE.
// That is wrong on its own terms (this file's header, A11): seq is issued
// PER (site_id, node_epoch) — a new epoch restarts it at 1 — so a
// low-numbered seq on a LATER epoch is not "earlier" than a high-numbered
// seq on an earlier epoch. This fixture forces exactly that inversion: the
// trip's own epoch-1 ack is pushed to a high seq by padding, and its
// epoch-2 ack (a fresh stream) lands at seq 1.
func TestWitnessP4SortAckKeysOrdersByEpochBeforeSeq(t *testing.T) {
	st := newSite(t)
	trip, _ := dispatchInbound(t, st, 60, "driver-1")

	// Advance (here, epoch 1) past seq 4 with another trip's plain acks —
	// padding, filtered out of this trip's EffectiveAcks by trip_id.
	other, _ := dispatchInbound(t, st, 60, "driver-2")
	for i := 0; i < 4; i++ {
		if err := Acknowledge(st, here, 1, other.TripID, nil, 0, "default"); err != nil {
			t.Fatalf("fixture: padding ack %d: %v", i, err)
		}
	}
	if err := Acknowledge(st, here, 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatalf("fixture: epoch-1 ack: %v", err)
	}
	lowEpochHighSeq := AckKey{here, 1, 5}

	// A fresh epoch on the SAME site restarts seq at 1 — lower than the key
	// above, but from a strictly LATER epoch.
	if err := Acknowledge(st, here, 2, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatalf("fixture: epoch-2 ack: %v", err)
	}
	highEpochLowSeq := AckKey{here, 2, 1}

	got, err := EffectiveAcks(st, trip.TripID)
	if err != nil {
		t.Fatal(err)
	}
	want := []AckKey{lowEpochHighSeq, highEpochLowSeq}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("EffectiveAcks = %v, want %v in (site_id, node_epoch, seq) order — epoch 1 must sort before epoch 2 on the same site regardless of which side carries the higher seq", got, want)
	}
}
