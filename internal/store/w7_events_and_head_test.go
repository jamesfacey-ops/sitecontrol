// w7_events_and_head_test.go — W7: Store.EventsAndHead, the pull server's
// one-snapshot read. The pre-fix server shape — Events, then Watermark, two
// transactions — can straddle an Author commit and advertise a head with no
// page to back it; the client refuses that as a protocol anomaly (SYNC ERROR
// state=error, self-clearing). This test runs BOTH shapes, interleaved,
// against the same hot author: the two-call control MUST straddle at least
// once (or the run could not pose the case and says so), and the snapshot
// read must never.
package store

import (
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestEventsAndHeadComeFromOneSnapshotSoAnAdvertisedHeadAlwaysComesWithItsPage(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "w7-snapshot.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	payload := json.RawMessage(`{"type":"tick","k":"w7"}`)

	// A hot author: commits every few hundred microseconds — slower than the
	// reader's loop, so most reads see an empty page and the straddle needs
	// a commit to land exactly between a page read and a head read.
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := st.Author("s", 1, payload, 0, "default"); err != nil {
				t.Error(err)
				return
			}
			time.Sleep(300 * time.Microsecond)
		}
	}()

	// The reader behaves like a caught-up puller: its watermark is the last
	// seq it has seen. straddle := the server advertises head > wm but the
	// page does not begin at wm+1 (empty, in practice).
	straddled := func(page []Envelope, head, wm int64) bool {
		return head > wm && (len(page) == 0 || page[0].Seq != wm+1)
	}
	var wm int64
	control, snapshot, iters := 0, 0, 0
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) && !(control >= 20 && iters >= 500) {
		iters++
		// Control — the pre-fix server shape, two transactions.
		page, err := st.Events("s", 1, wm+1, 200)
		if err != nil {
			t.Fatal(err)
		}
		head, err := st.Watermark("s", 1)
		if err != nil {
			t.Fatal(err)
		}
		if straddled(page, head, wm) {
			control++
		}
		if n := len(page); n > 0 {
			wm = page[n-1].Seq
		}
		// The fix — one snapshot.
		page, head, err = st.EventsAndHead("s", 1, wm+1, 200)
		if err != nil {
			t.Fatal(err)
		}
		if straddled(page, head, wm) {
			snapshot++
		}
		if n := len(page); n > 0 {
			wm = page[n-1].Seq
		}
	}
	close(stop)
	wg.Wait()

	if control == 0 {
		t.Fatalf("fixture: the two-call control never straddled a commit in %d iterations — this run could not pose the case (rule 6), so it proves nothing either way", iters)
	}
	if snapshot != 0 {
		t.Fatalf("EventsAndHead advertised a head with no page to back it %d times in %d iterations (the two-call control straddled %d times)", snapshot, iters, control)
	}
	t.Logf("%d iterations: two-call control straddled a commit %d times; the one-snapshot read 0 times (watermark reached %d)", iters, control, wm)
}
