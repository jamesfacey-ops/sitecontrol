// w7_onauthored_test.go — W7 / Y-17: Store.OnAuthored, the §8.9 poke fast
// path's one seam. Pins the contract the engine relies on: ONCE per COMMITTED
// authoring transaction (standalone Author; a Transact however many events;
// a nested Transact once for the outermost), AFTER the commit with the
// connection released (the hook reads the store through the pool and sees
// its own event), NEVER on a rollback, never for a Transact that authored
// nothing, and nil-safe.
package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func TestOnAuthoredFiresOncePerCommittedAuthoringTransactionAfterCommit(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "w7.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	payload := json.RawMessage(`{"type":"tick","k":"w7"}`)

	var calls int
	var seen []int64
	st.OnAuthored = func() {
		calls++
		// Through the POOL store, inside the hook: if the hook ran before
		// release() this would wait on the single connection (the reentrancy
		// guard turns that into a panic naming the rule); if it ran before
		// commit, it would not see the event.
		wm, err := st.Watermark("s", 1)
		if err != nil {
			t.Errorf("Watermark inside the hook: %v", err)
		}
		seen = append(seen, wm)
	}

	if _, err := st.Author("s", 1, payload, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(seen) != 1 || seen[0] != 1 {
		t.Fatalf("after one standalone Author: calls=%d seen=%v, want 1 call that saw watermark 1", calls, seen)
	}

	// A Transact authoring two events: ONE firing, after the commit, seeing
	// both.
	err = st.Transact("two", func(v *Store) error {
		for i := 0; i < 2; i++ {
			if _, err := v.Author("s", 1, payload, 0, "default"); err != nil {
				return err
			}
		}
		if calls != 1 {
			return errors.New("the hook fired INSIDE the transaction, before the commit")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || seen[1] != 3 {
		t.Fatalf("after a two-event Transact: calls=%d seen=%v, want 2 calls, the second seeing watermark 3", calls, seen)
	}

	// A refused (rolled-back) Transact that authored: nothing fires, nothing
	// is on the log.
	refused := errors.New("refused")
	if err := st.Transact("refused", func(v *Store) error {
		if _, err := v.Author("s", 1, payload, 0, "default"); err != nil {
			return err
		}
		return refused
	}); !errors.Is(err, refused) {
		t.Fatalf("Transact returned %v, want the callback's refusal", err)
	}
	if calls != 2 {
		t.Fatalf("the hook fired for a rolled-back Transact (calls=%d)", calls)
	}
	if wm, _ := st.Watermark("s", 1); wm != 3 {
		t.Fatalf("watermark after the refused Transact = %d, want 3", wm)
	}

	// A committed Transact that authored nothing: nothing fires.
	if err := st.Transact("no author", func(v *Store) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("the hook fired for a Transact that authored nothing (calls=%d)", calls)
	}

	// Nested Transact: the outermost commit fires exactly once.
	err = st.Transact("outer", func(v *Store) error {
		return v.Transact("inner", func(v2 *Store) error {
			_, err := v2.Author("s", 1, payload, 0, "default")
			return err
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || seen[2] != 4 {
		t.Fatalf("after a nested Transact: calls=%d seen=%v, want 3 calls, the third seeing watermark 4", calls, seen)
	}

	// Nil hook: the default, and every caller that never wires one.
	st.OnAuthored = nil
	if _, err := st.Author("s", 1, payload, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if wm, _ := st.Watermark("s", 1); wm != 5 {
		t.Fatalf("watermark with a nil hook = %d, want 5", wm)
	}
}
