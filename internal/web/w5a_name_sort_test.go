// w5a_name_sort_test.go — W5a-4 at the SEVEN WEB sort sites. (Sites 1 and 2
// are in internal/domain/w5a_created_half_test.go.)
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, criteria LOCKED 2026-08-31.
//
// D-48 derived ruling 2: "The sort reads Name, never LastName." With
// LastName newly available on the projection, keying the sort on it is the
// obvious "improvement" — and under this ruling it orders the roster by an
// invisible field that may disagree with what is printed on the page. Two
// officers reading the same muster would find the same person in different
// places.
//
// ⚠ "THE SORT" IS NINE SORTS (H13/TE-1). Measured at 88dd6a6 there are nine
// person-name ordering statements across five files and THEY SHARE NO
// COMPARATOR — there is no lessPersonName helper; each is an inline closure:
//
//  1. internal/domain/muster.go:232   bucket members            (domain file)
//  2. internal/domain/muster.go:255   flat summary bucket       (domain file)
//  3. internal/web/gate.go:343        on-site rows              <- here
//  4. internal/web/gate.go:369        gate roster / roster-data <- here
//  5. internal/web/trips.go:413       sortedPersons             <- here
//  6. internal/web/intake.go:367      roster People             <- here
//  7. internal/web/intake.go:368      NeedsCompletion           <- here
//  8. internal/web/intake.go:371      Unconfirmed               <- here
//  9. internal/web/picker.go:211      picker rows               <- here
//
// The enumeration is HAND-KEPT and is stated because nothing structural
// detects an unenumerated one. A test-writer given "the sort" and a mutant
// naming "the roster/muster sort" pins one surface, and (m5a-4) planted at
// any of the other eight survives — and since this criterion is already true,
// the mutant is its ONLY evidence, so an under-scoped mutant is under-scoped
// evidence with nothing behind it.
//
// ⚠ Collapsing the nine onto one shared comparator would ALSO satisfy W5a-4
// and is a perfectly good build. It is an OPTION, not an obligation — a
// criteria lock does not prescribe the build (arbiter §2.5). This file
// asserts the ORDER at each site, never the presence of nine closures.
//
// ⚠ D-36/J-12's case-folded surname sort with a PersonID tiebreak is
// deliberately NOT asserted: STRUCK from W5a-4 by the lock (M13) and recorded
// as a HOMELESS obligation in the Seams table. Measured, 0 of the 9 sorts
// case-fold and 1 of 9 (picker.go:211) carries a tiebreak, on Value rather
// than PersonID. Asserting it would make W5a-4 permanently red for another
// chunk's unbuilt work, and would steer the fixture author into a case-folded
// fixture that fails for the wrong reason.
//
// ⟦NO PRE-CHUNK RED — sort half⟧ All nine read Name at 88dd6a6, because no
// part is readable there. **A clean pass is not evidence.** The ledger row is
// (m5a-4) PRODUCED AT EACH of the nine sites.
package web

import (
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// TestW5aSortFixtureSelfCheckPositiveControlWeb — the runtime self-check's
// own positive control (M3/F-9: "the self-check is itself a check and needs a
// positive control"). A deliberately degenerate fixture — parts AGREEING with
// Name — is built for real, read back for real, and the fixture-defect
// verdict is OBSERVED, before any real fixture's pass counts.
//
// Without it, a self-check that could never fire would make every
// "non-degenerate" claim in this file true by construction — standing rule
// 5's exact shape.
func TestW5aSortFixtureSelfCheckPositiveControlWeb(t *testing.T) {
	_, st := w5aServer(t, "site-a", "")
	idA, idB := w5aSortFixture(t, st, "site-a", true /* degenerate */, nil, nil)
	agrees, defect := w5aRivalOrderAgrees(w5aReadBack(t, st, idA, idB))
	if defect != "" {
		t.Fatalf("CONTROL FAILED: the degenerate fixture could not be evaluated: %s", defect)
	}
	if !agrees {
		t.Fatalf("CONTROL FAILED: the self-check did NOT flag a deliberately degenerate fixture (parts agreeing with Name) — it can never fire, so W5a-4's non-degeneracy claim would be true by construction (M3)")
	}

	_, st2 := w5aServer(t, "site-a", "")
	idC, idD := w5aSortFixture(t, st2, "site-a", false, nil, nil)
	agrees2, defect2 := w5aRivalOrderAgrees(w5aReadBack(t, st2, idC, idD))
	if defect2 != "" || agrees2 {
		t.Fatalf("CONTROL FAILED: the self-check flagged the NON-degenerate fixture (agrees=%v defect=%q)", agrees2, defect2)
	}
}

// TestW5aGateSortsReadNameNotLastName — sites 3 and 4.
func TestW5aGateSortsReadNameNotLastName(t *testing.T) {
	srv, st := w5aServer(t, "site-a", "")
	co, err := domain.CreateCompany(st, "site-a", 1, "Kakivik Asset Management", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	idA, idB := w5aSortFixture(t, st, "site-a", false, &co.CompanyID, nil)
	w5aRequireNonDegenerateSortFixture(t, w5aReadBack(t, st, idA, idB))

	// --- SITE 3: gate.go:343, buildOnSiteRows. Called directly, so the
	// assertion is about that comparator and nothing else.
	personName := map[string]string{}
	for _, p := range w5aReadBack(t, st, idA, idB) {
		personName[p.PersonID] = p.Name
	}
	// The visits are supplied in the REVERSE of the certified order, so a
	// build that did not sort at all would also fail. Rule 6: the input
	// order is stated, not assumed.
	visits := []domain.Visit{
		{VisitID: "v-b", SiteID: "site-a", PersonID: idB, IsOpen: 1},
		{VisitID: "v-a", SiteID: "site-a", PersonID: idA, IsOpen: 1},
	}
	rows := buildOnSiteRows(visits, "site-a", personName)
	if len(rows) != 2 {
		t.Fatalf("fixture defect: buildOnSiteRows returned %d rows, want 2", len(rows))
	}
	w5aAssertNameOrder(t, "gate.go:343 (on-site rows)", []string{rows[0].Person, rows[1].Person})

	// --- SITE 4: gate.go:369, the gate page's roster-data blob. The sorted
	// slice feeds roster.Persons, which is marshalled into RosterJSON and
	// embedded in <script id="roster-data">, so the order IS observable.
	body := w5aGetAuthed(t, srv, "/gate")
	i := strings.Index(body, `id="roster-data"`)
	if i < 0 {
		t.Fatalf("fixture defect: the gate page carries no roster-data blob, so sort site 4 cannot be observed. Body length %d", len(body))
	}
	rest := body[i:]
	j := strings.Index(rest, "</script>")
	if j < 0 {
		t.Fatalf("fixture defect: no </script> after the roster-data blob")
	}
	w5aAssertNameOrder(t, "gate.go:369 (gate roster-data)",
		w5aOrderInText(t, rest[:j], "the gate roster-data blob", w5aExpectedNameOrder))
}

// TestW5aTripsSortedPersonsReadsNameNotLastName — site 5.
func TestW5aTripsSortedPersonsReadsNameNotLastName(t *testing.T) {
	_, st := w5aServer(t, "site-a", "")
	idA, idB := w5aSortFixture(t, st, "site-a", false, nil, nil)
	w5aRequireNonDegenerateSortFixture(t, w5aReadBack(t, st, idA, idB))

	persons, byID, err := sortedPersons(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(persons) != 2 {
		t.Fatalf("fixture defect: sortedPersons returned %d people, want 2", len(persons))
	}
	// Rule 6: the id->name index must agree with the slice, or the two halves
	// of this function have drifted apart and the order proves less than it
	// looks.
	for _, p := range persons {
		if byID[p.PersonID] != p.Name {
			t.Fatalf("fixture defect: sortedPersons' index says %q for %s but the slice says %q", byID[p.PersonID], p.PersonID, p.Name)
		}
	}
	w5aAssertNameOrder(t, "trips.go:413 (sortedPersons)", []string{persons[0].Name, persons[1].Name})
}

// TestW5aRosterPageSortsReadNameNotLastName — sites 6, 7 and 8, all three on
// the one /roster render.
//
// The fixture is a post whose ENTRANCE is elsewhere and whose two people have
// NO company, which is what puts both of them into all three lists at once:
// People (everyone), NeedsCompletion (no company on file) and Unconfirmed
// (record made away from the entrance, nothing linking it yet).
func TestW5aRosterPageSortsReadNameNotLastName(t *testing.T) {
	srv, st := w5aServer(t, "site-b", "site-a")
	idA, idB := w5aSortFixture(t, st, "site-b", false, nil, nil)
	w5aRequireNonDegenerateSortFixture(t, w5aReadBack(t, st, idA, idB))

	body := w5aGet(t, srv, "/roster")
	for _, site := range []struct{ table, label string }{
		{"roster-table", "intake.go:367 (roster People)"},
		{"needs-completion", "intake.go:368 (NeedsCompletion)"},
		{"unconfirmed-identities", "intake.go:371 (Unconfirmed)"},
	} {
		w5aAssertNameOrder(t, site.label, w5aOrderIn(t, body, site.table, w5aExpectedNameOrder))
	}
}

// TestW5aPickerRowsSortReadNameNotLastName — site 9, picker.go:211.
//
// The tiebreak on Value is exercised too, because the criterion's fixture
// must not accidentally make the primary key the only thing under test: two
// people with the SAME Name are added and their relative order pinned by
// Value, which is what the site's own comment says it is for ("stable for two
// people with one name").
func TestW5aPickerRowsSortReadNameNotLastName(t *testing.T) {
	_, st := w5aServer(t, "site-a", "")
	idA, idB := w5aSortFixture(t, st, "site-a", false, nil, nil)
	people := w5aReadBack(t, st, idA, idB)
	w5aRequireNonDegenerateSortFixture(t, people)

	// The people are handed to rosterGroups in the REVERSE of the certified
	// order, so a build that did not sort at all fails here too.
	reversed := []domain.Person{}
	for i := len(people) - 1; i >= 0; i-- {
		reversed = append(reversed, people[i])
	}
	if reversed[0].Name != w5aSortNameB {
		// people came back from Persons() in some order; force the reverse
		// of the certified one explicitly rather than hoping.
		reversed = []domain.Person{}
		for _, want := range []string{w5aSortNameB, w5aSortNameA} {
			for _, p := range people {
				if p.Name == want {
					reversed = append(reversed, p)
				}
			}
		}
	}
	if len(reversed) != 2 || reversed[0].Name != w5aSortNameB || reversed[1].Name != w5aSortNameA {
		t.Fatalf("fixture defect: the input to rosterGroups is not the reverse of the certified order: %v", w5aNamesOf(reversed))
	}

	groups := rosterGroups(reversed, map[string]string{}, map[string]string{}, nil)
	var rows []pickerRow
	for _, g := range groups {
		for _, sub := range g.Subs {
			rows = append(rows, sub.Rows...)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("fixture defect: the picker returned %d rows, want 2", len(rows))
	}
	w5aAssertNameOrder(t, "picker.go:211 (picker rows)", []string{rows[0].Name, rows[1].Name})
}

func w5aNamesOf(ps []domain.Person) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}
