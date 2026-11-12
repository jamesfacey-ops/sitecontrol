package store

// backup.go — FIX16 D (James, 2026-10-06): the nightly local backup's copy,
// check, naming and rotation. Everything that touches SQLite lives here,
// behind the store boundary; the timer and the restore procedure are the
// composition root's (cmd/sitectl/backup.go) and the /status judgement is
// web's (web/backup_status.go).
//
// A copy only ever gets its final name AFTER it has passed OpenVerified, so
// "a file with a backup name exists" means "a verified copy exists" —
// /status reads the folder, not a side record, and a copy taken by hand
// with `sitectl -backup` counts the same as a nightly one.
//
//   - BackupFile: a consistent ONLINE copy, by VACUUM INTO on a separate
//     connection. Never a raw file copy: a live WAL database is three files
//     (db, -wal, -shm) and a copy of any one of them mid-write is not a
//     database. VACUUM INTO runs inside one read transaction, so under WAL
//     it sees one committed snapshot and never blocks the post's writer —
//     the only cost to a running post is that the WAL cannot checkpoint past
//     that snapshot until the copy finishes (seconds at season scale).
//   - OpenVerified: the check a copy must pass before it counts — opened
//     read-only, the same IntegrityCheck store.Open runs (PRAGMA
//     integrity_check + the row_hash pass), and the schema_version this
//     binary requires.
//   - InUse: whether another process holds the database open — restore's
//     second guard against overwriting a running post.

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// BackupFile writes a consistent copy of the database at src to dst, which
// must not exist. Safe while the post is running and writing: it opens its
// OWN connection (never the Store's single writer connection, which it
// would hold for the whole copy) and reads one snapshot. The copy is a
// single self-contained file — no -wal/-shm siblings.
func BackupFile(src, dst string) error {
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("store: backup source: %w", err)
	}
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("store: backup target %s already exists", dst)
	}
	// It issues VACUUM INTO and nothing else (query_only would refuse even
	// that: SQLite counts the target as a write). busy_timeout
	// covers the instant the writer holds the WAL index lock.
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(10000)", filepath.ToSlash(src))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("store: backup open %s: %w", src, err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`VACUUM INTO ?`, dst); err != nil {
		os.Remove(dst) // a half-written target is never left to look like a copy
		return fmt.Errorf("store: backup %s -> %s: %w", src, dst, err)
	}
	return nil
}

// OpenVerified opens a backup copy READ-ONLY and refuses it unless it passes
// the checks store.Open applies to a live database: PRAGMA integrity_check,
// the row_hash pass over every event, and a schema_version equal to this
// binary's. The returned Store reads only (Epochs, Watermarks, …); the
// caller closes it. immutable=1: a copy is a static file, so SQLite takes no
// locks and creates no -wal/-shm beside it (a copy on a read-only USB stick
// still verifies).
func OpenVerified(path string) (*Store, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("store: %s is not a file", path)
	}
	dsn := fmt.Sprintf("file:%s?mode=ro&immutable=1", filepath.ToSlash(path))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	var stored string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&stored); err != nil {
		db.Close()
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: %s has no schema_version marker — not a SiteControl database copy", path)
		}
		return nil, fmt.Errorf("store: %s is not a readable SiteControl database: %w", path, err)
	}
	if stored != fmt.Sprint(SchemaVersion) {
		db.Close()
		return nil, fmt.Errorf("store: %s has schema_version %s but this binary requires %d", path, stored, SchemaVersion)
	}
	s := &Store{db: db, Path: path, Clock: SystemClock}
	if err := s.IntegrityCheck(); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: %s failed integrity check: %w", path, err)
	}
	return s, nil
}

// InUse reports whether another connection holds the database at path open
// — a running post always does. It asks SQLite for an EXCLUSIVE lock with no
// wait: under WAL every open connection keeps a shared lock on the file, so
// the request fails while any other process has it open. A missing file is
// not in use. It writes nothing: the lock is taken by a read and released at
// once. Not mode=ro: a read-only open of a WAL file that nobody has open
// fails with a disk I/O error instead of answering (measured).
func InUse(path string) (bool, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	dsn := fmt.Sprintf("file:%s?_pragma=locking_mode(EXCLUSIVE)&_pragma=busy_timeout(0)", filepath.ToSlash(path))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return false, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var n int
	err = db.QueryRow(`PRAGMA user_version`).Scan(&n) // any read takes the lock
	if err == nil {
		return false, nil
	}
	if isBusy(err) {
		return true, nil
	}
	return false, err
}

// isBusy reports whether err is SQLITE_BUSY / SQLITE_LOCKED from the driver
// (matched on text: the modernc error type carries the code, but the text
// is what every SQLite build prints).
func isBusy(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "SQLITE_BUSY") || strings.Contains(msg, "database is locked") || strings.Contains(msg, "SQLITE_LOCKED")
}

// ReadEpochs lists the node_epochs siteID has authored in, as recorded in
// the database at path, opened directly (not through Open: no schema write,
// no integrity pass — this is restore's read of the file it is about to set
// aside, which may be damaged; an error here is reported, not fatal). The
// caller has established that no other process has the file open.
func ReadEpochs(path, siteID string) ([]int64, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(0)", filepath.ToSlash(path))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	s := &Store{db: db, Path: path}
	return s.Epochs(siteID)
}

// Keep is how many verified copies each folder holds; older ones are
// deleted after a new copy verifies, never before.
const BackupKeep = 7

// backupStampLayout is the dated part of a copy's name, local time (what an
// officer reading the folder expects to see). Sorting by it is sorting by
// age.
const backupStampLayout = "20060102-150405"

// BackupDirs is where copies go: Primary always; Mirror too when non-empty.
type BackupDirs struct {
	Primary string
	Mirror  string
}

// backupPrefix is a copy's file-name prefix for the database at dbPath:
// "site-a.db" -> "site-a-backup-". Rotation only ever touches names that
// carry it, so a folder shared with anything else is safe.
func backupPrefix(dbPath string) string {
	base := filepath.Base(dbPath)
	return strings.TrimSuffix(base, filepath.Ext(base)) + "-backup-"
}

// BackupName is the file name of a copy of dbPath taken at t.
func BackupName(dbPath string, t time.Time) string {
	return backupPrefix(dbPath) + t.Format(backupStampLayout) + ".db"
}

// BackupCopy is one verified copy found in a folder.
type BackupCopy struct {
	Path string
	At   time.Time // from the name: when the copy was taken
}

// ListBackups returns the copies of dbPath in dir, newest first. A missing folder
// holds no copies. Unfinished (.partial) files never match.
func ListBackups(dir, dbPath string) ([]BackupCopy, error) {
	ents, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p := backupPrefix(dbPath)
	var out []BackupCopy
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasPrefix(n, p) || !strings.HasSuffix(n, ".db") {
			continue
		}
		at, err := time.ParseInLocation(backupStampLayout, strings.TrimSuffix(strings.TrimPrefix(n, p), ".db"), time.Local)
		if err != nil {
			continue
		}
		out = append(out, BackupCopy{Path: filepath.Join(dir, n), At: at})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out, nil
}

// BackupResult is one completed backup.
type BackupResult struct {
	Path       string        // the verified primary copy
	At         time.Time     // when it was taken
	Took       time.Duration // copy + verify
	MirrorPath string        // the verified second copy, "" if none
	MirrorErr  error         // why the second copy failed (the primary still counts)
	Removed    []string      // older copies rotated out
}

// TakeBackup makes one backup of the database at dbPath: an online copy into
// dirs.Primary under a .partial name, verified (integrity_check, the
// row_hash pass, schema_version), then renamed to its dated name; then the
// same file copied to dirs.Mirror and verified there; then each folder
// rotated to BackupKeep. Safe while the post runs. An error means no new primary
// copy exists.
func TakeBackup(dbPath string, dirs BackupDirs, now time.Time) (BackupResult, error) {
	start := time.Now()
	res := BackupResult{At: now}
	if strings.TrimSpace(dirs.Primary) == "" {
		return res, fmt.Errorf("no backup folder configured")
	}
	if err := os.MkdirAll(dirs.Primary, 0o750); err != nil {
		return res, fmt.Errorf("cannot create the backup folder %s: %w", dirs.Primary, err)
	}
	final := filepath.Join(dirs.Primary, BackupName(dbPath, now))
	if _, err := os.Stat(final); err == nil {
		return res, fmt.Errorf("a copy named %s already exists", filepath.Base(final))
	}
	tmp := final + ".partial"
	os.Remove(tmp)
	if err := BackupFile(dbPath, tmp); err != nil {
		return res, fmt.Errorf("the copy could not be made: %w", err)
	}
	if err := verifyBackup(tmp); err != nil {
		os.Remove(tmp)
		return res, fmt.Errorf("the copy was made but failed its check, so it was discarded: %w", err)
	}
	if err := syncFile(tmp); err != nil {
		os.Remove(tmp)
		return res, fmt.Errorf("the copy could not be flushed to disk: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return res, fmt.Errorf("the checked copy could not be given its name: %w", err)
	}
	SyncDir(dirs.Primary)
	res.Path = final
	res.Removed = append(res.Removed, rotateBackups(dirs.Primary, dbPath)...)
	if m := strings.TrimSpace(dirs.Mirror); m != "" {
		res.MirrorPath, res.MirrorErr = mirrorBackup(final, m, dbPath)
		if res.MirrorErr == nil {
			res.Removed = append(res.Removed, rotateBackups(m, dbPath)...)
		}
	}
	res.Took = time.Since(start)
	return res, nil
}

// mirrorBackup copies a verified primary copy into dir (a plain file copy is right
// here: the source is a closed, self-contained file, not a live database),
// and verifies the result before naming it.
func mirrorBackup(src, dir, dbPath string) (string, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("cannot reach the second backup folder %s: %w", dir, err)
	}
	final := filepath.Join(dir, filepath.Base(src))
	tmp := final + ".partial"
	os.Remove(tmp)
	if err := CopyFileSynced(src, tmp); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("could not write to the second backup folder %s: %w", dir, err)
	}
	if err := verifyBackup(tmp); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("the copy in the second backup folder failed its check: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("could not name the copy in the second backup folder: %w", err)
	}
	SyncDir(dir)
	return final, nil
}

// verifyBackup is the check a copy passes before it counts (store.OpenVerified).
func verifyBackup(path string) error {
	s, err := OpenVerified(path)
	if err != nil {
		return err
	}
	return s.Close()
}

// rotateBackups deletes all but the Keep newest copies of dbPath in dir and
// returns what it removed. Failures to delete are left for the next run.
func rotateBackups(dir, dbPath string) []string {
	cs, err := ListBackups(dir, dbPath)
	if err != nil || len(cs) <= BackupKeep {
		return nil
	}
	var removed []string
	for _, c := range cs[BackupKeep:] {
		if os.Remove(c.Path) == nil {
			removed = append(removed, c.Path)
		}
	}
	return removed
}

// CopyFileSynced copies a CLOSED file (never a live database) to dst,
// which must not exist, and flushes it to disk.
func CopyFileSynced(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func syncFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// SyncDir makes a rename durable where the platform supports it; on
// Windows opening a directory for sync fails and is ignored.
func SyncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
}
