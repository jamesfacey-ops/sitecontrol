// y11_unref_witness_test.go — witness phase, row Y, worktree y11-unref
// (reports/witness/_BRIEF.md). Kills census survivors Q10 and Q11 in
// internal/domain/arrival_unref.go.
//
// [MEASURED] Before this file, `grep -rn "RetractArrivalRef\|UnrefRefusal"
// internal/domain/*_test.go` returns zero hits: every existing W9b test
// constructs its `arrival_ref_retracted` rows as HAND-BUILT WIRE ENVELOPES
// (w9b_seams_test.go's `w9bRetraction`/`w9bApply`) and reads outcomes off
// `LoadUnrefOutcome`/`ArrivalStateByID` — never once through the authoring
// function itself, and never once through `UnrefRefusal.Error()`. The three
// pre-append refusals F4 §3.6 promises the officer are reached by nothing.
package domain

import (
	"errors"
	"testing"
)

// TestWitnessQ10ArrivalUnrefRefusesUnknownArrival kills plant Q10
// (arrival_unref.go:518, `if arrival == nil {` -> `if false && arrival == nil {`).
//
// RetractArrivalRef's guard right after the raw lookup refuses an arrival ID
// this site has never authored. Under the plant that guard never fires, and
// execution falls straight into the next line, `if arrival.TripRef == nil`,
// with arrival itself nil — a nil-pointer dereference. Either failure mode
// (wrong error, or an outright crash) must turn this test red, so the panic
// is recovered and converted into a plain t.Fatalf rather than left to abort
// the process.
func TestWitnessQ10ArrivalUnrefRefusesUnknownArrival(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("RetractArrivalRef panicked instead of refusing an unknown arrival: %v", r)
		}
	}()

	st := w9bNewStore(t)

	_, err := RetractArrivalRef(st, "site-a", 1, "arrival-never-authored", nil, w9bBase, "default")
	if err == nil {
		t.Fatalf("RetractArrivalRef: got nil error for an arrival this site has never heard of, want a refusal")
	}
	var refusal *UnrefRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("RetractArrivalRef: err = %v (%T), want *UnrefRefusal", err, err)
	}
	if refusal.Code != UnrefArrivalUnknown {
		t.Fatalf("refusal.Code = %v, want UnrefArrivalUnknown", refusal.Code)
	}
	const want = `domain: unref: arrival arrival-never-authored is not known at this site`
	if got := refusal.Error(); got != want {
		t.Fatalf("officer-facing refusal text = %q, want %q", got, want)
	}
}

// TestWitnessQ11ArrivalUnrefRefusalRendersItsSentence kills plant Q11
// (arrival_unref.go:491, inside `func (e *UnrefRefusal) Error() string {` —
// `if true { return "" }` inserted at the top, so EVERY unref refusal
// renders as the empty string).
//
// ⭐ This is the census's own headline example: the refusal still HAPPENS —
// RetractArrivalRef still returns a *UnrefRefusal carrying the right Code —
// but the sentence the officer reads is gone, and nothing before this test
// noticed because nothing ever called .Error() on one. Exercised across all
// three refusal codes so the plant's full blast radius (every refusal, not
// one sampled path) is on the record, and because the third case also
// exercises the ACCEPTING half of RetractArrivalRef (the first retraction
// must succeed) before it exercises the refusal.
func TestWitnessQ11ArrivalUnrefRefusalRendersItsSentence(t *testing.T) {
	st := w9bNewStore(t)

	cases := []struct {
		name  string
		code  UnrefRefusalCode
		want  string
		setup func(t *testing.T) string // returns the arrivalID to retract
	}{
		{
			name: "unknown arrival",
			code: UnrefArrivalUnknown,
			want: `domain: unref: arrival q11-unknown is not known at this site`,
			setup: func(t *testing.T) string {
				return "q11-unknown"
			},
		},
		{
			name: "no trip reference",
			code: UnrefNoReference,
			want: `domain: unref: arrival q11-noref carries no trip reference — its binding is fold-derived (§5 rule 4); resolve it through the review lane`,
			setup: func(t *testing.T) string {
				w9bApplyNext(t, st, "site-a", 1, w9bBase, w9bWireArrival("q11-noref", "", nil, nil))
				return "q11-noref"
			},
		},
		{
			name: "already retracted",
			code: UnrefAlreadyRetracted,
			want: `domain: unref: arrival q11-already's reference has already been retracted`,
			setup: func(t *testing.T) string {
				w9bApplyNext(t, st, "site-a", 1, w9bBase+w9bMinute, w9bWireArrival("q11-already", "some-trip", nil, nil))
				if _, err := RetractArrivalRef(st, "site-a", 1, "q11-already", nil, w9bBase+2*w9bMinute, "default"); err != nil {
					t.Fatalf("fixture: first (accepting) retraction of q11-already must succeed: %v", err)
				}
				return "q11-already"
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			arrivalID := tc.setup(t)
			_, err := RetractArrivalRef(st, "site-a", 1, arrivalID, nil, w9bBase+3*w9bMinute, "default")
			if err == nil {
				t.Fatalf("RetractArrivalRef(%s): got nil error, want a %v refusal", arrivalID, tc.code)
			}
			var refusal *UnrefRefusal
			if !errors.As(err, &refusal) {
				t.Fatalf("RetractArrivalRef(%s): err = %v (%T), want *UnrefRefusal", arrivalID, err, err)
			}
			if refusal.Code != tc.code {
				t.Fatalf("refusal.Code = %v, want %v", refusal.Code, tc.code)
			}
			if got := refusal.Error(); got != tc.want {
				t.Fatalf("officer-facing refusal text = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRegressionArrivalUnrefRefusesEmptyArrivalID renders line 511-513's
// refusal sentence (arrival_unref.go, `if arrivalID == "" {`), one of the 34
// unreached blocks in this file's census row and NOT covered by Q10 or Q11
// above (Q10's guard fires only after a store lookup for a NON-empty
// arrivalID comes back empty; this is the earlier guard that catches an
// empty arrivalID before any lookup happens at all).
//
// ⚠ NOT A WITNESS — SELF-AUTHORED PLANT Q10a SURVIVES, REPORTED AS A
// SURVIVOR. I wrote a line-anchored plant for this block
// (`if arrivalID == "" {` -> `if false && arrivalID == "" {"`, following the
// "produce the plant first" pattern the brief specifies for y9/y12), applied
// it, and re-ran this exact test: it still PASSED. Root cause, measured: for
// arrivalID == "", `unrefRawArrival(st, "")` genuinely finds no row (no
// arrival is ever authored with an empty id), so bypassing this guard falls
// straight into Q10's `if arrival == nil` guard three lines down, which
// fires anyway and returns byte-identical output — same Code
// (UnrefArrivalUnknown), same ArrivalID (""), same .Error() string. The two
// guards are behaviorally redundant for every input capable of reaching
// this one. No black-box, officer-facing assertion can tell them apart; the
// only observable difference is an extra no-op store round-trip, which the
// brief's own rule ("assert the officer-facing behaviour, not the
// implementation") says not to test for. Kept as a regression test of the
// rendered sentence, not claimed as a kill.
func TestRegressionArrivalUnrefRefusesEmptyArrivalID(t *testing.T) {
	st := w9bNewStore(t)

	_, err := RetractArrivalRef(st, "site-a", 1, "", nil, w9bBase, "default")
	if err == nil {
		t.Fatalf("RetractArrivalRef: got nil error for an empty arrivalID, want a refusal")
	}
	var refusal *UnrefRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("RetractArrivalRef: err = %v (%T), want *UnrefRefusal", err, err)
	}
	if refusal.Code != UnrefArrivalUnknown {
		t.Fatalf("refusal.Code = %v, want UnrefArrivalUnknown", refusal.Code)
	}
	if refusal.ArrivalID != "" {
		t.Fatalf("refusal.ArrivalID = %q, want empty (this guard fires before arrivalID is ever recorded on the refusal)", refusal.ArrivalID)
	}
	const want = `domain: unref: arrival  is not known at this site`
	if got := refusal.Error(); got != want {
		t.Fatalf("officer-facing refusal text = %q, want %q", got, want)
	}
}
