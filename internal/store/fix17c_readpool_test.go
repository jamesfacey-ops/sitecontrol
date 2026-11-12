package store

// FIX17c: reads run on the read pool (readpool.go) and never wait for a
// writer; a goroutine's read view is one snapshot; a goroutine that writes
// reads its own write; the memo never hands out a value built from a
// snapshot other than the caller's.

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fix17cStuckCommit arms s so that the next writer commit stops inside the
// commit window — its writes complete, not yet visible, the writer held —
// until release is called. entered closes once a commit is stuck.
func fix17cStuckCommit(t *testing.T, s *Store) (entered <-chan struct{}, release func()) {
	t.Helper()
	in := make(chan struct{})
	out := make(chan struct{})
	var once sync.Once
	s.BeforeCommit = func() {
		once.Do(func() {
			close(in)
			<-out
		})
	}
	var rel sync.Once
	release = func() { rel.Do(func() { close(out) }) }
	t.Cleanup(release)
	return in, release
}

func fix17cHead(t *testing.T, s *Store) int64 {
	t.Helper()
	wm, err := s.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	return wm
}

// within runs f and fails the test if it has not returned within d — the
// shape every "does not wait for the writer" assertion here takes. A read
// that queues for the stuck writer would otherwise hang the test.
func within(t *testing.T, d time.Duration, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("FIX17c: %s did not return within %s while a commit was stuck — it is waiting for the writer", what, d)
	}
}

func TestFix17cReadsDoNotWaitForAStuckCommitAndSeeTheStateBeforeIt(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < 3; i++ {
		if _, err := s.Author("site-a", 1, tick("a"), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	builds := 0
	memo := func() any {
		v, err := s.Memo("fix17c.head", func() (any, error) {
			builds++
			wm, err := s.Watermark("site-a", 1)
			return wm, err
		})
		if err != nil {
			t.Error(err)
		}
		return v
	}
	if got := memo(); got != int64(3) {
		t.Fatalf("memo before = %v, want 3", got)
	}
	memo() // on every pool connection the memo has seen, the label is known
	before := builds

	entered, release := fix17cStuckCommit(t, s)
	authored := make(chan error, 1)
	go func() {
		_, err := s.Author("site-a", 1, tick("a"), 0, "default")
		authored <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the commit never reached the stall")
	}

	const bound = 2 * time.Second
	within(t, bound, "Watermark", func() {
		if wm := fix17cHead(t, s); wm != 3 {
			t.Errorf("Watermark during the stuck commit = %d, want 3 (the state before it)", wm)
		}
	})
	within(t, bound, "EventsAndHead", func() {
		evs, head, err := s.EventsAndHead("site-a", 1, 1, 0)
		if err != nil {
			t.Error(err)
			return
		}
		// Replication serves from the pool: it must never ship the commit
		// that is still in flight (it can yet fail, or be lost to a crash).
		if len(evs) != 3 || head != 3 {
			t.Errorf("EventsAndHead during the stuck commit = %d events, head %d; want 3, 3", len(evs), head)
		}
	})
	within(t, bound, "DumpProjections", func() {
		if _, err := s.DumpProjections(); err != nil {
			t.Error(err)
		}
	})
	within(t, bound, "LocalRead", func() {
		var n int
		if err := s.LocalRead(func(tx *sql.Tx) error {
			return tx.QueryRow(`SELECT count(*) FROM event_log`).Scan(&n)
		}); err != nil {
			t.Error(err)
		}
		if n != 3 {
			t.Errorf("LocalRead during the stuck commit counts %d events, want 3", n)
		}
	})
	within(t, bound, "a read view", func() {
		end := s.ReadView()
		defer end()
		if wm := fix17cHead(t, s); wm != 3 {
			t.Errorf("a read view during the stuck commit sees head %d, want 3", wm)
		}
		if got := memo(); got != int64(3) {
			t.Errorf("memo in a read view during the stuck commit = %v, want 3", got)
		}
	})
	within(t, bound, "Memo", func() {
		if got := memo(); got != int64(3) {
			t.Errorf("memo during the stuck commit = %v, want 3", got)
		}
	})
	// The snapshot is the one the memo was built from — nothing has been
	// committed since — so the memo SERVES it rather than rebuilding: the
	// expensive pages stay cheap while a write is stuck.
	if builds != before {
		t.Errorf("the memo rebuilt %d times during the stuck commit, want 0 (no commit is visible yet)", builds-before)
	}

	release()
	if err := <-authored; err != nil {
		t.Fatal(err)
	}
	if wm := fix17cHead(t, s); wm != 4 {
		t.Fatalf("after the commit returned, head = %d, want 4", wm)
	}
	if got := memo(); got != int64(4) {
		t.Fatalf("memo after the commit = %v, want 4", got)
	}
}

func TestFix17cAReadViewIsOneSnapshotUntilItsOwnWrite(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Author("site-a", 1, tick("a"), 0, "default"); err != nil {
		t.Fatal(err)
	}
	end := s.ReadView()
	if wm := fix17cHead(t, s); wm != 1 {
		t.Fatalf("head = %d, want 1", wm)
	}
	// Another goroutine commits while this view is open: the view keeps
	// its snapshot — one render, one state.
	done := make(chan error, 1)
	go func() {
		_, err := s.Author("site-a", 1, tick("a"), 0, "default")
		done <- err
	}()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if wm := fix17cHead(t, s); wm != 1 {
		t.Fatalf("FIX17c: a read view saw another goroutine's commit mid-view (head %d, want 1) — the page would be torn", wm)
	}
	var n int
	if err := s.LocalRead(func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT count(*) FROM event_log`).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("FIX17c: LocalRead in the view counted %d events, want the view's 1", n)
	}
	// A write by the view's own goroutine: what it reads next shows it.
	if _, err := s.Author("site-a", 1, tick("a"), 0, "default"); err != nil {
		t.Fatal(err)
	}
	if wm := fix17cHead(t, s); wm != 3 {
		t.Fatalf("FIX17c: after its own write the view reads head %d, want 3 — it does not read its own write", wm)
	}
	end()
	if wm := fix17cHead(t, s); wm != 3 {
		t.Fatalf("after the view, head = %d, want 3", wm)
	}
}

// TestFix17cTheMemoOnlyServesTheCallersOwnSnapshot races writers against
// readers: every read view asks the memo for the head and reads the head
// itself, and the two must always agree — a value built from any other
// snapshot (older or newer) would show as a mismatch.
func TestFix17cTheMemoOnlyServesTheCallersOwnSnapshot(t *testing.T) {
	s := newTestStore(t)
	const writes = 150
	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer stop.Store(true)
		for i := 0; i < writes; i++ {
			if _, err := s.Author("site-a", 1, tick("a"), 0, "default"); err != nil {
				t.Error(err)
				return
			}
			if i%10 == 0 {
				// a read-only LocalTx: a commit that changes nothing
				_ = s.LocalTx(func(tx *sql.Tx) error { return nil })
			}
			if i%2 == 0 {
				time.Sleep(time.Millisecond) // quiet moments, where the memo serves
			}
		}
	}()
	var checks, hits atomic.Int64
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				func() {
					end := s.ReadView()
					defer end()
					built := false
					v, err := s.Memo("fix17c.race", func() (any, error) {
						built = true
						return s.Watermark("site-a", 1)
					})
					if err != nil {
						t.Error(err)
						return
					}
					own, err := s.Watermark("site-a", 1)
					if err != nil {
						t.Error(err)
						return
					}
					if v.(int64) != own {
						t.Errorf("FIX17c: the memo served head %d to a snapshot whose head is %d", v, own)
					}
					checks.Add(1)
					if !built {
						hits.Add(1)
					}
				}()
			}
		}()
	}
	wg.Wait()
	if checks.Load() < 50 {
		t.Fatalf("only %d checks ran — the race measured nothing", checks.Load())
	}
	t.Logf("%d checks against %d writes, %d served from the memo", checks.Load(), writes, hits.Load())
}

func TestFix17cAnotherProcessesCommitIsNeverServedStale(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Author("site-a", 1, tick("a"), 0, "default"); err != nil {
		t.Fatal(err)
	}
	count := func() int64 {
		v, err := s.Memo("fix17c.count", func() (any, error) {
			var n int64
			err := s.LocalRead(func(tx *sql.Tx) error {
				return tx.QueryRow(`SELECT count(*) FROM meta`).Scan(&n)
			})
			return n, err
		})
		if err != nil {
			t.Fatal(err)
		}
		return v.(int64)
	}
	n0 := count()
	other, err := sql.Open("sqlite", filepath.ToSlash(s.Path))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	for i := 0; i < 3; i++ {
		if _, err := other.Exec(`INSERT INTO meta (key, value) VALUES (?, 'x')`, fmt.Sprint("fix17c_other_", i)); err != nil {
			t.Fatal(err)
		}
		if got := count(); got != n0+int64(i)+1 {
			t.Fatalf("FIX17c: after another process's commit #%d the memo served %d, want %d", i+1, got, n0+int64(i)+1)
		}
	}
}

func TestFix17cThePoolIsOffInMemoryAndUnderATempShadow(t *testing.T) {
	m, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if m.pool != nil {
		t.Fatal("an in-memory store opened a read pool — a second connection is a different, empty database")
	}
	s := newTestStore(t)
	if !s.pool.usable() {
		t.Fatal("a file store has no usable read pool")
	}
	if err := s.LocalTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE TEMP VIEW test_tally AS SELECT 'shadow' AS k, 7 AS n`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if s.pool.usable() {
		t.Fatal("FIX17c: the writer holds TEMP schema but reads still go to the pool — a probe's shadow view would be bypassed")
	}
	cols, rows, err := s.ProjectionRowsWhere("test_tally")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0][0] != "shadow" {
		t.Fatalf("a read under the TEMP shadow did not see it: %v %v", cols, rows)
	}
}
