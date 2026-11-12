// w5a_intake_edge_test.go — W5a at the INTAKE EDGE:
//
//	W5a-7(a)  the refusal preserves everything typed, asserted on the form
//	          CONTROL's value="…" (W5-X2), with a real field-discarding
//	          re-render as its CONTROL
//	W5a-2     intake stops discarding the two parts it already collects
//	W5a-8     composition is one-way at the authoring edge
//
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, criteria LOCKED 2026-08-31.
// Seams and machinery: w5a_seams_test.go (same package).
package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// w5aIntakeServer is a post with one company, so intake has something to
// attach to (a person with no company is refused for a different reason,
// which would make every assertion below pass for the wrong cause).
func w5aIntakeServer(t *testing.T) (*Server, string) {
	t.Helper()
	srv, st := w5aServer(t, "site-a", "")
	co, err := domain.CreateCompany(st, "site-a", 1, "Kakivik Asset Management", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	return srv, co.CompanyID
}

// =======================================================================
// W5a-7(a) — requiredness lives at the intake edge, with everything typed
// preserved
// =======================================================================

// TestW5aIntakeRefusalPreservesTypedNamePartsInFormControls — W5a-7's
// refusal-preservation half.
//
// ⟦NO PRE-CHUNK RED⟧ — and this half was locked UNMARKED until rev 7 found
// it. The intake refusal ALREADY re-renders into the form controls at
// `88dd6a6`: templates/intake.html:159/:163 carry
// `value="{{.Form.Last}}"`/`value="{{.Form.First}}"` and `intake.go:427`'s
// `refuse(...)` goes through `renderRoster(w, r, 409, form, …)`. The refusal
// sentence occurs in ZERO test files, so **booking this test's clean pass as
// a ledger row credits work nobody did.** Its evidence is the planted
// field-discarding re-render — (m5a-7b) — produced below through the REAL
// template and the REAL render path.
//
// (m5a-7) cannot serve this half: it is a createPerson/fold mutant and leaves
// the re-render untouched.
//
// ⚠ ACCOUNTING (§5's rule, applied across criteria): W5a-7 and W5b-2 assert
// the same property on the same form. If the builder writes one shared
// helper, ONE produced red must not be booked for two criteria. **This
// criterion's plant discards a NAME PART**; W5b-2's discards the phone.
//
// The assertion is the W5-X2 / SEAM W5a-S-VALUE form: `value="…"` on the
// named control, NEVER strings.Contains over the body — the W4 gate found a
// body-contains assertion that passed on the refusal sentence quoting the
// value back, so a mutant discarding the officer's typing survived it.
func TestW5aIntakeRefusalPreservesTypedNamePartsInFormControls(t *testing.T) {
	srv, companyID := w5aIntakeServer(t)

	// --- CONTROL FIRST — the planted field-discarding re-render, (m5a-7b).
	// This is not a synthetic string: it is the REAL renderRoster path with
	// an EMPTY intakeForm, which is byte-for-byte what a re-render that
	// dropped the officer's typing produces. If the assertion below cannot
	// tell it from the real refusal, the assertion proves nothing.
	rec := httptest.NewRecorder()
	srv.renderRoster(rec, httptest.NewRequest("GET", "/roster", nil), http.StatusConflict,
		intakeForm{}, "planted: a refusal that discarded what was typed", "", false)
	discarded := rec.Body.String()
	for _, control := range []string{"last-name", "first-name"} {
		v, found := w5aControlValue(discarded, control)
		if !found {
			t.Fatalf("CONTROL FAILED: the refusal page has no control id=%q at all (SEAM W5a-S-FORM) — the assertion cannot discriminate", control)
		}
		if v != "" {
			t.Fatalf("CONTROL FAILED: the field-discarding re-render put %q in the %s control; the plant is not a plant", v, control)
		}
	}
	// And the mirror the W4 gate's finding demands: a body-contains
	// assertion CANNOT tell the two apart, which is why W5-X2 forbids it.
	// Demonstrated once, here, so the reason the form-control form is used is
	// on the record rather than asserted.
	if strings.Contains(discarded, "Ferguson") {
		t.Fatalf("CONTROL: the discarding re-render already contains \"Ferguson\" — the fixture cannot demonstrate W5-X2's point")
	}

	// --- the real refusal: a half-entered name, with the last name typed.
	rec = postForm(t, srv, "/roster/intake", intakeForm2("Ferguson", "", companyID, "", "", ""))
	if rec.Code != http.StatusConflict {
		t.Fatalf("W5a-7: a half-entered name returned %d, want 409 — requiredness lives at the intake edge (plan `:2032`)", rec.Code)
	}
	body := rec.Body.String()
	got, found := w5aControlValue(body, "last-name")
	if !found {
		t.Fatalf("W5a-7 (m5a-7b): the refusal page carries no `last-name` control — there is nothing for the officer to correct")
	}
	if got != "Ferguson" {
		t.Errorf("W5a-7 (m5a-7b): the refusal re-rendered the `last-name` control with value=%q, want %q. Intake refuses a half-entered name WITH EVERYTHING TYPED PRESERVED — a refusal that empties the boxes makes the officer retype a name they already typed, in front of the person", got, "Ferguson")
	}
	// The other control must exist and be empty — that is what was NOT typed.
	if v, found := w5aControlValue(body, "first-name"); !found || v != "" {
		t.Errorf("W5a-7: the refusal rendered `first-name` value=%q (found=%v), want an empty control", v, found)
	}

	// The mirror case, so the assertion is not keyed to one box.
	rec = postForm(t, srv, "/roster/intake", intakeForm2("", "Mary", companyID, "", "", ""))
	if rec.Code != http.StatusConflict {
		t.Fatalf("W5a-7: a half-entered name (first only) returned %d, want 409", rec.Code)
	}
	if got, _ := w5aControlValue(rec.Body.String(), "first-name"); got != "Mary" {
		t.Errorf("W5a-7 (m5a-7b): the refusal re-rendered the `first-name` control with value=%q, want %q", got, "Mary")
	}
}

// =======================================================================
// W5a-2 — intake stops discarding the parts it already collects
// =======================================================================

// TestW5aIntakeKeepsTheNamePartsItCollects — the third of W5a's three build
// deltas ("intake stops discarding the parts it already collects").
//
// EXPECTED RED (pre-chunk, runtime): intake composes at `intake.go:425` and
// throws both boxes away; the enrolled person's LastName/FirstName are
// absent.
// EXPECTED RED (m5a-2): the fold silently not writing a part.
func TestW5aIntakeKeepsTheNamePartsItCollects(t *testing.T) {
	srv, companyID := w5aIntakeServer(t)
	rec := postForm(t, srv, "/roster/intake", intakeForm2("Ferguson", "Mary", companyID, "", "", ""))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("fixture defect: a complete intake returned %d, want 303: %s", rec.Code, rec.Body.String())
	}

	people := personNamed(t, srv, "Ferguson, Mary")
	if len(people) != 1 {
		t.Fatalf("fixture defect: %d people named %q after one enrolment, want 1", len(people), "Ferguson, Mary")
	}
	p := people[0]
	if p.LastName == nil {
		t.Errorf("W5a-2: the enrolled person carries no last_name. Intake ALREADY collects Last and First as two boxes and discards them by composing at intake.go:425 — W5a's third delta is that it stops")
	} else if *p.LastName != "Ferguson" {
		t.Errorf("W5a-2 (SEAM W5a-S-KEEP): last_name = %q, want %q — the part is what the officer typed", *p.LastName, "Ferguson")
	}
	if p.FirstName == nil {
		t.Errorf("W5a-2: the enrolled person carries no first_name")
	} else if *p.FirstName != "Mary" {
		t.Errorf("W5a-2 (SEAM W5a-S-KEEP): first_name = %q, want %q", *p.FirstName, "Mary")
	}
	if p.Name != "Ferguson, Mary" {
		t.Errorf("W5a-2/W5a-3: Name = %q, want the composed %q — display Name stays required and authoritative", p.Name, "Ferguson, Mary")
	}
	// D-21.4: Position is not an intake field, so it stays absent.
	if p.Position != nil {
		t.Errorf("W5a-9: an intake enrolment set Position = %q; intake has no position box, so it must stay absent", *p.Position)
	}
}

// =======================================================================
// W5a-8 — composition is one-way
// =======================================================================

// TestW5aCompositionIsOneWayAtTheIntakeEdge — W5a-8's behavioural half. The
// structural half (no reader assigns to a part from Name) is
// TestW5aCompositionIsOneWayStructurally in internal/store.
//
// ⟦NO PRE-CHUNK RED⟧ — `composeName` exists and is already one-way
// (intake.go:200–214). **The clean pass is not evidence**; the planted
// reverse assignment in the structural half is.
//
// EXPECTED RED (m5a-8): composeName made two-way, or an edge path assigning
// parts from Name — the second half below reds, because a person created
// with a comma-bearing Name and no parts would acquire them.
func TestW5aCompositionIsOneWayAtTheIntakeEdge(t *testing.T) {
	// Forward: parts -> Name, in the canonical "Last, First" order.
	for _, c := range []struct{ last, first, want string }{
		{"Van der Berg", "Jan", "Van der Berg, Jan"},
		{"Ferguson", "Mary", "Ferguson, Mary"},
		{"O'Brien", "Mary Kate", "O'Brien, Mary Kate"},
		{"Cher", "", "Cher"},
		{"", "Prince", "Prince"},
	} {
		if got := composeName(c.last, c.first); got != c.want {
			t.Errorf("W5a-8: composeName(%q, %q) = %q, want %q", c.last, c.first, got, c.want)
		}
	}

	// Forward, end to end through the edge: what intake stores as Name is
	// what composeName produces from the two boxes.
	srv, companyID := w5aIntakeServer(t)
	if rec := postForm(t, srv, "/roster/intake", intakeForm2("Van der Berg", "Jan", companyID, "", "", "")); rec.Code != http.StatusSeeOther {
		t.Fatalf("fixture defect: enrolment returned %d, want 303", rec.Code)
	}
	people := personNamed(t, srv, composeName("Van der Berg", "Jan"))
	if len(people) != 1 {
		t.Fatalf("W5a-8: %d people named %q — intake's stored Name is not composeName's output", len(people), composeName("Van der Berg", "Jan"))
	}

	// REVERSE, and this is the half that can fail: a person whose Name
	// obviously LOOKS decomposable — "Last, First", with a comma — created
	// through an authoring path that was given NO parts must end up with NO
	// parts. Nothing anywhere derives a part from a Name.
	_, st := srv, srv.Store
	q, err := domain.CreatePerson(st, "site-a", 1, "Decomposable, Name", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	got, err := domain.PersonByID(st, q.PersonID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatalf("fixture defect: no person %s", q.PersonID)
	}
	// Rule 6 — the arrangement: the Name really does look decomposable, or
	// this assertion is about a name no reverse composition would touch.
	if !strings.Contains(got.Name, ", ") {
		t.Fatalf("fixture defect: %q carries no %q separator, so a reverse composition would have nothing to split on", got.Name, ", ")
	}
	if got.LastName != nil {
		t.Errorf("W5a-8 (m5a-8): a person created with no parts acquired last_name = %q from their Name — composition is one-way: parts -> Name at the authoring edge, NEVER Name -> parts anywhere", *got.LastName)
	}
	if got.FirstName != nil {
		t.Errorf("W5a-8 (m5a-8): a person created with no parts acquired first_name = %q from their Name", *got.FirstName)
	}
}
