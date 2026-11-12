// w4_consumer_scan_test.go — W4-13(b): the VF §7.6 consumer scan,
// COMPLETED for the correction carrier ("whichever registers last
// completes it" — W3 landed the amendment half as W3-14; this is the
// correction half): no package outside internal/domain reads the
// occurred_at_correction side table or its event-type constants (SQL
// identifiers and Go constants both). Sanctioned readers are the resolver
// (ResolveOccurredAt/Outcome) and the populating fold — both
// internal/domain, which this scan excludes wholesale; everything else
// resolves through the resolver or goes red.
// design/WINDOW-BUILD-PLAN-v1.md "### W4", criteria LOCKED 2026-08-27.
//
// package store, DELIBERATELY (beside W1's and W3's scan machinery, which
// it reuses): a file-system source scan referencing no unbuilt Go symbol,
// so it COMPILES AND RUNS against the clean tree, and it proves it detects
// a PLANTED violation before any clean pass counts (§5's CONTROL row —
// the lock's CONTROL row 1).
//
// KILLER MUTANT/CONTROL: the planted side-table read (the control below,
// plus the ledger's out-of-band planted-file run recorded in
// reports/window-build/W4-tests.md).
package store

import (
	"os"
	"testing"
)

const (
	w4ScanSideTableIdent = "occurred_at_correction" // the side table's SQL identifier
	w4ScanEventTypeLit   = "occurred_at_corrected"  // the event-type constant's string value
)

// TestW4ConsumerScanCorrectionSideTable — W4-13(b). The forbidden set:
// the side-table identifier and the event-type literal in ANY string (SQL
// or Go), plus the payload-type Go identifier (building correction
// payloads outside domain bypasses the edge — W3-14's own reasoning,
// applied to this carrier). The input-struct spelling the web handler
// passes to the EDGE (domain.TimeCorrection, SEAM W4-S-EDGE) is
// deliberately NOT forbidden: calling the edge is the sanctioned path.
// The allowlist is EMPTY and load-bearing.
func TestW4ConsumerScanCorrectionSideTable(t *testing.T) {
	forbiddenIdents := map[string]bool{
		w4ScanSideTableIdent:  true, // a Go identifier named after the table
		"OccurredAtCorrected": true, // the payload type — payload construction outside domain bypasses the edge
	}
	forbiddenInStrings := []string{w4ScanSideTableIdent, w4ScanEventTypeLit}
	allowlist := map[string]bool{} // EMPTY at W4 — extensions are gate rulings, not edits made in passing

	// --- CONTROL FIRST: the planted consumer must be detected. ----------
	planted := `package sneaky

const q = "SELECT corrected_occurred_at FROM occurred_at_correction WHERE target_seq = ?"

func decide(typ string) bool { return typ == "occurred_at_corrected" }
`
	hits := w1ScanGoTokens(t, "planted/consumer.go", []byte(planted), forbiddenIdents, forbiddenInStrings)
	if len(hits) < 2 {
		t.Fatalf("W4-13b CONTROL FAILED: the scan found %d hit(s) in the planted consumer, want >= 2 (the SQL identifier and the event-type constant) — a scan that cannot detect its plant proves nothing. Hits: %v", len(hits), hits)
	}
	clean := "package fine\n\nvar ok = true\n"
	if extra := w1ScanGoTokens(t, "planted/clean.go", []byte(clean), forbiddenIdents, forbiddenInStrings); len(extra) != 0 {
		t.Fatalf("W4-13b CONTROL: the scan flagged a clean file: %v", extra)
	}

	// --- the real pass over every non-domain package --------------------
	var found []w1Hit
	for _, path := range w3NonDomainGoFiles(t) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range w1ScanGoTokens(t, path, src, forbiddenIdents, forbiddenInStrings) {
			if !allowlist[h.File] {
				found = append(found, h)
			}
		}
	}
	if len(found) != 0 {
		t.Fatalf("W4-13b: package(s) outside internal/domain read the occurred_at_correction side table or its event-type constants — every consumer resolves through ResolveOccurredAt/Outcome, never the table (the twice-produced zero-resolution CRITICAL's bypass, refused mechanically):\n%v", found)
	}
}
