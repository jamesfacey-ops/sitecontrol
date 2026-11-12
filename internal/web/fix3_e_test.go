// fix3_e_test.go — fix round 3, stream E (FINDINGS-R5): the NOT TRACKED
// rows and copy (F3a, F10), the muster/trips lines of F13, and F15's
// tally-equals-names pin.
package web

import (
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
)

// fix3eNotTrackedWorld: TRK-100 site-a → site-b ("Pad Site Control") with
// Nadeau driving and Ohara, Petrov aboard. The truck is renumbered TRK-12
// while out; Petrov is taken off the run by a correction (the D1 row), and
// the run arrives at site-b without Ohara (the not-aboard row).
func fix3eNotTrackedWorld(t *testing.T) (*roadWorld, string) {
	t.Helper()
	w := newRoadWorld(t)
	w.srv.Cfg.LoginPassword = testLoginPassword
	n := "TRK-12"
	if _, err := domain.AmendVehicle(w.st, "site-a", 1, domain.VehicleAmendment{VehicleID: w.trip.VehicleID, Number: &n}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	ohara, petrov := w.riding[1], w.riding[2]
	if _, err := domain.AmendTrip(w.st, "site-a", 1, domain.TripAmendment{TripID: w.trip.TripID, OccupantsRemove: []string{petrov.PersonID}}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if _, err := domain.RecordExpectedArrivalExcluding(w.st, "site-b", 1, w.trip.TripID, []string{ohara.PersonID}, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	return w, styleRe.ReplaceAllString(rawPage(t, w.srv, "/muster"), "")
}

// TestFix3ENotTrackedRowsNameTheDestinationTheLiveTruckAndATime — F3a, F10
// (L01-6, L07 O-L07-2, F-L06-1). FAIL BEFORE: "on <code>TRK-100</code> to
// site-b, …" — the raw site id, the as-dispatched truck and no time.
func TestFix3ENotTrackedRowsNameTheDestinationTheLiveTruckAndATime(t *testing.T) {
	w, page := fix3eNotTrackedWorld(t)
	// Lead, 2026-10-04: on screen AND in print.
	print := styleRe.ReplaceAllString(rawPage(t, w.srv, "/muster/print"), "")
	if p := print[strings.Index(print, `data-muster-section="not-tracked"`):]; strings.Contains(p, "to site-b") || !strings.Contains(p, "to Pad Site Control") {
		t.Errorf("F3a (print): the printed NOT TRACKED rows do not name the destination by display name:\n%s", p)
	}
	if !d80InSection(page, "not-tracked", w.riding[1].Name) || !d80InSection(page, "not-tracked", w.riding[2].Name) {
		t.Fatalf("PRECONDITION: Ohara and Petrov are not both NOT TRACKED:\n%s", page)
	}
	at := strings.Index(page, `data-muster-section="not-tracked"`)
	section := page[at:]
	if strings.Contains(section, "to site-b") {
		t.Errorf("F3a: a NOT TRACKED row names the raw site id \"site-b\":\n%s", section)
	}
	// Each time names the clock it was read on (B's F9 rule, elsewhereSince):
	// the correction was recorded HERE (site-a), the arrival at site-b.
	// ⟦was: "at \d\d:\d\d \d\d/\d\d" with no clock named⟧
	here := regexp.QuoteMeta(template.HTMLEscapeString(w.srv.Cfg.SiteName("site-a") + "'s clock"))
	there := regexp.QuoteMeta(template.HTMLEscapeString(w.srv.Cfg.SiteName("site-b") + "'s clock"))
	for _, want := range []*regexp.Regexp{
		regexp.MustCompile(`on <code>TRK-12</code> \(dispatched as <code>TRK-100</code>\) to Pad Site Control, taken off this run by a trip correction at \d\d:\d\d \d\d/\d\d by ` + here + ` — whereabouts not recorded`),
		regexp.MustCompile(`on <code>TRK-12</code> \(dispatched as <code>TRK-100</code>\) to Pad Site Control, not aboard at arrival at \d\d:\d\d \d\d/\d\d by ` + there + ` — whereabouts not recorded`),
	} {
		if !want.MatchString(section) {
			t.Errorf("F10: no NOT TRACKED row matches %s:\n%s", want, section)
		}
	}
}

// TestFix3ENotTrackedTallyAndFootnoteCoverCorrectionRows — F10 (F-L06-2
// (iii)). FAIL BEFORE: both described only "did not arrive with it".
func TestFix3ENotTrackedTallyAndFootnoteCoverCorrectionRows(t *testing.T) {
	_, page := fix3eNotTrackedWorld(t)
	if n := strings.Count(page, "taken off a run’s list by a trip correction"); n < 2 {
		t.Fatalf("F10: the NOT TRACKED tally note and footnote should both cover correction rows; found the phrase %d time(s):\n%s", n, page)
	}
}

// TestFix3EPrintNoteNamesWhatTheSheetCarries — F13 (L13-3d). FAIL BEFORE:
// "the printed sheet is the roster only", while the print also carries On
// the road and Not tracked.
func TestFix3EPrintNoteNamesWhatTheSheetCarries(t *testing.T) {
	_, page := fix3eNotTrackedWorld(t)
	if strings.Contains(page, "is the roster only") {
		t.Errorf("F13: the muster still says the printed sheet is the roster only")
	}
	if !strings.Contains(page, "the printed sheet carries the roster, On the road and\n        Not tracked") {
		t.Errorf("F13: the away note does not say what the printed sheet carries:\n%s", page)
	}
}

// TestFix3ED80RefusalSaysNotTracked — F10 (F-L06-2 (i)). FAIL BEFORE: "would
// leave them on no sheet at either post", untrue since the D1 ruling.
func TestFix3ED80RefusalSaysNotTracked(t *testing.T) {
	got := unsightedRemovalRefusal([]domain.OccupantRef{{PersonID: "p1", Name: "Ohara, Wendy"}})
	if strings.Contains(got, "no sheet") {
		t.Errorf("F10: the D-80 refusal still says \"no sheet\": %q", got)
	}
	if !strings.Contains(got, "NOT TRACKED on this run") {
		t.Errorf("F10: the D-80 refusal does not say they would be NOT TRACKED on this run: %q", got)
	}
}

// TestFix3ENewBDriverHelpNoLongerCoachesARemove — F10 (F-L06-2 (ii)). FAIL
// BEFORE: the help said to put a never-aboard previous driver in "Remove",
// which D-80 always refuses on an open trip.
func TestFix3ENewBDriverHelpNoLongerCoachesARemove(t *testing.T) {
	w := newRoadWorld(t)
	w.srv.Cfg.LoginPassword = testLoginPassword
	page := w9bGet(t, w.srv, "/trips/amend?trip_id="+url.QueryEscape(w.trip.TripID))
	if !strings.Contains(page, "keeps the previous driver on the trip's list") {
		t.Fatalf("PRECONDITION: the NEW-b driver help is not on the page:\n%s", page)
	}
	if strings.Contains(page, "If they were never aboard, put them in") {
		t.Errorf("F10: the driver help still coaches a Remove that D-80 refuses")
	}
}

// TestFix3EReferenceWithdrawnRowIsInWordsWithANextStep — F13 (L06-09).
// FAIL BEFORE: "<strong>reference-withdrawn</strong>: arrival … checked in
// against trip … ; resolve through review (F4 §3.6)".
func TestFix3EReferenceWithdrawnRowIsInWordsWithANextStep(t *testing.T) {
	w := w9bNewMisClickWorld(t, "fix3e-refwithdrawn.db")
	if _, _, code, authored := w9bUnref(t, w.Srv, w.Store, url.Values{w9bFieldArrival: {w.A1.ArrivalID}}); code != 303 || !authored {
		t.Fatalf("PRECONDITION: the un-reference returned %d (authored=%v)", code, authored)
	}
	page := w9bGet(t, w.Srv, w9bTripsPath)
	var row string
	for _, r := range w9bReviewRows(page) {
		if strings.HasPrefix(r, "reference-withdrawn: ") {
			row = r
		}
	}
	if row == "" {
		t.Fatalf("PRECONDITION: no reference-withdrawn row:\n%s", page)
	}
	for _, bad := range []string{"F4 §3.6", "resolve through review", "<strong>reference-withdrawn</strong>"} {
		if strings.Contains(row, bad) {
			t.Errorf("F13: the row still carries %q:\n%s", bad, row)
		}
	}
	for _, want := range []string{"<strong>Arrival no longer matched to its trip</strong>", "ICE TRUCK 7", "confirm by radio with"} {
		if !strings.Contains(row, want) {
			t.Errorf("F13: the row does not carry %q:\n%s", want, row)
		}
	}
}

// TestFix3EInTransitTallyEqualsRoadBlockNames — F15 (L08 OBS-HAND). A rider
// who checks in again at the origin while the run is open leaves the road
// block by in_transit.go rule 3, and the tally follows the same rows. This
// PINS the agreement (both are read from one InTransitFrom result); the
// evidence (h18.check: "tally=10 road-block names=10 /trips outbound
// occupants=11") showed the 11 was the /trips row, not the road block.
func TestFix3EInTransitTallyEqualsRoadBlockNames(t *testing.T) {
	w := newRoadWorld(t)
	back := w.riding[2]
	checkIn(t, w.st, "site-a", back.PersonID, nil)
	page := styleRe.ReplaceAllString(rawPage(t, w.srv, "/muster"), "")
	if !strings.Contains(page, `data-tally-count="in-transit">2<`) {
		t.Fatalf("F15: the in-transit tally is not 2 after one of three riders checked in again at the origin:\n%s", page)
	}
	at := strings.Index(page, `class="road-trip"`)
	if at < 0 {
		t.Fatalf("PRECONDITION: no road block:\n%s", page)
	}
	end := strings.Index(page[at:], "</div>")
	block := page[at : at+end]
	names := strings.Count(block, "<li>")
	if names != 2 {
		t.Errorf("F15: the road block lists %d names, the tally says 2:\n%s", names, block)
	}
	if strings.Contains(block, back.Name) {
		t.Errorf("F15: %s is back on site at the origin and still named on the road:\n%s", back.Name, block)
	}
}

// TestFix3EAmbiguousRowLeadsWithWordsNotTheKindToken — F13 ("ambiguous-
// arrival:" prefix, L08/L13; routed from C, trips.go:577). FAIL BEFORE: the
// row rendered <strong>ambiguous-arrival</strong>.
func TestFix3EAmbiguousRowLeadsWithWordsNotTheKindToken(t *testing.T) {
	srv, _, _, _, _ := c1cOwnParked(t)
	body := c1cGet(t, srv, "/trips")
	if !strings.Contains(body, `data-review-kind="ambiguous-arrival"`) {
		t.Fatalf("PRECONDITION: no ambiguous row:\n%s", body)
	}
	if strings.Contains(body, "<strong>ambiguous-arrival</strong>") {
		t.Errorf("F13: the ambiguous row still leads with the raw kind token")
	}
	if !strings.Contains(body, "<strong>Matches more than one trip</strong>") {
		t.Errorf("F13: the ambiguous row has no worded label")
	}
}

// TestFix3EDispatchRefusalsQuoteNoConfigTokensOrIds — F13 crafted-POST
// copy (routed from C: trips.go deadline / destination refusals). FAIL
// BEFORE: "deadline_minutes must be …", "destination \"ghost\" is not
// configured …", "[road_work] block", "[[destinations]]".
func TestFix3EDispatchRefusalsQuoteNoConfigTokensOrIds(t *testing.T) {
	w := newRoadWorld(t)
	cfg := w.srv.Cfg
	for _, c := range []struct {
		name                       string
		destID, destSite, deadline string
		wantCode                   int
		want                       string
	}{
		{"bad allowance", "site-b", "", "ninety", http.StatusBadRequest, "the travel allowance must be a whole number of minutes"},
		{"unknown destination", "ghost-pad", "", "", http.StatusConflict, "that destination is not configured at this site"},
		{"legacy unknown site", "", "ghost-site", "", http.StatusConflict, "that site is not a destination this post dispatches to"},
		{"no road work", config.RoadWorkChoice, "", "", http.StatusConflict, "this post does not record equipment working on the road"},
	} {
		var in domain.DispatchInput
		code, err := applyDispatchDestination(cfg, c.destID, c.destSite, c.deadline, &in)
		if err == nil || code != c.wantCode {
			t.Errorf("%s: got %d %v, want %d and a refusal", c.name, code, err, c.wantCode)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, c.want) {
			t.Errorf("%s: refusal %q does not say %q", c.name, msg, c.want)
		}
		for _, bad := range []string{"deadline_minutes", "ghost-pad", "ghost-site", "[road_work]", "[[destinations]]"} {
			if strings.Contains(msg, bad) {
				t.Errorf("%s: refusal %q quotes %q", c.name, msg, bad)
			}
		}
	}
}
