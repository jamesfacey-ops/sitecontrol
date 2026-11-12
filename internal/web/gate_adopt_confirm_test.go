// gate_adopt_confirm_test.go — the gate's type-ahead ASKS before it adopts
// (James, 2026-09-09: "always confirm before adopting"; the why is on the
// .adopt-confirm block in gate.html).
//
// WHAT A GO TEST CAN AND CANNOT SEE HERE — read before trusting the green.
// The ask is client-side script, and this module carries no JavaScript
// runtime (gate.html, resolvePick's header: a decision, not an omission), so
// nothing in this package can press Enter in the open box. The scripted path
// was driven in a real browser for reports/witness/GATE-ADOPT-CONFIRM.md;
// that harness is not in this tree. What CAN be pinned without a runtime,
// and is:
//
//  1. the SHAPE. The served script no longer carries the one-keystroke
//     adoption of a lone filter match — the exact text the review at f7afc3a
//     measured — and every open box carries the ask's container, empty and
//     unnamed, so it sits outside every positional array.
//  2. the SERVER half of the same property, driven end to end through the
//     served markup. A typed name that is a PREFIX of a roster name
//     ("Ferguson, Mary" against "Ferguson, Maryanne") resolves to NOBODY on
//     the JS-off path and the commit puts a NEW person on the muster — so
//     the two are two lines, never one. And an exactly-typed roster name
//     still completes to that person, once, minting nobody. Both held at
//     the tip already (resolveTypedPerson is exact); they are pinned because
//     the script's fix was argued FROM them, and so a loosening of the
//     server rule cannot pass in silence.
package web

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// adoptedAtOneKeystroke is the defect's exact shape as measured
// (reports/witness/REVIEW-PERSON-DUP.md, at f7afc3a): a lone filter match
// handed straight to adopt. Either arm — the substring one or the exact
// one — is an unasked adoption, which the ruling forbids.
var adoptedAtOneKeystroke = regexp.MustCompile(`(ms|exact)\.length === 1\) \{ open\.adopt\(`)

func TestGateOpenBoxAsksBeforeAdoptingALoneMatch(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	code, page := getGate(t, srv)
	if code != http.StatusOK {
		t.Fatalf("GET /gate = %d", code)
	}
	// ⟦R-28, team-lead, 2026-09-16⟧ RETARGETED: the departing form only
	// ever appears via the roster's own tick-and-select door now
	// (gate_roster_half) — reach it the same way, then check its OWN
	// markup below instead of the fresh page's. Its open box (and the
	// adopt-confirm container inside it) is `hidden`, not omitted, by
	// Compact mode (.compact-hide's own comment: every occupant_* field
	// in there must stay in the DOM), so the checks below are unaffected
	// by Compact — this seeds only the state needed to reach the form.
	visitIDs := checkInCrew(t, srv, st, 1)
	outboundPage, _ := gateDepartSelected(t, srv, visitIDs)
	if m := adoptedAtOneKeystroke.FindString(page); m != "" {
		t.Errorf("the gate script still adopts a lone filter match at one keystroke: %q — a typed name that is a PREFIX of a roster name is taken as that person with no ask, which is a false merge", m)
	}
	for _, fn := range []string{"function proposeAdopt(", "function confirmAdopt(", "function declineAdopt("} {
		if !strings.Contains(page, fn) {
			t.Errorf("the gate script carries no %s — the ask is missing", fn)
		}
	}
	// James, 2026-10-03 (W8, M-25): the container is now a live region (role="status"); still empty and hidden when served.
	const container = `<div class="adopt-confirm" role="status" hidden></div>`
	for _, dir := range []string{gateInbound, gateOutbound} {
		src := page
		if dir == gateOutbound {
			src = outboundPage
		}
		form := gateFormMarkup(t, src, formIDFor(dir))
		i := strings.Index(form, container)
		if i < 0 {
			t.Errorf("the %q form serves no empty adopt-confirm container — the script has nowhere to ask, and a scriptless browser must see nothing:\n%s", dir, form)
			continue
		}
		if n := strings.Count(form, `class="adopt-confirm"`); n != 1 {
			t.Errorf("the %q form serves %d adopt-confirm containers, want exactly 1", dir, n)
		}
		// Inside the open box: after its visible input, before its hidden
		// fields — and carrying no name, so nothing about the positional
		// arrays moves.
		if box := strings.Index(form, `class="p-open"`); !(box >= 0 && box < i && i < strings.Index(form, `name="new_kind"`)) {
			t.Errorf("the %q form's adopt-confirm container is not between the open box's input and its hidden fields", dir)
		}
	}
}

// TestGatePrefixOfARosterNameIsNotAdoptedByTheServer drives the JS-off door
// with the review's own example. The server's rule is exact, so "Ferguson,
// Mary" is nobody on a roster holding "Ferguson, Maryanne": the row is shown
// as a new person, and the commit mints her — two people, two lines.
func TestGatePrefixOfARosterNameIsNotAdoptedByTheServer(t *testing.T) {
	srv, st, fixture := newGateTestServer(t)
	co, err := domain.CreateCompany(st, "site-a", 1, "ASRC", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	tr, err := domain.CreateTrade(st, "site-a", 1, "Catering", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	maryanne, err := domain.CreatePerson(st, "site-a", 1, "Ferguson, Maryanne", &co.CompanyID, &tr.TradeID, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	page, vals := gateBuild(t, srv, gateInbound, "TRK-14", []string{"Ferguson, Mary"}, 0)
	form := gateFormMarkup(t, page, formIDFor(gateInbound))
	if strings.Contains(form, `value="`+maryanne.PersonID+`"`) {
		t.Fatalf("the accumulated row for %q carries Maryanne's id — the server adopted a prefix match:\n%s", "Ferguson, Mary", form)
	}
	if !strings.Contains(form, notOnRoster) {
		t.Fatalf("the accumulated row for %q is not shown as a new person:\n%s", "Ferguson, Mary", form)
	}

	// ⟦R-26⟧ this test is about NAME resolution, not company — give the new
	// row one so the commit is not refused for the reason R-26 added.
	gateFillNewRowCompanies(t, vals, "R-26 Test Co")

	rec := postForm(t, srv, "/gate/checkin", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("commit = %d, want 303 — the entry was BLOCKED: %s", rec.Code, rec.Body.String())
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 {
		t.Fatalf("%d open visits after one check-in, want 1", len(open))
	}
	if open[0].PersonID == maryanne.PersonID {
		t.Fatalf("FALSE MERGE: the visit for the typed %q is Maryanne's (%s) — one muster line where there should be two", "Ferguson, Mary", maryanne.PersonID)
	}
	people, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if want := len(fixture) + 2; len(people) != want {
		t.Fatalf("the roster holds %d people, want %d (the fixture, Maryanne, and a NEW Mary)", len(people), want)
	}
	var visited *domain.Person
	for i := range people {
		if people[i].PersonID == open[0].PersonID {
			visited = &people[i]
		}
	}
	if visited == nil || visited.Name != "Ferguson, Mary" {
		t.Fatalf("the person on the muster is %+v, want a new person named %q", visited, "Ferguson, Mary")
	}
}

// TestGateExactlyTypedRosterNameStillCompletesToThatPerson is the never-block
// half: a routine entry, typed as the roster has it, still ends on the
// muster as that one person, with nobody minted.
func TestGateExactlyTypedRosterNameStillCompletesToThatPerson(t *testing.T) {
	srv, st, fixture := newGateTestServer(t)
	dana := fixture[0]
	_, vals := gateBuild(t, srv, gateInbound, "TRK-15", []string{dana.Name}, 0)
	rec := postForm(t, srv, "/gate/checkin", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("commit = %d, want 303 — the entry was BLOCKED: %s", rec.Code, rec.Body.String())
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].PersonID != dana.PersonID {
		t.Fatalf("open visits = %+v, want exactly one, for %s (%s)", open, dana.Name, dana.PersonID)
	}
	people, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != len(fixture) {
		t.Fatalf("the roster grew to %d from %d — an exactly-typed roster name minted a duplicate", len(people), len(fixture))
	}
}
