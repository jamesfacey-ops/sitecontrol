// w8_config_key_test.go — W8-21: the config key is RENAMED WITH THE CONCEPT.
// ⟦DR-W8-17⟧
//
// Written from the LOCKED W8 criteria only. No built implementation was read.
//
// ⚠ NARROWNESS ON ITS FACE: NO W8 CRITERION ASSERTS THE VALUE. D-30(d).4
// ruled `0` for the Pad; ICE ROAD IS NOT RULED (§10(a) Q5). The criterion is
// about the KEY, and it stays that way whatever the answer.
//
// ⚠ A RESIDUAL IS CARRIED, NOT FIXED: an operator who DELETES the line rather
// than renaming it gets the legal zero value, WHICH SILENTLY DISABLES FOLDING
// [DOC — reports/DTM-FA1.md:113]. Named as a known residual under this
// criterion so it is not discovered later as a defect.
package config

import (
	"os"
	"path/filepath"

	"strings"
	"testing"
)

const (
	w8OldMusterKey = "muster_min_crew_size"
	w8NewMusterKey = "muster_min_group_size"
)

func w8ConfigModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found above the package directory")
		}
		dir = parent
	}
}

// w8DeployTOMLs derives the shipped deploy configs from the deploy/ directory
// — never a hand-kept list of four names. ⚠ W8's own §2.3 records that a
// hand-kept enumeration of these files is the class this project has measured
// short NINE times, always short.
func w8DeployTOMLs(t *testing.T) []string {
	t.Helper()
	dir := filepath.Join(w8ConfigModuleRoot(t), "deploy")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("PRECONDITION: %s unreadable: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	if len(out) == 0 {
		t.Fatal("PRECONDITION (instrument): no deploy .toml files found — the assertion below could not fail")
	}
	return out
}

// TestW8OldMusterKeyRefusesToBoot is W8-21(b) — PRODUCED, not asserted.
//
// ⭐ ⟦LOCK — OPS-F10: THE REFUSAL NAMES THE REPLACEMENT, pinned as a literal.
// ⚠ The shipped message is `unknown key %q (typo?)` [MEASURED —
// config.go:363-375], and "(typo?)" SENDS SOMEONE HUNTING A MIS-SPELLING
// THAT IS NOT THERE, AT A POST THAT WILL NOT START.⟧
//
// THE ASSERTED LITERAL: "`muster_min_crew_size` was renamed to
// `muster_min_group_size` at the v10 changeover".
//
// ⭐ GENUINE PRE-CHUNK RED: today the old key BOOTS (it is the live key), so
// this fails at the refusal itself.
func TestW8OldMusterKeyRefusesToBoot(t *testing.T) {
	dir := t.TempDir()

	base := strings.Join([]string{
		`site_id = "site-a"`,
		`display_name = "Ice Road A"`,
		`listen_addr = ":8080"`,
		`node_epoch = 1`,
		`db_path = "/tmp/w8.db"`,
		`sync_interval_seconds = 10`,
		`login_password = "w8-test"`,
		`orientation = false`,
		`site_type = "text_message"`,
	}, "\n")

	// --- CONTROL FIRST: the same config WITHOUT the muster key must LOAD.
	// Without it a refusal for any other reason — a missing required field,
	// a typo in the fixture — reads as this criterion passing.
	ctl := filepath.Join(dir, "control.toml")
	if err := os.WriteFile(ctl, []byte(base+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(ctl); err != nil {
		t.Fatalf("W8-21(b) CONTROL FAILED: a config with NO muster key did not load: %v — the fixture is broken, "+
			"so the refusal below would prove nothing about the key", err)
	}

	// --- the produced refusal ---------------------------------------------
	stale := filepath.Join(dir, "stale.toml")
	if err := os.WriteFile(stale, []byte(base+"\n"+w8OldMusterKey+" = 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(stale)
	if err == nil {
		t.Fatalf("W8-21(b) ((m8-50)): a configuration still carrying %q LOADED. It must REFUSE TO BOOT — the key "+
			"is the ONE B-2 REMOVE entry THE PROGRAM ENFORCES AT BOOT, and a post that starts with the stale key "+
			"silently reverts the fold threshold to the zero value", w8OldMusterKey)
	}
	msg := err.Error()
	for _, want := range []string{w8OldMusterKey, w8NewMusterKey, "renamed"} {
		if !strings.Contains(msg, want) {
			t.Errorf("W8-21(b) (OPS-F10): the boot refusal %q does not contain %q.\n"+
				"⚠ THE REFUSAL NAMES THE REPLACEMENT. The shipped message is `unknown key %%q (typo?)`, and "+
				"\"(typo?)\" sends someone hunting a mis-spelling that is not there, AT A POST THAT WILL NOT "+
				"START. The asserted literal: \"`muster_min_crew_size` was renamed to `muster_min_group_size` "+
				"at the v10 changeover\"", msg, want)
		}
	}

	// --- and the NEW key must load, so the rename is a rename and not a
	// deletion.
	fresh := filepath.Join(dir, "fresh.toml")
	if err := os.WriteFile(fresh, []byte(base+"\n"+w8NewMusterKey+" = 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(fresh); err != nil {
		t.Errorf("W8-21(a): a configuration carrying the NEW key %q did not load: %v — the key must be RENAMED, "+
			"not removed", w8NewMusterKey, err)
	}
}
