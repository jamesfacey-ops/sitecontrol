package main

// FIX16 H: `sitectl admin` as a command line — the verbs dispatch, exit
// codes are scriptable, and nothing it prints carries a key or password.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sitecontrol/internal/config"
)

const fix16AdminPlan = `
[[post]]
site_id = "a"
display_name = "Alpha"
base_url = "http://127.0.0.1:1"
listen_addr = "127.0.0.1:1"
site_type = "camp"
orientation = true
[[post]]
site_id = "b"
display_name = "Bravo"
base_url = "http://127.0.0.1:2"
listen_addr = "127.0.0.1:2"
site_type = "text_message"
orientation = false
[[route]]
a = "a"
b = "b"
travel_minutes = 40
`

func admin(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := runAdmin(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestFix16AdminSeasonThenCheckThroughTheCommandLine(t *testing.T) {
	dir := t.TempDir()
	plan := filepath.Join(dir, "plan.toml")
	os.WriteFile(plan, []byte(fix16AdminPlan), 0o600)
	out := filepath.Join(dir, "season")
	code, stdout, stderr := admin(t, "season", "-plan", plan, "-out", out)
	if code != 0 {
		t.Fatalf("season exit %d: %s", code, stderr)
	}
	a, err := config.Load(filepath.Join(out, "a", "site.toml"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := config.Load(filepath.Join(out, "b", "site.toml"))
	code, chk, _ := admin(t, "check", filepath.Join(out, "a", "site.toml"), filepath.Join(out, "b", "site.toml"))
	if code != 0 || !strings.Contains(chk, "MATCH") {
		t.Fatalf("check exit %d: %s", code, chk)
	}
	for _, s := range []string{a.PeerKey, b.PeerKey, a.LoginPassword, b.LoginPassword} {
		if strings.Contains(stdout+stderr+chk, s) {
			t.Fatal("the command line printed a secret")
		}
	}
	// A broken pair: b holds a wrong key for a.
	raw, _ := os.ReadFile(filepath.Join(out, "b", "site.toml"))
	os.WriteFile(filepath.Join(out, "b", "site.toml"), []byte(strings.Replace(string(raw), a.PeerKey, "not-the-key", 1)), 0o600)
	code, chk, _ = admin(t, "check", filepath.Join(out, "a", "site.toml"), filepath.Join(out, "b", "site.toml"))
	if code != 1 || !strings.Contains(chk, "DO NOT MATCH") || !strings.Contains(chk, "b cannot read a") {
		t.Fatalf("check on a broken pair: exit %d\n%s", code, chk)
	}
	// Season into a used folder refuses with exit 1.
	if code, _, stderr := admin(t, "season", "-plan", plan, "-out", out); code != 1 || !strings.Contains(stderr, "not empty") {
		t.Fatalf("reuse: exit %d %s", code, stderr)
	}
}

func TestFix16AdminUsageAndUnknownVerbs(t *testing.T) {
	if code, out, _ := admin(t); code != 0 || !strings.Contains(out, "sitectl admin season") || !strings.Contains(out, "swap status") {
		t.Fatalf("usage: %d %s", code, out)
	}
	if code, _, errs := admin(t, "explode"); code != 2 || !strings.Contains(errs, "unknown command") {
		t.Fatalf("unknown verb: %d %s", code, errs)
	}
	if code, _, errs := admin(t, "swap", "plan"); code != 2 || !strings.Contains(errs, "needs -config") {
		t.Fatalf("missing flag: %d %s", code, errs)
	}
	// swap status against a post that is not running: NOT READY, exit 1.
	dir := t.TempDir()
	plan := filepath.Join(dir, "plan.toml")
	os.WriteFile(plan, []byte(fix16AdminPlan), 0o600)
	admin(t, "season", "-plan", plan, "-out", filepath.Join(dir, "s"))
	code, out, _ := admin(t, "swap", "status", "-config", filepath.Join(dir, "s", "a", "site.toml"))
	if code != 1 || !strings.Contains(out, "NOT READY: Alpha") || !strings.Contains(out, "is it running?") {
		t.Fatalf("status of a stopped post: %d %s", code, out)
	}
}
