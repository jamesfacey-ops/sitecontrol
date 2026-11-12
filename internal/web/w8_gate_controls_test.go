// w8_gate_controls_test.go — W8 (item 10, the trade register): [[D-71]]'s
// THREE LOCK CONDITIONS, which gate P-4 for W8 was discharged on.
//
// Written from the LOCKED W8 criteria only. No built implementation was read.
//
// D-71's owner is "W8's CRITERIA LOCK" and its trigger is "gate P-4 for W8 —
// ALL THREE, BEFORE ANY OF IT IS BUILT". The three:
//
//	OBLIGATION 1  W8-23(c) — the trade control at ALL FIVE gate.html sites,
//	              ENUMERATED, plus (2′) the field is emitted on EVERY ROW,
//	              empty when unused, NEVER CONDITIONALLY.
//	OBLIGATION 2  W8-23(c) — the add-it escape MUST NOT REFUSE, with what the
//	              screen does NEXT asserted: the person is created, tagged to
//	              the EXISTING trade, and EXACTLY ONE event is appended.
//	OBLIGATION 3  W8-23(g) — the POSITIONAL-ZIP hazard guarded on ARRAY
//	              LENGTH, not occupant_index, CHECKED ON THE RECEIVED BODY.
//
// ⚠ A D-nn citation may claim only what that ruling's text says. D-71 is
// cited here for those three obligations and for nothing else; where W8's
// screens SIT is not ruled by it and no assertion below depends on that.
package web

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// w8GateTemplate reads gate_vehicle.html as bytes. The five sites are
// asserted over the template SOURCE because three of the five are inside a
// JAVASCRIPT STRING and never reach the server-rendered DOM at all.
//
// ⟦R-27, James, 2026-09-16⟧ was gate.html — the whole-vehicle accumulate
// form (the passenger-template, the five trade-control sites, gate_script)
// moved unchanged to gate_vehicle.html when gate.html became the
// streamlined individual page; see that file's own comment. The
// individual page's CHECK IN form has no trade control at all (R-27's own
// field list omits Trade), so these five sites only ever existed on the
// vehicle page.
func w8GateTemplate(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "templates", "gate_vehicle.html")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("PRECONDITION: gate_vehicle.html could not be read at %s: %v — D-71's five sites are asserted over this file", p, err)
	}
	return string(b)
}

// w8GateSite is one of D-71's five enumerated sites, named by ROLE and
// identified by an anchor that survives the workgroup→trade rename.
//
// ⚠⚠ THE SITES ARE FIVE AND REVISION 2 SAID "the officer is offered the trade
// list", SINGULAR, "while this document's own §2.9 had MEASURED four of the
// five and listed them". D-71 names it "the FILTER NARROWER THAN ITS SUBJECT
// class" in terms. The control is asserted AT EVERY SITE, ENUMERATED.
//
// ⟦[[D-84]], 2026-09-05⟧ ⚠⚠ AND THE CONTROL IS ASSERTED BY KIND, NOT BY
// PRESENCE. ⟦was: `hasNew := strings.Contains(block, site.TradeAnchor)` — a
// containment check on `name="occupant_trade"` etc.⟧ The criterion says "the
// officer is offered the trade LIST"; the containment check was GREEN on a
// FREE-TEXT BOX carrying that name, so this test certified W8-23(c) through a
// lock while the build offered no list anywhere [MEASURED by d83-checkin-gap:
// select=0 datalist=0 list-attr=0 on the served form]. Criterion said picker ·
// test checked presence · build was free text. That is the asserted-but-non-
// discriminating class this project catalogued for a day, sitting on a LOCKED
// criterion. James chose to fix the test as well as the build.
//
// The corrected instrument: at each CONTROL site the trade control must be a
// <select> (Picker) that offers the "not listed — add it" escape (Escape); at
// the CARRIER site the hidden field must carry the PICK; at the COPY site the
// script must copy the PICK. A text box satisfies none of these — and
// TestW8GateOffersTheTradeControlAtAllFiveSites PRODUCES that: it re-applies
// the old free-text form to the template text and requires its own check to
// go red on it.
type w8GateSite struct {
	Role string
	// Block names WHICH of the template's three regions the site lives in.
	// ⚠ THE REGION IS PART OF THE SITE'S IDENTITY, and getting it wrong is
	// how the enumeration silently collapses from five to three: `class=
	// "p-workgroup"` occurs in BOTH the server-rendered row and the
	// <template>, so a whole-file Contains() scores site (3) off site (1)'s
	// markup and (m8-75) — the drift mutant — becomes undetectable.
	Block w8Region
	// WorkgroupAnchor is the site's control as measured at the W8 lock
	// (gate.html, 1,148 lines): :508, :420, :708, :545, :1064/:1070. It is
	// the PRECONDITION that the site still exists — never the criterion.
	WorkgroupAnchor string
	// TradeAnchor is the free-text spelling W8 built. Kept as the second
	// half of the same precondition (the site exists under either spelling);
	// ⚠ its PRESENCE no longer satisfies anything.
	TradeAnchor string
	// Picker is the D-84 criterion at this site: the trade control IS A
	// LIST. At the two non-control sites it is the carrier/copy of the pick.
	Picker *regexp.Regexp
	// Escape, at the control sites: the list offers "not listed — add a new
	// trade" AND the escape's own text box sits beside it (intake's shape).
	// nil at the carrier/copy sites.
	Escape *regexp.Regexp
	// FreeTextPlant re-applies the OLD free-text form to this site's block,
	// for the produced control: the text the instrument must go red on.
	FreeTextPlant func(block string) string
	Why           string
}

// w8Region names one of gate.html's three regions.
type w8Region int

const (
	// w8ServerRow is everything BEFORE <template id="passenger-template">:
	// the markup the SERVER renders, including the accumulated rows, the
	// Add-person box and the hidden undo row.
	w8ServerRow w8Region = iota
	// w8RowTemplate is the <template id="passenger-template"> element: the
	// row the BROWSER clones. ⚠ It is NOT inside <script> — a split on the
	// first <script> lands on the roster-data block at :291 and puts the
	// whole server row on the wrong side. [MEASURED: that split yielded 0
	// server fields and 6 template fields, and the instrument said so.]
	w8RowTemplate
	// w8Script is the <script> block at :715: the JS COPY at :1064/:1070.
	w8Script
)

func (r w8Region) String() string {
	switch r {
	case w8ServerRow:
		return "the SERVER-rendered markup"
	case w8RowTemplate:
		return "<template id=\"passenger-template\">"
	default:
		return "the <script> block"
	}
}

// w8Regions splits gate.html into its three regions, by ANCHOR rather than by
// line number — line numbers move and W8 edits this file.
func w8Regions(t *testing.T, tpl string) map[w8Region]string {
	t.Helper()
	const tplOpen = `<template id="passenger-template">`
	const tplClose = `</template>`
	i := strings.Index(tpl, tplOpen)
	if i < 0 {
		t.Fatalf("PRECONDITION (instrument): gate.html carries no %s — D-71's site (3), the JS ROW TEMPLATE, "+
			"cannot be separated from the server-rendered row, and (m8-75) is undetectable", tplOpen)
	}
	j := strings.Index(tpl[i:], tplClose)
	if j < 0 {
		t.Fatalf("PRECONDITION (instrument): %s is never closed", tplOpen)
	}
	j += i + len(tplClose)
	return map[w8Region]string{
		w8ServerRow:   tpl[:i],
		w8RowTemplate: tpl[i:j],
		w8Script:      tpl[j:],
	}
}

// w8TradeSelect returns a regexp matching the whole <select> block that
// carries the trade PICK under `name`, its option body captured.
func w8TradeSelect(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?s)<select\b[^>]*\bname="` + name + `"[^>]*>(.*?)</select>`)
}

// w8DropSelect is the free-text plant for a control site: it deletes the
// picker and leaves the escape's text box — which is EXACTLY the pre-D-84
// form (a lone <input type="text" name="occupant_trade"/"new_trade">).
func w8DropSelect(name string) func(string) string {
	re := w8TradeSelect(name)
	return func(block string) string { return re.ReplaceAllString(block, "") }
}

// w8GateSites is D-71 obligation 1's enumeration, by ROLE and REGION, with
// D-84's picker criterion at each.
var w8GateSites = []w8GateSite{
	{
		Role:            "(1) the accumulated passenger row",
		Block:           w8ServerRow,
		WorkgroupAnchor: `name="occupant_workgroup"`,
		TradeAnchor:     `name="occupant_trade"`,
		Picker:          w8TradeSelect("occupant_trade_id"),
		Escape:          regexp.MustCompile(`<input\b[^>]*\bname="occupant_trade"`),
		FreeTextPlant:   w8DropSelect("occupant_trade_id"),
		Why:             "the row the officer has already added; without a trade LIST every accumulated passenger is a text box",
	},
	{
		Role:            "(2) the *Add person* box",
		Block:           w8ServerRow,
		WorkgroupAnchor: `name="new_workgroup"`,
		TradeAnchor:     `name="new_trade"`,
		Picker:          w8TradeSelect("new_trade_id"),
		Escape:          regexp.MustCompile(`<input\b[^>]*\bname="new_trade"`),
		FreeTextPlant:   w8DropSelect("new_trade_id"),
		Why:             "the open box D-66 names — the door most persons enter the roster through",
	},
	{
		Role:            "(3) the JAVASCRIPT ROW TEMPLATE",
		Block:           w8RowTemplate,
		WorkgroupAnchor: `name="occupant_workgroup"`,
		TradeAnchor:     `name="occupant_trade"`,
		Picker:          w8TradeSelect("occupant_trade_id"),
		Escape:          regexp.MustCompile(`<input\b[^>]*\bname="occupant_trade"`),
		FreeTextPlant:   w8DropSelect("occupant_trade_id"),
		Why:             "must not drift from the server version — (m8-75). A template that drifts drops the field for EVERY row the officer adds in the browser, and nothing on the page says so",
	},
	{
		Role:            "(4) the HIDDEN UNDO ROW",
		Block:           w8ServerRow,
		WorkgroupAnchor: `name="removed_workgroup"`,
		TradeAnchor:     `name="removed_trade"`,
		Picker:          regexp.MustCompile(`<input\b[^>]*type="hidden"[^>]*\bname="removed_trade_id"`),
		FreeTextPlant: func(block string) string {
			return regexp.MustCompile(`<input\b[^>]*\bname="removed_trade_id"[^>]*>`).ReplaceAllString(block, "")
		},
		Why: "restores a removed passenger's fields; without carrying the PICK an undo silently drops the trade the officer chose",
	},
	{
		Role:            "(5) the JS COPY",
		Block:           w8Script,
		WorkgroupAnchor: `p-open-workgroup`,
		TradeAnchor:     `p-open-trade`,
		Picker:          regexp.MustCompile(`p-open-trade-id`),
		FreeTextPlant: func(block string) string {
			return strings.ReplaceAll(block, "p-open-trade-id", "p-open-trade")
		},
		Why: "copies the open box's PICK into the new row and then clears it; without it the chosen trade is lost between box and row",
	},
}

// w8SiteOffersTheList is the discriminating check for one site: true only
// when the site's block carries the trade control AS A LIST (or, at the
// carrier/copy sites, carries/copies the PICK), with the escape beside it.
func w8SiteOffersTheList(site w8GateSite, block string) (bool, string) {
	m := site.Picker.FindStringSubmatch(block)
	if m == nil {
		return false, "no trade picker (or pick carrier) matched " + site.Picker.String()
	}
	if site.Escape == nil {
		return true, ""
	}
	// The picker must offer the "not listed — add a new trade" escape …
	if len(m) < 2 || !strings.Contains(m[1], `value="__new__"`) {
		return false, "the trade <select> offers no \"not listed — add a new trade\" option"
	}
	// … and the escape's own text box must sit beside it (intake's shape).
	if !site.Escape.MatchString(block) {
		return false, "the add-new escape has no text box beside the picker: " + site.Escape.String()
	}
	return true, ""
}

// TestW8GateOffersTheTradeControlAtAllFiveSites is [[D-71]] OBLIGATION 1,
// corrected under [[D-84]] to assert the criterion's own word: a LIST.
//
// ⚠ THE PRECONDITION IS THE WORKGROUP/TRADE ANCHOR, and it is what makes the
// absence below mean "not built" rather than "the site moved": each of the
// five is asserted to exist TODAY under one of its two spellings, so a red on
// the picker is a statement about the control's KIND and not about the
// template drifting out from under this test.
//
// ⭐ THE CONTROL IS PRODUCED, per site: the old free-text form is re-applied
// to the block's TEXT (never to the file) and the instrument must go red on
// it. An instrument that passes a text box has not been corrected, whatever
// it passes on the real template.
func TestW8GateOffersTheTradeControlAtAllFiveSites(t *testing.T) {
	regions := w8Regions(t, w8GateTemplate(t))

	var missing []string
	for _, site := range w8GateSites {
		block := regions[site.Block]
		hasOld := strings.Contains(block, site.WorkgroupAnchor)
		hasNew := strings.Contains(block, site.TradeAnchor)
		if !hasOld && !hasNew {
			t.Errorf("PRECONDITION: site %s, in %s, carries NEITHER %q nor %q — the site moved, and this assertion "+
				"is about the wrong thing. D-71 enumerates FIVE sites and this test asserts each by ROLE AND "+
				"REGION; if the template restructured, the enumeration must be re-derived from it, not patched",
				site.Role, site.Block, site.WorkgroupAnchor, site.TradeAnchor)
			continue
		}
		// THE CRITERION, first: is the officer offered the LIST here?
		if ok, why := w8SiteOffersTheList(site, block); !ok {
			missing = append(missing, site.Role+" — "+why+" — "+site.Why)
			continue
		}
		// --- CONTROL, produced where the site passes: the pre-D-84
		// free-text form of this very site must FAIL the same check. The
		// plant is applied to the text, never to the file.
		planted := site.FreeTextPlant(block)
		if planted == block {
			t.Fatalf("(instrument) site %s: the free-text plant changed nothing although the site passed — the plant "+
				"and the criterion disagree about what the picker is, so the control below would test the real block twice", site.Role)
		}
		if ok, _ := w8SiteOffersTheList(site, planted); ok {
			t.Fatalf("[[D-84]] CONTROL FAILED at site %s: with the picker removed and only the free-text box left — the "+
				"exact pre-D-84 form — this instrument still says the officer is offered the trade list. "+
				"An instrument that cannot tell a picker from a text box is how W8-23(c) survived a lock unmet; "+
				"its clean pass means nothing", site.Role)
		}
	}
	if len(missing) != 0 {
		t.Fatalf("[[D-71]] OBLIGATION 1 / W8-23(c) as corrected by [[D-84]]: the officer is NOT offered the trade LIST "+
			"at %d of the %d enumerated gate.html sites:\n  %s\n"+
			"⚠ W8-23(c) says \"the officer is offered the trade list\" — a picker with a \"not on the list — add it\" "+
			"escape. A free-text box carrying the field's name is NOT that, and this test used to accept one.\n"+
			"⚠ Site (3) is the one that produces (m8-75): a JS row template that drifts from the server-rendered row "+
			"drops the control for EVERY row the officer adds in the browser",
			len(missing), len(w8GateSites), strings.Join(missing, "\n  "))
	}
}

// w8TemplateCommentRe matches one `{{/* … */}}` template comment.
var w8TemplateCommentRe = regexp.MustCompile(`(?s)\{\{/\*.*?\*/\}\}`)

// w8OccupantFieldRe finds every `name="occupant_…"` in a chunk of template.
var w8OccupantFieldRe = regexp.MustCompile(`name="(occupant_[a-z_]+)"`)

// TestW8ServerRowAndJSRowTemplateDoNotDrift is (m8-75)'s assertion, DERIVED.
//
// ⭐ MUTANT (m8-75): SERVER/JS TEMPLATE DRIFT — the trade control present in
// the server-rendered row (site 2/1) and ABSENT from the JS row template
// (site 3). ⚠ "That is how a half-typed row is lost."
//
// The field set is DERIVED from each block, never listed — the property is
// EQUALITY of the two sets, so a field added to one and not the other reds
// whatever it is called.
//
// ⟦NO PRE-CHUNK RED — the two sets are equal today.⟧ Its evidence is the
// PLANT produced below, never its clean pass.
func TestW8ServerRowAndJSRowTemplateDoNotDrift(t *testing.T) {
	tpl := w8GateTemplate(t)

	regions := w8Regions(t, tpl)
	server, js := regions[w8ServerRow], regions[w8RowTemplate]
	serverFields := w8OccupantFields(server)
	jsFields := w8OccupantFields(js)

	// PRECONDITION: both blocks must actually carry occupant fields, or the
	// equality below is `{} == {}` and could never fail.
	if len(serverFields) == 0 || len(jsFields) == 0 {
		t.Fatalf("PRECONDITION (instrument): the server row block yielded %d occupant fields and the JS row template %d. "+
			"An equality assertion between two empty sets cannot fail — this is the INSTRUMENT, not W8. "+
			"server=%v js=%v", len(serverFields), len(jsFields), serverFields, jsFields)
	}

	// --- CONTROL, produced: drop one field from the JS copy and the
	// equality must red. The plant is applied to the TEXT, never to the file.
	planted := strings.Replace(js, `name="`+jsFields[0]+`"`, `name="w8_planted_drift"`, 1)
	if w8SetsEqual(w8OccupantFields(planted), serverFields) {
		t.Fatalf("(m8-75) CONTROL FAILED: renaming %q out of the JS row template did NOT break the derived equality. "+
			"The instrument cannot see drift, so its clean pass means nothing", jsFields[0])
	}

	if !w8SetsEqual(serverFields, jsFields) {
		t.Fatalf("(m8-75) SERVER/JS TEMPLATE DRIFT: the server-rendered passenger row emits %v and the JS row template "+
			"emits %v. ⚠ A template that drifts from the server DROPS THE FIELD FOR EVERY ROW THE OFFICER ADDS IN "+
			"THE BROWSER, and nothing on the page says so — which is how a half-typed row is lost. "+
			"⚠ The sets are DERIVED from each block, never listed, so this reds for a field of any name",
			serverFields, jsFields)
	}
}

func w8OccupantFields(block string) []string {
	seen := map[string]bool{}
	for _, m := range w8OccupantFieldRe.FindAllStringSubmatch(block, -1) {
		seen[m[1]] = true
	}
	var out []string
	for f := range seen {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

func w8SetsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestW8TradeFieldIsEmittedOnEveryRowNeverConditionally is D-71 obligation
// 1's clause (2′).
//
// "THE FIELD IS EMITTED ON EVERY ROW, EMPTY WHEN UNUSED, NEVER CONDITIONALLY
// — asserted; mutant: emitted conditionally. ⚠ See (g): a conditionally-
// emitted field is the SHORT direction the existing zip guard is blind to."
//
// ⭐ This is the criterion where obligations 1 and 3 MEET, and stating that is
// the point: a template that emits the trade input inside a `{{if}}` produces
// exactly the array-length mismatch (g) guards, so a build that gets (2′)
// wrong and (g) right is still safe, and a build that gets BOTH wrong
// silently attaches one person's trade to another person's row.
//
// ⭐ GENUINE PRE-CHUNK RED: there is no trade input to be unconditional.
func TestW8TradeFieldIsEmittedOnEveryRowNeverConditionally(t *testing.T) {
	server := w8Regions(t, w8GateTemplate(t))[w8ServerRow]

	// ⟦[[D-84]]⟧ TWO fields now, both parallel arrays: the trade PICKER
	// (`occupant_trade_id`) and the add-new escape's box (`occupant_trade`).
	// Each must be unconditional; a `{{if}}` around either is the short case.
	for _, field := range []string{`name="occupant_trade_id"`, `name="occupant_trade"`} {
		idx := strings.Index(server, field)
		if idx < 0 {
			t.Fatalf("[[D-71]] OBLIGATION 1 clause (2′) / W8-23(c): the server-rendered passenger row emits no "+
				"%s field at all, so there is nothing to assert is UNCONDITIONAL. "+
				"⚠ The field must be emitted ON EVERY ROW, EMPTY WHEN UNUSED, NEVER CONDITIONALLY — a "+
				"conditionally-emitted field is the SHORT direction the existing zip guard (W8-23(g)) is blind to, "+
				"and the two defects compose into a silent field shift.\n"+
				"Fields the row emits today: %v", field, w8OccupantFields(server))
		}

		// The field must not sit inside a conditional action. The window
		// scanned backwards from the input to the enclosing ROW is small on
		// purpose: an `{{if}}` anywhere between the row's start and the field
		// is the defect. ⚠ The row now nests two `{{range}}`s (the pickers'
		// option lists), so the window runs back to the ROW's range — the
		// `{{range $f.Rows}}` — not merely the nearest one.
		window := server[:idx]
		if last := strings.LastIndex(window, "{{range $f.Rows}}"); last >= 0 {
			// Template COMMENTS are prose, not actions: a `{{/* … */}}` that
			// mentions a branch is not a branch. Stripped before the scan so
			// the instrument reads code.
			row := w8TemplateCommentRe.ReplaceAllString(window[last:], "")
			if strings.Contains(row, "{{if") || strings.Contains(row, "{{with") {
				t.Errorf("[[D-71]] OBLIGATION 1 clause (2′): the %s field is emitted inside a conditional "+
					"template action. It must be emitted on EVERY row, empty when unused. A row that emits a "+
					"different SET of the parallel arrays shifts every LATER row's fields by one", field)
			}
		} else {
			t.Fatalf("(instrument) no `{{range $f.Rows}}` precedes %s — the row's range anchor moved; re-derive it", field)
		}
	}
}

// ---------------------------------------------------------------------------
// [[D-71]] OBLIGATION 3 / W8-23(g) — THE POSITIONAL-ZIP HAZARD
// ---------------------------------------------------------------------------

// TestW8ShortOccupantArrayIsRefusedWhole is [[D-71]] OBLIGATION 3.
//
// ⚠⚠ THE EXISTING GUARD IS BLIND IN EXACTLY THE DIRECTION A TRADE FIELD
// PRODUCES, and this is MEASURED, not predicted. [MEASURED —
// internal/web/gate.go:566-578] `parseVehicleForm` builds `rows` as the
// MAXIMUM of the parallel arrays; [gate.go:627-639] `checkIndexAlignment`
// compares `len(r.Form["occupant_index"])` against `rows` and requires the
// indices to be 0..N-1.
//
// ⚠ SO THE GUARD FIRES WHEN AN ARRAY IS LONGER THAN THE INDEX LIST AND IS
// BLIND WHEN ONE IS SHORTER — and a field emitted only on the rows that need
// it is EXACTLY THE SHORT CASE: `rows` is unchanged (it is the max of the
// others), `occupant_index` still has length `rows`, THE GUARD PASSES, and
// every value after the first gap attaches to the wrong person.
//
// The file's own comment states the consequence [MEASURED — gate.go:614-620]:
//
//	"A row that emitted a different set of them would shift every LATER row's
//	fields by one — SILENTLY ATTACHING ONE PERSON'S NAME TO ANOTHER PERSON'S
//	COMPANY, and at 26 rows that is both likelier and far harder to spot than
//	at four."
//
// THE ASSERTION: every occupant array present in the submitted body has the
// SAME LENGTH as occupant_index — CHECKED ON THE RECEIVED BODY, never on the
// parsed rows, "because parseVehicleForm has already normalised the lengths
// AND THAT NORMALISATION IS PRECISELY WHAT WOULD HIDE THE SHIFT".
//
// ⚠ THE ARRAY EXERCISED IS `occupant_company`, and the choice is stated: it
// is an occupant array that SURVIVES W8, so this test names no unbuilt field
// while asserting the identical property (m8-76) names for the trade one.
// The guard is one guard over all the arrays; a build that fixes it for
// `occupant_trade` alone has not fixed it.
//
// ⭐ GENUINE PRE-CHUNK RED: [MEASURED] checkIndexAlignment asserts NOTHING
// about array-length parity, so the parity assertion is ABSENT, NOT MERELY
// UNSATISFIED — and a short array is ACCEPTED today.
func TestW8ShortOccupantArrayIsRefusedWhole(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	_, vals := gateBuild(t, srv, gateInbound, "TRK-14",
		[]string{"Dana Driver", "Pat Passenger", "Chris Crew"}, 0)

	// PRECONDITION: the intact form is intact — three rows, three indices,
	// three of every occupant array. Without this the "short" body below is
	// not short, it is just different.
	if got := len(vals["occupant_index"]); got != 3 {
		t.Fatalf("PRECONDITION: the intact form carries %d indices, want 3", got)
	}
	if got := len(vals["occupant_company"]); got != 3 {
		t.Fatalf("PRECONDITION: the intact form carries %d occupant_company values, want 3 — this test's whole "+
			"subject is an array SHORTER than occupant_index, and if the array is not full-length to begin with "+
			"the case is not posed", got)
	}

	// THE SHORT BODY: the MIDDLE row's occupant_company is omitted, exactly
	// as a template emitting the field only on the rows that need it would.
	// Row 2 then receives row 3's company and row 3 receives nothing.
	short := make([]string, 0, 2)
	short = append(short, vals["occupant_company"][0], vals["occupant_company"][2])
	vals["occupant_company"] = short

	rec := postForm(t, srv, "/gate/checkin", vals)
	if rec.Code == http.StatusSeeOther {
		trips, err := domain.Trips(st)
		if err != nil {
			t.Fatal(err)
		}
		t.Fatalf("[[D-71]] OBLIGATION 3 / W8-23(g) ((m8-76)): a submit carrying 3 occupant_index values and only "+
			"%d occupant_company values was ACCEPTED (303, %d trip(s) authored).\n"+
			"⚠ The existing guard compares len(occupant_index) against `rows`, and `rows` is the MAXIMUM of the "+
			"parallel arrays — so it fires when an array is LONGER and is BLIND when one is SHORTER. A field "+
			"emitted only on the rows that need it is exactly the short case.\n"+
			"⚠ The middle row has just taken the THIRD person's company. gate.go's own comment says what that is: "+
			"\"would shift every LATER row's fields by one — silently attaching one person's name to another "+
			"person's company, and at 26 rows that is both likelier and far harder to spot than at four.\"\n"+
			"⚠ On a life-safety document, silently attaching one person's details to another is the worst failure "+
			"this system can produce, AND IT WOULD NOT ANNOUNCE ITSELF — D-71's own words.\n"+
			"⚠ THE ASSERTION MUST BE ON THE RECEIVED BODY, never on the parsed rows: parseVehicleForm has already "+
			"normalised the lengths and that normalisation is precisely what would hide the shift",
			len(short), len(trips))
	}
	if rec.Code != http.StatusConflict {
		t.Errorf("W8-23(g): a short-array submit answered %d, want 409 — the vehicle is refused WHOLE rather than "+
			"half-attributed", rec.Code)
	}
	if got := vehicleError(t, rec.Body.String(), gateInbound); !strings.Contains(got, "not submitted intact") {
		t.Errorf("W8-23(g): the refusal does not tell the officer what went wrong: %q", got)
	}
	trips, err := domain.Trips(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 0 {
		t.Fatalf("W8-23(g): a refused submit authored %d trip(s) — nothing may be recorded", len(trips))
	}
}

// TestW8LongOccupantArrayIsStillRefused is the CONTROL for the direction the
// existing guard ALREADY covers, run here so the parity assertion above is
// not mistaken for a replacement.
//
// ⚠ A build that "fixes" W8-23(g) by replacing the index check with a parity
// check has traded one blindness for another: the LONG direction is what
// occupant_index catches today. Both directions must hold.
//
// ⟦NO PRE-CHUNK RED — this direction is already guarded, which is the point.⟧
// Its evidence is that it is the arm a parity-only rewrite would lose.
func TestW8LongOccupantArrayIsStillRefused(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	_, vals := gateBuild(t, srv, gateInbound, "TRK-14",
		[]string{"Dana Driver", "Pat Passenger", "Chris Crew"}, 0)
	vals["occupant_company"] = append(vals["occupant_company"], "one too many")

	rec := postForm(t, srv, "/gate/checkin", vals)
	if rec.Code == http.StatusSeeOther {
		t.Fatalf("W8-23(g) CONTROL (the LONG direction): a submit carrying 3 indices and 4 occupant_company values " +
			"was ACCEPTED. This direction is the one occupant_index already catches; if it stops holding, a parity " +
			"rewrite has traded one blindness for another")
	}
	trips, err := domain.Trips(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 0 {
		t.Fatalf("W8-23(g) CONTROL: a refused submit authored %d trip(s)", len(trips))
	}

}
