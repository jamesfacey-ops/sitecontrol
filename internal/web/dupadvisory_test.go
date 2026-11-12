// dupadvisory_test.go — the gate's "possible existing match" advisory
// (dupadvisory.go): a non-blocking note shown after an off-roster
// add-on-demand person is created whose name (and, if the officer typed
// one, company) matches somebody already on the roster.
//
// [[D-66]] — this can NEVER refuse the check-in. Every test below asserts
// the check-in still succeeds (303, and the new person genuinely created)
// alongside whatever the advisory does or does not say. A version of this
// feature that blocks anything is a wrong implementation, not a stricter
// one.
//
// TestGenuinelyNewNameShowsNoAdvisory is the non-alarm test: an officer
// adding a name that matches nobody must see nothing. A note that fires on
// ordinary work trains officers to stop reading it, which defeats the
// point of adding one.
package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// scriptedCheckInBody builds a passenger_list=1 ("live"/scripted) gate
// check-in body by hand — deliberately NOT via gateBuild/gateAdd, because
// those simulate the real accumulator, which resolves an exact roster-name
// match to "existing" itself (the same behaviour resolvePick documents:
// resolveTypedPerson/the client's resolvePick both auto-adopt an
// unambiguous exact match). This advisory exists for the case the roster
// matching does NOT catch: a row that reaches the server explicitly typed
// as "new" — a script race, a bypassed dropdown, or (as here) a name typed
// into a form the ordinary UI would have auto-resolved. driverIdx names
// which row (0-based) carries the driver flag.
func scriptedCheckInBody(vehicle string, driverIdx int, rows []passengerRow) url.Values {
	vals := url.Values{
		"passenger_list": {"1"},
		"direction":      {gateInbound},
		"vehicle":        {vehicle},
		"driver_index":   {strconv.Itoa(driverIdx)},
		"time_source":    {"default"},
	}
	for i, row := range rows {
		vals.Add("occupant_index", strconv.Itoa(i))
		vals.Add("occupant_typed", row.Typed)
		vals.Add("occupant_kind", row.Kind)
		vals.Add("occupant_value", row.Value)
		// ⟦[[D-84]], 2026-09-05⟧ the row's company/trade PICKERS ride beside
		// the escape boxes, exactly as the served row emits them: a known
		// person's row carries their record's ids (the form pre-selects
		// them), a new person's row carries newRef when a name is typed. A
		// body that omits the pickers for a known person reads as "cleared,
		// unconfirmed" and is refused — the real form never omits them.
		vals.Add("occupant_company_id", row.CompanyID)
		vals.Add("occupant_company", row.Company)
		vals.Add("occupant_trade_id", row.TradeID)
		vals.Add("occupant_trade", row.Trade)
	}
	return vals
}

// knownRow is the scripted body's row for a roster person: the id the picker
// resolved, and the record's company/trade as the served row pre-selects
// them (gate.go decorateRow / gate.html showKnown).
func knownRow(p domain.Person) passengerRow {
	row := passengerRow{Typed: p.Name, Kind: "existing", Value: p.PersonID}
	if p.CompanyID != nil {
		row.CompanyID = *p.CompanyID
	}
	if p.TradeID != nil {
		row.TradeID = *p.TradeID
	}
	return row
}

// countPersonsNamed is a MEASURED count (domain.Persons, not an estimate)
// of how many roster rows carry exactly this name — the direct proof that
// the advisory did not refuse anything: a genuine duplicate now exists.
func countPersonsNamed(t *testing.T, all []domain.Person, name string) int {
	t.Helper()
	n := 0
	for _, p := range all {
		if p.Name == name {
			n++
		}
	}
	return n
}

// TestNewOccupantMatchingRosterNameGetsPossibleMatchAdvisory is the
// produced-red case: an officer types "new" for a name that is already on
// the roster (no company given, so the match is on name alone). The
// check-in must still succeed AND create the second person — advisory,
// never a refusal — and the very next page must name the existing
// candidate.
func TestNewOccupantMatchingRosterNameGetsPossibleMatchAdvisory(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	existing := persons[1] // "Pat Passenger"

	// ⟦R-26⟧ this test is about the possible-match advisory, not company —
	// give the deliberately-off-roster row one so the commit is not
	// refused for the reason R-26 added. It must be the SAME company as
	// the candidate (the fixture's own "Kakivik Asset Management", which
	// all three fixture persons share): the advisory itself narrows away a
	// same-name candidate whose GIVEN company disagrees
	// (TestSameNameDifferentCompanyGetsNoAdvisory, below) — a different
	// company here would silently break the very case this test measures.
	rec := postForm(t, srv, "/gate/checkin", scriptedCheckInBody("TRK-14", 0, []passengerRow{
		knownRow(persons[0]),
		{Typed: existing.Name, Kind: "new", Value: existing.Name, CompanyID: newRef, Company: "kakivik asset management"}, // deliberately off-roster despite the exact name match
	}))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("check-in = %d: %s", rec.Code, rec.Body.String())
	}

	all, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if got := countPersonsNamed(t, all, existing.Name); got != 2 {
		t.Fatalf("persons named %q = %d, want 2 — the advisory must not have blocked the second one from being created", existing.Name, got)
	}

	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, dupWarnQueryKey+"="+existing.PersonID) {
		t.Fatalf("redirect Location = %q, want it to carry dup=%s", loc, existing.PersonID)
	}

	body := rawPage(t, srv, loc)
	if !strings.Contains(body, "POSSIBLE EXISTING MATCH") {
		t.Fatalf("landing page does not render the advisory notice:\n%s", body)
	}
	if !strings.Contains(body, existing.Name) {
		t.Fatalf("advisory notice does not name the candidate %q:\n%s", existing.Name, body)
	}
}

// TestGenuinelyNewNameShowsNoAdvisory is the non-alarm test (task's own
// explicit requirement): a name that matches nobody on the roster must
// raise nothing at all.
func TestGenuinelyNewNameShowsNoAdvisory(t *testing.T) {
	srv, st, persons := newGateTestServer(t)

	// ⟦R-26⟧ this test is about the NON-alarm case, not company — give the
	// new row one so the commit is not refused for the reason R-26 added.
	rec := postForm(t, srv, "/gate/checkin", scriptedCheckInBody("TRK-15", 0, []passengerRow{
		knownRow(persons[0]),
		{Typed: "Totally New Guy", Kind: "new", Value: "Totally New Guy", CompanyID: newRef, Company: "R-26 Test Co"},
	}))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("check-in = %d: %s", rec.Code, rec.Body.String())
	}

	all, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if got := countPersonsNamed(t, all, "Totally New Guy"); got != 1 {
		t.Fatalf("persons named %q = %d, want 1", "Totally New Guy", got)
	}

	loc := rec.Header().Get("Location")
	if strings.Contains(loc, dupWarnQueryKey+"=") {
		t.Fatalf("redirect Location = %q, should carry no dup advisory for a genuinely new name", loc)
	}

	body := rawPage(t, srv, loc)
	if strings.Contains(body, "POSSIBLE EXISTING MATCH") {
		t.Fatalf("landing page renders an advisory for a name that matches nobody:\n%s", body)
	}
}

// TestSameNameDifferentCompanyGetsNoAdvisory: the task's own qualifier —
// "same name, and same company if a company is given" — means a company
// given that does NOT match narrows the candidate away. Two "Pat
// Passenger"s at two different companies are a plausible, distinct pair of
// people; guessing they are the same one is worse than saying nothing.
func TestSameNameDifferentCompanyGetsNoAdvisory(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	existing := persons[1] // "Pat Passenger", company "Kakivik Asset Management"

	rec := postForm(t, srv, "/gate/checkin", scriptedCheckInBody("TRK-16", 0, []passengerRow{
		knownRow(persons[0]),
		{Typed: existing.Name, Kind: "new", Value: existing.Name, CompanyID: newRef, Company: "Different Co Ltd"},
	}))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("check-in = %d: %s", rec.Code, rec.Body.String())
	}

	all, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if got := countPersonsNamed(t, all, existing.Name); got != 2 {
		t.Fatalf("persons named %q = %d, want 2", existing.Name, got)
	}

	loc := rec.Header().Get("Location")
	if strings.Contains(loc, dupWarnQueryKey+"=") {
		t.Fatalf("redirect Location = %q, should carry no dup advisory — the typed company does not match the existing person's", loc)
	}
}

// TestSameNameSameCompanyGetsAdvisory: the positive half of the same
// qualifier — a company that DOES match (case-insensitively) still
// advises.
func TestSameNameSameCompanyGetsAdvisory(t *testing.T) {
	srv, _, persons := newGateTestServer(t)
	existing := persons[1] // "Pat Passenger", company "Kakivik Asset Management"

	rec := postForm(t, srv, "/gate/checkin", scriptedCheckInBody("TRK-17", 0, []passengerRow{
		knownRow(persons[0]),
		{Typed: existing.Name, Kind: "new", Value: existing.Name, CompanyID: newRef, Company: "kakivik asset management"},
	}))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("check-in = %d: %s", rec.Code, rec.Body.String())
	}

	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, dupWarnQueryKey+"="+existing.PersonID) {
		t.Fatalf("redirect Location = %q, want it to carry dup=%s (company matches case-insensitively)", loc, existing.PersonID)
	}
}

// TestLegacyOffRosterOccupantMatchingRosterNameGetsAdvisory covers the
// no-JS/legacy check-in body (legacyGateCheckIn) — the same shape
// TestOffRosterOccupantTriggersAddOnDemandAndCompletes already exercises
// for the add-it escape itself. The advisory must fire here too: the
// legacy body is a live, capturing surface (see legacyGateCheckIn's own
// header comment), not a fossil this feature can skip.
func TestLegacyOffRosterOccupantMatchingRosterNameGetsAdvisory(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	existing := persons[1] // "Pat Passenger"

	rec := postForm(t, srv, "/gate/checkin", url.Values{
		"time_source":      {"default"},
		"vehicle":          {"TRK-18"},
		"driver_kind":      {"existing"},
		"driver_value":     {persons[0].PersonID},
		"occupant_kind":    {"new"},
		"occupant_value":   {existing.Name},
		"occupant_typed":   {existing.Name},
		"occupant_company": {""},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("legacy check-in = %d: %s", rec.Code, rec.Body.String())
	}

	all, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if got := countPersonsNamed(t, all, existing.Name); got != 2 {
		t.Fatalf("persons named %q = %d, want 2 — the legacy path's advisory must not refuse either", existing.Name, got)
	}

	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, dupWarnQueryKey+"="+existing.PersonID) {
		t.Fatalf("legacy redirect Location = %q, want it to carry dup=%s", loc, existing.PersonID)
	}
	body := rawPage(t, srv, loc)
	if !strings.Contains(body, "POSSIBLE EXISTING MATCH") {
		t.Fatalf("legacy landing page does not render the advisory notice:\n%s", body)
	}
}
