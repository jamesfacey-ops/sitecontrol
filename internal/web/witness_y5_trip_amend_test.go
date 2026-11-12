// witness_y5_trip_amend_test.go — witness/y5-tripweb (row-Y census).
//
// This file renders officer-facing behaviour on `internal/web/trip_amend.go`
// that the pre-existing suite never executes at all. Each test's doc comment
// names the plant (from `row-y-evidence/plants.py`) it was built to kill, or
// the census row (`row-y-evidence/blocks19_unc.tsv`) it reaches for the
// unassigned ones this worktree was told to work outward into.
//
// See `C:\Users\james\SiteControl\reports\witness\_BRIEF.md` for the phase
// rules: tests only, no production edits, revert with `git checkout -- .`.
package web

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// =========================================================================
// Q12c — POST /arrivals/amend, a LIVE route no test enters
// =========================================================================

// TestWitnessY5Q12cArrivalAmendRouteIsReachable kills plant Q12c
// (trip_amend.go:513, `handleArrivalAmend`: an unconditional 500 inserted at
// the top of the handler).
//
// MEASURED before writing this test: `grep -rn "arrivals/amend"
// internal/web/*_test.go` finds the path only in a route-classification
// table (w2_route_walk_test.go, which never POSTs) and in a path constant
// (w9a_web_seams_test.go's `w9aArrivalAmendPath`, never used to POST). No
// test in the tree issues `POST /arrivals/amend`. This is that first test.
//
// It renders the ORDINARY correction path the doc comment at the top of
// trip_amend.go describes (§5 rule 4): an officer fixes what they mis-typed
// on an arrival's claimed vehicle, record-only. Officer-facing behaviour
// asserted: the redirect the officer follows, and that the corrected claim
// is what `EffectiveArrival` — the read path every surface uses — now
// reports.
func TestWitnessY5Q12cArrivalAmendRouteIsReachable(t *testing.T) {
	st := w3OpenRegistered(t, "witness-y5-q12c.db")
	srv := New(w3ConfigSiteB(st.Path), st)

	w9aSeedPerson(t, st, "site-a", 1, "drv-q12c", "Driver Q12c")
	trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRUCK 41", "drv-q12c", nil, 240)
	arr, err := domain.RecordExpectedArrival(st, "site-b", 1, trip.TripID, w9aStr("Pad Site Control"), 0, "default")
	if err != nil {
		t.Fatalf("witness fixture: recording the arrival: %v", err)
	}

	form := url.Values{
		"arrival_id":      {arr.ArrivalID},
		"vehicle_entered": {"TRUCK 41-CORRECTED"},
	}
	rec, cookie := w2PostForm(t, srv, w9aArrivalAmendPath, form)
	if rec.Code != 303 {
		t.Fatalf("Q12c: POST %s (an ordinary correction) = %d, want 303. Body: %s", w9aArrivalAmendPath, rec.Code, rec.Body.String())
	}
	loc := rec.Result().Header.Get("Location")
	if loc == "" {
		t.Fatalf("Q12c: POST %s returned 303 with no Location", w9aArrivalAmendPath)
	}
	next := w2Get(t, srv, loc, cookie)
	if next.Code != 200 {
		t.Fatalf("Q12c: GET %s (the confirmation) = %d: %s", loc, next.Code, next.Body.String())
	}

	eff, err := domain.EffectiveArrival(st, arr.ArrivalID)
	if err != nil || eff == nil {
		t.Fatalf("Q12c: EffectiveArrival(%s) = %v, %v, want a composed arrival", arr.ArrivalID, eff, err)
	}
	if eff.Arrival.VehicleEntered == nil || *eff.Arrival.VehicleEntered != "TRUCK 41-CORRECTED" {
		t.Fatalf("Q12c: the corrected vehicle did not land — EffectiveArrival reads %v, want a pointer to %q",
			eff.Arrival.VehicleEntered, "TRUCK 41-CORRECTED")
	}
	if !eff.Corrected {
		t.Fatalf("Q12c: EffectiveArrival(%s).Corrected is false after a successful amendment", arr.ArrivalID)
	}
}

// =========================================================================
// Q19 — the deadline_minutes parse guard, gutted
// =========================================================================

// TestWitnessY5Q19NonNumericDeadlineMinutesGetsItsOwnSentence kills plant Q19
// (trip_amend.go:353, `if err != nil {` -> `if false && err != nil {` inside
// handleTripAmend's destination-amendment block).
//
// Officer-facing distinction this guard exists for: a NON-NUMERIC travel
// allowance is an INPUT mistake caught here, at the web edge, with its own
// precise sentence ("must be a whole number of minutes") and the officer's
// typing preserved.
//
// ⚠ MEASURED, not guessed: under the plant the guard never fires, `m` is
// strconv.ParseInt's zero value (0), and — because this test's fixture trip
// is dispatched and still OPEN — the amendment reaches §4's Class-B gate
// (`classBRefusal`), which fires the UNRELATED "this trip is open and
// counting down… cancel it and re-dispatch" sentence. An initial hypothesis
// for this test guessed the domain layer's `validateDestinationAmend`
// refusal ("must carry the travel allowance in minutes") would show instead
// — that is wrong: this trip never reaches that far, because the Class-B
// gate refuses it first. The actually-produced sentence, quoted below, is a
// worse defect than either guess: an officer who mistyped a number is told
// to CANCEL AND RE-DISPATCH the trip, which is not what they asked to do and
// is the exact kind of instruction the file's own header warns against
// giving carelessly.
func TestWitnessY5Q19NonNumericDeadlineMinutesGetsItsOwnSentence(t *testing.T) {
	st := w3OpenRegistered(t, "witness-y5-q19.db")
	srv := New(w3ConfigSiteA(st.Path), st)

	w9aSeedPerson(t, st, "site-a", 1, "drv-q19", "Driver Q19")
	trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRUCK 19", "drv-q19", nil, 240)

	form := url.Values{
		"trip_id":           {trip.TripID},
		"destination_kind":  {"controlled"},
		"destination_value": {"site-b"},
		"deadline_minutes":  {"not-a-number"},
	}
	rec, _ := w2PostForm(t, srv, w9aTripAmendPath, form)
	if rec.Code != 409 {
		t.Fatalf("Q19: POST %s with a non-numeric deadline_minutes = %d, want 409. Body: %s", w9aTripAmendPath, rec.Code, rec.Body.String())
	}
	const want = "the travel allowance must be a whole number of minutes"
	body := rec.Body.String()
	if !strings.Contains(body, want) {
		t.Fatalf("Q19: a non-numeric travel allowance did not produce its own input-error sentence.\nwant substring: %q\ngot body:\n%s", want, body)
	}
	// ⟦D-78, 2026-09-05⟧ RE-POINTED. ⟦was: const wrongAndMisleading = "Cancel it and re-dispatch"⟧
	// D-78 replaced the Class-B gate's instruction, so a negative on the old
	// sentence would pass on every tree forever — the trivially-green shape
	// this file's own u1b-web §3.3 note warned about (that one was dead by
	// CASE; this one would have been dead by ABSENCE). The needle is now the
	// production constant the gate renders, classBWaitSentence, so it tracks
	// the live copy and can die by neither. What it guards is unchanged: a
	// numeric typo must be answered by naming the typo, never by the Class-B
	// sentence for the trip's state.
	if strings.Contains(body, classBWaitSentence) {
		t.Fatalf("Q19: the page answers a mistyped travel allowance with the Class-B state sentence (%q) — the guard-skipped failure mode measured for this plant: a numeric typo reaches §4's Class-B gate on this OPEN trip "+
			"and is answered with the trip's state instead of the typo being named.\nbody:\n%s", classBWaitSentence, body)
	}
}

// =========================================================================
// Working outward — additional unreached blocks in this file (blocks19_unc.tsv)
// =========================================================================

// TestWitnessY5BuildTripAmendOutcomeUnparsableKeyIsFailed reaches
// trip_amend.go:186-188 (`if !ok { return &tripAmendOutcomeView{Failed: true} }`)
// and :194 (the `len(outs)==0` STMT arm) — both ARM-NOT-TAKEN /
// FUNC-adjacent blocks the census marks unreached in `buildTripAmendOutcome`.
//
// Officer-facing behaviour: a `?recorded=` key this post cannot parse, and a
// syntactically valid key naming an amendment this post has no record of,
// must both render the SAME safe "Failed" confirmation rather than panic or
// leak an internal shape — VF §5 rendering 4.
func TestWitnessY5BuildTripAmendOutcomeUnparsableKeyIsFailed(t *testing.T) {
	st := w3OpenRegistered(t, "witness-y5-outcome-key.db")
	srv := New(w3ConfigSiteA(st.Path), st)

	if out := srv.buildTripAmendOutcome("not-a-key-at-all"); out == nil || !out.Failed {
		t.Fatalf("Q-outward: buildTripAmendOutcome(%q) = %+v, want Failed=true (parseRecordedKey must reject it)", "not-a-key-at-all", out)
	}
	if out := srv.buildTripAmendOutcome("site-a.1.99999"); out == nil || !out.Failed {
		t.Fatalf("Q-outward: buildTripAmendOutcome(%q) = %+v, want Failed=true (a well-formed key naming no recorded amendment)", "site-a.1.99999", out)
	}
}

// TestWitnessY5TripFactLabelNamesCargoNote reaches the `fieldCargoNote`
// CASE arm of `tripFactLabel` — unreached because every existing
// amendment-chain test exercises a different fact. ⟦was:
// "TestWitnessY5TripFactLabelNamesCargoNoteAndOverrideReason", which also
// pinned the `fieldOverrideReason` arm ("phase override reason") — that fact
// and its arm went with weather phases, James 2026-10-04⟧
//
// Officer-facing behaviour: the audit view and the outcome confirmation both
// call this to name a corrected fact; a fact whose label falls through to
// the raw wire key (the trailing `return fact`) would print "cargo_note" at
// an officer rather than "cargo note".
func TestWitnessY5TripFactLabelNamesCargoNote(t *testing.T) {
	st := w3OpenRegistered(t, "witness-y5-factlabel.db")
	srv := New(w3ConfigSiteA(st.Path), st)

	if got := srv.tripFactLabel(fieldCargoNote); got != "cargo note" {
		t.Fatalf("Q-outward: tripFactLabel(%q) = %q, want %q", fieldCargoNote, got, "cargo note")
	}
}

// TestWitnessY5RenderTripAmendErrorNeverLeaksTheRawFault reaches
// trip_amend.go:409-419 (`renderTripAmendError`'s InputError STMT/IF arm and
// the internal-fault STMT/log arm) — the file's own header invariant: "NO
// RAW `err.Error()` REACHES THIS PAGE".
//
// This is the strongest form of that invariant this suite can state: an
// error value carrying a literal, distinctive string is fed to the internal-
// fault arm, and the rendered page is asserted NOT to contain it, only the
// authored generic sentence. The input-error arm is asserted the opposite
// way — its Reason IS officer-authored copy and must appear verbatim.
func TestWitnessY5RenderTripAmendErrorNeverLeaksTheRawFault(t *testing.T) {
	st := w3OpenRegistered(t, "witness-y5-rendererr.db")
	srv := New(w3ConfigSiteA(st.Path), st)

	// --- the INPUT-ERROR arm: authored copy, rendered verbatim ----------
	w := httptest.NewRecorder()
	srv.renderTripAmendError(w, tripAmendPage{}, &domain.TripAmendInputError{Reason: "Q-OUTWARD-INPUT-MARKER"})
	if w.Code != 409 {
		t.Fatalf("Q-outward: renderTripAmendError(TripAmendInputError) = %d, want 409", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Q-OUTWARD-INPUT-MARKER") {
		t.Fatalf("Q-outward: the input-error page does not carry the officer-authored reason.\nbody:\n%s", w.Body.String())
	}

	// --- the INTERNAL-FAULT arm: the raw string must NOT appear ----------
	w2 := httptest.NewRecorder()
	const secret = "Q-OUTWARD-RAW-FAULT-STRING-6f2a"
	srv.renderTripAmendError(w2, tripAmendPage{}, errStr(secret))
	if w2.Code != 500 {
		t.Fatalf("Q-outward: renderTripAmendError(plain error) = %d, want 500", w2.Code)
	}
	body := w2.Body.String()
	if strings.Contains(body, secret) {
		t.Fatalf("Q-outward (file header invariant broken): the raw fault string %q reached the page.\nbody:\n%s", secret, body)
	}
	const wantGeneric = "this post could not complete the correction"
	if !strings.Contains(body, wantGeneric) {
		t.Fatalf("Q-outward: the internal-fault page does not carry the authored generic sentence %q.\nbody:\n%s", wantGeneric, body)
	}
}

// errStr is a trivial error whose message is the exact string given — used
// only to prove that string never reaches an officer-facing page.
type errStr string

func (e errStr) Error() string { return string(e) }

// TestWitnessY5ClassBRefusalUnknownOrNonOpenTripDoesNotGuess reaches
// trip_amend.go:436-438 (`trip == nil` after `TripByID`) and :443-445
// (`state == nil || state.State != TripStateOpen`) in `classBRefusal`.
//
// classBRefusal's own doc comment says it "returns (\"\", false) for
// anything it cannot decide here — the domain edge refuses again as the
// backstop, in its own words." These two arms are exactly that: a trip id
// this post has never heard of, and a trip that exists but is not OPEN
// (already arrived). Officer-facing behaviour: NEITHER produces the §4
// countdown-gate sentence — that sentence is only correct for a live, open
// trip, and asserting it here would be the web layer guessing.
func TestWitnessY5ClassBRefusalUnknownOrNonOpenTripDoesNotGuess(t *testing.T) {
	st := w3OpenRegistered(t, "witness-y5-classb.db")
	srv := New(w3ConfigSiteA(st.Path), st)

	// --- unknown trip: TripByID returns (nil, nil) ----------------------
	vehicle := "TRUCK 1"
	in := domain.TripAmendment{TripID: "no-such-trip", VehicleEntered: &vehicle}
	if reason, refused := srv.classBRefusal(in); refused {
		t.Fatalf("Q-outward: classBRefusal on an unknown trip id refused with %q, want (\"\", false) — the domain edge is the backstop for a trip this post has never heard of", reason)
	}

	// --- a trip that exists but has already ARRIVED (not OPEN) ----------
	w9aSeedPerson(t, st, "site-a", 1, "drv-classb", "Driver ClassB")
	trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRUCK 2", "drv-classb", nil, 240)
	if _, err := domain.RecordExpectedArrival(st, "site-a", 1, trip.TripID, w9aStr("Ice Road"), 0, "default"); err != nil {
		t.Fatalf("witness fixture: closing the trip: %v", err)
	}
	state, err := domain.TripStateByID(st, trip.TripID)
	if err != nil || state == nil || state.State != domain.TripStateArrived {
		t.Fatalf("PRECONDITION: trip %s state = %+v, %v, want %q", trip.TripID, state, err, domain.TripStateArrived)
	}
	in2 := domain.TripAmendment{TripID: trip.TripID, VehicleEntered: &vehicle}
	if reason, refused := srv.classBRefusal(in2); refused {
		t.Fatalf("Q-outward: classBRefusal on a trip that has already ARRIVED refused with %q, want (\"\", false) — the countdown-gate sentence is only correct for an OPEN trip", reason)
	}
}
