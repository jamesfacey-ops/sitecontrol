// orientation_controls_test.go — tester-go (reports/witness/TESTER-step4.md):
// the POSITIVE CONTROL the road-post and anonymous NEGATIVES in
// orientation_test.go are stated against. Those assert a body LACKS
// "Record the ticked completions" / "Retract (mis-tick)"; nothing asserted
// the pad page CARRIES them, so a template edit could make both negatives
// pass vacuously ([PRODUCED] — renaming the button left all three builder
// tests green; the report's §6). This anchors them on the pad page, pins the
// anonymous GET to the LOGIN form (requireLogin renders it; no redirect),
// and pins a road post's POSTs to the mux's refusal with NO orientation
// envelope of either type in this post's log.
package web

import (
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestOrientationPadPageCarriesTheControlsTheNegativesAreStatedAgainst(t *testing.T) {
	srv, _, ids := newOrientationServer(t, true)
	cookie := loginCookie(t, srv, testLoginPassword)

	body := w2Get(t, srv, "/orientation", cookie).Body.String()
	if !strings.Contains(body, "Record the ticked completions") || !strings.Contains(body, `action="/orientation/complete"`) {
		t.Fatalf("the pad page does not carry the completion control the road-post negative is stated against — that negative is vacuous:\n%s", body)
	}
	if strings.Contains(body, "Retract (mis-tick)") {
		t.Fatalf("a retract control is offered while nobody holds the orientation:\n%s", body)
	}

	rec, cookie := w2PostForm(t, srv, "/orientation/complete", url.Values{"person_id": {ids[0]}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /orientation/complete = %d: %s", rec.Code, rec.Body.String())
	}
	body = w2Get(t, srv, "/orientation", cookie).Body.String()
	if !strings.Contains(body, "Retract (mis-tick)") {
		t.Fatalf("after a completion the pad page carries no retract control — the negatives on that string are vacuous:\n%s", body)
	}
	if n := strings.Count(body, `action="/orientation/retract"`); n != 1 {
		t.Fatalf("want exactly 1 retract form for the one holder, found %d:\n%s", n, body)
	}
	if !strings.Contains(body, `<input type="hidden" name="person_id" value="`+html.EscapeString(ids[0])+`">`) {
		t.Fatalf("the retract form does not name the holder %q:\n%s", ids[0], body)
	}
	if n := len(orientationCheckboxRe.FindAllString(body, -1)); n != 2 {
		t.Fatalf("want 2 checkboxes for the two who still lack it, found %d:\n%s", n, body)
	}

	// The anonymous GET is the LOGIN form (requireLogin renders it inline),
	// not merely "something other than the page".
	anon := w2Get(t, srv, "/orientation", nil)
	if anon.Code != http.StatusOK || !strings.Contains(anon.Body.String(), `name="password"`) {
		t.Fatalf("an anonymous GET /orientation did not serve the login form (%d):\n%s", anon.Code, anon.Body.String())
	}
	if strings.Contains(anon.Body.String(), "Record the ticked completions") || strings.Contains(anon.Body.String(), `name="person_id"`) {
		t.Fatalf("the anonymous response carries the roster or its controls:\n%s", anon.Body.String())
	}
}

func TestOrientationRoadPostPOSTsAreTheMuxsRefusalAndLeaveNoOrientationEnvelope(t *testing.T) {
	srv, st, ids := newOrientationServer(t, false)
	for _, path := range []string{"/orientation/complete", "/orientation/retract"} {
		rec, _ := w2PostForm(t, srv, path, url.Values{"person_id": ids[:1]})
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("POST %s at a road post = %d, want the mux's 404/405 — the route must not EXIST there: %s", path, rec.Code, rec.Body.String())
		}
	}
	evs, err := st.Events("site-b", 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		var head struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(e.Payload, &head)
		if strings.HasPrefix(head.Type, "orientation_") {
			t.Fatalf("a road post's own log holds %q at %v", head.Type, e.Key())
		}
	}
	cookie := loginCookie(t, srv, testLoginPassword)
	body := w2Get(t, srv, "/orientation", cookie).Body.String()
	if strings.Contains(body, `action="/orientation/complete"`) || strings.Contains(body, `name="person_id"`) {
		t.Fatalf("a road post served the completion form:\n%s", body)
	}
}
