package web

// fix3_d_retired_test.go — fix round 3, stream D (2026-10-04): the
// retired-name rulings and seams. Each test fails on e3d507d and passes
// after; the finding or ruling it pins is in its name.

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// fix3dPersonCount is the roster size, retired included.
func fix3dPersonCount(t *testing.T, st *store.Store) int {
	t.Helper()
	all, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	return len(all)
}

// retiredAmendPersonIDOf is the amend-link id a gate form would render
// for err (setVehicleFormError's own reading).
func retiredAmendPersonIDOf(err error) string {
	var f vehicleForm
	(&Server{}).setVehicleFormError(&f, err)
	return f.ErrorAmendPersonID
}

// fix3dSpellings is L12-2's Q-FG1 probe set against a retired "Ashton, Joss":
// refused is what James's ruling says each must do.
var fix3dSpellings = []struct {
	typed   string
	refused bool
}{
	{"Ashton,  Joss", true}, // double space — displays identically (HTML collapses it)
	{"Ashton,Joss", true},   // no space after the comma
	{"Ashton Joss", true},   // no comma
	{"Joss Ashton", false},  // word order swapped — a different spelling, not refused
}

// Q-FG1 (James, 2026-10-04: the retired-name check normalises whitespace and
// comma spacing), the typed (scripting-off) path: the gate's typed-name
// resolver refuses three of L12's four spellings with the retired refusal,
// and lets the word-order swap through as a new name.
func TestFix3QFG1TypedRetiredCheckNormalisesWhitespaceAndCommas(t *testing.T) {
	yes := "true"
	ret := domain.Person{PersonID: "p-ret", Name: "Ashton, Joss", Retired: &yes}
	for _, c := range fix3dSpellings {
		slot, err := resolveTypedPerson(c.typed, []domain.Person{ret})
		got := err != nil && strings.Contains(err.Error(), "RETIRED")
		if got != c.refused {
			t.Errorf("%q: retired refusal = %v (slot %+v, err %v), want %v", c.typed, got, slot, err, c.refused)
		}
		if got && retiredAmendPersonIDOf(err) != ret.PersonID {
			t.Errorf("%q: the refusal's amend id = %q, want %q", c.typed, retiredAmendPersonIDOf(err), ret.PersonID)
		}
	}
	// A live record carrying the exact typed spelling is adopted, as before.
	live := domain.Person{PersonID: "p-live", Name: "Ashton,Joss"}
	if slot, err := resolveTypedPerson("Ashton,Joss", []domain.Person{ret, live}); err != nil || slot.ExistingID != live.PersonID {
		t.Errorf("an exact live namesake: %+v, %v — want it adopted", slot, err)
	}
}

// Q-FG1, the commit path (domain.refuseRetiredNamesake, checkin.go): a
// "No — this is a different person" decline posted for each spelling. Three
// are refused and mint nothing; the swap mints. One normaliser serves both
// this and the typed path above.
func TestFix3QFG1StaleDeclineSpellingsOfARetiredNameMintNothing(t *testing.T) {
	srv, st := w5fixServer(t, false)
	joss := orientBlockPerson(t, st, "Ashton, Joss")
	w5fixRetire(t, srv, joss.PersonID)
	for i, c := range fix3dSpellings {
		before := fix3dPersonCount(t, st)
		decline := []passengerRow{{Kind: "new", Value: c.typed, Typed: c.typed, CompanyID: newRef, Company: "Boreal Camp Catering"}}
		rec := postForm(t, srv, "/gate/checkin", scriptedCheckInBody("TRK-95"+string(rune('0'+i)), 0, decline))
		after := fix3dPersonCount(t, st)
		if c.refused {
			if rec.Code == http.StatusSeeOther || after != before {
				t.Errorf("%q: %d, roster %d -> %d — want refused with nothing minted", c.typed, rec.Code, before, after)
				continue
			}
			if !strings.Contains(rec.Body.String(), "RETIRED") {
				t.Errorf("%q: the refusal does not say the name is retired: %s", c.typed, w5fixNotices(rec.Body.String(), "notice-critical"))
			}
		} else if rec.Code != http.StatusSeeOther || after != before+1 {
			t.Errorf("%q (word order swapped): %d, roster %d -> %d — want accepted as a new person: %s", c.typed, rec.Code, before, after, w5fixNotices(rec.Body.String(), "notice-critical"))
		}
	}
}

// F12 (James, 2026-10-04: the L02 amend link must appear on EVERY
// retired-name refusal surface), the JS-on vehicle door: a scripted row for a
// retired name arrives as new_kind=new and was refused by the domain's own
// sentence with no link (L12-5). It now carries the same sentence and link
// as the typed refusal.
func TestFix3F12ScriptedVehicleDoorRetiredRefusalCarriesTheAmendLink(t *testing.T) {
	srv, st := w5fixServer(t, false)
	joss := orientBlockPerson(t, st, "Ashton, Joss")
	w5fixRetire(t, srv, joss.PersonID)
	row := []passengerRow{{Kind: "new", Value: joss.Name, Typed: joss.Name, CompanyID: newRef, Company: "Boreal Camp Catering"}}
	rec := postForm(t, srv, "/gate/checkin", scriptedCheckInBody("TRK-960", 0, row))
	if rec.Code == http.StatusSeeOther {
		t.Fatalf("the retired name was checked in (%s)", rec.Header().Get("Location"))
	}
	body := rec.Body.String()
	if !strings.Contains(body, `href="/person/amend?person_id=`+joss.PersonID+`"`) {
		t.Errorf("F12: no amend link for %s on the vehicle door's retired refusal: %s", joss.PersonID, w5fixNotices(body, "notice-critical"))
	}
	if !strings.Contains(body, "this entry does not check them in") {
		t.Errorf("F12: not the one retired sentence: %s", w5fixNotices(body, "notice-critical"))
	}
}

// F12, the individual gate's stale decline: a tab opened before the retire
// declines the namesake ("No — this is a different person"); R6 refuses it
// at commit, and the refusal now carries the amend link.
func TestFix3F12StaleDeclineAtTheIndividualGateCarriesTheAmendLink(t *testing.T) {
	srv, st := w5fixServer(t, false)
	joss := orientBlockPerson(t, st, "Ashton, Joss")
	vals := singleCheckInForm(t, srv, joss.Name) // the form, loaded BEFORE the retire
	w5fixRetire(t, srv, joss.PersonID)
	vals.Set("new_kind", "new")
	vals.Set("new_value", joss.Name)
	vals.Set("new_company_id", newRef)
	vals.Set("new_company", "Boreal Camp Catering")
	before := fix3dPersonCount(t, st)
	rec := postForm(t, srv, "/gate/checkin/single", vals)
	if rec.Code == http.StatusSeeOther {
		t.Fatalf("the stale decline was accepted (%s)", rec.Header().Get("Location"))
	}
	if n := fix3dPersonCount(t, st); n != before {
		t.Errorf("roster %d -> %d: the stale decline minted a namesake", before, n)
	}
	if !strings.Contains(rec.Body.String(), `href="/person/amend?person_id=`+joss.PersonID+`"`) {
		t.Errorf("F12: no amend link on the stale-decline refusal: %s", w5fixNotices(rec.Body.String(), "notice-critical"))
	}
}

// F12, /orientation/complete: the stale-retired refusal named "the amend
// page" with no link (L12-4). It now links each retired person's record.
func TestFix3F12StaleRetiredOrientationRefusalLinksTheAmendPage(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	w5fixRetire(t, w.srv, w.uma.PersonID)
	rec := postForm(t, w.srv, "/orientation/complete", url.Values{"person_id": {w.uma.PersonID}})
	if rec.Code == http.StatusSeeOther {
		t.Fatalf("the stale tick for a retired person was recorded (%s)", rec.Header().Get("Location"))
	}
	if !strings.Contains(rec.Body.String(), `href="/person/amend?person_id=`+w.uma.PersonID+`"`) {
		t.Errorf("F12: no amend link on the stale-retired orientation refusal: %s", w5fixNotices(rec.Body.String(), "notice-warn"))
	}
}

// F12, the vehicle door's "Possibly already on the roster" offers: a retired
// record was offered ("Use Ashton, Joss …") and then refused when used (L12
// obs). The inbound offers omit it.
func TestFix3F12VehicleDoorDuplicateOffersOmitRetiredRecords(t *testing.T) {
	srv, st := w5fixServer(t, false)
	joss := orientBlockPerson(t, st, "Ashton, Joss")
	w5fixRetire(t, srv, joss.PersonID)
	f := vehicleForm{Direction: gateInbound, Rows: []passengerRow{{Kind: "new", Value: "Ashton, Joss", Typed: "Ashton, Joss"}}}
	srv.decorateRows(&f, map[string]string{}, map[string]string{})
	for _, c := range f.Rows[0].Candidates {
		if c.PersonID == joss.PersonID {
			t.Errorf("F12: the inbound row offers the RETIRED record %s (%s)", c.PersonID, c.Name)
		}
	}
}

// Q-R5-3 (James, 2026-10-04: JS-off same-name lists show the origin post):
// two records called "Testpair, Alex", one added here and one at Ice Road,
// with the same (empty) company and trade. ⟦was: the two "Use …" offers and
// the two on-site rows differed only by "person id …tail" (L11-2d)⟧. Each
// now says which post added it, by display name, beside the id tail.
func TestFix3QR53SameNameRowsShowTheirOriginPostWithScriptingOff(t *testing.T) {
	srv, st := w5fixServer(t, false)
	here := orientBlockPerson(t, st, "Testpair, Alex")
	there, err := domain.CreatePerson(st, "site-a", 1, "Testpair, Alex", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	// The vehicle door's "Possibly already on the roster" offers.
	_, page := getGate(t, srv)
	page = gateSetVehicle(t, srv, page, gateInbound, "TRK-971")
	page = gateAdd(t, srv, page, gateInbound, "Testpair, Alex")
	offers := fix3dGrep(page, `class="p-pick"`)
	for _, want := range []string{
		`value="0:` + here.PersonID + `">Use Testpair, Alex`,
		`value="0:` + there.PersonID + `">Use Testpair, Alex`,
		"(added at Pad Site Control, person id",
		"(added at Ice Road, person id",
	} {
		if !strings.Contains(offers, want) {
			t.Errorf("Q-R5-3: the JS-off offers lack %q:\n%s", want, offers)
		}
	}

	// The on-site list (checked in, both): the flat roster table.
	rec := postForm(t, srv, "/gate/checkin", scriptedCheckInBody("TRK-970", 0, orientBlockRows(here, there)))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("checking both in = %d: %s", rec.Code, w5fixNotices(rec.Body.String(), "notice-critical"))
	}
	_, gate := getGate(t, srv)
	for _, want := range []string{"(added at Pad Site Control, person id", "(added at Ice Road, person id"} {
		if !strings.Contains(gate, want) {
			t.Errorf("Q-R5-3: the on-site list lacks %q:\n%s", want, fix3dGrep(gate, "roster-ref"))
		}
	}
}

// fix3dGrep returns the lines of page containing needle, for a failure message.
func fix3dGrep(page, needle string) string {
	var out []string
	for _, l := range strings.Split(page, "\n") {
		if strings.Contains(l, needle) {
			out = append(out, strings.TrimSpace(l))
		}
	}
	return strings.Join(out, "\n")
}

// Q-R5-3, the typed (scripting-off) refusal for a name two live records
// carry: it named them by raw person id (L11-2d (a)). It now names the post
// that added each, by display name, and no id.
func TestFix3QR53AmbiguousTypedNameRefusalNamesOriginPostsNotIDs(t *testing.T) {
	srv, st := w5fixServer(t, false)
	here := orientBlockPerson(t, st, "Testpair, Alex")
	there, err := domain.CreatePerson(st, "site-a", 1, "Testpair, Alex", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	vals := singleCheckInForm(t, srv, "Testpair, Alex")
	vals.Set("new_company_id", newRef)
	vals.Set("new_company", "Boreal Camp Catering")
	rec := postForm(t, srv, "/gate/checkin/single", vals)
	if rec.Code == http.StatusSeeOther {
		t.Fatalf("an ambiguous typed name was checked in (%s)", rec.Header().Get("Location"))
	}
	notice := fix3dGrep(rec.Body.String(), "vehicle-error")
	if !strings.Contains(notice, "added at Pad Site Control") || !strings.Contains(notice, "added at Ice Road") {
		t.Errorf("Q-R5-3: the ambiguous-name refusal does not name the posts that added each record: %s", notice)
	}
	for _, id := range []string{here.PersonID, there.PersonID} {
		if strings.Contains(notice, id) {
			t.Errorf("Q-R5-3: the refusal still quotes the raw person id %s: %s", id, notice)
		}
	}
}

// F11 — the D22 residual (L14-4). A REAL sealed confirmation of another kind
// read "Recorded, but the outcome could not be determined" on a page where
// nothing was recorded: (1) any other-kind event key on /arrivals/resolve
// and /arrivals/unref, which had no other-kind arm; (2) /trade's sealed
// trade-id confirmation on every amend page and the arrivals pages, because
// a trade id is not an event key and so missed recordedOtherKind. Each now
// reads NOTHING TO CONFIRM.
func TestFix3F11SealedConfirmationOfAnotherKindIsNothingToConfirm(t *testing.T) {
	w := newFJWorld(t)
	rec := postForm(t, w.srv, "/trade/create", url.Values{"name": {"Fix3 Rigging"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("trade create = %d: %s", rec.Code, rec.Body.String())
	}
	tradeLoc := rec.Header().Get("Location")
	if !strings.Contains(tradeLoc, "recorded=site-b-trade-") {
		t.Fatalf("precondition: the trade create's sealed redirect does not carry a trade id: %s", tradeLoc)
	}
	probes := []string{}
	for _, page := range []string{"/person/amend", "/company/amend", "/vehicle/amend", "/time/correct", resolvePath, unrefPath} {
		probes = append(probes, page+"?"+fjQueryOf(t, tradeLoc))
	}
	for _, page := range []string{resolvePath, unrefPath} {
		probes = append(probes, page+"?"+fjQueryOf(t, w.personLoc), page+"?"+fjQueryOf(t, w.companyLoc))
	}
	for _, probe := range probes {
		got := fjText(w3Get(t, w.srv, probe))
		if !strings.Contains(got, "nothing to confirm") {
			t.Errorf("F11: %s does not say NOTHING TO CONFIRM", probe)
		}
		if strings.Contains(got, "could not be determined") {
			t.Errorf("F11: %s still says \"Recorded, but the outcome could not be determined\"", probe)
		}
	}
}

// A-03: /gate and /gate/vehicle had no <h1> at all (axe page-has-heading-one).
// Each now has exactly one, visually hidden, naming the post by display name.
func TestFix3A03GatePagesHaveExactlyOneH1(t *testing.T) {
	srv, _ := w5fixServer(t, false)
	_, individual := getGateIndividual(t, srv)
	_, vehicle := getGate(t, srv)
	for page, body := range map[string]string{"/gate": individual, "/gate/vehicle": vehicle} {
		if n := strings.Count(body, "<h1"); n != 1 {
			t.Errorf("A-03: %s has %d <h1>, want exactly 1", page, n)
		}
		if !strings.Contains(body, `<h1 class="sr-only">Pad Site Control gate`) {
			t.Errorf("A-03: %s's <h1> does not name the post by display name:\n%s", page, fix3dGrep(body, "<h1"))
		}
	}
}

// F16 / F-L03-R4-3 (L13-7a): with scripting on, a vehicle-door row retyped
// in place to another roster person's exact name lost its NOT ORIENTED HERE
// block, although the commit checks the row in AS that person. The served
// script now rebuilds the block for the person the typed text resolves to
// (exactly one live roster hit, never a deliberate "different person" row).
func TestFix3F16RetypedRowKeepsTheNotOrientedWarning(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	page := orientBlockGet(t, w.srv, "/gate/vehicle")
	i := strings.Index(page, "function showNew(row, reset)")
	if i < 0 {
		t.Fatal("precondition: no showNew in the served script")
	}
	showNew := page[i:]
	if j := strings.Index(showNew, "\n    }\n"); j > 0 {
		showNew = showNew[:j]
	}
	if !strings.Contains(showNew, "syncOrientRow(row, exactRosterHit(row));") {
		t.Errorf("F-L03-R4-3: showNew still drops the orientation block on a retyped row:\n%s", showNew)
	}
	for _, want := range []string{
		"function exactRosterHit(row)",
		"row._offRosterAs === t) return null", // a deliberate new person gets no block
		"return hits === 1 ? hit : null",      // an ambiguous name gets no block
	} {
		if !strings.Contains(page, want) {
			t.Errorf("F-L03-R4-3: the served script lacks %q", want)
		}
	}
}

// F16 / L17-15 (L13-7b): a person still recorded aboard ANOTHER post's open
// run (here site-a → site-c) was checked in at this third post with no
// word, and sat on this muster and on the run's sheet at once. The check-in
// still succeeds — presence is never refused — and the gate now WARNS, naming
// the run and the posts to radio. A run bound for THIS post is its own
// arrival, not this question, and warns nothing.
func TestFix3F16CheckInOfSomeoneAboardAnotherPostsRunWarns(t *testing.T) {
	srv, st := w5fixServer(t, false)
	gray := orientBlockPerson(t, st, "Fenham, Gray")
	nell := orientBlockPerson(t, st, "Branham, Nell")
	deadline := int64(65)
	for _, run := range []struct {
		who  domain.Person
		dest string
		veh  string
	}{{gray, "site-c", "TRK-19"}, {nell, "site-b", "TRK-20"}} {
		dest := run.dest
		if _, err := domain.Dispatch(st, "site-a", 1, domain.DispatchInput{
			VehicleID: w7SeedVehicleID(t, st, "site-a", run.veh), DriverPersonID: run.who.PersonID,
			DestinationSite: &dest, DeadlineMinutes: &deadline,
		}, 0, "default"); err != nil {
			t.Fatalf("dispatching %s: %v", run.veh, err)
		}
	}
	rec := postForm(t, srv, "/gate/checkin", scriptedCheckInBody("TRK-980", 0, orientBlockRows(gray, nell)))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("L17-15 is a warning, not a refusal: check-in = %d: %s", rec.Code, w5fixNotices(rec.Body.String(), "notice-critical"))
	}
	page := w3Get(t, srv, rec.Header().Get("Location"))
	warn := fix3dGrep(page, "aboard-warning")
	if !strings.Contains(warn, "ALSO ABOARD AN OPEN RUN") || !strings.Contains(warn, "Fenham, Gray") || !strings.Contains(warn, "TRK-19") || !strings.Contains(warn, "Ice Road") {
		t.Errorf("L17-15: no warning naming Fenham, Gray aboard TRK-19 from Ice Road:\n%s", warn)
	}
	if strings.Contains(warn, "Branham, Nell") {
		t.Errorf("a run bound for THIS post is its own arrival, not another post's run — it must not warn:\n%s", warn)
	}
}

// Copy (from stream C's sweep): the individual gate's name-length refusal
// read "Nothing was recorded. the name typed is …" — a sentence opening in
// lower case. It now opens with a capital.
func TestFix3GateOverlongNameRefusalOpensWithACapital(t *testing.T) {
	srv, _ := w5fixServer(t, false)
	vals := singleCheckInForm(t, srv, strings.Repeat("Z", 130))
	vals.Set("new_company_id", newRef)
	vals.Set("new_company", "Boreal Camp Catering")
	rec := postForm(t, srv, "/gate/checkin/single", vals)
	notice := fix3dGrep(rec.Body.String(), "vehicle-error")
	if !strings.Contains(notice, "Nothing was recorded.</strong> The name typed is 130 characters long") {
		t.Errorf("the name-length refusal does not open with a capital: %s", notice)
	}
}

// F13 (routed from stream E): the /arrivals/unref footer said "the origin is
// refused", which is false for an arrival the origin recorded with "Arrived
// back here" — that post recorded it, so it may withdraw it (D-87).
func TestFix3UnrefFooterDoesNotSayTheOriginIsRefused(t *testing.T) {
	srv, _ := w5fixServer(t, false)
	text := fjText(w3Get(t, srv, unrefPath))
	if strings.Contains(text, "the origin is refused") {
		t.Errorf("the footer still says the origin is refused")
	}
	// ⟦fix4 H, 2026-10-05: the decision id left the officer's copy — was "… its reference (d-87). that is …"⟧
	want := "only the post that recorded the arrival may withdraw its reference. that is usually the destination, but it is the origin for an arrival recorded there with “arrived back here”. any other post is refused, so if the mistake is found elsewhere, radio the post that recorded the arrival."
	if !strings.Contains(text, want) {
		t.Errorf("the footer lacks the corrected wording:\n%s", text)
	}
}
