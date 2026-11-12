package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// FIX16c (James, 2026-10-06): the keyboard-only check-in loop for the
// shift-change rush. The Enter-walk itself is script (gate_script,
// _gatekeys.html) and is driven end to end by the Playwright run in the
// stream's report; what the server decides — which control holds focus on
// each page the loop lands on, that the native tab order is the entry
// order, and that the shortcuts help is served — is pinned here.

var fix16AutofocusTagRe = regexp.MustCompile(`<(?:input|select|button|textarea)\b[^>]*\bautofocus\b[^>]*>`)

// fix16Focused returns every control on the page carrying autofocus, with
// <style>/<script> bodies stripped so prose cannot count.
func fix16Focused(page string) []string {
	page = styleRe.ReplaceAllString(page, "")
	page = regexp.MustCompile(`(?s)<script\b.*?</script>`).ReplaceAllString(page, "")
	return fix16AutofocusTagRe.FindAllString(page, -1)
}

func fix16cGet(t *testing.T, srv *Server, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d", path, rec.Code)
	}
	return rec.Body.String()
}

// fix16AssertNameBoxFocused: exactly one focused control, and it is the
// Last name box (id new-in) — the first field of the next check-in.
func fix16AssertNameBoxFocused(t *testing.T, page, when string) {
	t.Helper()
	got := fix16Focused(page)
	if len(got) != 1 {
		t.Fatalf("%s: %d controls carry autofocus, want exactly 1: %q", when, len(got), got)
	}
	if !strings.Contains(got[0], `id="new-in"`) || !strings.Contains(got[0], `name="new_last"`) {
		t.Fatalf("%s: focus lands on %s, want the Last name box (id new-in) ready for the next person", when, got[0])
	}
}

func TestFix16NameBoxHoldsFocusAndTabOrderIsTheEntryOrder(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	page := fix16cGet(t, srv, "/gate")
	fix16AssertNameBoxFocused(t, page, "a fresh /gate")

	// Native tab order = DOM order (no positive tabindex anywhere), so the
	// JS-off order must already be Last -> First -> Company -> Vehicle ->
	// CHECK IN.
	if m := regexp.MustCompile(`tabindex="[1-9]`).FindString(page); m != "" {
		t.Errorf("a positive tabindex (%s) on /gate reorders Tab away from the page order", m)
	}
	form := singleCheckInFormRe.FindString(page)
	if form == "" {
		t.Fatal("no individual check-in form on /gate")
	}
	order := []string{`id="new-in"`, `id="new-first-in"`, `id="company-select-in"`, `id="vehicle-ind"`, `class="button-primary p-checkin-individual`}
	last := -1
	for _, m := range order {
		i := strings.Index(form, m)
		if i < 0 {
			t.Fatalf("individual form lacks %s", m)
		}
		if i < last {
			t.Errorf("%s comes before the field ahead of it — Tab would not walk name, company, vehicle, CHECK IN", m)
		}
		last = i
	}

	vpage := fix16cGet(t, srv, "/gate/vehicle")
	fix16AssertNameBoxFocused(t, vpage, "a fresh /gate/vehicle")
	if m := regexp.MustCompile(`tabindex="[1-9]`).FindString(vpage); m != "" {
		t.Errorf("a positive tabindex (%s) on /gate/vehicle", m)
	}
}

func TestFix16AfterASaveTheReturnedPageFocusesTheNameBox(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	ids := checkInCrew(t, srv, st, 2)

	// 1. one person checked in on /gate.
	vals := singleCheckInForm(t, srv, persons[0].Name)
	vals.Set("new_kind", "existing")
	vals.Set("new_value", persons[0].PersonID)
	rec := postForm(t, srv, "/gate/checkin/single", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("check-in = %d: %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/gate") || strings.HasPrefix(loc, "/gate/") {
		t.Fatalf("an individual check-in lands on %q, want /gate", loc)
	}
	fix16AssertNameBoxFocused(t, fix16cGet(t, srv, loc), "after a check-in")

	// 2. a check-out off the roster on /gate (Remove — no departure).
	out := url.Values{"gate_page": {"individual"}, "visit_ids": {ids[0]}, "time_source": {"default"}}
	rec = postForm(t, srv, "/gate/checkout", out)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("roster check-out = %d: %s", rec.Code, rec.Body.String())
	}
	fix16AssertNameBoxFocused(t, fix16cGet(t, srv, rec.Header().Get("Location")), "after a roster check-out")

	// 3. a staged check-out committed from the confirm box.
	_, cv := gateDepartSelected(t, srv, ids[1:])
	cv.Set("vehicle_unknown", "1")
	cv.Set("destination_id", "conneco")
	cv = gateMarkDriver(cv, 0)
	rec = postForm(t, srv, "/gate/checkout/vehicle", cv)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("staged check-out commit = %d: %s", rec.Code, rec.Body.String())
	}
	fix16AssertNameBoxFocused(t, fix16cGet(t, srv, rec.Header().Get("Location")), "after a staged check-out")
}

// The staged check-out confirm box is a question the officer has just asked
// for, so its first box (Vehicle number) takes focus — and the Last name box
// gives it up for that state, so there is still exactly one.
func TestFix16StagedCheckOutFocusesTheConfirmBox(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	ids := checkInCrew(t, srv, st, 2)
	for _, gatePage := range []string{"individual", ""} {
		form := url.Values{"visit_ids": ids}
		if gatePage != "" {
			form.Set("gate_page", gatePage)
		}
		rec := postForm(t, srv, "/gate/checkout/select", form)
		if rec.Code != http.StatusOK {
			t.Fatalf("stage (%q) = %d", gatePage, rec.Code)
		}
		got := fix16Focused(rec.Body.String())
		if len(got) != 1 || !strings.Contains(got[0], `id="vehicle-out"`) {
			t.Errorf("staged check-out (gate_page=%q): focused %q, want only the confirm box's Vehicle number (vehicle-out)", gatePage, got)
		}
	}
}

func TestFix16ShortcutsHelpIsServedOnBothGatePages(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	for _, path := range []string{"/gate", "/gate/vehicle"} {
		page := fix16cGet(t, srv, path)
		help := regexp.MustCompile(`(?s)<details class="gate-keys"[^>]*>.*?</details>`).FindString(page)
		if help == "" {
			t.Fatalf("%s: no Keyboard shortcuts <details>", path)
		}
		if !strings.Contains(help, " hidden>") {
			t.Errorf("%s: the help must be served hidden (shown by its script) so a scripting-off officer is not offered dead keys", path)
		}
		if !strings.Contains(help, "<summary>Keyboard shortcuts</summary>") {
			t.Errorf("%s: help summary is not \"Keyboard shortcuts\"", path)
		}
		for _, k := range []string{"i", "o", "v", "/", "h", "?"} {
			if !strings.Contains(help, "<kbd>"+k+"</kbd>") {
				t.Errorf("%s: help does not list the %q key", path, k)
			}
		}
		for _, banned := range []string{"Super", "Windows", "Ctrl", "Shift+"} {
			if strings.Contains(help, banned) {
				t.Errorf("%s: help names a %s combo — the work PC swallows Super, and Ctrl+Shift+digit rewrites the key", path, banned)
			}
		}
		hi, si := strings.Index(page, `data-gate-keys`), strings.Index(page, `<script id="gate-keys-script">`)
		if si < 0 || si < hi {
			t.Errorf("%s: the shortcuts script is missing or runs before the help it reveals", path)
		}
	}
	// On /gate with vehicle mode, v goes to the vehicle page; on the vehicle
	// page it is the Vehicle number box.
	if !strings.Contains(fix16cGet(t, srv, "/gate"), "<kbd>v</kbd></dt><dd>Vehicle check-in page") {
		t.Error("/gate (vehicle mode on): v is not described as the vehicle check-in page")
	}
	if !strings.Contains(fix16cGet(t, srv, "/gate/vehicle"), "<kbd>v</kbd></dt><dd>Vehicle number box") {
		t.Error("/gate/vehicle: v is not described as the Vehicle number box")
	}
}

// The keys move focus and follow links; none of them may write, confirm on
// the officer's behalf, or press the commit. The one button the script may
// press is "Check out selected", which only stages.
func TestFix16ShortcutsNeverCommitOrAnswerAConfirm(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	page := fix16cGet(t, srv, "/gate")
	script := regexp.MustCompile(`(?s)<script id="gate-keys-script">(.*?)</script>`).FindStringSubmatch(page)
	if script == nil {
		t.Fatal("no gate-keys-script on /gate")
	}
	src := script[1]
	for _, banned := range []string{"confirm(", ".submit(", "requestSubmit", "gate-commit", "fetch(", "XMLHttpRequest", "Super"} {
		if strings.Contains(src, banned) {
			t.Errorf("the shortcuts script contains %q", banned)
		}
	}
	if n := strings.Count(src, ".click()"); n != 1 || !strings.Contains(src, `button[formaction="/gate/checkout/select"]`) {
		t.Errorf("the shortcuts script presses %d buttons; the only one allowed is Check out selected (staging)", n)
	}
	// Letters are shortcuts only outside text boxes and selects, and never
	// with a modifier held.
	for _, must := range []string{"typingIn(e.target)", "e.ctrlKey || e.metaKey || e.altKey", "'SELECT'"} {
		if !strings.Contains(src, must) {
			t.Errorf("the shortcuts script lacks its guard %q", must)
		}
	}

	// The form halves: the Enter-walk lands ON the commit and stops; the
	// individual CHECK IN's own confirm is still asked in its submit listener.
	gs := page[strings.Index(page, `<template id="passenger-template">`):strings.Index(page, `<script id="gate-keys-script">`)]
	if !strings.Contains(gs, "focusDecisionAfter") || !strings.Contains(gs, "focusAfter(openInput)") {
		t.Fatal("the Enter-walk is not in gate_script")
	}
	if regexp.MustCompile(`commit\.click\(\)|gate-commit'\)\.click|requestSubmit`).MatchString(gs) {
		t.Error("gate_script presses the vehicle commit itself — Enter must stop on it, never through its confirm")
	}
	if !strings.Contains(gs, "if (msg && !confirm(msg)) { e.preventDefault(); }") {
		t.Error("the individual CHECK IN no longer asks its confirm")
	}
}
