// w5a_seams_test.go — THE ONE place W5a's web tests bind what the LOCKED
// criteria leave undetermined, plus the shared fixture machinery. This file
// holds NO assertions of its own.
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, criteria LOCKED 2026-08-31.
//
// ⚠ NO UNBUILT GO SYMBOL IS REFERENCED, deliberately — see
// internal/domain/w5a_seams_test.go's header for the reasoning. Everything
// here drives the REAL HTTP surfaces (`POST /roster/intake`, `GET /roster`,
// `GET /gate`) or calls the package's own unexported helpers
// (`composeName`, `resolveTypedPerson`, `rowPersonSlot`, `buildOnSiteRows`,
// `sortedPersons`, `rosterGroups`, `renderRoster`), so every failure at
// `88dd6a6` is a PRODUCED RUNTIME RED rather than a compile error, and the
// builder is left free to choose the authoring signature.
//
// SHAPE PRESUMPTIONS (SEAMS), each bound once, disputes go to the gate:
//
//   - SEAM W5a-S-FORM: the intake form's two name boxes keep their existing
//     field names `last_name` / `first_name` and their existing control ids
//     `last-name` / `first-name` (templates/intake.html:158/:162 — MEASURED).
//     W5a changes what intake DOES with the two boxes it already has; it does
//     not rename them.
//   - SEAM W5a-S-KEEP: on a successful enrolment the two typed parts are
//     recorded on the person as `LastName` / `FirstName` VERBATIM, i.e. the
//     values `composeName` was given after its own whitespace collapse. W5a-2
//     says intake "ceases to discard what it already collects"; it does not
//     say what normalisation, if any, the stored part carries, and this is
//     the narrowest reading — the part is what the officer typed.
//   - SEAM W5a-S-VALUE: refusal preservation is asserted on the rendered
//     `value="…"` attribute of the named control, never by
//     `strings.Contains` over the body. This is not a free choice: it is
//     W5-X2, made standing after the W4 gate found a body-contains assertion
//     that passed on the refusal sentence quoting the value back.
//
// NOT seams (DOC-pinned or MEASURED, cited at each point of use): the nine
// sort sites; the four person-name compare sites; the three roster table ids
// (`unconfirmed-identities`, `needs-completion`, `roster-table`).
package web

import (
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// --- the W5a-4 fixture, shared by every web sort site -------------------
//
// Two people whose LastName order is the REVERSE of their Name order. Lawful
// under D-48 — and the lock's own stated gain from that ruling: before D-48
// such a person was an undefined state nobody would put in a fixture.
const (
	w5aSortNameA = "Aaron, Zed"
	w5aSortNameB = "Zeta, Ann"
	w5aSortLastA = "Zulu"  // disagrees with the surname in Name A
	w5aSortLastB = "Alpha" // disagrees with the surname in Name B
)

// w5aExpectedNameOrder is the CERTIFIED order, pinned as a LITERAL.
var w5aExpectedNameOrder = []string{w5aSortNameA, w5aSortNameB}

// w5aServer builds a post with a chosen site id and entrance, a login
// password, and NO seeded people — every W5a fixture states its own roster.
func w5aServer(t *testing.T, siteID, entranceID string) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "w5a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		SiteID: siteID, DisplayName: siteID, ListenAddr: "127.0.0.1:0",
		NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 10,
		LoginPassword:  testLoginPassword,
		EntranceSiteID: entranceID,
	}
	return New(cfg, st), st
}

// w5aPerson creates a person and, when last != "", amends their LastName to
// it. The part arrives by AMENDMENT rather than at creation, deliberately:
// the amendment path exists at 88dd6a6, so a fixture that needs a divergent
// part does not depend on W5a-2 landing and means the same thing before and
// after the build.
func w5aPerson(t *testing.T, st *store.Store, siteID, name, last string, companyID, tradeID *string) domain.Person {
	t.Helper()
	p, err := domain.CreatePerson(st, siteID, 1, name, companyID, tradeID, 0, "default")
	if err != nil {
		t.Fatalf("fixture defect: creating %q: %v", name, err)
	}
	if last != "" {
		if _, err := domain.AmendPerson(st, siteID, 1, domain.PersonAmendment{
			PersonID: p.PersonID, LastName: &last,
		}, 0, "default"); err != nil {
			t.Fatalf("fixture defect: amending %q: %v", name, err)
		}
	}
	return p
}

// w5aSortFixture builds the two-person W5a-4 fixture on st. With
// degenerate=true the parts AGREE with the Names — the deliberately
// degenerate fixture the self-check's positive control needs.
func w5aSortFixture(t *testing.T, st *store.Store, siteID string, degenerate bool, companyID, tradeID *string) (idA, idB string) {
	t.Helper()
	lastA, lastB := w5aSortLastA, w5aSortLastB
	if degenerate {
		lastA, lastB = "Aaron", "Zeta"
	}
	a := w5aPerson(t, st, siteID, w5aSortNameA, lastA, companyID, tradeID)
	b := w5aPerson(t, st, siteID, w5aSortNameB, lastB, companyID, tradeID)
	return a.PersonID, b.PersonID
}

// w5aRivalOrderAgrees recomputes the LastName ordering FROM READ-BACK DATA —
// never from the fixture constants — and reports whether it equals the
// certified Name ordering. A non-empty defect means the fixture cannot pose
// the case at all.
func w5aRivalOrderAgrees(people []domain.Person) (agrees bool, defect string) {
	if len(people) < 2 {
		return false, "the sort fixture holds fewer than two people"
	}
	byName := append([]domain.Person{}, people...)
	sort.Slice(byName, func(i, j int) bool { return byName[i].Name < byName[j].Name })
	byLast := append([]domain.Person{}, people...)
	for _, p := range byLast {
		if p.LastName == nil {
			return false, "person " + p.Name + " has no last_name read back, so the LastName rival cannot be computed"
		}
	}
	sort.Slice(byLast, func(i, j int) bool { return *byLast[i].LastName < *byLast[j].LastName })
	var a, b []string
	for i := range byName {
		a = append(a, byName[i].PersonID)
		b = append(b, byLast[i].PersonID)
	}
	return strings.Join(a, "|") == strings.Join(b, "|"), ""
}

// w5aReadBack returns the named people as READ BACK through the composed
// read path.
func w5aReadBack(t *testing.T, st *store.Store, ids ...string) []domain.Person {
	t.Helper()
	all, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []domain.Person
	for _, p := range all {
		if want[p.PersonID] {
			out = append(out, p)
		}
	}
	if len(out) != len(ids) {
		t.Fatalf("fixture defect: read back %d of the %d fixture people", len(out), len(ids))
	}
	return out
}

// w5aRequireNonDegenerateSortFixture is W5a-4's runtime self-check. It fails
// as an explicit FIXTURE DEFECT — never as a criterion failure.
func w5aRequireNonDegenerateSortFixture(t *testing.T, people []domain.Person) {
	t.Helper()
	agrees, defect := w5aRivalOrderAgrees(people)
	if defect != "" {
		t.Fatalf("fixture defect: %s", defect)
	}
	if agrees {
		t.Fatalf("fixture defect: sorting by Name and sorting by LastName return the SAME order on this fixture, so a build keying the sort on LastName would pass it. Standing rule 3: a criterion certifying WHICH rule is in force needs a fixture on which the certified rule and every named rival disagree")
	}
}

// --- order observation --------------------------------------------------

// w5aOrderIn returns want's members in the order they appear inside the
// section of body that starts at `id="<tableID>"` and ends at the next
// `</table>`. It FAILS if a wanted name is absent from that section, so an
// assertion can never pass over a list that is not there — searching for a
// name in an empty string is exactly the false pass this project keeps
// finding.
func w5aOrderIn(t *testing.T, body, tableID string, want []string) []string {
	t.Helper()
	marker := `id="` + tableID + `"`
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("fixture defect: the rendered page has no element %s — the sort site it carries cannot be observed", marker)
	}
	rest := body[i:]
	j := strings.Index(rest, "</table>")
	if j < 0 {
		t.Fatalf("fixture defect: no </table> after %s", marker)
	}
	return w5aOrderInText(t, rest[:j], "section "+tableID, want)
}

// w5aOrderInText is w5aOrderIn over an already-scoped fragment.
func w5aOrderInText(t *testing.T, fragment, what string, want []string) []string {
	t.Helper()
	type at struct {
		name string
		idx  int
	}
	var found []at
	for _, name := range want {
		k := strings.Index(fragment, name)
		if k < 0 {
			t.Fatalf("fixture defect: %q is absent from %s, so its position there proves nothing", name, what)
		}
		found = append(found, at{name, k})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].idx < found[j].idx })
	out := make([]string, 0, len(found))
	for _, f := range found {
		out = append(out, f.name)
	}
	return out
}

// w5aAssertNameOrder pins the certified order as a LITERAL at one site.
func w5aAssertNameOrder(t *testing.T, site string, got []string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(w5aExpectedNameOrder, "|") {
		t.Errorf("W5a-4 (m5a-4) at %s: order = %v, want %v. Every person-name sort reads the composed Name, never LastName — a part may diverge from what is printed on the page, so ordering by an invisible field puts the same person in different places on two officers' screens (D-48 derived ruling 2)", site, got, w5aExpectedNameOrder)
	}
}

// --- form-control assertions (W5-X2 / SEAM W5a-S-VALUE) -----------------

// w5aControlValue returns the `value="…"` attribute of the input whose id is
// controlID, and whether the control was found at all. It reads the whole
// element declaration, so an attribute written before the id is still seen.
func w5aControlValue(body, controlID string) (value string, found bool) {
	marker := `id="` + controlID + `"`
	i := strings.Index(body, marker)
	if i < 0 {
		return "", false
	}
	start := strings.LastIndex(body[:i], "<")
	if start < 0 {
		return "", false
	}
	end := strings.Index(body[start:], ">")
	if end < 0 {
		return "", false
	}
	tag := body[start : start+end+1]
	k := strings.Index(tag, `value="`)
	if k < 0 {
		return "", true // the control exists and carries NO value attribute
	}
	rest := tag[k+len(`value="`):]
	q := strings.Index(rest, `"`)
	if q < 0 {
		return "", true
	}
	return rest[:q], true
}

// w5aGet fetches a page through the real handler.
// w5aGetAuthed is w5aGet with a login session. PII-1: /gate serves the roster
// payload only to an authenticated request, so a test reading the payload
// must be logged in the way an operator is.
func w5aGetAuthed(t *testing.T, srv *Server, path string) string {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.AddCookie(loginCookie(t, srv, testLoginPassword))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec.Body.String()
}

func w5aGet(t *testing.T, srv *Server, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec.Body.String()
}
