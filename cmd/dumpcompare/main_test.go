// Tests named for what they prove (BUILDER_BRIEF_RULES 4/5). The load-bearing
// one is TestDifferingComparedProjectionExitsNonzeroAndNamesTheRow: it is the
// proof that dumpcompare CAN fail — a tool that can only print IDENTICAL
// would make every §13.3 step-6 run green by construction.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const dumpOne = `{
	"company":[{"company_id":"site-a-company-1","name":"Acme"}],
	"trade":[],"person":[{"person_id":"site-a-person-1","name":"Driver"}],
	"trip":[{"trip_id":"site-a-trip-1","vehicle":"T-1"}],
	"trip_occupant":[],"trip_arrival":[],"trip_cancel":[],
	"trip_state":[{"trip_id":"site-a-trip-1","state":"open","arrival_id":null}],
	"arrival_state":[],"visit":[],"visit_checkout":[]
}`

// dumpOneReordered is dumpOne with object keys in a different order — the
// same data as served by a second site whose JSON marshalling happens to
// order differently. Must compare IDENTICAL (canonical re-marshal).
const dumpOneReordered = `{
	"visit_checkout":[],"visit":[],"arrival_state":[],
	"trip_state":[{"arrival_id":null,"state":"open","trip_id":"site-a-trip-1"}],
	"trip_cancel":[],"trip_arrival":[],"trip_occupant":[],
	"trip":[{"vehicle":"T-1","trip_id":"site-a-trip-1"}],
	"person":[{"name":"Driver","person_id":"site-a-person-1"}],
	"trade":[],"company":[{"name":"Acme","company_id":"site-a-company-1"}]
}`

// dumpTwo differs from dumpOne in exactly one compared row: the trip's
// vehicle text.
const dumpTwo = `{
	"company":[{"company_id":"site-a-company-1","name":"Acme"}],
	"trade":[],"person":[{"person_id":"site-a-person-1","name":"Driver"}],
	"trip":[{"trip_id":"site-a-trip-1","vehicle":"T-2-DIVERGED"}],
	"trip_occupant":[],"trip_arrival":[],"trip_cancel":[],
	"trip_state":[{"trip_id":"site-a-trip-1","state":"open","arrival_id":null}],
	"arrival_state":[],"visit":[],"visit_checkout":[]
}`

// saved writes a dump body to a file and returns its path — the ONLY input
// shape this tool accepts since D-56 removed GET /debug/projection-dump.
// These tests used to stand up an httptest server per side; that helper was
// deleted with the network path rather than left behind to keep proving a
// capability the tool no longer has.
// completed pads a hand-written dump fixture up to the FULL compare set,
// adding an empty row list for every compare-set projection the fixture does
// not mention.
//
// ⟦G17, 2026-09-09⟧ ⛔ WHY THIS EXISTS, because it looks like a workaround and
// is not. dumpcompare now REFUSES a run in which a compare-set projection is
// absent from both dumps (INCOMPLETE, exit 2) — the run never compared what it
// claims to have compared. The fixtures below name ~11 of the 24 compare-set
// projections, so before this helper every exit-0 assertion in this file was
// being made against a dump shape that CANNOT OCCUR:
//
//	[MEASURED 2026-09-09 — internal/store/projection.go:82] DumpProjections
//	loops over every registered projection and assigns out[p.Name] = rows
//	UNCONDITIONALLY, so a real `sitectl -dump` always emits a key per
//	registered projection, empty tables included. A partial dump is not a
//	small dump; it is a dump from an OLDER BINARY, which is precisely the
//	condition G17 refuses.
//
// ⭐ Padding is DERIVED from buildCompareSet(), never hardcoded, so a newly
// registered projection widens these fixtures automatically instead of
// silently re-breaking every test here.
//
// ⚠ Apply it only where a COMPLETE pair is the realistic input. A test that
// deliberately drops a key from ONE side must NOT use it — padding would
// restore the very key it removes. That is the case in
// TestComparedProjectionMissingFromOneSideFails, which is left alone.
func completed(t *testing.T, body string) string {
	t.Helper()
	var d map[string][]map[string]any
	if err := json.Unmarshal([]byte(body), &d); err != nil {
		t.Fatalf("completed: fixture is not a dump: %v", err)
	}
	set, err := buildCompareSet()
	if err != nil {
		t.Fatalf("completed: buildCompareSet: %v", err)
	}
	for _, k := range set {
		if _, ok := d[k]; !ok {
			d[k] = []map[string]any{}
		}
	}
	out, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("completed: marshal: %v", err)
	}
	return string(out)
}

func saved(t *testing.T, body string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "dump.json")
	if err := os.WriteFile(f, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}

func runTool(t *testing.T, a, b string) (code int, out, errOut string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code = run([]string{a, b}, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestIdenticalDumpsCompareEqual(t *testing.T) {
	code, out, _ := runTool(t, saved(t, completed(t, dumpOne)), saved(t, completed(t, dumpOneReordered)))
	if code != 0 {
		t.Fatalf("identical (reordered) dumps: exit %d, out:\n%s", code, out)
	}
	if !strings.Contains(out, "IDENTICAL") {
		t.Fatalf("want IDENTICAL verdict, got:\n%s", out)
	}
}

func TestDifferingComparedProjectionExitsNonzeroAndNamesTheRow(t *testing.T) {
	code, out, _ := runTool(t, saved(t, dumpOne), saved(t, dumpTwo))
	if code != 1 {
		t.Fatalf("differing trip row: want exit 1, got %d, out:\n%s", code, out)
	}
	if !strings.Contains(out, "DIFF trip[0]") {
		t.Fatalf("want the differing projection and row index named, got:\n%s", out)
	}
	if !strings.Contains(out, "T-2-DIVERGED") {
		t.Fatalf("want the differing row content shown, got:\n%s", out)
	}
}

// The §13.3 exclusion, both directions: a key OUTSIDE the compare set that
// diverges must not fail the run (deadline/sync/session state is site-local
// by design) — but its name must appear, so nothing is silently dropped.
func TestNonWhitelistedDivergenceIsIgnoredButNamed(t *testing.T) {
	a := strings.TrimSuffix(strings.TrimSpace(dumpOne), "}") + `,"alert_ack":[{"trip_id":"x"}]}`
	b := strings.TrimSuffix(strings.TrimSpace(dumpOne), "}") + `,"alert_ack":[]}`
	code, out, _ := runTool(t, saved(t, completed(t, a)), saved(t, completed(t, b)))
	if code != 0 {
		t.Fatalf("divergence outside the compare set must not fail: exit %d, out:\n%s", code, out)
	}
	if !strings.Contains(out, "ignored (not in the §13.3 compare set): alert_ack") {
		t.Fatalf("ignored key must be named, got:\n%s", out)
	}
}

func TestComparedProjectionMissingFromOneSideFails(t *testing.T) {
	var trimmed []string
	for _, line := range strings.Split(dumpOne, "\n") {
		if strings.Contains(line, `"visit":[],"visit_checkout":[]`) {
			line = `"visit":[]` // drop visit_checkout entirely
		}
		trimmed = append(trimmed, line)
	}
	code, out, _ := runTool(t, saved(t, dumpOne), saved(t, strings.Join(trimmed, "\n")))
	if code != 1 {
		t.Fatalf("compare-set key missing on one side: want exit 1, got %d, out:\n%s", code, out)
	}
	if !strings.Contains(out, "DIFF visit_checkout") {
		t.Fatalf("want the missing projection named, got:\n%s", out)
	}
}

func TestRowCountMismatchFails(t *testing.T) {
	// ⟦W8-13(d)⟧ ⟦was: the row-count probe was planted on the deleted
	// reference-merge projection⟧ — it moves to `trade`, which is the entry
	// that REPLACED it in the §13.3 compare set. The probe is about the
	// tool's row-count arm, not about which projection carries it.
	b := strings.Replace(dumpOne, `"trade":[]`,
		`"trade":[{"trade_id":"site-a-trade-1","name":"Scaffolding"}]`, 1)
	code, out, _ := runTool(t, saved(t, dumpOne), saved(t, b))
	if code != 1 {
		t.Fatalf("row-count mismatch: want exit 1, got %d, out:\n%s", code, out)
	}
	if !strings.Contains(out, "DIFF trade: 0 rows in a, 1 rows in b") {
		t.Fatalf("want row counts reported, got:\n%s", out)
	}
}

func TestFileArgumentsWork(t *testing.T) {
	dir := t.TempDir()
	pa, pb := filepath.Join(dir, "a.json"), filepath.Join(dir, "b.json")
	if err := os.WriteFile(pa, []byte(completed(t, dumpOne)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pb, []byte(completed(t, dumpTwo)), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := runTool(t, pa, pa); code != 0 {
		t.Fatalf("same file twice: want 0, got %d\n%s", code, out)
	}
	if code, _, _ := runTool(t, pa, pb); code != 1 {
		t.Fatalf("differing files: want 1, got %d", code)
	}
}

// The C1 catch-all trap (brief item 3): when this tool fetched over HTTP,
// every unmatched URL answered 200 with an HTML page, and two such pages
// comparing equal would have been a green run that proved nothing. The trap
// survives the move to files — an operator who saves the wrong output, or a
// redirected shell that captures a web page, hands this tool two identical
// non-dumps — so the loud exit-2 refusal is still asserted.
func TestNonDumpHTMLBodyIsAnErrorNotAnEmptyMatch(t *testing.T) {
	html := saved(t, "<!doctype html><html><body>login</body></html>")
	code, out, errOut := runTool(t, html, html)
	if code != 2 {
		t.Fatalf("HTML body: want exit 2 (error), got %d, out:\n%s", code, out)
	}
	if !strings.Contains(errOut, "not a projection dump") {
		t.Fatalf("want a loud parse refusal, got stderr:\n%s", errOut)
	}
}

func TestUsageErrorIsExit2(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"only-one"}, &stdout, &stderr); code != 2 {
		t.Fatalf("want exit 2 on bad usage, got %d", code)
	}
}

// TestADumpURLIsRefusedAndNamesTheReplacement pins D-56's removal at this
// tool's own boundary. GET /debug/projection-dump is gone, so an operator
// running the command in their old notes must be told what to run instead —
// not handed "no such file or directory", and above all not handed a fetch.
//
// Asserted on the MESSAGE, not on the exit code alone: exit 2 is also what a
// missing file gives, so a code-only assertion would pass whether the URL
// branch existed or not.
func TestADumpURLIsRefusedAndNamesTheReplacement(t *testing.T) {
	for _, u := range []string{
		"http://10.90.1.10:8081/debug/projection-dump",
		"https://10.90.2.10:8082/debug/projection-dump",
	} {
		var stdout, stderr bytes.Buffer
		code := run([]string{u, u}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("%s: want exit 2 (the assert never ran), got %d", u, code)
		}
		if !strings.Contains(stderr.String(), "no longer exist") ||
			!strings.Contains(stderr.String(), "sitectl -config") {
			t.Fatalf("%s: the refusal must name the replacement command, got stderr:\n%s", u, stderr.String())
		}
		if strings.Contains(stdout.String(), "IDENTICAL") {
			t.Fatalf("%s: two refused URLs must never compare IDENTICAL", u)
		}
	}
}
