// site_type_key_test.go — R-4/R-5 (James, 2026-09-09): the per-site
// `site_type` key is REQUIRED with NO DEFAULT, the D-88 `orientation`
// pattern. Absent refuses naming the line to write; a value outside the
// vocabulary refuses naming the value and both legal ones; the wrong TOML
// type is the decoder's refusal and names the line. Each refusal watched red
// on a mutant of config.go before it was trusted green —
// reports/witness/R4-R5-SITETYPE.md.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// siteTypeKeyTestBase is a complete, valid config EXCEPT for site_type —
// orientation is present, so a refusal below is about site_type alone.
const siteTypeKeyTestBase = `
site_id               = "site-x"
listen_addr           = "127.0.0.1:0"
node_epoch            = 1
db_path               = "test.db"
sync_interval_seconds = 10
login_password        = "x"
orientation           = false
`

func loadSiteTypeTOML(t *testing.T, body string) (*Config, string, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "site.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	return c, path, err
}

// TestSiteTypeKeyAbsentIsRefusedNamingTheFileAndTheLineToWrite — the
// MISSING case. The administrator reads: which file, which key, and the two
// exact lines one of which they must add. PROVEN RED BY: deleting the
// `c.SiteType == ""` check in validate() (the file then LOADS, SiteType "").
func TestSiteTypeKeyAbsentIsRefusedNamingTheFileAndTheLineToWrite(t *testing.T) {
	c, path, err := loadSiteTypeTOML(t, siteTypeKeyTestBase)
	if err == nil {
		t.Fatalf("a config with NO site_type key LOADED (SiteType=%q) — R-4/R-5: a default in either direction is a silent guess (D-77)", c.SiteType)
	}
	if c != nil {
		t.Fatalf("Load returned a non-nil *Config alongside its error: %+v", c)
	}
	msg := err.Error()
	for _, want := range []string{path, "site_type", "required", "no default", `site_type = "text_message"`, `site_type = "camp"`, "R-4", "R-5", "D-77"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the MISSING-key refusal lacks %q\n  got: %s", want, msg)
		}
	}
	for _, mustNot := range []string{"incompatible types", "expected value", "is not a site type"} {
		if strings.Contains(msg, mustNot) {
			t.Errorf("the MISSING-key refusal reads as a different refusal (%q) — the line does not exist to be wrong\n  got: %s", mustNot, msg)
		}
	}
}

// TestSiteTypeKeyPresentLoadsAndReadsAsWritten — positive controls: both
// legal values load and read back exactly as written.
func TestSiteTypeKeyPresentLoadsAndReadsAsWritten(t *testing.T) {
	for _, tc := range []struct {
		line string
		want domain.SiteType
	}{
		{`site_type = "text_message"` + "\n", domain.SiteTypeTextMessage},
		{`site_type = "camp"` + "\n", domain.SiteTypeCamp},
	} {
		c, _, err := loadSiteTypeTOML(t, siteTypeKeyTestBase+tc.line)
		if err != nil {
			t.Fatalf("%q: Load refused: %v — the fixture is broken, so the refusal tests prove nothing", strings.TrimSpace(tc.line), err)
		}
		if c.SiteType != tc.want {
			t.Fatalf("%q: SiteType = %q, want %q", strings.TrimSpace(tc.line), c.SiteType, tc.want)
		}
	}
}

// TestSiteTypeKeyUnknownValueIsRefusedNamingTheValueAndBothLegalOnes — a
// value outside the vocabulary is a perfectly valid TOML string, so the
// decoder accepts it; validate() must not, and the vocabulary is exact
// (case included — an administrator is told, not second-guessed). PROVEN
// RED BY: deleting the ParseSiteType call in validate() ("resort" then
// LOADS and the report would meet a third type).
func TestSiteTypeKeyUnknownValueIsRefusedNamingTheValueAndBothLegalOnes(t *testing.T) {
	for _, val := range []string{"resort", "Camp", "text-message", "TEXT_MESSAGE"} {
		body := siteTypeKeyTestBase + fmt.Sprintf("site_type = %q\n", val)
		c, path, err := loadSiteTypeTOML(t, body)
		if err == nil {
			t.Fatalf("site_type = %q LOADED (SiteType=%q) — the vocabulary is two values, exactly", val, c.SiteType)
		}
		msg := err.Error()
		for _, want := range []string{path, "site_type", fmt.Sprintf("%q", val), "is not a site type", `"text_message"`, `"camp"`, "R-4", "R-5"} {
			if !strings.Contains(msg, want) {
				t.Errorf("site_type = %q: the refusal lacks %q\n  got: %s", val, want, msg)
			}
		}
		if strings.Contains(msg, "the key is required") {
			t.Errorf("site_type = %q: the refusal reads as the MISSING one — the line IS there, and the administrator must be told it is wrong, not absent\n  got: %s", val, msg)
		}
	}
	// The empty string is the absent case by another route: the same
	// MISSING refusal, naming the line to write.
	_, _, err := loadSiteTypeTOML(t, siteTypeKeyTestBase+`site_type = ""`+"\n")
	if err == nil || !strings.Contains(err.Error(), "the key is required") {
		t.Fatalf(`site_type = "": want the MISSING refusal, got: %v`, err)
	}
}

// TestSiteTypeKeyWrongTOMLTypeIsTheDecodersRefusalNamingTheLine — the
// MALFORMED case an administrator actually types: a bool, an integer, a bare
// word. None loads; each refusal names the file and the decoder's line
// number, computed from the fixture rather than remembered.
func TestSiteTypeKeyWrongTOMLTypeIsTheDecodersRefusalNamingTheLine(t *testing.T) {
	for _, tc := range []struct{ name, line string }{
		{"bool", "site_type = true\n"},
		{"integer", "site_type = 1\n"},
		{"bare word", "site_type = camp\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := siteTypeKeyTestBase + tc.line
			c, path, err := loadSiteTypeTOML(t, body)
			if err == nil {
				t.Fatalf("%q LOADED (SiteType=%q) — a malformed value must refuse, never coerce", strings.TrimSpace(tc.line), c.SiteType)
			}
			lineNo := 1 + strings.Count(body[:strings.Index(body, "site_type")], "\n")
			msg := err.Error()
			for _, want := range []string{path, "site_type", fmt.Sprintf("line %d", lineNo)} {
				if !strings.Contains(msg, want) {
					t.Errorf("the MALFORMED refusal lacks %q\n  got: %s", want, msg)
				}
			}
			if strings.Contains(msg, "the key is required") {
				t.Errorf("the MALFORMED refusal reads as the MISSING one — the line IS there\n  got: %s", msg)
			}
		})
	}
}
