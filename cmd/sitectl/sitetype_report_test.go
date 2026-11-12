// sitetype_report_test.go — R-4/R-5 at the binary. The per-person report is
// a second operator door on the same posture as -report visits (CSV on
// stdout, denominator on stderr, never binds the listen address), and a
// config without `site_type` refuses to start naming the line to write.
// PROVEN RED BY: (report) removing the "signout-failures" arm from main.go's
// name check — the name is then refused, exit 1; (refusal) deleting the
// `c.SiteType == ""` check in config.validate() — the post then runs -dump
// with no type at all. See reports/witness/R4-R5-SITETYPE.md.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

func TestReportSignoutFailuresEmitsPerPersonCSVAndNeverBindsTheListenAddress(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "site-a.db")
	seedVisitStore(t, dbPath)
	held := heldAddr(t)
	cfg := writeConfig(t, dir, held, dbPath, nil)

	code, stdout, stderr := runSitectl(t, "-config", cfg, "-report", "signout-failures")
	if code != 0 {
		t.Fatalf("sitectl -report signout-failures exited %d on a listen address held by this test (%s) — it TRIED TO SERVE.\nstderr: %s", code, held, stderr)
	}
	lines := strings.Split(strings.TrimSpace(string(stdout)), "\n")
	if len(lines) != 2 {
		t.Fatalf("want header + 1 row on stdout, got %d lines:\n%s", len(lines), stdout)
	}
	if got, want := strings.TrimRight(lines[0], "\r"), strings.Join(domain.PersonSignoutFailureColumns, ","); got != want {
		t.Fatalf("header:\n got %q\nwant %q", got, want)
	}
	// The seeded person: one visit, signed out properly, at a site this
	// post types (its own) — zero failures, and zero could-not-judge.
	if !strings.Contains(lines[1], ",A Driver,1,0,,0,0") {
		t.Fatalf("row does not show one clean visit at a typed site: %q", lines[1])
	}
	for _, want := range []string{
		"site types known at this post: site-a=text_message",
		"missed sign-outs at text-message sites (R-4) 0",
	} {
		if !strings.Contains(string(stderr), want) {
			t.Fatalf("stderr lacks %q:\n%s", want, stderr)
		}
	}
	if strings.Contains(string(stdout), "site types") {
		t.Fatalf("the denominator leaked onto stdout, which must be pure CSV:\n%s", stdout)
	}
}

func TestConfigWithoutTheSiteTypeKeyRefusesToStartNamingTheLineToWrite(t *testing.T) {
	dir := t.TempDir()
	cfg := writeConfig(t, dir, heldAddr(t), filepath.Join(dir, "site-a.db"), nil)
	raw, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var kept []string
	for _, l := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(l, "site_type") {
			kept = append(kept, l)
		}
	}
	if len(kept) != strings.Count(string(raw), "\n") {
		t.Fatalf("fixture: expected to strip exactly one site_type line from writeConfig's output")
	}
	if err := os.WriteFile(cfg, []byte(strings.Join(kept, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runSitectl(t, "-config", cfg, "-dump")
	if code == 0 {
		t.Fatalf("sitectl EXITED 0 on a config with no `site_type` key — R-4/R-5: the post would report with no type, or a guessed one.\nstderr: %q", stderr)
	}
	for _, want := range []string{cfg, "site_type", "required", `site_type = "text_message"`, `site_type = "camp"`} {
		if !strings.Contains(string(stderr), want) {
			t.Errorf("the refusal on stderr lacks %q\n  got: %s", want, stderr)
		}
	}
	if len(stdout) != 0 {
		t.Fatalf("a refused start wrote %d bytes to STDOUT: %q", len(stdout), stdout)
	}
}
