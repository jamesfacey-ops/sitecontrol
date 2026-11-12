// witness_r28_revision2_test.go — R-28 revision 2 (team-lead, 2026-09-16),
// item 7: the Enter-key implicit-submit hazard flagged in VEHICLE-MODE.md's
// "found, not fixed" section, now ruled on.
//
// THE FIX CHOSEN, AND WHY THE LITERAL "server side" SUGGESTION DOES NOT
// WORK. team-lead offered "a checkout POST without the confirm button's
// own name/value is refused, or equivalent — choose and justify". Two
// server-side shapes were considered and both fail:
//
//   - Checking the COMMIT BUTTON's own name/value: per the HTML spec,
//     implicit submission (Enter) activates the form's "default button"
//     AS IF IT HAD BEEN CLICKED — its name/value pair rides the request
//     either way. A real click and an Enter-triggered implicit submit are
//     byte-identical on the wire; no server-side check can tell them
//     apart from the submitted body alone.
//   - A SEPARATE hidden field set only by a JS 'click' listener on the
//     button (real clicks fire a 'click' event; implicit submission does
//     not, in every major engine): this WOULD distinguish them, but it
//     would also make checkout impossible with scripting off — the field
//     would never be set at all, by any means, for that audience. This
//     codebase does not accept "requires JavaScript" for a route that
//     works without it today (every <details>/no-op-first-button pattern
//     in gate_vehicle.html exists specifically to avoid exactly that).
//
// THE FIX: a keydown listener, scoped to .vehicle-form-compact only,
// intercepting Enter on a text input and calling preventDefault() before
// the browser's own implicit-submission logic can activate anything (see
// gate_vehicle.html's own comment at the listener for the fuller
// derivation). This is a CLIENT fix; it does not touch the server, and it
// does not claim to close the scriptless case (stated plainly, the same
// "known gap" discipline picker_script's own header comment already uses
// for its own untestable runtime behaviour).
//
// THE PROOF: this package's own established pattern for a behaviour that
// only a JS RUNTIME could exercise (picker_test.go's byCommit/
// byIndividualCheckIn cases: "pageBody contains
// document.querySelectorAll(...)" as proof the WIRING exists, never a
// claim the click itself was executed) — a source-text assertion that the
// guard exists, is scoped correctly, and that the structural reason it is
// NEEDED (no Add button in Compact — see gate_vehicle_form's own header
// comment) still holds.
package web

import (
	"strings"
	"testing"
)

// TestWitnessCompactCheckoutBoxHasNoAddButtonSoEnterWouldOtherwiseCommit is
// the RED half: it re-proves, structurally, the precondition that makes
// the keydown guard necessary at all — the Compact form's only submit
// button is the commit, so without the guard, a scriptless-equivalent
// implicit submission has nothing else to activate. If a future edit ever
// gives Compact its own "Add" button (unlikely — Compact is a pre-
// populated confirm box, not an accumulate step), this precondition would
// go false and this test would need re-deriving, not silently pass.
func TestWitnessCompactCheckoutBoxHasNoAddButtonSoEnterWouldOtherwiseCommit(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	visitIDs := checkInCrew(t, srv, st, 2)
	page, _ := gateDepartSelected(t, srv, visitIDs)
	markup := gateFormMarkup(t, page, formIDFor(gateOutbound))

	if strings.Contains(markup, `class="p-add"`) {
		t.Fatalf("the Compact checkout box now has an Add button — the Enter-key guard's own precondition (no other submit button to structurally absorb implicit submission) no longer holds, and this test's reasoning needs re-deriving, not just re-passing:\n%s", markup)
	}
	submitButtons := strings.Count(markup, `type="submit"`)
	if submitButtons != 1 {
		t.Fatalf("the Compact checkout box has %d submit buttons, want exactly 1 (the commit) — this test's own precondition assumes there is nothing else implicit submission could activate: %s", submitButtons, markup)
	}
	if !strings.Contains(markup, `class="button-primary gate-commit"`) {
		t.Fatalf("the Compact checkout box's one submit button is not the commit — this test is not looking at the control it thinks it is:\n%s", markup)
	}
}

// TestWitnessCompactCheckoutBoxGuardsEnterFromImplicitlySubmitting is the
// GREEN half: the served script carries the keydown guard, scoped to
// .vehicle-form-compact (never the arriving/outbound-full forms, where
// Enter adding a passenger is the wanted behaviour —
// TestEnterCannotCommitAVehicle covers that half separately and is
// unaffected by this one).
func TestWitnessCompactCheckoutBoxGuardsEnterFromImplicitlySubmitting(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	_, page := getGate(t, srv)
	script := page[strings.Index(page, "<script"):]

	scopeCheck := `classList.contains('vehicle-form-compact')`
	guardAt := strings.Index(script, scopeCheck)
	if guardAt < 0 {
		t.Fatalf("the served script never scopes anything to vehicle-form-compact — the Enter-key guard cannot be scoped to it if it does not exist")
	}
	// The guard's own distinctive shape, all within a short window AFTER
	// that scoping check — a keydown listener that reads e.key, recognises
	// 'Enter', and calls preventDefault before anything else can act on
	// the keypress.
	window := script[guardAt+len(scopeCheck):]
	if len(window) > 1200 {
		window = window[:1200]
	}
	for _, want := range []string{
		`addEventListener('keydown'`,
		`e.key !== 'Enter'`,
		`e.preventDefault()`,
	} {
		if !strings.Contains(window, want) {
			t.Errorf("the served script does not carry %q shortly after the vehicle-form-compact scoping check — the Enter-key guard is not wired the way this test expects: %s", want, window)
		}
	}
}
