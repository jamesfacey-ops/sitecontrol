package store

import (
	"path/filepath"
	"strings"
	"testing"
)

// A binary opening a database stored under a different schema_version must
// refuse, not proceed (intra-node analogue of the design's §8.5 refusal).
func TestOpenRefusesForeignSchemaVersion(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	// Simulate a database written by a binary of another schema version.
	if _, err := s.db.Exec(`UPDATE meta SET value = '999' WHERE key = 'schema_version'`); err != nil {
		t.Fatalf("alter stored version: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if _, err := Open(dbPath); err == nil {
		t.Fatal("Open proceeded over schema_version 999; it must refuse")
	} else if !strings.Contains(err.Error(), "999") || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("refusal error should name both versions, got: %v", err)
	}
}

// Reopening a same-version database must still work (the guard must not
// refuse the normal path).
func TestOpenReopensOwnDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	for i := 0; i < 2; i++ {
		s, err := Open(dbPath)
		if err != nil {
			t.Fatalf("open #%d: %v", i+1, err)
		}
		s.Close()
	}
}
