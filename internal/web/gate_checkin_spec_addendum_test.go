// gate_checkin_spec_addendum_test.go — the acceptance test for James's
// 2026-09-13 addendum to the gate-checkin-spec rebuild, in his own words:
//
//	"the oriantation intake form has that information so we collect it...
//	the last name and first name box's should be a searchable field that
//	will auto fill based on text entered with a dropdown menu of all
//	persons in the entire database so that the information only needs to
//	be added in one location but the oriantation check can be ticked based
//	on current location so if joe smith is oriantated at STP and enjgested
//	there then gos to NPF the npf officer can just check the box for
//	oriantation completed and allow joe onsite."
//
// This is R-12 plus the cross-site orientation tick, already built and
// already covered elsewhere
// (TestCrossSiteWarningNamesThisSiteForAPersonOrientedElsewhere and this
// addendum's own one-shot verifications, not committed). What this file
// adds is the NAMED scenario, end to end,
// with his own labels: Joe Smith, enrolled and oriented at STP, found by
// type-ahead at NPF, ticked, admitted — so the acceptance test traces
// directly to his words rather than only to the mechanism's own tests.
package web

import (
	"net/http"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// TestJoeSmithOrientedAtOnePostEntersAtAnotherViaTypeAhead is the addendum's
// own acceptance test, run literally.
func TestJoeSmithOrientedAtOnePostEntersAtAnotherViaTypeAhead(t *testing.T) {
	// w.srv is NPF (site-b, orientation=true) — see newOrientBlockWorld.
	// Joe is enrolled and oriented at STP (site-a) directly through the
	// domain edge, exactly as a real STP enrolment + orientation would be —
	// never through NPF's own doors, and never given NPF's orientation.
	w := newOrientBlockWorld(t, true)
	joe, err := domain.CreatePerson(w.st, "site-a", 1, "Joe Smith", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.CompleteOrientation(w.st, "site-a", 1, joe.PersonID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}

	// THE WHOLE PERSON LIST IS ALREADY LOCAL AT NPF: domain.Persons(st) is
	// not scoped to any one site (dumpSection reads the composed "person"
	// table whole), and Store.Apply folds a peer's events with no site_id
	// filter either — so a single store already models "after normal sync"
	// without a second, simulated store. This is the claim the addendum
	// asked to be VERIFIED rather than assumed, and this line is that
	// verification: Joe, enrolled at site-a, is found at site-b with no
	// second store, no second query, and no new endpoint.
	all, err := domain.Persons(w.st)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range all {
		if p.PersonID == joe.PersonID {
			found = true
		}
	}
	if !found {
		t.Fatalf("VERIFICATION FAILED: Joe (enrolled at site-a) does not appear in domain.Persons read at site-b — the whole-database type-ahead has nothing to search")
	}

	// The NPF officer's row: Joe, picked (kind=existing, value=Joe's id —
	// exactly the shape a dropdown click produces, R-10's confirmation).
	// He does not hold NPF's (site-b's) orientation — only STP's (site-a's).
	_, page := getGate(t, w.srv)
	page = gateSetVehicle(t, w.srv, page, gateInbound, "NPF TRUCK 1")
	vals := gateFormValues(t, page, formIDFor(gateInbound))
	vals.Set("new_last", "Joe Smith")
	vals.Set("new_first", "")
	vals.Set("new_kind", "existing")
	vals.Set("new_value", joe.PersonID)
	rec := postForm(t, w.srv, "/gate/passenger/add", vals)
	if rec.Code != http.StatusOK {
		t.Fatalf("add Joe = %d: %s", rec.Code, rec.Body.String())
	}
	page = rec.Body.String()
	if !strings.Contains(page, "NOT ORIENTED HERE") || !strings.Contains(page, "Joe Smith") {
		t.Fatalf("Joe's row does not show NPF's not-oriented-here warning:\n%s", page)
	}
	// class="field orientation-onfile" is the RENDERED element, not the
	// bare CSS rule text (which — ⟦D-88, upstream fix 697f113⟧ — is now
	// itself gated to orientation posts in _styles.html, so this is no
	// longer even a false-positive shape at a road post; kept exact-match
	// here regardless, since a KNOWN orientation post is exactly where the
	// rule DOES ship, so the element check must still be precise).
	if strings.Contains(page, `class="field orientation-onfile"`) {
		t.Fatalf("Joe's row shows the ON-FILE state at NPF — he holds STP's orientation, not NPF's:\n%s", page)
	}

	// The NPF officer ticks "add NPF's orientation for Joe now, and check
	// him in" and commits.
	vals = gateFormValues(t, page, formIDFor(gateInbound))
	vals = gateMarkDriver(vals, 0)
	vals.Set("add_orientation", "0")
	vals.Set("orient_target", "0:"+joe.PersonID)
	rec = postForm(t, w.srv, "/gate/checkin", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("commit = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); strings.Contains(loc, "not_entered=") {
		t.Fatalf("Joe was held out despite the tick: %s", loc)
	}

	open, err := domain.OpenVisitsAt(w.st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	onSite := false
	for _, v := range open {
		if v.PersonID == joe.PersonID {
			onSite = true
		}
	}
	if !onSite {
		t.Fatal("Joe is not on NPF's (site-b's) muster after the ticked commit")
	}
	heldAtB, err := domain.OrientationHeld(w.st, joe.PersonID, "site-b")
	if err != nil || heldAtB == nil {
		t.Fatalf("Joe does not hold NPF's (site-b's) orientation after the tick — err=%v held=%v", err, heldAtB)
	}
	heldAtA, err := domain.OrientationHeld(w.st, joe.PersonID, "site-a")
	if err != nil || heldAtA == nil {
		t.Fatalf("Joe's ORIGINAL STP (site-a) orientation was disturbed — err=%v held=%v", err, heldAtA)
	}
}
