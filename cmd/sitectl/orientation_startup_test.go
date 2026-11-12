// orientation_startup_test.go — tester-go (reports/witness/TESTER-step4.md):
// the D-88 `orientation` key refusal at the BINARY, where the operator meets
// it, not at config.Load. A post whose TOML lacks the key, or carries a
// malformed value, must exit non-zero, name the FILE on stderr, name the
// line (to write / that is wrong), and write nothing to stdout. Both cases
// watched red on a mutant of config.go before they were trusted green.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfigWithOrientationLine is writeConfig with the orientation line
// under the caller's control: "" omits it; anything else is written verbatim.
func writeConfigWithOrientationLine(t *testing.T, dir, orientationLine string) string {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "site_id = %q\n", "site-a")
	fmt.Fprintf(&b, "display_name = %q\n", "Site A")
	fmt.Fprintf(&b, "listen_addr = %q\n", "127.0.0.1:0")
	fmt.Fprintf(&b, "node_epoch = 1\n")
	fmt.Fprintf(&b, "peer_key = %q\n", "demo-key")
	fmt.Fprintf(&b, "db_path = %q\n", filepath.Join(dir, "site-a.db"))
	fmt.Fprintf(&b, "sync_interval_seconds = 3600\n")
	fmt.Fprintf(&b, "muster_min_group_size = 2\n")
	fmt.Fprintf(&b, "login_password = %q\n", "demo-pass")
	fmt.Fprintf(&b, "site_type = %q\n", "text_message") // R-4/R-5: required, no default — present so the refusals below are about orientation alone
	if orientationLine != "" {
		fmt.Fprintf(&b, "%s\n", orientationLine)
	}
	p := filepath.Join(dir, "site.toml")
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	return p
}

// CONTROL FIRST: the same file WITH the key starts far enough to get past
// config (it then fails or serves on other grounds; what matters is that the
// refusal below is about the key and not about this fixture).
func TestOrientationStartupControlTheSameConfigWithTheKeyPassesConfigLoad(t *testing.T) {
	cfg := writeConfigWithOrientationLine(t, t.TempDir(), "orientation = false")
	// -dump exits after loading config and opening the store; if config had
	// refused, stderr would carry the orientation refusal and exit non-zero.
	code, _, stderr := runSitectl(t, "-config", cfg, "-dump")
	if strings.Contains(string(stderr), "orientation") {
		t.Fatalf("CONTROL FAILED: a config WITH `orientation = false` was refused on the key (exit %d):\n%s", code, stderr)
	}
}

func TestConfigWithoutTheOrientationKeyRefusesToStartNamingTheFileAndTheLineToWrite(t *testing.T) {
	cfg := writeConfigWithOrientationLine(t, t.TempDir(), "")
	code, stdout, stderr := runSitectl(t, "-config", cfg)
	if code == 0 {
		t.Fatalf("sitectl EXITED 0 on a config with no `orientation` key — D-88/D-91: at a pad that is a post entering every un-oriented arrival.\nstderr: %q", stderr)
	}
	for _, want := range []string{cfg, "orientation", "required", "orientation = true", "orientation = false"} {
		if !strings.Contains(string(stderr), want) {
			t.Errorf("the refusal on stderr lacks %q\n  got: %s", want, stderr)
		}
	}
	if len(stdout) != 0 {
		t.Fatalf("a refused start wrote %d bytes to STDOUT: %q", len(stdout), stdout)
	}
}

func TestConfigWithAMalformedOrientationValueRefusesToStartNamingTheFileAndTheLine(t *testing.T) {
	cfg := writeConfigWithOrientationLine(t, t.TempDir(), `orientation = "true"`)
	raw, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	lineNo := 1 + strings.Count(string(raw)[:strings.Index(string(raw), "orientation")], "\n")
	code, stdout, stderr := runSitectl(t, "-config", cfg)
	if code == 0 {
		t.Fatalf("sitectl EXITED 0 on `orientation = \"true\"` (a string, not a bool) — a malformed value must refuse, never coerce.\nstderr: %q", stderr)
	}
	for _, want := range []string{cfg, "orientation", fmt.Sprintf("line %d", lineNo)} {
		if !strings.Contains(string(stderr), want) {
			t.Errorf("the refusal on stderr lacks %q\n  got: %s", want, stderr)
		}
	}
	if len(stdout) != 0 {
		t.Fatalf("a refused start wrote %d bytes to STDOUT: %q", len(stdout), stdout)
	}
}
