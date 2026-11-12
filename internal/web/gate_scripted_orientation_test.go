// gate_scripted_orientation_test.go — ⟦X-R12-JSON / F-L03-R2-1, 2026-09-30⟧
// a KNOWN passenger added by the SCRIPT on /gate/vehicle at an orientation
// post gets the same R-12 "NOT ORIENTED HERE" warning + tick the server
// render has always given a scripting-off row.
//
// THE DEFECT, measured live on 6a3a0fa (lanes-r2/L05.md X-R12-JSON,
// lanes-r2/L03.md F-L03-R2-1): a row cloned from #passenger-template never
// went back to the server before commit, and neither the template nor the
// roster payload carried any orientation state, so with scripting on the
// warning and its tick never appeared — the officer's first notice was the
// post-commit NOT ENTERED banner. Fail-safe (still held out), but R-12's
// pre-commit warning-with-tick existed only with scripting off.
//
// WHAT A GO TEST CAN SEE (no JS runtime in this module, by decision — see
// gate_adopt_confirm_test.go's header). The browser half was driven live
// with Playwright at the pad. Pinned here:
//
//  1. the PAYLOAD: an authenticated orientation post's roster-data carries
//     each person's orientation-at-this-post (orient_needed / orient_held —
//     decorateRows' own two flags); a logged-out request still gets no
//     persons at all (PII-1). RED on 6a3a0fa.
//  2. the SCRIPT's shape: the served page carries the builder, hooked into
//     showKnown/showNew and renumber, posting the server block's own field
//     names. RED on 6a3a0fa.
//  3. the BODY that builder posts — add_orientation = row index plus
//     orient_target = "<row index>:<person id>", exactly the server block's
//     pair — orients and enters when ticked, and holds out when unticked.
//
// A road post's page must still carry zero "orient" occurrences (D-88):
// gate_road_post_css_test.go sweeps the whole served page, payload and
// script included, so the builder and both keys stay behind HasOrientation.
package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

func scriptedOrientPayload(t *testing.T, body string) map[string]rosterPerson {
	t.Helper()
	blob := sectionBetween(t, body, `id="roster-data"`, "</script>")
	blob = blob[strings.Index(blob, ">")+1:]
	var roster struct {
		Persons []rosterPerson `json:"persons"`
	}
	if err := json.Unmarshal([]byte(blob), &roster); err != nil {
		t.Fatalf("roster-data is not JSON: %v\n%s", err, blob)
	}
	out := map[string]rosterPerson{}
	for _, p := range roster.Persons {
		out[p.ID] = p
	}
	return out
}

func TestGateRosterPayloadCarriesThisPostsOrientationForTheScriptedRow(t *testing.T) {
	w := newOrientBlockWorld(t, true) // Dana + Orla hold site-b's orientation, Uma does not
	req := httptest.NewRequest("GET", "/gate/vehicle", nil)
	req.AddCookie(loginCookie(t, w.srv, testLoginPassword))
	rec := httptest.NewRecorder()
	w.srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /gate/vehicle = %d", rec.Code)
	}
	got := scriptedOrientPayload(t, rec.Body.String())
	for _, c := range []struct {
		p             domain.Person
		needed, held bool
	}{{w.uma, true, false}, {w.orla, false, true}, {w.driver, false, true}} {
		rp, ok := got[c.p.PersonID]
		if !ok {
			t.Fatalf("precondition: %s is not in the authenticated payload", c.p.Name)
		}
		if rp.OrientNeeded != c.needed || rp.OrientHeld != c.held {
			t.Errorf("%s: payload orient_needed=%v orient_held=%v, want %v/%v — the script cannot warn (or confirm) before commit", c.p.Name, rp.OrientNeeded, rp.OrientHeld, c.needed, c.held)
		}
	}
	// PII-1: the anonymous payload still names nobody, so it says nothing
	// about anybody's orientation either.
	if anon := scriptedOrientPayload(t, orientBlockGet(t, w.srv, "/gate/vehicle")); len(anon) != 0 {
		t.Errorf("a logged-out /gate/vehicle carries %d persons in its payload (PII-1)", len(anon))
	}
}

func TestGateVehicleScriptBuildsTheOrientationWarningForAScriptedKnownRow(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	page := orientBlockGet(t, w.srv, "/gate/vehicle")
	for _, want := range []string{
		"function syncOrientRow(",
		"syncOrientRow(row, it);",   // showKnown: a known pick gets the block
		// ⟦was: "syncOrientRow(row, null);" — showNew dropped the block on
		// every not-picked row. F-L03-R4-3 (fix3, 2026-10-04): a row retyped
		// to another roster person's exact name keeps it for that person;
		// exactRosterHit returns null for a new/edited-away row, which still
		// loses it.⟧
		"syncOrientRow(row, exactRosterHit(row));", // showNew
		"function exactRosterHit(row)",
		"cross-site-orientation",
		"NOT ORIENTED HERE",
		"'add_orientation'",
		"'orient_target'",
		"it.orient_needed",
		"row.querySelector('.p-add-orientation')", // renumber keeps the tick's row position
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the served /gate/vehicle script lacks %q — a scripted known row gets no R-12 warning/tick before commit", want)
		}
	}
}

// TestGateScriptedOrientationTickBodyOrientsAndEntersUntickedHoldsOut posts
// exactly what syncOrientRow's block submits for a scripted row at index 1:
// orient_target "1:<id>" always, add_orientation "1" only when ticked.
func TestGateScriptedOrientationTickBodyOrientsAndEntersUntickedHoldsOut(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	uma := w.uma
	if held, err := domain.OrientationHeld(w.st, uma.PersonID, "site-b"); err != nil || held != nil {
		t.Fatalf("precondition: Uma must NOT hold site-b's orientation (held=%v err=%v)", held, err)
	}

	unticked := scriptedCheckInBody("TRK-71", 0, orientBlockRows(w.driver, uma))
	unticked.Add("orient_target", "1:"+uma.PersonID)
	rec := postForm(t, w.srv, "/gate/checkin", unticked)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unticked scripted commit = %d: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, notEnteredQueryKey+"=") {
		t.Errorf("an UNTICKED row was not reported held out: %q", loc)
	}
	if orientBlockOpenPersonIDs(t, w.st)[uma.PersonID] {
		t.Fatal("an UNTICKED un-oriented row was entered")
	}
	if held, _ := domain.OrientationHeld(w.st, uma.PersonID, "site-b"); held != nil {
		t.Fatal("an UNTICKED row recorded an orientation")
	}

	ticked := scriptedCheckInBody("TRK-72", 0, orientBlockRows(w.driver, uma))
	ticked.Add("add_orientation", "1")
	ticked.Add("orient_target", "1:"+uma.PersonID)
	rec = postForm(t, w.srv, "/gate/checkin", ticked)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("ticked scripted commit = %d: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); strings.Contains(loc, notEnteredQueryKey+"=") {
		t.Errorf("a TICKED row was still held out: %q", loc)
	}
	if !orientBlockOpenPersonIDs(t, w.st)[uma.PersonID] {
		t.Fatal("a TICKED row was not entered")
	}
	if held, err := domain.OrientationHeld(w.st, uma.PersonID, "site-b"); err != nil || held == nil {
		t.Fatalf("a TICKED row did not record site-b's orientation (err=%v)", err)
	}
}
