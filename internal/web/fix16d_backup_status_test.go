package web

// FIX16 D (James, 2026-10-06): /status shows a Backup line — the newest
// verified copy's time, age and folder — amber past 30 h, red past 36 h or
// after a failed attempt with the reason in plain words; and a compact
// Backup tile in the Signal tile's style on the post's own status page, not
// on the dispatch host's view.

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func fix16dStatus(t *testing.T, srv *Server, host string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/status", nil)
	if host != "" {
		req.Host = host
	}
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("GET /status (host %q) = %d", host, rec.Code)
	}
	return rec.Body.String()
}

// The age thresholds, on the served page: under 30 h OK, past 30 h amber,
// past 36 h red — and the age is the newest VERIFIED copy's.
func TestFix16dBackupAgeThresholdsOnStatus(t *testing.T) {
	taken := time.Date(2026, 10, 5, 3, 0, 0, 0, time.Local)
	for _, c := range []struct {
		age        time.Duration
		level      string
		word, tile string
	}{
		{1 * time.Hour, "ok", "OK", `class="an-tile"`},
		{29*time.Hour + 59*time.Minute, "ok", "OK", `class="an-tile"`},
		{30*time.Hour + time.Minute, "amber", "OLD", "an-tile an-tile-warn"},
		{35*time.Hour + 59*time.Minute, "amber", "OLD", "an-tile an-tile-warn"},
		{36*time.Hour + time.Minute, "red", "OLD", "an-tile an-tile-crit"},
		{72 * time.Hour, "red", "OLD", "an-tile an-tile-crit"},
	} {
		srv := newTestServer(t)
		srv.Backup = func() BackupFacts {
			return BackupFacts{Now: taken.Add(c.age), Started: taken, Dir: "/b", HasCopy: true,
				Newest: "/b/test-backup-20261005-030000.db", NewestAt: taken}
		}
		body := fix16dStatus(t, srv, "")
		if !strings.Contains(body, `data-backup-line="`+c.level+`"`) || !strings.Contains(body, c.tile) ||
			!strings.Contains(body, "<strong>"+c.word+"</strong>") {
			t.Errorf("age %s: want %s / %s / %s", c.age, c.level, c.word, c.tile)
		}
	}
}

// Red with the reason; a second-folder failure is amber with its reason; no
// copy yet is neutral until the post has run past the red threshold.
func TestFix16dBackupFailureAndEmptyStates(t *testing.T) {
	now := time.Date(2026, 10, 6, 9, 0, 0, 0, time.Local)
	cases := []struct {
		name  string
		f     BackupFacts
		level string
		says  []string
	}{
		{"failed after a good copy", BackupFacts{Now: now, Started: now.Add(-48 * time.Hour), Dir: "/b", HasCopy: true, Newest: "/b/x.db",
			NewestAt: now.Add(-6 * time.Hour), LastAttempt: now.Add(-time.Hour), LastErr: "the copy could not be made: disk full"},
			"red", []string{"FAILED", "disk full", "newest good copy is 6h 0m old"}},
		{"failed with no copy", BackupFacts{Now: now, Started: now.Add(-time.Hour), Dir: "/b", LastAttempt: now, LastErr: "cannot create the backup folder /b"},
			"red", []string{"FAILED", "cannot create the backup folder", "no good copy"}},
		{"second folder failed", BackupFacts{Now: now, Started: now, Dir: "/b", Mirror: "E:\\bk", HasCopy: true, Newest: "/b/x.db", NewestAt: now.Add(-time.Hour),
			MirrorErr: "could not write to the second backup folder"}, "amber", []string{"second copy", "E:\\bk", "could not write"}},
		{"none yet", BackupFacts{Now: now, Started: now, Dir: "/b", NextDue: now.Add(2 * time.Minute)}, "pending", []string{"NONE YET", "first is due"}},
		{"none after 37 h", BackupFacts{Now: now, Started: now.Add(-37 * time.Hour), Dir: "/b"}, "red", []string{"MISSING"}},
	}
	for _, c := range cases {
		v := judgeBackup(c.f)
		if v.Level != c.level {
			t.Errorf("%s: level %s, want %s (%s)", c.name, v.Level, c.level, v.Summary)
		}
		for _, s := range c.says {
			if !strings.Contains(v.Word+" "+v.Summary, s) {
				t.Errorf("%s: %q does not say %q", c.name, v.Word+" "+v.Summary, s)
			}
		}
	}
}

// The tile is on the post's own status page and not on the dispatch host's
// view; nothing renders when the backup is not wired.
func TestFix16dBackupTileOnThePostNotOnDispatchAndAbsentWhenUnwired(t *testing.T) {
	srv := newTestServer(t)
	if body := fix16dStatus(t, srv, ""); strings.Contains(body, "data-backup-line") || strings.Contains(body, `data-status-tile="backup"`) {
		t.Fatal("a Backup line rendered with no backup wired")
	}
	now := time.Now()
	srv.Backup = func() BackupFacts {
		return BackupFacts{Now: now, Started: now, Dir: "/b", HasCopy: true, Newest: "/b/test-backup-x.db", NewestAt: now.Add(-time.Hour)}
	}
	body := fix16dStatus(t, srv, "")
	if !strings.Contains(body, `data-status-tile="backup"`) || !strings.Contains(body, "test-backup-x.db") || !strings.Contains(body, "newest verified copy") {
		t.Fatal("the post's own /status lacks the Backup tile or the copy's name")
	}
	if body := fix16dStatus(t, srv, "dispatch.example.com"); strings.Contains(body, `data-status-tile="backup"`) {
		t.Fatal("the Backup tile rendered on the dispatch host")
	}
}
