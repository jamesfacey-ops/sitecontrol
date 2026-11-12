// w5c_amend_surface_test.go — W5c-1 AT THE OFFICER'S OWN SURFACE —
// CORRECTED TO THE SCHEMA-11 SHAPE.
//
//	W5c-1  ⟦was⟧ Only `true` and absent are ever authored; revoke is `Clear`.
//	       No SURFACE can author "false", "", "0", "no" or any other value.
//	W5c-1  ⟦SchemaVersion 11⟧ No SURFACE can author pad orientation AT ALL.
//	       A POST carrying the struck key — with ANY value, `true` included —
//	       is refused pre-append (the edge sees an amendment that changes
//	       nothing); `clear=pad_oriented` is refused as not amendable; the
//	       rendered form offers no control for it; and the form still
//	       authors a surviving fact, so none of that is a dead form.
//
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, criteria LOCKED 2026-08-31.
//
// ⟦SchemaVersion 11 — CORRECTED, NOT DELETED, 2026-09-05⟧ [[D-88]]'s ERRATA
// struck `pad_oriented` from the person record (a flag on the ENROLLING
// site's row cannot be a per-site orientation; D-88 reverses D-10/D-21.1).
// This file's headline red used to be "an officer types `false` and it is
// authored"; its headline now is "an officer types ANYTHING against the
// stale key and nothing is authored" — the same POST, driven over real
// HTTP, with the value corpus kept in full (this is the one surface where a
// VALUE can still be typed against the key, so the corpus lives here).
//
// SEAMS bound here, once; disputes go to the gate:
//
//   - SEAM W5c-S-FORMKEY ⟦re-pointed⟧: the form field names are the payload
//     keys ("the wire spelling IS the form spelling"); `pad_oriented` is no
//     longer among them, and `position` is the surviving key the mirrors
//     drive.
//   - SEAM W5c-S-REFUSAL: a refused amendment answers 4xx and re-renders
//     this page. 409 is the measured house value (StatusConflict) but NO
//     criterion states a number, so 4xx is asserted and 409 is not pinned.
package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"testing"

	"path/filepath"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

const (
	w5cKeyPad       = "pad_oriented" // the STRUCK key, typed against the form as a stale field
	w5cOrientedV    = "true"         // ⟦was⟧ the one legal value; now just another value that authors nothing
	w5cAmendRoute   = "/person/amend"
	w5cSurvivingKey = "position" // a surviving form field the mirrors drive
)

// w5cForbidden is the corpus this surface must not be able to author. At
// SchemaVersion 11 it includes the former legal value: NOTHING is authorable
// against the struck key.
var w5cForbidden = []string{
	"true", "false", "False", "FALSE", "0", "no", "No", "off", "n",
	"null", "yes", "1", "x", "true false", "TRUE", "True", "oriented",
}

// --- machinery ----------------------------------------------------------

func w5cWebServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "w5c-web.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		SiteID: "site-a", DisplayName: "site-a", ListenAddr: "127.0.0.1:0",
		NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 10,
		LoginPassword: testLoginPassword,
	}
	return New(cfg, st), st
}

// w5cSeedPerson lands a person through the ORDINARY authoring path.
func w5cSeedPerson(t *testing.T, st *store.Store, name string) string {
	t.Helper()
	p, err := domain.CreatePerson(st, "site-a", 1, name, nil, nil, 0, "default")
	if err != nil {
		t.Fatalf("fixture defect: seeding %q: %v", name, err)
	}
	return p.PersonID
}

func w5cStreamLen(t *testing.T, srv *Server) int64 {
	t.Helper()
	wm, err := srv.Store.Watermark(srv.Cfg.SiteID, srv.Cfg.NodeEpoch)
	if err != nil {
		t.Fatalf("watermark: %v", err)
	}
	return wm
}

// w5cPersonKeys returns the composed person's JSON key set — the byte-level
// question "does the record carry the struck key at all", which no Go
// selector can ask any more.
func w5cPersonKeys(t *testing.T, srv *Server, id string) map[string]bool {
	t.Helper()
	p, err := domain.PersonByID(srv.Store, id)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatalf("person %s vanished from the roster", id)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for k := range m {
		out[k] = true
	}
	return out
}

func w5cPersonPosition(t *testing.T, srv *Server, id string) *string {
	t.Helper()
	p, err := domain.PersonByID(srv.Store, id)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatalf("person %s vanished from the roster", id)
	}
	return p.Position
}

func w5cRefused(code int) bool { return code >= 400 && code < 500 }

func w5cClip(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 300 {
		return s[:300] + " …"
	}
	return s
}

// =======================================================================
// W5c-1 — NO SURFACE CAN AUTHOR PAD ORIENTATION AT ALL
// =======================================================================

// TestW5cOfficerCannotAuthorPadOrientationThroughTheAmendmentForm — W5c-1,
// through the officer's real HTTP POST. THIS IS W5c's HEADLINE ROW, inverted:
// every value in the corpus — `true` included — POSTed against the struck
// key is REFUSED (4xx), appends nothing, and leaves the composed record
// without the key.
// ⟦was: TestW5cOfficerCannotAuthorFalseThroughTheAmendmentForm⟧
//
// Why 4xx and not "silently ignored": the handler no longer reads the key,
// so the amendment reaches the edge asserting nothing and clearing nothing,
// and the edge's no-op refusal (W3-2) answers. An officer who types into a
// stale field learns their POST did nothing rather than reading "Recorded."
func TestW5cOfficerCannotAuthorPadOrientationThroughTheAmendmentForm(t *testing.T) {
	for i, value := range w5cForbidden {
		value := value
		t.Run(fmt.Sprintf("value_%02d_%s", i, w5cFormLabel(value)), func(t *testing.T) {
			srv, st := w5cWebServer(t)
			id := w5cSeedPerson(t, st, "Aurora, Ada")
			before := w5cStreamLen(t, srv)

			rec := postForm(t, srv, w5cAmendRoute, url.Values{
				"person_id": {id}, w5cKeyPad: {value},
			})
			after := w5cStreamLen(t, srv)

			if !w5cRefused(rec.Code) {
				t.Errorf("W5c-1 (m5c-1′): an officer typed %q into a `%s` field and the POST answered %d, want 4xx. The fact is STRUCK at SchemaVersion 11 ([[D-88]] ERRATA): nothing may be authored against it, and a 2xx/3xx here is a confirmation for a change that did not happen.\n  body: %s", value, w5cKeyPad, rec.Code, w5cClip(rec.Body.String()))
			}
			if after != before {
				t.Errorf("W5c-1: the POST of %s=%q moved this post's log %d -> %d. Every refusal on this surface is PRE-APPEND — domain.AmendPerson holds them all and a refused POST authors nothing.", w5cKeyPad, value, before, after)
			}
			if keys := w5cPersonKeys(t, srv, id); keys[w5cKeyPad] {
				t.Errorf("W5c-1: after an officer typed %q the composed record carries the struck key %q", value, w5cKeyPad)
			}
		})
	}
}

// TestW5cAmendmentFormStillRecordsASurvivingFact — the MIRROR, and it is
// not optional: without it every arm above is satisfied by a form that
// refuses everything. `position` still authors: 303, exactly one append,
// composed.
// ⟦was: TestW5cAmendmentFormStillRecordsAnOrientation — the orientation is
// gone with the fact; the mirror moves to a field that survived⟧
func TestW5cAmendmentFormStillRecordsASurvivingFact(t *testing.T) {
	srv, st := w5cWebServer(t)
	id := w5cSeedPerson(t, st, "Aurora, Ada")
	before := w5cStreamLen(t, srv)

	rec := postForm(t, srv, w5cAmendRoute, url.Values{"person_id": {id}, w5cSurvivingKey: {"Rig Manager"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("W5c-1 MIRROR: recording %s answered %d, want 303 (the house PRG). A form that refuses everything satisfies every negative arm of W5c-1 and proves nothing.\n  body: %s", w5cSurvivingKey, rec.Code, w5cClip(rec.Body.String()))
	}
	if after := w5cStreamLen(t, srv); after != before+1 {
		t.Fatalf("W5c-1 MIRROR: recording %s moved the log %d -> %d, want exactly one append", w5cSurvivingKey, before, after)
	}
	if pos := w5cPersonPosition(t, srv, id); pos == nil || *pos != "Rig Manager" {
		t.Fatalf("W5c-1 MIRROR: after recording %s the person does not read it back", w5cSurvivingKey)
	}
}

// TestW5cClearPadOrientedThroughTheFormIsRefused — W5c-1's last clause at
// the surface, inverted. ⟦was: TestW5cOfficerCanRevokeAnOrientationThroughTheForm
// — "revoke = Clear"⟧ There is nothing to revoke: `clear=pad_oriented` is
// refused as not amendable, pre-append, and the page says REFUSED. The
// mirror proves the clear MECHANISM survives on a surviving field.
func TestW5cClearPadOrientedThroughTheFormIsRefused(t *testing.T) {
	srv, st := w5cWebServer(t)
	id := w5cSeedPerson(t, st, "Aurora, Ada")
	if rec := postForm(t, srv, w5cAmendRoute, url.Values{"person_id": {id}, w5cSurvivingKey: {"Rig Manager"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("fixture defect: could not author the surviving fact the mirror clears (status %d)", rec.Code)
	}
	before := w5cStreamLen(t, srv)

	rec := postForm(t, srv, w5cAmendRoute, url.Values{"person_id": {id}, "clear": {w5cKeyPad}})
	if !w5cRefused(rec.Code) {
		t.Fatalf("W5c-1: clear=%s answered %d, want 4xx — the fact is struck and is not an amendable field.\n  body: %s", w5cKeyPad, rec.Code, w5cClip(rec.Body.String()))
	}
	if !strings.Contains(rec.Body.String(), "not an amendable field") {
		t.Errorf("W5c-1: the refusal page does not say WHY (\"not an amendable field\") — an officer must learn the field is gone, not guess at a rule.\n  body: %s", w5cClip(rec.Body.String()))
	}
	if after := w5cStreamLen(t, srv); after != before {
		t.Fatalf("W5c-1: the refused clear moved the log %d -> %d", before, after)
	}

	// MIRROR: the clear mechanism itself works on a surviving field.
	rec = postForm(t, srv, w5cAmendRoute, url.Values{"person_id": {id}, "clear": {w5cSurvivingKey}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("W5c-1 MIRROR: clear=%s answered %d, want 303 — a surface that refuses every clear makes the refusal above meaningless", w5cSurvivingKey, rec.Code)
	}
	if pos := w5cPersonPosition(t, srv, id); pos != nil {
		t.Fatalf("W5c-1 MIRROR: after clear=%s the person still reads %q, want absent (W3-3)", w5cSurvivingKey, *pos)
	}
}

// =======================================================================
// W5c-1 — THE RENDERED SURFACE OFFERS NO CONTROL FOR THE STRUCK FACT
// =======================================================================

var (
	w5cTagRe  = regexp.MustCompile(`(?is)<(input|select|textarea|option)\b[^>]*>`)
	w5cAttrRe = regexp.MustCompile(`(?is)([a-zA-Z_:][-a-zA-Z0-9_:.]*)\s*=\s*"([^"]*)"`)
)

func w5cAttrs(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range w5cAttrRe.FindAllStringSubmatch(tag, -1) {
		out[strings.ToLower(m[1])] = m[2]
	}
	return out
}

// w5cPadControls is the detector, applied to a RENDERED page: every control
// (input/select/textarea) whose name or id answers to the struck key is a
// finding, whatever its shape. ⟦was: w5cPostableStates — three teeth for the
// two-state shapes (pair/value/option) over a control that was ALLOWED to
// exist. At SchemaVersion 11 the control itself is the finding, so the
// old teeth collapse into one and every old plant still fires.⟧
//
// Applied to the rendered page rather than only the template source because
// person_amend.html renders every field as `name="{{.Key}}"` — the literal
// never appears in the template; only the rendered page resolves it.
func w5cPadControls(body string) []string {
	var found []string
	for _, loc := range w5cTagRe.FindAllStringIndex(body, -1) {
		tag := body[loc[0]:loc[1]]
		m := w5cTagRe.FindStringSubmatch(tag)
		kind := strings.ToLower(m[1])
		if kind == "option" {
			continue
		}
		a := w5cAttrs(tag)
		if a["name"] != w5cKeyPad && a["id"] != "amend-"+w5cKeyPad && a["id"] != w5cKeyPad {
			continue
		}
		found = append(found, fmt.Sprintf("[control] a <%s> answers to %q (name=%q id=%q value=%q)", kind, w5cKeyPad, a["name"], a["id"], a["value"]))
	}
	sort.Strings(found)
	return found
}

// TestW5cRenderedSurfacesOfferNoPadOrientedControl — W5c-1's "no SURFACE can
// author it" clause, read off the pages an officer actually gets.
// ⟦was: TestW5cRenderedSurfacesOfferNoSecondState⟧
//
// Its evidence is the FIVE planted CONTROLs below — the four old two-state
// plants plus the bare free-text box the old criterion permitted — all of
// which must now fire, and the MIRROR: the amendment page must still carry
// the surviving `position` control, or "no pad control" is true of a page
// that dropped its form.
func TestW5cRenderedSurfacesOfferNoPadOrientedControl(t *testing.T) {
	plants := []struct{ name, body string }{
		{"the hidden+checkbox pair", `<form><input type="hidden" name="pad_oriented" value="false"><input type="checkbox" name="pad_oriented" value="true"></form>`},
		{"a lone hidden field", `<form><input type="hidden" name="pad_oriented" value="false"></form>`},
		{"a yes/no select", `<form><select name="pad_oriented"><option value="">—</option><option value="true">Yes</option><option value="false">No</option></select></form>`},
		{"a checkbox posting only true", `<form><input type="checkbox" name="pad_oriented" value="true"></form>`},
		{"the bare free-text box the old criterion permitted", `<form><input id="amend-pad_oriented" name="pad_oriented"></form>`},
	}
	for _, p := range plants {
		if found := w5cPadControls(p.body); len(found) == 0 {
			t.Fatalf("CONTROL FAILED (%s): the detector saw NOTHING in a planted pad_oriented control — the clean pass below would prove nothing (standing rule 5)", p.name)
		}
	}
	for _, clean := range []string{
		`<form><input id="amend-position" name="position"></form>`,
		`<form><input id="amend-{{.Key}}" name="{{.Key}}"></form>`,
	} {
		if found := w5cPadControls(clean); len(found) != 0 {
			t.Fatalf("CONTROL FAILED: the detector fired on a control that does not answer to the struck key %q: %v", clean, found)
		}
	}

	// --- the real pages ---
	srv, st := w5cWebServer(t)
	w5cSeedPerson(t, st, "Aurora, Ada")
	cookie := loginCookie(t, srv, testLoginPassword)
	for _, page := range []string{w5cAmendRoute, "/roster"} {
		req, err := http.NewRequest("GET", page, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(cookie)
		rec := w5cRecord(t, srv, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("fixture defect: GET %s answered %d — a page that did not render cannot be scanned, and an empty body would pass this test silently", page, rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "<form") {
			t.Fatalf("fixture defect: GET %s rendered no <form> element, so there is nothing for this scan to examine: %s", page, w5cClip(body))
		}
		if found := w5cPadControls(body); len(found) != 0 {
			t.Errorf("W5c-1 (m5c-1′): the rendered %s still offers a control for the struck fact:\n  %s\n  Orientation is the ORIENTING post's own record ([[D-88]]), not a box on the enrolling site's person form.", page, strings.Join(found, "\n  "))
		}
	}
	// MIRROR: the amendment page must still CARRY its form fields, or "no pad
	// control" is true of a page that dropped the form entirely.
	req, _ := http.NewRequest("GET", w5cAmendRoute, nil)
	req.AddCookie(cookie)
	body := w5cRecord(t, srv, req).Body.String()
	if !strings.Contains(body, `name="`+w5cSurvivingKey+`"`) {
		t.Errorf("W5c-1 MIRROR: the amendment form no longer offers the surviving `%s` control (SEAM W5c-S-FORMKEY) — striking one fact must not remove the form", w5cSurvivingKey)
	}
}

func w5cFormLabel(v string) string { return strings.ReplaceAll(v, " ", "_") }

// w5cRecord serves one request against the real handler stack (auth, routes,
// templates) and returns the recorder. httptest.NewRequest/NewRecorder only
// — nothing here binds a listener, so rule 15's `:PORT` trap cannot apply.
func w5cRecord(t *testing.T, srv *Server, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

var _ = w5cOrientedV
