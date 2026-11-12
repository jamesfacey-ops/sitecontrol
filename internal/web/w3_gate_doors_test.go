// w3_gate_doors_test.go — fork stream W3 ("gate doors"), 2026-10-03.
//
// One test (or one small group) per item of FIX-PLAN §1/§3 assigned to W3,
// each named for its finding. Every test here was run RED against the
// branch base (fork/next a982de3 + fork/boxes) before the fix it pins, and
// GREEN after. The script-side items are shape scans of the served page —
// this module carries no JavaScript runtime, by decision (see
// gate_adopt_confirm_test.go's header); their browser half was driven live
// on lane L10.
package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"sitecontrol/internal/domain"
)

// w3Get is an AUTHENTICATED GET through the real handler.
func w3Get(t *testing.T, srv *Server, path string) string {
	t.Helper()
	cookie := loginCookie(t, srv, testLoginPassword)
	req := httptest.NewRequest("GET", path, nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec.Body.String()
}

// w3AnonGet is a GET with no session at all.
func w3AnonGet(t *testing.T, srv *Server, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec.Body.String()
}

// --- M-1 / G-028 ---------------------------------------------------------

// TestW3ConcurrentIdenticalNewPersonCheckInsMintOnePerson: L01/L02 sent 3-6
// identical "new person" check-ins in parallel and got one person PER POST,
// each with an open visit — one human, several muster lines. The find (is
// anybody called this?) ran outside the transaction that minted. Serialised,
// the first mints and every later one finds that person and re-checks them
// in: one person, one open visit.
func TestW3ConcurrentIdenticalNewPersonCheckInsMintOnePerson(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	const name = "Zqrace, Eta"
	vals := singleCheckInForm(t, srv, name)
	vals.Set("new_company_id", newRef)
	vals.Set("new_company", "Race Test Co")
	body := vals.Encode()
	cookie := loginCookie(t, srv, testLoginPassword)

	const n = 8
	var start, done sync.WaitGroup
	start.Add(1)
	codes := make([]int, n)
	for i := 0; i < n; i++ {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			req := httptest.NewRequest("POST", "/gate/checkin/single", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			start.Wait()
			srv.Handler().ServeHTTP(rec, req)
			codes[i] = rec.Code
		}(i)
	}
	start.Done()
	done.Wait()
	for i, c := range codes {
		if c != http.StatusSeeOther {
			t.Fatalf("concurrent check-in %d answered %d, want 303", i, c)
		}
	}

	all, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, p := range all {
		if p.Name == name {
			ids = append(ids, p.PersonID)
		}
	}
	if len(ids) != 1 {
		t.Fatalf("%d concurrent identical check-ins minted %d persons called %q, want 1 (M-1/G-028)", n, len(ids), name)
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	visits := 0
	for _, v := range open {
		if v.PersonID == ids[0] {
			visits++
		}
	}
	if visits != 1 {
		t.Fatalf("%s holds %d open visits after %d identical check-ins, want exactly 1", name, visits, n)
	}
}

// --- M-7 / G-049, G-126 ---------------------------------------------------

var w3NoticeTexts = []string{
	"NOT ENTERED — NO ORIENTATION",
	"ORIENTATION NOT RECORDED — TICK DID NOT APPLY",
	"POSSIBLE EXISTING MATCH",
	"CHECK THE RECORDED TIME",
	"more than 30 days in the past",
	"arrived:",
	"Tick exactly one person on the roster",
}

func w3NoNotice(t *testing.T, page, what string) {
	t.Helper()
	for _, n := range w3NoticeTexts {
		if strings.Contains(page, n) {
			t.Errorf("%s: a notice rendered off a URL nobody's commit wrote — %q found", what, n)
		}
	}
}

// TestW3ForgedNoticeQueriesRenderNothing: L02 G-049 / L03 G-126 typed the
// notice keys into the address bar and the gate rendered them — NOT ENTERED
// for somebody on site, a duplicate advisory, a stale-tick notice, a time
// warning, and (anonymously) "FAKE-99 arrived: 42 checked in".
func TestW3ForgedNoticeQueriesRenderNothing(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	id := w.uma.PersonID
	forged := []string{
		"/gate?not_entered=" + id,
		"/gate/vehicle?not_entered=" + id,
		"/gate?stale_orient_tick=" + id,
		"/gate?dup=" + id,
		"/gate?tw=age",
		"/gate?editsel=none",
		"/gate/vehicle?arrived_vehicle=FAKE-99&arrived_checked=42",
		// a seal that is not one
		"/gate?not_entered=" + id + "&ns=1.deadbeef",
	}
	for _, path := range forged {
		w3NoNotice(t, w3AnonGet(t, w.srv, path), "anonymous "+path)
		w3NoNotice(t, w3Get(t, w.srv, path), "logged-in "+path)
	}
}

// TestW3ASealedNoticeRendersAndATamperedOrStaleOneDoesNot: the real redirect
// still renders its notice; the same URL with its value swapped, or read
// after the seal's window, renders nothing.
func TestW3ASealedNoticeRendersAndATamperedOrStaleOneDoesNot(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	rec := postForm(t, w.srv, "/gate/checkin", scriptedCheckInBody("TRK-90", 0, orientBlockRows(w.driver, w.uma)))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("check-in = %d: %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, noticeSealQueryKey+"=") {
		t.Fatalf("the commit's redirect %q carries no seal", loc)
	}
	if page := w3Get(t, w.srv, loc); !strings.Contains(page, "NOT ENTERED — NO ORIENTATION") || !strings.Contains(page, "Uma Unoriented") {
		t.Fatalf("the sealed redirect no longer renders its own NOT ENTERED notice")
	}
	tampered := strings.Replace(loc, url.QueryEscape(w.uma.PersonID), url.QueryEscape(w.orla.PersonID), 1)
	if tampered == loc {
		t.Fatalf("test defect: could not swap the id in %q", loc)
	}
	w3NoNotice(t, w3Get(t, w.srv, tampered), "tampered "+tampered)

	defer func(old func() time.Time) { noticeNow = old }(noticeNow)
	noticeNow = func() time.Time { return time.Now().Add(noticeSealTTL + time.Minute) }
	w3NoNotice(t, w3Get(t, w.srv, loc), "expired "+loc)
}

// --- M-3 / G-125 ----------------------------------------------------------

// TestW3InboundUntickIsCountedAndConfirmed: unticking someone under "adjust"
// changed nothing on screen ("Check in (2)" stayed) and asked nothing; the
// person went to NOT TRACKED.
func TestW3InboundUntickIsCountedAndConfirmed(t *testing.T) {
	w := newInboundWorld(t)
	block := vehicleBlock(t, noScriptMarkup(t, w.srv, "/gate/vehicle"), "TRK-100")
	for _, want := range []string{"data-inbound-confirm", `data-checkin-label="Check in"`, "Untick only someone who is NOT here", "NOT TRACKED"} {
		if !strings.Contains(block, want) {
			t.Errorf("the inbound row lacks %q (JS-off half of G-125):\n%s", want, block)
		}
	}
	page := rawPage(t, w.srv, "/gate/vehicle")
	for _, want := range []string{"form[data-inbound-confirm]", "' of ' + boxes.length", "NOT checking in "} {
		if !strings.Contains(page, want) {
			t.Errorf("the served script lacks %q — the untick is neither counted nor confirmed", want)
		}
	}
}

// --- G-078 ------------------------------------------------------------------

func w3StageCheckOut(t *testing.T, srv *Server, visitID string, extra url.Values) string {
	t.Helper()
	vals := url.Values{"visit_ids": {visitID}}
	for k, v := range extra {
		vals[k] = v
	}
	rec := postForm(t, srv, "/gate/checkout/select", vals)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /gate/checkout/select = %d: %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// TestW3NoDestinationPostServesTheConfirmDisabled: npf (no destinations, no
// road work) served a live "Confirm check out" that could only ever 409.
func TestW3NoDestinationPostServesTheConfirmDisabled(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	srv.Cfg.Destinations, srv.Cfg.Peers = nil, nil // no list, and none derived from peers (config.DestinationList)
	visits := checkInCrew(t, srv, st, 1)
	page := w3StageCheckOut(t, srv, visits[0], url.Values{"gate_page": {"individual"}})
	commit := regexp.MustCompile(`<button[^>]*class="button-primary gate-commit"[^>]*>`).FindString(page)
	if commit == "" {
		t.Fatal("precondition: no commit button on the staged check-out")
	}
	if !strings.Contains(commit, " disabled") || !strings.Contains(commit, "data-no-destination") {
		t.Errorf("with no destination configured the Confirm is served live: %s", commit)
	}
	// ⟦merge W3+W8, 2026-10-03⟧ W8 gave the reason an id and role="status"
	// (the disabled Confirm's aria-describedby points at it), so the tag is
	// matched by class with any attributes — but never `hidden`. ⟦was: the
	// exact `<p class="commit-blocked">No destination…`⟧
	if m := regexp.MustCompile(`<p class="commit-blocked"[^>]*>No destination is configured at this post`).FindString(page); m == "" || strings.Contains(m, "hidden") {
		t.Error("the disabled Confirm carries no visible reason")
	} else if !strings.Contains(commit, `aria-describedby="commit-blocked-`) || !strings.Contains(m, `id="commit-blocked-`) {
		t.Error("the disabled Confirm is not described by its visible reason (W8 aria-describedby)")
	}
	// A post WITH destinations is untouched.
	srv2, st2, _ := newGateTestServer(t)
	v2 := checkInCrew(t, srv2, st2, 1)
	page2 := w3StageCheckOut(t, srv2, v2[0], nil)
	if c := regexp.MustCompile(`<button[^>]*class="button-primary gate-commit"[^>]*>`).FindString(page2); strings.Contains(c, "disabled") {
		t.Errorf("a post with destinations serves its Confirm disabled: %s", c)
	}
}

// --- X-MERGE-WARN -----------------------------------------------------------

// TestW3AMergedAwayIdIsWarnedTheWayTheCommitReadsIt: Uma (no orientation
// here) merged same_as Orla (holds it). The commit resolves the merge and
// ENTERS her; the row warned "NOT ORIENTED HERE … will not be checked in".
func TestW3AMergedAwayIdIsWarnedTheWayTheCommitReadsIt(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	orla := w.orla.PersonID
	if _, err := domain.AmendPerson(w.st, "site-b", 1, domain.PersonAmendment{PersonID: w.uma.PersonID, SameAs: &orla}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	rec := postForm(t, w.srv, "/gate/passenger/add", url.Values{
		"direction": {"in"}, "passenger_list": {"1"}, "new_kind": {"existing"}, "new_last": {w.uma.Name},
	})
	page := rec.Body.String()
	if strings.Contains(page, "NOT ORIENTED HERE — "+w.uma.Name) {
		t.Errorf("the merged-away id is warned NOT ORIENTED HERE, but the commit enters her through the merge (X-MERGE-WARN)")
	}
	if !strings.Contains(page, "orientation on file") {
		t.Errorf("the merged-away id's row does not show the orientation its canonical record holds")
	}
	// The payload the scripted rows read says the same.
	payload := w3Get(t, w.srv, "/gate/vehicle")
	re := regexp.MustCompile(`"id":"` + regexp.QuoteMeta(w.uma.PersonID) + `"[^}]*`)
	if obj := re.FindString(payload); !strings.Contains(obj, `"orient_held":true`) || strings.Contains(obj, "orient_needed") {
		t.Errorf("the roster payload flags the merged-away id as needing the orientation: %s", obj)
	}
	// And the commit does what the row said.
	rec = postForm(t, w.srv, "/gate/checkin", scriptedCheckInBody("TRK-91", 0, orientBlockRows(w.driver, w.uma)))
	if rec.Code != http.StatusSeeOther || strings.Contains(rec.Header().Get("Location"), notEnteredQueryKey) {
		t.Fatalf("commit = %d %q — she should be entered with no NOT ENTERED", rec.Code, rec.Header().Get("Location"))
	}
}

// --- F-R2-L02-1 / A2 ----------------------------------------------------------

// TestW3TheRecordRefusalNeverCoachesAMissingTick: the individual door has no
// "change their record" tick; its refusal said "Tick it if their record
// should change".
func TestW3TheRecordRefusalNeverCoachesAMissingTick(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	chris := persons[2]
	other, err := domain.CreateCompany(st, "site-a", 1, "A Different Company LLC", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	vals := singleCheckInForm(t, srv, chris.Name)
	vals.Set("new_kind", "existing")
	vals.Set("new_value", chris.PersonID)
	vals.Set("new_company_id", other.CompanyID)
	rec := postForm(t, srv, "/gate/checkin/single", vals)
	if rec.Code != http.StatusConflict {
		t.Fatalf("precondition: a disagreeing company must still be refused, got %d", rec.Code)
	}
	body := rec.Body.String()
	for _, bad := range []string{"Tick it", "is not ticked"} {
		if strings.Contains(body, bad) {
			t.Errorf("the refusal coaches a tick the page does not have: %q", bad)
		}
	}
	if !strings.Contains(body, "Kakivik Asset Management · Inspection") || !strings.Contains(body, "Edit person") {
		t.Errorf("the refusal does not say what the record holds or how to change it")
	}
}

// TestW3PickingTheRecordsCompanyByNameIsNotAChange: three "Tundra Services"
// rows (one per post) made "put their details back" a guess — the right
// name under another row's id was refused as a record change.
func TestW3PickingTheRecordsCompanyByNameIsNotAChange(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	chris := persons[2]
	twin, err := domain.CreateCompany(st, "site-b", 1, "Kakivik Asset Management", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	vals := singleCheckInForm(t, srv, chris.Name)
	vals.Set("new_kind", "existing")
	vals.Set("new_value", chris.PersonID)
	vals.Set("new_company_id", twin.CompanyID)
	rec := postForm(t, srv, "/gate/checkin/single", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the record's company picked under its same-name twin row = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	now, err := domain.PersonByID(st, chris.PersonID)
	if err != nil || now == nil || now.CompanyID == nil || *now.CompanyID != *chris.CompanyID {
		t.Fatalf("the record's company moved to the twin row: %+v %v", now, err)
	}
}

// --- vehicle-door blank company (L12) -------------------------------------------

// TestW3AVehicleDoorBlankCompanyAssertsNothing: a crafted / JS-off body that
// left a picked person's company blank was refused as a record change.
func TestW3AVehicleDoorBlankCompanyAssertsNothing(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	dana, pat := persons[0], persons[1]
	row := knownRow(pat)
	row.CompanyID, row.TradeID = "", ""
	rec := postForm(t, srv, "/gate/checkin", scriptedCheckInBody("TRK-12", 0, []passengerRow{knownRow(dana), row}))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("a blank company/trade on a known row = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	now, err := domain.PersonByID(st, pat.PersonID)
	if err != nil || now == nil || now.CompanyID == nil || *now.CompanyID != *pat.CompanyID || now.TradeID == nil {
		t.Fatalf("a blank, unticked picker changed the record: %+v %v", now, err)
	}
}

// --- JS-off vehicle door new-person fields ----------------------------------------

// TestW3JSOffVehicleDoorShowsTheNewPersonFields: .new-fields is hidden by the
// stylesheet until it carries "show", which only the script ever added — so
// with scripting off a new person's Company (required) never rendered.
func TestW3JSOffVehicleDoorShowsTheNewPersonFields(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	markup := noScriptMarkup(t, srv, "/gate/vehicle")
	if !strings.Contains(markup, `<div class="new-fields show" id="new-fields-in">`) {
		t.Fatalf("the arriving form's new-person fields are served hidden — invisible with scripting off")
	}
	if !regexp.MustCompile(`syncFirstNameField\(\); \} \}\);\s*open\.syncNewFields\(\);`).MatchString(rawPage(t, srv, "/gate/vehicle")) {
		t.Error("the script no longer hides the fields again on load")
	}
}

// --- D-48 / Y-20c -------------------------------------------------------------------

// TestW3TheGateRosterListRendersName: amending Last alone (lawful, D-48)
// gave the gate's on-site list a second surname for one person.
func TestW3TheGateRosterListRendersName(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	dana, pat := persons[0], persons[1]
	div, last, first := "Smythe", "Dana", "Driver"
	if _, err := domain.AmendPerson(st, "site-a", 1, domain.PersonAmendment{PersonID: dana.PersonID, LastName: &div, FirstName: &last}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	agreeName := "Passenger, Pat"
	agreeLast, agreeFirst := "Passenger", "Pat"
	if _, err := domain.AmendPerson(st, "site-a", 1, domain.PersonAmendment{PersonID: pat.PersonID, Name: &agreeName, LastName: &agreeLast, FirstName: &agreeFirst}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	_ = first
	rec := postForm(t, srv, "/gate/checkin", scriptedCheckInBody("TRK-48", 0, []passengerRow{knownRow(dana), {Kind: "existing", Value: pat.PersonID, Typed: agreeName, CompanyID: *pat.CompanyID, TradeID: *pat.TradeID}}))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("check-in = %d: %s", rec.Code, rec.Body.String())
	}
	_, page := getGateIndividual(t, srv)
	got := map[string][2]string{}
	for _, r := range rosterRows(t, page, "checkout-picker") {
		got[r.value] = [2]string{r.last, r.first}
	}
	open, _ := domain.OpenVisitsAt(st, "site-a")
	for _, v := range open {
		cells := got[v.VisitID]
		switch v.PersonID {
		case dana.PersonID:
			if cells != [2]string{dana.Name, ""} {
				t.Errorf("a person whose parts disagree with Name shows %q / %q, want the Name %q whole (D-48)", cells[0], cells[1], dana.Name)
			}
		case pat.PersonID:
			if cells != [2]string{"Passenger", "Pat"} {
				t.Errorf("a person whose parts ARE the Name shows %q / %q, want Passenger / Pat", cells[0], cells[1])
			}
		}
	}
}

// --- orientation flags, anonymous (F-L03-R3-2) -------------------------------------------

// TestW3AnAnonymousAccumulateNamesNobodysOrientation: one anonymous POST per
// name read this post's orientation register ("NOT ORIENTED HERE — X").
func TestW3AnAnonymousAccumulateNamesNobodysOrientation(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	for _, p := range []domain.Person{w.uma, w.orla} {
		form := url.Values{"direction": {"in"}, "passenger_list": {"1"}, "new_kind": {"existing"}, "new_last": {p.Name}}
		anon := scriptRe.ReplaceAllString(postFormAnon(w.srv, "/gate/passenger/add", form).Body.String(), "") // the script's own string literals are not a person's status
		for _, leak := range []string{"NOT ORIENTED HERE", "orientation on file"} {
			if strings.Contains(anon, leak) {
				t.Errorf("anonymous add of %s renders %q — orientation status read without a login", p.Name, leak)
			}
		}
	}
	// Control: logged in, the warning is there.
	if authed := postForm(t, w.srv, "/gate/passenger/add", url.Values{"direction": {"in"}, "passenger_list": {"1"}, "new_kind": {"existing"}, "new_last": {w.uma.Name}}).Body.String(); !strings.Contains(authed, "NOT ORIENTED HERE") {
		t.Fatal("control: the logged-in add no longer warns — this test would prove nothing")
	}
}

// --- S-034 ---------------------------------------------------------------------------------

func TestW3AnAnonymousGatePageDoesNotClaimASession(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	for _, path := range []string{"/gate", "/gate/vehicle"} {
		anon := w3AnonGet(t, srv, path)
		if strings.Contains(anon, ">Log out<") || strings.Contains(anon, "Recording as:") {
			t.Errorf("anonymous %s shows \"Recording as … / Log out\" (S-034)", path)
		}
		if !strings.Contains(anon, "Not logged in") {
			t.Errorf("anonymous %s does not say it is not logged in", path)
		}
		if authed := w3Get(t, srv, path); !strings.Contains(authed, ">Log out<") {
			t.Errorf("logged-in %s lost its Log out control", path)
		}
	}
}

// --- G-076 -------------------------------------------------------------------------------------

func TestW3TheInlineCancelReturnsToThePageTheOfficerIsOn(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	visits := checkInCrew(t, srv, st, 1)
	cancelRe := regexp.MustCompile(`<a class="button cancel-inline" href="([^"]*)">`)
	if m := cancelRe.FindStringSubmatch(w3StageCheckOut(t, srv, visits[0], nil)); m == nil || m[1] != "/gate/vehicle" {
		t.Errorf("vehicle page's inline Cancel = %v, want /gate/vehicle (G-076)", m)
	}
	if m := cancelRe.FindStringSubmatch(w3StageCheckOut(t, srv, visits[0], url.Values{"gate_page": {"individual"}})); m == nil || m[1] != "/gate" {
		t.Errorf("individual page's inline Cancel = %v, want /gate", m)
	}
}

// --- an individual refusal is not "a part-entered vehicle" ------------------------------------

func TestW3AnIndividualRefusalDoesNotClaimAPartEnteredVehicle(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	vals := singleCheckInForm(t, srv, "Zqnocomp, Beta") // a new person, no company: refused (R-26)
	rec := postForm(t, srv, "/gate/checkin/single", vals)
	if rec.Code != http.StatusConflict {
		t.Fatalf("precondition: refused, got %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "An arriving vehicle is part-entered above") {
		t.Error("the individual page's refusal says a vehicle is part-entered — there is none on this page")
	}
	if !strings.Contains(body, `id="checkout-picker"`) {
		t.Error("the refusal page lost the on-site roster")
	}
}

// --- overlong names (L04) ------------------------------------------------------------------------

func TestW3AnOverlongNewNameIsRefusedAndMintsNothing(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	vals := singleCheckInForm(t, srv, strings.Repeat("x", 528))
	vals.Set("new_company_id", newRef)
	vals.Set("new_company", "Long Name Co")
	rec := postForm(t, srv, "/gate/checkin/single", vals)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "528 characters long") {
		t.Fatalf("a 528-character name = %d, want 409 naming the length: %s", rec.Code, rec.Body.String())
	}
	if all, _ := domain.Persons(st); len(all) != len(persons) {
		t.Fatalf("the refused overlong name minted a person: %d -> %d", len(persons), len(all))
	}
}

// --- identical duplicate-offer buttons (L04/L14) --------------------------------------------------

func TestW3IdenticalDuplicateOffersAreToldApart(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	co, tr := persons[0].CompanyID, persons[0].TradeID
	for i := 0; i < 2; i++ {
		if _, err := domain.CreatePerson(st, "site-a", 1, "Zqtwin, Pat", co, tr, 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	rec := postForm(t, srv, "/gate/passenger/add", url.Values{"direction": {"in"}, "passenger_list": {"1"}, "new_kind": {"existing"}, "new_last": {"Zqtwin, Pat"}})
	buttons := regexp.MustCompile(`<button type="submit" class="p-pick"[^>]*>([^<]*)</button>`).FindAllStringSubmatch(rec.Body.String(), -1)
	if len(buttons) != 2 {
		t.Fatalf("precondition: want two duplicate offers, got %d", len(buttons))
	}
	if buttons[0][1] == buttons[1][1] {
		t.Errorf("two offers for two different records read identically: %q", buttons[0][1])
	}
}

// --- script shapes (browser half driven live on L10) --------------------------------------------

func TestW3ServedScriptShapes(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	page := rawPage(t, srv, "/gate/vehicle")
	ind := rawPage(t, srv, "/gate")
	for _, c := range []struct{ page, want, why string }{
		// the decline ruling: First completing to a DIFFERENT exact person asks
		{page, "function askAboutComposed(typed)", "vehicle open box: decline + First name completing to another roster person is not asked about"},
		{page, "'Is this ' + it.name + '?'", "the second ask does not name the person"},
		{ind, "proposeAdopt(others[0], full, true)", "individual page: decline + First name is not asked about"},
		// a retyped row's label says what the commit will do
		{page, "function newRowLabel(row)", "a retyped row still claims \"Not on the roster — this will add them\""},
		// a refusal re-render keeps the pick; a pick never posts a stale company
		{ind, "var repick = personById(valueField.value);", "the individual page forgets the pick on a refusal re-render"},
		{ind, "if (coSel) coSel.value = '';", "a picked person's hidden company picker still posts a stale company"},
		// "Picked:" carries the company (L10 LOW)
		{ind, "if (it.company) parts.push(it.company);", "the Picked line omits the company"},
		// T-023: the dispatch confirm keys by value
		{rawPage(t, srv, "/trips"), "var key = box.value;", "the confirm still counts a driver ticked twice as two people"},
	} {
		if !strings.Contains(c.page, c.want) {
			t.Errorf("%s (missing %q)", c.why, c.want)
		}
	}
}

// --- M-24 (taken over from W8): the type-ahead is a keyboard combobox --------

func TestW3TheTypeAheadIsAKeyboardCombobox(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	for _, path := range []string{"/gate", "/gate/vehicle"} {
		page := rawPage(t, srv, path)
		for _, want := range []string{
			"input.setAttribute('role', 'combobox')",
			"list.setAttribute('role', 'listbox')",
			"li.setAttribute('role', 'option')",
			"e.key === 'ArrowDown' || e.key === 'ArrowUp'",
			"input.setAttribute('aria-activedescendant', lis[active].id)",
			"e.stopImmediatePropagation();",
		} {
			if !strings.Contains(page, want) {
				t.Errorf("%s: the type-ahead script lacks %q — suggestions cannot be reached or announced by keyboard (M-24)", path, want)
			}
		}
	}
}
