// fix3_e_test.go — fix round 3, stream E (FINDINGS-R5): trips and NotTracked.
package domain

import "testing"

// TestQR52OnlyTheOriginOrTheDestinationMayAddAboard — Q-R5-2 (L14-1).
// James, 2026-10-04: only the trip's origin or destination may add someone
// aboard, the same rule as a driver change (rung 8a). FAIL BEFORE: a third
// post's "Aboard after all" of a FREE person was Recorded.
func TestQR52OnlyTheOriginOrTheDestinationMayAddAboard(t *testing.T) {
	w := newFix2World(t)
	third := newDomainTestStore(t)
	fix2Ship(t, w.origin, third, "site-a")
	before, _ := third.Watermark("site-c", 1)
	_, err := AmendTrip(third, "site-c", 1, TripAmendment{TripID: w.trip.TripID, OccupantsAdd: []string{w.b.PersonID}}, 0, "default")
	fix2Refused(t, third, "site-c", before, err,
		"only "+SiteLabel("site-a")+" or "+SiteLabel("site-b"), "who was aboard this trip")
	if after, _ := third.Watermark("site-c", 1); after != 0 {
		t.Fatalf("the third post holds %d row(s) of its own, want 0", after)
	}
	// The origin and the destination are still accepted.
	if _, err := AmendTrip(w.origin, "site-a", 1, TripAmendment{TripID: w.trip.TripID, OccupantsAdd: []string{w.b.PersonID}}, 0, "default"); err != nil {
		t.Fatalf("the origin's Aboard after all was refused: %v", err)
	}
	if _, err := AmendTrip(w.dest, "site-b", 1, TripAmendment{TripID: w.trip.TripID, OccupantsAdd: []string{w.c.PersonID}}, 0, "default"); err != nil {
		t.Fatalf("the destination's Aboard after all was refused: %v", err)
	}
}
