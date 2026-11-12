// w9c_unack_surface_test.go — W9c-9 (exactly N over the EFFECTIVE set,
// separated from the raw set). ⟦was: also W9c-6, the wide phase sweep —
// deleted with the weather-phase ack scope, James 2026-10-04; the m4c/m4b
// mutants below have nothing left to bite on⟧
//
// design/WINDOW-BUILD-PLAN-v1.md "#### W9c", both criteria ⟦REWRITTEN at the
// rev-4 fold⟧, against design/alert-unack-v1.md revision 4 §7 rows T4b and
// T7.
//
// KILLER MUTANTS: m4c (the PARTIAL phase sweep — the handler sweeps the
// first effective phase ack it finds) and m4b (dormant-ack-left-standing —
// rule 1 implemented over "currently suppressing" acks) on W9c-6; m7c (the
// RAW-SET target enumeration) and m7 (retract-first-only) and m7b
// (refusal-that-authors) on W9c-9.
//
// ⚠ Each rev-3 fixture these replace could NOT tell its rivals apart: T4b's
// old target set had size ONE, so "count = target-set size exactly" asserted
// 1 == 1; T7's old fixture had |effective| = |raw| = 2, so a raw enumeration
// and an effective one authored the same two events. Both fixtures below are
// built so the certified rule and every named rival return DIFFERENT,
// literally pinned numbers.
package web

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// w9cKeysEqual compares two key slices element-wise, duplicates included —
// a set comparison that collapsed duplicates would hide m7c's second
// retraction of an already-retracted ack.
func w9cKeysEqual(a, b []w9cKey) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestW9c9OnePostAuthorsExactlyOneRetractionPerEFFECTIVEAck — W9c-9 (T7).
//
// A trip carries THREE plain acks of which ONE is already retracted before
// the POST: raw set 3, effective set 2 — the two quantities the rev-3
// fixture could not tell apart. The pre-POST retraction R0 is authored AT
// THE FOLD SEAM as fixture setup, NOT through a prior POST: a prior POST
// would sweep the whole effective set and could never leave the mixed
// raw/effective state this fixture exists to create.
func TestW9c9OnePostAuthorsExactlyOneRetractionPerEFFECTIVEAck(t *testing.T) {
	srv, st, d, trip := newAlertingServer(t)

	// A second inbound expectation, never acknowledged — the empty-target
	// half. Dispatched FIRST so the stated ack seqs below are reachable.
	quiet := dispatchInboundTrip(t, st, d, "TRUCK QUIET", 60, 30*time.Minute)

	// ⟦LOCK — §90, 2026-09-03⟧ THE KEYS ARE DERIVED FROM THE STREAM, NOT STATED.
	//
	// ⟦was: "\tack1 := w9cKey{w9cPeerSite, 1, 4}  // (site-a, epoch 1, seq 4)"⟧
	// ⟦was: "\tack3 := w9cKey{w9cPeerSite, 1, 5}  // (site-a, epoch 1, seq 5)"⟧
	// ⟦was: "\tack2 := w9cKey{w9cBoardSite, 1, 6} // (site-b, epoch 1, seq 6)"⟧
	//
	// ⚠ WHY: seqs 4/5/6 were free when this was written and are not now — a
	// later window's events occupy them, and `w9cPadTo` fails loudly rather
	// than quietly overwriting ("site-a epoch 1 is already at seq 4"). ⭐ The
	// fixture was pinned to an event-stream POSITION that moved underneath it;
	// that is the failure, and it is not a defect in the criterion.
	//
	// ⚠ THE DESIGN DOC IS NOT AMENDED ⟦§90(4)⟧. `design/alert-unack-v1.md`
	// revision 4 §7 row T7 states these keys as `(site-a,1,4) (site-a,1,5)
	// (site-b,1,6)`, and THEY ARE ILLUSTRATIVE OF A SHAPE, not constitutive of
	// the criterion. Amending the doc so a fixture builds would be a criteria
	// change made to accommodate a repair, which is backwards. The doc keeps
	// its numbers; this test derives equivalent positions.
	//
	// ⭐ WHAT IS ACTUALLY THE CRITERION — "one post authors exactly one
	// retraction per EFFECTIVE ack" — is carried by FOUR RELATIONSHIPS, all
	// preserved and all now ASSERTED below rather than left to arithmetic:
	//
	//	(i)   ack1 and ack3 on the SAME site,
	//	(ii)  in that ORDER,
	//	(iii) ack2 on a DIFFERENT site,
	//	(iv)  the pre-POST retraction R0 naming ack3.
	//
	// Together those give |raw| = 3 and |effective| = 2 — the two quantities
	// the rev-3 fixture could not tell apart, and the whole reason this
	// fixture was rewritten at the rev-4 fold.
	ack1 := w9cAckAt(t, st, w9cKey{w9cPeerSite, 1, w9cNextSeq(t, st, w9cPeerSite, 1)},
		trip.TripID, "Pad Site Control", 1_700_000_100_000)
	ack3 := w9cAckAt(t, st, w9cKey{w9cPeerSite, 1, w9cNextSeq(t, st, w9cPeerSite, 1)},
		trip.TripID, "Pad Site Control", 1_700_000_200_000)
	ack2 := w9cAckAt(t, st, w9cKey{w9cBoardSite, 1, w9cNextSeq(t, st, w9cBoardSite, 1)},
		trip.TripID, "Ice Camp B", 1_700_000_300_000)

	// ⟦LOCK — §90, 2026-09-03⟧ THE RELATIONSHIPS, ASSERTED. ⚠ Deriving the
	// positions means they are no longer visible in the literals, so the shape
	// the criterion depends on is checked instead of read. A re-pin that
	// quietly put both same-site acks on different sites, or reversed them,
	// would otherwise still build and still go green.
	if ack1.Site != ack3.Site {
		t.Fatalf("W9c-9 fixture (i): ack1 %v and ack3 %v must be on the SAME site — the retracted ack and its "+
			"surviving same-site sibling are what separate |effective| from |raw|", ack1, ack3)
	}
	if !(ack1.Epoch == ack3.Epoch && ack1.Seq < ack3.Seq) {
		t.Fatalf("W9c-9 fixture (ii): ack1 %v must precede ack3 %v on that stream", ack1, ack3)
	}
	if ack2.Site == ack1.Site {
		t.Fatalf("W9c-9 fixture (iii): ack2 %v must be on a DIFFERENT site from ack1 %v — a trip carrying two "+
			"posts' acks is the case rule 1's word \"every\" is about", ack2, ack1)
	}

	// R0 — the pre-POST retraction, naming ack3, authored at site-b at the
	// fold seam. This is what puts the HANDLER in front of a trip with a
	// retracted ack in its history, which nothing in rev 3 did.
	w9cRetractAt(t, st, w9cKey{w9cBoardSite, 1, 0}, ack3, "Ice Camp B", 1_700_000_400_000)

	// ⟦LOCK — §90, 2026-09-03⟧ the derived positions, recorded — so the next
	// reader who meets a collision here sees what this run actually used
	// rather than the doc's illustrative 4/5/6.
	t.Logf("W9c-9 [MEASURED] derived keys: ack1=%v ack3=%v (same site, in order); ack2=%v (different site); "+
		"|raw|=3 |effective|=2", ack1, ack3, ack2)

	// FIXTURE GUARD (iv), and it can fail: |raw| = 3 and |effective| = 2, so the
	// two enumerations are DISTINGUISHABLE. A fixture where they coincided
	// would exercise the word "effective" with nothing.
	if got := w9cRetractionTargets(t, st); len(got) != 1 || got[0] != ack3 {
		t.Fatalf("W9c-9 fixture: pre-POST retractions = %v, want exactly [%v] — raw 3 and effective 2 is the whole point of this fixture", got, ack3)
	}

	// --- one POST: +2 EXACTLY, one per EFFECTIVE ack -------------------
	before := w9cLogCount(t, st)
	rec := w9cUnack(t, srv, trip.TripID)
	page := w9cFollow303(t, srv, rec, "W9c-9")
	if got, want := w9cLogCount(t, st), before+2; got != want {
		t.Errorf("W9c-9: one POST authored %d event(s), want EXACTLY 2 — one retraction per EFFECTIVE ack. THREE means the handler enumerated the RAW alert_ack rows (m7c) and authored a second retraction naming the already-retracted %v; ONE means it retracted only one of the two effective acks (m7), leaving the banner silent on the officer's own screen",
			got-before, ack3)
	}
	if got, want := w9cSortKeys(w9cRetractionTargets(t, st)), w9cSortKeys([]w9cKey{ack3, ack1, ack2}); !w9cKeysEqual(got, want) {
		t.Errorf("W9c-9: after the POST the retractions name %v, want exactly %v", got, want)
	}

	// The banner shows the alert again, on the page the 303 leads to.
	row := w9cRowText(t, page, "the un-acked trip", "ICE TRUCK 7")
	if !strings.Contains(row, "data-alert-row") {
		t.Errorf("W9c-9: after following the 303 the trip has no alert-surface row — the un-ack did not return the alert. Row text:\n%.600s", row)
	}

	// --- second half: an EMPTY effective target set --------------------
	// 409, and NOTHING authored (m7b — refusal-that-authors).
	before = w9cLogCount(t, st)
	rec = w9cUnack(t, srv, quiet.TripID)
	if rec.Code != http.StatusConflict {
		t.Errorf("W9c-9: POST %s on a trip with no effective ack = %d, want 409 — the refusal is exactly the target set's emptiness (F6 §3.4 rule 1); body:\n%.400s",
			w9cUnackPath, rec.Code, rec.Body.String())
	}
	if got := w9cLogCount(t, st); got != before {
		t.Errorf("W9c-9: the 409 refusal authored %d event(s) — a refusal authors NOTHING", got-before)
	}
}
