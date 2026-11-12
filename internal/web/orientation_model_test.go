// orientation_model_test.go — ORIENTATION CREATES THE PERSON; every other
// site is a tick (James, 2026-09-10; design/REQUIREMENTS-FROM-JAMES-2026-09-09.md
// R-12): "no one will be placed in the system without the oriantation. the
// site oriantations should just be a check box for the current site to add
// to a preexisting person in the overall system".
//
// MEASURED at 1859b1a before this file existed (reports/witness/
// ORIENTATION-MODEL.md): orientation was already PER-SITE and the marking
// surface was already a TICK against an existing person — but TWO doors at an
// orientation post created a person with NO orientation: POST /roster/intake
// (the new-person form) and the gate's add-on-demand slot (which then held
// the freshly-minted person out). This file is the witness set for closing
// both, and for the property that must not break beside them.
//
// Every proof drives the REAL HTTP door (gate_orientation_block_test.go's
// world and posture): a domain-level call would pass whether or not the
// handler ever consulted this post's own flag.
//
//  1. orientation=true: intake ENROLS AND RECORDS this post's orientation in
//     the same submission — the new-person form IS the orientation form.
//  2. orientation=true: the gate's "not on the roster — add them" slot is
//     REFUSED, nothing authored, the list intact, the next action named.
//     Both body shapes (the live passenger list and the legacy stale tab).
//  3. orientation=true: intake's duplicate refusal OFFERS THE TICK against
//     each existing candidate (R-10: the officer confirms a NAMED person with
//     their company and origin site, never a silent adopt) and the tick
//     enrols nobody; a candidate who already holds it is not offered.
//  4. orientation=false (a road post — the ice road, D-88): BOTH doors keep
//     creating people, owing no orientation anywhere. Breaking this would
//     shut the ice road (R-9).
//  5. the cross-site tick: a person oriented at post C arriving at post B is
//     held out; ticking B's box (and nothing else) lets them in; C's row is
//     untouched.
package web

import (
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// omodelCompany gives the pad a company so intake's mandatory company pick
// has something to pick.
func omodelCompany(t *testing.T, w *orientBlockWorld) domain.Company {
	t.Helper()
	co, err := domain.CreateCompany(w.st, "site-b", 1, "Northwind Drilling", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	return co
}

func omodelWatermark(t *testing.T, w *orientBlockWorld) int64 {
	t.Helper()
	wm, err := w.st.Watermark("site-b", 1)
	if err != nil {
		t.Fatal(err)
	}
	return wm
}

func omodelPersonCount(t *testing.T, w *orientBlockWorld) int {
	t.Helper()
	ps, err := domain.Persons(w.st)
	if err != nil {
		t.Fatal(err)
	}
	return len(ps)
}

// omodelNewRow is the passenger-list row the live form submits for a name
// the type-ahead found nowhere on the roster: kind "new", the typed name as
// value, no company/trade picked.
func omodelNewRow(name string) passengerRow {
	return passengerRow{Kind: "new", Value: name, Typed: name}
}

// omodelLandingPage follows a 303 as the same logged-in officer.
func omodelLandingPage(t *testing.T, w *orientBlockWorld, loc string) string {
	t.Helper()
	rec := w2Get(t, w.srv, loc, loginCookie(t, w.srv, testLoginPassword))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", loc, rec.Code, rec.Body.String())
	}
	return html.UnescapeString(rec.Body.String())
}

// omodelText is a served body with html/template's escaping undone, for
// assertions on officer copy (an apostrophe in a Go string renders as &#39;).
func omodelText(s string) string { return html.UnescapeString(s) }

// --- 1. intake at an orientation post enrols AND records the orientation --

func TestIntakeAtAnOrientationPostRecordsThatPostsOrientationWithTheEnrolment(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	co := omodelCompany(t, w)

	// The form says what it does before it is submitted.
	page := omodelLandingPage(t, w, "/roster")
	if !strings.Contains(page, "record Pad Site Control's orientation") {
		t.Errorf("the intake form at an orientation post does not say it records the orientation:\n%s", page)
	}

	rec := postForm(t, w.srv, "/roster/intake", intakeForm2("Newby", "Nora", co.CompanyID, "", "", ""))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /roster/intake = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	people := personNamed(t, w.srv, "Newby, Nora")
	if len(people) != 1 {
		t.Fatalf("%d rows for Newby, Nora after one enrolment, want 1", len(people))
	}
	held, err := domain.OrientationHeld(w.st, people[0].PersonID, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if held == nil {
		t.Fatalf("Newby, Nora was ENROLLED at an orientation post with NO orientation row for it — the new-person form is the orientation form (R-12); nothing may be placed in the system without one")
	}
	if held.RecordedBy == nil || *held.RecordedBy != "Pad Site Control" {
		t.Errorf("the completion is attributed to %v, want this post (D-5)", held.RecordedBy)
	}
	// Exactly ONE completion: enrol-and-orient is two events, never three.
	if all, err := domain.OrientationsOf(w.st, people[0].PersonID); err != nil || len(all) != 1 {
		t.Errorf("OrientationsOf(Nora) = %+v (%v), want exactly the one site-b row", all, err)
	}
	// The confirmation says so in the past tense, and the marking page
	// lists her as holding it.
	landing := omodelLandingPage(t, w, rec.Header().Get("Location"))
	if !strings.Contains(landing, "ENROLLED") || !strings.Contains(landing, "Pad Site Control's orientation recorded") {
		t.Errorf("the enrolment confirmation does not say the orientation was recorded:\n%s", landing)
	}
	orient := omodelLandingPage(t, w, "/orientation")
	if !strings.Contains(orient, "Hold Pad Site Control's orientation (3)") {
		t.Errorf("/orientation does not count Nora among the holders (Dana, Orla + Nora = 3):\n%s", orient)
	}
}

// --- 2. the gate's add-on-demand slot is refused at an orientation post ---

func TestGateAddOnDemandIsRefusedAtAnOrientationPostAndAuthorsNothing(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	before, count := omodelWatermark(t, w), omodelPersonCount(t, w)

	// The live form: two known people and one the roster does not have.
	body := scriptedCheckInBody("TRK-90", 0, append(orientBlockRows(w.driver, w.orla), omodelNewRow("Walkin Wanda")))
	rec := postForm(t, w.srv, "/gate/checkin", body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("POST /gate/checkin with an off-roster name at an orientation post = %d, want 409 (refused): %s", rec.Code, rec.Body.String())
	}
	page := omodelText(rec.Body.String())
	for _, want := range []string{
		"Walkin Wanda",
		"is not on the roster",
		"Nobody is added to the system without an orientation",
		"Pad Site Control's orientation",
		"/roster",
		"Nothing was recorded",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the refusal lacks %q — the officer must be told who, why and what to do next", want)
		}
	}
	// The list is intact: the two known names are still on the re-rendered form.
	for _, name := range []string{"Dana Driver", "Orla Oriented"} {
		if !strings.Contains(page, name) {
			t.Errorf("the refused form lost %s from the accumulated list", name)
		}
	}
	if got := omodelPersonCount(t, w); got != count {
		t.Fatalf("the refused check-in CREATED a person: roster %d → %d", count, got)
	}
	if after := omodelWatermark(t, w); after != before {
		t.Fatalf("the refused check-in authored: watermark %d → %d (a person, company, trade or vehicle event landed before the refusal)", before, after)
	}
	if open := orientBlockOpenPersonIDs(t, w.st); len(open) != 0 {
		t.Fatalf("the refused check-in entered somebody: open visits %v", open)
	}

	// The legacy body (a stale tab): the same door, the same answer.
	legacy := url.Values{
		"vehicle": {"TRK-91"}, "driver_kind": {"new"}, "driver_value": {"Walkin Wanda"},
		"time_source": {"default"},
	}
	rec = postForm(t, w.srv, "/gate/checkin", legacy)
	if rec.Code != http.StatusConflict {
		t.Fatalf("legacy POST /gate/checkin with a new driver at an orientation post = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(omodelText(rec.Body.String()), "Nobody is added to the system without an orientation") {
		t.Errorf("the legacy body's refusal does not carry the ruling's copy:\n%s", rec.Body.String())
	}
	if got := omodelPersonCount(t, w); got != count {
		t.Fatalf("the legacy body CREATED a person: roster %d → %d", count, got)
	}
	if after := omodelWatermark(t, w); after != before {
		t.Fatalf("the legacy body authored: watermark %d → %d", before, after)
	}
}

// --- 3. intake's duplicate refusal offers the tick (R-10 + the tick) -------

func TestIntakeDuplicateRefusalOffersToTickThisPostsOrientationForTheExistingPerson(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	co := omodelCompany(t, w)
	// Enrolled at the road post (site-a's stream, replicated in), as the
	// ruling's own example: in the overall system, not oriented here.
	sitey, err := domain.CreatePerson(w.st, "site-a", 1, "Cross, Sitey", &co.CompanyID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	count := omodelPersonCount(t, w)

	rec := postForm(t, w.srv, "/roster/intake", intakeForm2("Cross", "Sitey", co.CompanyID, "", "", ""))
	if rec.Code != http.StatusConflict {
		t.Fatalf("intake of a name already in the system = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	page := omodelText(rec.Body.String())
	offer := `<form method="post" action="/orientation/complete"`
	if !strings.Contains(page, offer) {
		t.Fatalf("the duplicate refusal at an orientation post offers no tick against the existing person — the officer is left to find them on another screen:\n%s", page)
	}
	if !strings.Contains(page, `name="person_id" value="`+sitey.PersonID+`"`) {
		t.Errorf("the offer does not name the existing record's id")
	}
	for _, want := range []string{"Same person", "Cross, Sitey", "Northwind Drilling", "site-a", "record Pad Site Control's orientation"} {
		if !strings.Contains(page, want) {
			t.Errorf("the offer lacks %q — R-10: the confirmation shows WHO, their COMPANY and where they were enrolled", want)
		}
	}
	if !strings.Contains(page, `name="same_name"`) {
		t.Errorf("the different-person affirmation is gone from the refusal — a genuine second person of the same name needs it")
	}
	if got := omodelPersonCount(t, w); got != count {
		t.Fatalf("the refusal itself created a person: %d → %d", count, got)
	}

	// The officer confirms: the tick lands against the EXISTING record and
	// enrols nobody.
	rec, _ = w2PostForm(t, w.srv, "/orientation/complete", url.Values{"person_id": {sitey.PersonID}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /orientation/complete from the offer = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	if held, err := domain.OrientationHeld(w.st, sitey.PersonID, "site-b"); err != nil || held == nil {
		t.Fatalf("Cross, Sitey does not hold site-b's orientation after the tick (held=%v, err=%v)", held, err)
	}
	if got := omodelPersonCount(t, w); got != count {
		t.Fatalf("the tick created a person: %d → %d — a tick is against a PREEXISTING person, never a re-entry", count, got)
	}

	// Now that they hold it, the same refusal offers no tick for them — and
	// says why — while still offering the different-person affirmation.
	rec = postForm(t, w.srv, "/roster/intake", intakeForm2("Cross", "Sitey", co.CompanyID, "", "", ""))
	if rec.Code != http.StatusConflict {
		t.Fatalf("second intake of the same name = %d, want 409", rec.Code)
	}
	page = omodelText(rec.Body.String())
	if strings.Contains(page, `name="person_id" value="`+sitey.PersonID+`"`) {
		t.Errorf("the refusal offers to tick a person who ALREADY holds this post's orientation")
	}
	if !strings.Contains(page, "already holds Pad Site Control's orientation") {
		t.Errorf("the refusal does not say the candidate already holds it:\n%s", page)
	}
	if !strings.Contains(page, `name="same_name"`) {
		t.Errorf("the different-person affirmation is missing on the second refusal")
	}
}

func TestIntakeDuplicateRefusalOffersNoTickAtARoadPost(t *testing.T) {
	w := newOrientBlockWorld(t, false)
	co := omodelCompany(t, w)
	if _, err := domain.CreatePerson(w.st, "site-a", 1, "Cross, Sitey", &co.CompanyID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	rec := postForm(t, w.srv, "/roster/intake", intakeForm2("Cross", "Sitey", co.CompanyID, "", "", ""))
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate intake at a road post = %d, want 409", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `action="/orientation/complete"`) {
		t.Fatalf("a road post offered an orientation tick — the route is not even registered there (D-88)")
	}
	if !strings.Contains(rec.Body.String(), `name="same_name"`) {
		t.Errorf("the road post's duplicate refusal lost the different-person affirmation")
	}
}

// --- 4. a road post keeps creating people, owing no orientation -----------

func TestARoadPostStillEnrolsAndAddsOnDemandOwingNoOrientation(t *testing.T) {
	w := newOrientBlockWorld(t, false)
	co := omodelCompany(t, w)

	page := omodelLandingPage(t, w, "/roster")
	if strings.Contains(page, "record Pad Site Control's orientation") {
		t.Errorf("a road post's intake form claims to record an orientation it does not administer")
	}
	rec := postForm(t, w.srv, "/roster/intake", intakeForm2("Newby", "Nora", co.CompanyID, "", "", ""))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("road-post intake = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	nora := personNamed(t, w.srv, "Newby, Nora")
	if len(nora) != 1 {
		t.Fatalf("%d rows for Newby, Nora, want 1", len(nora))
	}
	if all, err := domain.OrientationsOf(w.st, nora[0].PersonID); err != nil || len(all) != 0 {
		t.Fatalf("a road post recorded an orientation: %+v (%v)", all, err)
	}
	if landing := omodelLandingPage(t, w, rec.Header().Get("Location")); strings.Contains(landing, "orientation recorded") {
		t.Errorf("a road post's confirmation claims an orientation was recorded")
	}

	// The gate: an off-roster name is added and ENTERED, as it always was.
	// ⟦R-26⟧ this test is about the road post's own add-on-demand, not
	// company — give Wanda one so the commit is not refused for the reason
	// R-26 added.
	count := omodelPersonCount(t, w)
	wandaRow := omodelNewRow("Walkin Wanda")
	wandaRow.CompanyID, wandaRow.Company = newRef, "R-26 Test Co"
	rec = postForm(t, w.srv, "/gate/checkin", scriptedCheckInBody("TRK-92", 0, append(orientBlockRows(w.driver), wandaRow)))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("road-post gate check-in with an off-roster name = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	if got := omodelPersonCount(t, w); got != count+1 {
		t.Fatalf("the road post did not add the off-roster person: roster %d → %d", count, got)
	}
	wanda := personNamed(t, w.srv, "Walkin Wanda")
	if len(wanda) != 1 || !orientBlockOpenPersonIDs(t, w.st)[wanda[0].PersonID] {
		t.Fatalf("Walkin Wanda was not entered at the road post (rows=%d, open=%v)", len(wanda), orientBlockOpenPersonIDs(t, w.st))
	}
}

// --- 5. the cross-site tick, through the doors ------------------------------

func TestAPersonOrientedElsewhereIsHeldOutUntilThisPostTicksItsOwnBox(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	// In the overall system: enrolled and oriented at post C (a third work
	// site's stream, replicated in). Not oriented at B.
	sitey, err := domain.CreatePerson(w.st, "site-c", 1, "Cross, Sitey", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.CompleteOrientation(w.st, "site-c", 1, sitey.PersonID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	row := passengerRow{Kind: "existing", Value: sitey.PersonID, Typed: sitey.Name}

	// Arrives at B: held out — C's orientation is not B's.
	rec := postForm(t, w.srv, "/gate/checkin", scriptedCheckInBody("TRK-93", 0, append(orientBlockRows(w.driver), row)))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("first check-in at B = %d: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, notEnteredQueryKey+"="+sitey.PersonID) {
		t.Fatalf("Cross, Sitey (oriented at site-c) was NOT held out at site-b: redirect %q", loc)
	}
	if orientBlockOpenPersonIDs(t, w.st)[sitey.PersonID] {
		t.Fatalf("Cross, Sitey has an open visit at B on the strength of C's orientation")
	}
	held, err := domain.HeldOutAt(w.st, "site-b")
	if err != nil || len(held) != 1 || held[0].PersonID != sitey.PersonID {
		t.Fatalf("HeldOutAt(site-b) = %+v (%v), want exactly Sitey", held, err)
	}

	// B ticks ITS box against the existing record — a tick, not a re-entry.
	count := omodelPersonCount(t, w)
	rec, _ = w2PostForm(t, w.srv, "/orientation/complete", url.Values{"person_id": {sitey.PersonID}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /orientation/complete = %d: %s", rec.Code, rec.Body.String())
	}
	if got := omodelPersonCount(t, w); got != count {
		t.Fatalf("the tick created a person: %d → %d", count, got)
	}
	all, err := domain.OrientationsOf(w.st, sitey.PersonID)
	if err != nil {
		t.Fatal(err)
	}
	sites := map[string]bool{}
	for _, o := range all {
		sites[o.SiteID] = true
	}
	if !sites["site-b"] || !sites["site-c"] || len(sites) != 2 {
		t.Fatalf("after B's tick Sitey holds %v, want exactly {site-b, site-c} — one flag per (person, site), C's untouched", sites)
	}

	// Checked in again at B: entered, and gone from the held-out list.
	rec = postForm(t, w.srv, "/gate/checkin", scriptedCheckInBody("TRK-94", 0, append(orientBlockRows(w.driver), row)))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("second check-in at B = %d: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); strings.Contains(loc, notEnteredQueryKey+"=") {
		t.Fatalf("still held out after B's tick: %q", loc)
	}
	if !orientBlockOpenPersonIDs(t, w.st)[sitey.PersonID] {
		t.Fatalf("Cross, Sitey is not on site at B after B's tick and a second check-in")
	}
	if held, err := domain.HeldOutAt(w.st, "site-b"); err != nil || len(held) != 0 {
		t.Fatalf("HeldOutAt(site-b) after entry = %+v (%v), want empty", held, err)
	}
	if muster := orientBlockGet(t, w.srv, "/muster"); !strings.Contains(muster, "Cross, Sitey") {
		t.Errorf("Cross, Sitey is not on B's muster after entry")
	}
}
