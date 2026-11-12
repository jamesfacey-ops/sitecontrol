// witness_y5_trips_test.go — witness/y5-tripweb (row-Y census), the
// `internal/web/trips.go` half. See witness_y5_trip_amend_test.go's file
// comment for the phase this belongs to and where the report lives.
package web

import (
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// =========================================================================
// Q14 — the unattributed-retraction default, gutted
// =========================================================================

// TestWitnessY5Q14UnattributedRetractionGetsItsOwnSentence kills plant Q14
// (trips.go:512, `if by == "" {` -> `if false && by == "" {` in
// `buildReviewLane`'s fourth loop, over `lane.RefRetracted`).
//
// `recorded_by` is nullable on the wire (arrival_unref.go:100) precisely
// because a PEER can author a retraction this post never typed an
// attribution for. The comment at the guard says so: "an unattributed
// retraction is representable from a peer". This test produces exactly that
// — a hand-built `arrival_ref_retracted` envelope with no `recorded_by`
// field at all, applied as if delivered from the peer — and asserts the
// officer reads the authored default sentence, not a blank.
//
// Under the plant, `by` stays `""` and the rendered sentence becomes
// "reference withdrawn by ; the people are here…" — a blank where an
// attribution belongs. The witness asserts the FULL officer-facing phrase,
// so a blank cannot pass by accident of substring matching.
func TestWitnessY5Q14UnattributedRetractionGetsItsOwnSentence(t *testing.T) {
	w := w9bNewMisClickWorld(t, "witness-y5-q14.db")

	// A peer's retraction, delivered with NO recorded_by — exactly the wire
	// shape `w9bRetraction` produces when passed a nil pointer.
	w9bApplyNext(t, w.Store, w.Srv.Cfg.SiteID, w.Srv.Cfg.NodeEpoch, w2Base,
		w9bRetraction(w.A1.ArrivalID, nil))

	page := w9bGet(t, w.Srv, w9bTripsPath)
	rows := w9bReviewRows(page)
	if len(rows) == 0 {
		t.Fatalf("Q14 PRECONDITION: GET %s rendered no review-lane rows at all after the retraction", w9bTripsPath)
	}
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, w.A1.ArrivalID) {
		t.Fatalf("Q14 PRECONDITION: no review-lane row names the retracted arrival %s.\nrows:\n%s", w.A1.ArrivalID, joined)
	}

	const want = "reference withdrawn by a post that recorded no attribution"
	if !strings.Contains(joined, want) {
		t.Fatalf("Q14: no review-lane row carries the unattributed-retraction default sentence.\nwant substring: %q\nrows:\n%s", want, joined)
	}
	// The exact defect shape the plant produces: a blank where the
	// attribution belongs. Checked directly so a passing run on the clean
	// tree is provably not an accident of the positive assertion above.
	const blank = "reference withdrawn by ;"
	if strings.Contains(joined, blank) {
		t.Fatalf("Q14: a review-lane row renders the BLANK attribution %q — the default sentence did not fire.\nrows:\n%s", blank, joined)
	}
}

// =========================================================================
// Working outward — an additional unreached block in this file
// =========================================================================

// TestWitnessY5ArrivalClaimWithNoTypedFactsRendersNothing reaches
// trips.go:550-552 (`if len(parts) == 0 { return "", nil }` in
// `arrivalClaim`) — unreached because every existing caller of `arrivalClaim`
// exercises an UNSOLICITED arrival, which always carries at least a vehicle
// or an origin. An EXPECTED arrival (the ordinary ack-a-dispatch case,
// `RecordExpectedArrival`) types neither: `TripArrival`'s payload for that
// path carries only `trip_ref`, `visits` and `recorded_by`.
//
// Officer-facing behaviour: arrivalClaim's doc comment calls its output "the
// CLAIM" and this is the case where there is none to render — the review-
// lane row must show no parenthetical rather than "(claimed: )" or similar.
func TestWitnessY5ArrivalClaimWithNoTypedFactsRendersNothing(t *testing.T) {
	st := w3OpenRegistered(t, "witness-y5-claim-empty.db")
	srv := New(w3ConfigSiteB(st.Path), st)

	w9aSeedPerson(t, st, "site-a", 1, "drv-claimempty", "Driver ClaimEmpty")
	trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRUCK 7", "drv-claimempty", nil, 240)
	arr, err := domain.RecordExpectedArrival(st, "site-b", 1, trip.TripID, w9aStr("Pad Site Control"), 0, "default")
	if err != nil {
		t.Fatalf("witness fixture: recording the expected arrival: %v", err)
	}

	got, err := srv.arrivalClaim(arr.ArrivalID)
	if err != nil {
		t.Fatalf("Q-outward: arrivalClaim(%s) errored: %v", arr.ArrivalID, err)
	}
	if got != "" {
		t.Fatalf("Q-outward: arrivalClaim(%s) = %q, want \"\" — an expected arrival types neither a vehicle nor an origin", arr.ArrivalID, got)
	}
}
