// fix17c_scan_test.go — ⟦FIX17c, 2026-10-07⟧ the §9.4 scan's reads do not
// queue behind a held writer. Its capture read (loadCaptures) used to take
// the store's one connection through LocalTx, so a write stuck in its commit
// stopped the overdue scan — and every page's alert banner, which runs the
// same Evaluate — for as long as the write was stuck. The read now comes
// from the read pool (store/readpool.go).
package deadline

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFix17cScanTickDoesNotWaitForAHeldWriter(t *testing.T) {
	sink := captureLog(t)
	st := newSite(t)
	d := newScanner(t, st)
	d.syncInterval = 50 * time.Millisecond

	trip, env := dispatchInbound(t, st, 1, "driver-1")
	d.OnApplied(env, time.Now().Add(-time.Minute+80*time.Millisecond), true)
	time.Sleep(150 * time.Millisecond)

	const hold = 4 * time.Second
	var evals atomic.Int64
	release := make(chan struct{})
	holderDone := make(chan error, 1)
	var once sync.Once
	d.evalHook = func() {
		evals.Add(1)
		once.Do(func() {
			// Another goroutine holds the writer for the whole of hold.
			holding := make(chan struct{})
			go func() {
				holderDone <- st.LocalTx(func(tx *sql.Tx) error {
					close(holding)
					select {
					case <-release:
					case <-time.After(hold):
					}
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
	start := time.Now()
	for evals.Load() < 4 && time.Since(start) < hold/2 {
		time.Sleep(20 * time.Millisecond)
	}
	n, took := evals.Load(), time.Since(start)
	logged := sink.String()
	close(release)
	cancel()
	<-runDone
	if err := <-holderDone; err != nil {
		t.Fatalf("holder LocalTx: %v", err)
	}
	if n < 4 {
		t.Fatalf("FIX17c: with the writer held, the scan managed %d evaluations in %s — its reads are queueing for the writer", n, took)
	}
	if !strings.Contains(logged, "is OVERDUE by") || !strings.Contains(logged, trip.TripID) {
		t.Fatalf("FIX17c: the scan ran under a held writer but did not reach its verdict for trip %s:\n%s", trip.TripID, logged)
	}
	if strings.Contains(logged, "CONTENTION") {
		t.Fatalf("FIX17c: the scan waited for the writer:\n%s", logged)
	}
}
