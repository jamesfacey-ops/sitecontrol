// r26_company_required_test.go — R-26 (James, 2026-09-13, told a hard
// requirement breaks ~16 tests including one enforcing his own earlier "the
// gate never refuses anyone" / "the escape never refuses": "Refuse the
// check-in — company really is required."; his spec: "the onle required
// information is last name first name and company").
//
// This is the headline demonstration, on BOTH check-in rhythms, that:
//   - a check-in that would create a new person with NO company at all is
//     REFUSED and authors nothing (D-3/comint-on-refusal: no trace at all,
//     not even a partial one);
//   - the "not listed — add a new company" ESCAPE never refuses: typing a
//     name through it always proceeds, whether it folds to an existing
//     company or mints a genuinely new one;
//   - a check-in where a company WAS picked from the list still succeeds.
//
// See gate.go's requireCompanyForNewPersons and the doc comment above the
// two commits (handleGateCheckIn/handleGateCheckInSingle) for the
// implementation and the narrow reading of the supersession this enforces.
package web

import (
	"net/http"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// r26Counts snapshots persons/open-visits/vehicles so a refusal can be
// proven to have authored NOTHING — the same shape
// TestARefusedGateCheckInSingleAuthorsNothing already uses.
type r26Counts struct{ persons, open, vehicles int }

func r26Snapshot(t *testing.T, srv *Server) r26Counts {
	t.Helper()
	persons, err := domain.Persons(srv.Store)
	if err != nil {
		t.Fatal(err)
	}
	open, err := domain.OpenVisitsAt(srv.Store, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	vehicles, err := domain.Vehicles(srv.Store)
	if err != nil {
		t.Fatal(err)
	}
	return r26Counts{len(persons), len(open), len(vehicles)}
}

// --- 1. GROUP RHYTHM: no company at all is refused, authors nothing ------

func TestR26GroupCheckInWithNoCompanyIsRefusedAndAuthorsNothing(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	before := r26Snapshot(t, srv)

	_, vals := gateBuild(t, srv, gateInbound, "TRK-26", []string{"Newcomer Nadia"}, 0)
	// Deliberately NOT calling gateFillNewRowCompanies — this is the exact
	// case R-26 closes: a new person, no company picked, no escape typed.
	rec := postForm(t, srv, "/gate/checkin", vals)
	if rec.Code != http.StatusConflict {
		t.Fatalf("check-in of a new person with no company = %d, want 409 (refused): %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Newcomer Nadia", "company is required", "Nothing has been recorded", "list below is intact"} {
		if !strings.Contains(body, want) {
			t.Errorf("the refusal lacks %q — the officer must be told who, why and that the list survives:\n%s", want, body)
		}
	}
	after := r26Snapshot(t, srv)
	if after != before {
		t.Fatalf("a REFUSED check-in authored something: %+v -> %+v", before, after)
	}
}

// --- 2. SINGLE-PERSON RHYTHM: the same rule, through R-25's other door ---

func TestR26SingleCheckInWithNoCompanyIsRefusedAndAuthorsNothing(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	before := r26Snapshot(t, srv)

	vals := singleCheckInForm(t, srv, "Solo Newcomer")
	// Deliberately NOT setting new_company_id/new_company.
	rec := postForm(t, srv, "/gate/checkin/single", vals)
	if rec.Code != http.StatusConflict {
		t.Fatalf("single check-in of a new person with no company = %d, want 409 (refused): %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Solo Newcomer", "company is required", "Nothing has been recorded", "list below is intact"} {
		if !strings.Contains(body, want) {
			t.Errorf("the single-door refusal lacks %q:\n%s", want, body)
		}
	}
	after := r26Snapshot(t, srv)
	if after != before {
		t.Fatalf("a REFUSED single check-in authored something: %+v -> %+v", before, after)
	}
}

// --- 3. THE ESCAPE STILL NEVER REFUSES ------------------------------------

// TestR26TheAddNewCompanyEscapeStillNeverRefuses walks both escape shapes —
// a typed name that folds to a LISTED company (resolves, mints nothing) and
// a genuinely new one (mints exactly one) — and both succeed, on both
// rhythms. [[D-71]] obligation 2 is untouched by R-26: the officer is never
// stuck, they can always supply a company on the spot.
func TestR26TheAddNewCompanyEscapeStillNeverRefuses(t *testing.T) {
	t.Run("group rhythm, folded to a listed company", func(t *testing.T) {
		srv, st, _ := newGateTestServer(t)
		before, err := domain.Companies(st)
		if err != nil {
			t.Fatal(err)
		}
		_, vals := gateBuild(t, srv, gateInbound, "TRK-27", []string{"Escape Eddie"}, 0)
		vals.Set("occupant_company_id", newRef)
		vals.Set("occupant_company", "kakivik asset management") // folds to the fixture's own company
		rec := postForm(t, srv, "/gate/checkin", vals)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("check-in via the escape (folded name) = %d, want 303: %s", rec.Code, rec.Body.String())
		}
		after, err := domain.Companies(st)
		if err != nil {
			t.Fatal(err)
		}
		if len(after) != len(before) {
			t.Fatalf("a folded escape name minted a company: %d -> %d", len(before), len(after))
		}
	})
	t.Run("group rhythm, genuinely new company", func(t *testing.T) {
		srv, st, _ := newGateTestServer(t)
		before, err := domain.Companies(st)
		if err != nil {
			t.Fatal(err)
		}
		_, vals := gateBuild(t, srv, gateInbound, "TRK-28", []string{"Escape Erin"}, 0)
		vals.Set("occupant_company_id", newRef)
		vals.Set("occupant_company", "Brand New Logistics Co")
		rec := postForm(t, srv, "/gate/checkin", vals)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("check-in via the escape (new name) = %d, want 303: %s", rec.Code, rec.Body.String())
		}
		after, err := domain.Companies(st)
		if err != nil {
			t.Fatal(err)
		}
		if len(after) != len(before)+1 {
			t.Fatalf("a genuinely new escape name minted %d companies, want +1: %d -> %d", len(after)-len(before), len(before), len(after))
		}
	})
	t.Run("single-person rhythm, genuinely new company", func(t *testing.T) {
		srv, st, _ := newGateTestServer(t)
		vals := singleCheckInForm(t, srv, "Solo Escapee")
		vals.Set("new_company_id", newRef)
		vals.Set("new_company", "Yet Another New Co")
		rec := postForm(t, srv, "/gate/checkin/single", vals)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("single check-in via the escape = %d, want 303: %s", rec.Code, rec.Body.String())
		}
		open, err := domain.OpenVisitsAt(st, "site-a")
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, v := range open {
			if v.PersonSnapshotName != nil && *v.PersonSnapshotName == "Solo Escapee" {
				found = true
			}
		}
		if !found {
			t.Fatalf("Solo Escapee is not on site after the single check-in via the escape: %+v", open)
		}
	})
}

// --- 4. A COMPANY PICKED FROM THE LIST STILL SUCCEEDS ---------------------

func TestR26ACheckInWithACompanyPickedFromTheListStillSucceeds(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	kakivik := companyIDNamed(t, srv, "Kakivik Asset Management")

	_, vals := gateBuild(t, srv, gateInbound, "TRK-29", []string{"Picked Paula"}, 0)
	vals.Set("occupant_company_id", kakivik)
	rec := postForm(t, srv, "/gate/checkin", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("check-in with a picked company = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	paula := personNamed(t, srv, "Picked Paula")
	if len(paula) != 1 {
		t.Fatalf("%d rows for Picked Paula, want 1", len(paula))
	}
	if paula[0].CompanyID == nil || *paula[0].CompanyID != kakivik {
		t.Fatalf("Picked Paula's company = %v, want %s", paula[0].CompanyID, kakivik)
	}
}
