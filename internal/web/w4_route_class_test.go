// w4_route_class_test.go — W4-13(c) / Changes delta 7: TP §4.8's
// successor-table correction-route row lands IN THE SAME COMMIT that
// registers the route, so W2's route-walk stays green by CLASSIFICATION,
// never by an exemption minted in the walk. The row's shape is LOCKED:
// EXEMPT-with-written-reason — the typed instant is PAYLOAD (row 10's
// precedent); the envelope describes the ACT per D-44 — and the reason
// CITES D-44, never the stale D-42-obligation-3 citation VF §7.5-4's own
// text still carries (the addendum's Seam 3: apply it citing D-44, "or
// the superseded ruling gets copied into the plan at the same moment the
// addendum corrects it elsewhere").
//
// The row itself is criteria-lock material in w2_route_walk_test.go's
// table; W4's LOCKED delta 7 is the criteria-lock authority for exactly
// this one row (the W3 precedent: the /person/amend row, applied by W3's
// builder under VF §7.5 item 4's instruction). This test asserts the row
// arrived with the right shape; W2's walk separately reds on the route if
// it registers unclassified, and reds on the row if the route never
// registers (reverse reconciliation) — the two directions between them
// enforce "same commit".
// design/WINDOW-BUILD-PLAN-v1.md "### W4", criteria LOCKED 2026-08-27.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD (the package's other W4 files reference
// unbuilt domain symbols) — on the clean tree this test's would-be red
// ("no row for /time/correct") is subsumed by the package's vet red.
package web

import (
	"strings"
	"testing"
)

// TestW4CorrectionRouteClassifiedExemptCitingD44 — W4-13(c).
func TestW4CorrectionRouteClassifiedExemptCitingD44(t *testing.T) {
	var row *w2Row
	for i := range w2RouteTable {
		if w2RouteTable[i].Route == w4CorrectPath {
			row = &w2RouteTable[i]
			break
		}
	}
	if row == nil {
		t.Fatalf("W4-13c: w2RouteTable carries NO row for %s — TP §4.8's correction-route row must land in the same commit that registers the route (Changes delta 7); if the walk went red on the unclassified route first, that is the walk working, and the fix is the row, never an exemption minted in the walk", w4CorrectPath)
	}
	if row.Class != w2Exempt {
		t.Fatalf("W4-13c: the correction route is classified %d, want exempt-with-written-reason — takes-input would demand a wiring test no correct implementation can pass (VF §5's route classification; the typed instant is payload)", row.Class)
	}
	reason := row.Reason
	if strings.TrimSpace(reason) == "" {
		t.Fatalf("W4-13c: the correction route's exempt row carries no written reason — never a growing allowlist")
	}
	if !strings.Contains(strings.ToLower(reason), "payload") {
		t.Fatalf("W4-13c: the written reason %q does not state the ground — the typed instant is PAYLOAD (row 10's precedent)", reason)
	}
	if !strings.Contains(reason, "D-44") {
		t.Fatalf("W4-13c (the addendum's Seam 3): the written reason %q does not cite D-44 — the envelope-describes-the-act ground is D-44's ruling; citing anything staler copies a superseded ruling into the walk", reason)
	}
	if strings.Contains(strings.ToLower(reason), "obligation 3") {
		t.Fatalf("W4-13c (Seam 3): the written reason %q cites D-42 obligation 3, which D-44 SUPERSEDED on the storage question — VF §7.5-4's own stale citation must not be copied here", reason)
	}
}
