// displaymatrix.go — the provenance × clock_state DISPLAY-MATRIX helper
// (TP §7.4, a W2 deliverable; criterion W2-10).
//
// This is a CLASSIFICATION helper, not a renderer: it names which of §7.4's
// display classes one stored event's time falls in, and how a class is
// worded on a page stays TP's header-rule debt 2, owed at the first
// consumer build. It CONSUMES ProvenanceClass's result (provenance.go's
// one-place rule) and never re-derives provenance inline — callers hand it
// the Provenance they already classified, alongside the event's stored
// authoring clock state.
//
// The matrix (TP §7.4; unverified rides the unsynced column):
//
//	                      synced                     unsynced / unverified
//	default               DisplayObservation         DisplayObservationClockUntrusted
//	entered               DisplayClaim               DisplayClaim — the officer's claim is
//	                                                 NOT stamped with the clock warning
//	                                                 (the claim is not the machine's
//	                                                 reading; the AUTHORING stamp beside
//	                                                 it carries the warning — renderer's
//	                                                 job, debt 2)
//	untrusted provenance  DisplayUntrustedProvenance same — never an observation
//
// The entered×untrusted-clock cell is the round-1 print-inversion fix: a
// wording class derived from clock_state alone printed corrected times as
// "may be wrong". The substitution cell (a reader substituting a corrected
// instant, TP §4.7) is W4's to exercise — it calls this same function with
// the correction event's own clock_state, which the contract already gives.
package domain

import "sitecontrol/internal/store"

// TimeDisplayClass is one cell class of TP §7.4's display matrix. The zero
// value is DisplayUntrustedProvenance — fail closed by construction
// (Provenance's own precedent): a forgotten branch can never default to an
// observation.
type TimeDisplayClass int

const (
	// DisplayUntrustedProvenance: the stored bytes do not support either
	// honest reading. Rendered as UNTRUSTED PROVENANCE, never as an
	// observation, whatever the clock said.
	DisplayUntrustedProvenance TimeDisplayClass = iota
	// DisplayObservation: default × synced — "recorded ‹time›", an
	// observation the machine stamped under a trusted clock.
	DisplayObservation
	// DisplayObservationClockUntrusted: default × unsynced/unverified — an
	// observation whose recording clock was untrusted: "recorded ‹time› —
	// CLOCK UNTRUSTED at recording". Dropping this distinction is m14
	// limb (b).
	DisplayObservationClockUntrusted
	// DisplayClaim: entered, EITHER clock column — the officer's claim.
	// The entered value is NOT stamped with the clock warning: the claim
	// is not the machine's reading. Conflating this with the default
	// column is m14 limb (a), the measured round-1 print-inversion.
	DisplayClaim
)

// DisplayTimeClass classifies one stored event's rendered time into TP
// §7.4's matrix. p is ProvenanceClass's result for that event's stored
// pair (never re-derived here); clockState is the event's stored
// AuthoredAt.State (or, for §7.4's substitution cell, the correction
// event's own clock state — the typing act being contextualised).
//
// An unrecognised clock state is treated as untrusted for the default
// column (it is anything but a verified synced reading — fail toward the
// warning, never toward a clean observation); an unrecognised Provenance
// value falls through to DisplayUntrustedProvenance, the fail-closed zero.
func DisplayTimeClass(p Provenance, clockState store.ClockState) TimeDisplayClass {
	switch p {
	case ProvenanceEntered:
		return DisplayClaim
	case ProvenanceDefault:
		if clockState == store.ClockSynced {
			return DisplayObservation
		}
		return DisplayObservationClockUntrusted
	default:
		return DisplayUntrustedProvenance
	}
}
