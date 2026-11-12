// gate_vehicle_test.go — F43, web half: the gate form must not hand the
// operator a submittable vehicle id they never chose, and a POST carrying
// one must be refused rather than redirected.
//
// The domain half (internal/domain/vehicle_placeholder_test.go) owns the
// rule. This file owns the two things only the web layer can get wrong:
// serving a default back into the markup, and swallowing the domain's
// refusal into a 303 that looks to the operator exactly like success.
package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// vehicleInputRe finds the served vehicle input, whatever its other
// attributes are or become.
var vehicleInputRe = regexp.MustCompile(`<input[^>]*name="vehicle"[^>]*>`)

// TestServedVehicleFieldCarriesNoSubmittableDefault is the regression lock on
// the original defect. It asserts the SHAPE of the served input, not the
// absence of one string: `value="walk-in"` was the bug, but `value="truck"`,
// `value="TRK-1"` or any other prefill reintroduces it exactly — a field the
// operator can leave untouched and still submit.
//
// A `placeholder` attribute is fine and is not a default: browsers never
// submit it. That distinction is the whole point, so the test checks the
// attribute rather than the substring.
func TestServedVehicleFieldCarriesNoSubmittableDefault(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	_, page := getGate(t, srv)
	page = styleRe.ReplaceAllString(page, "")

	input := vehicleInputRe.FindString(page)
	if input == "" {
		t.Fatalf("no vehicle input on the gate page:\n%s", page)
	}
	if strings.Contains(input, "value=") {
		t.Fatalf("the vehicle field is served with a default value — an operator who never touches it "+
			"still submits a vehicle id nobody chose, which is the F43 defect:\n\t%s", input)
	}
	// ⟦SchemaVersion 13, R-17, 2026-09-11⟧ `required` is DELIBERATELY GONE
	// from the inbound field — was: "if !strings.Contains(input, "required")
	// { t.Errorf(...) }", asserting the opposite. The inbound field now has
	// a legitimate way to be submitted empty: the "Vehicle unknown"
	// checkbox this page renders on BOTH directions (it used to render
	// outbound only), matching the posture the outbound field has ALWAYS
	// had — it has never carried `required` either, because
	// resolveVehicleIDBeforeMinting/dispatchTrip is the real guard, on
	// every authoring path including a raw POST, and an HTML5 attribute
	// would only fight the checkbox it now has a sibling for. This is
	// checked below, and TestGateCheckInRefusesAPlaceholderVehicleIDOverHTTP
	// (further down this file) still proves the SERVER refuses an empty
	// vehicle with the box unticked.
	if strings.Contains(input, "required") {
		t.Errorf("the vehicle field carries `required` — R-17 removes it deliberately so the box can be "+
			"submitted empty when \"Vehicle unknown\" is ticked; the server-side refusal is the real guard:\n\t%s", input)
	}
	// The page must not still be telling the operator that arriving on foot
	// is an option the form supports. It is not, and the domain refuses it.
	rendered := strings.ToLower(page)
	if strings.Contains(rendered, "walk-in") || strings.Contains(rendered, "walk in") {
		t.Errorf(`the rendered gate page still offers "walk-in" to the operator — ` +
			`product copy for a case that does not exist at this post and that the domain now refuses`)
	}
}

// TestGateCheckInRefusesAPlaceholderVehicleIDOverHTTP: the refusal has to
// SURVIVE the handler. gate.go passes the vehicle straight through and hands
// domain errors to refuse(); if that ever became a redirect-anyway, the
// operator would see the ordinary post-check-in gate page and believe the
// crew was on the muster.
func TestGateCheckInRefusesAPlaceholderVehicleIDOverHTTP(t *testing.T) {
	for _, entered := range []string{"walk-in", "WALK-IN", "n/a", "-", "x"} {
		t.Run(entered, func(t *testing.T) {
			srv, st, persons := newGateTestServer(t)

			rec := postForm(t, srv, "/gate/checkin", url.Values{
				"time_source":  {"default"},
				"vehicle":      {entered},
				"driver_kind":  {"existing"},
				"driver_value": {persons[0].PersonID},
			})
			if rec.Code == http.StatusSeeOther {
				t.Fatalf("POST /gate/checkin with vehicle=%q redirected like a success", entered)
			}
			if rec.Code != http.StatusConflict {
				t.Fatalf("POST /gate/checkin with vehicle=%q = %d, want 409: %s", entered, rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "does not identify a vehicle") {
				t.Errorf("the operator is not told why: %s", rec.Body.String())
			}

			open, err := domain.OpenVisitsAt(st, "site-a")
			if err != nil {
				t.Fatal(err)
			}
			if len(open) != 0 {
				t.Fatalf("a refused check-in put %d person(s) on the muster", len(open))
			}
		})
	}
}

// TestJSOffGateCheckInStillWorksWithARealVehicleID — the positive control for
// the web half. The scripting-off path is the one that decides who is on the
// muster when the browser fails, so "we hardened the vehicle field" must not
// have meant "we broke the form".
func TestJSOffGateCheckInStillWorksWithARealVehicleID(t *testing.T) {
	srv, st, _ := newGateTestServer(t)

	rec := nojsCheckIn(t, srv, "TRK-14", "Dana Driver", "Pat Passenger", "", "", "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("JS-off check-in with a real vehicle id = %d: %s", rec.Code, rec.Body.String())
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 {
		t.Fatalf("driver + one occupant produced %d visits, want 2", len(open))
	}
	_, page := getGate(t, srv)
	if !strings.Contains(page, "TRK-14") && !strings.Contains(page, "Dana Driver") {
		t.Errorf("the check-in does not appear on the gate page:\n%s", page)
	}
}
