// d18_arrival_race_test.go — FINDINGS-R4 D18 (L13 R4-RACE-ARR): two
// simultaneous Arrived, or "Arrived back here", presses authored two arrival
// records (5 of 5 live tries) because the door checked "already arrived" and
// then wrote in separate store calls. The phantom second arrival then blocked
// unref. The check and the write are now one transaction.
package domain

import (
	"strings"
	"sync"
	"testing"
)

// d18Race fires n presses of door together and returns how many succeeded.
func d18Race(n int, door func() error) (ok int, errs []error) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := door()
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				ok++
			} else {
				errs = append(errs, err)
			}
		}()
	}
	close(start)
	wg.Wait()
	return ok, errs
}

func d18ArrivalsFor(t *testing.T, w *c6World, siteStore string, tripID string) int {
	t.Helper()
	st := w.a
	if siteStore == "site-b" {
		st = w.b
	}
	arrs, err := Arrivals(st)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, a := range arrs {
		if a.TripRef != nil && *a.TripRef == tripID {
			n++
		}
	}
	return n
}

func d18Dispatch(t *testing.T, w *c6World, vehicle string) Trip {
	t.Helper()
	dest := "site-b"
	deadline := int64(60)
	trip, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", vehicle), DriverPersonID: w.driver.PersonID,
		OccupantPersonIDs: []string{w.passenger.PersonID},
		DestinationSite:   &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	return trip
}

// TestD18SimultaneousArrivedPressesAuthorOneArrival: eight Arrived presses
// at the destination at once — exactly one arrival, seven refusals saying
// the trip has already arrived.
func TestD18SimultaneousArrivedPressesAuthorOneArrival(t *testing.T) {
	w := newC6World(t)
	w.checkInAtA()
	trip := d18Dispatch(t, w, "TRK-RACE-1")
	w.syncBFromA()

	ok, errs := d18Race(8, func() error {
		_, err := RecordExpectedArrival(w.b, "site-b", 1, trip.TripID, nil, 0, "default")
		return err
	})
	if n := d18ArrivalsFor(t, w, "site-b", trip.TripID); n != 1 || ok != 1 {
		t.Fatalf("D18: 8 simultaneous Arrived presses authored %d arrivals (%d accepted), want exactly 1 — check-then-write without a lock", n, ok)
	}
	for _, err := range errs {
		if !strings.Contains(err.Error(), "has already arrived here") {
			t.Fatalf("D18: a losing press was refused with %q, want the ordinary \"has already arrived here\"", err)
		}
	}
}

// TestD18SimultaneousArrivedBackHerePressesAuthorOneArrival: the same at the
// origin's "Arrived back here" door.
func TestD18SimultaneousArrivedBackHerePressesAuthorOneArrival(t *testing.T) {
	w := newC6World(t)
	w.checkInAtA()
	trip := d18Dispatch(t, w, "TRK-RACE-2")

	ok, errs := d18Race(8, func() error {
		_, err := RecordReturnToOrigin(w.a, "site-a", 1, trip.TripID, nil, nil, 0, "default", false)
		return err
	})
	if n := d18ArrivalsFor(t, w, "site-a", trip.TripID); n != 1 || ok != 1 {
		t.Fatalf("D18: 8 simultaneous \"Arrived back here\" presses authored %d arrivals (%d accepted), want exactly 1", n, ok)
	}
	for _, err := range errs {
		if !strings.Contains(err.Error(), "already been recorded as arrived") {
			t.Fatalf("D18: a losing press was refused with %q, want \"already been recorded as arrived\"", err)
		}
	}
}
