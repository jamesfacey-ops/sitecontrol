// orientation_test.go — SCHEMA-11 step 4: the marking surface's witnesses
// (reports/witness/STEP4-ORIENTATION.md §6 rows 10–11). At a road post the
// routes are NOT REGISTERED; at a pad the page lists who lacks the
// orientation, a bulk POST authors one event per ticked person, a duplicate
// re-POST is refused with the composed sentence and nothing authored, and a
// retract removes the row. Produced red first by registering the routes
// unconditionally — see the report's §7.
//
// ⚠ Gating is asserted on BODY and on "nothing authored", never on status
// alone: the "GET /" catch-all answers 200 to every unmatched GET (the C5
// carry-forward, row 22), so an unregistered GET /orientation serves the
// STATUS page, and an unregistered POST gets the mux's method refusal.
package web

import (
	"html"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// newOrientationServer is a site-b box ("Pad Site Control") whose config
// says whether it administers an orientation. Three people are enrolled.
func newOrientationServer(t *testing.T, orientation bool) (*Server, *store.Store, []string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "orientation.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	o := orientation
	cfg := &config.Config{
		SiteID: "site-b", DisplayName: "Pad Site Control", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 3600,
		LoginPassword: testLoginPassword,
		Peers:         []config.Peer{{SiteID: "site-a", BaseURL: "http://example.invalid"}},
		Destinations:  []config.Destination{{ID: "site-a", DisplayName: "Ice Road", Controlled: true}},
		Orientation:   &o,
	}
	var ids []string
	for _, name := range []string{"Diaz", "Evans", "Clark"} {
		p, err := domain.CreatePerson(st, "site-a", 1, name, nil, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.PersonID)
	}
	return New(cfg, st), st, ids
}

// orientationCheckboxRe matches one served person checkbox element, so the
// "unticked by default" assertion reads the element's own attributes and
// not the word "checked" wherever it occurs on the page (the shared
// stylesheet's comments use it).
var orientationCheckboxRe = regexp.MustCompile(`<input type="checkbox" name="person_id"[^>]*>`)

func orientationWatermark(t *testing.T, st *store.Store) int64 {
	t.Helper()
	wm, err := st.Watermark("site-b", 1)
	if err != nil {
		t.Fatal(err)
	}
	return wm
}

// TestOrientationRoutesAreNotRegisteredAtAPostWithoutAnOrientation — §6 row
// 10, the road-post half. The dev_routes pattern: not registered, never
// "disabled".
func TestOrientationRoutesAreNotRegisteredAtAPostWithoutAnOrientation(t *testing.T) {
	srv, st, ids := newOrientationServer(t, false)
	cookie := loginCookie(t, srv, testLoginPassword)
	before := orientationWatermark(t, st)

	rec := w2Get(t, srv, "/orientation", cookie)
	body := rec.Body.String()
	if strings.Contains(body, "Record the ticked completions") || strings.Contains(body, "Retract (mis-tick)") {
		t.Fatalf("a post with orientation = false served the marking page:\n%s", body)
	}
	for _, path := range []string{"/orientation/complete", "/orientation/retract"} {
		rec, _ := w2PostForm(t, srv, path, url.Values{"person_id": ids[:1]})
		if rec.Code == http.StatusSeeOther {
			t.Fatalf("POST %s at a post with orientation = false was ACCEPTED (303)", path)
		}
		if strings.Contains(rec.Body.String(), "already holds") || strings.Contains(rec.Body.String(), "nothing to retract") {
			t.Fatalf("POST %s at a post with orientation = false reached the orientation handler:\n%s", path, rec.Body.String())
		}
	}
	if after := orientationWatermark(t, st); after != before {
		t.Fatalf("an orientation POST at a road post authored: watermark %d → %d", before, after)
	}
	held, err := domain.OrientationsAt(st, "site-b")
	if err != nil || len(held) != 0 {
		t.Fatalf("OrientationsAt(site-b) = %+v (%v) at a road post, want none", held, err)
	}
}

// TestOrientationMarkingSurfaceRecordsRefusesDuplicatesAndRetracts — §6 row
// 10, the pad half, end to end through the served HTML.
func TestOrientationMarkingSurfaceRecordsRefusesDuplicatesAndRetracts(t *testing.T) {
	srv, st, ids := newOrientationServer(t, true)
	cookie := loginCookie(t, srv, testLoginPassword)

	// The page lists all three as lacking, none as holding, nothing ticked.
	rec := w2Get(t, srv, "/orientation", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /orientation = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, name := range []string{"Diaz", "Evans", "Clark"} {
		if !strings.Contains(body, name) {
			t.Fatalf("the page does not list %s:\n%s", name, body)
		}
	}
	if !strings.Contains(body, "Do not yet hold it (3)") || !strings.Contains(body, "orientation (0)") {
		t.Fatalf("the page's counts are wrong:\n%s", body)
	}
	if boxes := orientationCheckboxRe.FindAllString(body, -1); len(boxes) != 3 {
		t.Fatalf("want 3 person checkboxes, found %d:\n%s", len(boxes), body)
	} else {
		for _, box := range boxes {
			if strings.Contains(box, "checked") {
				t.Fatalf("a checkbox is ticked by default — D-10's inversion is broken: %s", box)
			}
		}
	}

	// Bulk: two ticked → two events, both held, the PRG echo says two.
	before := orientationWatermark(t, st)
	rec, cookie = w2PostForm(t, srv, "/orientation/complete", url.Values{"person_id": {ids[0], ids[1]}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /orientation/complete = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	if after := orientationWatermark(t, st); after != before+2 {
		t.Fatalf("two ticked people authored %d events, want 2", after-before)
	}
	for _, id := range ids[:2] {
		row, err := domain.OrientationHeld(st, id, "site-b")
		if err != nil || row == nil || row.RecordedBy == nil || *row.RecordedBy != "Pad Site Control" {
			t.Fatalf("%s: OrientationHeld = %+v (%v), want a site-b row recorded by this post", id, row, err)
		}
	}
	page := w2Get(t, srv, rec.Result().Header.Get("Location"), cookie).Body.String()
	if !strings.Contains(page, "Recorded 2 orientation completion(s)") || !strings.Contains(page, "orientation (2)") || !strings.Contains(page, "Do not yet hold it (1)") {
		t.Fatalf("the confirmation page does not reflect the two completions:\n%s", page)
	}

	// Duplicate: re-ticking Diaz with Clark refuses the WHOLE submission —
	// the composed sentence, an honest 409, nothing authored (Clark too).
	before = orientationWatermark(t, st)
	rec, _ = w2PostForm(t, srv, "/orientation/complete", url.Values{"person_id": {ids[2], ids[0]}})
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate POST = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	body = rec.Body.String()
	if !strings.Contains(body, html.EscapeString("Diaz already holds Pad Site Control's orientation (recorded by Pad Site Control)")) || !strings.Contains(body, "nothing was recorded") {
		t.Fatalf("the refusal sentence is missing or wrong:\n%s", body)
	}
	if strings.Contains(body, "domain: orientation:") {
		t.Fatalf("a raw err.Error() reached the officer:\n%s", body)
	}
	if after := orientationWatermark(t, st); after != before {
		t.Fatalf("the refused submission authored: watermark %d → %d", before, after)
	}
	if row, _ := domain.OrientationHeld(st, ids[2], "site-b"); row != nil {
		t.Fatal("Clark was entered by a refused submission")
	}

	// Retract Evans (D-95): the row goes, the page says so, Evans is back in
	// the lacking list; a second retract is refused with its sentence.
	rec, cookie = w2PostForm(t, srv, "/orientation/retract", url.Values{"person_id": {ids[1]}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /orientation/retract = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	if row, _ := domain.OrientationHeld(st, ids[1], "site-b"); row != nil {
		t.Fatal("Evans still holds the orientation after the retract")
	}
	page = w2Get(t, srv, rec.Result().Header.Get("Location"), cookie).Body.String()
	// (The notice's apostrophe is template text, not data, so it is not
	// escaped the way the refusal sentences above are.)
	if !strings.Contains(page, "Retracted: Evans no longer holds Pad Site Control's orientation") || !strings.Contains(page, "Do not yet hold it (2)") {
		t.Fatalf("the page does not reflect the retraction:\n%s", page)
	}
	rec, _ = w2PostForm(t, srv, "/orientation/retract", url.Values{"person_id": {ids[1]}})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), html.EscapeString("Evans does not hold Pad Site Control's orientation")) {
		t.Fatalf("second retract = %d, want 409 with the not-held sentence: %s", rec.Code, rec.Body.String())
	}

	// The name filter narrows both lists without changing what is held.
	page = w2Get(t, srv, "/orientation?q=dia", cookie).Body.String()
	if !strings.Contains(page, "Diaz") || strings.Contains(page, "Clark") || strings.Contains(page, "Evans") {
		t.Fatalf("?q=dia did not narrow the lists:\n%s", page)
	}
}

// The GET is behind the login gate, like /trade: an anonymous request is
// served the login form, not the roster.
func TestOrientationPageRequiresLogin(t *testing.T) {
	srv, _, _ := newOrientationServer(t, true)
	rec := w2Get(t, srv, "/orientation", nil)
	if strings.Contains(rec.Body.String(), "Record the ticked completions") {
		t.Fatalf("an anonymous GET /orientation served the marking page:\n%s", rec.Body.String())
	}
}
