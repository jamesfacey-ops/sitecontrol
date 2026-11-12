// w2_display_matrix_test.go — W2-10: the provenance × clock_state
// DISPLAY-MATRIX helper (TP §7.4) exists as a pure function consuming
// domain.ProvenanceClass's RESULT (never re-deriving provenance inline —
// provenance.go:4–7's one-place rule), and EACH of the six cells is
// asserted against §7.4's expected class.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD: DisplayTimeClass / TimeDisplayClass and
// its four constants do not exist at 49abf65 (provenance.go:8–10 says in
// terms the §7.4 helper "is W2's and CONSUMES this result"). That failing
// compile is this criterion's pre-build RED.
//
// SHAPE PRESUMPTION (seam W2-S-F, the one place W2 binds this spelling —
// plan §5 step 3: a builder who prefers another spelling routes it to the
// gate, and only this file's bindings change):
//
//	func DisplayTimeClass(p Provenance, clockState store.ClockState) TimeDisplayClass
//
//	const (
//	    DisplayUntrustedProvenance TimeDisplayClass = iota // the ZERO value — fail closed, Provenance's own precedent
//	    DisplayObservation                                 // default × synced: "recorded ‹time›" — an observation
//	    DisplayObservationClockUntrusted                   // default × unsynced/unverified: observation + "CLOCK UNTRUSTED at recording"
//	    DisplayClaim                                       // entered, EITHER column: a claim; the entered value is NOT stamped with the clock warning
//	)
//
// This is a CLASSIFICATION helper, not a renderer: how a class is worded
// on a page stays TP's header-rule debt 2, owed at the first consumer
// build — this test's green must never be over-read as a screen behaving.
// The §7.4 substitution cell is W4's to exercise; the helper W2 ships must
// simply be callable with a correction's clock_state, which the six-cell
// contract already gives.
// (WINDOW-BUILD-PLAN-v1 §3 W2, criteria LOCKED 2026-08-28. Killed by m14,
// both limbs.)
package domain

import (
	"testing"

	"sitecontrol/internal/store"
)

// TestW2DisplayMatrixClassifiesAllSixCells — every cell of TP §7.4's
// matrix against its expected class; unverified rides the unsynced column
// (the matrix's own column heading: "unsynced / unverified").
func TestW2DisplayMatrixClassifiesAllSixCells(t *testing.T) {
	cases := []struct {
		name  string
		p     Provenance
		state store.ClockState
		want  TimeDisplayClass
	}{
		{"default×synced", ProvenanceDefault, store.ClockSynced, DisplayObservation},
		{"default×unsynced", ProvenanceDefault, store.ClockUnsynced, DisplayObservationClockUntrusted},
		{"default×unverified", ProvenanceDefault, store.ClockUnverified, DisplayObservationClockUntrusted},
		{"entered×synced", ProvenanceEntered, store.ClockSynced, DisplayClaim},
		{"entered×unsynced", ProvenanceEntered, store.ClockUnsynced, DisplayClaim},
		{"entered×unverified", ProvenanceEntered, store.ClockUnverified, DisplayClaim},
		{"untrusted×synced", UntrustedProvenance, store.ClockSynced, DisplayUntrustedProvenance},
		{"untrusted×unsynced", UntrustedProvenance, store.ClockUnsynced, DisplayUntrustedProvenance},
		{"untrusted×unverified", UntrustedProvenance, store.ClockUnverified, DisplayUntrustedProvenance},
	}
	for _, c := range cases {
		if got := DisplayTimeClass(c.p, c.state); got != c.want {
			t.Errorf("W2-10 cell %s: DisplayTimeClass = %v, want %v (TP §7.4's matrix — each cell asserted against its expected class, not mere totality)", c.name, got, c.want)
		}
	}
}

// TestW2DisplayMatrixEnteredIsNeverStampedWithTheClockWarning — the FIRST
// pinned cell (the measured round-1 print-inversion): entered ×
// unsynced/unverified does NOT stamp the officer's claim with the clock
// warning — the claim is not the machine's reading (the AUTHORING stamp
// beside it carries the warning, and that is the renderer's job, debt 2).
// Kills m14 limb (a): a wording class derived from clock_state alone,
// stamping entered values "may be wrong".
func TestW2DisplayMatrixEnteredIsNeverStampedWithTheClockWarning(t *testing.T) {
	for _, state := range []store.ClockState{store.ClockUnsynced, store.ClockUnverified} {
		got := DisplayTimeClass(ProvenanceEntered, state)
		if got != DisplayClaim {
			t.Errorf("W2-10 pinned cell entered×%s: DisplayTimeClass = %v, want DisplayClaim exactly — an entered value classified into a clock-warned class prints corrected times as \"may be wrong\": the measured round-1 print-inversion (mutant m14 limb a)", state, got)
		}
		if got == DisplayTimeClass(ProvenanceDefault, state) {
			t.Errorf("W2-10 pinned cell entered×%s: the entered cell classifies identically to default×%s — the helper conflates the officer's claim with the machine's reading (m14 limb a)", state, state)
		}
	}
	// And limb (b)'s discriminator from the same pin: the default column
	// must NOT drop its clock context — default×unsynced is a DIFFERENT
	// class from default×synced ("CLOCK UNTRUSTED at recording" lost is
	// m14 limb b).
	if DisplayTimeClass(ProvenanceDefault, store.ClockUnsynced) == DisplayTimeClass(ProvenanceDefault, store.ClockSynced) {
		t.Error("W2-10: default×unsynced classifies identically to default×synced — the default-column drop: \"CLOCK UNTRUSTED at recording\" is lost (mutant m14 limb b)")
	}
}

// TestW2DisplayMatrixUntrustedProvenanceNeverClassifiesAsObservation — the
// SECOND pinned cell, driven through the REAL upstream: ProvenanceClass on
// an unknown time_source and on the ill pair state, its result handed to
// the helper (the helper consumes ProvenanceClass's result — the one-place
// rule), in both clock columns. Never an observation.
func TestW2DisplayMatrixUntrustedProvenanceNeverClassifiesAsObservation(t *testing.T) {
	upstream := []struct {
		name string
		p    Provenance
	}{
		{"unknown time_source", ProvenanceClass("postbump-third-value-vNEXT", 5, 5)},
		{"ill pair state (default with broken rule-W equality)", ProvenanceClass("default", 10, 20)},
	}
	for _, u := range upstream {
		for _, state := range []store.ClockState{store.ClockSynced, store.ClockUnsynced, store.ClockUnverified} {
			got := DisplayTimeClass(u.p, state)
			if got != DisplayUntrustedProvenance {
				t.Errorf("W2-10 pinned cell: %s × %s classifies %v, want DisplayUntrustedProvenance — rendered as UNTRUSTED PROVENANCE, never as an observation (TP §7.4)", u.name, state, got)
			}
			if got == DisplayObservation || got == DisplayObservationClockUntrusted {
				t.Errorf("W2-10: %s × %s reached an OBSERVATION class — a positive certification the stored bytes cannot support", u.name, state)
			}
		}
	}
	// Fail-closed zero value (Provenance's own precedent): a zero
	// TimeDisplayClass must BE the untrusted class, so a forgotten branch
	// cannot default to an observation.
	var zero TimeDisplayClass
	if zero != DisplayUntrustedProvenance {
		t.Error("W2-10: the zero TimeDisplayClass is not DisplayUntrustedProvenance — the helper does not fail closed by construction")
	}
}
