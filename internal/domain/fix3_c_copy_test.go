package domain

import (
	"strings"
	"testing"
)

// FIX3 stream C, F3b + F13. James, 2026-10-04: no screen shows a raw site id.
// FAIL BEFORE (e3d507d): the D17 refusal printed SiteLabel("site-a") = "site-a"
// twice, whatever display name the post's config gave it.
func TestFix3C_D17RefusalNamesThePostByItsDisplayName(t *testing.T) {
	w := newFix2World(t)
	w.dest.SiteNamer = func(id string) string {
		if id == "site-a" {
			return "Ice Road Entrance"
		}
		return id
	}
	if _, err := RecordReturnToOrigin(w.origin, "site-a", 1, w.trip.TripID, nil, nil, 0, "default", false); err != nil {
		t.Fatal(err)
	}
	w.sync(t)
	_, err := RecordExpectedArrivalExcluding(w.dest, "site-b", 1, w.trip.TripID, nil, nil, 0, "default")
	if err == nil {
		t.Fatal("accepted")
	}
	msg := err.Error()
	if strings.Contains(msg, "site-a") {
		t.Errorf("refusal shows the raw site id: %q", msg)
	}
	if got := strings.Count(msg, "Ice Road Entrance"); got != 2 {
		t.Errorf("display name appears %d times, want 2 (recorded back at …, … 's muster): %q", got, msg)
	}
}

// FAIL BEFORE: "trip <id> has already arrived here (state arrived)" — a raw
// id-bearing trip id and a state word nobody reads.
func TestFix3C_SamePostLoserSaysRunNotTripIDAndState(t *testing.T) {
	w := newFix2World(t)
	if _, err := RecordExpectedArrival(w.dest, "site-b", 1, w.trip.TripID, nil, 0, "default"); err != nil {
		// the dest may need the dispatch replicated first
		w.sync(t)
		if _, err = RecordExpectedArrival(w.dest, "site-b", 1, w.trip.TripID, nil, 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	_, err := RecordExpectedArrival(w.dest, "site-b", 1, w.trip.TripID, nil, 0, "default")
	if err == nil {
		t.Fatal("a second arrival was accepted")
	}
	msg := err.Error()
	for _, bad := range []string{w.trip.TripID, "(state ", "site-a-trip"} {
		if strings.Contains(msg, bad) {
			t.Errorf("same-post loser %q still contains %q", msg, bad)
		}
	}
	if !strings.Contains(msg, "has already arrived here") {
		t.Errorf("lost the ordinary sentence: %q", msg)
	}
}
