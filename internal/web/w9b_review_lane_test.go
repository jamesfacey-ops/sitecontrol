// w9b_review_lane_test.go — W9b-5 (F4 §12 T13c): the retracted arrival lands
// on a rendered SURFACE.
//
// ⚠ WHY THIS CRITERION EXISTS AT ALL, restated because it is the whole design
// of the test. F4 §3.6: "A builder who adds only the disposition constant and
// writes it in Pass 1's skip branch satisfies EVERY state-shaped assertion in
// W9b and the retracted arrival lands on no surface at all — not the review
// lane (no query selects it), not the expected list (it is no longer
// ArrivalExpected) — and the officer who follows this design's own instruction,
// 'resolve through review', finds nothing there." That build passes every test
// in `internal/domain/w9b_unbind_test.go`. It fails only here, and only because
// this test GETs the page rather than asserting the row.
//
// The lane is a three-field struct with three hardcoded `store.Eq` scans and
// ONE production caller [DOC — F4 §3.6, measured at `tripauthor.go`'s
// `ReviewLane`/`LoadReviewLane` and `web/trips.go`'s sole call site], so
// `ArrivalRefRetracted` requires all three of: a fourth `ReviewLane` field, a
// fourth filtered scan, and a template block.
//
// ⚠ THE ASSERTION IS SCOPED TO THE ROW, not to the page. All three literals —
// the retracted arrival, the repudiated trip id, the retracting post — must
// appear in the SAME rendered review-lane row. A page-wide `strings.Contains`
// would be satisfied by the topbar (which renders this post's name), by the
// board row (which renders the trip id) and by nothing at all having been
// added, which is precisely mutant m22 going green.
//
// KILLER MUTANT: m22 — the invisible disposition.
package web

import (
	"net/url"
	"strings"
	"testing"
)

// TestW9b5TheRetractedArrivalIsRenderedOnTheReviewLanePage — W9b-5.
func TestW9b5TheRetractedArrivalIsRenderedOnTheReviewLanePage(t *testing.T) {
	w := w9bNewMisClickWorld(t, "w9b-reviewlane.db")
	if !w9bRouteExists(t, w.Srv, "POST", w9bUnrefPath) {
		t.Fatalf("W9b-5: `POST %s` is NOT REGISTERED, so no retraction can be authored through the surface this criterion is about. On the pre-chunk tree that is the produced red", w9bUnrefPath)
	}

	// The lane BEFORE — so "the row is there" cannot be true because the page
	// always had a warning on it.
	before := w9bReviewRows(w9bGet(t, w.Srv, w9bTripsPath))

	_, _, code, authored := w9bUnref(t, w.Srv, w.Store, url.Values{w9bFieldArrival: {w.A1.ArrivalID}})
	if code != 303 || !authored {
		t.Fatalf("W9b-5 PRECONDITION: `POST %s` returned %d (authored=%v), want 303 with an event appended", w9bUnrefPath, code, authored)
	}

	page := w9bGet(t, w.Srv, w9bTripsPath)
	rows := w9bReviewRows(page)
	if len(rows) <= len(before) {
		t.Fatalf("W9b-5 (m22 — the invisible disposition): `GET %s` rendered %d review-lane rows before the un-reference and %d after.\nbefore: %v\nafter:  %v\n"+
			"F4 §3.6: the lane is a three-field struct with three hardcoded `store.Eq` scans and NONE selects the new disposition, so a build that adds only the constant leaves the officer "+
			"following its own \"resolve through review\" instruction and finding nothing. That is not merely a missing row: §8's shape argument loses a limb, because "+
			"\"the empty state is a legal representation of the open question\" represents nothing if the row does not exist",
			w9bTripsPath, len(before), len(rows), before, rows)
	}

	// The retracting post is DERIVED from the config the server loaded, never
	// hardcoded (VF §5's rule). The retraction was authored at this post.
	wantPost := w9bConfigPostName(w.Srv.Cfg)
	if wantPost == "" {
		t.Fatalf("w9b FIXTURE DEFECT: the server's config has no post name, so the retracting-post assertion below would be vacuous")
	}

	var match string
	for _, row := range rows {
		if strings.Contains(row, w.A1.ArrivalID) && strings.Contains(row, w.X.TripID) && strings.Contains(row, wantPost) {
			match = row
			break
		}
	}
	if match == "" {
		t.Fatalf("W9b-5: no single rendered review-lane row carries ALL THREE of the retracted arrival %q, the repudiated trip %q and the retracting post %q.\nrows rendered:\n%s\n"+
			"F4 §3.6's disposition text is \"checked in against ⟨trip⟩ — reference withdrawn by ⟨post⟩; the people are here; resolve through review\". "+
			"The three are asserted in ONE row on purpose: page-wide containment would be satisfied by the topbar's post name and the board row's trip id, with no lane row added at all",
			w.A1.ArrivalID, w.X.TripID, wantPost, strings.Join(rows, "\n"))
	}

	// The row must not quietly become a re-pointing affordance either — the
	// third of W9b-2's three surfaces, asserted here as well because this is
	// the test that actually renders it.
	w9bAssertNoRepointAffordance(t, "W9b-5's rendered review-lane row", match, w.Y.TripID)
}
