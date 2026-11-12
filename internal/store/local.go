// local.go — the SITE-LOCAL state seam (C7): tables that live in the same
// SQLite file as everything else but are NOT projections. They never
// replicate, never fold, and are deliberately EXCLUDED from DumpProjections —
// C7's deadline captures record B-local receipt instants that exist only at
// the destination site, so dumping them would make demo step 6's cross-site
// dump-and-compare fail on state that is CORRECTLY different per site.
//
// This seam deliberately mirrors the projection seam's shape: the DDL text
// lives in the owning package (not here — same convention as
// Projection.Schema), and access goes through a *sql.Tx callback so the
// *sql.DB stays unexported (C1 review M2) and the single-writer-connection
// discipline stays a store-internal default.
//
// The design's "no wall-clock column in the store" property is about the
// REPLICATED schema and the deadline arithmetic: nothing here changes that.
// A local table MAY hold wall time (C7's reboot bridge must: a monotonic
// instant is meaningless across a restart), but such a value is only ever a
// bridge across a process/host restart — never an ordering, conflict, or
// cross-site comparison input.
package store

import (
	"database/sql"
	"fmt"
)

// RegisterLocalSchema runs idempotent DDL for site-local (non-replicated,
// non-dumped) tables. Startup-time only, like RegisterProjection.
func (s *Store) RegisterLocalSchema(owner, ddl string) error {
	if err := s.execWriter(ddl); err != nil {
		return fmt.Errorf("store: local schema (%s): %w", owner, err)
	}
	return nil
}

// LocalTx runs fn inside one transaction — the sanctioned read/write path for
// site-local tables (the local analogue of "fold functions get a *sql.Tx").
//
// NEVER call LocalTx from inside a fold or any other code holding a store
// transaction: SetMaxOpenConns(1) means the inner Begin would wait forever on
// the outer commit (the same reason folds must never call Author, C2 review
// row 9). C7's OnApplied capture is safe — the hook fires after Apply's
// transaction has committed.
func (s *Store) LocalTx(fn func(tx *sql.Tx) error) error {
	// ⟦A3, 2026-09-29⟧ inside a Transact view fn joins the bound transaction
	// (under its own SAVEPOINT, so a failing fn still leaves nothing) instead
	// of waiting on the pool for a connection that transaction holds — the
	// deadlock the paragraph above describes, closed at the seam rather than
	// left to every caller. The composition reads that ride LocalTx
	// (person/vehicle amendment folds) therefore see a commit's own earlier
	// events, exactly as DumpProjectionWhere does. See transact.go.
	if s.tx != nil {
		return savepoint(s.tx, "sc_local", func() error { return fn(s.tx) })
	}
	tx, release, err := s.begin("LocalTx")
	if err != nil {
		return fmt.Errorf("store: local tx: %w", err)
	}
	defer release()
	defer tx.Rollback()
	// FIX17: most LocalTx calls only read; a commit that changed no row
	// leaves the memo (memo.go) valid. total_changes() is the connection's
	// running count of rows written, so the two readings bracket fn.
	var before, after int64
	if s.memo != nil {
		if err := tx.QueryRow(`SELECT total_changes()`).Scan(&before); err != nil {
			return fmt.Errorf("store: local tx: %w", err)
		}
	}
	if err := fn(tx); err != nil {
		return err
	}
	if s.memo != nil {
		if err := tx.QueryRow(`SELECT total_changes()`).Scan(&after); err != nil {
			return fmt.Errorf("store: local tx: %w", err)
		}
	}
	// ⟦FIX17c⟧ a TEMP object made here (the scoped-read probes' shadow
	// views) lives on the writer only: reads must stay with it (readpool.go).
	s.noteTempSchema(tx)
	if err := s.commit(tx, after != before); err != nil {
		return fmt.Errorf("store: local tx commit: %w", err)
	}
	return nil
}
