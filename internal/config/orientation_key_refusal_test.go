// orientation_key_refusal_test.go — tester-go's witnesses for the D-88
// config key (reports/witness/TESTER-step4.md): the MISSING case and the
// MALFORMED case are two different refusals, tested separately, and each
// names the FILE. The malformed one names the LINE NUMBER (the decoder's);
// the missing one has no line to point at, so it names the line TO WRITE.
// Each watched red on a mutant of config.go before it was trusted green.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeOrientationKeyTOML(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "site.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestOrientationKeyAbsentRefusalNamesTheFileAndTheLineToWrite — the MISSING
// case. The administrator reads: which file, which key, and the two exact
// lines one of which they must add. It is NOT the decoder's refusal: no
// "line N", no "incompatible types" — that would send them hunting a typo
// on a line that does not exist.
func TestOrientationKeyAbsentRefusalNamesTheFileAndTheLineToWrite(t *testing.T) {
	path := writeOrientationKeyTOML(t, orientationKeyTestBase)
	c, err := Load(path)
	if err == nil {
		t.Fatalf("a config with NO orientation key LOADED (HasOrientation()=%v)", c.HasOrientation())
	}
	if c != nil {
		t.Fatalf("Load returned a non-nil *Config alongside its error: %+v", c)
	}
	msg := err.Error()
	for _, want := range []string{path, "orientation", "required", "orientation = true", "orientation = false"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the MISSING-key refusal lacks %q\n  got: %s", want, msg)
		}
	}
	for _, mustNot := range []string{"incompatible types", "expected value"} {
		if strings.Contains(msg, mustNot) {
			t.Errorf("the MISSING-key refusal reads like a decoder error (%q) — the line does not exist to be malformed\n  got: %s", mustNot, msg)
		}
	}
}

// TestOrientationKeyMalformedRefusalNamesTheFileAndTheLine — the MALFORMED
// case, three shapes an administrator actually types. None may load; each
// refusal names the file and the decoder's line number, which is computed
// from the fixture rather than remembered.
func TestOrientationKeyMalformedRefusalNamesTheFileAndTheLine(t *testing.T) {
	for _, tc := range []struct{ name, line string }{
		{"quoted string", `orientation = "true"` + "\n"},
		{"integer", "orientation = 1\n"},
		{"bare word", "orientation = yes\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := orientationKeyTestBase + tc.line
			path := writeOrientationKeyTOML(t, body)
			c, err := Load(path)
			if err == nil {
				t.Fatalf("%q LOADED (HasOrientation()=%v) — a malformed value must refuse, never coerce", strings.TrimSpace(tc.line), c.HasOrientation())
			}
			if c != nil {
				t.Fatalf("Load returned a non-nil *Config alongside its error: %+v", c)
			}
			lineNo := 1 + strings.Count(body[:strings.Index(body, "orientation")], "\n")
			msg := err.Error()
			for _, want := range []string{path, "orientation", fmt.Sprintf("line %d", lineNo)} {
				if !strings.Contains(msg, want) {
					t.Errorf("the MALFORMED refusal lacks %q\n  got: %s", want, msg)
				}
			}
			if strings.Contains(msg, "the key is required") {
				t.Errorf("the MALFORMED refusal reads as the MISSING one — the line IS there, and the administrator must be told it is wrong, not absent\n  got: %s", msg)
			}
		})
	}
}
