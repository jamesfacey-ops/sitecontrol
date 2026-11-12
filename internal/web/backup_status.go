package web

// backup_status.go — FIX16 D (James, 2026-10-06): the /status Backup line
// and the compact Backup tile. The facts come from the nightly scheduler
// (cmd/sitectl/backup.go) through Server.Backup; the judgement — which
// colour, which word, which sentence — is made here, at render time.

import (
	"fmt"
	"path/filepath"
	"time"
)

// The thresholds (James, 2026-10-06): a nightly copy is normally under 24
// hours old; amber once the newest verified copy is older than
// BackupAmberAfter (one night missed, with slack), red past BackupRedAfter.
const (
	BackupAmberAfter = 30 * time.Hour
	BackupRedAfter   = 36 * time.Hour
)

// BackupFacts is what the scheduler knows, read at Now.
type BackupFacts struct {
	Now     time.Time
	Started time.Time // when the scheduler started (zero: unknown)
	Dir     string    // the main backup folder
	Mirror  string    // the second folder, "" if none

	HasCopy  bool
	Newest   string    // path of the newest verified copy
	NewestAt time.Time // when it was taken
	ListErr  string    // the folder could not be read

	LastAttempt time.Time
	LastErr     string // the last attempt failed, in its own words ("" = it did not)
	MirrorErr   string // the last good attempt's second copy failed
	NextDue     time.Time
}

// backupView is the line and the tile.
type backupView struct {
	Level   string // ok / pending / amber / red
	Word    string // OK / NONE YET / OLD / FAILED / MISSING — state in a word, never colour alone
	Summary string // the plain-words sentence
	Dir     string
	Mirror  string
	HasCopy bool
	Newest  string // file name only
}

func judgeBackup(f BackupFacts) backupView {
	v := backupView{Dir: f.Dir, Mirror: f.Mirror, HasCopy: f.HasCopy}
	var age time.Duration
	if f.HasCopy {
		v.Newest = filepath.Base(f.Newest)
		age = f.Now.Sub(f.NewestAt)
	}
	failedSince := f.LastErr != "" && (!f.HasCopy || f.LastAttempt.After(f.NewestAt))
	switch {
	case failedSince:
		v.Level, v.Word = "red", "FAILED"
		v.Summary = fmt.Sprintf("the last backup, at %s, FAILED: %s", f.LastAttempt.Format("Mon 15:04"), f.LastErr)
		if f.HasCopy {
			v.Summary += fmt.Sprintf(". The newest good copy is %s old", humanAge(age))
		} else {
			v.Summary += ". There is no good copy"
		}
	case f.ListErr != "":
		v.Level, v.Word = "red", "FAILED"
		v.Summary = "the backup folder cannot be read: " + f.ListErr
	case !f.HasCopy && (f.Started.IsZero() || f.Now.Sub(f.Started) > BackupRedAfter):
		v.Level, v.Word = "red", "MISSING"
		v.Summary = "there is no backup copy in the folder"
	case !f.HasCopy:
		v.Level, v.Word = "pending", "NONE YET"
		v.Summary = "no backup copy yet"
		if !f.NextDue.IsZero() {
			v.Summary += " — the first is due at " + f.NextDue.Format("Mon 15:04")
		}
	case age > BackupRedAfter:
		v.Level, v.Word = "red", "OLD"
		v.Summary = fmt.Sprintf("the newest copy is %s old — no nightly backup for more than %s", humanAge(age), humanAge(BackupRedAfter))
	case age > BackupAmberAfter:
		v.Level, v.Word = "amber", "OLD"
		v.Summary = fmt.Sprintf("the newest copy is %s old — last night's backup did not happen", humanAge(age))
	default:
		v.Level, v.Word = "ok", "OK"
		v.Summary = fmt.Sprintf("newest copy taken %s, %s ago", f.NewestAt.Format("Mon 15:04"), humanAge(age))
	}
	if f.MirrorErr != "" && f.Mirror != "" {
		if v.Level == "ok" {
			v.Level = "amber"
		}
		v.Summary += ". The second copy (" + f.Mirror + ") FAILED: " + f.MirrorErr
	}
	return v
}

// humanAge: "45m", "7h 12m", "1d 14h".
func humanAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Minute)
	h := int(d / time.Hour)
	m := int((d % time.Hour) / time.Minute)
	switch {
	case h >= 24:
		return fmt.Sprintf("%dd %dh", h/24, h%24)
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	default:
		return fmt.Sprintf("%dm", m)
	}
}
