// witness_y13_q13_test.go — witness phase (see reports/witness/_BRIEF.md),
// worktree y13-copy. Kills census survivor Q13.
//
// Q13: cmd/dumpcompare/main.go:305 `if !inRegistry[name] {` inside
// buildCompareSet — the "SHRINK TOOTH", a HARD FAILURE (per the function's
// own §67 LOCK comment) when ignoredProjections names a projection that is
// no longer registered. Nothing in this package exercised it before this
// file: no existing test in main_test.go touches buildCompareSet or
// ignoredProjections at all.
package main

import (
	"strings"
	"testing"
)

// TestWitnessQ13BuildCompareSetRefusesADeadIgnoreEntry injects one bogus
// entry into the package-level ignoredProjections map — a stand-in for a
// deliberately-excluded projection that has since been renamed or removed —
// and asserts buildCompareSet refuses to proceed silently. Without the
// guard, a rotted exclusion list passes clean and the operator running
// dumpcompare never learns their ignore-list no longer means what they
// think it means (main.go's own comment: "a rotted exclusion list is how
// this defect would come back one layer down").
func TestWitnessQ13BuildCompareSetRefusesADeadIgnoreEntry(t *testing.T) {
	const deadKey = "__witness_q13_dead_projection__"
	if _, exists := ignoredProjections[deadKey]; exists {
		t.Fatal("precondition: the witness key is already present in ignoredProjections")
	}
	ignoredProjections[deadKey] = "witness Q13: injected to prove buildCompareSet notices a rotted exclusion"
	t.Cleanup(func() { delete(ignoredProjections, deadKey) })

	_, err := buildCompareSet()
	if err == nil {
		t.Fatal("buildCompareSet() accepted an ignoredProjections entry naming an unregistered " +
			"projection — a rotted exclusion list now passes silently")
	}
	if !strings.Contains(err.Error(), deadKey) {
		t.Errorf("buildCompareSet() error = %q, want it to NAME the dead entry %q", err.Error(), deadKey)
	}
	if !strings.Contains(err.Error(), "NOT registered") {
		t.Errorf("buildCompareSet() error = %q, want it to say the entry is NOT registered", err.Error())
	}
}
