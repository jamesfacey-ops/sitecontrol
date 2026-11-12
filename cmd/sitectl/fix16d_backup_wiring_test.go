package main

// FIX16 D (James, 2026-10-06): the real binary. An old toml (no backup keys)
// starts and /status carries the Backup line; `sitectl -backup <dir>` takes
// a verified copy while the post is serving; `sitectl -restore` refuses
// while the post is running and touches nothing.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

func TestFix16dOldTomlStartsAndStatusCarriesTheBackupLine(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "site-a.db")
	seedStore(t, dbPath)
	addr := freeAddr(t)
	cfg := writeConfig(t, dir, addr, dbPath, nil) // carries no backup key
	raw, _ := os.ReadFile(cfg)
	if strings.Contains(string(raw), "backup") {
		t.Fatal("precondition: the harness config carries a backup key")
	}
	out := startSitectl(t, "-config", cfg)
	waitServing(t, addr, out)
	_, _, body := get(t, addr, "/status")
	if !strings.Contains(body, "data-backup-line=") || !strings.Contains(body, `data-status-tile="backup"`) {
		t.Fatalf("/status carries no Backup line/tile — the scheduler is not wired.\n%.400s", body)
	}
	if !strings.Contains(body, "NONE YET") || !strings.Contains(body, filepath.Join(dir, "backups")) {
		t.Fatalf("a fresh post must say no copy yet and name the default folder beside db_path.\n%s", body)
	}
	if !strings.Contains(out.String(), "backup: nightly copy at 03:00") {
		t.Fatalf("no schedule line at start:\n%s", out.String())
	}
}

func TestFix16dBackupCommandWhileServingAndRestoreRefusesARunningPost(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "site-a.db")
	seedStore(t, dbPath)
	addr := freeAddr(t)
	cfg := writeConfig(t, dir, addr, dbPath, nil)
	out := startSitectl(t, "-config", cfg)
	waitServing(t, addr, out)

	bdir := filepath.Join(dir, "manual")
	code, _, stderr := runSitectl(t, "-config", cfg, "-backup", bdir)
	if code != 0 {
		t.Fatalf("-backup while serving exited %d:\n%s", code, stderr)
	}
	cs, err := store.ListBackups(bdir, dbPath)
	if err != nil || len(cs) != 1 {
		t.Fatalf("-backup left %d copies (%v)", len(cs), err)
	}
	c, err := store.OpenVerified(cs[0].Path)
	if err != nil {
		t.Fatalf("the -backup copy does not verify: %v", err)
	}
	c.Close()

	before, _ := os.ReadFile(dbPath)
	code, _, stderr = runSitectl(t, "-config", cfg, "-restore", cs[0].Path)
	if code == 0 || !strings.Contains(string(stderr), "refusing to restore") || !strings.Contains(string(stderr), "RUNNING") {
		t.Fatalf("-restore on a running post: exit %d\n%s", code, stderr)
	}
	after, _ := os.ReadFile(dbPath)
	if string(before) != string(after) {
		t.Fatal("a refused restore changed the database")
	}
	if m, _ := filepath.Glob(dbPath + ".pre-restore-*"); len(m) > 0 {
		t.Fatal("a refused restore moved the database aside")
	}
}
