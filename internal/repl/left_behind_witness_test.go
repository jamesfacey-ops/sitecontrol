// left_behind_witness_test.go — LEFT-BEHIND-DETECT (2026-09-16), Tier 1
// left-behind-author detection. Reproduces SYNCSTATUS-HONESTY.md item 3's
// gap, measured live by STREAM-DIVERGENCE.md: once a peer's §8.6
// self-history latch closes (peerState.selfDrained), nothing in the
// pre-existing engine ever re-asks that peer what it holds of THIS post's
// own stream — so a genuinely left-behind author (restored from an older
// backup, or re-imaged without an epoch bump, at the SAME node_epoch a peer
// already holds more of) reads "ok" forever: Snapshot's own-row refresh
// (SYNCSTATUS-HONESTY, already shipped) correctly shows the LOCAL watermark,
// but the cached State stays "ok" because nothing ever compares it against
// what a peer actually holds.
package repl

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

// rollBackWatermark directly rewrites the watermark table's cursor for
// (site, epoch) to an earlier value. event_log is append-only by design
// (store.go: event_log_append_only_update/_delete triggers RAISE(ABORT) —
// durability of the authoritative log is the whole point), so a real
// restore-from-an-older-backup replaces the WHOLE FILE; there is no way to
// truncate individual rows in place, deliberately. watermark is a separate,
// plain, mutable cursor table (no such trigger), and it is the ONE fact
// checkLeftBehind (and Snapshot's own-row refresh) actually reads
// (store.Watermark queries it directly, never event_log) — rolling it back
// alone reproduces, exactly, the one observable difference a file-level
// restore leaves behind from Tier 1's point of view, without touching a
// trigger that exists to protect a different invariant.
func rollBackWatermark(t *testing.T, dbPath, site string, epoch, to int64) {
	t.Helper()
	dsn := "file:" + filepath.ToSlash(dbPath) + "?_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("rollBackWatermark: open: %v", err)
	}
	defer db.Close()
	res, err := db.Exec(`UPDATE watermark SET applied_seq = ? WHERE site_id = ? AND node_epoch = ?`, to, site, epoch)
	if err != nil {
		t.Fatalf("rollBackWatermark: update: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("rollBackWatermark: %d rows affected updating (%s,%d) to %d, want 1", n, site, epoch, to)
	}
}

// TestWitnessLeftBehindAuthorAtTheSameEpochReadsOkUntilTier1DetectsIt is the
// required red-then-green witness: two real engines, a genuine restart
// (same store, same epoch — the ordinary, non-lossy case, matching
// STREAM-DIVERGENCE.md's own witness pattern) to reach a legitimate "ok"
// own row, THEN a same-epoch watermark rollback (the lossy case) on the SAME
// engine instance — selfDrained stays latched throughout, so the boot-time
// self-history retry (which WOULD reject a same-epoch redelivery at the
// R-23/AF3 identity bound, engine.go's identityOK exception) never re-fires
// to mask this with a DIFFERENT existing state. This is the gap: nothing
// else re-asks after latch.
func TestWitnessLeftBehindAuthorAtTheSameEpochReadsOkUntilTier1DetectsIt(t *testing.T) {
	ctx := context.Background()
	mesh := newMesh(t, 2)
	a, b := mesh[0], mesh[1]

	a.eng.Rejoin(ctx)
	b.eng.Rejoin(ctx)

	// A authors its first event; B pulls it — an ordinary healthy start.
	if _, err := a.eng.Author(tick("seed"), 0, "default"); err != nil {
		t.Fatal(err)
	}
	b.eng.PullAll(ctx)
	if wm := mustWatermark(t, b.st, "site-a", 1); wm != 1 {
		t.Fatalf("fixture: B's view of site-a is %d, want 1", wm)
	}

	// A restarts ONCE, ordinarily — same store, same epoch, no data lost.
	// This is the realistic way an own row reaches a genuine "ok" state
	// (STREAM-DIVERGENCE.md's own witness restarts the same way): a fresh
	// Engine/peerState, so a fresh Rejoin's self-history ask to B finds
	// B already holding exactly what A holds (1/1) — drains trivially,
	// latches selfDrained immediately.
	a2 := New(a.cfg, a.st)
	a.lh.set(a2.SyncHandler())
	a2.Rejoin(ctx)
	if r := witnessRow(t, a2.Snapshot(), "site-a", 1); r.Watermark != 1 || r.Head != 1 || r.State != StateOK {
		t.Fatalf("fixture: own row after ordinary restart %+v, want 1/1 ok", r)
	}

	// A authors two more events (still on a2) and B genuinely catches up —
	// the peer now holds MORE than A is about to be left behind of.
	for i := 0; i < 2; i++ {
		if _, err := a2.Author(tick(fmt.Sprintf("post-restart-%d", i)), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	b.eng.PullAll(ctx)
	if wm := mustWatermark(t, b.st, "site-a", 1); wm != 3 {
		t.Fatalf("fixture: B's view of site-a is %d, want 3", wm)
	}

	// A is now restored from an OLDER backup at the SAME epoch: its local
	// watermark rolls back to 1, losing the two events B already holds.
	// a2 is NOT recreated — selfDrained stays latched true on a2's
	// peerState for B, so the boot-time self-history retry (needSelf) never
	// re-fires for this key; that retry, if it DID re-fire, would reject
	// same-epoch redelivery outright (StateIdentityRejected, the R-23/AF3
	// bound) rather than ever showing "ok" — a different existing signal,
	// not the one this test is about. This is deliberately the "process
	// keeps running, only the on-disk data was rolled back" shape.
	rollBackWatermark(t, a.cfg.DBPath, "site-a", 1, 1)
	if wm := mustWatermark(t, a.st, "site-a", 1); wm != 1 {
		t.Fatalf("fixture: rollback did not take, watermark=%d", wm)
	}

	// PRE-CONDITION, the defect itself: Snapshot's own-row refresh
	// (SYNCSTATUS-HONESTY, already shipped) reads the LOCAL watermark fresh
	// on every call, so Watermark/Head already correctly show 1/1 — but the
	// cached State is still "ok" from before the rollback, because nothing
	// has re-asked B since the latch closed. This is "today it shows ok".
	row := witnessRow(t, a2.Snapshot(), "site-a", 1)
	if row.State != StateOK || row.Watermark != 1 || row.Head != 1 {
		t.Fatalf("fixture: own row immediately after rollback (before any new pull cycle) %+v, want 1/1 ok (Watermark/Head honest, State stale)", row)
	}

	// RED (pre-fix) / GREEN (post-fix): one more ordinary routine cycle.
	// Pre-fix, nothing re-asks B what it holds of site-a's own stream, so
	// the row still reads "ok". Post-fix, checkLeftBehind (gated on the
	// latch already being closed, exactly the state this fixture is in)
	// asks B, finds head=3 against local watermark=1, and flags it.
	a2.PullAll(ctx)
	row = witnessRow(t, a2.Snapshot(), "site-a", 1)
	if row.State != StateLeftBehind {
		t.Fatalf("LEFT-BEHIND-DETECT not built/not wired: own row after restore + one routine cycle is %+v (peer B holds head 3, local watermark 1), want state=%s", row, StateLeftBehind)
	}
	if row.Watermark != 1 || row.Head != 3 {
		t.Fatalf("own row %+v, want watermark=1 (the honest local truth) head=3 (the peer's real head)", row)
	}
	if row.Detail == "" {
		t.Errorf("left-behind row carries no detail explaining the mismatch")
	}
}

// TestHealthyTwoPostRunAuthoringOnBothSidesNeverTripsLeftBehind is the
// required negative control: the false-positive reasoning is that at the
// SAME epoch a post's own authoring is synchronous with its own store
// write, so a peer can never legitimately get ahead — propagation lag runs
// the other way (the peer is always <= us, never >). Many routine cycles,
// both sites authoring concurrently and pulling both directions, must never
// raise StateLeftBehind on either post's own row.
func TestHealthyTwoPostRunAuthoringOnBothSidesNeverTripsLeftBehind(t *testing.T) {
	ctx := context.Background()
	mesh := newMesh(t, 2)
	a, b := mesh[0], mesh[1]

	a.eng.Rejoin(ctx)
	b.eng.Rejoin(ctx)

	for i := 0; i < 5; i++ {
		if _, err := a.eng.Author(tick(fmt.Sprintf("a%d", i)), 0, "default"); err != nil {
			t.Fatal(err)
		}
		if _, err := b.eng.Author(tick(fmt.Sprintf("b%d", i)), 0, "default"); err != nil {
			t.Fatal(err)
		}
		// A routine cycle after every pair of authors: this is exactly the
		// window checkLeftBehind runs in post-latch, repeated, with fresh
		// authoring landing between checks each time.
		a.eng.PullAll(ctx)
		b.eng.PullAll(ctx)
	}

	for _, m := range mesh {
		for _, ss := range m.eng.Snapshot().Streams {
			if ss.Own && ss.State == StateLeftBehind {
				t.Fatalf("%s: false positive %s on a healthy two-post run authoring on both sides: %+v",
					m.cfg.SiteID, StateLeftBehind, ss)
			}
		}
	}
}
