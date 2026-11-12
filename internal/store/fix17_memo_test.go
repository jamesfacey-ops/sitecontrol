package store

// fix17_memo_test.go — FIX17 (perf): Memo's one promise is that a value is
// never served across a change to what it was built from. Each write path
// the store has, and each way the data can change under it, must rebuild;
// a read must not; a Transact view must never see a memoised value.

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestFix17MemoRebuildsAfterEveryKindOfChangeAndOnlyThen(t *testing.T) {
	s := newTestStore(t)
	builds := 0
	get := func() int {
		t.Helper()
		v, err := s.Memo("fix17.test", func() (any, error) { builds++; return builds, nil })
		if err != nil {
			t.Fatal(err)
		}
		return v.(int)
	}
	expect := func(step string, rebuilt bool) {
		t.Helper()
		before := builds
		get()
		if got := builds > before; got != rebuilt {
			t.Fatalf("FIX17 memo, %s: rebuilt=%v, want %v", step, got, rebuilt)
		}
	}

	expect("first read", true)
	expect("second read, nothing written", false)

	if _, err := s.Author("site-a", 1, tick("a"), 0, "default"); err != nil {
		t.Fatal(err)
	}
	expect("after Author", true)

	if results, err := s.ApplyBatch(foreignStream("site-b", 1, 3)); err != nil || len(results) != 3 {
		t.Fatalf("apply: %v %v", results, err)
	}
	expect("after ApplyBatch that applied", true)
	if _, err := s.ApplyBatch(foreignStream("site-b", 1, 3)); err != nil {
		t.Fatal(err)
	}
	expect("after ApplyBatch of duplicates only", false)

	if err := s.Transact("fix17", func(v *Store) error {
		_, err := v.Author("site-a", 1, tick("b"), 0, "default")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	expect("after Transact commit", true)

	if err := s.LocalTx(func(tx *sql.Tx) error {
		var n int
		return tx.QueryRow(`SELECT count(*) FROM event_log`).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	expect("after a read-only LocalTx", false)

	if err := s.LocalTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS fix17_local (n INTEGER)`); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO fix17_local VALUES (1)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	expect("after a LocalTx that wrote a row", true)

	if err := s.LocalTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE TEMP VIEW fix17_shadow AS SELECT 1`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	expect("after temp DDL (a probe view shadowing a table)", true)

	// Another connection's commit: a second process beside the service.
	other, err := sql.Open("sqlite", filepath.ToSlash(s.Path))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err := other.Exec(`INSERT INTO fix17_local VALUES (2)`); err != nil {
		t.Fatal(err)
	}
	expect("after another connection's commit", true)
	expect("read again, nothing written", false)

	// A Transact view builds every time and never sees the shared value.
	if err := s.Transact("fix17 view", func(v *Store) error {
		for i := 0; i < 2; i++ {
			before := builds
			if _, err := v.Memo("fix17.test", func() (any, error) { builds++; return builds, nil }); err != nil {
				return err
			}
			if builds == before {
				t.Fatalf("FIX17 memo: a Transact view was served a memoised value (read %d)", i+1)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// A failed build keeps nothing.
	if _, err := s.Memo("fix17.err", func() (any, error) { return nil, sql.ErrNoRows }); err != sql.ErrNoRows {
		t.Fatalf("FIX17 memo: build error = %v, want it returned as is", err)
	}
	called := false
	if _, err := s.Memo("fix17.err", func() (any, error) { called = true; return 1, nil }); err != nil || !called {
		t.Fatalf("FIX17 memo: a failed build was kept (called=%v, err=%v)", called, err)
	}
}
