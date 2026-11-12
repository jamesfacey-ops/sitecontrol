package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestAuthorGuardsAgainstFoldReentrancy is a hardening test for the campaign
// concurrency angle's recommendation (reports/test-concurrency.md): a fold
// that calls back into LocalTx (or Author) on its own store, on the same
// goroutine that holds the outer apply transaction, can never complete —
// SetMaxOpenConns(1) means the inner Begin blocks forever waiting for the
// connection the outer transaction is holding on that same goroutine. This
// is not a reachable path today: Projection.Apply's signature gives a fold
// no parameter to reach *Store, and every real Author/LocalTx call site was
// read and confirmed outside any fold's call graph. This test proves the
// GUARD, not the (already-closed) hole: it wires a synthetic fold that takes
// the unreachable-in-production path deliberately.
//
// Fail-before / pass-after, verified by hand rather than only asserted:
// with beginGuarded's body reverted to a plain s.db.Begin() (i.e.
// log.go/local.go calling s.db.Begin() directly, as before this
// hardening), this hangs past its own 2s outer deadline and fails; against
// production code as it stands, the shrunk reentrancyGuardTimeout below
// fires well inside that 2s and it passes.
//
// beginGuarded is built on BeginTx precisely so a timed-out wait acquires
// NOTHING (see reentrancy_guard.go's doc comment) — by the time recover()
// below observes the panic, Author's own deferred Rollback has already run
// on the same goroutine and there is no transaction left open anywhere. s
// is closed synchronously at the end of the test body (not left to
// t.Cleanup ordering) so there is nothing async still touching the file
// when t.TempDir()'s own cleanup deletes it — an earlier draft of this
// guard (background goroutine racing a timer against plain Begin()) could
// leave a second, never-closed transaction alive well past this point,
// which is exactly what made Windows refuse to delete the temp file.
func TestAuthorGuardsAgainstFoldReentrancy(t *testing.T) {
	old := reentrancyGuardTimeout
	reentrancyGuardTimeout = 200 * time.Millisecond
	t.Cleanup(func() { reentrancyGuardTimeout = old })

	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	reentrant := Projection{
		Name:    "test_reentrant",
		Table:   "test_reentrant",
		OrderBy: "k",
		Schema:  `CREATE TABLE IF NOT EXISTS test_reentrant (k TEXT PRIMARY KEY) STRICT;`,
		Apply: func(tx *sql.Tx, env Envelope) error {
			// The hazard the guard exists for: calling back into LocalTx from
			// inside a fold, on the store that fold is folding for.
			return s.LocalTx(func(tx *sql.Tx) error { return nil })
		},
	}
	if err := s.RegisterProjection(reentrant); err != nil {
		s.Close()
		t.Fatalf("register: %v", err)
	}

	done := make(chan any, 1)
	go func() {
		defer func() { done <- recover() }()
		s.Author("site-a", 1, json.RawMessage(`{"type":"whatever"}`), 0, "default")
	}()

	select {
	case r := <-done:
		if r == nil {
			s.Close()
			t.Fatal("fold reentrancy into LocalTx did not panic — guard did not fire")
		}
		msg := fmt.Sprint(r)
		if !strings.Contains(msg, "LocalTx") {
			s.Close()
			t.Fatalf("panic message should name the reentrant call site (LocalTx), got: %s", msg)
		}
	case <-time.After(2 * time.Second):
		s.Close()
		t.Fatal("Author call is still blocked well past the shrunk guard timeout — reentrancy hang was not converted to a panic")
	}

	// Explicit, synchronous close — deliberately not left to t.Cleanup — so
	// nothing is still touching the database file when TempDir's own
	// cleanup (registered before this test's Open call) deletes it.
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
