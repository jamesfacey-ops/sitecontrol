// w5fix_surfaces_test.go — fork/w5 (2026-10-03): the officer-facing halves
// of the reference-data and roster fixes, each driven through srv.Handler()
// and asserted on the rendered page or the redirect, using only API that
// existed before the fix — so every test here was run against the unfixed
// tree first and FAILED there (the lane reports cite the same observables):
//
//	R-081 / M-17  /trade never rendered a supersession; a forged ?recorded=
//	              echoed "Recorded." (lanes-r2/L06.md R-081, R-080).
//	R-132         /person/amend wiped every box on a 409 (L06 R-132).
//	R-004x / M-15 a refusal at a non-entrance post re-collapsed the intake
//	              <details>, hiding the tick it told the officer to use
//	              (lanes-r2/L04.md R-004x-dup/-phone/-company).
//	R-118 / M-8   a retired person stayed in the gate type-ahead, the
//	              arrival picker and the orientation tick list (L06 R-118).
//	F-SETUP-1     same-name company/trade rows were indistinguishable on
//	              every picker (lanes-r2/L06.md seed note; L04 R-072).
//	L05           a completion that entered a person off a stale SECOND
//	              held-out arrival said only "Recorded 1" (lanes/L05.md
//	              observation, orientation.go:328-355 as it then stood).
package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// w5fixServer is a site-b post with one peer (site-a, display "Ice Road"),
// its own display name "Pad Site Control", and the orientation flag as asked.
func w5fixServer(t *testing.T, orientation bool) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "w5fix.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	o := orientation
	cfg := &config.Config{
		SiteID: "site-b", DisplayName: "Pad Site Control", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 3600,
		LoginPassword: testLoginPassword,
		Peers:         []config.Peer{{SiteID: "site-a", BaseURL: "http://example.invalid"}},
		Destinations:  []config.Destination{{ID: "site-a", DisplayName: "Ice Road", Controlled: true}},
		Orientation:   &o,
	}
	return New(cfg, st), st
}

// w5fixInputValue returns the value= of the first <input name="name"> on the
// page, and whether such an input exists.
func w5fixInputValue(page, name string) (string, bool) {
	re := regexp.MustCompile(`<input[^>]*\bname="` + regexp.QuoteMeta(name) + `"[^>]*>`)
	tag := re.FindString(page)
	if tag == "" {
		return "", false
	}
	m := regexp.MustCompile(`\bvalue="([^"]*)"`).FindStringSubmatch(tag)
	if m == nil {
		return "", true
	}
	return m[1], true
}

func w5fixLocation(t *testing.T, rec interface{ Header() http.Header }, what string) string {
	t.Helper()
	loc := rec.Header().Get("Location")
	if loc == "" {
		t.Fatalf("%s: no Location header", what)
	}
	return loc
}

// --- R-081: /trade renders the outcome -----------------------------------------

func TestR081TradePageShowsASupersededRenameAndFailsClosedOnAForgedKey(t *testing.T) {
	srv, st := w5fixServer(t, false)
	cookie := loginCookie(t, srv, testLoginPassword)
	crane, err := domain.CreateTrade(st, "site-b", 1, "Crane Operator", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	amend := func(name string) string {
		rec := postForm(t, srv, "/trade/amend", url.Values{"trade_id": {crane.TradeID}, "name": {name}})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("POST /trade/amend name=%q = %d: %s", name, rec.Code, rec.Body.String())
		}
		return w5fixLocation(t, rec, "trade amend")
	}
	first := amend("crane operator")
	second := amend("Crane Operator")
	if !strings.Contains(first, "?recorded=site-b.1.") || first == second {
		t.Fatalf("precondition: the two amendments did not redirect to two distinct keys: %q / %q", first, second)
	}

	losing := w2Get(t, srv, first, cookie).Body.String()
	if !strings.Contains(losing, "superseded") || !strings.Contains(losing, "made at this post") {
		t.Errorf("GET %s (the losing rename) does not say it is superseded by a later amendment made at this post — the officer is told their value stands when it does not (R-081):\n%s", first, okNoticeText(losing)+"\n"+w5fixWarnNotices(losing))
	}
	if strings.Contains(w5fixOkNotices(losing), "Recorded.") && !strings.Contains(losing, "superseded") {
		t.Errorf("GET %s shows the terse Recorded. beside no supersession", first)
	}
	winning := w2Get(t, srv, second, cookie).Body.String()
	if !strings.Contains(winning, "now stands") || !strings.Contains(winning, "this post") || strings.Contains(winning, "radio it through") {
		t.Errorf("GET %s (the standing rename) does not carry the same-post winner's sentence without a radio order:\n%s", second, w5fixOkNotices(winning))
	}

	// ⟦D23, fix2-fj 2026-10-04⟧ James, 2026-10-04 (FINDINGS-R4 cluster J): ?recorded= renders only off a sealed redirect, so this arm is reached through one; a hand-typed value renders nothing (fix2_fj_sealed_notices_test.go).
	forged := w2Get(t, srv, sealNotices("/trade?recorded=x"), cookie).Body.String()
	if !strings.Contains(forged, "outcome could not be determined") {
		t.Errorf("GET /trade?recorded=x does not fail closed:\n%s", w5fixOkNotices(forged)+w5fixWarnNotices(forged))
	}
	if strings.Contains(w5fixOkNotices(forged), "Recorded.") {
		t.Error("GET /trade?recorded=x still echoes Recorded. for a key that names nothing (R-080)")
	}
	// The create redirect's shape — a trade id — still confirms tersely.
	created := w2Get(t, srv, sealNotices("/trade?recorded="+crane.TradeID), cookie).Body.String()
	if !strings.Contains(w5fixOkNotices(created), "Recorded.") || strings.Contains(created, "could not be determined") {
		t.Errorf("GET /trade?recorded=<trade id> lost its Recorded. confirmation:\n%s", w5fixOkNotices(created)+w5fixWarnNotices(created))
	}
}

func w5fixNotices(page, class string) string {
	var out []string
	rest := page
	for {
		i := strings.Index(rest, `class="notice `+class+`"`)
		if i < 0 {
			break
		}
		rest = rest[i:]
		j := strings.Index(rest, "</div>")
		if j < 0 {
			break
		}
		out = append(out, rest[:j])
		rest = rest[j:]
	}
	return strings.Join(out, "\n")
}
func w5fixOkNotices(page string) string   { return w5fixNotices(page, "notice-ok") }
func w5fixWarnNotices(page string) string { return w5fixNotices(page, "notice-warn") }

// --- R-132: person amend keeps the typing --------------------------------------

func TestR132PersonAmendRefusalKeepsTheOfficersTyping(t *testing.T) {
	srv, _ := w5fixServer(t, false)
	rec := postForm(t, srv, "/person/amend", url.Values{
		"person_id": {"nope"}, "name": {"X"}, "phone": {"1"}, "role": {"r"}, "clear": {"position"},
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("POST /person/amend with an unknown id = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for name, want := range map[string]string{"person_id": "nope", "name": "X", "phone": "1", "role": "r", "clear": "position"} {
		got, present := w5fixInputValue(body, name)
		if !present {
			t.Errorf("the re-rendered form has no <input name=%q>", name)
			continue
		}
		if got != want {
			t.Errorf("after the refusal <input name=%q> carries value=%q, want the typed %q (R-132: the sibling forms keep theirs)", name, got, want)
		}
	}
	// An untouched box stays empty — the typing comes back, nothing else.
	if got, _ := w5fixInputValue(body, "same_as"); got != "" {
		t.Errorf("an untouched box re-rendered with value=%q", got)
	}
}

// --- R-004x: a refusal away from the entrance keeps the form open ---------------

func TestR004xIntakeRefusalAtANonEntrancePostOpensTheDisclosure(t *testing.T) {
	srv, st := intakeSiteServer(t, "site-b", "site-a")
	companyID := companyIDNamed(t, srv, "Kakivik Asset Management")
	if _, err := domain.CreatePerson(st, "site-a", 1, "Ashby, Bailey", &companyID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// Precondition: the plain page load keeps the demotion — closed.
	plain := rawPage(t, srv, "/roster")
	if !strings.Contains(plain, `id="intake-disclosure">`) || strings.Contains(plain, `id="intake-disclosure" open`) {
		t.Fatalf("precondition: the disclosure is not rendered closed on the plain load")
	}
	rec := postForm(t, srv, "/roster/intake", intakeForm2("Ashby", "Bailey", companyID, "", "", ""))
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate enrolment = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "different person, same name") {
		t.Fatalf("precondition: the refusal does not tell the officer to tick the box:\n%s", sectionBetween(t, body, `id="intake-error"`, "</div>"))
	}
	if !strings.Contains(body, `id="intake-disclosure" open`) {
		t.Errorf("after the duplicate refusal the intake <details> is rendered CLOSED — the tick the banner names is off screen (R-004x)")
	}
	disclosure := strings.Index(body, `id="intake-disclosure"`)
	tick := strings.Index(body, `name="same_name"`)
	if disclosure < 0 || tick < 0 || tick < disclosure {
		t.Errorf("the same_name tick is not inside the disclosure (disclosure at %d, tick at %d)", disclosure, tick)
	}
	if got, _ := w5fixInputValue(body, "last_name"); got != "Ashby" {
		t.Errorf("sticky last name = %q, want Ashby", got)
	}
	// The other two refusals the lane measured: blank phone, missing company.
	form := intakeForm2("Nophone", "Nell", companyID, "", "", "")
	form.Set("phone", "   ")
	if rec := postForm(t, srv, "/roster/intake", form); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `id="intake-disclosure" open`) {
		t.Errorf("after the blank-phone refusal (%d) the disclosure is not open", rec.Code)
	}
	if rec := postForm(t, srv, "/roster/intake", intakeForm2("Nocompany", "Nell", "", "", "", "")); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `id="intake-disclosure" open`) {
		t.Errorf("after the missing-company refusal (%d) the disclosure is not open", rec.Code)
	}
	// And the post-enrolment landing is closed again — the demotion stands.
	ok := postForm(t, srv, "/roster/intake", intakeForm2("Newcomer", "Nell", companyID, "", "", ""))
	if ok.Code != http.StatusSeeOther {
		t.Fatalf("a clean enrolment = %d: %s", ok.Code, ok.Body.String())
	}
	if landing := rawPage(t, srv, w5fixLocation(t, ok, "intake")); strings.Contains(landing, `id="intake-disclosure" open`) {
		t.Error("the post-enrolment landing renders the disclosure open — only a refusal should")
	}
}

// --- R-118: retired leaves every picker, never anyone on site ------------------

func w5fixRosterPayloadIDs(t *testing.T, page string) map[string]bool {
	t.Helper()
	blob := sectionBetween(t, page, `id="roster-data"`, "</script>")
	blob = blob[strings.Index(blob, ">")+1:]
	var payload struct {
		Persons []struct {
			ID string `json:"id"`
		} `json:"persons"`
	}
	if err := json.Unmarshal([]byte(blob), &payload); err != nil {
		t.Fatalf("roster-data is not JSON: %v\n%s", err, blob)
	}
	out := map[string]bool{}
	for _, p := range payload.Persons {
		out[p.ID] = true
	}
	return out
}

func w5fixRetire(t *testing.T, srv *Server, personID string) {
	t.Helper()
	rec := postForm(t, srv, "/person/amend", url.Values{"person_id": {personID}, "retired": {"true"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("retiring %s = %d: %s", personID, rec.Code, rec.Body.String())
	}
}

func TestR118ARetiredPersonIsNotSelectableAtTheGateOrForAnArrivalButStaysOnTheRoster(t *testing.T) {
	srv, st := w5fixServer(t, false) // a road post: every door enters
	cookie := loginCookie(t, srv, testLoginPassword)
	logan := orientBlockPerson(t, st, "Dunby, Logan")
	oakley := orientBlockPerson(t, st, "Dunby, Oakley")

	// Logan is checked in — on site. Retiring somebody on site is REFUSED.
	if rec := postForm(t, srv, "/gate/checkin", scriptedCheckInBody("TRK-1", 0, orientBlockRows(logan))); rec.Code != http.StatusSeeOther {
		t.Fatalf("check-in = %d: %s", rec.Code, rec.Body.String())
	}
	if !orientBlockOpenPersonIDs(t, st)[logan.PersonID] {
		t.Fatal("precondition: Logan has no open visit after the check-in")
	}
	rec := postForm(t, srv, "/person/amend", url.Values{"person_id": {logan.PersonID}, "retired": {"true"}})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "on site") {
		t.Fatalf("retiring a person ON SITE = %d, want a 409 saying they are on site: %s", rec.Code, w5fixNotices(rec.Body.String(), "notice-critical"))
	}
	if !orientBlockOpenPersonIDs(t, st)[logan.PersonID] {
		t.Fatal("the refused retirement removed Logan from the site")
	}
	// Still selectable while not retired — the control for every negative below.
	gate := w2Get(t, srv, "/gate", cookie).Body.String()
	if ids := w5fixRosterPayloadIDs(t, gate); !ids[logan.PersonID] || !ids[oakley.PersonID] {
		t.Fatalf("precondition: the type-ahead payload lacks an un-retired person: %v", ids)
	}

	// Checked out, then retired.
	open, err := domain.OpenVisitsAt(st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range open {
		if v.PersonID == logan.PersonID {
			if err := domain.CheckOutVisit(st, "site-b", 1, v.VisitID, nil, 0, "default"); err != nil {
				t.Fatal(err)
			}
		}
	}
	w5fixRetire(t, srv, logan.PersonID)

	// 1. The gate's type-ahead payload no longer offers Logan; Oakley stays.
	gate = w2Get(t, srv, "/gate", cookie).Body.String()
	ids := w5fixRosterPayloadIDs(t, gate)
	if ids[logan.PersonID] {
		t.Error("the gate type-ahead still offers a RETIRED person (R-118: D-21.5 'stops being selectable')")
	}
	if !ids[oakley.PersonID] {
		t.Error("retiring Logan dropped Oakley from the type-ahead")
	}
	// 2. The typed name at the gate (scripting off) does not adopt the
	//    retired record and does not mint a second one: it says why.
	rec = postForm(t, srv, "/gate/checkin", scriptedCheckInBody("TRK-2", 0, []passengerRow{{Kind: "existing", Value: "", Typed: logan.Name}}))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "RETIRED") {
		t.Errorf("typing a retired person's name at the gate = %d, want a 409 naming the retirement: %s", rec.Code, w5fixNotices(rec.Body.String(), "notice-critical"))
	}
	if orientBlockOpenPersonIDs(t, st)[logan.PersonID] {
		t.Error("the typed name checked the retired person in")
	}
	if n := len(personNamed(t, srv, logan.Name)); n != 1 {
		t.Errorf("%d roster rows named %q, want 1 — the typed name minted a duplicate of a retired record", n, logan.Name)
	}
	// 3. A crafted/stale id for the retired record is refused at the edge too.
	rec = postForm(t, srv, "/gate/checkin", scriptedCheckInBody("TRK-3", 0, orientBlockRows(logan)))
	if rec.Code == http.StatusSeeOther || orientBlockOpenPersonIDs(t, st)[logan.PersonID] {
		t.Errorf("a check-in carrying the retired record's id = %d and entered them", rec.Code)
	}
	// 4. The arrival picker's roster band does not offer Logan; Oakley yes.
	trips := w2Get(t, srv, "/trips", cookie).Body.String()
	arrival := sectionBetween(t, trips, `id="arrival-picker"`, "</form>")
	if strings.Contains(arrival, `value="`+logan.PersonID+`"`) {
		t.Error("the arrival picker still offers a retired person")
	}
	if !strings.Contains(arrival, `value="`+oakley.PersonID+`"`) {
		t.Error("retiring Logan dropped Oakley from the arrival picker")
	}
	// 5. The roster — the RECORD — still lists Logan, marked.
	roster := sectionBetween(t, rawPage(t, srv, "/roster"), `id="roster-table"`, "</table>")
	row := sectionBetween(t, roster, `data-person-id="`+logan.PersonID+`"`, "</tr>")
	if !strings.Contains(row, `data-retired="1"`) || !strings.Contains(row, "retired") {
		t.Errorf("the roster row for a retired person is not marked retired:\n%s", row)
	}
	// 6. Reversible: clear retired, and the type-ahead offers them again.
	if rec := postForm(t, srv, "/person/amend", url.Values{"person_id": {logan.PersonID}, "clear": {"retired"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("un-retire = %d: %s", rec.Code, rec.Body.String())
	}
	if ids := w5fixRosterPayloadIDs(t, w2Get(t, srv, "/gate", cookie).Body.String()); !ids[logan.PersonID] {
		t.Error("after Clear retired the type-ahead still hides Logan — retirement must be reversible (D-21.5)")
	}
}

func TestR118ARetiredPersonIsNotOfferedATickOnTheOrientationPage(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	cookie := loginCookie(t, w.srv, testLoginPassword)
	before := w2Get(t, w.srv, "/orientation", cookie).Body.String()
	if !strings.Contains(before, `id="orient-`+w.uma.PersonID+`"`) {
		t.Fatal("precondition: un-oriented Uma is not offered a tick before retirement")
	}
	w5fixRetire(t, w.srv, w.uma.PersonID)
	after := w2Get(t, w.srv, "/orientation", cookie).Body.String()
	if strings.Contains(after, `id="orient-`+w.uma.PersonID+`"`) {
		t.Error("the orientation tick list still offers a RETIRED person (R-118)")
	}
	// A retired person who already HOLDS the orientation keeps their record
	// (and the retract control): Orla holds it; retire her and she stays listed.
	w5fixRetire(t, w.srv, w.orla.PersonID)
	holding := sectionBetween(t, w2Get(t, w.srv, "/orientation", cookie).Body.String(), "Hold Pad Site Control's orientation", "Do not yet hold it")
	if !strings.Contains(holding, w.orla.PersonID) {
		t.Error("retiring a person who HOLDS the orientation removed their record from the Holding list — retirement affects selection, never the record")
	}
}

// --- F-SETUP-1: same-name rows are labelled with their origin -------------------

func TestFSetup1SameNameCompaniesAndTradesAreLabelledWithOriginAndHinted(t *testing.T) {
	srv, st := w5fixServer(t, false)
	cookie := loginCookie(t, srv, testLoginPassword)
	kestrelB, err := domain.CreateCompany(st, "site-b", 1, "Kestrel Survey Group", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	kestrelA, err := domain.CreateCompany(st, "site-a", 1, "Kestrel Survey Group", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.CreateCompany(st, "site-b", 1, "Tundra Services", 0, "default"); err != nil {
		t.Fatal(err)
	}
	if _, err := domain.CreateTrade(st, "site-b", 1, "Cook", 0, "default"); err != nil {
		t.Fatal(err)
	}
	// The second "Cook" arrives the only way a same-name trade can — as a
	// PEER's replicated row (the create door refuses a listed name locally):
	// one trade_created envelope authored by site-a, applied here.
	peerCook, err := json.Marshal(domain.TradeCreated{Type: "trade_created", TradeID: "site-a-trade-aaaaaaaaaaaaaaaa", Name: "Cook"})
	if err != nil {
		t.Fatal(err)
	}
	wm, err := st.Watermark("site-a", 1) // site-a's stream already carries the Kestrel row authored above
	if err != nil {
		t.Fatal(err)
	}
	if applied, err := st.Apply(store.Envelope{SiteID: "site-a", NodeEpoch: 1, Seq: wm + 1, SchemaVersion: store.SchemaVersion, OriginAuth: "site-a", Payload: peerCook}); err != nil || !applied {
		t.Fatalf("applying the peer's trade_created: applied=%v err=%v", applied, err)
	}
	if _, err := domain.CreateTrade(st, "site-b", 1, "Rigger", 0, "default"); err != nil {
		t.Fatal(err)
	}
	if trades, _ := domain.Trades(st); len(trades) != 3 {
		t.Fatalf("precondition: %d trades after seeding two Cooks and a Rigger, want 3: %+v", len(trades), trades)
	}

	roster := rawPage(t, srv, "/roster")
	companies := sectionBetween(t, roster, `id="company-id"`, "</select>")
	for _, want := range []string{"Kestrel Survey Group (added at Ice Road)", "Kestrel Survey Group (added at Pad Site Control)"} {
		if !strings.Contains(companies, want) {
			t.Errorf("the company picker does not label the same-name row %q:\n%s", want, companies)
		}
	}
	if strings.Contains(companies, "Tundra Services (added at") {
		t.Error("a uniquely named company is labelled with its origin — the label must fire only where there is something to tell apart")
	}
	trades := sectionBetween(t, roster, `id="trade-id"`, "</select>")
	for _, want := range []string{"Cook (added at Ice Road)", "Cook (added at Pad Site Control)"} {
		if !strings.Contains(trades, want) {
			t.Errorf("the trade picker does not label the same-name row %q:\n%s", want, trades)
		}
	}
	if strings.Contains(trades, "Rigger (added at") {
		t.Error("a uniquely named trade is labelled with its origin")
	}

	amend := w2Get(t, srv, "/company/amend", cookie).Body.String()
	if !strings.Contains(amend, `id="company-same-names"`) {
		t.Fatalf("/company/amend carries no same-name hint while two companies share a name")
	}
	hint := sectionBetween(t, amend, `id="company-same-names"`, "</section>")
	for _, want := range []string{kestrelA.CompanyID, kestrelB.CompanyID, "added at Ice Road", "added at Pad Site Control", "use Same as"} {
		if !strings.Contains(hint, want) {
			t.Errorf("the same-name hint lacks %q:\n%s", want, hint)
		}
	}
	if strings.Contains(hint, "Tundra Services") {
		t.Error("the same-name hint lists a uniquely named company")
	}

	trade := w2Get(t, srv, "/trade", cookie).Body.String()
	if n := strings.Count(trade, `data-same-name-trade=`); n != 2 {
		t.Errorf("%d same-name trade notes on /trade, want 2 (one per Cook row)", n)
	}
	for _, want := range []string{"added at Ice Road", "added at Pad Site Control", "use Same as"} {
		if !strings.Contains(trade, want) {
			t.Errorf("/trade lacks %q beside the same-name rows", want)
		}
	}
}

// --- L05: a completion that enters an already-sighted person says so ------------

// TestL05ACompletionThatEntersAPersonOffAStaleSecondArrivalSaysNowOnSite is
// the lane's exact sequence: Uma held out at TWO vehicles (two not_entered
// rows); ticked → entered off one; checked out; retracted; ticked again →
// entered off the other, still-unconsumed arrival — while the held-out list
// had already stopped listing her (sighted by the first entry). The banner
// used to say only "Recorded 1"; the muster gained a person in silence.
func TestL05ACompletionThatEntersAPersonOffAStaleSecondArrivalSaysNowOnSite(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	cookie := loginCookie(t, w.srv, testLoginPassword)
	for _, vehicle := range []string{"TRK-1", "TRK-2"} {
		if rec := postForm(t, w.srv, "/gate/checkin", scriptedCheckInBody(vehicle, 0, orientBlockRows(w.uma))); rec.Code != http.StatusSeeOther {
			t.Fatalf("check-in %s = %d: %s", vehicle, rec.Code, rec.Body.String())
		}
	}
	if orientBlockOpenPersonIDs(t, w.st)[w.uma.PersonID] {
		t.Fatal("precondition: Uma was entered by a hold-out check-in")
	}
	tick := func() string {
		rec := postForm(t, w.srv, "/orientation/complete", url.Values{"person_id": {w.uma.PersonID}})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("POST /orientation/complete = %d: %s", rec.Code, rec.Body.String())
		}
		return w5fixLocation(t, rec, "complete")
	}
	first := tick()
	if !orientBlockOpenPersonIDs(t, w.st)[w.uma.PersonID] {
		t.Fatal("precondition: the first completion did not enter Uma")
	}
	if !strings.Contains(first, "entered="+w.uma.PersonID) {
		t.Fatalf("control: the FIRST completion's redirect does not carry entered=: %s", first)
	}
	open, err := domain.OpenVisitsAt(w.st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range open {
		if v.PersonID == w.uma.PersonID {
			if err := domain.CheckOutVisit(w.st, "site-b", 1, v.VisitID, nil, 0, "default"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if rec := postForm(t, w.srv, "/orientation/retract", url.Values{"person_id": {w.uma.PersonID}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("retract = %d: %s", rec.Code, rec.Body.String())
	}
	// The scenario's own premise, asserted: Uma is NOT on the held-out list
	// now (sighted by the first entry), so a banner keyed on that list would
	// stay silent about what the next tick does.
	held, err := domain.HeldOutAt(w.st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range held {
		if h.PersonID == w.uma.PersonID {
			t.Fatal("precondition: Uma is still on the held-out list — the scenario does not pose the stale-second-arrival case")
		}
	}

	second := tick()
	if !orientBlockOpenPersonIDs(t, w.st)[w.uma.PersonID] {
		t.Fatal("precondition: the second completion did not re-enter Uma off her other arrival — nothing to announce")
	}
	if !strings.Contains(second, "entered="+w.uma.PersonID) {
		t.Errorf("the second completion ENTERED Uma but its redirect carries no entered= key: %s", second)
	}
	notice := okNoticeText(w2Get(t, w.srv, second, cookie).Body.String())
	if !strings.Contains(notice, "Uma Unoriented is now on site") {
		t.Errorf("the confirmation does not say Uma is now on site, though this tick put her on the muster (L05):\n%s", notice)
	}
}
