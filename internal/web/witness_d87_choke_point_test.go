// witness_d87_choke_point_test.go — D-87 at the two web surfaces that reach
// the two presence edges which carried no own-site check before the choke
// point (d86-ownership §3a #17 /arrivals/amend, #18 /arrivals/unref).
//
// The refusal must reach the officer as a REFUSAL they can act on — 409, the
// D-87 sentence, the post to radio — never the internal-fault rendering
// ("NOT RECORDED … the fault has been logged") and never the unref page's
// read-failure arm, both of which send the officer to the wrong remedy.
package web

import (
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// d87ForeignArrival: site-b takes a gate check-in INTO THE SAME STORE the
// site-a server reads (the post-replication picture: site-a holds site-b's
// trip, arrival and visit rows and owns none of them). Returns site-b's
// arrival id and, as the positive control, a site-a arrival of the same
// shape that site-a DOES own.
func d87ForeignArrival(t *testing.T, st *store.Store, persons []domain.Person) (foreign, own string) {
	t.Helper()
	checkIn := func(site, vehicle string, driver domain.Person) string {
		num := vehicle
		v, err := domain.CreateVehicle(st, site, 1, &num, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		_, arr, err := domain.GateCheckIn(st, site, 1, domain.GateCheckInInput{
			VehicleID: v.VehicleID, Driver: domain.PersonSlot{ExistingID: driver.PersonID},
		}, 0, "default")
		if err != nil {
			t.Fatalf("check-in at %s: %v", site, err)
		}
		if arr.SiteID != site {
			t.Fatalf("fixture: arrival tagged %q, want %q", arr.SiteID, site)
		}
		return arr.ArrivalID
	}
	co, err := domain.CreateCompany(st, "site-b", 1, "Nanuq Drilling Services", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	fred, err := domain.CreatePerson(st, "site-b", 1, "Faraway, Fred", &co.CompanyID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	return checkIn("site-b", "TRK-B", fred), checkIn("site-a", "TRK-A", persons[0])
}

func TestD87TheArrivalAmendSurfaceRefusesAnotherPostsArrivalAsARefusalNotAFault(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	foreign, own := d87ForeignArrival(t, st, persons)
	before := w2Watermark(t, st, "site-a", 1)

	rec := postForm(t, srv, "/arrivals/amend", url.Values{"arrival_id": {foreign}, "vehicle_entered": {"TRK-B (corrected at site-a)"}})
	// The sentence reaches this page through {{.Error}}, so its apostrophes
	// arrive as &#39; — compare the text the officer READS, not the bytes.
	body := html.UnescapeString(rec.Body.String())
	if rec.Code != http.StatusConflict {
		t.Fatalf("POST /arrivals/amend against site-b's arrival = %d, want 409; body:\n%s", rec.Code, body)
	}
	// ⟦was: store.OwnershipRule, which ended "(D-87)" — a decision id on an
	// officer-visible refusal. FIX5 pins the officer's words literally and
	// that no decision id is shown.⟧
	if !strings.Contains(body, fix5OwnershipWords) || !strings.Contains(body, "site-b") {
		t.Errorf("the 409 does not carry the D-87 sentence and the owning post; body:\n%s", body)
	}
	if strings.Contains(body, "(D-87)") {
		t.Errorf("the refusal shows the decision id \"(D-87)\" to the officer; body:\n%s", body)
	}
	if strings.Contains(body, "NOT RECORDED") || strings.Contains(body, "fault has been logged") {
		t.Errorf("the refusal was rendered as an INTERNAL FAULT — the wrong remedy for the officer; body:\n%s", body)
	}
	if after := w2Watermark(t, st, "site-a", 1); after != before {
		t.Errorf("site-a appended %d event(s) while refusing", after-before)
	}

	// POSITIVE CONTROL: the same form against site-a's OWN arrival lands —
	// a 303 AND exactly one appended event; the status alone is not a
	// witness that anything was recorded.
	rec = postForm(t, srv, "/arrivals/amend", url.Values{"arrival_id": {own}, "vehicle_entered": {"TRK-A (corrected at site-a)"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("CONTROL: amending site-a's own arrival = %d, want 303; body:\n%s", rec.Code, rec.Body.String())
	}
	if got := w2Watermark(t, st, "site-a", 1); got != before+1 {
		t.Errorf("CONTROL: the 303 appended %d event(s) at site-a (%d → %d), want exactly 1", got-before, before, got)
	}
}

func TestD87TheUnrefSurfaceRefusesAnotherPostsArrivalNamingThePostToRadio(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	foreign, own := d87ForeignArrival(t, st, persons)
	before := w2Watermark(t, st, "site-a", 1)

	rec := postForm(t, srv, "/arrivals/unref", url.Values{"arrival_id": {foreign}})
	body := rec.Body.String()
	if rec.Code != http.StatusConflict {
		t.Fatalf("POST /arrivals/unref against site-b's arrival = %d, want 409; body:\n%s", rec.Code, body)
	}
	// ⟦was: store.OwnershipRule, which ended "(D-87)" — a decision id on an
	// officer-visible refusal. FIX5 pins the officer's words literally and
	// that no decision id is shown.⟧
	if !strings.Contains(body, fix5OwnershipWords) || !strings.Contains(body, "Radio "+srv.Cfg.SiteName("site-b")) {
		t.Errorf("the 409 does not carry the D-87 sentence and 'Radio site-b'; body:\n%s", body)
	}
	if strings.Contains(body, "(D-87)") {
		t.Errorf("the refusal shows the decision id \"(D-87)\" to the officer; body:\n%s", body)
	}
	if strings.Contains(body, "withdrawal could not be") {
		t.Errorf("the refusal fell to the READ-FAILURE arm — the wrong remedy; body:\n%s", body)
	}
	if after := w2Watermark(t, st, "site-a", 1); after != before {
		t.Errorf("site-a appended %d event(s) while refusing", after-before)
	}

	// POSITIVE CONTROL: withdrawing site-a's OWN arrival's reference lands —
	// a 303 AND exactly one appended event.
	rec = postForm(t, srv, "/arrivals/unref", url.Values{"arrival_id": {own}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("CONTROL: un-referencing site-a's own arrival = %d, want 303; body:\n%s", rec.Code, rec.Body.String())
	}
	if got := w2Watermark(t, st, "site-a", 1); got != before+1 {
		t.Errorf("CONTROL: the 303 appended %d event(s) at site-a (%d → %d), want exactly 1", got-before, before, got)
	}
}
