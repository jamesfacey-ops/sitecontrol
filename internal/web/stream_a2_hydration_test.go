package web

// Stream A2 (2026-09-29) — cluster B of the campaign findings: record-field
// hydration on the /gate individual form and on the JS-off re-render.
//
//	C-2 / F-L02-3  JS off: after a refusal, retyping a DIFFERENT name
//	               re-checked-in the PREVIOUS person (hidden id honoured
//	               over the visible boxes). CRITICAL.
//	H-1 / F-L02-1  JS on: a type-ahead pick of a known person posts a blank
//	               company and no trade; the commit read them as "clear" and
//	               refused, coaching a tick /gate does not have. HIGH.
//	M-14 / G-102   JS off, vehicle page: a row renamed to another person kept
//	               the OLD person's hidden company/trade and was refused.
//
// The last test is the guard the fix must not break: a genuine [[D-84]]
// mismatch on the vehicle door is still refused without the tick and still
// applied with it (FIX-CONSTRAINTS: the tick must never reach /gate).

import (
	"net/http"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

func a2OpenVisitsOf(t *testing.T, srv *Server, personID string) []domain.Visit {
	t.Helper()
	open, err := domain.OpenVisitsAt(srv.Store, srv.Cfg.SiteID)
	if err != nil {
		t.Fatal(err)
	}
	var out []domain.Visit
	for _, v := range open {
		if v.PersonID == personID {
			out = append(out, v)
		}
	}
	return out
}

func a2PersonNamed(t *testing.T, srv *Server, name string) *domain.Person {
	t.Helper()
	all, err := domain.Persons(srv.Store)
	if err != nil {
		t.Fatal(err)
	}
	var hit *domain.Person
	for i := range all {
		if strings.EqualFold(strings.TrimSpace(all[i].Name), name) {
			if hit != nil {
				t.Fatalf("two roster people are called %q", name)
			}
			hit = &all[i]
		}
	}
	return hit
}

func a2Deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// TestGateIndividualRetypeAfterARefusalChecksInTheTypedPersonNotThePrevious
// is F-L02-3's exact JS-off sequence: Solo (a gate-minted person, company,
// NO trade — the shape that made the wrong-person commit silent) is checked
// in; a second attempt for Solo is refused over a placeholder vehicle, and
// the refusal re-renders Solo's id into the hidden field; the officer then
// types a different, new person over it and presses CHECK IN. The person
// typed must be the one recorded, and Solo's visit must not move.
func TestGateIndividualRetypeAfterARefusalChecksInTheTypedPersonNotThePrevious(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	co := *persons[0].CompanyID
	solo, err := domain.CreatePerson(st, "site-a", 1, "Solo", &co, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	// 1. Solo checks in.
	vals := singleCheckInForm(t, srv, "Solo")
	vals.Set("new_company_id", co)
	if rec := postForm(t, srv, "/gate/checkin/single", vals); rec.Code != http.StatusSeeOther {
		t.Fatalf("setup: Solo's check-in = %d: %s", rec.Code, rec.Body.String())
	}
	before := a2OpenVisitsOf(t, srv, solo.PersonID)
	if len(before) != 1 {
		t.Fatalf("setup: Solo has %d open visit(s), want 1", len(before))
	}

	// 2. Solo again, placeholder vehicle -> refused.
	vals = singleCheckInForm(t, srv, "Solo")
	vals.Set("new_company_id", co)
	vals.Set("vehicle", "x")
	rec := postForm(t, srv, "/gate/checkin/single", vals)
	if rec.Code == http.StatusSeeOther || !strings.Contains(rec.Body.String(), "Nothing was recorded") {
		t.Fatalf("setup: a placeholder vehicle was not refused (%d)", rec.Code)
	}
	re := singleCheckInFormValues(t, rec.Body.String())
	// Rule 6: the scenario needs the refusal to have left Solo's id in the
	// hidden field — without it this test poses nothing.
	if re.Get("new_kind") != "existing" || re.Get("new_value") != solo.PersonID {
		t.Fatalf("precondition: the refusal re-render does not carry Solo's id in the hidden field (kind=%q value=%q) — the stale-id case is not posed", re.Get("new_kind"), re.Get("new_value"))
	}

	// 3. The officer types somebody else over it (scripting off: the hidden
	// field is not touched) and presses CHECK IN.
	re.Set("new_last", "Zqfresh")
	re.Set("new_first", "Nu")
	re.Set("new_company_id", co)
	re.Set("vehicle", "TRK-88")
	rec = postForm(t, srv, "/gate/checkin/single", re)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the retyped check-in = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	fresh := a2PersonNamed(t, srv, "Zqfresh, Nu")
	if fresh == nil {
		t.Fatal("the person typed at the window (Zqfresh, Nu) was never added — the hidden id of the PREVIOUS person was honoured over the visible name")
	}
	if got := a2OpenVisitsOf(t, srv, fresh.PersonID); len(got) != 1 {
		t.Fatalf("Zqfresh, Nu has %d open visit(s), want 1 — not on the muster", len(got))
	}
	after := a2OpenVisitsOf(t, srv, solo.PersonID)
	if len(after) != 1 || after[0].VisitID != before[0].VisitID {
		t.Fatalf("Solo's visit moved (%v -> %v): the previous person was re-checked-in under the new name", before, after)
	}
}

// TestGateIndividualRetypeAfterARefusalDoesNotTransferTheOrientationTick is
// the same sequence at an orientation post (L02 G-047 addendum): the tick
// was given for A, the refusal re-rendered A, the officer retyped B. A must
// NOT be given this post's orientation or be entered, and the tick must not
// silently become B's either.
func TestGateIndividualRetypeAfterARefusalDoesNotTransferTheOrientationTick(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	uma := w.uma
	bo := orientBlockPerson(t, w.st, "Bo Bystander")
	for _, p := range []domain.Person{uma, bo} {
		if held, err := domain.OrientationHeld(w.st, p.PersonID, "site-b"); err != nil || held != nil {
			t.Fatalf("precondition: %s already holds the pad orientation (err=%v)", p.Name, err)
		}
	}

	_, page := getGateIndividual(t, w.srv)
	vals := singleCheckInFormValues(t, page)
	vals.Set("new_last", uma.Name)
	vals.Set("new_first", "")
	vals.Set("add_orientation", "1")
	vals.Set("vehicle", "x")
	rec := postForm(t, w.srv, "/gate/checkin/single", vals)
	if rec.Code == http.StatusSeeOther {
		t.Fatalf("setup: a placeholder vehicle was not refused")
	}
	re := singleCheckInFormValues(t, rec.Body.String())
	if re.Get("new_value") != uma.PersonID || re.Get("add_orientation") == "" {
		t.Fatalf("precondition: the refusal did not re-render A's id and the tick (value=%q tick=%q)", re.Get("new_value"), re.Get("add_orientation"))
	}

	re.Set("new_last", bo.Name)
	re.Set("vehicle", "TRK-89")
	rec = postForm(t, w.srv, "/gate/checkin/single", re)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the retyped check-in = %d: %s", rec.Code, rec.Body.String())
	}
	if held, _ := domain.OrientationHeld(w.st, uma.PersonID, "site-b"); held != nil {
		t.Fatal("A (Uma) was given the pad orientation for a check-in typed as B")
	}
	if got := a2OpenVisitsOf(t, w.srv, uma.PersonID); len(got) != 0 {
		t.Fatal("A (Uma) was entered at the pad for a check-in typed as B")
	}
	if held, _ := domain.OrientationHeld(w.st, bo.PersonID, "site-b"); held != nil {
		t.Fatal("B was given the orientation ticked for A — the tick transferred silently")
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "stale_orient_tick="+uma.PersonID) {
		t.Fatalf("the officer is not told the tick did not apply: Location %q", loc)
	}
}

// TestGateIndividualScriptedPickOfAKnownPersonWithATradeChecksInUnchanged is
// F-L02-1 with the addendum's person: the body is the JS type-ahead pick's
// exact shape (kind=existing, the id, the record's Name in the Last box,
// First disabled so absent, new_company_id BLANK, no trade/position/phone
// field at all), for a person whose record has BOTH a company and a trade.
// It must check in, and the record must be untouched.
func TestGateIndividualScriptedPickOfAKnownPersonWithATradeChecksInUnchanged(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	chris := persons[2]
	if chris.CompanyID == nil || chris.TradeID == nil {
		t.Fatal("precondition: the fixture person must have a company AND a trade on record")
	}
	pos, phone := "Foreman", "555-0101"
	if _, err := domain.AmendPerson(st, "site-a", 1, domain.PersonAmendment{PersonID: chris.PersonID, Position: &pos, Phone: &phone}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	before := d84Person(t, st, chris.PersonID)

	_, page := getGateIndividual(t, srv)
	vals := singleCheckInFormValues(t, page)
	vals.Set("new_kind", "existing")
	vals.Set("new_value", chris.PersonID)
	vals.Set("new_last", chris.Name)
	vals.Del("new_first") // disabled by the script on a pick: not submitted
	vals.Set("new_company_id", "")
	vals.Set("new_company", "")
	for _, k := range []string{"new_trade_id", "new_trade", "new_position", "new_phone"} {
		if vals.Has(k) {
			t.Fatalf("precondition: the individual page now carries %s — the JS pick shape this test models has changed", k)
		}
	}
	rec := postForm(t, srv, "/gate/checkin/single", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("a scripted pick of a known person = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	if got := a2OpenVisitsOf(t, srv, chris.PersonID); len(got) != 1 {
		t.Fatalf("Chris has %d open visit(s), want 1", len(got))
	}
	after := d84Person(t, st, chris.PersonID)
	if a2Deref(after.CompanyID) != a2Deref(before.CompanyID) || a2Deref(after.TradeID) != a2Deref(before.TradeID) ||
		a2Deref(after.Position) != pos || a2Deref(after.Phone) != phone {
		t.Fatalf("the record changed: company %q->%q trade %q->%q position %q phone %q",
			a2Deref(before.CompanyID), a2Deref(after.CompanyID), a2Deref(before.TradeID), a2Deref(after.TradeID), a2Deref(after.Position), a2Deref(after.Phone))
	}
}

// TestGateVehicleRenamedRowTakesTheNewPersonsRecord is M-14 (L03 G-102): JS
// off, a row added as Dana is renamed to Fenwick, who has a DIFFERENT
// company and trade. The commit must check in Fenwick with Fenwick's record
// untouched — not refuse over Dana's hidden pickers, and not check in Dana.
func TestGateVehicleRenamedRowTakesTheNewPersonsRecord(t *testing.T) {
	srv, st, persons, pipe, nanuq := d84Server(t)
	dana, pat := persons[0], persons[1]
	fen, err := domain.CreatePerson(st, "site-a", 1, "Fenwick Sage", &nanuq.CompanyID, &pipe.TradeID, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	_, vals := gateBuild(t, srv, gateInbound, "TRK-14", []string{dana.Name, pat.Name}, 1)
	if vals["occupant_value"][0] != dana.PersonID || vals["occupant_company_id"][0] != *dana.CompanyID || vals["occupant_trade_id"][0] != *dana.TradeID {
		t.Fatalf("precondition: row 0 does not carry Dana's id and pickers (%v %v %v)", vals["occupant_value"], vals["occupant_company_id"], vals["occupant_trade_id"])
	}
	d84Set(t, vals, "occupant_typed", 0, fen.Name) // renamed; hidden id and pickers untouched

	rec := postForm(t, srv, "/gate/checkin", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the renamed row's commit = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	if got := a2OpenVisitsOf(t, srv, fen.PersonID); len(got) != 1 {
		t.Fatalf("Fenwick has %d open visit(s), want 1", len(got))
	}
	if got := a2OpenVisitsOf(t, srv, dana.PersonID); len(got) != 0 {
		t.Fatal("Dana was checked in under Fenwick's name")
	}
	after := d84Person(t, st, fen.PersonID)
	if a2Deref(after.CompanyID) != nanuq.CompanyID || a2Deref(after.TradeID) != pipe.TradeID {
		t.Fatalf("Fenwick's record took Dana's details: company %q trade %q", a2Deref(after.CompanyID), a2Deref(after.TradeID))
	}
}

// TestD84VehicleDoorCompanyMismatchStillNeedsTheTick is the guard: the fix
// fills BLANKS on the individual door only. On the vehicle door a genuine
// company change is still refused unticked and applied ticked, and "none on
// file" + the tick is still a confirmed clear — exactly as before.
func TestD84VehicleDoorCompanyMismatchStillNeedsTheTick(t *testing.T) {
	srv, st, persons, _, nanuq := d84Server(t)
	dana, pat := persons[0], persons[1]

	_, vals := gateBuild(t, srv, gateInbound, "TRK-14", []string{dana.Name}, 0)
	d84Set(t, vals, "occupant_company_id", 0, nanuq.CompanyID)
	if rec := postForm(t, srv, "/gate/checkin", vals); rec.Code != http.StatusConflict {
		t.Fatalf("an unticked company change = %d, want 409", rec.Code)
	}
	if got := a2Deref(d84Person(t, st, dana.PersonID).CompanyID); got != *dana.CompanyID {
		t.Fatalf("the record changed without the tick: %q", got)
	}
	vals.Add("amend_confirm", "0")
	if rec := postForm(t, srv, "/gate/checkin", vals); rec.Code != http.StatusSeeOther {
		t.Fatalf("the ticked company change = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	if got := a2Deref(d84Person(t, st, dana.PersonID).CompanyID); got != nanuq.CompanyID {
		t.Fatalf("the ticked change was not applied: company %q", got)
	}

	_, vals = gateBuild(t, srv, gateInbound, "TRK-15", []string{pat.Name}, 0)
	d84Set(t, vals, "occupant_company_id", 0, "")
	vals.Add("amend_confirm", "0")
	if rec := postForm(t, srv, "/gate/checkin", vals); rec.Code != http.StatusSeeOther {
		t.Fatalf("the ticked clear = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	if got := d84Person(t, st, pat.PersonID).CompanyID; got != nil {
		t.Fatalf("\"none on file\" + tick on the vehicle door no longer clears: %q", *got)
	}
}
