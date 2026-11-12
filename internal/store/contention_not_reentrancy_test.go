// contention_not_reentrancy_test.go — ⟦PANIC-1, 2026-09-30⟧ the reentrancy
// guard must tell contention from reentrancy: a wait on the store's single
// connection that is longer than the guard timeout because ANOTHER goroutine
// holds it is not a programming error and must never panic — it waits,
// logs, and completes once the holder lets go. Reentrancy (the same
// goroutine already holding) still panics, and the panic names the holder:
// TestAuthorGuardsAgainstFoldReentrancy keeps that half; this file keeps the
// other.
//
// Fail-before / pass-after: with beginGuarded's timeout branch reverted to
// its unconditional panic, TestLocalTxUnderContentionWaitsAndNeverPanics
// observes a recovered panic and fails; against the guard as it stands the
// LocalTx returns nil after the holder releases and the log carries the
// CONTENTION line.
package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalTxUnderContentionWaitsAndNeverPanics(t *testing.T) {
	old := reentrancyGuardTimeout
	reentrancyGuardTimeout = 150 * time.Millisecond
	t.Cleanup(func() { reentrancyGuardTimeout = old })

	var logBuf bytes.Buffer
	prevOut := log.Writer()
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(prevOut) })

	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	// The holder: another goroutine keeps the single connection inside a
	// LocalTx for well over the (shrunk) guard timeout.
	holding := make(chan struct{})
	release := make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- s.LocalTx(func(tx *sql.Tx) error {
			close(holding)
			<-release
			return nil
		})
	}()
	<-holding
	// PRECONDITION: the connection is genuinely held by a tracked holder
	// on a goroutine that is not this one.
	gid, op, _ := s.holder.snapshot()
	if gid == 0 || gid == goroutineID() || op != "LocalTx" {
		t.Fatalf("PRECONDITION: holder record gid=%d op=%q (this goroutine %d) — the contention is not posed", gid, op, goroutineID())
	}

	// The contender: a LocalTx on THIS goroutine, which must wait past
	// several guard timeouts without panicking and then run.
	ran := false
	var recovered any
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { recovered = recover() }()
		err = s.LocalTx(func(tx *sql.Tx) error { ran = true; return nil })
	}()
	time.Sleep(4 * reentrancyGuardTimeout)
	select {
	case <-done:
		t.Fatalf("the contender returned while the holder still held the connection (ran=%v err=%v panic=%v)", ran, err, recovered)
	default:
	}
	close(release)
	if herr := <-holderDone; herr != nil {
		t.Fatalf("holder LocalTx: %v", herr)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the contender did not complete after the holder released the connection")
	}
	if recovered != nil {
		t.Fatalf("contention PANICKED — the guard judged another goroutine's hold as reentrancy: %v", recovered)
	}
	if err != nil || !ran {
		t.Fatalf("contender LocalTx err=%v ran=%v, want nil/true once the connection was free", err, ran)
	}
	logged := logBuf.String()
	if !strings.Contains(logged, "CONTENTION") || !strings.Contains(logged, "LocalTx on goroutine") || !strings.Contains(logged, "still waiting") {
		t.Fatalf("the wait was not logged as contention naming the holder; log:\n%s", logged)
	}
	if strings.Contains(logged, "REENTRANCY") {
		t.Fatalf("the log calls a contended wait reentrancy:\n%s", logged)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// TestUntrackedReaderHoldIsContentionNotReentrancy: the connection held by
// an untracked reader — a *sql.Rows another goroutine has not finished —
// leaves no holder record; the guard must read "nobody tracked" as
// contention (the never-fatal direction), never as reentrancy.
func TestUntrackedReaderHoldIsContentionNotReentrancy(t *testing.T) {
	old := reentrancyGuardTimeout
	reentrancyGuardTimeout = 150 * time.Millisecond
	t.Cleanup(func() { reentrancyGuardTimeout = old })
	var logBuf bytes.Buffer
	prevOut := log.Writer()
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(prevOut) })

	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := s.Author("site-a", 1, json.RawMessage(fmt.Sprintf(`{"type":"probe","n":%d}`, i)), 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	holding := make(chan struct{})
	release := make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		rows, err := s.db.Query(`SELECT seq FROM event_log`)
		if err != nil {
			holderDone <- err
			return
		}
		rows.Next() // one row read; the cursor — and the connection — stay open
		close(holding)
		<-release
		holderDone <- rows.Close()
	}()
	<-holding
	if gid, op, _ := s.holder.snapshot(); gid != 0 || op != "" {
		t.Fatalf("PRECONDITION: a bare rows cursor must leave no holder record, got gid=%d op=%q", gid, op)
	}
	var recovered any
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { recovered = recover() }()
		err = s.LocalTx(func(tx *sql.Tx) error { return nil })
	}()
	time.Sleep(3 * reentrancyGuardTimeout)
	close(release)
	if herr := <-holderDone; herr != nil {
		t.Fatalf("holder: %v", herr)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the contender did not complete after the reader closed its cursor")
	}
	if recovered != nil {
		t.Fatalf("an untracked reader's hold PANICKED the contender: %v", recovered)
	}
	if err != nil {
		t.Fatalf("contender: %v", err)
	}
	if logged := logBuf.String(); !strings.Contains(logged, "untracked reader") {
		t.Fatalf("the wait was not logged as an untracked reader's hold:\n%s", logged)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// TestReentrancyPanicNamesTheHolder: the other half, sharpened — the panic
// for a genuine same-goroutine hold now says so (it used to say "almost
// always means"), naming the op that holds, so a log reader can tell the
// two cases apart at a glance.
func TestReentrancyPanicNamesTheHolder(t *testing.T) {
	old := reentrancyGuardTimeout
	reentrancyGuardTimeout = 150 * time.Millisecond
	t.Cleanup(func() { reentrancyGuardTimeout = old })
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_ = s.LocalTx(func(tx *sql.Tx) error {
			// The hazard: LocalTx from inside LocalTx on the same goroutine.
			return s.LocalTx(func(*sql.Tx) error { return nil })
		})
	}()
	if recovered == nil {
		t.Fatal("same-goroutine LocalTx-in-LocalTx did not panic — reentrancy is no longer detected")
	}
	msg := fmt.Sprint(recovered)
	if !strings.Contains(msg, "REENTRANCY") || !strings.Contains(msg, "ALREADY HOLDS IT (as LocalTx") {
		t.Fatalf("the reentrancy panic does not name the case and the holder: %s", msg)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
