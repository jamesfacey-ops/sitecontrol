// orientation_sql_order_test.go — SCHEMA-11 step 4, the SQL half of the
// orientation derivation (reports/witness/STEP4-SQL-WITNESS.md). Produced
// RED first on `ORDER BY seq DESC, node_epoch DESC` at each of the two
// `LIMIT 1` reads in rederiveOrientation — separately and together — and
// the commands are recorded in that report.
package domain

import (
	"testing"

	"sitecontrol/internal/store"
)

// TestOrientationBothLimitOneReadsAreEpochMajorALaterEpochWithASmallerSeqWins
// witnesses the SQL half of the derivation. rederiveOrientation picks the
// greatest completion and the greatest retraction with
// `ORDER BY node_epoch DESC, seq DESC LIMIT 1` — the key order, epoch-major,
// the order `store.Key.Less` states in Go. Every other fixture holds one
// row per raw table per (person, site), or rows on which a seq-major
// ordering happens to agree, so `ORDER BY seq DESC, node_epoch DESC` at
// either read survives them all (measured: the package's FAIL set is
// unchanged under it). This fixture puts the LATER epoch at the SMALLER seq
// on exactly the table each read serves, for two people in one stream:
//
//	epoch 1: s1 ortiz C · s2 diaz C · s3 diaz R · s4 ortiz R
//	epoch 2: s1 diaz C  · s2 ortiz C
//	epoch 3: s1 ortiz R
//
// diaz — completions (1,2),(2,1); retraction (1,3). Epoch-major: max C =
// (2,1), max R = (1,3), R < C → HELD by (2,1). Seq-major at the COMPLETION
// read: max C = (1,2), and (1,3) is not less than it → NOT held: a person
// who holds the pad's orientation is reported as not holding it (under
// D-91, not entered). The retraction read has one diaz row, so diaz cannot
// see a mutant there.
//
// ortiz — completions (1,1),(2,2); retractions (1,4),(3,1). Epoch-major:
// max C = (2,2), max R = (3,1), R > C → NOT held. Seq-major at the
// RETRACTION read: max R = (1,4) < (2,2) → HELD: a retracted orientation is
// reported as held. The completion read's seq-major greatest is (2,2)
// either way, so ortiz cannot see a mutant there — which is why there are
// two people. With both reads mutated, both answers flip.
//
// Streams are contiguous per (site, epoch) — Apply refuses a gap — so the
// two histories interleave to fill every seq; nothing is a filler. The
// wall clock rises with the key here; the clock-inverted negative control
// is …FollowsTheKeyUnderEveryDeliveryShapeAndNeverTheClock, not this.
func TestOrientationBothLimitOneReadsAreEpochMajorALaterEpochWithASmallerSeqWins(t *testing.T) {
	by := w9bStr(orientTestPost)
	at := func(epoch, seq int64) int64 { return w9bBase + (epoch*10+seq)*w9bMinute }
	c := func(epoch, seq int64, who string) store.Envelope {
		return w9bEnvelope(orientTestSite, epoch, seq, at(epoch, seq), orientWire(evtOrientationCompleted, who, by))
	}
	r := func(epoch, seq int64, who string) store.Envelope {
		return w9bEnvelope(orientTestSite, epoch, seq, at(epoch, seq), orientWire(evtOrientationRetracted, who, by))
	}
	// One fixed delivery: stream order within each epoch, epochs in order.
	// The property is the SQL's, not the delivery's.
	events := []store.Envelope{
		c(1, 1, "ortiz"), c(1, 2, "diaz"), r(1, 3, "diaz"), r(1, 4, "ortiz"),
		c(2, 1, "diaz"), c(2, 2, "ortiz"),
		r(3, 1, "ortiz"),
	}
	st := w9bNewStore(t)
	for _, env := range events {
		w9bApply(t, st, env)
	}

	// The raw tables hold exactly what was delivered — the fixture is the
	// one drawn above, not a smaller one.
	comps, err := dumpSection[OrientationCompletionRow](st, "orientation_completion")
	if err != nil || len(comps) != 4 {
		t.Fatalf("orientation_completion has %d rows (err %v), want 4", len(comps), err)
	}
	rets, err := dumpSection[OrientationRetractionRow](st, "orientation_retraction")
	if err != nil || len(rets) != 3 {
		t.Fatalf("orientation_retraction has %d rows (err %v), want 3", len(rets), err)
	}

	t.Run("completion read: diaz's later-epoch completion has the smaller seq and wins", func(t *testing.T) {
		row := orientHeld(t, st, "diaz", orientTestSite)
		if row == nil {
			t.Fatal("diaz: NOT held — completions at (1,2) and (2,1), retraction at (1,3): the greatest completion is (2,1) and it outranks the retraction; the completion read picked the greatest SEQ, not the greatest KEY")
		}
		if row.NodeEpoch != 2 || row.Seq != 1 {
			t.Fatalf("diaz: held by (%d,%d), want (2,1) — the derived row must name the epoch-major greatest completion", row.NodeEpoch, row.Seq)
		}
	})
	t.Run("retraction read: ortiz's later-epoch retraction has the smaller seq and wins", func(t *testing.T) {
		if row := orientHeld(t, st, "ortiz", orientTestSite); row != nil {
			t.Fatalf("ortiz: HELD by (%d,%d) — retractions at (1,4) and (3,1), completions at (1,1) and (2,2): the greatest retraction is (3,1) and it outranks every completion; the retraction read picked the greatest SEQ, not the greatest KEY", row.NodeEpoch, row.Seq)
		}
	})
}
