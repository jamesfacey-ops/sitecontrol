// w4_guards_test.go — W4-9 (the §4.7 cycle guard: a forged 2-cycle of
// corrections resolves EXACTLY as a dangling target — fail-closed,
// Outcome errors, the resolver TERMINATES under a step budget) and W4-10
// (a dangling target resolves fail-closed with errors-not-empties, and
// SELF-HEALS with zero machinery when the target later replicates in —
// status is never stored).
// design/WINDOW-BUILD-PLAN-v1.md "### W4", criteria LOCKED 2026-08-27.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD — that IS the clean-tree red (go vet).
//
// KILLER MUTANTS: mTP-7b (a chain follower with no cycle detection —
// follows target references forever on a looped pair; the step budget
// w4Budgeted / SEAM W4-S-BUDGET is the observing channel, red attributed
// to the NAMED test, never a suite-`-timeout` abort), mTP-7 (silent drop
// on a dangling target), VF-m8 (stored status — the deferred winner
// arrives and a stamped outcome is permanently wrong: the self-heal half).
package domain

import (
	"testing"

	"sitecontrol/internal/store"
)

// TestW4ForgedCycleResolvesAsDanglingAndTerminates — W4-9 (mTP-7b). The
// loop is constructible in normal operation because next-seq is
// predictable (TP §4.7's worked shape): correction c1 at (site-b, 1, 1)
// targets (site-b, 1, 2) — the key the NEXT event will receive — and c2,
// landing at (site-b, 1, 2), targets c1. Both targets EXIST locally, so a
// dangling-only guard ("target absent → error") never fires — that is the
// fixture's non-degeneracy: only genuine cycle detection can produce the
// error, and only non-termination is the alternative, which the step
// budget converts into this test's OWN red.
func TestW4ForgedCycleResolvesAsDanglingAndTerminates(t *testing.T) {
	const (
		c1MS = w3Base - 9*w3Hour
		c2MS = w3Base - 8*w3Hour
	)
	st := w3NewStore(t)
	// A local roster so the store is not degenerate-empty.
	w3RosterPair(t, st, "Cycle Bystander A", "Cycle Bystander B")

	k1 := store.Key{SiteID: "site-b", NodeEpoch: 1, Seq: 1}
	k2 := store.Key{SiteID: "site-b", NodeEpoch: 1, Seq: 2}
	c1 := w4WireCorrection(t, "site-b", 1, 1, w3Base+1*w3Hour, store.ClockSynced, k2, c1MS, "Forger One")
	c2 := w4WireCorrection(t, "site-b", 1, 2, w3Base+2*w3Hour, store.ClockSynced, k1, c2MS, "Forger Two")
	w3Apply(t, st, c1)
	w3Apply(t, st, c2)

	// Fixture non-degeneracy, from the READ-BACK log: a genuine 2-cycle —
	// each correction's target is the other, and BOTH targets exist
	// locally (so this is provably not the dangling case).
	cs := w4CorrectionsInLog(t, st, [][2]any{{"site-b", int64(1)}})
	if len(cs) != 2 {
		t.Fatalf("FIXTURE DEFECT: %d corrections in the site-b stream, want 2", len(cs))
	}
	if cs[0].Target != cs[1].Env.Key() || cs[1].Target != cs[0].Env.Key() {
		t.Fatalf("FIXTURE DEFECT: not a 2-cycle — targets are %v and %v for keys %v and %v", cs[0].Target, cs[1].Target, cs[0].Env.Key(), cs[1].Env.Key())
	}
	for _, k := range []store.Key{k1, k2} {
		evs, err := st.Events(k.SiteID, k.NodeEpoch, k.Seq, 1)
		if err != nil || len(evs) == 0 || evs[0].Seq != k.Seq {
			t.Fatalf("FIXTURE DEFECT: loop member %v is not locally present — the fixture would exercise the DANGLING path, not the cycle guard", k)
		}
	}

	// Both loop keys, both read directions: the resolver TERMINATES (the
	// step budget is not exhausted — SEAM W4-S-BUDGET) and FAILS CLOSED
	// (a non-nil error; never either correction's value effective).
	for _, k := range []store.Key{k1, k2} {
		eff, chain, err := w4Resolve(t, st, k)
		if err == nil {
			t.Fatalf("W4-9 (mTP-7b's silent sibling): ResolveOccurredAt(%v) on the looped pair returned nil error (eff=%+v, chain=%d entries) — a cycle resolves EXACTLY as a dangling target: fail-closed, Outcome errors (TP §4.7)", k, eff, len(chain))
		}
		if eff.Corrected || eff.InstantUTCms == c1MS || eff.InstantUTCms == c2MS {
			t.Fatalf("W4-9: on error, ResolveOccurredAt(%v) still presents a correction's value as effective (%+v) — never either correction's value effective", k, eff)
		}
		outs, err := w4OutcomeBudgeted(t, st, k)
		if err == nil {
			t.Fatalf("W4-9 (mTP-7b): Outcome(%v) on the looped pair returned (%v, nil) — errors, not empties (VF §4.2)", k, outs)
		}
	}
}

// TestW4DanglingTargetFailsClosedThenSelfHeals — W4-10 (mTP-7; VF-m8 for
// the second half). Epoch-partial replication: the target lives at
// (site-b, epoch 1, seq 1) but only site-b's EPOCH-2 stream — carrying the
// correction — has arrived. The read fails CLOSED: a non-nil error, never
// ([]FactOutcome{}, nil), never a crash, never the correction's value
// presented as effective. Then the target's stream applies via the wire
// path and THE SAME READS resolve normally with zero machinery — status
// is never stored, so the deferred case comes out true at every later
// read (VF §4.3 rule 2 / T7's shape on this carrier).
func TestW4DanglingTargetFailsClosedThenSelfHeals(t *testing.T) {
	const correctedMS = w3Base - 9*w3Hour
	st := w3NewStore(t)
	w3RosterPair(t, st, "Dangling Bystander A", "Dangling Bystander B")

	targetKey := store.Key{SiteID: "site-b", NodeEpoch: 1, Seq: 1}
	// The target itself: a site-b person_created carrying an ENTERED
	// original instant — built now, applied only in phase 2.
	target := w3WireAmendment(t, "site-b", 1, 1, w3Base-2*w3Hour, "person-dangling", nil)
	target.Payload = []byte(`{"type":"person_created","person_id":"person-dangling","name":"Dangling Target"}`)
	target.OccurredAt = w4OriginalMS
	target.TimeSource = "entered"

	// The correction arrives FIRST, on the epoch-2 stream.
	corr := w4WireCorrection(t, "site-b", 2, 1, w3Base+1*w3Hour, store.ClockSynced, targetKey, correctedMS, "Riley Okpik")
	w3Apply(t, st, corr)

	// --- phase 1: dangling — fail closed, errors not empties ------------
	eff, chain, err := w4Resolve(t, st, targetKey)
	if err == nil {
		t.Fatalf("W4-10 (mTP-7, the silent dangling drop): ResolveOccurredAt on the absent target returned nil error (eff=%+v, chain=%d) — fail-closed means a non-nil error, never a silent drop, never the correction's value effective", eff, len(chain))
	}
	if eff.Corrected || eff.InstantUTCms == correctedMS {
		t.Fatalf("W4-10: on error the correction's value is still presented as effective (%+v)", eff)
	}
	outs, err := w4OutcomeBudgeted(t, st, corr.Key())
	if err == nil {
		if len(outs) == 0 {
			t.Fatalf("W4-10 (VF §4.2): Outcome(dangling correction) returned ([]FactOutcome{}, nil) — an empty successful result does not exist in this API; errors, not empties")
		}
		t.Fatalf("W4-10 (mTP-7): Outcome(dangling correction) returned (%v, nil) — the untrusted shape at rest is an ERROR", outs)
	}

	// --- phase 2: the target replicates in — the SAME reads self-heal ---
	w3Apply(t, st, target)
	eff, chain, err = w4Resolve(t, st, targetKey)
	if err != nil {
		t.Fatalf("W4-10 (VF-m8's shape): after the target applied, the same read still errors: %v — the deferred case must come out true at every later read with ZERO machinery (status is never stored)", err)
	}
	if !eff.Corrected || eff.InstantUTCms != correctedMS || eff.Winner != corr.Key() {
		t.Fatalf("W4-10 (VF-m8, stored status): after the target applied, resolved = (corrected=%v, %d, winner %v), want (true, %d, %v) — a status stamped while dangling is permanently wrong the moment the target arrives", eff.Corrected, eff.InstantUTCms, eff.Winner, correctedMS, corr.Key())
	}
	if len(chain) != 1 || chain[0].Key != corr.Key() || chain[0].Status != Effective {
		t.Fatalf("W4-10: healed chain = %+v, want exactly the one Effective correction %v", chain, corr.Key())
	}
	if out := w4OccurredAtFact(t, st, corr.Key()); out.Status != Effective || out.Winner != corr.Key() {
		t.Fatalf("W4-10: healed Outcome(correction) = (%v, winner %v), want (Effective, itself)", out.Status, out.Winner)
	}
}
