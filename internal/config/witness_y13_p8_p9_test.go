// witness_y13_p8_p9_test.go — witness phase (see reports/witness/_BRIEF.md),
// worktree y13-copy. Kills census survivors P8 and P9: two `validate`/`Load`
// refusals that a repair pass could silently disable with nobody noticing,
// because nothing before this file rendered either refusal's exact text.
//
// P8: internal/config/config.go:416 `case c.MusterMinGroupSize < 0:` — the
// only guard against a negative minimum group size reaching a booted post.
//
// P9: internal/config/config.go:396 the unknown-key ("typo?") refusal — the
// guard against a typo'd config key silently reverting a field to its zero
// value, the exact "unset means the default applies" trap this package's own
// Load doc comment names.
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadTOML writes exactly the given TOML text and Loads it. Deliberately NOT
// `loadWith` from config_test.go: that helper's base config ENDS with a
// `[[peers]]` table, so any extra key appended after it (TOML being
// order-sensitive) is parsed as `peers.<key>` rather than top-level — found
// the hard way running TestWitnessP8NegativeMusterMinGroupSizeIsRefused the
// first time, where
// `muster_min_group_size = -1` landed as an unknown key
// `peers.muster_min_group_size` instead of the top-level field. This helper
// puts the whole document under the caller's control instead.
func loadTOML(t *testing.T, doc string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "site.toml")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

const witnessP8P9Base = `
site_id               = "site-a"
listen_addr           = "127.0.0.1:0"
node_epoch            = 1
db_path               = "test.db"
sync_interval_seconds = 10
login_password        = "x"
orientation           = false
site_type             = "text_message"
`

// TestWitnessP8NegativeMusterMinGroupSizeIsRefused is P8's witness. Without
// the guard at config.go:416, a negative muster_min_group_size boots clean
// instead of refusing — an operator would not learn the post is
// misconfigured until the muster's folding behaves strangely, if ever.
func TestWitnessP8NegativeMusterMinGroupSizeIsRefused(t *testing.T) {
	_, err := loadTOML(t, witnessP8P9Base+"muster_min_group_size = -1\n")
	if err == nil {
		t.Fatal("Load succeeded with muster_min_group_size = -1 — a post can now boot with a negative " +
			"minimum group size, which the design never gives meaning to")
	}
	if !strings.Contains(err.Error(), "muster_min_group_size must be >= 0") {
		t.Errorf("Load(muster_min_group_size = -1) error = %q, want it to contain the "+
			"muster_min_group_size refusal", err.Error())
	}
}

// TestWitnessP9UnknownConfigKeyIsRefused is P9's witness. Without the guard
// at config.go:396, a typo'd or stale key is silently accepted and the field
// it would have set quietly keeps its zero value — Load's own doc comment
// calls this "the exact 'unset means the default applies' trap."
func TestWitnessP9UnknownConfigKeyIsRefused(t *testing.T) {
	_, err := loadTOML(t, witnessP8P9Base+"this_key_does_not_exist = true\n")
	if err == nil {
		t.Fatal("Load succeeded with an unrecognized key present — a typo in a config file is now " +
			"silently accepted instead of refusing to boot")
	}
	if !strings.Contains(err.Error(), "unknown key") {
		t.Errorf("Load with an unknown key: error = %q, want it to say \"unknown key\"", err.Error())
	}
	if !strings.Contains(err.Error(), "this_key_does_not_exist") {
		t.Errorf("Load with an unknown key: error = %q, want it to NAME the offending key", err.Error())
	}
}
