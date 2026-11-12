// heldout_recovery_test.go — the held-out person's route back onto site
// (reports/witness/HELDOUT-RECOVERY.md, 2026-09-10; R-9, R-11).
//
// MEASURED at 3f46fcb, live, at an orientation=true post (PRE SchemaVersion
// 12): a person arrives un-oriented → held out, 0 open visits, the check-in
// TRIP closes (state `arrived`, nobody entered). Their orientation was then
// recorded → still 0 visits (R-9: completion was a precondition, not an
// admission). POST /gate/resume on that trip → 409 "trip … is already
// arrived" — shown for a person who is NOT on site. "Arrived" is a fact
// about the TRIP; a gate officer reads it as a fact about the PERSON, and
// this is a muster system. The one route back in was a fresh gate check-in,
// signposted only in the held-out table's Next cell, not in the
// confirmation the officer reads when they tick the completion.
//
// ⚠ SchemaVersion 12 (R-16) CHANGED THE PREMISE: entry-on-completion (d91
// §3.4) means recording the completion now IS the route back in, when a
// held-out arrival exists at this site — no separate fresh check-in.
// "Completion is a precondition, not an admission" (R-9) stood for the
// PRE-entry-on-completion build; it does not survive this bump for the
// held-out case specifically. R-9's actual invariant — orientation is
// checked BEFORE anyone is entered, never bypassed — still holds; entry-
// on-completion is simply the moment that check is satisfied.
//
// Proofs, each through the REAL HTTP door, in gate_orientation_block's
// world (the words assert on what reaches the officer verbatim via refuse()):
//  1. the refused resume says the TRIP is done, that this enters nobody,
//     that it says nothing about who is on site, and — while the person is
//     genuinely still held out — names the route back; once entry-on-
//     completion has entered them, that epilogue disappears rather than
//     going stale;
//  2. at a road post (orientation = false) the same refusal is still true
//     and mentions no orientation — nobody owes one there (D-88);
//  3. the completion's own confirmation says who it JUST entered, read
//     live against a current open visit — a stale reload after they later
//     depart must stop claiming they are on site.
package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// okNoticeText is the text of the page's notice-ok div(s), asserted on the
// notice itself: the held-out table below it names the same person.
func okNoticeText(page string) string {
	var out []string
	rest := page
	for {
		i := strings.Index(rest, `class="notice notice-ok"`)
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

// --- 1. the refused resume tells the truth, and R-9 holds at every step ---

func TestResumingAClosedCheckInSaysTheTripIsDoneNotThatAnyoneIsOnSite(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	// Step 1: Uma, un-oriented, alone. Held out; the trip closes with
	// nobody entered.
	if rec := postForm(t, w.srv, "/gate/checkin", scriptedCheckInBody("TRK-90", 0, orientBlockRows(w.uma))); rec.Code != http.StatusSeeOther {
		t.Fatalf("step 1 POST /gate/checkin = %d: %s", rec.Code, rec.Body.String())
	}
	trip := orientBlockDegenerateTrip(t, w.st)
	if orientBlockOpenPersonIDs(t, w.st)[w.uma.PersonID] {
		t.Fatal("step 1: the gate entered un-oriented Uma — R-9 does not hold")
	}
	// Step 2: resume the closed check-in BEFORE her completion — the
	// misleading refusal's door, and she is still genuinely held out here,
	// so the "check them in again" epilogue is still true and must appear.
	rec := postForm(t, w.srv, "/gate/resume", url.Values{"trip_id": {trip.TripID}, "time_source": {"default"}})
	if rec.Code != http.StatusConflict {
		t.Fatalf("step 2 POST /gate/resume = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"has already arrived here",        // the trip — the fact the state word carries
		"enters nobody",                   // what resuming it would and would not do
		"not about who is on site",        // the misreading, refused in words
		"held out at that arrival",        // who this concerns at an orientation post
		"under Orientation",               // the next step…
		"check them in again at the gate", // …and the one after it — still true pre-completion
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the pre-completion resume refusal lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "is already arrived") {
		t.Errorf("the resume refusal still reads 'is already arrived' — the phrase a gate officer reads as 'already on site':\n%s", body)
	}
	if orientBlockOpenPersonIDs(t, w.st)[w.uma.PersonID] {
		t.Fatal("step 2: the refused resume entered Uma")
	}
	// Step 3: her completion is recorded. SchemaVersion 12 (R-16): entry-on-
	// completion means this event ALSO enters her — a held-out arrival
	// exists (step 1's), so the fold mints her visit off it. No separate
	// gate check-in is needed or offered any more.
	if rec := postForm(t, w.srv, "/orientation/complete", url.Values{"person_id": {w.uma.PersonID}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("step 3 POST /orientation/complete = %d: %s", rec.Code, rec.Body.String())
	}
	if !orientBlockOpenPersonIDs(t, w.st)[w.uma.PersonID] {
		t.Fatal("step 3: completing Uma's orientation against a held-out arrival did not enter her (d91 §3.4)")
	}
	// Step 4: resume the SAME closed trip again. The trip is still a fact
	// on record — "has already arrived", "enters nobody" — but the epilogue
	// must be GONE: nobody this trip held out is still held out, so telling
	// the officer to "check them in again" would be stale, and stale is
	// exactly the misreading this refusal exists to refuse.
	rec = postForm(t, w.srv, "/gate/resume", url.Values{"trip_id": {trip.TripID}, "time_source": {"default"}})
	if rec.Code != http.StatusConflict {
		t.Fatalf("step 4 POST /gate/resume = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	body = rec.Body.String()
	for _, want := range []string{"has already arrived here", "enters nobody", "not about who is on site"} {
		if !strings.Contains(body, want) {
			t.Errorf("the post-completion resume refusal lacks %q:\n%s", want, body)
		}
	}
	for _, stale := range []string{"held out at that arrival", "check them in again at the gate"} {
		if strings.Contains(body, stale) {
			t.Errorf("the post-completion resume refusal still says %q — Uma is on site now, this is stale guidance:\n%s", stale, body)
		}
	}
	if !orientBlockOpenPersonIDs(t, w.st)[w.uma.PersonID] {
		t.Fatal("step 4: the second refused resume somehow removed Uma's entry")
	}
}

// --- 2. at a road post the same refusal is true and says nothing of orientation

func TestResumingAClosedCheckInAtARoadPostMentionsNoOrientation(t *testing.T) {
	w := newOrientBlockWorld(t, false)
	if rec := postForm(t, w.srv, "/gate/checkin", scriptedCheckInBody("TRK-91", 0, orientBlockRows(w.uma))); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /gate/checkin = %d: %s", rec.Code, rec.Body.String())
	}
	if !orientBlockOpenPersonIDs(t, w.st)[w.uma.PersonID] {
		t.Fatal("a road post did not enter Uma — D-88: nobody owes an orientation there")
	}
	trip := orientBlockDegenerateTrip(t, w.st)
	rec := postForm(t, w.srv, "/gate/resume", url.Values{"trip_id": {trip.TripID}, "time_source": {"default"}})
	if rec.Code != http.StatusConflict {
		t.Fatalf("POST /gate/resume = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"has already arrived here", "enters nobody", "not about who is on site"} {
		if !strings.Contains(body, want) {
			t.Errorf("the road-post resume refusal lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(strings.ToLower(body), "orientation") {
		t.Errorf("the road-post resume refusal talks about an orientation nobody owes there:\n%s", body)
	}
}

// --- 3. the completion's confirmation names who it just entered, read live

// ⟦was: TestCompletingOrientationForAHeldOutPersonSaysTheyAreStillNotOnSite⟧
// SchemaVersion 12 (R-16) inverted this test's whole premise: completing a
// held-out person's orientation now ENTERS them (d91 §3.4) instead of
// leaving them for a fresh gate check-in, so the confirmation must say so.
func TestCompletingOrientationForAHeldOutPersonEntersThemAndSaysSo(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	cookie := loginCookie(t, w.srv, testLoginPassword)
	if rec := postForm(t, w.srv, "/gate/checkin", scriptedCheckInBody("TRK-92", 0, orientBlockRows(w.uma))); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /gate/checkin = %d: %s", rec.Code, rec.Body.String())
	}
	rec := postForm(t, w.srv, "/orientation/complete", url.Values{"person_id": {w.uma.PersonID}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /orientation/complete = %d: %s", rec.Code, rec.Body.String())
	}
	if !orientBlockOpenPersonIDs(t, w.st)[w.uma.PersonID] {
		t.Fatal("completing Uma's orientation against a held-out arrival did not enter her (d91 §3.4)")
	}
	loc := rec.Header().Get("Location")
	page := w2Get(t, w.srv, loc, cookie)
	if page.Code != http.StatusOK {
		t.Fatalf("GET %s = %d", loc, page.Code)
	}
	notice := okNoticeText(page.Body.String())
	for _, want := range []string{
		"Recorded 1 orientation completion",
		"Uma Unoriented is now on site",
		"entered them off the arrival that held them",
	} {
		if !strings.Contains(notice, want) {
			t.Errorf("the completion's confirmation lacks %q:\n%s", want, notice)
		}
	}
	for _, stale := range []string{"still not on site", "check them in again at the gate"} {
		if strings.Contains(notice, stale) {
			t.Errorf("the confirmation says %q — Uma was entered by this completion, not left for the gate:\n%s", stale, notice)
		}
	}

	// She later leaves. The SAME stale redirect target, re-rendered after
	// her departure, must stop claiming she is on site — "is now on site"
	// is a live present-tense claim (heldOutEnteredCopy's own comment),
	// checked against a CURRENT open visit, never asserted merely because
	// her id rode the query string.
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
	notice = okNoticeText(w2Get(t, w.srv, loc, cookie).Body.String())
	if strings.Contains(notice, "Uma Unoriented") || strings.Contains(notice, "now on site") {
		t.Errorf("the confirmation still calls Uma on site after she departed:\n%s", notice)
	}

	// Never held out: completing Nadia, who has not been to the gate, adds
	// no such sentence — the confirmation is not a standing warning.
	nadia := orientBlockPerson(t, w.st, "Nadia Newcomer")
	rec = postForm(t, w.srv, "/orientation/complete", url.Values{"person_id": {nadia.PersonID}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /orientation/complete (Nadia) = %d: %s", rec.Code, rec.Body.String())
	}
	notice = okNoticeText(w2Get(t, w.srv, rec.Header().Get("Location"), cookie).Body.String())
	if strings.Contains(notice, "still not on site") || strings.Contains(notice, "Nadia Newcomer") {
		t.Errorf("a never-held-out completion carries the held-out sentence:\n%s", notice)
	}
}
