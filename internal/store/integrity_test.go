// integrity_test.go — regression cover for the F25/F26 fixes
// (reports/fix-integrity.md):
//
//   - F25: a single-byte flip confined to event_log.payload's TEXT content
//     (no indexed column touched) used to leave Open, RegisterProjection
//     and DumpProjections all reporting success over silently wrong data.
//   - F26: deleting the schema_version meta row from an otherwise-populated
//     database used to make Open treat it as freshly-created-at-current-
//     version instead of refusing.
package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// corruptAllOccurrences flips one byte in every raw occurrence of needle in
// the file at path. SQLite's freelist can hold a stale, already-orphaned
// copy of a page from before a b-tree split moved a row elsewhere (observed
// live while building this test: a fresh store + one marked row + padding
// left one copy in freelist garbage and the live copy elsewhere in the same
// file) — corrupting only the first match can silently hit garbage rather
// than the row a read actually returns. Corrupting every match sidesteps
// needing to know which one is live.
func corruptAllOccurrences(t *testing.T, path string, needle []byte) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read raw: %v", err)
	}
	corrupted := append([]byte(nil), raw...)
	n := 0
	for search := corrupted; ; {
		i := bytes.Index(search, needle)
		if i < 0 {
			break
		}
		flipAt := i + len(needle)/2
		search[flipAt] ^= 0xFF
		n++
		search = search[i+len(needle):]
	}
	if n == 0 {
		t.Fatal("needle not found in raw file bytes — test premise broken")
	}
	if err := os.WriteFile(path, corrupted, 0o644); err != nil {
		t.Fatalf("write corrupted: %v", err)
	}
	// A lingering WAL/SHM sidecar could "heal" the read by replaying an
	// uncorrupted copy of the same page over the corrupted main file, which
	// would not reflect the real "the .db file itself is corrupted"
	// scenario (e.g. a backup or copy of just the .db file).
	os.Remove(path + "-wal")
	os.Remove(path + "-shm")
	return n
}

// F25: content-only corruption inside event_log.payload (never part of any
// index) must not be silently served. PRAGMA integrity_check alone does not
// catch this (verified manually while building the fix, see
// reports/fix-integrity.md); the row_hash re-verification pass does.
func TestOpenCatchesContentOnlyPayloadCorruption(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.db")

	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	const marker = "ZZCONTENTMARKERZZCONTENTMARKERZZ"
	if _, err := s.Author("site-a", 1, json.RawMessage(`{"type":"tick","k":"marker","note":"`+marker+`"}`), 0, "default"); err != nil {
		t.Fatalf("author: %v", err)
	}
	// Real, populated file rather than a degenerate one-row db: enough rows
	// to force at least one b-tree page split, which is what produced the
	// freelist-garbage copy this test's corruption strategy accounts for.
	for i := 0; i < 50; i++ {
		if _, err := s.Author("site-a", 1, tick("k"), 0, "default"); err != nil {
			t.Fatalf("author pad %d: %v", i, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	corruptAllOccurrences(t, path, []byte(marker))

	if _, err := Open(path); err == nil {
		t.Fatal("Open succeeded over a database with corrupted event_log.payload content; it must refuse")
	} else if !strings.Contains(err.Error(), "corrupted") {
		t.Fatalf("refusal error should say the database is corrupted, got: %v", err)
	}
}

// A database with no corruption at all must still open and read back
// exactly what was written — the row_hash pass must not false-positive on
// honest data (it recomputes Envelope.Hash() the same way insertAndFold
// does at write time).
func TestOpenAcceptsUncorruptedRowHashes(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clean.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for i := 0; i < 25; i++ {
		if _, err := s.Author("site-a", 1, tick("k"), 0, "default"); err != nil {
			t.Fatalf("author %d: %v", i, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	s2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopen a genuinely clean database was refused: %v", err)
	}
	defer s2.Close()
	if err := s2.IntegrityCheck(); err != nil {
		t.Fatalf("IntegrityCheck on a clean database: %v", err)
	}
}

// F26: a schema_version marker missing from an otherwise-populated database
// must refuse, not be silently re-seeded as current. This is external DB
// surgery — the app itself only ever INSERTs the row (ON CONFLICT DO
// NOTHING originally, now guarded by Open reading it first), never deletes
// it — but a botched manual recovery or a `.recover` pass could produce
// exactly this shape.
func TestOpenRefusesMissingSchemaVersionMarkerOnNonEmptyDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	if _, err := s.Author("site-a", 1, tick("k"), 0, "default"); err != nil {
		t.Fatalf("author: %v", err)
	}
	if _, err := s.db.Exec(`DELETE FROM meta WHERE key = 'schema_version'`); err != nil {
		t.Fatalf("delete marker: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	_, err = Open(dbPath)
	if err == nil {
		t.Fatal("Open silently re-seeded schema_version on a database with existing event_log rows; it must refuse")
	}
	msg := err.Error()
	if !strings.Contains(msg, "no schema_version marker") {
		t.Fatalf("refusal error should name the missing-marker condition distinctly from a version mismatch, got: %v", err)
	}
	if strings.Contains(msg, "requires") {
		t.Fatalf("refusal error should NOT read like the ordinary version-mismatch message (no stored/wanted version to report here), got: %v", err)
	}
}

// The companion case: a database with NO schema_version marker AND no rows
// anywhere else is genuinely fresh (this is exactly what every first Open
// of a brand-new file looks like) and must seed the marker and proceed, not
// be refused by the F26 guard.
func TestOpenSeedsMarkerOnGenuinelyFreshEmptyDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fresh.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open a brand-new database was refused: %v", err)
	}
	defer s.Close()
	v, err := s.StoredSchemaVersion()
	if err != nil {
		t.Fatalf("stored schema version: %v", err)
	}
	if v != fmt.Sprint(SchemaVersion) {
		t.Fatalf("stored schema version = %q, want current SchemaVersion %d", v, SchemaVersion)
	}
}
