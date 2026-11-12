// w4_seams_test.go — THE ONE place W4's domain tests bind every Go spelling
// the LOCKED criteria leave undetermined (design/WINDOW-BUILD-PLAN-v1.md,
// "### W4 — The correction event", criteria LOCKED 2026-08-27; plan §5
// step 2), plus the shared fixture machinery. This file holds NO assertions
// of its own.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD — every symbol under "SHAPE PRESUMPTIONS"
// below is unbuilt at the clean tree (app HEAD 8e24107: `occurred_at_corrected`
// appears nowhere in the app tree, per the lock's own pre-chunk measurement).
// That compile failure IS the ledger RED for the resolver-level tests
// (plan §5 step 2: "capture the go vet output as its red").
//
// SHAPE PRESUMPTIONS (SEAMS), each bound once; disputes go to the gate:
//
//   - SEAM W4-S-EDGE: the correction authoring edge is
//     domain.CorrectOccurredAt(st *store.Store, siteID string,
//     nodeEpoch int64, in domain.TimeCorrection) (store.Envelope, error)
//     with type TimeCorrection struct {
//     Target store.Key; CorrectedOccurredAtMS int64; RecordedBy string }.
//     DELIBERATELY NO (occurredAtMS, timeSource) trailing pair: overriding
//     the correction act's own time is DECLINED UI (VF §5, D-44), so the
//     edge offers no parameter for it — the envelope pair is always
//     default-captured (rule W). If the builder mirrors AmendPerson's
//     trailing pair instead, that is a gate dispute against THIS seam, not
//     a licence to diverge silently.
//   - SEAM W4-S-BUDGET: W4-9's mechanical termination form (the lock's
//     FA-2 clause: "the fixture invokes the resolver under a step budget —
//     a bounded-iteration harness call — and asserts the budget was NOT
//     exhausted") is bound as w4Budgeted below: the resolver runs in a
//     goroutine while the harness iterates a BOUNDED loop (w4StepBudget
//     iterations, w4StepMS apart) waiting for it; loop exhaustion is a
//     t.Fatal attributed to the NAMED calling test — never a
//     suite-`-timeout` abort, which is exactly the observing channel the
//     ledger's mTP-7b MUTANT row needs (W3's m23 / w3BudgetedResolve is
//     the worked precedent; there the harness could walk the snapshot
//     because the data structure WAS the termination guarantee — here the
//     fixture data deliberately contains a loop, so the only observable is
//     "the call returned", and the bounded loop makes that observation).
//   - SEAM W4-S-SUBST: how a consumer derives TP §7.4's substitution cell
//     from the resolver output, at rest, no renderer: the substituted
//     value's provenance input is "entered" BY CONSTRUCTION (TP §4.7 —
//     no third TimeSource state), so it classifies
//     ProvenanceClass("entered", eff.InstantUTCms, winnerRow.AuthoredAt.WallMS)
//     = ProvenanceEntered; the accompanying clock_state is the §4.1
//     point-read of eff.Winner's stored row — the WINNING CORRECTION's
//     AuthoredAt.State, never the target's.
//
// NOT seams (DOC-pinned, cited; a test disagreeing with these is wrong):
//   - Event type "occurred_at_corrected" (TP §4.7; plan W4 Changes).
//   - The FIVE payload key literals target_site_id / target_node_epoch /
//     target_seq / corrected_occurred_at / recorded_by — pinned at the
//     addendum's C-1 block and on B-2's wire-freeze list; nothing else,
//     no reason field (operative default #15).
//   - Side table "occurred_at_correction" (VF §7.5 obligation 1 / Changes
//     delta 1).
//   - ResolveOccurredAt / ResolvedTime / Correction / Outcome / FactOutcome
//     / Status signatures (VF §4.2 verbatim — "no field added, no field
//     renamed").
//   - Fact name "occurred_at" for this carrier (VF §4.2's own vocabulary).
//   - The correction's own envelope: TimeSource "default",
//     OccurredAt == AuthoredAt exactly (D-44; TP §4.7).
//   - Ascending-key composition, epoch-major (VF §4.3 rule 1; D-28 B-i).
package domain

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"sitecontrol/internal/store"
)

const (
	w4EvtCorrected = "occurred_at_corrected" // DOC — TP §4.7
	// The five pinned payload key literals (DOC — W4 addendum C-1 / B-2):
	w4KeyTargetSiteID = "target_site_id"
	w4KeyTargetEpoch  = "target_node_epoch"
	w4KeyTargetSeq    = "target_seq"
	w4KeyCorrectedAt  = "corrected_occurred_at"
	w4KeyRecordedBy   = "recorded_by"
	// The carrier's fact vocabulary (DOC — VF §4.2).
	w4FactOccurredAt = "occurred_at"
)

// w4OriginalMS is the target's own (entered) occurred instant in every
// fixture that corrects it — pinned, distinct from every corrected value.
const w4OriginalMS = w3Base - 10*w3Hour

// --- SEAM W4-S-BUDGET: the bounded-iteration harness call ----------------

const (
	w4StepBudget = 600 // bounded harness iterations
	w4StepMS     = 5   // ms per iteration (budget ≈ 3s of wall clock)
)

// w4Budgeted invokes fn under the step budget (SEAM W4-S-BUDGET). Budget
// exhaustion is a red attributed to the CALLING named test via t.Fatal —
// the observing channel a binary-level `-timeout` abort cannot supply
// (W4-9 ⟦LOCK — FA-2⟧; the ledger's mTP-7b row records the named test).
func w4Budgeted(t *testing.T, label string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		fn()
		close(done)
	}()
	for i := 0; i < w4StepBudget; i++ {
		select {
		case <-done:
			return
		default:
			time.Sleep(w4StepMS * time.Millisecond)
		}
	}
	t.Fatalf("W4-9 (mTP-7b): step budget EXHAUSTED (%d bounded iterations × %dms) in %s — the resolver did not terminate; a chain follower with no cycle detection follows target references forever. This red belongs to the NAMED test that owns this fixture, never to a suite -timeout abort.", w4StepBudget, w4StepMS, label)
}

// w4Resolve is ResolveOccurredAt under the step budget.
func w4Resolve(t *testing.T, st *store.Store, target store.Key) (ResolvedTime, []Correction, error) {
	t.Helper()
	var (
		eff   ResolvedTime
		chain []Correction
		err   error
	)
	w4Budgeted(t, fmt.Sprintf("ResolveOccurredAt(%v)", target), func() {
		eff, chain, err = ResolveOccurredAt(st, target)
	})
	return eff, chain, err
}

// w4OutcomeBudgeted is Outcome under the step budget.
func w4OutcomeBudgeted(t *testing.T, st *store.Store, k store.Key) ([]FactOutcome, error) {
	t.Helper()
	var (
		outs []FactOutcome
		err  error
	)
	w4Budgeted(t, fmt.Sprintf("Outcome(%v)", k), func() {
		outs, err = Outcome(st, k)
	})
	return outs, err
}

// w4OccurredAtFact returns the occurred_at FactOutcome of the correction
// event at k; an Outcome error or a missing fact is a FAILURE, never a
// skip (T-A precondition 2).
func w4OccurredAtFact(t *testing.T, st *store.Store, k store.Key) FactOutcome {
	t.Helper()
	outs, err := w4OutcomeBudgeted(t, st, k)
	if err != nil {
		t.Fatalf("Outcome(%v): %v", k, err)
	}
	for _, o := range outs {
		if o.Fact == w4FactOccurredAt {
			return o
		}
	}
	t.Fatalf("Outcome(%v) reported no %q fact (got %+v) — cannot-match is a FAILURE, never a skip (W4-12 / T-A precondition 2)", k, w4FactOccurredAt, outs)
	return FactOutcome{}
}

// --- fixture machinery ---------------------------------------------------

// w4Target mints one person whose person_created event is the correction
// target, authored with the ENTERED original instant w4OriginalMS, and
// returns that event's envelope (the last event on the stream).
func w4Target(t *testing.T, st *store.Store, siteID string, epoch int64, name string) store.Envelope {
	t.Helper()
	if _, err := CreatePerson(st, siteID, epoch, name, nil, nil, w4OriginalMS, "entered"); err != nil {
		t.Fatal(err)
	}
	evs := w3Events(t, st, siteID, epoch)
	env := evs[len(evs)-1]
	if env.OccurredAt != w4OriginalMS || env.TimeSource != "entered" {
		t.Fatalf("fixture defect: target envelope carries (%q, %d), want (\"entered\", %d)", env.TimeSource, env.OccurredAt, w4OriginalMS)
	}
	return env
}

// w4CorrectLocal authors one correction through the edge (SEAM W4-S-EDGE)
// with the clock parked at clockMS; a refusal is fatal (fixture, not
// assertion).
func w4CorrectLocal(t *testing.T, st *store.Store, siteID string, epoch, clockMS int64, target store.Key, correctedMS int64, recordedBy string) store.Envelope {
	t.Helper()
	st.Clock = w3Reading(clockMS)
	env, err := CorrectOccurredAt(st, siteID, epoch, TimeCorrection{
		Target: target, CorrectedOccurredAtMS: correctedMS, RecordedBy: recordedBy,
	})
	if err != nil {
		t.Fatalf("fixture correction refused: %v", err)
	}
	return env
}

// w4WireCorrection hand-builds an occurred_at_corrected envelope exactly as
// a peer's edge would ship it: the five pinned payload keys, the envelope
// pair default-captured (rule W / D-44).
func w4WireCorrection(t *testing.T, siteID string, epoch, seq, authoredMS int64, state store.ClockState, target store.Key, correctedMS int64, recordedBy string) store.Envelope {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"type":            w4EvtCorrected,
		w4KeyTargetSiteID: target.SiteID,
		w4KeyTargetEpoch:  target.NodeEpoch,
		w4KeyTargetSeq:    target.Seq,
		w4KeyCorrectedAt:  correctedMS,
		w4KeyRecordedBy:   recordedBy,
	})
	if err != nil {
		t.Fatal(err)
	}
	env := store.Envelope{
		SiteID: siteID, NodeEpoch: epoch, Seq: seq,
		SchemaVersion: store.SchemaVersion, OriginAuth: siteID,
		OccurredAt: authoredMS, TimeSource: "default", // D-44: the envelope describes the ACT
		Payload: json.RawMessage(raw),
	}
	env.AuthoredAt.WallMS = authoredMS
	env.AuthoredAt.MaxErrorUS = 42
	env.AuthoredAt.State = state
	return env
}

// w4LogCorrection is one correction read BACK from the log (rival
// recomputation reads the log, never the fixture constants).
type w4LogCorrection struct {
	Env         store.Envelope
	Target      store.Key
	CorrectedMS int64
	RecordedBy  string
}

// w4CorrectionsInLog parses every occurred_at_corrected event in the given
// (site, epoch) streams.
func w4CorrectionsInLog(t *testing.T, st *store.Store, streams [][2]any) []w4LogCorrection {
	t.Helper()
	var out []w4LogCorrection
	for _, s := range streams {
		site := s[0].(string)
		epoch := s[1].(int64)
		for _, e := range w3Events(t, st, site, epoch) {
			var pl struct {
				Type        string `json:"type"`
				TargetSite  string `json:"target_site_id"`
				TargetEpoch int64  `json:"target_node_epoch"`
				TargetSeq   int64  `json:"target_seq"`
				CorrectedAt int64  `json:"corrected_occurred_at"`
				RecordedBy  string `json:"recorded_by"`
			}
			if err := json.Unmarshal(e.Payload, &pl); err != nil {
				t.Fatal(err)
			}
			if pl.Type != w4EvtCorrected {
				continue
			}
			out = append(out, w4LogCorrection{
				Env:         e,
				Target:      store.Key{SiteID: pl.TargetSite, NodeEpoch: pl.TargetEpoch, Seq: pl.TargetSeq},
				CorrectedMS: pl.CorrectedAt,
				RecordedBy:  pl.RecordedBy,
			})
		}
	}
	return out
}

// --- the T-A agreement oracle over correction fixtures (W4-12) -----------

// w4TACorrection is one asserted corrected instant of one fixture event.
type w4TACorrection struct {
	Key         store.Key
	CorrectedMS int64
}

// w4RunTAOracle is VF §8's T-A agreement oracle applied to the correction
// carrier: for every fixture correction, Outcome Status == Effective IFF
// the resolved effective instant for the shared target equals that
// correction's asserted instant. Precondition 1 (pairwise-distinct values)
// is asserted at RUNTIME; precondition 2 (cannot-match = failure) is
// w4OccurredAtFact's fatal. The two implementations under agreement are
// the resolver's SQL-side read and store.Key.Less's Go-side ordering —
// drift between them (VF-m11) breaks the iff.
func w4RunTAOracle(t *testing.T, st *store.Store, target store.Key, fixtures []w4TACorrection) {
	t.Helper()
	seen := map[int64]store.Key{}
	for _, f := range fixtures {
		if prev, dup := seen[f.CorrectedMS]; dup {
			t.Fatalf("T-A FIXTURE DEFECT (precondition 1): corrected instant %d asserted twice (%v and %v) — with duplicate values the iff is unsatisfiable on correct code and the predictable repair greens over VF-m11", f.CorrectedMS, prev, f.Key)
		}
		seen[f.CorrectedMS] = f.Key
	}
	eff, _, err := w4Resolve(t, st, target)
	if err != nil {
		t.Fatalf("T-A: ResolveOccurredAt(%v): %v", target, err)
	}
	for _, f := range fixtures {
		out := w4OccurredAtFact(t, st, f.Key)
		matches := eff.InstantUTCms == f.CorrectedMS
		if (out.Status == Effective) != matches {
			t.Fatalf("W4-12 T-A disagreement (VF-m11): correction %v asserted %d; Outcome says %v but the resolved effective instant is %d — the SQL composition and store.Key.Less have drifted", f.Key, f.CorrectedMS, out.Status, eff.InstantUTCms)
		}
		if out.Status == Effective && out.Winner != f.Key {
			t.Fatalf("T-A: correction %v Effective but Winner %v != queried key (VF §4.2 convention)", f.Key, out.Winner)
		}
		if out.Status == Superseded {
			if out.Winner == (store.Key{}) || out.Winner == f.Key {
				t.Fatalf("T-A: correction %v Superseded with Winner %v — Winner is always a REAL other event's key (VF §4.2)", f.Key, out.Winner)
			}
		}
	}
}
