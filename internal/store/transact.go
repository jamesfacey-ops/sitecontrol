// transact.go — ONE COMMIT, ONE TRANSACTION ⟦A3 / cluster A, 2026-09-29⟧.
//
// Author appends exactly one event per call, each its own committed
// transaction (log.go). Every door that records a MOVEMENT authors more than
// one: a departure is a vehicle co-mint, the officer-confirmed record
// changes, then the trip; a check-in is the degenerate trip then its
// arrival; a resumed check-in is the ticked orientations then the arrival.
// Until now those were N separate commits, so a refusal — or a crash — after
// the first left the earlier events on the log and replicating while the
// officer read "Nothing was recorded" (FINDINGS H-4, H-5, H-6; GateCheckIn's
// own CRASH WINDOW note, which records that "a shared-transaction atomic
// author was not added" at the time for scope reasons, not design ones).
//
// Transact is that shared transaction. It hands fn a Store VIEW bound to one
// *sql.Tx: every Author, every LocalTx and every projection read made
// through the view runs on that transaction, so a door's later steps SEE the
// folds of its earlier events (the trip's snapshot reads the vehicle record
// it just co-minted; buildArrivalVisits reads the orientation it just
// recorded), and the whole set commits when fn returns nil or is discarded
// — every event, every fold, every watermark advance — when fn returns an
// error or panics. Nothing about the events themselves changes: each still
// gets the next contiguous seq, its own clock reading, its own Finalize, and
// the same payload bytes it would have carried authored alone, which is what
// keeps a replica's ApplyBatch of the same envelopes folding identically
// (Finalize derives from the raw tables, so once-per-event here versus
// once-per-batch there reaches the same state — the F28 property).
//
// ⚠⚠ THE ONE RULE FOR fn: USE THE STORE IT IS HANDED, NEVER THE ONE IT
// CLOSED OVER. The store allows exactly one open connection
// (SetMaxOpenConns(1), store.go), and the view holds it for the whole
// callback. A read or write through the ORIGINAL store from inside fn waits
// for that connection forever: an Author/LocalTx there is caught by the
// reentrancy guard's timeout and panics with a message naming this rule; a
// bare read (DumpProjectionWhere, Watermark, …) has no guard and hangs
// silently — the worst failure mode at a gate hut, so the web doors that use
// Transact pass the view into every helper they call rather than reaching
// for s.Store. A nested Transact on the view runs fn flat on the same
// transaction (the outermost owns commit and rollback), so a domain door
// that is itself transactional can be composed into a larger commit.
//
// The view is a shallow copy of the Store carrying the transaction: the
// projections, owners, clock and path are shared and immutable after Open,
// so nothing is duplicated and nothing needs synchronising. It is valid
// only for the duration of fn — a view kept past the return is a handle on
// a finished transaction, and using it is a bug this package does not try
// to detect.
package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// dbtx is what Author, LocalTx and the projection reads need from *sql.DB
// and *sql.Tx alike: the one open connection, or the one bound transaction.
type dbtx interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// conn is the read/write target for this Store: the bound transaction inside
// Transact, the connection pool outside it. Every non-startup query in this
// package goes through it, so a Transact view never reaches the pool — which
// would be a deadlock on the store's single connection — and a plain Store
// behaves exactly as it always has.
func (s *Store) conn() dbtx {
	if s.tx != nil {
		return s.tx
	}
	// ⟦FIX17c⟧ a read outside a write goes to the read pool — the bound
	// view's snapshot or a pool connection — never queueing for the writer
	// (readpool.go). With the pool off, the one connection, as before.
	if q := s.reader(); q != nil {
		return q
	}
	return s.db
}

// Transact runs fn against a view of this Store bound to ONE transaction and
// commits it when fn returns nil. On an error (or a panic, via the deferred
// rollback) nothing fn authored, folded or wrote through the view survives —
// not a log row, not a watermark. op names the caller for the reentrancy
// guard's diagnostics ("gate check-out", "gate resume"), the way Author and
// LocalTx name themselves.
//
// Inside an existing Transact (s is already a view) fn simply runs on the
// same transaction: composition is flat, and the outermost caller decides.
func (s *Store) Transact(op string, fn func(txst *Store) error) error {
	if s.tx != nil {
		return fn(s)
	}
	// OnAuthored (W7 / Y-17): ONE firing per committed Transact that authored
	// anything, however many events fn appended (view.authored is set by
	// Author's in-transaction arm). Registered BEFORE begin so it runs after
	// release() has freed the connection, and never on a rollback.
	var view Store
	committed := false
	defer func() {
		if committed && view.authored {
			s.fireOnAuthored()
		}
	}()
	tx, release, err := s.begin("Transact(" + op + ")")
	if err != nil {
		return fmt.Errorf("store: transact %s: %w", op, err)
	}
	defer release()
	defer tx.Rollback()
	view = *s
	view.tx = tx
	if err := fn(&view); err != nil {
		return err
	}
	if err := s.commit(tx, true); err != nil {
		return fmt.Errorf("store: transact %s commit: %w", op, err)
	}
	committed = true
	return nil
}

// InTransaction reports whether this Store is a Transact view — a bound
// transaction rather than the pool. Exported for tests and for callers that
// must refuse to run inside one (ApplyBatch).
func (s *Store) InTransaction() bool { return s.tx != nil }

// errApplyInsideTransact: replication applies on the unbound store, never
// from inside a door's commit — an apply inside a Transact would interleave a
// peer's events with this post's half-authored commit under one watermark
// advance, and its own Begin would deadlock on the bound connection anyway.
var errApplyInsideTransact = errors.New("store: apply inside Transact is not supported — deliver peer events on the unbound store")

// savepoint runs fn inside a named SQLite SAVEPOINT on tx: released on
// success, rolled back to (and released) on error, so one Author or LocalTx
// inside a Transact keeps the all-or-nothing contract it has on its own —
// a fold error after the log-row INSERT leaves no partial rows in the
// enclosing transaction even if a caller were to swallow the error and
// carry on. SQLite savepoints nest, and the same name may be reused: RELEASE
// and ROLLBACK TO act on the most recent savepoint of that name.
func savepoint(tx *sql.Tx, name string, fn func() error) error {
	if _, err := tx.Exec("SAVEPOINT " + name); err != nil {
		return fmt.Errorf("store: savepoint %s: %w", name, err)
	}
	if err := fn(); err != nil {
		_, _ = tx.Exec("ROLLBACK TO " + name)
		_, _ = tx.Exec("RELEASE " + name)
		return err
	}
	if _, err := tx.Exec("RELEASE " + name); err != nil {
		return fmt.Errorf("store: release savepoint %s: %w", name, err)
	}
	return nil
}
