// b6_cutover_test.go — gate B-6, the non-schema half (reports/witness/B6-CUTOVER-GAPS.md).
//
// D-85 EXECUTION (reports/JAMES-DECISIONS.md, 2026-09-05) MEASURED this tool
// printing "IDENTICAL: 12" over TWO EMPTY DATABASES: `sitectl -dump` ran with
// CWD "/" against a relative db_path, opened nothing, and two 428-byte dumps
// of nothing compared equal. dc.ps1 was repaired at the CWD; the TOOL was not,
// and the sheet (design/WINDOW-CHECKLIST-v1.md §3(b)) still tells the operator
// "two empty databases compare IDENTICAL and the tool exits 0 — the check is
// yours", at 4am, by hand. W10-2 requires "dumpcompare IDENTICAL over a
// NON-ZERO row count". These tests make the tool refuse the vacuous case
// itself and print the reconciliation W10-2(c) otherwise leaves to arithmetic.
//
// ⟦G17, 2026-09-09⟧ ⟦was: "The verdict line `IDENTICAL: %d compared
// projections match` is NOT changed: internal/store/w10_dumpcompare_verdict_test.go
// pins its count as len(compareSet) (W10-2(c)'s control). The reconciliation
// is ADDED beside it."⟧
// ⭐ THE VERDICT LINE IS NOW WITHHELD, NOT UNCHANGED. A compare-set
// projection absent from BOTH dumps makes the tool print INCOMPLETE and
// exit 2 instead (cmd/dumpcompare/main.go) — there is no verdict count at
// all on that run. On the unchanged happy path it still reads `IDENTICAL:
// %d compared projections match` with %d == len(compareSet), pinned by
// THIS file's own TestAnIdenticalRunPrintsTheRowReconciliationBesideTheVerdict
// below. internal/store/w10_dumpcompare_verdict_test.go no longer pins that
// count on its own: TestW10TheVerdictWordIsWithheldWhenTheCompareSetWasNotFullyCompared
// is W10-2(c)'s CONTROL now, re-derived after the hardening, and what it
// pins is the withholding itself. The reconciliation is still ADDED beside
// the verdict line.
package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// emptyStoreDump is the D-85 shape: every registered projection present, every
// one of them an empty list — exactly what `sitectl -dump` emits from a fresh
// database it should not have been reading.
func emptyStoreDump(t *testing.T) string {
	t.Helper()
	names, err := registeredProjections()
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	d := map[string][]map[string]any{}
	for _, n := range names {
		d[n] = []map[string]any{}
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// oneRowEachDump plants one row in every registered projection, so that the
// compared set is non-empty on that side.
func oneRowEachDump(t *testing.T) string {
	t.Helper()
	names, err := registeredProjections()
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	d := map[string][]map[string]any{}
	for _, n := range names {
		d[n] = []map[string]any{{"id": n + "-1"}}
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestTwoDumpsOfEmptyStoresAreVacuousNotIdentical is the D-85 false green,
// reproduced at the tool's own boundary: two syntactically valid dumps in
// which every compared projection is empty on BOTH sides must not be reported
// as agreement. Exit 2 is "the assert never ran" — which is the truth: nothing
// was compared. The word IDENTICAL must not appear, because it is the word the
// sheet tells the operator to write down.
func TestTwoDumpsOfEmptyStoresAreVacuousNotIdentical(t *testing.T) {
	empty := emptyStoreDump(t)
	code, out, errOut := runTool(t, saved(t, empty), saved(t, empty))
	if code != 2 {
		t.Fatalf("two empty-store dumps: want exit 2 (the assert never ran), got %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.Contains(out, "IDENTICAL") {
		t.Fatalf("two empty-store dumps must never print IDENTICAL — that is the D-85 false green:\n%s", out)
	}
	if !strings.Contains(out, "VACUOUS") {
		t.Fatalf("want a VACUOUS verdict word on stdout (the sheet's result-word line), got:\n%s", out)
	}
	if !strings.Contains(errOut, "D-85") || !strings.Contains(errOut, "/opt/sitectl") {
		t.Fatalf("stderr must name D-85 and the CWD the dump must be taken from, got:\n%s", errOut)
	}
}

// TestRowsOnOneSideOnlyIsADiffNotVacuous guards the guard: the vacuous case is
// "zero rows on BOTH sides". One side empty and the other populated is a real
// disagreement and must stay exit 1 with DIFF lines — the new refusal must not
// swallow a genuine failure into "never ran".
func TestRowsOnOneSideOnlyIsADiffNotVacuous(t *testing.T) {
	code, out, _ := runTool(t, saved(t, oneRowEachDump(t)), saved(t, emptyStoreDump(t)))
	if code != 1 {
		t.Fatalf("populated vs empty: want exit 1 (assert FAILED), got %d\n%s", code, out)
	}
	if !strings.Contains(out, "DIFF ") || strings.Contains(out, "VACUOUS") {
		t.Fatalf("populated vs empty must report DIFF lines and never VACUOUS:\n%s", out)
	}
}

// TestAnIdenticalRunPrintsTheRowReconciliationBesideTheVerdict: W10-2(c)
// requires the operator to reconcile `len(compareSet) − absent-from-both =
// actually compared` by hand. The tool now prints that arithmetic, plus the
// row totals each side, so a non-zero row count is READ off the output rather
// than inferred from file sizes.
//
// ⟦G17, 2026-09-09⟧ ⟦was: "The verdict line itself is unchanged (pinned
// elsewhere)."⟧ — the verdict line is no longer unchanged: a compare-set
// projection absent from BOTH dumps now withholds the word IDENTICAL and
// exits 2. This test's fixture is therefore COMPLETED first, because a
// partial dump is not a small dump — it is a dump from an older binary.
// The reconciliation line is still what is asserted, and on a completed
// pair the absent-from-both term is 0.
func TestAnIdenticalRunPrintsTheRowReconciliationBesideTheVerdict(t *testing.T) {
	set, err := buildCompareSet()
	if err != nil {
		t.Fatal(err)
	}
	inSet := map[string]bool{}
	for _, k := range set {
		inSet[k] = true
	}
	var one map[string][]map[string]any
	if err := json.Unmarshal([]byte(completed(t, dumpOne)), &one); err != nil {
		t.Fatal(err)
	}
	present, rows := 0, 0
	for k, v := range one {
		if inSet[k] {
			present++
			rows += len(v)
		}
	}
	absent := len(set) - present

	code, out, _ := runTool(t, saved(t, completed(t, dumpOne)), saved(t, completed(t, dumpOneReordered)))
	if code != 0 {
		t.Fatalf("identical dumps: exit %d\n%s", code, out)
	}
	want := fmt.Sprintf("compared: %d projections present in both dumps (rows a=%d b=%d); %d compare-set projections absent from both, not compared",
		present, rows, rows, absent)
	if !strings.Contains(out, want) {
		t.Fatalf("want the reconciliation line\n  %s\nin stdout:\n%s", want, out)
	}
	if !strings.Contains(out, fmt.Sprintf("IDENTICAL: %d compared projections match", len(set))) {
		t.Fatalf("the pinned verdict line must be unchanged (W10-2(c) control), got:\n%s", out)
	}
	if rows == 0 {
		t.Fatalf("fixture dumpOne carries no rows in the compare set; this test would be vacuous")
	}
}
