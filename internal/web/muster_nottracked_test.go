// muster_nottracked_test.go — the "not tracked" category rendered on the
// muster (operator ruling, 2026-08-20). Behavioral assertions only, against
// pre-existing helpers, styles stripped, outcomes on body text.
package web

import (
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

func TestMusterShowsNotTrackedDropOff(t *testing.T) {
	srv, w := newDestinationsTestServer(t)

	// BUS 7 site-b → site-a drops Pat en route; the site-a officer marks
	// them not aboard (the injected same-store pattern the fold suites use).
	dest, deadline := "site-a", int64(60)
	trip, err := domain.Dispatch(w.st, "site-b", 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "BUS 7", DriverPersonID: w.persons[0].PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.st, "site-b", "BUS 7"), DriverPersonID: w.persons[0].PersonID,
		OccupantPersonIDs: []string{w.persons[1].PersonID},
		DestinationSite:   &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.RecordExpectedArrivalExcluding(w.st, "site-a", 1, trip.TripID,
		[]string{w.persons[1].PersonID}, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}

	page := getPage(t, srv, "/muster")
	if !strings.Contains(page, `data-muster-section="not-tracked"`) {
		t.Fatalf("live muster has no Not tracked section:\n%s", page)
	}
	if !strings.Contains(page, "Not tracked") {
		t.Fatalf("the Not tracked section is not labelled in words:\n%s", page)
	}
	if !strings.Contains(page, `data-tally-count="not-tracked">1<`) {
		t.Fatalf("the headcount does not count the untracked person:\n%s", page)
	}
	// The exact shipped wording — honest in both directions.
	// ⟦was: want := "on <code>BUS 7</code> to site-a, not aboard at arrival — whereabouts not recorded"⟧
	// Fix round 3 (FINDINGS-R5 F3a/F10): display name, never the raw id; the arrival's time.
	want := regexp.MustCompile(`on <code>BUS 7</code> to [^,<]+, not aboard at arrival at \d\d:\d\d \d\d/\d\d by [^<]+&#39;s clock — whereabouts not recorded`)
	if !want.MatchString(page) || strings.Contains(page, "to site-a,") {
		t.Fatalf("live muster missing the wording %s", want)
	}
	if !strings.Contains(page, "Pat Passenger") {
		t.Fatal("the dropped-off person is not named")
	}
	// Framed as a knowledge gap, not a defect, and not reassuring.
	if !strings.Contains(page, "not an error") || !strings.Contains(page, "no timer will fire") {
		t.Fatal("the section must say both halves: routine (not an error) AND unwatched (no timer will fire)")
	}
	// Off the on-site count; named exactly once (never in the roster).
	if got := strings.Count(page, "Pat Passenger"); got != 1 {
		t.Fatalf("Pat Passenger appears %d times, want exactly 1 (the not-tracked row)", got)
	}

	// The print carries it too.
	print := getPage(t, srv, "/muster/print")
	if !want.MatchString(print) || !strings.Contains(print, "Pat Passenger") {
		t.Fatal("printed muster does not carry the not-tracked row")
	}
}

// A plain gate check-out must never appear as not tracked — that person is
// untracked in the expected, uninteresting way (no trip membership).
func TestPlainCheckOutIsNotListedAsNotTracked(t *testing.T) {
	srv, w := newDestinationsTestServer(t)
	walkIn, err := domain.DispatchLocalCheckIn(w.st, "site-b", 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "gate walk-in", DriverPersonID: w.persons[1].PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.st, "site-b", "gate walk-in"), DriverPersonID: w.persons[1].PersonID,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.RecordExpectedArrival(w.st, "site-b", 1, walkIn.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	open, err := domain.OpenVisitsAt(w.st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range open {
		if err := domain.CheckOutVisit(w.st, "site-b", 1, v.VisitID, nil, 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	page := getPage(t, srv, "/muster")
	// Asserted on the section's own structural hook, not on the words "Not
	// tracked": U3's headcount block names every category unconditionally
	// (including the ones holding nobody — F1's lesson, "the sheet says what
	// it knows, including that it knows of nobody"), so the phrase now
	// appears on every muster. What must not appear is the LISTING section,
	// and the count must be an explicit zero. Both halves are checked, which
	// is strictly more than the old substring proved.
	if strings.Contains(page, `data-muster-section="not-tracked"`) {
		t.Fatalf("a plain check-out produced a Not tracked section:\n%s", page)
	}
	if !strings.Contains(page, `data-tally-count="not-tracked">0<`) {
		t.Fatalf("the headcount does not affirmatively report nobody untracked:\n%s", page)
	}
}
