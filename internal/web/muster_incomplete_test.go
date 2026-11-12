// muster_incomplete_test.go — F1 (campaign finding, adversarial-verification
// posture: "the muster must say when it knows it is incomplete"): a row-31
// crash-recovery trip (GateCheckIn authored its dispatch but died before its
// arrival, checkin.go) previously rendered ONLY on /gate. An officer looking
// at /muster during an evacuation got no cue at all that the roster below
// might be short people. New coverage only — nothing in muster_test.go or
// gate_test.go is modified.
//
// CSS-class trap (shared project context / F19): every assertion here
// anchors on the literal class="incomplete-warn" ATTRIBUTE, never a bare
// substring search for "incomplete-warn" — that class name also appears as
// a bare CSS selector (.incomplete-warn { ... }) in the one shared
// stylesheet (templates/_styles.html, inlined into every render), so a bare
// substring match would be vacuously true regardless of whether the warning
// box actually rendered. The shared stylesheet makes that MORE true than it
// was when the rule lived in muster.html alone: the selector is now on every
// page, not just this one.
package web

import (
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// crashCheckIn authors ONLY the dispatch half of a gate check-in — never its
// arrival — reproducing row 31's crash window directly (the same shape
// GateCheckIn would leave behind if the process died between its two Author
// calls). IncompleteCheckIns/ResumeCheckIn (checkin.go) exist to recover
// exactly this state.
func crashCheckIn(t *testing.T, st *store.Store, siteID, vehicle, driverPersonID string) domain.Trip {
	t.Helper()
	trip, err := domain.DispatchLocalCheckIn(st, siteID, 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: vehicle, DriverPersonID: driverPersonID,"⟧
		VehicleID: w7SeedVehicleID(t, st, siteID, vehicle), DriverPersonID: driverPersonID,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	return trip
}

// TestMusterLiveSurfacesIncompleteCheckIn is F1's core fix: the LIVE board
// must show the row-31 warning, not just /gate.
func TestMusterLiveSurfacesIncompleteCheckIn(t *testing.T) {
	srv, st := newMusterTestServer(t, 0)
	p, err := domain.CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	crashCheckIn(t, st, "site-a", "TRUCK 9", p.PersonID)

	code, body := getMuster(t, srv, "/muster")
	if code != 200 {
		t.Fatalf("GET /muster = %d: %s", code, body)
	}
	if !strings.Contains(body, `class="incomplete-warn"`) {
		t.Fatalf("live muster did not surface the incomplete check-in:\n%s", body)
	}
	if !strings.Contains(body, "TRUCK 9") || !strings.Contains(body, "Dana Driver") {
		t.Fatalf("incomplete check-in row missing vehicle/driver detail:\n%s", body)
	}
}

// TestMusterPrintDirectSurfacesIncompleteCheckIn covers the OTHER half of
// the fix instruction — "and on the printed muster sheet" — for the direct
// (no ?visits=) one-click print path.
func TestMusterPrintDirectSurfacesIncompleteCheckIn(t *testing.T) {
	srv, st := newMusterTestServer(t, 0)
	p, err := domain.CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	crashCheckIn(t, st, "site-a", "TRUCK 9", p.PersonID)

	code, body := getMuster(t, srv, "/muster/print")
	if code != 200 {
		t.Fatalf("GET /muster/print = %d: %s", code, body)
	}
	if !strings.Contains(body, `class="incomplete-warn"`) {
		t.Fatalf("direct print did not surface the incomplete check-in:\n%s", body)
	}
}

// TestMusterPrintFrozenReprintStillSurfacesALaterIncompleteCheckIn proves
// the warning is deliberately NOT frozen into a ?visits= reprint's snapshot
// (musterData.Incomplete's doc comment): a reprint's MEMBER set is frozen at
// print time (§8.8-i), but a person mid-check-in has no visit row yet, so
// they can never be part of that frozen set either way — the warning must
// still be reported live, off whatever the site currently knows, no matter
// when the ?visits= link was minted.
func TestMusterPrintFrozenReprintStillSurfacesALaterIncompleteCheckIn(t *testing.T) {
	srv, st := newMusterTestServer(t, 0)
	p1, err := domain.CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	checkIn(t, st, "site-a", p1.PersonID, nil) // a COMPLETE check-in to freeze

	_, liveBody := getMuster(t, srv, "/muster")
	printHref := extractPrintHref(t, liveBody)

	// Crash a SECOND, unrelated check-in AFTER the print link was minted.
	p2, err := domain.CreatePerson(st, "site-a", 1, "Late Arrival", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	crashCheckIn(t, st, "site-a", "TRUCK LATE", p2.PersonID)

	code, body := getMuster(t, srv, printHref)
	if code != 200 {
		t.Fatalf("GET %s = %d: %s", printHref, code, body)
	}
	if !strings.Contains(body, `class="incomplete-warn"`) {
		t.Fatalf("frozen reprint did not surface a check-in that crashed AFTER freezing:\n%s", body)
	}
	if !strings.Contains(body, "Dana Driver") {
		t.Fatalf("frozen reprint lost its own originally-frozen member:\n%s", body)
	}
}

// TestMusterNeverShowsIncompleteWarningWhenNothingIsIncomplete is the
// negative control: an ordinary, fully-completed check-in must never trip
// the warning (proves the assertions above are non-vacuous).
func TestMusterNeverShowsIncompleteWarningWhenNothingIsIncomplete(t *testing.T) {
	srv, st := newMusterTestServer(t, 0)
	p, err := domain.CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	checkIn(t, st, "site-a", p.PersonID, nil)

	_, liveBody := getMuster(t, srv, "/muster")
	if strings.Contains(liveBody, `class="incomplete-warn"`) {
		t.Fatalf("live muster showed the incomplete-check-in warning with nothing incomplete:\n%s", liveBody)
	}
	_, printBody := getMuster(t, srv, "/muster/print")
	if strings.Contains(printBody, `class="incomplete-warn"`) {
		t.Fatalf("direct print showed the incomplete-check-in warning with nothing incomplete:\n%s", printBody)
	}
}
