package deadline

// fix2_fd_road_work_vehicle_test.go — FINDINGS-R4 D12 (L17-07 = L20 W-09):
// the road-work overdue banner, the outstanding-alerts rows and the log
// line all print Alert.Vehicle (⟦was: "…, the Phase-3 VERIFY rows and the log
// line" — the weather VERIFY row went with weather phases, James
// 2026-10-04⟧), and for a road-work
// run it was "" — domain.OpenRoadWork never filled Expectation.Vehicle. With
// several crews out, the bar named the driver but not the machine.
// James, 2026-10-04: every reader that switches on trip kind needs a
// road-work arm (cluster D).

import (
	"strings"
	"testing"
	"time"
)

// ⟦was: "TestRoadWorkOverdueAlertBannerAndPhaseReviewNameTheVehicle", built
// on phase_test.go's settable-phase scanner, with a second half asserting the
// phase-3 VERIFY row named the vehicle too. That half and the scanner went
// with weather phases (James, 2026-10-04); the alert and banner halves stand.⟧
func TestRoadWorkOverdueAlertAndBannerNameTheVehicle(t *testing.T) {
	st := newSite(t)
	ps := newScanner(t, st)
	trip, env := dispatchRoadWork(t, st, 2)
	authored := time.UnixMilli(env.AuthoredAt.WallMS)
	ps.WallNow = func() time.Time { return authored.Add(3 * time.Minute) }

	board := mustEval(t, ps)
	a := findAlert(t, board, trip.TripID)
	if a.Status != StatusOverdueLive || !a.RoadWork {
		t.Fatalf("precondition: status %q roadWork %v, want an overdue road-work row", a.Status, a.RoadWork)
	}
	if !strings.Contains(a.Vehicle, "GRADER 7") {
		t.Fatalf("overdue road-work alert names vehicle %q, want GRADER 7 (D12)", a.Vehicle)
	}
	var inBanner bool
	for _, b := range board.Banner() {
		if b.TripID == trip.TripID {
			inBanner = true
			if !strings.Contains(b.Vehicle, "GRADER 7") {
				t.Fatalf("banner row names vehicle %q, want GRADER 7", b.Vehicle)
			}
		}
	}
	if !inBanner {
		t.Fatal("the overdue road-work run is not on the banner")
	}
}
