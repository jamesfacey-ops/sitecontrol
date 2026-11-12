package main

// FIX16 D (James, 2026-10-06): the scheduler and the restore, in-process.
// The real store (real WAL database, real copies) and the real web server's
// /status; only the clock is a seam.

import (
	"bytes"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/store"
	"sitecontrol/internal/web"
)

func fix16dDB(t *testing.T, path string, n int) {
	t.Helper()
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for i := 0; i < n; i++ {
		if _, err := st.Author("site-a", 1, []byte(fmt.Sprintf(`{"type":"fix16d_probe","i":%d}`, i)), 0, ""); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

func fix16dScheduler(dir, db string, now *time.Time) *backupScheduler {
	return &backupScheduler{dbPath: db, dirs: store.BackupDirs{Primary: dir}, hour: 3, now: func() time.Time { return *now }}
}

// fix16dStatusPage renders the real /status over the scheduler's facts.
func fix16dStatusPage(t *testing.T, db string, s *backupScheduler) string {
	t.Helper()
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := web.New(&config.Config{SiteID: "site-a", DisplayName: "Site A", ListenAddr: ":0", NodeEpoch: 1, DBPath: db, SyncIntervalSeconds: 10}, st)
	srv.Backup = s.facts
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/status", nil))
	if rec.Code != 200 {
		t.Fatalf("GET /status = %d", rec.Code)
	}
	return rec.Body.String()
}

// A failed attempt surfaces RED on /status with the reason in plain words,
// even while an older good copy is still young; the next good copy clears
// it.
func TestFix16dFailedBackupSurfacesRedOnStatus(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "site-a.db")
	fix16dDB(t, db, 3)
	bdir := filepath.Join(dir, "backups")
	now := time.Now()
	s := fix16dScheduler(bdir, db, &now)
	s.start()
	if _, err := s.runOnce(); err != nil {
		t.Fatal(err)
	}
	if body := fix16dStatusPage(t, db, s); !strings.Contains(body, `data-backup-line="ok"`) {
		t.Fatal("after a good copy the line is not OK")
	}
	now = now.Add(time.Hour)
	blocked := filepath.Join(dir, "blocked")
	os.WriteFile(blocked, []byte("x"), 0o644) // the folder is a file: nothing can be written
	s.dirs.Primary = blocked
	if _, err := s.runOnce(); err == nil {
		t.Fatal("a backup into a file instead of a folder did not fail")
	}
	body := fix16dStatusPage(t, db, s)
	if !strings.Contains(body, `data-backup-line="red"`) || !strings.Contains(body, `data-backup-level="red"`) ||
		!strings.Contains(body, "an-tile an-tile-crit") || !strings.Contains(body, "FAILED") ||
		!strings.Contains(body, "cannot create the backup folder") {
		t.Fatalf("a failed attempt is not red with its reason on /status:\n%s", body)
	}
	s.dirs.Primary = bdir
	now = now.Add(time.Hour)
	if _, err := s.runOnce(); err != nil {
		t.Fatal(err)
	}
	if body := fix16dStatusPage(t, db, s); !strings.Contains(body, `data-backup-line="ok"`) {
		t.Fatal("a good copy after a failure did not clear the red")
	}
}

// The schedule: the next HH:MM local strictly after now; a post that starts
// with no copy from the last 24 h takes one backupCatchUpDelay after
// starting; a tick past the due time takes the copy and moves the due time
// to tomorrow; a restart with a fresh copy waits for the night.
func TestFix16dScheduleNightlyAndCatchUp(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 10, 6, h, m, 0, 0, time.Local) }
	if got := nextBackupAt(at(2, 59), 3, 0); !got.Equal(at(3, 0)) {
		t.Fatalf("next after 02:59 = %s", got)
	}
	if got := nextBackupAt(at(3, 0), 3, 0); !got.Equal(at(3, 0).AddDate(0, 0, 1)) {
		t.Fatalf("next after 03:00 = %s, want tomorrow", got)
	}
	dir := t.TempDir()
	db := filepath.Join(dir, "site-a.db")
	fix16dDB(t, db, 1)
	bdir := filepath.Join(dir, "backups")
	now := at(14, 0)
	s := fix16dScheduler(bdir, db, &now)
	s.start()
	if !s.nextDue.Equal(now.Add(backupCatchUpDelay)) {
		t.Fatalf("no copy at start: first due %s, want %s", s.nextDue, now.Add(backupCatchUpDelay))
	}
	s.tick()
	if cs, _ := store.ListBackups(bdir, db); len(cs) != 0 {
		t.Fatal("a tick before the due time took a copy")
	}
	now = now.Add(backupCatchUpDelay)
	s.tick()
	if cs, _ := store.ListBackups(bdir, db); len(cs) != 1 {
		t.Fatalf("the catch-up copy was not taken (%d copies)", len(cs))
	}
	if !s.nextDue.Equal(at(3, 0).AddDate(0, 0, 1)) {
		t.Fatalf("after the catch-up, next due %s, want tomorrow 03:00", s.nextDue)
	}
	s2 := fix16dScheduler(bdir, db, &now)
	s2.start()
	if !s2.nextDue.Equal(at(3, 0).AddDate(0, 0, 1)) {
		t.Fatalf("restart with a fresh copy: due %s, want tomorrow 03:00", s2.nextDue)
	}
}

func fix16dRestoreCfg(t *testing.T, db string, epoch int64) *config.Config {
	t.Helper()
	return &config.Config{SiteID: "site-a", ListenAddr: freeAddr(t), NodeEpoch: epoch, DBPath: db}
}

// Restore refuses a running post — by its listen address, and by the
// database being open in another connection — and changes nothing.
func TestFix16dRestoreRefusesARunningPost(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "site-a.db")
	fix16dDB(t, db, 5)
	res, err := store.TakeBackup(db, store.BackupDirs{Primary: filepath.Join(dir, "b")}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	cfg := fix16dRestoreCfg(t, db, 2) // epoch already bumped: only the running guard can refuse
	var out bytes.Buffer

	st, err := store.Open(db) // a running post holds its database open
	if err != nil {
		t.Fatal(err)
	}
	err = restoreBackup(cfg, res.Path, &out, time.Now())
	st.Close()
	if err == nil || !strings.Contains(err.Error(), "open in another process") {
		t.Fatalf("restore over an open database: err = %v", err)
	}

	l, err := net.Listen("tcp", cfg.ListenAddr) // a serving post holds its address
	if err != nil {
		t.Fatal(err)
	}
	err = restoreBackup(cfg, res.Path, &out, time.Now())
	l.Close()
	if err == nil || !strings.Contains(err.Error(), "RUNNING") {
		t.Fatalf("restore while the listen address is held: err = %v", err)
	}
	if m, _ := filepath.Glob(db + ".pre-restore-*"); len(m) > 0 {
		t.Fatalf("a refused restore moved the database aside: %v", m)
	}
}

// Restore verifies the copy first, refuses an un-bumped node_epoch, and
// with the epoch bumped puts the copy in place and keeps the old database.
func TestFix16dRestoreVerifiesAndRequiresTheEpochBump(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "site-a.db")
	fix16dDB(t, db, 5)
	res, err := store.TakeBackup(db, store.BackupDirs{Primary: filepath.Join(dir, "b")}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	fix16dDB(t, db, 3) // more events after the copy: the copy is behind

	var out bytes.Buffer
	junk := filepath.Join(dir, "junk.db")
	os.WriteFile(junk, []byte("SQLite format 3\x00 not really"), 0o644)
	for _, p := range []string{junk, filepath.Join(dir, "missing.db")} {
		if err := restoreBackup(fix16dRestoreCfg(t, db, 2), p, &out, time.Now()); err == nil || !strings.Contains(err.Error(), "failed its check") {
			t.Fatalf("restore of %s: err = %v, want the check refusal", p, err)
		}
	}
	if err := restoreBackup(fix16dRestoreCfg(t, db, 1), res.Path, &out, time.Now()); err == nil || !strings.Contains(err.Error(), "node_epoch = 2") {
		t.Fatalf("restore at the same epoch: err = %v, want the epoch-bump refusal naming the line", err)
	}

	out.Reset()
	if err := restoreBackup(fix16dRestoreCfg(t, db, 2), res.Path, &out, time.Now()); err != nil {
		t.Fatalf("restore with the epoch bumped: %v\n%s", err, out.String())
	}
	if m, _ := filepath.Glob(db + ".pre-restore-*"); len(m) == 0 {
		t.Fatal("the replaced database was not kept aside")
	}
	st, err := store.Open(db)
	if err != nil {
		t.Fatalf("the restored database does not open: %v", err)
	}
	defer st.Close()
	if wm, _ := st.Watermark("site-a", 1); wm != 5 {
		t.Fatalf("restored watermark %d, want the copy's 5", wm)
	}
}
