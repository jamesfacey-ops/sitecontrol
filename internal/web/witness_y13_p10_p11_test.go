// witness_y13_p10_p11_test.go — witness phase (see reports/witness/_BRIEF.md),
// worktree y13-copy. Kills census survivors P10 and P11: the SAME `if vehErr
// != "" {` shape at the two officer-facing gate doors — check-in
// (handleGateCheckIn, the busiest form in the application) and check-out
// (handleGateVehicleCheckOut) — that keep the co-mint's OWN refusal wording
// instead of folding it into the generic conflict message.
//
// ⚠ LINE DRIFT FROM THE CENSUS ANCHOR. plants.py (row-y-evidence) anchored
// these against app tip bbff17e at gate.go:1135 (P11) and gate.go:1346
// (P10). This worktree sits on integration/laptop-2026-09-04 (47cd71e),
// where BOTH sites have drifted down by exactly 26 lines — P11 is now at
// gate.go:1161 (inside handleGateCheckIn) and P10 is now at gate.go:1372
// (inside handleGateVehicleCheckOut). Re-anchored by `grep -n 'if vehErr !=
// "" {'`, which found a THIRD occurrence at gate.go:1530, inside
// legacyGateCheckIn — a site that did not exist in the census (see the
// report's findings section; it is not one of this worktree's plants and is
// not witnessed here).
//
// ⭐ P11 is the highest-stakes plant on this worktree's list: officer types a
// placeholder like "n/a" into the vehicle box on the passenger-list check-in
// form, and the SAME `if vehErr != ""` this file tests decides whether the
// officer reads the co-mint's own clean sentence or a wrapped
// "domain: dispatch: vehicle: ..." string with Go error-wrapping scaffolding
// leaked into it. P10 is the same shape at check-out, and there the
// difference is starker: the fallback path does not even mention the
// vehicle — it returns the position-independent "vehicle id is required",
// which reads as an EMPTY box rather than an invalid one.
package web

import (
	"net/http"
	"strings"
	"testing"
)

// vehiclePlaceholderRefusalText is vehicleIDRefusal's literal
// (internal/domain/trip.go), duplicated here byte-for-byte because it is
// unexported and this is exactly the sentence the officer must see
// UNWRAPPED — the whole point of both witnesses below.
func vehiclePlaceholderRefusalText(entered string) string {
	return `"` + entered + `" does not identify a vehicle. Enter the plate or unit number of the vehicle these people arrived in — ` +
		"it is what links them to that vehicle on the muster, and an evacuation reads it. " +
		"(Everyone arrives in a vehicle at this post; there is no placeholder for 'no vehicle'.)"
}

// TestWitnessP11GateCheckInVehicleRefusalKeepsItsOwnWords is P11's witness.
// gate.go's own comment at the mutated line: "a co-mint refusal keeps ITS
// OWN words and status. Folding it into the generic 409 would tell the
// officer the check-in was refused rather than that the VEHICLE NUMBER was
// — the wrong reason, on the busiest form in the application." This asserts
// the rendered notice text is EXACTLY that sentence, not a superset of it —
// the pre-existing TestGateCheckInRefusesAPlaceholderVehicleIDOverHTTP only
// asserts Contains("does not identify a vehicle"), which a
// "domain: dispatch: vehicle: <same text>" wrap still satisfies, which is
// why it did not catch this plant.
func TestWitnessP11GateCheckInVehicleRefusalKeepsItsOwnWords(t *testing.T) {
	srv, _, _ := newGateTestServer(t)

	_, vals := gateBuild(t, srv, gateInbound, "n/a", []string{"Dana Driver"}, 0)
	rec := postForm(t, srv, "/gate/checkin", vals)
	if rec.Code != http.StatusConflict {
		t.Fatalf("check-in with a placeholder vehicle = %d, want 409: %s", rec.Code, rec.Body.String())
	}

	// vehicleError's notice includes the form's own "Nothing was recorded."
	// lead-in (gate.html's <strong>) ahead of {{.}}.
	msg := vehicleError(t, rec.Body.String(), gateInbound)
	want := vehiclePlaceholderRefusalText("n/a")

	// u1b-web §3.2 CONFIRMED WEAK: Contains is *satisfied* by a wrap, because
	// the wrapped text embeds the whole sentence as a substring — produced:
	// the P11 plant plus the one-line "repair" `f.Error =
	// strings.TrimPrefix(err.Error(), "domain: ")` at gate.go:1166 turns this
	// test green while the officer reads `dispatch: vehicle: "n/a" does not
	// identify a vehicle…` — Go error-wrapping scaffolding, not the co-mint's
	// own words. This doc comment already promised "EXACTLY that sentence,
	// not a superset of it"; strip the lead-in and hold the code to it with
	// EQUALITY, which the wrap (any prefix at all) cannot satisfy.
	const leadIn = "Nothing was recorded."
	i := strings.Index(msg, leadIn)
	if i < 0 {
		t.Fatalf("check-in vehicle refusal notice does not carry its own %q lead-in at all: %q", leadIn, msg)
	}
	notice := strings.TrimSpace(msg[i+len(leadIn):])
	if notice != want {
		t.Errorf("check-in vehicle refusal text =\n\t%q\nwant EXACTLY the co-mint's own unwrapped sentence (no wrap, no superset):\n\t%q", notice, want)
	}
	// Belt, not the load-bearing check: a wrap this specific ("domain: ...")
	// would also be caught above, but a wrap using different wording would
	// not trip this alone — which is exactly why it is belt, not the fix.
	if strings.Contains(msg, "domain:") {
		t.Errorf("check-in vehicle refusal leaks Go error-wrapping scaffolding into the officer's screen: %q", msg)
	}
}

// TestWitnessP10GateCheckOutVehicleRefusalKeepsItsOwnWords is P10's witness,
// the same shape at the SECOND site — handleGateVehicleCheckOut. Here the
// fallen-through message is not merely wrapped, it is a DIFFERENT and
// misleading refusal: dispatchTrip's "vehicle id is required" is the
// EMPTY-BOX message, and typing "n/a" is not an empty box.
func TestWitnessP10GateCheckOutVehicleRefusalKeepsItsOwnWords(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	visitIDs := checkInCrew(t, srv, st, 1) // "Crew 00" is on site to depart

	// ⟦R-28⟧ RETARGETED: reached via the roster's own tick-and-select door.
	_, vals := gateBuildOutbound(t, srv, "n/a", visitIDs, nil, 0)
	vals.Set("destination_id", "conneco")
	rec := postForm(t, srv, "/gate/checkout/vehicle", vals)
	if rec.Code != http.StatusConflict {
		t.Fatalf("check-out with a placeholder vehicle = %d, want 409: %s", rec.Code, rec.Body.String())
	}

	msg := vehicleError(t, rec.Body.String(), gateOutbound)
	want := vehiclePlaceholderRefusalText("n/a")
	if !strings.Contains(msg, want) {
		t.Errorf("check-out vehicle refusal text =\n\t%q\ndoes not contain the co-mint's OWN unwrapped words:\n\t%q", msg, want)
	}
	if strings.Contains(msg, "vehicle id is required") {
		t.Errorf("check-out vehicle refusal reads as an EMPTY box (%q) when the officer typed %q — "+
			"a materially misleading reason, not merely a wrapped one", msg, "n/a")
	}
}
