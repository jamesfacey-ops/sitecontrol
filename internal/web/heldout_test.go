// heldout_test.go — the standing held-out list on /orientation
// (domain.HeldOutAt; orientation.go's HeldOut; the reader half of
// d91-arrival-respec.md §3.4). Four proofs, each by driving the REAL HTTP
// doors (gate_orientation_block_test.go's world and posture — a domain-level
// call would pass whether or not any page ever reads the table):
//  1. a held-out person APPEARS, with who / company / trade / when / why /
//     next — and on no other page;
//  2. they DISAPPEAR once entered — hold out, record the orientation (which,
//     SchemaVersion 12 (R-16), IS the entry: d91-arrival-respec.md §3.4),
//     gone from the list and on the muster — and STAY gone after they later
//     leave (the predicate is not "has an open visit");
//  3. at a road post (orientation = false) nothing is ever held out, so no
//     surface anywhere shows a held-out list;
//  4. an empty list renders as reassurance, not as an error.
package web

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

const heldOutSectionOpen = `<section data-orientation-section="held-out">`

// heldOutPage is GET /orientation as a logged-in officer.
func heldOutPage(t *testing.T, w *orientBlockWorld) string {
	t.Helper()
	rec := w2Get(t, w.srv, "/orientation", loginCookie(t, w.srv, testLoginPassword))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /orientation = %d: %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// heldOutSection is the held-out section's own markup — asserted on the
// section, never the whole page, because the page's own lists name the
// same people (the Lacking table lists every un-oriented person on the
// roster, held out or not).
func heldOutSection(t *testing.T, page string) string {
	t.Helper()
	i := strings.Index(page, heldOutSectionOpen)
	if i < 0 {
		t.Fatalf("/orientation has no held-out section:\n%s", page)
	}
	rest := page[i:]
	j := strings.Index(rest, "</section>")
	if j < 0 {
		t.Fatalf("the held-out section is not closed:\n%s", rest)
	}
	return rest[:j]
}

var heldOutWallRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}`)

// --- 1. a held-out person appears, with what an officer needs to act ------

func TestHeldOutListNamesWhoWhenWhyAndNextOnTheOrientationPage(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	co, err := domain.CreateCompany(w.st, "site-b", 1, "Northwind Drilling", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	tr, err := domain.CreateTrade(w.st, "site-b", 1, "Welder", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	vera, err := domain.CreatePerson(w.st, "site-b", 1, "Vera Unoriented", &co.CompanyID, &tr.TradeID, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// Vera's row carries her record's company and trade, as the live form
	// does for a picked person — a row that disagrees with the record is
	// the [[D-84]] "change their record?" refusal, not a check-in.
	veraRow := passengerRow{Kind: "existing", Value: vera.PersonID, Typed: vera.Name,
		CompanyID: co.CompanyID, Company: co.Name, TradeID: tr.TradeID, Trade: tr.Name}
	if rec := postForm(t, w.srv, "/gate/checkin", scriptedCheckInBody("TRK-81", 0, append(orientBlockRows(w.driver, w.orla), veraRow))); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /gate/checkin = %d: %s", rec.Code, rec.Body.String())
	}

	sec := heldOutSection(t, heldOutPage(t, w))
	for _, want := range []string{
		`data-held-out-person="` + vera.PersonID + `"`,
		"Vera Unoriented",
		"Northwind Drilling",
		"Welder",
		// Template-literal copy: the apostrophe is emitted as written.
		`waiting on Pad Site Control's orientation <span class="count">(1)</span>`,
		"did not hold Pad Site Control's orientation when they arrived",
		"not on site and not on the muster",
		"Orientation not yet recorded",
		"check them in again",
		`<a href="/gate">`,
	} {
		if !strings.Contains(sec, want) {
			t.Errorf("the held-out section lacks %q.\nsection: %s", want, sec)
		}
	}
	if !heldOutWallRe.MatchString(sec) {
		t.Errorf("the held-out section carries no wall time for the hold-out (want YYYY-MM-DD HH:MM):\n%s", sec)
	}
	for _, entered := range []string{"Dana Driver", "Orla Oriented"} {
		if strings.Contains(sec, entered) {
			t.Errorf("the held-out section names %s, who WAS entered:\n%s", entered, sec)
		}
	}

	// Oldest waiting first: Uma, held out on a later vehicle, lists AFTER
	// Vera.
	if rec := postForm(t, w.srv, "/gate/checkin", scriptedCheckInBody("TRK-86", 0, orientBlockRows(w.uma))); rec.Code != http.StatusSeeOther {
		t.Fatalf("second POST /gate/checkin = %d: %s", rec.Code, rec.Body.String())
	}
	sec = heldOutSection(t, heldOutPage(t, w))
	vi, ui := strings.Index(sec, "Vera Unoriented"), strings.Index(sec, "Uma Unoriented")
	if vi < 0 || ui < 0 || vi > ui {
		t.Errorf("want Vera (held out first) listed before Uma; Vera at %d, Uma at %d:\n%s", vi, ui, sec)
	}
	if !strings.Contains(sec, `<span class="count">(2)</span>`) {
		t.Errorf("the count does not read 2:\n%s", sec)
	}

	// Nowhere else: not the muster (D-10 — no muster, no printed sheet),
	// not the gate page as a standing block, and not either ungated
	// console (/trips, the operator console; / , the status page) — the
	// two paths a road post can also reach, so D-10 must hold on them
	// even though a road post cannot register /orientation at all.
	for _, path := range []string{"/muster", "/muster/print", "/gate", "/trips", "/status"} {
		if page := orientBlockGet(t, w.srv, path); strings.Contains(page, "data-orientation-section") || strings.Contains(page, "Held out at the gate") {
			t.Errorf("%s carries the held-out list; it belongs on /orientation only", path)
		}
	}
	if strings.Contains(orientBlockGet(t, w.srv, "/muster"), "Vera Unoriented") {
		t.Error("Vera Unoriented is on the muster")
	}
}

// --- 2. they disappear once entered, and stay gone after leaving ----------

func TestAHeldOutPersonDropsOffTheListOnceEnteredAndStaysOffAfterLeaving(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	if rec := postForm(t, w.srv, "/gate/checkin", scriptedCheckInBody("TRK-82", 0, orientBlockRows(w.driver, w.orla, w.uma))); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /gate/checkin = %d: %s", rec.Code, rec.Body.String())
	}
	umaRow := `data-held-out-person="` + w.uma.PersonID + `"`
	sec := heldOutSection(t, heldOutPage(t, w))
	if strings.Count(sec, umaRow) != 1 {
		t.Fatalf("precondition: Uma is not listed exactly once after the hold-out:\n%s", sec)
	}

	// Held out AGAIN, still un-oriented: two rows in the table, ONE line on
	// the list — and no entry.
	if rec := postForm(t, w.srv, "/gate/checkin", scriptedCheckInBody("TRK-83", 0, orientBlockRows(w.uma))); rec.Code != http.StatusSeeOther {
		t.Fatalf("second POST /gate/checkin = %d: %s", rec.Code, rec.Body.String())
	}
	if n := orientBlockNotEnteredCount(t, w.st); n != 2 {
		t.Fatalf("trip_arrival_not_entered rows = %d after two un-oriented passes, want 2", n)
	}
	sec = heldOutSection(t, heldOutPage(t, w))
	if strings.Count(sec, umaRow) != 1 || !strings.Contains(sec, "Orientation not yet recorded") {
		t.Fatalf("after a second hold-out Uma must list ONCE, still awaiting the orientation:\n%s", sec)
	}
	if orientBlockOpenPersonIDs(t, w.st)[w.uma.PersonID] {
		t.Fatal("a second un-oriented pass through the gate ENTERED Uma")
	}

	// The orientation is recorded. SchemaVersion 12 (R-16): completion IS
	// the entry now — a held-out arrival exists (TRK-83's), so the same
	// event that records the orientation also mints her visit off that
	// arrival. She is entered, on the muster, OFF the list — no separate
	// gate visit required or offered.
	if _, err := domain.CompleteOrientation(w.st, "site-b", 1, w.uma.PersonID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if !orientBlockOpenPersonIDs(t, w.st)[w.uma.PersonID] {
		t.Fatal("completing Uma's orientation against a held-out arrival did not enter her (d91 §3.4)")
	}
	sec = heldOutSection(t, heldOutPage(t, w))
	if strings.Contains(sec, "data-held-out-person=") {
		t.Fatalf("Uma was entered by the completion and is still listed as held out — the list is an append-only pile:\n%s", sec)
	}
	if !strings.Contains(sec, "Nobody is waiting at the gate on Pad Site Control's orientation.") || !strings.Contains(sec, `<span class="count">(0)</span>`) {
		t.Errorf("the emptied list does not say so:\n%s", sec)
	}
	if !strings.Contains(orientBlockGet(t, w.srv, "/muster"), "Uma Unoriented") {
		t.Error("Uma Unoriented is not on the muster after the entering completion")
	}

	// She leaves later. The hold-out rows are still in the table; she must
	// NOT reappear — "entered since" is a later entry, not an open visit.
	open, err := domain.OpenVisitsAt(w.st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	var visitID string
	for _, v := range open {
		if v.PersonID == w.uma.PersonID {
			visitID = v.VisitID
		}
	}
	if visitID == "" {
		t.Fatal("precondition: Uma has no open visit to check out")
	}
	if err := domain.CheckOutVisit(w.st, "site-b", 1, visitID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if orientBlockOpenPersonIDs(t, w.st)[w.uma.PersonID] {
		t.Fatal("precondition: Uma's visit did not close")
	}
	if n := orientBlockNotEnteredCount(t, w.st); n != 2 {
		t.Fatalf("the raw rows changed (%d); they are INSERT-only and must still be 2", n)
	}
	if sec = heldOutSection(t, heldOutPage(t, w)); strings.Contains(sec, "data-held-out-person=") {
		t.Fatalf("Uma REAPPEARED on the held-out list after leaving — the predicate must be 'entered since', not 'on site now':\n%s", sec)
	}
	if rows, err := domain.HeldOutAt(w.st, "site-b"); err != nil || len(rows) != 0 {
		t.Fatalf("HeldOutAt(site-b) = %+v (%v), want none", rows, err)
	}
}

// --- 3. a road post never holds anyone out; no surface shows a list -------

func TestARoadPostHoldsNobodyOutSoNoSurfaceShowsAHeldOutList(t *testing.T) {
	w := newOrientBlockWorld(t, false)
	if w.srv.Cfg.HasOrientation() {
		t.Fatal("precondition: this world must be a road post")
	}
	crew := []domain.Person{orientBlockPerson(t, w.st, "Road Driver"), orientBlockPerson(t, w.st, "Road One"), orientBlockPerson(t, w.st, "Road Two")}
	if rec := postForm(t, w.srv, "/gate/checkin", scriptedCheckInBody("RD-9", 0, orientBlockRows(crew...))); rec.Code != http.StatusSeeOther {
		t.Fatalf("road post POST /gate/checkin = %d: %s", rec.Code, rec.Body.String())
	}
	if n := orientBlockNotEnteredCount(t, w.st); n != 0 {
		t.Fatalf("a road post wrote %d trip_arrival_not_entered row(s)", n)
	}
	if rows, err := domain.HeldOutAt(w.st, "site-b"); err != nil || len(rows) != 0 {
		t.Fatalf("HeldOutAt at a road post = %+v (%v), want none", rows, err)
	}
	open := orientBlockOpenPersonIDs(t, w.st)
	for _, p := range crew {
		if !open[p.PersonID] {
			t.Errorf("%s has no open visit at the road post", p.Name)
		}
	}
	// /orientation is not registered here (web.go): the GET falls through to
	// the status page. No page anywhere carries the section or its heading.
	cookie := loginCookie(t, w.srv, testLoginPassword)
	if body := w2Get(t, w.srv, "/orientation", cookie).Body.String(); strings.Contains(body, "data-orientation-section") || strings.Contains(body, "Held out at the gate") {
		t.Errorf("a road post served a held-out list at /orientation:\n%s", body)
	}
	for _, path := range []string{"/gate", "/muster", "/trips", "/status"} {
		if page := orientBlockGet(t, w.srv, path); strings.Contains(page, "data-orientation-section") || strings.Contains(page, "Held out at the gate") {
			t.Errorf("a road post's %s carries a held-out list", path)
		}
	}
}

// --- 4. an empty list is reassurance, not an error -----------------------

func TestAnEmptyHeldOutListReadsAsReassuranceNotAnError(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	rec := postForm(t, w.srv, "/gate/checkin", scriptedCheckInBody("TRK-85", 0, orientBlockRows(w.driver, w.orla)))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /gate/checkin (all oriented) = %d: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); strings.Contains(loc, notEnteredQueryKey+"=") {
		t.Fatalf("an all-oriented check-in redirected with a hold-out: %q", loc)
	}
	sec := heldOutSection(t, heldOutPage(t, w))
	if !strings.Contains(sec, "Nobody is waiting at the gate on Pad Site Control's orientation.") {
		t.Errorf("the empty list does not state that nobody is waiting:\n%s", sec)
	}
	if !strings.Contains(sec, `<span class="count">(0)</span>`) {
		t.Errorf("the empty list does not count zero:\n%s", sec)
	}
	for _, alarm := range []string{"data-held-out-person=", "notice-warn", "NOT ENTERED", "not entered", "error", "Error"} {
		if strings.Contains(sec, alarm) {
			t.Errorf("the empty held-out section carries %q — it must read as reassurance:\n%s", alarm, sec)
		}
	}
}
