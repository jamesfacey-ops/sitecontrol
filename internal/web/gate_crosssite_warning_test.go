// gate_crosssite_warning_test.go — the D-88 CROSS-SITE ORIENTATION WARNING
// (James, 2026-09-10, an addition under R-12): "so now if someone shows up
// already in the system at site 1 but is coming to site 2 and is attempting
// to sign in we need a warning that they have not gone through site 2's
// oriantation. with a add oriantation check box in the warning for the
// current site."
//
// Reuses orientBlockWorld (gate_orientation_block_test.go) — a site-b pad
// (display name "Pad Site Control") with orientation=true — and its helpers.
// The scenario: a person already exists in the OVERALL system (here,
// oriented at "site-a" — a different site than the one under test, proving
// the warning is about THIS post's orientation specifically, not a global
// flag) and turns up at site-b's gate.
//
// Four cases (a-d), plus a fifth proving the tick authors the SAME SHAPE of
// event as POST /orientation/complete.
//
// A SIXTH case was added after James's clarification (2026-09-10): "it
// should not cause the same error for site 3's oriantation if they are at
// site 2" — a post consults ONLY its own orientation, in either direction.
// See TestCrossSiteWarningNeverConsultsOtherSitesOrientation.
package web

import (
	"database/sql"
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// crossSiteWarningText extracts the text of the cross-site orientation
// warning notice(s) on a gate page, tags mostly intact (asserted on the
// notice itself, never the whole body).
func crossSiteWarningText(page string) string {
	var out []string
	rest := page
	for {
		i := strings.Index(rest, `class="notice notice-warn cross-site-orientation"`)
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

// completionRecordedBy reads orientation_completion's recorded_by for one
// (person, site) pair — the raw table both POST /orientation/complete and
// the gate tick append to — nil if no row exists.
func completionRecordedBy(t *testing.T, st *store.Store, personID, siteID string) []sql.NullString {
	t.Helper()
	db, err := sql.Open("sqlite", st.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT recorded_by FROM orientation_completion WHERE person_id = ? AND site_id = ? ORDER BY seq`, personID, siteID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []sql.NullString
	for rows.Next() {
		var by sql.NullString
		if err := rows.Scan(&by); err != nil {
			t.Fatal(err)
		}
		out = append(out, by)
	}
	return out
}

// --- (a) known, oriented ELSEWHERE, warning names THIS site --------------

// TestCrossSiteWarningNamesThisSiteForAPersonOrientedElsewhere is case (a):
// Vera Visitor already holds site-a's orientation (a different site — she
// is genuinely "already in the system") but not site-b's. Accumulating her
// onto a site-b vehicle (the accumulate route, which authors nothing) must
// show a warning naming site-b's display name, with an UNTICKED checkbox.
func TestCrossSiteWarningNamesThisSiteForAPersonOrientedElsewhere(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	vera := orientBlockPerson(t, w.st, "Vera Visitor")
	if _, err := domain.CompleteOrientation(w.st, "site-a", 1, vera.PersonID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	body := scriptedCheckInBody("TRK-88", 0, orientBlockRows(w.driver, vera))
	body.Set("new_last", ""); body.Set("new_first", "") // an accumulate render, not an add
	rec := postForm(t, w.srv, "/gate/passenger/add", body)
	if rec.Code != 200 {
		t.Fatalf("POST /gate/passenger/add = %d: %s", rec.Code, rec.Body.String())
	}
	page := rec.Body.String()
	warning := crossSiteWarningText(page)
	if warning == "" {
		t.Fatalf("no cross-site orientation warning rendered for Vera Visitor (oriented at site-a, not site-b).\npage: %s", page)
	}
	for _, want := range []string{
		"NOT ORIENTED HERE",
		"Vera Visitor has not completed Pad Site Control's orientation",
		"there is no override",
		`name="add_orientation" value="1"`, // Vera is row index 1 (driver is 0)
	} {
		if !strings.Contains(warning, want) {
			t.Errorf("the cross-site warning lacks %q.\nwarning: %s", want, warning)
		}
	}
	// checkedattr (templatefuncs.go) emits a literal " checked" attribute
	// only when true — checked against the checkbox's OWN tag, not the
	// surrounding sentence, which legitimately contains the substring
	// "checked in" twice.
	if strings.Contains(warning, `value="1" checked>`) {
		t.Errorf("the add_orientation checkbox is PRE-TICKED — it must default unticked.\nwarning: %s", warning)
	}
	// The driver (already holds site-b's orientation) gets no warning.
	if strings.Contains(page, "Dana Driver has not completed") {
		t.Errorf("a warning was rendered for Dana Driver, who already holds site-b's orientation")
	}
}

// --- (b) submitted WITHOUT ticking: still refused / held out -------------

// TestCrossSiteWarningUntickedStillHoldsOut is case (b): the vehicle commits
// (it is not refused whole — that would weaken R-9's own partition
// behaviour), but Vera specifically is NOT entered, exactly as R-9's
// existing mechanism already does, and her site-a orientation is
// untouched — nothing was recorded for site-b.
func TestCrossSiteWarningUntickedStillHoldsOut(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	vera := orientBlockPerson(t, w.st, "Vera Visitor")
	if _, err := domain.CompleteOrientation(w.st, "site-a", 1, vera.PersonID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	body := scriptedCheckInBody("TRK-89", 0, orientBlockRows(w.driver, vera))
	// add_orientation deliberately OMITTED — the officer did not tick it.
	rec := postForm(t, w.srv, "/gate/checkin", body)
	if rec.Code != 303 {
		t.Fatalf("POST /gate/checkin = %d, want 303 (the vehicle still commits; only Vera is held out): %s", rec.Code, rec.Body.String())
	}
	open := orientBlockOpenPersonIDs(t, w.st)
	if open[vera.PersonID] {
		t.Fatalf("Vera Visitor has an OPEN VISIT at site-b despite an unticked cross-site warning")
	}
	if !open[w.driver.PersonID] {
		t.Fatalf("the driver (unaffected by the warning) did not enter")
	}
	held, err := domain.OrientationHeld(w.st, vera.PersonID, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if held != nil {
		t.Fatalf("site-b's orientation was recorded for Vera despite the checkbox being left unticked: %+v", held)
	}
	// Her site-a orientation is untouched.
	if held, err := domain.OrientationHeld(w.st, vera.PersonID, "site-a"); err != nil || held == nil {
		t.Fatalf("Vera's PRE-EXISTING site-a orientation was disturbed: held=%v err=%v", held, err)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, notEnteredQueryKey+"="+vera.PersonID) {
		t.Fatalf("redirect %q does not carry Vera as held out", loc)
	}
	if notice := notEnteredNoticeText(orientBlockGet(t, w.srv, loc)); !strings.Contains(notice, "Vera Visitor does not hold Pad Site Control's orientation") {
		t.Errorf("no NOT ENTERED notice for Vera after an unticked cross-site warning: %s", notice)
	}
}

// --- (c) submitted WITH the tick: recorded, entered, off the held-out list --

// TestCrossSiteWarningTickedRecordsEntersAndClearsHeldOut is case (c): the
// same scenario, but the officer ticks add_orientation for Vera's row. Her
// site-b orientation is authored, she is checked in, and she leaves the
// held-out list. Her PRE-EXISTING site-a orientation must be untouched
// (two independent (person, site) rows, not one flag rewritten).
func TestCrossSiteWarningTickedRecordsEntersAndClearsHeldOut(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	vera := orientBlockPerson(t, w.st, "Vera Visitor")
	if _, err := domain.CompleteOrientation(w.st, "site-a", 1, vera.PersonID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// Precondition: hold her out once first (an earlier vehicle, unticked),
	// so the held-out list has something in it to clear — the stronger
	// proof of "leaves the held-out list", not merely "was never on it".
	preBody := scriptedCheckInBody("TRK-90A", 0, orientBlockRows(w.driver, vera))
	if rec := postForm(t, w.srv, "/gate/checkin", preBody); rec.Code != 303 {
		t.Fatalf("precondition check-in = %d: %s", rec.Code, rec.Body.String())
	}
	heldOutBefore, err := domain.HeldOutAt(w.st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range heldOutBefore {
		if h.PersonID == vera.PersonID {
			found = true
		}
	}
	if !found {
		t.Fatalf("precondition: Vera is not on the held-out list before the ticked submission: %+v", heldOutBefore)
	}

	body := scriptedCheckInBody("TRK-90B", 0, orientBlockRows(w.driver, vera))
	body.Add("add_orientation", "1") // Vera is row index 1
	rec := postForm(t, w.srv, "/gate/checkin", body)
	if rec.Code != 303 {
		t.Fatalf("POST /gate/checkin (ticked) = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if strings.Contains(loc, notEnteredQueryKey+"=") {
		t.Fatalf("a TICKED submission still redirected with a hold-out: %q", loc)
	}
	open := orientBlockOpenPersonIDs(t, w.st)
	if !open[vera.PersonID] {
		t.Fatalf("Vera Visitor has no open visit after a ticked cross-site warning")
	}
	held, err := domain.OrientationHeld(w.st, vera.PersonID, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if held == nil {
		t.Fatalf("site-b's orientation was NOT recorded for Vera despite the tick")
	}
	if held, err := domain.OrientationHeld(w.st, vera.PersonID, "site-a"); err != nil || held == nil {
		t.Fatalf("Vera's PRE-EXISTING site-a orientation was disturbed by the site-b tick: held=%v err=%v", held, err)
	}
	heldOutAfter, err := domain.HeldOutAt(w.st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range heldOutAfter {
		if h.PersonID == vera.PersonID {
			t.Fatalf("Vera Visitor is STILL on the held-out list after entering: %+v", heldOutAfter)
		}
	}
	if notice := notEnteredNoticeText(orientBlockGet(t, w.srv, loc)); strings.Contains(notice, "Vera Visitor") {
		t.Errorf("a NOT ENTERED notice still names Vera after a ticked, successful entry: %s", notice)
	}
	muster := orientBlockGet(t, w.srv, "/muster")
	if !strings.Contains(muster, "Vera Visitor") {
		t.Errorf("Vera Visitor is not on the muster after a ticked cross-site entry")
	}
}

// --- (c2) TWO un-held rows, ONLY ONE ticked: the tick must not spill -----

// TestCrossSiteWarningTickAppliesOnlyToTheTickedRowNotEveryUnorientedRow is
// the harder case FALSIFIABILITY-2.md's M-A names: every existing fixture
// (this file's cases (a)-(c), gate_orientation_doors_test.go's doors) puts
// EXACTLY ONE un-held row on the vehicle, so a per-row tick and "any tick
// on this vehicle authors every row lacking this post's orientation" are
// indistinguishable — both pass every one of them. This vehicle carries
// TWO people who lack site-b's orientation (Vera, cross-site; Nora, never
// oriented anywhere) and tickets only Vera's row. If the officer's single
// tick were ever read as "the vehicle is cleared" rather than "this row is
// cleared", Nora would be entered and marked oriented at site-b without the
// orientation happening — a false record on the insurance-facing muster,
// worse than the plain hold-out this rule exists to enforce.
func TestCrossSiteWarningTickAppliesOnlyToTheTickedRowNotEveryUnorientedRow(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	vera := orientBlockPerson(t, w.st, "Vera Visitor")
	if _, err := domain.CompleteOrientation(w.st, "site-a", 1, vera.PersonID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	nora := orientBlockPerson(t, w.st, "Nora Newcomer") // holds NO site's orientation

	// driver (0, already oriented), Vera (1, ticked), Nora (2, NOT ticked).
	body := scriptedCheckInBody("TRK-91", 0, orientBlockRows(w.driver, vera, nora))
	body.Add("add_orientation", "1") // Vera's row only
	rec := postForm(t, w.srv, "/gate/checkin", body)
	if rec.Code != 303 {
		t.Fatalf("POST /gate/checkin = %d, want 303 (driver+Vera commit, Nora is held out): %s", rec.Code, rec.Body.String())
	}

	open := orientBlockOpenPersonIDs(t, w.st)
	if !open[vera.PersonID] {
		t.Fatalf("Vera Visitor (ticked) has no open visit at site-b")
	}
	if open[nora.PersonID] {
		t.Fatalf("Nora Newcomer (NOT ticked) has an OPEN VISIT at site-b — the tick spilled onto an un-ticked row")
	}

	if held, err := domain.OrientationHeld(w.st, vera.PersonID, "site-b"); err != nil || held == nil {
		t.Fatalf("Vera's site-b orientation was not recorded despite her tick: held=%v err=%v", held, err)
	}
	if held, err := domain.OrientationHeld(w.st, nora.PersonID, "site-b"); err != nil || held != nil {
		t.Fatalf("Nora's site-b orientation was recorded despite NO tick on her row: held=%v err=%v", held, err)
	}

	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, notEnteredQueryKey+"="+nora.PersonID) {
		t.Fatalf("redirect %q does not carry Nora as held out", loc)
	}
	if strings.Contains(loc, notEnteredQueryKey+"="+vera.PersonID) {
		t.Fatalf("redirect %q also holds out Vera, who was ticked and entered", loc)
	}

	heldOutAfter, err := domain.HeldOutAt(w.st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	foundNora := false
	for _, h := range heldOutAfter {
		if h.PersonID == vera.PersonID {
			t.Fatalf("Vera Visitor is STILL on the held-out list after her ticked entry: %+v", heldOutAfter)
		}
		if h.PersonID == nora.PersonID {
			foundNora = true
		}
	}
	if !foundNora {
		t.Fatalf("Nora Newcomer is NOT on the held-out list despite never being ticked: %+v", heldOutAfter)
	}

	muster := orientBlockGet(t, w.srv, "/muster")
	if !strings.Contains(muster, "Vera Visitor") {
		t.Errorf("Vera Visitor is not on the muster after her ticked entry")
	}
	if strings.Contains(muster, "Nora Newcomer") {
		t.Errorf("Nora Newcomer IS on the muster despite never being ticked or oriented at site-b")
	}
}

// --- (d) a ROAD POST shows none of this -----------------------------------

// TestCrossSiteWarningAbsentAtARoadPost is case (d): D-88's road post has no
// orientation concept at all. Accumulating an unoriented person there must
// show no warning, no checkbox, no mention of orientation anywhere on the
// page.
func TestCrossSiteWarningAbsentAtARoadPost(t *testing.T) {
	w := newOrientBlockWorld(t, false)
	if w.srv.Cfg.HasOrientation() {
		t.Fatal("precondition: this world must be a road post")
	}
	vera := orientBlockPerson(t, w.st, "Vera Visitor")
	body := scriptedCheckInBody("RD-88", 0, orientBlockRows(w.driver, vera))
	body.Set("new_last", ""); body.Set("new_first", "")
	rec := postForm(t, w.srv, "/gate/passenger/add", body)
	if rec.Code != 200 {
		t.Fatalf("road post POST /gate/passenger/add = %d: %s", rec.Code, rec.Body.String())
	}
	page := rec.Body.String()
	if warning := crossSiteWarningText(page); warning != "" {
		t.Errorf("a road post rendered a cross-site orientation warning: %s", warning)
	}
	if strings.Contains(page, "add_orientation") {
		t.Errorf("a road post's page mentions add_orientation at all: the field must not exist there")
	}
	if strings.Contains(page, "NOT ORIENTED HERE") {
		t.Errorf("a road post's page mentions orientation by name")
	}
	// And the vehicle checks in cleanly (D-88: nobody there owes one).
	rec = postForm(t, w.srv, "/gate/checkin", scriptedCheckInBody("RD-88", 0, orientBlockRows(w.driver, vera)))
	if rec.Code != 303 {
		t.Fatalf("road post POST /gate/checkin = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	if open := orientBlockOpenPersonIDs(t, w.st); !open[vera.PersonID] {
		t.Fatalf("Vera Visitor did not enter at the road post")
	}
}

// --- (e) the tick authors the SAME SHAPE as /orientation/complete --------

// TestCrossSiteWarningTickAuthorsTheSameShapeAsOrientationComplete proves
// completeTickedOrientations is not a lesser "gate shortcut" record: the
// gate-ticked completion and one recorded on the /orientation page itself
// land in the SAME raw table with the SAME populated columns — in
// particular the SAME attributed post name, because both go through
// s.postAttribution() into domain.CompleteOrientations.
func TestCrossSiteWarningTickAuthorsTheSameShapeAsOrientationComplete(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	vera := orientBlockPerson(t, w.st, "Vera Visitor")   // via the gate tick
	wendy := orientBlockPerson(t, w.st, "Wendy Witness") // via /orientation/complete
	for _, id := range []string{vera.PersonID, wendy.PersonID} {
		if _, err := domain.CompleteOrientation(w.st, "site-a", 1, id, nil, 0, "default"); err != nil {
			t.Fatal(err)
		}
	}

	// Wendy: the ordinary /orientation/complete path.
	rec := postForm(t, w.srv, "/orientation/complete", url.Values{"person_id": {wendy.PersonID}})
	if rec.Code != 303 {
		t.Fatalf("POST /orientation/complete = %d: %s", rec.Code, rec.Body.String())
	}

	// Vera: the gate tick.
	body := scriptedCheckInBody("TRK-91", 0, orientBlockRows(w.driver, vera))
	body.Add("add_orientation", "1")
	rec = postForm(t, w.srv, "/gate/checkin", body)
	if rec.Code != 303 {
		t.Fatalf("POST /gate/checkin (ticked) = %d: %s", rec.Code, rec.Body.String())
	}

	veraBy := completionRecordedBy(t, w.st, vera.PersonID, "site-b")
	wendyBy := completionRecordedBy(t, w.st, wendy.PersonID, "site-b")
	if len(veraBy) != 1 || len(wendyBy) != 1 {
		t.Fatalf("expected exactly one orientation_completion row for each at site-b: vera=%v wendy=%v", veraBy, wendyBy)
	}
	if !veraBy[0].Valid || !wendyBy[0].Valid {
		t.Fatalf("recorded_by is NULL on one of them: vera=%v wendy=%v", veraBy[0], wendyBy[0])
	}
	if veraBy[0].String != wendyBy[0].String {
		t.Fatalf("the gate tick and /orientation/complete attribute to DIFFERENT posts: gate tick=%q, orientation page=%q", veraBy[0].String, wendyBy[0].String)
	}
	if veraBy[0].String != "Pad Site Control" {
		t.Fatalf("attribution = %q, want the post's own display name", veraBy[0].String)
	}
	// Both are readable through the same domain API, indistinguishably.
	at, err := domain.OrientationsAt(w.st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, row := range at {
		if row.RecordedBy != nil {
			seen[row.PersonID] = *row.RecordedBy
		}
	}
	if seen[vera.PersonID] != seen[wendy.PersonID] {
		t.Fatalf("OrientationsAt reports different RecordedBy shapes: vera=%q wendy=%q", seen[vera.PersonID], seen[wendy.PersonID])
	}
}

// --- (f) a post consults ONLY its own orientation, never another's --------

// TestCrossSiteWarningNeverConsultsOtherSitesOrientation is the case James
// added on clarification (2026-09-10): "it should not cause the same error
// for site 3's oriantation if they are at site 2." A post's ONLY question is
// "does this person hold MY orientation" — status at any other site, in
// either direction, is irrelevant and must produce NO warning, NO checkbox,
// NO mention.
//
// Nora Normal holds site-b's ("site 2", the post under test) orientation
// but is GENUINELY missing site-c's ("site 3") — proven by actually holding
// her out at a REAL site-c gate first (a second *Server sharing the same
// store, orientation=true there too), so there is real cross-site data — a
// live held-out row naming site-c — for a wrong implementation to trip
// over. This is not "no data exists", it is "the data exists and must be
// ignored here."
//
// "No warning appeared" alone would pass just as well against a page that
// never warns about anything, so this test is paired with a mutant
// (documented in reports/witness/CROSSSITE-WARNING.md) that widens the
// check to "any site's orientation, not just this post's own" — verified to
// turn this specific test RED, then reverted.
func TestCrossSiteWarningNeverConsultsOtherSitesOrientation(t *testing.T) {
	w := newOrientBlockWorld(t, true) // site-b, "Pad Site Control" — "site 2"
	nora := orientBlockPerson(t, w.st, "Nora Normal")
	if _, err := domain.CompleteOrientation(w.st, "site-b", 1, nora.PersonID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}

	// A REAL site-c ("site 3"), orientation=true, sharing the SAME store
	// (the "overall system" this single-store harness models) — Nora is
	// genuinely, live, held out there.
	oc := true
	cfgC := &config.Config{
		SiteID: "site-c", DisplayName: "Camp Site Control", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: w.st.Path, SyncIntervalSeconds: 3600,
		LoginPassword: testLoginPassword,
		Peers:         []config.Peer{{SiteID: "site-b", BaseURL: "http://example.invalid"}},
		Destinations:  []config.Destination{{ID: "site-b", DisplayName: "Pad Site Control", Controlled: true}},
		Orientation:   &oc,
	}
	srvC := New(cfgC, w.st)
	campDriver := orientBlockPerson(t, w.st, "Camp Driver")
	if _, err := domain.CompleteOrientation(w.st, "site-c", 1, campDriver.PersonID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	recC := postForm(t, srvC, "/gate/checkin", scriptedCheckInBody("CAMP-1", 0, orientBlockRows(campDriver, nora)))
	if recC.Code != 303 {
		t.Fatalf("precondition: site-c check-in = %d, want 303: %s", recC.Code, recC.Body.String())
	}
	if loc := recC.Header().Get("Location"); !strings.Contains(loc, notEnteredQueryKey+"="+nora.PersonID) {
		t.Fatalf("precondition: Nora was not genuinely held out at site-c: %q", loc)
	}
	heldAtC, err := domain.HeldOutAt(w.st, "site-c")
	if err != nil {
		t.Fatal(err)
	}
	precondition := false
	for _, h := range heldAtC {
		if h.PersonID == nora.PersonID {
			precondition = true
		}
	}
	if !precondition {
		t.Fatalf("precondition: Nora is not on site-c's held-out list — there is no real cross-site data to trip over: %+v", heldAtC)
	}

	// site-b, where she DOES hold the orientation: accumulate her onto a
	// vehicle. No warning, of any kind, about anything.
	body := scriptedCheckInBody("TRK-92", 0, orientBlockRows(w.driver, nora))
	body.Set("new_last", ""); body.Set("new_first", "")
	rec := postForm(t, w.srv, "/gate/passenger/add", body)
	if rec.Code != 200 {
		t.Fatalf("POST /gate/passenger/add = %d: %s", rec.Code, rec.Body.String())
	}
	page := rec.Body.String()
	if warning := crossSiteWarningText(page); warning != "" {
		t.Fatalf("a warning was rendered for Nora Normal, who holds THIS site's (site-b's) orientation: %s", warning)
	}
	if strings.Contains(page, "Nora Normal has not completed") {
		t.Fatalf("the page says Nora Normal has not completed an orientation, even though she holds site-b's:\n%s", page)
	}
	if strings.Contains(page, "site-c") || strings.Contains(page, "Camp Site Control") {
		t.Fatalf("site-b's gate page mentions the OTHER site (site-c / Camp Site Control) at all")
	}

	// And she checks in cleanly — no friction.
	rec = postForm(t, w.srv, "/gate/checkin", scriptedCheckInBody("TRK-92", 0, orientBlockRows(w.driver, nora)))
	if rec.Code != 303 {
		t.Fatalf("POST /gate/checkin (Nora, oriented at site-b) = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if strings.Contains(loc, notEnteredQueryKey+"=") {
		t.Fatalf("Nora was held out at site-b despite holding site-b's own orientation: %q", loc)
	}
	if open := orientBlockOpenPersonIDs(t, w.st); !open[nora.PersonID] {
		t.Fatalf("Nora Normal did not enter site-b despite holding its orientation")
	}
	// She is not, and was never, on SITE-B's OWN held-out list — she was
	// never refused there at all, unlike her genuine, live hold-out at
	// site-c (checked below).
	heldAtB, err := domain.HeldOutAt(w.st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range heldAtB {
		if h.PersonID == nora.PersonID {
			t.Fatalf("Nora Normal is on site-b's held-out list, despite holding site-b's own orientation and entering cleanly: %+v", heldAtB)
		}
	}
	// Her site-c hold-out is untouched by the UNRELATED site-b entry — the
	// two sites' facts about her are independent.
	stillHeldAtC, err := domain.HeldOutAt(w.st, "site-c")
	if err != nil {
		t.Fatal(err)
	}
	stillFound := false
	for _, h := range stillHeldAtC {
		if h.PersonID == nora.PersonID {
			stillFound = true
		}
	}
	if !stillFound {
		t.Fatalf("Nora's site-c hold-out vanished after an unrelated site-b entry — the sites are not independent")
	}
}
