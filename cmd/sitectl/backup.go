package main

// backup.go — FIX16 D (James, 2026-10-06): the gate laptop's nightly local
// backup — the in-process timer that takes it, and the offline restore.
// The copy itself (VACUUM INTO, verification, naming, the 7-deep rotation,
// the second folder) is internal/store's (store/backup.go); the /status
// judgement is web's (web/backup_status.go). deploy/BACKUP-RESTORE.md is
// the operator's page.

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/store"
	"sitecontrol/internal/web"
)

// backupCatchUpDelay: a post that starts with no copy from the last 24 hours
// (a new post, or a laptop that was off at the nightly time) takes one this
// long after starting, instead of waiting for the next night.
const backupCatchUpDelay = 2 * time.Minute

// backupCheckEvery is how often the timer looks at the wall clock. The
// schedule compares wall time rather than sleeping until the due instant,
// so a laptop that slept through it, or a clock corrected across it, takes
// the copy at the next look rather than never.
const backupCheckEvery = 30 * time.Second

// backupScheduler takes the nightly copy in-process — the same on Windows
// and Linux, no OS scheduler to install — and holds what /status shows.
type backupScheduler struct {
	dbPath string
	dirs   store.BackupDirs
	hour   int
	minute int

	// now and take are seams for tests; nil means time.Now / store.TakeBackup.
	now  func() time.Time
	take func(dbPath string, dirs store.BackupDirs, now time.Time) (store.BackupResult, error)

	mu          sync.Mutex
	started     time.Time
	nextDue     time.Time
	lastAttempt time.Time
	lastErr     string
	mirrorErr   string
	running     bool
}

func newBackupScheduler(cfg *config.Config) *backupScheduler {
	h, m := cfg.BackupAt()
	return &backupScheduler{
		dbPath: cfg.DBPath,
		dirs:   store.BackupDirs{Primary: cfg.BackupDirPath(), Mirror: cfg.BackupMirrorPath()},
		hour:   h,
		minute: m,
	}
}

func (s *backupScheduler) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// nextBackupAt is the first HH:MM local strictly after t.
func nextBackupAt(t time.Time, hour, minute int) time.Time {
	n := time.Date(t.Year(), t.Month(), t.Day(), hour, minute, 0, 0, t.Location())
	if !n.After(t) {
		n = time.Date(t.Year(), t.Month(), t.Day()+1, hour, minute, 0, 0, t.Location())
	}
	return n
}

// start fixes the first due time: backupCatchUpDelay from now if no copy
// from the last 24 hours exists, else the next HH:MM.
func (s *backupScheduler) start() {
	now := s.clock()
	due := nextBackupAt(now, s.hour, s.minute)
	if cs, _ := store.ListBackups(s.dirs.Primary, s.dbPath); len(cs) == 0 || now.Sub(cs[0].At) > 24*time.Hour {
		if c := now.Add(backupCatchUpDelay); c.Before(due) {
			due = c
		}
	}
	s.mu.Lock()
	s.started, s.nextDue = now, due
	s.mu.Unlock()
	also := ""
	if s.dirs.Mirror != "" {
		also = " and " + s.dirs.Mirror
	}
	log.Printf("backup: nightly copy at %02d:%02d local into %s%s; next copy %s", s.hour, s.minute, s.dirs.Primary, also, due.Format("2006-01-02 15:04"))
}

// run takes a copy whenever one is due, until ctx is done.
func (s *backupScheduler) run(ctx context.Context) {
	s.start()
	t := time.NewTicker(backupCheckEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.tick()
		}
	}
}

// tick takes a copy if one is due (and schedules the next).
func (s *backupScheduler) tick() {
	s.mu.Lock()
	due := !s.nextDue.IsZero() && !s.clock().Before(s.nextDue) && !s.running
	s.mu.Unlock()
	if due {
		s.runOnce()
	}
}

// runOnce takes one copy now and records the outcome for /status.
func (s *backupScheduler) runOnce() (store.BackupResult, error) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return store.BackupResult{}, fmt.Errorf("a backup is already running")
	}
	s.running = true
	s.mu.Unlock()

	now := s.clock()
	take := s.take
	if take == nil {
		take = store.TakeBackup
	}
	res, err := take(s.dbPath, s.dirs, now)

	s.mu.Lock()
	s.running = false
	s.lastAttempt = now
	s.nextDue = nextBackupAt(now, s.hour, s.minute)
	if err != nil {
		s.lastErr = err.Error()
	} else {
		s.lastErr, s.mirrorErr = "", ""
		if res.MirrorErr != nil {
			s.mirrorErr = res.MirrorErr.Error()
		}
	}
	next := s.nextDue
	s.mu.Unlock()

	if err != nil {
		log.Printf("backup: FAILED: %v — the previous copies are kept; next attempt %s", err, next.Format("2006-01-02 15:04"))
		return res, err
	}
	log.Printf("backup: verified copy %s (%s)", res.Path, res.Took.Round(time.Millisecond))
	if res.MirrorErr != nil {
		log.Printf("backup: SECOND COPY FAILED: %v", res.MirrorErr)
	} else if res.MirrorPath != "" {
		log.Printf("backup: verified second copy %s", res.MirrorPath)
	}
	return res, nil
}

// facts is what /status judges (web.BackupFacts): the folder as it is now,
// and the last attempt.
func (s *backupScheduler) facts() web.BackupFacts {
	now := s.clock()
	s.mu.Lock()
	f := web.BackupFacts{
		Now: now, Started: s.started, Dir: s.dirs.Primary, Mirror: s.dirs.Mirror,
		LastAttempt: s.lastAttempt, LastErr: s.lastErr, MirrorErr: s.mirrorErr, NextDue: s.nextDue,
	}
	s.mu.Unlock()
	cs, err := store.ListBackups(s.dirs.Primary, s.dbPath)
	if err != nil {
		f.ListErr = err.Error()
	}
	if len(cs) > 0 {
		f.HasCopy, f.Newest, f.NewestAt = true, cs[0].Path, cs[0].At
	}
	return f
}

// restoreBackup replaces this post's database with a backup copy. It refuses
// unless every check passes, in this order, and changes nothing on a
// refusal:
//
//  1. The post is not running: its listen address is free AND no other
//     process holds the database open (store.InUse).
//  2. The copy passes the same check every copy passed when it was taken
//     (store.OpenVerified: integrity_check, the row_hash pass,
//     schema_version).
//  3. node_epoch in the config is ABOVE every epoch this post has authored
//     in — in the copy, and in the database being replaced when it can
//     still be read. A restored copy is behind its peers: they hold this
//     post's newer events at that epoch. Authoring at the same epoch again
//     would write different events under sequence numbers the peers already
//     hold, which they refuse for good (prefix_diverged, sync-protocol §2).
//     The re-image rule (§8.2/§8.6) is the cure, and the same one a wiped
//     post uses: bump node_epoch, and on start the post pulls its own older
//     epoch's newer events back from the peers before it serves anything.
//
// The replaced database (and any -wal/-shm beside it) is renamed aside to
// <db>.pre-restore-<stamp>, never deleted.
func restoreBackup(cfg *config.Config, src string, out io.Writer, now time.Time) error {
	// 1. Not running.
	ln, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("refusing to restore: the post appears to be RUNNING — %s is in use (%v). Stop the SiteControl service first", cfg.ListenAddr, err)
	}
	ln.Close()
	inUse, err := store.InUse(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("refusing to restore: cannot tell whether %s is in use: %w", cfg.DBPath, err)
	}
	if inUse {
		return fmt.Errorf("refusing to restore: %s is open in another process — the post (or a sitectl -dump/-report) is still running. Stop it first", cfg.DBPath)
	}

	// 2. The copy is good.
	bs, err := store.OpenVerified(src)
	if err != nil {
		return fmt.Errorf("refusing to restore: the copy failed its check: %w", err)
	}
	copyEpochs, err := bs.Epochs(cfg.SiteID)
	if err == nil {
		var wm int64
		if len(copyEpochs) > 0 {
			wm, _ = bs.Watermark(cfg.SiteID, copyEpochs[len(copyEpochs)-1])
		}
		fmt.Fprintf(out, "restore: %s verified (integrity_check ok, every event's hash ok, schema_version %d); this post's own events in it: epochs %v, last seq %d\n", src, store.SchemaVersion, copyEpochs, wm)
	}
	bs.Close()
	if err != nil {
		return fmt.Errorf("refusing to restore: reading the copy: %w", err)
	}

	// 3. The epoch rule.
	highest, highestFrom := int64(0), ""
	for _, e := range copyEpochs {
		if e > highest {
			highest, highestFrom = e, "the copy"
		}
	}
	liveNote := ""
	if _, statErr := os.Stat(cfg.DBPath); statErr == nil {
		live, err := store.ReadEpochs(cfg.DBPath, cfg.SiteID)
		if err != nil {
			liveNote = fmt.Sprintf("the database being replaced could not be read (%v), so its epochs are unknown — make sure node_epoch is above every epoch this post has ever authored in (a peer's /status sync section lists this post's streams)", err)
		}
		for _, e := range live {
			if e > highest {
				highest, highestFrom = e, "the database being replaced"
			}
		}
	}
	if cfg.NodeEpoch <= highest {
		return fmt.Errorf("refusing to restore: node_epoch in the config is %d, but %s already holds this post's events at epoch %d. "+
			"A restored copy is behind its peers, and authoring at epoch %d again would fork this post's history (peers refuse it for good). "+
			"Edit the config: node_epoch = %d (the re-image rule), then run this restore again", cfg.NodeEpoch, highestFrom, highest, highest, highest+1)
	}
	if liveNote != "" {
		fmt.Fprintf(out, "restore: WARNING: %s\n", liveNote)
	}

	// Copy in under a temporary name, check it again, then swap.
	tmp := cfg.DBPath + ".restoring"
	os.Remove(tmp)
	if err := store.CopyFileSynced(src, tmp); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("restore: copying %s: %w", src, err)
	}
	v, err := store.OpenVerified(tmp)
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("restore: the copied file failed its check (disk problem?): %w", err)
	}
	v.Close()
	aside := fmt.Sprintf("%s.pre-restore-%s", cfg.DBPath, now.Format("20060102-150405"))
	for _, suf := range []string{"", "-wal", "-shm"} {
		if _, err := os.Stat(cfg.DBPath + suf); err == nil {
			if err := os.Rename(cfg.DBPath+suf, aside+suf); err != nil {
				os.Remove(tmp)
				return fmt.Errorf("restore: setting the current database aside: %w", err)
			}
			if suf == "" {
				fmt.Fprintf(out, "restore: the database it replaces is kept as %s\n", aside)
			}
		}
	}
	if err := os.Rename(tmp, cfg.DBPath); err != nil {
		return fmt.Errorf("restore: putting the copy in place (the old database is at %s): %w", aside, err)
	}
	store.SyncDir(filepath.Dir(cfg.DBPath))
	fmt.Fprintf(out, "restore: %s is now the copy. Start the post: on start it rejoins — it pulls this post's own newer events back from its peers first, then theirs — before it serves. Check /status: every peer OK and no 'self-history pending'.\n", cfg.DBPath)
	return nil
}
