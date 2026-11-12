// w4_correction_chain_oracle_test.go — extends the criterion 3a INDEPENDENT
// oracle (d92_dominance_oracle_test.go, ruling/schema-11-step3a-oracle,
// merged 744d53d) to the CORRECTION CHAIN carrier: `occurred_at_corrected`
// (time_correct.go, item 14, LOCKED 2026-08-27 — READ-ONLY here, never
// edited, never will be). ResolveOccurredAt and Outcome's correction branch
// both resolve the chain through correctionChainForOriginal + resolveContest
// (person_amend.go); the existing T-A oracle for this carrier
// (w4RunTAOracle, w4_seams_test.go) checks Outcome against
// ResolveOccurredAt's OWN eff.InstantUTCms — so the two would AGREE IF BOTH
// WERE WRONG, exactly the coupling d92 exists to break for PersonAmended
// (reviewer-step2: "they'd agree if both wrong"). This file supplies the
// second, INDEPENDENT verdict for the correction chain.
//
// HOW THIS STAYS INDEPENDENT OF resolveContest (the headline):
//   - The WINNER is derived from d92RefResolve (d92_dominance_oracle_test.go)
//     — the reference DECISION PROCEDURE, already proven independent of
//     resolveContest over ~12,416 enumerated cases (that file's §A). This
//     file never calls resolveContest and never reads its verdict.
//   - The INPUT to that decision procedure is read by an ENTIRELY SEPARATE
//     pipeline from time_correct.go's: w4ChainRefDecode reads the raw
//     event_log payload with its own field lookups, never the
//     occurred_at_correction side table, never queryCorrections. The chain
//     of corrections is gathered by w4ChainRefState.chain — THIS FILE'S OWN
//     frontier walk over target references — never correctionOriginal or
//     correctionChainForOriginal (time_correct.go, gated). A fold that
//     mangles `seen`, a decode that drops the target triple, or a walk that
//     stops short is a disagreement HERE, not a shared blind spot.
//
// THE GAP THIS ORACLE IS BUILT TO CLOSE (measured, corrected from an
// earlier over-claim: w4_causal_resolution_test.go and
// w4_resolution_order_test.go already exercise a correction-of-a-correction
// — CorrectionTarget, outcome.go, IS exercised today). What is actually
// missing, measured by reading every existing correction fixture:
//
//	(1) DEPTH: the deepest existing chain is 2 (one correction of one
//	    correction —
//	    TestW4CorrectionOfACorrectionStampsTheHeadAndCompetesInTheOriginalsChain,
//	    TestW4CorrectionOfCorrectionCompetesInOriginalChain). Nothing
//	    reaches depth 3, where a member can be dominated by a NON-adjacent
//	    chain member and a stamp must union heads across more than one
//	    prior generation.
//	(2) SCOPE OF THE CONCURRENT CASE: every existing concurrent-pair fixture
//	    (TestW4GenuinelyConcurrentCorrectionsFallToTheSiteIDTiebreak,
//	    TestW4ConcurrentCorrectionPairIsClockFreeUnderEitherTimeBasis) poses
//	    the pair AT HOP 1, directly on the original — the correction-chain
//	    analogue of STEP3-BLINDSPOTS §3's merge-group finding
//	    ("target-scope-only tells the loser it won"): a witness that only
//	    poses the tiebreak at the top of the chain cannot see a defect that
//	    only misfires once dominance has to be threaded THROUGH an
//	    intermediate hop.
//	(3) METHOD: every existing assertion is a hand-derived expected
//	    constant, verified once per fixture by the test's author reasoning
//	    about the rule. That is real independence from resolveContest for
//	    the cases it covers, but it does not scale or generalise: this file
//	    instead derives the expected winner from a REFERENCE DECISION
//	    PROCEDURE (d92RefResolve) fed by this file's OWN raw-log read and
//	    OWN chain walk, so the same machinery re-verifies any chain shape,
//	    not only the ones an author sat down and hand-solved.
//
// Every fixture below has DEPTH >= 3 (an original plus a correction OF a
// correction OF a correction) and every assertion contests the WHOLE
// reference chain rooted at the original — never one node's direct
// targeters — queried from every hop, not only the original.
//
// THE CLOCK-IN-THE-TIEBREAK GUARD (STEP3-ORACLE-SIBLING measured: a
// causal-chain fixture cannot see it — a purely causal A<-B<-C proves
// nothing about the tiebreak, since dominance alone already decides it):
// TestW4ChainOracleConcurrentEmbeddedInAChain plants a genuinely concurrent
// pair TWO HOPS into the chain — neither names the other — with the GREATER
// key on the EARLIER clock, guarded by
// w4ChainRequireGreaterKeyCarriesTheEarlierClock, so a tiebreak reading the
// clock, greatest or least, must disagree with the rule; embedding the pair
// past hop 1 also means a target-scope-only witness could not have posed it.
//
// MT-1 (this file's own killer mutant; see
// reports/witness/STEP3A-CORRECTION-CHAIN.md §MT-1 for the full three-way
// production): CorrectionTarget (outcome.go) mutated to return the
// correction's OWN key instead of its recorded target. That mutation makes
// correctionOriginal's walk revisit its own starting key on the very first
// hop past a correction, which its cycle guard catches — and
// CorrectOccurredAt SWALLOWS that error and silently stamps no `seen` at
// all (time_correct.go's documented "stamps nothing rather than refusing"
// clause), so the corruption is silent, not a build break. This file's
// stamp check (w4OracleCorrectLocal) catches it at the FIRST hop-2
// authoring call; several pre-existing depth-2 fixtures also regress under
// it (this mutant is a real structural break, not a narrow blind spot) —
// what this file adds is an INDEPENDENT catch, through a pipeline that
// never calls correctionOriginal/correctionChainForOriginal/resolveContest,
// so the two verdicts are not "the same opinion read twice" even where
// both fire. Also reproduced through this same carrier, as a second,
// independent kill: resolveContest (person_amend.go) mutated to ignore
// `seen` entirely — the pre-[[D-92]] defect d92's own header names as its
// first killer mutant — caught here a second time, through the correction
// surface; neither mutation is ever committed to this tree, only measured
// against it and reverted.
//
// TESTS ONLY: TestW4ChainOracleOverDeepChains,
// TestW4ChainOracleConcurrentEmbeddedInAChain,
// TestW4ChainOracleAgreesFromEveryChainMember,
// TestW4ChainOracleKeyOrderOpposedToCausalOrder,
// TestW4ChainOracleWinnerReadWithTwoSurvivorsStanding.
//
// RA2-ORACLE-RERUN.md (reports/witness/), a Fable re-run measured at
// c01bc1e, found this file's independence genuine but its power against
// resolveContest entirely stamp-only: across 11 planted mutants, every
// catch fired at w4OracleCorrectLocal's `seen`-stamp check and NOT ONE
// winner/instant/recorded_by/length/order/status/Outcome assertion in
// w4RunChainOracle ever fired, because all three fixtures above align KEY
// order with CAUSAL order and none ever reads a winner while two
// survivors stand — so a mutant that only changes HOW a winner is chosen
// (never WHETHER a stamp is right) has nothing to disagree on. The two
// fixtures below are that report's own recommended fix, reusing this
// file's helpers exactly as its scratch probe did: one poses the
// causally-first correction on the GREATEST key, so D-92's dominance
// winner and D-28's "greatest key, dominance ignored" winner are
// different keys; the other leaves a genuinely concurrent pair standing
// UNRESOLVED at read time, so "least survivor" and "greatest survivor"
// are different keys too.
package domain

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// =======================================================================
// The reference's OWN read pipeline for the correction carrier
// =======================================================================

// w4ChainRefRow is one occurred_at_corrected event as THIS FILE reads it —
// its own field lookups off the raw payload, never time_correct.go's
// OccurredAtCorrected struct, never the occurred_at_correction side table.
type w4ChainRefRow struct {
	Key        d92RefKey
	Target     d92RefKey
	Corrected  int64
	RecordedBy string
	Seen       []d92RefKey
	Clock      int64
}

// asD92 hands the row to d92RefResolve — the reference DECISION PROCEDURE
// (d92_dominance_oracle_test.go), independent of resolveContest and
// truth-table proven over ~12,416 cases. d92RefResolve reads only Key and
// Seen, so no fact/value machinery is carried across.
func (r w4ChainRefRow) asD92() d92RefRow { return d92RefRow{Key: r.Key, Seen: r.Seen} }

// w4ChainRefDecode reads one log payload in the correction carrier's own
// wire shape (time_correct.go's five pinned keys plus `seen`, looked up
// independently); false when it is not an occurred_at_corrected event.
func w4ChainRefDecode(k d92RefKey, clock int64, payload []byte) (w4ChainRefRow, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		return w4ChainRefRow{}, false
	}
	var typ string
	if err := json.Unmarshal(raw["type"], &typ); err != nil || typ != "occurred_at_corrected" {
		return w4ChainRefRow{}, false
	}
	var site, by string
	var epoch, seq, corrected int64
	if err := json.Unmarshal(raw["target_site_id"], &site); err != nil {
		return w4ChainRefRow{}, false
	}
	if err := json.Unmarshal(raw["target_node_epoch"], &epoch); err != nil {
		return w4ChainRefRow{}, false
	}
	if err := json.Unmarshal(raw["target_seq"], &seq); err != nil {
		return w4ChainRefRow{}, false
	}
	if err := json.Unmarshal(raw["corrected_occurred_at"], &corrected); err != nil {
		return w4ChainRefRow{}, false
	}
	if err := json.Unmarshal(raw["recorded_by"], &by); err != nil {
		return w4ChainRefRow{}, false
	}
	return w4ChainRefRow{
		Key: k, Clock: clock, Target: d92RefKey{Site: site, Epoch: epoch, Seq: seq},
		Corrected: corrected, RecordedBy: by,
		Seen: d92RefSeenOf(payload), // the shared wire shape (contest.go's SeenRef): {site_id,node_epoch,seq}
	}, true
}

// w4ChainRefState is the reference's view of one post's raw correction log,
// read in CLOCK order (d92's own discipline: the one order the rule must
// ignore).
type w4ChainRefState struct{ rows []w4ChainRefRow }

func w4ChainRefReadLog(t *testing.T, st *store.Store) w4ChainRefState {
	t.Helper()
	var out []w4ChainRefRow
	err := st.LocalTx(func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT site_id, node_epoch, seq, authored_at_wall_ms, payload
		                         FROM event_log
		                        ORDER BY authored_at_wall_ms, site_id DESC, node_epoch DESC, seq DESC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var k d92RefKey
			var clock int64
			var payload string
			if err := rows.Scan(&k.Site, &k.Epoch, &k.Seq, &clock, &payload); err != nil {
				return err
			}
			if r, ok := w4ChainRefDecode(k, clock, []byte(payload)); ok {
				out = append(out, r)
			}
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("chain reference: read event_log: %v", err)
	}
	return w4ChainRefState{rows: out}
}

func (s w4ChainRefState) byKey(k d92RefKey) (w4ChainRefRow, bool) {
	for _, r := range s.rows {
		if r.Key == k {
			return r, true
		}
	}
	return w4ChainRefRow{}, false
}

// original follows target references — THIS FILE'S OWN WALK, never
// correctionOriginal (time_correct.go, gated). A cycle is a fixture defect
// for this oracle, exactly as d92's rep() treats a same_as cycle: this file
// contests the DOMINANCE RULE over the chain, not the gated file's own
// fail-closed cycle path.
func (s w4ChainRefState) original(t *testing.T, k d92RefKey) d92RefKey {
	t.Helper()
	cur, hops := k, map[d92RefKey]bool{}
	for {
		row, ok := s.byKey(cur)
		if !ok {
			return cur
		}
		if hops[cur] {
			t.Fatalf("fixture not posed for this oracle: correction target cycle through %v", cur)
		}
		hops[cur] = true
		cur = row.Target
	}
}

// chain gathers every correction reachable by following target references
// TO original — THIS FILE'S OWN frontier expansion, never
// correctionChainForOriginal — sorted ascending by key (d92RefCompare, the
// reference's own comparator).
func (s w4ChainRefState) chain(original d92RefKey) []w4ChainRefRow {
	var out []w4ChainRefRow
	seenKeys := map[d92RefKey]bool{original: true}
	frontier := []d92RefKey{original}
	for len(frontier) > 0 {
		var next []d92RefKey
		for _, tk := range frontier {
			for _, r := range s.rows {
				if r.Target == tk && !seenKeys[r.Key] {
					seenKeys[r.Key] = true
					out = append(out, r)
					next = append(next, r.Key)
				}
			}
		}
		frontier = next
	}
	sort.Slice(out, func(i, j int) bool { return d92RefCompare(out[i].Key, out[j].Key) < 0 })
	return out
}

// verdict resolves one original's chain through d92RefResolve — this
// file's only dependency on d92's machinery, and never on resolveContest.
func (s w4ChainRefState) verdict(t *testing.T, original d92RefKey) ([]w4ChainRefRow, d92RefVerdict, bool) {
	t.Helper()
	rows := s.chain(original)
	if len(rows) == 0 {
		return nil, d92RefVerdict{}, false
	}
	seenVal := map[int64]d92RefKey{}
	for _, r := range rows {
		if prev, dup := seenVal[r.Corrected]; dup {
			t.Fatalf("ORACLE FIXTURE DEFECT: corrected instant %d asserted twice (%v, %v) — the composed value could not identify its winner", r.Corrected, prev, r.Key)
		}
		seenVal[r.Corrected] = r.Key
	}
	contest := make([]d92RefRow, len(rows))
	for i, r := range rows {
		contest[i] = r.asD92()
	}
	v, ok := d92RefResolve(t, contest)
	return rows, v, ok
}

func w4ChainDescribeSeen(rows []w4ChainRefRow) string {
	var parts []string
	for _, r := range rows {
		parts = append(parts, fmt.Sprintf("%v.seen=%v", r.Key, r.Seen))
	}
	return strings.Join(parts, " ")
}

// =======================================================================
// Contesting the production surfaces
// =======================================================================

// w4RunChainOracle contests ResolveOccurredAt and Outcome against the
// reference for the chain containing `target` (ANY member — the original
// or any correction in it; the reference resolves to the original itself,
// independently of correctionOriginal, mirroring W4-11's rule that a query
// from any hop reaches the same chain).
func w4RunChainOracle(t *testing.T, st *store.Store, target store.Key, label string) {
	t.Helper()
	ref := w4ChainRefReadLog(t, st)
	original := ref.original(t, d92ToRef(target))
	rows, v, ok := ref.verdict(t, original)
	if !ok {
		t.Fatalf("%s: oracle fed no corrections for %v (original %v) — nothing contested", label, target, original)
	}
	winnerRow, _ := ref.byKey(v.Winner)

	eff, chain, err := w4Resolve(t, st, target)
	if err != nil {
		t.Fatalf("%s: ResolveOccurredAt(%v): %v", label, target, err)
	}
	if !eff.Corrected {
		t.Fatalf("%s: ResolveOccurredAt(%v).Corrected = false, want true — the reference found %d chain entries", label, target, len(rows))
	}
	if got, want := eff.Winner, d92FromRef(v.Winner); got != want {
		t.Fatalf("%s: CHAIN ORACLE DISAGREEMENT (winner) — ResolveOccurredAt(%v).Winner = %v, reference winner = %v (heads %v, rule-3 fallback=%v)\n  chain: %s",
			label, target, got, want, v.Heads, v.Fallback, w4ChainDescribeSeen(rows))
	}
	if eff.InstantUTCms != winnerRow.Corrected {
		t.Fatalf("%s: CHAIN ORACLE DISAGREEMENT (instant) — ResolveOccurredAt(%v).InstantUTCms = %d, reference winner %v asserted %d",
			label, target, eff.InstantUTCms, v.Winner, winnerRow.Corrected)
	}
	if eff.RecordedBy != winnerRow.RecordedBy {
		t.Fatalf("%s: CHAIN ORACLE DISAGREEMENT (recorded_by) — ResolveOccurredAt(%v).RecordedBy = %q, reference winner %v recorded_by %q",
			label, target, eff.RecordedBy, v.Winner, winnerRow.RecordedBy)
	}
	if len(chain) != len(rows) {
		t.Fatalf("%s: CHAIN ORACLE DISAGREEMENT (length) — ResolveOccurredAt(%v) chain has %d entries, reference chain (rooted at the original, every hop) has %d — a truncated or over-included walk",
			label, target, len(chain), len(rows))
	}
	for i, c := range chain {
		want := rows[i]
		if c.Key != d92FromRef(want.Key) {
			t.Fatalf("%s: CHAIN ORACLE DISAGREEMENT (order) — chain[%d].Key = %v, reference (ascending) wants %v", label, i, c.Key, d92FromRef(want.Key))
		}
		if c.InstantUTCms != want.Corrected {
			t.Fatalf("%s: CHAIN ORACLE DISAGREEMENT (value) — chain[%d] (%v).InstantUTCms = %d, reference = %d", label, i, c.Key, c.InstantUTCms, want.Corrected)
		}
		wantStatus := Superseded
		if want.Key == v.Winner {
			wantStatus = Effective
		}
		if c.Status != wantStatus {
			t.Fatalf("%s: CHAIN ORACLE DISAGREEMENT (status) — chain[%d] (%v).Status = %v, reference wants %v", label, i, c.Key, c.Status, wantStatus)
		}
		// Outcome, checked against the REFERENCE verdict directly — closing
		// the coupling w4RunTAOracle leaves open (it checks Outcome against
		// ResolveOccurredAt's OWN eff.InstantUTCms; here Outcome answers to
		// the independent reference instead).
		out := w4OccurredAtFact(t, st, d92FromRef(want.Key))
		wantEffective := want.Key == v.Winner
		if (out.Status == Effective) != wantEffective {
			t.Fatalf("%s: CHAIN ORACLE DISAGREEMENT (Outcome) — Outcome(%v).occurred_at.Status = %v, reference says winner=%v", label, want.Key, out.Status, wantEffective)
		}
		if out.Status == Effective && out.Winner != d92FromRef(want.Key) {
			t.Fatalf("%s: Outcome(%v) Effective but Winner %v != the queried key", label, want.Key, out.Winner)
		}
	}
}

// w4ChainOracleAtEveryPost runs the oracle at st and at fresh posts holding
// roster then the given streams in each listed order — d92's own
// discipline (d92OracleAtEveryPost), so a disagreement visible only under
// one delivery order is not missed.
func w4ChainOracleAtEveryPost(t *testing.T, st *store.Store, roster []store.Envelope, orders [][]store.Envelope, target store.Key, label string) {
	t.Helper()
	w4RunChainOracle(t, st, target, label+" @ authoring post")
	for i, evs := range orders {
		w4RunChainOracle(t, d92Fresh(t, roster, evs), target, fmt.Sprintf("%s @ fresh post, delivery order %d", label, i))
	}
}

// =======================================================================
// Checked authoring: the `seen` stamp, off THIS FILE'S OWN chain walk
// =======================================================================

// w4ChainWantStamp is the reference's own stamp: the undominated heads of
// target's chain as held RIGHT NOW — computed off the raw log, through THIS
// FILE'S walk, never correctionOriginal/correctionChainForOriginal.
func w4ChainWantStamp(t *testing.T, st *store.Store, target store.Key) []d92RefKey {
	t.Helper()
	ref := w4ChainRefReadLog(t, st)
	original := ref.original(t, d92ToRef(target))
	_, v, ok := ref.verdict(t, original)
	if !ok {
		return nil // no correction stands yet: the first correction carries no `seen`
	}
	return v.Heads
}

// w4OracleCorrectLocal authors one correction through the REAL edge
// (CorrectOccurredAt) and checks the `seen` stamp it wrote against the
// reference's stamp, computed BEFORE authoring — the correction carrier's
// analogue of d92OracleAmend.
func w4OracleCorrectLocal(t *testing.T, st *store.Store, siteID string, epoch, clockMS int64, target store.Key, correctedMS int64, recordedBy string) store.Envelope {
	t.Helper()
	want := w4ChainWantStamp(t, st, target)
	env := w4CorrectLocal(t, st, siteID, epoch, clockMS, target, correctedMS, recordedBy)
	got := d92RefSortKeys(d92RefSeenOf(env.Payload))
	if !d92RefKeysEqual(got, want) {
		t.Fatalf("CHAIN ORACLE DISAGREEMENT (the `seen` stamp): CorrectOccurredAt(target=%v) at %s stamped %v; the reference (undominated heads of the chain as held) says %v",
			target, siteID, got, want)
	}
	return env
}

// w4OracleCorrectRemote runs w4OracleCorrectLocal at a scratch post primed
// with primer (a real remote edge, d92OracleRemote's pattern) and returns
// the minted envelope.
func w4OracleCorrectRemote(t *testing.T, primer []store.Envelope, siteID string, epoch, clockMS int64, target store.Key, correctedMS int64, recordedBy string) store.Envelope {
	t.Helper()
	envs := w3AuthorRemote(t, primer, siteID, epoch, clockMS, func(rs *store.Store) {
		w4OracleCorrectLocal(t, rs, siteID, epoch, clockMS, target, correctedMS, recordedBy)
	})
	if len(envs) != 1 {
		t.Fatalf("fixture: remote correction minted %d events, want 1", len(envs))
	}
	return envs[0]
}

// w4ChainRequireGreaterKeyCarriesTheEarlierClock is blind spot 2's guard
// (d92RequireGreaterKeyCarriesTheEarlierClock), ported to the correction
// carrier's own reference read: the GREATER key's authored_at_wall_ms must
// be STRICTLY earlier, so a tiebreak reading the clock — greatest or
// least — must disagree with the rule on this pair.
func w4ChainRequireGreaterKeyCarriesTheEarlierClock(t *testing.T, st *store.Store, a, b store.Envelope) {
	t.Helper()
	ref := w4ChainRefReadLog(t, st)
	ra, oka := ref.byKey(d92ToRef(a.Key()))
	rb, okb := ref.byKey(d92ToRef(b.Key()))
	if !oka || !okb {
		t.Fatalf("fixture defect: %v/%v not both in the log", a.Key(), b.Key())
	}
	greater, lesser := ra, rb
	if d92RefCompare(ra.Key, rb.Key) < 0 {
		greater, lesser = rb, ra
	}
	if greater.Clock >= lesser.Clock {
		t.Fatalf("fixture defect: the greater key %v carries authored_at_wall_ms %d, not strictly earlier than %v's %d — the clock rival would agree with the tiebreak and certify nothing",
			greater.Key, greater.Clock, lesser.Key, lesser.Clock)
	}
}

// =======================================================================
// Fixtures
// =======================================================================

// TestW4ChainOracleOverDeepChains: a correction OF a correction OF a
// correction (depth 3), each on its OWN site stream (so any delivery order
// is legal — no same-stream seq-gap refusal). Closes the target-scope-only
// blind spot: no pre-existing fixture ever targets another correction, so
// the walk past hop 1 (correctionOriginal's generalised depth,
// CorrectionTarget in outcome.go) had zero coverage before this file.
func TestW4ChainOracleOverDeepChains(t *testing.T) {
	st := w3NewStore(t)
	target := w4Target(t, st, "site-a", 1, "Deep Chain Target")
	roster := []store.Envelope{target}

	c1 := w4OracleCorrectRemote(t, roster, "site-b", 1, w3Base+1*w3Hour, target.Key(), w3Base-9*w3Hour, "Officer One")
	w3ApplyAll(t, st, []store.Envelope{c1})

	held2 := append(append([]store.Envelope{}, roster...), c1)
	c2 := w4OracleCorrectRemote(t, held2, "site-c", 1, w3Base+2*w3Hour, c1.Key(), w3Base-8*w3Hour, "Officer Two")
	w3ApplyAll(t, st, []store.Envelope{c2})

	held3 := append(append([]store.Envelope{}, held2...), c2)
	c3 := w4OracleCorrectRemote(t, held3, "site-d", 1, w3Base+3*w3Hour, c2.Key(), w3Base-7*w3Hour, "Officer Three")
	w3ApplyAll(t, st, []store.Envelope{c3})

	orders := [][]store.Envelope{
		{c1, c2, c3},
		{c3, c2, c1},
		{c2, c3, c1},
		{c3, c1, c2},
	}
	w4ChainOracleAtEveryPost(t, st, roster, orders, c3.Key(), "deep chain (depth 3, three streams), queried at the deepest hop")

	eff, _, err := w4Resolve(t, st, target.Key())
	if err != nil {
		t.Fatal(err)
	}
	if eff.Winner != c3.Key() {
		t.Fatalf("fixture (non-vacuity): winner = %v, want the deepest correction %v", eff.Winner, c3.Key())
	}
}

// TestW4ChainOracleConcurrentEmbeddedInAChain: a genuinely concurrent pair
// TWO HOPS into the chain (c2a/c2b both correct c1, neither sees the
// other), greater key (site-d) on the EARLIER clock, then a third
// correction (c3) resolves the survivor deeper still. A witness posing the
// concurrent pair only at the top level (correcting the original directly)
// would never exercise the tiebreak PAST hop 1 — this combines both
// measured blind spots in one fixture.
func TestW4ChainOracleConcurrentEmbeddedInAChain(t *testing.T) {
	st := w3NewStore(t)
	target := w4Target(t, st, "site-a", 1, "Embedded Concurrent Target")
	roster := []store.Envelope{target}

	c1 := w4OracleCorrectRemote(t, roster, "site-b", 1, w3Base, target.Key(), w3Base-9*w3Hour, "Officer One")
	w3ApplyAll(t, st, []store.Envelope{c1})
	held := append(append([]store.Envelope{}, roster...), c1)

	// c2a (site-c, LATER clock) and c2b (site-d, EARLIER clock) both
	// correct c1 without seeing each other — genuinely concurrent — and
	// "site-d" > "site-c" lexicographically, so c2b holds the GREATER key
	// on the EARLIER clock.
	c2a := w4OracleCorrectRemote(t, held, "site-c", 1, w3Base+3*w3Hour, c1.Key(), w3Base-8*w3Hour, "Officer Two-A")
	c2b := w4OracleCorrectRemote(t, held, "site-d", 1, w3Base+1*w3Hour, c1.Key(), w3Base-8*w3Hour+1, "Officer Two-B")
	if d92RefCompare(d92ToRef(c2b.Key()), d92ToRef(c2a.Key())) <= 0 {
		t.Fatalf("fixture defect: c2b (%v) must hold the GREATER key than c2a (%v)", c2b.Key(), c2a.Key())
	}
	w3ApplyAll(t, st, []store.Envelope{c2a, c2b})
	w4ChainRequireGreaterKeyCarriesTheEarlierClock(t, st, c2a, c2b)

	// c3 (site-e) corrects the greater survivor (c2b), one hop deeper.
	held3 := append(append([]store.Envelope{}, held...), c2a, c2b)
	c3 := w4OracleCorrectRemote(t, held3, "site-e", 1, w3Base+5*w3Hour, c2b.Key(), w3Base-6*w3Hour, "Officer Three")
	w3ApplyAll(t, st, []store.Envelope{c3})

	orders := [][]store.Envelope{
		{c1, c2a, c2b, c3},
		{c1, c2b, c2a, c3},
		{c3, c1, c2b, c2a},
		{c2a, c3, c1, c2b},
	}
	w4ChainOracleAtEveryPost(t, st, roster, orders, c3.Key(), "concurrent pair embedded at hop 2, then extended")

	eff, _, err := w4Resolve(t, st, target.Key())
	if err != nil {
		t.Fatal(err)
	}
	if eff.Winner != c3.Key() {
		t.Fatalf("fixture (non-vacuity): winner = %v, want c3 %v", eff.Winner, c3.Key())
	}
}

// TestW4ChainOracleAgreesFromEveryChainMember: W4-11's one-hop-generalised
// rule ("target references locate the original and its chain") means a
// query from ANY member — the original, an intermediate, or the winner —
// must reach the SAME chain and the SAME winner. Queried from every one.
func TestW4ChainOracleAgreesFromEveryChainMember(t *testing.T) {
	st := w3NewStore(t)
	target := w4Target(t, st, "site-a", 1, "Every Member Target")
	c1 := w4OracleCorrectLocal(t, st, "site-a", 1, w3Base+1*w3Hour, target.Key(), w3Base-9*w3Hour, "Officer One")
	c2 := w4OracleCorrectLocal(t, st, "site-a", 1, w3Base+2*w3Hour, c1.Key(), w3Base-8*w3Hour, "Officer Two")
	c3 := w4OracleCorrectLocal(t, st, "site-a", 1, w3Base+3*w3Hour, c2.Key(), w3Base-7*w3Hour, "Officer Three")

	for _, k := range []store.Key{target.Key(), c1.Key(), c2.Key(), c3.Key()} {
		w4RunChainOracle(t, st, k, fmt.Sprintf("queried from %v", k))
	}
}

// TestW4ChainOracleKeyOrderOpposedToCausalOrder: RA2-ORACLE-RERUN.md's
// measured gap #1. Every existing fixture aligns KEY order with CAUSAL
// order (site-b<c<d, or ascending seq on one stream), so D-92's dominance
// winner and D-28's "greatest key over the whole contest, dominance
// ignored" winner always coincide and a mutation to resolveContest's
// winner rule has nothing to disagree on from the read side. Here the
// causally-FIRST correction (c1) is minted on the GREATEST key (site-z)
// and each later hop in this depth-3 chain holds a LESSER key (site-b,
// then site-c): the causal/dominance winner (the deepest correction, c3,
// key site-c) and the whole-contest greatest key (c1, key site-z) are
// different keys, so a winner mismatch is now visible from the read side.
func TestW4ChainOracleKeyOrderOpposedToCausalOrder(t *testing.T) {
	st := w3NewStore(t)
	target := w4Target(t, st, "site-a", 1, "Reversed Key Order Target")
	roster := []store.Envelope{target}

	// c1: causally FIRST, key GREATEST (site-z).
	c1 := w4OracleCorrectRemote(t, roster, "site-z", 1, w3Base+1*w3Hour, target.Key(), w3Base-9*w3Hour, "Officer One")
	w3ApplyAll(t, st, []store.Envelope{c1})

	// c2: causally second, corrects c1 in knowledge of it, key LESSER (site-b).
	held2 := append(append([]store.Envelope{}, roster...), c1)
	c2 := w4OracleCorrectRemote(t, held2, "site-b", 1, w3Base+2*w3Hour, c1.Key(), w3Base-8*w3Hour, "Officer Two")
	w3ApplyAll(t, st, []store.Envelope{c2})

	// c3: causally third and deepest, corrects c2, key between the other two (site-c).
	held3 := append(append([]store.Envelope{}, held2...), c2)
	c3 := w4OracleCorrectRemote(t, held3, "site-c", 1, w3Base+3*w3Hour, c2.Key(), w3Base-7*w3Hour, "Officer Three")
	w3ApplyAll(t, st, []store.Envelope{c3})

	orders := [][]store.Envelope{
		{c1, c2, c3},
		{c3, c2, c1},
		{c2, c3, c1},
		{c3, c1, c2},
	}
	w4ChainOracleAtEveryPost(t, st, roster, orders, c3.Key(), "key order opposed to causal order (causal-first carries the greatest key)")

	eff, _, err := w4Resolve(t, st, target.Key())
	if err != nil {
		t.Fatal(err)
	}
	if eff.Winner != c3.Key() {
		t.Fatalf("fixture (non-vacuity): winner = %v, want the causally deepest correction %v", eff.Winner, c3.Key())
	}
}

// TestW4ChainOracleWinnerReadWithTwoSurvivorsStanding: RA2-ORACLE-RERUN.md's
// measured gap #2. Every existing fixture resolves down to exactly ONE
// surviving head before any oracle run reads a winner (the deepest
// correction always dominates whatever pair preceded it), so a mutation
// that only changes HOW a winner is picked AMONG SEVERAL surviving heads
// — never WHETHER a row is dominated at all — has nothing to disagree on.
// Here a genuinely concurrent pair (c2a, c2b — neither's `seen` names the
// other) both correct c1 and are left standing UNRESOLVED at read time:
// both survive dominance, and the winner is read directly off that
// two-survivor contest.
func TestW4ChainOracleWinnerReadWithTwoSurvivorsStanding(t *testing.T) {
	st := w3NewStore(t)
	target := w4Target(t, st, "site-a", 1, "Two Survivors Standing Target")
	roster := []store.Envelope{target}

	c1 := w4OracleCorrectRemote(t, roster, "site-b", 1, w3Base, target.Key(), w3Base-9*w3Hour, "Officer One")
	w3ApplyAll(t, st, []store.Envelope{c1})
	held := append(append([]store.Envelope{}, roster...), c1)

	// c2a (site-c) and c2b (site-p) both correct c1 without seeing each
	// other — genuinely concurrent — and neither is corrected further: no
	// c3 resolves the pair down to one head.
	c2a := w4OracleCorrectRemote(t, held, "site-c", 1, w3Base+1*w3Hour, c1.Key(), w3Base-8*w3Hour, "Officer Two-A")
	c2b := w4OracleCorrectRemote(t, held, "site-p", 1, w3Base+2*w3Hour, c1.Key(), w3Base-8*w3Hour+1, "Officer Two-B")
	w3ApplyAll(t, st, []store.Envelope{c2a, c2b})

	orders := [][]store.Envelope{
		{c1, c2a, c2b},
		{c1, c2b, c2a},
		{c2a, c1, c2b},
		{c2b, c2a, c1},
	}
	w4ChainOracleAtEveryPost(t, st, roster, orders, c2b.Key(), "two genuinely-concurrent survivors read directly, unresolved by any further correction")

	eff, _, err := w4Resolve(t, st, target.Key())
	if err != nil {
		t.Fatal(err)
	}
	if eff.Winner != c2b.Key() {
		t.Fatalf("fixture (non-vacuity): winner = %v, want the greater of the two standing survivors %v", eff.Winner, c2b.Key())
	}
}
