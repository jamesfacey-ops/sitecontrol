// zz_window_audit_test.go — WINDOW-AUDIT witnesses (audit-only, never to be
// merged). Each test asserts the SAFE behaviour; a FAIL is the demonstration.
package web

import (
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

func auditOpenVisitWeb(t *testing.T, w *orientBlockWorld, personID string) *domain.Visit {
	t.Helper()
	vs, err := domain.OpenVisitsAt(w.st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	for i := range vs {
		if vs[i].PersonID == personID {
			return &vs[i]
		}
	}
	return nil
}

func auditNotice(page string) string {
	i := strings.Index(page, `vehicle-error"`)
	if i < 0 {
		return ""
	}
	rest := page[i:]
	j := strings.Index(rest, "</div>")
	if j < 0 {
		return rest
	}
	return rest[:j]
}

// D3 — the cross-site tick is authored BEFORE the check-in door validates the
// vehicle. A refused check-in tells the officer "Nothing was recorded" while
// (a) the orientation completion stands and (b) — if the person was ever held
// out at this gate before — entry-on-completion has put them on the muster.
func TestAuditTickedOrientationIsRecordedAndEntersEvenWhenTheCheckInIsRefused(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	vera := orientBlockPerson(t, w.st, "Vera Visitor")
	if _, err := domain.CompleteOrientation(w.st, "site-a", 1, vera.PersonID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// Day 1: Vera arrives, the officer does not tick — held out (R-9, case b)
	rec := postForm(t, w.srv, "/gate/checkin", scriptedCheckInBody("TRK-88", 0, orientBlockRows(w.driver, vera)))
	if rec.Code != 303 || !strings.Contains(rec.Header().Get("Location"), "not_entered=") {
		t.Fatalf("PRECONDITION: day-1 check-in = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if auditOpenVisitWeb(t, w, vera.PersonID) != nil {
		t.Fatal("PRECONDITION: Vera was entered on day 1")
	}
	// Day 2: Vera is back, alone, in a truck; the officer ticks "Add Pad Site
	// Control's orientation for Vera now, and check them in" — and leaves the
	// vehicle box blank.
	body := scriptedCheckInBody("", 0, orientBlockRows(vera))
	body.Add("add_orientation", "0")
	rec2 := postForm(t, w.srv, "/gate/checkin", body)
	page := rec2.Body.String()
	notice := auditNotice(page)
	t.Logf("day-2 response %d; notice: %s", rec2.Code, notice)
	if rec2.Code != 409 {
		t.Fatalf("PRECONDITION: expected the vehicle refusal (409), got %d", rec2.Code)
	}
	if held, _ := domain.OrientationHeld(w.st, vera.PersonID, "site-b"); held != nil {
		t.Errorf("DEFECT: the page says \"Nothing was recorded\" (present=%v) but Vera's site-b orientation WAS recorded (completion at epoch %d seq %d)", strings.Contains(notice, "Nothing was recorded"), held.NodeEpoch, held.Seq)
	}
	if v := auditOpenVisitWeb(t, w, vera.PersonID); v != nil {
		t.Errorf("DEFECT: Vera is ON THE MUSTER (open visit %s, trip_id=%v) after a REFUSED check-in — entry-on-completion fired from day 1's hold-out row", v.VisitID, v.TripID)
	}
	// and if the officer now abandons (Vera's truck turns round), she stays on
	// the muster; the redirect target never told the officer she was entered.
}

// D3b — same mechanism, the refusal coming from ANOTHER row (an off-roster
// name at an orientation post, R-12): one passenger's tick enters them while
// the whole vehicle is refused.
func TestAuditOneRowsTickEntersWhileTheVehicleIsRefusedForAnotherRow(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	vera := orientBlockPerson(t, w.st, "Vera Visitor")
	if _, err := domain.CompleteOrientation(w.st, "site-a", 1, vera.PersonID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	rec := postForm(t, w.srv, "/gate/checkin", scriptedCheckInBody("TRK-88", 0, orientBlockRows(w.driver, vera)))
	if rec.Code != 303 {
		t.Fatalf("PRECONDITION: %d", rec.Code)
	}
	rows := orientBlockRows(vera)
	rows = append(rows, passengerRow{Kind: "new", Value: "", Typed: "Nobody Newname"})
	body := scriptedCheckInBody("TRK-89", 0, rows)
	body.Add("add_orientation", "0")
	rec2 := postForm(t, w.srv, "/gate/checkin", body)
	notice := auditNotice(rec2.Body.String())
	t.Logf("response %d; notice: %s", rec2.Code, notice)
	if rec2.Code != 409 {
		t.Fatalf("PRECONDITION: expected R-12's refusal (409), got %d", rec2.Code)
	}
	if v := auditOpenVisitWeb(t, w, vera.PersonID); v != nil {
		t.Errorf("DEFECT: the vehicle was refused (R-12, another row) yet Vera is ON THE MUSTER (open visit %s) — her tick was authored before the door ran", v.VisitID)
	}
}

// D2 at the desk — the same re-arm through POST /orientation/complete: does
// the confirmation at least SAY the person was entered?
func TestAuditDeskRecompletionEntersAndSaysSo(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	vera := orientBlockPerson(t, w.st, "Vera Visitor")
	rec := postForm(t, w.srv, "/gate/checkin", scriptedCheckInBody("TRK-88", 0, orientBlockRows(w.driver, vera)))
	if rec.Code != 303 {
		t.Fatalf("PRECONDITION: %d", rec.Code)
	}
	// desk: complete → entered (intended); gate: check out; desk: retract
	if _, err := domain.CompleteOrientation(w.st, "site-b", 1, vera.PersonID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	v := auditOpenVisitWeb(t, w, vera.PersonID)
	if v == nil {
		t.Fatal("PRECONDITION: not entered")
	}
	if err := domain.CheckOutVisit(w.st, "site-b", 1, v.VisitID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if _, err := domain.RetractOrientation(w.st, "site-b", 1, vera.PersonID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// a week later, the desk records her orientation again
	body := url.Values{"person_id": {vera.PersonID}}
	rec2 := postForm(t, w.srv, "/orientation/complete", body)
	loc := rec2.Header().Get("Location")
	t.Logf("POST /orientation/complete → %d %s", rec2.Code, loc)
	page := getPage(t, w.srv, loc)
	if v2 := auditOpenVisitWeb(t, w, vera.PersonID); v2 != nil {
		i := strings.Index(page, "Vera Visitor is")
		snippet := ""
		if i >= 0 {
			snippet = page[i:min(i+160, len(page))]
		}
		t.Errorf("DEFECT: a desk completion re-ENTERED Vera (open visit %s). Confirmation copy: %q", v2.VisitID, snippet)
	}
}
