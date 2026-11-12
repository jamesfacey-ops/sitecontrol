// uncontrolled_test.go — configurable destinations (2026-08-20): a run to an
// UNCONTROLLED destination (a pad with no gate — destination_uncontrolled
// set, destination_site NULL) is outside the deadline machinery entirely. It
// rides the same structural exclusion as the §10 degenerate trip: no capture
// row, no board alert, no banner — and Dispatch refuses to attach a deadline
// to one in the first place, so the scanner can never even be handed the
// contradiction.
package deadline

import (
	"testing"
	"time"

	"sitecontrol/internal/domain"
)

func TestUncontrolledRunIsInvisibleToCaptureScanAndBanner(t *testing.T) {
	st := newSite(t)
	d := newScanner(t, st)

	// An uncontrolled run authored at the ORIGIN and replicated here — the
	// Conneco shape: A records a truck gone to the pad; B hears about it.
	pad := "conneco"
	trip, err := domain.Dispatch(st, origin, 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRUCK 12", DriverPersonID: "d1","⟧
		VehicleID: w7SeedVehicleID(t, st, origin, "TRUCK 12"), DriverPersonID: "d1",
		DestinationUncontrolled: &pad,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	env, err := st.Events(origin, 1, trip.Seq, 1)
	if err != nil || len(env) != 1 {
		t.Fatalf("fetch trip envelope: %v (%d)", err, len(env))
	}
	d.OnApplied(env[0], time.Now(), true)

	// No capture row: nothing for a reboot bridge to resurrect.
	caps, err := d.loadCaptures()
	if err != nil {
		t.Fatal(err)
	}
	if len(caps) != 0 {
		t.Fatalf("capture rows = %d, want 0 — an uncontrolled run is not an expectation", len(caps))
	}

	// No alert, no banner — including during a from-zero replay, whose gate
	// marks every open EXPECTATION overdue-unresolved but must leave
	// destination-site-less trips untouched (§9.4).
	for _, replaying := range []bool{false, true} {
		d.SetReplaying(replaying)
		board, err := d.Evaluate()
		if err != nil {
			t.Fatal(err)
		}
		if len(board.Alerts) != 0 {
			t.Fatalf("replaying=%v: %d alert(s) for an uncontrolled run, want 0: %+v", replaying, len(board.Alerts), board.Alerts)
		}
		if b := board.Banner(); len(b) != 0 {
			t.Fatalf("replaying=%v: banner rows = %d, want 0", replaying, len(b))
		}
	}
}
