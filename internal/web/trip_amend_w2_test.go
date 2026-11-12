// trip_amend_w2_test.go — fork workstream W2 (trip corrections), 2026-10-03.
//
// NEW-b (James): a driver correction on an OPEN trip keeps the previous
// driver on the trip as a passenger, in the same amendment, or refuses in
// words — and every muster shows them exactly once. M-4 (James): a
// destination correction must name a known site even at a post whose own
// destination list is empty. Plus the L09 LOW copy items on this surface
// (T-141, T-151, T-155, T-159, T-164, T-165c/d/f). Each test here reads the
// page an officer reads, through srv.Handler(), after a real login.
package web

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// w2TripWorld: the origin post (site-a, "Ice Road") with TRK-72 on the road
// to site-b — A driving, P aboard, B on the roster with no visit and no trip.
type w2TripWorld struct {
	srv  *Server
	st   *store.Store
	trip domain.Trip
}

const (
	w2PrevDriver = "p-w2-torstad"
	w2Passenger  = "p-w2-ravton"
	w2NextDriver = "p-w2-dunby"
	w2PrevName   = "Torstad, Owen"
	w2PaxName    = "Ravton, Val"
	w2NextName   = "Dunby, Logan"
)

func newW2TripWorld(t *testing.T, dbName string) *w2TripWorld {
	t.Helper()
	srv, st := w9aOriginServer(t, dbName)
	w9aSeedPerson(t, st, "site-r", 1, w2PrevDriver, w2PrevName)
	w9aSeedPerson(t, st, "site-r", 1, w2Passenger, w2PaxName)
	w9aSeedPerson(t, st, "site-r", 1, w2NextDriver, w2NextName)
	trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRK-72", w2PrevDriver, []string{w2Passenger}, 65)
	if s, _ := domain.TripStateByID(st, trip.TripID); s == nil || s.State != domain.TripStateOpen {
		t.Fatalf("PRECONDITION: trip %s is %+v, want open", trip.TripID, s)
	}
	if open, _ := domain.OpenVisitsAt(st, "site-a"); len(open) != 0 {
		t.Fatalf("PRECONDITION: %d open visit(s) at site-a before the test starts, want none", len(open))
	}
	return &w2TripWorld{srv: srv, st: st, trip: trip}
}

// amend POSTs the trip-amend form and returns the recorder, the session
// cookie and — on a redirect — the confirmation page the officer lands on.
func (w *w2TripWorld) amend(t *testing.T, form url.Values) (*httptest.ResponseRecorder, *http.Cookie, string) {
	t.Helper()
	form.Set("trip_id", w.trip.TripID)
	rec, cookie := w2PostForm(t, w.srv, "/trips/amend", form)
	if rec.Code != http.StatusSeeOther {
		return rec, cookie, ""
	}
	loc := rec.Result().Header.Get("Location")
	next := w2Get(t, w.srv, loc, cookie)
	if next.Code != http.StatusOK {
		t.Fatalf("GET %s (the confirmation) = %d: %s", loc, next.Code, next.Body.String())
	}
	return rec, cookie, html.UnescapeString(next.Body.String())
}

// musterText is the muster page as text — the sheet the invariant is
// stated over: a person is LISTED there exactly once. The road block's
// "Driver: <strong>X</strong>." meta line names the driver a second time
// beside the names list; it is a label on the vehicle, not a listing of a
// person, so it is blanked before counting (the names list and the on-site
// roster are what the count measures). w3PageText lower-cases, so names are
// counted through w2Times.
func (w *w2TripWorld) musterText(t *testing.T) string {
	t.Helper()
	page := w2DriverMeta.ReplaceAllString(w9aGetOK(t, w.srv, "/muster"), "Driver: -")
	return html.UnescapeString(w3PageText(page))
}

var w2DriverMeta = regexp.MustCompile(`Driver: <strong>[^<]*</strong>`)

// w2Times counts a name on lower-cased page text.
func w2Times(text, name string) int { return strings.Count(text, strings.ToLower(name)) }

// ---------------------------------------------------------------------
// NEW-b — the driver handover on the rendered surfaces
// ---------------------------------------------------------------------

// TestW2DriverCorrectionOnAnOpenTripKeepsThePreviousDriverOnTheMusterOnce:
// the live repro. FAIL BEFORE: after `Driver = B` the muster's road list no
// longer named A anywhere (and, because B had no visit either, the trip row
// vanished from the origin's sheet while the destination still counted it).
func TestW2DriverCorrectionOnAnOpenTripKeepsThePreviousDriverOnTheMusterOnce(t *testing.T) {
	w := newW2TripWorld(t, "w2-handover.db")
	before := w.musterText(t)
	if n := w2Times(before, w2PrevName); n != 1 {
		t.Fatalf("PRECONDITION: before any correction the muster names %s %d time(s), want exactly 1:\n%s", w2PrevName, n, before)
	}
	wm := w2Watermark(t, w.st, "site-a", 1)

	rec, _, page := w.amend(t, url.Values{"driver_person_id": {w2NextDriver}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("a plain driver correction on an open trip = %d, want 303; body:\n%s", rec.Code, html.UnescapeString(rec.Body.String()))
	}
	if got := w2Watermark(t, w.st, "site-a", 1); got != wm+1 {
		t.Fatalf("the correction authored %d event(s), want exactly 1 — the kept-aboard add must ride the same amendment", got-wm)
	}
	// The confirmation says both things that were recorded.
	if !strings.Contains(page, "Recorded. Effective: driver.") {
		t.Errorf("the confirmation does not record the driver; page text:\n%s", w3PageText(page))
	}
	if !strings.Contains(page, w2PrevName+" is on the trip's list (aboard)") {
		t.Errorf("the confirmation does not say %s stays aboard; page text:\n%s", w2PrevName, w3PageText(page))
	}

	after := w.musterText(t)
	if n := w2Times(after, w2PrevName); n != 1 {
		t.Fatalf("after the correction the muster names the previous driver %s %d time(s), want exactly 1 (on the road, as a passenger):\n%s", w2PrevName, n, after)
	}
	if n := w2Times(after, w2PaxName); n != 1 {
		t.Errorf("the passenger %s appears %d time(s), want 1", w2PaxName, n)
	}
	if raw := w9aGetOK(t, w.srv, "/muster"); !strings.Contains(raw, "Driver: <strong>"+w2NextName+"</strong>") {
		t.Errorf("the road block does not name the corrected driver %s", w2NextName)
	}
	// And the destination's reader agrees — the two musters cannot differ.
	exps, err := domain.OpenExpectations(w.st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, e := range exps {
		for _, p := range e.OccupantList {
			if p.PersonID == w2PrevDriver {
				count++
			}
		}
	}
	if count != 1 {
		t.Fatalf("the destination's expectation lists %s %d time(s), want exactly 1", w2PrevName, count)
	}
}

// TestW2DriverCorrectionRefusedWhenThePreviousDriverIsOnSiteNamesThePost:
// A is back on site at the origin. The refusal names the post in the
// officer's words ("Ice Road", the configured display name) and the way
// out; taking the way out lands and leaves A on exactly one sheet.
func TestW2DriverCorrectionRefusedWhenThePreviousDriverIsOnSiteNamesThePost(t *testing.T) {
	w := newW2TripWorld(t, "w2-handover-onsite.db")
	checkIn(t, w.st, "site-a", w2PrevDriver, nil)
	open, _ := domain.OpenVisitsAt(w.st, "site-a")
	onSite := false
	for _, v := range open {
		onSite = onSite || v.PersonID == w2PrevDriver
	}
	if !onSite {
		t.Fatalf("PRECONDITION: %s has no open visit at site-a", w2PrevName)
	}
	wm := w2Watermark(t, w.st, "site-a", 1)

	rec, _, _ := w.amend(t, url.Values{"driver_person_id": {w2NextDriver}})
	body := html.UnescapeString(rec.Body.String())
	if rec.Code != http.StatusConflict {
		t.Fatalf("driver correction while the previous driver is on site = %d, want 409; body:\n%s", rec.Code, body)
	}
	for _, want := range []string{"REFUSED", w2PrevName, "recorded on site at Ice Road", `"Remove from the trip's list"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the refusal does not say %q; page text:\n%s", want, w3PageText(body))
		}
	}
	if got := w2Watermark(t, w.st, "site-a", 1); got != wm {
		t.Fatalf("refused but authored %d event(s)", got-wm)
	}

	// The way out: A is sighted (open visit), so [[D-80]] lets the removal
	// through, record-only.
	rec, _, _ = w.amend(t, url.Values{"driver_person_id": {w2NextDriver}, "occupants_remove": {w2PrevDriver}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("driver + removal of the on-site previous driver = %d, want 303; body:\n%s", rec.Code, html.UnescapeString(rec.Body.String()))
	}
	after := w.musterText(t)
	if n := w2Times(after, w2PrevName); n != 1 {
		t.Fatalf("after the way out the muster names %s %d time(s), want exactly 1 (on site):\n%s", w2PrevName, n, after)
	}
}

// ---------------------------------------------------------------------
// M-4 / T-157 — a destination correction at a `destinations = []` post
// ---------------------------------------------------------------------

// TestW2DestinationCorrectionAtAPostWithNoDestinationsMustNameAPeer — the
// NPF shape, loaded from TOML so `destinations = []` is the real declared-
// empty list and not a derived one. FAIL BEFORE: "banana" was recorded and
// replicated to all five posts.
func TestW2DestinationCorrectionAtAPostWithNoDestinationsMustNameAPeer(t *testing.T) {
	st := w3OpenRegistered(t, "w2-m4.db")
	toml := `
site_id               = "npf"
display_name          = "NPF"
listen_addr           = "127.0.0.1:0"
node_epoch            = 1
db_path               = "` + st.Path + `"
sync_interval_seconds = 10
login_password        = "` + testLoginPassword + `"
orientation           = false
site_type             = "text_message"
destinations          = []

[[peers]]
site_id  = "site-a"
base_url = "http://127.0.0.1:0"
key      = "k"

[[peers]]
site_id  = "site-b"
base_url = "http://127.0.0.1:0"
key      = "k"
`
	path := filepath.Join(t.TempDir(), "npf.toml")
	if err := os.WriteFile(path, []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading the NPF-shaped config: %v", err)
	}
	if got := cfg.DestinationList(); len(got) != 0 {
		t.Fatalf("PRECONDITION: the post derived destinations %+v; the M-4 shape is an EMPTY list", got)
	}
	srv := New(cfg, st)

	w9aSeedPerson(t, st, "site-r", 1, "p-w2-m4", "Harmere, Logan")
	trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRK-11", "p-w2-m4", nil, 65)
	if _, err := domain.RecordExpectedArrival(st, "site-b", 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if s, _ := domain.TripStateByID(st, trip.TripID); s == nil || s.State != domain.TripStateArrived {
		t.Fatalf("PRECONDITION: trip is %+v, want arrived (a Class-B correction is only legal on a completed trip)", s)
	}
	wm := w2Watermark(t, st, "npf", 1)

	rec, _ := w2PostForm(t, srv, "/trips/amend", url.Values{
		"trip_id": {trip.TripID}, "destination_kind": {"controlled"}, "destination_value": {"banana"}, "deadline_minutes": {"5"},
	})
	body := html.UnescapeString(rec.Body.String())
	if rec.Code != http.StatusConflict {
		t.Fatalf("M-4: destination \"banana\" at a destinations=[] post = %d, want 409; body:\n%s", rec.Code, w3PageText(body))
	}
	if !strings.Contains(body, `"banana" is not a destination`) {
		t.Errorf("M-4: the refusal does not name banana; page text:\n%s", w3PageText(body))
	}
	if got := w2Watermark(t, st, "npf", 1); got != wm {
		t.Fatalf("M-4: refused but authored %d event(s)", got-wm)
	}
	if c, _ := domain.EffectiveTrip(st, trip.TripID); c == nil || c.Trip.DestinationSite == nil || *c.Trip.DestinationSite != "site-b" {
		t.Fatalf("M-4: the composed destination moved off site-b: %+v", c)
	}

	// POSITIVE CONTROL: a real peer is a known site and lands.
	rec, _ = w2PostForm(t, srv, "/trips/amend", url.Values{
		"trip_id": {trip.TripID}, "destination_kind": {"controlled"}, "destination_value": {"site-a"}, "deadline_minutes": {"65"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("CONTROL: correcting the destination to peer site-a = %d, want 303; body:\n%s", rec.Code, w3PageText(html.UnescapeString(rec.Body.String())))
	}
	if got := w2Watermark(t, st, "npf", 1); got != wm+1 {
		t.Fatalf("CONTROL: the 303 appended %d event(s), want 1", got-wm)
	}
}

// ---------------------------------------------------------------------
// L09 LOW copy items on this surface
// ---------------------------------------------------------------------

// T-155: a number typed wrong is not an id typed wrong.
func TestW2NonNumericAllowanceRefusalDoesNotSendTheOfficerToCheckAnId(t *testing.T) {
	w := newW2TripWorld(t, "w2-t155.db")
	rec, _, _ := w.amend(t, url.Values{"destination_kind": {"controlled"}, "destination_value": {"site-b"}, "deadline_minutes": {"ninety"}})
	body := html.UnescapeString(rec.Body.String())
	if rec.Code != http.StatusConflict || !strings.Contains(body, "the travel allowance must be a whole number of minutes") {
		t.Fatalf("T-155: = %d, want 409 with the allowance sentence; body:\n%s", rec.Code, w3PageText(body))
	}
	if strings.Contains(body, "Check the id against the record") {
		t.Fatalf("T-155: a number-format mistake is still followed by \"Check the id against the record\"; page text:\n%s", w3PageText(body))
	}
}

// T-159: "driver" typed into "Un-set a field" gets the whitelist sentence.
func TestW2UnsettingTheWordDriverGetsTheCannotBeUnsetSentence(t *testing.T) {
	w := newW2TripWorld(t, "w2-t159.db")
	rec, _, _ := w.amend(t, url.Values{"clear": {"driver"}})
	body := html.UnescapeString(rec.Body.String())
	if rec.Code != http.StatusConflict || !strings.Contains(body, `"driver" cannot be un-set`) {
		t.Fatalf("T-159: = %d, want 409 with the cannot-be-un-set sentence; body:\n%s", rec.Code, w3PageText(body))
	}
	if strings.Contains(body, "asserts nothing") {
		t.Fatalf("T-159: the officer who typed \"driver\" is still told they asserted nothing")
	}
}

// T-151: the overdue Class-B refusal no longer warns off an operation D-78
// removed.
func TestW2OverdueClassBRefusalNoLongerTellsTheOfficerNotToCancel(t *testing.T) {
	srv, _, trip := w9aOverdueAtDestination(t)
	rec, _ := w2PostForm(t, srv, "/trips/amend", url.Values{"trip_id": {trip.TripID}, "vehicle_entered": {"ICE TRUCK 8"}})
	body := html.UnescapeString(rec.Body.String())
	if rec.Code != http.StatusConflict || !strings.Contains(body, "this vehicle is OVERDUE and its alert is live") {
		t.Fatalf("T-151: = %d, want 409 with the OVERDUE rendering; body:\n%s", rec.Code, w3PageText(body))
	}
	if strings.Contains(body, "Do not cancel") || strings.Contains(body, "cancelling takes the alert") {
		t.Fatalf("T-151: the refusal still talks about cancelling, an operation [[D-78]] removed; page text:\n%s", w3PageText(body))
	}
	if !strings.Contains(body, "Verify the vehicle by radio and resolve the alert first") {
		t.Fatalf("T-151: the working path is gone from the refusal; page text:\n%s", w3PageText(body))
	}
}

// T-164: the losing driver arm names the person; a removal's confirmation
// says it is a removal.
func TestW2SupersededDriverArmNamesThePersonAndARemovalSaysSo(t *testing.T) {
	w := newW2TripWorld(t, "w2-t164.db")
	rec1, cookie, _ := w.amend(t, url.Values{"driver_person_id": {w2NextDriver}})
	rec2, _, page2 := w.amend(t, url.Values{"driver_person_id": {w2Passenger}})
	if rec1.Code != http.StatusSeeOther || rec2.Code != http.StatusSeeOther {
		t.Fatalf("fixture: the two driver corrections = %d, %d, want 303, 303", rec1.Code, rec2.Code)
	}
	if !strings.Contains(page2, w2NextName+" is on the trip's list (aboard)") {
		t.Errorf("T-164: the handover of %s is not confirmed as aboard; page text:\n%s", w2NextName, w3PageText(page2))
	}
	first := html.UnescapeString(w2Get(t, w.srv, rec1.Result().Header.Get("Location"), cookie).Body.String())
	if !strings.Contains(first, "superseded") {
		t.Fatalf("T-164 fixture: reopening the first correction does not render it superseded; page text:\n%s", w3PageText(first))
	}
	if !strings.Contains(first, "the record shows "+w2PaxName) {
		t.Errorf("T-164: the losing driver arm does not name the standing driver %s; page text:\n%s", w2PaxName, w3PageText(first))
	}
	if strings.Contains(first, "the record shows "+w2Passenger) {
		t.Errorf("T-164: the losing driver arm still shows the raw person id %s", w2Passenger)
	}

	// A removal, on the arrived trip. The arrival mints visits for the trip's
	// RAW occupants (A and P), so A is sighted and [[D-80]] passes the
	// removal as record-only; B, aboard only by amendment, got no visit from
	// the arrival and would be refused as unsighted — a pre-existing property
	// of the arrival door, noted in W2's report, not changed here.
	if _, err := domain.RecordExpectedArrival(w.st, "site-b", 1, w.trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	rec3, _, page3 := w.amend(t, url.Values{"occupants_remove": {w2PrevDriver}})
	if rec3.Code != http.StatusSeeOther {
		t.Fatalf("T-164 fixture: removing a sighted passenger = %d, want 303; body:\n%s", rec3.Code, w3PageText(html.UnescapeString(rec3.Body.String())))
	}
	if !strings.Contains(page3, w2PrevName+" is removed from the trip's list") {
		t.Errorf("T-164: the removal's confirmation does not say it is a removal; page text:\n%s", w3PageText(page3))
	}
}

// T-141: the correction surface is reachable from the trips detail, and
// shows the record it is about to correct, ids included.
func TestW2TripsDetailLinksToTheCorrectionAndTheAmendPageShowsTheRecordWithIds(t *testing.T) {
	w := newW2TripWorld(t, "w2-t141.db")
	cookie := loginCookie(t, w.srv, testLoginPassword)
	detail := w2Get(t, w.srv, "/trips/detail", cookie).Body.String()
	link := `href="/trips/amend?trip_id=` + w.trip.TripID + `"`
	if !strings.Contains(detail, link) {
		t.Fatalf("T-141: /trips/detail carries no %s link", link)
	}
	page := html.UnescapeString(w2Get(t, w.srv, "/trips/amend?trip_id="+w.trip.TripID, cookie).Body.String())
	for _, want := range []string{"What the record says now", w2PrevName, "<code>" + w2PrevDriver + "</code>", w2PaxName, "<code>" + w2Passenger + "</code>", "TRK-72"} {
		if !strings.Contains(page, want) {
			t.Errorf("T-141: the amend page opened for the trip does not show %q", want)
		}
	}
	// An unknown trip id renders the form and no record — never an error.
	rec := w2Get(t, w.srv, "/trips/amend?trip_id=no-such-trip", cookie)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "What the record says now") {
		t.Fatalf("T-141: an unknown trip id = %d / record block shown=%v, want 200 and no block", rec.Code, strings.Contains(rec.Body.String(), "What the record says now"))
	}
}

// T-165c / T-165d / T-165 note: the D-87 refusal of /arrivals/amend is
// authored copy, the origin claim is labelled in words, and no refusal of a
// form-less POST promises a form.
func TestW2ArrivalAmendRefusalsAreAuthoredCopy(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	foreign, own := d87ForeignArrival(t, st, persons)

	rec := postForm(t, srv, "/arrivals/amend", url.Values{"arrival_id": {foreign}, "vehicle_entered": {"TRK-B (corrected)"}})
	body := html.UnescapeString(rec.Body.String())
	if rec.Code != http.StatusConflict {
		t.Fatalf("T-165c: = %d, want 409; body:\n%s", rec.Code, w3PageText(body))
	}
	if strings.Contains(body, "store: author:") {
		t.Errorf("T-165c: a raw Go error string reaches the officer; page text:\n%s", w3PageText(body))
	}
	// ⟦was: store.OwnershipRule, which ended "(D-87)" — a decision id on an
	// officer-visible refusal. FIX5 pins the officer's words literally and
	// that no decision id is shown.⟧
	if strings.Contains(body, "(D-87)") {
		t.Errorf("T-165c: the refusal shows the decision id \"(D-87)\" to the officer; page text:\n%s", w3PageText(body))
	}
	for _, want := range []string{fix5OwnershipWords, "site-b", "Radio"} {
		if !strings.Contains(body, want) {
			t.Errorf("T-165c: the refusal does not carry %q; page text:\n%s", want, w3PageText(body))
		}
	}
	if strings.Contains(body, "still on the form below") {
		t.Errorf("T-165: a refusal of POST /arrivals/amend promises a form this page does not have for it")
	}

	rec = postForm(t, srv, "/arrivals/amend", url.Values{"arrival_id": {own}, "origin_site": {"site-b"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("T-165d fixture: correcting the own arrival's origin = %d, want 303; body:\n%s", rec.Code, w3PageText(html.UnescapeString(rec.Body.String())))
	}
	page := html.UnescapeString(w2Get(t, srv, rec.Result().Header.Get("Location"), loginCookie(t, srv, testLoginPassword)).Body.String())
	if !strings.Contains(page, "Effective: origin post, as claimed.") {
		t.Errorf("T-165d: the confirmation does not label the origin claim in words; page text:\n%s", w3PageText(page))
	}
	if strings.Contains(w3PageText(page), "origin_site") {
		t.Errorf("T-165d: the wire name origin_site still reaches the officer")
	}
}

// T-165f: the record-only note sits inside the claim's parenthesis, so the
// review-lane sentence that follows it still parses.
func TestW2CorrectedClaimDoesNotSpliceIntoTheReviewLaneSentence(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	arrival, _, _ := c1cParkAt(t, st, "site-a", "TRK-777", [2]domain.Person{persons[0], persons[1]}, persons[2])
	before := html.UnescapeString(c1cGet(t, srv, "/trips"))
	if !strings.Contains(before, "(claimed: vehicle TRK-777, from site-c) matched more than one open trip") {
		t.Fatalf("PRECONDITION: the review lane does not render the parked arrival's claim as expected; page text:\n%s", w3PageText(before))
	}
	rec := postForm(t, srv, "/arrivals/amend", url.Values{"arrival_id": {arrival}, "vehicle_entered": {"TRK-778"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("fixture: correcting the claim = %d, want 303; body:\n%s", rec.Code, w3PageText(html.UnescapeString(rec.Body.String())))
	}
	after := html.UnescapeString(c1cGet(t, srv, "/trips"))
	const want = "(claimed: vehicle TRK-778, from site-c; this claim was corrected — record-only, the matching was not re-run) matched more than one open trip"
	if !strings.Contains(after, want) {
		t.Fatalf("T-165f: the corrected claim does not read %q; page text:\n%s", want, w3PageText(after))
	}
	if strings.Contains(after, "re-run the matching matched") {
		t.Fatalf("T-165f: the spliced sentence is still on the board")
	}
}
