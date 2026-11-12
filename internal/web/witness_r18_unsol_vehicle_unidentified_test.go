// witness_r18_unsol_vehicle_unidentified_test.go — SchemaVersion 14 (R-18,
// James: "Fix it — same bump."): the unsolicited-arrival door's own share
// of R-17 ("there are no doors only vehicles"). The domain-level proofs
// (the contradiction refusal, the CHECK constraint, replication) live in
// internal/domain/witness_r18_unsol_vehicle_unidentified_test.go; this file
// proves the HTTP surface — the form, the checkbox wiring, F43's continued
// refusal at this door, and D-4 — the way witness_u8_post19_test.go already
// proves F43 here for the pre-R-18 shape.
package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// TestR18UnsolicitedArrivalCheckboxRecordsWithNoVehicleAndOriginSurvives is
// the HTTP-level proof of the domain package's
// TestR18UnsolicitedArrivalAuthorsCleanlyWithNoVehicleAndOriginSurvives:
// ticking the form's "Vehicle unknown" box with NOTHING typed in the
// vehicle box succeeds, checks both occupants in, and origin_site survives
// — the fact the gate-check-in workaround (R-18's own text) would drop.
func TestR18UnsolicitedArrivalCheckboxRecordsWithNoVehicleAndOriginSurvives(t *testing.T) {
	srv, st, persons := newTripsTestServer(t)

	rec := postForm(t, srv, "/trips/arrive-unsolicited", url.Values{
		"time_source":          {"default"},
		"vehicle_unidentified": {"1"},
		"origin_site":          {"site-a"},
		"person_ids":           {persons[0].PersonID, persons[1].PersonID},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unsolicited arrival with vehicle_unidentified ticked = %d: %s", rec.Code, rec.Body.String())
	}

	arrivals, err := domain.Arrivals(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(arrivals) != 1 {
		t.Fatalf("domain.Arrivals() = %d rows, want 1", len(arrivals))
	}
	arr := arrivals[0]
	if !arr.VehicleIsUnidentified() {
		t.Error("the authored arrival does not report VehicleIsUnidentified()")
	}
	if arr.VehicleID != nil {
		t.Errorf("arr.VehicleID = %q, want nil", *arr.VehicleID)
	}
	if arr.OriginSite == nil || *arr.OriginSite != "site-a" {
		t.Fatalf("arr.OriginSite = %v, want \"site-a\" — this is the fact the gate-check-in workaround drops (R-18)", arr.OriginSite)
	}

	open, err := domain.OpenVisitsAt(st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 {
		t.Fatalf("open visits at site-b = %d, want 2 — both occupants must still be checked in", len(open))
	}

	// Nothing minted: the vehicle-merge screen's one non-test caller,
	// domain.Vehicles(), stays flat (R-17's own accumulation criterion,
	// reused here since this door goes through the identical co-mint
	// machinery when NOT ticked).
	vehicles, err := domain.Vehicles(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(vehicles) != 0 {
		t.Fatalf("domain.Vehicles() = %d, want 0 — nothing should ever be minted for an unidentified arrival", len(vehicles))
	}
}

// TestR18UnsolicitedArrivalFormOffersTheCheckboxOnBothTheInputAndTheControl
// proves the rendered form actually carries the escape (not just that the
// domain accepts it if a raw POST is crafted): the "Vehicle unknown"
// checkbox is present, and the vehicle text input is no longer `required`
// — the same "required was never the real guard" posture the gate and
// console forms document.
func TestR18UnsolicitedArrivalFormOffersTheCheckboxOnBothTheInputAndTheControl(t *testing.T) {
	srv, _, _ := newTripsTestServer(t)
	body := getTrips(t, srv)
	if !strings.Contains(body, `name="vehicle_unidentified"`) {
		t.Fatalf("the unsolicited-arrival form carries no vehicle_unidentified checkbox:\n%s", body)
	}
	if strings.Contains(body, `name="vehicle" required`) {
		t.Fatalf("the vehicle text input is still `required` — this fights the checkbox exactly the way R-17's own gate/console fix avoided:\n%s", body)
	}
}

// TestR18UnsolicitedArrivalFormStillRefusesAPlaceholderVehicleNumber is
// F43's own proof at THIS door, post-R-18: with the checkbox left UNTICKED,
// typing a placeholder ("unknown") is refused exactly as
// witness_u8_post19_test.go already proves for "walk-in" — this test adds
// the second member of the ~40-entry list so the R-18 change is shown not
// to have widened F43's exemption by accident.
func TestR18UnsolicitedArrivalFormStillRefusesAPlaceholderVehicleNumber(t *testing.T) {
	srv, _, persons := newTripsTestServer(t)

	rec := postForm(t, srv, "/trips/arrive-unsolicited", url.Values{
		"time_source": {"default"},
		"vehicle":     {"unknown"},
		"origin_site": {"site-a"},
		"person_ids":  {persons[0].PersonID, persons[1].PersonID},
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("typing the placeholder \"unknown\" (box unticked) = %d, want %d (F43's VehicleCreateRefusal): body=%s",
			rec.Code, http.StatusConflict, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "does not identify a vehicle") {
		t.Fatalf("refusal body = %q, want F43's own sentence", rec.Body.String())
	}
}

// TestR18UnsolicitedArrivalNeverProducesAResumeAffordanceOrMayBeIncomplete
// is D-4, asserted directly over HTTP rather than assumed to hold merely
// because this door authors no `trip` row: after an unidentified-vehicle
// arrival, the gate page's `{{if .Incomplete}}`-guarded section (the
// "Incomplete check-ins — needs attention" heading and its Resume forms)
// does not render at all, and the muster's OWN `{{if .Incomplete}}`-guarded
// banner (muster.html:225, "MAY BE INCOMPLETE") does not either.
//
// ⟦GATE-STRIP-PROSE, 2026-09-13⟧ ⟦was: a note that "MAY BE INCOMPLETE" also
// rendered VERBATIM, unconditionally, in gate.html's own destination-field
// help copy⟧ — that help copy was removed with the rest of the page's
// descriptive prose, so the phrase is no longer in gate.html's static
// markup at all; the banner text is checked on /muster below, where it is
// guarded, and nowhere else.
func TestR18UnsolicitedArrivalNeverProducesAResumeAffordanceOrMayBeIncomplete(t *testing.T) {
	srv, _, persons := newTripsTestServer(t)

	rec := postForm(t, srv, "/trips/arrive-unsolicited", url.Values{
		"time_source":          {"default"},
		"vehicle_unidentified": {"1"},
		"origin_site":          {"site-a"},
		"person_ids":           {persons[0].PersonID, persons[1].PersonID},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unsolicited arrival = %d: %s", rec.Code, rec.Body.String())
	}

	// getGate/getMuster (gate_test.go / muster_test.go) — neither GET route
	// is behind requireLogin, so no session cookie is needed here.
	gateCode, gateBody := getGate(t, srv)
	if gateCode != http.StatusOK {
		t.Fatalf("GET /gate = %d: %s", gateCode, gateBody)
	}
	if strings.Contains(gateBody, "Incomplete check-ins — needs attention") {
		t.Fatalf("D-4: the gate page renders the incomplete-check-ins section after an unidentified-vehicle unsolicited arrival:\n%s", gateBody)
	}
	if strings.Contains(gateBody, `action="/gate/resume"`) {
		t.Fatalf("D-4: the gate page offers a Resume form after an unidentified-vehicle unsolicited arrival:\n%s", gateBody)
	}

	musterCode, musterBody := getMuster(t, srv, "/muster")
	if musterCode != http.StatusOK {
		t.Fatalf("GET /muster = %d: %s", musterCode, musterBody)
	}
	if strings.Contains(musterBody, "cat-tag-warn\">MAY BE INCOMPLETE") {
		t.Fatalf("D-4: the muster's guarded MAY BE INCOMPLETE banner rendered after an unidentified-vehicle unsolicited arrival:\n%s", musterBody)
	}
}

// TestR18ArrivalClaimRendersVehicleUnknownNeverABlankCell asserts DIRECTLY
// the rule R-17 established for `trip` ("never a blank cell, always say
// Vehicle unknown") over arrivalClaim, the one place this door's vehicle
// claim renders as text. Structurally unreachable through the review lane
// today (correlation treats a keyless arrival as an uncorrelatable plain
// visit — see arrivalClaim's own comment), which is exactly why this is
// asserted directly against the function rather than left to be exercised
// incidentally by some other test.
func TestR18ArrivalClaimRendersVehicleUnknownNeverABlankCell(t *testing.T) {
	srv, st, persons := newTripsTestServer(t)

	arr, err := domain.RecordUnsolicitedArrivalOriented(st, srv.Cfg.SiteID, srv.Cfg.NodeEpoch, "", "site-a",
		[]string{persons[0].PersonID}, nil, 0, "default", false, true)
	if err != nil {
		t.Fatalf("RecordUnsolicitedArrivalOriented: %v", err)
	}

	claim, err := srv.arrivalClaim(arr.ArrivalID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(claim, domain.VehicleUnidentifiedText) {
		t.Fatalf("arrivalClaim(%s) = %q, want it to contain %q — never a blank cell", arr.ArrivalID, claim, domain.VehicleUnidentifiedText)
	}
	if !strings.Contains(claim, "from site-a") {
		t.Fatalf("arrivalClaim(%s) = %q, want it to still name the origin", arr.ArrivalID, claim)
	}
}
