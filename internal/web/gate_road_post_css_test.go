// gate_road_post_css_test.go — D-88's own criterion, extended to the shared
// stylesheet. MEASURED live on the deployed road post (10.90.1.10): the
// gate rebuild's shared .orientation-onfile rule (_styles.html) — one
// static CSS class, no per-site branch anywhere else in that file — put
// exactly one "orient" substring on a road post's served page, where the
// criterion has always been zero. The class is genuinely inert there (no
// element on an orientation=false post ever carries it), so this was never
// a real D-88 breach — but a substring check that now always reports one
// hit is a detector the next reader learns to ignore, which is worse than
// no detector. Fixed at the source: the rule (and its own comment) is
// gated behind {{with field .Data "Cfg"}}{{if .HasOrientation}}, the same
// safe, reflection-based "does this page's data carry this" the shared top
// bar already uses (templatefuncs.go) — never a rename, which would only
// hide the word while a road post still shipped orientation styling it can
// never reach.
//
// newGateTestServer, not newOrientBlockWorld, is the fixture here
// deliberately: newOrientBlockWorld's own roster seeds people literally
// named "Orla Oriented" / "Uma Unoriented" for ITS tests' purposes, which
// would make a whole-page substring sweep here about THEIR names, not
// about the stylesheet this fix actually touches. newGateTestServer's
// fixture ("Dana Driver", "Pat Passenger", "Chris Crew") carries none of
// that, and its config never sets Orientation — HasOrientation() reads
// false, a road post by construction, same as newOrientBlockWorld(t,
// false) would give, without the name collision.
package web

import (
	"strings"
	"testing"
)

// TestARoadPostGatePageCarriesNoOrientationVocabulary is the RED this fix
// closes: before it, this failed with one hit, from .orientation-onfile's
// own rule and comment in the served <style> block — present on EVERY
// page (road post included) because _styles.html had no conditional
// inclusion mechanism otherwise.
func TestARoadPostGatePageCarriesNoOrientationVocabulary(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	if srv.Cfg.HasOrientation() {
		t.Fatal("precondition: this fixture must be a road post")
	}
	_, page := getGate(t, srv)
	if n := strings.Count(strings.ToLower(page), "orient"); n != 0 {
		t.Errorf("a road post's /gate page carries %d occurrence(s) of \"orient\" — D-88's own criterion is zero, not \"however many happen to be inert\"", n)
	}
}

// TestAnOrientationPostsStylesheetStillCarriesTheOnFileRule is the negative
// control: the fix above must not have also removed the rule everywhere.
// An orientation-bearing post still needs .orientation-onfile's rule —
// gate.html renders the class on a known, already-oriented row (the "show,
// don't ask" tick state) — so its CSS must still ship there.
func TestAnOrientationPostsStylesheetStillCarriesTheOnFileRule(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	if !w.srv.Cfg.HasOrientation() {
		t.Fatal("precondition: this world must be an orientation post")
	}
	page := orientBlockGet(t, w.srv, "/gate")
	if !strings.Contains(page, ".orientation-onfile") {
		t.Error("an orientation post's /gate page no longer carries the .orientation-onfile rule — the class exists here and needs its CSS")
	}
}
