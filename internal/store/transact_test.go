// transact_test.go — ⟦A3, 2026-09-29⟧ the one-commit-one-transaction seam
// (transact.go): every event of a commit lands or none does; a view reads
// its own uncommitted folds; the events it commits replay identically at a
// replica; and the ordinary one-event-per-commit Author is untouched.
package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// tallyRows counts test_tally rows through the store's own read path.
func tallyRows(t *testing.T, s *Store) int {
	t.Helper()
	b, err := s.DumpProjection("test_tally")
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(b, &rows); err != nil {
		t.Fatal(err)
	}
	return len(rows)
}

func logRows(t *testing.T, s *Store, site string) int {
	t.Helper()
	evs, err := s.Events(site, 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	return len(evs)
}

// TestTransactRollsBackEveryEventOnError is the fix's contract: two events
// authored inside one Transact, then a refusal — nothing survives, not the
// log rows, not the folds, not the watermark. It also asserts the
// PRECONDITION that the view saw both events before the error (watermark 2,
// two fold rows), so the rollback is proven over writes that really
// happened rather than over a callback that authored nothing.
func TestTransactRollsBackEveryEventOnError(t *testing.T) {
	s := newTestStore(t)
	refused := errors.New("refused after two events")
	var sawWatermark int64
	var sawRows int
	err := s.Transact("test", func(v *Store) error {
		if !v.InTransaction() {
			t.Fatal("the callback was not handed a Transact view")
		}
		for _, k := range []string{"a", "b"} {
			if _, err := v.Author("site-a", 1, tick(k), 0, "default"); err != nil {
				t.Fatalf("author %s inside Transact: %v", k, err)
			}
		}
		var err error
		if sawWatermark, err = v.Watermark("site-a", 1); err != nil {
			t.Fatal(err)
		}
		sawRows = tallyRows(t, v)
		return refused
	})
	if !errors.Is(err, refused) {
		t.Fatalf("Transact returned %v, want the callback's own error unchanged", err)
	}
	if sawWatermark != 2 || sawRows != 2 {
		t.Fatalf("precondition: inside the transaction the view saw watermark %d and %d fold rows, want 2 and 2 — the events were not written, so the rollback below proves nothing", sawWatermark, sawRows)
	}
	if wm, _ := s.Watermark("site-a", 1); wm != 0 {
		t.Errorf("after the refused Transact the watermark is %d, want 0", wm)
	}
	if n := logRows(t, s, "site-a"); n != 0 {
		t.Errorf("after the refused Transact the log holds %d event(s), want 0", n)
	}
	if n := tallyRows(t, s); n != 0 {
		t.Errorf("after the refused Transact the fold holds %d row(s), want 0", n)
	}
	if s.InTransaction() {
		t.Error("the ORIGINAL store became a view")
	}
}

// TestTransactCommitsContiguousSeqsThatReplayIdentically: three events in
// one commit get seqs 1, 2, 3, are all on the log once committed, and a
// replica applying them as one batch folds to the byte-identical dump.
func TestTransactCommitsContiguousSeqsThatReplayIdentically(t *testing.T) {
	s := newTestStore(t)
	var seqs []int64
	err := s.Transact("test", func(v *Store) error {
		for _, k := range []string{"a", "b", "a"} {
			env, err := v.Author("site-a", 1, tick(k), 0, "default")
			if err != nil {
				return err
			}
			seqs = append(seqs, env.Seq)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seqs) != 3 || seqs[0] != 1 || seqs[1] != 2 || seqs[2] != 3 {
		t.Fatalf("seqs inside one Transact = %v, want [1 2 3]", seqs)
	}
	if wm, _ := s.Watermark("site-a", 1); wm != 3 {
		t.Fatalf("committed watermark = %d, want 3", wm)
	}
	evs, err := s.Events("site-a", 1, 1, 0)
	if err != nil || len(evs) != 3 {
		t.Fatalf("committed log = %d events (%v), want 3", len(evs), err)
	}
	replica := newTestStore(t)
	results, err := replica.ApplyBatch(evs)
	if err != nil || len(results) != 3 {
		t.Fatalf("replica apply: %v %v", results, err)
	}
	if !bytes.Equal(mustDump(t, s), mustDump(t, replica)) {
		t.Fatalf("the replica's dump diverges from the author's — a commit's events do not replay identically")
	}
}

// TestTransactPanicDiscardsAndLeavesTheStoreUsable: a panic inside the
// callback rolls the transaction back (the deferred Rollback), and the store
// authors normally afterwards — the connection was not leaked.
func TestTransactPanicDiscardsAndLeavesTheStoreUsable(t *testing.T) {
	s := newTestStore(t)
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("the callback's panic did not propagate")
			}
		}()
		_ = s.Transact("test", func(v *Store) error {
			if _, err := v.Author("site-a", 1, tick("a"), 0, "default"); err != nil {
				t.Fatal(err)
			}
			panic("boom")
		})
	}()
	if wm, _ := s.Watermark("site-a", 1); wm != 0 {
		t.Fatalf("after the panic the watermark is %d, want 0", wm)
	}
	env, err := s.Author("site-a", 1, tick("a"), 0, "default")
	if err != nil || env.Seq != 1 {
		t.Fatalf("the store is not usable after a panicking Transact: seq %d, %v", env.Seq, err)
	}
}

// TestAuthorInsideTransactKeepsItsOwnAllOrNothing: the SAVEPOINT. A fold
// that fails on the second event leaves no trace of that event even when the
// callback swallows the error and commits — the first event lands whole, the
// second not at all (no log row, no watermark step).
func TestAuthorInsideTransactKeepsItsOwnAllOrNothing(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "sp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	for _, p := range testProjections() {
		if p.Name == "test_seen" {
			// Poison: refuse the payload "poison" AFTER test_tally has folded
			// it (projection order is by name: test_seen runs after
			// test_tally), so a partial fold exists to be discarded.
			inner := p.Apply
			p.Apply = func(tx *sql.Tx, env Envelope) error {
				if strings.Contains(string(env.Payload), "poison") {
					return errors.New("poisoned fold")
				}
				return inner(tx, env)
			}
		}
		if err := s.RegisterProjection(p); err != nil {
			t.Fatal(err)
		}
	}
	err = s.Transact("test", func(v *Store) error {
		if _, err := v.Author("site-a", 1, tick("ok"), 0, "default"); err != nil {
			return err
		}
		if _, err := v.Author("site-a", 1, tick("poison"), 0, "default"); err == nil {
			t.Fatal("precondition: the poisoned fold did not fail")
		}
		return nil // swallowed — the savepoint, not the caller, must have cleaned up
	})
	if err != nil {
		t.Fatal(err)
	}
	if wm, _ := s.Watermark("site-a", 1); wm != 1 {
		t.Errorf("watermark = %d, want 1 (the poisoned event must not have advanced it)", wm)
	}
	if n := logRows(t, s, "site-a"); n != 1 {
		t.Errorf("log holds %d event(s), want 1", n)
	}
	if n := tallyRows(t, s); n != 1 {
		t.Errorf("test_tally holds %d row(s), want 1 — the poisoned event's partial fold survived", n)
	}
}

// TestTransactViewReadsItsOwnFolds: inside the transaction a projection read
// sees the just-authored row; the ordinary store does not until commit.
func TestTransactViewReadsItsOwnFolds(t *testing.T) {
	s := newTestStore(t)
	var inside int
	err := s.Transact("test", func(v *Store) error {
		if _, err := v.Author("site-a", 1, tick("a"), 0, "default"); err != nil {
			return err
		}
		b, err := v.DumpProjectionWhere("test_tally", Eq{Column: "k", Value: "a"})
		if err != nil {
			return err
		}
		inside = bytes.Count(b, []byte(`"k"`))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if inside != 1 {
		t.Fatalf("the view read %d matching row(s) for its own fold, want 1", inside)
	}
	if n := tallyRows(t, s); n != 1 {
		t.Fatalf("after commit the store holds %d row(s), want 1", n)
	}
}

// TestNestedTransactRunsFlatAndTheOuterDecides: a Transact on a view runs on
// the same transaction; the outer error discards the inner's events too.
func TestNestedTransactRunsFlatAndTheOuterDecides(t *testing.T) {
	s := newTestStore(t)
	outer := errors.New("outer refused")
	err := s.Transact("outer", func(v *Store) error {
		if err := v.Transact("inner", func(vv *Store) error {
			if vv != v {
				t.Fatal("a nested Transact opened a second view instead of running flat")
			}
			_, err := vv.Author("site-a", 1, tick("a"), 0, "default")
			return err
		}); err != nil {
			return err
		}
		if wm, _ := v.Watermark("site-a", 1); wm != 1 {
			t.Fatalf("precondition: the inner event is not visible to the outer view (watermark %d)", wm)
		}
		return outer
	})
	if !errors.Is(err, outer) {
		t.Fatal(err)
	}
	if wm, _ := s.Watermark("site-a", 1); wm != 0 {
		t.Fatalf("the inner Transact's event survived the outer's refusal (watermark %d)", wm)
	}
}

// TestApplyInsideTransactIsRefused: replication never applies from inside a
// door's commit.
func TestApplyInsideTransactIsRefused(t *testing.T) {
	s := newTestStore(t)
	err := s.Transact("test", func(v *Store) error {
		_, err := v.ApplyBatch(foreignStream("site-b", 1, 1))
		return err
	})
	if !errors.Is(err, errApplyInsideTransact) {
		t.Fatalf("ApplyBatch inside Transact returned %v, want the refusal", err)
	}
}
