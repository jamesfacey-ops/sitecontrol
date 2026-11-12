package store

// FIX16 D (James, 2026-10-06): the nightly local backup's copy, check and
// rotation (backup.go). Real WAL database, real VACUUM INTO, real
// verification — nothing mocked.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fix16dSeeded(t *testing.T, path string, n int) *Store {
	t.Helper()
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		if _, err := st.Author("site-a", 1, []byte(fmt.Sprintf(`{"type":"fix16d_probe","i":%d}`, i)), 0, ""); err != nil {
			st.Close()
			t.Fatalf("seed author %d: %v", i, err)
		}
	}
	return st
}

// A copy taken while the post is writing is one consistent snapshot: it
// passes integrity_check and the row_hash pass, carries this binary's
// schema_version, and its own stream is gap-free (seq 1..N with the
// watermark at N) — a torn copy would show a watermark ahead of or behind
// the rows it holds, or fail the hash pass.
func TestFix16dBackupWhileWritingIsConsistentAndVerifies(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "site-a.db")
	st := fix16dSeeded(t, db, 200)
	defer st.Close()

	var stop atomic.Bool
	var wrote atomic.Int64
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; !stop.Load(); i++ {
			if _, err := st.Author("site-a", 1, []byte(fmt.Sprintf(`{"type":"fix16d_probe","w":%d,"pad":"%s"}`, i, strings.Repeat("x", 200))), 0, ""); err != nil {
				t.Errorf("writer: %v", err)
				return
			}
			wrote.Add(1)
		}
	}()

	bdir := filepath.Join(dir, "backups")
	base := time.Date(2026, 10, 6, 3, 0, 0, 0, time.Local)
	var heads []int64
	for k := 0; k < 5; k++ {
		before := wrote.Load()
		res, err := TakeBackup(db, BackupDirs{Primary: bdir}, base.Add(time.Duration(k)*time.Minute))
		if err != nil {
			t.Fatalf("backup %d during writes: %v", k, err)
		}
		if wrote.Load() == before {
			time.Sleep(20 * time.Millisecond)
		}
		c, err := OpenVerified(res.Path)
		if err != nil {
			t.Fatalf("copy %d does not verify: %v", k, err)
		}
		wm, err := c.Watermark("site-a", 1)
		if err != nil {
			t.Fatal(err)
		}
		var rows int64
		if err := c.EachEvent("site-a", 1, 1, 0, func(Envelope) error { rows++; return nil }); err != nil {
			t.Fatal(err)
		}
		c.Close()
		if rows != wm {
			t.Fatalf("copy %d is torn: watermark %d but %d rows", k, wm, rows)
		}
		heads = append(heads, wm)
	}
	stop.Store(true)
	wg.Wait()
	if wrote.Load() == 0 {
		t.Fatal("precondition: the writer wrote nothing while the copies were taken — the test proved nothing")
	}
	if heads[len(heads)-1] <= heads[0] {
		t.Fatalf("precondition: writes did not land between copies (heads %v) — the copies were not taken during writes", heads)
	}
	if _, err := os.Stat(db + "-wal"); err != nil {
		t.Fatalf("precondition: the source is not a live WAL database: %v", err)
	}
	// No .partial is ever left behind.
	if m, _ := filepath.Glob(filepath.Join(bdir, "*.partial")); len(m) > 0 {
		t.Fatalf("unfinished copies left in the folder: %v", m)
	}
	t.Logf("copies during %d concurrent writes, own heads %v", wrote.Load(), heads)
}

// Rotation keeps the 7 newest copies and deletes only this database's own
// copies — a stranger's file in the same folder survives.
func TestFix16dRotationKeepsTheSevenNewest(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "site-a.db")
	st := fix16dSeeded(t, db, 3)
	st.Close()
	bdir := filepath.Join(dir, "backups")
	if err := os.MkdirAll(bdir, 0o755); err != nil {
		t.Fatal(err)
	}
	stranger := filepath.Join(bdir, "site-b-backup-20200101-000000.db")
	os.WriteFile(stranger, []byte("not ours"), 0o644)

	base := time.Date(2026, 10, 1, 3, 0, 0, 0, time.Local)
	for d := 0; d < 10; d++ {
		if _, err := TakeBackup(db, BackupDirs{Primary: bdir}, base.AddDate(0, 0, d)); err != nil {
			t.Fatalf("night %d: %v", d, err)
		}
	}
	cs, err := ListBackups(bdir, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != BackupKeep || BackupKeep != 7 {
		t.Fatalf("kept %d copies, want 7", len(cs))
	}
	if !cs[0].At.Equal(base.AddDate(0, 0, 9)) || !cs[6].At.Equal(base.AddDate(0, 0, 3)) {
		t.Fatalf("kept the wrong ones: newest %s, oldest %s", cs[0].At, cs[6].At)
	}
	if _, err := os.Stat(stranger); err != nil {
		t.Fatalf("rotation deleted a file that is not this database's copy: %v", err)
	}
}

// The second folder (a USB drive) gets its own verified copy and its own 7;
// a second folder that cannot be written never fails the primary copy.
func TestFix16dMirrorFolderGetsAVerifiedCopyAndItsFailureIsNotThePrimarys(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "site-a.db")
	st := fix16dSeeded(t, db, 3)
	st.Close()
	res, err := TakeBackup(db, BackupDirs{Primary: filepath.Join(dir, "b"), Mirror: filepath.Join(dir, "usb")}, time.Now())
	if err != nil || res.MirrorErr != nil || res.MirrorPath == "" {
		t.Fatalf("take with a mirror: err=%v mirrorErr=%v mirror=%q", err, res.MirrorErr, res.MirrorPath)
	}
	if err := verifyBackup(res.MirrorPath); err != nil {
		t.Fatalf("the second copy does not verify: %v", err)
	}
	notADir := filepath.Join(dir, "usb-file")
	os.WriteFile(notADir, []byte("x"), 0o644)
	res, err = TakeBackup(db, BackupDirs{Primary: filepath.Join(dir, "b"), Mirror: notADir}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("a failed second copy failed the primary: %v", err)
	}
	if res.MirrorErr == nil {
		t.Fatal("a second folder that is a file reported no failure")
	}
}
