package domain

import (
	"strings"
	"testing"
)

// TestFix4ArrivedBackHereLoserNamesTheVehicle is L03-06: "Arrived back here"
// on a run whose arrival is already on record names the vehicle and the
// post, never the trip id or its state. FAIL BEFORE: "trip <id> has already
// arrived at …".
func TestFix4ArrivedBackHereLoserNamesTheVehicle(t *testing.T) {
	for _, tc := range []struct {
		name  string
		first func(w *fix2World) error
		where string
	}{
		{"same post", func(w *fix2World) error {
			_, err := RecordReturnToOrigin(w.origin, "site-a", 1, w.trip.TripID, nil, nil, 0, "default", false)
			return err
		}, "recorded as arrived here"},
		{"destination", func(w *fix2World) error {
			_, err := RecordExpectedArrivalExcluding(w.dest, "site-b", 1, w.trip.TripID, nil, nil, 0, "default")
			return err
		}, "recorded as arrived at " + SiteLabel("site-b")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newFix2World(t)
			if err := tc.first(w); err != nil {
				t.Fatal(err)
			}
			w.sync(t)
			before, _ := w.origin.Watermark("site-a", 1)
			_, err := RecordReturnToOrigin(w.origin, "site-a", 1, w.trip.TripID, nil, nil, 0, "default", false)
			if err == nil {
				t.Fatal("a second arrival of the run was ACCEPTED")
			}
			msg := err.Error()
			for _, bad := range []string{w.trip.TripID, "trip ", "(state "} {
				if strings.Contains(msg, bad) {
					t.Errorf("refusal %q contains %q", msg, bad)
				}
			}
			for _, want := range []string{"TRK-28", tc.where, "Nothing was recorded"} {
				if !strings.Contains(msg, want) {
					t.Errorf("refusal %q lacks %q", msg, want)
				}
			}
			if after, _ := w.origin.Watermark("site-a", 1); after != before {
				t.Fatalf("refused but authored %d event(s)", after-before)
			}
		})
	}
}
