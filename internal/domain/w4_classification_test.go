// w4_classification_test.go — W4-8: substitution CLASSIFIES per TP §7.4's
// substitution cell, asserted AT REST (TP §7.10's round-3 re-verb): the
// substituted value classifies `entered` via domain.ProvenanceClass — a
// claim by construction, no third TimeSource state — and the accompanying
// clock_state is a §4.1 point-read of the WINNING CORRECTION's stored row,
// never the target's. NO RENDERER IS INVOLVED, and none is asserted: the
// render half of the substitution cell is TP's tracked debt 3, owner the
// audit-line builder, due at the first consumer build — a green here
// proves resolution at rest and must never be over-read as the screen
// behaving.
// design/WINDOW-BUILD-PLAN-v1.md "### W4", criteria LOCKED 2026-08-27.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD — that IS the clean-tree red (go vet).
//
// KILLER MUTANT: m8 — substitution misclassification: classifies Default,
// or carries the TARGET's clock_state instead of the winning correction's
// (concretely: eff.Winner pointing at the target, so the consumer's
// point-read lands on the wrong row's clock_state).
package domain

import (
	"testing"

	"sitecontrol/internal/store"
)

// TestW4SubstitutionClassifiesEnteredWithWinnersClockState — W4-8 (m8).
// The fixture is non-degenerate on the clock_state axis BY CONSTRUCTION:
// the target's envelope is clock-SYNCED, the winning correction's row is
// clock-UNSYNCED — so a point-read through a Winner that names the target
// yields a DIFFERENT clock_state than one through the correction, and the
// carries-the-target's mutant is observable, not a coin flip.
func TestW4SubstitutionClassifiesEnteredWithWinnersClockState(t *testing.T) {
	const correctedMS = w3Base - 6*w3Hour
	st := w3NewStore(t)
	target := w4Target(t, st, "site-a", 1, "Substitution Target") // synced envelope (w3Reading)
	corr := w4WireCorrection(t, "site-b", 1, 1, w3Base+1*w3Hour, store.ClockUnsynced, target.Key(), correctedMS, "Casey Officer")
	w3Apply(t, st, corr)

	eff, _, err := w4Resolve(t, st, target.Key())
	if err != nil {
		t.Fatalf("W4-8: ResolveOccurredAt: %v", err)
	}
	if !eff.Corrected || eff.InstantUTCms != correctedMS {
		t.Fatalf("W4-8 (m8's classifies-Default limb / VF-m4): resolved = (corrected=%v, %d), want the substitution (true, %d)", eff.Corrected, eff.InstantUTCms, correctedMS)
	}
	if eff.Winner == target.Key() {
		t.Fatalf("W4-8 (m8): eff.Winner names the TARGET — the accompanying clock_state point-read would land on the target's row; it must be the winning correction's key %v", corr.Key())
	}
	if eff.Winner != corr.Key() {
		t.Fatalf("W4-8: eff.Winner = %v, want the winning correction's key %v", eff.Winner, corr.Key())
	}
	if eff.RecordedBy != "Casey Officer" {
		t.Fatalf("W4-8: eff.RecordedBy = %q, want the winning correction's %q (VF §4.2: populated for this carrier only)", eff.RecordedBy, "Casey Officer")
	}

	// The accompanying clock_state: the §4.1 point-read of eff.Winner's
	// stored row — which the fixture guarantees is discriminating.
	winnerRows, err := st.Events(eff.Winner.SiteID, eff.Winner.NodeEpoch, eff.Winner.Seq, 1)
	if err != nil || len(winnerRows) == 0 || winnerRows[0].Seq != eff.Winner.Seq {
		t.Fatalf("W4-8: the §4.1 point-read of eff.Winner %v failed (%v) — the accompanying clock_state is unreachable", eff.Winner, err)
	}
	winnerRow := winnerRows[0]
	targetRows, err := st.Events(target.SiteID, target.NodeEpoch, target.Seq, 1)
	if err != nil || len(targetRows) == 0 {
		t.Fatal("W4-8 fixture: target row unreadable")
	}
	if targetRows[0].AuthoredAt.State == winnerRow.AuthoredAt.State {
		t.Fatalf("W4-8 FIXTURE DEFECT: DEGENERATE — target and winning correction share clock_state %q; the carries-the-target's-clock_state mutant would be invisible (standing rule 3)", winnerRow.AuthoredAt.State)
	}
	if winnerRow.AuthoredAt.State != store.ClockUnsynced {
		t.Fatalf("W4-8 (m8, the target's clock_state): the point-read through eff.Winner yields clock_state %q, want the WINNING CORRECTION's %q — the typing act is what is being contextualised (TP §7.4's substitution cell), never the target's clock", winnerRow.AuthoredAt.State, store.ClockUnsynced)
	}

	// The substituted value classifies `entered` — a claim by construction
	// (SEAM W4-S-SUBST: the substitution cell's classifier input IS
	// "entered"; no third TimeSource state exists).
	if got := ProvenanceClass("entered", eff.InstantUTCms, winnerRow.AuthoredAt.WallMS); got != ProvenanceEntered {
		t.Fatalf("W4-8 (m8): the substitution cell classifies %v, want ProvenanceEntered — the corrected value is typed, always", got)
	}
	// Fixture non-degeneracy for the classification limb: the D-42-literal
	// rival derivation — classifying the substituted instant through the
	// winning correction's ENVELOPE pair (TimeSource "default" per D-44,
	// AuthoredAt != the substituted instant) — must NOT classify entered,
	// or a consumer that reads the envelope instead of the substitution
	// cell would be indistinguishable from a correct one.
	if winnerRow.TimeSource != "default" {
		t.Fatalf("W4-8 fixture: the winning correction's envelope TimeSource = %q, want \"default\" (D-44)", winnerRow.TimeSource)
	}
	if rival := ProvenanceClass(winnerRow.TimeSource, eff.InstantUTCms, winnerRow.AuthoredAt.WallMS); rival == ProvenanceEntered {
		t.Fatalf("W4-8 FIXTURE DEFECT: DEGENERATE — the envelope-pair rival derivation also classifies Entered; the misclassification would be invisible")
	}
}
