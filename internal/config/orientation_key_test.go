// orientation_key_test.go — SCHEMA-11 step 4 (D-88/D-91; D-77's pattern):
// the per-site `orientation` key is REQUIRED with NO DEFAULT. A TOML
// without it refuses to load, naming the key, both legal values and what
// each means; with it, `HasOrientation()` reads exactly what was written.
// Produced red first by removing the nil check from validate() — see
// reports/witness/STEP4-ORIENTATION.md §7.
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const orientationKeyTestBase = `
site_id               = "site-x"
listen_addr           = "127.0.0.1:0"
node_epoch            = 1
db_path               = "test.db"
sync_interval_seconds = 10
login_password        = "x"
site_type             = "text_message"
`

func loadOrientationKeyTOML(t *testing.T, body string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "site.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

// TestOrientationKeyAbsentIsRefusedAtLoadNamingTheLine is the refusal.
// Asserted on the text an administrator reads on the box: a refusal that
// only says "invalid" sends somebody hunting.
func TestOrientationKeyAbsentIsRefusedAtLoadNamingTheLine(t *testing.T) {
	c, err := loadOrientationKeyTOML(t, orientationKeyTestBase)
	if err == nil {
		t.Fatalf("a config with NO orientation key LOADED (HasOrientation()=%v) — D-88/D-91: the key is required; a default in either direction is a silent per-topology guess (D-77)", c.HasOrientation())
	}
	if c != nil {
		t.Fatalf("Load returned a non-nil *Config alongside its error: %+v", c)
	}
	for _, want := range []string{
		"orientation",
		"required",
		"no default",
		"orientation = true",
		"orientation = false",
		"D-91",
		"D-88",
		"D-77",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal text lacks %q\n  got: %v", want, err)
		}
	}
}

// Positive controls: the SAME TOML with the key written loads, and the read
// is exactly what was written — `false` is not "absent", and `true` is
// `true`.
func TestOrientationKeyPresentLoadsAndReadsAsWritten(t *testing.T) {
	for _, tc := range []struct {
		line string
		want bool
	}{
		{"orientation = false\n", false},
		{"orientation = true\n", true},
	} {
		c, err := loadOrientationKeyTOML(t, orientationKeyTestBase+tc.line)
		if err != nil {
			t.Fatalf("%q: Load refused: %v — the fixture is broken, so the refusal test proves nothing", strings.TrimSpace(tc.line), err)
		}
		if c.Orientation == nil {
			t.Fatalf("%q: Orientation decoded as nil", strings.TrimSpace(tc.line))
		}
		if got := c.HasOrientation(); got != tc.want {
			t.Fatalf("%q: HasOrientation() = %v, want %v", strings.TrimSpace(tc.line), got, tc.want)
		}
	}
	// A Config literal built without the key (web-layer tests) reads false,
	// never panics — the nil is the "absent" the loader refuses; here it is
	// simply "not a work site".
	if (&Config{}).HasOrientation() {
		t.Fatal("an unset Orientation read as true")
	}
}
