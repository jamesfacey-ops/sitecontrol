// person_dup_test.go — the person duplicate check at BOTH creation doors,
// proved in BOTH directions (person_match.go is the rule; this file is the
// doors).
//
//  1. IT CATCHES a real duplicate typed the sloppy way an officer actually
//     types — different case, a doubled space — and SURFACES the candidate
//     with their company.
//  2. IT NEVER BLOCKS, AND NEVER DECIDES. The officer can ignore every
//     candidate and the person is added; a folded match is never silently
//     resolved to the existing record (the false-merge mutant); two roster
//     people who share a name exactly are BOTH offered and the officer
//     picks; and a shared phone number under a different name is not a
//     duplicate at all.
//
// [[D-66]] — the gate never refuses. Every gate test below asserts the
// check-in still succeeds beside whatever the offer does or does not say.
package web

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// pickButtonFor is the "Use <name>" button the row renders for one
// candidate — asserted by its posted value, which is what the pick handler
// reads, not by its label.
func pickButtonFor(rowIndex int, personID string) string {
	return `name="pick" value="` + strconv.Itoa(rowIndex) + `:` + personID + `"`
}

// --- the gate -------------------------------------------------------------

// TestGateOffersTheRosterRecordForASloppilyTypedNameAndStillAddsThem is
// both directions on one row. The fixture's "Pat Passenger" is typed as
// "PAT  passenger" (case and a doubled space — inside the rule, and a
// spelling resolveTypedPerson does NOT auto-adopt). The row must OFFER Pat's
// record with company and trade; the officer ignores it and commits; the
// person is created and checked in. Nothing blocked, nothing decided.
func TestGateOffersTheRosterRecordForASloppilyTypedNameAndStillAddsThem(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	pat := persons[1]

	_, page := getGate(t, srv)
	page = gateSetVehicle(t, srv, page, gateInbound, "TRK-21")
	page = gateAdd(t, srv, page, gateInbound, "PAT  passenger")

	if !strings.Contains(page, "Possibly already on the roster") {
		t.Fatalf("the row for a name that reads as a roster person carries no offer:\n%s", page)
	}
	if !strings.Contains(page, pickButtonFor(0, pat.PersonID)) {
		t.Fatalf("the offer does not name Pat's record as a pick (want %s):\n%s", pickButtonFor(0, pat.PersonID), page)
	}
	if !strings.Contains(page, "Use Pat Passenger — Kakivik Asset Management · Inspection") {
		t.Fatalf("the offer does not show the company and trade beside the name — the field an officer tells two same-named people apart by:\n%s", page)
	}
	// The row is still NEW — the offer did not decide for the officer. (The
	// scriptless open box posts kind="existing" with an EMPTY value as its
	// default, and an empty value names nobody: the commit's identity rule
	// re-resolves the visible text. What must NOT be there is Pat's id.)
	vals := gateFormValues(t, page, formIDFor(gateInbound))
	if got := vals.Get("occupant_value"); got != "" {
		t.Fatalf("the row acquired a record id %q without the officer picking anything", got)
	}

	// Nothing was written by the accumulate step.
	all, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("roster is %d before the commit, want the fixture's 3", len(all))
	}

	// The officer ignores the offer and commits.
	// ⟦R-26⟧ this test is about the possible-match OFFER, not company — give
	// the new row one so the commit is not refused for the reason R-26
	// added. It must be the SAME company as Pat's own record: the
	// post-commit advisory this test checks below narrows away a same-name
	// candidate whose GIVEN company disagrees
	// (TestSameNameDifferentCompanyGetsNoAdvisory) — a different company
	// here would silently break the advisory assertion this test makes.
	gateFillNewRowCompanies(t, vals, "kakivik asset management")
	rec := postForm(t, srv, "/gate/checkin", gateMarkDriver(vals, 0))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("commit with the offer ignored = %d, want 303 — the check MUST NOT block an entry: %s", rec.Code, rec.Body.String())
	}
	all, err = domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		t.Fatalf("roster is %d after the commit, want 4 — the ignored offer must not have stopped the add", len(all))
	}
	if got := countPersonsNamed(t, all, "Pat Passenger"); got != 1 {
		t.Fatalf("the ORIGINAL Pat Passenger now has %d rows, want 1 — nothing may be merged or renamed", got)
	}
	// And the post-commit advisory (dupadvisory.go) fires on the same rule,
	// naming the same candidate — the scripting-on path's backstop.
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, dupWarnQueryKey+"="+pat.PersonID) {
		t.Fatalf("redirect %q carries no dup=%s — the post-commit advisory did not fire on a folded match", loc, pat.PersonID)
	}
}

// TestGatePickUsesTheExistingRecordAndMintsNobody is the offer TAKEN: the
// pick switches the row to Pat's record (visible name = roster name, so the
// commit's own identity rule agrees), the accumulate step writes nothing,
// and the commit checks Pat in without creating anybody.
func TestGatePickUsesTheExistingRecordAndMintsNobody(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	pat := persons[1]

	_, page := getGate(t, srv)
	page = gateSetVehicle(t, srv, page, gateInbound, "TRK-22")
	page = gateAdd(t, srv, page, gateInbound, "pat  PASSENGER")
	if !strings.Contains(page, pickButtonFor(0, pat.PersonID)) {
		t.Fatalf("precondition: no pick offered for Pat:\n%s", page)
	}

	vals := gateFormValues(t, page, formIDFor(gateInbound))
	vals.Set("pick", "0:"+pat.PersonID)
	rec := postForm(t, srv, "/gate/passenger/add", vals)
	if rec.Code != http.StatusOK {
		t.Fatalf("pick = %d, want 200 (an accumulate step renders directly): %s", rec.Code, rec.Body.String())
	}
	page = rec.Body.String()
	vals = gateFormValues(t, page, formIDFor(gateInbound))
	if got := vals.Get("occupant_kind"); got != "existing" {
		t.Fatalf("after the pick the row's kind is %q, want existing", got)
	}
	if got := vals.Get("occupant_value"); got != pat.PersonID {
		t.Fatalf("after the pick the row's value is %q, want Pat's id %s", got, pat.PersonID)
	}
	if got := vals.Get("occupant_typed"); got != pat.Name {
		t.Fatalf("after the pick the row's visible name is %q, want the roster name %q — the commit re-resolves the visible text, so a pick that leaves the typed spelling would be undone", got, pat.Name)
	}
	if strings.Contains(page, "Possibly already on the roster") {
		t.Fatalf("the offer is still shown on a row that now names a roster record:\n%s", page)
	}
	all, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("the pick authored: roster is %d, want 3", len(all))
	}

	rec = postForm(t, srv, "/gate/checkin", gateMarkDriver(vals, 0))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("commit after the pick = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	all, err = domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("roster is %d after the commit, want 3 — the pick should have used Pat's record, not minted one", len(all))
	}
	visits, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	onSite := false
	for _, v := range visits {
		if v.PersonID == pat.PersonID {
			onSite = true
		}
	}
	if !onSite {
		t.Fatalf("Pat has no open visit after the commit; open visits: %+v", visits)
	}
	if loc := rec.Header().Get("Location"); strings.Contains(loc, dupWarnQueryKey+"=") {
		t.Fatalf("redirect %q carries a dup advisory for a check-in that created nobody", loc)
	}
}

// TestTwoRosterPeopleWithOneNameAreBothOfferedAndThePickChoosesOne is the
// case the picker's own same-name machinery exists for, at the creation
// door: two roster rows called "Pat Passenger" at two companies. The strict
// key refuses to guess between them; the offer lists BOTH with their
// company, the officer picks the right one, and the commit checks that one
// in and mints nobody. MEASURED on the live data: company was the field
// that separated the two true different-people cases.
func TestTwoRosterPeopleWithOneNameAreBothOfferedAndThePickChoosesOne(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	patA := persons[1]
	other, err := domain.CreateCompany(st, "site-a", 1, "Northern Drilling", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	patB, err := domain.CreatePerson(st, "site-a", 1, "Pat Passenger", &other.CompanyID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	_, page := getGate(t, srv)
	page = gateSetVehicle(t, srv, page, gateInbound, "TRK-25")
	page = gateAdd(t, srv, page, gateInbound, "Pat Passenger")
	for _, want := range []string{
		pickButtonFor(0, patA.PersonID), "Kakivik Asset Management · Inspection",
		pickButtonFor(0, patB.PersonID), "Northern Drilling · none on file",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the offer for an exactly-shared name lacks %q:\n%s", want, page)
		}
	}
	vals := gateFormValues(t, page, formIDFor(gateInbound))
	if got := vals.Get("occupant_value"); got != "" {
		t.Fatalf("one of two same-named people was pre-picked: %q", got)
	}

	vals.Set("pick", "0:"+patB.PersonID)
	rec := postForm(t, srv, "/gate/passenger/add", vals)
	if rec.Code != http.StatusOK {
		t.Fatalf("pick = %d: %s", rec.Code, rec.Body.String())
	}
	vals = gateFormValues(t, rec.Body.String(), formIDFor(gateInbound))
	if got := vals.Get("occupant_value"); got != patB.PersonID {
		t.Fatalf("after the pick the row names %q, want the Northern Drilling Pat %s", got, patB.PersonID)
	}
	if rec = postForm(t, srv, "/gate/checkin", gateMarkDriver(vals, 0)); rec.Code != http.StatusSeeOther {
		t.Fatalf("commit = %d: %s", rec.Code, rec.Body.String())
	}
	all, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		t.Fatalf("roster is %d after the commit, want 4 (three fixtures plus the second Pat) — the pick must not mint", len(all))
	}
	visits, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	var onSite []string
	for _, v := range visits {
		onSite = append(onSite, v.PersonID)
	}
	if len(onSite) != 1 || onSite[0] != patB.PersonID {
		t.Fatalf("open visits = %v, want exactly the picked Pat %s", onSite, patB.PersonID)
	}
}

// TestAFoldedNameMatchIsNeverResolvedSilently is the false-merge guard —
// the most important assertion in this file. The loose key ASKS; only the
// strict key (TrimSpace + EqualFold, exactly one hit) DECIDES. A build that
// "helpfully" widens resolveTypedPerson to the fold would check a person in
// AS the roster record without a word — and if they are two people, one of
// them is off the muster while on site.
func TestAFoldedNameMatchIsNeverResolvedSilently(t *testing.T) {
	_, _, persons := newGateTestServer(t)
	pat := persons[1]

	for _, typed := range []string{"Pat  Passenger", "pat   passenger", " PAT  PASSENGER "} {
		if got := personMatchCandidates(persons, typed, ""); len(got) != 1 || got[0].Person.PersonID != pat.PersonID {
			t.Fatalf("precondition: %q should be OFFERED Pat and only Pat, got %+v", typed, got)
		}
		slot, err := resolveTypedPerson(typed, persons)
		if err != nil {
			t.Fatalf("resolveTypedPerson(%q): %v", typed, err)
		}
		if slot.ExistingID != "" {
			t.Fatalf("resolveTypedPerson(%q) silently resolved to %s — a folded match must be OFFERED, never adopted", typed, slot.ExistingID)
		}
		if slot.NewName == "" {
			t.Fatalf("resolveTypedPerson(%q) produced neither a record nor a new name", typed)
		}
	}
	// CONTROL: the strict key still resolves an exact (case-insensitive,
	// trimmed) spelling, as it always has.
	slot, err := resolveTypedPerson("  pat passenger ", persons)
	if err != nil {
		t.Fatal(err)
	}
	if slot.ExistingID != pat.PersonID {
		t.Fatalf("the strict key stopped resolving an exact spelling: %+v", slot)
	}
}

// TestGateOffersNothingForAGenuinelyNewName is the non-alarm half: a name
// that reads as nobody carries no offer. An offer that fires on ordinary
// work trains officers to stop reading it.
func TestGateOffersNothingForAGenuinelyNewName(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	_, page := getGate(t, srv)
	page = gateSetVehicle(t, srv, page, gateInbound, "TRK-23")
	page = gateAdd(t, srv, page, gateInbound, "Totally New Guy")
	if strings.Contains(page, "Possibly already on the roster") || strings.Contains(page, `name="pick"`) {
		t.Fatalf("a name that matches nobody drew an offer:\n%s", page)
	}
}

// TestAStalePickIsRefusedInWordsAndChangesNothing: a pick naming a row the
// list has moved past, or an id the roster does not hold, leaves the list
// intact and says so — the same posture as a stale delete.
func TestAStalePickIsRefusedInWordsAndChangesNothing(t *testing.T) {
	srv, _, persons := newGateTestServer(t)
	_, page := getGate(t, srv)
	page = gateSetVehicle(t, srv, page, gateInbound, "TRK-24")
	page = gateAdd(t, srv, page, gateInbound, "Pat  Passenger")
	before := gateFormValues(t, page, formIDFor(gateInbound))

	for _, pick := range []string{"7:" + persons[1].PersonID, "0:person-that-does-not-exist", "garbage"} {
		vals := gateFormValues(t, page, formIDFor(gateInbound))
		vals.Set("pick", pick)
		rec := postForm(t, srv, "/gate/passenger/add", vals)
		if rec.Code != http.StatusOK {
			t.Fatalf("stale pick %q = %d, want 200 with the list intact", pick, rec.Code)
		}
		if msg := vehicleError(t, rec.Body.String(), gateInbound); !strings.Contains(msg, "Nothing was changed") {
			t.Fatalf("stale pick %q: refusal does not say nothing changed: %q", pick, msg)
		}
		after := gateFormValues(t, rec.Body.String(), formIDFor(gateInbound))
		for _, f := range []string{"occupant_typed", "occupant_kind", "occupant_value"} {
			if before.Get(f) != after.Get(f) {
				t.Fatalf("stale pick %q changed %s from %q to %q", pick, f, before.Get(f), after.Get(f))
			}
		}
	}
}

// --- intake ----------------------------------------------------------------

// TestIntakeNamesEveryCandidateWithThePhoneSignalAndStillEnrolsOnAffirmation:
// "FERGUSON / mary" against an enrolled "Ferguson, Mary" with a DIFFERENT
// phone is refused, the candidate is named with company and origin site and
// the phone's word ("possibly a different person"), the affirmation is
// offered; affirmed, the second person is enrolled beside the first.
func TestIntakeNamesEveryCandidateWithThePhoneSignalAndStillEnrolsOnAffirmation(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	companyID := companyIDNamed(t, srv, "Kakivik Asset Management")

	if rec := postForm(t, srv, "/roster/intake", intakeForm2("Ferguson", "Mary", companyID, "", "", "")); rec.Code != http.StatusSeeOther {
		t.Fatalf("test setup: first enrolment = %d: %s", rec.Code, rec.Body.String())
	}

	second := intakeForm2("FERGUSON", "mary", companyID, "", "", "")
	second.Set("phone", "907-555-0199")
	rec := postForm(t, srv, "/roster/intake", second)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate enrolment = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// ⟦was: "(enrolled at site-a; …" — the raw id⟧ FIX3 C / F3e, James 2026-10-04: no screen shows a raw site id; the fixture names site-a "Ice Road A".
	for _, want := range []string{"Possibly already on the roster", "Ferguson, Mary — Kakivik Asset Management (enrolled at Ice Road A; different phone number on file, so possibly a different person)", `name="same_name"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("the refusal lacks %q:\n%s", want, body)
		}
	}
	if got := len(personNamed(t, srv, "FERGUSON, mary")); got != 0 {
		t.Fatalf("the refused enrolment was written anyway: %d rows", got)
	}

	second.Set("same_name", "1")
	if rec := postForm(t, srv, "/roster/intake", second); rec.Code != http.StatusSeeOther {
		t.Fatalf("affirmed enrolment = %d, want 303 — the check must not block a second person who reads alike: %s", rec.Code, rec.Body.String())
	}
	if got := len(personNamed(t, srv, "FERGUSON, mary")); got != 1 {
		t.Fatalf("%d rows for the affirmed second person, want 1", got)
	}
	if got := len(personNamed(t, srv, "Ferguson, Mary")); got != 1 {
		t.Fatalf("%d rows for the original, want 1 — nothing may be merged", got)
	}
}

// TestIntakeRefusalSaysSamePhoneWhenTheNumberAgrees: the same number under
// the same name leans "one person", and the refusal says so — as a word,
// not a decision.
func TestIntakeRefusalSaysSamePhoneWhenTheNumberAgrees(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	companyID := companyIDNamed(t, srv, "Kakivik Asset Management")
	if rec := postForm(t, srv, "/roster/intake", intakeForm2("Ferguson", "Mary", companyID, "", "", "")); rec.Code != http.StatusSeeOther {
		t.Fatalf("test setup: %d", rec.Code)
	}
	second := intakeForm2("Ferguson", "mary", companyID, "", "", "")
	second.Set("phone", "+1 907 555 0163") // the fixture's number, spelt differently
	rec := postForm(t, srv, "/roster/intake", second)
	if rec.Code != http.StatusConflict {
		t.Fatalf("= %d, want 409", rec.Code)
	}
	// ⟦was: "(enrolled at site-a; same phone number)"⟧ FIX3 C / F3e: display name.
	if !strings.Contains(rec.Body.String(), "(enrolled at Ice Road A; same phone number)") {
		t.Fatalf("the refusal does not say the phone agrees:\n%s", rec.Body.String())
	}
}

// TestASharedPhoneUnderADifferentNameIsNotADuplicate is the measured
// inversion at the door: crews share dispatch lines, so the fixture's one
// phone number under a second name enrols without a word — no refusal, no
// advisory, nothing carried on the redirect.
func TestASharedPhoneUnderADifferentNameIsNotADuplicate(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	companyID := companyIDNamed(t, srv, "Kakivik Asset Management")
	if rec := postForm(t, srv, "/roster/intake", intakeForm2("Ferguson", "Mary", companyID, "", "", "")); rec.Code != http.StatusSeeOther {
		t.Fatalf("test setup: %d", rec.Code)
	}
	rec := postForm(t, srv, "/roster/intake", intakeForm2("Smith", "Robert", companyID, "", "", "")) // intakeForm2's fixture phone again
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("same-phone, different-name enrolment = %d, want 303 — a shared number is not a duplicate: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/roster?enrolled=") || strings.Contains(loc, dupWarnQueryKey+"=") {
		t.Fatalf("redirect = %q, want a plain /roster?enrolled=<id> with nothing carried about the phone", loc)
	}
	if got := len(personNamed(t, srv, "Smith, Robert")); got != 1 {
		t.Fatalf("%d rows for Smith, Robert, want 1", got)
	}
}

// --- the variant this system manufactures, driven through the doors ------

// TestGateOffersTheIntakeSpellingForTheSpokenOrderAndStillAddsThem: intake
// enrols "Ferguson, Mary"; the gate hears "Mary Ferguson". MEASURED at
// 72d6111 (reports/witness/REVIEW-PERSON-DUP.md 🟠1) this minted a duplicate
// with no offer and no advisory. Now the row OFFERS Mary's record with her
// company beside it, nothing is pre-picked, the officer ignores it and the
// commit still succeeds — the person is added as typed, the original is
// untouched, and the post-commit advisory names her on the same rule.
func TestGateOffersTheIntakeSpellingForTheSpokenOrderAndStillAddsThem(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	companyID := companyIDNamed(t, srv, "Kakivik Asset Management")
	if rec := postForm(t, srv, "/roster/intake", intakeForm2("Ferguson", "Mary", companyID, "", "", "")); rec.Code != http.StatusSeeOther {
		t.Fatalf("test setup: enrolment = %d: %s", rec.Code, rec.Body.String())
	}
	enrolled := personNamed(t, srv, "Ferguson, Mary")
	if len(enrolled) != 1 {
		t.Fatalf("test setup: %d rows for Ferguson, Mary, want 1", len(enrolled))
	}
	mary := enrolled[0]

	_, page := getGate(t, srv)
	page = gateSetVehicle(t, srv, page, gateInbound, "TRK-31")
	page = gateAdd(t, srv, page, gateInbound, "Mary Ferguson")
	for _, want := range []string{"Possibly already on the roster", pickButtonFor(0, mary.PersonID), "Use Ferguson, Mary — Kakivik Asset Management"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the spoken-order row lacks %q — the comma swap did not offer the intake record:\n%s", want, page)
		}
	}
	vals := gateFormValues(t, page, formIDFor(gateInbound))
	if got := vals.Get("occupant_value"); got != "" {
		t.Fatalf("the row acquired a record id %q without the officer picking anything", got)
	}
	// The strict key did not adopt her either: the commit's own identity
	// rule sees a NEW name.
	all, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if slot, err := resolveTypedPerson("Mary Ferguson", all); err != nil || slot.ExistingID != "" {
		t.Fatalf("resolveTypedPerson(\"Mary Ferguson\") = %+v, %v — the comma swap must be OFFERED, never adopted", slot, err)
	}

	// ⟦R-26⟧ this test is about the comma-swap OFFER, not company — give the
	// new row one so the commit is not refused for the reason R-26 added.
	// It must be the SAME company Mary was enrolled under (Kakivik): the
	// post-commit advisory this test checks below narrows away a same-name
	// candidate whose GIVEN company disagrees
	// (TestSameNameDifferentCompanyGetsNoAdvisory).
	gateFillNewRowCompanies(t, vals, "kakivik asset management")
	rec := postForm(t, srv, "/gate/checkin", gateMarkDriver(vals, 0))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("commit with the offer ignored = %d, want 303 — the check MUST NOT block an entry: %s", rec.Code, rec.Body.String())
	}
	all, err = domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 {
		t.Fatalf("roster is %d after the commit, want 5 (three fixtures, Mary, and the row as typed) — the ignored offer must not have stopped the add", len(all))
	}
	if got := countPersonsNamed(t, all, "Ferguson, Mary"); got != 1 {
		t.Fatalf("the enrolled Ferguson, Mary now has %d rows, want 1 — nothing may be merged or renamed", got)
	}
	if got := countPersonsNamed(t, all, "Mary Ferguson"); got != 1 {
		t.Fatalf("%d rows for the name as typed, want 1", got)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, dupWarnQueryKey+"="+mary.PersonID) {
		t.Fatalf("redirect %q carries no dup=%s — the post-commit advisory did not fire on the comma swap", loc, mary.PersonID)
	}
}

// TestIntakeRefusesTheSpokenOrderAlreadyMintedAtTheGate is the other
// direction: a gate-minted "Mary Ferguson" is on the roster, and intake's
// composed "Ferguson, Mary" is refused NAMING her (MEASURED at 72d6111: it
// enrolled with no refusal). Affirmed, the second person is enrolled beside
// the first — the check never blocks a real second person.
func TestIntakeRefusesTheSpokenOrderAlreadyMintedAtTheGate(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	if _, err := domain.CreatePerson(st, "site-a", 1, "Mary Ferguson", persons[1].CompanyID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	companyID := companyIDNamed(t, srv, "Kakivik Asset Management")

	form := intakeForm2("Ferguson", "Mary", companyID, "", "", "")
	rec := postForm(t, srv, "/roster/intake", form)
	if rec.Code != http.StatusConflict {
		t.Fatalf("intake of the gate-minted person's other spelling = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	// ⟦was: "(enrolled at site-a"⟧ FIX3 C / F3e: display name.
	for _, want := range []string{"Possibly already on the roster", "Mary Ferguson — Kakivik Asset Management (enrolled at Ice Road A", `name="same_name"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("the refusal lacks %q:\n%s", want, rec.Body.String())
		}
	}
	if got := len(personNamed(t, srv, "Ferguson, Mary")); got != 0 {
		t.Fatalf("the refused enrolment was written anyway: %d rows", got)
	}

	form.Set("same_name", "1")
	if rec := postForm(t, srv, "/roster/intake", form); rec.Code != http.StatusSeeOther {
		t.Fatalf("affirmed enrolment = %d, want 303 — the check must not block a second person who reads alike: %s", rec.Code, rec.Body.String())
	}
	if got := len(personNamed(t, srv, "Ferguson, Mary")); got != 1 {
		t.Fatalf("%d rows for the affirmed enrolment, want 1", got)
	}
	if got := len(personNamed(t, srv, "Mary Ferguson")); got != 1 {
		t.Fatalf("%d rows for the gate-minted original, want 1 — nothing may be merged", got)
	}
}

// TestTwoPatsAtTwoCompaniesAreBothOfferedForTheFoldedSpellingAndAThirdIsStillAddable
// is the brief's named proof: two roster Pats at two companies, and the
// gate hears the intake spelling "Passenger, Pat". The fold offers BOTH
// with their companies (the field that tells them apart), pre-picks
// neither, and the strict key — which sees no exact hit — neither adopts
// nor refuses; the officer who ignores both adds a third record as typed.
// A genuinely different Pat stays addable at the busiest door.
func TestTwoPatsAtTwoCompaniesAreBothOfferedForTheFoldedSpellingAndAThirdIsStillAddable(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	patA := persons[1]
	other, err := domain.CreateCompany(st, "site-a", 1, "Northern Drilling", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	patB, err := domain.CreatePerson(st, "site-a", 1, "Pat Passenger", &other.CompanyID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	_, page := getGate(t, srv)
	page = gateSetVehicle(t, srv, page, gateInbound, "TRK-32")
	page = gateAdd(t, srv, page, gateInbound, "Passenger, Pat")
	for _, want := range []string{
		pickButtonFor(0, patA.PersonID), "Kakivik Asset Management · Inspection",
		pickButtonFor(0, patB.PersonID), "Northern Drilling · none on file",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the folded spelling's offer lacks %q:\n%s", want, page)
		}
	}
	vals := gateFormValues(t, page, formIDFor(gateInbound))
	if got := vals.Get("occupant_value"); got != "" {
		t.Fatalf("one of two same-named people was pre-picked for a folded spelling: %q", got)
	}
	all, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if slot, err := resolveTypedPerson("Passenger, Pat", all); err != nil || slot.ExistingID != "" || slot.NewName == "" {
		t.Fatalf("resolveTypedPerson(\"Passenger, Pat\") = %+v, %v — the strict key must see a NEW name, neither adopting nor refusing", slot, err)
	}

	// ⟦R-26⟧ this test is about two folded-spelling offers, not company —
	// give the new row one so the commit is not refused for the reason
	// R-26 added.
	gateFillNewRowCompanies(t, vals, "R-26 Test Co")
	rec := postForm(t, srv, "/gate/checkin", gateMarkDriver(vals, 0))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("commit with both offers ignored = %d, want 303 — two roster Pats must not make a third un-addable: %s", rec.Code, rec.Body.String())
	}
	all, err = domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 {
		t.Fatalf("roster is %d after the commit, want 5 (three fixtures, the second Pat, and the row as typed)", len(all))
	}
	if got := countPersonsNamed(t, all, "Pat Passenger"); got != 2 {
		t.Fatalf("the two roster Pats now number %d, want 2 — nothing may be merged or renamed", got)
	}
	if got := countPersonsNamed(t, all, "Passenger, Pat"); got != 1 {
		t.Fatalf("%d rows for the name as typed, want 1", got)
	}
}
