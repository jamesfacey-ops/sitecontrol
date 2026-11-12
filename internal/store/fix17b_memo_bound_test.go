package store

// FIX17b (memory): the memo holds only values built at the current head,
// and never more than memoMaxEntries of them. sc-stress measured 221-278 MB
// per post at season scale when entries stayed until their own key was
// asked for again.

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

func TestFix17bMemoRetainsOnlyCurrentBoundedEntries(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "memo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	builds := map[string]int{}
	get := func(key string) {
		if _, err := st.Memo(key, func() (any, error) { builds[key]++; return make([]byte, 1<<10), nil }); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		get(fmt.Sprint("k", i))
	}
	if n := st.memoEntries(); n != 3 {
		t.Fatalf("entries = %d, want 3", n)
	}
	// A write lets every entry go at once — none can be served again.
	if err := st.LocalTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT OR REPLACE INTO meta (key, value) VALUES ('zz_fix17b', 'x')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if n := st.memoEntries(); n != 0 {
		t.Fatalf("after a write the memo still holds %d stale entries", n)
	}
	// However many keys are asked for, at most memoMaxEntries are kept, and
	// the one in use is the one kept.
	for i := 0; i < memoMaxEntries+40; i++ {
		get(fmt.Sprint("many", i))
		get("hot") // used between every other build
	}
	if n := st.memoEntries(); n > memoMaxEntries {
		t.Fatalf("entries = %d, over the cap of %d", n, memoMaxEntries)
	}
	if builds["hot"] != 1 {
		t.Fatalf("the most used entry was evicted and rebuilt %d times", builds["hot"])
	}
	before := builds[fmt.Sprint("many", memoMaxEntries+39)]
	get(fmt.Sprint("many", memoMaxEntries+39))
	if builds[fmt.Sprint("many", memoMaxEntries+39)] != before {
		t.Fatal("the newest entry is not served from the memo")
	}
}
