// heldout_doors_muster_test.go — ⟦H-8 / T-084 / T-093 / M-37, 2026-09-29⟧
// the two halves of the held-out (not-entered) rendering defect, driven
// through the real HTTP doors on the step-5 wire fixtures:
//
//	(a) the console arrival doors — POST /trips/arrive and
//	    POST /trips/arrive-unsolicited — must tell the officer, by name and
//	    with the next action, who was just held out at the gate for lack of
//	    this site's orientation (R-9: "a refusal MUST tell the officer the
//	    next action"). The expected door carried the ids on its redirect and
//	    trips.html never rendered them; the unsolicited door discarded them
//	    before the redirect. /gate and /gate/vehicle rendered the same key.
//	(b) /muster and /muster/print must NOT file a held-out person under NOT
//	    TRACKED as "not aboard at arrival — whereabouts not recorded": they
//	    were aboard, and the post knows where they are (/orientation's
//	    "Held out at the gate" list). Per D-91 / D-10 / R-11 they are on no
//	    muster sheet at all. The genuine not-aboard case (ticked stayed
//	    behind) keeps its row and its wording, and the not-tracked tally
//	    counts only them.
//
// Each test asserts the precondition that poses its scenario (rule 6) —
// the not_entered row exists for exactly the held-out person — so a
// fixture drift cannot let it pass vacuously. All RED on fa904c2, GREEN
// after.
package web

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// getLoggedIn fetches path with a real session cookie and returns the body
// with styles stripped — /orientation is behind requireLogin (the other
// pages here are not, but a cookie costs nothing and keeps one helper).
func getLoggedIn(t *testing.T, srv *Server, path string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.AddCookie(loginCookie(t, srv, testLoginPassword))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec.Code, styleRe.ReplaceAllString(rec.Body.String(), "")
}

// assertHeldOutNoticeNames checks the locked NOT ENTERED wording names
// exactly the held-out person (and nobody else) and links the fixing page.
func assertHeldOutNoticeNames(t *testing.T, page, door, held, notHeld string) {
	t.Helper()
	for _, want := range []string{
		"NOT ENTERED — NO ORIENTATION",
		held + " does not hold Pad Site Control's orientation",
		"they are not on site and not on the muster",
		`href="/orientation"`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("%s: the page the officer lands on after holding %s out does not say %q:\n%s", door, held, want, page)
		}
	}
	if got := strings.Count(page, `class="notice notice-warn not-entered"`); got != 1 {
		t.Fatalf("%s: %d NOT ENTERED notices rendered, want exactly 1 (%s alone was held out)", door, got, held)
	}
	if strings.Contains(page, notHeld+" does not hold") {
		t.Fatalf("%s: %s holds the orientation and was entered, yet is named as held out", door, notHeld)
	}
}

func TestConsoleArrivalDoorTellsTheOfficerWhoWasHeldOut(t *testing.T) {
	w := newStep5WireWorld(t)
	rec := postForm(t, w.srv, "/trips/arrive", map[string][]string{
		"time_source":         {"default"},
		"trip_id":             {w.trip.TripID},
		"manifest_person_ids": {w.adams.PersonID, w.baker.PersonID},
		"arriving_person_ids": {w.adams.PersonID, w.baker.PersonID},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /trips/arrive = %d: %s", rec.Code, rec.Body.String())
	}
	// Precondition: the domain held Baker out and the redirect carries the
	// id (both already true before this fix — the rendering is the gap).
	if got := step5NotEnteredPersonIDs(t, w.st, step5ArrivalIDForTrip(t, w.st, w.trip.TripID)); len(got) != 1 || got[0] != w.baker.PersonID {
		t.Fatalf("precondition: not_entered rows = %v, want exactly Baker", got)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/trips") || !strings.Contains(loc, notEnteredQueryKey+"="+w.baker.PersonID) {
		t.Fatalf("precondition: redirect %q must land on /trips carrying %s=<Baker>", loc, notEnteredQueryKey)
	}
	code, page := getLoggedIn(t, w.srv, loc)
	if code != http.StatusOK {
		t.Fatalf("GET %s = %d", loc, code)
	}
	assertHeldOutNoticeNames(t, page, "console arrival door", "Baker", "Adams")
}

func TestUnsolicitedArrivalDoorTellsTheOfficerWhoWasHeldOut(t *testing.T) {
	w := newStep5UnsolicitedWireWorld(t)
	rec := postForm(t, w.srv, "/trips/arrive-unsolicited", map[string][]string{
		"time_source": {"default"},
		"vehicle":     {"TRK-900"},
		"origin_site": {"site-a"},
		"person_ids":  {w.adams.PersonID, w.baker.PersonID},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /trips/arrive-unsolicited = %d: %s", rec.Code, rec.Body.String())
	}
	// Precondition: the domain held Baker out on this unsolicited arrival.
	if got := step5NotEnteredPersonIDs(t, w.st, step5UnsolicitedArrivalID(t, w.st, "site-a")); len(got) != 1 || got[0] != w.baker.PersonID {
		t.Fatalf("precondition: not_entered rows = %v, want exactly Baker", got)
	}
	// This door dropped the ids before the redirect (T-093): the key on
	// the Location is itself part of the fix, not a precondition.
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/trips") || !strings.Contains(loc, notEnteredQueryKey+"="+w.baker.PersonID) {
		t.Fatalf("unsolicited door: redirect %q must land on /trips carrying %s=<Baker> — the door discarded arrival.NotEntered", loc, notEnteredQueryKey)
	}
	code, page := getLoggedIn(t, w.srv, loc)
	if code != http.StatusOK {
		t.Fatalf("GET %s = %d", loc, code)
	}
	assertHeldOutNoticeNames(t, page, "unsolicited arrival door", "Baker", "Adams")
}

func TestAHeldOutPersonIsOnNoMusterSheetWhileAGenuineNotAboardStillIs(t *testing.T) {
	w := newStep5WireWorld(t)
	// Adams (holds the orientation) is ticked STAYED BEHIND — the genuine
	// "not aboard at arrival" case, which never reaches the orientation
	// split. Baker arrives and is held out at the gate. Both end up with no
	// visit here; only Baker has a not_entered row, and that row is the
	// whole difference between "whereabouts not recorded" and "held out at
	// this gate, on /orientation".
	rec := postForm(t, w.srv, "/trips/arrive", map[string][]string{
		"time_source":         {"default"},
		"trip_id":             {w.trip.TripID},
		"manifest_person_ids": {w.adams.PersonID, w.baker.PersonID},
		"arriving_person_ids": {w.baker.PersonID},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /trips/arrive = %d: %s", rec.Code, rec.Body.String())
	}
	// Preconditions (rule 6).
	if got := step5NotEnteredPersonIDs(t, w.st, step5ArrivalIDForTrip(t, w.st, w.trip.TripID)); len(got) != 1 || got[0] != w.baker.PersonID {
		t.Fatalf("precondition: not_entered rows = %v, want exactly Baker (Adams stayed behind and must have none)", got)
	}
	open, err := domain.OpenVisitsAt(w.st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range open {
		if v.PersonID == w.adams.PersonID || v.PersonID == w.baker.PersonID {
			t.Fatalf("precondition: neither Adams nor Baker may be on site, got open visit %+v", v)
		}
	}
	// Baker's whereabouts ARE recorded: the standing held-out list.
	if code, orient := getLoggedIn(t, w.srv, "/orientation"); code != http.StatusOK ||
		!strings.Contains(orient, "Held out at the gate") ||
		!strings.Contains(orient, `data-held-out-person="`+w.baker.PersonID+`"`) {
		t.Fatalf("precondition: /orientation (%d) does not list Baker as held out at the gate", code)
	}

	for _, path := range []string{"/muster", "/muster/print"} {
		page := getPage(t, w.srv, path)
		// The genuine case keeps its row, its wording and its count.
		if !strings.Contains(page, `data-muster-section="not-tracked"`) {
			t.Fatalf("%s: the Not tracked section is missing — Adams stayed behind and must still be listed:\n%s", path, page)
		}
		if !strings.Contains(page, "Adams") ||
			// ⟦was: "on <code>TRK-500</code> to site-b, not aboard at arrival — whereabouts not recorded"⟧
			// Fix round 3 (FINDINGS-R5 F3a/F10): the destination by display name, never the raw
			// id, and the arrival's time.
			!regexp.MustCompile(`on <code>TRK-500</code> to [^,<]+, not aboard at arrival at \d\d:\d\d \d\d/\d\d by [^<]+&#39;s clock — whereabouts not recorded`).MatchString(page) ||
			strings.Contains(page, "to site-b,") {
			t.Fatalf("%s: Adams's genuine not-aboard row lost its wording:\n%s", path, page)
		}
		if !strings.Contains(page, `data-tally-count="not-tracked">1<`) {
			t.Fatalf("%s: not-tracked tally must count Adams alone (1):\n%s", path, page)
		}
		// The held-out person is on no sheet: not under NOT TRACKED, not
		// anywhere. (Baker has no visit, no road entry, no pad entry — the
		// only way the name could appear is the defect itself.)
		if got := strings.Count(page, "Baker"); got != 0 {
			t.Fatalf("%s: Baker (held out at this gate, listed on /orientation) appears %d time(s) — was aboard, whereabouts recorded; D-91/R-11 put them on no muster sheet:\n%s", path, got, page)
		}
	}
}
