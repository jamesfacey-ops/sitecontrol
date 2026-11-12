// witness_u5_legacy_test.go — witness phase (see reports/witness/_BRIEF.md),
// worktree u5-legacy. Kills a plant (L1) on gate.go's THIRD `if vehErr != ""
// {` site — the one the row-Y census never saw, because it postdates the
// census (the site's own comment is dated 2026-09-03, the day W7-4(d)(i)
// landed). This is legacyGateCheckIn, the JS-off / stale-tab check-in door
// — not either of the census's two live-form plants, P10
// (handleGateVehicleCheckOut) and P11 (handleGateCheckIn), which live in
// witness_y13_p10_p11_test.go and were hardened today by u1b-web/y13-copy.
//
// ⚠ WHY THE ASSERTION IS WHOLE-BODY HERE, UNLIKE P11'S SCOPED ONE. P10 and
// P11 both refuse through s.renderGate: the co-mint's sentence becomes
// f.Error, folded into gate.html's `<div class="notice notice-critical
// vehicle-error">` alongside the form's own "Nothing was recorded." lead-in,
// inside a whole re-rendered page — which is why P11's witness has to
// scrape the notice div out of a full HTML document (and why a bare
// Contains over the WHOLE page there is weak: the departing form's help
// text and the gate script's disabled-commit reason both carry unrelated
// prose that a naive whole-body scan could satisfy).
//
// legacyGateCheckIn's vehErr branch never touches that template. It is a
// bare `http.Error(w, vehErr, vehStatus)` — a stale-tab body with no
// scripting to redraw a form into, and no notice div, no lead-in, no page
// furniture at all. z3 measured this path specifically: it renders through
// http.Error(err.Error()), so the ENTIRE response body IS the notice, with
// nothing else present that could accidentally satisfy a loose assertion.
// Scoping this the way P11 is scoped would mean extracting a "notice" out
// of a page that has no other content to distinguish it from — harmless
// today, but it would silently stop testing anything the moment this door
// grew a second line of output. Whole-body equality is the correct choice
// for this site, not merely the convenient one.
package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestWitnessU5LegacyGateCheckInVehicleRefusalKeepsItsOwnWords kills the L1
// plant: gate.go:1530's `if vehErr != "" {` inside legacyGateCheckIn. Same
// shape and the same stakes as P11 — an officer whose tab went stale before
// a bump types a placeholder like "n/a" into the vehicle box and submits
// the pre-bump body; this branch decides whether they read the co-mint's
// own clean sentence or the generic refuse() fallback's (trips.go)
// "domain: dispatch: vehicle: <same sentence>" with Go error-wrapping
// scaffolding leaked into it — dispatchTrip (tripauthor.go) wraps
// legacyGateCheckIn's ResolveVehicle failure exactly the way it wraps
// handleGateCheckIn's (both ride through the same dispatchTrip via
// DispatchLocalCheckIn), so this site's mutant produces P11's wrapped
// shape, not P10's different "vehicle id is required" empty-box message.
func TestWitnessU5LegacyGateCheckInVehicleRefusalKeepsItsOwnWords(t *testing.T) {
	srv, _, persons := newGateTestServer(t)

	// The LEGACY shape: driver_kind/driver_value with no passenger_list=1,
	// so isPassengerListForm(r) is false and handleGateCheckIn routes this
	// straight to legacyGateCheckIn (gate.go:1102).
	rec := postForm(t, srv, "/gate/checkin", url.Values{
		"time_source":  {"default"},
		"vehicle":      {"n/a"},
		"driver_kind":  {"existing"},
		"driver_value": {persons[0].PersonID},
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("legacy check-in with a placeholder vehicle = %d, want 409: %s", rec.Code, rec.Body.String())
	}

	// http.Error's whole contract: Content-Type text/plain, body = the
	// message plus one trailing newline (net/http's fmt.Fprintln) — nothing
	// else is ever written on this path, so trimming that newline is the
	// entire job of extracting "the notice" here.
	msg := strings.TrimRight(rec.Body.String(), "\n")
	want := vehiclePlaceholderRefusalText("n/a")
	if msg != want {
		t.Errorf("legacy check-in vehicle refusal body =\n\t%q\nwant EXACTLY the co-mint's own unwrapped sentence (no wrap, no superset, nothing else in the response):\n\t%q", msg, want)
	}
	// Belt, not the load-bearing check (the equality above already fails on
	// this): names the failure the way an officer would describe it,
	// matching P10/P11's convention.
	if strings.Contains(msg, "domain:") {
		t.Errorf("legacy check-in vehicle refusal leaks Go error-wrapping scaffolding into the officer's screen: %q", msg)
	}
}
