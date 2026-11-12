// gate_decline_namesake_test.go — ⟦F-R2-L02-2, 2026-09-30⟧ the individual
// page's "No — this is a different person" answer is NEVER the namesake.
//
// THE DEFECT, as measured live on 6a3a0fa (lanes-r2/L02.md): the type-ahead
// asks "Confirm who this is" for an exactly-typed roster name; the officer
// answers "No — this is a different person"; the page switches to the
// new-person state (at an orientation post: "Not on the roster … this will
// be refused"); CHECK IN then posted new_kind=new with an EMPTY new_value.
// rowPersonSlot honours kind=new only while value equals the typed name, so
// the empty value fell through to resolveTypedPerson, whose single exact hit
// was the namesake: at the ice road the namesake was checked in and the
// company the officer picked was dropped; at the pad an un-oriented,
// un-enrolled stranger was entered on the namesake's orientation.
//
// WHAT A GO TEST CAN SEE (gate_adopt_confirm_test.go's header: this module
// carries no JavaScript runtime, by decision). Two halves, both pinned:
//
//  1. the SCRIPT's shape: the served finalizePick must carry a declined name
//     into new_value (composed exactly as the server composes it) instead of
//     an unconditional ''. This is the half that was broken, and the test
//     that goes RED on 6a3a0fa. The browser half was driven live with
//     Playwright at both posts, JS on and off.
//  2. the SERVER's answer to the body the fixed script posts — at a road post
//     a DISTINCT person is minted with the picked company and the namesake is
//     untouched; at an orientation post the check-in is refused and nothing
//     at all is authored. These held already (rowPersonSlot's deliberate
//     off-roster branch); pinned so the script's fix cannot be undone by a
//     server change in silence.
package web

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// servedFinalizePick is the individual form's finalizePick body as SERVED
// on /gate (template comments already stripped by html/template).
func servedFinalizePick(t *testing.T, page string) string {
	t.Helper()
	start := strings.Index(page, "function finalizePick() {")
	if start < 0 {
		t.Fatal("the served /gate page carries no finalizePick — the individual form's pick resolution is missing")
	}
	end := strings.Index(page[start:], "function syncFirstNameField(")
	if end < 0 {
		t.Fatal("finalizePick is not followed by syncFirstNameField in the served page — the scan's end marker moved; re-anchor it")
	}
	return page[start : start+end]
}

func TestGateIndividualDifferentPersonDeclinePostsTheComposedNameNotABlank(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	code, page := getGateIndividual(t, srv)
	if code != http.StatusOK {
		t.Fatalf("GET /gate = %d", code)
	}
	body := servedFinalizePick(t, page)
	// Precondition (rule 6): this IS the function that writes the hidden
	// pick fields — otherwise every assertion below is about nothing.
	if !strings.Contains(body, "valueField.value =") || !strings.Contains(body, "kindField.value =") {
		t.Fatalf("the scanned finalizePick does not write new_kind/new_value — wrong function scanned:\n%s", body)
	}
	if strings.Contains(body, "pick.id : '';") {
		t.Errorf("finalizePick still posts an EMPTY new_value for every non-existing pick — a \"No — this is a different person\" answer arrives indistinguishable from an unmatched name, and the server resolves it to the namesake:\n%s", body)
	}
	for _, want := range []string{"declined", "jsComposeName("} {
		if !strings.Contains(body, want) {
			t.Errorf("finalizePick does not reference %q — a declined name is not composed into new_value the way the server composes the boxes:\n%s", want, body)
		}
	}
}

// TestGateIndividualDeclinedNamesakeAtARoadPostMintsADistinctPerson: the
// body the fixed script posts at the ice road (a road post, D-88: it adds on
// demand). A new, distinct person is minted with the company the officer
// picked; the namesake's record and visit state are untouched.
func TestGateIndividualDeclinedNamesakeAtARoadPostMintsADistinctPerson(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	chris := persons[2] // "Chris Crew", Kakivik Asset Management
	if srv.Cfg.HasOrientation() {
		t.Fatal("precondition: newGateTestServer must be a road post (adds on demand)")
	}
	aurora, err := domain.CreateCompany(st, "site-a", 1, "Aurora Mechanical", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if chris.CompanyID == nil || *chris.CompanyID == aurora.CompanyID {
		t.Fatal("precondition: the namesake must hold a company OTHER than the one the officer picks")
	}
	before, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	same := 0
	for _, p := range before {
		if strings.EqualFold(p.Name, chris.Name) {
			same++
		}
	}
	if same != 1 {
		t.Fatalf("precondition: exactly one roster person must be called %q (the lone exact hit resolveTypedPerson adopts), got %d", chris.Name, same)
	}

	vals := singleCheckInForm(t, srv, chris.Name)
	vals.Set("new_kind", "new")
	vals.Set("new_value", chris.Name) // what the fixed script posts after the decline
	vals.Set("new_company_id", aurora.CompanyID)
	rec := postForm(t, srv, "/gate/checkin/single", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("declined-namesake check-in at a road post = %d, want 303: %s", rec.Code, rec.Body.String())
	}

	after, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before)+1 {
		t.Fatalf("roster %d -> %d: the declined name was not minted as a new person", len(before), len(after))
	}
	var minted *domain.Person
	for i := range after {
		if after[i].PersonID != chris.PersonID && strings.EqualFold(after[i].Name, chris.Name) {
			minted = &after[i]
		}
	}
	if minted == nil {
		t.Fatalf("no DISTINCT person called %q was minted", chris.Name)
	}
	if minted.CompanyID == nil || *minted.CompanyID != aurora.CompanyID {
		t.Errorf("the minted person's company = %v, want the officer's pick %s — the pick was dropped", minted.CompanyID, aurora.CompanyID)
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	var mintedOn, chrisOn bool
	for _, v := range open {
		mintedOn = mintedOn || v.PersonID == minted.PersonID
		chrisOn = chrisOn || v.PersonID == chris.PersonID
	}
	if !mintedOn {
		t.Error("the minted person is not on site")
	}
	if chrisOn {
		t.Error("the NAMESAKE was checked in — the officer said this is a different person")
	}
	now, err := domain.PersonByID(st, chris.PersonID)
	if err != nil || now == nil {
		t.Fatalf("namesake lookup: %v", err)
	}
	if now.CompanyID == nil || *now.CompanyID != *chris.CompanyID {
		t.Errorf("the namesake's company changed %v -> %v", *chris.CompanyID, now.CompanyID)
	}
}

// TestGateIndividualDeclinedNamesakeAtAnOrientationPostIsRefusedAndAuthorsNothing:
// the same body at the pad. The namesake holds the pad's orientation; the
// person at the window, by the officer's own answer, is somebody else — not
// on the roster, never oriented. Refused with the R-12 copy; the store's
// watermark does not move; nobody is entered.
func TestGateIndividualDeclinedNamesakeAtAnOrientationPostIsRefusedAndAuthorsNothing(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	orla := w.orla
	if held, err := domain.OrientationHeld(w.st, orla.PersonID, "site-b"); err != nil || held == nil {
		t.Fatalf("precondition: the namesake must hold this post's orientation (else a refusal proves nothing): %v", err)
	}
	if open := orientBlockOpenPersonIDs(t, w.st); len(open) != 0 {
		t.Fatalf("precondition: nobody on site, got %v", open)
	}
	before, count := omodelWatermark(t, w), omodelPersonCount(t, w)

	vals := singleCheckInForm(t, w.srv, orla.Name)
	vals.Set("new_kind", "new")
	vals.Set("new_value", orla.Name)
	vals.Set("vehicle", "TRK-303")
	rec := postForm(t, w.srv, "/gate/checkin/single", vals)
	if rec.Code != http.StatusConflict {
		t.Fatalf("declined-namesake check-in at an orientation post = %d, want 409 (refused): %s", rec.Code, rec.Body.String())
	}
	page := omodelText(rec.Body.String())
	// ⟦W3, 2026-10-03 — L02 r3 LOW⟧ ⟦was: "is not on the roster"⟧ — false
	// for a declined namesake (the roster DOES carry that name). The refusal
	// now says the roster's record is somebody else, by the officer's answer.
	for _, want := range []string{"is a different person from the " + orla.Name + " already on the roster", "Nobody is added to the system without an orientation", "Nothing was recorded"} {
		if !strings.Contains(page, want) {
			t.Errorf("the refusal lacks %q", want)
		}
	}
	if got := omodelPersonCount(t, w); got != count {
		t.Fatalf("the refused check-in CREATED a person: roster %d -> %d", count, got)
	}
	if after := omodelWatermark(t, w); after != before {
		t.Fatalf("the refused check-in authored: watermark %d -> %d", before, after)
	}
	if open := orientBlockOpenPersonIDs(t, w.st); len(open) != 0 {
		t.Fatalf("the refused check-in entered somebody (the namesake, on his orientation?): open visits %v", open)
	}
}

// ⟦F-R3-L02-1, 2026-09-30⟧ A REFUSAL MUST NOT FORGET THE "NO". Measured on the
// final build (lanes-r3/L02.md): decline "Vaycrest, Cameron" at the pad, get
// refused for anything (a bad vehicle number), fix it, CHECK IN — the fresh
// page had lost the script's decline memory, re-asked "Confirm who this is",
// and Enter checked in the namesake. The server already re-renders the
// decline (new_kind=new, new_value = the composed name); these pin (1) the
// re-render carries that pair AND the company the officer picked, which the
// page used to reset to "none on file", and (2) the served script reads the
// pair back into its declined state (a shape scan — no JS runtime here; the
// browser half was driven live at the pad).
func TestGateIndividualRefusalReRenderKeepsTheDeclineAndThePickedCompany(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	co := omodelCompany(t, w)
	orla := w.orla
	vals := singleCheckInForm(t, w.srv, orla.Name)
	vals.Set("new_kind", "new")
	vals.Set("new_value", orla.Name)
	vals.Set("new_company_id", co.CompanyID)
	rec := postForm(t, w.srv, "/gate/checkin/single", vals)
	if rec.Code != http.StatusConflict {
		t.Fatalf("precondition: the declined namesake must be refused at an orientation post, got %d", rec.Code)
	}
	again := singleCheckInFormValues(t, rec.Body.String())
	if again.Get("new_kind") != "new" || again.Get("new_value") != orla.Name {
		t.Errorf("the refusal re-render dropped the decline: new_kind=%q new_value=%q, want new/%q", again.Get("new_kind"), again.Get("new_value"), orla.Name)
	}
	form := singleCheckInFormRe.FindString(rec.Body.String())
	if !regexp.MustCompile(`value="` + regexp.QuoteMeta(co.CompanyID) + `"\s+selected`).MatchString(form) {
		t.Errorf("the refusal re-render reset the officer's company pick %s to none on file", co.CompanyID)
	}

	body := rec.Body.String()
	for _, want := range []string{
		"kindField.value === 'new' && valueField.value !== ''",
		"declined = openInput.value.trim();",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the served individual-form script lacks %q — a refusal re-render forgets the decline and the next CHECK IN re-asks with the namesake as the default", want)
		}
	}
}
