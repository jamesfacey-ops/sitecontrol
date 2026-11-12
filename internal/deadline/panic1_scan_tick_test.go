// panic1_scan_tick_test.go — ⟦PANIC-1, 2026-09-30⟧ the §9.4 scan loop is
// loud, never fatal. Two witnesses:
//
//   - a held store connection longer than the reentrancy guard's timeout,
//     hit ON THE SCANNER'S OWN LocalTx path, is contention: the tick waits,
//     the guard logs it as such, the tick COMPLETES (its write lands) and
//     the loop keeps ticking. Before the fix the guard panicked at 10 s and
//     nothing on this goroutine recovered it — the post died (L07 round 1;
//     L11 round 2, twice). With the guard reverted to its unconditional
//     panic this test fails on the missing OVERDUE transition and the "SCAN
//     TICK PANICKED" line; with scan.go's recover also reverted the test
//     binary dies outright.
//   - a panic raised inside a tick (a genuine reentrancy verdict, or any
//     programming error under Evaluate) is contained: logged with its
//     stack, the tick skipped, the next tick runs. Before the fix the
//     process died.
//
// The contention witness uses the guard's REAL 10 s timeout — the store's
// knob is unexported and this package deliberately does not reach for it —
// so the test takes ~11 s. The hold is taken from Evaluate's evalHook, which
// runs on the scanner goroutine immediately BEFORE the scan's first guarded
// LocalTx (loadCaptures), so the blocked call is deterministically the
// guarded one, not a pool read. Completion is proven by the tick's "is
// OVERDUE by" transition line, which logTransitions writes only after the
// contended Evaluate has returned its board.
//
// ⟦was: the hold was taken from the CurrentPhase hook, and completion was
// proven by the deadline_red_fired row the tick wrote (recordRedFired) — the
// hook, the table and the write all went with weather phases (James,
// 2026-10-04). evalHook is the same seam, placed where the old hook's
// guarantee — "the next store call is a guarded LocalTx" — still holds.⟧
package deadline

import (
	"bytes"
	"context"
	"database/sql"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// syncLog is a log sink the scanner goroutine and the test can share.
type syncLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *syncLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *syncLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func captureLog(t *testing.T) *syncLog {
	t.Helper()
	sink := &syncLog{}
	prev := log.Writer()
	log.SetOutput(sink)
	t.Cleanup(func() { log.SetOutput(prev) })
	return sink
}

func TestScanTickUnderStoreContentionWaitsLogsAndResumes(t *testing.T) {
	if testing.Short() {
		t.Skip("uses the guard's real 10s timeout")
	}
	sink := captureLog(t)
	st := newSite(t)
	// ⟦FIX17c⟧ the scan's reads (loadCaptures) now come from the read pool
	// and never wait for a held writer (store/readpool.go;
	// TestFix17cScanTickDoesNotWaitForAHeldWriter measures that). This
	// witness is about the guard on a CONTENDED guarded acquisition, so it
	// turns the pool off the way the store itself does for a writer holding
	// TEMP schema, and the scan's reads take the writer's guarded LocalTx
	// path exactly as they did before FIX17c.
	if err := st.LocalTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE TEMP TABLE fix17c_pool_off (x)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	d := newScanner(t, st)
	d.syncInterval = 50 * time.Millisecond

	trip, env := dispatchInbound(t, st, 1, "driver-1")
	d.OnApplied(env, time.Now().Add(-time.Minute+80*time.Millisecond), true) // X−G = 80ms
	time.Sleep(150 * time.Millisecond)                                       // now overdue: the tick will log the transition
	// PRECONDITION: no transition logged yet — the contended tick's
	// "is OVERDUE by" line is what proves the tick completed rather than
	// being skipped.
	if strings.Contains(sink.String(), "is OVERDUE by") {
		t.Fatalf("PRECONDITION: an OVERDUE transition was logged before any tick:\n%s", sink.String())
	}

	const hold = 10*time.Second + 800*time.Millisecond // > the guard's real timeout
	var evals atomic.Int64
	holderDone := make(chan error, 1)
	var once sync.Once
	d.evalHook = func() {
		evals.Add(1)
		once.Do(func() {
			// Another goroutine takes the store's single connection and
			// keeps it past the guard timeout; this hook returns only once
			// it is held, so the scanner's very next LocalTx must wait.
			holding := make(chan struct{})
			go func() {
				holderDone <- st.LocalTx(func(tx *sql.Tx) error {
					close(holding)
					time.Sleep(hold)
					return nil
				})
			}()
			<-holding
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		d.Run(ctx)
	}()

	// The first tick blocks inside loadCaptures for ~hold; then the loop
	// must keep evaluating. Wait for three more evaluations after that.
	deadline := time.Now().Add(hold + 10*time.Second)
	for evals.Load() < 4 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	select {
	case <-runDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if err := <-holderDone; err != nil {
		t.Fatalf("holder LocalTx: %v", err)
	}

	logged := sink.String()
	if strings.Contains(logged, "SCAN TICK PANICKED") {
		t.Fatalf("the contended tick PANICKED (and was only survived by the recover) — the guard judged contention as reentrancy:\n%s", logged)
	}
	if !strings.Contains(logged, "CONTENTION: LocalTx has waited") || !strings.Contains(logged, "still waiting") {
		t.Fatalf("the scanner's wait was not logged as contention by the guard:\n%s", logged)
	}
	if !strings.Contains(logged, "is OVERDUE by") || !strings.Contains(logged, trip.TripID) {
		t.Fatalf("the contended tick did not COMPLETE: no OVERDUE transition logged for trip %s after the hold released:\n%s", trip.TripID, logged)
	}
	if n := evals.Load(); n < 4 {
		t.Fatalf("the scanner did not resume after the contended tick: %d evaluations in total", n)
	}
}

func TestScanTickContainsAPanicLogsItAndTheNextTickRuns(t *testing.T) {
	sink := captureLog(t)
	st := newSite(t)
	d := newScanner(t, st)
	d.syncInterval = 20 * time.Millisecond

	var calls atomic.Int64
	d.evalHook = func() { // ⟦was: d.CurrentPhase — removed with weather phases⟧
		if calls.Add(1) == 1 {
			panic("simulated reentrancy verdict from under Evaluate")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		d.Run(ctx)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for calls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-runDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if n := calls.Load(); n < 3 {
		t.Fatalf("the loop did not keep ticking after the panicking tick: %d evaluations", n)
	}
	logged := sink.String()
	if !strings.Contains(logged, "SCAN TICK PANICKED") || !strings.Contains(logged, "simulated reentrancy verdict") {
		t.Fatalf("the tick's panic was not logged loudly:\n%s", logged)
	}
	if !strings.Contains(logged, "panic1_scan_tick_test.go") {
		t.Fatalf("the logged panic carries no stack naming the panicking frame:\n%s", logged)
	}
	if strings.Count(logged, "SCAN TICK PANICKED") != 1 {
		t.Fatalf("expected exactly one panicking tick, log:\n%s", logged)
	}
}
