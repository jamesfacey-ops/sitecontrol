// w4_resolution_order_test.go — W4-3 (latest-per-target in RECORD order,
// the clock decides nothing: cross-site both arrival orders, same-site
// same-epoch, and — via W4-12 — same-site cross-epoch), W4-12 (the §5
// standing cross-epoch CONTROL row, winner PINNED as a literal, epoch-blind
// mutant named, run under T-A), and W4-11 (correction of a correction
// competes in the ORIGINAL's chain — TP §4.7's one-hop rule).
// design/WINDOW-BUILD-PLAN-v1.md "### W4", criteria LOCKED 2026-08-27.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD — that IS the clean-tree red (go vet).
//
// KILLER MUTANTS: m2 = VF-m2 (resolve by corrected instant), VF-m1
// (arrival/application order), VF-m3 (the hybrid comparator — correct at
// site_id, wrong within a site), VF-m12 (the epoch-blind comparator),
// VF-m11 (implementation drift, via T-A membership), mTP-8 (sub-chain
// fork / dangling-treatment of a correction-of-correction).
package domain

import (
	"testing"

	"sitecontrol/internal/store"
)

// --- W4-3 (a): cross-site, BOTH arrival orders ---------------------------

// TestW4CrossSiteRecordOrderBothArrivalOrders — VF T1's shape on the
// correction carrier: two corrections, one target, two sites, the LOSING
// (site-a) correction carrying the LATER clock so clock order contradicts
// key order. The identical event set is applied to two stores in OPPOSITE
// orders; in both, the fixed-order winner (site-b — "site-b" > "site-a",
// always, regardless of seq or clock) wins, and the losing correction's
// Outcome observably reports Superseded with the winner's key.
func TestW4CrossSiteRecordOrderBothArrivalOrders(t *testing.T) {
	const (
		// ⟦PLAN OWNER 2026-08-29 — standing rule 3 repair. These two were the
		// other way round, giving the certified winner the LATEST corrected
		// instant, so the "latest-by-corrected-instant" rival picked the
		// winner's own value and this file's own guard fatalled DEGENERATE on
		// fixture arithmetic alone. The slip is instructive: the natural
		// instinct is "winner = later time", which is precisely the
		// chronological rule D-28 Part B rejects — the fixture meant to refute
		// it had encoded it. The winner now carries the EARLIER instant, so
		// record order and every rival disagree.⟧
		loserMS  = w3Base - 8*w3Hour // site-a's corrected instant (the loser)
		winnerMS = w3Base - 9*w3Hour // site-b's corrected instant (PINNED winner)
	)
	// Author the shared history once: roster + target + site-a's local
	// correction (LATER clock than site-b's).
	origin := w3NewStore(t)
	target := w4Target(t, origin, "site-a", 1, "Cross Site Target")
	loser := w4CorrectLocal(t, origin, "site-a", 1, w3Base+3*w3Hour, target.Key(), loserMS, "Ida Aputi")
	primer := w3Events(t, origin, "site-a", 1)
	winner := w4WireCorrection(t, "site-b", 1, 1, w3Base+1*w3Hour, store.ClockSynced, target.Key(), winnerMS, "Pat Bell")

	// Clock contradicts key: the loser's clock is LATER than the winner's.
	if loser.AuthoredAt.WallMS <= winner.AuthoredAt.WallMS {
		t.Fatalf("FIXTURE DEFECT: the losing correction's clock (%d) is not later than the winner's (%d) — clock order must contradict key order or a clock resolver passes", loser.AuthoredAt.WallMS, winner.AuthoredAt.WallMS)
	}

	// The site-a primer already contains the loser; order (a) is
	// loser-then-winner, order (b) applies the winner BEFORE the site-a
	// tail that carries the loser (the wire path both ways).
	build := func(winnerFirst bool) *store.Store {
		st := w3NewStore(t)
		if winnerFirst {
			// site-a's stream up to just before the loser, then the
			// winner, then the loser.
			for _, e := range primer {
				if e.Seq == loser.Seq {
					w3Apply(t, st, winner)
				}
				w3Apply(t, st, e)
			}
		} else {
			w3ApplyAll(t, st, primer)
			w3Apply(t, st, winner)
		}
		return st
	}

	for name, winnerFirst := range map[string]bool{"loser applied first": false, "winner applied first": true} {
		t.Run(name, func(t *testing.T) {
			st := build(winnerFirst)
			// Runtime non-degeneracy (standing rule 3): recompute the
			// named rivals from the READ-BACK log.
			w4RequireOrderRivalsDisagree(t, st, target.Key(), winnerMS,
				[][2]any{{"site-a", int64(1)}, {"site-b", int64(1)}})

			eff, _, err := w4Resolve(t, st, target.Key())
			if err != nil {
				t.Fatalf("W4-3a: ResolveOccurredAt: %v", err)
			}
			if eff.InstantUTCms != winnerMS || eff.Winner != winner.Key() {
				t.Fatalf("W4-3a (VF-m1/m2): resolved = (%d, winner %v), want (%d, %v) — the fixed-order winner wins regardless of arrival order and of the clock (D-28 B-i)", eff.InstantUTCms, eff.Winner, winnerMS, winner.Key())
			}
			out := w4OccurredAtFact(t, st, loser.Key())
			if out.Status != Superseded {
				t.Fatalf("W4-3a: the losing correction's Outcome = %v, want Superseded — the at-rest half of \"fail visibly\" (D-42 obligation 4)", out.Status)
			}
			if out.Winner != winner.Key() {
				t.Fatalf("W4-3a: loser's Winner = %v, want the winning correction's key %v", out.Winner, winner.Key())
			}
		})
	}
}

// w4RequireOrderRivalsDisagree recomputes the named rival resolutions from
// the read-back correction set for one target and requires each to pick a
// DIFFERENT instant than the certified winner (standing rule 3 — a fixture
// where a rival agrees certifies nothing). Rivals: latest-by-corrected-
// instant (m2), latest-by-act-clock (VF-m1's chronological face), and
// application order (VF-m1 proper — covered structurally by running both
// arrival orders, so it is not recomputed here).
func w4RequireOrderRivalsDisagree(t *testing.T, st *store.Store, target store.Key, certifiedMS int64, streams [][2]any) {
	t.Helper()
	var cs []w4LogCorrection
	for _, c := range w4CorrectionsInLog(t, st, streams) {
		if c.Target == target {
			cs = append(cs, c)
		}
	}
	if len(cs) < 2 {
		t.Fatalf("FIXTURE DEFECT: %d correction(s) for %v in the log, want >= 2 — nothing is contested", len(cs), target)
	}
	pick := func(less func(a, b w4LogCorrection) bool) int64 {
		best := cs[0]
		for _, c := range cs[1:] {
			if less(best, c) {
				best = c
			}
		}
		return best.CorrectedMS
	}
	rivals := map[string]int64{
		"latest-by-corrected-instant [m2/VF-m2]": pick(func(a, b w4LogCorrection) bool { return a.CorrectedMS < b.CorrectedMS }),
		"latest-by-act-clock [VF-m1]":            pick(func(a, b w4LogCorrection) bool { return a.Env.AuthoredAt.WallMS < b.Env.AuthoredAt.WallMS }),
	}
	for name, got := range rivals {
		if got == certifiedMS {
			t.Fatalf("FIXTURE DEFECT: DEGENERATE — rival %q picks %d, the certified winner's own instant; the fixture cannot kill that mutant (standing rule 3)", name, got)
		}
	}
}

// --- W4-3 (b): same site, same epoch — later seq wins --------------------

// TestW4SameSiteSameEpochLaterSeqWins — VF T2's shape: two corrections on
// ONE (site, epoch) stream; the later seq wins even though the earlier seq
// carries the LATER clock. Every cross-site fixture is decided at site_id
// and structurally cannot see the hybrid comparator (VF-m3) — only a
// same-site pair forces the seq comparison.
func TestW4SameSiteSameEpochLaterSeqWins(t *testing.T) {
	const (
		// ⟦PLAN OWNER 2026-08-29 — standing rule 3 repair; see the note above.⟧
		earlySeqMS = w3Base - 8*w3Hour // seq n, LATER clock — the loser
		lateSeqMS  = w3Base - 9*w3Hour // seq n+1, earlier clock — the PINNED winner
	)
	st := w3NewStore(t)
	target := w4Target(t, st, "site-a", 1, "Same Stream Target")
	c1 := w4CorrectLocal(t, st, "site-a", 1, w3Base+3*w3Hour, target.Key(), earlySeqMS, "Ida Aputi")
	c2 := w4CorrectLocal(t, st, "site-a", 1, w3Base+1*w3Hour, target.Key(), lateSeqMS, "Pat Bell")
	if c1.SiteID != c2.SiteID || c1.NodeEpoch != c2.NodeEpoch || c1.Seq >= c2.Seq {
		t.Fatalf("FIXTURE DEFECT: not a same-site same-epoch ascending pair: %v vs %v", c1.Key(), c2.Key())
	}
	if c1.AuthoredAt.WallMS <= c2.AuthoredAt.WallMS {
		t.Fatalf("FIXTURE DEFECT: the earlier seq must carry the LATER clock (got %d <= %d) — otherwise a chronological resolver agrees", c1.AuthoredAt.WallMS, c2.AuthoredAt.WallMS)
	}
	w4RequireOrderRivalsDisagree(t, st, target.Key(), lateSeqMS, [][2]any{{"site-a", int64(1)}})

	eff, _, err := w4Resolve(t, st, target.Key())
	if err != nil {
		t.Fatalf("W4-3b: ResolveOccurredAt: %v", err)
	}
	if eff.InstantUTCms != lateSeqMS || eff.Winner != c2.Key() {
		t.Fatalf("W4-3b (VF-m3, the hybrid comparator): resolved = (%d, winner %v), want (%d, %v) — later seq wins WITHIN a site", eff.InstantUTCms, eff.Winner, lateSeqMS, c2.Key())
	}
	if out := w4OccurredAtFact(t, st, c1.Key()); out.Status != Superseded || out.Winner != c2.Key() {
		t.Fatalf("W4-3b: earlier-seq correction's Outcome = (%v, winner %v), want (Superseded, %v)", out.Status, out.Winner, c2.Key())
	}
}

// --- W4-12: the standing cross-epoch CONTROL row -------------------------

// w4CrossEpochFixture is W4-12's fixture, shared with the T-A oracle run.
type w4CrossEpochFixture struct {
	st       *store.Store
	target   store.Envelope
	e1       store.Envelope // (site-a, epoch 1, seq 900) — HIGH seq, LATER clock: every rival's winner
	e2       store.Envelope // (site-a, epoch 2, seq 3)  — the re-image; the PINNED epoch-major winner
	loserMS  int64
	winnerMS int64
}

// w4BuildCrossEpochFixture constructs the LOCKED keys literally: a
// correction at (site-a, epoch 2, seq 3) against one at
// (site-a, epoch 1, seq 900) — the re-image shape. The epoch-1 stream is
// inflated with filler events so the correction lands at seq 900 EXACTLY;
// the epoch-2 stream carries two fillers so its correction lands at seq 3.
func w4BuildCrossEpochFixture(t *testing.T) w4CrossEpochFixture {
	t.Helper()
	fx := w4CrossEpochFixture{
		// ⟦PLAN OWNER 2026-08-29 — standing rule 3 repair; see the note above.⟧
		loserMS:  w3Base - 8*w3Hour,
		winnerMS: w3Base - 9*w3Hour,
	}
	fx.st = w3NewStore(t)
	fx.target = w4Target(t, fx.st, "site-a", 1, "Cross Epoch Target")

	// Inflate epoch 1 to watermark 899, then author the loser at seq 900.
	wm := w3Watermark(t, fx.st, "site-a", 1)
	for i := wm; i < 899; i++ {
		if _, err := CreatePerson(fx.st, "site-a", 1, "Epoch1 Filler", nil, nil, 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	fx.e1 = w4CorrectLocal(t, fx.st, "site-a", 1, w3Base+3*w3Hour, fx.target.Key(), fx.loserMS, "Ida Aputi")
	if fx.e1.Key() != (store.Key{SiteID: "site-a", NodeEpoch: 1, Seq: 900}) {
		t.Fatalf("FIXTURE DEFECT: the epoch-1 correction landed at %v, want the LOCKED literal (site-a, epoch 1, seq 900)", fx.e1.Key())
	}
	// Epoch 2 — the re-image: two fillers, then the winner at seq 3, with
	// an EARLIER clock than the loser's (clock contradicts key).
	for i := int64(0); i < 2; i++ {
		if _, err := CreatePerson(fx.st, "site-a", 2, "Epoch2 Filler", nil, nil, 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	fx.e2 = w4CorrectLocal(t, fx.st, "site-a", 2, w3Base+2*w3Hour, fx.target.Key(), fx.winnerMS, "Pat Bell")
	if fx.e2.Key() != (store.Key{SiteID: "site-a", NodeEpoch: 2, Seq: 3}) {
		t.Fatalf("FIXTURE DEFECT: the epoch-2 correction landed at %v, want the LOCKED literal (site-a, epoch 2, seq 3)", fx.e2.Key())
	}

	// Runtime non-degeneracy (standing rule 3): every named rival —
	// recomputed from the READ-BACK log — must pick the OTHER correction.
	cs := w4CorrectionsInLog(t, fx.st, [][2]any{{"site-a", int64(1)}, {"site-a", int64(2)}})
	var mine []w4LogCorrection
	for _, c := range cs {
		if c.Target == fx.target.Key() {
			mine = append(mine, c)
		}
	}
	if len(mine) != 2 {
		t.Fatalf("FIXTURE DEFECT: %d corrections for the target in the log, want 2", len(mine))
	}
	pick := func(less func(a, b w4LogCorrection) bool) int64 {
		best := mine[0]
		for _, c := range mine[1:] {
			if less(best, c) {
				best = c
			}
		}
		return best.CorrectedMS
	}
	rivals := map[string]int64{
		"seq-major (epoch-blind) [VF-m12]": pick(func(a, b w4LogCorrection) bool { return a.Env.Seq < b.Env.Seq }),
		"(site_id, seq)-only (epoch-blind) [VF-m12]": pick(func(a, b w4LogCorrection) bool {
			if a.Env.SiteID != b.Env.SiteID {
				return a.Env.SiteID < b.Env.SiteID
			}
			return a.Env.Seq < b.Env.Seq
		}),
		"latest-by-act-clock [VF-m1]":            pick(func(a, b w4LogCorrection) bool { return a.Env.AuthoredAt.WallMS < b.Env.AuthoredAt.WallMS }),
		"latest-by-corrected-instant [m2/VF-m2]": pick(func(a, b w4LogCorrection) bool { return a.CorrectedMS < b.CorrectedMS }),
	}
	for name, got := range rivals {
		if got == fx.winnerMS {
			t.Fatalf("W4-12 FIXTURE DEFECT: DEGENERATE — rival %q picks the certified winner %d; an epoch-blind comparator can be perfectly permutation-invariant and still wrong, and this fixture would not see it (standing rule 3)", name, got)
		}
	}
	return fx
}

// TestW4CrossEpochControlPinsEpochMajorWinner — W4-12 (VF-m12): epoch N+1
// / LOW seq beats epoch N / HIGH seq, the winner PINNED as a literal —
// (site-a, epoch 2, seq 3)'s instant IS the effective value. The stake is
// the repo's own sentence: pair-keying "would dedup a re-imaged site's
// fresh low sequence numbers as already-applied and silently drop an
// entire season of records" [DOC — envelope.go:44–47, cited by the lock].
func TestW4CrossEpochControlPinsEpochMajorWinner(t *testing.T) {
	fx := w4BuildCrossEpochFixture(t)
	eff, chain, err := w4Resolve(t, fx.st, fx.target.Key())
	if err != nil {
		t.Fatalf("W4-12: ResolveOccurredAt: %v", err)
	}
	if eff.InstantUTCms != fx.winnerMS || eff.Winner != fx.e2.Key() {
		t.Fatalf("W4-12 (VF-m12, the epoch-blind comparator): resolved = (%d, winner %v), want the PINNED epoch-2 literal (%d, %v) — epoch-major: (site-a, 2, 3) beats (site-a, 1, 900)", eff.InstantUTCms, eff.Winner, fx.winnerMS, fx.e2.Key())
	}
	if len(chain) != 2 {
		t.Fatalf("W4-12: chain has %d entries, want 2", len(chain))
	}
	if chain[0].Key != fx.e1.Key() || chain[1].Key != fx.e2.Key() {
		t.Fatalf("W4-12: chain order = [%v, %v], want ascending epoch-major [%v, %v]", chain[0].Key, chain[1].Key, fx.e1.Key(), fx.e2.Key())
	}
	if out := w4OccurredAtFact(t, fx.st, fx.e1.Key()); out.Status != Superseded || out.Winner != fx.e2.Key() {
		t.Fatalf("W4-12: the epoch-1 correction's Outcome = (%v, winner %v), want (Superseded, %v)", out.Status, out.Winner, fx.e2.Key())
	}
}

// TestW4TAOracleOverCorrectionFixtures — W4-12's "runs under T-A" clause
// plus T-A membership for the same-site pair: the agreement oracle over
// the cross-epoch fixture and the same-site same-epoch fixture, with
// pairwise-distinct instants asserted at runtime and cannot-match a
// failure, never a skip (T-A's two validity preconditions — the ledger's
// CONTROL row 4). Kills VF-m11 (drift between store.Key.Less and the
// resolver's SQL-side ordering).
func TestW4TAOracleOverCorrectionFixtures(t *testing.T) {
	t.Run("cross-epoch (W4-12)", func(t *testing.T) {
		fx := w4BuildCrossEpochFixture(t)
		w4RunTAOracle(t, fx.st, fx.target.Key(), []w4TACorrection{
			{fx.e1.Key(), fx.loserMS},
			{fx.e2.Key(), fx.winnerMS},
		})
	})
	t.Run("same-site same-epoch (W4-3b)", func(t *testing.T) {
		// ⟦PLAN OWNER 2026-08-29 — standing rule 3 repair; see the note above.⟧
		const earlyMS, lateMS = w3Base - 8*w3Hour, w3Base - 9*w3Hour
		st := w3NewStore(t)
		target := w4Target(t, st, "site-a", 1, "TA Same Stream")
		c1 := w4CorrectLocal(t, st, "site-a", 1, w3Base+3*w3Hour, target.Key(), earlyMS, "Ida Aputi")
		c2 := w4CorrectLocal(t, st, "site-a", 1, w3Base+1*w3Hour, target.Key(), lateMS, "Pat Bell")
		w4RunTAOracle(t, st, target.Key(), []w4TACorrection{
			{c1.Key(), earlyMS},
			{c2.Key(), lateMS},
		})
	})
}

// --- W4-11: correction of a correction -----------------------------------

// TestW4CorrectionOfCorrectionCompetesInOriginalChain — W4-11 (mTP-8): a
// correction whose target is itself an occurred_at_corrected event
// competes in the ORIGINAL target's chain, in record order, exactly as if
// it had targeted the original — never a sub-chain fork, never treated as
// dangling. The resolved effective value and the FULL chain (ascending key
// order) are asserted by fixture.
func TestW4CorrectionOfCorrectionCompetesInOriginalChain(t *testing.T) {
	const (
		c1MS = w3Base - 9*w3Hour // the bad first correction
		c2MS = w3Base - 8*w3Hour // the repair, targeting c1 — later in record order: the winner
	)
	st := w3NewStore(t)
	target := w4Target(t, st, "site-a", 1, "One Hop Target")
	c1 := w4CorrectLocal(t, st, "site-a", 1, w3Base+1*w3Hour, target.Key(), c1MS, "Ida Aputi")
	// The officer's most natural repair of a bad correction: correct the
	// CORRECTION.
	c2 := w4CorrectLocal(t, st, "site-a", 1, w3Base+2*w3Hour, c1.Key(), c2MS, "Pat Bell")

	eff, chain, err := w4Resolve(t, st, target.Key())
	if err != nil {
		t.Fatalf("W4-11 (mTP-8, dangling-treatment): ResolveOccurredAt(original) errored on a correction-of-correction fixture: %v — TP §4.7's one-hop rule says it competes in the ORIGINAL's chain, and the untrusted-render alternative was explicitly rejected", err)
	}
	if eff.InstantUTCms != c2MS || eff.Winner != c2.Key() || !eff.Corrected {
		t.Fatalf("W4-11 (mTP-8): resolved = (corrected=%v, %d, winner %v), want (true, %d, %v) — the correction-of-correction wins the ORIGINAL's chain in record order", eff.Corrected, eff.InstantUTCms, eff.Winner, c2MS, c2.Key())
	}
	if len(chain) != 2 {
		t.Fatalf("W4-11 (mTP-8, sub-chain fork): the original's chain has %d entr(ies), want 2 — \"the chain for a target\" is every correction REACHABLE by following target references (TP §4.7)", len(chain))
	}
	if chain[0].Key != c1.Key() || chain[0].Status != Superseded {
		t.Fatalf("W4-11: chain[0] = (%v, %v), want (%v, Superseded)", chain[0].Key, chain[0].Status, c1.Key())
	}
	if chain[1].Key != c2.Key() || chain[1].Status != Effective || chain[1].InstantUTCms != c2MS {
		t.Fatalf("W4-11: chain[1] = (%v, %v, %d), want (%v, Effective, %d)", chain[1].Key, chain[1].Status, chain[1].InstantUTCms, c2.Key(), c2MS)
	}
	// The repair is never treated as dangling: its own Outcome resolves.
	if out := w4OccurredAtFact(t, st, c2.Key()); out.Status != Effective || out.Winner != c2.Key() {
		t.Fatalf("W4-11: the correction-of-correction's own Outcome = (%v, winner %v), want (Effective, itself)", out.Status, out.Winner)
	}
	if out := w4OccurredAtFact(t, st, c1.Key()); out.Status != Superseded || out.Winner != c2.Key() {
		t.Fatalf("W4-11: the corrected correction's Outcome = (%v, winner %v), want (Superseded, %v)", out.Status, out.Winner, c2.Key())
	}
}
