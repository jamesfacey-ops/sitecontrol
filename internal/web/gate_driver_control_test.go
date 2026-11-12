package web

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// Stream A1 (campaign H-2, H-3, M-2): the gate's driver control and the
// compact deadline disclosure. These tests exist because the whole class was
// invisible to markup-string checks that only looked at names and counts: a
// `required` radio inside a `hidden` label is well-formed HTML that a browser
// can never satisfy, and a <details> inside a <p> is well-formed source that
// the parser silently rewrites.

var htmlTagRe = regexp.MustCompile(`(?s)<(/?)([a-zA-Z][a-zA-Z0-9]*)((?:[^>"']|"[^"]*"|'[^']*')*)>`)

var voidTags = map[string]bool{"input": true, "br": true, "hr": true, "img": true, "meta": true, "link": true}

// hasBoolAttr reports whether a tag's attribute text carries the boolean
// attribute name (as a whole word, outside quoted values).
func hasBoolAttr(attrs, name string) bool {
	stripped := regexp.MustCompile(`"[^"]*"|'[^']*'`).ReplaceAllString(attrs, `""`)
	return regexp.MustCompile(`(?i)(^|\s)` + name + `(\s|=|/|$)`).MatchString(stripped)
}

// requiredButUnreachable returns every `required` form control in markup that
// sits inside (or is itself) a `hidden` element or is type=hidden — controls
// a browser's constraint validation still enforces but the user cannot see,
// so the form can never be submitted.
func requiredButUnreachable(markup string) []string {
	var bad []string
	type frame struct {
		tag    string
		hidden bool
	}
	var stack []frame
	hiddenDepth := 0
	for _, m := range htmlTagRe.FindAllStringSubmatch(markup, -1) {
		closing, tag, attrs := m[1] == "/", strings.ToLower(m[2]), m[3]
		if closing {
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i].tag == tag {
					for _, f := range stack[i:] {
						if f.hidden {
							hiddenDepth--
						}
					}
					stack = stack[:i]
					break
				}
			}
			continue
		}
		self := hasBoolAttr(attrs, "hidden")
		if tag == "input" || tag == "select" || tag == "textarea" {
			hiddenType := regexp.MustCompile(`(?i)type\s*=\s*["']?hidden`).MatchString(attrs)
			if hasBoolAttr(attrs, "required") && (self || hiddenType || hiddenDepth > 0) {
				bad = append(bad, m[0])
			}
		}
		if voidTags[tag] || strings.HasSuffix(strings.TrimSpace(attrs), "/") {
			continue
		}
		stack = append(stack, frame{tag, self})
		if self {
			hiddenDepth++
		}
	}
	return bad
}

// TestRequiredButUnreachableCanFail is the instrument's own precondition
// (rule 5/6): the detector must flag the exact shape that shipped.
func TestRequiredButUnreachableCanFail(t *testing.T) {
	shipped := `<label class="driver-flag" hidden><input type="radio" name="driver_index" value="0" required><span>driver</span></label>`
	if got := requiredButUnreachable(shipped); len(got) != 1 {
		t.Fatalf("detector found %d, want 1 for the shipped shape", len(got))
	}
	ok := `<label class="driver-flag" hidden><input type="radio" name="driver_index" value="0"></label><select name="driver_index" required></select>`
	if got := requiredButUnreachable(ok); len(got) != 0 {
		t.Fatalf("detector flagged a fixed shape: %v", got)
	}
}

func gateDriverFixtures(t *testing.T) map[string]struct {
	page, formID string
} {
	t.Helper()
	srv, st, _ := newGateTestServer(t)
	visitIDs := checkInCrew(t, srv, st, 3)
	out, _ := gateBuildOutbound(t, srv, "TRK-14", visitIDs, nil, -1)
	srv2, _, _ := newGateTestServer(t)
	in, _ := gateBuild(t, srv2, gateInbound, "TRK-14", []string{"Dana Driver", "Pat Passenger", "Chris Crew"}, -1)
	return map[string]struct{ page, formID string }{
		"inbound (arriving vehicle)":    {in, formIDFor(gateInbound)},
		"outbound (compact check-out)": {out, formIDFor(gateOutbound)},
	}
}

// TestNoHiddenControlOnTheVehicleFormsIsRequired: H-2/H-3 root cause. With
// the shipped markup each row's hidden driver radio carried `required`, so
// the browser refused to submit (with no request and no message) whenever a
// form had 2+ rows.
func TestNoHiddenControlOnTheVehicleFormsIsRequired(t *testing.T) {
	for name, fx := range gateDriverFixtures(t) {
		t.Run(name, func(t *testing.T) {
			markup := gateFormMarkup(t, fx.page, fx.formID)
			// precondition: the rows' driver controls are actually in the markup.
			if n := len(driverRadioRe.FindAllString(markup, -1)); n != 3 {
				t.Fatalf("precondition: %d driver_index radios for 3 people — the test would pass by finding nothing", n)
			}
			if bad := requiredButUnreachable(markup); len(bad) != 0 {
				t.Fatalf("%d required control(s) the user cannot see or fill in, which blocks form submission in a browser:\n%s", len(bad), strings.Join(bad, "\n"))
			}
		})
	}
}

// TestScriptedRowTemplateHasNoRequiredHiddenControl: the clone source is
// inert but every row the script adds is built from it.
func TestScriptedRowTemplateHasNoRequiredHiddenControl(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	_, rec := getGate(t, srv)
	i := strings.Index(rec, `<template id="passenger-template">`)
	if i < 0 {
		t.Fatal("precondition: /gate/vehicle carries no passenger-template")
	}
	tpl := rec[i : i+strings.Index(rec[i:], "</template>")]
	if bad := requiredButUnreachable(tpl); len(bad) != 0 {
		t.Fatalf("scripted rows would carry hidden required controls: %v", bad)
	}
}

// TestScriptOffFormOffersAVisibleLabelledDriverChoice: JS off there is no
// script to reveal or fill anything, so the choice must be in the served
// markup: a visible select named driver_index, with an associated label,
// listing every current occupant.
func TestScriptOffFormOffersAVisibleLabelledDriverChoice(t *testing.T) {
	for name, fx := range gateDriverFixtures(t) {
		t.Run(name, func(t *testing.T) {
			markup := gateFormMarkup(t, fx.page, fx.formID)
			sel := driverSelectRe.FindString(markup)
			if sel == "" {
				t.Fatalf("no driver <select> in the served form:\n%s", markup)
			}
			open := sel[:strings.Index(sel, ">")+1]
			if hasBoolAttr(open, "hidden") {
				t.Fatalf("the driver select is hidden: %s", open)
			}
			id := regexp.MustCompile(`\bid="([^"]+)"`).FindStringSubmatch(open)
			if id == nil {
				t.Fatalf("the driver select has no id, so no <label for> can name it: %s", open)
			}
			lab := regexp.MustCompile(`(?s)<label[^>]*for="` + regexp.QuoteMeta(id[1]) + `"[^>]*>(.*?)</label>`).FindStringSubmatch(markup)
			if lab == nil || !strings.Contains(strings.ToLower(lab[1]), "driver") {
				t.Fatalf("the driver select has no visible label naming it (got %v)", lab)
			}
			if got := len(driverOptionRe.FindAllString(sel, -1)); got != 4 {
				t.Fatalf("driver select has %d options, want 4 (choose + 3 occupants): %s", got, sel)
			}
		})
	}
}

// TestCompactCheckOutOfTwoPeopleCommitsWithADriverChosenScriptOff drives the
// whole JS-off path: serve the form, verify no browser-blocking control,
// choose a driver from the select, commit, both people leave.
func TestCompactCheckOutOfTwoPeopleCommitsWithADriverChosenScriptOff(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	visitIDs := checkInCrew(t, srv, st, 2)
	page, vals := gateBuildOutbound(t, srv, "TRK-14", visitIDs, nil, -1)
	if bad := requiredButUnreachable(gateFormMarkup(t, page, formIDFor(gateOutbound))); len(bad) != 0 {
		t.Fatalf("a browser could not submit this form: %v", bad)
	}
	if got := vals.Get("driver_index"); got != "" {
		t.Fatalf("precondition: driver_index=%q before anyone was chosen", got)
	}
	// Server-side refusal is unchanged: no driver chosen is refused with a message.
	vals.Set("destination_id", "conneco")
	rec := postForm(t, srv, "/gate/checkout/vehicle", vals)
	if rec.Code == http.StatusSeeOther {
		t.Fatal("a departure with no driver chosen was accepted")
	}
	if got := vehicleError(t, rec.Body.String(), gateOutbound); !strings.Contains(got, "Mark who was driving") {
		t.Fatalf("refusal does not say what to do: %q", got)
	}
	vals = gateMarkDriver(vals, 1)
	rec = postForm(t, srv, "/gate/checkout/vehicle", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("2-person departure with a driver chosen = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 0 {
		t.Fatalf("%d people still on site after the crew departed", len(open))
	}
}

// TestDeadlineDisclosureIsNotInsideAParagraph: M-2. <details> closes an
// open <p> in the HTML parser, so a <details> written inside <p> is
// ejected and the wrapper the script queries is left empty.
func TestDeadlineDisclosureIsNotInsideAParagraph(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	visitIDs := checkInCrew(t, srv, st, 2)
	page, _ := gateBuildOutbound(t, srv, "TRK-14", visitIDs, nil, -1)
	markup := gateFormMarkup(t, page, formIDFor(gateOutbound))
	if !strings.Contains(markup, "<details") || !strings.Contains(markup, "deadline-compact") {
		t.Fatal("precondition: the compact form has no deadline disclosure")
	}
	// Flow content that implicitly closes an open <p>.
	closesP := map[string]bool{"details": true, "div": true, "ul": true, "ol": true, "table": true, "fieldset": true, "form": true, "p": true, "section": true, "h1": true, "h2": true, "h3": true}
	inP := false
	for _, m := range htmlTagRe.FindAllStringSubmatch(markup, -1) {
		closing, tag := m[1] == "/", strings.ToLower(m[2])
		switch {
		case tag == "p" && closing:
			inP = false
		case inP && !closing && closesP[tag]:
			t.Fatalf("<%s> opens inside an unclosed <p>; the parser will eject it: %s", tag, m[0])
		case tag == "p" && !closing:
			inP = true
		}
	}
	// And the wrapper the prefill script queries really contains the details.
	wrap := regexp.MustCompile(`(?s)<div[^>]*class="[^"]*deadline-compact[^"]*"[^>]*>(.*?)</div>`).FindStringSubmatch(markup)
	if wrap == nil || !strings.Contains(wrap[1], "<details") {
		t.Fatalf("the .deadline-compact wrapper does not contain the <details>: %v", wrap)
	}
}
